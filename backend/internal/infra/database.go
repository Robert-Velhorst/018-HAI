package infra

import (
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var (
	defaultDBMu          sync.Mutex
	defaultDB            *gorm.DB
	defaultDBStarting    chan struct{}
	defaultDBStartCancel context.CancelFunc
	defaultDBClosed      bool
	defaultDBCloseDone   chan struct{}
	defaultDBCloseErr    error
	openConfiguredDB     = OpenDefaultDBContext
	runDefaultMigrations = RunMigrationsContext
)

var ErrDefaultDBClosed = errors.New("shared database pool is shut down")

func NewPostgresDatabase(user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	return NewPostgresDatabaseContext(context.Background(), user, password, dbName, dbHost, dbPort)
}

// OpenDefaultDB opens the configured database without running migrations. Use it
// for read-only operations (e.g. `migrate status`) or explicit rollbacks.
func OpenDefaultDB() (*gorm.DB, error) {
	return OpenDefaultDBContext(context.Background())
}

// OpenDefaultDBContext opens a caller-owned, migration-free pool with the
// caller's deadline covering acquisition as well as the initial ping.
func OpenDefaultDBContext(ctx context.Context) (*gorm.DB, error) {
	return NewPostgresDatabaseContext(ctx, config.AppConfig.DbUser, config.AppConfig.DbPassword,
		config.AppConfig.DbName, config.AppConfig.DbHost, config.AppConfig.DbPort)
}

func GetDefaultDB() (*gorm.DB, error) {
	return GetDefaultDBContext(context.Background())
}

// GetDefaultDBContext preserves a neutral cached handle. Only initialization
// uses the first caller's context; waiting callers can leave without cancelling it.
func GetDefaultDBContext(ctx context.Context) (*gorm.DB, error) {
	if ctx == nil {
		return nil, errors.New("shared database acquisition requires a context")
	}
	var startupCtx context.Context
	var cancel context.CancelFunc
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		defaultDBMu.Lock()
		if defaultDBClosed {
			defaultDBMu.Unlock()
			return nil, ErrDefaultDBClosed
		}
		if defaultDB != nil {
			db := defaultDB
			defaultDBMu.Unlock()
			return db, nil
		}
		if pending := defaultDBStarting; pending != nil {
			defaultDBMu.Unlock()
			select {
			case <-pending:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		timeout, err := databaseStartupTimeout()
		if err != nil {
			defaultDBMu.Unlock()
			return nil, err
		}
		startupCtx, cancel = context.WithTimeout(ctx, timeout)
		defaultDBStarting = make(chan struct{})
		defaultDBStartCancel = cancel
		defaultDBMu.Unlock()
		break
	}
	defer cancel()

	db, err := openConfiguredDB(startupCtx)
	if err == nil && db == nil {
		err = errors.New("shared database opener returned no pool")
	}
	defaultDBMu.Lock()
	closed := defaultDBClosed
	defaultDBMu.Unlock()
	if err == nil && closed {
		err = ErrDefaultDBClosed
	}
	if err == nil {
		err = startupCtx.Err()
	}
	if err == nil && migrationsEnabledAtStartup() {
		err = runDefaultMigrations(startupCtx, db)
	}
	defaultDBMu.Lock()
	if err == nil {
		err = startupCtx.Err()
	}
	if err == nil && !defaultDBClosed {
		defaultDB = db
		close(defaultDBStarting)
		defaultDBStarting = nil
		defaultDBStartCancel = nil
		defaultDBMu.Unlock()
		return db, nil
	}
	closed = defaultDBClosed
	defaultDBMu.Unlock()
	var closeErr error
	if db != nil {
		closeErr = closePostgresDatabase(db)
	}
	defaultDBMu.Lock()
	if defaultDBClosed {
		defaultDBCloseErr = errors.Join(defaultDBCloseErr, closeErr)
		closed = true
	}
	close(defaultDBStarting)
	defaultDBStarting = nil
	defaultDBStartCancel = nil
	defaultDBMu.Unlock()
	if closed {
		err = errors.Join(err, ErrDefaultDBClosed)
	}
	return nil, errors.Join(err, closeErr)
}

// CloseDefaultDB is terminal for this process. Call only after API work drains.
// Candidate initialization and driver cleanup finish without holding the cache lock.
func CloseDefaultDB() error {
	defaultDBMu.Lock()
	if done := defaultDBCloseDone; done != nil {
		defaultDBMu.Unlock()
		<-done
		defaultDBMu.Lock()
		err := defaultDBCloseErr
		defaultDBMu.Unlock()
		return err
	}
	defaultDBClosed = true
	defaultDBCloseDone = make(chan struct{})
	db, pending := defaultDB, defaultDBStarting
	cancelStartup := defaultDBStartCancel
	defaultDB = nil
	defaultDBMu.Unlock()
	if cancelStartup != nil {
		cancelStartup()
	}
	if pending != nil {
		<-pending
	}
	var closeErr error
	if db != nil {
		closeErr = closePostgresDatabase(db)
	}
	defaultDBMu.Lock()
	defaultDBCloseErr = errors.Join(defaultDBCloseErr, closeErr)
	close(defaultDBCloseDone)
	err := defaultDBCloseErr
	defaultDBMu.Unlock()
	return err
}

// resetDefaultDBForTest clears the package connection cache. It is deliberately
// unexported: production uses one migrated pool for its process lifetime.
func resetDefaultDBForTest() {
	defaultDBMu.Lock()
	defer defaultDBMu.Unlock()
	defaultDB = nil
	defaultDBStarting = nil
	defaultDBStartCancel = nil
	defaultDBClosed = false
	defaultDBCloseDone = nil
	defaultDBCloseErr = nil
}

func databaseStartupTimeout() (time.Duration, error) {
	if raw := strings.TrimSpace(os.Getenv("DB_STARTUP_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return 0, errors.New("DB_STARTUP_TIMEOUT must be a positive duration, e.g. 5m")
		}
		return timeout, nil
	}
	return 5 * time.Minute, nil
}

// migrationsEnabledAtStartup keeps existing installations compatible while
// allowing a production API process to run with a DML-only database role.
// Schema migrations must then be applied by the explicit `app migrate up`
// command using the separate migration-owner credentials. An invalid value
// fails closed: the process will not silently assume schema privileges.
func migrationsEnabledAtStartup() bool {
	value, exists := os.LookupEnv("DB_MIGRATIONS_ENABLED")
	if !exists || strings.TrimSpace(value) == "" {
		return true
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && enabled
}

// autoMigrateEnabled reports whether Gorm AutoMigrate should run for table
// creation. It defaults to FALSE: the versioned migrations — including the
// generated baseline in migrations/pre/0002_baseline — are the source of truth
// for the schema, so production never mutates its own schema implicitly.
//
// Set DB_AUTOMIGRATE=true only in development, to let Gorm materialise a new
// model before you regenerate the baseline migration for it.
func autoMigrateEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DB_AUTOMIGRATE"))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// autoMigrateMissingTables is intentionally narrower than GORM AutoMigrate.
// Versioned SQL is authoritative for every existing table, index, and
// constraint. In the deliberate development-only AutoMigrate mode, only a
// model whose table is genuinely absent may be materialised. Altering an
// existing table remains a reviewed migration responsibility.
func autoMigrateMissingTables(db *gorm.DB, candidates ...interface{}) error {
	missing := make([]interface{}, 0, len(candidates))
	for _, candidate := range candidates {
		if !db.Migrator().HasTable(candidate) {
			missing = append(missing, candidate)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return db.AutoMigrate(missing...)
}

func RunMigrations(db *gorm.DB) error {
	return RunMigrationsContext(context.Background(), db)
}

// The migration clone must not pin the runtime pool to a startup deadline.
func RunMigrationsContext(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil || db.Config == nil {
		return errors.New("migrations require a context and initialized database")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db = db.WithContext(ctx)
	// Phase 1: versioned migrations that must precede table creation (extensions
	// the models' UUID defaults depend on).
	if _, err := ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		return fmt.Errorf("apply pre migrations: %w", err)
	}
	// pgvector is opt-in. Normal Postgres deployments remain usable when no
	// local embedding endpoint has been reviewed; an enabled deployment fails
	// early if its database image does not contain the extension.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_SEMANTIC_RETRIEVAL_ENABLED")), "true") {
		if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
			return fmt.Errorf("enable pgvector extension: %w", err)
		}
	}

	// Phase 2: optional dev-only missing-table creation. The baseline migration
	// already created every versioned table, so this is opt-in
	// (DB_AUTOMIGRATE=true) and exists only to materialise a newly-added model
	// before its migration is generated. It must never alter migration-owned
	// columns, indexes, or constraints.
	if autoMigrateEnabled() {
		if err := autoMigrateMissingTables(db,
			&models.Automation{},
			&models.AutomationHealthEvent{},
			&models.AutomationLaunchEvent{},
			&models.OpenClawGatewaySessionReceipt{},
			&models.OpenClawGatewayArtifactReceipt{},
			&models.AutomationDependency{},
			&models.AutomationRouteCheck{},
			&models.AutomationAlert{},
			&models.AutomationIncident{},
			&models.AutomationSLO{},
			&models.LLMProviderProbe{},
			&models.ContextMemory{},
			&models.AIConversationArchive{},
			&models.AIMemoryInsight{},
			&models.SourceConnector{},
			&models.ConnectedSource{},
			&models.SourceSyncJob{},
			&models.SourceRawItem{},
			&models.SourceExtraction{},
			&models.SourceExtractionCorrection{},
			&models.SourceIndexEntry{},
			&models.SourceAuditLog{},
			&models.SourceOAuthToken{},
			&models.VerificationRun{},
			&models.VerificationEvidence{},
			&models.VerificationClaim{},
			&models.VerificationAuditLog{},
			&models.WorkflowItem{},
			&models.WorkflowChecklistItem{},
			&models.WorkflowIntakeRecord{},
			&models.WorkflowProjectMatch{},
			&models.WorkflowEvidenceClaim{},
			&models.WorkflowOpenLoop{},
			&models.WorkflowProposal{},
			&models.WorkflowQualityGate{},
			&models.WorkflowRule{},
			&models.WorkflowTransition{},
			&models.WorkflowSourceLink{},
			&models.WorkflowDecision{},
			&models.WorkflowEvent{},
			&models.WorkflowCompletionAttestation{},
			&models.WorkflowReminderActivationRequest{},
			&models.WorkflowReminderActivationDecision{},
			&models.Pursuit{},
			&models.PursuitLink{},
			&models.PursuitActivity{},
			&models.PursuitTaskAttempt{},
			&models.PursuitPortfolioWorkflowSettlementProof{},
			&models.AmbientNeed{},
			&models.AmbientNeedOverride{},
			&models.AmbientOpportunity{},
			&models.AmbientScan{},
			&models.AutonomyWorldState{},
			&models.AutonomyActionTrace{},
			&models.AutonomyEvaluation{},
			&models.AutonomyStressRun{},
			// Phase 2 — Operation Ledger (§7/§10.5).
			&models.Operation{},
			&models.OperationEvent{},
			// Phase 2 — durable model telemetry (§18/§10.9).
			&models.ModelRunTelemetry{},
			&models.OptimizationProposalRun{},
			&models.TemporalWorkflowRun{},
			&models.BrowserVerificationRun{},
			&models.WASIRun{},
			// Durable worker: background jobs that survive a restart.
			&models.DurableJob{},
			// Owner-scoped Framework Registry preferences, immutable selection
			// audits, and versioned Robert Constitution records.
			&models.FrameworkPreference{},
			&models.FrameworkSelectionRecord{},
			&models.RobertConstitutionVersion{},
			// Owner-scoped, append-only task completion and approval state.
			&models.TaskOperationRecord{},
			&models.TaskCompletionPlanLog{},
			&models.TaskReviewItemRecord{},
			&models.TaskReviewDecisionRecord{},
			// Owner-scoped whole-life ontology, append-only need/capacity
			// observations, and durable goal hierarchy.
			&models.LifeEntityDomainLink{},
			&models.LifeNeedObservation{},
			&models.LifeCapacitySnapshot{},
			&models.LifeGoalNode{},
			&models.LifePriorityAssessment{},
			&models.StandingMandate{},
			&models.StandingMandateDecision{},
			&models.DomainPackPreference{},
		); err != nil {
			return err
		}
	}
	// Phase 3: versioned migrations that depend on the tables existing (indexes,
	// constraints, backfills). These replace the ad-hoc db.Exec DDL that used to
	// live here, so every schema change is now a reviewable, recorded migration.
	if _, err := ApplyMigrations(db, migrations.Files, "post"); err != nil {
		return fmt.Errorf("apply post migrations: %w", err)
	}
	return nil
}

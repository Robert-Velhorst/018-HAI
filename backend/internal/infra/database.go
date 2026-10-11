package infra

import (
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	defaultMaxOpenConnections = 16
	defaultMaxIdleConnections = 4
	defaultConnectionIdleTime = 5 * time.Minute
	defaultConnectionLifetime = 30 * time.Minute
	defaultSlowQueryThreshold = time.Second
)

type databaseIdentity struct {
	user     string
	password string
	name     string
	host     string
	port     int
}

type poolSettings struct {
	maxOpenConnections int
	maxIdleConnections int
	connectionIdleTime time.Duration
	connectionLifetime time.Duration
}

var defaultDatabaseState struct {
	sync.Mutex
	database *gorm.DB
	identity databaseIdentity
}

func NewPostgresDatabase(user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	settings, err := loadPoolSettings(
		defaultMaxOpenConnections,
		defaultMaxIdleConnections,
		defaultConnectionIdleTime,
		defaultConnectionLifetime,
	)
	if err != nil {
		return nil, err
	}
	loggerConfig, err := loadDatabaseLoggerConfig()
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC",
		dbHost, user, password, dbName, dbPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "", log.LstdFlags),
			loggerConfig,
		),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire postgres connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(settings.maxOpenConnections)
	sqlDB.SetMaxIdleConns(settings.maxIdleConnections)
	sqlDB.SetConnMaxIdleTime(settings.connectionIdleTime)
	sqlDB.SetConnMaxLifetime(settings.connectionLifetime)

	return db, nil
}

// loadDatabaseLoggerConfig keeps production database logs useful without
// serializing owner-scoped values, source text, or encrypted record payloads.
// Parameterization is unconditional even when an operator deliberately enables
// verbose diagnostics.
func loadDatabaseLoggerConfig() (logger.Config, error) {
	level := logger.Error
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DB_LOG_LEVEL"))) {
	case "", "error":
		level = logger.Error
	case "silent":
		level = logger.Silent
	case "warn":
		level = logger.Warn
	case "info":
		level = logger.Info
	default:
		return logger.Config{}, fmt.Errorf("DB_LOG_LEVEL must be one of silent, error, warn, or info")
	}

	return logger.Config{
		SlowThreshold:             defaultSlowQueryThreshold,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	}, nil
}

// OpenDefaultDB opens the configured database without running migrations. Use it
// for read-only operations (e.g. `migrate status`) or explicit rollbacks.
func OpenDefaultDB() (*gorm.DB, error) {
	return NewPostgresDatabase(config.AppConfig.DbUser, config.AppConfig.DbPassword,
		config.AppConfig.DbName, config.AppConfig.DbHost, config.AppConfig.DbPort)
}

func GetDefaultDB() (*gorm.DB, error) {
	identity := databaseIdentity{
		user:     config.AppConfig.DbUser,
		password: config.AppConfig.DbPassword,
		name:     config.AppConfig.DbName,
		host:     config.AppConfig.DbHost,
		port:     config.AppConfig.DbPort,
	}

	defaultDatabaseState.Lock()
	defer defaultDatabaseState.Unlock()
	if defaultDatabaseState.database != nil && defaultDatabaseState.identity == identity {
		return defaultDatabaseState.database, nil
	}

	db, err := NewPostgresDatabase(identity.user, identity.password, identity.name, identity.host, identity.port)
	if err != nil {
		return nil, err
	}

	if databaseMigrationsEnabled() {
		if err := RunMigrations(db); err != nil {
			closeDatabase(db)
			return nil, err
		}
	}

	if defaultDatabaseState.database != nil {
		closeDatabase(defaultDatabaseState.database)
	}
	defaultDatabaseState.database = db
	defaultDatabaseState.identity = identity

	return db, nil
}

// databaseMigrationsEnabled preserves the historical single-process behavior
// unless a deployment explicitly separates migration ownership from runtime
// access. Production Compose sets DB_RUN_MIGRATIONS=false on the long-lived
// backend and gates it on the one-shot migration service.
func databaseMigrationsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DB_RUN_MIGRATIONS"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func loadPoolSettings(defaultMaxOpen, defaultMaxIdle int, defaultIdleTime, defaultLifetime time.Duration) (poolSettings, error) {
	maxOpen, err := positiveEnvInt("DB_MAX_OPEN_CONNS", defaultMaxOpen)
	if err != nil {
		return poolSettings{}, err
	}
	maxIdle, err := nonNegativeEnvInt("DB_MAX_IDLE_CONNS", defaultMaxIdle)
	if err != nil {
		return poolSettings{}, err
	}
	if maxIdle > maxOpen {
		return poolSettings{}, fmt.Errorf("DB_MAX_IDLE_CONNS (%d) cannot exceed DB_MAX_OPEN_CONNS (%d)", maxIdle, maxOpen)
	}
	idleTime, err := nonNegativeEnvDuration("DB_CONN_MAX_IDLE_TIME", defaultIdleTime)
	if err != nil {
		return poolSettings{}, err
	}
	lifetime, err := nonNegativeEnvDuration("DB_CONN_MAX_LIFETIME", defaultLifetime)
	if err != nil {
		return poolSettings{}, err
	}
	return poolSettings{
		maxOpenConnections: maxOpen,
		maxIdleConnections: maxIdle,
		connectionIdleTime: idleTime,
		connectionLifetime: lifetime,
	}, nil
}

func positiveEnvInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func nonNegativeEnvInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func nonNegativeEnvDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative Go duration such as 5m or 30m", name)
	}
	return value, nil
}

func closeDatabase(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
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

func RunMigrations(db *gorm.DB) error {
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

	// Phase 2: optional dev-only table sync. The baseline migration already
	// created every table, so this is opt-in (DB_AUTOMIGRATE=true) and exists
	// only to materialise a newly-added model before its migration is generated.
	if autoMigrateEnabled() {
		if err := db.AutoMigrate(
			&models.Automation{},
			&models.AutomationHealthEvent{},
			&models.AutomationLaunchEvent{},
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
			&models.SourceIndexEntry{},
			&models.SourceAuditLog{},
			&models.SourceOAuthToken{},
			&models.SourceOAuthState{},
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

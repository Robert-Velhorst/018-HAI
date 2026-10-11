// Package phase2 wires the HAI Phase 2 autonomous back-office control plane
// (Operation Ledger + account feeds + execution broker + background loop) and
// exposes it over HTTP. It is the composition root for the Phase 2A vertical
// slice: real feeds in, real (local, verified) execution out, honest status.
package phase2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/frameworkevidence"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/sourceevidence"

	"github.com/google/uuid"
)

// Config configures the Phase 2 module. Zero values fall back to safe defaults.
type Config struct {
	OwnerUserID   string
	WorkspaceID   string
	WorkspaceDir  string   // confined root for the local safe worker
	FeedsDir      string   // directory holding local JSON feed files
	FeedFiles     []string // JSON feed filenames within FeedsDir
	StateDir      string   // persisted control state (emergency stop, mode)
	Mode          autonomypolicy.Mode
	EmergencyStop bool
}

// ConfigFromEnv reads the Phase 2 configuration from the environment.
func ConfigFromEnv() Config {
	return Config{
		OwnerUserID:  env("HAI_PHASE2_OWNER", "local-operator"),
		WorkspaceID:  env("HAI_PHASE2_WORKSPACE_ID", "local"),
		WorkspaceDir: env("HAI_PHASE2_WORKSPACE_DIR", filepath.Join("data", "phase2", "workspace")),
		FeedsDir:     env("HAI_PHASE2_FEEDS_DIR", filepath.Join("data", "phase2", "feeds")),
		// No feed is configured until the operator connects or explicitly imports
		// one. Assuming inbox.json made a fresh local install report a failed pass
		// before it had any data to read.
		FeedFiles:     splitList(env("HAI_PHASE2_FEED_FILES", "")),
		StateDir:      env("HAI_PHASE2_STATE_DIR", filepath.Join("data", "phase2", "state")),
		Mode:          autonomypolicy.Mode(env("HAI_PHASE2_MODE", string(autonomypolicy.ModeAutonomousSafe))),
		EmergencyStop: env("HAI_PHASE2_EMERGENCY_STOP", "") == "true",
	}
}

// Module is the composition of the Phase 2 services.
type Module struct {
	cfg         Config
	svc         *operations.Service
	broker      *executionbroker.Broker
	worker      *background.Worker
	readers     []accountfeed.Reader
	blockRules  *BlockRuleStore
	modelInt    *modelintelligence.Service
	evidence    EvidencePackRepository
	evidenceErr error
	control     background.Control
	runMu       sync.Mutex
	execAuth    *executionauth.Service
}

// NewModule wires a fail-closed module. Execution remains unavailable until a
// durable executionauth service is explicitly supplied.
func NewModule(svc *operations.Service, cfg Config) *Module {
	return newModule(svc, cfg, nil, nil, ErrEvidencePackRepositoryUnavailable)
}

// NewModuleWithExecutionAuthorization wires the production-safe local worker
// through durable executionauth receipts and final-boundary consumption.
func NewModuleWithExecutionAuthorization(
	svc *operations.Service,
	cfg Config,
	execAuth *executionauth.Service,
) *Module {
	return newModule(svc, cfg, execAuth, nil, ErrEvidencePackRepositoryUnavailable)
}

// NewModuleWithEvidencePackRepository supplies the durable evidence-pack
// boundary explicitly. It is intended for tests and composition roots that
// already own a database connection.
func NewModuleWithEvidencePackRepository(
	svc *operations.Service,
	cfg Config,
	execAuth *executionauth.Service,
	evidence EvidencePackRepository,
) *Module {
	if evidence == nil {
		return newModule(svc, cfg, execAuth, nil, ErrEvidencePackRepositoryUnavailable)
	}
	return newModule(svc, cfg, execAuth, evidence, nil)
}

func newModule(
	svc *operations.Service,
	cfg Config,
	execAuth *executionauth.Service,
	evidence EvidencePackRepository,
	evidenceErr error,
) *Module {
	cfg.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
	cfg.OwnerUserID = strings.TrimSpace(cfg.OwnerUserID)
	if cfg.WorkspaceID == "" {
		cfg.WorkspaceID = "local"
	}
	if cfg.OwnerUserID == "" {
		cfg.OwnerUserID = "local-operator"
	}
	if cfg.Mode == "" {
		cfg.Mode = autonomypolicy.ModeAutonomousSafe
	}
	broker := brokerForOwner(cfg, cfg.OwnerUserID, execAuth)
	readers := buildReaders(cfg)
	worker := background.New(svc, broker, readers, background.Options{
		OwnerUserID:   cfg.OwnerUserID,
		WorkspaceID:   cfg.WorkspaceID,
		Mode:          cfg.Mode,
		EmergencyStop: cfg.EmergencyStop,
	})
	blockRules := NewBlockRuleStore()
	worker.WithBlockRules(blockRules)
	return &Module{
		cfg:         cfg,
		svc:         svc,
		broker:      broker,
		worker:      worker,
		readers:     readers,
		blockRules:  blockRules,
		evidence:    evidence,
		evidenceErr: evidenceErr,
		execAuth:    execAuth,
	}
}

// DefaultModule builds the module from env over the default (DB-backed) service.
func DefaultModule() *Module {
	cfg := ConfigFromEnv()
	evidence, evidenceErr := DefaultEvidencePackRepository()
	return newModule(
		operations.DefaultService(),
		cfg,
		defaultExecutionAuthorizationService(cfg.OwnerUserID),
		evidence,
		evidenceErr,
	)
}

// DefaultModuleWithModelIntel builds the default module and attaches a shared
// model-intelligence service so the background loop drives the fast-triage lane
// (its telemetry then surfaces on the model-intelligence dashboard).
func DefaultModuleWithModelIntel(mi *modelintelligence.Service) *Module {
	cfg := ConfigFromEnv()
	evidence, evidenceErr := DefaultEvidencePackRepository()
	m := newModule(
		operations.DefaultService(),
		cfg,
		defaultExecutionAuthorizationService(cfg.OwnerUserID),
		evidence,
		evidenceErr,
	)
	m.worker.WithModelIntelligence(mi)
	m.modelInt = mi
	return m
}

// Service exposes the Operation Ledger service.
func (m *Module) Service() *operations.Service { return m.svc }

// Broker exposes the execution broker (e.g. for the Runtime Lab).
func (m *Module) Broker() *executionbroker.Broker { return m.broker }

// OwnerUserID returns the configured single operator id.
func (m *Module) OwnerUserID() string { return m.cfg.OwnerUserID }

// WorkspaceID returns the configured workspace id.
func (m *Module) WorkspaceID() string { return m.cfg.WorkspaceID }

// FeedsDir returns the configured feeds directory (allowlisted feed root).
func (m *Module) FeedsDir() string { return m.cfg.FeedsDir }

// FeedFiles returns the configured local feed filenames.
func (m *Module) FeedFiles() []string { return m.cfg.FeedFiles }

// Worker exposes the background worker.
func (m *Module) Worker() *background.Worker { return m.worker }

func (m *Module) evidencePackRepository() (EvidencePackRepository, error) {
	if m == nil || m.evidence == nil {
		if m != nil && m.evidenceErr != nil {
			return nil, m.evidenceErr
		}
		return nil, ErrEvidencePackRepositoryUnavailable
	}
	if m.evidenceErr != nil {
		return nil, m.evidenceErr
	}
	return m.evidence, nil
}

// OpsControl builds the always-on runtime control service, wires its controller
// into the background worker (so pause/resume/mode take effect live), and
// registers the background runner used by emergency-stop verification.
func (m *Module) OpsControl() *opscontrol.Service {
	svc := opscontrol.NewService(m.cfg.StateDir, m.broker, m.svc, m.cfg.OwnerUserID, m.cfg.WorkspaceID)
	m.control = svc.Control()
	m.worker.WithControl(m.control)
	svc.SetBackgroundRunner(func(ctx context.Context) (int, error) {
		rep, err := m.RunConfiguredBackground(ctx)
		if err != nil {
			return 0, err
		}
		return rep.Classified + rep.AutoExecuted, nil
	})
	return svc
}

// RunBackgroundForOwner runs one HTTP-requested background pass scoped to the
// authenticated caller. It intentionally does not fall back to the configured
// system owner: every feed and ledger query is rebound to ownerIdentity.
func (m *Module) RunBackgroundForOwner(ctx context.Context, ownerIdentity string) (background.Report, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return background.Report{}, fmt.Errorf("phase2: authenticated owner identity required")
	}

	m.runMu.Lock()
	defer m.runMu.Unlock()

	worker := m.newOwnerWorker(ownerIdentity)
	return worker.RunOnce(ctx)
}

// RunConfiguredBackground is the distinct internal scheduler path. It is not
// called by the HTTP handler and always uses the explicitly configured owner.
func (m *Module) RunConfiguredBackground(ctx context.Context) (background.Report, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	return m.worker.RunOnce(ctx)
}

func (m *Module) newOwnerWorker(ownerIdentity string) *background.Worker {
	readers := make([]accountfeed.Reader, 0, len(m.readers))
	for _, reader := range m.readers {
		readers = append(readers, ownerScopedReader{Reader: reader, ownerIdentity: ownerIdentity})
	}
	broker := brokerForOwner(m.cfg, ownerIdentity, m.execAuth)
	worker := background.New(m.svc, broker, readers, background.Options{
		OwnerUserID:   ownerIdentity,
		WorkspaceID:   m.cfg.WorkspaceID,
		Mode:          m.cfg.Mode,
		EmergencyStop: m.cfg.EmergencyStop,
	})
	worker.WithBlockRules(m.blockRules)
	if m.modelInt != nil {
		worker.WithModelIntelligence(m.modelInt)
	}
	if m.control != nil {
		worker.WithControl(m.control)
	}
	return worker
}

func brokerForOwner(
	cfg Config,
	ownerIdentity string,
	execAuth *executionauth.Service,
) *executionbroker.Broker {
	if execAuth == nil {
		return executionbroker.NewBroker(cfg.WorkspaceDir)
	}
	broker, err := executionbroker.NewAuthorizedBroker(
		cfg.WorkspaceDir,
		ownerIdentity,
		cfg.WorkspaceID,
		execAuth,
	)
	if err != nil {
		return executionbroker.NewBroker(cfg.WorkspaceDir)
	}
	return broker
}

func defaultExecutionAuthorizationService(ownerIdentity string) *executionauth.Service {
	frameworks, err := frameworkregistry.DefaultService()
	if err != nil {
		return nil
	}
	active, _, err := frameworks.ActiveConstitution(ownerIdentity)
	if err != nil || !hasExecutionConstitution(active) {
		return nil
	}
	constitution, err := executionauth.NewConstitutionPolicyAdapter(frameworks)
	if err != nil {
		return nil
	}
	repository, err := executionauth.DefaultRepository()
	if err != nil {
		return nil
	}
	service, err := executionauth.NewService(
		repository,
		constitution,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		return nil
	}
	selectionResolver, err := executionauth.NewFrameworkSelectionResolver(frameworks)
	if err != nil {
		return nil
	}
	service, err = service.WithFrameworkSelectionResolver(selectionResolver)
	if err != nil {
		return nil
	}
	preflightRepository, err := frameworkevidence.DefaultRepository()
	if err != nil {
		return nil
	}
	preflightResolver, err := executionauth.NewFrameworkEvidencePreflightResolver(
		preflightRepository,
	)
	if err != nil {
		return nil
	}
	service, err = service.WithFrameworkEvidencePreflightResolver(preflightResolver)
	if err != nil {
		return nil
	}
	sourceEvidenceRepository, err := sourceevidence.DefaultRepository()
	if err != nil {
		return nil
	}
	service, err = service.WithSourceEvidenceRepository(sourceEvidenceRepository)
	if err != nil {
		return nil
	}
	return service
}

func hasExecutionConstitution(value frameworkregistry.Constitution) bool {
	if value.Status != frameworkregistry.ConstitutionActive {
		return false
	}
	// The immutable built-in baseline is the safe first-run Constitution. It
	// explicitly permits only allowlisted, reversible, low-risk local checks;
	// accepting it here keeps a fresh install operational without allowing an
	// arbitrary non-persisted Constitution to authorize execution.
	if strings.TrimSpace(value.ID) == frameworkregistry.DefaultConstitution().ID {
		return true
	}
	id, err := uuid.Parse(strings.TrimSpace(value.ID))
	return err == nil && id != uuid.Nil
}

type ownerScopedReader struct {
	accountfeed.Reader
	ownerIdentity string
}

func (r ownerScopedReader) Feed() accountfeed.Feed {
	feed := r.Reader.Feed()
	feed.OwnerUserID = r.ownerIdentity
	// The operation repository's dedupe lookup is workspace-scoped. Namespace
	// the account label with a non-reversible owner digest so identical source
	// items cannot refresh another owner's operation.
	sum := sha256.Sum256([]byte(r.ownerIdentity))
	feed.AccountLabel += "#owner-" + hex.EncodeToString(sum[:8])
	return feed
}

// Handler builds the HTTP handler for this module.
func (m *Module) Handler() *Handler { return NewHandler(m) }

// buildReaders constructs a local-file reader for each configured feed file.
func buildReaders(cfg Config) []accountfeed.Reader {
	var readers []accountfeed.Reader
	for _, name := range cfg.FeedFiles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		feed := accountfeed.Feed{
			ID:           uuid.New(),
			Name:         strings.TrimSuffix(name, filepath.Ext(name)),
			Provider:     "local",
			AccountLabel: name,
			SourceType:   accountfeed.SourceLocalJSONFile,
			Path:         name,
			WorkspaceID:  cfg.WorkspaceID,
			OwnerUserID:  cfg.OwnerUserID,
			Enabled:      true,
		}
		r, err := accountfeed.NewLocalFileReader(feed, cfg.FeedsDir)
		if err != nil {
			continue // an invalid feed name is skipped, not fatal
		}
		readers = append(readers, r)
	}
	return readers
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

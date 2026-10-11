package phase2

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/operations"
)

func TestConfigFromEnvDoesNotInventAFeed(t *testing.T) {
	t.Setenv("HAI_PHASE2_FEED_FILES", "")
	cfg := ConfigFromEnv()
	if len(cfg.FeedFiles) != 0 {
		t.Fatalf("FeedFiles = %#v, want no implicit feed", cfg.FeedFiles)
	}
}

func TestRunBackgroundForOwnerRejectsBlankOwnerWithoutEffects(t *testing.T) {
	m := newTestModule(t)
	if _, err := m.RunBackgroundForOwner(t.Context(), " \t "); err == nil {
		t.Fatal("blank owner must be rejected")
	}
	ops, err := m.Service().List(operations.Filter{WorkspaceID: "local", Limit: 50})
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("rejected run produced %d operations", len(ops))
	}
}

func TestModuleWithoutExecutionAuthorizationFailsClosed(t *testing.T) {
	cfg := Config{
		OwnerUserID:  "robert",
		WorkspaceID:  "local",
		WorkspaceDir: filepath.Join(t.TempDir(), "not-created"),
		Mode:         autonomypolicy.ModeAutonomousSafe,
	}
	module := NewModule(
		operations.NewService(operations.NewMemoryRepository()),
		cfg,
	)
	if module.Broker().SafeWorker().HealthCheck(t.Context()).Status.CanExecute() {
		t.Fatal("module without executionauth must not expose an executable worker")
	}
	_, err := module.Broker().ExecuteLocalSafeWorker(
		t.Context(),
		executionbroker.SafeWorkerInput{
			ArtifactName: "artifact.txt",
			Marker:       "marker",
		},
	)
	if !errors.Is(err, executionbroker.ErrAuthorizationRequired) {
		t.Fatalf("execution error = %v, want authorization required", err)
	}
}

func TestModuleWithExecutionAuthorizationBuildsReadyBroker(t *testing.T) {
	cfg := Config{
		OwnerUserID:  "robert",
		WorkspaceID:  "local",
		WorkspaceDir: t.TempDir(),
		Mode:         autonomypolicy.ModeAutonomousSafe,
	}
	module := NewModuleWithExecutionAuthorization(
		operations.NewService(operations.NewMemoryRepository()),
		cfg,
		newTestExecutionAuthorizationService(t),
	)
	health := module.Broker().SafeWorker().HealthCheck(t.Context())
	if health.Status != executionbroker.RuntimeReady || !health.Status.CanExecute() {
		t.Fatalf("authorized broker health = %+v", health)
	}
}

func TestExecutionConstitutionAvailabilityAcceptsOnlyTrustedBaselineOrDurableActive(t *testing.T) {
	if !hasExecutionConstitution(frameworkregistry.DefaultConstitution()) {
		t.Fatal("trusted built-in baseline must enable bounded low-risk local execution")
	}
	if hasExecutionConstitution(frameworkregistry.Constitution{
		ID:     "untrusted-runtime-constitution",
		Status: frameworkregistry.ConstitutionActive,
	}) {
		t.Fatal("arbitrary non-persisted Constitution must not enable execution")
	}
	if !hasExecutionConstitution(frameworkregistry.Constitution{
		ID:     "8c508c67-691f-4d35-9487-fcd173f755d4",
		Status: frameworkregistry.ConstitutionActive,
	}) {
		t.Fatal("persistable active Constitution should enable composition")
	}
	if hasExecutionConstitution(frameworkregistry.Constitution{
		ID:     "8c508c67-691f-4d35-9487-fcd173f755d4",
		Status: frameworkregistry.ConstitutionDraft,
	}) {
		t.Fatal("draft Constitution must not enable execution")
	}
}

func newTestExecutionAuthorizationService(t *testing.T) *executionauth.Service {
	t.Helper()
	service, err := executionauth.NewService(
		executionauth.NewMemoryRepository(),
		phase2TestConstitution{},
		nil,
		nil,
		nil,
		func() time.Time {
			return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("new execution authorization service: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{Source: "phase2-test"}
	})
	return service
}

type phase2TestConstitution struct{}

func (phase2TestConstitution) EvaluateExecutionPolicy(
	_ string,
	_ []string,
	_ int,
) (executionauth.ConstitutionDecision, error) {
	return executionauth.ConstitutionDecision{
		ID:               "ee17faeb-3c0a-497a-9129-f49115872a2e",
		Version:          1,
		Source:           "test",
		Digest:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AuthorityCeiling: 10,
	}, nil
}

var _ executionauth.ConstitutionEvaluator = phase2TestConstitution{}

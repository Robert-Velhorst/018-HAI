package runtimelab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	broker := newAuthorizedRuntimeLabTestBroker(
		t,
		t.TempDir(),
		"local-operator",
		"local",
	)
	ops := operations.NewService(operations.NewMemoryRepository())
	return NewService(broker, ops, "local-operator", "local").WithSafeExecutionPolicy(allowRuntimeLabSafeExecution)
}

func allowRuntimeLabSafeExecution(string, string, string) bool { return true }

func newAuthorizedRuntimeLabTestBroker(
	t *testing.T,
	workspace string,
	owner string,
	workspaceID string,
) *executionbroker.Broker {
	t.Helper()
	frameworks, err := frameworkregistry.NewService(
		frameworkregistry.NewMemoryRepository(),
	)
	if err != nil {
		t.Fatalf("new framework registry: %v", err)
	}
	draft, err := frameworks.CreateConstitutionDraft(
		owner,
		frameworkregistry.ConstitutionDraftRequest{
			BaseVersion:   1,
			ChangeSummary: "Activate production-like local execution test policy.",
		},
	)
	if err != nil {
		t.Fatalf("create Constitution draft: %v", err)
	}
	active, err := frameworks.ActivateConstitution(
		owner,
		draft.ID,
		owner,
		frameworkregistry.ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Owner reviewed and approved this test policy.",
		},
	)
	if err != nil {
		t.Fatalf("activate Constitution: %v", err)
	}
	if active.Status != frameworkregistry.ConstitutionActive {
		t.Fatalf("Constitution status = %q, want active", active.Status)
	}
	constitution, err := executionauth.NewConstitutionPolicyAdapter(frameworks)
	if err != nil {
		t.Fatalf("adapt Constitution policy: %v", err)
	}
	authorization, err := executionauth.NewService(
		executionauth.NewMemoryRepository(),
		constitution,
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
	authorization.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{
			Source: "runtimelab-test",
		}
	})
	broker, err := executionbroker.NewAuthorizedBroker(
		workspace,
		owner,
		workspaceID,
		authorization,
	)
	if err != nil {
		t.Fatalf("new authorized broker: %v", err)
	}
	return broker
}

func TestOverviewTruthfulRuntimeStates(t *testing.T) {
	s := newTestService(t)
	byID := map[string]RuntimeSummary{}
	for _, r := range s.Overview(context.Background()) {
		byID[r.Info.ID] = r
	}
	// The safe worker is the only executable runtime.
	if !byID[executionbroker.LocalSafeWorkerID].CanExecute {
		t.Fatalf("local safe worker must be executable")
	}
	// External runtimes are not executable and carry setup requirements.
	for _, id := range []string{"hermes", "openclaw", "odysseus", "openhands"} {
		r := byID[id]
		if r.CanExecute {
			t.Fatalf("%s must not be executable without configuration", id)
		}
		if len(r.SetupRequirements) == 0 {
			t.Fatalf("%s must expose exact setup requirements", id)
		}
	}
	// Contracts are not executors.
	if byID["browser-runtime"].CanExecute || byID["local-script-runtime"].CanExecute {
		t.Fatalf("contract runtimes must not be executable")
	}
}

func TestFeatureParityAccountsForEveryRequiredAreaPerRuntime(t *testing.T) {
	s := newTestService(t)
	s.now = func() time.Time {
		return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	}
	overview, err := s.FeatureParity()
	if err != nil {
		t.Fatalf("feature parity: %v", err)
	}
	if len(overview.Inventories) != 3 {
		t.Fatalf("inventories = %d, want 3", len(overview.Inventories))
	}
	if len(overview.RequiredCoverageAreas) != len(requiredRuntimeCoverageAreas) {
		t.Fatalf("required coverage = %d, want %d", len(overview.RequiredCoverageAreas), len(requiredRuntimeCoverageAreas))
	}
	if overview.GeneratedAt != s.now().UTC() {
		t.Fatalf("generatedAt = %s, want %s", overview.GeneratedAt, s.now().UTC())
	}
	for _, inventory := range overview.Inventories {
		if inventory.ReadinessCeiling != "declared" {
			t.Fatalf("%s readiness ceiling = %q, want declared", inventory.RuntimeID, inventory.ReadinessCeiling)
		}
		if len(inventory.Features) < 13 {
			t.Fatalf("%s feature groups = %d, want at least 13", inventory.RuntimeID, len(inventory.Features))
		}
		if err := validateRuntimeInventory(inventory); err != nil {
			t.Fatalf("validate %s: %v", inventory.RuntimeID, err)
		}
		for _, item := range inventory.Features {
			if item.Disposition == DispositionDeferred || item.Disposition == DispositionBlockedExternal {
				if item.BacklogPriority == "" || item.RecommendedPath == "" || len(item.Requirements) == 0 {
					t.Fatalf("%s/%s lacks actionable backlog: %#v", inventory.RuntimeID, item.ID, item)
				}
			}
		}
	}
	if overview.DispositionCounts[string(DispositionIntegratedDirectly)] != 0 {
		t.Fatal("source review must not claim a direct integration")
	}
}

func TestOpenClawResilienceCatalogReflectsPersistedReceiptAndReconciliationContracts(t *testing.T) {
	inventory, ok, err := newTestService(t).RuntimeFeatureParity("openclaw")
	if err != nil || !ok {
		t.Fatalf("openclaw inventory = (%t, %v)", ok, err)
	}
	byID := map[string]RuntimeFeature{}
	for _, item := range inventory.Features {
		byID[item.ID] = item
	}
	resilience := byID["openclaw-resilience"]
	if resilience.ImplementationStatus != "partial" {
		t.Fatalf("resilience status = %q, want partial pending live companion acceptance", resilience.ImplementationStatus)
	}
	if resilience.TestStatus != "delegated_session_idempotency_terminal_stale_and_metadata_artifact_reconciliation_contract_tested" {
		t.Fatalf("resilience test status = %q", resilience.TestStatus)
	}
	if !strings.Contains(resilience.RecommendedPath, "persisted") || !strings.Contains(resilience.RecommendedPath, "indeterminate") {
		t.Fatalf("resilience recommendation does not describe current recovery boundary: %q", resilience.RecommendedPath)
	}
	for _, staleRequirement := range []string{"future Gateway adapter", "Add request/response idempotency"} {
		if strings.Contains(resilience.RecommendedPath, staleRequirement) {
			t.Fatalf("resilience recommendation is stale: %q", resilience.RecommendedPath)
		}
	}
}

func TestOdysseusParityFailsClosedOnLicenseAndUnsafeAdminTools(t *testing.T) {
	s := newTestService(t)
	inventory, ok, err := s.RuntimeFeatureParity(" ODYSSEUS ")
	if err != nil || !ok {
		t.Fatalf("odysseus inventory = (%t, %v)", ok, err)
	}
	if inventory.License != "AGPL-3.0-or-later" {
		t.Fatalf("license = %q", inventory.License)
	}
	byID := map[string]RuntimeFeature{}
	for _, item := range inventory.Features {
		byID[item.ID] = item
	}
	if byID["odysseus-capabilities"].Disposition != DispositionExcludedIncompatibleLicense {
		t.Fatalf("capability import disposition = %q", byID["odysseus-capabilities"].Disposition)
	}
	if byID["odysseus-host-tools"].Disposition != DispositionConstrainedUnsafe {
		t.Fatalf("host tool disposition = %q", byID["odysseus-host-tools"].Disposition)
	}
	if byID["odysseus-security"].ExclusionReason == "" {
		t.Fatal("unsafe admin boundary requires an explicit reason")
	}
}

func TestRuntimeFeatureParityUnknownRuntime(t *testing.T) {
	_, ok, err := newTestService(t).RuntimeFeatureParity("missing")
	if err != nil || ok {
		t.Fatalf("unknown inventory = (%t, %v), want false nil", ok, err)
	}
}

func TestCapabilityCardsAreCompleteAndNeverGrantAuthority(t *testing.T) {
	for _, key := range []string{"OPENCLAW_BASE_URL", "HERMES_BASE_URL", "ODYSSEUS_BASE_URL"} {
		t.Setenv(key, "")
	}
	overview, err := newTestService(t).CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("capability cards: %v", err)
	}
	if overview.Authority != "contract_only" || len(overview.Cards) != 11 {
		t.Fatalf("capability overview = %#v", overview)
	}
	seen := map[string]bool{}
	openClawCards := map[string]RuntimeCapabilityCard{}
	for _, card := range overview.Cards {
		if seen[card.ID] {
			t.Fatalf("duplicate card %q", card.ID)
		}
		seen[card.ID] = true
		if card.ReadinessLevel != ReadinessDeclared || card.CanInvoke || card.CanExecuteExternalEffect {
			t.Fatalf("card widened runtime authority: %#v", card)
		}
		if card.ExpectedCostEURMax != 0 || card.TimeoutSeconds < 1 || len(card.RequiredAuthority) == 0 ||
			len(card.ApprovalRequirements) == 0 || len(card.EvidenceReturned) == 0 ||
			card.InputSchema["type"] != "object" || card.OutputSchema["type"] != "object" {
			t.Fatalf("incomplete capability card: %#v", card)
		}
		if strings.HasSuffix(card.ID, ".discovery") {
			hasWritePermission := false
			for _, authority := range card.RequiredAuthority {
				hasWritePermission = hasWritePermission || authority == "write"
			}
			if !hasWritePermission {
				t.Errorf("discovery card must disclose the HAI write permission required by POST /probe: %#v", card)
			}
		}
		if strings.HasSuffix(card.ID, ".delegate") {
			if card.RiskLevel != "high" || card.CanInvoke || card.CanExecuteExternalEffect {
				t.Errorf("delegation must remain high-risk and non-invocable in Runtime Lab: %#v", card)
			}
			for _, required := range []string{"exact_execution_authorization_receipt"} {
				found := false
				for _, authority := range card.RequiredAuthority {
					found = found || authority == required
				}
				if !found {
					t.Errorf("delegation authority is missing %q: %#v", required, card.RequiredAuthority)
				}
			}
			for _, required := range []string{"Current policy approval", "exact effect authorization", "fresh source and plan digests"} {
				found := false
				for _, approval := range card.ApprovalRequirements {
					found = found || approval == required
				}
				if !found {
					t.Errorf("delegation approval policy is missing %q: %#v", required, card.ApprovalRequirements)
				}
			}
		}
		if card.RuntimeID == "openclaw" {
			openClawCards[card.ID] = card
		}
	}
	if _, found := openClawCards["openclaw.agents.discovery"]; !found {
		t.Fatal("OpenClaw agent-roster discovery card must be present")
	}
	capabilities, found := openClawCards["openclaw.capabilities.discovery"]
	if !found || capabilities.OutputSchema["properties"].(map[string]any)["sampledCommands"] == nil {
		t.Fatalf("OpenClaw capability card must describe its retained command count: %#v", capabilities)
	}
	if _, found := openClawCards["openclaw.models.discovery"]; !found {
		t.Fatalf("OpenClaw prepared-model discovery card is missing: %#v", openClawCards)
	}
	tasks, found := openClawCards["openclaw.tasks.discovery"]
	if !found || tasks.OutputSchema["properties"].(map[string]any)["sampledTasks"] == nil {
		t.Fatalf("OpenClaw task-ledger discovery card is missing or incomplete: %#v", tasks)
	}
}

func TestConfiguredCapabilityRemainsNonInvocable(t *testing.T) {
	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("HERMES_BASE_URL", "http://127.0.0.1:8999")
	overview, err := newTestService(t).CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("capability cards: %v", err)
	}
	for _, card := range overview.Cards {
		if card.RuntimeID != "hermes" {
			continue
		}
		if card.ReadinessLevel != ReadinessConfigured || card.AuthenticationState != "operator_configured_unverified" {
			t.Fatalf("configured Hermes card = %#v", card)
		}
		if card.CanInvoke || card.CanExecuteExternalEffect {
			t.Fatalf("configured Hermes card granted authority: %#v", card)
		}
	}
}

func TestSafeWorkerSelfTestThroughLedger(t *testing.T) {
	s := newTestService(t)
	attempt, ok := s.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
	if !ok {
		t.Fatalf("safe worker self-test must be found")
	}
	if attempt.Status != AttemptSucceeded || !attempt.VerificationPassed {
		t.Fatalf("safe worker self-test must succeed and verify, got %s verified=%v (%s)", attempt.Status, attempt.VerificationPassed, attempt.Detail)
	}
	if attempt.OperationID == "" {
		t.Fatalf("self-test must run through the Operation Ledger (operationId set)")
	}
	// A completed operation must exist in the ledger.
	completed, _ := s.ops.List(operations.Filter{OwnerUserID: "local-operator", WorkspaceID: "local", Status: operations.StatusCompleted})
	if len(completed) != 1 {
		t.Fatalf("self-test must produce one completed ledger operation, got %d", len(completed))
	}
}

func TestSafeWorkerSelfTestFailsClosedWithoutLivePolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy func(string, string, string) bool
	}{
		{name: "policy unavailable"},
		{name: "policy denies execution", policy: func(string, string, string) bool { return false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			ops := operations.NewService(operations.NewMemoryRepository())
			service := NewService(
				newAuthorizedRuntimeLabTestBroker(t, workspace, "local-operator", "local"),
				ops,
				"local-operator",
				"local",
			)
			if test.policy != nil {
				service.WithSafeExecutionPolicy(test.policy)
			}

			attempt, ok := service.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
			if !ok || attempt.Status != AttemptBlocked || attempt.VerificationPassed {
				t.Fatalf("self-test without an allowing live policy = (%#v, %t), want blocked and unverified", attempt, ok)
			}
			entries, err := os.ReadDir(workspace)
			if err != nil {
				t.Fatalf("read workspace: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("blocked self-test created %d filesystem artifacts", len(entries))
			}

			operationID, err := uuid.Parse(attempt.OperationID)
			if err != nil {
				t.Fatalf("self-test operation id %q: %v", attempt.OperationID, err)
			}
			claim, err := ops.ClaimOperation(context.Background(), "local-operator", "local", operationID, uuid.New(), time.Minute)
			if err != nil {
				t.Fatalf("blocked policy retained the operation claim: %v", err)
			}
			if err := ops.ReleaseClaim(context.Background(), claim.Claim); err != nil {
				t.Fatalf("release check claim: %v", err)
			}
		})
	}
}

type denyingTargetClaimRepository struct {
	*operations.MemoryRepository
}

func (r *denyingTargetClaimRepository) ClaimOperation(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (*operations.ClaimedOperation, error) {
	return nil, operations.ErrOperationClaimed
}

func TestSafeWorkerSelfTestClaimFailureHasNoEffect(t *testing.T) {
	workspace := t.TempDir()
	repository := &denyingTargetClaimRepository{MemoryRepository: operations.NewMemoryRepository()}
	ops := operations.NewService(repository)
	s := NewService(
		newAuthorizedRuntimeLabTestBroker(t, workspace, "local-operator", "local"),
		ops,
		"local-operator",
		"local",
	)

	attempt, ok := s.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
	if !ok || attempt.Status != AttemptFailed {
		t.Fatalf("self-test with denied claim = (%#v, %t), want failed", attempt, ok)
	}
	if !strings.Contains(attempt.Detail, operations.ErrOperationClaimed.Error()) {
		t.Fatalf("failed attempt detail = %q, want claim rejection", attempt.Detail)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("read safe-worker workspace: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("claim failure produced %d filesystem artifacts", len(entries))
	}
	classified, err := s.ops.List(operations.Filter{
		OwnerUserID: "local-operator",
		WorkspaceID: "local",
		Status:      operations.StatusClassified,
	})
	if err != nil {
		t.Fatalf("list classified self-test operation: %v", err)
	}
	if len(classified) != 1 || classified[0].AutonomyLevel != string(operations.AutonomyAuto) {
		t.Fatalf("operation after claim rejection = %#v, want one unchanged auto-classified item", classified)
	}
}

func TestUnauthorizedSafeWorkerIsBlockedAndSelfTestFailsClosed(t *testing.T) {
	workspace := t.TempDir()
	broker := executionbroker.NewBroker(workspace)
	ops := operations.NewService(operations.NewMemoryRepository())
	s := NewService(broker, ops, "local-operator", "local").WithSafeExecutionPolicy(allowRuntimeLabSafeExecution)

	byID := map[string]RuntimeSummary{}
	for _, runtime := range s.Overview(context.Background()) {
		byID[runtime.Info.ID] = runtime
	}
	safeWorker := byID[executionbroker.LocalSafeWorkerID]
	if safeWorker.CanExecute ||
		safeWorker.Status != executionbroker.RuntimeBlocked {
		t.Fatalf("unauthorized safe worker summary = %#v", safeWorker)
	}

	attempt, ok := s.SelfTest(
		context.Background(),
		executionbroker.LocalSafeWorkerID,
	)
	if !ok {
		t.Fatal("safe worker self-test must remain discoverable")
	}
	if attempt.Status != AttemptFailed || attempt.VerificationPassed {
		t.Fatalf("unauthorized self-test = %#v, want failed and unverified", attempt)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unauthorized self-test created %d artifacts", len(entries))
	}
	completed, err := ops.List(operations.Filter{
		OwnerUserID: "local-operator",
		WorkspaceID: "local",
		Status:      operations.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("list completed operations: %v", err)
	}
	if len(completed) != 0 {
		t.Fatalf("unauthorized self-test completed %d operations", len(completed))
	}
}

func TestExternalRuntimeSelfTestNeverFakes(t *testing.T) {
	s := newTestService(t)
	for _, id := range []string{"hermes", "openclaw", "odysseus", "openhands"} {
		attempt, ok := s.SelfTest(context.Background(), id)
		if !ok {
			t.Fatalf("%s self-test must be found", id)
		}
		if attempt.Status == AttemptSucceeded {
			t.Fatalf("%s must never report a successful self-test without a real runtime", id)
		}
		if attempt.Status != AttemptSetupRequired {
			t.Fatalf("%s self-test must be setup_required when unconfigured, got %s", id, attempt.Status)
		}
	}
}

func TestExternalRuntimeExecuteRefuses(t *testing.T) {
	s := newTestService(t)
	a, _ := s.reg.Adapter("hermes")
	if _, err := a.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatalf("hermes Execute must refuse (no fake execution)")
	}
	// Browser contract refuses too, and its forbidden boundary is published.
	b, _ := s.reg.Adapter("browser-runtime")
	if _, err := b.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatalf("browser contract Execute must refuse")
	}
	foundForbidden := false
	for _, cap := range b.Capabilities() {
		if cap == "forbidden:send_message" {
			foundForbidden = true
		}
	}
	if !foundForbidden {
		t.Fatalf("browser contract must publish its forbidden boundary")
	}
}

func TestAttemptsRecorded(t *testing.T) {
	s := newTestService(t)
	s.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
	s.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
	if len(s.Attempts(executionbroker.LocalSafeWorkerID)) != 2 {
		t.Fatalf("both self-test attempts must be recorded")
	}
}

func TestSafeWorkerAttemptsRecoverFromOperationLedgerAfterRestart(t *testing.T) {
	repository := operations.NewMemoryRepository()
	firstOperations := operations.NewService(repository)
	first := NewService(
		newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local"),
		firstOperations,
		"local-operator",
		"local",
	).WithSafeExecutionPolicy(allowRuntimeLabSafeExecution)
	attempt, ok := first.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
	if !ok || attempt.Status != AttemptSucceeded {
		t.Fatalf("initial self-test = (%#v, %t), want succeeded", attempt, ok)
	}

	restarted := NewService(
		newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local"),
		operations.NewService(repository),
		"local-operator",
		"local",
	).WithSafeExecutionPolicy(allowRuntimeLabSafeExecution)
	recovered := restarted.Attempts(executionbroker.LocalSafeWorkerID)
	if len(recovered) != 1 {
		t.Fatalf("recovered attempts = %d, want 1", len(recovered))
	}
	if recovered[0].OperationID != attempt.OperationID ||
		recovered[0].Status != AttemptSucceeded ||
		!recovered[0].VerificationPassed {
		t.Fatalf("recovered attempt = %#v, want verified original operation", recovered[0])
	}

	var safeWorker RuntimeSummary
	for _, runtime := range restarted.Overview(context.Background()) {
		if runtime.Info.ID == executionbroker.LocalSafeWorkerID {
			safeWorker = runtime
			break
		}
	}
	if safeWorker.LastAttempt == nil || safeWorker.LastAttempt.OperationID != attempt.OperationID {
		t.Fatalf("overview last attempt = %#v, want recovered operation %s", safeWorker.LastAttempt, attempt.OperationID)
	}
}

func TestValidateURLRejectsMetadata(t *testing.T) {
	t.Setenv(runtimeLabAllowedHostsEnv, defaultRuntimeLabAllowedHosts)
	for _, u := range []string{"http://169.254.169.254/", "ftp://x", "http://0.0.0.0/"} {
		if err := validateURL(u); err == nil {
			t.Fatalf("%q must be rejected", u)
		}
	}
	if err := validateURL("http://localhost:9000"); err != nil {
		t.Fatalf("localhost must be allowed: %v", err)
	}
}

func TestValidateURLRequiresExplicitRuntimeLabHost(t *testing.T) {
	t.Setenv(runtimeLabAllowedHostsEnv, "localhost,agent.local")
	if err := validateURL("https://agent.local/runtime"); err != nil {
		t.Fatalf("explicitly allowed host must be accepted: %v", err)
	}
	for _, raw := range []string{
		"https://unreviewed.example/runtime",
		"https://agent.local/runtime?token=secret",
		"https://user:secret@agent.local/runtime",
	} {
		if err := validateURL(raw); err == nil {
			t.Fatalf("%q must be rejected by Runtime Lab URL validation", raw)
		}
	}
}

func TestOpenHandsHealthProbeIsRealButExecutionRemainsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("OPENHANDS_BASE_URL", server.URL)
	t.Setenv("OPENHANDS_HEALTH_PATH", "/health")
	runtime := newRemoteRuntime("openhands", "OpenHands", "OPENHANDS_BASE_URL")

	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeBlocked || probe.DiscoveryState != "reachable_unverified" || probe.ProtocolValid {
		t.Fatalf("an unregistered OpenHands response must remain reachable but unverified: %#v", probe)
	}
	if _, err := runtime.Execute(context.Background(), map[string]any{"task": "do not run"}); err == nil {
		t.Fatal("a healthy OpenHands endpoint must not enable task execution")
	}
}

func TestOpenClawDiscoveryValidatesExactHealthContractWithoutGrantingExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"live"}`))
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("OPENCLAW_BASE_URL", server.URL)
	runtime := newRemoteRuntime("openclaw", "OpenClaw", "OPENCLAW_BASE_URL")
	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeBlocked || probe.ReadinessLevel != ReadinessAvailable ||
		!probe.ProtocolValid || probe.IdentityVerified || probe.EvidenceSHA256 == "" {
		t.Fatalf("OpenClaw discovery must validate only the reviewed read-only contract: %#v", probe)
	}
	if runtime.HealthCheck(context.Background()).Claim != executionbroker.ClaimProbed {
		t.Fatal("schema-valid discovery should raise only the probe claim")
	}
	if _, err := runtime.Execute(context.Background(), map[string]any{"task": "do not run"}); err == nil {
		t.Fatal("OpenClaw discovery must not enable execution")
	}
}

func TestOpenClawRuntimeLabUsesCanonicalAgentRuntimeRegistry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"live"}`))
	}))
	defer server.Close()

	t.Setenv("OPENCLAW_BASE_URL", "") // The legacy Runtime Lab endpoint must not be consulted.
	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_URL", server.URL)
	t.Setenv("OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED", "false")
	t.Setenv("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED", "false")
	t.Setenv("OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED", "false")
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")

	broker := newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local")
	ops := operations.NewService(operations.NewMemoryRepository())
	service := NewServiceWithAgentRuntimeRegistry(
		broker,
		ops,
		"local-operator",
		"local",
		agentruntime.DefaultRegistry(),
	)
	probe, ok := service.Probe(context.Background(), "openclaw")
	if !ok || probe.Status != executionbroker.RuntimeBlocked ||
		probe.ReadinessLevel != ReadinessAvailable || probe.ProtocolValid ||
		probe.RuntimeVersion != "" || probe.Authenticated || probe.IdentityVerified {
		t.Fatalf("canonical OpenClaw probe = (%#v, %t)", probe, ok)
	}
	if stored, err := ops.List(operations.Filter{OwnerUserID: "local-operator", WorkspaceID: "local", Limit: 10}); err != nil || len(stored) != 0 {
		t.Fatalf("health-only liveness must not create durable discovery evidence: (%#v, %v)", stored, err)
	}

	openclaw, ok := service.reg.Adapter("openclaw")
	if !ok {
		t.Fatal("OpenClaw adapter must remain available in Runtime Lab")
	}
	if _, err := openclaw.Execute(context.Background(), map[string]any{"task": "do not run"}); err == nil {
		t.Fatal("Runtime Lab must not gain OpenClaw execution authority from the canonical registry")
	}
}

func TestOpenClawProtocolDiscoveryPersistsRedactedEvidenceAndRecoversIt(t *testing.T) {
	ledger := operations.NewMemoryRepository()
	ops := operations.NewService(ledger)
	broker := newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local")
	canonical := agentruntime.NewRegistry(&runtimeLabAgentAdapter{
		info: agentruntime.Info{ID: "openclaw", Name: "OpenClaw", Enabled: true, Configured: true, ReadOnlyDefault: true},
		health: agentruntime.Health{
			RuntimeID:                "openclaw",
			Status:                   "available",
			Reason:                   "read-only protocol discovery verified",
			Version:                  "2026.8.1",
			GatewayProtocolValidated: true,
			GatewayAuthenticated:     true,
			GatewayScope:             "operator.read",
			GatewayEndpointSHA256:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			GatewayEvidenceSchema:    "openclaw-gateway-protocol-v4",
			GatewayTaskLedger: &agentruntime.GatewayTaskLedgerSummary{
				SampledTasks: 2,
				StatusCounts: map[string]int{"queued": 1, "running": 1},
				Truncated:    true,
			},
			GatewayCapabilityCatalog: &agentruntime.GatewayCapabilityCatalogSummary{
				SampledSkills:      3,
				EligibleSkills:     2,
				SampledCommands:    4,
				ToolCountsBySource: map[string]int{"core": 2, "plugin": 1},
			},
			GatewayPreparedModelCatalog: &agentruntime.GatewayPreparedModelCatalogSummary{
				SampledModels:             3,
				AvailableModels:           1,
				UnavailableModels:         1,
				UnknownAvailabilityModels: 1,
			},
			GatewayAgentRoster: &agentruntime.GatewayAgentRosterSummary{
				SampledAgents:    4,
				AgentCount:       2,
				SystemCount:      1,
				UnknownKindCount: 1,
			},
			CheckedAt: time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC),
		},
	})
	service := NewServiceWithAgentRuntimeRegistry(broker, ops, "local-operator", "local", canonical)
	service.now = func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) }

	probe, ok := service.Probe(context.Background(), "openclaw")
	if !ok || !probe.ProtocolValid || !probe.Authenticated || !probe.EvidencePersisted || probe.EvidenceSHA256 == "" {
		t.Fatalf("expected persisted protocol-validated OpenClaw discovery, got (%#v, %t)", probe, ok)
	}
	if probe.EndpointSHA256 != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || probe.EvidenceSchema != "openclaw-gateway-protocol-v4" {
		t.Fatalf("probe lost safe evidence metadata: %#v", probe)
	}

	stored, err := ops.List(operations.Filter{OwnerUserID: "local-operator", WorkspaceID: "local", Limit: 10})
	if err != nil || len(stored) != 1 {
		t.Fatalf("discovery ledger entries = (%#v, %v)", stored, err)
	}
	op := stored[0]
	if op.OperationType != openClawDiscoveryOperationType || op.RuntimeID != "openclaw" || op.Status != string(operations.StatusCompleted) {
		t.Fatalf("unexpected durable discovery operation: %#v", op)
	}
	if strings.Contains(op.EvidenceJSON, "gateway.example.test") || strings.Contains(op.EvidenceJSON, "secret") {
		t.Fatalf("durable evidence must not retain endpoint or secrets: %s", op.EvidenceJSON)
	}
	var evidence openClawDiscoveryEvidence
	if err := json.Unmarshal([]byte(op.EvidenceJSON), &evidence); err != nil {
		t.Fatalf("decode durable evidence: %v", err)
	}
	if evidence.EndpointSHA256 != probe.EndpointSHA256 || evidence.GatewayTaskLedger == nil || evidence.GatewayTaskLedger.SampledTasks != 2 || evidence.GatewayCapabilityCatalog == nil || evidence.GatewayCapabilityCatalog.SampledSkills != 3 || evidence.GatewayPreparedModelCatalog == nil || evidence.GatewayPreparedModelCatalog.UnknownAvailabilityModels != 1 || evidence.GatewayAgentRoster == nil || evidence.GatewayAgentRoster.AgentCount != 2 {
		t.Fatalf("unexpected durable evidence: %#v", evidence)
	}
	if probe.GatewayCapabilityCatalog == nil || probe.GatewayCapabilityCatalog.ToolCountsBySource["plugin"] != 1 || probe.GatewayPreparedModelCatalog == nil || probe.GatewayPreparedModelCatalog.AvailableModels != 1 || probe.GatewayAgentRoster == nil || probe.GatewayAgentRoster.SystemCount != 1 {
		t.Fatalf("capability discovery was not projected into Runtime Lab: %#v", probe)
	}

	// A new Runtime Lab instance has no in-memory probe, but it can recover the
	// still-fresh, owner-scoped discovery record without probing or executing.
	restarted := NewServiceWithAgentRuntimeRegistry(broker, ops, "local-operator", "local", canonical)
	restarted.now = service.now
	attempts := restarted.Attempts("openclaw")
	if len(attempts) != 1 || attempts[0].Status != AttemptSucceeded || !attempts[0].DiscoveryRecovered {
		t.Fatalf("durable discovery was not recovered after restart: %#v", attempts)
	}
	cards, err := restarted.CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("capability cards: %v", err)
	}
	cardState := map[string]RuntimeCapabilityCard{}
	for _, card := range cards.Cards {
		cardState[card.ID] = card
	}
	for _, cardID := range []string{"openclaw.tasks.discovery", "openclaw.capabilities.discovery", "openclaw.models.discovery", "openclaw.agents.discovery"} {
		card, found := cardState[cardID]
		if !found || !card.CanInvoke || card.LatestDiscovery == nil || !card.LatestDiscovery.EvidencePersisted {
			t.Fatalf("OpenClaw card %s did not recover durable discovery: %#v", cardID, card)
		}
	}
}

func TestOpenClawExpiredDiscoveryCannotRestoreReadiness(t *testing.T) {
	ledger := operations.NewMemoryRepository()
	ops := operations.NewService(ledger)
	broker := newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local")
	canonical := agentruntime.NewRegistry(&runtimeLabAgentAdapter{
		info: agentruntime.Info{ID: "openclaw", Name: "OpenClaw", Enabled: true, Configured: true, ReadOnlyDefault: true},
		health: agentruntime.Health{
			RuntimeID:                "openclaw",
			Status:                   "available",
			GatewayProtocolValidated: true,
			GatewayEndpointSHA256:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			GatewayEvidenceSchema:    "openclaw-gateway-protocol-v4",
			CheckedAt:                time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC),
		},
	})
	service := NewServiceWithAgentRuntimeRegistry(broker, ops, "local-operator", "local", canonical)
	service.now = func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) }
	if probe, ok := service.Probe(context.Background(), "openclaw"); !ok || !probe.EvidencePersisted {
		t.Fatalf("persist initial discovery = (%#v, %t)", probe, ok)
	}
	service.now = func() time.Time { return time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC) }
	currentCards, err := service.CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("in-memory expiry capability cards: %v", err)
	}
	for _, card := range currentCards.Cards {
		if card.ID == "openclaw.gateway.discovery" && card.CanInvoke {
			t.Fatalf("expired in-memory discovery must not enable invocation: %#v", card)
		}
	}

	expired := NewServiceWithAgentRuntimeRegistry(broker, ops, "local-operator", "local", canonical)
	expired.now = func() time.Time { return time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC) }
	if attempts := expired.Attempts("openclaw"); len(attempts) != 0 {
		t.Fatalf("expired discovery must not be restored as a runtime attempt: %#v", attempts)
	}
	cards, err := expired.CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("capability cards: %v", err)
	}
	for _, card := range cards.Cards {
		if card.ID == "openclaw.gateway.discovery" {
			if card.CanInvoke {
				t.Fatalf("expired discovery must not enable invocation: %#v", card)
			}
			return
		}
	}
	t.Fatal("missing OpenClaw discovery capability card")
}

func TestOpenClawAggregateCardsRequireAuthenticatedOperatorRead(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		authenticated bool
		scope         string
		wantPersisted bool
		wantAvailable bool
	}{
		{name: "unauthenticated aggregates", wantPersisted: false, wantAvailable: false},
		{name: "wrong authenticated scope", authenticated: true, scope: "operator.admin", wantPersisted: false, wantAvailable: false},
		{name: "operator read scope", authenticated: true, scope: "operator.read", wantPersisted: true, wantAvailable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := operations.NewService(operations.NewMemoryRepository())
			broker := newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local")
			canonical := agentruntime.NewRegistry(&runtimeLabAgentAdapter{
				info: agentruntime.Info{ID: "openclaw", Name: "OpenClaw", Enabled: true, Configured: true, ReadOnlyDefault: true},
				health: agentruntime.Health{
					RuntimeID:                "openclaw",
					Status:                   "available",
					Version:                  "2026.8.1",
					GatewayProtocolValidated: true,
					GatewayAuthenticated:     tt.authenticated,
					GatewayScope:             tt.scope,
					GatewayEndpointSHA256:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					GatewayEvidenceSchema:    openClawDiscoverySchema,
					GatewayTaskLedger:        &agentruntime.GatewayTaskLedgerSummary{SampledTasks: 1, StatusCounts: map[string]int{"queued": 1}},
					GatewayCapabilityCatalog: &agentruntime.GatewayCapabilityCatalogSummary{SampledSkills: 1, EligibleSkills: 1, SampledCommands: 1, ToolCountsBySource: map[string]int{"core": 1}},
					GatewayPreparedModelCatalog: &agentruntime.GatewayPreparedModelCatalogSummary{
						SampledModels: 1, AvailableModels: 1,
					},
					GatewayAgentRoster: &agentruntime.GatewayAgentRosterSummary{
						SampledAgents: 1, AgentCount: 1,
					},
				},
			})
			service := NewServiceWithAgentRuntimeRegistry(broker, ops, "local-operator", "local", canonical)
			service.now = func() time.Time { return now }

			probe, ok := service.Probe(context.Background(), "openclaw")
			if !ok || probe.EvidencePersisted != tt.wantPersisted {
				t.Fatalf("probe persisted = (%t, %v), want %v", ok, probe.EvidencePersisted, tt.wantPersisted)
			}
			cards, err := service.CapabilityCards(context.Background())
			if err != nil {
				t.Fatalf("capability cards: %v", err)
			}
			byID := make(map[string]RuntimeCapabilityCard, len(cards.Cards))
			for _, card := range cards.Cards {
				byID[card.ID] = card
			}
			for _, id := range []string{
				"openclaw.tasks.discovery",
				"openclaw.capabilities.discovery",
				"openclaw.models.discovery",
				"openclaw.agents.discovery",
			} {
				card, found := byID[id]
				if !found || card.CanInvoke != tt.wantAvailable || card.CanExecuteExternalEffect {
					t.Errorf("aggregate card %s = %#v, want invoke=%v and no external effects", id, card, tt.wantAvailable)
				}
			}
			delegation, found := byID["openclaw.agent.delegate"]
			if !found || delegation.CanInvoke || delegation.CanExecuteExternalEffect || delegation.RiskLevel != "high" || len(delegation.ApprovalRequirements) == 0 {
				t.Errorf("read-only discovery must not enable high-risk delegation: %#v", delegation)
			}
		})
	}
}

func TestRemoteCapabilityCardsRequireFreshDiscovery(t *testing.T) {
	t.Setenv("OPENCLAW_BASE_URL", "")
	t.Setenv("HERMES_BASE_URL", "http://127.0.0.1:8999")
	t.Setenv("ODYSSEUS_BASE_URL", "")
	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	service := newTestService(t)
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	adapter, ok := service.reg.Adapter("hermes")
	if !ok {
		t.Fatal("Hermes adapter is not registered")
	}
	remote, ok := adapter.(*remoteRuntime)
	if !ok {
		t.Fatalf("Hermes adapter type = %T, want *remoteRuntime", adapter)
	}

	tests := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "fresh", age: runtimeDiscoveryFreshnessTTL - time.Second, want: true},
		{name: "expired", age: runtimeDiscoveryFreshnessTTL, want: false},
		{name: "future dated", age: -time.Second, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote.remember(ProbeResult{
				RuntimeID:      "hermes",
				ProtocolValid:  true,
				ReadinessLevel: ReadinessHealthChecked,
				CheckedAt:      now.Add(-tt.age),
			})
			cards, err := service.CapabilityCards(context.Background())
			if err != nil {
				t.Fatalf("capability cards: %v", err)
			}
			for _, card := range cards.Cards {
				if card.ID == "hermes.gateway.discovery" {
					if card.CanInvoke != tt.want {
						t.Fatalf("CanInvoke = %v, want %v for %s evidence", card.CanInvoke, tt.want, tt.name)
					}
					return
				}
			}
			t.Fatal("missing Hermes gateway discovery card")
		})
	}
}

func TestOpenClawAggregateLedgerEvidenceRejectsMissingScopeAndFutureTime(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	probe := ProbeResult{
		RuntimeID:         "openclaw",
		Protocol:          "openclaw-gateway-v4",
		ProtocolValid:     true,
		Authenticated:     true,
		GatewayScope:      "operator.read",
		EvidenceSchema:    openClawDiscoverySchema,
		EndpointSHA256:    "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		GatewayTaskLedger: &GatewayTaskLedgerSummary{SampledTasks: 1, StatusCounts: map[string]int{"running": 1}},
		CheckedAt:         now,
	}
	evidence, err := newOpenClawDiscoveryEvidence(probe, now)
	if err != nil {
		t.Fatalf("build valid discovery evidence: %v", err)
	}
	if !validOpenClawDiscoveryEvidence(evidence, now) {
		t.Fatal("authenticated operator.read aggregate evidence should be valid")
	}

	unauthenticated := evidence
	unauthenticated.Authenticated = false
	unauthenticated.GatewayScope = ""
	unauthenticated.EvidenceSHA256 = openClawDiscoveryEvidenceSHA256(unauthenticated)
	if validOpenClawDiscoveryEvidence(unauthenticated, now) {
		t.Fatal("aggregate evidence without authentication must not be restored")
	}

	future := evidence
	future.CheckedAt = now.Add(time.Second)
	future.ExpiresAt = future.CheckedAt.Add(openClawDiscoveryEvidenceTTL)
	future.EvidenceSHA256 = openClawDiscoveryEvidenceSHA256(future)
	if validOpenClawDiscoveryEvidence(future, now) {
		t.Fatal("future-dated discovery evidence must not be restored")
	}
	futureProbe := probe
	futureProbe.CheckedAt = now.Add(time.Second)
	if _, err := newOpenClawDiscoveryEvidence(futureProbe, now); err == nil {
		t.Fatal("future-dated discovery must not be reported as persisted")
	}
}

type runtimeLabAgentAdapter struct {
	info   agentruntime.Info
	health agentruntime.Health
}

func (a *runtimeLabAgentAdapter) Info() agentruntime.Info { return a.info }

func (a *runtimeLabAgentAdapter) HealthCheck(context.Context) agentruntime.Health { return a.health }

func (*runtimeLabAgentAdapter) ListSkills(context.Context) []agentruntime.Skill { return nil }

func (a *runtimeLabAgentAdapter) ExecuteTask(context.Context, agentruntime.Task) agentruntime.Result {
	return agentruntime.Result{RuntimeID: a.info.ID, Status: "blocked"}
}

func (a *runtimeLabAgentAdapter) StopTask(context.Context, string) agentruntime.StopResult {
	return agentruntime.StopResult{RuntimeID: a.info.ID, Status: "unsupported"}
}

func TestHermesDiscoveryUsesIdentityAndOptionalAuthenticatedCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok","platform":"hermes-agent","version":"2026.8.3"}`))
		case "/v1/capabilities":
			if got := r.Header.Get("Authorization"); got != "Bearer local-test-key" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"object":"hermes.api_server.capabilities","platform":"hermes-agent","features":{"skills_api":true,"audio_api":false,"run_submission":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("HERMES_BASE_URL", server.URL)
	t.Setenv("HERMES_API_SERVER_KEY", "local-test-key")
	runtime := newRemoteRuntime("hermes", "Hermes", "HERMES_BASE_URL")
	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeBlocked || probe.ReadinessLevel != ReadinessHealthChecked ||
		!probe.ProtocolValid || !probe.IdentityVerified || !probe.Authenticated || probe.RuntimeVersion != "2026.8.3" {
		t.Fatalf("Hermes discovery result = %#v", probe)
	}
	if len(probe.Capabilities) != 2 || probe.Capabilities[0] != "run_submission" || probe.Capabilities[1] != "skills_api" {
		t.Fatalf("bounded enabled capabilities = %#v", probe.Capabilities)
	}
}

func TestOdysseusDiscoveryUsesReviewedHealthAndVersionPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"status":"healthy","timestamp":"2026-08-08T12:00:00Z"}`))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.9.0-dev"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("ODYSSEUS_BASE_URL", server.URL)
	t.Setenv("ODYSSEUS_HEALTH_PATH", "")
	runtime := newRemoteRuntime("odysseus", "Odysseus", "ODYSSEUS_BASE_URL")
	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeBlocked || probe.ReadinessLevel != ReadinessAvailable ||
		!probe.ProtocolValid || probe.IdentityVerified || probe.RuntimeVersion != "0.9.0-dev" {
		t.Fatalf("Odysseus discovery result = %#v", probe)
	}
}

func TestProtocolDiscoveryAdvancesOnlyTheReadOnlyCapabilityCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","platform":"hermes-agent","version":"2026.8.3"}`))
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("HERMES_BASE_URL", server.URL)
	t.Setenv("HERMES_API_SERVER_KEY", "")
	svc := newTestService(t)
	probe, ok := svc.Probe(context.Background(), "hermes")
	if !ok || !probe.ProtocolValid {
		t.Fatalf("Hermes probe = (%#v, %t)", probe, ok)
	}
	overview, err := svc.CapabilityCards(context.Background())
	if err != nil {
		t.Fatalf("capability cards: %v", err)
	}
	for _, card := range overview.Cards {
		if card.RuntimeID != "hermes" {
			continue
		}
		if card.ID == "hermes.gateway.discovery" {
			if !card.CanInvoke || card.ReadinessLevel != ReadinessHealthChecked || card.LatestDiscovery == nil {
				t.Fatalf("Hermes discovery card did not advance: %#v", card)
			}
			continue
		}
		if card.CanInvoke || card.ReadinessLevel != ReadinessConfigured || card.CanExecuteExternalEffect {
			t.Fatalf("non-discovery Hermes card widened: %#v", card)
		}
	}
}

func TestRemoteHealthProbeDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://unreviewed.example/health", http.StatusFound)
	}))
	defer server.Close()

	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("OPENHANDS_BASE_URL", server.URL)
	t.Setenv("OPENHANDS_HEALTH_PATH", "/health")
	runtime := newRemoteRuntime("openhands", "OpenHands", "OPENHANDS_BASE_URL")

	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeUnavailable || probe.Detail != "health HTTP 302" {
		t.Fatalf("redirecting health endpoint must not be followed: %#v", probe)
	}
}

func TestRemoteHealthProbeDoesNotExposeEndpointOnTransportFailure(t *testing.T) {
	t.Setenv(runtimeLabAllowedHostsEnv, "127.0.0.1")
	t.Setenv("OPENHANDS_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OPENHANDS_HEALTH_PATH", "/health")
	runtime := newRemoteRuntime("openhands", "OpenHands", "OPENHANDS_BASE_URL")

	probe := runtime.Probe(context.Background(), time.Now().UTC())
	if probe.Status != executionbroker.RuntimeUnavailable {
		t.Fatalf("probe status = %q, want unavailable: %#v", probe.Status, probe)
	}
	if probe.Detail != "runtime discovery could not reach or validate the configured endpoint; review the local runtime configuration" {
		t.Fatalf("probe detail = %q", probe.Detail)
	}
	for _, forbidden := range []string{"127.0.0.1", ":1", "connect"} {
		if strings.Contains(strings.ToLower(probe.Detail), strings.ToLower(forbidden)) {
			t.Fatalf("probe detail leaked %q: %q", forbidden, probe.Detail)
		}
	}
}

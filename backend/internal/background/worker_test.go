package background

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func buildWorker(t *testing.T, mode autonomypolicy.Mode, feedJSON string) (*Worker, *operations.Service, string) {
	return buildWorkerWithAuthorization(t, mode, feedJSON, true)
}

func buildWorkerWithAuthorization(
	t *testing.T,
	mode autonomypolicy.Mode,
	feedJSON string,
	authorized bool,
) (*Worker, *operations.Service, string) {
	t.Helper()
	feedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(feedDir, "feed.json"), []byte(feedJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	feed := accountfeed.Feed{
		ID:           uuid.New(),
		Name:         "inbox",
		Provider:     "local",
		AccountLabel: "primary",
		SourceType:   accountfeed.SourceLocalJSONFile,
		Path:         "feed.json",
		OwnerUserID:  "user-1",
		WorkspaceID:  "local",
		Enabled:      true,
	}
	reader, err := accountfeed.NewLocalFileReader(feed, feedDir)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	svc := operations.NewService(operations.NewMemoryRepository())
	workspace := t.TempDir()
	broker := executionbroker.NewBroker(workspace)
	if authorized {
		broker = newAuthorizedBackgroundTestBroker(
			t,
			workspace,
			"user-1",
			"local",
		)
	}
	w := New(svc, broker, []accountfeed.Reader{reader}, Options{
		OwnerUserID: "user-1", WorkspaceID: "local", Mode: mode,
	})
	return w, svc, workspace
}

func newAuthorizedBackgroundTestBroker(
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
			Source: "background-test",
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

const twoItemFeed = `[
  {"externalId":"a1","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"},
  {"externalId":"a2","title":"Pay invoice to landlord","body":"Send payment for the rent invoice"}
]`

func TestRunOnceVerticalSlice(t *testing.T) {
	w, svc, workspace := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if rep.OperationsCreated != 2 {
		t.Fatalf("want 2 operations created, got %d (errors: %v)", rep.OperationsCreated, rep.Errors)
	}
	if rep.AutoExecuted != 1 || rep.Verified != 1 {
		t.Fatalf("want one auto-executed+verified operation, got auto=%d verified=%d", rep.AutoExecuted, rep.Verified)
	}
	if rep.AwaitingApproval != 1 {
		t.Fatalf("want the high-risk operation awaiting approval, got %d", rep.AwaitingApproval)
	}

	// The safe worker must have actually written an artifact into the workspace.
	entries, _ := os.ReadDir(workspace)
	if len(entries) == 0 {
		t.Fatalf("safe worker did not create any artifact in the workspace")
	}

	// The completed operation must be verification-passed.
	completed, err := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 {
		t.Fatalf("want 1 completed operation, got %d", len(completed))
	}
	if completed[0].VerificationStatus != string(operations.VerificationPassed) {
		t.Fatalf("completed operation must be verification-passed, got %q", completed[0].VerificationStatus)
	}
	if completed[0].RuntimeID != executionbroker.LocalSafeWorkerID {
		t.Fatalf("completed operation must record the runtime, got %q", completed[0].RuntimeID)
	}

	// Audit trail exists.
	events, _ := svc.Events(completed[0].ID)
	if len(events) < 2 {
		t.Fatalf("completed operation must have an audit trail, got %d events", len(events))
	}
}

func TestRunOnceWithoutExecutionAuthorizationFailsClosed(t *testing.T) {
	w, svc, workspace := buildWorkerWithAuthorization(
		t,
		autonomypolicy.ModeAutonomousSafe,
		`[{"externalId":"a1","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"}]`,
		false,
	)
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if rep.Verified != 0 || rep.Failed != 1 {
		t.Fatalf(
			"unauthorized execution must fail without verification, got verified=%d failed=%d",
			rep.Verified,
			rep.Failed,
		)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("failed operation should be recorded, not hidden as a pass error: %v", rep.Errors)
	}
	entries, readErr := os.ReadDir(workspace)
	if readErr != nil {
		t.Fatalf("read workspace: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("unauthorized worker created %d workspace artifacts", len(entries))
	}
	failed, err := svc.List(operations.Filter{
		OwnerUserID: "user-1",
		WorkspaceID: "local",
		Status:      operations.StatusFailed,
	})
	if err != nil {
		t.Fatalf("list failed operations: %v", err)
	}
	if len(failed) != 1 ||
		!strings.Contains(failed[0].LastError, executionbroker.ErrAuthorizationRequired.Error()) {
		t.Fatalf("failed operation did not retain authorization cause: %#v", failed)
	}
}

func TestFastTriageLaneStampsOperations(t *testing.T) {
	w, svc, _ := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
	mi := modelintelligence.NewService(modelintelligence.NewRegistryFromEnv())
	w.WithModelIntelligence(mi)

	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Triaged != 2 {
		t.Fatalf("both operations must be triaged by the fast-triage lane, got %d", rep.Triaged)
	}
	// The lane must have stamped the model provider on the operation record.
	ops, _ := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local"})
	for _, op := range ops {
		if op.ModelProviderID != modelintelligence.ProviderTestFastTriage {
			t.Fatalf("operation %s missing triage model provider, got %q", op.ID, op.ModelProviderID)
		}
	}
	// The lane must have produced real telemetry surfaced by model intelligence.
	if len(mi.Telemetry()) < 2 {
		t.Fatalf("fast-triage lane must record telemetry, got %d rows", len(mi.Telemetry()))
	}
	if len(mi.LaneWinners()) == 0 {
		t.Fatalf("triage runs must yield a fast-triage lane winner")
	}
}

func TestRunOnceIsIdempotent(t *testing.T) {
	w, _, _ := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.OperationsCreated != 0 {
		t.Fatalf("second pass must not create duplicate operations, created %d", rep.OperationsCreated)
	}
}

func TestRunOnceStopsBeforeAnyWorkWhenContextIsCancelled(t *testing.T) {
	w, svc, workspace := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := w.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want context cancellation", err)
	}
	if report.FeedsRead != 0 || report.ItemsIngested != 0 || report.OperationsCreated != 0 || report.AutoExecuted != 0 {
		t.Fatalf("cancelled pass performed work: %#v", report)
	}
	operations, err := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local"})
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(operations) != 0 {
		t.Fatalf("cancelled pass created operations: %#v", operations)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled pass created artifacts: %#v", entries)
	}
}

func TestEmergencyStopProcessesNothing(t *testing.T) {
	w, svc, _ := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
	w.opts.EmergencyStop = true
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.OperationsCreated != 2 {
		t.Fatalf("emergency stop still records ingested items, got %d", rep.OperationsCreated)
	}
	if rep.AutoExecuted != 0 || rep.Classified != 0 {
		t.Fatalf("emergency stop must not process operations, auto=%d classified=%d", rep.AutoExecuted, rep.Classified)
	}
	// Everything stays in `new`.
	news, _ := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusNew})
	if len(news) != 2 {
		t.Fatalf("emergency stop must leave operations in `new`, got %d", len(news))
	}
}

func TestReadOnlyModeObservesOnly(t *testing.T) {
	w, svc, _ := buildWorker(t, autonomypolicy.ModeReadOnly, twoItemFeed)
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.AutoExecuted != 0 {
		t.Fatalf("read-only mode must not execute, got auto=%d", rep.AutoExecuted)
	}
	// Low-risk item is observed (stays classified), high-risk still needs approval.
	classified, _ := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusClassified})
	if len(classified) == 0 {
		t.Fatalf("read-only mode should classify+observe low-risk items")
	}
}

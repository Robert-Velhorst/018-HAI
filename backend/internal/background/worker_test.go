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
	"automation-hub-backend/internal/models"
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
	if rep.AutoExecuted != 0 || rep.Verified != 0 {
		t.Fatalf("source-derived feed items must not be auto-executed, got auto=%d verified=%d", rep.AutoExecuted, rep.Verified)
	}
	if rep.AwaitingApproval != 2 {
		t.Fatalf("both source-derived operations must await approval, got %d", rep.AwaitingApproval)
	}

	// Source intake may create internal ledger entries but no host artifact.
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unreviewed source content caused a workspace effect: entries=%v err=%v", entries, err)
	}

	// Imported items remain in the ledger for explicit owner review.
	completed, err := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 0 {
		t.Fatalf("unreviewed source-derived operations must not complete, got %d", len(completed))
	}
	awaiting, err := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusAwaitingApproval})
	if err != nil || len(awaiting) != 2 {
		t.Fatalf("unreviewed source items not retained for owner review: count=%d err=%v", len(awaiting), err)
	}
}

func TestRunOnceAutomaticallyRecoversExpiredOperationClaims(t *testing.T) {
	repo := operations.NewMemoryRepository()
	svc := operations.NewService(repo)
	ingested, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID: "user-1", WorkspaceID: "local", Title: "stale worker operation",
		OperationType: "test", SourceType: "runtime_lab", DedupeKey: "stale-worker-operation",
	})
	if err != nil {
		t.Fatalf("ingest operation: %v", err)
	}
	ingested.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	classified, err := svc.Transition(ingested.Operation, operations.StatusClassified, "hai", "", "classified for safe local execution")
	if err != nil {
		t.Fatalf("classify operation: %v", err)
	}
	ready, err := svc.Transition(*classified, operations.StatusReady, "hai", "", "ready for execution")
	if err != nil {
		t.Fatalf("ready operation: %v", err)
	}
	claimed, err := svc.ClaimNext(context.Background(), "user-1", "local", uuid.New(), 5*time.Millisecond)
	if err != nil || claimed == nil {
		t.Fatalf("claim operation: claim=%#v err=%v", claimed, err)
	}
	if claimed.Operation.ID != ready.ID {
		t.Fatalf("claimed operation %s, want %s", claimed.Operation.ID, ready.ID)
	}
	if _, err := svc.TransitionClaimed(context.Background(), claimed.Claim, claimed.Operation, operations.StatusRunning, "hai", "", "worker started"); err != nil {
		t.Fatalf("mark operation running: %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	worker := New(svc, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})
	report, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.RecoveredOperations != 1 || report.UnleasedExecutingOperations != 0 || report.ExpiredOperationClaimsRemaining != 0 {
		t.Fatalf("recovery report = %#v, want one recovered operation and no remaining claims", report)
	}
	interrupted, err := svc.List(operations.Filter{OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusInterrupted})
	if err != nil || len(interrupted) != 1 || interrupted[0].ID != ready.ID {
		t.Fatalf("interrupted operations = %#v, err=%v; want recovered operation", interrupted, err)
	}
}

func TestRunOnceSourceDerivedWorkAwaitsOwnerAcceptanceWithoutExecutor(t *testing.T) {
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
	if rep.Verified != 0 || rep.Failed != 0 || rep.AwaitingApproval != 1 {
		t.Fatalf("source-derived operation must await owner review before broker authorization: %+v", rep)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("approval routing should not report an execution failure: %v", rep.Errors)
	}
	entries, readErr := os.ReadDir(workspace)
	if readErr != nil {
		t.Fatalf("read workspace: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("unauthorized worker created %d workspace artifacts", len(entries))
	}
	awaiting, err := svc.List(operations.Filter{
		OwnerUserID: "user-1",
		WorkspaceID: "local",
		Status:      operations.StatusAwaitingApproval,
	})
	if err != nil {
		t.Fatalf("list operations awaiting approval: %v", err)
	}
	if len(awaiting) != 1 {
		t.Fatalf("source-derived operation not retained for owner review: %#v", awaiting)
	}
	retryReport, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if retryReport.AutoExecuted != 0 || retryReport.Failed != 0 {
		t.Fatalf("unreviewed source operation advanced on a later pass: %#v", retryReport)
	}
}

func TestSourceDerivedHostEffectGateRequiresExactUnconsumedOwnerReceipt(t *testing.T) {
	repo := operations.NewMemoryRepository()
	svc := operations.NewService(repo)
	sourceID := uuid.New()
	created, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID: "owner-1", WorkspaceID: "local", Title: "Review imported note",
		Description: "Create a local review artifact", OperationType: "review", SourceType: "email",
		SourceID: &sourceID, SourceRevisionHash: strings.Repeat("b", 64), DedupeKey: "gate-" + sourceID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	op := created.Operation
	op.CurrentDecision = string(operations.DecisionAskRobert)
	op.RequiresApproval = true
	op.RiskLevel = string(operations.RiskMedium)
	op.AutonomyLevel = string(operations.AutonomyApproval)
	classified, err := svc.Transition(op, operations.StatusClassified, string(operations.OwnerHAI), "", "classified")
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := svc.Transition(*classified, operations.StatusAwaitingApproval, string(operations.OwnerHAI), "", "awaiting owner")
	if err != nil {
		t.Fatal(err)
	}
	worker := New(svc, nil, nil, Options{OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, Mode: autonomypolicy.ModeAutonomousSafe})
	if worker.safeExecutionPolicyAllows(*awaiting) {
		t.Fatal("unapproved source-derived operation passed host-effect policy")
	}
	preview, err := operations.PreviewSourceApproval(*awaiting)
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := svc.ApproveSourceDerived(*awaiting, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !worker.safeExecutionPolicyAllows(*approved) {
		t.Fatal("exactly approved source revision was not eligible to enter claimed worker path")
	}
	claimed, err := svc.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("claim approved operation: %v", err)
	}
	prepared := claimed.Operation
	prepared.RuntimeID = executionbroker.LocalSafeWorkerID
	prepared.VerificationStatus = string(operations.VerificationPending)
	running, err := svc.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, claimed.Operation, prepared, string(operations.OwnerHAI), "worker")
	if err != nil {
		t.Fatalf("consume approved source receipt: %v", err)
	}
	if !worker.safeExecutionPolicyAllows(*running) {
		t.Fatal("exact, consumed receipt did not pass final source-derived host-effect gate")
	}
	if _, err := svc.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, *approved, prepared, string(operations.OwnerHAI), "replay"); !errors.Is(err, operations.ErrSourceApprovalReplayed) {
		t.Fatalf("replayed source receipt error = %v", err)
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
	w, _, workspace := buildWorker(t, autonomypolicy.ModeAutonomousSafe, twoItemFeed)
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
	if rep.AutoExecuted != 0 {
		t.Fatalf("second pass re-executed completed work: %#v", rep)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("artifact count after repeated pass = %d, err=%v; unreviewed source items must not write", len(entries), err)
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

func TestRunOnceLeaseIsSharedAcrossWorkersUsingSameService(t *testing.T) {
	started := make(chan struct{})
	continueRead := make(chan struct{})
	reader := blockingReader{started: started, continueRead: continueRead}
	svc := operations.NewService(operations.NewMemoryRepository())
	opts := Options{OwnerUserID: "user-1", WorkspaceID: "local", Mode: autonomypolicy.ModeAutonomousSafe}
	first := New(svc, nil, []accountfeed.Reader{reader}, opts)
	second := New(svc, nil, nil, opts)
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.RunOnce(context.Background())
		firstDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not enter the feed reader")
	}
	if _, err := second.RunOnce(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second worker RunOnce() error = %v, want ErrBusy", err)
	}
	close(continueRead)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first worker RunOnce() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not release its run lease")
	}
	if _, err := second.RunOnce(context.Background()); err != nil {
		t.Fatalf("second worker could not acquire released lease: %v", err)
	}
}

func TestRunOnceResumesClassifiedAndReadyOperations(t *testing.T) {
	for _, startStatus := range []operations.OperationStatus{operations.StatusClassified, operations.StatusReady} {
		t.Run(string(startStatus), func(t *testing.T) {
			w, svc, workspace := buildWorker(t, autonomypolicy.ModeAutonomousSafe, "[]")
			created, err := svc.Ingest(operations.NewOperationInput{
				OwnerUserID:   "user-1",
				WorkspaceID:   "local",
				Title:         "Organize local notes",
				OperationType: "organize",
				SourceType:    "manual",
				DedupeKey:     "resume-" + string(startStatus),
			})
			if err != nil {
				t.Fatalf("ingest operation: %v", err)
			}
			created.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
			created.Operation.RiskLevel = string(operations.RiskLow)
			created.Operation.AutonomyLevel = string(operations.AutonomyAuto)
			created.Operation.OwnerType = string(operations.OwnerHAI)
			created.Operation.RequiresApproval = false
			created.Operation.RecommendedAction = "run safe local worker"
			created.Operation.VerificationStatus = string(operations.VerificationPending)
			classified, err := svc.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified before worker restart")
			if err != nil {
				t.Fatalf("transition to classified: %v", err)
			}
			if startStatus == operations.StatusReady {
				classified, err = svc.Transition(*classified, operations.StatusReady, "hai", "", "ready before worker restart")
				if err != nil {
					t.Fatalf("transition to ready: %v", err)
				}
			}

			report, err := w.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if report.Verified != 1 {
				t.Fatalf("RunOnce verified %d operations, want 1", report.Verified)
			}
			completed, err := svc.Get("user-1", "local", created.Operation.ID)
			if err != nil {
				t.Fatalf("load operation: %v", err)
			}
			if completed.Status != string(operations.StatusCompleted) || completed.VerificationStatus != string(operations.VerificationPassed) {
				t.Fatalf("resumed operation state = %s/%s, want completed/passed", completed.Status, completed.VerificationStatus)
			}
			entries, err := os.ReadDir(workspace)
			if err != nil || len(entries) != 1 {
				t.Fatalf("safe artifact count = %d, err=%v; want one", len(entries), err)
			}
		})
	}
}

func TestExecuteSafeOperationCancellationIsInterruptedNotFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	baseRepository := operations.NewMemoryRepository()
	repo := &cancelOnStatusRepository{
		Repository:      baseRepository,
		ClaimRepository: baseRepository,
		status:          string(operations.StatusRunning),
		cancel:          cancel,
	}
	svc := operations.NewService(repo)
	workspace := t.TempDir()
	broker, err := executionbroker.NewAuthorizedBroker(
		workspace,
		"user-1",
		"local",
		cancellationAuthorization{},
	)
	if err != nil {
		t.Fatalf("create test broker: %v", err)
	}
	created, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID:   "user-1",
		WorkspaceID:   "local",
		Title:         "Organize local notes",
		OperationType: "organize",
		SourceType:    "manual",
		DedupeKey:     "cancel-at-running",
	})
	if err != nil {
		t.Fatalf("ingest operation: %v", err)
	}
	created.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	created.Operation.RiskLevel = string(operations.RiskLow)
	created.Operation.AutonomyLevel = string(operations.AutonomyAuto)
	created.Operation.OwnerType = string(operations.OwnerHAI)
	created.Operation.RequiresApproval = false
	created.Operation.VerificationStatus = string(operations.VerificationPending)
	_, err = svc.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("transition to classified: %v", err)
	}

	w := New(svc, broker, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})
	report, err := w.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want context.Canceled", err)
	}
	if report.Interrupted != 1 || report.Failed != 0 {
		t.Fatalf("canceled run report = %#v, want one interrupted and no failures", report)
	}
	stored, err := svc.Get("user-1", "local", created.Operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if stored.Status != string(operations.StatusInterrupted) {
		t.Fatalf("stored status = %q, want interrupted", stored.Status)
	}
	if stored.VerificationStatus == string(operations.VerificationFailed) {
		t.Fatal("canceled execution must not be reported as a verification failure")
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled run created artifacts: count=%d err=%v", len(entries), err)
	}
}

func TestRunDraftFinishesPersistedLocalDraftAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	baseRepository := operations.NewMemoryRepository()
	repo := &cancelOnStatusRepository{
		Repository:      baseRepository,
		ClaimRepository: baseRepository,
		status:          string(operations.StatusDrafting),
		cancel:          cancel,
	}
	svc := operations.NewService(repo)
	created, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID:   "user-1",
		WorkspaceID:   "local",
		Title:         "Prepare an internal note",
		OperationType: "draft",
		SourceType:    "manual",
		DedupeKey:     "draft-cancel-recovery",
	})
	if err != nil {
		t.Fatalf("ingest operation: %v", err)
	}
	created.Operation.CurrentDecision = string(operations.DecisionCreateDraft)
	created.Operation.RecommendedAction = "Prepare a short internal note"
	classified, err := svc.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("transition to classified: %v", err)
	}
	claimed, err := svc.ClaimNext(context.Background(), "user-1", "local", uuid.New(), operationClaimLease)
	if err != nil || claimed == nil || claimed.Operation.ID != classified.ID {
		t.Fatalf("claim operation: claim=%#v err=%v", claimed, err)
	}
	w := New(svc, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})
	if err := w.runDraft(ctx, claimed.Operation, claimed.Claim, &Report{}, autonomypolicy.Decision{RecommendedAction: created.Operation.RecommendedAction}); err != nil {
		t.Fatalf("runDraft: %v", err)
	}
	stored, err := svc.Get("user-1", "local", created.Operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if stored.Status != string(operations.StatusDraftReady) || stored.ResultSummary != created.Operation.RecommendedAction {
		t.Fatalf("draft state = %q with summary %q; want draft_ready with persisted summary", stored.Status, stored.ResultSummary)
	}
}

func TestRunOnceRecoversDraftAfterDraftReadyPersistenceFailure(t *testing.T) {
	baseRepository := operations.NewMemoryRepository()
	repo := &failStatusUpdateOnceRepository{
		Repository:      baseRepository,
		ClaimRepository: baseRepository,
		status:          string(operations.StatusDraftReady),
	}
	svc := operations.NewService(repo)
	created, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID:   "user-1",
		WorkspaceID:   "local",
		Title:         "Prepare an internal note",
		OperationType: "draft",
		SourceType:    "manual",
		DedupeKey:     "draft-retry-after-write-failure",
	})
	if err != nil {
		t.Fatalf("ingest operation: %v", err)
	}
	created.Operation.CurrentDecision = string(operations.DecisionCreateDraft)
	created.Operation.RecommendedAction = "Prepare a concise internal note"
	classified, err := svc.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("transition to classified: %v", err)
	}
	worker := New(svc, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})

	first, err := worker.RunOnce(context.Background())
	if !errors.Is(err, ErrReportedFailures) {
		t.Fatalf("first RunOnce error = %v, want reported persistence failure", err)
	}
	if len(first.Errors) != 1 || !strings.Contains(first.Errors[0], "simulated transient update failure") {
		t.Fatalf("first run errors = %v, want the injected DraftReady persistence failure", first.Errors)
	}
	partial, err := svc.Get("user-1", "local", classified.ID)
	if err != nil {
		t.Fatalf("load interrupted draft: %v", err)
	}
	if partial.Status != string(operations.StatusDrafting) {
		t.Fatalf("status after transient failure = %q, want drafting", partial.Status)
	}

	second, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("recovery RunOnce: %v", err)
	}
	if second.Drafted != 1 || len(second.Errors) != 0 {
		t.Fatalf("recovery report = %#v, want one completed draft and no errors", second)
	}
	recovered, err := svc.Get("user-1", "local", classified.ID)
	if err != nil {
		t.Fatalf("load recovered draft: %v", err)
	}
	if recovered.Status != string(operations.StatusDraftReady) || recovered.ResultSummary != created.Operation.RecommendedAction {
		t.Fatalf("recovered draft = status %q summary %q; want draft_ready with persisted recommendation", recovered.Status, recovered.ResultSummary)
	}
}

type blockingReader struct {
	started      chan struct{}
	continueRead chan struct{}
}

func (r blockingReader) Feed() accountfeed.Feed {
	return accountfeed.Feed{ID: uuid.New(), Name: "blocking", OwnerUserID: "user-1", WorkspaceID: "local", Enabled: true}
}

func (r blockingReader) Read(ctx context.Context) ([]accountfeed.FeedItem, error) {
	close(r.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.continueRead:
		return nil, nil
	}
}

type cancelOnStatusRepository struct {
	operations.Repository
	operations.ClaimRepository
	status string
	cancel context.CancelFunc
}

type failStatusUpdateOnceRepository struct {
	operations.Repository
	operations.ClaimRepository
	status string
	failed bool
}

func (r *failStatusUpdateOnceRepository) Update(op *models.Operation) (*models.Operation, error) {
	if op.Status == r.status && !r.failed {
		r.failed = true
		return nil, errors.New("simulated transient update failure")
	}
	return r.Repository.Update(op)
}

func (r *failStatusUpdateOnceRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if op.Status == r.status && !r.failed {
		r.failed = true
		return nil, errors.New("simulated transient update failure")
	}
	return r.ClaimRepository.TransitionClaimed(ctx, claim, op, event, release)
}

type cancellationAuthorization struct{}

func (cancellationAuthorization) Authorize(ctx context.Context, _ executionauth.Request) (executionauth.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return executionauth.Receipt{}, err
	}
	return executionauth.Receipt{}, errors.New("unexpected authorization request")
}

func (cancellationAuthorization) AuthorizeAndConsume(ctx context.Context, _ executionauth.Request, _, _ string) (executionauth.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return executionauth.Receipt{}, err
	}
	return executionauth.Receipt{}, errors.New("unexpected authorization consumption")
}

func (cancellationAuthorization) Get(ctx context.Context, _ string, _ uuid.UUID) (executionauth.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return executionauth.Receipt{}, err
	}
	return executionauth.Receipt{}, errors.New("unexpected authorization lookup")
}

func (r *cancelOnStatusRepository) Update(op *models.Operation) (*models.Operation, error) {
	updated, err := r.Repository.Update(op)
	if err == nil && op.Status == r.status {
		r.cancel()
	}
	return updated, err
}

func (r *cancelOnStatusRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	updated, err := r.ClaimRepository.TransitionClaimed(ctx, claim, op, event, release)
	if err == nil && op.Status == r.status {
		r.cancel()
	}
	return updated, err
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

func TestPausedModeIngestsButProcessesNothing(t *testing.T) {
	w, svc, _ := buildWorker(t, autonomypolicy.ModePaused, twoItemFeed)
	rep, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.OperationsCreated != 2 {
		t.Fatalf("paused mode should still record ingested items, got %d", rep.OperationsCreated)
	}
	if rep.Classified != 0 || rep.AutoExecuted != 0 || rep.Drafted != 0 || rep.Verified != 0 {
		t.Fatalf("paused mode must not process operations: %+v", rep)
	}
	newOperations, err := svc.List(operations.Filter{
		OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusNew,
	})
	if err != nil || len(newOperations) != 2 {
		t.Fatalf("paused mode should leave ingested operations new: count=%d err=%v", len(newOperations), err)
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

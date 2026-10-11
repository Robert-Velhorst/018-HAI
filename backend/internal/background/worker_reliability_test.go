package background

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func TestRunOnceDoesNotLetUnprocessableDueOperationsStarveNewWork(t *testing.T) {
	feed := `[{"externalId":"fresh","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"}]`
	worker, service, _ := buildWorker(t, autonomypolicy.ModeAutonomousSafe, feed)
	worker.opts.MaxOps = 1

	failed, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID:   "user-1",
		WorkspaceID:   "local",
		Title:         "Prior failed operation",
		OperationType: "organize",
		SourceType:    "manual",
		DedupeKey:     "older-failed-operation",
	})
	if err != nil {
		t.Fatalf("ingest prior operation: %v", err)
	}
	failed.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	failed.Operation.VerificationStatus = string(operations.VerificationPending)
	classified, err := service.Transition(failed.Operation, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("classify prior operation: %v", err)
	}
	ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready")
	if err != nil {
		t.Fatalf("ready prior operation: %v", err)
	}
	running, err := service.Transition(*ready, operations.StatusRunning, "hai", "", "running")
	if err != nil {
		t.Fatalf("start prior operation: %v", err)
	}
	if _, err := service.Transition(*running, operations.StatusFailed, "hai", "", "failed"); err != nil {
		t.Fatalf("fail prior operation: %v", err)
	}

	report, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.OperationsCreated != 1 || report.AwaitingApproval != 1 || report.Verified != 0 {
		t.Fatalf("fresh source operation was not routed to its required approval: %#v", report)
	}
	awaitingApproval, err := service.List(operations.Filter{
		OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusAwaitingApproval,
	})
	if err != nil {
		t.Fatalf("list operations awaiting approval: %v", err)
	}
	if len(awaitingApproval) != 1 || awaitingApproval[0].Title != "Organize workspace notes" {
		t.Fatalf("fresh source operation was starved or executed without approval: %#v", awaitingApproval)
	}
	remaining, err := service.List(operations.Filter{
		OwnerUserID: "user-1", WorkspaceID: "local", Status: operations.StatusNew,
	})
	if err != nil {
		t.Fatalf("list remaining new operations: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("new operation remains unprocessed: %#v", remaining)
	}
}

func TestReadySafeExecutionRechecksCurrentPolicyBeforeExecution(t *testing.T) {
	tests := []struct {
		name          string
		mode          autonomypolicy.Mode
		emergencyStop bool
		blocked       bool
	}{
		{name: "read only", mode: autonomypolicy.ModeReadOnly},
		{name: "paused", mode: autonomypolicy.ModePaused},
		{name: "draft only", mode: autonomypolicy.ModeDraftOnly},
		{name: "emergency stop", mode: autonomypolicy.ModeAutonomousSafe, emergencyStop: true},
		{name: "new operator block rule", mode: autonomypolicy.ModeAutonomousSafe, blocked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			created, err := service.Ingest(operations.NewOperationInput{
				OwnerUserID: "user-1", WorkspaceID: "local", Title: "Organize workspace notes",
				Description: "Create an internal note in the workspace", OperationType: "organize",
				SourceType: "manual", DedupeKey: "ready-policy-recheck-" + test.name,
			})
			if err != nil {
				t.Fatalf("ingest operation: %v", err)
			}
			op := created.Operation
			op.RiskLevel = string(operations.RiskLow)
			op.AutonomyLevel = string(operations.AutonomyAuto)
			op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
			op.OwnerType = string(operations.OwnerHAI)
			op.RequiresApproval = false
			op.VerificationStatus = string(operations.VerificationPending)
			classified, err := service.Transition(op, operations.StatusClassified, "hai", "", "classified under autonomous-safe mode")
			if err != nil {
				t.Fatalf("classify operation: %v", err)
			}
			ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready under autonomous-safe mode")
			if err != nil {
				t.Fatalf("ready operation: %v", err)
			}

			worker := New(service, nil, nil, Options{
				OwnerUserID: "user-1", WorkspaceID: "local", Mode: autonomypolicy.ModeAutonomousSafe,
			}).WithControl(fixedWorkerControl{mode: test.mode, emergencyStop: test.emergencyStop})
			if test.blocked {
				worker.WithBlockRules(fixedWorkerBlockRules(true))
			}
			report, err := worker.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("RunOnce should defer safely under the current policy: %v", err)
			}
			wantDeferred := 1
			if test.emergencyStop {
				wantDeferred = 0 // RunOnce exits before claiming any operation under emergency stop.
			}
			if report.AutoExecuted != 0 || report.DeferredByPolicy != wantDeferred {
				t.Fatalf("RunOnce report = %#v, want no execution and %d policy deferrals", report, wantDeferred)
			}
			stored, err := service.Get("user-1", "local", ready.ID)
			if err != nil {
				t.Fatalf("load operation: %v", err)
			}
			if stored.Status != string(operations.StatusReady) {
				t.Fatalf("operation advanced to %q while execution was disallowed", stored.Status)
			}
			if !test.emergencyStop && (stored.NextReviewAt == nil || !stored.NextReviewAt.After(time.Now().UTC())) {
				t.Fatal("policy deferral did not postpone the next review")
			}
			claimed, err := service.ClaimNext(context.Background(), "user-1", "local", uuid.New(), operationClaimLease)
			if err != nil {
				t.Fatalf("claim deferred operation: %v", err)
			}
			if !test.emergencyStop {
				if claimed != nil {
					t.Fatalf("deferred operation is immediately claimable: %#v", claimed)
				}
				return
			}
			if claimed == nil || claimed.Operation.ID != ready.ID {
				t.Fatalf("emergency-stop operation claim = %#v, want untouched operation %s", claimed, ready.ID)
			}
			if err := service.ReleaseClaim(context.Background(), claimed.Claim); err != nil {
				t.Fatalf("release verification claim: %v", err)
			}
		})
	}
}

type fixedWorkerControl struct {
	mode          autonomypolicy.Mode
	emergencyStop bool
}

func (c fixedWorkerControl) Mode() autonomypolicy.Mode { return c.mode }
func (c fixedWorkerControl) EmergencyStop() bool       { return c.emergencyStop }

type fixedWorkerBlockRules bool

func (rules fixedWorkerBlockRules) ShouldBlock(string, string) (bool, string) {
	return bool(rules), "operator rule changed after classification"
}

func TestRunOnceSurfacesLedgerReadFailureAlongsideReport(t *testing.T) {
	repositoryErr := errors.New("simulated ledger read failure")
	base := operations.NewMemoryRepository()
	repository := &failClaimRepository{
		Repository:      base,
		ClaimRepository: base,
		err:             repositoryErr,
	}
	service := operations.NewService(repository)
	worker := New(service, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})

	report, err := worker.RunOnce(context.Background())
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("RunOnce error = %v, want wrapped ledger error", err)
	}
	if len(report.Errors) != 1 || report.Errors[0] == "" {
		t.Fatalf("RunOnce report errors = %v, want the ledger failure retained", report.Errors)
	}
}

func TestRunOnceSurfacesFeedFailureAlongsideReport(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	worker := New(service, nil, []accountfeed.Reader{failingFeedReader{}}, Options{
		OwnerUserID: "user-1", WorkspaceID: "local",
	})

	report, err := worker.RunOnce(context.Background())
	if !errors.Is(err, ErrReportedFailures) {
		t.Fatalf("RunOnce error = %v, want reported feed failure", err)
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "test-feed") {
		t.Fatalf("RunOnce report errors = %v, want feed failure retained", report.Errors)
	}
}

func TestExecuteSafeOperationRejectsInconsistentApprovalOrRiskState(t *testing.T) {
	tests := []struct {
		name             string
		risk             operations.RiskLevel
		autonomy         operations.AutonomyLevel
		owner            operations.OwnerType
		requiresApproval bool
	}{
		{name: "approval required", risk: operations.RiskLow, autonomy: operations.AutonomyAuto, owner: operations.OwnerHAI, requiresApproval: true},
		{name: "high risk", risk: operations.RiskHigh, autonomy: operations.AutonomyAuto, owner: operations.OwnerHAI},
		{name: "non-automatic autonomy", risk: operations.RiskLow, autonomy: operations.AutonomyApproval, owner: operations.OwnerHAI},
		{name: "non-HAI owner", risk: operations.RiskLow, autonomy: operations.AutonomyAuto, owner: operations.OwnerRobert},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			created, err := service.Ingest(operations.NewOperationInput{
				OwnerUserID: "user-1", WorkspaceID: "local", Title: "Internal work",
				OperationType: "organize", SourceType: "manual", DedupeKey: "policy-" + test.name,
			})
			if err != nil {
				t.Fatalf("ingest operation: %v", err)
			}
			created.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
			created.Operation.RiskLevel = string(test.risk)
			created.Operation.AutonomyLevel = string(test.autonomy)
			created.Operation.OwnerType = string(test.owner)
			created.Operation.RequiresApproval = test.requiresApproval
			created.Operation.VerificationStatus = string(operations.VerificationPending)
			classified, err := service.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified")
			if err != nil {
				t.Fatalf("classify operation: %v", err)
			}
			ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready")
			if err != nil {
				t.Fatalf("ready operation: %v", err)
			}

			if _, err := ExecuteSafeOperation(context.Background(), service, nil, *ready, time.Now().UTC()); err == nil {
				t.Fatal("inconsistent approval/risk metadata was accepted")
			}
			stored, err := service.Get("user-1", "local", created.Operation.ID)
			if err != nil {
				t.Fatalf("load operation: %v", err)
			}
			if stored.Status != string(operations.StatusReady) {
				t.Fatalf("rejected operation advanced to %q; want ready with no execution attempt", stored.Status)
			}
		})
	}
}

func TestRunOnceDoesNotFinalizeDraftingOperationWithWrongDecision(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	created, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID: "user-1", WorkspaceID: "local", Title: "Prepare an internal note",
		OperationType: "draft", SourceType: "manual", DedupeKey: "draft-policy-mismatch",
	})
	if err != nil {
		t.Fatalf("ingest operation: %v", err)
	}
	created.Operation.CurrentDecision = string(operations.DecisionCreateDraft)
	created.Operation.AutonomyLevel = string(operations.AutonomyDraft)
	drafting, err := service.Transition(created.Operation, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("classify operation: %v", err)
	}
	drafting, err = service.Transition(*drafting, operations.StatusDrafting, "hai", "", "drafting")
	if err != nil {
		t.Fatalf("start draft: %v", err)
	}
	drafting.CurrentDecision = string(operations.DecisionObserveOnly)
	if _, err := service.Save(*drafting, "test_policy_mismatch", "test", "simulate inconsistent persisted decision"); err != nil {
		t.Fatalf("persist mismatched decision: %v", err)
	}
	worker := New(service, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})

	report, err := worker.RunOnce(context.Background())
	if !errors.Is(err, ErrReportedFailures) || len(report.Errors) != 1 {
		t.Fatalf("RunOnce = (%#v, %v), want reported policy mismatch", report, err)
	}
	stored, err := service.Get("user-1", "local", created.Operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if stored.Status != string(operations.StatusDrafting) {
		t.Fatalf("mismatched operation advanced to %q; want drafting for review", stored.Status)
	}
}

type failClaimRepository struct {
	operations.Repository
	operations.ClaimRepository
	err error
}

func (r *failClaimRepository) ClaimNext(context.Context, string, string, uuid.UUID, time.Duration) (*operations.ClaimedOperation, error) {
	return nil, r.err
}

type failingFeedReader struct{}

func (failingFeedReader) Feed() accountfeed.Feed {
	return accountfeed.Feed{ID: uuid.New(), Name: "test-feed", Enabled: true}
}

func (failingFeedReader) Read(context.Context) ([]accountfeed.FeedItem, error) {
	return nil, errors.New("simulated feed read failure")
}

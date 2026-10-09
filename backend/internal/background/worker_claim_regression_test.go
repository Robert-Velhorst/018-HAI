package background

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func createReadyRegressionOperation(t *testing.T, service *operations.Service) models.Operation {
	t.Helper()
	created, err := service.Ingest(operations.NewOperationInput{OwnerUserID: "user-1", WorkspaceID: "local", Title: "Organize older workspace notes", OperationType: "organize", SourceType: "manual", DedupeKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	op := created.Operation
	op.RiskLevel = string(operations.RiskLow)
	op.AutonomyLevel = string(operations.AutonomyAuto)
	op.OwnerType = string(operations.OwnerHAI)
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.VerificationStatus = string(operations.VerificationPending)
	op.RequiresApproval = false
	classified, err := service.Transition(op, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready")
	if err != nil {
		t.Fatal(err)
	}
	return *ready
}

type olderOnlyBlockRule struct{}

func (olderOnlyBlockRule) ShouldBlock(_ string, title string) (bool, string) {
	return strings.Contains(title, "older"), "operator blocked this item"
}

func TestPolicyDeferralDoesNotStarveOtherWorkAcrossPasses(t *testing.T) {
	for _, maxOps := range []int{1, 2} {
		t.Run(map[int]string{1: "one item per pass", 2: "two items per pass"}[maxOps], func(t *testing.T) {
			worker, _, workspace := buildWorker(t, autonomypolicy.ModeAutonomousSafe, `[]`)
			repo := operations.NewMemoryRepository()
			service := operations.NewService(repo)
			worker.svc = service
			worker.opts.MaxOps = maxOps
			worker.WithBlockRules(olderOnlyBlockRule{})
			// Seed a genuinely older record; Windows clock ticks can make two
			// successive intakes tie and put fresh work first by UUID.
			seed, err := operations.NewOperation(operations.NewOperationInput{
				OwnerUserID: "user-1", WorkspaceID: "local", Title: "Organize older workspace notes",
				OperationType: "organize", SourceType: "manual", DedupeKey: "older",
			}, time.Now().UTC().Add(-time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			seed.Status = string(operations.StatusReady)
			seed.AutonomyLevel = string(operations.AutonomyAuto)
			seed.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
			seed.VerificationStatus = string(operations.VerificationPending)
			older, err := repo.Create(&seed)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := service.Ingest(operations.NewOperationInput{OwnerUserID: "user-1", WorkspaceID: "local", Title: "Organize fresh workspace notes", OperationType: "organize", SourceType: "manual", DedupeKey: "fresh"})
			if err != nil {
				t.Fatal(err)
			}
			first, err := worker.RunOnce(context.Background())
			if err != nil || first.DeferredByPolicy != 1 {
				t.Fatalf("first pass = %#v, %v", first, err)
			}
			if maxOps == 1 {
				second, err := worker.RunOnce(context.Background())
				if err != nil || second.Verified != 1 {
					t.Fatalf("second pass = %#v, %v; fresh work was starved", second, err)
				}
			} else if first.Verified != 1 {
				t.Fatalf("first pass did not continue to fresh work: %#v", first)
			}
			stored, _ := service.Get("user-1", "local", fresh.Operation.ID)
			deferred, _ := service.Get("user-1", "local", older.ID)
			if stored.Status != string(operations.StatusCompleted) || deferred.Status != string(operations.StatusReady) || deferred.NextReviewAt == nil {
				t.Fatalf("fresh=%#v deferred=%#v", stored, deferred)
			}
			files, err := os.ReadDir(workspace)
			if err != nil || len(files) != 1 {
				t.Fatalf("artifacts=%d err=%v; want only permitted work", len(files), err)
			}
		})
	}
}

type failedDeferralRepository struct {
	operations.Repository
	operations.ClaimRepository
	releaseErr error
	saveErr    error
}

func (r *failedDeferralRepository) ReleaseClaim(ctx context.Context, claim operations.ExecutionClaim) error {
	if r.releaseErr != nil {
		return r.releaseErr
	}
	return r.ClaimRepository.ReleaseClaim(ctx, claim)
}
func (r *failedDeferralRepository) Update(op *models.Operation) (*models.Operation, error) {
	if op.NextReviewAt != nil && r.saveErr != nil {
		return nil, r.saveErr
	}
	return r.Repository.Update(op)
}

func TestPolicyDeferralPersistenceFailuresAreReported(t *testing.T) {
	for _, step := range []string{"release", "save"} {
		t.Run(step, func(t *testing.T) {
			base := operations.NewMemoryRepository()
			repo := &failedDeferralRepository{Repository: base, ClaimRepository: base}
			service := operations.NewService(repo)
			ready := createReadyRegressionOperation(t, service)
			injected := errors.New("injected deferral persistence failure")
			if step == "release" {
				repo.releaseErr = injected
			} else {
				repo.saveErr = injected
			}
			worker := New(service, nil, nil, Options{OwnerUserID: "user-1", WorkspaceID: "local", Mode: autonomypolicy.ModeReadOnly})
			report, err := worker.RunOnce(context.Background())
			if !errors.Is(err, injected) || !errors.Is(err, ErrReportedFailures) || len(report.Errors) != 1 || report.DeferredByPolicy != 0 || report.AutoExecuted != 0 {
				t.Fatalf("report=%#v err=%v", report, err)
			}
			stored, _ := service.Get("user-1", "local", ready.ID)
			if stored.Status != string(operations.StatusReady) || stored.NextReviewAt != nil {
				t.Fatalf("failed deferral mutated state: %#v", stored)
			}
		})
	}
}

type heartbeatLossRepository struct {
	operations.Repository
	operations.ClaimRepository
	replacement *operations.ClaimedOperation
	renewErr    error
}

func (r *heartbeatLossRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if op.Status == string(operations.StatusRunning) {
		<-ctx.Done()
		// Exercise the actual repository fence even if the stale worker ignores cancellation.
		if r.renewErr == nil {
			return r.ClaimRepository.TransitionClaimed(context.Background(), claim, op, event, release)
		}
		return nil, ctx.Err()
	}
	return r.ClaimRepository.TransitionClaimed(ctx, claim, op, event, release)
}
func (r *heartbeatLossRepository) RenewClaim(ctx context.Context, claim operations.ExecutionClaim, lease time.Duration) error {
	if r.renewErr != nil {
		return r.renewErr
	}
	if err := r.ClaimRepository.ReleaseClaim(ctx, claim); err != nil {
		return err
	}
	replacement, err := r.ClaimRepository.ClaimNext(ctx, claim.OwnerUserID, claim.WorkspaceID, uuid.New(), operationClaimLease)
	if err != nil {
		return err
	}
	r.replacement = replacement
	return r.ClaimRepository.RenewClaim(ctx, claim, lease)
}

func TestBackgroundHeartbeatFailureCancelsAndFencesExecution(t *testing.T) {
	for _, loseOwnership := range []bool{false, true} {
		t.Run(map[bool]string{false: "renewal error", true: "replacement generation"}[loseOwnership], func(t *testing.T) {
			base := operations.NewMemoryRepository()
			repo := &heartbeatLossRepository{Repository: base, ClaimRepository: base}
			wantErr := operations.ErrClaimLost
			if !loseOwnership {
				wantErr = errors.New("renewal transport failed")
				repo.renewErr = wantErr
			}
			service := operations.NewService(repo)
			ready := createReadyRegressionOperation(t, service)
			workspace := t.TempDir()
			worker := New(service, newAuthorizedBackgroundTestBroker(t, workspace, "user-1", "local"), nil, Options{OwnerUserID: "user-1", WorkspaceID: "local"})
			worker.claimLease = 300 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			report, err := worker.RunOnce(ctx)
			if !errors.Is(err, wantErr) || !errors.Is(err, ErrReportedFailures) || len(report.Errors) != 1 || report.Verified != 0 {
				t.Fatalf("report=%#v err=%v; want heartbeat failure", report, err)
			}
			stored, _ := service.Get("user-1", "local", ready.ID)
			if stored.Status != string(operations.StatusReady) {
				t.Fatalf("stale worker advanced state: %#v", stored)
			}
			if loseOwnership && (repo.replacement == nil || repo.replacement.Claim.Generation <= 1) {
				t.Fatal("test did not replace ownership")
			}
			files, err := os.ReadDir(workspace)
			if err != nil || len(files) != 0 {
				t.Fatalf("stale worker wrote %d artifacts: %v", len(files), err)
			}
		})
	}
}

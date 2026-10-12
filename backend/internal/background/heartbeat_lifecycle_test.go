package background

import (
	"context"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

type panicHeartbeatRepository struct {
	*operations.MemoryRepository
	entered chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (r *panicHeartbeatRepository) RenewClaim(ctx context.Context, _ operations.ExecutionClaim, _ time.Duration) error {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	close(r.stopped)
	return ctx.Err()
}

func (r *panicHeartbeatRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if op.Status == string(operations.StatusRunning) {
		<-r.entered
		panic("synthetic process panic")
	}
	return r.MemoryRepository.TransitionClaimed(ctx, claim, op, event, release)
}

func TestBackgroundPanicCancelsAndJoinsInFlightHeartbeat(t *testing.T) {
	base := operations.NewMemoryRepository()
	repo := &panicHeartbeatRepository{MemoryRepository: base, entered: make(chan struct{}), stopped: make(chan struct{})}
	svc := operations.NewService(repo)
	createReadyRegressionOperation(t, svc)
	claimed, err := base.ClaimNext(context.Background(), "user-1", "local", uuid.New(), operationClaimLease)
	if err != nil || claimed == nil {
		t.Fatalf("claim fixture: %v", err)
	}
	worker := New(svc, newAuthorizedBackgroundTestBroker(t, t.TempDir(), "user-1", "local"), nil,
		Options{OwnerUserID: "user-1", WorkspaceID: "local", Mode: autonomypolicy.ModeAutonomousSafe})
	worker.claimLease = 30 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	func() {
		defer func() {
			if recover() != "synthetic process panic" {
				t.Error("panic was swallowed or changed")
			}
		}()
		_ = worker.processClaimed(ctx, *claimed, &Report{})
	}()
	select {
	case <-repo.stopped:
	default:
		t.Fatal("panic returned before the claim heartbeat joined")
	}
}

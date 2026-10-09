package background

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/opscontrol"

	"github.com/google/uuid"
)

// Explicit synthetic permission only for tests unrelated to live controls.
func allowSyntheticSafePolicy(models.Operation) bool { return true }

type finalPolicyBarrierRepository struct {
	*operations.MemoryRepository
	entered chan struct{}
	proceed chan struct{}
}

func (r *finalPolicyBarrierRepository) WithClaimedSafeEffect(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, effect func(context.Context) error) error {
	close(r.entered)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.proceed:
	}
	return r.MemoryRepository.WithClaimedSafeEffect(ctx, claim, op, effect)
}

func TestRealSafeExecutionRequiresExactlyOneFinalPolicy(t *testing.T) {
	for _, policies := range [][]SafeExecutionPolicy{nil, {nil}, {allowSyntheticSafePolicy, allowSyntheticSafePolicy}} {
		service := operations.NewService(operations.NewMemoryRepository())
		op := createReadyRegressionOperation(t, service)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		claim, err := service.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		workspace := filepath.Join(t.TempDir(), "not-created")
		broker := newAuthorizedBackgroundTestBroker(t, workspace, op.OwnerUserID, op.WorkspaceID)
		out, err := ExecuteSafeOperationClaimed(ctx, service, broker, claim.Operation, claim.Claim, time.Now(), policies...)
		cancel()
		if !errors.Is(err, ErrSafeExecutionPolicyUnavailable) || out.Receipt != nil {
			t.Fatalf("missing/ambiguous live policy reached dispatch: %+v / %v", out, err)
		}
		stored, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
		if err != nil || stored.Version != op.Version || stored.Status != op.Status {
			t.Fatalf("policy admission mutated operation: %+v / %v", stored, err)
		}
		if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("policy admission touched workspace: %v", err)
		}
	}
}

func TestFinalPolicyRechecksModeAfterEarlierAdmission(t *testing.T) {
	repo := &finalPolicyBarrierRepository{MemoryRepository: operations.NewMemoryRepository(), entered: make(chan struct{}), proceed: make(chan struct{})}
	service := operations.NewService(repo)
	op := createReadyRegressionOperation(t, service)
	control := opscontrol.NewController(t.TempDir())
	policy := func(models.Operation) bool {
		return control.Mode() == autonomypolicy.ModeAutonomousSafe && !control.EmergencyStop()
	}
	if !policy(op) {
		t.Fatal("initial policy did not allow the operation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	claim, err := service.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "not-created")
	broker := newAuthorizedBackgroundTestBroker(t, workspace, op.OwnerUserID, op.WorkspaceID)
	type result struct {
		out SafeOutcome
		err error
	}
	done := make(chan result, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(repo.proceed) }) }
	defer unblock()
	go func() {
		out, err := ExecuteSafeOperationClaimed(ctx, service, broker, claim.Operation, claim.Claim, time.Now(), policy)
		done <- result{out, err}
	}()
	select {
	case <-repo.entered:
	case <-ctx.Done():
		t.Fatal("execution never reached the final boundary")
	}
	if _, err := control.SetMode(autonomypolicy.ModeReadOnly); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case got := <-done:
		if got.err != nil || !got.out.Failed || got.out.Verified || got.out.Interrupted || got.out.Receipt == nil || got.out.Receipt.Output.Progress.EffectStarted {
			t.Fatalf("changed mode crossed the final boundary: %+v / %v", got.out, got.err)
		}
		if got.out.Operation == nil || got.out.Operation.LastError != ErrSafeExecutionPolicyChanged.Error() {
			t.Fatalf("policy denial lost its actual reason: %+v", got.out.Operation)
		}
	case <-ctx.Done():
		t.Fatal("policy refusal did not finish")
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only mode produced a workspace effect: %v", err)
	}
	_, evidence := storedSafeIntent(t, service, op)
	if evidence.OutcomeUncertain || !evidence.BeforeEffect || evidence.Result.Output.Progress.Authorization != "" {
		t.Fatalf("pre-effect denial falsely consumed authority or permitted uncertain replay: %+v", evidence)
	}
}

func TestFinalEffectHoldsFenceUntilFilesystemWorkReturns(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	op := createReadyRegressionOperation(t, service)
	control := opscontrol.NewController(t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	claim, err := service.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker := newAuthorizedBackgroundTestBroker(t, workspace, op.OwnerUserID, op.WorkspaceID)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(proceed) }) }
	defer unblock()
	policy := func(models.Operation) bool {
		allowed := control.Mode() == autonomypolicy.ModeAutonomousSafe
		close(entered)
		select {
		case <-proceed:
			return allowed
		case <-ctx.Done():
			return false
		}
	}
	type result struct {
		out SafeOutcome
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := ExecuteSafeOperationClaimed(ctx, service, broker, claim.Operation, claim.Claim, time.Now(), policy)
		done <- result{out, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("policy was not reached")
	}
	mutation := make(chan error, 1)
	started := make(chan struct{})
	go func() { close(started); _, err := control.SetMode(autonomypolicy.ModeReadOnly); mutation <- err }()
	<-started
	select {
	case err := <-mutation:
		t.Fatalf("mode mutation crossed an active effect fence: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case got := <-done:
		if got.err != nil || !got.out.Verified || got.out.Receipt == nil || !got.out.Receipt.Output.Progress.FileClosed {
			t.Fatalf("admitted effect did not finish: %+v / %v", got.out, got.err)
		}
	case <-ctx.Done():
		t.Fatal("admitted effect did not finish")
	}
	select {
	case err := <-mutation:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("mode mutation did not finish")
	}
	if control.Mode() != autonomypolicy.ModeReadOnly {
		t.Fatal("restrictive mode was not retained")
	}
	if _, err := os.Stat(filepath.Join(workspace, safePayload(op).ArtifactName)); err != nil {
		t.Fatal("admitted effect did not create its artifact", err)
	}
}

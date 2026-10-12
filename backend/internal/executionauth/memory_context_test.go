package executionauth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryContextFixture struct {
	receipt     Receipt
	consumption Consumption
	exercise    FinalEffectExercise
}

func newMemoryContextFixture(t *testing.T) memoryContextFixture {
	t.Helper()
	repository := NewMemoryRepository()
	now := fixedNow()
	sourceID := "task-review:216967e4-d62e-4a73-ae3f-c62efcbf78f5"
	bindingDigest := strings.Repeat("a", 64)
	service := newTestService(t, repository, permissiveConstitution(), fakeApprovalResolver{
		values: map[string]ResolvedApproval{
			"alice\x00" + sourceID: {
				SourceID: sourceID, DecisionID: "a981bcb2-c57d-4f08-9504-91be0f20d287",
				DecisionDigest: strings.Repeat("b", 64), BindingDigest: bindingDigest,
				ApprovedBy: "alice", ApprovedAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute),
			},
		},
	}, nil)
	effectRequest, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes", "task-1", "alice", "project-1", "prepare a bounded result", sourceID, true,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	effectDigest, err := FinalEffectDigest(effectRequest)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	request := baseRequest("memory-context-receipt")
	request.Action = AgentRuntimeExecuteAction
	request.ResourceType = AgentRuntimeResourceType
	request.ResourceID = effectRequest.TaskID
	request.ProjectKey = effectRequest.ProjectKey
	request.RuntimeID = effectRequest.RuntimeID
	request.EffectDigest = effectDigest
	request.ApprovalSourceID = sourceID
	request.ApprovalBindingDigest = bindingDigest
	target, err := FinalEffectExecutionTarget(effectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	// As in the final-effect fixture, exercise the memory repository independently
	// of the production PostgreSQL task-review transaction wrapper.
	receipt, err := service.authorizeAndConsumeCore(context.Background(), request, "automation-runtime-handoff", target)
	if err != nil || receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("authorize fixture: outcome=%s error=%v", receipt.Outcome, err)
	}
	consumption, err := repository.GetConsumption(context.Background(), receipt.OwnerIdentity, receipt.ID)
	if err != nil {
		t.Fatalf("GetConsumption fixture: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(context.Background(), effectRequest, receipt.ID)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect fixture: %v", err)
	}
	exercise := finalEffectExercise(effectRequest, proofFromBinding(t, binding, effectDigest), now)
	if err := repository.ExerciseFinalEffect(context.Background(), exercise); err != nil {
		t.Fatalf("ExerciseFinalEffect fixture: %v", err)
	}
	return memoryContextFixture{receipt: receipt, consumption: consumption, exercise: exercise}
}

type memoryContextMethod struct {
	name string
	call func(context.Context, *MemoryRepository, memoryContextFixture) error
}

func memoryContextMethods() []memoryContextMethod {
	return []memoryContextMethod{
		{"CreateOrGet", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			_, _, err := r.CreateOrGet(ctx, f.receipt)
			return err
		}},
		{"Get", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			_, err := r.Get(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
			return err
		}},
		{"List", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			_, err := r.List(ctx, f.receipt.OwnerIdentity, 10)
			return err
		}},
		{"Consume", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			return r.Consume(ctx, f.consumption)
		}},
		{"GetConsumption", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			_, err := r.GetConsumption(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
			return err
		}},
		{"ExerciseFinalEffect", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			return r.ExerciseFinalEffect(ctx, f.exercise)
		}},
		{"GetFinalEffectExercise", func(ctx context.Context, r *MemoryRepository, f memoryContextFixture) error {
			_, err := r.GetFinalEffectExercise(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
			return err
		}},
	}
}

func memoryContextRepository(t *testing.T, name string, f memoryContextFixture) *MemoryRepository {
	t.Helper()
	r := NewMemoryRepository()
	if name == "CreateOrGet" {
		return r
	}
	if _, created, err := r.CreateOrGet(context.Background(), f.receipt); err != nil || !created {
		t.Fatalf("seed receipt: created=%t error=%v", created, err)
	}
	if name == "GetConsumption" || name == "ExerciseFinalEffect" || name == "GetFinalEffectExercise" {
		if err := r.Consume(context.Background(), f.consumption); err != nil {
			t.Fatalf("seed consumption: %v", err)
		}
	}
	if name == "GetFinalEffectExercise" {
		if err := r.ExerciseFinalEffect(context.Background(), f.exercise); err != nil {
			t.Fatalf("seed exercise: %v", err)
		}
	}
	return r
}

type memoryContextState struct {
	receipts       map[string]Receipt
	byID           map[string]string
	consumptions   map[string]Consumption
	approvalClaims map[string]uuid.UUID
	exercises      map[string]FinalEffectExercise
}

func memoryContextSnapshot(r *MemoryRepository) memoryContextState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := memoryContextState{
		receipts: map[string]Receipt{}, byID: map[string]string{}, consumptions: map[string]Consumption{},
		approvalClaims: map[string]uuid.UUID{}, exercises: map[string]FinalEffectExercise{},
	}
	for k, v := range r.receipts {
		s.receipts[k] = cloneReceipt(v)
	}
	for k, v := range r.byID {
		s.byID[k] = v
	}
	for k, v := range r.consumptions {
		s.consumptions[k] = v
	}
	for k, v := range r.approvalClaims {
		s.approvalClaims[k] = v
	}
	for k, v := range r.exercises {
		s.exercises[k] = v
	}
	return s
}

type memoryContextResult struct {
	err        error
	panicValue any
}

func memoryContextCall(t *testing.T, r *MemoryRepository, held bool, cancelWhileHeld context.CancelFunc, call func() error) memoryContextResult {
	t.Helper()
	if held {
		r.mu.Lock()
		defer func() {
			if held {
				r.mu.Unlock()
			}
		}()
	}
	started := make(chan struct{})
	done := make(chan memoryContextResult, 1)
	go func() {
		result := memoryContextResult{}
		defer func() {
			result.panicValue = recover()
			done <- result
		}()
		close(started)
		result.err = call()
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("repository call did not start")
	}
	if cancelWhileHeld != nil {
		cancelWhileHeld()
	}
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Error("repository call did not return within its bound; held mutex has not been released")
		// Release only after recording failure, then drain the call so late writes
		// remain observable and a failing implementation does not leak a goroutine.
		if held {
			r.mu.Unlock()
			held = false
		}
		select {
		case result := <-done:
			return result
		case <-time.After(2 * time.Second):
			t.Fatal("repository call did not finish after cleanup unlocked the mutex")
			return memoryContextResult{}
		}
	}
}

func testMemoryContextRefusal(t *testing.T, mode string) {
	t.Helper()
	f := newMemoryContextFixture(t)
	for _, method := range memoryContextMethods() {
		t.Run(method.name, func(t *testing.T) {
			r := memoryContextRepository(t, method.name, f)
			before := memoryContextSnapshot(r)
			var ctx context.Context
			var cancel context.CancelFunc
			var cancelWhileHeld context.CancelFunc
			var want error
			held := mode != "canceled"
			switch mode {
			case "canceled":
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
				want = context.Canceled
			case "locked_canceled":
				ctx, cancel = context.WithCancel(context.Background())
				cancelWhileHeld = cancel
				want = context.Canceled
			case "locked_deadline":
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				want = context.DeadlineExceeded
			case "nil":
			default:
				t.Fatalf("unknown context mode %q", mode)
			}
			if cancel != nil {
				defer cancel()
			}
			result := memoryContextCall(t, r, held, cancelWhileHeld, func() error {
				return method.call(ctx, r, f)
			})
			if result.panicValue != nil {
				t.Errorf("repository method panicked rather than refusing context: %v", result.panicValue)
			}
			if want == nil {
				if result.err == nil {
					t.Error("nil context was not explicitly refused")
				}
			} else if !errors.Is(result.err, want) {
				t.Errorf("repository error = %v, want %v", result.err, want)
			}
			after := memoryContextSnapshot(r)
			if !reflect.DeepEqual(before, after) {
				t.Error("refused call changed receipts, byID, consumptions, approvalClaims, or exercises")
			}
		})
	}
}

func TestMemoryRepositoryCanceledContextBeforeAccess(t *testing.T) {
	testMemoryContextRefusal(t, "canceled")
}

func TestMemoryRepositoryCancellationReturnsWhileMutexHeld(t *testing.T) {
	testMemoryContextRefusal(t, "locked_canceled")
}

func TestMemoryRepositoryContextDeadlineReturnsWhileMutexHeld(t *testing.T) {
	testMemoryContextRefusal(t, "locked_deadline")
}

func TestMemoryRepositoryNilContextIsRefusedWithoutPanic(t *testing.T) {
	testMemoryContextRefusal(t, "nil")
}

func TestMemoryRepositoryHealthyContextPreservesSingleUseAndIdempotency(t *testing.T) {
	f := newMemoryContextFixture(t)
	r := NewMemoryRepository()
	ctx := context.Background()
	first, created, err := r.CreateOrGet(ctx, f.receipt)
	if err != nil || !created || !reflect.DeepEqual(first, f.receipt) {
		t.Fatalf("create receipt: created=%t error=%v", created, err)
	}
	second, created, err := r.CreateOrGet(ctx, f.receipt)
	if err != nil || created || !reflect.DeepEqual(second, first) {
		t.Fatalf("idempotent receipt lookup: created=%t error=%v", created, err)
	}
	conflicting := cloneReceipt(f.receipt)
	conflicting.RequestDigest = strings.Repeat("f", 64)
	if _, _, err := r.CreateOrGet(ctx, conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting receipt: %v", err)
	}
	got, err := r.Get(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatalf("Get: %v", err)
	}
	listed, err := r.List(ctx, f.receipt.OwnerIdentity, 10)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], first) {
		t.Fatalf("List: receipts=%d error=%v", len(listed), err)
	}
	if err := r.Consume(ctx, f.consumption); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := r.Consume(ctx, f.consumption); !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("replayed consumption: %v", err)
	}
	consumed, err := r.GetConsumption(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
	if err != nil || !reflect.DeepEqual(consumed, f.consumption) {
		t.Fatalf("GetConsumption: %v", err)
	}
	if err := r.ExerciseFinalEffect(ctx, f.exercise); err != nil {
		t.Fatalf("ExerciseFinalEffect: %v", err)
	}
	if err := r.ExerciseFinalEffect(ctx, f.exercise); !errors.Is(err, ErrAlreadyExercised) {
		t.Fatalf("replayed exercise: %v", err)
	}
	exercised, err := r.GetFinalEffectExercise(ctx, f.receipt.OwnerIdentity, f.receipt.ID)
	if err != nil || !reflect.DeepEqual(exercised, f.exercise) {
		t.Fatalf("GetFinalEffectExercise: %v", err)
	}
	claimKey := approvalClaimKey(f.receipt.OwnerIdentity, f.receipt.ApprovalSourceID, f.receipt.Evidence.Approval.DecisionID)
	if got := memoryContextSnapshot(r).approvalClaims[claimKey]; got != f.receipt.ID {
		t.Fatalf("healthy consumption did not claim its approval: %v", got)
	}
}

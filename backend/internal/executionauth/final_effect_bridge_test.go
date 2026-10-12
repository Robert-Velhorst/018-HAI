package executionauth

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"github.com/google/uuid"
)

type cancelBeforeExerciseRepository struct {
	Repository
	cancel context.CancelFunc
}

func (r cancelBeforeExerciseRepository) ExerciseFinalEffect(
	ctx context.Context,
	exercise FinalEffectExercise,
) error {
	r.cancel()
	return r.Repository.ExerciseFinalEffect(ctx, exercise)
}

type cancelAfterConsumptionReadRepository struct {
	Repository
	cancel context.CancelFunc
}

func (r cancelAfterConsumptionReadRepository) GetConsumption(
	ctx context.Context,
	owner string,
	receiptID uuid.UUID,
) (Consumption, error) {
	value, err := r.Repository.GetConsumption(ctx, owner, receiptID)
	r.cancel()
	return value, err
}

func TestFinalEffectBridgeBindsAndAtomicallyExercisesMemoryReceipt(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		nil,
		nil,
	)
	request, effectRequest := authorizedRuntimeRequest(t, "memory-final-effect")
	target, err := FinalEffectExecutionTarget(request.EffectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"automation-runtime-handoff",
		target,
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(
		context.Background(),
		effectRequest,
		receipt.ID,
	)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	proof := proofFromBinding(t, binding, request.EffectDigest)

	start := make(chan struct{})
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- bridge.VerifyFinalEffectProof(
				context.Background(),
				effectRequest,
				proof,
			)
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var successes atomic.Int32
	var replays atomic.Int32
	for result := range results {
		switch {
		case result == nil:
			successes.Add(1)
		case errors.Is(result, ErrAlreadyExercised):
			replays.Add(1)
		default:
			t.Fatalf("unexpected final effect result: %v", result)
		}
	}
	if successes.Load() != 1 || replays.Load() != 15 {
		t.Fatalf(
			"final effect results successes=%d replays=%d, want 1 and 15",
			successes.Load(),
			replays.Load(),
		)
	}
	exercise, err := repository.GetFinalEffectExercise(
		context.Background(),
		request.OwnerIdentity,
		receipt.ID,
	)
	if err != nil {
		t.Fatalf("GetFinalEffectExercise: %v", err)
	}
	if exercise.RuntimeID != request.RuntimeID ||
		exercise.TaskID != request.TaskID ||
		exercise.EffectDigest != request.EffectDigest ||
		exercise.ConsumptionTarget != target {
		t.Fatalf("stored exercise is not exact: %#v", exercise)
	}
}

func TestFinalEffectBridgeRejectsStaleConsumedAuthorization(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request, effectRequest := authorizedRuntimeRequest(t, "stale-final-effect")
	target, err := FinalEffectExecutionTarget(request.EffectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	receipt, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"automation-runtime-handoff",
		target,
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, func() time.Time {
		return fixedNow().Add(time.Minute)
	})
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(context.Background(), effectRequest, receipt.ID)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	proof := proofFromBinding(t, binding, request.EffectDigest)
	if err := bridge.VerifyFinalEffectProof(context.Background(), effectRequest, proof); !errors.Is(err, ErrFinalEffectExpired) {
		t.Fatalf("stale consumed authorization error = %v, want ErrFinalEffectExpired", err)
	}
	if _, err := repository.GetFinalEffectExercise(context.Background(), request.OwnerIdentity, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale authorization created a final-effect exercise: %v", err)
	}
}

func TestFinalEffectBridgeRejectsExpiredApprovalAtEffectBoundary(t *testing.T) {
	repository := NewMemoryRepository()
	now := fixedNow()
	approvalSourceID := "task-review:216967e4-d62e-4a73-ae3f-c62efcbf78f5"
	approvalBindingDigest := strings.Repeat("a", 64)
	approval := ResolvedApproval{
		SourceID:       approvalSourceID,
		DecisionID:     "216967e4-d62e-4a73-ae3f-c62efcbf78f5",
		DecisionDigest: strings.Repeat("b", 64),
		BindingDigest:  approvalBindingDigest,
		ApprovedBy:     "alice",
		ApprovedAt:     now.Add(-time.Second),
		ExpiresAt:      now.Add(2 * time.Second),
	}
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		fakeApprovalResolver{values: map[string]ResolvedApproval{
			"alice\x00" + approvalSourceID: approval,
		}},
		nil,
	)
	effectRequest, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes", "task-1", "alice", "project-1", "perform approved work", approvalSourceID, true,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	effectDigest, err := FinalEffectDigest(effectRequest)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	request := baseRequest("expired-approval-final-effect")
	request.Action = AgentRuntimeExecuteAction
	request.ResourceType = AgentRuntimeResourceType
	request.ResourceID = effectRequest.TaskID
	request.ProjectKey = effectRequest.ProjectKey
	request.RuntimeID = effectRequest.RuntimeID
	request.EffectDigest = effectDigest
	request.ApprovalSourceID = approvalSourceID
	request.ApprovalBindingDigest = approvalBindingDigest
	target, err := FinalEffectExecutionTarget(effectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	// This test exercises the final-effect expiry boundary after authorization;
	// production task-review consumption is covered by the PostgreSQL transaction test.
	receipt, err := service.authorizeAndConsumeCore(
		context.Background(), request, "automation-runtime-handoff", target,
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, func() time.Time {
		return now.Add(3 * time.Second)
	})
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(context.Background(), effectRequest, receipt.ID)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	proof := proofFromBinding(t, binding, effectDigest)
	if err := bridge.VerifyFinalEffectProof(context.Background(), effectRequest, proof); !errors.Is(err, ErrFinalEffectExpired) {
		t.Fatalf("expired approval error = %v, want ErrFinalEffectExpired", err)
	}
	if _, err := repository.GetFinalEffectExercise(context.Background(), request.OwnerIdentity, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired approval created a final-effect exercise: %v", err)
	}
}

func TestFinalEffectBridgeAndMemoryRepositoryHonorCancellation(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request, effectRequest := authorizedRuntimeRequest(t, "cancelled-final-effect")
	target, err := FinalEffectExecutionTarget(request.EffectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	receipt, err := service.AuthorizeAndConsume(
		context.Background(), request, "automation-runtime-handoff", target,
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(context.Background(), effectRequest, receipt.ID)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	proof := proofFromBinding(t, binding, request.EffectDigest)
	exercise := finalEffectExercise(effectRequest, proof, fixedNow())

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.ExerciseFinalEffect(cancelledContext, exercise); !errors.Is(err, context.Canceled) {
		t.Fatalf("MemoryRepository.ExerciseFinalEffect error = %v, want context.Canceled", err)
	}

	ctx, cancelBeforeWrite := context.WithCancel(context.Background())
	defer cancelBeforeWrite()
	cancellingRepository := cancelBeforeExerciseRepository{
		Repository: repository,
		cancel:     cancelBeforeWrite,
	}
	cancellingBridge, err := NewFinalEffectBridge(cancellingRepository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge with cancellation repository: %v", err)
	}
	if err := cancellingBridge.VerifyFinalEffectProof(ctx, effectRequest, proof); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyFinalEffectProof error = %v, want context.Canceled", err)
	}
	if _, err := repository.GetFinalEffectExercise(context.Background(), request.OwnerIdentity, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled authorization created an exercise record: %v", err)
	}
}

func TestBindConsumedFinalEffectRejectsCancellationDuringLookup(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request, effectRequest := authorizedRuntimeRequest(t, "cancelled-final-effect-binding")
	target, err := FinalEffectExecutionTarget(request.EffectDigest)
	if err != nil {
		t.Fatalf("FinalEffectExecutionTarget: %v", err)
	}
	receipt, err := service.AuthorizeAndConsume(
		context.Background(), request, "automation-runtime-handoff", target,
	)
	if err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancellingRepository := cancelAfterConsumptionReadRepository{
		Repository: repository,
		cancel:     cancel,
	}
	bridge, err := NewFinalEffectBridge(cancellingRepository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	if binding, err := bridge.BindConsumedFinalEffect(ctx, effectRequest, receipt.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("BindConsumedFinalEffect returned binding %#v and error %v; want context.Canceled", binding, err)
	}
}

func TestFinalEffectBridgeRejectsUnconsumedOrMismatchedProofs(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(
		t,
		repository,
		permissiveConstitution(),
		nil,
		nil,
	)
	request, effectRequest := authorizedRuntimeRequest(t, "mismatch-final-effect")
	receipt, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, fixedNow)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	if _, err := bridge.BindConsumedFinalEffect(
		context.Background(),
		effectRequest,
		receipt.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unconsumed bind error = %v, want ErrNotFound", err)
	}

	target, _ := FinalEffectExecutionTarget(request.EffectDigest)
	if _, err := service.AuthorizeAndConsume(
		context.Background(),
		request,
		"automation-runtime-handoff",
		target,
	); err != nil {
		t.Fatalf("AuthorizeAndConsume: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(
		context.Background(),
		effectRequest,
		receipt.ID,
	)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	valid := proofFromBinding(t, binding, request.EffectDigest)

	tests := []struct {
		name    string
		request agentruntime.FinalEffectAuthorizationRequest
		proof   agentruntime.FinalEffectAuthorizationProof
	}{
		{
			name: "owner",
			request: func() agentruntime.FinalEffectAuthorizationRequest {
				value := effectRequest
				value.OwnerIdentity = "bob"
				return value
			}(),
			proof: valid,
		},
		{
			name: "runtime",
			request: func() agentruntime.FinalEffectAuthorizationRequest {
				value := effectRequest
				value.RuntimeID = "odysseus"
				return value
			}(),
			proof: valid,
		},
		{
			name: "task",
			request: func() agentruntime.FinalEffectAuthorizationRequest {
				value := effectRequest
				value.TaskID = "task-other"
				return value
			}(),
			proof: valid,
		},
		{
			name:    "request digest",
			request: effectRequest,
			proof: func() agentruntime.FinalEffectAuthorizationProof {
				value := valid
				value.AuthorizationRequestDigest = digestForTest("wrong-request")
				return value
			}(),
		},
		{
			name:    "decision digest",
			request: effectRequest,
			proof: func() agentruntime.FinalEffectAuthorizationProof {
				value := valid
				value.DecisionDigest = digestForTest("wrong-decision")
				return value
			}(),
		},
		{
			name:    "runtime proof",
			request: effectRequest,
			proof: func() agentruntime.FinalEffectAuthorizationProof {
				value := valid
				value.RuntimeProof = digestForTest("wrong-effect")
				return value
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := bridge.VerifyFinalEffectProof(
				context.Background(),
				test.request,
				test.proof,
			)
			if err == nil {
				t.Fatal("mismatched final effect proof was accepted")
			}
			if _, lookupErr := repository.GetFinalEffectExercise(
				context.Background(),
				request.OwnerIdentity,
				receipt.ID,
			); !errors.Is(lookupErr, ErrNotFound) {
				t.Fatalf("rejected proof created exercise: %v", lookupErr)
			}
		})
	}
}

func TestBuildAgentRuntimeFinalEffectRequestMatchesRuntimeDigestContract(t *testing.T) {
	request, err := BuildAgentRuntimeFinalEffectRequest(
		"HERMES",
		"task-1",
		"alice",
		"project-1",
		"write the verified report",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	digest, err := FinalEffectDigest(request)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	if request.RuntimeID != "hermes" || !validDigest(digest) {
		t.Fatalf("runtime request was not normalized and digested: %#v %q", request, digest)
	}
	second, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes",
		"task-1",
		"alice",
		"project-1",
		"write the verified report",
		"",
		false,
	)
	if err != nil || !reflect.DeepEqual(request, second) {
		t.Fatalf("runtime request is not deterministic: %#v %#v %v", request, second, err)
	}
}

func TestFinalEffectDigestMatchesAgentRuntimeRegistryBinding(t *testing.T) {
	task := agentruntime.Task{
		ID:               "task-1",
		Prompt:           "write the verified report",
		ProjectKey:       "project-1",
		OwnerIdentity:    "alice",
		ApprovalSourceID: "task-review:216967e4-d62e-4a73-ae3f-c62efcbf78f5",
	}
	request, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes",
		task.ID,
		task.OwnerIdentity,
		task.ProjectKey,
		task.Prompt,
		task.ApprovalSourceID,
		true,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	effectDigest, err := FinalEffectDigest(request)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	registry := agentruntime.DefaultRegistry()
	bound, err := registry.BindConsumedAuthorizationProof(
		"hermes",
		task,
		"216967e4-d62e-4a73-ae3f-c62efcbf78f5",
		digestForTest("authorization-request"),
		digestForTest("decision"),
		effectDigest,
	)
	if err != nil {
		t.Fatalf("BindConsumedAuthorizationProof: %v", err)
	}
	if bound.FinalEffectProof.RuntimeRequestDigest != effectDigest ||
		bound.FinalEffectProof.RuntimeProof != effectDigest {
		t.Fatalf(
			"executionauth digest does not match agentruntime binding: %#v",
			bound.FinalEffectProof,
		)
	}
}

func authorizedRuntimeRequest(
	t *testing.T,
	key string,
) (Request, agentruntime.FinalEffectAuthorizationRequest) {
	t.Helper()
	finalRequest, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes",
		"task-1",
		"alice",
		"project-1",
		"prepare a bounded result",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	effectDigest, err := FinalEffectDigest(finalRequest)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	request := baseRequest(key)
	request.Action = AgentRuntimeExecuteAction
	request.ResourceType = AgentRuntimeResourceType
	request.ResourceID = finalRequest.TaskID
	request.ProjectKey = finalRequest.ProjectKey
	request.RuntimeID = finalRequest.RuntimeID
	request.EffectDigest = effectDigest
	return request, finalRequest
}

func proofFromBinding(
	t *testing.T,
	binding FinalEffectBinding,
	effectDigest string,
) agentruntime.FinalEffectAuthorizationProof {
	t.Helper()
	if binding.ReceiptID == "" ||
		binding.AuthorizationRequestDigest == "" ||
		binding.DecisionDigest == "" ||
		binding.RuntimeProof != effectDigest {
		t.Fatalf("invalid final effect binding: %#v", binding)
	}
	return agentruntime.FinalEffectAuthorizationProof{
		ReceiptID:                  binding.ReceiptID,
		AuthorizationRequestDigest: binding.AuthorizationRequestDigest,
		DecisionDigest:             binding.DecisionDigest,
		RuntimeRequestDigest:       effectDigest,
		RuntimeProof:               binding.RuntimeProof,
	}
}

func digestForTest(value string) string {
	return fmt.Sprintf("%064x", value)
}

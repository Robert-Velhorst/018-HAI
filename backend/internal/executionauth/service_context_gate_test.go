package executionauth

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/agentruntime"

	"github.com/google/uuid"
)

func TestExecutionAuthorityRejectsAbsentOrCanceledContextBeforeDependencies(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{"absent", nil},
		{"canceled", canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			// No dependencies: touching a policy, receipt store, or clock panics.
			service := &Service{}
			bridge := &FinalEffectBridge{}
			for name, call := range map[string]func() error{
				"authorize": func() error {
					_, err := service.Authorize(test.ctx, baseRequest("context-gate"))
					return err
				},
				"authorize_and_consume": func() error {
					_, err := service.AuthorizeAndConsume(test.ctx, baseRequest("context-gate"), "worker", "local-file")
					return err
				},
				"task_review_transaction": func() error {
					request := baseRequest("context-gate")
					request.ApprovalSourceID = "task-review:context-gate"
					_, err := service.AuthorizeAndConsume(test.ctx, request, "worker", "local-file")
					return err
				},
				"bind_final_effect": func() error {
					_, err := bridge.BindConsumedFinalEffect(test.ctx, agentruntime.FinalEffectAuthorizationRequest{}, uuid.Nil)
					return err
				},
				"verify_final_effect": func() error {
					return bridge.VerifyFinalEffectProof(test.ctx, agentruntime.FinalEffectAuthorizationRequest{}, agentruntime.FinalEffectAuthorizationProof{})
				},
			} {
				t.Run(name, func(t *testing.T) {
					err := call()
					if err == nil || (test.ctx != nil && !errors.Is(err, context.Canceled)) {
						t.Fatalf("context refused incorrectly: %v", err)
					}
				})
			}
		})
	}
}

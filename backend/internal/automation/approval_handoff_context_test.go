package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type proofHandoffProbe struct {
	ApprovalProofService
	ctx          context.Context
	calls        int
	afterConsume context.CancelFunc
	verifyError  error
	mutateProof  bool
}

func (p *proofHandoffProbe) VerifyAndConsume(ctx context.Context, proof *ApprovalProof, _ ApprovalProofExpectation) error {
	p.ctx = ctx
	p.calls++
	if p.afterConsume != nil {
		p.afterConsume()
	}
	if p.mutateProof {
		proof.ActionDigest = strings.Repeat("b", 64)
	}
	return p.verifyError
}

func TestLauncherProofHandoffChecksAdapterCancellation(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after", "adapter_error", "cancel_with_error", "execute_cancel_after", "binding_after", "execute_binding_after"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "handoff"))
			defer cancel()
			id := uuid.New()
			configuration := &models.Automation{ID: id, Name: "controlled handoff", LaunchType: "script", LaunchTarget: "missing-proof-handoff-" + uuid.NewString() + ".ps1"}
			svc := newTestService(newFakeAutomationRepo(configuration), events.Publisher{}).(*service)
			request := TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Task: "Run tests", ApprovalSourceID: "task-review:" + uuid.NewString()}
			digest := automationActionDigest(configuration, request)
			proof, err := svc.approvalProofs.Issue(ApprovalProofIssueRequest{OwnerIdentity: request.OwnerIdentity, AutomationID: id, ActionDigest: digest, Scope: ApprovalScopeScript, ApprovalSourceID: request.ApprovalSourceID})
			if err != nil {
				t.Fatal(err)
			}
			request.ApprovalProof = proof
			request.ApprovalBindingDigest = digest
			request = captureLaunchActionBinding(configuration, request)
			cause := errors.New("controlled proof adapter failure")
			probe := &proofHandoffProbe{ApprovalProofService: svc.approvalProofs}
			svc.approvalProofs = probe
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after", "execute_cancel_after":
				probe.afterConsume = cancel
			case "adapter_error":
				probe.verifyError = cause
			case "cancel_with_error":
				probe.afterConsume = cancel
				probe.verifyError = cause
			case "binding_after", "execute_binding_after":
				probe.mutateProof = true
			}
			if boundary == "execute_cancel_after" || boundary == "execute_binding_after" {
				outcome := svc.executeLaunch(configuration, request, time.Now().UTC(), uuid.New())
				if outcome.Status != "blocked" {
					t.Fatal("cancelled adapter handoff admitted script execution")
				}
				for _, event := range outcome.AuditEvents {
					if strings.Contains(event, "proof verified and consumed") {
						t.Fatal("cancelled adapter fabricated verified authority")
					}
				}
				return
			}
			audit, err := svc.verifyAndConsumeApproval(configuration, request)
			if boundary == "valid" {
				if err != nil || len(audit) == 0 || probe.calls != 1 {
					t.Fatalf("valid proof handoff failed: %v", err)
				}
				if probe.ctx.Value(configurationContextKey{}) != "handoff" {
					t.Fatal("proof adapter lost caller context")
				}
				if _, ok := probe.ctx.Deadline(); !ok {
					t.Fatal("proof adapter has no deadline")
				}
			} else if err == nil || len(audit) != 0 {
				t.Fatal("uncertain adapter returned verified authority")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("proof handoff lost cancellation")
			}
			if (boundary == "adapter_error" || boundary == "cancel_with_error") && !errors.Is(err, cause) {
				t.Fatal("proof handoff lost adapter error")
			}
			if boundary == "cancel_before" && probe.calls != 0 {
				t.Fatal("cancelled handoff entered adapter")
			}
		})
	}
}

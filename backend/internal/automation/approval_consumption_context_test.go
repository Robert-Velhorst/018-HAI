package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type consumptionContextProbe struct {
	ApprovalProofConsumptionStore
	ctx          context.Context
	calls        int
	afterConsume context.CancelFunc
	consumeError error
}

func (p *consumptionContextProbe) Consume(ctx context.Context, value ApprovalProofConsumption) error {
	p.ctx = ctx
	p.calls++
	if p.consumeError != nil {
		return p.consumeError
	}
	err := p.ApprovalProofConsumptionStore.Consume(ctx, value)
	if p.afterConsume != nil {
		p.afterConsume()
	}
	return err
}

func TestApprovalConsumptionRespectsCallerAndRetainsSpentProof(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after_store", "storage_error", "cancel_during_clock"} {
		t.Run(boundary, func(t *testing.T) {
			now := time.Now().UTC()
			memory := &memoryApprovalProofConsumptionStore{consumed: make(map[string]time.Time)}
			probe := &consumptionContextProbe{ApprovalProofConsumptionStore: memory}
			svc, err := NewApprovalProofService([]byte("0123456789abcdef0123456789abcdef"), probe, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			request := ApprovalProofIssueRequest{OwnerIdentity: "alice", AutomationID: uuid.New(), ActionDigest: approvalTestDigest("owned consumption"), Scope: ApprovalScopeScript, ApprovalSourceID: "task-review:" + uuid.NewString()}
			proof, err := svc.Issue(request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "consume"))
			defer cancel()
			storageError := errors.New("controlled consumption storage error")
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after_store":
				probe.afterConsume = cancel
			case "storage_error":
				probe.consumeError = storageError
			case "cancel_during_clock":
				svc.(*approvalProofService).now = func() time.Time { cancel(); return now }
			}
			expected := ApprovalProofExpectation{OwnerIdentity: request.OwnerIdentity, AutomationID: request.AutomationID, ActionDigest: request.ActionDigest, Scope: request.Scope, ApprovalSourceID: request.ApprovalSourceID}
			err = svc.VerifyAndConsume(ctx, proof, expected)
			if boundary == "valid" {
				if err != nil || probe.calls != 1 {
					t.Fatalf("valid consumption failed: %v", err)
				}
				if probe.ctx.Value(configurationContextKey{}) != "consume" {
					t.Fatal("consumption lost caller context")
				}
				if _, ok := probe.ctx.Deadline(); !ok {
					t.Fatal("consumption has no deadline")
				}
			} else if err == nil {
				t.Fatal("uncertain consumption returned authority")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("consumption lost cancellation")
			}
			if boundary == "storage_error" && !errors.Is(err, storageError) {
				t.Fatal("consumption lost storage error")
			}
			if (boundary == "storage_error" || boundary == "cancel_after_store") && !errors.Is(err, ErrApprovalProofConsumptionUnconfirmed) {
				t.Fatal("uncertain consumption lost reconciliation requirement")
			}
			if (boundary == "cancel_before" || boundary == "cancel_during_clock") && probe.calls != 0 {
				t.Fatal("cancelled request entered consumption storage")
			}
			if boundary == "cancel_after_store" {
				probe.afterConsume = nil
				if err := svc.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofConsumed) {
					t.Fatal("cancelled acknowledgement restored spent proof")
				}
			}
		})
	}
}

func TestMemoryProofConsumptionRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	memory := &memoryApprovalProofConsumptionStore{consumed: make(map[string]time.Time)}
	if err := memory.Consume(ctx, ApprovalProofConsumption{}); !errors.Is(err, context.Canceled) {
		t.Fatal("memory consumption ignored cancellation")
	}
}

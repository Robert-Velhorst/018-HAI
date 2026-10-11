package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"github.com/google/uuid"
)

type contextualIssuanceLauncher struct {
	contextualApprovalLauncher
	proofContext context.Context
	proofCalls   int
	afterProof   context.CancelFunc
	proofError   error
}

func (l *contextualIssuanceLauncher) ActionApprovalRequiredContext(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}
func (l *contextualIssuanceLauncher) IssueApprovalProofContext(ctx context.Context, _ uuid.UUID, _ automation.TaskApprovalProofRequest) (*automation.ApprovalProof, error) {
	l.proofContext = ctx
	l.proofCalls++
	if l.afterProof != nil {
		l.afterProof()
	}
	return l.proof, l.proofError
}

type legacyIssuanceLauncher struct{ contextualApprovalLauncher }

func (l *legacyIssuanceLauncher) ActionApprovalRequiredContext(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func TestOwnedExecutionUsesContextualProofIssuance(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_after", "issue_error", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "proof"))
			defer cancel()
			id := uuid.New()
			source := "workflow-decision:" + uuid.NewString()
			now := time.Now().UTC()
			l := &contextualIssuanceLauncher{contextualApprovalLauncher: contextualApprovalLauncher{fakeAutomationLauncher: fakeAutomationLauncher{proof: &automation.ApprovalProof{ID: uuid.NewString(), OwnerIdentity: "alice", AutomationID: id, ActionDigest: strings.Repeat("a", 64), Scope: automation.ApprovalScopeScript, ApprovalSourceID: source, IssuedAt: now, ExpiresAt: now.Add(time.Minute), Nonce: "test-nonce", Signature: "test-signature"}, result: &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: "completed", ExitCode: 0, LaunchedAt: now}}}}
			var runtime automationLauncher = l
			if boundary == "legacy" {
				runtime = &legacyIssuanceLauncher{contextualApprovalLauncher: l.contextualApprovalLauncher}
			}
			if boundary == "cancel_after" {
				l.afterProof = cancel
			}
			issueError := errors.New("controlled proof issuance failure")
			if boundary == "issue_error" {
				l.proofError = issueError
			}
			result, err := NewAutomationToolExecutor(runtime).Execute(ToolExecutionRequest{ExecutionContext: ctx, OwnerIdentity: "alice", TaskID: "proof-test", AutomationID: id.String(), Task: "Run tests", WorkflowID: uuid.NewString(), ApprovalSourceID: source})
			if boundary == "valid" {
				if err != nil || result == nil || l.launchCalls != 1 || l.proofCalls != 1 {
					t.Fatalf("contextual issuance failed: %v", err)
				}
				if l.proofContext.Value(taskStorageContextKey{}) != "proof" {
					t.Fatal("issuance lost context")
				}
				if _, ok := l.proofContext.Deadline(); !ok {
					t.Fatal("issuance has no deadline")
				}
			} else if err == nil || l.launchCalls != 0 {
				t.Fatal("uncertain proof entered launch")
			}
			if l.issueCalls != 0 {
				t.Fatal("owned issuance used legacy signer")
			}
			if boundary == "cancel_after" && !errors.Is(err, context.Canceled) {
				t.Fatal("issuance lost cancellation")
			}
			if boundary == "issue_error" && !errors.Is(err, issueError) {
				t.Fatal("issuance lost error identity")
			}
		})
	}
}

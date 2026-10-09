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

type contextualApprovalLauncher struct {
	fakeAutomationLauncher
	ctx          context.Context
	contextCalls int
	afterRecord  context.CancelFunc
	recordError  error
}

func (l *contextualApprovalLauncher) RecordApprovalDecisionContext(ctx context.Context, _ uuid.UUID, _ automation.TaskApprovalDecisionRequest) error {
	l.contextCalls++
	l.ctx = ctx
	if l.afterRecord != nil {
		l.afterRecord()
	}
	return l.recordError
}

func (l *contextualApprovalLauncher) ActionApprovalRequiredContext(ctx context.Context, _ uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func TestOwnedExecutionUsesContextualApprovalRegistration(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_after_record", "record_error", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "approval-registration"))
			defer cancel()
			id := uuid.New()
			noProof := false
			launcher := &contextualApprovalLauncher{fakeAutomationLauncher: fakeAutomationLauncher{actionApprovalRequired: &noProof, result: &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: "completed", ExitCode: 0, LaunchedAt: time.Now().UTC()}}}
			var runtime automationLauncher = launcher
			if boundary == "legacy" {
				runtime = &launcher.fakeAutomationLauncher
			}
			if boundary == "cancel_after_record" {
				launcher.afterRecord = cancel
			}
			storageError := errors.New("controlled registration failure")
			if boundary == "record_error" {
				launcher.recordError = storageError
			}
			sourceID := "task-review:" + uuid.NewString()
			digest := strings.Repeat("a", 64)
			decision := &automation.TaskApprovalDecisionRequest{OwnerIdentity: "alice", Task: "Run tests", ProjectKey: "project", ApprovalSourceID: sourceID, ApprovalBindingDigest: digest, ApprovedAt: time.Now().UTC(), ReviewConfiguration: historicalReviewSnapshot(id)}
			result, err := NewAutomationToolExecutor(runtime).Execute(ToolExecutionRequest{ExecutionContext: ctx, OwnerIdentity: "alice", TaskID: "registration-test", AutomationID: id.String(), Task: "Run tests", ProjectKey: "project", ApprovalSourceID: sourceID, ApprovalBindingDigest: digest, approvalDecision: decision})
			if boundary == "valid" {
				if err != nil || result == nil || launcher.launchCalls != 1 || launcher.contextCalls != 1 {
					t.Fatalf("owned registration did not reach controlled launch: %v", err)
				}
				if launcher.ctx.Value(taskStorageContextKey{}) != "approval-registration" {
					t.Fatal("registration lost caller context")
				}
				if _, ok := launcher.ctx.Deadline(); !ok {
					t.Fatal("registration has no deadline")
				}
			} else if err == nil || launcher.launchCalls != 0 || launcher.issueCalls != 0 || launcher.approvalInspectionCalls != 0 {
				t.Fatalf("unconfirmed registration entered next stage: %v", err)
			}
			if launcher.recordCalls != 0 {
				t.Fatal("owned registration used legacy recorder")
			}
			if boundary == "cancel_after_record" && !errors.Is(err, context.Canceled) {
				t.Fatal("registration lost cancellation")
			}
			if boundary == "record_error" && !errors.Is(err, storageError) {
				t.Fatal("registration lost storage error identity")
			}
		})
	}
}

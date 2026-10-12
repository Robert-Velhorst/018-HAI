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

type contextualRequirementLauncher struct {
	contextualApprovalLauncher
	requirementContext context.Context
	requirementCalls   int
	afterRequirement   context.CancelFunc
	requirementError   error
}

type legacyRequirementLauncher struct{ target *contextualApprovalLauncher }

func (l *legacyRequirementLauncher) Launch(id uuid.UUID) (*automation.LaunchResult, error) {
	return l.target.Launch(id)
}
func (l *legacyRequirementLauncher) LaunchTask(id uuid.UUID, r automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	return l.target.LaunchTask(id, r)
}
func (l *legacyRequirementLauncher) RecordApprovalDecisionContext(ctx context.Context, id uuid.UUID, r automation.TaskApprovalDecisionRequest) error {
	return l.target.RecordApprovalDecisionContext(ctx, id, r)
}
func (l *legacyRequirementLauncher) ActionApprovalRequired(id uuid.UUID) (bool, error) {
	return l.target.ActionApprovalRequired(id)
}

func (l *contextualRequirementLauncher) ActionApprovalRequiredContext(ctx context.Context, _ uuid.UUID) (bool, error) {
	l.requirementContext = ctx
	l.requirementCalls++
	if l.afterRequirement != nil {
		l.afterRequirement()
	}
	return false, l.requirementError
}

func TestOwnedExecutionUsesContextualApprovalRequirement(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_after", "read_error", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "approval-requirement"))
			defer cancel()
			id := uuid.New()
			noProof := false
			l := &contextualRequirementLauncher{contextualApprovalLauncher: contextualApprovalLauncher{fakeAutomationLauncher: fakeAutomationLauncher{actionApprovalRequired: &noProof, result: &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: "completed", ExitCode: 0, LaunchedAt: time.Now().UTC()}}}}
			var runtime automationLauncher = l
			if boundary == "legacy" {
				runtime = &legacyRequirementLauncher{target: &l.contextualApprovalLauncher}
			}
			if boundary == "cancel_after" {
				l.afterRequirement = cancel
			}
			readError := errors.New("controlled requirement read failure")
			if boundary == "read_error" {
				l.requirementError = readError
			}
			source := "task-review:" + uuid.NewString()
			digest := strings.Repeat("a", 64)
			decision := &automation.TaskApprovalDecisionRequest{OwnerIdentity: "alice", Task: "Run tests", ProjectKey: "project", ApprovalSourceID: source, ApprovalBindingDigest: digest, ApprovedAt: time.Now().UTC(), ReviewConfiguration: historicalReviewSnapshot(id)}
			result, err := NewAutomationToolExecutor(runtime).Execute(ToolExecutionRequest{ExecutionContext: ctx, OwnerIdentity: "alice", TaskID: "requirement-test", AutomationID: id.String(), Task: "Run tests", ProjectKey: "project", ApprovalSourceID: source, ApprovalBindingDigest: digest, approvalDecision: decision})
			if boundary == "valid" {
				if err != nil || result == nil || l.launchCalls != 1 || l.requirementCalls != 1 {
					t.Fatalf("contextual requirement did not reach controlled launch: %v", err)
				}
				if l.requirementContext.Value(taskStorageContextKey{}) != "approval-requirement" {
					t.Fatal("requirement lost caller context")
				}
				if _, ok := l.requirementContext.Deadline(); !ok {
					t.Fatal("requirement has no deadline")
				}
			} else if err == nil || l.launchCalls != 0 || l.issueCalls != 0 {
				t.Fatalf("uncertain requirement continued execution: %v", err)
			}
			if l.approvalInspectionCalls != 0 {
				t.Fatal("owned requirement used legacy reader")
			}
			if boundary == "cancel_after" && !errors.Is(err, context.Canceled) {
				t.Fatal("requirement lost cancellation")
			}
			if boundary == "read_error" && !errors.Is(err, readError) {
				t.Fatal("requirement lost error identity")
			}
		})
	}
}

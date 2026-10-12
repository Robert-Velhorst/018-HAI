package task

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/automation"
	"github.com/google/uuid"
)

type contextualSnapshotProbe struct {
	historicalSnapshotExecutor
	ctx          context.Context
	contextCalls int
	afterRead    context.CancelFunc
}

type legacySnapshotLauncher struct {
	launchOnlyAutomationLauncher
	probe *contextualSnapshotProbe
}

func (l *legacySnapshotLauncher) InspectReviewConfiguration(id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	return l.probe.InspectReviewConfiguration(id)
}

type contextualSnapshotLauncher struct{ legacySnapshotLauncher }

func (l *contextualSnapshotLauncher) InspectReviewConfigurationContext(ctx context.Context, id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	return l.probe.InspectReviewConfigurationContext(ctx, id)
}

func TestAutomationExecutorForwardsBoundedSnapshotContext(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_after", "legacy", "nil_context", "nil_launcher"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "adapter-snapshot"))
			defer cancel()
			id := uuid.New()
			probe := &contextualSnapshotProbe{historicalSnapshotExecutor: historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}}
			var launcher automationLauncher = &contextualSnapshotLauncher{legacySnapshotLauncher{probe: probe}}
			if boundary == "legacy" {
				launcher = &legacySnapshotLauncher{probe: probe}
			}
			if boundary == "nil_launcher" {
				launcher = nil
			}
			if boundary == "cancel_after" {
				probe.afterRead = cancel
			}
			var callCtx context.Context = ctx
			if boundary == "nil_context" {
				callCtx = nil
			}
			snapshot, err := NewAutomationToolExecutor(launcher).InspectReviewConfigurationContext(callCtx, id)
			if boundary == "valid" {
				if err != nil || snapshot == nil || probe.contextCalls != 1 {
					t.Fatalf("adapter did not forward inspection: %v", err)
				}
				if probe.ctx.Value(taskStorageContextKey{}) != "adapter-snapshot" {
					t.Fatal("adapter lost caller context")
				}
				if _, ok := probe.ctx.Deadline(); !ok {
					t.Fatal("adapter has no inspection deadline")
				}
			} else if err == nil || snapshot != nil {
				t.Fatal("adapter returned unconfirmed snapshot")
			}
			if probe.inspectionCalls != 0 {
				t.Fatal("adapter used legacy fallback")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("adapter lost cancellation")
			}
		})
	}
}

func (e *contextualSnapshotProbe) InspectReviewConfigurationContext(ctx context.Context, id uuid.UUID) (*automation.ReviewConfigurationSnapshot, error) {
	e.contextCalls++
	e.ctx = ctx
	if e.afterRead != nil {
		e.afterRead()
	}
	return e.snapshot, nil
}

func TestOwnedReviewConfigurationUsesBoundedContext(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), taskStorageContextKey{}, "snapshot-read"))
			defer cancel()
			id := uuid.New()
			executor := &contextualSnapshotProbe{historicalSnapshotExecutor: historicalSnapshotExecutor{snapshot: historicalReviewSnapshot(id)}}
			var tool ToolExecutor = executor
			if boundary == "legacy" {
				tool = &executor.historicalSnapshotExecutor
			}
			s := newDurableTaskTestService(t, NewMemoryTaskStateRepository(), tool).(*service)
			if boundary == "cancel_before" {
				cancel()
			}
			if boundary == "cancel_after" {
				executor.afterRead = cancel
			}
			request, err := s.captureAutomationReviewConfiguration(IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", AutomationID: id.String()})
			if boundary == "valid" {
				if err != nil || request.automationReviewSnapshot == nil || executor.contextCalls != 1 {
					t.Fatalf("owned snapshot inspection not used: %v", err)
				}
				if executor.ctx.Value(taskStorageContextKey{}) != "snapshot-read" {
					t.Fatal("snapshot lost caller context")
				}
				if _, ok := executor.ctx.Deadline(); !ok {
					t.Fatal("snapshot inspection has no deadline")
				}
			} else if err == nil || request.automationReviewSnapshot != nil {
				t.Fatal("unsafe snapshot admitted")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("inspection cancellation lost: %v", err)
			}
			if executor.inspectionCalls != 0 {
				t.Fatal("owned inspection fell back to legacy inspector")
			}
		})
	}
}

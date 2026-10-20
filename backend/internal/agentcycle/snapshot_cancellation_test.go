package agentcycle

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
)

type interruptingSnapshotProvider struct {
	fakePursuitSnapshotProvider
	cancel context.CancelFunc
}

func (p interruptingSnapshotProvider) OperatingSnapshotForOwner(owner string) (*pursuit.OperatingSnapshot, error) {
	snapshot, err := p.fakePursuitSnapshotProvider.OperatingSnapshotForOwner(owner)
	if p.cancel != nil {
		p.cancel()
	}
	return snapshot, err
}

type interruptingSnapshotScanner struct {
	*fakeSnapshotAmbientScanner
	err error
}

func (a interruptingSnapshotScanner) ScanForOwnerWithPursuitDashboard(owner, trigger string, dashboard *pursuit.Dashboard) (*models.AmbientScan, error) {
	scan, _ := a.fakeSnapshotAmbientScanner.ScanForOwnerWithPursuitDashboard(owner, trigger, dashboard)
	return scan, a.err
}

func TestOwnerCycleStopsBetweenSnapshotAndAmbientScan(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
		err    error
	}{
		{name: "caller cancels after snapshot", cancel: true, err: context.Canceled},
		{name: "snapshot reports cancellation", err: fmt.Errorf("snapshot: %w", context.Canceled)},
		{name: "snapshot reports deadline", err: fmt.Errorf("snapshot: %w", context.DeadlineExceeded)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := interruptingSnapshotProvider{fakePursuitSnapshotProvider: fakePursuitSnapshotProvider{
				fakePursuitBriefProvider: fakePursuitBriefProvider{calls: &calls},
				dashboard:                &pursuit.Dashboard{},
			}}
			if tc.cancel {
				provider.cancel = cancel
			} else {
				provider.err = tc.err
			}
			ambient := &fakeSnapshotAmbientScanner{calls: &calls}
			service := NewServiceWithPursuits(fakeSourceSyncer{calls: &calls}, fakeWorkflowCoordinator{calls: &calls}, ambient, provider)
			result, err := service.RunContext(ctx, RunRequest{OwnerIdentity: "alice"})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if result == nil || result.Status != "interrupted" || result.SourceSync == nil || result.Workflows == nil {
				t.Fatalf("expected interrupted result preserving settled work: %#v", result)
			}
			if ambient.snapshotCalls != 0 || ambient.fallbackCalls != 0 {
				t.Fatalf("ambient started after interrupted snapshot: %#v", calls)
			}
			want := "[recover:alice source:alice open-loops:alice workflows:alice pursuit-snapshot:alice]"
			if fmt.Sprint(calls) != want {
				t.Fatalf("calls = %v, want %s", calls, want)
			}
		})
	}
}

func TestOwnerCycleStopsAfterInterruptedSnapshotScan(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			calls := []string{}
			ambient := interruptingSnapshotScanner{fakeSnapshotAmbientScanner: &fakeSnapshotAmbientScanner{calls: &calls}, err: fmt.Errorf("scan: %w", cause)}
			provider := fakePursuitSnapshotProvider{fakePursuitBriefProvider: fakePursuitBriefProvider{calls: &calls}, dashboard: &pursuit.Dashboard{}}
			service := NewServiceWithPursuits(fakeSourceSyncer{calls: &calls}, fakeWorkflowCoordinator{calls: &calls}, ambient, provider)
			result, err := service.RunContext(context.Background(), RunRequest{OwnerIdentity: "alice"})
			if !errors.Is(err, cause) || result == nil || result.Status != "interrupted" {
				t.Fatalf("result/error = %#v / %v, want interrupted / %v", result, err, cause)
			}
			if result.AmbientScan == nil || result.SourceSync == nil || result.Workflows == nil {
				t.Fatal("settled phase summaries must survive interruption")
			}
			want := "[recover:alice source:alice open-loops:alice workflows:alice pursuit-snapshot:alice ambient-snapshot:alice:agent-cycle.manual]"
			if fmt.Sprint(calls) != want {
				t.Fatalf("additional phase started after interrupted scan: %v", calls)
			}
		})
	}
}

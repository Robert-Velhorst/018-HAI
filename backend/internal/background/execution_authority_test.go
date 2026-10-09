package background

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

type backgroundExpiredContext struct{ context.Context }

func (backgroundExpiredContext) Deadline() (time.Time, bool) { return time.Now().Add(-time.Hour), true }

func TestBackgroundExecutionRejectsInvalidAuthorityBeforeIntakeOrTransition(t *testing.T) {
	for _, kind := range []string{"nil", "canceled", "absolute_expiry", "ended_observation"} {
		t.Run(kind, func(t *testing.T) {
			worker, _, _ := buildWorker(t, autonomypolicy.ModeAutonomousSafe, `[]`)
			service := worker.svc
			op := createReadyRegressionOperation(t, service)
			claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
			if err != nil || claimed == nil {
				t.Fatalf("fixture claim: %+v / %v", claimed, err)
			}
			var ctx context.Context
			want := operations.ErrInvalidSourceObservation
			switch kind {
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
				want = context.Canceled
			case "absolute_expiry":
				ctx, want = backgroundExpiredContext{context.Background()}, context.DeadlineExceeded
			case "ended_observation":
				err := service.WithSourceObservation(context.Background(), operations.SourceObservationStart{
					OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, OriginID: uuid.New(), ConfigDigest: strings.Repeat("a", 64),
				}, func(observed context.Context) error { ctx = context.WithoutCancel(observed); return nil })
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := service.Events(op.ID)
			if err != nil {
				t.Fatal(err)
			}
			executor := &outcomeSafeExecutor{result: verifiedOutcomeReceipt()}
			for name, call := range map[string]func() error{
				"worker_pass": func() error {
					report, err := worker.RunOnce(ctx)
					if !reflect.DeepEqual(report, Report{}) {
						t.Errorf("rejected pass reported work: %+v", report)
					}
					return err
				},
				"claimed_execution": func() error {
					out, err := ExecuteSafeOperationClaimed(ctx, service, worker.broker, claimed.Operation, claimed.Claim, time.Now())
					if !reflect.DeepEqual(out, SafeOutcome{}) {
						t.Errorf("rejected execution reported progress: %+v", out)
					}
					return err
				},
				"synthetic_execution": func() error {
					_, err := executeSafeOperation(ctx, service, executor, op, &claimed.Claim, time.Now())
					return err
				},
				"direct_dispatch": func() error {
					_, err := executeSafeWorker(ctx, executor, executionbroker.SafeWorkerInput{})
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					if err := call(); !errors.Is(err, want) {
						t.Fatalf("got %v, want %v", err, want)
					}
				})
			}
			after, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterEvents, err := service.Events(op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if executor.calls != 0 || !reflect.DeepEqual(after, before) || !reflect.DeepEqual(afterEvents, events) {
				t.Fatal("invalid authority changed operation/audit or dispatched work")
			}
			if err := service.RenewClaim(context.Background(), claimed.Claim, time.Minute); err != nil {
				t.Fatalf("refusal lost original claim: %v", err)
			}
		})
	}
}

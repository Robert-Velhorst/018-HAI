package hostruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type cancelAfterProtocolRepository struct {
	Repository
	operation string
	cancel    context.CancelFunc
	calls     int
}

func (r *cancelAfterProtocolRepository) BeginStartIntent(ctx context.Context, worker string, job uuid.UUID, token string, binding StartIntentBinding, gate StartGate, now time.Time) (*StartIntent, error) {
	intent, err := r.Repository.BeginStartIntent(ctx, worker, job, token, binding, gate, now)
	if r.operation == "begin" {
		r.calls++
		r.cancel()
	}
	return intent, err
}

func (r *cancelAfterProtocolRepository) AcknowledgeActualStart(ctx context.Context, worker string, job uuid.UUID, token string, binding StartIntentBinding, gate StartGate, now time.Time) error {
	err := r.Repository.AcknowledgeActualStart(ctx, worker, job, token, binding, gate, now)
	if r.operation == "ack" {
		r.calls++
		r.cancel()
	}
	return err
}

func (r *cancelAfterProtocolRepository) ReportStartOutcomeUnknown(ctx context.Context, worker string, job uuid.UUID, token string, binding StartIntentBinding, now time.Time) (bool, error) {
	already, err := r.Repository.ReportStartOutcomeUnknown(ctx, worker, job, token, binding, now)
	if r.operation == "unknown" {
		r.calls++
		r.cancel()
	}
	return already, err
}

func TestStartProtocolDoesNotReportSuccessAfterRequestCancellation(t *testing.T) {
	for _, operation := range []string{"begin", "ack", "unknown"} {
		t.Run(operation, func(t *testing.T) {
			service, repository, lease := leasedStartProtocolFixture(t, nil)
			binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
			if operation != "begin" {
				if _, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "unknown" {
				if err := service.AcknowledgeActualStart(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapped := &cancelAfterProtocolRepository{Repository: repository, operation: operation, cancel: cancel}
			service.repository = wrapped
			var err error
			switch operation {
			case "begin":
				var intent *StartIntent
				intent, err = service.BeginStartIntent(ctx, lease.WorkerID, lease.Job.ID, lease.Token, binding)
				if intent != nil {
					t.Fatal("cancelled request returned a successful start intent")
				}
			case "ack":
				err = service.AcknowledgeActualStart(ctx, lease.WorkerID, lease.Job.ID, lease.Token, binding)
			case "unknown":
				var acknowledged bool
				acknowledged, err = service.ReportStartOutcomeUnknown(ctx, lease.WorkerID, lease.Job.ID, lease.Token, binding)
				if acknowledged {
					t.Fatal("cancelled request returned a successful acknowledgment")
				}
			}
			if !errors.Is(err, context.Canceled) || wrapped.calls != 1 {
				t.Fatalf("protocol cancellation: error=%v calls=%d", err, wrapped.calls)
			}
			stored := repository.jobs[lease.Job.ID]
			if stored.StartIntentID == nil || *stored.StartIntentID != binding.IntentID {
				t.Fatal("already persisted intent was lost or replaced")
			}
			if operation != "begin" && stored.ActualStartAckAt == nil {
				t.Fatal("already persisted worker acknowledgment was lost")
			}
		})
	}
}

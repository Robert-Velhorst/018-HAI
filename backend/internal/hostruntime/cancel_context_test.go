package hostruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type cancelScopeKey struct{}

type cancelScopeRepository struct {
	*memoryRepository
	calls  int
	ctx    context.Context
	onCall func()
	job    *Job
	err    error
}

func (r *cancelScopeRepository) CancelTask(ctx context.Context, _, _ string, _ uuid.UUID, _ time.Time) (*Job, bool, error) {
	r.calls++
	r.ctx = ctx
	if r.onCall != nil {
		r.onCall()
	}
	return r.job, true, r.err
}

func TestHostCancelTaskRequestScope(t *testing.T) {
	for _, mode := range []string{"valid", "nil", "cancel_before", "short_deadline", "cancel_ack", "cancel_error"} {
		t.Run(mode, func(t *testing.T) {
			parent := context.WithValue(context.Background(), cancelScopeKey{}, "operator")
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "short_deadline" {
				ctx, cancel = context.WithTimeout(parent, time.Second)
			} else {
				ctx, cancel = context.WithCancel(parent)
			}
			defer cancel()
			job := &Job{ID: uuid.New(), OwnerIdentity: "robert", TaskID: "task", Status: StatusCancelled}
			r := &cancelScopeRepository{memoryRepository: newMemoryRepository(), job: job}
			s := newTestService(r)
			if mode == "nil" {
				ctx = nil
			}
			if mode == "cancel_before" {
				cancel()
			}
			if mode == "cancel_ack" || mode == "cancel_error" {
				r.onCall = cancel
			}
			driverErr := errors.New("synthetic cancellation write error")
			if mode == "cancel_error" {
				r.err = driverErr
			}
			result, revoked, err := s.CancelTask(ctx, "robert", "task", job.ID)
			if mode == "nil" || mode == "cancel_before" {
				if err == nil || r.calls != 0 || revoked {
					t.Fatalf("invalid scope reached storage: calls=%d revoked=%v err=%v", r.calls, revoked, err)
				}
				return
			}
			if r.calls != 1 || r.ctx.Value(cancelScopeKey{}) != "operator" {
				t.Fatalf("lost scope: calls=%d", r.calls)
			}
			deadline, bounded := r.ctx.Deadline()
			if !bounded || time.Until(deadline) > AdmissionTimeout {
				t.Fatal("unbounded cancellation write")
			}
			if mode == "short_deadline" {
				parentDeadline, _ := ctx.Deadline()
				if deadline.After(parentDeadline) {
					t.Fatal("shorter parent deadline extended")
				}
			}
			if r.ctx == ctx {
				t.Fatal("shared request context used without bounded child")
			}
			if mode == "cancel_ack" || mode == "cancel_error" {
				if !errors.Is(err, context.Canceled) || revoked || result != job {
					t.Fatalf("late acknowledgment: job=%#v revoked=%v err=%v", result, revoked, err)
				}
				if mode == "cancel_error" && !errors.Is(err, driverErr) {
					t.Fatal("driver error lost")
				}
			} else if err != nil || !revoked || result != job {
				t.Fatalf("valid cancel: revoked=%v err=%v", revoked, err)
			}
		})
	}
}

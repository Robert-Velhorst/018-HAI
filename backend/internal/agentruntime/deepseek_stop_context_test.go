package agentruntime

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"github.com/google/uuid"
)

type stopContextKey struct{}

type contextualHostCancellation struct {
	capturingHostRuntimeDispatcher
	ctx      context.Context
	onCancel func()
}

func (d *contextualHostCancellation) CancelTask(ctx context.Context, owner, task string, id uuid.UUID) (*hostruntime.Job, bool, error) {
	d.ctx = ctx
	if d.onCancel != nil {
		d.onCancel()
	}
	return d.capturingHostRuntimeDispatcher.CancelTask(ctx, owner, task, id)
}

func TestDeepSeekStopRequestContext(t *testing.T) {
	for _, mode := range []string{"valid", "nil", "cancelled", "late_ack", "enqueue_cleanup"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), stopContextKey{}, "operator"))
			defer cancel()
			d := &contextualHostCancellation{}
			d.job = &hostruntime.Job{ID: uuid.New(), RuntimeID: "deepseek-harness", OwnerIdentity: "robert", TaskID: "task", Status: hostruntime.StatusPending}
			a := &deepSeekHarnessAdapter{dispatcher: d}
			var request context.Context = ctx
			if mode == "nil" {
				request = nil
			}
			if mode == "cancelled" || mode == "enqueue_cleanup" {
				cancel()
			}
			if mode == "late_ack" {
				d.onCancel = cancel
			}
			if mode == "enqueue_cleanup" {
				result := a.cancelledEnqueueOutcome(ctx, time.Now(), Task{ID: "task", OwnerIdentity: "robert"}, d.job.ID)
				if result.Status != "cancelled" || !result.durableCancellationConfirmed {
					t.Fatalf("cleanup = %#v", result)
				}
			} else {
				result := a.StopTaskWithReference(request, "task", "robert", d.job.ID.String())
				switch mode {
				case "nil", "cancelled":
					if d.cancelCalls != 0 || result.Status == "cancelled" {
						t.Fatalf("invalid request dispatched: calls=%d result=%#v", d.cancelCalls, result)
					}
					return
				case "late_ack":
					if result.Status != "indeterminate" {
						t.Fatalf("late acknowledgement = %#v", result)
					}
				default:
					if result.Status != "cancelled" {
						t.Fatalf("stop = %#v", result)
					}
				}
			}
			deadline, bounded := d.ctx.Deadline()
			if d.cancelCalls != 1 || !bounded || time.Until(deadline) > hostCancellationTimeout || d.ctx.Value(stopContextKey{}) != "operator" {
				t.Fatalf("lost bounded request scope: calls=%d bounded=%v value=%v", d.cancelCalls, bounded, d.ctx.Value(stopContextKey{}))
			}
		})
	}
}

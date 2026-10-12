package agentruntime

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"github.com/google/uuid"
)

func TestDeepSeekStopReceiptState(t *testing.T) {
	for _, path := range []string{"reference", "owner"} {
		for _, mode := range []string{"valid", "wrong_runtime", "missing_runtime", "lease_digest", "lease_expiry", "start_intent", "actual_start"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				id := uuid.New()
				now := time.Now().UTC()
				job := &hostruntime.Job{ID: id, OwnerIdentity: "robert", RuntimeID: "deepseek-harness", TaskID: "task", Status: hostruntime.StatusCancelled}
				switch mode {
				case "wrong_runtime":
					job.RuntimeID = "hermes"
				case "missing_runtime":
					job.RuntimeID = ""
				case "lease_digest":
					job.LeaseDigest = "private-lease-digest"
				case "lease_expiry":
					job.LeaseExpires = &now
				case "start_intent":
					job.StartIntentID = &id
				case "actual_start":
					job.ActualStartAckAt = &now
				}
				d := &mismatchedStopDispatcher{result: job}
				a := &deepSeekHarnessAdapter{dispatcher: d}
				var result StopResult
				if path == "reference" {
					result = a.StopTaskWithReference(context.Background(), "task", "robert", id.String())
				} else {
					result = a.StopTaskForOwner(context.Background(), "task", "robert", true)
				}
				if d.calls != 1 {
					t.Fatal("unexpected repeated cancellation")
				}
				if mode == "valid" {
					if result.Status != "cancelled" {
						t.Fatalf("valid revocation: %#v", result)
					}
				} else if result.Status != "indeterminate" {
					t.Fatalf("contradictory revocation accepted: %#v", result)
				}
			})
		}
	}
}

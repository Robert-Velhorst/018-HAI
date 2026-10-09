package hostruntime

import (
	"errors"
	"testing"

	"automation-hub-backend/internal/safety"
)

func TestServiceFailsClosedAtFinalProcessBoundaryWhenSafetyControlIsUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	repository := newMemoryRepository()
	service := newTestService(repository)
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-final-gate",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}

	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); !errors.Is(err, ErrEmergencyStopped) {
		t.Fatalf("ConfirmLease error = %v, want fail-closed emergency-stop denial", err)
	}
}

package safety

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type emergencyStopProviderStub struct {
	engaged bool
	reason  string
	err     error
}

func (s emergencyStopProviderStub) EmergencyStopStatus() (bool, string, error) {
	return s.engaged, s.reason, s.err
}

func TestEmergencyStopActiveReadsSupportedFlags(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")

	if !EmergencyStopActive() {
		t.Fatalf("emergency stop should be active")
	}
}

func TestEmergencyStopReasonIsRedacted(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP_REASON", "operator stop token=super-secret-token")

	reason := EmergencyStopReason()
	if strings.Contains(reason, "super-secret-token") {
		t.Fatalf("emergency stop reason leaked secret: %s", reason)
	}
}

func TestPersistedEmergencyStopProviderIsUsed(t *testing.T) {
	restore := SetEmergencyStopProvider(emergencyStopProviderStub{
		engaged: true,
		reason:  "operator pause token=super-secret-token",
	})
	defer restore()

	decision := EvaluateEmergencyStop()
	if !decision.Active || decision.Source != "persisted_control" {
		t.Fatalf("decision = %#v, want active persisted control", decision)
	}
	if strings.Contains(decision.Reason, "super-secret-token") {
		t.Fatalf("persisted stop reason leaked secret: %s", decision.Reason)
	}
}

func TestPersistedEmergencyStopProviderFailsClosed(t *testing.T) {
	restore := SetEmergencyStopProvider(emergencyStopProviderStub{
		err: errors.New("state file is unreadable"),
	})
	defer restore()

	decision := EvaluateEmergencyStop()
	if !decision.Active || decision.Source != "persisted_control_error" {
		t.Fatalf("decision = %#v, want fail-closed persisted control error", decision)
	}
	if strings.Contains(decision.Reason, "unreadable") {
		t.Fatalf("provider error leaked through operator reason: %s", decision.Reason)
	}
}

func TestExecutionEmergencyStopFailsClosedWhenProviderIsNotConfigured(t *testing.T) {
	restore := SetEmergencyStopProvider(nil)
	defer restore()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")

	decision := EvaluateEmergencyStopForExecution()
	if !decision.Active || decision.Source != "control_unconfigured" {
		t.Fatalf("decision = %#v, want fail-closed unconfigured execution control", decision)
	}
}

func TestExecutionEmergencyStopFailsClosedWhenProviderCannotBeRead(t *testing.T) {
	restore := SetEmergencyStopProvider(emergencyStopProviderStub{err: errors.New("store unavailable")})
	defer restore()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")

	decision := EvaluateEmergencyStopForExecution()
	if !decision.Active || decision.Source != "persisted_control_error" {
		t.Fatalf("decision = %#v, want fail-closed persisted-control error", decision)
	}
}

func TestEnvironmentHardStopTakesPrecedence(t *testing.T) {
	restore := SetEmergencyStopProvider(emergencyStopProviderStub{})
	defer restore()
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "on")

	decision := EvaluateEmergencyStop()
	if !decision.Active || decision.Source != "environment" {
		t.Fatalf("decision = %#v, want environment hard stop", decision)
	}
}

func TestEmergencyStopMutationWaitsForFinalEffectCommitFence(t *testing.T) {
	releaseCommit := AcquireExecutionCommitFence()
	attempted := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		close(attempted)
		releaseMutation := AcquireEmergencyStopMutationFence()
		close(acquired)
		releaseMutation()
	}()
	<-attempted
	select {
	case <-acquired:
		releaseCommit()
		t.Fatal("emergency stop mutation crossed an active final-effect commit fence")
	case <-time.After(25 * time.Millisecond):
	}
	releaseCommit()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("emergency stop mutation did not proceed after the commit fence was released")
	}
}

func TestExecutionCommitFenceWaitIsCancelledByAdmissionDeadline(t *testing.T) {
	releaseStop := AcquireEmergencyStopMutationFence()
	defer releaseStop()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	if release, err := AcquireExecutionCommitFenceContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("fence acquisition error = %v, want deadline exceeded", err)
	}
}

func TestWithEmergencyStopCancelsActiveExecution(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	var engaged atomic.Bool
	restore := SetEmergencyStopProvider(EmergencyStopProviderFunc(func() (bool, string, error) {
		if engaged.Load() {
			return true, "operator pause", nil
		}
		return false, "", nil
	}))
	defer restore()

	ctx, cancel := WithEmergencyStop(context.Background())
	defer cancel()
	if ctx.Err() != nil {
		t.Fatalf("context cancelled before stop activation: %v", context.Cause(ctx))
	}

	engaged.Store(true)
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrEmergencyStopActivated) {
			t.Fatalf("context cause = %v, want emergency-stop activation", context.Cause(ctx))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active execution was not cancelled after emergency-stop activation")
	}
}

func TestWithEmergencyStopFailsClosedWhenProviderBecomesUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	var unavailable atomic.Bool
	restore := SetEmergencyStopProvider(EmergencyStopProviderFunc(func() (bool, string, error) {
		if unavailable.Load() {
			return false, "", errors.New("control store unavailable")
		}
		return false, "", nil
	}))
	defer restore()

	ctx, cancel := WithEmergencyStop(context.Background())
	defer cancel()
	unavailable.Store(true)
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrEmergencyStopActivated) {
			t.Fatalf("context cause = %v, want fail-closed emergency-stop activation", context.Cause(ctx))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("execution continued after emergency-stop state became unavailable")
	}
}

func TestWithEmergencyStopFailsClosedWhenProviderIsNotConfigured(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := SetEmergencyStopProvider(nil)
	defer restore()

	ctx, cancel := WithEmergencyStop(context.Background())
	defer cancel()
	if !errors.Is(context.Cause(ctx), ErrEmergencyStopActivated) {
		t.Fatalf("context cause = %v, want fail-closed stop for missing control provider", context.Cause(ctx))
	}
}

package safety

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

const defaultEmergencyStopReason = "emergency stop is active; autonomous execution is blocked until the stop is cleared"
const unavailableEmergencyStopReason = "persisted emergency-stop state is unavailable; autonomous execution is blocked until the control plane is healthy"
const emergencyStopPollInterval = 100 * time.Millisecond

var ErrEmergencyStopActivated = errors.New("emergency stop activated during execution")

type emergencyStopContextKey struct{}

// EmergencyStopProvider supplies the persisted operator stop without coupling
// execution packages to the ops-control implementation.
type EmergencyStopProvider interface {
	EmergencyStopStatus() (engaged bool, reason string, err error)
}

// EmergencyStopRevisionProvider exposes the durable stop revision so queued
// host approvals can be invalidated by any stop/clear cycle, even if a worker
// did not poll while the stop was active.
type EmergencyStopRevisionProvider interface {
	EmergencyStopRevision() (uint64, error)
}

// EmergencyStopProviderFunc adapts a function to EmergencyStopProvider.
type EmergencyStopProviderFunc func() (engaged bool, reason string, err error)

func (f EmergencyStopProviderFunc) EmergencyStopStatus() (bool, string, error) {
	return f()
}

// EmergencyStopDecision is one atomic view of all stop sources.
type EmergencyStopDecision struct {
	Active bool
	Reason string
	Source string
}

var (
	emergencyStopProviderMu sync.RWMutex
	emergencyStopProvider   EmergencyStopProvider
	emergencyStopFence      sync.RWMutex
)

// AcquireExecutionCommitFence linearizes a short, already-authorized durable
// commit against persisted emergency-stop changes. Stop-state writers wait
// until the final-effect transaction commits or rolls back.
func AcquireExecutionCommitFence() func() {
	emergencyStopFence.RLock()
	return emergencyStopFence.RUnlock
}

// AcquireExecutionCommitFenceContext waits for the short execution-commit
// fence without making callers wait past their admission deadline.
func AcquireExecutionCommitFenceContext(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		if emergencyStopFence.TryRLock() {
			return emergencyStopFence.RUnlock, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// CurrentEmergencyStopRevision reads the persisted revision used to bind
// unstarted host work to the stop state under which it was approved.
func CurrentEmergencyStopRevision() (uint64, error) {
	emergencyStopProviderMu.RLock()
	provider := emergencyStopProvider
	emergencyStopProviderMu.RUnlock()
	if provider == nil {
		return 0, errors.New("persisted emergency-stop revision provider is unavailable")
	}
	revisionProvider, ok := provider.(EmergencyStopRevisionProvider)
	if !ok {
		return 0, errors.New("persisted emergency-stop revision is unavailable")
	}
	revision, err := revisionProvider.EmergencyStopRevision()
	if err != nil {
		return 0, fmt.Errorf("read persisted emergency-stop revision: %w", err)
	}
	return revision, nil
}

// AcquireEmergencyStopMutationFence must wrap every persisted stop mutation.
// It gives a concurrent final-effect commit a clear before-or-after ordering.
func AcquireEmergencyStopMutationFence() func() {
	emergencyStopFence.Lock()
	return emergencyStopFence.Unlock
}

// SetEmergencyStopProvider installs the process-wide persisted stop provider.
// The returned function is intended for tests that need to restore prior state.
func SetEmergencyStopProvider(provider EmergencyStopProvider) func() {
	emergencyStopProviderMu.Lock()
	previous := emergencyStopProvider
	emergencyStopProvider = provider
	emergencyStopProviderMu.Unlock()

	return func() {
		emergencyStopProviderMu.Lock()
		emergencyStopProvider = previous
		emergencyStopProviderMu.Unlock()
	}
}

func EmergencyStopActive() bool {
	return EvaluateEmergencyStop().Active
}

func EmergencyStopReason() string {
	return EvaluateEmergencyStop().Reason
}

// EvaluateEmergencyStopForExecution additionally requires the persisted
// operator control to be installed. Status/reporting callers may use
// EvaluateEmergencyStop, but an execution boundary must not treat an
// unconfigured control plane as an affirmative clear decision.
func EvaluateEmergencyStopForExecution() EmergencyStopDecision {
	decision := EvaluateEmergencyStop()
	if decision.Source != "none" {
		return decision
	}
	return EmergencyStopDecision{
		Active: true,
		Reason: unavailableEmergencyStopReason,
		Source: "control_unconfigured",
	}
}

// WithEmergencyStop derives a context that is cancelled when any configured
// emergency-stop source engages or becomes unavailable. The persisted control
// plane currently exposes a synchronous status contract, so active executions
// poll it at a bounded interval. Nested HAI execution layers reuse the same
// monitor instead of creating more polling goroutines.
func WithEmergencyStop(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if parent.Value(emergencyStopContextKey{}) != nil {
		return parent, func() {}
	}

	ctx, cancelCause := context.WithCancelCause(parent)
	ctx = context.WithValue(ctx, emergencyStopContextKey{}, true)
	cancel := func() { cancelCause(context.Canceled) }
	finish, admitted := lifecycle.Enter(ctx, "emergency-stop-setup")
	if !admitted {
		cancel()
		return ctx, cancel
	}
	defer finish()
	if decision := EvaluateEmergencyStopForExecution(); decision.Active {
		cancelCause(fmt.Errorf("%w: %s", ErrEmergencyStopActivated, decision.Reason))
		return ctx, cancel
	}

	if !lifecycle.Go(ctx, "emergency-stop-monitor", func() {
		ticker := time.NewTicker(emergencyStopPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				decision := EvaluateEmergencyStopForExecution()
				if decision.Active {
					cancelCause(fmt.Errorf("%w: %s", ErrEmergencyStopActivated, decision.Reason))
					return
				}
			}
		}
	}) {
		cancel()
	}
	return ctx, cancel
}

// EvaluateEmergencyStop checks immutable environment hard stops first, then
// the persisted operator control. A configured provider failure blocks
// execution: inability to prove that the stop is clear is not permission to
// continue.
func EvaluateEmergencyStop() EmergencyStopDecision {
	if environmentEmergencyStopActive() {
		reason := strings.TrimSpace(os.Getenv("HAI_EMERGENCY_STOP_REASON"))
		if reason == "" {
			reason = defaultEmergencyStopReason
		}
		return EmergencyStopDecision{
			Active: true,
			Reason: RedactSecrets(reason),
			Source: "environment",
		}
	}

	emergencyStopProviderMu.RLock()
	provider := emergencyStopProvider
	emergencyStopProviderMu.RUnlock()
	if provider == nil {
		return EmergencyStopDecision{
			Reason: defaultEmergencyStopReason,
			Source: "none",
		}
	}

	engaged, reason, err := provider.EmergencyStopStatus()
	if err != nil {
		return EmergencyStopDecision{
			Active: true,
			Reason: unavailableEmergencyStopReason,
			Source: "persisted_control_error",
		}
	}
	if !engaged {
		return EmergencyStopDecision{
			Reason: defaultEmergencyStopReason,
			Source: "persisted_control",
		}
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = defaultEmergencyStopReason
	}
	return EmergencyStopDecision{
		Active: true,
		Reason: RedactSecrets(reason),
		Source: "persisted_control",
	}
}

func environmentEmergencyStopActive() bool {
	return truthyEnv("HAI_EMERGENCY_STOP") ||
		truthyEnv("AUTONOMY_EMERGENCY_STOP") ||
		truthyEnv("EMERGENCY_STOP")
}

func truthyEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

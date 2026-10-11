package safety

import (
	"os"
	"strings"
	"sync"
)

const defaultEmergencyStopReason = "emergency stop is active; autonomous execution is blocked until the stop is cleared"
const unavailableEmergencyStopReason = "persisted emergency-stop state is unavailable; autonomous execution is blocked until the control plane is healthy"

// EmergencyStopProvider supplies the persisted operator stop without coupling
// execution packages to the ops-control implementation.
type EmergencyStopProvider interface {
	EmergencyStopStatus() (engaged bool, reason string, err error)
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
)

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

// EvaluateEmergencyStop checks immutable environment hard stops first, then
// the persisted operator control. A configured provider failure blocks
// execution: inability to prove that the stop is clear is not permission to
// continue.
func EvaluateEmergencyStop() EmergencyStopDecision {
	if immutable := EvaluateImmutableEmergencyStop(); immutable.Active {
		return immutable
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

// EvaluateImmutableEmergencyStop reads only deployment-level hard stops. It is
// used by the narrowly-scoped recovery authorizer: an owner may clear the
// persisted operator stop through exact approval, but can never override an
// environment stop from inside the application.
func EvaluateImmutableEmergencyStop() EmergencyStopDecision {
	if !environmentEmergencyStopActive() {
		return EmergencyStopDecision{
			Reason: defaultEmergencyStopReason,
			Source: "environment",
		}
	}
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

package hostruntime

import (
	"os"
	"testing"

	"automation-hub-backend/internal/safety"
)

type hostRuntimeTestStopProvider struct{}

func (hostRuntimeTestStopProvider) EmergencyStopStatus() (bool, string, error) {
	return false, "", nil
}

func (hostRuntimeTestStopProvider) EmergencyStopRevision() (uint64, error) {
	return 1, nil
}

func TestMain(m *testing.M) {
	for _, name := range []string{"HAI_EMERGENCY_STOP", "AUTONOMY_EMERGENCY_STOP", "EMERGENCY_STOP"} {
		_ = os.Setenv(name, "false")
	}
	restore := safety.SetEmergencyStopProvider(hostRuntimeTestStopProvider{})
	code := m.Run()
	restore()
	os.Exit(code)
}

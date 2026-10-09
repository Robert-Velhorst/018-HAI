package workflow

import (
	"os"
	"testing"

	"automation-hub-backend/internal/safety"
)

func TestMain(m *testing.M) {
	for _, name := range []string{"HAI_EMERGENCY_STOP", "AUTONOMY_EMERGENCY_STOP", "EMERGENCY_STOP"} {
		_ = os.Setenv(name, "false")
	}
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return false, "", nil
	}))
	code := m.Run()
	restore()
	os.Exit(code)
}

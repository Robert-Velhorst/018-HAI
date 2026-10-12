package automation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func TestAPILaunchFailsClosedWhenEmergencyStopControlIsUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()

	var networkCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		networkCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	service := &service{executionAuth: &recordingExecutionAuthorizer{}}
	result := service.executeAPILaunch(
		&models.Automation{
			ID: uuid.New(), LaunchType: "api", LaunchTarget: "POST " + server.URL,
			ExpectedHTTPStatus: http.StatusNoContent,
		},
		TaskLaunchRequest{OwnerIdentity: "alice"},
		uuid.New(), time.Now().UTC(), nil,
	)
	if result.Status != "blocked" || !strings.Contains(result.Message, "persisted emergency-stop state is unavailable") {
		t.Fatalf("result = %#v, want fail-closed emergency-stop block", result)
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("network calls = %d, want zero while safety control is unavailable", networkCalls.Load())
	}
}

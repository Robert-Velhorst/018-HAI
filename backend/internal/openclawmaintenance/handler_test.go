package openclawmaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"leaseToken":"a"}{"leaseToken":"b"}`))
	var body struct {
		Token string `json:"leaseToken"`
	}
	if decode(c, &body) {
		t.Fatal("accepted multiple request bodies")
	}
}

func TestWorkerRejectsMissingOrWrongCredentials(t *testing.T) {
	for _, header := range []string{"", "Bearer wrong", strings.Repeat("s", 32)} {
		h := &Handler{enabled: true, token: strings.Repeat("s", 32)}
		r := gin.New()
		r.POST("/", h.WorkerAuth, func(c *gin.Context) { c.Status(204) })
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Authorization", header)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != 401 {
			t.Fatalf("accepted malformed credential: %d", res.Code)
		}
	}
}

func TestWorkerContactStatusDoesNotInferLivenessFromConfigurationOrQueue(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	h := &Handler{enabled: true}
	if got := h.workerContactStatus(now); got.State != "unknown" || got.LastSeenAt != nil {
		t.Fatalf("never-observed worker status = %+v, want unknown", got)
	}
	h.workerMu.Lock()
	h.workerLastSeen = now.Add(-time.Minute)
	h.workerMu.Unlock()
	if got := h.workerContactStatus(now); got.State != "recent_contact" || got.LastSeenAt == nil || !got.LastSeenAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("recent worker contact = %+v", got)
	}
	if got := h.workerContactStatus(now.Add(maintenanceWorkerContactTTL + time.Minute)); got.State != "stale_contact" {
		t.Fatalf("stale worker contact = %+v", got)
	}
	h.enabled = false
	if got := h.workerContactStatus(now); got.State != "disabled" {
		t.Fatalf("disabled maintenance worker contact = %+v", got)
	}
}

func TestWorkerAuthRecordsOnlyAuthenticatedContact(t *testing.T) {
	token := strings.Repeat("w", 64)
	h := &Handler{enabled: true, token: token}
	r := gin.New()
	r.POST("/", h.WorkerAuth, func(c *gin.Context) { c.Status(204) })

	bad := httptest.NewRequest("POST", "/", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	capabilityJSON, err := json.Marshal(verifiedCompanionCapability())
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("X-HAI-Worker-Capability", string(capabilityJSON))
	badResponse := httptest.NewRecorder()
	r.ServeHTTP(badResponse, bad)
	if badResponse.Code != 401 || !h.workerLastSeen.IsZero() || h.targetInstallationCapability("companion", time.Now().UTC()).Available {
		t.Fatalf("unauthenticated request affected worker contact: status=%d seen=%s", badResponse.Code, h.workerLastSeen)
	}

	good := httptest.NewRequest("POST", "/", nil)
	good.Header.Set("Authorization", "Bearer "+token)
	good.Header.Set("X-HAI-Worker-Capability", string(capabilityJSON))
	goodResponse := httptest.NewRecorder()
	r.ServeHTTP(goodResponse, good)
	if goodResponse.Code != 204 || h.workerContactStatus(time.Now().UTC()).State != "recent_contact" || !h.targetInstallationCapability("companion", time.Now().UTC()).Available {
		t.Fatalf("authenticated worker contact was not observed: status=%d worker=%+v", goodResponse.Code, h.workerContactStatus(time.Now().UTC()))
	}

	missingCapability := httptest.NewRequest("POST", "/", nil)
	missingCapability.Header.Set("Authorization", "Bearer "+token)
	missingResponse := httptest.NewRecorder()
	r.ServeHTTP(missingResponse, missingCapability)
	if missingResponse.Code != 204 || h.targetInstallationCapability("companion", time.Now().UTC()).Available {
		t.Fatal("authenticated request without a capability left a prior install lease valid")
	}
}

func TestWorkerTokenCannotReuseOtherRuntimeAuthority(t *testing.T) {
	setMaintenanceTestEnvironment(t, "true", strings.Repeat("a", 32))
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", strings.Repeat("a", 32))
	h := NewHandler(nil)
	if h.enabled {
		t.Fatal("reused delegation credential for maintenance")
	}
}

func TestMaintenanceDefaultsEnabledWhenWorkerPrerequisitesExist(t *testing.T) {
	setMaintenanceTestEnvironment(t, "", strings.Repeat("a", 64))
	h := NewHandler(nil)
	if !h.enabled {
		t.Fatal("maintenance should default on with valid dedicated worker credentials")
	}
	if !maintenanceConfigEnabledFromEnvironment() {
		t.Fatal("runtime scheduler configuration disagrees with handler default")
	}
}

func TestMaintenanceExplicitOptOutDisablesHandlerAndScheduler(t *testing.T) {
	setMaintenanceTestEnvironment(t, "false", strings.Repeat("a", 64))
	if NewHandler(nil).enabled {
		t.Fatal("explicit false must disable maintenance")
	}
	if err := StartScheduler(context.Background(), nil); err != nil {
		t.Fatalf("disabled scheduler should return without requiring a database: %v", err)
	}
}

func TestMaintenanceScheduleDelayFollowsPersisted24HourDeadline(t *testing.T) {
	checkedAt := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	due := checkedAt.Add(checkInterval)
	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{name: "one nanosecond before the 24 hour boundary", now: due.Add(-time.Nanosecond), want: time.Nanosecond},
		{name: "exactly at the 24 hour boundary", now: due, want: 0},
		{name: "after the 24 hour boundary catches up immediately", now: due.Add(time.Minute), want: 0},
		{name: "far deadline uses low wakeup refresh cap", now: checkedAt, want: maintenanceScheduleRefreshPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maintenanceScheduleDelay(tt.now, due, true, time.Time{}); got != tt.want {
				t.Fatalf("schedule delay = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMaintenanceScheduleDelayPreservesErrorRetryAndBoundsRecovery(t *testing.T) {
	now := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	if got := maintenanceScheduleDelay(now, now.Add(-time.Second), true, now.Add(maintenanceScheduleErrorRetry)); got != maintenanceScheduleErrorRetry {
		t.Fatalf("database scheduling retry delay = %s, want %s", got, maintenanceScheduleErrorRetry)
	}
	if got := maintenanceScheduleDelay(now, time.Time{}, false, now.Add(time.Minute)); got != time.Minute {
		t.Fatalf("refresh after a deadline-query failure = %s, want one minute", got)
	}
	if want := 20 * time.Minute; maintenanceRecoveryScanInterval+maintenanceRunnerPollInterval != want {
		t.Fatalf("durable fallback lateness = %s, want %s", maintenanceRecoveryScanInterval+maintenanceRunnerPollInterval, want)
	}
}

func TestMaintenanceScheduleRetryRunsWithoutPersistedDeadline(t *testing.T) {
	now := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	retryAt := now.Add(maintenanceScheduleErrorRetry)
	delay := maintenanceScheduleDelay(now, time.Time{}, false, retryAt)
	if delay != maintenanceScheduleErrorRetry {
		t.Fatalf("retry without a persisted deadline waits %s, want %s", delay, maintenanceScheduleErrorRetry)
	}
	if !maintenanceScheduleReady(now.Add(delay), time.Time{}, false, retryAt, nil) {
		t.Fatal("elapsed scheduling retry did not trigger a new scheduling attempt without a persisted deadline")
	}
	tests := []struct {
		name        string
		due         time.Time
		hasDue      bool
		retryAt     time.Time
		deadlineErr error
		want        bool
	}{
		{
			name:    "retry after schedule failure with no deadline",
			retryAt: now,
			want:    true,
		},
		{
			name:        "retry after deadline lookup failure",
			retryAt:     now,
			deadlineErr: errors.New("database unavailable"),
			want:        true,
		},
		{
			name:    "wait before retry deadline",
			retryAt: now.Add(time.Nanosecond),
			want:    false,
		},
		{
			name:   "run when persisted check is due",
			due:    now,
			hasDue: true,
			want:   true,
		},
		{
			name:   "do not run before persisted check is due",
			due:    now.Add(time.Nanosecond),
			hasDue: true,
			want:   false,
		},
		{
			name:        "deadline error is not treated as a due check without retry",
			due:         now.Add(-time.Second),
			hasDue:      true,
			deadlineErr: errors.New("database unavailable"),
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maintenanceScheduleReady(now, tt.due, tt.hasDue, tt.retryAt, tt.deadlineErr); got != tt.want {
				t.Fatalf("maintenanceScheduleReady() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestMaintenanceMissingOrInvalidPrerequisitesFailClosed(t *testing.T) {
	tests := []struct {
		name        string
		flag, token string
		other       string
	}{
		{name: "missing token"},
		{name: "short token", token: "short"},
		{name: "non literal token", token: strings.Repeat("a", 31) + "\n"},
		{name: "reused backend key", token: strings.Repeat("a", 64), other: strings.Repeat("a", 64)},
		{name: "invalid flag", flag: "enabled", token: strings.Repeat("a", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if maintenanceConfigEnabled(tt.flag, tt.token, tt.other) {
				t.Fatal("maintenance enabled without valid prerequisites")
			}
		})
	}
}

func setMaintenanceTestEnvironment(t *testing.T, flag, token string) {
	t.Helper()
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_ENABLED", flag)
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_TOKEN", token)
	for _, name := range []string{
		"BACKEND_API_SHARED_KEY",
		"HAI_HOST_RUNTIME_BRIDGE_TOKEN",
		"OPENCLAW_GATEWAY_TOKEN",
		"OPENCLAW_GATEWAY_DELEGATION_TOKEN",
	} {
		t.Setenv(name, "")
	}
}

package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/doctor"

	"github.com/gin-gonic/gin"
)

func serveReadiness(t *testing.T, report doctor.Report) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readinessHandler(func(context.Context) doctor.Report { return report })(c)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, body
}

func serveReadinessDetail(t *testing.T, report doctor.Report) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/readiness", nil)
	readinessDetailHandler(func(context.Context) doctor.Report { return report })(c)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness detail body: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, body
}

func TestReadinessReadyWhenEverythingPasses(t *testing.T) {
	report := doctor.Report{Checks: []doctor.Check{
		{Name: "database.host", Severity: doctor.SeverityOK, Detail: "postgres"},
		{Name: "database.connection", Severity: doctor.SeverityOK, Detail: "reachable in 3ms"},
	}}
	code, body := serveReadiness(t, report)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if body["status"] != "ready" {
		t.Fatalf("status = %v, want ready", body["status"])
	}
	if _, exposed := body["checks"]; exposed {
		t.Fatal("public readiness response exposed detailed checks")
	}
}

func TestPublicHealthResponsesAreNeverCacheable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{name: "liveness", handler: livenessHandler},
		{name: "readiness", handler: readinessHandler(func(context.Context) doctor.Report { return doctor.Report{} })},
		{name: "detailed readiness", handler: readinessDetailHandler(func(context.Context) doctor.Report { return doctor.Report{} })},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			test.handler(c)
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestReadinessDetailIncludesChecksForAuthenticatedSystemRoute(t *testing.T) {
	report := doctor.Report{Checks: []doctor.Check{
		{Name: "database.host", Severity: doctor.SeverityOK, Detail: "postgres"},
	}}
	code, body := serveReadinessDetail(t, report)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("checks = %#v, want one detailed check", body["checks"])
	}
}

// A warning means the service is serving but something is missing or unused —
// no LLM provider, Kafka down, a placeholder secret. It must stay 200 (it can
// serve) while still being visibly distinct from a clean "ready".
func TestReadinessDegradedWhenWarningsButNoFailures(t *testing.T) {
	report := doctor.Report{Checks: []doctor.Check{
		{Name: "database.connection", Severity: doctor.SeverityOK, Detail: "reachable in 3ms"},
		{Name: "kafka.connection", Severity: doctor.SeverityWarn, Detail: "unreachable: no brokers"},
	}}
	code, body := serveReadiness(t, report)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (a degraded service is still serving)", code)
	}
	if body["status"] != "degraded" {
		t.Fatalf("status = %v, want degraded", body["status"])
	}
}

// The regression this whole change exists to prevent: a sound configuration
// whose database is unreachable must not report itself ready.
func TestReadinessNotReadyWhenLiveDependencyFailsDespiteValidConfig(t *testing.T) {
	report := doctor.Report{Checks: []doctor.Check{
		{Name: "database.host", Severity: doctor.SeverityOK, Detail: "postgres-automation"},
		{Name: "database.port", Severity: doctor.SeverityOK, Detail: "5432"},
		{Name: "database.connection", Severity: doctor.SeverityFail, Detail: "unreachable after 3s: connection refused"},
	}}
	code, body := serveReadiness(t, report)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 when the database is unreachable", code)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", body["status"])
	}
}

func TestReadinessNotReadyWhenAnyFailure(t *testing.T) {
	report := doctor.Report{Checks: []doctor.Check{
		{Name: "database.host", Severity: doctor.SeverityFail, Detail: "DB_HOST empty"},
	}}
	code, body := serveReadiness(t, report)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", code)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", body["status"])
	}
}

func TestReadinessCoalescesOnlyOverlappingLiveProbes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	handler := readinessHandler(func(context.Context) doctor.Report {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return doctor.Report{Checks: []doctor.Check{{Name: "database.connection", Severity: doctor.SeverityOK}}}
	})

	router := gin.New()
	router.GET("/readyz", handler)
	server := httptest.NewServer(router)
	defer server.Close()

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := http.Get(server.URL + "/readyz")
			if err != nil {
				t.Errorf("readiness request: %v", err)
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Errorf("readiness status = %d, want 200", response.StatusCode)
			}
		}()
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first readiness probe did not start")
	}
	// Give the second request a short scheduling window to join the active run.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("overlapping readiness calls = %d, want one probe run", got)
	}

	response, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("fresh readiness request: %v", err)
	}
	response.Body.Close()
	if got := calls.Load(); got != 2 {
		t.Fatalf("sequential readiness calls = %d, want a fresh second probe run", got)
	}
}

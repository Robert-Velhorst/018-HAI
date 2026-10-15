package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/ambientmonitor"

	"github.com/gin-gonic/gin"
)

func TestEnabledExporterRequiresSeparateBearerToken(t *testing.T) {
	if _, err := New(true, ""); err == nil {
		t.Fatal("enabled metrics must require a token")
	}

	gin.SetMode(gin.TestMode)
	exporter, err := New(true, "metrics-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	router := gin.New()
	router.Use(exporter.Middleware())
	router.GET("/work/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/metrics", exporter.RequireBearerToken(), gin.WrapH(exporter.Handler()))

	work := httptest.NewRecorder()
	router.ServeHTTP(work, httptest.NewRequest(http.MethodGet, "/work/not-a-label", nil))
	if work.Code != http.StatusNoContent {
		t.Fatalf("work status = %d, want 204", work.Code)
	}

	blocked := httptest.NewRecorder()
	router.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("metrics without token status = %d, want 401", blocked.Code)
	}

	metrics := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer metrics-secret")
	router.ServeHTTP(metrics, request)
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", metrics.Code)
	}
	body := metrics.Body.String()
	if !strings.Contains(body, `hai_http_requests_total{method="GET",route="/work/:id",status="204"} 1`) {
		t.Fatalf("metrics did not record templated route: %s", body)
	}
	if strings.Contains(body, "not-a-label") {
		t.Fatalf("raw path leaked into metrics labels: %s", body)
	}
}

func TestOutcomeMonitorMetricsUseFixedLabelsAndExposeRealOutcomes(t *testing.T) {
	exporter, err := New(true, "metrics-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	exporter.ObserveOutcomeMonitorSweep(ambientmonitor.SweepMetrics{
		Duration: 250 * time.Millisecond, Result: ambientmonitor.SweepResultCompleted,
		DueCollectionScopes: 2, DueCompositionScopes: 1,
		CollectionLeasesRecovered: 1, CompositionLeasesRecovered: 2,
		CollectionClaimed: 3, CollectionCompleted: 2, CollectionFailed: 1,
		CompositionClaimed: 4, CompositionSucceeded: 2, CompositionRetrying: 1, CompositionFailed: 1,
	})
	metrics := httptest.NewRecorder()
	exporter.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	for _, expected := range []string{
		`hai_outcome_monitor_sweeps_total{result="completed"} 1`,
		`hai_outcome_monitor_due_scopes_discovered{kind="collection"} 2`,
		`hai_outcome_monitor_items_total{result="retrying",stage="composition"} 1`,
		`hai_outcome_monitor_items_total{result="failed",stage="collection"} 1`,
		`hai_outcome_monitor_last_success_timestamp_seconds`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics did not contain %q: %s", expected, body)
		}
	}
	for _, sensitive := range []string{"owner-a", "workspace-a", "target-", "prompt text", "private@example.com"} {
		if strings.Contains(body, sensitive) {
			t.Errorf("metrics leaked %q: %s", sensitive, body)
		}
	}
}

func TestOutcomeMonitorMetricsNormalizeUnknownResult(t *testing.T) {
	exporter, err := New(true, "metrics-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	exporter.ObserveOutcomeMonitorSweep(ambientmonitor.SweepMetrics{Result: "owner@example.com"})
	metrics := httptest.NewRecorder()
	exporter.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	if !strings.Contains(body, `hai_outcome_monitor_sweeps_total{result="failed"} 1`) || strings.Contains(body, "owner@example.com") {
		t.Fatalf("unknown result was not normalized safely: %s", body)
	}
}

func TestSkippedOutcomeMonitorSweepDoesNotPolluteDurationHistogram(t *testing.T) {
	exporter, err := New(true, "metrics-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	exporter.ObserveOutcomeMonitorSweep(ambientmonitor.SweepMetrics{Result: ambientmonitor.SweepResultSkipped})
	metrics := httptest.NewRecorder()
	exporter.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), "hai_outcome_monitor_sweep_duration_seconds_count 0") {
		t.Fatalf("skipped sweep was counted as a zero-duration observation: %s", metrics.Body.String())
	}
}

func TestEnabledOutcomeMonitorMetricsExposeStableZeroSeries(t *testing.T) {
	exporter, err := New(true, "metrics-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	metrics := httptest.NewRecorder()
	exporter.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	for _, expected := range []string{
		`hai_outcome_monitor_sweeps_total{result="completed"} 0`,
		`hai_outcome_monitor_sweeps_total{result="interrupted"} 0`,
		`hai_outcome_monitor_due_scopes_discovered{kind="collection"} 0`,
		`hai_outcome_monitor_leases_recovered_total{kind="composition"} 0`,
		`hai_outcome_monitor_items_total{result="claimed",stage="collection"} 0`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics did not expose stable zero series %q: %s", expected, body)
		}
	}
}

func TestDisabledExporterDoesNotCollectRequests(t *testing.T) {
	exporter, err := New(false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if exporter.Enabled() {
		t.Fatal("disabled exporter reported enabled")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(exporter.Middleware())
	router.GET("/work", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/work", nil))

	metrics := httptest.NewRecorder()
	exporter.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(metrics.Body.String(), "hai_http_requests_total") {
		t.Fatalf("disabled exporter collected request metrics: %s", metrics.Body.String())
	}
}

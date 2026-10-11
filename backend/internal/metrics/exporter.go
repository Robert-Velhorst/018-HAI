// Package metrics exposes a small, opt-in Prometheus surface for HAI service
// health. It deliberately tracks bounded operational mechanics only: source
// contents, prompts, identities, credentials, and record IDs never become
// labels.
package metrics

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/ambientmonitor"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	enabledEnv = "HAI_PROMETHEUS_ENABLED"
	tokenEnv   = "HAI_PROMETHEUS_TOKEN"
)

// Exporter owns an isolated registry so tests and embedded deployments do not
// conflict through Prometheus's global registry.
type Exporter struct {
	enabled               bool
	token                 string
	registry              *prometheus.Registry
	requests              *prometheus.CounterVec
	duration              *prometheus.HistogramVec
	monitorSweepDuration  prometheus.Histogram
	monitorSweeps         *prometheus.CounterVec
	monitorDueScopes      *prometheus.GaugeVec
	monitorLeaseRecovered *prometheus.CounterVec
	monitorItems          *prometheus.CounterVec
	monitorLastSuccess    prometheus.Gauge
}

// NewFromEnv creates a disabled exporter unless HAI_PROMETHEUS_ENABLED is set
// to true. An enabled endpoint must always have a separate bearer token.
func NewFromEnv() (*Exporter, error) {
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true")
	return New(enabled, strings.TrimSpace(os.Getenv(tokenEnv)))
}

// New creates the exporter. It fails closed when metrics were requested but
// no authentication boundary was configured.
func New(enabled bool, token string) (*Exporter, error) {
	if enabled && strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%s must be set when %s=true", tokenEnv, enabledEnv)
	}

	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hai",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Completed HAI HTTP requests by matched route, method, and status.",
	}, []string{"method", "route", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "hai",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HAI HTTP request duration by matched route and method.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})
	monitorSweepDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "sweep_duration_seconds",
		Help:    "Duration of bounded durable outcome-monitor sweeps.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})
	monitorSweeps := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "sweeps_total",
		Help: "Durable outcome-monitor sweeps by bounded result.",
	}, []string{"result"})
	monitorDueScopes := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "due_scopes_discovered",
		Help: "Scopes discovered by the latest bounded outcome-monitor sweep; this is capped, not an exact global backlog.",
	}, []string{"kind"})
	monitorLeaseRecovered := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "leases_recovered_total",
		Help: "Expired outcome-monitor leases recovered by lease class.",
	}, []string{"kind"})
	monitorItems := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "items_total",
		Help: "Bounded outcome-monitor item outcomes by stage and result.",
	}, []string{"stage", "result"})
	monitorLastSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "hai", Subsystem: "outcome_monitor", Name: "last_success_timestamp_seconds",
		Help: "Unix timestamp of the latest completed durable outcome-monitor sweep.",
	})
	registry.MustRegister(
		requests, duration, monitorSweepDuration, monitorSweeps, monitorDueScopes,
		monitorLeaseRecovered, monitorItems, monitorLastSuccess,
	)

	exporter := &Exporter{
		enabled:               enabled,
		token:                 token,
		registry:              registry,
		requests:              requests,
		duration:              duration,
		monitorSweepDuration:  monitorSweepDuration,
		monitorSweeps:         monitorSweeps,
		monitorDueScopes:      monitorDueScopes,
		monitorLeaseRecovered: monitorLeaseRecovered,
		monitorItems:          monitorItems,
		monitorLastSuccess:    monitorLastSuccess,
	}
	if enabled {
		exporter.initializeOutcomeMonitorSeries()
	}
	return exporter, nil
}

func (e *Exporter) Enabled() bool { return e != nil && e.enabled }

func (e *Exporter) initializeOutcomeMonitorSeries() {
	for _, result := range []string{
		ambientmonitor.SweepResultCompleted,
		ambientmonitor.SweepResultFailed,
		ambientmonitor.SweepResultInterrupted,
		ambientmonitor.SweepResultSkipped,
	} {
		e.monitorSweeps.WithLabelValues(result)
	}
	for _, kind := range []string{"collection", "composition"} {
		e.monitorDueScopes.WithLabelValues(kind).Set(0)
		e.monitorLeaseRecovered.WithLabelValues(kind)
	}
	for _, outcome := range [][2]string{
		{"collection", "claimed"}, {"collection", "completed"}, {"collection", "failed"},
		{"composition", "claimed"}, {"composition", "completed"},
		{"composition", "retrying"}, {"composition", "failed"},
	} {
		e.monitorItems.WithLabelValues(outcome[0], outcome[1])
	}
}

// ObserveOutcomeMonitorSweep records only fixed-cardinality scheduler outcomes.
// The scheduler summary contains no tenant identity or user-controlled labels.
func (e *Exporter) ObserveOutcomeMonitorSweep(observation ambientmonitor.SweepMetrics) {
	if !e.Enabled() {
		return
	}
	result := observation.Result
	switch result {
	case ambientmonitor.SweepResultCompleted, ambientmonitor.SweepResultFailed,
		ambientmonitor.SweepResultInterrupted, ambientmonitor.SweepResultSkipped:
	default:
		result = ambientmonitor.SweepResultFailed
	}
	e.monitorSweepDuration.Observe(max(0, observation.Duration.Seconds()))
	e.monitorSweeps.WithLabelValues(result).Inc()
	e.monitorDueScopes.WithLabelValues("collection").Set(float64(max(0, observation.DueCollectionScopes)))
	e.monitorDueScopes.WithLabelValues("composition").Set(float64(max(0, observation.DueCompositionScopes)))
	e.monitorLeaseRecovered.WithLabelValues("collection").Add(float64(max(0, observation.CollectionLeasesRecovered)))
	e.monitorLeaseRecovered.WithLabelValues("composition").Add(float64(max(0, observation.CompositionLeasesRecovered)))
	recordMonitorItems(e.monitorItems, "collection", "claimed", observation.CollectionClaimed)
	recordMonitorItems(e.monitorItems, "collection", "completed", observation.CollectionCompleted)
	recordMonitorItems(e.monitorItems, "collection", "failed", observation.CollectionFailed)
	recordMonitorItems(e.monitorItems, "composition", "claimed", observation.CompositionClaimed)
	recordMonitorItems(e.monitorItems, "composition", "completed", observation.CompositionSucceeded)
	recordMonitorItems(e.monitorItems, "composition", "retrying", observation.CompositionRetrying)
	recordMonitorItems(e.monitorItems, "composition", "failed", observation.CompositionFailed)
	if result == ambientmonitor.SweepResultCompleted {
		e.monitorLastSuccess.SetToCurrentTime()
	}
}

func recordMonitorItems(counter *prometheus.CounterVec, stage, result string, count int) {
	if count > 0 {
		counter.WithLabelValues(stage, result).Add(float64(count))
	}
}

// Middleware records only route templates. It avoids raw paths so UUIDs,
// emails, document names, and other user-provided values cannot create labels.
func (e *Exporter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !e.Enabled() || c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		status := strconv.Itoa(c.Writer.Status())
		e.requests.WithLabelValues(method, route, status).Inc()
		e.duration.WithLabelValues(method, route).Observe(time.Since(started).Seconds())
	}
}

// RequireBearerToken protects the local scrape endpoint without involving the
// browser-oriented API identity stack. Prometheus can send this static token
// from a local secret file; the endpoint is never enabled by default.
func (e *Exporter) RequireBearerToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		providedHash := sha256.Sum256([]byte(provided))
		expectedHash := sha256.Sum256([]byte(e.token))
		if subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "metrics bearer token required"})
			return
		}
		c.Next()
	}
}

// Handler returns the Prometheus text exposition handler for the isolated
// registry. The caller must register RequireBearerToken before this handler.
func (e *Exporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

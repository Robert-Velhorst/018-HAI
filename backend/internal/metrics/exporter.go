// Package metrics exposes a small, opt-in Prometheus surface for HAI service
// health. It deliberately tracks HTTP mechanics only: source contents,
// prompts, identities, credentials, and record IDs never become labels.
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
	enabled  bool
	token    string
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
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
	registry.MustRegister(requests, duration)

	return &Exporter{
		enabled:  enabled,
		token:    token,
		registry: registry,
		requests: requests,
		duration: duration,
	}, nil
}

func (e *Exporter) Enabled() bool { return e != nil && e.enabled }

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

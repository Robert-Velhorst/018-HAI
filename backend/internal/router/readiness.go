package router

import (
	"context"
	"net/http"
	"sync"

	"automation-hub-backend/internal/doctor"

	"github.com/gin-gonic/gin"
)

func livenessHandler(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "backend"})
}

// readinessHandler exposes only the aggregate readiness state needed by a
// load balancer or container orchestrator. Detailed checks can contain
// internal topology and configuration information, so they are served by the
// authenticated system readiness endpoint instead.
//
// The three states are distinct on purpose, because an operator needs to tell
// them apart:
//
//	ready     — configuration is sound and every dependency answered.
//	degraded  — serving, but something is missing or unused: no LLM provider,
//	            Kafka down (events dropped), a placeholder secret still in place.
//	not_ready — a critical dependency is unreachable. 503, so an orchestrator
//	            stops sending traffic to a process that cannot serve it.
//
// Only a critical failure returns 503. A degraded service is still a serving
// service, and reporting it as down would be its own kind of dishonesty.
//
// The diagnose function is injected so the handler is testable without touching
// global configuration or opening real sockets.
func readinessHandler(diagnose func(ctx context.Context) doctor.Report) gin.HandlerFunc {
	return readinessResponseHandler(diagnose, false)
}

// readinessDetailHandler exposes the same live report with individual checks.
// Route registration must keep this handler behind owner authentication and
// read permission checks.
func readinessDetailHandler(diagnose func(ctx context.Context) doctor.Report) gin.HandlerFunc {
	return readinessResponseHandler(diagnose, true)
}

func readinessResponseHandler(diagnose func(ctx context.Context) doctor.Report, includeChecks bool) gin.HandlerFunc {
	probes := &readinessProbes{}
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		report, completed := probes.run(c.Request.Context(), diagnose)
		if !completed {
			return
		}
		ok, warn, fail := report.Counts()

		status := "ready"
		code := http.StatusOK
		switch {
		case fail > 0:
			status = "not_ready"
			code = http.StatusServiceUnavailable
		case warn > 0:
			status = "degraded"
		}

		response := gin.H{
			"status":  status,
			"service": "backend",
			"summary": gin.H{"ok": ok, "warn": warn, "fail": fail},
		}
		if includeChecks {
			response["checks"] = report.Checks
		}
		c.JSON(code, response)
	}
}

type readinessFlight struct {
	done   chan struct{}
	report doctor.Report
}

type readinessProbes struct {
	mu     sync.Mutex
	flight *readinessFlight
}

func (p *readinessProbes) run(
	ctx context.Context,
	diagnose func(context.Context) doctor.Report,
) (doctor.Report, bool) {
	p.mu.Lock()
	if flight := p.flight; flight != nil {
		p.mu.Unlock()
		select {
		case <-flight.done:
			return flight.report, true
		case <-ctx.Done():
			return doctor.Report{}, false
		}
	}
	flight := &readinessFlight{done: make(chan struct{})}
	p.flight = flight
	p.mu.Unlock()

	flight.report = diagnose(ctx)
	p.mu.Lock()
	p.flight = nil
	close(flight.done)
	p.mu.Unlock()
	return flight.report, true
}

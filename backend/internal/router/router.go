package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/metrics"
	"automation-hub-backend/internal/ratelimit"

	"github.com/gin-gonic/gin"
)

const (
	maxRequestReadDuration   = 15 * time.Minute
	maxUploadWriteDuration   = 15 * time.Minute
	defaultIdleConnectionTTL = 60 * time.Second
)

func Initialize() error {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	return initializeWithContext(ctx)
}

func initializeWithContext(ctx context.Context) (result error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Startup config guard: refuse to serve with a broken configuration so a
	// misconfigured deployment fails fast and loudly rather than half-working.
	// Warnings (e.g. empty optional keys) do not block startup.
	if report := doctor.Diagnose(config.AppConfig); report.HasFailures() {
		_, _, fail := report.Counts()
		return fmt.Errorf("configuration not ready: %d failing check(s); run `backend doctor` for details", fail)
	}
	metricsExporter, err := metrics.NewFromEnv()
	if err != nil {
		return err
	}
	workers := lifecycle.New(ctx)
	ctx = workers.Context()
	var limiter ratelimit.Enforcer
	defer func() {
		result = errors.Join(result, finishRuntime(workers, func() error {
			var limiterErr error
			if closer, ok := limiter.(interface{ Close() error }); ok {
				limiterErr = closer.Close()
			}
			return errors.Join(limiterErr, infra.CloseDefaultDB())
		}, 20*time.Second))
	}()

	// Initialize before context-free repository constructors can acquire the
	// shared pool. Signals now cancel connection acquisition and migration SQL.
	if _, err := infra.GetDefaultDBContext(ctx); err != nil {
		return fmt.Errorf("initialize shared database: %w", err)
	}

	// Keep production startup quiet and efficient. RUN_MODE already controls
	// whether real side effects are permitted, so use the same source of truth
	// for Gin instead of requiring a second deployment-only switch.
	if strings.EqualFold(strings.TrimSpace(config.AppConfig.RunMode), "production") {
		gin.SetMode(gin.ReleaseMode)
	}

	// initialize Router
	router := gin.Default()
	router.Use(runtimeOwnershipMiddleware(workers))
	if err := router.SetTrustedProxies(nil); err != nil {
		return err
	}
	router.Use(securityHeadersMiddleware())
	limiter = newRateLimitEnforcer()
	router.Use(rateLimitMiddleware(limiter))
	router.Use(idempotencyMiddleware(idempotency.New(10 * time.Minute)))
	router.Use(jsonRequestBodyLimitMiddleware(maxJSONAPIRequestBytes))
	router.Use(localCaptureCORSMiddleware())
	router.Use(metricsExporter.Middleware())

	// initialize routes
	err = initializeRoutesWithContext(router, ctx)
	if err != nil {
		return err
	}
	if metricsExporter.Enabled() {
		router.GET("/metrics", metricsExporter.RequireBearerToken(), gin.WrapH(metricsExporter.Handler()))
	}

	server := newHTTPServer(config.AppConfig.ServerPort, router)
	server.RegisterOnShutdown(workers.Stop)
	return serveWithContext(ctx, server, nil)
}

func runtimeOwnershipMiddleware(workers *lifecycle.Group) gin.HandlerFunc {
	return func(c *gin.Context) {
		finish, admitted := workers.Enter("http-request")
		if !admitted {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		defer finish()
		c.Request = c.Request.WithContext(workers.WithContext(c.Request.Context()))
		c.Next()
	}
}

func finishRuntime(workers *lifecycle.Group, closePool func() error, timeout time.Duration) error {
	if workers == nil || closePool == nil || timeout <= 0 {
		return errors.New("runtime cleanup requires workers, pool closer and positive timeout")
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := workers.StopAndWait(drainCtx); err != nil {
		// Do not close a shared pool while unfinished handlers still need settlement.
		return err
	}
	return closePool()
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       maxRequestReadDuration,
		IdleTimeout:       defaultIdleConnectionTTL,
		WriteTimeout:      maxUploadWriteDuration,
	}
}

// serveWithContext gives the API and the schedulers that derive from its
// lifecycle one termination signal. It supports a listener in tests and uses
// the configured server address in production.
func serveWithContext(ctx context.Context, server *http.Server, listener net.Listener) error {
	return serveWithShutdownTimeout(ctx, server, listener, 20*time.Second)
}

func serveWithShutdownTimeout(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) error {
	if server == nil {
		return errors.New("HTTP server is required")
	}
	if timeout <= 0 {
		return errors.New("HTTP shutdown timeout must be positive")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	serveDone := make(chan struct{})
	shutdownResult := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-serveDone:
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			// Shutdown closes listeners before draining requests. On timeout,
			// close the remaining sockets and preserve the failed drain result.
			shutdownResult <- errors.Join(err, server.Close())
			return
		}
		shutdownResult <- nil
	}()

	var err error
	if listener != nil {
		err = server.Serve(listener)
	} else {
		err = server.ListenAndServe()
	}
	close(serveDone)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	// Serve returns as soon as Shutdown closes the listener, not when the
	// active requests finish. Join the drain before allowing process exit.
	if shutdownErr := <-shutdownResult; shutdownErr != nil {
		return errors.Join(err, shutdownErr)
	}
	return err
}

// newRateLimitEnforcer selects where rate-limit counters live. When REDIS_ADDR
// is set and reachable, counters are shared through Redis so the limit survives
// restarts and holds across multiple backend instances. Otherwise it falls back
// to the in-process limiter — correct for a single instance, but per-process and
// reset on restart. The fallback is deliberate: a misconfigured or briefly
// unreachable Redis at startup degrades the limiter rather than failing the boot.
func newRateLimitEnforcer() ratelimit.Enforcer {
	limit := config.AppConfig.RateLimitPerMinute
	window := time.Minute

	addr := strings.TrimSpace(config.AppConfig.RedisAddr)
	if addr == "" {
		return ratelimit.Memory(limit, window)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	redisLimiter, err := ratelimit.NewRedisLimiter(ctx, addr, limit, window)
	if err != nil {
		log.Printf("ratelimit: falling back to in-process limiter: %v", err)
		return ratelimit.Memory(limit, window)
	}
	log.Printf("ratelimit: using shared Redis store at %s", addr)
	return redisLimiter
}

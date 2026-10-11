package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/metrics"
	"automation-hub-backend/internal/ratelimit"

	"github.com/gin-gonic/gin"
)

const (
	httpShutdownTimeout = 10 * time.Second
	// The reviewed OpenClaw archive route permits large local uploads. Keep a
	// finite connection lifetime without imposing a 30-second failure on that
	// explicitly bounded operation; public traffic is additionally constrained
	// by the loopback gateway and its client-body timeout.
	httpReadTimeout    = 5 * time.Minute
	httpIdleTimeout    = 60 * time.Second
	httpMaxHeaderBytes = 1 << 20
)

func Initialize() error {
	return InitializeContext(context.Background())
}

// InitializeContext starts the HTTP server and all background workers under a
// single application lifetime. Cancelling ctx stops accepting requests,
// drains in-flight handlers, and cancels every scheduler started by routes.
func InitializeContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("application context is required")
	}

	// Startup config guard: refuse to serve with a broken configuration so a
	// misconfigured deployment fails fast and loudly rather than half-working.
	// Warnings (e.g. empty optional keys) do not block startup.
	if report := doctor.Diagnose(config.AppConfig); report.HasFailures() {
		_, _, fail := report.Counts()
		return fmt.Errorf("configuration not ready: %d failing check(s); run `backend doctor` for details", fail)
	}

	// Keep production startup quiet and efficient. RUN_MODE already controls
	// whether real side effects are permitted, so use the same source of truth
	// for Gin instead of requiring a second deployment-only switch.
	if strings.EqualFold(strings.TrimSpace(config.AppConfig.RunMode), "production") {
		gin.SetMode(gin.ReleaseMode)
	}

	// initialize Router
	router := gin.Default()
	if err := router.SetTrustedProxies(nil); err != nil {
		return err
	}
	router.Use(securityHeadersMiddleware())
	router.Use(requestBodyLimitMiddleware(maxNonMultipartRequestBytes))
	router.Use(rateLimitMiddleware(newRateLimitEnforcer()))
	router.Use(idempotencyMiddleware(idempotency.New(10 * time.Minute)))
	router.Use(localCaptureCORSMiddleware())
	metricsExporter, err := metrics.NewFromEnv()
	if err != nil {
		return err
	}
	router.Use(metricsExporter.Middleware())

	// initialize routes
	err = initializeRoutes(ctx, router, metricsExporter)
	if err != nil {
		return err
	}
	if metricsExporter.Enabled() {
		router.GET("/metrics", metricsExporter.RequireBearerToken(), gin.WrapH(metricsExporter.Handler()))
	}

	server := newHTTPServer(config.AppConfig.ServerPort, router)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	return serveUntilCancelled(ctx, server, listener)
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       httpReadTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
}

func serveUntilCancelled(ctx context.Context, server *http.Server, listener net.Listener) error {
	if ctx == nil || server == nil || listener == nil {
		return fmt.Errorf("server lifecycle requires context, server, and listener")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful HTTP shutdown: %w", err)
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
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

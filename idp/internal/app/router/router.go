package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"automation-hub-idp/internal/app/config"
	"github.com/gin-gonic/gin"
)

const httpShutdownTimeout = 10 * time.Second

func Initialize() error {
	return InitializeContext(context.Background())
}

// InitializeContext keeps HTTP serving under the process lifetime so SIGTERM
// can drain in-flight authentication requests before the container exits.
func InitializeContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("application context is required")
	}

	// initialize Router
	router := gin.Default()
	if err := router.SetTrustedProxies(nil); err != nil {
		return err
	}

	// initialize routes
	err := initializeRoutes(router)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              ":" + config.ServerConfig.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	return serveUntilCancelled(ctx, server, listener)
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

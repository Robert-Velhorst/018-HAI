package router

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var errIdentityDrainIncomplete = errors.New("identity requests did not finish draining")

type identityHTTPServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
	Close() error
}

func serveIdentity(ctx context.Context, server identityHTTPServer, timeout time.Duration) error {
	if ctx == nil || server == nil || timeout <= 0 {
		return errors.New("identity server requires a context, server and positive shutdown timeout")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe() }()
	var serveErr error
	var stopped bool
	select {
	case serveErr = <-served:
		stopped = true
	case <-ctx.Done():
	}
	// Shutdown closes listeners first, then waits for active HTTP handlers.
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	drainErr := server.Shutdown(drainCtx)
	var closeErr error
	if drainErr != nil {
		closeErr = server.Close()
	}
	if !stopped {
		serveErr = <-served
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if drainErr != nil {
		// Closing sockets does not establish that handler goroutines finished.
		return errors.Join(errIdentityDrainIncomplete, serveErr, drainErr, closeErr)
	}
	return serveErr
}

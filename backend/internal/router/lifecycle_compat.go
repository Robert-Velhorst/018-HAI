package router

import (
	"context"
	"errors"
	"net/http"
)

type listenAndShutdown interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func serveUntilContextDone(ctx context.Context, server listenAndShutdown) error {
	if ctx == nil || server == nil {
		return errors.New("server lifecycle requires context and server")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
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
			return err
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

package main

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/router"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runIdentity(ctx, config.Setup, router.InitializeContext, os.Stderr)
}

func runIdentity(ctx context.Context, setup func() error, serve func(context.Context) error, diagnostics io.Writer) int {
	if err := setup(); err != nil {
		reportFailure(diagnostics, "configuration", err)
		return 1
	}
	if err := serve(ctx); err != nil {
		// Only a bare expected cancellation is normal: joined failures must remain errors.
		if err == context.Canceled && ctx.Err() == context.Canceled {
			return 0
		}
		reportFailure(diagnostics, "startup or shutdown", err)
		return 1
	}
	return 0
}

func reportFailure(writer io.Writer, phase string, err error) {
	category := "operation failed"
	var networkError net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		category = "deadline exceeded"
	case errors.Is(err, context.Canceled):
		category = "operation cancelled with a failure"
	case errors.As(err, &networkError):
		category = "network operation failed"
	}
	// Driver error text may contain credentials, SQL values or token-bearing URLs.
	fmt.Fprintf(writer, "HAI identity %s: %s; raw error details withheld\n", phase, category)
}

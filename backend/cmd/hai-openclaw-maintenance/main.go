// The Windows maintenance worker has no listener and accepts fixed target jobs only.
package main

import (
	m "automation-hub-backend/internal/openclawmaintenance"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	w := m.NewWorker()
	if len(args) == 2 && args[0] == "check" && m.ValidTarget(args[1]) {
		bounded, done := context.WithTimeout(ctx, 5*time.Minute)
		defer done()
		r, err := w.Execute(bounded, m.Job{Target: args[1], Kind: "check"})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	once := len(args) == 1 && args[0] == "--once"
	if len(args) > 0 && !once {
		return fmt.Errorf("usage: hai-openclaw-maintenance [--once | check companion | check gateway_core]")
	}
	client, err := m.NewPullClient(os.Getenv("HAI_OPENCLAW_MAINTENANCE_URL"), os.Getenv("HAI_OPENCLAW_MAINTENANCE_TOKEN"), os.Getenv("BACKEND_API_SHARED_KEY"))
	if err != nil {
		return err
	}
	execute := func(ctx context.Context, job m.Job, permit func() error) (m.Report, error) {
		worker := *w
		worker.BeforeApply = permit
		return worker.Execute(ctx, job)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		capability := w.CapabilityHandshake(ctx)
		err := client.PollOnceWithCapabilities(ctx, capability, execute)
		if once {
			return err
		}
		if errors.Is(err, m.ErrWorkerStopAfterCancellation) {
			return err
		}
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

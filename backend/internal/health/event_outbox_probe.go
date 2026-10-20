package health

import (
	"context"
	"fmt"
	"time"

	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/infra"
)

// EventOutboxProbe reports delivery degradation independently of broker reachability.
// A reachable broker does not prove that queued events are being delivered.
func EventOutboxProbe() doctor.Probe {
	return eventOutboxProbe(func(ctx context.Context) (events.OutboxStats, error) {
		db, err := infra.GetDefaultDB()
		if err != nil {
			return events.OutboxStats{}, err
		}
		return events.NewOutboxStore(db).Stats(ctx)
	}, time.Now)
}

func eventOutboxProbe(load func(context.Context) (events.OutboxStats, error), now func() time.Time) doctor.Probe {
	return doctor.Probe{
		Name:     "events.outbox",
		Critical: false,
		Run: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			stats, err := load(ctx)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if stats.DeadLettered > 0 {
				return fmt.Errorf("%d event deliveries are dead-lettered", stats.DeadLettered)
			}
			if stats.Pending > 0 {
				if stats.OldestPendingAt == nil {
					return fmt.Errorf("%d event deliveries are pending; delivery age is unavailable", stats.Pending)
				}
				if age := now().Sub(*stats.OldestPendingAt); age > 2*time.Minute {
					return fmt.Errorf("%d event deliveries are pending; oldest is %s old", stats.Pending, age.Round(time.Second))
				}
			}
			return nil
		},
	}
}

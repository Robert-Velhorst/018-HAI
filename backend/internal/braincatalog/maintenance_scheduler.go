package braincatalog

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

const catalogRevalidationSchedulerIntervalEnv = "HAI_CATALOG_REVALIDATION_SCHEDULER_INTERVAL_MINUTES"

// StartCatalogRevalidationScheduler refreshes only owner-enabled public
// catalog metadata. It honours HAI's emergency stop and relies on durable
// records to avoid duplicate checks between sweeps.
func StartCatalogRevalidationScheduler(ctx context.Context, service *CatalogMaintenanceService, backgroundAllowed func() bool) {
	if service == nil || !catalogRevalidationEnabled() || !catalogRevalidationSchedulerEnabled() {
		return
	}
	run := func() {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return
		}
		service.RunDueRevalidationsContext(ctx)
	}
	go func() {
		run()
		ticker := time.NewTicker(catalogRevalidationSchedulerInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func catalogRevalidationSchedulerEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("HAI_CATALOG_REVALIDATION_SCHEDULER_ENABLED"))
	return raw == "" || strings.EqualFold(raw, "true")
}

func catalogRevalidationSchedulerInterval() time.Duration {
	minutes := 60
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(catalogRevalidationSchedulerIntervalEnv))); err == nil && value != 0 {
		minutes = value
	}
	if minutes < 15 {
		minutes = 15
	}
	if minutes > 24*60 {
		minutes = 24 * 60
	}
	return time.Duration(minutes) * time.Minute
}

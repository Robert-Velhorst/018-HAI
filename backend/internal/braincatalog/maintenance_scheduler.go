package braincatalog

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

const catalogRevalidationSchedulerIntervalEnv = "HAI_CATALOG_REVALIDATION_SCHEDULER_INTERVAL_MINUTES"

// StartCatalogRevalidationScheduler refreshes only owner-enabled public
// catalog metadata. It honours HAI's emergency stop and relies on durable
// records to avoid duplicate checks between sweeps.
func StartCatalogRevalidationScheduler(ctx context.Context, service *CatalogMaintenanceService, backgroundAllowed func() bool) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if service == nil || !catalogRevalidationEnabled() || !catalogRevalidationSchedulerEnabled() {
		return
	}
	run := func() {
		if ctx.Err() != nil {
			return
		}
		if backgroundAllowed != nil && !backgroundAllowed() {
			return
		}
		reportCatalogRevalidationRun(service.RunDueRevalidations())
	}
	lifecycle.Go(ctx, "brain-catalog-maintenance", func() {
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
	})
}

// reportCatalogRevalidationRun keeps unattended failures visible without
// logging upstream responses, repository metadata, or any configured secrets.
func reportCatalogRevalidationRun(run CatalogRevalidationRun) {
	if !catalogMaintenanceNeedsReport(run) {
		return
	}
	collectionFailed := run.CollectionReview != nil && run.CollectionReview.Failed
	discoveryFailed := run.RepositoryDiscoveryReview != nil && run.RepositoryDiscoveryReview.Failed
	log.Printf(
		"brain catalog maintenance failed=%d checked=%d reused=%d collection_failed=%t discovery_failed=%t",
		run.Failed,
		run.Checked,
		run.Reused,
		collectionFailed,
		discoveryFailed,
	)
}

func catalogMaintenanceNeedsReport(run CatalogRevalidationRun) bool {
	return run.Failed > 0 ||
		(run.CollectionReview != nil && run.CollectionReview.Failed) ||
		(run.RepositoryDiscoveryReview != nil && run.RepositoryDiscoveryReview.Failed)
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

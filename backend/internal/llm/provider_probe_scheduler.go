package llm

import (
	"context"
	"errors"
	"sync"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
)

const (
	providerProbeSchedulerFailureBackoff = 30 * time.Second
	providerProbeSchedulerLeaseRetry     = 5 * time.Second
	providerProbeSchedulerLeaseTimeout   = 5 * time.Second
	providerProbeSchedulerMaximumRefresh = 6 * time.Hour
	providerProbeSchedulerMinimumRefresh = 30 * time.Second
	providerProbeSchedulerLeaseProvider  = "__hai_internal_provider_probe_scheduler__"
	providerProbeSchedulerLeaseModel     = "all-configured-providers"
)

var activeProviderProbeSchedulers sync.Map

func startProviderProbeScheduler(ctx context.Context, service *Service, backgroundAllowed func() bool) bool {
	if ctx == nil || service == nil || backgroundAllowed == nil || service.probeHistory == nil {
		return false
	}
	if _, ok := service.maintenanceHistory.(modelMaintenanceLeaseRepository); !ok {
		return false
	}
	if _, loaded := activeProviderProbeSchedulers.LoadOrStore(service, struct{}{}); loaded {
		return false
	}
	if !lifecycle.Go(ctx, "llm-provider-probes", func() {
		defer activeProviderProbeSchedulers.Delete(service)
		runProviderProbeSchedulerWithWait(
			ctx,
			backgroundAllowed,
			service,
			waitForModelMaintenanceScheduler,
			time.Now,
		)
	}) {
		activeProviderProbeSchedulers.Delete(service)
		return false
	}
	return true
}

func runProviderProbeSchedulerWithWait(
	ctx context.Context,
	backgroundAllowed func() bool,
	service *Service,
	wait func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent,
	now func() time.Time,
) {
	if ctx == nil || service == nil || wait == nil || now == nil || service.probeHistory == nil {
		return
	}
	if _, ok := service.maintenanceHistory.(modelMaintenanceLeaseRepository); !ok {
		return
	}

	var nextCheckAt time.Time
	var retryAfter time.Time
	previouslyAllowed := false
	for ctx.Err() == nil {
		allowed := modelMaintenanceBackgroundAllowed(backgroundAllowed)
		if allowed && !previouslyAllowed {
			nextCheckAt = time.Time{}
		}
		previouslyAllowed = allowed
		if !allowed {
			nextCheckAt = time.Time{}
			if wait(ctx, modelMaintenanceSchedulerPermissionPollInterval, nil) == modelMaintenanceSchedulerCancelled {
				return
			}
			previouslyAllowed = false
			continue
		}

		currentTime := now().UTC()
		if !retryAfter.IsZero() && currentTime.Before(retryAfter) {
			if wait(ctx, min(retryAfter.Sub(currentTime), modelMaintenanceSchedulerPermissionPollInterval), nil) == modelMaintenanceSchedulerCancelled {
				return
			}
			continue
		}
		if nextCheckAt.IsZero() || !currentTime.Before(nextCheckAt) {
			policy := service.Policy()
			provider, dueAt, err := service.nextScheduledProviderProbe(ctx, policy, currentTime)
			if err != nil {
				retryAfter = currentTime.Add(providerProbeSchedulerFailureBackoff)
				nextCheckAt = retryAfter
			} else if provider == nil {
				retryAfter = time.Time{}
				nextCheckAt = currentTime.Add(modelMaintenanceSchedulerPermissionPollInterval)
			} else if currentTime.Before(dueAt) {
				retryAfter = time.Time{}
				nextCheckAt = dueAt
			} else {
				var probed bool
				runErr := runScheduledProviderProbe(ctx, backgroundAllowed, func(runCtx context.Context) error {
					var probeErr error
					probed, probeErr = service.runNextDueProviderProbeWithLease(runCtx, now().UTC())
					return probeErr
				})
				if errors.Is(runErr, context.Canceled) && ctx.Err() == nil {
					previouslyAllowed = false
					nextCheckAt = time.Time{}
					retryAfter = now().UTC().Add(modelMaintenanceSchedulerPermissionPollInterval)
					continue
				}
				if runErr != nil {
					retryAfter = now().UTC().Add(providerProbeSchedulerFailureBackoff)
					nextCheckAt = retryAfter
				} else if !probed {
					retryAfter = time.Time{}
					nextCheckAt = now().UTC().Add(providerProbeSchedulerLeaseRetry)
				} else {
					retryAfter = time.Time{}
					nextCheckAt = time.Time{}
					continue
				}
			}
		}

		currentTime = now().UTC()
		waitFor := modelMaintenanceSchedulerPermissionPollInterval
		if !nextCheckAt.IsZero() && nextCheckAt.After(currentTime) {
			waitFor = min(waitFor, nextCheckAt.Sub(currentTime))
		}
		if wait(ctx, waitFor, nil) == modelMaintenanceSchedulerCancelled {
			return
		}
	}
}

func runScheduledProviderProbe(ctx context.Context, backgroundAllowed func() bool, run func(context.Context) error) error {
	if ctx == nil || run == nil || ctx.Err() != nil {
		return context.Canceled
	}
	finish, admitted := lifecycle.Enter(ctx, "llm-probe-run")
	if !admitted || !modelMaintenanceBackgroundAllowed(backgroundAllowed) {
		if admitted {
			finish()
		}
		return context.Canceled
	}
	defer finish()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorReady := make(chan struct{})
	monitorDone := make(chan struct{})
	if !lifecycle.Go(runCtx, "llm-probe-permit-monitor", func() {
		defer close(monitorDone)
		ticker := time.NewTicker(modelMaintenanceEmergencyStopPollInterval)
		defer ticker.Stop()
		if runCtx.Err() != nil || !modelMaintenanceBackgroundAllowed(backgroundAllowed) {
			cancel()
			return
		}
		close(monitorReady)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if !modelMaintenanceBackgroundAllowed(backgroundAllowed) {
					cancel()
					return
				}
			}
		}
	}) {
		return context.Canceled
	}
	defer func() { cancel(); <-monitorDone }()
	select {
	case <-monitorReady:
	case <-monitorDone:
		return context.Canceled
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	err := run(runCtx)
	cancel()
	<-monitorDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !modelMaintenanceBackgroundAllowed(backgroundAllowed) {
		return context.Canceled
	}
	return err
}

func (s *Service) nextScheduledProviderProbe(
	ctx context.Context,
	policy Policy,
	now time.Time,
) (*Provider, time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var selected *Provider
	var earliest time.Time
	for index := range policy.Providers {
		provider := policy.Providers[index]
		if !providerProbeSchedulerEligible(provider, policy) {
			continue
		}
		latest, err := findLatestProviderProbeWithContext(ctx, s.probeHistory, provider.ID)
		if err != nil {
			return nil, time.Time{}, err
		}
		dueAt := providerProbeNextDueAt(latest, policy, now)
		if selected == nil || dueAt.Before(earliest) {
			providerCopy := provider
			selected = &providerCopy
			earliest = dueAt
		}
	}
	return selected, earliest, nil
}

func providerProbeSchedulerEligible(provider Provider, policy Policy) bool {
	if !provider.Enabled || !providerRuntimeReadiness(provider).configured {
		return false
	}
	if provider.Local && !policy.LocalModelsAllowed {
		return false
	}
	if provider.Paid && (!policy.PaidCallsAllowed || policy.RequireApprovalBeforePaidUsage || !paidProviderProbeBudgetAvailable(policy)) {
		return false
	}
	if !provider.Local && !provider.Paid {
		if provider.QuotaRemaining <= 0 && !policy.FreeCloudQuotaAllowed {
			return false
		}
		if provider.QuotaRemaining == 0 {
			return false
		}
	}
	for _, model := range provider.Models {
		if model.Enabled {
			return true
		}
	}
	return false
}

func providerProbeNextDueAt(probe *models.LLMProviderProbe, policy Policy, now time.Time) time.Time {
	now = now.UTC()
	if probe == nil || probe.CheckedAt.IsZero() || probe.CheckedAt.After(now) {
		return now
	}
	delay := providerProbeRefreshInterval(policy)
	if !probe.Live {
		delay = providerProbeSchedulerFailureBackoff
	}
	return probe.CheckedAt.UTC().Add(delay)
}

func providerProbeRefreshInterval(policy Policy) time.Duration {
	seconds := policy.ProviderProbeMaxAgeSeconds
	if seconds <= 0 {
		seconds = 900
	}
	const maximumAgeSeconds = int(providerProbeSchedulerMaximumRefresh / time.Second * 2)
	if seconds > maximumAgeSeconds {
		return providerProbeSchedulerMaximumRefresh
	}
	interval := time.Duration(seconds) * time.Second / 2
	if interval < providerProbeSchedulerMinimumRefresh {
		return providerProbeSchedulerMinimumRefresh
	}
	return interval
}

func (s *Service) runNextDueProviderProbeWithLease(ctx context.Context, now time.Time) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.probeHistory == nil {
		return false, errors.New("durable provider probe history is unavailable")
	}
	leaseRepository, ok := s.maintenanceHistory.(modelMaintenanceLeaseRepository)
	if !ok {
		return false, errors.New("cross-process provider probe lease is unavailable")
	}
	leaseCtx, cancel := context.WithTimeout(ctx, providerProbeSchedulerLeaseTimeout)
	release, acquired, err := leaseRepository.AcquireModelMaintenanceLease(
		leaseCtx,
		providerProbeSchedulerLeaseProvider,
		providerProbeSchedulerLeaseModel,
	)
	cancel()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	if release == nil {
		return false, errors.New("provider probe lease was acquired without a release function")
	}
	defer release()

	policy := s.Policy()
	provider, dueAt, err := s.nextScheduledProviderProbe(ctx, policy, now)
	if err != nil {
		return false, err
	}
	if provider == nil || now.Before(dueAt) {
		return false, nil
	}
	_, err = s.probeAndRecordProvidersWithPolicy(ctx, policy, []Provider{*provider})
	if err != nil {
		return false, err
	}
	return true, nil
}

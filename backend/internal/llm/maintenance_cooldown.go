package llm

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"
)

const maxLocalModelMaintenanceCooldowns = 256

type modelMaintenanceCooldownKey struct {
	ownerDigest [32]byte
	providerID  string
	modelID     string
}

type modelMaintenanceCooldown struct {
	configurationFingerprint string
	retryAt                  time.Time
}

type modelMaintenanceScope struct {
	ownerIdentity string
	local         bool
}

type modelMaintenanceScopeContextKey struct{}

func withModelMaintenanceScope(ctx context.Context, ownerIdentity string, local bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		ownerIdentity = "system"
	}
	return context.WithValue(ctx, modelMaintenanceScopeContextKey{}, modelMaintenanceScope{
		ownerIdentity: ownerIdentity,
		local:         local,
	})
}

func modelMaintenanceOwnerIdentity(effectContexts []*EffectContext, fallback *EffectContext) string {
	for _, effectContext := range effectContexts {
		if effectContext != nil && strings.TrimSpace(effectContext.OwnerIdentity) != "" {
			return strings.TrimSpace(effectContext.OwnerIdentity)
		}
	}
	if fallback != nil && strings.TrimSpace(fallback.OwnerIdentity) != "" {
		return strings.TrimSpace(fallback.OwnerIdentity)
	}
	return "system"
}

func modelMaintenanceCooldownKeyFor(scope modelMaintenanceScope, providerID, modelID string) modelMaintenanceCooldownKey {
	ownerDigest := sha256.Sum256([]byte(scope.ownerIdentity))
	return modelMaintenanceCooldownKey{ownerDigest: ownerDigest, providerID: providerID, modelID: modelID}
}

func (s *Service) localModelMaintenanceCooldown(ctx context.Context, provider Provider, model Model, fingerprint string, now time.Time) *ModelMaintenanceResult {
	if !provider.Local {
		return nil
	}
	scope, _ := ctx.Value(modelMaintenanceScopeContextKey{}).(modelMaintenanceScope)
	key := modelMaintenanceCooldownKeyFor(scope, provider.ID, model.ID)
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	s.pruneModelMaintenanceCooldownsLocked(now)

	var retryAt time.Time
	if cooldown, ok := s.maintenanceCooldowns[key]; ok {
		if cooldown.configurationFingerprint != fingerprint {
			delete(s.maintenanceCooldowns, key)
		} else if cooldown.retryAt.After(now) {
			retryAt = cooldown.retryAt
		}
	}
	if s.maintenanceCooldownOverflowUntil.After(now) && s.maintenanceCooldownOverflowUntil.After(retryAt) {
		retryAt = s.maintenanceCooldownOverflowUntil
	}
	if retryAt.IsZero() {
		return nil
	}
	return maintenanceCooldownBlockedResult(provider, model, fingerprint, retryAt)
}

func (s *Service) rememberLocalModelMaintenanceFailure(ctx context.Context, providerID, modelID, fingerprint string, retryAt time.Time) {
	scope, ok := ctx.Value(modelMaintenanceScopeContextKey{}).(modelMaintenanceScope)
	if !ok || !scope.local || fingerprint == "" {
		return
	}
	key := modelMaintenanceCooldownKeyFor(scope, providerID, modelID)
	now := time.Now().UTC()
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	s.pruneModelMaintenanceCooldownsLocked(now)
	if s.maintenanceCooldowns == nil {
		s.maintenanceCooldowns = make(map[modelMaintenanceCooldownKey]modelMaintenanceCooldown)
	}
	if existing, ok := s.maintenanceCooldowns[key]; ok {
		existing.configurationFingerprint = fingerprint
		existing.retryAt = retryAt
		s.maintenanceCooldowns[key] = existing
		return
	}
	if len(s.maintenanceCooldowns) < maxLocalModelMaintenanceCooldowns {
		s.maintenanceCooldowns[key] = modelMaintenanceCooldown{configurationFingerprint: fingerprint, retryAt: retryAt}
		return
	}

	// Never evict an active owner/model cooldown to make room. If the bounded
	// registry is saturated, fail closed for local models until the longest
	// outstanding retry deadline.
	if retryAt.After(s.maintenanceCooldownOverflowUntil) {
		s.maintenanceCooldownOverflowUntil = retryAt
	}
	for _, cooldown := range s.maintenanceCooldowns {
		if cooldown.retryAt.After(s.maintenanceCooldownOverflowUntil) {
			s.maintenanceCooldownOverflowUntil = cooldown.retryAt
		}
	}
}

func (s *Service) clearLocalModelMaintenanceCooldown(ctx context.Context, providerID, modelID, fingerprint string) {
	scope, ok := ctx.Value(modelMaintenanceScopeContextKey{}).(modelMaintenanceScope)
	if !ok || !scope.local {
		return
	}
	key := modelMaintenanceCooldownKeyFor(scope, providerID, modelID)
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if cooldown, exists := s.maintenanceCooldowns[key]; exists && cooldown.configurationFingerprint != fingerprint {
		delete(s.maintenanceCooldowns, key)
	}
}

func (s *Service) clearVerifiedLocalModelMaintenanceCooldown(ctx context.Context, providerID, modelID, fingerprint string) {
	scope, ok := ctx.Value(modelMaintenanceScopeContextKey{}).(modelMaintenanceScope)
	if !ok || !scope.local {
		return
	}
	key := modelMaintenanceCooldownKeyFor(scope, providerID, modelID)
	s.maintenanceMu.Lock()
	delete(s.maintenanceCooldowns, key)
	s.maintenanceMu.Unlock()
}

func (s *Service) pruneModelMaintenanceCooldownsLocked(now time.Time) {
	for key, cooldown := range s.maintenanceCooldowns {
		if !cooldown.retryAt.After(now) {
			delete(s.maintenanceCooldowns, key)
		}
	}
	if !s.maintenanceCooldownOverflowUntil.After(now) {
		s.maintenanceCooldownOverflowUntil = time.Time{}
	}
}

func maintenanceCooldownBlockedResult(provider Provider, model Model, fingerprint string, retryAt time.Time) *ModelMaintenanceResult {
	result := &ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		Status: "failed", Reason: "daily model maintenance is in a local retry cooldown because its result could not be persisted",
		ConfigurationFingerprint: fingerprint, BlocksExecution: true,
		CheckedAt: time.Now().UTC(), NextCheckDueAt: &retryAt,
	}
	return result
}

func maintenanceStatusBlocksExecution(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "current", "updated", "installed", "provider_managed", "not_enforced":
		return false
	default:
		// Unknown and empty values are not positive evidence of a safe model.
		return true
	}
}

func isVerifiedMaintenanceStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "current", "updated", "installed":
		return true
	default:
		return false
	}
}

func isVerifiedLocalMaintenanceResult(provider Provider, result ModelMaintenanceResult) bool {
	return provider.Local && isVerifiedMaintenanceStatus(result.Status) && !result.BlocksExecution
}

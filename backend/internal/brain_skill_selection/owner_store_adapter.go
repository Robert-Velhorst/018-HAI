package brain_skill_selection

import (
	"context"
	"fmt"

	"automation-hub-backend/internal/brainskills"
)

// TaskContextProvider exposes only the selected HAI-authored prompt summaries
// and their immutable provenance. It has no action or tool authority.
type TaskContextProvider interface {
	GuidanceForTask(context.Context, string, string, string) ([]AppliedGuidance, error)
}

type OwnerSelectionStoreAdapter struct {
	service *SelectionService
}

func NewOwnerSelectionStoreAdapter(service *SelectionService) *OwnerSelectionStoreAdapter {
	return &OwnerSelectionStoreAdapter{service: service}
}

func (a *OwnerSelectionStoreAdapter) State(ctx context.Context, ownerIdentity string, skill brainskills.Skill) (brainskills.SelectionState, error) {
	states, err := a.StatesForOwner(ctx, ownerIdentity, []brainskills.Skill{skill})
	if err != nil {
		return brainskills.SelectionState{}, err
	}
	state, ok := states[skill.ID]
	if !ok {
		return brainskills.SelectionState{}, ErrSkillNotFound
	}
	return state, nil
}

// StatesForOwner adapts the service's single owner-scoped read to the
// handler's complete catalog inventory. The caller supplies the catalog
// entries it will render so stale or mismatched pins fail closed.
func (a *OwnerSelectionStoreAdapter) StatesForOwner(ctx context.Context, ownerIdentity string, skills []brainskills.Skill) (map[string]brainskills.SelectionState, error) {
	if a == nil || a.service == nil {
		return nil, ErrRepositoryRequired
	}
	wanted := make(map[string]struct{}, len(skills))
	catalog := make(map[string]brainskills.Skill)
	for _, current := range a.service.catalog.List() {
		catalog[current.ID] = current
	}
	for _, skill := range skills {
		if _, duplicate := wanted[skill.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate requested skill %q", ErrInvalidCatalog, skill.ID)
		}
		current, exists := catalog[skill.ID]
		if !exists {
			return nil, ErrSkillNotFound
		}
		if !sameCatalogPin(skill, current) {
			return nil, fmt.Errorf("%w: supplied skill is not the current server catalog pin", ErrInvalidCatalog)
		}
		wanted[skill.ID] = struct{}{}
	}
	serviceStates, err := a.service.StatesForOwner(ctx, ownerIdentity)
	if err != nil {
		return nil, err
	}
	bySkill := make(map[string]SelectionState, len(serviceStates))
	for _, state := range serviceStates {
		if _, requested := wanted[state.ID]; requested {
			bySkill[state.ID] = state
		}
	}

	result := make(map[string]brainskills.SelectionState, len(skills))
	for _, skill := range skills {
		state, exists := bySkill[skill.ID]
		if !exists {
			return nil, fmt.Errorf("%w: selection state missing for %q", ErrSkillNotFound, skill.ID)
		}
		result[skill.ID] = toOwnerSelectionState(state)
	}
	return result, nil
}

func (a *OwnerSelectionStoreAdapter) Set(ctx context.Context, ownerIdentity string, skill brainskills.Skill, enabled bool, reviewedCatalogFingerprint string) (brainskills.SelectionState, error) {
	if a == nil || a.service == nil {
		return brainskills.SelectionState{}, ErrRepositoryRequired
	}
	snapshot, ok, err := a.service.catalogSnapshot(skill.ID)
	if err != nil {
		return brainskills.SelectionState{}, err
	}
	if !ok {
		return brainskills.SelectionState{}, ErrSkillNotFound
	}
	if !sameCatalogPin(skill, snapshot.skill) {
		return brainskills.SelectionState{}, fmt.Errorf("%w: supplied skill is not the current server catalog pin", ErrInvalidCatalog)
	}
	if enabled && reviewedCatalogFingerprint != a.service.catalog.Fingerprint() {
		return brainskills.SelectionState{}, brainskills.ErrCatalogReviewStale
	}
	event, err := a.service.Set(ctx, ownerIdentity, ownerIdentity, skill.ID, enabled, reviewedCatalogFingerprint)
	if err != nil {
		return brainskills.SelectionState{}, err
	}
	if !samePin(event, snapshot.skill) || event.CatalogFingerprint != a.service.catalog.Fingerprint() ||
		event.OwnerIdentity != ownerIdentity || event.ActorIdentity != ownerIdentity || event.Enabled != enabled {
		return brainskills.SelectionState{}, fmt.Errorf("%w: appended event does not match current owner selection", ErrInvalidCatalog)
	}
	// Another session can append a later decision after this event is stored.
	// Return the effective latest state rather than echoing this request, so the
	// UI does not claim a selection that the task runtime will no longer apply.
	state, err := a.State(ctx, ownerIdentity, skill)
	if err != nil {
		return brainskills.SelectionState{}, err
	}
	state.SupersededByConcurrentDecision = state.Decision != nil && state.Decision.ID > event.ID
	return state, nil
}

func (s *SelectionService) validateCurrentSkill(skill brainskills.Skill) error {
	snapshot, ok, err := s.catalogSnapshot(skill.ID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSkillNotFound
	}
	if !sameCatalogPin(skill, snapshot.skill) {
		return fmt.Errorf("%w: supplied skill is not the current server catalog pin", ErrInvalidCatalog)
	}
	return nil
}

func (s *SelectionService) catalogSnapshot(id string) (catalogSnapshot, bool, error) {
	snapshots, err := s.catalogSnapshots()
	if err != nil {
		return catalogSnapshot{}, false, err
	}
	snapshot, ok := snapshots[id]
	return snapshot, ok, nil
}

var _ brainskills.OwnerSelectionStore = (*OwnerSelectionStoreAdapter)(nil)
var _ brainskills.OwnerSelectionBatchStore = (*OwnerSelectionStoreAdapter)(nil)
var _ TaskContextProvider = (*SelectionService)(nil)

func toOwnerSelectionState(state SelectionState) brainskills.SelectionState {
	var decision *brainskills.SelectionDecisionMetadata
	if summary := state.Decision; summary != nil {
		decision = &brainskills.SelectionDecisionMetadata{
			ID:             summary.ID,
			ActorIdentity:  summary.ActorIdentity,
			DecidedAt:      summary.DecidedAt,
			GuidanceSHA256: summary.GuidanceSHA256,
		}
	}
	return brainskills.SelectionState{
		Enabled:         state.Enabled && state.Status == StatusEnabled && !state.NeedsReapproval,
		NeedsReapproval: state.NeedsReapproval,
		Decision:        decision,
	}
}

// Package brain_skill_selection stores durable owner consent for applying
// reviewed HAI-authored skill summaries as prompt guidance. It never loads
// upstream skill files or grants tools, actions, or runtime capabilities.
package brain_skill_selection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"automation-hub-backend/internal/brainskills"
)

var (
	ErrInvalidInput       = errors.New("invalid brain skill selection input")
	ErrSkillNotFound      = errors.New("brain skill is not in the reviewed catalog")
	ErrClientPinRejected  = errors.New("client-supplied skill pins are not accepted")
	ErrInvalidCatalog     = errors.New("brain skill catalog pin is invalid")
	ErrRepositoryRequired = errors.New("brain skill selection repository is required")
)

type SelectionStatus string

const (
	StatusNotSelected     SelectionStatus = "not_selected"
	StatusEnabled         SelectionStatus = "enabled"
	StatusDisabled        SelectionStatus = "disabled"
	StatusNeedsReapproval SelectionStatus = "needs_reapproval"
)

// Pin identifies the exact catalog revision and HAI-authored summary covered
// by an owner's consent. Pins are always resolved from the server catalog.
type Pin struct {
	SourceCommit   string `json:"sourceCommit"`
	SourceSHA256   string `json:"sourceSHA256"`
	GuidanceSHA256 string `json:"guidanceSHA256"`
}

// SelectionEvent is one immutable enable/disable decision. OwnerIdentity and
// ActorIdentity must come from the authenticated caller, never from request
// JSON. OwnerIdentity is omitted from JSON responses to avoid leaking scope.
type SelectionEvent struct {
	ID                 int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	OwnerIdentity      string    `json:"-" gorm:"column:owner_identity"`
	SkillID            string    `json:"skillId" gorm:"column:skill_id"`
	SourceCommit       string    `json:"sourceCommit" gorm:"column:source_commit"`
	SourceSHA256       string    `json:"sourceSHA256" gorm:"column:source_sha256"`
	GuidanceSHA256     string    `json:"guidanceSHA256" gorm:"column:guidance_sha256"`
	CatalogFingerprint string    `json:"-" gorm:"column:catalog_fingerprint"`
	ActorIdentity      string    `json:"actorIdentity" gorm:"column:actor_identity"`
	Enabled            bool      `json:"enabled" gorm:"column:enabled"`
	DecidedAt          time.Time `json:"decidedAt" gorm:"column:decided_at"`
}

func (SelectionEvent) TableName() string { return "brain_skill_selection_events" }

// Repository appends decisions and returns only the latest event per skill
// for the requested owner. Implementations must never update prior events.
type Repository interface {
	Append(context.Context, SelectionEvent) (SelectionEvent, error)
	LatestForOwner(context.Context, string) ([]SelectionEvent, error)
}

// Store is retained as a descriptive alias for callers that model persistence
// as a store rather than a repository.
type Store = Repository

// SelectionRequest is the HTTP-friendly mutation shape. Client pins are
// present only so the service can explicitly reject attempts to choose or
// forge catalog digests. Actor and owner identities are supplied separately
// from authenticated server context.
type SelectionRequest struct {
	SkillID                    string  `json:"skillId"`
	Enabled                    bool    `json:"enabled"`
	Pin                        *Pin    `json:"pin,omitempty"`
	SourceCommit               *string `json:"sourceCommit,omitempty"`
	SourceSHA256               *string `json:"sourceSHA256,omitempty"`
	GuidanceSHA256             *string `json:"guidanceSHA256,omitempty"`
	ReviewedCatalogFingerprint string  `json:"-"`
}

type SelectionDecisionSummary struct {
	ID             int64     `json:"id"`
	Enabled        bool      `json:"enabled"`
	SourceCommit   string    `json:"sourceCommit"`
	SourceSHA256   string    `json:"sourceSHA256"`
	GuidanceSHA256 string    `json:"guidanceSHA256"`
	ActorIdentity  string    `json:"actorIdentity"`
	DecidedAt      time.Time `json:"decidedAt"`
}

// SelectionState is the current per-catalog status. A stale last decision is
// visible as needs_reapproval and is never treated as enabled.
type SelectionState struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Category        string                    `json:"category"`
	Status          SelectionStatus           `json:"status"`
	Enabled         bool                      `json:"enabled"`
	NeedsReapproval bool                      `json:"needsReapproval"`
	SourceCommit    string                    `json:"sourceCommit"`
	SourceSHA256    string                    `json:"sourceSHA256"`
	GuidanceSHA256  string                    `json:"guidanceSHA256"`
	Decision        *SelectionDecisionSummary `json:"decision,omitempty"`
	decisionEvent   *SelectionEvent
	summary         string
}

// AppliedGuidance contains only the HAI-authored summary and its provenance.
// It is prompt guidance, not source evidence, policy, authorization, or a tool
// definition.
type AppliedGuidance struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Commit              string `json:"commit"`
	SourceSHA256        string `json:"sourceSHA256"`
	GuidanceSHA256      string `json:"guidanceSHA256"`
	SelectionDecisionID int64  `json:"selectionDecisionId"`
	Summary             string `json:"summary"`
}

type SelectionService struct {
	repository Repository
	catalog    brainskills.Catalog
	now        func() time.Time
}

func NewSelectionService(repository Repository, catalog brainskills.Catalog) *SelectionService {
	return &SelectionService{repository: repository, catalog: catalog, now: time.Now}
}

// Set records owner consent for the server-resolved current catalog pin.
func (s *SelectionService) Set(ctx context.Context, owner, actor, skillID string, enabled bool, reviewedCatalogFingerprint string) (SelectionEvent, error) {
	return s.SetRequest(ctx, owner, actor, SelectionRequest{
		SkillID: skillID, Enabled: enabled, ReviewedCatalogFingerprint: reviewedCatalogFingerprint,
	})
}

// SetRequest rejects any client-supplied pin and resolves every value from the
// catalog. The request cannot grant runtime capabilities.
func (s *SelectionService) SetRequest(ctx context.Context, owner, actor string, request SelectionRequest) (SelectionEvent, error) {
	if s == nil || s.repository == nil {
		return SelectionEvent{}, ErrRepositoryRequired
	}
	if err := validateIdentity(owner); err != nil {
		return SelectionEvent{}, fmt.Errorf("owner: %w", err)
	}
	if err := validateIdentity(actor); err != nil {
		return SelectionEvent{}, fmt.Errorf("actor: %w", err)
	}
	if request.Pin != nil || request.SourceCommit != nil || request.SourceSHA256 != nil || request.GuidanceSHA256 != nil {
		return SelectionEvent{}, ErrClientPinRejected
	}
	if !validSkillID(request.SkillID) {
		return SelectionEvent{}, fmt.Errorf("skill id: %w", ErrInvalidInput)
	}
	catalogFingerprint := s.catalog.Fingerprint()
	if request.Enabled {
		if request.ReviewedCatalogFingerprint != catalogFingerprint {
			return SelectionEvent{}, brainskills.ErrCatalogReviewStale
		}
	} else if request.ReviewedCatalogFingerprint != "" {
		return SelectionEvent{}, fmt.Errorf("disabled selections do not accept a review fingerprint: %w", ErrInvalidInput)
	}

	snapshots, err := s.catalogSnapshots()
	if err != nil {
		return SelectionEvent{}, err
	}
	snapshot, ok := snapshots[request.SkillID]
	if !ok {
		return SelectionEvent{}, ErrSkillNotFound
	}
	if err := validateIdentity(request.SkillID); err != nil {
		return SelectionEvent{}, fmt.Errorf("skill id: %w", err)
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	event := SelectionEvent{
		OwnerIdentity:      owner,
		ActorIdentity:      actor,
		SkillID:            snapshot.skill.ID,
		SourceCommit:       snapshot.skill.Commit,
		SourceSHA256:       snapshot.skill.SourceSHA256,
		GuidanceSHA256:     snapshot.skill.GuidanceSHA256,
		CatalogFingerprint: catalogFingerprint,
		Enabled:            request.Enabled,
		DecidedAt:          now().UTC(),
	}
	if err := validateEvent(event); err != nil {
		return SelectionEvent{}, err
	}
	saved, err := s.repository.Append(ctx, event)
	if err != nil {
		return SelectionEvent{}, err
	}
	if saved.ID <= 0 || saved.OwnerIdentity != owner || saved.ActorIdentity != actor ||
		saved.SkillID != event.SkillID || !samePin(saved, snapshot.skill) ||
		saved.CatalogFingerprint != event.CatalogFingerprint ||
		saved.Enabled != request.Enabled || saved.DecidedAt.IsZero() {
		return SelectionEvent{}, errors.New("selection repository returned an inconsistent decision")
	}
	return saved, nil
}

// StatesForOwner returns one current state for every skill in the pinned
// catalog. The repository lookup is owner-scoped and stale decisions fail
// closed as needs_reapproval.
func (s *SelectionService) StatesForOwner(ctx context.Context, owner string) ([]SelectionState, error) {
	if s == nil || s.repository == nil {
		return nil, ErrRepositoryRequired
	}
	if err := validateIdentity(owner); err != nil {
		return nil, fmt.Errorf("owner: %w", err)
	}
	snapshots, err := s.catalogSnapshots()
	if err != nil {
		return nil, err
	}
	return s.statesForOwner(ctx, owner, snapshots)
}

func (s *SelectionService) statesForOwner(ctx context.Context, owner string, snapshots map[string]catalogSnapshot) ([]SelectionState, error) {
	catalogFingerprint := s.catalog.Fingerprint()
	latest, err := s.repository.LatestForOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("load owner skill selections: %w", err)
	}
	bySkill := make(map[string]SelectionEvent, len(latest))
	for _, event := range latest {
		if event.OwnerIdentity != owner {
			return nil, errors.New("selection repository returned another owner's event")
		}
		if err := validateEvent(event); err != nil {
			return nil, fmt.Errorf("invalid stored skill selection: %w", err)
		}
		if event.ID <= 0 {
			return nil, errors.New("selection repository returned an event without a persistent order")
		}
		if _, duplicate := bySkill[event.SkillID]; duplicate {
			return nil, fmt.Errorf("selection repository returned duplicate latest state for %q", event.SkillID)
		}
		bySkill[event.SkillID] = event
	}

	states := make([]SelectionState, 0, len(snapshots))
	for _, skill := range s.catalog.List() {
		snapshot := snapshots[skill.ID]
		state := SelectionState{
			ID:             skill.ID,
			Name:           skill.Name,
			Category:       skill.Category,
			Status:         StatusNotSelected,
			SourceCommit:   snapshot.skill.Commit,
			SourceSHA256:   snapshot.skill.SourceSHA256,
			GuidanceSHA256: snapshot.skill.GuidanceSHA256,
			summary:        snapshot.summary,
		}
		event, exists := bySkill[skill.ID]
		if exists {
			eventCopy := event
			state.decisionEvent = &eventCopy
			state.Decision = decisionSummary(event)
			if !event.Enabled {
				state.Status = StatusDisabled
			} else if !samePin(event, snapshot.skill) || event.CatalogFingerprint != catalogFingerprint {
				state.Status = StatusNeedsReapproval
				state.NeedsReapproval = true
			} else {
				state.Status = StatusEnabled
				state.Enabled = true
			}
		}
		states = append(states, state)
	}
	return states, nil
}

// EnabledForOwner returns only latest enabled events whose complete immutable
// pin still matches the reviewed catalog revision.
func (s *SelectionService) EnabledForOwner(ctx context.Context, owner string) ([]SelectionEvent, error) {
	states, err := s.StatesForOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	enabled := make([]SelectionEvent, 0)
	for _, state := range states {
		if state.Status == StatusEnabled && state.decisionEvent != nil {
			enabled = append(enabled, *state.decisionEvent)
		}
	}
	return enabled, nil
}

// GuidanceForTask applies only deterministic catalog matches with current,
// owner-enabled consent. It returns no raw upstream content or executable
// material. Any lookup or catalog-integrity error returns no guidance.
func (s *SelectionService) GuidanceForTask(ctx context.Context, owner, taskType, request string) ([]AppliedGuidance, error) {
	if s == nil || s.repository == nil {
		return nil, ErrRepositoryRequired
	}
	if err := validateIdentity(owner); err != nil {
		return nil, fmt.Errorf("owner: %w", err)
	}
	if err := brainskills.ValidateMatchInput(taskType, request); err != nil {
		return nil, err
	}
	snapshots, err := s.catalogSnapshots()
	if err != nil {
		return nil, err
	}
	matched := s.catalog.Match(taskType, request)
	if len(matched) == 0 {
		return []AppliedGuidance{}, nil
	}
	states, err := s.statesForOwner(ctx, owner, snapshots)
	if err != nil {
		return nil, err
	}
	stateByID := make(map[string]SelectionState, len(states))
	for _, state := range states {
		stateByID[state.ID] = state
	}

	result := make([]AppliedGuidance, 0, len(matched))
	seen := make(map[string]struct{}, len(matched))
	for _, candidate := range matched {
		if _, duplicate := seen[candidate.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate task match %q", ErrInvalidCatalog, candidate.ID)
		}
		seen[candidate.ID] = struct{}{}
		state, exists := stateByID[candidate.ID]
		if !exists {
			return nil, fmt.Errorf("%w: task match %q is not allowlisted", ErrInvalidCatalog, candidate.ID)
		}
		if candidate.Name != state.Name || candidate.Commit != state.SourceCommit ||
			candidate.SourceSHA256 != state.SourceSHA256 || candidate.GuidanceSHA256 != state.GuidanceSHA256 {
			return nil, fmt.Errorf("%w: task match pin differs for %q", ErrInvalidCatalog, candidate.ID)
		}
		if state.Status != StatusEnabled {
			continue
		}
		decision := state.Decision
		if decision == nil || decision.ID <= 0 || !decision.Enabled ||
			decision.SourceCommit != candidate.Commit || decision.SourceSHA256 != candidate.SourceSHA256 ||
			decision.GuidanceSHA256 != candidate.GuidanceSHA256 {
			return nil, fmt.Errorf("%w: enabled selection for %q has no matching durable decision", ErrInvalidCatalog, candidate.ID)
		}
		result = append(result, AppliedGuidance{
			ID:                  candidate.ID,
			Name:                candidate.Name,
			Commit:              candidate.Commit,
			SourceSHA256:        candidate.SourceSHA256,
			GuidanceSHA256:      candidate.GuidanceSHA256,
			SelectionDecisionID: decision.ID,
			Summary:             state.summary,
		})
	}
	return result, nil
}

type catalogSnapshot struct {
	skill   brainskills.Skill
	summary string
}

func (s *SelectionService) catalogSnapshots() (map[string]catalogSnapshot, error) {
	if s == nil {
		return nil, ErrInvalidCatalog
	}
	listed := s.catalog.List()
	if len(listed) == 0 {
		return nil, fmt.Errorf("%w: empty catalog", ErrInvalidCatalog)
	}
	snapshots := make(map[string]catalogSnapshot, len(listed))
	for _, skill := range listed {
		if _, duplicate := snapshots[skill.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate skill id %q", ErrInvalidCatalog, skill.ID)
		}
		if err := validateCatalogSkill(skill); err != nil {
			return nil, err
		}
		summary, ok := s.catalog.GuidanceFor(skill.ID)
		if !ok || !validGuidanceSummary(summary, skill.GuidanceSHA256) {
			return nil, fmt.Errorf("%w: guidance summary failed integrity or size validation for %q", ErrInvalidCatalog, skill.ID)
		}
		snapshots[skill.ID] = catalogSnapshot{skill: skill, summary: summary}
	}
	return snapshots, nil
}

func validateCatalogSkill(skill brainskills.Skill) error {
	if !validSkillID(skill.ID) {
		return fmt.Errorf("%w: invalid skill id", ErrInvalidCatalog)
	}
	if skill.Repository != brainskills.RepositoryURL || skill.Commit != brainskills.SourceCommit ||
		skill.License != brainskills.License || skill.Status != brainskills.Status ||
		skill.Scope != brainskills.Scope || skill.Bundled || strings.TrimSpace(skill.Boundary) == "" ||
		strings.TrimSpace(skill.Name) == "" {
		return fmt.Errorf("%w: unreviewed metadata for %q", ErrInvalidCatalog, skill.ID)
	}
	wantSourcePath := path.Join("skills", skill.ID, "SKILL.md")
	wantLicensePath := path.Join("skills", skill.ID, "LICENSE.txt")
	if skill.SourcePath != wantSourcePath || skill.LicensePath != wantLicensePath ||
		skill.SourceURL != brainskills.RepositoryURL+"/blob/"+skill.Commit+"/"+wantSourcePath ||
		skill.LicenseURL != brainskills.RepositoryURL+"/blob/"+skill.Commit+"/"+wantLicensePath {
		return fmt.Errorf("%w: source or license path is not pinned for %q", ErrInvalidCatalog, skill.ID)
	}
	if !isLowerHex(skill.Commit, 40) || !isLowerHex(skill.SourceSHA256, 64) ||
		!isLowerHex(skill.LicenseSHA256, 64) || !isLowerHex(skill.GuidanceSHA256, 64) {
		return fmt.Errorf("%w: malformed immutable pin for %q", ErrInvalidCatalog, skill.ID)
	}
	return nil
}

func validateEvent(event SelectionEvent) error {
	if err := validateIdentity(event.OwnerIdentity); err != nil {
		return fmt.Errorf("%w: owner identity", ErrInvalidInput)
	}
	if err := validateIdentity(event.ActorIdentity); err != nil {
		return fmt.Errorf("%w: actor identity", ErrInvalidInput)
	}
	if !validSkillID(event.SkillID) {
		return fmt.Errorf("%w: skill id", ErrInvalidInput)
	}
	if !isLowerHex(event.SourceCommit, 40) || !isLowerHex(event.SourceSHA256, 64) || !isLowerHex(event.GuidanceSHA256, 64) {
		return fmt.Errorf("%w: malformed skill pin", ErrInvalidInput)
	}
	if event.CatalogFingerprint != "" && !isLowerHex(event.CatalogFingerprint, 64) {
		return fmt.Errorf("%w: malformed catalog fingerprint", ErrInvalidInput)
	}
	if event.DecidedAt.IsZero() {
		return fmt.Errorf("%w: missing decision time", ErrInvalidInput)
	}
	if event.ID < 0 {
		return fmt.Errorf("%w: negative event id", ErrInvalidInput)
	}
	return nil
}

func validSkillID(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	previousDash := false
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			previousDash = false
			continue
		}
		if c == '-' && !previousDash && i < len(value)-1 {
			previousDash = true
			continue
		}
		return false
	}
	return true
}

func sameCatalogPin(left, right brainskills.Skill) bool {
	return left == right
}

func validateIdentity(value string) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > 255 {
		return ErrInvalidInput
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ErrInvalidInput
		}
	}
	return nil
}

func isLowerHex(value string, length int) bool {
	if len(value) != length || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded)*2 == length
}

func digestMatches(value, want string) bool {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:]) == want
}

func validGuidanceSummary(value, wantHash string) bool {
	return value != "" && len(value) <= brainskills.MaxGuidanceSummaryBytes &&
		utf8.ValidString(value) && digestMatches(value, wantHash)
}

func samePin(event SelectionEvent, skill brainskills.Skill) bool {
	return event.SourceCommit == skill.Commit && event.SourceSHA256 == skill.SourceSHA256 && event.GuidanceSHA256 == skill.GuidanceSHA256
}

func decisionSummary(event SelectionEvent) *SelectionDecisionSummary {
	return &SelectionDecisionSummary{
		ID: event.ID, Enabled: event.Enabled, SourceCommit: event.SourceCommit,
		SourceSHA256: event.SourceSHA256, GuidanceSHA256: event.GuidanceSHA256,
		ActorIdentity: event.ActorIdentity, DecidedAt: event.DecidedAt,
	}
}

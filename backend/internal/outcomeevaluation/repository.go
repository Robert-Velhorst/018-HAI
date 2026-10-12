package outcomeevaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type Repository interface {
	SaveOutcome(context.Context, string, string, OutcomeRevision, int64, WriteToken) (OutcomeRevision, bool, error)
	GetOutcome(context.Context, string, string, string) (OutcomeRevision, error)
	ListOutcomeHistory(context.Context, string, string, string) ([]OutcomeRevision, error)
	AppendEvaluation(context.Context, string, string, string, EvaluationRecord, WriteToken) (EvaluationRecord, bool, error)
	GetEvaluation(context.Context, string, string, string, string) (EvaluationRecord, error)
	ListEvaluations(context.Context, string, string, string) ([]EvaluationRecord, error)
	AppendCorrection(context.Context, string, string, string, CorrectionRecord, WriteToken) (CorrectionRecord, bool, error)
	ListCorrections(context.Context, string, string, string) ([]CorrectionRecord, error)
}

// OutcomeRevisionResolver resolves one immutable outcome revision by its
// complete historical identity. Implementations must not substitute the
// latest revision when either selector does not match.
type OutcomeRevisionResolver interface {
	ResolveOutcomeRevision(context.Context, string, string, string, int64, string) (OutcomeRevision, error)
}

type HistoryLimits struct {
	OutcomeRevisions int
	Evaluations      int
	Corrections      int
}

var defaultHistoryLimits = HistoryLimits{
	OutcomeRevisions: 20,
	Evaluations:      100,
	Corrections:      100,
}

const maxHistoryLimit = 10000

type MemoryRepository struct {
	mu     sync.RWMutex
	limits HistoryLimits
	data   map[repositoryKey]*memoryOutcomeState
}

type repositoryKey struct {
	ownerID     string
	workspaceID string
	outcomeID   string
}

type memoryOutcomeState struct {
	revisions                  []OutcomeRevision
	exactRevisions             map[int64]OutcomeRevision
	evaluations                []EvaluationRecord
	corrections                []CorrectionRecord
	outcomeIdempotency         map[string]outcomeIdempotencyEntry
	evaluationIdempotency      map[string]evaluationIdempotencyEntry
	correctionIdempotency      map[string]correctionIdempotencyEntry
	outcomeIdempotencyOrder    []string
	evaluationIdempotencyOrder []string
	correctionIdempotencyOrder []string
}

type outcomeIdempotencyEntry struct {
	digest string
	record OutcomeRevision
}

type evaluationIdempotencyEntry struct {
	digest string
	record EvaluationRecord
}

type correctionIdempotencyEntry struct {
	digest string
	record CorrectionRecord
}

func NewMemoryRepository() *MemoryRepository {
	repository, _ := NewMemoryRepositoryWithLimits(defaultHistoryLimits)
	return repository
}

func NewMemoryRepositoryWithLimits(limits HistoryLimits) (*MemoryRepository, error) {
	if err := validateHistoryLimits(limits); err != nil {
		return nil, err
	}
	return &MemoryRepository{limits: limits, data: make(map[repositoryKey]*memoryOutcomeState)}, nil
}

func validateHistoryLimits(limits HistoryLimits) error {
	for name, value := range map[string]int{
		"outcome revisions": limits.OutcomeRevisions,
		"evaluations":       limits.Evaluations,
		"corrections":       limits.Corrections,
	} {
		if value < 1 || value > maxHistoryLimit {
			return invalid("%s history limit must be between 1 and %d", name, maxHistoryLimit)
		}
	}
	return nil
}

func (r *MemoryRepository) SaveOutcome(ctx context.Context, ownerID, workspaceID string, record OutcomeRevision, expectedRevision int64, token WriteToken) (OutcomeRevision, bool, error) {
	if err := contextError(ctx); err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := r.ready(); err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return OutcomeRevision{}, false, err
	}
	if record.Outcome.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) {
		return OutcomeRevision{}, false, ErrScopeViolation
	}
	if record.Revision != expectedRevision+1 || expectedRevision < 0 {
		return OutcomeRevision{}, false, ErrRevisionConflict
	}
	if err := VerifyOutcomeRevisionDigest(record); err != nil {
		return OutcomeRevision{}, false, err
	}
	key := repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: record.Outcome.ID}

	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.data[key]
	if state != nil {
		if entry, ok := state.outcomeIdempotency[token.Key]; ok {
			if entry.digest != token.RequestDigest {
				return OutcomeRevision{}, false, ErrIdempotencyConflict
			}
			value, err := cloneValue(entry.record)
			return value, false, err
		}
	}
	currentRevision := int64(0)
	if state != nil && len(state.revisions) > 0 {
		currentRevision = state.revisions[len(state.revisions)-1].Revision
	}
	if currentRevision != expectedRevision {
		return OutcomeRevision{}, false, ErrRevisionConflict
	}
	if state == nil {
		state = newMemoryOutcomeState()
		r.data[key] = state
	}
	stored, err := cloneValue(record)
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	state.revisions = append(state.revisions, stored)
	state.exactRevisions[stored.Revision] = stored
	state.revisions = trimFront(state.revisions, r.limits.OutcomeRevisions)
	state.outcomeIdempotency[token.Key] = outcomeIdempotencyEntry{digest: token.RequestDigest, record: stored}
	state.outcomeIdempotencyOrder = append(state.outcomeIdempotencyOrder, token.Key)
	trimIdempotency(state.outcomeIdempotencyOrder, r.limits.OutcomeRevisions, func(key string) { delete(state.outcomeIdempotency, key) }, &state.outcomeIdempotencyOrder)
	result, err := cloneValue(stored)
	return result, true, err
}

// ResolveOutcomeRevision returns only the revision identified by both the
// revision number and audit digest. The exact-revision index is intentionally
// independent from the bounded history listing so delayed replay remains
// possible for the lifetime of the memory repository.
func (r *MemoryRepository) ResolveOutcomeRevision(ctx context.Context, ownerID, workspaceID, outcomeID string, revision int64, auditDigest string) (OutcomeRevision, error) {
	if err := contextError(ctx); err != nil {
		return OutcomeRevision{}, err
	}
	if err := r.ready(); err != nil {
		return OutcomeRevision{}, err
	}
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := validateOutcomeRevisionSelector(revision, auditDigest); err != nil {
		return OutcomeRevision{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil {
		return OutcomeRevision{}, ErrNotFound
	}
	value, found := state.exactRevisions[revision]
	if !found {
		return OutcomeRevision{}, ErrNotFound
	}
	if err := VerifyOutcomeRevisionDigest(value); err != nil {
		return OutcomeRevision{}, err
	}
	if !equalSHA256(value.AuditDigest, auditDigest) {
		return OutcomeRevision{}, ErrNotFound
	}
	return cloneValue(value)
}

func (r *MemoryRepository) GetOutcome(ctx context.Context, ownerID, workspaceID, outcomeID string) (OutcomeRevision, error) {
	if err := contextError(ctx); err != nil {
		return OutcomeRevision{}, err
	}
	if err := r.ready(); err != nil {
		return OutcomeRevision{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil || len(state.revisions) == 0 {
		return OutcomeRevision{}, ErrNotFound
	}
	value := state.revisions[len(state.revisions)-1]
	if err := VerifyOutcomeRevisionDigest(value); err != nil {
		return OutcomeRevision{}, err
	}
	return cloneValue(value)
}

func (r *MemoryRepository) ListOutcomeHistory(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]OutcomeRevision, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil || len(state.revisions) == 0 {
		return nil, ErrNotFound
	}
	for _, value := range state.revisions {
		if err := VerifyOutcomeRevisionDigest(value); err != nil {
			return nil, err
		}
	}
	return cloneValue(state.revisions)
}

func (r *MemoryRepository) AppendEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID string, record EvaluationRecord, token WriteToken) (EvaluationRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := r.ready(); err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return EvaluationRecord{}, false, err
	}
	if record.Evaluation.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) || record.Evaluation.OutcomeID != outcomeID {
		return EvaluationRecord{}, false, ErrScopeViolation
	}
	if err := VerifyEvaluationRecordDigest(record); err != nil {
		return EvaluationRecord{}, false, err
	}
	key := repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.data[key]
	if state == nil || len(state.revisions) == 0 {
		return EvaluationRecord{}, false, ErrNotFound
	}
	if token.OutcomeAuditDigest != "" {
		if err := validateOutcomeRevisionSelector(record.OutcomeRevision, token.OutcomeAuditDigest); err != nil {
			return EvaluationRecord{}, false, err
		}
		revision, ok := state.exactRevisions[record.OutcomeRevision]
		if !ok {
			return EvaluationRecord{}, false, ErrNotFound
		}
		if err := verifyOutcomeRevisionScope(revision, ownerID, workspaceID, outcomeID); err != nil {
			return EvaluationRecord{}, false, err
		}
		if revision.Revision != record.OutcomeRevision || !equalSHA256(revision.AuditDigest, token.OutcomeAuditDigest) {
			return EvaluationRecord{}, false, ErrNotFound
		}
	}
	if entry, ok := state.evaluationIdempotency[token.Key]; ok {
		if entry.digest != token.RequestDigest {
			return EvaluationRecord{}, false, ErrIdempotencyConflict
		}
		if token.OutcomeAuditDigest != "" {
			if err := verifyPinnedEvaluationResult(entry.record, record); err != nil {
				return EvaluationRecord{}, false, err
			}
		}
		value, err := cloneValue(entry.record)
		return value, false, err
	}
	if token.OutcomeAuditDigest == "" {
		if state.revisions[len(state.revisions)-1].Revision != record.OutcomeRevision {
			return EvaluationRecord{}, false, ErrRevisionConflict
		}
	}
	stored, err := cloneValue(record)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	state.evaluations = trimFront(append(state.evaluations, stored), r.limits.Evaluations)
	state.evaluationIdempotency[token.Key] = evaluationIdempotencyEntry{digest: token.RequestDigest, record: stored}
	state.evaluationIdempotencyOrder = append(state.evaluationIdempotencyOrder, token.Key)
	trimIdempotency(state.evaluationIdempotencyOrder, r.limits.Evaluations, func(key string) { delete(state.evaluationIdempotency, key) }, &state.evaluationIdempotencyOrder)
	value, err := cloneValue(stored)
	return value, true, err
}

func (r *MemoryRepository) GetEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID, evaluationID string) (EvaluationRecord, error) {
	if err := contextError(ctx); err != nil {
		return EvaluationRecord{}, err
	}
	if err := r.ready(); err != nil {
		return EvaluationRecord{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil {
		return EvaluationRecord{}, ErrNotFound
	}
	for i := len(state.evaluations) - 1; i >= 0; i-- {
		if state.evaluations[i].Evaluation.ID == evaluationID {
			if err := VerifyEvaluationRecordDigest(state.evaluations[i]); err != nil {
				return EvaluationRecord{}, err
			}
			return cloneValue(state.evaluations[i])
		}
	}
	return EvaluationRecord{}, ErrNotFound
}

func (r *MemoryRepository) ListEvaluations(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]EvaluationRecord, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil || len(state.revisions) == 0 {
		return nil, ErrNotFound
	}
	for _, value := range state.evaluations {
		if err := VerifyEvaluationRecordDigest(value); err != nil {
			return nil, err
		}
	}
	return cloneValue(state.evaluations)
}

func (r *MemoryRepository) AppendCorrection(ctx context.Context, ownerID, workspaceID, outcomeID string, record CorrectionRecord, token WriteToken) (CorrectionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := r.ready(); err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return CorrectionRecord{}, false, err
	}
	expectedScope := Scope{OwnerID: ownerID, WorkspaceID: workspaceID}
	if record.OutcomeID != outcomeID || record.Observation.Scope != expectedScope || record.Correction.Scope != expectedScope {
		return CorrectionRecord{}, false, ErrScopeViolation
	}
	if err := VerifyCorrectionRecordDigest(record); err != nil {
		return CorrectionRecord{}, false, err
	}
	key := repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.data[key]
	if state == nil || len(state.revisions) == 0 {
		return CorrectionRecord{}, false, ErrNotFound
	}
	if entry, ok := state.correctionIdempotency[token.Key]; ok {
		if entry.digest != token.RequestDigest {
			return CorrectionRecord{}, false, ErrIdempotencyConflict
		}
		value, err := cloneValue(entry.record)
		return value, false, err
	}
	if state.revisions[len(state.revisions)-1].Revision != record.OutcomeRevision {
		return CorrectionRecord{}, false, ErrRevisionConflict
	}
	stored, err := cloneValue(record)
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	state.corrections = trimFront(append(state.corrections, stored), r.limits.Corrections)
	state.correctionIdempotency[token.Key] = correctionIdempotencyEntry{digest: token.RequestDigest, record: stored}
	state.correctionIdempotencyOrder = append(state.correctionIdempotencyOrder, token.Key)
	trimIdempotency(state.correctionIdempotencyOrder, r.limits.Corrections, func(key string) { delete(state.correctionIdempotency, key) }, &state.correctionIdempotencyOrder)
	value, err := cloneValue(stored)
	return value, true, err
}

func (r *MemoryRepository) ListCorrections(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]CorrectionRecord, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := r.data[repositoryKey{ownerID: ownerID, workspaceID: workspaceID, outcomeID: outcomeID}]
	if state == nil || len(state.revisions) == 0 {
		return nil, ErrNotFound
	}
	for _, value := range state.corrections {
		if err := VerifyCorrectionRecordDigest(value); err != nil {
			return nil, err
		}
	}
	return cloneValue(state.corrections)
}

func newMemoryOutcomeState() *memoryOutcomeState {
	return &memoryOutcomeState{
		outcomeIdempotency:    make(map[string]outcomeIdempotencyEntry),
		evaluationIdempotency: make(map[string]evaluationIdempotencyEntry),
		correctionIdempotency: make(map[string]correctionIdempotencyEntry),
		exactRevisions:        make(map[int64]OutcomeRevision),
	}
}

func trimFront[T any](values []T, limit int) []T {
	if len(values) <= limit {
		return values
	}
	trimmed := make([]T, limit)
	copy(trimmed, values[len(values)-limit:])
	return trimmed
}

func trimIdempotency(order []string, limit int, remove func(string), target *[]string) {
	if len(order) <= limit {
		return
	}
	for _, key := range order[:len(order)-limit] {
		remove(key)
	}
	*target = append([]string(nil), order[len(order)-limit:]...)
}

func cloneValue[T any](value T) (T, error) {
	var clone T
	encoded, err := json.Marshal(value)
	if err != nil {
		return clone, fmt.Errorf("clone outcome evaluation record: %w", err)
	}
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return clone, fmt.Errorf("clone outcome evaluation record: %w", err)
	}
	return clone, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return invalid("context is required")
	}
	return ctx.Err()
}

func (r *MemoryRepository) ready() error {
	if r == nil || r.data == nil {
		return errorsUnavailable()
	}
	return validateHistoryLimits(r.limits)
}

func validateWriteToken(token WriteToken) error {
	if err := validateText("idempotency key", token.Key, maxIDRunes, true); err != nil {
		return err
	}
	if !sha256Pattern.MatchString(token.RequestDigest) {
		return invalid("request digest must be a lowercase SHA-256 digest")
	}
	return nil
}

func validateOutcomeRevisionSelector(revision int64, auditDigest string) error {
	if revision < 1 {
		return invalid("outcome revision must be positive")
	}
	if !sha256Pattern.MatchString(auditDigest) {
		return invalid("outcome audit digest must be a lowercase SHA-256 digest")
	}
	return nil
}

func errorsUnavailable() error {
	return fmt.Errorf("outcome evaluation repository is unavailable")
}

var _ OutcomeRevisionResolver = (*MemoryRepository)(nil)

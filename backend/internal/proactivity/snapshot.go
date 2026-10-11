package proactivity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

var (
	ErrSnapshotInvalid     = errors.New("proactivity evaluation snapshot is invalid")
	ErrSnapshotUnavailable = errors.New("proactivity evaluation snapshot is unavailable")
)

// Durable monitor transitions may advance a coarse host clock by one
// microsecond for a claim and once more for its completion. Accept only that
// bounded logical-clock movement; materially future snapshots remain invalid.
const snapshotMonotonicClockAllowance = 2 * time.Microsecond

// PolicySnapshotReference identifies one immutable policy record. All three
// fields are required so a replay cannot silently substitute another policy.
type PolicySnapshotReference struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	PayloadDigest  string    `json:"payloadDigest"`
	RecordedAt     time.Time `json:"recordedAt"`
}

// SnapshotRecordCursor is an inclusive upper cursor in the immutable ordering
// used by signal and decision ledgers.
type SnapshotRecordCursor struct {
	RecordedAt     time.Time `json:"recordedAt"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Ordinal        int       `json:"ordinal"`
	PayloadDigest  string    `json:"payloadDigest"`
}

// SnapshotWatermark binds a bounded ledger window to its head cursor, exact
// item count, and canonical digest. A zero-count watermark has no cursor but
// still carries the digest of the empty window.
type SnapshotWatermark struct {
	Cursor       *SnapshotRecordCursor `json:"cursor,omitempty"`
	Count        int                   `json:"count"`
	WindowDigest string                `json:"windowDigest"`
}

// FeedbackSnapshotCursor is the feedback-ledger equivalent of
// SnapshotRecordCursor. RecordDigest binds the append-only feedback chain;
// PayloadDigest binds the original idempotent feedback request.
type FeedbackSnapshotCursor struct {
	RecordedAt     time.Time `json:"recordedAt"`
	FeedbackID     string    `json:"feedbackId"`
	IdempotencyKey string    `json:"idempotencyKey"`
	PayloadDigest  string    `json:"payloadDigest"`
	RecordDigest   string    `json:"recordDigest"`
}

type FeedbackSnapshotWatermark struct {
	Cursor       *FeedbackSnapshotCursor `json:"cursor,omitempty"`
	Count        int                     `json:"count"`
	WindowDigest string                  `json:"windowDigest"`
}

// EvaluationSnapshot is a compact immutable input contract for deterministic
// owner-attention replay. InputDigest covers every field except itself.
type EvaluationSnapshot struct {
	ContractVersion int                       `json:"contractVersion"`
	OwnerIdentity   string                    `json:"ownerIdentity"`
	CapturedAt      time.Time                 `json:"capturedAt"`
	Policy          PolicySnapshotReference   `json:"policy"`
	Signals         SnapshotWatermark         `json:"signals"`
	Decisions       SnapshotWatermark         `json:"decisions"`
	Feedback        FeedbackSnapshotWatermark `json:"feedback"`
	InputDigest     string                    `json:"inputDigest"`
}

type EvaluateStoredSnapshotRequest struct {
	IdempotencyKey    string             `json:"idempotencyKey"`
	Now               time.Time          `json:"now"`
	Snapshot          EvaluationSnapshot `json:"snapshot"`
	AdditionalSignals []OpenLoopSignal   `json:"additionalSignals,omitempty"`
}

// VerifyEvaluationSnapshot performs pure contract, scope, cursor, and digest
// validation. It does not access storage and is safe for callers validating
// persisted snapshot JSON before starting composition.
func VerifyEvaluationSnapshot(owner string, snapshot EvaluationSnapshot) error {
	owner, err := validateServiceIdentity(owner)
	if err != nil {
		return err
	}
	_, err = validateEvaluationSnapshot(owner, snapshot)
	return err
}

type snapshotPolicyMaterial struct {
	Reference PolicySnapshotReference
	Record    PolicyRecord
}

type snapshotSignalMaterial struct {
	Cursor SnapshotRecordCursor
	Record SignalRecord
}

type snapshotDecisionMaterial struct {
	Cursor SnapshotRecordCursor
	Record DecisionRecord
}

type snapshotFeedbackMaterial struct {
	Cursor FeedbackSnapshotCursor
	Record FeedbackRecord
}

type evaluationSnapshotState struct {
	Policy    snapshotPolicyMaterial
	Signals   []snapshotSignalMaterial
	Decisions []snapshotDecisionMaterial
	Feedback  []snapshotFeedbackMaterial
}

// snapshotRepository is deliberately package-private. Snapshot material is a
// verified persistence concern; callers receive only the compact exported
// EvaluationSnapshot contract.
type snapshotRepository interface {
	captureEvaluationSnapshotState(context.Context, string, time.Time) (evaluationSnapshotState, error)
	resolveEvaluationSnapshotState(context.Context, string, EvaluationSnapshot) (evaluationSnapshotState, error)
}

var (
	_ snapshotRepository = (*MemoryRepository)(nil)
	_ snapshotRepository = (*PostgresRepository)(nil)
)

// CaptureEvaluationSnapshot captures exact bounded policy, signal, decision,
// and feedback inputs at or before at. It is read-only and grants no authority.
func (s *Service) CaptureEvaluationSnapshot(ctx context.Context, owner string, at time.Time) (EvaluationSnapshot, error) {
	owner, err := validateServiceIdentity(owner)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	if at.IsZero() {
		return EvaluationSnapshot{}, fmt.Errorf("%w: capture time is required", ErrSnapshotInvalid)
	}
	if err := s.available(); err != nil {
		return EvaluationSnapshot{}, err
	}
	at = snapshotTime(at)
	now := snapshotTime(s.now())
	if at.After(now) && at.Sub(now) > snapshotMonotonicClockAllowance {
		return EvaluationSnapshot{}, fmt.Errorf("%w: capture time cannot be in the future", ErrSnapshotInvalid)
	}
	repository, ok := s.repository.(snapshotRepository)
	if !ok || repository == nil {
		return EvaluationSnapshot{}, ErrSnapshotUnavailable
	}
	state, err := repository.captureEvaluationSnapshotState(ctx, owner, at)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	state = canonicalEvaluationSnapshotState(state)
	if err := validateEvaluationSnapshotState(owner, at, state); err != nil {
		return EvaluationSnapshot{}, err
	}
	snapshot, err := snapshotFromState(owner, at, state)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	validated, err := validateEvaluationSnapshot(owner, snapshot)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	return cloneEvaluationSnapshot(validated), nil
}

// EvaluateStoredSnapshot evaluates and records advisory decisions from exactly
// the immutable records named by request.Snapshot. EvaluateStored retains its
// existing current-state behavior and does not call this method.
func (s *Service) EvaluateStoredSnapshot(ctx context.Context, owner string, request EvaluateStoredSnapshotRequest) (DecisionBatch, bool, error) {
	owner, err := validateServiceIdentity(owner)
	if err != nil {
		return DecisionBatch{}, false, err
	}
	if err := validateIdempotencyKey(request.IdempotencyKey); err != nil {
		return DecisionBatch{}, false, err
	}
	if request.Now.IsZero() {
		return DecisionBatch{}, false, errors.New("evaluation time is required")
	}
	request.Now = snapshotTime(request.Now)
	if err := s.available(); err != nil {
		return DecisionBatch{}, false, err
	}
	snapshot, err := validateEvaluationSnapshot(owner, request.Snapshot)
	if err != nil {
		return DecisionBatch{}, false, err
	}
	if request.Now.Before(snapshot.CapturedAt) {
		return DecisionBatch{}, false, fmt.Errorf("%w: evaluation time precedes capture time", ErrSnapshotInvalid)
	}
	repository, ok := s.repository.(snapshotRepository)
	if !ok || repository == nil {
		return DecisionBatch{}, false, ErrSnapshotUnavailable
	}
	state, err := repository.resolveEvaluationSnapshotState(ctx, owner, snapshot)
	if err != nil {
		return DecisionBatch{}, false, err
	}
	state = canonicalEvaluationSnapshotState(state)
	if err := validateEvaluationSnapshotState(owner, snapshot.CapturedAt, state); err != nil {
		return DecisionBatch{}, false, err
	}
	if err := verifySnapshotState(owner, snapshot, state); err != nil {
		return DecisionBatch{}, false, err
	}
	baselineSignals := latestSnapshotSignals(state.Signals)
	additionalSignals, additionalSignalsDigest, err := normalizeSnapshotAdditionalSignals(
		owner, request.AdditionalSignals, request.Now, baselineSignals,
	)
	if err != nil {
		return DecisionBatch{}, false, err
	}

	digest, err := snapshotDecisionDigest(owner, request.Now, snapshot.InputDigest, additionalSignalsDigest)
	if err != nil {
		return DecisionBatch{}, false, fmt.Errorf("digest exact proactivity evaluation: %w", err)
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if existing, found, findErr := s.repository.FindDecisionBatch(ctx, owner, key, digest); findErr != nil {
		return DecisionBatch{}, false, findErr
	} else if found {
		cleaned, cleanErr := validateStoredDecisionBatch(owner, cloneDecisionBatch(existing))
		if cleanErr != nil {
			return DecisionBatch{}, false, cleanErr
		}
		if cleaned.SnapshotInputDigest != snapshot.InputDigest {
			return DecisionBatch{}, false, ErrIdempotencyConflict
		}
		if cleaned.AdditionalSignalsDigest != additionalSignalsDigest {
			return DecisionBatch{}, false, ErrIdempotencyConflict
		}
		return cleaned, false, nil
	}
	evaluationSignals := append(baselineSignals, additionalSignals...)

	result, err := Evaluate(EvaluationRequest{
		ContractVersion: ContractVersion,
		OwnerIdentity:   owner,
		Now:             request.Now,
		Preferences:     clonePreferences(state.Policy.Record.Policy),
		Signals:         evaluationSignals,
		History:         snapshotDecisionHistory(owner, state.Decisions),
		Controls:        snapshotAttentionControls(state.Feedback),
	})
	if err != nil {
		return DecisionBatch{}, false, err
	}
	for index := range result.Decisions {
		result.Decisions[index] = advisoryDecision(result.Decisions[index])
	}
	batch := DecisionBatch{
		ContractVersion:         ContractVersion,
		OwnerIdentity:           owner,
		SnapshotInputDigest:     snapshot.InputDigest,
		AdditionalSignalsDigest: additionalSignalsDigest,
		Result:                  result,
		RecordedAt:              snapshotTime(s.now()),
	}
	stored, created, err := s.repository.RecordDecisionBatch(ctx, owner, key, digest, batch)
	if err != nil {
		return DecisionBatch{}, false, err
	}
	cleaned, err := validateStoredDecisionBatch(owner, cloneDecisionBatch(stored))
	if err == nil && cleaned.SnapshotInputDigest != snapshot.InputDigest {
		return DecisionBatch{}, false, ErrIdempotencyConflict
	}
	if err == nil && cleaned.AdditionalSignalsDigest != additionalSignalsDigest {
		return DecisionBatch{}, false, ErrIdempotencyConflict
	}
	return cleaned, created, err
}

func snapshotFromState(owner string, at time.Time, state evaluationSnapshotState) (EvaluationSnapshot, error) {
	signalDigest, err := signalSnapshotWindowDigest(owner, state.Signals)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	decisionDigest, err := decisionSnapshotWindowDigest(owner, state.Decisions)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	feedbackDigest, err := feedbackSnapshotWindowDigest(owner, state.Feedback)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	snapshot := EvaluationSnapshot{
		ContractVersion: ContractVersion,
		OwnerIdentity:   owner,
		CapturedAt:      snapshotTime(at),
		Policy:          canonicalPolicySnapshotReference(state.Policy.Reference),
		Signals: SnapshotWatermark{
			Cursor: cloneSnapshotRecordCursor(firstSignalCursor(state.Signals)),
			Count:  len(state.Signals), WindowDigest: signalDigest,
		},
		Decisions: SnapshotWatermark{
			Cursor: cloneSnapshotRecordCursor(firstDecisionCursor(state.Decisions)),
			Count:  len(state.Decisions), WindowDigest: decisionDigest,
		},
		Feedback: FeedbackSnapshotWatermark{
			Cursor: cloneFeedbackSnapshotCursor(firstFeedbackCursor(state.Feedback)),
			Count:  len(state.Feedback), WindowDigest: feedbackDigest,
		},
	}
	snapshot.InputDigest, err = evaluationSnapshotDigest(owner, snapshot)
	return snapshot, err
}

func validateEvaluationSnapshot(owner string, snapshot EvaluationSnapshot) (EvaluationSnapshot, error) {
	snapshot = cloneEvaluationSnapshot(snapshot)
	snapshot = canonicalEvaluationSnapshot(snapshot)
	snapshot.OwnerIdentity = strings.TrimSpace(snapshot.OwnerIdentity)
	snapshot.InputDigest = strings.ToLower(strings.TrimSpace(snapshot.InputDigest))
	snapshot.Policy.IdempotencyKey = strings.TrimSpace(snapshot.Policy.IdempotencyKey)
	snapshot.Policy.PayloadDigest = strings.ToLower(strings.TrimSpace(snapshot.Policy.PayloadDigest))
	snapshot.Signals.WindowDigest = strings.ToLower(strings.TrimSpace(snapshot.Signals.WindowDigest))
	snapshot.Decisions.WindowDigest = strings.ToLower(strings.TrimSpace(snapshot.Decisions.WindowDigest))
	snapshot.Feedback.WindowDigest = strings.ToLower(strings.TrimSpace(snapshot.Feedback.WindowDigest))
	if snapshot.ContractVersion != ContractVersion || snapshot.OwnerIdentity != owner || snapshot.CapturedAt.IsZero() {
		return EvaluationSnapshot{}, fmt.Errorf("%w: contract, owner, or capture time mismatch", ErrSnapshotInvalid)
	}
	if err := validatePolicySnapshotReference(snapshot.Policy, snapshot.CapturedAt); err != nil {
		return EvaluationSnapshot{}, err
	}
	if err := validateSnapshotWatermark("signals", snapshot.Signals, MaxSignalHistory, snapshot.CapturedAt); err != nil {
		return EvaluationSnapshot{}, err
	}
	if err := validateSnapshotWatermark("decisions", snapshot.Decisions, MaxDecisionHistory, snapshot.CapturedAt); err != nil {
		return EvaluationSnapshot{}, err
	}
	if err := validateFeedbackSnapshotWatermark(snapshot.Feedback, snapshot.CapturedAt); err != nil {
		return EvaluationSnapshot{}, err
	}
	if !digestPattern.MatchString(snapshot.InputDigest) {
		return EvaluationSnapshot{}, fmt.Errorf("%w: input digest is invalid", ErrSnapshotInvalid)
	}
	expected, err := evaluationSnapshotDigest(owner, snapshot)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	if snapshot.InputDigest != expected {
		return EvaluationSnapshot{}, fmt.Errorf("%w: input digest mismatch", ErrSnapshotInvalid)
	}
	return snapshot, nil
}

func validatePolicySnapshotReference(reference PolicySnapshotReference, capturedAt time.Time) error {
	if err := validateIdentifier("policy idempotency key", strings.TrimSpace(reference.IdempotencyKey)); err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshotInvalid, err)
	}
	if !digestPattern.MatchString(strings.ToLower(strings.TrimSpace(reference.PayloadDigest))) || reference.RecordedAt.IsZero() || reference.RecordedAt.After(capturedAt) {
		return fmt.Errorf("%w: policy reference is invalid", ErrSnapshotInvalid)
	}
	return nil
}

func validateSnapshotWatermark(kind string, watermark SnapshotWatermark, maximum int, capturedAt time.Time) error {
	if watermark.Count < 0 || watermark.Count > maximum || !digestPattern.MatchString(watermark.WindowDigest) {
		return fmt.Errorf("%w: %s watermark bounds or digest are invalid", ErrSnapshotInvalid, kind)
	}
	if watermark.Count == 0 {
		if watermark.Cursor != nil {
			return fmt.Errorf("%w: empty %s watermark has a cursor", ErrSnapshotInvalid, kind)
		}
		return nil
	}
	if watermark.Cursor == nil || watermark.Cursor.RecordedAt.IsZero() || watermark.Cursor.RecordedAt.After(capturedAt) || watermark.Cursor.Ordinal < 0 || watermark.Cursor.Ordinal >= MaxSignals {
		return fmt.Errorf("%w: %s cursor ordering is invalid", ErrSnapshotInvalid, kind)
	}
	if err := validateIdentifier(kind+" cursor idempotency key", strings.TrimSpace(watermark.Cursor.IdempotencyKey)); err != nil || !digestPattern.MatchString(strings.ToLower(strings.TrimSpace(watermark.Cursor.PayloadDigest))) {
		return fmt.Errorf("%w: %s cursor identity is invalid", ErrSnapshotInvalid, kind)
	}
	return nil
}

func validateFeedbackSnapshotWatermark(watermark FeedbackSnapshotWatermark, capturedAt time.Time) error {
	if watermark.Count < 0 || watermark.Count > MaxFeedbackHistory || !digestPattern.MatchString(watermark.WindowDigest) {
		return fmt.Errorf("%w: feedback watermark bounds or digest are invalid", ErrSnapshotInvalid)
	}
	if watermark.Count == 0 {
		if watermark.Cursor != nil {
			return fmt.Errorf("%w: empty feedback watermark has a cursor", ErrSnapshotInvalid)
		}
		return nil
	}
	if watermark.Cursor == nil || watermark.Cursor.RecordedAt.IsZero() || watermark.Cursor.RecordedAt.After(capturedAt) {
		return fmt.Errorf("%w: feedback cursor ordering is invalid", ErrSnapshotInvalid)
	}
	if err := validateIdentifier("feedback id", strings.TrimSpace(watermark.Cursor.FeedbackID)); err != nil {
		return fmt.Errorf("%w: feedback cursor id is invalid", ErrSnapshotInvalid)
	}
	if err := validateIdentifier("feedback idempotency key", strings.TrimSpace(watermark.Cursor.IdempotencyKey)); err != nil {
		return fmt.Errorf("%w: feedback cursor idempotency key is invalid", ErrSnapshotInvalid)
	}
	if !digestPattern.MatchString(strings.ToLower(strings.TrimSpace(watermark.Cursor.PayloadDigest))) || !digestPattern.MatchString(strings.ToLower(strings.TrimSpace(watermark.Cursor.RecordDigest))) {
		return fmt.Errorf("%w: feedback cursor digest is invalid", ErrSnapshotInvalid)
	}
	return nil
}

func validateEvaluationSnapshotState(owner string, at time.Time, state evaluationSnapshotState) error {
	policy, err := validateStoredPolicyRecord(owner, clonePolicyRecord(state.Policy.Record))
	if err != nil || !reflect.DeepEqual(policy, state.Policy.Record) || !policy.RecordedAt.Equal(state.Policy.Reference.RecordedAt) || policy.RecordedAt.After(at) {
		return fmt.Errorf("%w: policy material failed validation", ErrSnapshotUnavailable)
	}
	policyDigest, err := advisoryDigest(idempotencyKindPolicy, owner, policy.Policy)
	if err != nil || policyDigest != state.Policy.Reference.PayloadDigest {
		return fmt.Errorf("%w: policy payload digest mismatch", ErrSnapshotUnavailable)
	}
	for index, item := range state.Signals {
		if item.Record.RecordedAt.After(at) || !item.Record.RecordedAt.Equal(item.Cursor.RecordedAt) {
			return fmt.Errorf("%w: signal %d is outside its cursor", ErrSnapshotUnavailable, index)
		}
		normalized, normalizeErr := normalizeSignal(owner, cloneSignal(item.Record.Signal), item.Record.RecordedAt.UTC())
		canonical := cloneSignalRecord(item.Record)
		canonical.RecordedAt = canonical.RecordedAt.UTC()
		canonical.Signal = normalized
		if normalizeErr != nil || !reflect.DeepEqual(canonical, item.Record) {
			return fmt.Errorf("%w: signal %d failed validation", ErrSnapshotUnavailable, index)
		}
	}
	for index, item := range state.Decisions {
		if item.Record.RecordedAt.After(at) || !item.Record.RecordedAt.Equal(item.Cursor.RecordedAt) {
			return fmt.Errorf("%w: decision %d is outside its cursor", ErrSnapshotUnavailable, index)
		}
		if _, validateErr := validateStoredDecisionRecord(owner, cloneDecisionRecord(item.Record)); validateErr != nil {
			return fmt.Errorf("%w: decision %d failed no-authority validation", ErrSnapshotUnavailable, index)
		}
	}
	for index, item := range state.Feedback {
		if item.Record.RecordedAt.After(at) || !item.Record.RecordedAt.Equal(item.Cursor.RecordedAt) {
			return fmt.Errorf("%w: feedback %d is outside its cursor", ErrSnapshotUnavailable, index)
		}
		if _, validateErr := validateStoredFeedbackRecord(owner, cloneFeedbackRecord(item.Record)); validateErr != nil {
			return fmt.Errorf("%w: feedback %d failed no-authority validation", ErrSnapshotUnavailable, index)
		}
	}
	return nil
}

func verifySnapshotState(owner string, snapshot EvaluationSnapshot, state evaluationSnapshotState) error {
	if !reflect.DeepEqual(state.Policy.Reference, snapshot.Policy) {
		return fmt.Errorf("%w: exact policy reference mismatch", ErrSnapshotUnavailable)
	}
	if err := verifySignalWindow(owner, snapshot.Signals, state.Signals); err != nil {
		return err
	}
	if err := verifyDecisionWindow(owner, snapshot.Decisions, state.Decisions); err != nil {
		return err
	}
	return verifyFeedbackWindow(owner, snapshot.Feedback, state.Feedback)
}

func verifySignalWindow(owner string, watermark SnapshotWatermark, items []snapshotSignalMaterial) error {
	if len(items) != watermark.Count || !snapshotRecordCursorPointersEqual(watermark.Cursor, firstSignalCursor(items)) {
		return fmt.Errorf("%w: signal cursor or count mismatch", ErrSnapshotUnavailable)
	}
	digest, err := signalSnapshotWindowDigest(owner, items)
	if err != nil || digest != watermark.WindowDigest {
		return fmt.Errorf("%w: signal window digest mismatch", ErrSnapshotUnavailable)
	}
	return nil
}

func verifyDecisionWindow(owner string, watermark SnapshotWatermark, items []snapshotDecisionMaterial) error {
	if len(items) != watermark.Count || !snapshotRecordCursorPointersEqual(watermark.Cursor, firstDecisionCursor(items)) {
		return fmt.Errorf("%w: decision cursor or count mismatch", ErrSnapshotUnavailable)
	}
	digest, err := decisionSnapshotWindowDigest(owner, items)
	if err != nil || digest != watermark.WindowDigest {
		return fmt.Errorf("%w: decision window digest mismatch", ErrSnapshotUnavailable)
	}
	return nil
}

func verifyFeedbackWindow(owner string, watermark FeedbackSnapshotWatermark, items []snapshotFeedbackMaterial) error {
	if len(items) != watermark.Count || !feedbackSnapshotCursorPointersEqual(watermark.Cursor, firstFeedbackCursor(items)) {
		return fmt.Errorf("%w: feedback cursor or count mismatch", ErrSnapshotUnavailable)
	}
	digest, err := feedbackSnapshotWindowDigest(owner, items)
	if err != nil || digest != watermark.WindowDigest {
		return fmt.Errorf("%w: feedback window digest mismatch", ErrSnapshotUnavailable)
	}
	return nil
}

func evaluationSnapshotDigest(owner string, snapshot EvaluationSnapshot) (string, error) {
	projection := canonicalEvaluationSnapshot(cloneEvaluationSnapshot(snapshot))
	projection.InputDigest = ""
	return advisoryDigest("evaluation-snapshot", owner, projection)
}

func snapshotDecisionDigest(owner string, decidedAt time.Time, inputDigest, additionalSignalsDigest string) (string, error) {
	if additionalSignalsDigest == "" {
		return advisoryDigest(idempotencyKindDecisions, owner, struct {
			DecidedAt   time.Time `json:"decidedAt"`
			InputDigest string    `json:"inputDigest"`
		}{snapshotTime(decidedAt), inputDigest})
	}
	return advisoryDigest(idempotencyKindDecisions, owner, struct {
		DecidedAt               time.Time `json:"decidedAt"`
		InputDigest             string    `json:"inputDigest"`
		AdditionalSignalsDigest string    `json:"additionalSignalsDigest"`
	}{snapshotTime(decidedAt), inputDigest, additionalSignalsDigest})
}

func snapshotTime(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return value.UTC().Truncate(time.Microsecond)
}

func canonicalEvaluationSnapshot(value EvaluationSnapshot) EvaluationSnapshot {
	value.CapturedAt = snapshotTime(value.CapturedAt)
	value.Policy = canonicalPolicySnapshotReference(value.Policy)
	if value.Signals.Cursor != nil {
		value.Signals.Cursor.RecordedAt = snapshotTime(value.Signals.Cursor.RecordedAt)
	}
	if value.Decisions.Cursor != nil {
		value.Decisions.Cursor.RecordedAt = snapshotTime(value.Decisions.Cursor.RecordedAt)
	}
	if value.Feedback.Cursor != nil {
		value.Feedback.Cursor.RecordedAt = snapshotTime(value.Feedback.Cursor.RecordedAt)
	}
	return value
}

func canonicalPolicySnapshotReference(value PolicySnapshotReference) PolicySnapshotReference {
	value.RecordedAt = snapshotTime(value.RecordedAt)
	return value
}

func canonicalSnapshotRecordCursor(value SnapshotRecordCursor) SnapshotRecordCursor {
	value.RecordedAt = snapshotTime(value.RecordedAt)
	return value
}

func canonicalFeedbackSnapshotCursor(value FeedbackSnapshotCursor) FeedbackSnapshotCursor {
	value.RecordedAt = snapshotTime(value.RecordedAt)
	return value
}

func canonicalEvaluationSnapshotState(value evaluationSnapshotState) evaluationSnapshotState {
	result := cloneEvaluationSnapshotState(value)
	result.Policy.Reference = canonicalPolicySnapshotReference(result.Policy.Reference)
	result.Policy.Record.RecordedAt = snapshotTime(result.Policy.Record.RecordedAt)
	for index := range result.Signals {
		result.Signals[index].Cursor = canonicalSnapshotRecordCursor(result.Signals[index].Cursor)
		result.Signals[index].Record.RecordedAt = snapshotTime(result.Signals[index].Record.RecordedAt)
	}
	for index := range result.Decisions {
		result.Decisions[index].Cursor = canonicalSnapshotRecordCursor(result.Decisions[index].Cursor)
		result.Decisions[index].Record.RecordedAt = snapshotTime(result.Decisions[index].Record.RecordedAt)
	}
	for index := range result.Feedback {
		result.Feedback[index].Cursor = canonicalFeedbackSnapshotCursor(result.Feedback[index].Cursor)
		result.Feedback[index].Record.RecordedAt = snapshotTime(result.Feedback[index].Record.RecordedAt)
	}
	return result
}

func decisionBatchPayloadDigest(owner string, batch DecisionBatch) (string, error) {
	if batch.SnapshotInputDigest == "" {
		return advisoryDigest(idempotencyKindDecisions, owner, batch.Result.DecidedAt)
	}
	return snapshotDecisionDigest(owner, batch.Result.DecidedAt, batch.SnapshotInputDigest, batch.AdditionalSignalsDigest)
}

func normalizeSnapshotAdditionalSignals(owner string, values []OpenLoopSignal, now time.Time, baseline []OpenLoopSignal) ([]OpenLoopSignal, string, error) {
	if len(values) == 0 {
		return nil, "", nil
	}
	if len(values) > MaxSignals || len(baseline)+len(values) > MaxSignals {
		return nil, "", fmt.Errorf("additional signal count exceeds the bounded snapshot capacity of %d", MaxSignals)
	}
	seen := make(map[string]struct{}, len(baseline)+len(values))
	for _, signal := range baseline {
		seen[signal.ID] = struct{}{}
	}
	result := make([]OpenLoopSignal, len(values))
	for index, signal := range values {
		normalized, err := normalizeSignal(owner, cloneSignal(signal), now)
		if err != nil {
			return nil, "", fmt.Errorf("additional signal %d: %w", index, err)
		}
		if _, exists := seen[normalized.ID]; exists {
			return nil, "", fmt.Errorf("additional signal %d duplicates a pinned or added signal id", index)
		}
		seen[normalized.ID] = struct{}{}
		result[index] = normalized
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	digest, err := advisoryDigest("evaluation-snapshot-additional-signals", owner, result)
	if err != nil {
		return nil, "", fmt.Errorf("digest additional snapshot signals: %w", err)
	}
	return result, digest, nil
}

func signalSnapshotWindowDigest(owner string, values []snapshotSignalMaterial) (string, error) {
	if values == nil {
		values = []snapshotSignalMaterial{}
	}
	return advisoryDigest("evaluation-snapshot-signals", owner, values)
}

func decisionSnapshotWindowDigest(owner string, values []snapshotDecisionMaterial) (string, error) {
	if values == nil {
		values = []snapshotDecisionMaterial{}
	}
	return advisoryDigest("evaluation-snapshot-decisions", owner, values)
}

func feedbackSnapshotWindowDigest(owner string, values []snapshotFeedbackMaterial) (string, error) {
	if values == nil {
		values = []snapshotFeedbackMaterial{}
	}
	return advisoryDigest("evaluation-snapshot-feedback", owner, values)
}

func latestSnapshotSignals(items []snapshotSignalMaterial) []OpenLoopSignal {
	result := make([]OpenLoopSignal, 0, min(len(items), MaxSignals))
	seen := make(map[string]struct{}, min(len(items), MaxSignals))
	for _, item := range items {
		if _, exists := seen[item.Record.Signal.ID]; exists {
			continue
		}
		seen[item.Record.Signal.ID] = struct{}{}
		result = append(result, cloneSignal(item.Record.Signal))
		if len(result) == MaxSignals {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func snapshotDecisionHistory(owner string, items []snapshotDecisionMaterial) []DecisionHistory {
	result := make([]DecisionHistory, 0, len(items))
	for _, item := range items {
		result = append(result, DecisionHistory{
			ContractVersion: ContractVersion, OwnerIdentity: owner,
			OpenLoopKey:  item.Record.Decision.OpenLoopKey,
			SignalDigest: item.Record.Decision.SignalDigest,
			Outcome:      item.Record.Decision.Outcome, DecidedAt: item.Record.Decision.DecidedAt,
		})
	}
	return result
}

func snapshotAttentionControls(items []snapshotFeedbackMaterial) []AttentionControl {
	result := make([]AttentionControl, 0, min(len(items), MaxSignals))
	seen := make(map[string]struct{}, min(len(items), MaxSignals))
	for _, item := range items {
		if _, exists := seen[item.Record.OpenLoopKey]; exists {
			continue
		}
		seen[item.Record.OpenLoopKey] = struct{}{}
		result = append(result, AttentionControl{
			OpenLoopKey: item.Record.OpenLoopKey, SignalDigest: item.Record.SignalDigest,
			Action: item.Record.Action, SnoozedUntil: cloneTimePointer(item.Record.SnoozedUntil),
			RecordedAt: item.Record.RecordedAt,
		})
		if len(result) == MaxSignals {
			break
		}
	}
	return result
}

func firstSignalCursor(items []snapshotSignalMaterial) *SnapshotRecordCursor {
	if len(items) == 0 {
		return nil
	}
	value := items[0].Cursor
	return &value
}

func firstDecisionCursor(items []snapshotDecisionMaterial) *SnapshotRecordCursor {
	if len(items) == 0 {
		return nil
	}
	value := items[0].Cursor
	return &value
}

func firstFeedbackCursor(items []snapshotFeedbackMaterial) *FeedbackSnapshotCursor {
	if len(items) == 0 {
		return nil
	}
	value := items[0].Cursor
	return &value
}

func cloneEvaluationSnapshot(value EvaluationSnapshot) EvaluationSnapshot {
	value.Signals.Cursor = cloneSnapshotRecordCursor(value.Signals.Cursor)
	value.Decisions.Cursor = cloneSnapshotRecordCursor(value.Decisions.Cursor)
	value.Feedback.Cursor = cloneFeedbackSnapshotCursor(value.Feedback.Cursor)
	return value
}

func cloneSnapshotRecordCursor(value *SnapshotRecordCursor) *SnapshotRecordCursor {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFeedbackSnapshotCursor(value *FeedbackSnapshotCursor) *FeedbackSnapshotCursor {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func snapshotRecordCursorPointersEqual(left, right *SnapshotRecordCursor) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftValue := canonicalSnapshotRecordCursor(*left)
	rightValue := canonicalSnapshotRecordCursor(*right)
	return leftValue.RecordedAt.Equal(rightValue.RecordedAt) && leftValue.IdempotencyKey == rightValue.IdempotencyKey && leftValue.Ordinal == rightValue.Ordinal && leftValue.PayloadDigest == rightValue.PayloadDigest
}

func feedbackSnapshotCursorPointersEqual(left, right *FeedbackSnapshotCursor) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftValue := canonicalFeedbackSnapshotCursor(*left)
	rightValue := canonicalFeedbackSnapshotCursor(*right)
	return leftValue.RecordedAt.Equal(rightValue.RecordedAt) && leftValue.FeedbackID == rightValue.FeedbackID && leftValue.IdempotencyKey == rightValue.IdempotencyKey && leftValue.PayloadDigest == rightValue.PayloadDigest && leftValue.RecordDigest == rightValue.RecordDigest
}

func compareSnapshotRecordCursor(left, right SnapshotRecordCursor) int {
	left = canonicalSnapshotRecordCursor(left)
	right = canonicalSnapshotRecordCursor(right)
	if left.RecordedAt.After(right.RecordedAt) {
		return 1
	}
	if left.RecordedAt.Before(right.RecordedAt) {
		return -1
	}
	if left.IdempotencyKey > right.IdempotencyKey {
		return 1
	}
	if left.IdempotencyKey < right.IdempotencyKey {
		return -1
	}
	if left.Ordinal > right.Ordinal {
		return 1
	}
	if left.Ordinal < right.Ordinal {
		return -1
	}
	return 0
}

func compareFeedbackSnapshotCursor(left, right FeedbackSnapshotCursor) int {
	left = canonicalFeedbackSnapshotCursor(left)
	right = canonicalFeedbackSnapshotCursor(right)
	if left.RecordedAt.After(right.RecordedAt) {
		return 1
	}
	if left.RecordedAt.Before(right.RecordedAt) {
		return -1
	}
	if left.FeedbackID > right.FeedbackID {
		return 1
	}
	if left.FeedbackID < right.FeedbackID {
		return -1
	}
	return 0
}

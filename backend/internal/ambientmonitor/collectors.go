package ambientmonitor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

const (
	workflowOpenLoopLimit = 256
	sourceSnapshotLimit   = 256
)

const workflowOpenLoopCountSQL = `
SELECT COUNT(*)::bigint
FROM workflow_open_loops
JOIN workflow_items ON workflow_items.id = workflow_open_loops.workflow_id
WHERE workflow_items.owner_identity = ?
  AND workflow_open_loops.status = 'open'
  AND (workflow_open_loops.follow_up_at IS NULL OR workflow_open_loops.follow_up_at <= ?)
  AND workflow_items.archived = FALSE
  AND workflow_items.current_state NOT IN ('archived', 'completed')`

const workflowOpenLoopSnapshotSQL = `
SELECT
    workflow_open_loops.id::text AS record_id,
    workflow_open_loops.workflow_id::text AS parent_id,
    workflow_open_loops.status AS state,
    '' AS record_digest,
    0::bigint AS revision,
    workflow_open_loops.follow_up_at AS due_at,
    workflow_open_loops.updated_at AS source_at
FROM workflow_open_loops
JOIN workflow_items ON workflow_items.id = workflow_open_loops.workflow_id
WHERE workflow_items.owner_identity = ?
  AND workflow_open_loops.status = 'open'
  AND (workflow_open_loops.follow_up_at IS NULL OR workflow_open_loops.follow_up_at <= ?)
  AND workflow_items.archived = FALSE
  AND workflow_items.current_state NOT IN ('archived', 'completed')
ORDER BY workflow_open_loops.follow_up_at ASC NULLS LAST,
         workflow_open_loops.updated_at DESC,
         workflow_open_loops.id ASC
LIMIT 256`

const workflowVerifiedCompletionCountSQL = `
SELECT COUNT(*)::bigint
FROM workflow_completion_attestations
WHERE owner_identity = ?
  AND completion_status = 'completed'
  AND verification_status IN ('verified', 'test_passed')`

const workflowVerifiedCompletionSnapshotSQL = `
SELECT
    id::text AS record_id,
    workflow_id::text AS parent_id,
    verification_status AS state,
    record_digest,
    0::bigint AS revision,
    NULL::timestamptz AS due_at,
    completed_at AS source_at
FROM workflow_completion_attestations
WHERE owner_identity = ?
  AND completion_status = 'completed'
  AND verification_status IN ('verified', 'test_passed')
ORDER BY completed_at DESC, id ASC
LIMIT 256`

const overdueCommitmentCountSQL = `
WITH latest AS (
    SELECT DISTINCT ON (commitment_key)
        commitment_key,
        revision,
        payload
    FROM life_ledger_commitment_revisions
    WHERE owner_identity = ?
    ORDER BY commitment_key, revision DESC
)
SELECT COUNT(*)::bigint
FROM latest
WHERE payload ->> 'dueAt' IS NOT NULL
  AND (payload ->> 'dueAt')::timestamptz < ?
  AND payload ->> 'status' IN ('proposed', 'active', 'waiting', 'breached', 'disputed')`

const overdueCommitmentSnapshotSQL = `
WITH latest AS (
    SELECT DISTINCT ON (commitment_key)
        commitment_key,
        revision,
        record_digest,
        payload,
        recorded_at
    FROM life_ledger_commitment_revisions
    WHERE owner_identity = ?
    ORDER BY commitment_key, revision DESC
)
SELECT
    commitment_key AS record_id,
    COALESCE(payload ->> 'projectKey', '') AS parent_id,
    payload ->> 'status' AS state,
    record_digest,
    revision,
    (payload ->> 'dueAt')::timestamptz AS due_at,
    recorded_at AS source_at
FROM latest
WHERE payload ->> 'dueAt' IS NOT NULL
  AND (payload ->> 'dueAt')::timestamptz < ?
  AND payload ->> 'status' IN ('proposed', 'active', 'waiting', 'breached', 'disputed')
ORDER BY (payload ->> 'dueAt')::timestamptz ASC, commitment_key ASC
LIMIT 256`

type sourceSnapshotRecord struct {
	RecordID     string     `json:"recordId"`
	ParentID     string     `json:"parentId,omitempty"`
	State        string     `json:"state"`
	RecordDigest string     `json:"recordDigest,omitempty"`
	Revision     int64      `json:"revision,omitempty"`
	DueAt        *time.Time `json:"dueAt,omitempty"`
	SourceAt     time.Time  `json:"sourceAt"`
}

type sourceSnapshot struct {
	Count   int64                  `json:"count"`
	Records []sourceSnapshotRecord `json:"records"`
}

type collectorSourceReader interface {
	workflowOpenLoops(context.Context, string, time.Time) (sourceSnapshot, error)
	workflowVerifiedCompletions(context.Context, string) (sourceSnapshot, error)
	overdueCommitments(context.Context, string, time.Time) (sourceSnapshot, error)
}

type gormCollectorSourceReader struct{ db *gorm.DB }

// GormCollector reads three fixed, owner-scoped operational projections. It
// accepts no SQL, expressions, URLs, or source configuration from a target.
type GormCollector struct {
	reader collectorSourceReader
	clock  func() time.Time
}

var _ Collector = (*GormCollector)(nil)

// NewGormCollector creates a read-only collector over the canonical Postgres
// ledgers. A clock may be supplied for deterministic scheduling tests.
func NewGormCollector(db *gorm.DB, clocks ...func() time.Time) (*GormCollector, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: collector database is required", ErrCollectorUnavailable)
	}
	clock := time.Now
	if len(clocks) > 1 || (len(clocks) == 1 && clocks[0] == nil) {
		return nil, fmt.Errorf("%w: exactly one valid collector clock may be supplied", ErrInvalidInput)
	}
	if len(clocks) == 1 {
		clock = clocks[0]
	}
	return &GormCollector{reader: &gormCollectorSourceReader{db: db}, clock: clock}, nil
}

func DefaultCollector() (Collector, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open ambient monitor collector database: %w", err)
	}
	return NewGormCollector(db)
}

func newCollectorWithReader(reader collectorSourceReader, clock func() time.Time) *GormCollector {
	return &GormCollector{reader: reader, clock: clock}
}

func (collector *GormCollector) Collect(ctx context.Context, target MonitorTarget) (CollectedObservation, error) {
	if err := checkContext(ctx); err != nil {
		return CollectedObservation{}, err
	}
	if collector == nil || collector.reader == nil || collector.clock == nil {
		return CollectedObservation{}, ErrCollectorUnavailable
	}
	scope, err := validateScope(target.Scope)
	if err != nil {
		return CollectedObservation{}, err
	}
	if err := validateSourceKind(target.SourceKind); err != nil {
		return CollectedObservation{}, err
	}
	observedAt, err := validateTime("collector observation time", collector.clock())
	if err != nil {
		return CollectedObservation{}, err
	}

	var snapshot sourceSnapshot
	switch target.SourceKind {
	case SourceWorkflowOpenLoopCount:
		snapshot, err = collector.reader.workflowOpenLoops(ctx, scope.OwnerID, observedAt)
	case SourceWorkflowVerifiedCompletionCount:
		snapshot, err = collector.reader.workflowVerifiedCompletions(ctx, scope.OwnerID)
	case SourceOverdueCommitmentCount:
		snapshot, err = collector.reader.overdueCommitments(ctx, scope.OwnerID, observedAt)
	}
	if err != nil {
		return CollectedObservation{}, fmt.Errorf("%w: %s", ErrCollectorFailed, sanitizedCollectorError(err))
	}
	normalizeSnapshotRecords(snapshot.Records)
	if err := validateSourceSnapshot(target.SourceKind, observedAt, snapshot); err != nil {
		return CollectedObservation{}, err
	}
	digest, err := sourceSnapshotDigest(scope, target.SourceKind, observedAt, snapshot)
	if err != nil {
		return CollectedObservation{}, fmt.Errorf("%w: encode source snapshot", ErrCollectorFailed)
	}
	return CollectedObservation{
		Value: float64(snapshot.Count), ObservedAt: observedAt, SourceDigest: digest,
	}, nil
}

func (reader *gormCollectorSourceReader) workflowOpenLoops(ctx context.Context, owner string, now time.Time) (sourceSnapshot, error) {
	if reader == nil || reader.db == nil {
		return sourceSnapshot{}, ErrCollectorUnavailable
	}
	return reader.readSnapshot(ctx, workflowOpenLoopLimit,
		workflowOpenLoopCountSQL, []any{owner, now},
		workflowOpenLoopSnapshotSQL, []any{owner, now},
	)
}

func (reader *gormCollectorSourceReader) workflowVerifiedCompletions(ctx context.Context, owner string) (sourceSnapshot, error) {
	if reader == nil || reader.db == nil {
		return sourceSnapshot{}, ErrCollectorUnavailable
	}
	return reader.readSnapshot(ctx, sourceSnapshotLimit,
		workflowVerifiedCompletionCountSQL, []any{owner},
		workflowVerifiedCompletionSnapshotSQL, []any{owner},
	)
}

func (reader *gormCollectorSourceReader) overdueCommitments(ctx context.Context, owner string, now time.Time) (sourceSnapshot, error) {
	if reader == nil || reader.db == nil {
		return sourceSnapshot{}, ErrCollectorUnavailable
	}
	return reader.readSnapshot(ctx, sourceSnapshotLimit,
		overdueCommitmentCountSQL, []any{owner, now},
		overdueCommitmentSnapshotSQL, []any{owner, now},
	)
}

func (reader *gormCollectorSourceReader) readSnapshot(
	ctx context.Context,
	capacity int,
	countSQL string,
	countArgs []any,
	recordsSQL string,
	recordsArgs []any,
) (sourceSnapshot, error) {
	var snapshot sourceSnapshot
	err := withCollectorSnapshot(ctx, reader.db, func(tx *gorm.DB) error {
		if err := tx.Raw(countSQL, countArgs...).Scan(&snapshot.Count).Error; err != nil {
			return err
		}
		snapshot.Records = make([]sourceSnapshotRecord, 0, capacity)
		return tx.Raw(recordsSQL, recordsArgs...).Scan(&snapshot.Records).Error
	})
	if err != nil {
		return sourceSnapshot{}, err
	}
	normalizeSnapshotRecords(snapshot.Records)
	return snapshot, nil
}

func withCollectorSnapshot(ctx context.Context, db *gorm.DB, read func(*gorm.DB) error) error {
	if db == nil || read == nil {
		return ErrCollectorUnavailable
	}
	return db.WithContext(ctx).Transaction(read, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
}

func validateSourceSnapshot(kind SourceKind, observedAt time.Time, snapshot sourceSnapshot) error {
	if snapshot.Count < 0 || snapshot.Count > int64(maxCountValue) || snapshot.Count < int64(len(snapshot.Records)) {
		return fmt.Errorf("%w: collector returned an invalid count", ErrCollectorFailed)
	}
	limit := sourceSnapshotLimit
	if kind == SourceWorkflowOpenLoopCount {
		limit = workflowOpenLoopLimit
	}
	if len(snapshot.Records) > limit {
		return fmt.Errorf("%w: collector snapshot exceeds its fixed limit", ErrCollectorFailed)
	}
	for _, record := range snapshot.Records {
		if strings.TrimSpace(record.RecordID) == "" || strings.TrimSpace(record.State) == "" || record.SourceAt.IsZero() {
			return fmt.Errorf("%w: collector snapshot contains an incomplete record", ErrCollectorFailed)
		}
		switch kind {
		case SourceWorkflowOpenLoopCount:
			if record.ParentID == "" || record.State != "open" || (record.DueAt != nil && record.DueAt.After(observedAt)) {
				return fmt.Errorf("%w: open-loop snapshot violates dashboard semantics", ErrCollectorFailed)
			}
		case SourceWorkflowVerifiedCompletionCount:
			if record.ParentID == "" || (record.State != "verified" && record.State != "test_passed") || !digestPattern.MatchString(record.RecordDigest) {
				return fmt.Errorf("%w: completion snapshot is not immutably verified", ErrCollectorFailed)
			}
		case SourceOverdueCommitmentCount:
			if record.DueAt == nil || !record.DueAt.Before(observedAt) || !digestPattern.MatchString(record.RecordDigest) || !overdueCommitmentState(record.State) {
				return fmt.Errorf("%w: commitment snapshot is not overdue and active", ErrCollectorFailed)
			}
		}
	}
	return nil
}

func overdueCommitmentState(state string) bool {
	switch state {
	case "proposed", "active", "waiting", "breached", "disputed":
		return true
	default:
		return false
	}
}

func normalizeSnapshotRecords(records []sourceSnapshotRecord) {
	for index := range records {
		records[index].RecordID = strings.TrimSpace(records[index].RecordID)
		records[index].ParentID = strings.TrimSpace(records[index].ParentID)
		records[index].State = strings.TrimSpace(records[index].State)
		records[index].RecordDigest = strings.ToLower(strings.TrimSpace(records[index].RecordDigest))
		records[index].SourceAt = records[index].SourceAt.UTC().Truncate(time.Microsecond)
		if records[index].DueAt != nil {
			due := records[index].DueAt.UTC().Truncate(time.Microsecond)
			records[index].DueAt = &due
		}
	}
}

func sourceSnapshotDigest(scope Scope, kind SourceKind, observedAt time.Time, snapshot sourceSnapshot) (string, error) {
	payload := struct {
		ContractVersion int                    `json:"contractVersion"`
		OwnerID         string                 `json:"ownerId"`
		WorkspaceID     string                 `json:"workspaceId"`
		SourceKind      SourceKind             `json:"sourceKind"`
		ObservedAt      string                 `json:"observedAt"`
		Count           int64                  `json:"count"`
		Records         []sourceSnapshotRecord `json:"records"`
	}{
		ContractVersion: ContractVersion,
		OwnerID:         scope.OwnerID, WorkspaceID: scope.WorkspaceID, SourceKind: kind,
		ObservedAt: observedAt.UTC().Format(time.RFC3339Nano), Count: snapshot.Count, Records: snapshot.Records,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sanitizedCollectorError(err error) string {
	if err == nil {
		return "source query failed"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" || len(message) > 160 || containsSecret(message) {
		return "source query failed"
	}
	return message
}

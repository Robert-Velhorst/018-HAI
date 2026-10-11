package ambientmonitor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const postgresCompositionSelect = `
	SELECT delivery_id::text AS delivery_id, owner_identity, workspace_key,
		target_id::text AS target_id, run_id::text AS run_id, run_digest,
		observation_id::text AS observation_id, observation_digest, status,
		revision, attempt_count, max_attempts, next_attempt_at, lease_generation,
		lease_id::text AS lease_id, lease_owner, lease_until, last_attempt_at,
		last_failure_code, created_at, updated_at, completed_at, binding_digest,
		snapshot_status, composer_version, snapshot_captured_at, outcome_revision,
		outcome_audit_digest, attention_snapshot::text AS attention_snapshot, snapshot_digest,
		authority, can_execute, delivery_authorized, execution_authorized
	FROM public.outcome_monitor_composition_deliveries `

type postgresCompositionRow struct {
	DeliveryID, OwnerIdentity, WorkspaceKey, TargetID, RunID string
	RunDigest, ObservationID, ObservationDigest, Status      string
	Revision, LeaseGeneration                                int64
	AttemptCount, MaxAttempts                                int
	NextAttemptAt, CreatedAt, UpdatedAt                      time.Time
	LeaseID, LeaseOwner, LastFailureCode                     sql.NullString
	LeaseUntil, LastAttemptAt, CompletedAt                   sql.NullTime
	SnapshotStatus, ComposerVersion, SnapshotDigest          string
	SnapshotCapturedAt                                       time.Time
	OutcomeRevision                                          sql.NullInt64
	OutcomeAuditDigest, AttentionSnapshot                    sql.NullString
	BindingDigest, Authority                                 string
	CanExecute, DeliveryAuthorized, ExecutionAuthorized      bool
}

func (r *PostgresRepository) GetCompositionByRun(ctx context.Context, owner, workspace, runID string) (CompositionDelivery, error) {
	if err := r.ready(ctx); err != nil {
		return CompositionDelivery{}, err
	}
	if err := validateRepositoryScope(owner, workspace); err != nil {
		return CompositionDelivery{}, err
	}
	runUUID, err := postgresRecordUUID(runID, "run")
	if err != nil {
		return CompositionDelivery{}, err
	}
	var row postgresCompositionRow
	if err := r.DB.WithContext(ctx).Raw(postgresCompositionSelect+`WHERE owner_identity = ? AND workspace_key = ? AND run_id = ?`, owner, workspace, runUUID).Scan(&row).Error; err != nil {
		return CompositionDelivery{}, fmt.Errorf("load ambient composition: %w", err)
	}
	if row.DeliveryID == "" {
		return CompositionDelivery{}, ErrNotFound
	}
	return decodePostgresComposition(row, owner, workspace)
}

func (r *PostgresRepository) GetComposition(ctx context.Context, owner, workspace, deliveryID string) (CompositionDelivery, error) {
	if err := r.ready(ctx); err != nil {
		return CompositionDelivery{}, err
	}
	if err := validateRepositoryScope(owner, workspace); err != nil {
		return CompositionDelivery{}, err
	}
	deliveryUUID, err := postgresRecordUUID(deliveryID, "cmp")
	if err != nil {
		return CompositionDelivery{}, err
	}
	var row postgresCompositionRow
	if err := r.DB.WithContext(ctx).Raw(postgresCompositionSelect+`WHERE owner_identity = ? AND workspace_key = ? AND delivery_id = ?`, owner, workspace, deliveryUUID).Scan(&row).Error; err != nil {
		return CompositionDelivery{}, err
	}
	if row.DeliveryID == "" {
		return CompositionDelivery{}, ErrNotFound
	}
	return decodePostgresComposition(row, owner, workspace)
}

func (r *PostgresRepository) LoadCompositionSignal(ctx context.Context, owner, workspace, deliveryID string) (AdvisorySignal, error) {
	if err := r.ready(ctx); err != nil {
		return AdvisorySignal{}, err
	}
	deliveryUUID, err := postgresRecordUUID(deliveryID, "cmp")
	if err != nil {
		return AdvisorySignal{}, err
	}
	var row postgresCompositionRow
	if err := r.DB.WithContext(ctx).Raw(postgresCompositionSelect+`WHERE owner_identity = ? AND workspace_key = ? AND delivery_id = ?`, owner, workspace, deliveryUUID).Scan(&row).Error; err != nil {
		return AdvisorySignal{}, fmt.Errorf("load ambient composition source: %w", err)
	}
	if row.DeliveryID == "" {
		return AdvisorySignal{}, ErrNotFound
	}
	delivery, err := decodePostgresComposition(row, owner, workspace)
	if err != nil {
		return AdvisorySignal{}, err
	}
	var runRow postgresRunRow
	if err := r.DB.WithContext(ctx).Raw(postgresRunSelect+`WHERE r.owner_identity = ? AND r.workspace_key = ? AND r.run_id = ?`, owner, workspace, row.RunID).Scan(&runRow).Error; err != nil {
		return AdvisorySignal{}, fmt.Errorf("load ambient composition run: %w", err)
	}
	run, err := decodePostgresRun(runRow, owner, workspace, delivery.TargetID)
	if err != nil {
		return AdvisorySignal{}, err
	}
	var observationRow postgresObservationRow
	if err := r.DB.WithContext(ctx).Raw(postgresObservationSelect+`WHERE owner_identity = ? AND workspace_key = ? AND observation_id = ?`, owner, workspace, row.ObservationID).Scan(&observationRow).Error; err != nil {
		return AdvisorySignal{}, fmt.Errorf("load ambient composition observation: %w", err)
	}
	observation, err := decodePostgresObservation(observationRow, owner, workspace, delivery.TargetID)
	if err != nil {
		return AdvisorySignal{}, err
	}
	if run.ID != delivery.RunID || run.RecordDigest != delivery.RunDigest || observation.ID != delivery.ObservationID || observation.RecordDigest != delivery.ObservationDigest {
		return AdvisorySignal{}, ErrCorruptStorage
	}
	return AdvisorySignal{Observation: observation, Run: run, Snapshot: delivery.Snapshot, Authority: advisoryAuthority()}, nil
}

func (r *PostgresRepository) ListPendingCompositionScopes(ctx context.Context, now time.Time, limit int) ([]Scope, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	var err error
	if now, err = validateTime("composition scope time", now); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxClaimLimit {
		return nil, fmt.Errorf("%w: composition scope limit is invalid", ErrInvalidInput)
	}
	var rows []struct{ OwnerIdentity, WorkspaceKey string }
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, workspace_key
		FROM public.outcome_monitor_composition_deliveries
		WHERE status = 'pending' AND next_attempt_at <= ?
			AND (lease_until IS NULL OR lease_until <= ?)
		GROUP BY owner_identity, workspace_key
		ORDER BY owner_identity, workspace_key LIMIT ?`, now.UTC(), now.UTC(), limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list ambient composition scopes: %w", err)
	}
	items := make([]Scope, 0, len(rows))
	for _, row := range rows {
		scope, err := validateScope(Scope{OwnerID: row.OwnerIdentity, WorkspaceID: row.WorkspaceKey})
		if err != nil {
			return nil, ErrCorruptStorage
		}
		items = append(items, scope)
	}
	return items, nil
}

func (r *PostgresRepository) ClaimDueCompositions(ctx context.Context, owner, workspace, worker string, now time.Time, leaseDuration time.Duration, limit int) ([]CompositionDelivery, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	if err := validateRepositoryScope(owner, workspace); err != nil {
		return nil, err
	}
	if err := validatePostgresWorker(worker); err != nil {
		return nil, err
	}
	var err error
	if now, err = validateTime("composition claim time", now); err != nil {
		return nil, err
	}
	if err := validateLeaseDuration(leaseDuration); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxClaimLimit {
		return nil, fmt.Errorf("%w: composition claim limit is invalid", ErrInvalidInput)
	}
	claimed := make([]CompositionDelivery, 0, limit)
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []postgresCompositionRow
		if err := tx.Raw(postgresCompositionSelect+`WHERE owner_identity = ? AND workspace_key = ? AND status = 'pending' AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?) ORDER BY next_attempt_at, delivery_id FOR UPDATE SKIP LOCKED LIMIT ?`, owner, workspace, now.UTC(), now.UTC(), limit).Scan(&rows).Error; err != nil {
			return fmt.Errorf("lock ambient compositions: %w", err)
		}
		for _, row := range rows {
			claimAt := now
			if !claimAt.After(row.UpdatedAt) {
				claimAt = row.UpdatedAt.Add(time.Microsecond)
			}
			generation := uint64(row.LeaseGeneration + 1)
			claimID := postgresCompositionClaimID(owner, workspace, row.DeliveryID, worker, generation)
			result := tx.Exec(`UPDATE public.outcome_monitor_composition_deliveries SET revision=revision+1, lease_generation=?, lease_id=?, lease_owner=?, lease_until=?, updated_at=? WHERE owner_identity=? AND workspace_key=? AND delivery_id=? AND revision=? AND status='pending' AND (lease_until IS NULL OR lease_until<=?)`, generation, claimID, worker, claimAt.Add(leaseDuration), claimAt, owner, workspace, row.DeliveryID, row.Revision, now.UTC())
			if result.Error != nil {
				return mapPostgresMonitorError("claim ambient composition", result.Error)
			}
			if result.RowsAffected != 1 {
				continue
			}
			var updated postgresCompositionRow
			if err := tx.Raw(postgresCompositionSelect+`WHERE owner_identity=? AND workspace_key=? AND delivery_id=?`, owner, workspace, row.DeliveryID).Scan(&updated).Error; err != nil {
				return err
			}
			item, err := decodePostgresComposition(updated, owner, workspace)
			if err != nil {
				return err
			}
			claimed = append(claimed, item)
		}
		return nil
	})
	return claimed, err
}

func (r *PostgresRepository) CompleteComposition(ctx context.Context, owner, workspace, deliveryID, worker string, generation uint64, attempt CompositionAttempt, completedAt time.Time) (CompositionDelivery, CompositionAttempt, error) {
	return r.finishPostgresComposition(ctx, owner, workspace, deliveryID, worker, generation, attempt, completedAt, time.Time{}, false)
}

func (r *PostgresRepository) FailComposition(ctx context.Context, owner, workspace, deliveryID, worker string, generation uint64, attempt CompositionAttempt, next time.Time, deadLetter bool) (CompositionDelivery, CompositionAttempt, error) {
	return r.finishPostgresComposition(ctx, owner, workspace, deliveryID, worker, generation, attempt, attempt.FinishedAt, next, deadLetter)
}

func (r *PostgresRepository) finishPostgresComposition(ctx context.Context, owner, workspace, deliveryID, worker string, generation uint64, attempt CompositionAttempt, at, next time.Time, deadLetter bool) (CompositionDelivery, CompositionAttempt, error) {
	if err := r.ready(ctx); err != nil {
		return CompositionDelivery{}, CompositionAttempt{}, err
	}
	deliveryUUID, err := postgresRecordUUID(deliveryID, "cmp")
	if err != nil {
		return CompositionDelivery{}, CompositionAttempt{}, err
	}
	attemptUUID, err := postgresRecordUUID(attempt.ID, "cat")
	if err != nil {
		return CompositionDelivery{}, CompositionAttempt{}, err
	}
	cleanAttempt, err := validateCompositionAttempt(attempt)
	if err != nil {
		return CompositionDelivery{}, CompositionAttempt{}, err
	}
	var stored CompositionDelivery
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row postgresCompositionRow
		if err := tx.Raw(postgresCompositionSelect+`WHERE owner_identity=? AND workspace_key=? AND delivery_id=? FOR UPDATE`, owner, workspace, deliveryUUID).Scan(&row).Error; err != nil {
			return err
		}
		if row.DeliveryID == "" {
			return ErrNotFound
		}
		delivery, err := decodePostgresComposition(row, owner, workspace)
		if err != nil {
			return err
		}
		if delivery.Status != CompositionPending || !delivery.Lease.Active() || delivery.Lease.WorkerID != worker || delivery.Lease.Generation != generation || at.After(delivery.Lease.ExpiresAt) {
			return ErrLeaseLost
		}
		if cleanAttempt.Scope != delivery.Scope || cleanAttempt.DeliveryID != delivery.ID || cleanAttempt.TargetID != delivery.TargetID || cleanAttempt.RunID != delivery.RunID || cleanAttempt.RunDigest != delivery.RunDigest || cleanAttempt.SnapshotDigest != delivery.Snapshot.SnapshotDigest || cleanAttempt.WorkerID != worker || cleanAttempt.LeaseGeneration != generation || cleanAttempt.AttemptNumber != delivery.AttemptCount+1 || !cleanAttempt.StartedAt.Equal(delivery.Lease.ClaimedAt) || !cleanAttempt.FinishedAt.Equal(at) {
			return fmt.Errorf("%w: composition attempt binding is invalid", ErrInvalidInput)
		}
		claimID := postgresCompositionClaimID(owner, workspace, row.DeliveryID, worker, generation)
		var failure any
		if cleanAttempt.FailureCode != "" {
			failure = cleanAttempt.FailureCode
		}
		result := tx.Exec(`INSERT INTO public.outcome_monitor_composition_attempts (attempt_id,delivery_id,owner_identity,workspace_key,target_id,run_id,run_digest,snapshot_digest,attempt_number,claim_id,lease_generation,worker_id,status,failure_code,started_at,finished_at,request_digest,record_digest,authority,can_execute,delivery_authorized,execution_authorized) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,false,false,false)`, attemptUUID, deliveryUUID, owner, workspace, row.TargetID, row.RunID, cleanAttempt.RunDigest, cleanAttempt.SnapshotDigest, cleanAttempt.AttemptNumber, claimID, generation, worker, string(cleanAttempt.Status), failure, cleanAttempt.StartedAt.UTC(), cleanAttempt.FinishedAt.UTC(), cleanAttempt.RequestDigest, cleanAttempt.RecordDigest, AuthorityLabel)
		if result.Error != nil {
			return mapPostgresMonitorError("append ambient composition attempt", result.Error)
		}
		if cleanAttempt.Status == CompositionAttemptSucceeded {
			result = tx.Exec(`UPDATE public.outcome_monitor_composition_deliveries SET status='succeeded',revision=revision+1,attempt_count=attempt_count+1,lease_id=NULL,lease_owner=NULL,lease_until=NULL,last_attempt_at=?,last_failure_code=NULL,updated_at=?,completed_at=? WHERE owner_identity=? AND workspace_key=? AND delivery_id=? AND revision=? AND lease_id=?`, at.UTC(), at.UTC(), at.UTC(), owner, workspace, deliveryUUID, row.Revision, claimID)
		} else {
			status := "pending"
			var completed any
			if deadLetter || delivery.AttemptCount+1 >= delivery.MaxAttempts {
				status = "dead_lettered"
				completed = at.UTC()
			} else {
				if next.IsZero() || !next.After(at) {
					return fmt.Errorf("%w: composition retry time is invalid", ErrInvalidInput)
				}
			}
			result = tx.Exec(`UPDATE public.outcome_monitor_composition_deliveries SET status=?,revision=revision+1,attempt_count=attempt_count+1,next_attempt_at=?,lease_id=NULL,lease_owner=NULL,lease_until=NULL,last_attempt_at=?,last_failure_code=?,updated_at=?,completed_at=? WHERE owner_identity=? AND workspace_key=? AND delivery_id=? AND revision=? AND lease_id=?`, status, next.UTC(), at.UTC(), cleanAttempt.FailureCode, at.UTC(), completed, owner, workspace, deliveryUUID, row.Revision, claimID)
		}
		if result.Error != nil {
			return mapPostgresMonitorError("finish ambient composition", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrLeaseLost
		}
		var updated postgresCompositionRow
		if err := tx.Raw(postgresCompositionSelect+`WHERE owner_identity=? AND workspace_key=? AND delivery_id=?`, owner, workspace, deliveryUUID).Scan(&updated).Error; err != nil {
			return err
		}
		stored, err = decodePostgresComposition(updated, owner, workspace)
		return err
	})
	return stored, cleanAttempt, err
}

func (r *PostgresRepository) RecoverExpiredCompositionLeases(ctx context.Context, owner, workspace string, now time.Time) (int, error) {
	if err := r.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateRepositoryScope(owner, workspace); err != nil {
		return 0, err
	}
	var err error
	if now, err = validateTime("composition recovery time", now); err != nil {
		return 0, err
	}
	result := r.DB.WithContext(ctx).Exec(`UPDATE public.outcome_monitor_composition_deliveries SET revision=revision+1,lease_id=NULL,lease_owner=NULL,lease_until=NULL,updated_at=GREATEST(?,updated_at+interval '1 microsecond') WHERE owner_identity=? AND workspace_key=? AND status='pending' AND lease_id IS NOT NULL AND lease_until<=?`, now.UTC(), owner, workspace, now.UTC())
	if result.Error != nil {
		return 0, mapPostgresMonitorError("recover ambient composition leases", result.Error)
	}
	return int(result.RowsAffected), nil
}

func (r *PostgresRepository) ListCompositions(ctx context.Context, owner, workspace, targetID string, limit int) ([]CompositionDelivery, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	targetUUID, err := postgresTargetUUID(targetID)
	if err != nil {
		return nil, err
	}
	var rows []postgresCompositionRow
	if err := r.DB.WithContext(ctx).Raw(postgresCompositionSelect+`WHERE owner_identity=? AND workspace_key=? AND target_id=? ORDER BY created_at DESC,delivery_id DESC LIMIT ?`, owner, workspace, targetUUID, boundedHistoryLimit(limit)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]CompositionDelivery, 0, len(rows))
	for _, row := range rows {
		item, err := decodePostgresComposition(row, owner, workspace)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *PostgresRepository) ListCompositionAttempts(ctx context.Context, owner, workspace, deliveryID string, limit int) ([]CompositionAttempt, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	deliveryUUID, err := postgresRecordUUID(deliveryID, "cmp")
	if err != nil {
		return nil, err
	}
	var rows []postgresCompositionAttemptRow
	if err := r.DB.WithContext(ctx).Raw(postgresCompositionAttemptSelect+`WHERE owner_identity=? AND workspace_key=? AND delivery_id=? ORDER BY attempt_number DESC LIMIT ?`, owner, workspace, deliveryUUID, boundedHistoryLimit(limit)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]CompositionAttempt, 0, len(rows))
	for _, row := range rows {
		item, err := decodePostgresCompositionAttempt(row, owner, workspace)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

const postgresCompositionAttemptSelect = `SELECT attempt_id::text AS attempt_id,delivery_id::text AS delivery_id,owner_identity,workspace_key,target_id::text AS target_id,run_id::text AS run_id,run_digest,snapshot_digest,attempt_number,lease_generation,worker_id,status,failure_code,started_at,finished_at,request_digest,record_digest,authority,can_execute,delivery_authorized,execution_authorized FROM public.outcome_monitor_composition_attempts `

type postgresCompositionAttemptRow struct {
	AttemptID, DeliveryID, OwnerIdentity, WorkspaceKey, TargetID, RunID, RunDigest, SnapshotDigest string
	AttemptNumber                                                                                  int
	LeaseGeneration                                                                                int64
	WorkerID, Status                                                                               string
	FailureCode                                                                                    sql.NullString
	StartedAt, FinishedAt                                                                          time.Time
	RequestDigest, RecordDigest, Authority                                                         string
	CanExecute, DeliveryAuthorized, ExecutionAuthorized                                            bool
}

type postgresCompositionSnapshotInsert struct {
	OutcomeRevision, OutcomeAuditDigest          any
	PolicyKey, PolicyDigest, PolicyAt            any
	SignalAt, SignalKey, DecisionAt, DecisionKey any
	FeedbackAt, FeedbackID, FeedbackDigest       any
	AttentionJSON, AttentionDigest               any
}

func postgresCompositionSnapshotColumns(snapshot CompositionSnapshot) (postgresCompositionSnapshotInsert, error) {
	clean, err := validateCompositionSnapshot(snapshot)
	if err != nil {
		return postgresCompositionSnapshotInsert{}, err
	}
	if clean.Status == CompositionSnapshotLegacyUnpinned {
		return postgresCompositionSnapshotInsert{}, nil
	}
	payload, err := json.Marshal(clean.Attention)
	if err != nil {
		return postgresCompositionSnapshotInsert{}, fmt.Errorf("marshal composition attention snapshot: %w", err)
	}
	result := postgresCompositionSnapshotInsert{
		OutcomeRevision: clean.OutcomeRevision, OutcomeAuditDigest: clean.OutcomeAuditDigest,
		PolicyKey: clean.Attention.Policy.IdempotencyKey, PolicyDigest: clean.Attention.Policy.PayloadDigest,
		PolicyAt: clean.Attention.Policy.RecordedAt.UTC(), AttentionJSON: string(payload),
		AttentionDigest: clean.Attention.InputDigest,
	}
	if cursor := clean.Attention.Signals.Cursor; cursor != nil {
		result.SignalAt = cursor.RecordedAt.UTC()
		result.SignalKey, err = postgresSnapshotCursorKey(cursor.IdempotencyKey, cursor.Ordinal)
		if err != nil {
			return postgresCompositionSnapshotInsert{}, err
		}
	}
	if cursor := clean.Attention.Decisions.Cursor; cursor != nil {
		result.DecisionAt = cursor.RecordedAt.UTC()
		result.DecisionKey, err = postgresSnapshotCursorKey(cursor.IdempotencyKey, cursor.Ordinal)
		if err != nil {
			return postgresCompositionSnapshotInsert{}, err
		}
	}
	if cursor := clean.Attention.Feedback.Cursor; cursor != nil {
		result.FeedbackAt = cursor.RecordedAt.UTC()
		result.FeedbackID = cursor.FeedbackID
		result.FeedbackDigest = cursor.RecordDigest
	}
	return result, nil
}

func postgresSnapshotCursorKey(key string, ordinal int) (string, error) {
	encodedKey, err := json.Marshal(key)
	if err != nil {
		return "", fmt.Errorf("marshal composition snapshot cursor: %w", err)
	}
	// PostgreSQL jsonb_build_array(... )::text places one space after the
	// comma. This text column is compared byte-for-byte by the integrity
	// trigger, so emit the database's canonical representation exactly.
	return fmt.Sprintf("[%s, %d]", encodedKey, ordinal), nil
}

func decodePostgresComposition(row postgresCompositionRow, owner, workspace string) (CompositionDelivery, error) {
	if row.OwnerIdentity != owner || row.WorkspaceKey != workspace || row.CanExecute || row.DeliveryAuthorized || row.ExecutionAuthorized || row.Authority != AuthorityLabel {
		return CompositionDelivery{}, ErrCorruptStorage
	}
	id, err := domainRecordID(row.DeliveryID, "cmp")
	if err != nil {
		return CompositionDelivery{}, err
	}
	runID, err := domainRecordID(row.RunID, "run")
	if err != nil {
		return CompositionDelivery{}, err
	}
	observationID, err := domainRecordID(row.ObservationID, "obs")
	if err != nil {
		return CompositionDelivery{}, err
	}
	snapshot, err := decodePostgresCompositionSnapshot(row, owner)
	if err != nil {
		return CompositionDelivery{}, err
	}
	item := CompositionDelivery{ContractVersion: ContractVersion, ID: id, Scope: Scope{OwnerID: owner, WorkspaceID: workspace}, TargetID: row.TargetID, RunID: runID, RunDigest: row.RunDigest, ObservationID: observationID, ObservationDigest: row.ObservationDigest, Snapshot: snapshot, Status: CompositionStatus(row.Status), Revision: uint64(row.Revision), AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts, NextAttemptAt: row.NextAttemptAt, Lease: Lease{Generation: uint64(row.LeaseGeneration)}, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, BindingDigest: row.BindingDigest, Authority: advisoryAuthority()}
	if row.LeaseID.Valid {
		item.Lease.WorkerID = row.LeaseOwner.String
		item.Lease.ClaimedAt = row.UpdatedAt
		item.Lease.ExpiresAt = row.LeaseUntil.Time
	}
	if row.LastAttemptAt.Valid {
		item.LastAttemptAt = row.LastAttemptAt.Time
	}
	if row.LastFailureCode.Valid {
		item.LastFailureCode = row.LastFailureCode.String
	}
	if row.CompletedAt.Valid {
		item.CompletedAt = row.CompletedAt.Time
	}
	return validateCompositionDelivery(item)
}

func decodePostgresCompositionSnapshot(row postgresCompositionRow, owner string) (CompositionSnapshot, error) {
	value := CompositionSnapshot{
		ContractVersion: compositionSnapshotVersion,
		Status:          CompositionSnapshotStatus(row.SnapshotStatus), ComposerVersion: row.ComposerVersion,
		CapturedAt: row.SnapshotCapturedAt, SnapshotDigest: row.SnapshotDigest,
	}
	if value.Status == CompositionSnapshotPinned {
		if !row.OutcomeRevision.Valid || !row.OutcomeAuditDigest.Valid || !row.AttentionSnapshot.Valid {
			return CompositionSnapshot{}, ErrCorruptStorage
		}
		value.OutcomeRevision = row.OutcomeRevision.Int64
		value.OutcomeAuditDigest = row.OutcomeAuditDigest.String
		if err := json.Unmarshal([]byte(row.AttentionSnapshot.String), &value.Attention); err != nil {
			return CompositionSnapshot{}, ErrCorruptStorage
		}
		if value.Attention.OwnerIdentity != owner {
			return CompositionSnapshot{}, ErrCorruptStorage
		}
	}
	clean, err := validateCompositionSnapshot(value)
	if err != nil {
		return CompositionSnapshot{}, ErrCorruptStorage
	}
	return clean, nil
}

func decodePostgresCompositionAttempt(row postgresCompositionAttemptRow, owner, workspace string) (CompositionAttempt, error) {
	if row.OwnerIdentity != owner || row.WorkspaceKey != workspace || row.CanExecute || row.DeliveryAuthorized || row.ExecutionAuthorized || row.Authority != AuthorityLabel {
		return CompositionAttempt{}, ErrCorruptStorage
	}
	id, err := domainRecordID(row.AttemptID, "cat")
	if err != nil {
		return CompositionAttempt{}, err
	}
	deliveryID, err := domainRecordID(row.DeliveryID, "cmp")
	if err != nil {
		return CompositionAttempt{}, err
	}
	runID, err := domainRecordID(row.RunID, "run")
	if err != nil {
		return CompositionAttempt{}, err
	}
	item := CompositionAttempt{ContractVersion: ContractVersion, ID: id, Scope: Scope{OwnerID: owner, WorkspaceID: workspace}, DeliveryID: deliveryID, TargetID: row.TargetID, RunID: runID, RunDigest: row.RunDigest, SnapshotDigest: row.SnapshotDigest, AttemptNumber: row.AttemptNumber, LeaseGeneration: uint64(row.LeaseGeneration), WorkerID: row.WorkerID, Status: CompositionAttemptStatus(row.Status), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, RequestDigest: row.RequestDigest, RecordDigest: row.RecordDigest, Authority: advisoryAuthority()}
	if row.FailureCode.Valid {
		item.FailureCode = row.FailureCode.String
	}
	return validateCompositionAttempt(item)
}

func postgresCompositionClaimID(owner, workspace, deliveryID, worker string, generation uint64) uuid.UUID {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d", owner, workspace, deliveryID, worker, generation)))
	var value uuid.UUID
	copy(value[:], sum[:16])
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return value
}

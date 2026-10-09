package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	correctionPhaseIntentPersisted    = "intent_persisted"
	correctionPhaseWorkflowRetracted  = "workflow_retracted"
	correctionPhasePatchApplied       = "patch_applied"
	correctionPhaseWorkflowReconciled = "workflow_reconciled"
	correctionPhaseIndexUpdated       = "index_updated"
	correctionPhaseGraphProjected     = "graph_projected"
	correctionPhaseMemoryRecorded     = "memory_recorded"
	correctionPhaseCompleted          = "completed"
	correctionPhaseConflictReconcile  = "conflict_reconcile"
	correctionPhaseConflict           = "conflict"
	correctionPhaseFailed             = "failed"
)

var (
	errExtractionCorrectionRevisionSuperseded = errors.New("source extraction revision was superseded after correction application")
	errExtractionCorrectionOwnerChanged       = errors.New("source owner changed after correction application")
)

type extractionCorrectionJobPayload struct {
	CorrectionID string `json:"correctionId"`
}

type extractionCorrectionExecutor interface {
	runExtractionCorrection(context.Context, durablejob.Job, uuid.UUID) error
}

type extractionCorrectionWorkerError struct {
	stage     string
	retryable bool
	cause     error
}

func (e *extractionCorrectionWorkerError) Error() string {
	if e == nil || e.stage == "" {
		return "source extraction correction failed"
	}
	if e.retryable {
		return "source extraction correction step failed: " + e.stage
	}
	return "source extraction correction failed permanently: " + e.stage
}

func (e *extractionCorrectionWorkerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *extractionCorrectionWorkerError) Retryable() bool {
	return e != nil && e.retryable
}

func (s *service) SubmitExtractionCorrection(
	ownerIdentity string,
	id uuid.UUID,
	expectedRevision time.Time,
	patch ExtractionPatch,
	idempotencyKey string,
) (*ExtractionCorrectionView, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if ownerIdentity == "" || len(ownerIdentity) > 255 || id == uuid.Nil || expectedRevision.IsZero() || idempotencyKey == "" ||
		!validateCorrectionIdempotencyKey(idempotencyKey) || patch.validate() != nil {
		return nil, ErrInvalidExtractionPatch
	}
	encodedPatch, err := json.Marshal(patch)
	if err != nil {
		return nil, errors.New("could not encode source correction")
	}
	expectedRevision = expectedRevision.UTC()
	requestBytes, err := json.Marshal(struct {
		ExpectedRevision string          `json:"expectedRevision"`
		Patch            json.RawMessage `json:"patch"`
	}{ExpectedRevision: expectedRevision.Format(time.RFC3339Nano), Patch: encodedPatch})
	if err != nil {
		return nil, errors.New("could not encode source correction request")
	}
	requestDigest := sha256.Sum256(requestBytes)
	requestHash := hex.EncodeToString(requestDigest[:])
	repository, ok := s.repo.(extractionCorrectionRepository)
	if !ok {
		return nil, ErrExtractionPatchUnavailable
	}

	keyDigest := sha256.Sum256([]byte(idempotencyKey))
	keyHash := hex.EncodeToString(keyDigest[:])
	if existing, durable, found, lookupErr := repository.FindExtractionCorrectionByIdempotency(ownerIdentity, id, keyHash); lookupErr != nil {
		return nil, errors.New("could not inspect source correction state")
	} else if found {
		if existing.RequestHash != requestHash {
			return nil, ErrExtractionCorrectionIdempotency
		}
		return extractionCorrectionView(existing, durable), nil
	}
	if existing, durable, found, lookupErr := repository.FindActiveExtractionCorrection(ownerIdentity, id, requestHash); lookupErr != nil {
		return nil, errors.New("could not inspect pending source correction")
	} else if found {
		return extractionCorrectionView(existing, durable), nil
	}

	before, err := s.repo.FindMutableExtractionForOwner(id, ownerIdentity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSourceExtractionNotFound
	}
	if err != nil {
		return nil, ErrExtractionPatchUnavailable
	}
	if before.Archived {
		return nil, ErrArchivedExtractionPatch
	}
	if !before.UpdatedAt.Equal(expectedRevision) {
		return nil, ErrExtractionPatchConflict
	}
	if patch.matches(before) {
		prior, priorJob, found, lookupErr := repository.FindLatestAppliedExtractionCorrection(ownerIdentity, id, string(encodedPatch), before.UpdatedAt)
		if lookupErr != nil {
			return nil, errors.New("could not inspect applied source correction recovery state")
		}
		if found && prior.Status == models.SourceExtractionCorrectionCompleted {
			return extractionCorrectionView(prior, priorJob), nil
		}
		if found && prior.Status == models.SourceExtractionCorrectionFailed {
			now := time.Now().UTC()
			appliedRevision := before.UpdatedAt.UTC()
			correctionID, durableID := uuid.New(), uuid.New()
			payload, err := json.Marshal(extractionCorrectionJobPayload{CorrectionID: correctionID.String()})
			if err != nil {
				return nil, errors.New("could not encode source correction recovery job")
			}
			recovery := &models.SourceExtractionCorrection{
				ID: correctionID, OwnerIdentity: ownerIdentity, SourceID: before.SourceID,
				ExtractionID: id, IdempotencyKeyHash: keyHash, RequestHash: requestHash,
				ExpectedRevision: before.UpdatedAt.UTC(), PatchJSON: string(encodedPatch),
				AppliedRevision: &appliedRevision,
				Status:          models.SourceExtractionCorrectionPending, DurableJobID: durableID,
				MaxAttempts: extractionCorrectionMaxAttempts, CreatedAt: now, UpdatedAt: now,
			}
			job := &models.DurableJob{
				ID: durableID, Queue: "source", Kind: JobKindExtractionCorrection, Payload: string(payload),
				Status: models.DurableJobPending, RunAt: now, MaxAttempts: extractionCorrectionMaxAttempts,
				CreatedAt: now, UpdatedAt: now,
			}
			stored, durable, _, createErr := repository.CreateExtractionCorrectionRecoveryIntent(recovery, job, prior.ID)
			if createErr != nil {
				switch {
				case errors.Is(createErr, ErrExtractionCorrectionPersistenceUnknown):
					return nil, &ExtractionCorrectionPersistenceUnknownError{CorrectionID: recovery.ID}
				case errors.Is(createErr, ErrExtractionPatchConflict), errors.Is(createErr, ErrArchivedExtractionPatch):
					return nil, createErr
				case errors.Is(createErr, ErrExtractionCorrectionIdempotency), errors.Is(createErr, ErrExtractionCorrectionActive):
					return nil, createErr
				default:
					return nil, ErrExtractionCorrectionNotSaved
				}
			}
			return extractionCorrectionView(stored, durable), nil
		}
		return &ExtractionCorrectionView{
			ExtractionID: id.String(), Status: models.SourceExtractionCorrectionCompleted,
			Phase: correctionPhaseCompleted, PatchSaved: true, AlreadyApplied: true,
			ExpectedRevision: before.UpdatedAt.UTC(), Message: "The requested values already match the current extraction; no recovery work is required.",
		}, nil
	}
	beforeJSON, err := json.Marshal(extractionCorrectionBefore(before))
	if err != nil {
		return nil, errors.New("could not encode source correction checkpoint")
	}
	now := time.Now().UTC()
	correctionID, durableID := uuid.New(), uuid.New()
	payload, err := json.Marshal(extractionCorrectionJobPayload{CorrectionID: correctionID.String()})
	if err != nil {
		return nil, errors.New("could not encode source correction job")
	}
	correction := &models.SourceExtractionCorrection{
		ID: correctionID, OwnerIdentity: ownerIdentity, SourceID: before.SourceID,
		ExtractionID: id, IdempotencyKeyHash: keyHash, RequestHash: requestHash,
		ExpectedRevision: expectedRevision.UTC(), PatchJSON: string(encodedPatch), BeforeStateJSON: string(beforeJSON),
		RequiresRetraction: patch.removesAction(before), Phase: correctionPhaseIntentPersisted,
		Status: models.SourceExtractionCorrectionPending, DurableJobID: durableID,
		MaxAttempts: extractionCorrectionMaxAttempts, CreatedAt: now, UpdatedAt: now,
	}
	job := &models.DurableJob{
		ID: durableID, Queue: "source", Kind: JobKindExtractionCorrection, Payload: string(payload),
		Status: models.DurableJobPending, RunAt: now, MaxAttempts: extractionCorrectionMaxAttempts,
		CreatedAt: now, UpdatedAt: now,
	}
	stored, durable, _, err := repository.CreateExtractionCorrectionIntent(correction, job)
	if err != nil {
		switch {
		case errors.Is(err, ErrExtractionCorrectionPersistenceUnknown):
			return nil, &ExtractionCorrectionPersistenceUnknownError{CorrectionID: correction.ID}
		case errors.Is(err, ErrExtractionPatchConflict):
			return nil, ErrExtractionPatchConflict
		case errors.Is(err, ErrArchivedExtractionPatch):
			return nil, ErrArchivedExtractionPatch
		case errors.Is(err, ErrExtractionCorrectionIdempotency):
			return nil, ErrExtractionCorrectionIdempotency
		case errors.Is(err, ErrExtractionCorrectionActive):
			return nil, ErrExtractionCorrectionActive
		default:
			return nil, ErrExtractionCorrectionNotSaved
		}
	}
	return extractionCorrectionView(stored, durable), nil
}

func (s *service) ExtractionCorrectionForOwner(ownerIdentity string, id uuid.UUID) (*ExtractionCorrectionView, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || id == uuid.Nil {
		return nil, ErrExtractionCorrectionNotFound
	}
	repository, ok := s.repo.(extractionCorrectionRepository)
	if !ok {
		return nil, ErrExtractionPatchUnavailable
	}
	correction, durable, err := repository.FindExtractionCorrectionForOwner(ownerIdentity, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExtractionCorrectionNotFound
	}
	if err != nil {
		return nil, errors.New("could not load source correction state")
	}
	return extractionCorrectionView(correction, durable), nil
}

func extractionCorrectionView(correction *models.SourceExtractionCorrection, job *models.DurableJob) *ExtractionCorrectionView {
	if correction == nil {
		return nil
	}
	status := correction.Status
	if status == models.SourceExtractionCorrectionPending && job != nil {
		switch job.Status {
		case models.DurableJobRunning:
			status = "running"
		case models.DurableJobDead, models.DurableJobSucceeded:
			status = models.SourceExtractionCorrectionFailed
		}
	}
	patchSaved := correction.AppliedRevision != nil
	view := &ExtractionCorrectionView{
		ID: correction.ID.String(), ExtractionID: correction.ExtractionID.String(), Status: status,
		Phase: correction.Phase, IntentPersisted: true, PatchSaved: patchSaved,
		RecoveryPending:  status == models.SourceExtractionCorrectionPending || status == "running",
		NeedsReview:      status == models.SourceExtractionCorrectionConflict || status == models.SourceExtractionCorrectionFailed,
		ExpectedRevision: correction.ExpectedRevision.UTC(), AppliedRevision: correction.AppliedRevision,
		Attempts: correction.Attempts, MaxAttempts: correction.MaxAttempts,
		ErrorCode: safeExtractionCorrectionErrorCode(correction.ErrorCode),
		CreatedAt: correction.CreatedAt.UTC(), UpdatedAt: correction.UpdatedAt.UTC(), CompletedAt: correction.CompletedAt,
	}
	if job != nil {
		view.Attempts = max(view.Attempts, job.Attempts)
		if job.MaxAttempts > 0 {
			view.MaxAttempts = job.MaxAttempts
		}
	}
	switch status {
	case models.SourceExtractionCorrectionCompleted:
		view.Message = "The correction and its dependent workflow, index, and source projections are complete."
	case models.SourceExtractionCorrectionConflict:
		view.Message = "The source revision no longer matches this correction. HAI stopped dependent recovery; review the current extraction before submitting a new correction."
	case models.SourceExtractionCorrectionFailed:
		view.Message = "The correction could not complete within its retry limit. Review the saved-state indicator and inspect the extraction before retrying."
	case "running":
		view.Message = "The correction is being applied and verified by the background worker."
	default:
		if patchSaved {
			view.Message = "The source patch is saved; dependent workflow, index, and memory recovery is queued."
		} else {
			view.Message = "The correction intent is safely queued; the source patch is not yet confirmed."
		}
	}
	return view
}

func safeExtractionCorrectionErrorCode(code string) string {
	switch code {
	case "revision_conflict", "workflow_retract_failed", "workflow_reconcile_failed", "index_update_failed",
		"life_graph_projection_failed", "correction_memory_failed", "source_owner_changed", "extraction_missing",
		"applied_revision_superseded", "invalid_intent", "retry_limit_reached", "queue_terminal_unreconciled", "phase_invalid":
		return code
	default:
		return ""
	}
}

func (s *service) runExtractionCorrection(ctx context.Context, job durablejob.Job, correctionID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return durablejob.Defer("source correction paused while the worker is stopping")
	}
	repository, ok := s.repo.(extractionCorrectionRepository)
	if !ok {
		return permanentCorrectionError("repository_unavailable", errors.New("repository unavailable"))
	}
	correction, _, err := repository.FindExtractionCorrectionForWorker(correctionID, job.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Deletion may have committed after the queue handed this stale job to
		// the worker. A missing intent is terminal and has no effects to replay.
		return nil
	}
	if err != nil {
		return retryCorrectionError("intent_load", err)
	}
	if correction == nil {
		return retryCorrectionError("intent_load", errors.New("source correction intent is unavailable"))
	}
	if correction.Status != models.SourceExtractionCorrectionPending {
		return nil
	}
	capacity, ok := s.repo.(extractionCorrectionWorkerPoolCapacity)
	if !ok {
		return permanentCorrectionError("worker_pool_capacity_unavailable", errors.New("repository cannot verify source correction worker pool capacity"))
	}
	if err := capacity.RequireExtractionCorrectionWorkerPoolCapacity(); err != nil {
		return permanentCorrectionError("worker_pool_capacity", err)
	}
	lockedSourceID := correction.SourceID
	lockedOwnerIdentity := correction.OwnerIdentity
	lockedExtractionID := correction.ExtractionID
	release, err := s.acquireSourceExtractionLocks(ctx, lockedSourceID, lockedOwnerIdentity, lockedExtractionID)
	if err != nil {
		if errors.Is(err, ErrSyncInProgress) {
			return durablejob.Defer("source correction is waiting for source sync or another extraction operation")
		}
		if errors.Is(err, ErrSourceSyncLeaseUnavailable) || errors.Is(err, ErrSourceExtractionFenceUnavailable) {
			return permanentCorrectionError("worker_fence_unavailable", err)
		}
		if ctx.Err() != nil {
			return durablejob.Defer("source correction paused while the worker is stopping")
		}
		return retryCorrectionError("worker_fence_acquire", err)
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return durablejob.Defer("source correction paused while the worker is stopping")
	}
	correction, durableJob, err := repository.FindExtractionCorrectionForWorker(correctionID, job.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// The row can disappear while this worker waits for the shared lock. In
		// particular, deletion may have committed before the session lock was
		// granted. Re-read under the fence before recording attempts or effects.
		return nil
	}
	if err != nil {
		return retryCorrectionError("intent_recheck", err)
	}
	if correction == nil {
		return retryCorrectionError("intent_recheck", errors.New("source correction intent is unavailable"))
	}
	if correction.SourceID != lockedSourceID || correction.OwnerIdentity != lockedOwnerIdentity || correction.ExtractionID != lockedExtractionID {
		return nil
	}
	if correction.Status != models.SourceExtractionCorrectionPending || durableJob == nil ||
		durableJob.Status != models.DurableJobRunning || durableJob.LockedBy != job.LockedBy ||
		durableJob.LeaseGeneration != job.LeaseGeneration {
		return nil
	}
	return s.runExtractionCorrectionLocked(ctx, job, correctionID, repository, correction)
}

func (s *service) runExtractionCorrectionLocked(
	ctx context.Context,
	job durablejob.Job,
	correctionID uuid.UUID,
	repository extractionCorrectionRepository,
	correction *models.SourceExtractionCorrection,
) error {
	if job.Attempts+1 > 0 {
		if err := repository.RecordExtractionCorrectionAttempt(correctionID, job.ID, job.LockedBy, job.LeaseGeneration, job.Attempts+1); err != nil {
			return retryCorrectionError("attempt_checkpoint", err)
		}
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = extractionCorrectionMaxAttempts
	}
	for steps := 0; steps < 10; steps++ {
		if err := ctx.Err(); err != nil {
			return durablejob.Defer("source correction paused while the worker is stopping")
		}
		if s.evaluateSourceEmergencyStop().Active {
			return durablejob.Defer("source corrections are paused by the emergency stop")
		}
		switch correction.Phase {
		case correctionPhaseIntentPersisted:
			if correction.RequiresRetraction {
				var before extractionCorrectionBeforeState
				if err := json.Unmarshal([]byte(correction.BeforeStateJSON), &before); err != nil {
					return s.finishCorrectionFailure(repository, correction, job, "invalid_intent", "invalid_intent")
				}
				if err := s.retractWorkflowForExtraction(before.extraction(), "corrected extraction no longer contains an actionable task or follow-up"); err != nil {
					return s.correctionFailure(repository, correction, job, "workflow_retract_failed", err)
				}
				if err := repository.AdvanceExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration, correctionPhaseIntentPersisted, correctionPhaseWorkflowRetracted); err != nil {
					return s.correctionFailure(repository, correction, job, "workflow_retract_checkpoint", err)
				}
				correction.Phase = correctionPhaseWorkflowRetracted
				continue
			}
			fallthrough
		case correctionPhaseWorkflowRetracted:
			updated, extraction, conflicted, applyErr := repository.ApplyExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration)
			if applyErr != nil {
				return s.correctionFailure(repository, correction, job, "patch_apply_failed", applyErr)
			}
			correction = updated
			if conflicted && correction.Phase != correctionPhaseConflictReconcile {
				return nil
			}
			if correction.Phase == correctionPhaseConflictReconcile {
				continue
			}
			if extraction == nil {
				return s.correctionFailure(repository, correction, job, "patch_apply_failed", errors.New("applied extraction unavailable"))
			}
			continue
		case correctionPhaseConflictReconcile:
			current, readErr := s.repo.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
			if readErr == nil {
				if err := s.reconcileCurrentExtractionForCorrection(current); err != nil {
					return s.correctionFailure(repository, correction, job, "workflow_reconcile_failed", err)
				}
			} else if !errors.Is(readErr, gorm.ErrRecordNotFound) {
				return s.correctionFailure(repository, correction, job, "workflow_reconcile_failed", readErr)
			}
			if err := repository.FinishExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration,
				models.SourceExtractionCorrectionConflict, correctionPhaseConflict, "revision_conflict"); err != nil {
				return s.correctionFailure(repository, correction, job, "conflict_checkpoint", err)
			}
			return nil
		case correctionPhasePatchApplied:
			current, source, readErr := s.currentCorrectionState(correction)
			if errors.Is(readErr, gorm.ErrRecordNotFound) {
				return s.finishCorrectionConflict(repository, correction, job, "extraction_missing")
			}
			if code := extractionCorrectionStateConflictCode(readErr); code != "" {
				return s.finishCorrectionConflict(repository, correction, job, code)
			}
			if readErr != nil {
				return s.correctionFailure(repository, correction, job, "workflow_reconcile_failed", readErr)
			}
			if strings.TrimSpace(source.OwnerIdentity) != correction.OwnerIdentity {
				return s.finishCorrectionConflict(repository, correction, job, "source_owner_changed")
			}
			if err := s.reconcileCurrentExtractionForCorrection(current); err != nil {
				return s.correctionFailure(repository, correction, job, "workflow_reconcile_failed", err)
			}
			if err := repository.AdvanceExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration, correctionPhasePatchApplied, correctionPhaseWorkflowReconciled); err != nil {
				return s.correctionFailure(repository, correction, job, "workflow_reconcile_checkpoint", err)
			}
			correction.Phase = correctionPhaseWorkflowReconciled
			continue
		case correctionPhaseWorkflowReconciled:
			current, _, readErr := s.currentCorrectionState(correction)
			if errors.Is(readErr, gorm.ErrRecordNotFound) {
				return s.finishCorrectionConflict(repository, correction, job, "extraction_missing")
			}
			if code := extractionCorrectionStateConflictCode(readErr); code != "" {
				return s.finishCorrectionConflict(repository, correction, job, code)
			}
			if readErr != nil {
				return s.correctionFailure(repository, correction, job, "index_update_failed", readErr)
			}
			if err := s.indexExtractionContext(ctx, current); err != nil {
				return s.correctionFailure(repository, correction, job, "index_update_failed", err)
			}
			if err := repository.AdvanceExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration, correctionPhaseWorkflowReconciled, correctionPhaseIndexUpdated); err != nil {
				return s.correctionFailure(repository, correction, job, "index_checkpoint", err)
			}
			correction.Phase = correctionPhaseIndexUpdated
			continue
		case correctionPhaseIndexUpdated:
			current, source, readErr := s.currentCorrectionState(correction)
			if errors.Is(readErr, gorm.ErrRecordNotFound) {
				return s.finishCorrectionConflict(repository, correction, job, "extraction_missing")
			}
			if code := extractionCorrectionStateConflictCode(readErr); code != "" {
				return s.finishCorrectionConflict(repository, correction, job, code)
			}
			if readErr != nil {
				return s.correctionFailure(repository, correction, job, "life_graph_projection_failed", readErr)
			}
			if strings.TrimSpace(source.OwnerIdentity) != correction.OwnerIdentity {
				return s.finishCorrectionConflict(repository, correction, job, "source_owner_changed")
			}
			if _, err := s.projectExtractionToLifeGraph(ctx, source, current); err != nil {
				return s.correctionFailure(repository, correction, job, "life_graph_projection_failed", err)
			}
			if err := repository.AdvanceExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration, correctionPhaseIndexUpdated, correctionPhaseGraphProjected); err != nil {
				return s.correctionFailure(repository, correction, job, "graph_projection_checkpoint", err)
			}
			correction.Phase = correctionPhaseGraphProjected
			continue
		case correctionPhaseGraphProjected:
			before, current, source, readErr := s.correctionMemorySnapshots(correction)
			if errors.Is(readErr, gorm.ErrRecordNotFound) {
				return s.finishCorrectionConflict(repository, correction, job, "extraction_missing")
			}
			if code := extractionCorrectionStateConflictCode(readErr); code != "" {
				return s.finishCorrectionConflict(repository, correction, job, code)
			}
			if readErr != nil {
				return s.correctionFailure(repository, correction, job, "correction_memory_failed", readErr)
			}
			if extractionCorrectionLessonRequired(source, before, current) {
				persister, ok := repository.(extractionCorrectionLessonPersister)
				if !ok {
					return s.correctionFailure(repository, correction, job, "correction_memory_failed",
						errors.New("transactional source correction lesson persistence is unavailable"))
				}
				if correction.AppliedRevision == nil {
					return s.finishCorrectionConflict(repository, correction, job, "applied_revision_missing")
				}
				request := extractionCorrectionMemoryRequest(source, before, current)
				internalSourceURI := "source-extraction://" + correction.ExtractionID.String()
				persisted, err := persister.PersistExtractionCorrectionLesson(
					ctx, correction.ID, job.ID, job.LockedBy, job.LeaseGeneration,
					*correction.AppliedRevision, correction.OwnerIdentity, internalSourceURI, request,
				)
				if err != nil {
					return s.correctionFailure(repository, correction, job, "correction_memory_failed", err)
				}
				if persisted == nil || persisted.SourceURI != request.SourceURI || persisted.SourceExtractionID == nil ||
					*persisted.SourceExtractionID != correction.ExtractionID {
					return s.correctionFailure(repository, correction, job, "correction_memory_failed",
						errors.New("persisted correction lesson did not preserve source evidence provenance"))
				}
				memory.IndexPersistedSourceExtractionLesson(s.memoryService, persisted)
				s.autoLinkPursuitMemory(source, current, persisted)
				s.audit(correction.SourceID, "extraction.correction_memory_created", "stored reviewable source correction lesson")
			}
			if err := repository.AdvanceExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration, correctionPhaseGraphProjected, correctionPhaseMemoryRecorded); err != nil {
				return s.correctionFailure(repository, correction, job, "memory_checkpoint", err)
			}
			correction.Phase = correctionPhaseMemoryRecorded
			continue
		case correctionPhaseMemoryRecorded:
			if err := repository.FinishExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration,
				models.SourceExtractionCorrectionCompleted, correctionPhaseCompleted, ""); err != nil {
				return s.correctionFailure(repository, correction, job, "completion_checkpoint", err)
			}
			s.audit(correction.SourceID, "extraction.corrected", "operator correction and dependent recovery completed")
			return nil
		case correctionPhaseCompleted, correctionPhaseConflict, correctionPhaseFailed:
			return nil
		default:
			return s.finishCorrectionFailure(repository, correction, job, "phase_invalid", "phase_invalid")
		}
	}
	return s.correctionFailure(repository, correction, job, "step_limit", errors.New("correction phase loop exceeded bound"))
}

func (s *service) currentCorrectionState(correction *models.SourceExtractionCorrection) (*models.SourceExtraction, *models.ConnectedSource, error) {
	current, err := s.repo.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
	if err != nil {
		return nil, nil, err
	}
	if current == nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	if correction.AppliedRevision == nil || !current.UpdatedAt.Equal(*correction.AppliedRevision) {
		return nil, nil, errExtractionCorrectionRevisionSuperseded
	}
	source, err := s.repo.FindSource(current.SourceID)
	if err != nil {
		return nil, nil, err
	}
	if source == nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	if strings.TrimSpace(source.OwnerIdentity) != correction.OwnerIdentity {
		return nil, nil, errExtractionCorrectionOwnerChanged
	}
	return current, source, nil
}

func (s *service) reconcileCurrentExtractionForCorrection(current *models.SourceExtraction) error {
	if current == nil {
		return errors.New("current extraction unavailable")
	}
	if current.Archived || firstNonEmpty(current.Tasks, current.FollowUps) == "" {
		return s.retractWorkflowForExtraction(current, "source extraction has no actionable task or follow-up")
	}
	return s.reconcileWorkflowFromExtraction(current)
}

func (s *service) correctionMemorySnapshots(correction *models.SourceExtractionCorrection) (*models.SourceExtraction, *models.SourceExtraction, *models.ConnectedSource, error) {
	var before extractionCorrectionBeforeState
	if err := json.Unmarshal([]byte(correction.BeforeStateJSON), &before); err != nil {
		return nil, nil, nil, err
	}
	current, source, err := s.currentCorrectionState(correction)
	if err != nil {
		return nil, nil, nil, err
	}
	return before.extraction(), current, source, nil
}

func extractionCorrectionLessonRequired(source *models.ConnectedSource, before, after *models.SourceExtraction) bool {
	if source == nil || before == nil || after == nil || !extractionCorrectionUseful(before, after) {
		return false
	}
	if isManualPlanningContextOnlyConnector(source.ConnectorKey) || source.ConnectorKey == trelloConnectorKey {
		return false
	}
	return source.ConnectorKey != "github" || (!after.Uncertain && !after.Sensitive)
}

func extractionCorrectionStateConflictCode(err error) string {
	switch {
	case errors.Is(err, errExtractionCorrectionRevisionSuperseded):
		return "applied_revision_superseded"
	case errors.Is(err, errExtractionCorrectionOwnerChanged):
		return "source_owner_changed"
	default:
		return ""
	}
}

func (s *service) correctionFailure(repository extractionCorrectionRepository, correction *models.SourceExtractionCorrection, job durablejob.Job, code string, cause error) error {
	attempt := job.Attempts + 1
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = extractionCorrectionMaxAttempts
	}
	if attempt >= maxAttempts {
		return s.finishCorrectionFailure(repository, correction, job, code, "retry_limit_reached")
	}
	return retryCorrectionError(code, cause)
}

func (s *service) finishCorrectionFailure(repository extractionCorrectionRepository, correction *models.SourceExtractionCorrection, job durablejob.Job, code, terminalCode string) error {
	phase := correctionPhaseFailed
	if correction != nil && correction.AppliedRevision != nil {
		phase = extractionCorrectionResumePhase(correction)
	}
	if err := repository.FinishExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration,
		models.SourceExtractionCorrectionFailed, phase, terminalCode); err != nil {
		return retryCorrectionError("terminal_checkpoint", err)
	}
	return &extractionCorrectionWorkerError{stage: terminalCode, retryable: false}
}

func extractionCorrectionResumePhase(correction *models.SourceExtractionCorrection) string {
	if correction != nil {
		switch correction.Phase {
		case correctionPhasePatchApplied, correctionPhaseWorkflowReconciled, correctionPhaseIndexUpdated,
			correctionPhaseGraphProjected, correctionPhaseMemoryRecorded:
			return correction.Phase
		}
	}
	return correctionPhasePatchApplied
}

func (s *service) finishCorrectionConflict(repository extractionCorrectionRepository, correction *models.SourceExtractionCorrection, job durablejob.Job, code string) error {
	if err := repository.FinishExtractionCorrection(correction.ID, job.ID, job.LockedBy, job.LeaseGeneration,
		models.SourceExtractionCorrectionConflict, correctionPhaseConflict, code); err != nil {
		return s.correctionFailure(repository, correction, job, "conflict_checkpoint", err)
	}
	return nil
}

func retryCorrectionError(stage string, cause error) error {
	return &extractionCorrectionWorkerError{stage: stage, retryable: true, cause: cause}
}

func permanentCorrectionError(stage string, cause error) error {
	return &extractionCorrectionWorkerError{stage: stage, retryable: false, cause: cause}
}

func extractionCorrectionJobHandler(service Service, allowed ...func() bool) durablejob.Handler {
	backgroundAllowed := schedulerBackgroundGate(allowed)
	return func(ctx context.Context, job durablejob.Job) error {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		var payload extractionCorrectionJobPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return permanentCorrectionError("invalid_job_payload", err)
		}
		correctionID, err := uuid.Parse(strings.TrimSpace(payload.CorrectionID))
		if err != nil || correctionID == uuid.Nil {
			return permanentCorrectionError("invalid_job_identity", err)
		}
		executor, ok := service.(extractionCorrectionExecutor)
		if !ok {
			return permanentCorrectionError("worker_unavailable", errors.New("source correction worker unavailable"))
		}
		return executor.runExtractionCorrection(ctx, job, correctionID)
	}
}

func parseExtractionCorrectionRevision(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	if value == "" || value == "*" || strings.HasPrefix(value, "W/") {
		return time.Time{}, ErrExtractionPatchConflict
	}
	revision, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("invalid extraction revision")
	}
	return revision.UTC(), nil
}

var _ extractionCorrectionExecutor = (*service)(nil)
var _ ExtractionCorrectionService = (*service)(nil)

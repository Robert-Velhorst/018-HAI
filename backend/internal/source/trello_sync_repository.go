package source

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTrelloSyncAlreadyActive   = errors.New("another Trello sync owns the durable board checkpoint")
	ErrTrelloSyncBindingChanged  = errors.New("Trello sync owner or board binding changed; create a new connected source")
	ErrTrelloSyncCheckpointStale = errors.New("Trello page checkpoint no longer matches the durable source state")
	ErrTrelloSyncSourceInactive  = errors.New("Trello source is disabled, paused, or revoked")
)

const (
	trelloPhaseBackfillCards      = "backfill_cards"
	trelloPhaseBackfillActions    = "backfill_actions"
	trelloPhaseCatchUpActions     = "catch_up_actions"
	trelloPhaseIncrementalActions = "incremental_actions"
	trelloPhaseIncrementalCards   = "incremental_cards"
	trelloPhaseReconcileInventory = "reconcile_inventory"
	trelloPhaseIdle               = "idle"
	trelloActionOverlap           = 24 * time.Hour
)

type trelloSyncStateRepository interface {
	FindTrelloSyncState(sourceID uuid.UUID) (*models.TrelloSyncState, error)
	FindTrelloStaleCardsAfter(sourceID uuid.UUID, cycleStartedAt time.Time, afterCardID string, limit int) ([]models.SourceRawItem, error)
	PrepareTrelloSyncState(source *models.ConnectedSource, job *models.SourceSyncJob, now time.Time) (*models.TrelloSyncState, error)
	StartTrelloSyncJob(jobID, sourceID uuid.UUID, now time.Time) (*models.SourceSyncJob, error)
	CommitTrelloSyncPage(source *models.ConnectedSource, job *models.SourceSyncJob, current, next *models.TrelloSyncState, page models.TrelloSyncPage, receipts []models.TrelloActionReceipt, seenCardExternalIDs, verifiedBoardCardExternalIDs []string, complete bool) (*models.ConnectedSource, *models.SourceSyncJob, *models.TrelloSyncState, error)
}

func trelloSourceCanSync(source *models.ConnectedSource) bool {
	if source == nil || !source.Enabled || source.RevokedAt != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(source.Status)) {
	case "paused", "revoked", "reconnect_required":
		return false
	default:
		return true
	}
}

func canonicalTrelloCursor(value string) bool {
	if value == "" {
		return true
	}
	normalized, valid := normalizedTrelloMongoID(value)
	return valid && normalized == value
}

func validTrelloFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validTrelloActionReceipt(receipt models.TrelloActionReceipt, sourceID uuid.UUID) bool {
	actionID, validActionID := normalizedTrelloMongoID(receipt.ActionID)
	cardID, validCardID := normalizedTrelloMongoID(receipt.CardID)
	return receipt.SourceID == sourceID && validActionID && actionID == receipt.ActionID &&
		validCardID && cardID == receipt.CardID && validTrelloFingerprint(receipt.Fingerprint) &&
		!receipt.OccurredAt.IsZero()
}

func validateTrelloPageCursorProgress(
	current, next *models.TrelloSyncState,
	page models.TrelloSyncPage,
	receipts []models.TrelloActionReceipt,
	seenCardExternalIDs []string,
	complete bool,
	jobStatus string,
) error {
	if current == nil || next == nil || current.Phase == trelloPhaseIdle || !isTrelloSyncPhase(current.Phase) ||
		!canonicalTrelloCursor(current.CardCursor) || !canonicalTrelloCursor(current.ActionCursor) ||
		!canonicalTrelloCursor(page.CursorBefore) || !canonicalTrelloCursor(page.CursorAfter) ||
		!validTrelloFingerprint(page.Fingerprint) || page.RecordCount < 0 || page.RecordCount > trelloMaxRecordsPerSync ||
		page.RequestCount < 1 || page.RequestCount > trelloMaxRequestsPerSync ||
		page.ResponseBytes < 1 || page.ResponseBytes > trelloMaxPageBytesPerSync || current.PagesProcessed < 0 {
		return errors.New("Trello page cursor or evidence contract is invalid")
	}
	if err := validateTrelloSeenCardIDs(current.Phase, page.RecordCount, seenCardExternalIDs); err != nil {
		return err
	}
	before := current.CardCursor
	if strings.Contains(current.Phase, "action") {
		before = current.ActionCursor
	}
	if page.CursorBefore != before {
		return errors.New("Trello page cursor-before does not match the durable checkpoint")
	}
	if next.PagesProcessed != current.PagesProcessed+1 || current.RecordsProcessed < 0 || next.RecordsProcessed < current.RecordsProcessed ||
		next.RecordsProcessed-current.RecordsProcessed != int64(page.RecordCount) {
		return errors.New("Trello page progress counters do not match its durable records")
	}
	seenReceiptIDs := make(map[string]struct{}, len(receipts))
	previousReceiptID := ""
	for _, receipt := range receipts {
		if !validTrelloActionReceipt(receipt, current.SourceID) {
			return errors.New("Trello action receipt identity or fingerprint is invalid")
		}
		if _, duplicate := seenReceiptIDs[receipt.ActionID]; duplicate {
			return errors.New("Trello action page contains a duplicate receipt")
		}
		if previousReceiptID != "" && receipt.ActionID >= previousReceiptID {
			return errors.New("Trello action receipts are not in descending provider order")
		}
		if current.ActionCursor != "" && receipt.ActionID >= current.ActionCursor {
			return errors.New("Trello action receipt does not advance beyond its durable cursor")
		}
		seenReceiptIDs[receipt.ActionID] = struct{}{}
		previousReceiptID = receipt.ActionID
	}

	decreasing := func(after string) bool {
		return canonicalTrelloCursor(after) && after != "" && (before == "" || after < before)
	}
	advancesReconciliation := func(after string) bool {
		return canonicalTrelloCursor(after) && after != "" && (before == "" || after > before)
	}

	switch current.Phase {
	case trelloPhaseBackfillCards, trelloPhaseIncrementalCards:
		if current.ActionCursor != "" || next.ActionCursor != "" || len(receipts) != 0 ||
			page.RecordCount > trelloCardPageSize || len(seenCardExternalIDs) != page.RecordCount {
			return errors.New("Trello card page count does not match its observed identities")
		}
		expectedAfter := before
		previousCardID := ""
		for index, externalID := range seenCardExternalIDs {
			cardID := strings.TrimPrefix(externalID, trelloCardExternalIDPrefix)
			if index == 0 && cardID == before {
				previousCardID = cardID
				continue
			}
			if previousCardID != "" && cardID >= previousCardID {
				return errors.New("Trello card identities are not in descending provider order")
			}
			if before != "" && cardID >= before {
				return errors.New("Trello card page does not advance beyond its durable cursor")
			}
			previousCardID = cardID
		}
		if len(seenCardExternalIDs) > 0 {
			expectedAfter = strings.TrimPrefix(seenCardExternalIDs[len(seenCardExternalIDs)-1], trelloCardExternalIDPrefix)
		}
		if page.CursorAfter != expectedAfter {
			return errors.New("Trello card page cursor-after does not match its final observed card")
		}
		if page.RecordCount == trelloCardPageSize {
			if next.Phase != current.Phase || next.CardCursor != page.CursorAfter || !decreasing(next.CardCursor) {
				return errors.New("full Trello card page must advance its descending card cursor")
			}
			return nil
		}
		expectedPhase := trelloPhaseReconcileInventory
		if current.Phase == trelloPhaseBackfillCards {
			expectedPhase = trelloPhaseBackfillActions
		}
		if next.Phase != expectedPhase || next.CardCursor != "" || next.ActionCursor != "" ||
			(before != "" && page.CursorAfter != before && page.CursorAfter >= before) {
			return errors.New("short Trello card page must release its cursor and advance to the next phase")
		}
	case trelloPhaseBackfillActions, trelloPhaseCatchUpActions, trelloPhaseIncrementalActions:
		if current.CardCursor != "" || next.CardCursor != "" ||
			page.RecordCount > trelloActionPageSize || len(receipts) > page.RecordCount || page.RecordCount-len(receipts) > 1 {
			return errors.New("Trello action page count does not match its durable receipts")
		}
		expectedAfter := before
		if len(receipts) > 0 {
			expectedAfter = receipts[len(receipts)-1].ActionID
		}
		if page.CursorAfter != expectedAfter {
			return errors.New("Trello action page cursor-after does not match its final action receipt")
		}
		if page.RecordCount == trelloActionPageSize {
			if next.Phase != current.Phase || next.ActionCursor != page.CursorAfter || !decreasing(next.ActionCursor) {
				return errors.New("full Trello action page must advance its descending action cursor")
			}
			return nil
		}
		expectedPhase := trelloPhaseIncrementalCards
		if current.Phase == trelloPhaseBackfillActions {
			expectedPhase = trelloPhaseCatchUpActions
		}
		if next.Phase != expectedPhase || next.ActionCursor != "" || next.CardCursor != "" ||
			(before != "" && page.CursorAfter != before && page.CursorAfter >= before) {
			return errors.New("short Trello action page must release its cursor and advance to the next phase")
		}
	case trelloPhaseReconcileInventory:
		if current.ActionCursor != "" || next.ActionCursor != "" || len(receipts) != 0 ||
			page.RecordCount > trelloMaxRequestsPerSync {
			return errors.New("Trello inventory page exceeds the bounded verification request count")
		}
		if complete {
			if next.Phase != trelloPhaseIdle || next.CardCursor != "" || next.ActionCursor != "" ||
				!trelloCompletionJobStatusAllowed(jobStatus) ||
				(page.RecordCount == 0 && page.CursorAfter != before) ||
				(page.RecordCount > 0 && before != "" && page.CursorAfter <= before) {
				return errors.New("completed Trello inventory page must release its checkpoint after verified progress")
			}
			if page.RecordCount > 0 && !advancesReconciliation(page.CursorAfter) {
				return errors.New("completed Trello inventory page has an invalid final cursor")
			}
			return nil
		}
		if next.Phase != trelloPhaseReconcileInventory || next.CardCursor != page.CursorAfter || next.ActionCursor != "" ||
			(page.RecordCount == 0 && page.CursorAfter != before) ||
			(page.RecordCount > 0 && !advancesReconciliation(page.CursorAfter)) {
			return errors.New("Trello inventory page does not match its ascending verification cursor")
		}
	default:
		return errors.New("Trello page uses an unsupported durable phase")
	}
	return nil
}

func (r *GormRepository) FindTrelloStaleCardsAfter(sourceID uuid.UUID, cycleStartedAt time.Time, afterCardID string, limit int) ([]models.SourceRawItem, error) {
	if sourceID == uuid.Nil || cycleStartedAt.IsZero() || limit < 1 || limit > trelloMaxRecordsPerSync {
		return nil, errors.New("Trello stale-card query bounds are invalid")
	}
	if afterCardID != "" {
		normalized, valid := normalizedTrelloMongoID(afterCardID)
		if !valid || normalized != afterCardID {
			return nil, errors.New("Trello stale-card cursor is invalid")
		}
	}
	var cards []models.SourceRawItem
	query := r.DB.Where("source_id = ? AND item_type = ? AND external_id LIKE ?", sourceID, "trello_card", trelloCardExternalIDPrefix+"%").
		Where("(fetched_at IS NULL OR fetched_at < ?)", cycleStartedAt.UTC())
	if afterCardID != "" {
		query = query.Where("external_id > ?", trelloCardExternalIDPrefix+afterCardID)
	}
	err := query.Order("external_id asc").Limit(limit).Find(&cards).Error
	return cards, err
}

func (r *GormRepository) PrepareTrelloSyncState(source *models.ConnectedSource, job *models.SourceSyncJob, now time.Time) (*models.TrelloSyncState, error) {
	if source == nil || job == nil || source.ID == uuid.Nil || job.ID == uuid.Nil || source.ID != job.SourceID {
		return nil, errors.New("Trello sync state identity is incomplete")
	}
	boardID, err := trelloBoardID(source.SyncTarget)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(source.OwnerIdentity)
	if owner == "" || owner != source.OwnerIdentity {
		return nil, errors.New("Trello source must have a canonical owner identity")
	}
	if job.Mode == ModeManualAsyncSync && job.OwnerIdentity != owner {
		return nil, ErrTrelloSyncBindingChanged
	}
	now = now.UTC()
	var state models.TrelloSyncState
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		var persistedSource models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND connector_key = ? AND owner_identity = ?", source.ID, trelloConnectorKey, owner).
			First(&persistedSource).Error; err != nil {
			return ErrTrelloSyncBindingChanged
		}
		if !trelloSourceCanSync(&persistedSource) {
			return ErrTrelloSyncSourceInactive
		}
		persistedBoardID, err := trelloBoardID(persistedSource.SyncTarget)
		if err != nil || persistedBoardID != boardID {
			return ErrTrelloSyncBindingChanged
		}

		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", source.ID).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = models.TrelloSyncState{
				SourceID: source.ID, OwnerIdentity: owner, BoardID: boardID,
				LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseBackfillCards,
				CycleStartedAt: now, UpdatedAt: now,
			}
			return tx.Create(&state).Error
		}
		if err != nil {
			return err
		}
		if state.OwnerIdentity != owner || state.BoardID != boardID {
			return ErrTrelloSyncBindingChanged
		}
		if !isTrelloSyncPhase(state.Phase) {
			return fmt.Errorf("Trello sync state has unsupported phase %q", state.Phase)
		}
		if state.Phase == trelloPhaseIdle && state.LogicalJobID != nil {
			return errors.New("completed Trello sync state still references an active logical job")
		}
		if state.LogicalJobID != nil && *state.LogicalJobID != job.ID {
			var previousJob models.SourceSyncJob
			if err := tx.Where("id = ? AND source_id = ?", *state.LogicalJobID, source.ID).First(&previousJob).Error; err != nil {
				return ErrTrelloSyncAlreadyActive
			}
			if previousJob.Mode == ModeManualAsyncSync && previousJob.OwnerIdentity != owner {
				return ErrTrelloSyncBindingChanged
			}
			if !terminalTrelloSyncJob(&previousJob) {
				return ErrTrelloSyncAlreadyActive
			}
			// A terminal manual job may be replaced by a new owner-authorized
			// request. Keep the generation, phase, and cursors so this is a resume,
			// not a second full-board scan.
			state.LogicalJobID = trelloUUIDPointer(job.ID)
		}
		if state.Phase != trelloPhaseIdle && state.LogicalJobID == nil {
			return errors.New("active Trello sync state is missing its logical job identity")
		}
		if state.Phase == trelloPhaseIdle {
			beginTrelloSyncGeneration(&state, now)
		}
		state.LogicalJobID = trelloUUIDPointer(job.ID)
		state.UpdatedAt = now
		if err := tx.Select("*").Save(&state).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func beginTrelloSyncGeneration(state *models.TrelloSyncState, now time.Time) {
	state.Generation++
	state.Phase = trelloPhaseIncrementalActions
	state.CardCursor = ""
	state.ActionCursor = ""
	state.MaxCardActivityAt = nil
	state.ActionSince = nil
	if state.LastSuccessfulAt != nil {
		// New actions can arrive above the first descending page while later
		// slices are still running. Completion time is not an action watermark.
		watermark := *state.LastSuccessfulAt
		if !state.CycleStartedAt.IsZero() && state.CycleStartedAt.Before(watermark) {
			watermark = state.CycleStartedAt
		}
		value := watermark.Add(-trelloActionOverlap)
		state.ActionSince = &value
	}
	state.PagesProcessed = 0
	state.RecordsProcessed = 0
	state.CycleStartedAt = now.UTC()
}

func (r *GormRepository) FindTrelloSyncState(sourceID uuid.UUID) (*models.TrelloSyncState, error) {
	if sourceID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}
	var state models.TrelloSyncState
	if err := r.DB.Where("source_id = ?", sourceID).First(&state).Error; err != nil {
		return nil, err
	}
	if state.LogicalJobID == nil || state.Phase == trelloPhaseIdle {
		return &state, nil
	}
	var previousJob models.SourceSyncJob
	err := r.DB.Where("id = ? AND source_id = ?", *state.LogicalJobID, sourceID).First(&previousJob).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &state, nil
	}
	if err != nil {
		return nil, err
	}
	if !terminalTrelloSyncJob(&previousJob) {
		return &state, nil
	}
	// The service uses this read to reject a different logical job. Surface a
	// replacement only for an accepted owner-bound manual request; PrepareTrelloSyncState
	// rechecks the terminal job and transfers the persisted checkpoint atomically.
	var candidates []models.SourceSyncJob
	if err := r.DB.Where(
		"source_id = ? AND owner_identity = ? AND mode = ? AND status IN ? AND id <> ?",
		sourceID, state.OwnerIdentity, ModeManualAsyncSync, []string{"queued", "running"}, previousJob.ID,
	).Order("created_at asc, id asc").Limit(2).Find(&candidates).Error; err != nil {
		return nil, err
	}
	state.LogicalJobID, err = trelloResumeJobIDForOwner(&state, &previousJob, candidates)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// StartTrelloSyncJob starts an existing logical job without resetting page
// progress. It is used by both scheduled and manual workers after a defer or
// crash-recovery claim.
func (r *GormRepository) StartTrelloSyncJob(jobID, sourceID uuid.UUID, now time.Time) (*models.SourceSyncJob, error) {
	if jobID == uuid.Nil || sourceID == uuid.Nil {
		return nil, errors.New("Trello sync job identity is incomplete")
	}
	var job models.SourceSyncJob
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND source_id = ?", jobID, sourceID).First(&job).Error; err != nil {
			return err
		}
		if job.Status == "completed" || job.Status == "cancelled" {
			return fmt.Errorf("Trello sync job is already terminal")
		}
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND connector_key = ?", sourceID, trelloConnectorKey).
			First(&source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTrelloSyncBindingChanged
			}
			return err
		}
		if !trelloSourceCanSync(&source) {
			return ErrTrelloSyncSourceInactive
		}
		if job.Mode == ModeManualAsyncSync && job.OwnerIdentity != source.OwnerIdentity {
			return ErrTrelloSyncBindingChanged
		}
		job.Status = "running"
		if job.StartedAt.IsZero() {
			job.StartedAt = now.UTC()
		}
		job.CompletedAt = nil
		job.CursorAfter = source.Cursor
		job.UpdatedAt = now.UTC()
		job.Message = "Trello sync is running; page progress is saved between worker slices."
		return tx.Select("*").Save(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func validateTrelloSeenCardIDs(phase string, recordCount int, externalIDs []string) error {
	if strings.Contains(phase, "cards") {
		if len(externalIDs) != recordCount {
			return errors.New("Trello card page checkpoint is missing observed card identities")
		}
		seen := make(map[string]struct{}, len(externalIDs))
		for _, externalID := range externalIDs {
			cardID, valid := normalizedTrelloMongoID(strings.TrimPrefix(externalID, trelloCardExternalIDPrefix))
			if !strings.HasPrefix(externalID, trelloCardExternalIDPrefix) || !valid || cardID != strings.TrimPrefix(externalID, trelloCardExternalIDPrefix) {
				return errors.New("Trello card page contains an invalid observed card identity")
			}
			if _, duplicate := seen[externalID]; duplicate {
				return errors.New("Trello card page contains a duplicate observed card identity")
			}
			seen[externalID] = struct{}{}
		}
		return nil
	}
	if len(externalIDs) != 0 {
		return errors.New("Trello action page cannot checkpoint card inventory identities")
	}
	return nil
}

func validateTrelloVerifiedBoardCardIDs(phase string, pageIDs, verifiedIDs []string) error {
	if len(verifiedIDs) > 0 && phase != trelloPhaseReconcileInventory {
		return errors.New("Trello verified board identities are only valid during inventory reconciliation")
	}
	seen := make(map[string]struct{}, len(pageIDs)+len(verifiedIDs))
	for _, externalID := range pageIDs {
		seen[externalID] = struct{}{}
	}
	for _, externalID := range verifiedIDs {
		cardID, valid := normalizedTrelloMongoID(strings.TrimPrefix(externalID, trelloCardExternalIDPrefix))
		if !strings.HasPrefix(externalID, trelloCardExternalIDPrefix) || !valid || cardID != strings.TrimPrefix(externalID, trelloCardExternalIDPrefix) {
			return errors.New("Trello verified board inventory contains an invalid card identity")
		}
		if _, duplicate := seen[externalID]; duplicate {
			return errors.New("Trello verified board inventory contains a duplicate card identity")
		}
		seen[externalID] = struct{}{}
	}
	return nil
}

func trelloUUIDPointer(value uuid.UUID) *uuid.UUID {
	copyValue := value
	return &copyValue
}

func isTrelloSyncPhase(phase string) bool {
	switch phase {
	case trelloPhaseBackfillCards, trelloPhaseBackfillActions, trelloPhaseCatchUpActions,
		trelloPhaseIncrementalActions, trelloPhaseIncrementalCards, trelloPhaseReconcileInventory, trelloPhaseIdle:
		return true
	default:
		return false
	}
}

func validTrelloSyncTransition(current, next string) bool {
	switch current {
	case trelloPhaseBackfillCards:
		return next == trelloPhaseBackfillCards || next == trelloPhaseBackfillActions
	case trelloPhaseBackfillActions:
		return next == trelloPhaseBackfillActions || next == trelloPhaseCatchUpActions
	case trelloPhaseCatchUpActions, trelloPhaseIncrementalActions:
		return next == current || next == trelloPhaseIncrementalCards
	case trelloPhaseIncrementalCards:
		return next == trelloPhaseIncrementalCards || next == trelloPhaseReconcileInventory
	case trelloPhaseReconcileInventory:
		return next == trelloPhaseReconcileInventory || next == trelloPhaseIdle
	default:
		return false
	}
}

func terminalTrelloSyncJob(job *models.SourceSyncJob) bool {
	if job == nil || job.CompletedAt == nil {
		return false
	}
	return job.Status == "failed" || job.Status == "cancelled"
}

func trelloResumeJobIDForOwner(
	state *models.TrelloSyncState,
	previousJob *models.SourceSyncJob,
	candidates []models.SourceSyncJob,
) (*uuid.UUID, error) {
	if state == nil || state.LogicalJobID == nil {
		return nil, nil
	}
	checkpointJobID := *state.LogicalJobID
	if state.Phase == trelloPhaseIdle || previousJob == nil || previousJob.ID != checkpointJobID ||
		previousJob.SourceID != state.SourceID || !terminalTrelloSyncJob(previousJob) ||
		(previousJob.Mode == ModeManualAsyncSync && previousJob.OwnerIdentity != state.OwnerIdentity) {
		return state.LogicalJobID, nil
	}

	var replacementID uuid.UUID
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.ID == uuid.Nil || candidate.ID == checkpointJobID || candidate.SourceID != state.SourceID ||
			candidate.OwnerIdentity != state.OwnerIdentity || candidate.Mode != ModeManualAsyncSync ||
			(candidate.Status != "queued" && candidate.Status != "running") {
			continue
		}
		if replacementID != uuid.Nil {
			return nil, ErrTrelloSyncAlreadyActive
		}
		replacementID = candidate.ID
	}
	if replacementID == uuid.Nil {
		return state.LogicalJobID, nil
	}
	return trelloUUIDPointer(replacementID), nil
}

func (r *GormRepository) CommitTrelloSyncPage(
	source *models.ConnectedSource,
	job *models.SourceSyncJob,
	current, next *models.TrelloSyncState,
	page models.TrelloSyncPage,
	receipts []models.TrelloActionReceipt,
	seenCardExternalIDs, verifiedBoardCardExternalIDs []string,
	complete bool,
) (*models.ConnectedSource, *models.SourceSyncJob, *models.TrelloSyncState, error) {
	if source == nil || job == nil || current == nil || next == nil || source.ID == uuid.Nil ||
		job.ID == uuid.Nil || source.ID != job.SourceID || current.SourceID != source.ID ||
		next.SourceID != source.ID || current.LogicalJobID == nil || *current.LogicalJobID != job.ID ||
		page.SourceID != source.ID || page.LogicalJobID != job.ID {
		return nil, nil, nil, errors.New("Trello page checkpoint identity is invalid")
	}
	if strings.TrimSpace(current.OwnerIdentity) == "" || current.OwnerIdentity != source.OwnerIdentity ||
		strings.TrimSpace(current.BoardID) == "" || next.OwnerIdentity != current.OwnerIdentity ||
		next.BoardID != current.BoardID || next.Generation != current.Generation ||
		next.SourceID != current.SourceID || next.CycleStartedAt != current.CycleStartedAt {
		return nil, nil, nil, ErrTrelloSyncBindingChanged
	}
	if page.Generation != current.Generation || page.Phase != current.Phase || !isTrelloSyncPhase(page.Phase) ||
		page.RecordCount < 0 || page.RecordCount > trelloMaxRecordsPerSync ||
		page.RequestCount < 0 || page.RequestCount > trelloMaxRequestsPerSync ||
		page.ResponseBytes < 0 || page.ResponseBytes > trelloMaxPageBytesPerSync {
		return nil, nil, nil, errors.New("Trello page exceeds or violates its durable checkpoint contract")
	}
	pageCursorBefore := current.CardCursor
	if strings.Contains(current.Phase, "action") {
		pageCursorBefore = current.ActionCursor
	}
	_, pageCursorAfterValid := normalizedTrelloMongoID(page.CursorAfter)
	if page.CursorBefore != pageCursorBefore || (page.CursorAfter != "" && !pageCursorAfterValid) {
		return nil, nil, nil, errors.New("Trello page cursors do not match its phase checkpoint")
	}
	if complete {
		if current.Phase != trelloPhaseReconcileInventory || next.Phase != trelloPhaseIdle || next.LogicalJobID != nil || !trelloCompletionJobStatusAllowed(job.Status) {
			return nil, nil, nil, errors.New("completed Trello page must release its logical job checkpoint")
		}
	} else if next.Phase == trelloPhaseIdle || next.LogicalJobID == nil || *next.LogicalJobID != job.ID {
		return nil, nil, nil, errors.New("unfinished Trello page must retain its logical job checkpoint")
	}
	if !validTrelloSyncTransition(current.Phase, next.Phase) ||
		next.PagesProcessed != current.PagesProcessed+1 || next.RecordsProcessed < current.RecordsProcessed ||
		next.RecordsProcessed-current.RecordsProcessed > int64(page.RecordCount) ||
		(next.Phase == trelloPhaseBackfillCards || next.Phase == trelloPhaseIncrementalCards) && next.ActionCursor != "" ||
		(next.Phase == trelloPhaseBackfillActions || next.Phase == trelloPhaseCatchUpActions || next.Phase == trelloPhaseIncrementalActions) && next.CardCursor != "" ||
		(next.Phase == trelloPhaseReconcileInventory && next.ActionCursor != "") {
		return nil, nil, nil, errors.New("Trello page would violate phase, cursor, or progress ordering")
	}
	if err := validateTrelloSeenCardIDs(current.Phase, page.RecordCount, seenCardExternalIDs); err != nil {
		return nil, nil, nil, err
	}
	if err := validateTrelloVerifiedBoardCardIDs(current.Phase, seenCardExternalIDs, verifiedBoardCardExternalIDs); err != nil {
		return nil, nil, nil, err
	}
	if err := validateTrelloPageCursorProgress(current, next, page, receipts, seenCardExternalIDs, complete, job.Status); err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().UTC()
	page.ID = uuid.New()
	page.CommittedAt = now
	job.UpdatedAt = now
	next.UpdatedAt = now
	var committedState models.TrelloSyncState
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var persistedSource models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ? AND connector_key = ?", source.ID, current.OwnerIdentity, trelloConnectorKey).
			First(&persistedSource).Error; err != nil {
			return ErrTrelloSyncBindingChanged
		}
		if !trelloSourceCanSync(&persistedSource) {
			return ErrTrelloSyncSourceInactive
		}
		persistedBoardID, err := trelloBoardID(persistedSource.SyncTarget)
		if err != nil || persistedBoardID != current.BoardID {
			return ErrTrelloSyncBindingChanged
		}

		var persisted models.TrelloSyncState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ?", source.ID).First(&persisted).Error; err != nil {
			return err
		}
		if persisted.OwnerIdentity != current.OwnerIdentity || persisted.BoardID != current.BoardID ||
			persisted.LogicalJobID == nil || *persisted.LogicalJobID != job.ID ||
			persisted.Generation != current.Generation || persisted.Phase != current.Phase ||
			persisted.CardCursor != current.CardCursor || persisted.ActionCursor != current.ActionCursor {
			return ErrTrelloSyncCheckpointStale
		}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&page)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTrelloSyncCheckpointStale
		}
		for index := range receipts {
			receipt := receipts[index]
			var existing models.TrelloActionReceipt
			err := tx.Where("source_id = ? AND action_id = ?", receipt.SourceID, receipt.ActionID).First(&existing).Error
			if err == nil {
				if existing.CardID != receipt.CardID {
					return fmt.Errorf("Trello action %s changed its card identity after its durable receipt", receipt.ActionID)
				}
				if existing.Fingerprint != receipt.Fingerprint || !existing.OccurredAt.Equal(receipt.OccurredAt) {
					if err := tx.Model(&existing).Updates(map[string]any{
						"occurred_at": receipt.OccurredAt, "fingerprint": receipt.Fingerprint,
					}).Error; err != nil {
						return err
					}
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Create(&receipt).Error; err != nil {
				return err
			}
		}
		fetchedCardExternalIDs := append(append([]string(nil), seenCardExternalIDs...), verifiedBoardCardExternalIDs...)
		if len(fetchedCardExternalIDs) > 0 {
			if err := tx.Model(&models.SourceRawItem{}).
				Where("source_id = ? AND external_id IN ?", source.ID, fetchedCardExternalIDs).
				Update("fetched_at", now).Error; err != nil {
				return err
			}
		}

		stateUpdate := tx.Model(&models.TrelloSyncState{}).
			Where("source_id = ? AND owner_identity = ? AND board_id = ? AND logical_job_id = ? AND generation = ? AND phase = ? AND card_cursor = ? AND action_cursor = ?",
				source.ID, current.OwnerIdentity, current.BoardID, job.ID, current.Generation, current.Phase, current.CardCursor, current.ActionCursor).
			Select("owner_identity", "board_id", "logical_job_id", "generation", "phase", "card_cursor", "action_cursor", "action_since", "cycle_started_at", "last_successful_at", "max_card_activity_at", "pages_processed", "records_processed", "updated_at").
			UpdateColumns(next)
		if stateUpdate.Error != nil {
			return stateUpdate.Error
		}
		if stateUpdate.RowsAffected != 1 {
			return ErrTrelloSyncCheckpointStale
		}

		if complete {
			if !trelloCompletionJobStatusAllowed(job.Status) || source.LastSyncedAt == nil || strings.TrimSpace(source.Cursor) == "" {
				return errors.New("Trello completion requires a verified final cursor")
			}
			update := tx.Model(&models.ConnectedSource{}).Where("id = ? AND owner_identity = ?", source.ID, current.OwnerIdentity).
				Updates(map[string]any{"cursor": source.Cursor, "last_synced_at": source.LastSyncedAt, "updated_at": now})
			if update.Error != nil || update.RowsAffected != 1 {
				if update.Error != nil {
					return update.Error
				}
				return gorm.ErrRecordNotFound
			}
		}
		jobResult := tx.Model(&models.SourceSyncJob{}).Where("id = ? AND source_id = ?", job.ID, source.ID).Select("*").Updates(job)
		if jobResult.Error != nil {
			return jobResult.Error
		}
		if jobResult.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("source_id = ?", source.ID).First(&committedState).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	source.UpdatedAt = now
	job.UpdatedAt = now
	return source, job, &committedState, nil
}

func trelloCompletionJobStatusAllowed(status string) bool {
	return status == "completed" || status == "partial_failure"
}

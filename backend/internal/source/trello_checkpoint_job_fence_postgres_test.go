package source

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestTrelloPostgresCheckpointCannotOverwriteStoppedOrReboundJob(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	for _, mutation := range []string{"cancelled", "completed", "failed", "partial_failure", "queued", "completion marker", "owner", "mode", "legacy scheduled null owner"} {
		t.Run(mutation, func(t *testing.T) {
			withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
				source := newTrelloSource(uuid.New(), "abc123XY", "")
				source.OwnerIdentity = "trello-checkpoint-fixture-" + uuid.NewString()
				at := time.Now().UTC().Truncate(time.Microsecond)
				job := models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, Mode: ModeManualAsyncSync, Status: "running", StartedAt: at}
				if mutation == "legacy scheduled null owner" {
					job.OwnerIdentity, job.Mode = "", ModeScheduledSync
				}
				state := models.TrelloSyncState{SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY", LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseReconcileInventory, CycleStartedAt: at}
				cardID := testTrelloMongoID(20)
				card := trelloPostgresRawCard(source.ID, cardID, at.Add(-time.Hour))
				for _, record := range []any{source, &job, &state, &card} {
					if err := tx.Create(record).Error; err != nil {
						return err
					}
				}
				next := state
				next.CardCursor = cardID
				next.PagesProcessed++
				next.RecordsProcessed++
				page := models.TrelloSyncPage{SourceID: source.ID, LogicalJobID: job.ID, Generation: state.Generation, Phase: state.Phase, CursorAfter: cardID, RecordCount: 1, RequestCount: 1, ResponseBytes: 64, Fingerprint: strings.Repeat("a", 64)}
				changes := map[string]any{}
				switch mutation {
				case "legacy scheduled null owner":
					changes["owner_identity"] = nil
				case "owner":
					changes["owner_identity"] = "different-owner"
				case "mode":
					changes["mode"] = "different-mode"
				case "completion marker":
					changes["completed_at"] = at
				default:
					changes["status"] = mutation
					if mutation != "queued" {
						changes["completed_at"] = at
					}
				}
				if err := tx.Model(&models.SourceSyncJob{}).Where("id = ?", job.ID).Updates(changes).Error; err != nil {
					return err
				}
				var stopped models.SourceSyncJob
				if err := tx.First(&stopped, "id = ?", job.ID).Error; err != nil {
					return err
				}
				repository := &GormRepository{DB: tx}
				if mutation == "legacy scheduled null owner" {
					_, _, _, err := repository.CommitTrelloSyncPage(source, &job, &state, &next, page, nil, nil, []string{card.ExternalID}, false)
					return err
				}
				if _, _, _, err := repository.CommitTrelloSyncPage(source, &job, &state, &next, page, nil, nil, []string{card.ExternalID}, false); !errors.Is(err, ErrTrelloSyncCheckpointStale) {
					return fmt.Errorf("late checkpoint error = %v, want stale checkpoint", err)
				}
				var observed models.SourceSyncJob
				var checkpoint models.TrelloSyncState
				var retained models.SourceRawItem
				if err := tx.First(&observed, "id = ?", job.ID).Error; err != nil {
					return err
				}
				if err := tx.First(&checkpoint, "source_id = ?", source.ID).Error; err != nil {
					return err
				}
				if err := tx.First(&retained, "id = ?", card.ID).Error; err != nil {
					return err
				}
				var pages int64
				if err := tx.Model(&models.TrelloSyncPage{}).Where("source_id = ?", source.ID).Count(&pages).Error; err != nil {
					return err
				}
				if observed.Status != stopped.Status || observed.OwnerIdentity != stopped.OwnerIdentity || observed.Mode != stopped.Mode || !sameTrelloCompletionMarker(observed.CompletedAt, stopped.CompletedAt) ||
					checkpoint.CardCursor != state.CardCursor || checkpoint.PagesProcessed != state.PagesProcessed || !retained.FetchedAt.Equal(card.FetchedAt) || pages != 0 {
					return errors.New("rejected late checkpoint changed durable job, card, page ledger or checkpoint")
				}
				if err := tx.Model(&models.SourceSyncJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": "running", "completed_at": nil, "owner_identity": job.OwnerIdentity, "mode": job.Mode}).Error; err != nil {
					return err
				}
				if _, _, _, err := repository.CommitTrelloSyncPage(source, &job, &state, &next, page, nil, nil, []string{card.ExternalID}, false); err != nil {
					return fmt.Errorf("valid running checkpoint rejected: %w", err)
				}
				return nil
			})
		})
	}
}

func sameTrelloCompletionMarker(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTrelloRepositoryPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("validated Trello test configuration could not be parsed")
	}
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["statement_timeout"] = "10000"
	sqlDB := stdlib.OpenDB(*config)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal("configured dedicated Trello test database is unavailable")
	}

	var databaseName string
	if err := sqlDB.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal("could not verify dedicated Trello test database identity")
	}
	if databaseName != "hai_migration_runner_test" {
		t.Fatal("connected Trello test database identity does not match the dedicated target")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("could not initialize guarded Trello test database access")
	}
	for _, model := range []any{
		&models.ConnectedSource{}, &models.SourceSyncJob{}, &models.SourceRawItem{},
		&models.TrelloSyncState{}, &models.TrelloSyncPage{}, &models.TrelloWebhookReceipt{},
		&models.TrelloWebhookReconciliationState{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("dedicated Trello test database is not migrated: missing table for %T", model)
		}
	}
	if !db.Migrator().HasColumn(&models.SourceSyncJob{}, "progress_phase") {
		t.Fatal("dedicated Trello test database is missing migration 0095 source_sync_jobs.progress_phase")
	}
	return db
}

func withTrelloPostgresRollback(t *testing.T, db *gorm.DB, check func(*gorm.DB) error) {
	t.Helper()
	rollback := errors.New("rollback Trello Postgres integration fixture")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := check(tx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Trello Postgres integration fixture did not roll back: %v", err)
	}
}

func TestTrelloPostgresFindStaleCardsAfterOrdersPaginatesAndFilters(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		sourceID := uuid.New()
		otherSourceID := uuid.New()
		cycleStartedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		staleAt := cycleStartedAt.Add(-time.Hour)
		items := []models.SourceRawItem{
			trelloPostgresRawCard(sourceID, testTrelloMongoID(5), staleAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(10), staleAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(20), staleAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(30), staleAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(40), staleAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(50), cycleStartedAt),
			trelloPostgresRawCard(sourceID, testTrelloMongoID(60), cycleStartedAt.Add(time.Minute)),
			trelloPostgresRawCard(otherSourceID, testTrelloMongoID(25), staleAt),
			{
				ID: uuid.New(), SourceID: sourceID, ExternalID: "trello:action:" + testTrelloMongoID(15),
				ItemType: "trello_action", FetchedAt: staleAt,
			},
			{
				ID: uuid.New(), SourceID: sourceID, ExternalID: "not-a-trello-card", ItemType: "trello_card",
				FetchedAt: staleAt,
			},
		}
		if err := tx.Create(&items).Error; err != nil {
			return fmt.Errorf("seed Trello stale-card query fixtures: %w", err)
		}
		nullFetchedAtExternalID := trelloCardExternalIDPrefix + testTrelloMongoID(35)
		if err := tx.Exec(
			"INSERT INTO source_raw_items (id, source_id, external_id, item_type, fetched_at) VALUES (?, ?, ?, ?, NULL)",
			uuid.New(), sourceID, nullFetchedAtExternalID, "trello_card",
		).Error; err != nil {
			return fmt.Errorf("seed historical Trello card with NULL fetched_at: %w", err)
		}

		repository := &GormRepository{DB: tx}
		firstPage, err := repository.FindTrelloStaleCardsAfter(sourceID, cycleStartedAt, testTrelloMongoID(10), 2)
		if err != nil {
			return fmt.Errorf("query first stale-card page: %w", err)
		}
		if got, want := trelloPostgresExternalIDs(firstPage), []string{
			trelloCardExternalIDPrefix + testTrelloMongoID(20),
			trelloCardExternalIDPrefix + testTrelloMongoID(30),
		}; !equalTrelloPostgresStrings(got, want) {
			return fmt.Errorf("first stale-card page = %v, want %v", got, want)
		}

		secondPage, err := repository.FindTrelloStaleCardsAfter(sourceID, cycleStartedAt, testTrelloMongoID(30), 2)
		if err != nil {
			return fmt.Errorf("query next stale-card page: %w", err)
		}
		if got, want := trelloPostgresExternalIDs(secondPage), []string{
			nullFetchedAtExternalID,
			trelloCardExternalIDPrefix + testTrelloMongoID(40),
		}; !equalTrelloPostgresStrings(got, want) {
			return fmt.Errorf("next stale-card page = %v, want %v", got, want)
		}
		return nil
	})
}

func TestTrelloPostgresOwnerManualRequestResumesCancelledCheckpoint(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		repository := &GormRepository{DB: tx}
		now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
		source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
		source.OwnerIdentity = "trello-resume-test-" + uuid.NewString()
		previousID := uuid.New()
		resumableID := uuid.New()
		completedAt := now.Add(-time.Minute)
		previous := models.SourceSyncJob{
			ID: previousID, SourceID: source.ID, OwnerIdentity: source.OwnerIdentity,
			IdempotencyKeyHash: strings.Repeat("a", 64), RequestHash: strings.Repeat("c", 64),
			Mode: ModeManualAsyncSync, Status: "cancelled", CompletedAt: &completedAt,
		}
		resumable := models.SourceSyncJob{
			ID: resumableID, SourceID: source.ID, OwnerIdentity: source.OwnerIdentity,
			IdempotencyKeyHash: strings.Repeat("b", 64), RequestHash: strings.Repeat("d", 64),
			Mode: ModeManualAsyncSync, Status: "queued", CreatedAt: now, UpdatedAt: now,
		}
		state := models.TrelloSyncState{
			SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
			LogicalJobID: trelloUUIDPointer(previousID), Generation: 4, Phase: trelloPhaseBackfillCards,
			CardCursor: testTrelloMongoID(900), CycleStartedAt: now.Add(-time.Hour),
			PagesProcessed: 7, RecordsProcessed: 900,
		}
		for _, value := range []any{&source, &previous, &resumable, &state} {
			if err := tx.Create(value).Error; err != nil {
				return fmt.Errorf("create cancelled-checkpoint resume fixture %T: %w", value, err)
			}
		}

		visible, err := repository.FindTrelloSyncState(source.ID)
		if err != nil {
			return fmt.Errorf("load checkpoint for owner-authorized resume: %w", err)
		}
		if visible.LogicalJobID == nil || *visible.LogicalJobID != resumableID {
			return fmt.Errorf("service-visible checkpoint job = %v, want queued owner job %s", visible.LogicalJobID, resumableID)
		}
		var persisted models.TrelloSyncState
		if err := tx.Where("source_id = ?", source.ID).First(&persisted).Error; err != nil {
			return fmt.Errorf("reload persisted checkpoint after resume preflight: %w", err)
		}
		if persisted.LogicalJobID == nil || *persisted.LogicalJobID != previousID {
			return fmt.Errorf("resume preflight changed the persisted job binding to %v", persisted.LogicalJobID)
		}
		started, err := repository.StartManualSyncJob(resumableID, source.ID, now)
		if err != nil {
			return fmt.Errorf("start owner-authorized resume job: %w", err)
		}
		prepared, err := repository.PrepareTrelloSyncState(source, started, now)
		if err != nil {
			return fmt.Errorf("rebind terminal checkpoint to owner-authorized resume: %w", err)
		}
		if prepared.LogicalJobID == nil || *prepared.LogicalJobID != resumableID ||
			prepared.Generation != state.Generation || prepared.Phase != state.Phase ||
			prepared.CardCursor != state.CardCursor || prepared.PagesProcessed != state.PagesProcessed ||
			prepared.RecordsProcessed != state.RecordsProcessed {
			return fmt.Errorf("resumed checkpoint = %#v; want same saved page progress bound to job %s", prepared, resumableID)
		}
		return nil
	})
}

func TestTrelloPostgresInventoryCheckpointUpdatesFetchedAtAndCursorAtomically(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		source := newTrelloSource(uuid.New(), "abc123XY", "")
		source.OwnerIdentity = "trello-postgres-test-" + uuid.NewString()
		jobID := uuid.New()
		cycleStartedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		job := &models.SourceSyncJob{
			ID: jobID, SourceID: source.ID, OwnerIdentity: source.OwnerIdentity,
			Mode: ModeManualAsyncSync, Status: "running", StartedAt: cycleStartedAt,
		}
		if err := tx.Create(source).Error; err != nil {
			return fmt.Errorf("create Trello test source: %w", err)
		}
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("create Trello test sync job: %w", err)
		}

		beforeCardID := testTrelloMongoID(10)
		checkedCardID := testTrelloMongoID(20)
		checkedExternalID := trelloCardExternalIDPrefix + checkedCardID
		initialFetchedAt := cycleStartedAt.Add(-time.Hour)
		checkedCard := trelloPostgresRawCard(source.ID, checkedCardID, initialFetchedAt)
		if err := tx.Create(&checkedCard).Error; err != nil {
			return fmt.Errorf("create stale Trello inventory card: %w", err)
		}

		state := models.TrelloSyncState{
			SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
			LogicalJobID: trelloUUIDPointer(job.ID), Generation: 3, Phase: trelloPhaseReconcileInventory,
			CardCursor: beforeCardID, CycleStartedAt: cycleStartedAt, PagesProcessed: 4, RecordsProcessed: 8,
		}
		if err := tx.Create(&state).Error; err != nil {
			return fmt.Errorf("create Trello inventory checkpoint: %w", err)
		}
		current := state
		next := state
		next.CardCursor = checkedCardID
		next.PagesProcessed++
		next.RecordsProcessed++
		page := models.TrelloSyncPage{
			SourceID: source.ID, LogicalJobID: job.ID, Generation: state.Generation,
			Phase: state.Phase, CursorBefore: beforeCardID, CursorAfter: checkedCardID,
			RecordCount: 1, RequestCount: 1, ResponseBytes: 64,
			Fingerprint: strings.Repeat("a", 64),
		}
		repository := &GormRepository{DB: tx}

		badJob := *job
		badJob.ProgressPhase = strings.Repeat("x", 41)
		if _, _, _, err := repository.CommitTrelloSyncPage(
			source, &badJob, &current, &next, page, nil, nil, []string{checkedExternalID}, false,
		); err == nil {
			return errors.New("checkpoint with invalid job data unexpectedly committed")
		}
		var afterFailedCommit models.SourceRawItem
		if err := tx.First(&afterFailedCommit, "source_id = ? AND external_id = ?", source.ID, checkedExternalID).Error; err != nil {
			return fmt.Errorf("reload inventory card after rejected checkpoint: %w", err)
		}
		var stateAfterFailure models.TrelloSyncState
		if err := tx.First(&stateAfterFailure, "source_id = ?", source.ID).Error; err != nil {
			return fmt.Errorf("reload checkpoint after rejected commit: %w", err)
		}
		if !afterFailedCommit.FetchedAt.Equal(initialFetchedAt) || stateAfterFailure.CardCursor != beforeCardID || stateAfterFailure.PagesProcessed != state.PagesProcessed {
			return fmt.Errorf("rejected checkpoint left partial inventory state: fetched_at=%s cursor=%q pages=%d", afterFailedCommit.FetchedAt, stateAfterFailure.CardCursor, stateAfterFailure.PagesProcessed)
		}
		var pageCount int64
		if err := tx.Model(&models.TrelloSyncPage{}).Where("source_id = ? AND logical_job_id = ?", source.ID, job.ID).Count(&pageCount).Error; err != nil {
			return fmt.Errorf("count page ledger after rejected checkpoint: %w", err)
		}
		if pageCount != 0 {
			return fmt.Errorf("rejected checkpoint left %d page-ledger rows, want none", pageCount)
		}

		_, _, committedState, err := repository.CommitTrelloSyncPage(
			source, job, &current, &next, page, nil, nil, []string{checkedExternalID}, false,
		)
		if err != nil {
			return fmt.Errorf("commit Trello inventory checkpoint: %w", err)
		}
		if committedState.CardCursor != checkedCardID || committedState.PagesProcessed != next.PagesProcessed {
			return fmt.Errorf("committed inventory checkpoint = cursor %q pages %d, want cursor %q pages %d", committedState.CardCursor, committedState.PagesProcessed, checkedCardID, next.PagesProcessed)
		}
		var committedCard models.SourceRawItem
		if err := tx.First(&committedCard, "source_id = ? AND external_id = ?", source.ID, checkedExternalID).Error; err != nil {
			return fmt.Errorf("reload card after inventory checkpoint: %w", err)
		}
		if !committedCard.FetchedAt.After(initialFetchedAt) || !committedCard.FetchedAt.Equal(committedState.UpdatedAt) {
			return fmt.Errorf("inventory fetched_at=%s and checkpoint updated_at=%s; want one newer atomic checkpoint timestamp", committedCard.FetchedAt, committedState.UpdatedAt)
		}
		return nil
	})
}

func TestTrelloPostgresRejectsSourcePausedBeforeDurableOperations(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		repository := &GormRepository{DB: tx}
		now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

		inactive := newTrelloSource(uuid.New(), "abc123XY", "")
		inactive.OwnerIdentity = "trello-paused-test-" + uuid.NewString()
		inactive.Status = "paused"
		job := &models.SourceSyncJob{
			ID: uuid.New(), SourceID: inactive.ID, OwnerIdentity: inactive.OwnerIdentity,
			Mode: ModeManualAsyncSync, Status: "queued", StartedAt: now,
		}
		if err := tx.Create(inactive).Error; err != nil {
			return fmt.Errorf("create inactive Trello source: %w", err)
		}
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("create inactive Trello job: %w", err)
		}
		if _, err := repository.PrepareTrelloSyncState(inactive, job, now); !errors.Is(err, ErrTrelloSyncSourceInactive) {
			return fmt.Errorf("prepare paused Trello source error = %v, want ErrTrelloSyncSourceInactive", err)
		}
		if _, err := repository.StartTrelloSyncJob(job.ID, inactive.ID, now); !errors.Is(err, ErrTrelloSyncSourceInactive) {
			return fmt.Errorf("start paused Trello job error = %v, want ErrTrelloSyncSourceInactive", err)
		}

		commitSource := newTrelloSource(uuid.New(), "abc123XY", "")
		commitSource.OwnerIdentity = "trello-paused-commit-test-" + uuid.NewString()
		commitJob := &models.SourceSyncJob{
			ID: uuid.New(), SourceID: commitSource.ID, OwnerIdentity: commitSource.OwnerIdentity,
			Mode: ModeManualAsyncSync, Status: "running", StartedAt: now,
		}
		if err := tx.Create(commitSource).Error; err != nil {
			return fmt.Errorf("create Trello source for commit recheck: %w", err)
		}
		if err := tx.Create(commitJob).Error; err != nil {
			return fmt.Errorf("create Trello job for commit recheck: %w", err)
		}
		state := models.TrelloSyncState{
			SourceID: commitSource.ID, OwnerIdentity: commitSource.OwnerIdentity, BoardID: "abc123XY",
			LogicalJobID: trelloUUIDPointer(commitJob.ID), Generation: 1, Phase: trelloPhaseBackfillCards,
			CycleStartedAt: now, PagesProcessed: 0, RecordsProcessed: 0,
		}
		if err := tx.Create(&state).Error; err != nil {
			return fmt.Errorf("create Trello card checkpoint for commit recheck: %w", err)
		}
		if err := tx.Model(&models.ConnectedSource{}).Where("id = ?", commitSource.ID).Update("status", "paused").Error; err != nil {
			return fmt.Errorf("pause Trello source before page commit: %w", err)
		}
		current := state
		next := state
		next.Phase = trelloPhaseBackfillActions
		next.PagesProcessed++
		page := models.TrelloSyncPage{
			SourceID: commitSource.ID, LogicalJobID: commitJob.ID, Generation: state.Generation,
			Phase: state.Phase, RequestCount: 1, ResponseBytes: 64,
			Fingerprint: strings.Repeat("a", 64),
		}
		if _, _, _, err := repository.CommitTrelloSyncPage(commitSource, commitJob, &current, &next, page, nil, nil, nil, false); !errors.Is(err, ErrTrelloSyncSourceInactive) {
			return fmt.Errorf("commit after source was paused error = %v, want ErrTrelloSyncSourceInactive", err)
		}
		var pageCount int64
		if err := tx.Model(&models.TrelloSyncPage{}).Where("source_id = ?", commitSource.ID).Count(&pageCount).Error; err != nil {
			return fmt.Errorf("count pages after rejected paused-source commit: %w", err)
		}
		if pageCount != 0 {
			return fmt.Errorf("paused source commit persisted %d page rows, want zero", pageCount)
		}
		return nil
	})
}

func trelloPostgresRawCard(sourceID uuid.UUID, cardID string, fetchedAt time.Time) models.SourceRawItem {
	return models.SourceRawItem{
		ID: uuid.New(), SourceID: sourceID, ExternalID: trelloCardExternalIDPrefix + cardID,
		ItemType: "trello_card", Title: "Trello integration fixture", FetchedAt: fetchedAt,
	}
}

func trelloPostgresExternalIDs(items []models.SourceRawItem) []string {
	ids := make([]string, len(items))
	for index := range items {
		ids[index] = items[index].ExternalID
	}
	return ids
}

func equalTrelloPostgresStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

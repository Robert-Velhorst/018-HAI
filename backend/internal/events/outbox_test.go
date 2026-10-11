package events

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestOutboxStatsSerializesEmptyFailuresAsArray(t *testing.T) {
	payload, err := json.Marshal(OutboxStats{RecentFailures: make([]OutboxFailure, 0)})
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}
	if !strings.Contains(string(payload), `"recentFailures":[]`) {
		t.Fatalf("empty failure list payload = %s", payload)
	}
}

type recordingPublisher struct {
	events []*AutomationEvent
	err    error
}

func (p *recordingPublisher) Publish(event *AutomationEvent) error {
	if p.err != nil {
		return p.err
	}
	copied := *event
	p.events = append(p.events, &copied)
	return nil
}

func TestNormalizeAutomationEventCreatesStableEnvelope(t *testing.T) {
	event := &AutomationEvent{Type: CreateEvent, Automation: &models.Automation{ID: uuid.New()}}
	if err := normalizeAutomationEvent(event); err != nil {
		t.Fatalf("normalize event: %v", err)
	}
	if event.ID == uuid.Nil || event.OccurredAt.IsZero() {
		t.Fatalf("event envelope was not completed: %#v", event)
	}
	firstID, firstTime := event.ID, event.OccurredAt
	if err := normalizeAutomationEvent(event); err != nil {
		t.Fatalf("normalize event again: %v", err)
	}
	if event.ID != firstID || !event.OccurredAt.Equal(firstTime) {
		t.Fatalf("event envelope changed during retry: %#v", event)
	}
}

func TestOutboxRetryDelayIsBounded(t *testing.T) {
	if got := outboxRetryDelay(1); got != time.Second {
		t.Fatalf("first retry delay = %s", got)
	}
	if got := outboxRetryDelay(100); got != maximumOutboxRetry {
		t.Fatalf("maximum retry delay = %s, want %s", got, maximumOutboxRetry)
	}
}

func TestPostgresOutboxCommitDispatchAndRollback(t *testing.T) {
	db := outboxPostgresDB(t)
	store := NewOutboxStore(db)
	automationID := uuid.New()
	event := &AutomationEvent{Type: CreateEvent, Automation: &models.Automation{ID: automationID}}
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueTx(tx, event) }); err != nil {
		t.Fatalf("enqueue committed event: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id = ?", automationID) })

	publisher := &recordingPublisher{}
	dispatcher := NewOutboxDispatcher(store, publisher)
	processed, err := dispatcher.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch event: %v", err)
	}
	if processed != 1 || len(publisher.events) != 1 || publisher.events[0].ID != event.ID {
		t.Fatalf("dispatch result = processed %d events %#v", processed, publisher.events)
	}
	var status string
	if err := db.Raw("SELECT status FROM automation_event_outbox WHERE id = ?", event.ID).Scan(&status).Error; err != nil {
		t.Fatalf("read event status: %v", err)
	}
	if status != string(OutboxPublished) {
		t.Fatalf("event status = %q", status)
	}

	rolledBackID := uuid.New()
	rollbackEvent := &AutomationEvent{Type: DeleteEvent, Automation: &models.Automation{ID: rolledBackID}}
	expected := errors.New("rollback fixture")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := EnqueueTx(tx, rollbackEvent); err != nil {
			return err
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("rollback error = %v", err)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM automation_event_outbox WHERE aggregate_id = ?", rolledBackID).Scan(&count).Error; err != nil {
		t.Fatalf("count rolled back event: %v", err)
	}
	if count != 0 {
		t.Fatalf("rolled back event count = %d", count)
	}
}

func TestPostgresOutboxRetriesPublisherFailure(t *testing.T) {
	db := outboxPostgresDB(t)
	store := NewOutboxStore(db)
	automationID := uuid.New()
	event := &AutomationEvent{Type: UpdateEvent, Automation: &models.Automation{ID: automationID}}
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueTx(tx, event) }); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id = ?", automationID) })

	dispatcher := NewOutboxDispatcher(store, &recordingPublisher{err: errors.New("broker unavailable")})
	if _, err := dispatcher.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("publisher failure should be recorded, not abort dispatch: %v", err)
	}
	var record OutboxRecord
	if err := db.Raw("SELECT * FROM automation_event_outbox WHERE id = ?", event.ID).Scan(&record).Error; err != nil {
		t.Fatalf("read retried event: %v", err)
	}
	if record.Status != OutboxPending || record.AttemptCount != 1 || record.LastError == "" || !record.NextAttemptAt.After(record.UpdatedAt) {
		t.Fatalf("retry state = %#v", record)
	}
}

func TestPostgresOutboxExpiresAbandonedFinalLease(t *testing.T) {
	db := outboxPostgresDB(t)
	store := NewOutboxStore(db)
	automationID := uuid.New()
	event := &AutomationEvent{Type: UpdateEvent, Automation: &models.Automation{ID: automationID}}
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueTx(tx, event) }); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id = ?", automationID) })

	expiredAt := time.Now().Add(-time.Minute).UTC()
	if err := db.Exec(`
		UPDATE automation_event_outbox
		SET attempt_count = max_attempts, lease_token = ?, lease_until = ?
		WHERE id = ?
	`, uuid.New(), expiredAt, event.ID).Error; err != nil {
		t.Fatalf("prepare abandoned lease: %v", err)
	}
	records, err := store.Claim(context.Background(), uuid.New(), time.Now(), 1)
	if err != nil {
		t.Fatalf("claim after abandoned final lease: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("abandoned final attempt was reclaimed: %#v", records)
	}
	var record OutboxRecord
	if err := db.Raw("SELECT * FROM automation_event_outbox WHERE id = ?", event.ID).Scan(&record).Error; err != nil {
		t.Fatalf("read dead letter: %v", err)
	}
	if record.Status != OutboxDeadLettered || record.LastError == "" {
		t.Fatalf("abandoned final attempt state = %#v", record)
	}
}

func TestPostgresOutboxStatsRetryAndPrune(t *testing.T) {
	db := outboxPostgresDB(t)
	store := NewOutboxStore(db)
	automationID := uuid.New()
	event := &AutomationEvent{Type: DeleteEvent, Automation: &models.Automation{ID: automationID}}
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueTx(tx, event) }); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id = ?", automationID) })

	now := time.Now().UTC()
	if err := db.Exec(`
		UPDATE automation_event_outbox
		SET status = 'dead_lettered', attempt_count = max_attempts,
			last_error = 'broker unavailable', updated_at = ?
		WHERE id = ?
	`, now, event.ID).Error; err != nil {
		t.Fatalf("prepare dead letter: %v", err)
	}
	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("read stats: %v", err)
	}
	if stats.DeadLettered < 1 || stats.CheckedAt.IsZero() || len(stats.RecentFailures) == 0 {
		t.Fatalf("stats omitted dead-letter detail: %#v", stats)
	}
	if got := stats.RecentFailures[0]; got.ID != event.ID || got.LastError == "" {
		t.Fatalf("recent failure = %#v", got)
	}

	if err := store.RetryDeadLetter(context.Background(), event.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("retry dead letter: %v", err)
	}
	var retried OutboxRecord
	if err := db.Raw("SELECT * FROM automation_event_outbox WHERE id = ?", event.ID).Scan(&retried).Error; err != nil {
		t.Fatalf("read retried event: %v", err)
	}
	if retried.Status != OutboxPending || retried.AttemptCount != 0 || retried.LastError != "" {
		t.Fatalf("retried state = %#v", retried)
	}

	oldPublishedAt := now.Add(-defaultOutboxRetention - time.Hour)
	if err := db.Exec(`
		UPDATE automation_event_outbox
		SET status = 'published', published_at = ?, updated_at = ?
		WHERE id = ?
	`, oldPublishedAt, oldPublishedAt, event.ID).Error; err != nil {
		t.Fatalf("prepare old published row: %v", err)
	}
	pruned, err := store.PrunePublished(context.Background(), now.Add(-defaultOutboxRetention), 10)
	if err != nil {
		t.Fatalf("prune published rows: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned rows = %d, want 1", pruned)
	}
}

func outboxPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return db
}

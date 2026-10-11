package automation

import (
	"errors"
	"os"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresAutomationMutationsCommitOutboxAtomically(t *testing.T) {
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

	repo := &GormUserRepository{DB: db}
	automationID := uuid.New()
	maxPosition, err := repo.MaxPosition()
	if err != nil {
		t.Fatalf("read maximum automation position: %v", err)
	}
	position := maxPosition + 1
	item := &models.Automation{
		ID: automationID, Name: "Outbox " + automationID.String(),
		URLPath: "outbox-" + automationID.String(), Host: "backend", Port: 80,
		Position: position,
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id IN ?", []uuid.UUID{automationID})
		db.Unscoped().Delete(&models.Automation{}, automationID)
	})

	createEvent := &events.AutomationEvent{Type: events.CreateEvent, Automation: item}
	if _, err := repo.CreateWithEvent(item, createEvent); err != nil {
		t.Fatalf("create automation with outbox event: %v", err)
	}

	item.Name = "Updated " + automationID.String()
	item.URLPath = "updated-" + automationID.String()
	item.OldUrlPath = "outbox-" + automationID.String()
	if _, err := repo.UpdateWithEvent(item, &events.AutomationEvent{Type: events.UpdateEvent, Automation: item}); err != nil {
		t.Fatalf("update automation with outbox event: %v", err)
	}
	if err := repo.DeleteWithEvent(automationID, item, &events.AutomationEvent{Type: events.DeleteEvent, Automation: item}); err != nil {
		t.Fatalf("delete automation with outbox event: %v", err)
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM automation_event_outbox WHERE aggregate_id = ?", automationID).Scan(&count).Error; err != nil {
		t.Fatalf("count CRUD events: %v", err)
	}
	if count != 3 {
		t.Fatalf("outbox rows = %d, want one event per committed mutation", count)
	}

	// Reusing an existing event UUID forces the enqueue step to fail after the
	// automation INSERT. The transaction must roll that INSERT back as well.
	conflictEventID := uuid.New()
	conflictAggregateID := uuid.New()
	seedEvent := &events.AutomationEvent{
		ID: conflictEventID, Type: events.CreateEvent,
		Automation: &models.Automation{ID: conflictAggregateID},
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return events.EnqueueTx(tx, seedEvent) }); err != nil {
		t.Fatalf("seed duplicate event id: %v", err)
	}
	rollbackID := uuid.New()
	rollbackItem := &models.Automation{
		ID: rollbackID, Name: "Rollback " + rollbackID.String(),
		URLPath: "rollback-" + rollbackID.String(), Host: "backend", Port: 80,
		Position: position + 1,
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM automation_event_outbox WHERE aggregate_id = ?", conflictAggregateID)
		db.Unscoped().Delete(&models.Automation{}, rollbackID)
	})
	err = func() error {
		_, createErr := repo.CreateWithEvent(rollbackItem, &events.AutomationEvent{
			ID: conflictEventID, Type: events.CreateEvent, Automation: rollbackItem,
		})
		return createErr
	}()
	if err == nil {
		t.Fatal("expected duplicate outbox event id to fail the transaction")
	}
	var persisted models.Automation
	lookupErr := db.First(&persisted, "id = ?", rollbackID).Error
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		t.Fatalf("automation survived failed outbox enqueue: lookup error %v", lookupErr)
	}
}

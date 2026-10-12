package retention

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func mem(archived bool, updatedDaysAgo int, now time.Time) models.ContextMemory {
	return models.ContextMemory{Archived: archived, UpdatedAt: now.AddDate(0, 0, -updatedDaysAgo)}
}

func TestDueForArchival(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	p := DefaultPolicy() // archive after 180d
	memories := []models.ContextMemory{
		mem(false, 200, now), // due
		mem(false, 10, now),  // too recent
		mem(true, 200, now),  // already archived — not for archival
	}
	due := DueForArchival(memories, p, now)
	if len(due) != 1 || due[0].UpdatedAt != now.AddDate(0, 0, -200) {
		t.Fatalf("archival due = %d, want the 200-day live memory", len(due))
	}
}

func TestDueForArchivalUsesMostRecentActivity(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	memory := mem(false, 200, now)
	lastUsed := now.AddDate(0, 0, -10)
	memory.LastUsedAt = &lastUsed

	if due := DueForArchival([]models.ContextMemory{memory}, DefaultPolicy(), now); len(due) != 0 {
		t.Fatalf("recently used memory was marked for archival: %#v", due)
	}
}

func TestDueForDeletion(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	p := DefaultPolicy() // delete archived after 365d
	memories := []models.ContextMemory{
		mem(true, 400, now),  // due
		mem(true, 100, now),  // too recent
		mem(false, 400, now), // live — not for deletion
	}
	due := DueForDeletion(memories, p, now)
	if len(due) != 1 {
		t.Fatalf("deletion due = %d, want 1", len(due))
	}
}

func TestDisabledStagesReturnNothing(t *testing.T) {
	now := time.Now()
	memories := []models.ContextMemory{mem(false, 9999, now), mem(true, 9999, now)}
	if DueForArchival(memories, Policy{ArchiveAfterDays: 0}, now) != nil {
		t.Fatalf("zero archival threshold should disable archival")
	}
	if DueForDeletion(memories, Policy{DeleteAfterDays: 0}, now) != nil {
		t.Fatalf("zero deletion threshold should disable deletion")
	}
}

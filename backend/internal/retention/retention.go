// Package retention implements a pure, policy-driven data-retention evaluator.
// It decides which context memories are due for archival or deletion based on
// age, without performing any I/O — the caller applies the returned decisions.
package retention

import (
	"time"

	"automation-hub-backend/internal/models"
)

// Policy describes retention thresholds in days. A non-positive threshold
// disables that stage.
type Policy struct {
	ArchiveAfterDays int `json:"archiveAfterDays"` // inactive live memories older than this are due for archival
	DeleteAfterDays  int `json:"deleteAfterDays"`  // archived memories older than this are due for deletion
}

// DefaultPolicy is a conservative default: archive after 180 days of inactivity,
// delete archived memories after a further year.
func DefaultPolicy() Policy {
	return Policy{ArchiveAfterDays: 180, DeleteAfterDays: 365}
}

// DueForArchival returns live (non-archived) memories whose most recent update
// or use is older than the archival threshold.
func DueForArchival(memories []models.ContextMemory, policy Policy, now time.Time) []models.ContextMemory {
	if policy.ArchiveAfterDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -policy.ArchiveAfterDays)
	var due []models.ContextMemory
	for _, m := range memories {
		lastActivity := m.UpdatedAt
		if m.LastUsedAt != nil && m.LastUsedAt.After(lastActivity) {
			lastActivity = *m.LastUsedAt
		}
		if !m.Archived && lastActivity.Before(cutoff) {
			due = append(due, m)
		}
	}
	return due
}

// DueForDeletion returns archived memories whose last update is older than the
// deletion threshold.
func DueForDeletion(memories []models.ContextMemory, policy Policy, now time.Time) []models.ContextMemory {
	if policy.DeleteAfterDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -policy.DeleteAfterDays)
	var due []models.ContextMemory
	for _, m := range memories {
		if m.Archived && m.UpdatedAt.Before(cutoff) {
			due = append(due, m)
		}
	}
	return due
}

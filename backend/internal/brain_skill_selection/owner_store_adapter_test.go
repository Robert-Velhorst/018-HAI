package brain_skill_selection

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/brainskills"
)

func TestOwnerSelectionBatchFetchesOnceAndPreservesLatestDecisionMetadata(t *testing.T) {
	service, repository := testService()
	ctx := context.Background()
	owner := "owner-batch-test"
	actor := "authenticated-actor@example.test"
	enabledSkill := catalogSkill(t, "frontend-design")
	enabledEvent, err := service.Set(ctx, owner, actor, enabledSkill.ID, true, currentCatalogFingerprint())
	if err != nil {
		t.Fatalf("append current decision: %v", err)
	}
	decidedAt := time.Date(2026, 9, 24, 13, 14, 15, 0, time.UTC)
	staleEvent, err := repository.Append(ctx, SelectionEvent{
		OwnerIdentity:  owner,
		ActorIdentity:  "authenticated-actor@example.test",
		SkillID:        "mcp-builder",
		SourceCommit:   strings.Repeat("a", 40),
		SourceSHA256:   strings.Repeat("b", 64),
		GuidanceSHA256: strings.Repeat("c", 64),
		Enabled:        true,
		DecidedAt:      decidedAt,
	})
	if err != nil {
		t.Fatalf("append stale decision: %v", err)
	}

	catalog := brainskills.DefaultCatalog().List()
	states, err := NewOwnerSelectionStoreAdapter(service).StatesForOwner(ctx, owner, catalog)
	if err != nil {
		t.Fatalf("batch selection lookup: %v", err)
	}
	if len(repository.owners) != 1 || repository.owners[0] != owner {
		t.Fatalf("repository owner lookups = %v, want exactly one lookup for %q", repository.owners, owner)
	}
	if len(states) != len(catalog) {
		t.Fatalf("batch state count = %d, want full catalog count %d", len(states), len(catalog))
	}

	current := states[enabledSkill.ID]
	if !current.Enabled || current.NeedsReapproval || current.Decision == nil ||
		current.Decision.ID != enabledEvent.ID || current.Decision.ActorIdentity != actor ||
		!current.Decision.DecidedAt.Equal(enabledEvent.DecidedAt) {
		t.Fatalf("current decision was not mapped correctly: %#v", current)
	}
	stale := states["mcp-builder"]
	if stale.Enabled || !stale.NeedsReapproval || stale.Decision == nil ||
		stale.Decision.ID != staleEvent.ID || stale.Decision.ActorIdentity != staleEvent.ActorIdentity ||
		!stale.Decision.DecidedAt.Equal(decidedAt) {
		t.Fatalf("stale selection was not disabled or its latest metadata was lost: %#v", stale)
	}
	if unselected := states["webapp-testing"]; unselected.Enabled || unselected.NeedsReapproval || unselected.Decision != nil {
		t.Fatalf("unselected skill state = %#v", unselected)
	}
}

func TestOwnerSelectionBatchRejectsCatalogMismatchBeforeReadingOwnerState(t *testing.T) {
	service, repository := testService()
	skill := catalogSkill(t, "frontend-design")
	skill.SourceSHA256 = strings.Repeat("f", 64)
	_, err := NewOwnerSelectionStoreAdapter(service).StatesForOwner(context.Background(), "owner-a", []brainskills.Skill{skill})
	if err == nil {
		t.Fatal("batch accepted a skill whose catalog metadata did not match the pinned server entry")
	}
	if len(repository.owners) != 0 {
		t.Fatalf("catalog mismatch reached owner-scoped repository: %v", repository.owners)
	}
}

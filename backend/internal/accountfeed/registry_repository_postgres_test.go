package accountfeed

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/schema"
)

func TestRegistryPostgresModelColumnsMatchMigration(t *testing.T) {
	for _, tc := range []struct {
		model   any
		table   string
		columns []string
	}{
		{&accountFeedRow{}, "account_feeds", []string{"id", "owner_user_id", "workspace_id", "name", "provider", "account_label", "source_type", "path", "url", "project_key", "operation_type", "enabled", "config_version", "last_attempt_at", "last_success_at", "last_items_read", "sync_token", "sync_started_at"}},
		{&accountFeedAuditRow{}, "account_feed_audits", []string{"id", "feed_id", "owner_user_id", "workspace_id", "event_type", "message", "created_at"}},
	} {
		model, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if model.Table != tc.table {
			t.Fatalf("table=%s", model.Table)
		}
		if len(model.DBNames) != len(tc.columns) {
			t.Fatalf("unexpected columns %v", model.DBNames)
		}
		for _, name := range tc.columns {
			if model.LookUpField(name) == nil {
				t.Errorf("missing database mapping %s.%s", tc.table, name)
			}
		}
		if len(model.PrimaryFields) != 1 || model.PrimaryFields[0].DBName != "id" {
			t.Fatalf("primary key mapping=%v", model.PrimaryFields)
		}
	}
}

func TestConfiguredFeedIDStableAndScopeSeparated(t *testing.T) {
	first := ConfiguredFeedID("owner", "local", "feed.json")
	if first == uuid.Nil || first != ConfiguredFeedID(" owner ", " local ", " feed.json ") {
		t.Fatal("bootstrap identity must be stable")
	}
	for _, other := range []uuid.UUID{ConfiguredFeedID("other", "local", "feed.json"), ConfiguredFeedID("owner", "other", "feed.json"), ConfiguredFeedID("owner", "local", "other.json"), ConfiguredFeedID("owner|local", "", "feed.json")} {
		if first == other {
			t.Fatal("bootstrap identities collided across scope/path")
		}
	}
	if ConfiguredFeedID("owner", "", "feed.json") != first {
		t.Fatal("default workspace identity differs")
	}
}

// This is a wiring contract, not a live startup or database acceptance test.
func TestProductionRouterUsesDurableAccountFeedsWithoutIgnoredSeeds(t *testing.T) {
	body, err := os.ReadFile("../router/routes.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{"accountfeed.NewPostgresRegistry(runtimeCtx", "seedAccountFeeds(runtimeCtx, feedRegistry, phase2Module)", "phase2Module.WithFeedRegistry(feedRegistry)", "reg.RegisterContext(ctx", "accountfeed.ConfiguredFeedID(m.OwnerUserID(), m.WorkspaceID(), name)", "initialize durable account feeds", "seed durable account feeds"} {
		if !strings.Contains(text, required) {
			t.Errorf("production composition missing %q", required)
		}
	}
	for _, forbidden := range []string{"feedRegistry := accountfeed.NewRegistry(", "_, _ = reg.Register("} {
		if strings.Contains(text, forbidden) {
			t.Errorf("volatile/ignored-error composition remains: %s", forbidden)
		}
	}
}

func TestContextAPIRejectsMissingOwnerOrWorkspaceBeforeStoreAccess(t *testing.T) {
	reg := NewRegistry(nil, nil, FetchOptions{})
	for _, scope := range []FeedScope{{}, {OwnerUserID: "owner"}, {WorkspaceID: "local"}, {OwnerUserID: " ", WorkspaceID: "local"}} {
		if _, err := reg.ListContext(context.Background(), scope); err != ErrFeedInvalid {
			t.Fatalf("incomplete scope accepted: %+v err=%v", scope, err)
		}
	}
}

func TestPersistedUnknownProviderIsNotAdvertisedOrFetched(t *testing.T) {
	store := NewMemoryRegistryRepository()
	feed := persistenceSeed()
	feed.Provider = "unrecognized_provider"
	// Model an incompatible/manual persisted row rather than bypassing the
	// service registration validator through its public API.
	if _, err := store.Register(t.Context(), feed, newFeedAudit(feed.ID, "registered", "synthetic incompatible record", time.Now())); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistryWithRepository(store, nil, nil, FetchOptions{FeedsRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	health, err := reg.ListContext(t.Context(), feedScope(feed))
	if err != nil || len(health) != 1 || health[0].ConnectionStatus == ConnAvailable {
		t.Fatalf("unknown provider advertised as available: %+v %v", health, err)
	}
	report, err := reg.SyncContext(t.Context(), feedScope(feed), feed.ID)
	if err != nil || !report.Recorded || report.ItemsRead != 0 || len(report.Errors) != 1 || report.Errors[0] != "stored feed provider requires operator review" {
		t.Fatalf("incompatible provider reached fetch: %+v %v", report, err)
	}
}

package router

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/phase2"
)

func TestAccountFeedBootstrapPreservesOperatorConfiguration(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	module := phase2.NewModule(service, phase2.Config{OwnerUserID: "owner", WorkspaceID: "workspace", FeedFiles: []string{"inbox.json", " inbox.json ", "", "tasks.json"}})
	registry := accountfeed.NewRegistry(service, nil, accountfeed.FetchOptions{})
	if err := seedAccountFeeds(context.Background(), registry, module); err != nil {
		t.Fatal(err)
	}
	scope := accountfeed.FeedScope{OwnerUserID: "owner", WorkspaceID: "workspace"}
	rows, err := registry.ListContext(t.Context(), scope)
	if err != nil || len(rows) != 2 {
		t.Fatalf("bootstrap rows=%+v err=%v", rows, err)
	}
	id := accountfeed.ConfiguredFeedID("owner", "workspace", "inbox.json")
	disabled := false
	name := "Operator renamed inbox"
	if _, err := registry.PatchContext(t.Context(), scope, id, accountfeed.FeedPatch{Enabled: &disabled, Name: &name}); err != nil {
		t.Fatal(err)
	}
	if err := seedAccountFeeds(t.Context(), registry, module); err != nil {
		t.Fatal(err)
	}
	feed, err := registry.GetContext(t.Context(), scope, id)
	if err != nil || feed.Enabled || feed.Name != name {
		t.Fatalf("reseed replaced operator values: %+v %v", feed, err)
	}
	audit, err := registry.AuditContext(t.Context(), scope, id)
	if err != nil || len(audit) != 2 {
		t.Fatalf("reseed duplicated audit: %+v %v", audit, err)
	}
}

func TestAccountFeedBootstrapPropagatesUnavailableStorage(t *testing.T) {
	registry, err := accountfeed.NewRegistryWithRepository(accountfeed.NewGormRegistryRepository(nil), nil, nil, accountfeed.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	module := phase2.NewModule(operations.NewService(operations.NewMemoryRepository()), phase2.Config{FeedFiles: []string{"inbox.json"}})
	if err := seedAccountFeeds(t.Context(), registry, module); !errors.Is(err, accountfeed.ErrFeedStorageUnavailable) {
		t.Fatalf("storage failure swallowed: %v", err)
	}
}

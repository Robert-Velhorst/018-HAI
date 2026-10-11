package phase2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/operations"
)

func TestModuleUsesCanonicalRegistryForConfiguredAndOwnerPasses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "canonical.json"), []byte(`{"items":[{"externalId":"one","title":"Review source","content":"source record","itemType":"email","provider":"gmail"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := operations.NewService(operations.NewMemoryRepository())
	registry := accountfeed.NewRegistry(service, nil, accountfeed.FetchOptions{FeedsRoot: root})
	feed, err := registry.RegisterContext(t.Context(), accountfeed.Feed{
		Name: "canonical", Provider: "generic_json_feed", SourceType: accountfeed.SourceLocalJSONFile,
		Path: "canonical.json", OwnerUserID: "owner", WorkspaceID: "local", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	module := NewModule(service, Config{OwnerUserID: "owner", WorkspaceID: "local", FeedsDir: root,
		FeedFiles: []string{"missing-cached-reader.json"}, Mode: autonomypolicy.ModeReadOnly}).WithFeedRegistry(registry)
	first, err := module.RunConfiguredBackground(t.Context())
	if err != nil || first.FeedsRead != 1 || first.ItemsIngested != 1 || first.OperationsCreated != 1 {
		t.Fatalf("configured pass did not use canonical registry: %+v / %v", first, err)
	}
	other, err := module.RunBackgroundForOwner(t.Context(), "another-owner")
	if err != nil || other.FeedsRead != 0 || other.ItemsIngested != 0 || other.Classified != 0 {
		t.Fatalf("owner pass reused configured-owner feeds: %+v / %v", other, err)
	}
	disabled := false
	if _, err := registry.PatchContext(t.Context(), accountfeed.FeedScope{OwnerUserID: "owner", WorkspaceID: "local"}, feed.ID, accountfeed.FeedPatch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func() (int, error){
		func() (int, error) {
			rep, err := module.RunConfiguredBackground(t.Context())
			return rep.FeedsRead, err
		},
		func() (int, error) {
			rep, err := module.RunBackgroundForOwner(t.Context(), "owner")
			return rep.FeedsRead, err
		},
	} {
		if reads, err := run(); err != nil || reads != 0 {
			t.Fatalf("disabled registry fell back to cached reader: reads=%d / %v", reads, err)
		}
	}
}

type unavailableCanonicalRegistry struct {
	*accountfeed.MemoryRegistryRepository
}

func (r *unavailableCanonicalRegistry) List(context.Context, accountfeed.FeedScope) ([]accountfeed.FeedRecord, error) {
	return nil, errors.New("synthetic unavailable registry password=not-for-public")
}

func TestModuleRegistryFailureStopsBeforeProcessingOrCachedReaderFallback(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	input := operations.NewOperationInput{OwnerUserID: "owner", WorkspaceID: "local", Title: "Organize local notes",
		Description: "Sort local notes", OperationType: "review_source_item", SourceType: "manual", DedupeKey: "existing"}
	created, err := service.IngestContext(t.Context(), input)
	if err != nil || !created.Created {
		t.Fatalf("seed stored work: %+v / %v", created, err)
	}
	beforeEvents, err := service.Events(created.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := accountfeed.NewRegistryWithRepository(&unavailableCanonicalRegistry{accountfeed.NewMemoryRegistryRepository()}, service, nil, accountfeed.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	module := NewModule(service, Config{OwnerUserID: "owner", WorkspaceID: "local", FeedsDir: t.TempDir(),
		FeedFiles: []string{"must-not-fallback.json"}, Mode: autonomypolicy.ModeReadOnly}).WithFeedRegistry(registry)
	for _, run := range []func() (background.Report, error){
		func() (background.Report, error) { return module.RunConfiguredBackground(t.Context()) },
		func() (background.Report, error) { return module.RunBackgroundForOwner(t.Context(), "owner") },
	} {
		report, err := run()
		if !errors.Is(err, background.ErrReportedFailures) || len(report.Errors) != 1 || report.FeedsRead != 0 || report.ItemsIngested != 0 ||
			report.Classified != 0 || report.AutoExecuted != 0 || report.Observed != 0 || report.Drafted != 0 {
			t.Fatalf("registry failure fell through to processing or cached readers: %+v / %v", report, err)
		}
		if strings.Contains(strings.Join(report.Errors, " ")+err.Error(), "not-for-public") {
			t.Fatal("registry storage error exposed private diagnostics")
		}
		stored, err := service.Get("owner", "local", created.Operation.ID)
		if err != nil || stored == nil || !reflect.DeepEqual(*stored, created.Operation) {
			t.Fatalf("failed source verification changed stored work: %+v / %v", stored, err)
		}
		events, err := service.Events(created.Operation.ID)
		if err != nil || !reflect.DeepEqual(events, beforeEvents) {
			t.Fatalf("failed source verification rewrote work audit: %+v / %v", events, err)
		}
	}
}

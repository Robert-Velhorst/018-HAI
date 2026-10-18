package accountfeed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"automation-hub-backend/internal/operations"
)

func TestAutoposterConfigurationStartsDisabled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		if req.URL.Path == "/api/hai/status" {
			_, _ = w.Write([]byte(`{"status":"ok","connector":"secondhand-platforms-autoposter","owner":"42","mode":"read_only_pull","write_back":false}`))
			return
		}
		writeTestPage(w, []autoposterRecord{}, "", false)
	}))
	defer server.Close()
	ops := operations.NewService(operations.NewMemoryRepository())
	reg := NewRegistry(ops, nil, FetchOptions{Autoposter: &AutoposterConnection{
		BaseURL: server.URL, Token: syntheticAutoposterToken, RemoteOwner: "42", OwnerUserID: "owner", WorkspaceID: "local",
	}})
	feed, err := reg.ConfigureAutoposter(context.Background())
	if err != nil || feed.Enabled {
		t.Fatalf("Autoposter must be registered disabled: enabled=%t err=%v", feed.Enabled, err)
	}
	report, ok := reg.Sync(context.Background(), feed.ID)
	if !ok || report.ItemsRead != 0 || requests.Load() != 0 {
		t.Fatalf("disabled source was contacted: requests=%d report=%+v", requests.Load(), report)
	}
	enabled := true
	if _, err := reg.PatchContext(context.Background(), feedScope(feed), feed.ID, FeedPatch{Enabled: &enabled}); err != nil {
		t.Fatal("explicit source enable failed")
	}
	report, ok = reg.Sync(context.Background(), feed.ID)
	if !ok || len(report.Errors) != 0 || requests.Load() != 2 { // status and records endpoints
		t.Fatalf("explicit enable did not permit read-only sync: requests=%d report=%+v", requests.Load(), report)
	}
}

func TestAutoposterDisabledSourceIsNotContacted(t *testing.T) {
	var requests atomic.Int32
	reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeTestPage(w, []autoposterRecord{}, "", false)
	})
	disabled := false
	if _, ok := reg.Patch(feed.ID, &disabled, nil, nil); !ok {
		t.Fatal("disable failed")
	}
	report, ok := reg.Sync(context.Background(), feed.ID)
	if !ok || report.ItemsRead != 0 || requests.Load() != 0 {
		t.Fatalf("disabled source was contacted or processed: requests=%d report=%+v", requests.Load(), report)
	}
}

func TestAutoposterDisableFencesFetchedPage(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var recordsRequests atomic.Int32
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		if recordsRequests.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
		}
		writeTestPage(w, []autoposterRecord{testRecord("1", "Chair", "2026-09-13T10:00:00Z", false)}, "MQ", false)
	})
	enabled := true
	if _, ok := reg.Patch(feed.ID, &enabled, nil, nil); !ok {
		t.Fatal("explicit source enable failed")
	}
	done := make(chan SyncReport, 1)
	go func() {
		report, _ := reg.Sync(context.Background(), feed.ID)
		done <- report
	}()
	<-started
	disabled := false
	if _, ok := reg.Patch(feed.ID, &disabled, nil, nil); !ok {
		t.Fatal("source disable failed")
	}
	close(release)
	report := <-done
	rows, err := ops.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	checkpoint, checkpointErr := ops.LoadReferenceCheckpoint(context.Background(), referenceKey(feed))
	if len(report.Errors) == 0 || report.ItemsRead != 0 || len(rows) != 0 || err != nil || checkpointErr != nil || checkpoint != (operations.ReferenceCheckpoint{}) {
		t.Fatalf("page fetched before disable crossed the durable control fence: report=%+v rows=%d checkpoint=%+v err=%v/%v", report, len(rows), checkpoint, err, checkpointErr)
	}
}

func TestAutoposterControlSurvivesRegistryRecreation(t *testing.T) {
	var requests atomic.Int32
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeTestPage(w, []autoposterRecord{}, "", false)
	})
	disabled := false
	if _, ok := reg.Patch(feed.ID, &disabled, nil, nil); !ok {
		t.Fatal("disable failed")
	}
	restarted, err := NewRegistryWithRepository(reg.repo, ops, nil, reg.opts)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := restarted.ConfigureAutoposter(context.Background())
	if err != nil || configured.Enabled {
		t.Fatalf("restart re-enabled the disabled source: enabled=%t err=%v", configured.Enabled, err)
	}
	if report, ok := restarted.Sync(context.Background(), feed.ID); !ok || report.ItemsRead != 0 || requests.Load() != 0 {
		t.Fatalf("disabled source synced after registry recreation: ok=%t requests=%d report=%+v", ok, requests.Load(), report)
	}
}

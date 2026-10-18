package accountfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
	"github.com/gin-gonic/gin"
)

const syntheticAutoposterToken = "hai_synthetic_test_token_not_a_real_credential"

func testAutoposter(t *testing.T, pages http.HandlerFunc) (*Registry, Feed, *operations.Service) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer "+syntheticAutoposterToken {
			t.Error("request must be read-only and authenticated")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if req.URL.Path == "/api/hai/status" {
			fmt.Fprint(w, `{"status":"ok","connector":"secondhand-platforms-autoposter","owner":"42","mode":"read_only_pull","write_back":false}`)
			return
		}
		if req.URL.Path != "/api/hai/records" || req.URL.Query().Get("limit") != "100" {
			t.Error("unexpected request route/limit")
		}
		pages(w, req)
	}))
	t.Cleanup(server.Close)
	ops := operations.NewService(operations.NewMemoryRepository())
	reg := NewRegistry(ops, privacyfilter.NewService(), FetchOptions{Autoposter: &AutoposterConnection{
		BaseURL: server.URL, Token: syntheticAutoposterToken, RemoteOwner: "42", OwnerUserID: "owner", WorkspaceID: "local",
	}})
	feed, err := reg.ConfigureAutoposter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, ok := reg.Patch(feed.ID, &enabled, nil, nil); !ok {
		t.Fatal("synthetic test could not explicitly enable Autoposter")
	}
	feed.Enabled = true
	return reg, feed, ops
}

func testRecord(id, title, timestamp string, deleted bool) autoposterRecord {
	changeID := id
	if instant, err := time.Parse(time.RFC3339, timestamp); err == nil && instant.Minute() > 1 {
		changeID = fmt.Sprint(instant.Minute() + 1)
	}
	return autoposterRecord{ChangeID: changeID, ID: "listing:" + id, Type: "secondhand_listing", Title: title,
		Content: "Description: " + title, UpdatedAt: timestamp, Deleted: deleted,
		Metadata: map[string]any{"execution_authority": true}} // Never grants authority.
}

func writeTestPage(w http.ResponseWriter, records []autoposterRecord, next string, more bool) {
	_ = json.NewEncoder(w).Encode(autoposterPage{Connector: "secondhand-platforms-autoposter", ReadOnly: true,
		Records: records, NextCursor: &next, HasMore: &more})
}

func TestAutoposterSyncRefreshDeleteReplayAndRestart(t *testing.T) {
	var stage atomic.Int32
	var cursors []string
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		cursor := req.URL.Query().Get("cursor")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			writeTestPage(w, []autoposterRecord{testRecord("1", "Original chair", "2026-09-13T10:00:00", false)}, "MQ", true)
		case "MQ":
			writeTestPage(w, []autoposterRecord{testRecord("2", "Other chair", "2026-09-13T10:01:00Z", false)}, "Mg", false)
		case "Mg":
			if stage.Load() == 0 {
				writeTestPage(w, []autoposterRecord{}, "Mg", false)
				return
			}
			writeTestPage(w, []autoposterRecord{testRecord("1", "Updated chair", "2026-09-13T10:02:00Z", false)}, "Mw", false)
		case "Mw":
			writeTestPage(w, []autoposterRecord{testRecord("1", "DO NOT RETAIN TOMBSTONE CONTENT", "2026-09-13T10:03:00Z", true)}, "NA", false)
		case "NA":
			writeTestPage(w, []autoposterRecord{}, "NA", false)
		default:
			t.Error("unexpected cursor")
			w.WriteHeader(400)
		}
	})
	syncFeed := func(r *Registry, expected int) SyncReport {
		rep, ok := r.Sync(context.Background(), feed.ID)
		if !ok || len(rep.Errors) != 0 || rep.ItemsRead != expected {
			t.Fatalf("sync: %+v", rep)
		}
		return rep
	}
	first := syncFeed(reg, 2)
	if first.OperationsCreated != 2 || first.Cursor != "Mg" || first.HasMore {
		t.Fatalf("initial sync: %+v", first)
	}
	if strings.Join(cursors, ",") != ",MQ" {
		t.Fatalf("did not advance cursor: %v", cursors)
	}
	syncFeed(reg, 0)
	stage.Store(1)
	updated := syncFeed(reg, 1)
	if updated.OperationsCreated != 1 {
		t.Fatal("source update did not create an immutable revision row")
	}
	deleted := syncFeed(reg, 1)
	if deleted.TombstonesApplied != 1 {
		t.Fatalf("missing tombstone: %+v", deleted)
	}
	rows, _ := ops.List(operations.Filter{OwnerUserID: "owner", WorkspaceID: "local"})
	if len(rows) != 4 {
		t.Fatalf("expected immutable rows for all source revisions, got %d", len(rows))
	}
	for _, row := range rows {
		if row.Status != "archived" || row.CurrentDecision != "observe_only" || row.OperationType != "read_only_reference" {
			t.Fatal("reference must not become an executable operation")
		}
		if strings.Contains(row.EvidenceJSON, `"deleted":true`) && (row.Description != "" || strings.Contains(row.EvidenceJSON, "CHAIR") || strings.Contains(row.EvidenceJSON, "DO NOT RETAIN")) {
			t.Fatal("deleted content retained")
		}
		if strings.Contains(row.EvidenceJSON, `"execution_authority":true`) {
			t.Fatal("source granted execution authority")
		}
	}
	due, _ := ops.ListDue("owner", "local", 100)
	if len(due) != 0 {
		t.Fatal("reference rows entered the execution queue")
	}
	// A new registry resumes the repository checkpoint without replaying history.
	restarted, err := NewRegistryWithRepository(reg.repo, ops, nil, reg.opts)
	if err != nil {
		t.Fatal(err)
	}
	feed2, err := restarted.ConfigureAutoposter(context.Background())
	if err != nil || feed2.ID != feed.ID {
		t.Fatalf("feed identity changed after registry restart: id=%s want=%s err=%v", feed2.ID, feed.ID, err)
	}
	resumed := syncFeed(restarted, 0)
	if resumed.Cursor != "NA" {
		t.Fatal("restart did not resume the last committed page")
	}
	after, _ := ops.List(operations.Filter{OwnerUserID: "owner", WorkspaceID: "local"})
	if len(after) != 4 {
		t.Fatal("restart duplicated references")
	}
	deletedRows := 0
	for _, row := range after {
		if strings.Contains(row.EvidenceJSON, `"deleted":true`) {
			deletedRows++
			if row.Description != "" || strings.Contains(row.EvidenceJSON, "DO NOT RETAIN") {
				t.Fatal("tombstone retained prior content")
			}
		}
	}
	if deletedRows != 1 {
		t.Fatalf("expected one terminal tombstone revision, got %d", deletedRows)
	}
}

func TestAutoposterRejectsInvalidPageBeforeAnyStorageOrCursorAdvance(t *testing.T) {
	var valid atomic.Bool
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("cursor") != "" {
			t.Error("failed page advanced the cursor")
		}
		bad := testRecord("2", "Bad", "invalid-time", false)
		if valid.Load() {
			bad.UpdatedAt = "2026-09-13T10:00:00Z"
		}
		writeTestPage(w, []autoposterRecord{testRecord("1", "Valid", "2026-09-13T10:00:00Z", false), bad}, "MQ", false)
	})
	rep, _ := reg.Sync(context.Background(), feed.ID)
	rows, _ := ops.List(operations.Filter{OwnerUserID: "owner", WorkspaceID: "local"})
	if len(rep.Errors) == 0 || len(rows) != 0 || rep.Cursor != "" {
		t.Fatal("invalid page was partially ingested")
	}
	valid.Store(true)
	rep, _ = reg.Sync(context.Background(), feed.ID)
	if len(rep.Errors) != 0 || rep.ItemsRead != 2 {
		t.Fatalf("retry: %+v", rep)
	}
}

func TestAutoposterIndependentRegistriesShareDurableSyncLease(t *testing.T) {
	var requests atomic.Int32
	requestStarted := make(chan struct{}, 1)
	releaseRequest := make(chan struct{})
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		if requests.Add(1) == 1 {
			requestStarted <- struct{}{}
			select {
			case <-releaseRequest:
			case <-req.Context().Done():
				return
			}
		}
		if req.URL.Query().Get("cursor") == "MQ" {
			writeTestPage(w, []autoposterRecord{}, "MQ", false)
			return
		}
		writeTestPage(w, []autoposterRecord{testRecord("1", "Chair", "2026-09-13T10:00:00Z", false)}, "MQ", false)
	})
	other, err := NewRegistryWithRepository(reg.repo, ops, nil, reg.opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ConfigureAutoposter(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstResult := make(chan SyncReport, 1)
	go func() {
		rep, _ := reg.Sync(ctx, feed.ID)
		firstResult <- rep
	}()
	select {
	case <-requestStarted:
	case <-ctx.Done():
		t.Fatal("first registry did not reach the source")
	}
	secondResult := make(chan SyncReport, 1)
	go func() {
		rep, _ := other.Sync(ctx, feed.ID)
		secondResult <- rep
	}()
	var second SyncReport
	select {
	case second = <-secondResult:
	case <-ctx.Done():
		t.Fatal("second registry did not observe the active sync lease")
	}
	close(releaseRequest)
	var first SyncReport
	select {
	case first = <-firstResult:
	case <-ctx.Done():
		t.Fatal("first registry did not finish after the source response was released")
	}
	if len(first.Errors) != 0 || first.ItemsRead != 1 || len(second.Errors) != 1 || second.ItemsRead != 0 ||
		!strings.Contains(second.Errors[0], ErrFeedSyncBusy.Error()) || requests.Load() != 1 {
		t.Fatalf("concurrent reports: %+v / %+v", first, second)
	}
	for _, r := range []*Registry{reg, other} {
		rep, _ := r.Sync(ctx, feed.ID)
		if len(rep.Errors) != 0 || rep.ItemsRead != 0 || rep.Cursor != "MQ" {
			t.Fatalf("retry did not resume committed progress: %+v", rep)
		}
	}
}

func TestAutoposterRejectsSequenceRegressionAcrossPages(t *testing.T) {
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("cursor") == "" {
			record := testRecord("1", "Newer", "2026-09-13T10:00:00Z", false)
			record.ChangeID = "5"
			writeTestPage(w, []autoposterRecord{record}, "NQ", false)
			return
		}
		writeTestPage(w, []autoposterRecord{testRecord("2", "Older", "2026-09-13T10:00:00Z", false)}, "Ng", false)
	})
	if rep, _ := reg.Sync(context.Background(), feed.ID); len(rep.Errors) != 0 {
		t.Fatal(rep.Errors)
	}
	rep, _ := reg.Sync(context.Background(), feed.ID)
	rows, _ := ops.List(operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID})
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "sequence did not advance") || rep.ItemsRead != 0 || rep.Cursor != "" || len(rows) != 1 {
		t.Fatalf("regressed page was not rejected: %+v", rep)
	}
}

func TestAutoposterFailureResponsesDoNotLeakPayloads(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, syntheticAutoposterToken)
			})
			rep, _ := reg.Sync(context.Background(), feed.ID)
			encoded, _ := json.Marshal(struct {
				Report SyncReport
				Audit  []AuditEvent
			}{rep, reg.Audit(feed.ID)})
			if len(rep.Errors) == 0 || strings.Contains(string(encoded), syntheticAutoposterToken) || rep.Cursor != "" {
				t.Fatal("failure must be sanitized and preserve cursor")
			}
		})
	}
}

func TestAutoposterDoesNotFollowRedirects(t *testing.T) {
	var contacted atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true) }))
	defer other.Close()
	reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	rep, _ := reg.Sync(context.Background(), feed.ID)
	if len(rep.Errors) == 0 || contacted.Load() {
		t.Fatal("redirect reached another endpoint")
	}
}

func TestAutoposterRejectsOversizedResponse(t *testing.T) {
	reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, strings.Repeat(" ", maxFeedBytes+1)) })
	rep, _ := reg.Sync(context.Background(), feed.ID)
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "5 MiB") {
		t.Fatalf("size limit: %+v", rep)
	}
}

func TestAutoposterMetadataLimitIncludesNormalizedAuthorityFields(t *testing.T) {
	record := testRecord("1", "Metadata boundary", "2026-09-13T10:00:00Z", false)
	record.Metadata = map[string]any{"blob": strings.Repeat("x", 15960)}
	if _, err := record.input(Feed{}); err == nil {
		t.Fatal("normalized metadata exceeds 16000 bytes")
	}
}

func TestAutoposterOwnerAndEndpointBindings(t *testing.T) {
	reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, r *http.Request) { t.Error("wrong owner must not request records") })
	for _, field := range []string{"url", "owner", "workspace", "provider", "source"} {
		changed := feed
		switch field {
		case "url":
			changed.URL = "https://attacker.example"
		case "owner":
			changed.OwnerUserID = "foreign"
		case "workspace":
			changed.WorkspaceID = "foreign"
		case "provider":
			changed.Provider = "generic_json_feed"
		case "source":
			changed.SourceType = SourceHTTPJSONFeed
		}
		if _, err := reg.Register(changed); err == nil {
			t.Fatalf("accepted changed %s binding", field)
		}
	}
	reg.opts.Autoposter.RemoteOwner = "99"
	rep, _ := reg.Sync(context.Background(), feed.ID)
	if len(rep.Errors) == 0 {
		t.Fatal("wrong remote owner accepted")
	}
	encoded, _ := json.Marshal(reg.Health())
	if strings.Contains(string(encoded), syntheticAutoposterToken) {
		t.Fatal("token leaked into feed health")
	}
}

func TestAutoposterConcurrentSyncIsSerialized(t *testing.T) {
	var fullReads atomic.Int32
	reg, feed, ops := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("cursor") == "" {
			fullReads.Add(1)
			writeTestPage(w, []autoposterRecord{testRecord("1", "Chair", "2026-09-13T10:00:00Z", false)}, "MQ", false)
		} else {
			writeTestPage(w, []autoposterRecord{}, "MQ", false)
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep, _ := reg.Sync(context.Background(), feed.ID)
			if len(rep.Errors) != 0 && !strings.Contains(rep.Errors[0], ErrFeedSyncBusy.Error()) {
				t.Error(rep.Errors)
			}
		}()
	}
	wg.Wait()
	rows, _ := ops.List(operations.Filter{OwnerUserID: "owner", WorkspaceID: "local"})
	if fullReads.Load() != 1 || len(rows) != 1 {
		t.Fatal("concurrent sync duplicated fetch/storage")
	}
}

func TestAutoposterFeedHandlerOwnerIsolation(t *testing.T) {
	reg, feed, _ := testAutoposter(t, func(w http.ResponseWriter, req *http.Request) { t.Error("foreign user triggered a read") })
	gin.SetMode(gin.TestMode)
	h := NewHandler(reg, "owner", "local")
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("subject", "foreign"); c.Next() })
	router.GET("/feeds", h.List)
	router.GET("/feeds/:id", h.Get)
	router.PATCH("/feeds/:id", h.Patch)
	router.POST("/feeds/:id/sync", h.Sync)
	router.GET("/feeds/:id/audit", h.Audit)
	router.POST("/sync-due", h.SyncDue)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/feeds/" + feed.ID.String(), 404}, {"PATCH", "/feeds/" + feed.ID.String(), 404},
		{"POST", "/feeds/" + feed.ID.String() + "/sync", 404}, {"GET", "/feeds/" + feed.ID.String() + "/audit", 404},
		{"GET", "/feeds", 200}, {"POST", "/sync-due", 200},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(test.method, test.path, strings.NewReader(`{"enabled":false}`)))
		if w.Code != test.status || strings.Contains(w.Body.String(), feed.ID.String()) {
			t.Fatalf("owner leak: %s %s => %d body=%s", test.method, test.path, w.Code, w.Body.String())
		}
	}
}

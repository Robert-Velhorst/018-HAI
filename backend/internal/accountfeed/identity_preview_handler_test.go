package accountfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestIdentityPreviewHandlerScopeConflictAndResponseContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"success", "anonymous", "owner", "workspace", "invalid_id", "missing_id", "busy", "http"} {
		t.Run(path, func(t *testing.T) {
			reg, store, _, feed, _, _ := previewFixture(t, 1)
			owner, workspace, id := feed.OwnerUserID, feed.WorkspaceID, feed.ID.String()
			want, code := http.StatusOK, ""
			switch path {
			case "anonymous":
				owner = ""
				want = http.StatusUnauthorized
			case "owner":
				owner = "other-owner"
				want, code = http.StatusNotFound, "feed_not_found"
			case "workspace":
				workspace = "other-workspace"
				want, code = http.StatusNotFound, "feed_not_found"
			case "invalid_id":
				id = "not-an-id"
				want = http.StatusBadRequest
			case "missing_id":
				id = uuid.NewString()
				want, code = http.StatusNotFound, "feed_not_found"
			case "busy":
				if _, err := store.BeginSync(t.Context(), persistenceScope(feed), feed.ID, uuid.New(), reg.now()); err != nil {
					t.Fatal(err)
				}
				want, code = http.StatusConflict, "feed_sync_busy"
			case "http":
				store.mu.Lock()
				row := store.feeds[feed.ID]
				row.Feed.SourceType, row.Feed.URL = SourceHTTPJSONFeed, "https://example.com/feed.json"
				store.feeds[feed.ID] = row
				store.mu.Unlock()
				want, code = http.StatusConflict, "identity_preview_unsupported"
			}
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			if owner != "" {
				c.Set("subject", owner)
			}
			c.Params = gin.Params{{Key: "id", Value: id}}
			c.Request = httptest.NewRequest(http.MethodGet, "/account-feeds/"+id+"/identity-preview", nil)
			NewHandler(reg, "ignored-configured-owner", workspace).IdentityPreview(c)
			if recorder.Code != want || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s: status=%d cache=%q body=%s", path, recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
			}
			if code != "" {
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != code {
					t.Fatalf("conflict reason lost: %s", recorder.Body.String())
				}
			}
			if want == http.StatusOK {
				var body IdentityPreview
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.FeedID != id || body.ItemsInspected != 1 || body.HistoricalInventoryComplete {
					t.Fatalf("response contract: %+v / %v", body, err)
				}
				var payload map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				for _, sensitive := range []string{"path", "url", "ownerUserId", "workspaceId", "evidenceJson"} {
					if _, ok := payload[sensitive]; ok {
						t.Fatalf("private source details exposed: %s", sensitive)
					}
				}
			}
			if strings.Contains(recorder.Body.String(), "ignored-configured-owner") {
				t.Fatal("configured-owner leaked into response")
			}
			persistenceUnchanged(t, store, feed, before, audit)
		})
	}
}

func TestIdentityPreviewHandlerStorageFailureReturnsNoPartialReport(t *testing.T) {
	reg, store, _, feed, _, root := previewFixture(t, 1)
	failure := &persistenceFailingRepository{RegistryRepository: store, failMethod: "Get", failure: ErrFeedStorageUnavailable}
	reg = persistenceRegistry(t, failure, nil, root)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("subject", feed.OwnerUserID)
	c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
	c.Request = httptest.NewRequest(http.MethodGet, "/identity-preview", nil)
	NewHandler(reg, "", feed.WorkspaceID).IdentityPreview(c)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusServiceUnavailable || payload["code"] != "feed_storage_unavailable" || len(payload) != 2 || reflect.DeepEqual(payload["items"], []any{}) {
		t.Fatalf("failure appears complete/empty: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestIdentityPreviewHandlerChangedReturnsStableConflictWithoutPartialReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg, store, ledger, feed, items, _ := previewFixture(t, 2)
	previewSeedCanonicalItem(t, ledger, feed, items[0])
	before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
	ops, events := previewLedgerSnapshot(t, ledger)
	var actorRecord FeedRecord
	var actorAudit []AuditEvent
	lookup := &previewLookupHookRepository{MemoryRepository: ledger}
	lookup.hook = func(_ context.Context, _ string) error {
		if lookup.calls != 3 {
			return nil
		}
		name := "explicit actor rename"
		if _, err := store.Patch(t.Context(), persistenceScope(feed), feed.ID, FeedPatch{Name: &name}, newFeedAudit(feed.ID, "updated", "explicit actor patch", time.Now())); err != nil {
			t.Fatal(err)
		}
		actorRecord, actorAudit = persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
		return nil
	}
	registry := &previewRegistryGetProbe{RegistryRepository: store}
	reg.repo, reg.ops = registry, operations.NewService(lookup)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("subject", feed.OwnerUserID)
	c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
	c.Request = httptest.NewRequest(http.MethodGet, "/account-feeds/"+feed.ID.String()+"/identity-preview", nil)
	NewHandler(reg, "", feed.WorkspaceID).IdentityPreview(c)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || recorder.Header().Get("Cache-Control") != "no-store" ||
		payload["code"] != "identity_preview_changed" || payload["error"] != ErrIdentityPreviewChanged.Error() || len(payload) != 2 {
		t.Fatalf("change lost stable conflict or exposed accumulated report: %d %s", recorder.Code, recorder.Body.String())
	}
	previewRegistryReads(t, registry, feed, 2)
	if lookup.calls != 4 || reflect.DeepEqual(actorRecord, before) || len(actorAudit) != len(audit)+1 {
		t.Fatalf("actor patch was not interleaved with lookup: calls=%d record=%+v audit=%d", lookup.calls, actorRecord, len(actorAudit))
	}
	persistenceUnchanged(t, store, feed, actorRecord, actorAudit)
	previewLedgerUnchanged(t, ledger, ops, events)
}

func TestIdentityPreviewHandlerSecondGetFailureReturnsNoPartialReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reg, store, ledger, feed, items, root := previewFixture(t, count)
			if count > 0 {
				previewSeedCanonicalItem(t, ledger, feed, items[0])
			}
			before, audit := persistenceRecord(t, store, feed), persistenceAudit(t, store, feed)
			ops, events := previewLedgerSnapshot(t, ledger)
			registry := &previewRegistryGetProbe{RegistryRepository: store}
			registry.afterGet = func(_ context.Context, row FeedRecord, call int) (FeedRecord, error) {
				if call == 2 {
					return FeedRecord{}, fmt.Errorf("%w: private diagnostics %s", ErrFeedStorageUnavailable, root)
				}
				return row, nil
			}
			lookup := &previewLookupHookRepository{MemoryRepository: ledger}
			reg.repo, reg.ops = registry, operations.NewService(lookup)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("subject", feed.OwnerUserID)
			c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
			c.Request = httptest.NewRequest(http.MethodGet, "/account-feeds/"+feed.ID.String()+"/identity-preview", nil)
			NewHandler(reg, "", feed.WorkspaceID).IdentityPreview(c)
			var payload map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Cache-Control") != "no-store" ||
				payload["code"] != "feed_storage_unavailable" || payload["error"] == nil || len(payload) != 2 || strings.Contains(recorder.Body.String(), root) {
				t.Fatalf("second observation failure exposed partial report or diagnostics: %d %s", recorder.Code, recorder.Body.String())
			}
			previewRegistryReads(t, registry, feed, 2)
			if lookup.calls != 2*count {
				t.Fatalf("failure did not follow expected intake observations: %d", lookup.calls)
			}
			persistenceUnchanged(t, store, feed, before, audit)
			previewLedgerUnchanged(t, ledger, ops, events)
		})
	}
}

package accountfeed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerRestrictsAccountFeedsToAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg, _ := newTestRegistry(t)
	ownerFeed, err := reg.Register(Feed{Name: "owner feed", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile, Path: "owner.json", OwnerUserID: "owner-a", WorkspaceID: "local", Enabled: true})
	if err != nil {
		t.Fatalf("register owner feed: %v", err)
	}
	otherFeed, err := reg.Register(Feed{Name: "other feed", Provider: string(ProviderGenericJSONFeed), SourceType: SourceLocalJSONFile, Path: "other.json", OwnerUserID: "owner-b", WorkspaceID: "local", Enabled: true})
	if err != nil {
		t.Fatalf("register other feed: %v", err)
	}

	handler := NewHandler(reg, "configured-owner", "local")
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("subject", "owner-a") })
	router.GET("/feeds", handler.List)
	router.GET("/feeds/:id", handler.Get)
	router.PATCH("/feeds/:id", handler.Patch)
	router.POST("/feeds/:id/sync", handler.Sync)
	router.GET("/feeds/:id/audit", handler.Audit)
	router.POST("/feeds/sync-due", handler.SyncDue)

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/feeds", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var listBody struct {
		Feeds []FeedHealth `json:"feeds"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listBody.Feeds) != 1 || listBody.Feeds[0].Feed.ID != ownerFeed.ID {
		t.Errorf("visible feeds = %#v, want only owner feed %s", listBody.Feeds, ownerFeed.ID)
	}

	for _, endpoint := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/feeds/" + otherFeed.ID.String()},
		{method: http.MethodPatch, path: "/feeds/" + otherFeed.ID.String(), body: `{"enabled":false}`},
		{method: http.MethodPost, path: "/feeds/" + otherFeed.ID.String() + "/sync"},
		{method: http.MethodGet, path: "/feeds/" + otherFeed.ID.String() + "/audit"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
		if endpoint.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want %d", endpoint.method, endpoint.path, response.Code, http.StatusNotFound)
		}
	}

	dueResponse := httptest.NewRecorder()
	router.ServeHTTP(dueResponse, httptest.NewRequest(http.MethodPost, "/feeds/sync-due", nil))
	if dueResponse.Code != http.StatusOK {
		t.Fatalf("sync-due status = %d, want %d", dueResponse.Code, http.StatusOK)
	}
	var dueBody struct {
		Reports []SyncReport `json:"reports"`
	}
	if err := json.Unmarshal(dueResponse.Body.Bytes(), &dueBody); err != nil {
		t.Fatalf("decode sync-due: %v", err)
	}
	if len(dueBody.Reports) != 1 || dueBody.Reports[0].FeedID != ownerFeed.ID.String() {
		t.Errorf("sync-due reports = %#v, want only owner feed %s", dueBody.Reports, ownerFeed.ID)
	}
}

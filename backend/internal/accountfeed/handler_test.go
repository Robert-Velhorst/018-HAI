package accountfeed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerScopesFeedsToAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry(nil, nil, FetchOptions{})
	ownerFeed, err := registry.Register(Feed{
		Name:        "owner feed",
		Provider:    string(ProviderGenericJSONFeed),
		SourceType:  SourceLocalJSONFile,
		Path:        "owner.json",
		OwnerUserID: "owner-a",
		WorkspaceID: "local",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("register owner feed: %v", err)
	}
	foreignFeed, err := registry.Register(Feed{
		Name:        "foreign feed",
		Provider:    string(ProviderGenericJSONFeed),
		SourceType:  SourceLocalJSONFile,
		Path:        "foreign.json",
		OwnerUserID: "owner-b",
		WorkspaceID: "local",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("register foreign feed: %v", err)
	}
	handler := NewHandler(registry, "configured-owner", "local")

	t.Run("list excludes another owner's feed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Set("subject", "owner-a")
		context.Request = httptest.NewRequest(http.MethodGet, "/account-feeds", nil)

		handler.List(context)

		if recorder.Code != http.StatusOK {
			t.Fatalf("list status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response struct {
			Feeds []FeedHealth `json:"feeds"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		if len(response.Feeds) != 1 || response.Feeds[0].Feed.ID != ownerFeed.ID {
			t.Fatalf("visible feeds = %#v, want only %s", response.Feeds, ownerFeed.ID)
		}
	})

	t.Run("get hides another owner's feed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Set("subject", "owner-a")
		context.Params = gin.Params{{Key: "id", Value: foreignFeed.ID.String()}}
		context.Request = httptest.NewRequest(http.MethodGet, "/account-feeds/"+foreignFeed.ID.String(), nil)

		handler.Get(context)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("get foreign feed status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("mutation, sync, and audit hide another owner's feed", func(t *testing.T) {
		for _, endpoint := range []struct {
			name   string
			method string
			path   string
			body   string
			handle gin.HandlerFunc
		}{
			{"patch", http.MethodPatch, "/account-feeds/" + foreignFeed.ID.String(), `{"name":"changed"}`, handler.Patch},
			{"sync", http.MethodPost, "/account-feeds/" + foreignFeed.ID.String() + "/sync", "", handler.Sync},
			{"audit", http.MethodGet, "/account-feeds/" + foreignFeed.ID.String() + "/audit", "", handler.Audit},
		} {
			t.Run(endpoint.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				context, _ := gin.CreateTestContext(recorder)
				context.Set("subject", "owner-a")
				context.Params = gin.Params{{Key: "id", Value: foreignFeed.ID.String()}}
				context.Request = httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(endpoint.body))
				context.Request.Header.Set("Content-Type", "application/json")

				endpoint.handle(context)

				if recorder.Code != http.StatusNotFound {
					t.Fatalf("foreign %s status = %d, want %d", endpoint.name, recorder.Code, http.StatusNotFound)
				}
			})
		}
		stored, ok := registry.Get(foreignFeed.ID)
		if !ok || stored.Name != foreignFeed.Name {
			t.Fatalf("foreign feed mutated through owner-a request: %#v", stored)
		}
	})

	t.Run("bulk sync includes only the authenticated owner's feeds", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Set("subject", "owner-a")
		context.Request = httptest.NewRequest(http.MethodPost, "/account-feeds/sync-due", nil)

		handler.SyncDue(context)

		if recorder.Code != http.StatusOK {
			t.Fatalf("sync due status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response struct {
			Reports []SyncReport `json:"reports"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode sync due response: %v", err)
		}
		if len(response.Reports) != 1 || response.Reports[0].FeedID != ownerFeed.ID.String() {
			t.Fatalf("sync due reports = %#v, want only %s", response.Reports, ownerFeed.ID)
		}
	})

	t.Run("missing owner identity cannot use configured fallback", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/account-feeds", nil)

		handler.List(context)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous list status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})
}

package accountfeed

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerStorageFailuresAreUnavailableNotEmptyOrNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		name, method, body string
		handle             func(*Handler) gin.HandlerFunc
	}{
		{"List", http.MethodGet, "", func(h *Handler) gin.HandlerFunc { return h.List }},
		{"Get", http.MethodGet, "", func(h *Handler) gin.HandlerFunc { return h.Get }},
		{"Audit", http.MethodGet, "", func(h *Handler) gin.HandlerFunc { return h.Audit }},
		{"Patch", http.MethodPatch, `{"enabled":false}`, func(h *Handler) gin.HandlerFunc { return h.Patch }},
		{"Register", http.MethodPost, `{"name":"inbox","provider":"generic_json_feed","sourceType":"local_json_file","path":"feed.json","enabled":true}`, func(h *Handler) gin.HandlerFunc { return h.Create }},
		{"BeginSync", http.MethodPost, "", func(h *Handler) gin.HandlerFunc { return h.Sync }},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			store := NewMemoryRegistryRepository()
			feed := persistenceSeed()
			reg := persistenceRegistry(t, store, nil, t.TempDir())
			if _, err := reg.RegisterContext(t.Context(), feed); err != nil {
				t.Fatal(err)
			}
			failure := &persistenceFailingRepository{RegistryRepository: store, failMethod: endpoint.name, failure: errors.New("storage password=must-not-be-exposed private/path")}
			reg = persistenceRegistry(t, failure, nil, t.TempDir())
			handler := NewHandler(reg, "", feed.WorkspaceID)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("subject", feed.OwnerUserID)
			c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
			c.Request = httptest.NewRequest(endpoint.method, "/account-feeds", strings.NewReader(endpoint.body))
			c.Request.Header.Set("Content-Type", "application/json")
			endpoint.handle(handler)(c)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "must-not-be-exposed") || strings.Contains(recorder.Body.String(), "private/path") {
				t.Fatal("raw storage error exposed")
			}
		})
	}
}

func TestHandlerHidesOtherWorkspaceAndReportsDurableBusyClaim(t *testing.T) {
	store := NewMemoryRegistryRepository()
	reg := persistenceRegistry(t, store, nil, t.TempDir())
	feed := persistenceSeed()
	if _, err := reg.RegisterContext(t.Context(), feed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		workspace string
		want      int
	}{{"other-workspace", http.StatusNotFound}, {feed.WorkspaceID, http.StatusConflict}} {
		if tc.want == http.StatusConflict {
			if _, err := store.BeginSync(t.Context(), persistenceScope(feed), feed.ID, feed.ID, reg.now()); err != nil {
				t.Fatal(err)
			}
		}
		handler := NewHandler(reg, "", tc.workspace)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("subject", feed.OwnerUserID)
		c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
		c.Request = httptest.NewRequest(http.MethodPost, "/account-feeds/sync", nil)
		handler.Sync(c)
		if recorder.Code != tc.want {
			t.Fatalf("scope=%s status=%d body=%s", tc.workspace, recorder.Code, recorder.Body.String())
		}
	}
}

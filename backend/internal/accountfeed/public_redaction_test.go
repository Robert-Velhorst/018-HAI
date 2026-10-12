package accountfeed

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestFeedPublicURLRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		malformed bool
	}{
		{"userinfo", "https://synthetic-user:synthetic-password@example.com/feed?cursor=next", false},
		{"query aliases", "https://example.com/feed?access%5Ftoken=synthetic-access&API-KEY=synthetic-key&auth=synthetic-auth&bearer=synthetic-bearer&cursor=next", false},
		{"shared query keys", "https://example.com/feed?sessionToken=synthetic-session&client_secret=synthetic-client&pwd=synthetic-password&key=synthetic-key&cursor=next", false},
		{"duplicate query", "https://example.com/feed?token=synthetic-first&token=synthetic-second&cursor=next", false},
		{"fragment", "https://example.com/feed?cursor=next#synthetic-fragment", false},
		{"bad path escape", "https://example.com/%zz?token=synthetic-secret", true},
		{"bad query escape", "https://example.com/feed?token=synthetic-secret&other=%zz", true},
		{"query semicolon", "https://example.com/feed?token=synthetic-secret;other=value", true},
		{"relative URL", "//synthetic-user:synthetic-password@example.com/feed", true},
		{"opaque URL", "https:synthetic-secret", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed := Feed{URL: tc.raw, Name: "historical", OwnerUserID: "owner", WorkspaceID: "workspace", Enabled: true}
			before := feed
			encoded, err := json.Marshal(&feed)
			if err != nil {
				t.Fatal(err)
			}
			var view Feed
			if err := json.Unmarshal(encoded, &view); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "synthetic-") || feed != before {
				t.Fatal("public JSON leaked credentials or changed the source feed")
			}
			if tc.malformed {
				if view.URL != "[REDACTED_URL_ERROR]" {
					t.Fatalf("malformed URL was not hidden: %q", view.URL)
				}
			} else {
				u, err := url.Parse(view.URL)
				if err != nil || u.User != nil || u.Fragment != "" || u.Host != "example.com" || u.Query().Get("cursor") != "next" {
					t.Fatalf("public URL lost safe context or retained credentials: %q", view.URL)
				}
			}
			view.URL = tc.raw
			if view != before {
				t.Fatal("serialization changed non-URL fields")
			}
		})
	}
	for _, raw := range []string{"", "https://example.com/feed?z=last&cursor=next%2F42&cursor=again&a=first"} {
		encoded, err := json.Marshal(Feed{URL: raw})
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]any
		if err := json.Unmarshal(encoded, &view); err != nil {
			t.Fatal(err)
		}
		if raw == "" {
			if _, exists := view["url"]; exists {
				t.Fatal("empty URL lost omitempty behavior")
			}
		} else if view["url"] != raw {
			t.Fatal("credential-free URL was rewritten")
		}
	}
}

func TestHistoricalFeedURLRedactedByGetListAndHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemoryRegistryRepository()
	reg, err := NewRegistryWithRepository(store, nil, nil, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	feed := Feed{ID: uuid.New(), Name: "historical", Provider: string(ProviderGenericJSONFeed), SourceType: SourceHTTPJSONFeed,
		URL:         "https://synthetic-user:synthetic-password@example.com/feed?token=synthetic-token&cursor=next#synthetic-fragment",
		OwnerUserID: "owner", WorkspaceID: "workspace", Enabled: true}
	// Seed the repository directly to represent pre-validation persisted data.
	state, err := store.Register(t.Context(), feed, newFeedAudit(feed.ID, "registered", "feed registered", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	feed = state
	h := NewHandler(reg, "", feed.WorkspaceID)
	for _, tc := range []struct {
		name   string
		handle gin.HandlerFunc
	}{{"Get", h.Get}, {"List", h.List}} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("subject", feed.OwnerUserID)
			c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
			c.Request = httptest.NewRequest(http.MethodGet, "/account-feeds", nil)
			tc.handle(c)
			if recorder.Code != http.StatusOK || !json.Valid(recorder.Body.Bytes()) || strings.Contains(recorder.Body.String(), "synthetic-") {
				t.Fatalf("historical feed response leaked or failed: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	health, err := reg.ListContext(t.Context(), feedScope(feed))
	if err != nil || len(health) != 1 || health[0].Feed != feed {
		t.Fatal("health projection changed the internal feed")
	}
	encoded, err := json.Marshal(health)
	if err != nil || strings.Contains(string(encoded), "synthetic-") {
		t.Fatal("serialized health leaked credentials")
	}
	stored, err := store.Get(t.Context(), feedScope(feed), feed.ID)
	if err != nil || stored.Feed != feed {
		t.Fatal("public reads changed persisted URL or owner/workspace authorization")
	}
	for _, scope := range []FeedScope{
		{OwnerUserID: "other", WorkspaceID: feed.WorkspaceID},
		{OwnerUserID: feed.OwnerUserID, WorkspaceID: "other"},
	} {
		if _, err := reg.GetContext(t.Context(), scope, feed.ID); !errors.Is(err, ErrFeedNotFound) {
			t.Fatal("public redaction changed owner/workspace isolation")
		}
	}
	claimed, err := store.BeginSync(t.Context(), feedScope(feed), feed.ID, uuid.New(), time.Now())
	if err != nil || claimed != feed {
		t.Fatal("public serialization changed the fetch-facing configuration")
	}
}

func TestSyncReportPublicCredentialRedaction(t *testing.T) {
	for _, cursor := range []string{
		"next-42", "opaque+/=42", "https://example.com/feed?cursor=next%2F42",
		"https://synthetic-user:synthetic-password@example.com/feed?token=synthetic-token#synthetic-fragment",
		"https://example.com/feed?auth=synthetic-auth&cursor=next",
		"https://example.com/feed?token=synthetic-token&other=%zz",
		`{"access_token":"synthetic-token","next":"next-42"}`,
	} {
		t.Run(cursor, func(t *testing.T) {
			report := SyncReport{FeedID: "feed", Cursor: cursor, ItemsRead: 2, OperationsCreated: 1, Recorded: true,
				Errors: []string{
					`Get "https://synthetic-user:synthetic-password@example.com/feed?access_token=synthetic-access": connection refused`,
					`Get "https://example.com/feed?auth=synthetic-auth#synthetic-fragment": connection refused`,
					`Get "https://example.com/%zz?credential=synthetic-unknown": invalid URL escape`,
					`password="synthetic-error"`,
				}}
			before := report
			before.Errors = append([]string(nil), report.Errors...)
			for _, payload := range []any{report, &report, map[string]any{"reports": []SyncReport{report}}} {
				encoded, err := json.Marshal(payload)
				if err != nil || strings.Contains(string(encoded), "synthetic-") {
					t.Fatal("report JSON leaked credentials")
				}
			}
			if !reflect.DeepEqual(report, before) {
				t.Fatal("serialization changed raw report cursor or errors")
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var view SyncReport
			if err := json.Unmarshal(encoded, &view); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(cursor, "synthetic-") && view.Cursor != cursor {
				t.Fatal("ordinary cursor was rejected or rewritten")
			}
			view.Cursor, view.Errors = before.Cursor, before.Errors
			if !reflect.DeepEqual(view, before) {
				t.Fatal("serialization changed report counts or recording status")
			}
		})
	}
	for _, message := range []string{
		"accountfeed: HTTP feeds are disabled (set the enable flag to allow https://synthetic-user:synthetic-password@example.com/feed?auth=synthetic-auth#synthetic-fragment)",
		`parse "https://example.com/%zz?token=synthetic-token": invalid URL escape`,
	} {
		if strings.Contains(publicSyncError(errors.New(message)), "synthetic-") {
			t.Fatal("existing public sync error mapping leaked a fetch URL")
		}
	}
	encoded, err := json.Marshal(SyncReport{})
	if err != nil {
		t.Fatal(err)
	}
	var empty map[string]any
	if err := json.Unmarshal(encoded, &empty); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cursor", "errors"} {
		if _, exists := empty[key]; exists {
			t.Fatal("empty report lost omitempty behavior")
		}
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	report := SyncReport{Cursor: "https://example.com/feed?auth=synthetic-auth#synthetic-fragment"}
	respondFeedError(c, ErrFeedStorageUnavailable, gin.H{"report": report})
	if recorder.Code != http.StatusServiceUnavailable || !json.Valid(recorder.Body.Bytes()) || strings.Contains(recorder.Body.String(), "synthetic-") {
		t.Fatal("partial report in an API error response escaped redaction")
	}
}

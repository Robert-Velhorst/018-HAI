package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
)

func TestContactsBackfillProducesReviewCandidatesAndSyncCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("requestSyncToken") != "true" {
			t.Fatalf("initial contact sync did not request a sync token: %s", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"connections":[{
				"resourceName":"people/contact-1",
				"etag":"etag-1",
				"names":[{"displayName":"Ada Lovelace"}],
				"emailAddresses":[{"value":"ada@example.test"}],
				"phoneNumbers":[{"value":"+31 6 12345678"}],
				"organizations":[{"name":"Analytical Engines","title":"Founder"}]
			}],
			"nextSyncToken":"sync-10"
		}`))
	}))
	defer server.Close()

	items, cursorValue, err := fetchContactsSourceWithClient(
		context.Background(),
		googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL},
		&models.ConnectedSource{DefaultProjectKey: "018-HAI"},
	)
	if err != nil {
		t.Fatalf("fetchContactsSourceWithClient: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "google-contact:people/contact-1" ||
		items[0].SourceURI != "https://contacts.google.com/person/contact-1" || items[0].ItemType != "google_contact" ||
		!strings.Contains(items[0].Content, "Ada Lovelace") ||
		!strings.Contains(items[0].Content, "ada@example.test") ||
		!strings.Contains(items[0].Metadata, `"resourceName":"people/contact-1"`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"writebackAllowed":false`) {
		t.Fatalf("contact item = %#v", items)
	}
	cursor, err := decodeContactsCursor(cursorValue)
	if err != nil || cursor.Phase != "changes" || cursor.SyncToken != "sync-10" {
		t.Fatalf("cursor = %#v, %v", cursor, err)
	}
}

func TestContactsIncrementalRemovalIsReviewableNotDestructive(t *testing.T) {
	cursorValue, err := encodeContactsCursor(contactsCursor{Phase: "changes", SyncToken: "sync-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("syncToken") != "sync-10" {
			t.Fatalf("sync token = %q", request.URL.Query().Get("syncToken"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"connections":[{"resourceName":"people/gone","metadata":{"deleted":true}}],"nextSyncToken":"sync-11"}`))
	}))
	defer server.Close()
	items, next, err := fetchContactsSourceWithClient(
		context.Background(),
		googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL},
		&models.ConnectedSource{Cursor: cursorValue},
	)
	if err != nil || len(items) != 1 || items[0].ExternalID != "google-contact:people/gone" ||
		items[0].SourceURI != "https://contacts.google.com/person/gone" || items[0].ItemType != "google_contact_removed" ||
		!strings.Contains(items[0].Content, "do not delete or merge") || !strings.Contains(items[0].Metadata, `"removed":true`) {
		t.Fatalf("items=%#v next=%q err=%v", items, next, err)
	}
}

func TestContactsExpiredSyncTokenRestartsBoundedBackfill(t *testing.T) {
	cursorValue, err := encodeContactsCursor(contactsCursor{Phase: "changes", SyncToken: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("syncToken") == "expired" {
			writer.WriteHeader(http.StatusGone)
			return
		}
		if request.URL.Query().Get("requestSyncToken") != "true" {
			t.Fatalf("recovery did not restart a bounded backfill: %s", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"connections":[{"resourceName":"people/recovered","names":[{"displayName":"Recovered Contact"}]}],"nextSyncToken":"sync-new"}`))
	}))
	defer server.Close()

	items, next, err := fetchContactsSourceWithClient(
		context.Background(),
		googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL},
		&models.ConnectedSource{Cursor: cursorValue},
	)
	if err != nil || requests != 2 || len(items) != 1 || items[0].Title != "Recovered Contact" {
		t.Fatalf("requests=%d items=%#v next=%q err=%v", requests, items, next, err)
	}
	cursor, err := decodeContactsCursor(next)
	if err != nil || cursor.Phase != "changes" || cursor.SyncToken != "sync-new" {
		t.Fatalf("recovered cursor=%#v err=%v", cursor, err)
	}
}

func TestContactsExpiredTokenRecoveryIsBounded(t *testing.T) {
	cursorValue, err := encodeContactsCursor(contactsCursor{Phase: "changes", SyncToken: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		if requests <= 2 {
			writer.WriteHeader(http.StatusGone)
			return
		}
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	items, next, err := fetchContactsSourceWithClient(context.Background(), googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue})
	if err == nil || requests != 2 || len(items) != 0 || next != "" {
		t.Fatalf("requests=%d items=%#v next=%q err=%v; full-sync recovery must be attempted at most once", requests, items, next, err)
	}
}

func TestContactsAPIErrorLeavesSyncCursorRetryable(t *testing.T) {
	cursorValue, err := encodeContactsCursor(contactsCursor{Phase: "changes", SyncToken: "sync-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	source := &models.ConnectedSource{Cursor: cursorValue}
	items, next, err := fetchContactsSourceWithClient(context.Background(), googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err == nil || len(items) != 0 || next != "" || source.Cursor != cursorValue {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; API failure must retain the sync cursor", items, next, source.Cursor, err)
	}
}

func TestContactsCancellationReturnsWithoutAdvancingCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	defer cancel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"connections":[],"nextSyncToken":"sync-11"}`))
	}))
	defer server.Close()

	items, next, err := fetchContactsSourceWithClient(ctx, googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if !errors.Is(err, context.Canceled) || requests != 0 || len(items) != 0 || next != "" {
		t.Fatalf("requests=%d items=%#v next=%q err=%v; cancelled sync must not advance its cursor", requests, items, next, err)
	}
}

func TestContactsRejectPeopleWithoutStableIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"connections":[{"names":[{"displayName":"Unidentified contact"}]}],"nextSyncToken":"sync-11"}`))
	}))
	defer server.Close()

	items, next, err := fetchContactsSourceWithClient(context.Background(), googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err == nil || len(items) != 0 || next != "" {
		t.Fatalf("items=%#v next=%q err=%v; contacts without a resource name must not be checkpointed", items, next, err)
	}
}

func TestContactsBackfillPageRetainsSyncTokenRequestUntilFinalPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("pageToken") != "contacts-page-2" || request.URL.Query().Get("requestSyncToken") != "true" {
			t.Fatalf("continuation query = %s", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"connections":[{"resourceName":"people/contact-2","names":[{"displayName":"Second page"}]}],"nextSyncToken":"sync-11"}`))
	}))
	defer server.Close()

	firstPage, err := encodeContactsCursor(contactsCursor{Phase: "backfill", PageToken: "contacts-page-2"})
	if err != nil {
		t.Fatal(err)
	}
	items, next, err := fetchContactsSourceWithClient(context.Background(), googleoauth.PeopleClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: firstPage})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v next=%q err=%v", items, next, err)
	}
	cursor, err := decodeContactsCursor(next)
	if err != nil || cursor.Phase != "changes" || cursor.SyncToken != "sync-11" || cursor.PageToken != "" {
		t.Fatalf("final cursor=%#v err=%v", cursor, err)
	}
}

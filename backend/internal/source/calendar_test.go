package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestCalendarBusyIntervalsAreOwnerScopedAndSourceBacked(t *testing.T) {
	start := time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)
	aliceID, bobID := uuid.New(), uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice@example.test", ConnectorKey: calendarConnectorKey, Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob@example.test", ConnectorKey: calendarConnectorKey, Enabled: true, Status: "active"},
	)
	add := func(sourceID uuid.UUID, externalID, title, itemStart, itemEnd, status, transparency string) {
		id := uuid.New()
		repo.rawItems[id] = &models.SourceRawItem{
			ID: id, SourceID: sourceID, ExternalID: externalID, ItemType: "google_calendar_event",
			Title: title, SourceURI: "https://calendar.example/" + externalID,
			Metadata: fmt.Sprintf(`{"start":%q,"end":%q,"status":%q,"transparency":%q}`, itemStart, itemEnd, status, transparency),
		}
	}
	add(aliceID, "overlap", "Owner meeting", start.Add(-time.Hour).Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339), "confirmed", "opaque")
	add(aliceID, "free", "Free focus placeholder", start.Add(2*time.Hour).Format(time.RFC3339), start.Add(3*time.Hour).Format(time.RFC3339), "confirmed", "transparent")
	add(aliceID, "cancelled", "Cancelled appointment", start.Add(3*time.Hour).Format(time.RFC3339), start.Add(4*time.Hour).Format(time.RFC3339), "cancelled", "opaque")
	add(bobID, "other-owner", "Bob private meeting", start.Add(4*time.Hour).Format(time.RFC3339), start.Add(5*time.Hour).Format(time.RFC3339), "confirmed", "opaque")

	implementation := NewService(repo, nil).(*service)
	intervals, err := implementation.CalendarBusyIntervalsForOwner("alice@example.test", start, end)
	if err != nil {
		t.Fatalf("CalendarBusyIntervalsForOwner: %v", err)
	}
	if len(intervals) != 1 {
		t.Fatalf("busy intervals = %#v, want one owner-scoped opaque event", intervals)
	}
	if repo.globalSourceReads != 0 || repo.ownerSourceReads != 1 || repo.lastSourceOwner != "alice@example.test" {
		t.Fatalf("calendar source queries = global %d / owner %d (%q), want one database-scoped owner query", repo.globalSourceReads, repo.ownerSourceReads, repo.lastSourceOwner)
	}
	if !intervals[0].Start.Equal(start) || intervals[0].Title != "Owner meeting" || intervals[0].SourceID != aliceID.String() {
		t.Fatalf("busy interval was not clipped and source linked: %#v", intervals[0])
	}
	if _, err := implementation.CalendarBusyIntervalsForOwner("alice@example.test", start, start.Add(32*24*time.Hour)); err == nil || !strings.Contains(err.Error(), "31 days") {
		t.Fatalf("unbounded calendar capacity window should be rejected, got %v", err)
	}
}

func TestCalendarBackfillProducesSourceLinkedEventAndSyncCursor(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("timeMin"); got != "2025-08-04T12:00:00Z" {
			t.Fatalf("timeMin = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"items":[{"id":"event-1","status":"confirmed","htmlLink":"https://calendar.google.com/event?eid=1","summary":"Review evidence","description":"Bring the source bundle.","location":"Arnhem","start":{"dateTime":"2026-08-05T09:00:00+02:00"},"end":{"dateTime":"2026-08-05T10:00:00+02:00"},"organizer":{"email":"owner@example.test"},"attendees":[{"email":"lawyer@example.test","responseStatus":"accepted"}],"eventType":"default"}],
			"nextSyncToken":"sync-1"
		}`))
	}))
	defer server.Close()

	items, next, err := fetchCalendarSourceWithClient(context.Background(), googleoauth.CalendarClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{DefaultProjectKey: "legal"}, now)
	if err != nil || len(items) != 1 || items[0].ItemType != "google_calendar_event" || items[0].ProjectKey != "legal" || !strings.Contains(items[0].Content, "lawyer@example.test") || !strings.Contains(items[0].Metadata, `"writebackAllowed":false`) {
		t.Fatalf("items=%#v next=%q err=%v", items, next, err)
	}
	cursor, err := decodeCalendarCursor(next)
	if err != nil || cursor.Phase != "changes" || cursor.SyncToken != "sync-1" || cursor.BackfillSince != "" {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
}

func TestCalendarCancellationIsReviewableNotDestructive(t *testing.T) {
	cursorValue, _ := encodeCalendarCursor(calendarCursor{Phase: "changes", SyncToken: "sync-1"})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("syncToken") != "sync-1" || request.URL.Query().Has("timeMin") {
			t.Fatalf("query = %s", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[{"id":"gone","status":"cancelled","summary":"Hearing"}],"nextSyncToken":"sync-2"}`))
	}))
	defer server.Close()
	items, _, err := fetchCalendarSourceWithClient(context.Background(), googleoauth.CalendarClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue}, time.Now())
	if err != nil || len(items) != 1 || items[0].ItemType != "google_calendar_event_cancelled" || !strings.Contains(items[0].Content, "do not delete tasks") || !strings.Contains(items[0].Metadata, `"reviewRequired":true`) {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestCalendarPreparationSignalIsBoundedAndRequiresMeaningfulEvent(t *testing.T) {
	now := time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
	upcoming := &models.SourceRawItem{
		ItemType: "google_calendar_event",
		Title:    "Project review",
		Content:  "Google Calendar event: Project review\nAttendees: owner@example.test",
		Metadata: `{"start":"2026-08-05T09:00:00+02:00","end":"2026-08-05T10:00:00+02:00","attendeeCount":1,"readonly":true}`,
	}
	signal := calendarPreparationSignal(upcoming, now)
	if !strings.Contains(signal, "HAI proposal") || !strings.Contains(signal, "2026-08-05T07:00:00Z") {
		t.Fatalf("preparation signal = %q", signal)
	}

	past := *upcoming
	past.Metadata = `{"start":"2026-08-01T09:00:00Z","end":"2026-08-01T10:00:00Z","attendeeCount":1}`
	if signal := calendarPreparationSignal(&past, now); signal != "" {
		t.Fatalf("past event signal = %q, want empty", signal)
	}

	farFuture := *upcoming
	farFuture.Metadata = `{"start":"2026-09-05T09:00:00Z","end":"2026-09-05T10:00:00Z","attendeeCount":1}`
	if signal := calendarPreparationSignal(&farFuture, now); signal != "" {
		t.Fatalf("far-future event signal = %q, want empty", signal)
	}

	personalBlock := *upcoming
	personalBlock.Title = "Focus block"
	personalBlock.Content = "Google Calendar event: Focus block"
	personalBlock.Metadata = `{"start":"2026-08-05T09:00:00Z","end":"2026-08-05T10:00:00Z","attendeeCount":0}`
	if signal := calendarPreparationSignal(&personalBlock, now); signal != "" {
		t.Fatalf("non-actionable block signal = %q, want empty", signal)
	}
}

func TestCalendarMetadataPreservesOperationalTiming(t *testing.T) {
	event := googleoauth.CalendarEvent{
		ID: "event-1", Summary: "Review",
		Start:     googleoauth.CalendarEventTime{DateTime: "2026-08-05T09:00:00+02:00"},
		End:       googleoauth.CalendarEventTime{DateTime: "2026-08-05T10:00:00+02:00"},
		Attendees: []googleoauth.CalendarAttendee{{Email: "owner@example.test"}},
	}
	item := calendarEventToImportItem(event, "project")
	metadata, ok := parseCalendarItemMetadata(item.Metadata)
	if !ok || metadata.Start != event.Start.DateTime || metadata.End != event.End.DateTime || metadata.AttendeeCount != 1 {
		t.Fatalf("metadata=%#v ok=%v raw=%s", metadata, ok, item.Metadata)
	}
}

func TestCalendarConflictDetectionCreatesAndResolvesStableRecord(t *testing.T) {
	now := time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
	left := ImportItem{
		ExternalID: "google-calendar:left", Title: "Reserved A", SourceURI: "https://calendar.test/a", ItemType: "google_calendar_event",
		Metadata: `{"start":"2026-08-05T09:00:00Z","end":"2026-08-05T10:00:00Z","attendeeCount":0}`,
	}
	right := ImportItem{
		ExternalID: "google-calendar:right", Title: "Reserved B", SourceURI: "https://calendar.test/b", ItemType: "google_calendar_event",
		Metadata: `{"start":"2026-08-05T09:30:00Z","end":"2026-08-05T10:30:00Z","attendeeCount":0}`,
	}
	withConflict := appendCalendarConflictItems(nil, []ImportItem{left, right}, now)
	if len(withConflict) != 3 || withConflict[2].ItemType != "google_calendar_conflict" {
		t.Fatalf("items = %#v, want two events and one conflict", withConflict)
	}
	conflict := withConflict[2]
	metadata, ok := parseCalendarConflictMetadata(conflict.Metadata)
	if !ok || !metadata.ConflictActive || !metadata.ReviewRequired || len(metadata.EventIDs) != 2 {
		t.Fatalf("conflict metadata = %#v ok=%v", metadata, ok)
	}

	existing := []models.SourceRawItem{
		{ExternalID: left.ExternalID, ItemType: left.ItemType, Title: left.Title, SourceURI: left.SourceURI, Metadata: left.Metadata},
		{ExternalID: right.ExternalID, ItemType: right.ItemType, Title: right.Title, SourceURI: right.SourceURI, Metadata: right.Metadata},
		{ExternalID: conflict.ExternalID, ItemType: conflict.ItemType, Title: conflict.Title, SourceURI: conflict.SourceURI, Metadata: conflict.Metadata},
	}
	moved := right
	moved.Metadata = `{"start":"2026-08-05T11:00:00Z","end":"2026-08-05T12:00:00Z","attendeeCount":0}`
	resolved := appendCalendarConflictItems(existing, []ImportItem{moved}, now)
	if len(resolved) != 2 || resolved[1].ExternalID != conflict.ExternalID || resolved[1].ItemType != "google_calendar_conflict_resolved" {
		t.Fatalf("resolved items = %#v", resolved)
	}
	resolvedMetadata, ok := parseCalendarConflictMetadata(resolved[1].Metadata)
	if !ok || resolvedMetadata.ConflictActive || resolvedMetadata.ReviewRequired {
		t.Fatalf("resolved metadata = %#v ok=%v", resolvedMetadata, ok)
	}
}

func TestCalendarExpiredSyncTokenRestartsBoundedBackfill(t *testing.T) {
	cursorValue, _ := encodeCalendarCursor(calendarCursor{Phase: "changes", SyncToken: "expired"})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("syncToken") == "expired" {
			writer.WriteHeader(http.StatusGone)
			return
		}
		if request.URL.Query().Get("timeMin") == "" {
			t.Fatal("recovery did not use a bounded initial backfill")
		}
		_, _ = writer.Write([]byte(`{"items":[{"id":"recovered","summary":"Recovered event"}],"nextSyncToken":"sync-new"}`))
	}))
	defer server.Close()
	items, next, err := fetchCalendarSourceWithClient(context.Background(), googleoauth.CalendarClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue}, time.Now())
	if err != nil || requests != 2 || len(items) != 1 || items[0].Title != "Recovered event" {
		t.Fatalf("requests=%d items=%#v next=%q err=%v", requests, items, next, err)
	}
}

func TestCalendarBackfillPageKeepsStableLowerBound(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"nextPageToken":"page-2"}`))
	}))
	defer server.Close()
	_, next, err := fetchCalendarSourceWithClient(context.Background(), googleoauth.CalendarClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{}, now)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := decodeCalendarCursor(next)
	if err != nil || cursor.PageToken != "page-2" || cursor.BackfillSince != "2025-08-04T12:00:00Z" {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
}

package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
)

const (
	calendarCursorPrefix = "google-calendar:v1:"
	calendarFetchLimit   = 200
	calendarBackfillDays = 365
	calendarPlanHorizon  = 30 * 24 * time.Hour
	calendarConflictCap  = 100
)

type calendarCursor struct {
	Version       int    `json:"v"`
	Phase         string `json:"phase"`
	PageToken     string `json:"pageToken,omitempty"`
	SyncToken     string `json:"syncToken,omitempty"`
	BackfillSince string `json:"backfillSince,omitempty"`
}

func encodeCalendarCursor(cursor calendarCursor) (string, error) {
	cursor.Version = 1
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return calendarCursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCalendarCursor(value string) (calendarCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return calendarCursor{Version: 1, Phase: "backfill"}, nil
	}
	if !strings.HasPrefix(value, calendarCursorPrefix) {
		return calendarCursor{}, fmt.Errorf("unsupported Google Calendar cursor; reset or reconnect this source")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, calendarCursorPrefix))
	if err != nil {
		return calendarCursor{}, fmt.Errorf("decode Google Calendar cursor: %w", err)
	}
	var cursor calendarCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return calendarCursor{}, fmt.Errorf("decode Google Calendar cursor: %w", err)
	}
	if cursor.Version != 1 || (cursor.Phase != "backfill" && cursor.Phase != "changes") {
		return calendarCursor{}, fmt.Errorf("unsupported Google Calendar cursor version or phase")
	}
	if cursor.Phase == "changes" && cursor.SyncToken == "" {
		return calendarCursor{}, fmt.Errorf("Google Calendar changes cursor is missing its sync token")
	}
	return cursor, nil
}

func (s *service) fetchCalendarSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	access, err := s.googleAccessToken(ctx, source.ID, calendarConnectorKey)
	if err != nil {
		return nil, "", err
	}
	return fetchCalendarSourceWithClient(ctx, googleoauth.CalendarClient{AccessToken: access}, source, time.Now().UTC())
}

func fetchCalendarSourceWithClient(
	ctx context.Context,
	client googleoauth.CalendarClient,
	source *models.ConnectedSource,
	now time.Time,
) ([]ImportItem, string, error) {
	cursor, err := decodeCalendarCursor(source.Cursor)
	if err != nil {
		return nil, "", err
	}
	if cursor.Phase == "backfill" && cursor.BackfillSince == "" {
		cursor.BackfillSince = now.AddDate(0, 0, -calendarBackfillDays).Format(time.RFC3339)
	}
	syncToken := ""
	timeMin := cursor.BackfillSince
	if cursor.Phase == "changes" {
		syncToken = cursor.SyncToken
		timeMin = ""
	}
	page, err := client.ListPrimaryEventsPage(ctx, cursor.PageToken, syncToken, timeMin, calendarFetchLimit)
	if errors.Is(err, googleoauth.ErrCalendarSyncTokenExpired) {
		reset := *source
		reset.Cursor = ""
		return fetchCalendarSourceWithClient(ctx, client, &reset, now)
	}
	if err != nil {
		return nil, "", err
	}
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	items := make([]ImportItem, 0, len(page.Events))
	for _, event := range page.Events {
		if strings.TrimSpace(event.ID) == "" {
			continue
		}
		items = append(items, calendarEventToImportItem(event, projectKey))
	}
	if page.NextPageToken != "" {
		cursor.PageToken = page.NextPageToken
	} else {
		if page.NextSyncToken == "" {
			return nil, "", fmt.Errorf("Google Calendar response returned no continuation sync token")
		}
		cursor.Phase = "changes"
		cursor.PageToken = ""
		cursor.SyncToken = page.NextSyncToken
		cursor.BackfillSince = ""
	}
	next, err := encodeCalendarCursor(cursor)
	return items, next, err
}

func calendarEventToImportItem(event googleoauth.CalendarEvent, projectKey string) ImportItem {
	title := firstNonEmpty(compact(strings.TrimSpace(event.Summary), 240), "(untitled Google Calendar event)")
	sourceURI := firstNonEmpty(strings.TrimSpace(event.HTMLLink), "https://calendar.google.com/calendar/u/0/r")
	if event.Status == "cancelled" {
		return ImportItem{
			ExternalID: "google-calendar:" + event.ID,
			Title:      title + " (cancelled in Google Calendar)",
			Content:    "Google Calendar reports that this event was cancelled. Preserve prior HAI context and obligations for owner review; do not delete tasks, commitments, or evidence automatically.",
			SourceURI:  sourceURI,
			ItemType:   "google_calendar_event_cancelled",
			ProjectKey: projectKey,
			Metadata:   calendarEventMetadata(event, true),
		}
	}
	lines := []string{"Google Calendar event: " + title}
	if start := calendarEventTime(event.Start); start != "" {
		lines = append(lines, "Start: "+start)
	}
	if end := calendarEventTime(event.End); end != "" {
		lines = append(lines, "End: "+end)
	}
	if event.Status != "" {
		lines = append(lines, "Status: "+compact(event.Status, 40))
	}
	if event.EventType != "" {
		lines = append(lines, "Event type: "+compact(event.EventType, 80))
	}
	if location := compact(strings.TrimSpace(event.Location), 500); location != "" {
		lines = append(lines, "Location: "+location)
	}
	if organizer := compact(firstNonEmpty(event.Organizer.DisplayName, event.Organizer.Email), 160); organizer != "" {
		lines = append(lines, "Organizer: "+organizer)
	}
	if attendees := calendarAttendeeSummary(event.Attendees); attendees != "" {
		lines = append(lines, "Attendees: "+attendees)
	}
	if description := compact(strings.TrimSpace(event.Description), 4000); description != "" {
		lines = append(lines, "Description: "+description)
	}
	return ImportItem{
		ExternalID: "google-calendar:" + event.ID,
		Title:      title,
		Content:    strings.Join(lines, "\n"),
		SourceURI:  sourceURI,
		ItemType:   "google_calendar_event",
		ProjectKey: projectKey,
		Metadata:   calendarEventMetadata(event, false),
	}
}

func calendarEventTime(value googleoauth.CalendarEventTime) string {
	if value.DateTime != "" {
		return compact(value.DateTime, 80)
	}
	return compact(value.Date, 40)
}

func calendarAttendeeSummary(attendees []googleoauth.CalendarAttendee) string {
	const maxAttendees = 20
	values := make([]string, 0, min(len(attendees), maxAttendees))
	for _, attendee := range attendees {
		value := compact(firstNonEmpty(attendee.DisplayName, attendee.Email), 120)
		if value == "" {
			continue
		}
		if attendee.ResponseStatus != "" {
			value += " (" + compact(attendee.ResponseStatus, 40) + ")"
		}
		values = append(values, value)
		if len(values) == maxAttendees {
			break
		}
	}
	if len(attendees) > maxAttendees {
		values = append(values, fmt.Sprintf("and %d more", len(attendees)-maxAttendees))
	}
	return strings.Join(values, ", ")
}

func calendarEventMetadata(event googleoauth.CalendarEvent, cancelled bool) string {
	payload, _ := json.Marshal(map[string]any{
		"source": "google-calendar", "eventId": event.ID,
		"status": event.Status, "eventType": event.EventType,
		"start": calendarEventTime(event.Start), "end": calendarEventTime(event.End),
		"attendeeCount":    len(event.Attendees),
		"recurringEventId": event.RecurringEventID, "cancelled": cancelled,
		"transparency": event.Transparency,
		"readonly":     true, "sourceSupported": true, "reviewRequired": cancelled,
		"writebackAllowed": false,
	})
	return string(payload)
}

type calendarItemMetadata struct {
	Start          string `json:"start"`
	End            string `json:"end"`
	AttendeeCount  int    `json:"attendeeCount"`
	Status         string `json:"status"`
	Cancelled      bool   `json:"cancelled"`
	Transparency   string `json:"transparency"`
	ReviewRequired bool   `json:"reviewRequired"`
}

// CalendarBusyInterval is owner-scoped, read-only capacity evidence derived
// from an ingested Google Calendar event. It never grants scheduling or
// Calendar write authority.
type CalendarBusyInterval struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Title     string    `json:"title"`
	SourceURI string    `json:"sourceUri,omitempty"`
	SourceID  string    `json:"sourceId"`
}

// CalendarBusyIntervalsForOwner returns source-backed busy time for one
// verified owner and a bounded planning horizon. Disabled, revoked, cancelled,
// and explicitly transparent/free events cannot constrain the plan.
func (s *service) CalendarBusyIntervalsForOwner(ownerIdentity string, start, end time.Time) ([]CalendarBusyInterval, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	start = start.UTC()
	end = end.UTC()
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("calendar capacity requires a valid planning window")
	}
	if end.Sub(start) > 31*24*time.Hour {
		return nil, fmt.Errorf("calendar capacity planning window cannot exceed 31 days")
	}
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("calendar source repository is unavailable")
	}

	sources, err := s.repo.FindSourcesForOwner(ownerIdentity, true, false)
	if err != nil {
		return nil, fmt.Errorf("find calendar sources: %w", err)
	}
	intervals := make([]CalendarBusyInterval, 0)
	seen := map[string]bool{}
	for _, connected := range sources {
		if connected.ConnectorKey != calendarConnectorKey || !connected.Enabled || connected.RevokedAt != nil || !strings.EqualFold(connected.Status, "active") {
			continue
		}
		items, err := s.repo.FindRawItems(connected.ID)
		if err != nil {
			return nil, fmt.Errorf("read calendar items for source %s: %w", connected.ID, err)
		}
		for index := range items {
			item := &items[index]
			if item.ItemType != "google_calendar_event" {
				continue
			}
			metadata, ok := parseCalendarItemMetadata(item.Metadata)
			if !ok || metadata.Cancelled || strings.EqualFold(metadata.Status, "cancelled") || strings.EqualFold(metadata.Transparency, "transparent") {
				continue
			}
			itemStart, startOK := parseCalendarItemTime(metadata.Start)
			itemEnd, endOK := parseCalendarItemTime(metadata.End)
			if !startOK || !endOK || !itemStart.Before(itemEnd) || !itemEnd.After(start) || !itemStart.Before(end) {
				continue
			}
			if itemStart.Before(start) {
				itemStart = start
			}
			if itemEnd.After(end) {
				itemEnd = end
			}
			key := item.ExternalID + "|" + itemStart.Format(time.RFC3339Nano) + "|" + itemEnd.Format(time.RFC3339Nano)
			if seen[key] {
				continue
			}
			seen[key] = true
			intervals = append(intervals, CalendarBusyInterval{
				Start: itemStart, End: itemEnd, Title: compact(item.Title, 240),
				SourceURI: item.SourceURI, SourceID: connected.ID.String(),
			})
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].Start.Equal(intervals[j].Start) {
			if intervals[i].End.Equal(intervals[j].End) {
				return intervals[i].Title < intervals[j].Title
			}
			return intervals[i].End.Before(intervals[j].End)
		}
		return intervals[i].Start.Before(intervals[j].Start)
	})
	return intervals, nil
}

func parseCalendarItemMetadata(value string) (calendarItemMetadata, bool) {
	var metadata calendarItemMetadata
	if strings.TrimSpace(value) == "" || json.Unmarshal([]byte(value), &metadata) != nil {
		return calendarItemMetadata{}, false
	}
	return metadata, true
}

func calendarPreparationSignal(raw *models.SourceRawItem, now time.Time) string {
	if raw == nil || raw.ItemType != "google_calendar_event" {
		return ""
	}
	metadata, ok := parseCalendarItemMetadata(raw.Metadata)
	if !ok || metadata.Cancelled {
		return ""
	}
	start, ok := parseCalendarItemTime(metadata.Start)
	if !ok {
		return ""
	}
	end := start
	if parsedEnd, parsed := parseCalendarItemTime(metadata.End); parsed {
		end = parsedEnd
	}
	now = now.UTC()
	if !end.After(now) || start.After(now.Add(14*24*time.Hour)) {
		return ""
	}
	text := strings.ToLower(raw.Title + " " + raw.Content)
	meaningful := metadata.AttendeeCount > 0 || containsAny(text,
		"meeting", "appointment", "hearing", "interview", "deadline", "review", "call",
		"vergadering", "afspraak", "hoorzitting", "gesprek", "controleer", "voorbereid")
	if !meaningful {
		return ""
	}
	return fmt.Sprintf("HAI proposal: review preparation, source material, travel or access needs, and open commitments before %s.", start.Format(time.RFC3339))
}

func parseCalendarItemTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

type calendarPlanningEvent struct {
	ExternalID string
	Title      string
	SourceURI  string
	Start      time.Time
	End        time.Time
}

type calendarConflictMetadata struct {
	EventIDs       []string `json:"eventIds"`
	ConflictActive bool     `json:"conflictActive"`
	ReviewRequired bool     `json:"reviewRequired"`
}

func appendCalendarConflictItems(existing []models.SourceRawItem, incoming []ImportItem, now time.Time) []ImportItem {
	if len(incoming) == 0 {
		return incoming
	}
	now = now.UTC()
	events := map[string]calendarPlanningEvent{}
	existingConflicts := map[string]calendarConflictMetadata{}
	for index := range existing {
		raw := &existing[index]
		if raw.ItemType == "google_calendar_event" {
			if event, ok := calendarPlanningEventFromRaw(raw, now); ok {
				events[event.ExternalID] = event
			}
			continue
		}
		if raw.ItemType == "google_calendar_conflict" {
			if metadata, ok := parseCalendarConflictMetadata(raw.Metadata); ok {
				existingConflicts[raw.ExternalID] = metadata
			}
		}
	}
	incomingIDs := map[string]bool{}
	for _, item := range incoming {
		if !strings.HasPrefix(item.ItemType, "google_calendar_event") {
			continue
		}
		incomingIDs[item.ExternalID] = true
		if item.ItemType == "google_calendar_event_cancelled" {
			delete(events, item.ExternalID)
			continue
		}
		if event, ok := calendarPlanningEventFromImport(item, now); ok {
			events[event.ExternalID] = event
		} else {
			delete(events, item.ExternalID)
		}
	}

	ordered := make([]calendarPlanningEvent, 0, len(events))
	for _, event := range events {
		ordered = append(ordered, event)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Start.Equal(ordered[j].Start) {
			return ordered[i].ExternalID < ordered[j].ExternalID
		}
		return ordered[i].Start.Before(ordered[j].Start)
	})
	active := map[string]calendarConflictMetadata{}
	conflicts := make([]ImportItem, 0)
	for leftIndex, left := range ordered {
		for rightIndex := leftIndex + 1; rightIndex < len(ordered); rightIndex++ {
			right := ordered[rightIndex]
			if !right.Start.Before(left.End) {
				break
			}
			if !left.Start.Before(right.End) || (!incomingIDs[left.ExternalID] && !incomingIDs[right.ExternalID]) {
				continue
			}
			item := calendarConflictImportItem(left, right, true)
			metadata, _ := parseCalendarConflictMetadata(item.Metadata)
			active[item.ExternalID] = metadata
			if len(conflicts) < calendarConflictCap {
				conflicts = append(conflicts, item)
			}
		}
	}
	for externalID, prior := range existingConflicts {
		if len(conflicts) >= calendarConflictCap || len(prior.EventIDs) != 2 {
			continue
		}
		if !incomingIDs[prior.EventIDs[0]] && !incomingIDs[prior.EventIDs[1]] {
			continue
		}
		if _, stillActive := active[externalID]; stillActive {
			continue
		}
		conflicts = append(conflicts, calendarConflictResolvedItem(externalID, prior))
	}
	return append(incoming, conflicts...)
}

func calendarPlanningEventFromRaw(raw *models.SourceRawItem, now time.Time) (calendarPlanningEvent, bool) {
	if raw == nil {
		return calendarPlanningEvent{}, false
	}
	return calendarPlanningEventFromValues(raw.ExternalID, raw.Title, raw.SourceURI, raw.Metadata, now)
}

func calendarPlanningEventFromImport(item ImportItem, now time.Time) (calendarPlanningEvent, bool) {
	return calendarPlanningEventFromValues(item.ExternalID, item.Title, item.SourceURI, item.Metadata, now)
}

func calendarPlanningEventFromValues(externalID, title, sourceURI, metadataValue string, now time.Time) (calendarPlanningEvent, bool) {
	metadata, ok := parseCalendarItemMetadata(metadataValue)
	if !ok || metadata.Cancelled || strings.EqualFold(metadata.Status, "cancelled") {
		return calendarPlanningEvent{}, false
	}
	start, startOK := parseCalendarItemTime(metadata.Start)
	end, endOK := parseCalendarItemTime(metadata.End)
	if !startOK || !endOK || !start.Before(end) || !end.After(now) || start.After(now.Add(calendarPlanHorizon)) {
		return calendarPlanningEvent{}, false
	}
	return calendarPlanningEvent{ExternalID: externalID, Title: title, SourceURI: sourceURI, Start: start, End: end}, true
}

func calendarConflictImportItem(left, right calendarPlanningEvent, active bool) ImportItem {
	if right.ExternalID < left.ExternalID {
		left, right = right, left
	}
	externalID := "google-calendar-conflict:" + hashText(left.ExternalID+"|"+right.ExternalID)
	metadata, _ := json.Marshal(calendarConflictMetadata{
		EventIDs: []string{left.ExternalID, right.ExternalID}, ConflictActive: active, ReviewRequired: true,
	})
	content := strings.Join([]string{
		"Calendar conflict detected between two source-backed events.",
		"Start: " + earliestTime(left.Start, right.Start).Format(time.RFC3339),
		fmt.Sprintf("Event A: %s (%s to %s) %s", left.Title, left.Start.Format(time.RFC3339), left.End.Format(time.RFC3339), left.SourceURI),
		fmt.Sprintf("Event B: %s (%s to %s) %s", right.Title, right.Start.Format(time.RFC3339), right.End.Format(time.RFC3339), right.SourceURI),
		"HAI proposal: review the overlap, travel or access constraints, priorities, and the safest resolution. Do not reschedule or contact attendees without approval.",
	}, "\n")
	return ImportItem{
		ExternalID: externalID,
		Title:      compact("Calendar conflict: "+left.Title+" / "+right.Title, 240),
		Content:    content,
		SourceURI:  firstNonEmpty(left.SourceURI, right.SourceURI),
		ItemType:   "google_calendar_conflict",
		Metadata:   string(metadata),
	}
}

func calendarConflictResolvedItem(externalID string, prior calendarConflictMetadata) ImportItem {
	prior.ConflictActive = false
	prior.ReviewRequired = false
	metadata, _ := json.Marshal(prior)
	return ImportItem{
		ExternalID: externalID,
		Title:      "Calendar conflict resolved by source change",
		Content:    "The previously detected Calendar overlap is no longer present in the current source records. Stop the stale conflict workflow while preserving its audit history.",
		ItemType:   "google_calendar_conflict_resolved",
		Metadata:   string(metadata),
	}
}

func parseCalendarConflictMetadata(value string) (calendarConflictMetadata, bool) {
	var metadata calendarConflictMetadata
	if json.Unmarshal([]byte(value), &metadata) != nil || len(metadata.EventIDs) != 2 {
		return calendarConflictMetadata{}, false
	}
	return metadata, true
}

func calendarConflictWorkflowSignal(raw *models.SourceRawItem) string {
	if raw == nil || raw.ItemType != "google_calendar_conflict" {
		return ""
	}
	return "HAI proposal: review the detected schedule conflict and choose a safe resolution; no Calendar or attendee write-back is authorized."
}

func earliestTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

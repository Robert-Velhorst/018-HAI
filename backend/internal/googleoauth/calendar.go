package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultCalendarBaseURL = "https://www.googleapis.com/calendar/v3"
	maxCalendarBodyBytes   = 8 << 20
)

var ErrCalendarSyncTokenExpired = errors.New("calendar sync token expired")

type CalendarClient struct {
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

type CalendarEventTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
	TimeZone string `json:"timeZone"`
}

type CalendarAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	ResponseStatus string `json:"responseStatus"`
	Self           bool   `json:"self"`
}

type CalendarEvent struct {
	ID               string             `json:"id"`
	Status           string             `json:"status"`
	HTMLLink         string             `json:"htmlLink"`
	Summary          string             `json:"summary"`
	Description      string             `json:"description"`
	Location         string             `json:"location"`
	Start            CalendarEventTime  `json:"start"`
	End              CalendarEventTime  `json:"end"`
	Created          time.Time          `json:"created"`
	Updated          time.Time          `json:"updated"`
	Organizer        CalendarAttendee   `json:"organizer"`
	Attendees        []CalendarAttendee `json:"attendees"`
	RecurringEventID string             `json:"recurringEventId"`
	EventType        string             `json:"eventType"`
	Transparency     string             `json:"transparency"`
	Visibility       string             `json:"visibility"`
	HangoutLink      string             `json:"hangoutLink"`
}

type CalendarEventPage struct {
	Events        []CalendarEvent `json:"items"`
	NextPageToken string          `json:"nextPageToken"`
	NextSyncToken string          `json:"nextSyncToken"`
}

// ListPrimaryEventsPage performs one bounded, read-only Calendar API request.
// timeMin is used only for the initial full sync; Google rejects it together
// with a sync token. pageToken may accompany either phase.
func (c CalendarClient) ListPrimaryEventsPage(
	ctx context.Context,
	pageToken string,
	syncToken string,
	timeMin string,
	pageSize int,
) (CalendarEventPage, error) {
	if !validGoogleEndpoint(c.baseURL(), "www.googleapis.com", "/calendar/v3") {
		return CalendarEventPage{}, fmt.Errorf("calendar API endpoint must use HTTPS on Google's Calendar API host or a loopback test server")
	}
	if pageSize <= 0 || pageSize > 2500 {
		pageSize = 200
	}
	query := url.Values{}
	query.Set("maxResults", strconv.Itoa(pageSize))
	query.Set("singleEvents", "true")
	query.Set("showDeleted", "true")
	query.Set("timeZone", "UTC")
	query.Set("fields", "nextPageToken,nextSyncToken,items(id,status,htmlLink,summary,description,location,start,end,created,updated,organizer,attendees,recurringEventId,eventType,transparency,visibility,hangoutLink)")
	if strings.TrimSpace(pageToken) != "" {
		query.Set("pageToken", pageToken)
	}
	if strings.TrimSpace(syncToken) != "" {
		query.Set("syncToken", syncToken)
	} else if strings.TrimSpace(timeMin) != "" {
		query.Set("timeMin", timeMin)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/calendars/primary/events?"+query.Encode(), nil)
	if err != nil {
		return CalendarEventPage{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.AccessToken)
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return CalendarEventPage{}, fmt.Errorf("calendar request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var cause error
		if response.StatusCode == http.StatusGone {
			cause = ErrCalendarSyncTokenExpired
		}
		return CalendarEventPage{}, newProviderAPIError("Google Calendar", response, cause)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCalendarBodyBytes+1))
	if err != nil {
		return CalendarEventPage{}, err
	}
	if len(body) > maxCalendarBodyBytes {
		return CalendarEventPage{}, fmt.Errorf("calendar response exceeded the safety limit")
	}
	var page CalendarEventPage
	if err := json.Unmarshal(body, &page); err != nil {
		return CalendarEventPage{}, fmt.Errorf("calendar returned unparseable JSON: %w", err)
	}
	return page, nil
}

func (c CalendarClient) baseURL() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultCalendarBaseURL
}

func (c CalendarClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

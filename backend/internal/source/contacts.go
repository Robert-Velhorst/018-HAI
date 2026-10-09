package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
)

const (
	contactsCursorPrefix = "google-contacts:v1:"
	contactsFetchLimit   = 200
)

type contactsCursor struct {
	Version   int    `json:"v"`
	Phase     string `json:"phase"`
	PageToken string `json:"pageToken,omitempty"`
	SyncToken string `json:"syncToken,omitempty"`
}

func encodeContactsCursor(cursor contactsCursor) (string, error) {
	cursor.Version = 1
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return contactsCursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeContactsCursor(value string) (contactsCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return contactsCursor{Version: 1, Phase: "backfill"}, nil
	}
	if !strings.HasPrefix(value, contactsCursorPrefix) {
		return contactsCursor{}, fmt.Errorf("unsupported Google Contacts cursor; reset or reconnect this source")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, contactsCursorPrefix))
	if err != nil {
		return contactsCursor{}, fmt.Errorf("decode Google Contacts cursor: %w", err)
	}
	var cursor contactsCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return contactsCursor{}, fmt.Errorf("decode Google Contacts cursor: %w", err)
	}
	if cursor.Version != 1 || (cursor.Phase != "backfill" && cursor.Phase != "changes") {
		return contactsCursor{}, fmt.Errorf("unsupported Google Contacts cursor version or phase")
	}
	if cursor.Phase == "changes" && cursor.SyncToken == "" {
		return contactsCursor{}, fmt.Errorf("Google Contacts changes cursor is missing its sync token")
	}
	return cursor, nil
}

func (s *service) fetchContactsSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	access, err := s.googleAccessToken(ctx, source.ID, contactsConnectorKey)
	if err != nil {
		return nil, "", err
	}
	client := googleoauth.PeopleClient{
		AccessToken: access,
		HTTPClient:  s.googleOAuthReadHTTPClient(source.ID, contactsConnectorKey, access),
	}
	return fetchContactsSourceWithClient(ctx, client, source)
}

func fetchContactsSourceWithClient(
	ctx context.Context,
	client googleoauth.PeopleClient,
	source *models.ConnectedSource,
) ([]ImportItem, string, error) {
	return fetchContactsSourceWithClientRecovery(ctx, client, source, true)
}

func fetchContactsSourceWithClientRecovery(
	ctx context.Context,
	client googleoauth.PeopleClient,
	source *models.ConnectedSource,
	allowExpiredTokenRecovery bool,
) ([]ImportItem, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	cursor, err := decodeContactsCursor(source.Cursor)
	if err != nil {
		return nil, "", err
	}
	syncToken := ""
	if cursor.Phase == "changes" {
		syncToken = cursor.SyncToken
	}
	page, err := client.ListConnectionsPage(ctx, cursor.PageToken, syncToken, contactsFetchLimit)
	if errors.Is(err, googleoauth.ErrPeopleSyncTokenExpired) {
		if !allowExpiredTokenRecovery {
			return nil, "", err
		}
		reset := *source
		reset.Cursor = ""
		return fetchContactsSourceWithClientRecovery(ctx, client, &reset, false)
	}
	if err != nil {
		return nil, "", err
	}
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	items := make([]ImportItem, 0, len(page.Connections))
	for _, person := range page.Connections {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		resourceName := strings.TrimSpace(person.ResourceName)
		if !strings.HasPrefix(resourceName, "people/") || strings.TrimSpace(strings.TrimPrefix(resourceName, "people/")) == "" {
			return nil, "", fmt.Errorf("Google Contacts person is missing a valid resource name")
		}
		person.ResourceName = resourceName
		items = append(items, contactToImportItem(person, projectKey))
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if page.NextPageToken != "" {
		cursor.PageToken = page.NextPageToken
	} else {
		if page.NextSyncToken == "" {
			return nil, "", fmt.Errorf("Google Contacts response returned no continuation sync token")
		}
		cursor.Phase = "changes"
		cursor.PageToken = ""
		cursor.SyncToken = page.NextSyncToken
	}
	next, err := encodeContactsCursor(cursor)
	return items, next, err
}

func contactToImportItem(person googleoauth.Person, projectKey string) ImportItem {
	name := "(unnamed Google contact)"
	if len(person.Names) > 0 {
		name = firstNonEmpty(person.Names[0].DisplayName, name)
	}
	if person.Metadata.Deleted {
		return ImportItem{
			ExternalID: "google-contact:" + person.ResourceName,
			Title:      name + " (removed from Google Contacts)",
			Content:    "Google Contacts reports that this contact was removed. Preserve prior HAI context for owner review; do not delete or merge records automatically.",
			SourceURI:  "https://contacts.google.com/person/" + strings.TrimPrefix(person.ResourceName, "people/"),
			ItemType:   "google_contact_removed",
			ProjectKey: projectKey,
			Metadata:   contactMetadata(person, true),
		}
	}
	lines := []string{"Google contact candidate: " + name}
	for _, email := range person.EmailAddresses {
		if value := strings.TrimSpace(email.Value); value != "" {
			lines = append(lines, "Email: "+value)
		}
	}
	for _, phone := range person.PhoneNumbers {
		if value := strings.TrimSpace(phone.Value); value != "" {
			lines = append(lines, "Phone: "+value)
		}
	}
	for _, organization := range person.Organizations {
		value := strings.TrimSpace(strings.Join([]string{organization.Name, organization.Title}, " - "))
		if value != "" && value != "-" {
			lines = append(lines, "Organization: "+value)
		}
	}
	return ImportItem{
		ExternalID: "google-contact:" + person.ResourceName,
		Title:      name,
		Content:    strings.Join(lines, "\n"),
		SourceURI:  "https://contacts.google.com/person/" + strings.TrimPrefix(person.ResourceName, "people/"),
		ItemType:   "google_contact",
		ProjectKey: projectKey,
		Metadata:   contactMetadata(person, false),
	}
}

func contactMetadata(person googleoauth.Person, removed bool) string {
	payload, _ := json.Marshal(map[string]any{
		"source": "google-contacts", "resourceName": person.ResourceName,
		"etag": person.Etag, "removed": removed, "readonly": true,
		"reviewRequired": true, "writebackAllowed": false,
	})
	return string(payload)
}

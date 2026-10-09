package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
)

const driveCursorPrefix = "drive:v1:"

const driveContentSizeLimitStatus = "size_limit"

type driveCursor struct {
	Version     int    `json:"v"`
	Phase       string `json:"phase"`
	PageToken   string `json:"pageToken,omitempty"`
	ChangeToken string `json:"changeToken,omitempty"`
}

func encodeDriveCursor(cursor driveCursor) (string, error) {
	cursor.Version = 1
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return driveCursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeDriveCursor(value string) (driveCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return driveCursor{Version: 1, Phase: "backfill"}, nil
	}
	if !strings.HasPrefix(value, driveCursorPrefix) {
		return driveCursor{}, fmt.Errorf("unsupported Drive cursor; reset or reconnect this source")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, driveCursorPrefix))
	if err != nil {
		return driveCursor{}, fmt.Errorf("decode Drive cursor: %w", err)
	}
	var cursor driveCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return driveCursor{}, fmt.Errorf("decode Drive cursor: %w", err)
	}
	if cursor.Version != 1 || (cursor.Phase != "backfill" && cursor.Phase != "changes") {
		return driveCursor{}, fmt.Errorf("unsupported Drive cursor version or phase")
	}
	return cursor, nil
}

func (s *service) fetchDriveSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	access, err := s.googleAccessToken(ctx, source.ID, driveConnectorKey)
	if err != nil {
		return nil, "", err
	}
	client := googleoauth.DriveClient{
		AccessToken: access,
		HTTPClient:  s.googleOAuthReadHTTPClient(source.ID, driveConnectorKey, access),
	}
	return fetchDriveSourceWithClient(ctx, client, source)
}

func fetchDriveSourceWithClient(ctx context.Context, client googleoauth.DriveClient, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	cursor, err := decodeDriveCursor(source.Cursor)
	if err != nil {
		return nil, "", err
	}
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	if cursor.Phase == "backfill" {
		if cursor.ChangeToken == "" {
			cursor.ChangeToken, err = client.GetStartPageToken(ctx)
			if err != nil {
				return nil, "", googleProviderSyncError("Google Drive", fmt.Errorf("capture Drive changes boundary: %w", err))
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		page, err := client.ListFilesPage(ctx, cursor.PageToken, driveFetchLimit)
		if err != nil {
			return nil, "", googleProviderSyncError("Google Drive", err)
		}
		if page.NextPageToken != "" && page.NextPageToken == cursor.PageToken {
			return nil, "", fmt.Errorf("Google Drive backfill page did not advance its provider cursor")
		}
		items, err := driveFilesToImportItems(ctx, client, page.Files, projectKey)
		if err != nil {
			return nil, "", err
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if page.NextPageToken != "" {
			cursor.PageToken = page.NextPageToken
		} else {
			cursor.Phase = "changes"
			cursor.PageToken = cursor.ChangeToken
			cursor.ChangeToken = ""
		}
		next, err := encodeDriveCursor(cursor)
		return items, next, err
	}

	page, err := client.ListChangesPage(ctx, cursor.PageToken, driveFetchLimit)
	if err != nil {
		return nil, "", googleProviderSyncError("Google Drive", err)
	}
	if page.NextPageToken != "" && page.NextPageToken == cursor.PageToken {
		return nil, "", fmt.Errorf("Google Drive changes page did not advance its provider cursor")
	}
	items := make([]ImportItem, 0, len(page.Changes))
	for _, change := range page.Changes {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		fileID := strings.TrimSpace(change.FileID)
		if fileID == "" {
			if change.File != nil {
				fileID = strings.TrimSpace(change.File.ID)
			}
			if fileID == "" {
				if change.File != nil {
					return nil, "", fmt.Errorf("Drive file change is missing its file ID")
				}
				// Shared-drive metadata changes are not file records and have no file ID.
				continue
			}
		}
		if change.File != nil && strings.TrimSpace(change.File.ID) != "" && strings.TrimSpace(change.File.ID) != fileID {
			return nil, "", fmt.Errorf("Drive change file ID %q does not match change ID %q", change.File.ID, fileID)
		}
		if change.Removed || change.File == nil || change.File.Trashed {
			items = append(items, ImportItem{
				ExternalID: "drive:" + fileID,
				Title:      "Drive item no longer accessible",
				Content:    "Google Drive reports that this item was removed, trashed, or is no longer accessible. Preserve prior conclusions for review; do not treat removal as permission to delete HAI records.",
				SourceURI:  "https://drive.google.com/open?id=" + fileID,
				ItemType:   "drive_file_removed",
				ProjectKey: projectKey,
				Metadata:   driveMetadata(fileID, "", change.Time, 0, false, true, "", "removed"),
			})
			continue
		}
		item, err := driveFileToImportItem(ctx, client, *change.File, projectKey)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	nextToken := firstNonEmpty(page.NextPageToken, page.NewStartPageToken)
	if nextToken == "" {
		return nil, "", fmt.Errorf("Drive changes response returned no continuation token")
	}
	cursor.PageToken = nextToken
	next, err := encodeDriveCursor(cursor)
	return items, next, err
}

func driveFilesToImportItems(ctx context.Context, client googleoauth.DriveClient, files []googleoauth.DriveFile, projectKey string) ([]ImportItem, error) {
	items := make([]ImportItem, 0, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Trashed || file.MimeType == "application/vnd.google-apps.folder" {
			continue
		}
		item, err := driveFileToImportItem(ctx, client, file, projectKey)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func driveFileToImportItem(ctx context.Context, client googleoauth.DriveClient, file googleoauth.DriveFile, projectKey string) (ImportItem, error) {
	fileID := strings.TrimSpace(file.ID)
	if fileID == "" {
		return ImportItem{}, fmt.Errorf("Drive file is missing its file ID")
	}
	file.ID = fileID
	text, fetched, fetchErr := client.FetchText(ctx, file)
	contentFetchError := ""
	contentStatus := "fetched"
	if errors.Is(fetchErr, googleoauth.ErrDriveContentTooLarge) {
		contentStatus = driveContentSizeLimitStatus
		contentFetchError = "The file's textual content exceeds HAI's extraction-size limit; file metadata was preserved for review."
	} else if errors.Is(fetchErr, googleoauth.ErrDriveResourceUnavailable) {
		contentStatus = "unavailable"
		contentFetchError = "The file is no longer accessible or no longer exists; prior HAI evidence is preserved for review."
	} else if fetchErr != nil {
		return ImportItem{}, googleProviderSyncError("Google Drive", fmt.Errorf("fetch Drive file %s content: %w", fileID, fetchErr))
	} else if !fetched {
		contentStatus = driveUnsupportedContentStatus(file)
	} else if strings.TrimSpace(text) == "" {
		contentStatus = "empty_content"
	}
	if err := ctx.Err(); err != nil {
		return ImportItem{}, err
	}
	content := strings.Join([]string{
		"Google Drive file: " + firstNonEmpty(file.Name, "(untitled)"),
		"MIME type: " + firstNonEmpty(file.MimeType, "unknown"),
		"Modified: " + formatOptionalTime(file.ModifiedTime),
	}, "\n")
	if fetched && strings.TrimSpace(text) != "" {
		content += "\n\nExtracted content:\n" + text
	} else if contentStatus != "unavailable" {
		content += "\n\nContent not extracted: " + contentStatus + ". The file metadata was retained for review."
	}
	if contentFetchError != "" {
		content += "\n\nContent unavailable: " + contentFetchError
	}
	sourceURI := firstNonEmpty(file.WebViewLink, "https://drive.google.com/open?id="+fileID)
	return ImportItem{
		ExternalID: "drive:" + fileID,
		Title:      firstNonEmpty(file.Name, "(untitled Drive file)"),
		Content:    content,
		SourceURI:  sourceURI,
		ItemType:   "drive_file",
		ProjectKey: projectKey,
		Metadata:   driveMetadata(fileID, file.MimeType, file.ModifiedTime, file.Size, fetched, false, contentFetchError, contentStatus),
	}, nil
}

func driveUnsupportedContentStatus(file googleoauth.DriveFile) string {
	if sourceDriveTextMime(file.MimeType) && file.Size > 1<<20 {
		return driveContentSizeLimitStatus
	}
	if file.MimeType == "application/vnd.google-apps.document" || file.MimeType == "application/vnd.google-apps.spreadsheet" || sourceDriveTextMime(file.MimeType) {
		return "not_fetched"
	}
	return "unsupported_mime"
}

func sourceDriveTextMime(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/yaml" ||
		mimeType == "application/x-yaml"
}

func driveMetadata(fileID, mimeType string, modified time.Time, size int64, contentFetched, removed bool, contentFetchError, contentStatus string) string {
	metadata := map[string]any{
		"source": "google-drive", "fileId": fileID, "mimeType": mimeType,
		"modifiedTime": formatOptionalTime(modified), "size": size,
		"contentFetched": contentFetched, "removed": removed, "readonly": true,
	}
	if strings.TrimSpace(contentStatus) != "" {
		metadata["contentStatus"] = contentStatus
	}
	if contentFetchError != "" {
		metadata["contentFetchError"] = contentFetchError
	}
	if contentStatus == driveContentSizeLimitStatus {
		metadata["reviewRequired"] = true
		metadata["reviewReason"] = "content_exceeds_extraction_limit"
		metadata["contentLimitBytes"] = 1 << 20
	}
	payload, _ := json.Marshal(metadata)
	return string(payload)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}

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

func TestDriveLargeBinaryRemainsUnsupportedInsteadOfTextSizeLimit(t *testing.T) {
	status := driveUnsupportedContentStatus(googleoauth.DriveFile{
		MimeType: "application/pdf",
		Size:     (1 << 20) + 1,
	})
	if status != "unsupported_mime" {
		t.Fatalf("large binary content status = %q; want unsupported_mime", status)
	}
}

func TestDriveBackfillCapturesBoundaryAndAdvancesPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"nextPageToken":"files-2","files":[{"id":"doc-1","name":"Project plan","mimeType":"application/vnd.google-apps.document","modifiedTime":"2026-08-03T10:00:00Z","webViewLink":"https://drive.google.com/document/d/doc-1"}]}`))
		case "/files/doc-1/export":
			_, _ = w.Write([]byte("Decision: use live incremental sync. Follow up: verify the workflow."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{DefaultProjectKey: "018-HAI"}
	items, cursorValue, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err != nil {
		t.Fatalf("fetchDriveSourceWithClient: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "drive:doc-1" || items[0].SourceURI != "https://drive.google.com/document/d/doc-1" ||
		!strings.Contains(items[0].Content, "live incremental sync") || !strings.Contains(items[0].Metadata, `"fileId":"doc-1"`) ||
		!strings.Contains(items[0].Metadata, `"contentFetched":true`) {
		t.Fatalf("items = %#v", items)
	}
	cursor, err := decodeDriveCursor(cursorValue)
	if err != nil || cursor.Phase != "backfill" || cursor.PageToken != "files-2" || cursor.ChangeToken != "changes-10" {
		t.Fatalf("cursor = %#v, %v", cursor, err)
	}
}

func TestDriveRepeatedPageTokensDoNotAdvanceCursor(t *testing.T) {
	for _, phase := range []string{"backfill", "changes"} {
		t.Run(phase, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if phase == "backfill" && r.URL.Path == "/files" {
					_, _ = w.Write([]byte(`{"files":[],"nextPageToken":"repeat"}`))
					return
				}
				if phase == "changes" && r.URL.Path == "/changes" {
					_, _ = w.Write([]byte(`{"changes":[],"nextPageToken":"repeat"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			original, _ := encodeDriveCursor(driveCursor{Phase: phase, PageToken: "repeat", ChangeToken: "boundary"})
			items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: original})
			if err == nil || len(items) != 0 || next != "" {
				t.Fatalf("items=%#v next=%q err=%v; repeated provider token must fail without checkpoint", items, next, err)
			}
		})
	}
}

func TestDriveChangesRetainRemovalAsReviewableTombstone(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"newStartPageToken":"changes-11","changes":[{"fileId":"gone","removed":true,"time":"2026-08-03T11:00:00Z"}]}`))
	}))
	defer server.Close()

	source := &models.ConnectedSource{Cursor: cursorValue, DefaultProjectKey: "018-HAI"}
	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err != nil || len(items) != 1 || items[0].ExternalID != "drive:gone" || items[0].SourceURI != "https://drive.google.com/open?id=gone" ||
		items[0].ItemType != "drive_file_removed" || !strings.Contains(items[0].Content, "do not treat removal as permission") ||
		!strings.Contains(items[0].Metadata, `"removed":true`) {
		t.Fatalf("items=%#v next=%q err=%v", items, next, err)
	}
	decoded, err := decodeDriveCursor(next)
	if err != nil || decoded.PageToken != "changes-11" {
		t.Fatalf("next cursor = %#v, %v", decoded, err)
	}
}

func TestDriveTextFetchFailureDoesNotAdvanceCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"doc-1","name":"Plan","mimeType":"application/vnd.google-apps.document"}]}`))
		case "/files/doc-1/export":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{}
	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err == nil || len(items) != 0 || next != "" || source.Cursor != "" {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; failed content fetch must leave the page retryable", items, next, source.Cursor, err)
	}
}

func TestDriveTransientOversizedExportFailureRemainsRetryable(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "backfill", ChangeToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"large-doc","name":"Large document","mimeType":"application/vnd.google-apps.document"}]}`))
		case "/files/large-doc/export":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{Cursor: cursorValue}
	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.upstreamStatus != http.StatusServiceUnavailable ||
		len(items) != 0 || next != "" || source.Cursor != cursorValue {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; transient oversized error bodies must preserve the cursor and retry", items, next, source.Cursor, err)
	}
}

func TestDriveUnavailableFileRemainsReviewableAndBackfillAdvances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"doc-1","name":"Plan","mimeType":"application/vnd.google-apps.document"}]}`))
		case "/files/doc-1/export":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"File not found"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v next=%q err=%v; one inaccessible file should be preserved as reviewable metadata", items, next, err)
	}
	if items[0].ExternalID != "drive:doc-1" || items[0].ItemType != "drive_file" ||
		!strings.Contains(items[0].Content, "prior HAI evidence is preserved for review") ||
		!strings.Contains(items[0].Metadata, "contentFetchError") {
		t.Fatalf("unavailable Drive file was not marked for review: %#v", items[0])
	}
	cursor, err := decodeDriveCursor(next)
	if err != nil || cursor.Phase != "changes" || cursor.PageToken != "changes-10" || cursor.ChangeToken != "" {
		t.Fatalf("backfill cursor = %#v, %v; a permanent per-file 404 should not pin the inventory page", cursor, err)
	}
}

func TestDriveUnavailableChangedFileAdvancesChangeCursor(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes":
			_, _ = w.Write([]byte(`{"newStartPageToken":"changes-11","changes":[{"fileId":"doc-1","file":{"id":"doc-1","name":"Plan","mimeType":"application/vnd.google-apps.document"}}]}`))
		case "/files/doc-1/export":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"File not found"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue})
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Metadata, "contentFetchError") {
		t.Fatalf("items=%#v next=%q err=%v; a file-level 404 should remain reviewable", items, next, err)
	}
	decoded, err := decodeDriveCursor(next)
	if err != nil || decoded.Phase != "changes" || decoded.PageToken != "changes-11" || decoded.ChangeToken != "" {
		t.Fatalf("change cursor = %#v, %v; a permanent file-level 404 should not pin the changes page", decoded, err)
	}
}

func TestDriveUnsupportedContentIsRetainedWithExplicitStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"pdf-1","name":"Evidence.pdf","mimeType":"application/pdf","size":"100"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Content, "unsupported_mime") ||
		!strings.Contains(items[0].Metadata, `"contentStatus":"unsupported_mime"`) || next == "" {
		t.Fatalf("unsupported file outcome was not retained explicitly: items=%#v next=%q err=%v", items, next, err)
	}
}

func TestDriveOversizedTextIsMarkedAndBackfillAdvances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"large-1","name":"Large.txt","mimeType":"text/plain","size":"2097152"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Metadata, `"contentStatus":"size_limit"`) || next == "" {
		t.Fatalf("oversized file outcome was not retained explicitly: items=%#v next=%q err=%v", items, next, err)
	}
}

func TestDriveOversizedExportIsRetainedAndBackfillAdvances(t *testing.T) {
	oversizedText := strings.Repeat("x", (1<<20)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"large-doc","name":"Large document","mimeType":"application/vnd.google-apps.document","modifiedTime":"2026-08-03T10:00:00Z","webViewLink":"https://drive.google.com/document/d/large-doc"},{"id":"normal-doc","name":"Normal document","mimeType":"application/vnd.google-apps.document"}]}`))
		case "/files/large-doc/export":
			_, _ = w.Write([]byte(oversizedText))
		case "/files/normal-doc/export":
			_, _ = w.Write([]byte("ordinary document text"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err != nil || len(items) != 2 || next == "" {
		t.Fatalf("items=%#v next=%q err=%v; oversized export must not pin the backfill page", items, next, err)
	}
	if items[0].ExternalID != "drive:large-doc" || items[0].SourceURI != "https://drive.google.com/document/d/large-doc" ||
		!strings.Contains(items[0].Metadata, `"fileId":"large-doc"`) ||
		!strings.Contains(items[0].Metadata, `"modifiedTime":"2026-08-03T10:00:00Z"`) ||
		!strings.Contains(items[0].Metadata, `"contentStatus":"size_limit"`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewReason":"content_exceeds_extraction_limit"`) ||
		!strings.Contains(items[0].Metadata, `"contentLimitBytes":1048576`) ||
		!strings.Contains(items[0].Metadata, `"contentFetched":false`) ||
		!strings.Contains(items[0].Content, "exceeds HAI's extraction-size limit") ||
		strings.Contains(items[0].Content, oversizedText[:128]) || len(items[0].Content) >= 1<<20 {
		t.Fatalf("oversized export was not retained explicitly: %#v", items[0])
	}
	if !sourceRawItemRequiresReview(&models.SourceRawItem{Metadata: items[0].Metadata}) {
		t.Fatal("oversized-content metadata does not activate the persisted source review marker")
	}
	if items[1].ExternalID != "drive:normal-doc" || !strings.Contains(items[1].Content, "ordinary document text") {
		t.Fatalf("later file was not ingested: %#v", items[1])
	}
	backfillCursor, err := decodeDriveCursor(next)
	if err != nil || backfillCursor.Phase != "changes" || backfillCursor.PageToken != "changes-10" {
		t.Fatalf("backfill cursor = %#v, %v; expected checkpoint after retaining the oversized record", backfillCursor, err)
	}
}

func TestDriveOversizedExportIsRetainedAndChangesCursorAdvances(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes":
			_, _ = w.Write([]byte(`{"newStartPageToken":"changes-11","changes":[{"fileId":"large-doc","time":"2026-08-03T11:00:00Z","file":{"id":"large-doc","name":"Large document","mimeType":"application/vnd.google-apps.document","webViewLink":"https://drive.google.com/document/d/large-doc"}}]}`))
		case "/files/large-doc/export":
			_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue})
	if err != nil || len(items) != 1 || next == "" {
		t.Fatalf("items=%#v next=%q err=%v; oversized changed file must be retained and checkpointable", items, next, err)
	}
	if items[0].ExternalID != "drive:large-doc" || items[0].SourceURI != "https://drive.google.com/document/d/large-doc" ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"contentStatus":"size_limit"`) {
		t.Fatalf("oversized changed file lost review status or provenance: %#v", items[0])
	}
	decoded, err := decodeDriveCursor(next)
	if err != nil || decoded.Phase != "changes" || decoded.PageToken != "changes-11" {
		t.Fatalf("changes cursor = %#v, %v; expected safe advancement to changes-11", decoded, err)
	}
}

func TestDriveCancellationDuringTextFetchDoesNotAdvanceCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"doc-1","name":"Plan","mimeType":"application/vnd.google-apps.document"}]}`))
		case "/files/doc-1/export":
			cancel()
			_, _ = w.Write([]byte("partial response"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{}
	items, next, err := fetchDriveSourceWithClient(ctx, googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if !errors.Is(err, context.Canceled) || len(items) != 0 || next != "" || source.Cursor != "" {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; cancellation must leave the page retryable", items, next, source.Cursor, err)
	}
}

func TestDriveBackfillRejectsFilesWithoutStableIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"changes-10"}`))
		case "/files":
			_, _ = w.Write([]byte(`{"files":[{"name":"Unidentified document","mimeType":"application/vnd.google-apps.document"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	if err == nil || len(items) != 0 || next != "" {
		t.Fatalf("items=%#v next=%q err=%v; records without a provider ID must not be checkpointed", items, next, err)
	}
}

func TestDriveChangesPageTokenIsRetainedUntilFinalPage(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-page-2"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("pageToken"); got != "changes-page-2" {
			t.Fatalf("pageToken = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nextPageToken":"changes-page-3","changes":[{"fileId":"doc-1","removed":true}]}`))
	}))
	defer server.Close()

	_, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue})
	if err != nil {
		t.Fatalf("fetchDriveSourceWithClient: %v", err)
	}
	decoded, err := decodeDriveCursor(next)
	if err != nil || decoded.PageToken != "changes-page-3" {
		t.Fatalf("next cursor = %#v, %v", decoded, err)
	}
}

func TestDriveChangesAPIErrorLeavesCursorRetryable(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary outage"}}`))
	}))
	defer server.Close()

	source := &models.ConnectedSource{Cursor: cursorValue}
	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err == nil || len(items) != 0 || next != "" || source.Cursor != cursorValue {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; API failure must retain the provider cursor", items, next, source.Cursor, err)
	}
}

func TestDriveChangesIgnoreSharedDriveMetadataWithoutFileIdentity(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"newStartPageToken":"changes-11","changes":[{"removed":true}]}`))
	}))
	defer server.Close()

	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: cursorValue})
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v next=%q err=%v; shared-drive metadata must not become a file tombstone", items, next, err)
	}
}

func TestDriveChangesRejectFileRecordsWithoutStableIdentity(t *testing.T) {
	cursorValue, err := encodeDriveCursor(driveCursor{Phase: "changes", PageToken: "changes-10"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"newStartPageToken":"changes-11","changes":[{"file":{"name":"Unidentified document"}}]}`))
	}))
	defer server.Close()

	source := &models.ConnectedSource{Cursor: cursorValue}
	items, next, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err == nil || len(items) != 0 || next != "" || source.Cursor != cursorValue {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; unidentified file changes must not be checkpointed", items, next, source.Cursor, err)
	}
}

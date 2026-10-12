package googleoauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type driveTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper driveTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

type driveTestCountingBody struct {
	reader io.Reader
	read   int
}

func (body *driveTestCountingBody) Read(buffer []byte) (int, error) {
	n, err := body.reader.Read(buffer)
	body.read += n
	return n, err
}

func (body *driveTestCountingBody) Close() error { return nil }

func TestDriveInitialInventoryAndChangesArePaged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"change-10"}`))
		case "/files":
			if r.URL.Query().Get("q") != "trashed = false" || r.URL.Query().Get("pageToken") != "page-1" {
				t.Errorf("files query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"nextPageToken":"page-2","files":[{"id":"f1","name":"Plan.md","mimeType":"text/markdown","modifiedTime":"2026-08-03T10:00:00Z","size":"42","webViewLink":"https://drive.google.com/file/d/f1/view"}]}`))
		case "/changes":
			if r.URL.Query().Get("pageToken") != "change-10" {
				t.Errorf("changes page token = %q", r.URL.Query().Get("pageToken"))
			}
			_, _ = w.Write([]byte(`{"newStartPageToken":"change-12","changes":[{"fileId":"f2","time":"2026-08-03T11:00:00Z","file":{"id":"f2","name":"Notes.txt","mimeType":"text/plain","size":"8"}},{"fileId":"gone","removed":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := DriveClient{AccessToken: "token", BaseURL: server.URL}
	token, err := client.GetStartPageToken(context.Background())
	if err != nil || token != "change-10" {
		t.Fatalf("GetStartPageToken = %q, %v", token, err)
	}
	files, err := client.ListFilesPage(context.Background(), "page-1", 100)
	if err != nil || len(files.Files) != 1 || files.NextPageToken != "page-2" || files.Files[0].Size != 42 {
		t.Fatalf("ListFilesPage = %#v, %v", files, err)
	}
	changes, err := client.ListChangesPage(context.Background(), token, 100)
	if err != nil || len(changes.Changes) != 2 || changes.NewStartPageToken != "change-12" || !changes.Changes[1].Removed {
		t.Fatalf("ListChangesPage = %#v, %v", changes, err)
	}
}

func TestDriveFetchTextExportsDocsAndLeavesBinaryMetadataOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/doc-1/export" || r.URL.Query().Get("mimeType") != "text/plain" {
			t.Errorf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("source-backed document text"))
	}))
	defer server.Close()

	client := DriveClient{AccessToken: "token", BaseURL: server.URL}
	text, fetched, err := client.FetchText(context.Background(), DriveFile{ID: "doc-1", MimeType: "application/vnd.google-apps.document"})
	if err != nil || !fetched || text != "source-backed document text" {
		t.Fatalf("FetchText = %q, %v, %v", text, fetched, err)
	}
	text, fetched, err = client.FetchText(context.Background(), DriveFile{ID: "pdf-1", MimeType: "application/pdf", Size: 100})
	if err != nil || fetched || strings.TrimSpace(text) != "" {
		t.Fatalf("binary FetchText = %q, %v, %v", text, fetched, err)
	}
}

func TestDriveFetchTextClassifiesUnavailableResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"File not found"}}`))
	}))
	defer server.Close()

	_, fetched, err := (DriveClient{AccessToken: "token", BaseURL: server.URL}).FetchText(context.Background(), DriveFile{
		ID: "doc-1", MimeType: "application/vnd.google-apps.document",
	})
	if fetched || !errors.Is(err, ErrDriveResourceUnavailable) {
		t.Fatalf("FetchText = fetched:%v err:%v; expected typed unavailable-resource result", fetched, err)
	}
}

func TestDriveFetchTextClassifiesOversizedExport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/doc-1/export" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", maxDriveTextBytes+1)))
	}))
	defer server.Close()

	_, fetched, err := (DriveClient{AccessToken: "token", BaseURL: server.URL}).FetchText(context.Background(), DriveFile{
		ID: "doc-1", MimeType: "application/vnd.google-apps.document",
	})
	if fetched || !errors.Is(err, ErrDriveContentTooLarge) {
		t.Fatalf("FetchText = fetched:%v err:%v; expected typed size-limit result", fetched, err)
	}
}

func TestDriveOversizedExportReadIsBounded(t *testing.T) {
	body := &driveTestCountingBody{reader: strings.NewReader(strings.Repeat("x", maxDriveTextBytes*4))}
	client := DriveClient{
		AccessToken: "test-token",
		BaseURL:     "http://localhost",
		HTTPClient: &http.Client{Transport: driveTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       body,
				Request:    request,
			}, nil
		})},
	}

	_, fetched, err := client.FetchText(context.Background(), DriveFile{ID: "large-doc", MimeType: "application/vnd.google-apps.document"})
	if fetched || !errors.Is(err, ErrDriveContentTooLarge) {
		t.Fatalf("FetchText = fetched:%v err:%v; expected an oversized-content outcome", fetched, err)
	}
	if body.read != int(maxDriveTextBytes+1) {
		t.Fatalf("read %d bytes, want exactly the %d-byte cap plus one detection byte", body.read, maxDriveTextBytes+1)
	}
}

func TestDriveOversizedTransientErrorRemainsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", maxDriveTextBytes+1)))
	}))
	defer server.Close()

	_, fetched, err := (DriveClient{AccessToken: "token", BaseURL: server.URL}).FetchText(context.Background(), DriveFile{
		ID: "doc-1", MimeType: "application/vnd.google-apps.document",
	})
	if fetched || err == nil || errors.Is(err, ErrDriveContentTooLarge) || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("oversized transient response = fetched:%v err:%v; want provider HTTP 503, not a permanent size-limit result", fetched, err)
	}
}

func TestDriveProviderAPIErrorPreservesSafeRetryMetadata(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"private response content"}`))
			}))
			defer server.Close()

			_, err := (DriveClient{AccessToken: "token", BaseURL: server.URL}).GetStartPageToken(context.Background())
			var apiErr *ProviderAPIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryAfterHeader != "17" {
				t.Fatalf("error = %#v; want typed status %d and Retry-After metadata", err, status)
			}
			if strings.Contains(err.Error(), "private response content") {
				t.Fatalf("provider body leaked through error: %v", err)
			}
			if status == http.StatusNotFound && !errors.Is(err, ErrDriveResourceUnavailable) {
				t.Fatalf("404 error = %v; want ErrDriveResourceUnavailable preserved", err)
			}
		})
	}
}

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
	DefaultDriveBaseURL = "https://www.googleapis.com/drive/v3"
	maxDriveTextBytes   = 1 << 20
	driveFileFields     = "id,name,mimeType,modifiedTime,webViewLink,size,md5Checksum,trashed,parents"
)

var (
	ErrDriveResourceUnavailable = errors.New("Google Drive resource is unavailable")
	ErrDriveContentTooLarge     = errors.New("Google Drive content exceeds the extraction safety limit")
)

// DriveClient is a read-only Google Drive v3 client. It exposes the initial
// file inventory and the provider-native changes cursor separately so callers
// can persist progress after each bounded page.
type DriveClient struct {
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

type DriveFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	ModifiedTime time.Time `json:"-"`
	WebViewLink  string    `json:"webViewLink"`
	Size         int64     `json:"-"`
	MD5Checksum  string    `json:"md5Checksum"`
	Trashed      bool      `json:"trashed"`
	Parents      []string  `json:"parents"`
}

type driveFileWire struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mimeType"`
	ModifiedTime string   `json:"modifiedTime"`
	WebViewLink  string   `json:"webViewLink"`
	Size         string   `json:"size"`
	MD5Checksum  string   `json:"md5Checksum"`
	Trashed      bool     `json:"trashed"`
	Parents      []string `json:"parents"`
}

func (f driveFileWire) model() DriveFile {
	modified, _ := time.Parse(time.RFC3339, f.ModifiedTime)
	size, _ := strconv.ParseInt(f.Size, 10, 64)
	return DriveFile{
		ID: f.ID, Name: f.Name, MimeType: f.MimeType, ModifiedTime: modified,
		WebViewLink: f.WebViewLink, Size: size, MD5Checksum: f.MD5Checksum,
		Trashed: f.Trashed, Parents: append([]string(nil), f.Parents...),
	}
}

type DriveFilePage struct {
	Files         []DriveFile
	NextPageToken string
}

type DriveChange struct {
	FileID  string
	Removed bool
	Time    time.Time
	File    *DriveFile
}

type DriveChangePage struct {
	Changes           []DriveChange
	NextPageToken     string
	NewStartPageToken string
}

func (d DriveClient) baseURL() string {
	if strings.TrimSpace(d.BaseURL) != "" {
		return strings.TrimRight(d.BaseURL, "/")
	}
	return DefaultDriveBaseURL
}

func (d DriveClient) httpClient() *http.Client {
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// GetStartPageToken captures the point from which future changes are read. A
// caller captures this before its initial inventory so concurrent edits are
// replayed after backfill instead of falling through a race window.
func (d DriveClient) GetStartPageToken(ctx context.Context) (string, error) {
	var response struct {
		StartPageToken string `json:"startPageToken"`
	}
	if err := d.getJSON(ctx, "/changes/startPageToken?supportsAllDrives=true", &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.StartPageToken) == "" {
		return "", fmt.Errorf("drive returned no start page token")
	}
	return response.StartPageToken, nil
}

func (d DriveClient) ListFilesPage(ctx context.Context, pageToken string, maxResults int) (DriveFilePage, error) {
	if maxResults <= 0 || maxResults > 1000 {
		maxResults = 100
	}
	q := url.Values{}
	q.Set("pageSize", strconv.Itoa(maxResults))
	q.Set("q", "trashed = false")
	q.Set("spaces", "drive")
	q.Set("corpora", "user")
	q.Set("includeItemsFromAllDrives", "true")
	q.Set("supportsAllDrives", "true")
	q.Set("fields", "nextPageToken,files("+driveFileFields+")")
	if strings.TrimSpace(pageToken) != "" {
		q.Set("pageToken", pageToken)
	}
	var response struct {
		Files         []driveFileWire `json:"files"`
		NextPageToken string          `json:"nextPageToken"`
	}
	if err := d.getJSON(ctx, "/files?"+q.Encode(), &response); err != nil {
		return DriveFilePage{}, err
	}
	page := DriveFilePage{NextPageToken: response.NextPageToken, Files: make([]DriveFile, 0, len(response.Files))}
	for _, file := range response.Files {
		page.Files = append(page.Files, file.model())
	}
	return page, nil
}

func (d DriveClient) ListChangesPage(ctx context.Context, pageToken string, maxResults int) (DriveChangePage, error) {
	if strings.TrimSpace(pageToken) == "" {
		return DriveChangePage{}, fmt.Errorf("drive changes page token is required")
	}
	if maxResults <= 0 || maxResults > 1000 {
		maxResults = 100
	}
	q := url.Values{}
	q.Set("pageToken", pageToken)
	q.Set("pageSize", strconv.Itoa(maxResults))
	q.Set("spaces", "drive")
	q.Set("includeItemsFromAllDrives", "true")
	q.Set("supportsAllDrives", "true")
	q.Set("fields", "nextPageToken,newStartPageToken,changes(fileId,removed,time,file("+driveFileFields+"))")
	var response struct {
		Changes []struct {
			FileID  string         `json:"fileId"`
			Removed bool           `json:"removed"`
			Time    string         `json:"time"`
			File    *driveFileWire `json:"file"`
		} `json:"changes"`
		NextPageToken     string `json:"nextPageToken"`
		NewStartPageToken string `json:"newStartPageToken"`
	}
	if err := d.getJSON(ctx, "/changes?"+q.Encode(), &response); err != nil {
		return DriveChangePage{}, err
	}
	page := DriveChangePage{NextPageToken: response.NextPageToken, NewStartPageToken: response.NewStartPageToken}
	for _, change := range response.Changes {
		changedAt, _ := time.Parse(time.RFC3339, change.Time)
		entry := DriveChange{FileID: change.FileID, Removed: change.Removed, Time: changedAt}
		if change.File != nil {
			file := change.File.model()
			entry.File = &file
		}
		page.Changes = append(page.Changes, entry)
	}
	return page, nil
}

// FetchText returns bounded textual content when Drive can expose it without
// conversion ambiguity. Binary files remain metadata-only and can be routed to
// the governed document-extraction service separately.
func (d DriveClient) FetchText(ctx context.Context, file DriveFile) (string, bool, error) {
	if file.Trashed || strings.TrimSpace(file.ID) == "" {
		return "", false, nil
	}
	var path string
	switch file.MimeType {
	case "application/vnd.google-apps.document":
		path = "/files/" + url.PathEscape(file.ID) + "/export?mimeType=" + url.QueryEscape("text/plain")
	case "application/vnd.google-apps.spreadsheet":
		path = "/files/" + url.PathEscape(file.ID) + "/export?mimeType=" + url.QueryEscape("text/csv")
	default:
		if !driveTextMime(file.MimeType) || file.Size > maxDriveTextBytes {
			return "", false, nil
		}
		path = "/files/" + url.PathEscape(file.ID) + "?alt=media&supportsAllDrives=true"
	}
	body, err := d.getBytes(ctx, path, maxDriveTextBytes)
	if err != nil {
		if errors.Is(err, ErrDriveContentTooLarge) {
			return "", false, fmt.Errorf("%w: Drive content exceeded the %d byte safety limit", ErrDriveContentTooLarge, maxDriveTextBytes)
		}
		return "", false, err
	}
	return strings.TrimSpace(string(body)), true, nil
}

func driveTextMime(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/yaml" ||
		mimeType == "application/x-yaml"
}

func (d DriveClient) getJSON(ctx context.Context, path string, target any) error {
	body, err := d.getBytes(ctx, path, 8<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("drive returned unparseable JSON: %w", err)
	}
	return nil
}

func (d DriveClient) getBytes(ctx context.Context, path string, limit int64) ([]byte, error) {
	if !validGoogleEndpoint(d.baseURL(), "www.googleapis.com", "/drive/v3") {
		return nil, fmt.Errorf("drive API endpoint must use HTTPS on Google's Drive API host or a loopback test server")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.AccessToken)
	req.Header.Set("Accept", "application/json, text/plain, text/csv")
	response, err := d.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var cause error
		if response.StatusCode == http.StatusNotFound {
			cause = ErrDriveResourceUnavailable
		}
		return nil, newProviderAPIError("Google Drive", response, cause)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: drive response exceeded the %d byte safety limit", ErrDriveContentTooLarge, limit)
	}
	return body, nil
}

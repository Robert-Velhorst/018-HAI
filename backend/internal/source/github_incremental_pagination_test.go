package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func configureGitHubPaginationFixture(t *testing.T, server *httptest.Server) {
	t.Helper()
	t.Setenv("GITHUB_SOURCE_API_BASE_URL", server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv("GITHUB_SOURCE_TOKEN", "")
	t.Setenv(githubSourceTokenOwnerIdentityEnv, "")
}

func writeGitHubJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func writeGitHubEmptyEndpoints(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/repos/acme/demo":
		writeGitHubJSON(w, `{"id":1,"full_name":"acme/demo"}`)
		return true
	case "/repos/acme/demo/issues", "/repos/acme/demo/pulls":
		writeGitHubJSON(w, `[]`)
		return true
	}
	return false
}

func TestGitHubIncrementalCommitsUseOverlappingTimeWindowAndAdvanceOnlyAfterComplete(t *testing.T) {
	const oldCursor = "2026-09-24T12:00:00Z"
	firstUpper := time.Date(2026, 9, 24, 12, 0, 10, 0, time.UTC)
	secondUpper := firstUpper.Add(10 * time.Second)
	var commitPages []string
	var commitWindows []url.Values
	pass := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			pass++
			commitPages = append(commitPages, r.URL.Query().Get("page"))
			commitWindows = append(commitWindows, r.URL.Query())
			if pass == 1 && r.URL.Query().Get("page") == "1" {
				records := make([]string, githubSourcePageSize)
				for i := range records {
					records[i] = fmt.Sprintf(`{"sha":"%040x","commit":{"message":"commit %d","committer":{"date":"2026-09-24T12:00:02Z"}}}`, i+1, i+1)
				}
				writeGitHubJSON(w, "["+strings.Join(records, ",")+"]")
				return
			}
			if pass == 2 && r.URL.Query().Get("page") == "2" {
				writeGitHubJSON(w, `[{"sha":"0000000000000000000000000000000000000101","commit":{"message":"tail","committer":{"date":"2026-09-24T12:00:09Z"}}}]`)
				return
			}
			if pass == 3 && r.URL.Query().Get("page") == "1" {
				writeGitHubJSON(w, `[{"sha":"0000000000000000000000000000000000000201","commit":{"message":"boundary","committer":{"date":"2026-09-24T12:00:10Z"}}}]`)
				return
			}
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			writeGitHubJSON(w, `{"total_count":0,"workflow_runs":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	t.Setenv("GITHUB_SOURCE_MAX_PAGES", "2")

	source := &models.ConnectedSource{ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: oldCursor}
	items, firstCursor, err := fetchGitHubSourceAt(context.Background(), source, firstUpper)
	if err != nil {
		t.Fatalf("first incremental scan: %v", err)
	}
	if got := countGitHubItems(items, "github_commit"); got != githubSourcePageSize+1 {
		t.Fatalf("first scan imported %d commits, want %d", got, githubSourcePageSize+1)
	}
	if got := strings.Join(commitPages, ","); got != "1,2" {
		t.Fatalf("first commit pages = %s, want 1,2", got)
	}
	if got := commitWindows[0].Get("since"); got != "2026-09-24T11:59:58Z" {
		t.Fatalf("commit lower bound = %q, want two-second overlap for exclusive since", got)
	}
	if got := commitWindows[0].Get("until"); got != firstUpper.Format(time.RFC3339) {
		t.Fatalf("commit upper bound = %q, want %q", got, firstUpper.Format(time.RFC3339))
	}
	state, err := parseGitHubSourceCursor(firstCursor)
	if err != nil {
		t.Fatalf("parse first cursor: %v", err)
	}
	if state.Commits != firstUpper.Format(time.RFC3339) || state.Runs != firstUpper.Format(time.RFC3339) {
		t.Fatalf("first cursor windows = commits %q, runs %q; want %q", state.Commits, state.Runs, firstUpper.Format(time.RFC3339))
	}

	source.Cursor = firstCursor
	items, secondCursor, err := fetchGitHubSourceAt(context.Background(), source, secondUpper)
	if err != nil {
		t.Fatalf("second incremental scan: %v", err)
	}
	if countGitHubItems(items, "github_commit") != 1 {
		t.Fatalf("boundary scan items = %#v, want one commit at the previous upper bound", items)
	}
	var boundaryID string
	for _, item := range items {
		if item.ItemType == "github_commit" {
			boundaryID = item.ExternalID
		}
	}
	if !strings.Contains(boundaryID, "0000000000000000000000000000000000000201") {
		t.Fatalf("boundary commit ID = %q, want the record at the previous upper bound", boundaryID)
	}
	if got := commitWindows[2].Get("since"); got != "2026-09-24T12:00:08Z" {
		t.Fatalf("second lower bound = %q, want overlap at previous upper bound", got)
	}
	state, err = parseGitHubSourceCursor(secondCursor)
	if err != nil || state.Commits != secondUpper.Format(time.RFC3339) {
		t.Fatalf("second cursor = %#v, error %v; want commit watermark %q", state, err, secondUpper.Format(time.RFC3339))
	}
}

func TestGitHubIncrementalWorkflowRunsSplitDenseCreatedWindowsWithoutGaps(t *testing.T) {
	const oldCursor = "2026-09-24T11:00:00Z"
	upper := time.Date(2026, 9, 24, 11, 0, 20, 0, time.UTC)
	var windows []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			created := r.URL.Query().Get("created")
			windows = append(windows, created)
			if len(windows) == 1 {
				writeGitHubJSON(w, `{"total_count":1000,"workflow_runs":[]}`)
				return
			}
			if len(windows) > 3 {
				http.Error(w, "unexpected extra window", http.StatusBadRequest)
				return
			}
			if len(windows) == 2 {
				writeGitHubJSON(w, `{"total_count":1,"workflow_runs":[{"id":701,"name":"boundary","status":"completed","conclusion":"failure","created_at":"2026-09-24T11:00:09Z","updated_at":"2026-09-24T11:00:09Z"}]}`)
				return
			}
			writeGitHubJSON(w, `{"total_count":2,"workflow_runs":[{"id":701,"name":"boundary duplicate","status":"completed","conclusion":"failure","created_at":"2026-09-24T11:00:09Z","updated_at":"2026-09-24T11:00:09Z"},{"id":702,"name":"later","status":"completed","conclusion":"failure","created_at":"2026-09-24T11:00:15Z","updated_at":"2026-09-24T11:00:15Z"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	t.Setenv("GITHUB_SOURCE_MAX_PAGES", "2")

	items, cursor, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: oldCursor}, upper)
	if err != nil {
		t.Fatalf("fetchGitHubSourceAt: %v", err)
	}
	if got := countGitHubItems(items, "github_workflow_run"); got != 2 {
		t.Fatalf("workflow items = %d, want both split-window records", got)
	}
	if len(windows) != 3 {
		t.Fatalf("created searches = %v, want one parent plus two bounded children", windows)
	}
	if windows[0] != "2026-09-24T10:59:58Z..2026-09-24T11:00:20Z" {
		t.Fatalf("parent created range = %q", windows[0])
	}
	leftStart, leftEnd, ok := parseGitHubCreatedRange(windows[1])
	if !ok {
		t.Fatalf("left created range %q did not parse", windows[1])
	}
	rightStart, rightEnd, ok := parseGitHubCreatedRange(windows[2])
	if !ok {
		t.Fatalf("right created range %q did not parse", windows[2])
	}
	if leftStart.After(rightStart) || leftEnd.Before(rightStart) || rightStart.After(leftEnd) || !rightEnd.Equal(upper) {
		t.Fatalf("split windows do not overlap and cover parent: left=%s..%s right=%s..%s", leftStart, leftEnd, rightStart, rightEnd)
	}
	state, err := parseGitHubSourceCursor(cursor)
	if err != nil || state.Runs != upper.Format(time.RFC3339) {
		t.Fatalf("run cursor = %#v, error %v; want %q", state, err, upper.Format(time.RFC3339))
	}
}

func TestGitHubIncrementalCommitsSplitDenseTimeWindowsAndDeduplicateOverlap(t *testing.T) {
	upper := time.Date(2026, 9, 24, 12, 0, 20, 0, time.UTC)
	var windows []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			windows = append(windows, r.URL.Query())
			if len(windows) == 1 {
				records := make([]string, githubSourcePageSize)
				for i := range records {
					records[i] = fmt.Sprintf(`{"sha":"%040x","commit":{"message":"dense parent","committer":{"date":"2026-09-24T12:00:05Z"}}}`, i+1)
				}
				writeGitHubJSON(w, "["+strings.Join(records, ",")+"]")
				return
			}
			if len(windows) == 2 {
				writeGitHubJSON(w, `[{"sha":"0000000000000000000000000000000000000701","commit":{"message":"boundary","committer":{"date":"2026-09-24T12:00:09Z"}}}]`)
				return
			}
			writeGitHubJSON(w, `[{"sha":"0000000000000000000000000000000000000701","commit":{"message":"boundary duplicate","committer":{"date":"2026-09-24T12:00:09Z"}}},{"sha":"0000000000000000000000000000000000000702","commit":{"message":"later","committer":{"date":"2026-09-24T12:00:15Z"}}}]`)
		case "/repos/acme/demo/actions/runs":
			writeGitHubJSON(w, `{"total_count":0,"workflow_runs":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	t.Setenv("GITHUB_SOURCE_MAX_PAGES", "1")

	items, cursor, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{
		ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: "2026-09-24T12:00:00Z",
	}, upper)
	if err != nil {
		t.Fatalf("fetchGitHubSourceAt: %v", err)
	}
	if got := countGitHubItems(items, "github_commit"); got != 2 {
		t.Fatalf("unique commits = %d, want 2 after overlapping window dedupe", got)
	}
	if len(windows) != 3 {
		t.Fatalf("commit queries = %d, want one parent and two child windows", len(windows))
	}
	leftSince, leftUntil := windows[1].Get("since"), windows[1].Get("until")
	rightSince, rightUntil := windows[2].Get("since"), windows[2].Get("until")
	leftStart, _ := time.Parse(time.RFC3339, leftSince)
	leftEnd, _ := time.Parse(time.RFC3339, leftUntil)
	rightStart, _ := time.Parse(time.RFC3339, rightSince)
	rightEnd, _ := time.Parse(time.RFC3339, rightUntil)
	if leftStart.After(rightStart) || leftEnd.Before(rightStart) || rightStart.After(leftEnd) || !rightEnd.Equal(upper) {
		t.Fatalf("commit windows do not overlap and cover parent: left=%s..%s right=%s..%s", leftSince, leftUntil, rightSince, rightUntil)
	}
	state, err := parseGitHubSourceCursor(cursor)
	if err != nil || state.Commits != upper.Format(time.RFC3339) {
		t.Fatalf("commit cursor = %#v, error %v; want %q", state, err, upper.Format(time.RFC3339))
	}
}

func TestGitHubIncrementalWorkflowRunsPaginateWithinBoundedWindow(t *testing.T) {
	const oldCursor = "2026-09-24T11:00:00Z"
	upper := time.Date(2026, 9, 24, 11, 2, 0, 0, time.UTC)
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			pages = append(pages, strconv.Itoa(page))
			runs := make([]string, 0, githubSourcePageSize)
			if page == 1 {
				for i := 0; i < githubSourcePageSize; i++ {
					runs = append(runs, fmt.Sprintf(`{"id":%d,"name":"run %d","status":"completed","conclusion":"failure","created_at":"2026-09-24T11:01:00Z","updated_at":"2026-09-24T11:01:00Z"}`, 1000+i, i))
				}
			} else if page == 2 {
				runs = append(runs, `{"id":2000,"name":"last run","status":"completed","conclusion":"failure","created_at":"2026-09-24T11:01:01Z","updated_at":"2026-09-24T11:01:01Z"}`)
			}
			writeGitHubJSON(w, fmt.Sprintf(`{"total_count":101,"workflow_runs":[%s]}`, strings.Join(runs, ",")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	t.Setenv("GITHUB_SOURCE_MAX_PAGES", "2")

	items, cursor, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: oldCursor}, upper)
	if err != nil {
		t.Fatalf("fetchGitHubSourceAt: %v", err)
	}
	if countGitHubItems(items, "github_workflow_run") != 101 || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("workflow records/pages = %d/%v, want 101/[1 2]", countGitHubItems(items, "github_workflow_run"), pages)
	}
	state, err := parseGitHubSourceCursor(cursor)
	if err != nil || state.Runs != upper.Format(time.RFC3339) {
		t.Fatalf("run cursor = %#v, error %v; want %q", state, err, upper.Format(time.RFC3339))
	}
}

func TestGitHubPendingWorkflowRunRefreshesCompletionByIDWithoutMovingCreatedWindow(t *testing.T) {
	firstUpper := time.Date(2026, 9, 24, 12, 0, 10, 0, time.UTC)
	secondUpper := firstUpper.Add(10 * time.Second)
	var createdRanges []string
	detailCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			created := r.URL.Query().Get("created")
			createdRanges = append(createdRanges, created)
			writeGitHubJSON(w, `{"total_count":1,"workflow_runs":[{"id":901,"name":"checks","status":"in_progress","created_at":"2026-09-24T12:00:09Z","updated_at":"2026-09-24T12:00:09Z"}]}`)
		case "/repos/acme/demo/actions/runs/901":
			detailCalls++
			writeGitHubJSON(w, `{"id":901,"name":"checks","status":"completed","conclusion":"failure","created_at":"2026-09-24T12:00:09Z","updated_at":"2026-09-24T12:00:18Z"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)

	source := &models.ConnectedSource{ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: "2026-09-24T12:00:00Z"}
	firstItems, firstCursor, err := fetchGitHubSourceAt(context.Background(), source, firstUpper)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if countGitHubItems(firstItems, "github_workflow_run") != 1 {
		t.Fatalf("first sync items = %#v, want the in-progress run", firstItems)
	}
	state, err := parseGitHubSourceCursor(firstCursor)
	if err != nil || !reflect.DeepEqual(state.PendingRuns, []string{"901"}) {
		t.Fatalf("first cursor pending runs = %#v, error %v; want [901]", state.PendingRuns, err)
	}

	source.Cursor = firstCursor
	secondItems, secondCursor, err := fetchGitHubSourceAt(context.Background(), source, secondUpper)
	if err != nil {
		t.Fatalf("completion refresh: %v", err)
	}
	if countGitHubItems(secondItems, "github_workflow_run") != 1 || detailCalls != 1 {
		t.Fatalf("completion refresh items/detail calls = %d/%d, want 1/1", countGitHubItems(secondItems, "github_workflow_run"), detailCalls)
	}
	var refreshed ImportItem
	for _, item := range secondItems {
		if item.ItemType == "github_workflow_run" {
			refreshed = item
		}
	}
	var metadata githubImportMetadata
	if err := json.Unmarshal([]byte(refreshed.Metadata), &metadata); err != nil {
		t.Fatalf("decode refreshed run metadata: %v", err)
	}
	if metadata.Status != "completed" || metadata.Conclusion != "failure" {
		t.Fatalf("refreshed run status/conclusion = %q/%q; want provider values completed/failure", metadata.Status, metadata.Conclusion)
	}
	if !strings.Contains(refreshed.Content, "Status last observed at: "+metadata.FetchedAt) || !strings.Contains(refreshed.Content, "not a live status") {
		t.Fatalf("run content does not clearly label the last status observation: %q", refreshed.Content)
	}
	state, err = parseGitHubSourceCursor(secondCursor)
	if err != nil || len(state.PendingRuns) != 0 {
		t.Fatalf("completed run remains pending in cursor: %#v, error %v", state.PendingRuns, err)
	}
	if len(createdRanges) != 2 || createdRanges[1] != "2026-09-24T12:00:08Z..2026-09-24T12:00:20Z" {
		t.Fatalf("created windows = %v; completion refresh must leave the incremental created window intact", createdRanges)
	}
}

func TestGitHubPendingWorkflowRunRefreshFailureReturnsNoCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			writeGitHubJSON(w, `{"total_count":0,"workflow_runs":[]}`)
		case "/repos/acme/demo/actions/runs/902":
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	upper := time.Date(2026, 9, 24, 12, 0, 20, 0, time.UTC)
	cursor, err := encodeGitHubSourceCursor(githubSourceCursor{
		Version: 1, Updated: "2026-09-24T12:00:10Z", Commits: "2026-09-24T12:00:10Z", Runs: "2026-09-24T12:00:10Z", PendingRuns: []string{"902"},
	})
	if err != nil {
		t.Fatalf("encode pending-run cursor: %v", err)
	}
	_, nextCursor, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: cursor}, upper)
	if err == nil {
		t.Fatal("expected pending-run refresh error")
	}
	if nextCursor != "" {
		t.Fatalf("failed refresh returned cursor %q; want no cursor", nextCursor)
	}
}

func TestGitHubIncrementalIncompleteCommitOrRunScanReturnsNoCursor(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "commits", path: "/repos/acme/demo/commits"},
		{name: "workflow runs", path: "/repos/acme/demo/actions/runs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upper := time.Date(2026, 9, 24, 11, 0, 2, 0, time.UTC)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if writeGitHubEmptyEndpoints(w, r) {
					return
				}
				if r.URL.Path == tt.path {
					if strings.HasSuffix(tt.path, "/commits") {
						records := make([]string, githubSourcePageSize)
						for i := range records {
							records[i] = fmt.Sprintf(`{"sha":"%040x","commit":{"message":"commit","committer":{"date":"2026-09-24T11:00:01Z"}}}`, i+1)
						}
						writeGitHubJSON(w, "["+strings.Join(records, ",")+"]")
						return
					}
					writeGitHubJSON(w, `{"total_count":101,"workflow_runs":[{"id":1,"name":"run","created_at":"2026-09-24T11:00:01Z"}]}`)
					return
				}
				switch r.URL.Path {
				case "/repos/acme/demo/commits":
					writeGitHubJSON(w, `[]`)
				case "/repos/acme/demo/actions/runs":
					writeGitHubJSON(w, `{"total_count":0,"workflow_runs":[]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			configureGitHubPaginationFixture(t, server)
			t.Setenv("GITHUB_SOURCE_MAX_PAGES", "1")

			_, cursor, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{
				ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: "2026-09-24T11:00:00Z",
			}, upper)
			if err == nil {
				t.Fatal("expected truncated scan error")
			}
			if cursor != "" {
				t.Fatalf("incomplete scan cursor = %q, want no cursor", cursor)
			}
		})
	}
}

func TestGitHubIncrementalPaginationUsesOnlyOwnerBoundDeploymentCredential(t *testing.T) {
	var authorization []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		if writeGitHubEmptyEndpoints(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/acme/demo/commits":
			writeGitHubJSON(w, `[]`)
		case "/repos/acme/demo/actions/runs":
			writeGitHubJSON(w, `{"total_count":0,"workflow_runs":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureGitHubPaginationFixture(t, server)
	t.Setenv("GITHUB_SOURCE_TOKEN", "alice-private-token")
	t.Setenv(githubSourceTokenOwnerIdentityEnv, "alice")
	upper := time.Date(2026, 9, 24, 12, 0, 10, 0, time.UTC)

	_, _, err := fetchGitHubSourceAt(context.Background(), &models.ConnectedSource{
		OwnerIdentity: "bob", ConnectorKey: "github", SyncTarget: "acme/demo", Cursor: "2026-09-24T12:00:00Z",
	}, upper)
	if err != nil {
		t.Fatalf("unauthenticated public fixture sync: %v", err)
	}
	if len(authorization) != 5 {
		t.Fatalf("request count = %d, want five configured GitHub endpoints", len(authorization))
	}
	for i, header := range authorization {
		if header != "" {
			t.Fatalf("request %d carried an owner-mismatched credential", i+1)
		}
	}
}

func TestParseGitHubSourceCursorRejectsUnknownOrMalformedValues(t *testing.T) {
	for _, cursor := range []string{"not-a-time", "hai-github-cursor-v9:{}", "hai-github-cursor-v1:{bad"} {
		t.Run(cursor, func(t *testing.T) {
			if _, err := parseGitHubSourceCursor(cursor); err == nil {
				t.Fatalf("parseGitHubSourceCursor(%q) succeeded", cursor)
			}
		})
	}
	legacy, err := parseGitHubSourceCursor("2026-09-24T12:00:00Z")
	if err != nil || legacy.Commits != legacy.Updated || legacy.Runs != legacy.Updated {
		t.Fatalf("legacy cursor = %#v, error %v", legacy, err)
	}
	encoded, err := encodeGitHubSourceCursor(githubSourceCursor{Version: 1, Updated: legacy.Updated, Commits: legacy.Commits, Runs: legacy.Runs})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := parseGitHubSourceCursor(encoded)
	if err != nil || !reflect.DeepEqual(decoded, githubSourceCursor{Version: 1, Updated: legacy.Updated, Commits: legacy.Commits, Runs: legacy.Runs}) {
		t.Fatalf("round-tripped cursor = %#v, error %v", decoded, err)
	}
	if _, err := json.Marshal(decoded); err != nil {
		t.Fatalf("cursor should remain JSON-safe: %v", err)
	}
}

func parseGitHubCreatedRange(value string) (time.Time, time.Time, bool) {
	parts := strings.Split(value, "..")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

func countGitHubItems(items []ImportItem, itemType string) int {
	count := 0
	for _, item := range items {
		if item.ItemType == itemType {
			count++
		}
	}
	return count
}

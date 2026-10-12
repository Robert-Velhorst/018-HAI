package source

// CloudQuery is intentionally external to HAI. This adapter reads only the
// bounded JSONL summary produced by an operator-run `cloudquery sync` command;
// it never executes CloudQuery, reads its config/credentials, or sees raw
// source or destination data.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"automation-hub-backend/internal/models"
)

const (
	cloudQuerySummaryConnectorKey = "cloudquery-summary"
	cloudQuerySummaryCursorPrefix = "cloudquery-summary-offset:"
	cloudQuerySummaryMaxBytes     = 1 << 20
	cloudQuerySummaryMaxLineBytes = 16 << 10
	cloudQuerySummaryDefaultLimit = 50
	cloudQuerySummaryHardLimit    = 100
)

type cloudQuerySummaryConfig struct {
	root       string
	summary    string
	maxEntries int
}

type cloudQuerySyncSummary struct {
	CLIVersion     string                      `json:"cli_version"`
	SyncID         string                      `json:"sync_id"`
	SyncTime       string                      `json:"sync_time"`
	SyncDurationMS int64                       `json:"sync_duration_ms"`
	Resources      int64                       `json:"resources"`
	Sources        []cloudQuerySummaryEndpoint `json:"sources"`
	Destinations   []cloudQuerySummaryEndpoint `json:"destinations"`
	SyncGroupID    string                      `json:"sync_group_id"`
	UnexpectedData json.RawMessage             `json:"-"`
}

type cloudQuerySummaryEndpoint struct {
	Name     string                   `json:"name"`
	Version  string                   `json:"version"`
	Errors   []json.RawMessage        `json:"errors"`
	Warnings []json.RawMessage        `json:"warnings"`
	Tables   []cloudQuerySummaryTable `json:"tables"`
}

type cloudQuerySummaryTable struct {
	Name       string            `json:"name"`
	Resources  int64             `json:"resources"`
	Errors     []json.RawMessage `json:"errors"`
	DurationMS int64             `json:"duration_ms"`
}

func cloudQuerySummaryConfigFromEnv() (cloudQuerySummaryConfig, error) {
	if !envBool("HAI_CLOUDQUERY_SUMMARY_ENABLED") {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_SUMMARY_ENABLED is false")
	}
	root, err := secureCloudQueryPath(strings.TrimSpace(os.Getenv("HAI_CLOUDQUERY_ALLOWED_ROOT")), "HAI_CLOUDQUERY_ALLOWED_ROOT")
	if err != nil {
		return cloudQuerySummaryConfig{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_ALLOWED_ROOT must reference an existing directory")
	}
	summary, err := secureCloudQueryPath(strings.TrimSpace(os.Getenv("HAI_CLOUDQUERY_SUMMARY_PATH")), "HAI_CLOUDQUERY_SUMMARY_PATH")
	if err != nil {
		return cloudQuerySummaryConfig{}, err
	}
	if strings.ToLower(filepath.Ext(summary)) != ".jsonl" {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_SUMMARY_PATH must reference a .jsonl file")
	}
	if !cloudQueryPathWithinRoot(root, summary) {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_SUMMARY_PATH must remain inside HAI_CLOUDQUERY_ALLOWED_ROOT")
	}
	info, err := os.Stat(summary)
	if err != nil || !info.Mode().IsRegular() {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_SUMMARY_PATH must reference an existing regular file")
	}
	if info.Size() > cloudQuerySummaryMaxBytes {
		return cloudQuerySummaryConfig{}, fmt.Errorf("HAI_CLOUDQUERY_SUMMARY_PATH exceeds the 1 MiB safety limit")
	}
	return cloudQuerySummaryConfig{root: root, summary: summary, maxEntries: boundedCloudQuerySummaryLimit(os.Getenv("HAI_CLOUDQUERY_MAX_ENTRIES"))}, nil
}

func secureCloudQueryPath(raw, name string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("%s must be an absolute local path", name)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s must exist and must not contain unresolved links", name)
	}
	return filepath.Clean(resolved), nil
}

func cloudQueryPathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func boundedCloudQuerySummaryLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return cloudQuerySummaryDefaultLimit
	}
	if value > cloudQuerySummaryHardLimit {
		return cloudQuerySummaryHardLimit
	}
	return value
}

func fetchCloudQuerySummary(source *models.ConnectedSource) ([]ImportItem, string, error) {
	return fetchCloudQuerySummaryContext(context.Background(), source)
}

func fetchCloudQuerySummaryContext(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if source == nil {
		return nil, "", fmt.Errorf("CloudQuery summary source is required")
	}
	config, err := cloudQuerySummaryConfigFromEnv()
	if err != nil {
		return nil, "", err
	}
	currentSummary, err := secureCloudQueryPath(config.summary, "HAI_CLOUDQUERY_SUMMARY_PATH")
	if err != nil || !cloudQueryPathWithinRoot(config.root, currentSummary) {
		return nil, "", fmt.Errorf("configured CloudQuery summary is outside its allowed root")
	}
	file, err := os.Open(currentSummary)
	if err != nil {
		return nil, "", fmt.Errorf("open configured CloudQuery summary: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > cloudQuerySummaryMaxBytes {
		return nil, "", fmt.Errorf("configured CloudQuery summary is not a safe regular file")
	}
	offset := cloudQuerySummaryOffset(source.Cursor)
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("seek configured CloudQuery summary: %w", err)
	}
	reader := bufio.NewReaderSize(file, cloudQuerySummaryMaxLineBytes+1)
	items := make([]ImportItem, 0, config.maxEntries)
	for len(items) < config.maxEntries {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		line, readErr := reader.ReadSlice('\n')
		if readErr == bufio.ErrBufferFull {
			return nil, "", fmt.Errorf("CloudQuery summary contains a line over the 16 KiB safety limit")
		}
		if len(line) > cloudQuerySummaryMaxLineBytes {
			return nil, "", fmt.Errorf("CloudQuery summary contains a line over the 16 KiB safety limit")
		}
		if readErr != nil && readErr != io.EOF {
			return nil, "", fmt.Errorf("read configured CloudQuery summary: %w", readErr)
		}
		if readErr == io.EOF {
			// CloudQuery may still be writing the final JSONL line. Do not parse or
			// advance past it until the producer closes the record with a newline.
			break
		}
		offset += int64(len(line))
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		item, err := cloudQuerySummaryItem(source, []byte(line))
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	return items, cloudQuerySummaryCursorPrefix + strconv.FormatInt(offset, 10), nil
}

func cloudQuerySummaryOffset(cursor string) int64 {
	value := strings.TrimPrefix(strings.TrimSpace(cursor), cloudQuerySummaryCursorPrefix)
	if value == strings.TrimSpace(cursor) {
		return 0
	}
	offset, err := strconv.ParseInt(value, 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func cloudQuerySummaryItem(source *models.ConnectedSource, line []byte) (ImportItem, error) {
	var summary cloudQuerySyncSummary
	if err := json.Unmarshal(line, &summary); err != nil {
		return ImportItem{}, fmt.Errorf("CloudQuery summary contains invalid JSONL")
	}
	syncID := strings.TrimSpace(summary.SyncID)
	if syncID == "" {
		return ImportItem{}, fmt.Errorf("CloudQuery summary is missing sync_id")
	}
	sourceNames := cloudQueryEndpointNames(summary.Sources)
	destinationNames := cloudQueryEndpointNames(summary.Destinations)
	sourceErrors, destinationErrors, tableCount := cloudQuerySummaryCounts(summary)
	metadata, _ := json.Marshal(map[string]any{
		"connector":               cloudQuerySummaryConnectorKey,
		"producer":                "operator-run cloudquery sync summary",
		"read_only":               true,
		"sync_id":                 compact(syncID, 160),
		"sync_time":               compact(strings.TrimSpace(summary.SyncTime), 80),
		"sync_duration_ms":        summary.SyncDurationMS,
		"resources":               summary.Resources,
		"source_count":            len(summary.Sources),
		"destination_count":       len(summary.Destinations),
		"source_error_count":      sourceErrors,
		"destination_error_count": destinationErrors,
		"table_count":             tableCount,
	})
	content := fmt.Sprintf("CloudQuery sync summary. Sources: %s. Destinations: %s. Resources: %d. Duration ms: %d. Source errors: %d. Destination errors: %d. Tables reported: %d.", sourceNames, destinationNames, summary.Resources, summary.SyncDurationMS, sourceErrors, destinationErrors, tableCount)
	return ImportItem{
		ExternalID: "cloudquery-summary:" + compact(syncID, 160),
		Title:      compact("CloudQuery sync "+sourceNames+" to "+destinationNames, 180),
		Content:    content,
		SourceURI:  "cloudquery://sync/" + url.PathEscape(compact(syncID, 160)),
		ItemType:   "cloudquery_sync_summary",
		ProjectKey: source.DefaultProjectKey,
		Metadata:   string(metadata),
	}, nil
}

func cloudQueryEndpointNames(endpoints []cloudQuerySummaryEndpoint) string {
	names := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if name := compact(strings.TrimSpace(endpoint.Name), 80); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "not reported"
	}
	if len(names) > 5 {
		names = append(names[:5], "and more")
	}
	return strings.Join(names, ", ")
}

func cloudQuerySummaryCounts(summary cloudQuerySyncSummary) (sourceErrors, destinationErrors, tableCount int) {
	for _, endpoint := range summary.Sources {
		sourceErrors += len(endpoint.Errors)
		tableCount += len(endpoint.Tables)
	}
	for _, endpoint := range summary.Destinations {
		destinationErrors += len(endpoint.Errors)
		tableCount += len(endpoint.Tables)
	}
	return sourceErrors, destinationErrors, tableCount
}

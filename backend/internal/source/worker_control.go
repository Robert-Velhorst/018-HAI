package source

// The Worker Control adapter imports minimized operational events from the VA
// dashboard. The dashboard remains the credential and authorization boundary;
// HAI is read-only and never stores the bearer token in a source record.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	workerControlConnectorKey       = "worker-control"
	workerControlFeedPath           = "/api/integrations/hai/feed"
	workerControlDefaultLimit       = 50
	workerControlMaxLimit           = 100
	workerControlMaxResponseBytes   = 1 << 20
	workerControlConnectorUserAgent = "HAI-Worker-Control-Connector/1.0"
)

type workerControlConfig struct {
	baseURL *url.URL
	token   string
	limit   int
}

func workerControlConfigFromEnv() (workerControlConfig, error) {
	if !envBool("HAI_WORKER_CONTROL_ENABLED") {
		return workerControlConfig{}, fmt.Errorf("HAI_WORKER_CONTROL_ENABLED is false")
	}
	baseURL, err := parseWorkerControlBaseURL(os.Getenv("HAI_WORKER_CONTROL_BASE_URL"))
	if err != nil {
		return workerControlConfig{}, err
	}
	token := strings.TrimSpace(os.Getenv("HAI_WORKER_CONTROL_CONNECTOR_TOKEN"))
	if !strings.HasPrefix(token, "vwc_hai_") || len(token) > 128 {
		return workerControlConfig{}, fmt.Errorf("HAI_WORKER_CONTROL_CONNECTOR_TOKEN is missing or invalid")
	}
	return workerControlConfig{
		baseURL: baseURL,
		token:   token,
		limit:   boundedWorkerControlLimit(os.Getenv("HAI_WORKER_CONTROL_SYNC_LIMIT")),
	}, nil
}

func parseWorkerControlBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("HAI_WORKER_CONTROL_BASE_URL must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("HAI_WORKER_CONTROL_BASE_URL must use HTTP or HTTPS")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("HAI_WORKER_CONTROL_BASE_URL must not contain credentials, query, or fragments")
	}
	if u.Scheme == "http" && !isWorkerControlLocalHost(u.Hostname()) {
		return nil, fmt.Errorf("HAI_WORKER_CONTROL_BASE_URL must use HTTPS unless it targets a local Worker Control runtime")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func isWorkerControlLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || host == "host.docker.internal" || host == "worker-control" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && (address.IsLoopback() || address.IsPrivate())
}

func boundedWorkerControlLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return workerControlDefaultLimit
	}
	if value > workerControlMaxLimit {
		return workerControlMaxLimit
	}
	return value
}

func fetchWorkerControlSource(ctx context.Context, sourceCursor string, defaultProjectKey string) ([]ImportItem, string, error) {
	config, err := workerControlConfigFromEnv()
	if err != nil {
		return nil, "", err
	}
	endpoint := *config.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + workerControlFeedPath
	query := endpoint.Query()
	query.Set("limit", strconv.Itoa(config.limit))
	if strings.TrimSpace(sourceCursor) != "" {
		query.Set("cursor", sourceCursor)
	}
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Worker Control feed request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+config.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", workerControlConnectorUserAgent)
	client := &http.Client{
		Timeout:   sourceHTTPTimeout(),
		Transport: sourceHTTPTransport(),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch Worker Control feed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, workerControlMaxResponseBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read Worker Control feed: %w", err)
	}
	if len(body) > workerControlMaxResponseBytes {
		return nil, "", fmt.Errorf("Worker Control feed exceeds the %d-byte safety limit", workerControlMaxResponseBytes)
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			return nil, "", fmt.Errorf("Worker Control rejected the connector key; rotate it in dashboard Settings")
		}
		return nil, "", fmt.Errorf("Worker Control feed returned HTTP %d", response.StatusCode)
	}
	var envelope jsonFeedEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(body), &envelope); err != nil {
		return nil, "", fmt.Errorf("decode Worker Control feed: %w", err)
	}
	if len(envelope.Items) > config.limit {
		return nil, "", fmt.Errorf("Worker Control returned %d items above the requested %d-item limit", len(envelope.Items), config.limit)
	}
	items := make([]ImportItem, 0, len(envelope.Items))
	for _, item := range envelope.Items {
		if !strings.HasPrefix(item.ExternalID, "worker-control-event:") || item.ItemType != "worker_commitment_event" {
			return nil, "", fmt.Errorf("Worker Control feed returned an unsupported record contract")
		}
		if err := validateWorkerControlProvenanceURI(item.SourceURI); err != nil {
			return nil, "", err
		}
		item.ProjectKey = firstNonEmpty(strings.TrimSpace(item.ProjectKey), defaultProjectKey)
		items = append(items, item)
	}
	return items, firstNonEmpty(strings.TrimSpace(envelope.NextCursor), sourceCursor), nil
}

func validateWorkerControlProvenanceURI(raw string) error {
	uri, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || uri.Scheme != "worker-control" || uri.Host != "commitments" || uri.User != nil || uri.RawQuery != "" || uri.Fragment != "" {
		return fmt.Errorf("Worker Control feed returned an unsafe provenance URI")
	}
	segments := strings.Split(strings.Trim(uri.EscapedPath(), "/"), "/")
	if len(segments) != 3 || segments[1] != "events" || !isWorkerControlIdentifier(segments[0]) || !isWorkerControlIdentifier(segments[2]) {
		return fmt.Errorf("Worker Control feed returned an unsafe provenance URI")
	}
	return nil
}

func isWorkerControlIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == ':') {
			return false
		}
	}
	return true
}

// Package mcppreflight provides a deliberately narrow MCP server review gate.
//
// It can only handshake with explicitly configured local Streamable HTTP MCP
// servers and list their tools. It does not start processes, accept arbitrary
// URLs, retain server responses, or call a tool. That keeps inspection useful
// before a runtime adapter is approved without turning HAI into an unrestricted
// MCP client.
package mcppreflight

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/braincatalog"
)

const (
	serversEnv       = "HAI_MCP_PREFLIGHT_SERVERS"
	tokensEnv        = "HAI_MCP_PREFLIGHT_TOKENS"
	allowRemoteEnv   = "HAI_MCP_PREFLIGHT_ALLOW_REMOTE"
	enabledEnv       = "HAI_MCP_PREFLIGHT_ENABLED"
	timeoutEnv       = "HAI_MCP_PREFLIGHT_TIMEOUT_SECONDS"
	protocolVersion  = "2025-06-18"
	maxResponseBytes = 1 << 20
	maxTools         = 100
)

var serverIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

var preflightCatalogIDs = map[string]bool{
	"chatgpt-codex-mcp-daemon": true,
	"mcp-inspector":            true,
	"github-mcp-server":        true,
	"playwright-mcp":           true,
	"google-genai-toolbox":     true,
	"serena":                   true,
}

// Server is a reviewed MCP endpoint. It intentionally has no auth fields:
// secrets belong in a dedicated adapter after review, never in a listing tool.
type Server struct {
	ID        string `json:"id"`
	CatalogID string `json:"catalogId"`
	URL       string `json:"url"`
	// BearerToken is never serialized. A preflight result is shown in the UI and
	// written to the audit trail, and the secret belongs in neither.
	BearerToken string `json:"-"`
}

// Config controls the optional local-only preflight service.
type Config struct {
	Enabled bool
	Servers []Server
	Timeout time.Duration
	// AllowRemoteEndpoints lifts the local-only restriction on server URLs. It
	// has to be said outright, and a server it admits must then be reached over
	// TLS with a bearer token, so lifting the restriction cannot happen by
	// accident and cannot happen in the clear.
	AllowRemoteEndpoints bool
}

// Tool is the bounded, non-secret portion of a tools/list result. HAI does not
// retain tool schemas or descriptions because those are third-party payloads.
type Tool struct {
	Name           string `json:"name"`
	Title          string `json:"title,omitempty"`
	HasInputSchema bool   `json:"hasInputSchema"`
}

// Result is an auditable result of a read-only preflight. It contains no raw
// MCP response, credentials, headers, or tool arguments.
type Result struct {
	ID               string    `json:"id"`
	ServerID         string    `json:"serverId"`
	CatalogID        string    `json:"catalogId,omitempty"`
	CatalogName      string    `json:"catalogName,omitempty"`
	URL              string    `json:"url,omitempty"`
	Status           string    `json:"status"`
	Detail           string    `json:"detail"`
	ProtocolVersion  string    `json:"protocolVersion,omitempty"`
	ToolCount        int       `json:"toolCount"`
	Tools            []Tool    `json:"tools,omitempty"`
	Truncated        bool      `json:"truncated"`
	ReadOnlyVerified bool      `json:"readOnlyVerified"`
	DurationMs       int64     `json:"durationMs"`
	CheckedAt        time.Time `json:"checkedAt"`
}

// ServerStatus is safe to show in an authenticated operator view.
type ServerStatus struct {
	ID          string  `json:"id"`
	CatalogID   string  `json:"catalogId,omitempty"`
	CatalogName string  `json:"catalogName,omitempty"`
	URL         string  `json:"url,omitempty"`
	Configured  bool    `json:"configured"`
	LastAttempt *Result `json:"lastAttempt,omitempty"`
}

// Overview explains whether the preflight is configured without pretending a
// server was contacted.
type Overview struct {
	Enabled     bool           `json:"enabled"`
	ConfigError string         `json:"configError,omitempty"`
	Scope       string         `json:"scope"`
	Servers     []ServerStatus `json:"servers"`
}

// Service owns the configured review boundary and a bounded in-memory recent
// attempt view. The durable operation/audit ledger remains the place for a
// later, approved execution adapter; preflight itself never executes a tool.
type Service struct {
	config    Config
	configErr string
	client    *http.Client
	now       func() time.Time

	mu       sync.Mutex
	sequence int
	last     map[string]Result
}

// NewService builds a preflight service from explicit config.
func NewService(config Config) *Service {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.Timeout > 30*time.Second {
		config.Timeout = 30 * time.Second
	}
	s := &Service{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			// A configured endpoint must be contacted directly. Following a
			// redirect would defeat the local-endpoint review boundary.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     &http.Transport{Proxy: nil},
		},
		now:  time.Now,
		last: map[string]Result{},
	}
	if config.Enabled {
		s.configErr = validateConfig(config)
	}
	return s
}

// NewServiceFromEnv builds an optional service. The server format is a
// semicolon-separated list, for example:
// HAI_MCP_PREFLIGHT_SERVERS=local-docs@mcp-inspector=http://host.docker.internal:3001/mcp
func NewServiceFromEnv() *Service {
	timeout := 5 * time.Second
	if raw := strings.TrimSpace(os.Getenv(timeoutEnv)); raw != "" {
		if seconds, err := time.ParseDuration(raw + "s"); err == nil {
			timeout = seconds
		}
	}
	return NewService(Config{
		Enabled: strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true"),
		Servers: withBearerTokens(parseServers(os.Getenv(serversEnv)), os.Getenv(tokensEnv)),
		Timeout: timeout,
		AllowRemoteEndpoints: strings.EqualFold(
			strings.TrimSpace(os.Getenv(allowRemoteEnv)), "true",
		),
	})
}

// Overview reports the static configuration and last preflight per server.
func (s *Service) Overview() Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	servers := make([]ServerStatus, 0, len(s.config.Servers))
	for _, server := range s.config.Servers {
		item := ServerStatus{ID: server.ID, CatalogID: server.CatalogID, CatalogName: catalogName(server.CatalogID), URL: server.URL, Configured: s.config.Enabled && s.configErr == ""}
		if attempt, ok := s.last[server.ID]; ok {
			copy := attempt
			item.LastAttempt = &copy
		}
		servers = append(servers, item)
	}
	return Overview{
		Enabled:     s.config.Enabled,
		ConfigError: s.configErr,
		Scope:       "Read-only Streamable HTTP preflight: initialize and tools/list only. HAI never starts an MCP process or calls a listed tool.",
		Servers:     servers,
	}
}

// Preflight performs initialize, initialized notification, and tools/list for
// an explicitly configured local endpoint. A successful result is evidence of
// reachability and declared tools only; it is never execution approval.
func (s *Service) Preflight(ctx context.Context, serverID string) (Result, bool) {
	server, ok := s.server(serverID)
	if !ok {
		return Result{}, false
	}
	start := time.Now()
	result := Result{
		ServerID:    server.ID,
		CatalogID:   server.CatalogID,
		CatalogName: catalogName(server.CatalogID),
		URL:         server.URL,
		CheckedAt:   s.now().UTC(),
	}
	if !s.config.Enabled {
		result.Status = "disabled"
		result.Detail = enabledEnv + " is false"
		return s.record(result, start), true
	}
	if s.configErr != "" {
		result.Status = "blocked"
		result.Detail = "preflight configuration is invalid: " + s.configErr
		return s.record(result, start), true
	}

	init, sessionID, err := s.rpc(ctx, server, "initialize", 1, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "HAI MCP preflight",
			"version": "1.0.0",
		},
	}, "")
	if err != nil {
		result.Status = "failed"
		result.Detail = safeError("initialize", err)
		return s.record(result, start), true
	}
	result.ProtocolVersion = protocolFromResult(init.Result)
	if result.ProtocolVersion != protocolVersion {
		result.Status = "failed"
		result.Detail = "initialize returned an unsupported MCP protocol version"
		return s.record(result, start), true
	}
	if err := s.notifyInitialized(ctx, server, sessionID, result.ProtocolVersion); err != nil {
		result.Status = "failed"
		result.Detail = safeError("initialized notification", err)
		return s.record(result, start), true
	}

	toolsResponse, _, err := s.rpc(ctx, server, "tools/list", 2, map[string]any{}, sessionID)
	if err != nil {
		result.Status = "failed"
		result.Detail = safeError("tools/list", err)
		return s.record(result, start), true
	}
	tools, declaredToolNames, count, truncated, err := boundedTools(toolsResponse.Result)
	if err != nil {
		result.Status = "failed"
		result.Detail = "tools/list returned an invalid result"
		return s.record(result, start), true
	}
	result.Status = "ready"
	result.Detail = "MCP handshake and tool listing completed; no tool was called and no runtime was enabled"
	result.Tools = tools
	result.ToolCount = count
	result.Truncated = truncated
	if allowedTools, contractName, hasContract := readOnlyToolContract(server.CatalogID); hasContract {
		if count == 0 {
			result.Status = "blocked"
			result.Detail = contractName + " declared no reviewed read-only context tools; keep the server blocked until an explicit inspection-only toolset is configured"
			return s.record(result, start), true
		}
		if disallowed := readOnlyToolNameViolations(declaredToolNames, allowedTools); len(disallowed) > 0 {
			result.Status = "blocked"
			result.Detail = contractName + " declared tools outside HAI's reviewed inspection-only context allowlist; keep interactive, file, storage, and unknown tools unavailable"
			return s.record(result, start), true
		}
		result.ReadOnlyVerified = true
	}
	return s.record(result, start), true
}

func (s *Service) server(id string) (Server, bool) {
	for _, server := range s.config.Servers {
		if server.ID == id {
			return server, true
		}
	}
	return Server{}, false
}

func (s *Service) record(result Result, start time.Time) Result {
	result.DurationMs = time.Since(start).Milliseconds()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	result.ID = fmt.Sprintf("mcp-preflight-%d", s.sequence)
	s.last[result.ServerID] = result
	return result
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code int `json:"code"`
}

func (s *Service) rpc(ctx context.Context, server Server, method string, id int, params any, sessionID string) (rpcResponse, string, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	body, err := json.Marshal(payload)
	if err != nil {
		return rpcResponse{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	req.Header.Set("User-Agent", "HAI-MCP-Preflight/1.0")
	if safeBearerToken(server.BearerToken) {
		req.Header.Set("Authorization", "Bearer "+server.BearerToken)
	}
	if sessionID != "" {
		req.Header.Set("MCP-Session-Id", sessionID)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return rpcResponse{}, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return rpcResponse{}, "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	decoded, err := decodeResponse(response.Body, response.Header.Get("Content-Type"))
	if err != nil {
		return rpcResponse{}, "", err
	}
	if !matchesRequestID(decoded.ID, id) {
		return rpcResponse{}, "", fmt.Errorf("response id does not match request")
	}
	if decoded.Error != nil {
		return rpcResponse{}, "", fmt.Errorf("MCP error %d", decoded.Error.Code)
	}
	if len(decoded.Result) == 0 || string(decoded.Result) == "null" {
		return rpcResponse{}, "", fmt.Errorf("MCP result is empty")
	}
	return decoded, strings.TrimSpace(response.Header.Get("MCP-Session-Id")), nil
}

func (s *Service) notifyInitialized(ctx context.Context, server Server, sessionID, version string) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", version)
	req.Header.Set("User-Agent", "HAI-MCP-Preflight/1.0")
	if safeBearerToken(server.BearerToken) {
		req.Header.Set("Authorization", "Bearer "+server.BearerToken)
	}
	if sessionID != "" {
		req.Header.Set("MCP-Session-Id", sessionID)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func decodeResponse(body io.Reader, contentType ...string) (rpcResponse, error) {
	limited := io.LimitReader(body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return rpcResponse{}, err
	}
	if len(data) > maxResponseBytes {
		return rpcResponse{}, fmt.Errorf("response exceeds %d byte limit", maxResponseBytes)
	}
	var decoded rpcResponse
	if len(contentType) > 0 && strings.Contains(strings.ToLower(contentType[0]), "text/event-stream") {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if json.Unmarshal([]byte(candidate), &decoded) == nil && decoded.JSONRPC == "2.0" {
				return decoded, nil
			}
		}
		return rpcResponse{}, fmt.Errorf("response event stream did not contain JSON-RPC")
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return rpcResponse{}, fmt.Errorf("response is not JSON")
	}
	if decoded.JSONRPC != "2.0" {
		return rpcResponse{}, fmt.Errorf("response does not use JSON-RPC 2.0")
	}
	return decoded, nil
}

func protocolFromResult(raw json.RawMessage) string {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ""
	}
	return strings.TrimSpace(result.ProtocolVersion)
}

func matchesRequestID(raw json.RawMessage, expected int) bool {
	return strings.TrimSpace(string(raw)) == strconv.Itoa(expected)
}

func boundedTools(raw json.RawMessage) ([]Tool, []string, int, bool, error) {
	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			Title       string          `json:"title"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, nil, 0, false, err
	}
	count := len(result.Tools)
	declaredNames := make([]string, 0, count)
	for _, item := range result.Tools {
		name := redactDisplay(strings.TrimSpace(item.Name))
		if name == "" {
			name = "redacted-tool"
		}
		declaredNames = append(declaredNames, truncate(name, 128))
	}
	truncated := count > maxTools
	if truncated {
		result.Tools = result.Tools[:maxTools]
	}
	tools := make([]Tool, 0, len(result.Tools))
	for _, item := range result.Tools {
		tools = append(tools, Tool{
			Name:           declaredNames[len(tools)],
			Title:          truncate(redactDisplay(strings.TrimSpace(item.Title)), 160),
			HasInputSchema: len(item.InputSchema) > 0 && string(item.InputSchema) != "null",
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, declaredNames, count, truncated, nil
}

var githubReadOnlyContextTools = map[string]struct{}{
	"get_commit": {}, "get_file_contents": {}, "get_pull_request": {},
	"get_pull_request_diff": {}, "get_pull_request_files": {}, "get_repository": {},
	"get_workflow_run_logs": {}, "issue_read": {}, "list_branches": {},
	"list_commits": {}, "list_issues": {}, "list_pull_requests": {},
	"list_workflow_runs": {}, "pull_request_read": {}, "search_code": {},
	"search_issues": {}, "search_pull_requests": {}, "search_repositories": {},
}

var playwrightReadOnlyContextTools = map[string]struct{}{
	"browser_console_messages": {}, "browser_find": {}, "browser_get_config": {},
	"browser_route_list": {}, "browser_snapshot": {},
}

var chatgptLogsReadOnlyContextTools = map[string]struct{}{
	"get_context": {}, "get_conversation": {}, "get_message": {}, "get_raw": {},
	"list_conversations": {}, "list_sources": {}, "search": {}, "search_insights": {},
	"search_passages": {}, "stats": {}, "sync_status": {},
}

func readOnlyToolContract(catalogID string) (map[string]struct{}, string, bool) {
	switch strings.TrimSpace(catalogID) {
	case "chatgpt-codex-mcp-daemon":
		return chatgptLogsReadOnlyContextTools, "ChatGPT logs MCP", true
	case "github-mcp-server":
		return githubReadOnlyContextTools, "GitHub MCP", true
	case "playwright-mcp":
		return playwrightReadOnlyContextTools, "Playwright MCP", true
	default:
		return nil, "", false
	}
}

func readOnlyToolNameViolations(names []string, allowedTools map[string]struct{}) []string {
	violations := make([]string, 0)
	for _, declared := range names {
		name := strings.TrimSpace(declared)
		if _, allowed := allowedTools[name]; !allowed {
			violations = append(violations, name)
		}
	}
	sort.Strings(violations)
	return violations
}

func safeError(stage string, err error) string {
	message := strings.TrimSpace(err.Error())
	if strings.HasPrefix(message, "HTTP ") || strings.HasPrefix(message, "MCP error") || strings.HasPrefix(message, "response ") {
		return stage + " failed: " + message
	}
	return stage + " failed; endpoint did not provide a usable response"
}

func redactDisplay(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"password", "secret", "token", "authorization", "api key", "apikey"} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func parseServers(raw string) []Server {
	parts := strings.Split(raw, ";")
	servers := make([]Server, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		profile, endpoint, ok := strings.Cut(part, "=")
		if !ok {
			servers = append(servers, Server{ID: part})
			continue
		}
		id, catalogID, hasCatalogID := strings.Cut(strings.TrimSpace(profile), "@")
		server := Server{ID: strings.TrimSpace(id), URL: strings.TrimSpace(endpoint)}
		if hasCatalogID {
			server.CatalogID = strings.TrimSpace(catalogID)
		}
		servers = append(servers, server)
	}
	return servers
}

// withBearerTokens attaches the token each listener asks for, keyed by server
// id. The tokens live in their own variable rather than inside the server list
// so the endpoint list stays printable in logs and support output.
//
// Format: HAI_MCP_PREFLIGHT_TOKENS=chatgpt-logs=abc123;other-server=def456
func withBearerTokens(servers []Server, raw string) []Server {
	tokens := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, token, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		tokens[strings.TrimSpace(id)] = strings.TrimSpace(token)
	}
	for index := range servers {
		servers[index].BearerToken = tokens[servers[index].ID]
	}
	return servers
}

// safeBearerToken refuses a secret that could become header syntax rather than
// a header value.
func safeBearerToken(token string) bool {
	if token == "" || len([]rune(token)) > 512 {
		return false
	}
	for _, r := range token {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func validateConfig(config Config) string {
	if len(config.Servers) == 0 {
		return serversEnv + " must contain at least one reviewed local server when preflight is enabled"
	}
	seen := map[string]bool{}
	for _, server := range config.Servers {
		if !serverIDPattern.MatchString(server.ID) {
			return "server id must use letters, digits, hyphen, or underscore"
		}
		if seen[server.ID] {
			return "server ids must be unique"
		}
		seen[server.ID] = true
		if err := validateCatalogProfile(server.CatalogID); err != nil {
			return "server " + server.ID + ": " + err.Error()
		}
		if err := validateEndpointURL(server, config.AllowRemoteEndpoints); err != nil {
			return "server " + server.ID + ": " + err.Error()
		}
	}
	return ""
}

func validateCatalogProfile(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("catalog profile is required")
	}
	entry, ok := braincatalog.EntryByID(id)
	if !ok {
		return fmt.Errorf("catalog profile is not reviewed")
	}
	if entry.Status != braincatalog.StatusIntegrated && entry.Status != braincatalog.StatusCandidate && entry.Status != braincatalog.StatusCompatibility {
		return fmt.Errorf("catalog profile is not eligible for MCP preflight")
	}
	if !preflightCatalogIDs[entry.ID] {
		return fmt.Errorf("catalog profile is not an MCP capability")
	}
	return nil
}

func catalogName(id string) string {
	entry, ok := braincatalog.EntryByID(strings.TrimSpace(id))
	if !ok {
		return ""
	}
	return entry.Name
}

func validateEndpointURL(server Server, allowRemote bool) error {
	raw := server.URL
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http/https")
	}
	if u.User != nil {
		return fmt.Errorf("URL credentials are not allowed")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL host is empty")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("URL query and fragment are not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || host == "host.docker.internal" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if !allowRemote {
		return fmt.Errorf(
			"only localhost, loopback IPs, or host.docker.internal are allowed unless %s is true",
			allowRemoteEnv,
		)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("a remote server must use https when %s is true", allowRemoteEnv)
	}
	if !safeBearerToken(server.BearerToken) {
		return fmt.Errorf("a remote server needs a bearer token in %s when %s is true", tokensEnv, allowRemoteEnv)
	}
	return nil
}

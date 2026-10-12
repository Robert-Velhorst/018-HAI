// Package chatgptlogs provides a bounded, read-only task-context adapter for
// an operator-run chatgpt-codex-mcp-daemon endpoint. HAI never starts the MCP
// process and exposes only a statically reviewed tool subset to its model loop.
package chatgptlogs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	enabledEnv             = "HAI_CHATGPT_LOGS_MCP_ENABLED"
	baseURLEnv             = "HAI_CHATGPT_LOGS_MCP_URL"
	timeoutEnv             = "HAI_CHATGPT_LOGS_MCP_TIMEOUT_SECONDS"
	tokenEnv               = "HAI_CHATGPT_LOGS_MCP_TOKEN"
	allowRemoteEnv         = "HAI_CHATGPT_LOGS_MCP_ALLOW_REMOTE"
	protocolVersion        = "2025-06-18"
	searchTool             = "search"
	maxQueryRunes          = 1000
	maxProjectRunes        = 240
	maxToolTextRunes       = 12000
	maxResponseBytes int64 = 128 << 10
)

var (
	ErrNotConfigured  = errors.New("ChatGPT logs MCP context is not configured")
	ErrInvalidRequest = errors.New("invalid ChatGPT logs MCP tool request")
)

type Status struct {
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Endpoint     string   `json:"endpoint,omitempty"`
	ConfigError  string   `json:"configError,omitempty"`
	Capabilities []string `json:"capabilities"`
	Restrictions []string `json:"restrictions"`
	Scope        string   `json:"scope"`
}

type SearchRequest struct {
	Query      string `json:"query"`
	ProjectKey string `json:"projectKey,omitempty"`
}

type CallRequest struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Arguments   string `json:"arguments"`
}

type ContextItem struct {
	Provider   string `json:"provider"`
	Tool       string `json:"tool"`
	Query      string `json:"query"`
	ProjectKey string `json:"projectKey,omitempty"`
	Content    string `json:"content"`
	SourceURI  string `json:"sourceUri"`
	Untrusted  bool   `json:"untrusted"`
}

type Service interface {
	Status() Status
	Tools() []ToolDescriptor
	Call(context.Context, CallRequest) (*ContextItem, error)
	Search(context.Context, SearchRequest) ([]ContextItem, error)
}

type service struct {
	enabled     bool
	baseURL     *url.URL
	bearerToken string
	configErr   string
	client      *http.Client
}

func DefaultService() Service {
	timeout := 8 * time.Second
	if raw := strings.TrimSpace(os.Getenv(timeoutEnv)); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 1 && seconds <= 30 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{Proxy: nil},
	}
	return NewServiceWithOptions(Options{
		Enabled:             strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true"),
		BaseURL:             os.Getenv(baseURLEnv),
		BearerToken:         os.Getenv(tokenEnv),
		AllowRemoteEndpoint: strings.EqualFold(strings.TrimSpace(os.Getenv(allowRemoteEnv)), "true"),
		Client:              client,
	})
}

// Options configures the adapter. A listener may ask callers to identify
// themselves, and it may live somewhere other than this machine; both are
// stated here rather than inferred from the URL.
type Options struct {
	Enabled     bool
	BaseURL     string
	BearerToken string
	// AllowRemoteEndpoint lifts the local-network restriction on BaseURL.
	//
	// By default the adapter may only reach this machine or its own network,
	// which is what keeps a misconfigured URL from turning HAI into a client
	// for an arbitrary host on the internet. Some deployments genuinely keep
	// the corpus elsewhere, so the restriction can be lifted — but only by
	// saying so outright, never as a side effect of writing a public URL, and
	// only over TLS with a bearer token, because otherwise the token and every
	// retrieved conversation cross the internet in the clear.
	AllowRemoteEndpoint bool
	Client              *http.Client
}

func NewService(enabled bool, rawBaseURL string, client *http.Client) Service {
	return NewServiceWithOptions(Options{Enabled: enabled, BaseURL: rawBaseURL, Client: client})
}

func NewServiceWithOptions(options Options) Service {
	enabled, rawBaseURL, bearerToken, client := options.Enabled, options.BaseURL, options.BearerToken, options.Client
	s := &service{enabled: enabled}
	if client == nil {
		client = &http.Client{
			Timeout:       8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     &http.Transport{Proxy: nil},
		}
	}
	s.client = client
	if enabled {
		s.baseURL, s.configErr = parseBaseURL(rawBaseURL, options.AllowRemoteEndpoint)
		if s.configErr == "" {
			s.bearerToken, s.configErr = parseBearerToken(bearerToken)
		}
		if s.configErr == "" {
			s.configErr = checkRemoteEndpointTerms(s.baseURL, s.bearerToken, options.AllowRemoteEndpoint)
		}
	}
	return s
}

// checkRemoteEndpointTerms holds a lifted restriction to the terms that make
// lifting it defensible: encrypted transport and a caller the listener can
// recognise. A local listener needs neither, and is left alone.
func checkRemoteEndpointTerms(endpoint *url.URL, bearerToken string, allowRemote bool) string {
	if !allowRemote || endpoint == nil || isLocalEndpointHost(endpoint.Hostname()) {
		return ""
	}
	if endpoint.Scheme != "https" {
		return baseURLEnv + " must use https when " + allowRemoteEnv + " is true"
	}
	if bearerToken == "" {
		return tokenEnv + " is required when " + allowRemoteEnv + " is true"
	}
	return ""
}

// parseBearerToken keeps a malformed secret out of the request rather than
// letting it become part of the header syntax.
func parseBearerToken(raw string) (string, string) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", ""
	}
	if len([]rune(token)) > 512 {
		return "", "ChatGPT logs MCP bearer token is too long"
	}
	for _, r := range token {
		if r < 0x21 || r > 0x7e {
			return "", "ChatGPT logs MCP bearer token must be printable ASCII without spaces"
		}
	}
	return token, ""
}

func (s *service) Status() Status {
	status := Status{
		Enabled:     s.enabled,
		Configured:  s.configured(),
		ConfigError: s.configErr,
		Capabilities: []string{
			"model-directed bounded conversation-history retrieval",
			"read-only session, message, provenance, and synchronization inspection",
		},
		Restrictions: []string{
			"no MCP process launch, unreviewed tool selection, or write-capable call",
			"per-call argument, result, model-round, tool-call, and aggregate context limits",
			"retrieved text is untrusted context and never grants execution authority",
		},
		Scope: "Opt-in local chatgpt-codex-mcp-daemon retrieval. The model may choose among eleven statically reviewed read-only tools; HAI validates every call and supplies bounded results as untrusted task context.",
	}
	if s.baseURL != nil {
		status.Endpoint = s.baseURL.String()
	}
	return status
}

func (s *service) Tools() []ToolDescriptor {
	return append([]ToolDescriptor(nil), reviewedTools...)
}

func (s *service) Search(ctx context.Context, input SearchRequest) ([]ContextItem, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	query := strings.TrimSpace(input.Query)
	project := strings.TrimSpace(input.ProjectKey)
	if query == "" || utf8.RuneCountInString(query) > maxQueryRunes || strings.ContainsRune(query, '\x00') || utf8.RuneCountInString(project) > maxProjectRunes || strings.ContainsRune(project, '\x00') {
		return nil, ErrInvalidRequest
	}
	arguments := map[string]any{
		"query":     query,
		"limit":     5,
		"offset":    0,
		"order":     "rank",
		"rank_pool": 200,
		"max_chars": maxToolTextRunes,
	}
	if project != "" {
		arguments["project"] = project
	}
	raw, _ := json.Marshal(arguments)
	item, err := s.Call(ctx, CallRequest{Tool: searchTool, Arguments: raw})
	if err != nil {
		return nil, err
	}
	item.Query = query
	item.ProjectKey = project
	return []ContextItem{*item}, nil
}

func (s *service) Call(ctx context.Context, input CallRequest) (*ContextItem, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	tool := strings.TrimSpace(input.Tool)
	arguments, err := normalizeArguments(tool, input.Arguments)
	if err != nil {
		return nil, err
	}
	session, tools, err := s.sessionAndTools(ctx)
	if err != nil {
		return nil, err
	}
	if !hasTool(tools, tool) {
		return nil, fmt.Errorf("ChatGPT logs MCP endpoint does not expose the requested reviewed read-only tool")
	}
	response, _, err := s.rpc(ctx, session, 3, "tools/call", map[string]any{"name": tool, "arguments": arguments})
	if err != nil {
		return nil, fmt.Errorf("ChatGPT logs MCP tool call failed")
	}
	text, err := toolText(response.Result)
	if err != nil {
		return nil, fmt.Errorf("ChatGPT logs MCP tool returned no usable context")
	}
	return &ContextItem{
		Provider:  "chatgpt-codex-mcp-daemon",
		Tool:      tool,
		Content:   truncateRunes(text, maxToolTextRunes),
		SourceURI: s.baseURL.String(),
		Untrusted: true,
	}, nil
}

func (s *service) configured() bool { return s.enabled && s.baseURL != nil && s.configErr == "" }

type mcpTool struct {
	Name string `json:"name"`
}

func (s *service) sessionAndTools(ctx context.Context) (string, []mcpTool, error) {
	init, session, err := s.rpc(ctx, "", 1, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "HAI ChatGPT logs context", "version": "1.0"},
	})
	if err != nil || !matchesID(init.ID, 1) || protocolFromResult(init.Result) != protocolVersion {
		return "", nil, fmt.Errorf("ChatGPT logs MCP initialize failed")
	}
	if _, _, err := s.rpc(ctx, session, 0, "notifications/initialized", map[string]any{}); err != nil {
		return "", nil, fmt.Errorf("ChatGPT logs MCP initialization notification failed")
	}
	listed, _, err := s.rpc(ctx, session, 2, "tools/list", map[string]any{})
	if err != nil || !matchesID(listed.ID, 2) {
		return "", nil, fmt.Errorf("ChatGPT logs MCP tool inventory failed")
	}
	var payload struct {
		Tools []mcpTool `json:"tools"`
	}
	if json.Unmarshal(listed.Result, &payload) != nil {
		return "", nil, fmt.Errorf("ChatGPT logs MCP tool inventory was invalid")
	}
	return session, payload.Tools, nil
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func (s *service) rpc(ctx context.Context, session string, id int, method string, params any) (rpcResponse, string, error) {
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id > 0 {
		payload["id"] = id
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return rpcResponse{}, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, "", err
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("MCP-Protocol-Version", protocolVersion)
	request.Header.Set("User-Agent", "HAI-ChatGPT-Logs-ReadOnly/1.0")
	if s.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.bearerToken)
	}
	if session != "" {
		request.Header.Set("MCP-Session-Id", session)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return rpcResponse{}, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return rpcResponse{}, "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if id == 0 {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return rpcResponse{}, safeSessionID(response.Header.Get("MCP-Session-Id")), err
	}
	decoded, err := decodeRPCResponse(response)
	if err != nil || decoded.JSONRPC != "2.0" || decoded.Error != nil {
		return rpcResponse{}, "", fmt.Errorf("invalid JSON-RPC response")
	}
	return decoded, safeSessionID(response.Header.Get("MCP-Session-Id")), nil
}

func decodeRPCResponse(response *http.Response) (rpcResponse, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || int64(len(data)) > maxResponseBytes {
		return rpcResponse{}, fmt.Errorf("response exceeded limit or could not be read")
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var decoded rpcResponse
			if json.Unmarshal([]byte(candidate), &decoded) == nil && decoded.JSONRPC == "2.0" {
				return decoded, nil
			}
		}
		return rpcResponse{}, fmt.Errorf("event stream contained no JSON-RPC response")
	}
	var decoded rpcResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return rpcResponse{}, err
	}
	return decoded, nil
}

func toolText(raw json.RawMessage) (string, error) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IsError {
		return "", ErrInvalidRequest
	}
	for _, item := range result.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text), nil
		}
	}
	return "", ErrInvalidRequest
}

func isLocalEndpointHost(hostname string) bool {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if host == "localhost" || host == "host.docker.internal" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func parseBaseURL(raw string, allowRemote bool) (*url.URL, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, baseURLEnv + " must be a plain local HTTP(S) MCP URL without credentials, query data, or fragments"
	}
	if !allowRemote && !isLocalEndpointHost(parsed.Hostname()) {
		return nil, baseURLEnv + " must use localhost, host.docker.internal, or a literal local/private IP unless " + allowRemoteEnv + " is true"
	}
	if strings.TrimRight(parsed.Path, "/") == "" {
		return nil, baseURLEnv + " must include the configured MCP path"
	}
	return parsed, ""
}

func protocolFromResult(raw json.RawMessage) string {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(raw, &result)
	return strings.TrimSpace(result.ProtocolVersion)
}

func matchesID(raw json.RawMessage, expected int) bool {
	return strings.TrimSpace(string(raw)) == strconv.Itoa(expected)
}

func hasTool(tools []mcpTool, name string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == name {
			return true
		}
	}
	return false
}

func safeSessionID(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

var reviewedTools = []ToolDescriptor{
	{Name: "list_sources", Description: "Inspect corpus sources, coverage, freshness, and available filter dimensions.", Arguments: `{}`},
	{Name: "list_conversations", Description: "List matching conversations or Codex sessions, newest first, without message text.", Arguments: `{"platform?":"codex|chatgpt|chatgpt_work","project?":"substring","repository?":"substring","machine?":"substring","title_contains?":"substring","updated_since?":"ISO date","updated_before?":"ISO date","status?":"synced|stale|never|syncing|failed","scope?":"managed|history","with_messages_only?":true,"limit?":1-20,"offset?":0+}`},
	{Name: "search_insights", Description: "Recall which past conversation covered something, using generated summaries, decisions, goals and open questions. Try this before full-text search when the question is which conversation rather than what was said.", Arguments: `{"query":"required","platform?":"codex|chatgpt|chatgpt_work","project?":"substring","repository?":"substring","machine?":"substring","conversation_id?":"id","updated_since?":"ISO date","updated_before?":"ISO date","freshness?":"any|fresh|stale","mode?":"hybrid|semantic|keyword","min_similarity?":0.0-1.0,"limit?":1-20,"offset?":0+}`},
	{Name: "search_passages", Description: "Recall the wording of what was actually said, as passages of a conversation with the message range they cover. Follow up with get_context or get_conversation to read around a passage.", Arguments: `{"query":"required","platform?":"codex|chatgpt|chatgpt_work","project?":"substring","repository?":"substring","machine?":"substring","conversation_id?":"id","updated_since?":"ISO date","updated_before?":"ISO date","mode?":"hybrid|semantic|keyword","min_similarity?":0.0-1.0,"per_conversation?":1-20,"text_chars?":200-4000,"limit?":1-20,"offset?":0+}`},
	{Name: "search", Description: "Search message text. Use filters and recent ordering when asking for the latest instruction.", Arguments: `{"query":"required","platform?":"codex|chatgpt|chatgpt_work","project?":"substring","repository?":"substring","machine?":"substring","conversation_id?":"id","roles?":["user","assistant"],"since?":"ISO date","until?":"ISO date","order?":"rank|recent","limit?":1-20,"offset?":0+}`},
	{Name: "get_conversation", Description: "Read a bounded page of one conversation after discovering its conversation_id.", Arguments: `{"conversation_id":"required id","roles?":["user","assistant"],"branches?":false,"include_boilerplate?":false,"limit?":1-20,"offset?":0}`},
	{Name: "get_context", Description: "Read messages around one search hit using its message_id.", Arguments: `{"message_id":"required id","before?":0-20,"after?":0-20,"roles?":["user","assistant"]}`},
	{Name: "get_message", Description: "Read one original message by message_id, with bounded paging.", Arguments: `{"message_id":"required id","offset?":0}`},
	{Name: "get_raw", Description: "Read a bounded page of the original imported record when exact provenance is necessary.", Arguments: `{"message_id?":"id","conversation_id?":"id","offset?":0,"artifacts?":0-20}`},
	{Name: "sync_status", Description: "Inspect recent imports, pending conversations, and failures.", Arguments: `{"source?":"source_id","runs?":0-20,"pending?":0-20,"failures?":0-20}`},
	{Name: "stats", Description: "Inspect bounded corpus counts, date ranges, roles, and project distribution.", Arguments: `{"source?":"source_id","platform?":"codex|chatgpt|chatgpt_work","top_projects?":0-20}`},
}

type argumentRule struct {
	allowed  map[string]string
	required []string
}

var reviewedToolRules = map[string]argumentRule{
	"list_sources": {allowed: map[string]string{"max_chars": "int"}},
	"list_conversations": {allowed: map[string]string{
		"limit": "int", "offset": "int", "source": "string", "platform": "platform", "project": "string", "repository": "string", "machine": "string", "title_contains": "string", "updated_since": "string", "updated_before": "string", "status": "status", "scope": "scope", "with_messages_only": "bool", "max_chars": "int",
	}},
	"search_insights": {allowed: map[string]string{
		"query": "query", "limit": "int", "offset": "int", "source": "string", "platform": "platform", "project": "string", "repository": "string", "machine": "string", "conversation_id": "id", "updated_since": "string", "updated_before": "string", "freshness": "freshness", "mode": "mode", "min_similarity": "ratio", "max_chars": "int",
	}, required: []string{"query"}},
	"search_passages": {allowed: map[string]string{
		"query": "query", "limit": "int", "offset": "int", "source": "string", "platform": "platform", "project": "string", "repository": "string", "machine": "string", "conversation_id": "id", "updated_since": "string", "updated_before": "string", "mode": "mode", "min_similarity": "ratio", "per_conversation": "int", "text_chars": "int", "max_chars": "int",
	}, required: []string{"query"}},
	"search": {allowed: map[string]string{
		"query": "query", "limit": "int", "offset": "int", "source": "string", "platform": "platform", "project": "string", "repository": "string", "machine": "string", "conversation_id": "id", "roles": "roles", "since": "string", "until": "string", "include_boilerplate": "bool", "order": "order", "rank_pool": "int", "max_chars": "int",
	}, required: []string{"query"}},
	"get_conversation": {allowed: map[string]string{
		"conversation_id": "id", "limit": "int", "offset": "int", "roles": "roles", "branches": "bool", "include_boilerplate": "bool", "max_text_chars": "int", "max_chars": "int",
	}, required: []string{"conversation_id"}},
	"get_context": {allowed: map[string]string{
		"message_id": "id", "before": "int", "after": "int", "roles": "roles", "max_text_chars": "int", "max_chars": "int",
	}, required: []string{"message_id"}},
	"get_message": {allowed: map[string]string{"message_id": "id", "offset": "int", "max_chars": "int"}, required: []string{"message_id"}},
	"get_raw":     {allowed: map[string]string{"message_id": "id", "conversation_id": "id", "offset": "int", "max_chars": "int", "artifacts": "int"}},
	"sync_status": {allowed: map[string]string{"runs": "int", "pending": "int", "failures": "int", "source": "string", "max_chars": "int"}},
	"stats":       {allowed: map[string]string{"source": "string", "platform": "platform", "top_projects": "int", "max_chars": "int"}},
}

func normalizeArguments(tool string, raw json.RawMessage) (map[string]any, error) {
	rule, ok := reviewedToolRules[tool]
	if !ok {
		return nil, ErrInvalidRequest
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var input map[string]any
	if decoder.Decode(&input) != nil || input == nil {
		return nil, ErrInvalidRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, ErrInvalidRequest
	}
	for _, required := range rule.required {
		if _, exists := input[required]; !exists {
			return nil, ErrInvalidRequest
		}
	}
	if tool == "get_raw" {
		if _, message := input["message_id"]; !message {
			if _, conversation := input["conversation_id"]; !conversation {
				return nil, ErrInvalidRequest
			}
		}
	}
	output := make(map[string]any, len(input)+1)
	for key, value := range input {
		kind, allowed := rule.allowed[key]
		if !allowed {
			return nil, ErrInvalidRequest
		}
		normalized, err := normalizeArgumentValue(key, kind, value)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		output[key] = normalized
	}
	output["max_chars"] = maxToolTextRunes
	return output, nil
}

func normalizeArgumentValue(key, kind string, value any) (any, error) {
	switch kind {
	case "query", "string", "platform", "status", "scope", "order", "freshness", "mode":
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		limit := 500
		if kind == "query" {
			limit = maxQueryRunes
		}
		if !ok || text == "" || utf8.RuneCountInString(text) > limit || strings.ContainsRune(text, '\x00') {
			return nil, ErrInvalidRequest
		}
		allowed := map[string][]string{
			"platform":  {"codex", "chatgpt", "chatgpt_work"},
			"status":    {"synced", "stale", "never", "syncing", "failed"},
			"scope":     {"managed", "history"},
			"order":     {"rank", "recent"},
			"freshness": {"any", "fresh", "stale"},
			"mode":      {"hybrid", "semantic", "keyword"},
		}
		if choices := allowed[kind]; len(choices) > 0 && !containsString(choices, text) {
			return nil, ErrInvalidRequest
		}
		return text, nil
	case "id":
		switch typed := value.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text == "" || utf8.RuneCountInString(text) > 240 || strings.ContainsRune(text, '\x00') {
				return nil, ErrInvalidRequest
			}
			return text, nil
		case json.Number:
			number, err := typed.Int64()
			if err != nil || number < 1 {
				return nil, ErrInvalidRequest
			}
			return number, nil
		default:
			return nil, ErrInvalidRequest
		}
	case "ratio":
		number, ok := value.(json.Number)
		if !ok {
			return nil, ErrInvalidRequest
		}
		parsed, err := number.Float64()
		if err != nil || math.IsNaN(parsed) || parsed < 0 || parsed > 1 {
			return nil, ErrInvalidRequest
		}
		return parsed, nil
	case "int":
		number, ok := value.(json.Number)
		if !ok {
			return nil, ErrInvalidRequest
		}
		parsed, err := number.Int64()
		if err != nil || parsed < 0 {
			return nil, ErrInvalidRequest
		}
		maximum := int64(100000)
		switch key {
		case "limit":
			maximum = 20
			if parsed < 1 {
				parsed = 1
			}
		case "before", "after", "runs", "pending", "failures", "artifacts", "top_projects":
			maximum = 20
		case "per_conversation":
			maximum = 20
			if parsed < 1 {
				parsed = 1
			}
		case "text_chars":
			maximum = 4000
			if parsed < 200 {
				parsed = 200
			}
		case "rank_pool":
			maximum = 500
		case "max_text_chars":
			maximum = 4000
		case "max_chars":
			return maxToolTextRunes, nil
		}
		if parsed > maximum {
			parsed = maximum
		}
		return parsed, nil
	case "bool":
		boolean, ok := value.(bool)
		if !ok {
			return nil, ErrInvalidRequest
		}
		return boolean, nil
	case "roles":
		values, ok := value.([]any)
		if !ok || len(values) == 0 || len(values) > 8 {
			return nil, ErrInvalidRequest
		}
		roles := make([]string, 0, len(values))
		allowed := []string{"user", "assistant", "reasoning", "tool_call", "tool_output", "developer", "system"}
		for _, rawRole := range values {
			role, ok := rawRole.(string)
			if !ok || !containsString(allowed, role) {
				return nil, ErrInvalidRequest
			}
			roles = append(roles, role)
		}
		return roles, nil
	default:
		return nil, ErrInvalidRequest
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

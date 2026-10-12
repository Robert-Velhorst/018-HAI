package mcppreflight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPreflightUsesHandshakeAndNeverCallsTool(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "session-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
		case "notifications/initialized":
			if r.Header.Get("MCP-Session-Id") != "session-1" {
				t.Fatalf("initialized notification did not use session id")
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if r.Header.Get("MCP-Session-Id") != "session-1" {
				t.Fatalf("tools/list did not use session id")
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"read_case","title":"Read case","inputSchema":{"type":"object"}},{"name":"token_dump","title":"Secret","inputSchema":{}}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", request.Method)
		}
	}))
	defer server.Close()

	svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "test", CatalogID: "mcp-inspector", URL: server.URL}}})
	result, found := svc.Preflight(context.Background(), "test")
	if !found || result.Status != "ready" || result.ToolCount != 2 || result.ProtocolVersion != protocolVersion {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.CatalogID != "mcp-inspector" || result.CatalogName != "MCP Inspector" {
		t.Fatalf("preflight result must preserve reviewed catalog provenance: %#v", result)
	}
	if strings.Join(methods, ",") != "initialize,notifications/initialized,tools/list" {
		t.Fatalf("unexpected protocol methods: %v", methods)
	}
	if len(result.Tools) != 2 || result.Tools[0].Name != "[redacted]" || result.Tools[0].Title != "[redacted]" {
		t.Fatalf("tool output was not bounded/redacted: %#v", result.Tools)
	}
	if result.Tools[1].Name != "read_case" || !result.Tools[1].HasInputSchema {
		t.Fatalf("safe tool summary missing: %#v", result.Tools)
	}
	if strings.Contains(strings.ToLower(result.Detail), "token") {
		t.Fatalf("result detail must not echo server payload: %q", result.Detail)
	}
}

func TestPreflightAcceptsStreamableHTTPSSEForChatGPTLogs(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "text/event-stream")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "history-session")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2025-06-18\"}}\n\n"))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"search\",\"inputSchema\":{\"type\":\"object\"}},{\"name\":\"list_sources\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n"))
		}
	}))
	defer server.Close()

	svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "history", CatalogID: "chatgpt-codex-mcp-daemon", URL: server.URL}}})
	result, found := svc.Preflight(context.Background(), "history")
	if !found || result.Status != "ready" || !result.ReadOnlyVerified || result.ToolCount != 2 || strings.Join(methods, ",") != "initialize,notifications/initialized,tools/list" {
		t.Fatalf("unexpected SSE preflight: result=%#v methods=%v", result, methods)
	}
}

func TestPreflightIsFailClosedForDisabledOrUnsafeConfig(t *testing.T) {
	disabled := NewService(Config{Enabled: false, Servers: []Server{{ID: "local", CatalogID: "mcp-inspector", URL: "http://127.0.0.1:3000/mcp"}}})
	result, found := disabled.Preflight(context.Background(), "local")
	if !found || result.Status != "disabled" {
		t.Fatalf("disabled service must not run, got %#v", result)
	}

	unsafe := NewService(Config{Enabled: true, Servers: []Server{{ID: "remote", CatalogID: "mcp-inspector", URL: "https://example.com/mcp"}}})
	if unsafe.Overview().ConfigError == "" {
		t.Fatalf("non-local endpoint must be rejected")
	}
	result, found = unsafe.Preflight(context.Background(), "remote")
	if !found || result.Status != "blocked" {
		t.Fatalf("invalid config must be blocked, got %#v", result)
	}
}

func TestPreflightRequiresAnEligibleMCPCatalogProfile(t *testing.T) {
	for _, server := range []Server{
		{ID: "missing-profile", URL: "http://127.0.0.1:3000/mcp"},
		{ID: "unknown-profile", CatalogID: "not-a-profile", URL: "http://127.0.0.1:3000/mcp"},
		{ID: "non-mcp-profile", CatalogID: "cloudquery", URL: "http://127.0.0.1:3000/mcp"},
	} {
		svc := NewService(Config{Enabled: true, Servers: []Server{server}})
		if svc.Overview().ConfigError == "" {
			t.Fatalf("server %#v must be rejected without a reviewed MCP profile", server)
		}
		result, found := svc.Preflight(context.Background(), server.ID)
		if !found || result.Status != "blocked" {
			t.Fatalf("invalid server must fail closed: %#v", result)
		}
	}

	servers := parseServers("github@github-mcp-server=http://127.0.0.1:3000/mcp")
	if len(servers) != 1 || servers[0].ID != "github" || servers[0].CatalogID != "github-mcp-server" {
		t.Fatalf("profile-aware server parsing failed: %#v", servers)
	}
	if svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "serena", CatalogID: "serena", URL: "http://127.0.0.1:3000/mcp"}}}); svc.Overview().ConfigError != "" {
		t.Fatalf("reviewed read-only Serena MCP profile must be preflight eligible: %q", svc.Overview().ConfigError)
	}
}

func TestValidateLocalURLRejectsCredentialsAndNonLocalHosts(t *testing.T) {
	for _, raw := range []string{
		"http://user:password@127.0.0.1:8080/mcp",
		"http://169.254.169.254/mcp",
		"http://0.0.0.0/mcp",
		"http://example.com/mcp",
		"http://localhost/mcp?access_token=secret",
	} {
		if err := validateEndpointURL(Server{URL: raw}, false); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
	for _, raw := range []string{"http://localhost:8080/mcp", "http://127.0.0.1:8080/mcp", "http://host.docker.internal:8080/mcp"} {
		if err := validateEndpointURL(Server{URL: raw}, false); err != nil {
			t.Fatalf("%q should be allowed: %v", raw, err)
		}
	}
}

func TestPreflightFailsClosedForProtocolDowngradeOrMismatchedResponseID(t *testing.T) {
	for _, reply := range []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":99,"result":{"protocolVersion":"2025-06-18"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(reply))
		}))
		svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "test", CatalogID: "mcp-inspector", URL: server.URL}}})
		result, found := svc.Preflight(context.Background(), "test")
		server.Close()
		if !found || result.Status != "failed" || !strings.Contains(result.Detail, "initialize") {
			t.Fatalf("preflight must reject reply %s, got %#v", reply, result)
		}
	}
}

func TestPreflightBlocksGitHubWriteInventory(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "github-session")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_repository"},{"name":"create_issue"}]}}`))
		default:
			t.Fatalf("preflight must not call %q", request.Method)
		}
	}))
	defer server.Close()

	svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "github", CatalogID: "github-mcp-server", URL: server.URL}}})
	result, found := svc.Preflight(context.Background(), "github")
	if !found || result.Status != "blocked" || result.ReadOnlyVerified {
		t.Fatalf("write-capable GitHub inventory must be blocked: %#v", result)
	}
	if got := strings.Join(methods, ","); got != "initialize,notifications/initialized,tools/list" {
		t.Fatalf("methods = %q", got)
	}
}

func TestPreflightVerifiesReviewedReadOnlyInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "github-session")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_repository"},{"name":"list_issues"}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", request.Method)
		}
	}))
	defer server.Close()

	svc := NewService(Config{Enabled: true, Servers: []Server{{ID: "github", CatalogID: "github-mcp-server", URL: server.URL}}})
	result, found := svc.Preflight(context.Background(), "github")
	if !found || result.Status != "ready" || !result.ReadOnlyVerified {
		t.Fatalf("reviewed GitHub inventory must be ready: %#v", result)
	}
}

func TestToolAllowlistChecksNamesBeyondDisplayLimit(t *testing.T) {
	declared := make([]map[string]string, 0, maxTools+1)
	for index := 0; index < maxTools; index++ {
		declared = append(declared, map[string]string{"name": "get_repository"})
	}
	declared = append(declared, map[string]string{"name": "create_issue"})
	raw, err := json.Marshal(map[string]any{"tools": declared})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tools, names, count, truncated, err := boundedTools(raw)
	if err != nil || len(tools) != maxTools || len(names) != maxTools+1 || count != maxTools+1 || !truncated {
		t.Fatalf("bounded tools = tools:%d names:%d count:%d truncated:%t err:%v", len(tools), len(names), count, truncated, err)
	}
	violations := readOnlyToolNameViolations(names, githubReadOnlyContextTools)
	if len(violations) != 1 || violations[0] != "create_issue" {
		t.Fatalf("tail violation = %#v", violations)
	}
}

func TestChatGPTLogsAllowlistCoversRecallToolsAndStillBlocksUnreviewedNames(t *testing.T) {
	declared := []string{
		"get_context", "get_conversation", "get_message", "get_raw", "list_conversations",
		"list_sources", "search", "search_insights", "search_passages", "stats", "sync_status",
	}
	if violations := readOnlyToolNameViolations(declared, chatgptLogsReadOnlyContextTools); len(violations) != 0 {
		t.Fatalf("current MemoryLayerMCP inventory must pass the reviewed allowlist: %#v", violations)
	}
	for _, unreviewed := range []string{"delete_conversation", "write_note", "run_command", "search_insight"} {
		if violations := readOnlyToolNameViolations([]string{unreviewed}, chatgptLogsReadOnlyContextTools); len(violations) != 1 {
			t.Fatalf("%q must stay blocked, violations = %#v", unreviewed, violations)
		}
	}
}

func TestPreflightPresentsTheTokenItsListenerAsksFor(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Mcp-Session-Id", "s-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","tools":[{"name":"stats"}]}}`))
	}))
	defer server.Close()

	servers := withBearerTokens(
		[]Server{{ID: "chatgpt-logs", CatalogID: "chatgpt-codex-mcp-daemon", URL: server.URL}},
		"chatgpt-logs=t0ken-abc",
	)
	service := NewService(Config{Enabled: true, Servers: servers, Timeout: 5 * time.Second})
	if _, ok := service.Preflight(context.Background(), "chatgpt-logs"); !ok {
		t.Fatal("preflight did not run")
	}

	if len(seen) == 0 {
		t.Fatal("the listener was never reached")
	}
	for index, header := range seen {
		if header != "Bearer t0ken-abc" {
			t.Fatalf("request %d presented %q", index, header)
		}
	}
}

func TestPreflightNeverPutsATokenInWhatItReports(t *testing.T) {
	servers := withBearerTokens(
		[]Server{{ID: "chatgpt-logs", URL: "http://127.0.0.1:8101/mcp"}},
		"chatgpt-logs=t0ken-abc",
	)
	service := NewService(Config{Enabled: true, Servers: servers, Timeout: time.Second})

	encoded, err := json.Marshal(service.Overview())
	if err != nil {
		t.Fatalf("encode overview: %v", err)
	}
	if strings.Contains(string(encoded), "t0ken-abc") {
		t.Fatalf("the overview leaked the bearer token: %s", encoded)
	}
}

func TestAnUnusableTokenIsNotSentAsAHeader(t *testing.T) {
	for _, token := range []string{"", "has space", "line\r\nX-Injected: 1"} {
		if safeBearerToken(token) {
			t.Fatalf("token %q would have been sent", token)
		}
	}
	if !safeBearerToken("t0ken-abc") {
		t.Fatal("a plain token was refused")
	}
}

func TestPreflightRefusesARemoteServerUntilItIsExplicitlyAllowed(t *testing.T) {
	servers := withBearerTokens(
		[]Server{{ID: "remote-logs", CatalogID: "chatgpt-codex-mcp-daemon", URL: "https://logs.example.com/mcp"}},
		"remote-logs=t0ken-abc",
	)

	blocked := NewService(Config{Enabled: true, Servers: servers, Timeout: time.Second})
	if blocked.Overview().ConfigError == "" {
		t.Fatal("a remote server was accepted by default")
	}

	allowed := NewService(Config{Enabled: true, Servers: servers, Timeout: time.Second, AllowRemoteEndpoints: true})
	if allowed.Overview().ConfigError != "" {
		t.Fatalf("an explicitly allowed remote server was still refused: %q", allowed.Overview().ConfigError)
	}
}

func TestAnAllowedRemoteServerStillNeedsTLSAndAToken(t *testing.T) {
	plaintext := NewService(Config{
		Enabled:              true,
		Timeout:              time.Second,
		AllowRemoteEndpoints: true,
		Servers: withBearerTokens(
			[]Server{{ID: "remote-logs", CatalogID: "chatgpt-codex-mcp-daemon", URL: "http://logs.example.com/mcp"}},
			"remote-logs=t0ken-abc",
		),
	})
	if plaintext.Overview().ConfigError == "" {
		t.Fatal("a remote server was accepted over plaintext")
	}

	anonymous := NewService(Config{
		Enabled:              true,
		Timeout:              time.Second,
		AllowRemoteEndpoints: true,
		Servers:              []Server{{ID: "remote-logs", CatalogID: "chatgpt-codex-mcp-daemon", URL: "https://logs.example.com/mcp"}},
	})
	if anonymous.Overview().ConfigError == "" {
		t.Fatal("a remote server was accepted without a token")
	}
}

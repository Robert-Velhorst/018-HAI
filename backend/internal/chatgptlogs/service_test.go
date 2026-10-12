package chatgptlogs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchUsesOnlyBoundedReadOnlySearchAndAcceptsSSE(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json, text/event-stream" || r.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe headers: accept=%q authorization=%q", r.Header.Get("Accept"), r.Header.Get("Authorization"))
		}
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "text/event-stream")
		switch request.Method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", "history-session")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2025-06-18\"}}\n\n"))
		case "notifications/initialized":
			if r.Header.Get("MCP-Session-Id") != "history-session" {
				t.Fatalf("notification session = %q", r.Header.Get("MCP-Session-Id"))
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"search\"},{\"name\":\"get_raw\"}]}}\n\n"))
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.Name != "search" || params.Arguments["query"] != "why did the build fail" || params.Arguments["project"] != "018-HAI" || params.Arguments["limit"] != float64(5) || params.Arguments["offset"] != float64(0) || params.Arguments["rank_pool"] != float64(200) || params.Arguments["max_chars"] != float64(maxToolTextRunes) {
				t.Fatalf("unexpected tool call: %#v", params)
			}
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"bounded historical context\"}]}}\n\n"))
		default:
			t.Fatalf("unexpected MCP method %q", request.Method)
		}
	}))
	defer server.Close()

	service := NewService(true, server.URL+"/mcp", server.Client())
	items, err := service.Search(context.Background(), SearchRequest{Query: "why did the build fail", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if strings.Join(methods, ",") != "initialize,notifications/initialized,tools/list,tools/call" {
		t.Fatalf("methods = %v", methods)
	}
	if len(items) != 1 || items[0].Content != "bounded historical context" || !items[0].Untrusted || items[0].Tool != "search" {
		t.Fatalf("unexpected context: %#v", items)
	}
}

func TestSearchFailsClosedForUnsafeConfigurationAndMissingSearch(t *testing.T) {
	unsafe := NewService(true, "https://example.com/mcp", nil)
	if unsafe.Status().Configured {
		t.Fatalf("remote endpoint must be rejected: %#v", unsafe.Status())
	}
	if _, err := unsafe.Search(context.Background(), SearchRequest{Query: "test"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unsafe Search() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_raw"}]}}`))
		}
	}))
	defer server.Close()
	configured := NewService(true, server.URL+"/mcp", server.Client())
	if _, err := configured.Search(context.Background(), SearchRequest{Query: "test"}); err == nil || !strings.Contains(err.Error(), "requested reviewed read-only tool") {
		t.Fatalf("missing search error = %v", err)
	}
}

func TestSearchRejectsInvalidInputBeforeNetwork(t *testing.T) {
	service := NewService(true, "http://127.0.0.1:8099/mcp", &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached network")
		return nil, nil
	})})
	for _, request := range []SearchRequest{{}, {Query: "nul\x00query"}, {Query: strings.Repeat("x", maxQueryRunes+1)}, {Query: "ok", ProjectKey: strings.Repeat("p", maxProjectRunes+1)}} {
		if _, err := service.Search(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Search(%#v) error = %v", request, err)
		}
	}
}

func TestNormalizeArgumentsAllowsReviewedToolsAndClampsBudgets(t *testing.T) {
	tests := []struct {
		tool string
		raw  string
	}{
		{"list_sources", `{}`},
		{"list_conversations", `{"platform":"codex","project":"018-HAI","limit":200}`},
		{"search", `{"query":"unfinished commitment","roles":["user","assistant"],"order":"recent"}`},
		{"search_insights", `{"query":"where did we leave off","platform":"codex","freshness":"stale","mode":"semantic","min_similarity":0.4,"limit":200,"max_chars":900000}`},
		{"search_passages", `{"query":"what did we say about the tunnel","mode":"keyword","min_similarity":0,"per_conversation":200,"text_chars":100000,"limit":200,"offset":5}`},
		{"get_conversation", `{"conversation_id":42,"limit":100}`},
		{"get_context", `{"message_id":"message-1","before":50,"after":50}`},
		{"get_message", `{"message_id":99}`},
		{"get_raw", `{"conversation_id":"conversation-1","artifacts":50}`},
		{"sync_status", `{"runs":50,"pending":50,"failures":50}`},
		{"stats", `{"platform":"chatgpt_work","top_projects":50}`},
	}
	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			arguments, err := normalizeArguments(test.tool, json.RawMessage(test.raw))
			if err != nil {
				t.Fatalf("normalizeArguments() error = %v", err)
			}
			if arguments["max_chars"] != maxToolTextRunes {
				t.Fatalf("max_chars = %#v", arguments["max_chars"])
			}
			for _, key := range []string{"limit", "before", "after", "artifacts", "runs", "pending", "failures", "top_projects", "per_conversation"} {
				if value, exists := arguments[key]; exists && value.(int64) > 20 {
					t.Fatalf("%s escaped clamp: %#v", key, value)
				}
			}
		})
	}
}

func TestNormalizeArgumentsRejectsUnknownToolsFieldsAndMissingIdentifiers(t *testing.T) {
	for _, test := range []CallRequest{
		{Tool: "delete_conversation", Arguments: json.RawMessage(`{}`)},
		{Tool: "search", Arguments: json.RawMessage(`{"query":"ok","command":"rm"}`)},
		{Tool: "search", Arguments: json.RawMessage(`{"query":"ok","platform":"unknown"}`)},
		{Tool: "get_message", Arguments: json.RawMessage(`{}`)},
		{Tool: "get_raw", Arguments: json.RawMessage(`{}`)},
	} {
		if _, err := normalizeArguments(test.Tool, test.Arguments); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("normalizeArguments(%#v) error = %v", test, err)
		}
	}
}

func TestRecallToolsStayBoundedAndReadOnly(t *testing.T) {
	insights, err := normalizeArguments("search_insights", json.RawMessage(`{"query":"018-HAI","min_similarity":0.4,"limit":200}`))
	if err != nil {
		t.Fatalf("search_insights error = %v", err)
	}
	if insights["min_similarity"] != 0.4 || insights["limit"].(int64) != 20 || insights["max_chars"] != maxToolTextRunes {
		t.Fatalf("search_insights arguments = %#v", insights)
	}

	passages, err := normalizeArguments("search_passages", json.RawMessage(`{"query":"018-HAI","per_conversation":0,"text_chars":10,"offset":3}`))
	if err != nil {
		t.Fatalf("search_passages error = %v", err)
	}
	if passages["per_conversation"].(int64) != 1 || passages["text_chars"].(int64) != 200 || passages["offset"].(int64) != 3 || passages["max_chars"] != maxToolTextRunes {
		t.Fatalf("search_passages arguments = %#v", passages)
	}

	for _, test := range []CallRequest{
		{Tool: "search_insights", Arguments: json.RawMessage(`{}`)},
		{Tool: "search_passages", Arguments: json.RawMessage(`{}`)},
		{Tool: "search_insights", Arguments: json.RawMessage(`{"query":"ok","command":"rm -rf /"}`)},
		{Tool: "search_passages", Arguments: json.RawMessage(`{"query":"ok","path":"/etc/passwd"}`)},
		{Tool: "search_insights", Arguments: json.RawMessage(`{"query":"ok","freshness":"whatever"}`)},
		{Tool: "search_insights", Arguments: json.RawMessage(`{"query":"ok","mode":"sql"}`)},
		{Tool: "search_passages", Arguments: json.RawMessage(`{"query":"ok","min_similarity":1.5}`)},
		{Tool: "search_passages", Arguments: json.RawMessage(`{"query":"ok","min_similarity":"0.5"}`)},
		{Tool: "search_insights", Arguments: json.RawMessage(`{"query":""}`)},
	} {
		if _, err := normalizeArguments(test.Tool, test.Arguments); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("normalizeArguments(%s, %s) error = %v", test.Tool, test.Arguments, err)
		}
	}

	names := map[string]bool{}
	for _, descriptor := range (&service{}).Tools() {
		names[descriptor.Name] = true
		if _, reviewed := reviewedToolRules[descriptor.Name]; !reviewed {
			t.Fatalf("descriptor %q has no argument rule", descriptor.Name)
		}
	}
	if !names["search_insights"] || !names["search_passages"] || len(names) != len(reviewedToolRules) {
		t.Fatalf("reviewed descriptors and rules are out of sync: %#v", names)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestAListenerThatAsksForATokenGetsOne(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen == "" {
			seen = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{}"}]}}`))
	}))
	defer server.Close()

	service := NewServiceWithOptions(Options{Enabled: true, BaseURL: server.URL + "/mcp", BearerToken: "t0ken-abc", Client: server.Client()})
	if !service.Status().Configured {
		t.Fatalf("a tokened service was not configured: %#v", service.Status())
	}
	_, _ = service.Call(context.Background(), CallRequest{Tool: "stats", Arguments: []byte(`{}`)})

	if seen != "Bearer t0ken-abc" {
		t.Fatalf("bearer token was not presented: %q", seen)
	}
}

func TestNoTokenMeansNoAuthorizationHeaderAtAll(t *testing.T) {
	requests := 0
	present := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if _, ok := r.Header["Authorization"]; ok {
			present = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{}"}]}}`))
	}))
	defer server.Close()

	service := NewService(true, server.URL+"/mcp", server.Client())
	_, _ = service.Call(context.Background(), CallRequest{Tool: "stats", Arguments: []byte(`{}`)})

	if requests == 0 {
		t.Fatal("the adapter never reached the listener")
	}
	if present {
		t.Fatal("an empty token still sent an Authorization header")
	}
}

func TestATokenThatCouldForgeHeadersIsRefusedBeforeAnyCall(t *testing.T) {
	for _, token := range []string{"good\r\nX-Injected: 1", "has space", "wide token"} {
		service := NewServiceWithOptions(Options{Enabled: true, BaseURL: "http://127.0.0.1:8099/mcp", BearerToken: token})
		status := service.Status()
		if status.Configured || status.ConfigError == "" {
			t.Fatalf("token %q was accepted: %#v", token, status)
		}
	}
}

func TestAPublicEndpointIsRefusedUntilItIsExplicitlyAllowed(t *testing.T) {
	blocked := NewService(true, "https://logs.example.com/mcp", nil)
	if blocked.Status().Configured || !strings.Contains(blocked.Status().ConfigError, allowRemoteEnv) {
		t.Fatalf("a public endpoint was accepted by default: %#v", blocked.Status())
	}

	allowed := NewServiceWithOptions(Options{
		Enabled:             true,
		BaseURL:             "https://logs.example.com/mcp",
		BearerToken:         "t0ken-abc",
		AllowRemoteEndpoint: true,
	})
	if !allowed.Status().Configured {
		t.Fatalf("an explicitly allowed endpoint was still refused: %#v", allowed.Status())
	}
}

func TestAllowingARemoteEndpointStillRequiresTLSAndAToken(t *testing.T) {
	plaintext := NewServiceWithOptions(Options{
		Enabled:             true,
		BaseURL:             "http://logs.example.com/mcp",
		BearerToken:         "t0ken-abc",
		AllowRemoteEndpoint: true,
	})
	if plaintext.Status().Configured {
		t.Fatalf("a remote endpoint was accepted over plaintext: %#v", plaintext.Status())
	}

	anonymous := NewServiceWithOptions(Options{
		Enabled:             true,
		BaseURL:             "https://logs.example.com/mcp",
		AllowRemoteEndpoint: true,
	})
	if anonymous.Status().Configured {
		t.Fatalf("a remote endpoint was accepted without a token: %#v", anonymous.Status())
	}
}

func TestAllowingRemoteDoesNotBurdenALocalListener(t *testing.T) {
	local := NewServiceWithOptions(Options{
		Enabled:             true,
		BaseURL:             "http://host.docker.internal:8101/mcp",
		AllowRemoteEndpoint: true,
	})
	if !local.Status().Configured {
		t.Fatalf("a local listener was refused for lack of TLS or a token: %#v", local.Status())
	}
}

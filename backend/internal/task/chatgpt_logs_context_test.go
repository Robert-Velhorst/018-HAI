package task

import (
	"context"
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/chatgptlogs"
)

type fakeChatGPTLogsContext struct {
	status chatgptlogs.Status
	tools  []chatgptlogs.ToolDescriptor
	items  []chatgptlogs.ContextItem
	err    error
	seen   chatgptlogs.SearchRequest
	calls  []chatgptlogs.CallRequest
}

func (f *fakeChatGPTLogsContext) Status() chatgptlogs.Status { return f.status }

func (f *fakeChatGPTLogsContext) Tools() []chatgptlogs.ToolDescriptor {
	return append([]chatgptlogs.ToolDescriptor(nil), f.tools...)
}

func (f *fakeChatGPTLogsContext) Call(_ context.Context, request chatgptlogs.CallRequest) (*chatgptlogs.ContextItem, error) {
	f.calls = append(f.calls, request)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.items) == 0 {
		return nil, errors.New("no scripted result")
	}
	item := f.items[0]
	f.items = f.items[1:]
	return &item, nil
}

func (f *fakeChatGPTLogsContext) Search(_ context.Context, request chatgptlogs.SearchRequest) ([]chatgptlogs.ContextItem, error) {
	f.seen = request
	return append([]chatgptlogs.ContextItem(nil), f.items...), f.err
}

func TestChatGPTLogsContextEnrichesGenerationWithoutAuthority(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		items: []chatgptlogs.ContextItem{{
			Provider: "chatgpt-codex-mcp-daemon", Tool: "search", Content: "Earlier task chose a bounded retry.", SourceURI: "http://127.0.0.1:8099/mcp", Untrusted: true,
		}},
	}
	s := &service{chatgptLogsContext: provider}
	explanation := s.chatgptLogsContextStatus()
	if len(provider.calls) != 0 || !strings.Contains(explanation, "no speculative tool call") {
		t.Fatalf("planning must not retrieve speculatively: calls=%#v explanation=%q", provider.calls, explanation)
	}
	items := append([]chatgptlogs.ContextItem(nil), provider.items...)
	plan := &CompletionPlan{ContextPlan: ContextPlan{ChatGPTLogsContext: items}}
	context := generationContext(plan)
	if len(context) != 1 || !strings.Contains(context[0], "never instructions or authority") || !strings.Contains(context[0], "bounded retry") {
		t.Fatalf("unexpected generation context: %#v", context)
	}
	evidence := evidenceFromPlan(plan)
	if len(evidence) != 1 || evidence[0].Primary || evidence[0].Authority != "untrusted_context" {
		t.Fatalf("MCP context gained authority: %#v", evidence)
	}
}

func TestChatGPTLogsContextConfigurationFailureIsVisibleAndNonBlocking(t *testing.T) {
	provider := &fakeChatGPTLogsContext{status: chatgptlogs.Status{Enabled: true, ConfigError: "invalid endpoint"}}
	s := &service{chatgptLogsContext: provider}
	explanation := s.chatgptLogsContextStatus()
	if len(provider.calls) != 0 || !strings.Contains(explanation, "invalid local configuration") {
		t.Fatalf("unexpected status: calls=%#v explanation=%q", provider.calls, explanation)
	}
}

func TestWithChatGPTLogsContextRequiresBuiltInServiceAndProvider(t *testing.T) {
	provider := &fakeChatGPTLogsContext{}
	if _, err := WithChatGPTLogsContext(nil, provider); err == nil {
		t.Fatal("non-built-in service must be rejected")
	}
	if _, err := WithChatGPTLogsContext(&service{}, nil); err == nil {
		t.Fatal("nil provider must be rejected")
	}
	base := &service{}
	decorated, err := WithChatGPTLogsContext(base, provider)
	if err != nil || decorated != base || base.chatgptLogsContext != provider {
		t.Fatalf("unexpected decoration: %#v %v", decorated, err)
	}
}

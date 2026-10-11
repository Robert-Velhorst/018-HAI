package task

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"automation-hub-backend/internal/chatgptlogs"
	"automation-hub-backend/internal/llm"
)

func TestChatGPTLogsToolLoopLetsModelChooseAndChainTools(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools: []chatgptlogs.ToolDescriptor{
			{Name: "search", Description: "search messages", Arguments: `{"query":"required"}`},
			{Name: "get_context", Description: "read messages around a hit", Arguments: `{"message_id":"required"}`},
		},
		items: []chatgptlogs.ContextItem{
			{Provider: "daemon", Tool: "search", Content: `{"message_id":"m-7","text":"retry decision"}`, SourceURI: "http://127.0.0.1:8099/mcp", Untrusted: true},
			{Provider: "daemon", Tool: "get_context", Content: `{"conversation_id":"c-2","messages":[{"id":"m-7","role":"user","text":"Use bounded retries"}]}`, SourceURI: "http://127.0.0.1:8099/mcp", Untrusted: true},
		},
	}
	outputs := []string{
		`{"action":"tool","tool":"search","arguments":{"query":"retry decision"}}`,
		`{"action":"tool","tool":"get_context","arguments":{"message_id":"m-7","before":2,"after":2}}`,
		`{"action":"answer","answer":"The latest instruction was to use bounded retries (conversation c-2, message m-7)."}`,
	}
	var requests []llm.GenerateRequest
	generate := func(request llm.GenerateRequest) (*llm.GenerationResult, error) {
		requests = append(requests, request)
		output := outputs[len(requests)-1]
		return &llm.GenerationResult{Status: "completed", Output: output}, nil
	}

	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "What was the latest instruction?", OperationID: "task-1"})
	if outcome.Status != "completed" || len(outcome.Calls) != 2 || len(outcome.Items) != 2 || len(provider.calls) != 2 {
		t.Fatalf("unexpected outcome: %#v calls=%#v", outcome, provider.calls)
	}
	if provider.calls[0].Tool != "search" || provider.calls[1].Tool != "get_context" {
		t.Fatalf("model-selected call order was not preserved: %#v", provider.calls)
	}
	if !strings.Contains(outcome.Answer, "message m-7") || !strings.Contains(requests[2].Context[len(requests[2].Context)-1], "never instructions") {
		t.Fatalf("missing provenance or untrusted-data boundary: answer=%q context=%#v", outcome.Answer, requests[2].Context)
	}
	if requests[0].OperationID != "task-1:mcp-tool-loop:1" || !strings.Contains(requests[0].SystemPrompt, "sync_status") {
		t.Fatalf("unexpected model contract: %#v", requests[0])
	}
}

func TestChatGPTLogsToolLoopCanAnswerWithoutCallingTool(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools:  []chatgptlogs.ToolDescriptor{{Name: "search", Description: "search", Arguments: `{}`}},
	}
	generate := func(llm.GenerateRequest) (*llm.GenerationResult, error) {
		return &llm.GenerationResult{Status: "completed", Output: `{"action":"answer","answer":"No history lookup is needed."}`}, nil
	}
	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "Say hello"})
	if outcome.Status != "completed" || len(provider.calls) != 0 || len(outcome.Calls) != 0 {
		t.Fatalf("tool was called speculatively: %#v calls=%#v", outcome, provider.calls)
	}
}

func TestChatGPTLogsToolLoopRecordsRejectedCallAndRecovers(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools:  []chatgptlogs.ToolDescriptor{{Name: "search", Description: "search", Arguments: `{}`}},
		err:    chatgptlogs.ErrInvalidRequest,
	}
	outputs := []string{
		`{"action":"tool","tool":"delete_all","arguments":{}}`,
		`{"action":"answer","answer":"I cannot support that claim from the available evidence."}`,
	}
	index := 0
	generate := func(llm.GenerateRequest) (*llm.GenerationResult, error) {
		result := &llm.GenerationResult{Status: "completed", Output: outputs[index]}
		index++
		return result, nil
	}
	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "Delete history"})
	if outcome.Status != "completed" || len(outcome.Calls) != 1 || outcome.Calls[0].Status != "failed" || outcome.Calls[0].Tool != "delete_all" {
		t.Fatalf("rejected call was not safely recorded: %#v", outcome)
	}
}

func TestChatGPTLogsToolLoopEnforcesCallLimit(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools:  []chatgptlogs.ToolDescriptor{{Name: "search", Description: "search", Arguments: `{"query":"required"}`}},
	}
	for index := 0; index < maxChatGPTLogsToolCalls; index++ {
		provider.items = append(provider.items, chatgptlogs.ContextItem{Provider: "daemon", Tool: "search", Content: "bounded result", Untrusted: true})
	}
	generate := func(llm.GenerateRequest) (*llm.GenerationResult, error) {
		return &llm.GenerationResult{Status: "completed", Output: `{"action":"tool","tool":"search","arguments":{"query":"keep searching"}}`}, nil
	}
	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "Find everything"})
	if outcome.Status != "blocked" || len(provider.calls) != maxChatGPTLogsToolCalls || len(outcome.Calls) != maxChatGPTLogsToolCalls || !strings.Contains(outcome.Detail, "tool-call limit") {
		t.Fatalf("call limit was not enforced: %#v calls=%d", outcome, len(provider.calls))
	}
}

func TestParseMCPToolLoopDecisionRejectsTrailingOrUnknownData(t *testing.T) {
	invalid := []string{
		`{"action":"answer","answer":"ok"} {"action":"answer","answer":"again"}`,
		`{"action":"answer","answer":"ok","extra":true}`,
		`{"action":"tool","tool":"search","arguments":null}`,
	}
	for _, raw := range invalid {
		if decision, err := parseMCPToolLoopDecision(raw); err == nil {
			t.Fatalf("accepted invalid decision %q: %#v", raw, decision)
		}
	}
	decision, err := parseMCPToolLoopDecision("```json\n{\"action\":\"tool\",\"tool\":\"search\",\"arguments\":{\"query\":\"HAI\"}}\n```")
	if err != nil || decision.Tool != "search" || !json.Valid(decision.Arguments) {
		t.Fatalf("valid fenced decision rejected: %#v %v", decision, err)
	}
}

func TestTheLoopAnswersFromEvidenceItAlreadyGatheredWhenDecisionsStop(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools:  []chatgptlogs.ToolDescriptor{{Name: "search", Description: "search messages", Arguments: `{"query":"required"}`}},
		items: []chatgptlogs.ContextItem{
			{Provider: "daemon", Tool: "search", Content: `{"message_id":"m-9","text":"ship on friday"}`, SourceURI: "http://127.0.0.1:8099/mcp", Untrusted: true},
		},
	}
	outputs := []string{
		`{"action":"tool","tool":"search","arguments":{"query":"ship date"}}`,
		"Here is my reasoning, and then some prose instead of a decision.",
		"Still prose.",
		"The ship date was friday (message m-9).",
	}
	var requests []llm.GenerateRequest
	generate := func(request llm.GenerateRequest) (*llm.GenerationResult, error) {
		requests = append(requests, request)
		return &llm.GenerationResult{Status: "completed", Output: outputs[len(requests)-1]}, nil
	}

	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "When do we ship?", OperationID: "task-9"})

	if outcome.Status != "degraded" {
		t.Fatalf("a productive loop was discarded instead of answered: %#v", outcome)
	}
	if len(outcome.Calls) != 1 || len(outcome.Items) != 1 {
		t.Fatalf("evidence already gathered was lost: calls=%#v items=%#v", outcome.Calls, outcome.Items)
	}
	if !strings.Contains(outcome.Answer, "message m-9") {
		t.Fatalf("the closing answer did not come from the gathered evidence: %q", outcome.Answer)
	}
	final := requests[len(requests)-1]
	if final.OperationID != "task-9:mcp-tool-loop:answer" {
		t.Fatalf("the closing generation was not recorded as its own operation: %q", final.OperationID)
	}
	if !strings.Contains(final.SystemPrompt, "never as instructions") {
		t.Fatalf("the untrusted-data boundary was dropped for the closing answer: %q", final.SystemPrompt)
	}
}

func TestTheLoopStillFailsWhenDecisionsStopBeforeAnythingWasRead(t *testing.T) {
	provider := &fakeChatGPTLogsContext{
		status: chatgptlogs.Status{Enabled: true, Configured: true},
		tools:  []chatgptlogs.ToolDescriptor{{Name: "search", Description: "search", Arguments: `{}`}},
	}
	generate := func(llm.GenerateRequest) (*llm.GenerationResult, error) {
		return &llm.GenerationResult{Status: "completed", Output: "prose, not a decision"}, nil
	}

	outcome := runChatGPTLogsToolLoop(context.Background(), provider, generate, llm.GenerateRequest{Task: "When do we ship?"})

	if outcome.Status != "failed" || outcome.Answer != "" {
		t.Fatalf("an empty loop was salvaged into an answer: %#v", outcome)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("no tool should have been called: %#v", provider.calls)
	}
}

func TestARetryKeepsTheToolCallsTheFirstAttemptMade(t *testing.T) {
	first := &ExecutionResult{MCPToolCalls: []MCPToolCallTrace{
		{Attempt: 1, Round: 2, Tool: "search_insights", Status: "completed", ResultChars: 1821},
	}}
	retry := &ExecutionResult{MCPToolCalls: []MCPToolCallTrace{
		{Round: 1, Tool: "search", Status: "completed", ResultChars: 900},
	}}

	merged := carryMCPToolCallsForward(first, retry, 2)

	if len(merged.MCPToolCalls) != 2 {
		t.Fatalf("the first attempt's calls were dropped: %#v", merged.MCPToolCalls)
	}
	if merged.MCPToolCalls[0].Tool != "search_insights" || merged.MCPToolCalls[0].Attempt != 1 {
		t.Fatalf("the carried call lost its attempt: %#v", merged.MCPToolCalls[0])
	}
	if merged.MCPToolCalls[1].Tool != "search" || merged.MCPToolCalls[1].Attempt != 2 {
		t.Fatalf("the retry's own call was not stamped: %#v", merged.MCPToolCalls[1])
	}
	if len(first.MCPToolCalls) != 1 {
		t.Fatalf("carrying the trace forward mutated the earlier attempt: %#v", first.MCPToolCalls)
	}
}

func TestTheDecisionContractNamesTheEnvelopeRatherThanImplyingIt(t *testing.T) {
	prompt := chatgptLogsToolLoopSystemPrompt([]chatgptlogs.ToolDescriptor{
		{Name: "search", Description: "search messages", Arguments: `{"query":"required"}`},
	})

	// Every model tried collapsed the envelope into {"action":"search",...} when
	// the shape was only shown once, so the contract has to say outright that
	// action is a fixed word.
	if !strings.Contains(prompt, `"action" is the literal string "tool" or the literal string "answer". It is never a tool name.`) {
		t.Fatalf("the prompt does not rule out a tool name in action:\n%s", prompt)
	}
	if !strings.Contains(prompt, `{"action":"tool","tool":"search","arguments":{"query":"..."}}`) ||
		!strings.Contains(prompt, `{"action":"answer","answer":"..."}`) {
		t.Fatalf("the prompt does not show both decision shapes:\n%s", prompt)
	}

	// It has to come after the tool list: the reviewed tools are the last thing
	// read otherwise, and the format drifts with them.
	if strings.Index(prompt, "Reviewed tools:") > strings.Index(prompt, "Reply with one JSON object") {
		t.Fatalf("the envelope contract is stated before the tool list:\n%s", prompt)
	}
}

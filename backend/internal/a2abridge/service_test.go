package a2abridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"automation-hub-backend/internal/task"

	"github.com/gin-gonic/gin"
)

const testBridgeToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type previewStub struct {
	requests []task.IntakeRequest
}

func (s *previewStub) Preview(request task.IntakeRequest) (*task.CompletionPlan, error) {
	s.requests = append(s.requests, request)
	return &task.CompletionPlan{
		Intake:           task.IntakeAnalysis{TaskType: "research", SuccessCriteria: []string{"produce a source-backed draft"}},
		RiskAssessment:   task.RiskAssessment{Level: "high", ApprovalRequired: true},
		ValidationResult: task.ValidationResult{NextAction: "ask Robert to review the draft"},
		CompletionStatus: "review_required",
		Steps:            []task.TaskStep{{Name: "Gather evidence", Purpose: "find relevant records", RequiresApproval: false, Status: "ready"}},
	}, nil
}

func configuredService(planner task.PreviewService) *Service {
	return NewService(Config{Enabled: true, Token: testBridgeToken, OwnerID: "owner@example.test", URL: "http://127.0.0.1/api/v1/a2a"}, planner)
}

func TestDraftCreatesSanitizedPreviewWithoutExecution(t *testing.T) {
	planner := &previewStub{}
	service := configuredService(planner)
	proposal, err := service.Draft("Prepare a legal draft")
	if err != nil {
		t.Fatalf("Draft returned error: %v", err)
	}
	if len(planner.requests) != 1 || planner.requests[0].OwnerIdentity != "owner@example.test" || planner.requests[0].ExecuteAllowed || planner.requests[0].HumanApproved {
		t.Fatalf("preview request was not bounded: %#v", planner.requests)
	}
	if proposal.NeedsApproval != true || proposal.RiskLevel != "high" || len(proposal.Steps) != 1 {
		t.Fatalf("proposal = %#v", proposal)
	}
	if strings.Contains(strings.ToLower(proposal.Scope), "execute") == false || strings.Contains(strings.ToLower(proposal.Scope), "did not create") == false {
		t.Fatalf("proposal scope is not explicit: %q", proposal.Scope)
	}
}

func TestBoundedPreservesUnicodeAtTheLimit(t *testing.T) {
	value := strings.Repeat("e", 77) + "\u20acuro"
	result := bounded(value, 80)
	if !utf8.ValidString(result) {
		t.Fatalf("bounded returned invalid UTF-8: %q", result)
	}
	if utf8.RuneCountInString(result) != 80 || !strings.HasSuffix(result, "...") {
		t.Fatalf("bounded unicode result = %q (%d runes)", result, utf8.RuneCountInString(result))
	}
	if strings.ContainsRune(result, utf8.RuneError) {
		t.Fatalf("bounded replaced a split rune: %q", result)
	}
}

func TestStatusRejectsExternalEndpointAndWrongToken(t *testing.T) {
	service := NewService(Config{Enabled: true, Token: testBridgeToken, OwnerID: "owner", URL: "https://example.com/a2a"}, &previewStub{})
	if service.Status().Configured {
		t.Fatal("external endpoint must not configure bridge")
	}
	if configuredService(&previewStub{}).Authorize("wrong") {
		t.Fatal("wrong token authorized")
	}
}

func TestPublicNgrokBridgeRequiresExactFailClosedProductionConfiguration(t *testing.T) {
	valid := Config{
		Enabled:     true,
		Token:       testBridgeToken,
		OwnerID:     "owner@example.test",
		URL:         "https://hai-example.ngrok.app/api/v1/a2a",
		PublicNgrok: true,
		NgrokURL:    "https://hai-example.ngrok.app",
		RunMode:     "production",
	}
	if status := NewService(valid, &previewStub{}).Status(); !status.Configured || status.Transport != "fixed_ngrok_https" {
		t.Fatalf("valid public bridge status = %#v", status)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "explicit opt in", mutate: func(c *Config) { c.PublicNgrok = false }},
		{name: "production mode", mutate: func(c *Config) { c.RunMode = "development" }},
		{name: "login bypass disabled", mutate: func(c *Config) { c.LocalLoginBypass = true }},
		{name: "https only", mutate: func(c *Config) { c.URL = "http://hai-example.ngrok.app/api/v1/a2a" }},
		{name: "known ngrok host", mutate: func(c *Config) { c.URL = "https://example.com/api/v1/a2a" }},
		{name: "matching origin", mutate: func(c *Config) { c.NgrokURL = "https://other.ngrok.app" }},
		{name: "exact endpoint path", mutate: func(c *Config) { c.URL = "https://hai-example.ngrok.app/a2a" }},
		{name: "no public port", mutate: func(c *Config) { c.URL = "https://hai-example.ngrok.app:8443/api/v1/a2a" }},
		{name: "public flag cannot label local URL", mutate: func(c *Config) { c.URL = "http://127.0.0.1/api/v1/a2a" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			status := NewService(config, &previewStub{}).Status()
			if status.Configured || status.ConfigError == "" {
				t.Fatalf("misconfigured public bridge status = %#v", status)
			}
		})
	}
}

func TestLocalBridgeRequiresExactEndpointPath(t *testing.T) {
	service := NewService(Config{Enabled: true, Token: testBridgeToken, OwnerID: "owner", URL: "http://127.0.0.1/not-a2a"}, &previewStub{})
	if status := service.Status(); status.Configured || !strings.Contains(status.ConfigError, "/api/v1/a2a") {
		t.Fatalf("local path validation status = %#v", status)
	}
}

func TestLocalBridgeRejectsLANEndpointAndWhitespaceToken(t *testing.T) {
	lan := NewService(Config{Enabled: true, Token: testBridgeToken, OwnerID: "owner", URL: "http://192.168.1.10/api/v1/a2a"}, &previewStub{})
	if status := lan.Status(); status.Configured || !strings.Contains(status.ConfigError, "must be local") {
		t.Fatalf("LAN endpoint status = %#v", status)
	}
	spaced := NewService(Config{Enabled: true, Token: "aaaaaaaaaaaaaaaa aaaaaaaaaaaaaaaa", OwnerID: "owner", URL: "http://127.0.0.1/api/v1/a2a"}, &previewStub{})
	if status := spaced.Status(); status.Configured || !strings.Contains(status.ConfigError, "non-whitespace") {
		t.Fatalf("whitespace token status = %#v", status)
	}
}

func TestHandlerProvidesCardAndTokenBoundedSendMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(configuredService(&previewStub{}))
	router := gin.New()
	router.GET("/.well-known/agent-card.json", handler.AgentCard)
	router.POST("/api/v1/a2a", handler.Send)

	card := httptest.NewRecorder()
	router.ServeHTTP(card, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), "hai_controlled_planning") || !strings.Contains(card.Body.String(), "supportedInterfaces") || !strings.Contains(card.Body.String(), "http://127.0.0.1/api/v1/a2a") {
		t.Fatalf("agent card = %d %s", card.Code, card.Body.String())
	}
	var cardPayload map[string]any
	if json.Unmarshal(card.Body.Bytes(), &cardPayload) != nil || cardPayload["url"] != nil || cardPayload["protocolVersion"] != nil {
		t.Fatalf("agent card retains legacy top-level interface fields: %s", card.Body.String())
	}
	if cache := card.Header().Get("Cache-Control"); cache == "" || card.Header().Get("ETag") == "" {
		t.Fatalf("agent card caching headers = %q / %q", cache, card.Header().Get("ETag"))
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"message-1","role":"ROLE_USER","parts":[{"text":"Plan a source-backed response","mediaType":"text/plain"}]}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/a2a", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testBridgeToken)
	request.Header.Set("A2A-Version", "1.0")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hai-controlled-planning-proposal") || !strings.Contains(response.Body.String(), "TASK_STATE_COMPLETED") || strings.Contains(response.Body.String(), "sourceContext") {
		t.Fatalf("task response = %d %s", response.Code, response.Body.String())
	}

	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/a2a", strings.NewReader(body))
	deniedRequest.Header.Set("Authorization", "Bearer not-the-token")
	deniedRequest.Header.Set("A2A-Version", "1.0")
	deniedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("invalid token status = %d", denied.Code)
	}
}

func TestHandlerRejectsOldShapesAndUnsupportedA2AVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(configuredService(&previewStub{}))
	router := gin.New()
	router.POST("/api/v1/a2a", handler.Send)

	oldShape := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"old"}]}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/a2a", strings.NewReader(oldShape))
	request.Header.Set("Authorization", "Bearer "+testBridgeToken)
	request.Header.Set("A2A-Version", "1.0")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "only SendMessage") {
		t.Fatalf("old shape response = %d %s", response.Code, response.Body.String())
	}

	unsupportedVersion := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"message-1","role":"ROLE_USER","parts":[{"text":"test"}]}}}`
	request = httptest.NewRequest(http.MethodPost, "/api/v1/a2a", strings.NewReader(unsupportedVersion))
	request.Header.Set("Authorization", "Bearer "+testBridgeToken)
	request.Header.Set("A2A-Version", "0.3")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "-32009") {
		t.Fatalf("version response = %d %s", response.Code, response.Body.String())
	}
}

func TestHandlerRejectsAmbiguousJSONRPCEnvelopesBeforePlanning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	planner := &previewStub{}
	handler := NewHandler(configuredService(planner))
	router := gin.New()
	router.POST("/api/v1/a2a", handler.Send)

	valid := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"message-1","role":"ROLE_USER","parts":[{"text":"test"}]}}}`
	tests := map[string]string{
		"trailing document":   valid + `{}`,
		"object id":           `{"jsonrpc":"2.0","id":{"unsafe":true},"method":"SendMessage","params":{"message":{"messageId":"message-1","role":"ROLE_USER","parts":[{"text":"test"}]}}}`,
		"unknown envelope":    `{"jsonrpc":"2.0","id":1,"method":"SendMessage","unexpected":true,"params":{"message":{"messageId":"message-1","role":"ROLE_USER","parts":[{"text":"test"}]}}}`,
		"unknown message key": `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"message-1","role":"ROLE_USER","unexpected":true,"parts":[{"text":"test"}]}}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/a2a", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+testBridgeToken)
			request.Header.Set("A2A-Version", "1.0")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
	if len(planner.requests) != 0 {
		t.Fatalf("ambiguous envelopes reached planner: %#v", planner.requests)
	}
}

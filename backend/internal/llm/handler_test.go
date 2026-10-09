package llm

import (
	"automation-hub-backend/internal/models"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestGenerateHandlerIgnoresClientPaidApprovalFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "paid answer"}},
			},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	paidIndex := providerIndex(t, policy, "paid-provider")
	policy.Providers[paidIndex].Enabled = true
	policy.Providers[paidIndex].EndpointURL = server.URL
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	handler := &Handler{service: &Service{policy: policy}}

	body, err := json.Marshal(GenerateRequest{
		Task:              "Handle a difficult verification task",
		AllowPaidApproved: true,
		RouteDecision: &RouteDecision{
			SelectedProviderID: "paid-provider",
			SelectedModelID:    "paid-high-capability",
			SelectedModelName:  "Paid high capability model",
			Tier:               TierExpensive,
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.Generate(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if called {
		t.Fatalf("paid provider endpoint was called from client-supplied approval")
	}
	var result GenerationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
}

func TestProviderProbeHandlersRecordAndReturnHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]string{{"name": "phi3:mini"}},
		})
	}))
	defer server.Close()

	service := &Service{
		policy:       Policy{Providers: []Provider{{ID: "ollama", Name: "Ollama", Enabled: true, Local: true, EndpointURL: server.URL}}},
		probeHistory: &fakeProbeHistoryRepository{},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/probes", handler.ProviderProbes)
	router.GET("/probes/history", handler.ProviderProbeHistory)

	probeResponse := httptest.NewRecorder()
	router.ServeHTTP(probeResponse, httptest.NewRequest(http.MethodGet, "/probes", nil))
	if probeResponse.Code != http.StatusOK {
		t.Fatalf("probe status = %d, body=%s", probeResponse.Code, probeResponse.Body.String())
	}

	historyResponse := httptest.NewRecorder()
	router.ServeHTTP(historyResponse, httptest.NewRequest(http.MethodGet, "/probes/history?limit=5", nil))
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history status = %d, body=%s", historyResponse.Code, historyResponse.Body.String())
	}
	var history []ProviderProbeResult
	if err := json.Unmarshal(historyResponse.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode probe history: %v", err)
	}
	if len(history) != 1 || !history[0].Live || history[0].LastSuccessfulAt == nil {
		t.Fatalf("history = %#v, want persisted live probe", history)
	}
}

func TestContextAwareRoutingAndMaintenanceHandlersReturnGatewayTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&Service{})
	router := gin.New()
	router.POST("/route", handler.Route)
	router.GET("/maintenance/history", handler.ModelMaintenanceHistory)

	deadlineContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	routeRequest := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader(`{"task":"classify this request"}`)).WithContext(deadlineContext)
	routeRequest.Header.Set("Content-Type", "application/json")
	routeResponse := httptest.NewRecorder()
	router.ServeHTTP(routeResponse, routeRequest)
	if routeResponse.Code != http.StatusGatewayTimeout || !strings.Contains(routeResponse.Body.String(), "model routing did not finish") {
		t.Fatalf("route deadline response = %d %q, want 504 with a timeout message", routeResponse.Code, routeResponse.Body.String())
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/maintenance/history", nil).WithContext(deadlineContext)
	historyResponse := httptest.NewRecorder()
	router.ServeHTTP(historyResponse, historyRequest)
	if historyResponse.Code != http.StatusGatewayTimeout || !strings.Contains(historyResponse.Body.String(), "model maintenance history did not load") {
		t.Fatalf("maintenance history deadline response = %d %q, want 504 with a timeout message", historyResponse.Code, historyResponse.Body.String())
	}
}

func TestContextAwareRoutingAndMaintenanceHandlersReturnServiceUnavailableOnCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&Service{})
	router := gin.New()
	router.POST("/route", handler.Route)
	router.GET("/maintenance/history", handler.ModelMaintenanceHistory)

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "routing", method: http.MethodPost, path: "/route", body: `{"task":"classify this request"}`},
		{name: "maintenance history", method: http.MethodGet, path: "/maintenance/history"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)).WithContext(requestContext)
			if test.method == http.MethodPost {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "request was cancelled") {
				t.Fatalf("cancellation response = %d %q, want 503 with a cancellation message", response.Code, response.Body.String())
			}
		})
	}
}

func TestProviderProbesHandlerPropagatesRequestCancellationToProbeLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstStarted := make(chan struct{}, 1)
	firstCancelled := make(chan struct{}, 1)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstStarted <- struct{}{}
		select {
		case <-r.Context().Done():
			firstCancelled <- struct{}{}
		case <-time.After(4 * time.Second):
		}
	}))
	defer first.Close()
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "model"}}})
	}))
	defer second.Close()

	service := &Service{
		policy: Policy{Providers: []Provider{
			{ID: "ollama", Name: "Ollama", Enabled: true, Local: true, EndpointURL: first.URL},
			{ID: "lm-studio", Name: "LM Studio", Enabled: true, Local: true, EndpointURL: second.URL},
		}},
		probeHistory: &fakeProbeHistoryRepository{},
	}
	router := gin.New()
	router.GET("/probes", NewHandler(service).ProviderProbes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/probes", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(requestDone)
	}()
	select {
	case <-firstStarted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("manual probe did not reach its first provider")
	}
	select {
	case <-firstCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation did not reach the provider probe")
	}
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("manual provider-probe loop did not stop after cancellation")
	}
	if got := secondCalls.Load(); got != 0 {
		t.Fatalf("probe loop contacted %d later provider(s) after cancellation", got)
	}
}

func TestRunDueModelMaintenanceHandlerReturnsAggregateOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &Service{policy: Policy{}, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}}
	handler := NewHandler(service)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/model-maintenance/run", nil)

	handler.RunDueModelMaintenance(context)

	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("\"eligible\":0")) || !bytes.Contains(response.Body.Bytes(), []byte("\"results\":[]")) {
		t.Fatalf("maintenance run = %d %s", response.Code, response.Body.String())
	}
}

func TestRunDueModelMaintenanceHandlerRespectsEmergencyStop(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	service := &Service{policy: Policy{}, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}}
	handler := NewHandler(service)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/model-maintenance/run", nil)
	handler.RunDueModelMaintenance(context)

	if response.Code != http.StatusConflict {
		t.Fatalf("maintenance run = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "blocked") {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestGenerationHistoryHandlerReturnsRedactedLedger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &fakeGenerationHistoryRepository{records: []models.LLMGenerationRecord{{
		ProviderID: "ollama", ModelID: "qwen2.5:7b", Status: "completed", InputTokens: 12, OutputTokens: 4, UsageSource: "provider_reported", LoggedAt: time.Now().UTC(),
	}}}
	handler := NewHandler(&Service{generationHistory: history})
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/generations?limit=5", nil)

	handler.GenerationHistory(context)

	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("\"output\"")) {
		t.Fatalf("generation history = %d %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("\"usageSource\":\"provider_reported\"")) {
		t.Fatalf("generation history did not return usage evidence: %s", response.Body.String())
	}
}

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

	policy := withTestPaidProvider(testPolicyWithoutEndpoints(), server.URL)
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	handler := &Handler{service: &Service{policy: policy}}

	body, err := json.Marshal(GenerateRequest{
		Task:              "Handle a difficult verification task",
		AllowPaidApproved: true,
		RouteDecision: &RouteDecision{
			SelectedProviderID: "test-paid-provider",
			SelectedModelID:    "test-paid-high-capability",
			SelectedModelName:  "Test paid high capability model",
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

func TestGenerateHandlerCancelsProviderWithHTTPContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseProvider
	}))
	defer func() {
		close(releaseProvider)
		server.CloseClientConnections()
		server.Close()
	}()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, usage: map[string]UsageCounter{}})
	handler := NewHandlerWithEffectContext(service, func(*gin.Context) (EffectContext, error) {
		return *trustedTestEffectContext(), nil
	})
	body, err := json.Marshal(GenerateRequest{
		Task: "Summarize this short note",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierLocal,
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(response)
	ginContext.Request = request
	done := make(chan struct{})
	go func() {
		handler.Generate(ginContext)
		close(done)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("generation handler did not return after HTTP cancellation")
	}

	var result GenerationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if response.Code != http.StatusOK || result.Status != "stopped" {
		t.Fatalf("canceled handler response = %d %#v", response.Code, result)
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

func TestRunDueModelMaintenanceHandlerStopsWithCanceledRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &fakeModelMaintenanceRepository{}
	service := &Service{policy: testPolicyWithoutEndpoints(), maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	handler := NewHandler(service)
	response := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(response)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/model-maintenance/run", nil).WithContext(requestContext)

	handler.RunDueModelMaintenance(ginContext)

	if response.Body.Len() != 0 {
		t.Fatalf("canceled handler wrote a response body: %s", response.Body.String())
	}
	if len(history.records) != 0 {
		t.Fatalf("canceled handler persisted maintenance records: %#v", history.records)
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

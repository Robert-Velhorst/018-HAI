package llm

import (
	"automation-hub-backend/internal/models"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestModelMaintenanceCheckedAtReflectsProviderCompletion(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")

	tests := []struct {
		name               string
		responseStatus     int
		responseBody       string
		wantStatus         string
		wantFailureBackoff bool
	}{
		{
			name:           "successful provider catalog check",
			responseStatus: http.StatusOK,
			responseBody:   `{"data":[{"id":"configured-model"}]}`,
			wantStatus:     "provider_managed",
		},
		{
			name:               "failed provider check preserves retry backoff",
			responseStatus:     http.StatusServiceUnavailable,
			responseBody:       `{"error":"temporary outage"}`,
			wantStatus:         "failed",
			wantFailureBackoff: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestStarted := make(chan struct{}, 1)
			releaseProvider := make(chan struct{})
			providerCompleted := make(chan time.Time, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestStarted <- struct{}{}
				<-releaseProvider
				providerCompleted <- time.Now().UTC()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseStatus)
				_, _ = fmt.Fprint(w, tt.responseBody)
			}))
			defer server.Close()

			provider := Provider{
				ID: "test-cloud", Name: "Test provider", Enabled: true,
				EndpointURL: server.URL, QuotaRemaining: 1,
				Models: []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
			}
			history := &fakeModelMaintenanceRepository{}
			service := &Service{
				policy: Policy{
					FreeCloudQuotaAllowed: true,
					Providers:             []Provider{provider},
				},
				maintenanceHistory: history,
				maintenanceRunning: make(map[string]*sync.Mutex),
			}

			resultChannel := make(chan ModelMaintenanceResult, 1)
			go func() {
				resultChannel <- service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0])
			}()

			select {
			case <-requestStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("maintenance did not reach the test provider")
			}
			close(releaseProvider)

			var providerCompletedAt time.Time
			select {
			case providerCompletedAt = <-providerCompleted:
			case <-time.After(2 * time.Second):
				t.Fatal("test provider did not complete its delayed response")
			}

			var result ModelMaintenanceResult
			select {
			case result = <-resultChannel:
			case <-time.After(2 * time.Second):
				t.Fatal("maintenance did not finish after provider completion")
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("maintenance status = %q, want %q (reason: %s)", result.Status, tt.wantStatus, result.Reason)
			}
			if result.CheckedAt.IsZero() || result.CheckedAt.Before(providerCompletedAt) {
				t.Fatalf("CheckedAt = %s, want timestamp at or after provider completion %s", result.CheckedAt, providerCompletedAt)
			}

			history.mu.Lock()
			records := append([]models.LLMModelMaintenance(nil), history.records...)
			history.mu.Unlock()
			if len(records) != 1 {
				t.Fatalf("persisted records = %d, want exactly one", len(records))
			}
			if !records[0].CheckedAt.Equal(result.CheckedAt) {
				t.Fatalf("persisted CheckedAt = %s, result CheckedAt = %s", records[0].CheckedAt, result.CheckedAt)
			}

			if tt.wantFailureBackoff {
				if result.Status == "provider_managed" || !result.BlocksExecution {
					t.Fatalf("failed provider response was treated as successful: %#v", result)
				}
				wantRetryAt := result.CheckedAt.Add(modelMaintenanceFailureRetryInterval())
				if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(wantRetryAt) {
					t.Fatalf("NextCheckDueAt = %v, want failure backoff deadline %s", result.NextCheckDueAt, wantRetryAt)
				}
				return
			}

			if result.BlocksExecution {
				t.Fatalf("successful provider response unexpectedly blocks execution: %#v", result)
			}
			wantNextCheck := result.CheckedAt.Add(modelMaintenanceInterval())
			if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(wantNextCheck) {
				t.Fatalf("NextCheckDueAt = %v, want daily deadline %s", result.NextCheckDueAt, wantNextCheck)
			}
		})
	}
}

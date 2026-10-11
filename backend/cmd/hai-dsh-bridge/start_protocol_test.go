package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func startProtocolFixture(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client, config, lease) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{baseURL: baseURL, token: strings.Repeat("t", 32)}
	leased := lease{Token: "one-use-lease-token", WorkerID: "worker-01", ApprovalDigest: strings.Repeat("a", 64), StopRevision: 9}
	leased.Job.ID = uuid.NewString()
	return server, server.Client(), configuration, leased
}

func TestBridgeStartIntentUsesExactBindingForIdempotentReplay(t *testing.T) {
	var received []startProtocolRequest
	server, client, configuration, leased := startProtocolFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 32) {
			t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
		}
		if !strings.HasSuffix(request.URL.Path, "/start-intent") {
			t.Errorf("path = %q", request.URL.Path)
		}
		pathParts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		jobID, parseErr := uuid.Parse(pathParts[len(pathParts)-2])
		if parseErr != nil {
			t.Errorf("job id in path: %v", parseErr)
		}
		var payload startProtocolRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		received = append(received, payload)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(startIntentReceipt{
			JobID: jobID, IntentID: payload.IntentID, WorkerID: "worker-01",
			ApprovalDigest: payload.ApprovalDigest, StopRevision: payload.StopRevision,
		})
	}))
	configuration.baseURL, _ = url.Parse(server.URL)

	binding, err := newStartProtocolRequest(leased)
	if err != nil {
		t.Fatalf("newStartProtocolRequest: %v", err)
	}
	first, err := beginStartIntent(context.Background(), client, configuration, leased, binding)
	if err != nil || first == nil {
		t.Fatalf("begin start intent: receipt=%#v err=%v", first, err)
	}
	second, err := beginStartIntent(context.Background(), client, configuration, leased, binding)
	if err != nil || second == nil || *first != *second {
		t.Fatalf("idempotent begin replay: first=%#v second=%#v err=%v", first, second, err)
	}
	if len(received) != 2 || received[0] != binding || received[1] != binding {
		t.Fatalf("replayed binding payloads = %#v; want same one-shot identity", received)
	}
}

func TestBridgeStartIntentRejectsMismatchedReceiptAndLeaseBinding(t *testing.T) {
	_, client, configuration, leased := startProtocolFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload startProtocolRequest
		_ = json.NewDecoder(request.Body).Decode(&payload)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(startIntentReceipt{
			JobID: uuid.New(), IntentID: payload.IntentID, WorkerID: "worker-01",
			ApprovalDigest: payload.ApprovalDigest, StopRevision: payload.StopRevision,
		})
	}))
	binding, err := newStartProtocolRequest(leased)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beginStartIntent(context.Background(), client, configuration, leased, binding); !errors.Is(err, errStartOutcomeReview) {
		t.Fatalf("mismatched receipt error = %v, want fail-closed review", err)
	}

	binding.StopRevision++
	if _, err := beginStartIntent(context.Background(), client, configuration, leased, binding); err == nil {
		t.Fatal("client sent an intent whose stop revision did not match its lease")
	}
}

func TestBridgeAckAmbiguityIsReconciledOrMadeNonRetryable(t *testing.T) {
	var ackCalls, unknownCalls int
	server, client, configuration, leased := startProtocolFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/start-ack"):
			ackCalls++
			writer.WriteHeader(http.StatusInternalServerError)
		case strings.HasSuffix(request.URL.Path, "/start-unknown"):
			unknownCalls++
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	configuration.baseURL, _ = url.Parse(server.URL)
	binding, err := newStartProtocolRequest(leased)
	if err != nil {
		t.Fatal(err)
	}
	if err := acknowledgeActualStart(context.Background(), client, configuration, leased, binding); err != nil {
		t.Fatalf("ack transport uncertainty reconciled against committed server acknowledgment: %v", err)
	}
	if ackCalls != 1 || unknownCalls != 1 {
		t.Fatalf("calls: ack=%d unknown=%d; want one each and no process retry", ackCalls, unknownCalls)
	}
}

func TestBridgeAckAmbiguityThatWasNotCommittedRequiresReview(t *testing.T) {
	_, client, configuration, leased := startProtocolFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":"start_outcome_review"}`))
	}))
	binding, err := newStartProtocolRequest(leased)
	if err != nil {
		t.Fatal(err)
	}
	if err := acknowledgeActualStart(context.Background(), client, configuration, leased, binding); !errors.Is(err, errStartOutcomeReview) {
		t.Fatalf("unresolved actual-start report error = %v, want non-retryable review", err)
	}
}

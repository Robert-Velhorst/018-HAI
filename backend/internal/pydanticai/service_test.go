package pydanticai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type maintenanceGateStub struct {
	endpoint string
	modelID  string
	calls    int
	err      error
}

func (s *maintenanceGateStub) EnsureConfiguredLocalModel(endpointURL, modelID string) error {
	s.calls++
	s.endpoint, s.modelID = endpointURL, modelID
	return s.err
}

func TestPydanticAIBridgeUsesOnlyLocalTypedProposalRunner(t *testing.T) {
	input := Request{Request: "Prepare a source-grounded plan", SuccessCriteria: []string{"Use relevant sources", "Do not send messages"}}
	events := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			events = append(events, "healthz")
			_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
		case "/v1/probe":
			if r.Method != http.MethodPost || r.Header.Get("User-Agent") != "HAI-PydanticAI-Proposal/1.0" {
				t.Fatalf("unexpected probe request")
			}
			_, _ = w.Write([]byte(`{"status":"ok","engine":"pydantic-ai 2.13.0","modelId":"qwen-local"}`))
		case "/v1/propose":
			events = append(events, "propose")
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "HAI-PydanticAI-Proposal/1.0" {
				t.Fatalf("unexpected proposal request")
			}
			var received Request
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil || received.Request != input.Request {
				t.Fatalf("unexpected input: %#v %v", received, err)
			}
			result := Response{Engine: "pydantic-ai 2.13.0", ModelID: "qwen-local", RequestDigest: requestDigest(input), Proposal: Proposal{Goal: "Prepare a source-grounded plan", SuccessCriteria: []string{"Use relevant sources"}, NextSteps: []string{"Review the relevant evidence"}, Risk: "low", RequiresApproval: false, Reasons: []string{"No external action is proposed"}, Uncertainties: []string{}}}
			_ = json.NewEncoder(w).Encode(result)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	gate := &maintenanceGateStub{}
	service := WithModelMaintenance(NewService(true, server.URL, 0, nil), gate)
	if probe, err := service.Probe(context.Background()); err != nil || !probe.Reachable || probe.ModelID != "qwen-local" {
		t.Fatalf("unexpected probe: %#v %v", probe, err)
	}
	events = events[:0]
	result, err := service.Propose(context.Background(), input)
	if err != nil || result.Proposal.Risk != "low" || result.Proposal.NextSteps[0] != "Review the relevant evidence" {
		t.Fatalf("unexpected proposal: %#v %v", result, err)
	}
	if len(events) != 2 || events[0] != "healthz" || events[1] != "propose" {
		t.Fatalf("proposal order = %#v, want healthz then propose", events)
	}
	if gate.calls != 1 || gate.endpoint != "http://127.0.0.1:11434/v1" || gate.modelID != "qwen-local" {
		t.Fatalf("maintenance gate received calls=%d endpoint=%q model=%q", gate.calls, gate.endpoint, gate.modelID)
	}
}

func TestPydanticAIProposalFailsClosedBeforeRunnerProposal(t *testing.T) {
	for _, test := range []struct {
		name       string
		gate       *maintenanceGateStub
		wantHealth int
	}{
		{name: "gate unavailable"},
		{name: "maintenance rejects model", gate: &maintenanceGateStub{err: errors.New("daily refresh failed")}, wantHealth: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var healthCalls, proposalCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					healthCalls++
					_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
				case "/v1/propose":
					proposalCalls++
					w.WriteHeader(http.StatusInternalServerError)
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
				}
			}))
			defer server.Close()

			var gate ModelMaintenanceGate
			if test.gate != nil {
				gate = test.gate
			}
			service := WithModelMaintenance(NewService(true, server.URL, 0, nil), gate)
			if _, err := service.Propose(context.Background(), Request{Request: "Prepare a bounded plan"}); err == nil {
				t.Fatal("proposal must be blocked when maintenance is unavailable or rejected")
			}
			if healthCalls != test.wantHealth || proposalCalls != 0 {
				t.Fatalf("runner calls: health=%d proposal=%d, want health=%d proposal=0", healthCalls, proposalCalls, test.wantHealth)
			}
			if test.gate != nil && test.gate.calls != 1 {
				t.Fatalf("maintenance gate called %d times, want 1", test.gate.calls)
			}
		})
	}
}

func TestPydanticAIBridgeRejectsExternalDisabledAndUnboundedRequests(t *testing.T) {
	external := NewService(true, "https://example.com", 0, nil)
	if external.Status().Configured || external.Status().ConfigError == "" {
		t.Fatalf("external runner must be rejected: %#v", external.Status())
	}
	disabled := NewService(false, "http://127.0.0.1:8080", 0, nil)
	_, err := disabled.Propose(context.Background(), Request{Request: "Make a plan"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("disabled runner must not be contacted: %v", err)
	}
	_, err = NewService(true, "http://127.0.0.1:8080", 0, nil).Propose(context.Background(), Request{Request: "\n"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("multiline request must be rejected: %v", err)
	}
}

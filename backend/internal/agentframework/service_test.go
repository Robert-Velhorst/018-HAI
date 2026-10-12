package agentframework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type maintenanceGateStub struct {
	endpoint       string
	modelID        string
	calls          int
	onCall         func()
	waitForContext bool
	err            error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (s *maintenanceGateStub) EnsureConfiguredLocalModelWithContext(ctx context.Context, endpointURL, modelID string) error {
	s.calls++
	if s.onCall != nil {
		s.onCall()
	}
	s.endpoint, s.modelID = endpointURL, modelID
	if s.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.err
}

func TestAgentFrameworkBridgeUsesOnlyLocalPlanningRunner(t *testing.T) {
	input := Request{Request: "Prepare a source-grounded plan", SuccessCriteria: []string{"Use relevant sources", "Do not send messages"}}
	events := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			events = append(events, "healthz")
			_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
		case "/v1/probe":
			events = append(events, "probe")
			if r.Method != http.MethodPost || r.Header.Get("User-Agent") != "HAI-Agent-Framework-Planning/1.0" {
				t.Fatalf("unexpected probe request")
			}
			_, _ = w.Write([]byte(`{"status":"ok","engine":"microsoft-agent-framework core=1.11.0","modelId":"qwen-local"}`))
		case "/v1/propose":
			events = append(events, "propose")
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "HAI-Agent-Framework-Planning/1.0" {
				t.Fatalf("unexpected proposal request")
			}
			var received runnerRequest
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil || received.Request != input.Request {
				t.Fatalf("unexpected input: %#v %v", received, err)
			}
			_ = json.NewEncoder(w).Encode(Response{Status: "completed", Engine: "microsoft-agent-framework core=1.11.0", ModelID: "qwen-local", RequestDigest: requestDigest(input), Proposal: Proposal{Goal: "Prepare a source-grounded plan", SuccessCriteria: []string{"Use relevant sources"}, NextSteps: []string{"Review relevant evidence"}, Risk: "low", RequiresApproval: false, Reasons: []string{"No external action is proposed"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	gate := &maintenanceGateStub{onCall: func() { events = append(events, "maintenance") }}
	service := WithModelMaintenance(NewService(true, server.URL, 0, nil), gate)
	if probe, err := service.Probe(context.Background()); err != nil || !probe.Reachable || probe.ModelID != "qwen-local" {
		t.Fatalf("unexpected probe: %#v %v", probe, err)
	}
	events = events[:0]
	if result, err := service.Propose(context.Background(), input); err != nil || result.Proposal.NextSteps[0] != "Review relevant evidence" || result.Status != proposalStatusDraft {
		t.Fatalf("unexpected proposal: %#v %v", result, err)
	}
	if len(events) != 3 || events[0] != "healthz" || events[1] != "maintenance" || events[2] != "propose" {
		t.Fatalf("proposal order = %#v, want healthz, maintenance, propose", events)
	}
	if gate.endpoint != "http://127.0.0.1:11434/v1" || gate.modelID != "qwen-local" {
		t.Fatalf("model maintenance gate received %#v", gate)
	}
}

func TestAgentFrameworkStatusDistinguishesAdvisoryGuidanceFromExecutableSkills(t *testing.T) {
	status := NewService(false, "", 0, nil).Status()
	capabilities := strings.Join(status.Capabilities, "\n")
	restrictions := strings.Join(status.Restrictions, "\n")
	if !strings.Contains(capabilities, "owner-consented HAI catalog summaries as bounded advisory planning context") {
		t.Fatalf("status does not disclose the supported guidance path: %#v", status.Capabilities)
	}
	if !strings.Contains(restrictions, "executable skill runtimes or upstream skill assets") ||
		!strings.Contains(restrictions, "HAI catalog summaries are bounded non-authoritative text only") ||
		!strings.Contains(restrictions, "grant no tools, access, approval, or execution capability") {
		t.Fatalf("status does not distinguish advisory text from executable authority: %#v", status.Restrictions)
	}
}

func TestAgentFrameworkCancellationStopsBeforePlanningRequest(t *testing.T) {
	var healthCalls, proposalCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			healthCalls++
			_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
		case "/v1/propose":
			proposalCalls++
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	gate := &maintenanceGateStub{}
	service := WithModelMaintenance(NewService(true, server.URL, 0, nil), gate)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Propose(ctx, Request{Request: "Prepare a bounded plan"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request error = %v, want context.Canceled", err)
	}
	if healthCalls != 0 || gate.calls != 0 || proposalCalls != 0 {
		t.Fatalf("cancelled request reached work: health=%d maintenance=%d propose=%d", healthCalls, gate.calls, proposalCalls)
	}
}

func TestAgentFrameworkCancellationDuringMaintenanceDoesNotSubmitPlan(t *testing.T) {
	var proposalCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
		case "/v1/propose":
			proposalCalls++
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	gate := &maintenanceGateStub{onCall: cancel}
	service := WithModelMaintenance(NewService(true, server.URL, 0, nil), gate)
	if _, err := service.Propose(ctx, Request{Request: "Prepare a bounded plan"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request error = %v, want context.Canceled", err)
	}
	if gate.calls != 1 || proposalCalls != 0 {
		t.Fatalf("cancelled request reached maintenance=%d planner=%d; want maintenance=1 planner=0", gate.calls, proposalCalls)
	}
}

func TestAgentFrameworkCancellationReachesPlanningRunner(t *testing.T) {
	proposeStarted := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/healthz":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`)),
				Request:    request,
			}, nil
		case "/v1/propose":
			close(proposeStarted)
			<-request.Context().Done()
			return nil, request.Context().Err()
		default:
			t.Errorf("unexpected request path %s", request.URL.Path)
			return nil, errors.New("unexpected request")
		}
	})}

	service := WithModelMaintenance(NewService(true, "http://127.0.0.1:8080", 0, client), &maintenanceGateStub{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := service.Propose(ctx, Request{Request: "Prepare a bounded plan"})
		result <- err
	}()

	select {
	case <-proposeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("planning request did not reach the local runner")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled planning request error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("planning request did not return after cancellation")
	}
}

func TestAgentFrameworkTimeoutAppliesToInjectedHTTPClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	service := NewService(true, "http://127.0.0.1:8080", 20*time.Millisecond, client)

	started := time.Now()
	_, err := service.Probe(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("injected client ignored configured request timeout: elapsed %s", elapsed)
	}
}

func TestAgentFrameworkInjectedClientDoesNotFollowExternalRedirects(t *testing.T) {
	var externalCalls int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalCalls++
		_, _ = w.Write([]byte(`{"status":"ok","engine":"microsoft-agent-framework core=1.11.0","modelId":"qwen-local"}`))
	}))
	defer external.Close()

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", external.URL+"/v1/probe")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer local.Close()

	service := NewService(true, local.URL, time.Second, &http.Client{})
	if _, err := service.Probe(context.Background()); err == nil {
		t.Fatal("probe accepted a redirect response")
	}
	if externalCalls != 0 {
		t.Fatalf("injected client followed redirect to an external host %d times", externalCalls)
	}
}

func TestAgentFrameworkTimeoutCancelsModelMaintenance(t *testing.T) {
	var proposalCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
		case "/v1/propose":
			proposalCalls++
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	gate := &maintenanceGateStub{waitForContext: true}
	service := WithModelMaintenance(NewService(true, server.URL, 20*time.Millisecond, nil), gate)
	started := time.Now()
	_, err := service.Propose(context.Background(), Request{Request: "Prepare a bounded plan"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("proposal error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("model maintenance exceeded request timeout: elapsed %s", elapsed)
	}
	if gate.calls != 1 || proposalCalls != 0 {
		t.Fatalf("maintenance calls=%d planner calls=%d; want one maintenance call and no planner request", gate.calls, proposalCalls)
	}
}

func TestAgentFrameworkRequiresExplicitApprovalAndRejectsHighRiskWithoutIt(t *testing.T) {
	input := Request{Request: "Prepare a bounded plan"}
	tests := []struct {
		name          string
		risk          string
		approvalField bool
		requires      bool
		wantValid     bool
	}{
		{name: "approval omitted", risk: "low"},
		{name: "high risk without approval", risk: "high", approvalField: true},
		{name: "high risk with approval", risk: "high", approvalField: true, requires: true, wantValid: true},
		{name: "low risk without approval", risk: "low", approvalField: true, wantValid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proposal := map[string]any{
				"goal": "Prepare a bounded plan", "successCriteria": []string{"Review evidence"},
				"nextSteps": []string{"Review the source"}, "risk": test.risk,
				"reasons": []string{"Planning only"}, "uncertainties": []string{},
			}
			if test.approvalField {
				proposal["requiresApproval"] = test.requires
			}
			body, err := json.Marshal(map[string]any{
				"status": "completed", "engine": "microsoft-agent-framework core=1.11.0",
				"modelId": "qwen-local", "requestDigest": requestDigest(input), "proposal": proposal,
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					_, _ = w.Write([]byte(`{"status":"ok","configured":true,"modelId":"qwen-local","modelEndpoint":"http://127.0.0.1:11434/v1"}`))
				case "/v1/propose":
					_, _ = w.Write(body)
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
				}
			}))
			defer server.Close()

			service := WithModelMaintenance(NewService(true, server.URL, 0, nil), &maintenanceGateStub{})
			result, err := service.Propose(context.Background(), input)
			if test.wantValid {
				if err != nil || result == nil || result.Status != proposalStatusDraft {
					t.Fatalf("proposal = %#v, error = %v; want an explicitly marked draft", result, err)
				}
				return
			}
			if err == nil || result != nil {
				t.Fatalf("proposal = %#v, error = %v; want invalid approval metadata rejected", result, err)
			}
		})
	}
}

func TestAgentFrameworkProposalFailsClosedBeforeRunnerSideEffect(t *testing.T) {
	tests := []struct {
		name        string
		gatePresent bool
		wantHealth  int
		gateErr     error
	}{
		{name: "gate unavailable"},
		{name: "maintenance rejected", gatePresent: true, gateErr: errors.New("daily model refresh failed"), wantHealth: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gate *maintenanceGateStub
			var policy ModelMaintenanceGate
			if test.gatePresent {
				gate = &maintenanceGateStub{err: test.gateErr}
				policy = gate
			}
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

			service := WithModelMaintenance(NewService(true, server.URL, 0, nil), policy)
			if _, err := service.Propose(context.Background(), Request{Request: "Prepare a bounded plan"}); err == nil {
				t.Fatal("proposal must be blocked when maintenance is unavailable or rejected")
			}
			if healthCalls != test.wantHealth || proposalCalls != 0 {
				t.Fatalf("runner calls: health=%d proposal=%d, want health=%d proposal=0", healthCalls, proposalCalls, test.wantHealth)
			}
			if gate != nil && gate.calls != 1 {
				t.Fatalf("maintenance gate called %d times, want 1", gate.calls)
			}
			if gate == nil && healthCalls != 0 {
				t.Fatalf("runner was contacted without a maintenance gate: health calls=%d", healthCalls)
			}
		})
	}
}

func TestAgentFrameworkBridgeRejectsExternalDisabledAndUnboundedRequests(t *testing.T) {
	external := NewService(true, "https://example.com", 0, nil)
	if external.Status().Configured || external.Status().ConfigError == "" {
		t.Fatalf("external runner must be rejected: %#v", external.Status())
	}
	if _, err := NewService(false, "http://127.0.0.1:8080", 0, nil).Propose(context.Background(), Request{Request: "Make a plan"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("disabled runner must not be contacted: %v", err)
	}
	if _, err := NewService(true, "http://127.0.0.1:8080", 0, nil).Propose(context.Background(), Request{Request: "\n"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("multiline request must be rejected: %v", err)
	}
}

func TestRequestDigestRetainsEmptyCriteriaArrayForRunnerParity(t *testing.T) {
	input := Request{Request: "Prepare a bounded plan"}
	encoded, err := json.Marshal(struct {
		Request         string   `json:"request"`
		SuccessCriteria []string `json:"successCriteria"`
	}{Request: input.Request, SuccessCriteria: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if got, want := requestDigest(input), hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestBoundedRunnerJSONRejectsOversizeAndTrailingValues(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		max  int64
	}{
		{name: "over limit", body: `{"ok":true}`, max: 5},
		{name: "second JSON value", body: `{"ok":true} {"ok":false}`, max: 128},
	} {
		t.Run(test.name, func(t *testing.T) {
			var decoded map[string]bool
			if err := decodeBoundedJSON(strings.NewReader(test.body), test.max, &decoded); err == nil {
				t.Fatalf("accepted invalid bounded response %#v", decoded)
			}
		})
	}
}

func TestRequestDigestEscapesHTMLLikeTheIsolatedPythonRunner(t *testing.T) {
	input := Request{Request: "Review A&B < C", SuccessCriteria: []string{"Keep <evidence> source-linked"}}
	const expected = "e091ac81205677f94ac2ea17c0a9f5b7759c2f4b0ee93b9ceaaf6cc43e70b7af"
	if got := requestDigest(input); got != expected {
		t.Fatalf("digest = %s, want %s", got, expected)
	}
}

package agentruntime

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/safety"
)

func TestMain(m *testing.M) {
	if os.Getenv("HAI_RUNTIME_TASK_ID") == "block-runtime-cli" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if len(os.Args) > 1 && (os.Args[1] == "chat" || os.Args[1] == "agent") {
		for _, arg := range os.Args[1:] {
			fmt.Fprintln(os.Stdout, arg)
		}
		for _, key := range []string{
			"HERMES_HOME",
			"HERMES_PROFILE",
			"HERMES_IGNORE_USER_CONFIG",
			"TERMINAL_CWD",
			"OPENCLAW_STATE_DIR",
			"OPENCLAW_HOME",
			"HAI_RUNTIME_TASK_ID",
		} {
			if value, ok := os.LookupEnv(key); ok {
				fmt.Fprintf(os.Stdout, "%s=%s\n", key, value)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRegistryRequiresApproval(t *testing.T) {
	adapter := &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
	registry := NewRegistry(adapter)
	result := registry.Execute(context.Background(), "test", Task{
		ID:            "task-1",
		Prompt:        "do work",
		OwnerIdentity: "alice",
	})
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("unapproved task was executed: %#v", result)
	}
}

func TestRegistryRejectsCallerControlledApprovalFlagWithoutProvenance(t *testing.T) {
	adapter := &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
	registry := NewRegistry(adapter)

	for _, task := range []Task{
		{ID: "task-1", Prompt: "do work", OwnerIdentity: "alice", HumanApproved: true},
		{ID: "task-2", Prompt: "do work", OwnerIdentity: "alice", HumanApproved: true, ApprovalSourceID: "workflow-decision:forged"},
		{ID: "task-3", Prompt: "do work", HumanApproved: true, ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111"},
	} {
		result := registry.Execute(context.Background(), "test", task)
		if result.Status != "blocked" || adapter.called {
			t.Fatalf("unproven approval task executed: task=%#v result=%#v", task, result)
		}
	}
}

func TestRegistryBlocksWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	adapter := &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
	registry := newVerifiedTestRegistry(adapter)
	result := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "run approved work"))
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("emergency stop did not prevent runtime execution: %#v", result)
	}
	if !strings.Contains(result.Message, "emergency stop") || !containsString(result.AuditEvents, "emergency stop blocked agent runtime execution") {
		t.Fatalf("emergency stop result lacks controlled audit evidence: %#v", result)
	}
}

func TestRegistryBlocksWhenPersistedEmergencyStopActive(t *testing.T) {
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator paused execution", nil
	}))
	defer restore()

	adapter := &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
	registry := newVerifiedTestRegistry(adapter)
	result := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "run approved work"))
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("persisted emergency stop did not prevent runtime execution: %#v", result)
	}
	if result.Message != "operator paused execution" {
		t.Fatalf("blocked reason = %q", result.Message)
	}
}

func TestRegistryExecutesApprovedTask(t *testing.T) {
	adapter := &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
	task := approvedRuntimeTask("task-1", "do work")
	task.ProjectKey = "project-1"
	task = withValidFinalEffectProof("test", task, adapter.info)
	var captured FinalEffectAuthorizationRequest
	var capturedProof FinalEffectAuthorizationProof
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			captured = request
			capturedProof = proof
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)
	result := registry.Execute(context.Background(), "test", task)
	if result.Status != "completed" || !adapter.called {
		t.Fatalf("approved task was not executed: %#v", result)
	}
	sum := sha256.Sum256([]byte(task.Prompt))
	if captured.Operation != runtimeExecuteTaskOperation ||
		captured.RuntimeID != "test" ||
		captured.TaskID != task.ID ||
		captured.OwnerIdentity != task.OwnerIdentity ||
		captured.ProjectKey != task.ProjectKey ||
		captured.ApprovalSourceID != task.ApprovalSourceID ||
		captured.PromptDigest != hex.EncodeToString(sum[:]) ||
		!captured.RequiresApproval {
		t.Fatalf("final-effect request was not exactly bound to the task: %#v", captured)
	}
	if capturedProof != task.FinalEffectProof {
		t.Fatalf("verifier did not receive the task proof: got=%#v want=%#v", capturedProof, task.FinalEffectProof)
	}
	if !containsString(result.AuditEvents, "runtime adapter invoked with verified consumed authorization proof") {
		t.Fatalf("result lacks final-effect authorization evidence: %#v", result.AuditEvents)
	}
}

func TestRegistryFailsClosedWithoutFinalEffectVerifier(t *testing.T) {
	adapter := executableFakeAdapter()
	registry := NewRegistry(adapter)

	result := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))

	if result.Status != "blocked" || adapter.called {
		t.Fatalf("registry without verifier reached adapter: result=%#v called=%t", result, adapter.called)
	}
	if !strings.Contains(result.Message, "could not be verified") ||
		!containsString(result.AuditEvents, "final-effect proof verifier failed closed before runtime adapter access") {
		t.Fatalf("fail-closed result lacks controlled evidence: %#v", result)
	}
}

func TestRegistryFinalEffectProofFailuresNeverReachAdapter(t *testing.T) {
	tests := []struct {
		name              string
		mutate            func(*FinalEffectAuthorizationProof)
		verifierErr       error
		wantVerifierCalls int
	}{
		{
			name: "missing receipt",
			mutate: func(proof *FinalEffectAuthorizationProof) {
				proof.ReceiptID = ""
			},
		},
		{
			name: "invalid authorization request digest",
			mutate: func(proof *FinalEffectAuthorizationProof) {
				proof.AuthorizationRequestDigest = "not-a-sha256-digest"
			},
		},
		{
			name: "invalid decision digest",
			mutate: func(proof *FinalEffectAuthorizationProof) {
				proof.DecisionDigest = "not-a-sha256-digest"
			},
		},
		{
			name: "runtime binding belongs to another request",
			mutate: func(proof *FinalEffectAuthorizationProof) {
				proof.RuntimeRequestDigest = strings.Repeat("b", 64)
			},
		},
		{
			name: "arbitrary syntactically valid receipt rejected by durable verifier",
			mutate: func(proof *FinalEffectAuthorizationProof) {
				proof.ReceiptID = "22222222-2222-4222-8222-222222222222"
			},
			verifierErr:       errors.New("receipt not found"),
			wantVerifierCalls: 1,
		},
		{
			name:              "verifier unavailable",
			verifierErr:       errors.New("policy database unavailable"),
			wantVerifierCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := executableFakeAdapter()
			task := approvedRuntimeTask("task-1", "do work")
			if test.mutate != nil {
				test.mutate(&task.FinalEffectProof)
			}
			verifierCalls := 0
			registry := NewRegistryWithFinalEffectVerifier(
				FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
					verifierCalls++
					if test.verifierErr != nil {
						return test.verifierErr
					}
					return verifyTestFinalEffectProof(request, proof)
				}),
				adapter,
			)

			result := registry.Execute(context.Background(), "test", task)

			if result.Status != "blocked" || adapter.called {
				t.Fatalf("invalid proof reached adapter: result=%#v called=%t", result, adapter.called)
			}
			if verifierCalls != test.wantVerifierCalls {
				t.Fatalf("verifier calls=%d want=%d", verifierCalls, test.wantVerifierCalls)
			}
			if strings.Contains(result.Message, "policy database unavailable") ||
				strings.Contains(result.Message, "receipt not found") {
				t.Fatalf("internal verifier error leaked to caller: %#v", result)
			}
		})
	}
}

func TestRegistryRechecksEmergencyStopAfterFinalEffectProofVerification(t *testing.T) {
	engaged := false
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return engaged, "operator engaged stop during proof verification", nil
	}))
	defer restore()

	adapter := executableFakeAdapter()
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			if err := verifyTestFinalEffectProof(request, proof); err != nil {
				return err
			}
			engaged = true
			return nil
		}),
		adapter,
	)

	result := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))

	if result.Status != "blocked" || adapter.called {
		t.Fatalf("stop engaged after authorization reached adapter: result=%#v called=%t", result, adapter.called)
	}
	if result.Message != "operator engaged stop during proof verification" ||
		!containsString(result.AuditEvents, "verified final-effect proof was not exercised") {
		t.Fatalf("post-authorization emergency-stop evidence missing: %#v", result)
	}
}

func TestRegistryCancellationDuringProofVerificationNeverReachesAdapter(t *testing.T) {
	adapter := executableFakeAdapter()
	verificationStarted := make(chan struct{})
	releaseVerification := make(chan struct{})
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			close(verificationStarted)
			<-releaseVerification
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))
	}()

	select {
	case <-verificationStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("final-effect proof verification did not start")
	}
	stop := registry.StopTask(context.Background(), "test", "task-1", "alice")
	if stop.Status != "stopping" {
		t.Fatalf("stop while verifying proof = %#v", stop)
	}
	close(releaseVerification)

	select {
	case result := <-resultCh:
		if result.Status != "blocked" || adapter.called {
			t.Fatalf("cancelled proof verification reached adapter: result=%#v called=%t", result, adapter.called)
		}
		if !containsString(result.AuditEvents, "runtime cancellation observed after final-effect proof verification") {
			t.Fatalf("cancellation evidence missing: %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled execution did not return")
	}
}

func TestRegistryReadAPIsRemainAvailableWithoutFinalEffectVerifier(t *testing.T) {
	adapter := executableFakeAdapter()
	adapter.info.ID = "hermes"
	registry := NewRegistry(adapter)

	if got := registry.List(); len(got) != 1 || got[0].ID != "hermes" {
		t.Fatalf("runtime discovery should not require execution authorization: %#v", got)
	}
	health := registry.Health(context.Background())
	if len(health) != 1 || health[0].RuntimeID != "hermes" || health[0].Status != "ready" {
		t.Fatalf("health should not require execution authorization: %#v", health)
	}
	skills, err := registry.Skills(context.Background(), "hermes")
	if err != nil || len(skills) != 1 {
		t.Fatalf("skills should not require execution authorization: skills=%#v err=%v", skills, err)
	}
}

func TestBindConsumedAuthorizationProofBindsExactTaskAndRejectsMutation(t *testing.T) {
	adapter := executableFakeAdapter()
	binder := NewRegistry(adapter)
	task := approvedRuntimeTask("task-1", "do work")
	task.FinalEffectProof = FinalEffectAuthorizationProof{}

	bound, err := binder.BindConsumedAuthorizationProof(
		"test",
		task,
		"11111111-1111-4111-8111-111111111111",
		strings.Repeat("c", 64),
		strings.Repeat("a", 64),
		"signed-runtime-handoff",
	)
	if err != nil {
		t.Fatalf("bind consumed authorization proof: %v", err)
	}
	expectedRequest := runtimeFinalEffectRequest("test", bound, adapter.info)
	if bound.FinalEffectProof.RuntimeRequestDigest != finalEffectRequestDigest(expectedRequest) ||
		bound.FinalEffectProof.RuntimeProof != "signed-runtime-handoff" {
		t.Fatalf("runtime proof was not bound exactly: %#v", bound.FinalEffectProof)
	}

	verifierCalls := 0
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(context.Context, FinalEffectAuthorizationRequest, FinalEffectAuthorizationProof) error {
			verifierCalls++
			return nil
		}),
		adapter,
	)
	bound.Prompt = "mutated after authorization handoff"
	result := registry.Execute(context.Background(), "test", bound)
	if result.Status != "blocked" || adapter.called || verifierCalls != 0 {
		t.Fatalf("mutated task crossed proof boundary: result=%#v called=%t verifierCalls=%d", result, adapter.called, verifierCalls)
	}

	if _, err := binder.BindConsumedAuthorizationProof(
		"test",
		task,
		"arbitrary-id",
		strings.Repeat("c", 64),
		strings.Repeat("a", 64),
		"",
	); err == nil {
		t.Fatal("binder accepted malformed receipt id")
	}
}

func TestRegistryBlockMessageIncludesConfigurationReasons(t *testing.T) {
	adapter := &fakeAdapter{info: Info{
		ID:                   "test",
		Enabled:              true,
		Configured:           false,
		ExecutionEnabled:     false,
		RequiresApproval:     true,
		MissingConfiguration: []string{"TEST_WORKSPACE", "runtime endpoint missing"},
	}}
	registry := newVerifiedTestRegistry(adapter)
	result := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("misconfigured runtime should not execute: %#v", result)
	}
	for _, expected := range []string{"runtime not configured", "execution disabled", "TEST_WORKSPACE", "runtime endpoint missing"} {
		if !strings.Contains(result.Message, expected) {
			t.Fatalf("block message %q missing %q", result.Message, expected)
		}
	}
	if !strings.Contains(strings.Join(result.AuditEvents, " "), "runtime registry policy blocked execution") {
		t.Fatalf("expected registry policy audit event: %#v", result.AuditEvents)
	}
}

func TestRegistryStopTaskCancelsActiveRuntimeExecution(t *testing.T) {
	adapter := &blockingAdapter{
		info: Info{
			ID:               "test",
			Enabled:          true,
			Configured:       true,
			ExecutionEnabled: true,
			RequiresApproval: true,
		},
		started: make(chan struct{}),
	}
	registry := newVerifiedTestRegistry(adapter)
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))
	}()

	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime task did not start")
	}

	wrongOwner := registry.StopTask(context.Background(), "test", "task-1", "bob")
	if wrongOwner.Status != "blocked" || !strings.Contains(wrongOwner.Message, "different owner") {
		t.Fatalf("cross-owner stop result = %#v", wrongOwner)
	}

	stop := registry.StopTask(context.Background(), "test", "task-1", "alice")
	if stop.Status != "stopping" || !strings.Contains(stop.Message, "cancellation signal") {
		t.Fatalf("stop result = %#v", stop)
	}

	select {
	case result := <-resultCh:
		if result.Status != "blocked" || !strings.Contains(result.Message, "cancelled") {
			t.Fatalf("runtime result after stop = %#v", result)
		}
		if !containsString(result.AuditEvents, "runtime registry cancellation observed") {
			t.Fatalf("runtime cancellation audit missing: %#v", result.AuditEvents)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime task was not cancelled")
	}
}

func TestRegistryRejectsDuplicateActiveRuntimeTaskID(t *testing.T) {
	adapter := &blockingAdapter{
		info: Info{
			ID:               "test",
			Enabled:          true,
			Configured:       true,
			ExecutionEnabled: true,
			RequiresApproval: true,
		},
		started: make(chan struct{}),
	}
	registry := newVerifiedTestRegistry(adapter)
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do work"))
	}()

	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime task did not start")
	}

	duplicate := registry.Execute(context.Background(), "test", approvedRuntimeTask("task-1", "do duplicate work"))
	if duplicate.Status != "blocked" || !strings.Contains(duplicate.Message, "already running") {
		t.Fatalf("duplicate task result = %#v", duplicate)
	}

	_ = registry.StopTask(context.Background(), "test", "task-1", "alice")
	select {
	case <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime task was not cancelled")
	}
}

func TestOdysseusAdapterUsesAgentModeWithoutBash(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/codex/capabilities" {
			_, _ = w.Write([]byte(`{"integration":"codex"}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"delta\":\"completed safely\"}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := &odysseusAdapter{
		enabled:     true,
		baseURL:     server.URL,
		token:       "test-token",
		sessionID:   "session-1",
		timeout:     30 * time.Second,
		outputLimit: defaultOutputLimit,
		allowedHost: map[string]bool{"127.0.0.1": true},
	}
	result := adapter.ExecuteTask(context.Background(), Task{Prompt: "inspect the task", HumanApproved: true})
	if result.Status != "completed" || result.Output != "completed safely" {
		t.Fatalf("result = %#v", result)
	}
	for _, expected := range []string{"mode=agent", "allow_bash=false", "allow_web_search=false"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("request body %q missing %q", body, expected)
		}
	}
}

func TestAgentRuntimeHTTPClientRejectsBlockedDNSResolution(t *testing.T) {
	t.Parallel()

	client := noRedirectClientWithResolver(5*time.Second, func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	})
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("agent runtime transport must not inherit environment proxy settings")
	}
	_, err := transport.DialContext(context.Background(), "tcp", "odysseus.example:443")
	if err == nil || !strings.Contains(err.Error(), "blocked address space") {
		t.Fatalf("DialContext error = %v, want blocked address rejection", err)
	}
}

func TestOdysseusAdapterFromEnvReusesDirectHTTPClient(t *testing.T) {
	t.Setenv("ODYSSEUS_AGENT_TIMEOUT_SECONDS", "15")
	adapter := newOdysseusAdapterFromEnv()
	if adapter.httpClient == nil || adapter.directHTTPClient() != adapter.directHTTPClient() {
		t.Fatal("production Odysseus adapter must reuse its bounded HTTP client")
	}
	transport, ok := adapter.httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("Odysseus transport = %#v, want direct no-proxy transport", adapter.httpClient.Transport)
	}
}

func TestRuntimeEndpointURLsRejectEmbeddedCredentials(t *testing.T) {
	t.Parallel()

	odysseus := &odysseusAdapter{
		baseURL:     "https://user:secret@odysseus.example",
		allowedHost: map[string]bool{"odysseus.example": true},
	}
	if reason := odysseus.validBaseURL(); !strings.Contains(reason, "must not contain credentials") {
		t.Fatalf("Odysseus URL rejection = %q", reason)
	}
	openClaw := &openClawAdapter{
		gatewayURL: "wss://user:secret@openclaw.example",
		allowedHost: map[string]bool{
			"openclaw.example": true,
		},
	}
	if reason := openClaw.validGatewayURL(); !strings.Contains(reason, "must not contain credentials") {
		t.Fatalf("OpenClaw URL rejection = %q", reason)
	}
}

func TestOdysseusStreamRejectsTruncatedOutput(t *testing.T) {
	if _, err := readOdysseusStream(strings.NewReader("data: {\"delta\":\"partial\"}\n\n"), 4096); err == nil {
		t.Fatalf("expected incomplete stream to be rejected")
	}
}

func TestOdysseusInfoAdvertisesEcosystemAndControls(t *testing.T) {
	adapter := &odysseusAdapter{
		enabled:                    true,
		baseURL:                    "http://127.0.0.1:7000",
		token:                      "scoped-token",
		sessionID:                  "session-1",
		timeout:                    30 * time.Second,
		outputLimit:                defaultOutputLimit,
		allowedHost:                map[string]bool{"127.0.0.1": true},
		todosEnabled:               true,
		emailEnabled:               true,
		calendarEnabled:            true,
		documentsEnabled:           true,
		memorySyncEnabled:          true,
		researchEnabled:            true,
		searchEnabled:              true,
		mcpEnabled:                 true,
		cookbookEnabled:            true,
		localModelDiscoveryEnabled: true,
		shellEnabled:               true,
		codexBridgeEnabled:         true,
		claudeBridgeEnabled:        true,
		agentMigrationEnabled:      true,
		contextBudgetEnabled:       true,
	}
	info := adapter.Info()
	if !info.Configured || !info.ExecutionEnabled {
		t.Fatalf("expected configured Odysseus adapter: %#v", info)
	}
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"scoped Codex API", "MCP manager", "Cookbook model-serving", "Codex and Claude bridge", "context budget"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("Odysseus capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side HAI approval", "scoped ODYSSEUS_API_TOKEN", "AGENT_RUNTIME_ALLOWED_HOSTS", "allow_bash=false"} {
		if !strings.Contains(joinedControls, expected) {
			t.Fatalf("Odysseus control %q not advertised: %#v", expected, info.Controls)
		}
	}
	if len(info.Architecture) == 0 {
		t.Fatalf("expected Odysseus architecture chain to be visible")
	}
}

func TestHermesWorkspaceMustStayInsideRuntimeRoot(t *testing.T) {
	root := t.TempDir()
	adapter := &hermesAdapter{
		workspaceRoot: root,
		workspace:     root + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "outside",
	}
	if reason := adapter.workspaceBlockedReason(); reason == "" {
		t.Fatalf("expected workspace escape to be rejected")
	}
}

func TestHermesInfoAdvertisesEcosystemAndControls(t *testing.T) {
	adapter := &hermesAdapter{
		enabled:          true,
		executable:       "hermes",
		workspace:        t.TempDir(),
		workspaceRoot:    t.TempDir(),
		toolsets:         []string{"safe", "skills"},
		skills:           []string{"legal-drafting"},
		terminalBackends: []string{"local", "docker"},
		gatewayEnabled:   true,
		mcpEnabled:       true,
	}
	info := adapter.Info()
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"skills and skill learning", "MCP servers and tools", "gateway channels", "subagent delegation", "ACP adapter"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("Hermes capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side HAI approval", "AGENT_RUNTIME_WORKSPACE_ROOT", "HERMES_TOOLSETS=safe,skills", "HERMES_SKILLS=legal-drafting"} {
		if !strings.Contains(joinedControls, expected) {
			t.Fatalf("Hermes control %q not advertised: %#v", expected, info.Controls)
		}
	}
	if len(info.Architecture) == 0 {
		t.Fatalf("expected Hermes architecture chain to be visible")
	}
}

func TestHermesAdapterInvokesControlledCli(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve native test executable: %v", err)
	}
	adapter := &hermesAdapter{
		enabled:          true,
		executable:       executable,
		home:             home,
		profile:          "hai",
		workspace:        workspace,
		workspaceRoot:    root,
		maxTurns:         3,
		timeout:          30 * time.Second,
		toolsets:         []string{"safe"},
		skills:           []string{"legal-drafting"},
		outputLimit:      defaultOutputLimit,
		ignoreUserConfig: true,
		terminalBackends: []string{"local"},
	}

	result := adapter.ExecuteTask(context.Background(), Task{ID: "task-1", Prompt: "draft safely", ProjectKey: "case-1"})
	if result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
	for _, expected := range []string{
		"chat", "-q", "draft safely", "-Q", "--source", "tool", "--max-turns", "3", "--checkpoints",
		"--toolsets", "safe", "--skills", "legal-drafting",
		"HERMES_HOME=" + home, "HERMES_PROFILE=hai", "HERMES_IGNORE_USER_CONFIG=1", "TERMINAL_CWD=" + workspace,
	} {
		if !strings.Contains(result.Output, expected) {
			t.Fatalf("output %q missing %q", result.Output, expected)
		}
	}
	if strings.Contains(result.Output, "--yolo") {
		t.Fatalf("Hermes execution must not use --yolo: %q", result.Output)
	}
}

func TestHermesAdapterCancelsControlledCli(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve native test executable: %v", err)
	}
	adapter := &hermesAdapter{
		enabled:       true,
		executable:    executable,
		workspace:     root,
		workspaceRoot: root,
		maxTurns:      1,
		timeout:       30 * time.Second,
		outputLimit:   defaultOutputLimit,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() {
		done <- adapter.ExecuteTask(ctx, Task{ID: "block-runtime-cli", Prompt: "wait"})
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case result := <-done:
		if result.Status != "blocked" || !strings.Contains(result.Message, "process tree was stopped") {
			t.Fatalf("canceled Hermes result = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Hermes CLI remained alive after caller cancellation")
	}
}

func TestOpenClawInfoAdvertisesEcosystemAndControls(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	adapter := &openClawAdapter{
		enabled:            true,
		executable:         "openclaw",
		workspace:          workspace,
		workspaceRoot:      root,
		gatewayURL:         "ws://127.0.0.1:18789",
		gatewayToken:       "gateway-token",
		thinking:           "high",
		timeout:            30 * time.Second,
		outputLimit:        defaultOutputLimit,
		allowedHost:        map[string]bool{"127.0.0.1": true},
		agentCLIEnabled:    true,
		gatewayEnabled:     true,
		messagesEnabled:    true,
		skillsEnabled:      true,
		pluginsEnabled:     true,
		mcpEnabled:         true,
		memoryEnabled:      true,
		cronEnabled:        true,
		browserEnabled:     true,
		canvasEnabled:      true,
		nodesEnabled:       true,
		voiceEnabled:       true,
		talkEnabled:        true,
		webchatEnabled:     true,
		multiAgentEnabled:  true,
		localModelsEnabled: true,
		highRiskExecution:  true,
		sandboxRequired:    true,
		sandboxMode:        "all",
		sandboxDocker:      true,
		channelsEnabled:    []string{"whatsapp", "telegram"},
		providersEnabled:   []string{"ollama", "openrouter"},
		companionApps:      []string{"windows", "android"},
	}
	info := adapter.Info()
	if !info.Configured || !info.ExecutionEnabled {
		t.Fatalf("expected configured OpenClaw adapter: %#v", info)
	}
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"local-first Gateway", "multi-channel inbox", "multi-agent session routing", "skills, ClawHub packages", "Live Canvas", "sandbox backends"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("OpenClaw capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side HAI approval", "openclaw agent --message", "AGENT_RUNTIME_WORKSPACE_ROOT", "OPENCLAW_GATEWAY_TOKEN"} {
		if !strings.Contains(joinedControls, expected) {
			t.Fatalf("OpenClaw control %q not advertised: %#v", expected, info.Controls)
		}
	}
	if len(info.Architecture) == 0 {
		t.Fatalf("expected OpenClaw architecture chain to be visible")
	}
}

func TestOpenClawHighRiskSurfacesBlockExecutionUntilAcknowledged(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	adapter := &openClawAdapter{
		enabled:          true,
		executable:       "openclaw",
		workspace:        workspace,
		workspaceRoot:    root,
		allowedHost:      map[string]bool{"127.0.0.1": true},
		agentCLIEnabled:  true,
		messagesEnabled:  true,
		browserEnabled:   true,
		hostToolsEnabled: true,
		sandboxRequired:  true,
		sandboxMode:      "all",
	}

	info := adapter.Info()
	if info.Configured || info.ExecutionEnabled {
		t.Fatalf("high-risk OpenClaw surfaces should block generic execution: %#v", info)
	}
	joinedMissing := strings.Join(info.MissingConfiguration, " ")
	for _, expected := range []string{"high-risk surfaces", "messaging/channel", "browser control", "host tools"} {
		if !strings.Contains(joinedMissing, expected) {
			t.Fatalf("missing configuration %q did not explain %q", joinedMissing, expected)
		}
	}
	if health := adapter.HealthCheck(context.Background()); health.Status != "blocked" || !strings.Contains(health.Reason, "high-risk surfaces") {
		t.Fatalf("health should be blocked by high-risk surfaces: %#v", health)
	}
	result := adapter.ExecuteTask(context.Background(), Task{Prompt: "do work", HumanApproved: true})
	if result.Status != "blocked" || !strings.Contains(result.Message, "high-risk surfaces") {
		t.Fatalf("execution should be blocked by high-risk surfaces: %#v", result)
	}
	registry := NewRegistry(adapter)
	registryResult := registry.Execute(context.Background(), "openclaw", approvedRuntimeTask("task-1", "do work"))
	if registryResult.Status != "blocked" || !strings.Contains(registryResult.Message, "browser control") || !strings.Contains(registryResult.Message, "host tools") {
		t.Fatalf("registry execution should preserve OpenClaw policy reason: %#v", registryResult)
	}

	adapter.highRiskExecution = true
	info = adapter.Info()
	if !info.Configured || !info.ExecutionEnabled {
		t.Fatalf("explicit acknowledgement should allow configured runtime state: %#v", info)
	}
	if !strings.Contains(strings.Join(info.Controls, " "), "OPENCLAW_ALLOW_HIGH_RISK_EXECUTION=true") {
		t.Fatalf("controls should disclose high-risk acknowledgement: %#v", info.Controls)
	}
}

func TestOpenClawSetEcosystemPathReleasesPreviousUploadArtifact(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	firstUpload := filepath.Join(root, "openclaw-ecosystem-first.zip")
	secondUpload := filepath.Join(root, "openclaw-ecosystem-second.zip")
	manualFirst := filepath.Join(root, "openclaw-main.zip")
	manualSecond := filepath.Join(root, "openclaw-checkpoint.zip")
	if err := writeMinimalOpenClawZip(firstUpload); err != nil {
		t.Fatalf("write first upload file: %v", err)
	}
	if err := writeMinimalOpenClawZip(secondUpload); err != nil {
		t.Fatalf("write second upload file: %v", err)
	}
	if err := writeMinimalOpenClawZip(manualFirst); err != nil {
		t.Fatalf("write manual file: %v", err)
	}
	if err := writeMinimalOpenClawZip(manualSecond); err != nil {
		t.Fatalf("write manual file: %v", err)
	}

	adapter := &openClawAdapter{
		enabled:         true,
		executable:      "openclaw",
		workspace:       workspace,
		workspaceRoot:   root,
		ecosystemPath:   firstUpload,
		allowedHost:     map[string]bool{"127.0.0.1": true},
		agentCLIEnabled: true,
	}
	registry := NewRegistry(adapter)

	if _, err := registry.SetOpenClawEcosystemPath(secondUpload); err != nil {
		t.Fatalf("set second upload path: %v", err)
	}
	if _, err := os.Stat(firstUpload); err == nil {
		t.Fatalf("expected previous upload path to be removed: %s", firstUpload)
	}

	if _, err := registry.SetOpenClawEcosystemPath(manualFirst); err != nil {
		t.Fatalf("set manual path: %v", err)
	}
	if _, err := os.Stat(secondUpload); err == nil {
		t.Fatalf("expected upload path to be removed when leaving it: %s", secondUpload)
	}

	if _, err := registry.SetOpenClawEcosystemPath(manualSecond); err != nil {
		t.Fatalf("set second manual path: %v", err)
	}
	if _, err := os.Stat(manualFirst); err != nil {
		t.Fatalf("manual path should not be removed by cleanup: %v", err)
	}
}

func TestOpenClawAdapterInvokesControlledCli(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve native test executable: %v", err)
	}
	adapter := &openClawAdapter{
		enabled:         true,
		executable:      executable,
		workspace:       workspace,
		workspaceRoot:   root,
		stateDir:        stateDir,
		thinking:        "high",
		timeout:         30 * time.Second,
		outputLimit:     defaultOutputLimit,
		allowedHost:     map[string]bool{"127.0.0.1": true},
		agentCLIEnabled: true,
		sandboxRequired: true,
		sandboxMode:     "all",
	}

	result := adapter.ExecuteTask(context.Background(), Task{ID: "task-1", Prompt: "move safe work forward", ProjectKey: "project-1"})
	if result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
	for _, expected := range []string{
		"agent", "--message", "move safe work forward", "--thinking", "high",
		"HAI approved OpenClaw task envelope", "Execution mode:", "Blocked surfaces:", "Validation checklist:",
		"OPENCLAW_STATE_DIR=" + stateDir, "OPENCLAW_HOME=" + stateDir, "HAI_RUNTIME_TASK_ID=task-1",
	} {
		if !strings.Contains(result.Output, expected) {
			t.Fatalf("output %q missing %q", result.Output, expected)
		}
	}
	for _, forbidden := range []string{"openclaw message send", "pairing approve --yes", "--yolo"} {
		if strings.Contains(result.Output, forbidden) {
			t.Fatalf("OpenClaw execution exposed forbidden operation %q in %q", forbidden, result.Output)
		}
	}
}

func TestOpenClawAdapterCancelsControlledCli(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve native test executable: %v", err)
	}
	adapter := &openClawAdapter{
		enabled:         true,
		executable:      executable,
		workspace:       root,
		workspaceRoot:   root,
		timeout:         30 * time.Second,
		outputLimit:     defaultOutputLimit,
		agentCLIEnabled: true,
		sandboxRequired: true,
		sandboxMode:     "all",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() {
		done <- adapter.ExecuteTask(ctx, Task{ID: "block-runtime-cli", Prompt: "wait"})
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case result := <-done:
		if result.Status != "blocked" || !strings.Contains(result.Message, "process tree was stopped") {
			t.Fatalf("canceled OpenClaw result = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OpenClaw CLI remained alive after caller cancellation")
	}
}

func TestOpenClawTaskEnvelopeRoutesThroughIndexedSkillsAndControls(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "openclaw-main.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{
		"openclaw-main/package.json",
		"openclaw-main/.agents/skills/autoreview/SKILL.md",
		"openclaw-main/.agents/skills/gitcrawl/SKILL.md",
		"openclaw-main/.agents/skills/channel-message-flows/SKILL.md",
		"openclaw-main/.agents/skills/technical-documentation/SKILL.md",
		"openclaw-main/extensions/whatsapp/package.json",
		"openclaw-main/extensions/ollama/package.json",
		"openclaw-main/extensions/browser/package.json",
		"openclaw-main/extensions/openshell/package.json",
		"openclaw-main/.github/workflows/ci.yml",
		"openclaw-main/.github/actions/setup-node-env/action.yml",
		"openclaw-main/.github/codeql/codeql-core-auth-secrets-critical-security.yml",
		"openclaw-main/.github/ISSUE_TEMPLATE/bug_report.yml",
		"openclaw-main/.github/instructions/copilot.instructions.md",
		"openclaw-main/qa/live/whatsapp-smoke.md",
		"openclaw-main/test/e2e/openclaw-smoke.test.ts",
		"openclaw-main/security/secret-scanning.yml",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		payload := []byte("{}")
		if name == "openclaw-main/package.json" {
			payload = []byte(`{"name":"openclaw","version":"2026.6.10"}`)
		}
		if _, err := entry.Write(payload); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	adapter := &openClawAdapter{
		enabled:          true,
		executable:       "openclaw",
		workspace:        root,
		workspaceRoot:    root,
		ecosystemPath:    zipPath,
		agentCLIEnabled:  true,
		sandboxRequired:  true,
		sandboxMode:      "all",
		messagesEnabled:  false,
		browserEnabled:   false,
		hostToolsEnabled: false,
	}

	envelope := adapter.openClawTaskEnvelope(Task{
		ID:         "task-1",
		ProjectKey: "share-t",
		Prompt:     "Review the GitHub pull request, security CI, issue triage instructions, and draft a WhatsApp follow-up, but do not send it.",
	})
	for _, expected := range []string{
		"HAI approved OpenClaw task envelope",
		"HAI task id: task-1",
		"HAI project key: share-t",
		"software engineering and repository workflow",
		"autoreview",
		"gitcrawl",
		"channel-message-flows",
		"ollama",
		"browser",
		"Relevant OpenClaw maps:",
		"github-action:setup-node-env",
		"security:codeql-core-auth-secrets-critical-security",
		"security-asset:secret-scanning",
		"qa:live/whatsapp-smoke",
		"test:e2e/openclaw-smoke.test",
		"issue-template:bug_report",
		"instruction:copilot.instructions",
		"whatsapp outbound send without separate HAI approval",
		"outbound message sending",
		"do not send messages",
		"Return format: concise completion summary",
	} {
		if !strings.Contains(envelope, expected) {
			t.Fatalf("task envelope missing %q:\n%s", expected, envelope)
		}
	}
	if strings.Contains(envelope, "public posting is allowed") {
		t.Fatalf("task envelope should not permit public effects:\n%s", envelope)
	}
	trace := openClawRouteTrace(adapter.openClawTaskProfile(Task{
		ID:         "task-1",
		ProjectKey: "share-t",
		Prompt:     "Review the GitHub pull request, security CI, issue triage instructions, and draft a WhatsApp follow-up, but do not send it.",
	}))
	if trace == nil || trace.RuntimeID != "openclaw" || trace.Intent != "software engineering and repository workflow" {
		t.Fatalf("route trace missing OpenClaw intent: %#v", trace)
	}
	for _, expected := range []string{"autoreview", "gitcrawl", "channel-message-flows"} {
		if !containsString(trace.RecommendedSkills, expected) {
			t.Fatalf("route trace missing skill %q from %#v", expected, trace.RecommendedSkills)
		}
	}
	if !containsString(trace.VisibleProviders, "ollama") || !containsString(trace.VisibleTools, "browser") {
		t.Fatalf("route trace did not expose provider/tool context: %#v", trace)
	}
	if !containsString(trace.BlockedSurfaces, "whatsapp outbound send without separate HAI approval") {
		t.Fatalf("route trace missing blocked channel send: %#v", trace.BlockedSurfaces)
	}
	for _, expected := range []string{"github-action:setup-node-env", "security:codeql-core-auth-secrets-critical-security", "issue-template:bug_report", "instruction:copilot.instructions"} {
		if !containsString(trace.RelevantMaps, expected) {
			t.Fatalf("route trace missing OpenClaw map %q from %#v", expected, trace.RelevantMaps)
		}
	}
}

func TestOpenClawTaskEnvelopeRoutesPersonalOperatingWork(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "openclaw-main.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{
		"openclaw-main/package.json",
		"openclaw-main/AGENTS.md",
		"openclaw-main/README.md",
		"openclaw-main/pnpm-workspace.yaml",
		"openclaw-main/.github/codex/prompts/maturity-scorecard-agent.md",
		"openclaw-main/.agents/maintainer-notes/telegram.md",
		"openclaw-main/.agents/skills/taskflow/SKILL.md",
		"openclaw-main/.agents/skills/agent-transcript/SKILL.md",
		"openclaw-main/.agents/skills/claw-score/SKILL.md",
		"openclaw-main/.agents/skills/claw-score/references/completeness/whatsapp.md",
		"openclaw-main/.agents/skills/technical-documentation/SKILL.md",
		"openclaw-main/extensions/document-extract/SKILL.md",
		"openclaw-main/extensions/ollama/package.json",
		"openclaw-main/extensions/whatsapp/package.json",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		payload := []byte("{}")
		if name == "openclaw-main/package.json" {
			payload = []byte(`{"name":"openclaw","version":"2026.6.10"}`)
		}
		if _, err := entry.Write(payload); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	adapter := &openClawAdapter{
		enabled:         true,
		executable:      "openclaw",
		workspace:       root,
		workspaceRoot:   root,
		ecosystemPath:   zipPath,
		agentCLIEnabled: true,
		sandboxRequired: true,
		sandboxMode:     "all",
	}
	task := Task{
		ID:         "pursuit-1",
		ProjectKey: "robert-os",
		Prompt:     "Create a pursuit next action from WhatsApp evidence, timeline, deadline follow-up, Odoo HERP operation, and Ollama local model routing. Do not send, publish, or delete anything.",
	}
	envelope := adapter.openClawTaskEnvelope(task)
	for _, expected := range []string{
		"HAI pursuit and open-loop operations",
		"taskflow",
		"agent-transcript",
		"technical-documentation",
		"claw-score",
		"document-extract/default",
		"completeness:claw-score/completeness/whatsapp",
		"maintainer-note:telegram",
		"root-doc:AGENTS",
		"root-doc:README",
		"root-config:pnpm-workspace.yaml",
		"codex-prompt:maturity-scorecard-agent",
		"outbound communication without separate HAI approval",
		"public posting without source-grounded review and separate HAI approval",
		"destructive or irreversible file action without rollback plan and explicit approval",
		"pursuit/open-loop state and next safe action are explicit when applicable",
		"source, evidence, or missing-evidence status is reported when factual claims are made",
	} {
		if !strings.Contains(envelope, expected) {
			t.Fatalf("personal operating envelope missing %q:\n%s", expected, envelope)
		}
	}
	trace := openClawRouteTrace(adapter.openClawTaskProfile(task))
	if trace == nil || trace.Intent != "HAI pursuit and open-loop operations" {
		t.Fatalf("route trace missing personal-ops intent: %#v", trace)
	}
	for _, expected := range []string{"taskflow", "agent-transcript", "technical-documentation", "claw-score", "document-extract/default"} {
		if !containsString(trace.RecommendedSkills, expected) {
			t.Fatalf("route trace missing personal-ops skill %q from %#v", expected, trace.RecommendedSkills)
		}
	}
	if !containsString(trace.VisibleProviders, "ollama") {
		t.Fatalf("route trace should expose local model provider context: %#v", trace.VisibleProviders)
	}
}

func TestOpenClawEcosystemInventoryFromZip(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	zipPath := filepath.Join(root, "openclaw-main.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{
		"openclaw-main/package.json",
		"openclaw-main/AGENTS.md",
		"openclaw-main/CLAUDE.md",
		"openclaw-main/README.md",
		"openclaw-main/.crabbox.yaml",
		"openclaw-main/Dockerfile",
		"openclaw-main/pnpm-workspace.yaml",
		"openclaw-main/.github/codex/prompts/docs-agent.md",
		"openclaw-main/.github/codex/prompts/maturity-scorecard-agent.md",
		"openclaw-main/.github/workflows/ci.yml",
		"openclaw-main/.github/workflows/openclaw-release-publish.yml",
		"openclaw-main/.github/actions/docker-e2e-plan/action.yml",
		"openclaw-main/.github/actions/setup-node-env/action.yml",
		"openclaw-main/.github/ISSUE_TEMPLATE/bug_report.yml",
		"openclaw-main/.github/ISSUE_TEMPLATE/feature_request.yml",
		"openclaw-main/.github/codeql/codeql-core-auth-secrets-critical-security.yml",
		"openclaw-main/.github/codeql/openclaw-boundary/queries/managed-proxy-runtime-mutation.ql",
		"openclaw-main/.github/package-trusted-sources.json",
		"openclaw-main/.github/zizmor.yml",
		"openclaw-main/.github/instructions/copilot.instructions.md",
		"openclaw-main/docs/gateway/architecture.md",
		"openclaw-main/scripts/install-openclaw.mjs",
		"openclaw-main/qa/live/whatsapp-smoke.md",
		"openclaw-main/test/e2e/openclaw-smoke.test.ts",
		"openclaw-main/config/gateway.policy.json",
		"openclaw-main/security/secret-scanning.yml",
		"openclaw-main/deploy/docker-compose.preview.yml",
		"openclaw-main/fly.toml",
		"openclaw-main/.agents/maintainer-notes/telegram.md",
		"openclaw-main/.agents/skills/autoreview/SKILL.md",
		"openclaw-main/.agents/skills/autoreview/scripts/autoreview",
		"openclaw-main/.agents/skills/autoreview/scripts/test-review-harness.ps1",
		"openclaw-main/.agents/skills/gitcrawl/SKILL.md",
		"openclaw-main/.agents/skills/gitcrawl/agents/openai.yaml",
		"openclaw-main/.agents/skills/gitcrawl/references/source-map.md",
		"openclaw-main/.agents/skills/claw-score/SKILL.md",
		"openclaw-main/.agents/skills/claw-score/references/completeness/whatsapp.md",
		"openclaw-main/.agents/skills/technical-documentation/SKILL.md",
		"openclaw-main/.agents/skills/technical-documentation/agents/docs-framework-agent.md",
		"openclaw-main/.agents/skills/technical-documentation/scripts/docs-summary.mjs",
		"openclaw-main/extensions/acpx/skills/acp-router/SKILL.md",
		"openclaw-main/extensions/acpx/skills/acp-router/agents/openai.yaml",
		"openclaw-main/extensions/acpx/skills/acp-router/references/router.md",
		"openclaw-main/extensions/azure-speech/package.json",
		"openclaw-main/extensions/browser/skills/browser-automation/SKILL.md",
		"openclaw-main/extensions/github-copilot/package.json",
		"openclaw-main/extensions/lobster/SKILL.md",
		"openclaw-main/extensions/lobster/package.json",
		"openclaw-main/extensions/openai/package.json",
		"openclaw-main/extensions/ollama/package.json",
		"openclaw-main/extensions/whatsapp/package.json",
		"openclaw-main/extensions/browser/package.json",
		"openclaw-main/apps/android/README.md",
		"openclaw-main/packages/agent-core/package.json",
		"openclaw-main/skills/taskflow/SKILL.md",
		"openclaw-main/src/agents/index.ts",
		"openclaw-main/src/gateway/index.ts",
		"openclaw-main/ui/src/ui/views/overview.ts",
		"openclaw-main/ui/src/ui/views/overview.test.ts",
		"openclaw-main/ui/src/ui/controllers/exec-approval.ts",
		"openclaw-main/ui/src/ui/controllers/exec-approval.test.ts",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		payload := []byte("{}")
		if name == "openclaw-main/package.json" {
			payload = []byte(`{"name":"openclaw","version":"2026.6.10","license":"MIT","packageManager":"pnpm@11.2.2+sha512.test","engines":{"node":">=22.19.0"}}`)
		}
		if _, err := entry.Write(payload); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	adapter := &openClawAdapter{
		enabled:         true,
		executable:      "openclaw",
		workspace:       workspace,
		workspaceRoot:   root,
		ecosystemPath:   zipPath,
		agentCLIEnabled: true,
		sandboxRequired: true,
		sandboxMode:     "all",
		companionApps:   []string{"windows"},
	}
	info := adapter.Info()
	joined := strings.Join(ecosystemSurfaceStrings(info.Ecosystem), " ")
	for _, expected := range []string{
		"Skills:8",
		"Skill scripts:3",
		"Package metadata:5",
		"Configured HAI surfaces:3",
		"HAI-blocked high-risk surfaces:10",
		"Operator setup checklist:5",
		"Agent profiles:3",
		"Skill reference maps:3",
		"Completeness maps:1",
		"Maintainer notes:1",
		"Documentation corpus:1",
		"Root scripts:1",
		"QA assets:1",
		"Test suites:1",
		"Configuration profiles:1",
		"Security assets:1",
		"Deployment targets:2",
		"Codex prompt maps:2",
		"GitHub workflows:2",
		"GitHub Actions:2",
		"GitHub issue templates:2",
		"Security and CodeQL maps:4",
		"Repository instructions:1",
		"Repository docs:3",
		"Repository config:4",
		"Provider extensions:3",
		"Channel extensions:1",
		"Tool/runtime extensions:4",
		"Companion apps:1",
		"Core packages:1",
		"Source modules:2",
		"Control UI views:1",
		"Control UI controllers:1",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("ecosystem inventory missing %q from %#v", expected, info.Ecosystem)
		}
	}
	metadata := runtimeSurfaceItems(info.Ecosystem, "Package metadata")
	for _, expected := range []string{"package=openclaw", "version=2026.6.10", "license=MIT", "node=>=22.19.0", "package-manager=pnpm@11.2.2"} {
		if !containsString(metadata, expected) {
			t.Fatalf("OpenClaw metadata missing %q from %#v", expected, metadata)
		}
	}
	inventory := runtimeSurface(info.Ecosystem, "Package inventory")
	if inventory.Count != summedInventoryItems(inventory.Items) {
		t.Fatalf("package inventory count %d does not match displayed inventory items %#v", inventory.Count, inventory.Items)
	}
	if !containsString(inventory.Items, "4 tool/runtime extensions") {
		t.Fatalf("package inventory should include tool/runtime extension count: %#v", inventory.Items)
	}
	if views := runtimeSurfaceItems(info.Ecosystem, "Control UI views"); !containsString(views, "overview") || containsString(views, "overview.test") {
		t.Fatalf("OpenClaw UI view inventory should include production views only: %#v", views)
	}
	if controllers := runtimeSurfaceItems(info.Ecosystem, "Control UI controllers"); !containsString(controllers, "exec-approval") || containsString(controllers, "exec-approval.test") {
		t.Fatalf("OpenClaw controller inventory should include production controllers only: %#v", controllers)
	}
	if surface := runtimeSurface(info.Ecosystem, "Channel extensions"); surface.RiskLevel != "high" || !surface.ApprovalRequired {
		t.Fatalf("channel extensions should be high-risk and approval-gated: %#v", surface)
	}
	if surface := runtimeSurface(info.Ecosystem, "Provider extensions"); surface.RiskLevel != "medium" || !surface.ApprovalRequired {
		t.Fatalf("provider extensions should be medium-risk and approval-gated: %#v", surface)
	}
	if surface := runtimeSurface(info.Ecosystem, "Skill scripts"); surface.RiskLevel != "high" || !surface.ApprovalRequired {
		t.Fatalf("skill scripts should be high-risk and approval-gated: %#v", surface)
	}
	scripts := runtimeSurfaceItems(info.Ecosystem, "Skill scripts")
	for _, expected := range []string{"autoreview/autoreview", "autoreview/test-review-harness.ps1", "technical-documentation/docs-summary.mjs"} {
		if !containsString(scripts, expected) {
			t.Fatalf("OpenClaw script inventory missing %q from %#v", expected, scripts)
		}
	}
	skills := adapter.ListSkills(context.Background())
	foundScriptSkill := false
	for _, skill := range skills {
		if skill.Name == "autoreview/autoreview" && skill.Category == "skill_script" {
			foundScriptSkill = true
			if skill.RiskLevel != "high" || !skill.ApprovalRequired || skill.ExecutionMode != "catalog_only_not_directly_invoked" {
				t.Fatalf("OpenClaw script skill should be high-risk catalog-only: %#v", skill)
			}
		}
	}
	if !foundScriptSkill {
		t.Fatalf("OpenClaw ListSkills did not surface executable skill scripts: %#v", skills)
	}
}

func ecosystemSurfaceStrings(surfaces []RuntimeEcosystemSurface) []string {
	result := []string{}
	for _, surface := range surfaces {
		result = append(result, surface.Category+":"+strconv.Itoa(surface.Count))
	}
	return result
}

func runtimeSurfaceItems(surfaces []RuntimeEcosystemSurface, category string) []string {
	return runtimeSurface(surfaces, category).Items
}

func runtimeSurface(surfaces []RuntimeEcosystemSurface, category string) RuntimeEcosystemSurface {
	for _, surface := range surfaces {
		if surface.Category == category {
			return surface
		}
	}
	return RuntimeEcosystemSurface{}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func summedInventoryItems(values []string) int {
	total := 0
	for _, value := range values {
		var count int
		if _, err := fmt.Sscanf(value, "%d", &count); err == nil {
			total += count
		}
	}
	return total
}

func TestOpenClawSetEcosystemPathRejectsCallerSelectedPathOutsideAllowedRoots(t *testing.T) {
	parent := t.TempDir()
	allowed := filepath.Join(parent, "allowed")
	outside := filepath.Join(parent, "outside")
	for _, directory := range []string{allowed, outside} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	initial := filepath.Join(allowed, "openclaw-main.zip")
	callerSelected := filepath.Join(outside, "openclaw-main.zip")
	for _, archive := range []string{initial, callerSelected} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write OpenClaw archive %s: %v", archive, err)
		}
	}
	adapter := &openClawAdapter{
		workspace:     allowed,
		workspaceRoot: allowed,
		ecosystemPath: initial,
	}

	if err := adapter.setEcosystemPath(callerSelected); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside ecosystem path error = %v, want allowlist rejection", err)
	}
}

func TestOpenClawSetEcosystemPathRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	allowed := filepath.Join(parent, "allowed")
	outside := filepath.Join(parent, "outside")
	for _, directory := range []string{allowed, outside} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	initial := filepath.Join(allowed, "openclaw-main.zip")
	outsideArchive := filepath.Join(outside, "openclaw-main.zip")
	for _, archive := range []string{initial, outsideArchive} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write OpenClaw archive %s: %v", archive, err)
		}
	}
	link := filepath.Join(allowed, "linked-openclaw.zip")
	if err := os.Symlink(outsideArchive, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	adapter := &openClawAdapter{
		workspace:     allowed,
		workspaceRoot: allowed,
		ecosystemPath: initial,
	}

	if err := adapter.setEcosystemPath(link); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlink ecosystem path error = %v, want resolved allowlist rejection", err)
	}
}

func TestOdysseusAdapterEmergencyStopPreventsNetworkIO(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	adapter := &odysseusAdapter{
		enabled:     true,
		baseURL:     server.URL,
		timeout:     time.Second,
		outputLimit: defaultOutputLimit,
		allowedHost: map[string]bool{"127.0.0.1": true},
	}

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("task-1", "do not send"))
	if result.Status != "blocked" || calls != 0 {
		t.Fatalf("emergency-stop result=%#v calls=%d, want block before network I/O", result, calls)
	}
}

func approvedRuntimeTask(id, prompt string) Task {
	task := Task{
		ID:               id,
		Prompt:           prompt,
		OwnerIdentity:    "alice",
		HumanApproved:    true,
		ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}
	return withValidFinalEffectProof("test", task, Info{RequiresApproval: true})
}

func withValidFinalEffectProof(runtimeID string, task Task, info Info) Task {
	request := runtimeFinalEffectRequest(runtimeID, task, info)
	task.FinalEffectProof = FinalEffectAuthorizationProof{
		ReceiptID:                  "11111111-1111-4111-8111-111111111111",
		AuthorizationRequestDigest: strings.Repeat("c", 64),
		DecisionDigest:             strings.Repeat("a", 64),
		RuntimeRequestDigest:       finalEffectRequestDigest(request),
	}
	return task
}

func verifyTestFinalEffectProof(
	request FinalEffectAuthorizationRequest,
	proof FinalEffectAuthorizationProof,
) error {
	if proof.ReceiptID != "11111111-1111-4111-8111-111111111111" {
		return errors.New("receipt not found")
	}
	if proof.AuthorizationRequestDigest != strings.Repeat("c", 64) {
		return errors.New("authorization request digest mismatch")
	}
	if proof.DecisionDigest != strings.Repeat("a", 64) {
		return errors.New("decision digest mismatch")
	}
	if proof.RuntimeRequestDigest != finalEffectRequestDigest(request) {
		return errors.New("runtime request digest mismatch")
	}
	return nil
}

func newVerifiedTestRegistry(adapters ...Adapter) *Registry {
	return NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapters...,
	)
}

func executableFakeAdapter() *fakeAdapter {
	return &fakeAdapter{info: Info{
		ID:               "test",
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}}
}

type fakeAdapter struct {
	info   Info
	called bool
}

func (a *fakeAdapter) Info() Info {
	return a.info
}

func (a *fakeAdapter) HealthCheck(context.Context) Health {
	return Health{RuntimeID: a.info.ID, Status: "ready"}
}

func (a *fakeAdapter) ListSkills(context.Context) []Skill {
	return []Skill{{
		ID:               a.info.ID + ":skill:test",
		RuntimeID:        a.info.ID,
		Name:             "test",
		Category:         "skill",
		RiskLevel:        "low",
		ApprovalRequired: false,
		ExecutionMode:    "test",
	}}
}

func (a *fakeAdapter) ExecuteTask(context.Context, Task) Result {
	a.called = true
	return Result{RuntimeID: a.info.ID, Status: "completed"}
}

func (a *fakeAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return StopResult{RuntimeID: a.info.ID, TaskID: taskID, Status: "stopped"}
}

type blockingAdapter struct {
	info    Info
	started chan struct{}
}

func (a *blockingAdapter) Info() Info {
	return a.info
}

func (a *blockingAdapter) HealthCheck(context.Context) Health {
	return Health{RuntimeID: a.info.ID, Status: "ready"}
}

func (a *blockingAdapter) ListSkills(context.Context) []Skill {
	return nil
}

func (a *blockingAdapter) ExecuteTask(ctx context.Context, _ Task) Result {
	if a.started == nil {
		a.started = make(chan struct{})
	}
	close(a.started)
	<-ctx.Done()
	return Result{
		RuntimeID:   a.info.ID,
		Status:      "failed",
		Message:     ctx.Err().Error(),
		ExitCode:    -1,
		AuditEvents: []string{"blocking adapter saw context cancellation"},
	}
}

func (a *blockingAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return StopResult{RuntimeID: a.info.ID, TaskID: taskID, Status: "unsupported"}
}

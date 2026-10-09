package agentruntime

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Fprint(os.Stdout, os.Getenv("HAI_DSH_TEST_VERSION_OUTPUT"))
		os.Exit(0)
	}
	if len(os.Args) > 1 && (os.Args[1] == "chat" || os.Args[1] == "agent" || os.Args[1] == "--profile") {
		if waitForFile := os.Getenv("HAI_DSH_TEST_WAIT_FOR_FILE"); waitForFile != "" {
			for {
				if _, err := os.Stat(waitForFile); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		for _, arg := range os.Args[1:] {
			fmt.Fprintln(os.Stdout, arg)
		}
		for _, key := range []string{
			"HERMES_HOME",
			"HERMES_PROFILE",
			"HERMES_IGNORE_USER_CONFIG",
			"DSH_HOME",
			"TERMINAL_CWD",
			"OPENCLAW_STATE_DIR",
			"OPENCLAW_HOME",
			"OPENCLAW_GATEWAY_TOKEN",
			"HAI_RUNTIME_TASK_ID",
		} {
			if value, ok := os.LookupEnv(key); ok {
				fmt.Fprintf(os.Stdout, "%s=%s\n", key, value)
			}
		}
		os.Exit(0)
	}
	for _, name := range []string{"HAI_EMERGENCY_STOP", "AUTONOMY_EMERGENCY_STOP", "EMERGENCY_STOP"} {
		_ = os.Setenv(name, "false")
	}
	restoreSafety := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return false, "", nil
	}))
	code := m.Run()
	restoreSafety()
	os.Exit(code)
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

func TestRegistryFailsClosedWhenEmergencyStopControlIsUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()
	adapter := executableFakeAdapter()
	result := newVerifiedTestRegistry(adapter).Execute(
		context.Background(), "test", approvedRuntimeTask("task-control-unavailable", "run approved work"),
	)
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("runtime crossed dispatch boundary without readable stop control: result=%#v called=%t", result, adapter.called)
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

func TestRegistryCanonicalizesAuthorizedScopeBeforeAdapterExecution(t *testing.T) {
	base := executableFakeAdapter()
	adapter := &taskCapturingAdapter{fakeAdapter: base}
	task := Task{
		ID:               " task-1 ",
		Prompt:           "do work",
		OwnerIdentity:    " alice ",
		ProjectKey:       " project-1 ",
		HumanApproved:    true,
		ApprovalSourceID: " task-review:11111111-1111-4111-8111-111111111111 ",
	}
	task = withValidFinalEffectProof("test", task, adapter.info)
	var verified FinalEffectAuthorizationRequest
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			verified = request
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)

	result := registry.Execute(context.Background(), "test", task)
	if result.Status != "completed" || !adapter.called {
		t.Fatalf("canonical scope task was not executed: result=%#v", result)
	}
	if adapter.received.ID != "task-1" || adapter.received.OwnerIdentity != "alice" ||
		adapter.received.ProjectKey != "project-1" ||
		adapter.received.ApprovalSourceID != "task-review:11111111-1111-4111-8111-111111111111" {
		t.Fatalf("adapter received scope values different from the authorized request: task=%#v", adapter.received)
	}
	if verified.TaskID != adapter.received.ID || verified.OwnerIdentity != adapter.received.OwnerIdentity ||
		verified.ProjectKey != adapter.received.ProjectKey || verified.ApprovalSourceID != adapter.received.ApprovalSourceID {
		t.Fatalf("final-effect request and adapter task differ: request=%#v task=%#v", verified, adapter.received)
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
	if stop.Status != "cancellation_requested" || !strings.Contains(stop.Message, "delivery is not yet verified") {
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

func TestRegistryHealthForChecksOnlyTheRequestedRuntime(t *testing.T) {
	openClaw := &fakeAdapter{info: Info{ID: "openclaw"}}
	hermes := &fakeAdapter{info: Info{ID: "hermes"}}
	registry := NewRegistry(openClaw, hermes)

	health, ok := registry.HealthFor(context.Background(), "openclaw")
	if !ok || health.RuntimeID != "openclaw" {
		t.Fatalf("HealthFor(openclaw) = (%#v, %t)", health, ok)
	}
	if openClaw.healthCalls != 1 || hermes.healthCalls != 0 {
		t.Fatalf("HealthFor must not probe unrelated runtimes: openclaw=%d hermes=%d", openClaw.healthCalls, hermes.healthCalls)
	}
}

func TestBindConsumedAuthorizationProofBindsExactTaskAndRejectsMutation(t *testing.T) {
	adapter := executableFakeAdapter()
	binder := NewRegistry(adapter)
	task := approvedRuntimeTask("task-1", "do work")
	task.ID = " task-1 "
	task.OwnerIdentity = " alice "
	task.ProjectKey = " project-1 "
	task.ApprovalSourceID = " task-review:11111111-1111-4111-8111-111111111111 "
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
	if bound.ID != "task-1" || bound.OwnerIdentity != "alice" || bound.ProjectKey != "project-1" ||
		bound.ApprovalSourceID != "task-review:11111111-1111-4111-8111-111111111111" {
		t.Fatalf("bound task retained non-canonical authorization scope: %#v", bound)
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
	if stop.Status != "cancellation_requested" || !strings.Contains(stop.Message, "delivery is not yet verified") {
		t.Fatalf("stop result = %#v", stop)
	}
	if !containsString(stop.AuditEvents, "runtime cancellation delivery is not yet verified") {
		t.Fatalf("stop request must not claim a verified downstream stop: %#v", stop.AuditEvents)
	}

	select {
	case result := <-resultCh:
		if result.Status != "indeterminate" || !strings.Contains(result.Message, "cancellation interrupted") {
			t.Fatalf("runtime result after stop = %#v", result)
		}
		if !containsString(result.AuditEvents, "runtime cancellation observed after adapter start; downstream outcome is unverified") {
			t.Fatalf("runtime cancellation audit missing: %#v", result.AuditEvents)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime task was not cancelled")
	}
}

func TestRegistryEmergencyStopCancelsActiveRuntimeExecution(t *testing.T) {
	var engaged atomic.Bool
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		if engaged.Load() {
			return true, "operator emergency stop", nil
		}
		return false, "", nil
	}))
	defer restore()

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
		resultCh <- registry.Execute(context.Background(), "test", approvedRuntimeTask("task-emergency-stop", "do work"))
	}()

	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime task did not start")
	}
	engaged.Store(true)

	select {
	case result := <-resultCh:
		if result.Status != "indeterminate" || !strings.Contains(result.Message, "emergency stop interrupted") {
			t.Fatalf("runtime result after emergency stop = %#v", result)
		}
		if !containsString(result.AuditEvents, "emergency stop cancellation observed after runtime adapter start") {
			t.Fatalf("emergency-stop cancellation audit missing: %#v", result.AuditEvents)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("emergency stop did not cancel the active runtime task")
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

func TestOdysseusAdapterBlocksExecutionUntilToolCallsAreMediated(t *testing.T) {
	var contacted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := &odysseusAdapter{
		enabled:      true,
		baseURL:      server.URL,
		token:        "test-token",
		sessionID:    "session-1",
		timeout:      30 * time.Second,
		outputLimit:  defaultOutputLimit,
		allowedHost:  map[string]bool{"127.0.0.1": true},
		emailEnabled: true,
		mcpEnabled:   true,
	}
	result := adapter.ExecuteTask(context.Background(), Task{Prompt: "inspect the task", HumanApproved: true})
	if result.Status != "blocked" || result.Message != odysseusToolMediationBlockReason {
		t.Fatalf("result = %#v", result)
	}
	if contacted {
		t.Fatal("Odysseus runner must not be contacted for an unmediated agent task")
	}
	info := adapter.Info()
	if !info.Configured || info.ExecutionEnabled {
		t.Fatalf("configured but unmediated Odysseus must remain non-executable: %#v", info)
	}
}

func TestOdysseusAdapterDoesNotContactRunnerWhenHighRiskToolsAreConfigured(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := &odysseusAdapter{
		enabled:         true,
		baseURL:         server.URL,
		token:           "test-token",
		sessionID:       "session-1",
		timeout:         time.Second,
		outputLimit:     defaultOutputLimit,
		allowedHost:     map[string]bool{"127.0.0.1": true},
		allowBash:       true,
		shellEnabled:    true,
		emailEnabled:    true,
		calendarEnabled: true,
		mcpEnabled:      true,
	}

	result := adapter.ExecuteTask(context.Background(), Task{Prompt: "inspect the task"})
	if result.Status != "blocked" || calls != 0 {
		t.Fatalf("result=%#v runner_calls=%d; execution must remain blocked without per-tool mediation", result, calls)
	}
	if !containsString(adapter.controls(), odysseusToolMediationBlockReason) {
		t.Fatalf("runtime controls do not disclose the execution block: %#v", adapter.controls())
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
	if !info.Configured || info.ExecutionEnabled {
		t.Fatalf("expected configured but non-executable Odysseus adapter: %#v", info)
	}
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"scoped Codex API", "MCP manager", "Cookbook model-serving", "Codex and Claude bridge", "context budget"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("Odysseus capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side HAI approval", "scoped ODYSSEUS_API_TOKEN", "AGENT_RUNTIME_ALLOWED_HOSTS", odysseusToolMediationBlockReason} {
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
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	adapter := &hermesAdapter{
		enabled:          true,
		executable:       "hermes",
		workspace:        workspace,
		workspaceRoot:    root,
		toolsets:         []string{"safe", "skills"},
		skills:           []string{"legal-drafting"},
		terminalBackends: []string{"local", "docker"},
		gatewayEnabled:   true,
		mcpEnabled:       true,
	}
	info := adapter.Info()
	if !info.Configured {
		t.Fatalf("valid Hermes configuration should remain visible: %#v", info)
	}
	if info.ExecutionEnabled || info.ReadOnlyDefault {
		t.Fatalf("Hermes must be non-executable and not represented as read-only until tool calls are mediated: %#v", info)
	}
	if !containsExact(info.MissingConfiguration, hermesToolPolicyMediationBlockReason) {
		t.Fatalf("runtime info must expose the per-tool mediation blocker: %#v", info.MissingConfiguration)
	}
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"skills and skill learning", "MCP servers and tools", "gateway channels", "subagent delegation", "ACP adapter"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("Hermes capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side task approval", "AGENT_RUNTIME_WORKSPACE_ROOT", "HERMES_TOOLSETS=safe,skills", "HERMES_SKILLS=legal-drafting", hermesToolPolicyMediationBlockReason} {
		if !strings.Contains(joinedControls, expected) {
			t.Fatalf("Hermes control %q not advertised: %#v", expected, info.Controls)
		}
	}
	for _, skill := range adapter.ListSkills(context.Background()) {
		if skill.ExecutionMode != "inventory_only_blocked" || skill.RiskLevel != "unknown" || !strings.Contains(skill.Description, "Inventory only") {
			t.Fatalf("unreviewed Hermes configuration must remain inventory-only with unknown risk: %#v", skill)
		}
	}
	if len(info.Architecture) == 0 {
		t.Fatalf("expected Hermes architecture chain to be visible")
	}
}

func TestHermesAdapterBlocksCliBeforeProcessCreationWithoutToolMediation(t *testing.T) {
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
	if result.Status != "blocked" || !strings.Contains(result.Message, hermesToolPolicyMediationBlockReason) {
		t.Fatalf("result = %#v", result)
	}
	if result.Output != "" || result.ExitCode != -1 {
		t.Fatalf("blocked Hermes execution must not launch the test executable or produce output: %#v", result)
	}
	if !containsExact(result.AuditEvents, "Hermes CLI launch blocked before process creation because configured tool calls are not individually mediated") {
		t.Fatalf("result must record the pre-launch policy gate: %#v", result.AuditEvents)
	}
}

func TestHermesRegistryDoesNotTreatTaskApprovalAsToolAuthorization(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	adapter := &hermesAdapter{
		enabled:       true,
		executable:    "hermes",
		workspace:     workspace,
		workspaceRoot: root,
	}
	var proofCalls atomic.Int32
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			proofCalls.Add(1)
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)
	task := approvedRuntimeTask("task-1", "send an external message")
	task = withValidFinalEffectProof("hermes", task, adapter.Info())
	result := registry.Execute(context.Background(), "hermes", task)
	if result.Status != "blocked" || !strings.Contains(result.Message, hermesToolPolicyMediationBlockReason) {
		t.Fatalf("task-level approval must not enable Hermes tool calls: %#v", result)
	}
	if proofCalls.Load() != 0 {
		t.Fatalf("unavailable per-tool mediation must block before final proof exercise and adapter invocation; proof calls=%d", proofCalls.Load())
	}
}

func TestHermesHealthReportsExecutionPolicyBlock(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	adapter := &hermesAdapter{
		enabled:       true,
		executable:    executable,
		workspace:     workspace,
		workspaceRoot: root,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "blocked" || !strings.Contains(health.Reason, hermesToolPolicyMediationBlockReason) {
		t.Fatalf("available Hermes binary must not be reported executable without tool mediation: %#v", health)
	}
}

func TestOpenClawInfoAdvertisesEcosystemAndControls(t *testing.T) {
	adapter := &openClawAdapter{
		enabled:                  true,
		gatewayEnabled:           true,
		gatewayDelegationEnabled: true,
		gatewayURL:               "ws://127.0.0.1:18789",
		gatewayToken:             "gateway-read-token",
		gatewayDelegationToken:   "gateway-write-token",
		gatewayReceiptStore:      &fakeOpenClawGatewayReceiptStore{},
		thinking:                 "high",
		timeout:                  30 * time.Second,
		outputLimit:              defaultOutputLimit,
		allowedHost:              map[string]bool{"127.0.0.1": true},
		agentCLIEnabled:          true,
		messagesEnabled:          true,
		skillsEnabled:            true,
		pluginsEnabled:           true,
		mcpEnabled:               true,
		memoryEnabled:            true,
		cronEnabled:              true,
		browserEnabled:           true,
		canvasEnabled:            true,
		nodesEnabled:             true,
		voiceEnabled:             true,
		talkEnabled:              true,
		webchatEnabled:           true,
		multiAgentEnabled:        true,
		localModelsEnabled:       true,
		highRiskExecution:        true,
		sandboxRequired:          true,
		sandboxMode:              "all",
		sandboxDocker:            true,
		channelsEnabled:          []string{"whatsapp", "telegram"},
		providersEnabled:         []string{"ollama", "openrouter"},
		companionApps:            []string{"windows", "android"},
	}
	info := adapter.Info()
	if info.Configured || info.ExecutionEnabled || !containsExact(info.MissingConfiguration, gatewayPolicyAttestationBlockReason) {
		t.Fatalf("unattested OpenClaw Gateway must remain unavailable for execution: %#v", info)
	}
	joinedCapabilities := strings.Join(info.Capabilities, " ")
	for _, expected := range []string{"Gateway inspection and owner-bound recovery", "CLI task execution is blocked", "multi-channel inbox", "multi-agent session routing", "skills, ClawHub packages", "Live Canvas", "sandbox backends"} {
		if !strings.Contains(joinedCapabilities, expected) {
			t.Fatalf("OpenClaw capability %q not advertised: %#v", expected, info.Capabilities)
		}
	}
	joinedControls := strings.Join(info.Controls, " ")
	for _, expected := range []string{"server-side HAI approval", "direct OpenClaw CLI task execution is blocked", "run-bound", "OPENCLAW_GATEWAY_TOKEN"} {
		if !strings.Contains(joinedControls, expected) {
			t.Fatalf("OpenClaw control %q not advertised: %#v", expected, info.Controls)
		}
	}
	if len(info.Architecture) == 0 {
		t.Fatalf("expected OpenClaw architecture chain to be visible")
	}
}

func TestRegistryReportsGatewayReferenceRouteOnlyWhenDelegationIsReady(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	cli := &openClawAdapter{
		enabled: true, executable: "openclaw", workspace: workspace, workspaceRoot: root,
		agentCLIEnabled: true, sandboxRequired: false, outputLimit: defaultOutputLimit,
	}
	if registry := NewRegistry(cli); registry.OpenClawGatewayDelegationReady() {
		t.Fatal("CLI-selected OpenClaw route reported a Gateway-only reference capability")
	}

	delegated := &openClawAdapter{
		enabled: true, gatewayEnabled: true, gatewayDelegationEnabled: true,
		gatewayURL: "ws://127.0.0.1:18789", gatewayToken: "read-token",
		gatewayDelegationToken: "write-token", gatewayReceiptStore: &fakeOpenClawGatewayReceiptStore{},
		allowedHost: map[string]bool{"127.0.0.1": true}, sandboxRequired: true,
	}
	if registry := NewRegistry(delegated); registry.OpenClawGatewayDelegationReady() {
		t.Fatalf("unattested Gateway route reported executable: %#v", delegated.Info())
	}

	delegated.gatewayDelegationToken = delegated.gatewayToken
	if registry := NewRegistry(delegated); registry.OpenClawGatewayDelegationReady() {
		t.Fatal("unsafe same-token Gateway configuration reported delegation ready")
	}
}

func TestOpenClawCompanionGatewayHealthDoesNotRequireHostCLI(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet || request.URL.Path != "/health" {
			t.Fatalf("unexpected companion health request: %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	}))
	defer server.Close()

	gatewayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	adapter := &openClawAdapter{
		enabled:        true,
		gatewayEnabled: true,
		gatewayURL:     gatewayURL,
		allowedHost:    map[string]bool{"127.0.0.1": true},
		timeout:        time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "available" {
		t.Fatalf("companion gateway health = %#v, want available", health)
	}
	if !strings.Contains(health.Reason, "Gateway health endpoint is live") {
		t.Fatalf("companion gateway reason = %q", health.Reason)
	}
	if !strings.Contains(health.Reason, "connectivity only") || !strings.Contains(health.Reason, "CLI task execution is blocked") {
		t.Fatalf("health-only Gateway must not imply an executable task route: %#v", health)
	}
	if info := adapter.Info(); info.Configured || info.ExecutionEnabled {
		t.Fatalf("read-only Gateway health must not mark task execution configured: %#v", info)
	}
	if health.GatewayProtocolValidated || health.GatewayAuthenticated || health.GatewayScope != "" {
		t.Fatalf("health-only probe must not claim protocol or authentication proof: %#v", health)
	}
	if !strings.HasPrefix(health.GatewayEndpointSHA256, "sha256:") || strings.Contains(health.GatewayEndpointSHA256, server.URL) {
		t.Fatalf("health-only probe must expose only a non-secret endpoint digest: %#v", health)
	}
	if requests != 1 {
		t.Fatalf("companion health request count = %d, want 1", requests)
	}
}

func TestOpenClawAgentCLIOptInCannotBypassRunBoundPolicyBlock(t *testing.T) {
	for _, test := range []struct {
		name          string
		sandboxNeeded string
		sandboxMode   string
	}{
		{name: "mirror claims sandbox enabled", sandboxNeeded: "true", sandboxMode: "all"},
		{name: "mirror claims sandbox disabled", sandboxNeeded: "false", sandboxMode: "off"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
			t.Setenv("OPENCLAW_AGENT_CLI_ENABLED", "true")
			t.Setenv("OPENCLAW_EXECUTABLE", "must-not-be-invoked")
			t.Setenv("OPENCLAW_WORKSPACE", t.TempDir())
			t.Setenv("AGENT_RUNTIME_WORKSPACE_ROOT", t.TempDir())
			t.Setenv("OPENCLAW_SANDBOX_REQUIRED", test.sandboxNeeded)
			t.Setenv("OPENCLAW_SANDBOX_MODE", test.sandboxMode)
			t.Setenv("OPENCLAW_GATEWAY_ENABLED", "false")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "false")

			adapter := newOpenClawAdapterFromEnv()
			if !adapter.agentCLIEnabled {
				t.Fatal("test must exercise the legacy explicit CLI opt-in")
			}
			info := adapter.Info()
			if info.Configured || info.ExecutionEnabled {
				t.Fatalf("CLI opt-in or sandbox mirrors must not configure execution: %#v", info)
			}
			if !containsString(info.MissingConfiguration, openClawCLIExecutionBlockedReason) {
				t.Fatalf("runtime info must name the run-bound attestation blocker: %#v", info.MissingConfiguration)
			}
			capabilities := strings.Join(info.Capabilities, " ")
			if !strings.Contains(capabilities, "CLI task execution is blocked") {
				t.Fatalf("capabilities must disclose the blocked CLI route: %#v", info.Capabilities)
			}

			health := adapter.HealthCheck(context.Background())
			if health.Status != "blocked" || !strings.Contains(health.Reason, "run-bound") {
				t.Fatalf("health must report the unattested CLI route as blocked: %#v", health)
			}

			result := adapter.ExecuteTask(context.Background(), Task{ID: "task-1", Prompt: "inspect local work"})
			if result.Status != "blocked" || !strings.Contains(result.Message, "run-bound") {
				t.Fatalf("CLI must fail closed without starting a process: %#v", result)
			}
			registryResult := NewRegistry(adapter).Execute(context.Background(), "openclaw", approvedRuntimeTask("task-2", "inspect local work"))
			if registryResult.Status != "blocked" {
				t.Fatalf("registry must reject the unconfigured CLI route: %#v", registryResult)
			}
		})
	}
}

func TestOpenClawCompanionGatewayHealthRejectsUnexpectedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"starting"}`))
	}))
	defer server.Close()

	adapter := &openClawAdapter{
		enabled:        true,
		gatewayEnabled: true,
		gatewayURL:     "ws" + strings.TrimPrefix(server.URL, "http"),
		allowedHost:    map[string]bool{"127.0.0.1": true},
		timeout:        time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "unavailable" || !strings.Contains(health.Reason, "unexpected health response") {
		t.Fatalf("unexpected companion gateway health = %#v", health)
	}
}

func TestOpenClawGatewayTaskLedgerDiscoveryRequiresAuthenticatedDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	}))
	defer server.Close()

	adapter := &openClawAdapter{
		enabled:                           true,
		gatewayEnabled:                    true,
		gatewayTaskLedgerDiscoveryEnabled: true,
		gatewayURL:                        "ws" + strings.TrimPrefix(server.URL, "http"),
		allowedHost:                       map[string]bool{"127.0.0.1": true},
		timeout:                           time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "blocked" || !strings.Contains(health.Reason, "OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED") {
		t.Fatalf("task-ledger discovery without authenticated discovery = %#v", health)
	}
}

func TestOpenClawGatewayTaskLedgerDiscoveryRequiresToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	}))
	defer server.Close()

	adapter := &openClawAdapter{
		enabled:                              true,
		gatewayEnabled:                       true,
		gatewayAuthenticatedDiscoveryEnabled: true,
		gatewayTaskLedgerDiscoveryEnabled:    true,
		gatewayURL:                           "ws" + strings.TrimPrefix(server.URL, "http"),
		allowedHost:                          map[string]bool{"127.0.0.1": true},
		timeout:                              time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "blocked" || !strings.Contains(health.Reason, "OPENCLAW_GATEWAY_TOKEN") {
		t.Fatalf("task-ledger discovery without token = %#v", health)
	}
}

func TestOpenClawCompanionGatewayHealthRejectsCredentialBearingURL(t *testing.T) {
	adapter := &openClawAdapter{
		enabled:        true,
		gatewayEnabled: true,
		gatewayURL:     "ws://gateway-token@127.0.0.1:18789",
		allowedHost:    map[string]bool{"127.0.0.1": true},
		timeout:        time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "blocked" || !strings.Contains(health.Reason, "must not include credentials") {
		t.Fatalf("credential-bearing companion gateway URL health = %#v", health)
	}
}

func TestOpenClawCompanionGatewayProtocolChallengeIsReadOnly(t *testing.T) {
	var receivedFrame string
	var authorizationHeader string
	gateway := websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			authorizationHeader = request.Header.Get("Authorization")
			return nil
		},
		Handler: func(connection *websocket.Conn) {
			if err := websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			if err := websocket.Message.Receive(connection, &receivedFrame); err != nil && !errors.Is(err, io.EOF) {
				t.Errorf("receive unexpected client frame: %v", err)
			}
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := &openClawAdapter{
		enabled:                         true,
		gatewayEnabled:                  true,
		gatewayProtocolDiscoveryEnabled: true,
		gatewayURL:                      "ws" + strings.TrimPrefix(server.URL, "http"),
		gatewayToken:                    "must-not-be-sent",
		allowedHost:                     map[string]bool{"127.0.0.1": true},
		timeout:                         time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "available" || !strings.Contains(health.Reason, "protocol challenge was verified") {
		t.Fatalf("companion gateway protocol health = %#v", health)
	}
	if !health.GatewayProtocolValidated || health.GatewayAuthenticated || health.GatewayScope != "" || health.GatewayEvidenceSchema != "openclaw-gateway-protocol-v4" {
		t.Fatalf("protocol challenge evidence = %#v", health)
	}
	if receivedFrame != "" {
		t.Fatalf("challenge probe sent a client frame: %q", receivedFrame)
	}
	if authorizationHeader != "" {
		t.Fatalf("challenge probe sent an authorization header: %q", authorizationHeader)
	}
}

func TestOpenClawCompanionGatewayProtocolChallengeRejectsMalformedFrame(t *testing.T) {
	gateway := websocket.Server{
		Handshake: func(_ *websocket.Config, _ *http.Request) error { return nil },
		Handler: func(connection *websocket.Conn) {
			_ = websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "unexpected.event",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1)},
			})
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := &openClawAdapter{
		enabled:                         true,
		gatewayEnabled:                  true,
		gatewayProtocolDiscoveryEnabled: true,
		gatewayURL:                      "ws" + strings.TrimPrefix(server.URL, "http"),
		allowedHost:                     map[string]bool{"127.0.0.1": true},
		timeout:                         time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "unavailable" || !strings.Contains(health.Reason, "protocol challenge") {
		t.Fatalf("malformed companion gateway protocol health = %#v", health)
	}
}

func TestOpenClawCompanionGatewayAuthenticatedReadDiscoveryUsesBoundedOperatorHandshake(t *testing.T) {
	var receivedFrame map[string]any
	var authorizationHeader string
	done := make(chan struct{})
	gateway := websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			authorizationHeader = request.Header.Get("Authorization")
			return nil
		},
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			if err := websocket.JSON.Receive(connection, &receivedFrame); err != nil {
				return
			}
			requestID, _ := receivedFrame["id"].(string)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "res",
				"id":   requestID,
				"ok":   true,
				"payload": map[string]any{
					"type":     "hello-ok",
					"protocol": float64(4),
					"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
					"features": map[string]any{"methods": []string{"status"}, "events": []string{}},
					"snapshot": map[string]any{},
					"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
					"policy":   map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
				},
			}); err != nil {
				t.Errorf("send hello response: %v", err)
			}
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	health := newOpenClawAdapterFromEnv().HealthCheck(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish the authenticated handshake")
	}
	if health.Status != "available" || !strings.Contains(health.Reason, "authenticated operator.read") {
		t.Fatalf("authenticated discovery health = %#v", health)
	}
	if health.Version != "2026.8.1" {
		t.Fatalf("authenticated discovery version = %q, want server version", health.Version)
	}
	if authorizationHeader != "" {
		t.Fatalf("authenticated discovery sent an authorization header: %q", authorizationHeader)
	}
	if receivedFrame["type"] != "req" || receivedFrame["method"] != "connect" {
		t.Fatalf("unexpected gateway frame: %#v", receivedFrame)
	}
	params, ok := receivedFrame["params"].(map[string]any)
	if !ok {
		t.Fatalf("connect params missing: %#v", receivedFrame)
	}
	scopes, ok := params["scopes"].([]any)
	if !ok {
		t.Fatalf("connect scopes missing: %#v", params)
	}
	hasRead := false
	hasWrite := false
	for _, scope := range scopes {
		value, _ := scope.(string)
		hasRead = hasRead || value == "operator.read"
		hasWrite = hasWrite || value == "operator.write"
	}
	if params["role"] != "operator" || len(scopes) != 1 || !hasRead || hasWrite {
		t.Fatalf("connect scopes were not bounded to operator.read: %#v", params)
	}
	auth, ok := params["auth"].(map[string]any)
	if !ok || auth["token"] != "gateway-read-token" {
		t.Fatalf("connect auth token was not sent through the protocol body: %#v", params)
	}
}

func TestOpenClawGatewayDelegationFailsClosedWithoutRunBoundPolicyAttestation(t *testing.T) {
	testOpenClawGatewayDelegationModel(t, "")
}

func TestOpenClawGatewayDelegationSelectedModelCannotBypassPolicyAttestation(t *testing.T) {
	testOpenClawGatewayDelegationModel(t, "ollama/qwen3:14b")
}

func testOpenClawGatewayDelegationModel(t *testing.T, model string) {
	t.Helper()
	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "gateway-write-token")
	t.Setenv("OPENCLAW_GATEWAY_ALLOWED_MODELS", "ollama/qwen3:14b")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws://127.0.0.1:18789")
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	receipts := &fakeOpenClawGatewayReceiptStore{}
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = receipts
	adapter.maintenanceGate = func(context.Context) (func(), error) { return func() {}, nil }
	task := approvedRuntimeTask("task-1", "Prepare a factual draft without sending it.")
	modelJSON, _ := json.Marshal(map[string]string{"RuntimeModel": model})
	if err := json.Unmarshal(modelJSON, &task); err != nil {
		t.Fatal(err)
	}
	result := adapter.ExecuteTask(context.Background(), task)
	if result.Status != "blocked" || !strings.Contains(result.Message, gatewayPolicyAttestationBlockReason) {
		t.Fatalf("unattested delegated task result = %#v, want explicit policy block", result)
	}
	if result.ExecutionReference != "" || len(receipts.receipts) != 0 {
		t.Fatalf("unattested execution created a reference or durable receipt: result=%#v receipts=%#v", result, receipts.receipts)
	}
	if adapter.OpenClawGatewayDelegationReady() || adapter.Info().ExecutionEnabled {
		t.Fatal("unattested Gateway route was advertised as executable")
	}
	if model != "" && task.RuntimeModel != model {
		t.Fatalf("test did not retain selected model: got %q want %q", task.RuntimeModel, model)
	}
}

type fakeOpenClawGatewayReceiptStore struct {
	receipts    []OpenClawGatewayReceipt
	beforeAdmit func(OpenClawGatewayReceipt)
}

func (s *fakeOpenClawGatewayReceiptStore) CreateOpenClawGatewayReceipt(_ context.Context, receipt OpenClawGatewayReceipt) error {
	if receipt.Status != "admitting" || receipt.SessionKey != "" || receipt.RunID != "" {
		return fmt.Errorf("receipt must be persisted as a pre-admission intent")
	}
	s.receipts = append(s.receipts, receipt)
	return nil
}

func (s *fakeOpenClawGatewayReceiptStore) MarkOpenClawGatewayReceiptAdmitted(_ context.Context, receipt OpenClawGatewayReceipt) (bool, error) {
	for i := range s.receipts {
		if s.receipts[i].ExecutionReference == receipt.ExecutionReference && s.receipts[i].OwnerIdentity == receipt.OwnerIdentity && s.receipts[i].RuntimeTaskID == receipt.RuntimeTaskID && s.receipts[i].Status == "admitting" {
			if s.beforeAdmit != nil {
				s.beforeAdmit(s.receipts[i])
			}
			previous := s.receipts[i]
			receipt.CancellationIntentID = previous.CancellationIntentID
			receipt.CancellationStatus = previous.CancellationStatus
			receipt.CancellationMessage = previous.CancellationMessage
			receipt.CancellationAttempts = previous.CancellationAttempts
			receipt.CancellationAt = previous.CancellationAt
			receipt.CancellationTriedAt = previous.CancellationTriedAt
			s.receipts[i] = receipt
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeOpenClawGatewayReceiptStore) MarkOpenClawGatewayReceiptNotAdmitted(_ context.Context, reference, owner, taskID string) (bool, error) {
	for i := range s.receipts {
		if s.receipts[i].ExecutionReference == reference && s.receipts[i].OwnerIdentity == owner && s.receipts[i].RuntimeTaskID == taskID && s.receipts[i].Status == "admitting" {
			s.receipts[i].Status = "not_admitted"
			if s.receipts[i].CancellationIntentID != "" {
				s.receipts[i].CancellationStatus = "not_required"
				s.receipts[i].CancellationMessage = "Gateway explicitly reported no admitted run; no cancellation was required"
			}
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeOpenClawGatewayReceiptStore) EnsureOpenClawGatewayCancellationIntent(_ context.Context, requested OpenClawGatewayReceipt, now time.Time) (OpenClawGatewayReceipt, error) {
	for i := range s.receipts {
		stored := &s.receipts[i]
		if stored.ExecutionReference != requested.ExecutionReference || stored.OwnerIdentity != requested.OwnerIdentity || stored.RuntimeTaskID != requested.RuntimeTaskID {
			continue
		}
		if (requested.SessionKey != "" && requested.SessionKey != stored.SessionKey) || (requested.RunID != "" && requested.RunID != stored.RunID) {
			return OpenClawGatewayReceipt{}, fmt.Errorf("receipt binding changed")
		}
		if stored.Status == "terminal" || stored.Status == "not_admitted" {
			return *stored, nil
		}
		if stored.CancellationIntentID == "" {
			stored.CancellationIntentID = "ocancel:v1:" + strings.TrimPrefix(stored.ExecutionReference, "ocgw:v2:")
			stored.CancellationAt = now.UTC()
		}
		if stored.CancellationStatus != "acknowledged" {
			if !ValidOpenClawGatewayRunID(stored.RunID) || !validOpenClawGatewaySessionKey(stored.SessionKey) {
				stored.CancellationStatus = "awaiting_identity"
			} else if stored.CancellationStatus == "" || stored.CancellationStatus == "awaiting_identity" || stored.CancellationStatus == "not_required" {
				stored.CancellationStatus = "requested"
			}
		}
		return *stored, nil
	}
	return OpenClawGatewayReceipt{}, fmt.Errorf("receipt not found")
}

func (s *fakeOpenClawGatewayReceiptStore) RecordOpenClawGatewayCancellationOutcome(_ context.Context, expected OpenClawGatewayReceipt, outcome, message string, attemptedAt time.Time) error {
	for i := range s.receipts {
		stored := &s.receipts[i]
		if stored.ExecutionReference != expected.ExecutionReference || stored.OwnerIdentity != expected.OwnerIdentity || stored.RuntimeTaskID != expected.RuntimeTaskID || stored.CancellationIntentID == "" {
			continue
		}
		if expected.SessionKey != stored.SessionKey || expected.RunID != stored.RunID {
			return fmt.Errorf("receipt run binding changed")
		}
		if stored.CancellationStatus != "acknowledged" {
			stored.CancellationStatus = outcome
			stored.CancellationMessage = message
		}
		if outcome == "acknowledged" || outcome == "delivery_failed" {
			stored.CancellationAttempts++
			stored.CancellationTriedAt = attemptedAt.UTC()
		}
		return nil
	}
	return fmt.Errorf("cancellation intent not found")
}

func (s *fakeOpenClawGatewayReceiptStore) ListOpenClawGatewayCancellationReceipts(int, time.Time, string) ([]OpenClawGatewayReceipt, error) {
	var pending []OpenClawGatewayReceipt
	for _, receipt := range s.receipts {
		if receipt.CancellationIntentID != "" && receipt.CancellationStatus != "acknowledged" && receipt.CancellationStatus != "not_required" && receipt.Status != "terminal" && receipt.Status != "not_admitted" {
			pending = append(pending, receipt)
		}
	}
	return pending, nil
}

func (s *fakeOpenClawGatewayReceiptStore) FindOpenClawGatewayReceipt(reference string) (OpenClawGatewayReceipt, error) {
	for _, receipt := range s.receipts {
		if receipt.ExecutionReference == reference {
			return receipt, nil
		}
	}
	return OpenClawGatewayReceipt{}, fmt.Errorf("receipt not found")
}

func TestOpenClawGatewayDelegatedSessionStopUsesOpaqueReference(t *testing.T) {
	var frames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "event", "event": "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			for {
				var frame map[string]any
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					return
				}
				frames = append(frames, frame)
				requestID, _ := frame["id"].(string)
				switch frame["method"] {
				case "connect":
					_ = websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"type": "hello-ok", "protocol": float64(4),
							"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
							"features": map[string]any{"methods": []string{"sessions.abort"}}, "snapshot": map[string]any{},
							"auth":   map[string]any{"role": "operator", "scopes": []string{"operator.write"}},
							"policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
						},
					})
				case "sessions.abort":
					_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": requestID, "ok": true, "payload": map[string]any{"ok": true}})
					return
				default:
					t.Errorf("unexpected gateway method %q", frame["method"])
					return
				}
			}
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "gateway-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	adapter := newOpenClawAdapterFromEnv()
	reference := openClawGatewayReceiptReference()
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "hai-owned-run", CreatedAt: time.Now().UTC(),
	}}}
	result := adapter.StopTaskWithReference(context.Background(), "task-1", "alice", reference)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish delegated-session cancellation")
	}
	if result.Status != "cancellation_requested" || strings.Contains(result.Message, "agent:main:hai-task") {
		t.Fatalf("delegated stop result = %#v", result)
	}
	if len(frames) != 2 || frames[1]["method"] != "sessions.abort" {
		t.Fatalf("unexpected gateway frames: %#v", frames)
	}
	params, ok := frames[1]["params"].(map[string]any)
	if !ok || params["key"] != "agent:main:hai-task" || params["runId"] != "hai-owned-run" || len(params) != 2 {
		t.Fatalf("abort must target the persisted run without clearing another run or queue: %#v", frames[1])
	}
}

func TestOpenClawGatewayDelegatedSessionReconciliationUsesRunReceiptWithoutImportingReply(t *testing.T) {
	var frames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			_ = websocket.JSON.Send(connection, map[string]any{
				"type": "event", "event": "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			})
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			for {
				var frame map[string]any
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					return
				}
				frames = append(frames, frame)
				requestID, _ := frame["id"].(string)
				switch frame["method"] {
				case "connect":
					_ = websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"type": "hello-ok", "protocol": float64(4),
							"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
							"features": map[string]any{"methods": []string{"agent.wait"}}, "snapshot": map[string]any{},
							"auth":   map[string]any{"role": "operator", "scopes": []string{"operator.write"}},
							"policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
						},
					})
				case "agent.wait":
					_ = websocket.JSON.Send(connection, map[string]any{
						"type": "event", "event": "agent", "seq": 1,
						"payload": map[string]any{"runId": "unrelated-run", "status": "completed", "text": "sensitive unrelated reply"},
					})
					_ = websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{"runId": "private-run-id", "status": "ok", "endedAt": float64(1_788_521_600_000), "terminalReply": "sensitive draft reply"},
					})
					return
				default:
					t.Errorf("unexpected gateway method %q", frame["method"])
					return
				}
			}
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "gateway-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")
	reference := "ocgw:v2:" + uuid.NewString()
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "private-run-id", CreatedAt: time.Now().UTC(),
	}}}

	result := adapter.ReconcileDelegatedSession(context.Background(), "task-1", "alice", reference)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish delegated-session reconciliation")
	}
	if result.Status != "completed" || strings.Contains(result.Message, "sensitive") || strings.Contains(strings.Join(result.AuditEvents, "\n"), "sensitive") {
		t.Fatalf("delegated reconciliation result = %#v", result)
	}
	if len(frames) != 2 || frames[1]["method"] != "agent.wait" {
		t.Fatalf("unexpected gateway frames: %#v", frames)
	}
	params, ok := frames[1]["params"].(map[string]any)
	if !ok || params["runId"] != "private-run-id" || params["timeoutMs"] != float64(0) || len(params) != 2 {
		t.Fatalf("agent.wait must use only the private persisted run receipt: %#v", frames[1])
	}
}

func TestOpenClawGatewayReconciliationReconnectsOnlyBeforeTerminalObservation(t *testing.T) {
	connections := 0
	waitRequests := 0
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer connection.Close()
		connections++
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			t.Errorf("send protocol challenge: %v", err)
			return
		}
		var frame map[string]any
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		requestID, _ := frame["id"].(string)
		if connections == 1 {
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "res", "id": requestID, "ok": false,
				"error": map[string]any{"code": "UNAVAILABLE", "retryable": true, "retryAfterMs": float64(1)},
			}); err != nil {
				t.Errorf("send unavailable handshake: %v", err)
			}
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": requestID, "ok": true, "payload": map[string]any{
				"type": "hello-ok", "protocol": float64(4),
				"server":   map[string]any{"version": "2026.8.1", "connId": "connection-2"},
				"features": map[string]any{"methods": []string{"agent.wait"}}, "snapshot": map[string]any{},
				"auth":   map[string]any{"role": "operator", "scopes": []string{"operator.write"}},
				"policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
			},
		}); err != nil {
			t.Errorf("send retry handshake: %v", err)
			return
		}
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Errorf("receive agent.wait: %v", err)
			return
		}
		if frame["method"] != "agent.wait" {
			t.Errorf("unexpected gateway method %q", frame["method"])
			return
		}
		waitRequests++
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": frame["id"], "ok": true,
			"payload": map[string]any{"runId": "private-run-id", "status": "ok", "endedAt": float64(1_788_521_600_000)},
		}); err != nil {
			t.Errorf("send terminal observation: %v", err)
			return
		}
		close(done)
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "gateway-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")
	reference := "ocgw:v2:" + uuid.NewString()
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "private-run-id", CreatedAt: time.Now().UTC(),
	}}}

	result := adapter.ReconcileDelegatedSession(context.Background(), "task-1", "alice", reference)
	if result.Status != "completed" {
		t.Fatalf("reconciliation result = %#v, want a verified terminal receipt", result)
	}
	if connections != 2 || waitRequests != 1 {
		t.Fatalf("connections=%d waitRequests=%d, want one fresh handshake retry and one terminal observation", connections, waitRequests)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete retried reconciliation")
	}
}

func TestOpenClawGatewayArtifactDescriptorsKeepOnlyBoundedMetadata(t *testing.T) {
	payload := json.RawMessage(`{"artifacts":[{"id":"artifact-secret-123","title":"Private lawyer correspondence.pdf","type":"document","mimeType":"application/pdf","sizeBytes":4096,"runId":"run-private","download":{"mode":"url","url":"https://private.example/artifact-secret-123"}}]}`)
	descriptors, err := openClawGatewayArtifactDescriptorsFromResponse(payload, "run-private")
	if err != nil {
		t.Fatalf("openClawGatewayArtifactDescriptorsFromResponse() error = %v", err)
	}
	if len(descriptors) != 1 {
		t.Fatalf("descriptor count = %d, want 1", len(descriptors))
	}
	descriptor := descriptors[0]
	if descriptor.Type != "document" || descriptor.MIMEType != "application/pdf" || descriptor.SizeBytes == nil || *descriptor.SizeBytes != 4096 {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if descriptor.Digest == "" || strings.Contains(descriptor.Digest, "artifact-secret") || strings.Contains(fmt.Sprintf("%#v", descriptor), "Private lawyer") || strings.Contains(fmt.Sprintf("%#v", descriptor), "private.example") {
		t.Fatalf("artifact descriptor retained private artifact data: %#v", descriptor)
	}
}

func TestOpenClawGatewayReadOnlyRequestClassifiesUnavailableWithoutLeakingGatewayDetails(t *testing.T) {
	const privateGatewayMessage = "gateway provider token is private-token-value"
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive request: %v", err)
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": false,
			"error": map[string]any{
				"code": "UNAVAILABLE", "message": privateGatewayMessage,
				"details":   map[string]any{"provider": "private-provider", "token": "private-token-value"},
				"retryable": true, "retryAfterMs": float64(1250),
			},
		}); err != nil {
			t.Errorf("send gateway error: %v", err)
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer connection.Close()

	_, err = openClawGatewayReadOnlyRequest(connection, "artifacts.list", map[string]any{"runId": "private-run"})
	if err == nil {
		t.Fatal("openClawGatewayReadOnlyRequest() error = nil, want classified unavailable error")
	}
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error type = %T, want *openClawGatewayResponseError", err)
	}
	if gatewayErr.Category != openClawGatewayErrorUnavailable || !gatewayErr.Retryable || gatewayErr.RetryAfter != 1250*time.Millisecond {
		t.Fatalf("gateway error = %#v", gatewayErr)
	}
	if strings.Contains(err.Error(), privateGatewayMessage) || strings.Contains(err.Error(), "private-token-value") || strings.Contains(err.Error(), "private-provider") {
		t.Fatalf("gateway error leaked provider detail: %q", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete unavailable response")
	}
}

func TestOpenClawGatewayReadHandshakeClassifiesRejectedFrameWithoutLeakingGatewayDetails(t *testing.T) {
	const privateGatewayMessage = "gateway access denied for token private-token-value"
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": false,
			"error": map[string]any{
				"code": "FORBIDDEN", "message": privateGatewayMessage,
				"details": map[string]any{"missingScope": "operator.read", "token": "private-token-value"},
			},
		}); err != nil {
			t.Errorf("send handshake failure: %v", err)
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer connection.Close()

	adapter := &openClawAdapter{gatewayToken: "read-token"}
	_, err = adapter.openClawGatewayOperatorReadHandshake(connection)
	if err == nil {
		t.Fatal("openClawGatewayOperatorReadHandshake() error = nil, want classified gateway rejection")
	}
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error type = %T, want *openClawGatewayResponseError", err)
	}
	if gatewayErr.Category != openClawGatewayErrorAccessDenied || gatewayErr.Retryable {
		t.Fatalf("gateway error = %#v", gatewayErr)
	}
	if strings.Contains(err.Error(), privateGatewayMessage) || strings.Contains(err.Error(), "private-token-value") || strings.Contains(err.Error(), "operator.read") {
		t.Fatalf("handshake error leaked gateway detail: %q", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete handshake rejection")
	}
}

func TestOpenClawGatewayRequestClassifiesDeniedMutationWithoutRetrySignal(t *testing.T) {
	const privateGatewayMessage = "missing operator.write for project /private/customer"
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive request: %v", err)
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": false,
			"error": map[string]any{
				"code": "FORBIDDEN", "message": privateGatewayMessage,
				"details":   map[string]any{"code": "MISSING_SCOPE", "missingScope": "operator.write"},
				"retryable": true, "retryAfterMs": float64(500),
			},
		}); err != nil {
			t.Errorf("send gateway error: %v", err)
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer connection.Close()

	_, err = openClawGatewayRequest(connection, "sessions.create", map[string]any{"idempotencyKey": "bounded-key"})
	if err == nil {
		t.Fatal("openClawGatewayRequest() error = nil, want classified denied error")
	}
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error type = %T, want *openClawGatewayResponseError", err)
	}
	if gatewayErr.Category != openClawGatewayErrorAccessDenied || gatewayErr.Retryable || gatewayErr.RetryAfter != 500*time.Millisecond {
		t.Fatalf("gateway error = %#v", gatewayErr)
	}
	if strings.Contains(err.Error(), privateGatewayMessage) || strings.Contains(err.Error(), "MISSING_SCOPE") || strings.Contains(err.Error(), "operator.write") {
		t.Fatalf("gateway error leaked provider detail: %q", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete denied response")
	}
}

func TestOpenClawGatewayWriteHandshakeRejectsIncompleteHello(t *testing.T) {
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": true,
			"payload": map[string]any{
				"type": "hello-ok", "protocol": float64(4),
				"features": map[string]any{"methods": []string{"sessions.create"}},
				"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.write"}},
			},
		}); err != nil {
			t.Errorf("send incomplete handshake: %v", err)
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer connection.Close()

	adapter := &openClawAdapter{gatewayDelegationToken: "write-token"}
	if _, err := adapter.openClawGatewayOperatorWriteHandshake(connection); err == nil {
		t.Fatal("openClawGatewayOperatorWriteHandshake() error = nil, want incomplete hello rejection")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete incomplete handshake")
	}
}

func TestOpenClawGatewayWriteHandshakeDoesNotSendAfterCancellation(t *testing.T) {
	received := make(chan bool, 1)
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer connection.Close()
		var request map[string]any
		err := websocket.JSON.Receive(connection, &request)
		received <- err == nil
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := &openClawAdapter{gatewayDelegationToken: "write-token"}
	if _, err := adapter.openClawGatewayOperatorWriteHandshakeContext(ctx, connection); !errors.Is(err, context.Canceled) {
		connection.Close()
		t.Fatalf("handshake error = %v, want context.Canceled", err)
	}
	connection.Close()
	select {
	case gotFrame := <-received:
		if gotFrame {
			t.Fatal("cancelled request sent an operator.write connect frame")
		}
	case <-time.After(time.Second):
		t.Fatal("Gateway did not observe the cancelled connection closing")
	}
}

func TestOpenClawGatewayDelegationRejectsReadTokenReuse(t *testing.T) {
	adapter := &openClawAdapter{
		enabled:                  true,
		gatewayEnabled:           true,
		gatewayDelegationEnabled: true,
		gatewayURL:               "ws://127.0.0.1:18789",
		gatewayToken:             "shared-token",
		gatewayDelegationToken:   "shared-token",
		gatewayReceiptStore:      &fakeOpenClawGatewayReceiptStore{},
		allowedHost:              map[string]bool{"127.0.0.1": true},
	}
	if reason := adapter.gatewayDelegationBlockedReason(); !strings.Contains(reason, "must differ") {
		t.Fatalf("gatewayDelegationBlockedReason() = %q, want read/write token reuse rejection", reason)
	}
	info := adapter.Info()
	if info.Configured || info.ExecutionEnabled || !containsString(info.MissingConfiguration, "OPENCLAW_GATEWAY_DELEGATION_TOKEN must differ from OPENCLAW_GATEWAY_TOKEN") {
		t.Fatalf("runtime info accepted reused gateway credentials: %#v", info)
	}
	if health := adapter.HealthCheck(context.Background()); health.Status != "blocked" || !strings.Contains(health.Reason, "must differ") {
		t.Fatalf("HealthCheck() = %#v, want fail-closed reused gateway credential rejection", health)
	}
}

func TestOpenClawGatewayReadOnlyRequestRetriesOneBoundedUnavailableResponse(t *testing.T) {
	requests := 0
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		for {
			var request map[string]any
			if err := websocket.JSON.Receive(connection, &request); err != nil {
				return
			}
			requests++
			if requests == 1 {
				if err := websocket.JSON.Send(connection, map[string]any{
					"type": "res", "id": request["id"], "ok": false,
					"error": map[string]any{"code": "UNAVAILABLE", "retryable": true, "retryAfterMs": float64(1)},
				}); err != nil {
					t.Errorf("send unavailable response: %v", err)
				}
				continue
			}
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "res", "id": request["id"], "ok": true, "payload": map[string]any{"artifacts": []any{}},
			}); err != nil {
				t.Errorf("send retry response: %v", err)
			}
			return
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer connection.Close()

	payload, err := openClawGatewayReadOnlyRequestWithRetry(context.Background(), connection, "artifacts.list", map[string]any{"runId": "private-run"})
	if err != nil {
		t.Fatalf("openClawGatewayReadOnlyRequestWithRetry() error = %v", err)
	}
	if !presentGatewayJSON(payload) || requests != 2 {
		t.Fatalf("retry payload = %s requests=%d", payload, requests)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete retry response")
	}
}

func TestOpenClawGatewayMethodAllowlistRejectsCrossScopeCallsBeforeSending(t *testing.T) {
	if _, err := openClawGatewayReadOnlyRequest(nil, "sessions.create", map[string]any{}); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("read-only sessions.create error = %v, want allowlist rejection", err)
	}
	if _, err := openClawGatewayRequest(nil, "tasks.list", map[string]any{}); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("write-scoped tasks.list error = %v, want allowlist rejection", err)
	}
}

func TestOpenClawGatewayArtifactImportUsesReadScopeAndPrivateMetadataOnly(t *testing.T) {
	var frames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		_ = websocket.JSON.Send(connection, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)}})
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		for {
			var frame map[string]any
			if err := websocket.JSON.Receive(connection, &frame); err != nil {
				return
			}
			frames = append(frames, frame)
			requestID, _ := frame["id"].(string)
			switch frame["method"] {
			case "connect":
				_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": requestID, "ok": true, "payload": map[string]any{
					"type": "hello-ok", "protocol": float64(4), "server": map[string]any{"version": "2026.8.1", "connId": "connection-1"},
					"features": map[string]any{"methods": []string{"artifacts.list"}}, "snapshot": map[string]any{},
					"auth": map[string]any{"role": "operator", "scopes": []string{"operator.read"}}, "policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
				}})
			case "artifacts.list":
				_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": requestID, "ok": true, "payload": map[string]any{"artifacts": []map[string]any{{
					"id": "secret-artifact-id", "title": "Private legal bundle", "type": "document", "mimeType": "application/pdf", "sizeBytes": float64(1024), "runId": "private-run-id", "download": map[string]any{"mode": "url", "url": "https://private.example/secret-artifact-id"},
				}}}})
				return
			default:
				t.Errorf("unexpected gateway method %q", frame["method"])
				return
			}
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")
	reference := "ocgw:v2:" + uuid.NewString()
	receipts := &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "private-run-id", CreatedAt: time.Now().UTC(), TerminalStatus: "completed", TerminalAt: time.Now().UTC(),
	}}}
	artifacts := &fakeOpenClawGatewayArtifactStore{}
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = receipts
	adapter.gatewayArtifactStore = artifacts

	result := adapter.ImportDelegatedArtifacts(context.Background(), "task-1", reference)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete artifact import")
	}
	if result.Status != "imported" || result.Count != 1 || len(artifacts.descriptors) != 1 {
		t.Fatalf("artifact import result = %#v stored=%#v", result, artifacts.descriptors)
	}
	if strings.Contains(fmt.Sprintf("%#v", artifacts.descriptors), "secret-artifact-id") || strings.Contains(fmt.Sprintf("%#v", artifacts.descriptors), "Private legal") || strings.Contains(fmt.Sprintf("%#v", artifacts.descriptors), "private.example") {
		t.Fatalf("artifact store retained private artifact data: %#v", artifacts.descriptors)
	}
	if len(frames) != 2 || frames[1]["method"] != "artifacts.list" {
		t.Fatalf("gateway frames = %#v", frames)
	}
	connect, _ := frames[0]["params"].(map[string]any)
	if scopes, ok := connect["scopes"].([]any); !ok || len(scopes) != 1 || scopes[0] != "operator.read" {
		t.Fatalf("artifact import requested an unsafe gateway scope: %#v", connect)
	}
	if auth, ok := connect["auth"].(map[string]any); !ok || auth["token"] != "gateway-read-token" {
		t.Fatalf("artifact import did not use the read-only token: %#v", connect)
	}
	params, _ := frames[1]["params"].(map[string]any)
	if len(params) != 1 || params["runId"] != "private-run-id" {
		t.Fatalf("artifact import did not use the private run receipt: %#v", params)
	}
}

func TestOpenClawGatewayArtifactImportRejectsReceiptWithoutCompletedTerminalStatus(t *testing.T) {
	reference := "ocgw:v2:" + uuid.NewString()
	adapter := &openClawAdapter{
		gatewayEnabled:               true,
		gatewayArtifactImportEnabled: true,
		gatewayURL:                   "ws://127.0.0.1:1",
		gatewayToken:                 "gateway-read-token",
		gatewayReceiptStore: &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
			ExecutionReference: reference,
			RuntimeTaskID:      "task-1",
			OwnerIdentity:      "alice",
			SessionKey:         "agent:main:hai-task",
			RunID:              "private-run-id",
			CreatedAt:          time.Now().UTC(),
		}}},
		gatewayArtifactStore: &fakeOpenClawGatewayArtifactStore{},
		allowedHost:          map[string]bool{"127.0.0.1": true},
	}

	result := adapter.ImportDelegatedArtifacts(context.Background(), "task-1", reference)
	if result.RuntimeID != "openclaw" || result.Status != "not_terminal" {
		t.Fatalf("ImportDelegatedArtifacts() = %#v, want openclaw not_terminal", result)
	}
	if len(result.AuditEvents) != 1 || !strings.Contains(result.AuditEvents[0], "source-backed completed") {
		t.Fatalf("ImportDelegatedArtifacts() audit = %#v, want terminal verification rejection", result.AuditEvents)
	}
}

func TestOpenClawGatewayVerifiedArtifactImportRejectsMismatchedTerminalReceipt(t *testing.T) {
	adapter := &openClawAdapter{}
	result := adapter.ImportDelegatedArtifactsAfterTerminalVerification(context.Background(), "task-1", "ocgw:v2:"+uuid.NewString(), DelegatedSessionReconcileResult{
		RuntimeID: "openclaw",
		TaskID:    "other-task",
		Status:    "completed",
	})
	if result.RuntimeID != "openclaw" || result.TaskID != "task-1" || result.Status != "not_terminal" {
		t.Fatalf("ImportDelegatedArtifactsAfterTerminalVerification() = %#v, want mismatched completed receipt rejection", result)
	}
}

func TestOpenClawVerifiedArtifactImportRequiresMatchingDurableTerminalReceipt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	reference := "ocgw:v2:" + uuid.NewString()
	tests := []struct {
		name     string
		receipt  OpenClawGatewayReceipt
		terminal DelegatedSessionReconcileResult
	}{
		{
			name: "caller supplied completed state while durable receipt is pending",
			receipt: OpenClawGatewayReceipt{
				ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice",
				SessionKey: "agent:main:task-1", RunID: "private-run", CreatedAt: now,
			},
			terminal: DelegatedSessionReconcileResult{
				RuntimeID: "openclaw", TaskID: "task-1", OwnerIdentity: "alice",
				ExecutionReference: reference, Status: "completed", FinishedAt: now,
			},
		},
		{
			name: "owner does not match durable receipt",
			receipt: OpenClawGatewayReceipt{
				ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice",
				SessionKey: "agent:main:task-1", RunID: "private-run", CreatedAt: now,
				TerminalStatus: "completed", TerminalAt: now,
			},
			terminal: DelegatedSessionReconcileResult{
				RuntimeID: "openclaw", TaskID: "task-1", OwnerIdentity: "mallory",
				ExecutionReference: reference, Status: "completed", FinishedAt: now,
			},
		},
		{
			name: "finish time does not match durable receipt",
			receipt: OpenClawGatewayReceipt{
				ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice",
				SessionKey: "agent:main:task-1", RunID: "private-run", CreatedAt: now,
				TerminalStatus: "completed", TerminalAt: now,
			},
			terminal: DelegatedSessionReconcileResult{
				RuntimeID: "openclaw", TaskID: "task-1", OwnerIdentity: "alice",
				ExecutionReference: reference, Status: "completed", FinishedAt: now.Add(time.Second),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &openClawAdapter{
				gatewayEnabled:               true,
				gatewayArtifactImportEnabled: true,
				gatewayURL:                   "ws://127.0.0.1:1",
				gatewayToken:                 "read-token",
				gatewayReceiptStore:          &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{test.receipt}},
				gatewayArtifactStore:         &fakeOpenClawGatewayArtifactStore{},
				allowedHost:                  map[string]bool{"127.0.0.1": true},
			}
			result := adapter.ImportDelegatedArtifactsAfterTerminalVerification(context.Background(), "task-1", reference, test.terminal)
			if result.Status != "not_terminal" {
				t.Fatalf("artifact import result = %#v, want not_terminal", result)
			}
			if len(adapter.gatewayArtifactStore.(*fakeOpenClawGatewayArtifactStore).descriptors) != 0 {
				t.Fatal("artifact metadata was persisted for an unbound terminal observation")
			}
		})
	}
}

func TestOpenClawGatewayArtifactImportReconnectsOnlyBeforeReadRequest(t *testing.T) {
	connections := 0
	artifactRequests := 0
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer connection.Close()
		connections++
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			t.Errorf("send protocol challenge: %v", err)
			return
		}
		var frame map[string]any
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		requestID, _ := frame["id"].(string)
		if connections == 1 {
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "res", "id": requestID, "ok": false,
				"error": map[string]any{"code": "UNAVAILABLE", "retryable": true, "retryAfterMs": float64(1)},
			}); err != nil {
				t.Errorf("send unavailable handshake: %v", err)
			}
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": requestID, "ok": true, "payload": map[string]any{
				"type": "hello-ok", "protocol": float64(4), "server": map[string]any{"version": "2026.8.1", "connId": "connection-2"},
				"features": map[string]any{"methods": []string{"artifacts.list"}}, "snapshot": map[string]any{},
				"auth": map[string]any{"role": "operator", "scopes": []string{"operator.read"}}, "policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
			},
		}); err != nil {
			t.Errorf("send retry handshake: %v", err)
			return
		}
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Errorf("receive artifact request: %v", err)
			return
		}
		if frame["method"] != "artifacts.list" {
			t.Errorf("unexpected gateway method %q", frame["method"])
			return
		}
		artifactRequests++
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{"artifacts": []any{}},
		}); err != nil {
			t.Errorf("send artifacts response: %v", err)
			return
		}
		close(done)
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")
	reference := "ocgw:v2:" + uuid.NewString()
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "private-run-id", CreatedAt: time.Now().UTC(), TerminalStatus: "completed", TerminalAt: time.Now().UTC(),
	}}}
	adapter.gatewayArtifactStore = &fakeOpenClawGatewayArtifactStore{}

	result := adapter.ImportDelegatedArtifacts(context.Background(), "task-1", reference)
	if result.Status != "none" {
		t.Fatalf("artifact import result = %#v, want no artifact metadata", result)
	}
	if connections != 2 || artifactRequests != 1 {
		t.Fatalf("connections=%d artifactRequests=%d, want one fresh handshake retry and one metadata request", connections, artifactRequests)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete retried artifact import")
	}
}

type fakeOpenClawGatewayArtifactStore struct {
	descriptors []GatewayArtifactDescriptor
}

func (s *fakeOpenClawGatewayArtifactStore) CreateOpenClawGatewayArtifactDescriptors(_ context.Context, _ string, descriptors []GatewayArtifactDescriptor) error {
	s.descriptors = append(s.descriptors, descriptors...)
	return nil
}

func TestOpenClawCompanionGatewayAuthenticatedReadDiscoveryClassifiesRejectedHandshake(t *testing.T) {
	const privateGatewayMessage = "gateway rejected private read token token-should-not-leak"
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			t.Errorf("send protocol challenge: %v", err)
			return
		}
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": false,
			"error": map[string]any{
				"code": "FORBIDDEN", "message": privateGatewayMessage,
				"details": map[string]any{"missingScope": "operator.read", "token": "token-should-not-leak"},
			},
		}); err != nil {
			t.Errorf("send handshake rejection: %v", err)
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := &openClawAdapter{
		gatewayURL:   "ws" + strings.TrimPrefix(server.URL, "http"),
		gatewayToken: "read-token",
		timeout:      time.Second,
		allowedHost:  map[string]bool{"127.0.0.1": true},
	}
	_, err := adapter.gatewayAuthenticatedOperatorReadDiscovery(context.Background())
	if err == nil {
		t.Fatal("gatewayAuthenticatedOperatorReadDiscovery() error = nil, want classified handshake rejection")
	}
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error type = %T, want *openClawGatewayResponseError", err)
	}
	if gatewayErr.Category != openClawGatewayErrorAccessDenied || gatewayErr.Retryable {
		t.Fatalf("gateway error = %#v", gatewayErr)
	}
	if strings.Contains(err.Error(), privateGatewayMessage) || strings.Contains(err.Error(), "token-should-not-leak") || strings.Contains(err.Error(), "operator.read") {
		t.Fatalf("discovery error leaked gateway detail: %q", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete handshake rejection")
	}
}

func TestOpenClawCompanionGatewayAuthenticatedReadDiscoveryRetriesOneBoundedUnavailableHandshake(t *testing.T) {
	connections := 0
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer connection.Close()
		connections++
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			t.Errorf("send protocol challenge: %v", err)
			return
		}
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			t.Errorf("receive handshake: %v", err)
			return
		}
		if connections == 1 {
			if err := websocket.JSON.Send(connection, map[string]any{
				"type": "res", "id": request["id"], "ok": false,
				"error": map[string]any{"code": "UNAVAILABLE", "retryable": true, "retryAfterMs": float64(1)},
			}); err != nil {
				t.Errorf("send unavailable response: %v", err)
			}
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": request["id"], "ok": true,
			"payload": map[string]any{
				"type":     "hello-ok",
				"protocol": float64(4),
				"server":   map[string]any{"version": "2026.8.1", "connId": "connection-2"},
				"features": map[string]any{"methods": []string{}, "events": []string{}},
				"snapshot": map[string]any{},
				"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
				"policy":   map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
			},
		}); err != nil {
			t.Errorf("send retry hello response: %v", err)
			return
		}
		close(done)
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := &openClawAdapter{
		gatewayURL:   "ws" + strings.TrimPrefix(server.URL, "http"),
		gatewayToken: "read-token",
		timeout:      time.Second,
		allowedHost:  map[string]bool{"127.0.0.1": true},
	}
	evidence, err := adapter.gatewayAuthenticatedOperatorReadDiscovery(context.Background())
	if err != nil {
		t.Fatalf("gatewayAuthenticatedOperatorReadDiscovery() error = %v", err)
	}
	if evidence.Version != "2026.8.1" || connections != 2 {
		t.Fatalf("read discovery evidence = %#v connections=%d", evidence, connections)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not complete retried handshake")
	}
}

func TestOpenClawCompanionGatewayReadOnlyTaskLedgerDiscoveryReturnsOnlyAggregateCounts(t *testing.T) {
	var receivedFrames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			for len(receivedFrames) < 2 {
				var frame map[string]any
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					return
				}
				receivedFrames = append(receivedFrames, frame)
				requestID, _ := frame["id"].(string)
				if frame["method"] == "connect" {
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res",
						"id":   requestID,
						"ok":   true,
						"payload": map[string]any{
							"type":     "hello-ok",
							"protocol": float64(4),
							"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
							"features": map[string]any{"methods": []string{"tasks.list"}, "events": []string{}},
							"snapshot": map[string]any{},
							"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
							"policy":   map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
						},
					}); err != nil {
						t.Errorf("send hello response: %v", err)
						return
					}
					continue
				}
				if err := websocket.JSON.Send(connection, map[string]any{
					"type": "res",
					"id":   requestID,
					"ok":   true,
					"payload": map[string]any{
						"tasks": []map[string]any{
							{"id": "sensitive-task-one", "title": "Never expose this", "status": "running"},
							{"id": "sensitive-task-two", "title": "Never expose this either", "status": "completed"},
							{"id": "sensitive-task-three", "title": "Still private", "status": "running"},
						},
						"nextCursor": "more-private-tasks",
					},
				}); err != nil {
					t.Errorf("send task ledger response: %v", err)
				}
			}
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	health := newOpenClawAdapterFromEnv().HealthCheck(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish the bounded task ledger discovery")
	}
	if health.Status != "available" || health.GatewayTaskLedger == nil {
		t.Fatalf("task-ledger discovery health = %#v", health)
	}
	if health.GatewayTaskLedger.SampledTasks != 3 || !health.GatewayTaskLedger.Truncated || health.GatewayTaskLedger.StatusCounts["running"] != 2 || health.GatewayTaskLedger.StatusCounts["completed"] != 1 {
		t.Fatalf("unexpected aggregate task ledger = %#v", health.GatewayTaskLedger)
	}
	if !health.GatewayProtocolValidated || !health.GatewayAuthenticated || health.GatewayScope != "operator.read" || health.GatewayEvidenceSchema != "openclaw-gateway-protocol-v4" {
		t.Fatalf("authenticated task-ledger evidence = %#v", health)
	}
	if len(receivedFrames) != 2 || receivedFrames[0]["method"] != "connect" || receivedFrames[1]["method"] != "tasks.list" {
		t.Fatalf("unexpected gateway frames = %#v", receivedFrames)
	}
	params, ok := receivedFrames[1]["params"].(map[string]any)
	if !ok || params["limit"] != float64(openClawGatewayTaskLedgerLimit) || len(params) != 1 {
		t.Fatalf("task-ledger request should contain only its bounded limit: %#v", receivedFrames[1])
	}
}

func TestOpenClawCompanionGatewayCapabilityAndPreparedModelDiscoveryReturnsOnlyAggregateCounts(t *testing.T) {
	var receivedFrames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			for len(receivedFrames) < 5 {
				var frame map[string]any
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					return
				}
				receivedFrames = append(receivedFrames, frame)
				requestID, _ := frame["id"].(string)
				switch frame["method"] {
				case "connect":
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res",
						"id":   requestID,
						"ok":   true,
						"payload": map[string]any{
							"type":     "hello-ok",
							"protocol": float64(4),
							"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
							"features": map[string]any{"methods": []string{"commands.list", "models.list", "skills.status", "tools.catalog"}, "events": []string{}},
							"snapshot": map[string]any{},
							"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
							"policy":   map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
						},
					}); err != nil {
						t.Errorf("send hello response: %v", err)
					}
				case "skills.status":
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"skills": []map[string]any{
								{"skillKey": "private-project-context", "eligible": true, "description": "Never retain this text"},
								{"skillKey": "credential-sensitive", "eligible": false, "description": "Neither this"},
							},
						},
					}); err != nil {
						t.Errorf("send skills response: %v", err)
					}
				case "tools.catalog":
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"groups": []map[string]any{{
								"tools": []map[string]any{
									{"name": "private-shell-tool", "source": "core"},
									{"name": "private-plugin-tool", "source": "plugin", "pluginId": "private-plugin"},
								},
							}},
						},
					}); err != nil {
						t.Errorf("send tools response: %v", err)
					}
				case "commands.list":
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"commands": []map[string]any{
								{"name": "private-command", "args": []string{"never retain arguments"}},
								{"name": "also-private"},
							},
						},
					}); err != nil {
						t.Errorf("send commands response: %v", err)
					}
				case "models.list":
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res", "id": requestID, "ok": true,
						"payload": map[string]any{
							"models": []map[string]any{
								{"id": "private-model-a", "provider": "private-provider", "available": true},
								{"id": "private-model-b", "provider": "private-provider", "available": false},
								{"id": "private-model-c", "provider": "private-provider"},
							},
						},
					}); err != nil {
						t.Errorf("send models response: %v", err)
					}
				default:
					t.Errorf("unexpected Gateway method %q", frame["method"])
					return
				}
			}
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	health := newOpenClawAdapterFromEnv().HealthCheck(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish bounded capability discovery")
	}
	if health.Status != "available" || health.GatewayCapabilityCatalog == nil || health.GatewayPreparedModelCatalog == nil {
		t.Fatalf("capability discovery health = %#v", health)
	}
	catalog := health.GatewayCapabilityCatalog
	if catalog.SampledSkills != 2 || catalog.EligibleSkills != 1 || catalog.SampledCommands != 2 || catalog.ToolCountsBySource["core"] != 1 || catalog.ToolCountsBySource["plugin"] != 1 {
		t.Fatalf("unexpected aggregate capability catalog = %#v", catalog)
	}
	models := health.GatewayPreparedModelCatalog
	if models.SampledModels != 3 || models.AvailableModels != 1 || models.UnavailableModels != 1 || models.UnknownAvailabilityModels != 1 {
		t.Fatalf("unexpected aggregate prepared model catalog = %#v", models)
	}
	if len(receivedFrames) != 5 || receivedFrames[0]["method"] != "connect" || receivedFrames[1]["method"] != "skills.status" || receivedFrames[2]["method"] != "tools.catalog" || receivedFrames[3]["method"] != "commands.list" || receivedFrames[4]["method"] != "models.list" {
		t.Fatalf("unexpected gateway frames = %#v", receivedFrames)
	}
	for _, frame := range receivedFrames[1:] {
		params, ok := frame["params"].(map[string]any)
		if !ok || (frame["method"] != "commands.list" && frame["method"] != "models.list" && len(params) != 0) || (frame["method"] == "commands.list" && (len(params) != 1 || params["includeArgs"] != false)) || (frame["method"] == "models.list" && (len(params) != 2 || params["view"] != "configured" || params["preparedOnly"] != true)) {
			t.Fatalf("capability request should be parameter-free and read-only: %#v", frame)
		}
	}
}

func TestOpenClawCompanionGatewayAgentRosterDiscoveryReturnsOnlyAggregateCounts(t *testing.T) {
	var receivedFrames []map[string]any
	done := make(chan struct{})
	gateway := websocket.Server{
		Handler: func(connection *websocket.Conn) {
			defer close(done)
			if err := websocket.JSON.Send(connection, map[string]any{
				"type":    "event",
				"event":   "connect.challenge",
				"payload": map[string]any{"nonce": "challenge-nonce", "ts": float64(1_737_264_000_000)},
			}); err != nil {
				t.Errorf("send protocol challenge: %v", err)
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			for len(receivedFrames) < 2 {
				var frame map[string]any
				if err := websocket.JSON.Receive(connection, &frame); err != nil {
					return
				}
				receivedFrames = append(receivedFrames, frame)
				requestID, _ := frame["id"].(string)
				if frame["method"] == "connect" {
					if err := websocket.JSON.Send(connection, map[string]any{
						"type": "res",
						"id":   requestID,
						"ok":   true,
						"payload": map[string]any{
							"type":     "hello-ok",
							"protocol": float64(4),
							"server":   map[string]any{"version": "2026.8.1", "connId": "connection-1"},
							"features": map[string]any{"methods": []string{"agents.list"}, "events": []string{}},
							"snapshot": map[string]any{},
							"auth":     map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
							"policy":   map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
						},
					}); err != nil {
						t.Errorf("send hello response: %v", err)
					}
					continue
				}
				if frame["method"] != "agents.list" {
					t.Errorf("unexpected Gateway method %q", frame["method"])
					return
				}
				if err := websocket.JSON.Send(connection, map[string]any{
					"type": "res", "id": requestID, "ok": true,
					"payload": map[string]any{"agents": []map[string]any{
						{"id": "private-agent-a", "kind": "agent", "model": "private-model", "workspace": "/private/a"},
						{"id": "private-agent-b", "kind": "agent", "model": "private-model", "workspace": "/private/b"},
						{"id": "private-system", "kind": "system", "model": "private-model"},
						{"id": "legacy-agent", "model": "private-model"},
					}},
				}); err != nil {
					t.Errorf("send agents response: %v", err)
				}
			}
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"status":"live"}`))
	})
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	health := newOpenClawAdapterFromEnv().HealthCheck(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not finish bounded agent-roster discovery")
	}
	if health.Status != "available" || health.GatewayAgentRoster == nil {
		t.Fatalf("agent-roster discovery health = %#v", health)
	}
	roster := health.GatewayAgentRoster
	if roster.SampledAgents != 4 || roster.AgentCount != 2 || roster.SystemCount != 1 || roster.UnknownKindCount != 1 {
		t.Fatalf("unexpected aggregate agent roster = %#v", roster)
	}
	if len(receivedFrames) != 2 || receivedFrames[0]["method"] != "connect" || receivedFrames[1]["method"] != "agents.list" {
		t.Fatalf("unexpected gateway frames = %#v", receivedFrames)
	}
	params, ok := receivedFrames[0]["params"].(map[string]any)
	if !ok {
		t.Fatalf("connect params missing: %#v", receivedFrames[0])
	}
	caps, ok := params["caps"].([]any)
	if !ok || len(caps) != 1 || caps[0] != "agent-kind" {
		t.Fatalf("agent roster discovery should request only the agent-kind capability: %#v", params)
	}
	if readParams, ok := receivedFrames[1]["params"].(map[string]any); !ok || len(readParams) != 0 {
		t.Fatalf("agents.list should be parameter-free and read-only: %#v", receivedFrames[1])
	}
}

func TestOpenClawGatewayTaskLedgerRejectsUnknownTaskStatus(t *testing.T) {
	_, err := openClawGatewayTaskLedgerFromResponse(json.RawMessage(`{"tasks":[{"status":"unexpected"}]}`))
	if err == nil {
		t.Fatal("unknown task status should be rejected")
	}
}

func TestOpenClawGatewayCapabilityCatalogRejectsMissingToolGroups(t *testing.T) {
	_, err := openClawGatewayCapabilityCatalogFromResponses(
		json.RawMessage(`{"skills":[]}`),
		json.RawMessage(`{}`),
		json.RawMessage(`{"commands":[]}`),
	)
	if err == nil {
		t.Fatal("missing tools catalog groups should be rejected")
	}
}

func TestOpenClawGatewayPreparedModelCatalogRejectsMalformedModels(t *testing.T) {
	_, err := openClawGatewayPreparedModelCatalogFromResponse(json.RawMessage(`{"models":{}}`))
	if err == nil {
		t.Fatal("non-array model catalog should be rejected")
	}
}

func TestPresentGatewayJSONRejectsMissingOrNullValues(t *testing.T) {
	tests := []struct {
		name  string
		value json.RawMessage
		want  bool
	}{
		{name: "missing", value: nil, want: false},
		{name: "empty", value: json.RawMessage("  "), want: false},
		{name: "null", value: json.RawMessage("null"), want: false},
		{name: "object", value: json.RawMessage(`{}`), want: true},
		{name: "array", value: json.RawMessage(`[]`), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := presentGatewayJSON(test.value); got != test.want {
				t.Fatalf("presentGatewayJSON(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestValidGatewayEvidenceValueRejectsUnboundedOrControlValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "version", value: "2026.7.1-2", want: true},
		{name: "control character", value: "2026.7\n1", want: false},
		{name: "oversized", value: strings.Repeat("a", 129), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validGatewayEvidenceValue(test.value); got != test.want {
				t.Fatalf("validGatewayEvidenceValue(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestOpenClawHighRiskSurfacesBlockExecutionUntilAcknowledged(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	adapter := &openClawAdapter{
		enabled:                  true,
		gatewayEnabled:           true,
		gatewayDelegationEnabled: true,
		gatewayURL:               "ws://127.0.0.1:18789",
		gatewayToken:             "gateway-read-token",
		gatewayDelegationToken:   "gateway-write-token",
		gatewayReceiptStore:      &fakeOpenClawGatewayReceiptStore{},
		allowedHost:              map[string]bool{"127.0.0.1": true},
		agentCLIEnabled:          true,
		messagesEnabled:          true,
		browserEnabled:           true,
		hostToolsEnabled:         true,
		sandboxRequired:          true,
		sandboxMode:              "all",
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
	if info.Configured || info.ExecutionEnabled || !containsExact(info.MissingConfiguration, gatewayPolicyAttestationBlockReason) {
		t.Fatalf("acknowledging high-risk surfaces must not bypass run-bound policy attestation: %#v", info)
	}
	if !strings.Contains(strings.Join(info.Controls, " "), "OPENCLAW_ALLOW_HIGH_RISK_EXECUTION=true") {
		t.Fatalf("controls should disclose high-risk acknowledgement: %#v", info.Controls)
	}
}

func TestOpenClawSetEcosystemPathPreservesPreviousArchives(t *testing.T) {
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
	if _, err := os.Stat(firstUpload); err != nil {
		t.Fatalf("previous upload path should remain available: %v", err)
	}

	if _, err := registry.SetOpenClawEcosystemPath(manualFirst); err != nil {
		t.Fatalf("set manual path: %v", err)
	}
	if _, err := os.Stat(secondUpload); err != nil {
		t.Fatalf("prior upload path should remain available: %v", err)
	}

	if _, err := registry.SetOpenClawEcosystemPath(manualSecond); err != nil {
		t.Fatalf("set second manual path: %v", err)
	}
	if _, err := os.Stat(manualFirst); err != nil {
		t.Fatalf("manual path should not be removed by cleanup: %v", err)
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
		"Configured HAI surfaces:2",
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

func TestDeepSeekHarnessAdapterIsRegisteredAndDisabledByDefault(t *testing.T) {
	t.Setenv("DEEPSEEK_HARNESS_ENABLED", "false")
	t.Setenv("DEEPSEEK_HARNESS_WORKSPACE", "")
	t.Setenv("DEEPSEEK_HARNESS_STATE_DIR", "")
	t.Setenv("AGENT_RUNTIME_WORKSPACE_ROOT", "")
	registry := NewRegistry(newDeepSeekHarnessAdapterFromEnv())
	infos := registry.List()
	if len(infos) != 1 {
		t.Fatalf("runtime count = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.ID != "deepseek-harness" || info.Enabled || info.ExecutionEnabled || info.Type != "deepseek_harness" {
		t.Fatalf("unexpected DeepSeek Harness info: %#v", info)
	}
	if !containsString(info.MissingConfiguration, "DEEPSEEK_HARNESS_WORKSPACE") {
		t.Fatalf("missing configuration = %#v, want workspace", info.MissingConfiguration)
	}
	if !containsString(info.MissingConfiguration, "DEEPSEEK_HARNESS_VERSION") {
		t.Fatalf("missing configuration = %#v, want pinned version", info.MissingConfiguration)
	}
	if skills, err := registry.Skills(context.Background(), "deepseek-harness"); err != nil || len(skills) != 1 || skills[0].ExecutionMode != "approved_headless_task" {
		t.Fatalf("skills = %#v, err = %v", skills, err)
	}
}

func TestDeepSeekHarnessAdapterRequiresExplicitHeadlessExecutionOptIn(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executable:                  "dsh",
		expectedVersion:             "0.1.7-alpha.2",
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		allowDirectExecutionForTest: true,
	}
	info := adapter.Info()
	if !info.Configured || info.ExecutionEnabled {
		t.Fatalf("adapter configuration = %#v, want configured but execution opt-in disabled", info)
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "EXECUTION_ENABLED") {
		t.Fatalf("result = %#v, want execution opt-in block", result)
	}
}

func TestDeepSeekHarnessProductionAdapterFailsClosedWithoutIsolation(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_HARNESS_ENABLED", "true")
	t.Setenv("DEEPSEEK_HARNESS_EXECUTION_ENABLED", "true")
	t.Setenv("DEEPSEEK_HARNESS_WORKSPACE", workspace)
	t.Setenv("AGENT_RUNTIME_WORKSPACE_ROOT", root)
	t.Setenv("DEEPSEEK_HARNESS_STATE_DIR", filepath.Join(workspace, ".state"))
	t.Setenv("DEEPSEEK_HARNESS_VERSION", "0.1.7-alpha.2")
	t.Setenv("HAI_HOST_RUNTIME_BRIDGE_ENABLED", "true")
	t.Setenv("HAI_HOST_RUNTIME_BRIDGE_TOKEN", strings.Repeat("x", 40))
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := newDeepSeekHarnessAdapterFromEnv(dispatcher)

	if info := adapter.Info(); info.Configured || !containsString(info.MissingConfiguration, dshExecutionIsolationUnavailable) {
		t.Fatalf("production runtime info = %#v; want an explicit isolation block", info)
	}
	if health := adapter.HealthCheck(context.Background()); health.Status != "blocked" || !strings.Contains(health.Reason, "OS-enforced least-privilege sandbox") || !strings.Contains(health.Reason, "acknowledged server-to-Windows process-start protocol") {
		t.Fatalf("production runtime health = %#v; want blocked before readiness probes", health)
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("production-block", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "OS-enforced least-privilege sandbox") || !strings.Contains(result.Message, "acknowledged server-to-Windows process-start protocol") {
		t.Fatalf("production task result = %#v; want isolation block", result)
	}
	if dispatcher.task.TaskID != "" {
		t.Fatalf("production runtime queued host work without isolation: %#v", dispatcher.task)
	}
}

func TestDeepSeekHarnessAdapterQueuesApprovedTaskToHostBridge(t *testing.T) {
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		expectedVersion:             "0.1.1-rc.2",
		workspaceKey:                "hai",
		dispatcher:                  dispatcher,
		hostDispatchEnabled:         true,
		allowDirectExecutionForTest: true,
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "queued" || !strings.Contains(result.Message, "Windows host bridge") {
		t.Fatalf("result = %#v, want queued host bridge task", result)
	}
	if dispatcher.task.RuntimeID != "deepseek-harness" || dispatcher.task.TaskID != "harness-task" || !dispatcher.task.Approved {
		t.Fatalf("host runtime task = %#v", dispatcher.task)
	}
}

func TestDeepSeekHarnessAdapterDoesNotQueuePreCancelledTask(t *testing.T) {
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := adapter.ExecuteTask(ctx, approvedRuntimeTask("cancelled-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "cancelled before dispatch") {
		t.Fatalf("pre-cancelled task result = %#v", result)
	}
	if dispatcher.task.TaskID != "" {
		t.Fatalf("pre-cancelled task reached durable dispatcher: %#v", dispatcher.task)
	}
}

func TestDeepSeekHarnessAdapterSurfacesCancellationDuringDurableEnqueue(t *testing.T) {
	var registry *Registry
	var stopResult StopResult
	dispatcher := &capturingHostRuntimeDispatcher{onEnqueue: func() {
		stopResult = registry.StopTask(context.Background(), "deepseek-harness", "racing-task", "alice")
	}}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	registry = newVerifiedTestRegistry(adapter)
	task := withValidFinalEffectProof("deepseek-harness", Task{
		ID: "racing-task", Prompt: "inspect workspace", OwnerIdentity: "alice",
		HumanApproved: true, ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}, adapter.Info())

	result := registry.Execute(context.Background(), "deepseek-harness", task)
	if stopResult.Status != "cancelled" || !strings.Contains(stopResult.Message, "no longer leaseable") {
		t.Fatalf("Registry stop result = %#v, want durable cancellation confirmation", stopResult)
	}
	if result.Status != "cancelled" || result.ExecutionReference == "" || !result.durableCancellationConfirmed {
		t.Fatalf("enqueue-race result = %#v, want durable reference and confirmed cancellation", result)
	}
	if dispatcher.task.TaskID != "racing-task" || dispatcher.job.Status != hostruntime.StatusCancelled || !containsString(result.AuditEvents, "stop request confirmed the exact host job was durably revoked before execution") {
		t.Fatalf("enqueue-race did not retain durable dispatch evidence: task=%#v result=%#v", dispatcher.task, result)
	}
}

func TestDeepSeekHarnessAdapterRevokesJobWhenStopArrivesBeforeEnqueuePersists(t *testing.T) {
	var registry *Registry
	var stopResult StopResult
	dispatcher := &capturingHostRuntimeDispatcher{beforeEnqueue: func() {
		stopResult = registry.StopTask(context.Background(), "deepseek-harness", "precommit-race", "alice")
	}}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	registry = newVerifiedTestRegistry(adapter)
	task := withValidFinalEffectProof("deepseek-harness", Task{
		ID: "precommit-race", Prompt: "inspect workspace", OwnerIdentity: "alice",
		HumanApproved: true, ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}, adapter.Info())

	result := registry.Execute(context.Background(), "deepseek-harness", task)
	if stopResult.Status != "cancellation_requested" {
		t.Fatalf("stop before durable enqueue = %#v; want request-pending status, not success", stopResult)
	}
	if result.Status != "cancelled" || result.ExecutionReference == "" || !result.durableCancellationConfirmed {
		t.Fatalf("enqueue response did not trigger a durable retry of cancellation: %#v", result)
	}
	if dispatcher.job == nil || dispatcher.job.Status != hostruntime.StatusCancelled || dispatcher.cancelCalls < 2 {
		t.Fatalf("racing host job was not revoked after persistence: job=%#v calls=%d", dispatcher.job, dispatcher.cancelCalls)
	}
}

func TestDeepSeekHarnessOwnerStopRevokesQueuedJobAndRefusesFalseSuccessAfterConfirmation(t *testing.T) {
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	registry := NewRegistry(adapter)
	queued := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("durable-stop", "inspect workspace"))
	if queued.Status != "queued" || queued.ExecutionReference == "" {
		t.Fatalf("initial host queue result = %#v", queued)
	}
	stop := registry.StopTask(context.Background(), "deepseek-harness", "durable-stop", "alice")
	if stop.Status != "cancelled" || stop.ExecutionReference != queued.ExecutionReference || dispatcher.job.Status != hostruntime.StatusCancelled {
		t.Fatalf("owner-bound stop = %#v; job=%#v, want matching durable cancellation", stop, dispatcher.job)
	}

	confirmedDispatcher := &capturingHostRuntimeDispatcher{}
	confirmedAdapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: confirmedDispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}
	confirmedQueued := confirmedAdapter.ExecuteTask(context.Background(), approvedRuntimeTask("already-confirmed", "inspect workspace"))
	confirmedDispatcher.job.Status = hostruntime.StatusLeased
	confirmedAt := time.Now().UTC()
	confirmedDispatcher.job.ExecutionConfirmedAt = &confirmedAt
	confirmedRegistry := NewRegistry(confirmedAdapter)
	uncertain := confirmedRegistry.StopTaskWithReference(context.Background(), "deepseek-harness", "already-confirmed", "alice", confirmedQueued.ExecutionReference)
	if uncertain.Status == "cancelled" || !strings.Contains(uncertain.Message, "already confirmed execution") || confirmedDispatcher.job.Status != hostruntime.StatusLeased {
		t.Fatalf("stop after worker confirmation = %#v; job=%#v, must not claim cancellation", uncertain, confirmedDispatcher.job)
	}
}

func TestDeepSeekHarnessAdapterTreatsMissingEnqueueReceiptAsIndeterminate(t *testing.T) {
	for _, test := range []struct {
		name     string
		job      *hostruntime.Job
		override bool
	}{
		{name: "nil job", override: true},
		{name: "empty identity", job: &hostruntime.Job{}, override: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := &capturingHostRuntimeDispatcher{job: test.job, overrideJob: test.override}
			adapter := &deepSeekHarnessAdapter{
				enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
				workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
				allowDirectExecutionForTest: true,
			}

			result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("unknown-enqueue", "inspect workspace"))
			if result.Status != "indeterminate" || result.ExecutionReference != "" || !strings.Contains(result.Message, "outcome is unknown") {
				t.Fatalf("missing enqueue receipt result = %#v", result)
			}
		})
	}
}

func TestDeepSeekHarnessAdapterDoesNotBypassUnavailableStopControl(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, expectedVersion: "0.1.1-rc.2",
		workspaceKey: "hai", dispatcher: dispatcher, hostDispatchEnabled: true,
		allowDirectExecutionForTest: true,
	}

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("task-secondary-runtime", "inspect workspace"))
	if result.Status != "blocked" {
		t.Fatalf("secondary runtime status = %q, want blocked without stop control", result.Status)
	}
	if dispatcher.task.TaskID != "" {
		t.Fatalf("secondary runtime bypassed the stop gate and queued work: %#v", dispatcher.task)
	}
}

func TestDeepSeekHarnessAdapterDoesNotQueueWhenHostBridgeIsDisabled(t *testing.T) {
	dispatcher := &capturingHostRuntimeDispatcher{}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		expectedVersion:             "0.1.1-rc.2",
		workspaceKey:                "hai",
		dispatcher:                  dispatcher,
		allowDirectExecutionForTest: true,
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "host bridge is disabled") {
		t.Fatalf("result = %#v, want disabled host bridge block", result)
	}
	if dispatcher.task.TaskID != "" {
		t.Fatalf("disabled host bridge unexpectedly received task: %#v", dispatcher.task)
	}
}

func TestDeepSeekHarnessAdapterRunsDocumentedHeadlessProfile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		versionProbe:                func(context.Context) (string, error) { return "0.1.7-alpha.2", nil },
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		timeout:                     5 * time.Second,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "needs_review" || result.ExitCode != 0 ||
		!strings.Contains(result.Message, "has not independently verified the requested outcome") ||
		!containsString(result.AuditEvents, "zero process exit does not verify the requested outcome or downstream effects") ||
		!strings.Contains(result.Output, "--profile") || !strings.Contains(result.Output, "headless") {
		t.Fatalf("result = %#v, want successful headless process recorded for outcome review", result)
	}
	if !strings.Contains(result.Output, "DSH_HOME=") || !strings.Contains(result.Output, "HAI_RUNTIME_TASK_ID=harness-task") {
		t.Fatalf("result output = %q, want isolated state and task metadata", result.Output)
	}
}

func TestDeepSeekHarnessAdapterRejectsOptionLikePrompt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		versionProbe:                func(context.Context) (string, error) { return "0.1.7-alpha.2", nil },
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		timeout:                     time.Second,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "--install-plugin=untrusted"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "must not start with a command option") {
		t.Fatalf("result = %#v, want option-like prompt rejection", result)
	}
}

func TestDeepSeekHarnessAdapterRejectsLauncherSubcommandPrompt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		timeout:                     time.Second,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}

	for _, prompt := range []string{"web", "plugin"} {
		result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", prompt))
		if result.Status != "blocked" || !strings.Contains(result.Message, "launcher subcommand") {
			t.Fatalf("prompt %q result = %#v, want launcher subcommand rejection", prompt, result)
		}
	}
}

func TestDeepSeekHarnessAdapterBoundsWaitingForSharedState(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		timeout:                     10 * time.Millisecond,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}
	if !adapter.acquireExecutionGate(context.Background()) {
		t.Fatal("could not acquire test execution gate")
	}
	defer adapter.releaseExecutionGate()

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "already running") {
		t.Fatalf("result = %#v, want bounded shared-state wait", result)
	}
}

func TestDeepSeekHarnessAdapterRejectsWorkspaceOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	adapter := &deepSeekHarnessAdapter{
		enabled:       true,
		executable:    "dsh",
		workspace:     outside,
		workspaceRoot: root,
		stateDir:      filepath.Join(outside, ".dsh-state"),
	}
	if reason := adapter.workspaceBlockedReason(); !strings.Contains(reason, "must stay inside") {
		t.Fatalf("workspace block reason = %q", reason)
	}
}

func TestDeepSeekHarnessAdapterRejectsMissingWorkspaceRoot(t *testing.T) {
	workspace := t.TempDir()
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		workspace:                   workspace,
		workspaceRoot:               "",
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		allowDirectExecutionForTest: true,
	}

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "workspace root") {
		t.Fatalf("result = %#v, want missing workspace root block", result)
	}
}

func TestDeepSeekHarnessAdapterRejectsStateDirectoryOutsideRoot(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(t.TempDir(), ".dsh-state"),
		allowDirectExecutionForTest: true,
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "state directory must stay inside") {
		t.Fatalf("result = %#v, want state directory block", result)
	}
}

func TestDeepSeekHarnessAdapterRejectsStateDirectorySymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	stateDir := filepath.Join(workspace, ".dsh-state")
	if err := os.Symlink(outside, stateDir); err != nil {
		t.Skipf("symlink creation is unavailable in this test environment: %v", err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:          true,
		executionEnabled: true,
		executable:       os.Args[0],
		expectedVersion:  "0.1.7-alpha.2",
		workspace:        workspace,
		workspaceRoot:    root,
		stateDir:         stateDir,
	}
	if reason := adapter.stateDirBlockedReason(); !strings.Contains(reason, "must not be a symbolic link") {
		t.Fatalf("state directory block reason = %q", reason)
	}
}

func TestDeepSeekHarnessHealthReportsPreviewReadiness(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		versionProbe:                func(context.Context) (string, error) { return "0.1.7-alpha.2", nil },
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		allowDirectExecutionForTest: true,
	}
	health := adapter.HealthCheck(context.Background())
	if health.Status != "ready" || !strings.Contains(health.Reason, "headless") {
		t.Fatalf("health = %#v, want headless readiness", health)
	}
}

func TestDeepSeekHarnessHostBridgeHealthRequiresWorkerHeartbeat(t *testing.T) {
	adapter := &deepSeekHarnessAdapter{
		enabled:             true,
		executionEnabled:    true,
		expectedVersion:     "0.1.7-alpha.2",
		workspaceKey:        "hai",
		dispatcher:          &admissionTestDispatcher{},
		hostDispatchEnabled: true,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "unavailable" || !strings.Contains(health.Reason, "current worker heartbeat") {
		t.Fatalf("host bridge health = %#v, want unavailable until worker heartbeat is authenticated and current", health)
	}
}

func TestDeepSeekHarnessAdapterBlocksVersionMismatchBeforeTaskExecution(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "deepseek-harness")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.8-alpha.2",
		versionProbe:                func(context.Context) (string, error) { return "0.1.7-alpha.2", nil },
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    filepath.Join(workspace, ".dsh-state"),
		timeout:                     time.Second,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("harness-task", "inspect workspace"))
	if result.Status != "blocked" || !strings.Contains(result.Message, "version mismatch") {
		t.Fatalf("result = %#v, want version mismatch block", result)
	}
}

func TestDeepSeekHarnessVersionProbeRequiresExactSemVer(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		reported string
		blocked  bool
	}{
		{name: "normalized v prefix and whitespace", expected: "v0.1.7-alpha.2", reported: " 0.1.7-alpha.2 \n"},
		{name: "exact build metadata", expected: "0.1.7-alpha.2+build.1", reported: "v0.1.7-alpha.2+build.1"},
		{name: "prefix impostor", expected: "0.1.7-alpha.2", reported: "release-0.1.7-alpha.2", blocked: true},
		{name: "suffix impostor", expected: "0.1.7-alpha.2", reported: "0.1.7-alpha.2-evil", blocked: true},
		{name: "display label", expected: "0.1.7-alpha.2", reported: "dsh 0.1.7-alpha.2", blocked: true},
		{name: "trailing text", expected: "0.1.7-alpha.2", reported: "0.1.7-alpha.2 preview", blocked: true},
		{name: "multiple lines", expected: "0.1.7-alpha.2", reported: "0.1.7-alpha.2\n0.1.7-alpha.2", blocked: true},
		{name: "different build metadata", expected: "0.1.7-alpha.2+build.1", reported: "0.1.7-alpha.2+build.2", blocked: true},
		{name: "invalid pinned wildcard", expected: "0.1.*", reported: "0.1.7-alpha.2", blocked: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &deepSeekHarnessAdapter{
				expectedVersion: test.expected,
				versionProbe: func(context.Context) (string, error) {
					return test.reported, nil
				},
			}
			reason := adapter.versionBlockedReason(context.Background())
			if test.blocked && reason == "" {
				t.Fatalf("version probe was accepted; expected it to be blocked")
			}
			if !test.blocked && reason != "" {
				t.Fatalf("version probe was blocked: %s", reason)
			}
		})
	}
}

func TestDeepSeekHarnessCLIVersionProbeRequiresExactSemVer(t *testing.T) {
	tests := []struct {
		name     string
		reported string
		blocked  bool
	}{
		{name: "exact token", reported: "0.1.7-alpha.2\n"},
		{name: "prefix impostor", reported: "release-0.1.7-alpha.2", blocked: true},
		{name: "suffix impostor", reported: "0.1.7-alpha.2-evil", blocked: true},
		{name: "display label", reported: "dsh 0.1.7-alpha.2", blocked: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HAI_DSH_TEST_VERSION_OUTPUT", test.reported)
			adapter := &deepSeekHarnessAdapter{
				executable:      os.Args[0],
				expectedVersion: "0.1.7-alpha.2",
				workspace:       t.TempDir(),
				timeout:         5 * time.Second,
				outputLimit:     defaultOutputLimit,
				envAllow:        []string{"HAI_DSH_TEST_VERSION_OUTPUT"},
			}
			reason := adapter.versionBlockedReason(context.Background())
			if test.blocked && !strings.Contains(reason, "version mismatch") {
				t.Fatalf("version probe reason = %q, want mismatch block", reason)
			}
			if !test.blocked && reason != "" {
				t.Fatalf("version probe reason = %q, want successful exact comparison", reason)
			}
		})
	}
}

type capturingHostRuntimeDispatcher struct {
	task          hostruntime.ApprovedTask
	job           *hostruntime.Job
	overrideJob   bool
	err           error
	beforeEnqueue func()
	onEnqueue     func()
	cancelCalls   int
}

func (d *capturingHostRuntimeDispatcher) EnqueueContext(_ context.Context, task hostruntime.ApprovedTask) (*hostruntime.Job, error) {
	d.task = task
	if d.beforeEnqueue != nil {
		d.beforeEnqueue()
	}
	if d.err != nil {
		return nil, d.err
	}
	if d.overrideJob {
		return d.job, nil
	}
	d.job = &hostruntime.Job{ID: uuid.New(), RuntimeID: task.RuntimeID, OwnerIdentity: task.OwnerIdentity, TaskID: task.TaskID, Status: hostruntime.StatusPending}
	if d.onEnqueue != nil {
		d.onEnqueue()
	}
	return d.job, nil
}

func (d *capturingHostRuntimeDispatcher) CancelTask(_ context.Context, ownerIdentity, taskID string, jobID uuid.UUID) (*hostruntime.Job, bool, error) {
	d.cancelCalls++
	if d.job == nil || d.job.OwnerIdentity != ownerIdentity || d.job.TaskID != taskID || (jobID != uuid.Nil && d.job.ID != jobID) {
		return nil, false, hostruntime.ErrJobNotFound
	}
	if d.job.Status == hostruntime.StatusPending || (d.job.Status == hostruntime.StatusLeased && d.job.ExecutionConfirmedAt == nil) {
		d.job.Status = hostruntime.StatusCancelled
		d.job.LeaseDigest = ""
		d.job.LeaseExpires = nil
		return d.job, true, nil
	}
	return d.job, d.job.Status == hostruntime.StatusCancelled, nil
}

type fakeAdapter struct {
	info        Info
	called      bool
	healthCalls int
}

type taskCapturingAdapter struct {
	*fakeAdapter
	received Task
}

func (a *taskCapturingAdapter) ExecuteTask(_ context.Context, task Task) Result {
	a.called = true
	a.received = task
	return Result{RuntimeID: a.info.ID, Status: "completed"}
}

func (a *fakeAdapter) Info() Info {
	return a.info
}

func (a *fakeAdapter) HealthCheck(context.Context) Health {
	a.healthCalls++
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

func TestOpenClawGatewayTransportPolicy(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		allowedHost map[string]bool
		gateway     bool
		pins        []string
		wantBlocked bool
	}{
		{name: "plaintext loopback IPv4", endpoint: "ws://127.0.0.1:18789", allowedHost: map[string]bool{"127.0.0.1": true}},
		{name: "plaintext localhost", endpoint: "http://localhost:18789", allowedHost: map[string]bool{"localhost": true}},
		{name: "TLS remote host", endpoint: "wss://gateway.example:18789", allowedHost: map[string]bool{"gateway.example": true}},
		{name: "remote plaintext WebSocket", endpoint: "ws://gateway.example:18789", allowedHost: map[string]bool{"gateway.example": true}, wantBlocked: true},
		{name: "remote plaintext HTTP", endpoint: "http://gateway.example:18789", allowedHost: map[string]bool{"gateway.example": true}, wantBlocked: true},
		{name: "wildcard allowlist", endpoint: "ws://127.0.0.1:18789", allowedHost: map[string]bool{"*": true, "127.0.0.1": true}, wantBlocked: true},
		{name: "private literal despite TLS", endpoint: "wss://10.1.2.3:18789", allowedHost: map[string]bool{"10.1.2.3": true}, wantBlocked: true},
		{name: "link-local literal", endpoint: "wss://169.254.10.20:18789", allowedHost: map[string]bool{"169.254.10.20": true}, wantBlocked: true},
		{name: "IPv6 loopback", endpoint: "ws://[::1]:18789", allowedHost: map[string]bool{"::1": true}},
		{name: "Compose host gateway exact opt-in", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, pins: []string{"192.168.65.254"}},
		{name: "Compose TLS host gateway exact opt-in", endpoint: "wss://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, pins: []string{"fd00::1"}},
		{name: "Compose host gateway requires IP pins", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, wantBlocked: true},
		{name: "Compose host gateway rejects public pin", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, pins: []string{"93.184.216.34"}, wantBlocked: true},
		{name: "Compose host gateway rejects invalid pin", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, pins: []string{"invalid"}, wantBlocked: true},
		{name: "Compose host gateway without allowlist entry", endpoint: "ws://host.docker.internal:18789", gateway: true, wantBlocked: true},
		{name: "Compose host gateway requires enabled gateway", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"host.docker.internal": true}, wantBlocked: true},
		{name: "Compose host gateway requires explicit port", endpoint: "ws://host.docker.internal", allowedHost: map[string]bool{"host.docker.internal": true}, gateway: true, wantBlocked: true},
		{name: "Compose host gateway rejects wildcard", endpoint: "ws://host.docker.internal:18789", allowedHost: map[string]bool{"*": true, "host.docker.internal": true}, gateway: true, wantBlocked: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &openClawAdapter{gatewayEnabled: test.gateway, gatewayURL: test.endpoint, allowedHost: test.allowedHost, gatewayDockerHostIPs: test.pins}
			reason := adapter.validGatewayURL()
			if test.wantBlocked && reason == "" {
				t.Fatal("validGatewayURL() accepted an unsafe endpoint")
			}
			if !test.wantBlocked && reason != "" {
				t.Fatalf("validGatewayURL() = %q, want allowed endpoint", reason)
			}
		})
	}
}

func TestOpenClawGatewayConstructorLoadsDockerHostPins(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws://host.docker.internal:18789")
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "host.docker.internal")
	t.Setenv("OPENCLAW_GATEWAY_DOCKER_HOST_IPS", " 192.168.65.254 , fd00::1 ")
	adapter := newOpenClawAdapterFromEnv()
	if adapter.validGatewayURL() != "" || !adapter.allowedDockerGatewayAddress(net.ParseIP("192.168.65.254")) || !adapter.allowedDockerGatewayAddress(net.ParseIP("fd00::1")) || adapter.allowedDockerGatewayAddress(net.ParseIP("10.0.0.5")) {
		t.Fatal("constructor must apply only the explicit Docker-host IP pins")
	}
}

func TestOpenClawGatewayHealthDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Add(1)
		fmt.Fprint(w, `{"ok":true,"status":"live"}`)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/health", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	adapter := &openClawAdapter{gatewayURL: origin.URL, allowedHost: map[string]bool{"127.0.0.1": true}, timeout: time.Second}
	health := adapter.gatewayHealthCheck(context.Background(), time.Now())
	if health.Status != "unavailable" || followed.Load() != 0 {
		t.Fatalf("redirect health=%s, redirected requests=%d; want unavailable and none", health.Status, followed.Load())
	}
}

func TestOpenClawGatewayHealthRejectsUntrustedTLSCertificate(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ok":true,"status":"live"}`)
	}))
	defer server.Close()
	adapter := &openClawAdapter{gatewayURL: server.URL, allowedHost: map[string]bool{"127.0.0.1": true}, timeout: time.Second}
	health := adapter.gatewayHealthCheck(context.Background(), time.Now())
	if health.Status != "unavailable" || requests.Load() != 0 {
		t.Fatalf("TLS health=%s, HTTP requests=%d; want certificate rejected before HTTP", health.Status, requests.Load())
	}
}

func TestOpenClawGatewayDefaultAllowlistDoesNotOptIntoDockerHost(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws://host.docker.internal:18789")
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "")

	adapter := newOpenClawAdapterFromEnv()
	if reason := adapter.validGatewayURL(); reason == "" {
		t.Fatal("default host allowlist unexpectedly opted into host.docker.internal")
	}
}

func TestOpenClawGatewayDNSResolutionRejectsUnsafeAnswers(t *testing.T) {
	tests := []struct {
		name               string
		host               string
		addresses          []string
		dockerGatewayOptIn bool
		wantBlocked        bool
	}{
		{name: "localhost loopback", host: "localhost", addresses: []string{"127.0.0.1"}},
		{name: "localhost dual stack loopback", host: "localhost", addresses: []string{"::1", "127.0.0.1"}},
		{name: "localhost public rebinding", host: "localhost", addresses: []string{"93.184.216.34"}, wantBlocked: true},
		{name: "localhost mixed public rebinding", host: "localhost", addresses: []string{"127.0.0.1", "93.184.216.34"}, wantBlocked: true},
		{name: "remote public address", host: "gateway.example", addresses: []string{"93.184.216.34"}},
		{name: "private DNS answer", host: "gateway.example", addresses: []string{"10.2.3.4"}, wantBlocked: true},
		{name: "loopback DNS rebinding", host: "gateway.example", addresses: []string{"127.0.0.1"}, wantBlocked: true},
		{name: "mixed public and private answers", host: "gateway.example", addresses: []string{"93.184.216.34", "192.168.1.8"}, wantBlocked: true},
		{name: "shared address space", host: "gateway.example", addresses: []string{"100.64.0.10"}, wantBlocked: true},
		{name: "localhost private answer", host: "localhost", addresses: []string{"127.0.0.1", "10.0.0.8"}, wantBlocked: true},
		{name: "link-local metadata address", host: "gateway.example", addresses: []string{"169.254.169.254"}, wantBlocked: true},
		{name: "empty DNS answer", host: "gateway.example", wantBlocked: true},
		{name: "Docker host private gateway answer", host: "host.docker.internal", addresses: []string{"192.168.65.254"}, dockerGatewayOptIn: true},
		{name: "Docker host IPv6 ULA gateway answer", host: "host.docker.internal", addresses: []string{"fd00::1"}, dockerGatewayOptIn: true},
		{name: "Docker host unpinned private answer", host: "host.docker.internal", addresses: []string{"10.2.3.4"}, dockerGatewayOptIn: true, wantBlocked: true},
		{name: "Docker host private DNS without opt-in", host: "host.docker.internal", addresses: []string{"192.168.65.254"}, wantBlocked: true},
		{name: "Docker host public DNS answer", host: "host.docker.internal", addresses: []string{"93.184.216.34"}, dockerGatewayOptIn: true, wantBlocked: true},
		{name: "Docker host loopback DNS answer", host: "host.docker.internal", addresses: []string{"127.0.0.1"}, dockerGatewayOptIn: true, wantBlocked: true},
		{name: "Docker host link-local metadata answer", host: "host.docker.internal", addresses: []string{"169.254.169.254"}, dockerGatewayOptIn: true, wantBlocked: true},
		{name: "Docker host shared address-space answer", host: "host.docker.internal", addresses: []string{"100.64.0.10"}, dockerGatewayOptIn: true, wantBlocked: true},
		{name: "Docker host mixed DNS answers", host: "host.docker.internal", addresses: []string{"192.168.65.254", "93.184.216.34"}, dockerGatewayOptIn: true, wantBlocked: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &openClawAdapter{
				gatewayEnabled:       test.dockerGatewayOptIn,
				gatewayDockerHostIPs: []string{"192.168.65.254", "fd00::1"},
				gatewayURL:           "ws://host.docker.internal:18789",
				allowedHost:          map[string]bool{"host.docker.internal": true},
				gatewayResolver: func(_ context.Context, host string) ([]net.IP, error) {
					if host != test.host {
						return nil, fmt.Errorf("unexpected lookup host %q", host)
					}
					ips := make([]net.IP, 0, len(test.addresses))
					for _, address := range test.addresses {
						ips = append(ips, net.ParseIP(address))
					}
					return ips, nil
				},
			}
			addresses, err := adapter.resolveOpenClawGatewayAddresses(context.Background(), test.host)
			if test.wantBlocked && err == nil {
				t.Fatalf("resolveOpenClawGatewayAddresses() accepted %#v", addresses)
			}
			if !test.wantBlocked && err != nil {
				t.Fatalf("resolveOpenClawGatewayAddresses() error = %v", err)
			}
		})
	}
}

func TestOpenClawGatewayDialUsesOnlyVettedResolvedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			acceptErrors <- err
			return
		}
		accepted <- connection
	}()

	var lookups atomic.Int32
	endpoint, err := url.Parse("ws://localhost:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	if err != nil {
		t.Fatalf("parse test endpoint: %v", err)
	}
	adapter := &openClawAdapter{
		gatewayEnabled: true,
		gatewayURL:     endpoint.String(),
		allowedHost:    map[string]bool{"localhost": true},
		gatewayResolver: func(_ context.Context, host string) ([]net.IP, error) {
			lookups.Add(1)
			if host != "localhost" {
				return nil, fmt.Errorf("unexpected lookup host %q", host)
			}
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	}
	connection, err := adapter.dialOpenClawGatewayTransport(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial vetted loopback address: %v", err)
	}
	defer connection.Close()
	if lookups.Load() != 1 {
		t.Fatalf("DNS lookup count = %d, want exactly one resolution before the pinned dial", lookups.Load())
	}
	remote, ok := connection.RemoteAddr().(*net.TCPAddr)
	if !ok || !remote.IP.IsLoopback() {
		t.Fatalf("connected remote address = %v, want vetted loopback IP", connection.RemoteAddr())
	}
	select {
	case serverConnection := <-accepted:
		_ = serverConnection.Close()
	case err := <-acceptErrors:
		t.Fatalf("accept pinned connection: %v", err)
	case <-time.After(time.Second):
		t.Fatal("listener did not receive the pinned connection")
	}
}

func TestOpenClawGatewayDialPinsConfiguredHostAndPort(t *testing.T) {
	var lookups atomic.Int32
	adapter := &openClawAdapter{
		gatewayDockerHostIPs: []string{"192.168.65.254"},
		gatewayEnabled:       true,
		gatewayURL:           "ws://host.docker.internal:18789",
		allowedHost:          map[string]bool{"host.docker.internal": true},
		gatewayResolver: func(context.Context, string) ([]net.IP, error) {
			lookups.Add(1)
			return []net.IP{net.ParseIP("192.168.65.254")}, nil
		},
	}

	for _, target := range []struct {
		host string
		port string
	}{
		{host: "host.docker.internal", port: "18790"},
		{host: "other.internal", port: "18789"},
	} {
		if _, err := adapter.dialOpenClawGatewayHostPort(context.Background(), target.host, target.port, time.Second); err == nil {
			t.Fatalf("dialOpenClawGatewayHostPort(%q, %q) succeeded outside the configured destination", target.host, target.port)
		}
	}
	if lookups.Load() != 0 {
		t.Fatalf("resolved %d hosts before rejecting an unconfigured destination; want 0", lookups.Load())
	}
	if !adapter.gatewayDialTargetMatchesConfiguredURL("HOST.DOCKER.INTERNAL.", "18789") {
		t.Fatal("configured host and port should match case-insensitively with a trailing DNS dot")
	}
	adapter.gatewayEnabled = false
	if adapter.gatewayDialTargetMatchesConfiguredURL("host.docker.internal", "18789") {
		t.Fatal("disabled Gateway unexpectedly authorized a transport dial")
	}
}

func TestOpenClawGatewayHealthProbeBlocksPrivateDNSAnswer(t *testing.T) {
	var lookups atomic.Int32
	adapter := &openClawAdapter{
		enabled:        true,
		gatewayEnabled: true,
		gatewayURL:     "wss://gateway.example:18789",
		allowedHost:    map[string]bool{"gateway.example": true},
		gatewayResolver: func(_ context.Context, host string) ([]net.IP, error) {
			lookups.Add(1)
			if host != "gateway.example" {
				return nil, fmt.Errorf("unexpected lookup host %q", host)
			}
			return []net.IP{net.ParseIP("10.2.3.4")}, nil
		},
		timeout: time.Second,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Status != "unavailable" || !strings.Contains(health.Reason, "health endpoint is unavailable") {
		t.Fatalf("HealthCheck() = %#v, want blocked network probe", health)
	}
	if lookups.Load() != 1 {
		t.Fatalf("health probe resolved target %d times, want one vetted resolution", lookups.Load())
	}
}

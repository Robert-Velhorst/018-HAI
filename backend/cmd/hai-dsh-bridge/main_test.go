package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopbackURLRejectsPublicAndCredentialedEndpoints(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8092",
		"http://localhost:8092",
		"http://[::1]:8092",
	} {
		endpoint, err := url.Parse(raw)
		if err != nil || !isLoopbackURL(endpoint) {
			t.Fatalf("%q must be accepted", raw)
		}
	}
	for _, raw := range []string{
		"http://example.com:8092",
		"http://127.0.0.1:8092?token=unsafe",
		"http://token@127.0.0.1:8092",
	} {
		endpoint, _ := url.Parse(raw)
		if isLoopbackURL(endpoint) {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}

func TestVerifyVersionOutputRequiresExactSemVerToken(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		output   string
		wantErr  bool
	}{
		{name: "official prerelease token", expected: "0.1.7-alpha.2", output: "0.1.7-alpha.2\n"},
		{name: "optional v prefix on pin", expected: "v0.1.7-alpha.2", output: "0.1.7-alpha.2"},
		{name: "optional v prefix on output", expected: "0.1.7-alpha.2", output: "v0.1.7-alpha.2\r\n"},
		{name: "build metadata is part of the exact pin", expected: "1.2.3+build.1", output: "1.2.3+build.1"},
		{name: "different valid version", expected: "0.1.7-alpha.2", output: "0.1.7-alpha.20", wantErr: true},
		{name: "suffix impostor", expected: "0.1.7", output: "0.1.7-malicious", wantErr: true},
		{name: "prefix impostor", expected: "0.1.7", output: "not-0.1.7", wantErr: true},
		{name: "decorated output is not the CLI version token", expected: "0.1.7", output: "dsh 0.1.7", wantErr: true},
		{name: "trailing output is rejected", expected: "0.1.7", output: "0.1.7 extra", wantErr: true},
		{name: "multiple output lines are rejected", expected: "0.1.7", output: "0.1.7\n0.1.7", wantErr: true},
		{name: "wildcard pin is rejected", expected: "0.1.*", output: "0.1.7", wantErr: true},
		{name: "leading zero is invalid SemVer", expected: "01.1.7", output: "01.1.7", wantErr: true},
		{name: "build metadata mismatch", expected: "1.2.3+build.1", output: "1.2.3+build.2", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyVersionOutput(tt.expected, []byte(tt.output))
			if tt.wantErr && err == nil {
				t.Fatal("verifyVersionOutput succeeded; want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("verifyVersionOutput returned unexpected error: %v", err)
			}
		})
	}
}

func TestConfirmLeaseSendsTokenAndRejectsNonNoContentResponses(t *testing.T) {
	var received confirmRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/host-runtime/leases/job-1/confirm" {
			t.Fatalf("unexpected confirmation request: %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode confirmation: %v", err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	configuration := config{baseURL: endpoint, token: strings.Repeat("a", 32)}
	leased := lease{Token: "lease-token"}
	leased.Job.ID = "job-1"
	if err := confirmLease(context.Background(), server.Client(), configuration, leased); err != nil {
		t.Fatalf("confirmLease: %v", err)
	}
	if received.LeaseToken != leased.Token {
		t.Fatalf("confirmation token = %q, want %q", received.LeaseToken, leased.Token)
	}
}

func TestConfirmLeaseDistinguishesCancellationFromStaleLease(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want error
	}{
		{name: "durable cancellation", body: `{"code":"cancellation_requested"}`, want: errCancellationRequested},
		{name: "generic conflict remains stale", body: `{"error":"lease expired"}`, want: errStaleLease},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusConflict)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			configuration := config{baseURL: endpoint, token: strings.Repeat("a", 32)}
			leased := lease{Token: "lease-token"}
			leased.Job.ID = "job-1"
			if err := confirmLease(context.Background(), server.Client(), configuration, leased); !errors.Is(err, test.want) {
				t.Fatalf("confirmLease error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConfirmLeasePreservesIsolationBlockFromHTTP423(t *testing.T) {
	const isolationReason = "host runtime execution is blocked because operating-system isolation is unavailable"
	for _, test := range []struct {
		name         string
		body         string
		want         error
		wantContains []string
	}{
		{
			name:         "isolation unavailable remains distinct from emergency stop",
			body:         `{"code":"execution_isolation_unavailable","error":"` + isolationReason + `"}`,
			want:         errExecutionIsolationUnavailable,
			wantContains: []string{"HTTP 423", "execution_isolation_unavailable", isolationReason},
		},
		{
			name: "other locked response remains emergency stop",
			body: `{"error":"host runtime execution is blocked by emergency stop"}`,
			want: errEmergencyStop,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusLocked)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			configuration := config{baseURL: endpoint, token: strings.Repeat("a", 32)}
			leased := lease{Token: "lease-token"}
			leased.Job.ID = "job-1"

			err = confirmLease(context.Background(), server.Client(), configuration, leased)
			if !errors.Is(err, test.want) {
				t.Fatalf("confirmLease error = %v, want %v", err, test.want)
			}
			if test.want == errExecutionIsolationUnavailable && errors.Is(err, errEmergencyStop) {
				t.Fatalf("isolation refusal was incorrectly classified as emergency stop: %v", err)
			}
			for _, fragment := range test.wantContains {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("confirmLease error %q does not preserve %q", err, fragment)
				}
			}
		})
	}
}

func TestSubmitCompletionAcknowledgesExplicitStopOnlyAfterKnownTermination(t *testing.T) {
	for _, test := range []struct {
		name            string
		fatal           bool
		wantTermination bool
	}{
		{name: "process tree exited", wantTermination: true},
		{name: "termination uncertain", fatal: true, wantTermination: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			var acknowledgments []completion
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/api/v1/host-runtime/leases/job-1/complete" {
					t.Errorf("unexpected path: %s", request.URL.Path)
				}
				var payload completion
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				if calls.Add(1) == 1 {
					writer.WriteHeader(http.StatusConflict)
					_, _ = writer.Write([]byte(`{"code":"cancellation_requested"}`))
					return
				}
				acknowledgments = append(acknowledgments, payload)
				writer.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			configuration := config{baseURL: endpoint, token: strings.Repeat("c", 32)}
			leased := lease{Token: "lease-token"}
			leased.Job.ID = "job-1"
			result := completion{LeaseToken: leased.Token, ExitCode: -1, Error: "execution result", processTreeTerminated: test.wantTermination, fatal: test.fatal}
			err := submitCompletion(context.Background(), server.Client(), configuration, leased, result)
			if test.fatal {
				if !errors.Is(err, errFatalSupervisorState) {
					t.Fatalf("submitCompletion error = %v; uncertain termination must stop the bridge", err)
				}
			} else if err != nil {
				t.Fatalf("submitCompletion: %v", err)
			}
			if calls.Load() != 2 || len(acknowledgments) != 1 {
				t.Fatalf("completion calls=%d acknowledgments=%#v", calls.Load(), acknowledgments)
			}
			ack := acknowledgments[0]
			if !ack.CancellationRequested || ack.TerminationVerified != test.wantTermination {
				t.Fatalf("cancellation acknowledgment flags = %#v", ack)
			}
			if test.fatal && ack.TerminationVerified {
				t.Fatal("uncertain process-tree termination was reported as verified")
			}
		})
	}
}

func TestSubmitCompletionDoesNotTreatGenericConflictAsStop(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":"stale_lease"}`))
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	leased := lease{Token: "lease-token"}
	leased.Job.ID = "job-1"
	err := submitCompletion(context.Background(), server.Client(), config{baseURL: endpoint, token: strings.Repeat("x", 32)}, leased, completion{LeaseToken: leased.Token})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("submitCompletion error=%v calls=%d; generic conflict must not trigger cancellation retry", err, calls.Load())
	}
}

func TestBridgeRequestsDoNotFollowRedirectsOrForwardBearerToken(t *testing.T) {
	var redirectedCalls atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectedCalls.Add(1)
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect target received Authorization header %q", got)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer redirectTarget.Close()

	for _, test := range []struct {
		name string
		call func(*http.Client, config, lease) error
	}{
		{
			name: "lease confirmation",
			call: func(client *http.Client, configuration config, leased lease) error {
				return confirmLease(context.Background(), client, configuration, leased)
			},
		},
		{
			name: "completion",
			call: func(client *http.Client, configuration config, leased lease) error {
				return submitCompletion(context.Background(), client, configuration, leased, completion{LeaseToken: leased.Token, ExitCode: 0})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gatewayCalls atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				gatewayCalls.Add(1)
				if got := request.Header.Get("Authorization"); got != "Bearer bridge-secret" {
					t.Errorf("gateway Authorization = %q, want configured bearer token", got)
				}
				http.Redirect(writer, request, redirectTarget.URL+"/capture", http.StatusFound)
			}))
			defer gateway.Close()

			endpoint, err := url.Parse(gateway.URL)
			if err != nil {
				t.Fatal(err)
			}
			configuration := config{baseURL: endpoint, token: "bridge-secret"}
			leased := lease{Token: "lease-token"}
			leased.Job.ID = "job-1"
			if err := test.call(gateway.Client(), configuration, leased); err == nil || !strings.Contains(err.Error(), "302") {
				t.Fatalf("bridge request error = %v, want redirect response to fail closed", err)
			}
			if gatewayCalls.Load() != 1 {
				t.Fatalf("gateway request count = %d, want one", gatewayCalls.Load())
			}
			if redirectedCalls.Load() != 0 {
				t.Fatalf("redirect target received %d requests; bridge must not follow gateway redirects", redirectedCalls.Load())
			}
		})
	}
}

func TestAmbiguousProcessStartFailureCannotAuthorizeStopAcknowledgment(t *testing.T) {
	startErr := errors.New("process setup failed after child creation; cleanup was not verified")
	_, runErr := runContainedProcess(context.Background(), processSpec{Path: "dsh.exe"}, func(context.Context) error {
		return nil
	}, nil, func(processSpec) (suspendedProcess, error) {
		return nil, startErr
	})
	result := completionFromProcessResult(context.Background(), processResult{ExitCode: -1}, runErr)
	if result.processTreeTerminated || result.TerminationVerified {
		t.Fatalf("ambiguous process-start failure must remain unverified: %#v", result)
	}

	var calls atomic.Int32
	var stopAcknowledgment completion
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload completion
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode completion: %v", err)
		}
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusConflict)
			_, _ = writer.Write([]byte(`{"code":"cancellation_requested"}`))
			return
		}
		stopAcknowledgment = payload
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	leased := lease{Token: "lease-token"}
	leased.Job.ID = "job-1"
	result.LeaseToken = leased.Token
	err = submitCompletion(context.Background(), server.Client(), config{baseURL: endpoint, token: strings.Repeat("s", 32)}, leased, result)
	if !errors.Is(err, errFatalSupervisorState) {
		t.Fatalf("submitCompletion error = %v, want bridge stop while process termination remains uncertain", err)
	}
	if calls.Load() != 2 || !stopAcknowledgment.CancellationRequested || stopAcknowledgment.TerminationVerified {
		t.Fatalf("stop acknowledgement calls=%d payload=%#v; uncertain termination must never be reported as verified", calls.Load(), stopAcknowledgment)
	}
}

func TestLeasePayloadDecodesGatewayJob(t *testing.T) {
	var leased lease
	if err := json.Unmarshal([]byte(`{"job":{"id":"job-1","runtimeId":"deepseek-harness","prompt":"inspect","workspaceKey":"deepseek-harness"},"leaseToken":"token"}`), &leased); err != nil {
		t.Fatalf("decode lease: %v", err)
	}
	if leased.Job.ID != "job-1" || leased.Job.RuntimeID != "deepseek-harness" || leased.Token != "token" {
		t.Fatalf("lease = %#v", leased)
	}
}

func TestSafeEnvironmentDropsUnapprovedVariables(t *testing.T) {
	t.Setenv("HAI_DSH_UNAPPROVED", "must-not-pass")
	t.Setenv("DEEPSEEK_API_KEY", "allowed-for-test")
	t.Setenv("USERPROFILE", `C:\Users\operator`)
	t.Setenv("LOCALAPPDATA", `C:\Users\operator\AppData\Local`)
	t.Setenv("HOME", `C:\Users\operator`)
	t.Setenv("DSH_HOME", `C:\untrusted-profile-state`)
	values := safeEnvironment([]string{"DEEPSEEK_API_KEY"}, map[string]string{"DSH_HOME": "C:\\state"})
	joined := strings.Join(values, "\n")
	for _, disallowed := range []string{"HAI_DSH_UNAPPROVED", "USERPROFILE", "LOCALAPPDATA", "HOME"} {
		for _, pair := range values {
			if strings.HasPrefix(pair, disallowed+"=") {
				t.Fatalf("safe environment leaked %q: %q", disallowed, joined)
			}
		}
	}
	if strings.Contains(joined, `C:\untrusted-profile-state`) {
		t.Fatalf("safe environment retained the ambient DSH profile: %q", joined)
	}
	if !strings.Contains(joined, "DEEPSEEK_API_KEY=allowed-for-test") || strings.Count(joined, "DSH_HOME=") != 1 || !strings.Contains(joined, "DSH_HOME=C:\\state") {
		t.Fatalf("safe environment = %q", joined)
	}
}

func TestInvalidPromptRejectsCLIControlValues(t *testing.T) {
	for _, prompt := range []string{"-unsafe", "web", "plugin", "\x00"} {
		if invalidPrompt(prompt) == "" {
			t.Fatalf("%q must be rejected", prompt)
		}
	}
	if invalidPrompt("inspect this approved workspace") != "" {
		t.Fatal("ordinary prompt was rejected")
	}
}

func TestMonitorExecutionCancelsRunningWorkWhenEmergencyStopStarts(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var confirmations atomic.Int32
	result := monitorExecution(context.Background(), 5*time.Millisecond, func(context.Context) error {
		if confirmations.Add(1) == 1 {
			return nil
		}
		return errEmergencyStop
	}, func(ctx context.Context, revalidate func(context.Context) error, onStarted func()) completion {
		if err := revalidate(ctx); err != nil {
			return completion{ExitCode: -1, Error: executionStoppedReason(err)}
		}
		onStarted()
		close(started)
		<-ctx.Done()
		close(stopped)
		return completion{ExitCode: 0, Output: "partial output"}
	})
	select {
	case <-started:
	default:
		t.Fatal("execution did not start")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("execution was not cancelled")
	}
	if result.ExitCode != -1 || result.Output != "partial output" || result.Error != "DeepSeek Harness execution was stopped because HAI emergency stop is active" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMonitorExecutionMarksStopOnlyAfterLaunchConfirmsTermination(t *testing.T) {
	for _, test := range []struct {
		name            string
		fatal           bool
		wantTermination bool
	}{
		{name: "terminated job object", wantTermination: true},
		{name: "job termination uncertain", fatal: true, wantTermination: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var confirmations atomic.Int32
			result := monitorExecution(context.Background(), 5*time.Millisecond, func(context.Context) error {
				if confirmations.Add(1) == 1 {
					return nil
				}
				return errCancellationRequested
			}, func(ctx context.Context, revalidate func(context.Context) error, onStarted func()) completion {
				if err := revalidate(ctx); err != nil {
					return completion{ExitCode: -1, Error: executionStoppedReason(err)}
				}
				onStarted()
				<-ctx.Done()
				return completion{ExitCode: -1, Error: "process stopped", processTreeTerminated: !test.fatal, fatal: test.fatal}
			})
			if !result.CancellationRequested || result.TerminationVerified != test.wantTermination || result.fatal != test.fatal {
				t.Fatalf("monitored stop result = %#v", result)
			}
		})
	}
}

func TestMonitorExecutionFailsClosedWhenLeaseCannotBeReconfirmed(t *testing.T) {
	confirmationFailure := errors.New("gateway unavailable")
	var launchCount atomic.Int32
	result := monitorExecution(context.Background(), 5*time.Millisecond, func(context.Context) error {
		return confirmationFailure
	}, func(ctx context.Context, revalidate func(context.Context) error, onStarted func()) completion {
		launchCount.Add(1)
		if err := revalidate(ctx); err != nil {
			return completion{ExitCode: -1, Error: executionStoppedReason(err)}
		}
		onStarted()
		return completion{ExitCode: 1, Error: "process cancelled"}
	})
	if result.ExitCode != -1 || !strings.Contains(result.Error, "could not reconfirm the execution lease") || !strings.Contains(result.Error, "gateway unavailable") {
		t.Fatalf("result = %#v", result)
	}
	if launchCount.Load() != 1 {
		t.Fatalf("launch invocations = %d, want exactly one authorization-gated attempt", launchCount.Load())
	}
}

func TestMonitorExecutionUsesShortBoundForRunningConfirmation(t *testing.T) {
	if executionConfirmationTimeout != 2*time.Second {
		t.Fatalf("production reconfirm timeout = %s, want explicit 2s bound", executionConfirmationTimeout)
	}
	const testTimeout = 40 * time.Millisecond
	var confirmations atomic.Int32
	observedDeadline := make(chan time.Duration, 1)
	startedAt := time.Now()
	result := monitorExecutionWithConfirmTimeout(context.Background(), time.Millisecond, testTimeout, func(ctx context.Context) error {
		if confirmations.Add(1) == 1 {
			return nil
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("running confirmation has no deadline")
		}
		observedDeadline <- time.Until(deadline)
		<-ctx.Done()
		return ctx.Err()
	}, func(ctx context.Context, revalidate func(context.Context) error, onStarted func()) completion {
		if err := revalidate(ctx); err != nil {
			return completion{ExitCode: -1, Error: executionStoppedReason(err)}
		}
		onStarted()
		<-ctx.Done()
		return completion{ExitCode: 0}
	})
	deadlineBudget := <-observedDeadline
	if deadlineBudget <= 0 || deadlineBudget > testTimeout {
		t.Fatalf("running confirmation deadline budget = %s, want (0, %s]", deadlineBudget, testTimeout)
	}
	if elapsed := time.Since(startedAt); elapsed > testTimeout+time.Second {
		t.Fatalf("failed confirmation took %s to stop execution, expected a short bounded response", elapsed)
	}
	if result.ExitCode != -1 || !strings.Contains(result.Error, "could not reconfirm the execution lease") {
		t.Fatalf("result = %#v, want fail-closed confirmation timeout", result)
	}
}

func setPermissiveBridgeEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"HAI_HOST_RUNTIME_BRIDGE_ENABLED":    "true",
		"DEEPSEEK_HARNESS_ENABLED":           "true",
		"DEEPSEEK_HARNESS_EXECUTION_ENABLED": "true",
		"HAI_HOST_RUNTIME_BRIDGE_URL":        "http://127.0.0.1:8092",
		"HAI_HOST_RUNTIME_BRIDGE_TOKEN":      strings.Repeat("t", 32),
		"DEEPSEEK_HARNESS_EXECUTABLE":        "dsh.exe",
		"DEEPSEEK_HARNESS_VERSION":           "1.2.3",
		"DEEPSEEK_HARNESS_WORKSPACE":         t.TempDir(),
		"DEEPSEEK_HARNESS_STATE_DIR":         t.TempDir(),
		"DEEPSEEK_HARNESS_WORKSPACE_KEY":     "deepseek-harness",
		"DEEPSEEK_HARNESS_TIMEOUT_SECONDS":   "120",
		"DEEPSEEK_HARNESS_ENV_ALLOWLIST":     "DEEPSEEK_API_KEY",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadConfigReturnsIsolationUnavailableDespitePermissiveEnvironment(t *testing.T) {
	setPermissiveBridgeEnvironment(t)
	if _, err := loadConfig(); !errors.Is(err, errDSHIsolationUnavailable) {
		t.Fatalf("loadConfig error = %v, want unconditional isolation-unavailable refusal", err)
	} else if !strings.Contains(err.Error(), "TLS peer pinning") || !strings.Contains(err.Error(), "OS-enforced least-privilege sandbox") || !strings.Contains(err.Error(), "acknowledged server-to-Windows process-start protocol") {
		t.Fatalf("isolation error does not identify the missing trust, containment, and stop-order controls: %v", err)
	}
}

func TestRunAndLeaseAcquisitionRefuseBeforeNetworkOrExecution(t *testing.T) {
	setPermissiveBridgeEnvironment(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"job":{"id":"forged-job","runtimeId":"deepseek-harness","prompt":"run this","workspaceKey":"deepseek-harness"},"leaseToken":"forged-token"}`))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{baseURL: endpoint, token: strings.Repeat("t", 32), workspaceKey: "deepseek-harness"}
	if err := run(context.Background(), configuration); !errors.Is(err, errDSHIsolationUnavailable) {
		t.Fatalf("run error = %v, want isolation-unavailable refusal", err)
	}
	var executorCalls atomic.Int32
	if err := runWithExecutor(context.Background(), server.Client(), configuration, func(context.Context, *http.Client, config, lease) completion {
		executorCalls.Add(1)
		return completion{ExitCode: 0}
	}); !errors.Is(err, errDSHIsolationUnavailable) || !strings.Contains(err.Error(), "acknowledged server-to-Windows process-start protocol") {
		t.Fatalf("runWithExecutor error = %v, want fail-closed refusal naming the missing stop-order protocol", err)
	}
	leased, found, err := requestLease(context.Background(), server.Client(), configuration)
	if !errors.Is(err, errDSHIsolationUnavailable) || found || leased.Token != "" {
		t.Fatalf("requestLease result = %#v, found=%v, error=%v; must reject before accepting the forged lease", leased, found, err)
	}
	if requests.Load() != 0 || executorCalls.Load() != 0 {
		t.Fatalf("bridge reached HTTP or task execution: requests=%d executorCalls=%d", requests.Load(), executorCalls.Load())
	}
}

func TestFabricatedLeaseAndVersionProbeCannotStartDSH(t *testing.T) {
	setPermissiveBridgeEnvironment(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{baseURL: endpoint, token: strings.Repeat("t", 32), executable: "fabricated-dsh.exe", version: "1.2.3"}
	var fabricated lease
	fabricated.Job.ID = "fabricated-job"
	fabricated.Job.Prompt = "run this"
	fabricated.Token = "fabricated-token"
	var processStarts atomic.Int32

	for name, result := range map[string]completion{
		"monitored lease execution": executeWithLeaseMonitor(context.Background(), server.Client(), configuration, fabricated),
		"direct execution": execute(context.Background(), configuration, fabricated.Job.Prompt, nil, func() {
			processStarts.Add(1)
		}),
	} {
		if result.ExitCode != -1 || !strings.Contains(result.Error, "execution is disabled") || !strings.Contains(result.Error, "acknowledged server-to-Windows process-start protocol") {
			t.Errorf("%s result = %#v, want non-success isolation refusal", name, result)
		}
	}
	if err := verifyVersion(context.Background(), configuration); !errors.Is(err, errDSHIsolationUnavailable) {
		t.Errorf("verifyVersion error = %v, want isolation-unavailable refusal", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("fabricated lease reached HTTP confirmation endpoint %d times", requests.Load())
	}
	if processStarts.Load() != 0 {
		t.Fatalf("DSH start callback invoked %d times despite the isolation gate", processStarts.Load())
	}
}

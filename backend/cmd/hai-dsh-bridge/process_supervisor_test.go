package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type failedOutputReader struct{ err error }

func (r failedOutputReader) Read([]byte) (int, error) { return 0, r.err }

type fakeSuspendedProcess struct {
	events        *[]string
	resumeErr     error
	terminateErr  error
	waitResult    processResult
	waitErr       error
	waitForCancel bool
	onResume      func()
	resumed       bool
	resumedSignal chan struct{}
	terminated    bool
	closed        bool
}

func (p *fakeSuspendedProcess) Resume() error {
	if p.onResume != nil {
		p.onResume()
	}
	*p.events = append(*p.events, "resume")
	if p.resumeErr == nil {
		p.resumed = true
		if p.resumedSignal != nil {
			close(p.resumedSignal)
		}
	}
	return p.resumeErr
}

func (p *fakeSuspendedProcess) Wait(ctx context.Context) (processResult, error) {
	*p.events = append(*p.events, "wait")
	if p.waitForCancel {
		<-ctx.Done()
		return processResult{ExitCode: -1}, ctx.Err()
	}
	return p.waitResult, p.waitErr
}

func (p *fakeSuspendedProcess) Terminate() error {
	*p.events = append(*p.events, "terminate-job")
	p.terminated = true
	return p.terminateErr
}

func (p *fakeSuspendedProcess) Close() error {
	*p.events = append(*p.events, "close-job")
	p.closed = true
	return nil
}

func TestRunContainedProcessDoesNotResumeAfterFailedLeaseRecheck(t *testing.T) {
	confirmationErr := errEmergencyStop
	events := []string{}
	process := &fakeSuspendedProcess{events: &events}
	result, err := runContainedProcess(context.Background(), processSpec{Path: "dsh.exe"}, func(context.Context) error {
		events = append(events, "reconfirm")
		return confirmationErr
	}, func() { events = append(events, "started") }, func(processSpec) (suspendedProcess, error) {
		events = append(events, "create-suspended-in-job")
		return process, nil
	})
	if !errors.Is(err, errEmergencyStop) || result.ExitCode != -1 {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
	if process.resumed || !process.terminated || !process.closed {
		t.Fatalf("process state resumed=%v terminated=%v closed=%v", process.resumed, process.terminated, process.closed)
	}
	if !reflect.DeepEqual(events, []string{"create-suspended-in-job", "reconfirm", "terminate-job", "close-job"}) {
		t.Fatalf("lifecycle events = %v", events)
	}
}

func TestRunContainedProcessAcknowledgesStopOnlyAfterJobTreeIsEmpty(t *testing.T) {
	for _, test := range []struct {
		name            string
		terminateErr    error
		wantTermination bool
	}{
		{name: "job object emptied", wantTermination: true},
		{name: "job object termination uncertain", terminateErr: errors.New("job accounting query failed"), wantTermination: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			process := &fakeSuspendedProcess{events: &events, terminateErr: test.terminateErr}
			result, err := runContainedProcess(context.Background(), processSpec{}, func(context.Context) error {
				return errCancellationRequested
			}, nil, func(processSpec) (suspendedProcess, error) { return process, nil })
			mapped := completionFromProcessResult(context.Background(), result, err)
			if !process.terminated || process.resumed || !process.closed {
				t.Fatalf("stop process lifecycle = %#v events=%v", process, events)
			}
			if !mapped.CancellationRequested || mapped.TerminationVerified != test.wantTermination || mapped.processTreeTerminated != test.wantTermination || mapped.fatal == test.wantTermination {
				t.Fatalf("stop completion = %#v; want verified=%v", mapped, test.wantTermination)
			}
		})
	}
}

func TestRunContainedProcessRevalidatesBeforeResumeWithoutAtomicStopOrdering(t *testing.T) {
	events := []string{}
	stopCommitted := false
	process := &fakeSuspendedProcess{
		events:     &events,
		waitResult: processResult{ExitCode: 0, Stdout: "done"},
		onResume: func() {
			stopCommitted = true
			events = append(events, "server-stop-committed-after-recheck")
		},
	}
	result, err := runContainedProcess(context.Background(), processSpec{}, func(context.Context) error {
		events = append(events, "reconfirm")
		return nil
	}, func() { events = append(events, "started") }, func(processSpec) (suspendedProcess, error) {
		events = append(events, "create-suspended-in-job")
		return process, nil
	})
	if err != nil || result.ExitCode != 0 || result.Stdout != "done" {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
	want := []string{"create-suspended-in-job", "reconfirm", "server-stop-committed-after-recheck", "resume", "started", "wait", "close-job"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want the stop-between-check-and-Resume interleaving %v", events, want)
	}
	if !stopCommitted || !process.resumed {
		t.Fatalf("race fixture did not expose the remote ordering gap: stopCommitted=%v resumed=%v", stopCommitted, process.resumed)
	}
}

func TestRunContainedProcessTerminatesWholeJobOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := []string{}
	process := &fakeSuspendedProcess{events: &events, waitForCancel: true, resumedSignal: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		_, err := runContainedProcess(ctx, processSpec{}, func(context.Context) error { return nil }, nil, func(processSpec) (suspendedProcess, error) {
			return process, nil
		})
		finished <- err
	}()
	select {
	case <-process.resumedSignal:
	case <-time.After(time.Second):
		t.Fatal("process did not reach resume")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not return within the bound")
	}
	if !process.terminated || !process.closed {
		t.Fatalf("process tree was not terminated and closed: %#v", process)
	}
}

func TestMonitorExecutionCarriesVerifiedJobTerminationIntoStopAcknowledgment(t *testing.T) {
	events := []string{}
	process := &fakeSuspendedProcess{events: &events, waitForCancel: true, resumedSignal: make(chan struct{})}
	var confirmations atomic.Int32
	result := monitorExecution(context.Background(), 5*time.Millisecond, func(context.Context) error {
		if confirmations.Add(1) == 1 {
			return nil
		}
		return errCancellationRequested
	}, func(ctx context.Context, revalidate func(context.Context) error, onStarted func()) completion {
		processResult, err := runContainedProcess(ctx, processSpec{}, revalidate, onStarted, func(processSpec) (suspendedProcess, error) {
			return process, nil
		})
		return completionFromProcessResult(ctx, processResult, err)
	})
	if !result.CancellationRequested || !result.TerminationVerified || !result.processTreeTerminated || result.fatal {
		t.Fatalf("monitored Stop result = %#v; successful Job Object termination must be acknowledged as verified", result)
	}
	if !process.terminated || !process.closed {
		t.Fatalf("Stop lifecycle did not terminate and close the contained process: %#v", process)
	}
}

func TestRunContainedProcessDeadlineTerminatesAndMapsTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	events := []string{}
	process := &fakeSuspendedProcess{events: &events, waitForCancel: true, resumedSignal: make(chan struct{})}
	type outcome struct {
		result processResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := runContainedProcess(ctx, processSpec{}, func(context.Context) error { return nil }, nil, func(processSpec) (suspendedProcess, error) {
			return process, nil
		})
		finished <- outcome{result: result, err: err}
	}()
	select {
	case <-process.resumedSignal:
	case <-time.After(time.Second):
		t.Fatal("process did not reach resume before its deadline")
	}
	select {
	case completed := <-finished:
		mapped := completionFromProcessResult(ctx, completed.result, completed.err)
		if !errors.Is(completed.err, context.DeadlineExceeded) || !strings.Contains(mapped.Error, "exceeded the configured timeout") {
			t.Fatalf("run error/completion = %v / %#v", completed.err, mapped)
		}
		if !mapped.processTreeTerminated {
			t.Fatalf("verified timeout termination was not carried into completion: %#v", mapped)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline did not terminate and return from the process tree")
	}
	if !process.terminated || !process.closed {
		t.Fatalf("deadline left process tree alive: %#v", process)
	}
}

func TestRunContainedProcessRejectsMissingAuthorizationAndStopsOnResumeFailure(t *testing.T) {
	events := []string{}
	process := &fakeSuspendedProcess{events: &events, resumeErr: errors.New("resume denied")}
	if _, err := runContainedProcess(context.Background(), processSpec{}, nil, nil, func(processSpec) (suspendedProcess, error) {
		return process, nil
	}); err == nil || !strings.Contains(err.Error(), "authorization check") {
		t.Fatalf("missing authorization error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("process was created without authorization: %v", events)
	}
	_, err := runContainedProcess(context.Background(), processSpec{}, func(context.Context) error { return nil }, nil, func(processSpec) (suspendedProcess, error) {
		return process, nil
	})
	if err == nil || !strings.Contains(err.Error(), "resume denied") || !process.terminated || !process.closed {
		t.Fatalf("resume failure was not fail-closed: err=%v process=%#v", err, process)
	}
}

func TestCompletionPreservesExistingNonzeroExitFailureContract(t *testing.T) {
	withDiagnostic := completionFromProcessResult(context.Background(), processResult{ExitCode: 7, Stdout: "partial", Stderr: "provider rejected request"}, nil)
	if withDiagnostic.ExitCode != 7 || withDiagnostic.Output != "partial" || withDiagnostic.Error != "provider rejected request" {
		t.Fatalf("completion = %#v", withDiagnostic)
	}
	withoutDiagnostic := completionFromProcessResult(context.Background(), processResult{ExitCode: 7}, nil)
	if withoutDiagnostic.Error != "DeepSeek Harness process failed without diagnostic output" {
		t.Fatalf("completion = %#v", withoutDiagnostic)
	}
	valid259 := completionFromProcessResult(context.Background(), processResult{ExitCode: 259}, nil)
	if valid259.ExitCode != 259 {
		t.Fatalf("exit code 259 was not preserved: %#v", valid259)
	}
}

func TestOutputCaptureByteLimitBoundaryForStdoutAndStderr(t *testing.T) {
	const limit = 4
	for _, stream := range []string{"stdout", "stderr"} {
		for _, test := range []struct {
			name      string
			input     string
			truncated bool
		}{
			{name: "exactly at limit", input: "abcd"},
			{name: "beyond limit", input: "abcde", truncated: true},
		} {
			t.Run(stream+"/"+test.name, func(t *testing.T) {
				bounded := limitedBuffer{remaining: limit}
				copyErr := copyProcessOutput(strings.NewReader(test.input), &bounded)
				if bounded.buffer.String() != "abcd" || bounded.remaining != 0 || bounded.truncated != test.truncated {
					t.Fatalf("captured=%q remaining=%d truncated=%v", bounded.buffer.String(), bounded.remaining, bounded.truncated)
				}

				result := processResult{ExitCode: 0, Stdout: "stdout retained"}
				if stream == "stdout" {
					result.Stdout = bounded.buffer.String()
				} else {
					result.Stderr = bounded.buffer.String()
				}

				if !test.truncated {
					if copyErr != nil {
						t.Fatalf("exact-limit copy error = %v", copyErr)
					}
					completed := completionFromProcessResult(context.Background(), result, nil)
					if completed.ExitCode != 0 || completed.Error != "" {
						t.Fatalf("exact-limit output incorrectly marked incomplete: %#v", completed)
					}
					return
				}

				if !errors.Is(copyErr, errOutputCaptureLimitExceeded) {
					t.Fatalf("over-limit copy error = %v, want byte-limit error", copyErr)
				}
				captureErr := outputCaptureError(stream, copyErr)
				completed := completionFromProcessResult(context.Background(), result, captureErr)
				if completed.ExitCode != -1 || !strings.Contains(completed.Error, "output capture was incomplete") || !strings.Contains(completed.Error, "capture "+stream) || !strings.Contains(completed.Error, "exceeded the configured byte limit") {
					t.Fatalf("over-limit completion = %#v", completed)
				}
				if stream == "stdout" && completed.Output != "abcd" {
					t.Fatalf("partial stdout was not preserved: %#v", completed)
				}
				if stream == "stderr" && completed.Output != "stdout retained" {
					t.Fatalf("stdout was not preserved when stderr overflowed: %#v", completed)
				}
				if stream == "stderr" && !strings.Contains(completed.Error, "Captured stderr (bounded): abcd") {
					t.Fatalf("partial stderr was not preserved: %#v", completed)
				}
			})
		}
	}
}

func TestTruncatedOutputCompletionSubmitsFailureNotSuccess(t *testing.T) {
	bounded := limitedBuffer{remaining: 4}
	copyErr := copyProcessOutput(strings.NewReader("abcdef"), &bounded)
	if !errors.Is(copyErr, errOutputCaptureLimitExceeded) {
		t.Fatalf("copy error = %v, want byte-limit error", copyErr)
	}
	result := completionFromProcessResult(context.Background(), processResult{
		ExitCode: 0,
		Stdout:   bounded.buffer.String(),
	}, outputCaptureError("stdout", copyErr))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/host-runtime/leases/truncated-job/complete" {
			t.Errorf("unexpected completion request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var submitted completion
		if err := json.NewDecoder(request.Body).Decode(&submitted); err != nil {
			t.Errorf("decode submitted completion: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if submitted.ExitCode != -1 || submitted.Output != "abcd" ||
			!strings.Contains(submitted.Error, "output capture was incomplete") ||
			!strings.Contains(submitted.Error, "capture stdout") {
			t.Errorf("truncated task was not submitted for review: %#v", submitted)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var leased lease
	leased.Job.ID = "truncated-job"
	leased.Token = "lease-token"
	configuration := config{baseURL: endpoint, token: strings.Repeat("t", 32)}
	if err := submitCompletion(context.Background(), server.Client(), configuration, leased, result); err != nil {
		t.Fatalf("submit truncated completion: %v", err)
	}
}

func TestIncompleteOutputCaptureFailsCompletionAndPreservesOutputLimit(t *testing.T) {
	copyErr := errors.New("simulated pipe read failure")
	if err := copyProcessOutput(failedOutputReader{err: copyErr}, &bytes.Buffer{}); !errors.Is(err, copyErr) {
		t.Fatalf("copy error = %v, want original read failure", err)
	}
	captureErr := outputCaptureError("stderr", copyErr)
	if !errors.Is(captureErr, errOutputCaptureIncomplete) || !errors.Is(captureErr, copyErr) {
		t.Fatalf("capture error = %v, want typed and underlying causes", captureErr)
	}
	completed := completionFromProcessResult(context.Background(), processResult{Stdout: "partial"}, captureErr)
	if completed.ExitCode != -1 || completed.Output != "partial" || !strings.Contains(completed.Error, "output capture was incomplete") {
		t.Fatalf("incomplete output did not fail completion: %#v", completed)
	}
}

func TestFatalCompletionRequiresTypedTerminationFailure(t *testing.T) {
	typed := completionFromProcessResult(context.Background(), processResult{}, errProcessTreeTerminationUnverified)
	if !typed.fatal {
		t.Fatal("typed process-tree termination failure was not marked fatal")
	}
	lookalike := completionFromProcessResult(context.Background(), processResult{}, errors.New(errProcessTreeTerminationUnverified.Error()))
	if lookalike.fatal {
		t.Fatal("fatality was inferred from an error string instead of the typed sentinel")
	}
}

func TestCancellationCompletionRequiresVerifiedProcessTreeExit(t *testing.T) {
	verified := completionFromProcessResult(context.Background(), processResult{Stdout: "partial"}, errCancellationRequested)
	if !verified.CancellationRequested || !verified.TerminationVerified || !verified.processTreeTerminated || verified.fatal {
		t.Fatalf("verified Stop completion = %#v; want request, verified termination, and nonfatal state", verified)
	}

	uncertainErr := errors.Join(errCancellationRequested, errProcessTreeTerminationUnverified)
	uncertain := completionFromProcessResult(context.Background(), processResult{Stdout: "partial"}, uncertainErr)
	if !uncertain.CancellationRequested || uncertain.TerminationVerified || uncertain.processTreeTerminated || !uncertain.fatal {
		t.Fatalf("uncertain Stop completion = %#v; must not report termination verified", uncertain)
	}
}

func TestUnverifiedTreeTerminationIsNotHiddenByTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	events := []string{}
	process := &fakeSuspendedProcess{events: &events, waitForCancel: true, terminateErr: errors.New("kill-on-close tree completion unverified"), resumedSignal: make(chan struct{})}
	type outcome struct {
		result processResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := runContainedProcess(ctx, processSpec{}, func(context.Context) error { return nil }, nil, func(processSpec) (suspendedProcess, error) {
			return process, nil
		})
		finished <- outcome{result: result, err: err}
	}()
	select {
	case <-process.resumedSignal:
	case <-time.After(time.Second):
		t.Fatal("process did not reach resume before its deadline")
	}
	select {
	case completed := <-finished:
		mapped := completionFromProcessResult(ctx, completed.result, completed.err)
		if !errors.Is(completed.err, errProcessTreeTerminationUnverified) || !strings.Contains(mapped.Error, "process supervisor failed") || !strings.Contains(mapped.Error, "completion unverified") {
			t.Fatalf("termination failure was hidden: err=%v completion=%#v", completed.err, mapped)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline did not return from the process tree")
	}
}

func TestIsWSLBackedConfigFailsClosedForDetectableRoutes(t *testing.T) {
	for _, test := range []struct {
		name       string
		executable string
		workspace  string
		stateDir   string
		allowlist  []string
	}{
		{name: "wsl executable", executable: "wsl.exe"},
		{name: "absolute wsl executable", executable: `C:\Windows\System32\wsl.exe`},
		{name: "wsl share workspace", executable: "dsh.exe", workspace: `\\wsl$\Ubuntu\workspace`},
		{name: "wsl localhost state", executable: "dsh.exe", stateDir: `\\wsl.localhost\Ubuntu\state`},
		{name: "forwarded wsl environment", executable: "dsh.exe", allowlist: []string{"WSL_DISTRO_NAME"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !isWSLBackedConfig(test.executable, test.workspace, test.stateDir, test.allowlist) {
				t.Fatal("detectable WSL-backed configuration was accepted")
			}
		})
	}
	if isWSLBackedConfig(`C:\Program Files\DeepSeek\dsh.exe`, `C:\HAI\workspace`, `C:\HAI\state`, []string{"DEEPSEEK_API_KEY"}) {
		t.Fatal("native Windows configuration was incorrectly classified as WSL-backed")
	}
}

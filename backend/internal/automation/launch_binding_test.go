package automation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// Low-level dispatch tests isolate authorization/admission from proof issuance.
// Public LaunchTask regressions below use recorded, signed approval fixtures.
func launchBindingDispatchFixture(automation *models.Automation, request TaskLaunchRequest) TaskLaunchRequest {
	if request.ApprovalSourceID == "" {
		request.ApprovalSourceID = "task-review:" + uuid.NewString()
	}
	digest := automationActionDigest(automation, request)
	scope, _ := approvalScopeForAutomation(automation)
	request.ApprovalBindingDigest = digest
	request.ApprovalProof = &ApprovalProof{
		AutomationID:     automation.ID,
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		ActionDigest:     digest,
		Scope:            scope,
		ApprovalSourceID: request.ApprovalSourceID,
		ExpiresAt:        time.Now().UTC().Add(time.Minute),
	}
	return captureLaunchActionBinding(automation, request)
}

func TestLaunchBindingRejectsAPIPolicyChangeDuringAuthorization(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "127.0.0.1")
			var calls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer target.Close()
			repo := newFakeAutomationRepo(&models.Automation{
				ID: uuid.New(), LaunchType: "api", LaunchTarget: method + " " + target.URL,
				ExpectedHTTPStatus: http.StatusNoContent,
			})
			svc := newTestService(repo, events.Publisher{})
			authorizer := &recordingExecutionAuthorizer{onCall: func() {
				t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "localhost")
			}}
			svc.(*service).executionAuth = authorizer
			request := approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{})
			result, err := svc.LaunchTask(repo.automation.ID, request)
			if err != nil || result == nil || result.Status != "blocked" || !strings.Contains(result.Message, "launch action digest mismatch") {
				t.Fatalf("policy drift result=%#v err=%v", result, err)
			}
			if authorizer.calls.Load() != 1 || calls.Load() != 0 || !containsString(result.AuditEvents, "action-bound approval proof verified and consumed") {
				t.Fatalf("expected post-approval/post-authorization rejection: authorization=%d effects=%d audit=%v", authorizer.calls.Load(), calls.Load(), result.AuditEvents)
			}
			if authorizer.request.EffectDigest != request.ApprovalProof.ActionDigest {
				t.Fatalf("authorization did not preserve the captured intent digest: %q", authorizer.request.EffectDigest)
			}
			if len(repo.launchIntents) != 1 || launchIntentActionDigest(&repo.launchIntents[0]) != request.ApprovalProof.ActionDigest || len(repo.launchEvents) != 1 {
				t.Fatalf("original intent or blocked outcome lost: intents=%#v outcomes=%#v", repo.launchIntents, repo.launchEvents)
			}
			t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "127.0.0.1")
			replayed, err := svc.LaunchTask(repo.automation.ID, request)
			if err != nil || replayed == nil || replayed.Status != "blocked" || replayed.LaunchEventID != result.LaunchEventID || calls.Load() != 0 || authorizer.calls.Load() != 1 {
				t.Fatalf("blocked intent redispatched: result=%#v err=%v effects=%d authorization=%d", replayed, err, calls.Load(), authorizer.calls.Load())
			}
		})
	}
}

func TestLaunchBindingRejectsReviewedMetadataMismatch(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, binding := range []string{"", strings.Repeat("f", 64)} {
			t.Run(method+"/"+binding, func(t *testing.T) {
				var calls atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.WriteHeader(http.StatusNoContent)
				}))
				defer target.Close()
				repo := newFakeAutomationRepo(&models.Automation{
					ID: uuid.New(), LaunchType: "api", LaunchTarget: method + " " + target.URL,
					ExpectedHTTPStatus: http.StatusNoContent,
				})
				svc := newTestService(repo, events.Publisher{})
				authorizer := &recordingExecutionAuthorizer{}
				svc.(*service).executionAuth = authorizer
				request := approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{})
				request.ApprovalBindingDigest = binding
				result, err := svc.LaunchTask(repo.automation.ID, request)
				if err != nil || result == nil || result.Status != "blocked" || !strings.Contains(result.Message, "approval binding digest mismatch") {
					t.Fatalf("metadata mismatch result=%#v err=%v", result, err)
				}
				if calls.Load() != 0 || authorizer.calls.Load() != 0 || containsString(result.AuditEvents, "action-bound approval proof verified and consumed") {
					t.Fatalf("metadata mismatch passed approval boundary: effects=%d authorization=%d audit=%v", calls.Load(), authorizer.calls.Load(), result.AuditEvents)
				}
			})
		}
	}
}

func TestLaunchBindingCopiesConfigurationAndProofBeforeAuthorization(t *testing.T) {
	var originalCalls, foreignCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/approved" {
			originalCalls.Add(1)
		} else {
			foreignCalls.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	repo := referenceAutomationRepo{newFakeAutomationRepo(&models.Automation{
		ID: uuid.New(), LaunchType: "api", LaunchTarget: "POST " + target.URL + "/approved",
		ExpectedHTTPStatus: http.StatusNoContent,
	})}
	svc := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{})
	originalDigest := request.ApprovalProof.ActionDigest
	svc.(*service).executionAuth = &recordingExecutionAuthorizer{onCall: func() {
		repo.automation.LaunchTarget = "POST " + target.URL + "/unreviewed"
		request.ApprovalProof.ActionDigest = strings.Repeat("e", 64)
	}}
	result, err := svc.LaunchTask(repo.automation.ID, request)
	if err != nil || result == nil || result.Status != "completed" || originalCalls.Load() != 1 || foreignCalls.Load() != 0 {
		t.Fatalf("launch did not use private action copy: result=%#v err=%v original=%d foreign=%d", result, err, originalCalls.Load(), foreignCalls.Load())
	}
	if result.Target != "POST "+target.URL+"/approved" || launchIntentActionDigest(&repo.launchIntents[0]) != originalDigest {
		t.Fatalf("launch result does not describe persisted original action: result=%#v intent=%#v", result, repo.launchIntents[0])
	}
}

func TestLaunchBindingRejectsDockerPolicyChangeDuringAuthorization(t *testing.T) {
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "reviewed-container")
	t.Setenv("AUTOMATION_DOCKER_SOCKET", filepath.Join(t.TempDir(), "no-docker.sock"))
	repo := newFakeAutomationRepo(&models.Automation{
		ID: uuid.New(), LaunchType: "docker_service", LaunchTarget: "reviewed-container", ServiceName: "reviewed-container",
	})
	svc := newTestService(repo, events.Publisher{})
	authorizer := &recordingExecutionAuthorizer{onCall: func() {
		t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "false")
	}}
	svc.(*service).executionAuth = authorizer
	result, err := svc.LaunchTask(repo.automation.ID, approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{}))
	if err != nil || result == nil || result.Status != "blocked" || !strings.Contains(result.Message, "launch action digest mismatch") || authorizer.calls.Load() != 1 {
		t.Fatalf("Docker policy drift result=%#v err=%v authorization=%d", result, err, authorizer.calls.Load())
	}
	if !containsString(result.AuditEvents, "launch action binding rejected before Docker socket access") {
		t.Fatalf("Docker drift did not stop at dispatch: %v", result.AuditEvents)
	}
}

func TestLaunchBindingRejectsScriptAndPinSwapDuringAuthorization(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir, staging := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", staging)
	script := filepath.Join(dir, "reviewed.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf approved > executed.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
	repo := newFakeAutomationRepo(&models.Automation{ID: uuid.New(), LaunchType: "script", LaunchTarget: filepath.Base(script)})
	svc := newTestService(repo, events.Publisher{})
	authorizer := &recordingExecutionAuthorizer{onCall: func() {
		if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf unreviewed > executed.txt\n"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
	}}
	svc.(*service).executionAuth = authorizer
	result, err := svc.LaunchTask(repo.automation.ID, approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{}))
	if err != nil || result == nil || result.Status != "blocked" || !strings.Contains(result.Message, "launch action digest mismatch") || authorizer.calls.Load() != 1 {
		t.Fatalf("script/pin swap result=%#v err=%v authorization=%d", result, err, authorizer.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "executed.txt")); !os.IsNotExist(err) {
		t.Fatalf("changed script reached process execution: %v", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("blocked action leaked staged script: entries=%v err=%v", entries, err)
	}
}

func TestLaunchBindingValidationFailsClosed(t *testing.T) {
	for _, launchType := range []string{"api", "script", "docker_service", "agent_runtime"} {
		t.Run(launchType, func(t *testing.T) {
			automation := &models.Automation{ID: uuid.New(), LaunchType: launchType, LaunchTarget: "GET http://localhost/reviewed", RuntimeType: "hermes"}
			request := launchBindingDispatchFixture(automation, TaskLaunchRequest{OwnerIdentity: "alice"})
			if err := validateLaunchActionBinding(automation, request); err != nil {
				t.Fatalf("unchanged binding rejected: %v", err)
			}
			for _, test := range []struct {
				name   string
				mutate func(*TaskLaunchRequest)
			}{
				{"missing snapshot", func(r *TaskLaunchRequest) { r.launchActionDigest = "" }},
				{"missing proof", func(r *TaskLaunchRequest) { r.ApprovalProof = nil }},
				{"missing metadata", func(r *TaskLaunchRequest) { r.ApprovalBindingDigest = "" }},
				{"changed task", func(r *TaskLaunchRequest) { r.Task = "unreviewed task" }},
				{"changed project", func(r *TaskLaunchRequest) { r.ProjectKey = "unreviewed project" }},
				{"changed mandate", func(r *TaskLaunchRequest) { r.MandateID = uuid.NewString() }},
				{"changed owner", func(r *TaskLaunchRequest) { r.OwnerIdentity = "bob" }},
				{"changed source", func(r *TaskLaunchRequest) { r.ApprovalSourceID = "task-review:" + uuid.NewString() }},
				{"changed scope", func(r *TaskLaunchRequest) { r.ApprovalProof.Scope = "unsupported" }},
				{"expired proof", func(r *TaskLaunchRequest) { r.ApprovalProof.ExpiresAt = time.Now().UTC().Add(-time.Second) }},
			} {
				t.Run(test.name, func(t *testing.T) {
					candidate := request
					proof := *request.ApprovalProof
					candidate.ApprovalProof = &proof
					test.mutate(&candidate)
					if err := validateLaunchActionBinding(automation, candidate); err == nil {
						t.Fatal("changed or missing action binding accepted")
					}
				})
			}
		})
	}
}

func TestLaunchBindingValidationRejectsActionPolicyDrift(t *testing.T) {
	for _, test := range []struct {
		launchType string
		policyKey  string
	}{
		{"api", "AUTOMATION_API_ALLOWED_HOSTS"},
		{"script", "AUTOMATION_SCRIPT_SHA256_ALLOWLIST"},
		{"docker_service", "AUTOMATION_DOCKER_SOCKET"},
		{"agent_runtime", "HERMES_TOOLSETS"},
	} {
		t.Run(test.launchType, func(t *testing.T) {
			t.Setenv(test.policyKey, "reviewed-policy")
			automation := &models.Automation{ID: uuid.New(), LaunchType: test.launchType, LaunchTarget: "GET http://localhost/reviewed", RuntimeType: "hermes"}
			request := launchBindingDispatchFixture(automation, TaskLaunchRequest{OwnerIdentity: "alice"})
			t.Setenv(test.policyKey, "changed-policy")
			if err := validateLaunchActionBinding(automation, request); err == nil || !strings.Contains(err.Error(), "launch action digest mismatch") {
				t.Fatalf("action policy drift accepted: %v", err)
			}
		})
	}
}

func TestLaunchProjectionFailureRetainsPersistedReceipt(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	repo := newFakeAutomationRepo(&models.Automation{
		ID: uuid.New(), LaunchType: "api", LaunchTarget: "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	repo.updateErr = errors.New("launch summary unavailable")
	svc := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, svc, repo.automation.ID, TaskLaunchRequest{})
	first, err := svc.LaunchTask(repo.automation.ID, request)
	if !errors.Is(err, repo.updateErr) || first == nil || first.Status != "completed" || len(repo.launchEvents) != 1 || first.LaunchEventID != repo.launchEvents[0].ID {
		t.Fatalf("projection failure lost executed receipt: result=%#v err=%v outcomes=%#v", first, err, repo.launchEvents)
	}
	retry, err := svc.LaunchTask(repo.automation.ID, request)
	if !errors.Is(err, repo.updateErr) || retry == nil || retry.LaunchEventID != first.LaunchEventID || retry.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("retry lost receipt or duplicated effect: result=%#v err=%v calls=%d", retry, err, calls.Load())
	}
	repo.updateErr = nil
	repaired, err := svc.LaunchTask(repo.automation.ID, request)
	if err != nil || repaired == nil || repaired.LaunchEventID != first.LaunchEventID || calls.Load() != 1 || repo.automation.LastLaunchAt == nil {
		t.Fatalf("summary repair changed receipt or duplicated effect: result=%#v err=%v calls=%d", repaired, err, calls.Load())
	}
}

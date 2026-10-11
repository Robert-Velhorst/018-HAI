package automation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestExecutionAuthorizationProfileUsesBoundedAutomationTarget(t *testing.T) {
	id := uuid.New()
	automation := &models.Automation{
		ID:           id,
		LaunchType:   "api",
		LaunchTarget: "GET https://example.test/health?verbose=true",
	}

	_, _, _, _, _, _, _, target := executionAuthorizationProfile(automation, http.MethodGet)
	if target != "automation:"+id.String() {
		t.Fatalf("authorization target = %q, want bounded automation identity", target)
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		switch os.Getenv("HAI_TEST_OPENCLAW_MODE") {
		case "complete":
			fmt.Println("openclaw-cli-test-completed")
			os.Exit(0)
		case "wait":
			if path := strings.TrimSpace(os.Getenv("HAI_TEST_OPENCLAW_STARTED_FILE")); path != "" {
				_ = os.WriteFile(path, []byte("started"), 0o600)
			}
			for {
				time.Sleep(time.Second)
			}
		}
	}
	switch os.Getenv("HAI_TEST_SCRIPT_MODE") {
	case "ok":
		fmt.Println("script-ok")
		os.Exit(0)
	case "clean-environment":
		if os.Getenv("SECRET_TOKEN") != "" {
			fmt.Println("leaked")
		} else {
			fmt.Println("clean")
		}
		os.Exit(0)
	case "redact":
		fmt.Println("token=super-secret-token")
		os.Exit(0)
	case "fail-with-secret":
		fmt.Fprintln(os.Stderr, "token=super-secret-token")
		os.Exit(1)
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

func TestLaunchExecutesAPITargetAndAuditsResult(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		_, _ = w.Write([]byte("started"))
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "API Automation",
		URLPath:            "api-automation",
		Host:               "localhost",
		Port:               8080,
		LaunchType:         "api",
		LaunchTarget:       server.URL,
		ExpectedHTTPStatus: http.StatusOK,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
	}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !called {
		t.Fatalf("expected API target to be called")
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.Output != "started" {
		t.Fatalf("output = %q, want started", result.Output)
	}
	if len(repo.launchEvents) != 1 {
		t.Fatalf("expected launch event to be persisted")
	}
	if result.LaunchEventID == uuid.Nil || result.LaunchEventID != repo.launchEvents[0].ID {
		t.Fatalf("launch event id was not returned with launch result: result=%s event=%s", result.LaunchEventID, repo.launchEvents[0].ID)
	}
	if repo.launchEvents[0].OwnerIdentity != "alice" {
		t.Fatalf("launch event owner = %q, want alice", repo.launchEvents[0].OwnerIdentity)
	}
}

func TestLaunchBlocksMutatingAPIWithoutApprovalBeforeNetworkAccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Unapproved API Automation",
		URLPath:      "unapproved-api-automation",
		LaunchType:   "api",
		LaunchTarget: "POST " + server.URL + "/mutate",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "unapproved-mutation"})
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("mutating API received %d calls without approval, want zero", calls.Load())
	}
	assertLauncherApprovalBlocked(t, result, repo, "action-bound approval proof rejected before network access")
}

func TestLaunchRejectsApprovalAfterAPITargetChangesBeforeNetworkAccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:           id,
				Name:         "Digest-bound API Automation",
				URLPath:      "digest-bound-api-automation",
				LaunchType:   "api",
				LaunchTarget: method + " " + server.URL + "/approved-target",
			})
			service := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
				OwnerIdentity: "alice",
				Task:          "Perform the reviewed " + method + " request.",
				ProjectKey:    "018-hai",
			})

			repo.automation.LaunchTarget = method + " " + server.URL + "/changed-after-review"
			result, err := service.LaunchTask(id, request)
			if err != nil {
				t.Fatalf("LaunchTask: %v", err)
			}
			if calls.Load() != 0 {
				t.Fatalf("changed action caused %d network calls, want zero", calls.Load())
			}
			assertLauncherApprovalBlocked(t, result, repo, "action-bound approval proof rejected before network access")
			if !strings.Contains(result.Message, "action digest mismatch") {
				t.Fatalf("blocked message = %q, want action digest mismatch", result.Message)
			}
		})
	}
}

func TestLaunchRejectsMismatchedApprovalBindingBeforeNetworkAccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tests := []struct {
		name          string
		mutate        func(*TaskLaunchRequest)
		expectedError string
	}{
		{name: "owner", mutate: func(request *TaskLaunchRequest) { request.OwnerIdentity = "bob" }, expectedError: "owner mismatch"},
		{name: "task", mutate: func(request *TaskLaunchRequest) { request.Task = "Different action" }, expectedError: "action digest mismatch"},
		{name: "review item", mutate: func(request *TaskLaunchRequest) { request.ApprovalSourceID = "task-review:" + uuid.NewString() }, expectedError: "approval source mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:           id,
				Name:         "Bound API Automation",
				URLPath:      "bound-api-automation",
				LaunchType:   "api",
				LaunchTarget: "POST " + server.URL + "/mutate",
			})
			service := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
				OwnerIdentity: "alice",
				Task:          "Perform the reviewed action.",
				ProjectKey:    "018-hai",
			})
			test.mutate(&request)

			result, err := service.LaunchTask(id, request)
			if err != nil {
				t.Fatalf("LaunchTask: %v", err)
			}
			if result.Status != "blocked" || !strings.Contains(result.Message, test.expectedError) {
				t.Fatalf("mismatch result = %#v, want %q block", result, test.expectedError)
			}
			if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "blocked" {
				t.Fatalf("mismatch denial was not audited: %#v", repo.launchEvents)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("mismatched proofs caused %d network calls, want zero", calls.Load())
	}
}

func TestLaunchConsumesApprovalProofOnceBeforeMutatingAPI(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Single-use API Automation",
		URLPath:            "single-use-api-automation",
		LaunchType:         "api",
		LaunchTarget:       "POST " + server.URL + "/mutate",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Perform this mutation once.",
		ProjectKey:    "018-hai",
	})

	first, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("first LaunchTask: %v", err)
	}
	if first.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("first result = %#v calls=%d, want one completed mutation", first, calls.Load())
	}
	request.IdempotencyKey = "new-logical-launch-after-consumed-approval"
	second, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("second LaunchTask: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("replayed proof caused %d network calls, want one total", calls.Load())
	}
	if second.Status != "blocked" || !strings.Contains(second.Message, ErrApprovalProofConsumed.Error()) {
		t.Fatalf("replay result = %#v, want consumed-proof block", second)
	}
	if len(repo.launchEvents) != 2 || repo.launchEvents[1].Status != "blocked" {
		t.Fatalf("replay denial was not audited: %#v", repo.launchEvents)
	}
}

func TestLaunchRejectsExpiredApprovalBeforeMutatingAPI(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	proofService := newApprovalProofTestService(t, func() time.Time { return now })
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Expiring API Automation",
		URLPath:      "expiring-api-automation",
		LaunchType:   "api",
		LaunchTarget: "POST " + server.URL + "/mutate",
	})
	service := newTestServiceWithRuntimeRegistryAndApprovalProofs(
		repo,
		events.Publisher{},
		agentruntime.DefaultRegistry(),
		proofService,
	)
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Perform the reviewed mutation.",
	})
	now = now.Add(defaultApprovalProofTTL)

	result, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("expired proof caused %d network calls, want zero", calls.Load())
	}
	if result.Status != "blocked" || !strings.Contains(result.Message, ErrApprovalProofExpired.Error()) {
		t.Fatalf("expired result = %#v, want expiry block", result)
	}
}

func TestApprovedGETAndHEADAPIProbesExecuteWithActionBoundProof(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != method {
					t.Errorf("method = %s, want %s", r.Method, method)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:                 id,
				Name:               "Read-only API Probe",
				URLPath:            "read-only-api-probe",
				LaunchType:         "api",
				LaunchTarget:       method + " " + server.URL + "/health",
				ExpectedHTTPStatus: http.StatusOK,
			})
			service := newTestService(repo, events.Publisher{})

			request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
				OwnerIdentity: "alice",
				Task:          "Run the reviewed " + method + " API probe.",
			})
			if request.ApprovalProof.Scope != ApprovalScopeAPIRead {
				t.Fatalf("proof scope = %q, want %q", request.ApprovalProof.Scope, ApprovalScopeAPIRead)
			}
			result, err := service.LaunchTask(id, request)
			if err != nil {
				t.Fatalf("LaunchTask: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("read-only API received %d calls, want one", calls.Load())
			}
			if result.Status != "completed" || result.RequiresApproval {
				t.Fatalf("approved API result = %#v, want completed after approval", result)
			}
			if !containsAuditFragment(result.AuditEvents, "action-bound approval proof verified and consumed") {
				t.Fatalf("approved API execution did not audit proof consumption: %#v", result.AuditEvents)
			}
			if !containsAuditFragment(
				result.AuditEvents,
				"unified execution authorization receipt",
			) {
				t.Fatalf("API authorization receipt missing from audit: %#v", result.AuditEvents)
			}
		})
	}
}

func TestResolveImagePathRejectsTraversal(t *testing.T) {
	previousDir := config.AppConfig.ImageSaveDir
	previousExtensions := config.AppConfig.ImageExtensions
	t.Cleanup(func() {
		config.AppConfig.ImageSaveDir = previousDir
		config.AppConfig.ImageExtensions = previousExtensions
	})
	config.AppConfig.ImageSaveDir = t.TempDir()
	config.AppConfig.ImageExtensions = []string{".png", ".jpg", ".jpeg"}

	if _, err := resolveImagePath("../secret.png"); err == nil {
		t.Fatalf("expected traversal image path to be rejected")
	}
	if _, err := resolveImagePath(`folder\secret.png`); err == nil {
		t.Fatalf("expected backslash image path to be rejected")
	}
}

func TestResolveImagePathAllowsSingleGeneratedFileName(t *testing.T) {
	previousDir := config.AppConfig.ImageSaveDir
	previousExtensions := config.AppConfig.ImageExtensions
	t.Cleanup(func() {
		config.AppConfig.ImageSaveDir = previousDir
		config.AppConfig.ImageExtensions = previousExtensions
	})
	config.AppConfig.ImageSaveDir = t.TempDir()
	config.AppConfig.ImageExtensions = []string{".png", ".jpg", ".jpeg"}

	path, err := resolveImagePath("123e4567-e89b-12d3-a456-426614174000.png")
	if err != nil {
		t.Fatalf("resolveImagePath: %v", err)
	}
	rel, err := filepath.Rel(config.AppConfig.ImageSaveDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		t.Fatalf("path = %q, want inside %q", path, config.AppConfig.ImageSaveDir)
	}
}

func TestProcessImageFileRejectsCorruptImage(t *testing.T) {
	previousDir := config.AppConfig.ImageSaveDir
	previousExtensions := config.AppConfig.ImageExtensions
	previousMaxSize := config.AppConfig.ImageMaxSize
	t.Cleanup(func() {
		config.AppConfig.ImageSaveDir = previousDir
		config.AppConfig.ImageExtensions = previousExtensions
		config.AppConfig.ImageMaxSize = previousMaxSize
	})
	config.AppConfig.ImageSaveDir = t.TempDir()
	config.AppConfig.ImageExtensions = []string{".gif"}
	config.AppConfig.ImageMaxSize = 1024

	file := testMultipartFileHeader(t, "imageFile", "bad.gif", []byte("GIF89a"))
	if _, err := (&service{}).processImageFile(file); err == nil {
		t.Fatalf("expected corrupt image to be rejected")
	}
}

func TestCreateRejectsUnsafeHostForGeneratedNginxConfig(t *testing.T) {
	repo := newFakeAutomationRepo(&models.Automation{ID: uuid.New(), Name: "Existing", URLPath: "existing", Host: "backend", Port: 80})
	service := newTestService(repo, events.Publisher{})

	created, err := service.Create(&models.Automation{
		Name: "Unsafe Host",
		Host: "backend;\nproxy_pass http://example.com;",
		Port: 80,
	})
	if err == nil {
		t.Fatalf("expected unsafe host validation error")
	}
	if created != nil {
		t.Fatalf("created = %#v, want nil", created)
	}
}

func testMultipartFileHeader(t *testing.T, fieldName, fileName string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1024 * 1024); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}
	files := request.MultipartForm.File[fieldName]
	if len(files) != 1 {
		t.Fatalf("files[%s] length = %d, want 1", fieldName, len(files))
	}
	return files[0]
}

func TestHealthCheckBlocksHTTPOutsideAllowlist(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "External Health",
		URLPath:            "external-health",
		Host:               "example.com",
		Port:               80,
		HealthCheckType:    "http",
		HealthCheckURL:     "http://example.com/health",
		ExpectedHTTPStatus: http.StatusOK,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.RunHealthCheck(id)
	if err != nil {
		t.Fatalf("RunHealthCheck: %v", err)
	}
	if result.Status == "healthy" {
		t.Fatalf("status = %q, want blocked health check failure", result.Status)
	}
	if !strings.Contains(result.FailureReason, "not allowlisted") {
		t.Fatalf("failureReason = %q, want allowlist failure", result.FailureReason)
	}
}

func TestHealthCheckBlocksTCPOutsideAllowlist(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:              id,
		Name:            "External TCP Health",
		URLPath:         "external-tcp-health",
		Host:            "example.com",
		Port:            443,
		HealthCheckType: "tcp",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.RunHealthCheck(id)
	if err != nil {
		t.Fatalf("RunHealthCheck: %v", err)
	}
	if result.Status == "healthy" {
		t.Fatalf("status = %q, want blocked health check failure", result.Status)
	}
	if !strings.Contains(result.FailureReason, "not allowlisted") {
		t.Fatalf("failureReason = %q, want allowlist failure", result.FailureReason)
	}
}

func TestHealthCheckDoesNotFollowHTTPRedirect(t *testing.T) {
	redirectCalled := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCalled = true
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Redirect Health",
		URLPath:            "redirect-health",
		Host:               "localhost",
		Port:               8080,
		HealthCheckType:    "http",
		HealthCheckURL:     server.URL,
		ExpectedHTTPStatus: http.StatusOK,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.RunHealthCheck(id)
	if err != nil {
		t.Fatalf("RunHealthCheck: %v", err)
	}
	if redirectCalled {
		t.Fatalf("redirect target was called; health checks must not follow redirects")
	}
	if result.Status == "healthy" {
		t.Fatalf("status = %q, want failed redirect status", result.Status)
	}
	if !strings.Contains(result.FailureReason, "unexpected HTTP status") {
		t.Fatalf("failureReason = %q, want unexpected HTTP status", result.FailureReason)
	}
}

func TestLaunchDoesNotFollowAPIRedirect(t *testing.T) {
	redirectCalled := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCalled = true
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Redirect API Automation",
		URLPath:            "redirect-api-automation",
		LaunchType:         "api",
		LaunchTarget:       server.URL,
		ExpectedHTTPStatus: http.StatusOK,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if redirectCalled {
		t.Fatalf("redirect target was called; API launch must not follow redirects")
	}
	if result.Status != "failed" || result.ExitCode != http.StatusFound {
		t.Fatalf("status/exit = %q/%d, want failed/%d", result.Status, result.ExitCode, http.StatusFound)
	}
}

func TestLaunchRunsAllowlistedScriptWithoutShell(t *testing.T) {
	dir := t.TempDir()
	target := writeExecutableScriptFixture(t, dir, "ok")
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, filepath.Join(dir, target)))

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Script Automation",
		URLPath:      "script-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: target,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed: %s", result.Status, result.Message)
	}
	if result.Output != "script-ok" {
		t.Fatalf("output = %q, want script-ok", result.Output)
	}
}

func TestLaunchEmergencyStopInterruptsActiveScriptAndKeepsOutcomeIndeterminate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}

	var engaged atomic.Bool
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		if engaged.Load() {
			return true, "operator emergency stop", nil
		}
		return false, "", nil
	}))
	defer restore()

	dir := t.TempDir()
	script := filepath.Join(dir, "blocking.sh")
	started := filepath.Join(dir, "started.txt")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf started > started.txt\nexec sleep 30\n"), 0755); err != nil {
		t.Fatalf("write blocking script: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Interruptible Script",
		URLPath:      "interruptible-script",
		LaunchType:   "script",
		LaunchTarget: filepath.Base(script),
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{})
	resultCh := make(chan *LaunchResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.LaunchTask(id, request)
		resultCh <- result
		errCh <- err
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("script did not start before emergency-stop test deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	engaged.Store(true)

	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatalf("LaunchTask: %v", err)
		}
		if result.Status != "indeterminate" || !strings.Contains(result.Message, "emergency stop interrupted") {
			t.Fatalf("launch result after emergency stop = %#v", result)
		}
		if !containsString(result.AuditEvents, "emergency stop observed during script execution") {
			t.Fatalf("launch audit missing emergency-stop observation: %#v", result.AuditEvents)
		}
		if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "indeterminate" {
			t.Fatalf("persisted launch event = %#v, want indeterminate outcome", repo.launchEvents)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("emergency stop did not interrupt the active script")
	}
}

func TestLaunchCancellationTerminatesScriptBackgroundChild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process-group cancellation is only available on Linux")
	}
	for _, mode := range []string{"emergency-stop", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			var engaged atomic.Bool
			restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
				if engaged.Load() {
					return true, "operator emergency stop", nil
				}
				return false, "", nil
			}))
			defer restore()

			dir := t.TempDir()
			script := filepath.Join(dir, "background-child.sh")
			started := filepath.Join(dir, "started.txt")
			sideEffect := filepath.Join(dir, "side-effect.txt")
			body := "#!/bin/sh\n(sleep 2; printf unexpected > side-effect.txt) &\nprintf started > started.txt\nwait\n"
			if err := os.WriteFile(script, []byte(body), 0755); err != nil {
				t.Fatalf("write background-child script: %v", err)
			}
			t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
			t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
			t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
			if mode == "deadline" {
				t.Setenv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", "1")
			}

			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:           id,
				Name:         "Background Child Cancellation",
				URLPath:      "background-child-cancellation",
				LaunchType:   "script",
				LaunchTarget: filepath.Base(script),
			})
			service := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{})
			type outcome struct {
				result *LaunchResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := service.LaunchTask(id, request)
				done <- outcome{result: result, err: err}
			}()

			startDeadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(startDeadline) {
					t.Fatal("script did not start before cancellation test deadline")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "emergency-stop" {
				engaged.Store(true)
			}

			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("LaunchTask: %v", got.err)
				}
				if got.result.Status != "indeterminate" {
					t.Fatalf("launch status = %q, want indeterminate", got.result.Status)
				}
				if !containsString(got.result.AuditEvents, "script process group termination verified") {
					t.Fatalf("script process-group termination was not verified: %#v", got.result.AuditEvents)
				}
			case <-time.After(4 * time.Second):
				t.Fatalf("%s did not cancel the script and background child", mode)
			}

			// Wait beyond the child's scheduled write: an absent file immediately
			// after cancellation alone would not prove the child could no longer run.
			time.Sleep(2200 * time.Millisecond)
			if _, err := os.Stat(sideEffect); !os.IsNotExist(err) {
				t.Fatalf("background child performed its delayed side effect after cancellation: stat err=%v", err)
			}
		})
	}
}

type deadlineWaitingScriptAuthorizer struct {
	deadline time.Time
}

func (a *deadlineWaitingScriptAuthorizer) AuthorizeAndConsume(ctx context.Context, _ executionauth.Request, _, _ string) (executionauth.Receipt, error) {
	a.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return executionauth.Receipt{}, ctx.Err()
}

func TestLaunchScriptAuthorizationUsesExecutionDeadline(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir := t.TempDir()
	target := writeExecutableScriptFixture(t, dir, "ok")
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, filepath.Join(dir, target)))
	t.Setenv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", "1")
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "script", LaunchTarget: target})
	authorizer := &deadlineWaitingScriptAuthorizer{}
	svc := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(&contextualConfigurationReadProbe{Repository: repo}, events.Publisher{}, agentruntime.DefaultRegistry(), newUnitTestApprovalProofService(), authorizer)
	parent, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request := approvedTaskLaunchRequest(t, svc, id, TaskLaunchRequest{ExecutionContext: parent})
	started := time.Now()
	result, err := svc.LaunchTask(id, request)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("authorization ignored the execution deadline: elapsed=%s", elapsed)
	}
	if authorizer.deadline.IsZero() || authorizer.deadline.After(started.Add(2*time.Second)) {
		t.Fatalf("authorizer received unbounded or parent deadline: %s", authorizer.deadline)
	}
	if errors.Is(parent.Err(), context.DeadlineExceeded) {
		t.Fatal("script authorization ran until the parent deadline")
	}
	if result.Status != "indeterminate" || !containsString(result.AuditEvents, "script authorization deadline elapsed") {
		t.Fatalf("deadline outcome = %#v", result)
	}
}

func TestLaunchScriptBoundsInheritedOutputPipesAndCleansStaging(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	for _, mode := range []string{"parent-exits", "parent-waits"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			staging := t.TempDir()
			t.Setenv("TMPDIR", staging)
			script := filepath.Join(dir, "pipes.sh")
			body := "#!/bin/sh\nprintf '%s' \"$0\" > staged-path.txt\nsleep 30 &\nprintf '%s' \"$!\" > child.pid\n"
			if mode == "parent-waits" {
				body += "wait\n"
			}
			if err := os.WriteFile(script, []byte(body), 0755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
				if err == nil {
					var pid int
					if _, err := fmt.Sscan(string(data), &pid); err == nil && pid > 0 {
						if process, err := os.FindProcess(pid); err == nil {
							_ = process.Kill()
						}
					}
				}
			})
			t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
			t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
			t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
			t.Setenv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", "1")
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "script", LaunchTarget: filepath.Base(script)})
			svc := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, svc, id, TaskLaunchRequest{})
			type outcome struct {
				result *LaunchResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := svc.LaunchTask(id, request)
				done <- outcome{result, err}
			}()
			select {
			case got := <-done:
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.result.Status != "indeterminate" {
					t.Fatalf("held-pipe outcome = %#v", got.result)
				}
				if mode == "parent-exits" && !containsString(got.result.AuditEvents, "script output drain deadline elapsed") {
					t.Fatalf("missing output-drain audit: %#v", got.result.AuditEvents)
				}
				if mode == "parent-waits" && !containsString(got.result.AuditEvents, "script execution deadline elapsed") {
					t.Fatalf("missing execution-deadline audit: %#v", got.result.AuditEvents)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("inherited output pipes prevented bounded return")
			}
			path, err := os.ReadFile(filepath.Join(dir, "staged-path.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
				t.Fatalf("staged executable was not cleaned up: %v", err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil || len(entries) != 0 {
				t.Fatalf("staging directory not empty: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestLaunchEmergencyStopCancelsActiveAPIRequestAsIndeterminate(t *testing.T) {
	var engaged atomic.Bool
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		if engaged.Load() {
			return true, "operator emergency stop", nil
		}
		return false, "", nil
	}))
	defer restore()

	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestCancelled)
	}))
	defer server.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Interruptible API",
		URLPath:            "interruptible-api",
		LaunchType:         "api",
		LaunchTarget:       server.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{})
	type launchOutcome struct {
		result *LaunchResult
		err    error
	}
	resultCh := make(chan launchOutcome, 1)
	go func() {
		result, err := service.LaunchTask(id, request)
		resultCh <- launchOutcome{result: result, err: err}
	}()

	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("API request did not start before emergency-stop test deadline")
	}
	engaged.Store(true)

	select {
	case outcome := <-resultCh:
		if outcome.err != nil {
			t.Fatalf("LaunchTask: %v", outcome.err)
		}
		if outcome.result.Status != "indeterminate" || !strings.Contains(outcome.result.Message, "emergency stop interrupted") {
			t.Fatalf("API launch result after emergency stop = %#v", outcome.result)
		}
		if !containsString(outcome.result.AuditEvents, "emergency stop observed during API request") {
			t.Fatalf("API launch audit missing emergency-stop observation: %#v", outcome.result.AuditEvents)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("emergency stop did not cancel the active API request")
	}
	select {
	case <-requestCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe API request cancellation")
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "indeterminate" {
		t.Fatalf("persisted API launch event = %#v, want indeterminate outcome", repo.launchEvents)
	}
}

func TestLaunchBlocksScriptChangedDuringFinalAuthorization(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "launch.sh")
	marker := filepath.Join(dir, "changed-script-ran.txt")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'reviewed-script'\n"), 0755); err != nil {
		t.Fatalf("WriteFile reviewed script: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Pinned Script Automation",
		URLPath:      "pinned-script-automation",
		LaunchType:   "script",
		LaunchTarget: filepath.Base(script),
	})
	authorizer := &recordingExecutionAuthorizer{
		onCall: func() {
			if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+filepath.Base(marker)+"\n"), 0755); err != nil {
				t.Fatalf("replace script during authorization: %v", err)
			}
		},
	}
	service := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		repo,
		events.Publisher{},
		agentruntime.DefaultRegistry(),
		newUnitTestApprovalProofService(),
		authorizer,
	)

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "blocked" || !strings.Contains(result.Message, "SHA-256 does not match") {
		t.Fatalf("result = %#v, want changed script blocked by the final pin check", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("replacement script executed or stat returned unexpected error: %v", err)
	}
	if authorizer.calls.Load() != 1 {
		t.Fatalf("authorization calls = %d, want one", authorizer.calls.Load())
	}
	if len(repo.launchEvents) != 1 || !containsString(repo.launchEvents[0].AuditEvents, "script hash pin rejected after execution authorization") {
		t.Fatalf("launch audit = %#v, want final pin rejection", repo.launchEvents)
	}
}

func TestStagePinnedScriptExecutesVerifiedCopyAfterAllowlistChanges(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "launch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf reviewed-bytes\n"), 0755); err != nil {
		t.Fatalf("write reviewed script: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))

	staged, cleanup, err := stagePinnedScript(context.Background(), script)
	if err != nil {
		t.Fatalf("stage pinned script: %v", err)
	}
	defer cleanup()
	for _, path := range []string{staged, filepath.Dir(staged)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("private staging permissions for %s: info=%v err=%v", path, info, err)
		}
	}
	command := exec.Command(staged)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	decision, admissionErr, startErr := startScriptWithEmergencyStopAdmission(context.Background(), func() error {
		// Mutate the original at the last possible moment before the OS opens
		// the executable. The staged copy must still be the authorized artifact.
		if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf replacement-bytes\n"), 0755); err != nil {
			t.Fatalf("replace allowlist source: %v", err)
		}
		return command.Start()
	})
	if admissionErr != nil || decision.Active {
		t.Fatalf("script admission decision = %#v, err=%v", decision, admissionErr)
	}
	if startErr != nil {
		t.Fatalf("start staged script: %v", startErr)
	}
	err = command.Wait()
	if err != nil {
		t.Fatalf("execute staged script: %v; output: %s", err, output.String())
	}
	if got := output.String(); got != "reviewed-bytes" {
		t.Fatalf("staged output = %q, want reviewed bytes", got)
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(staged)); !os.IsNotExist(err) {
		t.Fatalf("staged directory survived cleanup: %v", err)
	}
}

func TestScriptPreprocessingRejectsCancelledContext(t *testing.T) {
	for _, mode := range []string{"cancelled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			staging := t.TempDir()
			t.Setenv("TMPDIR", staging)
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				cancel()
			}
			defer cancel()
			// No pin or readable source is needed: cancellation must precede I/O.
			path := filepath.Join(staging, "missing.sh")
			if err := verifyPinnedScript(ctx, path); !errors.Is(err, ctx.Err()) {
				t.Fatalf("verification ignored cancellation: %v", err)
			}
			path, cleanup, err := stagePinnedScript(ctx, path)
			if !errors.Is(err, ctx.Err()) || path != "" || cleanup != nil {
				t.Fatalf("staging ignored cancellation: path=%q cleanup=%v err=%v", path, cleanup != nil, err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil || len(entries) != 0 {
				t.Fatalf("cancelled preprocessing left files: entries=%v err=%v", entries, err)
			}
		})
	}
}

type scriptTestReader func([]byte) (int, error)

func (read scriptTestReader) Read(p []byte) (int, error) { return read(p) }

type scriptTestWriter func([]byte) (int, error)

func (write scriptTestWriter) Write(p []byte) (int, error) { return write(p) }

func TestCopyScriptBytesObservesCancellation(t *testing.T) {
	for _, mode := range []string{"before-read", "during-read", "during-write"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reads, writes int
			source := scriptTestReader(func(p []byte) (int, error) {
				reads++
				if mode == "during-read" {
					cancel()
				}
				return copy(p, "bytes"), nil
			})
			dst := scriptTestWriter(func(p []byte) (int, error) {
				writes++
				cancel()
				return len(p), nil
			})
			if mode == "before-read" {
				cancel()
			}
			_, err := copyScriptBytes(ctx, dst, source)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("copy ignored cancellation: %v", err)
			}
			if mode == "before-read" && reads != 0 || mode != "during-write" && writes != 0 || reads > 1 || writes > 1 {
				t.Fatalf("copy continued after cancellation: reads=%d writes=%d", reads, writes)
			}
		})
	}
}

func TestCopyScriptBytesEnforcesStreamingSizeLimit(t *testing.T) {
	for _, size := range []int64{maxScriptBytes, maxScriptBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source := io.LimitReader(scriptTestReader(func(p []byte) (int, error) {
				clear(p)
				return len(p), nil
			}), size)
			copied, err := copyScriptBytes(context.Background(), io.Discard, source)
			if copied != maxScriptBytes {
				t.Fatalf("copied=%d, want maximum %d", copied, maxScriptBytes)
			}
			if size == maxScriptBytes && err != nil || size > maxScriptBytes && (err == nil || !strings.Contains(err.Error(), "maximum size")) {
				t.Fatalf("stream size %d: err=%v", size, err)
			}
		})
	}
}

func TestScriptPreprocessingSizeAndHashRejectionCleansStaging(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script opening is supported on Linux only")
	}
	for _, mode := range []string{"oversized", "hash-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			staging := t.TempDir()
			t.Setenv("TMPDIR", staging)
			script := filepath.Join(t.TempDir(), "launch.sh")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
			want := "SHA-256 does not match"
			if mode == "oversized" {
				want = "maximum size"
				if err := os.Truncate(script, maxScriptBytes+1); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(script, []byte("unreviewed bytes"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := verifyPinnedScript(context.Background(), script); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("verification did not reject %s: %v", mode, err)
			}
			path, cleanup, err := stagePinnedScript(context.Background(), script)
			if err == nil || !strings.Contains(err.Error(), want) || path != "" || cleanup != nil {
				t.Fatalf("staging did not reject %s: path=%q cleanup=%v err=%v", mode, path, cleanup != nil, err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected staging left files: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestScriptSafeOpenRejectsNonRegularAndSymlinkTargets(t *testing.T) {
	if runtime.GOOS != "linux" {
		file, err := openRegularScript(context.Background(), "unused")
		if file != nil || err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("unsupported platform did not fail closed: file=%v err=%v", file, err)
		}
		return
	}
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.sh")
	if err := os.WriteFile(regular, []byte("reviewed"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.sh")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "mkfifo", fifo).CombinedOutput(); err != nil {
		t.Fatalf("create offline FIFO fixture: %v: %s", err, output)
	}
	for _, path := range []string{dir, os.DevNull, link, fifo} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			started := time.Now()
			file, err := openRegularScript(context.Background(), path)
			if file != nil {
				_ = file.Close()
			}
			if file != nil || err == nil {
				t.Fatalf("unsafe target accepted: file=%v err=%v", file, err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("special-file opening blocked")
			}
		})
	}
}

func TestLaunchBlocksFIFOReplacementDuringScriptAuthorization(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir := t.TempDir()
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	script := filepath.Join(dir, "launch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf executed > executed.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
	authorizer := &recordingExecutionAuthorizer{onCall: func() {
		if err := os.Remove(script); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "mkfifo", script).CombinedOutput(); err != nil {
			t.Fatalf("replace source with offline FIFO fixture: %v: %s", err, output)
		}
	}}
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "script", LaunchTarget: filepath.Base(script)})
	svc := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(repo, events.Publisher{}, agentruntime.DefaultRegistry(), newUnitTestApprovalProofService(), authorizer)
	result, err := svc.LaunchTask(id, approvedTaskLaunchRequest(t, svc, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || !strings.Contains(result.Message, "regular file") || authorizer.calls.Load() != 1 {
		t.Fatalf("FIFO replacement outcome = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "executed.txt")); !os.IsNotExist(err) {
		t.Fatalf("replaced source executed: %v", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("FIFO rejection left staging files: entries=%v err=%v", entries, err)
	}
}

func TestLaunchScriptCancellationDuringAuthorizationDoesNotStageOrStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	dir := t.TempDir()
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	script := filepath.Join(dir, "launch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf executed > executed.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authorizer := &recordingExecutionAuthorizer{onCall: cancel}
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "script", LaunchTarget: filepath.Base(script)})
	svc := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(&contextualConfigurationReadProbe{Repository: repo}, events.Publisher{}, agentruntime.DefaultRegistry(), newUnitTestApprovalProofService(), authorizer)
	result, err := svc.LaunchTask(id, approvedTaskLaunchRequest(t, svc, id, TaskLaunchRequest{ExecutionContext: ctx}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "indeterminate" || !containsString(result.AuditEvents, "script authorization context cancellation observed") || authorizer.calls.Load() != 1 {
		t.Fatalf("authorization cancellation outcome = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "executed.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancelled script executed: %v", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled authorization left staging files: entries=%v err=%v", entries, err)
	}
}

func TestScriptUnsupportedSafeOpenFailsClosed(t *testing.T) {
	dir := t.TempDir()
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	script := filepath.Join(dir, "launch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := openRegularScript(context.Background(), script)
	if file != nil {
		_ = file.Close()
	}
	if runtime.GOOS == "linux" && err == nil {
		t.Skip("normal Linux build uses the supported helper; exercise the fallback with an explicit-file build")
	}
	if file != nil || err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported helper did not fail closed: file=%v err=%v", file, err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, script))
	if err := verifyPinnedScript(context.Background(), script); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported verification accepted the source: %v", err)
	}
	path, cleanup, err := stagePinnedScript(context.Background(), script)
	if path != "" || cleanup != nil || err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported staging accepted the source: path=%q cleanup=%v err=%v", path, cleanup != nil, err)
	}
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "script", LaunchTarget: filepath.Base(script)})
	authorizer := &recordingExecutionAuthorizer{}
	svc := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(repo, events.Publisher{}, agentruntime.DefaultRegistry(), newUnitTestApprovalProofService(), authorizer)
	result, err := svc.LaunchTask(id, approvedTaskLaunchRequest(t, svc, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || !strings.Contains(result.Message, "unsupported") || authorizer.calls.Load() != 0 {
		t.Fatalf("unsupported launch was not rejected before authorization: %#v", result)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported launch left staging files: entries=%v err=%v", entries, err)
	}
}

func TestLaunchBlocksScriptWithoutApprovalBeforeProcessExecution(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "launch.sh")
	marker := filepath.Join(dir, "executed.txt")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch executed.txt\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Unapproved Script Automation",
		URLPath:      "unapproved-script-automation",
		LaunchType:   "script",
		LaunchTarget: "launch.sh",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "unapproved-script-test"})
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("script side-effect marker exists or stat returned unexpected error: %v", err)
	}
	assertLauncherApprovalBlocked(t, result, repo, "action-bound approval proof rejected before process or filesystem access")
}

func TestLaunchRunsScriptWithMinimalEnvironment(t *testing.T) {
	dir := t.TempDir()
	target := writeExecutableScriptFixture(t, dir, "clean-environment")
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, filepath.Join(dir, target)))
	t.Setenv("SECRET_TOKEN", "must-not-leak")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Script Automation",
		URLPath:      "script-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: target,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed: %s", result.Status, result.Message)
	}
	if result.Output != "clean" {
		t.Fatalf("output = %q, want clean", result.Output)
	}
}

func TestLaunchRedactsScriptOutputSecrets(t *testing.T) {
	dir := t.TempDir()
	target := writeExecutableScriptFixture(t, dir, "redact")
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, filepath.Join(dir, target)))

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Script Automation",
		URLPath:      "script-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: target,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed: %s", result.Status, result.Message)
	}
	if strings.Contains(result.Output, "super-secret-token") {
		t.Fatalf("script output leaked secret: %s", result.Output)
	}
	if len(repo.launchEvents) != 1 || strings.Contains(repo.launchEvents[0].Output, "super-secret-token") {
		t.Fatalf("launch event leaked secret: %#v", repo.launchEvents)
	}
}

func TestLaunchRedactsScriptFailureSecrets(t *testing.T) {
	dir := t.TempDir()
	target := writeExecutableScriptFixture(t, dir, "fail-with-secret")
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", scriptPin(t, filepath.Join(dir, target)))

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Failing Script Automation",
		URLPath:      "failing-script-automation",
		LaunchType:   "script",
		LaunchTarget: target,
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if strings.Contains(result.Output, "super-secret-token") || strings.Contains(result.Message, "super-secret-token") {
		t.Fatalf("failed script result leaked a secret: %#v", result)
	}
	if len(repo.launchEvents) != 1 || strings.Contains(repo.launchEvents[0].Output, "super-secret-token") || strings.Contains(repo.launchEvents[0].Message, "super-secret-token") {
		t.Fatalf("launch event leaked a secret: %#v", repo.launchEvents)
	}
}

func writeExecutableScriptFixture(t *testing.T, dir, mode string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("safe local script execution is supported on Linux only")
	}
	t.Setenv("HAI_TEST_SCRIPT_MODE", mode)
	t.Setenv("AUTOMATION_SCRIPT_ENV_ALLOWLIST", "HAI_TEST_SCRIPT_MODE")
	if runtime.GOOS == "windows" {
		source, err := os.Executable()
		if err != nil {
			t.Fatalf("locate test executable: %v", err)
		}
		target := filepath.Join(dir, "hai-script-test-helper.exe")
		payload, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read test executable: %v", err)
		}
		if err := os.WriteFile(target, payload, 0755); err != nil {
			t.Fatalf("write test executable fixture: %v", err)
		}
		return filepath.Base(target)
	}

	target := filepath.Join(dir, "hai-script-test-helper.sh")
	var body string
	switch mode {
	case "ok":
		body = "#!/bin/sh\necho script-ok\n"
	case "clean-environment":
		body = "#!/bin/sh\nif [ -n \"$SECRET_TOKEN\" ]; then echo leaked; else echo clean; fi\n"
	case "redact":
		body = "#!/bin/sh\necho 'token=super-secret-token'\n"
	case "fail-with-secret":
		body = "#!/bin/sh\necho 'token=super-secret-token' >&2\nexit 1\n"
	default:
		t.Fatalf("unsupported script fixture mode %q", mode)
	}
	if err := os.WriteFile(target, []byte(body), 0755); err != nil {
		t.Fatalf("write script fixture: %v", err)
	}
	return filepath.Base(target)
}

func scriptPin(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	sum := sha256.Sum256(contents)
	return filepath.Base(path) + "=" + hex.EncodeToString(sum[:])
}

func TestBoundedOutputCapsCombinedProcessOutput(t *testing.T) {
	output := newBoundedOutput(8)
	if _, err := output.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("67890")); err != nil {
		t.Fatal(err)
	}
	if got := string(output.Bytes()); got != "12345678" {
		t.Fatalf("bounded output = %q, want %q", got, "12345678")
	}
	if !output.Truncated() {
		t.Fatal("overflow was not recorded")
	}
}

func TestLaunchBlocksScriptWhenPolicyDisabled(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "launch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho script-ok\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "false")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Script Automation",
		URLPath:      "script-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: "launch.sh",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !result.RequiresApproval {
		t.Fatalf("expected disabled script execution to require approval")
	}
}

func TestLaunchBlocksScriptOutsideAllowlist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Blocked Script",
		URLPath:      "blocked-script",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: "../outside.sh",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !result.RequiresApproval {
		t.Fatalf("expected blocked script to require approval")
	}
}

func TestLaunchBlocksScriptSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sh")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\necho outside\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available on this platform: %v", err)
	}
	t.Setenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED", "true")
	t.Setenv("AUTOMATION_SCRIPT_DIR", dir)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Blocked Script",
		URLPath:      "blocked-script",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "script",
		LaunchTarget: "link.sh",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want no script output", result.Output)
	}
}

func TestLaunchBlocksDockerWhenPolicyDisabled(t *testing.T) {
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "false")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Docker Automation",
		URLPath:      "docker-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "docker_service",
		LaunchTarget: "container-name",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if len(repo.launchEvents) != 1 {
		t.Fatalf("expected blocked launch to be audited")
	}
}

func TestLaunchBlocksWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Emergency Automation",
		URLPath:      "emergency-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "browser_url",
		LaunchTarget: "http://localhost:8080",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.Launch(id)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !result.RequiresApproval {
		t.Fatalf("emergency stop block should require review")
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "blocked" {
		t.Fatalf("expected blocked launch event, got %#v", repo.launchEvents)
	}
}

func TestLaunchBlocksWhenPersistedEmergencyStopActive(t *testing.T) {
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator paused execution", nil
	}))
	defer restore()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Persisted Stop Automation",
		URLPath:      "persisted-stop-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "browser_url",
		LaunchTarget: "http://localhost:8080",
	})
	service := newTestService(repo, events.Publisher{})
	result, err := service.Launch(id)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" || result.Message != "operator paused execution" {
		t.Fatalf("persisted stop did not block automation launch: %#v", result)
	}
}

func TestAgentRuntimeLaunchRequiresApprovalAndReceivesTask(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "hermes"}
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Hermes Runtime",
		URLPath:      "hermes-runtime",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "agent_runtime",
		RuntimeType:  "hermes",
		LaunchTarget: "runtime://hermes",
	})
	service := newTestServiceWithAuthorizedRuntime(
		t,
		repo,
		events.Publisher{},
		adapter,
	)

	blocked, err := service.LaunchTask(
		id,
		TaskLaunchRequest{
			OwnerIdentity:  "alice",
			IdempotencyKey: "unapproved-runtime-test",
			Task:           "Inspect the project without an approval.",
		},
	)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if blocked.Status != "blocked" || adapter.called {
		t.Fatalf("direct launch bypassed approval: %#v", blocked)
	}
	if len(repo.launchEvents) != 1 ||
		!containsString(
			repo.launchEvents[0].AuditEvents,
			"action-bound approval proof rejected before agent runtime access",
		) {
		t.Fatalf("blocked runtime audit was not persisted: %#v", repo.launchEvents)
	}

	completed, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		Task:       "Inspect the project and report verified completion.",
		ProjectKey: "018-hai",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if completed.Status != "completed" || !adapter.called {
		t.Fatalf("approved task did not run: %#v", completed)
	}
	if !strings.HasPrefix(completed.RuntimeTaskID, "automation:"+id.String()+":intent:") {
		t.Fatalf("runtime task id = %q, want launch-scoped id for automation %s", completed.RuntimeTaskID, id)
	}
	if adapter.task.Prompt != "Inspect the project and report verified completion." || adapter.task.ProjectKey != "018-hai" {
		t.Fatalf("task context was not propagated: %#v", adapter.task)
	}
	if adapter.task.ID != completed.RuntimeTaskID {
		t.Fatalf("adapter task id = %q, want launch result id %s", adapter.task.ID, completed.RuntimeTaskID)
	}
	if len(repo.launchEvents) != 2 || !containsString(repo.launchEvents[1].AuditEvents, "fake runtime executed under approval") {
		t.Fatalf("completed runtime audit was not persisted: %#v", repo.launchEvents)
	}
	if repo.launchEvents[1].RuntimeTaskID != completed.RuntimeTaskID {
		t.Fatalf("launch event runtime task id = %q, want %s", repo.launchEvents[1].RuntimeTaskID, completed.RuntimeTaskID)
	}
}

func TestLaunchStateUpdatePreservesConcurrentAutomationConfigurationEdit(t *testing.T) {
	id := uuid.New()
	adapter := &launchGateAgentRuntimeAdapter{
		fakeAgentRuntimeAdapter: fakeAgentRuntimeAdapter{id: "hermes"},
		started:                 make(chan struct{}),
		resume:                  make(chan struct{}),
	}
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: "Hermes Runtime", URLPath: "hermes-runtime", Host: "localhost", Port: 8080,
		LaunchType: "agent_runtime", RuntimeType: "hermes", LaunchTarget: "runtime://hermes",
	})
	service := newTestServiceWithAuthorizedRuntime(t, repo, events.Publisher{}, adapter)
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{Task: "Run the approved test task."})

	type launchOutcome struct {
		result *LaunchResult
		err    error
	}
	finished := make(chan launchOutcome, 1)
	go func() {
		result, err := service.LaunchTask(id, request)
		finished <- launchOutcome{result: result, err: err}
	}()

	select {
	case <-adapter.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not reach the blocking launch")
	}

	updated := &models.Automation{
		ID: id, Name: "Hermes Runtime", URLPath: "hermes-runtime", Host: "localhost", Port: 8080,
		LaunchType: "agent_runtime", RuntimeType: "hermes", LaunchTarget: "runtime://updated-hermes",
		DependencyNotes: "updated while the runtime launch was in progress",
	}
	if _, err := service.Update(updated); err != nil {
		t.Fatalf("concurrent automation configuration update: %v", err)
	}
	close(adapter.resume)

	select {
	case outcome := <-finished:
		if outcome.err != nil || outcome.result == nil {
			t.Fatalf("LaunchTask: result=%#v err=%v", outcome.result, outcome.err)
		}
		if outcome.result.Status != "completed" {
			t.Fatalf("launch status = %q, want completed", outcome.result.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("launch did not finish after the runtime was resumed")
	}

	persisted, err := repo.FindByID(id)
	if err != nil {
		t.Fatalf("reload automation: %v", err)
	}
	if persisted.LaunchTarget != updated.LaunchTarget || persisted.DependencyNotes != updated.DependencyNotes {
		t.Fatalf("launch-state persistence overwrote the newer configuration: target=%q notes=%q", persisted.LaunchTarget, persisted.DependencyNotes)
	}
	if persisted.LastLaunchAt == nil {
		t.Fatal("launch summary was not recorded")
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "completed" {
		t.Fatalf("launch outcome was not recorded: %#v", repo.launchEvents)
	}
}

func TestStopRuntimeTaskUsesVerifiedOwnerAutomationRuntimeAndTaskID(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw"}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "OpenClaw Runtime",
		URLPath:      "openclaw-runtime",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "agent_runtime",
		RuntimeType:  "openclaw",
		LaunchTarget: "runtime://openclaw",
	})
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	result, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if result.RuntimeID != "openclaw" || result.TaskID != "" || result.Status != "blocked" ||
		!strings.Contains(result.Message, "no runtime task identity was substituted") {
		t.Fatalf("stop result = %#v", result)
	}
	if result.EvidenceURI == "" {
		t.Fatalf("stop result missing evidence URI: %#v", result)
	}
	if len(repo.launchIntents) != 0 {
		t.Fatalf("must not persist a stop intent without an exact task binding: %#v", repo.launchIntents)
	}
	if len(repo.launchEvents) != 1 {
		t.Fatalf("expected runtime stop launch event, got %#v", repo.launchEvents)
	}
	event := repo.launchEvents[0]
	if event.LaunchType != "agent_runtime_stop" || event.RuntimeTaskID != "" || event.Status != "blocked" ||
		event.OwnerIdentity != "alice" {
		t.Fatalf("runtime stop event = %#v", event)
	}
	if result.EvidenceURI != "automation-launch://"+event.ID.String() {
		t.Fatalf("evidence URI = %q, want event %s", result.EvidenceURI, event.ID)
	}
	if !containsString(event.AuditEvents, "runtime stop requested") {
		t.Fatalf("runtime stop audit missing: %#v", event.AuditEvents)
	}
}

func TestStopRuntimeTaskResolvesPendingOwnerBoundLaunchIntent(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw", LaunchTarget: "runtime://openclaw"})
	intentID := uuid.New()
	taskID := automationLaunchRuntimeTaskID(repo.automation, intentID)
	reference := "ocgw:v2:" + intentID.String()
	repo.launchIntents = []models.AutomationLaunchEvent{{ID: intentID, AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime_intent", RuntimeTaskID: taskID, ExecutionReference: reference, Status: "pending"}}
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	result, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if result.TaskID != taskID || result.ExecutionReference != reference || adapter.stopReference != reference || result.Status != "cancellation_requested" {
		t.Fatalf("pending intent was not stopped with its exact binding: result=%#v stopReference=%q", result, adapter.stopReference)
	}
	if len(repo.launchIntents) != 2 || repo.launchIntents[1].LaunchType != "agent_runtime_stop_intent" || repo.launchIntents[1].RuntimeTaskID != taskID || repo.launchIntents[1].ExecutionReference != reference {
		t.Fatalf("stop intent did not preserve task/reference binding: %#v", repo.launchIntents)
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].OwnerIdentity != "alice" || repo.launchEvents[0].RuntimeTaskID != taskID || repo.launchEvents[0].ExecutionReference != reference {
		t.Fatalf("stop outcome did not persist owner/task/reference binding: %#v", repo.launchEvents)
	}
}

func TestStopRuntimeTaskLookupFailureDoesNotMeanNoTaskRunning(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw"}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
	repo.activeLookupErr = fmt.Errorf("database unavailable")
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	result, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if result.Status != "indeterminate" || result.TaskID != "" || result.ExecutionReference != "" || adapter.stopReference != "" || !strings.Contains(result.Message, "absence of a task was not inferred") {
		t.Fatalf("failed lookup was treated as proof there is no running task: %#v stopReference=%q", result, adapter.stopReference)
	}
}

func TestStopRuntimeTaskRetainsPendingIntentBindingAfterOutcomeLookupFailure(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
	repo.activeLookupErr = fmt.Errorf("outcome query unavailable")
	intentID := uuid.New()
	taskID := automationLaunchRuntimeTaskID(repo.automation, intentID)
	reference := "ocgw:v2:" + intentID.String()
	repo.launchIntents = []models.AutomationLaunchEvent{{ID: intentID, AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime_intent", RuntimeTaskID: taskID, ExecutionReference: reference, Status: "pending"}}
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	result, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if result.Status != "cancellation_requested" || result.TaskID != taskID || result.ExecutionReference != reference || adapter.stopReference != reference {
		t.Fatalf("stop did not retain the exact trusted pending-intent binding: result=%#v stopReference=%q", result, adapter.stopReference)
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].OwnerIdentity != "alice" || repo.launchEvents[0].RuntimeTaskID != taskID || repo.launchEvents[0].ExecutionReference != reference {
		t.Fatalf("stop outcome lost its owner/task/reference binding: %#v", repo.launchEvents)
	}
}

func TestStopRuntimeTaskUsesLatestOwnerBoundRunningLaunchReference(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: "OpenClaw Runtime", URLPath: "openclaw-runtime", Host: "localhost", Port: 8080,
		LaunchType: "agent_runtime", RuntimeType: "openclaw", LaunchTarget: "runtime://openclaw",
	})
	launchID := "automation:" + id.String() + ":intent:" + uuid.NewString()
	repo.launchEvents = []models.AutomationLaunchEvent{{
		ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime",
		RuntimeTaskID: launchID, ExecutionReference: "ocgw:v1:YWdlbnQ6bWFpbjpoYWktdGFzaw", Status: "running", StartedAt: time.Now().UTC(),
	}}
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	result, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if result.TaskID != launchID || adapter.stopReference != "ocgw:v1:YWdlbnQ6bWFpbjpoYWktdGFzaw" || result.Status != "cancellation_requested" {
		t.Fatalf("stop result did not target the persisted running launch: result=%#v reference=%q", result, adapter.stopReference)
	}
	if len(repo.launchIntents) != 1 || repo.launchIntents[0].RuntimeTaskID != launchID {
		t.Fatalf("stop intent must record the exact running task: %#v", repo.launchIntents)
	}
}

func TestAgentRuntimeLaunchBlocksOpenClawWithoutMaintenanceAdmission(t *testing.T) {
	testAgentRuntimeLaunchOpenClawModel(t, "")
}

func TestAgentRuntimeLaunchBlocksStoredOpenClawModelWithoutMaintenanceAdmission(t *testing.T) {
	testAgentRuntimeLaunchOpenClawModel(t, "ollama/qwen3:14b")
}

func testAgentRuntimeLaunchOpenClawModel(t *testing.T, model string) {
	t.Helper()
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: model != ""}
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "OpenClaw Runtime",
		RuntimeModel: model,
		URLPath:      "openclaw-runtime",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "agent_runtime",
		RuntimeType:  "openclaw",
		LaunchTarget: "runtime://openclaw",
	})
	service := newTestServiceWithAuthorizedRuntime(
		t,
		repo,
		events.Publisher{},
		adapter,
	)

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		Task:       "Move approved OpenClaw work forward safely.",
		ProjectKey: "018-hai",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "blocked" || adapter.called {
		t.Fatalf("OpenClaw without a concrete maintenance-gated adapter must remain blocked: result=%#v called=%t", result, adapter.called)
	}
	if !strings.Contains(result.Message, "maintenance admission is not configured") ||
		!containsString(result.AuditEvents, "OpenClaw execution rejected because maintenance admission is unavailable") {
		t.Fatalf("missing fail-closed maintenance admission evidence: %#v", result)
	}
	if len(repo.launchIntents) != 1 || repo.launchIntents[0].RuntimeTaskID != result.RuntimeTaskID || repo.launchIntents[0].ExecutionReference != result.ExecutionReference {
		t.Fatalf("blocked launch lost its persisted task binding: intents=%#v result=%#v", repo.launchIntents, result)
	}
	if repo.launchIntents[0].ID == uuid.Nil {
		t.Fatalf("immutable launch intent has no identity: %#v", repo.launchIntents[0])
	}
	if model == "" && result.ExecutionReference != "" {
		t.Fatalf("CLI route received a Gateway-only reference: intent=%#v result=%#v", repo.launchIntents[0], result)
	}
	if model != "" && result.ExecutionReference != "ocgw:v2:"+repo.launchIntents[0].ID.String() {
		t.Fatalf("Gateway reference was not bound to the immutable intent: intent=%#v result=%#v", repo.launchIntents[0], result)
	}
}

func TestAuthorizeAgentRuntimeLaunchBindsStoredOpenClawModel(t *testing.T) {
	const model = "ollama/qwen3:14b"
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "OpenClaw Runtime",
		RuntimeModel: model,
		URLPath:      "openclaw-runtime",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "agent_runtime",
		RuntimeType:  "openclaw",
		LaunchTarget: "runtime://openclaw",
	})
	runtimeService := newTestServiceWithAuthorizedRuntime(t, repo, events.Publisher{}, adapter)
	authorizedService, ok := runtimeService.(*service)
	if !ok {
		t.Fatalf("service has unexpected implementation %T", runtimeService)
	}
	intentID := uuid.New()
	request := approvedTaskLaunchRequest(t, runtimeService, id, TaskLaunchRequest{
		Task:       "Prepare a factual draft without sending it.",
		ProjectKey: "018-hai",
	})
	runtimeTask, _, err := authorizedService.authorizeAgentRuntimeLaunch(
		repo.automation,
		request,
		intentID,
	)
	if err != nil {
		t.Fatalf("authorizeAgentRuntimeLaunch: %v", err)
	}
	if runtimeTask.RuntimeModel != model || runtimeTask.ID != automationLaunchRuntimeTaskID(repo.automation, intentID) ||
		runtimeTask.ExecutionReference != "ocgw:v2:"+intentID.String() || !runtimeTask.HumanApproved {
		t.Fatalf("authorized task lost its stored model or immutable intent binding: %#v", runtimeTask)
	}
}

func TestOwnerStopCancelsAgentRuntimeBlockedDuringLaunchTask(t *testing.T) {
	for _, runtimeID := range []string{"deepseek-harness", "hermes", "odysseus"} {
		t.Run(runtimeID, func(t *testing.T) {
			testOwnerStopCancelsAgentRuntimeBlockedDuringLaunchTask(t, runtimeID)
		})
	}
}

func testOwnerStopCancelsAgentRuntimeBlockedDuringLaunchTask(t *testing.T, runtimeID string) {
	t.Helper()
	id := uuid.New()
	adapter := &blockingAgentRuntimeAdapter{fakeAgentRuntimeAdapter: fakeAgentRuntimeAdapter{id: runtimeID}, started: make(chan agentruntime.Task, 1)}
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: runtimeID + " Runtime", URLPath: runtimeID + "-runtime", Host: "localhost", Port: 8080,
		LaunchType: "agent_runtime", RuntimeType: runtimeID, LaunchTarget: "runtime://" + runtimeID,
	})
	service := newTestServiceWithAuthorizedRuntime(t, repo, events.Publisher{}, adapter)
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{Task: "Perform only the approved read-only task."})
	type launchOutcome struct {
		result *LaunchResult
		err    error
	}
	finished := make(chan launchOutcome, 1)
	go func() {
		result, err := service.LaunchTask(id, request)
		finished <- launchOutcome{result: result, err: err}
	}()

	var launched agentruntime.Task
	select {
	case launched = <-adapter.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime adapter did not reach the blocked LaunchTask call")
	}
	if len(repo.launchIntents) != 1 {
		t.Fatalf("expected one immutable launch intent before dispatch, got %#v", repo.launchIntents)
	}
	wantTaskID := automationLaunchRuntimeTaskID(repo.automation, repo.launchIntents[0].ID)
	if repo.launchIntents[0].RuntimeTaskID != launched.ID || launched.ID != wantTaskID || strings.TrimSpace(launched.ID) == "" {
		t.Fatalf("pending intent does not match dispatched launch-scoped runtime identity: intents=%#v task=%#v wantTaskID=%q", repo.launchIntents, launched, wantTaskID)
	}
	if launched.ID == id.String() {
		t.Fatalf("runtime task ID unexpectedly fell back to automation ID %q", launched.ID)
	}
	unauthorized, err := service.StopRuntimeTaskForOwner(id, "bob")
	if err != nil {
		t.Fatalf("cross-owner stop: %v", err)
	}
	if unauthorized.Status != "blocked" || unauthorized.TaskID != "" || adapter.stopCalls != 0 {
		t.Fatalf("cross-owner stop was not rejected before runtime access: result=%#v stopCalls=%d", unauthorized, adapter.stopCalls)
	}
	select {
	case outcome := <-finished:
		t.Fatalf("cross-owner stop cancelled the in-flight launch: %#v", outcome)
	default:
	}
	stopped, err := service.StopRuntimeTaskForOwner(id, launched.OwnerIdentity)
	if err != nil {
		t.Fatalf("owner stop: %v", err)
	}
	if stopped.Status != "cancellation_requested" || stopped.TaskID != launched.ID || adapter.stopReference != launched.ExecutionReference {
		t.Fatalf("owner stop did not target the exact in-flight launch: stop=%#v adapterRef=%q wantTaskID=%q", stopped, adapter.stopReference, launched.ID)
	}
	select {
	case outcome := <-finished:
		if outcome.err != nil || outcome.result == nil {
			t.Fatalf("LaunchTask: result=%#v err=%v", outcome.result, outcome.err)
		}
		if outcome.result.Status == "completed" || outcome.result.RuntimeTaskID != launched.ID || outcome.result.ExecutionReference != launched.ExecutionReference {
			t.Fatalf("local cancellation was misreported or lost its binding: %#v", outcome.result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("adapter context was not cancelled by owner stop")
	}
}

func TestAgentRuntimeLaunchAllowsDeepSeekHarness(t *testing.T) {
	id := uuid.New()
	adapter := &fakeAgentRuntimeAdapter{id: "deepseek-harness"}
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "DeepSeek Harness Runtime",
		URLPath:      "deepseek-harness-runtime",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "agent_runtime",
		RuntimeType:  "deepseek-harness",
		LaunchTarget: "runtime://deepseek-harness",
	})
	service := newTestServiceWithAuthorizedRuntime(t, repo, events.Publisher{}, adapter)

	completed, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		Task:       "Inspect the approved workspace and report evidence.",
		ProjectKey: "018-hai",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if completed.Status != "completed" || !adapter.called {
		t.Fatalf("approved DeepSeek Harness task did not run: %#v", completed)
	}
	if adapter.task.Prompt != "Inspect the approved workspace and report evidence." || adapter.task.ProjectKey != "018-hai" {
		t.Fatalf("task context was not propagated: %#v", adapter.task)
	}
}

func TestLaunchBlocksDockerWithoutApprovalBeforeSocketAccess(t *testing.T) {
	socketPath, calls := startDockerTestServer(t)
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "safe-container")
	t.Setenv("AUTOMATION_DOCKER_SOCKET", socketPath)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Unapproved Docker Automation",
		URLPath:      "unapproved-docker-automation",
		LaunchType:   "docker_service",
		LaunchTarget: "safe-container",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "unapproved-docker-test"})
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("Docker API received %d calls without approval, want zero", calls.Load())
	}
	assertLauncherApprovalBlocked(t, result, repo, "action-bound approval proof rejected before docker socket access")
}

func TestLaunchAllowsDockerWithApproval(t *testing.T) {
	socketPath, calls := startDockerTestServer(t)
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "safe-container")
	t.Setenv("AUTOMATION_DOCKER_SOCKET", socketPath)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Approved Docker Automation",
		URLPath:      "approved-docker-automation",
		LaunchType:   "docker_service",
		LaunchTarget: "safe-container",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Docker API received %d calls, want one", calls.Load())
	}
	if result.Status != "completed" || result.ExitCode != http.StatusNoContent {
		t.Fatalf("approved Docker result = %#v, want completed HTTP %d", result, http.StatusNoContent)
	}
	if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "completed" {
		t.Fatalf("approved Docker launch was not audited: %#v", repo.launchEvents)
	}
}

func TestLaunchRedactsDockerFailureOutput(t *testing.T) {
	socketPath, calls := startDockerTestServerWithResponse(t, http.StatusInternalServerError, "token=super-secret-token")
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "safe-container")
	t.Setenv("AUTOMATION_DOCKER_SOCKET", socketPath)

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Docker failure redaction",
		URLPath:      "docker-failure-redaction",
		LaunchType:   "docker_service",
		LaunchTarget: "safe-container",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{OwnerIdentity: "alice"}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if calls.Load() != 1 || result.Status != "failed" {
		t.Fatalf("Docker launch result = %#v calls=%d, want one failed launch", result, calls.Load())
	}
	if strings.Contains(result.Output, "super-secret-token") {
		t.Fatalf("Docker failure output leaked a secret: %#v", result)
	}
	if len(repo.launchEvents) != 1 || strings.Contains(repo.launchEvents[0].Output, "super-secret-token") {
		t.Fatalf("Docker launch event leaked a secret: %#v", repo.launchEvents)
	}
}

func TestLaunchBlocksDockerWhenContainerNotAllowlisted(t *testing.T) {
	t.Setenv("AUTOMATION_DOCKER_CONTROL_ENABLED", "true")
	t.Setenv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "safe-container")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Docker Automation",
		URLPath:      "docker-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "docker_service",
		LaunchTarget: "dangerous-container",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !result.RequiresApproval {
		t.Fatalf("expected blocked docker launch to require approval")
	}
}

func TestLaunchBlocksAPITargetOutsideAllowlist(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "External API Automation",
		URLPath:      "external-api-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "api",
		LaunchTarget: "POST https://example.com/start",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !result.RequiresApproval {
		t.Fatalf("expected blocked API launch to require approval")
	}
}

func TestLaunchBlocksAPILinkLocalTarget(t *testing.T) {
	t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "169.254.169.254")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Metadata API Automation",
		URLPath:      "metadata-api-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "api",
		LaunchTarget: "POST http://169.254.169.254/latest/meta-data",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
}

func TestLaunchRedactsAPITargetAndResponseSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("password=hunter2"))
	}))
	defer server.Close()
	t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "127.0.0.1")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "API Automation",
		URLPath:      "api-automation",
		Host:         "localhost",
		Port:         8080,
		LaunchType:   "api",
		LaunchTarget: "POST " + server.URL + "/start?token=super-secret-token",
	})
	service := newTestService(repo, events.Publisher{})

	result, err := service.LaunchTask(id, approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{}))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	for _, value := range []string{result.Target, result.Message, result.Output, repo.launchEvents[0].Target, repo.launchEvents[0].Message, repo.launchEvents[0].Output} {
		if strings.Contains(value, "super-secret-token") || strings.Contains(value, "hunter2") {
			t.Fatalf("launch leaked secret in %q", value)
		}
	}
}

func assertLauncherApprovalBlocked(
	t *testing.T,
	result *LaunchResult,
	repo *fakeAutomationRepo,
	expectedAudit string,
) {
	t.Helper()
	if result == nil || result.Status != "blocked" || !result.RequiresApproval {
		t.Fatalf("launcher result = %#v, want blocked and approval-required", result)
	}
	if result.Output != "" {
		t.Fatalf("blocked launch output = %q, want empty", result.Output)
	}
	if len(repo.launchEvents) != 1 {
		t.Fatalf("blocked launch event count = %d, want one", len(repo.launchEvents))
	}
	event := repo.launchEvents[0]
	if event.Status != "blocked" || !containsString(event.AuditEvents, expectedAudit) {
		t.Fatalf("blocked launch audit = %#v, want %q", event, expectedAudit)
	}
	if result.LaunchEventID == uuid.Nil || result.LaunchEventID != event.ID {
		t.Fatalf("blocked launch event id = %s, want %s", result.LaunchEventID, event.ID)
	}
}

func approvedTaskLaunchRequest(
	t *testing.T,
	service Service,
	id uuid.UUID,
	request TaskLaunchRequest,
) TaskLaunchRequest {
	t.Helper()
	if strings.TrimSpace(request.OwnerIdentity) == "" {
		request.OwnerIdentity = "alice"
	}
	if strings.TrimSpace(request.ApprovalSourceID) == "" {
		request.ApprovalSourceID = "task-review:" + uuid.NewString()
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		request.IdempotencyKey = "test-launch:" + request.ApprovalSourceID
	}
	recorder, ok := service.(ApprovalDecisionRecorder)
	if !ok {
		t.Fatalf("automation service does not expose the trusted approval decision recorder")
	}
	inspector, ok := service.(ReviewConfigurationInspector)
	if !ok {
		t.Fatal("automation service does not expose the pre-review configuration inspector")
	}
	snapshot, err := inspector.InspectReviewConfiguration(id)
	if err != nil {
		t.Fatalf("InspectReviewConfiguration before approval: %v", err)
	}
	reviewPayload, err := json.Marshal(struct {
		Request       TaskLaunchRequest
		Configuration *ReviewConfigurationSnapshot
	}{request, snapshot})
	if err != nil {
		t.Fatalf("encode test review request: %v", err)
	}
	reviewDigest := sha256.Sum256(reviewPayload)
	if err := recorder.RecordApprovalDecision(id, TaskApprovalDecisionRequest{
		OwnerIdentity:         request.OwnerIdentity,
		Task:                  request.Task,
		ProjectKey:            request.ProjectKey,
		MandateID:             request.MandateID,
		ApprovalSourceID:      request.ApprovalSourceID,
		ApprovalBindingDigest: hex.EncodeToString(reviewDigest[:]),
		ReviewConfiguration:   snapshot,
		ApprovedAt:            time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordApprovalDecision: %v", err)
	}
	issuer, ok := service.(ApprovalProofIssuer)
	if !ok {
		t.Fatalf("automation service does not expose the trusted approval proof issuer")
	}
	proof, err := issuer.IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity:    request.OwnerIdentity,
		Task:             request.Task,
		ProjectKey:       request.ProjectKey,
		MandateID:        request.MandateID,
		ApprovalSourceID: request.ApprovalSourceID,
	})
	if err != nil {
		t.Fatalf("IssueApprovalProof: %v", err)
	}
	request.ApprovalProof = proof
	request.ApprovalBindingDigest = proof.ActionDigest
	return request
}

func startDockerTestServer(t *testing.T) (string, *atomic.Int32) {
	return startDockerTestServerWithResponse(t, http.StatusNoContent, "")
}

func startDockerTestServerWithResponse(t *testing.T, status int, body string) (string, *atomic.Int32) {
	t.Helper()
	socketPath := filepath.Join(os.TempDir(), "hai-"+uuid.NewString()+".sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("Unix sockets are unavailable on this platform: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	calls := &atomic.Int32{}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})
	return socketPath, calls
}

type fakeAutomationRepo struct {
	automation          *models.Automation
	launchIntents       []models.AutomationLaunchEvent
	launchEvents        []models.AutomationLaunchEvent
	launchEventsMu      sync.RWMutex
	healthEvents        []models.AutomationHealthEvent
	approvalDecisions   map[string]ApprovalDecisionRecord
	saveIntentErr       error
	saveLaunchErr       error
	saveLaunchErrOnCall int
	saveLaunchCalls     int
	activeLookupErr     error
	pendingLookupErr    error
	updateErr           error
	updateErrOnCall     int
	updateCalls         int
}

type fakeAgentRuntimeAdapter struct {
	id                string
	called            bool
	task              agentruntime.Task
	stopReference     string
	stopCalls         int
	gatewayDelegation bool
}

func (a *fakeAgentRuntimeAdapter) OpenClawGatewayDelegationReady() bool {
	return strings.EqualFold(strings.TrimSpace(a.id), "openclaw") && a.gatewayDelegation
}

func (a *fakeAgentRuntimeAdapter) Info() agentruntime.Info {
	id := firstNonEmptyString(a.id, "hermes")
	return agentruntime.Info{
		ID:               id,
		Name:             id,
		Enabled:          true,
		Configured:       true,
		ExecutionEnabled: true,
		RequiresApproval: true,
	}
}

func (a *fakeAgentRuntimeAdapter) HealthCheck(context.Context) agentruntime.Health {
	return agentruntime.Health{RuntimeID: firstNonEmptyString(a.id, "hermes"), Status: "ready"}
}

func (a *fakeAgentRuntimeAdapter) ListSkills(context.Context) []agentruntime.Skill {
	id := firstNonEmptyString(a.id, "hermes")
	return []agentruntime.Skill{{
		ID:               id + ":skill:test",
		RuntimeID:        id,
		Name:             "test",
		Category:         "skill",
		RiskLevel:        "low",
		ApprovalRequired: false,
		ExecutionMode:    "test",
	}}
}

func (a *fakeAgentRuntimeAdapter) ExecuteTask(_ context.Context, task agentruntime.Task) agentruntime.Result {
	a.called = true
	a.task = task
	return agentruntime.Result{
		RuntimeID: firstNonEmptyString(a.id, "hermes"),
		Status:    "completed",
		Output:    "verified runtime output",
		RouteTrace: &agentruntime.RouteTrace{
			RuntimeID:         firstNonEmptyString(a.id, "hermes"),
			Intent:            "software engineering and repository workflow",
			ExecutionMode:     "read-only planning plus approved low-risk local actions",
			RiskLevel:         "medium",
			RecommendedSkills: []string{"autoreview", "gitcrawl"},
			VisibleProviders:  []string{"ollama"},
			VisibleTools:      []string{"browser"},
			BlockedSurfaces:   []string{"outbound message sending"},
		},
		AuditEvents: []string{"fake runtime executed under approval"},
	}
}

func (a *fakeAgentRuntimeAdapter) StopTask(_ context.Context, taskID string) agentruntime.StopResult {
	return agentruntime.StopResult{RuntimeID: firstNonEmptyString(a.id, "hermes"), TaskID: taskID, Status: "stopped"}
}

func (a *fakeAgentRuntimeAdapter) StopTaskWithReference(_ context.Context, taskID, owner, reference string) agentruntime.StopResult {
	a.stopCalls++
	a.stopReference = reference
	return agentruntime.StopResult{RuntimeID: firstNonEmptyString(a.id, "hermes"), TaskID: taskID, ExecutionReference: reference, Status: "cancellation_requested"}
}

type blockingAgentRuntimeAdapter struct {
	fakeAgentRuntimeAdapter
	started chan agentruntime.Task
}

type launchGateAgentRuntimeAdapter struct {
	fakeAgentRuntimeAdapter
	started chan struct{}
	resume  chan struct{}
}

func (a *launchGateAgentRuntimeAdapter) ExecuteTask(ctx context.Context, task agentruntime.Task) agentruntime.Result {
	a.called = true
	a.task = task
	close(a.started)
	select {
	case <-a.resume:
		return agentruntime.Result{RuntimeID: firstNonEmptyString(a.id, "hermes"), ExecutionReference: task.ExecutionReference, Status: "completed", Output: "runtime completed"}
	case <-ctx.Done():
		return agentruntime.Result{RuntimeID: firstNonEmptyString(a.id, "hermes"), ExecutionReference: task.ExecutionReference, Status: "indeterminate", Message: "launch context was cancelled"}
	}
}

func (a *blockingAgentRuntimeAdapter) ExecuteTask(ctx context.Context, task agentruntime.Task) agentruntime.Result {
	a.called = true
	a.task = task
	a.started <- task
	<-ctx.Done()
	return agentruntime.Result{RuntimeID: firstNonEmptyString(a.id, "hermes"), ExecutionReference: task.ExecutionReference, Status: "indeterminate", Message: "local execution context was cancelled; remote status remains unverified"}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsAuditFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func newFakeAutomationRepo(automation *models.Automation) *fakeAutomationRepo {
	return &fakeAutomationRepo{
		automation:        automation,
		approvalDecisions: map[string]ApprovalDecisionRecord{},
	}
}

func (r *fakeAutomationRepo) FindByID(id uuid.UUID) (*models.Automation, error) {
	if r.automation.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *r.automation
	return &copied, nil
}

func (r *fakeAutomationRepo) Create(automation *models.Automation) (*models.Automation, error) {
	r.automation = automation
	return automation, nil
}

func (r *fakeAutomationRepo) Update(automation *models.Automation) (*models.Automation, error) {
	r.launchEventsMu.Lock()
	defer r.launchEventsMu.Unlock()
	r.updateCalls++
	if r.updateErr != nil && (r.updateErrOnCall == 0 || r.updateErrOnCall == r.updateCalls) {
		return nil, r.updateErr
	}
	r.automation = automation
	return automation, nil
}

func (r *fakeAutomationRepo) UpdateRuntimeStopFailure(id uuid.UUID, startedAt time.Time, reason string) error {
	r.launchEventsMu.Lock()
	defer r.launchEventsMu.Unlock()
	r.updateCalls++
	if r.updateErr != nil && (r.updateErrOnCall == 0 || r.updateErrOnCall == r.updateCalls) {
		return r.updateErr
	}
	if r.automation == nil || r.automation.ID != id || startedAt.IsZero() {
		return gorm.ErrRecordNotFound
	}
	if r.automation.LastLaunchAt == nil || !startedAt.Before(*r.automation.LastLaunchAt) {
		r.automation.LastFailureReason = reason
	}
	return nil
}

func (r *fakeAutomationRepo) UpdateLaunchState(id uuid.UUID, startedAt time.Time, failureReason *string) error {
	r.launchEventsMu.Lock()
	defer r.launchEventsMu.Unlock()
	r.updateCalls++
	if r.updateErr != nil && (r.updateErrOnCall == 0 || r.updateErrOnCall == r.updateCalls) {
		return r.updateErr
	}
	if r.automation == nil || r.automation.ID != id {
		return gorm.ErrRecordNotFound
	}
	if r.automation.LastLaunchAt != nil && startedAt.Before(*r.automation.LastLaunchAt) {
		return nil
	}
	launchAt := startedAt
	r.automation.LastLaunchAt = &launchAt
	if failureReason != nil {
		r.automation.LastFailureReason = *failureReason
	}
	return nil
}

func (r *fakeAutomationRepo) Delete(id uuid.UUID) error {
	return nil
}

func (r *fakeAutomationRepo) FindAll() ([]*models.Automation, error) {
	return []*models.Automation{r.automation}, nil
}

func (r *fakeAutomationRepo) MaxPosition() (int, error) {
	return 0, nil
}

func (r *fakeAutomationRepo) GetByURLPath(urlPath string) (*models.Automation, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeAutomationRepo) Transaction(txFunc func(tx *gorm.DB) error) (err error) {
	return nil
}

func (r *fakeAutomationRepo) SaveHealthEvent(event *models.AutomationHealthEvent) error {
	r.healthEvents = append(r.healthEvents, *event)
	return nil
}

func (r *fakeAutomationRepo) FindHealthEvents(automationID uuid.UUID, limit int) ([]models.AutomationHealthEvent, error) {
	return r.healthEvents, nil
}

func (r *fakeAutomationRepo) SaveLaunchEvent(event *models.AutomationLaunchEvent) error {
	r.launchEventsMu.Lock()
	r.saveLaunchCalls++
	call := r.saveLaunchCalls
	r.launchEventsMu.Unlock()
	if r.saveLaunchErr != nil && (r.saveLaunchErrOnCall == 0 || r.saveLaunchErrOnCall == call) {
		return r.saveLaunchErr
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	r.launchEventsMu.Lock()
	defer r.launchEventsMu.Unlock()
	if key := strings.TrimSpace(event.EventKey); key != "" {
		for _, existing := range r.launchEvents {
			if existing.EventKey == key {
				return nil
			}
		}
	}
	r.launchEvents = append(r.launchEvents, *event)
	return nil
}

func (r *fakeAutomationRepo) SaveLaunchIntent(event *models.AutomationLaunchEvent) error {
	if r.saveIntentErr != nil {
		return r.saveIntentErr
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	r.launchEventsMu.Lock()
	defer r.launchEventsMu.Unlock()
	if key := strings.TrimSpace(event.EventKey); key != "" {
		for _, existing := range r.launchIntents {
			if existing.EventKey == key {
				return errLaunchIntentAlreadyExists
			}
		}
	}
	r.launchIntents = append(r.launchIntents, *event)
	return nil
}

func (r *fakeAutomationRepo) FindLaunchIntentByEventKey(eventKey string) (*models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchIntents) - 1; index >= 0; index-- {
		intent := r.launchIntents[index]
		if intent.EventKey == eventKey {
			return &intent, nil
		}
	}
	return nil, nil
}

func (r *fakeAutomationRepo) FindLaunchOutcomeByIntentID(intentID uuid.UUID) (*models.AutomationLaunchEvent, error) {
	keys := map[string]bool{
		automationLaunchOutcomeEventKey(intentID): true,
		runtimeStopOutcomeEventKey(intentID):      true,
	}
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	var directOutcome *models.AutomationLaunchEvent
	for index := len(r.launchEvents) - 1; index >= 0; index-- {
		outcome := r.launchEvents[index]
		if keys[outcome.EventKey] {
			directOutcome = &outcome
			break
		}
	}
	var intent *models.AutomationLaunchEvent
	for index := len(r.launchIntents) - 1; index >= 0; index-- {
		if r.launchIntents[index].ID == intentID {
			candidate := r.launchIntents[index]
			intent = &candidate
			break
		}
	}
	if intent == nil || intent.LaunchType != "agent_runtime_intent" {
		return directOutcome, nil
	}
	for index := len(r.launchEvents) - 1; index >= 0; index-- {
		outcome := r.launchEvents[index]
		if outcome.AutomationID == intent.AutomationID && outcome.OwnerIdentity == intent.OwnerIdentity &&
			strings.EqualFold(outcome.RuntimeType, intent.RuntimeType) && outcome.RuntimeTaskID == intent.RuntimeTaskID &&
			(outcome.LaunchType == "agent_runtime_openclaw_terminal" || outcome.LaunchType == "agent_runtime_host_completion") {
			return &outcome, nil
		}
	}
	return directOutcome, nil
}

func TestStopRuntimeTaskRetryDoesNotRepeatCancellationWhenOutcomePersistenceFails(t *testing.T) {
	id := uuid.New()
	taskID := "automation:" + id.String() + ":intent:" + uuid.NewString()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
	repo.launchEvents = []models.AutomationLaunchEvent{{
		ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime",
		RuntimeTaskID: taskID, ExecutionReference: "ocgw:v2:" + uuid.NewString(), Status: "running", StartedAt: time.Now().UTC(),
	}}
	repo.saveLaunchErr = fmt.Errorf("outcome store unavailable")
	repo.saveLaunchErrOnCall = 1
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)

	first, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("first StopRuntimeTaskForOwner: %v", err)
	}
	if first.Status != "indeterminate" || adapter.stopCalls != 1 || len(repo.launchIntents) != 1 {
		t.Fatalf("first stop should execute once and retain its intent: result=%#v stopCalls=%d intents=%#v", first, adapter.stopCalls, repo.launchIntents)
	}
	second, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("retry StopRuntimeTaskForOwner: %v", err)
	}
	if second.Status != "indeterminate" || adapter.stopCalls != 1 || len(repo.launchIntents) != 1 {
		t.Fatalf("retry repeated cancellation or created another intent: result=%#v stopCalls=%d intents=%#v", second, adapter.stopCalls, repo.launchIntents)
	}
	if second.EvidenceURI != "automation-launch://"+repo.launchIntents[0].ID.String() {
		t.Fatalf("retry must point to the unresolved immutable intent, got %q", second.EvidenceURI)
	}
}

func TestStopRuntimeTaskReplayReturnsPersistedOutcomeWithoutRepeatingCancellation(t *testing.T) {
	id := uuid.New()
	taskID := "automation:" + id.String() + ":intent:" + uuid.NewString()
	adapter := &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}
	registry := agentruntime.NewRegistry(adapter)
	repo := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
	repo.launchEvents = []models.AutomationLaunchEvent{{
		ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime",
		RuntimeTaskID: taskID, ExecutionReference: "ocgw:v2:" + uuid.NewString(), Status: "running", StartedAt: time.Now().UTC(),
	}}
	service := newTestServiceWithRuntimeRegistry(repo, events.Publisher{}, registry)
	first, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil || first.Status != "cancellation_requested" {
		t.Fatalf("first stop = %#v, err=%v", first, err)
	}
	second, err := service.StopRuntimeTaskForOwner(id, "alice")
	if err != nil {
		t.Fatalf("replayed StopRuntimeTaskForOwner: %v", err)
	}
	if second.Status != first.Status || second.EvidenceURI != first.EvidenceURI || adapter.stopCalls != 1 || len(repo.launchIntents) != 1 {
		t.Fatalf("persisted stop outcome was not replayed exactly once: first=%#v second=%#v stopCalls=%d intents=%#v", first, second, adapter.stopCalls, repo.launchIntents)
	}
}

func TestDiagnosticsForOwnerOnlyReturnsThatOwnersLaunchOutput(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{ID: id, Name: "Shared automation"})
	repo.launchEvents = []models.AutomationLaunchEvent{
		{ID: uuid.New(), AutomationID: id, OwnerIdentity: "alice", LaunchType: "script", Output: "alice private output"},
		{ID: uuid.New(), AutomationID: id, OwnerIdentity: "bob", LaunchType: "script", Output: "bob private output"},
	}
	service := NewService(repo, events.Publisher{})

	result, err := service.DiagnosticsForOwner(id, "alice")
	if err != nil {
		t.Fatalf("DiagnosticsForOwner: %v", err)
	}
	if len(result.RecentLaunches) != 1 || result.RecentLaunches[0].OwnerIdentity != "alice" || result.RecentLaunches[0].Output != "alice private output" {
		t.Fatalf("diagnostics exposed another owner's launch output: %#v", result.RecentLaunches)
	}

	legacy, err := service.Diagnostics(id)
	if err != nil {
		t.Fatalf("unscoped Diagnostics: %v", err)
	}
	if len(legacy.RecentLaunches) != 0 {
		t.Fatalf("unscoped diagnostics must fail closed on private launch history: %#v", legacy.RecentLaunches)
	}
}

func (r *fakeAutomationRepo) FindLaunchEvents(automationID uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	return append([]models.AutomationLaunchEvent(nil), r.launchEvents...), nil
}

func (r *fakeAutomationRepo) FindOwnerLaunchEvents(automationID uuid.UUID, owner string, limit int) ([]models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	var matches []models.AutomationLaunchEvent
	for _, event := range r.launchEvents {
		if event.AutomationID == automationID && event.OwnerIdentity == owner && event.LaunchType != "approval_decision" && !strings.HasSuffix(event.LaunchType, "_intent") {
			matches = append(matches, event)
		}
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[len(matches)-limit:]
	}
	return matches, nil
}

func (r *fakeAutomationRepo) FindOwnerActiveRuntimeLaunch(automationID uuid.UUID, runtimeID, owner string) (*models.AutomationLaunchEvent, error) {
	if r.activeLookupErr != nil {
		return nil, r.activeLookupErr
	}
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchEvents) - 1; index >= 0; index-- {
		event := r.launchEvents[index]
		if event.AutomationID == automationID && event.OwnerIdentity == owner && strings.EqualFold(event.RuntimeType, runtimeID) && event.LaunchType == "agent_runtime" && activeRuntimeLaunchEventStatus(&event, runtimeID) {
			terminal := false
			for _, candidate := range r.launchEvents {
				if candidate.AutomationID == event.AutomationID && candidate.OwnerIdentity == event.OwnerIdentity &&
					strings.EqualFold(candidate.RuntimeType, event.RuntimeType) && candidate.RuntimeTaskID == event.RuntimeTaskID &&
					(candidate.LaunchType == "agent_runtime_openclaw_terminal" || candidate.LaunchType == "agent_runtime_host_completion") {
					terminal = true
					break
				}
			}
			if terminal {
				continue
			}
			return &event, nil
		}
	}
	return nil, nil
}

func (r *fakeAutomationRepo) FindPendingRuntimeLaunchIntent(automationID uuid.UUID, owner string) (*models.AutomationLaunchEvent, error) {
	if r.pendingLookupErr != nil {
		return nil, r.pendingLookupErr
	}
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchIntents) - 1; index >= 0; index-- {
		intent := r.launchIntents[index]
		if intent.AutomationID != automationID || intent.OwnerIdentity != owner || intent.LaunchType != "agent_runtime_intent" || intent.Status != "pending" || strings.TrimSpace(intent.RuntimeTaskID) == "" {
			continue
		}
		matched := false
		for _, event := range r.launchEvents {
			if event.AutomationID == intent.AutomationID && event.OwnerIdentity == intent.OwnerIdentity && event.RuntimeType == intent.RuntimeType && event.LaunchType == "agent_runtime" && event.RuntimeTaskID == intent.RuntimeTaskID {
				matched = true
				break
			}
		}
		if !matched {
			return &intent, nil
		}
	}
	return nil, nil
}

func (r *fakeAutomationRepo) FindUnresolvedRuntimeStopIntent(automationID uuid.UUID, owner, taskID string) (*models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchIntents) - 1; index >= 0; index-- {
		intent := r.launchIntents[index]
		if intent.AutomationID != automationID || intent.OwnerIdentity != owner || intent.RuntimeTaskID != taskID || intent.LaunchType != "agent_runtime_stop_intent" || intent.Status != "pending" {
			continue
		}
		matched := false
		for _, outcome := range r.launchEvents {
			if outcome.AutomationID == intent.AutomationID && outcome.OwnerIdentity == intent.OwnerIdentity && outcome.RuntimeTaskID == intent.RuntimeTaskID && outcome.LaunchType == "agent_runtime_stop" && outcome.EventKey == runtimeStopOutcomeEventKey(intent.ID) {
				matched = true
				break
			}
		}
		if !matched {
			return &intent, nil
		}
	}
	return nil, nil
}

func (r *fakeAutomationRepo) FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchEvents) - 1; index >= 0; index-- {
		event := r.launchEvents[index]
		if event.ExecutionReference == reference {
			return &event, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeAutomationRepo) FindLaunchIntentByExecutionReference(reference string) (*models.AutomationLaunchEvent, error) {
	r.launchEventsMu.RLock()
	defer r.launchEventsMu.RUnlock()
	for index := len(r.launchIntents) - 1; index >= 0; index-- {
		intent := r.launchIntents[index]
		if intent.ExecutionReference == reference {
			return &intent, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeAutomationRepo) SaveApprovalDecision(record *ApprovalDecisionRecord) error {
	if err := validateApprovalDecisionRecord(record); err != nil {
		return err
	}
	if existing, ok := r.approvalDecisions[record.SourceID]; ok {
		if sameApprovalDecision(&existing, record) {
			return nil
		}
		return fmt.Errorf("approval decision conflicts with the recorded action binding")
	}
	r.approvalDecisions[record.SourceID] = *record
	return nil
}

func (r *fakeAutomationRepo) FindApprovalDecision(sourceID string) (*ApprovalDecisionRecord, error) {
	record, ok := r.approvalDecisions[sourceID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return &record, nil
}

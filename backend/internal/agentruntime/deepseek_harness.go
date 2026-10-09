package agentruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/pathsafety"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const hostCancellationTimeout = 10 * time.Second

// Native DSH execution stays unavailable until the Windows worker provides
// verified server authentication and an OS-enforced least-privilege sandbox.
// Approval and Job Objects do not establish either boundary.
const dshExecutionIsolationUnavailable = "DeepSeek Harness execution is blocked: TLS peer pinning, a verified OS-enforced least-privilege sandbox, and an acknowledged server-to-Windows process-start protocol that orders Resume against emergency stop are not implemented"

// deepSeekHarnessAdapter invokes only the upstream documented headless profile.
// DeepSeek still labels the harness a developer preview, so execution remains
// separately opt-in and stays behind HAI's final-effect proof boundary.
type deepSeekHarnessAdapter struct {
	enabled              bool
	executionEnabled     bool
	executable           string
	expectedVersion      string
	versionProbe         func(context.Context) (string, error)
	workspace            string
	workspaceRoot        string
	stateDir             string
	timeout              time.Duration
	outputLimit          int64
	envAllow             []string
	executionGate        chan struct{}
	executionGateOnce    sync.Once
	workspaceKey         string
	dispatcher           hostruntime.Dispatcher
	hostDispatchEnabled  bool
	executionBlockReason string
	processStarter       func(*exec.Cmd) error

	// allowDirectExecutionForTest is intentionally unset in production. The
	// container never directly launches DSH; it dispatches a durably approved
	// job to the configured Windows host bridge instead.
	allowDirectExecutionForTest bool
}

func (*deepSeekHarnessAdapter) RuntimeID() string { return "deepseek-harness" }

func newDeepSeekHarnessAdapterFromEnv(dispatchers ...hostruntime.Dispatcher) *deepSeekHarnessAdapter {
	var dispatcher hostruntime.Dispatcher
	for _, candidate := range dispatchers {
		if candidate != nil {
			dispatcher = candidate
			break
		}
	}
	return &deepSeekHarnessAdapter{
		enabled:              envEnabled("DEEPSEEK_HARNESS_ENABLED"),
		executionEnabled:     envEnabled("DEEPSEEK_HARNESS_EXECUTION_ENABLED"),
		executable:           firstNonEmpty(os.Getenv("DEEPSEEK_HARNESS_EXECUTABLE"), "dsh"),
		expectedVersion:      strings.TrimSpace(os.Getenv("DEEPSEEK_HARNESS_VERSION")),
		workspace:            strings.TrimSpace(os.Getenv("DEEPSEEK_HARNESS_WORKSPACE")),
		workspaceRoot:        strings.TrimSpace(os.Getenv("AGENT_RUNTIME_WORKSPACE_ROOT")),
		stateDir:             strings.TrimSpace(os.Getenv("DEEPSEEK_HARNESS_STATE_DIR")),
		timeout:              time.Duration(boundedIntEnv("DEEPSEEK_HARNESS_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second,
		outputLimit:          int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		envAllow:             csvValues(os.Getenv("DEEPSEEK_HARNESS_ENV_ALLOWLIST")),
		workspaceKey:         firstNonEmpty(os.Getenv("DEEPSEEK_HARNESS_WORKSPACE_KEY"), "deepseek-harness"),
		dispatcher:           dispatcher,
		hostDispatchEnabled:  envEnabled("HAI_HOST_RUNTIME_BRIDGE_ENABLED") && len(strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_BRIDGE_TOKEN"))) >= 32,
		executionBlockReason: dshExecutionIsolationUnavailable,
	}
}

func (a *deepSeekHarnessAdapter) executionUnavailableReason() string {
	if a == nil {
		return dshExecutionIsolationUnavailable
	}
	if a.allowDirectExecutionForTest {
		return ""
	}
	if strings.TrimSpace(a.executionBlockReason) != "" {
		return strings.TrimSpace(a.executionBlockReason)
	}
	if a.dispatcher == nil {
		return dshExecutionIsolationUnavailable
	}
	availability, ok := a.dispatcher.(interface{ ExecutionAvailability() (bool, string) })
	if !ok {
		return "DeepSeek Harness execution is blocked: the host dispatcher does not attest an operating-system isolation boundary"
	}
	available, reason := availability.ExecutionAvailability()
	if !available {
		if strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
		return dshExecutionIsolationUnavailable
	}
	return ""
}

func (a *deepSeekHarnessAdapter) Info() Info {
	isolationReason := a.executionUnavailableReason()
	if a.dispatcher != nil {
		missing := []string{}
		if strings.TrimSpace(a.expectedVersion) == "" {
			missing = append(missing, "DEEPSEEK_HARNESS_VERSION")
		}
		if strings.TrimSpace(a.workspaceKey) == "" {
			missing = append(missing, "DEEPSEEK_HARNESS_WORKSPACE_KEY")
		}
		if !a.hostDispatchEnabled {
			missing = append(missing, "HAI_HOST_RUNTIME_BRIDGE_ENABLED and HAI_HOST_RUNTIME_BRIDGE_TOKEN")
		}
		if isolationReason != "" {
			missing = append(missing, isolationReason)
		}
		configured := len(missing) == 0
		return Info{
			ID:                   "deepseek-harness",
			Name:                 "DeepSeek Harness",
			Type:                 "deepseek_harness",
			Enabled:              a.enabled,
			Configured:           configured,
			ExecutionEnabled:     a.enabled && a.executionEnabled && configured,
			RequiresApproval:     true,
			ReadOnlyDefault:      true,
			Capabilities:         a.capabilities(),
			Architecture:         a.architecture(),
			Controls:             a.controls(),
			MissingConfiguration: missing,
			Endpoint:             "local Windows host bridge",
		}
	}
	missing := []string{}
	if strings.TrimSpace(a.executable) == "" {
		missing = append(missing, "DEEPSEEK_HARNESS_EXECUTABLE")
	}
	if strings.TrimSpace(a.expectedVersion) == "" {
		missing = append(missing, "DEEPSEEK_HARNESS_VERSION")
	}
	if strings.TrimSpace(a.workspace) == "" {
		missing = append(missing, "DEEPSEEK_HARNESS_WORKSPACE")
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		missing = append(missing, "AGENT_RUNTIME_WORKSPACE_ROOT")
	}
	if strings.TrimSpace(a.stateDir) == "" {
		missing = append(missing, "DEEPSEEK_HARNESS_STATE_DIR")
	}
	if isolationReason != "" {
		missing = append(missing, isolationReason)
	}
	workspaceReason := a.workspaceBlockedReason()
	return Info{
		ID:                   "deepseek-harness",
		Name:                 "DeepSeek Harness",
		Type:                 "deepseek_harness",
		Enabled:              a.enabled,
		Configured:           len(missing) == 0 && workspaceReason == "" && a.stateDirBlockedReason() == "",
		ExecutionEnabled:     a.enabled && a.executionEnabled && len(missing) == 0 && workspaceReason == "" && a.stateDirBlockedReason() == "",
		RequiresApproval:     true,
		ReadOnlyDefault:      true,
		Capabilities:         a.capabilities(),
		Architecture:         a.architecture(),
		Controls:             a.controls(),
		MissingConfiguration: missing,
		Endpoint:             a.executable,
	}
}

func (a *deepSeekHarnessAdapter) HealthCheck(ctx context.Context) Health {
	started := time.Now()
	health := Health{RuntimeID: "deepseek-harness", Status: "disabled", CheckedAt: time.Now().UTC()}
	if !a.enabled {
		health.Reason = "DEEPSEEK_HARNESS_ENABLED is false"
		return health
	}
	if reason := a.executionUnavailableReason(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if a.dispatcher != nil {
		if !a.hostDispatchEnabled {
			health.Status = "blocked"
			health.Reason = "HAI host runtime bridge is disabled or missing its dedicated token"
			return health
		}
		if strings.TrimSpace(a.expectedVersion) == "" {
			health.Status = "blocked"
			health.Reason = "DEEPSEEK_HARNESS_VERSION is required to pin this developer-preview runtime"
			return health
		}
		if strings.TrimSpace(a.workspaceKey) == "" {
			health.Status = "blocked"
			health.Reason = "DEEPSEEK_HARNESS_WORKSPACE_KEY is required for the Windows host bridge"
			return health
		}
		if !a.executionEnabled {
			health.Status = "blocked"
			health.Reason = "DEEPSEEK_HARNESS_EXECUTION_ENABLED is false; host execution remains opt-in while upstream is a developer preview"
			return health
		}
		health.Status = "unavailable"
		health.Reason = "The Windows host bridge does not expose an authenticated, current worker heartbeat; HAI cannot verify that queued work can be executed"
		return health
	}
	if strings.TrimSpace(a.workspace) == "" {
		health.Status = "blocked"
		health.Reason = "DEEPSEEK_HARNESS_WORKSPACE is required"
		return health
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	stateDir, reason := a.resolvedStateDir()
	if reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if stat, err := os.Stat(a.workspace); err != nil || !stat.IsDir() {
		health.Status = "blocked"
		health.Reason = "DeepSeek Harness workspace is not an accessible directory"
		return health
	}
	path, err := exec.LookPath(a.executable)
	if err != nil {
		health.Status = "unavailable"
		health.Reason = "DeepSeek Harness executable was not found"
		return health
	}
	if reason := a.versionBlockedReasonWithStateDir(ctx, stateDir); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if !a.executionEnabled {
		health.Status = "blocked"
		health.Reason = "DEEPSEEK_HARNESS_EXECUTION_ENABLED is false; headless execution remains opt-in while upstream is a developer preview"
		return health
	}
	health.Status = "ready"
	health.Reason = "DeepSeek Harness headless profile is available at " + filepath.Base(path) + "; every task still needs HAI approval and a final-effect proof"
	health.LatencyMs = time.Since(started).Milliseconds()
	return health
}

func (a *deepSeekHarnessAdapter) ListSkills(context.Context) []Skill {
	return []Skill{{
		ID:               "deepseek-harness:preview-readiness",
		RuntimeID:        "deepseek-harness",
		Name:             "Preview readiness check",
		Category:         "agent_harness",
		RiskLevel:        "high",
		ApprovalRequired: true,
		ExecutionMode:    "approved_headless_task",
		Source:           "DEEPSEEK_HARNESS_EXECUTABLE",
		Description:      "Runs only DeepSeek Harness' documented one-shot headless profile after HAI approval. HAI never launches the Web UI or ACP server and never installs plugins.",
		Tags:             []string{"deepseek", "harness", "headless", "approval-gated"},
	}}
}

func (a *deepSeekHarnessAdapter) ExecuteTask(parent context.Context, task Task) Result {
	started := time.Now()
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return deepSeekHarnessCancellationResult(started, "DeepSeek Harness task was cancelled before dispatch; no host job was queued", "cancelled before host dispatch")
	}
	if result, blocked := emergencyStopResult("deepseek-harness"); blocked {
		return result
	}
	if !a.enabled {
		return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: "DEEPSEEK_HARNESS_ENABLED is false", ExitCode: -1}
	}
	if !a.executionEnabled {
		return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: "DEEPSEEK_HARNESS_EXECUTION_ENABLED is false", ExitCode: -1}
	}
	if reason := a.executionUnavailableReason(); reason != "" {
		return Result{
			RuntimeID:   "deepseek-harness",
			Status:      "blocked",
			Message:     reason,
			ExitCode:    -1,
			AuditEvents: []string{"no host job or local process was started because operating-system isolation is unavailable"},
		}
	}
	if reason := deepSeekHarnessPromptBlockedReason(task.Prompt); reason != "" {
		return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: reason, ExitCode: -1,
			AuditEvents: []string{"DeepSeek Harness prompt rejected before host dispatch"}}
	}
	if a.dispatcher != nil {
		if !a.hostDispatchEnabled {
			return Result{
				RuntimeID: "deepseek-harness",
				Status:    "blocked",
				Message:   "DeepSeek Harness host bridge is disabled or missing its dedicated token; no host task was queued",
				ExitCode:  -1,
				AuditEvents: []string{
					"DeepSeek Harness host dispatch blocked because the dedicated bridge is not enabled",
				},
			}
		}
		if strings.TrimSpace(a.expectedVersion) == "" {
			return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: "DEEPSEEK_HARNESS_VERSION is required to pin this developer-preview runtime", ExitCode: -1}
		}
		// Admission holds the stop fence only across a bounded, cancellable
		// durable enqueue. If a timed-out write may have committed, do not invite
		// a retry until the existing task ID is reconciled.
		if parent.Err() != nil {
			return deepSeekHarnessCancellationResult(started, "DeepSeek Harness task was cancelled before dispatch; no host job was queued", "cancelled before host dispatch")
		}
		var job *hostruntime.Job
		enqueueAttempted := false
		admissionResult, stopBlocked, err := withExecutionAdmission(parent, a.RuntimeID(), func(admissionCtx context.Context) error {
			if admissionCtx.Err() != nil || parent.Err() != nil {
				return errRuntimeAdmissionCancelled
			}
			enqueueAttempted = true
			var enqueueErr error
			job, enqueueErr = a.dispatcher.EnqueueContext(admissionCtx, hostruntime.ApprovedTask{
				OwnerIdentity: task.OwnerIdentity,
				RuntimeID:     a.RuntimeID(),
				TaskID:        task.ID,
				Prompt:        task.Prompt,
				WorkspaceKey:  firstNonEmpty(a.workspaceKey, "deepseek-harness"),
				Approved:      true,
			})
			return enqueueErr
		})
		if stopBlocked {
			return admissionResult
		}
		if err != nil {
			if errors.Is(err, errRuntimeAdmissionCancelled) {
				return deepSeekHarnessCancellationResult(started, "DeepSeek Harness task was cancelled before dispatch; no host job was queued", "cancelled before host dispatch")
			}
			if parent.Err() != nil {
				return a.cancelledEnqueueOutcome(parent, started, task, uuid.Nil)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				if enqueueAttempted {
					return Result{
						RuntimeID:  "deepseek-harness",
						Status:     "indeterminate",
						Message:    "DeepSeek Harness enqueue timed out after the durable write was attempted; reconcile this task ID before retrying",
						ExitCode:   -1,
						DurationMs: time.Since(started).Milliseconds(),
						AuditEvents: []string{
							"bounded host-job admission expired; the database may have committed despite the timeout",
							"automatic retry was withheld until the durable task ID is reconciled",
						},
					}
				}
				return Result{
					RuntimeID:   "deepseek-harness",
					Status:      "blocked",
					Message:     "DeepSeek Harness admission timed out before the durable enqueue began; no host job was written",
					ExitCode:    -1,
					DurationMs:  time.Since(started).Milliseconds(),
					AuditEvents: []string{"bounded admission ended before any durable host-job write was attempted"},
				}
			}
			return Result{
				RuntimeID: "deepseek-harness",
				Status:    "failed",
				Message:   "DeepSeek Harness host bridge could not queue the approved task",
				ExitCode:  -1,
				AuditEvents: []string{
					"DeepSeek Harness host dispatch failed before any container or host process was invoked",
				},
			}
		}
		if job == nil || job.ID == uuid.Nil {
			if parent.Err() != nil {
				return a.cancelledEnqueueOutcome(parent, started, task, uuid.Nil)
			}
			return Result{
				RuntimeID:   "deepseek-harness",
				Status:      "indeterminate",
				Message:     "DeepSeek Harness host bridge returned no durable job identity; enqueue outcome is unknown and must be reconciled before retrying",
				ExitCode:    -1,
				DurationMs:  time.Since(started).Milliseconds(),
				AuditEvents: []string{"dispatcher returned success without a usable durable execution reference", "automatic retry was not suggested because a duplicate host job cannot be excluded"},
			}
		}
		if parent.Err() != nil {
			return a.cancelledEnqueueOutcome(parent, started, task, job.ID)
		}
		return Result{
			RuntimeID:          "deepseek-harness",
			ExecutionReference: job.ID.String(),
			Status:             "queued",
			Message:            "DeepSeek Harness task is queued for the local Windows host bridge; no container process was invoked",
			ExitCode:           0,
			DurationMs:         time.Since(started).Milliseconds(),
			AuditEvents: []string{
				"server-side approval and final-effect proof verified by HAI before host dispatch",
				"approved task persisted for the loopback-only Windows host bridge",
				"no DeepSeek Harness process was started inside the backend container",
				"host runtime job queued: " + job.ID.String(),
			},
		}
	}
	if !a.allowDirectExecutionForTest {
		return Result{
			RuntimeID: "deepseek-harness",
			Status:    "blocked",
			Message:   "DeepSeek Harness host bridge is unavailable; no container process was invoked",
			ExitCode:  -1,
			AuditEvents: []string{
				"DeepSeek Harness execution failed closed because no host dispatcher is configured",
			},
		}
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: reason, ExitCode: -1}
	}
	stateDir, reason := a.resolvedStateDir()
	if reason != "" {
		return Result{RuntimeID: "deepseek-harness", Status: "blocked", Message: reason, ExitCode: -1}
	}
	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()
	if !a.acquireExecutionGate(ctx) {
		return Result{
			RuntimeID: "deepseek-harness",
			Status:    "blocked",
			Message:   "DeepSeek Harness is already running an approved task against its shared state directory; the queued run timed out before it could start",
			ExitCode:  -1,
			AuditEvents: []string{
				"DeepSeek Harness run blocked before invocation because its shared DSH_HOME is busy",
			},
		}
	}
	defer a.releaseExecutionGate()
	if reason := a.versionBlockedReasonWithStateDir(ctx, stateDir); reason != "" {
		return Result{
			RuntimeID:   "deepseek-harness",
			Status:      "blocked",
			Message:     reason,
			ExitCode:    -1,
			AuditEvents: []string{"DeepSeek Harness version probe blocked task execution before CLI task invocation"},
		}
	}
	cmd := exec.CommandContext(ctx, a.executable, "--profile", "headless", task.Prompt)
	cmd.Dir = a.workspace
	cmd.Env = safeEnvironment(a.envAllow, map[string]string{
		"DSH_HOME":            stateDir,
		"HAI_RUNTIME_TASK_ID": task.ID,
		"HAI_PROJECT_KEY":     task.ProjectKey,
		"TERMINAL_CWD":        a.workspace,
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &stdout, remaining: a.outputLimit}
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: a.outputLimit / 4}
	startProcess := a.processStarter
	if startProcess == nil {
		startProcess = func(cmd *exec.Cmd) error { return cmd.Start() }
	}
	admissionResult, stopBlocked, startErr := withExecutionAdmission(ctx, "deepseek-harness", func(admissionCtx context.Context) error {
		if admissionCtx.Err() != nil || ctx.Err() != nil {
			return errRuntimeAdmissionCancelled
		}
		return startProcess(cmd)
	})
	if stopBlocked {
		return admissionResult
	}
	if errors.Is(startErr, errRuntimeAdmissionCancelled) {
		return deepSeekHarnessCancellationResult(started, "DeepSeek Harness task was cancelled before process start", "no DeepSeek Harness task process was started")
	}
	err := startErr
	if err == nil {
		err = cmd.Wait()
	}
	if err == nil && ctx.Err() != nil {
		// CommandContext owns cancellation after Start; Wait remains outside the
		// stop fence so the operator stop can be persisted immediately.
		return Result{
			RuntimeID:   "deepseek-harness",
			Status:      "blocked",
			Message:     "DeepSeek Harness task was cancelled after process start; its outcome requires verification",
			ExitCode:    -1,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: []string{"process started before cancellation and was waited outside the emergency-stop admission fence"},
		}
	}
	output := redactRuntimeOutput(stdout.String(), a.outputLimit, cmd.Env)
	message := "DeepSeek Harness process exited successfully; HAI has not independently verified the requested outcome"
	status, exitCode := "needs_review", 0
	if err != nil {
		status, exitCode = "failed", -1
		message = redactRuntimeOutput(stderr.String(), a.outputLimit/4, cmd.Env)
		if message == "" {
			message = "DeepSeek Harness process failed without diagnostic output"
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			status = "blocked"
			message = "DeepSeek Harness execution exceeded the configured timeout and was stopped"
		}
	}
	return Result{
		RuntimeID:  "deepseek-harness",
		Status:     status,
		Message:    message,
		Output:     output,
		ExitCode:   exitCode,
		DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: []string{
			"server-side approval and final-effect proof verified by HAI before adapter execution",
			"DeepSeek Harness invoked only through documented --profile headless without shell interpolation",
			"Web UI, ACP server, browser control, and plugin installation were not invoked",
			"dedicated workspace, state directory, serialized state access, timeout, output limit, environment allowlist, and secret redaction enforced by HAI",
			"zero process exit does not verify the requested outcome or downstream effects",
		},
	}
}

func deepSeekHarnessCancellationResult(started time.Time, message, audit string) Result {
	return Result{
		RuntimeID:   "deepseek-harness",
		Status:      "blocked",
		Message:     message,
		ExitCode:    -1,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: []string{audit},
	}
}

// versionBlockedReason verifies the operator-pinned preview release before a
// task is allowed to run. The probe asks only for the launcher's version: it
// does not boot a profile, contact a model, or enable tools.
func (a *deepSeekHarnessAdapter) versionBlockedReason(parent context.Context) string {
	return a.versionBlockedReasonWithStateDir(parent, a.stateDir)
}

func (a *deepSeekHarnessAdapter) versionBlockedReasonWithStateDir(parent context.Context, stateDir string) string {
	expected := strings.TrimSpace(a.expectedVersion)
	if expected == "" {
		return "DEEPSEEK_HARNESS_VERSION is required to pin this developer-preview runtime"
	}
	expectedToken, ok := parseDeepSeekHarnessVersionToken(expected)
	if !ok {
		return "DEEPSEEK_HARNESS_VERSION must be an exact SemVer version token"
	}
	if a.versionProbe != nil {
		actual, err := a.versionProbe(parent)
		if err != nil {
			return "DeepSeek Harness version probe failed: " + safety.RedactSecrets(err.Error())
		}
		if reason := deepSeekHarnessVersionMismatchReason(expectedToken, actual); reason != "" {
			return reason
		}
		return ""
	}
	probeTimeout := a.timeout
	if probeTimeout <= 0 {
		probeTimeout = time.Duration(defaultTimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, minDuration(probeTimeout, 10*time.Second))
	defer cancel()
	cmd := exec.CommandContext(ctx, a.executable, "--version")
	cmd.Dir = a.workspace
	cmd.Env = safeEnvironment(nonSensitiveEnvironmentAllowlist(a.envAllow), map[string]string{
		"DSH_HOME": stateDir,
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &stdout, remaining: a.outputLimit / 4}
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: a.outputLimit / 8}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "DeepSeek Harness version probe timed out"
		}
		diagnostic := redactRuntimeOutput(stderr.String(), a.outputLimit/8, cmd.Env)
		if diagnostic == "" {
			diagnostic = "the executable returned an unsuccessful status without diagnostic output"
		}
		return "DeepSeek Harness version probe failed: " + diagnostic
	}
	return deepSeekHarnessVersionMismatchReason(expectedToken, redactRuntimeOutput(stdout.String(), a.outputLimit/4, cmd.Env))
}

func nonSensitiveEnvironmentAllowlist(allow []string) []string {
	filtered := make([]string, 0, len(allow))
	for _, key := range allow {
		if !safety.IsSensitiveKey(key) {
			filtered = append(filtered, key)
		}
	}
	return filtered
}

func redactRuntimeOutput(value string, limit int64, environment []string) string {
	value = strings.TrimSpace(value)
	for _, entry := range environment {
		key, secret, found := strings.Cut(entry, "=")
		if found && secret != "" && safety.IsSensitiveKey(key) {
			value = strings.ReplaceAll(value, secret, "[REDACTED_ENV_SECRET]")
		}
	}
	value = safety.RedactSecrets(value)
	if limit >= 0 && int64(len(value)) > limit {
		value = value[:limit]
	}
	return strings.TrimSpace(value)
}

var deepSeekHarnessVersionTokenPattern = regexp.MustCompile(`\Av?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\z`)

func parseDeepSeekHarnessVersionToken(value string) (string, bool) {
	match := deepSeekHarnessVersionTokenPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func deepSeekHarnessVersionMismatchReason(expectedToken, actual string) string {
	actual = strings.TrimSpace(actual)
	actualToken, ok := parseDeepSeekHarnessVersionToken(actual)
	if !ok || actualToken != expectedToken {
		return fmt.Sprintf("DeepSeek Harness version mismatch: expected %q, probe returned %q", expectedToken, safety.RedactSecrets(actual))
	}
	return ""
}

// acquireExecutionGate serializes HAI-owned runs because the upstream preview
// currently initializes and rewrites shared profile state under DSH_HOME.
// The caller's timeout includes queueing time, so a busy runtime cannot leave
// a workflow waiting forever.
func (a *deepSeekHarnessAdapter) acquireExecutionGate(ctx context.Context) bool {
	a.executionGateOnce.Do(func() { a.executionGate = make(chan struct{}, 1) })
	select {
	case a.executionGate <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *deepSeekHarnessAdapter) releaseExecutionGate() {
	<-a.executionGate
}

// deepSeekHarnessPromptBlockedReason rejects values that a CLI parser could
// mistake for an option. The documented upstream invocation accepts a prompt
// as a positional argument, but it does not document an argument separator,
// so HAI refuses ambiguous input instead of relying on parser-specific rules.
func deepSeekHarnessPromptBlockedReason(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if strings.HasPrefix(prompt, "-") {
		return "DeepSeek Harness task prompt must not start with a command option"
	}
	if prompt == "web" || prompt == "plugin" {
		return "DeepSeek Harness task prompt must not be an upstream launcher subcommand"
	}
	if strings.ContainsRune(prompt, '\x00') {
		return "DeepSeek Harness task prompt contains an invalid null byte"
	}
	return ""
}

func (a *deepSeekHarnessAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return StopResult{
		RuntimeID: "deepseek-harness",
		TaskID:    strings.TrimSpace(taskID),
		Status:    "blocked",
		Message:   "an authenticated owner is required to revoke the durable DeepSeek host job",
		AuditEvents: []string{
			"ownerless DeepSeek host-job cancellation rejected",
		},
	}
}

// StopTaskForOwner is used while the in-process HAI task is still active. A
// missing row means enqueue may still be in flight, so only a request is
// reported; ExecuteTask retries revocation after enqueue returns.
func (a *deepSeekHarnessAdapter) StopTaskForOwner(ctx context.Context, taskID, ownerIdentity string, enqueueMayBeInFlight bool) StopResult {
	return a.cancelHostTask(ctx, taskID, ownerIdentity, uuid.Nil, enqueueMayBeInFlight)
}

// StopTaskWithReference is the restart-safe path for a durable HAI execution
// record. The opaque reference is parsed as the exact host-job UUID and still
// bound to both the authenticated owner and runtime task ID in the repository.
func (a *deepSeekHarnessAdapter) StopTaskWithReference(ctx context.Context, taskID, ownerIdentity, executionReference string) StopResult {
	jobID, err := uuid.Parse(strings.TrimSpace(executionReference))
	if err != nil || jobID == uuid.Nil {
		return StopResult{
			RuntimeID:          "deepseek-harness",
			TaskID:             strings.TrimSpace(taskID),
			ExecutionReference: strings.TrimSpace(executionReference),
			Status:             "blocked",
			Message:            "a valid durable host-job reference is required",
			AuditEvents:        []string{"DeepSeek cancellation rejected without a valid owner-bound host-job reference"},
		}
	}
	return a.cancelHostTask(ctx, taskID, ownerIdentity, jobID, false)
}

func (a *deepSeekHarnessAdapter) cancelHostTask(ctx context.Context, taskID, ownerIdentity string, jobID uuid.UUID, missingMeansPending bool) StopResult {
	base := StopResult{RuntimeID: "deepseek-harness", TaskID: strings.TrimSpace(taskID)}
	if jobID != uuid.Nil {
		base.ExecutionReference = jobID.String()
	}
	if strings.TrimSpace(ownerIdentity) == "" || strings.TrimSpace(taskID) == "" {
		base.Status = "blocked"
		base.Message = "authenticated task owner and ID are required for host-job cancellation"
		base.AuditEvents = []string{"ownerless or unidentified DeepSeek cancellation rejected"}
		return base
	}
	if a.dispatcher == nil {
		base.Status = "blocked"
		base.Message = "DeepSeek host-job cancellation is unavailable because the durable dispatcher is not configured"
		base.AuditEvents = []string{"durable host cancellation was not available; no cancellation success claimed"}
		return base
	}
	if ctx == nil || ctx.Err() != nil {
		base.Status = "blocked"
		base.Message = "a live request context is required for host-job cancellation; no cancellation was dispatched"
		base.AuditEvents = []string{"missing or cancelled DeepSeek stop request rejected before durable dispatch"}
		return base
	}
	requestCtx, cancel := context.WithTimeout(ctx, hostCancellationTimeout)
	defer cancel()
	job, revoked, err := a.dispatcher.CancelTask(requestCtx, strings.TrimSpace(ownerIdentity), strings.TrimSpace(taskID), jobID)
	if requestCtx.Err() != nil {
		base.Status = "indeterminate"
		base.Message = "the cancellation request expired before acknowledgment; inspect the durable job before retrying"
		base.AuditEvents = []string{"host cancellation acknowledgment was not confirmed within the request scope; persistence may have occurred"}
		return base
	}
	if err != nil {
		if errors.Is(err, hostruntime.ErrJobNotFound) && missingMeansPending {
			base.Status = "cancellation_requested"
			base.Message = "HAI cancelled the active task context; host enqueue may still be in flight, so durable cancellation is not yet confirmed"
			base.AuditEvents = []string{"no durable host row exists yet; the enqueue path must revoke it if it is subsequently persisted"}
			return base
		}
		base.Status = "indeterminate"
		base.Message = "HAI could not confirm whether the DeepSeek host job was durably revoked; inspect the job before retrying"
		base.AuditEvents = []string{"owner-bound host cancellation persistence was not confirmed: " + safety.RedactSecrets(err.Error())}
		return base
	}
	if job == nil {
		if missingMeansPending {
			base.Status = "cancellation_requested"
			base.Message = "HAI cancelled the active task context; host enqueue may still be in flight, so durable cancellation is not yet confirmed"
			base.AuditEvents = []string{"no durable host row exists yet; the enqueue path must revoke it if it is subsequently persisted"}
			return base
		}
		base.Status = "indeterminate"
		base.Message = "the referenced DeepSeek host job could not be found under this owner and task; cancellation is unconfirmed"
		base.AuditEvents = []string{"exact owner/task/job reference did not resolve to a durable host row"}
		return base
	}
	if job.ID == uuid.Nil ||
		strings.ToLower(strings.TrimSpace(job.RuntimeID)) != "deepseek-harness" ||
		strings.TrimSpace(job.OwnerIdentity) != strings.TrimSpace(ownerIdentity) ||
		strings.TrimSpace(job.TaskID) != strings.TrimSpace(taskID) ||
		(jobID != uuid.Nil && job.ID != jobID) {
		base.Status = "indeterminate"
		base.Message = "the cancellation receipt did not match the requested runtime, owner, task and job; inspect the original job before retrying"
		base.AuditEvents = []string{"host cancellation receipt identity mismatch; no foreign job reference or cancellation success was substituted"}
		return base
	}
	base.ExecutionReference = job.ID.String()
	if revoked && job.Status == hostruntime.StatusCancelled {
		if job.LeaseDigest != "" || job.LeaseExpires != nil || job.StartIntentID != nil || job.ActualStartAckAt != nil {
			base.Status = "indeterminate"
			base.Message = "the cancelled host record retains lease or execution-start evidence; pre-start revocation and process termination are not confirmed"
			base.AuditEvents = []string{"cancelled host receipt conflicts with lease/start state; inspect durable execution evidence before retrying"}
			return base
		}
		base.Status = "cancelled"
		base.Message = "DeepSeek host job was durably revoked before execution start and is no longer leaseable"
		base.AuditEvents = []string{"owner, task, and host-job identity matched", "durable host job status is cancelled", "lease token invalidated; worker confirmation can no longer start this job"}
		return base
	}
	base.Status = "indeterminate"
	switch {
	case job.Status == hostruntime.StatusLeased && job.ExecutionConfirmedAt != nil:
		base.Message = "the DeepSeek worker already confirmed execution; HAI cannot prove the host process stopped"
		base.AuditEvents = []string{"execution confirmation won the cancellation race; no cancellation success claimed"}
	case job.Status == hostruntime.StatusCompleted:
		base.Message = "the DeepSeek host job has already completed; inspect its result rather than treating it as cancelled"
		base.AuditEvents = []string{"completed host job cannot be reported as cancelled"}
	default:
		base.Message = "the DeepSeek host job is not in a state HAI can safely revoke; inspect its durable status"
		base.AuditEvents = []string{"host job was not transitioned to cancelled; cancellation success not claimed"}
	}
	return base
}

func (a *deepSeekHarnessAdapter) cancelledEnqueueOutcome(parent context.Context, started time.Time, task Task, jobID uuid.UUID) Result {
	// Enqueue cleanup is an existing-effect recovery, not a new stop request.
	// Keep its owner context while allowing bounded revocation after cancellation.
	cleanup := parent
	if parent != nil {
		cleanup = context.WithoutCancel(parent)
	}
	stop := a.cancelHostTask(cleanup, task.ID, task.OwnerIdentity, jobID, true)
	result := Result{
		RuntimeID:          "deepseek-harness",
		ExecutionReference: stop.ExecutionReference,
		Status:             "indeterminate",
		Message:            stop.Message,
		ExitCode:           -1,
		DurationMs:         time.Since(started).Milliseconds(),
		AuditEvents:        append([]string{"DeepSeek enqueue overlapped a stop request"}, stop.AuditEvents...),
	}
	if stop.Status == "cancelled" {
		result.Status = "cancelled"
		result.Message = stop.Message
		result.durableCancellationConfirmed = true
		result.AuditEvents = append(result.AuditEvents, "durable host job cancellation confirmed before worker execution")
	}
	return result
}

func (a *deepSeekHarnessAdapter) stateDirBlockedReason() string {
	_, reason := a.resolvedStateDir()
	return reason
}

func (a *deepSeekHarnessAdapter) resolvedStateDir() (string, string) {
	if strings.TrimSpace(a.stateDir) == "" {
		return "", "DEEPSEEK_HARNESS_STATE_DIR is required"
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return "", "agent runtime workspace root is required"
	}
	root, err := filepath.Abs(filepath.Clean(a.workspaceRoot))
	if err != nil {
		return "", "agent runtime workspace root is invalid"
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "agent runtime workspace root is not accessible"
	}
	stateDir, err := filepath.Abs(filepath.Clean(a.stateDir))
	if err != nil {
		return "", "DeepSeek Harness state directory is invalid"
	}
	if info, err := os.Lstat(stateDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "DeepSeek Harness state directory must not be a symbolic link"
		}
		if !info.IsDir() {
			return "", "DeepSeek Harness state directory must be a directory"
		}
		stateDir, err = filepath.EvalSymlinks(stateDir)
		if err != nil {
			return "", "DeepSeek Harness state directory is not accessible"
		}
	} else if !os.IsNotExist(err) {
		return "", "DeepSeek Harness state directory cannot be inspected"
	}
	parent := filepath.Dir(stateDir)
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", "DeepSeek Harness state directory parent is not accessible"
	}
	stateDir = filepath.Join(parent, filepath.Base(stateDir))
	relative, err := filepath.Rel(root, stateDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", "DeepSeek Harness state directory must stay inside AGENT_RUNTIME_WORKSPACE_ROOT"
	}
	return stateDir, ""
}

func (a *deepSeekHarnessAdapter) workspaceBlockedReason() string {
	if strings.TrimSpace(a.workspace) == "" {
		return "DEEPSEEK_HARNESS_WORKSPACE is required"
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return "agent runtime workspace root is required"
	}
	root, err := filepath.Abs(filepath.Clean(a.workspaceRoot))
	if err != nil {
		return "agent runtime workspace root is invalid"
	}
	// EvalSymlinks alone can leave Windows junctions unresolved. Reject every
	// link/reparse component before a version probe or task process can start.
	root, err = pathsafety.ValidateNoLinks(root, false)
	if err != nil {
		if errors.Is(err, pathsafety.ErrPathLink) {
			return "agent runtime workspace root must not contain symbolic links or Windows reparse points"
		}
		return "agent runtime workspace root is not accessible"
	}
	workspace, err := filepath.Abs(filepath.Clean(a.workspace))
	if err != nil {
		return "DeepSeek Harness workspace is invalid"
	}
	workspace, err = pathsafety.ValidateNoLinks(workspace, false)
	if err != nil {
		if errors.Is(err, pathsafety.ErrPathLink) {
			return "DeepSeek Harness workspace must not contain symbolic links or Windows reparse points"
		}
		return "DeepSeek Harness workspace is not accessible"
	}
	relative, err := filepath.Rel(root, workspace)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "DeepSeek Harness workspace must stay inside AGENT_RUNTIME_WORKSPACE_ROOT"
	}
	return ""
}

func (a *deepSeekHarnessAdapter) capabilities() []string {
	return []string{
		"documented one-shot headless execution",
		"plugin-based runtime architecture",
		"operator-configured model routing",
		"operator-managed model routing and plugin architecture",
	}
}

func (a *deepSeekHarnessAdapter) architecture() []string {
	return []string{
		"HAI workflow approval queue and final-effect proof",
		"HAI agent-runtime registry",
		"DeepSeek Harness documented headless profile capability boundary",
		"operator-managed model and permission policy",
		"HAI source-grounded verification and audit log",
	}
}

func (a *deepSeekHarnessAdapter) controls() []string {
	return []string{
		"disabled by default through DEEPSEEK_HARNESS_ENABLED and DEEPSEEK_HARNESS_EXECUTION_ENABLED",
		"server-side HAI approval and final-effect proof required before every headless task",
		"owner-bound stop atomically revokes pending or unconfirmed host jobs; after worker confirmation, HAI reports an uncertain outcome instead of cancellation success",
		"dedicated workspace must remain under AGENT_RUNTIME_WORKSPACE_ROOT",
		"dedicated state directory must remain under AGENT_RUNTIME_WORKSPACE_ROOT",
		"does not launch the Web UI, ACP server, control a browser, or install plugins",
		"timeout, output limit, environment allowlist, and secret redaction remain enforced by HAI",
		"DeepSeek Harness permission prompts and model credentials remain operator-managed",
	}
}

package agentruntime

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/pathsafety"
	"automation-hub-backend/internal/processcontrol"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	defaultTimeoutSeconds = 120
	defaultOutputLimit    = 64 * 1024
	maxOutputLimit        = 1024 * 1024
	maxTaskPromptBytes    = 50 * 1024
)

type Task struct {
	ID               string
	Prompt           string
	ProjectKey       string
	OwnerIdentity    string
	HumanApproved    bool
	ApprovalSourceID string
	FinalEffectProof FinalEffectAuthorizationProof
}

type Result struct {
	RuntimeID   string      `json:"runtimeId"`
	Status      string      `json:"status"`
	Message     string      `json:"message,omitempty"`
	Output      string      `json:"output,omitempty"`
	RouteTrace  *RouteTrace `json:"routeTrace,omitempty"`
	ExitCode    int         `json:"exitCode"`
	DurationMs  int64       `json:"durationMs"`
	AuditEvents []string    `json:"auditEvents"`
}

type RouteTrace struct {
	RuntimeID           string   `json:"runtimeId"`
	Intent              string   `json:"intent,omitempty"`
	ExecutionMode       string   `json:"executionMode,omitempty"`
	RiskLevel           string   `json:"riskLevel,omitempty"`
	RecommendedSkills   []string `json:"recommendedSkills,omitempty"`
	VisibleProviders    []string `json:"visibleProviders,omitempty"`
	VisibleTools        []string `json:"visibleTools,omitempty"`
	RelevantMaps        []string `json:"relevantMaps,omitempty"`
	BlockedSurfaces     []string `json:"blockedSurfaces,omitempty"`
	RequiredControls    []string `json:"requiredControls,omitempty"`
	ValidationChecklist []string `json:"validationChecklist,omitempty"`
}

type Health struct {
	RuntimeID string    `json:"runtimeId"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CheckedAt time.Time `json:"checkedAt"`
	LatencyMs int64     `json:"latencyMs"`
}

type Info struct {
	ID                   string                    `json:"id"`
	Name                 string                    `json:"name"`
	Type                 string                    `json:"type"`
	Enabled              bool                      `json:"enabled"`
	Configured           bool                      `json:"configured"`
	ExecutionEnabled     bool                      `json:"executionEnabled"`
	RequiresApproval     bool                      `json:"requiresApproval"`
	ReadOnlyDefault      bool                      `json:"readOnlyDefault"`
	Capabilities         []string                  `json:"capabilities"`
	Architecture         []string                  `json:"architecture,omitempty"`
	Controls             []string                  `json:"controls,omitempty"`
	Ecosystem            []RuntimeEcosystemSurface `json:"ecosystem,omitempty"`
	EcosystemPath        string                    `json:"ecosystemPath,omitempty"`
	MissingConfiguration []string                  `json:"missingConfiguration,omitempty"`
	Endpoint             string                    `json:"endpoint,omitempty"`
}

type Skill struct {
	ID               string   `json:"id"`
	RuntimeID        string   `json:"runtimeId"`
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	RiskLevel        string   `json:"riskLevel"`
	ApprovalRequired bool     `json:"approvalRequired"`
	ExecutionMode    string   `json:"executionMode"`
	Source           string   `json:"source,omitempty"`
	Description      string   `json:"description,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

type StopResult struct {
	RuntimeID   string   `json:"runtimeId"`
	TaskID      string   `json:"taskId"`
	Status      string   `json:"status"`
	Message     string   `json:"message,omitempty"`
	EvidenceURI string   `json:"evidenceUri,omitempty"`
	AuditEvents []string `json:"auditEvents,omitempty"`
}

type RuntimeEcosystemSurface struct {
	Category         string   `json:"category"`
	Status           string   `json:"status"`
	Count            int      `json:"count"`
	Items            []string `json:"items,omitempty"`
	More             int      `json:"more,omitempty"`
	Control          string   `json:"control,omitempty"`
	RiskLevel        string   `json:"riskLevel,omitempty"`
	ApprovalRequired bool     `json:"approvalRequired,omitempty"`
}

type Adapter interface {
	Info() Info
	HealthCheck(context.Context) Health
	ListSkills(context.Context) []Skill
	ExecuteTask(context.Context, Task) Result
	StopTask(context.Context, string) StopResult
}

type Registry struct {
	adapters            map[string]Adapter
	finalEffectVerifier FinalEffectProofVerifier
	running             map[string]runningTask
	mu                  sync.Mutex
}

type runningTask struct {
	ownerIdentity string
	cancel        context.CancelFunc
}

func NewRegistry(adapters ...Adapter) *Registry {
	return NewRegistryWithFinalEffectVerifier(nil, adapters...)
}

// NewRegistryWithFinalEffectVerifier builds a registry with an explicit
// final-effect proof boundary. A nil verifier intentionally leaves runtime
// discovery and health available while all adapter execution fails closed.
func NewRegistryWithFinalEffectVerifier(verifier FinalEffectProofVerifier, adapters ...Adapter) *Registry {
	registry := &Registry{
		adapters:            map[string]Adapter{},
		finalEffectVerifier: verifier,
		running:             map[string]runningTask{},
	}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		id := ""
		if identified, ok := adapter.(interface{ RuntimeID() string }); ok {
			id = strings.TrimSpace(identified.RuntimeID())
		}
		if id == "" {
			id = strings.TrimSpace(adapter.Info().ID)
		}
		if id != "" {
			registry.adapters[id] = adapter
		}
	}
	return registry
}

func DefaultRegistry() *Registry {
	// Production composition must inject its authoritative execution service
	// before enabling effects. The default registry is intentionally read-only.
	return DefaultRegistryWithFinalEffectVerifier(nil)
}

// DefaultRegistryWithFinalEffectVerifier is the production composition point
// for enabling runtime effects without exporting concrete adapter types.
func DefaultRegistryWithFinalEffectVerifier(verifier FinalEffectProofVerifier) *Registry {
	return NewRegistryWithFinalEffectVerifier(
		verifier,
		newHermesAdapterFromEnv(),
		newOdysseusAdapterFromEnv(),
		newOpenClawAdapterFromEnv(),
	)
}

func (r *Registry) List() []Info {
	result := []Info{}
	for _, id := range []string{"hermes", "odysseus", "openclaw"} {
		if adapter := r.adapters[id]; adapter != nil {
			result = append(result, adapter.Info())
		}
	}
	return result
}

func (r *Registry) Health(ctx context.Context) []Health {
	result := []Health{}
	for _, id := range []string{"hermes", "odysseus", "openclaw"} {
		if adapter := r.adapters[id]; adapter != nil {
			result = append(result, adapter.HealthCheck(ctx))
		}
	}
	return result
}

func (r *Registry) Skills(ctx context.Context, runtimeID string) ([]Skill, error) {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	adapter := r.adapters[runtimeID]
	if adapter == nil {
		return nil, fmt.Errorf("agent runtime %q is not registered", runtimeID)
	}
	return adapter.ListSkills(ctx), nil
}

// BindConsumedAuthorizationProof attaches an already-issued execution receipt
// to the exact runtime task without re-authorizing or consuming it. The caller
// must pass the receipt's stored request and decision digests. Execute still
// requires the injected verifier to load and validate the durable records.
//
// runtimeProof is optional opaque evidence for verifier implementations that
// use a signed or separately persisted handoff. It is never treated as
// authority by Registry itself.
func (r *Registry) BindConsumedAuthorizationProof(
	runtimeID string,
	task Task,
	receiptID string,
	authorizationRequestDigest string,
	decisionDigest string,
	runtimeProof string,
) (Task, error) {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	adapter := r.adapters[runtimeID]
	if adapter == nil {
		return Task{}, fmt.Errorf("agent runtime %q is not registered", runtimeID)
	}
	if strings.TrimSpace(task.ID) == "" ||
		strings.TrimSpace(task.OwnerIdentity) == "" ||
		strings.TrimSpace(task.Prompt) == "" {
		return Task{}, fmt.Errorf("runtime task id, owner, and prompt are required before binding authorization")
	}
	request := runtimeFinalEffectRequest(runtimeID, task, adapter.Info())
	task.FinalEffectProof = FinalEffectAuthorizationProof{
		ReceiptID:                  strings.TrimSpace(receiptID),
		AuthorizationRequestDigest: strings.ToLower(strings.TrimSpace(authorizationRequestDigest)),
		DecisionDigest:             strings.ToLower(strings.TrimSpace(decisionDigest)),
		RuntimeRequestDigest:       finalEffectRequestDigest(request),
		RuntimeProof:               strings.TrimSpace(runtimeProof),
	}
	if err := validateFinalEffectAuthorizationProof(request, task.FinalEffectProof); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (r *Registry) StopTask(_ context.Context, runtimeID string, taskID string, ownerIdentity string) StopResult {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	taskID = strings.TrimSpace(taskID)
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return StopResult{
			RuntimeID:   runtimeID,
			TaskID:      taskID,
			Status:      "blocked",
			Message:     "authenticated runtime task owner is required",
			AuditEvents: []string{"ownerless runtime task stop request rejected"},
		}
	}
	if taskID == "" {
		return StopResult{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     "agent runtime task id is required",
			AuditEvents: []string{"empty runtime task stop request rejected"},
		}
	}
	found, ownerMatched := r.stopRunningTask(runtimeID, taskID, ownerIdentity)
	if found && !ownerMatched {
		return StopResult{
			RuntimeID:   runtimeID,
			TaskID:      taskID,
			Status:      "blocked",
			Message:     "runtime task belongs to a different owner",
			AuditEvents: []string{"cross-owner runtime task stop request rejected"},
		}
	}
	if found {
		return StopResult{
			RuntimeID: runtimeID,
			TaskID:    taskID,
			Status:    "stopping",
			Message:   "HAI cancellation signal was sent to the active runtime task",
			AuditEvents: []string{
				"running runtime task located",
				"HAI-managed context cancellation requested",
			},
		}
	}
	if r.adapters[runtimeID] == nil {
		return StopResult{
			RuntimeID:   runtimeID,
			TaskID:      taskID,
			Status:      "blocked",
			Message:     fmt.Sprintf("agent runtime %q is not registered", runtimeID),
			AuditEvents: []string{"runtime registry lookup failed"},
		}
	}
	return StopResult{
		RuntimeID:   runtimeID,
		TaskID:      taskID,
		Status:      "blocked",
		Message:     "no active owner-bound runtime task was found",
		AuditEvents: []string{"untracked runtime stop rejected before adapter access"},
	}
}

func runtimeTaskKey(runtimeID string, taskID string) string {
	return strings.ToLower(strings.TrimSpace(runtimeID)) + ":" + strings.TrimSpace(taskID)
}

func (r *Registry) registerRunningTask(parent context.Context, runtimeID string, task Task) (context.Context, context.CancelFunc, bool) {
	taskID := strings.TrimSpace(task.ID)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return parent, func() {}, false
	}
	key := runtimeTaskKey(runtimeID, taskID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.running[key]; exists {
		return parent, func() {}, false
	}
	ctx, cancel := context.WithCancel(parent)
	r.running[key] = runningTask{
		ownerIdentity: strings.TrimSpace(task.OwnerIdentity),
		cancel:        cancel,
	}
	return ctx, cancel, true
}

func (r *Registry) finishRunningTask(runtimeID string, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	key := runtimeTaskKey(runtimeID, taskID)
	r.mu.Lock()
	delete(r.running, key)
	r.mu.Unlock()
}

func (r *Registry) stopRunningTask(runtimeID string, taskID string, ownerIdentity string) (bool, bool) {
	key := runtimeTaskKey(runtimeID, taskID)
	r.mu.Lock()
	defer r.mu.Unlock()
	task, exists := r.running[key]
	if !exists {
		return false, false
	}
	if task.ownerIdentity != strings.TrimSpace(ownerIdentity) {
		return true, false
	}
	task.cancel()
	return true, true
}

func (r *Registry) OpenClawAdapter() (*openClawAdapter, bool) {
	adapter := r.adapters["openclaw"]
	openClaw, ok := adapter.(*openClawAdapter)
	return openClaw, ok
}

func (r *Registry) SetOpenClawEcosystemPath(path string) (Info, error) {
	openClaw, ok := r.OpenClawAdapter()
	if !ok {
		return Info{}, fmt.Errorf("openclaw runtime is not registered")
	}
	if err := openClaw.setEcosystemPath(path); err != nil {
		return Info{}, err
	}
	return openClaw.Info(), nil
}

func (r *Registry) setUploadedOpenClawEcosystemPath(path string) (Info, error) {
	openClaw, ok := r.OpenClawAdapter()
	if !ok {
		return Info{}, fmt.Errorf("openclaw runtime is not registered")
	}
	if err := openClaw.setUploadedEcosystemPath(path); err != nil {
		return Info{}, err
	}
	return openClaw.Info(), nil
}

func (r *Registry) RefreshOpenClawEcosystem() (Info, error) {
	openClaw, ok := r.OpenClawAdapter()
	if !ok {
		return Info{}, fmt.Errorf("openclaw runtime is not registered")
	}
	openClaw.refreshEcosystemInventory()
	return openClaw.Info(), nil
}

func (r *Registry) Execute(ctx context.Context, runtimeID string, task Task) Result {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	if result, blocked := emergencyStopResult(runtimeID); blocked {
		return result
	}
	adapter := r.adapters[runtimeID]
	if adapter == nil {
		return Result{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     fmt.Sprintf("agent runtime %q is not registered", runtimeID),
			ExitCode:    -1,
			AuditEvents: []string{"runtime registry lookup failed"},
		}
	}
	info := adapter.Info()
	if !info.Enabled || !info.Configured || !info.ExecutionEnabled {
		return Result{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     runtimePolicyBlockMessage(info),
			ExitCode:    -1,
			AuditEvents: []string{"runtime registry policy blocked execution"},
		}
	}
	if strings.TrimSpace(task.ID) == "" {
		return blockedRuntimeResult(runtimeID, "agent runtime task id is required", "untracked agent task rejected")
	}
	if strings.TrimSpace(task.OwnerIdentity) == "" {
		return blockedRuntimeResult(runtimeID, "agent runtime task owner is required", "ownerless agent task rejected")
	}
	if info.RequiresApproval {
		if !task.HumanApproved {
			return blockedRuntimeResult(
				runtimeID,
				"agent runtime execution requires a server-side human approval record",
				"agent approval gate blocked execution",
			)
		}
		if err := validateRuntimeApprovalSource(task.ApprovalSourceID); err != nil {
			return blockedRuntimeResult(
				runtimeID,
				"agent runtime execution requires exact approval provenance: "+err.Error(),
				"agent approval provenance gate blocked execution",
			)
		}
	}
	if strings.TrimSpace(task.Prompt) == "" {
		return Result{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     "agent runtime task prompt is required",
			ExitCode:    -1,
			AuditEvents: []string{"empty agent task rejected"},
		}
	}
	if len(task.Prompt) > maxTaskPromptBytes {
		return Result{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     "agent runtime task prompt exceeds the 50 KiB execution boundary; store large context in HAI memory or connected sources instead",
			ExitCode:    -1,
			AuditEvents: []string{"oversized agent task rejected"},
		}
	}
	executionCtx, cancel, registered := r.registerRunningTask(ctx, runtimeID, task)
	if !registered {
		return Result{
			RuntimeID:   runtimeID,
			Status:      "blocked",
			Message:     "agent runtime task with this id is already running",
			ExitCode:    -1,
			AuditEvents: []string{"duplicate runtime task rejected"},
		}
	}
	defer cancel()
	defer r.finishRunningTask(runtimeID, task.ID)

	effectRequest := runtimeFinalEffectRequest(runtimeID, task, info)
	if validationErr := validateFinalEffectAuthorizationProof(effectRequest, task.FinalEffectProof); validationErr != nil {
		return finalEffectDeniedResult(runtimeID, validationErr.Error(), false)
	}
	if r.finalEffectVerifier == nil {
		return finalEffectDeniedResult(runtimeID, "proof verifier is not configured", true)
	}
	if err := r.finalEffectVerifier.VerifyFinalEffectProof(executionCtx, effectRequest, task.FinalEffectProof); err != nil {
		return finalEffectDeniedResult(runtimeID, "", true)
	}
	if executionCtx.Err() != nil {
		return blockedRuntimeResult(
			runtimeID,
			"agent runtime task was cancelled before adapter execution",
			"runtime cancellation observed after final-effect proof verification",
		)
	}
	// Re-evaluate after proof verification and immediately before the adapter
	// call. A stop engaged while durable verification was running wins over the
	// already-consumed receipt.
	if result, blocked := emergencyStopResult(runtimeID); blocked {
		result.AuditEvents = append(result.AuditEvents, "verified final-effect proof was not exercised")
		return result
	}

	result := adapter.ExecuteTask(executionCtx, task)
	result.AuditEvents = append(result.AuditEvents, "runtime adapter invoked with verified consumed authorization proof")
	if executionCtx.Err() == context.Canceled {
		result.RuntimeID = firstNonEmpty(result.RuntimeID, runtimeID)
		result.Status = "blocked"
		result.Message = "agent runtime task was cancelled by HAI before completion"
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
		result.AuditEvents = append(result.AuditEvents, "runtime registry cancellation observed")
	}
	return result
}

func validateRuntimeApprovalSource(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	for _, prefix := range []string{"task-review:", "workflow-decision:"} {
		if !strings.HasPrefix(sourceID, prefix) {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(sourceID, prefix))
		if err != nil || id == uuid.Nil {
			return fmt.Errorf("approval source must contain a valid decision UUID")
		}
		return nil
	}
	return fmt.Errorf("approval source type is not supported")
}

func blockedRuntimeResult(runtimeID, message, auditEvent string) Result {
	return Result{
		RuntimeID:   runtimeID,
		Status:      "blocked",
		Message:     message,
		ExitCode:    -1,
		AuditEvents: []string{auditEvent},
	}
}

func emergencyStopResult(runtimeID string) (Result, bool) {
	decision := safety.EvaluateEmergencyStop()
	if !decision.Active {
		return Result{}, false
	}
	return blockedRuntimeResult(
		runtimeID,
		decision.Reason,
		"emergency stop blocked agent runtime execution",
	), true
}

func runtimePolicyBlockMessage(info Info) string {
	reasons := []string{}
	if !info.Enabled {
		reasons = append(reasons, "runtime disabled")
	}
	if !info.Configured {
		reasons = append(reasons, "runtime not configured")
	}
	if !info.ExecutionEnabled {
		reasons = append(reasons, "execution disabled")
	}
	for _, missing := range info.MissingConfiguration {
		missing = strings.TrimSpace(missing)
		if missing != "" {
			reasons = append(reasons, missing)
		}
	}
	reasons = sortedUnique(reasons)
	if len(reasons) == 0 {
		return "agent runtime is disabled or incomplete; review runtime registry configuration"
	}
	return safety.RedactSecrets("agent runtime blocked by registry policy: " + strings.Join(reasons, "; "))
}

func unsupportedStopTask(runtimeID string, taskID string, reason string) StopResult {
	runtimeID = strings.TrimSpace(runtimeID)
	taskID = strings.TrimSpace(taskID)
	return StopResult{
		RuntimeID: runtimeID,
		TaskID:    taskID,
		Status:    "unsupported",
		Message:   firstNonEmpty(reason, "runtime does not expose a safe durable stop-task operation through HAI yet"),
		AuditEvents: []string{
			"stop request recorded",
			"runtime has no HAI-managed durable process handle to stop",
			"timeout, workspace, and downstream runtime policies remain the active containment controls",
		},
	}
}

type hermesAdapter struct {
	enabled       bool
	executable    string
	home          string
	profile       string
	workspace     string
	workspaceRoot string
	maxTurns      int
	timeout       time.Duration
	toolsets      []string
	skills        []string
	envAllow      []string
	outputLimit   int64

	ignoreUserConfig  bool
	gatewayEnabled    bool
	cronEnabled       bool
	mcpEnabled        bool
	moaEnabled        bool
	subagentsEnabled  bool
	memorySyncEnabled bool
	acpEnabled        bool
	terminalBackends  []string
}

func newHermesAdapterFromEnv() *hermesAdapter {
	return &hermesAdapter{
		enabled:           envEnabled("HERMES_AGENT_ENABLED"),
		executable:        firstNonEmpty(os.Getenv("HERMES_EXECUTABLE"), "hermes"),
		home:              strings.TrimSpace(os.Getenv("HERMES_HOME")),
		profile:           strings.TrimSpace(os.Getenv("HERMES_PROFILE")),
		workspace:         strings.TrimSpace(os.Getenv("HERMES_WORKSPACE")),
		workspaceRoot:     strings.TrimSpace(os.Getenv("AGENT_RUNTIME_WORKSPACE_ROOT")),
		maxTurns:          boundedIntEnv("HERMES_MAX_TURNS", 20, 1, 100),
		timeout:           time.Duration(boundedIntEnv("HERMES_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second,
		toolsets:          csvValues(os.Getenv("HERMES_TOOLSETS")),
		skills:            csvValues(os.Getenv("HERMES_SKILLS")),
		envAllow:          csvValues(os.Getenv("HERMES_ENV_ALLOWLIST")),
		outputLimit:       int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		ignoreUserConfig:  envEnabled("HERMES_IGNORE_USER_CONFIG"),
		gatewayEnabled:    envEnabled("HERMES_GATEWAY_ENABLED"),
		cronEnabled:       envEnabled("HERMES_CRON_ENABLED"),
		mcpEnabled:        envEnabled("HERMES_MCP_ENABLED"),
		moaEnabled:        envEnabled("HERMES_MOA_ENABLED"),
		subagentsEnabled:  envEnabled("HERMES_SUBAGENTS_ENABLED"),
		memorySyncEnabled: envEnabled("HERMES_MEMORY_SYNC_ENABLED"),
		acpEnabled:        envEnabled("HERMES_ACP_ENABLED"),
		terminalBackends:  csvValues(firstNonEmpty(os.Getenv("HERMES_TERMINAL_BACKENDS"), "local,docker,ssh,singularity,modal,daytona")),
	}
}

func (a *hermesAdapter) Info() Info {
	missing := []string{}
	if strings.TrimSpace(a.executable) == "" {
		missing = append(missing, "HERMES_EXECUTABLE")
	}
	if strings.TrimSpace(a.workspace) == "" {
		missing = append(missing, "HERMES_WORKSPACE")
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		missing = append(missing, "AGENT_RUNTIME_WORKSPACE_ROOT")
	}
	workspaceReason := a.workspaceBlockedReason()
	return Info{
		ID:                   "hermes",
		Name:                 "Hermes Agent",
		Type:                 "hermes",
		Enabled:              a.enabled,
		Configured:           len(missing) == 0 && workspaceReason == "",
		ExecutionEnabled:     a.enabled && len(missing) == 0 && workspaceReason == "",
		RequiresApproval:     true,
		ReadOnlyDefault:      true,
		Capabilities:         a.capabilities(),
		Architecture:         a.architecture(),
		Controls:             a.controls(),
		MissingConfiguration: missing,
		Endpoint:             a.executable,
	}
}

func (a *hermesAdapter) HealthCheck(ctx context.Context) Health {
	started := time.Now()
	health := Health{RuntimeID: "hermes", Status: "disabled", CheckedAt: time.Now().UTC()}
	if !a.enabled {
		health.Reason = "HERMES_AGENT_ENABLED is false"
		return health
	}
	if strings.TrimSpace(a.workspace) == "" {
		health.Status = "blocked"
		health.Reason = "HERMES_WORKSPACE is required"
		return health
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if stat, err := os.Stat(a.workspace); err != nil || !stat.IsDir() {
		health.Status = "blocked"
		health.Reason = "Hermes workspace is not an accessible directory"
		return health
	}
	path, err := exec.LookPath(a.executable)
	if err != nil {
		health.Status = "unavailable"
		health.Reason = "Hermes executable was not found"
		return health
	}
	health.Status = "ready"
	health.Reason = "Hermes executable and workspace are available: " + filepath.Base(path) + "; " + strings.Join(a.ecosystemReadiness(), ", ")
	health.LatencyMs = time.Since(started).Milliseconds()
	return health
}

func (a *hermesAdapter) ListSkills(context.Context) []Skill {
	skills := []Skill{}
	for _, skill := range sortedUnique(a.skills) {
		skills = append(skills, Skill{
			ID:               "hermes:skill:" + skill,
			RuntimeID:        "hermes",
			Name:             skill,
			Category:         "skill",
			RiskLevel:        "medium",
			ApprovalRequired: true,
			ExecutionMode:    "approved_cli_skill",
			Source:           "HERMES_SKILLS",
			Description:      "Configured Hermes skill surfaced through HAI's approval-gated runtime path.",
			Tags:             []string{"configured", "hermes"},
		})
	}
	for _, toolset := range sortedUnique(a.toolsets) {
		skills = append(skills, Skill{
			ID:               "hermes:toolset:" + toolset,
			RuntimeID:        "hermes",
			Name:             toolset,
			Category:         "toolset",
			RiskLevel:        "medium",
			ApprovalRequired: true,
			ExecutionMode:    "approved_cli_toolset",
			Source:           "HERMES_TOOLSETS",
			Description:      "Configured Hermes toolset. HAI still requires a server-side approval before execution.",
			Tags:             []string{"configured", "toolset", "hermes"},
		})
	}
	return skills
}

func (a *hermesAdapter) ExecuteTask(parent context.Context, task Task) Result {
	started := time.Now()
	if result, blocked := emergencyStopResult("hermes"); blocked {
		return result
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		return Result{RuntimeID: "hermes", Status: "blocked", Message: reason, ExitCode: -1}
	}
	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()

	args := []string{"chat", "-q", task.Prompt, "-Q", "--source", "tool", "--max-turns", strconv.Itoa(a.maxTurns), "--checkpoints"}
	if len(a.toolsets) > 0 {
		args = append(args, "--toolsets", strings.Join(a.toolsets, ","))
	}
	if len(a.skills) > 0 {
		args = append(args, "--skills", strings.Join(a.skills, ","))
	}
	cmd := exec.CommandContext(ctx, a.executable, args...)
	processcontrol.Configure(cmd)
	cmd.Dir = a.workspace
	envAdditions := map[string]string{
		"HAI_RUNTIME_TASK_ID": task.ID,
		"HAI_PROJECT_KEY":     task.ProjectKey,
		"TERMINAL_CWD":        a.workspace,
	}
	if a.home != "" {
		envAdditions["HERMES_HOME"] = a.home
	}
	if a.profile != "" {
		envAdditions["HERMES_PROFILE"] = a.profile
	}
	if a.ignoreUserConfig {
		envAdditions["HERMES_IGNORE_USER_CONFIG"] = "1"
	}
	cmd.Env = safeEnvironment(a.envAllow, envAdditions)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &stdout, remaining: a.outputLimit}
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: a.outputLimit / 4}
	if result, blocked := emergencyStopResult("hermes"); blocked {
		return result
	}
	err := cmd.Run()
	output := trimAndRedact(stdout.String(), a.outputLimit)
	message := "Hermes completed the approved agent task"
	exitCode := 0
	status := "completed"
	if err != nil {
		status = "failed"
		message = safety.RedactSecrets(strings.TrimSpace(stderr.String()))
		if message == "" {
			message = err.Error()
		}
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			status = "blocked"
			message = "Hermes execution exceeded the configured timeout and was stopped"
		} else if ctx.Err() == context.Canceled {
			status = "blocked"
			message = "Hermes execution was canceled and its process tree was stopped"
		}
	}
	return Result{
		RuntimeID:  "hermes",
		Status:     status,
		Message:    message,
		Output:     output,
		ExitCode:   exitCode,
		DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: []string{
			"server-side approval verified",
			"Hermes invoked without shell interpolation or --yolo",
			"filesystem checkpoints enabled",
			"Hermes ecosystem constrained through configured toolsets, skills, workspace, and HAI approval gates",
			"runtime cancellation terminates the Hermes process tree",
			"bounded runtime output captured",
		},
	}
}

func (a *hermesAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return unsupportedStopTask("hermes", taskID, "Hermes runs are currently bounded by per-task process timeouts; no durable Hermes stop handle is stored by HAI yet")
}

func (a *hermesAdapter) capabilities() []string {
	return []string{
		"noninteractive chat execution",
		"model/provider routing",
		"toolsets",
		"skills and skill learning",
		"memory and user profile",
		"session search",
		"MCP servers and tools",
		"gateway channels: Telegram, Discord, Slack, WhatsApp, Signal, email, Matrix, SMS",
		"cron/scheduled automations",
		"subagent delegation",
		"mixture-of-agents orchestration",
		"terminal backends: " + strings.Join(a.terminalBackends, ", "),
		"browser, file, terminal, web, vision, TTS, todo toolsets",
		"context compression and prompt caching",
		"OpenClaw migration",
		"ACP adapter",
		"filesystem checkpoints",
	}
}

func (a *hermesAdapter) architecture() []string {
	return []string{
		"HAI workflow approval queue",
		"HAI agent-runtime registry",
		"Hermes noninteractive CLI",
		"Hermes toolset and skill system",
		"Hermes memory and session stores",
		"Hermes gateway and cron surfaces",
		"Hermes MCP and ACP bridges",
		"HAI source-grounded verification and audit log",
	}
}

func (a *hermesAdapter) controls() []string {
	controls := []string{
		"disabled by default through HERMES_AGENT_ENABLED",
		"server-side HAI approval required before every task",
		"dedicated workspace must remain under AGENT_RUNTIME_WORKSPACE_ROOT",
		"invoked without shell interpolation and never with --yolo",
		"filesystem checkpoints forced on every run",
		"bounded timeout and output capture with secret redaction",
		"environment inheritance limited to HERMES_ENV_ALLOWLIST plus HAI task metadata",
	}
	if len(a.toolsets) > 0 {
		controls = append(controls, "tool surface constrained to HERMES_TOOLSETS="+strings.Join(a.toolsets, ","))
	} else {
		controls = append(controls, "Hermes tool surface follows its configured platform_toolsets")
	}
	if len(a.skills) > 0 {
		controls = append(controls, "preloaded skills constrained to HERMES_SKILLS="+strings.Join(a.skills, ","))
	}
	if a.home != "" {
		controls = append(controls, "Hermes state scoped with HERMES_HOME")
	}
	if a.profile != "" {
		controls = append(controls, "Hermes profile selected with HERMES_PROFILE")
	}
	if a.ignoreUserConfig {
		controls = append(controls, "user config ignored through HERMES_IGNORE_USER_CONFIG")
	}
	return controls
}

func (a *hermesAdapter) ecosystemReadiness() []string {
	return []string{
		"gateway=" + boolLabel(a.gatewayEnabled),
		"cron=" + boolLabel(a.cronEnabled),
		"mcp=" + boolLabel(a.mcpEnabled),
		"moa=" + boolLabel(a.moaEnabled),
		"subagents=" + boolLabel(a.subagentsEnabled),
		"memory-sync=" + boolLabel(a.memorySyncEnabled),
		"acp=" + boolLabel(a.acpEnabled),
	}
}

func (a *hermesAdapter) workspaceBlockedReason() string {
	if strings.TrimSpace(a.workspace) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return ""
	}
	root, err := filepath.Abs(filepath.Clean(a.workspaceRoot))
	if err != nil {
		return "agent runtime workspace root is invalid"
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "agent runtime workspace root is not accessible"
	}
	workspace, err := filepath.Abs(filepath.Clean(a.workspace))
	if err != nil {
		return "Hermes workspace is invalid"
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return "Hermes workspace is not accessible"
	}
	relative, err := filepath.Rel(root, workspace)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "Hermes workspace must stay inside AGENT_RUNTIME_WORKSPACE_ROOT"
	}
	return ""
}

type openClawAdapter struct {
	enabled        bool
	executable     string
	workspace      string
	workspaceRoot  string
	ecosystemPath  string
	ecosystemRoots []string
	stateDir       string
	configPath     string
	gatewayURL     string
	gatewayToken   string
	thinking       string
	timeout        time.Duration
	outputLimit    int64
	envAllow       []string
	allowedHost    map[string]bool

	agentCLIEnabled    bool
	gatewayEnabled     bool
	messagesEnabled    bool
	skillsEnabled      bool
	pluginsEnabled     bool
	mcpEnabled         bool
	memoryEnabled      bool
	cronEnabled        bool
	browserEnabled     bool
	canvasEnabled      bool
	nodesEnabled       bool
	voiceEnabled       bool
	talkEnabled        bool
	webchatEnabled     bool
	pairingEnabled     bool
	execApprovals      bool
	hostToolsEnabled   bool
	publicPosting      bool
	webSearchEnabled   bool
	multiAgentEnabled  bool
	appSDKEnabled      bool
	pluginSDKEnabled   bool
	localModelsEnabled bool
	highRiskExecution  bool
	sandboxRequired    bool
	sandboxMode        string
	sandboxDocker      bool
	sandboxSSH         bool
	sandboxOpenShell   bool
	channelsEnabled    []string
	providersEnabled   []string
	companionApps      []string

	inventoryMu        sync.Mutex
	inventoryLoaded    bool
	inventoryPath      string
	inventorySignature string
	inventory          openClawEcosystemInventory
}

// RuntimeID lets registry composition identify this adapter without scanning
// the configured OpenClaw ecosystem. Full inventory remains lazy until an
// operator explicitly opens runtime detail.
func (*openClawAdapter) RuntimeID() string { return "openclaw" }

func newOpenClawAdapterFromEnv() *openClawAdapter {
	return &openClawAdapter{
		enabled:          envEnabled("OPENCLAW_AGENT_ENABLED"),
		executable:       firstNonEmpty(os.Getenv("OPENCLAW_EXECUTABLE"), "openclaw"),
		workspace:        strings.TrimSpace(os.Getenv("OPENCLAW_WORKSPACE")),
		workspaceRoot:    strings.TrimSpace(os.Getenv("AGENT_RUNTIME_WORKSPACE_ROOT")),
		ecosystemPath:    strings.TrimSpace(firstNonEmpty(os.Getenv("OPENCLAW_ECOSYSTEM_PATH"), os.Getenv("OPENCLAW_WORKSPACE"))),
		ecosystemRoots:   csvValues(os.Getenv("OPENCLAW_ECOSYSTEM_ALLOWED_ROOTS")),
		stateDir:         strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR")),
		configPath:       strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")),
		gatewayURL:       strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_URL")),
		gatewayToken:     strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")),
		thinking:         firstNonEmpty(os.Getenv("OPENCLAW_THINKING"), "high"),
		timeout:          time.Duration(boundedIntEnv("OPENCLAW_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second,
		outputLimit:      int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		envAllow:         csvValues(os.Getenv("OPENCLAW_ENV_ALLOWLIST")),
		allowedHost:      csvMap(firstNonEmpty(os.Getenv("AGENT_RUNTIME_ALLOWED_HOSTS"), "localhost,127.0.0.1,::1,host.docker.internal,openclaw")),
		agentCLIEnabled:  envEnabledDefault("OPENCLAW_AGENT_CLI_ENABLED", true),
		gatewayEnabled:   envEnabled("OPENCLAW_GATEWAY_ENABLED"),
		messagesEnabled:  envEnabled("OPENCLAW_MESSAGES_ENABLED"),
		skillsEnabled:    envEnabled("OPENCLAW_SKILLS_ENABLED"),
		pluginsEnabled:   envEnabled("OPENCLAW_PLUGINS_ENABLED"),
		mcpEnabled:       envEnabled("OPENCLAW_MCP_ENABLED"),
		memoryEnabled:    envEnabled("OPENCLAW_MEMORY_ENABLED"),
		cronEnabled:      envEnabled("OPENCLAW_CRON_ENABLED"),
		browserEnabled:   envEnabled("OPENCLAW_BROWSER_ENABLED"),
		canvasEnabled:    envEnabled("OPENCLAW_CANVAS_ENABLED"),
		nodesEnabled:     envEnabled("OPENCLAW_NODES_ENABLED"),
		voiceEnabled:     envEnabled("OPENCLAW_VOICE_ENABLED"),
		talkEnabled:      envEnabled("OPENCLAW_TALK_ENABLED"),
		webchatEnabled:   envEnabled("OPENCLAW_WEBCHAT_ENABLED"),
		pairingEnabled:   envEnabled("OPENCLAW_PAIRING_ENABLED"),
		execApprovals:    envEnabled("OPENCLAW_EXEC_APPROVALS_ENABLED"),
		hostToolsEnabled: envEnabled("OPENCLAW_HOST_TOOLS_ENABLED"),
		publicPosting:    envEnabled("OPENCLAW_PUBLIC_POSTING_ENABLED"),
		webSearchEnabled: envEnabled("OPENCLAW_WEB_SEARCH_ENABLED"),

		multiAgentEnabled:  envEnabled("OPENCLAW_MULTI_AGENT_ENABLED"),
		appSDKEnabled:      envEnabled("OPENCLAW_APP_SDK_ENABLED"),
		pluginSDKEnabled:   envEnabled("OPENCLAW_PLUGIN_SDK_ENABLED"),
		localModelsEnabled: envEnabled("OPENCLAW_LOCAL_MODELS_ENABLED"),
		highRiskExecution:  envEnabled("OPENCLAW_ALLOW_HIGH_RISK_EXECUTION"),
		sandboxRequired:    envEnabledDefault("OPENCLAW_SANDBOX_REQUIRED", true),
		sandboxMode:        firstNonEmpty(os.Getenv("OPENCLAW_SANDBOX_MODE"), "all"),
		sandboxDocker:      envEnabled("OPENCLAW_SANDBOX_DOCKER_ENABLED"),
		sandboxSSH:         envEnabled("OPENCLAW_SANDBOX_SSH_ENABLED"),
		sandboxOpenShell:   envEnabled("OPENCLAW_SANDBOX_OPENSHELL_ENABLED"),
		channelsEnabled:    csvValues(os.Getenv("OPENCLAW_CHANNELS_ENABLED")),
		providersEnabled:   csvValues(os.Getenv("OPENCLAW_PROVIDERS_ENABLED")),
		companionApps:      csvValues(firstNonEmpty(os.Getenv("OPENCLAW_COMPANION_APPS"), "windows,macos,ios,android")),
	}
}

func (a *openClawAdapter) Info() Info {
	missing := []string{}
	if strings.TrimSpace(a.executable) == "" {
		missing = append(missing, "OPENCLAW_EXECUTABLE")
	}
	if strings.TrimSpace(a.workspace) == "" {
		missing = append(missing, "OPENCLAW_WORKSPACE")
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		missing = append(missing, "AGENT_RUNTIME_WORKSPACE_ROOT")
	}
	if a.gatewayEnabled && strings.TrimSpace(a.gatewayToken) == "" {
		missing = append(missing, "OPENCLAW_GATEWAY_TOKEN")
	}
	gatewayReason := a.validGatewayURL()
	workspaceReason := a.workspaceBlockedReason()
	if workspaceReason != "" {
		missing = append(missing, workspaceReason)
	}
	if gatewayReason != "" {
		missing = append(missing, gatewayReason)
	}
	if !a.agentCLIEnabled {
		missing = append(missing, "OPENCLAW_AGENT_CLI_ENABLED")
	}
	if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
		missing = append(missing, blocked...)
	}
	configured := len(missing) == 0 && workspaceReason == "" && gatewayReason == "" && a.agentCLIEnabled
	return Info{
		ID:                   "openclaw",
		Name:                 "OpenClaw Gateway Agent",
		Type:                 "openclaw",
		Enabled:              a.enabled,
		Configured:           configured,
		ExecutionEnabled:     a.enabled && configured,
		RequiresApproval:     true,
		ReadOnlyDefault:      true,
		Capabilities:         a.capabilities(),
		Architecture:         a.architecture(),
		Controls:             a.controls(),
		Ecosystem:            a.ecosystem(),
		EcosystemPath:        a.ecosystemPath,
		MissingConfiguration: missing,
		Endpoint:             safety.RedactURL(firstNonEmpty(a.gatewayURL, a.executable)),
	}
}

func (a *openClawAdapter) HealthCheck(ctx context.Context) Health {
	started := time.Now()
	health := Health{RuntimeID: "openclaw", Status: "disabled", CheckedAt: time.Now().UTC()}
	if !a.enabled {
		health.Reason = "OPENCLAW_AGENT_ENABLED is false"
		return health
	}
	if !a.agentCLIEnabled {
		health.Status = "blocked"
		health.Reason = "OPENCLAW_AGENT_CLI_ENABLED is false"
		return health
	}
	if strings.TrimSpace(a.workspace) == "" {
		health.Status = "blocked"
		health.Reason = "OPENCLAW_WORKSPACE is required"
		return health
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if stat, err := os.Stat(a.workspace); err != nil || !stat.IsDir() {
		health.Status = "blocked"
		health.Reason = "OpenClaw workspace is not an accessible directory"
		return health
	}
	if reason := a.validGatewayURL(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if a.gatewayEnabled && a.gatewayToken == "" {
		health.Status = "blocked"
		health.Reason = "OPENCLAW_GATEWAY_TOKEN is required when OPENCLAW_GATEWAY_ENABLED=true"
		return health
	}
	if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
		health.Status = "blocked"
		health.Reason = strings.Join(blocked, "; ")
		return health
	}
	path, err := exec.LookPath(a.executable)
	if err != nil {
		health.Status = "unavailable"
		health.Reason = "OpenClaw executable was not found"
		return health
	}
	select {
	case <-ctx.Done():
		health.Status = "blocked"
		health.Reason = "OpenClaw health check was cancelled"
		return health
	default:
	}
	health.Status = "ready"
	health.Reason = "OpenClaw executable and workspace are available: " + filepath.Base(path) + "; " + strings.Join(a.ecosystemReadiness(), ", ")
	health.LatencyMs = time.Since(started).Milliseconds()
	return health
}

func (a *openClawAdapter) ListSkills(context.Context) []Skill {
	inventory := a.ecosystemInventory()
	skills := []Skill{}
	for _, name := range sortedUnique(inventory.skills) {
		skills = append(skills, Skill{
			ID:               "openclaw:skill:" + name,
			RuntimeID:        "openclaw",
			Name:             name,
			Category:         "skill",
			RiskLevel:        "medium",
			ApprovalRequired: true,
			ExecutionMode:    "approved_agent_cli_envelope",
			Source:           "OPENCLAW_ECOSYSTEM_PATH",
			Description:      "Indexed OpenClaw skill available for HAI task-envelope planning. Execution remains routed through the approved OpenClaw agent CLI path.",
			Tags:             []string{"openclaw", "skill"},
		})
	}
	for _, name := range sortedUnique(inventory.skillScripts) {
		skills = append(skills, Skill{
			ID:               "openclaw:script:" + name,
			RuntimeID:        "openclaw",
			Name:             name,
			Category:         "skill_script",
			RiskLevel:        "high",
			ApprovalRequired: true,
			ExecutionMode:    "catalog_only_not_directly_invoked",
			Source:           "OPENCLAW_ECOSYSTEM_PATH",
			Description:      "Execution-capable OpenClaw skill script cataloged for operator review. HAI does not invoke this script directly.",
			Tags:             []string{"openclaw", "script", "high-risk"},
		})
	}
	return skills
}

func (a *openClawAdapter) ExecuteTask(parent context.Context, task Task) Result {
	started := time.Now()
	if result, blocked := emergencyStopResult("openclaw"); blocked {
		return result
	}
	if !a.agentCLIEnabled {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: "OPENCLAW_AGENT_CLI_ENABLED is false", ExitCode: -1}
	}
	if reason := a.workspaceBlockedReason(); reason != "" {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: reason, ExitCode: -1}
	}
	if reason := a.validGatewayURL(); reason != "" {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: reason, ExitCode: -1}
	}
	if a.gatewayEnabled && a.gatewayToken == "" {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: "OPENCLAW_GATEWAY_TOKEN is required when OPENCLAW_GATEWAY_ENABLED=true", ExitCode: -1}
	}
	if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: strings.Join(blocked, "; "), ExitCode: -1}
	}

	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()
	profile := a.openClawTaskProfile(task)
	envelope := openClawTaskEnvelope(task, profile)
	args := []string{"agent", "--message", envelope}
	if a.thinking != "" {
		args = append(args, "--thinking", a.thinking)
	}
	cmd := exec.CommandContext(ctx, a.executable, args...)
	processcontrol.Configure(cmd)
	cmd.Dir = a.workspace
	envAdditions := map[string]string{
		"HAI_RUNTIME_TASK_ID":    task.ID,
		"HAI_PROJECT_KEY":        task.ProjectKey,
		"OPENCLAW_HOME":          a.stateDir,
		"OPENCLAW_STATE_DIR":     a.stateDir,
		"OPENCLAW_CONFIG_PATH":   a.configPath,
		"OPENCLAW_GATEWAY_TOKEN": a.gatewayToken,
	}
	cmd.Env = safeEnvironment(a.envAllow, envAdditions)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &stdout, remaining: a.outputLimit}
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: a.outputLimit / 4}
	if result, blocked := emergencyStopResult("openclaw"); blocked {
		return result
	}
	err := cmd.Run()
	output := trimAndRedact(stdout.String(), a.outputLimit)
	message := "OpenClaw completed the approved agent task"
	exitCode := 0
	status := "completed"
	if err != nil {
		status = "failed"
		message = safety.RedactSecrets(strings.TrimSpace(stderr.String()))
		if message == "" {
			message = err.Error()
		}
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			status = "blocked"
			message = "OpenClaw execution exceeded the configured timeout and was stopped"
		} else if ctx.Err() == context.Canceled {
			status = "blocked"
			message = "OpenClaw execution was canceled and its process tree was stopped"
		}
	}
	return Result{
		RuntimeID:  "openclaw",
		Status:     status,
		Message:    message,
		Output:     output,
		RouteTrace: openClawRouteTrace(profile),
		ExitCode:   exitCode,
		DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: []string{
			"server-side approval verified",
			"OpenClaw invoked through noninteractive agent CLI without shell interpolation",
			"HAI task envelope selected relevant OpenClaw skills and carried safety constraints into the runtime prompt",
			"OpenClaw messaging, public posting, nodes, browser, cron, and host tools are not invoked by this adapter",
			"OpenClaw Gateway auth, pairing, scopes, sandboxing, and tool approvals remain authoritative",
			"dedicated workspace, timeout, output limit, environment allowlist, and secret redaction enforced by HAI",
			"runtime cancellation terminates the OpenClaw process tree",
		},
	}
}

func (a *openClawAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return unsupportedStopTask("openclaw", taskID, "OpenClaw tasks are currently invoked as bounded noninteractive CLI runs; HAI does not persist a runtime process/session handle for external stop yet")
}

type openClawTaskProfile struct {
	Intent              string
	ExecutionMode       string
	RiskLevel           string
	RecommendedSkills   []string
	VisibleProviders    []string
	VisibleTools        []string
	RelevantMaps        []string
	BlockedSurfaces     []string
	RequiredControls    []string
	ValidationChecklist []string
}

func (a *openClawAdapter) openClawTaskEnvelope(task Task) string {
	return openClawTaskEnvelope(task, a.openClawTaskProfile(task))
}

func openClawTaskEnvelope(task Task, profile openClawTaskProfile) string {
	var builder strings.Builder
	builder.WriteString("HAI approved OpenClaw task envelope\n")
	builder.WriteString("Runtime role: execute only the approved task below through OpenClaw's noninteractive agent path.\n")
	if task.ID != "" {
		builder.WriteString("HAI task id: " + task.ID + "\n")
	}
	if task.ProjectKey != "" {
		builder.WriteString("HAI project key: " + task.ProjectKey + "\n")
	}
	builder.WriteString("Intent: " + profile.Intent + "\n")
	builder.WriteString("Execution mode: " + profile.ExecutionMode + "\n")
	builder.WriteString("Risk level: " + profile.RiskLevel + "\n")
	builder.WriteString("Recommended OpenClaw skills: " + joinOrNone(profile.RecommendedSkills) + "\n")
	builder.WriteString("Visible provider extensions: " + joinOrNone(profile.VisibleProviders) + "\n")
	builder.WriteString("Visible tool/runtime extensions: " + joinOrNone(profile.VisibleTools) + "\n")
	builder.WriteString("Relevant OpenClaw maps: " + joinOrNone(profile.RelevantMaps) + "\n")
	builder.WriteString("Blocked surfaces: " + joinOrNone(profile.BlockedSurfaces) + "\n")
	builder.WriteString("Required controls: " + joinOrNone(profile.RequiredControls) + "\n")
	builder.WriteString("Validation checklist: " + joinOrNone(profile.ValidationChecklist) + "\n")
	builder.WriteString("\nOriginal request:\n")
	builder.WriteString(task.Prompt)
	builder.WriteString("\n\nReturn format: concise completion summary, actions taken, verification evidence, blocked items, and next safe action. Do not claim external effects unless they actually happened through an approved tool path.\n")
	return builder.String()
}

func openClawRouteTrace(profile openClawTaskProfile) *RouteTrace {
	return &RouteTrace{
		RuntimeID:           "openclaw",
		Intent:              profile.Intent,
		ExecutionMode:       profile.ExecutionMode,
		RiskLevel:           profile.RiskLevel,
		RecommendedSkills:   sortedUnique(profile.RecommendedSkills),
		VisibleProviders:    sortedUnique(profile.VisibleProviders),
		VisibleTools:        sortedUnique(profile.VisibleTools),
		RelevantMaps:        sortedUnique(profile.RelevantMaps),
		BlockedSurfaces:     sortedUnique(profile.BlockedSurfaces),
		RequiredControls:    sortedUnique(profile.RequiredControls),
		ValidationChecklist: sortedUnique(profile.ValidationChecklist),
	}
}

func (a *openClawAdapter) openClawTaskProfile(task Task) openClawTaskProfile {
	inventory := a.ecosystemInventory()
	prompt := strings.ToLower(task.Prompt)
	intent := "general autonomous assistance"
	risk := "medium"
	if containsAny(prompt, "legal", "lawyer", "court", "government", "municipality", "insurance", "contract") {
		intent = "regulated or legal-sensitive workflow support"
		risk = "high"
	} else if containsAny(prompt, "github", "pull request", "commit", "code", "bug", "test", "readme", "review") {
		intent = "software engineering and repository workflow"
		risk = "medium"
	} else if containsAny(prompt, "pursuit", "open loop", "next action", "blocked", "waiting", "approval", "decision", "delegate", "va-ready", "follow-up") {
		intent = "HAI pursuit and open-loop operations"
		risk = "medium"
	} else if containsAny(prompt, "deadline", "calendar", "reminder", "appointment", "hearing", "due date", "schedule") {
		intent = "deadline and calendar preparation"
		risk = "medium"
	} else if containsAny(prompt, "memory", "context", "source", "evidence", "timeline", "claim", "citation", "provenance", "ingestion", "extract") {
		intent = "source-grounded memory and evidence workflow"
		risk = "medium"
	} else if containsAny(prompt, "odoo", "erp", "herp", "invoice", "crm", "sales", "quote", "client", "operation") {
		intent = "personal operations and HERP workflow"
		risk = "medium"
	} else if containsAny(prompt, "ollama", "local model", "provider", "llm", "qwen", "deepseek", "llama", "mistral", "gemma", "phi") {
		intent = "local model and provider routing setup"
		risk = "medium"
	} else if containsAny(prompt, "whatsapp", "telegram", "slack", "discord", "email", "message", "reply", "send", "post", "publish") {
		intent = "communication drafting and channel workflow"
		risk = "high"
	} else if containsAny(prompt, "document", "docs", "pdf", "summary", "documentation", "transcript") {
		intent = "document and knowledge workflow"
	}

	skills := []string{}
	if containsAny(prompt, "github", "pull request", "repo", "commit", "branch") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "gitcrawl", "autoreview", "openclaw-pr-maintainer", "tag-duplicate-prs-issues")...)
	}
	if containsAny(prompt, "code", "bug", "test", "qa", "debug", "review") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "autoreview", "openclaw-debugging", "openclaw-testing", "openclaw-qa-testing", "openclaw-small-bugfix-sweep", "security-triage")...)
	}
	if containsAny(prompt, "security", "secret", "vulnerability", "risk") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "security-triage", "openclaw-secret-scanning-maintainer")...)
	}
	if containsAny(prompt, "pursuit", "open loop", "next action", "blocked", "waiting", "approval", "decision", "delegate", "va-ready", "follow-up", "task") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "taskflow", "agent-transcript", "technical-documentation", "channel-message-flows", "openclaw-qa-testing")...)
	}
	if containsAny(prompt, "memory", "context", "source", "evidence", "timeline", "claim", "citation", "provenance", "ingestion", "extract", "document", "pdf") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "technical-documentation", "agent-transcript", "claw-score", "document-extract", "memory", "taskflow")...)
	}
	if containsAny(prompt, "deadline", "calendar", "reminder", "appointment", "hearing", "due date", "schedule") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "taskflow", "channel-message-flows", "technical-documentation")...)
	}
	if containsAny(prompt, "odoo", "erp", "herp", "invoice", "crm", "sales", "quote", "client", "operation") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "taskflow", "technical-documentation", "agent-transcript", "openclaw-qa-testing")...)
	}
	if containsAny(prompt, "ollama", "local model", "provider", "llm", "qwen", "deepseek", "llama", "mistral", "gemma", "phi") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "claw-score", "technical-documentation", "taskflow")...)
	}
	if containsAny(prompt, "document", "docs", "readme", "documentation", "transcript", "summary") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "technical-documentation", "agent-transcript", "openclaw-refactor-docs")...)
	}
	if containsAny(prompt, "whatsapp", "telegram", "slack", "discord", "message", "reply", "channel") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "channel-message-flows", "slacrawl", "discrawl", "notcrawl", "telegram", "discord")...)
	}
	if containsAny(prompt, "release", "changelog", "announcement") {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "openclaw-changelog-update", "release-openclaw-maintainer", "release-openclaw-announcement", "release-openclaw-ci")...)
	}
	if len(skills) == 0 {
		skills = append(skills, matchingOpenClawItems(inventory.skills, "taskflow", "technical-documentation", "autoreview")...)
	}
	maps := []string{}
	if containsAny(prompt, "security", "secret", "vulnerability", "risk", "codeql", "ssrf", "auth", "token") {
		maps = append(maps, prefixOpenClawMaps("security", inventory.githubSecurityMaps)...)
		maps = append(maps, prefixOpenClawMaps("security-asset", matchingOpenClawItems(inventory.securityAssets, "policy", "scan", "secret", "security", "auth", "token", "guard"))...)
		maps = append(maps, prefixOpenClawMaps("codeql", matchingOpenClawItems(inventory.githubSecurityMaps, "security", "secrets", "auth", "ssrf", "runtime", "boundary"))...)
	}
	if containsAny(prompt, "ci", "workflow", "github action", "release", "build", "test", "e2e", "smoke") {
		maps = append(maps, prefixOpenClawMaps("github-action", matchingOpenClawItems(inventory.githubActions, "ci", "release", "test", "e2e", "smoke", "performance", "security", "setup"))...)
		maps = append(maps, prefixOpenClawMaps("workflow", matchingOpenClawItems(inventory.githubWorkflows, "ci", "release", "test", "e2e", "smoke", "performance", "security"))...)
		maps = append(maps, prefixOpenClawMaps("qa", matchingOpenClawItems(inventory.qaAssets, "e2e", "smoke", "proof", "profile", "channel", "matrix", "live"))...)
		maps = append(maps, prefixOpenClawMaps("test", matchingOpenClawItems(inventory.testSuites, "e2e", "smoke", "performance", "release", "security", "plugin"))...)
	}
	if containsAny(prompt, "issue", "bug report", "feature request", "triage") {
		maps = append(maps, prefixOpenClawMaps("issue-template", inventory.githubIssues)...)
	}
	if containsAny(prompt, "instruction", "copilot", "codex prompt", "docs", "documentation") {
		maps = append(maps, prefixOpenClawMaps("instruction", inventory.githubInstructions)...)
		maps = append(maps, prefixOpenClawMaps("codex-prompt", matchingOpenClawItems(inventory.codexPrompts, "docs", "maturity", "performance"))...)
		maps = append(maps, prefixOpenClawMaps("doc", matchingOpenClawItems(inventory.docs, "architecture", "gateway", "agent", "provider", "plugin", "install", "security", "memory"))...)
	}
	if containsAny(prompt, "pursuit", "open loop", "memory", "source", "evidence", "timeline", "claim", "whatsapp", "channel", "deadline", "calendar", "odoo", "erp", "herp", "provider", "llm") {
		maps = append(maps, prefixOpenClawMaps("completeness", inventory.completenessMaps)...)
		maps = append(maps, prefixOpenClawMaps("maintainer-note", inventory.maintainerNotes)...)
		maps = append(maps, prefixOpenClawMaps("root-doc", matchingOpenClawItems(inventory.rootDocs, "AGENTS", "CLAUDE", "README", "SECURITY"))...)
		maps = append(maps, prefixOpenClawMaps("root-config", matchingOpenClawItems(inventory.rootConfigs, "package", "docker", "pnpm", "wrangler"))...)
		maps = append(maps, prefixOpenClawMaps("config", matchingOpenClawItems(inventory.configProfiles, "gateway", "provider", "memory", "security", "policy", "channel"))...)
		maps = append(maps, prefixOpenClawMaps("codex-prompt", inventory.codexPrompts)...)
	}
	if containsAny(prompt, "deploy", "docker", "container", "hosting", "fly", "render", "kubernetes", "installer", "install") {
		maps = append(maps, prefixOpenClawMaps("deploy", inventory.deployTargets)...)
		maps = append(maps, prefixOpenClawMaps("script", matchingOpenClawItems(inventory.scripts, "install", "release", "build", "docker", "setup"))...)
	}

	blocked := append([]string{}, a.blockedOpenClawSurfaces()...)
	for _, channel := range inventory.channels {
		if strings.Contains(prompt, strings.ToLower(channel)) {
			blocked = append(blocked, channel+" outbound send without separate HAI approval")
		}
	}
	if containsAny(prompt, "send", "reply", "email", "message", "whatsapp", "telegram", "slack", "discord") {
		blocked = append(blocked, "outbound communication without separate HAI approval")
	}
	if containsAny(prompt, "post", "publish", "public", "medium", "social") {
		blocked = append(blocked, "public posting without source-grounded review and separate HAI approval")
	}
	if containsAny(prompt, "delete", "remove", "overwrite", "move file", "archive") {
		blocked = append(blocked, "destructive or irreversible file action without rollback plan and explicit approval")
	}
	if containsAny(prompt, "pay", "payment", "invoice", "bank", "contract", "legal", "lawyer", "government", "insurance") {
		blocked = append(blocked, "financial/legal/government commitment without explicit approval")
	}
	if len(a.highRiskConfiguredSurfaces()) > 0 && !a.highRiskExecution {
		blocked = append(blocked, a.highRiskConfiguredSurfaces()...)
	}

	mode := "read-only planning plus approved low-risk local actions"
	if risk == "high" {
		mode = "draft and analyze only; external effects require a separate HAI approval"
	}

	return openClawTaskProfile{
		Intent:            intent,
		ExecutionMode:     mode,
		RiskLevel:         risk,
		RecommendedSkills: sortedUnique(skills),
		VisibleProviders:  limitStrings(inventory.providers, 12),
		VisibleTools:      limitStrings(inventory.tools, 12),
		RelevantMaps:      limitStrings(maps, 12),
		BlockedSurfaces:   sortedUnique(blocked),
		RequiredControls: []string{
			"respect HAI approval boundaries",
			"do not send messages, publish, approve pairings, run cron, control browser/nodes, or use host tools from this adapter",
			"use source-grounded claims and mark uncertainty",
			"keep paid-provider usage disabled unless HAI policy explicitly approves it",
			"return verification evidence and remaining blockers",
		},
		ValidationChecklist: []string{
			"requested outcome addressed",
			"pursuit/open-loop state and next safe action are explicit when applicable",
			"source, evidence, or missing-evidence status is reported when factual claims are made",
			"risky action not executed without approval",
			"important claims grounded or marked uncertain",
			"next safe action identified",
		},
	}
}

func (a *openClawAdapter) capabilities() []string {
	return []string{
		"noninteractive OpenClaw agent CLI execution",
		"local-first Gateway control plane over WebSocket",
		"operator, node, and control UI protocol roles",
		"multi-channel inbox and outbound routing: WhatsApp, Telegram, Slack, Discord, Google Chat, Signal, iMessage, IRC, Microsoft Teams, Matrix, Feishu, LINE, Mattermost, Nextcloud Talk, Nostr, Synology Chat, Tlon, Twitch, Zalo, WeChat, QQ, and WebChat",
		"multi-agent session routing with isolated agents, workspaces, and sessions",
		"skills, ClawHub packages, plugin SDK, and app SDK surfaces",
		"local/free/cloud model provider routing including Ollama, LM Studio, llama.cpp, LocalAI, vLLM, OpenAI-compatible endpoints, OpenRouter, OpenAI, Anthropic, Gemini, and Codex provider paths",
		"tools for browser, canvas, nodes, cron, sessions, Discord and Slack actions",
		"Live Canvas and A2UI surfaces",
		"voice wake and talk mode surfaces",
		"Windows Hub, macOS menu bar, iOS, Android, Linux, and small-device companion surfaces: " + strings.Join(a.companionApps, ", "),
		"sandbox backends: Docker, SSH, and OpenShell",
		"Gateway health, status, diagnostics, pairing, and device identity operations",
		"HAI task envelope routing that maps tasks to indexed OpenClaw skills while preserving approval and audit controls",
	}
}

func (a *openClawAdapter) architecture() []string {
	return []string{
		"HAI workflow intake, policy, approval queue, and audit log",
		"HAI agent-runtime registry",
		"OpenClaw CLI agent command",
		"OpenClaw Gateway local-first control plane",
		"OpenClaw channel, node, canvas, voice, plugin, skill, and model-provider ecosystems",
		"OpenClaw sandbox backends and tool approval layer",
		"HAI source-grounded verification and workflow completion state machine",
	}
}

func (a *openClawAdapter) controls() []string {
	controls := []string{
		"disabled by default through OPENCLAW_AGENT_ENABLED",
		"server-side HAI approval required before every task",
		"dedicated workspace must remain under AGENT_RUNTIME_WORKSPACE_ROOT",
		"invoked without shell interpolation through openclaw agent --message",
		"bounded timeout and output capture with secret redaction",
		"environment inheritance limited to OPENCLAW_ENV_ALLOWLIST plus explicit HAI/OpenClaw metadata",
		"Gateway URL host constrained by AGENT_RUNTIME_ALLOWED_HOSTS when configured",
		"HAI adapter does not call openclaw message send, pairing approve, node commands, browser actions, cron writes, or public posting",
	}
	if a.sandboxRequired {
		controls = append(controls, "OpenClaw sandbox expected through OPENCLAW_SANDBOX_REQUIRED=true and OPENCLAW_SANDBOX_MODE="+a.sandboxMode)
	} else {
		controls = append(controls, "OpenClaw sandbox requirement disabled; use only with a disposable local workspace")
	}
	if a.gatewayEnabled {
		controls = append(controls, "OpenClaw Gateway access requires OPENCLAW_GATEWAY_TOKEN and keeps Gateway scopes/pairing authoritative")
	}
	if a.messagesEnabled || len(a.channelsEnabled) > 0 {
		controls = append(controls, "messaging surfaces are visible but outbound sends require separate HAI approval workflows")
	}
	if a.hostToolsEnabled || a.execApprovals {
		controls = append(controls, "host tools and exec approvals are visible but not invoked by the OpenClaw adapter execution path")
	}
	if a.publicPosting {
		controls = append(controls, "public posting is marked configured but remains blocked by HAI high-risk action policy")
	}
	if len(a.highRiskConfiguredSurfaces()) > 0 {
		if a.highRiskExecution {
			controls = append(controls, "OPENCLAW_ALLOW_HIGH_RISK_EXECUTION=true acknowledges configured high-risk OpenClaw surfaces; per-task HAI approval and downstream OpenClaw policy still apply")
		} else {
			controls = append(controls, "configured high-risk OpenClaw surfaces block HAI runtime execution until disabled or explicitly acknowledged with OPENCLAW_ALLOW_HIGH_RISK_EXECUTION=true")
		}
	}
	return controls
}

func (a *openClawAdapter) ecosystemReadiness() []string {
	return []string{
		"agent-cli=" + boolLabel(a.agentCLIEnabled),
		"gateway=" + boolLabel(a.gatewayEnabled),
		"messages=" + boolLabel(a.messagesEnabled),
		"channels=" + countLabel(len(a.channelsEnabled)),
		"skills=" + boolLabel(a.skillsEnabled),
		"plugins=" + boolLabel(a.pluginsEnabled),
		"mcp=" + boolLabel(a.mcpEnabled),
		"memory=" + boolLabel(a.memoryEnabled),
		"cron=" + boolLabel(a.cronEnabled),
		"browser=" + boolLabel(a.browserEnabled),
		"canvas=" + boolLabel(a.canvasEnabled),
		"nodes=" + boolLabel(a.nodesEnabled),
		"voice=" + boolLabel(a.voiceEnabled || a.talkEnabled),
		"webchat=" + boolLabel(a.webchatEnabled),
		"multi-agent=" + boolLabel(a.multiAgentEnabled),
		"local-models=" + boolLabel(a.localModelsEnabled),
		"providers=" + countLabel(len(a.providersEnabled)),
		"sandbox=" + a.sandboxMode,
		"docker-sandbox=" + boolLabel(a.sandboxDocker),
		"ssh-sandbox=" + boolLabel(a.sandboxSSH),
		"openshell-sandbox=" + boolLabel(a.sandboxOpenShell),
	}
}

func (a *openClawAdapter) ecosystem() []RuntimeEcosystemSurface {
	inventory := a.ecosystemInventory()
	surfaces := []RuntimeEcosystemSurface{
		openClawInventorySurface(inventory),
		ecosystemSurfaceWithRisk("Package metadata", inventory.status, inventory.metadata, "Version, license, runtime, and package-manager metadata read from OpenClaw package.json when available.", "low", false),
		ecosystemSurfaceWithRisk("Configured HAI surfaces", "policy", a.configuredEcosystemSurfaces(), "These OpenClaw surfaces are visible or enabled through HAI configuration; execution still requires HAI approval.", a.configuredSurfaceRiskLevel(), len(a.highRiskConfiguredSurfaces()) > 0),
		ecosystemSurfaceWithRisk("HAI-blocked high-risk surfaces", "blocked", a.blockedOpenClawSurfaces(), "These OpenClaw surfaces are intentionally blocked by default and need separate HAI policy, approval, and verification work before use.", "high", true),
		ecosystemSurfaceWithRisk("Operator setup checklist", "operator_action", a.openClawSetupChecklist(inventory), "Minimum setup required before OpenClaw should be trusted as a runtime substrate for HAI.", "medium", true),
		ecosystemSurfaceWithRisk("Skills", inventory.status, inventory.skills, "Visible to HAI planning; execution still goes through the approved OpenClaw agent CLI task path.", "medium", true),
		ecosystemSurfaceWithRisk("Skill scripts", inventory.status, inventory.skillScripts, "Execution-capable OpenClaw skill scripts are cataloged for operator review only; HAI does not invoke them directly.", "high", true),
		ecosystemSurfaceWithRisk("Agent profiles", inventory.status, inventory.agentProfiles, "OpenClaw skill-level agent profiles are cataloged for planning and delegation mapping only.", "low", false),
		ecosystemSurfaceWithRisk("Skill reference maps", inventory.status, inventory.skillReferences, "Reference documents are indexed as operator/planning context; HAI does not execute referenced procedures directly.", "low", false),
		ecosystemSurfaceWithRisk("Completeness maps", inventory.status, inventory.completenessMaps, "Completeness scorecards are visible for architecture-gap planning and must still be verified against HAI implementation.", "low", false),
		ecosystemSurfaceWithRisk("Maintainer notes", inventory.status, inventory.maintainerNotes, "Maintainer notes are visible for operator review and future adapter planning.", "low", false),
		ecosystemSurfaceWithRisk("Documentation corpus", inventory.status, inventory.docs, "OpenClaw documentation is indexed as setup, architecture, and operator-planning context; HAI does not import it as executable behavior.", "low", false),
		ecosystemSurfaceWithRisk("Root scripts", inventory.status, inventory.scripts, "OpenClaw repository scripts are cataloged for compatibility planning only; HAI does not invoke them directly.", "high", true),
		ecosystemSurfaceWithRisk("QA assets", inventory.status, inventory.qaAssets, "OpenClaw QA assets are visible for test-planning and proof mapping; HAI does not dispatch upstream QA automation from this adapter.", "medium", true),
		ecosystemSurfaceWithRisk("Test suites", inventory.status, inventory.testSuites, "OpenClaw test suites are cataloged for architecture comparison and future adapter validation planning only.", "medium", true),
		ecosystemSurfaceWithRisk("Configuration profiles", inventory.status, inventory.configProfiles, "OpenClaw configuration profiles are cataloged for operator review; HAI keeps its own configuration and secret handling.", "medium", true),
		ecosystemSurfaceWithRisk("Security assets", inventory.status, inventory.securityAssets, "OpenClaw security policy, scan, and guard assets are available as review context; HAI still enforces its own approval and verification gates.", "medium", true),
		ecosystemSurfaceWithRisk("Deployment targets", inventory.status, inventory.deployTargets, "OpenClaw deployment descriptors are visible for compatibility planning only; HAI does not deploy OpenClaw from this adapter.", "medium", true),
		ecosystemSurfaceWithRisk("Codex prompt maps", inventory.status, inventory.codexPrompts, "OpenClaw repository prompts are indexed as planning/reference material only; HAI does not import them as instructions automatically.", "low", false),
		ecosystemSurfaceWithRisk("GitHub workflows", inventory.status, inventory.githubWorkflows, "OpenClaw CI/release automations are visible for compatibility planning only; HAI never dispatches upstream workflows from this adapter.", "medium", true),
		ecosystemSurfaceWithRisk("GitHub Actions", inventory.status, inventory.githubActions, "Reusable OpenClaw GitHub Actions are indexed for CI/release compatibility planning only; HAI does not dispatch or mutate upstream workflow runs from this adapter.", "medium", true),
		ecosystemSurfaceWithRisk("GitHub issue templates", inventory.status, inventory.githubIssues, "Issue templates are visible for triage/delegation mapping and do not grant GitHub write access.", "low", false),
		ecosystemSurfaceWithRisk("Security and CodeQL maps", inventory.status, inventory.githubSecurityMaps, "OpenClaw security guard, CodeQL, and trust-boundary maps are available as planning context for security reviews; execution remains inside HAI verification and approval gates.", "medium", true),
		ecosystemSurfaceWithRisk("Repository instructions", inventory.status, inventory.githubInstructions, "Repository-level agent instructions are cataloged as reference only and are never imported as direct HAI system instructions.", "low", false),
		ecosystemSurfaceWithRisk("Repository docs", inventory.status, inventory.rootDocs, "Top-level OpenClaw operator and agent documents are cataloged for setup/review context.", "low", false),
		ecosystemSurfaceWithRisk("Repository config", inventory.status, inventory.rootConfigs, "Top-level OpenClaw config files are cataloged for compatibility checks and operator review.", "medium", true),
		ecosystemSurfaceWithRisk("Provider extensions", inventory.status, inventory.providers, "Provider credentials are not inherited unless explicitly allowlisted; HAI's $0 LLM policy still controls paid usage.", "medium", true),
		ecosystemSurfaceWithRisk("Channel extensions", inventory.status, inventory.channels, "Messaging surfaces are cataloged only; outbound sends remain high-risk approval-gated HAI actions.", "high", true),
		ecosystemSurfaceWithRisk("Tool/runtime extensions", inventory.status, inventory.tools, "Browser, node, cron, shell, and host-tool surfaces are visible but not invoked by the HAI OpenClaw adapter.", "high", true),
		ecosystemSurfaceWithRisk("Companion apps", inventory.status, inventory.apps, "Companion apps remain external OpenClaw surfaces; HAI only records readiness and routes approved work.", "medium", true),
		ecosystemSurfaceWithRisk("Core packages", inventory.status, inventory.packages, "SDK/runtime packages are used for compatibility planning, not vendored into HAI.", "low", false),
		ecosystemSurfaceWithRisk("Source modules", inventory.status, inventory.sourceModules, "OpenClaw source domains are cataloged for architecture mapping only; HAI does not import these modules.", "low", false),
		ecosystemSurfaceWithRisk("Control UI views", inventory.status, inventory.uiViews, "OpenClaw control-plane screens are cataloged for operator mapping; HAI keeps its own dashboard as the canonical UI.", "low", false),
		ecosystemSurfaceWithRisk("Control UI controllers", inventory.status, inventory.uiControllers, "OpenClaw controller surfaces are cataloged for integration planning; HAI does not bypass its approval APIs.", "medium", true),
	}
	if len(inventory.extensions) > 0 {
		surfaces = append(surfaces, ecosystemSurfaceWithRisk("All extensions", inventory.status, inventory.extensions, "Full extension inventory for operator review and future adapter planning.", "review", true))
	}
	if len(inventory.warnings) > 0 {
		surfaces = append(surfaces, ecosystemSurfaceWithRisk("Inventory warnings", "review", inventory.warnings, "Review OpenClaw package path and extraction state before enabling runtime execution.", "medium", true))
	}
	return surfaces
}

func (a *openClawAdapter) ecosystemInventory() openClawEcosystemInventory {
	path := strings.TrimSpace(a.ecosystemPath)
	signature := openClawEcosystemSignature(path)
	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()
	if a.inventoryLoaded && a.inventoryPath == path && a.inventorySignature == signature {
		return cloneOpenClawInventory(a.inventory)
	}
	inventory := scanOpenClawEcosystem(path)
	a.inventoryLoaded = true
	a.inventoryPath = path
	a.inventorySignature = signature
	a.inventory = cloneOpenClawInventory(inventory)
	return inventory
}

func (a *openClawAdapter) setEcosystemPath(path string) error {
	return a.setEcosystemPathWithTrust(path, false)
}

func (a *openClawAdapter) setUploadedEcosystemPath(path string) error {
	if !isOpenClawUploadArtifactPath(path) {
		return fmt.Errorf("openclaw uploaded ecosystem path is not a HAI-managed temporary artifact")
	}
	return a.setEcosystemPathWithTrust(path, true)
}

type preparedOpenClawEcosystemPath struct {
	targetPath        string
	targetSignature   string
	previousPath      string
	previousSignature string
	deleteManagedPath string
}

func (a *openClawAdapter) setEcosystemPathWithTrust(path string, trustedUpload bool) error {
	path = strings.TrimSpace(path)
	prepared, err := a.prepareEcosystemPath(path, trustedUpload)
	if err != nil {
		return err
	}
	return a.applyPreparedEcosystemPath(prepared)
}

func (a *openClawAdapter) prepareEcosystemPath(
	path string,
	trustedUpload bool,
) (preparedOpenClawEcosystemPath, error) {
	path = strings.TrimSpace(path)
	if trustedUpload && !isOpenClawUploadArtifactPath(path) {
		return preparedOpenClawEcosystemPath{},
			fmt.Errorf("openclaw uploaded ecosystem path is not a HAI-managed temporary artifact")
	}
	if err := validateOpenClawEcosystemPath(path); err != nil {
		return preparedOpenClawEcosystemPath{}, err
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return preparedOpenClawEcosystemPath{},
			fmt.Errorf("openclaw ecosystem path is invalid")
	}

	a.inventoryMu.Lock()
	roots := append([]string{}, a.ecosystemRoots...)
	if len(roots) == 0 {
		roots = a.initialEcosystemRoots()
	}
	previousPath := strings.TrimSpace(a.ecosystemPath)
	a.inventoryMu.Unlock()

	if trustedUpload {
		absolutePath, err = filepath.EvalSymlinks(absolutePath)
		if err != nil {
			return preparedOpenClawEcosystemPath{},
				fmt.Errorf("openclaw uploaded ecosystem path cannot be resolved")
		}
	} else {
		resolvedPath, allowed := resolvePathWithinAnyRoot(roots, absolutePath)
		if !allowed {
			return preparedOpenClawEcosystemPath{},
				fmt.Errorf("openclaw ecosystem path is outside OPENCLAW_ECOSYSTEM_ALLOWED_ROOTS")
		}
		absolutePath = resolvedPath
	}

	deleteManagedPath := ""
	if isOpenClawUploadArtifactPath(previousPath) && !sameFilePath(previousPath, absolutePath) {
		deleteManagedPath = previousPath
	}
	return preparedOpenClawEcosystemPath{
		targetPath:        absolutePath,
		targetSignature:   openClawEcosystemSignature(absolutePath),
		previousPath:      previousPath,
		previousSignature: openClawEcosystemSignature(previousPath),
		deleteManagedPath: deleteManagedPath,
	}, nil
}

func (a *openClawAdapter) applyPreparedEcosystemPath(
	prepared preparedOpenClawEcosystemPath,
) error {
	if err := validateOpenClawEcosystemPath(prepared.targetPath); err != nil {
		return err
	}
	if openClawEcosystemSignature(prepared.targetPath) != prepared.targetSignature {
		return ErrEcosystemMutationConflict
	}

	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()
	currentPath := strings.TrimSpace(a.ecosystemPath)
	if currentPath != prepared.previousPath ||
		openClawEcosystemSignature(currentPath) != prepared.previousSignature {
		return ErrEcosystemMutationConflict
	}
	if prepared.deleteManagedPath != "" {
		if prepared.deleteManagedPath != currentPath ||
			!isOpenClawUploadArtifactPath(prepared.deleteManagedPath) {
			return ErrEcosystemMutationConflict
		}
		if err := os.Remove(prepared.deleteManagedPath); err != nil {
			return fmt.Errorf("remove previous managed OpenClaw ecosystem archive: %w", err)
		}
	}
	a.ecosystemPath = prepared.targetPath
	a.inventoryLoaded = false
	a.inventoryPath = ""
	a.inventorySignature = ""
	a.inventory = openClawEcosystemInventory{}
	return nil
}

func (a *openClawAdapter) initialEcosystemRoots() []string {
	candidates := append([]string{}, a.ecosystemRoots...)
	candidates = append(candidates, a.workspaceRoot, a.workspace)
	if current := strings.TrimSpace(a.ecosystemPath); current != "" && !isOpenClawUploadArtifactPath(current) {
		if stat, err := os.Stat(current); err == nil && stat.IsDir() {
			candidates = append(candidates, current)
		} else {
			candidates = append(candidates, filepath.Dir(current))
		}
	}
	roots := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(filepath.Clean(candidate))
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		key := strings.ToLower(resolved)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, resolved)
	}
	return roots
}

func resolvePathWithinAnyRoot(roots []string, target string) (string, bool) {
	for _, root := range roots {
		if resolved, err := pathsafety.ResolveWithinBase(root, target); err == nil {
			return resolved, true
		}
	}
	return "", false
}

func validateOpenClawEcosystemPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("openclaw ecosystem path is required")
	}
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("openclaw ecosystem path does not exist")
	}
	if !stat.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".zip") {
			return fmt.Errorf("openclaw ecosystem file must be a .zip archive")
		}
		if err := validateOpenClawZip(path); err != nil {
			return err
		}
		return nil
	}
	root := normalizeOpenClawRoot(path)
	if root == "" {
		return fmt.Errorf("openclaw ecosystem directory is not accessible")
	}
	packagePath := filepath.Join(root, "package.json")
	hasPackage := false
	if _, err := os.Stat(packagePath); err == nil {
		hasPackage = true
	}
	if (!hasPackage || !packageFileLooksLikeOpenClaw(packagePath)) && !hasOpenClawMarkers(root) {
		return fmt.Errorf("openclaw ecosystem directory does not look like an OpenClaw checkout")
	}
	return nil
}

func packageFileLooksLikeOpenClaw(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var metadata struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 128*1024)).Decode(&metadata); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(metadata.Name)), "openclaw")
}

func sameFilePath(left, right string) bool {
	left = strings.TrimSpace(strings.ToLower(filepath.Clean(left)))
	right = strings.TrimSpace(strings.ToLower(filepath.Clean(right)))
	return left == right
}

func isOpenClawUploadArtifactPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return false
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "openclaw-ecosystem-") {
		return false
	}

	tempDir := filepath.Clean(os.TempDir())
	absPath := filepath.Clean(path)
	relative, err := filepath.Rel(tempDir, absPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return false
	}
	return true
}

func (a *openClawAdapter) refreshEcosystemInventory() {
	a.inventoryMu.Lock()
	a.inventoryLoaded = false
	a.inventoryPath = ""
	a.inventorySignature = ""
	a.inventory = openClawEcosystemInventory{}
	a.inventoryMu.Unlock()
}

func (a *openClawAdapter) ecosystemState() (string, string) {
	a.inventoryMu.Lock()
	path := strings.TrimSpace(a.ecosystemPath)
	a.inventoryMu.Unlock()
	return path, openClawEcosystemSignature(path)
}

func (a *openClawAdapter) refreshEcosystemInventoryIfCurrent(
	expectedPath string,
	expectedSignature string,
) error {
	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()
	currentPath := strings.TrimSpace(a.ecosystemPath)
	if currentPath != expectedPath ||
		openClawEcosystemSignature(currentPath) != expectedSignature {
		return ErrEcosystemMutationConflict
	}
	a.inventoryLoaded = false
	a.inventoryPath = ""
	a.inventorySignature = ""
	a.inventory = openClawEcosystemInventory{}
	return nil
}

func openClawEcosystemSignature(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "unavailable:" + filepath.Clean(path)
	}
	return fmt.Sprintf("%s:%d:%d", filepath.Clean(path), stat.ModTime().UnixNano(), stat.Size())
}

func cloneOpenClawInventory(inventory openClawEcosystemInventory) openClawEcosystemInventory {
	return openClawEcosystemInventory{
		status:             inventory.status,
		metadata:           append([]string{}, inventory.metadata...),
		skills:             append([]string{}, inventory.skills...),
		skillScripts:       append([]string{}, inventory.skillScripts...),
		extensions:         append([]string{}, inventory.extensions...),
		providers:          append([]string{}, inventory.providers...),
		channels:           append([]string{}, inventory.channels...),
		tools:              append([]string{}, inventory.tools...),
		agentProfiles:      append([]string{}, inventory.agentProfiles...),
		skillReferences:    append([]string{}, inventory.skillReferences...),
		completenessMaps:   append([]string{}, inventory.completenessMaps...),
		maintainerNotes:    append([]string{}, inventory.maintainerNotes...),
		docs:               append([]string{}, inventory.docs...),
		scripts:            append([]string{}, inventory.scripts...),
		qaAssets:           append([]string{}, inventory.qaAssets...),
		testSuites:         append([]string{}, inventory.testSuites...),
		configProfiles:     append([]string{}, inventory.configProfiles...),
		securityAssets:     append([]string{}, inventory.securityAssets...),
		deployTargets:      append([]string{}, inventory.deployTargets...),
		codexPrompts:       append([]string{}, inventory.codexPrompts...),
		githubWorkflows:    append([]string{}, inventory.githubWorkflows...),
		githubActions:      append([]string{}, inventory.githubActions...),
		githubIssues:       append([]string{}, inventory.githubIssues...),
		githubSecurityMaps: append([]string{}, inventory.githubSecurityMaps...),
		githubInstructions: append([]string{}, inventory.githubInstructions...),
		rootDocs:           append([]string{}, inventory.rootDocs...),
		rootConfigs:        append([]string{}, inventory.rootConfigs...),
		apps:               append([]string{}, inventory.apps...),
		packages:           append([]string{}, inventory.packages...),
		sourceModules:      append([]string{}, inventory.sourceModules...),
		uiViews:            append([]string{}, inventory.uiViews...),
		uiControllers:      append([]string{}, inventory.uiControllers...),
		warnings:           append([]string{}, inventory.warnings...),
	}
}

func (a *openClawAdapter) configuredEcosystemSurfaces() []string {
	items := []string{}
	if a.agentCLIEnabled {
		items = append(items, "approved noninteractive agent CLI")
	}
	if a.gatewayEnabled {
		items = append(items, "Gateway control plane")
	}
	if a.messagesEnabled {
		items = append(items, "message relay visibility")
	}
	if len(a.channelsEnabled) > 0 {
		items = append(items, "channels: "+strings.Join(a.channelsEnabled, ", "))
	}
	if a.skillsEnabled {
		items = append(items, "skills")
	}
	if a.pluginsEnabled {
		items = append(items, "plugins")
	}
	if a.mcpEnabled {
		items = append(items, "MCP bridge")
	}
	if a.memoryEnabled {
		items = append(items, "OpenClaw memory visibility")
	}
	if a.browserEnabled {
		items = append(items, "browser surface visibility")
	}
	if a.canvasEnabled {
		items = append(items, "Live Canvas visibility")
	}
	if a.nodesEnabled {
		items = append(items, "node/device surface visibility")
	}
	if a.voiceEnabled || a.talkEnabled {
		items = append(items, "voice/talk surfaces")
	}
	if a.webchatEnabled {
		items = append(items, "WebChat")
	}
	if a.multiAgentEnabled {
		items = append(items, "multi-agent routing")
	}
	if a.localModelsEnabled {
		items = append(items, "local model providers")
	}
	if len(a.providersEnabled) > 0 {
		items = append(items, "providers: "+strings.Join(a.providersEnabled, ", "))
	}
	if a.sandboxRequired {
		items = append(items, "sandbox required: "+firstNonEmpty(a.sandboxMode, "all"))
	}
	if a.sandboxDocker {
		items = append(items, "Docker sandbox backend")
	}
	if a.sandboxSSH {
		items = append(items, "SSH sandbox backend")
	}
	if a.sandboxOpenShell {
		items = append(items, "OpenShell sandbox backend")
	}
	if len(a.companionApps) > 0 {
		items = append(items, "companion apps: "+strings.Join(a.companionApps, ", "))
	}
	if a.highRiskExecution {
		items = append(items, "high-risk execution acknowledged")
	}
	return items
}

func (a *openClawAdapter) highRiskConfiguredSurfaces() []string {
	items := []string{}
	if a.messagesEnabled || len(a.channelsEnabled) > 0 {
		items = append(items, "messaging/channel surfaces")
	}
	if a.pairingEnabled {
		items = append(items, "pairing approval")
	}
	if a.execApprovals {
		items = append(items, "OpenClaw exec approvals")
	}
	if a.hostToolsEnabled {
		items = append(items, "host tools")
	}
	if a.publicPosting {
		items = append(items, "public posting")
	}
	if a.webSearchEnabled {
		items = append(items, "web search")
	}
	if a.cronEnabled {
		items = append(items, "cron jobs")
	}
	if a.browserEnabled {
		items = append(items, "browser control")
	}
	if a.nodesEnabled {
		items = append(items, "node/device control")
	}
	if a.canvasEnabled {
		items = append(items, "Live Canvas writes")
	}
	if a.voiceEnabled || a.talkEnabled || a.webchatEnabled {
		items = append(items, "voice/talk/webchat surfaces")
	}
	if a.sandboxSSH {
		items = append(items, "SSH sandbox backend")
	}
	if a.sandboxOpenShell {
		items = append(items, "OpenShell sandbox backend")
	}
	if !a.sandboxRequired {
		items = append(items, "sandbox requirement disabled")
	}
	return sortedUnique(items)
}

func (a *openClawAdapter) highRiskExecutionBlockers() []string {
	if a.highRiskExecution {
		return nil
	}
	surfaces := a.highRiskConfiguredSurfaces()
	if len(surfaces) == 0 {
		return nil
	}
	return []string{"configured OpenClaw high-risk surfaces block generic runtime execution until disabled or explicitly acknowledged: " + strings.Join(surfaces, ", ")}
}

func (a *openClawAdapter) configuredSurfaceRiskLevel() string {
	if len(a.highRiskConfiguredSurfaces()) > 0 {
		return "high"
	}
	if a.skillsEnabled || a.pluginsEnabled || a.mcpEnabled || a.memoryEnabled || a.multiAgentEnabled || len(a.providersEnabled) > 0 || a.sandboxDocker {
		return "medium"
	}
	return "low"
}

func (a *openClawAdapter) blockedOpenClawSurfaces() []string {
	items := []string{}
	if !a.messagesEnabled {
		items = append(items, "outbound message sending")
	}
	if !a.pairingEnabled {
		items = append(items, "pairing approval")
	}
	if !a.execApprovals {
		items = append(items, "OpenClaw exec approvals")
	}
	if !a.hostToolsEnabled {
		items = append(items, "host tools")
	}
	if !a.publicPosting {
		items = append(items, "public posting")
	}
	if !a.webSearchEnabled {
		items = append(items, "web search")
	}
	if !a.cronEnabled {
		items = append(items, "cron jobs")
	}
	if !a.browserEnabled {
		items = append(items, "browser control")
	}
	if !a.nodesEnabled {
		items = append(items, "node/device control")
	}
	if !a.canvasEnabled {
		items = append(items, "Live Canvas writes")
	}
	return items
}

func (a *openClawAdapter) openClawSetupChecklist(inventory openClawEcosystemInventory) []string {
	items := []string{
		"install OpenClaw separately with Node 24 or Node 22.19+",
		"run openclaw onboard and openclaw gateway status outside HAI",
		"set OPENCLAW_WORKSPACE to a dedicated folder under AGENT_RUNTIME_WORKSPACE_ROOT",
		"create a HAI automation with launchType=agent_runtime and runtimeType=openclaw",
		"keep high-risk channel, host, browser, cron, node, and posting surfaces disabled until each has a HAI approval workflow",
	}
	if inventory.status != "available" {
		items = append(items, "set OPENCLAW_ECOSYSTEM_PATH to openclaw-main.zip or an extracted OpenClaw checkout for read-only inventory")
	}
	if a.gatewayEnabled {
		items = append(items, "set scoped OPENCLAW_GATEWAY_TOKEN and constrain OPENCLAW_GATEWAY_URL with AGENT_RUNTIME_ALLOWED_HOSTS")
	}
	if !a.enabled {
		items = append(items, "set OPENCLAW_AGENT_ENABLED=true only after workspace, approval, audit, and verification policies are ready")
	}
	return items
}

const maxRuntimeEcosystemItems = 24

type openClawEcosystemInventory struct {
	status             string
	metadata           []string
	skills             []string
	skillScripts       []string
	extensions         []string
	providers          []string
	channels           []string
	tools              []string
	agentProfiles      []string
	skillReferences    []string
	completenessMaps   []string
	maintainerNotes    []string
	docs               []string
	scripts            []string
	qaAssets           []string
	testSuites         []string
	configProfiles     []string
	securityAssets     []string
	deployTargets      []string
	codexPrompts       []string
	githubWorkflows    []string
	githubActions      []string
	githubIssues       []string
	githubSecurityMaps []string
	githubInstructions []string
	rootDocs           []string
	rootConfigs        []string
	apps               []string
	packages           []string
	sourceModules      []string
	uiViews            []string
	uiControllers      []string
	warnings           []string
}

func openClawInventorySurface(inventory openClawEcosystemInventory) RuntimeEcosystemSurface {
	skillCount := len(sortedUnique(inventory.skills))
	skillScriptCount := len(sortedUnique(inventory.skillScripts))
	extensionCount := len(sortedUnique(inventory.extensions))
	providerCount := len(sortedUnique(inventory.providers))
	channelCount := len(sortedUnique(inventory.channels))
	toolCount := len(sortedUnique(inventory.tools))
	agentProfileCount := len(sortedUnique(inventory.agentProfiles))
	skillReferenceCount := len(sortedUnique(inventory.skillReferences))
	completenessMapCount := len(sortedUnique(inventory.completenessMaps))
	maintainerNoteCount := len(sortedUnique(inventory.maintainerNotes))
	docCount := len(sortedUnique(inventory.docs))
	scriptCount := len(sortedUnique(inventory.scripts))
	qaAssetCount := len(sortedUnique(inventory.qaAssets))
	testSuiteCount := len(sortedUnique(inventory.testSuites))
	configProfileCount := len(sortedUnique(inventory.configProfiles))
	securityAssetCount := len(sortedUnique(inventory.securityAssets))
	deployTargetCount := len(sortedUnique(inventory.deployTargets))
	codexPromptCount := len(sortedUnique(inventory.codexPrompts))
	githubWorkflowCount := len(sortedUnique(inventory.githubWorkflows))
	githubActionCount := len(sortedUnique(inventory.githubActions))
	githubIssueCount := len(sortedUnique(inventory.githubIssues))
	githubSecurityMapCount := len(sortedUnique(inventory.githubSecurityMaps))
	githubInstructionCount := len(sortedUnique(inventory.githubInstructions))
	rootDocCount := len(sortedUnique(inventory.rootDocs))
	rootConfigCount := len(sortedUnique(inventory.rootConfigs))
	appCount := len(sortedUnique(inventory.apps))
	packageCount := len(sortedUnique(inventory.packages))
	sourceModuleCount := len(sortedUnique(inventory.sourceModules))
	uiViewCount := len(sortedUnique(inventory.uiViews))
	uiControllerCount := len(sortedUnique(inventory.uiControllers))
	total := skillCount + skillScriptCount + extensionCount + providerCount + channelCount + toolCount + agentProfileCount + skillReferenceCount + completenessMapCount + maintainerNoteCount + docCount + scriptCount + qaAssetCount + testSuiteCount + configProfileCount + securityAssetCount + deployTargetCount + codexPromptCount + githubWorkflowCount + githubActionCount + githubIssueCount + githubSecurityMapCount + githubInstructionCount + rootDocCount + rootConfigCount + appCount + packageCount + sourceModuleCount + uiViewCount + uiControllerCount
	items := []string{}
	if total > 0 {
		items = []string{
			fmt.Sprintf("%d skills", skillCount),
			fmt.Sprintf("%d skill scripts", skillScriptCount),
			fmt.Sprintf("%d extensions", extensionCount),
			fmt.Sprintf("%d providers", providerCount),
			fmt.Sprintf("%d channels", channelCount),
			fmt.Sprintf("%d tool/runtime extensions", toolCount),
			fmt.Sprintf("%d agent profiles", agentProfileCount),
			fmt.Sprintf("%d skill references", skillReferenceCount),
			fmt.Sprintf("%d completeness maps", completenessMapCount),
			fmt.Sprintf("%d maintainer notes", maintainerNoteCount),
			fmt.Sprintf("%d docs", docCount),
			fmt.Sprintf("%d root scripts", scriptCount),
			fmt.Sprintf("%d QA assets", qaAssetCount),
			fmt.Sprintf("%d test suites", testSuiteCount),
			fmt.Sprintf("%d configuration profiles", configProfileCount),
			fmt.Sprintf("%d security assets", securityAssetCount),
			fmt.Sprintf("%d deployment targets", deployTargetCount),
			fmt.Sprintf("%d Codex prompts", codexPromptCount),
			fmt.Sprintf("%d GitHub workflows", githubWorkflowCount),
			fmt.Sprintf("%d GitHub Actions", githubActionCount),
			fmt.Sprintf("%d GitHub issue templates", githubIssueCount),
			fmt.Sprintf("%d security maps", githubSecurityMapCount),
			fmt.Sprintf("%d repository instructions", githubInstructionCount),
			fmt.Sprintf("%d repository docs", rootDocCount),
			fmt.Sprintf("%d repository configs", rootConfigCount),
			fmt.Sprintf("%d apps", appCount),
			fmt.Sprintf("%d packages", packageCount),
			fmt.Sprintf("%d source modules", sourceModuleCount),
			fmt.Sprintf("%d UI views", uiViewCount),
			fmt.Sprintf("%d UI controllers", uiControllerCount),
		}
	}
	return RuntimeEcosystemSurface{
		Category:         "Package inventory",
		Status:           inventory.status,
		Count:            total,
		Items:            items,
		Control:          "Set OPENCLAW_ECOSYSTEM_PATH to an extracted OpenClaw repo or zip to make HAI aware of installed OpenClaw surfaces.",
		RiskLevel:        "low",
		ApprovalRequired: false,
	}
}

func ecosystemSurface(category string, status string, items []string, control string) RuntimeEcosystemSurface {
	return ecosystemSurfaceWithRisk(category, status, items, control, "", false)
}

func ecosystemSurfaceWithRisk(category string, status string, items []string, control string, riskLevel string, approvalRequired bool) RuntimeEcosystemSurface {
	items = sortedUnique(items)
	limited := items
	more := 0
	if len(items) > maxRuntimeEcosystemItems {
		limited = items[:maxRuntimeEcosystemItems]
		more = len(items) - maxRuntimeEcosystemItems
	}
	return RuntimeEcosystemSurface{
		Category:         category,
		Status:           status,
		Count:            len(items),
		Items:            limited,
		More:             more,
		Control:          control,
		RiskLevel:        riskLevel,
		ApprovalRequired: approvalRequired,
	}
}

func scanOpenClawEcosystem(path string) openClawEcosystemInventory {
	path = strings.TrimSpace(path)
	if path == "" {
		return openClawEcosystemInventory{status: "not_configured"}
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return scanOpenClawZip(path)
	}
	return scanOpenClawDirectory(path)
}

func scanOpenClawZip(path string) openClawEcosystemInventory {
	inventory := openClawEcosystemInventory{status: "available"}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return openClawEcosystemInventory{status: "unavailable", warnings: []string{"OpenClaw ecosystem zip is not readable"}}
	}
	defer reader.Close()
	for _, file := range reader.File {
		parts := strings.Split(strings.Trim(file.Name, "/"), "/")
		collectOpenClawPath(parts, &inventory)
		if isOpenClawRootPackage(parts) {
			read, err := file.Open()
			if err != nil {
				inventory.warnings = append(inventory.warnings, "OpenClaw package metadata could not be read")
				continue
			}
			collectOpenClawPackageMetadata(read, &inventory)
			_ = read.Close()
		}
	}
	classifyOpenClawExtensions(&inventory)
	return inventory
}

func scanOpenClawDirectory(path string) openClawEcosystemInventory {
	root := normalizeOpenClawRoot(path)
	if root == "" {
		return openClawEcosystemInventory{status: "unavailable", warnings: []string{"OpenClaw ecosystem directory is not accessible"}}
	}
	inventory := openClawEcosystemInventory{status: "available"}
	extensionRoot := filepath.Join(root, "extensions")
	inventory.skills = append(inventory.skills, listChildDirs(filepath.Join(root, ".agents", "skills"))...)
	inventory.skills = append(inventory.skills, listChildDirs(filepath.Join(root, "skills"))...)
	inventory.skills = append(inventory.skills, listOpenClawExtensionSkills(extensionRoot)...)
	inventory.extensions = listPackageDirs(extensionRoot)
	inventory.apps = listChildDirs(filepath.Join(root, "apps"))
	inventory.packages = listPackageDirs(filepath.Join(root, "packages"))
	inventory.sourceModules = listChildDirs(filepath.Join(root, "src"))
	inventory.uiViews = listTypeScriptModules(filepath.Join(root, "ui", "src", "ui", "views"))
	inventory.uiControllers = listTypeScriptModules(filepath.Join(root, "ui", "src", "ui", "controllers"))
	collectOpenClawPackageMetadataFile(filepath.Join(root, "package.json"), &inventory)
	walkOpenClawInventory(root, &inventory)
	classifyOpenClawExtensions(&inventory)
	return inventory
}

func isOpenClawRootPackage(parts []string) bool {
	if len(parts) == 1 {
		return parts[0] == "package.json"
	}
	return len(parts) == 2 && parts[1] == "package.json"
}

func collectOpenClawPackageMetadataFile(path string, inventory *openClawEcosystemInventory) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	collectOpenClawPackageMetadata(file, inventory)
}

func collectOpenClawPackageMetadata(reader io.Reader, inventory *openClawEcosystemInventory) {
	var metadata struct {
		Name           string            `json:"name"`
		Version        string            `json:"version"`
		License        string            `json:"license"`
		PackageManager string            `json:"packageManager"`
		Engines        map[string]string `json:"engines"`
	}
	if err := json.NewDecoder(io.LimitReader(reader, 128*1024)).Decode(&metadata); err != nil {
		inventory.warnings = append(inventory.warnings, "OpenClaw package metadata JSON could not be parsed")
		return
	}
	if metadata.Name != "" {
		inventory.metadata = append(inventory.metadata, "package="+metadata.Name)
	}
	if metadata.Version != "" {
		inventory.metadata = append(inventory.metadata, "version="+metadata.Version)
	}
	if metadata.License != "" {
		inventory.metadata = append(inventory.metadata, "license="+metadata.License)
	}
	if node := strings.TrimSpace(metadata.Engines["node"]); node != "" {
		inventory.metadata = append(inventory.metadata, "node="+node)
	}
	if metadata.PackageManager != "" {
		inventory.metadata = append(inventory.metadata, "package-manager="+compactPackageManager(metadata.PackageManager))
	}
}

func compactPackageManager(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.Index(value, "+"); index > 0 {
		return value[:index]
	}
	return value
}

func normalizeOpenClawRoot(path string) string {
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return ""
	}
	stat, err := os.Stat(cleaned)
	if err != nil || !stat.IsDir() {
		return ""
	}
	if hasOpenClawMarkers(cleaned) {
		return cleaned
	}
	children, err := os.ReadDir(cleaned)
	if err != nil {
		return ""
	}
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		candidate := filepath.Join(cleaned, child.Name())
		if hasOpenClawMarkers(candidate) {
			return candidate
		}
	}
	return cleaned
}

func hasOpenClawMarkers(path string) bool {
	if stat, err := os.Stat(filepath.Join(path, ".agents", "skills")); err == nil && stat.IsDir() {
		return hasOpenClawSkillFiles(filepath.Join(path, ".agents", "skills"))
	}
	if stat, err := os.Stat(filepath.Join(path, "skills")); err == nil && stat.IsDir() {
		return hasOpenClawSkillFiles(filepath.Join(path, "skills"))
	}
	if stat, err := os.Stat(filepath.Join(path, "extensions")); err == nil && stat.IsDir() {
		return true
	}
	return false
}

func hasOpenClawSkillFiles(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "SKILL.md" {
			found = true
		}
		return nil
	})
	return found
}

func collectOpenClawPath(parts []string, inventory *openClawEcosystemInventory) {
	if len(parts) >= 3 && parts[1] == "src" {
		inventory.sourceModules = append(inventory.sourceModules, parts[2])
	}
	if len(parts) >= 3 && parts[1] == "docs" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.docs = append(inventory.docs, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "scripts" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.scripts = append(inventory.scripts, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "qa" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.qaAssets = append(inventory.qaAssets, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "test" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.testSuites = append(inventory.testSuites, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "config" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.configProfiles = append(inventory.configProfiles, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "security" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.securityAssets = append(inventory.securityAssets, name)
		}
	}
	if len(parts) >= 3 && parts[1] == "deploy" {
		if name := openClawCatalogPathName(parts[2:]); name != "" {
			inventory.deployTargets = append(inventory.deployTargets, name)
		}
	}
	if len(parts) == 2 {
		if name := openClawRootDocName(parts[1]); name != "" {
			inventory.rootDocs = append(inventory.rootDocs, name)
		}
		if name := openClawRootConfigName(parts[1]); name != "" {
			inventory.rootConfigs = append(inventory.rootConfigs, name)
		}
		if name := openClawRootDeployName(parts[1]); name != "" {
			inventory.deployTargets = append(inventory.deployTargets, name)
		}
	}
	if len(parts) >= 4 && parts[1] == ".github" && parts[2] == "workflows" {
		if name := openClawWorkflowName(parts[3]); name != "" {
			inventory.githubWorkflows = append(inventory.githubWorkflows, name)
		}
	}
	if len(parts) >= 5 && parts[1] == ".github" && parts[2] == "actions" && parts[4] == "action.yml" {
		inventory.githubActions = append(inventory.githubActions, parts[3])
	}
	if len(parts) >= 4 && parts[1] == ".github" && parts[2] == "ISSUE_TEMPLATE" {
		if name := openClawWorkflowName(parts[3]); name != "" {
			inventory.githubIssues = append(inventory.githubIssues, name)
		}
	}
	if len(parts) >= 4 && parts[1] == ".github" && parts[2] == "instructions" {
		if name := openClawMarkdownName(parts[3]); name != "" {
			inventory.githubInstructions = append(inventory.githubInstructions, name)
		}
	}
	if len(parts) >= 4 && parts[1] == ".github" && parts[2] == "codeql" {
		if name := openClawSecurityMapName(parts[3:]); name != "" {
			inventory.githubSecurityMaps = append(inventory.githubSecurityMaps, name)
		}
	}
	if len(parts) >= 3 && parts[1] == ".github" {
		switch strings.ToLower(parts[2]) {
		case "package-trusted-sources.json", "zizmor.yml", "zizmor.yaml", "dependabot.yml", "dependabot.yaml", "actionlint.yaml":
			inventory.githubSecurityMaps = append(inventory.githubSecurityMaps, strings.TrimSuffix(parts[2], filepath.Ext(parts[2])))
		}
	}
	if len(parts) >= 5 && parts[1] == ".github" && parts[2] == "codex" && parts[3] == "prompts" {
		if name := openClawMarkdownName(parts[4]); name != "" {
			inventory.codexPrompts = append(inventory.codexPrompts, name)
		}
	}
	if len(parts) >= 6 && parts[1] == "ui" && parts[2] == "src" && parts[3] == "ui" {
		if name := openClawTSModuleName(parts[5]); name != "" {
			switch parts[4] {
			case "views":
				inventory.uiViews = append(inventory.uiViews, name)
			case "controllers":
				inventory.uiControllers = append(inventory.uiControllers, name)
			}
		}
	}
	for index := 0; index < len(parts); index++ {
		part := parts[index]
		if part == "skills" && index+2 < len(parts) && parts[index+2] == "SKILL.md" {
			name := parts[index+1]
			if index >= 2 && parts[index-2] == "extensions" {
				name = parts[index-1] + "/" + name
				inventory.extensions = append(inventory.extensions, parts[index-1])
			}
			inventory.skills = append(inventory.skills, name)
		}
		if part == "extensions" && index+2 < len(parts) && parts[index+2] == "SKILL.md" {
			inventory.extensions = append(inventory.extensions, parts[index+1])
			inventory.skills = append(inventory.skills, parts[index+1]+"/default")
		}
		if part == "extensions" && index+2 < len(parts) && parts[index+2] == "package.json" {
			inventory.extensions = append(inventory.extensions, parts[index+1])
		}
		if part == "packages" && index+2 < len(parts) && parts[index+2] == "package.json" {
			inventory.packages = append(inventory.packages, parts[index+1])
		}
		if part == "apps" && index+1 < len(parts) && parts[index+1] != "" {
			inventory.apps = append(inventory.apps, parts[index+1])
		}
		if part == "agents" && index+1 < len(parts) {
			if name := openClawAgentProfileName(parts, index); name != "" {
				inventory.agentProfiles = append(inventory.agentProfiles, name)
			}
		}
		if part == "scripts" && index+1 < len(parts) && index > 1 {
			if name := openClawScriptName(parts, index); name != "" {
				inventory.skillScripts = append(inventory.skillScripts, name)
			}
		}
		if part == "references" && index+1 < len(parts) {
			if name := openClawReferenceName(parts, index); name != "" {
				inventory.skillReferences = append(inventory.skillReferences, name)
				if strings.Contains("/"+strings.Join(parts[index+1:len(parts)-1], "/")+"/", "/completeness/") {
					inventory.completenessMaps = append(inventory.completenessMaps, name)
				}
			}
		}
		if part == "maintainer-notes" && index+1 < len(parts) {
			if name := openClawMarkdownName(parts[len(parts)-1]); name != "" {
				inventory.maintainerNotes = append(inventory.maintainerNotes, name)
			}
		}
	}
}

func walkOpenClawInventory(root string, inventory *openClawEcosystemInventory) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		parts := append([]string{filepath.Base(root)}, strings.Split(filepath.ToSlash(relative), "/")...)
		collectOpenClawPath(parts, inventory)
		return nil
	})
}

func openClawAgentProfileName(parts []string, index int) string {
	if index+1 >= len(parts) {
		return ""
	}
	filename := parts[len(parts)-1]
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".yaml" && ext != ".yml" && ext != ".md" {
		return ""
	}
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	if name == "" {
		return ""
	}
	return openClawSkillScopedName(parts, index) + "/" + name
}

func openClawReferenceName(parts []string, index int) string {
	if index+1 >= len(parts) {
		return ""
	}
	name := openClawMarkdownName(parts[len(parts)-1])
	if name == "" {
		return ""
	}
	suffixParts := append([]string{}, parts[index+1:len(parts)-1]...)
	suffixParts = append(suffixParts, name)
	return openClawSkillScopedName(parts, index) + "/" + strings.Join(suffixParts, "/")
}

func openClawScriptName(parts []string, index int) string {
	if index+1 >= len(parts) {
		return ""
	}
	filename := strings.TrimSpace(parts[len(parts)-1])
	if filename == "" || strings.HasPrefix(filename, ".") {
		return ""
	}
	return openClawSkillScopedName(parts, index) + "/" + filename
}

func openClawSkillScopedName(parts []string, index int) string {
	if index <= 0 {
		return "global"
	}
	skill := parts[index-1]
	if index >= 4 && parts[index-2] == "skills" && parts[index-4] == "extensions" {
		return parts[index-3] + "/" + skill
	}
	return skill
}

func openClawMarkdownName(filename string) string {
	if !strings.EqualFold(filepath.Ext(filename), ".md") {
		return ""
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func openClawWorkflowName(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".yml" && ext != ".yaml" {
		return ""
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func openClawSecurityMapName(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	filename := parts[len(parts)-1]
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yml", ".yaml", ".ql":
	default:
		return ""
	}
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	if name == "" {
		return ""
	}
	if ext == ".ql" && len(parts) > 1 {
		prefix := append([]string{}, parts[:len(parts)-1]...)
		prefix = append(prefix, name)
		return strings.Join(prefix, "/")
	}
	return name
}

func openClawRootDocName(filename string) string {
	base := strings.TrimSpace(filename)
	switch strings.ToUpper(base) {
	case "README.MD", "AGENTS.MD", "CLAUDE.MD", "CONTRIBUTING.MD", "SECURITY.MD", "CHANGELOG.MD", "LICENSE.MD", "LICENSE":
		return strings.TrimSuffix(base, filepath.Ext(base))
	default:
		return ""
	}
}

func openClawRootConfigName(filename string) string {
	base := strings.TrimSpace(filename)
	lower := strings.ToLower(base)
	if lower == "" || strings.HasPrefix(lower, ".git") {
		return ""
	}
	switch lower {
	case ".crabbox.yaml", ".crabbox.yml", ".dockerignore", ".env.example", "dockerfile", "docker-compose.yml", "docker-compose.yaml", "package.json", "pnpm-workspace.yaml", "turbo.json", "tsconfig.json", "vitest.config.ts", "playwright.config.ts", "eslint.config.js", "biome.json", "wrangler.jsonc":
		return base
	default:
		return ""
	}
}

func openClawRootDeployName(filename string) string {
	base := strings.TrimSpace(filename)
	switch strings.ToLower(base) {
	case "fly.toml", "render.yaml", "render.yml", "appcast.xml":
		return base
	default:
		return ""
	}
}

func openClawCatalogPathName(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	filename := strings.TrimSpace(parts[len(parts)-1])
	if filename == "" || strings.HasPrefix(filename, ".") {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" && len(parts) == 1 {
		return ""
	}
	switch ext {
	case ".md", ".mdx", ".txt", ".json", ".jsonc", ".yaml", ".yml", ".toml", ".xml", ".mjs", ".js", ".ts", ".tsx", ".sh", ".ps1", ".py", ".kt", ".kts", ".swift", ".go", ".rs", ".sql":
	default:
		if ext != "" {
			return ""
		}
	}
	nameParts := append([]string{}, parts...)
	last := strings.TrimSuffix(filename, filepath.Ext(filename))
	if last != "" {
		nameParts[len(nameParts)-1] = last
	}
	return strings.Join(nameParts, "/")
}

func listChildDirs(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	return sortedUnique(result)
}

func listTypeScriptModules(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if name := openClawTSModuleName(entry.Name()); name != "" {
			result = append(result, name)
		}
	}
	return sortedUnique(result)
}

func openClawTSModuleName(filename string) string {
	if !strings.HasSuffix(filename, ".ts") || strings.HasSuffix(filename, ".d.ts") {
		return ""
	}
	name := strings.TrimSuffix(filename, ".ts")
	if strings.Contains(name, ".test") || strings.Contains(name, ".browser") || strings.Contains(name, ".node") || strings.HasSuffix(name, ".types") {
		return ""
	}
	return name
}

func listOpenClawExtensionSkills(extensionRoot string) []string {
	entries, err := os.ReadDir(extensionRoot)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		extensionName := entry.Name()
		extensionPath := filepath.Join(extensionRoot, extensionName)
		if _, err := os.Stat(filepath.Join(extensionPath, "SKILL.md")); err == nil {
			result = append(result, extensionName+"/default")
		}
		for _, skill := range listChildDirs(filepath.Join(extensionPath, "skills")) {
			result = append(result, extensionName+"/"+skill)
		}
	}
	return sortedUnique(result)
}

func listPackageDirs(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, entry.Name(), "package.json")); err == nil {
			result = append(result, entry.Name())
		}
	}
	return sortedUnique(result)
}

func classifyOpenClawExtensions(inventory *openClawEcosystemInventory) {
	inventory.extensions = sortedUnique(inventory.extensions)
	channelNames := openClawChannelExtensions()
	providerNames := openClawProviderExtensions()
	toolNames := openClawToolExtensions()
	for _, extension := range inventory.extensions {
		key := strings.ToLower(extension)
		if channelNames[key] {
			inventory.channels = append(inventory.channels, extension)
		}
		if providerNames[key] {
			inventory.providers = append(inventory.providers, extension)
		}
		if toolNames[key] {
			inventory.tools = append(inventory.tools, extension)
		}
	}
	inventory.skills = sortedUnique(inventory.skills)
	inventory.skillScripts = sortedUnique(inventory.skillScripts)
	inventory.metadata = sortedUnique(inventory.metadata)
	inventory.channels = sortedUnique(inventory.channels)
	inventory.providers = sortedUnique(inventory.providers)
	inventory.tools = sortedUnique(inventory.tools)
	inventory.agentProfiles = sortedUnique(inventory.agentProfiles)
	inventory.skillReferences = sortedUnique(inventory.skillReferences)
	inventory.completenessMaps = sortedUnique(inventory.completenessMaps)
	inventory.maintainerNotes = sortedUnique(inventory.maintainerNotes)
	inventory.docs = sortedUnique(inventory.docs)
	inventory.scripts = sortedUnique(inventory.scripts)
	inventory.qaAssets = sortedUnique(inventory.qaAssets)
	inventory.testSuites = sortedUnique(inventory.testSuites)
	inventory.configProfiles = sortedUnique(inventory.configProfiles)
	inventory.securityAssets = sortedUnique(inventory.securityAssets)
	inventory.deployTargets = sortedUnique(inventory.deployTargets)
	inventory.codexPrompts = sortedUnique(inventory.codexPrompts)
	inventory.githubWorkflows = sortedUnique(inventory.githubWorkflows)
	inventory.githubActions = sortedUnique(inventory.githubActions)
	inventory.githubIssues = sortedUnique(inventory.githubIssues)
	inventory.githubSecurityMaps = sortedUnique(inventory.githubSecurityMaps)
	inventory.githubInstructions = sortedUnique(inventory.githubInstructions)
	inventory.rootDocs = sortedUnique(inventory.rootDocs)
	inventory.rootConfigs = sortedUnique(inventory.rootConfigs)
	inventory.apps = sortedUnique(inventory.apps)
	inventory.packages = sortedUnique(inventory.packages)
	inventory.sourceModules = sortedUnique(inventory.sourceModules)
	inventory.uiViews = sortedUnique(inventory.uiViews)
	inventory.uiControllers = sortedUnique(inventory.uiControllers)
}

func openClawChannelExtensions() map[string]bool {
	return map[string]bool{
		"discord": true, "feishu": true, "googlechat": true, "imessage": true, "irc": true,
		"line": true, "matrix": true, "mattermost": true, "msteams": true, "nextcloud-talk": true,
		"nostr": true, "qqbot": true, "signal": true, "slack": true, "sms": true,
		"synology-chat": true, "telegram": true, "tlon": true, "twitch": true, "voice-call": true,
		"webhooks": true, "whatsapp": true, "zalo": true, "zalouser": true,
	}
}

func openClawProviderExtensions() map[string]bool {
	return map[string]bool{
		"alibaba": true, "amazon-bedrock": true, "amazon-bedrock-mantle": true, "anthropic": true,
		"anthropic-vertex": true, "arcee": true, "byteplus": true, "cerebras": true,
		"chutes": true, "cloudflare-ai-gateway": true, "codex": true, "cohere": true,
		"copilot": true, "copilot-proxy": true, "deepinfra": true, "deepseek": true,
		"fireworks": true, "github-copilot": true, "gmi": true, "google": true, "gradium": true, "groq": true,
		"huggingface": true, "inworld": true, "kilocode": true, "kimi-coding": true, "litellm": true,
		"llama-cpp": true, "lmstudio": true, "microsoft": true, "microsoft-foundry": true,
		"minimax": true, "mistral": true, "moonshot": true, "novita": true, "nvidia": true,
		"ollama": true, "openai": true, "openrouter": true, "perplexity": true, "qianfan": true,
		"qwen": true, "sglang": true, "stepfun": true, "synthetic": true, "tencent": true,
		"together": true, "tokenjuice": true, "venice": true, "vercel-ai-gateway": true,
		"vllm": true, "volcengine": true, "voyage": true, "xai": true, "xiaomi": true,
		"zai": true,
	}
}

func openClawToolExtensions() map[string]bool {
	return map[string]bool{
		"acpx": true, "admin-http-rpc": true, "azure-speech": true, "bonjour": true, "browser": true, "brave": true, "canvas": true,
		"clickclack": true, "codex-supervisor": true, "comfy": true, "diagnostics-otel": true,
		"diagnostics-prometheus": true, "deepgram": true, "diffs": true, "diffs-language-pack": true, "document-extract": true,
		"duckduckgo": true, "elevenlabs": true, "exa": true, "fal": true,
		"file-transfer": true, "firecrawl": true, "google-meet": true,
		"image-generation-core": true, "llm-task": true, "media-understanding-core": true,
		"memory-core": true, "memory-lancedb": true, "memory-wiki": true, "migrate-claude": true,
		"migrate-hermes": true, "lobster": true, "oc-path": true, "open-prose": true, "opencode": true, "opencode-go": true,
		"openshell": true, "parallel": true, "pixverse": true, "policy": true,
		"qa-channel": true, "qa-lab": true, "qa-matrix": true, "raft": true,
		"runway": true, "searxng": true, "senseaudio": true, "tavily": true,
		"tts-local-cli": true, "video-generation-core": true, "vydra": true,
		"web-readability": true, "workboard": true,
	}
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func joinOrNone(values []string) string {
	values = sortedUnique(values)
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func containsAny(text string, needles ...string) bool {
	text = strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func matchingOpenClawItems(items []string, candidates ...string) []string {
	result := []string{}
	for _, item := range items {
		itemKey := strings.ToLower(item)
		for _, candidate := range candidates {
			candidate = strings.ToLower(candidate)
			if itemKey == candidate || strings.Contains(itemKey, candidate) {
				result = append(result, item)
				break
			}
		}
	}
	return sortedUnique(result)
}

func prefixOpenClawMaps(prefix string, items []string) []string {
	result := []string{}
	prefix = strings.TrimSpace(prefix)
	for _, item := range sortedUnique(items) {
		if prefix == "" {
			result = append(result, item)
			continue
		}
		result = append(result, prefix+":"+item)
	}
	return result
}

func limitStrings(values []string, max int) []string {
	values = sortedUnique(values)
	if max <= 0 || len(values) <= max {
		return values
	}
	return values[:max]
}

func (a *openClawAdapter) validGatewayURL() string {
	if strings.TrimSpace(a.gatewayURL) == "" {
		return ""
	}
	parsed, err := url.Parse(a.gatewayURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "OPENCLAW_GATEWAY_URL must be an absolute URL"
	}
	if parsed.User != nil {
		return "OPENCLAW_GATEWAY_URL must not contain credentials"
	}
	switch parsed.Scheme {
	case "ws", "wss", "http", "https":
	default:
		return "OPENCLAW_GATEWAY_URL must use ws, wss, http, or https"
	}
	host := strings.ToLower(parsed.Hostname())
	if !a.allowedHost["*"] && !a.allowedHost[host] {
		return "OpenClaw Gateway host is not in AGENT_RUNTIME_ALLOWED_HOSTS"
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return "OpenClaw Gateway endpoint uses blocked address space"
	}
	return ""
}

func (a *openClawAdapter) workspaceBlockedReason() string {
	if strings.TrimSpace(a.workspace) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return ""
	}
	root, err := filepath.Abs(filepath.Clean(a.workspaceRoot))
	if err != nil {
		return "agent runtime workspace root is invalid"
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "agent runtime workspace root is not accessible"
	}
	workspace, err := filepath.Abs(filepath.Clean(a.workspace))
	if err != nil {
		return "OpenClaw workspace is invalid"
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return "OpenClaw workspace is not accessible"
	}
	relative, err := filepath.Rel(root, workspace)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "OpenClaw workspace must stay inside AGENT_RUNTIME_WORKSPACE_ROOT"
	}
	return ""
}

type odysseusAdapter struct {
	enabled     bool
	baseURL     string
	token       string
	sessionID   string
	workspace   string
	timeout     time.Duration
	outputLimit int64
	allowedHost map[string]bool
	httpClient  *http.Client

	allowBash      bool
	allowWebSearch bool
	allowResearch  bool

	todosEnabled               bool
	emailEnabled               bool
	calendarEnabled            bool
	contactsEnabled            bool
	documentsEnabled           bool
	memorySyncEnabled          bool
	notesEnabled               bool
	tasksEnabled               bool
	researchEnabled            bool
	searchEnabled              bool
	mcpEnabled                 bool
	cookbookEnabled            bool
	localModelDiscoveryEnabled bool
	shellEnabled               bool
	browserEnabled             bool
	vaultEnabled               bool
	galleryEnabled             bool
	ttsEnabled                 bool
	sttEnabled                 bool
	companionEnabled           bool
	webhooksEnabled            bool
	codexBridgeEnabled         bool
	claudeBridgeEnabled        bool
	agentMigrationEnabled      bool
	contextBudgetEnabled       bool
}

func newOdysseusAdapterFromEnv() *odysseusAdapter {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("ODYSSEUS_BASE_URL")), "/")
	timeout := time.Duration(boundedIntEnv("ODYSSEUS_AGENT_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second
	return &odysseusAdapter{
		enabled:     envEnabled("ODYSSEUS_AGENT_ENABLED"),
		baseURL:     baseURL,
		token:       strings.TrimSpace(os.Getenv("ODYSSEUS_API_TOKEN")),
		sessionID:   strings.TrimSpace(os.Getenv("ODYSSEUS_AGENT_SESSION_ID")),
		workspace:   strings.TrimSpace(os.Getenv("ODYSSEUS_AGENT_WORKSPACE")),
		timeout:     timeout,
		outputLimit: int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		allowedHost: csvMap(firstNonEmpty(os.Getenv("AGENT_RUNTIME_ALLOWED_HOSTS"), "localhost,127.0.0.1,::1,host.docker.internal,odysseus")),
		httpClient:  noRedirectClient(timeout),

		allowBash:      envEnabled("ODYSSEUS_AGENT_ALLOW_BASH"),
		allowWebSearch: envEnabled("ODYSSEUS_AGENT_ALLOW_WEB_SEARCH"),
		allowResearch:  envEnabled("ODYSSEUS_AGENT_ALLOW_RESEARCH"),

		todosEnabled:               envEnabled("ODYSSEUS_TODOS_ENABLED"),
		emailEnabled:               envEnabled("ODYSSEUS_EMAIL_ENABLED"),
		calendarEnabled:            envEnabled("ODYSSEUS_CALENDAR_ENABLED"),
		contactsEnabled:            envEnabled("ODYSSEUS_CONTACTS_ENABLED"),
		documentsEnabled:           envEnabled("ODYSSEUS_DOCUMENTS_ENABLED"),
		memorySyncEnabled:          envEnabled("ODYSSEUS_MEMORY_SYNC_ENABLED"),
		notesEnabled:               envEnabled("ODYSSEUS_NOTES_ENABLED"),
		tasksEnabled:               envEnabled("ODYSSEUS_TASKS_ENABLED"),
		researchEnabled:            envEnabled("ODYSSEUS_RESEARCH_ENABLED"),
		searchEnabled:              envEnabled("ODYSSEUS_SEARCH_ENABLED"),
		mcpEnabled:                 envEnabled("ODYSSEUS_MCP_ENABLED"),
		cookbookEnabled:            envEnabled("ODYSSEUS_COOKBOOK_ENABLED"),
		localModelDiscoveryEnabled: envEnabled("ODYSSEUS_LOCAL_MODEL_DISCOVERY_ENABLED"),
		shellEnabled:               envEnabled("ODYSSEUS_SHELL_ENABLED"),
		browserEnabled:             envEnabled("ODYSSEUS_BROWSER_ENABLED"),
		vaultEnabled:               envEnabled("ODYSSEUS_VAULT_ENABLED"),
		galleryEnabled:             envEnabled("ODYSSEUS_GALLERY_ENABLED"),
		ttsEnabled:                 envEnabled("ODYSSEUS_TTS_ENABLED"),
		sttEnabled:                 envEnabled("ODYSSEUS_STT_ENABLED"),
		companionEnabled:           envEnabled("ODYSSEUS_COMPANION_ENABLED"),
		webhooksEnabled:            envEnabled("ODYSSEUS_WEBHOOKS_ENABLED"),
		codexBridgeEnabled:         envEnabledDefault("ODYSSEUS_CODEX_BRIDGE_ENABLED", true),
		claudeBridgeEnabled:        envEnabled("ODYSSEUS_CLAUDE_BRIDGE_ENABLED"),
		agentMigrationEnabled:      envEnabled("ODYSSEUS_AGENT_MIGRATION_ENABLED"),
		contextBudgetEnabled:       envEnabled("ODYSSEUS_CONTEXT_BUDGET_ENABLED"),
	}
}

func (a *odysseusAdapter) Info() Info {
	missing := []string{}
	if a.baseURL == "" {
		missing = append(missing, "ODYSSEUS_BASE_URL")
	}
	if a.token == "" {
		missing = append(missing, "ODYSSEUS_API_TOKEN")
	}
	if a.sessionID == "" {
		missing = append(missing, "ODYSSEUS_AGENT_SESSION_ID")
	}
	return Info{
		ID:                   "odysseus",
		Name:                 "Odysseus Workspace Agent",
		Type:                 "odysseus",
		Enabled:              a.enabled,
		Configured:           len(missing) == 0 && a.validBaseURL() == "",
		ExecutionEnabled:     a.enabled && len(missing) == 0 && a.validBaseURL() == "",
		RequiresApproval:     true,
		ReadOnlyDefault:      true,
		Capabilities:         a.capabilities(),
		Architecture:         a.architecture(),
		Controls:             a.controls(),
		MissingConfiguration: missing,
		Endpoint:             safety.RedactURL(a.baseURL),
	}
}

func (a *odysseusAdapter) validBaseURL() string {
	parsed, err := url.Parse(a.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "ODYSSEUS_BASE_URL must be an absolute HTTP or HTTPS URL"
	}
	if parsed.User != nil {
		return "ODYSSEUS_BASE_URL must not contain credentials"
	}
	host := strings.ToLower(parsed.Hostname())
	if !a.allowedHost["*"] && !a.allowedHost[host] {
		return "Odysseus host is not in AGENT_RUNTIME_ALLOWED_HOSTS"
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return "Odysseus endpoint uses blocked address space"
	}
	return ""
}

func (a *odysseusAdapter) HealthCheck(parent context.Context) Health {
	started := time.Now()
	health := Health{RuntimeID: "odysseus", Status: "disabled", CheckedAt: time.Now().UTC()}
	if !a.enabled {
		health.Reason = "ODYSSEUS_AGENT_ENABLED is false"
		return health
	}
	if reason := a.validBaseURL(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if a.token == "" {
		health.Status = "blocked"
		health.Reason = "ODYSSEUS_API_TOKEN is required for scoped Odysseus access"
		return health
	}
	if a.sessionID == "" {
		health.Status = "blocked"
		health.Reason = "ODYSSEUS_AGENT_SESSION_ID is required for controlled runtime execution"
		return health
	}
	ctx, cancel := context.WithTimeout(parent, minDuration(a.timeout, 10*time.Second))
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/api/codex/capabilities", nil)
	a.authorize(req)
	resp, err := a.directHTTPClient().Do(req)
	if err != nil {
		health.Status = "unavailable"
		health.Reason = safety.RedactSecrets(err.Error())
		return health
	}
	defer resp.Body.Close()
	health.LatencyMs = time.Since(started).Milliseconds()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		health.Status = "auth_required"
		health.Reason = "Odysseus rejected the configured API token"
		return health
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		health.Status = "unavailable"
		health.Reason = fmt.Sprintf("Odysseus capabilities returned HTTP %d", resp.StatusCode)
		return health
	}
	health.Status = "ready"
	health.Reason = "Odysseus scoped capabilities API is reachable; " + strings.Join(a.ecosystemReadiness(), ", ")
	return health
}

func (a *odysseusAdapter) ListSkills(context.Context) []Skill {
	skills := []Skill{{
		ID:               "odysseus:agent-mode",
		RuntimeID:        "odysseus",
		Name:             "agent mode",
		Category:         "agent",
		RiskLevel:        "medium",
		ApprovalRequired: true,
		ExecutionMode:    "approved_chat_stream",
		Source:           "ODYSSEUS_AGENT_SESSION_ID",
		Description:      "Controlled Odysseus /api/chat_stream agent run through the configured session.",
		Tags:             []string{"odysseus", "agent"},
	}}
	add := func(enabled bool, id string, name string, category string, risk string, description string, tags ...string) {
		if !enabled {
			return
		}
		skills = append(skills, Skill{
			ID:               "odysseus:" + id,
			RuntimeID:        "odysseus",
			Name:             name,
			Category:         category,
			RiskLevel:        risk,
			ApprovalRequired: risk != "low",
			ExecutionMode:    "approved_scoped_api",
			Source:           "ODYSSEUS_*_ENABLED",
			Description:      description,
			Tags:             append([]string{"odysseus"}, tags...),
		})
	}
	add(a.todosEnabled, "todos", "todos", "task", "low", "Todo and checklist operations through scoped Odysseus access.", "todo")
	add(a.emailEnabled, "email", "email", "communication", "high", "Email read/draft/send surfaces; external sending remains HAI approval-gated.", "email", "high-risk")
	add(a.calendarEnabled, "calendar", "calendar", "schedule", "medium", "Calendar operations through scoped Odysseus access.", "calendar")
	add(a.contactsEnabled, "contacts", "contacts", "people", "medium", "Contact lookup and relationship context through scoped Odysseus access.", "contacts")
	add(a.documentsEnabled, "documents", "documents", "document", "medium", "Document library and extraction operations through scoped Odysseus access.", "documents")
	add(a.memorySyncEnabled, "memory-sync", "memory sync", "memory", "medium", "Memory review/sync surfaces for compact context updates.", "memory")
	add(a.notesEnabled, "notes", "notes", "knowledge", "low", "Notes and lightweight knowledge capture through scoped Odysseus access.", "notes")
	add(a.tasksEnabled, "tasks", "tasks", "task", "low", "Task operations through scoped Odysseus access.", "task")
	add(a.researchEnabled, "research", "research", "research", "medium", "Research mode is available only when ODYSSEUS_AGENT_ALLOW_RESEARCH also permits it for execution.", "research")
	add(a.searchEnabled, "search", "search", "research", "medium", "Search/web-fetch mode is available only when ODYSSEUS_AGENT_ALLOW_WEB_SEARCH also permits it for execution.", "search")
	add(a.mcpEnabled, "mcp", "MCP", "tool", "high", "MCP server/tool access through Odysseus policy boundaries.", "mcp", "high-risk")
	add(a.cookbookEnabled, "cookbook", "Cookbook", "model-serving", "medium", "Model-serving diagnostics, presets, serve/stop/logs through Odysseus Cookbook scopes.", "models")
	add(a.localModelDiscoveryEnabled, "local-model-discovery", "local model discovery", "model-routing", "low", "Local model endpoint and hardware-fit discovery.", "local-models")
	add(a.shellEnabled, "shell", "shell", "host-control", "high", "Shell access is high-risk and only executable when ODYSSEUS_AGENT_ALLOW_BASH is also true.", "shell", "high-risk")
	add(a.browserEnabled, "browser", "browser", "browser", "high", "Browser surface visibility through Odysseus; HAI keeps consequential browsing actions approval-gated.", "browser", "high-risk")
	add(a.vaultEnabled, "vault", "vault", "sensitive-data", "high", "Vault access is sensitive and must remain scoped and approval-gated.", "vault", "high-risk")
	add(a.galleryEnabled, "gallery", "gallery", "media", "medium", "Gallery/media surfaces through scoped Odysseus access.", "media")
	add(a.ttsEnabled, "tts", "text to speech", "voice", "medium", "Text-to-speech surface through scoped Odysseus access.", "voice")
	add(a.sttEnabled, "stt", "speech to text", "voice", "medium", "Speech-to-text surface through scoped Odysseus access.", "voice")
	add(a.companionEnabled, "companion", "companion", "device", "high", "Companion/device surface remains high-risk and approval-gated.", "device", "high-risk")
	add(a.webhooksEnabled, "webhooks", "webhooks", "integration", "high", "Webhook surface can affect external systems and remains approval-gated.", "webhook", "high-risk")
	add(a.codexBridgeEnabled, "codex-bridge", "Codex bridge", "agent-bridge", "medium", "Codex bridge integration through Odysseus policy boundaries.", "codex")
	add(a.claudeBridgeEnabled, "claude-bridge", "Claude bridge", "agent-bridge", "medium", "Claude bridge integration through Odysseus policy boundaries.", "claude")
	add(a.agentMigrationEnabled, "agent-migration", "agent migration", "agent-bridge", "medium", "Agent migration manifests and compatibility support.", "migration")
	add(a.contextBudgetEnabled, "context-budget", "context budget", "context", "low", "Context budget and compaction support.", "context")
	return skills
}

func (a *odysseusAdapter) ExecuteTask(parent context.Context, task Task) Result {
	started := time.Now()
	if result, blocked := emergencyStopResult("odysseus"); blocked {
		return result
	}
	if reason := a.validBaseURL(); reason != "" {
		return Result{RuntimeID: "odysseus", Status: "blocked", Message: reason, ExitCode: -1}
	}
	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()
	form := url.Values{}
	form.Set("message", task.Prompt)
	form.Set("session", a.sessionID)
	form.Set("mode", "agent")
	allowBash := a.allowBash && a.shellEnabled
	allowWebSearch := a.allowWebSearch && a.searchEnabled
	allowResearch := a.allowResearch && a.researchEnabled
	form.Set("allow_bash", strconv.FormatBool(allowBash))
	form.Set("allow_web_search", strconv.FormatBool(allowWebSearch))
	form.Set("use_web", strconv.FormatBool(allowWebSearch))
	form.Set("use_research", strconv.FormatBool(allowResearch))
	if a.workspace != "" {
		form.Set("workspace", a.workspace)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/chat_stream", strings.NewReader(form.Encode()))
	if err != nil {
		return runtimeFailure("odysseus", started, err.Error())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "018-HAI-Agent-Runtime/1.0")
	a.authorize(req)
	if result, blocked := emergencyStopResult("odysseus"); blocked {
		return result
	}
	resp, err := a.directHTTPClient().Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{RuntimeID: "odysseus", Status: "blocked", Message: "Odysseus execution exceeded the configured timeout and was stopped", ExitCode: -1, DurationMs: time.Since(started).Milliseconds()}
		}
		return runtimeFailure("odysseus", started, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Result{
			RuntimeID:  "odysseus",
			Status:     "failed",
			Message:    fmt.Sprintf("Odysseus returned HTTP %d: %s", resp.StatusCode, trimAndRedact(string(body), 4096)),
			ExitCode:   resp.StatusCode,
			DurationMs: time.Since(started).Milliseconds(),
		}
	}
	output, streamErr := readOdysseusStream(resp.Body, a.outputLimit)
	if streamErr != nil {
		return runtimeFailure("odysseus", started, streamErr.Error())
	}
	if strings.TrimSpace(output) == "" {
		return Result{RuntimeID: "odysseus", Status: "failed", Message: "Odysseus completed without a final response", ExitCode: -1, DurationMs: time.Since(started).Milliseconds()}
	}
	return Result{
		RuntimeID:  "odysseus",
		Status:     "completed",
		Message:    "Odysseus completed the approved agent task",
		Output:     output,
		ExitCode:   0,
		DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: []string{
			"server-side approval verified",
			fmt.Sprintf("Odysseus agent mode invoked with allow_bash=%t allow_web_search=%t use_research=%t", allowBash, allowWebSearch, allowResearch),
			"configured session and scoped API token used",
			"Odysseus scoped API and token permissions remain authoritative; HTTP 403 is not bypassed",
			"bounded SSE output captured",
		},
	}
}

func (a *odysseusAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return unsupportedStopTask("odysseus", taskID, "Odysseus chat_stream requests are currently bounded by HAI timeouts; no durable Odysseus stop endpoint is configured in the adapter yet")
}

func (a *odysseusAdapter) capabilities() []string {
	return []string{
		"agent mode through /api/chat_stream",
		"scoped Codex API: todos, email, memory, calendar, documents, and cookbook",
		"todos, reminders, notes, and task/checklist operations",
		"email read, draft-document, draft, and send surfaces through token scopes",
		"calendar, contacts, and document library operations through token scopes",
		"memory review/sync plus compact agent-migration manifests",
		"session search, RAG, document extraction, and workspace context",
		"research/search/web-fetch pipeline",
		"MCP manager and MCP servers",
		"Cookbook model-serving diagnostics, cached model discovery, presets, serve, stop, and logs",
		"local model endpoint/model discovery and hardware-fit helpers",
		"workspace/browser/files/vault/gallery/TTS/STT/companion/webhook surfaces",
		"Codex and Claude bridge integrations",
		"context budget, compaction, prompt security, and tool security layers",
	}
}

func (a *odysseusAdapter) architecture() []string {
	return []string{
		"HAI workflow intake, policy, and approval queue",
		"HAI agent-runtime registry",
		"Odysseus scoped /api/codex capability boundary",
		"Odysseus /api/chat_stream agent loop",
		"Odysseus prompt-security, tool-policy, and tool-security gates",
		"Odysseus memory, session search, RAG, documents, and workspace stores",
		"Odysseus MCP, Cookbook, Codex, Claude, companion, and webhook bridges",
		"HAI source-grounded verification, audit log, and completion state machine",
	}
}

func (a *odysseusAdapter) controls() []string {
	controls := []string{
		"disabled by default through ODYSSEUS_AGENT_ENABLED",
		"server-side HAI approval required before every task",
		"scoped ODYSSEUS_API_TOKEN required; Odysseus 403 responses are treated as intentional restrictions",
		"base URL constrained by AGENT_RUNTIME_ALLOWED_HOSTS with no redirects and link-local blocking",
		"preselected ODYSSEUS_AGENT_SESSION_ID required; HAI does not create arbitrary sessions",
		"bounded timeout and SSE output capture with secret redaction",
		"HAI uses HTTP APIs only; no SSH, Docker, direct database, Python imports, or Odysseus internals",
		"email sending, calendar writes, document deletion, host control, and public posting stay behind HAI approval workflows",
	}
	if a.shellEnabled && a.allowBash {
		controls = append(controls, "Odysseus shell ecosystem is enabled and /api/chat_stream allow_bash=true; use only with a dedicated local workspace")
	} else if a.shellEnabled {
		controls = append(controls, "Odysseus shell ecosystem is visible but /api/chat_stream allow_bash=false until ODYSSEUS_AGENT_ALLOW_BASH=true")
	} else {
		controls = append(controls, "Odysseus shell/bash execution disabled by ODYSSEUS_SHELL_ENABLED=false and ODYSSEUS_AGENT_ALLOW_BASH=false")
	}
	if a.searchEnabled && a.allowWebSearch {
		controls = append(controls, "web search may be used only because ODYSSEUS_SEARCH_ENABLED and ODYSSEUS_AGENT_ALLOW_WEB_SEARCH are both true")
	} else {
		controls = append(controls, "web search disabled for runtime execution unless ODYSSEUS_SEARCH_ENABLED and ODYSSEUS_AGENT_ALLOW_WEB_SEARCH are both true")
	}
	if a.researchEnabled && a.allowResearch {
		controls = append(controls, "research mode may be used only because ODYSSEUS_RESEARCH_ENABLED and ODYSSEUS_AGENT_ALLOW_RESEARCH are both true")
	} else {
		controls = append(controls, "research mode disabled for runtime execution unless ODYSSEUS_RESEARCH_ENABLED and ODYSSEUS_AGENT_ALLOW_RESEARCH are both true")
	}
	if a.workspace != "" {
		controls = append(controls, "Odysseus workspace forwarded as ODYSSEUS_AGENT_WORKSPACE")
	}
	return controls
}

func (a *odysseusAdapter) ecosystemReadiness() []string {
	return []string{
		"codex-api=" + boolLabel(a.codexBridgeEnabled),
		"todos=" + boolLabel(a.todosEnabled),
		"email=" + boolLabel(a.emailEnabled),
		"calendar=" + boolLabel(a.calendarEnabled),
		"contacts=" + boolLabel(a.contactsEnabled),
		"documents=" + boolLabel(a.documentsEnabled),
		"memory-sync=" + boolLabel(a.memorySyncEnabled),
		"notes=" + boolLabel(a.notesEnabled),
		"tasks=" + boolLabel(a.tasksEnabled),
		"research=" + boolLabel(a.researchEnabled),
		"search=" + boolLabel(a.searchEnabled),
		"mcp=" + boolLabel(a.mcpEnabled),
		"cookbook=" + boolLabel(a.cookbookEnabled),
		"local-model-discovery=" + boolLabel(a.localModelDiscoveryEnabled),
		"shell=" + boolLabel(a.shellEnabled && a.allowBash),
		"browser=" + boolLabel(a.browserEnabled),
		"vault=" + boolLabel(a.vaultEnabled),
		"gallery=" + boolLabel(a.galleryEnabled),
		"tts=" + boolLabel(a.ttsEnabled),
		"stt=" + boolLabel(a.sttEnabled),
		"companion=" + boolLabel(a.companionEnabled),
		"webhooks=" + boolLabel(a.webhooksEnabled),
		"claude-bridge=" + boolLabel(a.claudeBridgeEnabled),
		"agent-migration=" + boolLabel(a.agentMigrationEnabled),
		"context-budget=" + boolLabel(a.contextBudgetEnabled),
	}
}

func (a *odysseusAdapter) authorize(req *http.Request) {
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
}

func readOdysseusStream(reader io.Reader, limit int64) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, limit))
	scanner.Buffer(make([]byte, 64*1024), int(minInt64(limit, 1024*1024)))
	var output strings.Builder
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			break
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if delta, ok := event["delta"].(string); ok {
			output.WriteString(delta)
		}
		if message, ok := event["error"].(string); ok && strings.TrimSpace(message) != "" {
			return "", fmt.Errorf("Odysseus stream error: %s", safety.RedactSecrets(message))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !done {
		return "", fmt.Errorf("Odysseus stream ended before completion or exceeded the configured output limit")
	}
	return trimAndRedact(output.String(), limit), nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	original := len(p)
	if w.remaining <= 0 {
		return original, nil
	}
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	_, err := w.writer.Write(p)
	w.remaining -= int64(len(p))
	return original, err
}

func safeEnvironment(allow []string, additions map[string]string) []string {
	env := []string{}
	for _, key := range append([]string{"PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "WINDIR", "TEMP", "TMP"}, allow...) {
		if value, ok := os.LookupEnv(key); ok && validEnvKey(key) {
			env = append(env, key+"="+value)
		}
	}
	for key, value := range additions {
		if validEnvKey(key) && strings.TrimSpace(value) != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func runtimeFailure(runtimeID string, started time.Time, message string) Result {
	return Result{
		RuntimeID:  runtimeID,
		Status:     "failed",
		Message:    safety.RedactSecrets(message),
		ExitCode:   -1,
		DurationMs: time.Since(started).Milliseconds(),
	}
}

type runtimeLookupIPAddr func(context.Context, string) ([]net.IPAddr, error)

func noRedirectClient(timeout time.Duration) *http.Client {
	return noRedirectClientWithResolver(timeout, net.DefaultResolver.LookupIPAddr)
}

func noRedirectClientWithResolver(timeout time.Duration, lookup runtimeLookupIPAddr) *http.Client {
	dialTimeout := timeout
	if dialTimeout <= 0 || dialTimeout > 10*time.Second {
		dialTimeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 nil,
			MaxIdleConns:          8,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: dialTimeout,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, fmt.Errorf("agent runtime: invalid network address: %w", err)
				}
				resolved, err := lookup(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("agent runtime: resolve endpoint: %w", err)
				}
				if len(resolved) == 0 {
					return nil, fmt.Errorf("agent runtime: endpoint resolved to no addresses")
				}
				for _, candidate := range resolved {
					if runtimeIPAddressBlocked(candidate.IP) {
						return nil, fmt.Errorf("agent runtime: endpoint resolved to blocked address space")
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
			},
		},
	}
}

func (a *odysseusAdapter) directHTTPClient() *http.Client {
	if a.httpClient != nil {
		return a.httpClient
	}
	return noRedirectClient(a.timeout)
}

func runtimeIPAddressBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.String() == "169.254.169.254"
}

func csvValues(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func csvMap(value string) map[string]bool {
	result := map[string]bool{}
	for _, item := range csvValues(value) {
		result[strings.ToLower(item)] = true
	}
	return result
}

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func envEnabledDefault(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "":
		return fallback
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

func boolLabel(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func countLabel(value int) string {
	if value == 0 {
		return "none"
	}
	return strconv.Itoa(value) + " configured"
}

func intEnv(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func boundedIntEnv(name string, fallback, minimum, maximum int) int {
	value := intEnv(name, fallback)
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		letter := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		if letter || index > 0 && (digit || r == '_') {
			continue
		}
		return false
	}
	return true
}

func trimAndRedact(value string, limit int64) string {
	value = strings.TrimSpace(value)
	if int64(len(value)) > limit {
		value = value[:limit]
	}
	return strings.TrimSpace(safety.RedactSecrets(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

package agentruntime

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"unicode/utf8"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/pathsafety"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

const (
	defaultTimeoutSeconds                = 120
	defaultOutputLimit                   = 64 * 1024
	maxOutputLimit                       = 1024 * 1024
	maxTaskPromptBytes                   = 50 * 1024
	openClawGatewayProtocolVersion       = 4
	openClawGatewayTaskLedgerLimit       = 50
	openClawGatewayCapabilityLimit       = 500
	openClawGatewayArtifactLimit         = 20
	openClawCLIExecutionBlockedReason    = "direct OpenClaw CLI execution is blocked until HAI can verify run-bound effective sandbox and tool policy for the exact task"
	openClawMaintenanceGateMissingReason = "OpenClaw task execution is blocked because maintenance admission is not configured"
	hermesToolPolicyMediationBlockReason = "Hermes task execution is blocked until HAI can authorize each Hermes tool invocation; task-level approval does not mediate configured side effects"
	odysseusToolMediationBlockReason     = "Odysseus task execution is blocked until HAI can authorize and audit each Odysseus tool invocation; task-level approval does not mediate configured side effects"
)

type Task struct {
	ID                 string
	ExecutionReference string
	RuntimeModel       string
	Prompt             string
	ProjectKey         string
	OwnerIdentity      string
	HumanApproved      bool
	ApprovalSourceID   string
	FinalEffectProof   FinalEffectAuthorizationProof
}

type Result struct {
	RuntimeID string `json:"runtimeId"`
	// ExecutionReference identifies durable work created outside the backend
	// process, such as a host-runtime job. It is an opaque audit reference, not
	// a user-supplied target or command.
	ExecutionReference           string      `json:"executionReference,omitempty"`
	Status                       string      `json:"status"`
	Message                      string      `json:"message,omitempty"`
	Output                       string      `json:"output,omitempty"`
	RouteTrace                   *RouteTrace `json:"routeTrace,omitempty"`
	ExitCode                     int         `json:"exitCode"`
	DurationMs                   int64       `json:"durationMs"`
	AuditEvents                  []string    `json:"auditEvents"`
	durableCancellationConfirmed bool
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
	RuntimeID string `json:"runtimeId"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	// Version is populated only from a validated authenticated runtime response.
	// Health-only probes deliberately leave it empty rather than inferring identity.
	Version                     string                              `json:"version,omitempty"`
	GatewayProtocolValidated    bool                                `json:"gatewayProtocolValidated"`
	GatewayAuthenticated        bool                                `json:"gatewayAuthenticated"`
	GatewayScope                string                              `json:"gatewayScope,omitempty"`
	GatewayEndpointSHA256       string                              `json:"gatewayEndpointSha256,omitempty"`
	GatewayEvidenceSchema       string                              `json:"gatewayEvidenceSchema,omitempty"`
	GatewayTaskLedger           *GatewayTaskLedgerSummary           `json:"gatewayTaskLedger,omitempty"`
	GatewayCapabilityCatalog    *GatewayCapabilityCatalogSummary    `json:"gatewayCapabilityCatalog,omitempty"`
	GatewayPreparedModelCatalog *GatewayPreparedModelCatalogSummary `json:"gatewayPreparedModelCatalog,omitempty"`
	GatewayAgentRoster          *GatewayAgentRosterSummary          `json:"gatewayAgentRoster,omitempty"`
	CheckedAt                   time.Time                           `json:"checkedAt"`
	LatencyMs                   int64                               `json:"latencyMs"`
}

// GatewayTaskLedgerSummary is a bounded, non-persistent projection obtained
// only through the optional operator.read task-ledger discovery. It contains
// status counts only: task titles, IDs, prompts, owner data, and errors never
// cross the HAI gateway boundary.
type GatewayTaskLedgerSummary struct {
	SampledTasks int            `json:"sampledTasks"`
	StatusCounts map[string]int `json:"statusCounts"`
	Truncated    bool           `json:"truncated"`
}

// GatewayCapabilityCatalogSummary is a bounded aggregate obtained only through
// the optional operator.read capability discovery. It deliberately excludes
// skill names, descriptions, tool names, plugin IDs, arguments, and raw
// Gateway payloads; HAI uses it only to prove that a read-only inventory was
// available at the time of discovery.
type GatewayCapabilityCatalogSummary struct {
	SampledSkills      int            `json:"sampledSkills"`
	EligibleSkills     int            `json:"eligibleSkills"`
	SampledCommands    int            `json:"sampledCommands"`
	ToolCountsBySource map[string]int `json:"toolCountsBySource"`
}

// GatewayPreparedModelCatalogSummary retains only availability counts from a
// prepared-only OpenClaw model catalog. It deliberately excludes model IDs,
// providers, endpoints, costs, credentials, and routing configuration.
type GatewayPreparedModelCatalogSummary struct {
	SampledModels             int `json:"sampledModels"`
	AvailableModels           int `json:"availableModels"`
	UnavailableModels         int `json:"unavailableModels"`
	UnknownAvailabilityModels int `json:"unknownAvailabilityModels"`
}

// GatewayAgentRosterSummary is the bounded, aggregate-only projection of an
// authenticated agents.list request. It deliberately excludes agent IDs,
// labels, model metadata, workspace paths, creation provenance, and raw
// Gateway payloads.
type GatewayAgentRosterSummary struct {
	SampledAgents    int `json:"sampledAgents"`
	AgentCount       int `json:"agentCount"`
	SystemCount      int `json:"systemCount"`
	UnknownKindCount int `json:"unknownKindCount"`
}

type Info struct {
	ID                         string                    `json:"id"`
	Name                       string                    `json:"name"`
	Type                       string                    `json:"type"`
	Enabled                    bool                      `json:"enabled"`
	Configured                 bool                      `json:"configured"`
	ExecutionEnabled           bool                      `json:"executionEnabled"`
	RequiresApproval           bool                      `json:"requiresApproval"`
	ReadOnlyDefault            bool                      `json:"readOnlyDefault"`
	Capabilities               []string                  `json:"capabilities"`
	Architecture               []string                  `json:"architecture,omitempty"`
	Controls                   []string                  `json:"controls,omitempty"`
	Ecosystem                  []RuntimeEcosystemSurface `json:"ecosystem,omitempty"`
	EcosystemPath              string                    `json:"ecosystemPath,omitempty"`
	EcosystemRollbackAvailable bool                      `json:"ecosystemRollbackAvailable"`
	MissingConfiguration       []string                  `json:"missingConfiguration,omitempty"`
	Endpoint                   string                    `json:"endpoint,omitempty"`
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
	RuntimeID          string   `json:"runtimeId"`
	TaskID             string   `json:"taskId"`
	ExecutionReference string   `json:"executionReference,omitempty"`
	Status             string   `json:"status"`
	Message            string   `json:"message,omitempty"`
	EvidenceURI        string   `json:"evidenceUri,omitempty"`
	AuditEvents        []string `json:"auditEvents,omitempty"`
}

// DelegatedSessionReconcileResult is a deliberately small terminal-state
// projection. Gateway-private reply text, error detail, session IDs, and run
// IDs are not imported into HAI's general event stream.
type DelegatedSessionReconcileResult struct {
	RuntimeID          string
	TaskID             string
	OwnerIdentity      string
	ExecutionReference string
	Status             string
	Message            string
	FinishedAt         time.Time
	AuditEvents        []string
}

// OpenClawGatewayReceipt is the private mapping between HAI's opaque external
// execution reference and the Gateway identifiers required to reconcile or
// cancel a delegated run. Session and run identifiers must never reach the
// browser/API response or the general automation event stream.
type OpenClawGatewayReceipt struct {
	ExecutionReference   string
	RuntimeTaskID        string
	OwnerIdentity        string
	Status               string
	SessionKey           string
	SessionID            string
	RequestedModel       string
	RunID                string
	CreatedAt            time.Time
	TerminalStatus       string
	TerminalAt           time.Time
	CancellationIntentID string
	CancellationStatus   string
	CancellationMessage  string
	CancellationAttempts int
	CancellationAt       time.Time
	CancellationTriedAt  time.Time
}

// OpenClawGatewayReceiptStore keeps Gateway-private identifiers in a narrowly
// scoped durable ledger. A nil store intentionally disables delegated session
// creation: HAI must not start work it cannot reconcile after a restart.
type OpenClawGatewayReceiptStore interface {
	CreateOpenClawGatewayReceipt(context.Context, OpenClawGatewayReceipt) error
	MarkOpenClawGatewayReceiptAdmitted(context.Context, OpenClawGatewayReceipt) (bool, error)
	MarkOpenClawGatewayReceiptNotAdmitted(context.Context, string, string, string) (bool, error)
	FindOpenClawGatewayReceipt(string) (OpenClawGatewayReceipt, error)
}

// OpenClawGatewayCancellationStore persists an idempotent, receipt-bound stop
// intent and its delivery outcome. It deliberately cannot finalize a receipt.
type OpenClawGatewayCancellationStore interface {
	EnsureOpenClawGatewayCancellationIntent(context.Context, OpenClawGatewayReceipt, time.Time) (OpenClawGatewayReceipt, error)
	RecordOpenClawGatewayCancellationOutcome(context.Context, OpenClawGatewayReceipt, string, string, time.Time) error
	ListOpenClawGatewayCancellationReceipts(int, time.Time, string) ([]OpenClawGatewayReceipt, error)
}

// OpenClawGatewayArtifactStore is intentionally metadata-only. It must not be
// implemented by a transcript, object-store, or browser-facing repository.
type OpenClawGatewayArtifactStore interface {
	CreateOpenClawGatewayArtifactDescriptors(context.Context, string, []GatewayArtifactDescriptor) error
}

// GatewayArtifactDescriptor deliberately retains only bounded, typed metadata.
// The original Gateway artifact ID, title, source, URL, and content remain
// outside HAI's general runtime and audit surfaces.
type GatewayArtifactDescriptor struct {
	Digest    string
	Type      string
	MIMEType  string
	SizeBytes *int64 `json:"sizeBytes"`
}

// DelegatedArtifactImportResult projects a bounded metadata import. It never
// contains remote artifact references, titles, URLs, or content.
type DelegatedArtifactImportResult struct {
	RuntimeID   string
	TaskID      string
	Status      string
	Count       int
	AuditEvents []string
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

// referenceStopAdapter is intentionally narrower than Adapter. Only runtimes
// that persist an opaque external execution reference may receive a restart-
// safe stop request after their in-process context is gone.
type referenceStopAdapter interface {
	StopTaskWithReference(context.Context, string, string, string) StopResult
}

type ownerBoundStopAdapter interface {
	StopTaskForOwner(context.Context, string, string, bool) StopResult
}

type exactGatewayReceiptStopAdapter interface {
	StopOpenClawGatewayReceipt(context.Context, OpenClawGatewayReceipt) StopResult
}

type openClawGatewayDelegationCapability interface {
	OpenClawGatewayDelegationReady() bool
}

type delegatedSessionReconcileAdapter interface {
	ReconcileDelegatedSession(context.Context, string, string, string) DelegatedSessionReconcileResult
}

type delegatedArtifactImportAdapter interface {
	ImportDelegatedArtifacts(context.Context, string, string) DelegatedArtifactImportResult
}

// delegatedVerifiedArtifactImportAdapter is available only to the durable
// reconciliation path after it has observed a completed Gateway run.
type delegatedVerifiedArtifactImportAdapter interface {
	ImportDelegatedArtifactsAfterTerminalVerification(context.Context, string, string, DelegatedSessionReconcileResult) DelegatedArtifactImportResult
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
	return DefaultRegistryWithFinalEffectVerifierAndHostRuntime(verifier, nil)
}

// DefaultRegistryWithFinalEffectVerifierAndHostRuntime is the production
// composition point for runtimes that need a Windows-host execution bridge.
// A nil dispatcher leaves DeepSeek Harness unavailable rather than allowing
// the backend container to execute a host-oriented runtime directly.
func DefaultRegistryWithFinalEffectVerifierAndHostRuntime(verifier FinalEffectProofVerifier, dispatcher hostruntime.Dispatcher) *Registry {
	return DefaultRegistryWithFinalEffectVerifierAndHostRuntimeAndOpenClawGatewayReceiptStore(verifier, dispatcher, nil)
}

// DefaultRegistryWithFinalEffectVerifierAndHostRuntimeAndOpenClawGatewayReceiptStore
// adds the private receipt ledger required for restart-safe OpenClaw Gateway
// delegation. Keeping the existing factory preserves read-only runtime
// discovery for callers that have not composed the durable receipt store.
func DefaultRegistryWithFinalEffectVerifierAndHostRuntimeAndOpenClawGatewayReceiptStore(verifier FinalEffectProofVerifier, dispatcher hostruntime.Dispatcher, receiptStore OpenClawGatewayReceiptStore) *Registry {
	openClaw := newOpenClawAdapterFromEnv()
	openClaw.gatewayReceiptStore = receiptStore
	if artifactStore, ok := receiptStore.(OpenClawGatewayArtifactStore); ok {
		openClaw.gatewayArtifactStore = artifactStore
	}
	return NewRegistryWithFinalEffectVerifier(
		verifier,
		newHermesAdapterFromEnv(),
		newDeepSeekHarnessAdapterFromEnv(dispatcher),
		newOdysseusAdapterFromEnv(),
		openClaw,
	)
}

func (r *Registry) List() []Info {
	result := []Info{}
	for _, id := range []string{"hermes", "deepseek-harness", "odysseus", "openclaw"} {
		if adapter := r.adapters[id]; adapter != nil {
			result = append(result, adapter.Info())
		}
	}
	return result
}

// RuntimeOverview combines the stable runtime registry with its bounded health
// probe. Dashboard callers need both values together, so serving them from one
// endpoint avoids duplicate request setup while keeping probe state explicit.
type RuntimeOverview struct {
	Runtimes []Info   `json:"runtimes"`
	Health   []Health `json:"health"`
}

func (r *Registry) Overview(ctx context.Context) RuntimeOverview {
	return RuntimeOverview{
		Runtimes: r.List(),
		Health:   r.Health(ctx),
	}
}

func (r *Registry) Health(ctx context.Context) []Health {
	result := []Health{}
	for _, id := range []string{"hermes", "deepseek-harness", "odysseus", "openclaw"} {
		if health, ok := r.HealthFor(ctx, id); ok {
			result = append(result, health)
		}
	}
	return result
}

// HealthFor probes exactly one registered runtime. Callers that render a
// single runtime must use this instead of Health so unrelated provider and
// local-runtime checks are not started as a side effect.
func (r *Registry) HealthFor(ctx context.Context, runtimeID string) (Health, bool) {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	adapter := r.adapters[runtimeID]
	if adapter == nil {
		return Health{}, false
	}
	return adapter.HealthCheck(ctx), true
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
	task = canonicalizeRuntimeTaskScope(task)
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

func (r *Registry) StopTask(ctx context.Context, runtimeID string, taskID string, ownerIdentity string) StopResult {
	return r.stopTask(ctx, runtimeID, taskID, ownerIdentity, "")
}

// StopTaskWithReference may be used only by a caller that obtained the opaque
// reference from its own owner-bound, durable execution ledger. It allows an
// adapter to request cancellation after a backend restart without accepting a
// user-supplied Gateway session key.
func (r *Registry) StopTaskWithReference(ctx context.Context, runtimeID string, taskID string, ownerIdentity string, executionReference string) StopResult {
	return r.stopTask(ctx, runtimeID, taskID, ownerIdentity, executionReference)
}

// StopOpenClawGatewayReceipt requests cancellation only when the complete
// durable HAI receipt binding is present. The adapter re-reads and compares
// the stored session key and run ID immediately before Gateway access.
func (r *Registry) StopOpenClawGatewayReceipt(ctx context.Context, expected OpenClawGatewayReceipt) StopResult {
	base := StopResult{
		RuntimeID:          "openclaw",
		TaskID:             strings.TrimSpace(expected.RuntimeTaskID),
		ExecutionReference: strings.TrimSpace(expected.ExecutionReference),
	}
	if strings.TrimSpace(expected.OwnerIdentity) == "" || strings.TrimSpace(expected.RuntimeTaskID) == "" ||
		!validOpenClawGatewayReceiptReference(expected.ExecutionReference) ||
		strings.TrimSpace(expected.SessionKey) == "" || !ValidOpenClawGatewayRunID(expected.RunID) ||
		(expected.Status != "admitted" && expected.Status != "needs_review") {
		base.Status = "blocked"
		base.Message = "exact owner, task, execution reference, session key, and run ID are required"
		base.AuditEvents = []string{"OpenClaw cancellation rejected without a complete durable run binding"}
		return base
	}
	adapter, ok := r.adapters["openclaw"].(exactGatewayReceiptStopAdapter)
	if !ok {
		base.Status = "blocked"
		base.Message = "OpenClaw exact-receipt cancellation is unavailable"
		base.AuditEvents = []string{"exact OpenClaw receipt stop adapter is not registered"}
		return base
	}
	result := adapter.StopOpenClawGatewayReceipt(ctx, expected)
	return checkedStopResult(result, "openclaw", base.TaskID, base.ExecutionReference, true)
}

// ReconcileOpenClawGatewaySession asks a configured adapter to observe a
// previously admitted, owner-bound Gateway run. It does not execute, retry, or
// complete work; callers must persist any terminal projection separately.
func (r *Registry) ReconcileOpenClawGatewaySession(ctx context.Context, ownerIdentity string, taskID string, executionReference string) DelegatedSessionReconcileResult {
	adapter, ok := r.adapters["openclaw"].(delegatedSessionReconcileAdapter)
	if !ok {
		return DelegatedSessionReconcileResult{RuntimeID: "openclaw", TaskID: strings.TrimSpace(taskID), OwnerIdentity: strings.TrimSpace(ownerIdentity), ExecutionReference: strings.TrimSpace(executionReference), Status: "indeterminate", Message: "OpenClaw delegated-session reconciliation is unavailable"}
	}
	return adapter.ReconcileDelegatedSession(ctx, taskID, ownerIdentity, executionReference)
}

// ImportOpenClawGatewayArtifacts is a metadata-only post-terminal operation.
// It cannot execute work or retrieve Gateway artifact contents.
func (r *Registry) ImportOpenClawGatewayArtifacts(ctx context.Context, taskID string, executionReference string) DelegatedArtifactImportResult {
	adapter, ok := r.adapters["openclaw"].(delegatedArtifactImportAdapter)
	if !ok {
		return DelegatedArtifactImportResult{RuntimeID: "openclaw", TaskID: strings.TrimSpace(taskID), Status: "unavailable"}
	}
	return adapter.ImportDelegatedArtifacts(ctx, taskID, executionReference)
}

// ImportOpenClawGatewayArtifactsAfterTerminalVerification is reserved for the
// reconciliation worker. The supplied result must be the completed terminal
// observation it just obtained for this owner-bound receipt.
func (r *Registry) ImportOpenClawGatewayArtifactsAfterTerminalVerification(ctx context.Context, taskID string, executionReference string, terminal DelegatedSessionReconcileResult) DelegatedArtifactImportResult {
	adapter, ok := r.adapters["openclaw"].(delegatedVerifiedArtifactImportAdapter)
	if !ok {
		return DelegatedArtifactImportResult{RuntimeID: "openclaw", TaskID: strings.TrimSpace(taskID), Status: "unavailable"}
	}
	return adapter.ImportDelegatedArtifactsAfterTerminalVerification(ctx, taskID, executionReference, terminal)
}

func (r *Registry) stopTask(ctx context.Context, runtimeID string, taskID string, ownerIdentity string, executionReference string) StopResult {
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
		if executionReference != "" {
			if adapter, ok := r.adapters[runtimeID].(referenceStopAdapter); ok {
				result := adapter.StopTaskWithReference(ctx, taskID, ownerIdentity, executionReference)
				result = checkedStopResult(result, runtimeID, taskID, executionReference, true)
				result.AuditEvents = append(result.AuditEvents, "HAI-managed local execution context cancellation requested; this does not prove remote cancellation")
				return result
			}
		}
		if adapter, ok := r.adapters[runtimeID].(ownerBoundStopAdapter); ok {
			result := adapter.StopTaskForOwner(ctx, taskID, ownerIdentity, true)
			result = checkedStopResult(result, runtimeID, taskID, "", false)
			result.AuditEvents = append(result.AuditEvents, "HAI-managed local execution context cancellation requested; durable host cancellation is reported only after repository confirmation")
			return result
		}
		return StopResult{
			RuntimeID: runtimeID,
			TaskID:    taskID,
			Status:    "cancellation_requested",
			Message:   "HAI requested cancellation of the active runtime task; downstream delivery is not yet verified",
			AuditEvents: []string{
				"running runtime task located",
				"HAI-managed local execution context cancellation requested; remote cancellation is not proven",
				"runtime cancellation delivery is not yet verified",
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
	if executionReference != "" {
		if adapter, ok := r.adapters[runtimeID].(referenceStopAdapter); ok {
			result := adapter.StopTaskWithReference(ctx, taskID, ownerIdentity, executionReference)
			return checkedStopResult(result, runtimeID, taskID, executionReference, true)
		}
	}
	if adapter, ok := r.adapters[runtimeID].(ownerBoundStopAdapter); ok {
		result := adapter.StopTaskForOwner(ctx, taskID, ownerIdentity, false)
		return checkedStopResult(result, runtimeID, taskID, "", false)
	}
	return StopResult{
		RuntimeID:   runtimeID,
		TaskID:      taskID,
		Status:      "blocked",
		Message:     "no active owner-bound runtime task was found",
		AuditEvents: []string{"untracked runtime stop rejected before adapter access"},
	}
}

func checkedStopResult(result StopResult, runtimeID, taskID, reference string, referenceKnown bool) StopResult {
	if strings.ToLower(strings.TrimSpace(result.RuntimeID)) != runtimeID ||
		strings.TrimSpace(result.TaskID) != taskID ||
		(referenceKnown && strings.TrimSpace(result.ExecutionReference) != strings.TrimSpace(reference)) {
		// The adapter may already have acted. Retain uncertainty and the requested
		// identity, not an acknowledgement or private payload from another run.
		return StopResult{
			RuntimeID: runtimeID, TaskID: taskID, ExecutionReference: strings.TrimSpace(reference),
			Status: "indeterminate", Message: "runtime cancellation response identity did not match the request; inspect the persisted stop intent before retrying",
			AuditEvents: []string{"adapter cancellation acknowledgement rejected because runtime, task or execution reference did not match", "cancellation was not repeated and downstream completion was not inferred"},
		}
	}
	return result
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

// OpenClawGatewayDelegationReady reports whether this registry currently
// permits new task execution through OpenClaw. It remains false until HAI can
// verify identity-bound sandbox policy for the exact Gateway run.
// Callers use it before persisting Gateway-specific references; an unavailable
// or non-production adapter deliberately reports false.
func (r *Registry) OpenClawGatewayDelegationReady() bool {
	if r == nil {
		return false
	}
	capability, ok := r.adapters["openclaw"].(openClawGatewayDelegationCapability)
	return ok && capability.OpenClawGatewayDelegationReady()
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
	if err := openClaw.refreshEcosystemInventory(); err != nil {
		return Info{}, err
	}
	return openClaw.Info(), nil
}

func (r *Registry) Execute(ctx context.Context, runtimeID string, task Task) Result {
	runtimeID = strings.ToLower(strings.TrimSpace(runtimeID))
	task = canonicalizeRuntimeTaskScope(task)
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
	// Registry constructors also support read-only/reference runtimes. Require
	// OpenClaw's admission dependency at the execution boundary, after preserving
	// the normal policy response for those non-executing registries.
	if runtimeID == "openclaw" {
		openClaw, ok := adapter.(*openClawAdapter)
		if !ok || openClaw == nil || openClaw.maintenanceGate == nil {
			return blockedRuntimeResult(runtimeID, openClawMaintenanceGateMissingReason, "OpenClaw execution rejected because maintenance admission is unavailable")
		}
	}
	if strings.TrimSpace(task.ID) == "" {
		return blockedRuntimeResult(runtimeID, "agent runtime task id is required", "untracked agent task rejected")
	}
	if strings.TrimSpace(task.OwnerIdentity) == "" {
		return blockedRuntimeResult(runtimeID, "agent runtime task owner is required", "ownerless agent task rejected")
	}
	if err := safety.ValidateRuntimeModel(runtimeID, task.RuntimeModel); err != nil {
		return blockedRuntimeResult(runtimeID, err.Error(), "runtime model selection rejected")
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
	emergencyStopCtx, cancelEmergencyStop := safety.WithEmergencyStop(ctx)
	defer cancelEmergencyStop()
	executionCtx, cancel, registered := r.registerRunningTask(emergencyStopCtx, runtimeID, task)
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
	if errors.Is(context.Cause(executionCtx), safety.ErrEmergencyStopActivated) {
		return blockedRuntimeResult(
			runtimeID,
			safety.EmergencyStopReason(),
			"emergency stop cancelled runtime work before adapter execution",
		)
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
	if executionCtx.Err() != nil {
		result.RuntimeID = firstNonEmpty(result.RuntimeID, runtimeID)
		if result.durableCancellationConfirmed {
			result.Status = "cancelled"
			result.Message = "runtime task was cancelled and its durable host job is no longer leaseable"
			result.ExitCode = -1
			result.AuditEvents = append(result.AuditEvents, "stop request confirmed the exact host job was durably revoked before execution")
		} else {
			result.Status = "indeterminate"
			result.Message = "runtime cancellation interrupted an active call; downstream effects require verification"
			if result.ExitCode == 0 {
				result.ExitCode = -1
			}
			if errors.Is(context.Cause(executionCtx), safety.ErrEmergencyStopActivated) {
				result.Message = "emergency stop interrupted an active runtime call; downstream effects require verification"
				result.AuditEvents = append(result.AuditEvents, "emergency stop cancellation observed after runtime adapter start")
			} else {
				result.AuditEvents = append(result.AuditEvents, "runtime cancellation observed after adapter start; downstream outcome is unverified")
			}
		}
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
	decision := safety.EvaluateEmergencyStopForExecution()
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
	configured := len(missing) == 0 && workspaceReason == ""
	blockers := append([]string(nil), missing...)
	if workspaceReason != "" {
		blockers = append(blockers, workspaceReason)
	}
	blockers = append(blockers, hermesToolPolicyMediationBlockReason)
	return Info{
		ID:                   "hermes",
		Name:                 "Hermes Agent",
		Type:                 "hermes",
		Enabled:              a.enabled,
		Configured:           configured,
		ExecutionEnabled:     false,
		RequiresApproval:     true,
		ReadOnlyDefault:      false,
		Capabilities:         a.capabilities(),
		Architecture:         a.architecture(),
		Controls:             a.controls(),
		MissingConfiguration: sortedUnique(blockers),
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
	health.Status = "blocked"
	health.Reason = hermesToolPolicyMediationBlockReason + "; executable and workspace are available: " + filepath.Base(path) + "; " + strings.Join(a.ecosystemReadiness(), ", ")
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
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			ExecutionMode:    "inventory_only_blocked",
			Source:           "HERMES_SKILLS",
			Description:      "Inventory only. Hermes execution is blocked until HAI can mediate each tool invocation.",
			Tags:             []string{"configured", "hermes"},
		})
	}
	for _, toolset := range sortedUnique(a.toolsets) {
		skills = append(skills, Skill{
			ID:               "hermes:toolset:" + toolset,
			RuntimeID:        "hermes",
			Name:             toolset,
			Category:         "toolset",
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			ExecutionMode:    "inventory_only_blocked",
			Source:           "HERMES_TOOLSETS",
			Description:      "Inventory only. Task approval does not mediate tool calls; Hermes execution is blocked.",
			Tags:             []string{"configured", "toolset", "hermes"},
		})
	}
	return skills
}

func (a *hermesAdapter) ExecuteTask(_ context.Context, _ Task) Result {
	if result, blocked := emergencyStopResult("hermes"); blocked {
		return result
	}
	return blockedRuntimeResult(
		"hermes",
		hermesToolPolicyMediationBlockReason,
		"Hermes CLI launch blocked before process creation because configured tool calls are not individually mediated",
	)
}

func (a *hermesAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return StopResult{
		RuntimeID: "hermes",
		TaskID:    strings.TrimSpace(taskID),
		Status:    "unsupported",
		Message:   "the current HAI adapter starts no Hermes process while per-tool authorization is unavailable, and it has no durable handle for processes started outside this adapter",
		AuditEvents: []string{
			"stop request recorded",
			"current Hermes adapter has no active process to stop",
			"no durable handle is available for an earlier or externally started Hermes process",
		},
	}
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
		"server-side task approval is necessary but does not authorize individual Hermes tool calls",
		hermesToolPolicyMediationBlockReason,
		"dedicated workspace must remain under AGENT_RUNTIME_WORKSPACE_ROOT",
		"Hermes CLI is not started by HAI until per-tool authorization is available",
		"configured profiles, skills, toolsets, and environment allowlists are inventory only; they do not enable execution",
	}
	if len(a.toolsets) > 0 {
		controls = append(controls, "configured inventory includes HERMES_TOOLSETS="+strings.Join(a.toolsets, ",")+"; these toolsets are not individually mediated")
	} else {
		controls = append(controls, "configured Hermes profile may enable platform toolsets; HAI does not mediate their invocations")
	}
	if len(a.skills) > 0 {
		controls = append(controls, "configured inventory includes HERMES_SKILLS="+strings.Join(a.skills, ",")+"; listed skills are not execution authorization")
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
	enabled                 bool
	executable              string
	workspace               string
	workspaceRoot           string
	ecosystemPath           string
	ecosystemRoots          []string
	stateDir                string
	configPath              string
	gatewayURL              string
	gatewayDockerHostIPs    []string
	gatewayToken            string
	gatewayDelegationToken  string
	gatewayAllowedModels    []string
	artifactDownloadOrigins []string
	gatewayReceiptStore     OpenClawGatewayReceiptStore
	gatewayArtifactStore    OpenClawGatewayArtifactStore
	thinking                string
	timeout                 time.Duration
	outputLimit             int64
	envAllow                []string
	allowedHost             map[string]bool
	gatewayResolver         func(context.Context, string) ([]net.IP, error)

	agentCLIEnabled                      bool
	gatewayEnabled                       bool
	gatewayProtocolDiscoveryEnabled      bool
	gatewayAuthenticatedDiscoveryEnabled bool
	gatewayTaskLedgerDiscoveryEnabled    bool
	gatewayCapabilityDiscoveryEnabled    bool
	gatewayModelCatalogDiscoveryEnabled  bool
	gatewayAgentRosterDiscoveryEnabled   bool
	gatewayDelegationEnabled             bool
	gatewayArtifactImportEnabled         bool
	messagesEnabled                      bool
	skillsEnabled                        bool
	pluginsEnabled                       bool
	mcpEnabled                           bool
	memoryEnabled                        bool
	cronEnabled                          bool
	browserEnabled                       bool
	canvasEnabled                        bool
	nodesEnabled                         bool
	voiceEnabled                         bool
	talkEnabled                          bool
	webchatEnabled                       bool
	pairingEnabled                       bool
	execApprovals                        bool
	hostToolsEnabled                     bool
	publicPosting                        bool
	webSearchEnabled                     bool
	multiAgentEnabled                    bool
	appSDKEnabled                        bool
	pluginSDKEnabled                     bool
	localModelsEnabled                   bool
	highRiskExecution                    bool
	sandboxRequired                      bool
	sandboxMode                          string
	sandboxDocker                        bool
	sandboxSSH                           bool
	sandboxOpenShell                     bool
	channelsEnabled                      []string
	providersEnabled                     []string
	companionApps                        []string

	inventoryMu                   sync.Mutex
	managedArchiveSelection       openClawArchiveSelection
	managedArchiveSelectionLoaded bool
	managedArchiveLoadError       string
	managedArchiveWarning         string
	maintenanceGate               func(context.Context) (func(), error)
	inventoryLoaded               bool
	inventoryPath                 string
	inventorySignature            string
	inventory                     openClawEcosystemInventory
}

// RuntimeID lets registry composition identify this adapter without scanning
// the configured OpenClaw ecosystem. Full inventory remains lazy until an
// operator explicitly opens runtime detail.
func (*openClawAdapter) RuntimeID() string { return "openclaw" }

func newOpenClawAdapterFromEnv() *openClawAdapter {
	adapter := &openClawAdapter{
		enabled:                              envEnabled("OPENCLAW_AGENT_ENABLED"),
		executable:                           firstNonEmpty(os.Getenv("OPENCLAW_EXECUTABLE"), "openclaw"),
		workspace:                            strings.TrimSpace(os.Getenv("OPENCLAW_WORKSPACE")),
		workspaceRoot:                        strings.TrimSpace(os.Getenv("AGENT_RUNTIME_WORKSPACE_ROOT")),
		ecosystemPath:                        strings.TrimSpace(firstNonEmpty(os.Getenv("OPENCLAW_ECOSYSTEM_PATH"), os.Getenv("OPENCLAW_WORKSPACE"))),
		ecosystemRoots:                       csvValues(os.Getenv("OPENCLAW_ECOSYSTEM_ALLOWED_ROOTS")),
		stateDir:                             strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR")),
		configPath:                           strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")),
		gatewayURL:                           strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_URL")),
		gatewayDockerHostIPs:                 csvValues(os.Getenv("OPENCLAW_GATEWAY_DOCKER_HOST_IPS")),
		gatewayToken:                         strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")),
		gatewayDelegationToken:               strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN")),
		gatewayAllowedModels:                 csvValues(os.Getenv("OPENCLAW_GATEWAY_ALLOWED_MODELS")),
		artifactDownloadOrigins:              csvValues(os.Getenv("OPENCLAW_ARTIFACT_DOWNLOAD_ORIGINS")),
		thinking:                             firstNonEmpty(os.Getenv("OPENCLAW_THINKING"), "high"),
		timeout:                              time.Duration(boundedIntEnv("OPENCLAW_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second,
		outputLimit:                          int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		envAllow:                             csvValues(os.Getenv("OPENCLAW_ENV_ALLOWLIST")),
		allowedHost:                          csvMap(firstNonEmpty(os.Getenv("AGENT_RUNTIME_ALLOWED_HOSTS"), "localhost,127.0.0.1,::1,openclaw")),
		agentCLIEnabled:                      envEnabled("OPENCLAW_AGENT_CLI_ENABLED"),
		gatewayEnabled:                       envEnabled("OPENCLAW_GATEWAY_ENABLED"),
		gatewayProtocolDiscoveryEnabled:      envEnabled("OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED"),
		gatewayAuthenticatedDiscoveryEnabled: envEnabled("OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED"),
		gatewayTaskLedgerDiscoveryEnabled:    envEnabled("OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED"),
		gatewayCapabilityDiscoveryEnabled:    envEnabled("OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED"),
		gatewayModelCatalogDiscoveryEnabled:  envEnabled("OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED"),
		gatewayAgentRosterDiscoveryEnabled:   envEnabled("OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED"),
		gatewayDelegationEnabled:             envEnabled("OPENCLAW_GATEWAY_DELEGATION_ENABLED"),
		gatewayArtifactImportEnabled:         envEnabled("OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED"),
		messagesEnabled:                      envEnabled("OPENCLAW_MESSAGES_ENABLED"),
		skillsEnabled:                        envEnabled("OPENCLAW_SKILLS_ENABLED"),
		pluginsEnabled:                       envEnabled("OPENCLAW_PLUGINS_ENABLED"),
		mcpEnabled:                           envEnabled("OPENCLAW_MCP_ENABLED"),
		memoryEnabled:                        envEnabled("OPENCLAW_MEMORY_ENABLED"),
		cronEnabled:                          envEnabled("OPENCLAW_CRON_ENABLED"),
		browserEnabled:                       envEnabled("OPENCLAW_BROWSER_ENABLED"),
		canvasEnabled:                        envEnabled("OPENCLAW_CANVAS_ENABLED"),
		nodesEnabled:                         envEnabled("OPENCLAW_NODES_ENABLED"),
		voiceEnabled:                         envEnabled("OPENCLAW_VOICE_ENABLED"),
		talkEnabled:                          envEnabled("OPENCLAW_TALK_ENABLED"),
		webchatEnabled:                       envEnabled("OPENCLAW_WEBCHAT_ENABLED"),
		pairingEnabled:                       envEnabled("OPENCLAW_PAIRING_ENABLED"),
		execApprovals:                        envEnabled("OPENCLAW_EXEC_APPROVALS_ENABLED"),
		hostToolsEnabled:                     envEnabled("OPENCLAW_HOST_TOOLS_ENABLED"),
		publicPosting:                        envEnabled("OPENCLAW_PUBLIC_POSTING_ENABLED"),
		webSearchEnabled:                     envEnabled("OPENCLAW_WEB_SEARCH_ENABLED"),

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
	adapter.inventoryMu.Lock()
	if err := adapter.loadManagedArchiveSelectionLocked(); err != nil {
		adapter.managedArchiveLoadError = err.Error()
		adapter.ecosystemPath = ""
	}
	adapter.inventoryMu.Unlock()
	return adapter
}

func (a *openClawAdapter) Info() Info {
	missing := []string{}
	a.inventoryMu.Lock()
	archiveLoadError := strings.TrimSpace(a.managedArchiveLoadError)
	archiveWarning := strings.TrimSpace(a.managedArchiveWarning)
	archiveRollbackAvailable := archiveLoadError == "" &&
		a.managedArchiveSelection.SelectedDigest != "" && a.managedArchiveSelection.PreviousDigest != ""
	a.inventoryMu.Unlock()
	if archiveLoadError != "" {
		missing = append(missing, "HAI-managed OpenClaw archive failed integrity validation; selection is disabled")
	}
	if archiveWarning != "" {
		missing = append(missing, archiveWarning)
	}
	readToken := strings.TrimSpace(a.gatewayToken)
	delegationToken := strings.TrimSpace(a.gatewayDelegationToken)
	distinctGatewayTokens := readToken == "" || delegationToken == "" || readToken != delegationToken
	delegationConfigured := a.gatewayDelegationEnabled && a.gatewayEnabled && delegationToken != "" && distinctGatewayTokens && a.gatewayReceiptStore != nil
	gatewayReason := ""
	if a.gatewayEnabled {
		gatewayReason = a.validGatewayURL()
	}
	if !delegationConfigured {
		missing = append(missing, openClawCLIExecutionBlockedReason)
	}
	if gatewayReason != "" {
		missing = append(missing, gatewayReason)
	}
	if a.gatewayDelegationEnabled && !a.gatewayEnabled {
		missing = append(missing, "OPENCLAW_GATEWAY_ENABLED")
	}
	if a.gatewayDelegationEnabled && strings.TrimSpace(a.gatewayDelegationToken) == "" {
		missing = append(missing, "OPENCLAW_GATEWAY_DELEGATION_TOKEN")
	}
	if a.gatewayDelegationEnabled && !distinctGatewayTokens {
		missing = append(missing, "OPENCLAW_GATEWAY_DELEGATION_TOKEN must differ from OPENCLAW_GATEWAY_TOKEN")
	}
	if a.gatewayDelegationEnabled && strings.TrimSpace(a.gatewayURL) == "" {
		missing = append(missing, "OPENCLAW_GATEWAY_URL")
	}
	if a.gatewayDelegationEnabled && a.gatewayReceiptStore == nil {
		missing = append(missing, "OPENCLAW_GATEWAY_RECEIPT_STORE")
	}
	if a.gatewayDelegationEnabled {
		missing = append(missing, gatewayPolicyAttestationBlockReason)
	}
	if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
		missing = append(missing, blocked...)
	}
	configured := delegationConfigured && len(missing) == 0 && gatewayReason == ""
	return Info{
		ID:                         "openclaw",
		Name:                       "OpenClaw Gateway Agent",
		Type:                       "openclaw",
		Enabled:                    a.enabled,
		Configured:                 configured,
		ExecutionEnabled:           a.enabled && configured,
		RequiresApproval:           true,
		ReadOnlyDefault:            true,
		Capabilities:               a.capabilities(),
		Architecture:               a.architecture(),
		Controls:                   a.controls(),
		Ecosystem:                  a.ecosystem(),
		EcosystemPath:              a.ecosystemPath,
		EcosystemRollbackAvailable: archiveRollbackAvailable,
		MissingConfiguration:       missing,
		Endpoint:                   safety.RedactURL(firstNonEmpty(a.gatewayURL, a.executable)),
	}
}

// OpenClawGatewayDelegationReady reports executable policy readiness, not a
// health probe result. Configuration and Gateway liveness alone do not enable
// new sessions without run-bound effective sandbox-policy evidence.
func (a *openClawAdapter) OpenClawGatewayDelegationReady() bool {
	if a == nil || !a.gatewayDelegationEnabled {
		return false
	}
	info := a.Info()
	return info.Enabled && info.Configured && info.ExecutionEnabled
}

func (a *openClawAdapter) HealthCheck(ctx context.Context) Health {
	started := time.Now()
	health := Health{RuntimeID: "openclaw", Status: "disabled", CheckedAt: time.Now().UTC()}
	if !a.enabled {
		health.Reason = "OPENCLAW_AGENT_ENABLED is false"
		return health
	}
	if a.gatewayDelegationEnabled {
		readToken := strings.TrimSpace(a.gatewayToken)
		if readToken != "" && readToken == strings.TrimSpace(a.gatewayDelegationToken) {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_DELEGATION_TOKEN must differ from OPENCLAW_GATEWAY_TOKEN"
			return health
		}
	}
	if reason := a.validGatewayURL(); reason != "" {
		health.Status = "blocked"
		health.Reason = reason
		return health
	}
	if a.gatewayDelegationEnabled {
		if reason := a.gatewayDelegationBlockedReason(); reason != "" {
			health.Status = "blocked"
			health.Reason = reason
			return health
		}
		if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
			health.Status = "blocked"
			health.Reason = strings.Join(blocked, "; ")
			return health
		}
		health.Status = "blocked"
		health.Reason = gatewayPolicyAttestationBlockReason
		return health
	}
	if a.gatewayEnabled {
		if strings.TrimSpace(a.gatewayURL) == "" {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_URL is required when OPENCLAW_GATEWAY_ENABLED=true"
			return health
		}
		if a.gatewayTaskLedgerDiscoveryEnabled && !a.gatewayAuthenticatedDiscoveryEnabled {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true"
			return health
		}
		if a.gatewayCapabilityDiscoveryEnabled && !a.gatewayAuthenticatedDiscoveryEnabled {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true"
			return health
		}
		if a.gatewayModelCatalogDiscoveryEnabled && !a.gatewayAuthenticatedDiscoveryEnabled {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true"
			return health
		}
		if a.gatewayAgentRosterDiscoveryEnabled && !a.gatewayAuthenticatedDiscoveryEnabled {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true"
			return health
		}
		if a.gatewayTaskLedgerDiscoveryEnabled && strings.TrimSpace(a.gatewayToken) == "" {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_TOKEN"
			return health
		}
		if a.gatewayCapabilityDiscoveryEnabled && strings.TrimSpace(a.gatewayToken) == "" {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_TOKEN"
			return health
		}
		if a.gatewayModelCatalogDiscoveryEnabled && strings.TrimSpace(a.gatewayToken) == "" {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_TOKEN"
			return health
		}
		if a.gatewayAgentRosterDiscoveryEnabled && strings.TrimSpace(a.gatewayToken) == "" {
			health.Status = "blocked"
			health.Reason = "OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED requires OPENCLAW_GATEWAY_TOKEN"
			return health
		}
		gatewayHealth := a.gatewayHealthCheck(ctx, started)
		if !a.gatewayDelegationEnabled {
			gatewayHealth.Reason = strings.TrimSpace(gatewayHealth.Reason + "; connectivity only; no HAI delegated-session route is configured, and direct CLI task execution is blocked because effective run-bound policy cannot be verified")
		}
		return gatewayHealth
	}
	health.Status = "blocked"
	health.Reason = openClawCLIExecutionBlockedReason
	return health
}

func (a *openClawAdapter) gatewayHealthCheck(ctx context.Context, started time.Time) Health {
	health := Health{RuntimeID: "openclaw", Status: "unavailable", CheckedAt: time.Now().UTC()}
	endpoint, err := openClawGatewayHealthURL(a.gatewayURL)
	if err != nil {
		health.Status = "blocked"
		health.Reason = "OpenClaw Gateway health endpoint is invalid"
		return health
	}
	health.GatewayEndpointSHA256 = openClawGatewayEndpointDigest(a.gatewayURL)
	endpointURL, err := url.Parse(endpoint)
	if err != nil || endpointURL.Hostname() == "" {
		health.Status = "blocked"
		health.Reason = "OpenClaw Gateway health endpoint is invalid"
		return health
	}
	timeout := a.timeout
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		health.Status = "blocked"
		health.Reason = "OpenClaw Gateway health request could not be created"
		return health
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" && network != "tcp4" && network != "tcp6" {
					return nil, fmt.Errorf("unsupported OpenClaw Gateway health transport")
				}
				dialHost, dialPort, err := net.SplitHostPort(address)
				if err != nil || !strings.EqualFold(strings.TrimSuffix(dialHost, "."), strings.TrimSuffix(endpointURL.Hostname(), ".")) || !a.gatewayDialTargetMatchesConfiguredURL(dialHost, dialPort) {
					return nil, fmt.Errorf("OpenClaw Gateway health target changed")
				}
				return a.dialOpenClawGatewayHostPort(ctx, dialHost, dialPort, timeout)
			},
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		health.Reason = "OpenClaw Gateway health endpoint is unavailable"
		return health
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		health.Reason = "OpenClaw Gateway health endpoint returned an unexpected status"
		return health
	}
	var payload struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4*1024)).Decode(&payload); err != nil || !payload.OK || strings.ToLower(strings.TrimSpace(payload.Status)) != "live" {
		health.Reason = "OpenClaw Gateway health endpoint returned an unexpected health response"
		return health
	}
	health.Status = "available"
	if a.gatewayAuthenticatedDiscoveryEnabled {
		if strings.TrimSpace(a.gatewayToken) == "" {
			health.Reason = "OpenClaw Companion gateway health endpoint is live; authenticated operator.read discovery was skipped because OPENCLAW_GATEWAY_TOKEN is not configured"
		} else if evidence, err := a.gatewayAuthenticatedOperatorReadDiscovery(ctx); err != nil {
			health.Status = "unavailable"
			health.Reason = "OpenClaw Gateway authenticated operator.read discovery was unavailable, malformed, or over-scoped"
			return health
		} else {
			health.Version = evidence.Version
			health.GatewayProtocolValidated = true
			health.GatewayAuthenticated = true
			health.GatewayScope = "operator.read"
			health.GatewayEvidenceSchema = "openclaw-gateway-protocol-v4"
			health.GatewayTaskLedger = evidence.TaskLedger
			health.GatewayCapabilityCatalog = evidence.CapabilityCatalog
			health.GatewayPreparedModelCatalog = evidence.PreparedModelCatalog
			health.GatewayAgentRoster = evidence.AgentRoster
			if evidence.TaskLedger != nil {
				health.Reason = "OpenClaw Companion gateway health endpoint is live and authenticated operator.read discovery verified a bounded task-ledger summary; HAI closes the connection without enabling task execution"
			} else if evidence.CapabilityCatalog != nil {
				health.Reason = "OpenClaw Companion gateway health endpoint is live and authenticated operator.read discovery verified bounded skill and tool aggregate metadata; HAI closes the connection without enabling task execution"
			} else if evidence.PreparedModelCatalog != nil {
				health.Reason = "OpenClaw Companion gateway health endpoint is live and authenticated operator.read discovery verified a bounded prepared-model availability summary; HAI does not select, refresh, or update models"
			} else if evidence.AgentRoster != nil {
				health.Reason = "OpenClaw Companion gateway health endpoint is live and authenticated operator.read discovery verified a bounded agent-roster summary; HAI closes the connection without enabling task execution"
			} else {
				health.Reason = "OpenClaw Companion gateway health endpoint is live and authenticated operator.read discovery was verified; HAI closes the connection without calling Gateway RPCs or enabling task execution"
			}
		}
	} else if a.gatewayProtocolDiscoveryEnabled {
		if err := a.gatewayProtocolChallengeCheck(ctx); err != nil {
			health.Status = "unavailable"
			health.Reason = "OpenClaw Gateway protocol challenge was unavailable or malformed"
			return health
		}
		health.GatewayProtocolValidated = true
		health.GatewayEvidenceSchema = "openclaw-gateway-protocol-v4"
		health.Reason = "OpenClaw Companion Gateway health endpoint is live and its protocol challenge was verified; read-only discovery does not attest task policy or authorize a HAI execution route"
	} else {
		health.Reason = "OpenClaw Companion Gateway health endpoint is live; read-only discovery does not attest task policy or authorize a HAI execution route"
	}
	health.LatencyMs = time.Since(started).Milliseconds()
	return health
}

// openClawGatewayEndpointDigest returns a non-reversible identifier for the
// configured endpoint. Discovery evidence keeps this digest, never the URL.
func openClawGatewayEndpointDigest(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	sum := sha256.Sum256([]byte(parsed.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (a *openClawAdapter) gatewayProtocolChallengeCheck(ctx context.Context) error {
	connection, err := a.openClawGatewayPreAuthConnection(ctx)
	if err != nil {
		return err
	}
	return connection.Close()
}

type openClawGatewayReadOnlyEvidence struct {
	Version              string
	TaskLedger           *GatewayTaskLedgerSummary
	CapabilityCatalog    *GatewayCapabilityCatalogSummary
	PreparedModelCatalog *GatewayPreparedModelCatalogSummary
	AgentRoster          *GatewayAgentRosterSummary
}

func (a *openClawAdapter) gatewayAuthenticatedOperatorReadDiscovery(ctx context.Context) (openClawGatewayReadOnlyEvidence, error) {
	connection, evidence, methods, err := a.openClawGatewayAuthenticatedReadConnection(ctx)
	if err != nil {
		return openClawGatewayReadOnlyEvidence{}, err
	}
	defer connection.Close()
	if a.gatewayTaskLedgerDiscoveryEnabled {
		if !containsExact(methods, "tasks.list") {
			return openClawGatewayReadOnlyEvidence{}, fmt.Errorf("gateway does not advertise read-only tasks.list")
		}
		ledgerPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "tasks.list", map[string]any{"limit": openClawGatewayTaskLedgerLimit})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		ledger, err := openClawGatewayTaskLedgerFromResponse(ledgerPayload)
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		evidence.TaskLedger = &ledger
	}
	if a.gatewayCapabilityDiscoveryEnabled {
		if !containsExact(methods, "skills.status") || !containsExact(methods, "tools.catalog") || !containsExact(methods, "commands.list") {
			return openClawGatewayReadOnlyEvidence{}, fmt.Errorf("gateway does not advertise the required read-only capability methods")
		}
		skillsPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "skills.status", map[string]any{})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		toolsPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "tools.catalog", map[string]any{})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		commandsPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "commands.list", map[string]any{"includeArgs": false})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		catalog, err := openClawGatewayCapabilityCatalogFromResponses(skillsPayload, toolsPayload, commandsPayload)
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		evidence.CapabilityCatalog = &catalog
	}
	if a.gatewayModelCatalogDiscoveryEnabled {
		if !containsExact(methods, "models.list") {
			return openClawGatewayReadOnlyEvidence{}, fmt.Errorf("gateway does not advertise read-only models.list")
		}
		modelsPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "models.list", map[string]any{"view": "configured", "preparedOnly": true})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		catalog, err := openClawGatewayPreparedModelCatalogFromResponse(modelsPayload)
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		evidence.PreparedModelCatalog = &catalog
	}
	if a.gatewayAgentRosterDiscoveryEnabled {
		if !containsExact(methods, "agents.list") {
			return openClawGatewayReadOnlyEvidence{}, fmt.Errorf("gateway does not advertise read-only agents.list")
		}
		rosterPayload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "agents.list", map[string]any{})
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		roster, err := openClawGatewayAgentRosterFromResponse(rosterPayload)
		if err != nil {
			return openClawGatewayReadOnlyEvidence{}, err
		}
		evidence.AgentRoster = &roster
	}
	return evidence, nil
}

// openClawGatewayAuthenticatedReadConnection retries exactly one initial
// read-only handshake when the Gateway explicitly reports a bounded,
// retryable UNAVAILABLE response. A new socket is used because Gateway
// startup failures may close the first connection. No Gateway RPC, task, or
// mutation is repeated by this retry.
func (a *openClawAdapter) openClawGatewayAuthenticatedReadConnection(ctx context.Context) (*websocket.Conn, openClawGatewayReadOnlyEvidence, []string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		connection, err := a.openClawGatewayPreAuthConnection(ctx)
		if err != nil {
			return nil, openClawGatewayReadOnlyEvidence{}, nil, err
		}
		evidence, methods, err := a.openClawGatewayAuthenticatedReadHandshake(ctx, connection)
		if err == nil {
			return connection, evidence, methods, nil
		}
		_ = connection.Close()

		var gatewayErr *openClawGatewayResponseError
		if attempt != 0 || !errors.As(err, &gatewayErr) || !gatewayErr.Retryable || gatewayErr.RetryAfter <= 0 {
			return nil, openClawGatewayReadOnlyEvidence{}, nil, err
		}
		timer := time.NewTimer(gatewayErr.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, openClawGatewayReadOnlyEvidence{}, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("OpenClaw Gateway read-only handshake retry exhausted")
}

func (a *openClawAdapter) openClawGatewayAuthenticatedReadHandshake(ctx context.Context, connection *websocket.Conn) (openClawGatewayReadOnlyEvidence, []string, error) {

	requestID := uuid.NewString()
	request := map[string]any{
		"type":   "req",
		"id":     requestID,
		"method": "connect",
		"params": map[string]any{
			"minProtocol": openClawGatewayProtocolVersion,
			"maxProtocol": openClawGatewayProtocolVersion,
			"client": map[string]any{
				"id":       "gateway-client",
				"version":  "hai-openclaw-discovery/1",
				"platform": "linux",
				"mode":     "backend",
			},
			"role":        "operator",
			"scopes":      []string{"operator.read"},
			"caps":        a.gatewayReadOnlyCapabilities(),
			"commands":    []string{},
			"permissions": map[string]bool{},
			"auth":        map[string]string{"token": a.gatewayToken},
			"locale":      "en-US",
			"userAgent":   "hai-openclaw-discovery/1",
		},
	}
	if err := websocket.JSON.Send(connection, request); err != nil {
		return openClawGatewayReadOnlyEvidence{}, nil, err
	}

	var response struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error struct {
			Code         string      `json:"code"`
			Retryable    bool        `json:"retryable"`
			RetryAfterMs json.Number `json:"retryAfterMs"`
		} `json:"error"`
		Payload struct {
			Type     string      `json:"type"`
			Protocol json.Number `json:"protocol"`
			Server   struct {
				Version string `json:"version"`
				ConnID  string `json:"connId"`
			} `json:"server"`
			Features json.RawMessage `json:"features"`
			Snapshot json.RawMessage `json:"snapshot"`
			Auth     struct {
				Role   string   `json:"role"`
				Scopes []string `json:"scopes"`
			} `json:"auth"`
			Policy struct {
				MaxPayload       json.Number `json:"maxPayload"`
				MaxBufferedBytes json.Number `json:"maxBufferedBytes"`
			} `json:"policy"`
		} `json:"payload"`
	}
	if err := receiveOpenClawGatewayJSONContext(ctx, connection, &response); err != nil {
		return openClawGatewayReadOnlyEvidence{}, nil, err
	}
	if response.Type != "res" || response.ID != requestID {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("unexpected authenticated gateway response")
	}
	if !response.OK {
		return openClawGatewayReadOnlyEvidence{}, nil, openClawGatewayResponseErrorFromPayload(response.Error)
	}
	if response.Payload.Type != "hello-ok" {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("unexpected authenticated gateway response")
	}
	protocol, err := response.Payload.Protocol.Int64()
	serverVersion := strings.TrimSpace(response.Payload.Server.Version)
	if err != nil || protocol != openClawGatewayProtocolVersion || !validGatewayEvidenceValue(serverVersion) || !validGatewayEvidenceValue(strings.TrimSpace(response.Payload.Server.ConnID)) || !presentGatewayJSON(response.Payload.Features) || !presentGatewayJSON(response.Payload.Snapshot) {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("invalid authenticated gateway hello response")
	}
	maxPayload, err := response.Payload.Policy.MaxPayload.Int64()
	if err != nil || maxPayload <= 0 {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("invalid authenticated gateway payload policy")
	}
	maxBufferedBytes, err := response.Payload.Policy.MaxBufferedBytes.Int64()
	if err != nil || maxBufferedBytes <= 0 {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("invalid authenticated gateway buffer policy")
	}
	if response.Payload.Auth.Role != "operator" || len(response.Payload.Auth.Scopes) != 1 || response.Payload.Auth.Scopes[0] != "operator.read" {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("authenticated gateway negotiated unexpected operator scope")
	}
	evidence := openClawGatewayReadOnlyEvidence{Version: serverVersion}
	var features struct {
		Methods []string `json:"methods"`
	}
	if err := json.Unmarshal(response.Payload.Features, &features); err != nil {
		return openClawGatewayReadOnlyEvidence{}, nil, fmt.Errorf("invalid authenticated gateway feature inventory")
	}
	return evidence, append([]string(nil), features.Methods...), nil
}

func (a *openClawAdapter) gatewayReadOnlyCapabilities() []string {
	if a.gatewayAgentRosterDiscoveryEnabled {
		return []string{"agent-kind"}
	}
	return []string{}
}

type openClawGatewayErrorCategory string

const (
	openClawGatewayErrorUnavailable  openClawGatewayErrorCategory = "unavailable"
	openClawGatewayErrorAccessDenied openClawGatewayErrorCategory = "access_denied"
	openClawGatewayErrorInvalid      openClawGatewayErrorCategory = "invalid"
	openClawGatewayErrorRejected     openClawGatewayErrorCategory = "rejected"
)

// openClawGatewayResponseError retains only a stable, non-sensitive category.
// Gateway error messages and details are provider-controlled and can contain
// credentials, paths, or task material, so they must not cross into HAI logs,
// launch events, or caller-visible errors.
type openClawGatewayResponseError struct {
	Category   openClawGatewayErrorCategory
	Retryable  bool
	RetryAfter time.Duration
}

func (e *openClawGatewayResponseError) Error() string {
	if e == nil {
		return "OpenClaw Gateway request was rejected"
	}
	switch e.Category {
	case openClawGatewayErrorUnavailable:
		return "OpenClaw Gateway is temporarily unavailable"
	case openClawGatewayErrorAccessDenied:
		return "OpenClaw Gateway denied the requested capability"
	case openClawGatewayErrorInvalid:
		return "OpenClaw Gateway rejected the request as invalid"
	default:
		return "OpenClaw Gateway request was rejected"
	}
}

type openClawGatewayRPCResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload"`
	Error   struct {
		Code         string      `json:"code"`
		Retryable    bool        `json:"retryable"`
		RetryAfterMs json.Number `json:"retryAfterMs"`
	} `json:"error"`
}

var openClawGatewayReadOnlyMethods = map[string]struct{}{
	"sessions.usage":     {},
	"agents.list":        {},
	"artifacts.list":     {},
	"artifacts.download": {},
	"commands.list":      {},
	"models.list":        {},
	"skills.status":      {},
	"tasks.list":         {},
	"tools.catalog":      {},
}

var openClawGatewayWriteScopedMethods = map[string]struct{}{
	"agent.wait":      {},
	"sessions.abort":  {},
	"sessions.create": {},
}

func openClawGatewayMethodAllowed(method string, allowed map[string]struct{}) bool {
	_, ok := allowed[strings.TrimSpace(method)]
	return ok
}

func openClawGatewayResponsePayload(response openClawGatewayRPCResponse, requestID string) (json.RawMessage, error) {
	if response.Type != "res" || response.ID != requestID {
		return nil, fmt.Errorf("unexpected OpenClaw Gateway response frame")
	}
	if !response.OK {
		return nil, openClawGatewayResponseErrorFromPayload(response.Error)
	}
	if !presentGatewayJSON(response.Payload) {
		return nil, fmt.Errorf("OpenClaw Gateway response payload is missing")
	}
	return response.Payload, nil
}

func openClawGatewayResponseErrorFromPayload(payload struct {
	Code         string      `json:"code"`
	Retryable    bool        `json:"retryable"`
	RetryAfterMs json.Number `json:"retryAfterMs"`
}) *openClawGatewayResponseError {
	category := openClawGatewayErrorRejected
	switch strings.ToUpper(strings.TrimSpace(payload.Code)) {
	case "UNAVAILABLE":
		category = openClawGatewayErrorUnavailable
	case "FORBIDDEN", "UNAUTHENTICATED":
		category = openClawGatewayErrorAccessDenied
	case "INVALID_REQUEST", "VALIDATION_FAILED", "BAD_REQUEST":
		category = openClawGatewayErrorInvalid
	}
	retryAfter := time.Duration(0)
	if milliseconds, err := payload.RetryAfterMs.Int64(); err == nil && milliseconds > 0 && milliseconds <= int64((30*time.Second)/time.Millisecond) {
		retryAfter = time.Duration(milliseconds) * time.Millisecond
	}
	// HAI never automatically retries a Gateway mutation. Read-only callers can
	// later use this bounded signal within their own operation budget.
	retryable := category == openClawGatewayErrorUnavailable && payload.Retryable && retryAfter > 0
	return &openClawGatewayResponseError{Category: category, Retryable: retryable, RetryAfter: retryAfter}
}

func openClawGatewayReadOnlyRequest(connection *websocket.Conn, method string, params map[string]any) (json.RawMessage, error) {
	return openClawGatewayReadOnlyRequestContext(context.Background(), connection, method, params)
}

func openClawGatewayReadOnlyRequestContext(ctx context.Context, connection *websocket.Conn, method string, params map[string]any) (json.RawMessage, error) {
	if !openClawGatewayMethodAllowed(method, openClawGatewayReadOnlyMethods) {
		return nil, fmt.Errorf("OpenClaw Gateway method is not allowlisted for a read-only connection")
	}
	requestID := uuid.NewString()
	request := map[string]any{"type": "req", "id": requestID, "method": method, "params": params}
	if err := sendOpenClawGatewayJSONContext(ctx, connection, request); err != nil {
		return nil, err
	}
	return receiveOpenClawGatewayRPCContext(ctx, connection, requestID)
}

// openClawGatewayReadOnlyRequestWithRetry retries once only when the Gateway
// explicitly reports a bounded, retryable UNAVAILABLE error. It is reserved
// for metadata discovery and never used for session creation, cancellation, or
// terminal reconciliation, where repeating a request could create ambiguity.
func openClawGatewayReadOnlyRequestWithRetry(ctx context.Context, connection *websocket.Conn, method string, params map[string]any) (json.RawMessage, error) {
	payload, err := openClawGatewayReadOnlyRequestContext(ctx, connection, method, params)
	if err == nil {
		return payload, nil
	}
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) || !gatewayErr.Retryable || gatewayErr.RetryAfter <= 0 {
		return nil, err
	}
	timer := time.NewTimer(gatewayErr.RetryAfter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return openClawGatewayReadOnlyRequestContext(ctx, connection, method, params)
}

func (a *openClawAdapter) openClawGatewayOperatorReadHandshake(connection *websocket.Conn) ([]string, error) {
	return a.openClawGatewayOperatorReadHandshakeContext(context.Background(), connection)
}

func (a *openClawAdapter) openClawGatewayOperatorReadHandshakeContext(ctx context.Context, connection *websocket.Conn) ([]string, error) {
	if strings.TrimSpace(a.gatewayToken) == "" {
		return nil, fmt.Errorf("OpenClaw Gateway read credential is unavailable")
	}
	requestID := uuid.NewString()
	request := map[string]any{
		"type": "req", "id": requestID, "method": "connect",
		"params": map[string]any{
			"minProtocol": openClawGatewayProtocolVersion, "maxProtocol": openClawGatewayProtocolVersion,
			"client": map[string]any{"id": "gateway-client", "version": "hai-openclaw-artifact-import/1", "platform": "linux", "mode": "backend"},
			"role":   "operator", "scopes": []string{"operator.read"}, "caps": []string{}, "commands": []string{}, "permissions": map[string]bool{},
			"auth": map[string]string{"token": a.gatewayToken}, "locale": "en-US", "userAgent": "hai-openclaw-artifact-import/1",
		},
	}
	if err := sendOpenClawGatewayJSONContext(ctx, connection, request); err != nil {
		return nil, err
	}
	var response struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error struct {
			Code         string      `json:"code"`
			Retryable    bool        `json:"retryable"`
			RetryAfterMs json.Number `json:"retryAfterMs"`
		} `json:"error"`
		Payload struct {
			Type     string      `json:"type"`
			Protocol json.Number `json:"protocol"`
			Server   struct {
				Version string `json:"version"`
				ConnID  string `json:"connId"`
			} `json:"server"`
			Features struct {
				Methods []string `json:"methods"`
			} `json:"features"`
			Snapshot json.RawMessage `json:"snapshot"`
			Auth     struct {
				Role   string   `json:"role"`
				Scopes []string `json:"scopes"`
			} `json:"auth"`
			Policy struct {
				MaxPayload       json.Number `json:"maxPayload"`
				MaxBufferedBytes json.Number `json:"maxBufferedBytes"`
			} `json:"policy"`
		} `json:"payload"`
	}
	if err := receiveOpenClawGatewayJSONContext(ctx, connection, &response); err != nil {
		return nil, err
	}
	if response.Type != "res" || response.ID != requestID {
		return nil, fmt.Errorf("unexpected OpenClaw Gateway read-only handshake")
	}
	if !response.OK {
		return nil, openClawGatewayResponseErrorFromPayload(response.Error)
	}
	protocol, err := response.Payload.Protocol.Int64()
	if err != nil || response.Payload.Type != "hello-ok" ||
		protocol != openClawGatewayProtocolVersion || !validGatewayEvidenceValue(strings.TrimSpace(response.Payload.Server.Version)) ||
		!validGatewayEvidenceValue(strings.TrimSpace(response.Payload.Server.ConnID)) || !presentGatewayJSON(response.Payload.Snapshot) ||
		response.Payload.Auth.Role != "operator" || len(response.Payload.Auth.Scopes) != 1 || response.Payload.Auth.Scopes[0] != "operator.read" {
		return nil, fmt.Errorf("unexpected OpenClaw Gateway read-only handshake")
	}
	maxPayload, maxPayloadErr := response.Payload.Policy.MaxPayload.Int64()
	maxBufferedBytes, maxBufferErr := response.Payload.Policy.MaxBufferedBytes.Int64()
	if maxPayloadErr != nil || maxBufferErr != nil || maxPayload <= 0 || maxBufferedBytes <= 0 || len(response.Payload.Features.Methods) > openClawGatewayCapabilityLimit {
		return nil, fmt.Errorf("invalid OpenClaw Gateway read-only handshake policy")
	}
	return append([]string(nil), response.Payload.Features.Methods...), nil
}

// openClawGatewayOperatorReadConnection retries exactly one initial
// operator.read handshake when the Gateway reports a bounded, retryable
// UNAVAILABLE response. The retry always uses a new socket and happens before
// any metadata RPC, so artifacts.list is never replayed by this helper.
func (a *openClawAdapter) openClawGatewayOperatorReadConnection(ctx context.Context) (*websocket.Conn, []string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		connection, err := a.openClawGatewayPreAuthConnection(ctx)
		if err != nil {
			return nil, nil, err
		}
		methods, err := a.openClawGatewayOperatorReadHandshakeContext(ctx, connection)
		if err == nil {
			return connection, methods, nil
		}
		_ = connection.Close()

		var gatewayErr *openClawGatewayResponseError
		if attempt != 0 || !errors.As(err, &gatewayErr) || !gatewayErr.Retryable || gatewayErr.RetryAfter <= 0 {
			return nil, nil, err
		}
		timer := time.NewTimer(gatewayErr.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, fmt.Errorf("OpenClaw Gateway operator.read handshake retry exhausted")
}

func openClawGatewayCapabilityCatalogFromResponses(skillsPayload, toolsPayload, commandsPayload json.RawMessage) (GatewayCapabilityCatalogSummary, error) {
	var skillsResponse struct {
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(skillsPayload, &skillsResponse); err != nil || !presentGatewayJSON(skillsResponse.Skills) {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid read-only skills status payload")
	}
	var skills []struct {
		Eligible bool `json:"eligible"`
	}
	if err := json.Unmarshal(skillsResponse.Skills, &skills); err != nil || len(skills) > openClawGatewayCapabilityLimit {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid bounded read-only skills status")
	}
	summary := GatewayCapabilityCatalogSummary{SampledSkills: len(skills), ToolCountsBySource: map[string]int{}}
	for _, skill := range skills {
		if skill.Eligible {
			summary.EligibleSkills++
		}
	}

	var toolsResponse struct {
		Groups json.RawMessage `json:"groups"`
	}
	if err := json.Unmarshal(toolsPayload, &toolsResponse); err != nil || !presentGatewayJSON(toolsResponse.Groups) {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid read-only tools catalog")
	}
	var groups []struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(toolsResponse.Groups, &groups); err != nil || len(groups) > openClawGatewayCapabilityLimit {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid bounded read-only tools catalog")
	}
	totalTools := 0
	for _, group := range groups {
		if !presentGatewayJSON(group.Tools) {
			return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid read-only tool group")
		}
		var tools []struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(group.Tools, &tools); err != nil || len(tools) > openClawGatewayCapabilityLimit || totalTools+len(tools) > openClawGatewayCapabilityLimit {
			return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid bounded read-only tool catalog")
		}
		totalTools += len(tools)
		for _, tool := range tools {
			source := strings.TrimSpace(tool.Source)
			if source != "core" && source != "plugin" {
				return GatewayCapabilityCatalogSummary{}, fmt.Errorf("unrecognized read-only tool provenance")
			}
			summary.ToolCountsBySource[source]++
		}
	}

	var commandsResponse struct {
		Commands json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal(commandsPayload, &commandsResponse); err != nil || !presentGatewayJSON(commandsResponse.Commands) {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid read-only commands catalog")
	}
	var commands []json.RawMessage
	if err := json.Unmarshal(commandsResponse.Commands, &commands); err != nil || len(commands) > openClawGatewayCapabilityLimit {
		return GatewayCapabilityCatalogSummary{}, fmt.Errorf("invalid bounded read-only commands catalog")
	}
	summary.SampledCommands = len(commands)
	return summary, nil
}

func openClawGatewayPreparedModelCatalogFromResponse(payload json.RawMessage) (GatewayPreparedModelCatalogSummary, error) {
	var response struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(payload, &response); err != nil || !presentGatewayJSON(response.Models) {
		return GatewayPreparedModelCatalogSummary{}, fmt.Errorf("invalid read-only prepared model catalog")
	}
	var models []struct {
		Available *bool `json:"available"`
	}
	if err := json.Unmarshal(response.Models, &models); err != nil || len(models) > openClawGatewayCapabilityLimit {
		return GatewayPreparedModelCatalogSummary{}, fmt.Errorf("invalid bounded read-only prepared model catalog")
	}
	summary := GatewayPreparedModelCatalogSummary{SampledModels: len(models)}
	for _, model := range models {
		switch {
		case model.Available == nil:
			summary.UnknownAvailabilityModels++
		case *model.Available:
			summary.AvailableModels++
		default:
			summary.UnavailableModels++
		}
	}
	return summary, nil
}

func openClawGatewayAgentRosterFromResponse(payload json.RawMessage) (GatewayAgentRosterSummary, error) {
	var response struct {
		Agents json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(payload, &response); err != nil || !presentGatewayJSON(response.Agents) {
		return GatewayAgentRosterSummary{}, fmt.Errorf("invalid read-only agent roster payload")
	}
	var agents []struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(response.Agents, &agents); err != nil || len(agents) > openClawGatewayCapabilityLimit {
		return GatewayAgentRosterSummary{}, fmt.Errorf("invalid bounded read-only agent roster")
	}
	summary := GatewayAgentRosterSummary{SampledAgents: len(agents)}
	for _, agent := range agents {
		if agent.Kind == nil {
			summary.UnknownKindCount++
			continue
		}
		switch strings.TrimSpace(*agent.Kind) {
		case "agent":
			summary.AgentCount++
		case "system":
			summary.SystemCount++
		default:
			return GatewayAgentRosterSummary{}, fmt.Errorf("invalid read-only agent kind")
		}
	}
	return summary, nil
}

func openClawGatewayTaskLedgerFromResponse(payload json.RawMessage) (GatewayTaskLedgerSummary, error) {
	var response struct {
		Tasks      json.RawMessage `json:"tasks"`
		NextCursor *string         `json:"nextCursor"`
	}
	if err := json.Unmarshal(payload, &response); err != nil || !presentGatewayJSON(response.Tasks) {
		return GatewayTaskLedgerSummary{}, fmt.Errorf("invalid read-only task ledger payload")
	}
	var tasks []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Tasks, &tasks); err != nil || len(tasks) > openClawGatewayTaskLedgerLimit {
		return GatewayTaskLedgerSummary{}, fmt.Errorf("invalid bounded read-only task ledger")
	}
	summary := GatewayTaskLedgerSummary{SampledTasks: len(tasks), StatusCounts: map[string]int{}}
	for _, task := range tasks {
		status := strings.TrimSpace(task.Status)
		if !validOpenClawGatewayTaskStatus(status) {
			return GatewayTaskLedgerSummary{}, fmt.Errorf("invalid read-only task status")
		}
		summary.StatusCounts[status]++
	}
	if response.NextCursor != nil && strings.TrimSpace(*response.NextCursor) != "" {
		summary.Truncated = true
	}
	return summary, nil
}

func openClawGatewayArtifactDescriptorsFromResponse(payload json.RawMessage, expectedRunID string) ([]GatewayArtifactDescriptor, error) {
	expectedRunID = strings.TrimSpace(expectedRunID)
	if !validOpenClawGatewayRunID(expectedRunID) {
		return nil, fmt.Errorf("invalid expected Gateway run identifier")
	}
	var response struct {
		Artifacts []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Type      string `json:"type"`
			MIMEType  string `json:"mimeType"`
			SizeBytes *int64 `json:"sizeBytes"`
			RunID     string `json:"runId"`
			Download  struct {
				Mode string `json:"mode"`
			} `json:"download"`
		} `json:"artifacts"`
	}
	if len(payload) > defaultOutputLimit || !utf8.Valid(payload) {
		return nil, fmt.Errorf("invalid Gateway artifact list")
	}
	if err := json.Unmarshal(payload, &response); err != nil || response.Artifacts == nil || len(response.Artifacts) > openClawGatewayArtifactLimit {
		return nil, fmt.Errorf("invalid Gateway artifact list")
	}
	descriptors := make([]GatewayArtifactDescriptor, 0, len(response.Artifacts))
	seen := make(map[string]bool, len(response.Artifacts))
	for _, artifact := range response.Artifacts {
		id := strings.TrimSpace(artifact.ID)
		title := strings.TrimSpace(artifact.Title)
		artifactType := strings.TrimSpace(artifact.Type)
		mimeType := strings.TrimSpace(artifact.MIMEType)
		if !validGatewayEvidenceValue(id) || title == "" || len(title) > 4096 || !validGatewayEvidenceValue(artifactType) ||
			(len(mimeType) > 0 && !validGatewayEvidenceValue(mimeType)) || (artifact.SizeBytes != nil && *artifact.SizeBytes < 0) || seen[id] ||
			strings.TrimSpace(artifact.RunID) != expectedRunID || !validOpenClawGatewayArtifactDownloadMode(artifact.Download.Mode) {
			return nil, fmt.Errorf("invalid Gateway artifact descriptor")
		}
		seen[id] = true
		digest := sha256.Sum256([]byte("openclaw-artifact/v1\n" + id))
		descriptors = append(descriptors, GatewayArtifactDescriptor{
			Digest:    hex.EncodeToString(digest[:]),
			Type:      artifactType,
			MIMEType:  mimeType,
			SizeBytes: artifact.SizeBytes,
		})
	}
	return descriptors, nil
}

func validOpenClawGatewayArtifactDownloadMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "bytes", "url", "unsupported":
		return true
	default:
		return false
	}
}

func validOpenClawGatewayTaskStatus(status string) bool {
	switch status {
	case "queued", "running", "completed", "failed", "cancelled", "timed_out":
		return true
	default:
		return false
	}
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func presentGatewayJSON(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func validGatewayEvidenceValue(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func (a *openClawAdapter) openClawGatewayPreAuthConnection(ctx context.Context) (*websocket.Conn, error) {
	if reason := a.validGatewayURL(); reason != "" {
		return nil, errors.New(reason)
	}
	endpoint, err := openClawGatewayProtocolURL(a.gatewayURL)
	if err != nil {
		return nil, err
	}
	config, err := websocket.NewConfig(endpoint, "http://hai.local")
	if err != nil {
		return nil, err
	}
	timeout := a.timeout
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL")
	}
	client, err := a.dialOpenClawGatewayTransport(connectCtx, parsedEndpoint, timeout)
	if err != nil {
		return nil, err
	}
	if parsedEndpoint.Scheme == "wss" {
		tlsClient := tls.Client(client, &tls.Config{
			ServerName: parsedEndpoint.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsClient.HandshakeContext(connectCtx); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("OpenClaw Gateway TLS handshake failed")
		}
		client = tlsClient
	}
	if deadline, ok := connectCtx.Deadline(); ok {
		if err := client.SetDeadline(deadline); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	connection, err := openClawGatewayWebsocketClient(connectCtx, config, client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	connection.MaxPayloadBytes = 64 * 1024
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = connection.Close()
		return nil, err
	}
	var challenge struct {
		Type    string `json:"type"`
		Event   string `json:"event"`
		Payload struct {
			Nonce string      `json:"nonce"`
			TS    json.Number `json:"ts"`
		} `json:"payload"`
	}
	if err := receiveOpenClawGatewayJSONContext(ctx, connection, &challenge); err != nil {
		_ = connection.Close()
		return nil, err
	}
	if challenge.Type != "event" || challenge.Event != "connect.challenge" || strings.TrimSpace(challenge.Payload.Nonce) == "" {
		_ = connection.Close()
		return nil, fmt.Errorf("unexpected protocol challenge frame")
	}
	ts, err := challenge.Payload.TS.Int64()
	if err != nil || ts < 0 {
		_ = connection.Close()
		return nil, fmt.Errorf("invalid protocol challenge timestamp")
	}
	return connection, nil
}

func (a *openClawAdapter) dialOpenClawGatewayTransport(ctx context.Context, endpoint *url.URL, timeout time.Duration) (net.Conn, error) {
	host := endpoint.Hostname()
	if host == "" || strings.Contains(host, "%") {
		return nil, fmt.Errorf("invalid OpenClaw Gateway host")
	}
	port := endpoint.Port()
	if port == "" {
		if endpoint.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid OpenClaw Gateway port")
	}
	return a.dialOpenClawGatewayHostPort(ctx, host, port, timeout)
}

func (a *openClawAdapter) dialOpenClawGatewayHostPort(ctx context.Context, host, port string, timeout time.Duration) (net.Conn, error) {
	if host == "" || strings.Contains(host, "%") {
		return nil, fmt.Errorf("invalid OpenClaw Gateway host")
	}
	if !a.gatewayDialTargetMatchesConfiguredURL(host, port) {
		return nil, fmt.Errorf("OpenClaw Gateway dial target does not match the configured URL")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid OpenClaw Gateway port")
	}
	addresses, err := a.resolveOpenClawGatewayAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: timeout}
	var lastErr error
	for _, address := range addresses {
		connection, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("OpenClaw Gateway connection failed")
	}
	return nil, fmt.Errorf("OpenClaw Gateway has no usable network address")
}

func (a *openClawAdapter) resolveOpenClawGatewayAddresses(ctx context.Context, host string) ([]net.IP, error) {
	var addresses []net.IP
	if ip := net.ParseIP(host); ip != nil {
		addresses = []net.IP{ip}
	} else {
		lookup := a.gatewayResolver
		if lookup == nil {
			lookup = func(ctx context.Context, host string) ([]net.IP, error) {
				resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				ips := make([]net.IP, 0, len(resolved))
				for _, address := range resolved {
					ips = append(ips, address.IP)
				}
				return ips, nil
			}
		}
		resolved, err := lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("OpenClaw Gateway host could not be resolved")
		}
		addresses = resolved
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("OpenClaw Gateway host resolved to no addresses")
	}
	allowLoopback := gatewayHostAllowsLoopback(host)
	allowDockerInternal := a.hostDockerInternalGatewayOptedInForHost(host)
	validated := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if len(address) == 0 || (allowLoopback && !address.IsLoopback()) || (allowDockerInternal && !a.allowedDockerGatewayAddress(address)) || (!allowDockerInternal && blockedOpenClawGatewayAddress(address, allowLoopback)) {
			return nil, fmt.Errorf("OpenClaw Gateway endpoint resolves to blocked address space")
		}
		validated = append(validated, append(net.IP(nil), address...))
	}
	return validated, nil
}

func gatewayHostAllowsLoopback(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func blockedOpenClawGatewayAddress(address net.IP, allowLoopback bool) bool {
	if address == nil {
		return true
	}
	if ipv4 := address.To4(); ipv4 != nil {
		address = ipv4
		if ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127 {
			return true
		}
	}
	if address.IsLoopback() {
		return !allowLoopback
	}
	return address.IsUnspecified() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() || !address.IsGlobalUnicast()
}

const openClawDockerInternalHost = "host.docker.internal"

func normalizedOpenClawGatewayHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func parsedOpenClawGatewayPort(endpoint *url.URL) (int, bool) {
	if endpoint == nil {
		return 0, false
	}
	port := endpoint.Port()
	if port == "" {
		switch strings.ToLower(endpoint.Scheme) {
		case "ws", "http":
			port = "80"
		case "wss", "https":
			port = "443"
		default:
			return 0, false
		}
	}
	number, err := strconv.Atoi(port)
	return number, err == nil && number >= 1 && number <= 65535
}

// host.docker.internal is a narrow Compose host-gateway exception, not a
// general private-network allowance. Enabling the Gateway and pinning this
// exact allowlisted URL, port, and expected IPs are the operator opt-in.
func (a *openClawAdapter) hostDockerInternalGatewayOptedIn(endpoint *url.URL) bool {
	if !a.gatewayEnabled || endpoint == nil || endpoint.User != nil || a.allowedHost["*"] {
		return false
	}
	if normalizedOpenClawGatewayHost(endpoint.Hostname()) != openClawDockerInternalHost || !a.allowedHost[openClawDockerInternalHost] || endpoint.Port() == "" {
		return false
	}
	if len(a.gatewayDockerHostIPs) == 0 {
		return false
	}
	for _, pin := range a.gatewayDockerHostIPs {
		if !allowedOpenClawDockerGatewayAddress(net.ParseIP(pin)) {
			return false
		}
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "ws", "wss", "http", "https":
	default:
		return false
	}
	_, ok := parsedOpenClawGatewayPort(endpoint)
	return ok
}

func (a *openClawAdapter) hostDockerInternalGatewayOptedInForHost(host string) bool {
	if normalizedOpenClawGatewayHost(host) != openClawDockerInternalHost {
		return false
	}
	endpoint, err := url.Parse(strings.TrimSpace(a.gatewayURL))
	return err == nil && normalizedOpenClawGatewayHost(endpoint.Hostname()) == openClawDockerInternalHost && a.hostDockerInternalGatewayOptedIn(endpoint)
}

func allowedOpenClawDockerGatewayAddress(address net.IP) bool {
	return address != nil && address.IsPrivate() && !address.IsLoopback() && !address.IsUnspecified() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsMulticast() && address.IsGlobalUnicast()
}

func (a *openClawAdapter) allowedDockerGatewayAddress(address net.IP) bool {
	if !allowedOpenClawDockerGatewayAddress(address) {
		return false
	}
	for _, pin := range a.gatewayDockerHostIPs {
		if net.ParseIP(pin).Equal(address) {
			return true
		}
	}
	return false
}

func (a *openClawAdapter) gatewayDialTargetMatchesConfiguredURL(host, port string) bool {
	if a.validGatewayURL() != "" {
		return false
	}
	endpoint, err := url.Parse(strings.TrimSpace(a.gatewayURL))
	if err != nil || normalizedOpenClawGatewayHost(endpoint.Hostname()) != normalizedOpenClawGatewayHost(host) {
		return false
	}
	expectedPort, ok := parsedOpenClawGatewayPort(endpoint)
	if !ok {
		return false
	}
	actualPort, err := strconv.Atoi(port)
	return err == nil && actualPort == expectedPort
}

func openClawGatewayWebsocketClient(ctx context.Context, config *websocket.Config, client net.Conn) (*websocket.Conn, error) {
	type result struct {
		connection *websocket.Conn
		err        error
	}
	completed := make(chan result, 1)
	go func() {
		connection, err := websocket.NewClient(config, client)
		completed <- result{connection: connection, err: err}
	}()
	select {
	case result := <-completed:
		return result.connection, result.err
	case <-ctx.Done():
		_ = client.SetDeadline(time.Now())
		result := <-completed
		if result.connection != nil {
			_ = result.connection.Close()
		}
		return nil, ctx.Err()
	}
}

func openClawGatewayHealthURL(rawURL string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return "", fmt.Errorf("invalid gateway URL")
	}
	endpoint.Scheme = strings.ToLower(endpoint.Scheme)
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported gateway URL scheme")
	}
	endpoint.Path = "/health"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return endpoint.String(), nil
}

func openClawGatewayProtocolURL(rawURL string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil {
		return "", fmt.Errorf("invalid gateway URL")
	}
	endpoint.Scheme = strings.ToLower(endpoint.Scheme)
	switch endpoint.Scheme {
	case "http":
		endpoint.Scheme = "ws"
	case "https":
		endpoint.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported gateway URL scheme")
	}
	if endpoint.Path == "" {
		endpoint.Path = "/"
	}
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return endpoint.String(), nil
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
			ExecutionMode:    "approved_gateway_session_envelope",
			Source:           "OPENCLAW_ECOSYSTEM_PATH",
			Description:      "Indexed OpenClaw skill available for HAI task-envelope planning. Execution remains blocked until HAI verifies identity-bound sandbox policy for the exact Gateway run.",
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
	if !a.gatewayDelegationEnabled {
		return blockedRuntimeResult("openclaw", openClawCLIExecutionBlockedReason, "direct CLI route is not an authorized execution path")
	}
	if err := safety.ValidateRuntimeModel("openclaw", task.RuntimeModel); err != nil {
		return blockedRuntimeResult("openclaw", err.Error(), "runtime model selection rejected")
	}
	if task.RuntimeModel != "" {
		if !containsExact(a.gatewayAllowedModels, task.RuntimeModel) {
			return blockedRuntimeResult("openclaw", "runtime model is not in OPENCLAW_GATEWAY_ALLOWED_MODELS", "unapproved model selection rejected")
		}
	}
	if reason := a.gatewayDelegationBlockedReason(); reason != "" {
		return blockedRuntimeResult("openclaw", reason, "Gateway delegated-session route is not configured")
	}
	if blocked := a.highRiskExecutionBlockers(); len(blocked) > 0 {
		return blockedRuntimeResult("openclaw", strings.Join(blocked, "; "), "high-risk OpenClaw surfaces block execution")
	}
	if result, blocked := emergencyStopResult("openclaw"); blocked {
		return result
	}
	if a.maintenanceGate == nil {
		return blockedRuntimeResult("openclaw", openClawMaintenanceGateMissingReason, "OpenClaw execution rejected because maintenance admission is unavailable")
	}
	release, err := a.maintenanceGate(parent)
	if err != nil || release == nil {
		if release != nil {
			release()
		}
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: "OpenClaw maintenance must complete or be reviewed before starting a task", ExitCode: -1}
	}
	defer release()
	if result, blocked := emergencyStopResult("openclaw"); blocked {
		return result
	}
	return a.executeGatewayDelegatedTask(parent, task, started)
}

// executeGatewayDelegatedTask creates one bounded OpenClaw session for an
// already-approved HAI task. An explicit model is bound to HAI authorization.
// It never selects an upstream agent,
// workspace, worktree, attachment, or tool capability. Gateway admission is
// not completion: HAI records it as running until a separately implemented,
// source-backed reconciliation path verifies a terminal outcome.
func (a *openClawAdapter) executeGatewayDelegatedTask(_ context.Context, _ Task, started time.Time) Result {
	if reason := a.gatewayDelegationBlockedReason(); reason != "" {
		return Result{RuntimeID: "openclaw", Status: "blocked", Message: reason, ExitCode: -1, DurationMs: time.Since(started).Milliseconds()}
	}
	return Result{
		RuntimeID: "openclaw", Status: "blocked", Message: gatewayPolicyAttestationBlockReason, ExitCode: -1,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: []string{"sessions.create was not sent because HAI cannot verify a run-bound effective sandbox and tool-policy attestation"},
	}
}

const gatewayPolicyAttestationBlockReason = "OpenClaw Gateway delegated execution is disabled because HAI cannot verify a run-bound effective sandbox and tool-policy attestation"

func (a *openClawAdapter) openClawReceiptPersistenceContext() (context.Context, context.CancelFunc) {
	timeout := a.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func openClawCancellationUnresolvedResult(receipt OpenClawGatewayReceipt, started time.Time, message string, audit []string) Result {
	return Result{
		RuntimeID: "openclaw", ExecutionReference: receipt.ExecutionReference, Status: "indeterminate",
		Message: message, ExitCode: -1, DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: append(audit, "terminal status remains unverified; receipt continues to block completion and maintenance"),
	}
}

func (a *openClawAdapter) gatewayDelegationBlockedReason() string {
	if !a.gatewayDelegationEnabled {
		return "OPENCLAW_GATEWAY_DELEGATION_ENABLED is false"
	}
	if !a.gatewayEnabled {
		return "OPENCLAW_GATEWAY_ENABLED is false"
	}
	if strings.TrimSpace(a.gatewayDelegationToken) == "" {
		return "OPENCLAW_GATEWAY_DELEGATION_TOKEN is required for delegated session creation"
	}
	if readToken := strings.TrimSpace(a.gatewayToken); readToken != "" && readToken == strings.TrimSpace(a.gatewayDelegationToken) {
		return "OPENCLAW_GATEWAY_DELEGATION_TOKEN must differ from OPENCLAW_GATEWAY_TOKEN"
	}
	if strings.TrimSpace(a.gatewayURL) == "" {
		return "OPENCLAW_GATEWAY_URL is required for delegated session creation"
	}
	if a.gatewayReceiptStore == nil {
		return "OpenClaw Gateway delegated-session receipt storage is unavailable"
	}
	if reason := a.validGatewayURL(); reason != "" {
		return reason
	}
	return ""
}

func (a *openClawAdapter) openClawGatewayOperatorWriteHandshake(connection *websocket.Conn) ([]string, error) {
	return a.openClawGatewayOperatorWriteHandshakeContext(context.Background(), connection)
}

func (a *openClawAdapter) openClawGatewayOperatorWriteHandshakeContext(ctx context.Context, connection *websocket.Conn) ([]string, error) {
	requestID := uuid.NewString()
	request := map[string]any{
		"type": "req", "id": requestID, "method": "connect",
		"params": map[string]any{
			"minProtocol": openClawGatewayProtocolVersion,
			"maxProtocol": openClawGatewayProtocolVersion,
			"client": map[string]any{
				"id": "gateway-client", "version": "hai-openclaw-delegation/1", "platform": "linux", "mode": "backend",
			},
			"role": "operator", "scopes": []string{"operator.write"}, "caps": []string{}, "commands": []string{},
			"permissions": map[string]bool{}, "auth": map[string]string{"token": a.gatewayDelegationToken},
			"locale": "en-US", "userAgent": "hai-openclaw-delegation/1",
		},
	}
	if err := sendOpenClawGatewayJSONContext(ctx, connection, request); err != nil {
		return nil, err
	}
	var response struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error struct {
			Code         string      `json:"code"`
			Retryable    bool        `json:"retryable"`
			RetryAfterMs json.Number `json:"retryAfterMs"`
		} `json:"error"`
		Payload struct {
			Type     string      `json:"type"`
			Protocol json.Number `json:"protocol"`
			Server   struct {
				Version string `json:"version"`
				ConnID  string `json:"connId"`
			} `json:"server"`
			Features struct {
				Methods []string `json:"methods"`
			} `json:"features"`
			Snapshot json.RawMessage `json:"snapshot"`
			Auth     struct {
				Role   string   `json:"role"`
				Scopes []string `json:"scopes"`
			} `json:"auth"`
			Policy struct {
				MaxPayload       json.Number `json:"maxPayload"`
				MaxBufferedBytes json.Number `json:"maxBufferedBytes"`
			} `json:"policy"`
		} `json:"payload"`
	}
	if err := receiveOpenClawGatewayJSONContext(ctx, connection, &response); err != nil {
		return nil, err
	}
	if response.Type != "res" || response.ID != requestID {
		return nil, fmt.Errorf("unexpected delegated Gateway handshake")
	}
	if !response.OK {
		return nil, openClawGatewayResponseErrorFromPayload(response.Error)
	}
	protocol, err := response.Payload.Protocol.Int64()
	if err != nil || response.Payload.Type != "hello-ok" ||
		protocol != openClawGatewayProtocolVersion || response.Payload.Auth.Role != "operator" ||
		len(response.Payload.Auth.Scopes) != 1 || response.Payload.Auth.Scopes[0] != "operator.write" ||
		!validGatewayEvidenceValue(strings.TrimSpace(response.Payload.Server.Version)) ||
		!validGatewayEvidenceValue(strings.TrimSpace(response.Payload.Server.ConnID)) ||
		!presentGatewayJSON(response.Payload.Snapshot) {
		return nil, fmt.Errorf("unexpected delegated Gateway handshake")
	}
	maxPayload, maxPayloadErr := response.Payload.Policy.MaxPayload.Int64()
	maxBufferedBytes, maxBufferErr := response.Payload.Policy.MaxBufferedBytes.Int64()
	if maxPayloadErr != nil || maxBufferErr != nil || maxPayload <= 0 || maxBufferedBytes <= 0 || len(response.Payload.Features.Methods) > openClawGatewayCapabilityLimit {
		return nil, fmt.Errorf("invalid delegated Gateway handshake policy")
	}
	return sortedUnique(response.Payload.Features.Methods), nil
}

// openClawGatewayOperatorWriteConnection retries exactly one initial
// operator.write handshake when the Gateway explicitly reports a bounded,
// retryable UNAVAILABLE response. It always uses a fresh socket and returns
// before sessions.create, sessions.abort, or agent.wait can be sent, so no
// Gateway mutation or terminal observation is replayed by connection recovery.
func (a *openClawAdapter) openClawGatewayOperatorWriteConnection(ctx context.Context) (*websocket.Conn, []string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		connection, err := a.openClawGatewayPreAuthConnection(ctx)
		if err != nil {
			return nil, nil, err
		}
		methods, err := a.openClawGatewayOperatorWriteHandshakeContext(ctx, connection)
		if err == nil {
			return connection, methods, nil
		}
		_ = connection.Close()

		var gatewayErr *openClawGatewayResponseError
		if attempt != 0 || !errors.As(err, &gatewayErr) || !gatewayErr.Retryable || gatewayErr.RetryAfter <= 0 {
			return nil, nil, err
		}
		timer := time.NewTimer(gatewayErr.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, fmt.Errorf("OpenClaw Gateway operator.write handshake retry exhausted")
}

func openClawGatewayRequest(connection *websocket.Conn, method string, params map[string]any) (json.RawMessage, error) {
	return openClawGatewayRequestContext(context.Background(), connection, method, params)
}

func openClawGatewayRequestContext(ctx context.Context, connection *websocket.Conn, method string, params map[string]any) (json.RawMessage, error) {
	if !openClawGatewayMethodAllowed(method, openClawGatewayWriteScopedMethods) {
		return nil, fmt.Errorf("OpenClaw Gateway method is not allowlisted for a write-scoped connection")
	}
	requestID := uuid.NewString()
	if err := sendOpenClawGatewayJSONContext(ctx, connection, map[string]any{"type": "req", "id": requestID, "method": method, "params": params}); err != nil {
		return nil, err
	}
	return receiveOpenClawGatewayRPCContext(ctx, connection, requestID)
}

type openClawGatewayCreatedSessionReceipt struct {
	Key        string
	SessionID  string
	RunID      string
	RunStarted bool
}

func openClawGatewayCreatedSession(payload json.RawMessage) (openClawGatewayCreatedSessionReceipt, error) {
	var receipt struct {
		OK         bool   `json:"ok"`
		Key        string `json:"key"`
		SessionID  string `json:"sessionId"`
		RunID      string `json:"runId"`
		RunStarted bool   `json:"runStarted"`
	}
	if err := json.Unmarshal(payload, &receipt); err != nil || !receipt.OK || !validOpenClawGatewaySessionKey(receipt.Key) {
		return openClawGatewayCreatedSessionReceipt{}, fmt.Errorf("invalid delegated session receipt")
	}
	if receipt.RunStarted && !validOpenClawGatewayRunID(receipt.RunID) {
		return openClawGatewayCreatedSessionReceipt{}, fmt.Errorf("delegated session did not provide a valid running receipt")
	}
	if receipt.SessionID != "" && (strings.TrimSpace(receipt.SessionID) != receipt.SessionID || !validOpenClawGatewayRunID(receipt.SessionID)) {
		return openClawGatewayCreatedSessionReceipt{}, fmt.Errorf("invalid delegated session instance")
	}
	return openClawGatewayCreatedSessionReceipt{Key: receipt.Key, SessionID: receipt.SessionID, RunID: strings.TrimSpace(receipt.RunID), RunStarted: receipt.RunStarted}, nil
}

func validOpenClawGatewaySessionKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 80 {
		return false
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validOpenClawGatewayRunID(runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(runID) > 256 {
		return false
	}
	for _, character := range runID {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

// ValidOpenClawGatewayRunID reports whether an identifier is safe to bind to
// an exact-run cancellation or terminal observation.
func ValidOpenClawGatewayRunID(runID string) bool {
	return validOpenClawGatewayRunID(runID)
}

func openClawGatewaySessionReference(sessionKey string) string {
	return "ocgw:v1:" + base64.RawURLEncoding.EncodeToString([]byte(sessionKey))
}

func openClawGatewayReceiptReference() string {
	return "ocgw:v2:" + uuid.NewString()
}

func validOpenClawGatewayReceiptReference(reference string) bool {
	const prefix = "ocgw:v2:"
	if !strings.HasPrefix(reference, prefix) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(reference, prefix))
	return err == nil
}

func openClawGatewayDelegationFailure(started time.Time, message string) Result {
	return Result{
		RuntimeID:  "openclaw",
		Status:     "blocked",
		Message:    message,
		ExitCode:   -1,
		DurationMs: time.Since(started).Milliseconds(),
		AuditEvents: []string{
			"OpenClaw Gateway delegated session was not claimed as created",
			"operator.write Gateway errors are intentionally not exposed in task output",
		},
	}
}

func (a *openClawAdapter) StopTask(_ context.Context, taskID string) StopResult {
	return unsupportedStopTask("openclaw", taskID, "OpenClaw session cancellation requires the persisted owner-bound Gateway execution reference; HAI will not infer a remote target from task ID alone")
}

func (a *openClawAdapter) StopTaskWithReference(ctx context.Context, taskID, ownerIdentity, executionReference string) StopResult {
	taskID = strings.TrimSpace(taskID)
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	executionReference = strings.TrimSpace(executionReference)
	base := StopResult{RuntimeID: "openclaw", TaskID: taskID, ExecutionReference: executionReference}
	if reason := a.gatewayDelegationBlockedReason(); reason != "" {
		base.Status, base.Message, base.AuditEvents = "blocked", reason, []string{"delegated-session stop rejected before Gateway access"}
		return base
	}
	receipt, err := a.openClawGatewayReceiptForOwnerReference(taskID, ownerIdentity, executionReference)
	if err != nil {
		base.Status, base.Message, base.AuditEvents = "indeterminate", "stored owner-bound OpenClaw delegated-session reference or exact run is unavailable; remote cancellation was not inferred", []string{"exact owner, task, and execution reference did not resolve to a persisted run"}
		return base
	}
	return a.stopExactOpenClawGatewayReceipt(ctx, receipt)
}

func (a *openClawAdapter) StopOpenClawGatewayReceipt(ctx context.Context, expected OpenClawGatewayReceipt) StopResult {
	base := StopResult{
		RuntimeID: "openclaw", TaskID: strings.TrimSpace(expected.RuntimeTaskID),
		ExecutionReference: strings.TrimSpace(expected.ExecutionReference),
	}
	if reason := a.gatewayDelegationBlockedReason(); reason != "" {
		base.Status, base.Message, base.AuditEvents = "blocked", reason, []string{"delegated-session stop rejected before Gateway access"}
		return base
	}
	stored, err := a.openClawGatewayReceiptForOwnerReference(expected.RuntimeTaskID, expected.OwnerIdentity, expected.ExecutionReference)
	if err != nil || stored.SessionKey != expected.SessionKey || stored.RunID != expected.RunID ||
		(expected.SessionID != "" && stored.SessionID != expected.SessionID) || stored.Status != expected.Status {
		base.Status = "indeterminate"
		base.Message = "the exact stored OpenClaw receipt changed or could not be verified; remote cancellation was not attempted"
		base.AuditEvents = []string{"exact owner/task/reference/session-key/run-ID binding did not match the current durable receipt"}
		return base
	}
	return a.stopExactOpenClawGatewayReceipt(ctx, stored)
}

func (a *openClawAdapter) stopExactOpenClawGatewayReceipt(ctx context.Context, receipt OpenClawGatewayReceipt) StopResult {
	base := StopResult{RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference}
	if !ValidOpenClawGatewayRunID(receipt.RunID) || strings.TrimSpace(receipt.SessionKey) == "" {
		base.Status, base.Message, base.AuditEvents = "indeterminate", "The stored OpenClaw reference has no exact run identity; the durable admission record remains under review", []string{"session-wide cancellation was not substituted for a missing run identity"}
		return base
	}
	requestContext, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	connection, features, err := a.openClawGatewayOperatorWriteConnection(requestContext)
	if err != nil {
		base.Status, base.Message, base.AuditEvents = "indeterminate", "OpenClaw Gateway cancellation could not be delivered; inspect the exact Gateway run before retrying", []string{"delegated-session cancellation connection failed", "cancellation outcome was not claimed"}
		return base
	}
	defer connection.Close()
	if !containsExact(features, "sessions.abort") {
		base.Status, base.Message, base.AuditEvents = "indeterminate", "OpenClaw Gateway cancellation authority could not be verified; inspect the exact Gateway run before retrying", []string{"delegated-session cancellation scope unavailable", "cancellation outcome was not claimed"}
		return base
	}
	if _, err := openClawGatewayRequestContext(requestContext, connection, "sessions.abort", map[string]any{"key": receipt.SessionKey, "runId": receipt.RunID}); err != nil {
		base.Status, base.Message, base.AuditEvents = "indeterminate", "OpenClaw Gateway did not acknowledge cancellation; inspect the exact run before retrying", []string{"delegated-session abort acknowledgment unavailable", "cancellation outcome was not claimed"}
		return base
	}
	base.Status = "cancellation_requested"
	base.Message = "OpenClaw Gateway accepted the exact-run cancellation request; terminal state still requires verification"
	base.AuditEvents = []string{
		"owner-bound opaque delegated-session reference resolved",
		"OpenClaw Gateway accepted sessions.abort for the exact stored run; unrelated queued work was not cleared",
		"cancellation delivery was acknowledged but terminal completion was not claimed",
	}
	return base
}

// ReconcileDelegatedSession observes one persisted Gateway run without
// importing its transcript or treating a wait timeout as a terminal result.
// OpenClaw's agent.wait requires operator.write in protocol v4, so this uses
// the already-separated delegation credential and never widens authority.
func (a *openClawAdapter) ReconcileDelegatedSession(ctx context.Context, taskID, ownerIdentity, executionReference string) DelegatedSessionReconcileResult {
	taskID = strings.TrimSpace(taskID)
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	executionReference = strings.TrimSpace(executionReference)
	result := DelegatedSessionReconcileResult{RuntimeID: "openclaw", TaskID: taskID, OwnerIdentity: ownerIdentity, ExecutionReference: executionReference}
	if reason := a.gatewayDelegationBlockedReason(); reason != "" {
		result.Status = "indeterminate"
		result.Message = reason
		return result
	}
	receipt, err := a.openClawGatewayReceiptForOwnerReference(taskID, ownerIdentity, executionReference)
	if err != nil || !validOpenClawGatewayRunID(receipt.RunID) {
		result.Status = "indeterminate"
		result.Message = "stored OpenClaw delegated-session receipt is invalid"
		return result
	}
	requestContext, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	connection, features, err := a.openClawGatewayOperatorWriteConnection(requestContext)
	if err != nil {
		result.Status = "indeterminate"
		result.Message = "OpenClaw Gateway terminal verification could not connect"
		return result
	}
	defer connection.Close()
	if !containsExact(features, "agent.wait") {
		result.Status = "indeterminate"
		result.Message = "OpenClaw Gateway terminal verification authority could not be verified"
		return result
	}
	payload, err := openClawGatewayRequestContext(requestContext, connection, "agent.wait", map[string]any{"runId": receipt.RunID, "timeoutMs": 0})
	if err != nil {
		result.Status = "indeterminate"
		result.Message = "OpenClaw Gateway did not provide terminal verification"
		return result
	}
	status, finishedAt, err := openClawGatewayWaitStatus(payload, receipt.RunID)
	if err != nil {
		result.Status = "indeterminate"
		result.Message = "OpenClaw Gateway returned an invalid terminal verification receipt"
		return result
	}
	result.Status = status
	result.FinishedAt = finishedAt
	switch status {
	case "completed":
		result.Message = "OpenClaw Gateway verified the delegated run completed"
		result.AuditEvents = []string{"Gateway agent.wait verified a terminal successful run", "Gateway reply content was not imported into the automation ledger"}
	case "failed":
		result.Message = "OpenClaw Gateway verified the delegated run failed"
		result.AuditEvents = []string{"Gateway agent.wait verified a terminal failed run", "Gateway error detail was not imported into the automation ledger"}
	case "running":
		result.Message = "OpenClaw Gateway has not provided a terminal run receipt"
		result.AuditEvents = []string{"Gateway agent.wait returned a nonterminal observation"}
	default:
		result.Status = "indeterminate"
		result.Message = "OpenClaw Gateway returned an unrecognized terminal observation"
	}
	if result.Status == "completed" || result.Status == "failed" {
		result.AuditEvents = append(result.AuditEvents, a.sessionUsageAudit(requestContext, receipt, result.FinishedAt))
	}
	return result
}

// ImportDelegatedArtifacts imports only metadata for a terminally confirmed,
// owner-bound delegated run. It deliberately never requests artifacts.get or
// artifacts.download, so Gateway content stays within the Gateway.
func (a *openClawAdapter) ImportDelegatedArtifacts(ctx context.Context, taskID string, executionReference string) DelegatedArtifactImportResult {
	return a.importDelegatedArtifacts(ctx, taskID, executionReference, true)
}

// ImportDelegatedArtifactsAfterTerminalVerification accepts only the bounded
// completed observation produced by the same reconciliation path. It exists
// so metadata import can remain ordered before the terminal event is written
// without letting other callers bypass durable-receipt verification.
func (a *openClawAdapter) ImportDelegatedArtifactsAfterTerminalVerification(ctx context.Context, taskID string, executionReference string, terminal DelegatedSessionReconcileResult) DelegatedArtifactImportResult {
	taskID = strings.TrimSpace(taskID)
	executionReference = strings.TrimSpace(executionReference)
	ownerIdentity := strings.TrimSpace(terminal.OwnerIdentity)
	if terminal.RuntimeID != "openclaw" || terminal.TaskID != taskID || terminal.Status != "completed" ||
		ownerIdentity == "" || strings.TrimSpace(terminal.ExecutionReference) != executionReference || terminal.FinishedAt.IsZero() {
		return DelegatedArtifactImportResult{
			RuntimeID: "openclaw",
			TaskID:    taskID,
			Status:    "not_terminal",
			AuditEvents: []string{
				"OpenClaw Gateway artifact metadata import requires a source-backed completed terminal receipt",
			},
		}
	}
	// The supplied projection is not an authority token. Re-bind it to the
	// owner-scoped durable receipt and require that terminal state has already
	// been committed before reading any Gateway artifact metadata.
	receipt, err := a.openClawGatewayReceiptForOwnerReference(taskID, ownerIdentity, executionReference)
	if err != nil || receipt.TerminalStatus != "completed" || receipt.TerminalAt.IsZero() || !receipt.TerminalAt.Equal(terminal.FinishedAt) {
		return DelegatedArtifactImportResult{
			RuntimeID: "openclaw",
			TaskID:    taskID,
			Status:    "not_terminal",
			AuditEvents: []string{
				"OpenClaw Gateway artifact metadata import requires a matching owner-bound durable completed receipt",
			},
		}
	}
	return a.importDelegatedArtifacts(ctx, taskID, executionReference, true)
}

func (a *openClawAdapter) importDelegatedArtifacts(ctx context.Context, taskID string, executionReference string, requireStoredTerminal bool) DelegatedArtifactImportResult {
	taskID = strings.TrimSpace(taskID)
	result := DelegatedArtifactImportResult{RuntimeID: "openclaw", TaskID: taskID}
	if !a.gatewayArtifactImportEnabled {
		result.Status = "not_configured"
		return result
	}
	if !a.gatewayEnabled || strings.TrimSpace(a.gatewayToken) == "" || a.gatewayReceiptStore == nil || a.gatewayArtifactStore == nil || a.validGatewayURL() != "" {
		result.Status = "unavailable"
		result.AuditEvents = []string{"OpenClaw Gateway artifact metadata import was not configured with the required read-only boundary"}
		return result
	}
	if _, blocked := emergencyStopResult("openclaw"); blocked {
		result.Status = "blocked"
		result.AuditEvents = []string{"OpenClaw Gateway artifact metadata import was blocked by the active emergency stop"}
		return result
	}
	receipt, err := a.openClawGatewayReceiptForReference(taskID, executionReference)
	if err != nil || !validOpenClawGatewayRunID(receipt.RunID) {
		result.Status = "unavailable"
		result.AuditEvents = []string{"OpenClaw Gateway artifact metadata import rejected an invalid owner-bound receipt"}
		return result
	}
	if requireStoredTerminal && (receipt.TerminalStatus != "completed" || receipt.TerminalAt.IsZero()) {
		result.Status = "not_terminal"
		result.AuditEvents = []string{"OpenClaw Gateway artifact metadata import requires a source-backed completed terminal receipt"}
		return result
	}
	descriptors, err := a.readArtifactDescriptors(ctx, receipt)
	if err != nil {
		result.Status = "unavailable"
		result.AuditEvents = []string{"OpenClaw Gateway returned invalid artifact metadata"}
		return result
	}
	if len(descriptors) == 0 {
		result.Status = "none"
		result.AuditEvents = []string{"OpenClaw Gateway terminal run exposed no eligible artifact metadata"}
		return result
	}
	_, stopBlocked, persistErr := withExecutionAdmission(ctx, "openclaw", func(commitCtx context.Context) error {
		return a.gatewayArtifactStore.CreateOpenClawGatewayArtifactDescriptors(commitCtx, receipt.ExecutionReference, descriptors)
	})
	if stopBlocked {
		result.Status = "blocked"
		result.AuditEvents = []string{"OpenClaw artifact metadata was not persisted because the emergency stop became active before the final commit"}
		return result
	}
	if persistErr != nil {
		result.Status = "unavailable"
		result.AuditEvents = []string{"HAI could not persist bounded OpenClaw Gateway artifact metadata"}
		return result
	}
	result.Status = "imported"
	result.Count = len(descriptors)
	result.AuditEvents = []string{"OpenClaw Gateway artifact metadata was read with operator.read and stored without artifact contents, titles, URLs, or raw identifiers"}
	return result
}

func (a *openClawAdapter) openClawGatewayReceiptForReference(taskID string, executionReference string) (OpenClawGatewayReceipt, error) {
	executionReference = strings.TrimSpace(executionReference)
	if strings.HasPrefix(executionReference, "ocgw:v2:") {
		if a.gatewayReceiptStore == nil || !validOpenClawGatewayReceiptReference(executionReference) {
			return OpenClawGatewayReceipt{}, fmt.Errorf("invalid delegated receipt reference")
		}
		receipt, err := a.gatewayReceiptStore.FindOpenClawGatewayReceipt(executionReference)
		if err != nil || receipt.ExecutionReference != executionReference || receipt.RuntimeTaskID != taskID || !validOpenClawGatewaySessionKey(receipt.SessionKey) {
			return OpenClawGatewayReceipt{}, fmt.Errorf("delegated receipt lookup failed")
		}
		return receipt, nil
	}
	// v1 entries are historic launch references. They predate persisted run IDs
	// and require manual review rather than guessing which current run they own.
	key, err := openClawGatewaySessionKeyFromReference(executionReference)
	if err != nil {
		return OpenClawGatewayReceipt{}, err
	}
	return OpenClawGatewayReceipt{ExecutionReference: executionReference, RuntimeTaskID: taskID, SessionKey: key}, nil
}

func (a *openClawAdapter) openClawGatewayReceiptForOwnerReference(taskID, ownerIdentity, executionReference string) (OpenClawGatewayReceipt, error) {
	taskID = strings.TrimSpace(taskID)
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	executionReference = strings.TrimSpace(executionReference)
	if ownerIdentity == "" || !validOpenClawGatewayReceiptReference(executionReference) {
		return OpenClawGatewayReceipt{}, fmt.Errorf("exact owner-bound Gateway receipt is required")
	}
	receipt, err := a.openClawGatewayReceiptForReference(taskID, executionReference)
	if err != nil || receipt.OwnerIdentity != ownerIdentity || receipt.RuntimeTaskID != taskID || receipt.ExecutionReference != executionReference {
		return OpenClawGatewayReceipt{}, fmt.Errorf("exact owner-bound Gateway receipt lookup failed")
	}
	return receipt, nil
}

func openClawGatewayWaitStatus(payload json.RawMessage, expectedRunID string) (string, time.Time, error) {
	var receipt struct {
		RunID   string      `json:"runId"`
		Status  string      `json:"status"`
		EndedAt json.Number `json:"endedAt"`
	}
	if err := json.Unmarshal(payload, &receipt); err != nil {
		return "", time.Time{}, err
	}
	if !validOpenClawGatewayRunID(expectedRunID) || receipt.RunID != expectedRunID {
		return "", time.Time{}, fmt.Errorf("agent.wait returned a different or missing run identity")
	}
	switch strings.TrimSpace(receipt.Status) {
	case "pending", "timeout":
		return "running", time.Time{}, nil
	case "ok":
		return "completed", openClawGatewayReceiptTime(receipt.EndedAt), nil
	case "error":
		return "failed", openClawGatewayReceiptTime(receipt.EndedAt), nil
	default:
		return "", time.Time{}, fmt.Errorf("unrecognized agent.wait status")
	}
}

func openClawGatewayReceiptTime(value json.Number) time.Time {
	milliseconds, err := value.Int64()
	if err != nil || milliseconds <= 0 {
		return time.Time{}
	}
	observed := time.UnixMilli(milliseconds).UTC()
	if observed.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) || observed.After(time.Now().UTC().Add(24*time.Hour)) {
		return time.Time{}
	}
	return observed
}

func openClawGatewaySessionKeyFromReference(reference string) (string, error) {
	const prefix = "ocgw:v1:"
	if !strings.HasPrefix(reference, prefix) {
		return "", fmt.Errorf("unrecognized session reference")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(reference, prefix))
	if err != nil || !validOpenClawGatewaySessionKey(string(decoded)) {
		return "", fmt.Errorf("invalid session reference")
	}
	return string(decoded), nil
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
		"Gateway inspection and owner-bound recovery for previously persisted sessions; direct CLI task execution is blocked, and new delegated execution remains blocked until run-bound effective sandbox and tool policy can be verified",
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
		"OpenClaw Gateway local-first control plane; delegated execution awaits identity-bound sandbox-policy verification",
		"OpenClaw channel, node, canvas, voice, plugin, skill, and model-provider ecosystems",
		"OpenClaw sandbox backends and tool approval layer",
		"HAI source-grounded verification and workflow completion state machine",
	}
}

func (a *openClawAdapter) controls() []string {
	controls := []string{
		"disabled by default through OPENCLAW_AGENT_ENABLED",
		"server-side HAI approval required before every task",
		"direct OpenClaw CLI task execution is blocked because HAI cannot verify the effective sandbox and tool policy bound to the exact run",
		"OPENCLAW_AGENT_CLI_ENABLED is a legacy setting and does not authorize direct CLI execution",
		"Gateway read and delegated-execution tokens are separate; no credentials are passed to an OpenClaw CLI process",
		"Gateway URL host constrained by AGENT_RUNTIME_ALLOWED_HOSTS when configured",
		"new Gateway session creation is blocked until an identity-bound sandbox-required role and exact-run policy evidence are implemented",
		"existing persisted Gateway receipts can be stopped or reconciled only by their exact owner-bound run identity",
		"HAI adapter does not call OpenClaw message send, pairing approve, node commands, browser actions, cron writes, or public posting",
	}
	if a.sandboxRequired {
		controls = append(controls, "configured sandbox mode is diagnostic only; it is not a run-bound policy attestation and does not authorize CLI execution")
	} else {
		controls = append(controls, "OpenClaw sandbox requirement is disabled in HAI configuration; direct CLI execution remains blocked")
	}
	if a.gatewayEnabled {
		controls = append(controls, "health-only Gateway discovery is token-free; authenticated operator.read discovery requires OPENCLAW_GATEWAY_TOKEN and keeps Gateway scopes/pairing authoritative")
	}
	if a.gatewayTaskLedgerDiscoveryEnabled && !a.gatewayAuthenticatedDiscoveryEnabled {
		controls = append(controls, "Gateway task-ledger discovery is blocked until OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true")
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
		"agent-cli=blocked-no-run-bound-effective-policy-attestation",
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
		ecosystemSurfaceWithRisk("Skills", inventory.status, inventory.skills, "Visible to HAI planning; execution stays blocked until HAI verifies identity-bound sandbox policy for the exact Gateway run.", "medium", true),
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
	if !isManagedOpenClawArchivePath(path, a) {
		return fmt.Errorf("openclaw uploaded ecosystem path is not a HAI-managed persistent archive")
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
	if trustedUpload && !isManagedOpenClawArchivePath(path, a) {
		return preparedOpenClawEcosystemPath{},
			fmt.Errorf("openclaw uploaded ecosystem path is not a HAI-managed persistent archive")
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

	return preparedOpenClawEcosystemPath{
		targetPath:        absolutePath,
		targetSignature:   openClawEcosystemSignature(absolutePath),
		previousPath:      previousPath,
		previousSignature: openClawEcosystemSignature(previousPath),
	}, nil
}

func (a *openClawAdapter) applyPreparedEcosystemPath(
	prepared preparedOpenClawEcosystemPath,
) error {
	if err := validateOpenClawEcosystemPath(prepared.targetPath); err != nil {
		return err
	}
	return a.commitPreparedEcosystemPath(prepared)
}

func (a *openClawAdapter) commitPreparedEcosystemPath(
	prepared preparedOpenClawEcosystemPath,
) error {
	decision, err := a.withEcosystemCommitFence(func() error {
		return a.commitPreparedEcosystemPathLocked(prepared)
	})
	if decision.Active {
		return ecosystemEmergencyStopError(decision)
	}
	return err
}

func (a *openClawAdapter) withEcosystemCommitFence(
	mutate func() error,
) (safety.EmergencyStopDecision, error) {
	// Take adapter state first: inventory scans may hold this lock while parsing
	// local files, which must not delay emergency-stop writers behind the fence.
	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()

	releaseFence := safety.AcquireExecutionCommitFence()
	defer releaseFence()
	decision := safety.EvaluateEmergencyStop()
	if decision.Active {
		return decision, nil
	}
	return decision, mutate()
}

func ecosystemEmergencyStopError(decision safety.EmergencyStopDecision) error {
	if reason := strings.TrimSpace(decision.Reason); reason != "" {
		return fmt.Errorf("emergency stop blocks OpenClaw ecosystem mutation: %s", reason)
	}
	return errors.New("emergency stop blocks OpenClaw ecosystem mutation")
}

// commitPreparedEcosystemPathLocked performs only metadata checks and state
// updates; callers must hold inventoryMu and validate archive contents first.
func (a *openClawAdapter) commitPreparedEcosystemPathLocked(
	prepared preparedOpenClawEcosystemPath,
) error {
	if openClawEcosystemSignature(prepared.targetPath) != prepared.targetSignature {
		return ErrEcosystemMutationConflict
	}

	currentPath := strings.TrimSpace(a.ecosystemPath)
	if currentPath != prepared.previousPath ||
		openClawEcosystemSignature(currentPath) != prepared.previousSignature {
		return ErrEcosystemMutationConflict
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

func (a *openClawAdapter) refreshEcosystemInventory() error {
	decision, err := a.withEcosystemCommitFence(func() error {
		a.refreshEcosystemInventoryLocked()
		return nil
	})
	if decision.Active {
		return ecosystemEmergencyStopError(decision)
	}
	return err
}

func (a *openClawAdapter) refreshEcosystemInventoryLocked() {
	a.inventoryLoaded = false
	a.inventoryPath = ""
	a.inventorySignature = ""
	a.inventory = openClawEcosystemInventory{}
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
	decision, err := a.withEcosystemCommitFence(func() error {
		return a.refreshEcosystemInventoryIfCurrentLocked(expectedPath, expectedSignature)
	})
	if decision.Active {
		return ecosystemEmergencyStopError(decision)
	}
	return err
}

// refreshEcosystemInventoryIfCurrentLocked requires inventoryMu to be held.
func (a *openClawAdapter) refreshEcosystemInventoryIfCurrentLocked(
	expectedPath string,
	expectedSignature string,
) error {
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
	if a.gatewayEnabled {
		items = append(items, "Gateway control plane")
	}
	if a.gatewayDelegationEnabled {
		items = append(items, "Gateway delegation configured but blocked pending identity-bound sandbox-policy attestation")
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
		"keep new Gateway delegation disabled; HAI requires a verified durable operator identity and sandbox-required role before session creation",
		"create a HAI automation with launchType=agent_runtime and runtimeType=openclaw",
		"keep high-risk channel, host, browser, cron, node, and posting surfaces disabled; direct CLI stays blocked without run-bound policy attestation",
	}
	if inventory.status != "available" {
		items = append(items, "set OPENCLAW_ECOSYSTEM_PATH to openclaw-main.zip or an extracted OpenClaw checkout for read-only inventory")
	}
	if a.gatewayAuthenticatedDiscoveryEnabled || a.gatewayTaskLedgerDiscoveryEnabled {
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
		return "OPENCLAW_GATEWAY_URL must not include credentials; use OPENCLAW_GATEWAY_TOKEN"
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "ws", "wss", "http", "https":
	default:
		return "OPENCLAW_GATEWAY_URL must use ws, wss, http, or https"
	}
	host := normalizedOpenClawGatewayHost(parsed.Hostname())
	if host == "" || strings.Contains(host, "%") {
		return "OPENCLAW_GATEWAY_URL must use a valid host"
	}
	if a.allowedHost["*"] {
		return "AGENT_RUNTIME_ALLOWED_HOSTS must not contain a wildcard"
	}
	if !a.allowedHost[host] {
		if host == openClawDockerInternalHost {
			return "host.docker.internal must be explicitly listed in AGENT_RUNTIME_ALLOWED_HOSTS"
		}
		return "OpenClaw Gateway host is not in AGENT_RUNTIME_ALLOWED_HOSTS"
	}
	dockerInternalOptedIn := a.hostDockerInternalGatewayOptedIn(parsed)
	if host == openClawDockerInternalHost && !dockerInternalOptedIn {
		return "host.docker.internal requires OPENCLAW_GATEWAY_ENABLED=true, an explicit URL port, and private IP pins in OPENCLAW_GATEWAY_DOCKER_HOST_IPS"
	}
	if (scheme == "ws" || scheme == "http") && !gatewayHostAllowsLoopback(host) && !dockerInternalOptedIn {
		return "OpenClaw Gateway plaintext transport is allowed only for loopback development"
	}
	ip := net.ParseIP(host)
	if ip != nil && blockedOpenClawGatewayAddress(ip, gatewayHostAllowsLoopback(host)) {
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
	return &odysseusAdapter{
		enabled:     envEnabled("ODYSSEUS_AGENT_ENABLED"),
		baseURL:     baseURL,
		token:       strings.TrimSpace(os.Getenv("ODYSSEUS_API_TOKEN")),
		sessionID:   strings.TrimSpace(os.Getenv("ODYSSEUS_AGENT_SESSION_ID")),
		workspace:   strings.TrimSpace(os.Getenv("ODYSSEUS_AGENT_WORKSPACE")),
		timeout:     time.Duration(boundedIntEnv("ODYSSEUS_AGENT_TIMEOUT_SECONDS", defaultTimeoutSeconds, 1, 900)) * time.Second,
		outputLimit: int64(boundedIntEnv("AGENT_RUNTIME_OUTPUT_LIMIT_BYTES", defaultOutputLimit, 4096, maxOutputLimit)),
		allowedHost: csvMap(firstNonEmpty(os.Getenv("AGENT_RUNTIME_ALLOWED_HOSTS"), "localhost,127.0.0.1,::1,host.docker.internal,odysseus")),

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
		ExecutionEnabled:     false,
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
	resp, err := noRedirectClient(a.timeout).Do(req)
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
	health.Status = "blocked"
	health.Reason = "Odysseus scoped capabilities API is reachable, but " + odysseusToolMediationBlockReason + "; " + strings.Join(a.ecosystemReadiness(), ", ")
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
		ExecutionMode:    "blocked_tool_mediation",
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
			ExecutionMode:    "blocked_tool_mediation",
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
	add(a.shellEnabled, "shell", "shell", "host-control", "high", "Shell access is high-risk. HAI keeps allow_bash=false until individual tool invocations can be authorized.", "shell", "high-risk")
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

func (a *odysseusAdapter) ExecuteTask(_ context.Context, _ Task) Result {
	started := time.Now()
	return Result{
		RuntimeID:  "odysseus",
		Status:     "blocked",
		Message:    odysseusToolMediationBlockReason,
		ExitCode:   -1,
		DurationMs: time.Since(started).Milliseconds(),
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
		odysseusToolMediationBlockReason,
	}
	if a.shellEnabled || a.allowBash {
		controls = append(controls, "Odysseus shell may be configured externally, but HAI always sends allow_bash=false until individual tool invocations can be authorized")
	} else {
		controls = append(controls, "Odysseus shell/bash execution is disabled by configuration; HAI also keeps allow_bash=false")
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

func noRedirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
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

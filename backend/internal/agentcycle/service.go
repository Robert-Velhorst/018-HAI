package agentcycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/workflow"
)

type SourceSyncer interface {
	RunDueScheduledSyncs(now time.Time) (*source.ScheduledSyncRun, error)
}

type OwnerScopedSourceSyncer interface {
	RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*source.ScheduledSyncRun, error)
}

type OwnerScopedContextSourceSyncer interface {
	RunDueScheduledSyncsForOwnerContext(context.Context, time.Time, string) (*source.ScheduledSyncRun, error)
}

type WorkflowCoordinator interface {
	RecoverStaleClaims(request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error)
	RunDueOpenLoops(request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error)
	RunDue(request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error)
	Dashboard() (*workflow.WorkflowDashboard, error)
}

type OwnerScopedWorkflowCoordinator interface {
	RecoverStaleClaimsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error)
	RunDueOpenLoopsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error)
	RunDueForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error)
	DashboardForOwner(ownerIdentity string) (*workflow.WorkflowDashboard, error)
}

type OwnerScopedContextWorkflowCoordinator interface {
	RecoverStaleClaimsForOwnerContext(context.Context, string, workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error)
	RunDueOpenLoopsForOwnerContext(context.Context, string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error)
	RunDueForOwnerContext(context.Context, string, workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error)
}

type AmbientScanner interface {
	Scan(trigger string) (*models.AmbientScan, error)
}

type OwnerScopedAmbientScanner interface {
	ScanForOwner(ownerIdentity, trigger string) (*models.AmbientScan, error)
}

type OwnerScopedAmbientSnapshotScanner interface {
	ScanForOwnerWithPursuitDashboard(ownerIdentity, trigger string, dashboard *pursuit.Dashboard) (*models.AmbientScan, error)
}

type PursuitBriefProvider interface {
	Brief() (*pursuit.Brief, error)
}

type PursuitDecisionProvider interface {
	Decisions() ([]pursuit.PursuitDashboardDecision, error)
}

type PursuitOwnerBriefProvider interface {
	BriefForOwner(ownerIdentity string) (*pursuit.Brief, error)
}

type PursuitOwnerDecisionProvider interface {
	DecisionsForOwner(ownerIdentity string) ([]pursuit.PursuitDashboardDecision, error)
}

type PursuitOperatingSnapshotProvider interface {
	OperatingSnapshot() (*pursuit.OperatingSnapshot, error)
}

type PursuitOwnerOperatingSnapshotProvider interface {
	OperatingSnapshotForOwner(ownerIdentity string) (*pursuit.OperatingSnapshot, error)
}

type RunRequest struct {
	OwnerIdentity  string `json:"-"`
	Trigger        string `json:"trigger,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	SkipSourceSync bool   `json:"skipSourceSync,omitempty"`
	SkipAmbient    bool   `json:"skipAmbient,omitempty"`
}

type PhaseError struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

type WorkerStep struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type RunResult struct {
	ExecutionScope   string                             `json:"executionScope"`
	Trigger          string                             `json:"trigger"`
	Status           string                             `json:"status"`
	StartedAt        time.Time                          `json:"startedAt"`
	CompletedAt      time.Time                          `json:"completedAt"`
	Steps            []WorkerStep                       `json:"steps"`
	Errors           []PhaseError                       `json:"errors"`
	AppliedContext   []memory.RankedMemory              `json:"appliedContext,omitempty"`
	ContextNote      string                             `json:"contextNote,omitempty"`
	SourceSync       *source.ScheduledSyncRun           `json:"sourceSync,omitempty"`
	Recovery         *workflow.ClaimRecoverySummary     `json:"recovery,omitempty"`
	OpenLoops        *workflow.OpenLoopRunSummary       `json:"openLoops,omitempty"`
	Workflows        *workflow.WorkflowRunSummary       `json:"workflows,omitempty"`
	AmbientScan      *models.AmbientScan                `json:"ambientScan,omitempty"`
	Dashboard        *workflow.WorkflowDashboard        `json:"dashboard,omitempty"`
	PursuitBrief     *pursuit.Brief                     `json:"pursuitBrief,omitempty"`
	PursuitDecisions []pursuit.PursuitDashboardDecision `json:"pursuitDecisions,omitempty"`
	PursuitState     *PursuitOperatingState             `json:"pursuitOperatingState,omitempty"`
	NextAction       string                             `json:"nextAction"`
	SafetySummary    string                             `json:"safetySummary"`
	LearningIDs      []string                           `json:"learningIds,omitempty"`
	LearningNote     string                             `json:"learningNote,omitempty"`
}

type PursuitOperatingState struct {
	OperatingMode        string `json:"operatingMode"`
	PrimaryLane          string `json:"primaryLane"`
	PrimaryAction        string `json:"primaryAction"`
	NeedsRobert          int    `json:"needsRobert"`
	ReadyToMove          int    `json:"readyToMove"`
	Stuck                int    `json:"stuck"`
	ReviewDue            int    `json:"reviewDue"`
	PlanningNeeded       int    `json:"planningNeeded"`
	CompletionCandidates int    `json:"completionCandidates"`
	RecentlyChanged      int    `json:"recentlyChanged"`
	AttentionTotal       int    `json:"attentionTotal"`
}

type Service struct {
	sources   SourceSyncer
	workflows WorkflowCoordinator
	ambient   AmbientScanner
	pursuits  PursuitBriefProvider
	memory    memory.Service
}

func NewService(sources SourceSyncer, workflows WorkflowCoordinator, ambient AmbientScanner, memoryServices ...memory.Service) *Service {
	return NewServiceWithPursuits(sources, workflows, ambient, nil, memoryServices...)
}

func NewServiceWithPursuits(sources SourceSyncer, workflows WorkflowCoordinator, ambient AmbientScanner, pursuits PursuitBriefProvider, memoryServices ...memory.Service) *Service {
	return &Service{
		sources:   sources,
		workflows: workflows,
		ambient:   ambient,
		pursuits:  pursuits,
		memory:    firstMemoryService(memoryServices...),
	}
}

func (s *Service) Run(request RunRequest) *RunResult {
	result, _ := s.RunContext(context.Background(), request)
	return result
}

// RunContext binds an interactive owner-scoped operating pass to its caller.
// Durable in-process workers continue to use Run, which supplies a background
// context. Cancellation is observed only between phases and atomic work units;
// already-claimed effects are allowed to settle and persist their audit state.
func (s *Service) RunContext(ctx context.Context, request RunRequest) (*RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ownerIdentity := strings.TrimSpace(request.OwnerIdentity); ownerIdentity != "" {
		return s.runForOwnerContext(ctx, ownerIdentity, request)
	}
	result := s.runSystem(request)
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}
	return result, nil
}

func (s *Service) runSystem(request RunRequest) *RunResult {
	started := time.Now().UTC()
	result := &RunResult{
		ExecutionScope: "system_worker",
		Trigger:        firstNonEmpty(request.Trigger, "manual"),
		Status:         "running",
		StartedAt:      started,
		Steps:          []WorkerStep{},
		Errors:         []PhaseError{},
		SafetySummary:  "Agent cycle cannot approve its own high-risk actions; approval gates, emergency stop, and workflow retry limits remain enforced inside the called engines.",
	}
	limit := normalizeLimit(request.Limit)
	workflowRequest := workflow.RunDueRequest{Limit: limit}

	contextResult, err := s.retrieveOperationalContext(result.Trigger)
	if err != nil {
		result.record("retrieve operational context", err, "context retrieval failed")
	} else {
		result.AppliedContext = contextResult
		result.ContextNote = appliedContextSummary(contextResult)
		result.record("retrieve operational context", nil, result.ContextNote)
	}

	if s.workflows == nil {
		result.addError("workflow", fmt.Errorf("workflow coordinator is not configured"))
	} else {
		recovery, err := s.workflows.RecoverStaleClaims(workflowRequest)
		result.Recovery = recovery
		result.record("recover stale claims", err, recoverySummary(recovery))
	}

	if !request.SkipSourceSync {
		if s.sources == nil {
			result.addError("source_sync", fmt.Errorf("source syncer is not configured"))
		} else {
			sync, err := s.sources.RunDueScheduledSyncs(time.Now().UTC())
			result.SourceSync = sync
			result.record("sync due sources", err, sourceSummary(sync))
		}
	} else {
		result.Steps = append(result.Steps, WorkerStep{Name: "sync due sources", Status: "skipped", Summary: "source sync skipped by request"})
	}

	if s.workflows != nil {
		openLoops, err := s.workflows.RunDueOpenLoops(workflowRequest)
		result.OpenLoops = openLoops
		result.record("run due follow-ups", err, openLoopSummary(openLoops))

		workflows, err := s.workflows.RunDue(workflowRequest)
		result.Workflows = workflows
		result.record("run safe workflows", err, workflowSummary(workflows))
	}

	if !request.SkipAmbient {
		if s.ambient == nil {
			result.addError("ambient", fmt.Errorf("ambient scanner is not configured"))
		} else {
			scan, err := s.ambient.Scan("agent-cycle." + result.Trigger)
			result.AmbientScan = scan
			result.record("scan ambient opportunities", err, ambientSummary(scan))
		}
	} else {
		result.Steps = append(result.Steps, WorkerStep{Name: "scan ambient opportunities", Status: "skipped", Summary: "ambient scan skipped by request"})
	}

	if s.workflows != nil {
		dashboard, err := s.workflows.Dashboard()
		result.Dashboard = dashboard
		result.record("refresh workflow dashboard", err, dashboardSummary(dashboard))
	}
	if provider, ok := s.pursuits.(PursuitOperatingSnapshotProvider); ok {
		snapshot, err := provider.OperatingSnapshot()
		applyPursuitSnapshot(result, snapshot)
		result.record("refresh pursuit operating brief", err, pursuitBriefSummary(result.PursuitBrief))
		result.record("refresh Robert decision queue", err, pursuitDecisionSummary(result.PursuitDecisions))
	} else if s.pursuits != nil {
		brief, err := s.pursuits.Brief()
		result.PursuitBrief = brief
		result.PursuitState = pursuitOperatingState(brief)
		result.record("refresh pursuit operating brief", err, pursuitBriefSummary(brief))
		if decisionProvider, ok := s.pursuits.(PursuitDecisionProvider); ok {
			decisions, err := decisionProvider.Decisions()
			result.PursuitDecisions = decisions
			result.record("refresh Robert decision queue", err, pursuitDecisionSummary(decisions))
		}
	}

	result.CompletedAt = time.Now().UTC()
	result.Status = cycleStatus(result)
	result.NextAction = nextAction(result)
	result.LearningIDs, result.LearningNote = s.rememberOperationalLesson(result)
	return result
}

// runForOwner is the authenticated operator path. Every operational phase must
// expose an owner-scoped contract; this path never falls back to a global read
// or worker method when an owner-specific implementation is unavailable.
func (s *Service) runForOwnerContext(ctx context.Context, ownerIdentity string, request RunRequest) (*RunResult, error) {
	started := time.Now().UTC()
	result := &RunResult{
		ExecutionScope: "owner_scoped",
		Trigger:        firstNonEmpty(request.Trigger, "manual"),
		Status:         "running",
		StartedAt:      started,
		Steps:          []WorkerStep{},
		Errors:         []PhaseError{},
		SafetySummary:  "Personal operating pass uses only owner-scoped source, workflow, ambient, pursuit, and memory contracts. High-risk actions still require approval and no phase may fall back to global execution.",
	}
	limit := normalizeLimit(request.Limit)
	workflowRequest := workflow.RunDueRequest{Limit: limit}
	var pursuitSnapshot *pursuit.OperatingSnapshot
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	contextResult, err := s.retrieveOperationalContextForOwner(ownerIdentity, result.Trigger)
	if err != nil {
		result.record("retrieve personal operational context", err, "owner-scoped context retrieval failed")
	} else {
		result.AppliedContext = contextResult
		result.ContextNote = appliedContextSummary(contextResult)
		result.record("retrieve personal operational context", nil, result.ContextNote)
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	ownerWorkflows, workflowsOwnerScoped := s.workflows.(OwnerScopedWorkflowCoordinator)
	if s.workflows == nil {
		result.addError("workflow", fmt.Errorf("workflow coordinator is not configured"))
	} else if !workflowsOwnerScoped {
		result.addError("workflow", fmt.Errorf("owner-scoped workflow coordinator is not configured"))
	} else {
		var recovery *workflow.ClaimRecoverySummary
		var err error
		if contextual, ok := s.workflows.(OwnerScopedContextWorkflowCoordinator); ok {
			recovery, err = contextual.RecoverStaleClaimsForOwnerContext(ctx, ownerIdentity, workflowRequest)
		} else {
			recovery, err = ownerWorkflows.RecoverStaleClaimsForOwner(ownerIdentity, workflowRequest)
		}
		result.Recovery = recovery
		if requestContextError(err) {
			return interruptCycle(result, err)
		}
		result.record("recover stale claims", err, recoverySummary(recovery))
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if request.SkipSourceSync {
		result.Steps = append(result.Steps, WorkerStep{Name: "sync due sources", Status: "skipped", Summary: "source sync skipped by request"})
	} else if s.sources == nil {
		result.addError("source_sync", fmt.Errorf("source syncer is not configured"))
	} else if ownerSources, ok := s.sources.(OwnerScopedSourceSyncer); !ok {
		result.addError("source_sync", fmt.Errorf("owner-scoped source syncer is not configured"))
	} else {
		var sync *source.ScheduledSyncRun
		var err error
		if contextual, ok := s.sources.(OwnerScopedContextSourceSyncer); ok {
			sync, err = contextual.RunDueScheduledSyncsForOwnerContext(ctx, time.Now().UTC(), ownerIdentity)
		} else {
			sync, err = ownerSources.RunDueScheduledSyncsForOwner(time.Now().UTC(), ownerIdentity)
		}
		result.SourceSync = sync
		if requestContextError(err) {
			return interruptCycle(result, err)
		}
		result.record("sync due sources", err, sourceSummary(sync))
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if workflowsOwnerScoped {
		var openLoops *workflow.OpenLoopRunSummary
		var err error
		if contextual, ok := s.workflows.(OwnerScopedContextWorkflowCoordinator); ok {
			openLoops, err = contextual.RunDueOpenLoopsForOwnerContext(ctx, ownerIdentity, workflowRequest)
		} else {
			openLoops, err = ownerWorkflows.RunDueOpenLoopsForOwner(ownerIdentity, workflowRequest)
		}
		result.OpenLoops = openLoops
		if requestContextError(err) {
			return interruptCycle(result, err)
		}
		result.record("run due follow-ups", err, openLoopSummary(openLoops))

		if err := ctx.Err(); err != nil {
			return interruptCycle(result, err)
		}
		var workflows *workflow.WorkflowRunSummary
		if contextual, ok := s.workflows.(OwnerScopedContextWorkflowCoordinator); ok {
			workflows, err = contextual.RunDueForOwnerContext(ctx, ownerIdentity, workflowRequest)
		} else {
			workflows, err = ownerWorkflows.RunDueForOwner(ownerIdentity, workflowRequest)
		}
		result.Workflows = workflows
		if requestContextError(err) {
			return interruptCycle(result, err)
		}
		result.record("run safe workflows", err, workflowSummary(workflows))
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if provider, ok := s.pursuits.(PursuitOwnerOperatingSnapshotProvider); ok {
		if scanner, scannerOK := s.ambient.(OwnerScopedAmbientSnapshotScanner); scannerOK && !request.SkipAmbient {
			snapshot, snapshotErr := provider.OperatingSnapshotForOwner(ownerIdentity)
			if snapshotErr == nil && snapshot != nil && snapshot.Dashboard != nil {
				pursuitSnapshot = snapshot
				scan, scanErr := scanner.ScanForOwnerWithPursuitDashboard(ownerIdentity, "agent-cycle."+result.Trigger, snapshot.Dashboard)
				result.AmbientScan = scan
				result.record("scan ambient opportunities", scanErr, ambientSummary(scan))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if request.SkipAmbient {
		result.Steps = append(result.Steps, WorkerStep{Name: "scan ambient opportunities", Status: "skipped", Summary: "ambient scan skipped by request"})
	} else if pursuitSnapshot != nil {
		// The optimized snapshot scanner already completed the ambient phase.
	} else if s.ambient == nil {
		result.addError("ambient", fmt.Errorf("ambient scanner is not configured"))
	} else if ownerAmbient, ok := s.ambient.(OwnerScopedAmbientScanner); !ok {
		result.addError("ambient", fmt.Errorf("owner-scoped ambient scanner is not configured"))
	} else {
		scan, err := ownerAmbient.ScanForOwner(ownerIdentity, "agent-cycle."+result.Trigger)
		result.AmbientScan = scan
		result.record("scan ambient opportunities", err, ambientSummary(scan))
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if workflowsOwnerScoped {
		dashboard, err := ownerWorkflows.DashboardForOwner(ownerIdentity)
		result.Dashboard = dashboard
		result.record("refresh workflow dashboard", err, dashboardSummary(dashboard))
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	if s.pursuits == nil {
		result.addError("pursuits", fmt.Errorf("pursuit brief provider is not configured"))
	} else if pursuitSnapshot != nil {
		applyPursuitSnapshot(result, pursuitSnapshot)
		result.record("refresh personal pursuit operating brief", nil, pursuitBriefSummary(result.PursuitBrief))
		result.record("refresh personal Robert decision queue", nil, pursuitDecisionSummary(result.PursuitDecisions))
	} else if provider, ok := s.pursuits.(PursuitOwnerOperatingSnapshotProvider); ok {
		snapshot, err := provider.OperatingSnapshotForOwner(ownerIdentity)
		applyPursuitSnapshot(result, snapshot)
		result.record("refresh personal pursuit operating brief", err, pursuitBriefSummary(result.PursuitBrief))
		result.record("refresh personal Robert decision queue", err, pursuitDecisionSummary(result.PursuitDecisions))
	} else if provider, ok := s.pursuits.(PursuitOwnerBriefProvider); !ok {
		result.addError("pursuits", fmt.Errorf("owner-scoped pursuit brief is not configured"))
	} else {
		brief, err := provider.BriefForOwner(ownerIdentity)
		result.PursuitBrief = brief
		result.PursuitState = pursuitOperatingState(brief)
		result.record("refresh personal pursuit operating brief", err, pursuitBriefSummary(brief))
		if decisionProvider, ok := s.pursuits.(PursuitOwnerDecisionProvider); ok {
			decisions, decisionErr := decisionProvider.DecisionsForOwner(ownerIdentity)
			result.PursuitDecisions = decisions
			result.record("refresh personal Robert decision queue", decisionErr, pursuitDecisionSummary(decisions))
		} else {
			result.addError("pursuit_decisions", fmt.Errorf("owner-scoped pursuit decisions are not configured"))
		}
	}
	if err := ctx.Err(); err != nil {
		return interruptCycle(result, err)
	}

	result.CompletedAt = time.Now().UTC()
	result.Status = cycleStatus(result)
	result.NextAction = nextAction(result)
	result.LearningIDs, result.LearningNote = s.rememberOperationalLessonForOwner(ownerIdentity, result)
	return result, nil
}

func requestContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func interruptCycle(result *RunResult, err error) (*RunResult, error) {
	if result != nil {
		result.CompletedAt = time.Now().UTC()
		result.Status = "interrupted"
		result.NextAction = "run the operating pass again when the caller is available"
		result.Steps = append(result.Steps, WorkerStep{
			Name:    "stop cancelled operating pass",
			Status:  "interrupted",
			Summary: "caller cancelled the pass; no additional phase or work unit was started",
		})
	}
	return result, err
}

func applyPursuitSnapshot(result *RunResult, snapshot *pursuit.OperatingSnapshot) {
	if result == nil || snapshot == nil {
		return
	}
	result.PursuitBrief = snapshot.Brief
	result.PursuitDecisions = snapshot.Decisions
	result.PursuitState = pursuitOperatingState(snapshot.Brief)
}

func (s *Service) retrieveOperationalContext(trigger string) ([]memory.RankedMemory, error) {
	return s.retrieveOperationalContextForOwner("", trigger)
}

func (s *Service) retrieveOperationalContextForOwner(ownerIdentity, trigger string) ([]memory.RankedMemory, error) {
	if s.memory == nil {
		return nil, nil
	}
	result, err := memory.RetrieveForOwner(s.memory, ownerIdentity, memory.RetrieveRequest{
		Query: "agent cycle operational lesson source sync workflow retry blocked follow-up ambient approval safety " + trigger,
		Limit: 5,
	})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.UsedContext) == 0 {
		return nil, nil
	}
	context := make([]memory.RankedMemory, 0, len(result.UsedContext))
	for _, ranked := range result.UsedContext {
		if operationalMemoryRelevant(ranked.Memory) {
			context = append(context, ranked)
		}
		if len(context) == 3 {
			break
		}
	}
	return context, nil
}

func (s *Service) rememberOperationalLesson(result *RunResult) ([]string, string) {
	if s.memory == nil || !operationalLessonUseful(result) {
		return nil, ""
	}
	content := operationalLessonContent(result)
	created, err := s.memory.Create(memory.CreateRequest{
		Kind:        "procedural",
		Content:     content,
		Summary:     compactText(operationalLessonSummary(result), 240),
		Tags:        operationalLessonTags(result),
		Confidence:  operationalLessonConfidence(result),
		SourceURI:   "agent-cycle://" + result.Trigger,
		SourceLabel: "Agent cycle operational learning",
	})
	if err != nil || created == nil {
		return nil, "operational learning memory was skipped because storage failed"
	}
	return []string{created.ID.String()}, "stored operational lesson for future agent-cycle planning"
}

func (s *Service) rememberOperationalLessonForOwner(ownerIdentity string, result *RunResult) ([]string, string) {
	if s.memory == nil || !operationalLessonUseful(result) {
		return nil, "no material owner-scoped operational lesson was produced"
	}
	created, err := memory.CreateForOwner(s.memory, ownerIdentity, memory.CreateRequest{
		Kind:        "procedural",
		Content:     operationalLessonContent(result),
		Summary:     compactText(operationalLessonSummary(result), 240),
		Tags:        operationalLessonTags(result),
		Confidence:  operationalLessonConfidence(result),
		SourceURI:   "agent-cycle://" + result.Trigger,
		SourceLabel: "Owner-scoped agent cycle operational learning",
	})
	if err != nil || created == nil {
		return nil, "owner-scoped operational learning was skipped because storage failed"
	}
	return []string{created.ID.String()}, "stored owner-scoped operational lesson for future planning"
}

func (r *RunResult) record(name string, err error, summary string) {
	if err != nil {
		r.addError(name, err)
		r.Steps = append(r.Steps, WorkerStep{Name: name, Status: "failed", Summary: err.Error()})
		return
	}
	r.Steps = append(r.Steps, WorkerStep{Name: name, Status: "completed", Summary: summary})
}

func (r *RunResult) addError(phase string, err error) {
	if err == nil {
		return
	}
	r.Errors = append(r.Errors, PhaseError{Phase: phase, Message: err.Error()})
}

func cycleStatus(result *RunResult) string {
	if len(result.Errors) == 0 {
		return "completed"
	}
	for _, step := range result.Steps {
		if step.Status == "completed" || step.Status == "skipped" {
			return "partial_failure"
		}
	}
	return "failed"
}

func nextAction(result *RunResult) string {
	if len(result.Errors) > 0 {
		return "review failed agent-cycle phases before trusting the cycle result"
	}
	if len(result.PursuitDecisions) > 0 {
		return "review pursuit decisions"
	}
	if result.PursuitBrief != nil {
		if result.PursuitBrief.NeedsRobert > 0 {
			return "review pursuit decisions"
		}
		if result.PursuitBrief.PlanningNeeded > 0 {
			return "create first workflow plans for pursuits"
		}
		if result.PursuitBrief.ReviewDue > 0 {
			return "review due pursuits"
		}
		if result.PursuitBrief.Stuck > 0 {
			return "clear stuck pursuits"
		}
	}
	if result.Dashboard != nil {
		if len(result.Dashboard.ApprovalItems) > 0 {
			return "review approval queue"
		}
		if len(result.Dashboard.BlockedItems) > 0 {
			return "clear blocked workflows"
		}
		if len(result.Dashboard.DueOpenLoops) > 0 {
			return "review due follow-ups"
		}
	}
	if result.AmbientScan != nil && result.AmbientScan.OpportunitiesFound > 0 {
		return "review ambient opportunities"
	}
	return "no immediate human action; continue scheduled monitoring"
}

func pursuitDecisionSummary(decisions []pursuit.PursuitDashboardDecision) string {
	if len(decisions) == 0 {
		return "no Robert-only pursuit decisions waiting"
	}
	first := decisions[0]
	return fmt.Sprintf("%d Robert decision(s); first: %s", len(decisions), firstNonEmpty(first.Decision.Recommended, first.NextAction, first.Pursuit.Title))
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func sourceSummary(run *source.ScheduledSyncRun) string {
	if run == nil {
		return "no source sync result"
	}
	return fmt.Sprintf("checked %d, due %d, completed %d, failed %d, skipped %d", run.Checked, run.Due, run.Completed, run.Failed, run.Skipped)
}

func recoverySummary(run *workflow.ClaimRecoverySummary) string {
	if run == nil {
		return "no stale claim recovery result"
	}
	return fmt.Sprintf("checked %d, workflows blocked %d, open loops reopened %d, skipped %d", run.Checked, run.WorkflowsBlocked, run.OpenLoopsReopened, run.Skipped)
}

func openLoopSummary(run *workflow.OpenLoopRunSummary) string {
	if run == nil {
		return "no open-loop run result"
	}
	return fmt.Sprintf("checked %d, triggered %d, resolved %d, skipped %d", run.Checked, run.Triggered, run.Resolved, run.Skipped)
}

func workflowSummary(run *workflow.WorkflowRunSummary) string {
	if run == nil {
		return "no workflow run result"
	}
	return fmt.Sprintf("checked %d, completed %d, retried %d, blocked %d, skipped %d", run.Checked, run.Completed, run.Retried, run.Blocked, run.Skipped)
}

func ambientSummary(scan *models.AmbientScan) string {
	if scan == nil {
		return "no ambient scan result"
	}
	return fmt.Sprintf("examined %d, opportunities %d, created %d, updated %d, filtered %d", scan.ItemsExamined, scan.OpportunitiesFound, scan.Created, scan.Updated, scan.Filtered)
}

func dashboardSummary(dashboard *workflow.WorkflowDashboard) string {
	if dashboard == nil {
		return "no dashboard result"
	}
	parts := []string{
		fmt.Sprintf("approvals %d", len(dashboard.ApprovalItems)),
		fmt.Sprintf("blocked %d", len(dashboard.BlockedItems)),
		fmt.Sprintf("ready %d", len(dashboard.ReadyItems)),
		fmt.Sprintf("due follow-ups %d", len(dashboard.DueOpenLoops)),
	}
	return strings.Join(parts, ", ")
}

func pursuitBriefSummary(brief *pursuit.Brief) string {
	if brief == nil {
		return "no pursuit operating brief"
	}
	return fmt.Sprintf("mode %s, Robert %d, planning %d, review due %d, stuck %d, ready %d", brief.OperatingMode, brief.NeedsRobert, brief.PlanningNeeded, brief.ReviewDue, brief.Stuck, brief.ReadyToMove)
}

func pursuitOperatingState(brief *pursuit.Brief) *PursuitOperatingState {
	if brief == nil {
		return nil
	}
	state := &PursuitOperatingState{
		OperatingMode:        brief.OperatingMode,
		PrimaryAction:        brief.PrimaryAction,
		NeedsRobert:          brief.NeedsRobert,
		ReadyToMove:          brief.ReadyToMove,
		Stuck:                brief.Stuck,
		ReviewDue:            brief.ReviewDue,
		PlanningNeeded:       brief.PlanningNeeded,
		CompletionCandidates: brief.CompletionCandidates,
		RecentlyChanged:      brief.RecentlyChanged,
	}
	state.AttentionTotal = state.NeedsRobert + state.Stuck + state.ReviewDue + state.PlanningNeeded + state.CompletionCandidates
	state.PrimaryLane = pursuitPrimaryLane(state)
	if strings.TrimSpace(state.PrimaryAction) == "" {
		state.PrimaryAction = pursuitPrimaryAction(state)
	}
	return state
}

func pursuitPrimaryLane(state *PursuitOperatingState) string {
	switch {
	case state == nil:
		return ""
	case state.NeedsRobert > 0:
		return "robert"
	case state.PlanningNeeded > 0:
		return "planning"
	case state.ReviewDue > 0:
		return "review"
	case state.Stuck > 0:
		return "stuck"
	case state.CompletionCandidates > 0:
		return "completion"
	case state.ReadyToMove > 0:
		return "ready"
	default:
		return "monitor"
	}
}

func pursuitPrimaryAction(state *PursuitOperatingState) string {
	switch pursuitPrimaryLane(state) {
	case "robert":
		return "Review Robert-only pursuit decisions."
	case "planning":
		return "Create first workflow plans for pursuits."
	case "review":
		return "Review due pursuits."
	case "stuck":
		return "Clear stuck pursuits."
	case "completion":
		return "Review completion candidates."
	case "ready":
		return "Move VA-ready and system-ready pursuit work."
	default:
		return "Continue scheduled pursuit monitoring."
	}
}

func operationalLessonUseful(result *RunResult) bool {
	if result == nil {
		return false
	}
	if len(result.Errors) > 0 {
		return true
	}
	if result.SourceSync != nil && result.SourceSync.Failed > 0 {
		return true
	}
	if result.Recovery != nil && (result.Recovery.WorkflowsBlocked > 0 || result.Recovery.OpenLoopsReopened > 0) {
		return true
	}
	if result.Workflows != nil && (result.Workflows.Blocked > 0 || result.Workflows.Retried > 0) {
		return true
	}
	if result.AmbientScan != nil && result.AmbientScan.Blocked > 0 {
		return true
	}
	if result.PursuitBrief != nil && (result.PursuitBrief.NeedsRobert > 0 || result.PursuitBrief.PlanningNeeded > 0 || result.PursuitBrief.ReviewDue > 0 || result.PursuitBrief.Stuck > 0) {
		return true
	}
	return false
}

func operationalMemoryRelevant(memory models.ContextMemory) bool {
	kind := strings.ToLower(strings.TrimSpace(memory.Kind))
	if kind == "procedural" || kind == "lesson" {
		return true
	}
	tags := strings.ToLower(memory.Tags)
	return strings.Contains(tags, "agent-cycle") ||
		strings.Contains(tags, "workflow-retry") ||
		strings.Contains(tags, "blocked-workflow") ||
		strings.Contains(tags, "claim-recovery") ||
		strings.Contains(tags, "source-sync")
}

func appliedContextSummary(context []memory.RankedMemory) string {
	if len(context) == 0 {
		return "no prior operational lessons matched this cycle"
	}
	summaries := make([]string, 0, len(context))
	for _, item := range context {
		summary := strings.TrimSpace(firstNonEmpty(item.Memory.Summary, item.Memory.Content))
		if summary == "" {
			continue
		}
		summaries = append(summaries, compactText(summary, 90))
	}
	if len(summaries) == 0 {
		return fmt.Sprintf("loaded %d prior operational lesson(s)", len(context))
	}
	return fmt.Sprintf("loaded %d prior operational lesson(s): %s", len(context), strings.Join(summaries, "; "))
}

func operationalLessonContent(result *RunResult) string {
	parts := []string{
		"HAI agent cycle observed an operational exception that should inform future orchestration.",
		"Trigger: " + result.Trigger + ".",
		"Status: " + result.Status + ".",
	}
	if len(result.Errors) > 0 {
		errorParts := make([]string, 0, len(result.Errors))
		for _, err := range result.Errors {
			errorParts = append(errorParts, err.Phase+": "+err.Message)
		}
		parts = append(parts, "Failed phases: "+compactText(strings.Join(errorParts, "; "), 600)+".")
	}
	if result.SourceSync != nil && result.SourceSync.Failed > 0 {
		parts = append(parts, fmt.Sprintf("Source sync failures: %d of %d checked.", result.SourceSync.Failed, result.SourceSync.Checked))
	}
	if result.Recovery != nil && (result.Recovery.WorkflowsBlocked > 0 || result.Recovery.OpenLoopsReopened > 0) {
		parts = append(parts, fmt.Sprintf("Recovered stale claims: %d workflows blocked and %d open loops reopened.", result.Recovery.WorkflowsBlocked, result.Recovery.OpenLoopsReopened))
	}
	if result.Workflows != nil && (result.Workflows.Blocked > 0 || result.Workflows.Retried > 0) {
		parts = append(parts, fmt.Sprintf("Workflow worker outcomes: %d blocked and %d retry scheduled.", result.Workflows.Blocked, result.Workflows.Retried))
	}
	if result.AmbientScan != nil && result.AmbientScan.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("Ambient execution blocked %d opportunities.", result.AmbientScan.Blocked))
	}
	if result.PursuitBrief != nil {
		parts = append(parts, "Pursuit operating brief: "+pursuitBriefSummary(result.PursuitBrief)+".")
	}
	if result.NextAction != "" {
		parts = append(parts, "Next operator action: "+result.NextAction+".")
	}
	parts = append(parts, "Future behavior: prioritize the failing phase, preserve approval gates, and avoid treating partial cycle progress as verified completion.")
	return strings.Join(parts, " ")
}

func operationalLessonSummary(result *RunResult) string {
	switch {
	case result == nil:
		return ""
	case len(result.Errors) > 0:
		return "Agent cycle partial failure: " + result.Errors[0].Phase
	case result.Workflows != nil && result.Workflows.Blocked > 0:
		return fmt.Sprintf("Agent cycle found %d blocked workflow(s)", result.Workflows.Blocked)
	case result.Workflows != nil && result.Workflows.Retried > 0:
		return fmt.Sprintf("Agent cycle scheduled %d workflow retry/retries", result.Workflows.Retried)
	case result.Recovery != nil && result.Recovery.WorkflowsBlocked > 0:
		return fmt.Sprintf("Agent cycle recovered %d stale workflow claim(s)", result.Recovery.WorkflowsBlocked)
	case result.SourceSync != nil && result.SourceSync.Failed > 0:
		return fmt.Sprintf("Agent cycle saw %d source sync failure(s)", result.SourceSync.Failed)
	case result.PursuitBrief != nil && result.PursuitBrief.NeedsRobert > 0:
		return fmt.Sprintf("Agent cycle found %d pursuit decision(s) for Robert", result.PursuitBrief.NeedsRobert)
	case result.PursuitBrief != nil && result.PursuitBrief.PlanningNeeded > 0:
		return fmt.Sprintf("Agent cycle found %d pursuit(s) needing first plan", result.PursuitBrief.PlanningNeeded)
	case result.PursuitBrief != nil && result.PursuitBrief.Stuck > 0:
		return fmt.Sprintf("Agent cycle found %d stuck pursuit(s)", result.PursuitBrief.Stuck)
	default:
		return "Agent cycle operational lesson"
	}
}

func operationalLessonTags(result *RunResult) []string {
	tags := []string{"agent-cycle", "procedural", result.Status}
	if len(result.Errors) > 0 {
		tags = append(tags, "phase-failure")
	}
	if result.SourceSync != nil && result.SourceSync.Failed > 0 {
		tags = append(tags, "source-sync")
	}
	if result.Workflows != nil && result.Workflows.Blocked > 0 {
		tags = append(tags, "blocked-workflow")
	}
	if result.Workflows != nil && result.Workflows.Retried > 0 {
		tags = append(tags, "workflow-retry")
	}
	if result.Recovery != nil && (result.Recovery.WorkflowsBlocked > 0 || result.Recovery.OpenLoopsReopened > 0) {
		tags = append(tags, "claim-recovery")
	}
	if result.PursuitBrief != nil {
		if result.PursuitBrief.NeedsRobert > 0 {
			tags = append(tags, "pursuit-decision")
		}
		if result.PursuitBrief.PlanningNeeded > 0 {
			tags = append(tags, "pursuit-planning")
		}
		if result.PursuitBrief.ReviewDue > 0 {
			tags = append(tags, "pursuit-review")
		}
		if result.PursuitBrief.Stuck > 0 {
			tags = append(tags, "stuck-pursuit")
		}
	}
	return tags
}

func operationalLessonConfidence(result *RunResult) float64 {
	if result == nil {
		return 0.55
	}
	if len(result.Errors) > 0 {
		return 0.78
	}
	if result.Workflows != nil && result.Workflows.Blocked > 0 {
		return 0.74
	}
	if result.Recovery != nil && (result.Recovery.WorkflowsBlocked > 0 || result.Recovery.OpenLoopsReopened > 0) {
		return 0.72
	}
	return 0.68
}

func compactText(value string, maxLength int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxLength <= 0 || len(value) <= maxLength {
		return value
	}
	if maxLength <= 3 {
		return value[:maxLength]
	}
	return value[:maxLength-3] + "..."
}

func firstMemoryService(services ...memory.Service) memory.Service {
	for _, service := range services {
		if service != nil {
			return service
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

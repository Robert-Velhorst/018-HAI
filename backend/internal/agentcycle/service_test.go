package agentcycle

import (
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

type fakeSourceSyncer struct {
	calls  *[]string
	err    error
	cancel context.CancelFunc
}

type globalOnlySourceSyncer struct {
	calls *[]string
}

func (s globalOnlySourceSyncer) RunDueScheduledSyncs(time.Time) (*source.ScheduledSyncRun, error) {
	*s.calls = append(*s.calls, "global-source")
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func (s fakeSourceSyncer) RunDueScheduledSyncs(now time.Time) (*source.ScheduledSyncRun, error) {
	*s.calls = append(*s.calls, "source")
	if s.err != nil {
		return nil, s.err
	}
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func (s fakeSourceSyncer) RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*source.ScheduledSyncRun, error) {
	*s.calls = append(*s.calls, "source:"+ownerIdentity)
	if s.err != nil {
		return nil, s.err
	}
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func (s fakeSourceSyncer) RunDueScheduledSyncsForOwnerContext(ctx context.Context, now time.Time, ownerIdentity string) (*source.ScheduledSyncRun, error) {
	*s.calls = append(*s.calls, "source:"+ownerIdentity)
	if s.cancel != nil {
		s.cancel()
	}
	if err := ctx.Err(); err != nil {
		return &source.ScheduledSyncRun{Checked: 1}, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return &source.ScheduledSyncRun{Checked: 1, Due: 1, Completed: 1}, nil
}

func TestRunContextStopsAfterCancelledSourceBatch(t *testing.T) {
	calls := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls, cancel: cancel},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	)

	result, err := service.RunContext(ctx, RunRequest{OwnerIdentity: "alice", Trigger: "cancel-proof"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext error = %v, want context canceled", err)
	}
	if result == nil || result.Status != "interrupted" {
		t.Fatalf("result = %#v, want interrupted partial result", result)
	}
	if got, want := fmt.Sprint(calls), "[recover:alice source:alice]"; got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
	if result.SourceSync == nil || result.SourceSync.Checked != 1 {
		t.Fatalf("partial source result = %#v, want retained settled batch summary", result.SourceSync)
	}
}

type fakeWorkflowCoordinator struct {
	calls   *[]string
	errAt   string
	blocked int
	retried int
}

func (w fakeWorkflowCoordinator) RecoverStaleClaims(request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error) {
	*w.calls = append(*w.calls, "recover")
	if w.errAt == "recover" {
		return nil, fmt.Errorf("recover failed")
	}
	return &workflow.ClaimRecoverySummary{Checked: 1}, nil
}

func (w fakeWorkflowCoordinator) RunDueOpenLoops(request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	*w.calls = append(*w.calls, "open-loops")
	if w.errAt == "open-loops" {
		return nil, fmt.Errorf("open loops failed")
	}
	return &workflow.OpenLoopRunSummary{Checked: 1, Triggered: 1}, nil
}

func (w fakeWorkflowCoordinator) RunDue(request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	*w.calls = append(*w.calls, "workflows")
	if w.errAt == "workflows" {
		return nil, fmt.Errorf("workflows failed")
	}
	return &workflow.WorkflowRunSummary{Checked: 1, Completed: 1, Blocked: w.blocked, Retried: w.retried}, nil
}

func (w fakeWorkflowCoordinator) Dashboard() (*workflow.WorkflowDashboard, error) {
	*w.calls = append(*w.calls, "dashboard")
	if w.errAt == "dashboard" {
		return nil, fmt.Errorf("dashboard failed")
	}
	return &workflow.WorkflowDashboard{Counts: map[string]int64{"ready": 0}}, nil
}

func (w fakeWorkflowCoordinator) RecoverStaleClaimsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error) {
	*w.calls = append(*w.calls, "recover:"+ownerIdentity)
	if w.errAt == "recover" {
		return nil, fmt.Errorf("recover failed")
	}
	return &workflow.ClaimRecoverySummary{Checked: 1}, nil
}

func (w fakeWorkflowCoordinator) RunDueOpenLoopsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	*w.calls = append(*w.calls, "open-loops:"+ownerIdentity)
	if w.errAt == "open-loops" {
		return nil, fmt.Errorf("open loops failed")
	}
	return &workflow.OpenLoopRunSummary{Checked: 1, Triggered: 1}, nil
}

func (w fakeWorkflowCoordinator) RunDueForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	*w.calls = append(*w.calls, "workflows:"+ownerIdentity)
	if w.errAt == "workflows" {
		return nil, fmt.Errorf("workflows failed")
	}
	return &workflow.WorkflowRunSummary{Checked: 1, Completed: 1, Blocked: w.blocked, Retried: w.retried}, nil
}

func (w fakeWorkflowCoordinator) DashboardForOwner(ownerIdentity string) (*workflow.WorkflowDashboard, error) {
	*w.calls = append(*w.calls, "dashboard:"+ownerIdentity)
	if w.errAt == "dashboard" {
		return nil, fmt.Errorf("dashboard failed")
	}
	return &workflow.WorkflowDashboard{Counts: map[string]int64{"ready": 0}}, nil
}

type fakeAmbientScanner struct {
	calls *[]string
	err   error
}

func (a fakeAmbientScanner) Scan(trigger string) (*models.AmbientScan, error) {
	*a.calls = append(*a.calls, "ambient:"+trigger)
	if a.err != nil {
		return nil, a.err
	}
	return &models.AmbientScan{Trigger: trigger, Status: "completed", ItemsExamined: 1, OpportunitiesFound: 1, Created: 1}, nil
}

func (a fakeAmbientScanner) ScanForOwner(ownerIdentity, trigger string) (*models.AmbientScan, error) {
	*a.calls = append(*a.calls, "ambient:"+ownerIdentity+":"+trigger)
	if a.err != nil {
		return nil, a.err
	}
	return &models.AmbientScan{Trigger: trigger, Status: "completed", ItemsExamined: 1, OpportunitiesFound: 1, Created: 1}, nil
}

type fakeSnapshotAmbientScanner struct {
	calls         *[]string
	dashboard     *pursuit.Dashboard
	snapshotCalls int
	fallbackCalls int
}

func (a *fakeSnapshotAmbientScanner) Scan(trigger string) (*models.AmbientScan, error) {
	a.fallbackCalls++
	*a.calls = append(*a.calls, "ambient-global-fallback:"+trigger)
	return &models.AmbientScan{Trigger: trigger, Status: "completed"}, nil
}

func (a *fakeSnapshotAmbientScanner) ScanForOwner(ownerIdentity, trigger string) (*models.AmbientScan, error) {
	a.fallbackCalls++
	*a.calls = append(*a.calls, "ambient-owner-fallback:"+ownerIdentity+":"+trigger)
	return &models.AmbientScan{OwnerIdentity: ownerIdentity, Trigger: trigger, Status: "completed"}, nil
}

func (a *fakeSnapshotAmbientScanner) ScanForOwnerWithPursuitDashboard(ownerIdentity, trigger string, dashboard *pursuit.Dashboard) (*models.AmbientScan, error) {
	a.snapshotCalls++
	a.dashboard = dashboard
	*a.calls = append(*a.calls, "ambient-snapshot:"+ownerIdentity+":"+trigger)
	return &models.AmbientScan{OwnerIdentity: ownerIdentity, Trigger: trigger, Status: "completed"}, nil
}

type fakePursuitBriefProvider struct {
	calls       *[]string
	brief       *pursuit.Brief
	decisions   []pursuit.PursuitDashboardDecision
	err         error
	decisionErr error
}

type fakePursuitSnapshotProvider struct {
	fakePursuitBriefProvider
	dashboard *pursuit.Dashboard
}

func (p fakePursuitSnapshotProvider) OperatingSnapshot() (*pursuit.OperatingSnapshot, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-snapshot")
	}
	if p.err != nil {
		return nil, p.err
	}
	return &pursuit.OperatingSnapshot{Brief: p.snapshotBrief(), Decisions: p.decisions, Dashboard: p.dashboard}, p.decisionErr
}

func (p fakePursuitSnapshotProvider) OperatingSnapshotForOwner(ownerIdentity string) (*pursuit.OperatingSnapshot, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-snapshot:"+ownerIdentity)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &pursuit.OperatingSnapshot{Brief: p.snapshotBrief(), Decisions: p.decisions, Dashboard: p.dashboard}, p.decisionErr
}

func (p fakePursuitSnapshotProvider) snapshotBrief() *pursuit.Brief {
	if p.brief != nil {
		return p.brief
	}
	return &pursuit.Brief{OperatingMode: "steady", Summary: "no pursuit pressure"}
}

func (p fakePursuitBriefProvider) Brief() (*pursuit.Brief, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-brief")
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.brief != nil {
		return p.brief, nil
	}
	return &pursuit.Brief{OperatingMode: "steady", Summary: "no pursuit pressure"}, nil
}

func (p fakePursuitBriefProvider) Decisions() ([]pursuit.PursuitDashboardDecision, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-decisions")
	}
	if p.decisionErr != nil {
		return nil, p.decisionErr
	}
	return p.decisions, nil
}

func (p fakePursuitBriefProvider) BriefForOwner(ownerIdentity string) (*pursuit.Brief, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-brief:"+ownerIdentity)
	}
	return p.Brief()
}

func (p fakePursuitBriefProvider) DecisionsForOwner(ownerIdentity string) ([]pursuit.PursuitDashboardDecision, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "pursuit-decisions:"+ownerIdentity)
	}
	return p.Decisions()
}

func TestAgentCycleRunsEnginesInOperationalOrder(t *testing.T) {
	calls := []string{}
	service := NewService(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
	)

	result := service.Run(RunRequest{Trigger: "test", Limit: 3})

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed: %#v", result.Status, result.Errors)
	}
	want := []string{"recover", "source", "open-loops", "workflows", "ambient:agent-cycle.test", "dashboard"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if result.SourceSync == nil || result.Workflows == nil || result.AmbientScan == nil || result.Dashboard == nil {
		t.Fatalf("cycle missed phase result: %#v", result)
	}
	if result.NextAction == "" || result.SafetySummary == "" {
		t.Fatalf("cycle did not return operator guidance: %#v", result)
	}
}

func TestAgentCycleRefreshesPursuitBriefAndPrioritizesRobertDecisions(t *testing.T) {
	calls := []string{}
	mem := &fakeCycleMemoryService{calls: &calls}
	decisionPursuitID := uuid.New()
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitSnapshotProvider{fakePursuitBriefProvider: fakePursuitBriefProvider{
			calls: &calls,
			brief: &pursuit.Brief{
				OperatingMode:  "needs_robert",
				Summary:        "2 active pursuits need Robert.",
				PrimaryAction:  "Review pursuit decisions.",
				NeedsRobert:    2,
				PlanningNeeded: 1,
				ReadyToMove:    1,
				Stuck:          1,
			},
			decisions: []pursuit.PursuitDashboardDecision{
				{
					Pursuit: models.Pursuit{
						ID:    decisionPursuitID,
						Title: "OpenClaw recovery",
					},
					Decision: pursuit.PursuitDecision{
						ID:               "pursuit:" + decisionPursuitID.String() + ":next-action",
						DecisionType:     "pursuit_next_action",
						Status:           "pending",
						Recommended:      "Approve recovery workflow.",
						YesLabel:         "Create workflow",
						NoLabel:          "Revise goal",
						RequiresApproval: true,
					},
					NextAction: "Approve recovery workflow.",
				},
			},
		}},
		mem,
	)

	result := service.Run(RunRequest{Trigger: "pursuit-check", Limit: 3})

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed: %#v", result.Status, result.Errors)
	}
	want := []string{"memory-retrieve", "recover", "source", "open-loops", "workflows", "ambient:agent-cycle.pursuit-check", "dashboard", "pursuit-snapshot"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if result.PursuitBrief == nil || result.PursuitBrief.NeedsRobert != 2 {
		t.Fatalf("pursuit brief = %#v, want Robert decision count", result.PursuitBrief)
	}
	if result.PursuitState == nil || result.PursuitState.PrimaryLane != "robert" || result.PursuitState.AttentionTotal != 4 {
		t.Fatalf("pursuit operating state = %#v, want structured Robert lane with attention total", result.PursuitState)
	}
	if result.PursuitState.PrimaryAction != "Review pursuit decisions." {
		t.Fatalf("pursuit primary action = %q", result.PursuitState.PrimaryAction)
	}
	if len(result.PursuitDecisions) != 1 || result.PursuitDecisions[0].Decision.Recommended != "Approve recovery workflow." {
		t.Fatalf("pursuit decisions = %#v, want top Robert decision", result.PursuitDecisions)
	}
	if result.NextAction != "review pursuit decisions" {
		t.Fatalf("next action = %q, want pursuit decision priority", result.NextAction)
	}
	if len(mem.created) != 1 || !containsTag(mem.created[0].Tags, "pursuit-decision") || !containsTag(mem.created[0].Tags, "pursuit-planning") {
		t.Fatalf("stored pursuit lesson = %#v, want pursuit tags", mem.created)
	}
	if !strings.Contains(mem.created[0].Content, "Pursuit operating brief") {
		t.Fatalf("lesson content = %q, want pursuit operating brief", mem.created[0].Content)
	}
}

func TestAuthenticatedAgentCycleRunsEveryOwnerScopedOperationalPhase(t *testing.T) {
	calls := []string{}
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitSnapshotProvider{fakePursuitBriefProvider: fakePursuitBriefProvider{
			calls:     &calls,
			brief:     &pursuit.Brief{OperatingMode: "needs_robert", NeedsRobert: 1, PrimaryAction: "Review Alice decision."},
			decisions: []pursuit.PursuitDashboardDecision{{Decision: pursuit.PursuitDecision{Recommended: "Review Alice decision."}}},
		}},
	)

	result := service.Run(RunRequest{OwnerIdentity: "alice", Trigger: "control-center"})

	if result.Status != "completed" || result.ExecutionScope != "owner_scoped" {
		t.Fatalf("owner cycle status/scope = %s/%s, want completed/owner_scoped: %#v", result.Status, result.ExecutionScope, result.Errors)
	}
	if result.SourceSync == nil || result.Recovery == nil || result.OpenLoops == nil || result.Workflows == nil || result.AmbientScan == nil || result.Dashboard == nil {
		t.Fatalf("owner cycle missed owner-scoped operational output: %#v", result)
	}
	if result.PursuitBrief == nil || len(result.PursuitDecisions) != 1 || result.NextAction != "review pursuit decisions" {
		t.Fatalf("owner pursuit state = %#v", result)
	}
	want := "[recover:alice source:alice open-loops:alice workflows:alice ambient:alice:agent-cycle.control-center dashboard:alice pursuit-snapshot:alice]"
	if got := fmt.Sprint(calls); got != want {
		t.Fatalf("owner cycle calls = %s, want only owner-scoped engines %s", got, want)
	}
	if result.LearningNote == "" || len(result.LearningIDs) != 0 {
		t.Fatalf("owner cycle stored shared learning: %#v", result)
	}
}

func TestAuthenticatedAgentCycleReusesOnePursuitDashboardForAmbientAndBrief(t *testing.T) {
	calls := []string{}
	dashboard := &pursuit.Dashboard{
		NeedsRobert: []pursuit.PursuitListItem{{
			Pursuit: models.Pursuit{ID: uuid.New(), OwnerIdentity: "alice", Title: "Review legal response"},
		}},
	}
	ambient := &fakeSnapshotAmbientScanner{calls: &calls}
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		ambient,
		fakePursuitSnapshotProvider{
			fakePursuitBriefProvider: fakePursuitBriefProvider{
				calls: &calls,
				brief: &pursuit.Brief{OperatingMode: "needs_robert", NeedsRobert: 1, PrimaryAction: "Review legal response."},
				decisions: []pursuit.PursuitDashboardDecision{{
					Pursuit:  dashboard.NeedsRobert[0].Pursuit,
					Decision: pursuit.PursuitDecision{Recommended: "Review legal response."},
				}},
			},
			dashboard: dashboard,
		},
	)

	result := service.Run(RunRequest{OwnerIdentity: "alice", Trigger: "reuse-proof"})

	if result.Status != "completed" || result.AmbientScan == nil || result.PursuitBrief == nil || len(result.PursuitDecisions) != 1 {
		t.Fatalf("optimized owner cycle result = %#v", result)
	}
	if ambient.snapshotCalls != 1 || ambient.fallbackCalls != 0 {
		t.Fatalf("ambient calls = snapshot %d fallback %d, want 1/0", ambient.snapshotCalls, ambient.fallbackCalls)
	}
	if ambient.dashboard != dashboard {
		t.Fatalf("ambient received dashboard %p, want snapshot dashboard %p", ambient.dashboard, dashboard)
	}
	want := "[recover:alice source:alice open-loops:alice workflows:alice pursuit-snapshot:alice ambient-snapshot:alice:agent-cycle.reuse-proof dashboard:alice]"
	if got := fmt.Sprint(calls); got != want {
		t.Fatalf("optimized owner cycle calls = %s, want %s", got, want)
	}
}

func TestAuthenticatedAgentCycleNeverFallsBackToGlobalSourceSync(t *testing.T) {
	calls := []string{}
	service := NewServiceWithPursuits(
		globalOnlySourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	)

	result := service.Run(RunRequest{OwnerIdentity: "alice", Trigger: "scope-check"})

	if result.Status != "partial_failure" || result.SourceSync != nil {
		t.Fatalf("global-only source sync was not rejected: %#v", result)
	}
	if got := fmt.Sprint(calls); strings.Contains(got, "global-source") {
		t.Fatalf("owner cycle invoked a global source method: %s", got)
	}
	found := false
	for _, phaseErr := range result.Errors {
		if phaseErr.Phase == "source_sync" && strings.Contains(phaseErr.Message, "owner-scoped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing fail-closed owner scope error: %#v", result.Errors)
	}
}

func TestAuthenticatedAgentCycleHonorsOwnerScopedSkipControls(t *testing.T) {
	calls := []string{}
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
	)

	result := service.Run(RunRequest{OwnerIdentity: "alice", Trigger: "brief-only", SkipSourceSync: true, SkipAmbient: true})

	if result.Status != "completed" || result.SourceSync != nil || result.AmbientScan != nil {
		t.Fatalf("owner skip controls were not honored: %#v", result)
	}
	if got := fmt.Sprint(calls); strings.Contains(got, "source") || strings.Contains(got, "ambient") {
		t.Fatalf("skipped owner engines were called: %s", got)
	}
	for _, name := range []string{"sync due sources", "scan ambient opportunities"} {
		found := false
		for _, step := range result.Steps {
			if step.Name == name && step.Status == "skipped" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing explicit skipped step %q: %#v", name, result.Steps)
		}
	}
}

func TestAuthenticatedAgentCycleStoresOnlyOwnerScopedOperationalLearning(t *testing.T) {
	calls := []string{}
	mem := &fakeCycleMemoryService{calls: &calls}
	service := NewServiceWithPursuits(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls, retried: 1},
		fakeAmbientScanner{calls: &calls},
		fakePursuitBriefProvider{calls: &calls},
		mem,
	)

	result := service.Run(RunRequest{OwnerIdentity: "alice", Trigger: "learning-proof"})

	if result.Status != "completed" || len(result.LearningIDs) != 1 {
		t.Fatalf("owner learning was not recorded: %#v", result)
	}
	if len(mem.createdOwners) != 1 || mem.createdOwners[0] != "alice" || len(mem.created) != 1 {
		t.Fatalf("learning scope = owners %#v, records %#v", mem.createdOwners, mem.created)
	}
	if mem.created[0].SourceLabel != "Owner-scoped agent cycle operational learning" {
		t.Fatalf("owner learning provenance = %#v", mem.created[0])
	}
}

func TestAgentCycleContinuesAfterSourceFailure(t *testing.T) {
	calls := []string{}
	service := NewService(
		fakeSourceSyncer{calls: &calls, err: fmt.Errorf("source unavailable")},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
	)

	result := service.Run(RunRequest{Trigger: "partial"})

	if result.Status != "partial_failure" {
		t.Fatalf("status = %q, want partial_failure", result.Status)
	}
	if len(result.Errors) != 1 || result.Errors[0].Phase != "sync due sources" {
		t.Fatalf("errors = %#v, want source phase error", result.Errors)
	}
	want := []string{"recover", "source", "open-loops", "workflows", "ambient:agent-cycle.partial", "dashboard"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %#v, want cycle to continue through safe phases %#v", calls, want)
	}
}

func TestAgentCycleLoadsOperationalLessonsBeforeRunningEngines(t *testing.T) {
	calls := []string{}
	mem := &fakeCycleMemoryService{
		calls: &calls,
		retrieveResult: &memory.RetrieveResult{
			UsedContext: []memory.RankedMemory{
				{
					Memory: models.ContextMemory{
						ID:      uuid.New(),
						Kind:    "procedural",
						Summary: "When workflow retries are high, inspect approval gates before running more work.",
						Tags:    "agent-cycle,workflow-retry",
					},
					Score:       0.9,
					Explanation: "same operational pattern",
				},
				{
					Memory: models.ContextMemory{
						ID:      uuid.New(),
						Kind:    "preference",
						Summary: "Unrelated user preference",
						Tags:    "profile",
					},
					Score:       0.8,
					Explanation: "weak match",
				},
			},
		},
	}
	service := NewService(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		mem,
	)

	result := service.Run(RunRequest{Trigger: "retry-check"})

	if len(result.AppliedContext) != 1 {
		t.Fatalf("applied context = %#v, want only relevant procedural lesson", result.AppliedContext)
	}
	if result.ContextNote == "" || !strings.Contains(result.ContextNote, "workflow retries are high") {
		t.Fatalf("context note = %q, want operational lesson summary", result.ContextNote)
	}
	if len(calls) < 2 || calls[0] != "memory-retrieve" || calls[1] != "recover" {
		t.Fatalf("calls = %#v, want memory retrieval before workflow engines", calls)
	}
}

func TestAgentCycleStoresOperationalLessonForBlockedWork(t *testing.T) {
	calls := []string{}
	mem := &fakeCycleMemoryService{}
	service := NewService(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls, blocked: 2, retried: 1},
		fakeAmbientScanner{calls: &calls},
		mem,
	)

	result := service.Run(RunRequest{Trigger: "blocked-check"})

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if len(result.LearningIDs) != 1 || result.LearningNote == "" {
		t.Fatalf("learning result = %#v, want stored memory", result)
	}
	if len(mem.created) != 1 {
		t.Fatalf("stored %d memories, want 1", len(mem.created))
	}
	created := mem.created[0]
	if created.Kind != "procedural" || !strings.Contains(created.Content, "Workflow worker outcomes: 2 blocked and 1 retry scheduled") {
		t.Fatalf("memory did not capture blocked workflow lesson: %#v", created)
	}
	if !containsTag(created.Tags, "blocked-workflow") || !containsTag(created.Tags, "workflow-retry") {
		t.Fatalf("tags = %#v, want blocked and retry tags", created.Tags)
	}
}

func TestOperationalLessonDoesNotRecursivelyStoreAppliedContext(t *testing.T) {
	result := &RunResult{
		Trigger: "command-center",
		Status:  "completed",
		AppliedContext: []memory.RankedMemory{{
			Memory: models.ContextMemory{
				Kind:    "procedural",
				Summary: "PRIOR LESSON SENTINEL that must not be copied into a new lesson.",
			},
		}},
		Workflows:  &workflow.WorkflowRunSummary{Checked: 1, Blocked: 1},
		NextAction: "review blocked workflow",
	}

	content := operationalLessonContent(result)
	if strings.Contains(content, "PRIOR LESSON SENTINEL") || strings.Contains(content, "prior operational lessons were loaded") {
		t.Fatalf("operational lesson recursively embedded retrieved context: %q", content)
	}
	if !strings.Contains(content, "Workflow worker outcomes: 1 blocked") {
		t.Fatalf("operational lesson lost the new material outcome: %q", content)
	}
}

func TestAgentCycleDoesNotStoreRoutineGreenCycle(t *testing.T) {
	calls := []string{}
	mem := &fakeCycleMemoryService{}
	service := NewService(
		fakeSourceSyncer{calls: &calls},
		fakeWorkflowCoordinator{calls: &calls},
		fakeAmbientScanner{calls: &calls},
		mem,
	)

	result := service.Run(RunRequest{Trigger: "routine"})

	if len(result.LearningIDs) != 0 || result.LearningNote != "" {
		t.Fatalf("routine cycle stored learning result: %#v", result)
	}
	if len(mem.created) != 0 {
		t.Fatalf("stored %d memories, want no routine cycle memory", len(mem.created))
	}
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

type fakeCycleMemoryService struct {
	calls          *[]string
	created        []memory.CreateRequest
	createdOwners  []string
	retrieveResult *memory.RetrieveResult
	retrieveErr    error
}

func (s *fakeCycleMemoryService) Create(request memory.CreateRequest) (*models.ContextMemory, error) {
	s.created = append(s.created, request)
	return &models.ContextMemory{ID: uuid.New(), Kind: request.Kind, Content: request.Content, Summary: request.Summary, Confidence: request.Confidence}, nil
}

func (s *fakeCycleMemoryService) Update(uuid.UUID, memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) FindAll(string, bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) FindByID(uuid.UUID) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) Archive(uuid.UUID, bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) Delete(uuid.UUID) error {
	return nil
}

func (s *fakeCycleMemoryService) Retrieve(memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, "memory-retrieve")
	}
	if s.retrieveErr != nil {
		return nil, s.retrieveErr
	}
	if s.retrieveResult != nil {
		return s.retrieveResult, nil
	}
	return &memory.RetrieveResult{}, nil
}

func (s *fakeCycleMemoryService) CreateForOwner(ownerIdentity string, request memory.CreateRequest) (*models.ContextMemory, error) {
	s.createdOwners = append(s.createdOwners, ownerIdentity)
	created, err := s.Create(request)
	if created != nil {
		created.OwnerIdentity = ownerIdentity
	}
	return created, err
}

func (s *fakeCycleMemoryService) UpdateForOwner(string, uuid.UUID, memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) FindAllForOwner(string, string, bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) FindByIDForOwner(string, uuid.UUID) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) ArchiveForOwner(string, uuid.UUID, bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeCycleMemoryService) DeleteForOwner(string, uuid.UUID) error {
	return nil
}

func (s *fakeCycleMemoryService) RetrieveForOwner(ownerIdentity string, request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, "memory-retrieve:"+ownerIdentity)
	}
	if s.retrieveErr != nil {
		return nil, s.retrieveErr
	}
	if s.retrieveResult != nil {
		return s.retrieveResult, nil
	}
	return &memory.RetrieveResult{}, nil
}

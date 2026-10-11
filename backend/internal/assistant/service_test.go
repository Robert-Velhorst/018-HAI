package assistant

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentcycle"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

func TestCommandRejectsMalformedStandingMandateBeforeAnyEngineCall(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	router := &fakePursuitCommandRouter{}
	service := NewService(tasks, cycle, router)

	result, err := service.Command(CommandRequest{
		Message:        "Plan a harmless local checklist.",
		MandateID:      "not-a-uuid",
		ExecuteAllowed: false,
		SkipSourceSync: true,
		SkipAmbient:    true,
	})
	if !errors.Is(err, ErrInvalidStandingMandateID) {
		t.Fatalf("Command error = %v, want %v", err, ErrInvalidStandingMandateID)
	}
	if result != nil {
		t.Fatalf("malformed mandate returned a result: %#v", result)
	}
	if tasks.planCalls != 0 || tasks.runCalls != 0 || cycle.calls != 0 ||
		router.matchCalls != 0 || router.routeCalls != 0 || router.intakeCalls != 0 {
		t.Fatalf("malformed mandate reached an engine: tasks=%#v cycle=%d router=%#v", tasks, cycle.calls, router)
	}
}

func TestCommandPlansAndRunsCycleForBlockerRequest(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	service := NewService(tasks, cycle)

	result, err := service.Command(CommandRequest{
		Message:        "Clear blockers and follow up on open loops for 018-HAI.",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.runCalls != 1 || tasks.planCalls != 0 {
		t.Fatalf("task calls plan=%d run=%d, want run only", tasks.planCalls, tasks.runCalls)
	}
	if cycle.calls != 1 {
		t.Fatalf("agent cycle calls = %d, want 1", cycle.calls)
	}
	if result.Plan == nil || result.AgentCycle == nil {
		t.Fatalf("result did not include both plan and cycle: %#v", result)
	}
	if result.Intent != "execute_and_cycle" {
		t.Fatalf("intent = %q, want execute_and_cycle", result.Intent)
	}
	if result.NextAction != "review approval queue" {
		t.Fatalf("next action = %q, want agent-cycle next action", result.NextAction)
	}
}

func TestCommandRoutesPursuitDecisionPromptThroughAgentCycle(t *testing.T) {
	tasks := &fakeTaskEngine{}
	pursuitID := uuid.New()
	cycle := &fakeAgentCycleRunner{
		result: &agentcycle.RunResult{
			Status:      "completed",
			StartedAt:   time.Now().UTC(),
			CompletedAt: time.Now().UTC(),
			PursuitState: &agentcycle.PursuitOperatingState{
				OperatingMode:  "decision_queue",
				PrimaryLane:    "robert",
				PrimaryAction:  "Review Robert-only pursuit decisions.",
				NeedsRobert:    2,
				AttentionTotal: 2,
			},
			PursuitDecisions: []pursuit.PursuitDashboardDecision{
				{
					Pursuit: models.Pursuit{
						ID:    pursuitID,
						Title: "OpenClaw recovery",
					},
					Decision: pursuit.PursuitDecision{
						ID:               "pursuit:" + pursuitID.String() + ":next-action",
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
			NextAction:    "Review Robert-only pursuit decisions.",
			SafetySummary: "approval gates remain enforced",
		},
	}
	service := NewService(tasks, cycle)

	result, err := service.Command(CommandRequest{
		Message:    "What pursuits need Robert right now?",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 1 || tasks.runCalls != 0 {
		t.Fatalf("task calls plan=%d run=%d, want plan only", tasks.planCalls, tasks.runCalls)
	}
	if cycle.calls != 1 {
		t.Fatalf("agent cycle calls = %d, want 1", cycle.calls)
	}
	if result.Intent != "plan_and_cycle" {
		t.Fatalf("intent = %q, want plan_and_cycle", result.Intent)
	}
	if result.AgentCycle == nil || result.AgentCycle.PursuitState == nil {
		t.Fatalf("result missing pursuit operating state: %#v", result.AgentCycle)
	}
	if !result.ReviewRequired {
		t.Fatalf("Robert-only pursuit decisions should require review: %#v", result)
	}
	if result.NextAction != "Review Robert-only pursuit decisions." {
		t.Fatalf("next action = %q", result.NextAction)
	}
	if !strings.Contains(result.Summary, "Robert-only pursuit decision") || !strings.Contains(result.Summary, "Approve recovery workflow.") {
		t.Fatalf("summary does not expose top pursuit decision: %q", result.Summary)
	}
}

func TestCommandPlanOnlyForOrdinaryRequest(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	service := NewService(tasks, cycle)

	result, err := service.Command(CommandRequest{
		Message:    "Plan the next implementation step for the dashboard.",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 1 || tasks.runCalls != 0 {
		t.Fatalf("task calls plan=%d run=%d, want plan only", tasks.planCalls, tasks.runCalls)
	}
	if cycle.calls != 0 {
		t.Fatalf("agent cycle calls = %d, want 0", cycle.calls)
	}
	if result.Plan == nil || result.AgentCycle != nil {
		t.Fatalf("unexpected result engines: %#v", result)
	}
	if result.Intent != "plan" {
		t.Fatalf("intent = %q, want plan", result.Intent)
	}
}

func TestCommandPlanningSuggestsPursuitsWithoutCreatingWorkflowWork(t *testing.T) {
	tasks := &fakeTaskEngine{}
	pursuitID := uuid.New()
	router := &fakePursuitCommandRouter{matches: []pursuit.MatchCandidate{{
		Pursuit: models.Pursuit{ID: pursuitID, Title: "Dashboard recovery"},
		Score:   0.82,
		Reasons: []string{"project key matches"},
	}}}
	service := NewService(tasks, nil, router)

	result, err := service.Command(CommandRequest{
		Message:    "Plan the next dashboard recovery step.",
		ProjectKey: "018-HAI",
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 1 || tasks.runCalls != 0 {
		t.Fatalf("task calls plan=%d run=%d, want plan only", tasks.planCalls, tasks.runCalls)
	}
	if router.matchCalls != 1 || router.routeCalls != 0 || router.intakeCalls != 0 {
		t.Fatalf("pursuit router calls match=%d route=%d intake=%d, want match only", router.matchCalls, router.routeCalls, router.intakeCalls)
	}
	if result.Pursuit == nil || result.Pursuit.Mode != "suggested" || len(result.Pursuit.Matches) != 1 {
		t.Fatalf("pursuit context = %#v", result.Pursuit)
	}
}

func TestCommandCandidateHandoffDoesNotCreateDirectTaskWork(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	pursuitID := uuid.New()
	router := &fakePursuitCommandRouter{routed: &pursuit.RoutedIntakeResult{
		Mode:             "candidate_created",
		CreatedCandidate: true,
		PursuitID:        pursuitID,
		Message:          "pursuit candidate created from source-derived workflow",
		Detail:           &pursuit.PursuitDetail{Pursuit: models.Pursuit{ID: pursuitID, Title: "Local runtime recovery"}},
	}}
	service := NewService(tasks, cycle, router)
	mandateID := uuid.NewString()

	result, err := service.Command(CommandRequest{
		Message:        "Run the local runtime recovery workflow safely.",
		ProjectKey:     "018-HAI",
		MandateID:      mandateID,
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 0 || tasks.runCalls != 0 {
		t.Fatalf("task calls plan=%d run=%d, want no direct work before candidate acceptance", tasks.planCalls, tasks.runCalls)
	}
	if router.routeCalls != 1 || router.lastRoute.SourceType != "assistant_command" || router.lastRoute.SourceID == "" {
		t.Fatalf("expected deduplicated assistant intake: %#v", router.lastRoute)
	}
	if router.lastRoute.MandateID != mandateID {
		t.Fatalf("assistant standing mandate was not forwarded to pursuit intake: %#v", router.lastRoute)
	}
	if cycle.calls != 0 {
		t.Fatalf("cycle calls = %d, want 0 because only an explicit cycle may process unrelated ready workflows", cycle.calls)
	}
	if result.Plan != nil {
		t.Fatalf("candidate handoff unexpectedly created task plan: %#v", result.Plan)
	}
	if !result.ReviewRequired {
		t.Fatalf("candidate handoff must require review: %#v", result)
	}
	if result.Pursuit == nil || !result.Pursuit.AwaitingAcceptance || result.Pursuit.ExecutionQueued || result.Pursuit.PursuitID != pursuitID.String() {
		t.Fatalf("pursuit context = %#v", result.Pursuit)
	}
}

func TestCommandQueuedWorkflowDoesNotCreateDuplicateDirectTaskPlan(t *testing.T) {
	tasks := &fakeTaskEngine{}
	pursuitID := uuid.New()
	router := &fakePursuitCommandRouter{routed: &pursuit.RoutedIntakeResult{
		Mode:      "matched_existing",
		Matched:   true,
		PursuitID: pursuitID,
		Message:   "assistant command was added to the matched pursuit workflow",
		Detail:    &pursuit.PursuitDetail{Pursuit: models.Pursuit{ID: pursuitID, Title: "Dashboard recovery"}},
	}}
	service := NewService(tasks, nil, router)

	result, err := service.Command(CommandRequest{
		Message:        "Run the dashboard recovery workflow safely.",
		ProjectKey:     "018-HAI",
		ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 0 || tasks.runCalls != 0 {
		t.Fatalf("queued workflow created duplicate direct task work: plan=%d run=%d", tasks.planCalls, tasks.runCalls)
	}
	if result.Plan != nil {
		t.Fatalf("queued workflow unexpectedly returned a direct task plan: %#v", result.Plan)
	}
	if result.Pursuit == nil || !result.Pursuit.ExecutionQueued || result.Pursuit.AwaitingAcceptance {
		t.Fatalf("workflow context = %#v", result.Pursuit)
	}
	if !strings.Contains(result.Summary, "governed workflow") || !strings.Contains(result.NextAction, "workflow") {
		t.Fatalf("workflow handoff was not explained: %#v", result)
	}
	if len(result.Actions) != 1 || result.Actions[0].Name != "pursuit workflow" {
		t.Fatalf("workflow handoff action = %#v", result.Actions)
	}
}

func TestCommandSelectedCandidateDoesNotCreatePlanOrWorkflow(t *testing.T) {
	tasks := &fakeTaskEngine{}
	pursuitID := uuid.New()
	router := &fakePursuitCommandRouter{
		detail: &pursuit.PursuitDetail{Pursuit: models.Pursuit{
			ID:               pursuitID,
			Title:            "Imported legal correspondence",
			SourceOfCreation: "source_pursuit_candidate",
		}},
	}
	service := NewService(tasks, nil, router)

	result, err := service.Command(CommandRequest{
		Message:   "Plan a response to the imported correspondence.",
		PursuitID: pursuitID.String(),
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 0 || tasks.runCalls != 0 || router.intakeCalls != 0 {
		t.Fatalf("candidate created task work: plan=%d run=%d intake=%d", tasks.planCalls, tasks.runCalls, router.intakeCalls)
	}
	if result.Pursuit == nil || !result.Pursuit.AwaitingAcceptance || result.Pursuit.PursuitID != pursuitID.String() {
		t.Fatalf("candidate context = %#v", result.Pursuit)
	}
	if !result.ReviewRequired || !strings.Contains(result.NextAction, "explicitly accept") {
		t.Fatalf("candidate result did not surface approval handoff: %#v", result)
	}
}

func TestCommandPlanningWithinSelectedPursuitDoesNotCreateWorkflow(t *testing.T) {
	tasks := &fakeTaskEngine{}
	pursuitID := uuid.New()
	router := &fakePursuitCommandRouter{}
	service := NewService(tasks, nil, router)

	result, err := service.Command(CommandRequest{
		Message:   "Plan the evidence review.",
		PursuitID: pursuitID.String(),
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if router.matchCalls != 0 || router.routeCalls != 0 || router.intakeCalls != 0 || router.detailCalls != 1 {
		t.Fatalf("planning should only validate the selected pursuit: match=%d route=%d intake=%d detail=%d", router.matchCalls, router.routeCalls, router.intakeCalls, router.detailCalls)
	}
	if result.Pursuit == nil || result.Pursuit.PursuitID != pursuitID.String() || result.Pursuit.Mode != "selected" {
		t.Fatalf("pursuit context = %#v", result.Pursuit)
	}
	if tasks.lastRequest.PursuitID != pursuitID.String() {
		t.Fatalf("task pursuit id = %q, want %q", tasks.lastRequest.PursuitID, pursuitID)
	}
}

func TestCommandCanRunCycleOnlyFromExplicitFlag(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	service := NewService(tasks, cycle)

	result, err := service.Command(CommandRequest{
		Message:  "Run an autonomous cycle.",
		RunCycle: true,
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if tasks.planCalls != 1 {
		t.Fatalf("explicit cycle should still create an auditable plan, planCalls=%d", tasks.planCalls)
	}
	if cycle.calls != 1 {
		t.Fatalf("agent cycle calls = %d, want 1", cycle.calls)
	}
	if result.SafetySummary == "" {
		t.Fatalf("expected safety summary")
	}
}

func TestCommandStoresBoundedRecentLogs(t *testing.T) {
	tasks := &fakeTaskEngine{}
	service := NewService(tasks, nil)

	for i := 0; i < 55; i++ {
		if _, err := service.Command(CommandRequest{Message: "Plan dashboard step"}); err != nil {
			t.Fatalf("Command %d: %v", i, err)
		}
	}

	logs := service.Logs()
	if len(logs) != 50 {
		t.Fatalf("logs = %d, want bounded 50", len(logs))
	}
	if logs[0].Summary == "" || logs[0].CreatedAt.IsZero() {
		t.Fatalf("newest log missing summary/timestamp: %#v", logs[0])
	}
}

func TestCommandScopesPursuitRoutingAndLogsToOwner(t *testing.T) {
	tasks := &fakeTaskEngine{}
	router := &fakePursuitCommandRouter{}
	service := NewService(tasks, nil, router)
	pursuitID := uuid.New()

	if _, err := service.Command(CommandRequest{Message: "Plan the evidence review.", PursuitID: pursuitID.String(), OwnerIdentity: "alice"}); err != nil {
		t.Fatalf("Alice command: %v", err)
	}
	if _, err := service.Command(CommandRequest{Message: "Plan the other evidence review.", OwnerIdentity: "bob"}); err != nil {
		t.Fatalf("Bob command: %v", err)
	}
	if router.lastOwner != "alice" {
		t.Fatalf("selected pursuit owner = %q, want alice", router.lastOwner)
	}
	if logs := service.LogsForOwner("alice"); len(logs) != 1 || logs[0].OwnerIdentity != "alice" {
		t.Fatalf("alice logs = %#v", logs)
	}
	if logs := service.LogsForOwner("bob"); len(logs) != 1 || logs[0].OwnerIdentity != "bob" {
		t.Fatalf("bob logs = %#v", logs)
	}
}

type fakeTaskEngine struct {
	planCalls   int
	runCalls    int
	lastRequest task.IntakeRequest
	planErr     error
	runErr      error
}

func (f *fakeTaskEngine) Plan(request task.IntakeRequest) (*task.CompletionPlan, error) {
	f.planCalls++
	f.lastRequest = request
	if f.planErr != nil {
		return nil, f.planErr
	}
	return fakePlan(request, "planned"), nil
}

func (f *fakeTaskEngine) Run(request task.IntakeRequest) (*task.CompletionPlan, error) {
	f.runCalls++
	f.lastRequest = request
	if f.runErr != nil {
		return nil, f.runErr
	}
	return fakePlan(request, "validated"), nil
}

func fakePlan(request task.IntakeRequest, status string) *task.CompletionPlan {
	return &task.CompletionPlan{
		ID:               "task-1",
		CreatedAt:        time.Now().UTC(),
		Request:          request.Request,
		ProjectKey:       request.ProjectKey,
		CompletionStatus: status,
		RiskAssessment: task.RiskAssessment{
			ApprovalRequired: false,
			AllowedNow:       request.ExecuteAllowed,
		},
		ValidationResult: task.ValidationResult{
			NextAction: "continue with validated task plan",
		},
	}
}

type fakeAgentCycleRunner struct {
	calls       int
	lastRequest agentcycle.RunRequest
	result      *agentcycle.RunResult
}

type fakePursuitCommandRouter struct {
	matchCalls  int
	routeCalls  int
	intakeCalls int
	detailCalls int
	matches     []pursuit.MatchCandidate
	routed      *pursuit.RoutedIntakeResult
	detail      *pursuit.PursuitDetail
	lastRoute   pursuit.IntakeRequest
	lastMatch   pursuit.MatchRequest
	lastOwner   string
}

func (f *fakePursuitCommandRouter) Match(request pursuit.MatchRequest) ([]pursuit.MatchCandidate, error) {
	f.matchCalls++
	f.lastMatch = request
	return f.matches, nil
}

func (f *fakePursuitCommandRouter) RouteIntake(request pursuit.IntakeRequest) (*pursuit.RoutedIntakeResult, error) {
	f.routeCalls++
	f.lastRoute = request
	if f.routed != nil {
		return f.routed, nil
	}
	return &pursuit.RoutedIntakeResult{Mode: "candidate_created"}, nil
}

func (f *fakePursuitCommandRouter) Intake(id uuid.UUID, request pursuit.IntakeRequest) (*pursuit.PursuitDetail, error) {
	f.intakeCalls++
	return &pursuit.PursuitDetail{Pursuit: models.Pursuit{ID: id, Title: "Selected pursuit"}}, nil
}

func (f *fakePursuitCommandRouter) DetailForOwner(ownerIdentity string, id uuid.UUID) (*pursuit.PursuitDetail, error) {
	f.detailCalls++
	f.lastOwner = ownerIdentity
	if f.detail != nil {
		copy := *f.detail
		copy.Pursuit.OwnerIdentity = firstNonEmpty(copy.Pursuit.OwnerIdentity, ownerIdentity)
		return &copy, nil
	}
	return &pursuit.PursuitDetail{Pursuit: models.Pursuit{ID: id, Title: "Selected pursuit", OwnerIdentity: ownerIdentity}}, nil
}

func (f *fakeAgentCycleRunner) Run(request agentcycle.RunRequest) *agentcycle.RunResult {
	f.calls++
	f.lastRequest = request
	if f.result != nil {
		result := *f.result
		result.Trigger = request.Trigger
		return &result
	}
	return &agentcycle.RunResult{
		Trigger:     request.Trigger,
		Status:      "completed",
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
		Dashboard: &workflow.WorkflowDashboard{
			ApprovalItems: []models.WorkflowItem{{ID: uuid.New()}},
		},
		NextAction:    "review approval queue",
		SafetySummary: "approval gates remain enforced",
	}
}

func TestCommandPassesAuthenticatedOwnerIntoAgentCycle(t *testing.T) {
	tasks := &fakeTaskEngine{}
	cycle := &fakeAgentCycleRunner{}
	service := NewService(tasks, cycle)

	if _, err := service.Command(CommandRequest{Message: "Run my operating refresh", RunCycle: true, OwnerIdentity: "alice"}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if cycle.lastRequest.OwnerIdentity != "alice" {
		t.Fatalf("agent-cycle owner = %q, want alice", cycle.lastRequest.OwnerIdentity)
	}
	if tasks.lastRequest.OwnerIdentity != "alice" {
		t.Fatalf("task owner = %q, want alice", tasks.lastRequest.OwnerIdentity)
	}
}

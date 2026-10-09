package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

const resourcePlanningAuthorityBoundary = "resource planning is advisory only and cannot grant execution authority, consume approval, or override the task risk gate"

type ResourcePlanner interface {
	PlanResources(request ResourcePlanningRequest) (*resourceplanner.Decision, error)
}

type ResourcePlanningRequest struct {
	OwnerIdentity  string
	WorkspaceID    string
	PlanID         string
	CreatedAt      time.Time
	Deadline       *time.Time
	Difficulty     int
	Steps          []TaskStep
	Risk           RiskAssessment
	ModelDecision  llm.RouteDecision
	SelectedTools  []string
	Capacity       *frameworkregistry.CapacitySnapshot
	CalendarBusy   []source.CalendarBusyInterval
	PaidAllowed    bool
	PaidBudgetEUR  float64
	PaidBudgetUsed float64
}

type resourcePlanningBridge struct {
	planner *resourceplanner.Planner
}

func NewResourcePlanningBridge(planner *resourceplanner.Planner) ResourcePlanner {
	if planner == nil {
		planner = resourceplanner.New()
	}
	return &resourcePlanningBridge{planner: planner}
}

func defaultResourcePlanner() ResourcePlanner {
	return NewResourcePlanningBridge(resourceplanner.New())
}

func WithResourcePlanning(base Service, planner ResourcePlanner) (Service, error) {
	implementation, ok := base.(*service)
	if !ok {
		return nil, fmt.Errorf("resource planning requires the built-in task service")
	}
	if planner == nil {
		return nil, fmt.Errorf("resource planner is required")
	}
	implementation.resourcePlanner = planner
	return implementation, nil
}

func (bridge *resourcePlanningBridge) PlanResources(request ResourcePlanningRequest) (*resourceplanner.Decision, error) {
	if bridge == nil || bridge.planner == nil {
		return nil, fmt.Errorf("resource planning bridge is not configured")
	}
	start := request.CreatedAt.UTC().Truncate(time.Minute)
	if start.IsZero() {
		return nil, fmt.Errorf("resource planning requires a creation timestamp")
	}
	horizonEnd := start.Add(7 * 24 * time.Hour)
	if request.Deadline != nil {
		deadline := request.Deadline.UTC().Truncate(time.Minute)
		if !deadline.After(start) {
			return nil, fmt.Errorf("task deadline has already passed")
		}
		horizonEnd = deadline
	}

	tasks := make([]resourceplanner.Task, 0, len(request.Steps))
	previous := ""
	automaticRuntime := admittedAutomaticRuntime(request.Risk)
	for index, step := range request.Steps {
		optimistic, expected, pessimistic := resourceDuration(request.Difficulty, step)
		// Personal-capacity uncertainty applies when the plan depends on Robert's
		// time. A bounded Level 8 deterministic runtime is independently
		// authorized and must not be converted into a human approval gate merely
		// because no personal capacity observation has been recorded yet.
		capacityNeedsReview := request.Capacity != nil && request.Capacity.NeedsReview && !automaticRuntime
		planned := resourceplanner.Task{
			ID:       resourceStepID(index),
			Duration: resourceplanner.DurationEstimate{OptimisticMinutes: optimistic, ExpectedMinutes: expected, PessimisticMinutes: pessimistic, Basis: "bounded task difficulty and step type estimate"},
			Priority: maxInt(1, 100-index),
			Approval: resourceplanner.TaskApproval{Required: step.RequiresApproval || request.Risk.ApprovalRequired || capacityNeedsReview},
		}
		if planned.Approval.Required {
			planned.Approval.Reasons = []string{"task risk, step policy, or uncertain owner capacity requires review before execution"}
		}
		if previous != "" {
			planned.Dependencies = []string{previous}
		}
		if index == 0 {
			planned.EstimatedUsage = resourceplanner.Usage{
				CostMicros:   eurToMicros(request.ModelDecision.EstimatedCostEUR),
				InputTokens:  int64(maxInt(0, request.ModelDecision.EstimatedInputTokens)),
				OutputTokens: int64(maxInt(0, request.ModelDecision.EstimatedOutputTokens)),
				ToolCalls:    int64(len(request.SelectedTools)),
			}
		}
		if request.Capacity != nil && !request.Capacity.NeedsReview {
			planned.Resources = []resourceplanner.ResourceRequirement{{ResourceID: "owner-capacity", CapacityUnits: 1}}
		}
		if index == len(request.Steps)-1 && request.Deadline != nil {
			deadline := request.Deadline.UTC().Truncate(time.Minute)
			planned.Deadline = &deadline
			planned.DeadlineKind = resourceplanner.HardDeadline
		}
		tasks = append(tasks, planned)
		previous = planned.ID
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("resource planning requires at least one task step")
	}

	zero := int64(0)
	remainingCost := request.PaidBudgetEUR - request.PaidBudgetUsed
	if !request.PaidAllowed || remainingCost < 0 {
		remainingCost = 0
	}
	maxCost := eurToMicros(remainingCost)
	budget := resourceplanner.Budget{MaxCostMicros: &maxCost}
	if !request.PaidAllowed {
		budget.MaxCostMicros = &zero
	}
	toolLimit := int64(len(request.SelectedTools))
	budget.MaxToolCalls = &toolLimit

	availability := []resourceplanner.CapacityWindow{}
	if request.Capacity != nil && request.Capacity.TimeAvailableMinutes > 0 {
		availableEnd := start.Add(time.Duration(request.Capacity.TimeAvailableMinutes) * time.Minute)
		if availableEnd.After(horizonEnd) {
			availableEnd = horizonEnd
		}
		availability = ownerCapacityWindows(start, availableEnd, request.CalendarBusy)
	}
	workspaceID := strings.TrimSpace(request.WorkspaceID)
	if workspaceID != "" {
		digest := sha256.Sum256([]byte(workspaceID))
		workspaceID = "workspace-" + hex.EncodeToString(digest[:8])
	}
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	if ownerIdentity == "" {
		ownerIdentity = "system:unowned-planning"
	}
	uncertaintyThreshold := int64(5000)
	if automaticRuntime {
		// The planner's generic duration range is intentionally conservative. It
		// must not convert that estimate alone into a human-approval requirement
		// after the task and framework gates have already admitted a bounded,
		// reversible Level 8 runtime. Capacity, budget, and explicit-risk flags
		// still flow through unchanged.
		uncertaintyThreshold = 0
	}
	decision, err := bridge.planner.Plan(resourceplanner.Request{
		OwnerIdentity:  ownerIdentity,
		WorkspaceID:    workspaceID,
		PlanID:         request.PlanID,
		AsOf:           start,
		HorizonStart:   start,
		HorizonEnd:     horizonEnd,
		DurationMode:   resourceplanner.ConservativeDuration,
		Tasks:          tasks,
		Availability:   availability,
		Budget:         budget,
		ApprovalPolicy: resourceplanner.ApprovalPolicy{SoftDeadlineMiss: true, UncertaintyThreshold: uncertaintyThreshold},
	})
	if err != nil {
		return nil, err
	}
	if decision.Authority != "advisory_only" || decision.CanExecute || decision.GrantsAuthority {
		return nil, fmt.Errorf("resource planner violated the advisory-only authority boundary")
	}
	return &decision, nil
}

func admittedAutomaticRuntime(risk RiskAssessment) bool {
	return strings.EqualFold(strings.TrimSpace(risk.Level), "low") &&
		risk.AllowedNow &&
		!risk.ApprovalRequired &&
		!risk.ApprovalGranted &&
		risk.RequiredFrameworkAutonomy >= 8 &&
		risk.FrameworkAutonomyCeiling >= risk.RequiredFrameworkAutonomy
}

func ownerCapacityWindows(start, end time.Time, busy []source.CalendarBusyInterval) []resourceplanner.CapacityWindow {
	if !end.After(start) {
		return nil
	}
	type interval struct{ start, end time.Time }
	blocked := make([]interval, 0, len(busy))
	for _, item := range busy {
		itemStart, itemEnd := item.Start.UTC(), item.End.UTC()
		if !itemStart.Before(itemEnd) || !itemEnd.After(start) || !itemStart.Before(end) {
			continue
		}
		if itemStart.Before(start) {
			itemStart = start
		}
		if itemEnd.After(end) {
			itemEnd = end
		}
		blocked = append(blocked, interval{start: itemStart, end: itemEnd})
	}
	sort.Slice(blocked, func(i, j int) bool {
		if blocked[i].start.Equal(blocked[j].start) {
			return blocked[i].end.Before(blocked[j].end)
		}
		return blocked[i].start.Before(blocked[j].start)
	})
	merged := make([]interval, 0, len(blocked))
	for _, current := range blocked {
		if len(merged) == 0 || current.start.After(merged[len(merged)-1].end) {
			merged = append(merged, current)
			continue
		}
		if current.end.After(merged[len(merged)-1].end) {
			merged[len(merged)-1].end = current.end
		}
	}
	windows := make([]resourceplanner.CapacityWindow, 0, len(merged)+1)
	cursor := start
	appendWindow := func(windowStart, windowEnd time.Time) {
		if windowEnd.After(windowStart) {
			windows = append(windows, resourceplanner.CapacityWindow{ResourceID: "owner-capacity", Start: windowStart, End: windowEnd, CapacityUnits: 1})
		}
	}
	for _, current := range merged {
		appendWindow(cursor, current.start)
		if current.end.After(cursor) {
			cursor = current.end
		}
	}
	appendWindow(cursor, end)
	return windows
}

func resourceDuration(difficulty int, step TaskStep) (int64, int64, int64) {
	difficulty = maxInt(1, difficulty)
	expected := int64(5 + difficulty*5)
	name := strings.ToLower(step.Name + " " + step.Purpose)
	if strings.Contains(name, "verify") || strings.Contains(name, "validate") {
		expected += 10
	}
	if strings.Contains(name, "execute") || strings.Contains(name, "tool") {
		expected += 15
	}
	optimistic := expected / 2
	if optimistic < 5 {
		optimistic = 5
	}
	return optimistic, expected, expected * 2
}

func resourceStepID(index int) string { return fmt.Sprintf("step-%03d", index+1) }

func eurToMicros(value float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value*1_000_000 + 0.5)
}

func applyResourcePlanningRisk(risk RiskAssessment, decision *resourceplanner.Decision) RiskAssessment {
	if decision == nil {
		risk.AllowedNow = false
		risk.Reasons = uniqueStrings(append(risk.Reasons, "resource and time feasibility was not evaluated"))
		return risk
	}
	if decision.Authority != "advisory_only" || decision.CanExecute || decision.GrantsAuthority {
		risk.AllowedNow = false
		risk.Reasons = uniqueStrings(append(risk.Reasons, "resource planning attempted to cross its advisory-only authority boundary"))
		return risk
	}
	if decision.Feasibility == resourceplanner.Infeasible {
		risk.AllowedNow = false
		for _, blocker := range decision.CriticalBlockers {
			risk.Reasons = append(risk.Reasons, "resource blocker: "+blocker.Code)
		}
	}
	if decision.Feasibility == resourceplanner.FeasibleWithApprovals || len(decision.ApprovalFlags) > 0 {
		risk.ApprovalRequired = true
		if !risk.ApprovalGranted {
			risk.AllowedNow = false
		}
		risk.Reasons = append(risk.Reasons, "resource plan requires review before execution")
	}
	risk.Reasons = uniqueStrings(append(risk.Reasons, resourcePlanningAuthorityBoundary))
	return risk
}

func applyResourcePlanningExecution(plan ExecutionPlan, decision *resourceplanner.Decision) ExecutionPlan {
	if decision == nil {
		plan.CapacityConstraints = uniqueStrings(append(plan.CapacityConstraints, "resource planning unavailable"))
		return plan
	}
	plan.CapacityConstraints = append(plan.CapacityConstraints, "resource feasibility: "+string(decision.Feasibility))
	for _, blocker := range decision.CriticalBlockers {
		plan.CapacityConstraints = append(plan.CapacityConstraints, blocker.Code+": "+blocker.Detail)
	}
	plan.CapacityConstraints = uniqueStrings(plan.CapacityConstraints)
	plan.AuditEvents = uniqueStrings(append(plan.AuditEvents, "resource and time plan bound: "+decision.DecisionDigest))
	return plan
}

func resourcePlanningSummary(decision *resourceplanner.Decision) string {
	if decision == nil {
		return "resource and time planning was unavailable"
	}
	return fmt.Sprintf("%s; %d scheduled; %d blockers; authority granted=false", decision.Feasibility, len(decision.Scheduled), len(decision.CriticalBlockers))
}

func (s *service) executeWithPursuitReservation(plan *CompletionPlan, request IntakeRequest, attempt int) *ExecutionResult {
	if taskExecutionContext(request).Err() != nil {
		return cancelledTaskExecution(plan.ExecutionResult, plan, request, time.Now().UTC())
	}
	if plan == nil || strings.TrimSpace(plan.PursuitID) == "" {
		return s.executeAllowedSteps(plan, request)
	}
	started := time.Now().UTC()
	pursuitID, err := uuid.Parse(strings.TrimSpace(plan.PursuitID))
	if err != nil {
		return pursuitReservationBlockedExecution(plan, request, "pursuit resource reservation received an invalid pursuit id", started)
	}
	manager, ok := s.pursuitAttempts.(PursuitResourceReservationManager)
	if !ok {
		return pursuitReservationBlockedExecution(plan, request, "pursuit resource reservation boundary is unavailable", started)
	}
	effortMinutes, costMicros := pursuitExecutionEstimate(plan)
	operationRoot := firstNonEmpty(request.operationID, plan.OperationID, plan.ID)
	operationID := operationRoot + ":attempt:" + strconv.Itoa(maxInt(attempt, 1))
	if err := manager.ReservePursuitTaskResources(pursuitID, plan.OwnerIdentity, operationID, effortMinutes, costMicros); err != nil {
		return pursuitReservationBlockedExecution(plan, request, "pursuit resource reservation blocked execution: "+err.Error(), started)
	}
	plan.Events = append(plan.Events, event("resources", fmt.Sprintf("reserved %d minutes and EUR %.6f for execution attempt %d", effortMinutes, float64(costMicros)/1_000_000, attempt)))

	result := s.executeAllowedSteps(plan, request)
	actualEffortMinutes := int64(math.Ceil(time.Since(started).Minutes()))
	if actualEffortMinutes < 1 {
		actualEffortMinutes = 1
	}
	actualCostMicros := int64(0)
	if result != nil && result.LLMGeneration != nil && result.LLMGeneration.EstimatedCostEUR > 0 {
		actualCostMicros = eurToMicros(result.LLMGeneration.EstimatedCostEUR)
	}
	if err := manager.SettlePursuitTaskResources(
		pursuitID, plan.OwnerIdentity, operationID, "consumed", actualEffortMinutes, actualCostMicros,
	); err != nil {
		if result == nil {
			result = newExecutionResult(plan, request, started)
		}
		result.BlockedReason = "execution completed but pursuit resource settlement requires review: " + err.Error()
		result.VerificationStatus = verification.StatusNeedsReview
		result.CompletedAt = time.Now().UTC()
		result.Actions = append(result.Actions, executedAction("pursuit.resource_settlement", "blocked", operationID, result.BlockedReason, started))
		plan.Events = append(plan.Events, event("resources", result.BlockedReason))
		return result
	}
	plan.Events = append(plan.Events, event("resources", fmt.Sprintf("settled execution attempt %d with %d minutes and EUR %.6f actual usage", attempt, actualEffortMinutes, float64(actualCostMicros)/1_000_000)))
	return result
}

func pursuitReservationBlockedExecution(plan *CompletionPlan, request IntakeRequest, reason string, started time.Time) *ExecutionResult {
	result := blockExecution(newExecutionResult(plan, request, started), reason, plan, started)
	// A denied new attempt does not establish that earlier execution had no effects.
	if previous := plan.ExecutionResult; previous != nil {
		result.OutcomeUncertain = executionOutcomeUncertain(previous)
		result.ToolExecution = previous.ToolExecution
		result.Actions = append(append([]ExecutedAction{}, previous.Actions...), result.Actions...)
		if previous.ToolExecution != nil || len(previous.Actions) > 0 {
			result.Output = "Further execution was blocked; prior execution evidence was retained for review. Reservation blocker: " + result.BlockedReason
		}
	}
	return result
}

func pursuitExecutionEstimate(plan *CompletionPlan) (int64, int64) {
	effortMinutes := int64(0)
	if plan != nil && plan.ResourceDecision != nil {
		for _, scheduled := range plan.ResourceDecision.Scheduled {
			if scheduled.PlannedDurationMinutes > 0 {
				effortMinutes += scheduled.PlannedDurationMinutes
			}
		}
	}
	if effortMinutes < 1 {
		effortMinutes = 1
	}
	costMicros := int64(0)
	if plan != nil && plan.ModelDecision.EstimatedCostEUR > 0 {
		costMicros = eurToMicros(plan.ModelDecision.EstimatedCostEUR)
	}
	return effortMinutes, costMicros
}

func newExecutionResult(plan *CompletionPlan, request IntakeRequest, started time.Time) *ExecutionResult {
	return &ExecutionResult{
		StartedAt: started, CompletedAt: started, Mode: executionMode(plan, request),
		VerificationStatus: verification.StatusNeedsReview, Actions: []ExecutedAction{},
	}
}

func taskResourceBudget(service *llm.Service) (bool, float64, float64) {
	if service == nil {
		return false, 0, 0
	}
	policy := service.Policy()
	used := 0.0
	for _, provider := range policy.Providers {
		used += provider.BudgetUsedEUR
	}
	return policy.PaidCallsAllowed, policy.DailyPaidBudgetEUR, used
}

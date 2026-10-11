package assistant

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/agentcycle"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/task"

	"github.com/google/uuid"
)

var ErrInvalidStandingMandateID = errors.New("invalid standing mandate id")

type TaskEngine interface {
	Plan(request task.IntakeRequest) (*task.CompletionPlan, error)
	Run(request task.IntakeRequest) (*task.CompletionPlan, error)
}

type AgentCycleRunner interface {
	Run(request agentcycle.RunRequest) *agentcycle.RunResult
}

type AgentCycleContextRunner interface {
	RunContext(context.Context, agentcycle.RunRequest) (*agentcycle.RunResult, error)
}

// PursuitCommandRouter is deliberately narrow: the assistant can inspect a
// possible parent pursuit and turn an explicit execution request into the
// existing governed workflow path, but it cannot bypass pursuit policy.
type PursuitCommandRouter interface {
	Match(request pursuit.MatchRequest) ([]pursuit.MatchCandidate, error)
	RouteIntake(request pursuit.IntakeRequest) (*pursuit.RoutedIntakeResult, error)
	Intake(id uuid.UUID, request pursuit.IntakeRequest) (*pursuit.PursuitDetail, error)
	DetailForOwner(ownerIdentity string, id uuid.UUID) (*pursuit.PursuitDetail, error)
}

type CommandRequest struct {
	Message         string   `json:"message"`
	ProjectKey      string   `json:"projectKey,omitempty"`
	PursuitID       string   `json:"pursuitId,omitempty"`
	AutomationID    string   `json:"automationId,omitempty"`
	MandateID       string   `json:"mandateId,omitempty"`
	SuccessCriteria []string `json:"successCriteria,omitempty"`
	ExecuteAllowed  bool     `json:"executeAllowed,omitempty"`
	RunCycle        bool     `json:"runCycle,omitempty"`
	SkipSourceSync  bool     `json:"skipSourceSync,omitempty"`
	SkipAmbient     bool     `json:"skipAmbient,omitempty"`
	OwnerIdentity   string   `json:"-"`
	Actor           string   `json:"-"`
}

type CommandAction struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type CommandPursuitContext struct {
	PursuitID          string                   `json:"pursuitId,omitempty"`
	Title              string                   `json:"title,omitempty"`
	Mode               string                   `json:"mode"`
	Matched            bool                     `json:"matched"`
	CreatedCandidate   bool                     `json:"createdCandidate,omitempty"`
	AwaitingAcceptance bool                     `json:"awaitingAcceptance,omitempty"`
	ExecutionQueued    bool                     `json:"executionQueued,omitempty"`
	Score              float64                  `json:"score,omitempty"`
	Reasons            []string                 `json:"reasons,omitempty"`
	Message            string                   `json:"message,omitempty"`
	Matches            []pursuit.MatchCandidate `json:"matches,omitempty"`
}

type CommandResult struct {
	OwnerIdentity  string                 `json:"-"`
	ID             string                 `json:"id"`
	CreatedAt      time.Time              `json:"createdAt"`
	Intent         string                 `json:"intent"`
	Summary        string                 `json:"summary"`
	NextAction     string                 `json:"nextAction"`
	SafetySummary  string                 `json:"safetySummary"`
	Actions        []CommandAction        `json:"actions"`
	ReviewRequired bool                   `json:"reviewRequired"`
	Plan           *task.CompletionPlan   `json:"plan,omitempty"`
	AgentCycle     *agentcycle.RunResult  `json:"agentCycle,omitempty"`
	Pursuit        *CommandPursuitContext `json:"pursuit,omitempty"`
}

type Service struct {
	tasks    TaskEngine
	cycle    AgentCycleRunner
	pursuits PursuitCommandRouter
	mu       sync.Mutex
	logs     []CommandResult
}

func NewService(tasks TaskEngine, cycle AgentCycleRunner, pursuitRouters ...PursuitCommandRouter) *Service {
	var pursuits PursuitCommandRouter
	if len(pursuitRouters) > 0 {
		pursuits = pursuitRouters[0]
	}
	return &Service{tasks: tasks, cycle: cycle, pursuits: pursuits}
}

func (s *Service) Command(request CommandRequest) (*CommandResult, error) {
	return s.CommandContext(context.Background(), request)
}

// CommandContext binds interactive assistant work to the caller lifecycle.
// Durable in-process callers retain Command, while HTTP requests can cancel
// planning and autonomous cycles when the client disconnects or times out.
func (s *Service) CommandContext(ctx context.Context, request CommandRequest) (*CommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateStandingMandateID(request.MandateID); err != nil {
		return nil, err
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		message = "Run the HAI autonomous maintenance cycle and surface the next best action."
		request.RunCycle = true
	}

	intent := classifyIntent(message, request)
	result := &CommandResult{
		OwnerIdentity: strings.TrimSpace(request.OwnerIdentity),
		ID:            uuid.NewString(),
		CreatedAt:     time.Now().UTC(),
		Intent:        intent,
		Actions:       []CommandAction{},
		SafetySummary: "Assistant commands are routed through existing HAI engines; risky execution remains blocked by task risk gates, workflow approval gates, emergency stop, and runtime safety controls.",
	}

	taskRequest := task.IntakeRequest{
		OwnerIdentity:   request.OwnerIdentity,
		Request:         message,
		ProjectKey:      request.ProjectKey,
		AutomationID:    request.AutomationID,
		MandateID:       request.MandateID,
		SuccessCriteria: request.SuccessCriteria,
		ExecuteAllowed:  request.ExecuteAllowed,
	}

	if s.pursuits != nil && shouldTrackCommand(message, request, intent) {
		pursuitContext, err := s.routePursuit(ctx, message, request)
		if err != nil {
			result.record("pursuit intake", err, "could not persist the command in the governed workflow path")
			return result, err
		}
		result.Pursuit = pursuitContext
		if pursuitContext != nil {
			if pursuitContext.AwaitingAcceptance {
				// A candidate is durable context, not active work. Do not turn a
				// valid candidate outcome into a direct task-plan failure or an
				// unapproved execution attempt.
				result.ReviewRequired = true
				result.NextAction = "review the pursuit candidate and explicitly accept or archive it before planning work"
				result.Summary = "HAI recorded a reviewable pursuit candidate and kept it outside the task and execution path until approval."
				result.record("pursuit candidate", nil, "recorded a reviewable candidate; no direct task plan, workflow execution, or runtime action was created")
				s.addLog(*result)
				return result, nil
			}
			if !pursuitContext.ExecutionQueued {
				taskRequest.PursuitID = pursuitContext.PursuitID
			}
		}
	}
	workflowQueued := result.Pursuit != nil && result.Pursuit.ExecutionQueued
	if s.tasks == nil && !workflowQueued {
		return nil, fmt.Errorf("task engine is not configured")
	}

	var plan *task.CompletionPlan
	var err error
	if workflowQueued {
		// Pursuit intake already created or reused the governed workflow. Its
		// worker passes WorkflowID into the task engine, which keeps task-run
		// evidence on the workflow ledger and avoids a duplicate direct plan.
		result.record("pursuit workflow", nil, "created or reused governed workflow work; the workflow worker owns planning, execution, retries, and verification")
	} else if request.ExecuteAllowed {
		if contextual, ok := s.tasks.(task.ContextService); ok {
			plan, err = contextual.RunContext(ctx, taskRequest)
		} else if err = ctx.Err(); err == nil {
			plan, err = s.tasks.Run(taskRequest)
		}
		result.record("task success engine", err, "ran safe allowed task steps")
	} else {
		if contextual, ok := s.tasks.(task.PlanningContextService); ok {
			plan, err = contextual.PlanContext(ctx, taskRequest)
		} else if err = ctx.Err(); err == nil {
			plan, err = s.tasks.Plan(taskRequest)
		}
		result.record("task planner", err, "created completion-first plan")
	}
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if plan != nil {
		result.Plan = plan
	}

	if shouldRunCycle(message, request, intent) {
		if s.cycle == nil {
			result.record("agent cycle", fmt.Errorf("agent cycle service is not configured"), "agent cycle unavailable")
		} else {
			cycleRequest := agentcycle.RunRequest{
				OwnerIdentity:  request.OwnerIdentity,
				Trigger:        "assistant." + intent,
				Limit:          8,
				SkipSourceSync: request.SkipSourceSync,
				SkipAmbient:    request.SkipAmbient,
			}
			var cycle *agentcycle.RunResult
			if contextual, ok := s.cycle.(AgentCycleContextRunner); ok {
				cycle, err = contextual.RunContext(ctx, cycleRequest)
			} else if err = ctx.Err(); err == nil {
				cycle = s.cycle.Run(cycleRequest)
			}
			result.AgentCycle = cycle
			result.record("agent cycle", err, cycleSummary(cycle))
			if err != nil {
				return result, err
			}
		}
	}

	result.ReviewRequired = planRequiresReview(result.Plan) || cycleRequiresReview(result.AgentCycle)
	result.NextAction = deriveNextAction(result)
	result.Summary = deriveSummary(result)
	if workflowQueued && result.AgentCycle == nil {
		result.NextAction = "review the governed workflow, its evidence, and any approval requirement before it runs"
		result.Summary = "HAI added the command to governed workflow work. The workflow worker owns planning, execution, retries, verification, and the audit trail."
	}
	s.addLog(*result)
	return result, nil
}

func validateStandingMandateID(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return ErrInvalidStandingMandateID
	}
	return nil
}

func (s *Service) routePursuit(ctx context.Context, message string, request CommandRequest) (*CommandPursuitContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input := pursuit.IntakeRequest{
		OwnerIdentity:  request.OwnerIdentity,
		Input:          message,
		ProjectKey:     request.ProjectKey,
		AutomationID:   request.AutomationID,
		MandateID:      request.MandateID,
		SourceType:     "assistant_command",
		SourceID:       assistantCommandSourceID(message, request),
		SourceURI:      "assistant://command/" + assistantCommandSourceID(message, request),
		SourceLabel:    "HAI chat command",
		ContentType:    "assistant_command",
		Trigger:        "assistant_command",
		Actor:          firstNonEmpty(request.Actor, "assistant"),
		RequiresReview: false,
	}

	if requestedID := strings.TrimSpace(request.PursuitID); requestedID != "" {
		id, err := uuid.Parse(requestedID)
		if err != nil {
			return nil, fmt.Errorf("invalid pursuit id")
		}
		detail, err := s.pursuits.DetailForOwner(request.OwnerIdentity, id)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if detail == nil {
			return nil, fmt.Errorf("selected pursuit was not found")
		}
		if pursuit.IsCandidate(detail.Pursuit) {
			return candidatePursuitContext(detail, "selected_candidate"), nil
		}
		if !request.ExecuteAllowed {
			context := pursuitContextFromDetail(detail, "selected", false)
			context.Message = "Planning is scoped to the selected pursuit. No workflow was created until you run the command."
			return context, nil
		}
		detail, err = s.pursuits.Intake(id, input)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return pursuitContextFromDetail(detail, "selected", true), nil
	}

	if !request.ExecuteAllowed {
		matches, err := s.pursuits.Match(pursuit.MatchRequest{
			OwnerIdentity: request.OwnerIdentity,
			Input:         message,
			ProjectKey:    request.ProjectKey,
			SourceType:    input.SourceType,
			SourceID:      input.SourceID,
			SourceURI:     input.SourceURI,
			Limit:         3,
		})
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &CommandPursuitContext{
			Mode:    "suggested",
			Matches: matches,
			Message: "Planning did not create work. Select a pursuit or run the command to create a governed workflow.",
		}, nil
	}

	routed, err := s.pursuits.RouteIntake(input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidatePending := routed.CreatedCandidate || strings.EqualFold(strings.TrimSpace(routed.Mode), "matched_candidate")
	if routed.Detail != nil && pursuit.IsCandidate(routed.Detail.Pursuit) {
		candidatePending = true
	}
	context := &CommandPursuitContext{
		Mode:               firstNonEmpty(routed.Mode, "routed"),
		Matched:            routed.Matched,
		CreatedCandidate:   routed.CreatedCandidate,
		AwaitingAcceptance: candidatePending,
		Score:              routed.Score,
		Reasons:            routed.Reasons,
		Message:            routed.Message,
		Matches:            routed.Matches,
		ExecutionQueued:    !candidatePending,
	}
	if candidatePending && strings.TrimSpace(context.Message) == "" {
		context.Message = "HAI recorded a reviewable pursuit candidate. Explicit acceptance is required before any task or workflow work is created."
	}
	if routed.PursuitID != uuid.Nil {
		context.PursuitID = routed.PursuitID.String()
	}
	if routed.Detail != nil {
		context.Title = routed.Detail.Pursuit.Title
		if context.PursuitID == "" {
			context.PursuitID = routed.Detail.Pursuit.ID.String()
		}
	}
	return context, nil
}

func candidatePursuitContext(detail *pursuit.PursuitDetail, mode string) *CommandPursuitContext {
	context := pursuitContextFromDetail(detail, mode, false)
	context.AwaitingAcceptance = true
	context.Message = "This pursuit is a reviewable candidate. Explicit approval is required before HAI creates a task plan, workflow, or execution attempt."
	return context
}

func pursuitContextFromDetail(detail *pursuit.PursuitDetail, mode string, queued bool) *CommandPursuitContext {
	if detail == nil {
		return &CommandPursuitContext{Mode: mode, ExecutionQueued: queued}
	}
	return &CommandPursuitContext{
		PursuitID:       detail.Pursuit.ID.String(),
		Title:           detail.Pursuit.Title,
		Mode:            mode,
		Matched:         true,
		ExecutionQueued: queued,
		Message:         "Command was added to the selected pursuit as governed workflow work.",
	}
}

func shouldTrackCommand(message string, request CommandRequest, intent string) bool {
	if strings.TrimSpace(request.PursuitID) != "" {
		return true
	}
	if request.ExecuteAllowed {
		return true
	}
	return !shouldRunCycle(message, request, intent)
}

func assistantCommandSourceID(message string, request CommandRequest) string {
	value := strings.Join([]string{
		strings.ToLower(strings.Join(strings.Fields(message), " ")),
		strings.ToLower(strings.TrimSpace(request.ProjectKey)),
		strings.ToLower(strings.TrimSpace(request.AutomationID)),
		strings.ToLower(strings.TrimSpace(request.MandateID)),
		strings.Join(request.SuccessCriteria, "\n"),
	}, "\n")
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("assistant-%x", sum[:12])
}

func (s *Service) Logs() []CommandResult {
	return s.LogsForOwner("")
}

func (s *Service) LogsForOwner(ownerIdentity string) []CommandResult {
	if s == nil {
		return nil
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make([]CommandResult, 0, len(s.logs))
	for _, result := range s.logs {
		if ownerIdentity == "" || result.OwnerIdentity == ownerIdentity {
			copied = append(copied, result)
		}
	}
	return copied
}

func (s *Service) addLog(result CommandResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append([]CommandResult{result}, s.logs...)
	if len(s.logs) > 50 {
		s.logs = s.logs[:50]
	}
}

func (r *CommandResult) record(name string, err error, summary string) {
	status := "completed"
	if err != nil {
		status = "failed"
		summary = err.Error()
	}
	r.Actions = append(r.Actions, CommandAction{Name: name, Status: status, Summary: summary})
}

func classifyIntent(message string, request CommandRequest) string {
	text := strings.ToLower(message)
	if request.RunCycle || shouldTriggerOperatingCycle(text) {
		if request.ExecuteAllowed {
			return "execute_and_cycle"
		}
		return "plan_and_cycle"
	}
	if request.ExecuteAllowed {
		return "execute_safe_steps"
	}
	return "plan"
}

func shouldRunCycle(message string, request CommandRequest, intent string) bool {
	if request.RunCycle {
		return true
	}
	text := strings.ToLower(message)
	return strings.Contains(intent, "cycle") ||
		shouldTriggerOperatingCycle(text)
}

func shouldTriggerOperatingCycle(text string) bool {
	return containsAny(text,
		"ambient",
		"agent cycle",
		"autonomous cycle",
		"open loop",
		"open loops",
		"follow-up",
		"follow up",
		"clear blocker",
		"clear blockers",
		"sync source",
		"sync sources",
		"what is stuck",
		"what's stuck",
		"pursuit",
		"pursuits",
		"long-running goal",
		"long running goal",
		"robert decision",
		"robert decisions",
		"needs robert",
		"need robert",
		"what needs me",
		"needs me",
		"decision queue",
		"yes/no queue",
		"yes no queue",
		"va-ready",
		"va ready",
		"system-ready",
		"system ready",
		"ready to move",
		"stale pursuit",
		"stuck pursuit",
	)
}

func deriveSummary(result *CommandResult) string {
	if result == nil {
		return ""
	}
	if result.Plan != nil && result.AgentCycle != nil {
		if len(result.AgentCycle.PursuitDecisions) > 0 {
			first := result.AgentCycle.PursuitDecisions[0]
			return fmt.Sprintf(
				"HAI prepared a task plan and found %d Robert-only pursuit decision(s). Top decision: %s",
				len(result.AgentCycle.PursuitDecisions),
				firstNonEmpty(first.Decision.Recommended, first.NextAction, first.Pursuit.Title),
			)
		}
		if state := result.AgentCycle.PursuitState; state != nil {
			return fmt.Sprintf(
				"HAI converted the command into a task success plan, refreshed pursuit operating state, and found %d item(s) needing attention in the %s lane.",
				state.AttentionTotal,
				firstNonEmpty(state.PrimaryLane, "monitor"),
			)
		}
		return "HAI converted the command into a task success plan and ran the autonomous maintenance cycle."
	}
	if result.Plan != nil {
		if result.Plan.RiskAssessment.ApprovalRequired {
			return "HAI prepared the command but left risky execution behind an approval gate."
		}
		return "HAI converted the command into a completion-first task plan."
	}
	if result.AgentCycle != nil {
		if state := result.AgentCycle.PursuitState; state != nil {
			return fmt.Sprintf("HAI refreshed pursuit operating state and found %d item(s) needing attention in the %s lane.", state.AttentionTotal, firstNonEmpty(state.PrimaryLane, "monitor"))
		}
		return "HAI ran the autonomous maintenance cycle."
	}
	return "HAI could not route the command to an engine."
}

func deriveNextAction(result *CommandResult) string {
	if result == nil {
		return ""
	}
	if result.AgentCycle != nil && strings.TrimSpace(result.AgentCycle.NextAction) != "" && result.AgentCycle.NextAction != "no immediate human action; continue scheduled monitoring" {
		return result.AgentCycle.NextAction
	}
	if result.Plan != nil {
		if strings.TrimSpace(result.Plan.ValidationResult.NextAction) != "" {
			return result.Plan.ValidationResult.NextAction
		}
		if result.Plan.RiskAssessment.ApprovalRequired && !result.Plan.RiskAssessment.ApprovalGranted {
			return "review approval queue before execution"
		}
	}
	if result.AgentCycle != nil && strings.TrimSpace(result.AgentCycle.NextAction) != "" {
		return result.AgentCycle.NextAction
	}
	return "continue scheduled monitoring"
}

func planRequiresReview(plan *task.CompletionPlan) bool {
	if plan == nil {
		return false
	}
	return plan.RiskAssessment.ApprovalRequired ||
		plan.ReviewQueueItem != nil ||
		strings.Contains(strings.ToLower(plan.CompletionStatus), "review")
}

func cycleRequiresReview(cycle *agentcycle.RunResult) bool {
	if cycle == nil {
		return false
	}
	if len(cycle.Errors) > 0 {
		return true
	}
	if cycle.Dashboard != nil {
		if len(cycle.Dashboard.ApprovalItems) > 0 || len(cycle.Dashboard.BlockedItems) > 0 {
			return true
		}
	}
	if len(cycle.PursuitDecisions) > 0 {
		return true
	}
	if cycle.PursuitState != nil {
		return cycle.PursuitState.NeedsRobert > 0 ||
			cycle.PursuitState.PlanningNeeded > 0 ||
			cycle.PursuitState.ReviewDue > 0 ||
			cycle.PursuitState.Stuck > 0
	}
	if cycle.PursuitBrief != nil {
		return cycle.PursuitBrief.NeedsRobert > 0 ||
			cycle.PursuitBrief.PlanningNeeded > 0 ||
			cycle.PursuitBrief.ReviewDue > 0 ||
			cycle.PursuitBrief.Stuck > 0
	}
	return false
}

func cycleSummary(cycle *agentcycle.RunResult) string {
	if cycle == nil {
		return "agent cycle returned no result"
	}
	parts := []string{"status " + cycle.Status}
	if cycle.SourceSync != nil {
		parts = append(parts, fmt.Sprintf("sources %d/%d completed", cycle.SourceSync.Completed, cycle.SourceSync.Checked))
	}
	if cycle.Workflows != nil {
		parts = append(parts, fmt.Sprintf("workflows checked %d", cycle.Workflows.Checked))
	}
	if cycle.AmbientScan != nil {
		parts = append(parts, fmt.Sprintf("ambient opportunities %d", cycle.AmbientScan.OpportunitiesFound))
	}
	if cycle.PursuitState != nil {
		parts = append(parts, fmt.Sprintf("pursuits lane %s attention %d Robert %d", cycle.PursuitState.PrimaryLane, cycle.PursuitState.AttentionTotal, cycle.PursuitState.NeedsRobert))
		if len(cycle.PursuitDecisions) > 0 {
			parts = append(parts, fmt.Sprintf("top decision %s", firstNonEmpty(cycle.PursuitDecisions[0].Decision.Recommended, cycle.PursuitDecisions[0].NextAction, cycle.PursuitDecisions[0].Pursuit.Title)))
		}
		return strings.Join(parts, "; ")
	}
	if cycle.PursuitBrief != nil {
		parts = append(parts, fmt.Sprintf("pursuits Robert %d planning %d stuck %d", cycle.PursuitBrief.NeedsRobert, cycle.PursuitBrief.PlanningNeeded, cycle.PursuitBrief.Stuck))
	}
	return strings.Join(parts, "; ")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
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

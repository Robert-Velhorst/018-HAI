package workflowtask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

type DeferredRunner struct {
	mu       sync.RWMutex
	delegate workflow.TaskRunner
}

type Runner struct {
	service                 task.Service
	approvalBindingPreparer automation.WorkflowApprovalBindingPreparer
	automationCatalog       automationCatalog
}

type automationCatalog interface {
	FindAll() ([]*models.Automation, error)
}

func NewDeferredRunner() *DeferredRunner {
	return &DeferredRunner{}
}

func (r *DeferredRunner) Set(delegate workflow.TaskRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delegate = delegate
}

func (r *DeferredRunner) RunWorkflowTask(request workflow.TaskRunRequest) (*workflow.TaskRunResult, error) {
	if request.ExecutionContext != nil {
		return r.RunWorkflowTaskContext(request.ExecutionContext, request)
	}
	r.mu.RLock()
	delegate := r.delegate
	r.mu.RUnlock()
	if delegate == nil {
		return nil, fmt.Errorf("workflow task runner is not initialized")
	}
	return delegate.RunWorkflowTask(request)
}

func (r *DeferredRunner) RunWorkflowTaskContext(ctx context.Context, request workflow.TaskRunRequest) (*workflow.TaskRunResult, error) {
	if ctx == nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(workflow.ErrTaskExecutionContextUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(err)
	}
	r.mu.RLock()
	delegate := r.delegate
	r.mu.RUnlock()
	contextual, ok := delegate.(workflow.ContextualTaskRunner)
	if !ok {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(workflow.ErrTaskExecutionContextUnavailable)
	}
	request.ExecutionContext = ctx
	return contextual.RunWorkflowTaskContext(ctx, request)
}

func (r *DeferredRunner) PrepareWorkflowApprovalBinding(request workflow.WorkflowApprovalBindingRequest) (string, error) {
	r.mu.RLock()
	delegate := r.delegate
	r.mu.RUnlock()
	if delegate == nil {
		return "", fmt.Errorf("workflow task runner is not initialized")
	}
	preparer, ok := delegate.(workflow.ApprovalBindingPreparer)
	if !ok {
		return "", fmt.Errorf("workflow task runner cannot prepare automation approval bindings")
	}
	return preparer.PrepareWorkflowApprovalBinding(request)
}

func (r *DeferredRunner) SelectWorkflowAutomations(request workflow.AutomationSelectionRequest) ([]workflow.AutomationCandidate, error) {
	r.mu.RLock()
	delegate := r.delegate
	r.mu.RUnlock()
	if delegate == nil {
		return nil, fmt.Errorf("workflow task runner is not initialized")
	}
	selector, ok := delegate.(workflow.AutomationSelector)
	if !ok {
		return nil, fmt.Errorf("workflow task runner cannot select automations")
	}
	return selector.SelectWorkflowAutomations(request)
}

func NewRunner(service task.Service, preparers ...automation.WorkflowApprovalBindingPreparer) *Runner {
	runner := &Runner{service: service}
	for _, preparer := range preparers {
		if preparer != nil {
			runner.approvalBindingPreparer = preparer
			if catalog, ok := preparer.(automationCatalog); ok {
				runner.automationCatalog = catalog
			}
			break
		}
	}
	return runner
}

func DefaultRunner() (*Runner, error) {
	service, err := task.DefaultService()
	if err != nil {
		return nil, err
	}
	return NewRunner(service), nil
}

func (r *Runner) PrepareWorkflowApprovalBinding(request workflow.WorkflowApprovalBindingRequest) (string, error) {
	if r == nil || r.approvalBindingPreparer == nil {
		return "", fmt.Errorf("automation approval binding preparer is not configured")
	}
	request.OwnerIdentity = strings.TrimSpace(request.OwnerIdentity)
	request.WorkflowID = strings.TrimSpace(request.WorkflowID)
	request.AutomationID = strings.TrimSpace(request.AutomationID)
	request.MandateID = strings.TrimSpace(request.MandateID)
	request.Request = strings.TrimSpace(request.Request)
	request.ProjectKey = strings.TrimSpace(request.ProjectKey)
	if request.OwnerIdentity == "" {
		return "", fmt.Errorf("workflow approval owner identity is required")
	}
	if _, err := uuid.Parse(request.WorkflowID); err != nil {
		return "", fmt.Errorf("workflow approval workflow ID must be a UUID")
	}
	automationID, err := uuid.Parse(request.AutomationID)
	if err != nil || automationID == uuid.Nil {
		return "", fmt.Errorf("workflow approval automation ID must be a UUID")
	}
	if request.Request == "" {
		return "", fmt.Errorf("workflow approval request is required")
	}
	executionTask := task.ExecutionTask(task.IntakeRequest{
		OwnerIdentity:  request.OwnerIdentity,
		WorkflowID:     request.WorkflowID,
		Request:        request.Request,
		ProjectKey:     request.ProjectKey,
		AutomationID:   request.AutomationID,
		MandateID:      request.MandateID,
		ExecuteAllowed: true,
		HumanApproved:  true,
	})
	return r.approvalBindingPreparer.PrepareWorkflowApprovalBinding(
		automationID,
		automation.TaskLaunchRequest{
			OwnerIdentity: request.OwnerIdentity,
			Task:          executionTask,
			ProjectKey:    request.ProjectKey,
			MandateID:     request.MandateID,
		},
	)
}

func (r *Runner) SelectWorkflowAutomations(request workflow.AutomationSelectionRequest) ([]workflow.AutomationCandidate, error) {
	if r == nil || r.automationCatalog == nil {
		return nil, fmt.Errorf("automation catalog is not configured")
	}
	automations, err := r.automationCatalog.FindAll()
	if err != nil {
		return nil, fmt.Errorf("load automation catalog: %w", err)
	}

	requestTerms := automationSelectionTerms(request.Request)
	taskTerms := automationSelectionTerms(request.TaskType)
	projectTerms := automationSelectionTerms(request.ProjectKey)
	candidates := make([]workflow.AutomationCandidate, 0, len(automations))
	for _, candidate := range automations {
		score, reasons, eligible := rankAutomationCandidate(candidate, taskTerms, projectTerms, requestTerms)
		if !eligible {
			continue
		}
		candidates = append(candidates, workflow.AutomationCandidate{
			ID:          candidate.ID.String(),
			Name:        strings.TrimSpace(candidate.Name),
			RuntimeType: strings.ToLower(strings.TrimSpace(candidate.RuntimeType)),
			LaunchType:  strings.ToLower(strings.TrimSpace(candidate.LaunchType)),
			Score:       score,
			Reason:      strings.Join(reasons, "; "),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		left := strings.ToLower(candidates[i].Name)
		right := strings.ToLower(candidates[j].Name)
		if left != right {
			return left < right
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates, nil
}

func rankAutomationCandidate(
	candidate *models.Automation,
	taskTerms []string,
	projectTerms []string,
	requestTerms []string,
) (int, []string, bool) {
	if !configuredAutomationCandidate(candidate) {
		return 0, nil, false
	}

	metadata := strings.ToLower(strings.Join([]string{
		candidate.Name,
		candidate.URLPath,
		candidate.RuntimeType,
		candidate.LaunchType,
		candidate.ServiceName,
		candidate.RoutePath,
		candidate.DependencyNotes,
	}, " "))
	metadataTerms := make(map[string]struct{})
	for _, term := range automationSelectionTerms(metadata) {
		metadataTerms[term] = struct{}{}
	}

	score := 10
	reasons := []string{"configured controlled-runtime automation"}
	matched := false
	if count := matchingAutomationTerms(taskTerms, metadataTerms); count > 0 {
		score += 30 + minInt(count-1, 3)*5
		reasons = append(reasons, fmt.Sprintf("task capability match (%d term(s))", count))
		matched = true
	}
	if count := matchingAutomationTerms(projectTerms, metadataTerms); count > 0 {
		score += 20 + minInt(count-1, 2)*4
		reasons = append(reasons, fmt.Sprintf("project match (%d term(s))", count))
		matched = true
	}
	if count := matchingAutomationTerms(requestTerms, metadataTerms); count > 0 {
		score += minInt(count, 6) * 4
		reasons = append(reasons, fmt.Sprintf("request metadata match (%d term(s))", count))
		matched = true
	}
	if !matched {
		return 0, nil, false
	}

	switch strings.ToLower(strings.TrimSpace(candidate.Status)) {
	case "healthy":
		score += 15
		reasons = append(reasons, "health status healthy")
	case "warning":
		score += 5
		reasons = append(reasons, "health status warning")
	case "degraded":
		score -= 10
		reasons = append(reasons, "health status degraded")
	default:
		reasons = append(reasons, "health status not yet verified")
	}
	return score, reasons, true
}

func configuredAutomationCandidate(candidate *models.Automation) bool {
	if candidate == nil || candidate.ID == uuid.Nil || strings.TrimSpace(candidate.Name) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(candidate.Status)) {
	case "broken", "disabled":
		return false
	}
	launchType := strings.ToLower(strings.TrimSpace(candidate.LaunchType))
	target := strings.TrimSpace(candidate.LaunchTarget)
	switch launchType {
	case "api", "script":
		return target != ""
	case "docker_service":
		return target != "" || strings.TrimSpace(candidate.ServiceName) != ""
	case "agent_runtime":
		return target != "" && strings.TrimSpace(candidate.RuntimeType) != ""
	default:
		return false
	}
}

func automationSelectionTerms(value string) []string {
	terms := make([]string, 0)
	seen := make(map[string]struct{})
	for _, field := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		field = strings.TrimSpace(field)
		if len(field) < 3 || automationSelectionStopWord(field) {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func matchingAutomationTerms(terms []string, metadata map[string]struct{}) int {
	count := 0
	for _, term := range terms {
		if _, ok := metadata[term]; ok {
			count++
		}
	}
	return count
}

func automationSelectionStopWord(value string) bool {
	switch value {
	case "and", "the", "for", "from", "with", "this", "that", "into", "then", "task", "workflow", "automation":
		return true
	default:
		return false
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (r *Runner) RunWorkflowTaskContext(ctx context.Context, request workflow.TaskRunRequest) (*workflow.TaskRunResult, error) {
	if ctx == nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(workflow.ErrTaskExecutionContextUnavailable)
	}
	request.ExecutionContext = ctx
	return r.RunWorkflowTask(request)
}

func (r *Runner) RunWorkflowTask(request workflow.TaskRunRequest) (*workflow.TaskRunResult, error) {
	if request.ExecutionContext != nil && request.ExecutionContext.Err() != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(request.ExecutionContext.Err())
	}
	request, err := normalizeWorkflowTaskRequest(request)
	if err != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(err)
	}
	intake := task.IntakeRequest{
		ExecutionContext:      request.ExecutionContext,
		OwnerIdentity:         request.OwnerIdentity,
		IdempotencyKey:        workflowTaskOperationKey(request),
		PursuitID:             request.PursuitID,
		WorkflowID:            request.WorkflowID,
		Request:               request.Request,
		ProjectKey:            request.ProjectKey,
		AutomationID:          request.AutomationID,
		MandateID:             request.MandateID,
		ExecuteAllowed:        true,
		HumanApproved:         request.HumanApproved,
		ApprovalNote:          request.ApprovalNote,
		ApprovalSourceID:      request.ApprovalSourceID,
		ApprovalBindingDigest: request.ApprovalBindingDigest,
		ApprovalActorIdentity: request.ApprovalActorIdentity,
		ApprovalApprovedAt:    request.ApprovalApprovedAt,
		Deadline:              request.Deadline,
		CoordinationPlan:      request.CoordinationPlan,
	}
	previewer, ok := r.service.(task.PreviewService)
	if !ok {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(fmt.Errorf("workflow task execution requires a side-effect-free framework selection preview"))
	}
	previewRequest := intake
	previewRequest.ExecutionRequested = true
	// Keep the approval state in the selector input so the preview's framework
	// authority contract is comparable to the eventual approved run. The
	// preview still clears HumanApproved and every approval proof, so it cannot
	// cross the execution boundary.
	previewRequest.FrameworkSelectionHumanApproved = request.HumanApproved
	previewRequest.ExecuteAllowed = false
	previewRequest.HumanApproved = false
	previewRequest.ApprovalNote = ""
	previewRequest.ApprovalSourceID = ""
	previewRequest.ApprovalBindingDigest = ""
	previewRequest.ApprovalActorIdentity = ""
	previewRequest.ApprovalApprovedAt = nil
	preview, err := previewer.Preview(previewRequest)
	if err != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(fmt.Errorf("workflow task framework selection preflight: %w", err))
	}
	preflightSelection, err := frameworkSelectionFromPlan(preview)
	if err != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(fmt.Errorf("workflow task framework selection preflight: %w", err))
	}
	if err := enforceFrameworkRiskFloor(preflightSelection, request.RiskLevel); err != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(fmt.Errorf("workflow task framework selection preflight: %w", err))
	}
	if request.ExecutionContext != nil && request.ExecutionContext.Err() != nil {
		return nil, workflow.MarkTaskFailureSafeNoSideEffect(request.ExecutionContext.Err())
	}

	plan, runErr := r.service.Run(intake)
	if request.ExecutionContext != nil && request.ExecutionContext.Err() != nil {
		runErr = errors.Join(runErr, request.ExecutionContext.Err())
	}
	if plan == nil {
		if runErr != nil {
			return nil, runErr
		}
		return nil, fmt.Errorf("workflow task engine returned no completion plan")
	}
	completionStatus := strings.ToLower(strings.TrimSpace(plan.CompletionStatus))
	result := &workflow.TaskRunResult{
		PlanID:           plan.ID,
		CompletionStatus: plan.CompletionStatus,
		Passed:           completionStatus == "validated" && plan.ValidationResult.Passed,
		ReviewRequired:   completionStatus == "review_required",
		FailureReason:    workflowTaskFailureReason(plan.ValidationResult.Failures...),
	}
	if result.ReviewRequired && plan.ReviewQueueItem != nil {
		result.FailureReason = workflowTaskFailureReason(append(
			[]string{plan.ReviewQueueItem.Reason}, plan.ValidationResult.Failures...,
		)...)
	}
	// Risk gates can stop a task before a runtime result exists. Carry that
	// approval request forward without relabelling missing prerequisites or
	// potentially executed work as a safe pre-execution approval.
	if taskPlanAwaitingApproval(plan) {
		result.ApprovalRequired = true
		reason := "approval required before task execution"
		if plan.ReviewQueueItem != nil {
			reason = firstNonEmpty(plan.ReviewQueueItem.Reason, reason)
		}
		result.FailureReason = workflowTaskFailureReason(reason)
	}
	var resultErr error
	if plan.ExecutionResult != nil {
		result.VerificationStatus = plan.ExecutionResult.VerificationStatus
		result.Output = plan.ExecutionResult.Output
		result.ExecutionOutcomeUncertain = task.ExecutionOutcomeUncertain(plan.ExecutionResult)
		if plan.ExecutionResult.ToolExecution != nil {
			tool := plan.ExecutionResult.ToolExecution
			launchEventID := strings.TrimSpace(tool.LaunchEventID)
			result.ExternalActionExecuted = strings.EqualFold(strings.TrimSpace(tool.Status), "completed")
			result.RuntimeRouteTrace = tool.RuntimeRouteTrace
			result.RuntimeEvidenceLabel = firstNonEmpty(
				tool.RuntimeType,
				tool.LaunchType,
				tool.Target,
				"controlled runtime launch",
			)
			if launchEventID != "" {
				eventID, parseErr := uuid.Parse(launchEventID)
				if parseErr != nil || eventID == uuid.Nil {
					resultErr = fmt.Errorf("workflow external action has an invalid launch-event evidence ID")
				} else {
					result.RuntimeEvidenceURI = "automation-launch://" + eventID.String()
				}
			}
			if result.ExternalActionExecuted && launchEventID == "" {
				resultErr = fmt.Errorf("completed workflow external action has no immutable launch-event evidence")
			}
		}
		result.ApprovalRequired = result.ApprovalRequired ||
			(plan.ExecutionResult.ToolExecution != nil && plan.ExecutionResult.ToolExecution.RequiresApproval)
		if plan.ExecutionResult.BlockedReason != "" {
			result.FailureReason = workflowTaskFailureReason(plan.ExecutionResult.BlockedReason)
		}
	}
	frameworkSelection, selectionErr := frameworkSelectionFromPlan(plan)
	if selectionErr == nil {
		result.FrameworkSelection = frameworkSelection
	} else {
		resultErr = firstWorkflowTaskError(resultErr, fmt.Errorf("workflow task framework selection: %w", selectionErr))
	}
	if selectionErr == nil {
		if err := enforceFrameworkRiskFloor(frameworkSelection, request.RiskLevel); err != nil {
			resultErr = firstWorkflowTaskError(resultErr, fmt.Errorf("workflow task framework selection: %w", err))
		} else if err := compareFrameworkRiskContracts(preflightSelection, frameworkSelection); err != nil {
			resultErr = firstWorkflowTaskError(resultErr, fmt.Errorf("workflow task framework selection changed after preflight: %w", err))
		}
	}
	if result.VerificationStatus == "" {
		result.VerificationStatus = plan.ValidationResult.Status
	}
	// Unknown effects require reconciliation, never approval-only routing or
	// optimistic validation, even if the receipt reports completed execution.
	if result.ExecutionOutcomeUncertain {
		result.Passed = false
		result.ReviewRequired = true
		result.ApprovalRequired = false
		result.CompletionStatus = "review_required"
		result.VerificationStatus = "uncertain"
		result.FailureReason = workflowTaskFailureReason(
			"controlled runtime outcome is uncertain; reconcile possible side effects before authorizing a separate attempt",
			result.FailureReason,
		)
	}
	if request.ExecutionContext != nil && request.ExecutionContext.Err() != nil {
		result.Passed = false
		result.ReviewRequired = true
		result.ApprovalRequired = false
		result.CompletionStatus = "review_required"
		result.VerificationStatus = "needs_review"
	}
	if runErr != nil && resultErr != nil {
		return result, errors.Join(runErr, resultErr)
	}
	if runErr != nil {
		return result, runErr
	}
	if resultErr != nil {
		return result, resultErr
	}
	return result, nil
}

const workflowTaskFailureReasonLimit = 2048

func workflowTaskFailureReason(values ...string) string {
	seen := make(map[string]struct{})
	result := ""
	for _, value := range values {
		value = strings.Join(strings.Fields(safety.RedactSecrets(value)), " ")
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		if result != "" {
			result += "; "
		}
		result += value
		if runes := []rune(result); len(runes) > workflowTaskFailureReasonLimit {
			return string(runes[:workflowTaskFailureReasonLimit-3]) + "..."
		}
	}
	return result
}

func taskPlanAwaitingApproval(plan *task.CompletionPlan) bool {
	if plan == nil || !strings.EqualFold(strings.TrimSpace(plan.CompletionStatus), "review_required") ||
		plan.ExecutionResult != nil || strings.TrimSpace(plan.ModelDecision.SelectedModelID) == "" {
		return false
	}
	resource := plan.ResourceDecision
	if resource == nil || resource.Authority != "advisory_only" || resource.CanExecute || resource.GrantsAuthority ||
		(resource.Feasibility != resourceplanner.Feasible && resource.Feasibility != resourceplanner.FeasibleWithApprovals) {
		return false
	}
	digest := strings.TrimSpace(resource.DecisionDigest)
	if len(digest) != sha256.Size*2 {
		return false
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return false
	}
	if domain := plan.DomainPackDecision; domain != nil && (!domain.AdvisoryOnly || domain.ExecutionAuthorityGranted) {
		return false
	}
	if selection := plan.FrameworkDecision; selection != nil {
		if selection.Capacity.Status == "unavailable" || selection.Capacity.Status == "overloaded" {
			return false
		}
		for _, action := range selection.ActionAutonomy {
			if action.Action == "execute_case_approved_action" && !action.Allowed {
				return false
			}
		}
	}
	risk := plan.RiskAssessment
	return risk.ApprovalRequired && !risk.ApprovalGranted && !risk.AllowedNow &&
		len(risk.MissingParameters) == 0 && len(risk.MissingRequiredAgents) == 0 &&
		(risk.ActionResolution == "" || risk.ActionResolution == "proceed") &&
		risk.FrameworkAutonomyCeiling >= risk.RequiredFrameworkAutonomy
}

func firstWorkflowTaskError(existing, next error) error {
	if existing != nil {
		return existing
	}
	return next
}

func workflowTaskOperationKey(request workflow.TaskRunRequest) string {
	key := "workflow:" + strings.TrimSpace(request.WorkflowID) + ":unapproved"
	if sourceID := strings.TrimSpace(request.ApprovalSourceID); sourceID != "" {
		key = "workflow:" + strings.TrimSpace(request.WorkflowID) + ":approval:" + sourceID
	}
	if !request.CoordinationPlan.IsZero() {
		binding := strings.Join([]string{
			request.CoordinationPlan.PlanID.String(),
			fmt.Sprintf("%d", request.CoordinationPlan.Revision),
			strings.ToLower(strings.TrimSpace(request.CoordinationPlan.Digest)),
			strings.TrimSpace(request.CoordinationPlan.NodeID),
		}, "\x1f")
		digest := sha256.Sum256([]byte(binding))
		key += fmt.Sprintf(":plan:%x", digest[:12])
	}
	if len(key) > 120 {
		digest := sha256.Sum256([]byte(key))
		key = "workflow:" + strings.TrimSpace(request.WorkflowID) + fmt.Sprintf(":operation:%x", digest[:16])
	}
	return key
}

func normalizeWorkflowTaskRequest(request workflow.TaskRunRequest) (workflow.TaskRunRequest, error) {
	request.OwnerIdentity = strings.TrimSpace(request.OwnerIdentity)
	request.WorkflowID = strings.TrimSpace(request.WorkflowID)
	request.Request = strings.TrimSpace(request.Request)
	request.ProjectKey = strings.TrimSpace(request.ProjectKey)
	request.AutomationID = strings.TrimSpace(request.AutomationID)
	request.MandateID = strings.TrimSpace(request.MandateID)
	if request.MandateID != "" {
		mandateID, err := uuid.Parse(request.MandateID)
		if err != nil || mandateID == uuid.Nil {
			return workflow.TaskRunRequest{}, fmt.Errorf("workflow task standing mandate ID must be a UUID")
		}
		request.MandateID = mandateID.String()
	}
	request.RiskLevel = strings.ToLower(strings.TrimSpace(request.RiskLevel))
	request.ApprovalNote = strings.TrimSpace(request.ApprovalNote)
	request.ApprovalSourceID = strings.TrimSpace(request.ApprovalSourceID)
	if request.OwnerIdentity == "" {
		return workflow.TaskRunRequest{}, fmt.Errorf("workflow task owner identity is required")
	}
	if request.WorkflowID == "" {
		return workflow.TaskRunRequest{}, fmt.Errorf("workflow task workflow ID is required")
	}
	if request.Request == "" {
		return workflow.TaskRunRequest{}, fmt.Errorf("workflow task request is required")
	}
	if request.RiskLevel == "" {
		request.RiskLevel = "low"
	}
	if _, _, err := workflowFrameworkRiskRank(request.RiskLevel); err != nil {
		return workflow.TaskRunRequest{}, fmt.Errorf("workflow task risk level %w", err)
	}
	if !request.HumanApproved {
		request.ApprovalNote = ""
		request.ApprovalSourceID = ""
		request.ApprovalBindingDigest = ""
		request.ApprovalActorIdentity = ""
		request.ApprovalApprovedAt = nil
		return request, nil
	}
	const prefix = "workflow-decision:"
	if !strings.HasPrefix(request.ApprovalSourceID, prefix) {
		return workflow.TaskRunRequest{}, fmt.Errorf("approved workflow task requires workflow-decision provenance")
	}
	decisionID, err := uuid.Parse(strings.TrimPrefix(request.ApprovalSourceID, prefix))
	if err != nil || decisionID == uuid.Nil {
		return workflow.TaskRunRequest{}, fmt.Errorf("approved workflow task requires a valid workflow decision UUID")
	}
	return request, nil
}

func frameworkSelectionFromPlan(plan *task.CompletionPlan) (*workflow.FrameworkSelectionProvenance, error) {
	if plan == nil {
		return nil, fmt.Errorf("task engine returned no plan")
	}
	if plan.FrameworkDecision == nil {
		return nil, fmt.Errorf("task plan %q has no framework selection decision", strings.TrimSpace(plan.ID))
	}
	decision := plan.FrameworkDecision
	riskContract, err := frameworkRiskContractFromDecision(decision)
	if err != nil {
		return nil, err
	}
	provenance := &workflow.FrameworkSelectionProvenance{
		SelectionDecisionID:       strings.TrimSpace(decision.ID),
		TaskPlanID:                strings.TrimSpace(decision.TaskPlanID),
		CatalogVersion:            strings.TrimSpace(decision.CatalogVersion),
		CatalogDigest:             strings.TrimSpace(decision.CatalogDigest),
		SelectorAlgorithmVersion:  strings.TrimSpace(decision.SelectorAlgorithmVersion),
		TaskRiskLevel:             riskContract.TaskRiskLevel,
		EffectiveRiskCeiling:      riskContract.EffectiveRiskCeiling,
		EffectivePreferenceDigest: strings.TrimSpace(decision.EffectivePreferenceDigest),
		ConstitutionVersion:       decision.ConstitutionVersion,
		ConstitutionDigest:        strings.TrimSpace(decision.ConstitutionDigest),
		ConstitutionSource:        strings.TrimSpace(decision.ConstitutionSource),
		OperatingContractDigest:   strings.TrimSpace(decision.OperatingContractDigest),
	}
	if strings.EqualFold(strings.TrimSpace(decision.SelectorAlgorithmVersion), "selector-v5") {
		maximumAutonomy := decision.MaximumAutonomyLevel
		requiresApproval := decision.RequiresApproval
		provenance.MaximumAutonomyLevel = &maximumAutonomy
		provenance.RequiresApproval = &requiresApproval
	}
	if err := provenance.Validate(plan.ID); err != nil {
		return nil, err
	}
	if err := enforceFrameworkPlanRisk(provenance, plan); err != nil {
		return nil, err
	}
	return provenance, nil
}

type frameworkRiskContract struct {
	TaskRiskLevel        string `json:"taskRiskLevel"`
	EffectiveRiskCeiling string `json:"effectiveRiskCeiling"`
}

func frameworkRiskContractFromDecision(decision interface{}) (frameworkRiskContract, error) {
	payload, err := json.Marshal(decision)
	if err != nil {
		return frameworkRiskContract{}, fmt.Errorf("encode framework selection risk contract: %w", err)
	}
	var contract frameworkRiskContract
	if err := json.Unmarshal(payload, &contract); err != nil {
		return frameworkRiskContract{}, fmt.Errorf("decode framework selection risk contract: %w", err)
	}
	contract.TaskRiskLevel = strings.ToLower(strings.TrimSpace(contract.TaskRiskLevel))
	contract.EffectiveRiskCeiling = strings.ToLower(strings.TrimSpace(contract.EffectiveRiskCeiling))
	return contract, nil
}

func enforceFrameworkRiskFloor(selection *workflow.FrameworkSelectionProvenance, requestedRisk string) error {
	if selection == nil {
		return fmt.Errorf("framework selection provenance is required")
	}
	if err := selection.Validate(selection.TaskPlanID); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(selection.SelectorAlgorithmVersion), "selector-v5") {
		return nil
	}
	requested, requestedRank, err := workflowFrameworkRiskRank(requestedRisk)
	if err != nil {
		return fmt.Errorf("workflow risk floor %w", err)
	}
	selected, selectedRank, err := workflowFrameworkRiskRank(selection.TaskRiskLevel)
	if err != nil {
		return fmt.Errorf("selector-v5 task risk level %w", err)
	}
	if selectedRank < requestedRank {
		return fmt.Errorf("selector-v5 task risk %q is below workflow risk floor %q", selected, requested)
	}
	return nil
}

func enforceFrameworkPlanRisk(selection *workflow.FrameworkSelectionProvenance, plan *task.CompletionPlan) error {
	if selection == nil || plan == nil || !strings.EqualFold(selection.SelectorAlgorithmVersion, "selector-v5") {
		return nil
	}
	planRisk := firstNonEmpty(plan.RiskAssessment.Level, plan.Intake.RiskLevel, "low")
	if err := enforceFrameworkRiskFloor(selection, planRisk); err != nil {
		return fmt.Errorf("selector-v5 risk contract does not cover task plan risk: %w", err)
	}
	return nil
}

func compareFrameworkRiskContracts(preflight, executed *workflow.FrameworkSelectionProvenance) error {
	if preflight == nil || executed == nil {
		return fmt.Errorf("preflight and executed framework selections are required")
	}
	if !strings.EqualFold(preflight.SelectorAlgorithmVersion, "selector-v5") &&
		!strings.EqualFold(executed.SelectorAlgorithmVersion, "selector-v5") {
		return nil
	}
	if !strings.EqualFold(preflight.SelectorAlgorithmVersion, executed.SelectorAlgorithmVersion) ||
		preflight.CatalogVersion != executed.CatalogVersion ||
		preflight.CatalogDigest != executed.CatalogDigest ||
		preflight.TaskRiskLevel != executed.TaskRiskLevel ||
		preflight.EffectiveRiskCeiling != executed.EffectiveRiskCeiling ||
		!equalOptionalInt(preflight.MaximumAutonomyLevel, executed.MaximumAutonomyLevel) ||
		!equalOptionalBool(preflight.RequiresApproval, executed.RequiresApproval) ||
		preflight.EffectivePreferenceDigest != executed.EffectivePreferenceDigest ||
		preflight.ConstitutionDigest != executed.ConstitutionDigest {
		return fmt.Errorf("selector-v5 risk contract or governing digests differ from the side-effect-free preview")
	}
	return nil
}

func equalOptionalInt(left, right *int) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && *left == *right)
}

func equalOptionalBool(left, right *bool) bool {
	return (left == nil && right == nil) ||
		(left != nil && right != nil && *left == *right)
}

func workflowFrameworkRiskRank(value string) (string, int, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "low":
		return normalized, 1, nil
	case "medium":
		return normalized, 2, nil
	case "high":
		return normalized, 3, nil
	case "critical":
		// The framework catalog currently has three selectable risk bands. This
		// adapter only compares that catalog contract; workflow and task state
		// retain the original critical classification and its approval gates.
		return "high", 3, nil
	default:
		return "", 0, fmt.Errorf("must be one of low, medium, high, or critical")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

package task

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/resourceplanner"
)

const (
	validationCriterionNotRun        = "not_run"
	validationCriterionPassed        = "passed"
	validationCriterionFailed        = "failed"
	validationCriterionNotApplicable = "not_applicable"
)

type ValidationCriterionResult struct {
	Criterion           string   `json:"criterion"`
	Kind                string   `json:"kind"`
	Status              string   `json:"status"`
	Evidence            []string `json:"evidence"`
	ApplicabilityReason string   `json:"applicabilityReason,omitempty"`
	Failure             string   `json:"failure,omitempty"`
}

func initialValidationResult(plan ValidationPlan) ValidationResult {
	return ValidationResult{
		Passed:        false,
		Status:        "not_run",
		Checked:       []string{},
		Failures:      []string{},
		Criteria:      plannedValidationCriteria(plan),
		NextAction:    "execute allowed steps, then validate",
		AttemptNumber: 0,
	}
}

func plannedValidationCriteria(plan ValidationPlan) []ValidationCriterionResult {
	criteria := make([]ValidationCriterionResult, 0,
		len(plan.SuccessCriteria)+
			len(plan.FrameworkEvidenceRequirements)+
			len(plan.FrameworkCompletionCriteria)+
			len(plan.FrameworkAssuranceCriteria),
	)
	appendCriteria := func(kind string, values []string) {
		for _, value := range uniqueStrings(values) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			criteria = append(criteria, ValidationCriterionResult{
				Criterion: value,
				Kind:      kind,
				Status:    validationCriterionNotRun,
				Evidence:  []string{},
			})
		}
	}
	appendCriteria("task_success", plan.SuccessCriteria)
	appendCriteria("framework_evidence", plan.FrameworkEvidenceRequirements)
	appendCriteria("framework_completion", plan.FrameworkCompletionCriteria)
	appendCriteria("framework_assurance", plan.FrameworkAssuranceCriteria)
	return criteria
}

func validatePlan(plan *CompletionPlan, attempt int) ValidationResult {
	if plan == nil {
		return ValidationResult{
			Passed:        false,
			Status:        "failed",
			Checked:       []string{"completion plan is present"},
			Failures:      []string{"completion plan is missing"},
			Criteria:      []ValidationCriterionResult{},
			NextAction:    "retry, escalate, or request review",
			AttemptNumber: attempt,
		}
	}

	failures := []string{}
	checked := []string{}
	criteria := []ValidationCriterionResult{}
	recordCheck := func(name string, passed bool, failure string, evidence ...string) {
		name = strings.TrimSpace(name)
		checked = append(checked, name)
		result := ValidationCriterionResult{
			Criterion: name,
			Kind:      "system_check",
			Status:    validationCriterionPassed,
			Evidence:  uniqueStrings(evidence),
		}
		if !passed {
			result.Status = validationCriterionFailed
			result.Failure = strings.TrimSpace(failure)
			failures = append(failures, result.Failure)
		}
		criteria = append(criteria, result)
	}

	recordCheck(
		"explicit success criteria are present",
		len(plan.Intake.SuccessCriteria) > 0,
		"success criteria are missing",
		fmt.Sprintf("criteria-count:%d", len(plan.Intake.SuccessCriteria)),
	)
	deterministicRuntime := attempt > 0 && plan.ExecutionResult != nil && !executionOutcomeUncertain(plan.ExecutionResult) &&
		deterministicReadOnlyRuntimeCompleted(plan.ExecutionResult.ToolExecution)
	modelEvidence := "model:" + strings.TrimSpace(plan.ModelDecision.SelectedModelID)
	if deterministicRuntime && strings.TrimSpace(plan.ModelDecision.SelectedModelID) == "" {
		modelEvidence = "model:not-required:deterministic-read-only-runtime"
	}
	recordCheck(
		"a capable model was selected",
		strings.TrimSpace(plan.ModelDecision.SelectedModelID) != "" || deterministicRuntime,
		"no capable model was selected",
		modelEvidence,
	)
	selectedFrameworks := selectedFrameworkEvidence(plan)
	recordCheck(
		"operating frameworks were selected",
		plan.FrameworkDecision != nil && len(plan.FrameworkDecision.Selected) > 0,
		"no operating frameworks were selected",
		selectedFrameworks...,
	)
	resourceEvidence := []string{}
	resourceValid := plan.ResourceDecision != nil
	if plan.ResourceDecision != nil {
		resourceEvidence = append(resourceEvidence,
			"resource-feasibility:"+string(plan.ResourceDecision.Feasibility),
			"resource-decision-digest:"+strings.TrimSpace(plan.ResourceDecision.DecisionDigest),
		)
		resourceValid = plan.ResourceDecision.Authority == "advisory_only" &&
			!plan.ResourceDecision.CanExecute && !plan.ResourceDecision.GrantsAuthority &&
			plan.ResourceDecision.Feasibility != resourceplanner.Infeasible &&
			strings.TrimSpace(plan.ResourceDecision.DecisionDigest) != ""
	}
	recordCheck(
		"resource and time feasibility was evaluated",
		resourceValid,
		"resource or time planning is missing, invalid, or infeasible",
		resourceEvidence...,
	)
	recordCheck(
		"required tools were selected",
		len(plan.ToolDecision.SelectedTools) > 0,
		"no tools were selected",
		plan.ToolDecision.SelectedTools...,
	)
	if plan.MinimalityDecision.Applicable {
		recordCheck(
			"implementation passed the necessity gate",
			plan.MinimalityDecision.Necessary,
			"implementation was rejected by the necessity gate",
			"selected-level:"+strings.TrimSpace(plan.MinimalityDecision.SelectedLevel),
		)
		recordCheck(
			"a minimality strategy was selected",
			strings.TrimSpace(plan.MinimalityDecision.SelectedLevel) != "",
			"minimality strategy was not selected",
			"selected-strategy:"+strings.TrimSpace(plan.MinimalityDecision.SelectedStrategy),
		)
	}
	approvalSatisfied := !plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.ApprovalGranted
	approvalEvidence := "approval:not-required"
	if plan.RiskAssessment.ApprovalRequired {
		approvalEvidence = fmt.Sprintf("approval-granted:%t", plan.RiskAssessment.ApprovalGranted)
	}
	recordCheck(
		"required approval was recorded",
		approvalSatisfied,
		"approval is required before execution",
		approvalEvidence,
	)
	if frameworkEvidencePreflightRequired(plan.ValidationPlan.FrameworkEvidenceContracts) {
		preflightPassed := plan.FrameworkEvidencePreflight != nil && plan.FrameworkEvidencePreflight.Passed
		preflightEvidence := frameworkEvidencePreflightSummary(plan.FrameworkEvidencePreflight)
		recordCheck(
			"framework evidence preconditions passed before execution",
			preflightPassed,
			"framework evidence preconditions were not verified before execution",
			preflightEvidence...,
		)
	}

	executionReady := false
	toolReady := !plan.Intake.NeedsTools && !plan.Intake.NeedsLocalExecution
	outputReady := false
	verificationReady := false
	claimsReady := false
	if attempt > 0 {
		executionReady = plan.ExecutionResult != nil
		recordCheck(
			"an execution result was produced",
			executionReady,
			"no execution result was produced",
		)

		if executionReady {
			result := plan.ExecutionResult
			recordCheck(
				"runtime outcome is established before completion",
				!executionOutcomeUncertain(result),
				uncertainToolOutcomeReason,
			)
			if plan.Intake.NeedsTools || plan.Intake.NeedsLocalExecution {
				toolReady = result.ToolExecution != nil && result.ToolExecution.Status == "completed" && !executionOutcomeUncertain(result)
				toolFailure := "required controlled runtime execution did not run"
				if result.ToolExecution != nil {
					toolFailure = "controlled runtime execution did not complete: " + result.ToolExecution.Status
				}
				recordCheck(
					"controlled runtime execution completed",
					toolReady,
					toolFailure,
					toolExecutionValidationEvidence(result.ToolExecution)...,
				)
			}

			outputReady = strings.TrimSpace(result.Output) != ""
			recordCheck(
				"execution produced output",
				outputReady,
				"execution produced no output",
			)
			verificationReady = verificationStatusAcceptsCompletion(result.VerificationStatus)
			recordCheck(
				"execution output passed verification",
				verificationReady,
				"execution output is not verified: "+firstNonEmpty(result.VerificationStatus, "unknown"),
				"verification-status:"+firstNonEmpty(result.VerificationStatus, "unknown"),
			)
			recordCheck(
				"execution has no unsupported claims",
				result.UnsupportedClaims == 0,
				"execution has unsupported or review-needed claims",
				fmt.Sprintf("unsupported-claims:%d", result.UnsupportedClaims),
			)
			claimsReady = len(result.Claims) > 0
			claimFailure := "verification produced no atomic claims"
			if claimsReady {
				for _, claim := range result.Claims {
					if claim.NeedsReview || !verificationStatusAcceptsCompletion(claim.Status) {
						claimsReady = false
						claimFailure = "claim requires review: " + compact(claim.ClaimText)
						break
					}
				}
			}
			recordCheck(
				"all atomic claims passed verification",
				claimsReady,
				claimFailure,
				fmt.Sprintf("claims-count:%d", len(result.Claims)),
			)
		} else {
			recordCheck(
				"execution produced output",
				false,
				"execution produced no output",
			)
			recordCheck(
				"execution output passed verification",
				false,
				"execution output is not verified: not_run",
			)
			recordCheck(
				"execution has no unsupported claims",
				false,
				"unsupported claims could not be evaluated without an execution result",
			)
			recordCheck(
				"all atomic claims passed verification",
				false,
				"atomic claims could not be evaluated without an execution result",
			)
		}
	}

	postconditionsPassed := attempt > 0 &&
		executionReady &&
		toolReady &&
		outputReady &&
		verificationReady &&
		claimsReady &&
		approvalSatisfied &&
		(plan.ExecutionResult == nil || plan.ExecutionResult.UnsupportedClaims == 0)
	for _, planned := range plannedValidationCriteria(plan.ValidationPlan) {
		checked = append(checked, planned.Kind+": "+planned.Criterion)
		if planned.Kind == "framework_assurance" {
			planned.Status = validationCriterionNotApplicable
			planned.ApplicabilityReason = "evaluated by registry assurance and longitudinal evaluation, not by each task run"
			planned.Evidence = []string{"evaluation-scope:framework-assurance"}
			criteria = append(criteria, planned)
			continue
		}
		if planned.Kind == "framework_evidence" {
			applicable, reason := frameworkEvidenceRequirementApplies(plan, planned.Criterion)
			if !applicable {
				planned.Status = validationCriterionNotApplicable
				planned.ApplicabilityReason = reason
				planned.Evidence = []string{"applicability:not-required-for-this-task"}
				criteria = append(criteria, planned)
				continue
			}
			if contracts := frameworkEvidenceContractsForRequirement(
				plan.ValidationPlan.FrameworkEvidenceContracts,
				planned.Criterion,
			); len(contracts) > 0 {
				planned.Status, planned.Evidence, planned.Failure =
					evaluateTypedFrameworkEvidence(plan, contracts)
				if planned.Status == validationCriterionFailed {
					failures = append(failures, planned.Failure+": "+planned.Criterion)
				}
				criteria = append(criteria, planned)
				continue
			}
		}
		planned.Status = validationCriterionPassed
		planned.Evidence = criterionValidationEvidence(plan, planned)
		if !postconditionsPassed {
			planned.Status = validationCriterionFailed
			planned.Failure = "criterion was not accepted because execution postconditions did not pass"
		} else if planned.Kind == "framework_evidence" &&
			(plan.ExecutionResult == nil || plan.ExecutionResult.EvidenceCount == 0) {
			planned.Status = validationCriterionFailed
			planned.Failure = "framework evidence requirement has no source or runtime evidence"
		} else if len(planned.Evidence) == 0 {
			planned.Status = validationCriterionFailed
			planned.Failure = "criterion has no directly related verified evidence"
		}
		if planned.Status == validationCriterionFailed {
			failures = append(failures, planned.Failure+": "+planned.Criterion)
		}
		criteria = append(criteria, planned)
	}

	failures = uniqueStrings(failures)
	checked = uniqueStrings(checked)
	passed := len(failures) == 0
	status := "passed"
	next := "mark task complete"
	if !passed {
		status = "failed"
		next = "retry, escalate, or request review"
	}
	return ValidationResult{
		Passed:        passed,
		Status:        status,
		Checked:       checked,
		Failures:      failures,
		Criteria:      criteria,
		NextAction:    next,
		AttemptNumber: attempt,
	}
}

func verificationQuestion(plan *CompletionPlan) string {
	if plan == nil {
		return ""
	}
	parts := []string{strings.TrimSpace(plan.RealGoal)}
	appendSection := func(title string, values []string) {
		values = uniqueStrings(values)
		if len(values) == 0 {
			return
		}
		parts = append(parts, title+":")
		for _, value := range values {
			parts = append(parts, "- "+strings.TrimSpace(value))
		}
	}
	appendSection("Task success criteria", plan.ValidationPlan.SuccessCriteria)
	appendSection("Framework evidence requirements", plan.ValidationPlan.FrameworkEvidenceRequirements)
	appendSection("Framework completion criteria", plan.ValidationPlan.FrameworkCompletionCriteria)
	return sanitizeTaskOperationalText(strings.Join(parts, "\n"), 8192)
}

func selectedFrameworkEvidence(plan *CompletionPlan) []string {
	if plan == nil || plan.FrameworkDecision == nil {
		return []string{}
	}
	evidence := make([]string, 0, len(plan.FrameworkDecision.Selected))
	for _, selected := range plan.FrameworkDecision.Selected {
		evidence = append(evidence, "framework:"+selected.ID+"@"+selected.Version)
	}
	return uniqueStrings(evidence)
}

func frameworkEvidenceRequirementApplies(plan *CompletionPlan, criterion string) (bool, string) {
	if plan == nil {
		return true, ""
	}
	normalized := strings.ToLower(strings.TrimSpace(criterion))
	switch normalized {
	case "applicable approval record", "standing mandate or case approval":
		if !plan.RiskAssessment.ApprovalRequired {
			return false, "the classified action does not require an approval mandate"
		}
	case "verified identity", "verified operator identity":
		if strings.TrimSpace(plan.OwnerIdentity) == "" &&
			!plan.RiskAssessment.ApprovalRequired &&
			strings.EqualFold(plan.RiskAssessment.Level, "low") {
			return false, "no consequential identity-bound action is being authorized"
		}
	case "status and ownership":
		if strings.TrimSpace(plan.OwnerIdentity) == "" &&
			!plan.RiskAssessment.ApprovalRequired &&
			strings.EqualFold(plan.RiskAssessment.Level, "low") {
			return false, "the internal low-risk task call has no authenticated owner-bound side effect"
		}
	case "alternative explanations", "stated assumptions":
		if plan.Intake.Difficulty < 7 &&
			!plan.Intake.NeedsDocuments &&
			!plan.Intake.NeedsWebAccess &&
			!strings.Contains(strings.ToLower(plan.Intake.TaskType), "research") &&
			!strings.Contains(strings.ToLower(plan.Intake.TaskType), "decision") {
			return false, "the task is a bounded deterministic operation rather than an uncertain diagnosis or forecast"
		}
	case "source authority and freshness", "fresh appointment information", "current route or timetable source",
		"verified contact route", "source-linked entity references", "source-backed deadlines", "retrieved evidence",
		"source reference", "original source link", "source uri and freshness", "claim-to-source links",
		"support for factual claims", "asset or property record", "quote or maintenance source",
		"customer or market evidence", "dated timeline", "claim-to-evidence map", "deadline provenance",
		"current recovery plan", "backup status", "upstream license and maintenance review",
		"license and maintenance status", "threat model", "privacy review", "coverage gap",
		"benchmark against postgresql baseline":
		if len(plan.ContextPlan.SourceContext) == 0 && !plan.Intake.NeedsDocuments && !plan.Intake.NeedsWebAccess {
			return false, "the task does not depend on connected-source evidence"
		}
	case "artifact provenance":
		if !plan.Intake.NeedsTools &&
			!plan.Intake.NeedsLocalExecution &&
			!strings.EqualFold(plan.Intake.TaskType, "coding") {
			return false, "the task does not produce or mutate an executable artifact"
		}
	case "idempotency key", "action receipt", "postcondition verification", "runtime boundary":
		if !plan.Intake.NeedsTools && !plan.Intake.NeedsLocalExecution {
			return false, "the task does not cross the controlled runtime boundary"
		}
	case "provider health", "capability profile", "price and quota data":
		if modelRouteIsNotRequiredForDeterministicRuntime(plan) {
			return false, "a low-risk controlled runtime is the sole execution path; no model route is required"
		}
	case "deterministic checks where possible":
		if !plan.Intake.NeedsTools &&
			!plan.Intake.NeedsLocalExecution &&
			!strings.EqualFold(plan.Intake.TaskType, "coding") &&
			!strings.Contains(strings.ToLower(plan.Request), "calculate") &&
			!strings.Contains(strings.ToLower(plan.Request), "reconcile") {
			return false, "no deterministic runtime, code, or calculation check is applicable"
		}
	case "threat scenario", "trust-boundary map", "mitigation and residual risk":
		if !plan.Intake.NeedsTools &&
			!plan.Intake.NeedsLocalExecution &&
			!plan.Intake.NeedsDocuments &&
			!plan.Intake.NeedsWebAccess {
			return false, "the task has no external, document, web, or runtime trust boundary"
		}
	}
	return true, ""
}

// modelRouteIsNotRequiredForDeterministicRuntime is deliberately narrow. It
// does not treat an unavailable model as a substitute for reasoning. It only
// applies when a low-risk, approval-free task has already been admitted to the
// controlled runtime boundary and no model was selected for the plan. The
// runtime executor still validates the exact allowlisted action and emits the
// launch evidence used by the execution and postcondition gates.
func modelRouteIsNotRequiredForDeterministicRuntime(plan *CompletionPlan) bool {
	if plan == nil || strings.TrimSpace(plan.ModelDecision.SelectedModelID) != "" {
		return false
	}
	if !strings.EqualFold(plan.RiskAssessment.Level, "low") ||
		plan.RiskAssessment.ApprovalRequired || !plan.RiskAssessment.AllowedNow {
		return false
	}
	return plan.Intake.NeedsTools || plan.Intake.NeedsLocalExecution
}

func taskDataClassification(plan *CompletionPlan) string {
	if plan == nil {
		return "unknown"
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		if ranked.Extraction.Sensitive {
			return "sensitive-source-derived"
		}
	}
	for _, suggestion := range plan.ContextPlan.LifeContext {
		if suggestion.Entity.LocalOnly || suggestion.Entity.Sensitivity == lifeontology.SensitivitySensitive || suggestion.Entity.Sensitivity == lifeontology.SensitivityRestricted {
			return "sensitive-whole-life-context"
		}
	}
	if plan.Intake.NeedsDocuments || len(plan.ContextPlan.SourceContext) > 0 {
		return "source-derived"
	}
	return "operational"
}

func taskProcessingLocationEvidence(plan *CompletionPlan) []string {
	if plan == nil {
		return []string{"processing-location:unknown"}
	}
	evidence := []string{}
	if provider := strings.TrimSpace(plan.ModelDecision.SelectedProviderID); provider != "" {
		evidence = append(evidence, "model-provider:"+provider)
	}
	if plan.ExecutionResult != nil && plan.ExecutionResult.ToolExecution != nil {
		tool := plan.ExecutionResult.ToolExecution
		evidence = append(evidence,
			"runtime-type:"+firstNonEmpty(tool.RuntimeType, tool.LaunchType, "unknown"),
		)
		if target := strings.TrimSpace(tool.Target); target != "" {
			evidence = append(evidence, "runtime-target:"+target)
		}
	}
	if len(evidence) == 0 {
		evidence = append(evidence, "processing-location:no-external-runtime")
	}
	return uniqueStrings(evidence)
}

func taskKnownLimitations(result *ExecutionResult) string {
	if result == nil {
		return "execution-result-unavailable"
	}
	if reason := strings.TrimSpace(result.BlockedReason); reason != "" {
		return compact(reason)
	}
	if result.UnsupportedClaims > 0 {
		return fmt.Sprintf("%d-unsupported-claims", result.UnsupportedClaims)
	}
	return "none-recorded-by-verifier"
}

func taskMemoryEvidence(plan *CompletionPlan) []string {
	if plan == nil || len(plan.ContextPlan.UsedContext) == 0 {
		return []string{"memory-type:none-retrieved"}
	}
	evidence := make([]string, 0, len(plan.ContextPlan.UsedContext))
	for _, ranked := range plan.ContextPlan.UsedContext {
		evidence = append(evidence,
			"memory-type:"+firstNonEmpty(strings.TrimSpace(ranked.Memory.Kind), "unspecified"),
		)
	}
	return uniqueStrings(evidence)
}

func taskRetentionEvidence(plan *CompletionPlan) []string {
	if plan == nil {
		return []string{"retention-state:unknown"}
	}
	evidence := []string{}
	if len(plan.ContextPlan.UsedContext) == 0 {
		evidence = append(evidence, "retention-state:no-memory-context-retained")
	} else {
		evidence = append(evidence, "retention-state:existing-owner-scoped-memory")
	}
	if len(plan.MemoryUpdateProposals) > 0 {
		evidence = append(evidence, "memory-update:pending-verified-completion")
	}
	return evidence
}

func taskConfidenceEvidence(plan *CompletionPlan) []string {
	if plan == nil {
		return []string{"confidence:unknown"}
	}
	evidence := []string{}
	for _, ranked := range plan.ContextPlan.UsedContext {
		evidence = append(evidence,
			fmt.Sprintf("memory-confidence:%.2f", ranked.Memory.Confidence),
			fmt.Sprintf("memory-relevance:%.2f", ranked.Score),
		)
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		evidence = append(evidence, fmt.Sprintf("source-relevance:%.2f", ranked.Score))
	}
	for _, suggestion := range plan.ContextPlan.LifeContext {
		evidence = append(evidence,
			fmt.Sprintf("life-context-confidence:%.2f", suggestion.Entity.Confidence),
			fmt.Sprintf("life-context-score:%d", suggestion.Score),
		)
	}
	if plan.ExecutionResult != nil {
		for _, claim := range plan.ExecutionResult.Claims {
			if claim.Confidence > 0 {
				evidence = append(evidence, fmt.Sprintf("claim-confidence:%.2f", claim.Confidence))
			}
		}
	}
	if len(evidence) == 0 {
		evidence = append(evidence, "confidence:no-numeric-score-produced")
	}
	return uniqueStrings(evidence)
}

type criterionEvidenceCandidate struct {
	description       string
	evidence          []string
	genericProvenance bool
}

func criterionValidationEvidence(
	plan *CompletionPlan,
	criterion ValidationCriterionResult,
) []string {
	if plan == nil || plan.ExecutionResult == nil {
		return []string{}
	}
	result := plan.ExecutionResult
	candidates := []criterionEvidenceCandidate{}
	add := func(description string, evidence ...string) {
		candidates = append(candidates, criterionEvidenceCandidate{
			description: description,
			evidence:    uniqueStrings(evidence),
		})
	}
	addGenericProvenance := func(description string, evidence ...string) {
		candidates = append(candidates, criterionEvidenceCandidate{
			description:       description,
			evidence:          uniqueStrings(evidence),
			genericProvenance: true,
		})
	}

	if strings.TrimSpace(plan.Intake.Reason) != "" {
		add(
			"classification confidence and reason",
			"task-type:"+firstNonEmpty(plan.Intake.TaskType, "unknown"),
			"classification-confidence:deterministic-rule-match",
			"classification-reason:"+strings.TrimSpace(plan.Intake.Reason),
		)
	}
	add(
		"explicit desired outcome",
		"task-plan:"+strings.TrimSpace(plan.ID),
		"real-goal:"+strings.TrimSpace(plan.RealGoal),
	)
	add(
		"original input retained as untrusted source",
		"input-provenance:task-plan:"+strings.TrimSpace(plan.ID),
		"input-trust:untrusted",
	)
	if len(plan.ValidationPlan.SuccessCriteria) > 0 {
		add(
			"evaluation dataset or criteria",
			fmt.Sprintf("success-criteria-count:%d", len(plan.ValidationPlan.SuccessCriteria)),
		)
	}
	add(
		"current mode",
		"execution-mode:"+firstNonEmpty(result.Mode, "action"),
	)
	add(
		"risk classification",
		"risk-level:"+firstNonEmpty(plan.RiskAssessment.Level, plan.Intake.RiskLevel, "unknown"),
	)
	if plan.FrameworkDecision != nil {
		add(
			"policy decision",
			"framework-selection:"+strings.TrimSpace(plan.FrameworkDecision.ID),
			fmt.Sprintf("autonomy-required:%d", plan.RiskAssessment.RequiredFrameworkAutonomy),
			fmt.Sprintf("autonomy-ceiling:%d", plan.RiskAssessment.FrameworkAutonomyCeiling),
		)
	}
	decisionEvidence := []string{
		"decision-goal:" + strings.TrimSpace(plan.RealGoal),
		"decision-risk:" + firstNonEmpty(plan.RiskAssessment.Level, plan.Intake.RiskLevel, "unknown"),
	}
	if plan.FrameworkDecision != nil {
		decisionEvidence = append(decisionEvidence,
			"framework-selection:"+strings.TrimSpace(plan.FrameworkDecision.ID),
		)
	}
	add("decision context", decisionEvidence...)
	recoveryEvidence := []string{
		fmt.Sprintf("retry-max-attempts:%d", plan.RetryPolicy.MaxAttempts),
	}
	for _, path := range plan.RetryPolicy.EscalationPath {
		recoveryEvidence = append(recoveryEvidence, "recovery-path:"+strings.TrimSpace(path))
	}
	add("accessible recovery path", recoveryEvidence...)
	statusEvidence := []string{
		"task-state:" + firstNonEmpty(plan.CompletionStatus, "in_progress"),
	}
	if owner := strings.TrimSpace(plan.OwnerIdentity); owner != "" {
		statusEvidence = append(statusEvidence, "task-owner:"+owner)
	}
	add("status and ownership", statusEvidence...)
	add(
		"data classification",
		"data-classification:"+taskDataClassification(plan),
	)
	add(
		"processing location",
		taskProcessingLocationEvidence(plan)...,
	)
	permissionEvidence := []string{"purpose:" + strings.TrimSpace(plan.RealGoal)}
	if plan.RiskAssessment.ApprovalRequired {
		permissionEvidence = append(permissionEvidence,
			fmt.Sprintf("approval-granted:%t", plan.RiskAssessment.ApprovalGranted),
		)
	} else {
		permissionEvidence = append(permissionEvidence, "approval:not-required")
	}
	add("purpose and permission", permissionEvidence...)
	add(
		"known limitations",
		"known-limitations:"+taskKnownLimitations(result),
	)
	add(
		"memory type",
		taskMemoryEvidence(plan)...,
	)
	add(
		"retention state",
		taskRetentionEvidence(plan)...,
	)
	add(
		"retrieved evidence",
		fmt.Sprintf("memory-context-count:%d", len(plan.ContextPlan.UsedContext)),
		fmt.Sprintf("source-context-count:%d", len(plan.ContextPlan.SourceContext)),
		fmt.Sprintf("life-context-count:%d", len(plan.ContextPlan.LifeContext)),
		fmt.Sprintf("verification-evidence-count:%d", result.EvidenceCount),
	)
	add(
		"confidence",
		taskConfidenceEvidence(plan)...,
	)
	parentEvidence := []string{"task-plan:" + strings.TrimSpace(plan.ID)}
	if strings.TrimSpace(plan.PursuitID) != "" {
		parentEvidence = append(parentEvidence, "pursuit:"+strings.TrimSpace(plan.PursuitID))
	}
	add("traceable parent pursuit or reviewable candidate", parentEvidence...)
	if owner := strings.TrimSpace(plan.OwnerIdentity); owner != "" {
		add("verified identity verified operator identity", "owner-identity:"+owner)
	}
	if plan.Intake.NeedsTools || plan.Intake.NeedsLocalExecution ||
		plan.Intake.NeedsDocuments || plan.Intake.NeedsWebAccess {
		add(
			"threat scenario",
			"threat-scenario:untrusted-external-or-runtime-content",
		)
		add(
			"trust boundary map",
			"trust-boundary:operator->task-engine->scoped-source-or-runtime",
		)
		add(
			"mitigation and residual risk",
			"mitigation:untrusted-content-cannot-change-authority",
			"residual-risk:"+firstNonEmpty(plan.RiskAssessment.Level, "unknown"),
		)
	}
	if strings.TrimSpace(result.Output) != "" {
		add(
			"user request answered implemented deliverable output result exists produced",
			"execution-output:present",
		)
	}
	if verificationStatusAcceptsCompletion(result.VerificationStatus) {
		add(
			"result deliverable validated verified before completion marked complete",
			"verification-status:"+result.VerificationStatus,
		)
	}
	if result.UnsupportedClaims == 0 {
		add(
			"unsupported claims rejected no unsupported claims accepted",
			"unsupported-claims:0",
		)
	}
	if verificationStatusAcceptsCompletion(result.VerificationStatus) &&
		result.UnsupportedClaims == 0 {
		add(
			"all consequential claims and actions satisfy the selected evidence requirements",
			"verification-status:"+result.VerificationStatus,
			"unsupported-claims:0",
		)
		add(
			"reproducible result",
			"verification-status:"+result.VerificationStatus,
			fmt.Sprintf("verified-claims:%d", len(result.Claims)),
		)
	}
	if result.EvidenceCount > 0 {
		add(
			"evidence collected available",
			fmt.Sprintf("evidence-count:%d", result.EvidenceCount),
		)
	}
	if !plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.ApprovalGranted {
		approval := "approval:not-required"
		if plan.RiskAssessment.ApprovalRequired {
			approval = "approval-granted:true"
		}
		add(
			"approval review gate satisfied complete authorized permission recorded",
			approval,
		)
	}
	if plan.RiskAssessment.ApprovalGranted && result.ToolExecution != nil &&
		strings.TrimSpace(result.ToolExecution.LaunchEventID) != "" {
		approvalSource := strings.TrimSpace(plan.RiskAssessment.ApprovalSourceID)
		if approvalSource != "" {
			add(
				"applicable approval record standing mandate or case approval",
				"approval-source:"+approvalSource,
			)
		}
		if actor := strings.TrimSpace(plan.RiskAssessment.ApprovalActorIdentity); actor != "" {
			add("approver identity", "approval-actor:"+actor)
		}
		add(
			"exact proposed action",
			"task-plan:"+strings.TrimSpace(plan.ID),
			"automation:"+strings.TrimSpace(result.ToolExecution.AutomationID),
			"launch-event:"+strings.TrimSpace(result.ToolExecution.LaunchEventID),
		)
		add(
			"risk and consequences",
			"risk-level:"+firstNonEmpty(plan.RiskAssessment.Level, "unknown"),
			"risk-reasons:"+strings.Join(plan.RiskAssessment.Reasons, "; "),
		)
		add(
			"scope and expiry",
			"approval-scope:exact-reviewed-task",
			"approval-freshness:verified-at-execution",
		)
	}
	if strings.TrimSpace(plan.ContextPlan.Explanation) != "" &&
		strings.TrimSpace(plan.ModelDecision.Reason) != "" {
		add(
			"selected context and model choice are explained",
			"context-explanation:present",
			"model:"+strings.TrimSpace(plan.ModelDecision.SelectedModelID),
			"model-reason:present",
		)
	}
	if plan.FrameworkDecision != nil &&
		plan.FrameworkDecision.ConstitutionVersion > 0 &&
		strings.TrimSpace(plan.FrameworkDecision.ConstitutionDigest) != "" {
		add(
			"active constitution version digest source",
			fmt.Sprintf("constitution-version:%d", plan.FrameworkDecision.ConstitutionVersion),
			"constitution-digest:"+strings.TrimSpace(plan.FrameworkDecision.ConstitutionDigest),
		)
	}
	for index, claim := range result.Claims {
		if claim.NeedsReview || !verificationStatusAcceptsCompletion(claim.Status) {
			continue
		}
		claimID := fmt.Sprintf("claim-index:%d", index)
		if claim.ID != [16]byte{} {
			claimID = "claim:" + claim.ID.String()
		}
		claimEvidence := []string{claimID}
		if ref := strings.TrimSpace(claim.SourceRefs); ref != "" {
			claimEvidence = append(claimEvidence, "claim-source:"+ref)
			add(
				"claim to source links",
				claimEvidence...,
			)
		}
		add(claim.ClaimText+" "+claim.SupportExplanation, claimEvidence...)
		if strings.TrimSpace(claim.SourceRefs) != "" {
			addGenericProvenance(
				"source reference citation provenance retained linked persisted grounded evidence",
				claimEvidence...,
			)
		}
	}
	if result.ToolExecution != nil && result.ToolExecution.Status == "completed" {
		tool := result.ToolExecution
		add(
			strings.Join([]string{
				"controlled runtime tool execution completed test checks run automation script command",
				tool.RuntimeType,
				tool.LaunchType,
				tool.Target,
				tool.Message,
				tool.Output,
			}, " "),
			toolExecutionValidationEvidence(tool)...,
		)
		add(
			"action receipt idempotency key postcondition verification artifact provenance reproducible result runtime boundary source reference retrieved evidence",
			toolExecutionValidationEvidence(tool)...,
		)
		add(
			"claim to source links source authority and freshness deterministic checks where possible",
			append(
				toolExecutionValidationEvidence(tool),
				"source-authority:deterministic-runtime",
				"source-freshness:"+tool.ExecutedAt.UTC().Format(time.RFC3339Nano),
			)...,
		)
	}
	for _, ranked := range plan.ContextPlan.UsedContext {
		memoryEvidence := []string{
			"memory:" + ranked.Memory.ID.String(),
			"source-authority:owner-scoped-memory",
		}
		if !ranked.Memory.UpdatedAt.IsZero() {
			memoryEvidence = append(memoryEvidence,
				"source-freshness:"+ranked.Memory.UpdatedAt.UTC().Format(time.RFC3339Nano),
			)
		}
		add("source authority and freshness", memoryEvidence...)
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		sourceEvidence := []string{
			"source-extraction:" + ranked.Extraction.ID.String(),
			"source-authority:connected-source",
		}
		if !ranked.Extraction.UpdatedAt.IsZero() {
			sourceEvidence = append(sourceEvidence,
				"source-freshness:"+ranked.Extraction.UpdatedAt.UTC().Format(time.RFC3339Nano),
			)
		}
		add("source authority and freshness", sourceEvidence...)
	}
	for _, suggestion := range plan.ContextPlan.LifeContext {
		entity := suggestion.Entity
		lifeEvidence := []string{
			"life-ontology-entity:" + entity.ID,
			"source-authority:owner-scoped-life-ontology",
			"source-freshness:" + entity.ObservedAt.UTC().Format(time.RFC3339Nano),
		}
		for _, provenance := range entity.Provenance {
			if strings.TrimSpace(provenance.ReferenceID) != "" {
				lifeEvidence = append(lifeEvidence, "source-reference:"+provenance.ReferenceID)
			}
		}
		add("source authority and freshness", lifeEvidence...)
	}
	if plan.FrameworkDecision != nil && len(plan.FrameworkDecision.Selected) > 0 {
		descriptions := []string{"selected operating frameworks"}
		for _, framework := range plan.FrameworkDecision.Selected {
			descriptions = append(descriptions, framework.ID, framework.Name)
		}
		add(strings.Join(descriptions, " "), selectedFrameworkEvidence(plan)...)
	}

	evidence := []string{}
	for _, candidate := range candidates {
		if candidate.genericProvenance &&
			!criterionIsProvenanceOrSourceRetention(criterion.Criterion) {
			continue
		}
		if criterionEvidenceMatches(criterion.Criterion, candidate.description) {
			evidence = append(evidence, candidate.evidence...)
		}
	}
	return uniqueStrings(evidence)
}

func criterionEvidenceMatches(criterion, description string) bool {
	if !validationValuesCompatible(criterion, description) ||
		!validationNegationCompatible(criterion, description) {
		return false
	}

	required := validationConcepts(criterion)
	available := validationConcepts(description)
	if len(required) == 0 || len(available) == 0 {
		return false
	}
	availableSet := make(map[string]struct{}, len(available))
	for _, concept := range available {
		availableSet[concept] = struct{}{}
	}
	matches := 0
	for _, concept := range required {
		if _, ok := availableSet[concept]; ok {
			matches++
		}
	}
	if len(required) == 1 {
		return matches == 1
	}
	return matches >= 2
}

var (
	validationCurrencyValuePattern = regexp.MustCompile(
		`(?i)(?:\b(?:eur|usd|gbp)\s*\d+(?:[.,]\d+)?\b|\b\d+(?:[.,]\d+)?\s*(?:eur|usd|gbp)\b|(?:\$|\x{20AC}|\x{00A3})\s*\d+(?:[.,]\d+)?)`,
	)
	validationDateValuePattern = regexp.MustCompile(
		`\b(?:\d{4}[-/]\d{1,2}[-/]\d{1,2}|\d{1,2}[-/]\d{1,2}[-/]\d{4})\b`,
	)
	validationVersionValuePattern = regexp.MustCompile(
		`(?i)(?:\b(?:v|version)\s*\d+(?:\.\d+)+(?:[-+][a-z0-9.-]+)?\b|\b\d+\.\d+\.\d+(?:[-+][a-z0-9.-]+)?\b)`,
	)
	validationNumericValuePattern = regexp.MustCompile(`\b\d+(?:[.,]\d+)*\b`)
)

type validationValueSignatures struct {
	numeric  map[string]struct{}
	currency map[string]struct{}
	date     map[string]struct{}
	version  map[string]struct{}
}

func validationValuesCompatible(criterion, description string) bool {
	required := validationValues(criterion)
	available := validationValues(description)
	return validationValueSetMatches(required.numeric, available.numeric) &&
		validationValueSetMatches(required.currency, available.currency) &&
		validationValueSetMatches(required.date, available.date) &&
		validationValueSetMatches(required.version, available.version)
}

func validationValues(value string) validationValueSignatures {
	signatures := validationValueSignatures{
		numeric:  map[string]struct{}{},
		currency: map[string]struct{}{},
		date:     map[string]struct{}{},
		version:  map[string]struct{}{},
	}
	for _, match := range validationNumericValuePattern.FindAllString(value, -1) {
		signatures.numeric[normalizeValidationNumber(match)] = struct{}{}
	}
	for _, match := range validationCurrencyValuePattern.FindAllString(value, -1) {
		signatures.currency[normalizeValidationCurrency(match)] = struct{}{}
	}
	for _, match := range validationDateValuePattern.FindAllString(value, -1) {
		signatures.date[normalizeValidationDate(match)] = struct{}{}
	}
	for _, match := range validationVersionValuePattern.FindAllString(value, -1) {
		signatures.version[normalizeValidationVersion(match)] = struct{}{}
	}
	return signatures
}

func validationValueSetMatches(required, available map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	if len(required) != len(available) {
		return false
	}
	for value := range required {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}

func normalizeValidationCurrency(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	currency := ""
	switch {
	case strings.HasPrefix(value, "eur"):
		currency, value = "eur", strings.TrimPrefix(value, "eur")
	case strings.HasSuffix(value, "eur"):
		currency, value = "eur", strings.TrimSuffix(value, "eur")
	case strings.HasPrefix(value, "usd"):
		currency, value = "usd", strings.TrimPrefix(value, "usd")
	case strings.HasSuffix(value, "usd"):
		currency, value = "usd", strings.TrimSuffix(value, "usd")
	case strings.HasPrefix(value, "gbp"):
		currency, value = "gbp", strings.TrimPrefix(value, "gbp")
	case strings.HasSuffix(value, "gbp"):
		currency, value = "gbp", strings.TrimSuffix(value, "gbp")
	case strings.HasPrefix(value, "$"):
		currency, value = "usd", strings.TrimPrefix(value, "$")
	case strings.HasPrefix(value, "\u20ac"):
		currency, value = "eur", strings.TrimPrefix(value, "\u20ac")
	case strings.HasPrefix(value, "\u00a3"):
		currency, value = "gbp", strings.TrimPrefix(value, "\u00a3")
	}
	return currency + ":" + normalizeValidationNumber(value)
}

func normalizeValidationDate(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '/'
	})
	for index := range parts {
		parts[index] = normalizeValidationInteger(parts[index])
	}
	return strings.Join(parts, "-")
}

func normalizeValidationVersion(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	value = strings.TrimPrefix(value, "version")
	value = strings.TrimPrefix(value, "v")
	return value
}

func normalizeValidationNumber(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parts := strings.Split(value, ".")
	for index := range parts {
		parts[index] = normalizeValidationInteger(parts[index])
	}
	if len(parts) == 2 {
		parts[1] = strings.TrimRight(parts[1], "0")
		if parts[1] == "" {
			return parts[0]
		}
	}
	return strings.Join(parts, ".")
}

func normalizeValidationInteger(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

const (
	validationPolarityPositive uint8 = 1 << iota
	validationPolarityNegative
)

func validationNegationCompatible(criterion, description string) bool {
	required := validationConceptPolarities(criterion)
	available := validationConceptPolarities(description)
	for concept, requiredPolarity := range required {
		availablePolarity, ok := available[concept]
		if !ok {
			continue
		}
		if requiredPolarity == validationPolarityPositive &&
			availablePolarity == validationPolarityNegative {
			return false
		}
		if requiredPolarity == validationPolarityNegative &&
			availablePolarity == validationPolarityPositive {
			return false
		}
	}
	return true
}

func validationConceptPolarities(value string) map[string]uint8 {
	polarities := map[string]uint8{}
	negatedConceptsRemaining := 0
	for _, token := range validationTextTokens(value) {
		if validationNegationWord(token) {
			negatedConceptsRemaining = 3
			continue
		}
		concept := canonicalValidationConcept(token)
		if concept == "" || validationConceptStopWord(concept) {
			continue
		}
		polarity := validationPolarityPositive
		if negatedConceptsRemaining > 0 {
			polarity = validationPolarityNegative
			negatedConceptsRemaining--
		}
		polarities[concept] |= polarity
	}
	return polarities
}

func validationTextTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

func validationNegationWord(value string) bool {
	switch value {
	case "cannot", "can't", "couldn't", "didn't", "doesn't", "don't", "hadn't",
		"hasn't", "haven't", "isn't", "mustn't", "neither", "never", "no", "nor",
		"not", "shouldn't", "wasn't", "weren't", "without", "won't", "wouldn't":
		return true
	default:
		return false
	}
}

func criterionIsProvenanceOrSourceRetention(criterion string) bool {
	hasSource := false
	hasReference := false
	hasRetention := false
	for _, token := range validationTextTokens(criterion) {
		switch token {
		case "citation", "citations", "grounded", "grounding", "provenance":
			return true
		case "source", "sources":
			hasSource = true
		case "reference", "references":
			hasReference = true
		case "attach", "attached", "link", "linked", "persist", "persisted",
			"retain", "retained", "save", "saved", "store", "stored":
			hasRetention = true
		}
	}
	return hasSource && (hasReference || hasRetention)
}

func validationConcepts(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	concepts := []string{}
	for _, part := range parts {
		concept := canonicalValidationConcept(part)
		if concept == "" || validationConceptStopWord(concept) {
			continue
		}
		concepts = append(concepts, concept)
	}
	return uniqueStrings(concepts)
}

func canonicalValidationConcept(value string) string {
	switch strings.TrimSpace(value) {
	case "source", "sources", "reference", "references", "citation", "citations",
		"provenance", "grounded", "grounding":
		return "source"
	case "retain", "retained", "store", "stored", "persist", "persisted", "save",
		"saved", "link", "linked", "attach", "attached":
		return "persist"
	case "deliverable", "deliverables", "output", "outputs", "result", "results",
		"artifact", "artifacts", "outcome", "outcomes":
		return "deliverable"
	case "verify", "verified", "verification", "validate", "validated", "validation":
		return "verify"
	case "review", "reviewed", "approval", "approved", "authorize", "authorized",
		"permission", "gate", "gated":
		return "approval"
	case "satisfy", "satisfied", "pass", "passed", "complete", "completed",
		"completion":
		return "complete"
	case "execute", "executed", "execution", "run", "ran", "runtime":
		return "execute"
	case "test", "tests", "tested", "testing", "check", "checks", "checked":
		return "test"
	case "explain", "explained", "explanation":
		return "explain"
	case "models":
		return "model"
	case "contexts":
		return "context"
	case "frameworks":
		return "framework"
	case "claims":
		return "claim"
	default:
		return strings.TrimSpace(value)
	}
}

func validationConceptStopWord(value string) bool {
	switch value {
	case "a", "an", "and", "any", "are", "as", "at", "be", "before", "being",
		"by", "criteria", "criterion", "every", "for", "from", "in", "into", "is",
		"it", "its", "of", "on", "only", "or", "required", "requirement",
		"relevant", "task", "that", "the", "then", "this", "to", "was", "when",
		"where", "which", "who", "with":
		return true
	default:
		return false
	}
}

func toolExecutionValidationEvidence(result *ToolExecutionResult) []string {
	if result == nil {
		return []string{}
	}
	evidence := []string{"runtime-status:" + firstNonEmpty(result.Status, "unknown")}
	if launchID := strings.TrimSpace(result.LaunchEventID); launchID != "" {
		evidence = append(evidence, "automation-launch://"+launchID)
	} else if automationID := strings.TrimSpace(result.AutomationID); automationID != "" {
		evidence = append(evidence, "automation://"+automationID)
	}
	return uniqueStrings(evidence)
}

func setTaskStepStatus(plan *CompletionPlan, id, status string) {
	if plan == nil {
		return
	}
	for i := range plan.Steps {
		if plan.Steps[i].ID == id {
			plan.Steps[i].Status = status
			return
		}
	}
}

func setExecutionStepStatus(plan *CompletionPlan) {
	if plan == nil || plan.ExecutionResult == nil {
		return
	}
	if strings.TrimSpace(plan.ExecutionResult.BlockedReason) != "" {
		setTaskStepStatus(plan, "execute", "blocked")
		return
	}
	if plan.ExecutionResult.ToolExecution != nil &&
		plan.ExecutionResult.ToolExecution.Status != "completed" {
		setTaskStepStatus(plan, "execute", "failed")
		return
	}
	setTaskStepStatus(plan, "execute", "completed")
}

func setValidationStepStatus(plan *CompletionPlan) {
	if plan == nil {
		return
	}
	if plan.ValidationResult.Passed {
		setTaskStepStatus(plan, "verify", "completed")
		return
	}
	if plan.ValidationResult.Status == "blocked" {
		setTaskStepStatus(plan, "verify", "blocked")
		return
	}
	setTaskStepStatus(plan, "verify", "failed")
}

func setMemoryStepStatus(plan *CompletionPlan) {
	if plan == nil {
		return
	}
	for i := range plan.Steps {
		if plan.Steps[i].ID != "memory" || plan.Steps[i].Status == "completed" {
			continue
		}
		switch {
		case len(plan.StoredMemoryIDs) > 0:
			plan.Steps[i].Status = "completed"
		case plan.ValidationResult.Passed && len(plan.LessonsLearned) > 0:
			plan.Steps[i].Status = "failed"
		default:
			plan.Steps[i].Status = "skipped"
		}
		return
	}
}

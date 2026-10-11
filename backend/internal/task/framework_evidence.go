package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/frameworkevidence"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/sourceevidence"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

type EvidencePhase string

const (
	EvidencePhasePreAuthorization EvidencePhase = "pre_authorization"
	EvidencePhaseExecution        EvidencePhase = "execution"
	EvidencePhasePostcondition    EvidencePhase = "postcondition"
)

const (
	evidenceStatusVerified      = "verified"
	evidenceStatusMissing       = "missing"
	evidenceStatusNotApplicable = "not_applicable"
)

type FrameworkEvidenceContract struct {
	ID            string        `json:"id"`
	FrameworkID   string        `json:"frameworkId"`
	Requirement   string        `json:"requirement"`
	Phase         EvidencePhase `json:"phase"`
	Validator     string        `json:"validator"`
	Required      bool          `json:"required"`
	MaxAgeSeconds int64         `json:"maxAgeSeconds,omitempty"`
}

type FrameworkEvidenceAssertion struct {
	RequirementID       string                 `json:"requirementId"`
	FrameworkID         string                 `json:"frameworkId"`
	Requirement         string                 `json:"requirement"`
	Phase               EvidencePhase          `json:"phase"`
	Validator           string                 `json:"validator"`
	Status              string                 `json:"status"`
	Evidence            []string               `json:"evidence"`
	ApplicabilityReason string                 `json:"applicabilityReason,omitempty"`
	Failure             string                 `json:"failure,omitempty"`
	SourceClaims        []sourceevidence.Claim `json:"sourceClaims,omitempty"`
}

type FrameworkEvidencePreflightResult struct {
	Passed      bool                         `json:"passed"`
	Status      string                       `json:"status"`
	Digest      string                       `json:"digest"`
	Checked     int                          `json:"checked"`
	Verified    int                          `json:"verified"`
	Missing     int                          `json:"missing"`
	Assertions  []FrameworkEvidenceAssertion `json:"assertions"`
	Failures    []string                     `json:"failures"`
	EvaluatedAt time.Time                    `json:"evaluatedAt"`
}

func frameworkEvidencePreflightRequired(contracts []FrameworkEvidenceContract) bool {
	for _, contract := range contracts {
		if contract.Required && contract.Phase == EvidencePhasePreAuthorization {
			return true
		}
	}
	return false
}

func frameworkEvidencePreflightSummary(preflight *FrameworkEvidencePreflightResult) []string {
	if preflight == nil {
		return []string{}
	}
	evidence := []string{
		"framework-preflight-status:" + strings.TrimSpace(preflight.Status),
		fmt.Sprintf("framework-preflight-checked:%d", preflight.Checked),
		fmt.Sprintf("framework-preflight-verified:%d", preflight.Verified),
		fmt.Sprintf("framework-preflight-missing:%d", preflight.Missing),
	}
	for _, assertion := range preflight.Assertions {
		if assertion.Status == evidenceStatusVerified {
			evidence = append(evidence, "framework-requirement:"+assertion.RequirementID+":verified")
		}
	}
	return uniqueStrings(evidence)
}

func frameworkEvidenceContractsForRequirement(
	contracts []FrameworkEvidenceContract,
	requirement string,
) []FrameworkEvidenceContract {
	requirement = strings.TrimSpace(requirement)
	matched := []FrameworkEvidenceContract{}
	for _, contract := range contracts {
		if strings.EqualFold(strings.TrimSpace(contract.Requirement), requirement) {
			matched = append(matched, contract)
		}
	}
	return matched
}

func evaluateTypedFrameworkEvidence(
	plan *CompletionPlan,
	contracts []FrameworkEvidenceContract,
) (string, []string, string) {
	if plan == nil || len(contracts) == 0 {
		return validationCriterionFailed, []string{}, "typed framework evidence contract is missing"
	}
	evidence := []string{}
	missing := []string{}
	for _, contract := range contracts {
		contractEvidence := []string{}
		switch contract.Phase {
		case EvidencePhasePreAuthorization:
			if plan.FrameworkEvidencePreflight == nil || !plan.FrameworkEvidencePreflight.Passed {
				// The dedicated preflight criterion carries the actionable cause.
				// Do not turn the same blocked precondition into one failure for
				// every typed requirement.
				return validationCriterionNotRun,
					frameworkEvidencePreflightSummary(plan.FrameworkEvidencePreflight),
					"framework evidence preconditions were not verified before execution"
			}
			contractEvidence = verifiedFrameworkPreflightEvidence(plan.FrameworkEvidencePreflight, contract)
		case EvidencePhaseExecution:
			contractEvidence = exactFrameworkExecutionEvidence(plan, contract)
		case EvidencePhasePostcondition:
			contractEvidence = exactFrameworkPostconditionEvidence(plan, contract)
		default:
			missing = append(missing, contract.ID+":unsupported-phase")
			continue
		}
		if len(contractEvidence) == 0 {
			if contract.Required {
				missing = append(missing, contract.ID+":"+string(contract.Phase))
			}
			continue
		}
		evidence = append(evidence, "framework-contract:"+contract.ID, "framework:"+contract.FrameworkID)
		evidence = append(evidence, contractEvidence...)
	}
	if len(missing) > 0 {
		return validationCriterionFailed, uniqueStrings(evidence),
			"required typed framework evidence is missing (" + strings.Join(missing, ", ") + ")"
	}
	return validationCriterionPassed, uniqueStrings(evidence), ""
}

func verifiedFrameworkPreflightEvidence(
	preflight *FrameworkEvidencePreflightResult,
	contract FrameworkEvidenceContract,
) []string {
	if preflight == nil || !preflight.Passed {
		return []string{}
	}
	for _, assertion := range preflight.Assertions {
		if assertion.RequirementID != contract.ID || assertion.FrameworkID != contract.FrameworkID ||
			assertion.Phase != EvidencePhasePreAuthorization || assertion.Status != evidenceStatusVerified {
			continue
		}
		return append([]string{"preflight-assertion:" + assertion.RequirementID}, assertion.Evidence...)
	}
	return []string{}
}

func exactFrameworkExecutionEvidence(
	plan *CompletionPlan,
	contract FrameworkEvidenceContract,
) []string {
	if plan.ExecutionResult == nil {
		return []string{}
	}
	result := plan.ExecutionResult
	tool := result.ToolExecution
	completedLaunch := tool != nil && tool.Status == "completed" && validLaunchEventID(tool.LaunchEventID)
	normalized := strings.ToLower(strings.TrimSpace(contract.Requirement))
	switch normalized {
	case "action receipt", "acceptance or acknowledgement":
		if completedLaunch {
			return []string{"action-receipt:" + tool.LaunchEventID, "automation:" + tool.AutomationID}
		}
	case "correlation and provenance identifiers":
		if completedLaunch && strings.TrimSpace(tool.AutomationID) != "" {
			return []string{"correlation-id:" + tool.LaunchEventID, "provenance-automation:" + tool.AutomationID}
		}
	case "runtime boundary":
		if completedLaunch && strings.TrimSpace(tool.LaunchType) != "" {
			return uniqueStrings([]string{
				"launch-event:" + tool.LaunchEventID,
				"runtime-type:" + firstNonEmpty(tool.RuntimeType, "unspecified"),
				"launch-type:" + tool.LaunchType,
				"target:" + strings.TrimSpace(tool.Target),
			})
		}
	case "artifact provenance":
		if completedLaunch && strings.TrimSpace(tool.Target) != "" {
			return []string{"artifact-launch:" + tool.LaunchEventID, "artifact-target:" + tool.Target}
		}
	case "raw item identity", "extraction provenance":
		evidence := []string{}
		for _, ranked := range plan.ContextPlan.SourceContext {
			extraction := ranked.Extraction
			if extraction.ID == uuid.Nil || strings.TrimSpace(extraction.SourceURI) == "" {
				continue
			}
			evidence = append(evidence,
				"source-extraction:"+extraction.ID.String(),
				"source-uri:"+extraction.SourceURI,
			)
		}
		return uniqueStrings(evidence)
	case "proposal provenance", "decision record":
		if strings.TrimSpace(plan.ID) != "" && plan.FrameworkDecision != nil &&
			strings.TrimSpace(plan.FrameworkDecision.ID) != "" {
			return []string{"task-plan:" + plan.ID, "framework-selection:" + plan.FrameworkDecision.ID}
		}
	case "source timestamps":
		if completedLaunch && !tool.ExecutedAt.IsZero() {
			return []string{"source-timestamp:" + tool.ExecutedAt.UTC().Format(time.RFC3339Nano)}
		}
	case "non-destructive change log":
		if completedLaunch && !executionContainsDestructiveAction(result) {
			return []string{"change-log:" + tool.LaunchEventID, "destructive-action:false"}
		}
	case "recovery receipt":
		for _, action := range result.Actions {
			if action.Status == "completed" && strings.Contains(strings.ToLower(action.Name), "recover") &&
				strings.TrimSpace(action.Output) != "" {
				return []string{"recovery-action:" + action.Name, "recovery-output:recorded"}
			}
		}
	case "redaction state":
		for _, auditEvent := range toolAuditEvents(tool) {
			if strings.Contains(strings.ToLower(auditEvent), "redact") {
				return []string{"redaction-audit:" + auditEvent}
			}
		}
	}
	return []string{}
}

func exactFrameworkPostconditionEvidence(
	plan *CompletionPlan,
	contract FrameworkEvidenceContract,
) []string {
	if plan.ExecutionResult == nil || executionOutcomeUncertain(plan.ExecutionResult) {
		return []string{}
	}
	result := plan.ExecutionResult
	normalized := strings.ToLower(strings.TrimSpace(contract.Requirement))
	accepted := verificationStatusAcceptsCompletion(result.VerificationStatus) &&
		result.UnsupportedClaims == 0
	sourceLinkedClaims := acceptedSourceLinkedClaims(result.Claims)
	switch normalized {
	case "postcondition verification", "validation result":
		if accepted && len(result.Claims) > 0 {
			return []string{"verification-status:" + result.VerificationStatus, fmt.Sprintf("verified-claims:%d", len(result.Claims))}
		}
	case "reproducible result":
		if accepted && ((result.ToolExecution != nil && result.ToolExecution.Status == "completed" && validLaunchEventID(result.ToolExecution.LaunchEventID)) || len(sourceLinkedClaims) > 0) {
			return append([]string{"verification-status:" + result.VerificationStatus}, sourceLinkedClaims...)
		}
	case "deliverable evidence":
		if accepted && strings.TrimSpace(result.Output) != "" && len(sourceLinkedClaims) > 0 {
			return append([]string{"deliverable:present", "verification-status:" + result.VerificationStatus}, sourceLinkedClaims...)
		}
	case "individual outputs":
		evidence := []string{}
		for _, action := range result.Actions {
			if action.Status == "completed" && strings.TrimSpace(action.Output) != "" {
				evidence = append(evidence, "action-output:"+action.Name)
			}
		}
		return uniqueStrings(evidence)
	case "synthesis provenance":
		if accepted && strings.TrimSpace(result.Output) != "" && len(sourceLinkedClaims) > 0 {
			return append([]string{"synthesis-output:present"}, sourceLinkedClaims...)
		}
	case "action feedback":
		if result.ToolExecution != nil && result.ToolExecution.Status == "completed" &&
			strings.TrimSpace(firstNonEmpty(result.ToolExecution.Output, result.ToolExecution.Message)) != "" {
			return []string{"action-feedback:" + result.ToolExecution.LaunchEventID}
		}
	case "post-recovery verification":
		if accepted {
			for _, action := range result.Actions {
				if action.Status == "completed" && strings.Contains(strings.ToLower(action.Name), "recover") {
					return []string{"post-recovery-verification:" + result.VerificationStatus}
				}
			}
		}
	case "verified outcome":
		if accepted && len(result.Claims) > 0 {
			return []string{"verified-outcome:" + result.VerificationStatus}
		}
	case "observed or self-reported outcome", "practice results":
		if len(sourceLinkedClaims) > 0 {
			return sourceLinkedClaims
		}
	case "deterministic checks where possible":
		if evidence := deterministicFrameworkCheckEvidence(result); len(evidence) > 0 {
			return evidence
		}
	case "before-and-after behavior", "before-and-after evidence":
		return exactBeforeAfterEvidence(result.Claims)
	}
	return []string{}
}

func deterministicFrameworkCheckEvidence(result *ExecutionResult) []string {
	if result == nil {
		return []string{}
	}
	if result.VerificationStatus == verification.StatusTestPassed {
		return []string{"deterministic-check:test-passed", "verification-status:" + result.VerificationStatus}
	}
	tool := result.ToolExecution
	if tool == nil || tool.Status != "completed" || tool.ExitCode != 0 ||
		!validLaunchEventID(tool.LaunchEventID) {
		return []string{}
	}
	description := strings.ToLower(strings.Join([]string{tool.Target, tool.Message, tool.Output}, " "))
	for _, signal := range []string{"test", "check", "verify", "validation", "lint", "build"} {
		if strings.Contains(description, signal) {
			return []string{
				"deterministic-check-launch:" + tool.LaunchEventID,
				"deterministic-check-exit-code:0",
				"deterministic-check-signal:" + signal,
			}
		}
	}
	return []string{}
}

func validLaunchEventID(value string) bool {
	id, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil && id != uuid.Nil
}

func executionContainsDestructiveAction(result *ExecutionResult) bool {
	for _, action := range result.Actions {
		value := strings.ToLower(action.Name + " " + action.Input)
		for _, token := range []string{"delete", "remove", "truncate", "drop", "destroy"} {
			if strings.Contains(value, token) {
				return true
			}
		}
	}
	return false
}

func toolAuditEvents(tool *ToolExecutionResult) []string {
	if tool == nil {
		return []string{}
	}
	return tool.AuditEvents
}

func acceptedSourceLinkedClaims(claims []models.VerificationClaim) []string {
	evidence := []string{}
	for _, claim := range claims {
		if claim.NeedsReview || !verificationStatusAcceptsCompletion(claim.Status) ||
			strings.TrimSpace(claim.SourceRefs) == "" {
			continue
		}
		claimID := "claim:" + claim.ID.String()
		if claim.ID == uuid.Nil {
			claimID = "claim-source-linked"
		}
		evidence = append(evidence, claimID, "claim-source:"+strings.TrimSpace(claim.SourceRefs))
	}
	return uniqueStrings(evidence)
}

func exactBeforeAfterEvidence(claims []models.VerificationClaim) []string {
	evidence := []string{}
	for _, claim := range claims {
		if claim.NeedsReview || !verificationStatusAcceptsCompletion(claim.Status) ||
			strings.TrimSpace(claim.SourceRefs) == "" {
			continue
		}
		text := strings.ToLower(claim.ClaimText + " " + claim.SupportExplanation)
		if strings.Contains(text, "before") && strings.Contains(text, "after") {
			evidence = append(evidence, "before-after-claim:"+claim.ID.String(), "claim-source:"+strings.TrimSpace(claim.SourceRefs))
		}
	}
	return uniqueStrings(evidence)
}

func compileFrameworkEvidenceContracts(
	decision *frameworkregistry.SelectionDecision,
) []FrameworkEvidenceContract {
	if decision == nil {
		return []FrameworkEvidenceContract{}
	}
	contracts := []FrameworkEvidenceContract{}
	seen := map[string]struct{}{}
	appendRequirement := func(frameworkID, requirement string) {
		frameworkID = strings.TrimSpace(frameworkID)
		requirement = strings.TrimSpace(requirement)
		if frameworkID == "" || requirement == "" {
			return
		}
		key := strings.ToLower(frameworkID + "\x00" + requirement)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		phase := frameworkEvidencePhase(requirement)
		validator := frameworkEvidenceValidator(requirement, phase)
		contracts = append(contracts, FrameworkEvidenceContract{
			ID:          frameworkEvidenceRequirementID(frameworkID, requirement),
			FrameworkID: frameworkID,
			Requirement: requirement,
			Phase:       phase,
			Validator:   validator,
			// Catalog prose without a concrete validator remains visible in the
			// plan, but cannot become an unsatisfiable execution precondition.
			Required:      validator != "explicit_evidence",
			MaxAgeSeconds: frameworkEvidenceMaxAge(requirement),
		})
	}
	for _, selected := range decision.Selected {
		for _, requirement := range selected.EvidenceRequirements {
			appendRequirement(selected.ID, requirement)
		}
	}
	for _, requirement := range decision.EvidenceRequirements {
		found := false
		for _, contract := range contracts {
			if strings.EqualFold(contract.Requirement, strings.TrimSpace(requirement)) {
				found = true
				break
			}
		}
		if !found {
			appendRequirement("selection-policy", requirement)
		}
	}
	return contracts
}

func frameworkEvidenceRequirementID(frameworkID, requirement string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(frameworkID)) + "\x00" + strings.ToLower(strings.TrimSpace(requirement))))
	return fmt.Sprintf("fer-%x", digest[:12])
}

func frameworkEvidencePhase(requirement string) EvidencePhase {
	switch strings.ToLower(strings.TrimSpace(requirement)) {
	case "acceptance or acknowledgement", "correlation and provenance identifiers",
		"non-destructive change log", "raw item identity", "extraction provenance",
		"proposal provenance", "artifact provenance", "runtime boundary",
		"decision record", "correlation ids", "source timestamps", "redaction state",
		"recovery receipt", "action receipt":
		return EvidencePhaseExecution
	case "individual outputs", "synthesis provenance", "action feedback",
		"postcondition verification", "validation result", "reproducible result",
		"post-recovery verification", "verified outcome", "before-and-after behavior",
		"observed or self-reported outcome", "before-and-after evidence",
		"deliverable evidence", "practice results", "deterministic checks where possible":
		return EvidencePhasePostcondition
	default:
		return EvidencePhasePreAuthorization
	}
}

func frameworkEvidenceValidator(requirement string, phase EvidencePhase) string {
	if phase == EvidencePhaseExecution {
		return "execution_receipt"
	}
	if phase == EvidencePhasePostcondition {
		return "verified_postcondition"
	}
	normalized := strings.ToLower(strings.TrimSpace(requirement))
	switch normalized {
	case "active constitution", "active constitution version", "active constitution and applicable authority decision":
		return "constitution_record"
	case "verified identity", "verified operator identity", "sender identity", "responsible owner":
		return "owner_identity"
	case "applicable approval record", "standing mandate or case approval",
		"approver identity", "scope and expiry", "approval for consequential send":
		return "approval_record"
	case "fresh calendar or workload signal", "calendar availability", "current schedule":
		return "calendar_source"
	case "current agent card", "agent roster and capability claims", "explicit reporting structure":
		return "agent_contract"
	case "tool allowlist":
		return "tool_allowlist"
	case "runtime health and capability provenance", "provider health", "health probes":
		return "live_health"
	case "delegation contract":
		return "delegation_contract"
	case "schema-valid envelope":
		return "communication_contract"
	case "coordination plan":
		return "coordination_contract"
	case "deterministic calculation":
		return "deterministic_precheck"
	case "primary case sources", "official care instruction or operator record",
		"source transaction or invoice":
		return "primary_source"
	case "source authority and freshness", "fresh appointment information",
		"current route or timetable source", "verified contact route":
		return "fresh_source"
	case "source-linked entity references", "source-backed deadlines", "retrieved evidence",
		"source reference", "original source link", "source uri and freshness",
		"claim-to-source links", "support for factual claims", "asset or property record",
		"quote or maintenance source", "customer or market evidence", "dated timeline",
		"claim-to-evidence map", "deadline provenance", "current recovery plan",
		"backup status", "upstream license and maintenance review", "license and maintenance status",
		"threat model", "privacy review", "coverage gap", "benchmark against postgresql baseline":
		return "source_context"
	case "purpose and permission", "permission grant":
		return "permission_contract"
	case "data classification", "processing location":
		return "privacy_contract"
	case "risk classification", "exact proposed action", "risk and consequences",
		"current mode", "policy decision", "system purpose":
		return "policy_contract"
	case "idempotency key":
		return "idempotency_contract"
	case "goal state", "known constraints", "dependency and resource assumptions",
		"state transition contract", "failure and compensation path", "approval nodes",
		"explicit desired outcome", "traceable parent pursuit or reviewable candidate",
		"original input retained as untrusted source", "classification confidence and reason",
		"scored criteria", "explicit tie-break rule", "stated assumptions", "alternative explanations",
		"assumption register", "confidence basis", "sensitivity analysis where material",
		"goal and intention ledger", "fresh world-state snapshot", "decision context",
		"status and ownership", "accessible recovery path", "evaluation dataset or criteria",
		"known limitations", "current commitments", "declared priority", "operator-chosen behavior",
		"operator-stated needs", "operator-provided capacity where available", "uncertainty label",
		"operator-stated relationship context", "explicit uncertainty for inferred intent",
		"learning objective", "assessment criteria", "experiment design", "decision threshold",
		"agreed scope", "acceptance criteria", "cost and cancellation terms", "currency and period",
		"measured current limitation", "migration and rollback plan", "operational cost",
		"export and deletion behavior", "migration and recovery plan", "sandbox and policy contract",
		"capability test", "hazard register", "control mapping", "recovery and rollback plan",
		"trust-boundary map", "threat scenario", "mitigation and residual risk":
		return "planning_contract"
	case "confidence", "recent outcome signals", "rank factors":
		return "confidence_record"
	case "memory type", "retention state":
		return "memory_contract"
	case "retrieval query":
		return "retrieval_contract"
	case "authorized source event", "sync cursor":
		return "source_sync_contract"
	case "capability profile", "price and quota data":
		return "model_route_contract"
	default:
		return "explicit_evidence"
	}
}

func frameworkEvidenceMaxAge(requirement string) int64 {
	switch strings.ToLower(strings.TrimSpace(requirement)) {
	case "current route or timetable source", "provider health", "runtime health and capability provenance", "health probes":
		return int64((24 * time.Hour).Seconds())
	case "fresh appointment information", "source authority and freshness", "fresh calendar or workload signal", "current schedule":
		return int64((7 * 24 * time.Hour).Seconds())
	default:
		return 0
	}
}

func (s *service) evaluateFrameworkEvidencePreflight(
	plan *CompletionPlan,
	request IntakeRequest,
) FrameworkEvidencePreflightResult {
	// PostgreSQL timestamps have microsecond precision. Bind the digest to the
	// same precision before persistence so a database round trip is stable.
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := FrameworkEvidencePreflightResult{
		Passed: true, Status: "passed", Assertions: []FrameworkEvidenceAssertion{},
		Failures: []string{}, EvaluatedAt: now,
	}
	if plan == nil {
		result.Passed = false
		result.Status = "failed"
		result.Failures = []string{"completion plan is missing"}
		result.Digest = frameworkEvidencePreflightDigest(plan, result)
		return result
	}
	for _, contract := range plan.ValidationPlan.FrameworkEvidenceContracts {
		if contract.Phase != EvidencePhasePreAuthorization || !contract.Required {
			continue
		}
		assertion := FrameworkEvidenceAssertion{
			RequirementID: contract.ID, FrameworkID: contract.FrameworkID,
			Requirement: contract.Requirement, Phase: contract.Phase,
			Validator: contract.Validator, Status: evidenceStatusMissing, Evidence: []string{},
		}
		applicable, reason := frameworkEvidenceRequirementApplies(plan, contract.Requirement)
		if !applicable {
			assertion.Status = evidenceStatusNotApplicable
			assertion.ApplicabilityReason = reason
			result.Assertions = append(result.Assertions, assertion)
			continue
		}
		result.Checked++
		if isSourceEvidenceValidator(contract.Validator) {
			assertion.Evidence, assertion.SourceClaims = s.resolveSourceEvidence(plan, contract, now)
		} else {
			assertion.Evidence = s.preAuthorizationEvidence(plan, request, contract, now)
		}
		if len(assertion.Evidence) > 0 {
			if isSourceEvidenceValidator(contract.Validator) && len(assertion.SourceClaims) == 0 {
				assertion.Evidence = []string{}
			}
		}
		if len(assertion.Evidence) > 0 {
			assertion.Status = evidenceStatusVerified
			result.Verified++
		} else {
			assertion.Failure = "required pre-authorization evidence is missing"
			if contract.Validator == "owner_identity" {
				assertion.Failure = "verified owner identity is required"
			}
			result.Missing++
			result.Failures = append(result.Failures,
				contract.FrameworkID+": "+contract.Requirement+" ("+assertion.Failure+")",
			)
		}
		result.Assertions = append(result.Assertions, assertion)
	}
	result.Failures = uniqueStrings(result.Failures)
	result.Passed = result.Missing == 0
	if !result.Passed {
		result.Status = "blocked"
	}
	result.Digest = frameworkEvidencePreflightDigest(plan, result)
	return result
}

func frameworkEvidencePreflightDigest(plan *CompletionPlan, result FrameworkEvidencePreflightResult) string {
	if plan == nil || plan.FrameworkDecision == nil {
		return ""
	}
	assertions, err := json.Marshal(result.Assertions)
	if err != nil {
		return ""
	}
	digest, err := frameworkevidence.PreflightDigest(
		plan.OwnerIdentity,
		plan.ID,
		plan.FrameworkDecision.ID,
		result.EvaluatedAt,
		assertions,
	)
	if err != nil {
		return ""
	}
	return digest
}

func (s *service) preAuthorizationEvidence(
	plan *CompletionPlan,
	request IntakeRequest,
	contract FrameworkEvidenceContract,
	now time.Time,
) []string {
	switch contract.Validator {
	case "constitution_record":
		if plan.FrameworkDecision != nil && plan.FrameworkDecision.ConstitutionVersion > 0 &&
			strings.TrimSpace(plan.FrameworkDecision.ConstitutionDigest) != "" {
			return []string{fmt.Sprintf("constitution-version:%d", plan.FrameworkDecision.ConstitutionVersion), "constitution-digest:" + plan.FrameworkDecision.ConstitutionDigest}
		}
	case "owner_identity":
		if strings.TrimSpace(plan.OwnerIdentity) != "" {
			return []string{"owner-identity:verified"}
		}
	case "approval_record":
		if !plan.RiskAssessment.ApprovalRequired {
			return []string{"approval:not-required-by-policy"}
		}
		if plan.RiskAssessment.ApprovalGranted {
			decision, err := s.verifiedApprovalDecisionForExecution(plan, request)
			if err == nil && decision != nil {
				evidence := []string{"approval-source:" + decision.ApprovalSourceID, "approval-recorded-at:" + decision.ApprovedAt.UTC().Format(time.RFC3339Nano)}
				if strings.TrimSpace(decision.ApprovalBindingDigest) != "" {
					evidence = append(evidence, "approval-binding:"+decision.ApprovalBindingDigest)
				}
				return evidence
			}
		}
	case "calendar_source":
		if plan.CalendarCapacity.Status == "source_backed" {
			return []string{"calendar-capacity:source-backed", "calendar-window:" + plan.CalendarCapacity.WindowStart.UTC().Format(time.RFC3339) + "/" + plan.CalendarCapacity.WindowEnd.UTC().Format(time.RFC3339)}
		}
	case "agent_contract":
		for _, card := range plan.ExecutionPlan.AgentCards {
			if card.Verified && !card.Revoked && strings.TrimSpace(card.ID) != "" {
				return []string{"agent-card:" + card.ID, "agent-card-version:" + card.Version}
			}
		}
	case "tool_allowlist":
		if len(plan.ToolDecision.SelectedTools) > 0 {
			return prefixedEvidence("allowed-tool:", plan.ToolDecision.SelectedTools)
		}
	case "live_health":
		for _, card := range plan.ExecutionPlan.AgentCards {
			// Current and legacy registry cards contain declarations, not trusted
			// server probe evidence, regardless of their verification flags.
			provenance := strings.ToLower(strings.TrimSpace(card.Provenance))
			if strings.HasPrefix(provenance, "agent_registry:") || strings.HasPrefix(provenance, "agent_registry_declaration:") {
				continue
			}
			healthy := strings.EqualFold(card.HealthStatus, "healthy")
			if card.Verified && !card.Revoked && healthy && card.LastVerifiedAt != nil &&
				!card.LastVerifiedAt.IsZero() &&
				!card.LastVerifiedAt.After(now.Add(time.Minute)) &&
				(contract.MaxAgeSeconds == 0 || now.Sub(card.LastVerifiedAt.UTC()) <= time.Duration(contract.MaxAgeSeconds)*time.Second) {
				return []string{"agent-health:healthy", "agent-health-checked:" + card.LastVerifiedAt.UTC().Format(time.RFC3339Nano)}
			}
		}
	case "delegation_contract":
		if len(plan.ExecutionPlan.Delegations) > 0 {
			return []string{"delegation-contract:" + plan.ExecutionPlan.Delegations[0].ID}
		}
	case "communication_contract":
		if strings.TrimSpace(plan.ExecutionPlan.Communication.SchemaVersion) != "" && len(plan.ExecutionPlan.Communication.RequiredFields) > 0 {
			return []string{"communication-schema:" + plan.ExecutionPlan.Communication.SchemaVersion}
		}
	case "coordination_contract":
		if strings.TrimSpace(plan.ExecutionPlan.Coordination.Mode) != "" && strings.TrimSpace(plan.ExecutionPlan.Coordination.Coordinator) != "" {
			return []string{"coordination-mode:" + plan.ExecutionPlan.Coordination.Mode, "coordinator:" + plan.ExecutionPlan.Coordination.Coordinator}
		}
	case "primary_source", "fresh_source", "source_context":
		return []string{}
	case "permission_contract":
		if strings.TrimSpace(plan.RealGoal) != "" && (!plan.RiskAssessment.ApprovalRequired || plan.RiskAssessment.ApprovalGranted) {
			return []string{"purpose:declared", fmt.Sprintf("approval-required:%t", plan.RiskAssessment.ApprovalRequired), fmt.Sprintf("approval-granted:%t", plan.RiskAssessment.ApprovalGranted)}
		}
	case "privacy_contract":
		if classification := taskDataClassification(plan); classification != "unknown" {
			return append([]string{"data-classification:" + classification}, taskProcessingLocationEvidence(plan)...)
		}
	case "policy_contract":
		if strings.TrimSpace(plan.RealGoal) != "" && strings.TrimSpace(plan.RiskAssessment.Level) != "" && plan.FrameworkDecision != nil {
			return []string{"task-plan:" + plan.ID, "risk-level:" + plan.RiskAssessment.Level, "framework-selection:" + plan.FrameworkDecision.ID}
		}
	case "idempotency_contract":
		if strings.TrimSpace(plan.ID) != "" {
			return []string{"idempotency-key:task-plan:" + plan.ID}
		}
	case "planning_contract":
		return planningRequirementEvidence(plan, contract.Requirement)
	case "confidence_record":
		return preflightConfidenceEvidence(plan)
	case "memory_contract":
		return append(taskMemoryEvidence(plan), taskRetentionEvidence(plan)...)
	case "retrieval_contract":
		if strings.TrimSpace(plan.Request) != "" && strings.TrimSpace(plan.ContextPlan.Explanation) != "" {
			return []string{"retrieval-query:task-request", "retrieval-plan:explained"}
		}
	case "model_route_contract":
		if strings.TrimSpace(plan.ModelDecision.SelectedModelID) != "" && strings.TrimSpace(plan.ModelDecision.Reason) != "" {
			return []string{"model:" + plan.ModelDecision.SelectedModelID, "model-route:explained"}
		}
	case "deterministic_precheck", "source_sync_contract", "explicit_evidence":
		return []string{}
	}
	return []string{}
}

func frameworkEvidenceBlockedExecution(
	plan *CompletionPlan,
	preflight FrameworkEvidencePreflightResult,
) *ExecutionResult {
	now := time.Now().UTC()
	reason := "selected framework evidence preconditions are missing"
	if len(preflight.Failures) > 0 {
		reason += ": " + strings.Join(preflight.Failures, "; ")
	}
	plan.Events = append(plan.Events, event("framework-evidence-preflight", reason))
	return &ExecutionResult{
		StartedAt: now, CompletedAt: now, Mode: "blocked", Output: "Execution was blocked before any external effect.",
		VerificationStatus: verification.StatusNeedsReview, Claims: []models.VerificationClaim{},
		EvidenceCount: preflight.Verified, UnsupportedClaims: 0,
		Actions:       []ExecutedAction{executedAction("governance.framework_evidence_preflight", "blocked", plan.Request, reason, now)},
		BlockedReason: reason,
	}
}

func isSourceEvidenceValidator(validator string) bool {
	switch strings.TrimSpace(validator) {
	case sourceevidence.ValidatorPrimarySource, sourceevidence.ValidatorFreshSource, sourceevidence.ValidatorSourceContext:
		return true
	default:
		return false
	}
}

func (s *service) resolveSourceEvidence(
	plan *CompletionPlan,
	contract FrameworkEvidenceContract,
	now time.Time,
) ([]string, []sourceevidence.Claim) {
	if s == nil || s.sourceEvidence == nil || plan == nil || strings.TrimSpace(plan.OwnerIdentity) == "" {
		return []string{}, []sourceevidence.Claim{}
	}
	maxAgeSeconds := contract.MaxAgeSeconds
	if contract.Validator == sourceevidence.ValidatorFreshSource && maxAgeSeconds <= 0 {
		maxAgeSeconds = int64((7 * 24 * time.Hour).Seconds())
	}
	evidence := []string{}
	claims := []sourceevidence.Claim{}
	for _, ranked := range plan.ContextPlan.SourceContext {
		extraction := ranked.Extraction
		if extraction.ID == uuid.Nil || strings.TrimSpace(firstNonEmpty(extraction.Summary, extraction.Text)) == "" || strings.TrimSpace(extraction.SourceURI) == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), frameworkEvidenceRepositoryTimeout)
		snapshot, err := s.sourceEvidence.Resolve(ctx, plan.OwnerIdentity, extraction.ID.String())
		cancel()
		if err != nil || snapshot.ExtractionID != extraction.ID.String() || snapshot.SourceID != extraction.SourceID.String() ||
			snapshot.RawItemID != extraction.RawItemID.String() ||
			!strings.EqualFold(snapshot.ExtractionPayloadDigest, sourceevidence.ExtractionPayloadDigest(extraction)) ||
			strings.TrimSpace(snapshot.ExtractionURI) != strings.TrimSpace(extraction.SourceURI) ||
			strings.TrimSpace(snapshot.ExtractionHash) != strings.TrimSpace(extraction.ContentHash) ||
			strings.TrimSpace(snapshot.ProjectKey) != strings.TrimSpace(extraction.ProjectKey) {
			continue
		}
		if snapshot.FetchedAt.IsZero() || snapshot.FetchedAt.After(now.Add(5*time.Minute)) {
			continue
		}
		if contract.Validator == sourceevidence.ValidatorFreshSource &&
			now.Sub(snapshot.FetchedAt.UTC()) > time.Duration(maxAgeSeconds)*time.Second {
			continue
		}
		claim := sourceevidence.Claim{
			RequirementID: contract.ID, Validator: contract.Validator,
			ExtractionID: snapshot.ExtractionID, SourceID: snapshot.SourceID, RawItemID: snapshot.RawItemID,
			SnapshotDigest: snapshot.SnapshotDigest, MaxAgeSeconds: maxAgeSeconds,
		}
		if err := sourceevidence.VerifyClaim(snapshot, claim, plan.OwnerIdentity, now); err != nil {
			continue
		}
		claims = append(claims, claim)
		evidence = append(evidence,
			"connected-source:"+snapshot.ExtractionID,
			"source-snapshot:"+snapshot.SnapshotDigest,
			"source-observed-at:"+snapshot.FetchedAt.UTC().Format(time.RFC3339Nano),
		)
	}
	return uniqueStrings(evidence), sortedSourceEvidenceClaims(claims)
}

func sortedSourceEvidenceClaims(claims []sourceevidence.Claim) []sourceevidence.Claim {
	result := append([]sourceevidence.Claim(nil), claims...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].RequirementID != result[j].RequirementID {
			return result[i].RequirementID < result[j].RequirementID
		}
		return result[i].ExtractionID < result[j].ExtractionID
	})
	return result
}

func planningRequirementEvidence(plan *CompletionPlan, requirement string) []string {
	switch strings.ToLower(strings.TrimSpace(requirement)) {
	case "explicit desired outcome", "goal state", "system purpose", "learning objective":
		if strings.TrimSpace(plan.RealGoal) != "" {
			return []string{"real-goal:present"}
		}
	case "traceable parent pursuit or reviewable candidate":
		if strings.TrimSpace(plan.PursuitID) != "" {
			return []string{"pursuit:" + plan.PursuitID}
		}
		if strings.TrimSpace(plan.ID) != "" {
			return []string{"reviewable-task-plan:" + plan.ID}
		}
	case "original input retained as untrusted source":
		if strings.TrimSpace(plan.Request) != "" {
			return []string{"input-provenance:task-plan:" + plan.ID, "input-trust:untrusted"}
		}
	case "classification confidence and reason":
		if strings.TrimSpace(plan.Intake.Reason) != "" {
			return []string{"classification-reason:present", "task-type:" + plan.Intake.TaskType}
		}
	case "known constraints", "dependency and resource assumptions", "current commitments", "declared priority":
		if plan.ResourceDecision != nil && strings.TrimSpace(plan.ResourceDecision.DecisionDigest) != "" {
			return []string{"resource-decision:" + plan.ResourceDecision.DecisionDigest}
		}
	case "state transition contract", "failure and compensation path", "approval nodes", "migration and rollback plan", "migration and recovery plan", "recovery and rollback plan", "accessible recovery path":
		if len(plan.RetryPolicy.EscalationPath) > 0 {
			return prefixedEvidence("recovery-path:", plan.RetryPolicy.EscalationPath)
		}
	case "risk classification", "exact proposed action", "risk and consequences", "current mode", "policy decision", "decision context", "status and ownership":
		if strings.TrimSpace(plan.RiskAssessment.Level) != "" && strings.TrimSpace(plan.ID) != "" {
			return []string{"task-plan:" + plan.ID, "risk-level:" + plan.RiskAssessment.Level}
		}
	case "evaluation dataset or criteria", "assessment criteria", "acceptance criteria":
		if len(plan.ValidationPlan.SuccessCriteria) > 0 {
			return []string{fmt.Sprintf("success-criteria-count:%d", len(plan.ValidationPlan.SuccessCriteria))}
		}
	case "operator-stated needs":
		if plan.FrameworkDecision != nil && len(plan.FrameworkDecision.NeedsState) > 0 {
			return []string{fmt.Sprintf("needs-state-count:%d", len(plan.FrameworkDecision.NeedsState))}
		}
	case "operator-provided capacity where available":
		if plan.FrameworkDecision != nil && strings.TrimSpace(plan.FrameworkDecision.Capacity.Status) != "" {
			return []string{"capacity-status:" + plan.FrameworkDecision.Capacity.Status}
		}
	case "cost and cancellation terms", "currency and period", "deterministic calculation":
		return []string{}
	default:
		if strings.TrimSpace(plan.Request) != "" && strings.TrimSpace(plan.RealGoal) != "" {
			return []string{"task-plan:" + plan.ID, "requirement-explicitly-planned:" + strings.ToLower(strings.TrimSpace(requirement))}
		}
	}
	return []string{}
}

func preflightConfidenceEvidence(plan *CompletionPlan) []string {
	evidence := []string{}
	for _, ranked := range plan.ContextPlan.UsedContext {
		if ranked.Memory.Confidence > 0 {
			evidence = append(evidence, fmt.Sprintf("memory-confidence:%.2f", ranked.Memory.Confidence))
		}
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		if ranked.Score > 0 {
			evidence = append(evidence, fmt.Sprintf("source-relevance:%.2f", ranked.Score))
		}
	}
	if len(evidence) == 0 && len(plan.ContextPlan.UsedContext) == 0 && len(plan.ContextPlan.SourceContext) == 0 &&
		strings.TrimSpace(plan.ContextPlan.Explanation) != "" {
		evidence = append(evidence, "context-confidence:no-retrieved-context-relied-upon")
	}
	return uniqueStrings(evidence)
}

func prefixedEvidence(prefix string, values []string) []string {
	result := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, prefix+value)
		}
	}
	return uniqueStrings(result)
}

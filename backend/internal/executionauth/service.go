package executionauth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/sourceevidence"
	"automation-hub-backend/internal/standingmandate"

	"github.com/google/uuid"
)

type Service struct {
	repository     Repository
	constitution   ConstitutionEvaluator
	mandates       MandateAuthorizer
	agents         AgentAuthorityResolver
	approvals      ApprovalResolver
	now            func() time.Time
	stop           func() EmergencyStopEvidence
	lifeGraph      LifeOntologyProjector
	frameworks     FrameworkSelectionResolver
	preflights     FrameworkEvidencePreflightResolver
	sourceEvidence sourceevidence.Repository
}

func (s *Service) WithSourceEvidenceRepository(repository sourceevidence.Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("source evidence repository is required")
	}
	s.sourceEvidence = repository
	return s, nil
}

// WithFrameworkSelectionResolver installs the owner-scoped immutable
// selection lookup used by selector-v5 authorization. Selector-v5 requests
// fail closed when no resolver is installed.
func (s *Service) WithFrameworkSelectionResolver(
	resolver FrameworkSelectionResolver,
) (*Service, error) {
	if resolver == nil {
		return nil, fmt.Errorf("framework selection resolver is required")
	}
	s.frameworks = resolver
	return s, nil
}

// WithFrameworkEvidencePreflightResolver installs the owner-scoped immutable
// preflight lookup required by selector-v5 authorization. Selector-v5 requests
// fail closed when no resolver is installed.
func (s *Service) WithFrameworkEvidencePreflightResolver(
	resolver FrameworkEvidencePreflightResolver,
) (*Service, error) {
	if resolver == nil {
		return nil, fmt.Errorf("framework evidence preflight resolver is required")
	}
	s.preflights = resolver
	return s, nil
}

func NewService(
	repository Repository,
	constitution ConstitutionEvaluator,
	mandates MandateAuthorizer,
	agents AgentAuthorityResolver,
	approvals ApprovalResolver,
	now func() time.Time,
) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("execution authorization repository is required")
	}
	if constitution == nil {
		return nil, fmt.Errorf("Constitution evaluator is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		repository:   repository,
		constitution: constitution,
		mandates:     mandates,
		agents:       agents,
		approvals:    approvals,
		now:          now,
		stop: func() EmergencyStopEvidence {
			decision := safety.EvaluateEmergencyStop()
			return EmergencyStopEvidence{
				Active: decision.Active,
				Source: decision.Source,
				Reason: decision.Reason,
			}
		},
	}, nil
}

// WithEmergencyStopEvaluator is for deterministic tests and alternate
// composition roots. Production callers should use the default persisted stop.
func (s *Service) WithEmergencyStopEvaluator(
	evaluator func() EmergencyStopEvidence,
) *Service {
	if evaluator != nil {
		s.stop = evaluator
	}
	return s
}

// CloneWithEmergencyStopEvaluator returns an isolated composition for a
// server-owned recovery boundary. It avoids mutating the shared authorizer and
// therefore cannot weaken concurrent ordinary execution decisions.
func (s *Service) CloneWithEmergencyStopEvaluator(
	evaluator func() EmergencyStopEvidence,
) *Service {
	if s == nil {
		return nil
	}
	clone := *s
	if evaluator != nil {
		clone.stop = evaluator
	}
	return &clone
}

func (s *Service) Authorize(ctx context.Context, input Request) (Receipt, error) {
	request, err := normalizeRequest(input)
	if err != nil {
		return Receipt{}, err
	}
	now := monotonicNow(s.now)
	if request.RequestedAt.IsZero() {
		request.RequestedAt = now
	}
	if request.RequestedAt.After(now.Add(5 * time.Minute)) {
		return Receipt{}, fmt.Errorf("execution request time is in the future")
	}
	requestHash, err := requestDigest(request)
	if err != nil {
		return Receipt{}, fmt.Errorf("digest execution authorization request: %w", err)
	}
	receipt := Receipt{
		ID:                   uuid.New(),
		ContractVersion:      ContractVersion,
		OwnerIdentity:        request.OwnerIdentity,
		IdempotencyKey:       request.IdempotencyKey,
		ActorIdentity:        request.ActorIdentity,
		ActorKind:            request.ActorKind,
		TaskID:               request.TaskID,
		Action:               request.Action,
		Stage:                request.Stage,
		ResourceType:         request.ResourceType,
		ResourceID:           request.ResourceID,
		ProjectKey:           request.ProjectKey,
		Domain:               request.Domain,
		RuntimeID:            request.RuntimeID,
		EffectDigest:         request.EffectDigest,
		Outcome:              OutcomeDenied,
		Reason:               "execution is denied until every policy boundary is evaluated",
		RequestDigest:        requestHash,
		RequiredAuthority:    request.RequiredAuthority,
		RequestedAutonomy:    request.RequestedAutonomy,
		EffectiveAutonomy:    request.RequestedAutonomy,
		Risk:                 request.Risk,
		Reversible:           request.Reversible,
		EstimatedCostEUR:     request.EstimatedCostEUR,
		NotificationRequired: request.RequestedAutonomy >= 9,
		EvaluatedAt:          now,
		Evidence: DecisionEvidence{
			ReasonCodes: []string{},
			Trace:       []string{},
		},
	}
	if request.Governance != nil {
		receipt.Evidence.Governance = *request.Governance
	}

	stop := s.stop()
	stop.Reason = safety.RedactSecrets(stop.Reason)
	receipt.Evidence.EmergencyStop = stop
	if stop.Active {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			firstNonEmpty(stop.Reason, "emergency stop is active"),
			"emergency_stop.active",
		)
	}

	if request.ActorKind == ActorSystem {
		workload, workloadErr := evaluateSystemWorkload(request)
		receipt.Evidence.SystemWorkload = workload
		if workloadErr != nil {
			return s.persistDecision(
				ctx,
				receipt,
				OutcomeDenied,
				workloadErr.Error(),
				"system_workload.denied",
			)
		}
	}

	if err := s.verifyFrameworkSelection(ctx, request, &receipt); err != nil {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"framework selection could not be independently verified",
			"framework.selection_unverified",
		)
	}
	if err := s.verifyFrameworkEvidencePreflight(ctx, request, &receipt); err != nil {
		if errors.Is(err, ErrSourceEvidenceUnverified) {
			return s.persistDecision(
				ctx,
				receipt,
				OutcomeDenied,
				"source evidence could not be independently verified",
				"source.evidence_unverified",
			)
		}
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"framework evidence preflight could not be independently verified",
			"framework.evidence_preflight_unverified",
		)
	}

	capabilities := deriveCapabilities(request)
	constitution, err := s.constitution.EvaluateExecutionPolicy(
		request.OwnerIdentity,
		capabilities,
		request.RequiredAuthority,
	)
	if err != nil {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"active Constitution could not be evaluated",
			"constitution.unavailable",
		)
	}
	receipt.Evidence.Constitution = ConstitutionEvidence{
		ID:                           constitution.ID,
		Version:                      constitution.Version,
		Source:                       constitution.Source,
		Digest:                       constitution.Digest,
		RequestedCapabilities:        capabilities,
		DeniedCapabilities:           append([]string(nil), constitution.DeniedCapabilities...),
		ApprovalRequiredCapabilities: append([]string(nil), constitution.ApprovalRequiredCapabilities...),
		AuthorityCeiling:             constitution.AuthorityCeiling,
	}
	if len(constitution.DeniedCapabilities) > 0 {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"active Constitution denies a requested execution capability",
			"constitution.denied",
		)
	}
	if request.RequiredAuthority > constitution.AuthorityCeiling {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"required authority exceeds the active Constitution ceiling",
			"constitution.authority_ceiling",
		)
	}
	receipt.EffectiveAutonomy = min(request.RequestedAutonomy, constitution.AuthorityCeiling)

	if request.ActorKind == ActorAgent {
		if err := s.evaluateAgent(ctx, request, &receipt); err != nil {
			return s.persistDecision(
				ctx,
				receipt,
				OutcomeDenied,
				err.Error(),
				"agent.authority_denied",
			)
		}
	}

	if request.RequestedAutonomy < 6 {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"autonomy levels 0 through 5 cannot perform external execution",
			"autonomy.non_executing",
		)
	}

	approval, approvalProvided, approvalErr := s.resolveApproval(ctx, request, now)
	if approvalErr != nil {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeDenied,
			"claimed approval evidence could not be verified",
			"approval.invalid",
		)
	}
	if approvalProvided {
		receipt.ApprovalSourceID = approval.SourceID
		receipt.Evidence.Approval = ApprovalEvidence{
			SourceID:       approval.SourceID,
			DecisionID:     approval.DecisionID,
			DecisionDigest: approval.DecisionDigest,
			ApprovedBy:     approval.ApprovedBy,
			ApprovedAt:     approval.ApprovedAt,
			ExpiresAt:      approval.ExpiresAt,
		}
	}
	if request.Governance != nil &&
		request.Governance.FrameworkRequiresApproval != nil &&
		*request.Governance.FrameworkRequiresApproval && !approvalProvided {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeRequiresApproval,
			"selected frameworks require an exact case-specific approval",
			"framework.approval_required",
		)
	}

	mandateAuthorized := false
	mandateRequired := request.RequestedAutonomy == 7 || request.RequestedAutonomy == 10
	if request.MandateID != "" {
		authorized, err := s.evaluateMandate(ctx, request, approval, approvalProvided, &receipt)
		if err != nil {
			return s.persistDecision(
				ctx,
				receipt,
				OutcomeDenied,
				err.Error(),
				"mandate.denied",
			)
		}
		mandateAuthorized = authorized
	}
	if mandateRequired && !mandateAuthorized {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeRequiresApproval,
			"requested autonomy requires an active bounded standing mandate",
			"mandate.required",
		)
	}

	approvalRequired := len(constitution.ApprovalRequiredCapabilities) > 0 ||
		request.RequestedAutonomy == 6 ||
		!request.Reversible ||
		request.Risk == RiskHigh ||
		request.Risk == RiskCritical ||
		isConsequentialStage(request.Stage)
	if request.RequestedAutonomy == 8 || request.RequestedAutonomy == 9 {
		if request.Risk != RiskLow || !request.Reversible {
			approvalRequired = true
		}
	}
	if request.RequestedAutonomy == 6 && !approvalProvided {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeRequiresApproval,
			"autonomy level 6 requires an exact case-specific approval",
			"approval.case_required",
		)
	}
	if approvalRequired && !approvalProvided && !mandateAuthorized {
		return s.persistDecision(
			ctx,
			receipt,
			OutcomeRequiresApproval,
			"this execution requires case-specific or standing approval",
			"approval.required",
		)
	}

	return s.persistDecision(
		ctx,
		receipt,
		OutcomeAuthorized,
		"execution is authorized within the evaluated policy boundaries",
		"decision.authorized",
	)
}

// AuthorizeAndConsume reserves a single execution from an authorized receipt.
// It rechecks mutable policy evidence immediately before the reservation.
func (s *Service) AuthorizeAndConsume(
	ctx context.Context,
	request Request,
	consumer string,
	executionTarget string,
) (Receipt, error) {
	receipt, err := s.Authorize(ctx, request)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.Outcome != OutcomeAuthorized {
		return receipt, ErrNotAuthorized
	}
	if err := s.recheck(ctx, request, receipt); err != nil {
		return receipt, err
	}
	consumer = compact(consumer)
	executionTarget = compact(executionTarget)
	if err := validateIdentifier("consumer", consumer); err != nil {
		return receipt, err
	}
	if err := validateIdentifier("execution target", executionTarget); err != nil {
		return receipt, err
	}
	if err := s.repository.Consume(ctx, Consumption{
		ReceiptID:       receipt.ID,
		OwnerIdentity:   receipt.OwnerIdentity,
		Consumer:        consumer,
		ExecutionTarget: executionTarget,
		ReceiptDigest:   receipt.DecisionDigest,
		ConsumedAt:      monotonicNow(s.now),
	}); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func (s *Service) Get(ctx context.Context, owner string, id uuid.UUID) (Receipt, error) {
	return s.repository.Get(ctx, compact(owner), id)
}

func (s *Service) List(ctx context.Context, owner string, limit int) ([]Receipt, error) {
	return s.repository.List(ctx, compact(owner), limit)
}

func (s *Service) GetConsumption(
	ctx context.Context,
	owner string,
	id uuid.UUID,
) (Consumption, error) {
	return s.repository.GetConsumption(ctx, compact(owner), id)
}

func (s *Service) evaluateAgent(
	ctx context.Context,
	request Request,
	receipt *Receipt,
) error {
	if s.agents == nil {
		return ErrPolicyUnavailable
	}
	assignment, err := s.agents.GetAssignment(ctx, request.OwnerIdentity, request.AssignmentID)
	if err != nil {
		return fmt.Errorf("agent assignment is unavailable")
	}
	agent, err := s.agents.Get(ctx, request.OwnerIdentity, request.AgentID)
	if err != nil {
		return fmt.Errorf("assigned agent is unavailable")
	}
	receipt.Evidence.Agent = AgentEvidence{
		AgentID:          agent.ID,
		AgentRevision:    agent.Revision,
		AssignmentID:     assignment.ID,
		GrantedAuthority: assignment.GrantedAuthority,
		GrantedAutonomy:  assignment.GrantedAutonomy,
		RuntimeID:        agent.Runtime.ID,
	}
	if assignment.OwnerIdentity != request.OwnerIdentity ||
		assignment.TaskID != request.TaskID ||
		assignment.AgentID != request.AgentID {
		return fmt.Errorf("agent assignment does not match this task and actor")
	}
	if agent.State != agentregistry.StateEnabled ||
		!agent.Health.Ready ||
		agent.Health.Status == agentregistry.HealthUnhealthy ||
		agent.Health.Status == agentregistry.HealthUnknown ||
		agent.Health.CheckedAt.IsZero() ||
		monotonicNow(s.now).After(agent.Health.CheckedAt.Add(agent.Health.FreshFor)) {
		return fmt.Errorf("assigned agent is not enabled with fresh executable health")
	}
	if request.RequiredAuthority > assignment.GrantedAuthority ||
		request.RequiredAuthority > agent.AuthorityCeiling ||
		request.RequestedAutonomy > assignment.GrantedAutonomy ||
		request.RequestedAutonomy > agent.AutonomyCeiling {
		return fmt.Errorf("requested authority or autonomy exceeds the assignment")
	}
	receipt.EffectiveAutonomy = min(
		receipt.EffectiveAutonomy,
		min(assignment.GrantedAutonomy, agent.AutonomyCeiling),
	)
	if request.RuntimeID != "" && !strings.EqualFold(request.RuntimeID, agent.Runtime.ID) {
		return fmt.Errorf("runtime does not match the assigned agent adapter")
	}
	if request.ToolID != "" && !containsFold(agent.ToolAllowlist, request.ToolID) {
		return fmt.Errorf("requested tool is not in the assigned agent allowlist")
	}
	if !containsAllFold(agent.DataAllowlist, request.DataScopes) {
		return fmt.Errorf("requested data exceeds the assigned agent allowlist")
	}
	if !foldersAllowed(agent.FolderAllowlist, request.FolderPaths) {
		return fmt.Errorf("requested folder exceeds the assigned agent allowlist")
	}
	return nil
}

func (s *Service) resolveApproval(
	ctx context.Context,
	request Request,
	now time.Time,
) (ResolvedApproval, bool, error) {
	if request.ApprovalSourceID == "" {
		return ResolvedApproval{}, false, nil
	}
	if s.approvals == nil {
		return ResolvedApproval{}, true, ErrPolicyUnavailable
	}
	value, err := s.approvals.Resolve(
		ctx,
		request.OwnerIdentity,
		request.ApprovalSourceID,
		request.ApprovalBindingDigest,
	)
	if err != nil {
		return ResolvedApproval{}, true, err
	}
	if value.SourceID != request.ApprovalSourceID ||
		value.BindingDigest != request.ApprovalBindingDigest ||
		value.DecisionID == "" ||
		value.DecisionDigest == "" ||
		value.ApprovedBy == "" ||
		value.ApprovedAt.IsZero() ||
		value.ExpiresAt.IsZero() ||
		value.ApprovedAt.After(now.Add(5*time.Second)) ||
		!now.Before(value.ExpiresAt) {
		return ResolvedApproval{}, true, fmt.Errorf("resolved approval is invalid or expired")
	}
	return value, true, nil
}

func (s *Service) evaluateMandate(
	ctx context.Context,
	request Request,
	approval ResolvedApproval,
	approvalProvided bool,
	receipt *Receipt,
) (bool, error) {
	if s.mandates == nil {
		return false, ErrPolicyUnavailable
	}
	mandateID, err := uuid.Parse(request.MandateID)
	if err != nil || mandateID == uuid.Nil {
		return false, fmt.Errorf("standing mandate id is invalid")
	}
	actionRequest := standingmandate.ActionRequest{
		OwnerIdentity:            request.OwnerIdentity,
		ActorIdentity:            request.ActorIdentity,
		Action:                   request.Action,
		ResourceType:             request.ResourceType,
		ResourceID:               request.ResourceID,
		ProjectKey:               request.ProjectKey,
		Domain:                   request.Domain,
		ToolID:                   firstNonEmpty(request.ToolID, request.RuntimeID),
		Risk:                     standingRisk(request.Risk),
		RequestedAutonomy:        request.RequestedAutonomy,
		UpstreamApprovalRequired: false,
		Facts:                    trustedMandateFacts(request),
		SourceReferences:         request.SourceReferences,
		RequestedAt:              request.RequestedAt,
	}
	decision, err := s.mandates.Authorize(ctx, mandateID, actionRequest)
	if err != nil {
		return false, fmt.Errorf("standing mandate evaluation failed: %w", err)
	}
	// The standing-mandate request digest is intentionally computed by that
	// policy engine. If the mandate requires a case approval, bind trusted
	// evidence to its exact digest and evaluate once more.
	if decision.Outcome == standingmandate.DecisionRequiresApproval && approvalProvided {
		actionRequest.Approval = &standingmandate.ApprovalEvidence{
			ID:            approval.DecisionID,
			ApprovedBy:    approval.ApprovedBy,
			ApproverRoles: append([]string(nil), approval.ApproverRoles...),
			ActionDigest:  decision.Evidence.RequestDigest,
			ApprovedAt:    approval.ApprovedAt,
			ExpiresAt:     approval.ExpiresAt,
			Source:        approval.SourceID,
		}
		decision, err = s.mandates.Authorize(ctx, mandateID, actionRequest)
		if err != nil {
			return false, fmt.Errorf("standing mandate approval evaluation failed: %w", err)
		}
	}
	receipt.Evidence.Mandate = MandateEvidence{
		ID:             decision.MandateID.String(),
		Revision:       decision.Evidence.MandateRevision,
		RequestDigest:  decision.Evidence.RequestDigest,
		MandateDigest:  decision.Evidence.MandateDigest,
		DecisionID:     decision.ID.String(),
		DecisionDigest: decision.Evidence.DecisionDigest,
		Outcome:        string(decision.Outcome),
	}
	switch decision.Outcome {
	case standingmandate.DecisionAuthorized:
		return true, nil
	case standingmandate.DecisionRequiresApproval:
		return false, nil
	default:
		return false, fmt.Errorf("standing mandate denied this exact execution")
	}
}

func trustedMandateFacts(request Request) map[string]string {
	facts := make(map[string]string, len(request.Facts)+4)
	for key, value := range request.Facts {
		facts[key] = value
	}
	requestedAt := request.RequestedAt.UTC()
	facts["estimated_cost_eur"] = fmt.Sprintf("%.6f", request.EstimatedCostEUR)
	facts["requested_at_utc_hour"] = fmt.Sprintf("%02d", requestedAt.Hour())
	facts["requested_at_utc_weekday"] = strings.ToLower(requestedAt.Weekday().String())
	facts["requested_at_utc_date"] = requestedAt.Format(time.DateOnly)
	return facts
}

func (s *Service) recheck(ctx context.Context, request Request, receipt Receipt) error {
	if stop := s.stop(); stop.Active {
		return fmt.Errorf("%w: emergency stop became active", ErrAuthorizationChanged)
	}
	if request.ActorKind == ActorSystem {
		workload, err := evaluateSystemWorkload(request)
		if err != nil || !workload.Matched ||
			workload.PolicyID != receipt.Evidence.SystemWorkload.PolicyID {
			return fmt.Errorf("%w: system workload policy changed", ErrAuthorizationChanged)
		}
	}
	recheck := receipt
	if err := s.verifyFrameworkSelection(ctx, request, &recheck); err != nil ||
		recheck.Evidence.FrameworkSelection != receipt.Evidence.FrameworkSelection {
		return fmt.Errorf("%w: framework selection changed", ErrAuthorizationChanged)
	}
	constitution, err := s.constitution.EvaluateExecutionPolicy(
		request.OwnerIdentity,
		receipt.Evidence.Constitution.RequestedCapabilities,
		request.RequiredAuthority,
	)
	if err != nil || constitution.Digest != receipt.Evidence.Constitution.Digest ||
		len(constitution.DeniedCapabilities) > 0 {
		return fmt.Errorf("%w: Constitution changed", ErrAuthorizationChanged)
	}
	if request.ActorKind == ActorAgent {
		recheck := receipt
		if err := s.evaluateAgent(ctx, request, &recheck); err != nil ||
			recheck.Evidence.Agent.AgentRevision != receipt.Evidence.Agent.AgentRevision {
			return fmt.Errorf("%w: agent authority changed", ErrAuthorizationChanged)
		}
	}
	if receipt.Evidence.Mandate.ID != "" {
		mandateID, err := uuid.Parse(receipt.Evidence.Mandate.ID)
		if err != nil || mandateID == uuid.Nil {
			return fmt.Errorf("%w: mandate identity invalid", ErrAuthorizationChanged)
		}
		decisionID, err := uuid.Parse(receipt.Evidence.Mandate.DecisionID)
		if err != nil || decisionID == uuid.Nil {
			return fmt.Errorf("%w: mandate decision identity invalid", ErrAuthorizationChanged)
		}
		snapshot, snapshotErr := s.mandates.GetAuthorizationSnapshot(
			ctx,
			request.OwnerIdentity,
			mandateID,
		)
		decision, decisionErr := s.mandates.GetDecision(
			ctx,
			request.OwnerIdentity,
			decisionID,
		)
		if snapshotErr != nil || decisionErr != nil || decision == nil ||
			snapshot.ID != mandateID ||
			snapshot.OwnerIdentity != request.OwnerIdentity ||
			snapshot.Status != standingmandate.StatusActive ||
			snapshot.Revision != receipt.Evidence.Mandate.Revision ||
			snapshot.Digest != receipt.Evidence.Mandate.MandateDigest ||
			(snapshot.ExpiresAt != nil && !monotonicNow(s.now).Before(*snapshot.ExpiresAt)) ||
			decision.ID != decisionID ||
			decision.MandateID != mandateID ||
			decision.OwnerIdentity != request.OwnerIdentity ||
			decision.ActorIdentity != request.ActorIdentity ||
			decision.Action != request.Action ||
			decision.Outcome != standingmandate.DecisionAuthorized ||
			decision.Evidence.MandateRevision != receipt.Evidence.Mandate.Revision ||
			decision.Evidence.RequestDigest != receipt.Evidence.Mandate.RequestDigest ||
			decision.Evidence.MandateDigest != receipt.Evidence.Mandate.MandateDigest ||
			decision.Evidence.DecisionDigest != receipt.Evidence.Mandate.DecisionDigest {
			return fmt.Errorf("%w: standing mandate changed", ErrAuthorizationChanged)
		}
	}
	if request.ApprovalSourceID != "" {
		if _, _, err := s.resolveApproval(ctx, request, monotonicNow(s.now)); err != nil {
			return fmt.Errorf("%w: approval changed", ErrAuthorizationChanged)
		}
	}
	// Keep this as the final policy lookup before the caller atomically claims
	// the receipt. A preflight removed or changed after authorization therefore
	// cannot survive into consumption.
	recheck = receipt
	if err := s.verifyFrameworkEvidencePreflight(ctx, request, &recheck); err != nil ||
		recheck.Evidence.FrameworkEvidencePreflight != receipt.Evidence.FrameworkEvidencePreflight {
		if errors.Is(err, ErrSourceEvidenceUnverified) {
			return fmt.Errorf(
				"%w: source.evidence_unverified: source evidence changed",
				ErrAuthorizationChanged,
			)
		}
		return fmt.Errorf(
			"%w: framework.evidence_preflight_unverified: framework evidence preflight changed",
			ErrAuthorizationChanged,
		)
	}
	return nil
}

func (s *Service) persist(
	ctx context.Context,
	receipt Receipt,
	_ error,
) (Receipt, error) {
	decisionDigest, err := finishDigest(receipt)
	if err != nil {
		return Receipt{}, err
	}
	receipt.DecisionDigest = decisionDigest
	stored, _, err := s.repository.CreateOrGet(ctx, receipt)
	if err != nil {
		return Receipt{}, err
	}
	s.projectReceipt(ctx, &stored)
	return stored, nil
}

func (s *Service) persistDecision(
	ctx context.Context,
	receipt Receipt,
	outcome Outcome,
	reason string,
	code string,
) (Receipt, error) {
	finished, err := finish(receipt, outcome, reason, code)
	return s.persist(ctx, finished, err)
}

func finish(receipt Receipt, outcome Outcome, reason, code string) (Receipt, error) {
	receipt.Outcome = outcome
	receipt.Reason = safety.RedactSecrets(reason)
	receipt.Evidence.ReasonCodes = append(receipt.Evidence.ReasonCodes, code)
	receipt.Evidence.Trace = append(receipt.Evidence.Trace, receipt.Reason)
	return receipt, nil
}

func deriveCapabilities(request Request) []string {
	values := map[string]struct{}{
		"execution": {},
	}
	if request.ToolID != "" {
		values["tool-execution"] = struct{}{}
	}
	if request.RuntimeID != "" || len(request.FolderPaths) > 0 {
		values["local-execution"] = struct{}{}
	}
	if request.Stage == StageDataAccess {
		values["document-read"] = struct{}{}
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	resourceType := strings.ToLower(strings.TrimSpace(request.ResourceType))
	if strings.Contains(action, ".api.") ||
		strings.Contains(action, ".http.") ||
		strings.Contains(resourceType, "api") ||
		strings.Contains(resourceType, "network") ||
		strings.Contains(resourceType, "http") ||
		strings.Contains(resourceType, "browser") {
		values["web-access"] = struct{}{}
	}
	switch request.Stage {
	case StageCommunication:
		values["external-communication"] = struct{}{}
	case StageExpenditure:
		values["financial-action"] = struct{}{}
	case StagePublication:
		values["external-communication"] = struct{}{}
		values["public-posting"] = struct{}{}
	case StageDeletion:
		values["destructive-action"] = struct{}{}
	case StagePrivilegeEscalation:
		values["account-change"] = struct{}{}
	case StageCommitment:
		values["consequential-action"] = struct{}{}
	case StageSelfModification:
		values["consequential-action"] = struct{}{}
	}
	domain := strings.ToLower(request.Domain)
	if strings.Contains(domain, "legal") || strings.Contains(domain, "government") {
		values["legal-government-action"] = struct{}{}
	}
	if request.Risk == RiskHigh || request.Risk == RiskCritical ||
		isConsequentialStage(request.Stage) {
		values["consequential-action"] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func isConsequentialStage(stage Stage) bool {
	switch stage {
	case StageExpenditure, StageCommunication, StageCommitment, StagePublication,
		StageDeletion, StagePrivilegeEscalation, StageSelfModification:
		return true
	default:
		return false
	}
}

func standingRisk(value RiskLevel) standingmandate.RiskLevel {
	switch value {
	case RiskLow:
		return standingmandate.RiskLow
	case RiskMedium:
		return standingmandate.RiskMedium
	case RiskHigh:
		return standingmandate.RiskHigh
	default:
		return standingmandate.RiskCritical
	}
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func containsAllFold(allowed, requested []string) bool {
	for _, value := range requested {
		if !containsFold(allowed, value) {
			return false
		}
	}
	return true
}

func foldersAllowed(allowed, requested []string) bool {
	for _, candidate := range requested {
		candidate = filepath.Clean(candidate)
		permitted := false
		for _, root := range allowed {
			root = filepath.Clean(root)
			relative, err := filepath.Rel(root, candidate)
			if err == nil && relative != ".." &&
				!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				permitted = true
				break
			}
		}
		if !permitted {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

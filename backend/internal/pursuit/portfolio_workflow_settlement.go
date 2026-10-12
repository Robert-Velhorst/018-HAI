package pursuit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

const (
	PortfolioWorkflowSettlementConfirmation = "SETTLE VERIFIED PORTFOLIO WORK"
	PortfolioWorkflowSettlementAuthority    = "verified_accounting_only"
	PortfolioWorkflowSettlementEvent        = "pursuit.portfolio_workflow_settled"
)

type PortfolioWorkflowSettlementRequest struct {
	WorkflowID          string `json:"workflowId"`
	ExpectedItemDigest  string `json:"expectedItemDigest"`
	ActualEffortMinutes int64  `json:"actualEffortMinutes"`
	ActualCostMicros    int64  `json:"actualCostMicros"`
	Confirmation        string `json:"confirmation"`
}

type PortfolioWorkflowSettlementResult struct {
	PursuitID                   uuid.UUID             `json:"pursuitId"`
	ProposalItemID              uuid.UUID             `json:"proposalItemId"`
	ReservationID               uuid.UUID             `json:"reservationId"`
	WorkflowID                  uuid.UUID             `json:"workflowId"`
	Disposition                 string                `json:"disposition"`
	ActualEffortMinutes         int64                 `json:"actualEffortMinutes"`
	ActualCostMicros            int64                 `json:"actualCostMicros"`
	VerificationStatus          string                `json:"verificationStatus"`
	EvidenceURI                 string                `json:"evidenceUri"`
	CompletionAttestationID     uuid.UUID             `json:"completionAttestationId"`
	CompletionAttestationDigest string                `json:"completionAttestationDigest"`
	SettlementProofID           uuid.UUID             `json:"settlementProofId"`
	SettlementProofDigest       string                `json:"settlementProofDigest"`
	LearningOutcomeID           string                `json:"learningOutcomeId,omitempty"`
	LearningStatus              string                `json:"learningStatus"`
	LearningProposalID          string                `json:"learningProposalId,omitempty"`
	LearningProposalStatus      string                `json:"learningProposalStatus"`
	LearningSampleCount         int                   `json:"learningSampleCount"`
	LearningNewEvidenceCount    int                   `json:"learningNewEvidenceCount"`
	LearningDriftDetected       bool                  `json:"learningDriftDetected"`
	LearningReviewRequired      bool                  `json:"learningReviewRequired"`
	Replayed                    bool                  `json:"replayed"`
	Authority                   string                `json:"authority"`
	CanExecute                  bool                  `json:"canExecute"`
	ResourceUsage               *PursuitResourceUsage `json:"resourceUsage"`
}

// SettlePortfolioWorkflowForOwner appends measured usage only after an exact
// receipt-bound workflow has reached verified completion. This is deliberately
// separate from execution: a successful external effect can never be replayed
// merely because accounting needs reconciliation.
func (s *service) SettlePortfolioWorkflowForOwner(
	ctx context.Context,
	ownerIdentity, actor string,
	itemID uuid.UUID,
	request PortfolioWorkflowSettlementRequest,
) (*PortfolioWorkflowSettlementResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actor = strings.TrimSpace(actor)
	request.WorkflowID = strings.TrimSpace(request.WorkflowID)
	request.ExpectedItemDigest = strings.TrimSpace(request.ExpectedItemDigest)
	request.Confirmation = strings.TrimSpace(request.Confirmation)
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if ownerIdentity == "" || actor == "" || ownerIdentity != actor {
		return nil, fmt.Errorf("the authenticated owner must settle verified portfolio work")
	}
	if itemID == uuid.Nil || !validPortfolioRecordDigest(request.ExpectedItemDigest) {
		return nil, fmt.Errorf("an exact portfolio proposal item is required")
	}
	workflowID, err := uuid.Parse(request.WorkflowID)
	if err != nil || workflowID == uuid.Nil {
		return nil, fmt.Errorf("a valid completed workflow id is required")
	}
	if request.Confirmation != PortfolioWorkflowSettlementConfirmation {
		return nil, fmt.Errorf("exact verified portfolio settlement confirmation is required")
	}
	if request.ActualEffortMinutes < 0 || request.ActualCostMicros < 0 {
		return nil, fmt.Errorf("actual effort and cost cannot be negative")
	}
	if s.portfolioWorkflowExecutor == nil {
		return nil, fmt.Errorf("portfolio workflow settlement verification is unavailable")
	}
	settlementRepository, ok := s.repo.(portfolioWorkflowSettlementRepository)
	if !ok {
		return nil, fmt.Errorf("durable portfolio workflow settlement proof storage is unavailable")
	}
	existingProof, existingSettlement, err := settlementRepository.FindPortfolioWorkflowSettlementProof(ownerIdentity, itemID, workflowID)
	if err != nil {
		return nil, fmt.Errorf("inspect portfolio workflow settlement replay: %w", err)
	}
	if existingProof != nil {
		if existingSettlement == nil || existingProof.ProposalItemDigest != request.ExpectedItemDigest ||
			existingProof.ActualEffortMinutes != request.ActualEffortMinutes ||
			existingProof.ActualCostMicros != request.ActualCostMicros || existingProof.Actor != actor {
			return nil, fmt.Errorf("portfolio workflow reservation was already settled with a different outcome")
		}
		attestation, attestationErr := settlementRepository.FindWorkflowCompletionAttestation(ownerIdentity, workflowID)
		if attestationErr != nil || attestation == nil || attestation.ID != existingProof.CompletionAttestationID ||
			attestation.RecordDigest != existingProof.CompletionAttestationDigest {
			return nil, fmt.Errorf("immutable workflow completion evidence for settlement replay is unavailable")
		}
		usage, usageErr := s.ResourceUsageForOwner(ownerIdentity, existingProof.PursuitID)
		if usageErr != nil {
			return nil, fmt.Errorf("read resource usage after settlement replay: %w", usageErr)
		}
		result := portfolioWorkflowSettlementResult(existingProof, existingSettlement, attestation, usage, true)
		applyPortfolioSettlementLearning(result, s.recordPortfolioSettlementOutcome(existingProof, existingSettlement, attestation))
		return result, nil
	}
	approvalRepository, ok := s.repo.(PortfolioWorkflowEffectApprovalRepository)
	if !ok {
		return nil, fmt.Errorf("durable portfolio workflow approval storage is unavailable")
	}
	decisionRepository, ok := s.repo.(interface {
		LoadPortfolioExecutionProposalDecisionSnapshot(string, uuid.UUID) (*portfolioExecutionProposalDecisionSnapshot, error)
	})
	if !ok {
		return nil, fmt.Errorf("portfolio workflow settlement lookup is unavailable")
	}
	current, err := decisionRepository.LoadPortfolioExecutionProposalDecisionSnapshot(ownerIdentity, itemID)
	if err != nil || current == nil {
		return nil, fmt.Errorf("portfolio execution proposal item is unavailable to this owner")
	}
	if err := validatePortfolioExecutionDecisionSource(ownerIdentity, current); err != nil {
		return nil, err
	}
	if current.Item.RecordDigest != request.ExpectedItemDigest {
		return nil, fmt.Errorf("portfolio proposal item changed; inspect the immutable item before settlement")
	}

	record, receiptURI, receiptID, err := s.loadPortfolioSettlementWorkflow(
		ownerIdentity, current.Pursuit.ID, workflowID,
	)
	if err != nil {
		return nil, err
	}
	receipt, err := s.portfolioWorkflowExecutor.Get(ctx, ownerIdentity, receiptID)
	if err != nil {
		return nil, fmt.Errorf("load portfolio workflow authorization receipt: %w", err)
	}
	decisionID, err := portfolioWorkflowDecisionIDFromApprovalSource(receipt.ApprovalSourceID)
	if err != nil {
		return nil, err
	}
	snapshot, err := approvalRepository.LoadPortfolioWorkflowEffectApprovalSnapshot(ctx, ownerIdentity, decisionID)
	if err != nil || snapshot == nil {
		return nil, fmt.Errorf("load receipt-bound portfolio workflow approval: %w", firstPortfolioError(err, ErrPortfolioWorkflowApprovalUnavailable))
	}
	if snapshot.Item.ID != itemID || snapshot.Item.RecordDigest != request.ExpectedItemDigest ||
		snapshot.Pursuit.ID != current.Pursuit.ID {
		return nil, fmt.Errorf("portfolio workflow receipt is bound to a different proposal item")
	}
	source := &portfolioExecutionProposalDecisionSnapshot{
		Proposal: snapshot.Proposal, Item: snapshot.Item,
		AllocationItem: snapshot.AllocationItem, Pursuit: snapshot.Pursuit,
		Settled: snapshot.Settled, LatestDecision: snapshot.LatestDecision,
	}
	if err := validatePortfolioExecutionDecisionSource(ownerIdentity, source); err != nil {
		return nil, err
	}
	if snapshot.Decision.Decision != PortfolioExecutionDecisionApproved {
		return nil, fmt.Errorf("portfolio workflow receipt is not backed by an approved decision")
	}
	if err := validatePortfolioExecutionDecisionEvidence(ownerIdentity, snapshot.Item, &snapshot.Decision); err != nil {
		return nil, err
	}
	effect, err := buildPortfolioWorkflowEffect(snapshot)
	if err != nil {
		return nil, err
	}
	authorizationRequest := buildPortfolioWorkflowAuthorizationRequest(
		ownerIdentity, actor, itemID, request.ExpectedItemDigest,
		snapshot.Decision.RecordDigest, snapshot, effect,
	)
	if receipt.Outcome != executionauth.OutcomeAuthorized ||
		!portfolioWorkflowReceiptMatches(receipt, authorizationRequest, effect) {
		return nil, fmt.Errorf("portfolio workflow authorization receipt does not match the settled effect")
	}
	target, err := portfolioWorkflowExecutionTarget(effect.EffectDigest)
	if err != nil {
		return nil, err
	}
	consumption, err := s.portfolioWorkflowExecutor.GetConsumption(ctx, ownerIdentity, receiptID)
	if err != nil || !portfolioWorkflowConsumptionMatches(consumption, receipt, target) {
		return nil, fmt.Errorf("portfolio workflow authorization consumption is missing or mismatched: %w", firstPortfolioError(err, executionauth.ErrFinalEffectMismatch))
	}
	exactRecord, linked, err := s.loadPortfolioWorkflowEffect(snapshot.Pursuit.ID, ownerIdentity, receipt, receiptURI)
	if err != nil || !linked || exactRecord == nil || exactRecord.Item.ID != workflowID {
		return nil, fmt.Errorf("authorization receipt is not linked to one exact portfolio workflow: %w", firstPortfolioError(err, ErrPortfolioWorkflowApprovalUnavailable))
	}
	record = exactRecord
	attestation, err := settlementRepository.FindWorkflowCompletionAttestation(ownerIdentity, workflowID)
	if err != nil || attestation == nil || attestation.WorkflowID != workflowID ||
		attestation.OwnerIdentity != ownerIdentity || attestation.CompletionStatus != workflow.StateCompleted ||
		!portfolioSettlementAcceptsVerification(attestation.VerificationStatus) ||
		attestation.RecordDigest == "" {
		return nil, fmt.Errorf("portfolio workflow is not backed by immutable verified completion")
	}
	if record.Item.CurrentState != workflow.StateCompleted || record.Item.CompletedAt == nil ||
		strings.TrimSpace(record.Item.LastTaskPlanID) == "" || record.Item.LastTaskPlanID != attestation.TaskPlanID ||
		!record.Item.CompletedAt.Equal(attestation.CompletedAt) || record.Item.VerificationStatus != attestation.VerificationStatus {
		return nil, fmt.Errorf("portfolio workflow is not backed by verified completion")
	}

	reservationRepository, ok := s.repo.(pursuitResourceReservationRepository)
	if !ok {
		return nil, fmt.Errorf("pursuit resource reservation ledger is unavailable")
	}
	reservation, err := reservationRepository.FindResourceReservationByID(
		ownerIdentity, snapshot.Pursuit.ID, snapshot.Item.ReservationID,
	)
	if err != nil {
		return nil, fmt.Errorf("portfolio resource reservation is unavailable")
	}
	now := time.Now().UTC().Truncate(time.Second)
	evidenceURI := "hai://workflow-completion-attestations/" + attestation.ID.String()
	settlement := &models.PursuitResourceReservationSettlement{
		ID: uuid.New(), ReservationID: reservation.ID, PursuitID: snapshot.Pursuit.ID,
		OwnerIdentity: ownerIdentity, Disposition: ResourceReservationConsumed,
		ActualEffortMinutes: request.ActualEffortMinutes, ActualCostMicros: request.ActualCostMicros,
		EvidenceURI: evidenceURI, Reason: "verified receipt-bound portfolio workflow completed",
		Actor: actor, SettledAt: now,
	}
	if settlement.ActualCostMicros > 0 {
		settlement.Currency = "EUR"
	}
	settlement.RecordDigest, err = settlementDigest(settlement)
	if err != nil {
		return nil, err
	}
	events, err := settlementResourceEvents(*settlement, reservation.OperationID, now)
	if err != nil {
		return nil, err
	}
	activities := []models.PursuitActivity{
		newPursuitResourceActivity(snapshot.Pursuit.ID, "pursuit.resource_reservation_settled",
			fmt.Sprintf("Resource reservation consumed with %d minutes and EUR %.6f actual usage.", request.ActualEffortMinutes, float64(request.ActualCostMicros)/1_000_000),
			actor, "pursuit_resource_reservation", reservation.ID.String(), evidenceURI, now),
		newPursuitResourceActivity(snapshot.Pursuit.ID, PortfolioWorkflowSettlementEvent,
			"Settled measured usage after the receipt-bound workflow reached verified completion.",
			actor, LinkWorkflow, workflowID.String(), evidenceURI, now),
	}
	proof := &models.PursuitPortfolioWorkflowSettlementProof{
		ID: uuid.New(), SettlementID: settlement.ID, SettlementDigest: settlement.RecordDigest,
		ReservationID: reservation.ID, PursuitID: snapshot.Pursuit.ID, OwnerIdentity: ownerIdentity,
		ProposalItemID: itemID, ProposalItemDigest: request.ExpectedItemDigest,
		ApprovalDecisionID: snapshot.Decision.ID, ApprovalDecisionDigest: snapshot.Decision.RecordDigest,
		AuthorizationReceiptID: receipt.ID, AuthorizationReceiptDigest: receipt.DecisionDigest,
		AuthorizationConsumptionKey: consumption.ReceiptDigest, AuthorizationTarget: consumption.ExecutionTarget,
		WorkflowID: workflowID, CompletionAttestationID: attestation.ID,
		CompletionAttestationDigest: attestation.RecordDigest,
		ActualEffortMinutes:         request.ActualEffortMinutes, ActualCostMicros: request.ActualCostMicros,
		Currency: settlement.Currency, Actor: actor, CreatedAt: now,
	}
	proof.RequestDigest, err = portfolioWorkflowSettlementRequestDigest(proof)
	if err != nil {
		return nil, err
	}
	proof.RecordDigest, err = portfolioWorkflowSettlementProofDigest(proof)
	if err != nil {
		return nil, err
	}
	storedProof, storedSettlement, created, err := settlementRepository.SettleVerifiedPortfolioWorkflow(portfolioWorkflowSettlementCommit{
		Settlement: settlement, Proof: proof, Events: events, Activities: activities,
	})
	if err != nil {
		return nil, fmt.Errorf("settle verified portfolio workflow: %w", err)
	}
	usage, err := s.ResourceUsageForOwner(ownerIdentity, snapshot.Pursuit.ID)
	if err != nil {
		return nil, fmt.Errorf("read resource usage after settlement: %w", err)
	}
	result := portfolioWorkflowSettlementResult(storedProof, storedSettlement, attestation, usage, !created)
	applyPortfolioSettlementLearning(result, s.recordPortfolioSettlementOutcome(storedProof, storedSettlement, attestation))
	return result, nil
}

func applyPortfolioSettlementLearning(result *PortfolioWorkflowSettlementResult, learning portfolioSettlementLearningResult) {
	if result == nil {
		return
	}
	result.LearningOutcomeID = learning.OutcomeID
	result.LearningStatus = learning.Status
	result.LearningProposalID = learning.ProposalID
	result.LearningProposalStatus = learning.ProposalStatus
	result.LearningSampleCount = learning.SampleCount
	result.LearningNewEvidenceCount = learning.NewEvidenceCount
	result.LearningDriftDetected = learning.DriftDetected
	result.LearningReviewRequired = learning.ReviewRequired
}

func (s *service) loadPortfolioSettlementWorkflow(
	ownerIdentity string,
	pursuitID, workflowID uuid.UUID,
) (*workflow.WorkflowRecord, string, uuid.UUID, error) {
	links, err := s.repo.FindLinks(pursuitID)
	if err != nil {
		return nil, "", uuid.Nil, fmt.Errorf("inspect portfolio workflow link: %w", err)
	}
	receiptURI := ""
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.Relationship == PortfolioWorkflowEffectRelationship &&
			link.LinkID == workflowID.String() {
			if receiptURI != "" && receiptURI != strings.TrimSpace(link.SourceURI) {
				return nil, "", uuid.Nil, fmt.Errorf("portfolio workflow has conflicting authorization links")
			}
			receiptURI = strings.TrimSpace(link.SourceURI)
		}
	}
	if receiptURI == "" {
		return nil, "", uuid.Nil, fmt.Errorf("workflow is not the receipt-bound workflow for this pursuit")
	}
	items, err := s.repo.FindLinkedWorkflows([]uuid.UUID{workflowID})
	if err != nil || len(items) != 1 {
		return nil, "", uuid.Nil, fmt.Errorf("receipt-bound workflow is missing or ambiguous")
	}
	item := items[0]
	if item.ID != workflowID || item.OwnerIdentity != ownerIdentity ||
		item.SourceType != PortfolioWorkflowEffectSourceType || item.SourceURI != receiptURI {
		return nil, "", uuid.Nil, fmt.Errorf("workflow does not match the owner-scoped portfolio receipt")
	}
	receiptID, err := uuid.Parse(strings.TrimSpace(item.SourceID))
	if err != nil || receiptID == uuid.Nil || receiptURI != "hai://execution-authorization-receipts/"+receiptID.String() {
		return nil, "", uuid.Nil, fmt.Errorf("workflow contains an invalid portfolio authorization receipt")
	}
	return &workflow.WorkflowRecord{Item: item}, receiptURI, receiptID, nil
}

func portfolioSettlementAcceptsVerification(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verified", "test_passed":
		return true
	default:
		return false
	}
}

type portfolioWorkflowSettlementDigestPayload struct {
	OwnerIdentity, ProposalItemID, ProposalItemDigest, ApprovalDecisionID, ApprovalDecisionDigest string
	ReceiptID, ReceiptDigest, ConsumptionDigest, AuthorizationTarget, WorkflowID                  string
	AttestationID, AttestationDigest, ReservationID                                               string
	ActualEffortMinutes, ActualCostMicros                                                         int64
}

func portfolioWorkflowSettlementRequestDigest(value *models.PursuitPortfolioWorkflowSettlementProof) (string, error) {
	payload := portfolioWorkflowSettlementDigestPayload{
		value.OwnerIdentity, value.ProposalItemID.String(), value.ProposalItemDigest,
		value.ApprovalDecisionID.String(), value.ApprovalDecisionDigest,
		value.AuthorizationReceiptID.String(), value.AuthorizationReceiptDigest,
		value.AuthorizationConsumptionKey, value.AuthorizationTarget, value.WorkflowID.String(),
		value.CompletionAttestationID.String(), value.CompletionAttestationDigest,
		value.ReservationID.String(), value.ActualEffortMinutes, value.ActualCostMicros,
	}
	return digestReservationPayload(payload)
}

func portfolioWorkflowSettlementProofDigest(value *models.PursuitPortfolioWorkflowSettlementProof) (string, error) {
	payload := struct {
		RequestDigest, SettlementID, SettlementDigest, Actor, Currency, SettledAt string
	}{
		value.RequestDigest, value.SettlementID.String(), value.SettlementDigest,
		value.Actor, value.Currency, value.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	return digestReservationPayload(payload)
}

func portfolioWorkflowSettlementResult(
	proof *models.PursuitPortfolioWorkflowSettlementProof,
	settlement *models.PursuitResourceReservationSettlement,
	attestation *models.WorkflowCompletionAttestation,
	usage *PursuitResourceUsage,
	replayed bool,
) *PortfolioWorkflowSettlementResult {
	return &PortfolioWorkflowSettlementResult{
		PursuitID: proof.PursuitID, ProposalItemID: proof.ProposalItemID,
		ReservationID: proof.ReservationID, WorkflowID: proof.WorkflowID,
		Disposition: settlement.Disposition, ActualEffortMinutes: settlement.ActualEffortMinutes,
		ActualCostMicros: settlement.ActualCostMicros, VerificationStatus: attestation.VerificationStatus,
		EvidenceURI:             settlement.EvidenceURI,
		CompletionAttestationID: attestation.ID, CompletionAttestationDigest: attestation.RecordDigest,
		SettlementProofID: proof.ID, SettlementProofDigest: proof.RecordDigest,
		LearningStatus: PortfolioLearningUnavailable,
		Replayed:       replayed,
		Authority:      PortfolioWorkflowSettlementAuthority, CanExecute: false, ResourceUsage: usage,
	}
}

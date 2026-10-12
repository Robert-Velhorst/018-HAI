package pursuit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

const (
	PortfolioDispatchConfirmation       = "DISPATCH APPROVED PORTFOLIO WORKFLOWS"
	PortfolioDispatchAuthority          = "portfolio_dispatch_result"
	PortfolioCoordinationAuthority      = "coordination_preview_only"
	PortfolioDispatchMaxItems           = 20
	PortfolioCoordinationMaxItems       = 500
	PortfolioDispatchMaxAttemptsPerItem = 1000

	PortfolioDispatchOutcomeWorkflowCreated = "workflow_created"
	PortfolioDispatchOutcomeReplayed        = "replayed"
	PortfolioDispatchOutcomeNeedsApproval   = "needs_approval"
	PortfolioDispatchOutcomeBlocked         = "blocked"
	PortfolioDispatchOutcomeStale           = "stale"
	PortfolioDispatchOutcomeFailed          = "failed"
	PortfolioDispatchOutcomeCancelled       = "cancelled"
	PortfolioCoordinationFreshnessCurrent   = "current_coordination_snapshot"
)

type PortfolioDispatchItemRequest struct {
	ProposalItemID         string `json:"proposalItemId"`
	ExpectedItemDigest     string `json:"expectedItemDigest"`
	ExpectedDecisionDigest string `json:"expectedDecisionDigest"`
}

type PortfolioDispatchRequest struct {
	ExpectedProposalDigest string                         `json:"expectedProposalDigest"`
	Items                  []PortfolioDispatchItemRequest `json:"items"`
	Confirmation           string                         `json:"confirmation"`
}

type PortfolioDispatchResult struct {
	Run         models.PursuitPortfolioDispatchRun          `json:"run"`
	Items       []models.PursuitPortfolioDispatchItemResult `json:"items"`
	Status      string                                      `json:"status"`
	Created     int                                         `json:"created"`
	Replayed    int                                         `json:"replayed"`
	NeedsReview int                                         `json:"needsReview"`
	Failed      int                                         `json:"failed"`
	Resumed     bool                                        `json:"resumed"`
	Authority   string                                      `json:"authority"`
	CanExecute  bool                                        `json:"canExecute"`
}

type PortfolioCoordinationItem struct {
	Item           models.PursuitPortfolioExecutionProposalItem      `json:"item"`
	Eligibility    string                                            `json:"eligibility"`
	Reason         string                                            `json:"reason"`
	Decision       *models.PursuitPortfolioExecutionProposalDecision `json:"decision,omitempty"`
	LatestDispatch *models.PursuitPortfolioDispatchItemResult        `json:"latestDispatch,omitempty"`
	Selectable     bool                                              `json:"selectable"`
}

type PortfolioCoordinationResult struct {
	Proposal      models.PursuitPortfolioExecutionProposal `json:"proposal"`
	Items         []PortfolioCoordinationItem              `json:"items"`
	DispatchRuns  []models.PursuitPortfolioDispatchRun     `json:"dispatchRuns"`
	Eligible      int                                      `json:"eligible"`
	NeedsApproval int                                      `json:"needsApproval"`
	Blocked       int                                      `json:"blocked"`
	Stale         int                                      `json:"stale"`
	Dispatched    int                                      `json:"dispatched"`
	Authority     string                                   `json:"authority"`
	CanExecute    bool                                     `json:"canExecute"`
	Freshness     PortfolioCoordinationFreshness           `json:"freshness"`
}

type PortfolioCoordinationFreshness struct {
	Status               string    `json:"status"`
	RevalidationRequired bool      `json:"revalidationRequired"`
	CheckedAt            time.Time `json:"checkedAt"`
	Reason               string    `json:"reason"`
}

type normalizedPortfolioDispatchItem struct {
	ID             uuid.UUID
	ItemDigest     string
	DecisionDigest string
}

// PortfolioDispatchCoordinationForOwner derives current eligibility from the
// same approval snapshots used by execution. It is observational only.
func (s *service) PortfolioDispatchCoordinationForOwner(
	ctx context.Context,
	ownerIdentity string,
	proposalID uuid.UUID,
) (*PortfolioCoordinationResult, error) {
	results, err := s.PortfolioDispatchCoordinationBatchForOwner(ctx, ownerIdentity, []uuid.UUID{proposalID})
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("portfolio execution proposal is unavailable to this owner")
	}
	return &results[0], nil
}

// PortfolioDispatchCoordinationBatchForOwner restores current coordination
// for a bounded set of immutable proposals using one aggregate repository
// read. It remains observational: no item is selected, approved, reserved, or
// dispatched by this method.
func (s *service) PortfolioDispatchCoordinationBatchForOwner(
	ctx context.Context,
	ownerIdentity string,
	proposalIDs []uuid.UUID,
) ([]PortfolioCoordinationResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || len(proposalIDs) == 0 || len(proposalIDs) > PortfolioDispatchMaxItems {
		return nil, fmt.Errorf("portfolio coordination requires between 1 and %d proposal ids", PortfolioDispatchMaxItems)
	}
	requested := make(map[uuid.UUID]struct{}, len(proposalIDs))
	for _, proposalID := range proposalIDs {
		if proposalID == uuid.Nil {
			return nil, fmt.Errorf("portfolio coordination proposal ids must be valid")
		}
		if _, duplicate := requested[proposalID]; duplicate {
			return nil, fmt.Errorf("portfolio coordination proposal ids must be unique")
		}
		requested[proposalID] = struct{}{}
	}
	repository, ok := s.repo.(pursuitPortfolioDispatchCoordinationRepository)
	if !ok {
		return nil, fmt.Errorf("durable portfolio coordination storage is unavailable")
	}
	evidenceByProposal, err := repository.LoadPortfolioDispatchCoordinationEvidence(
		ctx, ownerIdentity, proposalIDs, 10,
	)
	if err != nil {
		return nil, fmt.Errorf("load portfolio coordination evidence: %w", err)
	}
	if len(evidenceByProposal) != len(proposalIDs) {
		return nil, fmt.Errorf("one or more portfolio execution proposals are unavailable to this owner")
	}
	for proposalID := range evidenceByProposal {
		if _, expected := requested[proposalID]; !expected {
			return nil, fmt.Errorf("portfolio coordination evidence crossed its requested proposal boundary")
		}
	}
	checkedAt := time.Now().UTC()
	results := make([]PortfolioCoordinationResult, 0, len(evidenceByProposal))
	for _, proposalID := range proposalIDs {
		evidence, exists := evidenceByProposal[proposalID]
		if !exists {
			continue
		}
		result, err := portfolioCoordinationFromEvidence(ctx, ownerIdentity, evidence, checkedAt)
		if err != nil {
			return nil, fmt.Errorf("derive portfolio coordination for %s: %w", proposalID, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func portfolioCoordinationFromEvidence(
	ctx context.Context,
	ownerIdentity string,
	evidence portfolioDispatchCoordinationEvidence,
	checkedAt time.Time,
) (PortfolioCoordinationResult, error) {
	if err := validatePortfolioExecutionProposalEvidence(ownerIdentity, &evidence.Proposal, evidence.Items); err != nil {
		return PortfolioCoordinationResult{}, err
	}
	result := PortfolioCoordinationResult{
		Proposal: evidence.Proposal,
		DispatchRuns: append(
			[]models.PursuitPortfolioDispatchRun{},
			evidence.DispatchRuns...,
		),
		Items:     make([]PortfolioCoordinationItem, 0, len(evidence.Items)),
		Authority: PortfolioCoordinationAuthority, CanExecute: false,
		Freshness: currentPortfolioCoordinationFreshness(checkedAt),
	}
	for _, item := range evidence.Items {
		coordination := PortfolioCoordinationItem{Item: item}
		if latest, exists := evidence.LatestDispatch[item.ID]; exists {
			copy := latest
			coordination.LatestDispatch = &copy
			if portfolioDispatchOutcomeTerminal(latest.Outcome) {
				coordination.Eligibility = "dispatched"
				coordination.Reason = "A receipt-bound review-gated workflow already exists for the latest dispatch."
				result.Dispatched++
				result.Items = append(result.Items, coordination)
				continue
			}
		}
		eligibility, reason, decision, err := portfolioDispatchEligibilityFromSnapshot(
			ctx, ownerIdentity, item, evidence.ApprovalSnapshots[item.ID], checkedAt,
		)
		if err != nil {
			return PortfolioCoordinationResult{}, err
		}
		coordination.Eligibility = eligibility
		coordination.Reason = reason
		coordination.Decision = decision
		coordination.Selectable = eligibility == "eligible"
		switch eligibility {
		case "eligible":
			result.Eligible++
		case PortfolioDispatchOutcomeNeedsApproval:
			result.NeedsApproval++
		case PortfolioDispatchOutcomeBlocked:
			result.Blocked++
		default:
			result.Stale++
		}
		result.Items = append(result.Items, coordination)
	}
	return result, nil
}

func currentPortfolioCoordinationFreshness(checkedAt time.Time) PortfolioCoordinationFreshness {
	return PortfolioCoordinationFreshness{
		Status: PortfolioCoordinationFreshnessCurrent, RevalidationRequired: true,
		CheckedAt: checkedAt.UTC(),
		Reason:    "This is a current read-only coordination snapshot; dispatch independently revalidates every selected approval and immutable binding.",
	}
}

// DispatchPortfolioWorkflowsForOwner coordinates only explicitly selected,
// currently approved items. It delegates every authority check and concrete
// effect to the existing per-item authorization and execution boundaries.
func (s *service) DispatchPortfolioWorkflowsForOwner(
	ctx context.Context,
	ownerIdentity, actor string,
	proposalID uuid.UUID,
	request PortfolioDispatchRequest,
) (*PortfolioDispatchResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actor = strings.TrimSpace(actor)
	request.ExpectedProposalDigest = strings.TrimSpace(request.ExpectedProposalDigest)
	request.Confirmation = strings.TrimSpace(request.Confirmation)
	if ownerIdentity == "" || actor == "" || ownerIdentity != actor {
		return nil, fmt.Errorf("the authenticated owner must dispatch approved portfolio workflows")
	}
	if proposalID == uuid.Nil || !validPortfolioRecordDigest(request.ExpectedProposalDigest) {
		return nil, fmt.Errorf("an exact portfolio execution proposal is required")
	}
	if request.Confirmation != PortfolioDispatchConfirmation {
		return nil, fmt.Errorf("exact portfolio dispatch confirmation is required")
	}
	selected, err := normalizePortfolioDispatchItems(request.Items)
	if err != nil {
		return nil, err
	}
	dispatchRepository, _, evidence, err := s.loadPortfolioDispatchEvidence(ownerIdentity, proposalID)
	if err != nil {
		return nil, err
	}
	proposal := &evidence.Proposal
	proposalItems := evidence.Items
	if proposal.RecordDigest != request.ExpectedProposalDigest {
		return nil, fmt.Errorf("portfolio execution proposal changed; inspect the current immutable proposal")
	}
	itemsByID := make(map[uuid.UUID]models.PursuitPortfolioExecutionProposalItem, len(proposalItems))
	for _, item := range proposalItems {
		itemsByID[item.ID] = item
	}
	allocationItemsByID := make(map[uuid.UUID]models.PursuitPortfolioAllocationItem, len(evidence.AllocationItems))
	for _, item := range evidence.AllocationItems {
		allocationItemsByID[item.ID] = item
	}
	selectedIDs := make([]uuid.UUID, 0, len(selected))
	for _, item := range selected {
		proposalItem, ok := itemsByID[item.ID]
		if !ok || proposalItem.RecordDigest != item.ItemDigest {
			return nil, fmt.Errorf("selected portfolio proposal item changed or is unavailable")
		}
		selectedIDs = append(selectedIDs, item.ID)
	}
	selectedDigest, err := digestPortfolioDispatchSelection(selected)
	if err != nil {
		return nil, err
	}
	requestDigest, err := digestPortfolioPayload(struct {
		Owner, Actor, ProposalID, ProposalDigest, AllocationID, AllocationDigest string
		SelectionDigest, Confirmation                                            string
		CoordinationPlanID, CoordinationPlanDigest, CoordinationPlanNodeID       string
		CoordinationPlanRevision                                                 uint64
	}{
		Owner: ownerIdentity, Actor: actor, ProposalID: proposalID.String(),
		ProposalDigest: proposal.RecordDigest, AllocationID: evidence.Allocation.ID.String(),
		AllocationDigest: evidence.Allocation.RecordDigest, SelectionDigest: selectedDigest,
		Confirmation:             request.Confirmation,
		CoordinationPlanID:       optionalUUIDString(evidence.Allocation.CoordinationPlanID),
		CoordinationPlanRevision: evidence.Allocation.CoordinationPlanRevision,
		CoordinationPlanDigest:   portfolioCoordinationDigest(evidence.Allocation.CoordinationPlanDigest),
		CoordinationPlanNodeID:   evidence.Allocation.CoordinationPlanNodeID,
	})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	run := &models.PursuitPortfolioDispatchRun{
		ID: uuid.New(), ProposalID: proposalID, OwnerIdentity: ownerIdentity,
		ProposalDigest: proposal.RecordDigest, SelectedItemIDs: selectedIDs,
		SelectedItemsDigest: selectedDigest, RequestDigest: requestDigest,
		Actor: actor, Confirmation: request.Confirmation, RequestedAt: now,
	}
	run.RecordDigest, err = digestPortfolioDispatchRun(run)
	if err != nil {
		return nil, err
	}
	storedRun, created, err := dispatchRepository.FindOrCreatePortfolioDispatchRun(run)
	if err != nil {
		return nil, fmt.Errorf("persist immutable portfolio dispatch request: %w", err)
	}
	existing, err := dispatchRepository.ListPortfolioDispatchItemResults(ownerIdentity, storedRun.ID)
	if err != nil {
		return nil, fmt.Errorf("load portfolio dispatch resume state: %w", err)
	}
	latest := latestPortfolioDispatchResults(existing)
	results := make([]models.PursuitPortfolioDispatchItemResult, 0, len(selected))
	for _, selectedItem := range selected {
		if prior, ok := latest[selectedItem.ID]; ok && portfolioDispatchOutcomeTerminal(prior.Outcome) {
			results = append(results, prior)
			continue
		}
		proposalItem := itemsByID[selectedItem.ID]
		allocationItem, ok := allocationItemsByID[proposalItem.AllocationItemID]
		if !ok {
			return nil, fmt.Errorf("selected portfolio proposal item lost its immutable allocation evidence")
		}
		if _, revalidationErr := s.revalidatePortfolioAllocationCoordinationPlan(
			ownerIdentity, &evidence.Allocation, []models.PursuitPortfolioAllocationItem{allocationItem},
		); revalidationErr != nil {
			stale := newPortfolioDispatchItemResult(
				*storedRun, selectedItem, PortfolioDispatchOutcomeStale,
				"Accepted coordination plan requires review before dispatch: "+revalidationErr.Error(),
			)
			stored, _, appendErr := dispatchRepository.AppendPortfolioDispatchItemResult(&stale)
			if appendErr != nil {
				return nil, fmt.Errorf("persist stale portfolio dispatch result: %w", appendErr)
			}
			results = append(results, *stored)
			continue
		}
		if ctx.Err() != nil {
			cancelled := newPortfolioDispatchItemResult(*storedRun, selectedItem, PortfolioDispatchOutcomeCancelled, "Dispatch stopped before this item began because the request context ended.")
			stored, _, appendErr := dispatchRepository.AppendPortfolioDispatchItemResult(&cancelled)
			if appendErr != nil {
				return nil, fmt.Errorf("persist interrupted portfolio dispatch result: %w", appendErr)
			}
			results = append(results, *stored)
			continue
		}
		outcome := s.dispatchOnePortfolioWorkflow(ctx, ownerIdentity, actor, *storedRun, selectedItem, proposalItem)
		stored, _, appendErr := dispatchRepository.AppendPortfolioDispatchItemResult(&outcome)
		if appendErr != nil {
			return nil, fmt.Errorf("persist portfolio dispatch item result; retry the exact request to reconcile durable receipts: %w", appendErr)
		}
		results = append(results, *stored)
	}
	return summarizePortfolioDispatch(*storedRun, results, !created), nil
}

func (s *service) dispatchOnePortfolioWorkflow(
	ctx context.Context,
	ownerIdentity, actor string,
	run models.PursuitPortfolioDispatchRun,
	selected normalizedPortfolioDispatchItem,
	item models.PursuitPortfolioExecutionProposalItem,
) models.PursuitPortfolioDispatchItemResult {
	result := newPortfolioDispatchItemResult(run, selected, PortfolioDispatchOutcomeFailed, "Portfolio workflow dispatch failed before authorization.")
	if item.Status == PortfolioExecutionProposalItemBlocked || len(item.BlockedReasons) > 0 {
		result.Outcome = PortfolioDispatchOutcomeBlocked
		result.Message = firstNonEmpty(strings.Join(item.BlockedReasons, "; "), "The immutable proposal item is blocked.")
		return result
	}
	authorized, err := s.AuthorizePortfolioWorkflowEffectForOwner(ctx, ownerIdentity, actor, item.ID, PortfolioWorkflowEffectAuthorizationRequest{
		ExpectedItemDigest: item.RecordDigest, ExpectedDecisionDigest: selected.DecisionDigest,
		Confirmation: PortfolioWorkflowEffectConfirmation,
	})
	if err != nil {
		result.Outcome, result.Message = classifyPortfolioDispatchError(err)
		return result
	}
	if authorized.Receipt.Outcome != executionauth.OutcomeAuthorized {
		result.Outcome = PortfolioDispatchOutcomeFailed
		result.Message = firstNonEmpty(strings.TrimSpace(authorized.Receipt.Reason), "Execution policy denied this portfolio workflow effect.")
		return result
	}
	decisionID, parseErr := portfolioWorkflowDecisionIDFromApprovalSource(authorized.Receipt.ApprovalSourceID)
	if parseErr != nil {
		result.Message = parseErr.Error()
		return result
	}
	result.ApprovalDecisionID = &decisionID
	result.ApprovalDecisionDigest = selected.DecisionDigest
	receiptID := authorized.Receipt.ID
	result.AuthorizationReceiptID = &receiptID
	executed, err := s.ExecutePortfolioWorkflowEffectForOwner(ctx, ownerIdentity, actor, item.ID, PortfolioWorkflowEffectExecutionRequest{
		AuthorizationReceiptID: receiptID.String(), ExpectedItemDigest: item.RecordDigest,
		ExpectedDecisionDigest: selected.DecisionDigest,
		Confirmation:           PortfolioWorkflowEffectExecutionConfirmation,
	})
	if err != nil {
		result.Outcome, result.Message = classifyPortfolioDispatchError(err)
		return result
	}
	workflowID := executed.WorkflowID
	result.WorkflowID = &workflowID
	result.WorkflowState = executed.WorkflowState
	result.Replayed = executed.Replayed
	if executed.Replayed {
		result.Outcome = PortfolioDispatchOutcomeReplayed
		result.Message = "Recovered the existing receipt-bound review-gated workflow."
	} else {
		result.Outcome = PortfolioDispatchOutcomeWorkflowCreated
		result.Message = "Created one receipt-bound review-gated local workflow; downstream execution is still not authorized."
	}
	return result
}

func (s *service) loadPortfolioDispatchEvidence(
	ownerIdentity string,
	proposalID uuid.UUID,
) (pursuitPortfolioDispatchRepository, PortfolioWorkflowEffectApprovalRepository, *portfolioDispatchProposalEvidence, error) {
	dispatchRepository, ok := s.repo.(pursuitPortfolioDispatchRepository)
	if !ok {
		return nil, nil, nil, fmt.Errorf("durable portfolio dispatch storage is unavailable")
	}
	approvalRepository, ok := s.repo.(PortfolioWorkflowEffectApprovalRepository)
	if !ok {
		return nil, nil, nil, fmt.Errorf("durable portfolio workflow approval storage is unavailable")
	}
	evidence, err := dispatchRepository.LoadPortfolioDispatchProposal(ownerIdentity, proposalID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load portfolio execution proposal for dispatch: %w", err)
	}
	if evidence == nil {
		return nil, nil, nil, fmt.Errorf("portfolio execution proposal is unavailable to this owner")
	}
	if evidence.Proposal.AllocationID != evidence.Allocation.ID ||
		evidence.Proposal.AllocationRecordDigest != evidence.Allocation.RecordDigest {
		return nil, nil, nil, fmt.Errorf("portfolio execution proposal does not match its immutable parent allocation")
	}
	if err := validatePortfolioAllocationHistoryEvidence(ownerIdentity, &evidence.Allocation, evidence.AllocationItems); err != nil {
		return nil, nil, nil, err
	}
	if err := validatePortfolioExecutionProposalEvidence(ownerIdentity, &evidence.Proposal, evidence.Items); err != nil {
		return nil, nil, nil, err
	}
	allocationItemsByID := make(map[uuid.UUID]models.PursuitPortfolioAllocationItem, len(evidence.AllocationItems))
	for _, item := range evidence.AllocationItems {
		allocationItemsByID[item.ID] = item
	}
	for _, item := range evidence.Items {
		allocationItem, ok := allocationItemsByID[item.AllocationItemID]
		if !ok || allocationItem.PursuitID != item.PursuitID ||
			allocationItem.ReservationID != item.ReservationID || allocationItem.RecordDigest != item.AllocationItemDigest {
			return nil, nil, nil, fmt.Errorf("portfolio execution proposal lost its immutable allocation item evidence")
		}
	}
	return dispatchRepository, approvalRepository, evidence, nil
}

func normalizePortfolioDispatchItems(items []PortfolioDispatchItemRequest) ([]normalizedPortfolioDispatchItem, error) {
	if len(items) == 0 || len(items) > PortfolioDispatchMaxItems {
		return nil, fmt.Errorf("portfolio dispatch must select between 1 and %d items", PortfolioDispatchMaxItems)
	}
	result := make([]normalizedPortfolioDispatchItem, 0, len(items))
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		id, err := uuid.Parse(strings.TrimSpace(item.ProposalItemID))
		itemDigest := strings.TrimSpace(item.ExpectedItemDigest)
		decisionDigest := strings.TrimSpace(item.ExpectedDecisionDigest)
		if err != nil || id == uuid.Nil || !validPortfolioRecordDigest(itemDigest) || !validPortfolioRecordDigest(decisionDigest) {
			return nil, fmt.Errorf("every selected portfolio item requires exact item and approval decision evidence")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("portfolio dispatch cannot contain duplicate proposal items")
		}
		seen[id] = struct{}{}
		result = append(result, normalizedPortfolioDispatchItem{ID: id, ItemDigest: itemDigest, DecisionDigest: decisionDigest})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result, nil
}

func digestPortfolioDispatchSelection(items []normalizedPortfolioDispatchItem) (string, error) {
	payload := make([]struct{ ID, ItemDigest, DecisionDigest string }, 0, len(items))
	for _, item := range items {
		payload = append(payload, struct{ ID, ItemDigest, DecisionDigest string }{item.ID.String(), item.ItemDigest, item.DecisionDigest})
	}
	return digestPortfolioPayload(payload)
}

func digestPortfolioDispatchRun(value *models.PursuitPortfolioDispatchRun) (string, error) {
	if value == nil {
		return "", fmt.Errorf("portfolio dispatch run is required")
	}
	ids := make([]string, 0, len(value.SelectedItemIDs))
	for _, id := range value.SelectedItemIDs {
		ids = append(ids, id.String())
	}
	return digestPortfolioPayload(struct {
		ID, ProposalID, Owner, ProposalDigest, SelectionDigest, RequestDigest, Actor, Confirmation string
		SelectedItemIDs                                                                            []string
		RequestedAt                                                                                time.Time
	}{value.ID.String(), value.ProposalID.String(), value.OwnerIdentity, value.ProposalDigest, value.SelectedItemsDigest, value.RequestDigest, value.Actor, value.Confirmation, ids, value.RequestedAt.UTC()})
}

func digestPortfolioDispatchItemResult(value *models.PursuitPortfolioDispatchItemResult) (string, error) {
	if value == nil {
		return "", fmt.Errorf("portfolio dispatch item result is required")
	}
	return digestPortfolioPayload(struct {
		ID, RunID, ProposalID, ItemID, Owner, ItemDigest, DecisionID, DecisionDigest, Outcome, Message, ReceiptID, WorkflowID, WorkflowState string
		Attempt                                                                                                                              int
		Replayed                                                                                                                             bool
		AttemptedAt                                                                                                                          time.Time
	}{value.ID.String(), value.DispatchRunID.String(), value.ProposalID.String(), value.ProposalItemID.String(), value.OwnerIdentity,
		value.ProposalItemDigest, optionalUUIDString(value.ApprovalDecisionID), value.ApprovalDecisionDigest, value.Outcome, value.Message,
		optionalUUIDString(value.AuthorizationReceiptID), optionalUUIDString(value.WorkflowID), value.WorkflowState,
		value.AttemptNumber, value.Replayed, value.AttemptedAt.UTC()})
}

func newPortfolioDispatchItemResult(
	run models.PursuitPortfolioDispatchRun,
	selected normalizedPortfolioDispatchItem,
	outcome, message string,
) models.PursuitPortfolioDispatchItemResult {
	return models.PursuitPortfolioDispatchItemResult{
		ID: uuid.New(), DispatchRunID: run.ID, ProposalID: run.ProposalID,
		ProposalItemID: selected.ID, OwnerIdentity: run.OwnerIdentity,
		ProposalItemDigest: selected.ItemDigest, Outcome: outcome,
		Message: strings.TrimSpace(message), AttemptedAt: time.Now().UTC(),
	}
}

func latestPortfolioDispatchResults(records []models.PursuitPortfolioDispatchItemResult) map[uuid.UUID]models.PursuitPortfolioDispatchItemResult {
	result := make(map[uuid.UUID]models.PursuitPortfolioDispatchItemResult)
	for _, record := range records {
		current, found := result[record.ProposalItemID]
		if !found || record.AttemptNumber > current.AttemptNumber ||
			(record.AttemptNumber == current.AttemptNumber && record.AttemptedAt.After(current.AttemptedAt)) {
			result[record.ProposalItemID] = record
		}
	}
	return result
}

func portfolioDispatchOutcomeTerminal(outcome string) bool {
	return outcome == PortfolioDispatchOutcomeWorkflowCreated || outcome == PortfolioDispatchOutcomeReplayed
}

func classifyPortfolioDispatchError(err error) (string, string) {
	if err == nil {
		return PortfolioDispatchOutcomeFailed, "Portfolio workflow dispatch failed without a recorded cause."
	}
	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case errors.Is(err, ErrPortfolioWorkflowApprovalUnavailable):
		return PortfolioDispatchOutcomeNeedsApproval, "A current explicit owner approval is required before this item can be dispatched."
	case errors.Is(err, ErrPortfolioWorkflowApprovalStale), errors.Is(err, ErrPortfolioWorkflowApprovalInvalid),
		errors.Is(err, ErrPortfolioWorkflowBindingMismatch), errors.Is(err, executionauth.ErrAuthorizationChanged),
		errors.Is(err, executionauth.ErrFinalEffectMismatch), strings.Contains(lower, "changed"):
		return PortfolioDispatchOutcomeStale, message
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return PortfolioDispatchOutcomeCancelled, message
	default:
		return PortfolioDispatchOutcomeFailed, message
	}
}

func portfolioDispatchEligibilityFromSnapshot(
	ctx context.Context,
	ownerIdentity string,
	item models.PursuitPortfolioExecutionProposalItem,
	snapshot *PortfolioWorkflowEffectApprovalSnapshot,
	now time.Time,
) (string, string, *models.PursuitPortfolioExecutionProposalDecision, error) {
	if ctx == nil {
		return "", "", nil, fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if item.Status == PortfolioExecutionProposalItemBlocked || len(item.BlockedReasons) > 0 {
		return PortfolioDispatchOutcomeBlocked, firstNonEmpty(strings.Join(item.BlockedReasons, "; "), "The immutable proposal item is blocked."), nil, nil
	}
	if snapshot == nil {
		return "", "", nil, fmt.Errorf("portfolio coordination approval evidence is incomplete")
	}
	if snapshot.Item.ID != item.ID || snapshot.Proposal.ID != item.ProposalID ||
		snapshot.Item.OwnerIdentity != ownerIdentity || snapshot.Proposal.OwnerIdentity != ownerIdentity {
		return "", "", nil, fmt.Errorf("portfolio coordination approval evidence crossed its owner or item boundary")
	}
	if snapshot.LatestDecision == nil {
		return PortfolioDispatchOutcomeNeedsApproval, "No current explicit owner approval is available.", nil, nil
	}
	decision := *snapshot.LatestDecision
	if snapshot.Item.RecordDigest != item.RecordDigest {
		return PortfolioDispatchOutcomeStale, "The proposal item changed after this coordination view was loaded.", &decision, nil
	}
	if err := validatePortfolioWorkflowEffectApproval(ownerIdentity, snapshot, now); err != nil {
		outcome, reason := classifyPortfolioDispatchError(err)
		return outcome, reason, &decision, nil
	}
	return "eligible", "Current owner approval and immutable item evidence are valid for workflow creation.", &decision, nil
}

func summarizePortfolioDispatch(
	run models.PursuitPortfolioDispatchRun,
	items []models.PursuitPortfolioDispatchItemResult,
	resumed bool,
) *PortfolioDispatchResult {
	result := &PortfolioDispatchResult{
		Run: run, Items: items, Resumed: resumed,
		Authority: PortfolioDispatchAuthority, CanExecute: false,
	}
	for _, item := range items {
		switch item.Outcome {
		case PortfolioDispatchOutcomeWorkflowCreated:
			result.Created++
		case PortfolioDispatchOutcomeReplayed:
			result.Replayed++
		case PortfolioDispatchOutcomeNeedsApproval, PortfolioDispatchOutcomeBlocked, PortfolioDispatchOutcomeStale:
			result.NeedsReview++
		default:
			result.Failed++
		}
	}
	switch {
	case result.Failed > 0:
		result.Status = "partial_failure"
	case result.NeedsReview > 0:
		result.Status = "needs_review"
	default:
		result.Status = "workflows_created"
	}
	return result
}

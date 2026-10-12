package pursuit

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/safety"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	PortfolioAllocationConfirmation = "ACCEPT PORTFOLIO ALLOCATION"
	portfolioAllocationFreshness    = 15 * time.Minute
)

var portfolioDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PortfolioAllocationAcceptanceRequest struct {
	PlanningRequest        PortfolioPlanningRequest `json:"planningRequest"`
	ExpectedDecisionDigest string                   `json:"expectedDecisionDigest"`
	Confirmation           string                   `json:"confirmation"`
}

type PortfolioAllocationAcceptanceResult struct {
	Allocation *models.PursuitPortfolioAllocation      `json:"allocation"`
	Items      []models.PursuitPortfolioAllocationItem `json:"items"`
	Replayed   bool                                    `json:"replayed"`
	Authority  string                                  `json:"authority"`
	CanExecute bool                                    `json:"canExecute"`
}

type pursuitPortfolioAllocationRepository interface {
	FindPortfolioAllocationForOwner(ownerIdentity, planID string) (*models.PursuitPortfolioAllocation, []models.PursuitPortfolioAllocationItem, error)
	SavePortfolioAllocation(
		allocation *models.PursuitPortfolioAllocation,
		items []models.PursuitPortfolioAllocationItem,
		reservations []models.PursuitResourceReservation,
		activities []models.PursuitActivity,
	) (*models.PursuitPortfolioAllocation, []models.PursuitPortfolioAllocationItem, bool, error)
}

type pursuitPortfolioAllocationHistoryRepository interface {
	ListPortfolioAllocationsForOwner(ownerIdentity string, limit int) ([]models.PursuitPortfolioAllocation, []models.PursuitPortfolioAllocationItem, error)
}

// PortfolioAllocationHistoryForOwner returns immutable accepted allocations
// for inspection. History is evidence only and never grants execution authority.
func (s *service) PortfolioAllocationHistoryForOwner(ownerIdentity string, limit int) ([]PortfolioAllocationAcceptanceResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("an authenticated owner identity is required for portfolio allocation history")
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("portfolio allocation history limit must be between 1 and 100")
	}
	repository, ok := s.repo.(pursuitPortfolioAllocationHistoryRepository)
	if !ok {
		return nil, fmt.Errorf("portfolio allocation history storage is unavailable")
	}
	allocations, items, err := repository.ListPortfolioAllocationsForOwner(ownerIdentity, limit)
	if err != nil {
		return nil, fmt.Errorf("list portfolio allocation history: %w", err)
	}
	itemsByAllocation := make(map[uuid.UUID][]models.PursuitPortfolioAllocationItem, len(allocations))
	for _, item := range items {
		if item.OwnerIdentity != ownerIdentity {
			return nil, fmt.Errorf("portfolio allocation history crossed its owner boundary")
		}
		itemsByAllocation[item.AllocationID] = append(itemsByAllocation[item.AllocationID], item)
	}
	result := make([]PortfolioAllocationAcceptanceResult, 0, len(allocations))
	for index := range allocations {
		allocation := allocations[index]
		allocationItems := itemsByAllocation[allocation.ID]
		if err := validatePortfolioAllocationHistoryEvidence(ownerIdentity, &allocation, allocationItems); err != nil {
			return nil, err
		}
		sortPortfolioAllocationItems(allocationItems)
		result = append(result, PortfolioAllocationAcceptanceResult{
			Allocation: &allocation,
			Items:      allocationItems,
			Authority:  "allocation_only",
			CanExecute: false,
		})
	}
	return result, nil
}

func validatePortfolioAllocationHistoryEvidence(
	ownerIdentity string,
	allocation *models.PursuitPortfolioAllocation,
	items []models.PursuitPortfolioAllocationItem,
) error {
	if allocation == nil || allocation.ID == uuid.Nil || allocation.OwnerIdentity != ownerIdentity ||
		allocation.Actor != ownerIdentity || !validPortfolioPlanID(allocation.PlanID) ||
		!validPortfolioRecordDigest(allocation.RequestDigest) ||
		!validPortfolioRecordDigest(allocation.DecisionDigest) ||
		!validPortfolioRecordDigest(allocation.RecordDigest) {
		return fmt.Errorf("portfolio allocation history contains invalid owner-scoped evidence")
	}
	if allocation.Status != PortfolioAllocationAccepted && allocation.Status != PortfolioAllocationAcceptedNeedsApproval ||
		allocation.DurationMode != "expected" && allocation.DurationMode != "conservative" ||
		allocation.HorizonStart.IsZero() || !allocation.HorizonEnd.After(allocation.HorizonStart) ||
		allocation.AcceptedAt.IsZero() || allocation.Confirmation != PortfolioAllocationConfirmation {
		return fmt.Errorf("portfolio allocation history contains invalid acceptance evidence")
	}
	if err := validatePortfolioCoordinationBindingShape(allocation); err != nil {
		return err
	}
	expectedAllocationDigest, err := digestPortfolioAllocation(allocation)
	if err != nil || expectedAllocationDigest != allocation.RecordDigest {
		return fmt.Errorf("portfolio allocation history parent digest verification failed")
	}
	if len(items) == 0 || len(items) > 500 {
		return fmt.Errorf("portfolio allocation history is missing scheduled item evidence")
	}
	seenPursuits := make(map[uuid.UUID]struct{}, len(items))
	requiresApproval := false
	for _, item := range items {
		if item.ID == uuid.Nil || item.AllocationID != allocation.ID || item.PursuitID == uuid.Nil ||
			item.OwnerIdentity != ownerIdentity || item.ReservationID == uuid.Nil || item.CreatedAt.IsZero() ||
			!validPortfolioRecordDigest(item.RecordDigest) {
			return fmt.Errorf("portfolio allocation history contains invalid item evidence")
		}
		if _, duplicate := seenPursuits[item.PursuitID]; duplicate {
			return fmt.Errorf("portfolio allocation history contains duplicate pursuit evidence")
		}
		seenPursuits[item.PursuitID] = struct{}{}
		if item.ScheduledStart.Before(allocation.HorizonStart) || item.ScheduledEnd.After(allocation.HorizonEnd) ||
			item.DurationMinutes <= 0 || item.ScheduledEnd.Sub(item.ScheduledStart) != time.Duration(item.DurationMinutes)*time.Minute ||
			item.EstimatedCostMicros < 0 || len(item.ApprovalReasons) > 20 {
			return fmt.Errorf("portfolio allocation history contains invalid schedule evidence")
		}
		if item.RequiresApproval {
			requiresApproval = true
			if len(item.ApprovalReasons) == 0 {
				return fmt.Errorf("portfolio allocation history is missing an approval reason")
			}
		} else if len(item.ApprovalReasons) != 0 {
			return fmt.Errorf("portfolio allocation history contains inconsistent approval evidence")
		}
		for _, reason := range item.ApprovalReasons {
			if strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason || len(reason) > 1000 {
				return fmt.Errorf("portfolio allocation history contains invalid approval evidence")
			}
		}
		expectedItemDigest, digestErr := digestPortfolioAllocationItem(allocation.PlanID, item)
		if digestErr != nil || expectedItemDigest != item.RecordDigest {
			return fmt.Errorf("portfolio allocation history item digest verification failed")
		}
	}
	if requiresApproval && allocation.Status != PortfolioAllocationAcceptedNeedsApproval ||
		!requiresApproval && allocation.Status != PortfolioAllocationAccepted {
		return fmt.Errorf("portfolio allocation history status does not match its approval evidence")
	}
	return nil
}

// AcceptPortfolioAllocationForOwner turns one fresh, deterministic advisory
// decision into durable capacity holds. Acceptance does not approve the work,
// enqueue a task, mutate pursuit priority/state, or grant execution authority.
func (s *service) AcceptPortfolioAllocationForOwner(
	ownerIdentity, actor string,
	request PortfolioAllocationAcceptanceRequest,
) (*PortfolioAllocationAcceptanceResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actor = strings.TrimSpace(actor)
	request.ExpectedDecisionDigest = strings.TrimSpace(request.ExpectedDecisionDigest)
	request.Confirmation = strings.TrimSpace(request.Confirmation)
	if ownerIdentity == "" || actor == "" || actor != ownerIdentity {
		return nil, fmt.Errorf("the authenticated owner must accept the portfolio allocation")
	}
	if request.Confirmation != PortfolioAllocationConfirmation {
		return nil, fmt.Errorf("exact portfolio allocation confirmation is required")
	}
	if !portfolioDigestPattern.MatchString(request.ExpectedDecisionDigest) {
		return nil, fmt.Errorf("a valid expected decision digest is required")
	}
	request.PlanningRequest = normalizePortfolioPlanningRequest(request.PlanningRequest)
	requestDigest, err := digestPortfolioPayload(request.PlanningRequest)
	if err != nil {
		return nil, err
	}
	repository, ok := s.repo.(pursuitPortfolioAllocationRepository)
	if !ok {
		return nil, fmt.Errorf("durable portfolio allocation storage is unavailable")
	}
	existing, existingItems, err := repository.FindPortfolioAllocationForOwner(ownerIdentity, request.PlanningRequest.PlanID)
	if err != nil {
		return nil, fmt.Errorf("read portfolio allocation replay state: %w", err)
	}
	if existing != nil {
		if existing.OwnerIdentity != ownerIdentity || existing.RequestDigest != requestDigest || existing.DecisionDigest != request.ExpectedDecisionDigest {
			return nil, fmt.Errorf("portfolio plan id was already accepted with a different request or decision")
		}
		return &PortfolioAllocationAcceptanceResult{
			Allocation: existing, Items: existingItems, Replayed: true,
			Authority: "allocation_only", CanExecute: false,
		}, nil
	}
	now := time.Now().UTC().Truncate(time.Second)
	asOf := request.PlanningRequest.AsOf.UTC()
	if asOf.IsZero() || asOf.Before(now.Add(-portfolioAllocationFreshness)) || asOf.After(now.Add(time.Minute)) {
		return nil, fmt.Errorf("the portfolio proposal is stale; calculate a fresh plan before accepting it")
	}

	planned, err := s.PlanPortfolioForOwner(ownerIdentity, request.PlanningRequest)
	if err != nil {
		return nil, fmt.Errorf("revalidate portfolio proposal: %w", err)
	}
	if planned.Decision == nil || len(planned.Decision.Scheduled) == 0 {
		return nil, fmt.Errorf("the portfolio proposal contains no schedulable allocation")
	}
	if planned.Decision.DecisionDigest != request.ExpectedDecisionDigest {
		return nil, fmt.Errorf("portfolio proposal changed during acceptance; review the recalculated plan")
	}
	if planned.Decision.CanExecute || planned.Decision.GrantsAuthority || planned.Decision.Authority != "advisory_only" {
		return nil, fmt.Errorf("portfolio proposal violated its advisory authority boundary")
	}
	if planned.Decision.Feasibility != resourceplanner.Feasible && planned.Decision.Feasibility != resourceplanner.FeasibleWithApprovals {
		return nil, fmt.Errorf("only a feasible portfolio proposal can be accepted")
	}
	if len(planned.Decision.CriticalBlockers) > 0 {
		return nil, fmt.Errorf("portfolio proposal has critical blockers and cannot be accepted")
	}
	allocationID := uuid.New()
	status := "accepted"
	if hasMandatoryPortfolioApproval(planned.Decision.ApprovalFlags) {
		status = "accepted_needs_approval"
	}
	allocation := &models.PursuitPortfolioAllocation{
		ID: allocationID, OwnerIdentity: ownerIdentity,
		PlanID: request.PlanningRequest.PlanID, RequestDigest: requestDigest,
		DecisionDigest: planned.Decision.DecisionDigest, Status: status,
		DurationMode: string(request.PlanningRequest.DurationMode),
		HorizonStart: request.PlanningRequest.HorizonStart.UTC(), HorizonEnd: request.PlanningRequest.HorizonEnd.UTC(),
		Actor: actor, Confirmation: PortfolioAllocationConfirmation, AcceptedAt: now,
	}
	applyPortfolioCoordinationBinding(allocation, planned.CoordinationPlan)
	allocation.RecordDigest, err = digestPortfolioAllocation(allocation)
	if err != nil {
		return nil, err
	}

	inputs := make(map[string]PortfolioPursuitPlanningInput, len(request.PlanningRequest.Pursuits))
	for _, input := range request.PlanningRequest.Pursuits {
		inputs[input.PursuitID.String()] = input
	}
	approvalReasons := portfolioApprovalReasonsByTask(planned.Decision.ApprovalFlags)
	globalApprovalReasons := portfolioGlobalApprovalReasons(planned.Decision.ApprovalFlags)
	items := make([]models.PursuitPortfolioAllocationItem, 0, len(planned.Decision.Scheduled))
	reservations := make([]models.PursuitResourceReservation, 0, len(planned.Decision.Scheduled))
	activities := make([]models.PursuitActivity, 0, len(planned.Decision.Scheduled))
	for _, scheduled := range planned.Decision.Scheduled {
		pursuitID, parseErr := uuid.Parse(scheduled.TaskID)
		input, exists := inputs[scheduled.TaskID]
		if parseErr != nil || pursuitID == uuid.Nil || !exists {
			return nil, fmt.Errorf("scheduled portfolio item is not bound to an explicit pursuit estimate")
		}
		reservationID := uuid.New()
		reasons := append([]string(nil), globalApprovalReasons...)
		reasons = append(reasons, approvalReasons[scheduled.TaskID]...)
		reasons = uniqueSortedPortfolioReasons(reasons)
		item := models.PursuitPortfolioAllocationItem{
			ID: uuid.New(), AllocationID: allocationID, PursuitID: pursuitID, OwnerIdentity: ownerIdentity,
			ScheduledStart: scheduled.Start.UTC(), ScheduledEnd: scheduled.End.UTC(),
			DurationMinutes: scheduled.PlannedDurationMinutes, EstimatedCostMicros: input.EstimatedUsage.CostMicros,
			RequiresApproval: len(reasons) > 0, ApprovalReasons: reasons,
			ReservationID: reservationID, CreatedAt: now,
		}
		item.RecordDigest, err = digestPortfolioAllocationItem(request.PlanningRequest.PlanID, item)
		if err != nil {
			return nil, err
		}
		operationID := portfolioReservationOperationID(planned.Decision.DecisionDigest, pursuitID)
		reservation := models.PursuitResourceReservation{
			ID: reservationID, PursuitID: pursuitID, OwnerIdentity: ownerIdentity, OperationID: operationID,
			EstimatedEffortMinutes: scheduled.PlannedDurationMinutes,
			EstimatedCostMicros:    input.EstimatedUsage.CostMicros,
			Reason:                 "accepted portfolio allocation capacity hold", Actor: actor, ReservedAt: now,
		}
		if reservation.EstimatedCostMicros > 0 {
			reservation.Currency = "EUR"
		}
		reservation.RecordDigest, err = reservationDigest(&reservation)
		if err != nil {
			return nil, err
		}
		activity := newPursuitResourceActivity(
			pursuitID, "pursuit.portfolio_allocation_accepted",
			fmt.Sprintf("Accepted portfolio allocation %s for %d minutes; execution remains separately governed.", request.PlanningRequest.PlanID, scheduled.PlannedDurationMinutes),
			actor, "pursuit_portfolio_allocation", allocationID.String(),
			"hai://pursuits/"+pursuitID.String()+"/portfolio-allocations/"+allocationID.String(), now,
		)
		items = append(items, item)
		reservations = append(reservations, reservation)
		activities = append(activities, activity)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PursuitID.String() < items[j].PursuitID.String() })
	sort.Slice(reservations, func(i, j int) bool { return reservations[i].PursuitID.String() < reservations[j].PursuitID.String() })
	sort.Slice(activities, func(i, j int) bool { return activities[i].PursuitID.String() < activities[j].PursuitID.String() })

	stored, storedItems, created, err := repository.SavePortfolioAllocation(allocation, items, reservations, activities)
	if err != nil {
		return nil, fmt.Errorf("accept portfolio allocation: %w", err)
	}
	return &PortfolioAllocationAcceptanceResult{
		Allocation: stored, Items: storedItems, Replayed: !created,
		Authority: "allocation_only", CanExecute: false,
	}, nil
}

func normalizePortfolioPlanningRequest(request PortfolioPlanningRequest) PortfolioPlanningRequest {
	request.PlanID = strings.TrimSpace(request.PlanID)
	request.AsOf = request.AsOf.UTC().Truncate(time.Minute)
	request.HorizonStart = request.HorizonStart.UTC().Truncate(time.Minute)
	request.HorizonEnd = request.HorizonEnd.UTC().Truncate(time.Minute)
	if request.DurationMode == "" {
		request.DurationMode = resourceplanner.ExpectedDuration
	}
	request.Availability = append([]PortfolioCapacityWindow(nil), request.Availability...)
	for index := range request.Availability {
		request.Availability[index].Start = request.Availability[index].Start.UTC().Truncate(time.Minute)
		request.Availability[index].End = request.Availability[index].End.UTC().Truncate(time.Minute)
	}
	request.Pursuits = append([]PortfolioPursuitPlanningInput(nil), request.Pursuits...)
	request.CoordinationPlan.Digest = strings.ToLower(strings.TrimSpace(request.CoordinationPlan.Digest))
	request.CoordinationPlan.NodeID = strings.TrimSpace(request.CoordinationPlan.NodeID)
	return request
}

func hasMandatoryPortfolioApproval(flags []resourceplanner.ApprovalFlag) bool {
	for _, flag := range flags {
		if flag.Mandatory {
			return true
		}
	}
	return false
}

func portfolioApprovalReasonsByTask(flags []resourceplanner.ApprovalFlag) map[string][]string {
	result := map[string][]string{}
	for _, flag := range flags {
		if !flag.Mandatory || strings.TrimSpace(flag.TaskID) == "" {
			continue
		}
		reason := strings.Join(strings.Fields(flag.Reason), " ")
		if reason == "" {
			reason = strings.TrimSpace(flag.Code)
		}
		result[flag.TaskID] = append(result[flag.TaskID], reason)
	}
	for key := range result {
		sort.Strings(result[key])
	}
	return result
}

func portfolioGlobalApprovalReasons(flags []resourceplanner.ApprovalFlag) []string {
	reasons := []string{}
	for _, flag := range flags {
		if !flag.Mandatory || strings.TrimSpace(flag.TaskID) != "" {
			continue
		}
		reason := strings.Join(strings.Fields(flag.Reason), " ")
		if reason == "" {
			reason = strings.TrimSpace(flag.Code)
		}
		reasons = append(reasons, reason)
	}
	return uniqueSortedPortfolioReasons(reasons)
}

func uniqueSortedPortfolioReasons(reasons []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.Join(strings.Fields(reason), " ")
		if reason == "" {
			continue
		}
		if _, exists := seen[reason]; exists {
			continue
		}
		seen[reason] = struct{}{}
		result = append(result, reason)
	}
	sort.Strings(result)
	return result
}

func portfolioReservationOperationID(decisionDigest string, pursuitID uuid.UUID) string {
	return "portfolio:" + decisionDigest[:24] + ":" + pursuitID.String()
}

func digestPortfolioAllocation(value *models.PursuitPortfolioAllocation) (string, error) {
	payload := struct {
		OwnerIdentity, PlanID, RequestDigest, DecisionDigest, Status, DurationMode, Actor, Confirmation string
		CoordinationPlanID, CoordinationPlanDigest, CoordinationPlanNodeID                              string
		CoordinationPlanRevision                                                                        uint64
		HorizonStart, HorizonEnd                                                                        time.Time
	}{
		OwnerIdentity: value.OwnerIdentity, PlanID: value.PlanID, RequestDigest: value.RequestDigest,
		DecisionDigest: value.DecisionDigest, Status: value.Status, DurationMode: value.DurationMode,
		Actor: value.Actor, Confirmation: value.Confirmation,
		CoordinationPlanRevision: value.CoordinationPlanRevision,
		CoordinationPlanDigest:   portfolioCoordinationDigest(value.CoordinationPlanDigest),
		CoordinationPlanNodeID:   value.CoordinationPlanNodeID,
		HorizonStart:             value.HorizonStart.UTC(), HorizonEnd: value.HorizonEnd.UTC(),
	}
	if value.CoordinationPlanID != nil {
		payload.CoordinationPlanID = value.CoordinationPlanID.String()
	}
	return digestPortfolioPayload(payload)
}

func digestPortfolioAllocationItem(planID string, value models.PursuitPortfolioAllocationItem) (string, error) {
	payload := struct {
		PlanID, PursuitID, OwnerIdentity string
		ScheduledStart, ScheduledEnd     time.Time
		DurationMinutes, CostMicros      int64
		RequiresApproval                 bool
		ApprovalReasons                  []string
	}{planID, value.PursuitID.String(), value.OwnerIdentity, value.ScheduledStart.UTC(), value.ScheduledEnd.UTC(), value.DurationMinutes, value.EstimatedCostMicros, value.RequiresApproval, value.ApprovalReasons}
	return digestPortfolioPayload(payload)
}

func digestPortfolioPayload(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode portfolio allocation: %w", err)
	}
	validationPayload := encoded
	if request, ok := value.(PortfolioPlanningRequest); ok {
		// Numeric resource limits are not credentials. Rename only these typed
		// schema labels for scanning; the digest below still binds all originals.
		type planning PortfolioPlanningRequest
		type budget resourceplanner.Budget
		type policy resourceplanner.ApprovalPolicy
		type input PortfolioPursuitPlanningInput
		type calibration PortfolioEstimateCalibrationBinding
		var inputs []any
		if request.Pursuits != nil {
			inputs = make([]any, 0, len(request.Pursuits))
		}
		for _, entry := range request.Pursuits {
			var binding any
			if entry.Calibration != nil {
				binding = struct {
					calibration
					SourceEstimatedUsage any `json:"sourceEstimatedUsage"`
				}{calibration: calibration(*entry.Calibration), SourceEstimatedUsage: portfolioUsageSecretProjection(entry.Calibration.SourceEstimatedUsage)}
			}
			inputs = append(inputs, struct {
				input
				EstimatedUsage any `json:"estimatedUsage"`
				Calibration    any `json:"calibration,omitempty"`
			}{input: input(entry), EstimatedUsage: portfolioUsageSecretProjection(entry.EstimatedUsage), Calibration: binding})
		}
		validationPayload, err = json.Marshal(struct {
			planning
			Budget         any   `json:"budget"`
			ApprovalPolicy any   `json:"approvalPolicy"`
			Pursuits       []any `json:"pursuits"`
		}{planning: planning(request), Pursuits: inputs, Budget: struct {
			budget
			MaxInputTokens   *int64 `json:"maxInputTokens,omitempty"`
			MaxOutputTokens  *int64 `json:"maxOutputTokens,omitempty"`
			MaxInboundUnits  *int64 `json:"maxInboundUnits,omitempty"`
			MaxOutboundUnits *int64 `json:"maxOutboundUnits,omitempty"`
		}{budget: budget(request.Budget), MaxInboundUnits: request.Budget.MaxInputTokens,
			MaxOutboundUnits: request.Budget.MaxOutputTokens}, ApprovalPolicy: struct {
			policy
			InputTokenThreshold   *int64 `json:"inputTokenThreshold,omitempty"`
			OutputTokenThreshold  *int64 `json:"outputTokenThreshold,omitempty"`
			InboundUnitThreshold  *int64 `json:"inboundUnitThreshold,omitempty"`
			OutboundUnitThreshold *int64 `json:"outboundUnitThreshold,omitempty"`
		}{policy: policy(request.ApprovalPolicy), InboundUnitThreshold: request.ApprovalPolicy.InputTokenThreshold,
			OutboundUnitThreshold: request.ApprovalPolicy.OutputTokenThreshold}})
		if err != nil {
			return "", fmt.Errorf("encode portfolio validation: %w", err)
		}
	}
	if safety.RedactSecrets(string(validationPayload)) != string(validationPayload) {
		return "", fmt.Errorf("portfolio allocation must not contain secret material")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func portfolioUsageSecretProjection(value resourceplanner.Usage) any {
	type usage resourceplanner.Usage
	return struct {
		usage
		InputTokens   *int64 `json:"inputTokens,omitempty"`
		OutputTokens  *int64 `json:"outputTokens,omitempty"`
		InboundUnits  int64  `json:"inboundUnits"`
		OutboundUnits int64  `json:"outboundUnits"`
	}{usage: usage(value), InboundUnits: value.InputTokens, OutboundUnits: value.OutputTokens}
}

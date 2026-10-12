package pursuit

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ResourceReservationConsumed = "consumed"
	ResourceReservationReleased = "released"
)

// ReservePursuitTaskResources atomically holds conservative capacity for one
// execution attempt. The database trigger performs the authoritative
// recorded-plus-active reservation check under a pursuit-scoped lock.
func (s *service) ReservePursuitTaskResources(
	pursuitID uuid.UUID,
	ownerIdentity, operationID string,
	effortMinutes, costMicros int64,
) error {
	if err := s.ValidatePursuitTaskAttempt(pursuitID, ownerIdentity); err != nil {
		return err
	}
	pursuit, err := s.taskAttemptPursuit(pursuitID, ownerIdentity)
	if err != nil {
		return err
	}
	ownerIdentity = strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	if ownerIdentity == "" {
		return fmt.Errorf("an authenticated owner identity is required to reserve pursuit resources")
	}
	operationID = strings.TrimSpace(operationID)
	if !validReservationOperationID(operationID) {
		return fmt.Errorf("resource reservation operation id must contain 1 to 120 safe identifier characters")
	}
	if effortMinutes < 0 || costMicros < 0 || effortMinutes == 0 && costMicros == 0 {
		return fmt.Errorf("resource reservation requires a positive effort or cost estimate")
	}
	if effortMinutes > 60_000_000 || costMicros > 1_000_000_000_000_000 {
		return fmt.Errorf("resource reservation estimate is outside the supported range")
	}
	repository, ok := s.repo.(pursuitResourceReservationRepository)
	if !ok {
		return fmt.Errorf("pursuit resource reservation ledger is unavailable")
	}
	now := time.Now().UTC().Truncate(time.Second)
	reservation := &models.PursuitResourceReservation{
		ID:        uuid.New(),
		PursuitID: pursuitID, OwnerIdentity: ownerIdentity, OperationID: operationID,
		EstimatedEffortMinutes: effortMinutes, EstimatedCostMicros: costMicros,
		Reason: "conservative task execution reservation", Actor: "hai:task-engine", ReservedAt: now,
	}
	if costMicros > 0 {
		reservation.Currency = "EUR"
	}
	reservation.RecordDigest, err = reservationDigest(reservation)
	if err != nil {
		return err
	}
	activity := newPursuitResourceActivity(
		pursuitID, "pursuit.resource_reserved",
		fmt.Sprintf("Reserved %d minutes and EUR %.6f for task execution.", reservation.EstimatedEffortMinutes, float64(reservation.EstimatedCostMicros)/1_000_000),
		reservation.Actor, "pursuit_resource_reservation", reservation.ID.String(), "task://"+reservation.OperationID, now,
	)
	_, _, err = repository.ReserveResource(reservation, &activity)
	if err != nil {
		return fmt.Errorf("reserve pursuit resources: %w", err)
	}
	return nil
}

// SettlePursuitTaskResources closes a reservation and appends actual accounting
// records in the same database transaction. Replays are idempotent; a changed
// outcome under the same operation fails closed.
func (s *service) SettlePursuitTaskResources(
	pursuitID uuid.UUID,
	ownerIdentity, operationID, disposition string,
	actualEffortMinutes, actualCostMicros int64,
) error {
	_, err := s.settlePursuitResourceReservation(
		pursuitID, ownerIdentity, operationID, disposition,
		actualEffortMinutes, actualCostMicros, "hai:task-engine", "task execution completed and actual usage was measured", "task://"+strings.TrimSpace(operationID),
		nil,
	)
	return err
}

func (s *service) settlePursuitResourceReservation(
	pursuitID uuid.UUID,
	ownerIdentity, operationID, disposition string,
	actualEffortMinutes, actualCostMicros int64,
	actor, reason, evidenceURI string,
	additionalActivities []models.PursuitActivity,
) (bool, error) {
	pursuit, err := s.taskAttemptPursuit(pursuitID, ownerIdentity)
	if err != nil {
		return false, err
	}
	ownerIdentity = strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	operationID = strings.TrimSpace(operationID)
	disposition = strings.ToLower(strings.TrimSpace(disposition))
	actor = strings.TrimSpace(actor)
	reason = strings.Join(strings.Fields(strings.TrimSpace(reason)), " ")
	evidenceURI = strings.TrimSpace(evidenceURI)
	if ownerIdentity == "" || !validReservationOperationID(operationID) {
		return false, fmt.Errorf("valid owner and resource reservation operation are required")
	}
	if actor == "" || len([]rune(actor)) > 255 {
		return false, fmt.Errorf("a valid settlement actor is required")
	}
	if reason == "" || len([]rune(reason)) > 1000 || safety.RedactSecrets(reason) != reason {
		return false, fmt.Errorf("a safe settlement reason is required")
	}
	if len([]rune(evidenceURI)) > 2048 || safety.RedactSecrets(evidenceURI) != evidenceURI {
		return false, fmt.Errorf("resource reservation settlement evidence is invalid")
	}
	if disposition != ResourceReservationConsumed && disposition != ResourceReservationReleased {
		return false, fmt.Errorf("resource reservation disposition must be consumed or released")
	}
	if actualEffortMinutes < 0 || actualCostMicros < 0 || disposition == ResourceReservationReleased && (actualEffortMinutes != 0 || actualCostMicros != 0) {
		return false, fmt.Errorf("released reservations cannot contain actual usage")
	}
	repository, ok := s.repo.(pursuitResourceReservationRepository)
	if !ok {
		return false, fmt.Errorf("pursuit resource reservation ledger is unavailable")
	}
	reservation, err := s.findResourceReservation(repository, ownerIdentity, pursuitID, operationID)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	settlement := &models.PursuitResourceReservationSettlement{
		ID:            uuid.New(),
		ReservationID: reservation.ID, PursuitID: pursuitID, OwnerIdentity: ownerIdentity,
		Disposition: disposition, ActualEffortMinutes: actualEffortMinutes, ActualCostMicros: actualCostMicros,
		EvidenceURI: evidenceURI, Reason: reason, Actor: actor, SettledAt: now,
	}
	if actualCostMicros > 0 {
		settlement.Currency = "EUR"
	}
	settlement.RecordDigest, err = settlementDigest(settlement)
	if err != nil {
		return false, err
	}
	events, err := settlementResourceEvents(*settlement, operationID, now)
	if err != nil {
		return false, err
	}
	activities := []models.PursuitActivity{newPursuitResourceActivity(
		pursuitID, "pursuit.resource_reservation_settled",
		fmt.Sprintf("Resource reservation %s with %d minutes and EUR %.6f actual usage.", settlement.Disposition, settlement.ActualEffortMinutes, float64(settlement.ActualCostMicros)/1_000_000),
		settlement.Actor, "pursuit_resource_reservation", settlement.ReservationID.String(), settlement.EvidenceURI, now,
	)}
	for _, activity := range additionalActivities {
		activities = append(activities, newPursuitResourceActivity(
			pursuitID, activity.EventType, activity.Message, activity.Actor,
			activity.SourceType, activity.SourceID, activity.SourceURI, now,
		))
	}
	_, created, err := repository.SettleResourceReservation(settlement, events, activities)
	if err != nil {
		return false, fmt.Errorf("settle pursuit resources: %w", err)
	}
	return created, nil
}

// ReleaseResourceReservationForOwner closes a confirmed orphaned hold without
// mutating or deleting its original reservation. This is an explicit operator
// reconciliation path, not an age-based auto-release: a slow task must never
// lose its capacity merely because a clock threshold elapsed.
func (s *service) ReleaseResourceReservationForOwner(
	ownerIdentity string,
	pursuitID, reservationID uuid.UUID,
	request ReleasePursuitResourceReservationRequest,
) (*PursuitResourceUsage, error) {
	pursuit, err := s.repo.FindByID(pursuitID)
	if err != nil || !pursuitMutableBy(valueOrZero(pursuit), ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	ownerIdentity = strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.Join(strings.Fields(strings.TrimSpace(request.Reason)), " ")
	if !request.ConfirmedOrphan {
		return nil, fmt.Errorf("confirmedOrphan must be true before releasing a reservation")
	}
	if len([]rune(request.Reason)) < 12 || len([]rune(request.Reason)) > 1000 {
		return nil, fmt.Errorf("release reason must contain 12 to 1000 characters")
	}
	if safety.RedactSecrets(request.Reason) != request.Reason {
		return nil, fmt.Errorf("release reason must not contain secret material")
	}
	if request.Actor == "" {
		return nil, fmt.Errorf("an authenticated actor is required to release a reservation")
	}
	repository, ok := s.repo.(pursuitResourceReservationRepository)
	if !ok {
		return nil, fmt.Errorf("pursuit resource reservation ledger is unavailable")
	}
	reservation, err := repository.FindResourceReservationByID(ownerIdentity, pursuitID, reservationID)
	if err != nil {
		return nil, fmt.Errorf("active resource reservation not found")
	}
	evidenceURI := fmt.Sprintf("hai://pursuits/%s/resource-reservations/%s/release", pursuitID, reservationID)
	reconciliationActivity := models.PursuitActivity{
		EventType:  "pursuit.resource_reservation_reconciled",
		Message:    "Released a confirmed orphaned resource reservation. Reason: " + request.Reason,
		Actor:      request.Actor,
		SourceType: "pursuit_resource_reservation",
		SourceID:   reservationID.String(),
		SourceURI:  evidenceURI,
	}
	_, err = s.settlePursuitResourceReservation(
		pursuitID, ownerIdentity, reservation.OperationID, ResourceReservationReleased,
		0, 0, request.Actor, request.Reason, evidenceURI,
		[]models.PursuitActivity{reconciliationActivity},
	)
	if err != nil {
		return nil, err
	}
	return s.ResourceUsageForOwner(ownerIdentity, pursuitID)
}

func newPursuitResourceActivity(
	pursuitID uuid.UUID,
	eventType, message, actor, sourceType, sourceID, sourceURI string,
	createdAt time.Time,
) models.PursuitActivity {
	return models.PursuitActivity{
		ID:         uuid.New(),
		PursuitID:  pursuitID,
		EventType:  strings.TrimSpace(eventType),
		Message:    strings.TrimSpace(message),
		Actor:      strings.TrimSpace(actor),
		SourceType: strings.TrimSpace(sourceType),
		SourceID:   strings.TrimSpace(sourceID),
		SourceURI:  strings.TrimSpace(sourceURI),
		CreatedAt:  createdAt,
	}
}

func (s *service) findResourceReservation(repository pursuitResourceReservationRepository, ownerIdentity string, pursuitID uuid.UUID, operationID string) (*models.PursuitResourceReservation, error) {
	reservation, err := repository.FindResourceReservation(ownerIdentity, pursuitID, operationID)
	if err != nil {
		return nil, fmt.Errorf("resource reservation not found")
	}
	return reservation, nil
}

func settlementResourceEvents(settlement models.PursuitResourceReservationSettlement, operationID string, now time.Time) ([]models.PursuitResourceEvent, error) {
	if settlement.Disposition != ResourceReservationConsumed {
		return nil, nil
	}
	events := []models.PursuitResourceEvent{}
	if settlement.ActualEffortMinutes > 0 {
		event, err := normalizePursuitResourceEvent(settlement.PursuitID, settlement.OwnerIdentity, AppendPursuitResourceEventRequest{
			Kind: ResourceKindEffort, EffortHours: float64(settlement.ActualEffortMinutes) / 60,
			Note:        "Task execution effort settled from an atomic reservation.",
			EvidenceURI: settlement.EvidenceURI, IdempotencyKey: operationID + ":effort", Actor: settlement.Actor,
		}, now)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	if settlement.ActualCostMicros > 0 {
		cents := int64(math.Ceil(float64(settlement.ActualCostMicros) / 10_000))
		event, err := normalizePursuitResourceEvent(settlement.PursuitID, settlement.OwnerIdentity, AppendPursuitResourceEventRequest{
			Kind: ResourceKindSpend, SpendEUR: float64(cents) / 100,
			EvidenceURI: settlement.EvidenceURI, IdempotencyKey: operationID + ":spend", Actor: settlement.Actor,
		}, now)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, nil
}

func validReservationOperationID(value string) bool {
	return len(value) <= 120 && validResourceIdempotencyKey(value)
}

func reservationDigest(value *models.PursuitResourceReservation) (string, error) {
	payload := struct {
		PursuitID, OwnerIdentity, OperationID, Currency, Reason, Actor string
		EffortMinutes, CostMicros                                      int64
	}{value.PursuitID.String(), value.OwnerIdentity, value.OperationID, value.Currency, value.Reason, value.Actor, value.EstimatedEffortMinutes, value.EstimatedCostMicros}
	return digestReservationPayload(payload)
}

func settlementDigest(value *models.PursuitResourceReservationSettlement) (string, error) {
	payload := struct {
		ReservationID, PursuitID, OwnerIdentity, Disposition, Currency, EvidenceURI, Reason, Actor string
		EffortMinutes, CostMicros                                                                  int64
	}{value.ReservationID.String(), value.PursuitID.String(), value.OwnerIdentity, value.Disposition, value.Currency, value.EvidenceURI, value.Reason, value.Actor, value.ActualEffortMinutes, value.ActualCostMicros}
	return digestReservationPayload(payload)
}

func digestReservationPayload(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode pursuit resource reservation: %w", err)
	}
	validationPayload := encoded
	if payload, ok := value.(portfolioWorkflowSettlementDigestPayload); ok {
		// This is an execution destination, not an Authorization credential.
		// Its string value remains scanned; original bytes remain digest-bound.
		validationPayload, err = json.Marshal(struct {
			portfolioWorkflowSettlementDigestPayload
			AuthorizationTarget  *string `json:"AuthorizationTarget,omitempty"`
			ExecutionDestination string
		}{portfolioWorkflowSettlementDigestPayload: payload, ExecutionDestination: payload.AuthorizationTarget})
		if err != nil {
			return "", fmt.Errorf("encode resource validation: %w", err)
		}
	}
	if safety.RedactSecrets(string(validationPayload)) != string(validationPayload) {
		return "", fmt.Errorf("resource reservation must not contain secret material")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

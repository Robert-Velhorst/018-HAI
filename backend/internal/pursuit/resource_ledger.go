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
	"unicode"

	"github.com/google/uuid"
)

const (
	ResourceKindEffort = "effort_recorded"
	ResourceKindSpend  = "spend_incurred"
	ResourceKindRefund = "spend_refund"
)

type AppendPursuitResourceEventRequest struct {
	Kind           string  `json:"kind"`
	EffortHours    float64 `json:"effortHours,omitempty"`
	SpendEUR       float64 `json:"spendEur,omitempty"`
	Note           string  `json:"note,omitempty"`
	EvidenceURI    string  `json:"evidenceUri,omitempty"`
	IdempotencyKey string  `json:"idempotencyKey"`
	OccurredAt     string  `json:"occurredAt,omitempty"`
	Actor          string  `json:"-"`
}

type PursuitResourceTotals struct {
	EffortMinutes    int64      `gorm:"column:effort_minutes"`
	IncurredMinor    int64      `gorm:"column:incurred_minor"`
	RefundedMinor    int64      `gorm:"column:refunded_minor"`
	EventCount       int64      `gorm:"column:event_count"`
	LatestRecordedAt *time.Time `gorm:"column:latest_recorded_at"`
}

type PursuitResourceReservationTotals struct {
	EffortMinutes    int64      `gorm:"column:effort_minutes"`
	CostMicros       int64      `gorm:"column:cost_micros"`
	ReservationCount int64      `gorm:"column:reservation_count"`
	LatestReservedAt *time.Time `gorm:"column:latest_reserved_at"`
}

type PursuitResourceUsage struct {
	State                string                             `json:"state"`
	Available            bool                               `json:"available"`
	LimitsConfigured     bool                               `json:"limitsConfigured"`
	EffortRecordedHours  float64                            `json:"effortRecordedHours"`
	EffortReservedHours  float64                            `json:"effortReservedHours"`
	EffortCommittedHours float64                            `json:"effortCommittedHours"`
	EffortLimitHours     float64                            `json:"effortLimitHours"`
	EffortRemainingHours float64                            `json:"effortRemainingHours"`
	EffortExhausted      bool                               `json:"effortExhausted"`
	EffortExceeded       bool                               `json:"effortExceeded"`
	SpendIncurredEUR     float64                            `json:"spendIncurredEur"`
	SpendRefundedEUR     float64                            `json:"spendRefundedEur"`
	SpendNetEUR          float64                            `json:"spendNetEur"`
	SpendReservedEUR     float64                            `json:"spendReservedEur"`
	SpendCommittedEUR    float64                            `json:"spendCommittedEur"`
	SpendLimitEUR        float64                            `json:"spendLimitEur"`
	SpendRemainingEUR    float64                            `json:"spendRemainingEur"`
	SpendExhausted       bool                               `json:"spendExhausted"`
	SpendExceeded        bool                               `json:"spendExceeded"`
	EventCount           int64                              `json:"eventCount"`
	ActiveReservations   int64                              `json:"activeReservations"`
	LatestRecordedAt     *time.Time                         `json:"latestRecordedAt,omitempty"`
	LatestReservedAt     *time.Time                         `json:"latestReservedAt,omitempty"`
	BlockingReason       string                             `json:"blockingReason,omitempty"`
	Reservations         []PursuitActiveResourceReservation `json:"reservations"`
}

const staleResourceReservationAge = 24 * time.Hour

// PursuitActiveResourceReservation is the safe operator-facing projection of
// an unsettled immutable hold. It intentionally excludes record digests and
// owner identity while retaining enough evidence to reconcile a crashed run.
type PursuitActiveResourceReservation struct {
	ID                     uuid.UUID `json:"id"`
	OperationID            string    `json:"operationId"`
	EstimatedEffortMinutes int64     `json:"estimatedEffortMinutes"`
	EstimatedCostEUR       float64   `json:"estimatedCostEur"`
	Reason                 string    `json:"reason"`
	Actor                  string    `json:"actor"`
	ReservedAt             time.Time `json:"reservedAt"`
	Stale                  bool      `json:"stale"`
	ReviewReason           string    `json:"reviewReason,omitempty"`
}

type ReleasePursuitResourceReservationRequest struct {
	ConfirmedOrphan bool   `json:"confirmedOrphan"`
	Reason          string `json:"reason"`
	Actor           string `json:"-"`
}

func (s *service) AppendResourceEvent(id uuid.UUID, request AppendPursuitResourceEventRequest) (*models.PursuitResourceEvent, error) {
	return s.AppendResourceEventForOwner("", id, request)
}

func (s *service) AppendResourceEventForOwner(ownerIdentity string, id uuid.UUID, request AppendPursuitResourceEventRequest) (*models.PursuitResourceEvent, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil || !pursuitMutableBy(valueOrZero(pursuit), ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	ownerIdentity = strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	if ownerIdentity == "" {
		return nil, fmt.Errorf("an authenticated owner identity is required to record pursuit resources")
	}
	repository, ok := s.repo.(pursuitResourceRepository)
	if !ok {
		return nil, fmt.Errorf("pursuit resource ledger is unavailable")
	}
	event, err := normalizePursuitResourceEvent(id, ownerIdentity, request, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	stored, created, err := repository.AppendResourceEvent(event)
	if err != nil {
		return nil, err
	}
	if created {
		message := resourceEventActivityMessage(*stored)
		_, err = s.recordActivity(id, "pursuit.resource_recorded", message, stored.Actor, "pursuit_resource_event", stored.ID.String(), stored.EvidenceURI)
		if err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func (s *service) ResourceUsage(id uuid.UUID) (*PursuitResourceUsage, error) {
	return s.ResourceUsageForOwner("", id)
}

func (s *service) ResourceUsageForOwner(ownerIdentity string, id uuid.UUID) (*PursuitResourceUsage, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil || !pursuitVisibleTo(valueOrZero(pursuit), ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	usage := s.resourceUsageForPursuit(ownerIdentity, *pursuit)
	return &usage, nil
}

func (s *service) ResourceEventsForOwner(ownerIdentity string, id uuid.UUID, limit int) ([]models.PursuitResourceEvent, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil || !pursuitVisibleTo(valueOrZero(pursuit), ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	repository, ok := s.repo.(pursuitResourceRepository)
	if !ok {
		return nil, fmt.Errorf("pursuit resource ledger is unavailable")
	}
	effectiveOwner := strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	if effectiveOwner == "" {
		return []models.PursuitResourceEvent{}, nil
	}
	return repository.FindResourceEventsForOwner(effectiveOwner, id, limit)
}

func (s *service) resourceUsageForPursuit(ownerIdentity string, pursuit models.Pursuit) PursuitResourceUsage {
	usage := PursuitResourceUsage{
		State:            "not_configured",
		Reservations:     []PursuitActiveResourceReservation{},
		LimitsConfigured: pursuit.ResourceLimits.MaxEffortHours > 0 || pursuit.ResourceLimits.MaxSpendEUR > 0,
		EffortLimitHours: pursuit.ResourceLimits.MaxEffortHours,
		SpendLimitEUR:    pursuit.ResourceLimits.MaxSpendEUR,
	}
	if !usage.LimitsConfigured {
		return usage
	}
	repository, ok := s.repo.(pursuitResourceRepository)
	effectiveOwner := strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity))
	if !ok || effectiveOwner == "" {
		usage.State = "unavailable"
		usage.BlockingReason = "resource usage cannot be verified; new work is paused while a pursuit ceiling is configured"
		return usage
	}
	totals, err := repository.SummarizeResourceEventsForOwner(effectiveOwner, pursuit.ID)
	if err != nil {
		usage.State = "unavailable"
		usage.BlockingReason = "resource usage cannot be verified; new work is paused while a pursuit ceiling is configured"
		return usage
	}
	reserved := PursuitResourceReservationTotals{}
	if reservations, ok := s.repo.(pursuitResourceReservationSummaryRepository); ok {
		var reservationErr error
		reserved, reservationErr = reservations.SummarizeActiveResourceReservations(effectiveOwner, pursuit.ID)
		if reservationErr != nil {
			usage.State = "unavailable"
			usage.Available = false
			usage.BlockingReason = "active resource reservations cannot be verified; new work is paused"
			return usage
		}
	}
	active := []models.PursuitResourceReservation{}
	if reservations, ok := s.repo.(pursuitResourceReservationRepository); ok {
		var reservationErr error
		active, reservationErr = reservations.FindActiveResourceReservations(effectiveOwner, pursuit.ID, 50)
		if reservationErr != nil {
			usage.State = "unavailable"
			usage.Available = false
			usage.BlockingReason = "active resource reservation detail cannot be verified; new work is paused"
			return usage
		}
	}
	return pursuitResourceUsageFromRecords(pursuit, totals, reserved, active, time.Now().UTC())
}

func pursuitResourceUsageFromRecords(pursuit models.Pursuit, totals PursuitResourceTotals, reserved PursuitResourceReservationTotals, active []models.PursuitResourceReservation, now time.Time) PursuitResourceUsage {
	usage := PursuitResourceUsage{
		State: "within_limits", Available: true, LimitsConfigured: true,
		EffortLimitHours: pursuit.ResourceLimits.MaxEffortHours,
		SpendLimitEUR:    pursuit.ResourceLimits.MaxSpendEUR,
		Reservations:     []PursuitActiveResourceReservation{},
		EventCount:       totals.EventCount, LatestRecordedAt: totals.LatestRecordedAt,
		ActiveReservations: reserved.ReservationCount, LatestReservedAt: reserved.LatestReservedAt,
	}
	usage.EffortRecordedHours = roundResource(float64(totals.EffortMinutes) / 60)
	usage.EffortReservedHours = roundResource(float64(reserved.EffortMinutes) / 60)
	usage.SpendIncurredEUR = roundResource(float64(totals.IncurredMinor) / 100)
	usage.SpendRefundedEUR = roundResource(float64(totals.RefundedMinor) / 100)
	usage.SpendReservedEUR = roundResource(float64(reserved.CostMicros) / 1_000_000)
	netMinor := totals.IncurredMinor - totals.RefundedMinor
	if netMinor < 0 {
		netMinor = 0
	}
	usage.SpendNetEUR = roundResource(float64(netMinor) / 100)
	for _, reservation := range active {
		item := PursuitActiveResourceReservation{
			ID: reservation.ID, OperationID: reservation.OperationID,
			EstimatedEffortMinutes: reservation.EstimatedEffortMinutes,
			EstimatedCostEUR:       roundResource(float64(reservation.EstimatedCostMicros) / 1_000_000),
			Reason:                 reservation.Reason, Actor: reservation.Actor, ReservedAt: reservation.ReservedAt,
		}
		item.Stale = !reservation.ReservedAt.IsZero() && now.Sub(reservation.ReservedAt) >= staleResourceReservationAge
		if item.Stale {
			item.ReviewReason = "No settlement has been recorded for at least 24 hours; confirm the operation is no longer running before release."
		}
		usage.Reservations = append(usage.Reservations, item)
	}
	usage.EffortCommittedHours = roundResource(usage.EffortRecordedHours + usage.EffortReservedHours)
	usage.SpendCommittedEUR = roundResource(usage.SpendNetEUR + usage.SpendReservedEUR)
	if usage.EffortLimitHours > 0 {
		usage.EffortRemainingHours = roundResource(math.Max(0, usage.EffortLimitHours-usage.EffortCommittedHours))
		usage.EffortExhausted = usage.EffortCommittedHours >= usage.EffortLimitHours
		usage.EffortExceeded = usage.EffortRecordedHours > usage.EffortLimitHours
	}
	if usage.SpendLimitEUR > 0 {
		usage.SpendRemainingEUR = roundResource(math.Max(0, usage.SpendLimitEUR-usage.SpendCommittedEUR))
		usage.SpendExhausted = usage.SpendCommittedEUR >= usage.SpendLimitEUR
		usage.SpendExceeded = usage.SpendNetEUR > usage.SpendLimitEUR
	}
	usage.State = "within_limits"
	if usage.ActiveReservations > 0 {
		usage.State = "reserved"
	}
	if usage.EffortExhausted || usage.SpendExhausted {
		usage.State = "exhausted"
	}
	if usage.EffortExceeded || usage.SpendExceeded {
		usage.State = "exceeded"
	}
	usage.BlockingReason = resourceUsageBlockingReason(usage)
	return usage
}

func normalizePursuitResourceEvent(pursuitID uuid.UUID, ownerIdentity string, request AppendPursuitResourceEventRequest, now time.Time) (*models.PursuitResourceEvent, error) {
	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	if kind != ResourceKindEffort && kind != ResourceKindSpend && kind != ResourceKindRefund {
		return nil, fmt.Errorf("resource kind must be effort_recorded, spend_incurred, or spend_refund")
	}
	request.Note = strings.TrimSpace(request.Note)
	request.EvidenceURI = strings.TrimSpace(request.EvidenceURI)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Actor = strings.TrimSpace(request.Actor)
	if !validResourceIdempotencyKey(request.IdempotencyKey) {
		return nil, fmt.Errorf("idempotency key must contain 1 to 120 safe identifier characters")
	}
	if request.Actor == "" || len([]rune(request.Actor)) > 255 {
		return nil, fmt.Errorf("actor is required")
	}
	if len([]rune(request.Note)) > 2000 || len([]rune(request.EvidenceURI)) > 2048 {
		return nil, fmt.Errorf("resource note or evidence URI is too long")
	}
	if safety.RedactSecrets(request.Note) != request.Note || safety.RedactSecrets(request.EvidenceURI) != request.EvidenceURI {
		return nil, fmt.Errorf("resource evidence must not contain secret material")
	}
	if kind == ResourceKindEffort && request.Note == "" && request.EvidenceURI == "" {
		return nil, fmt.Errorf("effort records require a note or evidence URI")
	}
	if kind != ResourceKindEffort && request.EvidenceURI == "" {
		return nil, fmt.Errorf("spend and refund records require an evidence URI")
	}
	occurredAt := now.UTC().Truncate(time.Second)
	if strings.TrimSpace(request.OccurredAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(request.OccurredAt))
		if err != nil {
			return nil, fmt.Errorf("occurredAt must be RFC3339")
		}
		occurredAt = parsed.UTC().Truncate(time.Second)
	}
	if occurredAt.After(now.Add(5*time.Minute)) || occurredAt.Before(now.AddDate(-50, 0, 0)) {
		return nil, fmt.Errorf("occurredAt is outside the accepted accounting window")
	}

	event := &models.PursuitResourceEvent{
		PursuitID: pursuitID, OwnerIdentity: ownerIdentity, Kind: kind,
		Note: request.Note, EvidenceURI: request.EvidenceURI, Actor: request.Actor,
		IdempotencyKey: request.IdempotencyKey, OccurredAt: occurredAt,
	}
	if kind == ResourceKindEffort {
		if math.IsNaN(request.EffortHours) || math.IsInf(request.EffortHours, 0) || request.EffortHours <= 0 || request.EffortHours > 1_000_000 {
			return nil, fmt.Errorf("effortHours must be greater than zero and within the supported range")
		}
		event.EffortMinutes = int64(math.Round(request.EffortHours * 60))
		if event.EffortMinutes <= 0 {
			return nil, fmt.Errorf("effortHours must represent at least one minute")
		}
		if request.SpendEUR != 0 {
			return nil, fmt.Errorf("effort records cannot include spend")
		}
	} else {
		if math.IsNaN(request.SpendEUR) || math.IsInf(request.SpendEUR, 0) || request.SpendEUR <= 0 || request.SpendEUR > 100_000_000 {
			return nil, fmt.Errorf("spendEur must be greater than zero and within the supported range")
		}
		event.AmountMinor = int64(math.Round(request.SpendEUR * 100))
		if event.AmountMinor <= 0 {
			return nil, fmt.Errorf("spendEur must represent at least one cent")
		}
		event.Currency = "EUR"
		if request.EffortHours != 0 {
			return nil, fmt.Errorf("spend records cannot include effort")
		}
	}
	digestPayload := struct {
		PursuitID, OwnerIdentity, Kind, Note, EvidenceURI, Actor, IdempotencyKey string
		EffortMinutes, AmountMinor                                               int64
		Currency                                                                 string
		OccurredAt                                                               time.Time
	}{
		pursuitID.String(), ownerIdentity, event.Kind, event.Note, event.EvidenceURI, event.Actor, event.IdempotencyKey,
		event.EffortMinutes, event.AmountMinor, event.Currency, event.OccurredAt,
	}
	encoded, err := json.Marshal(digestPayload)
	if err != nil {
		return nil, fmt.Errorf("encode pursuit resource event: %w", err)
	}
	digest := sha256.Sum256(encoded)
	event.RecordDigest = hex.EncodeToString(digest[:])
	return event, nil
}

func validResourceIdempotencyKey(value string) bool {
	if value == "" || len(value) > 120 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("-_.:", character) {
			continue
		}
		return false
	}
	return true
}

func resourceUsageBlockingReason(usage PursuitResourceUsage) string {
	if usage.State == "unavailable" {
		return usage.BlockingReason
	}
	if usage.EffortExceeded {
		return fmt.Sprintf("effort ceiling exceeded (%.2f of %.2f hours recorded)", usage.EffortRecordedHours, usage.EffortLimitHours)
	}
	if usage.EffortExhausted {
		return fmt.Sprintf("effort ceiling exhausted (%.2f recorded + %.2f reserved of %.2f hours)", usage.EffortRecordedHours, usage.EffortReservedHours, usage.EffortLimitHours)
	}
	if usage.SpendExceeded {
		return fmt.Sprintf("spend ceiling exceeded (EUR %.2f of EUR %.2f recorded)", usage.SpendNetEUR, usage.SpendLimitEUR)
	}
	if usage.SpendExhausted {
		return fmt.Sprintf("spend ceiling exhausted (EUR %.2f recorded + EUR %.2f reserved of EUR %.2f)", usage.SpendNetEUR, usage.SpendReservedEUR, usage.SpendLimitEUR)
	}
	return ""
}

func (s *service) newWorkBlockerReasonForOwner(ownerIdentity string, pursuit models.Pursuit, workflows []models.WorkflowItem) string {
	if reason := pursuitNewWorkBlockerReason(pursuit, workflows); reason != "" {
		return reason
	}
	return resourceUsageBlockingReason(s.resourceUsageForPursuit(ownerIdentity, pursuit))
}

func pursuitResourceBlockers(pursuit models.Pursuit, usage PursuitResourceUsage) []PursuitBlocker {
	if pursuitClosed(pursuit) || !usage.LimitsConfigured || usage.BlockingReason == "" {
		return []PursuitBlocker{}
	}
	label := "Resource ceiling reached"
	if usage.State == "unavailable" {
		label = "Resource usage unavailable"
	}
	return []PursuitBlocker{{
		Label:  label,
		Reason: usage.BlockingReason,
		Owner:  "Robert",
	}}
}

func resourceEventActivityMessage(event models.PursuitResourceEvent) string {
	switch event.Kind {
	case ResourceKindEffort:
		return fmt.Sprintf("Recorded %.2f hours of pursuit effort.", float64(event.EffortMinutes)/60)
	case ResourceKindRefund:
		return fmt.Sprintf("Recorded EUR %.2f pursuit refund.", float64(event.AmountMinor)/100)
	default:
		return fmt.Sprintf("Recorded EUR %.2f pursuit spend.", float64(event.AmountMinor)/100)
	}
}

func roundResource(value float64) float64 { return math.Round(value*100) / 100 }

func valueOrZero(value *models.Pursuit) models.Pursuit {
	if value == nil {
		return models.Pursuit{}
	}
	return *value
}

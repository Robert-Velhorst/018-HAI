package lifeops

import (
	"fmt"
	"math"
	"strings"
	"time"
)

var verificationStatuses = map[string]bool{
	"verified":         true,
	"source_supported": true,
	"human_confirmed":  true,
	"uncertain":        true,
	"unsupported":      true,
	"needs_review":     true,
}

var needStates = map[string]bool{
	"unknown":            true,
	"stable":             true,
	"active":             true,
	"attention_required": true,
	"critical":           true,
	"improving":          true,
	"declining":          true,
	"met":                true,
}

var capacityStatuses = map[string]bool{
	CapacityUnknown:     true,
	CapacityAvailable:   true,
	CapacityConstrained: true,
	CapacityOverloaded:  true,
	CapacityUnavailable: true,
	CapacityRecovering:  true,
}

var goalStatuses = map[string]bool{
	"proposed":  true,
	"active":    true,
	"waiting":   true,
	"blocked":   true,
	"completed": true,
	"abandoned": true,
	"archived":  true,
}

func validUnitConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validateLinkRequest(request LinkEntityRequest) error {
	if request.OwnerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if request.EntityType == "" || request.EntityID == "" {
		return fmt.Errorf("entity type and entity id are required")
	}
	if !IsCanonicalDomain(request.DomainID) {
		return fmt.Errorf("unknown life domain %q", request.DomainID)
	}
	if !validUnitConfidence(request.Confidence) {
		return fmt.Errorf("link confidence must be between 0 and 1")
	}
	if request.SourceLabel == "" {
		return fmt.Errorf("link source label is required")
	}
	if !verificationStatuses[request.VerificationStatus] {
		return fmt.Errorf("invalid verification status %q", request.VerificationStatus)
	}
	return nil
}

func validateNeedRequest(request RecordNeedRequest, now time.Time) error {
	if request.OwnerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if !IsCanonicalDomain(request.DomainID) {
		return fmt.Errorf("unknown life domain %q", request.DomainID)
	}
	if request.NeedLevel == "" {
		return fmt.Errorf("need level is required")
	}
	if !needStates[request.State] {
		return fmt.Errorf("invalid need state %q", request.State)
	}
	for label, value := range map[string]int{
		"current level": request.CurrentLevel,
		"target level":  request.TargetLevel,
		"priority":      request.Priority,
	} {
		if value < 0 || value > 100 {
			return fmt.Errorf("need %s must be between 0 and 100", label)
		}
	}
	if !validUnitConfidence(request.Confidence) {
		return fmt.Errorf("need confidence must be between 0 and 1")
	}
	if request.SourceLabel == "" {
		return fmt.Errorf("need source label is required")
	}
	if !request.ObservedAt.IsZero() && request.ObservedAt.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("need observation cannot be in the future")
	}
	if request.ExpiresAt != nil {
		observedAt := request.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		if !request.ExpiresAt.After(observedAt) {
			return fmt.Errorf("need expiry must be after observation time")
		}
	}
	return nil
}

func validateCapacityRequest(request RecordCapacityRequest, now time.Time) error {
	if request.OwnerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if !capacityStatuses[request.Status] {
		return fmt.Errorf("invalid capacity status %q", request.Status)
	}
	if request.SourceLabel == "" {
		return fmt.Errorf("capacity source label is required")
	}
	if request.CapturedAt.IsZero() {
		return fmt.Errorf("capacity capture time is required")
	}
	if request.CapturedAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("capacity capture time cannot be in the future")
	}
	if request.TimeAvailableMinutes < 0 || request.ConcurrentWorkLimit < 0 {
		return fmt.Errorf("capacity time and concurrent work limit cannot be negative")
	}
	if request.CurrentLoad < 0 || request.CurrentLoad > 100 {
		return fmt.Errorf("capacity current load must be between 0 and 100")
	}
	if request.PlanningStepLimit < 0 || request.PlanningStepLimit > 20 {
		return fmt.Errorf("capacity planning step limit must be between 0 and 20")
	}
	if !validUnitConfidence(request.Confidence) {
		return fmt.Errorf("capacity confidence must be between 0 and 1")
	}
	for label, value := range capacitySignalValues(request.Signals) {
		if value < 0 || value > 100 {
			return fmt.Errorf("capacity signal %s must be between 0 and 100", label)
		}
	}
	return nil
}

func capacitySignalValues(signals CapacitySignals) map[string]int {
	return map[string]int{
		"energy":                   signals.Energy,
		"attention quality":        signals.AttentionQuality,
		"pain illness load":        signals.PainIllnessLoad,
		"sleep quality":            signals.SleepQuality,
		"stress load":              signals.StressLoad,
		"mobility":                 signals.Mobility,
		"financial liquidity":      signals.FinancialLiquidity,
		"deadline pressure":        signals.DeadlinePressure,
		"interruption sensitivity": signals.InterruptionSensitivity,
		"recovery requirement":     signals.RecoveryRequirement,
		"task switching cost":      signals.TaskSwitchingCost,
		"sensory load":             signals.SensoryLoad,
		"decision fatigue":         signals.DecisionFatigue,
		"risk tolerance":           signals.RiskTolerance,
		"confidence readiness":     signals.ConfidenceReadiness,
	}
}

func validateGoalFields(goal GoalNode) error {
	if goal.OwnerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if goal.ID == [16]byte{} {
		return fmt.Errorf("goal id is required")
	}
	rank, ok := GoalLevelRank(goal.Level)
	if !ok {
		return fmt.Errorf("invalid goal level %q", goal.Level)
	}
	if len(goal.DomainIDs) == 0 {
		return fmt.Errorf("at least one life domain is required")
	}
	for _, domainID := range goal.DomainIDs {
		if !IsCanonicalDomain(domainID) {
			return fmt.Errorf("unknown life domain %q", domainID)
		}
	}
	if strings.TrimSpace(goal.Title) == "" {
		return fmt.Errorf("goal title is required")
	}
	if !goalStatuses[goal.Status] {
		return fmt.Errorf("invalid goal status %q", goal.Status)
	}
	if !validUnitConfidence(goal.Confidence) {
		return fmt.Errorf("goal confidence must be between 0 and 1")
	}
	if goal.SourceLabel == "" {
		return fmt.Errorf("goal source label is required")
	}
	if rank >= 5 && rank <= 10 {
		if len(goal.SuccessCriteria) == 0 {
			return fmt.Errorf("pursuits and executable descendants require success criteria")
		}
		if len(goal.StopConditions) == 0 {
			return fmt.Errorf("pursuits and executable descendants require stop conditions")
		}
	}
	return nil
}

func validatePriorityRequest(request PriorityAssessmentRequest, now time.Time) error {
	if request.OwnerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if request.EntityType == "" || request.EntityID == "" || request.Title == "" {
		return fmt.Errorf("entity type, entity id, and title are required")
	}
	if request.Deadline != nil && request.Deadline.Before(now.Add(-365*24*time.Hour)) {
		return fmt.Errorf("deadline is implausibly old")
	}
	for label, value := range priorityFactorValues(request.Factors) {
		if value < 0 || value > 100 {
			return fmt.Errorf("priority factor %s must be between 0 and 100", label)
		}
	}
	if request.Capacity != nil {
		if request.Capacity.OwnerIdentity != request.OwnerIdentity {
			return fmt.Errorf("capacity snapshot owner does not match priority owner")
		}
		if request.Capacity.ID == [16]byte{} {
			return fmt.Errorf("capacity snapshot id is required")
		}
	}
	return nil
}

func priorityFactorValues(factors PriorityFactors) map[string]int {
	return map[string]int{
		"importance":                factors.Importance,
		"urgency":                   factors.Urgency,
		"human need affected":       factors.HumanNeedAffected,
		"deadline pressure":         factors.DeadlinePressure,
		"cost of delay":             factors.CostOfDelay,
		"expected value":            factors.ExpectedValue,
		"harm avoided":              factors.HarmAvoided,
		"probability of success":    factors.ProbabilityOfSuccess,
		"effort":                    factors.Effort,
		"duration":                  factors.Duration,
		"dependencies":              factors.Dependencies,
		"reversibility":             factors.Reversibility,
		"risk":                      factors.Risk,
		"legal obligation":          factors.LegalObligation,
		"relationship consequences": factors.RelationshipConsequences,
		"available capacity":        factors.AvailableCapacity,
		"energy fit":                factors.EnergyFit,
		"opportunity cost":          factors.OpportunityCost,
		"strategic alignment":       factors.StrategicAlignment,
		"learning value":            factors.LearningValue,
		"compounding value":         factors.CompoundingValue,
		"staleness":                 factors.Staleness,
		"commitment age":            factors.CommitmentAge,
		"people blocked":            factors.PeopleBlocked,
		"delegability":              factors.Delegability,
	}
}

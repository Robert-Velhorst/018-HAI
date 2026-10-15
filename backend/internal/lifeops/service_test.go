package lifeops

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidUnitConfidenceRejectsNonFiniteAndOutOfRangeValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.01, 1.01} {
		if validUnitConfidence(value) {
			t.Errorf("validUnitConfidence(%v) = true, want false", value)
		}
	}
	for _, value := range []float64{0, 0.5, 1} {
		if !validUnitConfidence(value) {
			t.Errorf("validUnitConfidence(%v) = false, want true", value)
		}
	}
}

func TestCanonicalLifeDomainsAreStableAndUnique(t *testing.T) {
	domains := CanonicalLifeDomains()
	if len(domains) != 24 {
		t.Fatalf("canonical domain count = %d, want 24", len(domains))
	}
	seen := map[DomainID]bool{}
	for _, domain := range domains {
		if domain.ID == "" || domain.Name == "" || domain.NeedClass == "" {
			t.Fatalf("incomplete domain: %#v", domain)
		}
		if seen[domain.ID] {
			t.Fatalf("duplicate domain id %q", domain.ID)
		}
		seen[domain.ID] = true
	}
	domains[0].Name = "mutated"
	fresh := CanonicalLifeDomains()
	if fresh[0].Name == "mutated" {
		t.Fatal("canonical domain catalog leaked mutable state")
	}
}

func TestEntityDomainLinksAreOwnerScopedAndMaintainOnePrimary(t *testing.T) {
	service := NewService(nil)
	first, err := service.LinkEntity(LinkEntityRequest{
		OwnerIdentity: "alice",
		EntityType:    "pursuit",
		EntityID:      "p-1",
		DomainID:      DomainWorkVenture,
		Confidence:    0.8,
		SourceLabel:   "operator",
	})
	if err != nil {
		t.Fatalf("LinkEntity first: %v", err)
	}
	if !first.Primary {
		t.Fatal("first domain link should become primary")
	}
	if _, err := service.LinkEntity(LinkEntityRequest{
		OwnerIdentity:      "alice",
		EntityType:         "pursuit",
		EntityID:           "p-1",
		DomainID:           DomainFinancial,
		Primary:            true,
		Confidence:         0.9,
		SourceLabel:        "source document",
		VerificationStatus: "source_supported",
	}); err != nil {
		t.Fatalf("LinkEntity second: %v", err)
	}
	links, err := service.EntityDomains("alice", "pursuit", "p-1")
	if err != nil {
		t.Fatalf("EntityDomains: %v", err)
	}
	if len(links) != 2 || !links[0].Primary || links[0].DomainID != DomainFinancial || links[1].Primary {
		t.Fatalf("unexpected domain links: %#v", links)
	}
	bob, err := service.EntityDomains("bob", "pursuit", "p-1")
	if err != nil || len(bob) != 0 {
		t.Fatalf("owner-scoped links leaked: %#v, %v", bob, err)
	}
}

func TestEntityDomainLinkRejectsUnknownDomain(t *testing.T) {
	service := NewService(nil)
	_, err := service.LinkEntity(LinkEntityRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "t-1",
		DomainID:      "imaginary",
		Confidence:    0.8,
		SourceLabel:   "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown life domain") {
		t.Fatalf("unknown domain returned %v", err)
	}
}

func TestNeedObservationsComputeGapAndRemainOwnerScoped(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	observation, err := service.RecordNeed(RecordNeedRequest{
		OwnerIdentity: "alice",
		DomainID:      DomainHealthWellbeing,
		NeedLevel:     "physiological_and_wellbeing",
		State:         "attention required",
		CurrentLevel:  35,
		TargetLevel:   80,
		Priority:      90,
		Confidence:    0.75,
		Evidence:      []string{"operator report", "operator report"},
		SourceLabel:   "daily check-in",
	})
	if err != nil {
		t.Fatalf("RecordNeed: %v", err)
	}
	if observation.Gap != 45 || observation.State != "attention_required" || len(observation.Evidence) != 1 {
		t.Fatalf("observation = %#v", observation)
	}
	alice, err := service.Needs("alice", DomainHealthWellbeing, 10)
	if err != nil || len(alice) != 1 {
		t.Fatalf("Alice needs = %#v, %v", alice, err)
	}
	bob, err := service.Needs("bob", "", 10)
	if err != nil || len(bob) != 0 {
		t.Fatalf("Alice need leaked to Bob: %#v, %v", bob, err)
	}
}

func TestCapacitySnapshotsEnforceFreshnessAndPlanningBounds(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	stale, err := service.RecordCapacity(RecordCapacityRequest{
		OwnerIdentity:        "alice",
		Status:               CapacityAvailable,
		Signals:              CapacitySignals{Energy: 80, AttentionQuality: 75, ConfidenceReadiness: 70},
		TimeAvailableMinutes: 120,
		ConcurrentWorkLimit:  3,
		CurrentLoad:          25,
		SourceLabel:          "operator check-in",
		CapturedAt:           now.Add(-25 * time.Hour),
		Confidence:           0.9,
	})
	if err != nil {
		t.Fatalf("RecordCapacity stale: %v", err)
	}
	if stale.Fresh || !stale.NeedsReview || !contains(stale.Constraints, "older than 24 hours") {
		t.Fatalf("stale snapshot not constrained: %#v", stale)
	}
	fresh, err := service.RecordCapacity(RecordCapacityRequest{
		OwnerIdentity:        "alice",
		Status:               CapacityConstrained,
		Signals:              CapacitySignals{Energy: 25, AttentionQuality: 30, StressLoad: 90},
		TimeAvailableMinutes: 20,
		ConcurrentWorkLimit:  1,
		CurrentLoad:          80,
		SourceLabel:          "operator check-in",
		CapturedAt:           now,
		Confidence:           0.9,
	})
	if err != nil {
		t.Fatalf("RecordCapacity fresh: %v", err)
	}
	if !fresh.Fresh || fresh.PlanningStepLimit != 3 {
		t.Fatalf("fresh constrained snapshot = %#v", fresh)
	}
	latest, err := service.LatestCapacity("alice")
	if err != nil || latest.ID != fresh.ID {
		t.Fatalf("LatestCapacity = %#v, %v", latest, err)
	}
}

func TestCapacityRejectsFutureAndInvalidSignals(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	base := RecordCapacityRequest{
		OwnerIdentity: "alice",
		Status:        CapacityAvailable,
		Signals:       CapacitySignals{Energy: 101},
		SourceLabel:   "operator",
		CapturedAt:    now,
		Confidence:    1,
	}
	if _, err := service.RecordCapacity(base); err == nil || !strings.Contains(err.Error(), "energy") {
		t.Fatalf("invalid signal returned %v", err)
	}
	base.Signals.Energy = 50
	base.CapturedAt = now.Add(10 * time.Minute)
	if _, err := service.RecordCapacity(base); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("future capacity returned %v", err)
	}
}

func TestGoalHierarchyBuildsTwelveLevelTree(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	root, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity: "alice",
		Level:         GoalLevelValues,
		DomainIDs:     []DomainID{DomainMeaningValues},
		Title:         "Live by verified commitments",
		Confidence:    1,
		SourceLabel:   "operator",
	})
	if err != nil {
		t.Fatalf("CreateGoal root: %v", err)
	}
	pursuit, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity:   "alice",
		ParentID:        &root.ID,
		Level:           GoalLevelPursuit,
		DomainIDs:       []DomainID{DomainWorkVenture},
		Title:           "Make HAI operational",
		SuccessCriteria: []string{"live task completes with evidence"},
		StopConditions:  []string{"operator abandons the pursuit"},
		Confidence:      0.9,
		SourceLabel:     "operator",
	})
	if err != nil {
		t.Fatalf("CreateGoal pursuit: %v", err)
	}
	task, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity:   "alice",
		ParentID:        &pursuit.ID,
		Level:           GoalLevelTask,
		DomainIDs:       []DomainID{DomainWorkVenture},
		Title:           "Implement life operations",
		SuccessCriteria: []string{"focused tests pass"},
		StopConditions:  []string{"scope boundary is reached"},
		Confidence:      0.9,
		SourceLabel:     "operator",
	})
	if err != nil {
		t.Fatalf("CreateGoal task: %v", err)
	}
	forest, err := service.GoalForest("alice")
	if err != nil {
		t.Fatalf("GoalForest: %v", err)
	}
	if len(forest) != 1 || len(forest[0].Children) != 1 ||
		len(forest[0].Children[0].Children) != 1 ||
		forest[0].Children[0].Children[0].Goal.ID != task.ID {
		t.Fatalf("unexpected goal forest: %#v", forest)
	}
}

func TestGoalHierarchyRejectsInvalidDirectionAndCycles(t *testing.T) {
	service := NewService(nil)
	root, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity: "alice",
		Level:         GoalLevelValues,
		DomainIDs:     []DomainID{DomainMeaningValues},
		Title:         "Root",
		Confidence:    1,
		SourceLabel:   "operator",
	})
	if err != nil {
		t.Fatalf("CreateGoal root: %v", err)
	}
	child, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity:   "alice",
		ParentID:        &root.ID,
		Level:           GoalLevelPursuit,
		DomainIDs:       []DomainID{DomainWorkVenture},
		Title:           "Child",
		SuccessCriteria: []string{"done"},
		StopConditions:  []string{"stop"},
		Confidence:      1,
		SourceLabel:     "operator",
	})
	if err != nil {
		t.Fatalf("CreateGoal child: %v", err)
	}
	parentID := child.ID
	if _, err := service.UpdateGoal("alice", root.ID, UpdateGoalRequest{ParentID: &parentID}); err == nil ||
		!strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle update returned %v", err)
	}
	wrongLevel := GoalLevelValues
	if _, err := service.UpdateGoal("alice", child.ID, UpdateGoalRequest{Level: &wrongLevel}); err == nil ||
		!strings.Contains(err.Error(), "must be above") {
		t.Fatalf("invalid direction returned %v", err)
	}
	if _, err := service.Goal("bob", root.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("goal owner isolation returned %v", err)
	}
}

func TestPursuitRequiresSuccessAndStopConditions(t *testing.T) {
	service := NewService(nil)
	_, err := service.CreateGoal(CreateGoalRequest{
		OwnerIdentity: "alice",
		Level:         GoalLevelPursuit,
		DomainIDs:     []DomainID{DomainWorkVenture},
		Title:         "Unbounded pursuit",
		Confidence:    0.8,
		SourceLabel:   "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "success criteria") {
		t.Fatalf("unbounded pursuit returned %v", err)
	}
}

func TestPriorityAssessmentUsesAllCriteriaAndExplainsScore(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	highFactors := filledFactors(85)
	highFactors.Effort = 15
	highFactors.Duration = 20
	highFactors.Dependencies = 10
	highFactors.OpportunityCost = 15
	lowFactors := filledFactors(20)
	lowFactors.Effort = 90
	lowFactors.Duration = 90
	lowFactors.Dependencies = 90
	lowFactors.OpportunityCost = 90
	high, err := service.AssessPriority(PriorityAssessmentRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "high",
		Title:         "High value work",
		Deadline:      timePointer(now.Add(12 * time.Hour)),
		Factors:       highFactors,
	})
	if err != nil {
		t.Fatalf("AssessPriority high: %v", err)
	}
	low, err := service.AssessPriority(PriorityAssessmentRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "low",
		Title:         "Low value work",
		Factors:       lowFactors,
	})
	if err != nil {
		t.Fatalf("AssessPriority low: %v", err)
	}
	if high.Score <= low.Score || len(high.Contributions) != 25 || len(high.Reasons) == 0 {
		t.Fatalf("priority assessments high=%#v low=%#v", high, low)
	}
	if high.Factors.DeadlinePressure != 95 || high.AlgorithmVersion != priorityAlgorithmVersion {
		t.Fatalf("deadline/version not applied: %#v", high)
	}
	history, err := service.PriorityHistory("alice", "task", "high", 10)
	if err != nil {
		t.Fatalf("PriorityHistory: %v", err)
	}
	if len(history) != 1 || history[0].ID != high.ID ||
		history[0].SourceLabel != "lifeops:priority_input" {
		t.Fatalf("priority history = %#v", history)
	}
	bobHistory, err := service.PriorityHistory("bob", "", "", 10)
	if err != nil || len(bobHistory) != 0 {
		t.Fatalf("cross-owner priority history = %#v, %v", bobHistory, err)
	}
}

func TestEvaluatePriorityIsPureAndUsesThePersistedAssessmentAlgorithm(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	request := PriorityAssessmentRequest{
		OwnerIdentity: "alice", EntityType: "pursuit", EntityID: "p-1",
		Title: "Compare portfolio work", Factors: filledFactors(70),
	}
	evaluation, err := EvaluatePriority(request, now)
	if err != nil {
		t.Fatalf("EvaluatePriority: %v", err)
	}
	if evaluation.ID != uuid.Nil || evaluation.Score == 0 || len(evaluation.Contributions) != 25 {
		t.Fatalf("pure priority evaluation = %#v", evaluation)
	}
	history, err := repo.PriorityAssessments("alice", "pursuit", "p-1", 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("pure evaluation persisted history: %#v, %v", history, err)
	}

	service := NewService(repo, WithClock(func() time.Time { return now }))
	persisted, err := service.AssessPriority(request)
	if err != nil {
		t.Fatalf("AssessPriority: %v", err)
	}
	if persisted.ID == uuid.Nil || persisted.Score != evaluation.Score || persisted.AlgorithmVersion != evaluation.AlgorithmVersion {
		t.Fatalf("persisted assessment diverged from pure evaluation: persisted=%#v evaluation=%#v", persisted, evaluation)
	}
}

func TestPriorityAssessmentAppliesCapacityWithoutCrossOwnerLeak(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service := NewService(nil, WithClock(func() time.Time { return now }))
	capacity, err := service.RecordCapacity(RecordCapacityRequest{
		OwnerIdentity: "alice",
		Status:        CapacityUnavailable,
		Signals:       CapacitySignals{Energy: 10},
		CurrentLoad:   100,
		SourceLabel:   "operator",
		CapturedAt:    now,
		Confidence:    1,
	})
	if err != nil {
		t.Fatalf("RecordCapacity: %v", err)
	}
	assessment, err := service.AssessPriority(PriorityAssessmentRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "t-1",
		Title:         "Capacity constrained task",
		Factors:       filledFactors(70),
		Capacity:      capacity,
	})
	if err != nil {
		t.Fatalf("AssessPriority: %v", err)
	}
	if !assessment.CapacityApplied || assessment.Factors.AvailableCapacity != 0 || assessment.Factors.EnergyFit != 0 {
		t.Fatalf("capacity was not applied: %#v", assessment)
	}
	capacity.OwnerIdentity = "bob"
	if _, err := service.AssessPriority(PriorityAssessmentRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "t-2",
		Title:         "Mismatched capacity",
		Factors:       filledFactors(50),
		Capacity:      capacity,
	}); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("cross-owner capacity returned %v", err)
	}
}

func TestMemoryRepositoryReturnsDefensiveCopies(t *testing.T) {
	service := NewService(nil)
	link, err := service.LinkEntity(LinkEntityRequest{
		OwnerIdentity: "alice",
		EntityType:    "task",
		EntityID:      "t-1",
		DomainID:      DomainWorkVenture,
		Confidence:    1,
		SourceLabel:   "operator",
		Evidence:      []string{"original"},
	})
	if err != nil {
		t.Fatalf("LinkEntity: %v", err)
	}
	link.Evidence[0] = "mutated"
	links, err := service.EntityDomains("alice", "task", "t-1")
	if err != nil {
		t.Fatalf("EntityDomains: %v", err)
	}
	if links[0].Evidence[0] != "original" {
		t.Fatalf("repository leaked mutable link: %#v", links[0])
	}
}

func filledFactors(value int) PriorityFactors {
	return PriorityFactors{
		Importance:               value,
		Urgency:                  value,
		HumanNeedAffected:        value,
		DeadlinePressure:         value,
		CostOfDelay:              value,
		ExpectedValue:            value,
		HarmAvoided:              value,
		ProbabilityOfSuccess:     value,
		Effort:                   value,
		Duration:                 value,
		Dependencies:             value,
		Reversibility:            value,
		Risk:                     value,
		LegalObligation:          value,
		RelationshipConsequences: value,
		AvailableCapacity:        value,
		EnergyFit:                value,
		OpportunityCost:          value,
		StrategicAlignment:       value,
		LearningValue:            value,
		CompoundingValue:         value,
		Staleness:                value,
		CommitmentAge:            value,
		PeopleBlocked:            value,
		Delegability:             value,
	}
}

func contains(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func TestLatestCapacityReturnsNotFound(t *testing.T) {
	service := NewService(nil)
	_, err := service.LatestCapacity("alice")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestCapacity without data returned %v", err)
	}
}

func TestGoalLookupRejectsNilID(t *testing.T) {
	service := NewService(nil)
	if _, err := service.Goal("alice", uuid.Nil); err == nil {
		t.Fatal("nil goal id was accepted")
	}
}

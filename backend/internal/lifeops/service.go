package lifeops

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	capacityFreshnessWindow   = 24 * time.Hour
	priorityAlgorithmVersion  = "lifeops-mcda-v1"
	defaultVerificationStatus = "needs_review"
)

type Service struct {
	repo Repository
	now  func() time.Time
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(service *Service) {
		if clock != nil {
			service.now = clock
		}
	}
}

func NewService(repo Repository, options ...Option) *Service {
	if repo == nil {
		repo = NewMemoryRepository()
	}
	service := &Service{repo: repo, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Domains() []LifeDomain {
	return CanonicalLifeDomains()
}

func (s *Service) Domain(id DomainID) (LifeDomain, error) {
	domain, ok := FindLifeDomain(id)
	if !ok {
		return LifeDomain{}, fmt.Errorf("unknown life domain %q", id)
	}
	return domain, nil
}

func (s *Service) LinkEntity(request LinkEntityRequest) (*EntityDomainLink, error) {
	request.OwnerIdentity = normalize(request.OwnerIdentity)
	request.EntityType = normalizeIdentifier(request.EntityType)
	request.EntityID = normalize(request.EntityID)
	request.SourceLabel = normalize(request.SourceLabel)
	request.SourceURI = normalize(request.SourceURI)
	request.Evidence = cleanStrings(request.Evidence)
	request.VerificationStatus = normalizeIdentifier(request.VerificationStatus)
	if request.VerificationStatus == "" {
		request.VerificationStatus = defaultVerificationStatus
	}
	if err := validateLinkRequest(request); err != nil {
		return nil, err
	}

	links, err := s.repo.EntityDomainLinks(request.OwnerIdentity, request.EntityType, request.EntityID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var result EntityDomainLink
	found := false
	for index := range links {
		if request.Primary {
			links[index].Primary = false
		}
		if links[index].DomainID != request.DomainID {
			continue
		}
		links[index].Primary = request.Primary
		links[index].Confidence = request.Confidence
		links[index].SourceLabel = request.SourceLabel
		links[index].SourceURI = request.SourceURI
		links[index].Evidence = append([]string(nil), request.Evidence...)
		links[index].VerificationStatus = request.VerificationStatus
		links[index].UpdatedAt = now
		result = links[index]
		found = true
	}
	if !found {
		result = EntityDomainLink{
			ID:                 uuid.New(),
			OwnerIdentity:      request.OwnerIdentity,
			EntityType:         request.EntityType,
			EntityID:           request.EntityID,
			DomainID:           request.DomainID,
			Primary:            request.Primary,
			Confidence:         request.Confidence,
			SourceLabel:        request.SourceLabel,
			SourceURI:          request.SourceURI,
			Evidence:           append([]string(nil), request.Evidence...),
			VerificationStatus: request.VerificationStatus,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		links = append(links, result)
	}
	if !hasPrimaryLink(links) {
		for index := range links {
			if links[index].DomainID == request.DomainID {
				links[index].Primary = true
				result.Primary = true
				break
			}
		}
	}
	sortLinks(links)
	if err := s.repo.ReplaceEntityDomainLinks(request.OwnerIdentity, request.EntityType, request.EntityID, links); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) EntityDomains(ownerIdentity, entityType, entityID string) ([]EntityDomainLink, error) {
	ownerIdentity = normalize(ownerIdentity)
	entityType = normalizeIdentifier(entityType)
	entityID = normalize(entityID)
	if ownerIdentity == "" || entityType == "" || entityID == "" {
		return nil, fmt.Errorf("owner identity, entity type, and entity id are required")
	}
	links, err := s.repo.EntityDomainLinks(ownerIdentity, entityType, entityID)
	if err != nil {
		return nil, err
	}
	sortLinks(links)
	return links, nil
}

func (s *Service) RecordNeed(request RecordNeedRequest) (*NeedObservation, error) {
	request.OwnerIdentity = normalize(request.OwnerIdentity)
	request.NeedLevel = normalizeIdentifier(request.NeedLevel)
	request.State = normalizeIdentifier(request.State)
	request.SourceLabel = normalize(request.SourceLabel)
	request.SourceURI = normalize(request.SourceURI)
	request.Evidence = cleanStrings(request.Evidence)
	if err := validateNeedRequest(request, s.now().UTC()); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	observation := NeedObservation{
		ID:            uuid.New(),
		OwnerIdentity: request.OwnerIdentity,
		DomainID:      request.DomainID,
		NeedLevel:     request.NeedLevel,
		State:         request.State,
		CurrentLevel:  request.CurrentLevel,
		TargetLevel:   request.TargetLevel,
		Gap:           maxInt(request.TargetLevel-request.CurrentLevel, 0),
		Priority:      request.Priority,
		Confidence:    request.Confidence,
		Evidence:      append([]string(nil), request.Evidence...),
		SourceLabel:   request.SourceLabel,
		SourceURI:     request.SourceURI,
		ObservedAt:    observedAt,
		ExpiresAt:     utcTimePointer(request.ExpiresAt),
		NeedsReview:   request.NeedsReview,
		CreatedAt:     now,
	}
	if err := s.repo.SaveNeedObservation(observation); err != nil {
		return nil, err
	}
	return &observation, nil
}

func (s *Service) Needs(ownerIdentity string, domainID DomainID, limit int) ([]NeedObservation, error) {
	ownerIdentity = normalize(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if domainID != "" && !IsCanonicalDomain(domainID) {
		return nil, fmt.Errorf("unknown life domain %q", domainID)
	}
	if limit < 0 || limit > 500 {
		return nil, fmt.Errorf("need observation limit must be between 0 and 500")
	}
	return s.repo.NeedObservations(ownerIdentity, domainID, limit)
}

func (s *Service) RecordCapacity(request RecordCapacityRequest) (*CapacitySnapshot, error) {
	request.OwnerIdentity = normalize(request.OwnerIdentity)
	request.Status = normalizeIdentifier(request.Status)
	request.SourceLabel = normalize(request.SourceLabel)
	request.SourceURI = normalize(request.SourceURI)
	request.Constraints = cleanStrings(request.Constraints)
	request.Signals.AvailableTools = cleanStrings(request.Signals.AvailableTools)
	request.Signals.AvailableHelpers = cleanStrings(request.Signals.AvailableHelpers)
	if err := validateCapacityRequest(request, s.now().UTC()); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	capturedAt := request.CapturedAt.UTC()
	fresh := now.Sub(capturedAt) <= capacityFreshnessWindow
	needsReview := request.NeedsReview || !fresh || request.Confidence < 0.6 || request.Status == CapacityUnknown
	constraints := append([]string(nil), request.Constraints...)
	if !fresh {
		constraints = cleanStrings(append(constraints, "capacity snapshot is older than 24 hours"))
	}
	planningStepLimit := request.PlanningStepLimit
	if planningStepLimit == 0 {
		planningStepLimit = derivePlanningStepLimit(request.Status, request.Signals, request.TimeAvailableMinutes)
	}
	snapshot := CapacitySnapshot{
		ID:                   uuid.New(),
		OwnerIdentity:        request.OwnerIdentity,
		Status:               request.Status,
		Signals:              request.Signals,
		TimeAvailableMinutes: request.TimeAvailableMinutes,
		ConcurrentWorkLimit:  request.ConcurrentWorkLimit,
		CurrentLoad:          request.CurrentLoad,
		PlanningStepLimit:    planningStepLimit,
		Constraints:          constraints,
		SourceLabel:          request.SourceLabel,
		SourceURI:            request.SourceURI,
		CapturedAt:           capturedAt,
		Confidence:           request.Confidence,
		Fresh:                fresh,
		NeedsReview:          needsReview,
		CreatedAt:            now,
	}
	if err := s.repo.SaveCapacitySnapshot(snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *Service) CapacityHistory(ownerIdentity string, limit int) ([]CapacitySnapshot, error) {
	ownerIdentity = normalize(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if limit < 0 || limit > 500 {
		return nil, fmt.Errorf("capacity history limit must be between 0 and 500")
	}
	return s.repo.CapacitySnapshots(ownerIdentity, limit)
}

func (s *Service) LatestCapacity(ownerIdentity string) (*CapacitySnapshot, error) {
	items, err := s.CapacityHistory(ownerIdentity, 1)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return &items[0], nil
}

func (s *Service) CreateGoal(request CreateGoalRequest) (*GoalNode, error) {
	now := s.now().UTC()
	goal := GoalNode{
		ID:              uuid.New(),
		OwnerIdentity:   normalize(request.OwnerIdentity),
		ParentID:        cloneUUIDPointer(request.ParentID),
		Level:           request.Level,
		DomainIDs:       cleanDomains(request.DomainIDs),
		Title:           normalize(request.Title),
		Description:     normalize(request.Description),
		SuccessCriteria: cleanStrings(request.SuccessCriteria),
		StopConditions:  cleanStrings(request.StopConditions),
		Status:          normalizeIdentifier(request.Status),
		Confidence:      request.Confidence,
		SourceLabel:     normalize(request.SourceLabel),
		SourceURI:       normalize(request.SourceURI),
		TargetAt:        utcTimePointer(request.TargetAt),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if goal.Status == "" {
		goal.Status = "active"
	}
	if err := s.validateGoal(goal); err != nil {
		return nil, err
	}
	if err := s.repo.SaveGoal(goal); err != nil {
		return nil, err
	}
	return &goal, nil
}

func (s *Service) UpdateGoal(ownerIdentity string, id uuid.UUID, request UpdateGoalRequest) (*GoalNode, error) {
	ownerIdentity = normalize(ownerIdentity)
	if ownerIdentity == "" || id == uuid.Nil {
		return nil, fmt.Errorf("owner identity and goal id are required")
	}
	goal, err := s.repo.FindGoal(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	if request.ClearParent {
		goal.ParentID = nil
	} else if request.ParentID != nil {
		goal.ParentID = cloneUUIDPointer(request.ParentID)
	}
	if request.Level != nil {
		goal.Level = *request.Level
	}
	if request.DomainIDs != nil {
		goal.DomainIDs = cleanDomains(request.DomainIDs)
	}
	if request.Title != nil {
		goal.Title = normalize(*request.Title)
	}
	if request.Description != nil {
		goal.Description = normalize(*request.Description)
	}
	if request.SuccessCriteria != nil {
		goal.SuccessCriteria = cleanStrings(request.SuccessCriteria)
	}
	if request.StopConditions != nil {
		goal.StopConditions = cleanStrings(request.StopConditions)
	}
	if request.Status != nil {
		goal.Status = normalizeIdentifier(*request.Status)
	}
	if request.Confidence != nil {
		goal.Confidence = *request.Confidence
	}
	if request.SourceLabel != nil {
		goal.SourceLabel = normalize(*request.SourceLabel)
	}
	if request.SourceURI != nil {
		goal.SourceURI = normalize(*request.SourceURI)
	}
	if request.ClearTarget {
		goal.TargetAt = nil
	} else if request.TargetAt != nil {
		goal.TargetAt = utcTimePointer(request.TargetAt)
	}
	goal.UpdatedAt = s.now().UTC()
	if err := s.validateGoal(*goal); err != nil {
		return nil, err
	}
	if err := s.repo.SaveGoal(*goal); err != nil {
		return nil, err
	}
	return goal, nil
}

func (s *Service) Goal(ownerIdentity string, id uuid.UUID) (*GoalNode, error) {
	ownerIdentity = normalize(ownerIdentity)
	if ownerIdentity == "" || id == uuid.Nil {
		return nil, fmt.Errorf("owner identity and goal id are required")
	}
	return s.repo.FindGoal(ownerIdentity, id)
}

func (s *Service) Goals(ownerIdentity string) ([]GoalNode, error) {
	ownerIdentity = normalize(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	return s.repo.ListGoals(ownerIdentity)
}

func (s *Service) GoalForest(ownerIdentity string) ([]GoalTreeNode, error) {
	goals, err := s.Goals(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return GoalForestFromGoals(goals), nil
}

// GoalForestFromGoals builds the hierarchy from an already owner-scoped goal
// list. Overview callers reuse their loaded list instead of issuing a second
// repository read solely to render the same tree.
func GoalForestFromGoals(goals []GoalNode) []GoalTreeNode {
	byParent := make(map[uuid.UUID][]GoalNode)
	roots := make([]GoalNode, 0)
	for _, goal := range goals {
		if goal.ParentID == nil {
			roots = append(roots, goal)
			continue
		}
		byParent[*goal.ParentID] = append(byParent[*goal.ParentID], goal)
	}
	var build func(GoalNode) GoalTreeNode
	build = func(goal GoalNode) GoalTreeNode {
		node := GoalTreeNode{Goal: goal, Children: []GoalTreeNode{}}
		for _, child := range byParent[goal.ID] {
			node.Children = append(node.Children, build(child))
		}
		return node
	}
	result := make([]GoalTreeNode, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}
	return result
}

func (s *Service) AssessPriority(request PriorityAssessmentRequest) (*PriorityAssessment, error) {
	assessment, err := EvaluatePriority(request, s.now().UTC())
	if err != nil {
		return nil, err
	}
	assessment.ID = uuid.New()
	if err := s.repo.SavePriorityAssessment(*assessment); err != nil {
		return nil, err
	}
	return assessment, nil
}

// EvaluatePriority applies the same deterministic multi-criteria model used by
// persisted LifeOps assessments without writing state. Advisory planners use
// it to compare current options before an operator accepts any change.
func EvaluatePriority(request PriorityAssessmentRequest, assessedAt time.Time) (*PriorityAssessment, error) {
	request.OwnerIdentity = normalize(request.OwnerIdentity)
	request.EntityType = normalizeIdentifier(request.EntityType)
	request.EntityID = normalize(request.EntityID)
	request.Title = normalize(request.Title)
	request.SourceLabel = normalize(request.SourceLabel)
	request.SourceURI = normalize(request.SourceURI)
	if request.SourceLabel == "" {
		request.SourceLabel = "lifeops:priority_input"
	}
	assessedAt = assessedAt.UTC()
	if assessedAt.IsZero() {
		return nil, fmt.Errorf("assessment time is required")
	}
	if err := validatePriorityRequest(request, assessedAt); err != nil {
		return nil, err
	}

	factors := request.Factors
	reasons := make([]string, 0)
	capacityApplied := false
	if request.Deadline != nil {
		derived := deadlinePressure(assessedAt, request.Deadline.UTC())
		if derived > factors.DeadlinePressure {
			factors.DeadlinePressure = derived
			reasons = append(reasons, fmt.Sprintf("deadline raises pressure to %d/100", derived))
		}
	}
	if request.Capacity != nil {
		capacity, energy := capacityFactors(*request.Capacity)
		factors.AvailableCapacity = capacity
		factors.EnergyFit = energy
		capacityApplied = true
		reasons = append(reasons, fmt.Sprintf("capacity snapshot applies %d/100 capacity and %d/100 energy fit", capacity, energy))
	}

	contributions, weightedTotal, weightTotal := priorityContributions(factors)
	score := int(math.Round(weightedTotal / weightTotal))
	score = clampInt(score, 0, 100)
	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].Contribution > contributions[j].Contribution
	})
	for _, contribution := range contributions {
		if len(reasons) >= 6 || contribution.Contribution < 4 {
			break
		}
		reasons = append(reasons, contribution.Reason)
	}
	assessment := PriorityAssessment{
		OwnerIdentity:    request.OwnerIdentity,
		EntityType:       request.EntityType,
		EntityID:         request.EntityID,
		Title:            request.Title,
		Score:            score,
		Band:             priorityBand(score),
		Factors:          factors,
		Contributions:    contributions,
		Reasons:          cleanStrings(reasons),
		CapacityApplied:  capacityApplied,
		AlgorithmVersion: priorityAlgorithmVersion,
		SourceLabel:      request.SourceLabel,
		SourceURI:        request.SourceURI,
		AssessedAt:       assessedAt,
	}
	return &assessment, nil
}

func (s *Service) PriorityHistory(ownerIdentity, entityType, entityID string, limit int) ([]PriorityAssessment, error) {
	ownerIdentity = normalize(ownerIdentity)
	entityType = normalizeIdentifier(entityType)
	entityID = normalize(entityID)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if (entityType == "") != (entityID == "") {
		return nil, fmt.Errorf("entity type and entity id must be supplied together")
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("priority history limit must be between 1 and 500")
	}
	return s.repo.PriorityAssessments(ownerIdentity, entityType, entityID, limit)
}

func (s *Service) validateGoal(goal GoalNode) error {
	if err := validateGoalFields(goal); err != nil {
		return err
	}
	goals, err := s.repo.ListGoals(goal.OwnerIdentity)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]GoalNode, len(goals))
	for _, existing := range goals {
		byID[existing.ID] = existing
	}
	byID[goal.ID] = goal
	visited := map[uuid.UUID]bool{}
	current := goal
	for current.ParentID != nil {
		if *current.ParentID == goal.ID {
			return fmt.Errorf("goal hierarchy cycle detected")
		}
		if visited[*current.ParentID] {
			return fmt.Errorf("goal hierarchy cycle detected")
		}
		visited[*current.ParentID] = true
		parent, ok := byID[*current.ParentID]
		if !ok {
			return fmt.Errorf("parent goal %s: %w", current.ParentID.String(), ErrNotFound)
		}
		current = parent
	}
	if goal.ParentID != nil {
		parent, ok := byID[*goal.ParentID]
		if !ok {
			return fmt.Errorf("parent goal %s: %w", goal.ParentID.String(), ErrNotFound)
		}
		parentRank, _ := GoalLevelRank(parent.Level)
		childRank, _ := GoalLevelRank(goal.Level)
		if parentRank >= childRank {
			return fmt.Errorf("parent goal level %q must be above child level %q", parent.Level, goal.Level)
		}
	}
	return nil
}

func priorityContributions(factors PriorityFactors) ([]FactorContribution, float64, float64) {
	type factorSpec struct {
		name   string
		value  int
		weight float64
		cost   bool
	}
	specs := []factorSpec{
		{"importance", factors.Importance, 9, false},
		{"urgency", factors.Urgency, 8, false},
		{"humanNeedAffected", factors.HumanNeedAffected, 7, false},
		{"deadlinePressure", factors.DeadlinePressure, 7, false},
		{"costOfDelay", factors.CostOfDelay, 6, false},
		{"expectedValue", factors.ExpectedValue, 5, false},
		{"harmAvoided", factors.HarmAvoided, 6, false},
		{"probabilityOfSuccess", factors.ProbabilityOfSuccess, 4, false},
		{"effort", factors.Effort, 5, true},
		{"duration", factors.Duration, 3, true},
		{"dependencies", factors.Dependencies, 3, true},
		{"reversibility", factors.Reversibility, 2, false},
		{"risk", factors.Risk, 5, false},
		{"legalObligation", factors.LegalObligation, 5, false},
		{"relationshipConsequences", factors.RelationshipConsequences, 3, false},
		{"availableCapacity", factors.AvailableCapacity, 4, false},
		{"energyFit", factors.EnergyFit, 4, false},
		{"opportunityCost", factors.OpportunityCost, 3, true},
		{"strategicAlignment", factors.StrategicAlignment, 5, false},
		{"learningValue", factors.LearningValue, 2, false},
		{"compoundingValue", factors.CompoundingValue, 3, false},
		{"staleness", factors.Staleness, 2, false},
		{"commitmentAge", factors.CommitmentAge, 1, false},
		{"peopleBlocked", factors.PeopleBlocked, 2, false},
		{"delegability", factors.Delegability, 1, false},
	}
	result := make([]FactorContribution, 0, len(specs))
	weightedTotal := 0.0
	weightTotal := 0.0
	for _, spec := range specs {
		effective := spec.value
		if spec.cost {
			effective = 100 - spec.value
		}
		contribution := float64(effective) * spec.weight
		weightedTotal += contribution
		weightTotal += spec.weight
		reason := fmt.Sprintf("%s contributes %.1f weighted points", spec.name, contribution/100)
		if spec.cost {
			reason = fmt.Sprintf("%s cost leaves %.1f weighted points", spec.name, contribution/100)
		}
		result = append(result, FactorContribution{
			Factor:         spec.name,
			Input:          spec.value,
			EffectiveInput: effective,
			Weight:         spec.weight,
			Contribution:   contribution / 100,
			CostFactor:     spec.cost,
			Reason:         reason,
		})
	}
	return result, weightedTotal, weightTotal
}

func capacityFactors(snapshot CapacitySnapshot) (int, int) {
	if !snapshot.Fresh || snapshot.NeedsReview {
		return minInt(30, maxInt(0, 100-snapshot.CurrentLoad)), minInt(30, snapshot.Signals.Energy)
	}
	switch snapshot.Status {
	case CapacityUnavailable:
		return 0, 0
	case CapacityOverloaded:
		return minInt(20, 100-snapshot.CurrentLoad), minInt(25, snapshot.Signals.Energy)
	case CapacityConstrained, CapacityRecovering:
		return minInt(50, maxInt(0, 100-snapshot.CurrentLoad)), minInt(50, snapshot.Signals.Energy)
	default:
		available := averageKnown(100-snapshot.CurrentLoad, snapshot.Signals.AttentionQuality, snapshot.Signals.ConfidenceReadiness)
		return clampInt(available, 0, 100), snapshot.Signals.Energy
	}
}

func deadlinePressure(now, deadline time.Time) int {
	remaining := deadline.Sub(now)
	switch {
	case remaining <= 0:
		return 100
	case remaining <= 24*time.Hour:
		return 95
	case remaining <= 3*24*time.Hour:
		return 85
	case remaining <= 7*24*time.Hour:
		return 70
	case remaining <= 14*24*time.Hour:
		return 55
	case remaining <= 30*24*time.Hour:
		return 35
	default:
		return 15
	}
}

func priorityBand(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 65:
		return "high"
	case score >= 45:
		return "medium"
	case score >= 25:
		return "low"
	default:
		return "defer"
	}
}

func derivePlanningStepLimit(status string, signals CapacitySignals, timeAvailableMinutes int) int {
	switch status {
	case CapacityUnavailable:
		return 1
	case CapacityOverloaded:
		return 2
	case CapacityConstrained, CapacityRecovering:
		return 3
	}
	if signals.Energy > 0 && signals.Energy < 35 {
		return 3
	}
	if timeAvailableMinutes > 0 && timeAvailableMinutes < 30 {
		return 3
	}
	return 8
}

func averageKnown(values ...int) int {
	total := 0
	count := 0
	for _, value := range values {
		if value < 0 {
			continue
		}
		total += value
		count++
	}
	if count == 0 {
		return 0
	}
	return total / count
}

func sortLinks(links []EntityDomainLink) {
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Primary != links[j].Primary {
			return links[i].Primary
		}
		return links[i].DomainID < links[j].DomainID
	})
}

func hasPrimaryLink(links []EntityDomainLink) bool {
	for _, link := range links {
		if link.Primary {
			return true
		}
	}
	return false
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func cloneUUIDPointer(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func normalize(value string) string {
	return strings.TrimSpace(value)
}

func normalizeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cleanDomains(values []DomainID) []DomainID {
	seen := map[DomainID]bool{}
	result := make([]DomainID, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

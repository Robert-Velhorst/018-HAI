package pursuit

import (
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// dashboardDetailsForOwner expands every active pursuit from one bounded set
// of repository reads. It returns false when the repository does not implement
// the optimized contract so callers can retain the proven detail path.
func (s *service) dashboardDetailsForOwner(ownerIdentity string, pursuits []models.Pursuit) (map[uuid.UUID]*PursuitDetail, bool, error) {
	bulk, ok := s.repo.(pursuitDashboardBulkRepository)
	if !ok {
		return nil, false, nil
	}
	active := make([]models.Pursuit, 0, len(pursuits))
	ids := make([]uuid.UUID, 0, len(pursuits))
	for _, pursuit := range pursuits {
		if pursuitClosed(pursuit) {
			continue
		}
		active = append(active, pursuit)
		ids = append(ids, pursuit.ID)
	}
	result := make(map[uuid.UUID]*PursuitDetail, len(active))
	if len(active) == 0 {
		return result, true, nil
	}

	links, err := bulk.FindVisibleLinksForPursuits(ownerIdentity, ids)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard links", err)
	}
	activity, err := bulk.FindActivitiesForPursuits(ids, 50)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard activity", err)
	}
	taskAttempts, err := bulk.FindTaskAttemptsForPursuits(ownerIdentity, ids, 20)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard task attempts", err)
	}

	workflowIDs := linkUUIDs(links, LinkWorkflow)
	memoryIDs := linkUUIDs(links, LinkMemory)
	conversationIDs := linkUUIDs(links, LinkAIConversation)
	ambientIDs := linkUUIDs(links, LinkAmbientOpportunity)
	sourceItemIDs := linkUUIDs(links, LinkSourceItem)
	extractionIDs := linkUUIDs(links, LinkSourceExtraction)
	verificationIDs := linkUUIDs(links, LinkVerification)
	linkedAutomationIDs := linkUUIDs(links, LinkAutomation)
	linkedRuntimeIDs := linkUUIDs(links, LinkAgentRuntime)

	workflows, err := s.repo.FindLinkedWorkflows(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard workflows", err)
	}
	checklist, err := s.repo.FindLinkedChecklistItems(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard checklist", err)
	}
	openLoops, err := s.repo.FindLinkedOpenLoops(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard open loops", err)
	}
	proposals, err := s.repo.FindLinkedProposals(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard proposals", err)
	}
	qualityGates, err := s.repo.FindLinkedQualityGates(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard quality gates", err)
	}
	decisions, err := s.repo.FindLinkedDecisions(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard decisions", err)
	}
	transitions, err := s.repo.FindLinkedTransitions(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard transitions", err)
	}
	sourceLinks, err := s.repo.FindLinkedSourceLinks(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard source links", err)
	}
	events, err := s.repo.FindLinkedEvents(workflowIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard events", err)
	}
	conversations, err := s.repo.FindLinkedConversations(conversationIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard conversations", err)
	}
	ambient, err := s.repo.FindLinkedAmbientOpportunities(ambientIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard ambient opportunities", err)
	}
	verificationRuns, err := s.repo.FindLinkedVerificationRuns(verificationIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard verification runs", err)
	}
	allVerificationRunIDs := verificationRunIDs(verificationRuns)
	evidenceProjection, err := bulk.FindEvidenceProjectionForDashboard(workflowIDs, memoryIDs, sourceItemIDs, extractionIDs, allVerificationRunIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard evidence projection", err)
	}
	evidence := evidenceProjection.WorkflowEvidence
	memories := evidenceProjection.Memories
	sourceItems := evidenceProjection.SourceItems
	extractions := evidenceProjection.SourceExtractions
	verificationClaims := evidenceProjection.VerificationClaims
	verificationEvidence := evidenceProjection.VerificationEvidence
	automationIDs := uniqueUUIDs(append(linkedAutomationIDs, workflowAutomationIDs(workflows)...))
	automations, err := s.repo.FindLinkedAutomations(automationIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard automations", err)
	}
	runtimeAttempts, err := bulk.FindRuntimeAttemptsForOwner(ownerIdentity, automationIDs, linkedRuntimeIDs)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard runtime attempts", err)
	}
	resources, err := bulk.FindResourceProjectionForPursuits(ownerIdentity, active)
	if err != nil {
		return nil, true, pursuitDetailLoadError("dashboard resource ledger", err)
	}

	linksByPursuit := groupRecords(links, func(item models.PursuitLink) uuid.UUID { return item.PursuitID })
	activityByPursuit := groupRecords(activity, func(item models.PursuitActivity) uuid.UUID { return item.PursuitID })
	taskAttemptsByPursuit := groupRecords(taskAttempts, func(item models.PursuitTaskAttempt) uuid.UUID { return item.PursuitID })

	workflowOwners := ownersFromLinks(links, LinkWorkflow)
	workflowsByPursuit := partitionRecords(workflows, workflowOwners, func(item models.WorkflowItem) uuid.UUID { return item.ID })
	checklistByPursuit := partitionRecords(checklist, workflowOwners, func(item models.WorkflowChecklistItem) uuid.UUID { return item.WorkflowID })
	openLoopsByPursuit := partitionRecords(openLoops, workflowOwners, func(item models.WorkflowOpenLoop) uuid.UUID { return item.WorkflowID })
	proposalsByPursuit := partitionRecords(proposals, workflowOwners, func(item models.WorkflowProposal) uuid.UUID { return item.WorkflowID })
	qualityGatesByPursuit := partitionRecords(qualityGates, workflowOwners, func(item models.WorkflowQualityGate) uuid.UUID { return item.WorkflowID })
	decisionsByPursuit := partitionRecords(decisions, workflowOwners, func(item models.WorkflowDecision) uuid.UUID { return item.WorkflowID })
	transitionsByPursuit := partitionRecords(transitions, workflowOwners, func(item models.WorkflowTransition) uuid.UUID { return item.WorkflowID })
	sourceLinksByPursuit := partitionRecords(sourceLinks, workflowOwners, func(item models.WorkflowSourceLink) uuid.UUID { return item.WorkflowID })
	eventsByPursuit := partitionRecords(events, workflowOwners, func(item models.WorkflowEvent) uuid.UUID { return item.WorkflowID })
	evidenceByPursuit := partitionRecords(evidence, workflowOwners, func(item models.WorkflowEvidenceClaim) uuid.UUID { return item.WorkflowID })

	memoryOwners := ownersFromLinks(links, LinkMemory)
	memoriesByPursuit := partitionRecords(memories, memoryOwners, func(item models.ContextMemory) uuid.UUID { return item.ID })
	conversationOwners := ownersFromLinks(links, LinkAIConversation)
	conversationsByPursuit := partitionRecords(conversations, conversationOwners, func(item models.AIConversationArchive) uuid.UUID { return item.ID })
	ambientOwners := ownersFromLinks(links, LinkAmbientOpportunity)
	ambientByPursuit := partitionRecords(ambient, ambientOwners, func(item models.AmbientOpportunity) uuid.UUID { return item.ID })
	sourceItemOwners := ownersFromLinks(links, LinkSourceItem)
	sourceItemsByPursuit := partitionRecords(sourceItems, sourceItemOwners, func(item models.SourceRawItem) uuid.UUID { return item.ID })
	extractionOwners := ownersFromLinks(links, LinkSourceExtraction)
	extractionsByPursuit := partitionRecords(extractions, extractionOwners, func(item models.SourceExtraction) uuid.UUID { return item.ID })

	verificationOwners := ownersFromLinks(links, LinkVerification)
	verificationRunsByPursuit := partitionRecords(verificationRuns, verificationOwners, func(item models.VerificationRun) uuid.UUID { return item.ID })
	verificationClaimsByPursuit := partitionRecords(verificationClaims, verificationOwners, func(item models.VerificationClaim) uuid.UUID { return item.RunID })
	verificationEvidenceByPursuit := partitionRecords(verificationEvidence, verificationOwners, func(item models.VerificationEvidence) uuid.UUID { return item.RunID })

	automationOwners := ownersFromLinks(links, LinkAutomation)
	for _, item := range workflows {
		automationID, parseErr := uuid.Parse(strings.TrimSpace(item.AutomationID))
		if parseErr != nil {
			continue
		}
		for _, pursuitID := range workflowOwners[item.ID] {
			addRecordOwner(automationOwners, automationID, pursuitID)
		}
	}
	automationsByPursuit := partitionRecords(automations, automationOwners, func(item models.Automation) uuid.UUID { return item.ID })
	runtimeAttemptsByPursuit := partitionRuntimeAttempts(runtimeAttempts, automationOwners, ownersFromLinks(links, LinkAgentRuntime), 20)

	for _, pursuit := range active {
		usage := dashboardResourceUsage(ownerIdentity, pursuit, resources)
		records := pursuitDetailRecords{
			Links:                recordsFor(linksByPursuit, pursuit.ID),
			Activity:             recordsFor(activityByPursuit, pursuit.ID),
			TaskAttempts:         recordsFor(taskAttemptsByPursuit, pursuit.ID),
			Workflows:            recordsFor(workflowsByPursuit, pursuit.ID),
			ChecklistItems:       recordsFor(checklistByPursuit, pursuit.ID),
			OpenLoops:            recordsFor(openLoopsByPursuit, pursuit.ID),
			Proposals:            recordsFor(proposalsByPursuit, pursuit.ID),
			QualityGates:         recordsFor(qualityGatesByPursuit, pursuit.ID),
			Decisions:            recordsFor(decisionsByPursuit, pursuit.ID),
			Transitions:          recordsFor(transitionsByPursuit, pursuit.ID),
			SourceLinks:          recordsFor(sourceLinksByPursuit, pursuit.ID),
			Events:               recordsFor(eventsByPursuit, pursuit.ID),
			Evidence:             recordsFor(evidenceByPursuit, pursuit.ID),
			Memories:             recordsFor(memoriesByPursuit, pursuit.ID),
			Conversations:        recordsFor(conversationsByPursuit, pursuit.ID),
			AmbientOpportunities: recordsFor(ambientByPursuit, pursuit.ID),
			SourceItems:          recordsFor(sourceItemsByPursuit, pursuit.ID),
			SourceExtractions:    recordsFor(extractionsByPursuit, pursuit.ID),
			VerificationRuns:     recordsFor(verificationRunsByPursuit, pursuit.ID),
			VerificationClaims:   recordsFor(verificationClaimsByPursuit, pursuit.ID),
			VerificationEvidence: recordsFor(verificationEvidenceByPursuit, pursuit.ID),
			Automations:          recordsFor(automationsByPursuit, pursuit.ID),
			RuntimeAttempts:      recordsFor(runtimeAttemptsByPursuit, pursuit.ID),
			ResourceUsage:        usage,
		}
		result[pursuit.ID] = s.buildPursuitDetail(pursuit, records)
	}
	return result, true, nil
}

type recordOwners map[uuid.UUID][]uuid.UUID

func ownersFromLinks(links []models.PursuitLink, linkType string) recordOwners {
	result := recordOwners{}
	for _, link := range links {
		if link.LinkType != linkType {
			continue
		}
		recordID, err := uuid.Parse(link.LinkID)
		if err != nil {
			continue
		}
		addRecordOwner(result, recordID, link.PursuitID)
	}
	return result
}

func addRecordOwner(owners recordOwners, recordID, pursuitID uuid.UUID) {
	if recordID == uuid.Nil || pursuitID == uuid.Nil {
		return
	}
	for _, existing := range owners[recordID] {
		if existing == pursuitID {
			return
		}
	}
	owners[recordID] = append(owners[recordID], pursuitID)
}

func groupRecords[T any](items []T, ownerID func(T) uuid.UUID) map[uuid.UUID][]T {
	result := map[uuid.UUID][]T{}
	for _, item := range items {
		id := ownerID(item)
		if id == uuid.Nil {
			continue
		}
		result[id] = append(result[id], item)
	}
	return result
}

func partitionRecords[T any](items []T, owners recordOwners, recordID func(T) uuid.UUID) map[uuid.UUID][]T {
	result := map[uuid.UUID][]T{}
	for _, item := range items {
		for _, pursuitID := range owners[recordID(item)] {
			result[pursuitID] = append(result[pursuitID], item)
		}
	}
	return result
}

func recordsFor[T any](records map[uuid.UUID][]T, pursuitID uuid.UUID) []T {
	if items, ok := records[pursuitID]; ok {
		return items
	}
	return []T{}
}

func partitionRuntimeAttempts(items []models.AutomationLaunchEvent, automationOwners, launchOwners recordOwners, limit int) map[uuid.UUID][]models.AutomationLaunchEvent {
	result := map[uuid.UUID][]models.AutomationLaunchEvent{}
	for _, item := range items {
		automationPursuits := automationOwners[item.AutomationID]
		for _, pursuitID := range automationPursuits {
			if limit > 0 && len(result[pursuitID]) >= limit {
				continue
			}
			result[pursuitID] = append(result[pursuitID], item)
		}
		for _, pursuitID := range launchOwners[item.ID] {
			if containsUUID(automationPursuits, pursuitID) || (limit > 0 && len(result[pursuitID]) >= limit) {
				continue
			}
			result[pursuitID] = append(result[pursuitID], item)
		}
	}
	return result
}

func containsUUID(ids []uuid.UUID, wanted uuid.UUID) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}

func dashboardResourceUsage(ownerIdentity string, pursuit models.Pursuit, projection pursuitDashboardResourceProjection) PursuitResourceUsage {
	configured := pursuit.ResourceLimits.MaxEffortHours > 0 || pursuit.ResourceLimits.MaxSpendEUR > 0
	if !configured {
		return PursuitResourceUsage{State: "not_configured", Reservations: []PursuitActiveResourceReservation{}, EffortLimitHours: pursuit.ResourceLimits.MaxEffortHours, SpendLimitEUR: pursuit.ResourceLimits.MaxSpendEUR}
	}
	if strings.TrimSpace(firstNonEmpty(ownerIdentity, pursuit.OwnerIdentity)) == "" {
		return PursuitResourceUsage{State: "unavailable", LimitsConfigured: true, Reservations: []PursuitActiveResourceReservation{}, EffortLimitHours: pursuit.ResourceLimits.MaxEffortHours, SpendLimitEUR: pursuit.ResourceLimits.MaxSpendEUR, BlockingReason: "resource usage cannot be verified; new work is paused while a pursuit ceiling is configured"}
	}
	return pursuitResourceUsageFromRecords(pursuit, projection.Totals[pursuit.ID], projection.ReservationTotals[pursuit.ID], projection.ActiveReservations[pursuit.ID], time.Now().UTC())
}

func dashboardProjectionMissing(id uuid.UUID) error {
	return fmt.Errorf("dashboard projection did not contain pursuit %s", id)
}

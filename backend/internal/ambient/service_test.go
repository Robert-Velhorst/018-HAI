package ambient

import (
	"automation-hub-backend/internal/memoryengine"
	"automation-hub-backend/internal/models"
	pursuitpkg "automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/workflow"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpportunityScoreRewardsNeedGapAndCapability(t *testing.T) {
	need := models.AmbientNeed{CurrentLevel: 20, TargetLevel: 90, PriorityWeight: 90}
	strong := models.AmbientOpportunity{Urgency: 80, Impact: 85, Effort: 20, Confidence: 90, Risk: 20}
	weak := models.AmbientOpportunity{Urgency: 30, Impact: 35, Effort: 80, Confidence: 40, Risk: 60}
	if opportunityScore(strong, need) <= opportunityScore(weak, need) {
		t.Fatalf("expected capable high-impact opportunity to rank higher")
	}
}

func TestFingerprintIsStableAndSourceScoped(t *testing.T) {
	first := fingerprint("workflow", "123", "growth")
	second := fingerprint("workflow", "123", "growth")
	other := fingerprint("workflow", "124", "growth")
	if first != second {
		t.Fatalf("expected stable fingerprint")
	}
	if first == other {
		t.Fatalf("expected source identity to change fingerprint")
	}
}

func TestNeedForWorkflowPrioritizesSafety(t *testing.T) {
	item := models.WorkflowItem{Title: "Reply to lawyer", RiskLevel: "high"}
	if got := needForWorkflow(item); got != "safety" {
		t.Fatalf("expected safety, got %s", got)
	}
}

func TestBuildCandidatesIncludesPursuitQueues(t *testing.T) {
	now := time.Now().UTC().Add(-20 * 24 * time.Hour)
	pursuitID := uuid.New()
	vaID := uuid.New()
	reviewID := uuid.New()
	planningID := uuid.New()
	highRiskID := uuid.New()
	pursuitDashboard := &pursuitpkg.Dashboard{
		NeedsRobert: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:                    pursuitID,
				Title:                 "Vivare legal dispute",
				ProjectKey:            "vivare",
				RiskLevel:             "high",
				NeedCategory:          "safety_and_stability",
				PriorityScore:         94,
				Confidence:            0.82,
				NextRecommendedAction: "Approve or reject the prepared lawyer follow-up.",
				UpdatedAt:             now,
			},
			NeedsRobert: 1,
			NextAction:  "Review legal response proposal",
		}},
		VAReady: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:            vaID,
				Title:         "Organize admin documents",
				RiskLevel:     "low",
				NeedCategory:  "esteem",
				PriorityScore: 60,
				Confidence:    0.76,
				UpdatedAt:     now,
			},
			NextAction: "Prepare document sorting checklist",
		}},
		ReviewDue: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:                    reviewID,
				Title:                 "Review insurance claim evidence",
				RiskLevel:             "high",
				NeedCategory:          "safety_and_stability",
				PriorityScore:         76,
				Confidence:            0.74,
				NextRecommendedAction: "Check whether the evidence bundle still needs Robert.",
				UpdatedAt:             now,
			},
			ReviewDue:  true,
			NextAction: "Review evidence bundle status",
		}},
		PlanningNeeded: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:            planningID,
				Title:         "Prepare business idea",
				RiskLevel:     "low",
				NeedCategory:  "growth",
				PriorityScore: 68,
				Confidence:    0.70,
				UpdatedAt:     now,
			},
			PlanningNeeded: true,
			NextAction:     "Create first workflow checklist",
		}},
		HighRisk: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:                    highRiskID,
				Title:                 "Keep legal evidence boundaries current",
				RiskLevel:             "high",
				NeedCategory:          "safety_and_stability",
				PriorityScore:         84,
				Confidence:            0.77,
				NextRecommendedAction: "Check approval boundary before any external action.",
				UpdatedAt:             now,
			},
			NextAction: "Review approval boundary",
		}},
	}

	candidates := buildCandidates(&workflow.WorkflowDashboard{}, nil, &memoryengine.CommandDashboard{}, pursuitDashboard, testNeedMap())
	foundDecision := false
	foundVAReady := false
	foundReviewDue := false
	foundPlanning := false
	foundHighRisk := false
	for _, candidate := range candidates {
		switch candidate.SourceType {
		case "pursuit_decision":
			foundDecision = candidate.SourceID == pursuitID.String() && candidate.RequiresApproval && candidate.NeedKey == "safety"
		case "pursuit_va_ready":
			foundVAReady = candidate.SourceID == vaID.String() && !candidate.RequiresApproval && candidate.NeedKey == "esteem"
		case "pursuit_review_due":
			foundReviewDue = candidate.SourceID == reviewID.String() && candidate.RequiresApproval && candidate.NeedKey == "safety"
		case "pursuit_planning_needed":
			foundPlanning = candidate.SourceID == planningID.String() && !candidate.RequiresApproval && candidate.NeedKey == "growth"
		case "pursuit_high_risk":
			foundHighRisk = candidate.SourceID == highRiskID.String() && candidate.RequiresApproval && candidate.NeedKey == "safety"
		}
	}
	if !foundDecision || !foundVAReady || !foundReviewDue || !foundPlanning || !foundHighRisk {
		t.Fatalf("pursuit candidates missing: decision=%v va=%v reviewDue=%v planning=%v highRisk=%v candidates=%#v", foundDecision, foundVAReady, foundReviewDue, foundPlanning, foundHighRisk, candidates)
	}
}

func TestHighRiskPursuitCandidateSkipsAlreadyCoveredQueues(t *testing.T) {
	pursuitID := uuid.New()
	pursuitDashboard := &pursuitpkg.Dashboard{
		HighRisk: []pursuitpkg.PursuitListItem{{
			Pursuit: models.Pursuit{
				ID:            pursuitID,
				Title:         "Legal response already needs Robert",
				RiskLevel:     "high",
				NeedCategory:  "safety_and_stability",
				PriorityScore: 90,
				Confidence:    0.8,
				UpdatedAt:     time.Now().UTC(),
			},
			NeedsRobert: 1,
			NextAction:  "Approve legal response",
		}},
	}

	candidates := buildCandidates(&workflow.WorkflowDashboard{}, nil, &memoryengine.CommandDashboard{}, pursuitDashboard, testNeedMap())
	for _, candidate := range candidates {
		if candidate.SourceType == "pursuit_high_risk" && candidate.SourceID == pursuitID.String() {
			t.Fatalf("high-risk duplicate candidate was created for already-covered pursuit: %#v", candidate)
		}
	}
}

func TestAcceptPursuitOpportunityCreatesWorkflowAndLinksBack(t *testing.T) {
	pursuitID := uuid.New()
	workflowID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:               uuid.New(),
		Status:           StatusProposed,
		NeedKey:          "safety",
		Title:            "Robert decision needed: Vivare",
		Rationale:        "High-risk pursuit needs approval.",
		NextAction:       "Review the prepared legal response.",
		SourceType:       "pursuit_decision",
		SourceID:         pursuitID.String(),
		SourceURI:        "pursuit://" + pursuitID.String(),
		Confidence:       88,
		RequiresApproval: true,
	}
	workflowSpy := &ambientWorkflowSpy{recordID: workflowID}
	pursuitSpy := &ambientPursuitSpy{}
	engine := NewServiceWithPursuits(&ambientRepositoryStub{opportunity: opportunity}, workflowSpy, nil, pursuitSpy)

	accepted, err := engine.Accept(opportunity.ID, ResolutionRequest{Note: "Approve draft creation.", Actor: "verified-operator"})
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	if accepted.WorkflowID == nil || *accepted.WorkflowID != workflowID {
		t.Fatalf("accepted workflow id = %v, want %s", accepted.WorkflowID, workflowID)
	}
	if len(workflowSpy.intakeRequests) != 1 || !workflowSpy.intakeRequests[0].RequiresReview {
		t.Fatalf("workflow intake requests = %#v", workflowSpy.intakeRequests)
	}
	if workflowSpy.intakeRequests[0].Actor != "verified-operator" {
		t.Fatalf("workflow actor = %q, want verified operator", workflowSpy.intakeRequests[0].Actor)
	}
	if len(pursuitSpy.links) != 2 || pursuitSpy.linkedPursuitIDs[0] != pursuitID || pursuitSpy.linkedPursuitIDs[1] != pursuitID {
		t.Fatalf("pursuit links = %#v ids=%#v", pursuitSpy.links, pursuitSpy.linkedPursuitIDs)
	}
	link := pursuitSpy.links[0]
	if link.LinkType != pursuitpkg.LinkWorkflow || link.LinkID != workflowID.String() || link.Relationship != "ambient_follow_up" {
		t.Fatalf("link = %#v, want ambient workflow link", link)
	}
	proposalLink := pursuitSpy.links[1]
	if proposalLink.LinkType != pursuitpkg.LinkAmbientOpportunity || proposalLink.LinkID != opportunity.ID.String() || proposalLink.Relationship != "ambient_proposal_accepted" {
		t.Fatalf("proposal link = %#v, want accepted ambient proposal link", proposalLink)
	}
	if proposalLink.SourceURI != opportunity.SourceURI || proposalLink.SourceLabel != opportunity.Title {
		t.Fatalf("proposal provenance was not preserved: %#v", proposalLink)
	}
	if link.Actor != "verified-operator" || proposalLink.Actor != "verified-operator" {
		t.Fatalf("pursuit link actors were not attributed to verified operator: %#v %#v", link, proposalLink)
	}
}

func TestAcceptPursuitOpportunityRetriesLinkWithoutDuplicatingWorkflow(t *testing.T) {
	pursuitID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:               uuid.New(),
		Status:           StatusProposed,
		NeedKey:          "safety",
		Title:            "Repair a pursuit follow-up",
		Rationale:        "The operational plan needs a safe follow-up.",
		NextAction:       "Prepare the next follow-up draft.",
		SourceType:       "pursuit_blocker",
		SourceID:         pursuitID.String(),
		SourceURI:        "pursuit://" + pursuitID.String(),
		Confidence:       80,
		RequiresApproval: true,
	}
	repo := &ambientRepositoryStub{opportunity: opportunity}
	workflowSpy := &ambientWorkflowSpy{}
	pursuitSpy := &ambientPursuitSpy{linkFailures: 1}
	engine := NewServiceWithPursuits(repo, workflowSpy, nil, pursuitSpy)

	if _, err := engine.Accept(opportunity.ID, ResolutionRequest{Actor: "verified-operator"}); err == nil {
		t.Fatal("expected the initial pursuit link failure")
	}
	if len(workflowSpy.intakeRequests) != 1 {
		t.Fatalf("workflow intake calls after failed link = %d, want 1", len(workflowSpy.intakeRequests))
	}
	if repo.opportunity.WorkflowID == nil || repo.opportunity.Status != StatusProposed {
		t.Fatalf("failed acceptance did not preserve resumable proposed state: %#v", repo.opportunity)
	}

	accepted, err := engine.Accept(opportunity.ID, ResolutionRequest{Actor: "verified-operator"})
	if err != nil {
		t.Fatalf("retry Accept returned error: %v", err)
	}
	if len(workflowSpy.intakeRequests) != 1 {
		t.Fatalf("workflow intake calls after retry = %d, want no duplicate workflow", len(workflowSpy.intakeRequests))
	}
	if accepted.Status != StatusAccepted || accepted.WorkflowID == nil || len(pursuitSpy.links) != 2 {
		t.Fatalf("retry did not finish acceptance and restore both pursuit links: accepted=%#v links=%#v", accepted, pursuitSpy.links)
	}
}

func TestAcceptNonPursuitOpportunityFailsClosedWithoutNativePursuitRouter(t *testing.T) {
	opportunity := &models.AmbientOpportunity{
		ID:               uuid.New(),
		OwnerIdentity:    "alice",
		Status:           StatusProposed,
		NeedKey:          "safety",
		Title:            "Resolve conflicting account evidence",
		Rationale:        "A source-backed contradiction needs review before it can guide action.",
		NextAction:       "Review the conflicting evidence and prepare the safest next step.",
		SourceType:       "memory_insight",
		SourceID:         uuid.NewString(),
		Confidence:       88,
		RequiresApproval: true,
	}
	repo := &ambientRepositoryStub{opportunity: opportunity}
	workflowSpy := &ambientWorkflowSpy{recordID: uuid.New()}
	pursuitSpy := &ambientPursuitSpy{}
	engine := NewServiceWithPursuits(repo, workflowSpy, nil, pursuitSpy)

	_, err := engine.Accept(opportunity.ID, ResolutionRequest{OwnerIdentity: "alice", Actor: "verified-operator"})
	if !errors.Is(err, pursuitpkg.ErrLifecycleRouterRequired) {
		t.Fatalf("Accept error = %v, want lifecycle router requirement", err)
	}
	if len(workflowSpy.intakeRequests) != 0 {
		t.Fatalf("incomplete pursuit integration created workflow work: %#v", workflowSpy.intakeRequests)
	}
	if repo.opportunity.Status != StatusProposed || repo.opportunity.WorkflowID != nil {
		t.Fatalf("failed-closed acceptance mutated opportunity: %#v", repo.opportunity)
	}
}

func TestAcceptNonPursuitOpportunityWithExistingWorkflowFailsClosedWithoutNativePursuitRouter(t *testing.T) {
	workflowID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "safety",
		Title:         "Repair an older ambient proposal",
		Rationale:     "A prior integration attempt retained a workflow reference.",
		NextAction:    "Review the source before taking another action.",
		SourceType:    "memory_insight",
		SourceID:      uuid.NewString(),
		WorkflowID:    &workflowID,
	}
	repo := &ambientRepositoryStub{opportunity: opportunity}
	workflowSpy := &ambientWorkflowSpy{}
	engine := NewServiceWithPursuits(repo, workflowSpy, nil, &ambientPursuitSpy{})

	_, err := engine.Accept(opportunity.ID, ResolutionRequest{OwnerIdentity: "alice", Actor: "verified-operator"})
	if !errors.Is(err, pursuitpkg.ErrLifecycleRouterRequired) {
		t.Fatalf("Accept error = %v, want lifecycle router requirement", err)
	}
	if len(workflowSpy.intakeRequests) != 0 || repo.opportunity.Status != StatusProposed || repo.opportunity.WorkflowID == nil || *repo.opportunity.WorkflowID != workflowID {
		t.Fatalf("failed-closed retry mutated legacy opportunity: intake=%#v opportunity=%#v", workflowSpy.intakeRequests, repo.opportunity)
	}
}

func TestAcceptNonPursuitOpportunityDefersCandidateWithoutWorkflow(t *testing.T) {
	opportunity := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "safety",
		Title:         "Review unmatched evidence",
		Rationale:     "This needs an explicit pursuit decision.",
		NextAction:    "Review the evidence and decide whether to create work.",
		SourceType:    "memory_insight",
		SourceID:      uuid.NewString(),
	}
	repo := &ambientRepositoryStub{opportunity: opportunity}
	workflowSpy := &ambientWorkflowSpy{recordID: uuid.New()}
	router := &ambientPursuitRouterSpy{
		ambientPursuitSpy: &ambientPursuitSpy{},
		result: &pursuitpkg.AmbientOpportunityRouteResult{
			Mode:             "candidate_created",
			PursuitID:        uuid.New(),
			CreatedCandidate: true,
			Message:          "candidate is awaiting explicit pursuit acceptance",
		},
	}
	engine := NewServiceWithPursuits(repo, workflowSpy, nil, router)

	accepted, err := engine.Accept(opportunity.ID, ResolutionRequest{OwnerIdentity: "alice", Actor: "alice"})
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	if accepted.Status != StatusAccepted || accepted.WorkflowID != nil {
		t.Fatalf("accepted ambient candidate = %#v", accepted)
	}
	if len(workflowSpy.intakeRequests) != 0 || len(router.requests) != 1 {
		t.Fatalf("candidate acceptance created work or skipped routing: intake=%#v routes=%#v", workflowSpy.intakeRequests, router.requests)
	}
	if router.requests[0].OpportunityID != opportunity.ID || router.requests[0].OwnerIdentity != "alice" {
		t.Fatalf("candidate route request = %#v", router.requests[0])
	}
}

func TestPursuitOpportunityOwnerCannotAcceptAnotherOwnersOpportunity(t *testing.T) {
	pursuitID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:         uuid.New(),
		Status:     StatusProposed,
		NeedKey:    "safety",
		Title:      "Private pursuit decision",
		Rationale:  "Requires review.",
		NextAction: "Review private evidence.",
		SourceType: "pursuit_decision",
		SourceID:   pursuitID.String(),
	}
	workflowSpy := &ambientWorkflowSpy{}
	pursuitSpy := &ambientPursuitSpy{owners: map[uuid.UUID]string{pursuitID: "bob"}}
	engine := NewServiceWithPursuits(&ambientRepositoryStub{opportunity: opportunity}, workflowSpy, nil, pursuitSpy)

	if _, err := engine.Accept(opportunity.ID, ResolutionRequest{OwnerIdentity: "alice"}); err == nil {
		t.Fatal("expected cross-owner pursuit opportunity acceptance to be rejected")
	}
	if len(workflowSpy.intakeRequests) != 0 {
		t.Fatalf("cross-owner acceptance created workflow work: %#v", workflowSpy.intakeRequests)
	}
}

func TestAcceptPursuitCandidateKeepsAmbientProposalAsContextOnly(t *testing.T) {
	pursuitID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "safety",
		Title:         "Review imported legal candidate",
		Rationale:     "The candidate needs explicit acceptance before operational work.",
		NextAction:    "Review candidate evidence and accept or archive it.",
		SourceType:    "pursuit_decision",
		SourceID:      pursuitID.String(),
		SourceURI:     "pursuit://" + pursuitID.String(),
	}
	repo := &ambientRepositoryStub{opportunity: opportunity}
	workflowSpy := &ambientWorkflowSpy{recordID: uuid.New()}
	pursuitSpy := &ambientPursuitSpy{details: map[uuid.UUID]models.Pursuit{
		pursuitID: {
			ID:               pursuitID,
			OwnerIdentity:    "alice",
			SourceOfCreation: "source_pursuit_candidate",
		},
	}}
	engine := NewServiceWithPursuits(repo, workflowSpy, nil, pursuitSpy)

	accepted, err := engine.Accept(opportunity.ID, ResolutionRequest{OwnerIdentity: "alice", Actor: "alice"})
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	if accepted.Status != StatusAccepted || accepted.WorkflowID != nil {
		t.Fatalf("candidate acceptance = %#v, want accepted context without workflow", accepted)
	}
	if len(workflowSpy.intakeRequests) != 0 {
		t.Fatalf("candidate ambient acceptance created workflow work: %#v", workflowSpy.intakeRequests)
	}
	if len(pursuitSpy.links) != 1 || pursuitSpy.links[0].LinkType != pursuitpkg.LinkAmbientOpportunity || pursuitSpy.links[0].LinkID != opportunity.ID.String() {
		t.Fatalf("candidate ambient proposal was not linked as pursuit context: %#v", pursuitSpy.links)
	}
	if !strings.Contains(accepted.ResolutionNote, "explicit candidate acceptance") {
		t.Fatalf("candidate acceptance note = %q, want explicit acceptance guard", accepted.ResolutionNote)
	}
}

func TestPursuitOpportunitiesAreFilteredForAuthenticatedOwner(t *testing.T) {
	aliceID := uuid.New()
	bobID := uuid.New()
	pursuits := &ambientPursuitSpy{
		owners: map[uuid.UUID]string{aliceID: "alice", bobID: "bob"},
		pursuits: []models.Pursuit{
			{ID: aliceID, OwnerIdentity: "alice"},
			{ID: bobID, OwnerIdentity: "bob"},
		},
	}
	engine := NewServiceWithPursuits(nil, nil, nil, pursuits).(*service)
	visible := engine.visibleOpportunities("alice", []models.AmbientOpportunity{
		{ID: uuid.New(), OwnerIdentity: "alice", SourceType: "pursuit_stale", SourceID: aliceID.String()},
		{ID: uuid.New(), OwnerIdentity: "bob", SourceType: "pursuit_stale", SourceID: bobID.String()},
		{ID: uuid.New(), SourceType: "workflow", SourceID: "shared-workflow"},
	})
	if len(visible) != 1 || visible[0].SourceID != aliceID.String() {
		t.Fatalf("owner-visible opportunities = %#v", visible)
	}
	if pursuits.listForOwnerCalls != 1 || pursuits.detailForOwnerCalls != 0 {
		t.Fatalf("pursuit visibility calls = list:%d detail:%d, want 1/0", pursuits.listForOwnerCalls, pursuits.detailForOwnerCalls)
	}
}

func TestPursuitOpportunityVisibilityUsesOneOwnerScopedRead(t *testing.T) {
	owned := make([]models.Pursuit, 0, 75)
	opportunities := make([]models.AmbientOpportunity, 0, 75)
	for index := 0; index < 75; index++ {
		id := uuid.New()
		owned = append(owned, models.Pursuit{ID: id, OwnerIdentity: "alice"})
		opportunities = append(opportunities, models.AmbientOpportunity{
			ID: uuid.New(), OwnerIdentity: "alice", SourceType: "pursuit_stale", SourceID: id.String(),
		})
	}
	pursuits := &ambientPursuitSpy{pursuits: owned}
	engine := NewServiceWithPursuits(nil, nil, nil, pursuits).(*service)

	visible := engine.visibleOpportunities("alice", opportunities)
	if len(visible) != len(opportunities) {
		t.Fatalf("visible opportunities = %d, want %d", len(visible), len(opportunities))
	}
	if pursuits.listForOwnerCalls != 1 || pursuits.detailForOwnerCalls != 0 {
		t.Fatalf("pursuit visibility calls = list:%d detail:%d, want 1/0", pursuits.listForOwnerCalls, pursuits.detailForOwnerCalls)
	}
}

func TestNonPursuitOpportunityVisibilityDoesNotReadPursuits(t *testing.T) {
	pursuits := &ambientPursuitSpy{}
	engine := NewServiceWithPursuits(nil, nil, nil, pursuits).(*service)
	opportunities := []models.AmbientOpportunity{
		{ID: uuid.New(), OwnerIdentity: "alice", SourceType: "workflow", SourceID: "workflow-1"},
	}

	visible := engine.visibleOpportunities("alice", opportunities)
	if len(visible) != 1 {
		t.Fatalf("visible opportunities = %d, want 1", len(visible))
	}
	if pursuits.listForOwnerCalls != 0 || pursuits.detailForOwnerCalls != 0 {
		t.Fatalf("non-pursuit visibility calls = list:%d detail:%d, want 0/0", pursuits.listForOwnerCalls, pursuits.detailForOwnerCalls)
	}
}

func TestPursuitOpportunityVisibilityFailsClosedWhenOwnerLookupFails(t *testing.T) {
	pursuitID := uuid.New()
	pursuits := &ambientPursuitSpy{listForOwnerErr: errors.New("pursuit repository unavailable")}
	engine := NewServiceWithPursuits(nil, nil, nil, pursuits).(*service)

	visible := engine.visibleOpportunities("alice", []models.AmbientOpportunity{
		{ID: uuid.New(), OwnerIdentity: "alice", SourceType: "pursuit_stale", SourceID: pursuitID.String()},
		{ID: uuid.New(), OwnerIdentity: "alice", SourceType: "workflow", SourceID: "workflow-1"},
	})
	if len(visible) != 1 || visible[0].SourceType != "workflow" {
		t.Fatalf("visible opportunities after pursuit lookup failure = %#v", visible)
	}
	if pursuits.listForOwnerCalls != 1 || pursuits.detailForOwnerCalls != 0 {
		t.Fatalf("pursuit visibility calls = list:%d detail:%d, want 1/0", pursuits.listForOwnerCalls, pursuits.detailForOwnerCalls)
	}
}

func TestOverviewForOwnerFallsBackToDefaults(t *testing.T) {
	repo := &ambientRepositoryStub{}
	engine := NewService(repo, nil, nil)

	overview, err := engine.OverviewForOwner("alice")
	if err != nil {
		t.Fatalf("OverviewForOwner: %v", err)
	}
	if len(overview.Needs) != len(defaultNeeds()) {
		t.Fatalf("fallback needs = %d, want %d", len(overview.Needs), len(defaultNeeds()))
	}
	if findAmbientNeed(overview.Needs, "safety").PriorityWeight != 100 {
		t.Fatalf("fallback safety need = %#v", findAmbientNeed(overview.Needs, "safety"))
	}
}

func TestOverviewSummaryForOwnerSkipsOpportunitiesAndBoundsScanHistory(t *testing.T) {
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	for index := 0; index < 8; index++ {
		repo.scans = append(repo.scans, models.AmbientScan{OwnerIdentity: "alice", Trigger: "scheduled"})
	}
	engine := NewService(repo, nil, nil)

	overview, err := engine.OverviewSummaryForOwner("alice")
	if err != nil {
		t.Fatalf("OverviewSummaryForOwner: %v", err)
	}
	if repo.opportunitiesForOwnerCalls != 0 {
		t.Fatalf("summary loaded %d opportunity collections, want 0", repo.opportunitiesForOwnerCalls)
	}
	if repo.lastScanLimit != 3 {
		t.Fatalf("summary scan limit = %d, want 3", repo.lastScanLimit)
	}
	if len(overview.Scans) != 3 {
		t.Fatalf("summary scans = %d, want 3", len(overview.Scans))
	}
	if overview.Opportunities == nil || len(overview.Opportunities) != 0 {
		t.Fatalf("summary opportunities = %#v, want an empty collection", overview.Opportunities)
	}
}

func TestScanForOwnerBuildsPrivatePursuitProposalsOnly(t *testing.T) {
	pursuitID := uuid.New()
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	pursuits := &ambientPursuitSpy{
		dashboard: &pursuitpkg.Dashboard{
			ReviewDue: []pursuitpkg.PursuitListItem{{
				Pursuit:    models.Pursuit{ID: pursuitID, OwnerIdentity: "alice", Title: "Prepare evidence bundle", RiskLevel: "medium", Confidence: 0.82, PriorityScore: 80},
				NextAction: "Review the evidence bundle and choose the next safe action.",
			}},
		},
		pursuits: []models.Pursuit{{ID: pursuitID, OwnerIdentity: "alice", Title: "Prepare evidence bundle", Status: pursuitpkg.StatusActive}},
	}
	engine := NewServiceWithPursuits(repo, nil, nil, pursuits)

	scan, err := engine.ScanForOwner("alice", "manual")
	if err != nil {
		t.Fatalf("ScanForOwner: %v", err)
	}
	if scan.OwnerIdentity != "alice" || scan.Created != 1 || scan.Advanced != 0 {
		t.Fatalf("owner scan = %#v, want one private suggestion-only proposal", scan)
	}
	if repo.opportunity == nil || repo.opportunity.OwnerIdentity != "alice" || repo.opportunity.SourceID != pursuitID.String() {
		t.Fatalf("stored opportunity = %#v, want Alice pursuit proposal", repo.opportunity)
	}
	if repo.opportunity.WorkflowID != nil {
		t.Fatalf("personal scan unexpectedly created executable workflow state: %#v", repo.opportunity)
	}
}

func TestValidatePursuitDashboardOwnerRejectsCrossOwnerAndUnscopedLegacyData(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "legacy-owner")
	alice := models.Pursuit{ID: uuid.New(), OwnerIdentity: "alice"}
	if err := validatePursuitDashboardOwner("alice", &pursuitpkg.Dashboard{
		ReviewDue:     []pursuitpkg.PursuitListItem{{Pursuit: alice}},
		DecisionQueue: []pursuitpkg.PursuitDashboardDecision{{Pursuit: alice}},
	}); err != nil {
		t.Fatalf("owner-matched dashboard rejected: %v", err)
	}

	for name, dashboard := range map[string]*pursuitpkg.Dashboard{
		"cross-owner item": {
			ReviewDue: []pursuitpkg.PursuitListItem{{Pursuit: models.Pursuit{ID: uuid.New(), OwnerIdentity: "bob"}}},
		},
		"cross-owner decision": {
			DecisionQueue: []pursuitpkg.PursuitDashboardDecision{{Pursuit: models.Pursuit{ID: uuid.New(), OwnerIdentity: "bob"}}},
		},
		"ownerless legacy item": {
			ReviewDue: []pursuitpkg.PursuitListItem{{Pursuit: models.Pursuit{ID: uuid.New()}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePursuitDashboardOwner("alice", dashboard); err == nil {
				t.Fatal("inaccessible dashboard data was accepted")
			}
		})
	}

	if err := validatePursuitDashboardOwner("legacy-owner", &pursuitpkg.Dashboard{
		ReviewDue: []pursuitpkg.PursuitListItem{{Pursuit: models.Pursuit{ID: uuid.New()}}},
	}); err != nil {
		t.Fatalf("migration owner could not read ownerless legacy pursuit: %v", err)
	}
}

func TestAmbientNeedProfilesArePrivateToEachOwner(t *testing.T) {
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	engine := NewService(repo, nil, nil).(*service)
	priority := 17
	note := "Prioritize stabilizing this account's legal and financial exposure."

	updated, err := engine.UpdateNeedForOwner("alice", "safety", NeedUpdateRequest{PriorityWeight: &priority, Notes: &note})
	if err != nil {
		t.Fatalf("UpdateNeedForOwner: %v", err)
	}
	if updated.PriorityWeight != priority || updated.Notes != note {
		t.Fatalf("alice update = %#v", updated)
	}

	aliceNeeds, err := engine.needsForOwner("alice")
	if err != nil {
		t.Fatalf("alice needs: %v", err)
	}
	bobNeeds, err := engine.needsForOwner("bob")
	if err != nil {
		t.Fatalf("bob needs: %v", err)
	}
	if findAmbientNeed(aliceNeeds, "safety").PriorityWeight != priority {
		t.Fatalf("alice safety profile was not applied: %#v", aliceNeeds)
	}
	if findAmbientNeed(bobNeeds, "safety").PriorityWeight != 100 {
		t.Fatalf("bob did not retain the system default: %#v", bobNeeds)
	}
	if findAmbientNeed(bobNeeds, "safety").Notes != "" {
		t.Fatalf("bob received alice's ambient notes: %#v", bobNeeds)
	}
}

func TestAmbientNeedUpdateUsesReadOnlyDefaultsFallback(t *testing.T) {
	repo := &ambientRepositoryStub{}
	engine := NewService(repo, nil, nil).(*service)
	priority := 84

	updated, err := engine.UpdateNeedForOwner("alice", "safety", NeedUpdateRequest{PriorityWeight: &priority})
	if err != nil {
		t.Fatalf("UpdateNeedForOwner with unseeded repository: %v", err)
	}
	if updated.Key != "safety" || updated.PriorityWeight != priority || len(repo.overrides) != 1 {
		t.Fatalf("fallback update = %#v overrides=%#v", updated, repo.overrides)
	}
}

func TestOwnerScanUsesPrivateNeedProfile(t *testing.T) {
	pursuitID := uuid.New()
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	engine := NewServiceWithPursuits(repo, nil, nil, &ambientPursuitSpy{
		dashboard: &pursuitpkg.Dashboard{PlanningNeeded: []pursuitpkg.PursuitListItem{{
			Pursuit:    models.Pursuit{ID: pursuitID, OwnerIdentity: "alice", Title: "Draft a future plan", NeedCategory: "growth", RiskLevel: "low", Confidence: 0.8, PriorityScore: 72},
			NextAction: "Prepare a safe first plan.",
		}}},
		pursuits: []models.Pursuit{{ID: pursuitID, OwnerIdentity: "alice", Status: pursuitpkg.StatusActive}},
	}).(*service)
	disabled := false
	if _, err := engine.UpdateNeedForOwner("alice", "growth", NeedUpdateRequest{Enabled: &disabled}); err != nil {
		t.Fatalf("disable Alice growth need: %v", err)
	}

	scan, err := engine.ScanForOwner("alice", "manual")
	if err != nil {
		t.Fatalf("ScanForOwner: %v", err)
	}
	if scan.Created != 0 || repo.opportunity != nil {
		t.Fatalf("disabled private need still produced a proposal: scan=%#v opportunity=%#v", scan, repo.opportunity)
	}
}

func TestStoreCandidatesAvoidsUnchangedHourlyWrite(t *testing.T) {
	now := time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
	candidate := models.AmbientOpportunity{
		OwnerIdentity: "alice", Fingerprint: strings.Repeat("a", 64), NeedKey: "growth",
		Title: "Review the active pursuit", Rationale: "The pursuit is due for review.",
		NextAction: "Review the evidence and choose the next action.", PriorityScore: 72,
		Urgency: 70, Impact: 75, Effort: 20, Confidence: 85, Risk: 20,
		EvidenceManifest: `{"type":"pursuit_review_due"}`, Status: StatusProposed,
	}
	repo := &ambientRepositoryStub{opportunity: &models.AmbientOpportunity{
		ID: uuid.New(), OwnerIdentity: candidate.OwnerIdentity, Fingerprint: candidate.Fingerprint,
		NeedKey: candidate.NeedKey, Title: candidate.Title, Rationale: candidate.Rationale,
		NextAction: candidate.NextAction, PriorityScore: candidate.PriorityScore,
		Urgency: candidate.Urgency, Impact: candidate.Impact, Effort: candidate.Effort,
		Confidence: candidate.Confidence, Risk: candidate.Risk,
		EvidenceManifest: candidate.EvidenceManifest, Status: StatusProposed,
		LastSeenAt: now.Add(-time.Hour),
	}}
	engine := NewService(repo, nil, nil).(*service)
	scan := &models.AmbientScan{}

	if err := engine.storeCandidates(scan, []models.AmbientOpportunity{candidate}, Policy{MinimumScore: 0, MinimumConfidence: 0}, now); err != nil {
		t.Fatalf("storeCandidates: %v", err)
	}
	if repo.saveOpportunityCalls != 0 || scan.Updated != 0 || scan.Deduplicated != 1 {
		t.Fatalf("unchanged candidate writes=%d scan=%#v, want no write and one deduplication", repo.saveOpportunityCalls, scan)
	}
	if !repo.opportunity.LastSeenAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("unchanged candidate refreshed last seen at %s", repo.opportunity.LastSeenAt)
	}
}

func TestStoreCandidatesPersistsSemanticChangeAndDailyFreshness(t *testing.T) {
	now := time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
	base := models.AmbientOpportunity{
		ID: uuid.New(), OwnerIdentity: "alice", Fingerprint: strings.Repeat("b", 64),
		NeedKey: "growth", Title: "Plan pursuit", Rationale: "Planning is needed.",
		NextAction: "Create the first plan.", PriorityScore: 64, Urgency: 60,
		Impact: 70, Effort: 25, Confidence: 80, Risk: 20,
		EvidenceManifest: `{"type":"pursuit_planning_needed"}`, Status: StatusProposed,
		LastSeenAt: now.Add(-time.Hour),
	}
	repo := &ambientRepositoryStub{opportunity: &base}
	engine := NewService(repo, nil, nil).(*service)
	changed := base
	changed.NextAction = "Create a source-backed plan."
	scan := &models.AmbientScan{}

	if err := engine.storeCandidates(scan, []models.AmbientOpportunity{changed}, Policy{}, now); err != nil {
		t.Fatalf("semantic update: %v", err)
	}
	if repo.saveOpportunityCalls != 1 || scan.Updated != 1 || repo.opportunity.NextAction != changed.NextAction || !repo.opportunity.LastSeenAt.Equal(now) {
		t.Fatalf("semantic update writes=%d scan=%#v item=%#v", repo.saveOpportunityCalls, scan, repo.opportunity)
	}

	repo.saveOpportunityCalls = 0
	repo.opportunity.LastSeenAt = now.Add(-ambientLastSeenWriteInterval)
	scan = &models.AmbientScan{}
	if err := engine.storeCandidates(scan, []models.AmbientOpportunity{changed}, Policy{}, now); err != nil {
		t.Fatalf("daily refresh: %v", err)
	}
	if repo.saveOpportunityCalls != 1 || scan.Updated != 1 || scan.Deduplicated != 1 || !repo.opportunity.LastSeenAt.Equal(now) {
		t.Fatalf("daily refresh writes=%d scan=%#v item=%#v", repo.saveOpportunityCalls, scan, repo.opportunity)
	}

	repo.saveOpportunityCalls = 0
	repo.opportunity.Status = StatusDismissed
	repo.opportunity.CooldownUntil = timePointer(now.Add(-time.Minute))
	scan = &models.AmbientScan{}
	if err := engine.storeCandidates(scan, []models.AmbientOpportunity{changed}, Policy{}, now); err != nil {
		t.Fatalf("cooldown reactivation: %v", err)
	}
	if repo.saveOpportunityCalls != 1 || scan.Updated != 1 || repo.opportunity.Status != StatusProposed {
		t.Fatalf("cooldown reactivation writes=%d scan=%#v item=%#v", repo.saveOpportunityCalls, scan, repo.opportunity)
	}
}

func TestStoreCandidatesUsesOneBulkFingerprintLookup(t *testing.T) {
	now := time.Date(2026, time.August, 14, 19, 0, 0, 0, time.UTC)
	existingA := models.AmbientOpportunity{
		ID: uuid.New(), OwnerIdentity: "alice", Fingerprint: strings.Repeat("c", 64),
		NeedKey: "growth", Title: "Review pursuit", Rationale: "Review is due.",
		NextAction: "Review the pursuit.", PriorityScore: 70, Confidence: 85,
		Status: StatusProposed, LastSeenAt: now,
	}
	existingB := existingA
	existingB.ID = uuid.New()
	existingB.Fingerprint = strings.Repeat("d", 64)
	existingB.Title = "Plan pursuit"
	existingB.NextAction = "Plan the pursuit."
	newCandidate := existingA
	newCandidate.ID = uuid.Nil
	newCandidate.Fingerprint = strings.Repeat("e", 64)
	newCandidate.Title = "Unblock pursuit"
	newCandidate.NextAction = "Resolve the blocker."
	filtered := existingA
	filtered.ID = uuid.Nil
	filtered.Fingerprint = strings.Repeat("f", 64)
	filtered.PriorityScore = 5

	repo := &ambientBatchRepositoryStub{
		ambientRepositoryStub: &ambientRepositoryStub{},
		existing: map[string]models.AmbientOpportunity{
			existingA.Fingerprint: existingA,
			existingB.Fingerprint: existingB,
		},
	}
	engine := NewService(repo, nil, nil).(*service)
	scan := &models.AmbientScan{}

	err := engine.storeCandidates(scan, []models.AmbientOpportunity{existingA, existingB, newCandidate, filtered}, Policy{MinimumScore: 10}, now)
	if err != nil {
		t.Fatalf("storeCandidates: %v", err)
	}
	if repo.batchCalls != 1 || repo.singleCalls != 0 {
		t.Fatalf("fingerprint lookups = batch:%d single:%d, want 1/0", repo.batchCalls, repo.singleCalls)
	}
	if len(repo.batchFingerprints) != 3 {
		t.Fatalf("bulk fingerprints = %d, want three eligible candidates", len(repo.batchFingerprints))
	}
	if scan.Deduplicated != 2 || scan.Created != 1 || scan.Filtered != 1 {
		t.Fatalf("scan counters = %#v, want two deduplicated, one created, one filtered", scan)
	}
}

type ambientBatchRepositoryStub struct {
	*ambientRepositoryStub
	existing          map[string]models.AmbientOpportunity
	batchCalls        int
	singleCalls       int
	batchFingerprints []string
}

func (r *ambientBatchRepositoryStub) FindOpportunitiesByFingerprints(fingerprints []string) ([]models.AmbientOpportunity, error) {
	r.batchCalls++
	r.batchFingerprints = append([]string{}, fingerprints...)
	result := make([]models.AmbientOpportunity, 0, len(fingerprints))
	for _, fingerprint := range fingerprints {
		if item, exists := r.existing[fingerprint]; exists {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *ambientBatchRepositoryStub) FindOpportunityByFingerprint(fingerprint string) (*models.AmbientOpportunity, error) {
	r.singleCalls++
	return r.ambientRepositoryStub.FindOpportunityByFingerprint(fingerprint)
}

func timePointer(value time.Time) *time.Time { return &value }

func findAmbientNeed(needs []models.AmbientNeed, key string) models.AmbientNeed {
	for _, need := range needs {
		if need.Key == key {
			return need
		}
	}
	return models.AmbientNeed{}
}

func testNeedMap() map[string]models.AmbientNeed {
	result := map[string]models.AmbientNeed{}
	for _, need := range defaultNeeds() {
		result[need.Key] = need
	}
	return result
}

type ambientWorkflowSpy struct {
	intakeRequests []workflow.IntakeRequest
	recordID       uuid.UUID
}

func (s *ambientWorkflowSpy) Dashboard() (*workflow.WorkflowDashboard, error) {
	return &workflow.WorkflowDashboard{}, nil
}

func (s *ambientWorkflowSpy) Items(bool) ([]models.WorkflowItem, error) {
	return nil, nil
}

func (s *ambientWorkflowSpy) Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	s.intakeRequests = append(s.intakeRequests, request)
	if s.recordID == uuid.Nil {
		s.recordID = uuid.New()
	}
	return &workflow.WorkflowRecord{Item: models.WorkflowItem{ID: s.recordID, Title: request.Input}}, nil
}

func (s *ambientWorkflowSpy) RunDue(workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	return &workflow.WorkflowRunSummary{}, nil
}

func (s *ambientWorkflowSpy) RunDueOpenLoops(workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	return &workflow.OpenLoopRunSummary{}, nil
}

type ambientPursuitSpy struct {
	dashboard           *pursuitpkg.Dashboard
	pursuits            []models.Pursuit
	details             map[uuid.UUID]models.Pursuit
	links               []pursuitpkg.LinkRequest
	linkedPursuitIDs    []uuid.UUID
	owners              map[uuid.UUID]string
	linkFailures        int
	autoLinkRequests    []pursuitpkg.AutoLinkWorkflowRequest
	autoLinkResult      *pursuitpkg.AutoLinkResult
	autoLinkErr         error
	listForOwnerCalls   int
	detailForOwnerCalls int
	listForOwnerErr     error
}

type ambientPursuitRouterSpy struct {
	*ambientPursuitSpy
	requests []pursuitpkg.AmbientOpportunityRouteRequest
	result   *pursuitpkg.AmbientOpportunityRouteResult
	err      error
}

func (s *ambientPursuitRouterSpy) RouteAmbientOpportunity(request pursuitpkg.AmbientOpportunityRouteRequest) (*pursuitpkg.AmbientOpportunityRouteResult, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *ambientPursuitSpy) Dashboard() (*pursuitpkg.Dashboard, error) {
	if s.dashboard != nil {
		return s.dashboard, nil
	}
	return &pursuitpkg.Dashboard{}, nil
}

func (s *ambientPursuitSpy) DashboardForOwner(_ string) (*pursuitpkg.Dashboard, error) {
	return s.Dashboard()
}

func (s *ambientPursuitSpy) List(bool) ([]models.Pursuit, error) {
	return s.pursuits, nil
}

func (s *ambientPursuitSpy) ListForOwner(ownerIdentity string, _ bool) ([]models.Pursuit, error) {
	s.listForOwnerCalls++
	if s.listForOwnerErr != nil {
		return nil, s.listForOwnerErr
	}
	result := []models.Pursuit{}
	for _, item := range s.pursuits {
		if item.OwnerIdentity == ownerIdentity {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *ambientPursuitSpy) Link(id uuid.UUID, request pursuitpkg.LinkRequest) (*models.PursuitLink, error) {
	if s.linkFailures > 0 {
		s.linkFailures--
		return nil, errors.New("temporary pursuit link failure")
	}
	s.linkedPursuitIDs = append(s.linkedPursuitIDs, id)
	s.links = append(s.links, request)
	return &models.PursuitLink{
		ID:           uuid.New(),
		PursuitID:    id,
		LinkType:     request.LinkType,
		LinkID:       request.LinkID,
		Relationship: request.Relationship,
		SourceURI:    request.SourceURI,
		SourceLabel:  request.SourceLabel,
		Confidence:   request.Confidence,
	}, nil
}

func (s *ambientPursuitSpy) AutoLinkWorkflow(request pursuitpkg.AutoLinkWorkflowRequest) (*pursuitpkg.AutoLinkResult, error) {
	s.autoLinkRequests = append(s.autoLinkRequests, request)
	if s.autoLinkErr != nil {
		return nil, s.autoLinkErr
	}
	if s.autoLinkResult != nil {
		return s.autoLinkResult, nil
	}
	return &pursuitpkg.AutoLinkResult{}, nil
}

func (s *ambientPursuitSpy) DetailForOwner(ownerIdentity string, id uuid.UUID) (*pursuitpkg.PursuitDetail, error) {
	s.detailForOwnerCalls++
	if owner, found := s.owners[id]; found && owner != "" && owner != ownerIdentity {
		return nil, errors.New("pursuit not found")
	}
	detail := models.Pursuit{ID: id, OwnerIdentity: ownerIdentity}
	if item, found := s.details[id]; found {
		detail = item
		if detail.OwnerIdentity == "" {
			detail.OwnerIdentity = ownerIdentity
		}
	}
	return &pursuitpkg.PursuitDetail{Pursuit: detail}, nil
}

func TestClosedPursuitOpportunityUpdatesCompleteOnlyOpenPursuitWork(t *testing.T) {
	completedID := uuid.New()
	archivedID := uuid.New()
	activeID := uuid.New()
	updatedAt := time.Now().UTC()
	updates := closedPursuitOpportunityUpdates(
		[]models.AmbientOpportunity{
			{ID: uuid.New(), SourceType: "pursuit_decision", SourceID: completedID.String(), Status: StatusProposed},
			{ID: uuid.New(), SourceType: "pursuit_blocker", SourceID: archivedID.String(), Status: StatusAccepted},
			{ID: uuid.New(), SourceType: "pursuit_high_risk", SourceID: activeID.String(), Status: StatusProposed},
			{ID: uuid.New(), SourceType: "pursuit_review_due", SourceID: completedID.String(), Status: StatusDismissed},
		},
		[]models.Pursuit{
			{ID: completedID, Status: pursuitpkg.StatusCompleted},
			{ID: archivedID, Status: pursuitpkg.StatusArchived, Archived: true},
			{ID: activeID, Status: pursuitpkg.StatusActive},
		},
		updatedAt,
	)
	if len(updates) != 2 {
		t.Fatalf("updates = %#v, want completed and archived pursuit opportunities only", updates)
	}
	for _, item := range updates {
		if item.Status != StatusCompleted || item.LastSeenAt != updatedAt || !strings.Contains(item.ResolutionNote, "Linked pursuit closed") {
			t.Fatalf("closed pursuit update = %#v", item)
		}
	}
}

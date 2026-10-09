package source

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestTrelloSyncPreservesUnverifiedProjectHintThroughPursuitRouter(t *testing.T) {
	cards := `[{"id":"hint-card","name":"Prepare case evidence","desc":"Review the source links and prepare a concise evidence packet for the upcoming hearing.","shortUrl":"https://trello.com/c/hint-card","dateLastActivity":"2026-09-20T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"open-item","name":"Verify source links and prepare a reviewable packet","state":"incomplete","pos":1}]}]}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceRecord := newTrelloSource(uuid.New(), "abc123XY", "")
	sourceRecord.DefaultProjectKey = "board-project-default"
	sourceRepo := newFakeSourceRepo(sourceRecord)

	pursuitRepo := &sourcePursuitIntakeRepository{}
	workflowService := &sourcePursuitWorkflowFixture{
		fakeSourceWorkflowService: &fakeSourceWorkflowService{},
		repo:                      pursuitRepo,
	}
	pursuitService := pursuit.NewService(pursuitRepo, workflowService)
	sourceService := NewServiceWithWorkflowAndPursuitLinker(
		sourceRepo,
		&fakeSourceMemoryService{},
		workflowService,
		pursuitService,
	)

	result, err := sourceService.Sync(sourceRecord.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Trello Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want one actionable card", len(result.Extractions))
	}
	if len(result.PursuitOutcomes) != 1 {
		t.Fatalf("pursuit outcomes = %#v, want one review-gated candidate", result.PursuitOutcomes)
	}
	outcome := result.PursuitOutcomes[0]
	if outcome.Status != "candidate_pending" || outcome.PursuitID == "" || outcome.WorkflowID != "" {
		t.Fatalf("pursuit outcome = %#v, want a candidate with no workflow before acceptance", outcome)
	}
	candidateID := outcome.PursuitID
	sourceEngine, ok := sourceService.(*service)
	if !ok {
		t.Fatal("source service is not the expected production service")
	}
	replayed, err := sourceEngine.createWorkflowFromExtractionWithSignal(sourceRecord, &result.Extractions[0], "")
	if err != nil {
		t.Fatalf("retry persisted source extraction: %v", err)
	}
	if replayed == nil || replayed.Status != "candidate_pending" || replayed.PursuitID != candidateID {
		t.Fatalf("source retry outcome = %#v, want the same candidate awaiting acceptance", replayed)
	}
	pursuitID, err := uuid.Parse(candidateID)
	if err != nil {
		t.Fatalf("candidate pursuit ID %q: %v", candidateID, err)
	}
	candidate, ok := pursuitRepo.pursuits[pursuitID]
	if !ok {
		t.Fatalf("candidate %s was not persisted by the pursuit router", pursuitID)
	}
	if candidate.ProjectKey != "" {
		t.Fatalf("Trello default project hint was promoted to confirmed ProjectKey %q", candidate.ProjectKey)
	}
	if !strings.Contains(candidate.Description, "Unverified project hint from the source: board-project-default") ||
		!strings.Contains(candidate.Description, "has not been confirmed") {
		t.Fatalf("candidate description did not retain the default project as explicitly unverified context: %q", candidate.Description)
	}
	if len(workflowService.requests) != 0 {
		t.Fatalf("unaccepted Trello candidate created %d workflow(s)", len(workflowService.requests))
	}
	if len(pursuitRepo.pursuits) != 1 {
		t.Fatalf("replaying source routing created %d pursuits, want exactly one", len(pursuitRepo.pursuits))
	}
	if !hasPursuitTestLink(pursuitRepo.links, pursuitID, "source_item") || !hasPursuitTestLink(pursuitRepo.links, pursuitID, "source_extraction") {
		t.Fatalf("candidate did not retain raw-item and extraction provenance links: %#v", pursuitRepo.links)
	}
	if !sourceRepo.hasAudit("pursuit.intake_deferred") || sourceRepo.hasAudit("workflow.intake_created") || sourceRepo.hasAudit("workflow.intake_failed") {
		t.Fatalf("source audit did not record a successful candidate deferral")
	}

	if _, err := pursuitService.AcceptCandidateForOwner("alice", pursuitID, pursuit.PlanRequest{Actor: "alice"}); err != nil {
		t.Fatalf("explicit candidate acceptance: %v", err)
	}
	if len(workflowService.requests) != 1 {
		t.Fatalf("explicit acceptance created %d workflow requests, want one", len(workflowService.requests))
	}
	if workflowService.requests[0].SourceType != "pursuit" || workflowService.requests[0].SourceURI != "pursuit://"+pursuitID.String() {
		t.Fatalf("accepted workflow did not retain pursuit provenance: %#v", workflowService.requests[0])
	}
	if !hasPursuitTestLink(pursuitRepo.links, pursuitID, "workflow") {
		t.Fatalf("accepted workflow was not linked to its pursuit: %#v", pursuitRepo.links)
	}
}

// This narrow in-memory repository lets the test exercise the real pursuit
// service without a database, provider account, or shared runtime state.
type sourcePursuitIntakeRepository struct {
	pursuit.Repository
	pursuits   map[uuid.UUID]models.Pursuit
	links      map[uuid.UUID]models.PursuitLink
	activities map[uuid.UUID][]models.PursuitActivity
	workflows  map[uuid.UUID]models.WorkflowItem
}

func (r *sourcePursuitIntakeRepository) Create(item *models.Pursuit) (*models.Pursuit, error) {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if r.pursuits == nil {
		r.pursuits = map[uuid.UUID]models.Pursuit{}
	}
	if r.links == nil {
		r.links = map[uuid.UUID]models.PursuitLink{}
	}
	if r.activities == nil {
		r.activities = map[uuid.UUID][]models.PursuitActivity{}
	}
	if r.workflows == nil {
		r.workflows = map[uuid.UUID]models.WorkflowItem{}
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	r.pursuits[item.ID] = *item
	return item, nil
}

func (r *sourcePursuitIntakeRepository) Update(item *models.Pursuit) (*models.Pursuit, error) {
	if _, ok := r.pursuits[item.ID]; !ok {
		return nil, gorm.ErrRecordNotFound
	}
	item.UpdatedAt = time.Now().UTC()
	r.pursuits[item.ID] = *item
	return item, nil
}

func (r *sourcePursuitIntakeRepository) FindByID(id uuid.UUID) (*models.Pursuit, error) {
	item, ok := r.pursuits[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return &item, nil
}

func (r *sourcePursuitIntakeRepository) FindAll(includeArchived bool) ([]models.Pursuit, error) {
	items := make([]models.Pursuit, 0, len(r.pursuits))
	for _, item := range r.pursuits {
		if includeArchived || !item.Archived {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *sourcePursuitIntakeRepository) FindLinks(pursuitID uuid.UUID) ([]models.PursuitLink, error) {
	links := make([]models.PursuitLink, 0)
	for _, link := range r.links {
		if link.PursuitID == pursuitID {
			links = append(links, link)
		}
	}
	sort.Slice(links, func(i, j int) bool { return links[i].CreatedAt.Before(links[j].CreatedAt) })
	return links, nil
}

func (r *sourcePursuitIntakeRepository) CreateLink(link *models.PursuitLink) (*models.PursuitLink, error) {
	if r.links == nil {
		r.links = map[uuid.UUID]models.PursuitLink{}
	}
	for _, existing := range r.links {
		if existing.PursuitID == link.PursuitID && existing.LinkType == link.LinkType &&
			existing.LinkID == link.LinkID && existing.Relationship == link.Relationship {
			copy := existing
			return &copy, nil
		}
	}
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	link.CreatedAt = time.Now().UTC()
	r.links[link.ID] = *link
	if link.LinkType == "workflow" {
		if id, err := uuid.Parse(link.LinkID); err == nil {
			if _, ok := r.workflows[id]; !ok {
				r.workflows[id] = models.WorkflowItem{ID: id, Title: "Accepted first workflow"}
			}
		}
	}
	return link, nil
}

func (r *sourcePursuitIntakeRepository) FindActivities(pursuitID uuid.UUID, limit int) ([]models.PursuitActivity, error) {
	items := append([]models.PursuitActivity(nil), r.activities[pursuitID]...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkForOwner(ownerIdentity, linkType, linkID string) (*models.PursuitLink, error) {
	for _, link := range r.links {
		item := r.pursuits[link.PursuitID]
		if link.LinkType == linkType && link.LinkID == linkID && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity) {
			copy := link
			return &copy, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *sourcePursuitIntakeRepository) FindLinkBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.PursuitLink, error) {
	for _, link := range r.links {
		item := r.pursuits[link.PursuitID]
		if link.SourceURI == sourceURI && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity) {
			copy := link
			return &copy, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *sourcePursuitIntakeRepository) CreateActivity(activity *models.PursuitActivity) (*models.PursuitActivity, error) {
	if _, ok := r.pursuits[activity.PursuitID]; !ok {
		return nil, fmt.Errorf("pursuit %s not found", activity.PursuitID)
	}
	if activity.ID == uuid.Nil {
		activity.ID = uuid.New()
	}
	if activity.CreatedAt.IsZero() {
		activity.CreatedAt = time.Now().UTC()
	}
	r.activities[activity.PursuitID] = append(r.activities[activity.PursuitID], *activity)
	return activity, nil
}

func (r *sourcePursuitIntakeRepository) FindTaskAttempts(uuid.UUID, int) ([]models.PursuitTaskAttempt, error) {
	return []models.PursuitTaskAttempt{}, nil
}

func (r *sourcePursuitIntakeRepository) FindTaskAttemptsNeedingReview(uuid.UUID) ([]models.PursuitTaskAttempt, error) {
	return []models.PursuitTaskAttempt{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedWorkflows(ids []uuid.UUID) ([]models.WorkflowItem, error) {
	items := make([]models.WorkflowItem, 0, len(ids))
	for _, id := range ids {
		if item, ok := r.workflows[id]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedChecklistItems([]uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	return []models.WorkflowChecklistItem{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedOpenLoops([]uuid.UUID) ([]models.WorkflowOpenLoop, error) {
	return []models.WorkflowOpenLoop{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedProposals([]uuid.UUID) ([]models.WorkflowProposal, error) {
	return []models.WorkflowProposal{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedQualityGates([]uuid.UUID) ([]models.WorkflowQualityGate, error) {
	return []models.WorkflowQualityGate{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedDecisions([]uuid.UUID) ([]models.WorkflowDecision, error) {
	return []models.WorkflowDecision{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedTransitions([]uuid.UUID) ([]models.WorkflowTransition, error) {
	return []models.WorkflowTransition{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedSourceLinks([]uuid.UUID) ([]models.WorkflowSourceLink, error) {
	return []models.WorkflowSourceLink{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedEvents([]uuid.UUID) ([]models.WorkflowEvent, error) {
	return []models.WorkflowEvent{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedEvidence([]uuid.UUID) ([]models.WorkflowEvidenceClaim, error) {
	return []models.WorkflowEvidenceClaim{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedMemories([]uuid.UUID) ([]models.ContextMemory, error) {
	return []models.ContextMemory{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedConversations([]uuid.UUID) ([]models.AIConversationArchive, error) {
	return []models.AIConversationArchive{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedAmbientOpportunities([]uuid.UUID) ([]models.AmbientOpportunity, error) {
	return []models.AmbientOpportunity{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedSourceItems([]uuid.UUID) ([]models.SourceRawItem, error) {
	return []models.SourceRawItem{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedExtractions([]uuid.UUID) ([]models.SourceExtraction, error) {
	return []models.SourceExtraction{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedVerificationRuns([]uuid.UUID) ([]models.VerificationRun, error) {
	return []models.VerificationRun{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedVerificationClaims([]uuid.UUID) ([]models.VerificationClaim, error) {
	return []models.VerificationClaim{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedVerificationEvidence([]uuid.UUID) ([]models.VerificationEvidence, error) {
	return []models.VerificationEvidence{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedAutomations([]uuid.UUID) ([]models.Automation, error) {
	return []models.Automation{}, nil
}

func (r *sourcePursuitIntakeRepository) FindLinkedAutomationLaunches([]uuid.UUID, []uuid.UUID, int) ([]models.AutomationLaunchEvent, error) {
	return []models.AutomationLaunchEvent{}, nil
}

func (r *sourcePursuitIntakeRepository) FindAllForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error) {
	items, err := r.FindAll(includeArchived)
	if err != nil {
		return nil, err
	}
	visible := items[:0]
	for _, item := range items {
		if item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity {
			visible = append(visible, item)
		}
	}
	return visible, nil
}

type sourcePursuitWorkflowFixture struct {
	*fakeSourceWorkflowService
	repo *sourcePursuitIntakeRepository
}

func (s *sourcePursuitWorkflowFixture) Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	if s.fakeSourceWorkflowService == nil {
		s.fakeSourceWorkflowService = &fakeSourceWorkflowService{}
	}
	record, err := s.fakeSourceWorkflowService.Intake(request)
	if err == nil && record != nil && record.Item.ID != uuid.Nil {
		if s.repo.workflows == nil {
			s.repo.workflows = map[uuid.UUID]models.WorkflowItem{}
		}
		s.repo.workflows[record.Item.ID] = record.Item
	}
	return record, err
}

func hasPursuitTestLink(links map[uuid.UUID]models.PursuitLink, pursuitID uuid.UUID, linkType string) bool {
	for _, link := range links {
		if link.PursuitID == pursuitID && link.LinkType == linkType {
			return true
		}
	}
	return false
}

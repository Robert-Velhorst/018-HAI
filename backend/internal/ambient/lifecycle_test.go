package ambient

import (
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestOpportunityScorePenalizesRiskWithoutHidingUrgentSafetyWork(t *testing.T) {
	need := models.AmbientNeed{
		CurrentLevel:   35,
		TargetLevel:    90,
		PriorityWeight: 100,
	}
	item := models.AmbientOpportunity{
		Urgency:          100,
		Impact:           85,
		Effort:           35,
		Confidence:       85,
		Risk:             80,
		RequiresApproval: true,
	}
	if got := opportunityScore(item, need); got < 35 {
		t.Fatalf("urgent safety work score = %d, expected it to remain visible", got)
	}
}

func TestScanRejectsConcurrentRun(t *testing.T) {
	engine := NewService(&ambientRepositoryStub{}, nil, nil).(*service)
	engine.scanning.Store(true)

	if _, err := engine.Scan("manual"); !errors.Is(err, ErrScanInProgress) {
		t.Fatalf("Scan error = %v, want ErrScanInProgress", err)
	}
}

func TestAcceptedOpportunityCannotBeDismissed(t *testing.T) {
	workflowID := uuid.New()
	item := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		WorkflowID:    &workflowID,
		Status:        StatusAccepted,
	}
	engine := NewService(&ambientRepositoryStub{opportunity: item}, nil, nil)

	if _, err := engine.Dismiss(item.ID, ResolutionRequest{}); err == nil {
		t.Fatalf("expected accepted opportunity dismissal to be rejected")
	}
}

func TestAcceptProposedOpportunityStoresResolutionNote(t *testing.T) {
	workflowID := uuid.New()
	item := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		WorkflowID:    &workflowID,
		Status:        StatusProposed,
	}
	repo := &ambientRepositoryStub{opportunity: item}
	engine := NewService(repo, nil, nil)

	accepted, err := engine.Accept(item.ID, ResolutionRequest{Note: "Proceed through the approval queue."})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.Status != StatusAccepted {
		t.Fatalf("status = %q, want accepted", accepted.Status)
	}
	if accepted.ResolutionNote == "" {
		t.Fatalf("expected operator resolution note to be retained")
	}
}

func TestAcceptOpportunityStoresAmbientLearningMemory(t *testing.T) {
	workflowID := uuid.New()
	item := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		WorkflowID:    &workflowID,
		Status:        StatusProposed,
		NeedKey:       "safety",
		Title:         "Prepare lawyer follow-up",
		Rationale:     "A legal workflow is waiting for a reply.",
		NextAction:    "Draft a formal lawyer follow-up with evidence links.",
		SourceType:    "workflow",
		SourceURI:     "workflow://legal-follow-up",
	}
	memorySpy := &ambientMemorySpy{}
	engine := NewService(&ambientRepositoryStub{opportunity: item}, nil, nil, memorySpy)

	if _, err := engine.Accept(item.ID, ResolutionRequest{OwnerIdentity: "alice"}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if len(memorySpy.created) != 1 {
		t.Fatalf("stored %d memories, want 1", len(memorySpy.created))
	}
	created := memorySpy.created[0]
	if created.Kind != "lesson" || !strings.Contains(created.Content, "similar proactive suggestions may be useful") {
		t.Fatalf("memory content did not capture accepted ambient lesson: %#v", created)
	}
	if !strings.Contains(strings.Join(created.Tags, ","), "ambient_opportunity_accepted") {
		t.Fatalf("memory tags = %#v, want accepted ambient signal", created.Tags)
	}
	if len(memorySpy.ownerCreateOwners) != 1 || memorySpy.ownerCreateOwners[0] != "alice" {
		t.Fatalf("ambient lesson owners = %#v, want alice", memorySpy.ownerCreateOwners)
	}
}

func TestDismissOpportunityStoresCorrectionMemoryWhenNoteIsUseful(t *testing.T) {
	item := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "belonging",
		Title:         "Follow up with client",
		Rationale:     "A message appears unanswered.",
		NextAction:    "Send a client follow-up draft.",
		SourceType:    "workflow_open_loop",
		SourceURI:     "workflow://client-loop",
	}
	memorySpy := &ambientMemorySpy{}
	engine := NewService(&ambientRepositoryStub{opportunity: item}, nil, nil, memorySpy)

	_, err := engine.Dismiss(item.ID, ResolutionRequest{OwnerIdentity: "alice", Note: "Do not suggest client follow-ups until the quote status has been checked."})
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if len(memorySpy.created) != 1 {
		t.Fatalf("stored %d memories, want 1", len(memorySpy.created))
	}
	created := memorySpy.created[0]
	if created.Confidence < 0.79 {
		t.Fatalf("confidence = %.2f, want strong correction signal", created.Confidence)
	}
	if !strings.Contains(created.Content, "avoid similar proactive suggestions") {
		t.Fatalf("memory content did not capture dismissal correction: %s", created.Content)
	}
}

func TestDismissOpportunityWithoutUsefulNoteDoesNotStoreMemory(t *testing.T) {
	item := &models.AmbientOpportunity{
		ID:         uuid.New(),
		Status:     StatusProposed,
		NeedKey:    "growth",
		Title:      "Review open idea",
		NextAction: "Review open idea.",
	}
	memorySpy := &ambientMemorySpy{}
	engine := NewService(&ambientRepositoryStub{opportunity: item}, nil, nil, memorySpy)

	if _, err := engine.Dismiss(item.ID, ResolutionRequest{Note: "no"}); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if len(memorySpy.created) != 0 {
		t.Fatalf("stored %d memories, want no low-signal dismissal memory", len(memorySpy.created))
	}
}

type ambientRepositoryStub struct {
	opportunity                *models.AmbientOpportunity
	needs                      []models.AmbientNeed
	overrides                  []models.AmbientNeedOverride
	scans                      []models.AmbientScan
	saveOpportunityCalls       int
	opportunitiesForOwnerCalls int
	lastScanLimit              int
}

func (r *ambientRepositoryStub) Needs() ([]models.AmbientNeed, error) {
	return r.needs, nil
}

func (r *ambientRepositoryStub) NeedOverridesForOwner(ownerIdentity string) ([]models.AmbientNeedOverride, error) {
	result := make([]models.AmbientNeedOverride, 0, len(r.overrides))
	for _, override := range r.overrides {
		if override.OwnerIdentity == ownerIdentity {
			result = append(result, override)
		}
	}
	return result, nil
}

func (r *ambientRepositoryStub) FindNeedOverride(ownerIdentity, needKey string) (*models.AmbientNeedOverride, error) {
	for _, override := range r.overrides {
		if override.OwnerIdentity == ownerIdentity && override.NeedKey == needKey {
			copy := override
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *ambientRepositoryStub) SaveNeedOverride(override *models.AmbientNeedOverride) (*models.AmbientNeedOverride, error) {
	copy := *override
	for index, existing := range r.overrides {
		if existing.OwnerIdentity == copy.OwnerIdentity && existing.NeedKey == copy.NeedKey {
			r.overrides[index] = copy
			return &copy, nil
		}
	}
	r.overrides = append(r.overrides, copy)
	return &copy, nil
}

func (r *ambientRepositoryStub) FindOpportunity(uuid.UUID) (*models.AmbientOpportunity, error) {
	if r.opportunity == nil {
		return nil, errors.New("opportunity not found")
	}
	copy := *r.opportunity
	return &copy, nil
}

func (r *ambientRepositoryStub) FindOpportunityByFingerprint(fingerprint string) (*models.AmbientOpportunity, error) {
	if r.opportunity != nil && r.opportunity.Fingerprint != "" && r.opportunity.Fingerprint == fingerprint {
		copy := *r.opportunity
		return &copy, nil
	}
	return nil, nil
}

func (r *ambientRepositoryStub) SaveOpportunity(item *models.AmbientOpportunity) (*models.AmbientOpportunity, error) {
	r.saveOpportunityCalls++
	copy := *item
	r.opportunity = &copy
	return item, nil
}

func (r *ambientRepositoryStub) Opportunities(string, int) ([]models.AmbientOpportunity, error) {
	if r.opportunity == nil {
		return nil, nil
	}
	return []models.AmbientOpportunity{*r.opportunity}, nil
}

func (r *ambientRepositoryStub) OpportunitiesForOwner(ownerIdentity, status string, limit int) ([]models.AmbientOpportunity, error) {
	r.opportunitiesForOwnerCalls++
	items, _ := r.Opportunities(status, limit)
	result := make([]models.AmbientOpportunity, 0, len(items))
	for _, item := range items {
		if item.OwnerIdentity == ownerIdentity {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *ambientRepositoryStub) CreateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	r.scans = append(r.scans, *scan)
	return scan, nil
}

func (r *ambientRepositoryStub) UpdateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	return scan, nil
}

func (r *ambientRepositoryStub) Scans(int) ([]models.AmbientScan, error) {
	return r.scans, nil
}

func (r *ambientRepositoryStub) ScansForOwner(ownerIdentity string, limit int) ([]models.AmbientScan, error) {
	r.lastScanLimit = limit
	result := []models.AmbientScan{}
	for _, scan := range r.scans {
		if scan.OwnerIdentity == ownerIdentity {
			result = append(result, scan)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *ambientRepositoryStub) PruneScans(int) error {
	return nil
}

type ambientMemorySpy struct {
	created           []memory.CreateRequest
	ownerCreateOwners []string
}

func (s *ambientMemorySpy) Create(request memory.CreateRequest) (*models.ContextMemory, error) {
	s.created = append(s.created, request)
	return &models.ContextMemory{ID: uuid.New(), Kind: request.Kind, Content: request.Content, Summary: request.Summary, Confidence: request.Confidence}, nil
}

func (s *ambientMemorySpy) CreateForOwner(ownerIdentity string, request memory.CreateRequest) (*models.ContextMemory, error) {
	s.ownerCreateOwners = append(s.ownerCreateOwners, ownerIdentity)
	created, err := s.Create(request)
	if created != nil {
		created.OwnerIdentity = ownerIdentity
	}
	return created, err
}

func (s *ambientMemorySpy) Update(uuid.UUID, memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *ambientMemorySpy) FindAll(string, bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *ambientMemorySpy) FindByID(uuid.UUID) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *ambientMemorySpy) Archive(uuid.UUID, bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *ambientMemorySpy) Delete(uuid.UUID) error {
	return nil
}

func (s *ambientMemorySpy) Retrieve(memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	return &memory.RetrieveResult{}, nil
}

func (s *ambientMemorySpy) UpdateForOwner(_ string, id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return s.Update(id, request)
}

func (s *ambientMemorySpy) FindAllForOwner(_ string, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return s.FindAll(projectKey, includeArchived)
}

func (s *ambientMemorySpy) FindByIDForOwner(_ string, id uuid.UUID) (*models.ContextMemory, error) {
	return s.FindByID(id)
}

func (s *ambientMemorySpy) ArchiveForOwner(_ string, id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return s.Archive(id, archived)
}

func (s *ambientMemorySpy) DeleteForOwner(_ string, id uuid.UUID) error {
	return s.Delete(id)
}

func (s *ambientMemorySpy) RetrieveForOwner(_ string, request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	return s.Retrieve(request)
}

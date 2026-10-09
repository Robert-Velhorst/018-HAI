package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type projectDossierMemoryStub struct {
	memory.Service
	memory.OwnerScopedService
	items      []models.ContextMemory
	owner      string
	projectKey string
	includeOld bool
	limit      int
	readErr    error
}

type projectDossierUnscopedMemoryStub struct {
	memory.Service
}

func (m *projectDossierMemoryStub) FindAllForOwner(owner, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	m.owner, m.projectKey, m.includeOld = owner, projectKey, includeArchived
	if m.readErr != nil {
		return nil, m.readErr
	}
	var result []models.ContextMemory
	for _, item := range m.items {
		if item.OwnerIdentity == owner && item.ProjectKey == projectKey && (includeArchived || !item.Archived) {
			result = append(result, item)
		}
	}
	return result, nil
}

func (m *projectDossierMemoryStub) RecentForOwner(owner, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error) {
	m.owner, m.projectKey, m.includeOld, m.limit = owner, projectKey, includeArchived, limit
	if m.readErr != nil {
		return nil, m.readErr
	}
	items, err := m.FindAllForOwner(owner, projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *fakeWorkflowRepo) FindProjectDossierItemsForOwner(owner, projectKey string, limit int) ([]models.WorkflowItem, int64, error) {
	var result []models.WorkflowItem
	for _, item := range r.items {
		if item.OwnerIdentity != owner || item.ProjectKey != projectKey || item.Archived ||
			item.CurrentState == StateCompleted || item.CurrentState == StateArchived {
			continue
		}
		result = append(result, *item)
	}
	sortProjectDossierItems(result)
	count := int64(len(result))
	if len(result) > limit {
		result = result[:limit]
	}
	return result, count, nil
}

func (r *fakeWorkflowRepo) FindProjectDossierContextForOwner(owner, projectKey string, workflowID uuid.UUID, limit int) (ProjectDossierWorkflowContext, error) {
	item := r.items[workflowID]
	if item == nil || item.OwnerIdentity != owner || item.ProjectKey != projectKey || item.Archived ||
		item.CurrentState == StateCompleted || item.CurrentState == StateArchived {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierWorkflowNotFound
	}
	context := ProjectDossierWorkflowContext{
		Item:        *item,
		Checklist:   append([]models.WorkflowChecklistItem(nil), r.checklist[workflowID]...),
		OpenLoops:   append([]models.WorkflowOpenLoop(nil), r.openLoops[workflowID]...),
		SourceLinks: append([]models.WorkflowSourceLink(nil), r.sourceLinks[workflowID]...),
		Evidence:    append([]models.WorkflowEvidenceClaim(nil), r.evidence[workflowID]...),
		Decisions:   append([]models.WorkflowDecision(nil), r.decisions[workflowID]...),
	}
	if limit <= 0 {
		limit = ProjectDossierContextLimit
	}
	if len(context.Checklist) > limit {
		context.Checklist = context.Checklist[:limit]
		context.Truncated = true
	}
	if len(context.OpenLoops) > limit {
		context.OpenLoops = context.OpenLoops[:limit]
		context.Truncated = true
	}
	if len(context.SourceLinks) > limit {
		context.SourceLinks = context.SourceLinks[:limit]
		context.Truncated = true
	}
	if len(context.Evidence) > limit {
		context.Evidence = context.Evidence[:limit]
		context.Truncated = true
	}
	if len(context.Decisions) > limit {
		context.Decisions = context.Decisions[:limit]
		context.Truncated = true
	}
	return context, nil
}

func sortProjectDossierItems(items []models.WorkflowItem) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].PriorityScore > items[i].PriorityScore ||
				(items[j].PriorityScore == items[i].PriorityScore && items[j].UpdatedAt.After(items[i].UpdatedAt)) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func newProjectDossierTestService(repo *fakeWorkflowRepo, memories memory.Service) ProjectDossierService {
	return NewProjectDossierService(repo, memories)
}

func TestProjectDossierForOwnerScopesAndMatchesExactProjectKey(t *testing.T) {
	repo := newFakeWorkflowRepo()
	owned := models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "Case-A", Title: "Prepare case", CurrentState: StateReady,
		SourceType: "gmail", SourceURI: "https://mail.example.test/thread/1", SourceLabel: "Case email",
	}
	for _, item := range []models.WorkflowItem{
		owned,
		{ID: uuid.New(), OwnerIdentity: "bob", ProjectKey: "Case-A", Title: "Bob case", CurrentState: StateReady},
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "case-a", Title: "Different case", CurrentState: StateReady},
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "Case-A-other", Title: "Prefix case", CurrentState: StateReady},
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "Case-A", Title: "Completed", CurrentState: StateCompleted},
	} {
		copy := item
		repo.items[item.ID] = &copy
	}
	repo.checklist[owned.ID] = []models.WorkflowChecklistItem{{ID: uuid.New(), WorkflowID: owned.ID, Label: "Collect evidence", Status: "open"}}
	repo.openLoops[owned.ID] = []models.WorkflowOpenLoop{{ID: uuid.New(), WorkflowID: owned.ID, ResponsibleParty: "lawyer", WaitingFor: "hearing date", Status: "open"}}
	repo.sourceLinks[owned.ID] = []models.WorkflowSourceLink{{ID: uuid.New(), WorkflowID: owned.ID, SourceType: "email", SourceLabel: "Lawyer email", Relationship: "supports"}}
	repo.evidence[owned.ID] = []models.WorkflowEvidenceClaim{{ID: uuid.New(), WorkflowID: owned.ID, ClaimText: "A sourced claim", Reliability: "direct", Status: "verified"}}
	repo.decisions[owned.ID] = []models.WorkflowDecision{{ID: uuid.New(), WorkflowID: owned.ID, DecisionType: "routing", Decision: "legal", Reason: "matched by project key", Actor: "owner-secret-id"}}
	repo.intake[owned.ID] = []models.WorkflowIntakeRecord{{WorkflowID: owned.ID, RawContent: "private intake payload"}}

	memories := &projectDossierMemoryStub{items: []models.ContextMemory{
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "Case-A", Kind: "preference", Content: "Use factual wording", Confidence: 0.9},
		{ID: uuid.New(), OwnerIdentity: "bob", ProjectKey: "Case-A", Kind: "private", Content: "Bob only"},
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "case-a", Kind: "private", Content: "Other key"},
		{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "Case-A", Kind: "archived", Content: "Old", Archived: true},
	}}

	dossier, err := newProjectDossierTestService(repo, memories).ProjectDossierForOwner("alice", "Case-A")
	if err != nil {
		t.Fatalf("ProjectDossierForOwner: %v", err)
	}
	if dossier.ProjectKey != "Case-A" || len(dossier.Workflows) != 1 || dossier.Workflows[0].ID != owned.ID {
		t.Fatalf("workflow selection = %#v, want only exact owner/project active workflow", dossier.Workflows)
	}
	if memories.owner != "alice" || memories.projectKey != "Case-A" || memories.includeOld || memories.limit != ProjectDossierMemoryLimit+1 {
		t.Fatalf("memory query owner/project/archive/limit = %q/%q/%t/%d", memories.owner, memories.projectKey, memories.includeOld, memories.limit)
	}
	if len(dossier.Memories) != 1 || dossier.Memories[0].Content != "Use factual wording" {
		t.Fatalf("memories = %#v, want only active exact-project owner memory", dossier.Memories)
	}
	workflow := dossier.Workflows[0]
	if len(workflow.Checklist) != 1 || len(workflow.OpenLoops) != 1 || len(workflow.SourceLinks) != 2 || len(workflow.Evidence) != 1 || len(workflow.Decisions) != 1 {
		t.Fatalf("workflow context was not projected: %#v", workflow)
	}
	if workflow.SourceLinks[0].SourceType != "gmail" || workflow.SourceLinks[0].SourceLabel != "Case email" {
		t.Fatalf("workflow source metadata was omitted: %#v", workflow.SourceLinks[0])
	}
	encoded, err := json.Marshal(dossier)
	if err != nil {
		t.Fatalf("marshal dossier: %v", err)
	}
	for _, forbidden := range []string{"OwnerIdentity", "owner-secret-id", "\"actor\"", "private intake payload", "Bob only", "Other key"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestProjectDossierRejectsMissingOwnerOrProjectKey(t *testing.T) {
	repo := newFakeWorkflowRepo()
	memories := &projectDossierMemoryStub{}
	service := newProjectDossierTestService(repo, memories)
	for _, test := range []struct {
		name, owner, projectKey string
		wantErr                 error
	}{
		{name: "missing owner", projectKey: "Case-A", wantErr: ErrProjectDossierOwnerRequired},
		{name: "blank owner", owner: " \t", projectKey: "Case-A", wantErr: ErrProjectDossierOwnerRequired},
		{name: "missing project", owner: "alice", wantErr: ErrProjectDossierProjectKeyRequired},
		{name: "blank project", owner: "alice", projectKey: " \t", wantErr: ErrProjectDossierProjectKeyRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.ProjectDossierForOwner(test.owner, test.projectKey)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestProjectDossierEmptyResultHasGeneratedAtAndZeroCounts(t *testing.T) {
	dossier, err := newProjectDossierTestService(newFakeWorkflowRepo(), &projectDossierMemoryStub{}).
		ProjectDossierForOwner("alice", "empty-project")
	if err != nil {
		t.Fatalf("ProjectDossierForOwner: %v", err)
	}
	if dossier.GeneratedAt.IsZero() || dossier.GeneratedAt.Location() != time.UTC {
		t.Fatalf("generatedAt = %v, want non-zero UTC timestamp", dossier.GeneratedAt)
	}
	if len(dossier.Workflows) != 0 || len(dossier.Memories) != 0 || dossier.Counts.MatchingWorkflows != 0 ||
		dossier.Counts.ReturnedWorkflows != 0 || dossier.Counts.ReturnedMemories != 0 {
		t.Fatalf("empty dossier contains data/counts: %#v", dossier)
	}
}

func TestProjectDossierCapsWorkflowsMemoriesAndReportsTruncation(t *testing.T) {
	repo := newFakeWorkflowRepo()
	memories := &projectDossierMemoryStub{}
	for i := 0; i < ProjectDossierWorkflowLimit+3; i++ {
		item := &models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "bounded", Title: fmt.Sprintf("work %d", i), CurrentState: StateReady, UpdatedAt: time.Now().Add(time.Duration(i) * time.Second)}
		repo.items[item.ID] = item
	}
	for i := 0; i < ProjectDossierMemoryLimit+3; i++ {
		memories.items = append(memories.items, models.ContextMemory{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "bounded", Kind: "note", Content: fmt.Sprintf("memory %d", i), CreatedAt: time.Now().Add(time.Duration(i) * time.Second)})
	}
	dossier, err := newProjectDossierTestService(repo, memories).ProjectDossierForOwner("alice", "bounded")
	if err != nil {
		t.Fatalf("ProjectDossierForOwner: %v", err)
	}
	if len(dossier.Workflows) != ProjectDossierWorkflowLimit || dossier.Counts.MatchingWorkflows != int64(ProjectDossierWorkflowLimit+3) || !dossier.Truncated.Workflows {
		t.Fatalf("workflow cap/count/truncation = %d/%d/%t", len(dossier.Workflows), dossier.Counts.MatchingWorkflows, dossier.Truncated.Workflows)
	}
	if len(dossier.Memories) != ProjectDossierMemoryLimit || dossier.Counts.ReturnedMemories != ProjectDossierMemoryLimit || !dossier.Truncated.Memories {
		t.Fatalf("memory cap/count/truncation = %d/%d/%t", len(dossier.Memories), dossier.Counts.ReturnedMemories, dossier.Truncated.Memories)
	}
}

func TestProjectDossierBoundsNestedContextAndRedactsCredentials(t *testing.T) {
	repo := newFakeWorkflowRepo()
	item := &models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice", ProjectKey: "secrets", Title: "Review password=hunter2 and AKIAABCDEFGHIJKLMNOP", CurrentState: StateReady}
	repo.items[item.ID] = item
	for i := 0; i < ProjectDossierContextLimit+1; i++ {
		repo.checklist[item.ID] = append(repo.checklist[item.ID], models.WorkflowChecklistItem{ID: uuid.New(), WorkflowID: item.ID, Label: "step", Status: "open"})
	}
	repo.sourceLinks[item.ID] = []models.WorkflowSourceLink{{ID: uuid.New(), WorkflowID: item.ID, SourceType: "drive", SourceURI: "https://private-user:private-pass@example.test/file?access_token=top-secret", SourceLabel: "evidence"}}
	dossier, err := newProjectDossierTestService(repo, &projectDossierMemoryStub{}).ProjectDossierForOwner("alice", "secrets")
	if err != nil {
		t.Fatalf("ProjectDossierForOwner: %v", err)
	}
	if len(dossier.Workflows[0].Checklist) != ProjectDossierContextLimit || !dossier.Truncated.WorkflowContext {
		t.Fatalf("nested cap/truncation = %d/%t", len(dossier.Workflows[0].Checklist), dossier.Truncated.WorkflowContext)
	}
	encoded, err := json.Marshal(dossier)
	if err != nil {
		t.Fatalf("marshal dossier: %v", err)
	}
	for _, forbidden := range []string{"hunter2", "AKIAABCDEFGHIJKLMNOP", "private-user", "private-pass", "top-secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("response leaked credential %q: %s", forbidden, encoded)
		}
	}
}

func TestProjectDossierFailsClosedWithoutOwnerScopedRecentMemory(t *testing.T) {
	for _, test := range []struct {
		name string
		mem  memory.Service
	}{
		{name: "missing memory service"},
		{name: "unscoped memory service", mem: &projectDossierUnscopedMemoryStub{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := newProjectDossierTestService(newFakeWorkflowRepo(), test.mem).
				ProjectDossierForOwner("alice", "Case-A")
			if !errors.Is(err, ErrProjectDossierMemoryUnavailable) {
				t.Fatalf("error = %v, want %v", err, ErrProjectDossierMemoryUnavailable)
			}
		})
	}
}

func TestProjectDossierFailsClosedWhenOwnerMemoryReadFails(t *testing.T) {
	memories := &projectDossierMemoryStub{readErr: errors.New("private database diagnostics")}
	_, err := newProjectDossierTestService(newFakeWorkflowRepo(), memories).ProjectDossierForOwner("alice", "Case-A")
	if !errors.Is(err, ErrProjectDossierMemoryUnavailable) {
		t.Fatalf("error = %v, want %v", err, ErrProjectDossierMemoryUnavailable)
	}
	if strings.Contains(fmt.Sprint(err), "private database diagnostics") {
		t.Fatal("service error should not contain private storage diagnostics")
	}
}

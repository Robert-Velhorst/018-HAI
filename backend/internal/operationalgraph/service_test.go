package operationalgraph

import (
	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/knowledgegraph"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type agentStub struct{ records []agentregistry.Agent }

func (s agentStub) List(_ context.Context, owner string) ([]agentregistry.Agent, error) {
	out := []agentregistry.Agent{}
	for _, record := range s.records {
		if record.OwnerIdentity == owner {
			out = append(out, record)
		}
	}
	return out, nil
}
func (s agentStub) Get(_ context.Context, owner, id string) (agentregistry.Agent, error) {
	for _, record := range s.records {
		if record.OwnerIdentity == owner && record.ID == id {
			return record, nil
		}
	}
	return agentregistry.Agent{}, agentregistry.ErrNotFound
}

type teamStub struct {
	records []frameworkregistry.AgentTeamContract
}

func (s teamStub) ListTeams(string) ([]frameworkregistry.AgentTeamContract, error) {
	return s.records, nil
}

type workflowStub struct{ records []models.WorkflowItem }

func (s workflowStub) ItemsForOwner(owner string, _ bool) ([]models.WorkflowItem, error) {
	out := []models.WorkflowItem{}
	for _, record := range s.records {
		if record.OwnerIdentity == owner {
			out = append(out, record)
		}
	}
	return out, nil
}

type pursuitStub struct{ records []models.Pursuit }

func (s pursuitStub) ListForOwner(owner string, _ bool) ([]models.Pursuit, error) {
	out := []models.Pursuit{}
	for _, record := range s.records {
		if record.OwnerIdentity == owner {
			out = append(out, record)
		}
	}
	return out, nil
}

type sourceStub struct{ records []models.ConnectedSource }

func (s sourceStub) SourcesForOwner(owner string, _ bool) ([]models.ConnectedSource, error) {
	out := []models.ConnectedSource{}
	for _, record := range s.records {
		if record.OwnerIdentity == owner {
			out = append(out, record)
		}
	}
	return out, nil
}

type memoryStub struct{ records []models.ContextMemory }

func (s *memoryStub) FindAllForOwner(owner, project string, _ bool) ([]models.ContextMemory, error) {
	out := []models.ContextMemory{}
	for _, record := range s.records {
		if record.OwnerIdentity == owner && (project == "" || record.ProjectKey == project) {
			out = append(out, record)
		}
	}
	return out, nil
}
func (s *memoryStub) CreateForOwner(owner string, request memory.CreateRequest) (*models.ContextMemory, error) {
	record := models.ContextMemory{ID: uuid.New(), OwnerIdentity: owner, ProjectKey: request.ProjectKey, Kind: request.Kind, Content: request.Content, Summary: request.Summary, Confidence: request.Confidence, SourceURI: request.SourceURI, SourceLabel: request.SourceLabel, CreatedAt: testTime, UpdatedAt: testTime}
	s.records = append(s.records, record)
	return &record, nil
}

var testTime = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func TestSnapshotIsOwnerScopedAndRedactsSecretMetadata(t *testing.T) {
	kg := knowledgegraph.NewMemoryRepository()
	writer := knowledgegraph.NewService(kg, func() time.Time { return testTime })
	_, err := writer.CreateNode(context.Background(), knowledgegraph.CreateNodeRequest{OwnerIdentity: "robert", Kind: knowledgegraph.NodeProject, DeduplicationKey: "hai", Label: "HAI", Properties: map[string]string{"apiToken": "do-not-leak", "stage": "active"}, Confidence: 1, VerificationStatus: knowledgegraph.VerificationVerified, Sensitivity: knowledgegraph.SensitivityInternal, LocalOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.CreateNode(context.Background(), knowledgegraph.CreateNodeRequest{OwnerIdentity: "other", Kind: knowledgegraph.NodeProject, DeduplicationKey: "private", Label: "Other owner", Confidence: 1, VerificationStatus: knowledgegraph.VerificationVerified, Sensitivity: knowledgegraph.SensitivityInternal, LocalOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	mem := &memoryStub{records: []models.ContextMemory{{ID: uuid.New(), OwnerIdentity: "robert", ProjectKey: "018-HAI", Kind: "decision", Content: "Use the canonical stack", Summary: "Canonical stack", Confidence: .9, UpdatedAt: testTime}, {ID: uuid.New(), OwnerIdentity: "other", Kind: "secret", Content: "Other owner's memory", Summary: "Leak", Confidence: 1, UpdatedAt: testTime}}}
	service := mustService(t, kg, writer, agentStub{}, teamStub{}, workflowStub{}, pursuitStub{}, sourceStub{records: []models.ConnectedSource{{ID: uuid.New(), OwnerIdentity: "robert", Name: "Trello", Category: "project", ConnectorKey: "trello", Permissions: "read", Status: "active", LocalOnly: true}, {ID: uuid.New(), OwnerIdentity: "other", Name: "Other source", Category: "mail", ConnectorKey: "gmail", Status: "active"}}}, mem)
	snapshot, err := service.Snapshot(context.Background(), "robert")
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]Node{}
	for _, node := range snapshot.Nodes {
		labels[node.Label] = node
	}
	if _, ok := labels["Other owner"]; ok {
		t.Fatal("another owner's knowledge node was projected")
	}
	if _, ok := labels["Leak"]; ok {
		t.Fatal("another owner's memory was projected")
	}
	if _, ok := labels["Other source"]; ok {
		t.Fatal("another owner's source was projected")
	}
	hai, ok := labels["HAI"]
	if !ok {
		t.Fatal("expected HAI node")
	}
	if _, ok := hai.Details["apiToken"]; ok {
		t.Fatal("secret-like metadata was exposed")
	}
	if hai.Details["stage"] != "active" {
		t.Fatalf("expected safe metadata, got %#v", hai.Details)
	}
	if snapshot.Quality.LocalOnlyNodes == 0 || snapshot.LayerCounts["knowledge"] == 0 || snapshot.LayerCounts["memory"] == 0 {
		t.Fatalf("unexpected snapshot quality: %#v %#v", snapshot.Quality, snapshot.LayerCounts)
	}
}

func TestAgentBootUsesStrictestTeamAndRoleConstraints(t *testing.T) {
	agent := agentregistry.Agent{ID: "planner", OwnerIdentity: "robert", Name: "Planner", AuthorityCeiling: 5, AutonomyCeiling: 4, Runtime: agentregistry.RuntimeAdapter{ID: "local", Type: "script"}, Health: agentregistry.HealthEvidence{Status: agentregistry.HealthHealthy}, ToolAllowlist: []string{"search", "read"}, Capabilities: []agentregistry.CapabilityDeclaration{{ID: "plan"}}}
	team := frameworkregistry.AgentTeamContract{ID: "team-1", Version: "1", Name: "Review team", Status: frameworkregistry.AgentTeamActive, AuthorityCeiling: 4, RiskCeiling: frameworkregistry.TeamRiskHigh, Members: []frameworkregistry.TeamMembership{{AgentID: "planner", Status: frameworkregistry.TeamMemberActive, AuthorityCeiling: 3, RiskCeiling: frameworkregistry.TeamRiskMedium, RoleIDs: []string{"reviewer"}, CapabilityIDs: []string{"review"}}}, Roles: []frameworkregistry.TeamRoleContract{{ID: "reviewer", AuthorityCeiling: 2, RiskCeiling: frameworkregistry.TeamRiskLow, ProhibitedActions: []string{"send_email", "spend_money"}, EvidenceRequirements: []string{"source_reference"}}}}
	kg := knowledgegraph.NewMemoryRepository()
	service := mustService(t, kg, knowledgegraph.NewService(kg, func() time.Time { return testTime }), agentStub{records: []agentregistry.Agent{agent}}, teamStub{records: []frameworkregistry.AgentTeamContract{team}}, workflowStub{}, pursuitStub{}, sourceStub{}, &memoryStub{})
	boot, err := service.AgentBoot(context.Background(), "robert", "planner")
	if err != nil {
		t.Fatal(err)
	}
	if boot.AuthorityCeiling != 2 || boot.RiskCeiling != "low" {
		t.Fatalf("constraints were not narrowed: %#v", boot)
	}
	if boot.GrantsExecutionAuthority || !boot.ExecutionAuthorizationRequired {
		t.Fatal("boot context incorrectly grants execution authority")
	}
	if len(boot.ProhibitedActions) != 2 || len(boot.EvidenceRequirements) != 1 || len(boot.Teams) != 1 {
		t.Fatalf("team context missing: %#v", boot)
	}
}

func TestSearchNeighborhoodPathAndGovernedWrites(t *testing.T) {
	kg := knowledgegraph.NewMemoryRepository()
	writer := knowledgegraph.NewService(kg, func() time.Time { return testTime })
	mem := &memoryStub{}
	workflowID := uuid.New()
	pursuitID := uuid.New()
	service := mustService(t, kg, writer, agentStub{}, teamStub{}, workflowStub{records: []models.WorkflowItem{{ID: workflowID, OwnerIdentity: "robert", Title: "Review legal reply", ProjectKey: "Vivare", CurrentState: "blocked", PriorityScore: 90, BlockedReason: "Needs approval", UpdatedAt: testTime}}}, pursuitStub{records: []models.Pursuit{{ID: pursuitID, OwnerIdentity: "robert", Title: "Resolve Vivare case", ProjectKey: "Vivare", Status: "active", PriorityScore: 95, UpdatedAt: testTime}}}, sourceStub{}, mem)
	search, err := service.Search(context.Background(), "robert", "legal", "work", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Results) != 1 || search.Results[0].Kind != "workflow" {
		t.Fatalf("unexpected search: %#v", search)
	}
	neighbor, err := service.Neighborhood(context.Background(), "robert", "project:vivare", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbor.Nodes) < 3 {
		t.Fatalf("expected project relationships, got %#v", neighbor)
	}
	path, err := service.Path(context.Background(), "robert", "pursuit:"+pursuitID.String(), "workflow:"+workflowID.String(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if !path.Found || len(path.NodeIDs) != 3 {
		t.Fatalf("expected pursuit-project-workflow path, got %#v", path)
	}
	written, err := service.RecordMemory("robert", MemoryWriteRequest{Kind: "decision", Content: "Keep external actions approval-gated", Summary: "Approval boundary", Confidence: .95})
	if err != nil {
		t.Fatal(err)
	}
	if written.OwnerIdentity != "robert" || len(mem.records) != 1 {
		t.Fatalf("memory was not owner scoped: %#v", written)
	}
	memorySearch, err := service.Search(context.Background(), "robert", "approval boundary", "memory", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(memorySearch.Results) != 1 {
		t.Fatalf("governed memory write did not invalidate the cached projection: %#v", memorySearch)
	}
	report, err := service.RecordReport(context.Background(), "robert", ReportWriteRequest{AgentID: "planner", Status: "warn", Summary: "Source is stale", Details: "Refresh before execution"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Node.VerificationStatus != knowledgegraph.VerificationNeedsReview || !report.Node.LocalOnly {
		t.Fatalf("report bypassed review defaults: %#v", report.Node)
	}
	reportSearch, err := service.Search(context.Background(), "robert", "source is stale", "knowledge", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reportSearch.Results) != 1 {
		t.Fatalf("governed report write did not invalidate the cached projection: %#v", reportSearch)
	}
	if _, err := service.RecordMemory(" ", MemoryWriteRequest{Content: "must not be stored"}); err == nil {
		t.Fatal("memory write accepted an empty authenticated owner")
	}
	if _, err := service.RecordReport(context.Background(), "", ReportWriteRequest{Status: "ok", Summary: "must not be stored"}); err == nil {
		t.Fatal("report write accepted an empty authenticated owner")
	}
}

func mustService(t *testing.T, kg knowledgegraph.Repository, writer *knowledgegraph.Service, agents AgentLister, teams TeamLister, workflows WorkflowLister, pursuits PursuitLister, sources SourceLister, memories MemoryStore) *Service {
	t.Helper()
	service, err := NewService(kg, writer, agents, teams, workflows, pursuits, sources, memories, func() time.Time { return testTime })
	if err != nil {
		t.Fatal(err)
	}
	return service
}

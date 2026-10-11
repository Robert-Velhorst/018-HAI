package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/frameworkregistry"
)

var agentContextTestNow = time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)

type failingAgentListRepository struct {
	err error
}

func (r failingAgentListRepository) List(context.Context, string) ([]agentregistry.Agent, error) {
	return nil, r.err
}

type leakingAgentListRepository struct {
	agent agentregistry.Agent
}

type cancellationAwareAgentListRepository struct{}

func (cancellationAwareAgentListRepository) List(ctx context.Context, _ string) ([]agentregistry.Agent, error) {
	return nil, ctx.Err()
}

func (r leakingAgentListRepository) List(context.Context, string) ([]agentregistry.Agent, error) {
	return []agentregistry.Agent{r.agent}, nil
}

type capturingFrameworkSelector struct {
	request frameworkregistry.SelectionRequest
}

func (s *capturingFrameworkSelector) PlanSelection(request frameworkregistry.SelectionRequest) (*frameworkregistry.SelectionDecision, error) {
	s.request = request
	return &frameworkregistry.SelectionDecision{
		ID:                   "agent-context-selection",
		TaskPlanID:           request.TaskPlanID,
		LifeDomain:           "work_and_income",
		NeedOrCommitment:     "complete assigned work",
		MaximumAutonomyLevel: 4,
		ConstitutionVersion:  1,
		Selected: []frameworkregistry.SelectedFramework{{
			ID: "least-authority", Version: "1.0.0", Name: "Least authority",
		}},
	}, nil
}

func TestRegistryAgentContextProviderMapsFreshOwnerScopedEvidence(t *testing.T) {
	repository := agentregistry.NewMemoryRepository()
	agent := validRegistryAgent("alice", agentContextTestNow)
	if _, err := repository.Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	provider, err := NewAgentContextProvider(repository)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	cards, err := provider.LatestAgents("alice", agentContextTestNow)
	if err != nil {
		t.Fatalf("latest agents: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	card := cards[0]
	if card.ID != agent.ID || card.Owner != "alice" || card.Role != "researcher" {
		t.Fatalf("identity mapping = %#v", card)
	}
	if !card.Verified || card.Status != "available" || card.HealthStatus != "available" {
		t.Fatalf("fresh health mapping = verified %t, status %q, health %q", card.Verified, card.Status, card.HealthStatus)
	}
	if card.AuthorityCeiling != 4 {
		t.Fatalf("authority ceiling = %d, want least of authority/autonomy (4)", card.AuthorityCeiling)
	}
	if !agentCardContainsString(card.Capabilities, "source-research@1.2.0") ||
		!agentCardContainsString(card.AllowedActions, "source-research.search") {
		t.Fatalf("capabilities/actions = %#v / %#v", card.Capabilities, card.AllowedActions)
	}
	if !agentCardContainsString(card.AllowedTools, "connected-sources.read") ||
		!agentCardContainsString(card.DataAccessBoundaries, "folder:C:/HAI/Research") {
		t.Fatalf("tool/boundary mapping = %#v / %#v", card.AllowedTools, card.DataAccessBoundaries)
	}
	if card.EvaluationScore <= 0 || card.EvaluationScore >= 1 ||
		card.EvaluationScoreSource != "agent registry success/failure beta prior" {
		t.Fatalf("reliability mapping = %f from %q", card.EvaluationScore, card.EvaluationScoreSource)
	}
	if card.LastVerifiedAt == nil || !card.LastVerifiedAt.Equal(agent.Health.CheckedAt) {
		t.Fatalf("last verified = %#v", card.LastVerifiedAt)
	}
	if !strings.Contains(card.Version, "revision-7") ||
		!strings.Contains(card.Provenance, "revision=7") {
		t.Fatalf("revision provenance = %q / %q", card.Version, card.Provenance)
	}

	otherCards, err := provider.LatestAgents("bob", agentContextTestNow)
	if err != nil {
		t.Fatalf("other owner query: %v", err)
	}
	if len(otherCards) != 0 {
		t.Fatalf("cross-owner cards leaked: %#v", otherCards)
	}
}

func TestRegistryAgentContextProviderKeepsStaleAndRevokedAgentsUnavailable(t *testing.T) {
	repository := agentregistry.NewMemoryRepository()
	stale := validRegistryAgent("alice", agentContextTestNow)
	stale.ID = "stale-researcher"
	stale.Health.CheckedAt = agentContextTestNow.Add(-2 * time.Hour)
	stale.Health.FreshFor = time.Hour
	disabled := validRegistryAgent("alice", agentContextTestNow)
	disabled.ID = "disabled-researcher"
	disabled.State = agentregistry.StateDisabled
	disabled.Health.Reason = "operator disabled runtime"
	for _, agent := range []agentregistry.Agent{stale, disabled} {
		if _, err := repository.Create(context.Background(), agent); err != nil {
			t.Fatalf("create %s: %v", agent.ID, err)
		}
	}
	provider, err := NewAgentContextProvider(repository)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	cards, err := provider.LatestAgents("alice", agentContextTestNow)
	if err != nil {
		t.Fatalf("latest agents: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	byID := map[string]frameworkregistry.AgentCard{}
	for _, card := range cards {
		byID[card.ID] = card
	}
	if byID[stale.ID].Verified || byID[stale.ID].LastVerifiedAt != nil {
		t.Fatalf("stale agent was verified: %#v", byID[stale.ID])
	}
	if !byID[disabled.ID].Revoked ||
		!strings.Contains(byID[disabled.ID].RevocationReason, "disabled") {
		t.Fatalf("disabled agent was not revoked: %#v", byID[disabled.ID])
	}
}

func TestRegistryAgentContextProviderFailsClosedOnRepositoryAndScopeErrors(t *testing.T) {
	provider, err := NewAgentContextProvider(failingAgentListRepository{err: errors.New("database unavailable")})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.LatestAgents("alice", agentContextTestNow); err == nil ||
		!strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("repository error = %v", err)
	}

	leaked := validRegistryAgent("bob", agentContextTestNow)
	provider, err = NewAgentContextProvider(leakingAgentListRepository{agent: leaked})
	if err != nil {
		t.Fatalf("new leaking provider: %v", err)
	}
	if _, err := provider.LatestAgents("alice", agentContextTestNow); err == nil ||
		!strings.Contains(err.Error(), "escaped owner scope") {
		t.Fatalf("scope error = %v", err)
	}
}

func TestTaskFrameworkSelectionLoadsRegistryAgentsOnlyWhenAbsent(t *testing.T) {
	repository := agentregistry.NewMemoryRepository()
	agent := validRegistryAgent("alice", time.Now().UTC())
	if _, err := repository.Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	provider, err := NewAgentContextProvider(repository)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	selector := &capturingFrameworkSelector{}
	svc := NewServiceWithDependenciesAndAgentContext(
		&fakeMemoryService{},
		newTaskTestLLMService(t),
		nil,
		nil,
		nil,
		nil,
		selector,
		NewMemoryTaskStateRepository(),
		nil,
		provider,
	).(*service)

	if _, err := svc.buildPlan(IntakeRequest{
		OwnerIdentity: "alice",
		Request:       "Research the connected evidence and prepare a verified summary.",
	}, false, false); err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(selector.request.AvailableAgents) != 1 ||
		selector.request.AvailableAgents[0].ID != agent.ID {
		t.Fatalf("selector agents = %#v", selector.request.AvailableAgents)
	}

	explicit := frameworkregistry.AgentCard{ID: "explicit-agent"}
	selector.request = frameworkregistry.SelectionRequest{}
	if _, err := svc.buildPlan(IntakeRequest{
		OwnerIdentity:   "alice",
		Request:         "Research a second item.",
		AvailableAgents: []frameworkregistry.AgentCard{explicit},
	}, false, false); err != nil {
		t.Fatalf("build plan with explicit agent: %v", err)
	}
	if len(selector.request.AvailableAgents) != 1 ||
		selector.request.AvailableAgents[0].ID != explicit.ID {
		t.Fatalf("explicit agents were replaced: %#v", selector.request.AvailableAgents)
	}
}

func TestLoadOperatingContextFailsClosedOnAgentProviderError(t *testing.T) {
	provider, err := NewAgentContextProvider(failingAgentListRepository{err: errors.New("registry offline")})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	svc := &service{agentContext: provider}
	if _, err := svc.loadOperatingContext(IntakeRequest{OwnerIdentity: "alice"}); err == nil ||
		!strings.Contains(err.Error(), "registry offline") {
		t.Fatalf("agent provider error = %v", err)
	}

	explicit := []frameworkregistry.AgentCard{{ID: "trusted-in-process"}}
	got, err := svc.loadOperatingContext(IntakeRequest{
		OwnerIdentity:   "alice",
		AvailableAgents: explicit,
	})
	if err != nil {
		t.Fatalf("explicit cards should bypass provider: %v", err)
	}
	if len(got.AvailableAgents) != 1 || got.AvailableAgents[0].ID != explicit[0].ID {
		t.Fatalf("explicit cards changed: %#v", got.AvailableAgents)
	}
}

func TestLoadOperatingContextPropagatesTaskCancellationToAgentRegistry(t *testing.T) {
	provider, err := NewAgentContextProvider(cancellationAwareAgentListRepository{})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &service{agentContext: provider}

	_, err = svc.loadOperatingContext(IntakeRequest{
		OwnerIdentity:    "alice",
		executionContext: ctx,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load operating context error = %v, want context canceled", err)
	}
}

func validRegistryAgent(owner string, now time.Time) agentregistry.Agent {
	return agentregistry.Agent{
		ContractVersion: agentregistry.ContractVersion,
		ID:              "research-agent",
		OwnerIdentity:   owner,
		Name:            "Source research agent",
		Type:            agentregistry.AgentTypeResearcher,
		Runtime: agentregistry.RuntimeAdapter{
			ID:              "local-research-runtime",
			Type:            "local",
			ProtocolVersion: "1.0.0",
		},
		Capabilities: []agentregistry.CapabilityDeclaration{{
			ID:         "source-research",
			Version:    "1.2.0",
			Operations: []string{"search", "summarize"},
		}},
		AuthorityCeiling: 6,
		AutonomyCeiling:  4,
		ToolAllowlist:    []string{"connected-sources.read"},
		DataAllowlist:    []string{"source-metadata", "extracted-text"},
		FolderAllowlist:  []string{"C:/HAI/Research"},
		Health: agentregistry.HealthEvidence{
			Status:    agentregistry.HealthHealthy,
			Ready:     true,
			CheckedAt: now.Add(-5 * time.Minute),
			FreshFor:  time.Hour,
		},
		State: agentregistry.StateEnabled,
		Availability: agentregistry.Availability{
			Available:     true,
			MaxConcurrent: 2,
		},
		Performance: agentregistry.PerformanceProfile{
			EstimatedCostEUR: 0,
			P95LatencyMs:     450,
			Locality:         agentregistry.LocalityLocal,
		},
		Reliability: agentregistry.ReliabilityEvidence{
			Successes:     18,
			Failures:      2,
			MeanLatencyMs: 210,
			LastOutcomeAt: now.Add(-10 * time.Minute),
		},
		Revision:  7,
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-5 * time.Minute),
	}
}

func agentCardContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

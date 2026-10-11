package haios

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHAIOSRouteRequiresVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(nil, nil)

	unauthenticated := gin.New()
	unauthenticated.GET("/os/overview", handler.Overview)
	recorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/os/overview", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("direct overview status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}

	gated := gin.New()
	gatedRoutes := gated.Group("/os")
	gatedRoutes.Use(RequireAuthenticatedOwner())
	gatedRoutes.GET("/overview", handler.Overview)
	recorder = httptest.NewRecorder()
	gated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/os/overview", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("route owner gate status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(identity.ContextSubjectKey, "alice")
	if owner := pursuitOwner(context); owner != "alice" {
		t.Fatalf("pursuit owner = %q, want alice", owner)
	}
}

func TestHAIOSOverviewFailsClosedWhenMetricsAreUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/os/overview", handler.Overview)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/os/overview", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("overview status = %d, want %d; body=%s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "HAI OS metrics are unavailable") || strings.Contains(body, "database is unavailable") {
		t.Fatalf("overview body should be sanitized, got %s", body)
	}
}

func TestLiveProviderConfiguredIgnoresApprovalGatedRuntimeOnlyProvider(t *testing.T) {
	policy := llm.Policy{
		LocalModelsAllowed: true,
		Providers: []llm.Provider{
			{
				ID:         "odysseus",
				Name:       "Odysseus AI Workspace",
				Enabled:    true,
				Configured: true,
				Local:      true,
				Models: []llm.Model{
					{ID: "odysseus-workspace-agent", Enabled: true, Tier: llm.TierFree, RequiresApproval: true},
				},
			},
		},
	}

	if liveProviderConfigured(policy) {
		t.Fatalf("probe-only or approval-gated runtime provider should not satisfy executable LLM readiness")
	}
	evidence := liveProviderEvidence(policy)
	if !strings.Contains(evidence, "not executable") || !strings.Contains(evidence, "Odysseus AI Workspace") {
		t.Fatalf("evidence = %q, want non-executable Odysseus explanation", evidence)
	}
}

func TestLiveProviderConfiguredAcceptsNoApprovalLocalModel(t *testing.T) {
	policy := llm.Policy{
		LocalModelsAllowed: true,
		Providers: []llm.Provider{
			{
				ID:         "ollama",
				Name:       "Ollama local",
				Enabled:    true,
				Configured: true,
				Local:      true,
				Models: []llm.Model{
					{ID: "qwen2.5-coder:7b", Enabled: true, Tier: llm.TierFree},
				},
			},
		},
	}

	if !liveProviderConfigured(policy) {
		t.Fatalf("configured local no-approval model should satisfy executable LLM readiness")
	}
	if evidence := liveProviderEvidence(policy); !strings.Contains(evidence, "Ollama local") {
		t.Fatalf("evidence = %q, want executable provider name", evidence)
	}
}

func TestLiveProviderConfiguredIgnoresFreeCloudWithoutQuota(t *testing.T) {
	policy := llm.Policy{
		FreeCloudQuotaAllowed: true,
		Providers: []llm.Provider{
			{
				ID:             "free-cloud",
				Name:           "Configured free cloud quota",
				Enabled:        true,
				Configured:     true,
				Local:          false,
				Paid:           false,
				QuotaRemaining: 0,
				Models: []llm.Model{
					{ID: "free-best-available", Enabled: true, Tier: llm.TierFree},
				},
			},
		},
	}

	if liveProviderConfigured(policy) {
		t.Fatalf("free-cloud provider with exhausted or unknown quota should not satisfy executable readiness")
	}
	if evidence := liveProviderEvidence(policy); !strings.Contains(evidence, "quota") {
		t.Fatalf("evidence = %q, want quota explanation", evidence)
	}
}

func TestReadinessGatesDescribeImplementedReadOnlyConnectors(t *testing.T) {
	var connectors ReadinessGate
	for _, gate := range readinessGates(llm.Policy{}) {
		if gate.Name == "Real account connectors" {
			connectors = gate
			break
		}
	}
	if connectors.Name == "" {
		t.Fatal("real account connectors readiness gate is missing")
	}
	for _, fragment := range []string{"Trello REST", "GitHub REST", "Gmail OAuth", "Calendar and Drive remain export/local-folder paths"} {
		if !strings.Contains(connectors.Evidence, fragment) {
			t.Fatalf("connector evidence = %q, want %q", connectors.Evidence, fragment)
		}
	}
}

func TestPursuitOverviewPrioritizesRobertQueue(t *testing.T) {
	id := uuid.New()
	overview := pursuitOverviewFromDashboard(&pursuit.Dashboard{
		Counts: map[string]int64{"active": 2, "waiting": 1, "blocked": 0},
		NeedsRobert: []pursuit.PursuitListItem{
			{
				Pursuit:        models.Pursuit{ID: id, Title: "Vivare dispute", Status: pursuit.StatusActive, RiskLevel: "high"},
				NeedsRobert:    1,
				NextAction:     "Approve formal reply draft",
				DecisionCards:  1,
				LinkedEvidence: 2,
				TimelineItems:  3,
				OpenLoops:      1,
			},
		},
		VAReady: []pursuit.PursuitListItem{
			{Pursuit: models.Pursuit{ID: uuid.New(), Title: "Evidence bundle", Status: pursuit.StatusActive, RiskLevel: "medium"}},
		},
		SystemReady: []pursuit.PursuitListItem{
			{Pursuit: models.Pursuit{ID: uuid.New(), Title: "Index source folder", Status: pursuit.StatusActive, RiskLevel: "low"}},
		},
	})

	if overview.Status != "needs_robert" {
		t.Fatalf("status = %q, want needs_robert", overview.Status)
	}
	if overview.TotalActive != 3 || overview.NeedsRobert != 1 || overview.VAReady != 1 || overview.SystemReady != 1 {
		t.Fatalf("unexpected overview counts: %+v", overview)
	}
	if len(overview.Spotlight) != 3 || overview.Spotlight[0].ID != id.String() {
		t.Fatalf("spotlight = %+v, want Robert queue item first", overview.Spotlight)
	}
	if !strings.Contains(overview.Next, "Robert-only") {
		t.Fatalf("next = %q, want Robert-only guidance", overview.Next)
	}
	if overview.DecisionCards != 1 || overview.LinkedEvidence != 2 || overview.OpenLoops != 1 || overview.TimelineItems != 3 {
		t.Fatalf("evidence/provenance counts not aggregated: %+v", overview)
	}
	if overview.EvidenceStatus != "source_linked" {
		t.Fatalf("evidence status = %q, want source_linked", overview.EvidenceStatus)
	}
	if !strings.Contains(overview.Spotlight[0].EvidenceLine, "2 evidence") {
		t.Fatalf("spotlight evidence line = %q, want linked evidence detail", overview.Spotlight[0].EvidenceLine)
	}
}

func TestPursuitOverviewUsesOwnerScopedDashboard(t *testing.T) {
	spy := &ownerScopedPursuitDashboard{dashboard: &pursuit.Dashboard{Counts: map[string]int64{"active": 1}}}
	handler := NewHandler(nil, spy)
	overview := handler.pursuitOverview("alice")

	if spy.ownerIdentity != "alice" {
		t.Fatalf("dashboard owner = %q, want alice", spy.ownerIdentity)
	}
	if !overview.Enabled || overview.TotalActive != 1 {
		t.Fatalf("overview = %#v", overview)
	}
}

type ownerScopedPursuitDashboard struct {
	ownerIdentity string
	dashboard     *pursuit.Dashboard
}

func (s *ownerScopedPursuitDashboard) DashboardForOwner(ownerIdentity string) (*pursuit.Dashboard, error) {
	s.ownerIdentity = ownerIdentity
	return s.dashboard, nil
}

func TestPursuitOverviewEmptyStateStaysActionable(t *testing.T) {
	overview := pursuitOverviewFromDashboard(&pursuit.Dashboard{
		Counts: map[string]int64{"active": 0, "waiting": 0, "blocked": 0},
	})

	if overview.Status != "empty" {
		t.Fatalf("status = %q, want empty", overview.Status)
	}
	if overview.Summary == "" || !strings.Contains(overview.Next, "Create or import pursuits") {
		t.Fatalf("overview should explain the empty state: %+v", overview)
	}
	if len(overview.Queues) != 9 {
		t.Fatalf("queues = %d, want 9 operating queues", len(overview.Queues))
	}
}

func TestPursuitOverviewMarksActiveUngroundedPursuits(t *testing.T) {
	overview := pursuitOverviewFromDashboard(&pursuit.Dashboard{
		Counts: map[string]int64{"active": 1, "waiting": 0, "blocked": 0},
		NeedsRobert: []pursuit.PursuitListItem{
			{
				Pursuit:     models.Pursuit{ID: uuid.New(), Title: "Unproven claim", Status: pursuit.StatusActive, RiskLevel: "high"},
				NeedsRobert: 1,
				NextAction:  "Collect source evidence before action",
			},
		},
	})

	if overview.EvidenceStatus != "ungrounded" {
		t.Fatalf("evidence status = %q, want ungrounded", overview.EvidenceStatus)
	}
	if overview.LinkedEvidence != 0 || overview.DecisionCards != 0 {
		t.Fatalf("ungrounded pursuit should not report evidence/decision totals: %+v", overview)
	}
	if !strings.Contains(overview.Queues[len(overview.Queues)-1].Name, "Evidence") {
		t.Fatalf("evidence queue missing from operating queues: %+v", overview.Queues)
	}
}

func TestAmbientPursuitLineExplainsProposalState(t *testing.T) {
	line := ambientPursuitLine(PursuitOverview{
		AmbientProposals:     3,
		AmbientApprovalQueue: 2,
		AmbientLastScan:      "completed",
	})

	for _, expected := range []string{"3 ambient pursuit proposals", "2 require approval", "last scan completed"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("ambient line %q missing %q", line, expected)
		}
	}
	if status := ambientPursuitStatus(PursuitOverview{AmbientProposals: 3, AmbientApprovalQueue: 2}); status != "needs_approval" {
		t.Fatalf("ambient status = %q, want needs_approval", status)
	}
	if status := ambientPursuitStatus(PursuitOverview{AmbientProposals: 1}); status != "proposed" {
		t.Fatalf("ambient status = %q, want proposed", status)
	}

	empty := ambientPursuitLine(PursuitOverview{})
	if !strings.Contains(empty, "run an ambient scan") {
		t.Fatalf("empty ambient line should guide scan action: %q", empty)
	}
	if status := ambientPursuitStatus(PursuitOverview{}); status != "not_scanned" {
		t.Fatalf("empty ambient status = %q, want not_scanned", status)
	}
}

func TestPursuitOverviewSurfacesPlanningAndReviewQueues(t *testing.T) {
	planningID := uuid.New()
	reviewID := uuid.New()
	overview := pursuitOverviewFromDashboard(&pursuit.Dashboard{
		Counts: map[string]int64{"active": 2, "waiting": 0, "blocked": 0},
		PlanningNeeded: []pursuit.PursuitListItem{
			{
				Pursuit:        models.Pursuit{ID: planningID, Title: "Make HAI operational", Status: pursuit.StatusActive, RiskLevel: "medium"},
				PlanningNeeded: true,
				NextAction:     "Create first executable workflow",
			},
		},
		ReviewDue: []pursuit.PursuitListItem{
			{
				Pursuit:    models.Pursuit{ID: reviewID, Title: "Vivare dispute", Status: pursuit.StatusWaiting, RiskLevel: "high"},
				ReviewDue:  true,
				NextAction: "Review waiting state",
			},
		},
	})

	if overview.Status != "needs_attention" {
		t.Fatalf("status = %q, want needs_attention", overview.Status)
	}
	if overview.PlanningNeeded != 1 || overview.ReviewDue != 1 {
		t.Fatalf("overview planning/review counts = %d/%d, want 1/1", overview.PlanningNeeded, overview.ReviewDue)
	}
	if !strings.Contains(overview.Summary, "need first plan") || !strings.Contains(overview.Summary, "review due") {
		t.Fatalf("summary = %q, want planning and review due signals", overview.Summary)
	}
	if !strings.Contains(overview.Next, "first executable workflow") {
		t.Fatalf("next = %q, want planning-first guidance", overview.Next)
	}
	if len(overview.Spotlight) != 2 || overview.Spotlight[0].ID != planningID.String() {
		t.Fatalf("spotlight = %+v, want planning-needed item first", overview.Spotlight)
	}
	if !overview.Spotlight[0].PlanningNeeded || !overview.Spotlight[1].ReviewDue {
		t.Fatalf("spotlight metadata missing planning/review flags: %+v", overview.Spotlight)
	}
}

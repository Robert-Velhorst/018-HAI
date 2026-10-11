package haios

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/safety"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Metric struct {
	Label  string `json:"label"`
	Value  int64  `json:"value"`
	Status string `json:"status"`
}

type PlaneStatus struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Links       []string `json:"links"`
}

type ReferenceStackStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Use    string `json:"use"`
}

type ReadinessGate struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Next     string `json:"next"`
}

type PursuitQueueStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int    `json:"count"`
	Status      string `json:"status"`
	Route       string `json:"route"`
}

type PursuitSpotlight struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	RiskLevel      string `json:"riskLevel"`
	NextAction     string `json:"nextAction,omitempty"`
	CurrentState   string `json:"currentState,omitempty"`
	EvidenceLine   string `json:"evidenceLine,omitempty"`
	NeedsRobert    int    `json:"needsRobert"`
	Blocked        int    `json:"blocked"`
	OpenLoops      int    `json:"openLoops"`
	DecisionCards  int    `json:"decisionCards"`
	LinkedEvidence int    `json:"linkedEvidence"`
	TimelineItems  int    `json:"timelineItems"`
	Stale          bool   `json:"stale"`
	ReviewDue      bool   `json:"reviewDue"`
	PlanningNeeded bool   `json:"planningNeeded"`
}

type PursuitOverview struct {
	Enabled              bool                 `json:"enabled"`
	Status               string               `json:"status"`
	TotalActive          int                  `json:"totalActive"`
	NeedsRobert          int                  `json:"needsRobert"`
	VAReady              int                  `json:"vaReady"`
	SystemReady          int                  `json:"systemReady"`
	Blocked              int                  `json:"blocked"`
	Stale                int                  `json:"stale"`
	ReviewDue            int                  `json:"reviewDue"`
	PlanningNeeded       int                  `json:"planningNeeded"`
	HighRisk             int                  `json:"highRisk"`
	CompletionCandidates int                  `json:"completionCandidates"`
	DecisionCards        int                  `json:"decisionCards"`
	LinkedEvidence       int                  `json:"linkedEvidence"`
	OpenLoops            int                  `json:"openLoops"`
	TimelineItems        int                  `json:"timelineItems"`
	EvidenceStatus       string               `json:"evidenceStatus"`
	AmbientProposals     int                  `json:"ambientProposals"`
	AmbientApprovalQueue int                  `json:"ambientApprovalQueue"`
	AmbientLastScan      string               `json:"ambientLastScan,omitempty"`
	AmbientLine          string               `json:"ambientLine,omitempty"`
	Summary              string               `json:"summary"`
	Next                 string               `json:"next"`
	Queues               []PursuitQueueStatus `json:"queues"`
	Spotlight            []PursuitSpotlight   `json:"spotlight"`
}

type HAIOSOverview struct {
	GeneratedAt         time.Time              `json:"generatedAt"`
	CanonicalStack      string                 `json:"canonicalStack"`
	ReferenceStacks     []ReferenceStackStatus `json:"referenceStacks"`
	LocalFirst          bool                   `json:"localFirst"`
	CompletionFirst     bool                   `json:"completionFirst"`
	PaidBudgetEUR       float64                `json:"paidBudgetEur"`
	PaidUsageAllowed    bool                   `json:"paidUsageAllowed"`
	Metrics             []Metric               `json:"metrics"`
	Planes              []PlaneStatus          `json:"planes"`
	ReadinessGates      []ReadinessGate        `json:"readinessGates"`
	PursuitOverview     PursuitOverview        `json:"pursuitOverview"`
	NeedsReviewTotal    int64                  `json:"needsReviewTotal"`
	EmergencyStop       bool                   `json:"emergencyStop"`
	EmergencyStopReason string                 `json:"emergencyStopReason"`
	EmergencyStopNote   string                 `json:"emergencyStopNote"`
}

type pursuitDashboardService interface {
	DashboardForOwner(ownerIdentity string) (*pursuit.Dashboard, error)
}

type Handler struct {
	db             *gorm.DB
	pursuitService pursuitDashboardService
}

func DefaultHandler() (*Handler, error) {
	return DefaultHandlerWithPursuits(nil)
}

func DefaultHandlerWithPursuits(pursuitService pursuitDashboardService) (*Handler, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewHandler(db, pursuitService), nil
}

func NewHandler(db *gorm.DB, pursuitService pursuitDashboardService) *Handler {
	return &Handler{db: db, pursuitService: pursuitService}
}

// RequireAuthenticatedOwner protects the personal operating view. The HAI OS
// page aggregates pursuit and ambient planning state, so HTTP clients must
// provide a verified IDP principal before it is rendered.
func RequireAuthenticatedOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if pursuitOwner(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for HAI OS access"})
			return
		}
		c.Next()
	}
}

func (h *Handler) Overview(c *gin.Context) {
	ownerIdentity := pursuitOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for HAI OS access"})
		return
	}
	policy, err := llm.NewServiceFromEnv()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	llmPolicy := policy.Policy()
	now := time.Now().UTC()
	snapshot, err := h.loadMetricSnapshot(c.Request.Context(), ownerIdentity, now)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "HAI OS metrics are unavailable"})
		return
	}
	reviewTotal := snapshot.ReviewTotal()
	emergencyActive := safety.EmergencyStopActive()
	emergencyReason := "emergency stop is clear"
	if emergencyActive {
		emergencyReason = safety.EmergencyStopReason()
	}
	pursuitOverview := h.pursuitOverview(ownerIdentity)
	attachAmbientPursuitState(snapshot, &pursuitOverview)
	pursuitStatus := pursuitOverview.Status
	if pursuitStatus == "" {
		pursuitStatus = "unavailable"
	}

	c.JSON(http.StatusOK, HAIOSOverview{
		GeneratedAt:    now,
		CanonicalStack: "Codex Go backend + Angular dashboard + Postgres is the canonical product stack",
		ReferenceStacks: []ReferenceStackStatus{
			{Name: "Manus React/tRPC/MySQL", Status: "reference_only", Use: "Use as a product/UX reference and port only deliberately selected behavior into the canonical Go/Angular stack."},
		},
		LocalFirst:       true,
		CompletionFirst:  true,
		PaidBudgetEUR:    llmPolicy.DailyPaidBudgetEUR,
		PaidUsageAllowed: llmPolicy.PaidCallsAllowed,
		Metrics: []Metric{
			{Label: "pursuits", Value: int64(pursuitOverview.TotalActive), Status: statusForCount(int64(pursuitOverview.TotalActive), "active")},
			{Label: "pursuits needing Robert", Value: int64(pursuitOverview.NeedsRobert), Status: statusForZero(int64(pursuitOverview.NeedsRobert))},
			{Label: "pursuits needing plan", Value: int64(pursuitOverview.PlanningNeeded), Status: statusForZero(int64(pursuitOverview.PlanningNeeded))},
			{Label: "pursuits review due", Value: int64(pursuitOverview.ReviewDue), Status: statusForZero(int64(pursuitOverview.ReviewDue))},
			{Label: "ambient pursuit proposals", Value: int64(pursuitOverview.AmbientProposals), Status: ambientPursuitStatus(pursuitOverview)},
			{Label: "VA-ready pursuits", Value: int64(pursuitOverview.VAReady), Status: statusForCount(int64(pursuitOverview.VAReady), "ready")},
			{Label: "system-ready pursuits", Value: int64(pursuitOverview.SystemReady), Status: statusForCount(int64(pursuitOverview.SystemReady), "ready")},
			{Label: "automations", Value: snapshot.Automations, Status: statusForCount(snapshot.Automations, "ready")},
			{Label: "unhealthy automations", Value: snapshot.UnhealthyAutomations, Status: statusForZero(snapshot.UnhealthyAutomations)},
			{Label: "connected sources", Value: snapshot.ConnectedSources, Status: statusForCount(snapshot.ConnectedSources, "ready")},
			{Label: "source extractions", Value: snapshot.SourceExtractions, Status: statusForCount(snapshot.SourceExtractions, "indexed")},
			{Label: "workflow items", Value: snapshot.WorkflowItems, Status: statusForCount(snapshot.WorkflowItems, "active")},
			{Label: "workflow approvals", Value: snapshot.WorkflowApprovals, Status: statusForZero(snapshot.WorkflowApprovals)},
			{Label: "due open loops", Value: snapshot.DueOpenLoops, Status: statusForZero(snapshot.DueOpenLoops)},
			{Label: "quality gates needing review", Value: snapshot.WorkflowQualityReview, Status: statusForZero(snapshot.WorkflowQualityReview)},
			{Label: "context memories", Value: snapshot.ContextMemories, Status: statusForCount(snapshot.ContextMemories, "available")},
			{Label: "llm providers", Value: int64(len(llmPolicy.Providers)), Status: statusForBool(liveProviderConfigured(llmPolicy), "executable", "no_executable")},
			{Label: "verification runs", Value: snapshot.VerificationRuns, Status: statusForCount(snapshot.VerificationRuns, "active")},
			{Label: "needs review", Value: reviewTotal, Status: statusForZero(reviewTotal)},
		},
		Planes: []PlaneStatus{
			{Name: "Control", Status: "operational", Description: "Dashboard, automation registry, health, workflow inbox, policy, and queues.", Links: []string{"/home", "/control-center", "/workflow-engine"}},
			{Name: "Pursuits", Status: pursuitStatus, Description: "Top-level life/work objectives now aggregate workflows, evidence, memory, approvals, blockers, and next actions.", Links: []string{"/pursuits", "/command-dashboard", "/workflow-engine"}},
			{Name: "Knowledge", Status: "partial", Description: "Local-folder ingestion plus read-only Trello, GitHub, and Gmail adapters are implemented. Account configuration and bounded live validation remain per connector; Calendar and Drive remain local-export paths.", Links: []string{"/connected-sources"}},
			{Name: "Memory", Status: "operational", Description: "Editable local memory with retrieval, dedupe, merge, archive, export, and correction.", Links: []string{"/memory"}},
			{Name: "Reasoning", Status: "operational", Description: "Task classifier, criteria generator, context planner, model router, workflow state machine, priority, retry, and review.", Links: []string{"/task-blueprint", "/llm-policy", "/workflow-engine"}},
			{Name: "Execution", Status: "guarded", Description: "Automation launch, workflow worker execution, retry limits, and controlled task runs exist; autonomous execution remains approval-gated.", Links: []string{"/control-center", "/task-blueprint", "/workflow-engine"}},
			{Name: "Verification", Status: "partial", Description: "Claim/status checks exist, but real-world correctness still depends on connected sources and live provider validation.", Links: []string{"/grounded-answers"}},
			{Name: "Governance", Status: "guarded", Description: "Local-only source controls, paid budget policy, sensitive flags, workflow approvals, and audit logs.", Links: []string{"/llm-policy", "/connected-sources", "/grounded-answers", "/workflow-engine"}},
			{Name: "Observability", Status: "operational", Description: "Health summaries, sync logs, workflow events, verification runs, routing logs, diagnostics, and review indicators.", Links: []string{"/control-center", "/connected-sources", "/grounded-answers", "/workflow-engine"}},
		},
		ReadinessGates:      append(readinessGates(llmPolicy), pursuitReadinessGate(pursuitOverview)),
		PursuitOverview:     pursuitOverview,
		NeedsReviewTotal:    reviewTotal,
		EmergencyStop:       emergencyActive,
		EmergencyStopReason: emergencyReason,
		EmergencyStopNote:   "Set HAI_EMERGENCY_STOP=true to block model generation, automation launches, task execution, workflow workers, and follow-up workers while keeping planning and review visible.",
	})
}

func (h *Handler) pursuitOverview(ownerIdentity string) PursuitOverview {
	if h.pursuitService == nil {
		return PursuitOverview{
			Enabled: false,
			Status:  "unavailable",
			Summary: "Pursuit service is not wired into the HAI OS overview handler.",
			Next:    "Wire the canonical pursuit service into the HAI OS handler.",
		}
	}
	dashboard, err := h.pursuitService.DashboardForOwner(ownerIdentity)
	if err != nil {
		return PursuitOverview{
			Enabled: false,
			Status:  "degraded",
			Summary: "Pursuit dashboard could not be loaded: " + err.Error(),
			Next:    "Check pursuit repository/database migrations before trusting OS-level pursuit status.",
		}
	}
	return pursuitOverviewFromDashboard(dashboard)
}

func pursuitOwner(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func attachAmbientPursuitState(snapshot ownerMetricSnapshot, overview *PursuitOverview) {
	if overview == nil {
		return
	}
	overview.AmbientProposals = int(snapshot.AmbientProposals)
	overview.AmbientApprovalQueue = int(snapshot.AmbientApprovalQueue)
	overview.AmbientLastScan = snapshot.AmbientLastScan
	overview.AmbientLine = ambientPursuitLine(*overview)
	overview.Queues = append(overview.Queues, PursuitQueueStatus{
		Name:        "Ambient proposals",
		Description: "Proactive pursuit opportunities created from stale, blocked, review-due, Robert-only, VA-ready, or system-ready signals.",
		Count:       overview.AmbientProposals,
		Status:      ambientPursuitStatus(*overview),
		Route:       "/ambient-brain",
	})
}

func ambientPursuitLine(overview PursuitOverview) string {
	if overview.AmbientProposals == 0 {
		if overview.AmbientLastScan == "" {
			return "No ambient pursuit proposals are stored yet; run an ambient scan to convert stale, blocked, review-due, or Robert-only pursuit signals into proposals."
		}
		return "No active ambient pursuit proposals after last scan: " + overview.AmbientLastScan + "."
	}
	parts := []string{fmtInt(overview.AmbientProposals) + " ambient pursuit proposal" + pluralSuffix(overview.AmbientProposals)}
	if overview.AmbientApprovalQueue > 0 {
		parts = append(parts, fmtInt(overview.AmbientApprovalQueue)+" require approval")
	}
	if overview.AmbientLastScan != "" {
		parts = append(parts, "last scan "+overview.AmbientLastScan)
	}
	return strings.Join(parts, " / ") + "."
}

func ambientPursuitStatus(overview PursuitOverview) string {
	if overview.AmbientApprovalQueue > 0 {
		return "needs_approval"
	}
	if overview.AmbientProposals > 0 {
		return "proposed"
	}
	if overview.AmbientLastScan == "" {
		return "not_scanned"
	}
	return "clear"
}

func pursuitOverviewFromDashboard(dashboard *pursuit.Dashboard) PursuitOverview {
	if dashboard == nil {
		return PursuitOverview{Enabled: false, Status: "unavailable", Summary: "Pursuit dashboard returned no data.", Next: "Review pursuit service wiring."}
	}
	total := int(dashboard.Counts["active"] + dashboard.Counts["waiting"] + dashboard.Counts["blocked"])
	items := uniquePursuitOperatingItems(
		dashboard.NeedsRobert,
		dashboard.PlanningNeeded,
		dashboard.ReviewDue,
		dashboard.Blocked,
		dashboard.Stale,
		dashboard.VAReady,
		dashboard.SystemReady,
		dashboard.CompletionCandidates,
		dashboard.RecentlyChanged,
		dashboard.HighRisk,
	)
	overview := PursuitOverview{
		Enabled:              true,
		TotalActive:          total,
		NeedsRobert:          len(dashboard.NeedsRobert),
		VAReady:              len(dashboard.VAReady),
		SystemReady:          len(dashboard.SystemReady),
		Blocked:              len(dashboard.Blocked),
		Stale:                len(dashboard.Stale),
		ReviewDue:            len(dashboard.ReviewDue),
		PlanningNeeded:       len(dashboard.PlanningNeeded),
		HighRisk:             len(dashboard.HighRisk),
		CompletionCandidates: len(dashboard.CompletionCandidates),
	}
	overview.DecisionCards, overview.LinkedEvidence, overview.OpenLoops, overview.TimelineItems = pursuitEvidenceTotals(items)
	overview.EvidenceStatus = pursuitEvidenceStatus(overview)
	overview.Status = pursuitOperatingStatus(overview)
	overview.Summary = pursuitOperatingSummary(overview)
	overview.Next = pursuitOperatingNext(overview)
	overview.Queues = []PursuitQueueStatus{
		{Name: "Robert-only decisions", Description: "Items HAI should not decide alone.", Count: overview.NeedsRobert, Status: statusForAttention(overview.NeedsRobert, "needs_decision"), Route: "/pursuits"},
		{Name: "Planning needed", Description: "Pursuits with no linked workflow yet; HAI should compile the first executable plan.", Count: overview.PlanningNeeded, Status: statusForAttention(overview.PlanningNeeded, "needs_plan"), Route: "/pursuits"},
		{Name: "Review due", Description: "Scheduled pursuit reviews that should be acknowledged, snoozed, or converted into next actions.", Count: overview.ReviewDue, Status: statusForAttention(overview.ReviewDue, "due"), Route: "/pursuits"},
		{Name: "VA-ready work", Description: "Structured work that can be delegated without Robert re-explaining context.", Count: overview.VAReady, Status: statusForCount(int64(overview.VAReady), "ready"), Route: "/pursuits"},
		{Name: "System-ready work", Description: "Low-risk work the governed engine can prepare or execute when allowed.", Count: overview.SystemReady, Status: statusForCount(int64(overview.SystemReady), "ready"), Route: "/pursuits"},
		{Name: "Blocked or stale", Description: "Pursuits needing unblock, follow-up, or review.", Count: overview.Blocked + overview.Stale, Status: statusForAttention(overview.Blocked+overview.Stale, "needs_attention"), Route: "/pursuits"},
		{Name: "High-risk", Description: "Legal, financial, public, account, or other consequential pursuits.", Count: overview.HighRisk, Status: statusForAttention(overview.HighRisk, "guarded"), Route: "/pursuits"},
		{Name: "Completion candidates", Description: "May be closeable only after evidence or approval confirms completion.", Count: overview.CompletionCandidates, Status: statusForCount(int64(overview.CompletionCandidates), "verify"), Route: "/pursuits"},
		{Name: "Evidence-backed", Description: "Top operating pursuits with linked evidence, memory, source, runtime, or verification context.", Count: overview.LinkedEvidence, Status: overview.EvidenceStatus, Route: "/pursuits"},
	}
	overview.Spotlight = pursuitSpotlight(
		dashboard.NeedsRobert,
		dashboard.PlanningNeeded,
		dashboard.ReviewDue,
		dashboard.Blocked,
		dashboard.Stale,
		dashboard.VAReady,
		dashboard.SystemReady,
		dashboard.CompletionCandidates,
	)
	return overview
}

func pursuitOperatingStatus(overview PursuitOverview) string {
	if !overview.Enabled {
		return "unavailable"
	}
	if overview.NeedsRobert > 0 {
		return "needs_robert"
	}
	if overview.PlanningNeeded > 0 || overview.ReviewDue > 0 || overview.Blocked > 0 || overview.Stale > 0 {
		return "needs_attention"
	}
	if overview.TotalActive == 0 {
		return "empty"
	}
	if overview.VAReady > 0 || overview.SystemReady > 0 {
		return "ready"
	}
	return "operational"
}

func pursuitOperatingSummary(overview PursuitOverview) string {
	if overview.TotalActive == 0 {
		return "No active pursuits are registered yet. HAI can still create pursuits from source intake, memory imports, or manual goals."
	}
	parts := []string{fmtInt(overview.TotalActive) + " active pursuits"}
	if overview.NeedsRobert > 0 {
		parts = append(parts, fmtInt(overview.NeedsRobert)+" need Robert")
	}
	if overview.VAReady > 0 {
		parts = append(parts, fmtInt(overview.VAReady)+" VA-ready")
	}
	if overview.SystemReady > 0 {
		parts = append(parts, fmtInt(overview.SystemReady)+" system-ready")
	}
	if overview.PlanningNeeded > 0 {
		parts = append(parts, fmtInt(overview.PlanningNeeded)+" need first plan")
	}
	if overview.ReviewDue > 0 {
		parts = append(parts, fmtInt(overview.ReviewDue)+" review due")
	}
	if overview.Blocked+overview.Stale > 0 {
		parts = append(parts, fmtInt(overview.Blocked+overview.Stale)+" blocked/stale")
	}
	return strings.Join(parts, ", ") + "."
}

func pursuitOperatingNext(overview PursuitOverview) string {
	if overview.NeedsRobert > 0 {
		return "Open the Robert-only pursuit queue and approve, reject, or correct the proposed next decisions."
	}
	if overview.PlanningNeeded > 0 {
		return "Create the first executable workflow plan for pursuits that have goals but no operational path yet."
	}
	if overview.ReviewDue > 0 {
		return "Review due pursuits, then mark them reviewed, snooze them, or turn the review into a next action."
	}
	if overview.Blocked > 0 || overview.Stale > 0 {
		return "Review blocked and stale pursuits; create follow-up proposals before marking anything complete."
	}
	if overview.VAReady > 0 {
		return "Delegate VA-ready pursuit work with context, boundaries, and source links attached."
	}
	if overview.SystemReady > 0 {
		return "Run low-risk system-ready pursuit steps through the governed workflow/automation layer."
	}
	if overview.CompletionCandidates > 0 {
		return "Verify evidence before closing completion candidates."
	}
	return "Create or import pursuits from real source intake so HAI has long-running objectives to advance."
}

func uniquePursuitOperatingItems(groups ...[]pursuit.PursuitListItem) []pursuit.PursuitListItem {
	result := []pursuit.PursuitListItem{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, item := range group {
			id := item.Pursuit.ID.String()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, item)
		}
	}
	return result
}

func pursuitEvidenceTotals(items []pursuit.PursuitListItem) (decisionCards int, linkedEvidence int, openLoops int, timelineItems int) {
	for _, item := range items {
		decisionCards += item.DecisionCards
		linkedEvidence += item.LinkedEvidence
		openLoops += item.OpenLoops
		timelineItems += item.TimelineItems
	}
	return decisionCards, linkedEvidence, openLoops, timelineItems
}

func pursuitEvidenceStatus(overview PursuitOverview) string {
	if overview.TotalActive == 0 {
		return "empty"
	}
	if overview.LinkedEvidence == 0 && overview.DecisionCards+overview.OpenLoops+overview.TimelineItems == 0 {
		return "ungrounded"
	}
	if overview.LinkedEvidence == 0 {
		return "needs_evidence"
	}
	if overview.CompletionCandidates > 0 {
		return "verify_before_close"
	}
	return "source_linked"
}

func pursuitEvidenceLine(item pursuit.PursuitListItem) string {
	parts := []string{
		fmtInt(item.DecisionCards) + " decision" + pluralSuffix(item.DecisionCards),
		fmtInt(item.LinkedEvidence) + " evidence",
		fmtInt(item.TimelineItems) + " timeline",
	}
	if item.OpenLoops > 0 {
		parts = append(parts, fmtInt(item.OpenLoops)+" open loop"+pluralSuffix(item.OpenLoops))
	}
	return strings.Join(parts, " / ")
}

func pursuitSpotlight(groups ...[]pursuit.PursuitListItem) []PursuitSpotlight {
	result := []PursuitSpotlight{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, item := range group {
			id := item.Pursuit.ID.String()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, PursuitSpotlight{
				ID:             id,
				Title:          item.Pursuit.Title,
				Status:         item.Pursuit.Status,
				RiskLevel:      item.Pursuit.RiskLevel,
				NextAction:     item.NextAction,
				CurrentState:   firstNonEmpty(item.CurrentState, item.Pursuit.CurrentStateSummary),
				EvidenceLine:   pursuitEvidenceLine(item),
				NeedsRobert:    item.NeedsRobert,
				Blocked:        item.Blocked,
				OpenLoops:      item.OpenLoops,
				DecisionCards:  item.DecisionCards,
				LinkedEvidence: item.LinkedEvidence,
				TimelineItems:  item.TimelineItems,
				Stale:          item.Stale,
				ReviewDue:      item.ReviewDue,
				PlanningNeeded: item.PlanningNeeded,
			})
			if len(result) >= 5 {
				return result
			}
		}
	}
	return result
}

func pursuitReadinessGate(overview PursuitOverview) ReadinessGate {
	if !overview.Enabled {
		return ReadinessGate{
			Name:     "Pursuit operating layer",
			Status:   overview.Status,
			Evidence: overview.Summary,
			Next:     overview.Next,
		}
	}
	return ReadinessGate{
		Name:     "Pursuit operating layer",
		Status:   overview.Status,
		Evidence: overview.Summary,
		Next:     overview.Next,
	}
}

func statusForCount(value int64, ready string) string {
	if value == 0 {
		return "empty"
	}
	return ready
}

func statusForZero(value int64) string {
	if value == 0 {
		return "clear"
	}
	return "needs_review"
}

func statusForAttention(value int, status string) string {
	if value == 0 {
		return "clear"
	}
	return status
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}

func pluralSuffix(value int) string {
	if value == 1 {
		return ""
	}
	return "s"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func readinessGates(policy llm.Policy) []ReadinessGate {
	return []ReadinessGate{
		{
			Name:     "Canonical stack selected",
			Status:   "decided",
			Evidence: "Go/Angular/Postgres is canonical; Manus React/tRPC/MySQL is reference-only.",
			Next:     "Port useful Manus behavior through explicit issues instead of running two competing products.",
		},
		{
			Name:     "Live LLM provider configured",
			Status:   statusForBool(liveProviderConfigured(policy), "configured", "not_configured"),
			Evidence: liveProviderEvidence(policy),
			Next:     "Connect a loopback Ollama, LM Studio, llama.cpp, LocalAI, or vLLM endpoint, or a configured free OpenAI-compatible endpoint, then run provider smoke tests.",
		},
		{
			Name:     "Real account connectors",
			Status:   "partial",
			Evidence: "Read-only Trello REST, GitHub REST, and Gmail OAuth adapters are implemented alongside manual import and local-folder sync. Calendar and Drive remain export/local-folder paths.",
			Next:     "Configure only the needed connector, then retain bounded live sync, provenance, revoke, and owner-isolation evidence before relying on it.",
		},
		{
			Name:     "Controlled runtime execution safety",
			Status:   runtimeSafetyStatus(),
			Evidence: runtimeSafetyEvidence(),
			Next:     "Keep script/Docker execution disabled until targets are allowlisted and reviewed; keep link-local API targets blocked.",
		},
		{
			Name:     "Autonomous workflow scheduler",
			Status:   workflowSchedulerStatus(),
			Evidence: workflowSchedulerEvidence(),
			Next:     "Keep run limits low, review blocked/approval queues, and use emergency stop during incidents.",
		},
		{
			Name:     "Emergency stop",
			Status:   statusForBool(safety.EmergencyStopActive(), "active", "clear"),
			Evidence: emergencyStopEvidence(),
			Next:     "Use HAI_EMERGENCY_STOP=true during incidents; clear it only after reviewing blocked work.",
		},
		{
			Name:     "External correctness proof",
			Status:   "unproven",
			Evidence: "Unit tests cover internal consistency; live provider/source fixtures and end-to-end account workflows are still required.",
			Next:     "Add integration tests against a local Ollama server, seeded local source folders, and one real account connector sandbox.",
		},
	}
}

func liveProviderConfigured(policy llm.Policy) bool {
	for _, provider := range policy.Providers {
		if providerHasExecutableNoApprovalModel(provider, policy) {
			return true
		}
	}
	return false
}

func liveProviderEvidence(policy llm.Policy) string {
	configured := []string{}
	notExecutable := []string{}
	for _, provider := range policy.Providers {
		if providerHasExecutableNoApprovalModel(provider, policy) {
			configured = append(configured, provider.Name)
			continue
		}
		if provider.Enabled && provider.Configured && providerHasEnabledModel(provider) {
			notExecutable = append(notExecutable, provider.Name)
		}
	}
	if len(configured) == 0 {
		message := "No enabled provider has a configured endpoint with a no-approval executable model."
		if len(notExecutable) > 0 {
			message += " Configured but not executable under current approval, local-model, budget, or quota policy: " + strings.Join(notExecutable, ", ") + "."
		}
		return message
	}
	message := "Configured executable provider endpoints: " + strings.Join(configured, ", ") + "."
	if len(notExecutable) > 0 {
		message += " Configured but non-executable providers are tracked separately: " + strings.Join(notExecutable, ", ") + "."
	}
	return message
}

func providerHasExecutableNoApprovalModel(provider llm.Provider, policy llm.Policy) bool {
	if !provider.Enabled || !provider.Configured || provider.Paid {
		return false
	}
	if provider.Local && !policy.LocalModelsAllowed {
		return false
	}
	if !provider.Local && provider.QuotaRemaining == 0 {
		return false
	}
	if !provider.Local && !policy.FreeCloudQuotaAllowed {
		return false
	}
	for _, model := range provider.Models {
		if model.Enabled && !model.RequiresApproval && model.EstimatedCostEUR <= 0 && model.Tier != llm.TierExpensive {
			return true
		}
	}
	return false
}

func providerHasEnabledModel(provider llm.Provider) bool {
	for _, model := range provider.Models {
		if model.Enabled {
			return true
		}
	}
	return false
}

func runtimeSafetyStatus() string {
	if strings.EqualFold(os.Getenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED"), "true") || strings.EqualFold(os.Getenv("AUTOMATION_DOCKER_CONTROL_ENABLED"), "true") {
		return "guarded_enabled"
	}
	return "guarded_disabled"
}

func runtimeSafetyEvidence() string {
	parts := []string{
		"API launches require AUTOMATION_API_ALLOWED_HOSTS and reject link-local/metadata targets by default.",
		"Script execution enabled: " + boolWord(strings.EqualFold(os.Getenv("AUTOMATION_SCRIPT_EXECUTION_ENABLED"), "true")),
		"Docker control enabled: " + boolWord(strings.EqualFold(os.Getenv("AUTOMATION_DOCKER_CONTROL_ENABLED"), "true")),
	}
	return strings.Join(parts, " ")
}

func workflowSchedulerStatus() string {
	if strings.EqualFold(os.Getenv("WORKFLOW_SCHEDULER_ENABLED"), "false") {
		return "paused"
	}
	return "guarded_enabled"
}

func workflowSchedulerEvidence() string {
	enabled := !strings.EqualFold(os.Getenv("WORKFLOW_SCHEDULER_ENABLED"), "false")
	openLoopsEnabled := !strings.EqualFold(os.Getenv("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED"), "false")
	limit := strings.TrimSpace(os.Getenv("WORKFLOW_SCHEDULER_RUN_LIMIT"))
	if limit == "" {
		limit = "2"
	}
	interval := strings.TrimSpace(os.Getenv("WORKFLOW_SCHEDULER_INTERVAL_SECONDS"))
	if interval == "" {
		interval = "600"
	}
	startup := strings.EqualFold(os.Getenv("WORKFLOW_SCHEDULER_RUN_ON_STARTUP"), "true")
	lease := strings.TrimSpace(os.Getenv("WORKFLOW_CLAIM_LEASE_SECONDS"))
	if lease == "" {
		lease = "900"
	}
	parts := []string{
		"Workflow scheduler enabled: " + boolWord(enabled),
		"Open-loop pass enabled: " + boolWord(openLoopsEnabled),
		"Interval seconds: " + interval,
		"Run limit per tick: " + limit,
		"Run on startup: " + boolWord(startup),
		"Claim lease seconds: " + lease,
	}
	return strings.Join(parts, " ")
}

func emergencyStopEvidence() string {
	if safety.EmergencyStopActive() {
		return safety.EmergencyStopReason()
	}
	return "Emergency stop is clear; autonomous execution still remains approval-gated by risk policy."
}

func statusForBool(value bool, positive, negative string) string {
	if value {
		return positive
	}
	return negative
}

func boolWord(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

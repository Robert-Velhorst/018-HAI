package pursuit

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

func TestDashboardSerializesEmptyQueuesAsArrays(t *testing.T) {
	service := NewService(newFakeRepo(), nil)
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	payload, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatalf("marshal dashboard: %v", err)
	}
	for _, field := range []string{
		"decisionQueue", "needsRobert", "vaReady", "systemReady", "blocked", "stale",
		"reviewDue", "planningNeeded", "recentlyChanged", "highRisk", "completionCandidates",
	} {
		if strings.Contains(string(payload), `"`+field+`":null`) {
			t.Fatalf("dashboard field %q serialized as null: %s", field, payload)
		}
	}
}

func TestDashboardSummaryKeepsActionContextAndDropsDetailOnlyFields(t *testing.T) {
	pursuitID := uuid.New()
	full := models.Pursuit{
		ID:                    pursuitID,
		OwnerIdentity:         "alice",
		Title:                 "Prepare the verified response",
		Description:           "Long source-backed description",
		WhyItMatters:          "A deadline is approaching.",
		CurrentStateSummary:   "Evidence is ready for review.",
		NextRecommendedAction: "Review the response.",
		CompletionDefinition:  "The approved response is delivered.",
		SuccessCriteria:       []models.PursuitSuccessCriterion{{ID: "criterion-1", Description: "Response approved"}},
		StopConditions:        []models.PursuitStopCondition{{ID: "stop-1", Description: "Conflicting evidence"}},
		Dependencies:          []models.PursuitDependency{{ID: "dependency-1", Label: "Evidence bundle"}},
		ResourceLimits:        models.PursuitResourceLimits{MaxEffortHours: 2, Notes: "detail only"},
	}
	dashboard := &Dashboard{
		Counts: map[string]int64{"active": 1},
		DecisionQueue: []PursuitDashboardDecision{{
			Pursuit: full,
			Decision: PursuitDecision{
				ID:               "decision-1",
				WorkflowID:       "workflow-1",
				WorkflowTitle:    "Private workflow title",
				DecisionType:     "approval",
				Status:           "pending",
				Recommended:      "Review the response.",
				Reason:           "The response affects an external party.",
				RiskLevel:        "high",
				EvidenceURI:      "private://evidence/1",
				EvidenceLabel:    "Private evidence label",
				YesLabel:         "Approve",
				NoLabel:          "Reject",
				YesConsequence:   "The workflow may continue.",
				NoConsequence:    "The workflow remains paused.",
				RequiresApproval: true,
				Actor:            "alice",
				CreatedAt:        time.Now().UTC().Format(time.RFC3339),
			},
		}},
		NeedsRobert: []PursuitListItem{
			{Pursuit: full, NeedsRobert: 1},
			{Pursuit: models.Pursuit{ID: uuid.New(), Title: "Second decision"}, NeedsRobert: 1},
		},
	}

	summary := dashboardSummary(dashboard)
	if summary == dashboard || summary.NeedsRobert[0].Pursuit.ID != pursuitID {
		t.Fatalf("summary identity was not preserved: %#v", summary)
	}
	for _, candidate := range []models.Pursuit{
		summary.NeedsRobert[0].Pursuit,
		summary.DecisionQueue[0].Pursuit,
	} {
		if candidate.Title != full.Title || candidate.WhyItMatters != full.WhyItMatters || candidate.CurrentStateSummary != full.CurrentStateSummary || candidate.NextRecommendedAction != full.NextRecommendedAction {
			t.Fatalf("summary removed action context: %#v", candidate)
		}
		if candidate.OwnerIdentity != "" || candidate.Description != "" || candidate.CompletionDefinition != "" || candidate.SuccessCriteria != nil || candidate.StopConditions != nil || candidate.Dependencies != nil || candidate.ResourceLimits.MaxEffortHours != 0 {
			t.Fatalf("summary retained detail-only pursuit data: %#v", candidate)
		}
	}
	if dashboard.NeedsRobert[0].Pursuit.Description == "" || len(dashboard.NeedsRobert[0].Pursuit.SuccessCriteria) != 1 {
		t.Fatalf("summary mutated the full dashboard: %#v", dashboard.NeedsRobert[0].Pursuit)
	}
	if len(summary.NeedsRobert) != 1 {
		t.Fatalf("summary needs-Robert queue length = %d, want one actionable preview", len(summary.NeedsRobert))
	}
	decision := summary.DecisionQueue[0].Decision
	if decision.ID != "decision-1" || decision.Status != "pending" || decision.Recommended == "" || decision.YesLabel != "Approve" || !decision.RequiresApproval {
		t.Fatalf("summary removed decision action fields: %#v", decision)
	}
	if decision.WorkflowID != "" || decision.WorkflowTitle != "" || decision.EvidenceURI != "" || decision.EvidenceLabel != "" || decision.YesConsequence != "" || decision.NoConsequence != "" || decision.Actor != "" || decision.CreatedAt != "" {
		t.Fatalf("summary retained decision detail fields: %#v", decision)
	}
}

func TestDetailSerializesEmptyConversationsAsArray(t *testing.T) {
	service := NewService(newFakeRepo(), nil)
	pursuit, err := service.Create(CreateRequest{Title: "Empty conversation context", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	detail, err := service.DetailForOwner("alice", pursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner returned error: %v", err)
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if strings.Contains(string(payload), `"conversations":null`) {
		t.Fatalf("conversation metadata serialized as null: %s", payload)
	}
}

func TestDetailLoadFailureBlocksDashboardInsteadOfInventingEmptyState(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Recover unavailable pursuit state", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	repo.linkedWorkflowErr = errors.New("database connection unavailable")

	if _, err := service.DetailForOwner("alice", created.ID); err == nil || !strings.Contains(err.Error(), "load pursuit linked workflows") {
		t.Fatalf("DetailForOwner error = %v, want linked-workflow load error", err)
	}
	dashboard, err := service.DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("DashboardForOwner returned error: %v", err)
	}
	if dashboard.Counts["blocked"] != 1 || dashboard.Counts["needsRobert"] != 1 || len(dashboard.Blocked) != 1 || len(dashboard.NeedsRobert) != 1 {
		t.Fatalf("dashboard hid detail failure: counts=%#v blocked=%#v needsRobert=%#v", dashboard.Counts, dashboard.Blocked, dashboard.NeedsRobert)
	}
	item := dashboard.Blocked[0]
	if !strings.Contains(item.CurrentState, "temporarily unavailable") || !strings.Contains(item.NextAction, "Retry") {
		t.Fatalf("dashboard failure item = %#v", item)
	}
	if len(dashboard.SystemReady) != 0 || len(dashboard.VAReady) != 0 || item.CompletionCandidate {
		t.Fatalf("failed detail was treated as ready: %#v", dashboard)
	}
}

func TestConversationArchiveLinksAreOwnerScopedAndMetadataOnly(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Resolve housing dispute", OwnerIdentity: "alice", ProjectKey: "housing"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	memoryID := uuid.New()
	repo.memories[memoryID] = models.ContextMemory{ID: memoryID, OwnerIdentity: "alice", ProjectKey: "housing", Summary: "Collect the housing evidence."}
	conversationID := uuid.New()
	repo.conversations[conversationID] = models.AIConversationArchive{
		ID:               conversationID,
		OwnerIdentity:    "alice",
		Platform:         "chatgpt",
		ExternalID:       "thread-housing-1",
		Title:            "Housing evidence discussion",
		SourceURI:        "chatgpt://thread-housing-1",
		MessageCount:     8,
		Preview:          "private conversation preview must not leak through pursuit detail",
		EncryptedPayload: []byte("private encrypted payload"),
		CapturedAt:       time.Now().UTC(),
	}

	result, err := service.AutoLinkMemory(AutoLinkMemoryRequest{
		OwnerIdentity:         "alice",
		MemoryID:              memoryID,
		Input:                 "Resolve housing dispute evidence",
		ProjectKey:            "housing",
		ConversationID:        conversationID,
		ConversationSourceURI: "chatgpt://thread-housing-1",
		ConversationLabel:     "Housing evidence discussion",
	})
	if err != nil {
		t.Fatalf("AutoLinkMemory returned error: %v", err)
	}
	if !result.Linked || !hasPursuitLink(result.Links, LinkAIConversation, conversationID.String()) {
		t.Fatalf("conversation metadata link missing from auto-link result: %#v", result)
	}

	detail, err := service.DetailForOwner("alice", pursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner returned error: %v", err)
	}
	if len(detail.Conversations) != 1 || detail.Conversations[0].ID != conversationID || detail.Conversations[0].Title != "Housing evidence discussion" {
		t.Fatalf("conversation metadata = %#v", detail.Conversations)
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if strings.Contains(string(payload), "private encrypted payload") || strings.Contains(string(payload), "private conversation preview") || strings.Contains(string(payload), "encryptedPayload") || strings.Contains(string(payload), "preview") {
		t.Fatalf("pursuit detail exposed archive content: %s", payload)
	}

	bobConversationID := uuid.New()
	repo.conversations[bobConversationID] = models.AIConversationArchive{ID: bobConversationID, OwnerIdentity: "bob", Platform: "chatgpt", ExternalID: "thread-bob", SourceURI: "chatgpt://thread-bob"}
	if _, err := service.Link(pursuit.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkAIConversation, LinkID: bobConversationID.String(), Relationship: "conversation_context"}); err == nil {
		t.Fatal("owner could link another user's conversation archive")
	}
}

func TestConversationIDFromAIChatSourceRejectsUntrustedFormats(t *testing.T) {
	conversationID := uuid.New()
	if got := conversationIDFromAIChatSource("ai_chat", conversationID.String()+":"+uuid.NewString()); got != conversationID {
		t.Fatalf("conversation ID = %s, want %s", got, conversationID)
	}
	for _, test := range []struct {
		sourceType string
		sourceID   string
	}{
		{sourceType: "email", sourceID: conversationID.String() + ":" + uuid.NewString()},
		{sourceType: "ai_chat", sourceID: conversationID.String()},
		{sourceType: "ai_chat", sourceID: "not-a-uuid:" + uuid.NewString()},
	} {
		if got := conversationIDFromAIChatSource(test.sourceType, test.sourceID); got != uuid.Nil {
			t.Fatalf("conversation ID for %#v = %s, want nil", test, got)
		}
	}
}

func hasPursuitLink(links []models.PursuitLink, linkType, linkID string) bool {
	for _, link := range links {
		if link.LinkType == linkType && link.LinkID == linkID {
			return true
		}
	}
	return false
}

func TestCreateClassifiesAndAuditsPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)

	created, err := service.Create(CreateRequest{
		Title:          "Vivare legal dispute",
		Description:    "Prepare evidence for lawyer and government-style hearing.",
		ProjectKey:     "vivare",
		DesiredOutcome: "Verified evidence bundle and approved legal reply.",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.RiskLevel != "high" || created.AutonomyLevel != "approve_before_execute" {
		t.Fatalf("risk/autonomy = %q/%q", created.RiskLevel, created.AutonomyLevel)
	}
	if created.NeedCategory != "safety_and_stability" {
		t.Fatalf("need category = %q", created.NeedCategory)
	}
	if len(created.SuccessCriteria) != 1 || created.SuccessCriteria[0].Status != CriterionPending {
		t.Fatalf("default success criteria = %#v", created.SuccessCriteria)
	}
	if len(created.StopConditions) != 1 || created.StopConditions[0].Status != StopMonitoring {
		t.Fatalf("default stop conditions = %#v", created.StopConditions)
	}
	activity, _ := repo.FindActivities(created.ID, 10)
	if len(activity) != 1 || activity[0].EventType != "pursuit.created" {
		t.Fatalf("activity = %#v", activity)
	}
}

func TestExplicitSuccessCriterionBlocksFalseCompletion(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title: "Ship verified feature",
		SuccessCriteria: []models.PursuitSuccessCriterion{{
			ID:               "acceptance-test",
			Description:      "Acceptance test passes against the live workflow",
			Status:           CriterionPending,
			EvidenceRequired: true,
		}},
		StopConditions: []models.PursuitStopCondition{{ID: "stop-on-regression", Description: "Stop when regression tests fail", Status: StopMonitoring}},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	_, err = service.Update(created.ID, UpdateRequest{Status: StatusCompleted, CompletionState: CompletionVerified})
	if err == nil || !strings.Contains(err.Error(), "success criterion") {
		t.Fatalf("completion error = %v, want unresolved success criterion guard", err)
	}
}

func TestSuccessCriterionCannotBeSatisfiedWithoutRequiredEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Evidence-backed outcome"})
	if err != nil {
		t.Fatal(err)
	}
	criteria := []models.PursuitSuccessCriterion{{
		ID: "explicit-criterion", Description: "Source-backed answer is delivered", Status: CriterionSatisfied, EvidenceRequired: true,
	}}
	_, err = service.Update(created.ID, UpdateRequest{SuccessCriteria: &criteria})
	if err == nil || !strings.Contains(err.Error(), "requires evidence") {
		t.Fatalf("Update error = %v, want evidence requirement", err)
	}
}

func TestTriggeredStopConditionPausesPlanning(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:          "Bounded pursuit",
		StopConditions: []models.PursuitStopCondition{{ID: "budget-stop", Description: "Budget boundary exceeded", Status: StopTriggered, Reason: "The approved ceiling was reached"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Plan(created.ID, PlanRequest{})
	if err == nil || !strings.Contains(err.Error(), "triggered stop condition") {
		t.Fatalf("Plan error = %v, want stop-condition guard", err)
	}
}

func TestDetailSurfacesGoalContractDeadlineAndDependencyBlockers(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	target := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	created, err := service.Create(CreateRequest{
		Title:        "Time-bounded pursuit",
		TargetAt:     target,
		Dependencies: []models.PursuitDependency{{ID: "external-answer", Label: "External answer arrives", Status: DependencyPending, Owner: "External"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Summary.TargetOverdue || detail.Summary.OpenDependencies != 1 || len(detail.Blockers) < 2 {
		t.Fatalf("goal contract summary/blockers = %#v / %#v", detail.Summary, detail.Blockers)
	}
}

func TestGoalContractGuardsRejectNewOperationalWork(t *testing.T) {
	tests := []struct {
		name        string
		request     CreateRequest
		wantMessage string
	}{
		{
			name: "triggered stop",
			request: CreateRequest{Title: "Paused pursuit", StopConditions: []models.PursuitStopCondition{{
				ID: "stop", Description: "Stop after a safety failure", Status: StopTriggered, Reason: "safety check failed",
			}}},
			wantMessage: "triggered stop condition",
		},
		{
			name:        "overdue target",
			request:     CreateRequest{Title: "Overdue pursuit", TargetAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)},
			wantMessage: "target date has passed",
		},
		{
			name: "blocked dependency",
			request: CreateRequest{Title: "Dependent pursuit", Dependencies: []models.PursuitDependency{{
				ID: "external", Label: "External authorization", Status: DependencyBlocked, Reason: "authorization was refused",
			}}},
			wantMessage: "dependency is blocked",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepo()
			workflowService := &fakeWorkflowIntake{repo: repo}
			service := NewService(repo, workflowService)
			created, err := service.Create(test.request)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Intake(created.ID, IntakeRequest{Input: "Create additional operational work"})
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Intake error = %v, want %q", err, test.wantMessage)
			}
			if workflowService.calls != 0 {
				t.Fatalf("guarded intake created %d workflow(s)", workflowService.calls)
			}
		})
	}
}

func TestParallelWorkflowCeilingBlocksAdditionalIntake(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{
		Title:          "Bounded parallel work",
		ResourceLimits: models.PursuitResourceLimits{MaxParallelWorkflows: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{ID: workflowID, CurrentState: workflow.StateWaitingInput}
	if _, err := service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String(), Relationship: "operational_work"}); err != nil {
		t.Fatal(err)
	}

	_, err = service.Intake(created.ID, IntakeRequest{Input: "Create a second workflow"})
	if err == nil || !strings.Contains(err.Error(), "parallel workflow ceiling reached (1 of 1 active)") {
		t.Fatalf("Intake error = %v, want parallel ceiling guard", err)
	}
	if workflowService.calls != 0 {
		t.Fatalf("parallel ceiling created %d workflow(s)", workflowService.calls)
	}

	completed := repo.workflows[workflowID]
	completed.CurrentState = workflow.StateCompleted
	repo.workflows[workflowID] = completed
	if _, err := service.Intake(created.ID, IntakeRequest{Input: "Create work after completion"}); err != nil {
		t.Fatalf("terminal workflow still consumed parallel capacity: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("workflow intake calls = %d, want 1", workflowService.calls)
	}
}

func TestPursuitResourceLedgerProjectsUsageIdempotentlyAndEnforcesCeilings(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "alice@example.com",
		Title:         "Resource-bounded pursuit",
		ResourceLimits: models.PursuitResourceLimits{
			MaxEffortHours: 2,
			MaxSpendEUR:    100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	effortRequest := AppendPursuitResourceEventRequest{
		Kind: ResourceKindEffort, EffortHours: 1.25, Note: "Reviewed source documents",
		IdempotencyKey: "effort-1", Actor: "alice@example.com",
	}
	first, err := service.AppendResourceEventForOwner("alice@example.com", created.ID, effortRequest)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.AppendResourceEventForOwner("alice@example.com", created.ID, effortRequest)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != first.ID {
		t.Fatalf("idempotent duplicate id = %s, want %s", duplicate.ID, first.ID)
	}
	if _, err := service.AppendResourceEventForOwner("alice@example.com", created.ID, AppendPursuitResourceEventRequest{
		Kind: ResourceKindEffort, EffortHours: 1.5, Note: "Different event",
		IdempotencyKey: "effort-1", Actor: "alice@example.com",
	}); err == nil || !strings.Contains(err.Error(), "idempotency key") {
		t.Fatalf("conflicting idempotency error = %v", err)
	}

	for _, request := range []AppendPursuitResourceEventRequest{
		{Kind: ResourceKindSpend, SpendEUR: 80, EvidenceURI: "receipt://one", IdempotencyKey: "spend-1", Actor: "alice@example.com"},
		{Kind: ResourceKindSpend, SpendEUR: 25, EvidenceURI: "receipt://two", IdempotencyKey: "spend-2", Actor: "alice@example.com"},
	} {
		if _, err := service.AppendResourceEventForOwner("alice@example.com", created.ID, request); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.EventCount != 3 || usage.EffortRecordedHours != 1.25 || usage.SpendNetEUR != 105 || !usage.SpendExceeded || usage.State != "exceeded" {
		t.Fatalf("resource usage = %#v", usage)
	}
	detail, err := service.DetailForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ActionQueues.SystemReady) != 0 {
		t.Fatalf("resource-blocked pursuit advertised system-ready work: %#v", detail.ActionQueues.SystemReady)
	}
	if len(detail.ActionQueues.NeedsRobert) == 0 || !strings.Contains(strings.ToLower(detail.ActionQueues.NeedsRobert[0].Reason), "resource ceiling") {
		t.Fatalf("resource blocker was not routed to Robert: %#v", detail.ActionQueues.NeedsRobert)
	}
	if _, err := service.IntakeForOwner("alice@example.com", created.ID, IntakeRequest{Input: "Create more work"}); err == nil || !strings.Contains(err.Error(), "spend ceiling exceeded") {
		t.Fatalf("intake ceiling error = %v", err)
	}
	validator := service.(interface {
		ValidatePursuitTaskAttempt(uuid.UUID, string) error
	})
	if err := validator.ValidatePursuitTaskAttempt(created.ID, "alice@example.com"); err == nil || !strings.Contains(err.Error(), "spend ceiling exceeded") {
		t.Fatalf("direct task ceiling error = %v", err)
	}

	if _, err := service.AppendResourceEventForOwner("alice@example.com", created.ID, AppendPursuitResourceEventRequest{
		Kind: ResourceKindRefund, SpendEUR: 10, EvidenceURI: "refund://one", IdempotencyKey: "refund-1", Actor: "alice@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	usage, err = service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.SpendNetEUR != 95 || usage.SpendExhausted || usage.State != "within_limits" {
		t.Fatalf("refunded resource usage = %#v", usage)
	}
	if _, err := service.IntakeForOwner("alice@example.com", created.ID, IntakeRequest{Input: "Create work within the restored ceiling"}); err != nil {
		t.Fatalf("intake after refund: %v", err)
	}
}

func TestPursuitResourceLedgerFailsClosedAndProtectsOwnerScope(t *testing.T) {
	base := newFakeRepo()
	wrapped := pursuitRepositoryWithoutResourceLedger{Repository: base}
	workflowService := &fakeWorkflowIntake{repo: base}
	service := NewService(wrapped, workflowService)
	created, err := service.Create(CreateRequest{
		OwnerIdentity:  "alice@example.com",
		Title:          "Ledger-required pursuit",
		ResourceLimits: models.PursuitResourceLimits{MaxEffortHours: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.State != "unavailable" || usage.BlockingReason == "" {
		t.Fatalf("unavailable usage = %#v", usage)
	}
	if _, err := service.IntakeForOwner("alice@example.com", created.ID, IntakeRequest{Input: "Unsafe unmetered work"}); err == nil || !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("unavailable-ledger intake error = %v", err)
	}
	if _, err := service.ResourceUsageForOwner("bob@example.com", created.ID); err == nil {
		t.Fatal("cross-owner resource usage read succeeded")
	}
}

func TestPursuitTaskResourceReservationIsIdempotentAndSettlesActualUsage(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, &fakeWorkflowIntake{})
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "alice@example.com",
		Title:         "Atomic task reservation", DesiredOutcome: "Complete one bounded local task",
		RiskLevel: "low", AutonomyLevel: "autonomous_safe",
		ResourceLimits: models.PursuitResourceLimits{MaxEffortHours: 2, MaxSpendEUR: 1},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	manager := service.(interface {
		ReservePursuitTaskResources(uuid.UUID, string, string, int64, int64) error
		SettlePursuitTaskResources(uuid.UUID, string, string, string, int64, int64) error
	})
	if err := manager.ReservePursuitTaskResources(created.ID, "alice@example.com", "task-plan:attempt:1", 45, 2_500); err != nil {
		t.Fatalf("ReservePursuitTaskResources: %v", err)
	}
	if err := manager.ReservePursuitTaskResources(created.ID, "alice@example.com", "task-plan:attempt:1", 45, 2_500); err != nil {
		t.Fatalf("idempotent reservation replay: %v", err)
	}
	if len(repo.resourceReservations) != 1 {
		t.Fatalf("reservations = %d, want one idempotent hold", len(repo.resourceReservations))
	}
	if countPursuitActivities(repo.activity[created.ID], "pursuit.resource_reserved") != 1 {
		t.Fatalf("reservation replay duplicated audit activity: %#v", repo.activity[created.ID])
	}
	if err := manager.SettlePursuitTaskResources(created.ID, "alice@example.com", "task-plan:attempt:1", ResourceReservationConsumed, 7, 2_000); err != nil {
		t.Fatalf("SettlePursuitTaskResources: %v", err)
	}
	if err := manager.SettlePursuitTaskResources(created.ID, "alice@example.com", "task-plan:attempt:1", ResourceReservationConsumed, 7, 2_000); err != nil {
		t.Fatalf("idempotent settlement replay: %v", err)
	}
	if len(repo.resourceSettlements) != 1 {
		t.Fatalf("settlements = %d, want one immutable settlement", len(repo.resourceSettlements))
	}
	if countPursuitActivities(repo.activity[created.ID], "pursuit.resource_reservation_settled") != 1 {
		t.Fatalf("settlement replay duplicated audit activity: %#v", repo.activity[created.ID])
	}
	if repo.pursuits[created.ID].LastActivityAt == nil {
		t.Fatal("resource ledger activity did not update pursuit freshness")
	}
	usage, err := service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatalf("ResourceUsageForOwner: %v", err)
	}
	if usage.EffortRecordedHours != 0.12 || usage.SpendNetEUR != 0.01 || usage.ActiveReservations != 0 {
		t.Fatalf("settled usage = %#v, want 7 minutes, rounded cent spend, and no active hold", usage)
	}
}

func TestPursuitResourceMutationRollsBackWhenAuditCannotCommit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, &fakeWorkflowIntake{})
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "alice@example.com", Title: "Atomic resource audit",
		RiskLevel: "low", AutonomyLevel: "autonomous_safe",
		ResourceLimits: models.PursuitResourceLimits{MaxEffortHours: 2, MaxSpendEUR: 1},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	manager := service.(interface {
		ReservePursuitTaskResources(uuid.UUID, string, string, int64, int64) error
		SettlePursuitTaskResources(uuid.UUID, string, string, string, int64, int64) error
	})
	operationID := "audit-failure:attempt:1"
	activityBefore := len(repo.activity[created.ID])
	repo.resourceAuditErr = errors.New("audit store unavailable")
	if err := manager.ReservePursuitTaskResources(created.ID, "alice@example.com", operationID, 30, 2_500); err == nil || !strings.Contains(err.Error(), "audit store unavailable") {
		t.Fatalf("reservation audit failure = %v", err)
	}
	if len(repo.resourceReservations) != 0 || len(repo.activity[created.ID]) != activityBefore {
		t.Fatalf("failed reservation mutated state: reservations=%d activities=%d", len(repo.resourceReservations), len(repo.activity[created.ID]))
	}

	repo.resourceAuditErr = nil
	if err := manager.ReservePursuitTaskResources(created.ID, "alice@example.com", operationID, 30, 2_500); err != nil {
		t.Fatalf("reservation after audit recovery: %v", err)
	}
	activityBefore = len(repo.activity[created.ID])
	repo.resourceAuditErr = errors.New("audit store unavailable")
	if err := manager.SettlePursuitTaskResources(created.ID, "alice@example.com", operationID, ResourceReservationConsumed, 5, 1_500); err == nil || !strings.Contains(err.Error(), "audit store unavailable") {
		t.Fatalf("settlement audit failure = %v", err)
	}
	if len(repo.resourceSettlements) != 0 || len(repo.resourceEvents[created.ID]) != 0 || len(repo.activity[created.ID]) != activityBefore {
		t.Fatalf("failed settlement mutated state: settlements=%d events=%d activities=%d", len(repo.resourceSettlements), len(repo.resourceEvents[created.ID]), len(repo.activity[created.ID]))
	}
	usage, err := service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatalf("ResourceUsageForOwner after failed settlement: %v", err)
	}
	if usage.ActiveReservations != 1 {
		t.Fatalf("failed settlement released reservation: %#v", usage)
	}

	repo.resourceAuditErr = nil
	if err := manager.SettlePursuitTaskResources(created.ID, "alice@example.com", operationID, ResourceReservationConsumed, 5, 1_500); err != nil {
		t.Fatalf("settlement after audit recovery: %v", err)
	}
	if len(repo.resourceSettlements) != 1 || len(repo.resourceEvents[created.ID]) != 2 {
		t.Fatalf("recovered settlement = %d settlements, %d events", len(repo.resourceSettlements), len(repo.resourceEvents[created.ID]))
	}
}

func TestConfirmedOrphanReservationReleaseIsAppendOnlyAndOwnerScoped(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, &fakeWorkflowIntake{})
	created, err := service.Create(CreateRequest{
		OwnerIdentity: "alice@example.com", Title: "Reconcile crashed task",
		RiskLevel: "low", AutonomyLevel: "autonomous_safe",
		ResourceLimits: models.PursuitResourceLimits{MaxEffortHours: 2, MaxSpendEUR: 1},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	manager := service.(interface {
		ReservePursuitTaskResources(uuid.UUID, string, string, int64, int64) error
	})
	if err := manager.ReservePursuitTaskResources(created.ID, "alice@example.com", "crashed-plan:attempt:1", 30, 125_000); err != nil {
		t.Fatalf("ReservePursuitTaskResources: %v", err)
	}
	var reservation models.PursuitResourceReservation
	for key, stored := range repo.resourceReservations {
		stored.ReservedAt = time.Now().UTC().Add(-25 * time.Hour)
		repo.resourceReservations[key] = stored
		reservation = stored
	}
	usage, err := service.ResourceUsageForOwner("alice@example.com", created.ID)
	if err != nil {
		t.Fatalf("ResourceUsageForOwner: %v", err)
	}
	if len(usage.Reservations) != 1 || !usage.Reservations[0].Stale || usage.Reservations[0].ReviewReason == "" {
		t.Fatalf("stale reservation projection = %#v", usage.Reservations)
	}
	if _, err := service.ReleaseResourceReservationForOwner("bob@example.com", created.ID, reservation.ID, ReleasePursuitResourceReservationRequest{
		ConfirmedOrphan: true, Reason: "Bob cannot release Alice's reservation.", Actor: "bob@example.com",
	}); err == nil {
		t.Fatal("cross-owner reservation release succeeded")
	}
	if _, err := service.ReleaseResourceReservationForOwner("alice@example.com", created.ID, reservation.ID, ReleasePursuitResourceReservationRequest{
		Reason: "The worker crashed and no process remains.", Actor: "alice@example.com",
	}); err == nil || !strings.Contains(err.Error(), "confirmedOrphan") {
		t.Fatalf("unconfirmed release error = %v", err)
	}
	usage, err = service.ReleaseResourceReservationForOwner("alice@example.com", created.ID, reservation.ID, ReleasePursuitResourceReservationRequest{
		ConfirmedOrphan: true, Reason: "The worker crashed and no process remains.", Actor: "alice@example.com",
	})
	if err != nil {
		t.Fatalf("ReleaseResourceReservationForOwner: %v", err)
	}
	if usage.ActiveReservations != 0 || len(usage.Reservations) != 0 {
		t.Fatalf("released usage = %#v", usage)
	}
	if len(repo.resourceReservations) != 1 || len(repo.resourceSettlements) != 1 {
		t.Fatalf("append-only ledger = %d reservations, %d settlements", len(repo.resourceReservations), len(repo.resourceSettlements))
	}
	if settlement := repo.resourceSettlements[reservation.ID]; settlement.Disposition != ResourceReservationReleased || settlement.Actor != "alice@example.com" || settlement.Reason != "The worker crashed and no process remains." {
		t.Fatalf("release settlement = %#v", settlement)
	}
	if countPursuitActivities(repo.activity[created.ID], "pursuit.resource_reservation_settled") != 1 ||
		countPursuitActivities(repo.activity[created.ID], "pursuit.resource_reservation_reconciled") != 1 {
		t.Fatalf("release audit activities = %#v", repo.activity[created.ID])
	}
	activityBeforeReplay := len(repo.activity[created.ID])
	if _, err := service.ReleaseResourceReservationForOwner("alice@example.com", created.ID, reservation.ID, ReleasePursuitResourceReservationRequest{
		ConfirmedOrphan: true, Reason: "The worker crashed and no process remains.", Actor: "alice@example.com",
	}); err != nil {
		t.Fatalf("idempotent release replay: %v", err)
	}
	if len(repo.activity[created.ID]) != activityBeforeReplay {
		t.Fatalf("release replay added duplicate audit activity: before=%d after=%d", activityBeforeReplay, len(repo.activity[created.ID]))
	}
}

func countPursuitActivities(items []models.PursuitActivity, eventType string) int {
	count := 0
	for _, item := range items {
		if item.EventType == eventType {
			count++
		}
	}
	return count
}

func TestReviewUsesPursuitCadenceWhenNoDateIsProvided(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Review on its own cadence", ReviewCadenceDays: 21})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(20*24*time.Hour + 23*time.Hour)
	detail, err := service.Review(created.ID, ReviewRequest{Action: "complete", Actor: "Robert"})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(21*24*time.Hour + time.Hour)
	if detail.Pursuit.NextReviewAt == nil || detail.Pursuit.NextReviewAt.Before(before) || detail.Pursuit.NextReviewAt.After(after) {
		t.Fatalf("next review = %v, want approximately 21 days", detail.Pursuit.NextReviewAt)
	}
}

func TestCreateCannotDowngradeDetectedHighRiskOrUpgradeAutonomy(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)

	created, err := service.Create(CreateRequest{
		Title:         "Send legal evidence to the government",
		Description:   "Prepare the lawyer reply and attach the insurance evidence.",
		RiskLevel:     "low",
		AutonomyLevel: "autonomous_full_local_only",
		Actor:         "test-operator",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.RiskLevel != "high" || created.AutonomyLevel != "approve_before_execute" {
		t.Fatalf("unsafe policy override was accepted: %q/%q", created.RiskLevel, created.AutonomyLevel)
	}
	activity, _ := repo.FindActivities(created.ID, 10)
	foundNormalization := false
	for _, item := range activity {
		if item.EventType == "pursuit.safety_normalized" && item.Actor == "test-operator" {
			foundNormalization = true
		}
	}
	if !foundNormalization {
		t.Fatalf("safety normalization was not audited: %#v", activity)
	}
}

func TestUpdateCannotDowngradeRiskWhenGoalBecomesHighRisk(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Organize documents", RiskLevel: "low", AutonomyLevel: "autonomous_safe"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	description := "Prepare the legal evidence bundle and lawyer reply."
	updated, err := service.Update(created.ID, UpdateRequest{
		Description:   &description,
		RiskLevel:     "low",
		AutonomyLevel: "autonomous_safe",
		Actor:         "test-operator",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.RiskLevel != "high" || updated.AutonomyLevel != "approve_before_execute" {
		t.Fatalf("unsafe update override was accepted: %q/%q", updated.RiskLevel, updated.AutonomyLevel)
	}
	activity, _ := repo.FindActivities(created.ID, 10)
	foundNormalization := false
	for _, item := range activity {
		if item.EventType == "pursuit.safety_normalized" && item.Actor == "test-operator" {
			foundNormalization = true
		}
	}
	if !foundNormalization {
		t.Fatalf("updated safety normalization was not audited: %#v", activity)
	}
}

func TestPursuitRationaleFlowsIntoOperationalContext(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	rationale := "Legal evidence must be ready before the municipality deadline."
	created, err := service.Create(CreateRequest{
		Title:          "Prepare case materials",
		WhyItMatters:   rationale,
		DesiredOutcome: "A source-linked evidence package is ready for review.",
		RiskLevel:      "low",
		AutonomyLevel:  "autonomous_safe",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.WhyItMatters != rationale {
		t.Fatalf("why it matters = %q, want %q", created.WhyItMatters, rationale)
	}
	if created.RiskLevel != "high" || created.AutonomyLevel != "approve_before_execute" {
		t.Fatalf("rationale did not raise the safety floor: %q/%q", created.RiskLevel, created.AutonomyLevel)
	}
	if created.Domain != "stability" {
		t.Fatalf("rationale did not classify the pursuit domain: %q", created.Domain)
	}

	matches, err := service.Match(MatchRequest{Input: "municipality legal evidence"})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matches) != 1 || matches[0].Pursuit.ID != created.ID {
		t.Fatalf("rationale was not included in matching: %#v", matches)
	}
	if !strings.Contains(pursuitPlanInput(*created), "Why it matters: "+rationale) {
		t.Fatalf("planner input did not include rationale: %q", pursuitPlanInput(*created))
	}

	updatedRationale := "A complete evidence trail protects the case and prevents an unsupported response."
	updated, err := service.Update(created.ID, UpdateRequest{WhyItMatters: &updatedRationale, Actor: "test-operator"})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.WhyItMatters != updatedRationale {
		t.Fatalf("updated rationale = %q, want %q", updated.WhyItMatters, updatedRationale)
	}

	brief, err := service.DelegationPackage(created.ID)
	if err != nil {
		t.Fatalf("DelegationPackage returned error: %v", err)
	}
	if brief.WhyItMatters != updatedRationale {
		t.Fatalf("delegation rationale = %q, want %q", brief.WhyItMatters, updatedRationale)
	}
}

func TestMatchUsesProjectAndExistingSourceLink(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "ASR insurance claim", ProjectKey: "asr", Description: "Insurance documents and claim follow-up"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := service.Link(created.ID, LinkRequest{LinkType: LinkSourceItem, LinkID: "email-1", Relationship: "evidence"}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	sourceMatches, err := service.Match(MatchRequest{SourceType: LinkSourceItem, SourceID: "email-1"})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(sourceMatches) != 1 || sourceMatches[0].Score < 0.9 {
		t.Fatalf("source match = %#v", sourceMatches)
	}
	if _, err := service.Link(created.ID, LinkRequest{
		LinkType:     LinkWorkflow,
		LinkID:       uuid.New().String(),
		Relationship: "operational_work",
		SourceURI:    "local://email/asr-claim-12",
	}); err != nil {
		t.Fatalf("Link source URI returned error: %v", err)
	}
	uriMatches, err := service.Match(MatchRequest{SourceURI: "local://email/asr-claim-12"})
	if err != nil {
		t.Fatalf("Match source URI returned error: %v", err)
	}
	if len(uriMatches) != 1 || uriMatches[0].Pursuit.ID != created.ID || uriMatches[0].Score < 0.9 {
		t.Fatalf("source URI match = %#v", uriMatches)
	}

	projectMatches, err := service.Match(MatchRequest{Input: "follow up insurance claim documents", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(projectMatches) == 0 || projectMatches[0].Pursuit.ID != created.ID {
		t.Fatalf("project match = %#v", projectMatches)
	}
}

func TestMatchUsesVisibleExactLinkWhenAnotherOwnerHasTheSameSource(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice legal evidence", OwnerIdentity: "alice", ProjectKey: "alice-case"})
	if err != nil {
		t.Fatalf("create alice pursuit: %v", err)
	}
	bob, err := service.Create(CreateRequest{Title: "Bob legal evidence", OwnerIdentity: "bob", ProjectKey: "bob-case"})
	if err != nil {
		t.Fatalf("create bob pursuit: %v", err)
	}
	const sourceID = "shared-message-41"
	const sourceURI = "local://mail/shared-message-41"
	if _, err := service.Link(bob.ID, LinkRequest{
		LinkType: LinkSourceItem, LinkID: sourceID, Relationship: "evidence", SourceURI: sourceURI, Confidence: 0.99,
	}); err != nil {
		t.Fatalf("link bob source: %v", err)
	}
	if _, err := service.Link(alice.ID, LinkRequest{
		LinkType: LinkSourceItem, LinkID: sourceID, Relationship: "evidence", SourceURI: sourceURI, Confidence: 0.10,
	}); err != nil {
		t.Fatalf("link alice source: %v", err)
	}

	sourceMatches, err := service.Match(MatchRequest{OwnerIdentity: " alice ", SourceType: LinkSourceItem, SourceID: sourceID})
	if err != nil {
		t.Fatalf("match by source id: %v", err)
	}
	if len(sourceMatches) != 1 || sourceMatches[0].Pursuit.ID != alice.ID {
		t.Fatalf("alice source matches = %#v, want alice pursuit", sourceMatches)
	}

	uriMatches, err := service.Match(MatchRequest{OwnerIdentity: "alice", SourceURI: sourceURI})
	if err != nil {
		t.Fatalf("match by source URI: %v", err)
	}
	if len(uriMatches) != 1 || uriMatches[0].Pursuit.ID != alice.ID {
		t.Fatalf("alice URI matches = %#v, want alice pursuit", uriMatches)
	}

	outsiderMatches, err := service.Match(MatchRequest{OwnerIdentity: "charlie", SourceType: LinkSourceItem, SourceID: sourceID})
	if err != nil {
		t.Fatalf("match for unrelated owner: %v", err)
	}
	if len(outsiderMatches) != 0 {
		t.Fatalf("unrelated owner received exact source match: %#v", outsiderMatches)
	}
}

func TestAutoLinkWorkflowLinksOperationalWorkAndSourceProvenance(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "ASR insurance claim", ProjectKey: "asr", Description: "Insurance documents and claim follow-up"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	rawItemID := uuid.New()
	extractionID := uuid.New()
	repo.sourceItems[rawItemID] = models.SourceRawItem{
		ID:         rawItemID,
		Title:      "ASR claim email",
		ItemType:   "email",
		ProjectKey: "asr",
		SourceURI:  "local://email/asr-claim",
	}
	repo.extractions[extractionID] = models.SourceExtraction{
		ID:          extractionID,
		RawItemID:   rawItemID,
		ProjectKey:  "asr",
		Summary:     "Follow up on ASR claim evidence.",
		SourceURI:   "local://email/asr-claim",
		SourceLabel: "ASR claim email",
	}

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		WorkflowID:   workflowID,
		Input:        "follow up insurance claim documents",
		ProjectKey:   "asr",
		SourceURI:    "local://email/asr-claim",
		SourceLabel:  "ASR claim email",
		ExtractionID: extractionID.String(),
		RawItemID:    rawItemID.String(),
		Actor:        "source-worker",
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if !result.Linked || result.PursuitID != created.ID || result.Score < defaultAutoLinkMinimumScore {
		t.Fatalf("auto-link result = %#v", result)
	}
	links, _ := repo.FindLinks(created.ID)
	found := map[string]bool{}
	for _, link := range links {
		found[link.LinkType] = true
	}
	for _, linkType := range []string{LinkWorkflow, LinkSourceItem, LinkSourceExtraction} {
		if !found[linkType] {
			t.Fatalf("missing %s link in %#v", linkType, links)
		}
	}
	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Workflows) != 1 || len(detail.SourceItems) != 1 || len(detail.SourceExtractions) != 1 {
		t.Fatalf("detail workflow/source counts = %d/%d/%d", len(detail.Workflows), len(detail.SourceItems), len(detail.SourceExtractions))
	}
}

func TestAutoLinkWorkflowSkipsWeakPursuitMatch(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "ASR insurance claim", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		WorkflowID: uuid.New(),
		Input:      "garden appointment and unrelated shopping reminder",
		ProjectKey: "garden-admin",
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if result.Linked {
		t.Fatalf("weak match should not link: %#v", result)
	}
	links, _ := repo.FindLinks(created.ID)
	if len(links) != 0 {
		t.Fatalf("links = %#v, want none", links)
	}
}

func TestAutoLinkWorkflowDoesNotAttachOperationalWorkToClosedPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Completed ASR insurance claim", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	closed := repo.pursuits[created.ID]
	closed.Status = StatusCompleted
	closed.CompletionState = CompletionVerified
	repo.pursuits[created.ID] = closed

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		WorkflowID: uuid.New(),
		Input:      "New ASR claim follow-up arrived after completion.",
		ProjectKey: "asr",
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if result.Linked || !strings.Contains(result.Message, "closed") {
		t.Fatalf("closed pursuit must reject operational auto-linking: %#v", result)
	}
	links, _ := repo.FindLinks(created.ID)
	if len(links) != 0 {
		t.Fatalf("closed pursuit links = %#v, want none", links)
	}
}

func TestRouteIntakeCreatesCandidateInsteadOfReopeningClosedPursuit(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	closed, err := service.Create(CreateRequest{Title: "Completed ASR insurance claim", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	closedRecord := repo.pursuits[closed.ID]
	closedRecord.Status = StatusCompleted
	closedRecord.CompletionState = CompletionVerified
	repo.pursuits[closed.ID] = closedRecord

	result, err := service.RouteIntake(IntakeRequest{
		Input:       "A new ASR claim follow-up arrived after the earlier claim was completed.",
		ProjectKey:  "asr",
		SourceType:  "email",
		SourceID:    "new-asr-follow-up",
		SourceURI:   "email://asr/new-follow-up",
		SourceLabel: "New ASR follow-up",
	})
	if err != nil {
		t.Fatalf("RouteIntake returned error: %v", err)
	}
	if !result.CreatedCandidate || result.PursuitID == uuid.Nil || result.PursuitID == closed.ID {
		t.Fatalf("closed pursuit should produce a new candidate: %#v", result)
	}
	if workflowService.calls != 0 || len(repo.workflows) != 0 {
		t.Fatalf("closed-pursuit follow-up created workflow work: calls=%d workflows=%#v", workflowService.calls, repo.workflows)
	}
	persistedClosed, err := repo.FindByID(closed.ID)
	if err != nil {
		t.Fatalf("FindByID(closed): %v", err)
	}
	if persistedClosed.Status != StatusCompleted || persistedClosed.CompletionState != CompletionVerified {
		t.Fatalf("closed pursuit was reactivated: %#v", persistedClosed)
	}
	links, _ := repo.FindLinks(closed.ID)
	if len(links) != 0 {
		t.Fatalf("closed pursuit received new operational links: %#v", links)
	}
}

func TestAutoLinkWorkflowCreatesReviewableCandidateWhenNoMatchExists(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	workflowID := uuid.New()
	rawItemID := uuid.New()
	extractionID := uuid.New()

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		WorkflowID:           workflowID,
		Input:                "Prepare YouTube appeal evidence and schedule follow-up with platform support.",
		ProjectKey:           "youtube-removal",
		SourceType:           "ai_chat",
		SourceID:             "chat-1:insight-1",
		SourceURI:            "chat://youtube-removal/1",
		SourceLabel:          "YouTube appeal planning chat",
		RawItemID:            rawItemID.String(),
		ExtractionID:         extractionID.String(),
		Actor:                "memory-engine",
		AllowCreateCandidate: true,
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if result.Linked || !result.Created || result.PursuitID == uuid.Nil {
		t.Fatalf("candidate result = %#v", result)
	}

	detail, err := service.Detail(result.PursuitID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Pursuit.Status != StatusActive || detail.Pursuit.AutonomyLevel != "suggest" {
		t.Fatalf("candidate pursuit state = %s/%s", detail.Pursuit.Status, detail.Pursuit.AutonomyLevel)
	}
	if detail.Pursuit.SourceOfCreation != "ai_chat_pursuit_candidate" {
		t.Fatalf("source of creation = %q", detail.Pursuit.SourceOfCreation)
	}
	links, _ := repo.FindLinks(result.PursuitID)
	found := map[string]bool{}
	for _, link := range links {
		found[link.LinkType+":"+link.Relationship] = true
	}
	if found[LinkWorkflow+":candidate_operational_work"] {
		t.Fatalf("unaccepted candidate received pre-existing workflow work: %#v", links)
	}
	for _, expected := range []string{
		LinkSourceItem + ":candidate_source_record",
		LinkSourceExtraction + ":candidate_source_extraction",
	} {
		if !found[expected] {
			t.Fatalf("candidate missing link %s in %#v", expected, links)
		}
	}
}

func TestAutoCreatedCandidateRequiresRobertDecisionAndCanBeAccepted(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	workflowID := uuid.New()

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		WorkflowID:           workflowID,
		Input:                "Imported AI agent thread found OpenClaw setup work that may belong in HAI.",
		ProjectKey:           "hai",
		SourceType:           "openclaw",
		SourceURI:            "openclaw://import/thread-1",
		SourceLabel:          "OpenClaw integration import",
		Actor:                "agent-runtime",
		AllowCreateCandidate: true,
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if !result.Created {
		t.Fatalf("expected auto-created pursuit candidate, got %#v", result)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if len(dashboard.NeedsRobert) == 0 || dashboard.NeedsRobert[0].Pursuit.ID != result.PursuitID {
		t.Fatalf("candidate was not surfaced in Robert queue: %#v", dashboard.NeedsRobert)
	}

	detail, err := service.Detail(result.PursuitID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.NeedsRobert == 0 {
		t.Fatalf("candidate summary did not require Robert: %#v", detail.Summary)
	}
	if len(detail.ActionQueues.NeedsRobert) == 0 {
		t.Fatalf("candidate action was not Robert-owned: %#v", detail.ActionQueues)
	}
	candidateDecision := false
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "pursuit_candidate_review" {
			candidateDecision = true
			if !decision.RequiresApproval || decision.YesLabel != "Accept and plan" || decision.NoLabel != "Archive" {
				t.Fatalf("candidate decision labels/gate invalid: %#v", decision)
			}
		}
	}
	if !candidateDecision {
		t.Fatalf("candidate review decision missing: %#v", detail.DecisionQueue)
	}

	accepted, err := service.AcceptCandidate(result.PursuitID, PlanRequest{Actor: "Robert"})
	if err != nil {
		t.Fatalf("AcceptCandidate returned error: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("accepted candidate created %d governed workflow(s), want 1", workflowService.calls)
	}
	if accepted.Pursuit.SourceOfCreation != "openclaw_pursuit_intake" {
		t.Fatalf("source marker was not cleared after acceptance: %q", accepted.Pursuit.SourceOfCreation)
	}
	for _, decision := range accepted.DecisionQueue {
		if decision.DecisionType == "pursuit_candidate_review" {
			t.Fatalf("accepted candidate still has pending candidate review decision: %#v", accepted.DecisionQueue)
		}
	}
}

func TestAutoLinkMemoryLinksStableContextToPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Vivare legal dispute", ProjectKey: "vivare", Description: "Housing and legal evidence pursuit"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	memoryID := uuid.New()
	repo.memories[memoryID] = models.ContextMemory{
		ID:          memoryID,
		ProjectKey:  "vivare",
		Kind:        "rule",
		Content:     "Always use formal Dutch legal tone for Vivare correspondence.",
		Summary:     "Formal Dutch legal tone for Vivare.",
		SourceURI:   "https://chatgpt.com/c/vivare",
		SourceLabel: "chatgpt: Vivare legal dispute",
	}

	result, err := service.AutoLinkMemory(AutoLinkMemoryRequest{
		MemoryID:    memoryID,
		Input:       "Vivare legal dispute rule: Always use formal Dutch legal tone for Vivare correspondence.",
		ProjectKey:  "vivare",
		SourceURI:   "https://chatgpt.com/c/vivare",
		SourceLabel: "chatgpt: Vivare legal dispute",
		Actor:       "memory-engine",
	})
	if err != nil {
		t.Fatalf("AutoLinkMemory returned error: %v", err)
	}
	if !result.Linked || result.PursuitID != created.ID || result.Score < defaultAutoLinkMinimumScore {
		t.Fatalf("auto-link memory result = %#v", result)
	}
	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Memories) != 1 || detail.Memories[0].ID != memoryID {
		t.Fatalf("detail memories = %#v, want linked memory %s", detail.Memories, memoryID)
	}
}

func TestAutoLinkMemoryRefreshDoesNotPersistOtherOwnerWorkflowState(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Alice evidence pursuit", OwnerIdentity: "alice", ProjectKey: "housing"})
	if err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}
	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{ID: bobWorkflowID, OwnerIdentity: "bob", Title: "Bob private workflow"}
	repo.links[uuid.New()] = models.PursuitLink{
		ID:           uuid.New(),
		PursuitID:    pursuit.ID,
		LinkType:     LinkWorkflow,
		LinkID:       bobWorkflowID.String(),
		Relationship: "legacy_malformed",
	}
	memoryID := uuid.New()
	repo.memories[memoryID] = models.ContextMemory{ID: memoryID, OwnerIdentity: "alice", ProjectKey: "housing", Content: "Use the verified housing evidence timeline."}

	result, err := service.AutoLinkMemory(AutoLinkMemoryRequest{
		OwnerIdentity: "alice",
		MemoryID:      memoryID,
		Input:         "Update Alice evidence pursuit with the verified housing evidence timeline.",
		ProjectKey:    "housing",
		Actor:         "memory-engine",
	})
	if err != nil {
		t.Fatalf("AutoLinkMemory: %v", err)
	}
	if !result.Linked || result.PursuitID != pursuit.ID {
		t.Fatalf("auto-link result = %#v", result)
	}
	updated, err := repo.FindByID(pursuit.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if strings.Contains(updated.CurrentStateSummary, "1 linked workflows") || strings.Contains(updated.CurrentStateSummary, "Bob private workflow") {
		t.Fatalf("owner-scoped refresh persisted other-owner workflow state: %#v", updated)
	}
}

func TestIntakeCreatesWorkflowAndLinksOperationalWork(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Government letter response", OwnerIdentity: "alice", ProjectKey: "letter", Description: "Legal/government reply"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	detail, err := service.Intake(created.ID, IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "New government letter asks for a reply before Friday.",
		SourceType:    "email",
		SourceID:      "email-2",
		SourceURI:     "local://email-2",
		SourceLabel:   "Government letter",
	})
	if err != nil {
		t.Fatalf("Intake returned error: %v", err)
	}
	if workflowService.received.ProjectKey != "letter" {
		t.Fatalf("workflow project key = %q", workflowService.received.ProjectKey)
	}
	if workflowService.received.OwnerIdentity != "alice" {
		t.Fatalf("workflow owner = %q, want persisted pursuit owner alice", workflowService.received.OwnerIdentity)
	}
	if len(detail.Workflows) != 1 || len(detail.ApprovalItems) != 1 {
		t.Fatalf("detail workflows/approvals = %#v / %#v", detail.Workflows, detail.ApprovalItems)
	}
	if len(detail.NextActions) == 0 || detail.NextActions[0].Owner != "Robert" {
		t.Fatalf("next actions = %#v", detail.NextActions)
	}
	links, _ := repo.FindLinks(created.ID)
	foundWorkflowLink := false
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.Relationship == "operational_work" {
			foundWorkflowLink = true
		}
	}
	if !foundWorkflowLink {
		t.Fatalf("workflow link missing: %#v", links)
	}
}

func TestCandidateIntakeRequiresExplicitAcceptanceBeforeCreatingWorkflow(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	candidate, err := service.Create(CreateRequest{
		Title:            "Imported legal follow-up",
		OwnerIdentity:    "alice",
		SourceOfCreation: "ai_chat_pursuit_candidate",
		Status:           StatusWaiting,
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}

	_, err = service.IntakeForOwner("alice", candidate.ID, IntakeRequest{
		Input:      "Draft the requested response.",
		SourceType: "ai_chat",
	})
	if err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("candidate intake error = %v, want explicit acceptance guard", err)
	}
	if workflowService.calls != 0 {
		t.Fatalf("candidate intake created %d workflow(s), want none", workflowService.calls)
	}

	if _, err := service.PlanForOwner("alice", candidate.ID, PlanRequest{Actor: "alice"}); err == nil || !strings.Contains(err.Error(), "explicit approval action") {
		t.Fatalf("PlanForOwner error = %v, want explicit approval action guard", err)
	}
	if workflowService.calls != 0 {
		t.Fatalf("generic candidate plan created %d workflow(s), want none", workflowService.calls)
	}
	if _, err := service.AcceptCandidateForOwner("alice", candidate.ID, PlanRequest{Actor: "alice"}); err != nil {
		t.Fatalf("AcceptCandidateForOwner returned error: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("accepted candidate created %d workflow(s), want one", workflowService.calls)
	}
}

func TestAutoLinkWorkflowDoesNotAttachOperationalWorkToUnacceptedCandidate(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	candidate, err := service.Create(CreateRequest{
		Title:            "Imported runtime recovery",
		OwnerIdentity:    "alice",
		ProjectKey:       "018-HAI",
		SourceOfCreation: "ai_chat_pursuit_candidate",
		Status:           StatusWaiting,
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	workflowID := uuid.New()

	result, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		OwnerIdentity: "alice",
		WorkflowID:    workflowID,
		Input:         "Prepare the local runtime recovery workflow safely.",
		ProjectKey:    "018-HAI",
		SourceType:    "ai_chat",
		SourceID:      "conversation:insight",
		SourceURI:     "chatgpt://conversation/runtime-recovery",
		Actor:         "memory-engine",
	})
	if err != nil {
		t.Fatalf("AutoLinkWorkflow returned error: %v", err)
	}
	if result.Linked || result.PursuitID != candidate.ID || !strings.Contains(result.Message, "awaits explicit acceptance") {
		t.Fatalf("candidate auto-link result = %#v", result)
	}
	links, err := repo.FindLinks(candidate.ID)
	if err != nil {
		t.Fatalf("FindLinks returned error: %v", err)
	}
	if pursuitLinkExists(links, LinkWorkflow, workflowID.String(), "operational_work") {
		t.Fatalf("unaccepted candidate received an operational workflow link: %#v", links)
	}
	activities, err := repo.FindActivities(candidate.ID, 20)
	if err != nil {
		t.Fatalf("FindActivities returned error: %v", err)
	}
	foundDeferredAudit := false
	for _, activity := range activities {
		if activity.EventType == "pursuit.candidate_workflow_link_deferred" && activity.SourceID == workflowID.String() {
			foundDeferredAudit = true
			break
		}
	}
	if !foundDeferredAudit {
		t.Fatalf("candidate workflow deferral was not audited: %#v", activities)
	}
}

func TestIntakeLinksSourceReferenceIntoPursuitEvidence(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "ASR claim evidence", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	rawID := uuid.New()
	extractionID := uuid.New()
	repo.sourceItems[rawID] = models.SourceRawItem{
		ID:         rawID,
		SourceID:   uuid.New(),
		ExternalID: "email-claim-1",
		ProjectKey: "asr",
		ItemType:   "email",
		Title:      "ASR requested receipt evidence",
		SourceURI:  "local://source/email-claim-1",
	}
	repo.extractions[extractionID] = models.SourceExtraction{
		ID:          extractionID,
		RawItemID:   rawID,
		ProjectKey:  "asr",
		Summary:     "ASR requested receipt evidence before Friday.",
		SourceURI:   "local://source/email-claim-1#extraction",
		SourceLabel: "ASR evidence request",
	}

	detail, err := service.Intake(created.ID, IntakeRequest{
		Input:       "Create a workflow to collect the receipts requested by ASR.",
		ProjectKey:  "asr",
		SourceType:  LinkSourceExtraction,
		SourceID:    extractionID.String(),
		SourceURI:   "local://source/email-claim-1#extraction",
		SourceLabel: "ASR evidence request",
	})
	if err != nil {
		t.Fatalf("Intake returned error: %v", err)
	}
	if len(detail.Workflows) != 1 {
		t.Fatalf("workflows = %d, want 1", len(detail.Workflows))
	}
	if len(detail.SourceExtractions) != 1 || detail.SourceExtractions[0].ID != extractionID {
		t.Fatalf("source extractions = %#v, want linked extraction %s", detail.SourceExtractions, extractionID)
	}
	links, _ := repo.FindLinks(created.ID)
	foundWorkflow := false
	foundExtraction := false
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.SourceURI == "local://source/email-claim-1#extraction" {
			foundWorkflow = true
		}
		if link.LinkType == LinkSourceExtraction && link.LinkID == extractionID.String() && link.Relationship == "source_extraction" {
			foundExtraction = true
		}
	}
	if !foundWorkflow || !foundExtraction {
		t.Fatalf("links did not preserve workflow and extraction provenance: %#v", links)
	}
}

func TestIntakePreservesDecisionEvidenceOnWorkflowLink(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Recover failed runtime", ProjectKey: "hai"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	sourceURI := "automation-launch://" + launchID.String()
	startedAt := time.Now().UTC()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "agent runtime registry is not configured",
		AuditEvents: []string{
			"launch requested",
			"agent runtime registry unavailable",
		},
		StartedAt:   startedAt,
		CompletedAt: startedAt.Add(time.Second),
	})

	detail, err := service.Intake(created.ID, IntakeRequest{
		Input:          "Create a governed recovery workflow before retrying this runtime attempt.",
		ProjectKey:     "hai",
		SourceType:     "pursuit_decision",
		SourceID:       "runtime:" + launchID.String() + ":review",
		SourceURI:      sourceURI,
		SourceLabel:    "openclaw",
		ContentType:    "runtime_attempt_review",
		Trigger:        "pursuit_decision_approved",
		Actor:          "Robert",
		RequiresReview: true,
		ReviewReason:   "blocked: agent runtime registry is not configured",
	})
	if err != nil {
		t.Fatalf("Intake returned error: %v", err)
	}
	if workflowService.received.SourceURI != sourceURI || workflowService.received.SourceLabel != "openclaw" {
		t.Fatalf("workflow provenance = %q/%q", workflowService.received.SourceURI, workflowService.received.SourceLabel)
	}
	if workflowService.received.ContentType != "runtime_attempt_review" || workflowService.received.Trigger != "pursuit_decision_approved" {
		t.Fatalf("workflow decision metadata = %q/%q", workflowService.received.ContentType, workflowService.received.Trigger)
	}
	links, _ := repo.FindLinks(created.ID)
	found := false
	foundRuntimeAttempt := false
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.SourceURI == sourceURI && link.SourceLabel == "openclaw" {
			found = true
		}
		if link.LinkType == LinkAgentRuntime && link.LinkID == launchID.String() && link.Relationship == "execution_attempt" {
			foundRuntimeAttempt = true
		}
	}
	if !found {
		t.Fatalf("workflow link did not retain decision evidence: %#v", links)
	}
	if !foundRuntimeAttempt {
		t.Fatalf("runtime launch evidence link missing after approved intake: %#v", links)
	}
	if len(detail.Workflows) != 1 {
		t.Fatalf("detail workflows = %#v", detail.Workflows)
	}
	if len(detail.RuntimeAttempts) != 1 || detail.RuntimeAttempts[0].ID != launchID {
		t.Fatalf("detail runtime attempts = %#v, want approved intake to retain runtime evidence", detail.RuntimeAttempts)
	}
	if !timelineContains(detail.Timeline, "runtime_audit", "agent runtime registry unavailable") {
		t.Fatalf("timeline = %#v, want linked runtime audit retained after recovery intake", detail.Timeline)
	}
	foundResolution := false
	for _, activity := range detail.Activity {
		if activity.EventType == "pursuit.decision_resolved" && activity.SourceID == "runtime:"+launchID.String()+":review" {
			foundResolution = true
		}
	}
	if !foundResolution {
		t.Fatalf("approved decision resolution activity missing: %#v", detail.Activity)
	}
}

func TestRouteIntakeMatchesExistingPursuitAndCreatesGovernedWorkflow(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{
		Title:         "Vivare legal dispute",
		OwnerIdentity: "alice",
		ProjectKey:    "vivare",
		Description:   "Housing/legal evidence and formal replies.",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	result, err := service.RouteIntake(IntakeRequest{
		Input:         "New Vivare lawyer email asks for evidence before the hearing.",
		ProjectKey:    "vivare",
		SourceType:    "email",
		SourceID:      "email-77",
		SourceURI:     "local://email/vivare-77",
		SourceLabel:   "Vivare lawyer email",
		Actor:         "source-worker",
		OwnerIdentity: "alice",
	})
	if err != nil {
		t.Fatalf("RouteIntake returned error: %v", err)
	}
	if !result.Matched || result.CreatedCandidate || result.PursuitID != created.ID || result.Detail == nil {
		t.Fatalf("route result = %#v, want existing pursuit detail", result)
	}
	if result.Mode != "matched_existing" || result.Score < defaultAutoLinkMinimumScore {
		t.Fatalf("route mode/score = %s/%.2f", result.Mode, result.Score)
	}
	if workflowService.received.ProjectKey != "vivare" || workflowService.received.SourceURI != "local://email/vivare-77" {
		t.Fatalf("workflow intake provenance = %#v", workflowService.received)
	}
	if workflowService.received.OwnerIdentity != "alice" {
		t.Fatalf("matched workflow owner = %q, want persisted pursuit owner alice", workflowService.received.OwnerIdentity)
	}
	links, _ := repo.FindLinks(created.ID)
	found := false
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.Relationship == "operational_work" && link.SourceURI == "local://email/vivare-77" {
			found = true
		}
	}
	if !found {
		t.Fatalf("routed workflow link missing provenance: %#v", links)
	}
}

func TestDetailForOwnerHidesLegacyCrossOwnerWorkflowLink(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alicePursuit, err := service.Create(CreateRequest{
		Title:         "Alice private pursuit",
		OwnerIdentity: "alice",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{
		ID:               bobWorkflowID,
		OwnerIdentity:    "bob",
		Title:            "Bob private approval workflow",
		CurrentState:     workflow.StateNeedsApproval,
		RequiresApproval: true,
		ApprovalStatus:   "pending",
	}
	foreignURI := "workflow://private/bob-approval"
	foreignLinkID := uuid.New()
	repo.links[foreignLinkID] = models.PursuitLink{
		ID:           foreignLinkID,
		PursuitID:    alicePursuit.ID,
		LinkType:     LinkWorkflow,
		LinkID:       bobWorkflowID.String(),
		Relationship: "legacy_import",
		SourceURI:    foreignURI,
		SourceLabel:  "Bob private workflow",
		CreatedAt:    time.Now().UTC(),
	}

	detail, err := service.DetailForOwner("alice", alicePursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner returned error: %v", err)
	}
	if len(detail.Links) != 0 || len(detail.Workflows) != 0 || len(detail.ApprovalItems) != 0 {
		t.Fatalf("owner detail exposed legacy foreign workflow: %#v", detail)
	}
	if _, err := service.ResolveEvidenceForOwner("alice", alicePursuit.ID, foreignURI); err == nil {
		t.Fatalf("ResolveEvidenceForOwner resolved evidence from a hidden legacy link")
	}
	overview, err := service.ApprovalsForOwner("alice", alicePursuit.ID)
	if err != nil {
		t.Fatalf("ApprovalsForOwner returned error: %v", err)
	}
	if len(overview.ApprovalItems) != 0 || overview.Counts["approvalItems"] != 0 {
		t.Fatalf("owner approval overview exposed legacy foreign workflow: %#v", overview)
	}
}

func TestActivityForOwnerRejectsAnotherOwnersAuditFeed(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice private activity", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bob, err := service.Create(CreateRequest{Title: "Bob private activity", OwnerIdentity: "bob"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}
	if _, err := repo.CreateActivity(&models.PursuitActivity{PursuitID: bob.ID, EventType: "pursuit.private", Message: "Bob's private source activity"}); err != nil {
		t.Fatalf("Create Bob activity: %v", err)
	}

	if _, err := service.ActivityForOwner("alice", bob.ID); err == nil {
		t.Fatalf("ActivityForOwner exposed another owner's audit feed")
	}
	activities, err := service.ActivityForOwner("alice", alice.ID)
	if err != nil {
		t.Fatalf("ActivityForOwner returned error for owner pursuit: %v", err)
	}
	if len(activities) == 0 {
		t.Fatalf("ActivityForOwner omitted owner's audit records")
	}
}

func TestDashboardForOwnerHidesLegacyCrossOwnerWorkflowLink(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alicePursuit, err := service.Create(CreateRequest{
		Title:         "Alice dashboard pursuit",
		OwnerIdentity: "alice",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{
		ID:               bobWorkflowID,
		OwnerIdentity:    "bob",
		Title:            "Bob private approval workflow",
		CurrentState:     workflow.StateNeedsApproval,
		RequiresApproval: true,
		ApprovalStatus:   "pending",
	}
	foreignLinkID := uuid.New()
	repo.links[foreignLinkID] = models.PursuitLink{
		ID:           foreignLinkID,
		PursuitID:    alicePursuit.ID,
		LinkType:     LinkWorkflow,
		LinkID:       bobWorkflowID.String(),
		Relationship: "legacy_import",
		SourceURI:    "workflow://private/bob-dashboard-approval",
		SourceLabel:  "Bob private workflow",
		CreatedAt:    time.Now().UTC(),
	}

	dashboard, err := service.DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("DashboardForOwner returned error: %v", err)
	}
	if dashboard.Counts["needsRobert"] != 0 || dashboard.Counts["decisionQueue"] != 0 || len(dashboard.NeedsRobert) != 0 || len(dashboard.DecisionQueue) != 0 {
		t.Fatalf("owner dashboard exposed legacy foreign workflow: %#v", dashboard)
	}
	if len(dashboard.RecentlyChanged) != 1 || dashboard.RecentlyChanged[0].NeedsRobert != 0 || dashboard.RecentlyChanged[0].DecisionCards != 0 {
		t.Fatalf("dashboard list item retained foreign workflow state: %#v", dashboard.RecentlyChanged)
	}
}

func TestDashboardBulkProjectionMatchesEstablishedOwnerScopedPath(t *testing.T) {
	repo := newFakeRepo()
	now := time.Now().UTC().Truncate(time.Second)
	firstID, secondID := uuid.New(), uuid.New()
	repo.pursuits[firstID] = models.Pursuit{
		ID: firstID, OwnerIdentity: "alice", Title: "Review legal evidence", Status: StatusActive,
		PriorityScore: 90, RiskLevel: "high", AutonomyLevel: "suggest", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	repo.pursuits[secondID] = models.Pursuit{
		ID: secondID, OwnerIdentity: "alice", Title: "Prepare routine follow-up", Status: StatusActive,
		PriorityScore: 60, RiskLevel: "low", AutonomyLevel: "autonomous_safe", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	}
	firstWorkflowID, secondWorkflowID, foreignWorkflowID := uuid.New(), uuid.New(), uuid.New()
	repo.workflows[firstWorkflowID] = models.WorkflowItem{
		ID: firstWorkflowID, OwnerIdentity: "alice", Title: "Approve legal reply", CurrentState: workflow.StateNeedsApproval,
		RequiresApproval: true, ApprovalStatus: "pending", PriorityScore: 95, UpdatedAt: now.Add(-20 * time.Minute), CreatedAt: now.Add(-time.Hour),
	}
	repo.workflows[secondWorkflowID] = models.WorkflowItem{
		ID: secondWorkflowID, OwnerIdentity: "alice", Title: "Draft follow-up", CurrentState: workflow.StateReady,
		PriorityScore: 55, UpdatedAt: now.Add(-40 * time.Minute), CreatedAt: now.Add(-90 * time.Minute),
	}
	repo.workflows[foreignWorkflowID] = models.WorkflowItem{
		ID: foreignWorkflowID, OwnerIdentity: "bob", Title: "Bob private work", CurrentState: workflow.StateNeedsApproval,
		RequiresApproval: true, ApprovalStatus: "pending", UpdatedAt: now,
	}
	sourceID, rawItemID, extractionID := uuid.New(), uuid.New(), uuid.New()
	memoryID, verificationID := uuid.New(), uuid.New()
	repo.sourceOwners[sourceID] = "alice"
	repo.sourceItems[rawItemID] = models.SourceRawItem{
		ID: rawItemID, SourceID: sourceID, ExternalID: "large-source", ItemType: "document", Title: "Linked source",
		SourceURI: "source://large", Content: strings.Repeat("raw-content-", 1000), Metadata: `{"page":1}`, ContentHash: "raw-hash",
		FetchedAt: now.Add(-25 * time.Minute), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-25 * time.Minute),
	}
	repo.extractions[extractionID] = models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: rawItemID, ContentType: "document", Summary: "Grounded source summary",
		Text: strings.Repeat("extracted-content-", 1000), Entities: strings.Repeat("entity,", 1000), SourceURI: "source://large", SourceLabel: "Linked source",
		CreatedAt: now.Add(-24 * time.Minute), UpdatedAt: now.Add(-24 * time.Minute),
	}
	repo.memories[memoryID] = models.ContextMemory{
		ID: memoryID, OwnerIdentity: "alice", Kind: "project", Content: strings.Repeat("memory-content-", 1000), Summary: "Stable memory summary",
		SourceURI: "memory://large", CreatedAt: now.Add(-50 * time.Minute), UpdatedAt: now.Add(-23 * time.Minute),
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID: uuid.New(), WorkflowID: firstWorkflowID, ClaimText: strings.Repeat("workflow-claim-", 1000), Status: "source_supported", CreatedAt: now.Add(-22 * time.Minute),
	})
	repo.verificationRuns[verificationID] = models.VerificationRun{
		ID: verificationID, OwnerIdentity: "alice", Mode: "grounded", Question: "Verify linked source", Answer: "Supported", Status: "source_supported",
		CreatedAt: now.Add(-21 * time.Minute), UpdatedAt: now.Add(-21 * time.Minute),
	}
	claimID, verificationEvidenceID := uuid.New(), uuid.New()
	repo.verificationClaims[claimID] = models.VerificationClaim{
		ID: claimID, RunID: verificationID, ClaimText: strings.Repeat("verification-claim-", 1000), Status: "source_supported", CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-20 * time.Minute),
	}
	repo.verificationEvidence[verificationEvidenceID] = models.VerificationEvidence{
		ID: verificationEvidenceID, RunID: verificationID, SourceType: "document", SourceURI: "source://large", Snippet: strings.Repeat("evidence-snippet-", 1000),
		Used: true, CreatedAt: now.Add(-19 * time.Minute), UpdatedAt: now.Add(-19 * time.Minute),
	}
	for _, link := range []models.PursuitLink{
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkWorkflow, LinkID: firstWorkflowID.String(), Relationship: "primary", CreatedAt: now},
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkWorkflow, LinkID: foreignWorkflowID.String(), Relationship: "legacy_import", CreatedAt: now.Add(-time.Minute)},
		{ID: uuid.New(), PursuitID: secondID, LinkType: LinkWorkflow, LinkID: secondWorkflowID.String(), Relationship: "primary", CreatedAt: now},
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkMemory, LinkID: memoryID.String(), Relationship: "context", CreatedAt: now},
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkSourceItem, LinkID: rawItemID.String(), Relationship: "source_record", CreatedAt: now},
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkSourceExtraction, LinkID: extractionID.String(), Relationship: "evidence", CreatedAt: now},
		{ID: uuid.New(), PursuitID: firstID, LinkType: LinkVerification, LinkID: verificationID.String(), Relationship: "verification", CreatedAt: now},
	} {
		repo.links[link.ID] = link
	}
	repo.activity[firstID] = []models.PursuitActivity{{ID: uuid.New(), PursuitID: firstID, EventType: "pursuit.updated", Message: "Evidence arrived", CreatedAt: now.Add(-10 * time.Minute)}}
	repo.activity[secondID] = []models.PursuitActivity{{ID: uuid.New(), PursuitID: secondID, EventType: "pursuit.updated", Message: "Follow-up planned", CreatedAt: now.Add(-30 * time.Minute)}}
	repo.taskAttempts["plan-1"] = models.PursuitTaskAttempt{ID: uuid.New(), PursuitID: firstID, TaskPlanID: "plan-1", OwnerIdentity: "alice", Mode: "plan", Status: "needs_review", UpdatedAt: now.Add(-5 * time.Minute), CreatedAt: now.Add(-15 * time.Minute)}

	established, err := NewService(repo, nil).DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("established dashboard: %v", err)
	}
	bulkRepo := &fakeBulkDashboardRepo{fakeRepo: repo}
	optimized, err := NewService(bulkRepo, nil).DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("bulk dashboard: %v", err)
	}
	establishedJSON, _ := json.Marshal(established)
	optimizedJSON, _ := json.Marshal(optimized)
	if string(establishedJSON) != string(optimizedJSON) {
		t.Fatalf("bulk dashboard changed established semantics\nestablished=%s\noptimized=%s", establishedJSON, optimizedJSON)
	}
	if bulkRepo.bulkCalls != 1 {
		t.Fatalf("bulk projection calls = %d, want 1", bulkRepo.bulkCalls)
	}
	if bulkRepo.evidenceProjectionCalls != 1 {
		t.Fatalf("dashboard evidence projection calls = %d, want 1", bulkRepo.evidenceProjectionCalls)
	}
	if bulkRepo.findByIDCalls != 0 {
		t.Fatalf("bulk dashboard re-fetched %d pursuits", bulkRepo.findByIDCalls)
	}
}

func TestAutoLinkWorkflowRejectsForeignOwnerWorkflow(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	if _, err := service.Create(CreateRequest{
		Title:         "Prepare Vivare evidence bundle",
		Description:   "Collect evidence for the Vivare hearing.",
		OwnerIdentity: "alice",
		ProjectKey:    "vivare",
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{
		ID:            bobWorkflowID,
		OwnerIdentity: "bob",
		Title:         "Bob private evidence workflow",
		ProjectKey:    "vivare",
		CurrentState:  workflow.StateReady,
	}

	_, err := service.AutoLinkWorkflow(AutoLinkWorkflowRequest{
		OwnerIdentity: "alice",
		WorkflowID:    bobWorkflowID,
		Input:         "Prepare Vivare evidence bundle for the hearing.",
		ProjectKey:    "vivare",
		SourceURI:     "local://alice/vivare-intake",
		SourceLabel:   "Alice evidence intake",
	})
	if err == nil {
		t.Fatalf("AutoLinkWorkflow accepted a workflow owned by another user")
	}
}

func TestRouteIntakeCreatesReviewableCandidateWhenNoPursuitMatches(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)

	result, err := service.RouteIntake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare YouTube removal appeal evidence and draft platform support follow-up.",
		ProjectKey:    "youtube-removal",
		SourceType:    "ai_chat",
		SourceID:      "chat-99:action-1",
		SourceURI:     "chat://youtube-removal/99",
		SourceLabel:   "YouTube appeal planning chat",
		Actor:         "memory-engine",
	})
	if err != nil {
		t.Fatalf("RouteIntake returned error: %v", err)
	}
	if !result.CreatedCandidate || result.PursuitID == uuid.Nil || result.Detail == nil {
		t.Fatalf("route result = %#v, want created candidate detail", result)
	}
	if result.Mode != "candidate_created" {
		t.Fatalf("route mode = %q", result.Mode)
	}
	if result.Detail.Pursuit.SourceOfCreation != "ai_chat_pursuit_candidate" {
		t.Fatalf("source marker = %q", result.Detail.Pursuit.SourceOfCreation)
	}
	if len(result.Detail.DecisionQueue) == 0 || result.Detail.Summary.NeedsRobert == 0 {
		t.Fatalf("candidate did not surface Robert review: decisions=%#v summary=%#v", result.Detail.DecisionQueue, result.Detail.Summary)
	}
	if workflowService.calls != 0 || len(repo.workflows) != 0 {
		t.Fatalf("unmatched candidate created workflow work: calls=%d workflows=%#v", workflowService.calls, repo.workflows)
	}
	links, err := repo.FindLinks(result.PursuitID)
	if err != nil {
		t.Fatalf("FindLinks returned error: %v", err)
	}
	if !pursuitLinkExists(links, "ai_chat", "chat-99:action-1", "intake_origin") {
		t.Fatalf("candidate did not retain source provenance: %#v", links)
	}
}

func TestCandidateFirstIntakeCreatesWorkflowOnlyAfterExplicitAcceptance(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)

	mandateID := uuid.NewString()
	routed, err := service.RouteIntake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Collect the evidence and prepare the formal response for the new legal matter.",
		ProjectKey:    "legal",
		MandateID:     mandateID,
		SourceType:    "email",
		SourceID:      "new-legal-matter",
		SourceURI:     "email://legal/new-matter",
		SourceLabel:   "New legal matter",
		Actor:         "source-worker",
	})
	if err != nil {
		t.Fatalf("RouteIntake: %v", err)
	}
	if !routed.CreatedCandidate || workflowService.calls != 0 {
		t.Fatalf("candidate-first intake = %#v calls=%d", routed, workflowService.calls)
	}
	if routed.Detail == nil || routed.Detail.Pursuit.MandateID == nil || routed.Detail.Pursuit.MandateID.String() != mandateID {
		t.Fatalf("candidate standing mandate = %#v, want %s", routed.Detail, mandateID)
	}

	detail, err := service.AcceptCandidateForOwner("alice", routed.PursuitID, PlanRequest{Actor: "Robert"})
	if err != nil {
		t.Fatalf("AcceptCandidateForOwner: %v", err)
	}
	if workflowService.calls != 1 || len(detail.Workflows) != 1 || isPursuitCandidate(detail.Pursuit) {
		t.Fatalf("accepted candidate did not create exactly one governed workflow: calls=%d detail=%#v", workflowService.calls, detail)
	}
	if workflowService.received.MandateID != mandateID {
		t.Fatalf("accepted candidate workflow mandate = %q, want %q", workflowService.received.MandateID, mandateID)
	}
}

func TestCandidateIntakeRejectsMalformedAndConflictingMandateReferences(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)

	if result, err := service.RouteIntake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare a governed project recovery plan.",
		MandateID:     "invalid",
	}); err == nil || !strings.Contains(err.Error(), "standing mandate id must be a UUID") || result != nil {
		t.Fatalf("malformed mandate result=%#v error=%v", result, err)
	}

	request := IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare a governed project recovery plan.",
		ProjectKey:    "018-hai",
		SourceType:    LinkAssistantCommand,
		SourceID:      "assistant-mandate-conflict",
		SourceURI:     "assistant://command/mandate-conflict",
		MandateID:     uuid.NewString(),
	}
	first, err := service.RouteIntake(request)
	if err != nil || first == nil || !first.CreatedCandidate {
		t.Fatalf("create candidate: result=%#v error=%v", first, err)
	}
	request.MandateID = uuid.NewString()
	if second, err := service.RouteIntake(request); err == nil || !strings.Contains(err.Error(), "different standing mandate") || second != nil {
		t.Fatalf("conflicting mandate result=%#v error=%v", second, err)
	}
}

func TestRouteWorkflowIntakeDefersUnmatchedCandidateBeforeWorkflowCreation(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	request := workflow.IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare the evidence bundle for the government hearing.",
		ProjectKey:    "vivare",
		SourceType:    "workflow_api",
		SourceID:      "workflow-api-vivare-01",
		SourceURI:     "workflow-api://intake/workflow-api-vivare-01",
		SourceLabel:   "Direct workflow API intake",
		Trigger:       "workflow_api_intake",
		Actor:         "verified-operator",
	}

	record, err := service.RouteWorkflowIntake(request)
	routed, pending := IsCandidatePending(err)
	if !pending {
		t.Fatalf("RouteWorkflowIntake error = %v, want candidate pending", err)
	}
	if record != nil || routed == nil || !routed.CreatedCandidate || routed.PursuitID == uuid.Nil {
		t.Fatalf("candidate pending result = record:%#v routed:%#v", record, routed)
	}
	if workflowService.calls != 0 || len(repo.workflows) != 0 {
		t.Fatalf("unmatched workflow endpoint intake created work: %#v", workflowService)
	}
	matches, err := service.Match(MatchRequest{SourceType: request.SourceType, SourceID: request.SourceID})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matches) != 1 || matches[0].Pursuit.ID == uuid.Nil {
		t.Fatalf("routed workflow did not create a traceable pursuit match: %#v", matches)
	}
}

func TestRouteIntakeReusesPursuitLinkedByAssistantCommandIdentity(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	request := IntakeRequest{
		Input:       "Prepare a local runtime recovery workflow safely.",
		ProjectKey:  "018-HAI",
		SourceType:  LinkAssistantCommand,
		SourceID:    "assistant-7bc9d40f",
		SourceURI:   "assistant://command/assistant-7bc9d40f",
		SourceLabel: "HAI chat command",
		Actor:       "operator",
	}

	first, err := service.RouteIntake(request)
	if err != nil {
		t.Fatalf("first RouteIntake returned error: %v", err)
	}
	if !first.CreatedCandidate || first.PursuitID == uuid.Nil {
		t.Fatalf("first route result = %#v, want candidate", first)
	}

	second, err := service.RouteIntake(request)
	if err != nil {
		t.Fatalf("second RouteIntake returned error: %v", err)
	}
	if !second.Matched || second.CreatedCandidate || second.PursuitID != first.PursuitID {
		t.Fatalf("second route result = %#v, want exact existing pursuit match", second)
	}
	if second.Mode != "matched_candidate" || workflowService.calls != 0 {
		t.Fatalf("repeated candidate intake mode/workflows = %s/%d, want matched_candidate/0", second.Mode, workflowService.calls)
	}
	links, err := repo.FindLinks(first.PursuitID)
	if err != nil {
		t.Fatalf("FindLinks returned error: %v", err)
	}
	if !pursuitLinkExists(links, LinkAssistantCommand, request.SourceID, "command_origin") {
		t.Fatalf("assistant command source identity was not persisted: %#v", links)
	}
}

func pursuitLinkExists(links []models.PursuitLink, linkType, linkID, relationship string) bool {
	for _, link := range links {
		if link.LinkType == linkType && link.LinkID == linkID && link.Relationship == relationship {
			return true
		}
	}
	return false
}

func TestPlanCreatesFirstWorkflowFromPursuitContext(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{
		Title:                "Insurance claim evidence bundle",
		OwnerIdentity:        "alice",
		ProjectKey:           "asr",
		Description:          "Collect evidence for insurer before any external response.",
		DesiredOutcome:       "Verified evidence bundle and approved reply draft.",
		CompletionDefinition: "Workflow exists, checklist is clear, and risky external sending remains approval-gated.",
		RiskLevel:            "high",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	detail, err := service.Plan(created.ID, PlanRequest{Actor: "robert"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if workflowService.received.SourceType != LinkPursuit || workflowService.received.SourceID != created.ID.String() {
		t.Fatalf("workflow source = %s/%s, want pursuit/%s", workflowService.received.SourceType, workflowService.received.SourceID, created.ID)
	}
	if workflowService.received.Trigger != "pursuit_planning" || !workflowService.received.RequiresReview {
		t.Fatalf("workflow trigger/review = %s/%v", workflowService.received.Trigger, workflowService.received.RequiresReview)
	}
	if workflowService.received.OwnerIdentity != "alice" {
		t.Fatalf("planned workflow owner = %q, want persisted pursuit owner alice", workflowService.received.OwnerIdentity)
	}
	if !strings.Contains(workflowService.received.Input, "Verified evidence bundle") || !strings.Contains(workflowService.received.Input, "approval-gated") {
		t.Fatalf("workflow input missing pursuit context: %s", workflowService.received.Input)
	}
	if len(detail.Workflows) != 1 || detail.Summary.PlanningNeeded {
		t.Fatalf("detail workflows/planning = %d/%v", len(detail.Workflows), detail.Summary.PlanningNeeded)
	}
	links, _ := repo.FindLinks(created.ID)
	foundWorkflowLink := false
	for _, link := range links {
		if link.LinkType == LinkWorkflow && link.Relationship == "first_workflow_plan" {
			foundWorkflowLink = true
		}
	}
	if !foundWorkflowLink {
		t.Fatalf("first workflow plan link missing: %#v", links)
	}
	activity, _ := service.Activity(created.ID)
	foundPlanned := false
	for _, item := range activity {
		if item.EventType == "pursuit.planned" {
			foundPlanned = true
			break
		}
	}
	if !foundPlanned {
		t.Fatalf("pursuit planned activity missing: %#v", activity)
	}
}

func TestRouteAmbientOpportunityCreatesCandidateBeforeWorkflow(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	opportunityID := uuid.New()
	repo.ambientOpportunities[opportunityID] = models.AmbientOpportunity{ID: opportunityID, OwnerIdentity: "alice"}

	result, err := service.RouteAmbientOpportunity(AmbientOpportunityRouteRequest{
		OwnerIdentity: "alice",
		OpportunityID: opportunityID,
		Title:         "Review an unresolved supplier dispute",
		Rationale:     "A source-backed issue needs a deliberate objective before work begins.",
		NextAction:    "Review the evidence and decide whether this should become active work.",
		SourceURI:     "ambient://opportunities/" + opportunityID.String(),
		Actor:         "alice",
	})
	if err != nil {
		t.Fatalf("RouteAmbientOpportunity returned error: %v", err)
	}
	if !result.CreatedCandidate || result.PursuitID == uuid.Nil || result.WorkflowID != uuid.Nil {
		t.Fatalf("ambient route result = %#v, want candidate without workflow", result)
	}
	if workflowService.calls != 0 || len(repo.workflows) != 0 {
		t.Fatalf("unmatched ambient opportunity created workflow work: calls=%d workflows=%#v", workflowService.calls, repo.workflows)
	}
	candidate, err := repo.FindByID(result.PursuitID)
	if err != nil || !isPursuitCandidate(*candidate) {
		t.Fatalf("ambient candidate = %#v err=%v", candidate, err)
	}
	links, err := repo.FindLinks(result.PursuitID)
	if err != nil || len(links) != 1 || links[0].LinkType != LinkAmbientOpportunity || links[0].LinkID != opportunityID.String() {
		t.Fatalf("ambient candidate provenance links = %#v err=%v", links, err)
	}
}

func TestRouteAmbientOpportunityCreatesWorkflowForActiveMatch(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	active, err := service.Create(CreateRequest{Title: "Supplier dispute", OwnerIdentity: "alice", ProjectKey: "legal"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	opportunityID := uuid.New()
	repo.ambientOpportunities[opportunityID] = models.AmbientOpportunity{ID: opportunityID, OwnerIdentity: "alice"}

	result, err := service.RouteAmbientOpportunity(AmbientOpportunityRouteRequest{
		OwnerIdentity:  "alice",
		OpportunityID:  opportunityID,
		Title:          "Prepare supplier dispute evidence",
		NextAction:     "Prepare the evidence bundle for review.",
		ProjectKey:     "legal",
		SourceURI:      "ambient://opportunities/" + opportunityID.String(),
		RequiresReview: true,
		ReviewReason:   "evidence needs review before action",
		Actor:          "alice",
	})
	if err != nil {
		t.Fatalf("RouteAmbientOpportunity returned error: %v", err)
	}
	if result.PursuitID != active.ID || result.WorkflowID == uuid.Nil || result.CreatedCandidate {
		t.Fatalf("ambient route result = %#v, want active pursuit workflow", result)
	}
	if workflowService.calls != 1 || !workflowService.received.RequiresReview || workflowService.received.SourceType != LinkAmbientOpportunity || workflowService.received.SourceID != opportunityID.String() {
		t.Fatalf("ambient workflow intake = %#v calls=%d", workflowService.received, workflowService.calls)
	}
	links, err := repo.FindLinks(active.ID)
	if err != nil || len(links) != 2 {
		t.Fatalf("active ambient links = %#v err=%v", links, err)
	}
}

func TestDetailSurfacesBlockersAndCompletionCandidate(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish HAI dashboard", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	blockedID := uuid.New()
	completedID := uuid.New()
	repo.workflows[blockedID] = models.WorkflowItem{
		ID:            blockedID,
		Title:         "Connect real source",
		ProjectKey:    "018-HAI",
		CurrentState:  workflow.StateBlocked,
		BlockedReason: "waiting for credentials",
		RiskLevel:     "medium",
	}
	repo.workflows[completedID] = models.WorkflowItem{
		ID:           completedID,
		Title:        "Create dashboard shell",
		ProjectKey:   "018-HAI",
		CurrentState: workflow.StateCompleted,
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: blockedID.String()})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: completedID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Blockers) != 1 || !strings.Contains(detail.Blockers[0].Reason, "credentials") {
		t.Fatalf("blockers = %#v", detail.Blockers)
	}
	if detail.Summary.CompletionCandidate {
		t.Fatalf("blocked pursuit cannot be a completion candidate")
	}
}

func TestDetailSurfacesFailedQualityGateAsPursuitBlocker(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Production-ready dashboard", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Validate dashboard build",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.qualityGates = append(repo.qualityGates, models.WorkflowQualityGate{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		Gate:       "tests or build evidence",
		Status:     "failed",
		Reason:     "the production build evidence is missing",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.QualityGates) != 1 || detail.Summary.QualityGatesNeedingReview != 1 {
		t.Fatalf("quality gate summary = gates=%#v summary=%#v", detail.QualityGates, detail.Summary)
	}
	if len(detail.Blockers) != 1 || !strings.Contains(detail.Blockers[0].Label, "tests or build evidence") {
		t.Fatalf("quality gate blocker = %#v", detail.Blockers)
	}
	if len(detail.ActionQueues.NeedsRobert) != 1 || !strings.Contains(detail.ActionQueues.NeedsRobert[0].Label, "production build evidence") {
		t.Fatalf("quality gate action queue = %#v", detail.ActionQueues)
	}
	if detail.Summary.CompletionCandidate {
		t.Fatalf("failed quality gate must prevent a completion candidate: %#v", detail.Summary)
	}
}

func TestDetailSurfacesCompletionReviewDecisionForVerifiedWorkflows(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Close verified OpenClaw recovery", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Verify OpenClaw recovery",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if !detail.Summary.CompletionCandidate {
		t.Fatalf("summary = %#v, want completion candidate", detail.Summary)
	}
	if detail.Summary.LinkedEvidence != 1 {
		t.Fatalf("linked evidence = %d, want verified workflow counted", detail.Summary.LinkedEvidence)
	}
	foundDecision := false
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "pursuit_completion_review" {
			foundDecision = true
			if decision.YesLabel != "Mark complete" || decision.NoLabel != "Keep active" || !decision.RequiresApproval {
				t.Fatalf("completion decision invalid: %#v", decision)
			}
		}
	}
	if !foundDecision {
		t.Fatalf("completion review decision missing: %#v", detail.DecisionQueue)
	}
	foundAction := false
	for _, action := range detail.NextActions {
		if action.YesLabel == "Mark complete" {
			foundAction = true
		}
	}
	if !foundAction || len(detail.ActionQueues.NeedsRobert) == 0 {
		t.Fatalf("completion review action missing: actions=%#v queues=%#v", detail.NextActions, detail.ActionQueues)
	}
}

func TestDetailCompletionReviewIsNotHiddenByRecordedWorkflowDecision(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Close verified decision trail", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Verify the decision trail",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.decisions = append(repo.decisions, models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   workflowID,
		DecisionType: "approval_gate",
		Decision:     "approved",
		Reason:       "Robert approved the governed workflow.",
		Actor:        "Robert",
		CreatedAt:    time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if !detail.Summary.CompletionCandidate {
		t.Fatalf("recorded decision hid completion candidate: %#v", detail.Summary)
	}
	foundRecorded := false
	foundCompletionReview := false
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "approval_gate" && decision.Status == "recorded" {
			foundRecorded = true
		}
		if decision.DecisionType == "pursuit_completion_review" && decision.Status == "pending" {
			foundCompletionReview = true
		}
	}
	if !foundRecorded || !foundCompletionReview {
		t.Fatalf("decision queue = %#v, want recorded history and pending completion review", detail.DecisionQueue)
	}
}

func TestResolveCompletionReviewDecisionMarksPursuitComplete(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Close verified evidence pursuit", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Verify evidence bundle",
		ProjectKey:         "vivare",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		ClaimText:  "Evidence bundle checked against source records.",
		SourceURI:  "local://evidence/vivare-bundle",
		Status:     "verified",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.ResolveDecision(created.ID, DecisionResolutionRequest{
		DecisionID:    completionReviewDecisionID(created.ID),
		DecisionType:  "pursuit_completion_review",
		Approved:      true,
		Note:          "Evidence is verified; close the pursuit.",
		EvidenceURI:   "local://evidence/vivare-bundle",
		EvidenceLabel: "Vivare verified evidence bundle",
		Actor:         "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveDecision returned error: %v", err)
	}
	if detail.Pursuit.Status != StatusCompleted || detail.Pursuit.CompletionState != CompletionVerified {
		t.Fatalf("pursuit completion = %s/%s, want completed/verified", detail.Pursuit.Status, detail.Pursuit.CompletionState)
	}
	if detail.Summary.CompletionCandidate || detail.Summary.NeedsRobert != 0 {
		t.Fatalf("completed pursuit still looks actionable: %#v", detail.Summary)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "pursuit_completion_review" {
			t.Fatalf("completed pursuit still has completion review decision: %#v", detail.DecisionQueue)
		}
	}
	if !timelineContains(detail.Timeline, "pursuit_activity", "Evidence is verified") {
		t.Fatalf("timeline = %#v, want completion decision note", detail.Timeline)
	}
	if !timelineContains(detail.Timeline, "pursuit_activity", "Pursuit marked completed") {
		t.Fatalf("timeline = %#v, want completed activity", detail.Timeline)
	}
}

func TestResolveCompletionReviewDecisionRejectsUnverifiedRequest(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Do not close unsupported pursuit", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	_, err = service.ResolveDecision(created.ID, DecisionResolutionRequest{
		DecisionID:   completionReviewDecisionID(created.ID),
		DecisionType: "pursuit_completion_review",
		Approved:     true,
		Actor:        "Robert",
	})
	if err == nil || !strings.Contains(err.Error(), "requires verified evidence") {
		t.Fatalf("ResolveDecision error = %v, want verified evidence guard", err)
	}
	unchanged, _ := repo.FindByID(created.ID)
	if unchanged.Status == StatusCompleted || unchanged.CompletionState == CompletionVerified {
		t.Fatalf("pursuit completion changed despite missing evidence: %#v", unchanged)
	}
	activity, _ := repo.FindActivities(created.ID, 10)
	for _, item := range activity {
		if item.EventType == "pursuit.decision_resolved" || item.EventType == "pursuit.completed" {
			t.Fatalf("unsafe completion decision was audited as resolved: %#v", activity)
		}
	}
}

func TestCandidateCannotCompleteUntilAcceptedAndPlanned(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	candidate, err := service.Create(CreateRequest{
		Title:            "Candidate from a chat import",
		OwnerIdentity:    "alice",
		SourceOfCreation: "ai_chat_pursuit_candidate",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		OwnerIdentity:      "alice",
		Title:              "Completed imported workflow",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		ClaimText:  "Imported work has verified evidence.",
		SourceURI:  "local://evidence/imported-work",
		Status:     "verified",
	})
	if _, err := service.Link(candidate.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkWorkflow, LinkID: workflowID.String(), Relationship: "candidate_operational_work"}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	detail, err := service.DetailForOwner("alice", candidate.ID)
	if err != nil {
		t.Fatalf("DetailForOwner returned error: %v", err)
	}
	if detail.Summary.CompletionCandidate {
		t.Fatalf("unaccepted candidate must not be a completion candidate: %#v", detail.Summary)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "pursuit_completion_review" {
			t.Fatalf("unaccepted candidate exposed completion decision: %#v", detail.DecisionQueue)
		}
	}
	if _, err := service.UpdateForOwner("alice", candidate.ID, UpdateRequest{Status: StatusCompleted, Actor: "Robert"}); err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("UpdateForOwner error = %v, want candidate acceptance guard", err)
	}
	if _, err := service.ResolveDecisionForOwner("alice", candidate.ID, DecisionResolutionRequest{
		DecisionID:   completionReviewDecisionID(candidate.ID),
		DecisionType: "pursuit_completion_review",
		Approved:     true,
		Actor:        "Robert",
	}); err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("ResolveDecisionForOwner error = %v, want candidate acceptance guard", err)
	}
	stored, err := repo.FindByID(candidate.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if pursuitClosed(*stored) {
		t.Fatalf("candidate was closed without acceptance: %#v", stored)
	}
}

func TestCandidateRejectsOperationalDecisionResolution(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	candidate, err := service.Create(CreateRequest{
		Title:            "Candidate with runtime evidence",
		OwnerIdentity:    "alice",
		SourceOfCreation: "agent_runtime_pursuit_candidate",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	_, err = service.ResolveDecisionForOwner("alice", candidate.ID, DecisionResolutionRequest{
		DecisionID:   "runtime-attempt:retry",
		DecisionType: "runtime_attempt_review",
		Approved:     true,
		Actor:        "Robert",
	})
	if err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("candidate operational decision resolution = %v, want acceptance guard", err)
	}
	if workflowService.calls != 0 || len(repo.workflows) != 0 {
		t.Fatalf("candidate decision created workflow work: calls=%d workflows=%#v", workflowService.calls, repo.workflows)
	}
}

func TestOwnerScopedCompletionIgnoresForeignLinkedEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice private completion", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{
		ID:                 bobWorkflowID,
		OwnerIdentity:      "bob",
		Title:              "Bob verified private workflow",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	foreignLinkID := uuid.New()
	repo.links[foreignLinkID] = models.PursuitLink{
		ID:           foreignLinkID,
		PursuitID:    alice.ID,
		LinkType:     LinkWorkflow,
		LinkID:       bobWorkflowID.String(),
		Relationship: "legacy_import",
		SourceURI:    "workflow://private/bob-completion",
		CreatedAt:    time.Now().UTC(),
	}

	if _, err := service.UpdateForOwner("alice", alice.ID, UpdateRequest{Status: StatusCompleted, Actor: "Alice"}); err == nil || !strings.Contains(err.Error(), "requires verified evidence") {
		t.Fatalf("UpdateForOwner error = %v, want owner-visible evidence guard", err)
	}
	stored, err := repo.FindByID(alice.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if pursuitClosed(*stored) {
		t.Fatalf("foreign evidence closed Alice pursuit: %#v", stored)
	}
}

func TestDashboardUsesComputedCompletionCandidate(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Verify dashboard completion queue", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Verify completed workflow evidence",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	stored, _ := repo.FindByID(created.ID)
	if stored.CompletionState == CompletionCandidate {
		t.Fatalf("test setup invalid: stored pursuit should not already be a completion candidate")
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["completionCandidates"] != 1 || len(dashboard.CompletionCandidates) != 1 {
		t.Fatalf("dashboard did not use computed completion candidate: counts=%#v candidates=%#v", dashboard.Counts, dashboard.CompletionCandidates)
	}
	if dashboard.CompletionCandidates[0].Pursuit.ID != created.ID || !dashboard.CompletionCandidates[0].CompletionCandidate {
		t.Fatalf("completion candidate metadata missing: %#v", dashboard.CompletionCandidates)
	}
}

func TestDashboardStatusCountsUseComputedBlockers(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	blockedPursuit, err := service.Create(CreateRequest{Title: "Blocked by linked workflow", ProjectKey: "ops"})
	if err != nil {
		t.Fatalf("Create blocked pursuit returned error: %v", err)
	}
	activePursuit, err := service.Create(CreateRequest{Title: "Active clean pursuit", ProjectKey: "ops"})
	if err != nil {
		t.Fatalf("Create active pursuit returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:            workflowID,
		Title:         "Wait for source credentials",
		ProjectKey:    "ops",
		CurrentState:  workflow.StateBlocked,
		BlockedReason: "missing source credentials",
	}
	_, _ = service.Link(blockedPursuit.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	stored, _ := repo.FindByID(blockedPursuit.ID)
	if stored.Status != StatusActive {
		t.Fatalf("test setup invalid: stored pursuit status = %q, want active", stored.Status)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["blocked"] != 1 || dashboard.Counts["active"] != 1 {
		t.Fatalf("dashboard counts = %#v, want computed blocked=1 and active=1", dashboard.Counts)
	}
	if len(dashboard.Blocked) != 1 || dashboard.Blocked[0].Pursuit.ID != blockedPursuit.ID {
		t.Fatalf("blocked queue = %#v, want blocked pursuit", dashboard.Blocked)
	}
	if activePursuit.ID == uuid.Nil {
		t.Fatalf("active pursuit was not created")
	}
}

func TestDashboardExcludesClosedPursuitsFromOperationalQueues(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	closed, err := service.Create(CreateRequest{Title: "Completed insurance evidence pursuit", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create closed pursuit returned error: %v", err)
	}
	active, err := service.Create(CreateRequest{Title: "Active automation pursuit", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create active pursuit returned error: %v", err)
	}
	stored, _ := repo.FindByID(closed.ID)
	stored.Status = StatusCompleted
	stored.CompletionState = CompletionVerified
	if _, err := repo.Update(stored); err != nil {
		t.Fatalf("mark pursuit complete: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["completed"] != 1 || dashboard.Counts["active"] != 1 {
		t.Fatalf("dashboard counts = %#v, want completed=1 active=1", dashboard.Counts)
	}
	for _, queue := range [][]PursuitListItem{
		dashboard.NeedsRobert,
		dashboard.VAReady,
		dashboard.SystemReady,
		dashboard.Blocked,
		dashboard.Stale,
		dashboard.ReviewDue,
		dashboard.PlanningNeeded,
		dashboard.HighRisk,
		dashboard.CompletionCandidates,
		dashboard.RecentlyChanged,
	} {
		for _, item := range queue {
			if item.Pursuit.ID == closed.ID {
				t.Fatalf("closed pursuit leaked into operational queue: %#v", item)
			}
		}
	}
	if active.ID == uuid.Nil {
		t.Fatalf("active pursuit was not created")
	}
}

func TestClosedPursuitRejectsOperationalMutationAndSummaryRefresh(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Completed legal evidence pursuit", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	stored, _ := repo.FindByID(created.ID)
	stored.Status = StatusCompleted
	stored.CompletionState = CompletionVerified
	if _, err := repo.Update(stored); err != nil {
		t.Fatalf("mark pursuit complete: %v", err)
	}

	operations := []struct {
		name string
		run  func() error
	}{
		{
			name: "intake",
			run: func() error {
				_, err := service.Intake(created.ID, IntakeRequest{Input: "Draft another lawyer response"})
				return err
			},
		},
		{
			name: "plan",
			run: func() error {
				_, err := service.Plan(created.ID, PlanRequest{})
				return err
			},
		},
		{
			name: "decision",
			run: func() error {
				_, err := service.ResolveDecision(created.ID, DecisionResolutionRequest{DecisionID: "workflow:late:approval", Approved: true})
				return err
			},
		},
	}
	for _, operation := range operations {
		if err := operation.run(); err == nil || !strings.Contains(err.Error(), "closed pursuit") {
			t.Fatalf("%s error = %v, want closed pursuit rejection", operation.name, err)
		}
	}
	if workflowService.calls != 0 {
		t.Fatalf("closed pursuit created %d workflow(s)", workflowService.calls)
	}

	refreshed, err := service.RefreshSummary(created.ID, "system")
	if err != nil {
		t.Fatalf("RefreshSummary returned error: %v", err)
	}
	if refreshed.Pursuit.Status != StatusCompleted || refreshed.Pursuit.CompletionState != CompletionVerified {
		t.Fatalf("closed pursuit was reactivated by refresh: %#v", refreshed.Pursuit)
	}
}

func TestReopenRequiresExplicitTransitionAndRestoresGovernedIntake(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Archived automation recovery", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	stored, _ := repo.FindByID(created.ID)
	stored.Status = StatusCompleted
	stored.CompletionState = CompletionVerified
	stored.Archived = true
	if _, err := repo.Update(stored); err != nil {
		t.Fatalf("close pursuit: %v", err)
	}

	if _, err := service.Update(created.ID, UpdateRequest{Status: StatusActive, Actor: "operator"}); err == nil || !strings.Contains(err.Error(), "explicit reopen") {
		t.Fatalf("generic reactivation error = %v, want explicit reopen rejection", err)
	}
	if _, err := service.Archive(created.ID, false, "Robert"); err != nil {
		t.Fatalf("Archive restore should use reopen transition: %v", err)
	}
	reopened, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("Find reopened pursuit: %v", err)
	}
	if reopened.Archived || reopened.Status != StatusActive || reopened.CompletionState != CompletionOpen {
		t.Fatalf("reopened pursuit = %#v", reopened)
	}
	activity, _ := repo.FindActivities(created.ID, 20)
	if !activityContains(activity, "pursuit.reopened") {
		t.Fatalf("reopen was not audited: %#v", activity)
	}
	if _, err := service.Intake(created.ID, IntakeRequest{Input: "Create a governed recovery workflow"}); err != nil {
		t.Fatalf("reopened pursuit intake returned error: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("reopened pursuit created %d workflows, want 1", workflowService.calls)
	}
}

func activityContains(activity []models.PursuitActivity, eventType string) bool {
	for _, item := range activity {
		if item.EventType == eventType {
			return true
		}
	}
	return false
}

func TestDetailSeparatesRobertVAAndSystemActionQueues(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Client delivery follow-up", ProjectKey: "client", Description: "Client deadline needs a quote reply"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	approvalID := uuid.New()
	loopID := uuid.New()
	followUp := time.Now().Add(-2 * time.Hour).UTC()
	repo.workflows[approvalID] = models.WorkflowItem{
		ID:               approvalID,
		Title:            "Send client quote reply",
		ProjectKey:       "client",
		CurrentState:     workflow.StateNeedsApproval,
		RequiresApproval: true,
		ApprovalStatus:   "pending",
		ApprovalReason:   "external client communication requires Robert approval",
		RiskLevel:        "medium",
		NextAction:       "Approve the prepared client reply",
	}
	repo.workflows[loopID] = models.WorkflowItem{
		ID:           loopID,
		Title:        "Chase missing client input",
		ProjectKey:   "client",
		CurrentState: workflow.StateWaitingInput,
		RiskLevel:    "medium",
	}
	repo.openLoops = append(repo.openLoops, models.WorkflowOpenLoop{
		ID:               uuid.New(),
		WorkflowID:       loopID,
		ResponsibleParty: "VA",
		WaitingFor:       "client address confirmation",
		NextAction:       "Prepare a polite follow-up for the missing address",
		FollowUpAt:       &followUp,
		Status:           "follow_up_due",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: approvalID.String()})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: loopID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.ActionQueues.NeedsRobert) != 1 || !strings.Contains(detail.ActionQueues.NeedsRobert[0].Label, "Approve") {
		t.Fatalf("Robert queue = %#v", detail.ActionQueues.NeedsRobert)
	}
	if len(detail.ActionQueues.VAReady) != 1 || !strings.Contains(detail.ActionQueues.VAReady[0].Label, "follow-up") {
		t.Fatalf("VA queue = %#v", detail.ActionQueues.VAReady)
	}
	if len(detail.ActionQueues.Waiting) != 0 {
		t.Fatalf("waiting queue duplicated actionable open loop: %#v", detail.ActionQueues.Waiting)
	}
	if detail.Summary.RobertActions != 1 || detail.Summary.VAReadyActions != 1 {
		t.Fatalf("summary action counts = %#v", detail.Summary)
	}

	emptyLowRisk, err := service.Create(CreateRequest{Title: "Organize local notes", ProjectKey: "notes"})
	if err != nil {
		t.Fatalf("Create empty pursuit returned error: %v", err)
	}
	emptyDetail, err := service.Detail(emptyLowRisk.ID)
	if err != nil {
		t.Fatalf("Detail empty pursuit returned error: %v", err)
	}
	if len(emptyDetail.ActionQueues.SystemReady) != 1 || emptyDetail.Summary.SystemReadyActions != 1 {
		t.Fatalf("system queue = %#v summary=%#v", emptyDetail.ActionQueues.SystemReady, emptyDetail.Summary)
	}
}

func TestUpdateRejectsVerifiedCompletionWithoutEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish legal evidence bundle", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	_, err = service.Update(created.ID, UpdateRequest{
		Status:          StatusCompleted,
		CompletionState: CompletionVerified,
		Actor:           "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "requires verified evidence") {
		t.Fatalf("Update error = %v, want verified evidence guard", err)
	}
	unchanged, _ := repo.FindByID(created.ID)
	if unchanged.Status == StatusCompleted || unchanged.CompletionState == CompletionVerified {
		t.Fatalf("pursuit completion changed despite missing evidence: %#v", unchanged)
	}
}

func TestUpdateAllowsVerifiedCompletionWithEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish legal evidence bundle", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Prepare evidence bundle",
		ProjectKey:         "vivare",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		ClaimText:  "Evidence bundle was reviewed and completed.",
		SourceURI:  "local://evidence/bundle",
		Status:     "verified",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	updated, err := service.Update(created.ID, UpdateRequest{
		Status: StatusCompleted,
		Actor:  "operator",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Status != StatusCompleted || updated.CompletionState != CompletionVerified {
		t.Fatalf("updated completion = %s/%s, want completed/verified", updated.Status, updated.CompletionState)
	}
	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.CompletionCandidate {
		t.Fatalf("completed pursuit remained a completion candidate: %#v", detail.Summary)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "pursuit_completion_review" {
			t.Fatalf("completed pursuit still has completion review decision: %#v", detail.DecisionQueue)
		}
	}
	for _, action := range detail.NextActions {
		if action.YesLabel == "Mark complete" {
			t.Fatalf("completed pursuit still has close-out action: %#v", detail.NextActions)
		}
	}
	if detail.Summary.NeedsRobert != 0 || len(detail.ActionQueues.NeedsRobert) != 0 {
		t.Fatalf("completed pursuit still needs Robert: summary=%#v queues=%#v", detail.Summary, detail.ActionQueues)
	}
}

func TestUpdateBlocksVerifiedCompletionWithUnresolvedWorkflowBlocker(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish legal evidence bundle with blocker", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	completedID := uuid.New()
	blockedID := uuid.New()
	repo.workflows[completedID] = models.WorkflowItem{
		ID:                 completedID,
		Title:              "Prepare evidence bundle",
		ProjectKey:         "vivare",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.workflows[blockedID] = models.WorkflowItem{
		ID:            blockedID,
		Title:         "Wait for missing lawyer document",
		ProjectKey:    "vivare",
		CurrentState:  workflow.StateBlocked,
		BlockedReason: "waiting for signed lawyer statement",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: completedID,
		ClaimText:  "Evidence bundle was reviewed and completed.",
		SourceURI:  "local://evidence/bundle",
		Status:     "verified",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: completedID.String()})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: blockedID.String()})

	_, err = service.Update(created.ID, UpdateRequest{
		Status: StatusCompleted,
		Actor:  "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "unresolved operational work") || !strings.Contains(err.Error(), "lawyer statement") {
		t.Fatalf("Update error = %v, want unresolved blocker guard", err)
	}
	unchanged, _ := repo.FindByID(created.ID)
	if unchanged.Status == StatusCompleted || unchanged.CompletionState == CompletionVerified {
		t.Fatalf("pursuit completion changed despite unresolved blocker: %#v", unchanged)
	}
}

func TestUpdateBlocksVerifiedCompletionWithOpenProposal(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish proposal-gated automation work", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Choose runtime recovery option",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		ClaimText:  "Runtime recovery option was verified.",
		SourceURI:  "local://evidence/runtime-recovery",
		Status:     "verified",
	})
	repo.proposals = append(repo.proposals, models.WorkflowProposal{
		ID:                uuid.New(),
		WorkflowID:        workflowID,
		Status:            "open",
		RecommendedAction: "Choose whether to retry OpenClaw now or keep it disabled.",
		Options:           "Retry now / keep disabled",
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	_, err = service.Update(created.ID, UpdateRequest{
		Status: StatusCompleted,
		Actor:  "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "unresolved operational work") || !strings.Contains(err.Error(), "retry OpenClaw") {
		t.Fatalf("Update error = %v, want unresolved proposal guard", err)
	}
	unchanged, _ := repo.FindByID(created.ID)
	if unchanged.Status == StatusCompleted || unchanged.CompletionState == CompletionVerified {
		t.Fatalf("pursuit completion changed despite open proposal: %#v", unchanged)
	}
}

func TestUpdateBlocksVerifiedCompletionWithNeedsReviewDecision(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Finish decision-gated automation work", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Verify runtime execution decision",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		VerificationStatus: "verified",
	}
	repo.evidence = append(repo.evidence, models.WorkflowEvidenceClaim{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		ClaimText:  "Runtime execution was verified.",
		SourceURI:  "local://evidence/runtime-execution",
		Status:     "verified",
	})
	repo.decisions = append(repo.decisions, models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   workflowID,
		DecisionType: "worker_execution",
		Decision:     "needs_review",
		Reason:       "runtime output still needs Robert review before closing",
		RuleApplied:  "completion engine",
		Actor:        "workflow-worker",
		CreatedAt:    time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	_, err = service.Update(created.ID, UpdateRequest{
		Status: StatusCompleted,
		Actor:  "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "unresolved operational work") || !strings.Contains(err.Error(), "Robert review") {
		t.Fatalf("Update error = %v, want unresolved decision guard", err)
	}
	unchanged, _ := repo.FindByID(created.ID)
	if unchanged.Status == StatusCompleted || unchanged.CompletionState == CompletionVerified {
		t.Fatalf("pursuit completion changed despite needs-review decision: %#v", unchanged)
	}
}

func TestUpdateAllowsVerifiedCompletionWithLinkedVerification(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Complete automation runtime audit", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkVerification,
		LinkID:       uuid.New().String(),
		Relationship: "completion_evidence",
		SourceURI:    "local://verification/runtime-audit",
		SourceLabel:  "Runtime audit verification",
	})

	updated, err := service.Update(created.ID, UpdateRequest{
		CompletionState: CompletionVerified,
		Actor:           "operator",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.CompletionState != CompletionVerified {
		t.Fatalf("completion state = %s, want verified", updated.CompletionState)
	}
}

func TestUpdateRejectsBareVerificationLinkWithoutEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Complete unsupported verification link", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType: LinkVerification,
		LinkID:   uuid.New().String(),
	})

	_, err = service.Update(created.ID, UpdateRequest{
		CompletionState: CompletionVerified,
		Actor:           "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "requires verified evidence") {
		t.Fatalf("Update error = %v, want verified evidence guard", err)
	}
}

func TestDetailSurfacesLinkedVerificationRunEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Verify OpenClaw completion", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	runID := uuid.New()
	claimID := uuid.New()
	evidenceID := uuid.New()
	repo.verificationRuns[runID] = models.VerificationRun{
		ID:         runID,
		Mode:       "action",
		Question:   "Did OpenClaw complete the approved task?",
		ProjectKey: "018-HAI",
		Status:     "verified",
		Answer:     "The runtime attempt completed under HAI controls.",
	}
	repo.verificationClaims[claimID] = models.VerificationClaim{
		ID:        claimID,
		RunID:     runID,
		ClaimText: "The runtime attempt completed under HAI controls.",
		Status:    "verified",
	}
	repo.verificationEvidence[evidenceID] = models.VerificationEvidence{
		ID:          evidenceID,
		RunID:       runID,
		SourceType:  "automation_launch",
		SourceURI:   "automation-launch://openclaw",
		SourceLabel: "OpenClaw launch event",
		Snippet:     "OpenClaw completed the approved agent task.",
		Used:        true,
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkVerification, LinkID: runID.String(), Relationship: "completion_evidence"})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.VerificationRuns) != 1 || detail.VerificationRuns[0].Status != "verified" {
		t.Fatalf("verification runs = %#v", detail.VerificationRuns)
	}
	if len(detail.VerificationClaims) != 1 || len(detail.VerificationEvidence) != 1 {
		t.Fatalf("verification claims/evidence = %#v / %#v", detail.VerificationClaims, detail.VerificationEvidence)
	}
	if detail.Summary.VerificationRuns != 1 || detail.Summary.LinkedEvidence == 0 {
		t.Fatalf("summary = %#v, want verification evidence counted", detail.Summary)
	}
}

func TestLinkVerificationCreatesAuditableEvidenceLink(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Verify legal evidence", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	verificationID := uuid.New()

	if err := service.LinkVerification(created.ID, verificationID); err != nil {
		t.Fatalf("LinkVerification returned error: %v", err)
	}

	links, err := repo.FindLinks(created.ID)
	if err != nil {
		t.Fatalf("FindLinks returned error: %v", err)
	}
	found := false
	for _, link := range links {
		if link.LinkType == LinkVerification && link.LinkID == verificationID.String() && link.Relationship == "verification_evidence" && link.SourceURI == "verification://"+verificationID.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("verification evidence link missing: %#v", links)
	}
}

func TestOwnerScopedVerificationLinkRejectsAnotherOwner(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	bob, err := service.Create(CreateRequest{Title: "Bob private verification", OwnerIdentity: "bob"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}

	if err := service.LinkVerificationForOwner("alice", bob.ID, uuid.New()); err == nil {
		t.Fatal("Alice could link verification evidence to Bob's pursuit")
	}
	links, err := repo.FindLinks(bob.ID)
	if err != nil {
		t.Fatalf("FindLinks: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("cross-owner verification link persisted: %#v", links)
	}
}

func TestDetailSurfacesTaskRunEvidenceFromLinkedWorkflows(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Advance task runner pursuit", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	lastRun := time.Now().UTC()
	nextRun := lastRun.Add(30 * time.Minute)
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Run task engine workflow",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateReady,
		LastTaskPlanID:     "task-plan-123",
		VerificationStatus: "needs_review",
		RetryCount:         1,
		MaxRetries:         2,
		LastRunAt:          &lastRun,
		NextRunAt:          &nextRun,
		LastWorkerError:    "validation failed after controlled runtime output",
		AutomationID:       uuid.NewString(),
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.TaskRuns) != 1 {
		t.Fatalf("task runs = %#v, want one execution summary", detail.TaskRuns)
	}
	run := detail.TaskRuns[0]
	if run.TaskPlanID != "task-plan-123" || !run.NeedsReview || run.Status != "blocked" {
		t.Fatalf("task run = %#v, want blocked needs-review task evidence", run)
	}
	if detail.Summary.TaskRuns != 1 {
		t.Fatalf("summary task runs = %d, want 1", detail.Summary.TaskRuns)
	}
}

func TestDetailSurfacesDirectPursuitTaskAttempts(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Plan direct evidence review", OwnerIdentity: "alice", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	now := time.Now().UTC()
	completed := now.Add(time.Minute)
	if err := service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:          created.ID,
		TaskPlanID:         "direct-plan-123",
		OwnerIdentity:      "alice",
		RequestSummary:     "Review the evidence before approving the response.",
		ProjectKey:         "018-HAI",
		Mode:               "run",
		Status:             "review_required",
		RiskLevel:          "high",
		VerificationStatus: "needs_review",
		BlockedReason:      "approval required before external action",
		StartedAt:          &now,
		CompletedAt:        &completed,
	}); err != nil {
		t.Fatalf("UpsertTaskAttempt returned error: %v", err)
	}

	detail, err := service.DetailForOwner("alice", created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.TaskAttempts) != 1 {
		t.Fatalf("task attempts = %#v, want one direct task attempt", detail.TaskAttempts)
	}
	attempt := detail.TaskAttempts[0]
	if attempt.TaskPlanID != "direct-plan-123" || attempt.Status != "review_required" || attempt.BlockedReason == "" {
		t.Fatalf("task attempt = %#v", attempt)
	}
	if detail.Summary.TaskRuns != 1 {
		t.Fatalf("summary task runs = %d, want direct attempt included", detail.Summary.TaskRuns)
	}
	foundTimeline := false
	for _, item := range detail.Timeline {
		if item.Kind == "task_attempt" && item.ID == "task-attempt:direct-plan-123" {
			foundTimeline = true
		}
	}
	if !foundTimeline {
		t.Fatalf("direct task attempt missing from timeline: %#v", detail.Timeline)
	}
}

func TestDirectPursuitTaskAttemptRejectsAnotherOwner(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Alice private pursuit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:     created.ID,
		TaskPlanID:    "bob-direct-plan",
		OwnerIdentity: "bob",
		Mode:          "plan",
		Status:        "planned",
	}); err == nil {
		t.Fatal("Bob could persist a task attempt under Alice's pursuit")
	}
	// Simulate an invalid pre-owner-scoping row that may already exist in a
	// local database. Detail must not disclose it to Alice.
	repo.taskAttempts["legacy-bob-direct-plan"] = models.PursuitTaskAttempt{
		PursuitID:      created.ID,
		TaskPlanID:     "legacy-bob-direct-plan",
		OwnerIdentity:  "bob",
		RequestSummary: "Bob private task request",
		Mode:           "run",
		Status:         "review_required",
	}
	detail, err := service.DetailForOwner("alice", created.ID)
	if err != nil {
		t.Fatalf("DetailForOwner: %v", err)
	}
	if len(detail.TaskAttempts) != 0 {
		t.Fatalf("cross-owner task attempt leaked into pursuit detail: %#v", detail.TaskAttempts)
	}
}

func TestDirectPursuitTaskAttemptRejectsUnacceptedCandidate(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	candidate, err := service.Create(CreateRequest{
		Title:            "Review imported legal correspondence",
		OwnerIdentity:    "alice",
		SourceOfCreation: "source_pursuit_candidate",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	err = service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:     candidate.ID,
		TaskPlanID:    "candidate-direct-plan",
		OwnerIdentity: "alice",
		Mode:          "plan",
		Status:        "planned",
	})
	if err == nil || !strings.Contains(err.Error(), "candidate must be accepted") {
		t.Fatalf("unaccepted candidate accepted direct task attempt: %v", err)
	}
	if attempts, findErr := repo.FindTaskAttempts(candidate.ID, 10); findErr != nil || len(attempts) != 0 {
		t.Fatalf("candidate task attempt persisted: attempts=%#v err=%v", attempts, findErr)
	}
}

func TestDirectPursuitTaskAttemptRejectsClosedPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Closed evidence review", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := service.ArchiveForOwner("alice", created.ID, true, "alice"); err != nil {
		t.Fatalf("ArchiveForOwner returned error: %v", err)
	}

	err = service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:     created.ID,
		TaskPlanID:    "closed-direct-plan",
		OwnerIdentity: "alice",
		Mode:          "plan",
		Status:        "planned",
	})
	if err == nil || !strings.Contains(err.Error(), "closed pursuit") {
		t.Fatalf("closed pursuit accepted direct task attempt: %v", err)
	}
	if attempts, findErr := repo.FindTaskAttempts(created.ID, 10); findErr != nil || len(attempts) != 0 {
		t.Fatalf("closed pursuit task attempt persisted: attempts=%#v err=%v", attempts, findErr)
	}
}

func TestDirectPursuitTaskAttemptRedactsPersistedText(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Protect direct task audit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:      created.ID,
		TaskPlanID:     "redacted-direct-plan",
		OwnerIdentity:  "alice",
		RequestSummary: "Run source check with api_key=plain-text-secret",
		BlockedReason:  "provider token=another-plain-secret requires review",
		Mode:           "run",
		Status:         "review_required",
	}); err != nil {
		t.Fatalf("UpsertTaskAttempt returned error: %v", err)
	}
	detail, err := service.DetailForOwner("alice", created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.TaskAttempts) != 1 {
		t.Fatalf("task attempts = %#v", detail.TaskAttempts)
	}
	attempt := detail.TaskAttempts[0]
	for _, value := range []string{attempt.RequestSummary, attempt.BlockedReason} {
		if strings.Contains(value, "plain-text-secret") || strings.Contains(value, "another-plain-secret") {
			t.Fatalf("task attempt leaked secret: %#v", attempt)
		}
	}
}

func TestDirectPursuitTaskAttemptLinksOnlyItsExactRuntimeLaunchEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Retain exact runtime evidence", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	automationID := uuid.New()
	selectedLaunchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents,
		models.AutomationLaunchEvent{ID: selectedLaunchID, AutomationID: automationID, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime", Status: "completed", StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()},
		models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: "alice", RuntimeType: "openclaw", LaunchType: "agent_runtime", Status: "completed", StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()},
	)
	if err := service.UpsertTaskAttempt(models.PursuitTaskAttempt{
		PursuitID:     created.ID,
		TaskPlanID:    "runtime-evidence-direct-plan",
		OwnerIdentity: "alice",
		Mode:          "run",
		Status:        "validated",
		AutomationID:  automationID.String(),
		LaunchEventID: selectedLaunchID.String(),
	}); err != nil {
		t.Fatalf("UpsertTaskAttempt returned error: %v", err)
	}
	detail, err := service.DetailForOwner("alice", created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.RuntimeAttempts) != 1 || detail.RuntimeAttempts[0].ID != selectedLaunchID {
		t.Fatalf("runtime attempts = %#v, want only the selected launch", detail.RuntimeAttempts)
	}
	if len(detail.Automations) != 0 {
		t.Fatalf("direct task attempt must not link a shared automation: %#v", detail.Automations)
	}
	foundLaunchLink := false
	for _, link := range detail.Links {
		if link.LinkType == LinkAgentRuntime && link.LinkID == selectedLaunchID.String() && link.Relationship == "execution_attempt" {
			foundLaunchLink = true
		}
	}
	if !foundLaunchLink {
		t.Fatalf("exact runtime evidence link missing: %#v", detail.Links)
	}
}

func TestOwnerScopedPursuitRejectsAndHidesAnotherOwnersRuntimeEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Alice private runtime review", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}
	bobLaunchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:            bobLaunchID,
		OwnerIdentity: "bob",
		RuntimeType:   "openclaw",
		LaunchType:    "agent_runtime",
		Status:        "completed",
		Output:        "Bob private runtime output",
		StartedAt:     time.Now().UTC(),
		CompletedAt:   time.Now().UTC(),
	})
	if _, err := service.Link(pursuit.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkAgentRuntime, LinkID: bobLaunchID.String(), Relationship: "execution_attempt"}); err == nil {
		t.Fatal("owner could link another user's runtime evidence")
	}

	legacyLinkID := uuid.New()
	repo.links[legacyLinkID] = models.PursuitLink{ID: legacyLinkID, PursuitID: pursuit.ID, LinkType: LinkAgentRuntime, LinkID: bobLaunchID.String(), Relationship: "legacy_evidence"}
	detail, err := service.DetailForOwner("alice", pursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner: %v", err)
	}
	if len(detail.RuntimeAttempts) != 0 || hasPursuitLink(detail.Links, LinkAgentRuntime, bobLaunchID.String()) {
		t.Fatalf("private runtime evidence leaked into owner detail: %#v", detail)
	}
}

func TestDetailSurfacesRobertDecisionQueueFromLinkedWorkflows(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Resolve legal approval pursuit", ProjectKey: "Vivare", RiskLevel: "high"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:               workflowID,
		Title:            "Draft formal reply to lawyer",
		ProjectKey:       "Vivare",
		CurrentState:     workflow.StateNeedsApproval,
		RiskLevel:        "high",
		RequiresApproval: true,
		ApprovalStatus:   "pending",
		ApprovalReason:   "Legal external email must be approved by Robert before sending.",
		NextAction:       "Approve the draft reply or request revision.",
		SourceURI:        "email://lawyer/request",
		SourceLabel:      "Lawyer request email",
	}
	repo.proposals = append(repo.proposals, models.WorkflowProposal{
		ID:                uuid.New(),
		WorkflowID:        workflowID,
		RecommendedAction: "Send the formal reply only after evidence links are checked.",
		Options:           "Approve / request more evidence / reject",
		Status:            "open",
		CreatedAt:         time.Now().UTC(),
	})
	repo.decisions = append(repo.decisions, models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   workflowID,
		DecisionType: "approval_gate",
		Decision:     "required",
		Reason:       "legal workflow must remain draft-only until approved",
		RuleApplied:  "approval rule engine",
		Actor:        "engine",
		CreatedAt:    time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Decisions) != 1 {
		t.Fatalf("decisions = %#v, want linked workflow decision history", detail.Decisions)
	}
	if len(detail.DecisionQueue) != 3 {
		t.Fatalf("decision queue = %#v, want approval, proposal, and audit cards", detail.DecisionQueue)
	}
	first := detail.DecisionQueue[0]
	if first.Status != "pending" || !first.RequiresApproval || first.EvidenceURI != "email://lawyer/request" {
		t.Fatalf("first decision card = %#v, want pending approval with source evidence", first)
	}
	if first.YesConsequence == "" || first.NoConsequence == "" {
		t.Fatalf("first decision card missing consequences: %#v", first)
	}
	if detail.Summary.NeedsRobert != 2 || detail.Summary.DecisionCards != 3 {
		t.Fatalf("summary = %#v, want two pending Robert decisions and three decision cards", detail.Summary)
	}
}

func TestApprovalsReturnsGovernedDecisionOverview(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Approve legal source response", ProjectKey: "Vivare", RiskLevel: "high"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:               workflowID,
		Title:            "Send lawyer evidence bundle",
		ProjectKey:       "Vivare",
		CurrentState:     workflow.StateNeedsApproval,
		RiskLevel:        "high",
		RequiresApproval: true,
		ApprovalStatus:   "pending",
		ApprovalReason:   "Legal correspondence must remain draft-only until Robert approves it.",
		NextAction:       "Review evidence bundle and approve or reject sending.",
		SourceURI:        "email://lawyer/evidence-bundle",
		SourceLabel:      "Lawyer evidence request",
	}
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	overview, err := service.Approvals(created.ID)
	if err != nil {
		t.Fatalf("Approvals returned error: %v", err)
	}
	if overview.Pursuit.ID != created.ID {
		t.Fatalf("overview pursuit id = %s, want %s", overview.Pursuit.ID, created.ID)
	}
	if len(overview.ApprovalItems) != 1 {
		t.Fatalf("approval items = %#v, want one raw approval workflow", overview.ApprovalItems)
	}
	if len(overview.DecisionQueue) != 1 || !overview.DecisionQueue[0].RequiresApproval {
		t.Fatalf("decision queue = %#v, want one Robert approval card", overview.DecisionQueue)
	}
	if len(overview.Actions) != 1 || !overview.Actions[0].RequiresApproval {
		t.Fatalf("approval actions = %#v, want one Robert approval action", overview.Actions)
	}
	if overview.Counts["approvalItems"] != 1 || overview.Counts["pendingDecisions"] != 1 || overview.Counts["needsRobert"] == 0 {
		t.Fatalf("counts = %#v, want approval, pending decision, and needs-Robert counts", overview.Counts)
	}
}

func TestDetailBuildsTimelineFromLinkedWorkflowAuditRecords(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Recover stuck workflow pursuit", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	base := time.Now().UTC().Add(time.Hour)
	lastRun := base.Add(2 * time.Minute)
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:             workflowID,
		Title:          "Recover controlled worker state",
		ProjectKey:     "018-HAI",
		CurrentState:   workflow.StateBlocked,
		RiskLevel:      "medium",
		LastRunAt:      &lastRun,
		LastTaskPlanID: "plan-recovery-1",
	}
	repo.transitions = append(repo.transitions, models.WorkflowTransition{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		FromState:  workflow.StateInProgress,
		ToState:    workflow.StateBlocked,
		Trigger:    "worker recovery",
		Actor:      "workflow-worker",
		Reason:     "runtime result needs review",
		CreatedAt:  base,
	})
	repo.sourceLinks = append(repo.sourceLinks, models.WorkflowSourceLink{
		ID:           uuid.New(),
		WorkflowID:   workflowID,
		SourceType:   "email",
		SourceURI:    "email://thread/123",
		SourceLabel:  "Source email",
		Relationship: "evidence",
		CreatedAt:    base.Add(time.Minute),
	})
	repo.events = append(repo.events, models.WorkflowEvent{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		EventType:  "worker_execution",
		Message:    "task engine blocked for review",
		ToState:    workflow.StateBlocked,
		Actor:      "workflow-worker",
		CreatedAt:  base.Add(3 * time.Minute),
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Transitions) != 1 || len(detail.SourceLinks) != 1 || len(detail.Events) != 1 {
		t.Fatalf("audit records transitions=%#v sourceLinks=%#v events=%#v", detail.Transitions, detail.SourceLinks, detail.Events)
	}
	if len(detail.Timeline) < 5 {
		t.Fatalf("timeline = %#v, want pursuit, workflow, source, transition, and task records", detail.Timeline)
	}
	if detail.Timeline[0].Kind != "workflow_event" || detail.Timeline[0].WorkflowTitle != "Recover controlled worker state" {
		t.Fatalf("first timeline item = %#v, want newest workflow event with title", detail.Timeline[0])
	}
	if detail.Summary.TimelineItems != len(detail.Timeline) || !strings.Contains(detail.Summary.WhatChanged, "worker_execution") {
		t.Fatalf("summary = %#v, want timeline count and newest change", detail.Summary)
	}
}

func TestDetailSurfacesAutomationAndRuntimeAttempts(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Operate OpenClaw runtime safely", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	automationID := uuid.New()
	workflowID := uuid.New()
	repo.automations[automationID] = models.Automation{
		ID:          automationID,
		Name:        "OpenClaw runtime",
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "healthy",
	}
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:           workflowID,
		Title:        "Run approved OpenClaw task",
		ProjectKey:   "018-HAI",
		CurrentState: workflow.StateCompleted,
		AutomationID: automationID.String(),
	}
	startedAt := time.Now().UTC()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:           uuid.New(),
		AutomationID: automationID,
		RuntimeType:  "openclaw",
		LaunchType:   "agent_runtime",
		Status:       "completed",
		Message:      "OpenClaw completed the approved agent task",
		Output:       "bounded runtime output",
		AuditEvents: []string{
			"launch requested",
			"runtime safety policy evaluated",
			"OpenClaw ecosystem execution completed under explicit controls",
		},
		StartedAt:   startedAt,
		CompletedAt: startedAt.Add(time.Second),
	})
	_, _ = service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String()})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.Automations) != 1 || detail.Automations[0].RuntimeType != "openclaw" {
		t.Fatalf("automations = %#v", detail.Automations)
	}
	if len(detail.RuntimeAttempts) != 1 || detail.RuntimeAttempts[0].Status != "completed" {
		t.Fatalf("runtime attempts = %#v", detail.RuntimeAttempts)
	}
	if detail.Summary.RuntimeAttempts != 1 || detail.Summary.LinkedEvidence == 0 {
		t.Fatalf("summary = %#v, want runtime attempts counted as operational evidence", detail.Summary)
	}
	if detail.OperationalDigest.RuntimeAttempts != 1 || !strings.Contains(detail.OperationalDigest.RuntimeLine, "openclaw") {
		t.Fatalf("operational digest runtime line = %#v", detail.OperationalDigest)
	}
	if detail.OperationalDigest.Evidence == 0 || !strings.Contains(detail.OperationalDigest.EvidenceLine, "evidence") {
		t.Fatalf("operational digest evidence line = %#v", detail.OperationalDigest)
	}
	if !timelineContains(detail.Timeline, "runtime_audit", "runtime safety policy evaluated") {
		t.Fatalf("timeline = %#v, want runtime audit policy evaluation surfaced", detail.Timeline)
	}
	if !timelineContains(detail.Timeline, "runtime_audit", "OpenClaw ecosystem execution completed under explicit controls") {
		t.Fatalf("timeline = %#v, want runtime completion audit surfaced", detail.Timeline)
	}
	if !timelineSourceContains(detail.Timeline, "runtime_attempt", "automation-launch://") {
		t.Fatalf("timeline = %#v, want runtime attempt source URI surfaced", detail.Timeline)
	}
}

func TestAmbientOpportunityLinkCountsAsPursuitEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Restart stale admin pursuit", RiskLevel: "medium"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	opportunityID := uuid.New()
	sourceURI := "ambient://opportunities/" + opportunityID.String()

	if _, err := service.Link(created.ID, LinkRequest{
		LinkType:     LinkAmbientOpportunity,
		LinkID:       opportunityID.String(),
		Relationship: "ambient_proposal_accepted",
		SourceURI:    sourceURI,
		SourceLabel:  "Restart stale pursuit",
		Confidence:   0.82,
		Actor:        "ambient",
	}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.LinkedEvidence != 1 {
		t.Fatalf("linked evidence = %d, want accepted ambient opportunity counted", detail.Summary.LinkedEvidence)
	}
	if detail.OperationalDigest.Evidence != 1 || !strings.Contains(detail.OperationalDigest.EvidenceLine, "1 evidence item") {
		t.Fatalf("operational digest = %#v, want ambient opportunity evidence surfaced", detail.OperationalDigest)
	}
	if !timelineContains(detail.Timeline, "pursuit_activity", "Linked ambient_opportunity") {
		t.Fatalf("timeline = %#v, want ambient opportunity link activity", detail.Timeline)
	}
}

func TestAmbientOpportunityLinksAreOwnerScopedAndProjected(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Advance housing case", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	opportunityID := uuid.New()
	repo.ambientOpportunities[opportunityID] = models.AmbientOpportunity{
		ID:               opportunityID,
		OwnerIdentity:    "alice",
		NeedKey:          "safety",
		Title:            "Review missing evidence",
		Rationale:        "The pursuit has not moved because a source is missing.",
		NextAction:       "Prepare a request for the missing document.",
		PriorityScore:    78,
		Confidence:       82,
		Risk:             65,
		RequiresApproval: true,
		Status:           "accepted",
		EvidenceManifest: "raw manifest must not be returned in pursuit detail",
		LastSeenAt:       time.Now().UTC(),
	}
	if _, err := service.Link(pursuit.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkAmbientOpportunity, LinkID: opportunityID.String(), Relationship: "ambient_proposal_accepted", SourceURI: "ambient://opportunities/" + opportunityID.String()}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	detail, err := service.DetailForOwner("alice", pursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner returned error: %v", err)
	}
	if len(detail.AmbientOpportunities) != 1 || detail.AmbientOpportunities[0].ID != opportunityID || detail.AmbientOpportunities[0].NextAction == "" {
		t.Fatalf("ambient opportunity projection = %#v", detail.AmbientOpportunities)
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if strings.Contains(string(payload), "raw manifest must not be returned") || strings.Contains(string(payload), "evidenceManifest") {
		t.Fatalf("pursuit detail exposed private ambient fields: %s", payload)
	}

	bobOpportunityID := uuid.New()
	repo.ambientOpportunities[bobOpportunityID] = models.AmbientOpportunity{ID: bobOpportunityID, OwnerIdentity: "bob", Title: "Bob private proposal"}
	if _, err := service.Link(pursuit.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkAmbientOpportunity, LinkID: bobOpportunityID.String(), Relationship: "ambient_proposal_accepted"}); err == nil {
		t.Fatal("owner could link another user's ambient opportunity")
	}
}

func TestResolveEvidenceReturnsLinkedRuntimeAttempt(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Inspect linked OpenClaw evidence", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:            launchID,
		RuntimeType:   "openclaw",
		LaunchType:    "agent_runtime_stop",
		RuntimeTaskID: created.ID.String(),
		Status:        "stopped",
		Message:       "runtime stop requested",
		AuditEvents:   []string{"runtime stop requested", "cancelled active runtime context"},
		ExitCode:      0,
		StartedAt:     time.Now().UTC(),
		CompletedAt:   time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    "automation-launch://" + launchID.String(),
		SourceLabel:  "OpenClaw runtime stop",
	})

	resolved, err := service.ResolveEvidence(created.ID, "automation-launch://"+launchID.String())
	if err != nil {
		t.Fatalf("ResolveEvidence returned error: %v", err)
	}
	if resolved.Kind != "runtime_attempt" || resolved.RuntimeAttempt == nil || resolved.RuntimeAttempt.ID != launchID {
		t.Fatalf("resolved evidence = %#v", resolved)
	}
	if resolved.Title != "Runtime stop: openclaw" || resolved.Status != "stopped" || resolved.NeedsReview {
		t.Fatalf("resolved runtime metadata = %#v", resolved)
	}
}

func TestResolveEvidenceRejectsUnlinkedRuntimeAttempt(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Do not leak runtime evidence", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})

	if _, err := service.ResolveEvidence(created.ID, "automation-launch://"+launchID.String()); err == nil {
		t.Fatalf("ResolveEvidence succeeded for unlinked runtime evidence")
	}
}

func TestFailedRuntimeAttemptCreatesPursuitBlockerAndRobertDecision(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Recover OpenClaw runtime", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "agent runtime registry is not configured",
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
			RuntimeID:         "openclaw",
			Intent:            "software engineering and repository workflow",
			ExecutionMode:     "read-only planning plus approved low-risk local actions",
			RiskLevel:         "high",
			RecommendedSkills: []string{"autoreview", "gitcrawl"},
			VisibleTools:      []string{"browser"},
			BlockedSurfaces:   []string{"outbound message sending"},
		},
		ExitCode:    -1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    "automation-launch://" + launchID.String(),
		SourceLabel:  "OpenClaw blocked launch",
	})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.Blocked == 0 || len(detail.Blockers) == 0 {
		t.Fatalf("failed runtime attempt was not surfaced as blocker: summary=%#v blockers=%#v", detail.Summary, detail.Blockers)
	}
	if detail.Summary.NeedsRobert == 0 || len(detail.ActionQueues.NeedsRobert) == 0 {
		t.Fatalf("failed runtime attempt did not require Robert: summary=%#v queues=%#v", detail.Summary, detail.ActionQueues)
	}
	foundDecision := false
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "runtime_attempt_review" {
			foundDecision = true
			if !decision.RequiresApproval || decision.YesLabel != "Create recovery workflow" || decision.NoLabel != "Keep blocked" {
				t.Fatalf("runtime review decision invalid: %#v", decision)
			}
			if decision.EvidenceURI != "automation-launch://"+launchID.String() {
				t.Fatalf("runtime decision evidence = %q", decision.EvidenceURI)
			}
			if decision.RiskLevel != "high" || !strings.Contains(decision.Reason, "skills=autoreview, gitcrawl") || !strings.Contains(decision.Reason, "blocked=outbound message sending") {
				t.Fatalf("runtime route trace missing from decision: %#v", decision)
			}
		}
	}
	if !foundDecision {
		t.Fatalf("runtime review decision missing: %#v", detail.DecisionQueue)
	}
}

func TestResolvedRuntimeAttemptDecisionStaysBlockedWithoutRepromptingRobert(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Keep failed OpenClaw runtime blocked", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "agent runtime registry is not configured",
		ExitCode:    -1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    "automation-launch://" + launchID.String(),
		SourceLabel:  "OpenClaw blocked launch",
	})
	decisionID := "runtime:" + launchID.String() + ":review"

	detail, err := service.ResolveDecision(created.ID, DecisionResolutionRequest{
		DecisionID:    decisionID,
		DecisionType:  "runtime_attempt_review",
		Approved:      false,
		Reason:        "blocked: agent runtime registry is not configured",
		Note:          "Keep blocked until OpenClaw is configured safely.",
		EvidenceURI:   "automation-launch://" + launchID.String(),
		EvidenceLabel: "OpenClaw blocked launch",
		Actor:         "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveDecision returned error: %v", err)
	}
	if detail.Summary.Blocked == 0 || len(detail.Blockers) == 0 {
		t.Fatalf("resolved runtime attempt should remain visible as blocker: summary=%#v blockers=%#v", detail.Summary, detail.Blockers)
	}
	if detail.Summary.NeedsRobert != 0 {
		t.Fatalf("resolved runtime decision should not keep Robert in queue: summary=%#v", detail.Summary)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.ID == decisionID {
			t.Fatalf("resolved runtime decision was regenerated: %#v", detail.DecisionQueue)
		}
	}
	if len(detail.ActionQueues.NeedsRobert) != 0 {
		t.Fatalf("resolved runtime blocker still routed to Robert: %#v", detail.ActionQueues)
	}
}

func TestApprovedRuntimeAttemptDecisionCreatesGovernedRecoveryWorkflow(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Recover OpenClaw runtime safely", OwnerIdentity: "alice", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	sourceURI := "automation-launch://" + launchID.String()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "high-risk surfaces detected",
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
			RuntimeID:           "openclaw",
			Intent:              "software engineering and repository workflow",
			ExecutionMode:       "read_only",
			RiskLevel:           "high",
			RecommendedSkills:   []string{"autoreview", "gitcrawl"},
			VisibleTools:        []string{"browser", "host tools"},
			BlockedSurfaces:     []string{"outbound message sending", "host tools"},
			RequiredControls:    []string{"server-side HAI approval", "AGENT_RUNTIME_WORKSPACE_ROOT"},
			ValidationChecklist: []string{"verify controlled output", "store runtime evidence"},
		},
		ExitCode:    -1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    sourceURI,
		SourceLabel:  "OpenClaw blocked launch",
	})
	decisionID := "runtime:" + launchID.String() + ":review"

	detail, err := service.ResolveDecision(created.ID, DecisionResolutionRequest{
		DecisionID:    decisionID,
		DecisionType:  "runtime_attempt_review",
		Approved:      true,
		Reason:        "blocked: high-risk surfaces detected",
		Note:          "Create a safe recovery path first.",
		EvidenceURI:   sourceURI,
		EvidenceLabel: "OpenClaw blocked launch",
		Actor:         "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveDecision returned error: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("workflow intake calls = %d, want 1", workflowService.calls)
	}
	if workflowService.received.SourceURI != sourceURI || workflowService.received.SourceID != decisionID {
		t.Fatalf("workflow provenance = %#v", workflowService.received)
	}
	if workflowService.received.ContentType != "runtime_attempt_review" || workflowService.received.Trigger != "pursuit_decision_approved" || !workflowService.received.RequiresReview {
		t.Fatalf("workflow recovery metadata = %#v", workflowService.received)
	}
	if workflowService.received.OwnerIdentity != "alice" {
		t.Fatalf("recovery workflow owner = %q, want persisted pursuit owner alice", workflowService.received.OwnerIdentity)
	}
	for _, expected := range []string{"Route trace:", "skills=autoreview, gitcrawl", "Required controls:", "Validation checklist:", "Do not retry the runtime directly"} {
		if !strings.Contains(workflowService.received.Input, expected) {
			t.Fatalf("recovery workflow input missing %q:\n%s", expected, workflowService.received.Input)
		}
	}
	foundRecoveryWorkflow := false
	for _, link := range detail.Links {
		if link.LinkType == LinkWorkflow && link.Relationship == "runtime_recovery_workflow" && link.SourceURI == sourceURI {
			foundRecoveryWorkflow = true
		}
	}
	if !foundRecoveryWorkflow {
		t.Fatalf("runtime recovery workflow link missing: %#v", detail.Links)
	}
	if len(detail.Workflows) != 1 || detail.Workflows[0].RequiresApproval != true {
		t.Fatalf("recovery workflow not visible as approval-gated work: %#v", detail.Workflows)
	}
	if len(detail.ApprovalItems) != 1 || detail.Summary.NeedsRobert == 0 {
		t.Fatalf("recovery workflow should route back to Robert approval: summary=%#v approvals=%#v", detail.Summary, detail.ApprovalItems)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.ID == decisionID {
			t.Fatalf("approved runtime decision was regenerated: %#v", detail.DecisionQueue)
		}
	}
}

func TestApprovedRuntimeDecisionRemainsPendingWhenRecoveryWorkflowFails(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{err: errNotFound("workflow intake")}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Keep failed runtime recovery actionable", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	sourceURI := "automation-launch://" + launchID.String()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "agent runtime registry is not configured",
		ExitCode:    -1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	if _, err := service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    sourceURI,
		SourceLabel:  "OpenClaw blocked launch",
	}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}
	decisionID := "runtime:" + launchID.String() + ":review"

	_, err = service.ResolveDecision(created.ID, DecisionResolutionRequest{
		DecisionID:   decisionID,
		DecisionType: "runtime_attempt_review",
		Approved:     true,
		EvidenceURI:  sourceURI,
		Actor:        "Robert",
	})
	if err == nil || !strings.Contains(err.Error(), "workflow intake not found") {
		t.Fatalf("ResolveDecision error = %v, want recovery workflow failure", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("workflow intake calls = %d, want 1", workflowService.calls)
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.NeedsRobert == 0 || detail.Summary.Blocked == 0 {
		t.Fatalf("failed recovery decision was removed from operator queues: summary=%#v", detail.Summary)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.ID == decisionID && decision.Status == "pending" {
			return
		}
	}
	t.Fatalf("failed recovery decision was incorrectly resolved: %#v", detail.DecisionQueue)
}

func TestRecoveredRuntimeAttemptStopsBlockingPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Recover OpenClaw through governed workflow", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	workflowID := uuid.New()
	sourceURI := "automation-launch://" + launchID.String()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "blocked",
		Message:     "agent runtime registry is not configured",
		ExitCode:    -1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:                 workflowID,
		Title:              "Recover OpenClaw runtime configuration",
		ProjectKey:         "018-HAI",
		CurrentState:       workflow.StateCompleted,
		SourceType:         "pursuit_decision",
		SourceURI:          sourceURI,
		SourceLabel:        "OpenClaw blocked launch",
		VerificationStatus: "verified",
		RecoveryStatus:     workflow.RecoveryCompletedAfterRetry,
	}
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "execution_attempt",
		SourceURI:    sourceURI,
		SourceLabel:  "OpenClaw blocked launch",
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkWorkflow,
		LinkID:       workflowID.String(),
		Relationship: "operational_work",
		SourceURI:    sourceURI,
		SourceLabel:  "OpenClaw recovery workflow",
	})

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.RuntimeAttempts) != 1 {
		t.Fatalf("runtime attempt evidence disappeared: %#v", detail.RuntimeAttempts)
	}
	if detail.Summary.Blocked != 0 || len(detail.Blockers) != 0 {
		t.Fatalf("recovered runtime attempt still blocks pursuit: summary=%#v blockers=%#v", detail.Summary, detail.Blockers)
	}
	if detail.Summary.LinkedEvidence != 1 {
		t.Fatalf("linked evidence = %d, want verified recovery workflow counted as evidence", detail.Summary.LinkedEvidence)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.DecisionType == "runtime_attempt_review" {
			t.Fatalf("recovered runtime attempt still requests Robert decision: %#v", detail.DecisionQueue)
		}
	}
	for _, action := range detail.NextActions {
		if strings.Contains(strings.ToLower(action.Label), "runtime attempt") {
			t.Fatalf("recovered runtime attempt still appears as next action: %#v", detail.NextActions)
		}
	}
	if detail.Summary.CompletionCandidate != true {
		t.Fatalf("summary = %#v, want recovered runtime workflow to make pursuit completion candidate", detail.Summary)
	}
}

func TestUpdateAllowsVerifiedCompletionWithLinkedRuntimeAttemptEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Complete OpenClaw runtime validation", ProjectKey: "018-HAI"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	launchID := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{
		ID:          launchID,
		RuntimeType: "openclaw",
		LaunchType:  "agent_runtime",
		Status:      "completed",
		Message:     "OpenClaw completed the approved agent task",
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	})
	_, _ = service.Link(created.ID, LinkRequest{
		LinkType:     LinkAgentRuntime,
		LinkID:       launchID.String(),
		Relationship: "completion_evidence",
		SourceURI:    "automation-launch://" + launchID.String(),
		SourceLabel:  "OpenClaw runtime launch",
	})

	updated, err := service.Update(created.ID, UpdateRequest{
		CompletionState: CompletionVerified,
		Actor:           "operator",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.CompletionState != CompletionVerified {
		t.Fatalf("completion state = %s, want verified", updated.CompletionState)
	}
}

func TestDashboardKeepsHighRiskPursuitInRobertQueue(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:       "Legal reply to municipality",
		Description: "Government legal response with possible financial consequence.",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if len(dashboard.NeedsRobert) != 1 || dashboard.NeedsRobert[0].Pursuit.ID != created.ID {
		t.Fatalf("high-risk pursuit was not surfaced for Robert: %#v", dashboard.NeedsRobert)
	}
	if len(dashboard.VAReady) != 0 || len(dashboard.SystemReady) != 0 {
		t.Fatalf("high-risk pursuit leaked into VA/system queues: va=%#v system=%#v", dashboard.VAReady, dashboard.SystemReady)
	}
	item := dashboard.NeedsRobert[0]
	if item.CurrentState == "" || item.WhatChanged == "" || item.DecisionCards == 0 || item.TimelineItems == 0 {
		t.Fatalf("dashboard item missing operational summary metadata: %#v", item)
	}
}

func TestDashboardAggregatesRobertDecisionCards(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:                 "Approve OpenClaw recovery plan",
		ProjectKey:            "018-HAI",
		RiskLevel:             "high",
		AutonomyLevel:         "approve_before_execute",
		NextRecommendedAction: "Create a governed recovery workflow before retrying OpenClaw.",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["decisionQueue"] != 1 {
		t.Fatalf("decision queue count = %d, want 1", dashboard.Counts["decisionQueue"])
	}
	if len(dashboard.DecisionQueue) != 1 {
		t.Fatalf("decision queue = %#v, want one card", dashboard.DecisionQueue)
	}
	card := dashboard.DecisionQueue[0]
	if card.Pursuit.ID != created.ID {
		t.Fatalf("decision card pursuit = %s, want %s", card.Pursuit.ID, created.ID)
	}
	if card.Decision.DecisionType != "pursuit_next_action" || card.Decision.Status != "pending" {
		t.Fatalf("decision card = %#v, want pending pursuit next action", card.Decision)
	}
	if !card.Decision.RequiresApproval || card.Decision.YesLabel == "" || card.Decision.NoLabel == "" {
		t.Fatalf("decision card missing Yes/No approval framing: %#v", card.Decision)
	}
	if card.CurrentState == "" || card.NextAction == "" || card.EvidenceLine == "" {
		t.Fatalf("decision card missing dashboard context: %#v", card)
	}
}

func TestApprovedPursuitNextActionCreatesGovernedWorkflowOnServer(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{
		Title:                 "Prepare legal recovery plan",
		OwnerIdentity:         "alice",
		ProjectKey:            "018-HAI",
		RiskLevel:             "high",
		AutonomyLevel:         "approve_before_execute",
		NextRecommendedAction: "Prepare a source-linked legal recovery workflow for Robert to review.",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	decisionID := nextActionDecisionID(created.ID)

	detail, err := service.ResolveDecisionForOwner("alice", created.ID, DecisionResolutionRequest{
		DecisionID:   decisionID,
		DecisionType: "pursuit_next_action",
		Approved:     true,
		Reason:       "Robert approved creating the governed workflow.",
		Actor:        "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveDecisionForOwner returned error: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("workflow intake calls = %d, want 1", workflowService.calls)
	}
	if workflowService.received.OwnerIdentity != "alice" || workflowService.received.SourceID != decisionID || workflowService.received.SourceType != "pursuit_decision" {
		t.Fatalf("workflow decision provenance = %#v", workflowService.received)
	}
	if workflowService.received.ContentType != "pursuit_next_action" || workflowService.received.Trigger != "pursuit_decision_approved" || !workflowService.received.RequiresReview {
		t.Fatalf("workflow decision guard = %#v", workflowService.received)
	}
	if !strings.Contains(workflowService.received.Input, "Recommended next action: Prepare a source-linked legal recovery workflow") {
		t.Fatalf("workflow input lost pursuit context: %q", workflowService.received.Input)
	}
	if len(detail.Workflows) != 1 || !detail.Workflows[0].RequiresApproval || detail.Workflows[0].SourceID != decisionID {
		t.Fatalf("governed workflow not linked to approved pursuit decision: %#v", detail.Workflows)
	}
	foundWorkflowLink := false
	for _, link := range detail.Links {
		if link.LinkType == LinkWorkflow && link.Relationship == "approved_next_action_workflow" && link.LinkID == detail.Workflows[0].ID.String() {
			foundWorkflowLink = true
		}
	}
	if !foundWorkflowLink {
		t.Fatalf("approved next-action workflow link missing: %#v", detail.Links)
	}
	for _, decision := range detail.DecisionQueue {
		if decision.ID == decisionID {
			t.Fatalf("approved next-action decision was regenerated: %#v", detail.DecisionQueue)
		}
	}

	if _, err := service.ResolveDecisionForOwner("alice", created.ID, DecisionResolutionRequest{
		DecisionID: decisionID, DecisionType: "pursuit_next_action", Approved: true, Actor: "Robert",
	}); err != nil {
		t.Fatalf("replayed approval should be idempotent: %v", err)
	}
	if workflowService.calls != 1 {
		t.Fatalf("replayed approval created duplicate workflow work: %d calls", workflowService.calls)
	}
}

func TestPursuitNextActionDecisionRejectsForgedOrNoLongerPendingAction(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	created, err := service.Create(CreateRequest{Title: "Low-risk notes", OwnerIdentity: "alice", RiskLevel: "low"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	_, err = service.ResolveDecisionForOwner("alice", created.ID, DecisionResolutionRequest{
		DecisionID: nextActionDecisionID(created.ID), DecisionType: "pursuit_next_action", Approved: true, Actor: "Robert",
	})
	if err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("forged next-action decision error = %v, want pending-decision guard", err)
	}
	if workflowService.calls != 0 {
		t.Fatalf("forged next-action decision created workflow work: %d calls", workflowService.calls)
	}
}

func TestDelegationPackageCompilesBoundedVAWorkWithChecklistAndSources(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:          "Organize insurance evidence",
		DesiredOutcome: "A complete, source-linked evidence bundle is ready for Robert to review.",
		RiskLevel:      "low",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:           workflowID,
		Title:        "Collect and organize evidence",
		Description:  "Review the source material and organize the evidence list.",
		CurrentState: "waiting_external_input",
		RiskLevel:    "low",
		NextAction:   "Prepare the evidence list from the linked sources.",
	}
	repo.openLoops = append(repo.openLoops, models.WorkflowOpenLoop{
		ID:               uuid.New(),
		WorkflowID:       workflowID,
		ResponsibleParty: "VA",
		WaitingFor:       "evidence review",
		NextAction:       "Prepare the evidence list from the linked sources.",
		Status:           "open",
	})
	repo.checklistItems = append(repo.checklistItems, models.WorkflowChecklistItem{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		Label:      "List each source with its date and relevance.",
		Status:     "pending",
		Position:   1,
	})
	repo.sourceLinks = append(repo.sourceLinks, models.WorkflowSourceLink{
		ID:           uuid.New(),
		WorkflowID:   workflowID,
		SourceType:   "email_export",
		SourceURI:    "file:///connected-sources/claim.mbox",
		SourceLabel:  "Claim correspondence export",
		Relationship: "evidence",
	})
	if _, err := service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String(), Relationship: "operational_work"}); err != nil {
		t.Fatalf("Link workflow returned error: %v", err)
	}

	brief, err := service.DelegationPackage(created.ID)
	if err != nil {
		t.Fatalf("DelegationPackage returned error: %v", err)
	}
	if !brief.Ready || brief.Status != "ready" {
		t.Fatalf("delegation package not ready: %#v", brief)
	}
	if len(brief.WorkItems) != 1 || brief.WorkItems[0].WorkflowID != workflowID.String() || len(brief.WorkItems[0].Checklist) != 1 {
		t.Fatalf("delegation work items missing workflow/checklist: %#v", brief.WorkItems)
	}
	if len(brief.SourceContext) != 1 || brief.SourceContext[0].SourceURI != "file:///connected-sources/claim.mbox" {
		t.Fatalf("delegation source context missing: %#v", brief.SourceContext)
	}
	if len(brief.BlockedActions) == 0 || len(brief.DeliveryRequirements) == 0 {
		t.Fatalf("delegation safety boundaries missing: %#v", brief)
	}
}

func TestDelegationPackageDoesNotReleaseHighRiskWorkToVA(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Send legal position to municipality", RiskLevel: "high"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	repo.workflows[workflowID] = models.WorkflowItem{ID: workflowID, Title: "Prepare legal response", CurrentState: "needs_approval", RiskLevel: "high", RequiresApproval: true, ApprovalStatus: "pending"}
	repo.openLoops = append(repo.openLoops, models.WorkflowOpenLoop{ID: uuid.New(), WorkflowID: workflowID, ResponsibleParty: "VA", NextAction: "Send the legal response to the municipality", Status: "open"})
	if _, err := service.Link(created.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: workflowID.String(), Relationship: "operational_work"}); err != nil {
		t.Fatalf("Link workflow returned error: %v", err)
	}

	brief, err := service.DelegationPackage(created.ID)
	if err != nil {
		t.Fatalf("DelegationPackage returned error: %v", err)
	}
	if brief.Ready || brief.Status != "not_ready" || len(brief.OutstandingRobertActions) == 0 {
		t.Fatalf("high-risk work leaked into VA handoff: %#v", brief)
	}
}

func TestFollowUpActionRiskUsesWholeActionTerms(t *testing.T) {
	if got := followUpActionRisk("high", "Prepare the applicant profile for Robert to review"); got != "low" {
		t.Fatalf("preparation action risk = %q, want low", got)
	}
	if got := followUpActionRisk("low", "File the completed response with the municipality"); got != "high" {
		t.Fatalf("filing action risk = %q, want high", got)
	}
}

func TestDashboardSurfacesDuePursuitReview(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:                 "Organize local notes",
		ProjectKey:            "ops",
		NextRecommendedAction: "Review stale notes and choose next step",
		NextReviewAt:          time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["reviewDue"] != 1 || len(dashboard.ReviewDue) != 1 {
		t.Fatalf("review-due pursuit was not surfaced: counts=%#v reviewDue=%#v", dashboard.Counts, dashboard.ReviewDue)
	}
	if dashboard.ReviewDue[0].Pursuit.ID != created.ID || !dashboard.ReviewDue[0].ReviewDue {
		t.Fatalf("review-due item metadata missing: %#v", dashboard.ReviewDue[0])
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if !detail.Summary.ReviewDue {
		t.Fatalf("detail summary did not mark review due: %#v", detail.Summary)
	}
	if len(detail.ActionQueues.SystemReady) == 0 {
		t.Fatalf("low-risk review-due pursuit should be system-ready: %#v", detail.ActionQueues)
	}
	if !strings.Contains(detail.ActionQueues.SystemReady[0].Reason, "scheduled pursuit review is due") {
		t.Fatalf("review action reason missing: %#v", detail.ActionQueues.SystemReady[0])
	}
}

func TestOperatingSnapshotBuildsBriefAndDecisionsFromOneDashboard(t *testing.T) {
	repo := newFakeRepo()
	engine := NewService(repo, nil)
	highRisk, err := engine.Create(CreateRequest{
		Title:                 "Prepare legal reply",
		ProjectKey:            "vivare",
		RiskLevel:             "high",
		NextRecommendedAction: "Approve formal reply direction",
	})
	if err != nil {
		t.Fatalf("Create high-risk returned error: %v", err)
	}
	_, err = engine.Create(CreateRequest{
		Title:        "Review source backlog",
		ProjectKey:   "hai",
		NextReviewAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Create review-due returned error: %v", err)
	}

	snapshot, err := engine.(*service).OperatingSnapshot()
	if err != nil {
		t.Fatalf("OperatingSnapshot returned error: %v", err)
	}
	brief := snapshot.Brief
	if snapshot.Dashboard == nil {
		t.Fatal("operating snapshot did not retain its source dashboard for in-cycle reuse")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal operating snapshot: %v", err)
	}
	if strings.Contains(string(encoded), "dashboard") {
		t.Fatalf("internal pursuit dashboard leaked into API JSON: %s", encoded)
	}
	if brief == nil || len(snapshot.Decisions) != 1 {
		t.Fatalf("operating snapshot = %#v, want one brief and one Robert decision", snapshot)
	}
	if snapshot.Decisions[0].Pursuit.ID != highRisk.ID {
		t.Fatalf("snapshot decision pursuit = %s, want %s", snapshot.Decisions[0].Pursuit.ID, highRisk.ID)
	}
	if brief.OperatingMode != "needs_robert" {
		t.Fatalf("operating mode = %q, want needs_robert", brief.OperatingMode)
	}
	if brief.NeedsRobert != 1 || brief.PlanningNeeded != 2 || brief.ReviewDue != 1 {
		t.Fatalf("brief counts = Robert %d planning %d review %d", brief.NeedsRobert, brief.PlanningNeeded, brief.ReviewDue)
	}
	if !strings.Contains(brief.PrimaryAction, "Robert-only") {
		t.Fatalf("primary action = %q, want Robert-first guidance", brief.PrimaryAction)
	}
	if len(brief.Cards) < 2 {
		t.Fatalf("brief cards too small: %#v", brief.Cards)
	}
	if brief.Cards[0].PursuitID != highRisk.ID.String() || brief.Cards[0].Queue != "Robert" {
		t.Fatalf("first card = %#v, want high-risk Robert card first", brief.Cards[0])
	}
	seen := map[string]bool{}
	for _, card := range brief.Cards {
		if seen[card.PursuitID] {
			t.Fatalf("brief card duplicated pursuit %s in %#v", card.PursuitID, brief.Cards)
		}
		seen[card.PursuitID] = true
	}
}

func TestReviewClearsDueReviewAndAuditsDecision(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:        "Review insurance claim pursuit",
		NextReviewAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	detail, err := service.Review(created.ID, ReviewRequest{Action: "complete", Actor: "robert"})
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if detail.Summary.ReviewDue {
		t.Fatalf("review should not remain due after completion: %#v", detail.Summary)
	}
	if detail.Pursuit.NextReviewAt == nil || !detail.Pursuit.NextReviewAt.After(time.Now().UTC()) {
		t.Fatalf("next review was not scheduled forward: %#v", detail.Pursuit.NextReviewAt)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["reviewDue"] != 0 || len(dashboard.ReviewDue) != 0 {
		t.Fatalf("review-due queue did not clear: counts=%#v reviewDue=%#v", dashboard.Counts, dashboard.ReviewDue)
	}
	activity, err := service.Activity(created.ID)
	if err != nil {
		t.Fatalf("Activity returned error: %v", err)
	}
	if len(activity) == 0 || activity[0].EventType != "pursuit.reviewed" {
		t.Fatalf("review activity was not recorded first: %#v", activity)
	}
}

func TestDashboardSurfacesPlanningNeededPursuits(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:      "Prepare business idea",
		ProjectKey: "growth",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if dashboard.Counts["planningNeeded"] != 1 || len(dashboard.PlanningNeeded) != 1 {
		t.Fatalf("planning-needed pursuit was not surfaced: counts=%#v planning=%#v", dashboard.Counts, dashboard.PlanningNeeded)
	}
	if dashboard.PlanningNeeded[0].Pursuit.ID != created.ID || !dashboard.PlanningNeeded[0].PlanningNeeded {
		t.Fatalf("planning-needed metadata missing: %#v", dashboard.PlanningNeeded[0])
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if !detail.Summary.PlanningNeeded {
		t.Fatalf("detail summary did not mark planning needed: %#v", detail.Summary)
	}
	if !strings.Contains(detail.Summary.CurrentState, "No linked workflow exists yet") {
		t.Fatalf("summary did not explain missing workflow planning need: %s", detail.Summary.CurrentState)
	}
}

func TestDetailIncludesCompactSourceItemsWithoutRawContent(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "ASR claim evidence", ProjectKey: "asr"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	rawID := uuid.New()
	repo.sourceItems[rawID] = models.SourceRawItem{
		ID:         rawID,
		SourceID:   uuid.New(),
		ExternalID: "email-123",
		ProjectKey: "asr",
		ItemType:   "email",
		Title:      "ASR requested receipts",
		SourceURI:  "local://source/email-123",
		Content:    "full raw private email body should not be exposed through pursuit detail sourceItems",
		Metadata:   `{"from":"insurer@example.test"}`,
		FetchedAt:  time.Now().UTC(),
	}
	if _, err := service.Link(created.ID, LinkRequest{LinkType: LinkSourceItem, LinkID: rawID.String(), Relationship: "evidence"}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if len(detail.SourceItems) != 1 || detail.SourceItems[0].Title != "ASR requested receipts" {
		t.Fatalf("source items = %#v", detail.SourceItems)
	}
	if detail.Summary.LinkedEvidence != 1 {
		t.Fatalf("linked evidence count = %d", detail.Summary.LinkedEvidence)
	}
}

func TestDetailBlocksArchivedAndMissingSourceExtractionEvidence(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Review legal evidence provenance", ProjectKey: "vivare"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	activeID := uuid.New()
	archivedID := uuid.New()
	missingID := uuid.New()
	repo.extractions[activeID] = models.SourceExtraction{
		ID:          activeID,
		ProjectKey:  "vivare",
		Summary:     "Active evidence summary.",
		SourceLabel: "Active evidence",
	}
	repo.extractions[archivedID] = models.SourceExtraction{
		ID:          archivedID,
		ProjectKey:  "vivare",
		Summary:     "Archived evidence summary.",
		SourceLabel: "Archived evidence",
		Archived:    true,
	}
	for _, link := range []LinkRequest{
		{LinkType: LinkSourceExtraction, LinkID: activeID.String(), Relationship: "evidence", SourceLabel: "Active evidence"},
		{LinkType: LinkSourceExtraction, LinkID: archivedID.String(), Relationship: "evidence", SourceLabel: "Archived evidence"},
		{LinkType: LinkSourceExtraction, LinkID: missingID.String(), Relationship: "evidence", SourceLabel: "Missing evidence"},
	} {
		if _, err := service.Link(created.ID, link); err != nil {
			t.Fatalf("Link returned error: %v", err)
		}
	}

	detail, err := service.Detail(created.ID)
	if err != nil {
		t.Fatalf("Detail returned error: %v", err)
	}
	if detail.Summary.LinkedEvidence != 1 {
		t.Fatalf("linked evidence count = %d, want only active extraction counted", detail.Summary.LinkedEvidence)
	}
	if detail.Summary.Blocked != 2 || len(detail.Blockers) != 2 {
		t.Fatalf("blockers summary=%#v blockers=%#v, want archived and missing extraction blockers", detail.Summary, detail.Blockers)
	}
	if detail.Summary.NeedsRobert == 0 || len(detail.ActionQueues.NeedsRobert) == 0 {
		t.Fatalf("source provenance blockers did not route to Robert: summary=%#v queues=%#v", detail.Summary, detail.ActionQueues)
	}
	reasons := strings.Join([]string{detail.Blockers[0].Reason, detail.Blockers[1].Reason}, " ")
	if !strings.Contains(reasons, "archived") || !strings.Contains(reasons, "missing") {
		t.Fatalf("source retraction blockers missing reason detail: %#v", detail.Blockers)
	}
	if !strings.Contains(detail.Summary.CurrentState, "source evidence was archived or is missing") {
		t.Fatalf("summary did not mention stale source evidence: %q", detail.Summary.CurrentState)
	}
}

func TestDeleteLinkRequiresOwningPursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	first, _ := service.Create(CreateRequest{Title: "First pursuit"})
	second, _ := service.Create(CreateRequest{Title: "Second pursuit"})
	link, err := service.Link(first.ID, LinkRequest{LinkType: LinkMemory, LinkID: uuid.New().String()})
	if err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	if err := service.DeleteLink(second.ID, link.ID, "test-operator"); err == nil {
		t.Fatalf("expected deleting another pursuit's link to fail")
	}
	links, _ := repo.FindLinks(first.ID)
	if len(links) != 1 {
		t.Fatalf("link was removed from wrong pursuit: %#v", links)
	}
	if err := service.DeleteLink(first.ID, link.ID, "test-operator"); err != nil {
		t.Fatalf("DeleteLink returned error for owner: %v", err)
	}
	links, _ = repo.FindLinks(first.ID)
	if len(links) != 0 {
		t.Fatalf("owned link still present: %#v", links)
	}
	activities, _ := repo.FindActivities(first.ID, 20)
	for _, activity := range activities {
		if activity.EventType == "pursuit.link_removed" {
			if activity.Actor != "test-operator" {
				t.Fatalf("link removal actor = %q, want verified actor", activity.Actor)
			}
			return
		}
	}
	t.Fatal("expected pursuit.link_removed activity")
}

func TestPursuitLinkRejectsSelfReference(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Keep relationship graph meaningful", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	_, err = service.Link(created.ID, LinkRequest{
		OwnerIdentity: "alice",
		LinkType:      LinkPursuit,
		LinkID:        created.ID.String(),
		Relationship:  "related_case",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be linked to itself") {
		t.Fatalf("self pursuit link error = %v, want self-link rejection", err)
	}
	links, findErr := repo.FindLinks(created.ID)
	if findErr != nil {
		t.Fatalf("FindLinks returned error: %v", findErr)
	}
	if len(links) != 0 {
		t.Fatalf("self pursuit link was persisted: %#v", links)
	}
}

func TestLinkActivityRefreshesPursuitFreshness(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Refresh pursuit activity"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	staleAt := time.Now().UTC().Add(-15 * 24 * time.Hour)
	record := repo.pursuits[created.ID]
	record.LastActivityAt = &staleAt
	repo.pursuits[created.ID] = record

	if _, err := service.Link(created.ID, LinkRequest{
		LinkType:     LinkMemory,
		LinkID:       uuid.New().String(),
		Relationship: "context_memory",
		SourceURI:    "memory://project-context",
		Actor:        "operator",
	}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	updated, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if updated.LastActivityAt == nil || !updated.LastActivityAt.After(staleAt) {
		t.Fatalf("last activity = %v, want time after %v", updated.LastActivityAt, staleAt)
	}
	if isStale(*updated) {
		t.Fatalf("pursuit remained stale after a recorded link activity: %#v", updated)
	}
}

func TestSummaryRefreshDoesNotMaskStalePursuit(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Keep stale pursuit visible"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	staleAt := time.Now().UTC().Add(-15 * 24 * time.Hour)
	record := repo.pursuits[created.ID]
	record.LastActivityAt = &staleAt
	repo.pursuits[created.ID] = record
	for index := range repo.activity[created.ID] {
		repo.activity[created.ID][index].CreatedAt = staleAt
	}

	if _, err := service.RefreshSummary(created.ID, "system"); err != nil {
		t.Fatalf("RefreshSummary returned error: %v", err)
	}
	updated, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if updated.LastActivityAt == nil || !updated.LastActivityAt.Equal(staleAt) {
		t.Fatalf("summary refresh changed last activity to %v, want %v", updated.LastActivityAt, staleAt)
	}
	if !isStale(*updated) {
		t.Fatalf("summary refresh hid a stale pursuit: %#v", updated)
	}
	activity, err := repo.FindActivities(created.ID, 20)
	if err != nil {
		t.Fatalf("FindActivities returned error: %v", err)
	}
	if !activityContains(activity, "pursuit.summary_refreshed") {
		t.Fatalf("summary refresh was not retained in the audit feed: %#v", activity)
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}
	if len(dashboard.Stale) != 1 || dashboard.Stale[0].Pursuit.ID != created.ID {
		t.Fatalf("summary-only audit activity hid stale work from dashboard: %#v", dashboard.Stale)
	}
}

func TestDashboardUsesLinkedOperationalActivityForFreshness(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{Title: "Track workflow progress", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	workflowID := uuid.New()
	now := time.Now().UTC()
	repo.workflows[workflowID] = models.WorkflowItem{
		ID:            workflowID,
		OwnerIdentity: "alice",
		Title:         "Run governed evidence check",
		CurrentState:  workflow.StateInProgress,
		UpdatedAt:     now,
	}
	if _, err := service.Link(created.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkWorkflow, LinkID: workflowID.String(), Relationship: "operational_work"}); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}
	staleAt := now.Add(-15 * 24 * time.Hour)
	record := repo.pursuits[created.ID]
	record.LastActivityAt = &staleAt
	repo.pursuits[created.ID] = record

	dashboard, err := service.DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("DashboardForOwner returned error: %v", err)
	}
	if len(dashboard.Stale) != 0 {
		t.Fatalf("linked workflow activity was ignored for freshness: %#v", dashboard.Stale)
	}
	if len(dashboard.RecentlyChanged) != 1 || dashboard.RecentlyChanged[0].EffectiveLastActivityAt == nil {
		t.Fatalf("recently changed pursuit lacks derived activity: %#v", dashboard.RecentlyChanged)
	}
	if dashboard.RecentlyChanged[0].EffectiveLastActivityAt.Before(now) {
		t.Fatalf("effective activity = %v, want workflow update at or after %v", dashboard.RecentlyChanged[0].EffectiveLastActivityAt, now)
	}
	updated, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if updated.LastActivityAt == nil || !updated.LastActivityAt.Equal(staleAt) {
		t.Fatalf("dashboard read mutated persisted freshness: %#v", updated)
	}
}

func TestDashboardRecentlyChangedRanksLinkedOperationalActivity(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	older, err := service.Create(CreateRequest{Title: "Earlier workflow update", OwnerIdentity: "alice", PriorityScore: 80})
	if err != nil {
		t.Fatalf("Create older pursuit returned error: %v", err)
	}
	newer, err := service.Create(CreateRequest{Title: "Latest workflow update", OwnerIdentity: "alice", PriorityScore: 10})
	if err != nil {
		t.Fatalf("Create newer pursuit returned error: %v", err)
	}
	now := time.Now().UTC()
	olderWorkflowID := uuid.New()
	newerWorkflowID := uuid.New()
	repo.workflows[olderWorkflowID] = models.WorkflowItem{
		ID:            olderWorkflowID,
		OwnerIdentity: "alice",
		Title:         "Older linked work",
		CurrentState:  workflow.StateInProgress,
		UpdatedAt:     now.Add(-3 * time.Minute),
	}
	repo.workflows[newerWorkflowID] = models.WorkflowItem{
		ID:            newerWorkflowID,
		OwnerIdentity: "alice",
		Title:         "Newer linked work",
		CurrentState:  workflow.StateInProgress,
		UpdatedAt:     now.Add(-1 * time.Minute),
	}
	for _, link := range []struct {
		pursuitID  uuid.UUID
		workflowID uuid.UUID
	}{
		{pursuitID: older.ID, workflowID: olderWorkflowID},
		{pursuitID: newer.ID, workflowID: newerWorkflowID},
	} {
		if _, err := service.Link(link.pursuitID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkWorkflow, LinkID: link.workflowID.String(), Relationship: "operational_work"}); err != nil {
			t.Fatalf("Link returned error: %v", err)
		}
		staleAt := now.Add(-15 * 24 * time.Hour)
		record := repo.pursuits[link.pursuitID]
		record.LastActivityAt = &staleAt
		repo.pursuits[link.pursuitID] = record
		// Link creation is itself audited at wall-clock time. Move that fixture
		// activity behind the linked workflow timestamps so this test isolates
		// the ranking signal named in its assertion instead of depending on
		// Windows clock resolution or the order of the Link calls above.
		for index := range repo.activity[link.pursuitID] {
			repo.activity[link.pursuitID][index].CreatedAt = staleAt
		}
	}

	dashboard, err := service.DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("DashboardForOwner returned error: %v", err)
	}
	if len(dashboard.RecentlyChanged) != 2 {
		t.Fatalf("recently changed = %#v, want both pursuits", dashboard.RecentlyChanged)
	}
	if dashboard.RecentlyChanged[0].Pursuit.ID != newer.ID || dashboard.RecentlyChanged[1].Pursuit.ID != older.ID {
		t.Fatalf("recently changed order = %#v, want newest linked activity first", dashboard.RecentlyChanged)
	}
}

func TestSortListItemsByEffectiveActivityUsesIDAsFinalTieBreaker(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	items := []PursuitListItem{
		{
			Pursuit: models.Pursuit{
				ID:            highID,
				Title:         "Same title",
				PriorityScore: 50,
				UpdatedAt:     at,
			},
			EffectiveLastActivityAt: &at,
		},
		{
			Pursuit: models.Pursuit{
				ID:            lowID,
				Title:         "Same title",
				PriorityScore: 50,
				UpdatedAt:     at,
			},
			EffectiveLastActivityAt: &at,
		},
	}

	sortListItemsByEffectiveActivity(items)
	if items[0].Pursuit.ID != lowID || items[1].Pursuit.ID != highID {
		t.Fatalf("IDs = %s/%s, want %s/%s", items[0].Pursuit.ID, items[1].Pursuit.ID, lowID, highID)
	}
}

func timelineContains(items []PursuitTimelineItem, kind, messagePart string) bool {
	for _, item := range items {
		if item.Kind == kind && strings.Contains(item.Message, messagePart) {
			return true
		}
	}
	return false
}

func timelineSourceContains(items []PursuitTimelineItem, kind, sourcePart string) bool {
	for _, item := range items {
		if item.Kind == kind && strings.Contains(item.SourceURI, sourcePart) {
			return true
		}
	}
	return false
}

func TestOwnerScopedMutationsDoNotAdoptOwnerlessLegacyPursuits(t *testing.T) {
	repo := newFakeRepo()
	workflowService := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflowService)
	legacy, err := service.Create(CreateRequest{Title: "Legacy migration pursuit", ProjectKey: "legacy-project"})
	if err != nil {
		t.Fatalf("Create legacy pursuit: %v", err)
	}

	for _, operation := range []struct {
		name string
		call func() error
	}{
		{name: "update", call: func() error {
			_, err := service.UpdateForOwner("alice", legacy.ID, UpdateRequest{Title: "Alice update"})
			return err
		}},
		{name: "archive", call: func() error { _, err := service.ArchiveForOwner("alice", legacy.ID, true, "alice"); return err }},
		{name: "reopen", call: func() error { _, err := service.ReopenForOwner("alice", legacy.ID, "alice", "reopen"); return err }},
		{name: "link", call: func() error {
			_, err := service.Link(legacy.ID, LinkRequest{OwnerIdentity: "alice", LinkType: LinkMemory, LinkID: uuid.NewString(), Relationship: "evidence"})
			return err
		}},
		{name: "delete link", call: func() error { return service.DeleteLinkForOwner("alice", legacy.ID, uuid.New(), "alice") }},
		{name: "intake", call: func() error {
			_, err := service.IntakeForOwner("alice", legacy.ID, IntakeRequest{Input: "new operational work"})
			return err
		}},
		{name: "plan", call: func() error {
			_, err := service.PlanForOwner("alice", legacy.ID, PlanRequest{Input: "plan legacy work"})
			return err
		}},
		{name: "resolve decision", call: func() error {
			_, err := service.ResolveDecisionForOwner("alice", legacy.ID, DecisionResolutionRequest{DecisionID: "completion", Approved: true})
			return err
		}},
		{name: "refresh summary", call: func() error { _, err := service.RefreshSummaryForOwner("alice", legacy.ID, "alice"); return err }},
		{name: "review", call: func() error {
			_, err := service.ReviewForOwner("alice", legacy.ID, ReviewRequest{Action: "complete", Actor: "alice"})
			return err
		}},
	} {
		if err := operation.call(); err == nil {
			t.Fatalf("%s adopted an ownerless legacy pursuit", operation.name)
		}
	}

	routed, err := service.RouteIntake(IntakeRequest{OwnerIdentity: "alice", ProjectKey: "legacy-project", Input: legacy.Title})
	if err != nil {
		t.Fatalf("route intake created no owner-scoped alternative: %v", err)
	}
	if routed.PursuitID == legacy.ID {
		t.Fatal("route intake adopted an ownerless legacy pursuit")
	}

	stored, err := repo.FindByID(legacy.ID)
	if err != nil {
		t.Fatalf("Find legacy pursuit: %v", err)
	}
	if stored.OwnerIdentity != "" || stored.Title != legacy.Title || stored.Archived || stored.Status != legacy.Status || stored.CompletionState != legacy.CompletionState {
		t.Fatalf("owner-scoped mutation changed legacy pursuit: %#v", stored)
	}
	links, err := repo.FindLinks(legacy.ID)
	if err != nil {
		t.Fatalf("Find legacy links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("owner-scoped mutation linked legacy pursuit: %#v", links)
	}
	if workflowService.calls != 0 {
		t.Fatalf("owner-scoped mutation sent work to workflow intake: %d calls", workflowService.calls)
	}
}

type fakeRepo struct {
	pursuits             map[uuid.UUID]models.Pursuit
	links                map[uuid.UUID]models.PursuitLink
	activity             map[uuid.UUID][]models.PursuitActivity
	taskAttempts         map[string]models.PursuitTaskAttempt
	resourceEvents       map[uuid.UUID][]models.PursuitResourceEvent
	resourceReservations map[string]models.PursuitResourceReservation
	resourceSettlements  map[uuid.UUID]models.PursuitResourceReservationSettlement
	workflowAttestations map[uuid.UUID]models.WorkflowCompletionAttestation
	portfolioProofs      map[uuid.UUID]models.PursuitPortfolioWorkflowSettlementProof
	resourceAuditErr     error
	workflows            map[uuid.UUID]models.WorkflowItem
	checklistItems       []models.WorkflowChecklistItem
	openLoops            []models.WorkflowOpenLoop
	proposals            []models.WorkflowProposal
	qualityGates         []models.WorkflowQualityGate
	decisions            []models.WorkflowDecision
	transitions          []models.WorkflowTransition
	sourceLinks          []models.WorkflowSourceLink
	events               []models.WorkflowEvent
	evidence             []models.WorkflowEvidenceClaim
	memories             map[uuid.UUID]models.ContextMemory
	conversations        map[uuid.UUID]models.AIConversationArchive
	ambientOpportunities map[uuid.UUID]models.AmbientOpportunity
	automations          map[uuid.UUID]models.Automation
	launchEvents         []models.AutomationLaunchEvent
	verificationRuns     map[uuid.UUID]models.VerificationRun
	verificationClaims   map[uuid.UUID]models.VerificationClaim
	verificationEvidence map[uuid.UUID]models.VerificationEvidence
	sourceItems          map[uuid.UUID]models.SourceRawItem
	extractions          map[uuid.UUID]models.SourceExtraction
	sourceOwners         map[uuid.UUID]string
	linkedWorkflowErr    error
}

// Embedding only the base contract intentionally hides the optional resource
// ledger capability so fail-closed service behavior can be tested.
type pursuitRepositoryWithoutResourceLedger struct{ Repository }

type fakeBulkDashboardRepo struct {
	*fakeRepo
	bulkCalls               int
	evidenceProjectionCalls int
	findByIDCalls           int
}

func (r *fakeBulkDashboardRepo) FindByID(id uuid.UUID) (*models.Pursuit, error) {
	r.findByIDCalls++
	return r.fakeRepo.FindByID(id)
}

func (r *fakeBulkDashboardRepo) FindVisibleLinksForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID) ([]models.PursuitLink, error) {
	r.bulkCalls++
	result := []models.PursuitLink{}
	for _, pursuitID := range pursuitIDs {
		links, err := r.fakeRepo.FindLinks(pursuitID)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			handled, allowed, err := r.fakeRepo.LinkVisibleToOwner(ownerIdentity, link.LinkType, link.LinkID)
			if err != nil {
				return nil, err
			}
			if !handled || allowed {
				result = append(result, link)
			}
		}
	}
	return result, nil
}

func (r *fakeBulkDashboardRepo) FindActivitiesForPursuits(pursuitIDs []uuid.UUID, limit int) ([]models.PursuitActivity, error) {
	result := []models.PursuitActivity{}
	for _, pursuitID := range pursuitIDs {
		items, err := r.fakeRepo.FindActivities(pursuitID, limit)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
	}
	return result, nil
}

func (r *fakeBulkDashboardRepo) FindTaskAttemptsForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID, limit int) ([]models.PursuitTaskAttempt, error) {
	result := []models.PursuitTaskAttempt{}
	for _, pursuitID := range pursuitIDs {
		items, err := r.fakeRepo.FindTaskAttempts(pursuitID, limit)
		if err != nil {
			return nil, err
		}
		result = append(result, taskAttemptsVisibleToOwner(ownerIdentity, items)...)
	}
	return result, nil
}

func (r *fakeBulkDashboardRepo) FindRuntimeAttemptsForOwner(ownerIdentity string, automationIDs, launchIDs []uuid.UUID) ([]models.AutomationLaunchEvent, error) {
	items, err := r.fakeRepo.FindLinkedAutomationLaunches(automationIDs, launchIDs, 50)
	return runtimeAttemptsVisibleToOwner(ownerIdentity, items), err
}

func (r *fakeBulkDashboardRepo) FindEvidenceProjectionForDashboard(workflowIDs, memoryIDs, sourceItemIDs, extractionIDs, verificationRunIDs []uuid.UUID) (pursuitDashboardEvidenceProjection, error) {
	r.evidenceProjectionCalls++
	result := pursuitDashboardEvidenceProjection{}
	var err error
	if result.WorkflowEvidence, err = r.fakeRepo.FindLinkedEvidence(workflowIDs); err != nil {
		return result, err
	}
	if result.Memories, err = r.fakeRepo.FindLinkedMemories(memoryIDs); err != nil {
		return result, err
	}
	if result.SourceItems, err = r.fakeRepo.FindLinkedSourceItems(sourceItemIDs); err != nil {
		return result, err
	}
	if result.SourceExtractions, err = r.fakeRepo.FindLinkedExtractions(extractionIDs); err != nil {
		return result, err
	}
	if result.VerificationClaims, err = r.fakeRepo.FindLinkedVerificationClaims(verificationRunIDs); err != nil {
		return result, err
	}
	if result.VerificationEvidence, err = r.fakeRepo.FindLinkedVerificationEvidence(verificationRunIDs); err != nil {
		return result, err
	}
	for index := range result.WorkflowEvidence {
		result.WorkflowEvidence[index].ClaimText = ""
		result.WorkflowEvidence[index].SourceURI = ""
		result.WorkflowEvidence[index].SourceLabel = ""
	}
	for index := range result.Memories {
		result.Memories[index] = models.ContextMemory{ID: result.Memories[index].ID}
	}
	for index := range result.SourceItems {
		result.SourceItems[index].Content = ""
		result.SourceItems[index].ContentHash = ""
	}
	for index := range result.SourceExtractions {
		result.SourceExtractions[index].Text = ""
		result.SourceExtractions[index].Entities = ""
		result.SourceExtractions[index].Dates = ""
		result.SourceExtractions[index].Tasks = ""
		result.SourceExtractions[index].Decisions = ""
		result.SourceExtractions[index].FollowUps = ""
		result.SourceExtractions[index].ContentHash = ""
	}
	for index := range result.VerificationClaims {
		result.VerificationClaims[index].ClaimText = ""
		result.VerificationClaims[index].SourceRefs = ""
		result.VerificationClaims[index].SupportExplanation = ""
	}
	for index := range result.VerificationEvidence {
		result.VerificationEvidence[index].Snippet = ""
		result.VerificationEvidence[index].SourceLabel = ""
		result.VerificationEvidence[index].Authority = ""
		result.VerificationEvidence[index].Freshness = ""
		result.VerificationEvidence[index].RejectReason = ""
	}
	return result, nil
}

func (r *fakeBulkDashboardRepo) FindResourceProjectionForPursuits(_ string, _ []models.Pursuit) (pursuitDashboardResourceProjection, error) {
	return pursuitDashboardResourceProjection{
		Totals: map[uuid.UUID]PursuitResourceTotals{}, ReservationTotals: map[uuid.UUID]PursuitResourceReservationTotals{},
		ActiveReservations: map[uuid.UUID][]models.PursuitResourceReservation{},
	}, nil
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		pursuits:             map[uuid.UUID]models.Pursuit{},
		links:                map[uuid.UUID]models.PursuitLink{},
		activity:             map[uuid.UUID][]models.PursuitActivity{},
		taskAttempts:         map[string]models.PursuitTaskAttempt{},
		resourceEvents:       map[uuid.UUID][]models.PursuitResourceEvent{},
		resourceReservations: map[string]models.PursuitResourceReservation{},
		resourceSettlements:  map[uuid.UUID]models.PursuitResourceReservationSettlement{},
		workflowAttestations: map[uuid.UUID]models.WorkflowCompletionAttestation{},
		portfolioProofs:      map[uuid.UUID]models.PursuitPortfolioWorkflowSettlementProof{},
		workflows:            map[uuid.UUID]models.WorkflowItem{},
		memories:             map[uuid.UUID]models.ContextMemory{},
		conversations:        map[uuid.UUID]models.AIConversationArchive{},
		ambientOpportunities: map[uuid.UUID]models.AmbientOpportunity{},
		automations:          map[uuid.UUID]models.Automation{},
		verificationRuns:     map[uuid.UUID]models.VerificationRun{},
		verificationClaims:   map[uuid.UUID]models.VerificationClaim{},
		verificationEvidence: map[uuid.UUID]models.VerificationEvidence{},
		sourceItems:          map[uuid.UUID]models.SourceRawItem{},
		extractions:          map[uuid.UUID]models.SourceExtraction{},
		sourceOwners:         map[uuid.UUID]string{},
	}
}

func (r *fakeRepo) Create(pursuit *models.Pursuit) (*models.Pursuit, error) {
	if pursuit.ID == uuid.Nil {
		pursuit.ID = uuid.New()
	}
	now := time.Now().UTC()
	pursuit.CreatedAt = now
	pursuit.UpdatedAt = now
	r.pursuits[pursuit.ID] = *pursuit
	return pursuit, nil
}

func (r *fakeRepo) Update(pursuit *models.Pursuit) (*models.Pursuit, error) {
	pursuit.UpdatedAt = time.Now().UTC()
	r.pursuits[pursuit.ID] = *pursuit
	return pursuit, nil
}

func (r *fakeRepo) FindByID(id uuid.UUID) (*models.Pursuit, error) {
	item, ok := r.pursuits[id]
	if !ok {
		return nil, errNotFound("pursuit")
	}
	return &item, nil
}

func (r *fakeRepo) FindAll(includeArchived bool) ([]models.Pursuit, error) {
	result := []models.Pursuit{}
	for _, item := range r.pursuits {
		if !includeArchived && item.Archived {
			continue
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].PriorityScore != result[j].PriorityScore {
			return result[i].PriorityScore > result[j].PriorityScore
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func (r *fakeRepo) CreateLink(link *models.PursuitLink) (*models.PursuitLink, error) {
	for _, existing := range r.links {
		if existing.PursuitID == link.PursuitID && existing.LinkType == link.LinkType && existing.LinkID == link.LinkID && existing.Relationship == link.Relationship {
			copyExisting := existing
			return &copyExisting, nil
		}
	}
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	link.CreatedAt = time.Now().UTC()
	r.links[link.ID] = *link
	if link.LinkType == LinkWorkflow {
		if id, err := uuid.Parse(link.LinkID); err == nil {
			if _, ok := r.workflows[id]; !ok {
				r.workflows[id] = models.WorkflowItem{ID: id, Title: "Generated workflow", CurrentState: workflow.StateNeedsApproval, RequiresApproval: true, ApprovalStatus: "pending"}
			}
		}
	}
	return link, nil
}

func (r *fakeRepo) LinkVisibleToOwner(ownerIdentity, linkType, linkID string) (bool, bool, error) {
	switch linkType {
	case LinkPursuit:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.pursuits[id]
		return true, ok && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity), nil
	case LinkWorkflow:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.workflows[id]
		return true, ok && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity), nil
	case LinkMemory:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.memories[id]
		return true, ok && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity), nil
	case LinkAIConversation:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.conversations[id]
		return true, ok && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity), nil
	case LinkAmbientOpportunity:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.ambientOpportunities[id]
		return true, ok && item.OwnerIdentity == ownerIdentity, nil
	case LinkSourceItem:
		for id, item := range r.sourceItems {
			if linkID != id.String() && linkID != item.ExternalID {
				continue
			}
			owner := r.sourceOwners[item.SourceID]
			return true, owner == "" || owner == ownerIdentity, nil
		}
		return true, false, nil
	case LinkSourceExtraction:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.extractions[id]
		if !ok {
			return true, false, nil
		}
		owner := r.sourceOwners[item.SourceID]
		return true, owner == "" || owner == ownerIdentity, nil
	case LinkVerification:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		item, ok := r.verificationRuns[id]
		return true, ok && (item.OwnerIdentity == "" || item.OwnerIdentity == ownerIdentity), nil
	case LinkAgentRuntime:
		id, err := uuid.Parse(linkID)
		if err != nil {
			return true, false, nil
		}
		for _, item := range r.launchEvents {
			if item.ID == id {
				return true, item.OwnerIdentity == ownerIdentity, nil
			}
		}
		return true, false, nil
	default:
		return false, true, nil
	}
}

func (r *fakeRepo) DeleteLink(pursuitID uuid.UUID, id uuid.UUID) error {
	link, ok := r.links[id]
	if !ok || link.PursuitID != pursuitID {
		return errNotFound("link")
	}
	delete(r.links, id)
	return nil
}

func (r *fakeRepo) FindLinks(pursuitID uuid.UUID) ([]models.PursuitLink, error) {
	result := []models.PursuitLink{}
	for _, link := range r.links {
		if link.PursuitID == pursuitID {
			result = append(result, link)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLink(linkType, linkID string) (*models.PursuitLink, error) {
	for _, link := range r.links {
		if link.LinkType == linkType && link.LinkID == linkID {
			copyLink := link
			return &copyLink, nil
		}
	}
	return nil, errNotFound("link")
}

func (r *fakeRepo) FindLinkBySourceURI(sourceURI string) (*models.PursuitLink, error) {
	for _, link := range r.links {
		if link.SourceURI == sourceURI {
			copyLink := link
			return &copyLink, nil
		}
	}
	return nil, errNotFound("link")
}

func (r *fakeRepo) FindLinkForOwner(ownerIdentity, linkType, linkID string) (*models.PursuitLink, error) {
	return r.findVisibleLink(ownerIdentity, func(link models.PursuitLink) bool {
		return link.LinkType == linkType && link.LinkID == linkID
	})
}

func (r *fakeRepo) FindLinkBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.PursuitLink, error) {
	return r.findVisibleLink(ownerIdentity, func(link models.PursuitLink) bool {
		return link.SourceURI == sourceURI
	})
}

func (r *fakeRepo) findVisibleLink(ownerIdentity string, matches func(models.PursuitLink) bool) (*models.PursuitLink, error) {
	var best *models.PursuitLink
	for _, link := range r.links {
		pursuit, ok := r.pursuits[link.PursuitID]
		if !ok || !pursuitVisibleTo(pursuit, ownerIdentity) || !matches(link) {
			continue
		}
		if best == nil || link.Confidence > best.Confidence || (link.Confidence == best.Confidence && link.CreatedAt.After(best.CreatedAt)) {
			copyLink := link
			best = &copyLink
		}
	}
	if best == nil {
		return nil, errNotFound("link")
	}
	return best, nil
}

func (r *fakeRepo) CreateActivity(activity *models.PursuitActivity) (*models.PursuitActivity, error) {
	if activity.IdempotencyKey != "" {
		for _, existing := range r.activity[activity.PursuitID] {
			if existing.IdempotencyKey == activity.IdempotencyKey {
				copyExisting := existing
				return &copyExisting, nil
			}
		}
	}
	if activity.ID == uuid.Nil {
		activity.ID = uuid.New()
	}
	if activity.CreatedAt.IsZero() {
		activity.CreatedAt = time.Now().UTC()
	}
	r.activity[activity.PursuitID] = append([]models.PursuitActivity{*activity}, r.activity[activity.PursuitID]...)
	return activity, nil
}

func (r *fakeRepo) FindActivities(pursuitID uuid.UUID, limit int) ([]models.PursuitActivity, error) {
	result := append([]models.PursuitActivity{}, r.activity[pursuitID]...)
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *fakeRepo) FindActivityByIdentity(
	pursuitID uuid.UUID,
	eventType, sourceType, sourceID, sourceURI string,
) (*models.PursuitActivity, bool, error) {
	for _, activity := range r.activity[pursuitID] {
		if activity.EventType == eventType && activity.SourceType == sourceType &&
			activity.SourceID == sourceID && activity.SourceURI == sourceURI {
			copyActivity := activity
			return &copyActivity, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeRepo) UpsertTaskAttempt(attempt *models.PursuitTaskAttempt) (*models.PursuitTaskAttempt, error) {
	if existing, ok := r.taskAttempts[attempt.TaskPlanID]; ok {
		if existing.PursuitID != attempt.PursuitID {
			return nil, fmt.Errorf("task attempt is already linked to another pursuit")
		}
		attempt.ID = existing.ID
		attempt.CreatedAt = existing.CreatedAt
	} else if attempt.ID == uuid.Nil {
		attempt.ID = uuid.New()
	}
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = time.Now().UTC()
	}
	attempt.UpdatedAt = time.Now().UTC()
	r.taskAttempts[attempt.TaskPlanID] = *attempt
	return attempt, nil
}

func (r *fakeRepo) FindTaskAttempts(pursuitID uuid.UUID, limit int) ([]models.PursuitTaskAttempt, error) {
	result := []models.PursuitTaskAttempt{}
	for _, attempt := range r.taskAttempts {
		if attempt.PursuitID == pursuitID {
			result = append(result, attempt)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *fakeRepo) AppendResourceEvent(event *models.PursuitResourceEvent) (*models.PursuitResourceEvent, bool, error) {
	for _, existing := range r.resourceEvents[event.PursuitID] {
		if existing.OwnerIdentity == event.OwnerIdentity && existing.IdempotencyKey == event.IdempotencyKey {
			if existing.RecordDigest != event.RecordDigest {
				return nil, false, fmt.Errorf("idempotency key was already used for a different pursuit resource event")
			}
			copyExisting := existing
			return &copyExisting, false, nil
		}
	}
	copyEvent := *event
	if copyEvent.ID == uuid.Nil {
		copyEvent.ID = uuid.New()
	}
	if copyEvent.RecordedAt.IsZero() {
		copyEvent.RecordedAt = time.Now().UTC()
	}
	r.resourceEvents[event.PursuitID] = append(r.resourceEvents[event.PursuitID], copyEvent)
	return &copyEvent, true, nil
}

func (r *fakeRepo) FindResourceEventsForOwner(ownerIdentity string, pursuitID uuid.UUID, limit int) ([]models.PursuitResourceEvent, error) {
	result := []models.PursuitResourceEvent{}
	for _, event := range r.resourceEvents[pursuitID] {
		if event.OwnerIdentity == ownerIdentity {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OccurredAt.After(result[j].OccurredAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *fakeRepo) SummarizeResourceEventsForOwner(ownerIdentity string, pursuitID uuid.UUID) (PursuitResourceTotals, error) {
	totals := PursuitResourceTotals{}
	for _, event := range r.resourceEvents[pursuitID] {
		if event.OwnerIdentity != ownerIdentity {
			continue
		}
		totals.EventCount++
		switch event.Kind {
		case ResourceKindEffort:
			totals.EffortMinutes += event.EffortMinutes
		case ResourceKindSpend:
			totals.IncurredMinor += event.AmountMinor
		case ResourceKindRefund:
			totals.RefundedMinor += event.AmountMinor
		}
		if totals.LatestRecordedAt == nil || event.RecordedAt.After(*totals.LatestRecordedAt) {
			value := event.RecordedAt
			totals.LatestRecordedAt = &value
		}
	}
	return totals, nil
}

func resourceReservationFakeKey(ownerIdentity string, pursuitID uuid.UUID, operationID string) string {
	return ownerIdentity + ":" + pursuitID.String() + ":" + operationID
}

func (r *fakeRepo) ReserveResource(reservation *models.PursuitResourceReservation, activity *models.PursuitActivity) (*models.PursuitResourceReservation, bool, error) {
	key := resourceReservationFakeKey(reservation.OwnerIdentity, reservation.PursuitID, reservation.OperationID)
	if existing, ok := r.resourceReservations[key]; ok {
		if existing.RecordDigest != reservation.RecordDigest {
			return nil, false, fmt.Errorf("resource reservation operation was already used with different estimates")
		}
		copyExisting := existing
		return &copyExisting, false, nil
	}
	if r.resourceAuditErr != nil {
		return nil, false, r.resourceAuditErr
	}
	copyReservation := *reservation
	if copyReservation.ID == uuid.Nil {
		copyReservation.ID = uuid.New()
	}
	r.resourceReservations[key] = copyReservation
	if activity != nil {
		copyActivity := *activity
		if copyActivity.ID == uuid.Nil {
			copyActivity.ID = uuid.New()
		}
		if copyActivity.CreatedAt.IsZero() {
			copyActivity.CreatedAt = time.Now().UTC()
		}
		r.activity[copyActivity.PursuitID] = append([]models.PursuitActivity{copyActivity}, r.activity[copyActivity.PursuitID]...)
		pursuit := r.pursuits[copyActivity.PursuitID]
		if pursuit.LastActivityAt == nil || pursuit.LastActivityAt.Before(copyActivity.CreatedAt) {
			activityAt := copyActivity.CreatedAt
			pursuit.LastActivityAt = &activityAt
			r.pursuits[pursuit.ID] = pursuit
		}
	}
	return &copyReservation, true, nil
}

func (r *fakeRepo) FindResourceReservation(ownerIdentity string, pursuitID uuid.UUID, operationID string) (*models.PursuitResourceReservation, error) {
	reservation, ok := r.resourceReservations[resourceReservationFakeKey(ownerIdentity, pursuitID, operationID)]
	if !ok {
		return nil, fmt.Errorf("resource reservation not found")
	}
	copyReservation := reservation
	return &copyReservation, nil
}

func (r *fakeRepo) FindResourceReservationByID(ownerIdentity string, pursuitID, reservationID uuid.UUID) (*models.PursuitResourceReservation, error) {
	for _, reservation := range r.resourceReservations {
		if reservation.OwnerIdentity == ownerIdentity && reservation.PursuitID == pursuitID && reservation.ID == reservationID {
			copyReservation := reservation
			return &copyReservation, nil
		}
	}
	return nil, fmt.Errorf("resource reservation not found")
}

func (r *fakeRepo) FindActiveResourceReservations(ownerIdentity string, pursuitID uuid.UUID, limit int) ([]models.PursuitResourceReservation, error) {
	result := []models.PursuitResourceReservation{}
	for _, reservation := range r.resourceReservations {
		if reservation.OwnerIdentity != ownerIdentity || reservation.PursuitID != pursuitID {
			continue
		}
		if _, settled := r.resourceSettlements[reservation.ID]; settled {
			continue
		}
		result = append(result, reservation)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ReservedAt.Before(result[right].ReservedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *fakeRepo) SettleResourceReservation(settlement *models.PursuitResourceReservationSettlement, events []models.PursuitResourceEvent, activities []models.PursuitActivity) (*models.PursuitResourceReservationSettlement, bool, error) {
	if existing, ok := r.resourceSettlements[settlement.ReservationID]; ok {
		if existing.RecordDigest != settlement.RecordDigest {
			return nil, false, fmt.Errorf("resource reservation was already settled with a different outcome")
		}
		copyExisting := existing
		return &copyExisting, false, nil
	}
	if r.resourceAuditErr != nil {
		return nil, false, r.resourceAuditErr
	}
	copySettlement := *settlement
	if copySettlement.ID == uuid.Nil {
		copySettlement.ID = uuid.New()
	}
	r.resourceSettlements[settlement.ReservationID] = copySettlement
	for index := range events {
		if _, _, err := r.AppendResourceEvent(&events[index]); err != nil {
			return nil, false, err
		}
	}
	for index := range activities {
		copyActivity := activities[index]
		if copyActivity.ID == uuid.Nil {
			copyActivity.ID = uuid.New()
		}
		if copyActivity.CreatedAt.IsZero() {
			copyActivity.CreatedAt = time.Now().UTC()
		}
		r.activity[copyActivity.PursuitID] = append([]models.PursuitActivity{copyActivity}, r.activity[copyActivity.PursuitID]...)
		pursuit := r.pursuits[copyActivity.PursuitID]
		if pursuit.LastActivityAt == nil || pursuit.LastActivityAt.Before(copyActivity.CreatedAt) {
			activityAt := copyActivity.CreatedAt
			pursuit.LastActivityAt = &activityAt
			r.pursuits[pursuit.ID] = pursuit
		}
	}
	return &copySettlement, true, nil
}

func (r *fakeRepo) FindWorkflowCompletionAttestation(ownerIdentity string, workflowID uuid.UUID) (*models.WorkflowCompletionAttestation, error) {
	attestation, ok := r.workflowAttestations[workflowID]
	if !ok || attestation.OwnerIdentity != ownerIdentity {
		return nil, nil
	}
	copyAttestation := attestation
	return &copyAttestation, nil
}

func (r *fakeRepo) FindPortfolioWorkflowSettlementProof(ownerIdentity string, itemID, workflowID uuid.UUID) (*models.PursuitPortfolioWorkflowSettlementProof, *models.PursuitResourceReservationSettlement, error) {
	for _, proof := range r.portfolioProofs {
		if proof.OwnerIdentity != ownerIdentity || proof.ProposalItemID != itemID || proof.WorkflowID != workflowID {
			continue
		}
		settlement, ok := r.resourceSettlements[proof.ReservationID]
		if !ok {
			return nil, nil, fmt.Errorf("portfolio proof settlement is missing")
		}
		copyProof, copySettlement := proof, settlement
		return &copyProof, &copySettlement, nil
	}
	return nil, nil, nil
}

func (r *fakeRepo) SettleVerifiedPortfolioWorkflow(commit portfolioWorkflowSettlementCommit) (*models.PursuitPortfolioWorkflowSettlementProof, *models.PursuitResourceReservationSettlement, bool, error) {
	if err := validatePortfolioWorkflowSettlementCommit(commit); err != nil {
		return nil, nil, false, err
	}
	proof := commit.Proof
	if existing, ok := r.portfolioProofs[proof.ReservationID]; ok {
		if existing.RequestDigest != proof.RequestDigest || existing.RecordDigest != proof.RecordDigest {
			return nil, nil, false, fmt.Errorf("portfolio workflow reservation was already settled with different proof")
		}
		settlement := r.resourceSettlements[proof.ReservationID]
		return &existing, &settlement, false, nil
	}
	workflowItem, ok := r.workflows[proof.WorkflowID]
	attestation, attested := r.workflowAttestations[proof.WorkflowID]
	if !ok || !attested || workflowItem.OwnerIdentity != proof.OwnerIdentity ||
		workflowItem.CurrentState != workflow.StateCompleted || attestation.RecordDigest != proof.CompletionAttestationDigest ||
		!portfolioSettlementAcceptsVerification(attestation.VerificationStatus) {
		return nil, nil, false, fmt.Errorf("immutable verified workflow completion is unavailable")
	}
	settlement, _, err := r.SettleResourceReservation(commit.Settlement, commit.Events, commit.Activities)
	if err != nil {
		return nil, nil, false, err
	}
	r.portfolioProofs[proof.ReservationID] = *proof
	copyProof := *proof
	return &copyProof, settlement, true, nil
}

func (r *fakeRepo) SummarizeActiveResourceReservations(ownerIdentity string, pursuitID uuid.UUID) (PursuitResourceReservationTotals, error) {
	totals := PursuitResourceReservationTotals{}
	for _, reservation := range r.resourceReservations {
		if reservation.OwnerIdentity != ownerIdentity || reservation.PursuitID != pursuitID {
			continue
		}
		if _, settled := r.resourceSettlements[reservation.ID]; settled {
			continue
		}
		totals.EffortMinutes += reservation.EstimatedEffortMinutes
		totals.CostMicros += reservation.EstimatedCostMicros
		totals.ReservationCount++
		if totals.LatestReservedAt == nil || reservation.ReservedAt.After(*totals.LatestReservedAt) {
			value := reservation.ReservedAt
			totals.LatestReservedAt = &value
		}
	}
	return totals, nil
}

func (r *fakeRepo) FindLinkedWorkflows(ids []uuid.UUID) ([]models.WorkflowItem, error) {
	if r.linkedWorkflowErr != nil {
		return nil, r.linkedWorkflowErr
	}
	result := []models.WorkflowItem{}
	for _, id := range ids {
		if item, ok := r.workflows[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedChecklistItems(workflowIDs []uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowChecklistItem{}
	for _, item := range r.checklistItems {
		if workflowSet[item.WorkflowID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedOpenLoops(workflowIDs []uuid.UUID) ([]models.WorkflowOpenLoop, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowOpenLoop{}
	for _, loop := range r.openLoops {
		if workflowSet[loop.WorkflowID] {
			result = append(result, loop)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedProposals(workflowIDs []uuid.UUID) ([]models.WorkflowProposal, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowProposal{}
	for _, proposal := range r.proposals {
		if workflowSet[proposal.WorkflowID] {
			result = append(result, proposal)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedQualityGates(workflowIDs []uuid.UUID) ([]models.WorkflowQualityGate, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowQualityGate{}
	for _, gate := range r.qualityGates {
		if workflowSet[gate.WorkflowID] {
			result = append(result, gate)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedDecisions(workflowIDs []uuid.UUID) ([]models.WorkflowDecision, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowDecision{}
	for _, decision := range r.decisions {
		if workflowSet[decision.WorkflowID] {
			result = append(result, decision)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedTransitions(workflowIDs []uuid.UUID) ([]models.WorkflowTransition, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowTransition{}
	for _, transition := range r.transitions {
		if workflowSet[transition.WorkflowID] {
			result = append(result, transition)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedSourceLinks(workflowIDs []uuid.UUID) ([]models.WorkflowSourceLink, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowSourceLink{}
	for _, link := range r.sourceLinks {
		if workflowSet[link.WorkflowID] {
			result = append(result, link)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedEvents(workflowIDs []uuid.UUID) ([]models.WorkflowEvent, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowEvent{}
	for _, event := range r.events {
		if workflowSet[event.WorkflowID] {
			result = append(result, event)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedEvidence(workflowIDs []uuid.UUID) ([]models.WorkflowEvidenceClaim, error) {
	workflowSet := map[uuid.UUID]bool{}
	for _, id := range workflowIDs {
		workflowSet[id] = true
	}
	result := []models.WorkflowEvidenceClaim{}
	for _, item := range r.evidence {
		if workflowSet[item.WorkflowID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedMemories(ids []uuid.UUID) ([]models.ContextMemory, error) {
	result := []models.ContextMemory{}
	for _, id := range ids {
		if item, ok := r.memories[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedConversations(ids []uuid.UUID) ([]models.AIConversationArchive, error) {
	result := []models.AIConversationArchive{}
	for _, id := range ids {
		if item, ok := r.conversations[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedAmbientOpportunities(ids []uuid.UUID) ([]models.AmbientOpportunity, error) {
	result := []models.AmbientOpportunity{}
	for _, id := range ids {
		if item, ok := r.ambientOpportunities[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedSourceItems(ids []uuid.UUID) ([]models.SourceRawItem, error) {
	result := []models.SourceRawItem{}
	for _, id := range ids {
		if item, ok := r.sourceItems[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedExtractions(ids []uuid.UUID) ([]models.SourceExtraction, error) {
	result := []models.SourceExtraction{}
	for _, id := range ids {
		if item, ok := r.extractions[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedVerificationRuns(ids []uuid.UUID) ([]models.VerificationRun, error) {
	result := []models.VerificationRun{}
	for _, id := range ids {
		if item, ok := r.verificationRuns[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedVerificationClaims(runIDs []uuid.UUID) ([]models.VerificationClaim, error) {
	runSet := map[uuid.UUID]bool{}
	for _, id := range runIDs {
		runSet[id] = true
	}
	result := []models.VerificationClaim{}
	for _, item := range r.verificationClaims {
		if runSet[item.RunID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedVerificationEvidence(runIDs []uuid.UUID) ([]models.VerificationEvidence, error) {
	runSet := map[uuid.UUID]bool{}
	for _, id := range runIDs {
		runSet[id] = true
	}
	result := []models.VerificationEvidence{}
	for _, item := range r.verificationEvidence {
		if runSet[item.RunID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedAutomations(ids []uuid.UUID) ([]models.Automation, error) {
	result := []models.Automation{}
	for _, id := range ids {
		if item, ok := r.automations[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *fakeRepo) FindLinkedAutomationLaunches(automationIDs []uuid.UUID, launchIDs []uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error) {
	automationSet := map[uuid.UUID]bool{}
	for _, id := range automationIDs {
		automationSet[id] = true
	}
	launchSet := map[uuid.UUID]bool{}
	for _, id := range launchIDs {
		launchSet[id] = true
	}
	result := []models.AutomationLaunchEvent{}
	for _, event := range r.launchEvents {
		if automationSet[event.AutomationID] || launchSet[event.ID] {
			result = append(result, event)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type fakeWorkflowIntake struct {
	received     workflow.IntakeRequest
	calls        int
	lastGetOwner string
	records      map[uuid.UUID]*workflow.WorkflowRecord
	repo         *fakeRepo
	err          error
}

func (f *fakeWorkflowIntake) Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	f.calls++
	f.received = request
	if f.err != nil {
		return nil, f.err
	}
	id := uuid.New()
	record := &workflow.WorkflowRecord{
		Item: models.WorkflowItem{
			ID:               id,
			OwnerIdentity:    request.OwnerIdentity,
			Title:            request.Input,
			ProjectKey:       request.ProjectKey,
			SourceType:       request.SourceType,
			SourceID:         request.SourceID,
			SourceURI:        request.SourceURI,
			CurrentState:     workflow.StateNeedsApproval,
			RiskLevel:        "high",
			RequiresApproval: true,
			ApprovalStatus:   "pending",
			ApprovalReason:   "high-risk pursuit intake",
			NextAction:       "Robert should approve the prepared response.",
		},
	}
	if f.records == nil {
		f.records = make(map[uuid.UUID]*workflow.WorkflowRecord)
	}
	f.records[id] = record
	if f.repo != nil {
		f.repo.workflows[id] = record.Item
	}
	return record, nil
}

func (f *fakeWorkflowIntake) GetForOwner(ownerIdentity string, id uuid.UUID) (*workflow.WorkflowRecord, error) {
	record, ok := f.records[id]
	if !ok {
		return nil, errNotFound("workflow")
	}
	f.lastGetOwner = ownerIdentity
	if record.Item.OwnerIdentity != "" && record.Item.OwnerIdentity != ownerIdentity {
		return nil, errNotFound("workflow")
	}
	return record, nil
}

type errNotFound string

func (e errNotFound) Error() string {
	return string(e) + " not found"
}

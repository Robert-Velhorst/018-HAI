package task

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/source"
)

func TestResourcePlanningSubtractsCalendarBusyTime(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "alice@example.test", PlanID: "calendar-aware", CreatedAt: now, Difficulty: 1,
		Steps:        []TaskStep{{ID: "plan", Name: "Plan", Allowed: true}},
		Capacity:     &frameworkregistry.CapacitySnapshot{TimeAvailableMinutes: 180},
		CalendarBusy: []source.CalendarBusyInterval{{Start: now.Add(10 * time.Minute), End: now.Add(time.Hour), Title: "Existing meeting"}},
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if len(decision.Scheduled) != 1 || !decision.Scheduled[0].Start.Equal(now.Add(time.Hour)) {
		t.Fatalf("task was scheduled through existing Calendar time: %#v", decision.Scheduled)
	}
}

func TestResourcePlanningFailsWhenCalendarConsumesConfirmedCapacity(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "alice@example.test", PlanID: "calendar-full", CreatedAt: now, Difficulty: 1,
		Steps:        []TaskStep{{ID: "plan", Name: "Plan", Allowed: true}},
		Capacity:     &frameworkregistry.CapacitySnapshot{TimeAvailableMinutes: 120},
		CalendarBusy: []source.CalendarBusyInterval{{Start: now, End: now.Add(2 * time.Hour), Title: "Existing work"}},
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if decision.Feasibility != resourceplanner.Infeasible || len(decision.CriticalBlockers) == 0 {
		t.Fatalf("calendar capacity exhaustion was not surfaced: %#v", decision)
	}
}

func TestResourcePlanningRequiresReviewForUnknownCapacity(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "alice@example.test", PlanID: "capacity-review", CreatedAt: now, Difficulty: 1,
		Steps:    []TaskStep{{ID: "plan", Name: "Plan", Allowed: true}},
		Capacity: &frameworkregistry.CapacitySnapshot{Status: "unknown", NeedsReview: true},
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if decision.Feasibility != resourceplanner.FeasibleWithApprovals || len(decision.ApprovalFlags) == 0 {
		t.Fatalf("unknown owner capacity did not require review: %#v", decision)
	}
}

func TestResourcePlanningBlocksPaidRouteUnderZeroBudget(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "alice@example.test",
		PlanID:        "plan-paid",
		CreatedAt:     now,
		Difficulty:    2,
		Steps:         []TaskStep{{ID: "generate", Name: "Generate result", Allowed: true}},
		ModelDecision: llm.RouteDecision{EstimatedCostEUR: 0.01, EstimatedInputTokens: 100, EstimatedOutputTokens: 50},
		SelectedTools: []string{"llm.generate"},
		PaidAllowed:   false,
		PaidBudgetEUR: 0,
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if decision.Feasibility != resourceplanner.Infeasible || decision.Budget.WithinCostLimit {
		t.Fatalf("paid route escaped zero budget: %#v", decision)
	}
	if decision.Authority != "advisory_only" || decision.CanExecute || decision.GrantsAuthority {
		t.Fatalf("resource plan granted authority: %#v", decision)
	}
}

func TestResourcePlanningCapacityAndDeadlineFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(2 * time.Hour)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "alice@example.test",
		WorkspaceID:   "project 018 HAI",
		PlanID:        "plan-capacity",
		CreatedAt:     now,
		Deadline:      &deadline,
		Difficulty:    5,
		Steps: []TaskStep{
			{ID: "plan", Name: "Plan", Allowed: true},
			{ID: "execute", Name: "Execute tool", Allowed: true},
		},
		ModelDecision: llm.RouteDecision{},
		SelectedTools: []string{},
		Capacity:      &frameworkregistry.CapacitySnapshot{TimeAvailableMinutes: 10},
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if decision.Feasibility != resourceplanner.Infeasible || len(decision.CriticalBlockers) == 0 {
		t.Fatalf("capacity shortage was not surfaced: %#v", decision)
	}

	past := now.Add(-time.Minute)
	_, err = defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		PlanID: "plan-past", CreatedAt: now, Deadline: &past, Difficulty: 1,
		Steps: []TaskStep{{ID: "step", Name: "Step"}},
	})
	if err == nil || !strings.Contains(err.Error(), "already passed") {
		t.Fatalf("past deadline should fail closed, got %v", err)
	}
}

func TestResourcePlanningUsesBoundedInternalScopeForPreview(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		PlanID: "preview-plan", CreatedAt: now, Difficulty: 1,
		Steps: []TaskStep{{ID: "inspect", Name: "Inspect", Allowed: true}},
	})
	if err != nil {
		t.Fatalf("unowned preview should remain plan-capable: %v", err)
	}
	if len(decision.OwnerScopeDigest) != 64 {
		t.Fatalf("preview owner scope was not bound: %#v", decision)
	}
}

func TestResourcePlanningKeepsAdmittedAutomaticRuntimeApprovalFree(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	risk := RiskAssessment{
		Level:                     "low",
		AllowedNow:                true,
		FrameworkAutonomyCeiling:  8,
		RequiredFrameworkAutonomy: 8,
	}
	decision, err := defaultResourcePlanner().PlanResources(ResourcePlanningRequest{
		OwnerIdentity: "operator@example.test",
		PlanID:        "automatic-readiness-runtime",
		CreatedAt:     now,
		Difficulty:    2,
		Risk:          risk,
		Steps: []TaskStep{
			{ID: "execute", Name: "Execute read-only readiness probe", Allowed: true},
			{ID: "verify", Name: "Verify readiness result", Allowed: true},
		},
		SelectedTools: []string{"tool-router"},
		// The backend readiness probe consumes controlled runtime capacity, not
		// Robert's time. An unavailable personal capacity observation must not
		// turn this already-admitted automatic runtime into an approval gate.
		Capacity: &frameworkregistry.CapacitySnapshot{Status: "unknown", NeedsReview: true},
	})
	if err != nil {
		t.Fatalf("PlanResources: %v", err)
	}
	if len(decision.ApprovalFlags) != 0 || decision.Feasibility != resourceplanner.Feasible {
		t.Fatalf("automatic runtime was turned into a resource approval gate: %#v", decision)
	}
	updated := applyResourcePlanningRisk(risk, decision)
	if updated.ApprovalRequired || !updated.AllowedNow {
		t.Fatalf("resource plan changed automatic runtime authority: %#v", updated)
	}
}

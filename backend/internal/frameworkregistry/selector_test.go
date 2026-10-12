package frameworkregistry

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildSelectionLegalGovernmentTask(t *testing.T) {
	now := time.Date(2026, time.July, 30, 8, 30, 0, 0, time.UTC)
	request := SelectionRequest{
		OwnerIdentity:    "robert",
		TaskPlanID:       "legal-plan-1",
		Request:          "Review the municipality letter and source documents, verify every factual claim, then draft a reply to the government lawyer.",
		ProjectKey:       "vivare-dispute",
		TaskType:         "legal correspondence",
		RiskLevel:        "high",
		SuccessCriteria:  []string{"reply is source-linked and ready for Robert's review"},
		NeedsDocuments:   true,
		NeedsWebAccess:   true,
		NeedsTools:       true,
		NeedsApproval:    true,
		ExecuteRequested: true,
		HumanApproved:    true,
	}

	decision, err := BuildSelection(testCatalog(t), testConstitution(), request, now)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}

	if decision.LifeDomain != "legal_government" {
		t.Fatalf("LifeDomain = %q, want legal_government", decision.LifeDomain)
	}
	if decision.TaskRiskLevel != "high" || decision.EffectiveRiskCeiling != "high" {
		t.Fatalf("high-risk contract = %q/%q", decision.TaskRiskLevel, decision.EffectiveRiskCeiling)
	}
	for _, id := range []string{
		"human-sovereignty",
		"intake-triage",
		"evaluation",
		"approval-control",
		"truth-evidence",
		"privacy-protection",
		"security-zero-trust",
		"autonomy-levels",
		"reliable-execution",
		"agent-threat-modeling",
		"legal-government-case",
	} {
		if !selectedID(decision, id) {
			t.Errorf("expected %q to be selected; got %v", id, selectedIDs(decision))
		}
	}
	if len(decision.Selected) > maxSelectedFrameworks {
		t.Fatalf("selected %d frameworks, want at most %d", len(decision.Selected), maxSelectedFrameworks)
	}
	if !decision.RequiresApproval {
		t.Fatal("high-risk legal work must remain approval-controlled even when HumanApproved is reported")
	}
	if !containsStringFragment(decision.ApprovalReasons, "does not remove") {
		t.Fatalf("approval reasons do not preserve other constraints: %v", decision.ApprovalReasons)
	}
	if decision.MaximumAutonomyLevel != caseApprovedExecutionAutonomyLevel {
		t.Fatalf(
			"MaximumAutonomyLevel = %d, want least-authority execution level %d",
			decision.MaximumAutonomyLevel,
			caseApprovedExecutionAutonomyLevel,
		)
	}
	if len(decision.RequiredAgents) == 0 || len(decision.EvidenceRequirements) == 0 ||
		len(decision.CompletionCriteria) == 0 || len(decision.LearningPlan) == 0 ||
		len(decision.ContextRequirements) == 0 {
		t.Fatalf("decision omitted operational requirements: %#v", decision)
	}
}

func TestBuildSelectionLowRiskPlanning(t *testing.T) {
	request := SelectionRequest{
		OwnerIdentity:   "robert",
		Request:         "Plan a low-risk weekly garden maintenance schedule around the available time.",
		TaskType:        "planning",
		RiskLevel:       "low",
		Difficulty:      4,
		SuccessCriteria: []string{"the weekly plan fits the available time"},
	}

	decision, err := BuildSelection(
		testCatalog(t),
		testConstitution(),
		request,
		time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}

	if decision.LifeDomain != "home_assets" {
		t.Fatalf("LifeDomain = %q, want home_assets", decision.LifeDomain)
	}
	if decision.TaskRiskLevel != "low" {
		t.Fatalf("TaskRiskLevel = %q, want low", decision.TaskRiskLevel)
	}
	for _, id := range []string{"human-sovereignty", "intake-triage", "evaluation", "formal-planning", "home-garden-assets"} {
		if !selectedID(decision, id) {
			t.Errorf("expected %q to be selected; got %v", id, selectedIDs(decision))
		}
	}
	for _, id := range []string{"approval-control", "privacy-protection", "security-zero-trust", "autonomy-levels", "reliable-execution", "agent-threat-modeling"} {
		if selectedID(decision, id) {
			t.Errorf("did not expect %q for low-risk planning; got %v", id, selectedIDs(decision))
		}
	}
	if decision.RequiresApproval {
		t.Fatalf("low-risk internal planning unexpectedly requires approval: %v", decision.ApprovalReasons)
	}
	if decision.MaximumAutonomyLevel != planAndSimulateAutonomyLevel {
		t.Fatalf(
			"MaximumAutonomyLevel = %d, want planning level %d",
			decision.MaximumAutonomyLevel,
			planAndSimulateAutonomyLevel,
		)
	}
}

func TestBuildSelectionRequiresExecutionAndUntrustedContentOverlays(t *testing.T) {
	decision, err := BuildSelection(
		testCatalog(t),
		testConstitution(),
		SelectionRequest{
			Request:             "Read an uploaded document, inspect current web content, and run a low-risk local tool to validate the result.",
			TaskType:            "document validation",
			RiskLevel:           "low",
			NeedsDocuments:      true,
			NeedsWebAccess:      true,
			NeedsTools:          true,
			NeedsLocalExecution: true,
			ExecuteRequested:    true,
		},
		time.Date(2026, time.July, 30, 9, 30, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	for _, id := range []string{
		"evaluation",
		"truth-evidence",
		"privacy-protection",
		"security-zero-trust",
		"autonomy-levels",
		"reliable-execution",
		"agent-threat-modeling",
	} {
		if !selectedID(decision, id) {
			t.Errorf("expected overlay %q to be selected; got %v", id, selectedIDs(decision))
		}
	}
	if len(decision.Selected) > maxSelectedFrameworks {
		t.Fatalf("selected %d frameworks, want at most %d", len(decision.Selected), maxSelectedFrameworks)
	}
	if decision.MaximumAutonomyLevel != reversibleAutomaticExecutionAutonomyLevel {
		t.Fatalf(
			"mandatory intake/evidence/security ceilings capped execution: got %d, want %d; selected=%v",
			decision.MaximumAutonomyLevel,
			reversibleAutomaticExecutionAutonomyLevel,
			selectedIDs(decision),
		)
	}
}

func TestBuildSelectionDoesNotTreatProhibitedExternalActionAsRequested(t *testing.T) {
	decision, err := BuildSelection(
		testCatalog(t),
		testConstitution(),
		SelectionRequest{
			Request:          "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
			TaskType:         "administrative",
			RiskLevel:        "low",
			NeedsTools:       true,
			ExecuteRequested: true,
		},
		time.Date(2026, time.July, 30, 9, 32, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if decision.RequiresApproval {
		t.Fatalf("prohibited external action incorrectly required approval: %v", decision.ApprovalReasons)
	}
	if selectedID(decision, "approval-control") {
		t.Fatalf("prohibited external action incorrectly selected approval control: %v", selectedIDs(decision))
	}
	if decision.MaximumAutonomyLevel != reversibleAutomaticExecutionAutonomyLevel {
		t.Fatalf("MaximumAutonomyLevel = %d, want %d", decision.MaximumAutonomyLevel, reversibleAutomaticExecutionAutonomyLevel)
	}
}

func TestContainsRequestedConsequentialActionHonorsDirectSafetyConstraints(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want bool
	}{
		{name: "requested", text: "Send the verified summary to the client.", want: true},
		{name: "do not send", text: "Do not send anything externally.", want: false},
		{name: "must not publish", text: "The system must not publish this draft.", want: false},
		{name: "never delete", text: "Never delete source evidence.", want: false},
		{name: "prohibition followed by a request", text: "Do not send the draft; publish the approved notice.", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := containsRequestedConsequentialAction(test.text, []string{"send", "publish", "delete"}); got != test.want {
				t.Fatalf("containsRequestedConsequentialAction(%q) = %t, want %t", test.text, got, test.want)
			}
		})
	}
}

func TestBuildSelectionOmitsOptionalFrameworkBelowTaskRisk(t *testing.T) {
	catalog := testCatalog(t)
	for index := range catalog {
		if catalog[index].ID == "formal-planning" {
			catalog[index].RiskCeiling = "low"
		}
	}

	decision, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:   "Create a dependency plan and schedule for a multi-step project.",
			TaskType:  "planning",
			RiskLevel: "medium",
		},
		time.Date(2026, time.July, 30, 9, 35, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if selectedID(decision, "formal-planning") {
		t.Fatalf("optional framework below task risk was selected: %v", selectedIDs(decision))
	}
}

func TestBuildSelectionAcceptsFrameworkAtExactRiskCeiling(t *testing.T) {
	catalog := testCatalog(t)
	for index := range catalog {
		if catalog[index].ID == "formal-planning" {
			catalog[index].RiskCeiling = "medium"
		}
	}

	decision, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:   "Create a dependency plan and schedule for a multi-step project.",
			TaskType:  "planning",
			RiskLevel: "medium",
		},
		time.Date(2026, time.July, 30, 9, 36, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if !selectedID(decision, "formal-planning") {
		t.Fatalf("framework at exact task risk ceiling was omitted: %v", selectedIDs(decision))
	}
}

func TestBuildSelectionFailsClosedWhenMandatoryFrameworkIsBelowTaskRisk(t *testing.T) {
	catalog := testCatalog(t)
	for index := range catalog {
		if catalog[index].ID == "human-sovereignty" {
			catalog[index].RiskCeiling = "low"
		}
	}

	_, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:   "Assess a high-risk operational request.",
			RiskLevel: "high",
		},
		time.Date(2026, time.July, 30, 9, 37, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("BuildSelection accepted a mandatory framework below the task risk")
	}
}

func TestBuildSelectionFailsClosedForInvalidOrMissingRiskCeiling(t *testing.T) {
	for _, test := range []struct {
		name        string
		riskCeiling string
	}{
		{name: "missing", riskCeiling: ""},
		{name: "invalid", riskCeiling: "critical"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := testCatalog(t)
			for index := range catalog {
				if catalog[index].ID == "human-sovereignty" {
					catalog[index].RiskCeiling = test.riskCeiling
				}
			}

			_, err := BuildSelection(
				catalog,
				testConstitution(),
				SelectionRequest{
					Request:   "Assess an ordinary low-risk request.",
					RiskLevel: "low",
				},
				time.Date(2026, time.July, 30, 9, 38, 0, 0, time.UTC),
			)
			if err == nil {
				t.Fatalf("BuildSelection accepted %s risk ceiling %q", test.name, test.riskCeiling)
			}
		})
	}
}

func TestSelectSmallestCapableCombinationFindsExactMinimum(t *testing.T) {
	required := selectionCandidate{
		view: FrameworkView{
			Framework: Framework{ID: "required-policy", RiskCeiling: "high"},
			Enabled:   true,
		},
		required: true,
	}
	wideGreedyChoice := selectionCandidate{
		view:     FrameworkView{Framework: Framework{ID: "a-wide-greedy-choice", RiskCeiling: "high"}, Enabled: true},
		score:    100,
		coverage: testCoverage("one", "two", "three", "four"),
	}
	firstExactChoice := selectionCandidate{
		view:     FrameworkView{Framework: Framework{ID: "b-first-exact-choice", RiskCeiling: "high"}, Enabled: true},
		score:    10,
		coverage: testCoverage("one", "two", "five"),
	}
	secondExactChoice := selectionCandidate{
		view:     FrameworkView{Framework: Framework{ID: "c-second-exact-choice", RiskCeiling: "high"}, Enabled: true},
		score:    10,
		coverage: testCoverage("three", "four", "six"),
	}

	selected, conflicts, err := selectSmallestCapableCombination([]selectionCandidate{
		required,
		wideGreedyChoice,
		firstExactChoice,
		secondExactChoice,
	})
	if err != nil {
		t.Fatalf("selectSmallestCapableCombination returned error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	if got := candidateIDs(selected); !reflect.DeepEqual(got, []string{
		"required-policy",
		"b-first-exact-choice",
		"c-second-exact-choice",
	}) {
		t.Fatalf("selected %v, want exact two-framework cover after required policy", got)
	}
}

func TestSelectSmallestCapableCombinationUsesDeterministicTieBreak(t *testing.T) {
	candidates := []selectionCandidate{
		{
			view:     FrameworkView{Framework: Framework{ID: "alpha", RiskCeiling: "high"}, Enabled: true},
			score:    5,
			coverage: testCoverage("capability"),
		},
		{
			view:     FrameworkView{Framework: Framework{ID: "beta", RiskCeiling: "high"}, Enabled: true},
			score:    5,
			coverage: testCoverage("capability"),
		},
	}

	first, _, err := selectSmallestCapableCombination(candidates)
	if err != nil {
		t.Fatalf("first selection returned error: %v", err)
	}
	second, _, err := selectSmallestCapableCombination(candidates)
	if err != nil {
		t.Fatalf("second selection returned error: %v", err)
	}
	if got := candidateIDs(first); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("first selection = %v, want [alpha]", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equal minimum covers were not deterministic: first=%#v second=%#v", first, second)
	}
}

func TestSelectSmallestCapableCombinationRejectsConflictingShortcut(t *testing.T) {
	required := selectionCandidate{
		view: FrameworkView{
			Framework: Framework{ID: "required-policy", RiskCeiling: "high"},
			Enabled:   true,
		},
		required: true,
	}
	conflictingShortcut := selectionCandidate{
		view: FrameworkView{
			Framework: Framework{
				ID:            "conflicting-shortcut",
				RiskCeiling:   "high",
				ConflictsWith: []string{"required-policy"},
			},
			Enabled: true,
		},
		score:    100,
		coverage: testCoverage("one", "two"),
	}
	firstSafeChoice := selectionCandidate{
		view:     FrameworkView{Framework: Framework{ID: "first-safe-choice", RiskCeiling: "high"}, Enabled: true},
		score:    10,
		coverage: testCoverage("one"),
	}
	secondSafeChoice := selectionCandidate{
		view:     FrameworkView{Framework: Framework{ID: "second-safe-choice", RiskCeiling: "high"}, Enabled: true},
		score:    10,
		coverage: testCoverage("two"),
	}

	selected, conflicts, err := selectSmallestCapableCombination([]selectionCandidate{
		required,
		conflictingShortcut,
		firstSafeChoice,
		secondSafeChoice,
	})
	if err != nil {
		t.Fatalf("selectSmallestCapableCombination returned error: %v", err)
	}
	if got := candidateIDs(selected); !reflect.DeepEqual(got, []string{
		"required-policy",
		"first-safe-choice",
		"second-safe-choice",
	}) {
		t.Fatalf("selected %v, want safe non-conflicting cover", got)
	}
	if len(conflicts) != 1 ||
		conflicts[0].SelectedID != "required-policy" ||
		conflicts[0].SkippedID != "conflicting-shortcut" {
		t.Fatalf("conflicting shortcut was not audited: %#v", conflicts)
	}
}

func TestBuildSelectionConstitutionCeilingCannotAuthorizePlanning(t *testing.T) {
	constitution := testConstitution()
	constitution.Prohibitions = []string{"HAI-RULE v1 authority-ceiling level=3"}

	decision, err := BuildSelection(
		testCatalog(t),
		constitution,
		SelectionRequest{
			Request:   "Plan a dependency-aware internal project.",
			TaskType:  "planning",
			RiskLevel: "low",
		},
		time.Date(2026, time.July, 30, 9, 45, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if decision.MaximumAutonomyLevel != 3 {
		t.Fatalf("MaximumAutonomyLevel = %d, want Constitution ceiling 3", decision.MaximumAutonomyLevel)
	}
	if !strings.Contains(decision.AuthoritySummary, "requires autonomy level 4/10") {
		t.Fatalf("authority summary does not expose the unmet planning level: %q", decision.AuthoritySummary)
	}
}

func TestBuildSelectionApprovalCannotRaiseExecutionFrameworkCeiling(t *testing.T) {
	catalog := testCatalog(t)
	for index := range catalog {
		if catalog[index].ID == "reliable-execution" {
			catalog[index].EffectiveAutonomyLevel = 5
		}
	}
	request := SelectionRequest{
		Request:             "Run the approved local checksum tool and verify its output.",
		TaskType:            "controlled execution",
		RiskLevel:           "medium",
		NeedsTools:          true,
		NeedsLocalExecution: true,
		NeedsApproval:       true,
		ExecuteRequested:    true,
		HumanApproved:       true,
	}

	approved, err := BuildSelection(
		catalog,
		testConstitution(),
		request,
		time.Date(2026, time.July, 30, 9, 50, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("approved BuildSelection returned error: %v", err)
	}
	request.HumanApproved = false
	unapproved, err := BuildSelection(
		catalog,
		testConstitution(),
		request,
		time.Date(2026, time.July, 30, 9, 50, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("unapproved BuildSelection returned error: %v", err)
	}
	if approved.MaximumAutonomyLevel != 5 || unapproved.MaximumAutonomyLevel != 5 {
		t.Fatalf(
			"approval raised framework ceiling: approved=%d unapproved=%d, want both 5",
			approved.MaximumAutonomyLevel,
			unapproved.MaximumAutonomyLevel,
		)
	}
}

func TestBuildSelectionHonorsDisabledFramework(t *testing.T) {
	catalog := testCatalog(t)
	for index := range catalog {
		if catalog[index].ID == "formal-planning" {
			catalog[index].Enabled = false
		}
	}

	decision, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:   "Create a dependency plan and schedule for a multi-step project.",
			TaskType:  "planning",
			RiskLevel: "low",
		},
		time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if selectedID(decision, "formal-planning") {
		t.Fatalf("disabled framework was selected: %v", selectedIDs(decision))
	}
}

func TestBuildSelectionExperimentalRequiresExplicitRelevance(t *testing.T) {
	catalog := testCatalog(t)
	now := time.Date(2026, time.July, 30, 11, 0, 0, 0, time.UTC)

	ordinary, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:   "Plan the next internal project milestone.",
			TaskType:  "planning",
			RiskLevel: "low",
		},
		now,
	)
	if err != nil {
		t.Fatalf("ordinary BuildSelection returned error: %v", err)
	}
	if selectedID(ordinary, "agent-development-adapters") {
		t.Fatalf("unrelated experimental framework was selected: %v", selectedIDs(ordinary))
	}

	explicit, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{
			Request:           "Evaluate a Microsoft AutoGen agent framework adapter without installing or executing it.",
			TaskType:          "agent framework integration",
			RiskLevel:         "low",
			RequiredReasoning: "architecture comparison",
		},
		now,
	)
	if err != nil {
		t.Fatalf("explicit BuildSelection returned error: %v", err)
	}
	if !selectedID(explicit, "agent-development-adapters") {
		t.Fatalf("explicitly relevant experimental framework was not selected: %v", selectedIDs(explicit))
	}
}

func TestBuildSelectionIsDeterministic(t *testing.T) {
	catalog := testCatalog(t)
	request := SelectionRequest{
		OwnerIdentity:     "robert",
		TaskPlanID:        "deterministic-1",
		Request:           "Research current source documents and plan a verified client response.",
		ProjectKey:        "client-project",
		TaskType:          "research and planning",
		RiskLevel:         "medium",
		Difficulty:        7,
		RequiredReasoning: "compare evidence",
		SuccessCriteria:   []string{"claims have current sources"},
		NeedsMemory:       true,
		NeedsDocuments:    true,
		NeedsWebAccess:    true,
	}
	now := time.Date(2026, time.July, 30, 12, 0, 0, 123, time.FixedZone("CEST", 2*60*60))

	first, err := BuildSelection(catalog, testConstitution(), request, now)
	if err != nil {
		t.Fatalf("first BuildSelection returned error: %v", err)
	}
	second, err := BuildSelection(catalog, testConstitution(), request, now)
	if err != nil {
		t.Fatalf("second BuildSelection returned error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("selection is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if _, err := uuid.Parse(first.ID); err != nil {
		t.Fatalf("deterministic selection ID %q is not a UUID: %v", first.ID, err)
	}
	if first.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt location = %v, want UTC", first.CreatedAt.Location())
	}
}

func TestBuildSelectionResolvesConflictsByScoreThenID(t *testing.T) {
	catalog := testCatalog(t)
	catalog = append(catalog,
		FrameworkView{
			Framework: Framework{
				ID:                   "alpha-option",
				Version:              "1.0.0",
				Name:                 "Alpha option",
				Family:               "test",
				SuitableProblemTypes: []string{"choice analysis"},
				TriggerConditions:    []string{"choose option"},
				RequiredInputs:       []string{"request"},
				RequiredAgents:       []string{"alpha_agent"},
				AuthorityRequirement: "recommend",
				RiskCeiling:          "low",
				EvidenceRequirements: []string{"alpha evidence"},
				EvaluationMethod:     []string{"alpha evaluation"},
				ConflictsWith:        []string{"beta-option"},
				Status:               StatusActive,
			},
			EffectiveStatus:        StatusActive,
			Enabled:                true,
			EffectiveAutonomyLevel: 4,
		},
		FrameworkView{
			Framework: Framework{
				ID:                   "beta-option",
				Version:              "1.0.0",
				Name:                 "Beta option",
				Family:               "test",
				SuitableProblemTypes: []string{"choice analysis"},
				TriggerConditions:    []string{"choose option"},
				RequiredInputs:       []string{"request"},
				RequiredAgents:       []string{"beta_agent"},
				AuthorityRequirement: "recommend",
				RiskCeiling:          "low",
				EvidenceRequirements: []string{"beta evidence"},
				EvaluationMethod:     []string{"beta evaluation"},
				Status:               StatusActive,
			},
			EffectiveStatus:        StatusActive,
			Enabled:                true,
			EffectiveAutonomyLevel: 4,
		},
	)

	decision, err := BuildSelection(
		catalog,
		testConstitution(),
		SelectionRequest{Request: "Choose option using a choice analysis.", RiskLevel: "low"},
		time.Date(2026, time.July, 30, 13, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSelection returned error: %v", err)
	}
	if !selectedID(decision, "alpha-option") || selectedID(decision, "beta-option") {
		t.Fatalf("conflict tie was not resolved by ID: selected=%v conflicts=%v", selectedIDs(decision), decision.Conflicts)
	}
	if len(decision.Conflicts) != 1 || decision.Conflicts[0].SelectedID != "alpha-option" ||
		decision.Conflicts[0].SkippedID != "beta-option" {
		t.Fatalf("unexpected conflict record: %#v", decision.Conflicts)
	}
}

func testCatalog(t *testing.T) []FrameworkView {
	t.Helper()
	frameworks := BuiltinCatalog()
	if err := ValidateCatalog(frameworks); err != nil {
		t.Fatalf("builtin catalog is invalid: %v", err)
	}
	result := make([]FrameworkView, 0, len(frameworks))
	for _, framework := range frameworks {
		result = append(result, FrameworkView{
			Framework:              framework,
			EffectiveStatus:        framework.Status,
			Enabled:                framework.Status != StatusDeprecated,
			EffectiveAutonomyLevel: framework.MaximumAutonomyLevel,
		})
	}
	return result
}

func testConstitution() Constitution {
	return Constitution{
		ID:             "robert-constitution",
		Version:        3,
		BaseVersion:    2,
		Status:         ConstitutionActive,
		Values:         []string{"human sovereignty", "verified completion"},
		ProtectedRules: []string{"HAI cannot grant itself authority"},
	}
}

func selectedID(decision SelectionDecision, id string) bool {
	for _, framework := range decision.Selected {
		if framework.ID == id {
			return true
		}
	}
	return false
}

func selectedIDs(decision SelectionDecision) []string {
	result := make([]string, 0, len(decision.Selected))
	for _, framework := range decision.Selected {
		result = append(result, framework.ID)
	}
	return result
}

func containsStringFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func testCoverage(keys ...string) []frameworkCoverage {
	result := make([]frameworkCoverage, 0, len(keys))
	for _, key := range keys {
		result = append(result, frameworkCoverage{key: key, reason: "covers " + key})
	}
	return result
}

func candidateIDs(candidates []selectionCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.view.ID)
	}
	return result
}

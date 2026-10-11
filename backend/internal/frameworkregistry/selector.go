package frameworkregistry

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const maxSelectedFrameworks = 16

var requiredFrameworkOrder = []string{
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
}

type selectionCandidate struct {
	view     FrameworkView
	score    float64
	reasons  []string
	coverage []frameworkCoverage
	required bool
	order    int
}

type frameworkCoverage struct {
	key    string
	reason string
}

type domainRule struct {
	name     string
	signals  []string
	need     string
	boostIDs []string
}

var domainRules = []domainRule{
	{
		name:     "legal_government",
		signals:  []string{"legal", "lawyer", "court", "hearing", "government", "municipality", "gemeente", "authority", "insurance", "insurer", "case", "dispute", "appeal", "objection"},
		need:     "legal or government obligation",
		boostIDs: []string{"legal-government-case", "truth-evidence", "communication"},
	},
	{
		name:     "emergency_continuity",
		signals:  []string{"emergency", "urgent danger", "incapacity", "continuity", "disaster", "account recovery", "critical backup"},
		need:     "safety or continuity need",
		boostIDs: []string{"emergency-continuity", "safety-engineering", "reliability-resilience"},
	},
	{
		name:     "health_wellbeing",
		signals:  []string{"health", "doctor", "medical", "medication", "therapy", "symptom", "sleep", "wellbeing", "stress", "care plan"},
		need:     "health or wellbeing need",
		boostIDs: []string{"health-personal-care", "needs-wellbeing", "capacity-state"},
	},
	{
		name:     "financial",
		signals:  []string{"finance", "financial", "money", "budget", "invoice", "payment", "bank", "tax", "debt", "cash flow", "price", "purchase"},
		need:     "financial stability commitment",
		boostIDs: []string{"financial-management", "truth-evidence", "approval-control"},
	},
	{
		name:     "work_venture",
		signals:  []string{"work", "client", "customer", "job", "service", "business", "venture", "startup", "developer", "software", "code", "github", "product", "deliverable"},
		need:     "work or venture commitment",
		boostIDs: []string{"work-service-delivery", "entrepreneurship-venture", "goal-hierarchy"},
	},
	{
		name:     "home_assets",
		signals:  []string{"home", "house", "garden", "maintenance", "repair", "warranty", "asset", "vehicle", "contractor", "household"},
		need:     "home or asset commitment",
		boostIDs: []string{"home-garden-assets", "formal-planning"},
	},
	{
		name:     "relationships_care",
		signals:  []string{"relationship", "family", "partner", "friend", "child", "parent", "caregiver", "social", "conflict with"},
		need:     "relationship or care need",
		boostIDs: []string{"relationships-care", "communication", "needs-wellbeing"},
	},
	{
		name:     "learning_growth",
		signals:  []string{"learn", "study", "course", "training", "skill", "competence", "research topic", "career development"},
		need:     "learning or competence need",
		boostIDs: []string{"learning-competence", "goal-hierarchy", "retrieval-context"},
	},
	{
		name:     "travel_mobility",
		signals:  []string{"travel", "trip", "flight", "train", "hotel", "route", "itinerary", "visa", "transport"},
		need:     "travel or mobility commitment",
		boostIDs: []string{"travel-mobility", "formal-planning"},
	},
	{
		name:     "personal_productivity",
		signals:  []string{"todo", "task list", "focus", "weekly review", "procrastinate", "habit", "routine", "calendar block", "personal goal"},
		need:     "personal productivity commitment",
		boostIDs: []string{"productivity-attention", "habit-behavior-change", "capacity-state"},
	},
	{
		name:     "identity_roles",
		signals:  []string{"identity document", "passport", "personal role", "profile", "biography", "who i am"},
		need:     "identity or role commitment",
		boostIDs: []string{"whole-life-ontology", "human-sovereignty"},
	},
	{
		name:     "family_household",
		signals:  []string{"household member", "family plan", "family appointment", "family responsibility", "household schedule"},
		need:     "family or household commitment",
		boostIDs: []string{"relationships-care", "whole-life-ontology"},
	},
	{
		name:     "food_nutrition",
		signals:  []string{"food", "nutrition", "meal", "groceries", "diet", "cook", "recipe"},
		need:     "food or nutrition need",
		boostIDs: []string{"health-personal-care", "needs-wellbeing"},
	},
	{
		name:     "communication_correspondence",
		signals:  []string{"email", "letter", "message", "reply", "correspondence", "inbox", "draft response"},
		need:     "communication or correspondence commitment",
		boostIDs: []string{"communication", "truth-evidence"},
	},
	{
		name:     "digital_accounts",
		signals:  []string{"online account", "login", "password reset", "oauth", "digital profile", "cloud account", "subscription"},
		need:     "digital account or access commitment",
		boostIDs: []string{"security-zero-trust", "privacy-protection"},
	},
	{
		name:     "possessions_inventory",
		signals:  []string{"inventory", "possession", "equipment", "tool list", "serial number", "storage box"},
		need:     "possession, equipment, or inventory commitment",
		boostIDs: []string{"home-garden-assets", "whole-life-ontology"},
	},
	{
		name:     "animals_dependants",
		signals:  []string{"pet", "animal", "dog", "cat", "veterinarian", "dependant care"},
		need:     "animal or dependant-care commitment",
		boostIDs: []string{"relationships-care", "formal-planning"},
	},
	{
		name:     "community_civic",
		signals:  []string{"community", "neighbourhood", "politics", "election", "volunteer", "civic", "public consultation"},
		need:     "community or civic commitment",
		boostIDs: []string{"whole-life-ontology", "communication"},
	},
	{
		name:     "leisure_recreation",
		signals:  []string{"leisure", "recreation", "hobby", "game night", "day out", "vacation activity"},
		need:     "leisure or recovery need",
		boostIDs: []string{"needs-wellbeing", "formal-planning"},
	},
	{
		name:     "creativity_expression",
		signals:  []string{"creative", "art", "music", "write a story", "photography", "design project"},
		need:     "creative-expression commitment",
		boostIDs: []string{"goal-hierarchy", "learning-competence"},
	},
	{
		name:     "meaning_values",
		signals:  []string{"meaning", "purpose in life", "spiritual", "values reflection", "legacy value"},
		need:     "meaning, values, or spiritual need",
		boostIDs: []string{"human-sovereignty", "needs-wellbeing"},
	},
	{
		name:     "environment_sustainability",
		signals:  []string{"environment", "sustainability", "energy use", "recycling", "carbon", "biodiversity"},
		need:     "environmental or sustainability commitment",
		boostIDs: []string{"whole-life-ontology", "formal-planning"},
	},
	{
		name:     "legacy_long_term",
		signals:  []string{"legacy", "estate plan", "long-term archive", "succession", "future generations"},
		need:     "long-term legacy commitment",
		boostIDs: []string{"goal-hierarchy", "truth-evidence"},
	},
	{
		name:     "safety_security",
		signals:  []string{"personal safety", "security incident", "burglary", "threat", "unsafe", "protective measure"},
		need:     "safety or security need",
		boostIDs: []string{"safety-engineering", "security-zero-trust", "emergency-continuity"},
	},
}

// BuildSelection returns a deterministic, auditable framework recommendation.
// It selects decision disciplines only; it does not grant authority or execute
// tools. Reasons expose matched policy and task signals, not hidden reasoning.
func BuildSelection(catalog []FrameworkView, constitution Constitution, request SelectionRequest, now time.Time) (SelectionDecision, error) {
	if strings.TrimSpace(request.Request) == "" {
		return SelectionDecision{}, fmt.Errorf("selection request is required")
	}
	if len(catalog) == 0 {
		return SelectionDecision{}, fmt.Errorf("framework catalog is required")
	}
	request.SuccessCriteria = redactContractStrings(request.SuccessCriteria)
	effectiveConstitution, err := compileEffectiveConstitutionRules(constitution)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("compile active Constitution rules: %w", err)
	}

	views, err := indexSelectableFrameworks(catalog)
	if err != nil {
		return SelectionDecision{}, err
	}

	taskText := selectionTaskText(request)
	lifeDomains := classifyLifeDomains(taskText)
	lifeDomain, domainNeed, domainBoosts := classifyLifeDomain(taskText)
	needOrCommitment := classifyNeedOrCommitment(request, taskText, domainNeed)
	highRisk := isHighRisk(request, taskText, lifeDomain)
	effectiveRisk := effectiveSelectionRisk(request, highRisk)
	requiresApproval, approvalReasons := approvalRequirement(request, taskText, highRisk)
	capabilities := requestedConstitutionCapabilities(request, taskText, lifeDomain, highRisk)
	requiresApproval, approvalReasons, err = applyEffectiveConstitutionRules(
		effectiveConstitution,
		capabilities,
		requiresApproval,
		approvalReasons,
	)
	if err != nil {
		return SelectionDecision{}, err
	}
	requiresTruth := requiresTruthEvidence(request, taskText)
	requiresPrivacySecurity := requiresPrivacyOrSecurity(request, taskText, lifeDomain)
	requiresExecutionControl := request.NeedsTools || request.NeedsLocalExecution || request.ExecuteRequested
	requiresThreatModeling := requiresUntrustedContentControls(request, taskText)

	required := map[string]string{
		"human-sovereignty": "required policy: operator sovereignty applies to every task",
		"intake-triage":     "required policy: every request must be classified and risk-triaged",
		"evaluation":        "required policy: every task needs explicit, verifiable completion checks",
	}
	if requiresApproval {
		required["approval-control"] = "required policy: approval or high-risk work must remain approval-controlled"
	}
	if requiresTruth {
		required["truth-evidence"] = "required policy: factual, document, or web work must be evidence-grounded"
	}
	if requiresPrivacySecurity {
		required["privacy-protection"] = "required policy: sensitive or tool-mediated work must minimize and protect data"
		required["security-zero-trust"] = "required policy: sensitive or tool-mediated work must use least privilege"
	}
	if requiresExecutionControl {
		required["autonomy-levels"] = "required policy: tool or runtime work needs an explicit effective autonomy ceiling"
		required["reliable-execution"] = "required policy: tool or runtime work needs bounded, verifiable execution controls"
	}
	if requiresThreatModeling {
		required["agent-threat-modeling"] = "required policy: untrusted document, web, or tool content must be threat-modelled"
	}

	if len(required) > maxSelectedFrameworks {
		return SelectionDecision{}, fmt.Errorf("required framework count %d exceeds selection limit %d", len(required), maxSelectedFrameworks)
	}

	requiredRank := make(map[string]int, len(requiredFrameworkOrder))
	for index, id := range requiredFrameworkOrder {
		requiredRank[id] = index
	}
	for id := range required {
		view, ok := views[id]
		if !ok {
			return SelectionDecision{}, fmt.Errorf("required framework %q is missing, disabled, deprecated, or invalid", id)
		}
		if statusOf(view) == StatusExperimental {
			return SelectionDecision{}, fmt.Errorf("required framework %q cannot be experimental", id)
		}
		if !frameworkSupportsRisk(view, effectiveRisk) {
			return SelectionDecision{}, fmt.Errorf(
				"required framework %q risk ceiling %q is below task risk %q",
				id,
				view.RiskCeiling,
				effectiveRisk,
			)
		}
	}

	candidates := make([]selectionCandidate, 0, len(views))
	for _, view := range views {
		if !frameworkSupportsRisk(view, effectiveRisk) {
			continue
		}
		status := statusOf(view)
		score, reasons := scoreFramework(view, taskText, request, lifeDomain, domainBoosts)
		requiredReason, isRequired := required[view.ID]
		if isRequired {
			score += 100
			reasons = append([]string{requiredReason}, reasons...)
		}
		if status == StatusExperimental && !isRequired && !explicitExperimentalMatch(view, taskText) {
			continue
		}
		if !isRequired && score <= 0 {
			continue
		}

		order := len(requiredFrameworkOrder)
		if rank, ok := requiredRank[view.ID]; ok {
			order = rank
		}
		candidates = append(candidates, selectionCandidate{
			view:     view,
			score:    score,
			reasons:  conciseReasons(reasons),
			coverage: optionalFrameworkCoverage(view, taskText, request, lifeDomain, domainBoosts, isRequired),
			required: isRequired,
			order:    order,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidatePrecedes(candidates[i], candidates[j])
	})

	selectedCandidates, conflicts, err := selectSmallestCapableCombination(candidates)
	if err != nil {
		return SelectionDecision{}, err
	}
	if len(selectedCandidates) == 0 {
		return SelectionDecision{}, fmt.Errorf("no enabled frameworks were suitable")
	}
	effectiveRiskCeiling, err := selectedRiskCeiling(selectedCandidates)
	if err != nil {
		return SelectionDecision{}, err
	}

	selected := make([]SelectedFramework, 0, len(selectedCandidates))
	requiredAgents := make([]string, 0)
	evidenceRequirements := make([]string, 0)
	completionCriteria := append([]string(nil), request.SuccessCriteria...)
	contextRequirements := make([]string, 0)
	for _, candidate := range selectedCandidates {
		view := candidate.view
		selected = append(selected, SelectedFramework{
			ID:                   view.ID,
			Version:              view.Version,
			Name:                 view.Name,
			Family:               view.Family,
			RiskCeiling:          normalizedRiskLevel(view.RiskCeiling),
			Score:                candidate.score,
			Reasons:              append([]string(nil), candidate.reasons...),
			MaximumAutonomyLevel: view.EffectiveAutonomyLevel,
			AuthorityRequirement: view.AuthorityRequirement,
			EvidenceRequirements: sortedUnique(view.EvidenceRequirements),
			EvaluationMethod:     sortedUnique(view.EvaluationMethod),
		})
		requiredAgents = append(requiredAgents, view.RequiredAgents...)
		evidenceRequirements = append(evidenceRequirements, view.EvidenceRequirements...)
		completionCriteria = append(completionCriteria, view.EvaluationMethod...)
		contextRequirements = append(contextRequirements, view.RequiredInputs...)
	}
	requiredAutonomy := requiredAutonomyLevel(request)
	maximumAutonomy := effectiveAuthorityCeiling(
		selectedCandidates,
		request,
		effectiveConstitution.AuthorityCeiling,
		requiredAutonomy,
	)

	contextRequirements = append(contextRequirements, explicitContextRequirements(request)...)
	evidenceRequirements = append(evidenceRequirements, "active Constitution and applicable authority decision")
	completionCriteria = append(completionCriteria,
		"all consequential claims and actions satisfy the selected evidence requirements",
		"the verified result meets the explicit success criteria before completion is recorded",
	)
	learningPlan := []string{
		"record the verified outcome against the explicit success criteria",
		"retain only source-supported or operator-confirmed lessons with provenance",
		"review framework fit, exceptions, and corrections without changing policy or authority automatically",
	}

	requiredAgents = sortedUnique(requiredAgents)
	evidenceRequirements = sortedUnique(evidenceRequirements)
	completionCriteria = sortedUnique(completionCriteria)
	contextRequirements = sortedUnique(contextRequirements)
	approvalReasons = sortedUnique(approvalReasons)

	createdAt := now.UTC()
	operating, err := buildOperatingContract(
		request,
		effectiveRisk,
		effectiveRiskCeiling,
		lifeDomains,
		requiredAgents,
		maximumAutonomy,
		requiresApproval,
		approvalReasons,
		evidenceRequirements,
		completionCriteria,
		contextRequirements,
		createdAt,
	)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("build chief-of-staff operating contract: %w", err)
	}
	decision := SelectionDecision{
		TaskPlanID:           strings.TrimSpace(request.TaskPlanID),
		CreatedAt:            createdAt,
		LifeDomain:           lifeDomain,
		NeedOrCommitment:     needOrCommitment,
		TaskRiskLevel:        effectiveRisk,
		EffectiveRiskCeiling: effectiveRiskCeiling,
		Selected:             selected,
		Conflicts:            conflicts,
		RequiredAgents:       requiredAgents,
		MaximumAutonomyLevel: maximumAutonomy,
		AuthoritySummary: fmt.Sprintf(
			"Requested operation requires autonomy level %d/10; its least-authority ceiling is %d/10 after applicable framework and Constitution limits. Selection and approval do not raise a framework ceiling or grant authority; tool, evidence, risk, and runtime controls remain binding.",
			requiredAutonomy,
			maximumAutonomy,
		),
		RequiresApproval:     requiresApproval,
		ApprovalReasons:      approvalReasons,
		EvidenceRequirements: evidenceRequirements,
		CompletionCriteria:   completionCriteria,
		LearningPlan:         learningPlan,
		ContextRequirements:  contextRequirements,
		SelectionReason: fmt.Sprintf(
			"Selected %d enabled frameworks for %s and %s at %s task risk; mandatory policies were retained and optional frameworks form the deterministic smallest non-conflicting risk-compatible capability cover.",
			len(selected), lifeDomain, needOrCommitment, effectiveRisk,
		),
		ConstitutionVersion:     constitution.Version,
		ConstitutionSource:      constitutionSource(constitution),
		LifeDomains:             operating.LifeDomains,
		NeedsState:              operating.NeedsState,
		Capacity:                operating.Capacity,
		AgentCards:              operating.AgentCards,
		Delegations:             operating.Delegations,
		Communication:           operating.Communication,
		Coordination:            operating.Coordination,
		ActionAutonomy:          operating.ActionAutonomy,
		StopConditions:          operating.StopConditions,
		OutcomeMonitoring:       operating.OutcomeMonitoring,
		ChiefOfStaff:            operating.ChiefOfStaff,
		OperatingContractDigest: operating.Digest,
	}
	decision.ID = deterministicSelectionID(decision, request)
	return decision, nil
}

func indexSelectableFrameworks(catalog []FrameworkView) (map[string]FrameworkView, error) {
	result := make(map[string]FrameworkView, len(catalog))
	for _, view := range catalog {
		view.ID = strings.TrimSpace(view.ID)
		if view.ID == "" {
			return nil, fmt.Errorf("framework id is required")
		}
		if _, exists := result[view.ID]; exists {
			return nil, fmt.Errorf("duplicate framework id %q", view.ID)
		}
		status := statusOf(view)
		if status != StatusActive && status != StatusExperimental && status != StatusDeprecated {
			return nil, fmt.Errorf("framework %q has invalid effective status %q", view.ID, status)
		}
		if !view.Enabled || status == StatusDeprecated {
			continue
		}
		if view.EffectiveAutonomyLevel < 0 || view.EffectiveAutonomyLevel > 10 {
			return nil, fmt.Errorf("framework %q has invalid effective autonomy level %d", view.ID, view.EffectiveAutonomyLevel)
		}
		if _, ok := selectionRiskRank(view.RiskCeiling); !ok {
			return nil, fmt.Errorf("framework %q has invalid risk ceiling %q", view.ID, view.RiskCeiling)
		}
		result[view.ID] = view
	}
	return result, nil
}

func statusOf(view FrameworkView) string {
	status := strings.ToLower(strings.TrimSpace(view.EffectiveStatus))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(view.Status))
	}
	if status == "" {
		return StatusActive
	}
	return status
}

func selectionTaskText(request SelectionRequest) string {
	values := []string{
		request.Request,
		request.ProjectKey,
		request.PursuitID,
		request.TaskType,
		request.RiskLevel,
		request.RequiredReasoning,
		strings.Join(request.SuccessCriteria, " "),
	}
	return normalizeText(strings.Join(values, " "))
}

func classifyLifeDomain(text string) (string, string, map[string]float64) {
	assignments := classifyLifeDomains(text)
	primary := assignments[0]
	boosts := map[string]float64{}
	for _, rule := range domainRules {
		if rule.name != primary.ID {
			continue
		}
		for index, id := range rule.boostIDs {
			boosts[id] = float64(6 - index)
		}
		break
	}
	return primary.ID, primary.Need, boosts
}

func classifyNeedOrCommitment(request SelectionRequest, text, domainNeed string) string {
	switch {
	case request.NeedsApproval:
		return "approval-gated commitment"
	case containsAnyPhrase(text, []string{"deadline", "due date", "must reply", "must send", "obligation", "commitment"}):
		return "time-bound external commitment"
	case request.NeedsDocuments || request.NeedsWebAccess || containsAnyPhrase(text, []string{"verify", "evidence", "research", "factual answer"}):
		return "evidence-backed information need"
	case request.ExecuteRequested || request.NeedsLocalExecution:
		return "controlled execution commitment"
	case containsAnyPhrase(text, []string{"plan", "schedule", "organize", "prioritize", "roadmap"}):
		return "planning commitment"
	case domainNeed != "":
		return domainNeed
	default:
		return "operator request or operational commitment"
	}
}

func isHighRisk(request SelectionRequest, text, lifeDomain string) bool {
	if strings.EqualFold(strings.TrimSpace(request.RiskLevel), "high") {
		return true
	}
	if lifeDomain == "legal_government" || lifeDomain == "emergency_continuity" {
		return true
	}
	return containsAnyPhrase(text, []string{
		"send legal", "file legal", "government filing", "public accusation",
		"publish", "pay", "transfer money", "delete", "account change",
		"sign contract", "medical decision",
	})
}

func effectiveSelectionRisk(request SelectionRequest, highRisk bool) string {
	if highRisk {
		return "high"
	}
	if normalizedRiskLevel(request.RiskLevel) == "medium" {
		return "medium"
	}
	if normalizedRiskLevel(request.RiskLevel) == "low" {
		return "low"
	}
	if request.NeedsTools || request.NeedsDocuments || request.NeedsWebAccess ||
		request.NeedsLocalExecution || request.NeedsApproval || request.ExecuteRequested {
		return "medium"
	}
	return "low"
}

func frameworkSupportsRisk(view FrameworkView, taskRisk string) bool {
	ceilingRank, ceilingOK := selectionRiskRank(view.RiskCeiling)
	taskRank, taskOK := selectionRiskRank(taskRisk)
	return ceilingOK && taskOK && ceilingRank >= taskRank
}

func selectedRiskCeiling(selected []selectionCandidate) (string, error) {
	if len(selected) == 0 {
		return "", fmt.Errorf("selected frameworks are required to derive a risk ceiling")
	}
	minimumRank := 4
	minimum := ""
	for _, candidate := range selected {
		rank, ok := selectionRiskRank(candidate.view.RiskCeiling)
		if !ok {
			return "", fmt.Errorf(
				"selected framework %q has invalid risk ceiling %q",
				candidate.view.ID,
				candidate.view.RiskCeiling,
			)
		}
		if rank < minimumRank {
			minimumRank = rank
			minimum = normalizedRiskLevel(candidate.view.RiskCeiling)
		}
	}
	return minimum, nil
}

func selectionRiskRank(value string) (int, bool) {
	switch normalizedRiskLevel(value) {
	case "low":
		return 1, true
	case "medium":
		return 2, true
	case "high":
		return 3, true
	default:
		return 0, false
	}
}

func normalizedRiskLevel(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func approvalRequirement(request SelectionRequest, text string, highRisk bool) (bool, []string) {
	reasons := make([]string, 0, 4)
	if request.NeedsApproval {
		reasons = append(reasons, "the request explicitly requires human approval")
	}
	if highRisk {
		reasons = append(reasons, "the task is classified as high risk")
	}
	if containsRequestedConsequentialAction(text, []string{"send", "publish", "post publicly", "pay", "purchase", "delete", "file with", "sign", "account change"}) {
		reasons = append(reasons, "the requested side effect can create an external, financial, public, account, or destructive consequence")
	}
	required := len(reasons) > 0
	if request.HumanApproved && required {
		reasons = append(reasons, "reported human approval does not remove Constitution, scope, evidence, privacy, safety, tool, or runtime constraints")
	}
	return required, reasons
}

func requiresTruthEvidence(request SelectionRequest, text string) bool {
	if request.NeedsDocuments || request.NeedsWebAccess {
		return true
	}
	return containsAnyPhrase(text, []string{
		"fact", "factual", "verify", "evidence", "source", "citation", "claim",
		"document", "file", "pdf", "web", "research", "current information",
		"contradiction", "timeline",
	})
}

func requiresPrivacyOrSecurity(request SelectionRequest, text, lifeDomain string) bool {
	if request.NeedsTools || request.NeedsLocalExecution || request.ExecuteRequested {
		return true
	}
	if lifeDomain == "legal_government" || lifeDomain == "health_wellbeing" || lifeDomain == "financial" {
		return true
	}
	return containsAnyPhrase(text, []string{
		"sensitive", "private", "personal data", "credential", "password", "secret",
		"token", "account", "email", "contact", "medical", "bank", "identity",
	})
}

func requiresUntrustedContentControls(request SelectionRequest, text string) bool {
	if request.NeedsDocuments || request.NeedsWebAccess || request.NeedsTools {
		return true
	}
	return containsAnyPhrase(text, []string{
		"untrusted", "uploaded document", "external document", "web content",
		"tool output", "prompt injection", "source content",
	})
}

func scoreFramework(view FrameworkView, taskText string, request SelectionRequest, lifeDomain string, domainBoosts map[string]float64) (float64, []string) {
	score := 0.0
	reasons := make([]string, 0, 8)

	triggerScore, triggerMatches := scoreDescriptors(taskText, view.TriggerConditions, 4)
	score += triggerScore
	if len(triggerMatches) > 0 {
		reasons = append(reasons, "trigger match: "+strings.Join(triggerMatches, ", "))
	}
	problemScore, problemMatches := scoreDescriptors(taskText, view.SuitableProblemTypes, 6)
	score += problemScore
	if len(problemMatches) > 0 {
		reasons = append(reasons, "problem-type match: "+strings.Join(problemMatches, ", "))
	}
	if boost := domainBoosts[view.ID]; boost > 0 {
		score += boost
		reasons = append(reasons, "life-domain match: "+lifeDomain)
	}

	flagScore, flagReasons := requestSignalScore(view.ID, request)
	score += flagScore
	reasons = append(reasons, flagReasons...)

	if containsPhrase(taskText, view.ID) || containsPhrase(taskText, view.Name) {
		score += 10
		reasons = append(reasons, "framework explicitly named by the request")
	}
	if view.Pinned && score > 0 {
		score += 1
		reasons = append(reasons, "operator-pinned framework")
	}
	return score, reasons
}

func scoreDescriptors(text string, descriptors []string, weight float64) (float64, []string) {
	score := 0.0
	matches := make([]string, 0)
	for _, descriptor := range sortedUnique(descriptors) {
		normalized := normalizeText(descriptor)
		if normalized == "" || normalized == "always" || normalized == "all tasks" {
			continue
		}
		if containsPhrase(text, normalized) {
			score += weight
			matches = append(matches, descriptor)
			continue
		}
		descriptorTokens := meaningfulTokens(normalized)
		if len(descriptorTokens) < 2 {
			continue
		}
		textTokens := tokenSet(text)
		matched := 0
		for _, token := range descriptorTokens {
			if _, ok := textTokens[token]; ok {
				matched++
			}
		}
		if matched >= 2 && float64(matched)/float64(len(descriptorTokens)) >= 0.6 {
			score += weight / 2
			matches = append(matches, descriptor)
		}
	}
	return score, matches
}

func requestSignalScore(id string, request SelectionRequest) (float64, []string) {
	score := 0.0
	reasons := make([]string, 0, 4)
	add := func(value float64, reason string) {
		score += value
		reasons = append(reasons, reason)
	}
	switch id {
	case "memory-architecture":
		if request.NeedsMemory {
			add(8, "request requires memory")
		}
	case "retrieval-context":
		if request.NeedsMemory || request.NeedsDocuments || request.NeedsWebAccess {
			add(7, "request requires scoped context retrieval")
		}
	case "truth-evidence":
		if request.NeedsDocuments || request.NeedsWebAccess {
			add(8, "request requires document or web evidence")
		}
	case "reliable-execution":
		if request.NeedsTools || request.NeedsLocalExecution || request.ExecuteRequested {
			add(8, "request requires controlled execution")
		}
	case "security-zero-trust", "privacy-protection":
		if request.NeedsTools || request.NeedsLocalExecution || request.ExecuteRequested {
			add(7, "request requires a protected tool or runtime boundary")
		}
	case "formal-planning":
		if request.Difficulty >= 6 || strings.Contains(strings.ToLower(request.TaskType), "plan") {
			add(6, "task difficulty or type requires structured planning")
		}
	case "reasoning-methods":
		if request.Difficulty >= 7 || strings.TrimSpace(request.RequiredReasoning) != "" {
			add(5, "task declares substantial reasoning needs")
		}
	case "evaluation":
		if len(request.SuccessCriteria) > 0 {
			add(5, "request supplies explicit success criteria")
		}
	case "approval-control":
		if request.NeedsApproval {
			add(8, "request explicitly requires approval")
		}
	case "autonomy-levels":
		if request.ExecuteRequested || request.NeedsLocalExecution {
			add(5, "execution request requires an explicit autonomy ceiling")
		}
	}
	return score, reasons
}

func explicitExperimentalMatch(view FrameworkView, taskText string) bool {
	if containsPhrase(taskText, view.ID) || containsPhrase(taskText, view.Name) {
		return true
	}
	for _, implementation := range view.CandidateImplementations {
		if containsPhrase(taskText, implementation) {
			return true
		}
	}
	for _, descriptor := range append(append([]string(nil), view.TriggerConditions...), view.SuitableProblemTypes...) {
		normalized := normalizeText(descriptor)
		if len(meaningfulTokens(normalized)) > 0 && containsPhrase(taskText, normalized) {
			return true
		}
	}
	return false
}

func candidatePrecedes(left, right selectionCandidate) bool {
	if left.required != right.required {
		return left.required
	}
	if left.required && left.order != right.order {
		return left.order < right.order
	}
	if left.score != right.score {
		return left.score > right.score
	}
	if left.view.Pinned != right.view.Pinned {
		return left.view.Pinned
	}
	if statusOf(left.view) != statusOf(right.view) {
		return statusOf(left.view) == StatusActive
	}
	return left.view.ID < right.view.ID
}

const (
	planAndSimulateAutonomyLevel              = 4
	caseApprovedExecutionAutonomyLevel        = 6
	reversibleAutomaticExecutionAutonomyLevel = 8
)

func requiredAutonomyLevel(request SelectionRequest) int {
	if request.ExecuteRequested {
		if !request.HumanApproved && !request.NeedsApproval {
			return reversibleAutomaticExecutionAutonomyLevel
		}
		return caseApprovedExecutionAutonomyLevel
	}
	return planAndSimulateAutonomyLevel
}

// effectiveAuthorityCeiling applies least authority to the operation being
// selected. A framework's autonomy level governs actions performed through
// that framework; it is not a global cap merely because the framework
// contributes intake, evidence, privacy, or evaluation support. The active
// Constitution is always global. Planning and execution authority frameworks
// are phase-specific, and approval can never raise either ceiling.
func effectiveAuthorityCeiling(
	selected []selectionCandidate,
	request SelectionRequest,
	constitutionCeiling int,
	requiredLevel int,
) int {
	ceiling := requiredLevel
	if constitutionCeiling < ceiling {
		ceiling = constitutionCeiling
	}
	for _, candidate := range selected {
		if !frameworkCeilingAppliesToOperation(candidate.view, request) {
			continue
		}
		if candidate.view.EffectiveAutonomyLevel < ceiling {
			ceiling = candidate.view.EffectiveAutonomyLevel
		}
	}
	return ceiling
}

func frameworkCeilingAppliesToOperation(view FrameworkView, request SelectionRequest) bool {
	if !request.ExecuteRequested {
		return view.Family == "planning"
	}
	if view.Family == "execution" {
		return true
	}
	switch view.ID {
	case "autonomy-levels", "approval-control", "reliable-execution":
		return true
	}
	taskText := selectionTaskText(request)
	if view.Family == "implementation" {
		return true
	}
	return view.Family == "domain_pack" && requestsConsequentialSideEffect(taskText)
}

func requestsConsequentialSideEffect(taskText string) bool {
	return containsRequestedConsequentialAction(taskText, []string{
		"send",
		"file with",
		"submit",
		"publish",
		"post publicly",
		"pay",
		"purchase",
		"transfer money",
		"delete",
		"remove permanently",
		"sign",
		"account change",
		"book",
	})
}

func optionalFrameworkCoverage(
	view FrameworkView,
	taskText string,
	request SelectionRequest,
	lifeDomain string,
	domainBoosts map[string]float64,
	required bool,
) []frameworkCoverage {
	if required {
		return nil
	}

	coverage := make(map[string]string)
	add := func(key, reason string) {
		key = strings.TrimSpace(key)
		reason = strings.TrimSpace(reason)
		if key == "" || reason == "" {
			return
		}
		if _, exists := coverage[key]; !exists {
			coverage[key] = reason
		}
	}

	if containsPhrase(taskText, view.ID) || containsPhrase(taskText, view.Name) {
		add("explicit:"+view.ID, "the request explicitly names "+view.ID)
	}

	if boost := domainBoosts[view.ID]; boost == 6 && lifeDomain != "general_operations" {
		add("domain:"+lifeDomain, "life-domain coverage for "+lifeDomain)
	}

	switch view.ID {
	case "memory-architecture":
		if request.NeedsMemory {
			add("capability:memory", "owner-scoped memory architecture required by the request")
		}
	case "retrieval-context":
		if request.NeedsMemory || request.NeedsDocuments || request.NeedsWebAccess {
			add("capability:retrieval", "scoped context retrieval required by the request")
		}
	case "formal-planning":
		if request.Difficulty >= 6 || strings.Contains(normalizeText(request.TaskType), "plan") {
			add("capability:planning", "structured planning required by task type or difficulty")
		}
	case "reasoning-methods":
		if request.Difficulty >= 7 || strings.TrimSpace(request.RequiredReasoning) != "" {
			add("capability:reasoning", "substantial reasoning required by the request")
		}
	}

	if exactFrameworkIntentMatch(view, taskText) {
		key := "family:" + view.Family
		switch view.Family {
		case "reasoning":
			key = "capability:reasoning"
		case "planning":
			key = "capability:planning"
		case "knowledge":
			key = "capability:retrieval"
		case "interaction":
			key = "capability:communication"
		}
		add(key, "direct task-intent coverage in the "+view.Family+" framework family")
	}

	keys := make([]string, 0, len(coverage))
	for key := range coverage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]frameworkCoverage, 0, len(keys))
	for _, key := range keys {
		result = append(result, frameworkCoverage{key: key, reason: coverage[key]})
	}
	return result
}

func exactFrameworkIntentMatch(view FrameworkView, taskText string) bool {
	descriptors := append([]string(nil), view.TriggerConditions...)
	descriptors = append(descriptors, view.SuitableProblemTypes...)
	for _, descriptor := range sortedUnique(descriptors) {
		normalized := normalizeText(descriptor)
		if normalized == "" || normalized == "always" || normalized == "all tasks" ||
			!containsPhrase(taskText, normalized) {
			continue
		}
		if isConsequentialActionDescriptor(normalized) && !containsRequestedConsequentialAction(taskText, []string{normalized}) {
			continue
		}
		tokens := meaningfulTokens(normalized)
		if len(tokens) == 1 && isLifeDomainSignal(tokens[0]) {
			continue
		}
		return true
	}
	return false
}

func isConsequentialActionDescriptor(value string) bool {
	for _, phrase := range []string{
		"send", "send legal", "file with", "file legal", "submit", "publish", "post publicly", "public post", "public accusation",
		"pay", "purchase", "transfer money", "delete", "remove permanently", "destroy", "overwrite", "sign", "sign contract",
		"account change", "change account", "reset account", "close account", "government filing", "medical decision",
	} {
		if normalizeText(value) == normalizeText(phrase) {
			return true
		}
	}
	return false
}

func isLifeDomainSignal(signal string) bool {
	signal = canonicalToken(normalizeText(signal))
	for _, rule := range domainRules {
		for _, candidate := range rule.signals {
			tokens := meaningfulTokens(candidate)
			if len(tokens) == 1 && tokens[0] == signal {
				return true
			}
		}
	}
	return false
}

// selectSmallestCapableCombination retains every mandatory policy overlay and
// finds the exact smallest non-conflicting optional set that covers every
// material task capability. Candidate precedence is used only to choose
// deterministically among equally small capable sets.
func selectSmallestCapableCombination(candidates []selectionCandidate) ([]selectionCandidate, []FrameworkConflict, error) {
	selected := make([]selectionCandidate, 0, maxSelectedFrameworks)
	optional := make([]selectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.required {
			optional = append(optional, candidate)
			continue
		}
		if existing, ok := firstConflictingFramework(candidate, selected); ok {
			if existing.required {
				return nil, nil, fmt.Errorf("required frameworks %q and %q declare a conflict", existing.view.ID, candidate.view.ID)
			}
		}
		selected = append(selected, candidate)
	}

	requiredCapabilities := make(map[string]string)
	for _, candidate := range optional {
		for _, capability := range candidate.coverage {
			if _, exists := requiredCapabilities[capability.key]; !exists {
				requiredCapabilities[capability.key] = capability.reason
			}
		}
	}

	availableSlots := maxSelectedFrameworks - len(selected)
	chosenIndexes, ok := exactOptionalCover(optional, selected, requiredCapabilities, availableSlots)
	if !ok {
		reasons := make([]string, 0, len(requiredCapabilities))
		for _, reason := range requiredCapabilities {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		return nil, nil, fmt.Errorf(
			"framework selection cannot cover %s within the %d-framework safety limit (%s)",
			strings.Join(reasons, "; "),
			maxSelectedFrameworks,
			strings.Join(optionalCoverProviderSummary(optional, selected, requiredCapabilities), "; "),
		)
	}

	chosen := make(map[string]struct{}, len(selected)+len(chosenIndexes))
	for _, candidate := range selected {
		chosen[candidate.view.ID] = struct{}{}
	}
	for _, index := range chosenIndexes {
		candidate := optional[index]
		for _, capability := range candidate.coverage {
			candidate.reasons = append(candidate.reasons, "coverage: "+capability.reason)
		}
		candidate.reasons = conciseReasons(candidate.reasons)
		selected = append(selected, candidate)
		chosen[candidate.view.ID] = struct{}{}
	}

	conflicts := make([]FrameworkConflict, 0)
	for _, candidate := range optional {
		if _, exists := chosen[candidate.view.ID]; exists || len(candidate.coverage) == 0 {
			continue
		}
		existing, conflictsWithSelected := firstConflictingFramework(candidate, selected)
		if !conflictsWithSelected {
			continue
		}
		conflicts = append(conflicts, FrameworkConflict{
			SelectedID: existing.view.ID,
			SkippedID:  candidate.view.ID,
			Reason:     conflictReason(existing, candidate),
		})
	}

	return selected, conflicts, nil
}

func optionalCoverProviderSummary(
	optional []selectionCandidate,
	required []selectionCandidate,
	capabilities map[string]string,
) []string {
	keys := make([]string, 0, len(capabilities))
	for capability := range capabilities {
		keys = append(keys, capability)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, capability := range keys {
		providers := make([]string, 0)
		for _, candidate := range optional {
			if !coverageContains(candidate.coverage, capability) {
				continue
			}
			label := candidate.view.ID
			if existing, conflicts := firstConflictingFramework(candidate, required); conflicts {
				label += " (conflicts with " + existing.view.ID + ")"
			}
			providers = append(providers, label)
		}
		result = append(result, capability+"=["+strings.Join(providers, ", ")+"]")
	}
	return result
}

func coverageContains(coverage []frameworkCoverage, key string) bool {
	for _, capability := range coverage {
		if capability.key == key {
			return true
		}
	}
	return false
}

func exactOptionalCover(
	optional []selectionCandidate,
	required []selectionCandidate,
	capabilities map[string]string,
	limit int,
) ([]int, bool) {
	if len(capabilities) == 0 {
		return nil, true
	}
	if limit <= 0 {
		return nil, false
	}

	providers := make(map[string][]int, len(capabilities))
	for index, candidate := range optional {
		if len(candidate.coverage) == 0 {
			continue
		}
		if _, conflicts := firstConflictingFramework(candidate, required); conflicts {
			continue
		}
		for _, capability := range candidate.coverage {
			providers[capability.key] = append(providers[capability.key], index)
		}
	}
	for capability := range capabilities {
		if len(providers[capability]) == 0 {
			return nil, false
		}
	}

	var best []int
	chosen := make([]int, 0, limit)
	chosenSet := make(map[int]struct{}, limit)
	covered := make(map[string]int, len(capabilities))

	var search func()
	search = func() {
		if len(covered) == len(capabilities) {
			candidate := append([]int(nil), chosen...)
			sort.Ints(candidate)
			if best == nil || len(candidate) < len(best) ||
				(len(candidate) == len(best) && indexCombinationPrecedes(candidate, best)) {
				best = candidate
			}
			return
		}
		if len(chosen) >= limit || (best != nil && len(chosen) >= len(best)) {
			return
		}

		uncoveredCount, maximumMarginal := coverSearchBounds(optional, required, chosen, chosenSet, covered, capabilities)
		if maximumMarginal == 0 {
			return
		}
		lowerBound := (uncoveredCount + maximumMarginal - 1) / maximumMarginal
		if len(chosen)+lowerBound > limit || (best != nil && len(chosen)+lowerBound > len(best)) {
			return
		}

		capability, feasibleProviders := rarestUncoveredCapability(
			optional,
			required,
			providers,
			chosen,
			chosenSet,
			covered,
			capabilities,
		)
		if capability == "" || len(feasibleProviders) == 0 {
			return
		}
		for _, index := range feasibleProviders {
			chosen = append(chosen, index)
			chosenSet[index] = struct{}{}
			for _, item := range optional[index].coverage {
				covered[item.key]++
			}

			search()

			for _, item := range optional[index].coverage {
				covered[item.key]--
				if covered[item.key] == 0 {
					delete(covered, item.key)
				}
			}
			delete(chosenSet, index)
			chosen = chosen[:len(chosen)-1]
		}
	}

	search()
	return best, best != nil
}

func coverSearchBounds(
	optional []selectionCandidate,
	required []selectionCandidate,
	chosen []int,
	chosenSet map[int]struct{},
	covered map[string]int,
	capabilities map[string]string,
) (int, int) {
	uncoveredCount := len(capabilities) - len(covered)
	maximumMarginal := 0
	for index, candidate := range optional {
		if _, exists := chosenSet[index]; exists ||
			candidateConflictsWithIndexes(candidate, optional, required, chosen) {
			continue
		}
		marginal := 0
		for _, capability := range candidate.coverage {
			if _, requiredCapability := capabilities[capability.key]; !requiredCapability {
				continue
			}
			if _, alreadyCovered := covered[capability.key]; !alreadyCovered {
				marginal++
			}
		}
		if marginal > maximumMarginal {
			maximumMarginal = marginal
		}
	}
	return uncoveredCount, maximumMarginal
}

func rarestUncoveredCapability(
	optional []selectionCandidate,
	required []selectionCandidate,
	providers map[string][]int,
	chosen []int,
	chosenSet map[int]struct{},
	covered map[string]int,
	capabilities map[string]string,
) (string, []int) {
	keys := make([]string, 0, len(capabilities))
	for capability := range capabilities {
		if _, exists := covered[capability]; !exists {
			keys = append(keys, capability)
		}
	}
	sort.Strings(keys)

	bestCapability := ""
	var bestProviders []int
	for _, capability := range keys {
		feasible := make([]int, 0, len(providers[capability]))
		for _, index := range providers[capability] {
			if _, exists := chosenSet[index]; exists ||
				candidateConflictsWithIndexes(optional[index], optional, required, chosen) {
				continue
			}
			feasible = append(feasible, index)
		}
		if len(feasible) == 0 {
			return capability, nil
		}
		if bestCapability == "" || len(feasible) < len(bestProviders) {
			bestCapability = capability
			bestProviders = feasible
		}
	}
	return bestCapability, bestProviders
}

func candidateConflictsWithIndexes(
	candidate selectionCandidate,
	optional []selectionCandidate,
	required []selectionCandidate,
	chosen []int,
) bool {
	if _, conflicts := firstConflictingFramework(candidate, required); conflicts {
		return true
	}
	for _, index := range chosen {
		if frameworksConflict(candidate.view, optional[index].view) {
			return true
		}
	}
	return false
}

func indexCombinationPrecedes(left, right []int) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func firstConflictingFramework(candidate selectionCandidate, selected []selectionCandidate) (selectionCandidate, bool) {
	for _, existing := range selected {
		if frameworksConflict(candidate.view, existing.view) {
			return existing, true
		}
	}
	return selectionCandidate{}, false
}

func frameworksConflict(left, right FrameworkView) bool {
	return stringSliceContains(left.ConflictsWith, right.ID) || stringSliceContains(right.ConflictsWith, left.ID)
}

func conflictReason(selected, skipped selectionCandidate) string {
	switch {
	case selected.required:
		return fmt.Sprintf("%s is required by policy and takes precedence", selected.view.ID)
	case selected.score > skipped.score:
		return fmt.Sprintf("%s has the higher transparent relevance score", selected.view.ID)
	case selected.score == skipped.score:
		return fmt.Sprintf("equal scores were resolved by deterministic framework ID order in favor of %s", selected.view.ID)
	default:
		return fmt.Sprintf("%s takes precedence under deterministic selection order", selected.view.ID)
	}
}

func explicitContextRequirements(request SelectionRequest) []string {
	result := make([]string, 0, 8)
	if strings.TrimSpace(request.ProjectKey) != "" {
		result = append(result, "project context for "+strings.TrimSpace(request.ProjectKey))
	}
	if strings.TrimSpace(request.PursuitID) != "" {
		result = append(result, "current pursuit state for "+strings.TrimSpace(request.PursuitID))
	}
	if request.NeedsMemory {
		result = append(result, "relevant owner-scoped memory with provenance")
	}
	if request.NeedsDocuments {
		result = append(result, "source documents and extraction provenance")
	}
	if request.NeedsWebAccess {
		result = append(result, "current authoritative web sources and retrieval timestamps")
	}
	if request.NeedsTools {
		result = append(result, "tool capability, allowlist, and current health")
	}
	if request.NeedsLocalExecution {
		result = append(result, "local runtime boundary, folder allowlist, and rollback plan")
	}
	if request.NeedsApproval || request.HumanApproved {
		result = append(result, "scoped approval record and its expiry")
	}
	return result
}

func constitutionSource(constitution Constitution) string {
	if id := strings.TrimSpace(constitution.ID); id != "" {
		return fmt.Sprintf("%s:v%d", id, constitution.Version)
	}
	return fmt.Sprintf("inline:v%d", constitution.Version)
}

func deterministicSelectionID(decision SelectionDecision, request SelectionRequest) string {
	parts := []string{
		decision.CreatedAt.Format(time.RFC3339Nano),
		strings.TrimSpace(request.OwnerIdentity),
		strings.TrimSpace(request.TaskPlanID),
		normalizeText(request.Request),
		decision.LifeDomain,
		decision.NeedOrCommitment,
		decision.CatalogVersion,
		decision.CatalogDigest,
		decision.SelectorAlgorithmVersion,
		decision.TaskRiskLevel,
		decision.EffectiveRiskCeiling,
		decision.OperatingContractDigest,
		strconv.Itoa(decision.ConstitutionVersion),
		strconv.FormatBool(decision.RequiresApproval),
		strconv.Itoa(decision.MaximumAutonomyLevel),
	}
	for _, framework := range decision.Selected {
		parts = append(parts,
			framework.ID,
			framework.Version,
			framework.RiskCeiling,
			strconv.FormatFloat(framework.Score, 'f', 4, 64),
			strconv.Itoa(framework.MaximumAutonomyLevel),
		)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return uuid.NewSHA1(uuid.NameSpaceOID, sum[:]).String()
}

func conciseReasons(values []string) []string {
	values = uniqueStrings(values)
	if len(values) > 6 {
		values = values[:6]
	}
	return values
}

func containsAnyPhrase(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if containsPhrase(text, phrase) {
			return true
		}
	}
	return false
}

// containsRequestedConsequentialAction keeps explicit safety constraints from
// being mistaken for requested side effects. It recognizes only direct, local
// negation around an action phrase; ambiguous wording stays conservative.
func containsRequestedConsequentialAction(text string, phrases []string) bool {
	words := strings.Fields(normalizeText(text))
	for _, phrase := range phrases {
		action := strings.Fields(normalizeText(phrase))
		if len(action) == 0 || len(action) > len(words) {
			continue
		}
		for start := 0; start+len(action) <= len(words); start++ {
			if !matchesWords(words[start:start+len(action)], action) {
				continue
			}
			if !consequentialActionIsExplicitlyProhibited(words, start) {
				return true
			}
		}
	}
	return false
}

func matchesWords(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func consequentialActionIsExplicitlyProhibited(words []string, actionStart int) bool {
	if actionStart == 0 {
		return false
	}
	previous := words[actionStart-1]
	if previous == "never" || previous == "without" || previous == "avoid" || previous == "prevent" || previous == "prohibit" || previous == "forbid" || previous == "not" {
		return true
	}
	return actionStart >= 2 && words[actionStart-2] == "not" && (previous == "to" || previous == "ever")
}

func containsPhrase(normalizedText, phrase string) bool {
	normalizedPhrase := normalizeText(phrase)
	if normalizedPhrase == "" {
		return false
	}
	return strings.Contains(" "+normalizedText+" ", " "+normalizedPhrase+" ")
}

func normalizeText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	lastSpace := true
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
			lastSpace = false
			continue
		}
		if !lastSpace {
			builder.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func meaningfulTokens(value string) []string {
	stopWords := map[string]struct{}{
		"a": {}, "all": {}, "an": {}, "and": {}, "for": {}, "in": {}, "of": {},
		"on": {}, "or": {}, "the": {}, "to": {}, "with": {},
	}
	result := make([]string, 0)
	for _, token := range strings.Fields(normalizeText(value)) {
		if _, skip := stopWords[token]; skip {
			continue
		}
		if len(token) <= 2 {
			continue
		}
		result = append(result, canonicalToken(token))
	}
	return uniqueStrings(result)
}

func tokenSet(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, token := range meaningfulTokens(value) {
		result[token] = struct{}{}
	}
	return result
}

func canonicalToken(token string) string {
	switch {
	case len(token) > 5 && strings.HasSuffix(token, "ies"):
		return strings.TrimSuffix(token, "ies") + "y"
	case len(token) > 5 && strings.HasSuffix(token, "ing"):
		return strings.TrimSuffix(token, "ing")
	case len(token) > 4 && strings.HasSuffix(token, "ed"):
		return strings.TrimSuffix(token, "ed")
	case len(token) > 4 && strings.HasSuffix(token, "s"):
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func stringSliceContains(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

package frameworkregistry

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

const expectedBuiltinCatalogV2Digest = "aaaab649a81eb8a3c55a3b43b63a954dc0e4f6ccd4c3ffed9508bf4de5575473"

var expectedFrameworkIDsBySection = []string{
	"human-sovereignty",
	"whole-life-ontology",
	"needs-wellbeing",
	"capacity-state",
	"goal-hierarchy",
	"intake-triage",
	"multi-criteria-prioritization",
	"multi-agent-organization",
	"agent-identity-capability",
	"delegation-accountability",
	"agent-communication",
	"multi-agent-coordination",
	"reasoning-methods",
	"cognitive-agent-architecture",
	"uncertainty-decision",
	"formal-planning",
	"workflow-modeling",
	"reliable-execution",
	"autonomy-levels",
	"approval-control",
	"memory-architecture",
	"personal-knowledge-management",
	"retrieval-context",
	"truth-evidence",
	"ingestion-synchronization",
	"ambient-perception",
	"human-ai-interaction",
	"privacy-protection",
	"security-zero-trust",
	"agent-threat-modeling",
	"safety-engineering",
	"ai-governance",
	"model-intelligence",
	"evaluation",
	"observability",
	"reliability-resilience",
	"controlled-learning",
	"productivity-attention",
	"habit-behavior-change",
	"health-personal-care",
	"financial-management",
	"home-garden-assets",
	"work-service-delivery",
	"entrepreneurship-venture",
	"legal-government-case",
	"communication",
	"relationships-care",
	"learning-competence",
	"travel-mobility",
	"emergency-continuity",
	"agent-development-adapters",
	"durable-workflow-platforms",
	"memory-knowledge-implementations",
	"policy-security-implementations",
	"evaluation-observability-implementations",
}

func TestBuiltinCatalogHasExactly55UniqueVersionedRecordsInSpecificationOrder(t *testing.T) {
	t.Parallel()

	catalog := BuiltinCatalog()
	if got, want := len(catalog), 55; got != want {
		t.Fatalf("catalog record count = %d, want %d", got, want)
	}

	ids := make(map[string]struct{}, len(catalog))
	versionedIDs := make(map[string]struct{}, len(catalog))
	for index, framework := range catalog {
		if got, want := framework.ID, expectedFrameworkIDsBySection[index]; got != want {
			t.Fatalf("catalog[%d].ID = %q, want %q", index, got, want)
		}
		if !semanticVersionPattern.MatchString(framework.Version) {
			t.Errorf("framework %q version %q is not semantic x.y.z", framework.ID, framework.Version)
		}
		if _, exists := ids[framework.ID]; exists {
			t.Errorf("duplicate framework ID %q", framework.ID)
		}
		ids[framework.ID] = struct{}{}

		versionedID := framework.ID + "@" + framework.Version
		if _, exists := versionedIDs[versionedID]; exists {
			t.Errorf("duplicate versioned framework record %q", versionedID)
		}
		versionedIDs[versionedID] = struct{}{}
	}
}

func TestBuiltinCatalogSectionsExistInImportedTaxonomySource(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("../../../docs/source-material/whole-life-framework-taxonomy.txt")
	if err != nil {
		t.Fatalf("read imported taxonomy source: %v", err)
	}

	headingPattern := regexp.MustCompile(`^##\s+([0-9]+)\.\s+(.+?)\s*$`)
	sections := make([]int, 0, len(expectedFrameworkIDsBySection))
	for _, line := range strings.Split(string(source), "\n") {
		matches := headingPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if matches == nil {
			continue
		}
		section, parseErr := strconv.Atoi(matches[1])
		if parseErr != nil || strings.TrimSpace(matches[2]) == "" {
			t.Fatalf("invalid imported taxonomy heading %q", line)
		}
		sections = append(sections, section)
	}
	if len(sections) != len(expectedFrameworkIDsBySection) {
		t.Fatalf("imported taxonomy has %d numbered framework sections, want %d", len(sections), len(expectedFrameworkIDsBySection))
	}
	for index, section := range sections {
		if want := index + 1; section != want {
			t.Errorf("imported taxonomy section at index %d = %d, want %d", index, section, want)
		}
	}
}

func TestBuiltinCatalogV2DigestCannotDriftWithoutAnExplicitVersionDecision(t *testing.T) {
	t.Parallel()

	catalog := BuiltinCatalog()
	sort.SliceStable(catalog, func(i, j int) bool {
		return catalog[i].ID < catalog[j].ID
	})
	digest, err := canonicalSHA256(catalog)
	if err != nil {
		t.Fatalf("digest built-in catalog: %v", err)
	}
	if frameworkCatalogVersion != "v2" {
		t.Fatalf("catalog version %q has no reviewed golden digest contract", frameworkCatalogVersion)
	}
	if digest != expectedBuiltinCatalogV2Digest {
		t.Fatalf(
			"catalog v2 digest changed: got %q, want %q; review the metadata and bump the catalog version before accepting intentional drift",
			digest,
			expectedBuiltinCatalogV2Digest,
		)
	}
}

func TestBuiltinOperationalContractsCoverCatalogExactly(t *testing.T) {
	t.Parallel()

	if got, want := len(builtinCatalogOperationalContracts), len(expectedFrameworkIDsBySection); got != want {
		t.Fatalf("operational contract count = %d, want %d", got, want)
	}

	expected := make(map[string]struct{}, len(expectedFrameworkIDsBySection))
	for _, id := range expectedFrameworkIDsBySection {
		expected[id] = struct{}{}
		if _, exists := builtinCatalogOperationalContracts[id]; !exists {
			t.Errorf("framework %q has no operational contract", id)
		}
	}
	for id := range builtinCatalogOperationalContracts {
		if _, exists := expected[id]; !exists {
			t.Errorf("operational contract %q has no catalog framework", id)
		}
	}
}

func TestBuiltinCatalogRequiredMetadataContract(t *testing.T) {
	t.Parallel()

	catalog := BuiltinCatalog()
	if err := ValidateCatalog(catalog); err != nil {
		t.Fatalf("ValidateCatalog(BuiltinCatalog()) returned error: %v", err)
	}

	knownIDs := frameworksByID(catalog)
	for _, framework := range catalog {
		requiredScalars := map[string]string{
			"id":                    framework.ID,
			"version":               framework.Version,
			"name":                  framework.Name,
			"family":                framework.Family,
			"purpose":               framework.Purpose,
			"authority requirement": framework.AuthorityRequirement,
			"risk ceiling":          framework.RiskCeiling,
			"source":                framework.Source,
			"provenance":            framework.Provenance,
			"status":                framework.Status,
		}
		for field, value := range requiredScalars {
			if strings.TrimSpace(value) == "" {
				t.Errorf("framework %q %s is empty", framework.ID, field)
			}
		}

		requiredSlices := map[string][]string{
			"suitable problem types":    framework.SuitableProblemTypes,
			"trigger conditions":        framework.TriggerConditions,
			"required inputs":           framework.RequiredInputs,
			"produced outputs":          framework.ProducedOutputs,
			"required agents":           framework.RequiredAgents,
			"workflow template":         framework.WorkflowTemplate,
			"decision rules":            framework.DecisionRules,
			"safety invariants":         framework.SafetyInvariants,
			"evidence requirements":     framework.EvidenceRequirements,
			"evaluation method":         framework.EvaluationMethod,
			"user-specific adaptations": framework.UserSpecificAdaptations,
		}
		for field, values := range requiredSlices {
			assertNonEmptyUniqueStrings(t, framework.ID+" "+field, values, true)
		}
		assertNonEmptyUniqueStrings(t, framework.ID+" conflicts", framework.ConflictsWith, false)
		assertNonEmptyUniqueStrings(t, framework.ID+" candidates", framework.CandidateImplementations, false)
		for _, conflictID := range framework.ConflictsWith {
			if _, exists := knownIDs[conflictID]; !exists {
				t.Errorf("framework %q conflict %q has no catalog target", framework.ID, conflictID)
			}
		}
	}
}

func TestBuiltinCatalogHasMateriallySpecificOperationalContracts(t *testing.T) {
	t.Parallel()

	type contractField struct {
		name    string
		minimum int
		values  func(Framework) []string
	}
	fields := []contractField{
		{"applicable problem types", 2, func(item Framework) []string { return item.SuitableProblemTypes }},
		{"trigger conditions", 2, func(item Framework) []string { return item.TriggerConditions }},
		{"required inputs", 2, func(item Framework) []string { return item.RequiredInputs }},
		{"produced outputs", 2, func(item Framework) []string { return item.ProducedOutputs }},
		{"workflow template", 4, func(item Framework) []string { return item.WorkflowTemplate }},
		{"decision rules", 3, func(item Framework) []string { return item.DecisionRules }},
		{"evidence requirements", 2, func(item Framework) []string { return item.EvidenceRequirements }},
		{"completion criteria", 2, func(item Framework) []string { return item.EvaluationMethod }},
	}

	seen := make(map[string]map[string]string, len(fields))
	for _, field := range fields {
		seen[field.name] = make(map[string]string)
	}

	for _, framework := range BuiltinCatalog() {
		hasContraindication := false
		for _, rule := range framework.DecisionRules {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rule)), "do not use this framework to ") {
				hasContraindication = true
				break
			}
		}
		if !hasContraindication {
			t.Errorf("framework %q has no explicit operational contraindication", framework.ID)
		}

		for _, field := range fields {
			values := field.values(framework)
			if len(values) < field.minimum {
				t.Errorf(
					"framework %q %s count = %d, want at least %d",
					framework.ID,
					field.name,
					len(values),
					field.minimum,
				)
				continue
			}
			signature := normalizedCatalogSignature(values)
			if prior, duplicate := seen[field.name][signature]; duplicate {
				t.Errorf(
					"frameworks %q and %q share the same %s contract",
					prior,
					framework.ID,
					field.name,
				)
			}
			seen[field.name][signature] = framework.ID

			for _, value := range values {
				normalized := strings.ToLower(strings.TrimSpace(value))
				if _, generic := forbiddenGenericCatalogEntries[normalized]; generic {
					t.Errorf("framework %q retains generic %s boilerplate %q", framework.ID, field.name, value)
				}
				if catalogPlaceholderPattern.MatchString(value) {
					t.Errorf("framework %q contains placeholder %s text %q", framework.ID, field.name, value)
				}
			}
		}
	}
}

func TestBuiltinCatalogConflictRelationshipsAreSymmetric(t *testing.T) {
	t.Parallel()

	byID := frameworksByID(BuiltinCatalog())
	expectedPairs := [][2]string{
		{"cognitive-agent-architecture", "reasoning-methods"},
		{"cognitive-agent-architecture", "workflow-modeling"},
	}
	for _, pair := range expectedPairs {
		left, leftExists := byID[pair[0]]
		right, rightExists := byID[pair[1]]
		if !leftExists || !rightExists {
			t.Fatalf("expected conflict pair references missing frameworks: %q <-> %q", pair[0], pair[1])
		}
		if !containsExactFold(left.ConflictsWith, right.ID) {
			t.Errorf("framework %q does not conflict with %q", left.ID, right.ID)
		}
		if !containsExactFold(right.ConflictsWith, left.ID) {
			t.Errorf("framework %q does not reciprocate conflict with %q", right.ID, left.ID)
		}
	}

	for _, framework := range byID {
		for _, conflictID := range framework.ConflictsWith {
			conflict, exists := byID[conflictID]
			if !exists {
				t.Errorf("framework %q references unknown conflict %q", framework.ID, conflictID)
				continue
			}
			if !containsExactFold(conflict.ConflictsWith, framework.ID) {
				t.Errorf("framework conflict %q -> %q is not symmetric", framework.ID, conflict.ID)
			}
		}
	}
}

func TestValidateCatalogRejectsInvalidMutations(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func([]Framework)
		wantError string
	}{
		{"invalid semantic version", func(items []Framework) {
			items[0].Version = "v1.0"
		}, "invalid semantic version"},
		{"unsupported risk ceiling", func(items []Framework) {
			items[0].RiskCeiling = "critical"
		}, "unsupported risk ceiling"},
		{"duplicate versioned record", func(items []Framework) {
			items[1] = items[0]
		}, "duplicate versioned framework record"},
		{"duplicate id ignoring case", func(items []Framework) {
			items[1].ID = strings.ToUpper(items[0].ID)
			items[1].Version = "2.0.0"
		}, "changes stable name or family"},
		{"blank scalar", func(items []Framework) {
			items[0].Purpose = " "
		}, "missing required scalar metadata"},
		{"empty required slice", func(items []Framework) {
			items[0].DecisionRules = nil
		}, "invalid decision rules"},
		{"blank slice element", func(items []Framework) {
			items[0].DecisionRules = append(items[0].DecisionRules, " ")
		}, "is blank"},
		{"duplicate slice element ignoring case", func(items []Framework) {
			items[0].DecisionRules = append(items[0].DecisionRules, strings.ToUpper(items[0].DecisionRules[0]))
		}, "contains duplicate element"},
		{"invalid status", func(items []Framework) {
			items[0].Status = "installed"
		}, "invalid status"},
		{"autonomy below range", func(items []Framework) {
			items[0].MaximumAutonomyLevel = -1
		}, "invalid autonomy level"},
		{"autonomy above range", func(items []Framework) {
			items[0].MaximumAutonomyLevel = 11
		}, "invalid autonomy level"},
		{"blank conflict", func(items []Framework) {
			items[0].ConflictsWith = []string{" "}
		}, "invalid conflicts"},
		{"duplicate conflict ignoring case", func(items []Framework) {
			items[0].ConflictsWith = []string{items[1].ID, strings.ToUpper(items[1].ID)}
		}, "contains duplicate element"},
		{"unknown conflict", func(items []Framework) {
			items[0].ConflictsWith = []string{"not-in-catalog"}
		}, "references unknown conflict"},
		{"self conflict", func(items []Framework) {
			items[0].ConflictsWith = []string{strings.ToUpper(items[0].ID)}
		}, "cannot conflict with itself"},
		{"asymmetric conflict", func(items []Framework) {
			for index := range items {
				if items[index].ID == "reasoning-methods" {
					items[index].ConflictsWith = nil
				}
			}
		}, "is not symmetric"},
		{"generic boilerplate", func(items []Framework) {
			items[0].RequiredInputs[0] = "operator-scoped request or source signal"
		}, "generic boilerplate"},
		{"placeholder text", func(items []Framework) {
			items[0].EvidenceRequirements[0] = "TBD framework-specific evidence"
		}, "placeholder text"},
		{"missing contraindication", func(items []Framework) {
			filtered := make([]string, 0, len(items[0].DecisionRules))
			for _, rule := range items[0].DecisionRules {
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(rule)), "do not use this framework to ") {
					filtered = append(filtered, rule)
				}
			}
			items[0].DecisionRules = filtered
		}, "no explicit contraindication"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			catalog := BuiltinCatalog()
			test.mutate(catalog)
			err := ValidateCatalog(catalog)
			if err == nil {
				t.Fatalf("ValidateCatalog() returned nil, want error containing %q", test.wantError)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantError)) {
				t.Fatalf("ValidateCatalog() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestValidateCatalogAcceptsMultipleVersionsOfOneStableFrameworkID(t *testing.T) {
	catalog := BuiltinCatalog()
	versionTwo := cloneFramework(catalog[0])
	versionTwo.Version = "2.0.0"
	versionTwo.Purpose += " Version two keeps an immutable lifecycle record."
	catalog = append(catalog, versionTwo)

	if err := ValidateCatalog(catalog); err != nil {
		t.Fatalf("ValidateCatalog rejected a distinct version of one stable ID: %v", err)
	}
}

func TestBuiltinCatalogAutonomyRiskStatusAndSectionContracts(t *testing.T) {
	t.Parallel()

	statusCounts := map[string]int{}
	sections := make(map[int]string, 55)
	const sourcePrefix = "HAI framework architecture specification, section "

	for _, framework := range BuiltinCatalog() {
		if framework.MaximumAutonomyLevel < 0 || framework.MaximumAutonomyLevel > 10 {
			t.Errorf("framework %q autonomy = %d, want 0..10", framework.ID, framework.MaximumAutonomyLevel)
		}
		if framework.RiskCeiling != "low" && framework.RiskCeiling != "medium" && framework.RiskCeiling != "high" {
			t.Errorf("framework %q risk ceiling = %q", framework.ID, framework.RiskCeiling)
		}
		statusCounts[framework.Status]++

		if !strings.HasPrefix(framework.Source, sourcePrefix) {
			t.Errorf("framework %q source %q lacks specification section", framework.ID, framework.Source)
			continue
		}
		section, err := strconv.Atoi(strings.TrimPrefix(framework.Source, sourcePrefix))
		if err != nil {
			t.Errorf("framework %q has invalid source section: %v", framework.ID, err)
			continue
		}
		if previous, duplicate := sections[section]; duplicate {
			t.Errorf("section %d is shared by %q and %q", section, previous, framework.ID)
		}
		sections[section] = framework.ID
	}

	if got := statusCounts[StatusActive]; got != 50 {
		t.Errorf("active framework count = %d, want 50", got)
	}
	if got := statusCounts[StatusExperimental]; got != 5 {
		t.Errorf("experimental framework count = %d, want 5", got)
	}
	if got := statusCounts[StatusDeprecated]; got != 0 {
		t.Errorf("deprecated framework count = %d, want 0", got)
	}
	for index, expectedID := range expectedFrameworkIDsBySection {
		if got := sections[index+1]; got != expectedID {
			t.Errorf("section %d framework = %q, want %q", index+1, got, expectedID)
		}
	}
}

func TestBuiltinCatalogKeepsSafetyAndProtectedFrameworkContracts(t *testing.T) {
	t.Parallel()

	safetyIDs := []string{
		"human-sovereignty",
		"autonomy-levels",
		"approval-control",
		"privacy-protection",
		"security-zero-trust",
		"agent-threat-modeling",
		"safety-engineering",
		"ai-governance",
		"emergency-continuity",
	}
	byID := frameworksByID(BuiltinCatalog())
	for _, id := range safetyIDs {
		framework, exists := byID[id]
		if !exists {
			t.Errorf("safety framework %q is missing", id)
			continue
		}
		if framework.Status != StatusActive {
			t.Errorf("safety framework %q status = %q", id, framework.Status)
		}
		assertContainsText(t, id+" invariants", framework.SafetyInvariants, "constitution")
		assertContainsText(t, id+" invariants", framework.SafetyInvariants, "untrusted")
		assertContainsText(t, id+" invariants", framework.SafetyInvariants, "evidence")
	}

	for _, framework := range byID {
		if !isProtectedMandatoryFramework(framework.ID) {
			continue
		}
		assertContainsText(t, framework.ID+" adaptations", framework.UserSpecificAdaptations, "cannot disable")
		if containsExactFold(framework.UserSpecificAdaptations, "operator may disable or pin this framework") {
			t.Errorf("protected framework %q advertises disabling", framework.ID)
		}
	}
}

func TestBuiltinCatalogImplementationEntriesRemainExperimentalMetadata(t *testing.T) {
	t.Parallel()

	expectedIDs := map[string]struct{}{
		"agent-development-adapters":               {},
		"durable-workflow-platforms":               {},
		"memory-knowledge-implementations":         {},
		"policy-security-implementations":          {},
		"evaluation-observability-implementations": {},
	}
	actualIDs := map[string]struct{}{}
	for _, framework := range BuiltinCatalog() {
		if framework.Family != "implementation" {
			continue
		}
		actualIDs[framework.ID] = struct{}{}
		if framework.Status != StatusExperimental {
			t.Errorf("implementation framework %q status = %q", framework.ID, framework.Status)
		}
		if len(framework.CandidateImplementations) == 0 {
			t.Errorf("implementation framework %q has no candidates", framework.ID)
		}

		payload, err := json.Marshal(framework)
		if err != nil {
			t.Fatalf("marshal framework %q: %v", framework.ID, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatalf("unmarshal framework %q: %v", framework.ID, err)
		}
		if _, exists := fields["candidateImplementations"]; !exists {
			t.Errorf("framework %q does not serialize candidate metadata", framework.ID)
		}
		for _, forbidden := range []string{
			"installedImplementations",
			"enabledImplementations",
			"availableImplementations",
			"healthyImplementations",
			"runningImplementations",
			"runtimeStatus",
			"healthStatus",
		} {
			if _, exists := fields[forbidden]; exists {
				t.Errorf("framework %q serializes runtime claim %q", framework.ID, forbidden)
			}
		}

		operational := append([]string{}, framework.RequiredAgents...)
		operational = append(operational, framework.ProducedOutputs...)
		operational = append(operational, framework.WorkflowTemplate...)
		for _, candidate := range framework.CandidateImplementations {
			if containsExactFold(operational, candidate) {
				t.Errorf("framework %q treats candidate %q as operationally available", framework.ID, candidate)
			}
		}
	}
	if !reflect.DeepEqual(actualIDs, expectedIDs) {
		t.Errorf("implementation framework IDs = %v, want %v", actualIDs, expectedIDs)
	}
}

func TestBuiltinCatalogMatchesSpecifiedImplementationCandidatesExactly(t *testing.T) {
	t.Parallel()

	expected := map[string][]string{
		"agent-development-adapters": {
			"LangGraph",
			"Microsoft AutoGen",
			"Microsoft Agent Framework",
			"Semantic Kernel",
			"OpenAI Agents SDK",
			"Google Agent Development Kit",
			"CrewAI",
			"PydanticAI",
			"LlamaIndex Agents",
			"Haystack Agents",
			"Hugging Face smolagents",
			"Agno",
			"BeeAI",
			"Mastra",
			"LangChain",
			"DSPy",
			"Letta",
			"CAMEL",
			"MetaGPT",
			"AutoGPT",
			"SuperAGI",
			"Flowise",
			"Langflow",
		},
		"policy-security-implementations": {
			"Open Policy Agent",
			"Cedar",
			"Casbin",
			"SpiceDB",
			"OpenFGA",
			"Keycloak",
			"Authentik",
			"HashiCorp Vault",
			"SOPS",
			"Sigstore",
			"in-toto",
			"TUF",
			"SLSA",
			"CycloneDX",
			"SPDX",
			"AIBOM",
			"gVisor",
			"Firecracker",
			"WebAssembly sandboxes",
			"container isolation",
			"seccomp",
			"AppArmor",
		},
	}

	byID := frameworksByID(BuiltinCatalog())
	for frameworkID, want := range expected {
		framework, exists := byID[frameworkID]
		if !exists {
			t.Errorf("implementation framework %q is missing", frameworkID)
			continue
		}
		if !reflect.DeepEqual(framework.CandidateImplementations, want) {
			t.Errorf(
				"framework %q candidates = %#v, want exact specification list %#v",
				frameworkID,
				framework.CandidateImplementations,
				want,
			)
		}
	}
}

func assertNonEmptyUniqueStrings(t *testing.T, field string, values []string, required bool) {
	t.Helper()
	if required && len(values) == 0 {
		t.Errorf("%s is empty", field)
		return
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			t.Errorf("%s[%d] is blank", field, index)
			continue
		}
		normalized := strings.ToLower(trimmed)
		if _, duplicate := seen[normalized]; duplicate {
			t.Errorf("%s contains duplicate %q", field, value)
		}
		seen[normalized] = struct{}{}
	}
}

func assertContainsText(t *testing.T, field string, values []string, expected string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(expected)) {
			return
		}
	}
	t.Errorf("%s does not contain %q", field, expected)
}

func frameworksByID(catalog []Framework) map[string]Framework {
	result := make(map[string]Framework, len(catalog))
	for _, framework := range catalog {
		result[framework.ID] = framework
	}
	return result
}

func containsExactFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func normalizedCatalogSignature(values []string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(value)))
	}
	return strings.Join(normalized, "\x00")
}

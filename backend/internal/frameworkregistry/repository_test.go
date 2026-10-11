package frameworkregistry

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFrameworkPreferenceLockIdentityIsTextSafeAndUnambiguous(t *testing.T) {
	first := frameworkPreferenceLockIdentity("a", "b\x00c")
	second := frameworkPreferenceLockIdentity("a\x00b", "c")

	if strings.ContainsRune(first, '\x00') || strings.ContainsRune(second, '\x00') {
		t.Fatal("PostgreSQL advisory-lock identity contains a NUL byte")
	}
	if first == second {
		t.Fatal("distinct framework preferences produced the same advisory-lock identity")
	}
}

func TestMemoryRepositoryPreferencesAreOwnerScopedAndDefensivelyCopied(t *testing.T) {
	repo := NewMemoryRepository()
	level := 3
	alice, err := repo.UpsertPreference(" alice ", Preference{
		FrameworkID:          "truth-evidence",
		State:                PreferenceEnabled,
		Pinned:               true,
		MaximumAutonomyLevel: &level,
		Adaptations:          []string{`Preserve "quoted" evidence`, "Keep\nline breaks safe"},
	})
	if err != nil {
		t.Fatalf("UpsertPreference alice: %v", err)
	}
	if _, err := repo.UpsertPreference("bob", Preference{
		FrameworkID: "truth-evidence",
		State:       PreferenceDisabled,
	}); err != nil {
		t.Fatalf("UpsertPreference bob: %v", err)
	}

	alice.Adaptations[0] = "caller mutation"
	alicePreferences, err := repo.ListPreferences("alice")
	if err != nil {
		t.Fatalf("ListPreferences alice: %v", err)
	}
	if len(alicePreferences) != 1 || alicePreferences[0].State != PreferenceEnabled {
		t.Fatalf("alice preferences = %#v", alicePreferences)
	}
	if alicePreferences[0].Adaptations[0] != `Preserve "quoted" evidence` {
		t.Fatalf("stored adaptations were mutated through return value: %#v", alicePreferences[0].Adaptations)
	}

	bobPreferences, err := repo.ListPreferences("bob")
	if err != nil {
		t.Fatalf("ListPreferences bob: %v", err)
	}
	if len(bobPreferences) != 1 || bobPreferences[0].State != PreferenceDisabled {
		t.Fatalf("bob preferences = %#v", bobPreferences)
	}
	systemPreferences, err := repo.ListPreferences("")
	if err != nil || len(systemPreferences) != 0 {
		t.Fatalf("ownerless preferences = %#v, %v", systemPreferences, err)
	}
}

func TestMemoryRepositorySelectionAuditIsTypedRedactedAndImmutableByCopy(t *testing.T) {
	repo := NewMemoryRepository()
	createdAt := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	decision := SelectionDecision{
		ID:                        uuid.NewString(),
		TaskPlanID:                "task-018",
		CreatedAt:                 createdAt,
		CatalogVersion:            "1.0.0",
		CatalogDigest:             repositoryTestDigest("catalog"),
		SelectorAlgorithmVersion:  "framework-selector-v1",
		EffectivePreferenceDigest: repositoryTestDigest("preferences"),
		ConstitutionDigest:        repositoryTestDigest("constitution"),
		OperatingContractDigest:   repositoryTestDigest("operating-contract"),
		LifeDomain:                "work",
		NeedOrCommitment:          "Review source evidence",
		Selected: []SelectedFramework{{
			ID:                   "truth-evidence",
			Version:              "1.0.0",
			Name:                 "Truth and evidence",
			Family:               "verification",
			Score:                0.92,
			Reasons:              []string{`Matches "source" review`},
			MaximumAutonomyLevel: 2,
			AuthorityRequirement: "recommend only",
			EvidenceRequirements: []string{"source record"},
			EvaluationMethod:     []string{"claim support"},
		}},
		Conflicts:            []FrameworkConflict{{SelectedID: "truth-evidence", SkippedID: "draft-only", Reason: "verification required"}},
		RequiredAgents:       []string{"evidence_reviewer"},
		MaximumAutonomyLevel: 2,
		AuthoritySummary:     "Recommend only",
		RequiresApproval:     true,
		ApprovalReasons:      []string{"consequential output"},
		EvidenceRequirements: []string{"source record"},
		CompletionCriteria:   []string{"claims verified"},
		LearningPlan:         []string{"record confirmed correction"},
		ContextRequirements:  []string{"project evidence"},
		SelectionReason:      "Verification is required.",
		ConstitutionVersion:  1,
		ConstitutionSource:   "builtin-robert-constitution-v1:v1",
	}
	hash := sha256.Sum256([]byte("source request"))
	summary := "Review source password=do-not-store Authorization: Bearer abc.def.ghi\nwith evidence"
	if err := repo.CreateSelection("alice", decision, fmt.Sprintf("%x", hash), summary); err != nil {
		t.Fatalf("CreateSelection: %v", err)
	}

	storedAudit := repo.selections["alice"][0]
	if strings.Contains(storedAudit.RequestSummary, "do-not-store") ||
		strings.Contains(storedAudit.RequestSummary, "abc.def.ghi") {
		t.Fatalf("request summary retained a secret: %q", storedAudit.RequestSummary)
	}
	if strings.Contains(storedAudit.RequestSummary, "\n") ||
		len([]rune(storedAudit.RequestSummary)) > maxRequestSummaryRunes {
		t.Fatalf("request summary is not compact: %q", storedAudit.RequestSummary)
	}

	selections, err := repo.ListSelections("alice", 20)
	if err != nil {
		t.Fatalf("ListSelections: %v", err)
	}
	if len(selections) != 1 || selections[0].CreatedAt != createdAt {
		t.Fatalf("selections = %#v", selections)
	}
	if got := selections[0].Selected[0].Reasons[0]; got != `Matches "source" review` {
		t.Fatalf("selected framework JSON did not round-trip: %q", got)
	}
	if selections[0].CatalogDigest != decision.CatalogDigest ||
		selections[0].EffectivePreferenceDigest != decision.EffectivePreferenceDigest ||
		selections[0].ConstitutionDigest != decision.ConstitutionDigest ||
		selections[0].ConstitutionSource != decision.ConstitutionSource {
		t.Fatalf("selection reproducibility metadata did not round-trip: %#v", selections[0])
	}
	exact, err := repo.GetSelection(context.Background(), "alice", decision.ID)
	if err != nil || exact.ID != decision.ID || exact.TaskPlanID != decision.TaskPlanID {
		t.Fatalf("GetSelection = %#v, %v", exact, err)
	}
	if _, err := repo.GetSelection(context.Background(), "bob", decision.ID); err == nil {
		t.Fatal("exact selection lookup crossed the owner boundary")
	}
	exact.Selected[0].Reasons[0] = "caller mutation"
	exactAgain, err := repo.GetSelection(context.Background(), "alice", decision.ID)
	if err != nil || exactAgain.Selected[0].Reasons[0] != `Matches "source" review` {
		t.Fatalf("exact selection lookup leaked a mutable record: %#v, %v", exactAgain, err)
	}
	selections[0].Selected[0].Reasons[0] = "caller mutation"
	again, err := repo.ListSelections("alice", 20)
	if err != nil {
		t.Fatalf("ListSelections again: %v", err)
	}
	if again[0].Selected[0].Reasons[0] != `Matches "source" review` {
		t.Fatal("selection audit was mutated through a returned nested slice")
	}
	legacyRow, err := selectionToModel(
		"alice",
		decision,
		fmt.Sprintf("%x", hash),
		"historical selection",
	)
	if err != nil {
		t.Fatalf("selectionToModel legacy fixture: %v", err)
	}
	legacyRow.SelectorAlgorithmVersion = "selector-v3"
	legacyRow.OperatingContractDigest = strings.Repeat("0", sha256.Size*2)
	legacyDecision, err := selectionFromModel(legacyRow)
	if err != nil {
		t.Fatalf("selectionFromModel legacy row: %v", err)
	}
	if legacyDecision.OperatingContractDigest != "" {
		t.Fatalf(
			"legacy row fabricated operating contract digest %q",
			legacyDecision.OperatingContractDigest,
		)
	}
	if bob, err := repo.ListSelections("bob", 20); err != nil || len(bob) != 0 {
		t.Fatalf("selection audit leaked to bob: %#v, %v", bob, err)
	}
	if err := repo.CreateSelection("alice", decision, "not-a-sha256", "safe summary"); err == nil {
		t.Fatal("invalid request hash was accepted")
	}
	missingMetadata := decision
	missingMetadata.ID = uuid.NewString()
	missingMetadata.CatalogDigest = ""
	if err := repo.CreateSelection("alice", missingMetadata, fmt.Sprintf("%x", hash), "safe summary"); err == nil {
		t.Fatal("selection without a catalog digest was accepted")
	}
	mismatchedSource := decision
	mismatchedSource.ID = uuid.NewString()
	mismatchedSource.ConstitutionSource = "builtin-robert-constitution-v1:v2"
	if err := repo.CreateSelection("alice", mismatchedSource, fmt.Sprintf("%x", hash), "safe summary"); err == nil {
		t.Fatal("selection with a mismatched Constitution source was accepted")
	}

	v5 := decision
	v5.ID = uuid.NewString()
	v5.CreatedAt = decision.CreatedAt.Add(time.Nanosecond)
	v5.SelectorAlgorithmVersion = "selector-v5"
	v5.TaskRiskLevel = "medium"
	v5.EffectiveRiskCeiling = "high"
	v5.Selected = append([]SelectedFramework(nil), decision.Selected...)
	v5.Selected[0].RiskCeiling = "high"
	if err := repo.CreateSelection("alice", v5, fmt.Sprintf("%x", hash), "v5 risk contract"); err != nil {
		t.Fatalf("CreateSelection v5: %v", err)
	}
	v5Selections, err := repo.ListSelections("alice", 20)
	if err != nil {
		t.Fatalf("ListSelections v5: %v", err)
	}
	if got := v5Selections[0]; got.TaskRiskLevel != "medium" || got.EffectiveRiskCeiling != "high" || got.Selected[0].RiskCeiling != "high" {
		t.Fatalf("v5 risk contract did not round-trip: %#v", got)
	}

	for _, test := range []struct {
		name   string
		mutate func(*SelectionDecision)
	}{
		{name: "missing task risk", mutate: func(item *SelectionDecision) { item.TaskRiskLevel = "" }},
		{name: "task exceeds ceiling", mutate: func(item *SelectionDecision) {
			item.TaskRiskLevel = "high"
			item.EffectiveRiskCeiling = "medium"
		}},
		{name: "selected framework below task", mutate: func(item *SelectionDecision) {
			item.TaskRiskLevel = "high"
			item.EffectiveRiskCeiling = "high"
			item.Selected[0].RiskCeiling = "medium"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := v5
			candidate.ID = uuid.NewString()
			candidate.Selected = append([]SelectedFramework(nil), v5.Selected...)
			test.mutate(&candidate)
			if err := repo.CreateSelection("alice", candidate, fmt.Sprintf("%x", hash), "invalid v5 risk contract"); err == nil {
				t.Fatal("invalid selector-v5 risk contract was accepted")
			}
		})
	}
}

func TestMemoryRepositoryConstitutionLifecycleIsOwnerScopedAndVersioned(t *testing.T) {
	repo := NewMemoryRepository()
	first, err := repo.CreateConstitution("alice", Constitution{
		ID:             uuid.NewString(),
		Status:         ConstitutionDraft,
		Values:         []string{"Keep Robert in control"},
		ProtectedRules: []string{"Never self-approve"},
		ChangeSummary:  "Initial owner version",
	})
	if err != nil {
		t.Fatalf("CreateConstitution first: %v", err)
	}
	if first.Version != 1 || first.Status != ConstitutionDraft {
		t.Fatalf("first constitution = %#v", first)
	}
	if _, err := repo.CreateConstitution("alice", Constitution{
		ID:            uuid.NewString(),
		Version:       1,
		Status:        ConstitutionDraft,
		ChangeSummary: "Duplicate version",
	}); err == nil {
		t.Fatal("duplicate owner/version was accepted")
	}
	if _, err := repo.CreateConstitution("alice", Constitution{
		ID:            uuid.NewString(),
		Version:       2,
		Status:        ConstitutionDraft,
		ChangeSummary: "Missing immutable base-version provenance",
	}); err == nil {
		t.Fatal("versioned Constitution without base-version provenance was accepted")
	}

	second, err := repo.CreateConstitution("alice", Constitution{
		ID:             uuid.NewString(),
		Version:        2,
		BaseVersion:    first.Version,
		Status:         ConstitutionDraft,
		Values:         []string{"Keep Robert in control", "Prefer verified completion"},
		ProtectedRules: []string{"Never self-approve"},
		ChangeSummary:  "Second owner version",
	})
	if err != nil {
		t.Fatalf("CreateConstitution second: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("second version = %d, want 2", second.Version)
	}

	approvedAt := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	if _, err := repo.ActivateConstitution(
		"alice",
		first.ID,
		"alice",
		"Reviewed password=do-not-store and approved.",
		approvedAt,
	); err != nil {
		t.Fatalf("ActivateConstitution first: %v", err)
	}
	active, err := repo.ActivateConstitution(
		"alice",
		second.ID,
		"alice",
		"Reviewed the owner-scoped changes.",
		approvedAt.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("ActivateConstitution second: %v", err)
	}
	if active.Status != ConstitutionActive || active.Version != 2 || active.ApprovedAt == nil {
		t.Fatalf("active constitution = %#v", active)
	}

	constitutions, err := repo.ListConstitutions("alice")
	if err != nil {
		t.Fatalf("ListConstitutions: %v", err)
	}
	if len(constitutions) != 2 ||
		constitutions[0].Status != ConstitutionActive ||
		constitutions[1].Status != ConstitutionSuperseded {
		t.Fatalf("constitution lifecycle = %#v", constitutions)
	}
	if strings.Contains(repo.constitutions["alice"][0].ApprovalNote, "do-not-store") {
		t.Fatal("approval note retained a secret")
	}
	if bob, err := repo.ListConstitutions("bob"); err != nil || len(bob) != 0 {
		t.Fatalf("constitutions leaked to bob: %#v, %v", bob, err)
	}
}

func TestJSONArrayHelpersRejectObjectsAndEncodeNilAsArray(t *testing.T) {
	encoded, err := encodeJSONArray[string](nil)
	if err != nil {
		t.Fatalf("encodeJSONArray: %v", err)
	}
	if encoded != "[]" {
		t.Fatalf("nil array encoded as %q", encoded)
	}
	if _, err := decodeJSONArray[string](`{"not":"an array"}`); err == nil {
		t.Fatal("object JSON was accepted as an array")
	}
}

func TestDestructiveIntegrationTargetGuard(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		databaseName string
		wantError    bool
	}{
		{name: "explicit test database", flag: "true", databaseName: "hai_framework_registry_test"},
		{name: "case insensitive flag", flag: "TRUE", databaseName: "automation_hub_test"},
		{name: "missing opt in", databaseName: "hai_framework_registry_test", wantError: true},
		{name: "false opt in", flag: "false", databaseName: "hai_framework_registry_test", wantError: true},
		{name: "production database", flag: "true", databaseName: "automation_hub", wantError: true},
		{name: "test word is not suffix", flag: "true", databaseName: "test_automation_hub", wantError: true},
		{name: "blank database", flag: "true", databaseName: "", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDestructiveIntegrationTarget(test.flag, test.databaseName)
			if test.wantError && err == nil {
				t.Fatal("unsafe integration target was accepted")
			}
			if !test.wantError && err != nil {
				t.Fatalf("safe integration target rejected: %v", err)
			}
		})
	}
}

func validateDestructiveIntegrationTarget(flag, databaseName string) error {
	if !strings.EqualFold(strings.TrimSpace(flag), "true") {
		return fmt.Errorf("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS must be true")
	}
	databaseName = strings.ToLower(strings.TrimSpace(databaseName))
	if !strings.HasSuffix(databaseName, "_test") {
		return fmt.Errorf("database %q must have the _test suffix", databaseName)
	}
	return nil
}

func repositoryTestDigest(label string) string {
	sum := sha256.Sum256([]byte(label))
	return fmt.Sprintf("%x", sum)
}

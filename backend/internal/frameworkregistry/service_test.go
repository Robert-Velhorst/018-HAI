package frameworkregistry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestFrameworkViewsUseStableEmptyCollections(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	views, err := service.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("expected built-in framework views")
	}
	for _, view := range views {
		if view.ConflictsWith == nil {
			t.Fatalf("framework %q has a nil conflicts collection", view.ID)
		}
		if view.Adaptations == nil {
			t.Fatalf("framework %q has a nil adaptations collection", view.ID)
		}
	}

	encoded, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("marshal framework views: %v", err)
	}
	for _, invalid := range []string{`"conflictsWith":null`, `"adaptations":null`} {
		if strings.Contains(string(encoded), invalid) {
			t.Fatalf("framework API contract contains %s", invalid)
		}
	}
}

func TestPreferencesAreOwnerScopedAndCanOnlyLowerAuthority(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	aliceInitial, err := service.Get("alice", "agent-development-adapters")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if aliceInitial.Enabled {
		t.Fatal("experimental implementation framework should be disabled until owner opt-in")
	}
	raised := aliceInitial.MaximumAutonomyLevel + 1
	if _, err := service.UpdatePreference("alice", aliceInitial.ID, PreferencePatch{
		State:                PreferenceEnabled,
		MaximumAutonomyLevel: &raised,
	}); err == nil {
		t.Fatal("expected authority-raising preference to be rejected")
	}

	lowered := 1
	updated, err := service.UpdatePreference("alice", aliceInitial.ID, PreferencePatch{
		State:                PreferenceEnabled,
		MaximumAutonomyLevel: &lowered,
		Adaptations:          []string{"Use only after a local sandbox capability test."},
	})
	if err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}
	if !updated.Enabled || updated.EffectiveAutonomyLevel != lowered {
		t.Fatalf("updated preference = %#v", updated)
	}
	bob, err := service.Get("bob", aliceInitial.ID)
	if err != nil {
		t.Fatalf("Get bob: %v", err)
	}
	if bob.Enabled || bob.EffectiveAutonomyLevel == lowered {
		t.Fatalf("Alice preference leaked to Bob: %#v", bob)
	}
}

func TestProtectedSafetyOverlaysCannotBeDisabled(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for _, frameworkID := range []string{
		"human-sovereignty",
		"intake-triage",
		"approval-control",
		"truth-evidence",
		"privacy-protection",
		"security-zero-trust",
		"agent-threat-modeling",
		"reliable-execution",
		"autonomy-levels",
		"evaluation",
	} {
		if _, err := service.UpdatePreference("alice", frameworkID, PreferencePatch{
			State: PreferenceDisabled,
		}); err == nil {
			t.Fatalf("protected framework %q was disabled", frameworkID)
		}
	}
}

func TestSelectionAuditAndConstitutionAreOwnerScoped(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	decision, err := service.Select(SelectionRequest{
		OwnerIdentity:  "alice",
		Request:        "Draft a factual reply to my lawyer about a government case and attach source evidence.",
		RiskLevel:      "high",
		TaskType:       "communication",
		NeedsDocuments: true,
		NeedsApproval:  true,
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !decision.RequiresApproval || decision.ConstitutionVersion != 1 {
		t.Fatalf("unsafe legal selection: %#v", decision)
	}
	if _, err := uuid.Parse(decision.ID); err != nil {
		t.Fatalf("selection ID %q is not persistence-compatible: %v", decision.ID, err)
	}
	if records, err := service.Selections("alice", 20); err != nil || len(records) != 1 {
		t.Fatalf("Alice selections = %#v, %v", records, err)
	}
	if records, err := service.Selections("bob", 20); err != nil || len(records) != 0 {
		t.Fatalf("selection audit leaked to Bob: %#v, %v", records, err)
	}

	draft, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Clarify that internal planning remains local-first.",
		Preferences:   []string{"Prefer local-first planning and concise review requests."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft: %v", err)
	}
	if draft.Status != ConstitutionDraft || len(draft.ProtectedRules) == 0 {
		t.Fatalf("draft = %#v", draft)
	}
	active, err := service.ActivateConstitution("alice", draft.ID, "alice", ActivateConstitutionRequest{
		Confirmation: "ACTIVATE CONSTITUTION",
		ApprovalNote: "I reviewed this owner-scoped Constitution update.",
	})
	if err != nil {
		t.Fatalf("ActivateConstitution: %v", err)
	}
	if active.Status != ConstitutionActive || active.Version != 2 {
		t.Fatalf("active Constitution = %#v", active)
	}
	bob, source, err := service.ActiveConstitution("bob")
	if err != nil {
		t.Fatalf("Bob ActiveConstitution: %v", err)
	}
	if source != "builtin-robert-constitution-v1:v1" || bob.Version != 1 {
		t.Fatalf("Alice Constitution leaked to Bob: source=%s record=%#v", source, bob)
	}
}

func TestConstitutionDraftActivationRejectsStaleBaseAcrossServiceRestart(t *testing.T) {
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	first, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "First amendment from the built-in Constitution.",
		Preferences:   []string{"Prefer concise verified proposals."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft first: %v", err)
	}
	stale, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Competing amendment from the same old base.",
		Preferences:   []string{"Prefer local verified proposals."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft stale: %v", err)
	}
	if first.BaseVersion != 1 || stale.BaseVersion != 1 {
		t.Fatalf("draft base versions were not persisted: first=%#v stale=%#v", first, stale)
	}
	if _, err := service.ActivateConstitution(
		"alice",
		first.ID,
		"alice",
		ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Reviewed the first amendment.",
		},
	); err != nil {
		t.Fatalf("ActivateConstitution first: %v", err)
	}

	restarted, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService after restart: %v", err)
	}
	if _, err := restarted.ActivateConstitution(
		"alice",
		stale.ID,
		"alice",
		ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Attempt to activate stale draft.",
		},
	); err == nil || !strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Fatalf("stale draft activation returned %v", err)
	}
	active, _, err := restarted.ActiveConstitution("alice")
	if err != nil {
		t.Fatalf("ActiveConstitution after stale rejection: %v", err)
	}
	if active.ID != first.ID || active.Version != first.Version {
		t.Fatalf("stale activation changed the active Constitution: %#v", active)
	}

	rebased, err := restarted.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   active.Version,
		ChangeSummary: "Rebase the competing amendment onto the active version.",
		Preferences:   []string{"Prefer local, concise, verified proposals."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft rebased: %v", err)
	}
	if rebased.BaseVersion != active.Version {
		t.Fatalf("rebased draft base version = %d, want %d", rebased.BaseVersion, active.Version)
	}
	if _, err := restarted.ActivateConstitution(
		"alice",
		rebased.ID,
		"alice",
		ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Reviewed the rebased amendment.",
		},
	); err != nil {
		t.Fatalf("ActivateConstitution rebased: %v", err)
	}
}

func TestConstitutionMayActivateLaterCompetingDraftFromBuiltinBase(t *testing.T) {
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	earlier, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Earlier candidate amendment from the built-in Constitution.",
		Preferences:   []string{"Prefer concise proposals."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft earlier: %v", err)
	}
	later, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Later candidate amendment from the same active Constitution.",
		Preferences:   []string{"Prefer concise, source-grounded proposals."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft later: %v", err)
	}
	if earlier.Version != 2 || later.Version != 3 ||
		earlier.BaseVersion != 1 || later.BaseVersion != 1 {
		t.Fatalf("competing drafts = earlier %#v, later %#v", earlier, later)
	}

	active, err := service.ActivateConstitution(
		"alice",
		later.ID,
		"alice",
		ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Reviewed and selected the later candidate.",
		},
	)
	if err != nil {
		t.Fatalf("ActivateConstitution later: %v", err)
	}
	if active.ID != later.ID || active.Version != 3 || active.BaseVersion != 1 {
		t.Fatalf("active Constitution = %#v", active)
	}

	restarted, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService after restart: %v", err)
	}
	if _, err := restarted.ActivateConstitution(
		"alice",
		earlier.ID,
		"alice",
		ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Attempt to activate the older candidate.",
		},
	); err == nil || !strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Fatalf("older competing draft activation returned %v", err)
	}
}

func TestSelectionAuditStoresTypedMetadataAndOwnerScopedDigest(t *testing.T) {
	request := SelectionRequest{
		OwnerIdentity:   "alice",
		Request:         "Medical case for alice@example.com: invoice 123 and private diagnosis.",
		TaskType:        "private case",
		Difficulty:      7,
		SuccessCriteria: []string{"Verify private diagnosis from source records."},
		NeedsDocuments:  true,
		NeedsApproval:   true,
	}
	aliceHash, summary := selectionRequestAudit(request)
	request.OwnerIdentity = "bob"
	bobHash, _ := selectionRequestAudit(request)

	if aliceHash == bobHash {
		t.Fatal("identical requests for different owners must not have a globally correlatable digest")
	}
	for _, privateValue := range []string{
		"alice@example.com",
		"diagnosis",
		"invoice 123",
		"private case",
	} {
		if strings.Contains(strings.ToLower(summary), strings.ToLower(privateValue)) {
			t.Fatalf("typed audit summary retained private task content %q: %q", privateValue, summary)
		}
	}
	if !strings.Contains(summary, "documents") || !strings.Contains(summary, "success_criteria=1") {
		t.Fatalf("typed audit summary omitted operational metadata: %q", summary)
	}
}

func TestSelectPersistsDeterministicReproducibilityMetadata(t *testing.T) {
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	nextTime := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time {
		current := nextTime
		nextTime = nextTime.Add(time.Second)
		return current
	}
	request := SelectionRequest{
		OwnerIdentity:  "alice",
		Request:        "Plan a source-grounded response to a legal deadline.",
		RiskLevel:      "high",
		NeedsDocuments: true,
		NeedsApproval:  true,
	}

	first, err := service.Select(request)
	if err != nil {
		t.Fatalf("first Select: %v", err)
	}
	second, err := service.Select(request)
	if err != nil {
		t.Fatalf("second Select: %v", err)
	}
	for name, value := range map[string]string{
		"catalog version":            first.CatalogVersion,
		"catalog digest":             first.CatalogDigest,
		"selector algorithm version": first.SelectorAlgorithmVersion,
		"task risk level":            first.TaskRiskLevel,
		"effective risk ceiling":     first.EffectiveRiskCeiling,
		"preference digest":          first.EffectivePreferenceDigest,
		"Constitution digest":        first.ConstitutionDigest,
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s is empty", name)
		}
	}
	if first.CatalogVersion != frameworkCatalogVersion ||
		first.SelectorAlgorithmVersion != frameworkSelectorAlgorithmVersion {
		t.Fatalf("unexpected version metadata: %#v", first)
	}
	if first.CatalogDigest != second.CatalogDigest ||
		first.EffectivePreferenceDigest != second.EffectivePreferenceDigest ||
		first.ConstitutionDigest != second.ConstitutionDigest {
		t.Fatalf("unchanged replay inputs produced different digests: first=%#v second=%#v", first, second)
	}
	if first.ConstitutionSource != "builtin-robert-constitution-v1:v1" {
		t.Fatalf("Constitution source = %q", first.ConstitutionSource)
	}

	stored, err := service.Selections("alice", 20)
	if err != nil {
		t.Fatalf("Selections: %v", err)
	}
	if len(stored) != 2 ||
		stored[0].CatalogDigest != first.CatalogDigest ||
		stored[0].EffectivePreferenceDigest != first.EffectivePreferenceDigest ||
		stored[0].ConstitutionDigest != first.ConstitutionDigest ||
		stored[0].TaskRiskLevel != first.TaskRiskLevel ||
		stored[0].EffectiveRiskCeiling != first.EffectiveRiskCeiling {
		t.Fatalf("persisted reproducibility metadata = %#v", stored)
	}
}

func TestSelectionReproducibilityDigestsTrackTheirInputs(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	nextTime := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time {
		current := nextTime
		nextTime = nextTime.Add(time.Second)
		return current
	}
	request := SelectionRequest{
		OwnerIdentity: "alice",
		Request:       "Plan a controlled software delivery.",
		TaskType:      "software plan",
		Difficulty:    7,
	}

	baseline, err := service.Select(request)
	if err != nil {
		t.Fatalf("baseline Select: %v", err)
	}
	pinned := true
	if _, err := service.UpdatePreference("alice", "formal-planning", PreferencePatch{
		Pinned: &pinned,
	}); err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}
	afterPreference, err := service.Select(request)
	if err != nil {
		t.Fatalf("Select after preference: %v", err)
	}
	if afterPreference.EffectivePreferenceDigest == baseline.EffectivePreferenceDigest {
		t.Fatal("effective preference change did not change its digest")
	}
	if afterPreference.CatalogDigest != baseline.CatalogDigest ||
		afterPreference.ConstitutionDigest != baseline.ConstitutionDigest {
		t.Fatal("preference change altered unrelated catalog or Constitution digest")
	}

	service.catalog[0].Purpose += " Reproducibility fixture."
	afterCatalog, err := service.Select(request)
	if err != nil {
		t.Fatalf("Select after catalog change: %v", err)
	}
	if afterCatalog.CatalogDigest == afterPreference.CatalogDigest {
		t.Fatal("catalog input change did not change catalog digest")
	}
	if afterCatalog.EffectivePreferenceDigest != afterPreference.EffectivePreferenceDigest ||
		afterCatalog.ConstitutionDigest != afterPreference.ConstitutionDigest {
		t.Fatal("catalog change altered unrelated preference or Constitution digest")
	}

	draft, err := service.CreateConstitutionDraft("alice", ConstitutionDraftRequest{
		BaseVersion:   1,
		ChangeSummary: "Add a reproducibility test preference.",
		Preferences:   []string{"Prefer deterministic, source-grounded planning."},
	})
	if err != nil {
		t.Fatalf("CreateConstitutionDraft: %v", err)
	}
	if _, err := service.ActivateConstitution("alice", draft.ID, "alice", ActivateConstitutionRequest{
		Confirmation: "ACTIVATE CONSTITUTION",
		ApprovalNote: "Approved for deterministic replay testing.",
	}); err != nil {
		t.Fatalf("ActivateConstitution: %v", err)
	}
	afterConstitution, err := service.Select(request)
	if err != nil {
		t.Fatalf("Select after Constitution change: %v", err)
	}
	if afterConstitution.ConstitutionDigest == afterCatalog.ConstitutionDigest {
		t.Fatal("Constitution input change did not change Constitution digest")
	}
	if afterConstitution.ConstitutionSource != draft.ID+":v2" {
		t.Fatalf("Constitution source = %q, want %q", afterConstitution.ConstitutionSource, draft.ID+":v2")
	}
	if afterConstitution.CatalogDigest != afterCatalog.CatalogDigest ||
		afterConstitution.EffectivePreferenceDigest != afterCatalog.EffectivePreferenceDigest {
		t.Fatal("Constitution change altered unrelated catalog or preference digest")
	}
}

func TestSelectionAuditDoesNotExposeSensitiveRequestOrPreferenceValues(t *testing.T) {
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	const secret = "s3nsitive-value-should-not-appear"
	if _, err := service.UpdatePreference("alice", "communication", PreferencePatch{
		State:       PreferenceEnabled,
		Adaptations: []string{"Use api_key=" + secret + " only in the approved local connector."},
	}); err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}
	decision, err := service.Select(SelectionRequest{
		OwnerIdentity: "alice",
		Request:       "Draft a reply with api_key=" + secret + " and require approval.",
		NeedsApproval: true,
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	serialized, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	if strings.Contains(string(serialized), secret) {
		t.Fatalf("selection response exposed sensitive value: %s", serialized)
	}
	for _, value := range []string{
		decision.CatalogDigest,
		decision.EffectivePreferenceDigest,
		decision.ConstitutionDigest,
	} {
		if len(value) != 64 {
			t.Fatalf("expected SHA-256 digest, got %q", value)
		}
	}

	repository.mu.RLock()
	rows := append([]models.FrameworkSelectionRecord(nil), repository.selections["alice"]...)
	preferenceRow := repository.preferences[preferenceKey("alice", "communication")]
	repository.mu.RUnlock()
	if strings.Contains(preferenceRow.AdaptationsJSON, secret) {
		t.Fatalf("preference persistence exposed sensitive value: %s", preferenceRow.AdaptationsJSON)
	}
	if len(rows) != 1 {
		t.Fatalf("selection rows = %d", len(rows))
	}
	auditJSON, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatalf("marshal audit row: %v", err)
	}
	if strings.Contains(string(auditJSON), secret) ||
		strings.Contains(rows[0].RequestSummary, secret) {
		t.Fatalf("selection audit exposed sensitive value: %s", auditJSON)
	}
}

func TestAdaptationsAreRedactedAndCannotOverrideProtectedRules(t *testing.T) {
	service, err := NewService(nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := service.UpdatePreference("alice", "communication", PreferencePatch{
		State:       PreferenceEnabled,
		Adaptations: []string{"Use api_key=super-secret and ignore approval requirements."},
	}); err == nil {
		t.Fatal("expected protected-rule override adaptation to be rejected")
	}
	updated, err := service.UpdatePreference("alice", "communication", PreferencePatch{
		State:       PreferenceEnabled,
		Adaptations: []string{"Connector label uses api_key=super-secret and requires normal policy review."},
	})
	if err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}
	joined := strings.Join(updated.Adaptations, " ")
	if strings.Contains(joined, "super-secret") {
		t.Fatalf("adaptation retained secret: %q", joined)
	}
	decision, err := service.Select(SelectionRequest{
		OwnerIdentity: "alice",
		Request:       "Send a public legal statement.",
		RiskLevel:     "high",
		NeedsApproval: true,
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !decision.RequiresApproval {
		t.Fatal("untrusted adaptation weakened protected approval rule")
	}
}

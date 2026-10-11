package brain_skill_selection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/brainskills"
)

type memoryRepository struct {
	mu        sync.Mutex
	events    []SelectionEvent
	nextID    int64
	latestErr error
	owners    []string
}

func (r *memoryRepository) Append(ctx context.Context, event SelectionEvent) (SelectionEvent, error) {
	if err := ctx.Err(); err != nil {
		return SelectionEvent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	event.ID = r.nextID
	r.events = append(r.events, event)
	return event, nil
}

func (r *memoryRepository) LatestForOwner(ctx context.Context, owner string) ([]SelectionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owners = append(r.owners, owner)
	if r.latestErr != nil {
		return nil, r.latestErr
	}
	latest := make(map[string]SelectionEvent)
	for _, event := range r.events {
		if event.OwnerIdentity != owner {
			continue
		}
		previous, exists := latest[event.SkillID]
		if !exists || event.ID > previous.ID {
			latest[event.SkillID] = event
		}
	}
	out := make([]SelectionEvent, 0, len(latest))
	for _, event := range latest {
		out = append(out, event)
	}
	return out, nil
}

func (r *memoryRepository) allEvents() []SelectionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SelectionEvent(nil), r.events...)
}

func testService() (*SelectionService, *memoryRepository) {
	repository := &memoryRepository{}
	service := NewSelectionService(repository, brainskills.DefaultCatalog())
	service.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }
	return service, repository
}

func currentCatalogFingerprint() string {
	return brainskills.DefaultCatalog().Fingerprint()
}

func catalogSkill(t *testing.T, id string) brainskills.Skill {
	t.Helper()
	for _, skill := range brainskills.DefaultCatalog().List() {
		if skill.ID == id {
			return skill
		}
	}
	t.Fatalf("test skill %q is missing", id)
	return brainskills.Skill{}
}

func TestSelectionServiceScopesConsentToOwner(t *testing.T) {
	service, repository := testService()
	ctx := context.Background()
	wantSkill := catalogSkill(t, "frontend-design")
	saved, err := service.Set(ctx, "owner-a", "owner-a", wantSkill.ID, true, currentCatalogFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if saved.CatalogFingerprint != currentCatalogFingerprint() {
		t.Fatalf("saved decision catalog fingerprint = %q, want current fingerprint", saved.CatalogFingerprint)
	}

	states, err := service.StatesForOwner(ctx, "owner-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := findState(t, states, wantSkill.ID); got.Status != StatusNotSelected || got.Enabled || got.NeedsReapproval {
		t.Fatalf("other owner's state = %#v", got)
	}
	if enabled, err := service.EnabledForOwner(ctx, "owner-b"); err != nil || len(enabled) != 0 {
		t.Fatalf("other owner's enabled selections = %v, %v", enabled, err)
	}
	enabled, err := service.EnabledForOwner(ctx, "owner-a")
	if err != nil || len(enabled) != 1 || enabled[0].OwnerIdentity != "owner-a" {
		t.Fatalf("owner's enabled selections = %v, %v", enabled, err)
	}
	for _, requestedOwner := range repository.owners {
		if requestedOwner != "owner-a" && requestedOwner != "owner-b" {
			t.Fatalf("repository was queried with unexpected owner %q", requestedOwner)
		}
	}
}

func TestSelectionServiceDisableRevokesGuidance(t *testing.T) {
	service, _ := testService()
	ctx := context.Background()
	if _, err := service.Set(ctx, "owner", "owner", "frontend-design", true, currentCatalogFingerprint()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(ctx, "owner", "owner", "frontend-design", false, ""); err != nil {
		t.Fatal(err)
	}
	state := findState(t, mustStates(t, service, "owner"), "frontend-design")
	if state.Status != StatusDisabled || state.Enabled || state.NeedsReapproval {
		t.Fatalf("revoked state = %#v", state)
	}
	guidance, err := service.GuidanceForTask(ctx, "owner", "frontend", "Redesign the dashboard")
	if err != nil || len(guidance) != 0 {
		t.Fatalf("guidance after revoke = %v, %v", guidance, err)
	}
}

func TestSelectionServiceRejectsStaleCatalogReviewBeforePersisting(t *testing.T) {
	service, repository := testService()
	_, err := service.Set(context.Background(), "owner", "owner", "frontend-design", true, strings.Repeat("0", 64))
	if !errors.Is(err, brainskills.ErrCatalogReviewStale) {
		t.Fatalf("stale catalog review error = %v, want %v", err, brainskills.ErrCatalogReviewStale)
	}
	if events := repository.allEvents(); len(events) != 0 {
		t.Fatalf("stale review persisted a selection: %#v", events)
	}
	if _, err := service.Set(context.Background(), "owner", "owner", "frontend-design", true, currentCatalogFingerprint()); err != nil {
		t.Fatalf("current catalog review was rejected: %v", err)
	}
	if _, err := service.Set(context.Background(), "owner", "owner", "frontend-design", false, ""); err != nil {
		t.Fatalf("disable required a fresh review fingerprint: %v", err)
	}
}

func TestSelectionServiceStalePinNeedsReapprovalAndFailsClosed(t *testing.T) {
	service, repository := testService()
	skill := catalogSkill(t, "frontend-design")
	stale := SelectionEvent{
		OwnerIdentity: "owner", ActorIdentity: "owner", SkillID: skill.ID,
		SourceCommit: skill.Commit, SourceSHA256: strings.Repeat("a", 64),
		GuidanceSHA256: skill.GuidanceSHA256, Enabled: true, DecidedAt: time.Now().UTC(),
	}
	if stale.SourceSHA256 == skill.SourceSHA256 {
		stale.SourceSHA256 = strings.Repeat("b", 64)
	}
	if _, err := repository.Append(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	state := findState(t, mustStates(t, service, "owner"), skill.ID)
	if state.Status != StatusNeedsReapproval || state.Enabled || !state.NeedsReapproval {
		t.Fatalf("stale enabled state = %#v", state)
	}
	if enabled, err := service.EnabledForOwner(context.Background(), "owner"); err != nil || len(enabled) != 0 {
		t.Fatalf("stale pin was returned as enabled: %v, %v", enabled, err)
	}
	guidance, err := service.GuidanceForTask(context.Background(), "owner", "frontend", "Redesign dashboard")
	if err != nil || len(guidance) != 0 {
		t.Fatalf("stale pin was applied: %v, %v", guidance, err)
	}
}

func TestSelectionServiceLegacyDecisionWithoutCatalogFingerprintNeedsReapproval(t *testing.T) {
	service, repository := testService()
	skill := catalogSkill(t, "frontend-design")
	legacy := SelectionEvent{
		OwnerIdentity: "owner", ActorIdentity: "owner", SkillID: skill.ID,
		SourceCommit: skill.Commit, SourceSHA256: skill.SourceSHA256,
		GuidanceSHA256: skill.GuidanceSHA256, Enabled: true, DecidedAt: time.Now().UTC(),
	}
	if _, err := repository.Append(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}

	state := findState(t, mustStates(t, service, "owner"), skill.ID)
	if state.Status != StatusNeedsReapproval || state.Enabled || !state.NeedsReapproval {
		t.Fatalf("legacy selection without catalog fingerprint = %#v", state)
	}
	if guidance, err := service.GuidanceForTask(context.Background(), "owner", "frontend", "Redesign dashboard"); err != nil || len(guidance) != 0 {
		t.Fatalf("legacy selection without catalog fingerprint applied guidance: %v, %v", guidance, err)
	}
}

func TestSelectionServiceStaleDisabledPinPreservesOptOut(t *testing.T) {
	service, repository := testService()
	skill := catalogSkill(t, "frontend-design")
	stale := SelectionEvent{
		OwnerIdentity: "owner", ActorIdentity: "owner", SkillID: skill.ID,
		SourceCommit: skill.Commit, SourceSHA256: strings.Repeat("a", 64),
		GuidanceSHA256: skill.GuidanceSHA256, Enabled: false, DecidedAt: time.Now().UTC(),
	}
	if stale.SourceSHA256 == skill.SourceSHA256 {
		stale.SourceSHA256 = strings.Repeat("b", 64)
	}
	if _, err := repository.Append(context.Background(), stale); err != nil {
		t.Fatal(err)
	}

	state := findState(t, mustStates(t, service, "owner"), skill.ID)
	if state.Status != StatusDisabled || state.Enabled || state.NeedsReapproval {
		t.Fatalf("stale disabled state did not preserve opt-out: %#v", state)
	}
	if enabled, err := service.EnabledForOwner(context.Background(), "owner"); err != nil || len(enabled) != 0 {
		t.Fatalf("stale disabled selection was returned as enabled: %v, %v", enabled, err)
	}
	guidance, err := service.GuidanceForTask(context.Background(), "owner", "frontend", "Redesign the dashboard")
	if err != nil || len(guidance) != 0 {
		t.Fatalf("stale disabled selection applied guidance: %v, %v", guidance, err)
	}
}

func TestGuidanceForTaskUsesOnlyExplicitCurrentExactMatches(t *testing.T) {
	service, repository := testService()
	ctx := context.Background()
	for _, id := range []string{"frontend-design", "mcp-builder"} {
		if _, err := service.Set(ctx, "owner", "owner", id, true, currentCatalogFingerprint()); err != nil {
			t.Fatal(err)
		}
	}

	got, err := service.GuidanceForTask(ctx, "owner", "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "frontend-design" {
		t.Fatalf("exact frontend match = %#v", got)
	}
	skill := catalogSkill(t, "frontend-design")
	summary, ok := brainskills.DefaultCatalog().GuidanceFor(skill.ID)
	var decisionID int64
	for _, event := range repository.allEvents() {
		if event.SkillID == skill.ID {
			decisionID = event.ID
			break
		}
	}
	if decisionID <= 0 {
		t.Fatal("test selection did not persist a durable decision ID")
	}
	if !ok || got[0] != (AppliedGuidance{
		ID: skill.ID, Name: skill.Name, Commit: skill.Commit, SourceSHA256: skill.SourceSHA256,
		GuidanceSHA256: skill.GuidanceSHA256, SelectionDecisionID: decisionID, Summary: summary,
	}) {
		t.Fatalf("guidance provenance or summary mismatch: %#v", got[0])
	}

	noMatch, err := service.GuidanceForTask(ctx, "owner", "insurance", "Prepare an evidence bundle")
	if err != nil || len(noMatch) != 0 {
		t.Fatalf("unmatched task received skills: %v, %v", noMatch, err)
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"id", "name", "commit", "sourceSHA256", "guidanceSHA256", "selectionDecisionId", "summary"}
	if len(object) != len(wantKeys) {
		t.Fatalf("applied guidance exposes unexpected fields: %s", encoded)
	}
	for _, key := range wantKeys {
		if _, exists := object[key]; !exists {
			t.Fatalf("applied guidance lacks %q: %s", key, encoded)
		}
	}
}

func TestGuidanceForTaskSkipsOwnerReadWhenCatalogHasNoMatch(t *testing.T) {
	service, repository := testService()
	got, err := service.GuidanceForTask(context.Background(), "owner", "insurance", "Prepare an evidence bundle")
	if err != nil || len(got) != 0 {
		t.Fatalf("unmatched task guidance = %#v, %v", got, err)
	}
	if len(repository.owners) != 0 {
		t.Fatalf("unmatched task performed owner-consent reads: %v", repository.owners)
	}
}

func TestGuidanceForTaskStillRejectsInvalidCatalogWithoutMatch(t *testing.T) {
	repository := &memoryRepository{}
	service := NewSelectionService(repository, brainskills.Catalog{})
	got, err := service.GuidanceForTask(context.Background(), "owner", "insurance", "Prepare an evidence bundle")
	if !errors.Is(err, ErrInvalidCatalog) || got != nil {
		t.Fatalf("empty-catalog task guidance = %#v, error = %v", got, err)
	}
	if len(repository.owners) != 0 {
		t.Fatalf("invalid catalog performed owner-consent reads: %v", repository.owners)
	}
}

func TestGuidanceForTaskBoundsInputsBeforeOwnerRead(t *testing.T) {
	service, repository := testService()
	for _, test := range []struct {
		name, taskType, request string
	}{
		{name: "oversized task type", taskType: strings.Repeat("x", brainskills.MaxMatchTaskTypeBytes+1), request: "Redesign dashboard"},
		{name: "oversized request", taskType: "frontend", request: strings.Repeat("x", brainskills.MaxMatchRequestBytes+1)},
		{name: "excessive contrast boundaries", taskType: "mcp", request: strings.Repeat("but ", brainskills.MaxMatchContrastBoundaries+1) + "MCP"},
		{name: "invalid utf8", taskType: "frontend", request: string([]byte{0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := service.GuidanceForTask(context.Background(), "owner", test.taskType, test.request)
			if !errors.Is(err, brainskills.ErrInvalidMatchInput) || got != nil {
				t.Fatalf("invalid task input guidance = %#v, error = %v", got, err)
			}
		})
	}
	if len(repository.owners) != 0 {
		t.Fatalf("invalid input performed owner-consent reads: %v", repository.owners)
	}
}

func TestGuidanceSummaryRequiresBoundedUTF8AndMatchingHash(t *testing.T) {
	valid := "A bounded HAI-authored summary."
	digest := sha256.Sum256([]byte(valid))
	wantHash := hex.EncodeToString(digest[:])
	maxBytes := strings.Repeat("x", brainskills.MaxGuidanceSummaryBytes)
	maxDigest := sha256.Sum256([]byte(maxBytes))
	tests := []struct {
		name, value, hash string
		want              bool
	}{
		{name: "valid exact summary", value: valid, hash: wantHash, want: true},
		{name: "exact byte limit", value: maxBytes, hash: hex.EncodeToString(maxDigest[:]), want: true},
		{name: "empty", value: "", hash: wantHash},
		{name: "over byte limit", value: strings.Repeat("x", brainskills.MaxGuidanceSummaryBytes+1), hash: wantHash},
		{name: "invalid UTF-8", value: string([]byte{0xff}), hash: wantHash},
		{name: "mismatched hash", value: valid, hash: strings.Repeat("0", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validGuidanceSummary(test.value, test.hash); got != test.want {
				t.Fatalf("validGuidanceSummary() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestGuidanceForTaskHonorsRequestLevelSkillExclusions(t *testing.T) {
	service, _ := testService()
	ctx := context.Background()
	for _, id := range []string{"mcp-builder", "webapp-testing"} {
		if _, err := service.Set(ctx, "owner", "owner", id, true, currentCatalogFingerprint()); err != nil {
			t.Fatalf("enable %s: %v", id, err)
		}
	}

	for _, request := range []string{
		"Do not use MCP for this integration",
		"MCP is not needed for this task",
	} {
		got, err := service.GuidanceForTask(ctx, "owner", "mcp-builder", request)
		if err != nil || len(got) != 0 {
			t.Fatalf("explicitly excluded MCP guidance was applied for %q: %#v, %v", request, got, err)
		}
	}

	decisionService, _ := testService()
	if _, err := decisionService.Set(ctx, "owner", "owner", "discernment-nudge", true, currentCatalogFingerprint()); err != nil {
		t.Fatalf("enable decision-calibration guidance: %v", err)
	}
	noPlanButExplicitAdvice, err := decisionService.GuidanceForTask(ctx, "owner", "decision-support", "I have no plan; what would you recommend for my career?")
	if err != nil || len(noPlanButExplicitAdvice) != 1 || noPlanButExplicitAdvice[0].ID != "discernment-nudge" {
		t.Fatalf("a negative state mention suppressed an explicit decision-support request: %#v, %v", noPlanButExplicitAdvice, err)
	}
	noAdvice, err := decisionService.GuidanceForTask(ctx, "owner", "decision-support", "No advice; what legal risks exist?")
	if err != nil || len(noAdvice) != 0 {
		t.Fatalf("explicit no-advice request was overridden by related context: %#v, %v", noAdvice, err)
	}
	noAdviceAfterPositiveMention, err := decisionService.GuidanceForTask(ctx, "owner", "decision-support", "I might recommend a plan, but no advice; what legal risks exist?")
	if err != nil || len(noAdviceAfterPositiveMention) != 0 {
		t.Fatalf("a prior positive mention overrode a later no-advice request: %#v, %v", noAdviceAfterPositiveMention, err)
	}
	noAdviceWithContextOnly, err := decisionService.GuidanceForTask(ctx, "owner", "decision-support", "No advice, but what legal risks exist?")
	if err != nil || len(noAdviceWithContextOnly) != 0 {
		t.Fatalf("context after contrast overrode an explicit no-advice request: %#v, %v", noAdviceWithContextOnly, err)
	}
	adviceAfterContrast, err := decisionService.GuidanceForTask(ctx, "owner", "decision-support", "No advice, but what would you recommend?")
	if err != nil || len(adviceAfterContrast) != 1 || adviceAfterContrast[0].ID != "discernment-nudge" {
		t.Fatalf("explicit advice request after contrast did not override earlier opt-out: %#v, %v", adviceAfterContrast, err)
	}

	manualTesting, err := service.GuidanceForTask(ctx, "owner", "testing", "Don't use Playwright; test the web app manually")
	if err != nil || len(manualTesting) != 1 || manualTesting[0].ID != "webapp-testing" {
		t.Fatalf("positive manual web-app testing intent was lost with a Playwright exclusion: %#v, %v", manualTesting, err)
	}

	corrected, err := service.GuidanceForTask(ctx, "owner", "mcp-builder", "No MCP, but review MCP documentation")
	if err != nil || len(corrected) != 1 || corrected[0].ID != "mcp-builder" {
		t.Fatalf("later positive MCP mention did not override an earlier exclusion: %#v, %v", corrected, err)
	}
}

func TestServiceRejectsForgedClientPinAtBoundary(t *testing.T) {
	service, repository := testService()
	forged := strings.Repeat("0", 64)
	_, err := service.SetRequest(context.Background(), "owner", "owner", SelectionRequest{
		SkillID: "frontend-design", Enabled: true, SourceSHA256: &forged,
	})
	if !errors.Is(err, ErrClientPinRejected) {
		t.Fatalf("forged client digest error = %v", err)
	}
	if events := repository.allEvents(); len(events) != 0 {
		t.Fatalf("forged pin appended decisions: %#v", events)
	}
	_, err = service.SetRequest(context.Background(), "owner", "owner", SelectionRequest{
		SkillID: "frontend-design", Enabled: true,
		Pin: &Pin{SourceCommit: brainskills.SourceCommit, SourceSHA256: forged, GuidanceSHA256: forged},
	})
	if !errors.Is(err, ErrClientPinRejected) {
		t.Fatalf("forged nested pin error = %v", err)
	}
}

func TestDecisionEventsAreAppendOnlyAndLatestOrderIsStable(t *testing.T) {
	service, repository := testService()
	ctx := context.Background()
	for _, enabled := range []bool{true, false, true} {
		if _, err := service.Set(ctx, "owner", "owner", "frontend-design", enabled, currentCatalogFingerprintFor(enabled)); err != nil {
			t.Fatal(err)
		}
	}
	events := repository.allEvents()
	if len(events) != 3 || events[0].ID != 1 || events[1].ID != 2 || events[2].ID != 3 {
		t.Fatalf("append order/events = %#v", events)
	}
	if !events[0].Enabled || events[1].Enabled || !events[2].Enabled {
		t.Fatalf("event history was mutated or reordered: %#v", events)
	}
	state := findState(t, mustStates(t, service, "owner"), "frontend-design")
	if !state.Enabled || state.Decision == nil || state.Decision.ID != 3 {
		t.Fatalf("latest selection state = %#v", state)
	}
}

func currentCatalogFingerprintFor(enabled bool) string {
	if enabled {
		return currentCatalogFingerprint()
	}
	return ""
}

func TestOwnerSelectionStoreAdapterUsesServerCatalogPin(t *testing.T) {
	service, repository := testService()
	adapter := NewOwnerSelectionStoreAdapter(service)
	skill := catalogSkill(t, "frontend-design")
	if _, err := adapter.Set(context.Background(), "owner", skill, true, strings.Repeat("0", 64)); !errors.Is(err, brainskills.ErrCatalogReviewStale) {
		t.Fatalf("adapter accepted stale review fingerprint: %v", err)
	}
	if events := repository.allEvents(); len(events) != 0 {
		t.Fatalf("adapter persisted stale review: %#v", events)
	}
	state, err := adapter.Set(context.Background(), "owner", skill, true, currentCatalogFingerprint())
	if err != nil || !state.Enabled || state.NeedsReapproval {
		t.Fatalf("adapter set = %#v, %v", state, err)
	}
	state, err = adapter.State(context.Background(), "owner", skill)
	if err != nil || !state.Enabled {
		t.Fatalf("adapter state = %#v, %v", state, err)
	}
	skill.SourceSHA256 = strings.Repeat("f", 64)
	if _, err := adapter.State(context.Background(), "owner", skill); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("adapter accepted stale/forged server pin: %v", err)
	}
	skill = catalogSkill(t, "frontend-design")
	skill.LicenseSHA256 = strings.Repeat("f", 64)
	if _, err := adapter.State(context.Background(), "owner", skill); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("adapter accepted altered license pin: %v", err)
	}
}

func TestOwnerSelectionStoreAdapterRejectsCatalogMetadataMismatch(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*brainskills.Skill)
	}{
		{name: "source path", mutate: func(skill *brainskills.Skill) { skill.SourcePath = "skills/other/SKILL.md" }},
		{name: "source URL", mutate: func(skill *brainskills.Skill) { skill.SourceURL = "https://example.invalid/source" }},
		{name: "license path", mutate: func(skill *brainskills.Skill) { skill.LicensePath = "LICENSE.txt" }},
		{name: "license URL", mutate: func(skill *brainskills.Skill) { skill.LicenseURL = "https://example.invalid/license" }},
		{name: "license identifier", mutate: func(skill *brainskills.Skill) { skill.License = "Unreviewed" }},
		{name: "display metadata", mutate: func(skill *brainskills.Skill) { skill.Purpose = "Different catalog entry" }},
	}

	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			service, repository := testService()
			adapter := NewOwnerSelectionStoreAdapter(service)
			skill := catalogSkill(t, "frontend-design")
			test.mutate(&skill)

			if _, err := adapter.State(context.Background(), "owner", skill); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("adapter accepted mismatched catalog metadata for state lookup: %v", err)
			}
			if len(repository.owners) != 0 {
				t.Fatalf("catalog mismatch reached owner state lookup: %v", repository.owners)
			}

			if _, err := adapter.Set(context.Background(), "owner", skill, true, currentCatalogFingerprint()); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("adapter accepted mismatched catalog metadata for enable: %v", err)
			}
			if events := repository.allEvents(); len(events) != 0 {
				t.Fatalf("catalog mismatch appended owner consent: %#v", events)
			}
		})
	}
}

func TestCatalogValidationRequiresPinnedPathsAndLicenseDigest(t *testing.T) {
	valid := catalogSkill(t, "discernment-nudge")
	if err := validateCatalogSkill(valid); err != nil {
		t.Fatalf("valid reviewed skill rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*brainskills.Skill)
	}{
		{name: "source path", mutate: func(skill *brainskills.Skill) { skill.SourcePath = "skills/other/SKILL.md" }},
		{name: "license path", mutate: func(skill *brainskills.Skill) { skill.LicensePath = "LICENSE.txt" }},
		{name: "license URL", mutate: func(skill *brainskills.Skill) { skill.LicenseURL = "https://example.invalid/LICENSE.txt" }},
		{name: "malformed license digest", mutate: func(skill *brainskills.Skill) { skill.LicenseSHA256 = strings.Repeat("z", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			skill := valid
			test.mutate(&skill)
			if err := validateCatalogSkill(skill); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("altered source/license metadata accepted: %v", err)
			}
		})
	}
}

func TestInvalidInputsAndRepositoryErrorsFailClosed(t *testing.T) {
	service, repository := testService()
	cases := []struct {
		name, owner, actor, skill string
		want                      error
	}{
		{"blank owner", " ", "owner", "frontend-design", ErrInvalidInput},
		{"blank actor", "owner", "", "frontend-design", ErrInvalidInput},
		{"path-like skill", "owner", "owner", "../frontend-design", ErrInvalidInput},
		{"unknown skill", "owner", "owner", "not-in-catalog", ErrSkillNotFound},
		{"trimmed owner", " owner", "owner", "frontend-design", ErrInvalidInput},
		{"invalid UTF-8 owner", string([]byte{0xff}), "owner", "frontend-design", ErrInvalidInput},
		{"invalid UTF-8 actor", "owner", string([]byte{0xff}), "frontend-design", ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Set(context.Background(), tc.owner, tc.actor, tc.skill, true, currentCatalogFingerprint()); !errors.Is(err, tc.want) {
				t.Fatalf("Set error = %v, want %v", err, tc.want)
			}
		})
	}
	if events := repository.allEvents(); len(events) != 0 {
		t.Fatalf("invalid input persisted decisions: %#v", events)
	}
	badPin := SelectionEvent{
		OwnerIdentity: "owner", ActorIdentity: "owner", SkillID: "frontend-design",
		SourceCommit: strings.ToUpper(brainskills.SourceCommit), SourceSHA256: strings.Repeat("a", 64),
		GuidanceSHA256: strings.Repeat("b", 64), Enabled: true, DecidedAt: time.Now().UTC(),
	}
	if !errors.Is(validateEvent(badPin), ErrInvalidInput) {
		t.Fatal("uppercase commit was accepted")
	}
	if isLowerHex(strings.Repeat("g", 64), 64) || isLowerHex(strings.Repeat("a", 63), 64) {
		t.Fatal("invalid digest format accepted")
	}
	repository.latestErr = errors.New("database unavailable")
	guidance, err := service.GuidanceForTask(context.Background(), "owner", "frontend", "Redesign dashboard")
	if err == nil || guidance != nil {
		t.Fatalf("repository error did not fail closed: %#v, %v", guidance, err)
	}
}

func findState(t *testing.T, states []SelectionState, id string) SelectionState {
	t.Helper()
	for _, state := range states {
		if state.ID == id {
			return state
		}
	}
	t.Fatalf("state %q missing from %#v", id, states)
	return SelectionState{}
}

func mustStates(t *testing.T, service *SelectionService, owner string) []SelectionState {
	t.Helper()
	states, err := service.StatesForOwner(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return states
}

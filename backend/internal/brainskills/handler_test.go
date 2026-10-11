package brainskills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
)

type selectionCall struct {
	owner string
	skill Skill
}

type setCall struct {
	selectionCall
	enabled                    bool
	reviewedCatalogFingerprint string
}

type fakeOwnerSelectionStore struct {
	mu            sync.Mutex
	states        map[string]SelectionState
	stateErr      error
	stateErrSkill string
	setErr        error
	setResult     SelectionState
	stateCall     []selectionCall
	setCall       []setCall
}

type batchSelectionCall struct {
	owner  string
	skills []Skill
}

type fakeBatchOwnerSelectionStore struct {
	*fakeOwnerSelectionStore
	batchErr   error
	omitSkill  string
	batchCalls []batchSelectionCall
}

func (f *fakeBatchOwnerSelectionStore) StatesForOwner(_ context.Context, owner string, skills []Skill) (map[string]SelectionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls = append(f.batchCalls, batchSelectionCall{owner: owner, skills: append([]Skill(nil), skills...)})
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	states := make(map[string]SelectionState, len(skills))
	for _, skill := range skills {
		if skill.ID == f.omitSkill {
			continue
		}
		if f.stateErr != nil && (f.stateErrSkill == "" || f.stateErrSkill == skill.ID) {
			return nil, f.stateErr
		}
		states[skill.ID] = f.states[skill.ID]
	}
	return states, nil
}

func (f *fakeOwnerSelectionStore) State(_ context.Context, owner string, skill Skill) (SelectionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateCall = append(f.stateCall, selectionCall{owner: owner, skill: skill})
	if f.stateErr != nil && (f.stateErrSkill == "" || f.stateErrSkill == skill.ID) {
		return SelectionState{}, f.stateErr
	}
	return f.states[skill.ID], nil
}

func (f *fakeOwnerSelectionStore) Set(_ context.Context, owner string, skill Skill, enabled bool, reviewedCatalogFingerprint string) (SelectionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCall = append(f.setCall, setCall{selectionCall: selectionCall{owner: owner, skill: skill}, enabled: enabled, reviewedCatalogFingerprint: reviewedCatalogFingerprint})
	if f.setErr != nil {
		return SelectionState{}, f.setErr
	}
	return f.setResult, nil
}

func newSkillsTestRouter(handler *Handler, withOwner bool, owner any) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if withOwner {
		router.Use(func(c *gin.Context) {
			c.Set(identity.ContextSubjectKey, owner)
			c.Next()
		})
	}
	router.GET("/", handler.Inventory)
	router.GET("/:id/guidance-preview", handler.GuidancePreview)
	router.PUT("/:id/selection", handler.SetSelection)
	return router
}

func TestOwnerScopedResponsesAreNotCacheable(t *testing.T) {
	handler := NewHandler(&fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}})
	router := newSkillsTestRouter(handler, true, "owner-a")
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "inventory", method: http.MethodGet, path: "/"},
		{name: "guidance preview", method: http.MethodGet, path: "/frontend-design/guidance-preview"},
		{name: "selection", method: http.MethodPut, path: "/frontend-design/selection", body: enableSelectionBody(t, "frontend-design")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.method == http.MethodPut {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func enableSelectionBody(t *testing.T, skillID string) string {
	t.Helper()
	return enableSelectionBodyWithCatalog(t, DefaultCatalog(), skillID)
}

func enableSelectionBodyWithCatalog(t *testing.T, catalog Catalog, skillID string) string {
	t.Helper()
	for _, skill := range catalog.List() {
		if skill.ID == skillID {
			return selectionBody(skill.GuidanceSHA256, catalog.Fingerprint())
		}
	}
	t.Fatalf("skill %q is absent from the catalog", skillID)
	return ""
}

func selectionBody(guidanceSHA256, catalogFingerprint string) string {
	return `{"enabled":true,"reviewedGuidanceSHA256":"` + guidanceSHA256 + `","reviewedCatalogFingerprint":"` + catalogFingerprint + `"}`
}

func TestInventoryReturnsPinnedMetadataAndOwnerStateWithoutGuidance(t *testing.T) {
	decidedAt := time.Date(2026, 9, 24, 12, 30, 45, 123000000, time.UTC)
	store := &fakeOwnerSelectionStore{states: map[string]SelectionState{
		"frontend-design": {Enabled: true, Decision: &SelectionDecisionMetadata{ID: 42, ActorIdentity: "actor-a", DecidedAt: decidedAt}},
		"mcp-builder":     {Enabled: true, NeedsReapproval: true, Decision: &SelectionDecisionMetadata{ID: 43, ActorIdentity: "actor-a", DecidedAt: decidedAt.Add(time.Minute), GuidanceSHA256: strings.Repeat("b", 64)}},
	}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var got inventoryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}
	if got.SourceRepository != RepositoryURL || got.SourceCommit != SourceCommit || got.SourceCommitDate != SourceCommitDate {
		t.Fatalf("unexpected source pin: %+v", got)
	}
	catalog := DefaultCatalog().List()
	if len(got.Skills) != len(catalog) || len(store.stateCall) != len(catalog) {
		t.Fatalf("inventory skills=%d catalog=%d store calls=%d", len(got.Skills), len(catalog), len(store.stateCall))
	}
	for index, skill := range catalog {
		entry := got.Skills[index]
		wantSourceURL := RepositoryURL + "/blob/" + SourceCommit + "/" + skill.SourcePath
		if entry.ID != skill.ID || entry.Name != skill.Name || entry.Description != skill.Purpose || entry.License != skill.License || entry.LicenseURL != skill.LicenseURL || entry.LicensePath != skill.LicensePath || entry.LicenseSHA256 != skill.LicenseSHA256 || entry.SourceURL != skill.SourceURL || entry.SourcePath != skill.SourcePath || entry.SourceSHA256 != skill.SourceSHA256 || entry.GuidanceSHA256 != skill.GuidanceSHA256 || entry.Scope != skill.Scope || entry.Status != skill.Status {
			t.Errorf("inventory metadata for %q does not match catalog: %+v", skill.ID, entry)
		}
		wantLicensePath := "skills/" + skill.ID + "/LICENSE.txt"
		wantLicenseURL := RepositoryURL + "/blob/" + SourceCommit + "/" + wantLicensePath
		if entry.LicensePath != wantLicensePath || entry.LicenseURL != wantLicenseURL {
			t.Errorf("license provenance for %q = (%q, %q), want pinned path/URL (%q, %q)", skill.ID, entry.LicensePath, entry.LicenseURL, wantLicensePath, wantLicenseURL)
		}
		if entry.SourceURL != wantSourceURL {
			t.Errorf("source URL for %q = %q, want pinned catalog URL %q", skill.ID, entry.SourceURL, wantSourceURL)
		}
		if store.stateCall[index].owner != "owner-a" || !reflect.DeepEqual(store.stateCall[index].skill, skill) {
			t.Errorf("store did not receive owner and full current catalog skill: %+v", store.stateCall[index])
		}
		if entry.ID == "frontend-design" && (!entry.Enabled || entry.NeedsReapproval) {
			t.Errorf("enabled state not mapped: %+v", entry)
		}
		if entry.ID == "mcp-builder" && (entry.Enabled || !entry.NeedsReapproval) {
			t.Errorf("stale state was not failed closed: %+v", entry)
		}
		if entry.ID == "frontend-design" && (entry.Decision == nil || entry.Decision.ID != 42 || entry.Decision.ActorIdentity != "actor-a" || !entry.Decision.DecidedAt.Equal(decidedAt)) {
			t.Errorf("latest decision metadata was not exposed: %+v", entry.Decision)
		}
		if entry.ID == "mcp-builder" && (entry.Decision == nil || entry.Decision.ID != 43 || entry.Decision.ActorIdentity != "actor-a" || !entry.Decision.DecidedAt.Equal(decidedAt.Add(time.Minute))) {
			t.Errorf("stale latest decision metadata was lost: %+v", entry.Decision)
		}
		guidance, ok := DefaultCatalog().GuidanceFor(skill.ID)
		if !ok {
			t.Fatalf("catalog guidance missing for %q", skill.ID)
		}
		if strings.Contains(response.Body.String(), guidance) {
			t.Errorf("inventory leaked HAI guidance for %q", skill.ID)
		}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 5 || raw["catalogFingerprint"] == nil {
		t.Fatalf("inventory exposed unexpected top-level fields: %v", raw)
	}
	var rawSkills []map[string]json.RawMessage
	if err := json.Unmarshal(raw["skills"], &rawSkills); err != nil {
		t.Fatal(err)
	}
	wantFields := map[string]bool{
		"id": true, "name": true, "description": true, "license": true,
		"licenseURL": true, "licensePath": true, "licenseSHA256": true,
		"sourceURL": true, "sourcePath": true, "sourceSHA256": true, "guidanceSHA256": true,
		"scope": true, "status": true, "enabled": true, "needsReapproval": true,
	}
	for _, entry := range rawSkills {
		if _, hasDecision := entry["selectionDecision"]; hasDecision {
			if len(entry) != len(wantFields)+1 {
				t.Fatalf("inventory skill has unexpected decision fields: %v", entry)
			}
			var decision map[string]json.RawMessage
			if err := json.Unmarshal(entry["selectionDecision"], &decision); err != nil {
				t.Fatalf("decode decision metadata: %v", err)
			}
			if len(decision) != 3 || decision["id"] == nil || decision["actorIdentity"] == nil || decision["decidedAt"] == nil {
				t.Fatalf("decision metadata includes unexpected or missing fields: %v", decision)
			}
		} else if len(entry) != len(wantFields) {
			t.Fatalf("inventory exposed unexpected skill fields: %v", entry)
		}
		for field := range entry {
			if !wantFields[field] && field != "selectionDecision" {
				t.Errorf("unexpected skill field %q", field)
			}
		}
	}
	for _, forbidden := range []string{"sourceUrl", "boundary", "bundled", "guidance", "scripts", "selectedForTaskCount", "ownerIdentity", "ownerScope", "summary"} {
		if strings.Contains(response.Body.String(), `"`+forbidden+`"`) {
			t.Errorf("inventory exposed forbidden field %q", forbidden)
		}
	}
	if strings.Contains(response.Body.String(), strings.Repeat("b", 64)) {
		t.Fatal("ordinary inventory exposed the prior approval guidance hash")
	}
}

func TestGuidancePreviewReturnsOnlyBoundedHAIGuidanceAndPreviousApprovalHash(t *testing.T) {
	catalog := DefaultCatalog()
	skill := catalog.List()[0]
	guidance, ok := catalog.GuidanceFor(skill.ID)
	if !ok {
		t.Fatal("catalog guidance missing")
	}
	oldHash := strings.Repeat("b", 64)
	store := &fakeOwnerSelectionStore{states: map[string]SelectionState{
		skill.ID: {
			NeedsReapproval: true,
			Decision: &SelectionDecisionMetadata{
				ID: 71, ActorIdentity: "owner-a", GuidanceSHA256: oldHash,
			},
		},
	}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+skill.ID+"/guidance-preview", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview GuidancePreviewResponse
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode guidance preview: %v", err)
	}
	if preview.CatalogFingerprint != catalog.Fingerprint() || !isSHA256Hash(preview.CatalogFingerprint) {
		t.Fatalf("preview catalog fingerprint = %q, want current stable digest %q", preview.CatalogFingerprint, catalog.Fingerprint())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(body) != 8 {
		t.Fatalf("preview fields = %v, want bounded guidance, authority, and approval-preview fields", body)
	}
	var got GuidancePreviewResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode typed preview: %v", err)
	}
	if got.SkillID != skill.ID || got.CurrentGuidance != guidance || got.CurrentGuidanceSHA256 != skill.GuidanceSHA256 ||
		got.AuthorityBoundary != boundary ||
		got.PreviousApprovedGuidanceSHA256 != oldHash || got.PreviousGuidanceTextStored ||
		!strings.Contains(got.PreviousGuidanceTextLimitation, "historical text") {
		t.Fatalf("preview = %+v, want exact current guidance and disclosed old-text limitation", got)
	}
	if len(got.CurrentGuidance) > maxGuidancePreviewBytes || strings.Contains(response.Body.String(), "SKILL.md body") ||
		strings.Contains(response.Body.String(), `"sourceBody"`) {
		t.Fatalf("preview exceeded bounds or exposed upstream content: %s", response.Body.String())
	}
	if len(store.stateCall) != 1 || store.stateCall[0].owner != "owner-a" || store.stateCall[0].skill.ID != skill.ID {
		t.Fatalf("preview state read was not owner-scoped: %+v", store.stateCall)
	}
}

func TestGuidancePreviewForFirstEnableOmitsPreviousApprovalDetails(t *testing.T) {
	skill := DefaultCatalog().List()[0]
	router := newSkillsTestRouter(NewHandler(&fakeOwnerSelectionStore{}), true, "owner-new")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+skill.ID+"/guidance-preview", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "previousApprovedGuidanceSHA256") || strings.Contains(response.Body.String(), "previousGuidanceTextLimitation") {
		t.Fatalf("first enable preview fabricated a previous approval: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"previousGuidanceTextStored":false`) {
		t.Fatalf("preview did not declare historical text availability: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"authorityBoundary":"`+boundary+`"`) {
		t.Fatalf("preview omitted the HAI authority boundary: %s", response.Body.String())
	}
}

func TestGuidancePreviewFailsClosedForBadReapprovalPinAndUnboundedText(t *testing.T) {
	t.Run("missing previous hash", func(t *testing.T) {
		skill := DefaultCatalog().List()[0]
		store := &fakeOwnerSelectionStore{states: map[string]SelectionState{skill.ID: {NeedsReapproval: true}}}
		router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+skill.ID+"/guidance-preview", nil))
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"currentGuidance"`) {
			t.Fatalf("missing approval pin was not failed closed: %d %s", response.Code, response.Body.String())
		}
	})

	t.Run("oversized catalog guidance", func(t *testing.T) {
		guidance := strings.Repeat("x", maxGuidancePreviewBytes+1)
		source := catalogEntry{skill: Skill{ID: "bounded-test", GuidanceSHA256: "unused"}, guidance: guidance}
		handler := &Handler{store: &fakeOwnerSelectionStore{}, catalog: Catalog{entries: []catalogEntry{source}}}
		router := newSkillsTestRouter(handler, true, "owner-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/bounded-test/guidance-preview", nil))
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), guidance) {
			t.Fatalf("oversized guidance was returned: %d body length %d", response.Code, response.Body.Len())
		}
	})
}

func TestGuidancePreviewRequiresAuthenticatedOwnerAndKnownSkill(t *testing.T) {
	store := &fakeOwnerSelectionStore{}
	unauthenticated := newSkillsTestRouter(NewHandler(store), false, nil)
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/frontend-design/guidance-preview", nil))
	if response.Code != http.StatusUnauthorized || len(store.stateCall) != 0 {
		t.Fatalf("unauthenticated preview status=%d store calls=%d", response.Code, len(store.stateCall))
	}

	authenticated := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	unknown := httptest.NewRecorder()
	authenticated.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/unknown/guidance-preview", nil))
	if unknown.Code != http.StatusNotFound || len(store.stateCall) != 0 {
		t.Fatalf("unknown-skill preview status=%d store calls=%d", unknown.Code, len(store.stateCall))
	}
}

func TestInventoryUsesOneBatchFetchForCompleteCatalog(t *testing.T) {
	catalog := DefaultCatalog().List()
	base := &fakeOwnerSelectionStore{states: map[string]SelectionState{
		"frontend-design": {Enabled: true},
	}}
	store := &fakeBatchOwnerSelectionStore{fakeOwnerSelectionStore: base}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-batch")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(store.batchCalls) != 1 {
		t.Fatalf("batch calls = %d, want one", len(store.batchCalls))
	}
	call := store.batchCalls[0]
	if call.owner != "owner-batch" || !reflect.DeepEqual(call.skills, catalog) {
		t.Fatalf("batch request was not owner-scoped to the full catalog: owner=%q skills=%d", call.owner, len(call.skills))
	}
	if len(base.stateCall) != 0 {
		t.Fatalf("batch inventory also made per-skill calls: %d", len(base.stateCall))
	}
	var got inventoryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != len(catalog) {
		t.Fatalf("batch inventory skill count = %d, want %d", len(got.Skills), len(catalog))
	}
	if !got.Skills[0].Enabled {
		t.Fatalf("batch inventory did not map the enabled state: %+v", got.Skills[0])
	}
}

func TestBatchInventoryFailureReturnsNoPartialInventory(t *testing.T) {
	store := &fakeBatchOwnerSelectionStore{
		fakeOwnerSelectionStore: &fakeOwnerSelectionStore{},
		batchErr:                errors.New("private batch store detail"),
	}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-batch")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private batch store detail") || strings.Contains(response.Body.String(), `"skills"`) {
		t.Fatalf("batch failure leaked details or a partial inventory: %d %s", response.Code, response.Body.String())
	}
	if len(store.batchCalls) != 1 || len(store.stateCall) != 0 {
		t.Fatalf("unexpected calls after batch failure: batch=%d individual=%d", len(store.batchCalls), len(store.stateCall))
	}
}

func TestBatchInventoryMissingStateReturnsNoPartialInventory(t *testing.T) {
	store := &fakeBatchOwnerSelectionStore{fakeOwnerSelectionStore: &fakeOwnerSelectionStore{}, omitSkill: "mcp-builder"}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-batch")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"skills"`) {
		t.Fatalf("incomplete batch returned partial inventory: %d %s", response.Code, response.Body.String())
	}
}

func TestOwnerIdentityIsRequiredAndNeverReadsOrWritesStore(t *testing.T) {
	tests := []struct {
		name      string
		withOwner bool
		owner     any
	}{
		{name: "missing"},
		{name: "blank", withOwner: true, owner: "  "},
		{name: "wrong type", withOwner: true, owner: 42},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeOwnerSelectionStore{}
			router := newSkillsTestRouter(NewHandler(store), test.withOwner, test.owner)
			get := httptest.NewRecorder()
			router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/", nil))
			put := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(put, request)
			if get.Code != http.StatusUnauthorized || put.Code != http.StatusUnauthorized {
				t.Fatalf("missing owner status GET=%d PUT=%d", get.Code, put.Code)
			}
			if len(store.stateCall) != 0 || len(store.setCall) != 0 {
				t.Fatalf("store was called without a valid owner: %+v %+v", store.stateCall, store.setCall)
			}
		})
	}
}

func TestUnknownSkillIDDoesNotReachStore(t *testing.T) {
	store := &fakeOwnerSelectionStore{}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	request := httptest.NewRequest(http.MethodPut, "/unknown/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(store.setCall) != 0 {
		t.Fatalf("unknown skill reached store: %+v", store.setCall)
	}
}

func TestSetSelectionAcceptsOnlyEnabledStateAndReviewedGuidance(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantEnabled bool
		status      int
	}{
		{name: "valid true", body: enableSelectionBody(t, "frontend-design"), contentType: "application/json", wantEnabled: true, status: http.StatusOK},
		{name: "valid false", body: `{"enabled":false}`, contentType: "application/json; charset=utf-8", wantEnabled: false, status: http.StatusOK},
		{name: "enable requires both review pins", body: `{"enabled":true}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "reviewed hash must match current guidance", body: selectionBody(strings.Repeat("0", 64), DefaultCatalog().Fingerprint()), contentType: "application/json", wantEnabled: true, status: http.StatusConflict},
		{name: "reviewed catalog fingerprint must match current catalog", body: selectionBody(DefaultCatalog().List()[0].GuidanceSHA256, strings.Repeat("0", 64)), contentType: "application/json", wantEnabled: true, status: http.StatusConflict},
		{name: "empty body", body: "", contentType: "application/json", status: http.StatusBadRequest},
		{name: "missing field", body: `{}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "null", body: `{"enabled":null}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong type", body: `{"enabled":"true"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "number", body: `{"enabled":1}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "unknown field", body: strings.TrimSuffix(enableSelectionBody(t, "frontend-design"), "}") + `,"ownerIdentity":"other"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "client hashes rejected", body: strings.TrimSuffix(enableSelectionBody(t, "frontend-design"), "}") + `,"sourceSHA256":"forged"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "duplicate key", body: `{"enabled":true,"enabled":false}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "array", body: `[{"enabled":true}]`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "malformed", body: `{"enabled":`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "trailing JSON", body: `{"enabled":true}{"enabled":false}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong content type", body: enableSelectionBody(t, "frontend-design"), contentType: "text/plain", status: http.StatusUnsupportedMediaType},
		{name: "too large", body: `{"enabled":true,"padding":"` + strings.Repeat("x", maxSelectionBodyBytes) + `"}`, contentType: "application/json", status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}}
			router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
			request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
			wantCalls := 0
			if test.status == http.StatusOK {
				wantCalls = 1
			}
			if len(store.setCall) != wantCalls {
				t.Fatalf("store set calls = %d, want %d", len(store.setCall), wantCalls)
			}
			if wantCalls == 1 && store.setCall[0].enabled != test.wantEnabled {
				t.Fatalf("store enabled value = %t, want %t", store.setCall[0].enabled, test.wantEnabled)
			}
		})
	}
}

func TestSetSelectionUsesAuthenticatedOwnerAndFullServerSkill(t *testing.T) {
	store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	request := httptest.NewRequest(http.MethodPut, "/mcp-builder/selection", strings.NewReader(enableSelectionBody(t, "mcp-builder")))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var got selectionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != (selectionResponse{Enabled: true}) {
		t.Fatalf("selection response = %+v", got)
	}
	if len(store.setCall) != 1 {
		t.Fatalf("store calls = %d", len(store.setCall))
	}
	call := store.setCall[0]
	var expected Skill
	for _, skill := range DefaultCatalog().List() {
		if skill.ID == "mcp-builder" {
			expected = skill
		}
	}
	if call.owner != "owner-a" || !call.enabled || call.reviewedCatalogFingerprint != DefaultCatalog().Fingerprint() || !reflect.DeepEqual(call.skill, expected) {
		t.Fatalf("store did not receive authenticated owner/full catalog skill: %+v", call)
	}
}

func TestSetSelectionRejectsChangedSourcePinWithUnchangedGuidance(t *testing.T) {
	original := DefaultCatalog()
	oldFingerprint := original.Fingerprint()
	entries := append([]catalogEntry(nil), original.entries...)
	entries[0].skill.SourceSHA256 = strings.Repeat("f", 64)
	changed := Catalog{entries: entries}
	oldSkill := original.List()[0]
	changedSkill := changed.List()[0]
	if oldSkill.GuidanceSHA256 != changedSkill.GuidanceSHA256 || oldSkill.SourceSHA256 == changedSkill.SourceSHA256 {
		t.Fatal("test must change only source identity while keeping guidance unchanged")
	}
	if oldFingerprint == changed.Fingerprint() {
		t.Fatal("catalog fingerprint did not change with a source pin")
	}

	store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}}
	handler := NewHandler(store)
	handler.catalog = changed
	router := newSkillsTestRouter(handler, true, "owner-a")
	request := httptest.NewRequest(http.MethodPut, "/"+changedSkill.ID+"/selection", strings.NewReader(selectionBody(changedSkill.GuidanceSHA256, oldFingerprint)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale source approval status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusConflict)
	}
	if len(store.setCall) != 0 {
		t.Fatalf("stale source approval reached owner-scoped persistence: %+v", store.setCall)
	}
}

func TestCurrentPreviewFingerprintCanAuthorizeEnable(t *testing.T) {
	catalog := DefaultCatalog()
	skill := catalog.List()[0]
	store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	previewResponse := httptest.NewRecorder()
	router.ServeHTTP(previewResponse, httptest.NewRequest(http.MethodGet, "/"+skill.ID+"/guidance-preview", nil))
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview GuidancePreviewResponse
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/"+skill.ID+"/selection", strings.NewReader(selectionBody(preview.CurrentGuidanceSHA256, preview.CatalogFingerprint)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("current preview selection status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(store.setCall) != 1 || store.setCall[0].reviewedCatalogFingerprint != catalog.Fingerprint() {
		t.Fatalf("current preview fingerprint was not passed to persistence: %+v", store.setCall)
	}
}

func TestSetSelectionResponseFailsClosedForReapproval(t *testing.T) {
	store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true, NeedsReapproval: true}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var got selectionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.NeedsReapproval {
		t.Fatalf("inconsistent stale state was not failed closed: %+v", got)
	}
}

func TestSetSelectionResponseIncludesOnlySafeLatestDecisionMetadata(t *testing.T) {
	decidedAt := time.Date(2026, 9, 24, 14, 5, 6, 7000000, time.UTC)
	store := &fakeOwnerSelectionStore{setResult: SelectionState{
		Enabled: true,
		Decision: &SelectionDecisionMetadata{
			ID:             57,
			ActorIdentity:  "authenticated-actor@example.test",
			DecidedAt:      decidedAt,
			GuidanceSHA256: strings.Repeat("b", 64),
		},
	}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-scope-must-not-be-returned")
	request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode mutation response: %v", err)
	}
	if len(body) != 3 || body["enabled"] == nil || body["needsReapproval"] == nil || body["selectionDecision"] == nil {
		t.Fatalf("mutation response fields = %v, want state plus decision metadata", body)
	}
	var decision map[string]json.RawMessage
	if err := json.Unmarshal(body["selectionDecision"], &decision); err != nil {
		t.Fatalf("decode latest decision: %v", err)
	}
	if len(decision) != 3 || decision["id"] == nil || decision["actorIdentity"] == nil || decision["decidedAt"] == nil {
		t.Fatalf("decision response fields = %v, want only id/actorIdentity/decidedAt", decision)
	}
	var got selectionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode typed mutation response: %v", err)
	}
	if !got.Enabled || got.NeedsReapproval || got.Decision == nil || got.Decision.ID != 57 ||
		got.Decision.ActorIdentity != "authenticated-actor@example.test" || !got.Decision.DecidedAt.Equal(decidedAt) {
		t.Fatalf("mutation response = %+v, want current state and exact decision metadata", got)
	}
	for _, forbidden := range []string{"ownerIdentity", "ownerScope", "guidance", "summary"} {
		if strings.Contains(response.Body.String(), `"`+forbidden+`"`) {
			t.Errorf("mutation response exposed forbidden field %q", forbidden)
		}
	}
	if strings.Contains(response.Body.String(), strings.Repeat("b", 64)) {
		t.Fatal("ordinary mutation response exposed the prior approval guidance hash")
	}
}

func TestStoreErrorsAreRedactedAndDoNotReturnPartialInventory(t *testing.T) {
	t.Run("inventory", func(t *testing.T) {
		store := &fakeOwnerSelectionStore{stateErr: errors.New("private database detail"), stateErrSkill: "mcp-builder"}
		router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private database detail") || strings.Contains(response.Body.String(), `"skills"`) {
			t.Fatalf("store error response was not redacted/atomic: %d %s", response.Code, response.Body.String())
		}
	})

	t.Run("selection", func(t *testing.T) {
		store := &fakeOwnerSelectionStore{setErr: errors.New("private database detail")}
		router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
		request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private database detail") {
			t.Fatalf("store error response was not redacted: %d %s", response.Code, response.Body.String())
		}
	})
}

func TestConcurrentInventoryRequestsReturnCompleteOwnerScopedResults(t *testing.T) {
	store := &fakeOwnerSelectionStore{states: map[string]SelectionState{"frontend-design": {Enabled: true}}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	const requests = 16
	var wait sync.WaitGroup
	errCh := make(chan error, requests)
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusOK {
				errCh <- fmt.Errorf("status = %d, body = %s", response.Code, response.Body.String())
				return
			}
			var inventory inventoryResponse
			if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil {
				errCh <- err
				return
			}
			if len(inventory.Skills) != len(DefaultCatalog().List()) {
				errCh <- fmt.Errorf("partial inventory: got %d skills", len(inventory.Skills))
			}
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if got, want := len(store.stateCall), requests*len(DefaultCatalog().List()); got != want {
		t.Fatalf("owner store calls = %d, want %d", got, want)
	}
	for _, call := range store.stateCall {
		if call.owner != "owner-a" {
			t.Errorf("concurrent inventory used wrong owner: %q", call.owner)
		}
	}
}

func TestUnavailableStoreFailsClosed(t *testing.T) {
	router := newSkillsTestRouter(NewHandler(nil), true, "owner-a")
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/", nil))
	putRequest := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(enableSelectionBody(t, "frontend-design")))
	putRequest.Header.Set("Content-Type", "application/json")
	put := httptest.NewRecorder()
	router.ServeHTTP(put, putRequest)
	if get.Code != http.StatusServiceUnavailable || put.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable store status GET=%d PUT=%d", get.Code, put.Code)
	}
}

func TestInventoryRejectsNonCanonicalOwnerWithoutStoreAccess(t *testing.T) {
	store := &fakeOwnerSelectionStore{states: map[string]SelectionState{
		"alice": {Enabled: true},
	}}
	router := newSkillsTestRouter(NewHandler(store), true, " alice ")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("padded subject status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if len(store.stateCall) != 0 || len(store.setCall) != 0 {
		t.Fatalf("invalid owner reached selection store: state=%d set=%d", len(store.stateCall), len(store.setCall))
	}
}

func TestSelectionRequestBodyLimitIsExact(t *testing.T) {
	store := &fakeOwnerSelectionStore{setResult: SelectionState{Enabled: true}}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	valid := enableSelectionBody(t, "frontend-design")
	padding := strings.Repeat(" ", maxSelectionBodyBytes-len(valid))
	body := valid + padding
	if len(body) != maxSelectionBodyBytes {
		t.Fatalf("test body length = %d", len(body))
	}
	request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("exact-limit request status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestDecodeSelectionRequestRejectsOversizedUnknownLengthBody(t *testing.T) {
	store := &fakeOwnerSelectionStore{}
	router := newSkillsTestRouter(NewHandler(store), true, "owner-a")
	request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(fmt.Sprintf(`{"enabled":true,"x":"%s"}`, strings.Repeat("x", maxSelectionBodyBytes))))
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(store.setCall) != 0 {
		t.Fatalf("oversized request reached store: %+v", store.setCall)
	}
}

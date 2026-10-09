package brain_skill_selection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
)

const frontendDesignReviewedGuidanceSHA256 = "42afa3c5e1d970c766dcb269485b891818bbb59da43e2f609476f88f923e626d"

func TestHTTPConsentControlsOwnerScopedTaskGuidance(t *testing.T) {
	service, repository := testService()
	catalog := brainskills.DefaultCatalog()
	skillID := "frontend-design"
	skill := catalogSkill(t, skillID)
	summary, ok := catalog.GuidanceFor(skillID)
	if !ok {
		t.Fatalf("HAI-authored summary for %q is missing", skillID)
	}
	guidanceDigest := sha256.Sum256([]byte(summary))
	if got := hex.EncodeToString(guidanceDigest[:]); got != frontendDesignReviewedGuidanceSHA256 || skill.GuidanceSHA256 != frontendDesignReviewedGuidanceSHA256 {
		t.Fatalf("frontend-design reviewed guidance hash = %q (catalog %q), want %q", got, skill.GuidanceSHA256, frontendDesignReviewedGuidanceSHA256)
	}

	owner := "owner-http-integration"
	ownerRouter := newBrainSkillIntegrationRouter(service, owner)
	inventoryResponse := performBrainSkillRequest(t, ownerRouter, http.MethodGet, "/api/v1/brain-skills/", "")
	if inventoryResponse.Code != http.StatusOK {
		t.Fatalf("initial inventory status = %d, body = %s", inventoryResponse.Code, inventoryResponse.Body.String())
	}
	inventory := decodeIntegrationJSON(t, inventoryResponse)
	assertExactJSONFields(t, inventory, "initial inventory", "sourceRepository", "sourceCommit", "sourceCommitDate", "catalogFingerprint", "skills")
	if string(inventory["sourceRepository"]) != `"`+brainskills.RepositoryURL+`"` ||
		string(inventory["sourceCommit"]) != `"`+brainskills.SourceCommit+`"` ||
		string(inventory["catalogFingerprint"]) != `"`+catalog.Fingerprint()+`"` {
		t.Fatalf("inventory source pin does not match reviewed catalog: %s", inventoryResponse.Body.String())
	}

	var inventorySkills []map[string]json.RawMessage
	if err := json.Unmarshal(inventory["skills"], &inventorySkills); err != nil {
		t.Fatalf("decode inventory skills: %v", err)
	}
	var initialSkill map[string]json.RawMessage
	for _, entry := range inventorySkills {
		var id string
		if err := json.Unmarshal(entry["id"], &id); err != nil {
			t.Fatalf("decode skill ID: %v", err)
		}
		if id == skillID {
			initialSkill = entry
			break
		}
	}
	if initialSkill == nil {
		t.Fatalf("inventory omitted %q", skillID)
	}
	assertExactJSONFields(t, initialSkill, "inventory skill", "id", "name", "description", "license", "licenseURL", "licensePath", "licenseSHA256", "sourceURL", "sourcePath", "sourceSHA256", "guidanceSHA256", "scope", "status", "enabled", "needsReapproval")
	assertJSONBool(t, initialSkill, "enabled", false)
	assertJSONBool(t, initialSkill, "needsReapproval", false)
	assertJSONString(t, initialSkill, "id", skill.ID)
	assertJSONString(t, initialSkill, "name", skill.Name)
	assertJSONString(t, initialSkill, "description", skill.Purpose)
	assertJSONString(t, initialSkill, "licenseURL", skill.LicenseURL)
	assertJSONString(t, initialSkill, "licensePath", skill.LicensePath)
	assertJSONString(t, initialSkill, "licenseSHA256", skill.LicenseSHA256)
	assertJSONString(t, initialSkill, "sourcePath", skill.SourcePath)
	assertJSONString(t, initialSkill, "sourceSHA256", skill.SourceSHA256)
	assertJSONString(t, initialSkill, "guidanceSHA256", skill.GuidanceSHA256)
	assertNoToolOrApprovalFields(t, inventory)
	if strings.Contains(inventoryResponse.Body.String(), summary) {
		t.Fatal("inventory returned the HAI-authored guidance body")
	}
	if initial := findState(t, mustStates(t, service, owner), skillID); initial.Enabled || initial.Status != StatusNotSelected {
		t.Fatalf("new owner's selection should start disabled: %#v", initial)
	}
	previewResponse := performBrainSkillRequest(t, ownerRouter, http.MethodGet, "/api/v1/brain-skills/"+skillID+"/guidance-preview", "")
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("guidance preview status = %d, body = %s", previewResponse.Code, previewResponse.Body.String())
	}
	preview := decodeIntegrationJSON(t, previewResponse)
	assertExactJSONFields(t, preview, "guidance preview", "skillId", "currentGuidance", "currentGuidanceSHA256", "catalogFingerprint", "authorityBoundary", "previousGuidanceTextStored")
	assertJSONString(t, preview, "skillId", skillID)
	assertJSONString(t, preview, "currentGuidance", summary)
	assertJSONString(t, preview, "currentGuidanceSHA256", skill.GuidanceSHA256)
	assertJSONString(t, preview, "catalogFingerprint", catalog.Fingerprint())
	assertJSONString(t, preview, "authorityBoundary", skill.Boundary)
	assertJSONBool(t, preview, "previousGuidanceTextStored", false)
	if strings.Contains(previewResponse.Body.String(), "SKILL.md body") {
		t.Fatal("approval preview returned upstream instruction content")
	}

	enableRequestBody := `{"enabled":true,"reviewedGuidanceSHA256":"` + frontendDesignReviewedGuidanceSHA256 + `","reviewedCatalogFingerprint":"` + catalog.Fingerprint() + `"}`
	enableResponse := performBrainSkillRequest(t, ownerRouter, http.MethodPut, "/api/v1/brain-skills/"+skillID+"/selection", enableRequestBody)
	if enableResponse.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", enableResponse.Code, enableResponse.Body.String())
	}
	enableResponseBody := decodeIntegrationJSON(t, enableResponse)
	assertExactJSONFields(t, enableResponseBody, "enable response", "enabled", "needsReapproval", "selectionDecision")
	assertJSONBool(t, enableResponseBody, "enabled", true)
	assertJSONBool(t, enableResponseBody, "needsReapproval", false)
	assertNoToolOrApprovalFields(t, enableResponseBody)

	events := repository.allEvents()
	if len(events) != 1 {
		t.Fatalf("consent event count = %d, want 1: %#v", len(events), events)
	}
	event := events[0]
	if event.OwnerIdentity != owner || event.ActorIdentity != owner || event.SkillID != skill.ID || !event.Enabled ||
		event.SourceCommit != skill.Commit || event.SourceSHA256 != skill.SourceSHA256 || event.GuidanceSHA256 != skill.GuidanceSHA256 {
		t.Fatalf("persisted consent does not bind the authenticated owner to the current catalog pin: %#v", event)
	}
	var enableDecision map[string]json.RawMessage
	if err := json.Unmarshal(enableResponseBody["selectionDecision"], &enableDecision); err != nil {
		t.Fatalf("decode enable decision metadata: %v", err)
	}
	assertExactJSONFields(t, enableDecision, "enable decision", "id", "actorIdentity", "decidedAt")
	var enableDecisionID int64
	if err := json.Unmarshal(enableDecision["id"], &enableDecisionID); err != nil || enableDecisionID != event.ID {
		t.Fatalf("enable decision ID = %d, err=%v; want %d", enableDecisionID, err, event.ID)
	}
	assertJSONString(t, enableDecision, "actorIdentity", owner)
	var enableDecidedAt time.Time
	if err := json.Unmarshal(enableDecision["decidedAt"], &enableDecidedAt); err != nil || !enableDecidedAt.Equal(event.DecidedAt) {
		t.Fatalf("enable decision time = %s, err=%v; want %s", enableDecidedAt, err, event.DecidedAt)
	}
	afterEnableResponse := performBrainSkillRequest(t, ownerRouter, http.MethodGet, "/api/v1/brain-skills/", "")
	if afterEnableResponse.Code != http.StatusOK {
		t.Fatalf("inventory after enable status = %d, body = %s", afterEnableResponse.Code, afterEnableResponse.Body.String())
	}
	afterEnable := decodeIntegrationJSON(t, afterEnableResponse)
	var afterEnableSkills []map[string]json.RawMessage
	if err := json.Unmarshal(afterEnable["skills"], &afterEnableSkills); err != nil {
		t.Fatalf("decode inventory after enable: %v", err)
	}
	enabledInventorySkill := integrationSkillByID(t, afterEnableSkills, skillID)
	var decision map[string]json.RawMessage
	if err := json.Unmarshal(enabledInventorySkill["selectionDecision"], &decision); err != nil {
		t.Fatalf("decode latest decision metadata: %v", err)
	}
	assertExactJSONFields(t, decision, "latest selection decision", "id", "actorIdentity", "decidedAt")
	var decisionID int64
	if err := json.Unmarshal(decision["id"], &decisionID); err != nil {
		t.Fatalf("decode decision ID: %v", err)
	}
	if decisionID != event.ID {
		t.Fatalf("inventory decision ID = %d, want %d", decisionID, event.ID)
	}
	assertJSONString(t, decision, "actorIdentity", event.ActorIdentity)
	var inventoryDecidedAt time.Time
	if err := json.Unmarshal(decision["decidedAt"], &inventoryDecidedAt); err != nil {
		t.Fatalf("decode decision timestamp: %v", err)
	}
	if !inventoryDecidedAt.Equal(event.DecidedAt) {
		t.Fatalf("inventory decision timestamp = %s, want %s", inventoryDecidedAt, event.DecidedAt)
	}

	selectedDecisions, err := service.EnabledForOwner(context.Background(), owner)
	if err != nil {
		t.Fatalf("load persisted enabled consent: %v", err)
	}
	if len(selectedDecisions) != 1 || selectedDecisions[0].ID != event.ID ||
		selectedDecisions[0].SourceCommit != event.SourceCommit ||
		selectedDecisions[0].SourceSHA256 != event.SourceSHA256 ||
		selectedDecisions[0].GuidanceSHA256 != event.GuidanceSHA256 {
		t.Fatalf("effective task consent does not resolve to the persisted enable event %d: %#v", event.ID, selectedDecisions)
	}

	applied, err := service.GuidanceForTask(context.Background(), owner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatalf("GuidanceForTask for consenting owner: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("matching task guidance count = %d, want 1: %#v", len(applied), applied)
	}
	wantDigest := sha256.Sum256([]byte(summary))
	wantGuidanceHash := hex.EncodeToString(wantDigest[:])
	wantApplied := AppliedGuidance{
		ID: skill.ID, Name: skill.Name, Commit: skill.Commit,
		SourceSHA256: skill.SourceSHA256, GuidanceSHA256: wantGuidanceHash,
		SelectionDecisionID: event.ID, Summary: summary,
	}
	if applied[0] != wantApplied || applied[0].GuidanceSHA256 != skill.GuidanceSHA256 {
		t.Fatalf("applied task guidance differs from current HAI-authored summary and catalog hashes: got %#v, want %#v", applied[0], wantApplied)
	}
	if applied[0].SelectionDecisionID != event.ID {
		t.Fatalf("task guidance consent decision ID = %d, want persisted enable-event ID %d", applied[0].SelectionDecisionID, event.ID)
	}

	otherOwner := "owner-http-other"
	otherRouter := newBrainSkillIntegrationRouter(service, otherOwner)
	otherInventoryResponse := performBrainSkillRequest(t, otherRouter, http.MethodGet, "/api/v1/brain-skills/", "")
	if otherInventoryResponse.Code != http.StatusOK {
		t.Fatalf("other owner's inventory status = %d, body = %s", otherInventoryResponse.Code, otherInventoryResponse.Body.String())
	}
	otherInventory := decodeIntegrationJSON(t, otherInventoryResponse)
	var otherSkills []map[string]json.RawMessage
	if err := json.Unmarshal(otherInventory["skills"], &otherSkills); err != nil {
		t.Fatalf("decode other owner's skills: %v", err)
	}
	otherSkill := integrationSkillByID(t, otherSkills, skillID)
	assertJSONBool(t, otherSkill, "enabled", false)
	assertJSONBool(t, otherSkill, "needsReapproval", false)
	otherApplied, err := service.GuidanceForTask(context.Background(), otherOwner, "frontend", "Redesign the dashboard")
	if err != nil || len(otherApplied) != 0 {
		t.Fatalf("another owner received guidance: %#v, %v", otherApplied, err)
	}
	if state := findState(t, mustStates(t, service, otherOwner), skillID); state.Enabled || state.Status != StatusNotSelected {
		t.Fatalf("another owner's stored state changed: %#v", state)
	}

	disableResponse := performBrainSkillRequest(t, ownerRouter, http.MethodPut, "/api/v1/brain-skills/"+skillID+"/selection", `{"enabled":false}`)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", disableResponse.Code, disableResponse.Body.String())
	}
	disableBody := decodeIntegrationJSON(t, disableResponse)
	assertExactJSONFields(t, disableBody, "disable response", "enabled", "needsReapproval", "selectionDecision")
	assertJSONBool(t, disableBody, "enabled", false)
	assertJSONBool(t, disableBody, "needsReapproval", false)
	assertNoToolOrApprovalFields(t, disableBody)

	revoked, err := service.GuidanceForTask(context.Background(), owner, "frontend", "Redesign the dashboard")
	if err != nil || len(revoked) != 0 {
		t.Fatalf("guidance remained available after revocation: %#v, %v", revoked, err)
	}
	events = repository.allEvents()
	if len(events) != 2 || !events[0].Enabled || events[1].Enabled || events[1].OwnerIdentity != owner ||
		events[1].SkillID != skill.ID || events[1].SourceCommit != skill.Commit ||
		events[1].SourceSHA256 != skill.SourceSHA256 || events[1].GuidanceSHA256 != skill.GuidanceSHA256 {
		t.Fatalf("append-only consent/revocation history is incorrect: %#v", events)
	}
	var disableDecision map[string]json.RawMessage
	if err := json.Unmarshal(disableBody["selectionDecision"], &disableDecision); err != nil {
		t.Fatalf("decode disable decision metadata: %v", err)
	}
	assertExactJSONFields(t, disableDecision, "disable decision", "id", "actorIdentity", "decidedAt")
	var disableDecisionID int64
	if err := json.Unmarshal(disableDecision["id"], &disableDecisionID); err != nil || disableDecisionID != events[1].ID {
		t.Fatalf("disable decision ID = %d, err=%v; want %d", disableDecisionID, err, events[1].ID)
	}
	assertJSONString(t, disableDecision, "actorIdentity", owner)
	var disableDecidedAt time.Time
	if err := json.Unmarshal(disableDecision["decidedAt"], &disableDecidedAt); err != nil || !disableDecidedAt.Equal(events[1].DecidedAt) {
		t.Fatalf("disable decision time = %s, err=%v; want %s", disableDecidedAt, err, events[1].DecidedAt)
	}
	afterDisableResponse := performBrainSkillRequest(t, ownerRouter, http.MethodGet, "/api/v1/brain-skills/", "")
	if afterDisableResponse.Code != http.StatusOK {
		t.Fatalf("inventory after disable status = %d, body = %s", afterDisableResponse.Code, afterDisableResponse.Body.String())
	}
	afterDisable := decodeIntegrationJSON(t, afterDisableResponse)
	if err := json.Unmarshal(afterDisable["skills"], &afterEnableSkills); err != nil {
		t.Fatalf("decode inventory after disable: %v", err)
	}
	disabledInventorySkill := integrationSkillByID(t, afterEnableSkills, skillID)
	var disabledDecision map[string]json.RawMessage
	if err := json.Unmarshal(disabledInventorySkill["selectionDecision"], &disabledDecision); err != nil {
		t.Fatalf("decode latest disabled decision: %v", err)
	}
	var disabledDecisionID int64
	if err := json.Unmarshal(disabledDecision["id"], &disabledDecisionID); err != nil {
		t.Fatalf("decode disabled decision ID: %v", err)
	}
	if disabledDecisionID != events[1].ID || events[1].Enabled {
		t.Fatalf("inventory did not expose the latest disable event: id=%d event=%#v", disabledDecisionID, events[1])
	}
	if state := findState(t, mustStates(t, service, owner), skillID); state.Enabled || state.Status != StatusDisabled {
		t.Fatalf("revoked owner's state is not disabled: %#v", state)
	}
}

type interleavingSelectionRepository struct {
	*memoryRepository
	interleaved bool
}

func (r *interleavingSelectionRepository) Append(ctx context.Context, event SelectionEvent) (SelectionEvent, error) {
	saved, err := r.memoryRepository.Append(ctx, event)
	if err != nil || r.interleaved || !event.Enabled {
		return saved, err
	}
	r.interleaved = true
	newer := event
	newer.ID = 0
	newer.Enabled = false
	newer.DecidedAt = event.DecidedAt.Add(1)
	if _, err := r.memoryRepository.Append(ctx, newer); err != nil {
		return SelectionEvent{}, err
	}
	return saved, nil
}

func TestHTTPSelectionResponseReflectsLatestConcurrentDecision(t *testing.T) {
	base := &memoryRepository{}
	repository := &interleavingSelectionRepository{memoryRepository: base}
	service := NewSelectionService(repository, brainskills.DefaultCatalog())
	service.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }
	owner := "owner-http-race"
	router := newBrainSkillIntegrationRouter(service, owner)

	frontendSkill := catalogSkill(t, "frontend-design")
	if frontendSkill.GuidanceSHA256 != frontendDesignReviewedGuidanceSHA256 {
		t.Fatalf("frontend-design catalog guidance hash = %q, want %q", frontendSkill.GuidanceSHA256, frontendDesignReviewedGuidanceSHA256)
	}
	response := performBrainSkillRequest(t, router, http.MethodPut, "/api/v1/brain-skills/frontend-design/selection", `{"enabled":true,"reviewedGuidanceSHA256":"`+frontendDesignReviewedGuidanceSHA256+`","reviewedCatalogFingerprint":"`+currentCatalogFingerprint()+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("selection status = %d, body = %s", response.Code, response.Body.String())
	}
	body := decodeIntegrationJSON(t, response)
	assertExactJSONFields(t, body, "concurrent selection response", "enabled", "needsReapproval", "selectionDecision", "supersededByConcurrentDecision")
	assertJSONBool(t, body, "enabled", false)
	assertJSONBool(t, body, "needsReapproval", false)
	assertJSONBool(t, body, "supersededByConcurrentDecision", true)

	guidance, err := service.GuidanceForTask(context.Background(), owner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatalf("load effective task guidance: %v", err)
	}
	if len(guidance) != 0 {
		t.Fatalf("task runtime still sees the concurrently revoked skill: %#v", guidance)
	}
	events := base.allEvents()
	if len(events) != 2 || !events[0].Enabled || events[1].Enabled || events[1].ID <= events[0].ID {
		t.Fatalf("interleaved append history = %#v", events)
	}
	var latestDecision map[string]json.RawMessage
	if err := json.Unmarshal(body["selectionDecision"], &latestDecision); err != nil {
		t.Fatalf("decode concurrently updated decision metadata: %v", err)
	}
	assertExactJSONFields(t, latestDecision, "concurrent latest decision", "id", "actorIdentity", "decidedAt")
	var latestDecisionID int64
	if err := json.Unmarshal(latestDecision["id"], &latestDecisionID); err != nil || latestDecisionID != events[1].ID {
		t.Fatalf("selection response decision ID = %d, err=%v; want latest %d", latestDecisionID, err, events[1].ID)
	}
	assertJSONString(t, latestDecision, "actorIdentity", owner)
	var latestDecidedAt time.Time
	if err := json.Unmarshal(latestDecision["decidedAt"], &latestDecidedAt); err != nil || !latestDecidedAt.Equal(events[1].DecidedAt) {
		t.Fatalf("selection response decision time = %s, err=%v; want %s", latestDecidedAt, err, events[1].DecidedAt)
	}
}

func newBrainSkillIntegrationRouter(service *SelectionService, owner string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, owner)
		c.Next()
	})
	handler := brainskills.NewHandler(NewOwnerSelectionStoreAdapter(service))
	routes := router.Group("/api/v1/brain-skills")
	routes.GET("/", handler.Inventory)
	routes.GET("/:id/guidance-preview", handler.GuidancePreview)
	routes.PUT("/:id/selection", handler.SetSelection)
	return router
}

func performBrainSkillRequest(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeIntegrationJSON(t *testing.T, response *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode handler response JSON: %v; body = %s", err, response.Body.String())
	}
	return result
}

func integrationSkillByID(t *testing.T, skills []map[string]json.RawMessage, id string) map[string]json.RawMessage {
	t.Helper()
	for _, skill := range skills {
		var gotID string
		if err := json.Unmarshal(skill["id"], &gotID); err != nil {
			t.Fatalf("decode skill id: %v", err)
		}
		if gotID == id {
			return skill
		}
	}
	t.Fatalf("inventory omitted skill %q", id)
	return nil
}

func assertExactJSONFields(t *testing.T, object map[string]json.RawMessage, label string, fields ...string) {
	t.Helper()
	want := make(map[string]bool, len(fields))
	for _, field := range fields {
		want[field] = true
	}
	if len(object) != len(want) {
		t.Fatalf("%s has fields %v, want exactly %v", label, integrationMapKeys(object), fields)
	}
	for field := range object {
		if !want[field] {
			t.Fatalf("%s unexpectedly contains field %q", label, field)
		}
	}
}

func assertJSONBool(t *testing.T, object map[string]json.RawMessage, field string, want bool) {
	t.Helper()
	var got bool
	if err := json.Unmarshal(object[field], &got); err != nil {
		t.Fatalf("decode %q: %v", field, err)
	}
	if got != want {
		t.Fatalf("field %q = %t, want %t", field, got, want)
	}
}

func assertJSONString(t *testing.T, object map[string]json.RawMessage, field, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(object[field], &got); err != nil {
		t.Fatalf("decode %q: %v", field, err)
	}
	if got != want {
		t.Fatalf("field %q = %q, want %q", field, got, want)
	}
}

func assertNoToolOrApprovalFields(t *testing.T, object map[string]json.RawMessage) {
	t.Helper()
	for field, raw := range object {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "tool") || (strings.Contains(lower, "approval") && lower != "needsreapproval") ||
			strings.Contains(lower, "rawbody") || strings.Contains(lower, "rawcontent") {
			t.Fatalf("response unexpectedly contains capability or raw-body field %q", field)
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil && nested != nil {
			assertNoToolOrApprovalFields(t, nested)
			continue
		}
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) == nil {
			for _, entry := range list {
				var nestedEntry map[string]json.RawMessage
				if json.Unmarshal(entry, &nestedEntry) == nil && nestedEntry != nil {
					assertNoToolOrApprovalFields(t, nestedEntry)
				}
			}
		}
	}
}

func integrationMapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

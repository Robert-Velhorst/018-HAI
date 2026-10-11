package ambientmonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/outcomeevaluation"

	"github.com/gin-gonic/gin"
)

type fakeOutcomeReader struct {
	records map[string]outcomeevaluation.OutcomeRevision
	err     error
}

func (r *fakeOutcomeReader) GetOutcome(_ context.Context, owner, workspace, outcome string) (outcomeevaluation.OutcomeRevision, error) {
	if r.err != nil {
		return outcomeevaluation.OutcomeRevision{}, r.err
	}
	record, ok := r.records[owner+"\x00"+workspace+"\x00"+outcome]
	if !ok {
		return outcomeevaluation.OutcomeRevision{}, outcomeevaluation.ErrNotFound
	}
	return record, nil
}

func TestRegisterRoutesRequiresCompleteSecurityBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := NewService(NewMemoryRepository(), nil, nil)
	handler := NewHandler(service, &fakeOutcomeReader{records: map[string]outcomeevaluation.OutcomeRevision{}})
	complete := ambientMonitorTestGuards()
	tests := []struct {
		name  string
		clear func(*RouteGuards)
	}{
		{"authenticated owner", func(value *RouteGuards) { value.AuthenticatedOwner = nil }},
		{"recognized role", func(value *RouteGuards) { value.RecognizedRole = nil }},
		{"read", func(value *RouteGuards) { value.Read = nil }},
		{"write", func(value *RouteGuards) { value.Write = nil }},
		{"govern", func(value *RouteGuards) { value.Govern = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			guards := complete
			test.clear(&guards)
			if err := RegisterRoutes(gin.New().Group("/api/v1"), handler, guards); err == nil {
				t.Fatal("RegisterRoutes() accepted an incomplete security boundary")
			}
		})
	}
	if err := RegisterRoutes(nil, handler, complete); err == nil {
		t.Fatal("RegisterRoutes() accepted a nil route group")
	}
	if err := RegisterRoutes(gin.New().Group("/api/v1"), NewHandler(service, nil), complete); err == nil {
		t.Fatal("RegisterRoutes() accepted a missing outcome reader")
	}
}

func TestRegisterRoutesExposesExpectedGuardedSurface(t *testing.T) {
	engine, _, _ := newAmbientMonitorHTTPTest(t)
	want := map[string]bool{
		"GET /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor":                                             false,
		"PUT /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor":                                             false,
		"PATCH /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor/:targetId/enabled":                         false,
		"GET /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor/:targetId/observations":                      false,
		"GET /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor/:targetId/runs":                              false,
		"GET /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor/:targetId/compositions":                      false,
		"GET /api/v1/outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/monitor/:targetId/compositions/:deliveryId/attempts": false,
		"POST /api/v1/outcome-evaluations/workspaces/:workspaceId/monitors/run-due":                                                       false,
		"POST /api/v1/outcome-evaluations/workspaces/:workspaceId/monitors/recover":                                                       false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}

func TestMonitorLifecycleReplayFilteringAndEvidenceHistory(t *testing.T) {
	engine, service, now := newAmbientMonitorHTTPTest(t)
	base := "/api/v1/outcome-evaluations/workspaces/workspace-hai"
	body := map[string]any{
		"idempotencyKey": "register-monitor-1", "targetId": "target-one", "indicatorId": "indicator-open",
		"sourceKind": SourceWorkflowOpenLoopCount, "enabled": true, "cadenceSeconds": 600, "firstRunAt": now,
	}
	created := performAmbientRequest(engine, http.MethodPut, base+"/outcomes/outcome-one/monitor", ambientJSON(t, body), "owner-robert", "owner", "application/json")
	if created.Code != http.StatusCreated {
		t.Fatalf("create monitor status = %d body=%s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"cadenceSeconds":600`) || strings.Contains(created.Body.String(), `"cadence":`) {
		t.Fatalf("create monitor cadence contract is not expressed in seconds: %s", created.Body.String())
	}
	assertNoAmbientAuthority(t, created.Body.Bytes())

	replay := performAmbientRequest(engine, http.MethodPut, base+"/outcomes/outcome-one/monitor", ambientJSON(t, body), "owner-robert", "owner", "application/json")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"created":false`) {
		t.Fatalf("exact replay status = %d body=%s", replay.Code, replay.Body.String())
	}

	second := testRegisterRequest(Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}, now)
	second.IdempotencyKey = "register-monitor-2"
	second.TargetID = "target-two"
	second.OutcomeID = "outcome-two"
	second.IndicatorID = "indicator-open"
	if _, _, err := service.RegisterTarget(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	list := performAmbientRequest(engine, http.MethodGet, base+"/outcomes/outcome-one/monitor", nil, "owner-robert", "viewer", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "target-one") || strings.Contains(list.Body.String(), "target-two") {
		t.Fatalf("filtered target list status = %d body=%s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"cadenceSeconds":600`) || strings.Contains(list.Body.String(), `"cadence":`) {
		t.Fatalf("target list cadence contract is not expressed in seconds: %s", list.Body.String())
	}

	runBody := ambientJSON(t, map[string]any{"workerId": "worker-http", "asOf": now, "leaseSeconds": 60, "limit": 10})
	run := performAmbientRequest(engine, http.MethodPost, base+"/monitors/run-due", runBody, "owner-robert", "operator", "application/json")
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"claimed":2`) || !strings.Contains(run.Body.String(), `"completions"`) {
		t.Fatalf("run due status = %d body=%s", run.Code, run.Body.String())
	}
	assertNoAmbientAuthority(t, run.Body.Bytes())

	observations := performAmbientRequest(engine, http.MethodGet, base+"/outcomes/outcome-one/monitor/target-one/observations?limit=10", nil, "owner-robert", "viewer", "")
	if observations.Code != http.StatusOK || !strings.Contains(observations.Body.String(), `"observations":[{`) {
		t.Fatalf("observations status = %d body=%s", observations.Code, observations.Body.String())
	}
	runs := performAmbientRequest(engine, http.MethodGet, base+"/outcomes/outcome-one/monitor/target-one/runs?limit=10", nil, "owner-robert", "viewer", "")
	if runs.Code != http.StatusOK || !strings.Contains(runs.Body.String(), `"status":"completed"`) {
		t.Fatalf("runs status = %d body=%s", runs.Code, runs.Body.String())
	}

	crossOutcome := performAmbientRequest(engine, http.MethodGet, base+"/outcomes/outcome-two/monitor/target-one/runs", nil, "owner-robert", "viewer", "")
	if crossOutcome.Code != http.StatusNotFound || strings.Contains(crossOutcome.Body.String(), "outcome-one") {
		t.Fatalf("cross-outcome lookup status = %d body=%s", crossOutcome.Code, crossOutcome.Body.String())
	}

	recoveryTarget := testRegisterRequest(Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}, now)
	recoveryTarget.IdempotencyKey = "register-recovery-target"
	recoveryTarget.TargetID = "target-recovery"
	recoveryTarget.OutcomeID = "outcome-one"
	recoveryTarget.IndicatorID = "indicator-open"
	if _, _, err := service.RegisterTarget(t.Context(), recoveryTarget); err != nil {
		t.Fatal(err)
	}
	if claimed, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: recoveryTarget.Scope, WorkerID: "worker-stale", Now: now, LeaseDuration: 5 * time.Second, Limit: 1}); err != nil || len(claimed) != 1 {
		t.Fatalf("recovery claim = (%+v, %v)", claimed, err)
	}
	recoverBody := ambientJSON(t, map[string]any{"asOf": now.Add(6 * time.Second)})
	recovered := performAmbientRequest(engine, http.MethodPost, base+"/monitors/recover", recoverBody, "owner-robert", "owner", "application/json")
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), `"recovered":1`) {
		t.Fatalf("recover status = %d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestMonitorConfigurationValidatesOutcomeIndicatorWindowAndEnabledReplay(t *testing.T) {
	engine, _, now := newAmbientMonitorHTTPTest(t)
	path := "/api/v1/outcome-evaluations/workspaces/workspace-hai/outcomes/outcome-one/monitor"
	valid := map[string]any{
		"idempotencyKey": "register-valid", "targetId": "target-valid", "indicatorId": "indicator-open",
		"sourceKind": SourceWorkflowOpenLoopCount, "enabled": true, "cadenceSeconds": 600, "firstRunAt": now,
	}
	for name, mutate := range map[string]func(map[string]any){
		"unknown indicator": func(v map[string]any) { v["indicatorId"] = "indicator-missing" },
		"outside window":    func(v map[string]any) { v["firstRunAt"] = now.Add(72 * time.Hour) },
		"weak cadence":      func(v map[string]any) { v["cadenceSeconds"] = 5 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneAmbientMap(valid)
			mutate(candidate)
			response := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, candidate), "owner-robert", "owner", "application/json")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
	created := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, valid), "owner-robert", "owner", "application/json")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	patchPath := path + "/target-valid/enabled"
	patch := ambientJSON(t, map[string]any{"idempotencyKey": "disable-valid", "enabled": false})
	first := performAmbientRequest(engine, http.MethodPatch, patchPath, patch, "owner-robert", "owner", "application/json")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"enabled":false`) {
		t.Fatalf("disable status = %d body=%s", first.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"cadenceSeconds":600`) || strings.Contains(first.Body.String(), `"cadence":`) {
		t.Fatalf("enable response cadence contract is not expressed in seconds: %s", first.Body.String())
	}
	replay := performAmbientRequest(engine, http.MethodPatch, patchPath, patch, "owner-robert", "owner", "application/json")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"updated":false`) {
		t.Fatalf("disable replay status = %d body=%s", replay.Code, replay.Body.String())
	}
}

func TestRecoverReturnsCollectionAndCompositionLeaseCounts(t *testing.T) {
	engine, service, now := newAmbientMonitorHTTPTest(t)
	scope := Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}

	collectionRequest := testRegisterRequest(scope, now)
	collectionRequest.IdempotencyKey = "register-stale-collection"
	collectionRequest.TargetID = "target-stale-collection"
	collectionRequest.OutcomeID = "outcome-one"
	collectionRequest.IndicatorID = "indicator-open"
	if _, _, err := service.RegisterTarget(t.Context(), collectionRequest); err != nil {
		t.Fatal(err)
	}
	collectionClaims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: "collection-worker", Now: now,
		LeaseDuration: 5 * time.Second, Limit: 1,
	})
	if err != nil || len(collectionClaims) != 1 {
		t.Fatalf("collection claim = (%+v, %v)", collectionClaims, err)
	}

	compositionRequest := testRegisterRequest(scope, now)
	compositionRequest.IdempotencyKey = "register-stale-composition"
	compositionRequest.TargetID = "target-stale-composition"
	compositionRequest.OutcomeID = "outcome-one"
	compositionRequest.IndicatorID = "indicator-open"
	compositionTarget, _, err := service.RegisterTarget(t.Context(), compositionRequest)
	if err != nil {
		t.Fatal(err)
	}
	monitorClaims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: "composition-collector", Now: now,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(monitorClaims) != 1 || monitorClaims[0].ID != compositionTarget.ID {
		t.Fatalf("composition monitor claim = (%+v, %v)", monitorClaims, err)
	}
	completedAt := monitorClaims[0].Lease.ClaimedAt.Add(time.Microsecond)
	completion, err := service.ProcessClaim(t.Context(), ProcessClaimRequest{
		IdempotencyKey: "complete-stale-composition", Scope: scope,
		TargetID: compositionTarget.ID, WorkerID: "composition-collector",
		LeaseGeneration: monitorClaims[0].Lease.Generation, CompletedAt: completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	compositionClaims, err := service.repository.ClaimDueCompositions(
		t.Context(), scope.OwnerID, scope.WorkspaceID, "composition-worker",
		completion.Composition.NextAttemptAt, 5*time.Second, 1,
	)
	if err != nil || len(compositionClaims) != 1 {
		t.Fatalf("composition claim = (%+v, %v)", compositionClaims, err)
	}

	asOf := compositionClaims[0].Lease.ExpiresAt.Add(time.Second)
	response := performAmbientRequest(
		engine, http.MethodPost,
		"/api/v1/outcome-evaluations/workspaces/workspace-hai/monitors/recover",
		ambientJSON(t, map[string]any{"asOf": asOf}),
		"owner-robert", "owner", "application/json",
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"recovered":2`) ||
		!strings.Contains(response.Body.String(), `"collectionRecovered":1`) ||
		!strings.Contains(response.Body.String(), `"compositionRecovered":1`) {
		t.Fatalf("combined recovery status = %d body=%s", response.Code, response.Body.String())
	}

	replay := performAmbientRequest(
		engine, http.MethodPost,
		"/api/v1/outcome-evaluations/workspaces/workspace-hai/monitors/recover",
		ambientJSON(t, map[string]any{"asOf": asOf}),
		"owner-robert", "owner", "application/json",
	)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"recovered":0`) {
		t.Fatalf("recovery replay status = %d body=%s", replay.Code, replay.Body.String())
	}
}

func TestCompositionReadAPIsEnforceScopeTargetBindingAndAdvisoryAuthority(t *testing.T) {
	engine, service, now := newAmbientMonitorHTTPTest(t)
	scope := Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}
	base := "/api/v1/outcome-evaluations/workspaces/workspace-hai/outcomes/outcome-one/monitor"

	first := testRegisterRequest(scope, now)
	first.IdempotencyKey = "register-composition-one"
	first.TargetID = "target-composition-one"
	first.OutcomeID = "outcome-one"
	first.IndicatorID = "indicator-open"
	if _, _, err := service.RegisterTarget(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.IdempotencyKey = "register-composition-two"
	second.TargetID = "target-composition-two"
	if _, _, err := service.RegisterTarget(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	run := performAmbientRequest(engine, http.MethodPost, "/api/v1/outcome-evaluations/workspaces/workspace-hai/monitors/run-due", ambientJSON(t, map[string]any{
		"workerId": "worker-composition-http", "asOf": now, "leaseSeconds": 60, "limit": 10,
	}), "owner-robert", "operator", "application/json")
	if run.Code != http.StatusOK {
		t.Fatalf("run due status = %d body=%s", run.Code, run.Body.String())
	}

	listPath := base + "/target-composition-one/compositions?limit=10"
	list := performAmbientRequest(engine, http.MethodGet, listPath, nil, "owner-robert", "viewer", "")
	if list.Code != http.StatusOK {
		t.Fatalf("composition list status = %d body=%s", list.Code, list.Body.String())
	}
	assertNoAmbientAuthority(t, list.Body.Bytes())
	var listed struct {
		Compositions []CompositionDelivery `json:"compositions"`
		Authority    AuthorityControl      `json:"authority"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Compositions) != 1 || listed.Compositions[0].TargetID != first.TargetID {
		t.Fatalf("compositions = %+v, want one record for %s", listed.Compositions, first.TargetID)
	}
	delivery := listed.Compositions[0]

	attemptPath := base + "/target-composition-one/compositions/" + delivery.ID + "/attempts?limit=10"
	attempts := performAmbientRequest(engine, http.MethodGet, attemptPath, nil, "owner-robert", "viewer", "")
	if attempts.Code != http.StatusOK {
		t.Fatalf("composition attempts status = %d body=%s", attempts.Code, attempts.Body.String())
	}
	assertNoAmbientAuthority(t, attempts.Body.Bytes())
	var attempted struct {
		Attempts  []CompositionAttempt `json:"attempts"`
		Authority AuthorityControl     `json:"authority"`
	}
	if err := json.Unmarshal(attempts.Body.Bytes(), &attempted); err != nil {
		t.Fatal(err)
	}
	if len(attempted.Attempts) != 1 || attempted.Attempts[0].DeliveryID != delivery.ID || attempted.Attempts[0].TargetID != first.TargetID {
		t.Fatalf("attempts = %+v, want one receipt bound to delivery %s and target %s", attempted.Attempts, delivery.ID, first.TargetID)
	}

	otherTarget := performAmbientRequest(engine, http.MethodGet, base+"/target-composition-two/compositions/"+delivery.ID+"/attempts", nil, "owner-robert", "viewer", "")
	if otherTarget.Code != http.StatusNotFound || strings.Contains(otherTarget.Body.String(), delivery.ID) {
		t.Fatalf("cross-target attempt status = %d body=%s", otherTarget.Code, otherTarget.Body.String())
	}
	otherOwner := performAmbientRequest(engine, http.MethodGet, attemptPath, nil, "owner-other", "viewer", "")
	if otherOwner.Code != http.StatusNotFound || strings.Contains(otherOwner.Body.String(), delivery.ID) {
		t.Fatalf("cross-owner attempt status = %d body=%s", otherOwner.Code, otherOwner.Body.String())
	}
	otherWorkspacePath := strings.Replace(attemptPath, "/workspaces/workspace-hai/", "/workspaces/workspace-other/", 1)
	otherWorkspace := performAmbientRequest(engine, http.MethodGet, otherWorkspacePath, nil, "owner-robert", "viewer", "")
	if otherWorkspace.Code != http.StatusNotFound || strings.Contains(otherWorkspace.Body.String(), delivery.ID) {
		t.Fatalf("cross-workspace attempt status = %d body=%s", otherWorkspace.Code, otherWorkspace.Body.String())
	}

	secondList := performAmbientRequest(engine, http.MethodGet, base+"/target-composition-two/compositions?limit=10", nil, "owner-robert", "viewer", "")
	if secondList.Code != http.StatusOK || strings.Contains(secondList.Body.String(), delivery.ID) {
		t.Fatalf("cross-target list status = %d body=%s", secondList.Code, secondList.Body.String())
	}
}

func TestCompositionReadAPIsRejectInvalidLimits(t *testing.T) {
	engine, service, now := newAmbientMonitorHTTPTest(t)
	scope := Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}
	request := testRegisterRequest(scope, now)
	request.IdempotencyKey = "register-composition-limit"
	request.TargetID = "target-composition-limit"
	request.OutcomeID = "outcome-one"
	request.IndicatorID = "indicator-open"
	if _, _, err := service.RegisterTarget(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: "worker-composition-limit", Now: now, LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(result.Completions) != 1 {
		t.Fatalf("process due = (%+v, %v)", result, err)
	}
	deliveryID := result.Completions[0].Composition.ID
	base := "/api/v1/outcome-evaluations/workspaces/workspace-hai/outcomes/outcome-one/monitor/target-composition-limit/compositions"
	for _, path := range []string{
		base + "?limit=0",
		base + "?limit=501",
		base + "?limit=invalid",
		base + "/" + deliveryID + "/attempts?limit=0",
		base + "/" + deliveryID + "/attempts?limit=501",
		base + "/" + deliveryID + "/attempts?limit=invalid",
	} {
		response := performAmbientRequest(engine, http.MethodGet, path, nil, "owner-robert", "viewer", "")
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestMonitorHTTPBoundaryRejectsAuthorityInjectionAndSanitizesFailures(t *testing.T) {
	engine, _, now := newAmbientMonitorHTTPTest(t)
	path := "/api/v1/outcome-evaluations/workspaces/workspace-hai/outcomes/outcome-one/monitor"
	valid := map[string]any{
		"idempotencyKey": "register-secure", "targetId": "target-secure", "indicatorId": "indicator-open",
		"sourceKind": SourceWorkflowOpenLoopCount, "enabled": true, "cadenceSeconds": 600, "firstRunAt": now,
	}
	injected := cloneAmbientMap(valid)
	injected["ownerId"] = "other-owner"
	injected["authority"] = map[string]any{"canExecute": true}
	response := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, injected), "owner-robert", "owner", "application/json")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("authority injection status = %d body=%s", response.Code, response.Body.String())
	}
	viewer := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, valid), "owner-robert", "viewer", "application/json")
	if viewer.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation status = %d", viewer.Code)
	}
	unknownRole := performAmbientRequest(engine, http.MethodGet, path, nil, "owner-robert", "future-role", "")
	if unknownRole.Code != http.StatusForbidden {
		t.Fatalf("unknown role status = %d", unknownRole.Code)
	}
	unauthenticated := performAmbientRequest(engine, http.MethodGet, path, nil, "", "viewer", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	wrongType := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, valid), "owner-robert", "owner", "text/plain")
	if wrongType.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status = %d", wrongType.Code)
	}
	created := performAmbientRequest(engine, http.MethodPut, path, ambientJSON(t, valid), "owner-robert", "owner", "application/json")
	if created.Code != http.StatusCreated {
		t.Fatalf("secure monitor create status = %d body=%s", created.Code, created.Body.String())
	}
	badLimit := performAmbientRequest(engine, http.MethodGet, path+"/target-secure/runs?limit=501", nil, "owner-robert", "viewer", "")
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("bounded limit status = %d body=%s", badLimit.Code, badLimit.Body.String())
	}
	oversized := append([]byte(`{"idempotencyKey":"oversized","targetId":"target-large","indicatorId":"indicator-open","sourceKind":"workflow_open_loop_count","enabled":true,"cadenceSeconds":600,"firstRunAt":"2026-08-05T12:00:00Z","padding":"`), bytes.Repeat([]byte("x"), maxAmbientMonitorRequestBytes)...)
	oversized = append(oversized, []byte(`"}`)...)
	tooLarge := performAmbientRequest(engine, http.MethodPut, path, oversized, "owner-robert", "owner", "application/json")
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status = %d body=%s", tooLarge.Code, tooLarge.Body.String())
	}
	overflowLease := performAmbientRequest(engine, http.MethodPost, "/api/v1/outcome-evaluations/workspaces/workspace-hai/monitors/run-due", ambientJSON(t, map[string]any{
		"workerId": "worker-overflow", "asOf": now, "leaseSeconds": int64(^uint64(0) >> 1), "limit": 1,
	}), "owner-robert", "operator", "application/json")
	if overflowLease.Code != http.StatusBadRequest {
		t.Fatalf("overflow lease status = %d body=%s", overflowLease.Code, overflowLease.Body.String())
	}

	gin.SetMode(gin.TestMode)
	leaking := gin.New()
	service := newService(NewMemoryRepository(), nil, nil, func() time.Time { return now })
	reader := &fakeOutcomeReader{err: errors.New("postgres://admin:database-password@example.test/private")}
	if err := RegisterRoutes(leaking.Group("/api/v1"), NewHandler(service, reader), ambientMonitorTestGuards()); err != nil {
		t.Fatal(err)
	}
	failure := performAmbientRequest(leaking, http.MethodGet, path, nil, "owner-robert", "viewer", "")
	if failure.Code != http.StatusInternalServerError || strings.Contains(failure.Body.String(), "postgres") || strings.Contains(failure.Body.String(), "password") || !strings.Contains(failure.Body.String(), "errorId") {
		t.Fatalf("sanitized failure status = %d body=%s", failure.Code, failure.Body.String())
	}
}

func newAmbientMonitorHTTPTest(t *testing.T) (*gin.Engine, *Service, time.Time) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	collector := &deterministicCollector{value: CollectedObservation{Value: 3, ObservedAt: now, SourceDigest: strings.Repeat("a", 64)}}
	service := newService(NewMemoryRepository(), collector, &recordingSink{}, func() time.Time { return now })
	reader := &fakeOutcomeReader{records: map[string]outcomeevaluation.OutcomeRevision{
		"owner-robert\x00workspace-hai\x00outcome-one": ambientOutcome("owner-robert", "workspace-hai", "outcome-one", now),
		"owner-robert\x00workspace-hai\x00outcome-two": ambientOutcome("owner-robert", "workspace-hai", "outcome-two", now),
	}}
	engine := gin.New()
	if err := RegisterRoutes(engine.Group("/api/v1"), NewHandler(service, reader), ambientMonitorTestGuards()); err != nil {
		t.Fatal(err)
	}
	return engine, service, now
}

func ambientOutcome(owner, workspace, outcome string, now time.Time) outcomeevaluation.OutcomeRevision {
	return outcomeevaluation.OutcomeRevision{Outcome: outcomeevaluation.IntendedOutcome{
		ID: outcome, Scope: outcomeevaluation.Scope{OwnerID: owner, WorkspaceID: workspace},
		Window:     outcomeevaluation.LongitudinalWindow{Start: now.Add(-24 * time.Hour), End: now.Add(48 * time.Hour)},
		Indicators: []outcomeevaluation.Indicator{{ID: "indicator-open", Name: "Open loops", Unit: "count"}},
	}}
}

func ambientMonitorTestGuards() RouteGuards {
	authenticated := func(c *gin.Context) {
		subject := strings.TrimSpace(c.GetHeader("X-Test-Subject"))
		if subject == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		c.Set(identity.ContextSubjectKey, subject)
		c.Next()
	}
	recognized := func(c *gin.Context) {
		switch c.GetHeader("X-Test-Role") {
		case "viewer", "operator", "owner":
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "recognized role required"})
		}
	}
	permission := func(roles ...string) gin.HandlerFunc {
		return func(c *gin.Context) {
			for _, role := range roles {
				if c.GetHeader("X-Test-Role") == role {
					c.Next()
					return
				}
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		}
	}
	return RouteGuards{
		AuthenticatedOwner: authenticated, RecognizedRole: recognized,
		Read: permission("viewer", "operator", "owner"), Write: permission("operator", "owner"), Govern: permission("owner"),
	}
}

func performAmbientRequest(engine *gin.Engine, method, path string, body []byte, subject, role, contentType string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if subject != "" {
		request.Header.Set("X-Test-Subject", subject)
	}
	if role != "" {
		request.Header.Set("X-Test-Role", role)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func ambientJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func cloneAmbientMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func assertNoAmbientAuthority(t *testing.T, payload []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			if label, exists := item["label"]; exists && label == AuthorityLabel {
				for _, capability := range []string{"canExecute", "canDeliver", "canNotify", "canWriteCalendar", "canMutateWorkflow", "canAuthorizeMandate", "canMutateLearning"} {
					if enabled, ok := item[capability].(bool); !ok || enabled {
						t.Fatalf("authority capability %s = %#v, want false", capability, item[capability])
					}
				}
			}
			for _, nested := range item {
				walk(nested)
			}
		case []any:
			for _, nested := range item {
				walk(nested)
			}
		}
	}
	walk(value)
}

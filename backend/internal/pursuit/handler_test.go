package pursuit

import (
	"automation-hub-backend/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestVerifiedActorUsesAuthenticatedPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(identity.ContextSubjectKey, "bundled-idp-user")

	if got := verifiedActor(context, "operator"); got != "bundled-idp-user" {
		t.Fatalf("verifiedActor() = %q, want authenticated principal", got)
	}
}

func TestVerifiedActorCanLeaveOwnerUnsetWithoutSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	if got := verifiedActor(context, ""); got != "" {
		t.Fatalf("verifiedActor() = %q, want empty owner without an authenticated session", got)
	}
}

func TestVerifiedActorDoesNotUseClientSuppliedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	if got := verifiedActor(context, "operator"); got != "operator" {
		t.Fatalf("verifiedActor() = %q, want honest local fallback", got)
	}
}

func TestSettlePortfolioWorkflowHandlerUsesVerifiedOwnerAndReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, _, item, execution := completedPortfolioWorkflowFixture(t, "verified")
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(context *gin.Context) {
		context.Set(identity.ContextSubjectKey, "alice")
		context.Set(identity.ContextRoleKey, "owner")
		context.Next()
	})
	router.POST(
		"/portfolio-execution-proposal-items/:itemId/settle-workflow",
		handler.SettlePortfolioWorkflow,
	)
	payload, err := json.Marshal(PortfolioWorkflowSettlementRequest{
		WorkflowID: execution.WorkflowID.String(), ExpectedItemDigest: item.RecordDigest,
		ActualEffortMinutes: 8, Confirmation: PortfolioWorkflowSettlementConfirmation,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/portfolio-execution-proposal-items/"+item.ID.String()+"/settle-workflow",
		strings.NewReader(string(payload)),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("settlement handler status=%d body=%s", response.Code, response.Body.String())
	}
	var result PortfolioWorkflowSettlementResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.WorkflowID != execution.WorkflowID || result.ProposalItemID != item.ID || result.Replayed {
		t.Fatalf("settlement response=%#v", result)
	}
}

func TestPursuitRoutesRequireAnAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	if _, err := service.Create(CreateRequest{Title: "Private pursuit", OwnerIdentity: "alice"}); err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}
	handler := NewHandler(service)

	unauthenticated := gin.New()
	unauthenticatedRoutes := unauthenticated.Group("/pursuits")
	unauthenticatedRoutes.Use(RequireAuthenticatedOwner())
	unauthenticatedRoutes.GET("/", handler.List)
	recorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pursuit list status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}

	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	authenticatedRoutes := authenticated.Group("/pursuits")
	authenticatedRoutes.Use(RequireAuthenticatedOwner())
	authenticatedRoutes.GET("/", handler.List)
	recorder = httptest.NewRecorder()
	authenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("authenticated pursuit list status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var records []models.Pursuit
	if err := json.Unmarshal(recorder.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode authenticated pursuit list: %v", err)
	}
	if len(records) != 1 || records[0].OwnerIdentity != "alice" {
		t.Fatalf("authenticated pursuit records = %#v", records)
	}
}

func TestDashboardCountsViewReturnsOnlyAggregateQueues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	if _, err := service.Create(CreateRequest{Title: "Count this pursuit", OwnerIdentity: "alice"}); err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/pursuits/dashboard", NewHandler(service).Dashboard)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits/dashboard?view=counts", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("counts dashboard status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var result Dashboard
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode counts dashboard: %v", err)
	}
	if result.Counts["active"] != 1 {
		t.Fatalf("counts dashboard active = %d, want 1", result.Counts["active"])
	}
	if len(result.DecisionQueue) != 0 || len(result.NeedsRobert) != 0 || len(result.RecentlyChanged) != 0 {
		t.Fatalf("counts dashboard retained record queues: %#v", result)
	}
}

func TestPursuitListSummaryOmitsDetailOnlyFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	created, err := service.Create(CreateRequest{
		Title:                "Compact pursuit list",
		Description:          "Long private context",
		OwnerIdentity:        "alice",
		CompletionDefinition: "All evidence is verified.",
		SuccessCriteria:      []models.PursuitSuccessCriterion{{ID: "criterion-1", Description: "Evidence verified"}},
		ResourceLimits:       models.PursuitResourceLimits{MaxEffortHours: 4},
	})
	if err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/pursuits/", NewHandler(service).List)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits/?view=summary", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("summary list status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var records []models.Pursuit
	if err := json.Unmarshal(recorder.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode summary list: %v", err)
	}
	if len(records) != 1 || records[0].ID != created.ID || records[0].Title != created.Title {
		t.Fatalf("summary list identity = %#v", records)
	}
	if records[0].OwnerIdentity != "" || records[0].Description != "" || records[0].CompletionDefinition != "" || records[0].SuccessCriteria != nil || records[0].ResourceLimits.MaxEffortHours != 0 {
		t.Fatalf("summary list retained detail-only fields: %#v", records[0])
	}
}

func TestPursuitListRejectsUnknownView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/pursuits/", NewHandler(NewService(newFakeRepo(), nil)).List)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits/?view=everything", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown list view status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestArchiveEndpointRequiresExplicitArchiveIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Keep lifecycle explicit"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	router := gin.New()
	router.POST("/pursuits/:id/archive", NewHandler(service).Archive)

	archive := httptest.NewRecorder()
	router.ServeHTTP(archive, httptest.NewRequest(http.MethodPost, "/pursuits/"+pursuit.ID.String()+"/archive", strings.NewReader(`{"archived":true}`)))
	if archive.Code != http.StatusOK {
		t.Fatalf("archive status = %d, body=%s", archive.Code, archive.Body.String())
	}

	for _, body := range []string{`{"archived":false}`, `{}`, `{`} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/pursuits/"+pursuit.ID.String()+"/archive", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("archive body %q status = %d, want %d; body=%s", body, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
		stored, findErr := repo.FindByID(pursuit.ID)
		if findErr != nil {
			t.Fatalf("FindByID after archive body %q: %v", body, findErr)
		}
		if !stored.Archived || stored.Status != StatusArchived {
			t.Fatalf("archive body %q changed closed pursuit: %#v", body, stored)
		}
	}
}

func TestPursuitEndpointsScopeRecordsToAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice pursuit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bob, err := service.Create(CreateRequest{Title: "Bob pursuit", OwnerIdentity: "bob"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}
	legacy, err := service.Create(CreateRequest{Title: "Local legacy pursuit"})
	if err != nil {
		t.Fatalf("Create legacy pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	handler := NewHandler(service)
	router.GET("/pursuits", handler.List)
	router.GET("/pursuits/:id", handler.Get)
	router.GET("/pursuits/:id/activity", handler.Activity)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/pursuits", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", list.Code, list.Body.String())
	}
	var visible []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &visible); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	seen := map[string]bool{}
	for _, pursuit := range visible {
		seen[pursuit.ID] = true
	}
	if !seen[alice.ID.String()] || seen[legacy.ID.String()] || seen[bob.ID.String()] {
		t.Fatalf("owner-scoped list leaked or hid records: %#v", seen)
	}

	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/pursuits/"+bob.ID.String(), nil))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-owner detail status = %d, want %d; body=%s", denied.Code, http.StatusNotFound, denied.Body.String())
	}

	denied = httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/pursuits/"+bob.ID.String()+"/activity", nil))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-owner activity status = %d, want %d; body=%s", denied.Code, http.StatusNotFound, denied.Body.String())
	}
}

func TestPursuitListAllowsOwnerlessRecordsOnlyForConfiguredLegacyOwner(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "alice")
	repo := newFakeRepo()
	service := NewService(repo, nil)
	legacy, err := service.Create(CreateRequest{Title: "Local legacy pursuit"})
	if err != nil {
		t.Fatalf("Create legacy pursuit: %v", err)
	}

	records, err := service.ListForOwner("alice", false)
	if err != nil {
		t.Fatalf("ListForOwner: %v", err)
	}
	if len(records) != 1 || records[0].ID != legacy.ID {
		t.Fatalf("configured migration owner records = %#v", records)
	}
	if records, err = service.ListForOwner("bob", false); err != nil || len(records) != 0 {
		t.Fatalf("non-migration owner saw legacy records: records=%#v err=%v", records, err)
	}
}

func TestPursuitResourceEndpointsAreOwnerScopedStrictAndIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{
		OwnerIdentity: "alice",
		Title:         "Alice metered pursuit",
		ResourceLimits: models.PursuitResourceLimits{
			MaxEffortHours: 4,
			MaxSpendEUR:    50,
		},
	})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bob, err := service.Create(CreateRequest{OwnerIdentity: "bob", Title: "Bob metered pursuit"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	handler := NewHandler(service)
	router.GET("/pursuits/:id/resources", handler.ResourceUsage)
	router.GET("/pursuits/:id/resource-events", handler.ResourceEvents)
	router.POST("/pursuits/:id/resource-events", handler.AppendResourceEvent)
	router.POST("/pursuits/:id/resource-reservations/:reservationId/release", handler.ReleaseResourceReservation)

	path := "/pursuits/" + alice.ID.String() + "/resource-events"
	body := `{"kind":"effort_recorded","effortHours":1.5,"note":"Reviewed evidence","idempotencyKey":"effort-http-1"}`
	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(first, request)
	if first.Code != http.StatusCreated {
		t.Fatalf("first resource event status = %d, body=%s", first.Code, first.Body.String())
	}
	var created models.PursuitResourceEvent
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode resource event: %v", err)
	}
	if created.Actor != "alice" {
		t.Fatalf("resource event trusted client actor: %#v", created)
	}
	storedEvents, err := repo.FindResourceEventsForOwner("alice", alice.ID, 10)
	if err != nil || len(storedEvents) != 1 || storedEvents[0].OwnerIdentity != "alice" {
		t.Fatalf("stored resource owner = %#v, error=%v", storedEvents, err)
	}

	replay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(replay, request)
	if replay.Code != http.StatusCreated {
		t.Fatalf("idempotent replay status = %d, body=%s", replay.Code, replay.Body.String())
	}
	var replayed models.PursuitResourceEvent
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replayed resource event: %v", err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("idempotent replay ID = %s, want %s", replayed.ID, created.ID)
	}

	conflict := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"kind":"effort_recorded","effortHours":2,"note":"Different event","idempotencyKey":"effort-http-1"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(conflict, request)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}

	spoof := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"kind":"effort_recorded","effortHours":1,"note":"Spoof","idempotencyKey":"effort-http-2","ownerIdentity":"bob","actor":"bob"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(spoof, request)
	if spoof.Code != http.StatusBadRequest {
		t.Fatalf("identity spoof status = %d, want %d; body=%s", spoof.Code, http.StatusBadRequest, spoof.Body.String())
	}

	invalidLimit := httptest.NewRecorder()
	router.ServeHTTP(invalidLimit, httptest.NewRequest(http.MethodGet, path+"?limit=501", nil))
	if invalidLimit.Code != http.StatusBadRequest {
		t.Fatalf("invalid event limit status = %d, want %d", invalidLimit.Code, http.StatusBadRequest)
	}

	usage := httptest.NewRecorder()
	router.ServeHTTP(usage, httptest.NewRequest(http.MethodGet, "/pursuits/"+alice.ID.String()+"/resources", nil))
	if usage.Code != http.StatusOK || !strings.Contains(usage.Body.String(), `"effortRecordedHours":1.5`) {
		t.Fatalf("resource usage status = %d, body=%s", usage.Code, usage.Body.String())
	}

	manager := service.(interface {
		ReservePursuitTaskResources(uuid.UUID, string, string, int64, int64) error
	})
	if err := manager.ReservePursuitTaskResources(alice.ID, "alice", "http-orphan:attempt:1", 15, 0); err != nil {
		t.Fatalf("reserve HTTP reconciliation hold: %v", err)
	}
	var reservationID uuid.UUID
	for _, reservation := range repo.resourceReservations {
		reservationID = reservation.ID
	}
	if reservationID == uuid.Nil {
		t.Fatal("reserved resource hold was not persisted")
	}
	releasePath := "/pursuits/" + alice.ID.String() + "/resource-reservations/" + reservationID.String() + "/release"

	invalidReservation := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/pursuits/"+alice.ID.String()+"/resource-reservations/not-a-uuid/release", strings.NewReader(`{"confirmedOrphan":true,"reason":"The worker process no longer exists."}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalidReservation, request)
	if invalidReservation.Code != http.StatusBadRequest {
		t.Fatalf("invalid reservation id status = %d, want %d; body=%s", invalidReservation.Code, http.StatusBadRequest, invalidReservation.Body.String())
	}

	unconfirmed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, releasePath, strings.NewReader(`{"reason":"The worker process no longer exists."}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(unconfirmed, request)
	if unconfirmed.Code != http.StatusBadRequest || !strings.Contains(unconfirmed.Body.String(), "confirmedOrphan") {
		t.Fatalf("unconfirmed release status = %d, body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}

	released := httptest.NewRecorder()
	releaseBody := `{"confirmedOrphan":true,"reason":"The worker process no longer exists."}`
	request = httptest.NewRequest(http.MethodPost, releasePath, strings.NewReader(releaseBody))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(released, request)
	if released.Code != http.StatusOK || !strings.Contains(released.Body.String(), `"activeReservations":0`) {
		t.Fatalf("confirmed release status = %d, body=%s", released.Code, released.Body.String())
	}
	if settlement := repo.resourceSettlements[reservationID]; settlement.Actor != "alice" || settlement.Reason != "The worker process no longer exists." {
		t.Fatalf("HTTP release trusted the wrong actor or lost its reason: %#v", settlement)
	}

	releaseReplay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, releasePath, strings.NewReader(releaseBody))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(releaseReplay, request)
	if releaseReplay.Code != http.StatusOK {
		t.Fatalf("idempotent release replay status = %d, body=%s", releaseReplay.Code, releaseReplay.Body.String())
	}

	for _, endpoint := range []string{
		"/pursuits/" + bob.ID.String() + "/resources",
		"/pursuits/" + bob.ID.String() + "/resource-events",
	} {
		denied := httptest.NewRecorder()
		router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if denied.Code != http.StatusNotFound {
			t.Fatalf("cross-owner GET %s status = %d, want %d; body=%s", endpoint, denied.Code, http.StatusNotFound, denied.Body.String())
		}
	}
}

func TestPursuitResourceEndpointRejectsOwnerlessLegacyMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	legacy, err := service.Create(CreateRequest{Title: "Legacy resource pursuit"})
	if err != nil {
		t.Fatalf("Create legacy pursuit: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/pursuits/:id/resource-events", NewHandler(service).AppendResourceEvent)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pursuits/"+legacy.ID.String()+"/resource-events", strings.NewReader(`{"kind":"effort_recorded","effortHours":1,"note":"Must not adopt legacy data","idempotencyKey":"legacy-1"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("ownerless legacy mutation status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestPursuitMutationEndpointsRejectOwnerlessLegacyRecords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	legacy, err := service.Create(CreateRequest{Title: "Legacy local pursuit", Description: "Read-compatible migration record"})
	if err != nil {
		t.Fatalf("Create legacy pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	handler := NewHandler(service)
	router.PATCH("/pursuits/:id", handler.Update)
	router.POST("/pursuits/:id/archive", handler.Archive)
	router.POST("/pursuits/:id/reopen", handler.Reopen)
	router.POST("/pursuits/:id/links", handler.Link)
	router.DELETE("/pursuits/:id/links/:linkId", handler.DeleteLink)
	router.POST("/pursuits/:id/intake", handler.Intake)
	router.POST("/pursuits/:id/plan", handler.Plan)
	router.POST("/pursuits/:id/decisions/resolve", handler.ResolveDecision)
	router.POST("/pursuits/:id/summary", handler.RefreshSummary)
	router.POST("/pursuits/:id/review", handler.Review)

	missingLinkID := uuid.New()
	for _, endpoint := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPatch, path: "/pursuits/" + legacy.ID.String(), body: `{"title":"Alice adopts the legacy record"}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/archive", body: `{"archived":true}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/reopen", body: `{}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/links", body: `{"linkType":"memory","linkId":"memory-1","relationship":"evidence"}`},
		{method: http.MethodDelete, path: "/pursuits/" + legacy.ID.String() + "/links/" + missingLinkID.String(), body: ""},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/intake", body: `{}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/plan", body: `{}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/decisions/resolve", body: `{}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/summary", body: `{}`},
		{method: http.MethodPost, path: "/pursuits/" + legacy.ID.String() + "/review", body: `{"action":"complete"}`},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want %d; body=%s", endpoint.method, endpoint.path, recorder.Code, http.StatusNotFound, recorder.Body.String())
		}
	}

	stored, err := repo.FindByID(legacy.ID)
	if err != nil {
		t.Fatalf("Find legacy pursuit: %v", err)
	}
	if stored.OwnerIdentity != "" || stored.Title != legacy.Title || stored.Archived || stored.Status != legacy.Status || stored.CompletionState != legacy.CompletionState {
		t.Fatalf("authenticated mutation changed legacy pursuit: %#v", stored)
	}
	links, err := repo.FindLinks(legacy.ID)
	if err != nil {
		t.Fatalf("Find legacy links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("authenticated mutation linked legacy pursuit: %#v", links)
	}
}

func TestPursuitOptionalBodyActionsRejectMalformedChunkedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{OwnerIdentity: "alice", Title: "Malformed request guard", DesiredOutcome: "Remain unchanged"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.ArchiveForOwner("alice", pursuit.ID, true, "alice"); err != nil {
		t.Fatalf("ArchiveForOwner: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	handler := NewHandler(service)
	router.POST("/pursuits/:id/reopen", handler.Reopen)
	router.POST("/pursuits/:id/summary", handler.RefreshSummary)

	for _, path := range []string{
		"/pursuits/" + pursuit.ID.String() + "/reopen",
		"/pursuits/" + pursuit.ID.String() + "/summary",
	} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"note":`))
		request.Header.Set("Content-Type", "application/json")
		request.ContentLength = -1
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}

	stored, err := repo.FindByID(pursuit.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !stored.Archived || stored.Status != StatusArchived {
		t.Fatalf("malformed optional-body action changed pursuit: %#v", stored)
	}
}

func TestDelegationPackageEndpointDoesNotExposeAnotherOwnersWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice delegation", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bob, err := service.Create(CreateRequest{Title: "Bob delegation", OwnerIdentity: "bob"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/pursuits/:id/delegation", NewHandler(service).DelegationPackage)

	visible := httptest.NewRecorder()
	router.ServeHTTP(visible, httptest.NewRequest(http.MethodGet, "/pursuits/"+alice.ID.String()+"/delegation", nil))
	if visible.Code != http.StatusOK {
		t.Fatalf("own delegation package status = %d, body=%s", visible.Code, visible.Body.String())
	}
	var packageResult PursuitDelegationPackage
	if err := json.Unmarshal(visible.Body.Bytes(), &packageResult); err != nil {
		t.Fatalf("decode delegation package: %v", err)
	}
	if packageResult.PursuitID != alice.ID.String() || packageResult.Title != alice.Title {
		t.Fatalf("delegation package identity = %#v", packageResult)
	}

	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/pursuits/"+bob.ID.String()+"/delegation", nil))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delegation status = %d, want %d; body=%s", denied.Code, http.StatusNotFound, denied.Body.String())
	}
}

func TestCandidateIntakeEndpointRejectsUnacceptedOperationalWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	candidate, err := service.Create(CreateRequest{
		Title:            "Imported candidate",
		OwnerIdentity:    "alice",
		SourceOfCreation: "source_pursuit_candidate",
		Status:           StatusWaiting,
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/pursuits/:id/intake", NewHandler(service).Intake)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/pursuits/"+candidate.ID.String()+"/intake",
		strings.NewReader(`{"input":"Prepare a response from the imported source."}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("candidate intake status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "candidate must be accepted") {
		t.Fatalf("candidate intake response did not explain lifecycle guard: %s", recorder.Body.String())
	}
}

func TestCandidateAcceptanceUsesExplicitApprovalEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	workflows := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflows)
	candidate, err := service.Create(CreateRequest{
		Title:            "Imported candidate",
		OwnerIdentity:    "alice",
		SourceOfCreation: "source_pursuit_candidate",
		Status:           StatusWaiting,
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "owner")
		c.Next()
	})
	handler := NewHandler(service)
	router.POST("/pursuits/:id/plan", handler.Plan)
	router.POST("/pursuits/:id/candidate/accept", handler.AcceptCandidate)

	plan := httptest.NewRecorder()
	planRequest := httptest.NewRequest(http.MethodPost, "/pursuits/"+candidate.ID.String()+"/plan", strings.NewReader(`{}`))
	planRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(plan, planRequest)
	if plan.Code != http.StatusBadRequest || !strings.Contains(plan.Body.String(), "explicit approval action") {
		t.Fatalf("generic candidate plan = %d %s, want explicit acceptance rejection", plan.Code, plan.Body.String())
	}
	if workflows.calls != 0 {
		t.Fatalf("generic candidate plan created %d workflow(s)", workflows.calls)
	}

	accept := httptest.NewRecorder()
	acceptRequest := httptest.NewRequest(http.MethodPost, "/pursuits/"+candidate.ID.String()+"/candidate/accept", strings.NewReader(`{}`))
	acceptRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(accept, acceptRequest)
	if accept.Code != http.StatusCreated {
		t.Fatalf("candidate acceptance = %d, want %d; body=%s", accept.Code, http.StatusCreated, accept.Body.String())
	}
	if workflows.calls != 1 {
		t.Fatalf("candidate acceptance created %d workflow(s), want one", workflows.calls)
	}
}

func TestCandidateAcceptanceEndpointRejectsNonApprover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	workflows := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflows)
	candidate, err := service.Create(CreateRequest{
		Title:            "Imported candidate",
		OwnerIdentity:    "alice",
		SourceOfCreation: "source_pursuit_candidate",
		Status:           StatusWaiting,
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "viewer")
		c.Next()
	})
	router.POST("/pursuits/:id/candidate/accept", NewHandler(service).AcceptCandidate)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pursuits/"+candidate.ID.String()+"/candidate/accept", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-approver candidate acceptance = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if workflows.calls != 0 {
		t.Fatalf("non-approver candidate acceptance created %d workflow(s)", workflows.calls)
	}
}

func TestCandidateAcceptanceEndpointRejectsActivePursuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	workflows := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflows)
	active, err := service.Create(CreateRequest{Title: "Active pursuit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create active pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "owner")
		c.Next()
	})
	router.POST("/pursuits/:id/candidate/accept", NewHandler(service).AcceptCandidate)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pursuits/"+active.ID.String()+"/candidate/accept", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("active pursuit candidate acceptance = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if workflows.calls != 0 {
		t.Fatalf("active pursuit candidate acceptance created %d workflow(s)", workflows.calls)
	}
}

func TestResolveDecisionEndpointRejectsHiddenCrossOwnerCompletionEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	alice, err := service.Create(CreateRequest{Title: "Alice completion", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{
		ID:                 bobWorkflowID,
		OwnerIdentity:      "bob",
		Title:              "Bob verified workflow",
		CurrentState:       "completed",
		VerificationStatus: "verified",
	}
	// Simulate a malformed legacy link that predates owner-aware link checks.
	legacyLinkID := uuid.New()
	repo.links[legacyLinkID] = models.PursuitLink{
		ID:           legacyLinkID,
		PursuitID:    alice.ID,
		LinkType:     LinkWorkflow,
		LinkID:       bobWorkflowID.String(),
		Relationship: "legacy_evidence",
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "owner")
		c.Next()
	})
	router.POST("/pursuits/:id/decisions/resolve", NewHandler(service).ResolveDecision)

	recorder := httptest.NewRecorder()
	body := fmt.Sprintf(`{"decisionId":%q,"decisionType":"pursuit_completion_review","approved":true}`, completionReviewDecisionID(alice.ID))
	request := httptest.NewRequest(http.MethodPost, "/pursuits/"+alice.ID.String()+"/decisions/resolve", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("cross-owner completion status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	stored, err := repo.FindByID(alice.ID)
	if err != nil {
		t.Fatalf("Find Alice pursuit: %v", err)
	}
	if stored.Status == StatusCompleted || stored.CompletionState == CompletionVerified {
		t.Fatalf("hidden cross-owner evidence completed Alice pursuit: %#v", stored)
	}
}

func TestResolveDecisionEndpointRejectsNonApproverBeforeCreatingWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	workflows := &fakeWorkflowIntake{repo: repo}
	service := NewService(repo, workflows)
	pursuit, err := service.Create(CreateRequest{Title: "Approval-gated pursuit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create pursuit: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Set(identity.ContextRoleKey, "viewer")
		c.Next()
	})
	router.POST("/pursuits/:id/decisions/resolve", NewHandler(service).ResolveDecision)

	recorder := httptest.NewRecorder()
	body := fmt.Sprintf(`{"decisionId":%q,"decisionType":"pursuit_next_action","approved":true}`, nextActionDecisionID(pursuit.ID))
	request := httptest.NewRequest(http.MethodPost, "/pursuits/"+pursuit.ID.String()+"/decisions/resolve", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-approver decision resolution = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if workflows.calls != 0 {
		t.Fatalf("non-approver resolution created %d workflow(s)", workflows.calls)
	}
	if activity, activityErr := repo.FindActivities(pursuit.ID, 20); activityErr != nil || len(activity) != 1 {
		t.Fatalf("non-approver resolution changed audit state: activity=%#v err=%v", activity, activityErr)
	}
}

func TestPursuitMatchDoesNotExposeAnotherOwnersSourceLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	bob, err := service.Create(CreateRequest{Title: "Bob private claim", OwnerIdentity: "bob", ProjectKey: "private"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}
	if _, err := service.Link(bob.ID, LinkRequest{LinkType: LinkSourceItem, LinkID: "private-source", Relationship: "evidence"}); err != nil {
		t.Fatalf("Link Bob source: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/pursuits/match", NewHandler(service).Match)

	recorder := httptest.NewRecorder()
	recorderRequest := httptest.NewRequest(http.MethodPost, "/pursuits/match", strings.NewReader(`{"sourceType":"source_item","sourceId":"private-source"}`))
	recorderRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, recorderRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("match status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var matches []MatchCandidate
	if err := json.Unmarshal(recorder.Body.Bytes(), &matches); err != nil {
		t.Fatalf("decode matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("cross-owner source link was matched: %#v", matches)
	}
}

func TestPursuitLinkRejectsAnotherOwnersPrivateRecords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepo()
	service := NewService(repo, nil)
	pursuit, err := service.Create(CreateRequest{Title: "Alice pursuit", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatalf("Create Alice pursuit: %v", err)
	}
	bobPursuit, err := service.Create(CreateRequest{Title: "Bob private pursuit", OwnerIdentity: "bob"})
	if err != nil {
		t.Fatalf("Create Bob pursuit: %v", err)
	}
	bobWorkflowID := uuid.New()
	repo.workflows[bobWorkflowID] = models.WorkflowItem{ID: bobWorkflowID, OwnerIdentity: "bob", Title: "Bob private workflow"}
	bobMemoryID := uuid.New()
	repo.memories[bobMemoryID] = models.ContextMemory{ID: bobMemoryID, OwnerIdentity: "bob", Content: "Bob private memory"}
	bobSourceID := uuid.New()
	repo.sourceOwners[bobSourceID] = "bob"
	repo.sourceItems[uuid.New()] = models.SourceRawItem{ID: uuid.New(), SourceID: bobSourceID, ExternalID: "bob-private-source", Title: "Bob private source"}
	bobVerificationID := uuid.New()
	repo.verificationRuns[bobVerificationID] = models.VerificationRun{ID: bobVerificationID, OwnerIdentity: "bob", Question: "Bob private verification"}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/pursuits/:id/links", NewHandler(service).Link)

	for _, target := range []struct {
		linkType string
		linkID   string
	}{
		{linkType: LinkPursuit, linkID: bobPursuit.ID.String()},
		{linkType: LinkWorkflow, linkID: bobWorkflowID.String()},
		{linkType: LinkMemory, linkID: bobMemoryID.String()},
		{linkType: LinkSourceItem, linkID: "bob-private-source"},
		{linkType: LinkVerification, linkID: bobVerificationID.String()},
	} {
		recorder := httptest.NewRecorder()
		body := fmt.Sprintf(`{"linkType":%q,"linkId":%q,"relationship":"evidence"}`, target.linkType, target.linkID)
		request := httptest.NewRequest(http.MethodPost, "/pursuits/"+pursuit.ID.String()+"/links", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("cross-owner %s link status = %d, want %d; body=%s", target.linkType, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}
	links, err := repo.FindLinks(pursuit.ID)
	if err != nil {
		t.Fatalf("FindLinks: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("cross-owner link was persisted: %#v", links)
	}

	legacyLinkID := uuid.New()
	repo.links[legacyLinkID] = models.PursuitLink{
		ID:           legacyLinkID,
		PursuitID:    pursuit.ID,
		LinkType:     LinkPursuit,
		LinkID:       bobPursuit.ID.String(),
		Relationship: "legacy_related",
	}
	detail, err := service.DetailForOwner("alice", pursuit.ID)
	if err != nil {
		t.Fatalf("DetailForOwner: %v", err)
	}
	if hasPursuitLink(detail.Links, LinkPursuit, bobPursuit.ID.String()) {
		t.Fatalf("legacy cross-owner pursuit link was visible: %#v", detail.Links)
	}
}

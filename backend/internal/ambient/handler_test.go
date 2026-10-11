package ambient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	pursuitpkg "automation-hub-backend/internal/pursuit"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestOverviewHandlerRequiresVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &ambientResolutionService{}
	handler := NewHandler(service)

	unauthenticated := gin.New()
	unauthenticated.GET("/ambient/overview", handler.Overview)
	recorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ambient/overview", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if service.overviewCalls != 0 {
		t.Fatalf("overview service was called without an authenticated owner: %d", service.overviewCalls)
	}

	gated := gin.New()
	gatedRoutes := gated.Group("/ambient")
	gatedRoutes.Use(RequireAuthenticatedOwner())
	gatedRoutes.GET("/overview", handler.Overview)
	recorder = httptest.NewRecorder()
	gated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ambient/overview", nil))
	if recorder.Code != http.StatusUnauthorized || service.overviewCalls != 0 {
		t.Fatalf("route owner gate status/calls = %d/%d, want %d/0; body=%s", recorder.Code, service.overviewCalls, http.StatusUnauthorized, recorder.Body.String())
	}

	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	authenticatedRoutes := authenticated.Group("/ambient")
	authenticatedRoutes.Use(RequireAuthenticatedOwner())
	authenticatedRoutes.GET("/overview", handler.Overview)
	recorder = httptest.NewRecorder()
	authenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ambient/overview", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("authenticated overview status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.overviewCalls != 1 || service.overviewOwner != "alice" {
		t.Fatalf("overview owner = %q calls=%d, want alice/1", service.overviewOwner, service.overviewCalls)
	}
}

func TestAcceptHandlerRejectsAnotherOwnersPursuitOpportunity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pursuitID := uuid.New()
	opportunity := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "safety",
		Title:         "Private pursuit decision",
		Rationale:     "Requires review.",
		NextAction:    "Review private evidence.",
		SourceType:    "pursuit_decision",
		SourceID:      pursuitID.String(),
	}
	workflowSpy := &ambientWorkflowSpy{}
	pursuitSpy := &ambientPursuitSpy{owners: map[uuid.UUID]string{pursuitID: "bob"}}
	handler := NewHandler(NewServiceWithPursuits(&ambientRepositoryStub{opportunity: opportunity}, workflowSpy, nil, pursuitSpy))

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/ambient/:id/accept", handler.Accept)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ambient/"+opportunity.ID.String()+"/accept", strings.NewReader(`{"note":"approve"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("accept status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(workflowSpy.intakeRequests) != 0 {
		t.Fatalf("cross-owner ambient acceptance created workflow work: %#v", workflowSpy.intakeRequests)
	}
}

func TestAcceptHandlerUsesVerifiedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	opportunity := &models.AmbientOpportunity{
		ID:            uuid.New(),
		OwnerIdentity: "alice",
		Status:        StatusProposed,
		NeedKey:       "growth",
		Title:         "Prepare an internal draft",
		Rationale:     "Low-risk preparation.",
		NextAction:    "Prepare the internal draft.",
		SourceType:    "workflow",
		SourceID:      "shared-workflow",
	}
	workflowSpy := &ambientWorkflowSpy{}
	handler := NewHandler(NewService(&ambientRepositoryStub{opportunity: opportunity}, workflowSpy, nil))

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/ambient/:id/accept", handler.Accept)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ambient/"+opportunity.ID.String()+"/accept", strings.NewReader(`{"note":"approve","actor":"untrusted"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(workflowSpy.intakeRequests) != 1 || workflowSpy.intakeRequests[0].Actor != "alice" {
		t.Fatalf("workflow request actor = %#v, want verified alice", workflowSpy.intakeRequests)
	}
}

func TestScanHandlerRequiresVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(&ambientRepositoryStub{needs: defaultNeeds()}, nil, nil))
	engine := gin.New()
	engine.POST("/ambient/scan", handler.Scan)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ambient/scan", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("scan status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestOverviewHandlerDoesNotExposeUnexpectedServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&ambientResolutionService{overviewErr: errors.New(`database password=do-not-expose at C:\\private`)})
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.GET("/ambient/overview", handler.Overview)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ambient/overview", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, "do-not-expose") || strings.Contains(body, `C:\\private`) || !strings.Contains(body, "ambient overview is unavailable") {
		t.Fatalf("overview response exposed internal error: %s", body)
	}
}

func TestUpdateNeedHandlerRequiresVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(&ambientRepositoryStub{needs: defaultNeeds()}, nil, nil))
	engine := gin.New()
	engine.PATCH("/ambient/needs/:key", handler.UpdateNeed)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/ambient/needs/safety", strings.NewReader(`{"priorityWeight": 100}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("update status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestUpdateNeedHandlerStoresPrivateOwnerProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	handler := NewHandler(NewService(repo, nil, nil))
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.PATCH("/ambient/needs/:key", handler.UpdateNeed)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/ambient/needs/safety", strings.NewReader(`{"priorityWeight": 100}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(repo.overrides) != 1 || repo.overrides[0].OwnerIdentity != "alice" || repo.overrides[0].NeedKey != "safety" || repo.overrides[0].PriorityWeight != 100 {
		t.Fatalf("stored override = %#v", repo.overrides)
	}
}

func TestScanHandlerCreatesPrivatePursuitProposal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pursuitID := uuid.New()
	repo := &ambientRepositoryStub{needs: defaultNeeds()}
	pursuits := &ambientPursuitSpy{
		dashboard: &pursuitpkg.Dashboard{PlanningNeeded: []pursuitpkg.PursuitListItem{{
			Pursuit:    models.Pursuit{ID: pursuitID, OwnerIdentity: "alice", Title: "Prepare a safe plan", RiskLevel: "low", Confidence: 0.8, PriorityScore: 72},
			NextAction: "Create the first governed workflow plan.",
		}}},
		pursuits: []models.Pursuit{{ID: pursuitID, OwnerIdentity: "alice", Status: pursuitpkg.StatusActive}},
	}
	handler := NewHandler(NewServiceWithPursuits(repo, nil, nil, pursuits))
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/ambient/scan", handler.Scan)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ambient/scan", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("scan status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.opportunity == nil || repo.opportunity.OwnerIdentity != "alice" || repo.opportunity.SourceID != pursuitID.String() {
		t.Fatalf("private scan stored %#v, want Alice pursuit proposal", repo.opportunity)
	}
}

func TestResolutionHandlersRequireVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &ambientResolutionService{}
	handler := NewHandler(service)
	engine := gin.New()
	engine.POST("/accept/:id", handler.Accept)
	engine.POST("/dismiss/:id", handler.Dismiss)

	for _, path := range []string{"/accept/" + uuid.NewString(), "/dismiss/" + uuid.NewString()} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, http.StatusUnauthorized, recorder.Body.String())
		}
	}
	if service.acceptCalls != 0 || service.dismissCalls != 0 {
		t.Fatalf("ambient resolution reached service without an owner: accept=%d dismiss=%d", service.acceptCalls, service.dismissCalls)
	}
}

func TestResolutionHandlersUseVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &ambientResolutionService{}
	handler := NewHandler(service)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	engine.POST("/accept/:id", handler.Accept)
	engine.POST("/dismiss/:id", handler.Dismiss)

	for _, path := range []string{"/accept/" + uuid.NewString(), "/dismiss/" + uuid.NewString()} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"note":"reviewed"}`))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, http.StatusOK, recorder.Body.String())
		}
	}
	if service.acceptRequest.OwnerIdentity != "alice" || service.acceptRequest.Actor != "alice" {
		t.Fatalf("accept request = %#v, want verified alice identity", service.acceptRequest)
	}
	if service.dismissRequest.OwnerIdentity != "alice" || service.dismissRequest.Actor != "alice" {
		t.Fatalf("dismiss request = %#v, want verified alice identity", service.dismissRequest)
	}
}

type ambientResolutionService struct {
	acceptCalls    int
	dismissCalls   int
	acceptRequest  ResolutionRequest
	dismissRequest ResolutionRequest
	overviewCalls  int
	overviewOwner  string
	overviewErr    error
}

func (s *ambientResolutionService) Overview() (*Overview, error) {
	return &Overview{}, nil
}

func (s *ambientResolutionService) OverviewForOwner(ownerIdentity string) (*Overview, error) {
	s.overviewCalls++
	s.overviewOwner = ownerIdentity
	if s.overviewErr != nil {
		return nil, s.overviewErr
	}
	return &Overview{}, nil
}

func (s *ambientResolutionService) Scan(string) (*models.AmbientScan, error) {
	return &models.AmbientScan{}, nil
}

func (s *ambientResolutionService) ScanForOwner(string, string) (*models.AmbientScan, error) {
	return &models.AmbientScan{}, nil
}

func (s *ambientResolutionService) UpdateNeedForOwner(string, string, NeedUpdateRequest) (*models.AmbientNeed, error) {
	return &models.AmbientNeed{}, nil
}

func (s *ambientResolutionService) Accept(id uuid.UUID, request ResolutionRequest) (*models.AmbientOpportunity, error) {
	s.acceptCalls++
	s.acceptRequest = request
	return &models.AmbientOpportunity{ID: id}, nil
}

func (s *ambientResolutionService) Dismiss(id uuid.UUID, request ResolutionRequest) (*models.AmbientOpportunity, error) {
	s.dismissCalls++
	s.dismissRequest = request
	return &models.AmbientOpportunity{ID: id}, nil
}

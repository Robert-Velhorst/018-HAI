package memory

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHandlerIgnoresForgedOwnerAndScopesMemoryListing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(newFakeRepository()))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, c.GetHeader("X-Test-Owner"))
	})
	router.POST("/memory", handler.Create)
	router.GET("/memory", handler.List)

	create := func(owner string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/memory", bytes.NewBufferString(`{"ownerIdentity":"alice","kind":"project","content":"private case context"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Owner", owner)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := create("bob"); response.Code != http.StatusCreated {
		t.Fatalf("Bob create status=%d body=%s", response.Code, response.Body.String())
	}

	list := func(owner string) []models.ContextMemory {
		req := httptest.NewRequest(http.MethodGet, "/memory", nil)
		req.Header.Set("X-Test-Owner", owner)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("%s list status=%d body=%s", owner, response.Code, response.Body.String())
		}
		var memories []models.ContextMemory
		if err := json.Unmarshal(response.Body.Bytes(), &memories); err != nil {
			t.Fatalf("decode %s memories: %v", owner, err)
		}
		return memories
	}
	if got := list("alice"); len(got) != 0 {
		t.Fatalf("alice received Bob's private memory: %#v", got)
	}
	if got := list("bob"); len(got) != 1 || got[0].Content != "private case context" {
		t.Fatalf("bob memories = %#v, want the private record", got)
	}
}

func TestHandlerRejectsCallerMintedReviewManagedMemoryAndMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepository()
	service := newVerifiedFactTestService(repo)
	trustedWriter := service.(OwnerScopedService)
	proof := VerifiedFactProvenance{RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/review/12"}
	fact, err := CreateVerifiedFactForOwner(service, "alice", CreateRequest{
		Kind: "source_supported_fact", Content: "The review is scheduled for Friday.",
		SourceURI: proof.SourceURI, SourceLabel: "verification-run:" + proof.RunID.String(), Confidence: 0.91,
	}, proof)
	if err != nil {
		t.Fatalf("seed verified fact: %v", err)
	}
	note, err := trustedWriter.CreateForOwner("alice", CreateRequest{Kind: "note", Content: "Ordinary note."})
	if err != nil {
		t.Fatalf("seed ordinary note: %v", err)
	}

	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, c.GetHeader("X-Test-Owner")) })
	router.POST("/memory", handler.Create)
	router.PUT("/memory/:id", handler.Update)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Owner", "alice")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	response := request(http.MethodPost, "/memory", `{"kind":"source_supported_fact","content":"An unverified model claim.","confidence":0.99,"sourceUri":"https://fake.example"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("caller-minted verified-kind status=%d body=%s, want %d", response.Code, response.Body.String(), http.StatusForbidden)
	}
	response = request(http.MethodPut, "/memory/"+fact.ID.String(), `{"content":"An unverified replacement claim."}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("verified-fact mutation status=%d body=%s, want %d", response.Code, response.Body.String(), http.StatusBadRequest)
	}
	response = request(http.MethodPut, "/memory/"+note.ID.String(), `{"kind":"correction_lesson"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("caller relabel status=%d body=%s, want %d", response.Code, response.Body.String(), http.StatusBadRequest)
	}

	if len(repo.memories) != 2 {
		t.Fatalf("rejected HTTP writes changed memory count to %d, want 2", len(repo.memories))
	}
	stored, err := trustedWriter.FindByIDForOwner("alice", fact.ID)
	if err != nil || stored.Content != fact.Content || stored.SourceURI != fact.SourceURI {
		t.Fatalf("rejected HTTP mutation changed verified fact: stored=%#v err=%v", stored, err)
	}
}

func TestHandlerFailsClosedWhenOwnerIdentityIsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)
	bobMemory, err := scoped.CreateForOwner("bob", CreateRequest{
		Kind: "note", Content: "Bob's private memory must not be exposed.",
	})
	if err != nil {
		t.Fatalf("seed Bob memory: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/memory", handler.List)
	router.GET("/memory/query", handler.Query)
	router.GET("/memory/health", handler.Health)
	router.GET("/memory/export", handler.Export)
	router.GET("/memory/:id", handler.Get)
	router.POST("/memory/retrieve", handler.Retrieve)
	router.POST("/memory/reindex", handler.ReindexSemantic)
	router.POST("/memory", handler.Create)
	router.PUT("/memory/:id", handler.Update)
	router.POST("/memory/:id/archive", handler.Archive)
	router.POST("/memory/:id/restore", handler.Restore)
	router.DELETE("/memory/:id", handler.Delete)

	ownerlessRecordPath := "/memory/" + bobMemory.ID.String()

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/memory"},
		{name: "recent list", method: http.MethodGet, path: "/memory?limit=1"},
		{name: "query", method: http.MethodGet, path: "/memory/query"},
		{name: "health", method: http.MethodGet, path: "/memory/health"},
		{name: "export", method: http.MethodGet, path: "/memory/export"},
		{name: "get by id", method: http.MethodGet, path: ownerlessRecordPath},
		{name: "retrieve", method: http.MethodPost, path: "/memory/retrieve", body: `{"query":"private memory"}`},
		{name: "semantic reindex", method: http.MethodPost, path: "/memory/reindex"},
		{name: "create", method: http.MethodPost, path: "/memory", body: `{"kind":"note","content":"Must not become global."}`},
		{name: "update", method: http.MethodPut, path: ownerlessRecordPath, body: `{"content":"Must not be changed."}`},
		{name: "archive", method: http.MethodPost, path: ownerlessRecordPath + "/archive"},
		{name: "restore", method: http.MethodPost, path: ownerlessRecordPath + "/restore"},
		{name: "delete", method: http.MethodDelete, path: ownerlessRecordPath},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("ownerless request status=%d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if bytes.Contains(response.Body.Bytes(), []byte("Bob's private memory")) {
				t.Fatalf("ownerless request exposed private memory: %s", response.Body.String())
			}
		})
	}
	if len(repo.memories) != 1 {
		t.Fatalf("ownerless create changed record count to %d, want 1", len(repo.memories))
	}
}

func TestHandlerBoundsRecentMemoryListing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newOwnerScopedFakeRepository()
	service := NewService(repo)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/memory", handler.Create)
	router.GET("/memory", handler.List)

	for index := 0; index < 3; index++ {
		request := httptest.NewRequest(http.MethodPost, "/memory", bytes.NewBufferString(fmt.Sprintf(`{"kind":"project","content":"private context %d"}`, index)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/memory?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("recent list status=%d body=%s", response.Code, response.Body.String())
	}
	var memories []models.ContextMemory
	if err := json.Unmarshal(response.Body.Bytes(), &memories); err != nil {
		t.Fatalf("decode recent memories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("recent list returned %d records, want 1", len(memories))
	}
}

func TestHandlerDoesNotExposeInternalSourceExtractionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepository()
	service := NewService(repo)
	memoryID := uuid.New()
	extractionID := uuid.New()
	repo.memories[memoryID] = models.ContextMemory{
		ID: memoryID, OwnerIdentity: "alice", Kind: "correction_lesson",
		Content: "The source extraction correction should stay owner-private.",
		Summary: "A private correction lesson.", SourceURI: "https://records.example/evidence/7",
		SourceExtractionID: &extractionID, SourceLabel: "reviewed-source-extraction",
		ContentHash: "internal-hash",
	}

	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, c.GetHeader("X-Test-Owner")) })
	router.GET("/memory", handler.List)
	router.GET("/memory/query", handler.Query)
	router.GET("/memory/export", handler.Export)
	router.GET("/memory/:id", handler.Get)
	router.POST("/memory/retrieve", handler.Retrieve)

	requests := []struct {
		name, method, path, body string
	}{
		{name: "list", method: http.MethodGet, path: "/memory"},
		{name: "query", method: http.MethodGet, path: "/memory/query"},
		{name: "export", method: http.MethodGet, path: "/memory/export"},
		{name: "get", method: http.MethodGet, path: "/memory/" + memoryID.String()},
		{name: "retrieve", method: http.MethodPost, path: "/memory/retrieve", body: `{"query":"source extraction correction"}`},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("X-Test-Owner", "alice")
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "sourceExtractionId") || strings.Contains(response.Body.String(), extractionID.String()) {
				t.Fatalf("API response exposed internal extraction lineage: %s", response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "https://records.example/evidence/7") {
				t.Fatalf("API response lost the user-facing evidence URI: %s", response.Body.String())
			}
		})
	}

	if stored := repo.memories[memoryID]; stored.SourceExtractionID == nil || *stored.SourceExtractionID != extractionID {
		t.Fatalf("response redaction changed stored provenance: %#v", stored.SourceExtractionID)
	}
}

func TestHandlerRejectsOversizedMemoryRequestBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeRepository()
	service := NewService(repo)
	seed, err := service.(OwnerScopedService).CreateForOwner("alice", CreateRequest{Kind: "note", Content: "keep this record unchanged"})
	if err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	handler := NewHandler(service)
	router.POST("/memory", handler.Create)
	router.POST("/memory/retrieve", handler.Retrieve)
	router.PUT("/memory/:id", handler.Update)
	oversizedBody := `{"kind":"note","content":"` + strings.Repeat("x", maxMemoryRequestBytes) + `"}`

	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/memory"},
		{method: http.MethodPost, path: "/memory/retrieve"},
		{method: http.MethodPut, path: "/memory/" + seed.ID.String()},
	} {
		req := httptest.NewRequest(request.method, request.path, bytes.NewBufferString(oversizedBody))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s %s status=%d body=%s, want 413", request.method, request.path, response.Code, response.Body.String())
		}
	}
	if len(repo.memories) != 1 || repo.memories[seed.ID].Content != seed.Content {
		t.Fatalf("oversized HTTP bodies changed persisted memory state: %#v", repo.memories)
	}
}

// buildQueryHandler wires the real handler over the in-memory fake repository
// and seeds a few memories, so the HTTP layer is exercised end-to-end.
func buildQueryHandler(t *testing.T) *Handler {
	t.Helper()
	repo := newFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)
	seed := []CreateRequest{
		{ProjectKey: "018-hai", Kind: "preference", Content: "Prefer local Ollama models before cloud models.", Tags: []string{"llm", "routing"}, Confidence: 0.9},
		{ProjectKey: "018-hai", Kind: "project", Content: "The dashboard is built with Angular and the backend in Go.", Tags: []string{"frontend"}, Confidence: 0.6},
		{ProjectKey: "018-hai", Kind: "preference", Content: "Always require approval before running automations.", Tags: []string{"safety"}, Confidence: 0.75},
	}
	for _, req := range seed {
		if _, err := scoped.CreateForOwner("test-owner", req); err != nil {
			t.Fatalf("seed memory: %v", err)
		}
	}
	return NewHandler(service)
}

func doQuery(t *testing.T, h *Handler, rawQuery string) PageResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/memory/query?"+rawQuery, nil)
	c.Set(identity.ContextSubjectKey, "test-owner")
	h.Query(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("Query status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var result PageResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode PageResult: %v (body: %s)", err, rec.Body.String())
	}
	return result
}

func TestHandlerQueryPaginatesAndFilters(t *testing.T) {
	h := buildQueryHandler(t)

	// Search narrows to the single Angular/Go memory.
	search := doQuery(t, h, "q=angular+backend")
	if search.Total != 1 || search.Items[0].Kind != "project" {
		t.Fatalf("search total=%d, want 1 project memory", search.Total)
	}

	// Kind filter + pagination: two preferences, one per page.
	page1 := doQuery(t, h, "kind=preference&pageSize=1&page=1")
	if page1.Total != 2 || page1.TotalPages != 2 || len(page1.Items) != 1 {
		t.Fatalf("kind page1 total=%d totalPages=%d items=%d, want 2/2/1", page1.Total, page1.TotalPages, len(page1.Items))
	}
	page2 := doQuery(t, h, "kind=preference&pageSize=1&page=2")
	if len(page2.Items) != 1 || page2.Items[0].ID == page1.Items[0].ID {
		t.Fatalf("kind page2 should return the other preference, got %d items", len(page2.Items))
	}

	// Echoed normalized params let a client render controls truthfully.
	if page1.PageSize != 1 || page1.Sort != "updatedAt" || page1.Order != "desc" {
		t.Fatalf("echoed params wrong: size=%d sort=%s order=%s", page1.PageSize, page1.Sort, page1.Order)
	}
}

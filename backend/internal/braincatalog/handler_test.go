package braincatalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeUpstreamReviewer struct {
	review UpstreamReview
	err    error
}

type fakeCollectionReviewer struct {
	review OSSInsightCollectionReview
	err    error
}

func (f fakeCollectionReviewer) ReviewCollections() (OSSInsightCollectionReview, error) {
	return f.review, f.err
}

type fakeRepositoryScout struct {
	report OSSInsightRepositoryDiscoveryReport
	err    error
}

func (f fakeRepositoryScout) DiscoverRepositories() (OSSInsightRepositoryDiscoveryReport, error) {
	return f.report, f.err
}

func (f fakeRepositoryScout) DiscoverReviewableRepositories() (OSSInsightRepositoryDiscoveryReport, error) {
	return f.report, f.err
}

func (f fakeRepositoryScout) DiscoverRepositoriesFor(_ OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error) {
	return f.report, f.err
}

func (f fakeUpstreamReviewer) Review(entry Entry) (UpstreamReview, error) {
	if f.review.ID == "" {
		f.review.ID = entry.ID
	}
	return f.review, f.err
}

func TestListHandlerPublishesReadOnlyCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler()
	router.GET("/", handler.List)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Catalog discovery is read-only") || !strings.Contains(response.Body.String(), "openhands") || !strings.Contains(response.Body.String(), "OSS Insight") || !strings.Contains(response.Body.String(), "litellm") || !strings.Contains(response.Body.String(), "langfuse") || !strings.Contains(response.Body.String(), "gosec") || !strings.Contains(response.Body.String(), "trivy") || !strings.Contains(response.Body.String(), "collectionScreening") || !strings.Contains(response.Body.String(), `"total":138`) || !strings.Contains(response.Body.String(), "license_review") || !strings.Contains(response.Body.String(), `"implementation"`) || !strings.Contains(response.Body.String(), "/api/v1/llm") {
		t.Fatalf("catalog response lacks policy or entry: %s", response.Body.String())
	}
	if etag := response.Header().Get("ETag"); etag == "" {
		t.Fatal("catalog response did not include an ETag")
	}
	if cache := response.Header().Get("Cache-Control"); !strings.Contains(cache, "private") || !strings.Contains(cache, "must-revalidate") {
		t.Fatalf("catalog cache policy = %q", cache)
	}
	if vary := response.Header().Get("Vary"); !strings.Contains(vary, "Authorization") || !strings.Contains(vary, "Cookie") {
		t.Fatalf("catalog Vary header = %q", vary)
	}

	revalidated := httptest.NewRecorder()
	revalidationRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	revalidationRequest.Header.Set("If-None-Match", "W/"+response.Header().Get("ETag"))
	router.ServeHTTP(revalidated, revalidationRequest)
	if revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("catalog revalidation = %d %q", revalidated.Code, revalidated.Body.String())
	}
}

func TestGetHandlerReturnsNotFoundForUnknownEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler()
	router.GET("/:id", handler.Get)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdoptionPlanHandlerReturnsReadOnlyImplementationQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler()
	router.GET("/adoption-plan", handler.AdoptionPlan)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/adoption-plan", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items"`) || !strings.Contains(response.Body.String(), `"cloudquery"`) || !strings.Contains(response.Body.String(), "does not install") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestRevalidateHandlerReturnsBoundedReview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandlerWithReviewer(fakeUpstreamReviewer{review: UpstreamReview{Available: true, License: "MIT", Message: "metadata only"}})
	router.POST("/:id/revalidate", handler.Revalidate)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/opencode/revalidate", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"opencode"`) || !strings.Contains(response.Body.String(), `"license":"MIT"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestRevalidateCollectionsHandlerReportsDriftWithoutMutatingCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandlerWithReviewers(
		fakeUpstreamReviewer{},
		fakeCollectionReviewer{review: OSSInsightCollectionReview{Available: true, ExpectedTotal: 138, CurrentTotal: 139, NewCollections: []string{"Future capability"}, Message: "drift only"}},
	)
	router.POST("/ossinsight/revalidate", handler.RevalidateCollections)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ossinsight/revalidate", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"newCollections":["Future capability"]`) || !strings.Contains(response.Body.String(), "drift only") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestCollectionMaintenanceHandlersReturnOnlyDurableIndexEvidence(t *testing.T) {
	t.Setenv("HAI_CATALOG_REVALIDATION_ENABLED", "true")
	t.Setenv("HAI_CATALOG_COLLECTION_REVALIDATION_ENABLED", "true")
	gin.SetMode(gin.TestMode)
	collectionHistory := &fakeCollectionReviewHistory{}
	maintenance := NewCatalogMaintenanceService(&fakeCatalogReviewer{}, &fakeCatalogReviewHistory{}).
		WithCollectionMaintenance(&fakeScheduledCollectionReviewer{}, collectionHistory)
	handler := NewHandler().WithMaintenance(maintenance)
	router := gin.New()
	router.POST("/collection-revalidation/run", handler.RunDueCollectionRevalidation)
	router.GET("/collection-revalidation-history", handler.CollectionRevalidationHistory)

	run := httptest.NewRecorder()
	router.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/collection-revalidation/run", nil))
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"expectedTotal":138`) || strings.Contains(run.Body.String(), "repository rows") {
		t.Fatalf("unexpected collection maintenance result: %d %s", run.Code, run.Body.String())
	}

	history := httptest.NewRecorder()
	router.ServeHTTP(history, httptest.NewRequest(http.MethodGet, "/collection-revalidation-history", nil))
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"currentTotal":138`) {
		t.Fatalf("unexpected collection maintenance history: %d %s", history.Code, history.Body.String())
	}
}

func TestRepositoryDiscoveryMaintenanceHandlersReturnCappedReviewEvidence(t *testing.T) {
	t.Setenv("HAI_CATALOG_REVALIDATION_ENABLED", "true")
	t.Setenv("HAI_CATALOG_REPOSITORY_DISCOVERY_REVALIDATION_ENABLED", "true")
	gin.SetMode(gin.TestMode)
	history := &fakeRepositoryDiscoveryReviewHistory{}
	maintenance := NewCatalogMaintenanceService(&fakeCatalogReviewer{}, &fakeCatalogReviewHistory{}).
		WithRepositoryDiscoveryMaintenance(&fakeScheduledRepositoryScout{}, history)
	handler := NewHandler().WithMaintenance(maintenance)
	router := gin.New()
	router.POST("/repository-discovery-revalidation/run", handler.RunDueRepositoryDiscoveryRevalidation)
	router.GET("/repository-discovery-revalidation-history", handler.RepositoryDiscoveryRevalidationHistory)

	run := httptest.NewRecorder()
	router.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/repository-discovery-revalidation/run", nil))
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), "owner/review-me") || strings.Contains(run.Body.String(), "package metadata") {
		t.Fatalf("unexpected repository discovery maintenance result: %d %s", run.Code, run.Body.String())
	}

	resultHistory := httptest.NewRecorder()
	router.ServeHTTP(resultHistory, httptest.NewRequest(http.MethodGet, "/repository-discovery-revalidation-history", nil))
	if resultHistory.Code != http.StatusOK || !strings.Contains(resultHistory.Body.String(), `"repositoriesChecked":116`) {
		t.Fatalf("unexpected repository discovery maintenance history: %d %s", resultHistory.Code, resultHistory.Body.String())
	}
}

func TestDiscoverRepositoriesHandlerReportsUnreviewedCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandlerWithReviewersAndScout(
		fakeUpstreamReviewer{},
		fakeCollectionReviewer{},
		fakeRepositoryScout{report: OSSInsightRepositoryDiscoveryReport{Available: true, CollectionsScreened: 138, Discoveries: []OSSInsightRepositoryDiscovery{{Repository: "owner/new-agent"}}, Message: "did not add catalog entries"}},
	)
	router.POST("/ossinsight/discover", handler.DiscoverRepositories)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ossinsight/discover", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "owner/new-agent") || !strings.Contains(response.Body.String(), "did not add catalog entries") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestDiscoverReviewableRepositoriesHandlerReportsRepresentedAndCandidateScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandlerWithReviewersAndScout(
		fakeUpstreamReviewer{},
		fakeCollectionReviewer{},
		fakeRepositoryScout{report: OSSInsightRepositoryDiscoveryReport{Available: true, Scope: OSSInsightReviewableScope, EligibleCollections: 20, Discoveries: []OSSInsightRepositoryDiscovery{{Repository: "owner/new-provider", Disposition: CollectionRepresented}}, Message: "did not add catalog entries"}},
	)
	router.POST("/ossinsight/discover/reviewable", handler.DiscoverReviewableRepositories)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ossinsight/discover/reviewable", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"scope":"reviewable"`) || !strings.Contains(response.Body.String(), `"disposition":"represented_in_catalog"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestRecommendCapabilitiesHandlerUsesReviewedCatalogOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler()
	router.POST("/recommend", handler.RecommendCapabilities)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/recommend", strings.NewReader(`{"need":"local model evaluation"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "lm-eval-harness") || strings.Contains(response.Body.String(), `"id":"minio"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

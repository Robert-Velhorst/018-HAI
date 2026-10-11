package braincatalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type discoveryReviewRequest struct {
	Repository string                   `json:"repository"`
	Scope      OSSInsightDiscoveryScope `json:"scope"`
}

type capabilityRecommendationRequest struct {
	Need string `json:"need"`
}

// Handler exposes the transparent catalog. It deliberately has no enable or
// install endpoint: activation belongs to a reviewed runtime adapter.
type Handler struct {
	reviewer           UpstreamReviewer
	collectionReviewer OSSInsightCollectionReviewer
	repositoryScout    OSSInsightRepositoryScout
	maintenance        *CatalogMaintenanceService
	catalogOnce        sync.Once
	catalogJSON        []byte
	catalogETag        string
	catalogErr         error
}

func NewHandler() *Handler {
	return NewHandlerWithReviewersAndScout(NewUpstreamReviewer(nil), NewOSSInsightCollectionReviewer(nil), NewOSSInsightRepositoryScout(nil))
}

func NewHandlerWithReviewer(reviewer UpstreamReviewer) *Handler {
	return NewHandlerWithReviewersAndScout(reviewer, NewOSSInsightCollectionReviewer(nil), NewOSSInsightRepositoryScout(nil))
}

func NewHandlerWithReviewers(reviewer UpstreamReviewer, collectionReviewer OSSInsightCollectionReviewer) *Handler {
	return NewHandlerWithReviewersAndScout(reviewer, collectionReviewer, NewOSSInsightRepositoryScout(nil))
}

func NewHandlerWithReviewersAndScout(reviewer UpstreamReviewer, collectionReviewer OSSInsightCollectionReviewer, repositoryScout OSSInsightRepositoryScout) *Handler {
	return &Handler{reviewer: reviewer, collectionReviewer: collectionReviewer, repositoryScout: repositoryScout}
}

// WithMaintenance adds durable, bounded catalog metadata maintenance without
// altering the handler's read-only catalog or its activation boundaries.
func (h *Handler) WithMaintenance(maintenance *CatalogMaintenanceService) *Handler {
	h.maintenance = maintenance
	return h
}

func (h *Handler) List(c *gin.Context) {
	h.catalogOnce.Do(func() {
		h.catalogJSON, h.catalogErr = json.Marshal(gin.H{
			"sourceCatalog":       sourceCatalogURL,
			"discoverySources":    DiscoverySources(),
			"verifiedAt":          verifiedAt,
			"entries":             Entries(),
			"planeCoverage":       CapabilityPlaneCoverageReport(),
			"collectionScreening": OSSInsightScreening(),
			"activationPolicy":    "Catalog discovery is read-only. HAI never installs, enables, or executes a listed project without a reviewed adapter and the existing approval gates.",
		})
		if h.catalogErr == nil {
			digest := sha256.Sum256(h.catalogJSON)
			h.catalogETag = fmt.Sprintf(`"%x"`, digest)
		}
	})
	if h.catalogErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not encode capability catalog"})
		return
	}
	c.Header("Cache-Control", "private, max-age=300, must-revalidate")
	c.Header("ETag", h.catalogETag)
	c.Header("Vary", "Authorization, Cookie")
	if matchesETag(c.GetHeader("If-None-Match"), h.catalogETag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", h.catalogJSON)
}

func matchesETag(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == current {
			return true
		}
	}
	return false
}

// AdoptionPlan exposes the reviewed implementation queue. It is derived only
// from HAI's local catalog and plane coverage, so it needs no upstream request
// and cannot install, configure, or activate any project.
func (h *Handler) AdoptionPlan(c *gin.Context) {
	c.JSON(http.StatusOK, AdoptionPlanReport())
}

func (h *Handler) Get(c *gin.Context) {
	entry, ok := EntryByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "catalog entry not found"})
		return
	}
	c.JSON(http.StatusOK, entry)
}

// Revalidate performs a bounded public metadata check for a fixed catalog
// entry. It does not mutate catalog status, install software, or enable a
// runtime; the admin route protects the external request from bulk use.
func (h *Handler) Revalidate(c *gin.Context) {
	entry, ok := EntryByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "catalog entry not found"})
		return
	}
	if h.reviewer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "upstream revalidation is unavailable"})
		return
	}
	review, err := reviewUpstream(c.Request.Context(), h.reviewer, entry)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not revalidate the configured upstream"})
		return
	}
	c.JSON(http.StatusOK, review)
}

// RevalidationHistory returns redacted evidence from bounded scheduled or
// owner-triggered upstream checks. It never returns archives or raw responses.
func (h *Handler) RevalidationHistory(c *gin.Context) {
	if h.maintenance == nil || h.maintenance.history == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog revalidation history is unavailable"})
		return
	}
	limit := 30
	if parsed, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "30"))); err == nil {
		limit = parsed
	}
	records, err := h.maintenance.history.FindRecentUpstreamReviews(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load catalog revalidation history"})
		return
	}
	reviews := make([]UpstreamReview, 0, len(records))
	for _, record := range records {
		reviews = append(reviews, upstreamReviewFromRecord(record))
	}
	c.JSON(http.StatusOK, reviews)
}

// CollectionRevalidationHistory returns bounded public OSS Insight index
// evidence. It never contains repository rows, source archives, credentials,
// or an activation decision.
func (h *Handler) CollectionRevalidationHistory(c *gin.Context) {
	if h.maintenance == nil || h.maintenance.collectionHistory == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog collection revalidation history is unavailable"})
		return
	}
	limit := 30
	if parsed, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "30"))); err == nil {
		limit = parsed
	}
	records, err := h.maintenance.collectionHistory.FindRecentCollectionReviews(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load catalog collection revalidation history"})
		return
	}
	reviews := make([]OSSInsightCollectionReview, 0, len(records))
	for _, record := range records {
		reviews = append(reviews, collectionReviewFromRecord(record))
	}
	c.JSON(http.StatusOK, reviews)
}

// RepositoryDiscoveryRevalidationHistory returns the compact daily gap-review
// evidence. It intentionally exposes only counts and capped repository names,
// never an upstream response, source archive, credential, or activation state.
func (h *Handler) RepositoryDiscoveryRevalidationHistory(c *gin.Context) {
	if h.maintenance == nil || h.maintenance.repositoryDiscoveryHistory == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog repository discovery revalidation history is unavailable"})
		return
	}
	limit := 30
	if parsed, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "30"))); err == nil {
		limit = parsed
	}
	records, err := h.maintenance.repositoryDiscoveryHistory.FindRecentRepositoryDiscoveryReviews(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load catalog repository discovery revalidation history"})
		return
	}
	reviews := make([]OSSInsightRepositoryDiscoveryMaintenanceReview, 0, len(records))
	for _, record := range records {
		reviews = append(reviews, repositoryDiscoveryReviewFromRecord(record))
	}
	c.JSON(http.StatusOK, reviews)
}

// RunDueRevalidations performs the same rate-bounded, fixed-entry sweep used
// by the scheduler. It cannot install, activate, configure, or reclassify a
// catalog entry.
func (h *Handler) RunDueRevalidations(c *gin.Context) {
	if h.maintenance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog revalidation is unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.maintenance.RunDueRevalidationsContext(c.Request.Context()))
}

// RunDueCollectionRevalidation runs the same bounded, daily source-index
// comparison used by the scheduler. It cannot enumerate repositories, mutate
// catalog entries, install a project, or enable a runtime.
func (h *Handler) RunDueCollectionRevalidation(c *gin.Context) {
	if h.maintenance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog collection revalidation is unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.maintenance.RunDueCollectionRevalidationContext(c.Request.Context()))
}

// RunDueRepositoryDiscoveryRevalidation runs the opted-in, daily read-only
// repository gap review. It cannot mutate the catalog, install a dependency,
// create a connector, or enable an execution runtime.
func (h *Handler) RunDueRepositoryDiscoveryRevalidation(c *gin.Context) {
	if h.maintenance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog repository discovery revalidation is unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.maintenance.RunDueRepositoryDiscoveryRevalidationContext(c.Request.Context()))
}

// RevalidateCollections compares HAI's fixed 138-category source snapshot to
// the public OSS Insight list. It is an admin-only, read-only drift check; it
// cannot add entries, change their status, or activate third-party code.
func (h *Handler) RevalidateCollections(c *gin.Context) {
	if h.collectionReviewer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OSS Insight collection revalidation is unavailable"})
		return
	}
	review, err := reviewCollections(c.Request.Context(), h.collectionReviewer)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not revalidate the OSS Insight collection list"})
		return
	}
	c.JSON(http.StatusOK, review)
}

// DiscoverRepositories reads repository names only from pre-screened candidate
// collections. It cannot add a catalog entry, create credentials, or execute
// a discovered project; the owner must initiate a separate review.
func (h *Handler) DiscoverRepositories(c *gin.Context) {
	if h.repositoryScout == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OSS Insight repository discovery is unavailable"})
		return
	}
	report, err := discoverRepositoriesFor(c.Request.Context(), h.repositoryScout, OSSInsightCandidateScope)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not discover OSS Insight repositories"})
		return
	}
	c.JSON(http.StatusOK, report)
}

// DiscoverReviewableRepositories extends source intake to categories that HAI
// already represents as well as categories awaiting adapter review. This helps
// identify replacement or complementary upstreams without making any catalog,
// runtime, credential, or execution change.
func (h *Handler) DiscoverReviewableRepositories(c *gin.Context) {
	if h.repositoryScout == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OSS Insight repository discovery is unavailable"})
		return
	}
	report, err := discoverRepositoriesFor(c.Request.Context(), h.repositoryScout, OSSInsightReviewableScope)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not discover OSS Insight reviewable repositories"})
		return
	}
	c.JSON(http.StatusOK, report)
}

// RevalidateDiscovery verifies GitHub metadata for one repository that was
// returned by the source-controlled discovery report. It rejects arbitrary
// repository names, does not alter the catalog, and cannot activate code.
func (h *Handler) RevalidateDiscovery(c *gin.Context) {
	if h.repositoryScout == nil || h.reviewer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "discovery metadata revalidation is unavailable"})
		return
	}
	var request discoveryReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Repository) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository is required"})
		return
	}
	report, err := discoverRepositoriesFor(c.Request.Context(), h.repositoryScout, request.Scope)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not verify the OSS Insight discovery report"})
		return
	}
	discovery, ok := discoveryByRepository(report, request.Repository)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository is not in the current OSS Insight discovery report"})
		return
	}
	entry, err := entryForDiscovery(discovery)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "discovered repository is not a valid GitHub repository path"})
		return
	}
	review, err := reviewUpstream(c.Request.Context(), h.reviewer, entry)
	if err != nil {
		if requestStopped(err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not retrieve discovered repository metadata"})
		return
	}
	c.JSON(http.StatusOK, review)
}

func requestStopped(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// RecommendCapabilities maps a stated need to the reviewed catalog only. It
// is a planner aid and cannot revalidate, install, configure, or run anything.
func (h *Handler) RecommendCapabilities(c *gin.Context) {
	var request capabilityRecommendationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "capability need is required"})
		return
	}
	response, err := RecommendForNeed(request.Need)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

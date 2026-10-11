package braincatalog

import (
	"automation-hub-backend/internal/models"
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCatalogRevalidationIntervalHours = 24
	minimumCatalogRevalidationIntervalHours = 1
	maximumCatalogRevalidationIntervalHours = 24
	defaultCatalogRevalidationBatchSize     = 8
	minimumCatalogRevalidationBatchSize     = 1
	maximumCatalogRevalidationBatchSize     = 20
)

// CatalogRevalidationRun is a bounded evidence sweep. It reports only static
// catalog metadata and never installs, starts, configures, or adopts a tool.
type CatalogRevalidationRun struct {
	Enabled                   bool                                       `json:"enabled"`
	Eligible                  int                                        `json:"eligible"`
	Checked                   int                                        `json:"checked"`
	Reused                    int                                        `json:"reused"`
	Failed                    int                                        `json:"failed"`
	Results                   []UpstreamReview                           `json:"results"`
	CollectionReview          *CatalogCollectionRevalidationRun          `json:"collectionReview,omitempty"`
	RepositoryDiscoveryReview *CatalogRepositoryDiscoveryRevalidationRun `json:"repositoryDiscoveryReview,omitempty"`
	RunAt                     time.Time                                  `json:"runAt"`
}

// CatalogCollectionRevalidationRun captures only public source-index drift.
// It contains no repository code or project activation decision.
type CatalogCollectionRevalidationRun struct {
	Enabled bool                       `json:"enabled"`
	Reused  bool                       `json:"reused"`
	Failed  bool                       `json:"failed"`
	Review  OSSInsightCollectionReview `json:"review"`
	RunAt   time.Time                  `json:"runAt"`
}

// CatalogRepositoryDiscoveryRevalidationRun captures the capped evidence from
// an opt-in, daily repository-gap review. Discoveries remain review-only and
// cannot install, configure, connect, or execute an upstream project.
type CatalogRepositoryDiscoveryRevalidationRun struct {
	Enabled bool                                           `json:"enabled"`
	Reused  bool                                           `json:"reused"`
	Failed  bool                                           `json:"failed"`
	Review  OSSInsightRepositoryDiscoveryMaintenanceReview `json:"review"`
	RunAt   time.Time                                      `json:"runAt"`
}

type CatalogMaintenanceService struct {
	reviewer                   UpstreamReviewer
	history                    UpstreamReviewHistoryRepository
	collectionReviewer         OSSInsightCollectionReviewer
	collectionHistory          CollectionReviewHistoryRepository
	repositoryScout            OSSInsightRepositoryScout
	repositoryDiscoveryHistory RepositoryDiscoveryReviewHistoryRepository
	now                        func() time.Time
	entries                    func() []Entry
}

func NewCatalogMaintenanceService(reviewer UpstreamReviewer, history UpstreamReviewHistoryRepository) *CatalogMaintenanceService {
	return &CatalogMaintenanceService{reviewer: reviewer, history: history, now: time.Now, entries: Entries}
}

// WithCollectionMaintenance adds an opt-in, once-daily public collection-index
// recheck. It deliberately does not trigger repository discovery, package
// installation, catalog mutation, or runtime activation.
func (s *CatalogMaintenanceService) WithCollectionMaintenance(reviewer OSSInsightCollectionReviewer, history CollectionReviewHistoryRepository) *CatalogMaintenanceService {
	if s == nil {
		return s
	}
	s.collectionReviewer = reviewer
	s.collectionHistory = history
	return s
}

// WithRepositoryDiscoveryMaintenance enables a separate, opt-in daily gap
// review. The persisted result is aggregate-only with at most 30 candidate
// names, and cannot change catalog or runtime state.
func (s *CatalogMaintenanceService) WithRepositoryDiscoveryMaintenance(scout OSSInsightRepositoryScout, history RepositoryDiscoveryReviewHistoryRepository) *CatalogMaintenanceService {
	if s == nil {
		return s
	}
	s.repositoryScout = scout
	s.repositoryDiscoveryHistory = history
	return s
}

// RunDueRevalidations checks only the next small batch of fixed GitHub catalog
// entries. Recent durable records make hourly scheduling safe and avoid
// unnecessary pressure on GitHub's public API.
func (s *CatalogMaintenanceService) RunDueRevalidations() CatalogRevalidationRun {
	return s.RunDueRevalidationsContext(context.Background())
}

func (s *CatalogMaintenanceService) RunDueRevalidationsContext(ctx context.Context) CatalogRevalidationRun {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return CatalogRevalidationRun{Results: []UpstreamReview{}, RunAt: time.Now().UTC()}
	}
	now := s.now().UTC()
	enabled := catalogRevalidationEnabled()
	run := CatalogRevalidationRun{Enabled: enabled, Results: []UpstreamReview{}, RunAt: now}
	run.CollectionReview = s.RunDueCollectionRevalidationContext(ctx)
	run.RepositoryDiscoveryReview = s.RunDueRepositoryDiscoveryRevalidationContext(ctx)
	if !enabled || s.reviewer == nil || s.history == nil {
		return run
	}
	limit := catalogRevalidationBatchSize()
	interval := catalogRevalidationInterval()
	for _, entry := range s.entries() {
		if ctx.Err() != nil {
			return run
		}
		if _, _, err := githubRepositoryPath(entry.UpstreamURL); err != nil {
			continue
		}
		run.Eligible++
		if run.Checked >= limit {
			continue
		}
		latest, err := s.history.FindLatestUpstreamReview(entry.ID)
		if err != nil {
			review := failedMaintenanceReview(entry, now)
			run.Results = append(run.Results, review)
			run.Checked++
			run.Failed++
			continue
		}
		if latest != nil && catalogReviewStillFresh(latest.CheckedAt, now, interval) {
			run.Reused++
			continue
		}
		review, err := reviewUpstream(ctx, s.reviewer, entry)
		if ctx.Err() != nil {
			return run
		}
		if err != nil {
			review = failedMaintenanceReview(entry, now)
			run.Failed++
		}
		if _, recordErr := s.history.RecordUpstreamReview(upstreamReviewRecord(entry, review)); recordErr != nil {
			review = failedPersistenceReview(entry, now)
			run.Failed++
		}
		run.Results = append(run.Results, review)
		run.Checked++
	}
	return run
}

// RunDueRepositoryDiscoveryRevalidation records a bounded, reviewable daily
// OSS Insight gap scan. It uses the already-screened reviewable scope and does
// not install packages, download source archives, or create catalog entries.
func (s *CatalogMaintenanceService) RunDueRepositoryDiscoveryRevalidation() *CatalogRepositoryDiscoveryRevalidationRun {
	return s.RunDueRepositoryDiscoveryRevalidationContext(context.Background())
}

func (s *CatalogMaintenanceService) RunDueRepositoryDiscoveryRevalidationContext(ctx context.Context) *CatalogRepositoryDiscoveryRevalidationRun {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if s != nil && s.now != nil {
		now = s.now().UTC()
	}
	run := &CatalogRepositoryDiscoveryRevalidationRun{
		Enabled: catalogRevalidationEnabled() && catalogRepositoryDiscoveryRevalidationEnabled(),
		RunAt:   now,
	}
	if s == nil || !run.Enabled || s.repositoryScout == nil || s.repositoryDiscoveryHistory == nil {
		return run
	}
	latest, err := s.repositoryDiscoveryHistory.FindLatestRepositoryDiscoveryReview()
	if err != nil {
		run.Failed = true
		run.Review = failedRepositoryDiscoveryMaintenanceReview(now)
		return run
	}
	if latest != nil && catalogReviewStillFresh(latest.CheckedAt, now, catalogRevalidationInterval()) {
		run.Reused = true
		run.Review = repositoryDiscoveryReviewFromRecord(*latest)
		return run
	}
	report, err := discoverRepositoriesFor(ctx, s.repositoryScout, OSSInsightReviewableScope)
	if ctx.Err() != nil {
		return run
	}
	var record *models.BrainCatalogRepositoryDiscoveryReview
	if err != nil {
		run.Failed = true
		run.Review = failedRepositoryDiscoveryMaintenanceReview(now)
	} else {
		record = repositoryDiscoveryReviewRecord(report)
		run.Review = repositoryDiscoveryReviewFromRecord(*record)
	}
	if record == nil {
		record = repositoryDiscoveryReviewRecord(OSSInsightRepositoryDiscoveryReport{
			CheckedAt: now.Format(time.RFC3339), SourceURL: ossInsightCollectionsURL, Scope: OSSInsightReviewableScope,
			Message: run.Review.Message,
		})
	}
	if _, recordErr := s.repositoryDiscoveryHistory.RecordRepositoryDiscoveryReview(record); recordErr != nil {
		run.Failed = true
		run.Review = failedRepositoryDiscoveryPersistenceReview(now)
	}
	return run
}

// RunDueCollectionRevalidation compares HAI's recorded collection snapshot to
// the fixed public OSS Insight index. A durable daily result makes the hourly
// scheduler inexpensive while preserving visible source-drift evidence.
func (s *CatalogMaintenanceService) RunDueCollectionRevalidation() *CatalogCollectionRevalidationRun {
	return s.RunDueCollectionRevalidationContext(context.Background())
}

func (s *CatalogMaintenanceService) RunDueCollectionRevalidationContext(ctx context.Context) *CatalogCollectionRevalidationRun {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if s != nil && s.now != nil {
		now = s.now().UTC()
	}
	run := &CatalogCollectionRevalidationRun{Enabled: catalogRevalidationEnabled() && catalogCollectionRevalidationEnabled(), RunAt: now}
	if s == nil || !run.Enabled || s.collectionReviewer == nil || s.collectionHistory == nil {
		return run
	}
	latest, err := s.collectionHistory.FindLatestCollectionReview()
	if err != nil {
		run.Failed = true
		run.Review = failedCollectionMaintenanceReview(now)
		return run
	}
	if latest != nil && catalogReviewStillFresh(latest.CheckedAt, now, catalogRevalidationInterval()) {
		run.Reused = true
		run.Review = collectionReviewFromRecord(*latest)
		return run
	}
	review, err := reviewCollections(ctx, s.collectionReviewer)
	if ctx.Err() != nil {
		return run
	}
	if err != nil {
		run.Failed = true
		review = failedCollectionMaintenanceReview(now)
	}
	if _, recordErr := s.collectionHistory.RecordCollectionReview(collectionReviewRecord(review)); recordErr != nil {
		run.Failed = true
		review = failedCollectionPersistenceReview(now)
	}
	run.Review = review
	return run
}

func failedMaintenanceReview(entry Entry, checkedAt time.Time) UpstreamReview {
	review := UpstreamReview{
		ID: entry.ID, Name: entry.Name, UpstreamURL: entry.UpstreamURL,
		CheckedAt: checkedAt.UTC().Format(time.RFC3339), Disposition: entry.Status,
		Message: "Upstream metadata could not be revalidated. HAI has not changed the catalog record or activated the project.",
	}
	applyReadinessAssessment(entry, &review)
	return review
}

func failedPersistenceReview(entry Entry, checkedAt time.Time) UpstreamReview {
	review := failedMaintenanceReview(entry, checkedAt)
	review.Message = "Upstream metadata result could not be persisted. HAI has not changed the catalog record or activated the project."
	return review
}

func failedCollectionMaintenanceReview(checkedAt time.Time) OSSInsightCollectionReview {
	return OSSInsightCollectionReview{
		CheckedAt: checkedAt.UTC().Format(time.RFC3339), SourceURL: ossInsightCollectionsURL,
		ExpectedTotal: len(expectedOSSInsightCollections()),
		Message:       "OSS Insight collection index could not be revalidated. HAI has not changed its catalog, discovered repositories, or activated any project.",
	}
}

func failedCollectionPersistenceReview(checkedAt time.Time) OSSInsightCollectionReview {
	review := failedCollectionMaintenanceReview(checkedAt)
	review.Message = "OSS Insight collection result could not be persisted. HAI has not changed its catalog, discovered repositories, or activated any project."
	return review
}

func failedRepositoryDiscoveryMaintenanceReview(checkedAt time.Time) OSSInsightRepositoryDiscoveryMaintenanceReview {
	return OSSInsightRepositoryDiscoveryMaintenanceReview{
		CheckedAt: checkedAt.UTC().Format(time.RFC3339), SourceURL: ossInsightCollectionsURL,
		Scope:   OSSInsightReviewableScope,
		Message: "OSS Insight repository gap review could not be completed. HAI has not changed the catalog, installed a project, or activated a runtime.",
	}
}

func failedRepositoryDiscoveryPersistenceReview(checkedAt time.Time) OSSInsightRepositoryDiscoveryMaintenanceReview {
	review := failedRepositoryDiscoveryMaintenanceReview(checkedAt)
	review.Message = "OSS Insight repository gap review could not be persisted. HAI has not changed the catalog, installed a project, or activated a runtime."
	return review
}

func catalogRevalidationEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_CATALOG_REVALIDATION_ENABLED")), "true")
}

func catalogCollectionRevalidationEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_CATALOG_COLLECTION_REVALIDATION_ENABLED")), "true")
}

func catalogRepositoryDiscoveryRevalidationEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_CATALOG_REPOSITORY_DISCOVERY_REVALIDATION_ENABLED")), "true")
}

func catalogRevalidationInterval() time.Duration {
	hours := catalogRevalidationIntEnv("HAI_CATALOG_REVALIDATION_INTERVAL_HOURS", defaultCatalogRevalidationIntervalHours)
	if hours < minimumCatalogRevalidationIntervalHours {
		hours = minimumCatalogRevalidationIntervalHours
	}
	if hours > maximumCatalogRevalidationIntervalHours {
		hours = maximumCatalogRevalidationIntervalHours
	}
	return time.Duration(hours) * time.Hour
}

func catalogRevalidationBatchSize() int {
	batchSize := catalogRevalidationIntEnv("HAI_CATALOG_REVALIDATION_BATCH_SIZE", defaultCatalogRevalidationBatchSize)
	if batchSize < minimumCatalogRevalidationBatchSize {
		return minimumCatalogRevalidationBatchSize
	}
	if batchSize > maximumCatalogRevalidationBatchSize {
		return maximumCatalogRevalidationBatchSize
	}
	return batchSize
}

func catalogRevalidationIntEnv(name string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil {
		return value
	}
	return fallback
}

func catalogReviewStillFresh(checkedAt, now time.Time, interval time.Duration) bool {
	checkedAt = checkedAt.UTC()
	return !checkedAt.IsZero() && !checkedAt.After(now) && now.Sub(checkedAt) < interval
}

package memory

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CreateRequest struct {
	OwnerIdentity string   `json:"-"`
	ProjectKey    string   `json:"projectKey,omitempty"`
	Kind          string   `json:"kind"`
	Content       string   `json:"content"`
	Summary       string   `json:"summary,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	SourceURI     string   `json:"sourceUri,omitempty"`
	SourceLabel   string   `json:"sourceLabel,omitempty"`
}

type UpdateRequest struct {
	ProjectKey  string   `json:"projectKey,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Content     string   `json:"content,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	SourceURI   string   `json:"sourceUri,omitempty"`
	SourceLabel string   `json:"sourceLabel,omitempty"`
	Archived    *bool    `json:"archived,omitempty"`
}

type RetrieveRequest struct {
	Query      string `json:"query"`
	ProjectKey string `json:"projectKey,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type RankedMemory struct {
	Memory      models.ContextMemory `json:"memory"`
	Score       float64              `json:"score"`
	Explanation string               `json:"explanation"`
}

type RetrieveResult struct {
	Query       string         `json:"query"`
	ProjectKey  string         `json:"projectKey,omitempty"`
	UsedContext []RankedMemory `json:"usedContext"`
	Explanation string         `json:"explanation"`
}

type SemanticReindexResult struct {
	Enabled     bool   `json:"enabled"`
	Attempted   int    `json:"attempted"`
	Indexed     int    `json:"indexed"`
	Failed      int    `json:"failed"`
	Deferred    int    `json:"deferred"`
	Explanation string `json:"explanation"`
}

const (
	memoryHealthStaleAfter           = 90 * 24 * time.Hour
	memoryLastUsedWriteInterval      = 15 * time.Minute
	maxMemoryConsolidationCandidates = 25
)

// MemoryHealthReport is a read-only owner-scoped review. It proposes no
// mutation; corrections, merges, archival, and deletion use existing paths.
type MemoryHealthReport struct {
	ProjectKey               string                         `json:"projectKey,omitempty"`
	GeneratedAt              time.Time                      `json:"generatedAt"`
	Active                   int                            `json:"active"`
	Archived                 int                            `json:"archived"`
	SourceLinked             int                            `json:"sourceLinked"`
	NeedsSourceReview        int                            `json:"needsSourceReview"`
	HighConfidenceUngrounded int                            `json:"highConfidenceUngrounded"`
	Stale                    int                            `json:"stale"`
	Dormant                  int                            `json:"dormant"`
	PossibleDuplicatePairs   int                            `json:"possibleDuplicatePairs"`
	ConsolidationCandidates  []MemoryConsolidationCandidate `json:"consolidationCandidates"`
	Scope                    string                         `json:"scope"`
}

type MemoryConsolidationCandidate struct {
	FirstID        uuid.UUID `json:"firstId"`
	SecondID       uuid.UUID `json:"secondId"`
	ProjectKey     string    `json:"projectKey,omitempty"`
	Kind           string    `json:"kind"`
	Similarity     float64   `json:"similarity"`
	SourceDiverges bool      `json:"sourceDiverges"`
	Reason         string    `json:"reason"`
}

type Service interface {
	Create(request CreateRequest) (*models.ContextMemory, error)
	Update(id uuid.UUID, request UpdateRequest) (*models.ContextMemory, error)
	FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error)
	FindByID(id uuid.UUID) (*models.ContextMemory, error)
	Archive(id uuid.UUID, archived bool) (*models.ContextMemory, error)
	Delete(id uuid.UUID) error
	Retrieve(request RetrieveRequest) (*RetrieveResult, error)
}

// OwnerScopedService is implemented by HAI's native memory service. Keeping it
// separate preserves the narrow Service contract used by background workers
// while giving authenticated boundaries a fail-closed owner-aware API.
type OwnerScopedService interface {
	CreateForOwner(ownerIdentity string, request CreateRequest) (*models.ContextMemory, error)
	UpdateForOwner(ownerIdentity string, id uuid.UUID, request UpdateRequest) (*models.ContextMemory, error)
	FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error)
	FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error)
	ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool) (*models.ContextMemory, error)
	DeleteForOwner(ownerIdentity string, id uuid.UUID) error
	RetrieveForOwner(ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error)
}

// OwnerContextService lets request-bound callers cancel semantic retrieval
// without widening the established owner-scoped service contract.
type OwnerContextService interface {
	RetrieveForOwnerContext(context.Context, string, RetrieveRequest) (*RetrieveResult, error)
}

// OwnerQueryService is an optional extension for bounded owner-scoped query
// execution. Keeping it separate preserves compatibility with integrations
// that only need the established OwnerScopedService contract.
type OwnerQueryService interface {
	QueryForOwner(context.Context, string, string, bool, QueryParams) (PageResult, error)
}

// SemanticReindexService is intentionally separate from Service so existing
// workers and test doubles do not gain a bulk local-embedding capability by
// accident. HTTP access remains authenticated and write-authorized.
type SemanticReindexService interface {
	ReindexSemanticForOwner(ownerIdentity string, limit int) (*SemanticReindexResult, error)
}

type SemanticReindexContextService interface {
	ReindexSemanticForOwnerContext(context.Context, string, int) (*SemanticReindexResult, error)
}

type MemoryHealthService interface {
	MemoryHealthForOwner(ownerIdentity, projectKey string) (*MemoryHealthReport, error)
}

type service struct {
	repo            Repository
	semanticService semantic.Service
}

func NewService(repo Repository) Service {
	return NewServiceWithSemantic(repo, nil)
}

// NewServiceWithSemantic adds optional local vector enrichment without
// changing HAI's single editable context-memory authority. When the local
// embedding endpoint is disabled or unavailable, keyword retrieval remains
// the complete fallback path.
func NewServiceWithSemantic(repo Repository, semanticService semantic.Service) Service {
	return &service{repo: repo, semanticService: semanticService}
}

func DefaultService() Service {
	return NewService(DefaultRepository())
}

func (s *service) Create(request CreateRequest) (*models.ContextMemory, error) {
	return s.createForOwner("", request)
}

func (s *service) MemoryHealthForOwner(ownerIdentity, projectKey string) (*MemoryHealthReport, error) {
	projectKey = strings.TrimSpace(projectKey)
	memories, err := s.FindAllForOwner(ownerIdentity, projectKey, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	staleBefore := now.Add(-memoryHealthStaleAfter)
	report := &MemoryHealthReport{
		ProjectKey:              projectKey,
		GeneratedAt:             now,
		ConsolidationCandidates: []MemoryConsolidationCandidate{},
		Scope:                   "Read-only owner-scoped memory health and consolidation preview. Suggestions never archive, merge, delete, change provenance, or make a memory eligible as verified context.",
	}
	active := make([]models.ContextMemory, 0, len(memories))
	for _, item := range memories {
		if item.Archived {
			report.Archived++
			continue
		}
		report.Active++
		active = append(active, item)
		if strings.TrimSpace(item.SourceURI) != "" {
			report.SourceLinked++
		} else {
			report.NeedsSourceReview++
			if item.Confidence >= 0.8 {
				report.HighConfidenceUngrounded++
			}
		}
		if !item.UpdatedAt.IsZero() && item.UpdatedAt.Before(staleBefore) {
			report.Stale++
		}
		if item.LastUsedAt == nil {
			if !item.CreatedAt.IsZero() && item.CreatedAt.Before(staleBefore) {
				report.Dormant++
			}
		} else if item.LastUsedAt.Before(staleBefore) {
			report.Dormant++
		}
	}

	for first := 0; first < len(active); first++ {
		for second := first + 1; second < len(active); second++ {
			left, right := active[first], active[second]
			if left.ProjectKey != right.ProjectKey || left.Kind != right.Kind {
				continue
			}
			score := similarity(left.Content, right.Content)
			if score < 0.78 {
				continue
			}
			report.PossibleDuplicatePairs++
			if len(report.ConsolidationCandidates) == maxMemoryConsolidationCandidates {
				continue
			}
			report.ConsolidationCandidates = append(report.ConsolidationCandidates, MemoryConsolidationCandidate{
				FirstID: left.ID, SecondID: right.ID, ProjectKey: left.ProjectKey, Kind: left.Kind,
				Similarity:     math.Round(score*1000) / 1000,
				SourceDiverges: strings.TrimSpace(left.SourceURI) != strings.TrimSpace(right.SourceURI),
				Reason:         "Similar active memories share a project and kind; inspect both records and their provenance before any manual consolidation.",
			})
		}
	}
	sort.SliceStable(report.ConsolidationCandidates, func(i, j int) bool {
		if report.ConsolidationCandidates[i].Similarity == report.ConsolidationCandidates[j].Similarity {
			return report.ConsolidationCandidates[i].FirstID.String() < report.ConsolidationCandidates[j].FirstID.String()
		}
		return report.ConsolidationCandidates[i].Similarity > report.ConsolidationCandidates[j].Similarity
	})
	return report, nil
}

func HealthForOwner(service Service, ownerIdentity, projectKey string) (*MemoryHealthReport, error) {
	health, ok := service.(MemoryHealthService)
	if !ok {
		return nil, fmt.Errorf("memory health review is unavailable")
	}
	return health.MemoryHealthForOwner(ownerIdentity, projectKey)
}

// CreateForOwner stores a memory under the authenticated owner. It never
// deduplicates against ownerless legacy records or another owner's records.
func (s *service) CreateForOwner(ownerIdentity string, request CreateRequest) (*models.ContextMemory, error) {
	return s.createForOwner(ownerIdentity, request)
}

func CreateForOwner(service Service, ownerIdentity string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return service.Create(request)
	}
	scoped, ok := service.(OwnerScopedService)
	if !ok {
		return nil, fmt.Errorf("owner-scoped memory service is unavailable")
	}
	return scoped.CreateForOwner(ownerIdentity, request)
}

func RetrieveForOwner(service Service, ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	return RetrieveForOwnerContext(service, context.Background(), ownerIdentity, request)
}

func RetrieveForOwnerContext(service Service, ctx context.Context, ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if contextual, ok := service.(OwnerContextService); ok {
		return contextual.RetrieveForOwnerContext(ctx, ownerIdentity, request)
	}
	if ownerIdentity == "" {
		return service.Retrieve(request)
	}
	scoped, ok := service.(OwnerScopedService)
	if !ok {
		return nil, fmt.Errorf("owner-scoped memory service is unavailable")
	}
	return scoped.RetrieveForOwner(ownerIdentity, request)
}

func (s *service) createForOwner(ownerIdentity string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	content := strings.TrimSpace(request.Content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	kind := firstNonEmpty(strings.TrimSpace(request.Kind), "project")
	projectKey := strings.TrimSpace(request.ProjectKey)
	contentHash := hashContent(projectKey, kind, content)

	if ownerIdentity == "" {
		if existing, err := s.repo.FindByHash(projectKey, kind, contentHash); err == nil {
			if provenanceCanMerge(existing.SourceURI, request.SourceURI) {
				saved, changed, err := s.mergeExact(existing, request)
				if changed && err == nil {
					s.indexMemory(saved)
				}
				return saved, err
			}
		} else if err != nil && !isNotFound(err) {
			return nil, err
		}
	}

	memories, err := s.FindAllForOwner(ownerIdentity, projectKey, false)
	if err != nil {
		return nil, err
	}
	for _, candidate := range memories {
		if ownerIdentity != "" && candidate.OwnerIdentity != ownerIdentity {
			continue
		}
		if candidate.Kind == kind && sameSourceURI(candidate.SourceURI, request.SourceURI) {
			copyCandidate := candidate
			if candidate.ContentHash == contentHash {
				saved, changed, err := s.mergeExact(&copyCandidate, request)
				if changed && err == nil {
					s.indexMemory(saved)
				}
				return saved, err
			}
			saved, err := s.mergeSimilar(&copyCandidate, request)
			s.indexMemory(saved)
			return saved, err
		}
		if candidate.Kind == kind && candidate.ContentHash == contentHash && provenanceCanMerge(candidate.SourceURI, request.SourceURI) {
			copyCandidate := candidate
			saved, changed, err := s.mergeExact(&copyCandidate, request)
			if changed && err == nil {
				s.indexMemory(saved)
			}
			return saved, err
		}
		if candidate.Kind == kind && provenanceCanMerge(candidate.SourceURI, request.SourceURI) && similarity(candidate.Content, content) >= 0.78 {
			copyCandidate := candidate
			saved, err := s.mergeSimilar(&copyCandidate, request)
			s.indexMemory(saved)
			return saved, err
		}
	}

	memory := &models.ContextMemory{
		OwnerIdentity: ownerIdentity,
		ProjectKey:    projectKey,
		Kind:          kind,
		Content:       content,
		Summary:       compactSummary(firstNonEmpty(request.Summary, content)),
		Tags:          joinTags(request.Tags),
		Confidence:    normalizeConfidence(request.Confidence),
		SourceURI:     strings.TrimSpace(request.SourceURI),
		SourceLabel:   strings.TrimSpace(request.SourceLabel),
		ContentHash:   contentHash,
	}
	saved, err := s.repo.Create(memory)
	s.indexMemory(saved)
	return saved, err
}

func (s *service) UpdateForOwner(ownerIdentity string, id uuid.UUID, request UpdateRequest) (*models.ContextMemory, error) {
	memory, err := s.writeableMemoryForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	return s.update(memory, request)
}

func (s *service) Update(id uuid.UUID, request UpdateRequest) (*models.ContextMemory, error) {
	memory, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	return s.update(memory, request)
}

func (s *service) update(memory *models.ContextMemory, request UpdateRequest) (*models.ContextMemory, error) {
	if request.ProjectKey != "" {
		memory.ProjectKey = strings.TrimSpace(request.ProjectKey)
	}
	if request.Kind != "" {
		memory.Kind = strings.TrimSpace(request.Kind)
	}
	if request.Content != "" {
		memory.Content = strings.TrimSpace(request.Content)
	}
	if request.Summary != "" {
		memory.Summary = compactSummary(request.Summary)
	} else {
		memory.Summary = compactSummary(memory.Content)
	}
	if request.Tags != nil {
		memory.Tags = joinTags(request.Tags)
	}
	if request.Confidence > 0 {
		memory.Confidence = normalizeConfidence(request.Confidence)
	}
	if request.SourceURI != "" {
		memory.SourceURI = strings.TrimSpace(request.SourceURI)
	}
	if request.SourceLabel != "" {
		memory.SourceLabel = strings.TrimSpace(request.SourceLabel)
	}
	if request.Archived != nil {
		memory.Archived = *request.Archived
	}
	memory.ContentHash = hashContent(memory.ProjectKey, memory.Kind, memory.Content)
	saved, err := s.repo.Update(memory)
	s.indexMemory(saved)
	return saved, err
}

func (s *service) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return s.repo.FindAll(projectKey, includeArchived)
}

func (s *service) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity != "" {
		if scoped, ok := s.repo.(OwnerScopedRepository); ok {
			return scoped.FindAllForOwner(ownerIdentity, projectKey, includeArchived)
		}
	}
	memories, err := s.repo.FindAll(projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	return filterReadableMemories(memories, ownerIdentity), nil
}

func (s *service) QueryForOwner(
	ctx context.Context,
	ownerIdentity string,
	projectKey string,
	includeArchived bool,
	params QueryParams,
) (PageResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if queryRepository, ok := s.repo.(OwnerQueryRepository); ok {
		return queryRepository.QueryForOwner(ctx, ownerIdentity, projectKey, includeArchived, params)
	}
	memories, err := s.FindAllForOwner(ownerIdentity, projectKey, includeArchived)
	if err != nil {
		return PageResult{}, err
	}
	return Query(memories, params), nil
}

func (s *service) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	return s.repo.FindByID(id)
}

func (s *service) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	memory, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !readableByOwner(memory, ownerIdentity) {
		return nil, gorm.ErrRecordNotFound
	}
	return memory, nil
}

func (s *service) Archive(id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	memory, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	memory.Archived = archived
	saved, err := s.repo.Update(memory)
	s.indexMemory(saved)
	return saved, err
}

func (s *service) ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	memory, err := s.writeableMemoryForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	memory.Archived = archived
	saved, err := s.repo.Update(memory)
	s.indexMemory(saved)
	return saved, err
}

func (s *service) Delete(id uuid.UUID) error {
	err := s.repo.Delete(id)
	if err == nil {
		s.deleteMemoryIndex(id)
	}
	return err
}

func (s *service) DeleteForOwner(ownerIdentity string, id uuid.UUID) error {
	if _, err := s.writeableMemoryForOwner(ownerIdentity, id); err != nil {
		return err
	}
	err := s.repo.Delete(id)
	if err == nil {
		s.deleteMemoryIndex(id)
	}
	return err
}

func (s *service) Retrieve(request RetrieveRequest) (*RetrieveResult, error) {
	return s.retrieveForOwner(context.Background(), "", request)
}

func (s *service) RetrieveForOwner(ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	return s.retrieveForOwner(context.Background(), ownerIdentity, request)
}

func (s *service) ReindexSemanticForOwner(ownerIdentity string, limit int) (*SemanticReindexResult, error) {
	return s.ReindexSemanticForOwnerContext(context.Background(), ownerIdentity, limit)
}

func (s *service) RetrieveForOwnerContext(ctx context.Context, ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	return s.retrieveForOwner(ctx, ownerIdentity, request)
}

func (s *service) ReindexSemanticForOwnerContext(ctx context.Context, ownerIdentity string, limit int) (*SemanticReindexResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.semanticService == nil || !s.semanticService.Enabled() {
		return &SemanticReindexResult{
			Enabled:     false,
			Explanation: "Local semantic retrieval is disabled; no context memories were sent for embedding.",
		}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	memories, err := s.repo.FindAll("", false)
	if err != nil {
		return nil, err
	}
	visible := filterReadableMemories(memories, ownerIdentity)
	result := &SemanticReindexResult{Enabled: true}
	for index, item := range visible {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if index >= limit {
			result.Deferred = len(visible) - index
			break
		}
		result.Attempted++
		copyItem := item
		if err := s.semanticService.IndexMemory(ctx, &copyItem); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			result.Failed++
			continue
		}
		result.Indexed++
	}
	result.Explanation = fmt.Sprintf("Attempted local semantic indexing for %d visible context memories; %d succeeded and %d failed. Owner and project retrieval filters still apply when matches are used.", result.Attempted, result.Indexed, result.Failed)
	if result.Deferred > 0 {
		result.Explanation += fmt.Sprintf(" %d additional visible memories were deferred by the 100-record safety limit; run another explicit backfill after reviewing local embedding capacity.", result.Deferred)
	}
	return result, nil
}

func (s *service) retrieveForOwner(ctx context.Context, ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	projectKey := strings.TrimSpace(request.ProjectKey)
	memories, err := s.repo.FindAll("", false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates := make([]models.ContextMemory, 0, len(memories))
	for _, memory := range memories {
		if !readableByOwner(&memory, ownerIdentity) {
			continue
		}
		if projectKey != "" && memory.ProjectKey != "" && memory.ProjectKey != projectKey {
			continue
		}
		candidates = append(candidates, memory)
	}
	semanticScores, semanticState, err := s.semanticScores(ctx, ownerIdentity, request)
	if err != nil {
		return nil, err
	}

	ranked := make([]RankedMemory, 0, len(candidates))
	for _, memory := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score, explanation := scoreMemory(memory, request, semanticScores[memory.ID])
		if score <= 0.12 {
			continue
		}
		ranked = append(ranked, RankedMemory{
			Memory:      memory,
			Score:       math.Round(score*1000) / 1000,
			Explanation: explanation,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	now := time.Now().UTC()
	for i := range ranked {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ranked[i].Memory.LastUsedAt != nil && now.Sub(*ranked[i].Memory.LastUsedAt) < memoryLastUsedWriteInterval {
			continue
		}
		ranked[i].Memory.LastUsedAt = &now
		updated := ranked[i].Memory
		if writeableByOwner(&updated, ownerIdentity) {
			if saved, errUpdate := s.repo.Update(&updated); errUpdate == nil {
				ranked[i].Memory = *saved
			}
		}
	}

	explanation := fmt.Sprintf("Retrieved %d relevant memories from %d visible candidates; project-scoped retrieval also considered visible global memories and did not load unrelated project or other-owner memories.", len(ranked), len(candidates))
	if semanticState != "" {
		explanation += " " + semanticState
	}
	return &RetrieveResult{
		Query:       request.Query,
		ProjectKey:  projectKey,
		UsedContext: ranked,
		Explanation: explanation,
	}, nil
}

func (s *service) writeableMemoryForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	memory, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !writeableByOwner(memory, ownerIdentity) {
		return nil, gorm.ErrRecordNotFound
	}
	return memory, nil
}

func readableByOwner(memory *models.ContextMemory, ownerIdentity string) bool {
	if memory == nil {
		return false
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		// Unscoped calls are reserved for trusted internal/system workflows.
		// Authenticated handlers always use the owner-scoped methods below.
		return true
	}
	recordOwner := strings.TrimSpace(memory.OwnerIdentity)
	return recordOwner == ownerIdentity || (recordOwner == "" && identity.CanReadLegacyOwnerlessData(ownerIdentity))
}

func writeableByOwner(memory *models.ContextMemory, ownerIdentity string) bool {
	if memory == nil {
		return false
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	return ownerIdentity == "" || (memory.OwnerIdentity != "" && memory.OwnerIdentity == ownerIdentity)
}

func filterReadableMemories(memories []models.ContextMemory, ownerIdentity string) []models.ContextMemory {
	visible := make([]models.ContextMemory, 0, len(memories))
	for _, memory := range memories {
		if readableByOwner(&memory, ownerIdentity) {
			visible = append(visible, memory)
		}
	}
	return visible
}

func (s *service) mergeExact(existing *models.ContextMemory, request CreateRequest) (*models.ContextMemory, bool, error) {
	if existing == nil {
		return nil, false, fmt.Errorf("existing memory is required")
	}

	changed := false
	confidence := math.Max(existing.Confidence, normalizeConfidence(request.Confidence))
	if confidence != existing.Confidence {
		existing.Confidence = confidence
		changed = true
	}
	tags := joinTags(mergeTags(splitTags(existing.Tags), request.Tags))
	if tags != existing.Tags {
		existing.Tags = tags
		changed = true
	}
	if existing.SourceURI == "" && strings.TrimSpace(request.SourceURI) != "" {
		existing.SourceURI = strings.TrimSpace(request.SourceURI)
		changed = true
	}
	if existing.SourceLabel == "" && strings.TrimSpace(request.SourceLabel) != "" {
		existing.SourceLabel = strings.TrimSpace(request.SourceLabel)
		changed = true
	}
	if request.Summary != "" {
		summary := compactSummary(request.Summary)
		if summary != existing.Summary {
			existing.Summary = summary
			changed = true
		}
	}
	if existing.Archived {
		existing.Archived = false
		changed = true
	}
	if !changed {
		return existing, false, nil
	}
	saved, err := s.repo.Update(existing)
	return saved, err == nil, err
}

func (s *service) mergeSimilar(existing *models.ContextMemory, request CreateRequest) (*models.ContextMemory, error) {
	if sameSourceURI(existing.SourceURI, request.SourceURI) {
		existing.Content = strings.TrimSpace(request.Content)
		existing.Summary = compactSummary(firstNonEmpty(request.Summary, request.Content))
	} else {
		existing.Content = compactMergedContent(existing.Content, request.Content)
		existing.Summary = compactSummary(existing.Content)
	}
	existing.Confidence = math.Max(existing.Confidence, normalizeConfidence(request.Confidence)-0.05)
	existing.Tags = joinTags(mergeTags(splitTags(existing.Tags), request.Tags))
	if existing.SourceURI == "" {
		existing.SourceURI = request.SourceURI
	}
	if existing.SourceLabel == "" {
		existing.SourceLabel = request.SourceLabel
	}
	existing.ContentHash = hashContent(existing.ProjectKey, existing.Kind, existing.Content)
	existing.Archived = false
	return s.repo.Update(existing)
}

func provenanceCanMerge(existingURI, incomingURI string) bool {
	existingURI = strings.TrimSpace(existingURI)
	incomingURI = strings.TrimSpace(incomingURI)
	return existingURI == "" || incomingURI == "" || existingURI == incomingURI
}

func sameSourceURI(existingURI, incomingURI string) bool {
	existingURI = strings.TrimSpace(existingURI)
	incomingURI = strings.TrimSpace(incomingURI)
	return existingURI != "" && existingURI == incomingURI
}

func scoreMemory(memory models.ContextMemory, request RetrieveRequest, semanticSimilarity float64) (float64, string) {
	queryTokens := tokenSet(request.Query)
	memoryTokens := tokenSet(memory.Content + " " + memory.Summary + " " + memory.Tags)
	relevance := overlapScore(queryTokens, memoryTokens)
	semanticRelevance := math.Max(0, math.Min(1, semanticSimilarity))
	if relevance == 0 && semanticRelevance == 0 {
		return 0, "no lexical or semantic relevance"
	}
	projectMatch := 0.0
	if request.ProjectKey != "" && request.ProjectKey == memory.ProjectKey {
		projectMatch = 0.2
	}
	recency := recencyScore(memory.UpdatedAt)
	confidence := normalizeConfidence(memory.Confidence) * 0.25
	score := relevance*0.45 + confidence + recency*0.10 + projectMatch
	if semanticRelevance > 0 {
		score = math.Max(score, semanticRelevance*0.45+confidence+recency*0.10+projectMatch)
	}

	parts := []string{
		fmt.Sprintf("relevance %.2f", relevance),
		fmt.Sprintf("confidence %.2f", memory.Confidence),
		fmt.Sprintf("recency %.2f", recency),
	}
	if semanticRelevance > 0 {
		parts = append(parts, fmt.Sprintf("local semantic similarity %.2f", semanticRelevance))
	}
	if projectMatch > 0 {
		parts = append(parts, "same project")
	}
	return score, strings.Join(parts, ", ")
}

func (s *service) indexMemory(memory *models.ContextMemory) {
	if memory == nil || s.semanticService == nil || !s.semanticService.Enabled() {
		return
	}
	// Indexing is optional enrichment. The saved context memory remains the
	// authority and keyword retrieval continues if its local index is offline.
	_ = s.semanticService.IndexMemory(context.Background(), memory)
}

func (s *service) deleteMemoryIndex(id uuid.UUID) {
	if id == uuid.Nil || s.semanticService == nil || !s.semanticService.Enabled() {
		return
	}
	_ = s.semanticService.DeleteMemory(context.Background(), id)
}

func (s *service) semanticScores(ctx context.Context, ownerIdentity string, request RetrieveRequest) (map[uuid.UUID]float64, string, error) {
	if s.semanticService == nil || !s.semanticService.Enabled() {
		return map[uuid.UUID]float64{}, "", nil
	}
	matches, err := s.semanticService.SearchMemory(ctx, semantic.MemorySearchRequest{
		OwnerIdentity: ownerIdentity,
		Query:         request.Query,
		ProjectKey:    request.ProjectKey,
		Limit:         request.Limit,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		return map[uuid.UUID]float64{}, "Local semantic memory enrichment was unavailable; keyword ranking was used.", nil
	}
	scores := make(map[uuid.UUID]float64, len(matches))
	for _, match := range matches {
		scores[match.Memory.ID] = match.Similarity
	}
	return scores, fmt.Sprintf("Local pgvector semantic enrichment evaluated %d eligible memory matches after owner and project filtering.", len(matches)), nil
}

func hashContent(projectKey, kind, content string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(projectKey + "|" + kind + "|" + normalizeText(content))))
	return fmt.Sprintf("%x", hash.Sum64())
}

func compactSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 360 {
		return value
	}
	return value[:357] + "..."
}

func compactMergedContent(existing, incoming string) string {
	merged := strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" || strings.Contains(strings.ToLower(merged), strings.ToLower(incoming)) {
		return compactLongText(merged)
	}
	return compactLongText(merged + "\n" + incoming)
}

func compactLongText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 1800 {
		return value
	}
	return value[:1797] + "..."
}

func similarity(left, right string) float64 {
	leftSet := tokenSet(left)
	rightSet := tokenSet(right)
	if len(leftSet) == 0 || len(rightSet) == 0 {
		return 0
	}
	intersection := 0
	for token := range leftSet {
		if rightSet[token] {
			intersection++
		}
	}
	union := len(leftSet) + len(rightSet) - intersection
	return float64(intersection) / float64(union)
}

func overlapScore(queryTokens, memoryTokens map[string]bool) float64 {
	if len(queryTokens) == 0 {
		return 0.2
	}
	matches := 0
	for token := range queryTokens {
		if memoryTokens[token] {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func recencyScore(value time.Time) float64 {
	days := time.Since(value).Hours() / 24
	if days <= 1 {
		return 1
	}
	if days >= 90 {
		return 0.05
	}
	return 1 - (days / 100)
}

func tokenSet(value string) map[string]bool {
	set := map[string]bool{}
	for _, token := range strings.Fields(normalizeText(value)) {
		if len(token) < 3 {
			continue
		}
		set[token] = true
	}
	return set
}

func normalizeText(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ", "\n", " ", "\t", " ", "(", " ", ")", " ")
	return replacer.Replace(value)
}

func normalizeConfidence(value float64) float64 {
	if value <= 0 {
		return 0.7
	}
	if value > 1 {
		return 1
	}
	return value
}

func joinTags(tags []string) string {
	return strings.Join(mergeTags(nil, tags), ",")
}

func splitTags(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func mergeTags(existing, incoming []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, tag := range append(existing, incoming...) {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

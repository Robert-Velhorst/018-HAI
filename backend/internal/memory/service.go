package memory

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	maxMemoryConsolidationCandidates = 25
	maxMemoryContentBytes            = 32 * 1024
	maxMemorySummaryInputBytes       = 8 * 1024
	maxMemoryRequestBytes            = 64 * 1024
	maxMemoryTags                    = 32
)

var memoryCreateLocks [128]sync.Mutex

func isReviewManagedMemoryKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind == "source" || kind == "source_supported_fact" || strings.HasPrefix(kind, "correction_")
}

func requireOwnerIdentity(ownerIdentity string) (string, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return "", errors.New("owner identity is required")
	}
	if err := validateMemoryText("owner identity", ownerIdentity, 255); err != nil {
		return "", err
	}
	return ownerIdentity, nil
}

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
// while giving authenticated boundaries a fail-closed owner-aware API. Every
// owner-scoped method rejects a blank identity; unscoped Service methods are
// reserved for trusted internal workflows.
type OwnerScopedService interface {
	CreateForOwner(ownerIdentity string, request CreateRequest) (*models.ContextMemory, error)
	UpdateForOwner(ownerIdentity string, id uuid.UUID, request UpdateRequest) (*models.ContextMemory, error)
	FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error)
	FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error)
	ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool) (*models.ContextMemory, error)
	DeleteForOwner(ownerIdentity string, id uuid.UUID) error
	RetrieveForOwner(ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error)
}

// VerifiedFactProvenance carries identifiers and source strings from the
// verification workflow. The configured repository must resolve these values
// against persisted owner-bound run, claim, evidence, and opt-in records before
// it permits a memory write.
type VerifiedFactProvenance struct {
	RunID      uuid.UUID
	ClaimID    uuid.UUID
	EvidenceID uuid.UUID
	SourceURI  string
	SourceID   string
}

// VerifiedFactWriter is deliberately separate from generic memory writes so
// callers cannot label arbitrary content as source-verified through Create.
type VerifiedFactWriter interface {
	CreateVerifiedFactForOwner(ownerIdentity string, request CreateRequest, provenance VerifiedFactProvenance) (*models.ContextMemory, error)
}

// SemanticReindexService is intentionally separate from Service so existing
// workers and test doubles do not gain a bulk local-embedding capability by
// accident. HTTP access remains authenticated and write-authorized.
type SemanticReindexService interface {
	ReindexSemanticForOwner(ownerIdentity string, limit int) (*SemanticReindexResult, error)
}

type MemoryHealthService interface {
	MemoryHealthForOwner(ownerIdentity, projectKey string) (*MemoryHealthReport, error)
}

// RecentMemoryService is a bounded, chronological listing for operational
// surfaces. It intentionally has no search or total count semantics.
type RecentMemoryService interface {
	RecentForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error)
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
	if isReviewManagedMemoryKind(request.Kind) {
		return nil, errors.New("review-managed memory must be written by its verification workflow")
	}
	return s.createForOwner("", request)
}

func (s *service) MemoryHealthForOwner(ownerIdentity, projectKey string) (*MemoryHealthReport, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
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
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	health, ok := service.(MemoryHealthService)
	if !ok {
		return nil, fmt.Errorf("memory health review is unavailable")
	}
	return health.MemoryHealthForOwner(ownerIdentity, projectKey)
}

// CreateForOwner stores a memory under the authenticated owner. It never
// deduplicates against ownerless legacy records or another owner's records.
func (s *service) CreateForOwner(ownerIdentity string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if isReviewManagedMemoryKind(request.Kind) {
		return nil, errors.New("review-managed memory must be written by its review workflow")
	}
	return s.createForOwner(ownerIdentity, request)
}

func (s *service) CreateVerifiedFactForOwner(ownerIdentity string, request CreateRequest, provenance VerifiedFactProvenance) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if err := validateVerifiedFactRequest(request, provenance); err != nil {
		return nil, err
	}
	request.Kind = "source_supported_fact"
	if repository, ok := s.repo.(VerifiedFactPromotionRepository); ok {
		projectKey := strings.TrimSpace(request.ProjectKey)
		lockKey := memoryCreateLockKey(ownerIdentity, projectKey, request.Kind)
		lock := memoryCreateLock(lockKey)
		lock.Lock()
		defer lock.Unlock()
		saved, writeErr := repository.WithVerifiedFactPromotion(
			context.Background(), ownerIdentity, request, provenance,
			func(txRepository Repository) (*models.ContextMemory, error) {
				return s.createForOwnerInRepository(txRepository, ownerIdentity, projectKey, request.Kind, strings.TrimSpace(request.Content), request)
			},
		)
		if writeErr != nil {
			return saved, writeErr
		}
		s.indexMemory(saved)
		return saved, nil
	}
	return nil, errors.New("durable verified-fact provenance validation is unavailable")
}

func validateVerifiedFactRequest(request CreateRequest, provenance VerifiedFactProvenance) error {
	if strings.TrimSpace(request.Kind) != "source_supported_fact" {
		return errors.New("verification workflow may only create source-supported facts")
	}
	if strings.TrimSpace(request.Content) == "" || math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) || request.Confidence <= 0 || request.Confidence > 1 {
		return errors.New("verified fact content and confidence are invalid")
	}
	if provenance.RunID == uuid.Nil || provenance.ClaimID == uuid.Nil || provenance.EvidenceID == uuid.Nil {
		return errors.New("verified fact run, claim, and evidence identifiers are required")
	}
	if strings.TrimSpace(request.SourceLabel) != "verification-run:"+provenance.RunID.String() {
		return errors.New("verified fact run label does not match its provenance")
	}
	proofSource := strings.TrimSpace(provenance.SourceURI)
	if proofSource == "" {
		proofSource = strings.TrimSpace(provenance.SourceID)
	}
	if proofSource == "" || strings.TrimSpace(request.SourceURI) != proofSource {
		return errors.New("verified fact source does not match its evidence provenance")
	}
	return nil
}

func CreateForOwner(service Service, ownerIdentity string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		// Trusted internal workers may explicitly use the legacy unscoped path.
		// Authenticated HTTP handlers validate identity before calling this helper.
		return service.Create(request)
	}
	scoped, ok := service.(OwnerScopedService)
	if !ok {
		return nil, fmt.Errorf("owner-scoped memory service is unavailable")
	}
	return scoped.CreateForOwner(ownerIdentity, request)
}

// CreateVerifiedFactForOwner is the sole owner-scoped persistence path for
// source-supported facts. The configured writer must validate durable run,
// claim, evidence, freshness, and owner opt-in records before persisting.
func CreateVerifiedFactForOwner(service Service, ownerIdentity string, request CreateRequest, provenance VerifiedFactProvenance) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	writer, ok := service.(VerifiedFactWriter)
	if !ok {
		return nil, fmt.Errorf("verified-fact memory writer is unavailable")
	}
	return writer.CreateVerifiedFactForOwner(ownerIdentity, request, provenance)
}

func RetrieveForOwner(service Service, ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		// Trusted internal workers may explicitly use the legacy unscoped path.
		// Authenticated HTTP handlers validate identity before calling this helper.
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
	if err := validateMemoryCreateRequest(ownerIdentity, request); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(request.Content)
	kind := firstNonEmpty(strings.TrimSpace(request.Kind), "project")
	projectKey := strings.TrimSpace(request.ProjectKey)
	lockKey := memoryCreateLockKey(ownerIdentity, projectKey, kind)
	lock := memoryCreateLock(lockKey)
	lock.Lock()
	saved, err := func() (*models.ContextMemory, error) {
		defer lock.Unlock()
		if repository, ok := s.repo.(MemoryDeduplicationRepository); ok {
			return repository.WithMemoryDeduplicationLock(context.Background(), lockKey, func(txRepository Repository) (*models.ContextMemory, error) {
				return s.createForOwnerInRepository(txRepository, ownerIdentity, projectKey, kind, content, request)
			})
		}
		return s.createForOwnerInRepository(s.repo, ownerIdentity, projectKey, kind, content, request)
	}()
	if err != nil {
		return saved, err
	}
	s.indexMemory(saved)
	return saved, nil
}

func (s *service) createForOwnerInRepository(repo Repository, ownerIdentity, projectKey, kind, content string, request CreateRequest) (*models.ContextMemory, error) {
	reviewManaged := isReviewManagedMemoryKind(kind)
	contentHash := hashContent(projectKey, kind, content)

	if ownerIdentity == "" {
		if existing, err := repo.FindByHash(projectKey, kind, contentHash); err == nil {
			return s.mergeExact(repo, existing, request)
		} else if err != nil && !isNotFound(err) {
			return nil, err
		}
	}

	memories, err := findAllReadableFromRepository(repo, ownerIdentity, projectKey, false)
	if err != nil {
		return nil, err
	}
	for _, candidate := range memories {
		if ownerIdentity != "" && candidate.OwnerIdentity != ownerIdentity {
			continue
		}
		if candidate.Kind == kind && candidate.ContentHash == contentHash && normalizeText(candidate.Content) == normalizeText(content) {
			if reviewManaged &&
				(strings.TrimSpace(candidate.SourceURI) != strings.TrimSpace(request.SourceURI) ||
					strings.TrimSpace(candidate.SourceLabel) != strings.TrimSpace(request.SourceLabel)) {
				continue
			}
			copyCandidate := candidate
			return s.mergeExact(repo, &copyCandidate, request)
		}
		// Review-managed facts retain their source/run lineage; approximate
		// deduplication can combine distinct claims and conceal an evidence link.
		if !reviewManaged && candidate.Kind == kind && similarity(candidate.Content, content) >= 0.78 {
			copyCandidate := candidate
			return s.mergeSimilar(repo, &copyCandidate, request)
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
	return repo.Create(memory)
}

func memoryCreateLock(key string) *sync.Mutex {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return &memoryCreateLocks[hash.Sum64()%uint64(len(memoryCreateLocks))]
}

func memoryCreateLockKey(ownerIdentity, projectKey, kind string) string {
	return fmt.Sprintf("%d:%s|%d:%s|%d:%s",
		len(ownerIdentity), ownerIdentity, len(projectKey), projectKey, len(kind), kind)
}

func validateMemoryCreateRequest(ownerIdentity string, request CreateRequest) error {
	if ownerIdentity != "" {
		if err := validateMemoryText("owner identity", ownerIdentity, 255); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.Content) == "" {
		return errors.New("content is required")
	}
	if err := validateMemoryText("content", request.Content, maxMemoryContentBytes); err != nil {
		return err
	}
	if err := validateMemoryText("project key", strings.TrimSpace(request.ProjectKey), 255); err != nil {
		return err
	}
	kind := firstNonEmpty(strings.TrimSpace(request.Kind), "project")
	if err := validateMemoryText("kind", kind, 50); err != nil {
		return err
	}
	if err := validateMemoryText("summary", request.Summary, maxMemorySummaryInputBytes); err != nil {
		return err
	}
	if len(request.Tags) > maxMemoryTags {
		return fmt.Errorf("tags may contain at most %d entries", maxMemoryTags)
	}
	for _, tag := range request.Tags {
		if err := validateMemoryText("tag", tag, 512); err != nil {
			return err
		}
	}
	if err := validateMemoryText("tags", joinTags(request.Tags), 512); err != nil {
		return err
	}
	if err := validateMemoryText("source URI", strings.TrimSpace(request.SourceURI), 1024); err != nil {
		return err
	}
	if err := validateMemoryText("source label", strings.TrimSpace(request.SourceLabel), 255); err != nil {
		return err
	}
	if math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) {
		return errors.New("confidence must be a finite number")
	}
	return nil
}

func validateMemoryUpdateRequest(request UpdateRequest) error {
	if request.Content != "" {
		if strings.TrimSpace(request.Content) == "" {
			return errors.New("content cannot be blank")
		}
		if err := validateMemoryText("content", request.Content, maxMemoryContentBytes); err != nil {
			return err
		}
	}
	if request.ProjectKey != "" {
		if err := validateMemoryText("project key", strings.TrimSpace(request.ProjectKey), 255); err != nil {
			return err
		}
	}
	if request.Kind != "" {
		kind := strings.TrimSpace(request.Kind)
		if kind == "" {
			return errors.New("kind cannot be blank")
		}
		if err := validateMemoryText("kind", kind, 50); err != nil {
			return err
		}
	}
	if err := validateMemoryText("summary", request.Summary, maxMemorySummaryInputBytes); err != nil {
		return err
	}
	if len(request.Tags) > maxMemoryTags {
		return fmt.Errorf("tags may contain at most %d entries", maxMemoryTags)
	}
	for _, tag := range request.Tags {
		if err := validateMemoryText("tag", tag, 512); err != nil {
			return err
		}
	}
	if request.Tags != nil {
		if err := validateMemoryText("tags", joinTags(request.Tags), 512); err != nil {
			return err
		}
	}
	if err := validateMemoryText("source URI", strings.TrimSpace(request.SourceURI), 1024); err != nil {
		return err
	}
	if err := validateMemoryText("source label", strings.TrimSpace(request.SourceLabel), 255); err != nil {
		return err
	}
	if math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) {
		return errors.New("confidence must be a finite number")
	}
	return nil
}

func validateMemoryText(field, value string, maxBytes int) error {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s contains invalid text", field)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%s exceeds the %d-byte limit", field, maxBytes)
	}
	return nil
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
	if memory != nil && (isReviewManagedMemoryKind(memory.Kind) || isReviewManagedMemoryKind(request.Kind)) {
		return nil, errors.New("review-managed memory can only be changed by its verification workflow")
	}
	if err := validateMemoryUpdateRequest(request); err != nil {
		return nil, err
	}
	wasArchived := memory.Archived
	previousProjectKey, previousContent, previousSummary, previousTags := memory.ProjectKey, memory.Content, memory.Summary, memory.Tags
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
	indexInputsChanged := memory.ProjectKey != previousProjectKey || memory.Content != previousContent || memory.Summary != previousSummary || memory.Tags != previousTags
	rebuildSemanticIndex := !memory.Archived && (wasArchived || request.Archived != nil || indexInputsChanged)
	removeStaleSemanticIndex := memory.Archived || rebuildSemanticIndex
	if removeStaleSemanticIndex {
		if err := s.deleteMemoryIndex(memory.ID); err != nil {
			return nil, fmt.Errorf("memory update was not persisted because semantic index deletion failed: %w", err)
		}
	}
	saved, err := s.repo.Update(memory)
	if err != nil {
		if removeStaleSemanticIndex {
			return nil, fmt.Errorf("semantic index entry was deleted before the SQL update; keyword retrieval remains available and the operation is retryable: %w", err)
		}
		return nil, err
	}
	if saved.Archived {
		return saved, nil
	}
	if rebuildSemanticIndex {
		if err := s.indexMemoryStrict(saved); err != nil {
			if wasArchived {
				return saved, fmt.Errorf("memory is active in SQL but semantic index rebuild failed; retry the unarchive operation: %w", err)
			}
			return saved, fmt.Errorf("memory was saved but its semantic index refresh failed; keyword retrieval remains available and reindexing can repair it: %w", err)
		}
	}
	return saved, nil
}

func (s *service) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return s.repo.FindAll(projectKey, includeArchived)
}

func (s *service) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.findAllReadable(ownerIdentity, projectKey, includeArchived)
}

func (s *service) RecentForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if scoped, ok := s.repo.(RecentOwnerScopedRepository); ok {
		return scoped.FindRecentForOwner(ownerIdentity, projectKey, includeArchived, limit)
	}
	memories, err := s.findAllReadable(ownerIdentity, projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	if len(memories) > limit {
		return memories[:limit], nil
	}
	return memories, nil
}

func (s *service) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	return s.repo.FindByID(id)
}

func (s *service) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if scoped, ok := s.repo.(OwnerScopedRepository); ok {
		return scoped.FindByIDForOwner(ownerIdentity, id)
	}
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
	return s.saveArchiveState(memory, archived)
}

func (s *service) ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	memory, err := s.writeableMemoryForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	return s.saveArchiveState(memory, archived)
}

func (s *service) saveArchiveState(memory *models.ContextMemory, archived bool) (*models.ContextMemory, error) {
	memory.Archived = archived
	if archived {
		if err := s.deleteMemoryIndex(memory.ID); err != nil {
			return nil, fmt.Errorf("memory was not archived because semantic index deletion failed: %w", err)
		}
	}
	saved, err := s.repo.Update(memory)
	if err != nil {
		if archived {
			return nil, fmt.Errorf("semantic index entry was deleted but SQL archive update failed; persisted state may be uncertain and the operation is retryable: %w", err)
		}
		return nil, err
	}
	if !archived {
		if err := s.indexMemoryStrict(saved); err != nil {
			return saved, fmt.Errorf("memory is unarchived in SQL but semantic index rebuild failed; retry the unarchive operation: %w", err)
		}
	}
	return saved, nil
}

func (s *service) Delete(id uuid.UUID) error {
	if err := s.deleteMemoryIndex(id); err != nil {
		return fmt.Errorf("SQL memory was preserved because semantic index deletion failed: %w", err)
	}
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("semantic index entry was deleted but SQL memory deletion failed; SQL state may be uncertain and the operation is retryable: %w", err)
	}
	return nil
}

func (s *service) DeleteForOwner(ownerIdentity string, id uuid.UUID) error {
	if _, err := s.writeableMemoryForOwner(ownerIdentity, id); err != nil {
		return err
	}
	if err := s.deleteMemoryIndex(id); err != nil {
		return fmt.Errorf("SQL memory was preserved because semantic index deletion failed: %w", err)
	}
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("semantic index entry was deleted but SQL memory deletion failed; SQL state may be uncertain and the operation is retryable: %w", err)
	}
	return nil
}

func (s *service) Retrieve(request RetrieveRequest) (*RetrieveResult, error) {
	return s.retrieveForOwner("", request)
}

func (s *service) RetrieveForOwner(ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.retrieveForOwner(ownerIdentity, request)
}

func (s *service) ReindexSemanticForOwner(ownerIdentity string, limit int) (*SemanticReindexResult, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
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
	memories, err := s.findAllReadable(ownerIdentity, "", false)
	if err != nil {
		return nil, err
	}
	result := &SemanticReindexResult{Enabled: true}
	for index, item := range memories {
		if index >= limit {
			result.Deferred = len(memories) - index
			break
		}
		result.Attempted++
		copyItem := item
		if err := s.semanticService.IndexMemory(context.Background(), &copyItem); err != nil {
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

func (s *service) retrieveForOwner(ownerIdentity string, request RetrieveRequest) (*RetrieveResult, error) {
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	projectKey := strings.TrimSpace(request.ProjectKey)
	memories, err := s.findAllReadable(ownerIdentity, "", false)
	if err != nil {
		return nil, err
	}
	candidates := make([]models.ContextMemory, 0, len(memories))
	for _, memory := range memories {
		if projectKey != "" && memory.ProjectKey != "" && memory.ProjectKey != projectKey {
			continue
		}
		candidates = append(candidates, memory)
	}
	semanticScores, semanticState := s.semanticScores(ownerIdentity, request)

	ranked := make([]RankedMemory, 0, len(candidates))
	for _, memory := range candidates {
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
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if scoped, ok := s.repo.(OwnerScopedRepository); ok {
		return scoped.FindByIDForOwner(ownerIdentity, id)
	}
	memory, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !writeableByOwner(memory, ownerIdentity) {
		return nil, gorm.ErrRecordNotFound
	}
	return memory, nil
}

func (s *service) findAllReadable(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	return findAllReadableFromRepository(s.repo, ownerIdentity, projectKey, includeArchived)
}

func findAllReadableFromRepository(repo Repository, ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	if scoped, ok := repo.(OwnerScopedRepository); ok && ownerIdentity != "" {
		return scoped.FindAllForOwner(ownerIdentity, projectKey, includeArchived)
	}
	memories, err := repo.FindAll(projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	return filterReadableMemories(memories, ownerIdentity), nil
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
	// Legacy records without an owner are quarantined. Treating them as global
	// would expose personal memories to every authenticated account.
	return memory.OwnerIdentity != "" && memory.OwnerIdentity == ownerIdentity
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

func (s *service) mergeExact(repo Repository, existing *models.ContextMemory, request CreateRequest) (*models.ContextMemory, error) {
	existing.Confidence = math.Max(existing.Confidence, normalizeConfidence(request.Confidence))
	existing.Tags = joinTags(mergeTags(splitTags(existing.Tags), request.Tags))
	if existing.SourceURI == "" {
		existing.SourceURI = request.SourceURI
	}
	if existing.SourceLabel == "" {
		existing.SourceLabel = request.SourceLabel
	}
	if request.Summary != "" {
		existing.Summary = compactSummary(request.Summary)
	}
	existing.Archived = false
	return repo.Update(existing)
}

func (s *service) mergeSimilar(repo Repository, existing *models.ContextMemory, request CreateRequest) (*models.ContextMemory, error) {
	existing.Content = compactMergedContent(existing.Content, request.Content)
	existing.Summary = compactSummary(existing.Content)
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
	return repo.Update(existing)
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

func (s *service) indexMemoryStrict(memory *models.ContextMemory) error {
	if memory == nil || s.semanticService == nil || !s.semanticService.Enabled() {
		return nil
	}
	return s.semanticService.IndexMemory(context.Background(), memory)
}

func (s *service) deleteMemoryIndex(id uuid.UUID) error {
	if id == uuid.Nil || s.semanticService == nil {
		return nil
	}
	// Deletion remains available when retrieval is disabled: the semantic
	// adapter performs database-only cleanup without requesting an embedding.
	return s.semanticService.DeleteMemory(context.Background(), id)
}

func (s *service) semanticScores(ownerIdentity string, request RetrieveRequest) (map[uuid.UUID]float64, string) {
	if s.semanticService == nil || !s.semanticService.Enabled() {
		return map[uuid.UUID]float64{}, ""
	}
	matches, err := s.semanticService.SearchMemory(context.Background(), semantic.MemorySearchRequest{
		OwnerIdentity: ownerIdentity,
		Query:         request.Query,
		ProjectKey:    request.ProjectKey,
		Limit:         request.Limit,
	})
	if err != nil {
		return map[uuid.UUID]float64{}, "Local semantic memory enrichment was unavailable; keyword ranking was used."
	}
	scores := make(map[uuid.UUID]float64, len(matches))
	for _, match := range matches {
		scores[match.Memory.ID] = match.Similarity
	}
	return scores, fmt.Sprintf("Local pgvector semantic enrichment evaluated %d eligible memory matches after owner and project filtering.", len(matches))
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
	return truncateAtUTF8Boundary(value, 357) + "..."
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
	return truncateAtUTF8Boundary(value, 1797) + "..."
}

func truncateAtUTF8Boundary(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	if maxBytes <= 0 {
		return ""
	}
	for maxBytes > 0 && !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
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

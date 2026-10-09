package verification

import (
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrVerificationRunNotFound indicates that an owner-scoped run lookup found no visible run.
var ErrVerificationRunNotFound = errors.New("verification run not found")

const (
	ModeDraft    = "draft"
	ModeGrounded = "grounded"
	ModeStrict   = "strict"
	ModeAction   = "action"

	StatusVerified        = "verified"
	StatusSourceSupported = "source_supported"
	StatusSchemaValidated = "schema_validated"
	StatusTestPassed      = "test_passed"
	StatusHumanApproved   = "human_approved"
	StatusUncertain       = "uncertain"
	StatusConflicting     = "conflicting"
	StatusUnsupported     = "unsupported"
	StatusNeedsReview     = "needs_review"

	authorityConnectedReviewRequired = "connected_source:review_required"
	minimumClaimEvidenceOverlap      = 0.22
	minimumMemoryPromotionConfidence = 0.70
	minimumMemoryEvidenceQuality     = 0.55
)

type EvidenceInput struct {
	SourceType  string `json:"sourceType"`
	SourceID    string `json:"sourceId,omitempty"`
	SourceURI   string `json:"sourceUri,omitempty"`
	SourceLabel string `json:"sourceLabel,omitempty"`
	Snippet     string `json:"snippet"`
	Authority   string `json:"authority,omitempty"`
	Freshness   string `json:"freshness,omitempty"`
	Official    bool   `json:"official,omitempty"`
	Primary     bool   `json:"primary,omitempty"`
	Generated   bool   `json:"generated,omitempty"`
}

type AnswerRequest struct {
	OwnerIdentity    string          `json:"-"`
	Question         string          `json:"question"`
	ProjectKey       string          `json:"projectKey,omitempty"`
	PursuitID        string          `json:"pursuitId,omitempty"`
	Mode             string          `json:"mode,omitempty"`
	DraftAnswer      string          `json:"draftAnswer,omitempty"`
	ExternalEvidence []EvidenceInput `json:"externalEvidence,omitempty"`
	IncludeSensitive bool            `json:"includeSensitive,omitempty"`
	// Legacy compatibility hints are deliberately excluded from JSON and are
	// never accepted as proof of a server-validated approval record.
	HumanApproved          bool   `json:"-"`
	HumanApprovalReference string `json:"-"`
	AllowMemoryUpdate      bool   `json:"allowMemoryUpdate,omitempty"`
}

type VerificationResult struct {
	Run                       models.VerificationRun        `json:"run"`
	PursuitID                 string                        `json:"pursuitId,omitempty"`
	PursuitLinked             bool                          `json:"pursuitLinked,omitempty"`
	PursuitLinkError          string                        `json:"pursuitLinkError,omitempty"`
	Claims                    []models.VerificationClaim    `json:"claims"`
	Evidence                  []models.VerificationEvidence `json:"evidence"`
	UnsupportedClaims         []models.VerificationClaim    `json:"unsupportedClaims"`
	ResearchQuestions         []string                      `json:"researchQuestions"`
	Logs                      []string                      `json:"logs"`
	AuditWarnings             []string                      `json:"auditWarnings,omitempty"`
	KnowledgeClaimIDs         []string                      `json:"knowledgeClaimIds,omitempty"`
	KnowledgeError            string                        `json:"knowledgeProjectionError,omitempty"`
	MemoryUpdateRequested     bool                          `json:"memoryUpdateRequested,omitempty"`
	MemoryUpdatesStored       int                           `json:"memoryUpdatesStored,omitempty"`
	MemoryUpdateFailures      int                           `json:"memoryUpdateFailures,omitempty"`
	MemoryUpdatesSkipped      int                           `json:"memoryUpdatesSkipped,omitempty"`
	MemoryUpdateBlockedReason string                        `json:"memoryUpdateBlockedReason,omitempty"`
}

type Service interface {
	Answer(request AnswerRequest) (*VerificationResult, error)
	Runs() ([]models.VerificationRun, error)
	RunsForOwner(ownerIdentity string) ([]models.VerificationRun, error)
	RunDetails(id uuid.UUID) (*VerificationResult, error)
	RunDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error)
}

type PursuitLinker interface {
	LinkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error
}

type service struct {
	repo              Repository
	sourceService     ConnectedSourceSearcher
	sourceEvidence    sourceevidence.Repository
	memoryService     memory.Service
	pursuitLinker     PursuitLinker
	authorityResolver EvidenceAuthorityResolver
	claimProjector    ClaimProjector
}

type ConnectedSourceSearcher interface {
	Search(source.SearchRequest) (*source.SearchResult, error)
}

// ClaimProjector copies eligible source-backed claims into immutable semantic
// storage. Projection is advisory and never grants execution authority.
type ClaimProjector interface {
	ProjectClaims(context.Context, AnswerRequest, models.VerificationRun, []models.VerificationClaim, []models.VerificationEvidence) ([]string, error)
}

// WithClaimProjector returns a service copy with semantic projection enabled.
func WithClaimProjector(base Service, projector ClaimProjector) (Service, error) {
	implementation, ok := base.(*service)
	if !ok || implementation == nil {
		return nil, fmt.Errorf("verification claim projection requires the built-in service")
	}
	if projector == nil {
		return nil, fmt.Errorf("verification claim projector is required")
	}
	copy := *implementation
	copy.claimProjector = projector
	return &copy, nil
}

func NewService(repo Repository, sourceService ConnectedSourceSearcher, memoryService memory.Service, pursuitLinkers ...PursuitLinker) Service {
	return NewServiceWithEvidenceResolvers(repo, sourceService, memoryService, nil, nil, pursuitLinkers...)
}

func NewServiceWithAuthorityResolver(
	repo Repository,
	sourceService ConnectedSourceSearcher,
	memoryService memory.Service,
	authorityResolver EvidenceAuthorityResolver,
	pursuitLinkers ...PursuitLinker,
) Service {
	return NewServiceWithEvidenceResolvers(
		repo, sourceService, memoryService, authorityResolver, nil, pursuitLinkers...,
	)
}

func NewServiceWithEvidenceResolvers(
	repo Repository,
	sourceService ConnectedSourceSearcher,
	memoryService memory.Service,
	authorityResolver EvidenceAuthorityResolver,
	sourceEvidence sourceevidence.Repository,
	pursuitLinkers ...PursuitLinker,
) Service {
	var pursuitLinker PursuitLinker
	if len(pursuitLinkers) > 0 {
		pursuitLinker = pursuitLinkers[0]
	}
	if authorityResolver == nil {
		authorityResolver = untrustedExternalAuthorityResolver{}
	}
	return &service{
		repo:              repo,
		sourceService:     sourceService,
		sourceEvidence:    sourceEvidence,
		memoryService:     memoryService,
		pursuitLinker:     pursuitLinker,
		authorityResolver: authorityResolver,
	}
}

func DefaultService() Service {
	resolver, _ := sourceevidence.DefaultRepository()
	return NewServiceWithEvidenceResolvers(
		DefaultRepository(), source.DefaultService(), memory.DefaultService(), nil, resolver,
	)
}

func (s *service) Answer(request AnswerRequest) (*VerificationResult, error) {
	request.OwnerIdentity = strings.TrimSpace(request.OwnerIdentity)
	if request.OwnerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required")
	}
	pursuitID, err := requestedPursuitID(request.PursuitID)
	if err != nil {
		return nil, err
	}
	if pursuitID != uuid.Nil && s.pursuitLinker == nil {
		return nil, fmt.Errorf("pursuit linking is not configured")
	}
	mode := normalizeMode(request.Mode)
	questions := researchQuestions(request.Question, request.ProjectKey)
	logs := []string{"converted request into research questions"}
	run, err := s.repo.CreateRun(&models.VerificationRun{
		OwnerIdentity:     strings.TrimSpace(request.OwnerIdentity),
		Mode:              mode,
		Question:          strings.TrimSpace(request.Question),
		ProjectKey:        strings.TrimSpace(request.ProjectKey),
		Status:            StatusUncertain,
		ResearchQuestions: joinValues(questions),
		SourcesSearched:   "connected_sources,provided_evidence",
	})
	if err != nil {
		return nil, err
	}

	evidence := s.collectEvidence(run.ID, request, questions, &logs)
	answer := buildAnswer(request, mode, evidence)
	claims := decomposeClaims(run.ID, answer, mode, request)
	verifiedClaims := verifyClaims(claims, evidence, request, mode)
	unsupported := unsupportedClaims(verifiedClaims)
	status := runStatus(verifiedClaims)

	run.Answer = answer
	run.Status = status
	run.SourcesUsed = sourceLabels(filterEvidence(evidence, true))
	run.SourcesRejected = sourceLabels(filterRejectedEvidence(evidence))
	run.MissingSources = missingSources(request, evidence)
	// A verification result is only trustworthy when its evidence, claims, and
	// final status were durably stored. The Postgres repository performs that
	// write set atomically; compact test repositories retain fail-closed order.
	// Callers must not receive a completed-looking result with a missing audit
	// trail.
	finalizedRun, finalizeErr := s.persistFinalization(run, evidence, verifiedClaims)
	if finalizeErr != nil {
		if err := s.audit(run.ID, "verification.persistence_failed", finalizeErr.Error()); err != nil {
			return nil, fmt.Errorf("%w; could not durably record the verification failure audit", finalizeErr)
		}
		return nil, finalizeErr
	}
	run = finalizedRun
	auditWarnings := []string{}
	var result *VerificationResult
	recordAudit := func(action, message string) {
		if err := s.audit(run.ID, action, message); err != nil {
			auditWarnings = append(auditWarnings, action)
			if result != nil {
				result.AuditWarnings = append(result.AuditWarnings, action)
			}
			logs = append(logs, "audit event "+action+" could not be durably recorded")
		}
	}
	memoryUpdatesStored, memoryUpdateFailures, memoryUpdatesSkipped := 0, 0, 0
	memoryUpdateBlockedReason := ""
	promotedClaims := []models.VerificationClaim(nil)
	if request.AllowMemoryUpdate {
		if err := s.audit(run.ID, "verification.memory_update_requested", "authenticated owner explicitly opted in to source-supported memory promotion"); err != nil {
			auditWarnings = append(auditWarnings, "verification.memory_update_requested")
			memoryUpdatesSkipped = len(verifiedClaims)
			memoryUpdateBlockedReason = "owner memory opt-in could not be durably recorded"
			logs = append(logs, "durable-memory promotion was blocked because owner opt-in could not be durably recorded")
		} else {
			outcome := s.storeVerifiedMemory(request, run, verifiedClaims, evidence)
			memoryUpdatesStored = outcome.Stored
			memoryUpdateFailures = outcome.Failed
			memoryUpdatesSkipped = outcome.Skipped
			memoryUpdateBlockedReason = outcome.BlockedReason
			promotedClaims = outcome.PromotedClaims
		}
		if memoryUpdatesStored > 0 {
			logs = append(logs, fmt.Sprintf("stored %d source-grounded claim(s) in owner-scoped long-term memory", memoryUpdatesStored))
			recordAudit("verification.memory_promoted", fmt.Sprintf("promoted %d source-grounded claim(s) with verification-run provenance", memoryUpdatesStored))
		}
		if memoryUpdateFailures > 0 {
			logs = append(logs, fmt.Sprintf("%d eligible claim(s) could not be saved to long-term memory; verification results remain available", memoryUpdateFailures))
			recordAudit("verification.memory_update_failed", fmt.Sprintf("%d verified memory write(s) failed", memoryUpdateFailures))
		}
		if memoryUpdatesSkipped > 0 {
			logs = append(logs, fmt.Sprintf("skipped %d claim(s) that did not meet durable-memory source, confidence, risk, owner, or provenance requirements", memoryUpdatesSkipped))
		}
		if memoryUpdateBlockedReason != "" {
			logs = append(logs, "durable-memory promotion was blocked: "+memoryUpdateBlockedReason)
			recordAudit("verification.memory_promotion_blocked", memoryUpdateBlockedReason)
		}
	}
	knowledgeClaimIDs := []string(nil)
	knowledgeError := ""
	if s.claimProjector != nil && request.AllowMemoryUpdate && len(promotedClaims) > 0 {
		knowledgeClaimIDs, err = s.claimProjector.ProjectClaims(
			context.Background(), request, *run, promotedClaims, evidence,
		)
		if err != nil {
			knowledgeError = "semantic claim projection failed"
			logs = append(logs, knowledgeError)
			recordAudit("verification.knowledge_projection_failed", err.Error())
		} else if len(knowledgeClaimIDs) > 0 {
			logs = append(logs, "promoted source-grounded claims into immutable semantic knowledge")
			recordAudit("verification.knowledge_projected", fmt.Sprintf("projected %d memory-approved semantic claim(s)", len(knowledgeClaimIDs)))
		}
	}
	result = &VerificationResult{
		Run:                       *run,
		Claims:                    verifiedClaims,
		Evidence:                  evidence,
		UnsupportedClaims:         unsupported,
		ResearchQuestions:         questions,
		Logs:                      append(logs, "verification status logged for every important claim"),
		AuditWarnings:             append([]string(nil), auditWarnings...),
		KnowledgeClaimIDs:         knowledgeClaimIDs,
		KnowledgeError:            knowledgeError,
		MemoryUpdateRequested:     request.AllowMemoryUpdate,
		MemoryUpdatesStored:       memoryUpdatesStored,
		MemoryUpdateFailures:      memoryUpdateFailures,
		MemoryUpdatesSkipped:      memoryUpdatesSkipped,
		MemoryUpdateBlockedReason: memoryUpdateBlockedReason,
	}
	if pursuitID != uuid.Nil {
		result.PursuitID = pursuitID.String()
		if err := s.pursuitLinker.LinkVerificationForOwner(request.OwnerIdentity, pursuitID, run.ID); err != nil {
			result.PursuitLinkError = err.Error()
			result.Logs = append(result.Logs, "verification was saved but could not be linked to the requested pursuit")
			recordAudit("verification.pursuit_link_failed", err.Error())
		} else {
			result.PursuitLinked = true
			result.Logs = append(result.Logs, "verification linked to the requested pursuit")
			recordAudit("verification.pursuit_linked", "verification run linked to requested pursuit "+pursuitID.String())
		}
	}
	return result, nil
}

func (s *service) persistFinalization(
	run *models.VerificationRun,
	evidence []models.VerificationEvidence,
	claims []models.VerificationClaim,
) (*models.VerificationRun, error) {
	var stored *models.VerificationRun
	persist := func(repository Repository) error {
		if err := persistEvidenceAndClaims(repository, evidence, claims); err != nil {
			return err
		}
		if _, err := repository.CreateAuditLog(&models.VerificationAuditLog{
			RunID:   run.ID,
			Action:  "verification.completed",
			Message: "important claims decomposed and verified before acceptance",
		}); err != nil {
			return fmt.Errorf("persist verification completion audit: %w", err)
		}
		updated, err := repository.UpdateRun(run)
		if err != nil {
			return fmt.Errorf("finalize verification run: %w", err)
		}
		stored = updated
		return nil
	}
	if atomic, ok := s.repo.(AtomicRepository); ok {
		if err := atomic.WithinTransaction(persist); err != nil {
			return nil, err
		}
		return stored, nil
	}
	if err := persist(s.repo); err != nil {
		return nil, err
	}
	return stored, nil
}

func persistEvidenceAndClaims(repository Repository, evidence []models.VerificationEvidence, claims []models.VerificationClaim) error {
	for index := range evidence {
		if _, err := repository.CreateEvidence(&evidence[index]); err != nil {
			return fmt.Errorf("persist verification evidence: %w", err)
		}
	}
	for index := range claims {
		if _, err := repository.CreateClaim(&claims[index]); err != nil {
			return fmt.Errorf("persist verification claim: %w", err)
		}
	}
	return nil
}

func requestedPursuitID(value string) (uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid pursuitId")
	}
	return id, nil
}

func (s *service) Runs() ([]models.VerificationRun, error) {
	return s.repo.FindRuns()
}

func (s *service) RunsForOwner(ownerIdentity string) ([]models.VerificationRun, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required")
	}
	return s.repo.FindRunsForOwner(ownerIdentity)
}

func (s *service) RunDetails(id uuid.UUID) (*VerificationResult, error) {
	return s.runDetailsForOwner("", id)
}

func (s *service) RunDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error) {
	return s.runDetailsForOwner(strings.TrimSpace(ownerIdentity), id)
}

func (s *service) runDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error) {
	run, err := s.runForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	claims, err := s.repo.FindClaims(id)
	if err != nil {
		return nil, err
	}
	evidence, err := s.repo.FindEvidence(id)
	if err != nil {
		return nil, err
	}
	return &VerificationResult{
		Run:               *run,
		Claims:            claims,
		Evidence:          evidence,
		UnsupportedClaims: unsupportedClaims(claims),
		Logs:              []string{"loaded persisted verification details"},
	}, nil
}

func (s *service) runForOwner(ownerIdentity string, id uuid.UUID) (*models.VerificationRun, error) {
	if ownerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required")
	}
	if scoped, ok := s.repo.(OwnerScopedRunRepository); ok {
		run, err := scoped.FindRunForOwner(ownerIdentity, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerificationRunNotFound
		}
		if err == nil && run == nil {
			return nil, ErrVerificationRunNotFound
		}
		return run, err
	}
	runs, err := s.RunsForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	for index := range runs {
		if runs[index].ID == id {
			copy := runs[index]
			return &copy, nil
		}
	}
	return nil, ErrVerificationRunNotFound
}

func (s *service) collectEvidence(runID uuid.UUID, request AnswerRequest, questions []string, logs *[]string) []models.VerificationEvidence {
	evidence := []models.VerificationEvidence{}
	if s.sourceService != nil {
		for _, question := range questions {
			result, err := s.sourceService.Search(source.SearchRequest{
				OwnerIdentity:    request.OwnerIdentity,
				Query:            question,
				ProjectKey:       request.ProjectKey,
				Limit:            6,
				IncludeSensitive: request.IncludeSensitive,
			})
			if err == nil {
				for _, ranked := range result.UsedContext {
					item := models.VerificationEvidence{
						RunID:        runID,
						SourceType:   "connected_source",
						SourceID:     ranked.Extraction.ID.String(),
						SourceURI:    ranked.Extraction.SourceURI,
						SourceLabel:  ranked.Extraction.SourceLabel,
						Snippet:      firstNonEmpty(ranked.Extraction.Summary, ranked.Extraction.Text),
						QualityScore: math.Min(1, 0.62+ranked.Score/2),
					}
					requiresReview := ranked.RequiresReview || ranked.Extraction.Uncertain || strings.EqualFold(strings.TrimSpace(ranked.Extraction.ContentType), "trello_card")
					if s.sourceEvidence == nil {
						item.Authority = authorityConnectedUnverified
						item.Rejected = true
						item.RejectReason = "connected-source provenance resolver is unavailable"
					} else {
						snapshot, resolveErr := s.sourceEvidence.Resolve(
							context.Background(), request.OwnerIdentity, ranked.Extraction.ID.String(),
						)
						payloadDigest := sourceevidence.ExtractionPayloadDigest(ranked.Extraction)
						if resolveErr != nil || !sourceSnapshotMatches(request.OwnerIdentity, ranked.Extraction, snapshot, payloadDigest) {
							item.Authority = authorityConnectedUnverified
							item.Rejected = true
							item.RejectReason = "connected-source extraction could not be matched to exact durable raw provenance"
						} else {
							item.Freshness = freshnessLabel(snapshot.FetchedAt)
							if requiresReview {
								item.Authority = authorityConnectedReviewRequired
								item.Rejected = true
								item.RejectReason = "connected-source content is uncertain or Trello-derived and requires owner review before it can support a claim"
							} else {
								item.Authority = authorityConnectedProvenance
								item.Used = true
							}
						}
					}
					evidence = append(evidence, item)
				}
				*logs = append(*logs, "searched connected-source index")
			}
		}
	} else {
		*logs = append(*logs, "connected-source index is not configured; using only supplied evidence")
	}
	for _, input := range request.ExternalEvidence {
		authority := normalizeAuthorityResolution(s.authorityResolver.ResolveExternalEvidence(request, input))
		score := evidenceQuality(input, authority, request.Question)
		if !authority.Trusted {
			*logs = append(*logs, authority.Reason)
		}
		evidence = append(evidence, models.VerificationEvidence{
			RunID:        runID,
			SourceType:   firstNonEmpty(input.SourceType, "external"),
			SourceID:     input.SourceID,
			SourceURI:    input.SourceURI,
			SourceLabel:  input.SourceLabel,
			Snippet:      input.Snippet,
			Authority:    authority.Authority,
			Freshness:    input.Freshness,
			QualityScore: score,
			Used:         score >= 0.35,
			Rejected:     score < 0.35,
			RejectReason: rejectReason(score, input, authority),
		})
	}
	evidence = deduplicateEvidence(evidence)
	sort.SliceStable(evidence, func(i, j int) bool {
		return evidence[i].QualityScore > evidence[j].QualityScore
	})
	return evidence
}

func sourceSnapshotMatches(ownerIdentity string, extraction models.SourceExtraction, snapshot sourceevidence.Snapshot, payloadDigest string) bool {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	return ownerIdentity != "" &&
		snapshot.OwnerIdentity == ownerIdentity &&
		snapshot.ExtractionID == extraction.ID.String() &&
		snapshot.SourceID == extraction.SourceID.String() &&
		snapshot.RawItemID == extraction.RawItemID.String() &&
		snapshot.ProjectKey == strings.TrimSpace(extraction.ProjectKey) &&
		snapshot.ProjectKey == snapshot.RawProjectKey &&
		snapshot.ExtractionURI == strings.TrimSpace(extraction.SourceURI) &&
		snapshot.ExtractionURI != "" && snapshot.ExtractionURI == snapshot.RawItemURI &&
		snapshot.ExtractionHash == strings.TrimSpace(extraction.ContentHash) &&
		snapshot.ExtractionHash != "" && snapshot.ExtractionHash == snapshot.RawItemHash &&
		snapshot.ExtractionPayloadDigest == payloadDigest &&
		!snapshot.FetchedAt.IsZero() && !snapshot.ExtractionAt.IsZero() &&
		strings.TrimSpace(snapshot.ConnectorKey) != "" &&
		snapshot.SnapshotDigest != "" && snapshot.SnapshotDigest == sourceevidence.SnapshotDigest(snapshot)
}

func deduplicateEvidence(values []models.VerificationEvidence) []models.VerificationEvidence {
	byKey := make(map[string]models.VerificationEvidence, len(values))
	order := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.Join([]string{
			strings.TrimSpace(value.SourceType), strings.TrimSpace(value.SourceID),
			strings.TrimSpace(value.SourceURI), strings.TrimSpace(value.Snippet),
		}, "\x00")
		current, exists := byKey[key]
		if !exists {
			order = append(order, key)
			byKey[key] = value
			continue
		}
		currentRequiresReview := current.Authority == authorityConnectedReviewRequired
		valueRequiresReview := value.Authority == authorityConnectedReviewRequired
		if valueRequiresReview && !currentRequiresReview {
			byKey[key] = value
			continue
		}
		if currentRequiresReview && !valueRequiresReview {
			continue
		}
		if (!value.Rejected && current.Rejected) || value.QualityScore > current.QualityScore {
			byKey[key] = value
		}
	}
	result := make([]models.VerificationEvidence, 0, len(order))
	for _, key := range order {
		result = append(result, byKey[key])
	}
	return result
}

func buildAnswer(request AnswerRequest, mode string, evidence []models.VerificationEvidence) string {
	if mode == ModeDraft {
		return "Draft hypothesis: " + strings.TrimSpace(request.Question)
	}
	if strings.TrimSpace(request.DraftAnswer) != "" {
		return strings.TrimSpace(request.DraftAnswer)
	}
	used := filterEvidence(evidence, true)
	if len(used) == 0 {
		return "No grounded answer can be produced because no supporting evidence was found."
	}
	lines := []string{}
	for _, item := range used {
		lines = append(lines, compact(item.Snippet, 260))
		if len(lines) >= 5 {
			break
		}
	}
	return strings.Join(lines, ". ")
}

func decomposeClaims(runID uuid.UUID, answer, mode string, request AnswerRequest) []models.VerificationClaim {
	claims := []models.VerificationClaim{}
	for _, sentence := range splitClaims(answer) {
		if sentence == "" {
			continue
		}
		claims = append(claims, models.VerificationClaim{
			RunID:     runID,
			ClaimText: sentence,
			Status:    StatusUncertain,
			HighRisk:  highRisk(request.Question + " " + sentence),
		})
	}
	if len(claims) == 0 {
		claims = append(claims, models.VerificationClaim{
			RunID:       runID,
			ClaimText:   "No answer claim was generated.",
			Status:      StatusNeedsReview,
			NeedsReview: true,
		})
	}
	return claims
}

func verifyClaims(claims []models.VerificationClaim, evidence []models.VerificationEvidence, request AnswerRequest, mode string) []models.VerificationClaim {
	for i := range claims {
		claim := &claims[i]
		best, score := bestEvidenceForClaim(claim.ClaimText, evidence)
		if mode == ModeDraft {
			claim.Status = StatusUncertain
			claim.NeedsReview = true
			claim.SupportExplanation = "draft mode does not grant factual confidence"
			claim.Confidence = 0.25
			continue
		}
		if claim.HighRisk {
			claim.Status = StatusNeedsReview
			claim.NeedsReview = true
			claim.SupportExplanation = "high-risk claims remain review-gated until a server-verified approval record can be checked"
			claim.Confidence = 0.2
			continue
		}
		if ok, passed := deterministicCalculationCheck(claim.ClaimText); ok {
			if passed {
				claim.Status = StatusVerified
				claim.Confidence = 1
				claim.SupportExplanation = "arithmetic claim passed deterministic calculation"
			} else {
				claim.Status = StatusUnsupported
				claim.NeedsReview = true
				claim.Confidence = 0
				claim.SupportExplanation = "arithmetic claim failed deterministic calculation"
			}
			continue
		}
		if best == nil || score < 0.22 {
			claim.Status = StatusUnsupported
			claim.NeedsReview = mode == ModeStrict || mode == ModeAction || mode == ModeGrounded
			claim.SupportExplanation = "no source precisely supports this claim"
			claim.Confidence = math.Round(score*100) / 100
			continue
		}
		claim.SourceRefs = firstNonEmpty(best.SourceURI, best.SourceID, best.SourceLabel)
		claim.Confidence = math.Round((score+best.QualityScore)/2*100) / 100
		if !isSourceSupportedEvidence(best.Authority) {
			claim.Status = StatusNeedsReview
			claim.NeedsReview = true
			claim.SupportExplanation = "source content overlaps the claim, but its provenance authority is untrusted; review is required"
			continue
		}
		if isStaleEvidence(best) {
			claim.Status = StatusNeedsReview
			claim.NeedsReview = true
			claim.SupportExplanation = "source provenance is authenticated, but its snapshot is stale and must be refreshed before this claim can be supported"
			continue
		}
		claim.SupportExplanation = "claim overlaps source evidence with authenticated provenance; semantic truth is not inferred"
		claim.Status = StatusSourceSupported
		if best.SourceType == "test_result" && containsAny(strings.ToLower(best.Snippet), "pass", "passed", "ok") {
			claim.Status = StatusTestPassed
		}
		if containsContradiction(claim.ClaimText, evidence) {
			claim.Status = StatusConflicting
			claim.NeedsReview = true
			claim.SupportExplanation = "the claim conflicts with its supporting evidence; human review is required"
		}
	}
	return claims
}

func bestEvidenceForClaim(claim string, evidence []models.VerificationEvidence) (*models.VerificationEvidence, float64) {
	var best *models.VerificationEvidence
	bestScore := 0.0
	claimTokens := tokenSet(claim)
	for i := range evidence {
		if evidence[i].Rejected || evidence[i].Snippet == "" {
			continue
		}
		overlap := overlapScore(claimTokens, tokenSet(evidence[i].Snippet))
		// Source quality establishes provenance, not relevance. Do not let a
		// high-authority document support a claim when its text does not actually
		// overlap the claim at all (or only matches a negligible fragment).
		if overlap < minimumClaimEvidenceOverlap || overlapCount(claimTokens, tokenSet(evidence[i].Snippet)) < 2 {
			continue
		}
		score := overlap*0.75 + evidence[i].QualityScore*0.25
		if score > bestScore {
			bestScore = score
			best = &evidence[i]
		}
	}
	return best, bestScore
}

func runStatus(claims []models.VerificationClaim) string {
	if len(claims) == 0 {
		return StatusNeedsReview
	}
	hasUnsupported := false
	hasReview := false
	hasSourceSupported := false
	hasTestPassed := false
	for _, claim := range claims {
		if claim.Status == StatusUnsupported {
			hasUnsupported = true
		}
		if claim.Status == StatusSourceSupported {
			hasSourceSupported = true
		}
		if claim.Status == StatusTestPassed {
			hasTestPassed = true
		}
		if claim.NeedsReview || claim.Status == StatusNeedsReview || claim.Status == StatusConflicting || claim.Status == StatusUncertain {
			hasReview = true
		}
	}
	if hasReview {
		return StatusNeedsReview
	}
	if hasUnsupported {
		return StatusUnsupported
	}
	// Keep an explicit test result distinct from the broader "verified" status.
	// The claim may be accepted for task completion, but a passing test alone
	// does not establish general factual verification.
	if hasTestPassed {
		return StatusTestPassed
	}
	if hasSourceSupported {
		return StatusSourceSupported
	}
	return StatusVerified
}

type memoryPromotionOutcome struct {
	Stored         int
	Failed         int
	Skipped        int
	BlockedReason  string
	PromotedClaims []models.VerificationClaim
}

func (s *service) storeVerifiedMemory(request AnswerRequest, run *models.VerificationRun, claims []models.VerificationClaim, evidence []models.VerificationEvidence) memoryPromotionOutcome {
	outcome := memoryPromotionOutcome{Skipped: len(claims)}
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" {
		outcome.BlockedReason = "an authenticated owner identity is required"
		return outcome
	}
	if run == nil || run.ID == uuid.Nil || strings.TrimSpace(run.OwnerIdentity) != owner {
		outcome.BlockedReason = "verification run ownership or provenance is unavailable"
		return outcome
	}
	if s.memoryService == nil {
		outcome.BlockedReason = "durable-memory storage is unavailable"
		return outcome
	}
	outcome.Skipped = 0
	for _, claim := range claims {
		proof, eligible := memoryPromotionEvidence(request, run, claim, evidence)
		if !eligible {
			outcome.Skipped++
			continue
		}
		created, err := memory.CreateVerifiedFactForOwner(s.memoryService, owner, memory.CreateRequest{
			ProjectKey:  request.ProjectKey,
			Kind:        "source_supported_fact",
			Content:     claim.ClaimText,
			Summary:     compact(claim.ClaimText, 240),
			Tags:        []string{"verification", claim.Status},
			Confidence:  claim.Confidence,
			SourceURI:   firstNonEmpty(strings.TrimSpace(proof.SourceURI), strings.TrimSpace(proof.SourceID)),
			SourceLabel: "verification-run:" + run.ID.String(),
		}, memory.VerifiedFactProvenance{RunID: run.ID, ClaimID: claim.ID, EvidenceID: proof.ID, SourceURI: proof.SourceURI, SourceID: proof.SourceID})
		if err != nil || created == nil {
			outcome.Failed++
			continue
		}
		outcome.Stored++
		outcome.PromotedClaims = append(outcome.PromotedClaims, claim)
	}
	return outcome
}

func memoryPromotionEvidence(request AnswerRequest, run *models.VerificationRun, claim models.VerificationClaim, evidence []models.VerificationEvidence) (*models.VerificationEvidence, bool) {
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" || run == nil || run.ID == uuid.Nil || strings.TrimSpace(run.OwnerIdentity) != owner ||
		claim.RunID != run.ID || claim.NeedsReview || math.IsNaN(claim.Confidence) ||
		math.IsInf(claim.Confidence, 0) || claim.Confidence < minimumMemoryPromotionConfidence || claim.Confidence > 1 ||
		normalizeMode(request.Mode) == ModeAction {
		return nil, false
	}
	switch claim.Status {
	case StatusSourceSupported:
		if claim.HighRisk {
			return nil, false
		}
	default:
		return nil, false
	}
	claimSource := strings.TrimSpace(claim.SourceRefs)
	if claimSource == "" {
		return nil, false
	}
	for _, item := range evidence {
		if item.RunID != run.ID || item.Rejected || !item.Used || strings.TrimSpace(item.SourceType) == "" ||
			!isSourceSupportedEvidence(item.Authority) ||
			math.IsNaN(item.QualityScore) || math.IsInf(item.QualityScore, 0) ||
			item.QualityScore < minimumMemoryEvidenceQuality || item.QualityScore > 1 || strings.TrimSpace(item.Snippet) == "" {
			continue
		}
		if claimSource == strings.TrimSpace(item.SourceURI) || claimSource == strings.TrimSpace(item.SourceID) {
			if strings.TrimSpace(item.SourceURI) == "" && strings.TrimSpace(item.SourceID) == "" {
				continue
			}
			// Lexical overlap is not semantic entailment. Until HAI has a
			// provider-backed entailment check, durable facts must be extractive:
			// the entire claim must equal a complete sentence in accepted evidence.
			if !matchesEvidenceSentence(claim.ClaimText, item.Snippet) {
				continue
			}
			proof := item
			return &proof, true
		}
	}
	return nil, false
}

func matchesEvidenceSentence(claim, snippet string) bool {
	want := normalizedEvidenceSentence(claim)
	if want == "" || strings.TrimSpace(snippet) == "" {
		return false
	}

	start := 0
	for index, char := range snippet {
		boundary := char == '\n' || char == '\r' || char == ';' || char == '!' || char == '?'
		if char == '.' {
			next := index + 1
			boundary = next >= len(snippet) || unicode.IsSpace(rune(snippet[next]))
		}
		if !boundary {
			continue
		}
		if normalizedEvidenceSentence(snippet[start:index+1]) == want {
			return true
		}
		start = index + 1
	}
	return normalizedEvidenceSentence(snippet[start:]) == want
}

func normalizedEvidenceSentence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'“”‘’")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ".!?;:")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func (s *service) audit(runID uuid.UUID, action, message string) error {
	_, err := s.repo.CreateAuditLog(&models.VerificationAuditLog{
		RunID:   runID,
		Action:  action,
		Message: message,
	})
	return err
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeDraft, ModeGrounded, ModeStrict, ModeAction:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ModeGrounded
	}
}

func researchQuestions(question, projectKey string) []string {
	base := strings.TrimSpace(question)
	questions := []string{base}
	if projectKey != "" {
		questions = append(questions, projectKey+" "+base)
	}
	if needsExternal(question) {
		questions = append(questions, "current official source for "+base)
	}
	return uniqueStrings(questions)
}

func needsExternal(text string) bool {
	return containsAny(strings.ToLower(text), "latest", "current", "today", "public", "official", "legal", "government", "financial", "medical")
}

func evidenceQuality(input EvidenceInput, authority EvidenceAuthorityResolution, question string) float64 {
	score := 0.35
	if authority.Trusted && authority.Official {
		score += 0.25
	}
	if authority.Trusted && authority.Primary {
		score += 0.2
	}
	if input.SourceURI != "" {
		score += 0.1
	}
	if input.Generated {
		score -= 0.25
	}
	if overlapScore(tokenSet(question), tokenSet(input.Snippet)) > 0.2 {
		score += 0.1
	}
	if score > 1 {
		return 1
	}
	if score < 0 {
		return 0
	}
	return math.Round(score*100) / 100
}

func rejectReason(score float64, input EvidenceInput, authority EvidenceAuthorityResolution) string {
	if score >= 0.35 {
		return ""
	}
	if input.Generated {
		return "low-quality generated source"
	}
	if !authority.Trusted {
		return authority.Reason
	}
	return "source authority or relevance too weak"
}

func missingSources(request AnswerRequest, evidence []models.VerificationEvidence) string {
	if len(filterEvidence(evidence, true)) > 0 {
		return ""
	}
	if needsExternal(request.Question) {
		return "authoritative external source required but not available"
	}
	return "connected-source or provided evidence required but not available"
}

func unsupportedClaims(claims []models.VerificationClaim) []models.VerificationClaim {
	result := []models.VerificationClaim{}
	for _, claim := range claims {
		if claim.Status == StatusUnsupported || claim.Status == StatusUncertain || claim.Status == StatusNeedsReview || claim.Status == StatusConflicting {
			result = append(result, claim)
		}
	}
	return result
}

func filterEvidence(evidence []models.VerificationEvidence, used bool) []models.VerificationEvidence {
	result := []models.VerificationEvidence{}
	for _, item := range evidence {
		if item.Used == used && !item.Rejected {
			result = append(result, item)
		}
	}
	return result
}

func filterRejectedEvidence(evidence []models.VerificationEvidence) []models.VerificationEvidence {
	result := []models.VerificationEvidence{}
	for _, item := range evidence {
		if item.Rejected {
			result = append(result, item)
		}
	}
	return result
}

func sourceLabels(evidence []models.VerificationEvidence) string {
	values := []string{}
	for _, item := range evidence {
		values = append(values, firstNonEmpty(item.SourceLabel, item.SourceURI, item.SourceID, item.SourceType))
	}
	return joinValues(values)
}

func containsContradiction(claim string, evidence []models.VerificationEvidence) bool {
	claimPolarity := statementPolarity(claim)
	claimTokens := tokenSet(claim)
	for _, item := range evidence {
		if item.Rejected || !item.Used {
			continue
		}
		evidenceTokens := tokenSet(item.Snippet)
		if overlapScore(claimTokens, evidenceTokens) < minimumClaimEvidenceOverlap || overlapCount(claimTokens, evidenceTokens) == 0 {
			continue
		}
		if hasConflictingSingleWeekday(claim, item.Snippet) {
			return true
		}
		if evidencePolarity := statementPolarity(item.Snippet); claimPolarity != 0 && evidencePolarity != 0 && evidencePolarity != claimPolarity {
			return true
		}
	}
	return false
}

func hasConflictingSingleWeekday(claim, evidence string) bool {
	claimDay, claimHasSingleDay := singleWeekday(claim)
	evidenceDay, evidenceHasSingleDay := singleWeekday(evidence)
	return claimHasSingleDay && evidenceHasSingleDay && claimDay != evidenceDay
}

func singleWeekday(value string) (string, bool) {
	tokens := tokenSet(value)
	day := ""
	for _, candidate := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		if !tokens[candidate] {
			continue
		}
		if day != "" {
			return "", false
		}
		day = candidate
	}
	return day, day != ""
}

func statementPolarity(value string) int {
	replacer := strings.NewReplacer(
		",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ",
		"\n", " ", "\t", " ", "(", " ", ")", " ", "-", " ",
	)
	tokens := map[string]bool{}
	for _, token := range strings.Fields(strings.ToLower(replacer.Replace(value))) {
		tokens[token] = true
	}
	positive := 0
	negative := 0
	for _, token := range []string{"accept", "accepted", "approve", "approved", "confirm", "confirmed", "enable", "enabled", "yes"} {
		if tokens[token] {
			positive++
		}
	}
	for _, token := range []string{"deny", "denied", "disable", "disabled", "no", "not", "reject", "rejected"} {
		if tokens[token] {
			negative++
		}
	}
	if positive > 0 && negative == 0 {
		return 1
	}
	if negative > 0 && positive == 0 {
		return -1
	}
	return 0
}

func splitClaims(answer string) []string {
	raw := strings.NewReplacer("\n", ". ", ";", ".").Replace(answer)
	claims := []string{}
	for _, part := range strings.Split(raw, ".") {
		part = strings.TrimSpace(part)
		if part != "" {
			claims = append(claims, part)
		}
	}
	return claims
}

func highRisk(text string) bool {
	return containsAny(strings.ToLower(text), "email", "send", "delete", "financial", "legal", "government", "medical", "account", "public posting", "contract")
}

func deterministicCalculationCheck(claim string) (bool, bool) {
	value := strings.ReplaceAll(claim, " ", "")
	if !strings.Contains(value, "=") {
		return false, false
	}
	parts := strings.Split(value, "=")
	if len(parts) != 2 {
		return false, false
	}
	expected, err := strconv.ParseFloat(trimNumber(parts[1]), 64)
	if err != nil {
		return false, false
	}
	left := parts[0]
	operator := ""
	for _, candidate := range []string{"+", "-", "*", "/"} {
		if strings.Contains(left, candidate) {
			operator = candidate
			break
		}
	}
	if operator == "" {
		return false, false
	}
	numbers := strings.Split(left, operator)
	if len(numbers) != 2 {
		return false, false
	}
	a, errA := strconv.ParseFloat(trimNumber(numbers[0]), 64)
	b, errB := strconv.ParseFloat(trimNumber(numbers[1]), 64)
	if errA != nil || errB != nil {
		return false, false
	}
	result := 0.0
	switch operator {
	case "+":
		result = a + b
	case "-":
		result = a - b
	case "*":
		result = a * b
	case "/":
		if b == 0 {
			return true, false
		}
		result = a / b
	}
	return true, math.Abs(result-expected) < 0.0001
}

func trimNumber(value string) string {
	return strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ€$,%:;,.")
}

func freshnessLabel(value time.Time) string {
	days := time.Since(value).Hours() / 24
	if days <= 1 {
		return "fresh"
	}
	if days <= 30 {
		return "recent"
	}
	return "stale"
}

func isStaleEvidence(evidence *models.VerificationEvidence) bool {
	return evidence != nil && strings.EqualFold(strings.TrimSpace(evidence.Freshness), "stale")
}

var verificationStopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "that": {}, "this": {}, "with": {}, "from": {}, "are": {}, "was": {}, "were": {},
	"is": {}, "on": {}, "in": {}, "at": {}, "to": {}, "of": {}, "a": {}, "an": {}, "or": {}, "as": {}, "by": {}, "it": {},
	"be": {}, "been": {}, "has": {}, "have": {}, "had": {}, "will": {}, "would": {}, "can": {}, "could": {}, "should": {},
	"de": {}, "het": {}, "een": {}, "en": {}, "van": {}, "voor": {}, "met": {}, "dat": {}, "dit": {}, "die": {}, "op": {},
	"aan": {}, "zijn": {}, "waren": {}, "als": {}, "bij": {}, "naar": {}, "door": {}, "er": {},
}

func tokenSet(value string) map[string]bool {
	set := map[string]bool{}
	replacer := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ", "\n", " ", "\t", " ", "(", " ", ")", " ", "-", " ")
	for _, token := range strings.Fields(strings.ToLower(replacer.Replace(value))) {
		if len(token) >= 3 {
			if _, stopWord := verificationStopWords[token]; stopWord {
				continue
			}
			if _, err := strconv.ParseFloat(token, 64); err == nil {
				set[token] = true
				continue
			}
			set[token] = true
		}
	}
	return set
}

func overlapScore(left, right map[string]bool) float64 {
	if len(left) == 0 {
		return 0
	}
	matches := 0
	for token := range left {
		if right[token] {
			matches++
		}
	}
	return float64(matches) / float64(len(left))
}

func overlapCount(left, right map[string]bool) int {
	count := 0
	for token := range left {
		if right[token] {
			count++
		}
	}
	return count
}

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinValues(values []string) string {
	return strings.Join(uniqueStrings(values), ",")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

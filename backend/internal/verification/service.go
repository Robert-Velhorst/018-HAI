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

	"github.com/google/uuid"
)

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
	OwnerIdentity     string          `json:"-"`
	Question          string          `json:"question"`
	ProjectKey        string          `json:"projectKey,omitempty"`
	PursuitID         string          `json:"pursuitId,omitempty"`
	Mode              string          `json:"mode,omitempty"`
	DraftAnswer       string          `json:"draftAnswer,omitempty"`
	ExternalEvidence  []EvidenceInput `json:"externalEvidence,omitempty"`
	IncludeSensitive  bool            `json:"includeSensitive,omitempty"`
	HumanApproved     bool            `json:"humanApproved,omitempty"`
	AllowMemoryUpdate bool            `json:"allowMemoryUpdate,omitempty"`
}

type VerificationResult struct {
	Run               models.VerificationRun        `json:"run"`
	PursuitID         string                        `json:"pursuitId,omitempty"`
	PursuitLinked     bool                          `json:"pursuitLinked,omitempty"`
	PursuitLinkError  string                        `json:"pursuitLinkError,omitempty"`
	Claims            []models.VerificationClaim    `json:"claims"`
	Evidence          []models.VerificationEvidence `json:"evidence"`
	UnsupportedClaims []models.VerificationClaim    `json:"unsupportedClaims"`
	ResearchQuestions []string                      `json:"researchQuestions"`
	Logs              []string                      `json:"logs"`
	KnowledgeClaimIDs []string                      `json:"knowledgeClaimIds,omitempty"`
	KnowledgeError    string                        `json:"knowledgeProjectionError,omitempty"`
}

type Service interface {
	Answer(request AnswerRequest) (*VerificationResult, error)
	Runs() ([]models.VerificationRun, error)
	RunsForOwner(ownerIdentity string) ([]models.VerificationRun, error)
	RunDetails(id uuid.UUID) (*VerificationResult, error)
	RunDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error)
}

// ContextService binds an interactive verification pass to its caller without
// changing the legacy Service contract used by focused test doubles.
type ContextService interface {
	AnswerContext(context.Context, AnswerRequest) (*VerificationResult, error)
}

func AnswerWithContext(service Service, ctx context.Context, request AnswerRequest) (*VerificationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contextual, ok := service.(ContextService); ok {
		return contextual.AnswerContext(ctx, request)
	}
	return service.Answer(request)
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

type contextConnectedSourceSearcher interface {
	SearchContext(context.Context, source.SearchRequest) (*source.SearchResult, error)
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
	return s.AnswerContext(context.Background(), request)
}

func (s *service) AnswerContext(ctx context.Context, request AnswerRequest) (*VerificationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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

	evidence, err := s.collectEvidence(ctx, run.ID, request, questions, &logs)
	if err != nil {
		s.markCanceledRun(run, err)
		return nil, err
	}
	answer := buildAnswer(request, mode, evidence)
	claims := decomposeClaims(run.ID, answer, mode, request)
	verifiedClaims := verifyClaims(claims, evidence, request, mode)
	unsupported := unsupportedClaims(verifiedClaims)
	status := runStatus(verifiedClaims, mode, request)

	run.Answer = answer
	run.Status = status
	run.SourcesUsed = sourceLabels(filterEvidence(evidence, true))
	run.SourcesRejected = sourceLabels(filterRejectedEvidence(evidence))
	run.MissingSources = missingSources(request, evidence)
	if err := s.stopIfCanceled(ctx, run); err != nil {
		return nil, err
	}
	run, err = s.repo.UpdateRun(run)
	if err != nil {
		return nil, err
	}
	for _, item := range evidence {
		if err := s.stopIfCanceled(ctx, run); err != nil {
			return nil, err
		}
		_, _ = s.repo.CreateEvidence(&item)
	}
	for _, claim := range verifiedClaims {
		if err := s.stopIfCanceled(ctx, run); err != nil {
			return nil, err
		}
		_, _ = s.repo.CreateClaim(&claim)
	}
	knowledgeClaimIDs := []string(nil)
	knowledgeError := ""
	if s.claimProjector != nil {
		knowledgeClaimIDs, err = s.claimProjector.ProjectClaims(
			ctx, request, *run, verifiedClaims, evidence,
		)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			cancelErr := firstVerificationContextError(ctx, err)
			s.markCanceledRun(run, cancelErr)
			return nil, cancelErr
		}
		if err != nil {
			knowledgeError = "semantic claim projection failed"
			logs = append(logs, knowledgeError)
			s.audit(run.ID, "verification.knowledge_projection_failed", err.Error())
		} else if len(knowledgeClaimIDs) > 0 {
			logs = append(logs, "source-backed claims projected into immutable semantic knowledge")
			s.audit(run.ID, "verification.knowledge_projected", fmt.Sprintf("projected %d semantic claim(s)", len(knowledgeClaimIDs)))
		}
	}
	if err := s.stopIfCanceled(ctx, run); err != nil {
		return nil, err
	}
	s.audit(run.ID, "verification.completed", "important claims decomposed and verified before acceptance")
	if request.AllowMemoryUpdate {
		s.storeVerifiedMemory(request, run, verifiedClaims)
	}
	result := &VerificationResult{
		Run:               *run,
		Claims:            verifiedClaims,
		Evidence:          evidence,
		UnsupportedClaims: unsupported,
		ResearchQuestions: questions,
		Logs:              append(logs, "verification status logged for every important claim"),
		KnowledgeClaimIDs: knowledgeClaimIDs,
		KnowledgeError:    knowledgeError,
	}
	if pursuitID != uuid.Nil {
		if err := s.stopIfCanceled(ctx, run); err != nil {
			return nil, err
		}
		result.PursuitID = pursuitID.String()
		if err := s.pursuitLinker.LinkVerificationForOwner(request.OwnerIdentity, pursuitID, run.ID); err != nil {
			result.PursuitLinkError = err.Error()
			result.Logs = append(result.Logs, "verification was saved but could not be linked to the requested pursuit")
			s.audit(run.ID, "verification.pursuit_link_failed", err.Error())
		} else {
			result.PursuitLinked = true
			result.Logs = append(result.Logs, "verification linked to the requested pursuit")
			s.audit(run.ID, "verification.pursuit_linked", "verification run linked to requested pursuit "+pursuitID.String())
		}
	}
	return result, nil
}

func (s *service) stopIfCanceled(ctx context.Context, run *models.VerificationRun) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	s.markCanceledRun(run, ctx.Err())
	return ctx.Err()
}

func (s *service) markCanceledRun(run *models.VerificationRun, cause error) {
	if run == nil || (!errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded)) {
		return
	}
	run.Status = StatusNeedsReview
	run.MissingSources = "verification request stopped before completion"
	_, _ = s.repo.UpdateRun(run)
	s.audit(run.ID, "verification.stopped", "caller context ended before verification completed")
}

func firstVerificationContextError(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return fallback
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
	return s.repo.FindRunsForOwner(strings.TrimSpace(ownerIdentity))
}

func (s *service) RunDetails(id uuid.UUID) (*VerificationResult, error) {
	return s.runDetailsForOwner("", id)
}

func (s *service) RunDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error) {
	return s.runDetailsForOwner(strings.TrimSpace(ownerIdentity), id)
}

func (s *service) runDetailsForOwner(ownerIdentity string, id uuid.UUID) (*VerificationResult, error) {
	run, err := s.repo.FindRunForOwner(ownerIdentity, id)
	if err != nil {
		return nil, fmt.Errorf("verification run not found")
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

func (s *service) collectEvidence(ctx context.Context, runID uuid.UUID, request AnswerRequest, questions []string, logs *[]string) ([]models.VerificationEvidence, error) {
	evidence := []models.VerificationEvidence{}
	if s.sourceService != nil {
		for _, question := range questions {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			searchRequest := source.SearchRequest{
				OwnerIdentity:    request.OwnerIdentity,
				Query:            question,
				ProjectKey:       request.ProjectKey,
				Limit:            6,
				IncludeSensitive: request.IncludeSensitive,
			}
			var result *source.SearchResult
			var err error
			if contextual, ok := s.sourceService.(contextConnectedSourceSearcher); ok {
				result, err = contextual.SearchContext(ctx, searchRequest)
			} else {
				result, err = s.sourceService.Search(searchRequest)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil, firstVerificationContextError(ctx, err)
			}
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
					if s.sourceEvidence == nil {
						item.Authority = authorityConnectedUnverified
						item.Rejected = true
						item.RejectReason = "connected-source provenance resolver is unavailable"
					} else {
						snapshot, resolveErr := s.sourceEvidence.Resolve(
							ctx, request.OwnerIdentity, ranked.Extraction.ID.String(),
						)
						if errors.Is(resolveErr, context.Canceled) || errors.Is(resolveErr, context.DeadlineExceeded) || ctx.Err() != nil {
							return nil, firstVerificationContextError(ctx, resolveErr)
						}
						payloadDigest := sourceevidence.ExtractionPayloadDigest(ranked.Extraction)
						if resolveErr != nil || snapshot.SourceID != ranked.Extraction.SourceID.String() ||
							snapshot.RawItemID != ranked.Extraction.RawItemID.String() ||
							snapshot.ExtractionPayloadDigest != payloadDigest {
							item.Authority = authorityConnectedUnverified
							item.Rejected = true
							item.RejectReason = "connected-source extraction could not be matched to exact durable raw provenance"
						} else {
							item.Authority = authorityConnectedProvenance
							item.Freshness = freshnessLabel(snapshot.FetchedAt)
							item.Used = true
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	evidence = deduplicateEvidence(evidence)
	sort.SliceStable(evidence, func(i, j int) bool {
		return evidence[i].QualityScore > evidence[j].QualityScore
	})
	return evidence, nil
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
		if claim.HighRisk && !request.HumanApproved {
			claim.Status = StatusNeedsReview
			claim.NeedsReview = true
			claim.SupportExplanation = "high-risk output requires human approval"
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
		claim.SupportExplanation = "claim overlaps source evidence with authenticated provenance; semantic truth is not inferred"
		claim.Status = StatusSourceSupported
		if best.SourceType == "test_result" && containsAny(strings.ToLower(best.Snippet), "pass", "passed", "ok") {
			claim.Status = StatusTestPassed
		}
		if request.HumanApproved && claim.HighRisk {
			claim.Status = StatusHumanApproved
		}
		if containsContradiction(claim.ClaimText, evidence) {
			claim.Status = StatusConflicting
			claim.NeedsReview = true
			claim.SupportExplanation = "supporting sources appear to disagree"
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
		score := overlapScore(claimTokens, tokenSet(evidence[i].Snippet))
		score = score*0.75 + evidence[i].QualityScore*0.25
		if score > bestScore {
			bestScore = score
			best = &evidence[i]
		}
	}
	return best, bestScore
}

func runStatus(claims []models.VerificationClaim, mode string, request AnswerRequest) string {
	if len(claims) == 0 {
		return StatusNeedsReview
	}
	hasUnsupported := false
	hasReview := false
	hasSourceSupported := false
	for _, claim := range claims {
		if claim.Status == StatusUnsupported {
			hasUnsupported = true
		}
		if claim.Status == StatusSourceSupported {
			hasSourceSupported = true
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
	if hasSourceSupported {
		return StatusSourceSupported
	}
	if request.HumanApproved && mode == ModeAction {
		return StatusHumanApproved
	}
	return StatusVerified
}

func (s *service) storeVerifiedMemory(request AnswerRequest, run *models.VerificationRun, claims []models.VerificationClaim) {
	for _, claim := range claims {
		if claim.Status != StatusVerified && claim.Status != StatusSourceSupported && claim.Status != StatusHumanApproved {
			continue
		}
		_, _ = memory.CreateForOwner(s.memoryService, request.OwnerIdentity, memory.CreateRequest{
			ProjectKey:  request.ProjectKey,
			Kind:        "verified_fact",
			Content:     claim.ClaimText,
			Summary:     compact(claim.ClaimText, 240),
			Tags:        []string{"verified", claim.Status},
			Confidence:  claim.Confidence,
			SourceURI:   claim.SourceRefs,
			SourceLabel: "verification-run:" + run.ID.String(),
		})
	}
}

func (s *service) audit(runID uuid.UUID, action, message string) {
	_, _ = s.repo.CreateAuditLog(&models.VerificationAuditLog{
		RunID:   runID,
		Action:  action,
		Message: message,
	})
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
	if claimPolarity == 0 {
		return false
	}
	for _, item := range evidence {
		if item.Rejected || !item.Used {
			continue
		}
		if evidencePolarity := statementPolarity(item.Snippet); evidencePolarity != 0 && evidencePolarity != claimPolarity {
			return true
		}
	}
	return false
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

func tokenSet(value string) map[string]bool {
	set := map[string]bool{}
	replacer := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ", "\n", " ", "\t", " ", "(", " ", ")", " ", "-", " ")
	for _, token := range strings.Fields(strings.ToLower(replacer.Replace(value))) {
		if len(token) >= 3 {
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

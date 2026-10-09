package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	verifiedRunSourceSupported      = "source_supported"
	verifiedClaimSourceSupported    = "source_supported"
	verifiedMemoryOptInAction       = "verification.memory_update_requested"
	verifiedMemoryOptInMessage      = "authenticated owner explicitly opted in to source-supported memory promotion"
	minimumPersistedClaimConfidence = 0.70
	minimumPersistedEvidenceQuality = 0.55
	maximumPersistedEvidenceAge     = 30 * 24 * time.Hour
)

// VerifiedFactPromotionRepository validates durable verification records in
// the same transaction that persists a promoted fact. A plain memory write
// cannot establish evidence authority or freshness on its own.
type VerifiedFactPromotionRepository interface {
	WithVerifiedFactPromotion(
		ctx context.Context,
		ownerIdentity string,
		request CreateRequest,
		provenance VerifiedFactProvenance,
		promote func(Repository) (*models.ContextMemory, error),
	) (*models.ContextMemory, error)
}

func (r *GormRepository) WithVerifiedFactPromotion(
	ctx context.Context,
	ownerIdentity string,
	request CreateRequest,
	provenance VerifiedFactProvenance,
	promote func(Repository) (*models.ContextMemory, error),
) (*models.ContextMemory, error) {
	if r == nil || r.DB == nil || r.DB.Dialector == nil || promote == nil {
		return nil, errors.New("verified-fact promotion repository is unavailable")
	}
	if r.DB.Dialector.Name() != "postgres" {
		return nil, errors.New("verified-fact promotion requires PostgreSQL provenance validation")
	}
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if err := validateVerifiedFactRequest(request, provenance); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("verified-fact promotion context is required")
	}

	var saved *models.ContextMemory
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockKey := memoryCreateLockKey(ownerIdentity, strings.TrimSpace(request.ProjectKey), "source_supported_fact")
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("lock verified-fact promotion: %w", err)
		}
		if err := validatePersistedVerifiedFactEvidence(tx, ownerIdentity, request, provenance); err != nil {
			return err
		}
		var err error
		saved, err = promote(&GormRepository{DB: tx})
		return err
	})
	if err != nil {
		return saved, err
	}
	return saved, nil
}

func validatePersistedVerifiedFactEvidence(tx *gorm.DB, ownerIdentity string, request CreateRequest, provenance VerifiedFactProvenance) error {
	var run models.VerificationRun
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND owner_identity = ?", provenance.RunID, ownerIdentity).
		First(&run).Error; err != nil {
		return fmt.Errorf("verified-fact owner-bound run is unavailable: %w", err)
	}
	now := time.Now().UTC()
	if run.ID == uuid.Nil || run.OwnerIdentity != ownerIdentity || run.Status != verifiedRunSourceSupported ||
		(run.Mode != "grounded" && run.Mode != "strict") || !isRecentPersistedTimestamp(run.CreatedAt, now) || !isRecentPersistedTimestamp(run.UpdatedAt, now) ||
		run.UpdatedAt.Before(run.CreatedAt) {
		return errors.New("verified-fact run is incomplete, stale, unsafe, or not source-supported")
	}

	var optIn models.VerificationAuditLog
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("run_id = ? AND action = ? AND message = ?", run.ID, verifiedMemoryOptInAction, verifiedMemoryOptInMessage).
		Order("created_at desc").First(&optIn).Error; err != nil {
		return fmt.Errorf("explicit owner memory opt-in is unavailable: %w", err)
	}
	if optIn.CreatedAt.IsZero() || optIn.CreatedAt.Before(run.UpdatedAt) || optIn.CreatedAt.After(now.Add(5*time.Minute)) {
		return errors.New("explicit owner memory opt-in is stale or inconsistent with the verification run")
	}

	var claim models.VerificationClaim
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND run_id = ?", provenance.ClaimID, run.ID).
		First(&claim).Error; err != nil {
		return fmt.Errorf("verified-fact claim is unavailable: %w", err)
	}
	if claim.ID == uuid.Nil || claim.RunID != run.ID || claim.Status != verifiedClaimSourceSupported || claim.NeedsReview || claim.HighRisk ||
		math.IsNaN(claim.Confidence) || math.IsInf(claim.Confidence, 0) || claim.Confidence < minimumPersistedClaimConfidence || claim.Confidence > 1 ||
		math.Abs(claim.Confidence-request.Confidence) > 0.001 || normalizeEvidenceText(claim.ClaimText) != normalizeEvidenceText(request.Content) {
		return errors.New("verified-fact claim is missing, contradictory, high-risk, unreviewed, or inconsistent with the requested memory")
	}
	claimSource := strings.TrimSpace(claim.SourceRefs)
	if claimSource == "" {
		return errors.New("verified-fact claim has no resolvable source reference")
	}

	var reviewCount int64
	if err := tx.Model(&models.VerificationClaim{}).
		Where("run_id = ? AND (needs_review = ? OR status IN ?)", run.ID, true, []string{"needs_review", "conflicting", "uncertain", "unsupported"}).
		Count(&reviewCount).Error; err != nil {
		return fmt.Errorf("check verification run for contradictory or unresolved claims: %w", err)
	}
	if reviewCount != 0 {
		return errors.New("verification run contains contradictory or unresolved claims")
	}

	var evidence models.VerificationEvidence
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND run_id = ?", provenance.EvidenceID, run.ID).
		First(&evidence).Error; err != nil {
		return fmt.Errorf("verified-fact evidence is unavailable: %w", err)
	}
	if evidence.ID == uuid.Nil || evidence.RunID != run.ID || !evidence.Used || evidence.Rejected ||
		strings.TrimSpace(evidence.SourceType) == "" || !isPersistedTrustedAuthority(evidence.Authority) ||
		!isPersistedFreshness(evidence.Freshness) || math.IsNaN(evidence.QualityScore) || math.IsInf(evidence.QualityScore, 0) ||
		evidence.QualityScore < minimumPersistedEvidenceQuality || evidence.QualityScore > 1 || strings.TrimSpace(evidence.Snippet) == "" ||
		!isRecentPersistedTimestamp(evidence.CreatedAt, now) || evidence.CreatedAt.Before(run.CreatedAt) ||
		!isRecentPersistedTimestamp(evidence.UpdatedAt, now) || evidence.UpdatedAt.Before(evidence.CreatedAt) {
		return errors.New("verified-fact evidence is missing, stale, rejected, untrusted, or incomplete")
	}

	evidenceURI := strings.TrimSpace(evidence.SourceURI)
	evidenceID := strings.TrimSpace(evidence.SourceID)
	if (evidenceURI == "" && evidenceID == "") || (claimSource != evidenceURI && claimSource != evidenceID) {
		return errors.New("verified-fact claim source does not resolve to its evidence")
	}
	proofSource := strings.TrimSpace(provenance.SourceURI)
	if proofSource == "" {
		proofSource = strings.TrimSpace(provenance.SourceID)
	}
	if proofSource == "" || proofSource != strings.TrimSpace(request.SourceURI) ||
		(request.SourceURI != evidenceURI && request.SourceURI != evidenceID) {
		return errors.New("verified-fact memory source does not match its persisted evidence")
	}
	if !containsExactEvidenceSentence(request.Content, evidence.Snippet) {
		return errors.New("verified-fact memory is not an exact sentence in its persisted evidence")
	}
	return nil
}

func isPersistedTrustedAuthority(authority string) bool {
	authority = strings.ToLower(strings.TrimSpace(authority))
	return authority == "connected_source:provenance_verified" ||
		strings.HasPrefix(authority, "trusted_external:") && strings.TrimSpace(strings.TrimPrefix(authority, "trusted_external:")) != ""
}

func isPersistedFreshness(freshness string) bool {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "fresh", "recent":
		return true
	default:
		return false
	}
}

func isRecentPersistedTimestamp(value, now time.Time) bool {
	return !value.IsZero() && !value.Before(now.Add(-maximumPersistedEvidenceAge)) && !value.After(now.Add(5*time.Minute))
}

func containsExactEvidenceSentence(claim, snippet string) bool {
	want := normalizeEvidenceText(claim)
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
		if normalizeEvidenceText(snippet[start:index+1]) == want {
			return true
		}
		start = index + 1
	}
	return normalizeEvidenceText(snippet[start:]) == want
}

func normalizeEvidenceText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'“”‘’")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ".!?;:")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

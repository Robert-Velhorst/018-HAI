//go:build integration

package memory

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type persistedPromotionProof struct {
	owner      string
	request    CreateRequest
	provenance VerifiedFactProvenance
	createdAt  time.Time
}

func seedPersistedPromotionProof(t *testing.T, db *gorm.DB, owner string) persistedPromotionProof {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	runID, claimID, evidenceID := uuid.New(), uuid.New(), uuid.New()
	sourceURI := "https://records.example/case/" + uuid.NewString()
	claimText := "The review is scheduled for Friday."
	run := models.VerificationRun{
		ID: runID, OwnerIdentity: owner, Mode: "grounded", Question: "When is the review?",
		Answer: claimText, Status: verifiedRunSourceSupported, CreatedAt: now, UpdatedAt: now,
	}
	claim := models.VerificationClaim{
		ID: claimID, RunID: runID, ClaimText: claimText, Status: verifiedClaimSourceSupported,
		SourceRefs: sourceURI, Confidence: 0.92, CreatedAt: now, UpdatedAt: now,
	}
	evidence := models.VerificationEvidence{
		ID: evidenceID, RunID: runID, SourceType: "connected_source", SourceURI: sourceURI,
		SourceLabel: "Review record", Snippet: "Meeting notes. " + claimText + " Next steps follow.",
		Authority: "connected_source:provenance_verified", Freshness: "fresh", QualityScore: 0.95,
		Used: true, CreatedAt: now, UpdatedAt: now,
	}
	optIn := models.VerificationAuditLog{
		ID: uuid.New(), RunID: runID, Action: verifiedMemoryOptInAction,
		Message: verifiedMemoryOptInMessage, CreatedAt: now.Add(time.Second),
	}
	for _, value := range []any{&run, &claim, &evidence, &optIn} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed persisted promotion proof: %v", err)
		}
	}
	request := CreateRequest{
		ProjectKey: "case-a", Kind: "source_supported_fact", Content: claimText, Confidence: claim.Confidence,
		SourceURI: sourceURI, SourceLabel: "verification-run:" + runID.String(),
	}
	return persistedPromotionProof{
		owner: owner, request: request,
		provenance: VerifiedFactProvenance{RunID: runID, ClaimID: claimID, EvidenceID: evidenceID, SourceURI: sourceURI},
		createdAt:  now,
	}
}

func invokePersistedPromotion(db *gorm.DB, owner string, proof persistedPromotionProof, promote func(Repository) (*models.ContextMemory, error)) (*models.ContextMemory, error) {
	return (&GormRepository{DB: db}).WithVerifiedFactPromotion(context.Background(), owner, proof.request, proof.provenance, promote)
}

func TestPostgresVerifiedFactPromotionRequiresCurrentOwnerOptInAndConsistentEvidence(t *testing.T) {
	tests := []struct {
		name      string
		seedOwner string
		promoteAs string
		mutate    func(*testing.T, *gorm.DB, persistedPromotionProof)
	}{
		{
			name: "run owned by another user", seedOwner: "owner-b", promoteAs: "owner-a",
		},
		{
			name: "missing explicit owner opt-in", seedOwner: "owner-a", promoteAs: "owner-a",
			mutate: func(t *testing.T, db *gorm.DB, proof persistedPromotionProof) {
				t.Helper()
				if err := db.Where("run_id = ? AND action = ?", proof.provenance.RunID, verifiedMemoryOptInAction).
					Delete(&models.VerificationAuditLog{}).Error; err != nil {
					t.Fatalf("remove opt-in fixture: %v", err)
				}
			},
		},
		{
			name: "stale freshness label", seedOwner: "owner-a", promoteAs: "owner-a",
			mutate: func(t *testing.T, db *gorm.DB, proof persistedPromotionProof) {
				t.Helper()
				if err := db.Model(&models.VerificationEvidence{}).Where("id = ?", proof.provenance.EvidenceID).
					Update("freshness", "stale").Error; err != nil {
					t.Fatalf("stale evidence fixture: %v", err)
				}
			},
		},
		{
			name: "old evidence despite fresh label", seedOwner: "owner-a", promoteAs: "owner-a",
			mutate: func(t *testing.T, db *gorm.DB, proof persistedPromotionProof) {
				t.Helper()
				staleAt := proof.createdAt.Add(-maximumPersistedEvidenceAge - time.Second)
				if err := db.Model(&models.VerificationEvidence{}).Where("id = ?", proof.provenance.EvidenceID).
					Updates(map[string]any{"created_at": staleAt, "updated_at": staleAt}).Error; err != nil {
					t.Fatalf("old evidence fixture: %v", err)
				}
			},
		},
		{
			name: "contradictory unresolved claim", seedOwner: "owner-a", promoteAs: "owner-a",
			mutate: func(t *testing.T, db *gorm.DB, proof persistedPromotionProof) {
				t.Helper()
				claim := models.VerificationClaim{
					ID: uuid.New(), RunID: proof.provenance.RunID, ClaimText: "The review is not scheduled for Friday.",
					Status: "conflicting", NeedsReview: true, Confidence: 0.8,
					CreatedAt: proof.createdAt, UpdatedAt: proof.createdAt,
				}
				if err := db.Create(&claim).Error; err != nil {
					t.Fatalf("seed contradictory claim: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := openSourceLessonPostgresFixture(t)
			proof := seedPersistedPromotionProof(t, f.db, test.seedOwner)
			if test.mutate != nil {
				test.mutate(t, f.db, proof)
			}
			promoteCalls := 0
			_, err := invokePersistedPromotion(f.db, test.promoteAs, proof, func(repository Repository) (*models.ContextMemory, error) {
				promoteCalls++
				return repository.Create(&models.ContextMemory{OwnerIdentity: test.promoteAs, Kind: "source_supported_fact", Content: proof.request.Content})
			})
			if err == nil {
				t.Fatal("promotion accepted missing, stale, unowned, or contradictory evidence")
			}
			if promoteCalls != 0 {
				t.Fatalf("promotion callback ran %d times despite failed proof", promoteCalls)
			}
			var count int64
			if err := f.db.Model(&models.ContextMemory{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rejected promotion persisted records: count=%d err=%v", count, err)
			}
		})
	}
}

func TestPostgresVerifiedFactPromotionPersistsOnlyAfterCompleteProof(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	proof := seedPersistedPromotionProof(t, f.db, "owner-a")
	promoteCalls := 0
	saved, err := invokePersistedPromotion(f.db, "owner-a", proof, func(repository Repository) (*models.ContextMemory, error) {
		promoteCalls++
		return repository.Create(&models.ContextMemory{
			OwnerIdentity: "owner-a", ProjectKey: proof.request.ProjectKey, Kind: proof.request.Kind,
			Content: proof.request.Content, SourceURI: proof.request.SourceURI, SourceLabel: proof.request.SourceLabel,
			Confidence: proof.request.Confidence,
		})
	})
	if err != nil || saved == nil {
		t.Fatalf("valid persisted promotion: saved=%#v err=%v", saved, err)
	}
	if promoteCalls != 1 || saved.OwnerIdentity != "owner-a" || saved.SourceURI != proof.request.SourceURI {
		t.Fatalf("persisted promotion callback/provenance mismatch: calls=%d saved=%#v", promoteCalls, saved)
	}
}

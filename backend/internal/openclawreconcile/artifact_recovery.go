package openclawreconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	artifactCollectionLease       = 3 * time.Minute
	artifactCollectionMax         = 8
	artifactCollectionBatch       = 10
	artifactCollectionReadTimeout = 2 * time.Minute
)

type ArtifactClaim struct {
	Receipt agentruntime.OpenClawGatewayReceipt
	Attempt int
	Token   uuid.UUID
}
type artifactRecoveryLedger interface {
	ClaimOpenClawArtifacts(context.Context, time.Time) (*ArtifactClaim, error)
	CompleteOpenClawArtifacts(context.Context, ArtifactClaim, []agentruntime.GatewayArtifactDescriptor, uuid.UUID) (bool, error)
	FailOpenClawArtifacts(context.Context, ArtifactClaim) (bool, error)
}
type artifactRecoveryReader interface {
	OpenClawArtifactRecoveryReady() bool
	ReadOpenClawGatewayArtifactDescriptors(context.Context, string, string) ([]agentruntime.GatewayArtifactDescriptor, error)
}

func (r *Repository) ClaimOpenClawArtifacts(ctx context.Context, now time.Time) (*ArtifactClaim, error) {
	if r == nil || r.db == nil || now.IsZero() {
		return nil, fmt.Errorf("OpenClaw artifact claim unavailable")
	}
	var claim *ArtifactClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			models.OpenClawGatewaySessionReceipt
			CollectionAttempts int
		}
		leaseCutoff := now.UTC().Add(-artifactCollectionLease)
		err := tx.Table("openclaw_gateway_session_receipts AS receipt").
			Select("receipt.*, COALESCE(collection.attempts, 0) AS collection_attempts").
			Joins("LEFT JOIN openclaw_gateway_artifact_collections AS collection ON collection.execution_reference = receipt.execution_reference").
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "receipt"}, Options: "SKIP LOCKED"}).
			Where("receipt.status = 'terminal' AND receipt.terminal_status = 'completed' AND receipt.terminal_at IS NOT NULL AND collection.captured_at IS NULL AND COALESCE(collection.attempts, 0) < ? AND ((collection.claimed_at IS NULL AND (collection.next_attempt_at IS NULL OR collection.next_attempt_at <= ?)) OR collection.claimed_at <= ?)", artifactCollectionMax, now.UTC(), leaseCutoff).
			Order("collection.next_attempt_at ASC NULLS FIRST, receipt.created_at ASC").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		attempt := row.CollectionAttempts + 1
		delay := 15 * time.Minute * time.Duration(1<<row.CollectionAttempts)
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		token := uuid.New()
		// The joined collection may have changed since this statement's snapshot.
		// Guard the upsert as well as the parent lock before issuing a fencing token.
		result := tx.Exec(`INSERT INTO openclaw_gateway_artifact_collections (execution_reference, attempts, next_attempt_at, claimed_at, claim_token)
   VALUES (?, ?, ?, ?, ?) ON CONFLICT (execution_reference) DO UPDATE SET attempts=EXCLUDED.attempts, next_attempt_at=EXCLUDED.next_attempt_at, claimed_at=EXCLUDED.claimed_at, claim_token=EXCLUDED.claim_token
   WHERE openclaw_gateway_artifact_collections.attempts = ? AND openclaw_gateway_artifact_collections.captured_at IS NULL
     AND ((openclaw_gateway_artifact_collections.claimed_at IS NULL AND (openclaw_gateway_artifact_collections.next_attempt_at IS NULL OR openclaw_gateway_artifact_collections.next_attempt_at <= ?)) OR openclaw_gateway_artifact_collections.claimed_at <= ?)`, row.ExecutionReference, attempt, now.UTC().Add(delay), now.UTC(), token, row.CollectionAttempts, now.UTC(), leaseCutoff)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		claim = &ArtifactClaim{Receipt: receiptFromModel(row.OpenClawGatewaySessionReceipt), Attempt: attempt, Token: token}
		return nil
	})
	return claim, err
}

// Parent-row locking uses the same order as Claim. Descriptors and the observed
// audit event are atomic with lease completion; stale claims cannot write either.
func (r *Repository) CompleteOpenClawArtifacts(ctx context.Context, claim ArtifactClaim, descriptors []agentruntime.GatewayArtifactDescriptor, automationID uuid.UUID) (bool, error) {
	if r == nil || r.db == nil || automationID == uuid.Nil || claim.Attempt < 1 || claim.Attempt > artifactCollectionMax || claim.Token == uuid.Nil || descriptors == nil || len(descriptors) > 20 {
		return false, fmt.Errorf("invalid OpenClaw artifact collection")
	}
	seen := map[string]bool{}
	for _, d := range descriptors {
		digest := strings.ToLower(strings.TrimSpace(d.Digest))
		if !validArtifactDescriptor(d) || seen[digest] {
			return false, fmt.Errorf("invalid OpenClaw artifact descriptor")
		}
		seen[digest] = true
	}
	stored := false
	now := time.Now().UTC()
	receipt := claim.Receipt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.OpenClawGatewaySessionReceipt
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND run_id = ? AND session_key = ? AND status = 'terminal' AND terminal_status = 'completed' AND terminal_at = ?", receipt.ExecutionReference, receipt.OwnerIdentity, receipt.RuntimeTaskID, receipt.RunID, receipt.SessionKey, receipt.TerminalAt).Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var bound int64
		if err := tx.Model(&models.AutomationLaunchEvent{}).Where("automation_id = ? AND execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND runtime_type = 'openclaw'", automationID, receipt.ExecutionReference, receipt.OwnerIdentity, receipt.RuntimeTaskID).Count(&bound).Error; err != nil {
			return err
		}
		if bound == 0 {
			return fmt.Errorf("OpenClaw artifact collection has no matching automation")
		}
		leaseCutoff := now.Add(-artifactCollectionLease)
		result := tx.Table("openclaw_gateway_artifact_collections").Where("execution_reference = ? AND attempts = ? AND claim_token = ? AND claimed_at > ? AND captured_at IS NULL", receipt.ExecutionReference, claim.Attempt, claim.Token, leaseCutoff).Updates(map[string]any{"captured_at": now, "next_attempt_at": nil, "claimed_at": nil, "claim_token": nil, "descriptor_count": len(descriptors)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if len(descriptors) > 0 {
			if err := NewRepository(tx).CreateOpenClawGatewayArtifactDescriptors(ctx, receipt.ExecutionReference, descriptors); err != nil {
				return err
			}
		}
		event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: receipt.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference,
			LaunchType: "agent_runtime_openclaw_artifacts", EventKey: "openclaw-gateway-artifacts:" + receipt.ExecutionReference, Target: "gateway://openclaw/artifact-metadata", Status: "observed",
			Message: fmt.Sprintf("OpenClaw artifact metadata collected: %d descriptor(s)", len(descriptors)), AuditEvents: []string{"Bounded native artifact metadata only; no content download, task re-execution or deliverable verification"}, StartedAt: now, CompletedAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		stored = true
		return nil
	})
	return stored && err == nil, err
}

// Fail releases only the still-current claim. The scheduled retry time remains
// durable, and a late response carrying the released token cannot complete it.
func (r *Repository) FailOpenClawArtifacts(ctx context.Context, claim ArtifactClaim) (bool, error) {
	if r == nil || r.db == nil || claim.Attempt < 1 || claim.Attempt > artifactCollectionMax || claim.Token == uuid.Nil {
		return false, fmt.Errorf("invalid OpenClaw artifact claim")
	}
	result := r.db.WithContext(ctx).Table("openclaw_gateway_artifact_collections").
		Where("execution_reference = ? AND attempts = ? AND claim_token = ? AND captured_at IS NULL", claim.Receipt.ExecutionReference, claim.Attempt, claim.Token).
		Updates(map[string]any{"claimed_at": nil, "claim_token": nil})
	return result.RowsAffected == 1, result.Error
}

func (s *Service) recoverArtifacts(limit int) error {
	ledger, ok := s.receipts.(artifactRecoveryLedger)
	if !ok {
		return nil
	}
	reader, ok := s.gateway.(artifactRecoveryReader)
	if !ok || !reader.OpenClawArtifactRecoveryReady() {
		return nil
	}
	if limit < 1 || limit > artifactCollectionBatch {
		limit = artifactCollectionBatch
	}
	ctx, cancel := context.WithTimeout(context.Background(), artifactCollectionReadTimeout)
	defer cancel()
	var failures []error
	for i := 0; i < limit; i++ {
		if !reader.OpenClawArtifactRecoveryReady() {
			break
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		claim, err := ledger.ClaimOpenClawArtifacts(ctx, s.now().UTC())
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if claim == nil {
			break
		}
		fail := func(cause error) {
			if _, releaseErr := ledger.FailOpenClawArtifacts(ctx, *claim); releaseErr != nil {
				failures = append(failures, fmt.Errorf("release OpenClaw artifact collection claim: %w", releaseErr))
			}
			failures = append(failures, cause)
		}
		automation, err := s.boundAutomation(claim.Receipt)
		if err != nil {
			fail(err)
			continue
		}
		descriptors, err := reader.ReadOpenClawGatewayArtifactDescriptors(ctx, claim.Receipt.RuntimeTaskID, claim.Receipt.ExecutionReference)
		if err != nil {
			fail(fmt.Errorf("OpenClaw artifact recovery attempt %d unavailable", claim.Attempt))
			continue
		}
		if _, err := ledger.CompleteOpenClawArtifacts(ctx, *claim, descriptors, automation.ID); err != nil {
			fail(err)
		}
	}
	return errors.Join(failures...)
}

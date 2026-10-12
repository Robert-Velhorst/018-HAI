package openclawreconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UsageClaim struct {
	Receipt agentruntime.OpenClawGatewayReceipt
	Attempt int
}

type usageRecoveryLedger interface {
	ClaimOpenClawUsage(context.Context, time.Time) (*UsageClaim, error)
	CompleteOpenClawUsage(context.Context, UsageClaim, *agentruntime.GatewaySessionUsageSnapshot, uuid.UUID) (bool, error)
}

type usageRecoveryReader interface {
	OpenClawUsageRecoveryReady() bool
	ReadOpenClawGatewayUsageSnapshot(context.Context, string, string) (*agentruntime.GatewaySessionUsageSnapshot, error)
}

func (r *Repository) ClaimOpenClawUsage(ctx context.Context, now time.Time) (*UsageClaim, error) {
	if r == nil || r.db == nil || now.IsZero() {
		return nil, fmt.Errorf("OpenClaw usage claim unavailable")
	}
	var claim *UsageClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.OpenClawGatewaySessionReceipt
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = 'terminal' AND terminal_status IN ('completed', 'failed') AND terminal_at IS NOT NULL AND session_id <> '' AND usage_snapshot_json IS NULL AND usage_attempts < 8 AND (usage_next_attempt_at IS NULL OR usage_next_attempt_at <= ?)", now.UTC()).
			Order("usage_next_attempt_at ASC NULLS FIRST, created_at ASC").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		attempt := row.UsageAttempts + 1
		delay := 15 * time.Minute * time.Duration(1<<row.UsageAttempts)
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		if err := tx.Model(&row).Updates(map[string]any{"usage_attempts": attempt, "usage_next_attempt_at": now.UTC().Add(delay)}).Error; err != nil {
			return err
		}
		claim = &UsageClaim{Receipt: receiptFromModel(row), Attempt: attempt}
		return nil
	})
	return claim, err
}

// The summary and its single audit event commit together; a stale worker cannot
// overwrite a newer lease or add a second usage event after successful capture.
func (r *Repository) CompleteOpenClawUsage(ctx context.Context, claim UsageClaim, snapshot *agentruntime.GatewaySessionUsageSnapshot, automationID uuid.UUID) (bool, error) {
	if r == nil || r.db == nil || automationID == uuid.Nil || claim.Attempt < 1 || claim.Attempt > 8 {
		return false, fmt.Errorf("invalid OpenClaw usage capture")
	}
	now := time.Now().UTC()
	if err := snapshot.ValidateFor(claim.Receipt, claim.Receipt.TerminalAt, now); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || len(encoded) > 65536 {
		return false, fmt.Errorf("invalid OpenClaw usage snapshot encoding")
	}
	summary := snapshot.AuditSummary()
	stored := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := claim.Receipt
		result := tx.Model(&models.OpenClawGatewaySessionReceipt{}).
			Where("execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND session_id = ? AND status = 'terminal' AND usage_attempts = ? AND usage_snapshot_json IS NULL", r.ExecutionReference, r.OwnerIdentity, r.RuntimeTaskID, r.SessionID, claim.Attempt).
			Updates(map[string]any{"usage_summary": summary, "usage_snapshot_json": string(encoded), "usage_captured_at": now, "usage_next_attempt_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		event := &models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automationID, OwnerIdentity: r.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: r.RuntimeTaskID, ExecutionReference: r.ExecutionReference,
			LaunchType: "agent_runtime_openclaw_usage", EventKey: "openclaw-gateway-usage-v2:" + r.ExecutionReference, Target: "gateway://openclaw/session-usage", Status: "observed",
			Message: "OpenClaw session usage recorded; not a budget debit", AuditEvents: []string{summary}, StartedAt: now, CompletedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(event).Error; err != nil {
			return err
		}
		stored = true
		return nil
	})
	return stored, err
}

func (s *Service) recoverUsage(limit int) error {
	ledger, ok := s.receipts.(usageRecoveryLedger)
	if !ok {
		return nil
	}
	reader, ok := s.gateway.(usageRecoveryReader)
	if !ok || !reader.OpenClawUsageRecoveryReady() {
		return nil
	}
	if limit < 1 || limit > 10 {
		limit = 10
	}
	var failures []error
	for i := 0; i < limit; i++ {
		claim, err := ledger.ClaimOpenClawUsage(context.Background(), s.now().UTC())
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if claim == nil {
			break
		}
		automation, err := s.boundAutomation(claim.Receipt)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		snapshot, err := reader.ReadOpenClawGatewayUsageSnapshot(context.Background(), claim.Receipt.RuntimeTaskID, claim.Receipt.ExecutionReference)
		if err != nil {
			failures = append(failures, fmt.Errorf("OpenClaw usage recovery attempt %d unavailable for %s", claim.Attempt, claim.Receipt.ExecutionReference))
			continue
		}
		if _, err := ledger.CompleteOpenClawUsage(context.Background(), *claim, snapshot, automation.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

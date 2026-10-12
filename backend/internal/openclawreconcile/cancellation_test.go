package openclawreconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func TestEmergencyStopCancellationPersistsIntentAndDoesNotFinalizeReceipt(t *testing.T) {
	withPersistedEmergencyStop(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	receipt := cancellationTestReceipt("admitted", now)
	repository := &cancellationTestLedger{rows: []agentruntime.OpenClawGatewayReceipt{receipt}}
	gateway := &cancellationTestGateway{stopStatuses: []string{"cancellation_requested"}}
	service := NewService(repository, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	targeted, acknowledged, awaiting, failed, err := service.FanOutEmergencyStop(context.Background())
	if err != nil || targeted != 1 || acknowledged != 1 || awaiting != 0 || failed != 0 {
		t.Fatalf("FanOutEmergencyStop = (%d,%d,%d,%d), %v", targeted, acknowledged, awaiting, failed, err)
	}
	stored := repository.rows[0]
	if stored.CancellationIntentID == "" || stored.CancellationStatus != "acknowledged" || stored.CancellationAttempts != 1 {
		t.Fatalf("cancellation intent/outcome was not persisted: %#v", stored)
	}
	if stored.Status != "admitted" || !stored.TerminalAt.IsZero() || stored.TerminalStatus != "" {
		t.Fatalf("sessions.abort acknowledgment was treated as terminal: %#v", stored)
	}
	if len(gateway.stopped) != 1 || gateway.stopped[0].OwnerIdentity != receipt.OwnerIdentity || gateway.stopped[0].RuntimeTaskID != receipt.RuntimeTaskID ||
		gateway.stopped[0].ExecutionReference != receipt.ExecutionReference || gateway.stopped[0].SessionKey != receipt.SessionKey || gateway.stopped[0].RunID != receipt.RunID {
		t.Fatalf("cancellation did not receive the complete durable binding: %#v", gateway.stopped)
	}
	if _, _, _, _, err := service.FanOutEmergencyStop(context.Background()); err != nil || len(gateway.stopped) != 1 {
		t.Fatalf("acknowledged intent was not idempotent: stops=%d err=%v", len(gateway.stopped), err)
	}
}

func TestEmergencyStopAdmittingReceiptWaitsForExactIdentityThenRetries(t *testing.T) {
	withPersistedEmergencyStop(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	receipt := cancellationTestReceipt("admitting", now)
	receipt.SessionKey = ""
	receipt.SessionID = ""
	receipt.RunID = ""
	repository := &cancellationTestLedger{rows: []agentruntime.OpenClawGatewayReceipt{receipt}}
	gateway := &cancellationTestGateway{stopStatuses: []string{"cancellation_requested"}}
	service := NewService(repository, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	targeted, acknowledged, awaiting, failed, err := service.FanOutEmergencyStop(context.Background())
	if err != nil || targeted != 1 || acknowledged != 0 || awaiting != 1 || failed != 0 || len(gateway.stopped) != 0 {
		t.Fatalf("admitting fan-out = (%d,%d,%d,%d), %v stops=%d", targeted, acknowledged, awaiting, failed, err, len(gateway.stopped))
	}
	if repository.rows[0].CancellationStatus != "awaiting_identity" || repository.rows[0].CancellationIntentID == "" {
		t.Fatalf("admitting receipt did not retain durable pending intent: %#v", repository.rows[0])
	}

	receipt.SessionKey = "agent:main:bound-session"
	receipt.SessionID = "session-instance-1"
	receipt.RunID = "exact-run-55"
	receipt.Status = "admitted"
	if admitted, err := repository.MarkOpenClawGatewayReceiptAdmitted(context.Background(), receipt); err != nil || !admitted {
		t.Fatalf("persist exact run identity: admitted=%v err=%v", admitted, err)
	}
	if err := service.retryPendingCancellations(10); err != nil {
		t.Fatalf("retry exact cancellation after identity persisted: %v", err)
	}
	stored := repository.rows[0]
	if len(gateway.stopped) != 1 || gateway.stopped[0].SessionKey != "agent:main:bound-session" || gateway.stopped[0].RunID != "exact-run-55" {
		t.Fatalf("retry did not use exact persisted run identity: %#v", gateway.stopped)
	}
	if stored.CancellationStatus != "acknowledged" || stored.Status != "admitted" || !stored.TerminalAt.IsZero() {
		t.Fatalf("post-race acknowledgment changed terminal state: %#v", stored)
	}
}

func TestEmergencyStopDeliveryFailureRemainsAuditedAndRetryable(t *testing.T) {
	withPersistedEmergencyStop(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	repository := &cancellationTestLedger{rows: []agentruntime.OpenClawGatewayReceipt{cancellationTestReceipt("admitted", now)}}
	gateway := &cancellationTestGateway{stopStatuses: []string{"indeterminate", "cancellation_requested"}}
	service := NewService(repository, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	targeted, acknowledged, awaiting, failed, err := service.FanOutEmergencyStop(context.Background())
	stored := repository.rows[0]
	if err == nil || targeted != 1 || acknowledged != 0 || awaiting != 0 || failed != 1 || stored.CancellationStatus != "delivery_failed" || stored.CancellationAttempts != 1 {
		t.Fatalf("failed delivery was not truthfully persisted: counts=(%d,%d,%d,%d) err=%v row=%#v", targeted, acknowledged, awaiting, failed, err, stored)
	}
	if stored.Status != "admitted" || !stored.TerminalAt.IsZero() {
		t.Fatalf("failed delivery finalized receipt: %#v", stored)
	}
	if err := service.retryPendingCancellations(10); err != nil || len(gateway.stopped) != 1 {
		t.Fatalf("scheduled cancellation retried before its backoff deadline: calls=%d err=%v", len(gateway.stopped), err)
	}
	service.now = func() time.Time { return repository.nextAttemptAt[stored.ExecutionReference] }
	if err := service.retryPendingCancellations(10); err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	stored = repository.rows[0]
	if stored.CancellationStatus != "acknowledged" || stored.CancellationAttempts != 2 || len(gateway.stopped) != 2 {
		t.Fatalf("retry did not retain idempotent audit history: row=%#v calls=%d", stored, len(gateway.stopped))
	}
}

func TestCancellationAutomaticRetriesBackOffEscalateAndAllowExactManualRetry(t *testing.T) {
	withPersistedEmergencyStop(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	receipt := cancellationTestReceipt("admitted", now)
	repository := &cancellationTestLedger{rows: []agentruntime.OpenClawGatewayReceipt{receipt}}
	statuses := []string{"indeterminate", "indeterminate", "indeterminate", "indeterminate", "indeterminate", "indeterminate", "indeterminate", "cancellation_requested"}
	gateway := &cancellationTestGateway{stopStatuses: statuses}
	service := NewService(repository, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	if _, _, _, failed, err := service.FanOutEmergencyStop(context.Background()); err == nil || failed != 1 {
		t.Fatalf("initial explicit cancellation = failed %d, err %v", failed, err)
	}
	intentID := repository.rows[0].CancellationIntentID

	for automaticAttempt := 1; automaticAttempt <= maxAutomaticCancellationAttempts; automaticAttempt++ {
		next := repository.nextAttemptAt[receipt.ExecutionReference]
		gatewayCalls := len(gateway.stopped)
		if next.IsZero() || gatewayCalls != automaticAttempt {
			t.Fatalf("retry #%d was not scheduled after prior attempt: next=%v calls=%d", automaticAttempt, next, gatewayCalls)
		}
		service.now = func() time.Time { return next.Add(-time.Nanosecond) }
		if err := service.retryPendingCancellations(1); err != nil || len(gateway.stopped) != automaticAttempt {
			t.Fatalf("retry #%d ran before its deadline: calls=%d err=%v", automaticAttempt, len(gateway.stopped), err)
		}
		service.now = func() time.Time { return next }
		if err := service.retryPendingCancellations(1); err == nil || len(gateway.stopped) != automaticAttempt+1 {
			t.Fatalf("automatic retry #%d = calls %d err %v", automaticAttempt, len(gateway.stopped), err)
		}
		stored := repository.rows[0]
		if stored.CancellationIntentID != intentID || stored.CancellationAttempts != automaticAttempt+1 {
			t.Fatalf("retry changed durable intent or attempt count: %#v", stored)
		}
		if automaticAttempt < maxAutomaticCancellationAttempts && stored.CancellationStatus != "delivery_failed" {
			t.Fatalf("retry #%d status = %q, want delivery_failed", automaticAttempt, stored.CancellationStatus)
		}
	}
	stored := repository.rows[0]
	if stored.CancellationStatus != "review_required" || !repository.nextAttemptAt[receipt.ExecutionReference].IsZero() || len(gateway.stopped) != 6 {
		t.Fatalf("automatic retry ceiling did not escalate and stop retries: row=%#v next=%v calls=%d", stored, repository.nextAttemptAt[receipt.ExecutionReference], len(gateway.stopped))
	}
	if err := service.retryPendingCancellations(10); err != nil || len(gateway.stopped) != 6 {
		t.Fatalf("escalated intent was retried automatically: calls=%d err=%v", len(gateway.stopped), err)
	}

	service.now = func() time.Time { return now.Add(24 * time.Hour) }
	if _, _, _, failed, err := service.FanOutEmergencyStop(context.Background()); err == nil || failed != 1 || len(gateway.stopped) != 7 {
		t.Fatalf("explicit operator retry failure = calls %d failed %d err %v", len(gateway.stopped), failed, err)
	}
	if repository.rows[0].CancellationStatus != "review_required" || repository.rows[0].CancellationIntentID != intentID {
		t.Fatalf("failed manual retry cleared escalation or changed intent: %#v", repository.rows[0])
	}
	if _, acknowledged, _, failed, err := service.FanOutEmergencyStop(context.Background()); err != nil || acknowledged != 1 || failed != 0 || len(gateway.stopped) != 8 {
		t.Fatalf("explicit operator retry acknowledgment = ack %d failed %d calls %d err %v", acknowledged, failed, len(gateway.stopped), err)
	}
	stored = repository.rows[0]
	if stored.CancellationStatus != "acknowledged" || stored.CancellationIntentID != intentID || stored.Status != "admitted" || !stored.TerminalAt.IsZero() {
		t.Fatalf("manual acknowledgment lost intent or implied terminal status: %#v", stored)
	}
}

func TestEmergencyStopFanOutScansAllUnresolvedReceiptPages(t *testing.T) {
	withPersistedEmergencyStop(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	rows := make([]agentruntime.OpenClawGatewayReceipt, 205)
	for index := range rows {
		rows[index] = cancellationTestReceipt("admitted", now.Add(time.Duration(index)*time.Microsecond))
	}
	repository := &cancellationTestLedger{rows: rows}
	gateway := &cancellationTestGateway{defaultStopStatus: "cancellation_requested"}
	service := NewService(repository, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	targeted, acknowledged, awaiting, failed, err := service.FanOutEmergencyStop(context.Background())
	if err != nil || targeted != len(rows) || acknowledged != len(rows) || awaiting != 0 || failed != 0 || len(gateway.stopped) != len(rows) {
		t.Fatalf("fan-out did not scan all pages: counts=(%d,%d,%d,%d), calls=%d err=%v", targeted, acknowledged, awaiting, failed, len(gateway.stopped), err)
	}
}

func withPersistedEmergencyStop(t *testing.T) {
	t.Helper()
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "test emergency stop", nil
	}))
	t.Cleanup(restore)
}

func cancellationTestReceipt(status string, createdAt time.Time) agentruntime.OpenClawGatewayReceipt {
	return agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-" + uuid.NewString(),
		OwnerIdentity: "owner@example.test", Status: status, SessionKey: "agent:main:session",
		SessionID: "session-instance", RunID: "run-" + uuid.NewString(), CreatedAt: createdAt,
	}
}

type cancellationTestLedger struct {
	rows          []agentruntime.OpenClawGatewayReceipt
	automatic     map[string]int
	nextAttemptAt map[string]time.Time
	leaseUntil    map[string]time.Time
	tokens        map[string]string
}

func (r *cancellationTestLedger) CreateOpenClawGatewayReceipt(_ context.Context, receipt agentruntime.OpenClawGatewayReceipt) error {
	r.rows = append(r.rows, receipt)
	return nil
}

func (r *cancellationTestLedger) MarkOpenClawGatewayReceiptAdmitted(_ context.Context, receipt agentruntime.OpenClawGatewayReceipt) (bool, error) {
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference == receipt.ExecutionReference && stored.OwnerIdentity == receipt.OwnerIdentity && stored.RuntimeTaskID == receipt.RuntimeTaskID && stored.Status == "admitting" {
			previous := *stored
			receipt.CancellationIntentID = previous.CancellationIntentID
			receipt.CancellationStatus = previous.CancellationStatus
			receipt.CancellationMessage = previous.CancellationMessage
			receipt.CancellationAttempts = previous.CancellationAttempts
			receipt.CancellationAt = previous.CancellationAt
			receipt.CancellationTriedAt = previous.CancellationTriedAt
			*stored = receipt
			return true, nil
		}
	}
	return false, nil
}

func (r *cancellationTestLedger) MarkOpenClawGatewayReceiptNotAdmitted(_ context.Context, reference, owner, taskID string) (bool, error) {
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference == reference && stored.OwnerIdentity == owner && stored.RuntimeTaskID == taskID && stored.Status == "admitting" {
			stored.Status = "not_admitted"
			if stored.CancellationIntentID != "" {
				stored.CancellationStatus = "not_required"
			}
			return true, nil
		}
	}
	return false, nil
}

func (r *cancellationTestLedger) FindOpenClawGatewayReceipt(reference string) (agentruntime.OpenClawGatewayReceipt, error) {
	for _, row := range r.rows {
		if row.ExecutionReference == reference {
			return row, nil
		}
	}
	return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("receipt not found")
}

func (r *cancellationTestLedger) FindOpenClawGatewayReceiptForEvent(_ context.Context, owner string, _ uuid.UUID) (agentruntime.OpenClawGatewayReceipt, error) {
	for _, row := range r.rows {
		if row.OwnerIdentity == owner {
			return row, nil
		}
	}
	return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("receipt not found")
}

func (r *cancellationTestLedger) ListUnreconciledOpenClawGatewayReceipts(limit int) ([]agentruntime.OpenClawGatewayReceipt, error) {
	rows := r.unresolved()
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (r *cancellationTestLedger) ListUnreconciledOpenClawGatewayReceiptsAfter(limit int, after time.Time, reference string) ([]agentruntime.OpenClawGatewayReceipt, error) {
	rows := r.unresolved()
	page := make([]agentruntime.OpenClawGatewayReceipt, 0, limit)
	for _, row := range rows {
		if !after.IsZero() && (row.CreatedAt.Before(after) || (row.CreatedAt.Equal(after) && row.ExecutionReference <= reference)) {
			continue
		}
		page = append(page, row)
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

func (r *cancellationTestLedger) ListOpenClawGatewayCancellationReceipts(limit int, after time.Time, reference string) ([]agentruntime.OpenClawGatewayReceipt, error) {
	rows := make([]agentruntime.OpenClawGatewayReceipt, 0)
	for _, row := range r.unresolved() {
		pending := row.CancellationStatus == "requested" || row.CancellationStatus == "delivery_failed" ||
			(row.CancellationStatus == "awaiting_identity" && row.SessionKey != "" && row.RunID != "")
		if !pending || (!after.IsZero() && (row.CreatedAt.Before(after) || (row.CreatedAt.Equal(after) && row.ExecutionReference <= reference))) {
			continue
		}
		rows = append(rows, row)
		if limit > 0 && len(rows) == limit {
			break
		}
	}
	return rows, nil
}

func (r *cancellationTestLedger) ListDueOpenClawGatewayCancellationReceipts(limit int, now time.Time) ([]agentruntime.OpenClawGatewayReceipt, error) {
	rows := make([]agentruntime.OpenClawGatewayReceipt, 0)
	for _, row := range r.unresolved() {
		pending := row.CancellationStatus == "requested" || row.CancellationStatus == "delivery_failed" ||
			(row.CancellationStatus == "awaiting_identity" && row.SessionKey != "" && row.RunID != "")
		if !pending || (!r.nextAttemptAt[row.ExecutionReference].IsZero() && r.nextAttemptAt[row.ExecutionReference].After(now)) ||
			r.leaseUntil[row.ExecutionReference].After(now) {
			continue
		}
		rows = append(rows, row)
		if limit > 0 && len(rows) == limit {
			break
		}
	}
	return rows, nil
}

func (r *cancellationTestLedger) EnsureOpenClawGatewayCancellationIntent(_ context.Context, requested agentruntime.OpenClawGatewayReceipt, now time.Time) (agentruntime.OpenClawGatewayReceipt, error) {
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference != requested.ExecutionReference {
			continue
		}
		if stored.OwnerIdentity != requested.OwnerIdentity || stored.RuntimeTaskID != requested.RuntimeTaskID ||
			(requested.SessionKey != "" && requested.SessionKey != stored.SessionKey) || (requested.RunID != "" && requested.RunID != stored.RunID) {
			return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("durable receipt binding changed")
		}
		if stored.Status == "terminal" || stored.Status == "not_admitted" {
			return *stored, nil
		}
		if stored.CancellationIntentID == "" {
			stored.CancellationIntentID = "ocancel:v1:" + strings.TrimPrefix(stored.ExecutionReference, "ocgw:v2:")
			stored.CancellationAt = now.UTC()
		}
		if stored.CancellationStatus != "acknowledged" && stored.CancellationStatus != "review_required" {
			if !agentruntime.ValidOpenClawGatewayRunID(stored.RunID) || strings.TrimSpace(stored.SessionKey) == "" {
				stored.CancellationStatus = "awaiting_identity"
			} else if stored.CancellationStatus == "" || stored.CancellationStatus == "awaiting_identity" || stored.CancellationStatus == "not_required" {
				stored.CancellationStatus = "requested"
			}
		}
		return *stored, nil
	}
	return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("receipt not found")
}

func (r *cancellationTestLedger) ClaimOpenClawGatewayCancellationAttempt(_ context.Context, expected agentruntime.OpenClawGatewayReceipt, at time.Time, manual bool) (agentruntime.OpenClawGatewayReceipt, string, bool, error) {
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference != expected.ExecutionReference {
			continue
		}
		if stored.OwnerIdentity != expected.OwnerIdentity || stored.RuntimeTaskID != expected.RuntimeTaskID ||
			stored.CancellationIntentID != expected.CancellationIntentID || stored.SessionKey != expected.SessionKey ||
			stored.SessionID != expected.SessionID || stored.RunID != expected.RunID {
			return agentruntime.OpenClawGatewayReceipt{}, "", false, fmt.Errorf("durable exact-run binding changed")
		}
		if stored.Status == "terminal" || stored.Status == "not_admitted" || stored.CancellationStatus == "acknowledged" ||
			stored.CancellationStatus == "not_required" || r.leaseUntil[stored.ExecutionReference].After(at) {
			return agentruntime.OpenClawGatewayReceipt{}, "", false, nil
		}
		if !manual {
			if stored.CancellationStatus == "review_required" || r.nextAttemptAt[stored.ExecutionReference].After(at) {
				return agentruntime.OpenClawGatewayReceipt{}, "", false, nil
			}
			if r.automatic[stored.ExecutionReference] >= maxAutomaticCancellationAttempts {
				stored.CancellationStatus = "review_required"
				return agentruntime.OpenClawGatewayReceipt{}, "", false, nil
			}
		} else if stored.CancellationStatus != "requested" && stored.CancellationStatus != "delivery_failed" &&
			stored.CancellationStatus != "awaiting_identity" && stored.CancellationStatus != "review_required" {
			return agentruntime.OpenClawGatewayReceipt{}, "", false, nil
		}
		if !manual {
			r.ensureRetryMaps()
			r.automatic[stored.ExecutionReference]++
		}
		stored.CancellationAttempts++
		stored.CancellationTriedAt = at.UTC()
		if stored.CancellationStatus == "awaiting_identity" {
			stored.CancellationStatus = "requested"
		}
		token := uuid.NewString()
		r.ensureRetryMaps()
		r.tokens[stored.ExecutionReference] = token
		r.leaseUntil[stored.ExecutionReference] = at.Add(cancellationDeliveryLeaseDuration)
		delete(r.nextAttemptAt, stored.ExecutionReference)
		return *stored, token, true, nil
	}
	return agentruntime.OpenClawGatewayReceipt{}, "", false, fmt.Errorf("cancellation intent not found")
}

func (r *cancellationTestLedger) RecordOpenClawGatewayCancellationAttemptOutcome(_ context.Context, expected agentruntime.OpenClawGatewayReceipt, token, outcome, message string, at time.Time) error {
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference != expected.ExecutionReference {
			continue
		}
		if stored.OwnerIdentity != expected.OwnerIdentity || stored.RuntimeTaskID != expected.RuntimeTaskID ||
			stored.CancellationIntentID != expected.CancellationIntentID || stored.SessionKey != expected.SessionKey ||
			stored.SessionID != expected.SessionID || stored.RunID != expected.RunID {
			return fmt.Errorf("durable exact-run binding changed")
		}
		if stored.CancellationStatus == "acknowledged" {
			return nil
		}
		if r.tokens[stored.ExecutionReference] != token || token == "" {
			return fmt.Errorf("cancellation attempt lease was superseded")
		}
		delete(r.tokens, stored.ExecutionReference)
		delete(r.leaseUntil, stored.ExecutionReference)
		delete(r.nextAttemptAt, stored.ExecutionReference)
		stored.CancellationMessage = message
		if outcome == "acknowledged" {
			stored.CancellationStatus = "acknowledged"
		} else if r.automatic[stored.ExecutionReference] >= maxAutomaticCancellationAttempts {
			stored.CancellationStatus = "review_required"
		} else {
			stored.CancellationStatus = "delivery_failed"
			delayAttempt := r.automatic[stored.ExecutionReference]
			if delayAttempt < 1 {
				delayAttempt = 1
			}
			r.nextAttemptAt[stored.ExecutionReference] = at.Add(cancellationRetryDelay(delayAttempt))
		}
		return nil
	}
	return fmt.Errorf("cancellation intent not found")
}

func (r *cancellationTestLedger) RecordOpenClawGatewayCancellationOutcome(_ context.Context, expected agentruntime.OpenClawGatewayReceipt, outcome, message string, _ time.Time) error {
	if outcome != "awaiting_identity" {
		return fmt.Errorf("unsupported direct cancellation outcome")
	}
	for index := range r.rows {
		stored := &r.rows[index]
		if stored.ExecutionReference != expected.ExecutionReference {
			continue
		}
		if stored.OwnerIdentity != expected.OwnerIdentity || stored.RuntimeTaskID != expected.RuntimeTaskID ||
			stored.SessionKey != expected.SessionKey || stored.RunID != expected.RunID ||
			stored.CancellationStatus == "acknowledged" || stored.CancellationStatus == "review_required" {
			return fmt.Errorf("durable exact-run binding changed")
		}
		stored.CancellationStatus = outcome
		stored.CancellationMessage = message
		return nil
	}
	return fmt.Errorf("cancellation intent not found")
}

func (r *cancellationTestLedger) ensureRetryMaps() {
	if r.automatic == nil {
		r.automatic = make(map[string]int)
	}
	if r.nextAttemptAt == nil {
		r.nextAttemptAt = make(map[string]time.Time)
	}
	if r.leaseUntil == nil {
		r.leaseUntil = make(map[string]time.Time)
	}
	if r.tokens == nil {
		r.tokens = make(map[string]string)
	}
}

func (r *cancellationTestLedger) MarkOpenClawGatewayReceiptTerminal(reference, status string, finishedAt time.Time) (bool, error) {
	for index := range r.rows {
		row := &r.rows[index]
		if row.ExecutionReference == reference && (row.Status == "admitted" || row.Status == "needs_review") {
			row.Status, row.TerminalStatus, row.TerminalAt = "terminal", status, finishedAt.UTC()
			return true, nil
		}
	}
	return false, nil
}

func (r *cancellationTestLedger) MarkOpenClawGatewayReceiptNeedsReview(reference, reason string, reviewedAt time.Time) (bool, error) {
	for index := range r.rows {
		if r.rows[index].ExecutionReference == reference {
			r.rows[index].Status = "needs_review"
			return true, nil
		}
	}
	return false, fmt.Errorf("receipt not found")
}

func (r *cancellationTestLedger) unresolved() []agentruntime.OpenClawGatewayReceipt {
	rows := make([]agentruntime.OpenClawGatewayReceipt, 0, len(r.rows))
	for _, row := range r.rows {
		if row.Status == "admitting" || row.Status == "admitted" || row.Status == "needs_review" {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ExecutionReference < rows[j].ExecutionReference
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	return rows
}

type cancellationTestGateway struct {
	stopStatuses      []string
	defaultStopStatus string
	stopped           []agentruntime.OpenClawGatewayReceipt
}

func (g *cancellationTestGateway) StopOpenClawGatewayReceipt(_ context.Context, receipt agentruntime.OpenClawGatewayReceipt) agentruntime.StopResult {
	g.stopped = append(g.stopped, receipt)
	status := g.defaultStopStatus
	if len(g.stopStatuses) > 0 {
		status = g.stopStatuses[0]
		g.stopStatuses = g.stopStatuses[1:]
	}
	return agentruntime.StopResult{RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference, Status: status}
}

func (g *cancellationTestGateway) ReconcileOpenClawGatewaySession(_ context.Context, owner, taskID, reference string) agentruntime.DelegatedSessionReconcileResult {
	return agentruntime.DelegatedSessionReconcileResult{RuntimeID: "openclaw", OwnerIdentity: owner, TaskID: taskID, ExecutionReference: reference, Status: "running"}
}

var _ automationLedger = (*fakeAutomationLedger)(nil)
var _ receiptLedger = (*cancellationTestLedger)(nil)
var _ cancellationReceiptLedger = (*cancellationTestLedger)(nil)
var _ cancellationAttemptLedger = (*cancellationTestLedger)(nil)

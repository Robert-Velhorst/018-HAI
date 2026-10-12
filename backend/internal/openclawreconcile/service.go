// Package openclawreconcile projects source-backed OpenClaw Gateway terminal
// observations into HAI's immutable automation ledger and delivers durable,
// exact-receipt cancellation intents after the persisted emergency stop.
package openclawreconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	terminalEventPrefix               = "openclaw-gateway-terminal:"
	reviewEventPrefix                 = "openclaw-gateway-review:"
	maxAutomaticCancellationAttempts  = 5
	cancellationRetryBaseDelay        = 30 * time.Second
	cancellationDeliveryLeaseDuration = 5 * time.Minute
)

type receiptLedger interface {
	agentruntime.OpenClawGatewayReceiptStore
	ListUnreconciledOpenClawGatewayReceipts(limit int) ([]agentruntime.OpenClawGatewayReceipt, error)
	MarkOpenClawGatewayReceiptTerminal(reference, status string, finishedAt time.Time) (bool, error)
	MarkOpenClawGatewayReceiptNeedsReview(reference, reason string, reviewedAt time.Time) (bool, error)
}

type pagedReceiptLedger interface {
	ListUnreconciledOpenClawGatewayReceiptsAfter(int, time.Time, string) ([]agentruntime.OpenClawGatewayReceipt, error)
}

type fairReceiptLedger interface {
	ClaimNextUnreconciledOpenClawGatewayReceiptPage(int) ([]agentruntime.OpenClawGatewayReceipt, error)
}

type cancellationReceiptLedger interface {
	agentruntime.OpenClawGatewayCancellationStore
}

type cancellationAttemptLedger interface {
	ListDueOpenClawGatewayCancellationReceipts(int, time.Time) ([]agentruntime.OpenClawGatewayReceipt, error)
	ClaimOpenClawGatewayCancellationAttempt(context.Context, agentruntime.OpenClawGatewayReceipt, time.Time, bool) (agentruntime.OpenClawGatewayReceipt, string, bool, error)
	RecordOpenClawGatewayCancellationAttemptOutcome(context.Context, agentruntime.OpenClawGatewayReceipt, string, string, string, time.Time) error
}

type gatewayReceiptCanceller interface {
	StopOpenClawGatewayReceipt(context.Context, agentruntime.OpenClawGatewayReceipt) agentruntime.StopResult
}

type automationLedger interface {
	FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error)
	FindLaunchIntentByExecutionReference(reference string) (*models.AutomationLaunchEvent, error)
	FindByID(id uuid.UUID) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	SaveLaunchEvent(event *models.AutomationLaunchEvent) error
}

type gatewayReconciler interface {
	ReconcileOpenClawGatewaySession(context.Context, string, string, string) agentruntime.DelegatedSessionReconcileResult
}

type ownerReceiptFinder interface {
	FindOpenClawGatewayReceiptForEvent(context.Context, string, uuid.UUID) (agentruntime.OpenClawGatewayReceipt, error)
}

type gatewayArtifactImporter interface {
	ImportOpenClawGatewayArtifactsAfterTerminalVerification(context.Context, string, string, agentruntime.DelegatedSessionReconcileResult) agentruntime.DelegatedArtifactImportResult
}

type Service struct {
	receipts   receiptLedger
	ledger     automationLedger
	gateway    gatewayReconciler
	artifacts  gatewayArtifactImporter
	now        func() time.Time
	staleAfter time.Duration
}

func NewService(receipts receiptLedger, ledger automationLedger, gateway gatewayReconciler) *Service {
	service := &Service{
		receipts: receipts, ledger: ledger, gateway: gateway,
		now: func() time.Time { return time.Now().UTC() }, staleAfter: staleAfter(),
	}
	if importer, ok := gateway.(gatewayArtifactImporter); ok {
		service.artifacts = importer
	}
	return service
}

// Reconcile observes pending receipts in bounded batches. A nonterminal or
// indeterminate Gateway observation deliberately remains pending, preserving
// the recovery path rather than inventing a terminal automation outcome.
func (s *Service) Reconcile(limit int) (int, error) {
	if s == nil || s.receipts == nil || s.ledger == nil || s.gateway == nil {
		return 0, fmt.Errorf("OpenClaw gateway reconciliation dependencies are unavailable")
	}
	fair, ok := s.receipts.(fairReceiptLedger)
	if !ok {
		return 0, fmt.Errorf("durable fair OpenClaw reconciliation paging is unavailable")
	}
	receipts, err := fair.ClaimNextUnreconciledOpenClawGatewayReceiptPage(limit)
	if err != nil {
		return 0, fmt.Errorf("claim next unreconciled OpenClaw gateway receipt page: %w", err)
	}
	completed := 0
	var failures []error
	if err := s.retryPendingCancellations(limit); err != nil {
		failures = append(failures, err)
	}
	for _, receipt := range receipts {
		settled, err := s.reconcileReceipt(receipt)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if settled {
			completed++
		}
	}
	if err := s.recoverUsage(limit); err != nil {
		failures = append(failures, err)
	}
	if err := s.recoverArtifacts(limit); err != nil {
		failures = append(failures, err)
	}
	return completed, errors.Join(failures...)
}

// FanOutEmergencyStop persists one idempotent cancellation intent per
// unresolved receipt, then requests abort only for receipts carrying the
// exact durable owner/task/reference/session-key/run-ID binding. An abort
// acknowledgment is an outcome of delivery, never a terminal observation.
func (s *Service) FanOutEmergencyStop(ctx context.Context) (targeted, acknowledged, awaitingIdentity, failed int, err error) {
	if s == nil || s.receipts == nil || s.gateway == nil {
		return 0, 0, 0, 1, fmt.Errorf("OpenClaw emergency-stop fan-out dependencies are unavailable")
	}
	if !safety.EvaluateEmergencyStop().Active {
		return 0, 0, 0, 1, fmt.Errorf("persisted emergency stop is not active; cancellation fan-out was not attempted")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	paged, ok := s.receipts.(pagedReceiptLedger)
	if !ok {
		return 0, 0, 0, 1, fmt.Errorf("paged OpenClaw receipt listing is unavailable; no cancellation fan-out was claimed")
	}
	store, ok := s.receipts.(cancellationReceiptLedger)
	if !ok {
		return 0, 0, 0, 1, fmt.Errorf("durable OpenClaw cancellation-intent storage is unavailable")
	}
	canceller, ok := s.gateway.(gatewayReceiptCanceller)
	if !ok {
		return 0, 0, 0, 1, fmt.Errorf("exact OpenClaw receipt cancellation is unavailable")
	}
	var failures []error
	var after time.Time
	afterReference := ""
	for {
		page, listErr := paged.ListUnreconciledOpenClawGatewayReceiptsAfter(100, after, afterReference)
		if listErr != nil {
			failed++
			failures = append(failures, fmt.Errorf("list unresolved OpenClaw receipts for stop fan-out: %w", listErr))
			break
		}
		if len(page) == 0 {
			break
		}
		for _, receipt := range page {
			if !validCancellationReceipt(receipt) {
				failed++
				failures = append(failures, fmt.Errorf("an unresolved OpenClaw receipt has an invalid owner-bound cancellation binding"))
				continue
			}
			if receipt.Status == "terminal" || receipt.Status == "not_admitted" {
				continue
			}
			result, countAsTarget, outcomeErr := requestExactCancellation(ctx, store, canceller, receipt, s.now().UTC(), true)
			if !countAsTarget {
				if outcomeErr != nil {
					failed++
					failures = append(failures, outcomeErr)
				}
				continue
			}
			targeted++
			switch result {
			case "acknowledged":
				acknowledged++
			case "awaiting_identity":
				awaitingIdentity++
			case "failed":
				failed++
			}
			if outcomeErr != nil {
				failures = append(failures, outcomeErr)
			}
		}
		last := page[len(page)-1]
		after, afterReference = last.CreatedAt.UTC(), last.ExecutionReference
		if len(page) < 100 {
			break
		}
	}
	return targeted, acknowledged, awaitingIdentity, failed, errors.Join(failures...)
}

func (s *Service) retryPendingCancellations(limit int) error {
	if s == nil || s.receipts == nil || s.gateway == nil {
		return nil
	}
	store, storeOK := s.receipts.(cancellationReceiptLedger)
	attempts, attemptsOK := s.receipts.(cancellationAttemptLedger)
	canceller, cancelOK := s.gateway.(gatewayReceiptCanceller)
	if !storeOK || !attemptsOK || !cancelOK {
		return nil
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	now := s.now().UTC()
	page, err := attempts.ListDueOpenClawGatewayCancellationReceipts(limit, now)
	if err != nil {
		return fmt.Errorf("list due OpenClaw cancellation intents: %w", err)
	}
	var failures []error
	for _, receipt := range page {
		_, _, cancelErr := requestExactCancellation(context.Background(), store, canceller, receipt, now, false)
		if cancelErr != nil {
			failures = append(failures, cancelErr)
		}
	}
	return errors.Join(failures...)
}

func requestExactCancellation(
	ctx context.Context,
	store cancellationReceiptLedger,
	canceller gatewayReceiptCanceller,
	receipt agentruntime.OpenClawGatewayReceipt,
	now time.Time,
	manualRetry bool,
) (outcome string, targeted bool, err error) {
	if !validCancellationReceipt(receipt) {
		return "failed", false, fmt.Errorf("invalid owner-bound OpenClaw cancellation binding")
	}
	intent, ensureErr := store.EnsureOpenClawGatewayCancellationIntent(ctx, receipt, now)
	if ensureErr != nil {
		return "failed", true, fmt.Errorf("persist OpenClaw cancellation intent: %w", ensureErr)
	}
	if intent.Status == "terminal" || intent.Status == "not_admitted" || intent.CancellationStatus == "not_required" {
		return "", false, nil
	}
	if intent.CancellationStatus == "acknowledged" {
		return "acknowledged", true, nil
	}
	if !agentruntime.ValidOpenClawGatewayRunID(intent.RunID) || strings.TrimSpace(intent.SessionKey) == "" {
		if intent.CancellationStatus == "review_required" {
			return "awaiting_identity", true, nil
		}
		if recordErr := store.RecordOpenClawGatewayCancellationOutcome(ctx, intent, "awaiting_identity", "cancellation intent is durable; exact run identity is not available yet", now); recordErr != nil {
			return "failed", true, fmt.Errorf("persist pending OpenClaw cancellation state: %w", recordErr)
		}
		return "awaiting_identity", true, nil
	}
	if intent.Status != "admitted" && intent.Status != "needs_review" {
		return "awaiting_identity", true, nil
	}
	attempts, ok := store.(cancellationAttemptLedger)
	if !ok {
		return "failed", true, fmt.Errorf("durable OpenClaw cancellation retry controls are unavailable")
	}
	attempt, token, claimed, claimErr := attempts.ClaimOpenClawGatewayCancellationAttempt(ctx, intent, now, manualRetry)
	if claimErr != nil {
		return "failed", true, fmt.Errorf("claim exact OpenClaw cancellation attempt: %w", claimErr)
	}
	if !claimed {
		if manualRetry {
			return "failed", true, fmt.Errorf("exact OpenClaw cancellation is already in progress or no longer retryable")
		}
		return "", false, nil
	}
	stop := canceller.StopOpenClawGatewayReceipt(ctx, attempt)
	if stop.Status != "cancellation_requested" {
		message := "exact-run cancellation delivery failed; the receipt remains unresolved"
		if manualRetry && intent.CancellationStatus == "review_required" {
			message = "operator retry did not receive Gateway acknowledgement; cancellation remains under review and automatic retries remain stopped"
		}
		recordErr := attempts.RecordOpenClawGatewayCancellationAttemptOutcome(ctx, attempt, token, "delivery_failed", message, now)
		return "failed", true, errors.Join(fmt.Errorf("OpenClaw exact-run cancellation was not acknowledged"), recordErr)
	}
	if recordErr := attempts.RecordOpenClawGatewayCancellationAttemptOutcome(ctx, attempt, token, "acknowledged", "Gateway acknowledged exact-run cancellation; terminal state remains unverified", now); recordErr != nil {
		return "failed", true, fmt.Errorf("persist acknowledged OpenClaw cancellation outcome: %w", recordErr)
	}
	return "acknowledged", true, nil
}

func cancellationRetryDelay(nextAutomaticAttempt int) time.Duration {
	if nextAutomaticAttempt < 1 {
		nextAutomaticAttempt = 1
	}
	delay := cancellationRetryBaseDelay
	for attempt := 1; attempt < nextAutomaticAttempt && delay < 30*time.Minute; attempt++ {
		delay *= 2
	}
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}

func validCancellationReceipt(receipt agentruntime.OpenClawGatewayReceipt) bool {
	return validOpenClawGatewayReceiptReference(receipt.ExecutionReference) &&
		strings.TrimSpace(receipt.OwnerIdentity) != "" && strings.TrimSpace(receipt.RuntimeTaskID) != ""
}

type OperatorReconcileResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ReconcileOwnerEvent is a narrow operator path: the event must belong to the
// authenticated owner and resolve to one persisted Gateway execution receipt.
func (s *Service) ReconcileOwnerEvent(ctx context.Context, owner string, eventID uuid.UUID) (*OperatorReconcileResult, error) {
	if s == nil || s.receipts == nil || s.ledger == nil || s.gateway == nil || strings.TrimSpace(owner) == "" || eventID == uuid.Nil {
		return nil, fmt.Errorf("owner-bound OpenClaw reconciliation is unavailable")
	}
	finder, ok := s.receipts.(ownerReceiptFinder)
	if !ok {
		return nil, fmt.Errorf("owner-bound OpenClaw receipt lookup is unavailable")
	}
	receipt, err := finder.FindOpenClawGatewayReceiptForEvent(ctx, owner, eventID)
	if err != nil {
		return nil, err
	}
	if receipt.OwnerIdentity != strings.TrimSpace(owner) || !validOpenClawGatewayReceiptReference(receipt.ExecutionReference) || strings.TrimSpace(receipt.RuntimeTaskID) == "" {
		return nil, fmt.Errorf("OpenClaw receipt does not match the requested owner and event")
	}
	if receipt.Status == "terminal" || receipt.Status == "not_admitted" {
		return &OperatorReconcileResult{Status: receipt.Status, Message: "the exact Gateway receipt is already settled"}, nil
	}
	if !agentruntime.ValidOpenClawGatewayRunID(receipt.RunID) || strings.TrimSpace(receipt.SessionKey) == "" {
		return &OperatorReconcileResult{Status: "indeterminate", Message: "the durable admission record has no exact run identity; no Gateway-wide lookup or cancellation was attempted"}, nil
	}
	if _, err := s.reconcileReceipt(receipt); err != nil {
		return nil, err
	}
	latest, err := s.receipts.FindOpenClawGatewayReceipt(receipt.ExecutionReference)
	if err != nil {
		return nil, fmt.Errorf("reload OpenClaw receipt after reconciliation: %w", err)
	}
	message := "the exact Gateway run remains nonterminal; completion was not claimed"
	if latest.Status == "terminal" {
		message = "terminal status was observed for the exact Gateway run"
	} else if latest.Status == "needs_review" {
		message = "the exact Gateway run remains under review and continues to block maintenance updates"
	}
	return &OperatorReconcileResult{Status: latest.Status, Message: message}, nil
}

func (s *Service) reconcileReceipt(receipt agentruntime.OpenClawGatewayReceipt) (bool, error) {
	if !agentruntime.ValidOpenClawGatewayRunID(receipt.RunID) || strings.TrimSpace(receipt.SessionKey) == "" {
		return s.markStaleForReview(receipt)
	}
	result := s.gateway.ReconcileOpenClawGatewaySession(context.Background(), receipt.OwnerIdentity, receipt.RuntimeTaskID, receipt.ExecutionReference)
	if result.RuntimeID != "openclaw" || result.TaskID != receipt.RuntimeTaskID || result.OwnerIdentity != receipt.OwnerIdentity || result.ExecutionReference != receipt.ExecutionReference {
		return false, fmt.Errorf("OpenClaw gateway receipt %q returned a mismatched reconciliation result", receipt.ExecutionReference)
	}
	switch result.Status {
	case "running":
		return s.markStaleForReview(receipt)
	case "completed", "failed":
		return s.persistTerminal(receipt, result)
	default:
		if marked, err := s.markStaleForReview(receipt); marked || err != nil {
			return marked, err
		}
		return false, fmt.Errorf("OpenClaw gateway receipt %q remains unverified: %s", receipt.ExecutionReference, firstNonEmpty(result.Message, "no terminal outcome"))
	}
}

func (s *Service) markStaleForReview(receipt agentruntime.OpenClawGatewayReceipt) (bool, error) {
	if receipt.CreatedAt.IsZero() || s.staleAfter <= 0 || s.now().UTC().Sub(receipt.CreatedAt.UTC()) < s.staleAfter {
		return false, nil
	}
	automation, err := s.boundAutomation(receipt)
	if err != nil {
		return false, err
	}
	reviewedAt := s.now().UTC()
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      receipt.OwnerIdentity,
		RuntimeType:        "openclaw",
		LaunchType:         "agent_runtime_openclaw_review",
		RuntimeTaskID:      receipt.RuntimeTaskID,
		ExecutionReference: receipt.ExecutionReference,
		EventKey:           reviewEventKey(receipt.ExecutionReference),
		Target:             "gateway://openclaw/delegated-session",
		Status:             "blocked",
		Message:            "OpenClaw Gateway terminal verification exceeded the configured review window",
		AuditEvents: []string{
			"OpenClaw Gateway session remains nonterminal or unverifiable after the configured review window",
			"No terminal outcome was inferred and no Gateway transcript, tool call, or raw error was imported",
		},
		ExitCode:    0,
		DurationMs:  durationSince(receipt.CreatedAt, reviewedAt),
		StartedAt:   receipt.CreatedAt.UTC(),
		CompletedAt: reviewedAt,
	}
	if err := s.ledger.SaveLaunchEvent(event); err != nil {
		return false, fmt.Errorf("persist OpenClaw gateway review event for receipt %q: %w", receipt.ExecutionReference, err)
	}
	if _, err := s.receipts.MarkOpenClawGatewayReceiptNeedsReview(receipt.ExecutionReference, event.Message, reviewedAt); err != nil {
		return false, fmt.Errorf("mark OpenClaw gateway receipt %q for review: %w", receipt.ExecutionReference, err)
	}
	return false, nil
}

func (s *Service) persistTerminal(receipt agentruntime.OpenClawGatewayReceipt, result agentruntime.DelegatedSessionReconcileResult) (bool, error) {
	automation, err := s.boundAutomation(receipt)
	if err != nil {
		return false, err
	}
	finished := result.FinishedAt.UTC()
	if finished.IsZero() {
		finished = s.now().UTC()
	}
	message := safety.RedactSecrets(firstNonEmpty(result.Message, "OpenClaw Gateway terminal verification recorded"))
	auditEvents := []string{"OpenClaw Gateway terminal outcome reconciled from an owner-bound private receipt", "Gateway transcript, tool calls, and raw error detail were not imported"}
	_, durableArtifacts := s.receipts.(artifactRecoveryLedger)
	if result.Status == "completed" && s.artifacts != nil && !durableArtifacts {
		artifactResult := s.artifacts.ImportOpenClawGatewayArtifactsAfterTerminalVerification(context.Background(), receipt.RuntimeTaskID, receipt.ExecutionReference, result)
		switch artifactResult.Status {
		case "imported":
			auditEvents = append(auditEvents, fmt.Sprintf("OpenClaw Gateway bounded artifact metadata import recorded %d descriptor(s)", artifactResult.Count))
		case "none":
			auditEvents = append(auditEvents, "OpenClaw Gateway terminal run had no eligible artifact metadata")
		case "not_configured":
			auditEvents = append(auditEvents, "OpenClaw Gateway artifact metadata import remains disabled")
		default:
			auditEvents = append(auditEvents, "OpenClaw Gateway artifact metadata import was unavailable; terminal status was unchanged")
		}
	}
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      receipt.OwnerIdentity,
		RuntimeType:        "openclaw",
		LaunchType:         "agent_runtime_openclaw_terminal",
		RuntimeTaskID:      receipt.RuntimeTaskID,
		ExecutionReference: receipt.ExecutionReference,
		EventKey:           terminalEventKey(receipt.ExecutionReference),
		Target:             "gateway://openclaw/delegated-session",
		Status:             result.Status,
		Message:            message,
		AuditEvents:        redactEvents(append(auditEvents, result.AuditEvents...)),
		ExitCode:           exitCodeForTerminalStatus(result.Status),
		DurationMs:         durationSince(receipt.CreatedAt, finished),
		StartedAt:          receipt.CreatedAt.UTC(),
		CompletedAt:        finished,
	}
	if err := s.ledger.SaveLaunchEvent(event); err != nil {
		return false, fmt.Errorf("persist OpenClaw gateway terminal event for receipt %q: %w", receipt.ExecutionReference, err)
	}
	if result.Status == "completed" {
		automation.LastSuccessAt = &finished
		automation.LastFailureReason = ""
	} else {
		automation.LastFailureAt = &finished
		automation.LastFailureReason = message
	}
	if _, err := s.ledger.Update(automation); err != nil {
		return false, fmt.Errorf("update automation terminal state for OpenClaw gateway receipt %q: %w", receipt.ExecutionReference, err)
	}
	if _, err := s.receipts.MarkOpenClawGatewayReceiptTerminal(receipt.ExecutionReference, result.Status, finished); err != nil {
		return false, fmt.Errorf("mark OpenClaw gateway receipt %q terminal: %w", receipt.ExecutionReference, err)
	}
	return true, nil
}

func (s *Service) boundAutomation(receipt agentruntime.OpenClawGatewayReceipt) (*models.Automation, error) {
	launch, err := s.ledger.FindLaunchEventByExecutionReference(receipt.ExecutionReference)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		launch, err = s.ledger.FindLaunchIntentByExecutionReference(receipt.ExecutionReference)
	}
	if err != nil {
		return nil, fmt.Errorf("find automation launch for OpenClaw gateway receipt %q: %w", receipt.ExecutionReference, err)
	}
	if launch == nil || launch.AutomationID == uuid.Nil || !strings.EqualFold(strings.TrimSpace(launch.RuntimeType), "openclaw") || launch.RuntimeTaskID != receipt.RuntimeTaskID || launch.OwnerIdentity != receipt.OwnerIdentity || launch.ExecutionReference != receipt.ExecutionReference {
		return nil, fmt.Errorf("OpenClaw gateway receipt %q does not match its owner-bound automation launch", receipt.ExecutionReference)
	}
	automation, err := s.ledger.FindByID(launch.AutomationID)
	if err != nil {
		return nil, fmt.Errorf("find automation for OpenClaw gateway receipt %q: %w", receipt.ExecutionReference, err)
	}
	if automation == nil || automation.ID != launch.AutomationID {
		return nil, fmt.Errorf("OpenClaw gateway receipt %q did not resolve its owner-bound automation", receipt.ExecutionReference)
	}
	return automation, nil
}

func terminalEventKey(reference string) string {
	return terminalEventPrefix + strings.TrimSpace(reference)
}

func reviewEventKey(reference string) string {
	return reviewEventPrefix + strings.TrimSpace(reference)
}

func exitCodeForTerminalStatus(status string) int {
	if status == "completed" {
		return 0
	}
	return -1
}

func durationSince(start, end time.Time) int64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func redactEvents(events []string) []string {
	redacted := make([]string, 0, len(events))
	for _, event := range events {
		if event = strings.TrimSpace(safety.RedactSecrets(event)); event != "" {
			redacted = append(redacted, event)
		}
	}
	return redacted
}

package openclawreconcile

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestReconcileTerminalGatewaySessionCreatesOneTerminalAutomationEvent(t *testing.T) {
	usage := "OpenClaw sessions.usage session-instance snapshot; UTC 2026-09-04 through 2026-09-04; tokens input=12 output=4; estimated USD=0.25 (not provider billing); not per-run usage or a budget debit"
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	receipt := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(),
		RuntimeTaskID:      "automation:" + automationID.String() + ":intent:" + uuid.NewString(),
		OwnerIdentity:      "robert@example.test",
		SessionKey:         "agent:main:test",
		RunID:              "gateway-run-1",
		CreatedAt:          now.Add(-time.Minute),
	}
	receipts := &fakeReceiptRepository{active: []agentruntime.OpenClawGatewayReceipt{receipt}}
	ledger := &fakeAutomationLedger{
		launch: &models.AutomationLaunchEvent{
			AutomationID: automationID, OwnerIdentity: receipt.OwnerIdentity, RuntimeType: "openclaw",
			RuntimeTaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference, Status: "running",
		},
		automation: &models.Automation{ID: automationID},
	}
	service := NewService(receipts, ledger, &fakeGatewayReconciler{results: map[string]agentruntime.DelegatedSessionReconcileResult{
		receipt.ExecutionReference: {RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, Status: "completed", FinishedAt: now, AuditEvents: []string{usage}},
	}})
	service.now = func() time.Time { return now }

	count, err := service.Reconcile(10)
	if err != nil || count != 1 {
		t.Fatalf("Reconcile = %d, %v", count, err)
	}
	if len(ledger.events) != 1 || ledger.events[0].Status != "completed" || ledger.events[0].EventKey != terminalEventKey(receipt.ExecutionReference) {
		t.Fatalf("terminal event = %#v", ledger.events)
	}
	if !strings.Contains(strings.Join(ledger.events[0].AuditEvents, "\n"), usage) {
		t.Fatalf("session usage provenance was lost before persistence: %#v", ledger.events[0])
	}
	if ledger.automation.LastSuccessAt == nil || !ledger.automation.LastSuccessAt.Equal(now) || !receipts.terminal[receipt.ExecutionReference] {
		t.Fatalf("terminal reconciliation did not persist: automation=%#v receipts=%#v", ledger.automation, receipts.terminal)
	}

	if count, err := service.Reconcile(10); err != nil || count != 0 || len(ledger.events) != 1 {
		t.Fatalf("terminal reconciliation was not idempotent: count=%d err=%v events=%#v", count, err, ledger.events)
	}
}

func TestReconcileTerminalGatewaySessionRecordsBoundedArtifactImportOutcome(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	receipt := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-artifacts", OwnerIdentity: "robert@example.test", SessionKey: "agent:main:test", RunID: "gateway-run-artifacts", CreatedAt: now.Add(-time.Minute),
	}
	receipts := &fakeReceiptRepository{active: []agentruntime.OpenClawGatewayReceipt{receipt}}
	ledger := &fakeAutomationLedger{
		launch:     &models.AutomationLaunchEvent{AutomationID: automationID, OwnerIdentity: receipt.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference},
		automation: &models.Automation{ID: automationID},
	}
	gateway := &fakeGatewayReconciler{results: map[string]agentruntime.DelegatedSessionReconcileResult{
		receipt.ExecutionReference: {RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, Status: "completed", FinishedAt: now},
	}, artifactResults: map[string]agentruntime.DelegatedArtifactImportResult{
		receipt.ExecutionReference: {RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, Status: "imported", Count: 2, AuditEvents: []string{"Gateway metadata was stored without content or URLs"}},
	}}
	service := NewService(receipts, ledger, gateway)
	service.now = func() time.Time { return now }

	count, err := service.Reconcile(10)
	if err != nil || count != 1 || len(ledger.events) != 1 || ledger.events[0].Status != "completed" {
		t.Fatalf("Reconcile = %d, %v events=%#v", count, err, ledger.events)
	}
	audit := strings.Join(ledger.events[0].AuditEvents, "\n")
	if !strings.Contains(audit, "bounded artifact metadata import recorded 2 descriptor(s)") || strings.Contains(audit, "secret-artifact") {
		t.Fatalf("terminal audit does not safely record artifact outcome: %q", audit)
	}
}

func TestReconcileKeepsRunningAndUnmatchedGatewaySessionsVisible(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	active := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-running", OwnerIdentity: "robert", SessionKey: "agent:main:run", RunID: "run-active", CreatedAt: now}
	unmatched := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-missing", OwnerIdentity: "robert", SessionKey: "agent:main:missing", RunID: "run-missing", CreatedAt: now}
	receipts := &fakeReceiptRepository{active: []agentruntime.OpenClawGatewayReceipt{active, unmatched}}
	ledger := &fakeAutomationLedger{findErr: errors.New("launch missing")}
	service := NewService(receipts, ledger, &fakeGatewayReconciler{results: map[string]agentruntime.DelegatedSessionReconcileResult{
		active.ExecutionReference:    {RuntimeID: "openclaw", TaskID: active.RuntimeTaskID, Status: "running"},
		unmatched.ExecutionReference: {RuntimeID: "openclaw", TaskID: unmatched.RuntimeTaskID, Status: "completed", FinishedAt: now},
	}})
	service.now = func() time.Time { return now }
	service.staleAfter = 10 * time.Minute

	count, err := service.Reconcile(10)
	if count != 0 || err == nil {
		t.Fatalf("Reconcile = %d, %v; want retained unmatched receipt error", count, err)
	}
	if len(receipts.terminal) != 0 || len(ledger.events) != 0 {
		t.Fatalf("nonterminal or unmatched receipt was silently finalized: terminal=%#v events=%#v", receipts.terminal, ledger.events)
	}
}

func TestReconcileUsesBoundedFairPagesAcrossPersistentScanCycles(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	rows := make([]agentruntime.OpenClawGatewayReceipt, 5)
	results := make(map[string]agentruntime.DelegatedSessionReconcileResult, len(rows)+1)
	for index := range rows {
		rows[index] = agentruntime.OpenClawGatewayReceipt{
			ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-page-" + uuid.NewString(),
			OwnerIdentity: "robert@example.test", SessionKey: "agent:main:fair", RunID: "run-fair-" + uuid.NewString(),
			CreatedAt: now.Add(time.Duration(index) * time.Second),
		}
		results[rows[index].ExecutionReference] = agentruntime.DelegatedSessionReconcileResult{
			RuntimeID: "openclaw", TaskID: rows[index].RuntimeTaskID, Status: "running",
		}
	}
	receipts := &fakeReceiptRepository{active: rows}
	gateway := &fakeGatewayReconciler{results: results}
	service := NewService(receipts, &fakeAutomationLedger{}, gateway)
	service.now = func() time.Time { return now }

	for pass := 1; pass <= 3; pass++ {
		if _, err := service.Reconcile(2); err != nil {
			t.Fatalf("reconcile pass %d: %v", pass, err)
		}
		wantCalls := pass * 2
		if pass == 3 {
			wantCalls = 5
		}
		if gateway.calls != wantCalls {
			t.Fatalf("pass %d made %d gateway calls, want %d", pass, gateway.calls, wantCalls)
		}
	}
	for _, expected := range rows {
		seen := 0
		for _, got := range gateway.references {
			if got == expected.ExecutionReference {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("receipt %s was visited %d times before the scan cycle wrapped", expected.ExecutionReference, seen)
		}
	}

	late := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "task-late-" + uuid.NewString(),
		OwnerIdentity: "robert@example.test", SessionKey: "agent:main:fair", RunID: "run-late-" + uuid.NewString(),
		CreatedAt: now.Add(24 * time.Hour),
	}
	receipts.active = append(receipts.active, late)
	results[late.ExecutionReference] = agentruntime.DelegatedSessionReconcileResult{RuntimeID: "openclaw", TaskID: late.RuntimeTaskID, Status: "running"}
	for pass := 4; pass <= 6; pass++ {
		if _, err := service.Reconcile(2); err != nil {
			t.Fatalf("reconcile pass %d after new arrival: %v", pass, err)
		}
	}
	lateSeen := false
	for _, got := range gateway.references {
		if got == late.ExecutionReference {
			lateSeen = true
		}
	}
	if !lateSeen {
		t.Fatalf("new receipt was not visited after bounded cycle reset: refs=%v", gateway.references)
	}
}

func TestReconcileMarksStaleUnverifiedGatewaySessionForReview(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	receipt := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(),
		RuntimeTaskID:      "automation:" + automationID.String() + ":intent:" + uuid.NewString(),
		OwnerIdentity:      "robert@example.test",
		SessionKey:         "agent:main:review",
		RunID:              "gateway-review-run",
		CreatedAt:          now.Add(-11 * time.Minute),
	}
	receipts := &fakeReceiptRepository{active: []agentruntime.OpenClawGatewayReceipt{receipt}}
	ledger := &fakeAutomationLedger{
		launch: &models.AutomationLaunchEvent{
			AutomationID: automationID, OwnerIdentity: receipt.OwnerIdentity, RuntimeType: "openclaw",
			RuntimeTaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference, Status: "running",
		},
		automation: &models.Automation{ID: automationID},
	}
	gateway := &fakeGatewayReconciler{results: map[string]agentruntime.DelegatedSessionReconcileResult{
		receipt.ExecutionReference: {RuntimeID: "openclaw", TaskID: receipt.RuntimeTaskID, Status: "running"},
	}}
	service := NewService(receipts, ledger, gateway)
	service.now = func() time.Time { return now }
	service.staleAfter = 10 * time.Minute

	settled, err := service.Reconcile(10)
	if err != nil || settled != 0 {
		t.Fatalf("Reconcile = %d, %v", settled, err)
	}
	if !receipts.reviewed[receipt.ExecutionReference] || len(ledger.events) != 1 {
		t.Fatalf("stale receipt was not made reviewable: reviewed=%#v events=%#v", receipts.reviewed, ledger.events)
	}
	event := ledger.events[0]
	if event.Status != "blocked" || event.LaunchType != "agent_runtime_openclaw_review" || event.EventKey != reviewEventKey(receipt.ExecutionReference) {
		t.Fatalf("review event = %#v", event)
	}
	if ledger.automation.LastFailureAt != nil || ledger.automation.LastSuccessAt != nil || ledger.automation.LastFailureReason != "" {
		t.Fatalf("ambiguous stale receipt changed terminal automation state: %#v", ledger.automation)
	}
	if _, err := service.Reconcile(10); err != nil || gateway.calls != 2 {
		t.Fatalf("needs_review receipt stopped polling: calls=%d err=%v", gateway.calls, err)
	}
}

func TestValidReceiptRejectsMalformedOpaqueReferences(t *testing.T) {
	valid := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(),
		OwnerIdentity:      "robert@example.test",
		RuntimeTaskID:      "task-1",
		SessionKey:         "agent:main:session:private",
		RunID:              "run-private",
		CreatedAt:          time.Now().UTC(),
	}
	if !validReceipt(valid) {
		t.Fatal("valid receipt rejected")
	}

	valid.ExecutionReference = "ocgw:v2:not-a-uuid"
	if validReceipt(valid) {
		t.Fatal("malformed opaque reference accepted")
	}
}

type fakeReceiptRepository struct {
	active          []agentruntime.OpenClawGatewayReceipt
	terminal        map[string]bool
	reviewed        map[string]bool
	cursorAt        time.Time
	cursorReference string
	cycleHigh       *agentruntime.OpenClawGatewayReceipt
}

func (r *fakeReceiptRepository) CreateOpenClawGatewayReceipt(context.Context, agentruntime.OpenClawGatewayReceipt) error {
	return nil
}

func (r *fakeReceiptRepository) MarkOpenClawGatewayReceiptAdmitted(context.Context, agentruntime.OpenClawGatewayReceipt) (bool, error) {
	return true, nil
}

func (r *fakeReceiptRepository) MarkOpenClawGatewayReceiptNotAdmitted(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (r *fakeReceiptRepository) FindOpenClawGatewayReceiptForEvent(_ context.Context, owner string, _ uuid.UUID) (agentruntime.OpenClawGatewayReceipt, error) {
	for _, receipt := range r.active {
		if receipt.OwnerIdentity == owner {
			return receipt, nil
		}
	}
	return agentruntime.OpenClawGatewayReceipt{}, errors.New("receipt missing")
}

func (r *fakeReceiptRepository) FindOpenClawGatewayReceipt(reference string) (agentruntime.OpenClawGatewayReceipt, error) {
	for _, receipt := range r.active {
		if receipt.ExecutionReference == reference {
			return receipt, nil
		}
	}
	return agentruntime.OpenClawGatewayReceipt{}, errors.New("receipt missing")
}

func (r *fakeReceiptRepository) ListUnreconciledOpenClawGatewayReceipts(limit int) ([]agentruntime.OpenClawGatewayReceipt, error) {
	active := make([]agentruntime.OpenClawGatewayReceipt, 0, len(r.active))
	for _, receipt := range r.active {
		if !r.terminal[receipt.ExecutionReference] {
			active = append(active, receipt)
		}
	}
	if limit <= 0 || limit > len(active) {
		limit = len(active)
	}
	return append([]agentruntime.OpenClawGatewayReceipt(nil), active[:limit]...), nil
}

func (r *fakeReceiptRepository) ClaimNextUnreconciledOpenClawGatewayReceiptPage(limit int) ([]agentruntime.OpenClawGatewayReceipt, error) {
	limit = boundedReceiptPage(limit)
	rows := r.pendingInOrder()
	if r.cycleHigh == nil {
		if len(rows) == 0 {
			return nil, nil
		}
		high := rows[len(rows)-1]
		r.cycleHigh = &high
	}
	page := r.scanPage(rows, limit)
	if len(page) == 0 {
		r.cursorAt = time.Time{}
		r.cursorReference = ""
		r.cycleHigh = nil
		rows = r.pendingInOrder()
		if len(rows) == 0 {
			return nil, nil
		}
		high := rows[len(rows)-1]
		r.cycleHigh = &high
		page = r.scanPage(rows, limit)
	}
	if len(page) > 0 {
		last := page[len(page)-1]
		r.cursorAt = last.CreatedAt
		r.cursorReference = last.ExecutionReference
	}
	return page, nil
}

func (r *fakeReceiptRepository) pendingInOrder() []agentruntime.OpenClawGatewayReceipt {
	rows := make([]agentruntime.OpenClawGatewayReceipt, 0, len(r.active))
	for _, receipt := range r.active {
		if !r.terminal[receipt.ExecutionReference] && receipt.Status != "terminal" && receipt.Status != "not_admitted" {
			rows = append(rows, receipt)
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

func (r *fakeReceiptRepository) scanPage(rows []agentruntime.OpenClawGatewayReceipt, limit int) []agentruntime.OpenClawGatewayReceipt {
	page := make([]agentruntime.OpenClawGatewayReceipt, 0, limit)
	for _, row := range rows {
		if r.cycleHigh != nil && (row.CreatedAt.After(r.cycleHigh.CreatedAt) ||
			(row.CreatedAt.Equal(r.cycleHigh.CreatedAt) && row.ExecutionReference > r.cycleHigh.ExecutionReference)) {
			continue
		}
		if !r.cursorAt.IsZero() && (row.CreatedAt.Before(r.cursorAt) ||
			(row.CreatedAt.Equal(r.cursorAt) && row.ExecutionReference <= r.cursorReference)) {
			continue
		}
		page = append(page, row)
		if len(page) == limit {
			break
		}
	}
	return page
}

func (r *fakeReceiptRepository) MarkOpenClawGatewayReceiptTerminal(reference, status string, finishedAt time.Time) (bool, error) {
	if r.terminal == nil {
		r.terminal = map[string]bool{}
	}
	if r.terminal[reference] {
		return false, nil
	}
	r.terminal[reference] = true
	return true, nil
}

func (r *fakeReceiptRepository) MarkOpenClawGatewayReceiptNeedsReview(reference, reason string, reviewedAt time.Time) (bool, error) {
	if strings.TrimSpace(reason) == "" || reviewedAt.IsZero() {
		return false, errors.New("invalid review marker")
	}
	if r.reviewed == nil {
		r.reviewed = map[string]bool{}
	}
	if r.reviewed[reference] {
		return false, nil
	}
	r.reviewed[reference] = true
	for index := range r.active {
		if r.active[index].ExecutionReference == reference {
			r.active[index].Status = "needs_review"
		}
	}
	return true, nil
}

type fakeGatewayReconciler struct {
	results         map[string]agentruntime.DelegatedSessionReconcileResult
	artifactResults map[string]agentruntime.DelegatedArtifactImportResult
	calls           int
	references      []string
}

func (r *fakeGatewayReconciler) ImportOpenClawGatewayArtifactsAfterTerminalVerification(_ context.Context, taskID, reference string, terminal agentruntime.DelegatedSessionReconcileResult) agentruntime.DelegatedArtifactImportResult {
	if terminal.RuntimeID != "openclaw" || terminal.TaskID != taskID || terminal.Status != "completed" {
		return agentruntime.DelegatedArtifactImportResult{RuntimeID: "openclaw", TaskID: taskID, Status: "not_terminal"}
	}
	result := r.artifactResults[reference]
	if result.TaskID != taskID {
		return agentruntime.DelegatedArtifactImportResult{RuntimeID: "openclaw", TaskID: taskID, Status: "unavailable"}
	}
	return result
}

func (r *fakeGatewayReconciler) ReconcileOpenClawGatewaySession(_ context.Context, owner, taskID, reference string) agentruntime.DelegatedSessionReconcileResult {
	r.calls++
	r.references = append(r.references, reference)
	result := r.results[reference]
	if result.TaskID != taskID {
		return agentruntime.DelegatedSessionReconcileResult{RuntimeID: "openclaw", OwnerIdentity: owner, ExecutionReference: reference, TaskID: taskID, Status: "indeterminate", Message: "receipt task binding mismatch"}
	}
	result.OwnerIdentity = owner
	result.ExecutionReference = reference
	return result
}

type fakeAutomationLedger struct {
	launch     *models.AutomationLaunchEvent
	automation *models.Automation
	events     []models.AutomationLaunchEvent
	findErr    error
}

func (l *fakeAutomationLedger) FindLaunchEventByExecutionReference(string) (*models.AutomationLaunchEvent, error) {
	if l.findErr != nil {
		return nil, l.findErr
	}
	return l.launch, nil
}

func (l *fakeAutomationLedger) FindLaunchIntentByExecutionReference(string) (*models.AutomationLaunchEvent, error) {
	if l.findErr != nil {
		return nil, l.findErr
	}
	return nil, errors.New("launch intent missing")
}

func (l *fakeAutomationLedger) FindByID(uuid.UUID) (*models.Automation, error) {
	return l.automation, nil
}

func (l *fakeAutomationLedger) Update(automation *models.Automation) (*models.Automation, error) {
	l.automation = automation
	return automation, nil
}

func (l *fakeAutomationLedger) SaveLaunchEvent(event *models.AutomationLaunchEvent) error {
	for _, existing := range l.events {
		if existing.EventKey == event.EventKey {
			return nil
		}
	}
	l.events = append(l.events, *event)
	return nil
}

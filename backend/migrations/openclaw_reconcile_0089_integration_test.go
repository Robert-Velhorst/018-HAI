package migrations_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const openClawReconcile0089 = "0089_openclaw_reconcile_fairness_and_cancellation_retry"

func TestOpenClawReconcile0089PostgresFairnessAndCancellationSafety(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	applyOpenClawReconcileFixture(t, db)
	repo := openclawreconcile.NewRepository(db)
	base := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	// A new repository instance on every pass demonstrates that the keyset and
	// fixed cycle high-water mark live in Postgres rather than process memory.
	references := make([]string, 7)
	for index := range references {
		references[index] = createAdmittedOpenClawReceipt(t, repo, base.Add(time.Duration(index)*time.Minute), fmt.Sprintf("fair-%02d", index))
	}
	seen := make(map[string]bool)
	for pageIndex := 0; pageIndex < 3; pageIndex++ {
		page, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(2)
		if err != nil {
			t.Fatalf("claim persisted page %d: %v", pageIndex+1, err)
		}
		if len(page) == 0 || len(page) > 2 {
			t.Fatalf("page %d length = %d, want 1..2", pageIndex+1, len(page))
		}
		for _, item := range page {
			if seen[item.ExecutionReference] {
				t.Fatalf("receipt %s was repeated before the cycle wrapped", item.ExecutionReference)
			}
			seen[item.ExecutionReference] = true
		}
	}
	for _, reference := range references[:6] {
		if !seen[reference] {
			t.Fatalf("bounded keyset passes starved receipt %s behind the oldest nonterminal rows", reference)
		}
	}

	// Concurrent reservations serialize on the persisted cursor and return
	// different pages even though each caller has a fresh repository value.
	var wg sync.WaitGroup
	pageResults := make(chan []agentruntime.OpenClawGatewayReceipt, 2)
	errResults := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(1)
			if err != nil {
				errResults <- err
				return
			}
			pageResults <- page
		}()
	}
	wg.Wait()
	close(pageResults)
	close(errResults)
	for err := range errResults {
		t.Fatalf("concurrent cursor reservation: %v", err)
	}
	concurrent := make(map[string]bool)
	for page := range pageResults {
		if len(page) != 1 {
			t.Fatalf("concurrent reservation length = %d, want 1", len(page))
		}
		if concurrent[page[0].ExecutionReference] {
			t.Fatalf("concurrent reservations duplicated receipt %s", page[0].ExecutionReference)
		}
		concurrent[page[0].ExecutionReference] = true
	}
	if len(concurrent) != 2 {
		t.Fatalf("got %d distinct concurrent reservations, want 2", len(concurrent))
	}

	// A later-arriving item cannot move the current cycle's high-water mark.
	lateReference := createAdmittedOpenClawReceipt(t, repo, base.Add(24*time.Hour), "late-arrival")
	for len(seen) < len(references) {
		page, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			seen[item.ExecutionReference] = true
		}
		if len(page) == 0 {
			t.Fatal("cursor did not wrap to a new cycle")
		}
	}
	// The next cycle must eventually include the new tail after its bounded
	// pages; the query remains page-limited throughout.
	for !seen[lateReference] {
		page, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			t.Fatal("cursor stopped returning active work before the late receipt was visited")
		}
		for _, item := range page {
			seen[item.ExecutionReference] = true
		}
	}

	// Cancellation intents preserve exact bindings and only become due after
	// exponential backoff; five automatic attempts escalate instead of looping.
	cancelReference := createAdmittedOpenClawReceipt(t, repo, base.Add(48*time.Hour), "cancel-main")
	entry, err := repo.FindOpenClawGatewayReceipt(cancelReference)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := repo.EnsureOpenClawGatewayCancellationIntent(context.Background(), entry, base)
	if err != nil {
		t.Fatalf("persist cancellation intent: %v", err)
	}
	intentID := intent.CancellationIntentID
	attemptedAt := base.Add(48 * time.Hour)
	claimed, token, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), intent, attemptedAt, false)
	if err != nil || !ok || token == "" {
		t.Fatalf("claim first cancellation attempt = ok %v token %q err %v", ok, token, err)
	}
	wrongOwner := claimed
	wrongOwner.OwnerIdentity = "other-owner"
	if _, _, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), wrongOwner, attemptedAt, true); err == nil || ok {
		t.Fatalf("wrong owner binding was accepted: claimed=%v err=%v", ok, err)
	}
	if _, _, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), claimed, attemptedAt, false); err != nil || ok {
		t.Fatalf("active delivery lease did not block duplicate attempt: claimed=%v err=%v", ok, err)
	}
	if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), claimed, token, "delivery_failed", "network unavailable", attemptedAt); err != nil {
		t.Fatalf("record first failed attempt: %v", err)
	}
	var persisted models.OpenClawGatewaySessionReceipt
	if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.CancellationAutomaticAttempts != 1 || persisted.CancellationAttempts != 1 || persisted.CancellationNextAttemptAt == nil ||
		!persisted.CancellationNextAttemptAt.Equal(attemptedAt.Add(30*time.Second)) || persisted.CancellationStatus != "delivery_failed" {
		t.Fatalf("first retry schedule was not durably recorded: %#v", persisted)
	}
	due, err := repo.ListDueOpenClawGatewayCancellationReceipts(10, attemptedAt.Add(29*time.Second))
	if err != nil || len(due) != 0 {
		t.Fatalf("cancellation was listed before backoff elapsed: %d err=%v", len(due), err)
	}

	for automaticAttempt := 2; automaticAttempt <= 5; automaticAttempt++ {
		attemptedAt = *persisted.CancellationNextAttemptAt
		persistedReceipt, err := repo.FindOpenClawGatewayReceipt(cancelReference)
		if err != nil {
			t.Fatal(err)
		}
		claimed, token, ok, err = repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), persistedReceipt, attemptedAt, false)
		if err != nil || !ok {
			t.Fatalf("claim automatic attempt %d = %v, %v", automaticAttempt, ok, err)
		}
		if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), claimed, token, "delivery_failed", "network unavailable", attemptedAt); err != nil {
			t.Fatalf("record automatic attempt %d: %v", automaticAttempt, err)
		}
		if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
			t.Fatal(err)
		}
	}
	var retryDeadlineCleared bool
	if err := db.Raw("SELECT cancellation_next_attempt_at IS NULL FROM openclaw_gateway_session_receipts WHERE execution_reference = ?", cancelReference).Row().Scan(&retryDeadlineCleared); err != nil {
		t.Fatal(err)
	}
	if !retryDeadlineCleared {
		t.Fatal("retry deadline remained persisted after automatic retry exhaustion")
	}
	persisted = models.OpenClawGatewaySessionReceipt{}
	if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.CancellationStatus != "review_required" || persisted.CancellationAutomaticAttempts != 5 ||
		persisted.CancellationNextAttemptAt != nil || persisted.CancellationReviewAt == nil || persisted.CancellationIntentID != intentID {
		t.Fatalf("automatic retry ceiling did not persist review escalation: %#v", persisted)
	}
	due, err = repo.ListDueOpenClawGatewayCancellationReceipts(10, attemptedAt.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range due {
		if item.ExecutionReference == cancelReference {
			t.Fatal("review-required cancellation remained in the automatic retry queue")
		}
	}

	manualReceipt, err := repo.FindOpenClawGatewayReceipt(cancelReference)
	if err != nil {
		t.Fatal(err)
	}
	manualAt := attemptedAt.Add(24 * time.Hour)
	claimed, token, ok, err = repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), manualReceipt, manualAt, true)
	if err != nil || !ok {
		t.Fatalf("explicit retry of escalated intent = %v, %v", ok, err)
	}
	if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), claimed, token, "delivery_failed", "operator retry unavailable", manualAt); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.CancellationStatus != "review_required" || persisted.CancellationIntentID != intentID || persisted.CancellationNextAttemptAt != nil {
		t.Fatalf("failed manual retry weakened the escalation: %#v", persisted)
	}
	manualReceipt, _ = repo.FindOpenClawGatewayReceipt(cancelReference)
	claimed, token, ok, err = repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), manualReceipt, manualAt.Add(time.Minute), true)
	if err != nil || !ok {
		t.Fatalf("second explicit retry of escalated intent = %v, %v", ok, err)
	}
	if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), claimed, token, "acknowledged", "abort acknowledged", manualAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.CancellationStatus != "acknowledged" || persisted.CancellationIntentID != intentID || persisted.Status != "admitted" || persisted.TerminalAt != nil {
		t.Fatalf("abort acknowledgment changed exact intent or implied terminal state: %#v", persisted)
	}

	// A worker crash leaves a lease, not a permanent lock. A later claim may
	// proceed after expiry, and the old worker's token can no longer complete it.
	crashReference := createAdmittedOpenClawReceipt(t, repo, base.Add(72*time.Hour), "cancel-crash")
	crashEntry, err := repo.FindOpenClawGatewayReceipt(crashReference)
	if err != nil {
		t.Fatal(err)
	}
	crashIntent, err := repo.EnsureOpenClawGatewayCancellationIntent(context.Background(), crashEntry, base)
	if err != nil {
		t.Fatal(err)
	}
	crashAt := base.Add(72 * time.Hour)
	firstClaim, firstToken, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), crashIntent, crashAt, false)
	if err != nil || !ok {
		t.Fatalf("claim before simulated crash = %v, %v", ok, err)
	}
	if _, _, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), firstClaim, crashAt.Add(time.Minute), false); err != nil || ok {
		t.Fatalf("unexpired cancellation lease allowed a duplicate: claimed=%v err=%v", ok, err)
	}
	afterCrash, err := repo.FindOpenClawGatewayReceipt(crashReference)
	if err != nil {
		t.Fatal(err)
	}
	secondClaim, secondToken, ok, err := repo.ClaimOpenClawGatewayCancellationAttempt(context.Background(), afterCrash, crashAt.Add(6*time.Minute), false)
	if err != nil || !ok || secondToken == firstToken {
		t.Fatalf("expired lease was not safely reclaimed: claimed=%v token=%q err=%v", ok, secondToken, err)
	}
	if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), firstClaim, firstToken, "acknowledged", "late stale acknowledgment", crashAt.Add(6*time.Minute)); err == nil {
		t.Fatal("stale worker outcome was accepted after lease reclamation")
	}
	if err := repo.RecordOpenClawGatewayCancellationAttemptOutcome(context.Background(), secondClaim, secondToken, "delivery_failed", "retry still unavailable", crashAt.Add(6*time.Minute)); err != nil {
		t.Fatalf("current lease owner could not persist outcome: %v", err)
	}
	for _, reference := range append(append(append([]string{}, references...), lateReference), cancelReference, crashReference) {
		if updated, err := repo.MarkOpenClawGatewayReceiptTerminal(reference, "completed", base.Add(96*time.Hour)); err != nil || !updated {
			t.Fatalf("settle test receipt %s: updated=%v err=%v", reference, updated, err)
		}
	}
	emptyPage, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(2)
	if err != nil || len(emptyPage) != 0 {
		t.Fatalf("empty scan cycle did not reset: page=%#v err=%v", emptyPage, err)
	}
	newReference := createAdmittedOpenClawReceipt(t, repo, base.Add(100*time.Hour), "after-empty-cycle")
	newPage, err := openclawreconcile.NewRepository(db).ClaimNextUnreconciledOpenClawGatewayReceiptPage(2)
	if err != nil || len(newPage) != 1 || newPage[0].ExecutionReference != newReference {
		t.Fatalf("new work was blocked by an empty cursor cycle: page=%#v err=%v", newPage, err)
	}

	downBytes, err := migrations.Files.ReadFile("pre/" + openClawReconcile0089 + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(downBytes)).Error })
	if err == nil || !strings.Contains(err.Error(), "cannot roll back OpenClaw retry state") {
		t.Fatalf("rollback did not refuse to discard persisted retry/cursor evidence: %v", err)
	}
	if !db.Migrator().HasTable("openclaw_gateway_reconcile_cursors") {
		t.Fatal("failed rollback dropped the persisted reconciliation cursor table")
	}
	if err := db.Where("execution_reference = ?", cancelReference).Take(&persisted).Error; err != nil {
		t.Fatalf("failed rollback lost cancellation history: %v", err)
	}
	if persisted.CancellationIntentID != intentID || persisted.CancellationAutomaticAttempts != 5 || persisted.CancellationStatus != "acknowledged" {
		t.Fatalf("failed rollback changed durable cancellation evidence: %#v", persisted)
	}
}

func applyOpenClawReconcileFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	apply := func(name string) {
		t.Helper()
		data, err := migrations.Files.ReadFile("pre/" + name + ".up.sql")
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(data)).Error }); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	for _, name := range []string{"0069_openclaw_gateway_session_receipts", "0070_openclaw_gateway_session_review", "0074_openclaw_session_instance"} {
		apply(name)
	}
	if err := db.Exec(`CREATE TABLE public.open_claw_gateway_session_receipts (LIKE public.openclaw_gateway_session_receipts INCLUDING ALL)`).Error; err != nil {
		t.Fatal(err)
	}
	apply("0075_openclaw_receipt_table_alignment")
	apply("0075_openclaw_receipt_table_alignment")
	for _, name := range []string{
		"0076_openclaw_usage_recovery", "0077_openclaw_usage_snapshot",
		"0081_openclaw_gateway_admission_intents", "0082_openclaw_gateway_cancellation_intents",
		openClawReconcile0089,
	} {
		apply(name)
	}
}

func createAdmittedOpenClawReceipt(t *testing.T, repo *openclawreconcile.Repository, createdAt time.Time, suffix string) string {
	t.Helper()
	receipt := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "owner:" + suffix,
		RuntimeTaskID: "task:" + suffix, Status: "admitting", RequestedModel: "provider/model", CreatedAt: createdAt,
	}
	if err := repo.CreateOpenClawGatewayReceipt(context.Background(), receipt); err != nil {
		t.Fatalf("create OpenClaw admission intent: %v", err)
	}
	admitted := receipt
	admitted.Status, admitted.SessionKey, admitted.SessionID, admitted.RunID = "admitted", "agent:main:"+suffix, "session:"+suffix, "run:"+suffix
	if updated, err := repo.MarkOpenClawGatewayReceiptAdmitted(context.Background(), admitted); err != nil || !updated {
		t.Fatalf("admit OpenClaw receipt: updated=%v err=%v", updated, err)
	}
	return receipt.ExecutionReference
}

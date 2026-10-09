package executionauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresRepositoryFailsClosedWithoutDatabase(t *testing.T) {
	repository := NewPostgresRepository(nil)
	receipt := postgresTestReceipt("owner@example.com", OutcomeAuthorized, time.Now().UTC())
	if _, _, err := repository.CreateOrGet(
		context.Background(),
		receipt,
	); err == nil {
		t.Fatal("CreateOrGet with nil database succeeded")
	}
	if _, err := repository.Get(
		context.Background(),
		receipt.OwnerIdentity,
		receipt.ID,
	); err == nil {
		t.Fatal("Get with nil database succeeded")
	}
	if err := repository.Consume(
		context.Background(),
		postgresTestConsumption(receipt),
	); err == nil {
		t.Fatal("Consume with nil database succeeded")
	}
	if err := repository.ExerciseFinalEffect(
		context.Background(),
		FinalEffectExercise{},
	); err == nil {
		t.Fatal("ExerciseFinalEffect with nil database succeeded")
	}
	if _, err := repository.GetFinalEffectExercise(
		context.Background(),
		receipt.OwnerIdentity,
		receipt.ID,
	); err == nil {
		t.Fatal("GetFinalEffectExercise with nil database succeeded")
	}
}

func TestReceiptReferencesAllowBuiltinConstitutionWithoutDatabaseForeignKey(t *testing.T) {
	receipt := postgresTestReceipt(
		"owner@example.com",
		OutcomeAuthorized,
		time.Now().UTC(),
	)
	receipt.Evidence.Constitution = ConstitutionEvidence{
		ID:                    "builtin-robert-constitution-v1",
		Version:               1,
		Source:                "builtin-robert-constitution-v1:v1",
		Digest:                postgresDigest("builtin-robert-constitution-v1"),
		RequestedCapabilities: []string{"document-read"},
		AuthorityCeiling:      10,
	}

	_, references, err := encodeReceipt(receipt)
	if err != nil {
		t.Fatalf("encode built-in Constitution receipt: %v", err)
	}
	if references.constitutionID != nil ||
		references.constitutionVersion != nil ||
		references.constitutionDigest != nil {
		t.Fatalf("built-in Constitution created database references: %#v", references)
	}
}

func TestReceiptReferencesRejectNonUUIDPersistedConstitution(t *testing.T) {
	receipt := postgresTestReceipt(
		"owner@example.com",
		OutcomeAuthorized,
		time.Now().UTC(),
	)
	receipt.Evidence.Constitution = ConstitutionEvidence{
		ID:                    "not-a-durable-constitution-id",
		Version:               1,
		Source:                "owner-approved",
		Digest:                postgresDigest("owner-approved"),
		RequestedCapabilities: []string{"document-read"},
		AuthorityCeiling:      10,
	}

	if _, _, err := encodeReceipt(receipt); err == nil {
		t.Fatal("non-UUID persisted Constitution was accepted")
	}
}

func TestPostgresRepositoryRejectsMalformedRecordsBeforeSQL(t *testing.T) {
	repository := NewPostgresRepository(&gorm.DB{})
	receipt := postgresTestReceipt("owner@example.com", OutcomeAuthorized, time.Now().UTC())

	receipt.ID = uuid.Nil
	if _, _, err := repository.CreateOrGet(
		context.Background(),
		receipt,
	); err == nil {
		t.Fatal("CreateOrGet accepted a nil receipt id")
	}

	receipt = postgresTestReceipt("owner@example.com", OutcomeAuthorized, time.Now().UTC())
	receipt.Evidence.Trace = []string{strings.Repeat("x", 65536)}
	if _, _, err := repository.CreateOrGet(
		context.Background(),
		receipt,
	); err == nil {
		t.Fatal("CreateOrGet accepted oversized evidence")
	}

	receipt = postgresTestReceipt("owner@example.com", OutcomeAuthorized, time.Now().UTC())
	consumption := postgresTestConsumption(receipt)
	consumption.ReceiptDigest = "not-a-digest"
	if err := repository.Consume(context.Background(), consumption); err == nil {
		t.Fatal("Consume accepted an invalid digest")
	}
}

func TestPostgresRepositoryRoundTripIdempotencyIsolationAndImmutability(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "execution-owner-" + uuid.NewString() + "@example.com"
	receipt := postgresTestReceipt(owner, OutcomeAuthorized, now)
	receipt.Domain = "legal_government"

	stored, created, err := repository.CreateOrGet(ctx, receipt)
	if err != nil {
		t.Fatalf("CreateOrGet: %v", err)
	}
	if !created || !reflect.DeepEqual(stored, receipt) {
		t.Fatalf("created receipt = (%t, %#v), want exact new receipt", created, stored)
	}
	replayed, created, err := repository.CreateOrGet(ctx, receipt)
	if err != nil {
		t.Fatalf("idempotent CreateOrGet: %v", err)
	}
	if created || !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("replayed receipt = (%t, %#v), want exact existing receipt", created, replayed)
	}

	conflicting := receipt
	conflicting.ID = uuid.New()
	conflicting.RequestDigest = postgresDigest("different request")
	conflicting.DecisionDigest = postgresDigest("different decision")
	if _, _, err := repository.CreateOrGet(
		ctx,
		conflicting,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrIdempotencyConflict", err)
	}

	got, err := repository.Get(ctx, owner, receipt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, receipt) {
		t.Fatalf("Get receipt differs:\ngot  %#v\nwant %#v", got, receipt)
	}
	if _, err := repository.Get(
		ctx,
		"other-"+owner,
		receipt.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Get error = %v, want ErrNotFound", err)
	}

	second := postgresTestReceipt(owner, OutcomeDenied, now.Add(time.Second))
	if _, created, err := repository.CreateOrGet(ctx, second); err != nil || !created {
		t.Fatalf("create second receipt = (%t, %v), want true, nil", created, err)
	}
	list, err := repository.List(ctx, owner, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("List = %#v, want newest receipt only", list)
	}
	otherList, err := repository.List(ctx, "other-"+owner, 50)
	if err != nil {
		t.Fatalf("cross-owner List: %v", err)
	}
	if len(otherList) != 0 {
		t.Fatalf("cross-owner List returned %#v", otherList)
	}

	if err := db.Exec(`
		UPDATE public.execution_authorization_receipts
		SET reason = 'tampered'
		WHERE owner_identity = ? AND id = ?`,
		owner,
		receipt.ID,
	).Error; err == nil {
		t.Fatal("database allowed receipt mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.execution_authorization_receipts
		WHERE owner_identity = ? AND id = ?`,
		owner,
		receipt.ID,
	).Error; err == nil {
		t.Fatal("database allowed receipt deletion")
	}

	missingAgent := postgresTestReceipt(owner, OutcomeDenied, now.Add(2*time.Second))
	missingAgent.Evidence.Agent.AgentID = "missing-agent-" + uuid.NewString()
	missingAgent.Evidence.Agent.AssignmentID = "missing-assignment-" + uuid.NewString()
	missingAgent.IdempotencyKey = "missing-reference-" + uuid.NewString()
	missingAgent.ID = uuid.New()
	missingAgent.RequestDigest = postgresDigest(missingAgent.ID.String() + "-request")
	missingAgent.DecisionDigest = postgresDigest(missingAgent.ID.String() + "-decision")
	if _, _, err := repository.CreateOrGet(ctx, missingAgent); err == nil {
		t.Fatal("database accepted receipt with missing owner-scoped agent references")
	}

	constitutionID := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.robert_constitution_versions (
			id, owner_identity, version, base_version, status, change_summary
		) VALUES (?, ?, 1, 0, 'draft', ?)`,
		constitutionID,
		owner,
		"Execution authorization repository integration fixture",
	).Error; err != nil {
		t.Fatalf("create Constitution draft fixture: %v", err)
	}
	if err := db.Exec(`
		UPDATE public.robert_constitution_versions
		SET status = 'active',
			approved_by = ?,
			approval_note = ?,
			approved_at = ?
		WHERE owner_identity = ? AND id = ?`,
		owner,
		"Approved for a bounded integration test",
		now,
		owner,
		constitutionID,
	).Error; err != nil {
		t.Fatalf("activate Constitution reference fixture: %v", err)
	}
	referenced := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(3*time.Second))
	referenced.Evidence.Constitution = ConstitutionEvidence{
		ID:                    constitutionID.String(),
		Version:               1,
		Source:                "integration-test",
		Digest:                postgresDigest("constitution-" + constitutionID.String()),
		RequestedCapabilities: []string{"workspace.safe_worker.execute"},
		AuthorityCeiling:      10,
	}
	if _, created, err := repository.CreateOrGet(ctx, referenced); err != nil || !created {
		t.Fatalf("create Constitution-referenced receipt = (%t, %v)", created, err)
	}
	referencedRoundTrip, err := repository.Get(ctx, owner, referenced.ID)
	if err != nil {
		t.Fatalf("get Constitution-referenced receipt: %v", err)
	}
	if !reflect.DeepEqual(referencedRoundTrip, referenced) {
		t.Fatalf(
			"Constitution-referenced receipt differs:\ngot  %#v\nwant %#v",
			referencedRoundTrip,
			referenced,
		)
	}

	crossOwnerReference := referenced
	crossOwnerReference.ID = uuid.New()
	crossOwnerReference.OwnerIdentity = "unrelated-" + owner
	crossOwnerReference.IdempotencyKey = "cross-owner-" + uuid.NewString()
	crossOwnerReference.TaskID = "task-" + crossOwnerReference.ID.String()
	crossOwnerReference.RequestDigest = postgresDigest(
		crossOwnerReference.ID.String() + "-request",
	)
	crossOwnerReference.DecisionDigest = postgresDigest(
		crossOwnerReference.ID.String() + "-decision",
	)
	if _, _, err := repository.CreateOrGet(ctx, crossOwnerReference); err == nil {
		t.Fatal("database accepted a cross-owner Constitution reference")
	}
}

func TestPostgresRepositoryAtomicSingleUseConsumption(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "consumer-owner-" + uuid.NewString() + "@example.com"

	denied := postgresTestReceipt(owner, OutcomeDenied, now)
	if _, _, err := repository.CreateOrGet(ctx, denied); err != nil {
		t.Fatalf("create denied receipt: %v", err)
	}
	if err := repository.Consume(
		ctx,
		postgresTestConsumption(denied),
	); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("denied consumption error = %v, want ErrNotAuthorized", err)
	}

	authorized := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(time.Second))
	if _, _, err := repository.CreateOrGet(ctx, authorized); err != nil {
		t.Fatalf("create authorized receipt: %v", err)
	}
	if _, err := repository.GetConsumption(
		ctx,
		owner,
		authorized.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pre-consumption lookup error = %v, want ErrNotFound", err)
	}
	badDigest := postgresTestConsumption(authorized)
	badDigest.ReceiptDigest = postgresDigest("wrong decision")
	if err := repository.Consume(
		ctx,
		badDigest,
	); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("wrong digest error = %v, want ErrNotAuthorized", err)
	}
	crossOwner := postgresTestConsumption(authorized)
	crossOwner.OwnerIdentity = "other-" + owner
	if err := repository.Consume(
		ctx,
		crossOwner,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner consumption error = %v, want ErrNotFound", err)
	}

	consumption := postgresTestConsumption(authorized)
	if err := repository.Consume(ctx, consumption); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	got, err := repository.GetConsumption(ctx, owner, authorized.ID)
	if err != nil {
		t.Fatalf("GetConsumption: %v", err)
	}
	if !reflect.DeepEqual(got, consumption) {
		t.Fatalf("stored consumption differs:\ngot  %#v\nwant %#v", got, consumption)
	}
	if err := repository.Consume(
		ctx,
		consumption,
	); !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("second consumption error = %v, want ErrAlreadyConsumed", err)
	}
	if err := db.Exec(`
		UPDATE public.execution_authorization_consumptions
		SET consumer = 'tampered'
		WHERE owner_identity = ? AND receipt_id = ?`,
		owner,
		authorized.ID,
	).Error; err == nil {
		t.Fatal("database allowed consumption mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.execution_authorization_consumptions
		WHERE owner_identity = ? AND receipt_id = ?`,
		owner,
		authorized.ID,
	).Error; err == nil {
		t.Fatal("database allowed consumption deletion")
	}

	concurrent := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(2*time.Second))
	if _, _, err := repository.CreateOrGet(ctx, concurrent); err != nil {
		t.Fatalf("create concurrent receipt: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for index := range 16 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			attempt := postgresTestConsumption(concurrent)
			attempt.Consumer = fmt.Sprintf("worker-%02d", index)
			<-start
			results <- repository.Consume(ctx, attempt)
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	alreadyConsumed := 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrAlreadyConsumed):
			alreadyConsumed++
		default:
			t.Fatalf("unexpected concurrent consumption result: %v", result)
		}
	}
	if successes != 1 || alreadyConsumed != 15 {
		t.Fatalf(
			"concurrent consumption successes=%d already-consumed=%d, want 1 and 15",
			successes,
			alreadyConsumed,
		)
	}
}

func TestPostgresRepositoryRejectsExpiredApprovalAtConsumption(t *testing.T) {
	repository, _ := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner := "expired-approval-claim-" + uuid.NewString() + "@example.com"
	receipt := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(-time.Second))
	receipt.ApprovalSourceID = "approval:expired-at-claim"
	receipt.Evidence.Approval = ApprovalEvidence{
		SourceID:       receipt.ApprovalSourceID,
		DecisionID:     uuid.NewString(),
		DecisionDigest: postgresDigest("expired-approval-decision"),
		ApprovedBy:     owner,
		ApprovedAt:     now.Add(-2 * time.Minute),
		ExpiresAt:      now.Add(-time.Second),
	}
	if _, created, err := repository.CreateOrGet(ctx, receipt); err != nil || !created {
		t.Fatalf("create expired-approval receipt = (%t, %v)", created, err)
	}

	consumption := postgresTestConsumption(receipt)
	consumption.ConsumedAt = now
	if err := repository.Consume(ctx, consumption); !errors.Is(err, ErrAuthorizationChanged) {
		t.Fatalf("Consume error = %v, want ErrAuthorizationChanged", err)
	}
	if _, err := repository.GetConsumption(ctx, owner, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired receipt has consumption record: err=%v", err)
	}
}

func TestPostgresRepositoryClaimsTaskReviewDecisionAcrossReceiptsAtomically(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "task-review-claim-owner-" + uuid.NewString() + "@example.com"
	decisionID, sourceID := createTaskReviewDecisionFixture(t, db, owner, now)

	receipts := []Receipt{
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(2*time.Second)),
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(3*time.Second)),
	}
	for index := range receipts {
		bindReceiptApproval(&receipts[index], sourceID, decisionID, owner, now)
		if _, created, err := repository.CreateOrGet(ctx, receipts[index]); err != nil || !created {
			t.Fatalf("create task-approved receipt %d = (%t, %v)", index, created, err)
		}
	}

	start := make(chan struct{})
	results := make(chan error, len(receipts))
	for index := range receipts {
		index := index
		go func() {
			consumption := postgresTestConsumption(receipts[index])
			consumption.Consumer = fmt.Sprintf("task-review-worker-%d", index)
			<-start
			results <- repository.Consume(ctx, consumption)
		}()
	}
	close(start)

	successes, claimed := 0, 0
	for range receipts {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrApprovalAlreadyClaimed):
			claimed++
		default:
			t.Fatalf("unexpected concurrent task-review consumption result: %v", err)
		}
	}
	if successes != 1 || claimed != 1 {
		t.Fatalf("consumption successes=%d approval-claimed=%d, want 1 and 1", successes, claimed)
	}
}

func TestPostgresRepositoryClaimsOwnerControlApprovalAcrossReceipts(t *testing.T) {
	repository, _ := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "owner-control-claim-" + uuid.NewString() + "@example.com"
	sourceID := "opscontrol-owner:nonce:" + uuid.NewString()
	decisionID := postgresDigest("owner-control-decision:" + uuid.NewString())
	receipts := []Receipt{
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(time.Second)),
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(2*time.Second)),
	}
	for index := range receipts {
		receipts[index].ApprovalSourceID = sourceID
		receipts[index].Evidence.Approval = ApprovalEvidence{
			SourceID: sourceID, DecisionID: decisionID,
			DecisionDigest: postgresDigest("decision-digest:" + uuid.NewString()),
			ApprovedBy:     owner, ApprovedAt: now, ExpiresAt: now.Add(10 * time.Minute),
		}
		if _, created, err := repository.CreateOrGet(ctx, receipts[index]); err != nil || !created {
			t.Fatalf("create owner-control receipt %d = (%t, %v)", index, created, err)
		}
	}

	start := make(chan struct{})
	results := make(chan error, len(receipts))
	for index := range receipts {
		index := index
		go func() {
			consumption := postgresTestConsumption(receipts[index])
			consumption.Consumer = fmt.Sprintf("owner-control-worker-%d", index)
			<-start
			results <- repository.Consume(ctx, consumption)
		}()
	}
	close(start)

	successes, claimed := 0, 0
	for range receipts {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrApprovalAlreadyClaimed):
			claimed++
		default:
			t.Fatalf("unexpected owner-control consumption result: %v", err)
		}
	}
	if successes != 1 || claimed != 1 {
		t.Fatalf("consumption successes=%d approval-claimed=%d, want one each", successes, claimed)
	}
}

func TestPostgresConsumptionTriggerClaimsTaskReviewForLegacyWriters(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "task-review-legacy-writer-" + uuid.NewString() + "@example.com"
	decisionID, sourceID := createTaskReviewDecisionFixture(t, db, owner, now)

	receipts := []Receipt{
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(2*time.Second)),
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(3*time.Second)),
	}
	for index := range receipts {
		bindReceiptApproval(&receipts[index], sourceID, decisionID, owner, now)
		if _, created, err := repository.CreateOrGet(ctx, receipts[index]); err != nil || !created {
			t.Fatalf("create legacy-writer receipt %d = (%t, %v)", index, created, err)
		}
	}

	first := postgresTestConsumption(receipts[0])
	if err := db.Exec(`
		INSERT INTO public.execution_authorization_consumptions (
			owner_identity, receipt_id, consumer, execution_target,
			receipt_digest, consumed_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		first.OwnerIdentity,
		first.ReceiptID,
		first.Consumer,
		first.ExecutionTarget,
		first.ReceiptDigest,
		first.ConsumedAt,
	).Error; err != nil {
		t.Fatalf("direct legacy-style consumption insert: %v", err)
	}

	var claimCount int64
	if err := db.Table("execution_authorization_task_review_claims").
		Where("owner_identity = ? AND task_review_decision_id = ?", owner, decisionID).
		Count(&claimCount).Error; err != nil || claimCount != 1 {
		t.Fatalf("trigger claim count = %d, err=%v; want one", claimCount, err)
	}

	second := postgresTestConsumption(receipts[1])
	err := db.Exec(`
		INSERT INTO public.execution_authorization_consumptions (
			owner_identity, receipt_id, consumer, execution_target,
			receipt_digest, consumed_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		second.OwnerIdentity,
		second.ReceiptID,
		second.Consumer,
		second.ExecutionTarget,
		second.ReceiptDigest,
		second.ConsumedAt,
	).Error
	if err == nil || !strings.Contains(err.Error(), "uq_execution_authorization_task_review_claim_decision") {
		t.Fatalf("second direct insert error = %v; want task-review claim uniqueness violation", err)
	}
	var secondConsumptionCount int64
	if err := db.Table("execution_authorization_consumptions").
		Where("owner_identity = ? AND receipt_id = ?", owner, second.ReceiptID).
		Count(&secondConsumptionCount).Error; err != nil || secondConsumptionCount != 0 {
		t.Fatalf("rejected legacy consumption count = %d, err=%v; want zero", secondConsumptionCount, err)
	}
}

func TestPostgresFinalEffectExerciseIsAtomicAndOwnerScoped(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-5 * time.Second)
	owner := "effect-owner-" + uuid.NewString() + "@example.com"
	finalRequest, err := BuildAgentRuntimeFinalEffectRequest(
		"hermes",
		"task-"+uuid.NewString(),
		owner,
		"project-final-effect",
		"perform the bounded runtime task",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
	}
	effectDigest, err := FinalEffectDigest(finalRequest)
	if err != nil {
		t.Fatalf("FinalEffectDigest: %v", err)
	}
	receipt := postgresTestReceipt(owner, OutcomeAuthorized, now)
	receipt.TaskID = finalRequest.TaskID
	receipt.Action = AgentRuntimeExecuteAction
	receipt.ResourceType = AgentRuntimeResourceType
	receipt.ResourceID = finalRequest.TaskID
	receipt.ProjectKey = finalRequest.ProjectKey
	receipt.RuntimeID = finalRequest.RuntimeID
	receipt.EffectDigest = effectDigest
	receipt.RequestDigest = postgresDigest(receipt.ID.String() + "-runtime-request")
	receipt.DecisionDigest = postgresDigest(receipt.ID.String() + "-runtime-decision")
	if _, created, err := repository.CreateOrGet(ctx, receipt); err != nil || !created {
		t.Fatalf("create runtime receipt = (%t, %v)", created, err)
	}
	target, _ := FinalEffectExecutionTarget(effectDigest)
	consumption := postgresTestConsumption(receipt)
	consumption.ExecutionTarget = target
	if err := repository.Consume(ctx, consumption); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	bridge, err := NewFinalEffectBridge(repository, func() time.Time {
		return now.Add(2 * time.Second)
	})
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	binding, err := bridge.BindConsumedFinalEffect(ctx, finalRequest, receipt.ID)
	if err != nil {
		t.Fatalf("BindConsumedFinalEffect: %v", err)
	}
	proof := proofFromBinding(t, binding, effectDigest)

	start := make(chan struct{})
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- bridge.VerifyFinalEffectProof(ctx, finalRequest, proof)
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	replays := 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrAlreadyExercised):
			replays++
		default:
			t.Fatalf("unexpected concurrent final-effect result: %v", result)
		}
	}
	if successes != 1 || replays != 15 {
		t.Fatalf(
			"concurrent final effects successes=%d replays=%d, want 1 and 15",
			successes,
			replays,
		)
	}
	stored, err := repository.GetFinalEffectExercise(ctx, owner, receipt.ID)
	if err != nil {
		t.Fatalf("GetFinalEffectExercise: %v", err)
	}
	if stored.EffectDigest != effectDigest ||
		stored.RuntimeID != finalRequest.RuntimeID ||
		stored.ConsumptionTarget != target {
		t.Fatalf("stored final effect differs: %#v", stored)
	}
	if _, err := repository.GetFinalEffectExercise(
		ctx,
		"other-"+owner,
		receipt.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner final effect lookup = %v, want ErrNotFound", err)
	}
	if err := db.Exec(`
		UPDATE public.execution_authorization_final_effect_exercises
		SET runtime_id = 'tampered'
		WHERE owner_identity = ? AND receipt_id = ?`,
		owner,
		receipt.ID,
	).Error; err == nil {
		t.Fatal("database allowed final effect mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.execution_authorization_final_effect_exercises
		WHERE owner_identity = ? AND receipt_id = ?`,
		owner,
		receipt.ID,
	).Error; err == nil {
		t.Fatal("database allowed final effect deletion")
	}
}

func TestPostgresFinalEffectExerciseRejectsStaleAuthority(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tests := []struct {
		name           string
		consumedAt     time.Time
		exercisedAt    time.Time
		approval       bool
		approvalExpiry time.Time
	}{
		{
			name:        "database time rejects stale consumption despite backdated caller time",
			consumedAt:  now.Add(-finalEffectFreshnessWindow - time.Second),
			exercisedAt: now.Add(-finalEffectFreshnessWindow),
		},
		{
			name:           "approval expired within freshness window",
			consumedAt:     now.Add(-5 * time.Second),
			exercisedAt:    now,
			approval:       true,
			approvalExpiry: now.Add(-time.Second),
		},
		{
			name:           "approval expired immediately before effect boundary",
			consumedAt:     now.Add(-5 * time.Second),
			exercisedAt:    now,
			approval:       true,
			approvalExpiry: now.Add(-time.Microsecond),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := "stale-effect-owner-" + uuid.NewString() + "@example.com"
			approvalSource := ""
			approvalDecisionID := uuid.Nil
			if test.approval {
				approvalDecisionID, approvalSource = createTaskReviewDecisionFixture(
					t,
					db,
					owner,
					test.consumedAt.Add(-10*time.Second),
				)
			}
			finalRequest, err := BuildAgentRuntimeFinalEffectRequest(
				"hermes",
				"task-"+uuid.NewString(),
				owner,
				"project-stale-final-effect",
				"perform the bounded runtime task",
				approvalSource,
				approvalSource != "",
			)
			if err != nil {
				t.Fatalf("BuildAgentRuntimeFinalEffectRequest: %v", err)
			}
			effectDigest, err := FinalEffectDigest(finalRequest)
			if err != nil {
				t.Fatalf("FinalEffectDigest: %v", err)
			}
			receipt := postgresTestReceipt(owner, OutcomeAuthorized, test.consumedAt.Add(-time.Second))
			receipt.TaskID = finalRequest.TaskID
			receipt.Action = AgentRuntimeExecuteAction
			receipt.ResourceType = AgentRuntimeResourceType
			receipt.ResourceID = finalRequest.TaskID
			receipt.ProjectKey = finalRequest.ProjectKey
			receipt.RuntimeID = finalRequest.RuntimeID
			receipt.ApprovalSourceID = approvalSource
			receipt.EffectDigest = effectDigest
			receipt.RequestDigest = postgresDigest(receipt.ID.String() + "-stale-request")
			receipt.DecisionDigest = postgresDigest(receipt.ID.String() + "-stale-decision")
			if approvalSource != "" {
				receipt.Evidence.Approval = ApprovalEvidence{
					SourceID:       approvalSource,
					DecisionID:     approvalDecisionID.String(),
					DecisionDigest: postgresDigest("stale-effect-approval"),
					ApprovedBy:     owner,
					ApprovedAt:     test.consumedAt.Add(-time.Second),
					ExpiresAt:      test.approvalExpiry,
				}
			}
			if _, created, err := repository.CreateOrGet(ctx, receipt); err != nil || !created {
				t.Fatalf("create runtime receipt = (%t, %v)", created, err)
			}
			target, err := FinalEffectExecutionTarget(effectDigest)
			if err != nil {
				t.Fatalf("FinalEffectExecutionTarget: %v", err)
			}
			consumption := postgresTestConsumption(receipt)
			consumption.ExecutionTarget = target
			consumption.ConsumedAt = test.consumedAt
			if err := repository.Consume(ctx, consumption); err != nil {
				t.Fatalf("Consume: %v", err)
			}
			proof := agentruntime.FinalEffectAuthorizationProof{
				ReceiptID:                  receipt.ID.String(),
				AuthorizationRequestDigest: receipt.RequestDigest,
				DecisionDigest:             receipt.DecisionDigest,
				RuntimeRequestDigest:       effectDigest,
				RuntimeProof:               effectDigest,
			}
			exercise := finalEffectExercise(finalRequest, proof, test.exercisedAt)
			if err := repository.ExerciseFinalEffect(ctx, exercise); !errors.Is(err, ErrFinalEffectExpired) {
				t.Fatalf("ExerciseFinalEffect error = %v, want ErrFinalEffectExpired", err)
			}
			if _, err := repository.GetFinalEffectExercise(ctx, owner, receipt.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("stale authority created an exercise record: %v", err)
			}
		})
	}
}

func TestPostgresReceiptPersistsOwnerSafePolymorphicApprovals(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "approval-owner-" + uuid.NewString() + "@example.com"

	taskDecisionID, taskApprovalSourceID := createTaskReviewDecisionFixture(t, db, owner, now)
	taskReceipt := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(5*time.Second))
	bindReceiptApproval(
		&taskReceipt,
		taskApprovalSourceID,
		taskDecisionID,
		owner,
		now,
	)
	storedTask, created, err := repository.CreateOrGet(ctx, taskReceipt)
	if err != nil || !created {
		t.Fatalf("create task-approved receipt = (%t, %v)", created, err)
	}
	if storedTask.ApprovalSourceID != taskReceipt.ApprovalSourceID {
		t.Fatalf("task approval source = %q", storedTask.ApprovalSourceID)
	}

	workflowID, workflowDecisionID := createWorkflowDecisionFixture(
		t,
		db,
		owner,
		now,
	)
	workflowReceipt := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(6*time.Second))
	bindReceiptApproval(
		&workflowReceipt,
		"workflow-decision:"+workflowDecisionID.String(),
		workflowDecisionID,
		owner,
		now,
	)
	storedWorkflow, created, err := repository.CreateOrGet(ctx, workflowReceipt)
	if err != nil || !created {
		t.Fatalf("create workflow-approved receipt = (%t, %v)", created, err)
	}
	if storedWorkflow.ApprovalSourceID != workflowReceipt.ApprovalSourceID {
		t.Fatalf("workflow approval source = %q", storedWorkflow.ApprovalSourceID)
	}

	controlReceipt := postgresTestReceipt(owner, OutcomeAuthorized, now.Add(7*time.Second))
	controlReceipt.ApprovalSourceID = "opscontrol-owner:test-signed-approval"
	controlReceipt.Evidence.Approval = ApprovalEvidence{
		SourceID:       controlReceipt.ApprovalSourceID,
		DecisionID:     postgresDigest("owner-control-decision"),
		DecisionDigest: postgresDigest("owner-control-receipt"),
		ApprovedBy:     owner,
		ApprovedAt:     now,
		ExpiresAt:      now.Add(time.Minute),
	}
	storedControl, created, err := repository.CreateOrGet(ctx, controlReceipt)
	if err != nil || !created {
		t.Fatalf("create owner-control-approved receipt = (%t, %v)", created, err)
	}
	if storedControl.ApprovalSourceID != controlReceipt.ApprovalSourceID ||
		storedControl.Evidence.Approval.DecisionID != controlReceipt.Evidence.Approval.DecisionID {
		t.Fatalf("owner control approval evidence = %#v", storedControl.Evidence.Approval)
	}

	crossOwner := workflowReceipt
	crossOwner.ID = uuid.New()
	crossOwner.OwnerIdentity = "other-" + owner
	crossOwner.IdempotencyKey = "cross-owner-approval-" + crossOwner.ID.String()
	crossOwner.TaskID = "task-" + crossOwner.ID.String()
	crossOwner.RequestDigest = postgresDigest(crossOwner.ID.String() + "-request")
	crossOwner.DecisionDigest = postgresDigest(crossOwner.ID.String() + "-decision")
	crossOwner.EffectDigest = postgresDigest(crossOwner.ID.String() + "-effect")
	if _, _, err := repository.CreateOrGet(ctx, crossOwner); err == nil {
		t.Fatal("database accepted cross-owner workflow approval provenance")
	}

	if err := db.Exec(`
		INSERT INTO public.workflow_decisions (
			id, workflow_id, owner_identity, decision_type, decision,
			reason, rule_applied, approved, actor, created_at
		) VALUES (?, ?, ?, 'approval', 'approved', ?, ?, true, ?, ?)`,
		uuid.New(),
		workflowID,
		"other-"+owner,
		"mismatched owner",
		"automation-action:test:"+postgresDigest("mismatch"),
		owner,
		now,
	).Error; err == nil {
		t.Fatal("database accepted workflow decision with mismatched owner")
	}
}

func TestReceiptReferencesAcceptSignedOwnerControlApproval(t *testing.T) {
	evidence := DecisionEvidence{Approval: ApprovalEvidence{
		SourceID:       "opscontrol-owner:fixture",
		DecisionID:     postgresDigest("owner-control-decision"),
		DecisionDigest: postgresDigest("owner-control-receipt"),
	}}
	references, err := receiptReferencesFromEvidence(evidence)
	if err != nil {
		t.Fatalf("signed owner-control approval reference: %v", err)
	}
	if references.taskReviewDecisionID != nil ||
		references.workflowDecisionID != nil ||
		references.portfolioProposalDecisionID != nil {
		t.Fatalf("owner-control approval must not create an unrelated foreign key: %#v", references)
	}

	evidence.Approval.DecisionID = "not-a-sha256-digest"
	if _, err := receiptReferencesFromEvidence(evidence); err == nil {
		t.Fatal("malformed signed owner-control approval was accepted")
	}
}

func executionAuthorizationPostgresRepository(
	t *testing.T,
) (*PostgresRepository, *gorm.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return NewPostgresRepository(db), db
}

func postgresTestReceipt(owner string, outcome Outcome, evaluatedAt time.Time) Receipt {
	id := uuid.New()
	return Receipt{
		ID:                   id,
		ContractVersion:      ContractVersion,
		OwnerIdentity:        owner,
		IdempotencyKey:       "execution-" + id.String(),
		ActorIdentity:        "system:test-runner",
		ActorKind:            ActorSystem,
		TaskID:               "task-" + id.String(),
		Action:               "workspace.safe_worker.execute",
		Stage:                StageExecution,
		ResourceType:         "workspace-file",
		ResourceID:           "artifact-" + id.String(),
		ProjectKey:           "project-" + id.String(),
		Domain:               "personal_admin",
		EffectDigest:         postgresDigest(id.String() + "-effect"),
		Outcome:              outcome,
		Reason:               "all execution policy boundaries were evaluated",
		RequestDigest:        postgresDigest(id.String() + "-request"),
		DecisionDigest:       postgresDigest(id.String() + "-decision"),
		RequiredAuthority:    4,
		RequestedAutonomy:    8,
		EffectiveAutonomy:    7,
		Risk:                 RiskLow,
		Reversible:           true,
		EstimatedCostEUR:     0,
		NotificationRequired: false,
		EvaluatedAt:          evaluatedAt.UTC(),
		Evidence: DecisionEvidence{
			EmergencyStop: EmergencyStopEvidence{
				Active: false,
				Source: "test",
			},
			ReasonCodes: []string{"test.authorized"},
			Trace:       []string{"policy evaluated"},
		},
	}
}

func postgresTestConsumption(receipt Receipt) Consumption {
	return Consumption{
		ReceiptID:       receipt.ID,
		OwnerIdentity:   receipt.OwnerIdentity,
		Consumer:        "system:test-runner",
		ExecutionTarget: "local-safe-worker",
		ReceiptDigest:   receipt.DecisionDigest,
		ConsumedAt:      receipt.EvaluatedAt.Add(time.Second).UTC(),
	}
}

func postgresDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func createTaskReviewDecisionFixture(
	t *testing.T,
	db *gorm.DB,
	owner string,
	now time.Time,
) (uuid.UUID, string) {
	t.Helper()
	itemID := uuid.New()
	decisionID := uuid.New()
	planID := "plan-" + itemID.String()
	requestDigest := postgresDigest("task-review-" + itemID.String())
	resolvedAt := now.Add(time.Second)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO public.task_review_items (
				id, owner_identity, original_task_plan_id,
				current_task_plan_id, request_digest, request_json,
				reason, priority, status, review_revision,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, 'normal',
				'needs_review', 1, ?, ?)`,
			itemID,
			owner,
			planID,
			planID,
			requestDigest,
			"owner review required",
			now,
			now,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO public.task_review_decisions (
				id, review_item_id, review_revision, owner_identity,
				task_plan_id, decision, resolution_note, resolved_by,
				approval_source, approval_source_id, request_digest,
				resolved_at
			) VALUES (?, ?, 1, ?, ?, 'approved', ?, ?,
				'task-review', ?, ?, ?)`,
			decisionID,
			itemID,
			owner,
			planID,
			"approved for execution authorization integration test",
			owner,
			"task-review:"+itemID.String(),
			requestDigest,
			resolvedAt,
		).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE public.task_review_items
			SET status = 'approved', updated_at = ?, resolved_at = ?
			WHERE id = ?`,
			resolvedAt,
			resolvedAt,
			itemID,
		).Error
	})
	if err != nil {
		t.Fatalf("create task review decision fixture: %v", err)
	}
	return decisionID, "task-review:" + itemID.String()
}

func createWorkflowDecisionFixture(
	t *testing.T,
	db *gorm.DB,
	owner string,
	now time.Time,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	workflowID := uuid.New()
	decisionID := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.workflow_items (
			id, owner_identity, title, current_state, created_at, updated_at
		) VALUES (?, ?, ?, 'awaiting_approval', ?, ?)`,
		workflowID,
		owner,
		"Execution authorization approval fixture",
		now,
		now,
	).Error; err != nil {
		t.Fatalf("create workflow item fixture: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO public.workflow_decisions (
			id, workflow_id, decision_type, decision, reason,
			rule_applied, approved, actor, created_at
		) VALUES (?, ?, 'approval', 'approved', ?, ?, true, ?, ?)`,
		decisionID,
		workflowID,
		"owner approved exact runtime action",
		"automation-action:agent-runtime.execute-task:"+
			postgresDigest("workflow-"+decisionID.String()),
		owner,
		now,
	).Error; err != nil {
		t.Fatalf("create workflow decision fixture: %v", err)
	}
	return workflowID, decisionID
}

func bindReceiptApproval(
	receipt *Receipt,
	sourceID string,
	decisionID uuid.UUID,
	owner string,
	now time.Time,
) {
	receipt.ApprovalSourceID = sourceID
	receipt.Evidence.Approval = ApprovalEvidence{
		SourceID:       sourceID,
		DecisionID:     decisionID.String(),
		DecisionDigest: postgresDigest("approval-" + decisionID.String()),
		ApprovedBy:     owner,
		ApprovedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
	}
}

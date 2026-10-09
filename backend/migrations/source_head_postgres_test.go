package migrations_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
)

// A definition for dedicated real-store acceptance. Without both operator
// opt-in and the exact dedicated loopback DSN it skips before creating a DB.
func TestSourceHeadPostgresPublicationEpochSupersessionAndRollback(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db := openIsolatedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	const version = "pre/0113_operation_source_heads"
	files := migrationFilesThrough(t, version)
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatal(err)
	}
	// Empty-only rollback remains usable before positive-epoch authority exists.
	if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
		t.Fatal(err)
	}
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatal(err)
	}
	if _, err := infra.ApplyMigrations(db, migrationFilesThrough(t, "pre/0114_operation_source_configuration"), "pre"); err != nil {
		t.Fatal(err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "post"); err != nil {
		t.Fatal(err)
	}
	service := operations.NewService(operations.NewGormRepository(db))
	start := operations.SourceObservationStart{OwnerUserID: "source-head-pg-owner", WorkspaceID: "local", OriginID: uuid.New(), ConfigDigest: strings.Repeat("a", 64)}
	input := func(revision string) operations.NewOperationInput {
		return operations.NewOperationInput{OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID, AccountFeedID: &start.OriginID,
			Title: "real source head acceptance", OperationType: "review_source_item", SourceType: "local_json_file", DedupeKey: "head-" + revision,
			SourceProvider: "local", SourceAccount: "account", SourceExternalID: "record", SourceRevisionHash: revision, EvidenceJSON: "{}"}
	}
	observe := func(revision string) (operations.IngestResult, operations.SourceObservation, error) {
		var result operations.IngestResult
		var observation operations.SourceObservation
		err := service.WithSourceObservation(ctx, start, func(observed context.Context) error {
			var ok bool
			observation, ok = operations.CurrentSourceObservation(observed)
			if !ok {
				return errors.New("missing private observation")
			}
			var err error
			result, err = service.IngestContext(observed, input(revision))
			return err
		})
		return result, observation, err
	}
	running := func(op models.Operation) (models.Operation, operations.ExecutionClaim) {
		t.Helper()
		op.RiskLevel, op.AutonomyLevel, op.OwnerType = string(operations.RiskLow), string(operations.AutonomyAuto), string(operations.OwnerHAI)
		op.CurrentDecision, op.RequiresApproval = string(operations.DecisionRunSafeLocalWorker), false
		classified, err := service.Transition(op, operations.StatusClassified, "hai", "", "prepare actual store test")
		if err != nil {
			t.Fatal(err)
		}
		ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready actual store test")
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := service.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
		if err != nil || claimed == nil {
			t.Fatalf("actual claim: %+v / %v", claimed, err)
		}
		ready.RuntimeID, ready.VerificationStatus = "hai-local-safe-worker", string(operations.VerificationPending)
		current, err := service.TransitionClaimed(ctx, claimed.Claim, *ready, operations.StatusRunning, "hai", "", "persist running intent")
		if err != nil {
			t.Fatal(err)
		}
		return *current, claimed.Claim
	}
	a, first, err := observe("A")
	if err != nil || !a.Created || first.ConfigEpoch != 1 {
		t.Fatalf("first acceptance: %+v / %+v / %v", a, first, err)
	}
	opA, claimA := running(a.Operation)
	calls := 0
	err = service.WithClaimedSafeEffect(ctx, claimA, opA, func(effect context.Context) error {
		calls++
		scope, ok := operations.CurrentSafeEffectScope(effect)
		if !ok || scope.SourceHeadGeneration != first.Generation || scope.SourceConfigEpoch != first.ConfigEpoch {
			return errors.New("actual effect scope lost source authority")
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("healthy actual source authority: calls=%d / %v", calls, err)
	}
	b, second, err := observe("B")
	if err != nil || !b.Created || second.Generation <= first.Generation {
		t.Fatalf("second acceptance: %+v / %+v / %v", b, second, err)
	}
	calls = 0
	err = service.WithClaimedSafeEffect(ctx, claimA, opA, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, operations.ErrSourceHeadSuperseded) || calls != 0 {
		t.Fatalf("old actual operation dispatched: calls=%d / %v", calls, err)
	}
	opB, claimB := running(b.Operation)
	start.ConfigDigest = strings.Repeat("b", 64)
	changed, err := operations.NewGormRepository(db).BeginSourceObservation(ctx, start)
	if err != nil || changed.ConfigEpoch != 2 {
		t.Fatalf("configuration change: %+v / %v", changed, err)
	}
	err = service.WithClaimedSafeEffect(ctx, claimB, opB, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, operations.ErrSourceHeadSuperseded) || calls != 0 {
		t.Fatalf("old epoch dispatched: calls=%d / %v", calls, err)
	}
	start.ConfigDigest = strings.Repeat("a", 64)
	returned, err := operations.NewGormRepository(db).BeginSourceObservation(ctx, start)
	if err != nil || returned.ConfigEpoch != 3 {
		t.Fatalf("configuration A-B-A reset epoch: %+v / %v", returned, err)
	}
	repeated, _, err := observe("A")
	if !errors.Is(err, operations.ErrSourceHeadReconciliation) || repeated.Operation.ID != uuid.Nil {
		t.Fatalf("semantic A-B-A resurrected old authority: %+v / %v", repeated, err)
	}
	var head operations.SourceHead
	if err := db.Where("owner_user_id = ? AND workspace_id = ? AND source_identity_hash = ?", opA.OwnerUserID, opA.WorkspaceID, opA.SourceIdentityHash).First(&head).Error; err != nil || head.State != "reconciliation_required" {
		t.Fatalf("actual A-B-A head: %+v / %v", head, err)
	}
	// Runtime needs 0114, but 0113's data guard must be tested at the real tail.
	// These origins are deliberately unmanaged, so removing 0114 preserves them.
	if err := infra.RollbackMigration(db, migrations.Files, "pre", "pre/0114_operation_source_configuration"); err != nil {
		t.Fatalf("rollback unmanaged configuration prerequisite before head rollback: %v", err)
	}
	err = infra.RollbackMigration(db, files, "pre", version)
	assertSourceObservationSQLState(t, err, "55000")
	var appliedHead int64
	if err := db.Raw(`SELECT count(*) FROM public.schema_migrations WHERE version = ?`, version).Row().Scan(&appliedHead); err != nil || appliedHead != 1 {
		t.Fatalf("refused populated head rollback changed its ledger: %d / %v", appliedHead, err)
	}
	for _, statement := range []string{
		"DELETE FROM public.operation_source_heads", "DELETE FROM public.operation_source_origins", "DELETE FROM public.operation_source_head_revisions",
		"UPDATE public.operation_source_heads SET observation_generation = observation_generation",
		"UPDATE public.operation_source_origins SET config_epoch = config_epoch + 1",
	} {
		assertSourceObservationSQLState(t, db.Exec(statement).Error, "23000")
	}
	var retained int64
	if err := db.Model(&operations.SourceHead{}).Where("owner_user_id = ?", start.OwnerUserID).Count(&retained).Error; err != nil || retained != 1 {
		t.Fatalf("refused mutations/rollback erased authority: %d / %v", retained, err)
	}
}

package workflow

import (
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresReminderRevocationAfterPreparationExpiryAndSourceClosure(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open dedicated PostgreSQL test database failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("canonical test migrations: %v", err)
	}
	repo := &GormRepository{DB: db}
	owner := "reminder-revocation-" + uuid.NewString()
	f := newHistoricalReminderFixtureForOwner(t, owner, time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond))
	// Commit synthetic setup before delivery: its atomic boundary must own the
	// root transaction, not report success from a caller-owned savepoint.
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, value := range []any{&f.source.Workflow, &f.source.Reminder, &f.activation, &f.approval, &f.authorization} {
			if err := tx.Create(value).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.WorkflowChecklistItem{}).Where("id = ?", f.source.Reminder.ID).Update("status", "completed").Error
	}); err != nil {
		t.Fatalf("seed historical reminder ledger and closed source: %v", err)
	}

	wanted := models.WorkflowReminderActivationDecision{
		ID: uuid.New(), ActivationRequestID: f.activation.ID, OwnerIdentity: owner, Decision: ReminderActivationDecisionRevoked,
		Reason: "Cancel the historical internal reminder.", Actor: owner, Confirmation: ReminderActivationRevokeConfirmation,
		ActivationRequestDigest: f.activation.RecordDigest, PreviousDecisionID: &f.approval.ID,
		Authority: ReminderActivationDecisionAuthority, DecidedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	wanted.RequestDigest, err = digestReminderActivationPayload(f.revokeRequest())
	if err != nil {
		t.Fatal(err)
	}
	wanted.RecordDigest, err = digestReminderActivationDecision(&wanted)
	if err != nil {
		t.Fatal(err)
	}
	stale := wanted
	stale.ID = uuid.New()
	stalePrevious := uuid.New()
	stale.PreviousDecisionID = &stalePrevious
	stale.RequestDigest, _ = digestReminderActivationPayload(stale.PreviousDecisionID)
	stale.RecordDigest, _ = digestReminderActivationDecision(&stale)
	if _, _, err := repo.SaveReminderActivationDecision(&stale); err == nil {
		t.Fatal("repository accepted a stale approval chain")
	}
	stored, created, err := repo.SaveReminderActivationDecision(&wanted)
	if err != nil || !created || stored == nil || stored.ID != wanted.ID {
		t.Fatalf("late revocation of closed source failed: stored=%#v created=%t err=%v", stored, created, err)
	}
	replay, created, err := repo.SaveReminderActivationDecision(&wanted)
	if err != nil || created || replay == nil || replay.ID != wanted.ID {
		t.Fatalf("late revocation replay failed: stored=%#v created=%t err=%v", replay, created, err)
	}
	sink := &reminderDeliverySinkSpy{}
	configured, err := WithReminderDeliverySink(NewService(repo), sink)
	if err != nil {
		t.Fatal(err)
	}
	run, err := configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner(owner, RunDueRequest{Limit: 10})
	if err != nil || run.Suppressed != 1 || len(sink.deliveries) != 0 {
		t.Fatalf("revoked authorization was delivered: summary=%#v sink=%d err=%v", run, len(sink.deliveries), err)
	}
	attempts, err := repo.ListReminderDeliveryAttemptsForOwner(owner, 10)
	if err != nil || len(attempts) != 1 || attempts[0].AuthorizationID != f.authorization.ID || attempts[0].Status != ReminderDeliveryStatusSuppressed {
		t.Fatalf("revoked authorization lacks a committed suppression receipt: attempts=%#v err=%v", attempts, err)
	}
}

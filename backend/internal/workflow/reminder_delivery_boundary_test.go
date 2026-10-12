package workflow

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This marker exists only in tests. Database repositories never opt out.
func (*reminderDeliveryFakeRepo) reminderDeliveryMemoryOnly() {}

type unmarkedReminderDeliveryRepository struct {
	Repository
	reminderActivationRepository
	reminderDeliveryRepository
}

func TestReminderDeliveryRejectsUnmarkedNontransactionalRepository(t *testing.T) {
	f := newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute))
	memory := newReminderDeliveryFakeRepo()
	f.seed(memory)
	repo := &unmarkedReminderDeliveryRepository{Repository: memory, reminderActivationRepository: memory, reminderDeliveryRepository: memory}
	sink := &reminderDeliverySinkSpy{}
	configured, err := WithReminderDeliverySink(NewService(repo), sink)
	if err != nil {
		t.Fatal(err)
	}
	result, err := configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err == nil || result != nil || len(sink.deliveries) != 0 || len(memory.attempts) != 0 {
		t.Fatalf("unmarked repository used unsafe fallback: result=%#v err=%v calls=%d", result, err, len(sink.deliveries))
	}
}

var _ transactionalReminderDeliveryRepository = (*GormRepository)(nil)
var _ TransactionalReminderDeliverySink = (*ProactivityReminderDeliverySink)(nil)

type nonPostgresReminderDialect struct{ gorm.Dialector }

func (nonPostgresReminderDialect) Name() string { return "sqlite" }

func TestReminderDeliveryRootGuardRejectsUnsafeDatabaseContexts(t *testing.T) {
	pool := &sql.DB{}
	dialect := postgres.New(postgres.Config{})
	for _, test := range []struct {
		name string
		db   *gorm.DB
	}{
		{"missing_database", nil},
		{"missing_config", &gorm.DB{}},
		{"missing_dialect", &gorm.DB{Config: &gorm.Config{}}},
		{"non_postgres_dialect", &gorm.DB{Config: &gorm.Config{Dialector: nonPostgresReminderDialect{dialect}}, Statement: &gorm.Statement{ConnPool: pool}}},
		{"missing_statement", &gorm.DB{Config: &gorm.Config{Dialector: dialect}}},
		{"missing_pool", &gorm.DB{Config: &gorm.Config{Dialector: dialect}, Statement: &gorm.Statement{}}},
		{"nested_transaction", &gorm.DB{Config: &gorm.Config{Dialector: dialect}, Statement: &gorm.Statement{ConnPool: &sql.Tx{}}}},
		{"dry_run", &gorm.DB{Config: &gorm.Config{Dialector: dialect, DryRun: true}, Statement: &gorm.Statement{ConnPool: pool}}},
		{"savepoints_disabled", &gorm.DB{Config: &gorm.Config{Dialector: dialect, DisableNestedTransaction: true}, Statement: &gorm.Statement{ConnPool: pool}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &GormRepository{DB: test.db}
			sink := &reminderDeliverySinkSpy{}
			candidate := reminderDeliveryCandidate{Authorization: models.WorkflowReminderDeliveryAuthorization{ID: uuid.New(), OwnerIdentity: "alice"}}
			if result, err := repo.ProcessReminderDelivery(candidate, sink); err == nil || result != nil || len(sink.deliveries) != 0 {
				t.Fatalf("unsafe database context accepted: result=%#v err=%v calls=%d", result, err, len(sink.deliveries))
			}
			if value, err := repo.withReminderDeliveryContext(context.Background()); value != nil || !errors.Is(err, ErrReminderDeliveryContextUnavailable) {
				t.Fatal("contextual storage admitted an unsafe root")
			}
		})
	}
}

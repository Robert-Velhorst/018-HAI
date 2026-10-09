package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/internal/proactivity"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type atomicReminderTestSink struct {
	base              TransactionalReminderDeliverySink
	calls             atomic.Int32
	fail              bool
	wait              bool
	entered           chan<- struct{}
	release           <-chan struct{}
	isolation         string
	cancelAfterSignal context.CancelFunc
}

func (s *atomicReminderTestSink) DeliverInternalReminder(ctx context.Context, envelope ReminderDeliveryEnvelope) error {
	s.calls.Add(1)
	return s.base.DeliverInternalReminder(ctx, envelope)
}

func (s *atomicReminderTestSink) DeliverInternalReminderInTransaction(ctx context.Context, tx *gorm.DB, envelope ReminderDeliveryEnvelope) error {
	s.calls.Add(1)
	if s.isolation != "" {
		var isolation string
		if err := tx.Raw("SHOW transaction_isolation").Scan(&isolation).Error; err != nil {
			return err
		}
		if isolation != s.isolation {
			return errors.New("unexpected reminder transaction isolation")
		}
	}
	if s.entered != nil {
		s.entered <- struct{}{}
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := s.base.DeliverInternalReminderInTransaction(ctx, tx, envelope); err != nil {
		return err
	}
	if s.cancelAfterSignal != nil {
		s.cancelAfterSignal()
		return nil
	}
	if s.fail {
		// Deliberately abort PostgreSQL after the real sink has written. The
		// savepoint must remove all signal rows and recover for a failure receipt.
		return tx.Exec("SELECT 1 / 0").Error
	}
	if s.wait {
		waitForReminderTestExpiry(envelope.Authorization.ExpiresAt)
	}
	return nil
}

func waitForReminderTestExpiry(expires time.Time) {
	// A duration sleep alone is not proof of wall-clock expiry in a VM.
	deadline := expires.Add(50 * time.Millisecond)
	for time.Now().UTC().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

type barrierReminderRepository struct {
	*GormRepository
	ready   chan<- struct{}
	release <-chan struct{}
}

func (r *barrierReminderRepository) FindDueReminderDeliveryAuthorizations(owner string, now time.Time, limit, maxAttempts int) ([]reminderDeliveryCandidate, error) {
	candidates, err := r.GormRepository.FindDueReminderDeliveryAuthorizations(owner, now, limit, maxAttempts)
	r.ready <- struct{}{}
	<-r.release
	return candidates, err
}

// Uses only the exact guarded disposable database, with canonical migrations
// and append-only synthetic fixtures. No application defaults or truncation.
func TestPostgresReminderDeliveryAtomicReplay(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open dedicated PostgreSQL test database failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = pool.Close() })
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("canonical test migrations: %v", err)
	}
	base := NewProactivityReminderDeliverySink(proactivity.NewService(proactivity.NewPostgresRepository(db))).(TransactionalReminderDeliverySink)

	seed := func(t *testing.T, mutations ...func(*historicalReminderFixture)) historicalReminderFixture {
		t.Helper()
		f := newHistoricalReminderFixtureForOwner(t, "atomic-reminder-"+uuid.NewString(), time.Now().UTC().Add(-3*time.Minute).Truncate(time.Microsecond))
		for _, mutate := range mutations {
			mutate(&f)
		}
		f.authorization.RecordDigest = ""
		f.authorization.RecordDigest, err = digestReminderActivationPayload(&f.authorization)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			for _, value := range []any{&f.source.Workflow, &f.source.Reminder, &f.activation, &f.approval, &f.authorization} {
				if err := tx.Create(value).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("seed reminder: %v", err)
		}
		return f
	}
	assertCounts := func(t *testing.T, f historicalReminderFixture, signals, receipts int64) {
		t.Helper()
		for _, check := range []struct {
			table string
			where string
			value any
			want  int64
		}{
			{"proactivity_signal_records", "owner_identity = ?", f.authorization.OwnerIdentity, signals},
			{"proactivity_signal_batches", "owner_identity = ?", f.authorization.OwnerIdentity, signals},
			{"proactivity_idempotency", "owner_identity = ?", f.authorization.OwnerIdentity, signals},
			{"workflow_reminder_delivery_attempts", "authorization_id = ?", f.authorization.ID, receipts},
		} {
			var count int64
			if err := db.Table(check.table).Where(check.where, check.value).Count(&count).Error; err != nil || count != check.want {
				t.Fatalf("%s count=%d want=%d err=%v", check.table, count, check.want, err)
			}
		}
	}
	run := func(t *testing.T, f historicalReminderFixture, sink ReminderDeliverySink) (*ReminderDeliveryRunSummary, error) {
		t.Helper()
		configured, err := WithReminderDeliverySink(NewService(&GormRepository{DB: db}), sink)
		if err != nil {
			t.Fatal(err)
		}
		return configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner(f.authorization.OwnerIdentity, RunDueRequest{Limit: 10})
	}
	waitForLock := func(t *testing.T, table string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var blocked int64
			if err := db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE ?", "%"+table+"%").Scan(&blocked).Error; err != nil {
				t.Fatal(err)
			}
			if blocked > 0 {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("worker did not actually block on %s", table)
	}
	workers := func(t *testing.T, f historicalReminderFixture, sink ReminderDeliverySink) int {
		t.Helper()
		const count = 8
		ready, release := make(chan struct{}, count), make(chan struct{})
		type workerResult struct {
			value *ReminderDeliveryRunSummary
			err   error
		}
		results := make(chan workerResult, count)
		for i := 0; i < count; i++ {
			go func() {
				repo := &barrierReminderRepository{GormRepository: &GormRepository{DB: db}, ready: ready, release: release}
				configured, err := WithReminderDeliverySink(NewService(repo), sink)
				if err != nil {
					results <- workerResult{err: err}
					return
				}
				value, err := configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner(f.authorization.OwnerIdentity, RunDueRequest{Limit: 10})
				results <- workerResult{value, err}
			}()
		}
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		for i := 0; i < count; i++ {
			select {
			case <-ready:
			case <-deadline.C:
				close(release)
				t.Fatal("workers did not reach the shared stale selection")
			}
		}
		close(release)
		delivered := 0
		for i := 0; i < count; i++ {
			select {
			case result := <-results:
				if result.err != nil {
					t.Fatal(result.err)
				}
				delivered += result.value.Delivered
			case <-deadline.C:
				t.Fatal("concurrent reminder workers did not complete")
			}
		}
		return delivered
	}

	for _, boundary := range []string{"before_delivery", "after_signal_write"} {
		t.Run("context_cancel_"+boundary, func(t *testing.T) {
			f := seed(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sink := &atomicReminderTestSink{base: base}
			if boundary == "before_delivery" {
				cancel()
			} else {
				sink.cancelAfterSignal = cancel
			}
			configured, err := WithReminderDeliverySink(NewService(&GormRepository{DB: db}), sink)
			if err != nil {
				t.Fatal(err)
			}
			result, err := configured.(ContextualReminderDeliveryService).RunDueReminderDeliveriesForOwnerContext(ctx, f.authorization.OwnerIdentity, RunDueRequest{Limit: 10})
			if !errors.Is(err, context.Canceled) || (result != nil && (result.Checked != 0 || result.Delivered != 0)) {
				t.Fatal("canceled transaction reported delivery")
			}
			wantCalls := int32(0)
			if boundary == "after_signal_write" {
				wantCalls = 1
			}
			if sink.calls.Load() != wantCalls {
				t.Fatal("cancellation boundary was not exercised")
			}
			assertCounts(t, f, 0, 0)
			sink.cancelAfterSignal = nil
			result, err = configured.(ContextualReminderDeliveryService).RunDueReminderDeliveriesForOwnerContext(context.Background(), f.authorization.OwnerIdentity, RunDueRequest{Limit: 10})
			if err != nil || result == nil || result.Delivered != 1 {
				t.Fatal("confirmed rollback did not leave a deliverable authorization")
			}
			assertCounts(t, f, 1, 1)
		})
	}

	t.Run("concurrent_workers_and_stale_replay", func(t *testing.T) {
		f := seed(t)
		sink := &atomicReminderTestSink{base: base}
		if delivered := workers(t, f, sink); delivered != 1 || sink.calls.Load() != 1 {
			t.Fatalf("delivered=%d sink calls=%d", delivered, sink.calls.Load())
		}
		assertCounts(t, f, 1, 1)
		stale := reminderDeliveryCandidate{Authorization: f.authorization}
		outcome, err := (&GormRepository{DB: db}).ProcessReminderDelivery(stale, sink)
		if err != nil || outcome != nil || sink.calls.Load() != 1 {
			t.Fatalf("stale worker replay=%#v err=%v calls=%d", outcome, err, sink.calls.Load())
		}
	})

	t.Run("caller_owned_outer_transaction_is_rejected", func(t *testing.T) {
		f := seed(t)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		sink := &atomicReminderTestSink{base: base}
		outcome, err := (&GormRepository{DB: tx}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: f.authorization}, sink)
		if err == nil || outcome != nil || sink.calls.Load() != 0 {
			t.Fatalf("uncommitted outer transaction reported delivery: %#v err=%v calls=%d", outcome, err, sink.calls.Load())
		}
		assertCounts(t, f, 0, 0)
	})

	t.Run("read_committed_overrides_session_snapshot_default", func(t *testing.T) {
		f := seed(t)
		sink := &atomicReminderTestSink{base: base, isolation: "read committed"}
		if err := db.Connection(func(connection *gorm.DB) error {
			if err := connection.Exec("SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; err != nil {
				return err
			}
			defer connection.Exec("SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL READ COMMITTED")
			outcome, err := (&GormRepository{DB: connection}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: f.authorization}, sink)
			if err != nil {
				return err
			}
			if outcome == nil || outcome.Status != ReminderDeliveryStatusDelivered || sink.calls.Load() != 1 {
				return errors.New("delivery did not use fresh per-statement authority snapshots")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		assertCounts(t, f, 1, 1)
	})

	for _, mode := range []string{"receipt_failure", "panic_after_signal", "connection_loss_after_signal"} {
		t.Run(mode, func(t *testing.T) {
			f := seed(t)
			sink := &atomicReminderTestSink{base: base}
			callback := "test:reminder:" + uuid.NewString()
			var injected atomic.Bool
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				attempt, ok := tx.Statement.Dest.(*models.WorkflowReminderDeliveryAttempt)
				if !ok || attempt.AuthorizationID != f.authorization.ID || !injected.CompareAndSwap(false, true) {
					return
				}
				if mode == "panic_after_signal" {
					panic("injected interruption after signal before receipt")
				}
				if mode == "connection_loss_after_signal" {
					var pid int
					if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						tx.AddError(err)
						return
					}
					// Terminate only the PID of this owned test transaction.
					tx.AddError(db.Exec("SELECT pg_terminate_backend(?)", pid).Error)
					return
				}
				tx.AddError(errors.New("injected receipt write failure"))
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			if mode == "panic_after_signal" {
				panicked := false
				func() {
					defer func() { panicked = recover() != nil }()
					_, _ = run(t, f, sink)
				}()
				if !panicked {
					t.Fatal("interruption was not injected")
				}
			} else if result, err := run(t, f, sink); err == nil || result != nil {
				t.Fatalf("receipt failure reported success: %#v err=%v", result, err)
			}
			if !injected.Load() || sink.calls.Load() != 1 {
				t.Fatalf("fault did not occur after real signal: injected=%t calls=%d", injected.Load(), sink.calls.Load())
			}
			assertCounts(t, f, 0, 0)
			if result, err := run(t, f, sink); err != nil || result.Delivered != 1 {
				t.Fatalf("fresh service replay failed: %#v err=%v", result, err)
			}
			assertCounts(t, f, 1, 1)
		})
	}

	t.Run("partial_sink_failure_has_bounded_concurrent_retries", func(t *testing.T) {
		f := seed(t)
		sink := &atomicReminderTestSink{base: base, fail: true}
		if delivered := workers(t, f, sink); delivered != 0 {
			t.Fatalf("failed signal reported delivered: %d", delivered)
		}
		for i := 0; i < ReminderDeliveryMaxAttempts; i++ {
			if _, err := run(t, f, sink); err != nil {
				t.Fatal(err)
			}
		}
		assertCounts(t, f, 0, ReminderDeliveryMaxAttempts)
		if sink.calls.Load() != ReminderDeliveryMaxAttempts {
			t.Fatalf("retry budget exceeded or skipped: calls=%d", sink.calls.Load())
		}
		attempts, err := (&GormRepository{DB: db}).ListReminderDeliveryAttemptsForOwner(f.authorization.OwnerIdentity, 10)
		if err != nil || len(attempts) != 3 || attempts[0].Status != ReminderDeliveryStatusDeadLettered {
			t.Fatalf("missing terminal bounded retry receipt: %#v err=%v", attempts, err)
		}
		for _, attempt := range attempts {
			if attempt.AttemptNumber < 1 || attempt.AttemptNumber > 3 {
				t.Fatalf("invalid attempt number: %d", attempt.AttemptNumber)
			}
		}
	})

	t.Run("legacy_signal_without_receipt_replays_exact_key", func(t *testing.T) {
		f := seed(t)
		if err := base.DeliverInternalReminder(context.Background(), ReminderDeliveryEnvelope{Authorization: f.authorization, Source: f.source}); err != nil {
			t.Fatal(err)
		}
		assertCounts(t, f, 1, 0)
		if result, err := run(t, f, base); err != nil || result.Delivered != 1 {
			t.Fatalf("legacy committed signal replay failed: %#v err=%v", result, err)
		}
		assertCounts(t, f, 1, 1)
	})

	t.Run("revocation_after_selection_is_revalidated", func(t *testing.T) {
		f := seed(t)
		repo := &GormRepository{DB: db}
		candidates, err := repo.FindDueReminderDeliveryAuthorizations(f.authorization.OwnerIdentity, time.Now().UTC(), 10, 3)
		if err != nil || len(candidates) != 1 {
			t.Fatalf("select: candidates=%d err=%v", len(candidates), err)
		}
		activation := NewService(repo).(ReminderActivationService)
		if _, err := activation.DecideReminderActivationForOwner(f.authorization.OwnerIdentity, f.authorization.OwnerIdentity, f.activation.ID, f.revokeRequest()); err != nil {
			t.Fatal(err)
		}
		sink := &atomicReminderTestSink{base: base}
		outcome, err := repo.ProcessReminderDelivery(candidates[0], sink)
		if err != nil || outcome == nil || outcome.Status != ReminderDeliveryStatusSuppressed || sink.calls.Load() != 0 {
			t.Fatalf("stale revoked candidate=%#v err=%v calls=%d", outcome, err, sink.calls.Load())
		}
		assertCounts(t, f, 0, 1)
	})

	t.Run("nontransactional_sink_never_called", func(t *testing.T) {
		f := seed(t)
		sink := &reminderDeliverySinkSpy{}
		if result, err := run(t, f, sink); err != nil || result.Retried != 1 || len(sink.deliveries) != 0 {
			t.Fatalf("unsafe sink called: result=%#v err=%v calls=%d", result, err, len(sink.deliveries))
		}
		assertCounts(t, f, 0, 1)
	})

	t.Run("locked_authorization_is_skipped_without_receipt", func(t *testing.T) {
		f := seed(t)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Exec("SELECT id FROM workflow_reminder_delivery_authorizations WHERE id = ? FOR UPDATE", f.authorization.ID).Error; err != nil {
			t.Fatal(err)
		}
		sink := &atomicReminderTestSink{base: base}
		result, err := run(t, f, sink)
		if err != nil || result.Checked != 0 || sink.calls.Load() != 0 {
			t.Fatalf("locked authorization was not skipped: %#v err=%v calls=%d", result, err, sink.calls.Load())
		}
		assertCounts(t, f, 0, 0)
	})

	t.Run("expiry_during_signal_write_rolls_back_effect", func(t *testing.T) {
		f := seed(t, func(f *historicalReminderFixture) {
			f.authorization.ExpiresAt = time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
		})
		sink := &atomicReminderTestSink{base: base, wait: true}
		result, err := run(t, f, sink)
		if err != nil || result.Expired != 1 || sink.calls.Load() != 1 {
			t.Fatalf("late signal not rolled back: result=%#v err=%v calls=%d", result, err, sink.calls.Load())
		}
		assertCounts(t, f, 0, 1)
	})

	t.Run("revalidation_locks_held_through_signal_and_receipt", func(t *testing.T) {
		f := seed(t)
		entered, release := make(chan struct{}, 1), make(chan struct{}, 1)
		sink := &atomicReminderTestSink{base: base, entered: entered, release: release}
		done := make(chan error, 1)
		go func() {
			_, err := (&GormRepository{DB: db}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: f.authorization}, sink)
			done <- err
		}()
		defer close(release)
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("sink was not reached")
		}
		for _, check := range []struct {
			table string
			id    uuid.UUID
		}{
			{"workflow_reminder_activation_requests", f.activation.ID},
			{"workflow_checklist_items", f.source.Reminder.ID},
			{"workflow_items", f.source.Workflow.ID},
		} {
			err := db.Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM "+check.table+" WHERE id = ? FOR UPDATE NOWAIT", check.id).Error
			})
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) || pgError.Code != "55P03" {
				t.Fatalf("%s revalidation lock was released before receipt: %v", check.table, err)
			}
		}
		// Release after checking all locks; leave deferred cleanup nonblocking.
		release <- struct{}{}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("locked delivery did not finish")
		}
		assertCounts(t, f, 1, 1)
	})

	for _, table := range []string{"workflow_reminder_activation_requests", "workflow_checklist_items"} {
		t.Run("expiry_after_waiting_for_"+table, func(t *testing.T) {
			f := seed(t, func(f *historicalReminderFixture) {
				f.authorization.ExpiresAt = time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
			})
			id := f.activation.ID
			if table == "workflow_checklist_items" {
				id = f.source.Reminder.ID
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := tx.Exec("SELECT id FROM "+table+" WHERE id = ? FOR UPDATE", id).Error; err != nil {
				t.Fatal(err)
			}
			sink := &atomicReminderTestSink{base: base}
			type result struct {
				outcome *ReminderDeliveryRunResult
				err     error
			}
			done := make(chan result, 1)
			go func() {
				outcome, err := (&GormRepository{DB: db}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: f.authorization}, sink)
				done <- result{outcome, err}
			}()
			waitForLock(t, table)
			if !f.authorization.ExpiresAt.After(time.Now().UTC()) {
				t.Fatal("worker did not enter the lock wait before expiry")
			}
			waitForReminderTestExpiry(f.authorization.ExpiresAt)
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil || result.outcome == nil || result.outcome.Status != ReminderDeliveryStatusExpired || sink.calls.Load() != 0 {
					t.Fatalf("expired authority dispatched after lock wait: %#v err=%v calls=%d", result.outcome, result.err, sink.calls.Load())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("revalidation lock wait did not finish")
			}
			assertCounts(t, f, 0, 1)
		})
	}

	t.Run("concurrent_source_update_is_revalidated_after_lock_wait", func(t *testing.T) {
		f := seed(t)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Model(&models.WorkflowChecklistItem{}).Where("id = ?", f.source.Reminder.ID).Update("label", "Changed under a concurrent transaction").Error; err != nil {
			t.Fatal(err)
		}
		sink := &atomicReminderTestSink{base: base}
		type result struct {
			outcome *ReminderDeliveryRunResult
			err     error
		}
		done := make(chan result, 1)
		go func() {
			outcome, err := (&GormRepository{DB: db}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: f.authorization}, sink)
			done <- result{outcome, err}
		}()
		waitForLock(t, "workflow_checklist_items")
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-done:
			if result.err != nil || result.outcome == nil || result.outcome.Status != ReminderDeliveryStatusSuppressed || sink.calls.Load() != 0 {
				t.Fatalf("concurrently changed source dispatched: %#v err=%v calls=%d", result.outcome, result.err, sink.calls.Load())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("source-update lock wait did not finish")
		}
		assertCounts(t, f, 0, 1)
	})
}

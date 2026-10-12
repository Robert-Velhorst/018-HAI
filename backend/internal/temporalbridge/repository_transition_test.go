package temporalbridge

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise the actual GORM update builder with an unconnected, dry-run pool.
// This proves query shape/result mapping, not real PostgreSQL locking/durability.
func temporalTransitionDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: &sql.DB{}}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestScheduleTransitionUsesOneAtomicProvenanceAndStatePredicate(t *testing.T) {
	db := temporalTransitionDryRunDB(t)
	var statement *gorm.Statement
	if err := db.Callback().Update().After("gorm:update").Register("schedule:capture", func(tx *gorm.DB) { statement = tx.Statement }); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	run := models.TemporalWorkflowRun{ID: uuid.New(), OwnerIdentity: "owner@example.test", TemporalWorkflowID: "test-workflow", WorkflowType: followUpWorkflowType, ScheduledFor: now.Add(time.Hour)}
	changed, err := NewGormRepository(db).TransitionSchedule(context.Background(), run, "dispatching", "scheduled", "accepted", now)
	if err != nil || changed || statement == nil {
		t.Fatalf("dry-run transition: changed=%t err=%v statement=%v", changed, err, statement)
	}
	query := strings.Join(strings.Fields(statement.SQL.String()), " ")
	want := `UPDATE "temporal_workflow_runs" SET "status"=$1,"summary"=$2,"updated_at"=$3 WHERE id = $4 AND owner_identity = $5 AND temporal_workflow_id = $6 AND workflow_type = $7 AND scheduled_for = $8 AND status = $9 AND started_at IS NULL AND completed_at IS NULL`
	if query != want {
		t.Fatalf("unfenced or excessive schedule mutation: %s", query)
	}
	values := []any{"scheduled", "accepted", now, run.ID, run.OwnerIdentity, run.TemporalWorkflowID, run.WorkflowType, run.ScheduledFor, "dispatching"}
	if !reflect.DeepEqual(statement.Vars, values) {
		t.Fatalf("wrong transition identity/intent: %#v", statement.Vars)
	}
}

func TestScheduleTransitionOnlyReportsAConfirmedSingleRow(t *testing.T) {
	wantErr := errors.New("synthetic write failure")
	for _, tc := range []struct {
		name    string
		rows    int64
		err     error
		changed bool
	}{
		{"conflict", 0, nil, false}, {"confirmed", 1, nil, true},
		{"too many rows", 2, nil, false}, {"error with row", 1, wantErr, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := temporalTransitionDryRunDB(t)
			if err := db.Callback().Update().After("gorm:update").Register("schedule:result", func(tx *gorm.DB) {
				tx.RowsAffected = tc.rows
				if tc.err != nil {
					tx.AddError(tc.err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			run := models.TemporalWorkflowRun{ID: uuid.New(), OwnerIdentity: "owner@example.test", TemporalWorkflowID: "workflow", WorkflowType: followUpWorkflowType}
			changed, err := NewGormRepository(db).TransitionSchedule(context.Background(), run, "preparing", "dispatching", "dispatching", time.Now())
			if changed != tc.changed || !errors.Is(err, tc.err) {
				t.Fatalf("mutation receipt: %t/%v", changed, err)
			}
		})
	}
}

func TestScheduleTransitionRejectsInvalidOrExecutableStatesWithoutSQL(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"preparing", "scheduled"}, {"running", "scheduled"}, {"completed", "failed"}, {"schedule_uncertain", "dispatching"}, {"failed", "dispatching"},
	} {
		t.Run(tc.from+"-"+tc.to, func(t *testing.T) {
			db := temporalTransitionDryRunDB(t)
			called := false
			if err := db.Callback().Update().After("gorm:update").Register("schedule:never", func(*gorm.DB) { called = true }); err != nil {
				t.Fatal(err)
			}
			run := models.TemporalWorkflowRun{ID: uuid.New(), OwnerIdentity: "owner@example.test", TemporalWorkflowID: "workflow", WorkflowType: followUpWorkflowType}
			changed, err := NewGormRepository(db).TransitionSchedule(context.Background(), run, tc.from, tc.to, "not allowed", time.Now())
			if changed || err == nil || called {
				t.Fatalf("invalid state mutation reached SQL: %t/%v called=%t", changed, err, called)
			}
		})
	}
}

package temporalbridge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func activityTransitionFixture() (models.TemporalWorkflowRun, models.TemporalWorkflowRun) {
	at := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	expected := models.TemporalWorkflowRun{ID: uuid.New(), OwnerIdentity: "owner@example.test", TemporalWorkflowID: "test-workflow",
		WorkflowType: followUpWorkflowType, Status: "scheduled", ScheduledFor: at, UpdatedAt: at, ResultJSON: "{}"}
	next := expected
	next.Status, next.StartedAt, next.UpdatedAt, next.Summary = "running", &at, at, "claimed"
	return expected, next
}

func TestActivityTransitionBuildsFencedUpdateNotInsertOrWholeRowSave(t *testing.T) {
	for _, settle := range []bool{false, true} {
		db := temporalTransitionDryRunDB(t)
		var statement *gorm.Statement
		if err := db.Callback().Update().After("gorm:update").Register("activity:capture", func(tx *gorm.DB) { statement = tx.Statement }); err != nil {
			t.Fatal(err)
		}
		expected, next := activityTransitionFixture()
		if settle {
			expected = next
			next.Status, next.Summary, next.ResultJSON = "completed", "completed", `{"checked":1,"triggered":1}`
			next.UpdatedAt = next.UpdatedAt.Add(time.Second)
			next.CompletedAt = &next.UpdatedAt
		}
		changed, err := NewGormRepository(db).TransitionActivity(context.Background(), expected, next)
		if err != nil || changed || statement == nil {
			t.Fatalf("dry-run activity transition: %t/%v statement=%v", changed, err, statement)
		}
		query := strings.Join(strings.Fields(statement.SQL.String()), " ")
		want := `UPDATE "temporal_workflow_runs" SET "completed_at"=$1,"result_json"=$2,"started_at"=$3,"status"=$4,"summary"=$5,"updated_at"=$6 WHERE (id = $7 AND owner_identity = $8 AND temporal_workflow_id = $9 AND workflow_type = $10 AND scheduled_for = $11 AND status = $12 AND updated_at = $13 AND completed_at IS NULL) AND `
		values := []any{next.CompletedAt, next.ResultJSON, next.StartedAt, next.Status, next.Summary, next.UpdatedAt,
			expected.ID, expected.OwnerIdentity, expected.TemporalWorkflowID, expected.WorkflowType, expected.ScheduledFor, expected.Status, expected.UpdatedAt}
		if settle {
			want += "started_at = $14"
			values = append(values, *expected.StartedAt)
		} else {
			want += "started_at IS NULL"
		}
		if query != want || len(statement.Vars) != len(values) {
			t.Fatalf("activity SQL lost provenance/claim fence or rewrites identity: %s", query)
		}
		for i, got := range statement.Vars {
			if !reflect.DeepEqual(got, values[i]) {
				t.Errorf("activity SQL argument %d: got %T %#v; want %T %#v", i, got, got, values[i], values[i])
			}
		}
	}
}

func TestActivityTransitionRejectsInvalidClaimsAndSettlementBeforeSQL(t *testing.T) {
	for name, change := range map[string]func(*models.TemporalWorkflowRun, *models.TemporalWorkflowRun){
		"missing ID":         func(expected, next *models.TemporalWorkflowRun) { expected.ID, next.ID = uuid.Nil, uuid.Nil },
		"missing owner":      func(expected, next *models.TemporalWorkflowRun) { expected.OwnerIdentity, next.OwnerIdentity = "", "" },
		"different owner":    func(_, next *models.TemporalWorkflowRun) { next.OwnerIdentity = "another@example.test" },
		"different workflow": func(_, next *models.TemporalWorkflowRun) { next.TemporalWorkflowID = "other-workflow" },
		"different type":     func(_, next *models.TemporalWorkflowRun) { next.WorkflowType = "other-type" },
		"different schedule": func(_, next *models.TemporalWorkflowRun) { next.ScheduledFor = next.ScheduledFor.Add(time.Hour) },
		"preparing":          func(expected, _ *models.TemporalWorkflowRun) { expected.Status = "preparing" },
		"reclaim running": func(expected, next *models.TemporalWorkflowRun) {
			expected.Status, expected.StartedAt = "running", next.StartedAt
		},
		"reclaim failed": func(expected, next *models.TemporalWorkflowRun) {
			expected.Status, expected.StartedAt = "failed", next.StartedAt
		},
		"missing start":     func(_, next *models.TemporalWorkflowRun) { next.StartedAt = nil },
		"missing freshness": func(_, next *models.TemporalWorkflowRun) { next.UpdatedAt = time.Time{} },
		"completed replay mutation": func(expected, next *models.TemporalWorkflowRun) {
			expected.Status, expected.StartedAt, expected.CompletedAt = "completed", next.StartedAt, next.StartedAt
		},
		"null completion": func(expected, next *models.TemporalWorkflowRun) {
			*expected = *next
			next.Status, next.CompletedAt, next.ResultJSON = "completed", next.StartedAt, "null"
		},
		"completion before start": func(expected, next *models.TemporalWorkflowRun) {
			*expected = *next
			before := next.StartedAt.Add(-time.Second)
			next.Status, next.CompletedAt = "completed", &before
		},
		"different start": func(expected, next *models.TemporalWorkflowRun) {
			*expected = *next
			later := next.StartedAt.Add(time.Second)
			next.Status, next.StartedAt = "failed", &later
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := temporalTransitionDryRunDB(t)
			called := false
			if err := db.Callback().Update().After("gorm:update").Register("activity:never", func(*gorm.DB) { called = true }); err != nil {
				t.Fatal(err)
			}
			expected, next := activityTransitionFixture()
			change(&expected, &next)
			changed, err := NewGormRepository(db).TransitionActivity(context.Background(), expected, next)
			if changed || err == nil || called {
				t.Fatalf("invalid claim reached SQL: %t/%v called=%t", changed, err, called)
			}
		})
	}
}

func TestActivityTransitionRequiresExactlyOneConfirmedRow(t *testing.T) {
	problem := errors.New("synthetic storage failure")
	for _, tc := range []struct {
		rows    int64
		err     error
		changed bool
	}{{0, nil, false}, {1, nil, true}, {2, nil, false}, {1, problem, false}} {
		db := temporalTransitionDryRunDB(t)
		if err := db.Callback().Update().After("gorm:update").Register("activity:result", func(tx *gorm.DB) {
			tx.RowsAffected = tc.rows
			if tc.err != nil {
				tx.AddError(tc.err)
			}
		}); err != nil {
			t.Fatal(err)
		}
		expected, next := activityTransitionFixture()
		changed, err := NewGormRepository(db).TransitionActivity(context.Background(), expected, next)
		if changed != tc.changed || !errors.Is(err, tc.err) {
			t.Fatalf("unconfirmed mutation receipt: %t/%v", changed, err)
		}
	}
}

func TestActivityRepositoryRejectsCanceledContextBeforeSQL(t *testing.T) {
	db := temporalTransitionDryRunDB(t)
	queried, mutated := false, false
	if err := db.Callback().Query().Before("gorm:query").Register("activity:query", func(*gorm.DB) { queried = true }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("activity:update", func(*gorm.DB) { mutated = true }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewGormRepository(db)
	expected, next := activityTransitionFixture()
	_, readErr := repo.FindByIDContext(ctx, expected.ID)
	changed, writeErr := repo.TransitionActivity(ctx, expected, next)
	if changed || !errors.Is(readErr, context.Canceled) || !errors.Is(writeErr, context.Canceled) || queried || mutated {
		t.Fatalf("canceled activity reached DB: %t read=%v write=%v query=%t mutation=%t", changed, readErr, writeErr, queried, mutated)
	}
}

package operations

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The existing synthetic driver checks transaction order; this is not real SQL acceptance.
func TestManagedSourceObservationPreservesLookupFailureBeforeAllocatingAuthority(t *testing.T) {
	for _, want := range []error{errors.New("synthetic origin lookup failure"), context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			state := &claimedEffectSQLState{}
			sqlDB := sql.OpenDB(claimedEffectSQLConnector{state: state})
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			queries := 0
			if err := db.Callback().Query().Before("gorm:query").Register("source-test:lookup-failure", func(tx *gorm.DB) {
				queries++
				tx.AddError(want)
			}); err != nil {
				t.Fatal(err)
			}
			observation, err := NewGormRepository(db).BeginSourceObservation(t.Context(), sourceConfigurationTestStart())
			if !errors.Is(err, want) || errors.Is(err, ErrSourceHeadSuperseded) || observation != (SourceObservation{}) || queries != 1 ||
				state.active || state.phase != 0 || state.commits != 0 || state.rollbacks != 1 {
				t.Fatalf("lookup failure was masked or allocated authority: observation=%+v err=%v queries=%d state=%+v", observation, err, queries, state)
			}
		})
	}
}

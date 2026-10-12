package durablejob

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSettlementSQLCommitFailureCannotAcknowledgeTerminalWrite(t *testing.T) {
	original := errors.New("commit acknowledgement lost")
	p := &reviewSQLPool{commitError: original}
	db := reviewProbeDB(t, p, true)
	if err := db.Callback().Update().After("gorm:update").Register("settlement-update", func(tx *gorm.DB) { tx.RowsAffected = 1 }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("settlement-existing-schedule", func(tx *gorm.DB) {
		if count, ok := tx.Statement.Dest.(*int64); ok {
			*count = 1
			tx.RowsAffected = 1
		}
	}); err != nil {
		t.Fatal(err)
	}
	r := &gormRepository{db: db}
	owned, created, err := r.CompleteRecurring(uuid.New(), "worker", 7, time.Now(), models.DurableJobSucceeded, 1, "", &models.DurableJob{Queue: "ordinary", Kind: "scan"})
	if !errors.Is(err, original) || owned || created || p.commits != 1 {
		t.Fatalf("uncommitted terminal write was acknowledged: owned=%v created=%v commits=%d err=%v", owned, created, p.commits, err)
	}
}

func TestSettlementSQLAdmissionIncludesPreparingBarrier(t *testing.T) {
	db := reviewProbeDB(t, &reviewSQLPool{}, true)
	protected := false
	if err := db.Callback().Query().After("gorm:query").Register("settlement-preparing-barrier", func(tx *gorm.DB) { protected = reviewHasValue(tx.Statement.Vars, "settling") }); err != nil {
		t.Fatal(err)
	}
	if _, err := (&gormRepository{db: db}).CountActiveByKind("scan"); err != nil || !protected {
		t.Fatal("preparing terminal settlement does not block new admission")
	}
}

func TestSettlementSQLLegacyCompletionRequiresPersistedReplayPermission(t *testing.T) {
	db := reviewProbeDB(t, &reviewSQLPool{}, true)
	query := ""
	var args []interface{}
	if err := db.Callback().Update().After("gorm:update").Register("legacy-settlement-policy", func(tx *gorm.DB) {
		query = tx.Statement.SQL.String()
		args = append([]interface{}(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := (&gormRepository{db: db}).CompleteRecurring(uuid.New(), "worker", 7, time.Now(), models.DurableJobSucceeded, 1, "", &models.DurableJob{Queue: "ordinary", Kind: "scan"})
	if err != nil {
		t.Fatal(err)
	}
	where := strings.SplitN(query, " WHERE ", 2)
	if len(where) != 2 || !strings.Contains(where[1], "replay_policy =") || !strings.Contains(where[1], "queue =") || !strings.Contains(where[1], "kind =") || !reviewHasValue(args, models.DurableJobReplayAtLeastOnce) || !reviewHasValue(args, "ordinary") || !reviewHasValue(args, "scan") {
		t.Fatal("legacy completion can bypass persisted replay authority or successor identity")
	}
}

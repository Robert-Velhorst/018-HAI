//go:build integration

package durablejob

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"gorm.io/gorm"
)

// Commit really reaches the dedicated PostgreSQL server; only its client
// acknowledgement is replaced. Never run this against a product database.
type settlementCommitPool struct {
	gorm.ConnPool
	begin              gorm.TxBeginner
	commits            int
	failAt             int
	lost               error
	cancelAfterPrepare context.CancelFunc
}

func (p *settlementCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.begin.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &settlementCommittedTx{ConnPool: tx, tx: tx, pool: p}, nil
}

type settlementCommittedTx struct {
	gorm.ConnPool
	tx   *sql.Tx
	pool *settlementCommitPool
}

func (tx *settlementCommittedTx) Commit() error {
	if err := tx.tx.Commit(); err != nil {
		return err
	}
	tx.pool.commits++
	if tx.pool.commits == tx.pool.failAt {
		return tx.pool.lost
	}
	if tx.pool.commits == 1 && tx.pool.cancelAfterPrepare != nil {
		tx.pool.cancelAfterPrepare()
	}
	return nil
}
func (tx *settlementCommittedTx) Rollback() error { return tx.tx.Rollback() }

func TestReviewSettlementPostgresLostAcknowledgementBarrier(t *testing.T) {
	for _, scenario := range []string{"success", "lost-prepare", "canceled-after-prepare", "lost-release"} {
		t.Run(scenario, func(t *testing.T) {
			repo, db := integrationRepo(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			locked := now.Add(-time.Second)
			current, err := repo.Enqueue(&models.DurableJob{Queue: "review", Kind: "ambient.scan", ReplayPolicy: models.DurableJobReviewUnknown, Status: models.DurableJobRunning, RunAt: locked, LockedAt: &locked, LockedBy: "worker", LeaseGeneration: 7, MaxAttempts: 3})
			if err != nil {
				t.Fatal(err)
			}
			ordinaryNext := &models.DurableJob{Queue: current.Queue, Kind: current.Kind, RunAt: now}
			if owned, created, err := repo.CompleteRecurring(current.ID, "worker", 7, now, models.DurableJobSucceeded, 1, "", ordinaryNext); err != nil || owned || created {
				t.Fatal("ordinary completion bypassed persisted review policy")
			}
			unchanged, err := repo.Find(current.ID)
			if err != nil || unchanged.Status != models.DurableJobRunning || unchanged.CompletedAt != nil {
				t.Fatal("refused legacy completion changed review occurrence")
			}
			root := db.Session(&gorm.Session{NewDB: true})
			begin, ok := root.Statement.ConnPool.(gorm.TxBeginner)
			if !ok {
				t.Fatal("dedicated SQL root does not support independent transaction")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lost := errors.New("simulated lost commit acknowledgement")
			pool := &settlementCommitPool{ConnPool: root.Statement.ConnPool, begin: begin, lost: lost}
			switch scenario {
			case "lost-prepare":
				pool.failAt = 1
			case "lost-release":
				pool.failAt = 2
			case "canceled-after-prepare":
				pool.cancelAfterPrepare = cancel
			}
			root.Statement.ConnPool = pool
			next := &models.DurableJob{Queue: "review", Kind: current.Kind, ReplayPolicy: models.DurableJobReviewUnknown, RunAt: now, MaxAttempts: 3}
			owned, created, err := (&gormRepository{db: root}).CompleteReviewRecurring(ctx, current.ID, "worker", 7, now, models.DurableJobSucceeded, 1, "token=must-not-leak", next)
			if scenario == "success" {
				if err != nil || !owned || !created {
					t.Fatalf("acknowledged completion refused: %v", err)
				}
			} else if err == nil || owned || created {
				t.Fatal("uncertain completion acknowledged")
			}
			if strings.HasPrefix(scenario, "lost-") && !errors.Is(err, lost) {
				t.Fatal("lost acknowledgement identity missing")
			}
			stored, err := repo.Find(current.ID)
			if err != nil {
				t.Fatal(err)
			}
			barrier := scenario == "lost-prepare" || scenario == "canceled-after-prepare"
			if barrier {
				if stored.Status != models.DurableJobSettling || stored.CompletedAt != nil || stored.LockedBy != "worker" || stored.LeaseGeneration != 7 {
					t.Fatalf("preparation lost barrier: %+v", stored)
				}
				if changed, err := repo.MarkSucceeded(current.ID, "worker", 7, now); err != nil || changed {
					t.Fatal("stale success overwrote preparation")
				}
				if reaped, err := repo.(*gormRepository).ReapExpiredLeasesForQueue("review", now.Add(time.Hour), time.Minute); err != nil || reaped != 0 {
					t.Fatal("reaper released preparation")
				}
				fresh := &models.DurableJob{Queue: "review", Kind: current.Kind, ReplayPolicy: models.DurableJobReviewUnknown, RunAt: now}
				if admitted, err := repo.(ReviewSchedulingRepository).EnqueueReviewIfNoActive(fresh); err != nil || admitted {
					t.Fatal("startup bypassed preparation")
				}
			} else if stored.Status != models.DurableJobSucceeded || stored.CompletedAt == nil || stored.LockedBy != "" || stored.LockedAt != nil {
				t.Fatal("committed release not observable")
			}
			if strings.Contains(stored.LastError, "must-not-leak") {
				t.Fatal("recognized secret retained")
			}
			var count int64
			if err := db.Model(&models.DurableJob{}).Where("queue = ? AND kind = ?", "review", current.Kind).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("successor duplicated or absent: count=%d err=%v", count, err)
			}
			child, err := repo.Find(next.ID)
			if err != nil || child.Status != models.DurableJobPending || child.ReplayPolicy != models.DurableJobReviewUnknown {
				t.Fatal("prepared successor missing")
			}
			claimed, err := repo.ClaimDue("next-worker", "review", now.Add(time.Minute), 10)
			if err != nil {
				t.Fatal(err)
			}
			if barrier && len(claimed) != 0 {
				t.Fatal("claim crossed uncertain preparation")
			}
			if !barrier && (len(claimed) != 1 || claimed[0].ID != next.ID) {
				t.Fatal("confirmed preparation release did not admit exact successor")
			}
		})
	}
}

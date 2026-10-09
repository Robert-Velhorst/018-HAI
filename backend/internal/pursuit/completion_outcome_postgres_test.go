//go:build integration

package pursuit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
)

func TestPostgresPursuitUnresolvedAttemptQueryIgnoresDisplayLimit(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := &GormRepository{DB: tx}
	p := createPortfolioTestPursuit(t, tx, "alice", 1)
	other := createPortfolioTestPursuit(t, tx, "bob", 1)
	for n := 0; n < 30; n++ {
		item := models.PursuitTaskAttempt{ID: uuid.New(), PursuitID: p.ID, TaskPlanID: uuid.NewString(), OwnerIdentity: "alice", Mode: "run", Status: "validated", VerificationStatus: "verified", UpdatedAt: time.Now().UTC()}
		if err := tx.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	statuses := []string{"needs_review", "uncertain", "unsupported", "conflicting", "failed", "fail", "\tuncertain\n", "\u00a0uncertain\u3000", "", "unknown", "provider_new"}
	for n, status := range statuses {
		item := models.PursuitTaskAttempt{ID: uuid.New(), PursuitID: p.ID, TaskPlanID: fmt.Sprintf("%s-old-%d", p.ID, n), OwnerIdentity: "alice", Mode: "run", Status: "validated", VerificationStatus: " " + status + " ", UpdatedAt: time.Now().UTC().Add(-time.Hour)}
		if err := tx.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	nullLegacy := models.PursuitTaskAttempt{ID: uuid.New(), PursuitID: p.ID, TaskPlanID: uuid.NewString(), Mode: "run", Status: "validated", UpdatedAt: time.Now().UTC().Add(-time.Hour)}
	if err := tx.Create(&nullLegacy).Error; err != nil {
		t.Fatal(err)
	}
	// Preserve the deliberately old timestamp when creating the nullable legacy row.
	if err := tx.Model(&models.PursuitTaskAttempt{}).Where("id = ?", nullLegacy.ID).UpdateColumn("verification_status", nil).Error; err != nil {
		t.Fatal(err)
	}
	foreign := models.PursuitTaskAttempt{ID: uuid.New(), PursuitID: other.ID, TaskPlanID: uuid.NewString(), OwnerIdentity: "bob", Mode: "run", Status: "review_required"}
	if err := tx.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	recent, err := repo.FindTaskAttempts(p.ID, 20)
	if err != nil || len(recent) != 20 {
		t.Fatalf("recent attempts count %d: %v", len(recent), err)
	}
	for _, item := range recent {
		if pursuitTaskAttemptNeedsReview(item) {
			t.Fatal("fixture did not put unresolved attempts outside display window")
		}
	}
	unresolved, err := repo.FindTaskAttemptsNeedingReview(p.ID)
	if err != nil || len(unresolved) != len(statuses)+1 {
		t.Fatalf("unresolved count %d: %v", len(unresolved), err)
	}
	for _, item := range unresolved {
		if item.PursuitID != p.ID || !pursuitTaskAttemptNeedsReview(item) {
			t.Fatalf("query returned wrong scope or non-review item: %#v", item)
		}
	}
}

func TestPostgresPortfolioCoordinationDigestSurvivesSaveReloadWithoutWeakeningEvidence(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := &GormRepository{DB: tx}
	svc := &service{repo: repo}
	planService := plangraph.NewService(plangraph.NewGormRepository(tx), func() time.Time { return time.Now().UTC().Truncate(time.Second) })
	for _, coordinated := range []bool{false, true} {
		p := createPortfolioTestPursuit(t, tx, "alice", 4)
		allocation, items, reservations, activities := portfolioAcceptanceFixture("alice", "padding-"+uuid.NewString(), p.ID, false, 45, 0)
		if coordinated {
			draft, err := planService.Preview(context.Background(), "alice", plangraph.PreviewRequest{
				IdempotencyKey: "padding-plan-" + uuid.NewString(), Title: "Coordination padding regression", CreatedBy: "alice",
				Nodes: []plangraph.Node{{ID: "synthetic-node", Type: "task", Title: "Review fixture", Owner: "hai", Status: plangraph.NodePlanned, Risk: plangraph.RiskLow, ApprovalState: plangraph.ApprovalNotRequired, Bindings: plangraph.Bindings{PursuitID: p.ID.String()}}}, Edges: []plangraph.Edge{},
			})
			if err != nil {
				t.Fatalf("create advisory plan: %v", err)
			}
			accepted, err := planService.Accept(context.Background(), "alice", draft.ID, plangraph.AcceptRequest{ExpectedRevision: draft.Revision, ExpectedDigest: draft.Digest, AcceptedBy: "alice"})
			if err != nil || accepted == nil || accepted.CanExecute {
				t.Fatalf("accept advisory plan: %v", err)
			}
			allocation.CoordinationPlanID = &accepted.ID
			allocation.CoordinationPlanRevision = accepted.Revision
			allocation.CoordinationPlanNodeID = "synthetic-node"
			allocation.CoordinationPlanDigest = accepted.Digest
		}
		var err error
		allocation.RecordDigest, err = digestPortfolioAllocation(allocation)
		if err != nil {
			t.Fatal(err)
		}
		items[0].RecordDigest, err = digestPortfolioAllocationItem(allocation.PlanID, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := repo.SavePortfolioAllocation(allocation, items, reservations, activities); err != nil {
			t.Fatal(err)
		}
		loaded, loadedItems, err := repo.FindPortfolioAllocationForOwner("alice", allocation.PlanID)
		if err != nil || loaded == nil {
			t.Fatalf("reload: %v", err)
		}
		// This records the actual driver representation, not an inferred type conversion.
		t.Logf("coordination present=%t loaded digest bytes=%d ASCII-space-only=%t", coordinated, len(loaded.CoordinationPlanDigest), strings.Trim(loaded.CoordinationPlanDigest, " ") == "")
		if coordinated && loaded.CoordinationPlanDigest != allocation.CoordinationPlanDigest {
			t.Fatal("present accepted digest changed")
		}
		if !coordinated && strings.Trim(loaded.CoordinationPlanDigest, " ") != "" {
			t.Fatal("absent digest became nonempty evidence")
		}
		if err := validatePortfolioAllocationHistoryEvidence("alice", loaded, loadedItems); err != nil {
			t.Fatalf("unchanged reloaded evidence: %v", err)
		}
		if replay, _, created, err := repo.SavePortfolioAllocation(allocation, items, reservations, activities); err != nil || created || replay == nil || replay.RecordDigest != allocation.RecordDigest {
			t.Fatalf("exact replay: created=%t err=%v", created, err)
		}
		if foreign, _, err := repo.FindPortfolioAllocationForOwner("bob", allocation.PlanID); err != nil || foreign != nil {
			t.Fatalf("foreign owner: %v", err)
		}
		altered := *loaded
		altered.CoordinationPlanDigest = strings.Repeat("b", 64)
		if err := validatePortfolioAllocationHistoryEvidence("alice", &altered, loadedItems); err == nil {
			t.Fatal("tampered digest passed evidence validation")
		}
	}
	history, err := svc.PortfolioAllocationHistoryForOwner("alice", 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("real reloaded history count=%d: %v", len(history), err)
	}
	for _, item := range history {
		if item.CanExecute {
			t.Fatal("history evidence granted execution authority")
		}
	}
}

func TestPostgresPursuitExactRuntimeQueryBatchesAllLinkedReceipts(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := &GormRepository{DB: tx}
	s := &service{repo: repo}
	ids := make([]uuid.UUID, 0, 51)
	for n := 0; n < 51; n++ {
		item := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: "alice", LaunchType: "script", Status: "completed", StartedAt: time.Now().UTC().Add(-time.Duration(n) * time.Minute), CompletedAt: time.Now().UTC()}
		if n == 50 {
			item.Status = "indeterminate"
		}
		if err := tx.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	recent, err := repo.FindLinkedAutomationLaunches(nil, ids, len(ids))
	if err != nil || len(recent) != 20 {
		t.Fatalf("real display cap %d: %v", len(recent), err)
	}
	attempts, err := s.findExactRuntimeAttempts("alice", ids)
	if err != nil || len(attempts) != 51 {
		t.Fatalf("exact receipt count %d: %v", len(attempts), err)
	}
	if len(runtimeAttemptBlockers(attempts, nil, nil)) != 1 {
		t.Fatal("oldest uncertainty omitted from batched authority query")
	}
	if _, err := s.findExactRuntimeAttempts("bob", ids); err == nil {
		t.Fatal("foreign owner received exact receipts")
	}
	if _, err := s.findExactRuntimeAttempts("alice", append(ids, uuid.New())); err == nil {
		t.Fatal("missing exact receipt silently treated as complete evidence")
	}
}

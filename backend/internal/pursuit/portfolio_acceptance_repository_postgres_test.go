//go:build integration

package pursuit

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresPortfolioAllocationRepositoryReplayRollbackAndOwnerIsolation(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin test transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repository := &GormRepository{DB: tx}

	alicePursuit := createPortfolioTestPursuit(t, tx, "alice", 4)
	aliceAllocation, aliceItems, aliceReservations, aliceActivities := portfolioAcceptanceFixture(
		"alice", "shared-plan", alicePursuit.ID, false, 60, 0,
	)
	stored, storedItems, created, err := repository.SavePortfolioAllocation(
		aliceAllocation, aliceItems, aliceReservations, aliceActivities,
	)
	if err != nil || !created || stored.ID != aliceAllocation.ID || len(storedItems) != 1 {
		t.Fatalf("first save = allocation %#v items %#v created=%t err=%v", stored, storedItems, created, err)
	}

	found, foundItems, err := repository.FindPortfolioAllocationForOwner("alice", "shared-plan")
	if err != nil || found == nil || found.ID != stored.ID || len(foundItems) != 1 {
		t.Fatalf("find accepted allocation = %#v items=%#v err=%v", found, foundItems, err)
	}
	history, historyItems, err := repository.ListPortfolioAllocationsForOwner("alice", 10)
	if err != nil || len(history) != 1 || history[0].ID != stored.ID || len(historyItems) != 1 || historyItems[0].AllocationID != stored.ID {
		t.Fatalf("list accepted allocations = %#v items=%#v err=%v", history, historyItems, err)
	}
	missing, missingItems, err := repository.FindPortfolioAllocationForOwner("mallory", "shared-plan")
	if err != nil || missing != nil || missingItems != nil {
		t.Fatalf("foreign allocation lookup = %#v items=%#v err=%v", missing, missingItems, err)
	}
	foreignHistory, foreignHistoryItems, err := repository.ListPortfolioAllocationsForOwner("mallory", 10)
	if err != nil || len(foreignHistory) != 0 || len(foreignHistoryItems) != 0 {
		t.Fatalf("foreign allocation history = %#v items=%#v err=%v", foreignHistory, foreignHistoryItems, err)
	}

	replayed, replayedItems, replayCreated, err := repository.SavePortfolioAllocation(
		aliceAllocation, aliceItems, aliceReservations, aliceActivities,
	)
	if err != nil || replayCreated || replayed.ID != stored.ID || len(replayedItems) != 1 {
		t.Fatalf("exact replay = allocation %#v items=%#v created=%t err=%v", replayed, replayedItems, replayCreated, err)
	}
	assertPortfolioAggregateCounts(t, tx, stored.ID, []uuid.UUID{storedItems[0].ReservationID}, 1, 1, 1, 1)

	changedParent := *aliceAllocation
	changedParent.RequestDigest = portfolioTestDigest("changed request")
	if _, _, _, err := repository.SavePortfolioAllocation(&changedParent, aliceItems, aliceReservations, aliceActivities); err == nil || !strings.Contains(err.Error(), "different digests") {
		t.Fatalf("changed request digest error = %v", err)
	}
	changedItems := append([]models.PursuitPortfolioAllocationItem(nil), aliceItems...)
	changedItems[0].RecordDigest = portfolioTestDigest("changed item")
	if _, _, _, err := repository.SavePortfolioAllocation(aliceAllocation, changedItems, aliceReservations, aliceActivities); err == nil || !strings.Contains(err.Error(), "different item digests") {
		t.Fatalf("changed item digest error = %v", err)
	}
	changedReservations := append([]models.PursuitResourceReservation(nil), aliceReservations...)
	changedReservations[0].RecordDigest = portfolioTestDigest("changed reservation")
	if _, _, _, err := repository.SavePortfolioAllocation(aliceAllocation, aliceItems, changedReservations, aliceActivities); err == nil || !strings.Contains(err.Error(), "different reservation digests") {
		t.Fatalf("changed reservation digest error = %v", err)
	}

	bobPursuit := createPortfolioTestPursuit(t, tx, "bob", 4)
	bobAllocation, bobItems, bobReservations, bobActivities := portfolioAcceptanceFixture(
		"bob", "shared-plan", bobPursuit.ID, false, 45, 0,
	)
	if _, _, created, err := repository.SavePortfolioAllocation(bobAllocation, bobItems, bobReservations, bobActivities); err != nil || !created {
		t.Fatalf("same plan for separate owner created=%t err=%v", created, err)
	}
	bobFound, _, err := repository.FindPortfolioAllocationForOwner("bob", "shared-plan")
	if err != nil || bobFound == nil || bobFound.OwnerIdentity != "bob" || bobFound.ID == stored.ID {
		t.Fatalf("owner-isolated lookup = %#v err=%v", bobFound, err)
	}

	crossOwnerAllocation, crossOwnerItems, crossOwnerReservations, crossOwnerActivities := portfolioAcceptanceFixture(
		"alice", "cross-owner-plan", bobPursuit.ID, false, 30, 0,
	)
	if _, _, _, err := repository.SavePortfolioAllocation(crossOwnerAllocation, crossOwnerItems, crossOwnerReservations, crossOwnerActivities); err == nil || !strings.Contains(err.Error(), "unavailable to this owner") {
		t.Fatalf("cross-owner save error = %v", err)
	}

	limitedPursuit := createPortfolioTestPursuit(t, tx, "alice", 1)
	failedAllocation, failedItems, failedReservations, failedActivities := portfolioAcceptanceFixture(
		"alice", "rollback-plan", limitedPursuit.ID, false, 120, 0,
	)
	if _, _, _, err := repository.SavePortfolioAllocation(failedAllocation, failedItems, failedReservations, failedActivities); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ceiling") {
		t.Fatalf("ceiling rollback error = %v", err)
	}
	assertPortfolioAggregateCounts(t, tx, failedAllocation.ID, []uuid.UUID{failedReservations[0].ID}, 0, 0, 0, 0)

	expectPortfolioPostgresRejection(t, tx, "allocation update", func(db *gorm.DB) error {
		return db.Model(&models.PursuitPortfolioAllocation{}).
			Where("id = ?", stored.ID).Update("status", PortfolioAllocationAcceptedNeedsApproval).Error
	})
	expectPortfolioPostgresRejection(t, tx, "item delete", func(db *gorm.DB) error {
		return db.Delete(&models.PursuitPortfolioAllocationItem{}, "id = ?", storedItems[0].ID).Error
	})
}

func TestPostgresPortfolioAllocationConcurrentExactReplay(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	owner := "portfolio-concurrent-" + uuid.NewString()
	pursuit := createPortfolioTestPursuit(t, db, owner, 4)
	planID := "concurrent-" + uuid.NewString()

	type outcome struct {
		allocation *models.PursuitPortfolioAllocation
		items      []models.PursuitPortfolioAllocationItem
		created    bool
		err        error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for caller := 0; caller < 2; caller++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			allocation, items, reservations, activities := portfolioAcceptanceFixture(
				owner, planID, pursuit.ID, false, 60, 0,
			)
			<-start
			stored, storedItems, created, err := (&GormRepository{DB: db}).SavePortfolioAllocation(
				allocation, items, reservations, activities,
			)
			results <- outcome{allocation: stored, items: storedItems, created: created, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	createdCount := 0
	var acceptedID uuid.UUID
	var acceptedReservationID uuid.UUID
	for result := range results {
		if result.err != nil || result.allocation == nil || len(result.items) != 1 {
			t.Fatalf("concurrent acceptance = %#v", result)
		}
		if result.created {
			createdCount++
		}
		if acceptedID == uuid.Nil {
			acceptedID = result.allocation.ID
			acceptedReservationID = result.items[0].ReservationID
		} else if result.allocation.ID != acceptedID {
			t.Fatalf("concurrent callers returned different allocations: %s and %s", acceptedID, result.allocation.ID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("concurrent exact replay created count = %d, want 1", createdCount)
	}
	assertPortfolioAggregateCounts(t, db, acceptedID, []uuid.UUID{acceptedReservationID}, 1, 1, 1, 1)
}

func TestPostgresPortfolioExecutionProposalRepositoryReplayOwnerIsolationAndImmutability(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repository := &GormRepository{DB: tx}

	pursuit := createPortfolioTestPursuit(t, tx, "alice", 4)
	pursuit.NextRecommendedAction = "Prepare a verified execution package"
	if err := tx.Save(&pursuit).Error; err != nil {
		t.Fatal(err)
	}
	allocation, allocationItems, reservations, allocationActivities := portfolioAcceptanceFixture(
		"alice", "execution-repository-"+uuid.NewString(), pursuit.ID, false, 45, 0,
	)
	storedAllocation, storedAllocationItems, _, err := repository.SavePortfolioAllocation(
		allocation, allocationItems, reservations, allocationActivities,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPortfolioExecutionProposalSnapshot("alice", storedAllocation.ID)
	if err != nil || snapshot == nil || len(snapshot.AllocationItems) != 1 || len(snapshot.Pursuits) != 1 || len(snapshot.SettledReservationIDs) != 0 {
		t.Fatalf("proposal snapshot=%#v err=%v", snapshot, err)
	}
	if foreign, err := repository.LoadPortfolioExecutionProposalSnapshot("bob", storedAllocation.ID); err != nil || foreign != nil {
		t.Fatalf("foreign proposal snapshot=%#v err=%v", foreign, err)
	}

	preparedAt := time.Now().UTC().Truncate(time.Second)
	proposalItems, snapshotDigest, status, err := buildPortfolioExecutionProposalItems(snapshot, preparedAt)
	if err != nil {
		t.Fatal(err)
	}
	proposal := &models.PursuitPortfolioExecutionProposal{
		ID: uuid.New(), AllocationID: storedAllocation.ID, OwnerIdentity: "alice",
		AllocationRecordDigest: storedAllocation.RecordDigest, SnapshotDigest: snapshotDigest,
		Status: status, Actor: "alice", Confirmation: PortfolioExecutionProposalConfirmation,
		Authority: PortfolioExecutionProposalAuthority, PreparedAt: preparedAt,
	}
	proposal.RecordDigest, err = digestPortfolioExecutionProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	activities := make([]models.PursuitActivity, 0, len(proposalItems))
	for index := range proposalItems {
		proposalItems[index].ID = uuid.New()
		proposalItems[index].ProposalID = proposal.ID
		proposalItems[index].OwnerIdentity = "alice"
		proposalItems[index].PreparedAt = preparedAt
		proposalItems[index].RecordDigest, err = digestPortfolioExecutionProposalItem(snapshotDigest, proposalItems[index])
		if err != nil {
			t.Fatal(err)
		}
		activities = append(activities, newPursuitResourceActivity(
			proposalItems[index].PursuitID, portfolioExecutionProposalActivityType,
			"Prepared proposal; execution remains separate.", "alice",
			portfolioExecutionProposalActivitySource, proposal.ID.String(),
			"hai://portfolio-execution-proposals/"+proposal.ID.String(), preparedAt,
		))
	}
	stored, storedItems, created, err := repository.SavePortfolioExecutionProposal(proposal, proposalItems, activities)
	if err != nil || !created || stored.ID != proposal.ID || len(storedItems) != 1 {
		t.Fatalf("save proposal=%#v items=%#v created=%t err=%v", stored, storedItems, created, err)
	}
	found, foundItems, err := repository.FindPortfolioExecutionProposalForSnapshot("alice", storedAllocation.ID, snapshotDigest)
	if err != nil || found == nil || found.ID != stored.ID || len(foundItems) != 1 {
		t.Fatalf("find proposal=%#v items=%#v err=%v", found, foundItems, err)
	}
	foreign, foreignItems, err := repository.FindPortfolioExecutionProposalForSnapshot("bob", storedAllocation.ID, snapshotDigest)
	if err != nil || foreign != nil || foreignItems != nil {
		t.Fatalf("foreign proposal=%#v items=%#v err=%v", foreign, foreignItems, err)
	}
	replayed, replayedItems, replayCreated, err := repository.SavePortfolioExecutionProposal(proposal, proposalItems, activities)
	if err != nil || replayCreated || replayed.ID != stored.ID || len(replayedItems) != 1 {
		t.Fatalf("proposal replay=%#v items=%#v created=%t err=%v", replayed, replayedItems, replayCreated, err)
	}
	if err := tx.Model(&models.Pursuit{}).Where("id = ?", pursuit.ID).
		Update("next_recommended_action", "Changed after proposal snapshot").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repository.SavePortfolioExecutionProposal(proposal, proposalItems, activities); err == nil || !strings.Contains(err.Error(), "state changed") {
		t.Fatalf("stale pursuit-state proposal error=%v", err)
	}

	var proposalCount, itemCount, activityCount int64
	if err := tx.Model(&models.PursuitPortfolioExecutionProposal{}).Where("id = ?", stored.ID).Count(&proposalCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.PursuitPortfolioExecutionProposalItem{}).Where("proposal_id = ?", stored.ID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.PursuitActivity{}).Where("source_type = ? AND source_id = ?", portfolioExecutionProposalActivitySource, stored.ID.String()).Count(&activityCount).Error; err != nil {
		t.Fatal(err)
	}
	if proposalCount != 1 || itemCount != 1 || activityCount != 1 || storedAllocationItems[0].ReservationID != storedItems[0].ReservationID {
		t.Fatalf("proposal aggregate counts=%d/%d/%d binding=%s/%s", proposalCount, itemCount, activityCount, storedAllocationItems[0].ReservationID, storedItems[0].ReservationID)
	}
	expectPortfolioPostgresRejection(t, tx, "proposal update", func(db *gorm.DB) error {
		return db.Model(&models.PursuitPortfolioExecutionProposal{}).Where("id = ?", stored.ID).Update("status", PortfolioExecutionProposalPreparedBlocked).Error
	})
	expectPortfolioPostgresRejection(t, tx, "proposal item delete", func(db *gorm.DB) error {
		return db.Delete(&models.PursuitPortfolioExecutionProposalItem{}, "id = ?", storedItems[0].ID).Error
	})
}

func TestPostgresPortfolioExecutionProposalDecisionReplayOwnerIsolationAndImmutability(t *testing.T) {
	db := openPortfolioAllocationPostgresTestDB(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repository := &GormRepository{DB: tx}
	svc := &service{repo: repository}

	pursuit := createPortfolioTestPursuit(t, tx, "alice", 4)
	pursuit.NextRecommendedAction = "Prepare a source-grounded response"
	pursuit.RiskLevel = "high"
	pursuit.AutonomyLevel = "approve_before_execute"
	if err := tx.Save(&pursuit).Error; err != nil {
		t.Fatal(err)
	}
	allocation, allocationItems, reservations, activities := portfolioAcceptanceFixture(
		"alice", "decision-repository-"+uuid.NewString(), pursuit.ID, false, 45, 0,
	)
	allocationDigest, digestErr := digestPortfolioAllocation(allocation)
	if digestErr != nil {
		t.Fatal(digestErr)
	}
	allocation.RecordDigest = allocationDigest
	for index := range allocationItems {
		allocationItems[index].RecordDigest, digestErr = digestPortfolioAllocationItem(allocation.PlanID, allocationItems[index])
		if digestErr != nil {
			t.Fatal(digestErr)
		}
	}
	storedAllocation, _, _, err := repository.SavePortfolioAllocation(allocation, allocationItems, reservations, activities)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.PreparePortfolioExecutionProposalsForOwner("alice", "alice", storedAllocation.ID, PortfolioExecutionProposalRequest{
		ExpectedAllocationDigest: storedAllocation.RecordDigest,
		Confirmation:             PortfolioExecutionProposalConfirmation,
	})
	if err != nil || len(proposal.Items) != 1 {
		t.Fatalf("prepare proposal=%#v err=%v", proposal, err)
	}
	item := proposal.Items[0]
	request := PortfolioExecutionProposalDecisionRequest{
		ExpectedItemDigest: item.RecordDigest,
		Decision:           PortfolioExecutionDecisionApproved,
		Reason:             "Owner reviewed this immutable item.",
		Confirmation:       PortfolioExecutionDecisionApproveConfirmation,
	}
	approved, err := svc.DecidePortfolioExecutionProposalItemForOwner("alice", "alice", item.ID, request)
	if err != nil || approved.Replayed || approved.CanExecute || approved.Decision == nil {
		t.Fatalf("approve decision=%#v err=%v", approved, err)
	}
	replayed, err := svc.DecidePortfolioExecutionProposalItemForOwner("alice", "alice", item.ID, request)
	if err != nil || !replayed.Replayed || replayed.Decision.ID != approved.Decision.ID {
		t.Fatalf("decision replay=%#v err=%v", replayed, err)
	}
	if foreign, err := repository.LoadPortfolioExecutionProposalDecisionSnapshot("bob", item.ID); err != nil || foreign != nil {
		t.Fatalf("foreign decision snapshot=%#v err=%v", foreign, err)
	}
	history, err := repository.ListPortfolioExecutionProposalDecisions("alice", item.ID, 10)
	if err != nil || len(history) != 1 || history[0].ID != approved.Decision.ID {
		t.Fatalf("decision history=%#v err=%v", history, err)
	}
	var decisionCount, activityCount int64
	if err := tx.Model(&models.PursuitPortfolioExecutionProposalDecision{}).Where("proposal_item_id = ?", item.ID).Count(&decisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.PursuitActivity{}).Where(
		"source_type = ? AND source_id = ?", "pursuit_portfolio_execution_proposal_decision", approved.Decision.ID.String(),
	).Count(&activityCount).Error; err != nil {
		t.Fatal(err)
	}
	if decisionCount != 1 || activityCount != 1 {
		t.Fatalf("decision aggregate counts=%d/%d", decisionCount, activityCount)
	}
	coordination, err := svc.PortfolioDispatchCoordinationBatchForOwner(
		context.Background(), "alice", []uuid.UUID{proposal.Proposal.ID},
	)
	if err != nil || len(coordination) != 1 || coordination[0].Eligible != 1 ||
		coordination[0].Authority != PortfolioCoordinationAuthority || coordination[0].CanExecute ||
		len(coordination[0].Items) != 1 || coordination[0].Items[0].Decision == nil ||
		coordination[0].Items[0].Decision.ID != approved.Decision.ID ||
		coordination[0].Freshness.Status != PortfolioCoordinationFreshnessCurrent ||
		!coordination[0].Freshness.RevalidationRequired {
		t.Fatalf("postgres batch coordination=%#v err=%v", coordination, err)
	}
	if _, err := svc.PortfolioDispatchCoordinationBatchForOwner(
		context.Background(), "bob", []uuid.UUID{proposal.Proposal.ID},
	); err == nil || !strings.Contains(err.Error(), "unavailable to this owner") {
		t.Fatalf("foreign postgres batch coordination error=%v", err)
	}
	expectPortfolioPostgresRejection(t, tx, "decision update", func(db *gorm.DB) error {
		return db.Model(&models.PursuitPortfolioExecutionProposalDecision{}).
			Where("id = ?", approved.Decision.ID).Update("reason", "tampered").Error
	})
	expectPortfolioPostgresRejection(t, tx, "decision delete", func(db *gorm.DB) error {
		return db.Delete(&models.PursuitPortfolioExecutionProposalDecision{}, "id = ?", approved.Decision.ID).Error
	})
	if err := tx.Model(&models.Pursuit{}).Where("id = ?", pursuit.ID).
		Update("next_recommended_action", "Changed after decision source snapshot").Error; err != nil {
		t.Fatal(err)
	}
	request.Decision = PortfolioExecutionDecisionRejected
	request.Reason = "Reject changed state"
	request.Confirmation = PortfolioExecutionDecisionRejectConfirmation
	if _, err := svc.DecidePortfolioExecutionProposalItemForOwner("alice", "alice", item.ID, request); err == nil || !strings.Contains(err.Error(), "fresh proposal") {
		t.Fatalf("stale proposal decision error=%v", err)
	}
}

func openPortfolioAllocationPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open portfolio allocation Postgres: %v", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("open portfolio test connection pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if databaseName != "hai_migration_runner_test" {
		t.Fatalf("refusing portfolio allocation integration test against database %q", databaseName)
	}
	return db
}

func createPortfolioTestPursuit(t *testing.T, db *gorm.DB, owner string, effortHours float64) models.Pursuit {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	pursuit := models.Pursuit{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Portfolio repository fixture",
		Status: StatusActive, RiskLevel: "low", AutonomyLevel: "autonomous_safe",
		SuccessCriteria: []models.PursuitSuccessCriterion{}, StopConditions: []models.PursuitStopCondition{},
		Dependencies: []models.PursuitDependency{}, ResourceLimits: models.PursuitResourceLimits{MaxEffortHours: effortHours},
		CompletionState: "open", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&pursuit).Error; err != nil {
		t.Fatalf("create portfolio test pursuit: %v", err)
	}
	return pursuit
}

func portfolioAcceptanceFixture(
	owner, planID string,
	pursuitID uuid.UUID,
	requiresApproval bool,
	durationMinutes, costMicros int64,
) (*models.PursuitPortfolioAllocation, []models.PursuitPortfolioAllocationItem, []models.PursuitResourceReservation, []models.PursuitActivity) {
	now := time.Now().UTC().Truncate(time.Second)
	allocationID := uuid.New()
	reservationID := uuid.New()
	status := PortfolioAllocationAccepted
	reasons := []string{}
	if requiresApproval {
		status = PortfolioAllocationAcceptedNeedsApproval
		reasons = []string{"owner approval remains required before execution"}
	}
	allocation := &models.PursuitPortfolioAllocation{
		ID: allocationID, OwnerIdentity: owner, PlanID: planID,
		RequestDigest:  portfolioTestDigest(owner + ":" + planID + ":request"),
		DecisionDigest: portfolioTestDigest(owner + ":" + planID + ":decision"),
		Status:         status, DurationMode: "conservative",
		HorizonStart: now, HorizonEnd: now.Add(4 * time.Hour), Actor: owner,
		Confirmation: "ACCEPT PORTFOLIO ALLOCATION",
		RecordDigest: portfolioTestDigest(owner + ":" + planID + ":allocation"), AcceptedAt: now,
	}
	item := models.PursuitPortfolioAllocationItem{
		ID: uuid.New(), AllocationID: allocationID, PursuitID: pursuitID, OwnerIdentity: owner,
		ScheduledStart: now, ScheduledEnd: now.Add(time.Duration(durationMinutes) * time.Minute),
		DurationMinutes: durationMinutes, EstimatedCostMicros: costMicros,
		RequiresApproval: requiresApproval, ApprovalReasons: reasons,
		ReservationID: reservationID,
		RecordDigest:  portfolioTestDigest(owner + ":" + planID + ":" + pursuitID.String() + ":item"), CreatedAt: now,
	}
	reservation := models.PursuitResourceReservation{
		ID: reservationID, PursuitID: pursuitID, OwnerIdentity: owner,
		OperationID:            "portfolio:" + allocation.DecisionDigest[:24] + ":" + pursuitID.String(),
		EstimatedEffortMinutes: durationMinutes, EstimatedCostMicros: costMicros,
		Reason: "accepted portfolio allocation capacity hold", Actor: owner,
		RecordDigest: portfolioTestDigest(owner + ":" + planID + ":" + pursuitID.String() + ":reservation"), ReservedAt: now,
	}
	if costMicros > 0 {
		reservation.Currency = "EUR"
	}
	activity := models.PursuitActivity{
		ID: uuid.New(), PursuitID: pursuitID, EventType: portfolioAllocationActivityType,
		Message: "Accepted portfolio allocation; execution remains separately governed.", Actor: owner,
		SourceType: portfolioAllocationActivitySource, SourceID: allocationID.String(),
		SourceURI: "hai://pursuits/" + pursuitID.String() + "/portfolio-allocations/" + allocationID.String(),
		CreatedAt: now,
	}
	return allocation,
		[]models.PursuitPortfolioAllocationItem{item},
		[]models.PursuitResourceReservation{reservation},
		[]models.PursuitActivity{activity}
}

func assertPortfolioAggregateCounts(
	t *testing.T,
	db *gorm.DB,
	allocationID uuid.UUID,
	reservationIDs []uuid.UUID,
	allocations, items, reservations, activities int64,
) {
	t.Helper()
	var allocationCount, itemCount, reservationCount, activityCount int64
	if err := db.Model(&models.PursuitPortfolioAllocation{}).Where("id = ?", allocationID).Count(&allocationCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("pursuit_portfolio_allocation_items").Where("allocation_id = ?", allocationID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("pursuit_resource_reservations").Where("id IN ?", reservationIDs).Count(&reservationCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("pursuit_activities").Where("source_type = ? AND source_id = ?", portfolioAllocationActivitySource, allocationID.String()).Count(&activityCount).Error; err != nil {
		t.Fatal(err)
	}
	if allocationCount != allocations || itemCount != items || reservationCount != reservations || activityCount != activities {
		t.Fatalf(
			"aggregate counts allocation=%d items=%d reservations=%d activities=%d, want %d/%d/%d/%d",
			allocationCount, itemCount, reservationCount, activityCount,
			allocations, items, reservations, activities,
		)
	}
}

func expectPortfolioPostgresRejection(t *testing.T, db *gorm.DB, label string, operation func(*gorm.DB) error) {
	t.Helper()
	err := db.Transaction(func(tx *gorm.DB) error { return operation(tx) })
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded", label)
	}
}

func portfolioTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

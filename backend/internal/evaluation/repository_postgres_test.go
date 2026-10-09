//go:build integration

package evaluation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openEvaluationPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_EVALUATION_TEST_DATABASE_DSN", "hai_evaluation_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read database name: %v", err)
	}
	if databaseName != "hai_evaluation_test" {
		t.Fatalf("refusing destructive evaluation test against unexpected database identity %q", databaseName)
	}
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	executeEvaluationMigration(t, db, "pre/0001_extensions.up.sql")
	executeEvaluationMigration(t, db, "pre/0009_evaluation.up.sql")
	return db
}

func executeEvaluationMigration(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	sql, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if err := db.Exec(string(sql)).Error; err != nil {
		t.Fatalf("execute migration %s: %v", path, err)
	}
}

func TestGormRepositoryOwnerIsolationRoundTripAndImmutableReceipts(t *testing.T) {
	db := openEvaluationPostgresTestDB(t)
	repository := NewGormRepository(db)
	ctx := context.Background()
	dataset := testDataset(t, 1, []byte(`{"prompt":"hello"}`))

	for _, owner := range []string{"alice", "bob"} {
		if err := repository.CreateDataset(ctx, owner, dataset); err != nil {
			t.Fatalf("create %s dataset: %v", owner, err)
		}
	}
	if _, err := repository.GetDataset(ctx, "mallory", dataset.ID, dataset.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner dataset read returned %v", err)
	}

	baseline := testRun(t, dataset, RunModeShadow, "", 1, CriterionPassed, 0.9)
	baseline.ID = "baseline"
	baseline.RecordDigest = runDigest(baseline)
	candidate := testRun(t, dataset, RunModeCanary, baseline.ID, 2, CriterionPassed, 0.95)
	if err := repository.CreateRun(ctx, "alice", baseline); err != nil {
		t.Fatalf("create baseline: %v", err)
	}
	if err := repository.CreateRun(ctx, "alice", candidate); err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	storedRun, err := repository.GetRun(ctx, "alice", candidate.ID)
	if err != nil || storedRun.RecordDigest != candidate.RecordDigest ||
		storedRun.ReproducibilityDigest != candidate.ReproducibilityDigest {
		t.Fatalf("run round trip = %#v, %v", storedRun, err)
	}
	if _, err := repository.GetRun(ctx, "bob", candidate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice run: %v", err)
	}

	thresholds := RegressionThresholds{
		MinOverallScore: 0.8, MinCasePassRate: 1,
		MaxOverallScoreDrop: 0.1, MaxCasePassRateDrop: 0,
		MaxRequiredFailures: 0, MaxCriterionErrors: 0,
	}
	comparison, err := NewBaselineComparisonReceipt(BaselineComparisonReceiptSpec{
		ID: "comparison-1", Candidate: candidate, Baseline: baseline,
		Thresholds: thresholds, CreatedAt: time.Date(2026, 7, 30, 14, 0, 0, 123, time.UTC),
	})
	if err != nil {
		t.Fatalf("new comparison: %v", err)
	}
	if err := repository.CreateComparisonReceipt(ctx, "alice", comparison); err != nil {
		t.Fatalf("create comparison: %v", err)
	}
	promotion, err := NewPromotionDecisionReceipt(PromotionDecisionReceiptSpec{
		ID: "promotion-1", Candidate: candidate, Baseline: &baseline,
		ComparisonReceiptID: comparison.ID,
		Thresholds:          thresholds,
		CreatedAt:           time.Date(2026, 7, 30, 14, 1, 0, 456, time.UTC),
	})
	if err != nil {
		t.Fatalf("new promotion: %v", err)
	}
	if err := repository.CreatePromotionDecisionReceipt(ctx, "alice", promotion); err != nil {
		t.Fatalf("create promotion: %v", err)
	}
	storedComparison, err := repository.GetComparisonReceipt(ctx, "alice", comparison.ID)
	if err != nil || storedComparison.ReceiptDigest != comparison.ReceiptDigest ||
		storedComparison.Comparison.CandidateRunID != candidate.ID {
		t.Fatalf("comparison round trip = %#v, %v", storedComparison, err)
	}
	storedPromotion, err := repository.GetPromotionDecisionReceipt(ctx, "alice", promotion.ID)
	if err != nil || storedPromotion.ReceiptDigest != promotion.ReceiptDigest ||
		!storedPromotion.Decision.Allowed {
		t.Fatalf("promotion round trip = %#v, %v", storedPromotion, err)
	}
	if _, err := repository.GetPromotionDecisionReceipt(ctx, "bob", promotion.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice promotion receipt: %v", err)
	}

	for label, operation := range map[string]func() error{
		"dataset update": func() error {
			return db.Model(&models.EvaluationDataset{}).
				Where("owner_identity = ? AND dataset_id = ?", "alice", dataset.ID).
				Update("digest", strings.Repeat("f", 64)).Error
		},
		"run delete": func() error {
			return db.Where("owner_identity = ? AND run_id = ?", "alice", candidate.ID).
				Delete(&models.EvaluationRun{}).Error
		},
		"comparison update": func() error {
			return db.Model(&models.EvaluationComparisonReceipt{}).
				Where("owner_identity = ? AND receipt_id = ?", "alice", comparison.ID).
				Update("comparison_json", `{}`).Error
		},
		"promotion delete": func() error {
			return db.Where("owner_identity = ? AND receipt_id = ?", "alice", promotion.ID).
				Delete(&models.EvaluationPromotionDecisionReceipt{}).Error
		},
		"truncate": func() error {
			return db.Exec("TRUNCATE TABLE public.evaluation_promotion_decision_receipts").Error
		},
	} {
		if err := operation(); err == nil {
			t.Fatalf("Postgres accepted immutable %s", label)
		}
	}
}

func TestGormRepositoryFailsClosedOnStoredDigestMismatch(t *testing.T) {
	db := openEvaluationPostgresTestDB(t)
	repository := NewGormRepository(db)
	ctx := context.Background()
	dataset := testDataset(t, 1, []byte(`{"prompt":"hello"}`))

	row := models.EvaluationDataset{
		OwnerIdentity:  "alice",
		DatasetID:      "forged",
		DatasetVersion: 1,
		SchemaVersion:  dataset.SchemaVersion,
		Name:           dataset.Name,
		Description:    dataset.Description,
		CreatedAtValue: formatExactTime(dataset.CreatedAt),
		Digest:         strings.Repeat("f", 64),
		RecordedAt:     time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert forged dataset metadata: %v", err)
	}
	caseRow := models.EvaluationDatasetCase{
		OwnerIdentity:   "alice",
		DatasetRecordID: row.ID,
		Ordinal:         0,
		CaseID:          dataset.Cases[0].ID,
		CaseVersion:     dataset.Cases[0].Version,
		InputJSON:       string(dataset.Cases[0].Input),
		ExpectedJSON:    string(dataset.Cases[0].Expected),
		CriteriaJSON:    `[{"id":"correct","required":true,"weight":1,"minScore":0.8}]`,
	}
	if err := db.Create(&caseRow).Error; err != nil {
		t.Fatalf("insert forged dataset case: %v", err)
	}
	if _, err := repository.GetDataset(ctx, "alice", "forged", 1); !errors.Is(err, ErrInvalidDataset) {
		t.Fatalf("forged stored dataset returned %v", err)
	}
}

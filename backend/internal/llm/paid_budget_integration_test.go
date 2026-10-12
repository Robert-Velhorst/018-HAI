//go:build integration

package llm

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPaidInferenceBudgetReservationIsAtomicAcrossRepositoryInstances(t *testing.T) {
	db := newPaidBudgetDisposablePostgres(t)
	firstRepository := NewGormGenerationHistoryRepository(db).(*GormGenerationHistoryRepository)
	secondRepository := NewGormGenerationHistoryRepository(db).(*GormGenerationHistoryRepository)
	start := time.Now().UTC().Truncate(time.Microsecond)
	records := []*models.LLMGenerationRecord{
		paidBudgetReservationRecord("provider-a", "model-a", start),
		paidBudgetReservationRecord("provider-b", "model-b", start),
	}
	gate := make(chan struct{})
	type result struct {
		record *models.LLMGenerationRecord
		err    error
	}
	results := make(chan result, len(records))
	repositories := []*GormGenerationHistoryRepository{firstRepository, secondRepository}
	var workers sync.WaitGroup
	for index := range records {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-gate
			err := repositories[index].ReservePaidGeneration(context.Background(), records[index], 1)
			results <- result{record: records[index], err: err}
		}(index)
	}
	close(gate)
	workers.Wait()
	close(results)

	var reserved *models.LLMGenerationRecord
	reservedCount, rejectedCount := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			reservedCount++
			reserved = result.record
		case errors.Is(result.err, ErrPaidBudgetExceeded):
			rejectedCount++
		default:
			t.Fatalf("reserve concurrent paid call: %v", result.err)
		}
	}
	if reservedCount != 1 || rejectedCount != 1 {
		t.Fatalf("concurrent reservation outcomes = accepted %d, budget-rejected %d; want one each", reservedCount, rejectedCount)
	}

	reserved.Status = "completed"
	reserved.Reason = "provider reported usage"
	reserved.EstimatedCostEUR = 0.4
	reserved.InputTokens = 10
	reserved.OutputTokens = 5
	reserved.UsageSource = "provider_reported"
	reserved.LoggedAt = start.Add(24 * time.Hour)
	if err := firstRepository.FinalizePaidGeneration(reserved); err != nil {
		t.Fatalf("finalize reservation: %v", err)
	}
	if err := firstRepository.FinalizePaidGeneration(reserved); err == nil {
		t.Fatal("duplicate settlement succeeded; want immutable one-time finalization")
	}
	usage, err := firstRepository.UsageBetween(utcDayStart(start), utcDayStart(start).Add(24*time.Hour))
	if err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if len(usage) != 1 || usage[0].BudgetUsedEUR != 0.4 {
		t.Fatalf("usage after reservation finalization = %#v; want one 0.4 EUR row on the reserved day", usage)
	}
	if _, err := firstRepository.UsageBetween(utcDayStart(start).Add(24*time.Hour), utcDayStart(start).Add(48*time.Hour)); err != nil {
		t.Fatalf("read following UTC day: %v", err)
	} else if nextDay, _ := firstRepository.UsageBetween(utcDayStart(start).Add(24*time.Hour), utcDayStart(start).Add(48*time.Hour)); len(nextDay) != 0 {
		t.Fatalf("finalization moved usage to the request completion day: %#v", nextDay)
	}
	history, err := firstRepository.FindRecentGenerations(10)
	if err != nil {
		t.Fatalf("read generation history: %v", err)
	}
	if len(history) != 1 || history[0].ID != reserved.ID || history[0].Status != "completed" ||
		history[0].EstimatedCostEUR != 0.4 || history[0].UsageSource != "provider_reported" || !history[0].LoggedAt.Equal(start) {
		t.Fatalf("resolved generation history = %#v; want one completed call on its reservation day with provider usage", history)
	}
	tooLarge := paidBudgetReservationRecord("provider-c", "model-c", start)
	tooLarge.EstimatedCostEUR = 0.7
	if err := firstRepository.ReservePaidGeneration(context.Background(), tooLarge, 1); !errors.Is(err, ErrPaidBudgetExceeded) {
		t.Fatalf("reserve after actual settlement returned %v; want actual 0.4 EUR usage included", err)
	}
}

func TestPaidInferenceBudgetUsesUTCDateAcrossLocalOffsetBoundary(t *testing.T) {
	db := newPaidBudgetDisposablePostgres(t)
	repository := NewGormGenerationHistoryRepository(db).(*GormGenerationHistoryRepository)
	local := time.FixedZone("test-western-europe", 2*60*60)
	before := time.Date(2026, 9, 30, 1, 59, 59, 0, local)
	after := before.Add(time.Second)
	if before.UTC().Format("2006-01-02 15:04:05") != "2026-09-29 23:59:59" ||
		after.UTC().Format("2006-01-02 15:04:05") != "2026-09-30 00:00:00" {
		t.Fatalf("test timestamps do not straddle UTC midnight: before=%s after=%s", before.UTC(), after.UTC())
	}
	for index, at := range []time.Time{before, after} {
		record := paidBudgetReservationRecord("provider", "model", at)
		if err := repository.ReservePaidGeneration(context.Background(), record, 0.6); err != nil {
			t.Fatalf("reserve on UTC date %d (%s): %v", index, at.UTC(), err)
		}
	}
	for _, day := range []time.Time{time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)} {
		usage, err := repository.UsageBetween(day, day.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("read UTC usage for %s: %v", day, err)
		}
		if len(usage) != 1 || usage[0].BudgetUsedEUR != 0.6 {
			t.Fatalf("usage for UTC date %s = %#v; want independent 0.6 EUR reservation", day.Format("2006-01-02"), usage)
		}
	}
}

func TestPaidInferenceBudgetCountsProviderReportedActualAboveEstimate(t *testing.T) {
	db := newPaidBudgetDisposablePostgres(t)
	repository := NewGormGenerationHistoryRepository(db).(*GormGenerationHistoryRepository)
	start := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	reservation := paidBudgetReservationRecord("provider", "model", start)
	if err := repository.ReservePaidGeneration(context.Background(), reservation, 1); err != nil {
		t.Fatalf("reserve estimated 0.6 EUR: %v", err)
	}
	settled := *reservation
	settled.Status = "completed"
	settled.Reason = "provider reported usage above estimate"
	settled.EstimatedCostEUR = 0.8
	settled.InputTokens = 100
	settled.OutputTokens = 120
	settled.UsageSource = "provider_reported"
	if err := repository.FinalizePaidGeneration(&settled); err != nil {
		t.Fatalf("append provider-reported settlement: %v", err)
	}
	tooLarge := paidBudgetReservationRecord("provider-2", "model-2", start)
	tooLarge.EstimatedCostEUR = 0.25
	if err := repository.ReservePaidGeneration(context.Background(), tooLarge, 1); !errors.Is(err, ErrPaidBudgetExceeded) {
		t.Fatalf("reserve after higher reported usage returned %v; want cap block", err)
	}
	usage, err := repository.UsageBetween(utcDayStart(start), utcDayStart(start).Add(24*time.Hour))
	if err != nil {
		t.Fatalf("read finalized usage: %v", err)
	}
	if len(usage) != 1 || usage[0].BudgetUsedEUR != 0.8 || usage[0].InputTokensUsed != 100 || usage[0].OutputTokensUsed != 120 {
		t.Fatalf("reported usage aggregate = %#v; want actual provider usage, not the estimate", usage)
	}
}

func paidBudgetReservationRecord(providerID, modelID string, at time.Time) *models.LLMGenerationRecord {
	return &models.LLMGenerationRecord{
		ID: uuid.New(), ProviderID: providerID, ModelID: modelID, ModelName: modelID,
		Tier: TierCheap, Status: "reserved", Reason: "test reservation",
		EstimatedCostEUR: 0.6, InputTokens: 10, OutputTokens: 10,
		UsageSource: "estimated_uncertain", LoggedAt: at.UTC(),
	}
}

func newPaidBudgetDisposablePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping disposable PostgreSQL budget test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for disposable PostgreSQL tests")
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	if host := strings.Trim(strings.ToLower(adminConfig.Host), "[]"); host != "localhost" && host != "127.0.0.1" && host != "::1" && net.ParseIP(host) == nil {
		t.Fatalf("refusing disposable PostgreSQL test against non-loopback host %q", adminConfig.Host)
	}
	if ip := net.ParseIP(strings.Trim(adminConfig.Host, "[]")); ip != nil && !ip.IsLoopback() {
		t.Fatalf("refusing disposable PostgreSQL test against non-loopback host %q", adminConfig.Host)
	}
	adminConfig.Database = "postgres"
	adminSQL := stdlib.OpenDB(*adminConfig)
	if err := adminSQL.PingContext(context.Background()); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("connect to local PostgreSQL admin database: %v", err)
	}
	databaseName := "hai_paid_budget_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedDatabase := `"` + databaseName + `"`
	if _, err := adminSQL.ExecContext(context.Background(), "CREATE DATABASE "+quotedDatabase); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("create isolated PostgreSQL database: %v", err)
	}
	testConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("parse isolated PostgreSQL DSN: %v", err)
	}
	testConfig.Database = databaseName
	testSQL := stdlib.OpenDB(*testConfig)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: testSQL}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_, _ = adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		_ = adminSQL.Close()
		t.Fatalf("open isolated PostgreSQL database: %v", err)
	}
	t.Cleanup(func() {
		if err := testSQL.Close(); err != nil {
			t.Errorf("close isolated PostgreSQL database: %v", err)
		}
		if _, err := adminSQL.ExecContext(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated PostgreSQL database: %v", err)
		}
		if err := adminSQL.Close(); err != nil {
			t.Errorf("close PostgreSQL administration connection: %v", err)
		}
	})
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatalf("create UUID extension in disposable DB: %v", err)
	}
	if err := db.AutoMigrate(&models.LLMGenerationRecord{}); err != nil {
		t.Fatalf("create generation history table: %v", err)
	}
	if err := db.Exec(`
CREATE FUNCTION public.test_reject_llm_generation_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'LLM generation records are append-only' USING ERRCODE = '55000';
END;
$$`).Error; err != nil {
		t.Fatalf("create append-only audit trigger function: %v", err)
	}
	if err := db.Exec(`
CREATE TRIGGER trg_test_llm_generation_records_immutable
BEFORE UPDATE OR DELETE ON public.llm_generation_records
FOR EACH ROW EXECUTE FUNCTION public.test_reject_llm_generation_mutation()`).Error; err != nil {
		t.Fatalf("create append-only audit trigger: %v", err)
	}
	if db.Dialector.Name() != "postgres" {
		t.Fatalf("unexpected database dialect %q", db.Dialector.Name())
	}
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("probe disposable database: %v", err)
	}
	return db
}

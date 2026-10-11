//go:build integration

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestOllamaAdmissionPostgresCrossInstanceAndRecovery(t *testing.T) {
	db := newOllamaAdmissionDisposablePostgres(t)
	baseRepository := NewGormModelMaintenanceRepository(db)
	base, ok := baseRepository.(*GormModelMaintenanceRepository)
	if !ok {
		t.Fatalf("model maintenance repository has unexpected type %T", baseRepository)
	}

	const fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	start := make(chan struct{})
	type acquireResult struct {
		claim    ModelMaintenanceAdmissionClaim
		acquired bool
		err      error
	}
	results := make(chan acquireResult, 2)
	for range 2 {
		go func() {
			<-start
			claim, acquired, err := base.AcquireModelMaintenanceAdmission(context.Background(), "ollama", "race-model", fingerprint, modelMaintenanceAdmissionLeaseDuration())
			results <- acquireResult{claim: claim, acquired: acquired, err: err}
		}()
	}
	close(start)
	var winner ModelMaintenanceAdmissionClaim
	acquiredCount := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("cross-instance claim: %v", result.err)
		}
		if result.acquired {
			acquiredCount++
			winner = result.claim
		}
	}
	if acquiredCount != 1 {
		t.Fatalf("concurrent independent repositories acquired %d claims, want exactly one", acquiredCount)
	}
	if err := base.FinalizeModelMaintenanceAdmission(context.Background(), winner, modelMaintenanceAdmissionRetry, time.Minute); err != nil {
		t.Fatalf("finalize race claim: %v", err)
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var firstCalls atomic.Int32
	firstServer := ollamaAdmissionPostgresServer(&firstCalls)
	defer firstServer.Close()
	provider, policy := admissionOllamaFixture(firstServer.URL)
	failingHistory := &failingPostgresModelMaintenanceHistory{GormModelMaintenanceRepository: base, err: errors.New("injected history-write failure")}
	firstService := ollamaAdmissionIntegrationService(policy, failingHistory)
	first := firstService.ensureModelFresh(provider, provider.Models[0], firstService.maintenanceEffectContext)
	if first.Status != "failed" || !first.BlocksExecution || !first.UpdateAttempted {
		t.Fatalf("first service after history failure = %#v, want blocked attempted refresh", first)
	}

	secondService := ollamaAdmissionIntegrationService(policy, base)
	second := secondService.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], &EffectContext{OwnerIdentity: "owner-b"})
	if second.Status != "failed" || !second.BlocksExecution || second.NextCheckDueAt == nil {
		t.Fatalf("fresh service and different owner = %#v, want durable retry block", second)
	}
	if firstCalls.Load() != 3 {
		t.Fatalf("provider requests after service restart = %d, want original tags/pull/tags only", firstCalls.Load())
	}

	var retryCount int64
	if err := db.Table("llm_model_maintenance_admission_claims").Where("provider_id = ? AND model_id = ? AND state = ?", provider.ID, provider.Models[0].ID, modelMaintenanceAdmissionRetryWait).Count(&retryCount).Error; err != nil {
		t.Fatalf("read durable retry claim: %v", err)
	}
	if retryCount != 1 {
		t.Fatalf("durable retry claims = %d, want one", retryCount)
	}

	var changedCalls atomic.Int32
	changedServer := ollamaAdmissionPostgresServer(&changedCalls)
	defer changedServer.Close()
	provider.EndpointURL = changedServer.URL
	policy.Providers[0].EndpointURL = changedServer.URL
	changedService := ollamaAdmissionIntegrationService(policy, base)
	changed := changedService.ensureModelFresh(provider, provider.Models[0], changedService.maintenanceEffectContext)
	if !isVerifiedLocalMaintenanceResult(provider, changed) || changedCalls.Load() != 3 {
		t.Fatalf("configuration-changed refresh = %#v, provider requests=%d", changed, changedCalls.Load())
	}

	reuseService := ollamaAdmissionIntegrationService(policy, base)
	reused := reuseService.ensureModelFresh(provider, provider.Models[0])
	if !reused.Reused || !isVerifiedLocalMaintenanceResult(provider, reused) || changedCalls.Load() != 3 {
		t.Fatalf("success reuse = %#v, provider requests=%d; want persisted history reuse", reused, changedCalls.Load())
	}

	var claims int64
	if err := db.Table("llm_model_maintenance_admission_claims").Where("provider_id = ? AND model_id = ?", provider.ID, provider.Models[0].ID).Count(&claims).Error; err != nil {
		t.Fatalf("count provider/model fingerprint claims: %v", err)
	}
	if claims != 2 {
		t.Fatalf("provider/model claims across two config fingerprints = %d, want 2", claims)
	}
}

type failingPostgresModelMaintenanceHistory struct {
	*GormModelMaintenanceRepository
	err error
}

func (r *failingPostgresModelMaintenanceHistory) RecordModelMaintenance(*models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	return nil, r.err
}

func (r *failingPostgresModelMaintenanceHistory) RecordModelMaintenanceWithContext(ctx context.Context, _ *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, r.err
}

func ollamaAdmissionIntegrationService(policy Policy, history ModelMaintenanceRepository) *Service {
	return (&Service{
		policy: policy, maintenanceHistory: history,
		maintenanceRunning: make(map[string]*sync.Mutex),
	}).WithFinalEffectAuthorization(
		FinalEffectAuthorizerFunc(func(context.Context, FinalEffectAuthorizationRequest) error { return nil }),
		EmergencyStopEvaluatorFunc(func(context.Context) (EmergencyStopState, error) { return EmergencyStopState{}, nil }),
	).WithMaintenanceEffectContext(*trustedTestEffectContext())
}

func ollamaAdmissionPostgresServer(calls *atomic.Int32) *httptest.Server {
	var tagCalls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api/tags":
			digest := "sha256:old"
			if tagCalls.Add(1) > 1 {
				digest = "sha256:new"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": digest}}})
		case "/api/pull":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func newOllamaAdmissionDisposablePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping disposable PostgreSQL admission test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for disposable PostgreSQL tests")
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	if !isLoopbackOllamaAdmissionHost(adminConfig.Host) {
		t.Fatalf("refusing PostgreSQL integration test on non-loopback host %q", adminConfig.Host)
	}
	adminConfig.Database = "postgres"
	adminSQL := stdlib.OpenDB(*adminConfig)
	if err := adminSQL.PingContext(context.Background()); err != nil {
		_ = adminSQL.Close()
		t.Fatalf("connect to local PostgreSQL admin database: %v", err)
	}
	databaseName := "hai_ollama_admission_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	if err := db.AutoMigrate(&models.LLMModelMaintenance{}); err != nil {
		t.Fatalf("create maintenance-history table in disposable DB: %v", err)
	}
	up, err := migrations.Files.ReadFile("pre/0101_llm_model_maintenance_admission_claims.up.sql")
	if err != nil {
		t.Fatalf("read admission migration: %v", err)
	}
	if err := db.Exec(string(up)).Error; err != nil {
		t.Fatalf("apply admission migration in disposable DB: %v", err)
	}
	return db
}

func isLoopbackOllamaAdmissionHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

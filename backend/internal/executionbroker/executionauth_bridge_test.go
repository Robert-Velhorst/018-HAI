package executionbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type bridgeHarness struct {
	repository *executionauth.MemoryRepository
	service    *executionauth.Service
	bridge     *DurableAuthorizationBridge
}

func newBridgeHarness(t *testing.T, owner string) bridgeHarness {
	t.Helper()
	repository := executionauth.NewMemoryRepository()
	service, err := executionauth.NewService(
		repository,
		allowLowRiskConstitution{},
		nil,
		nil,
		nil,
		func() time.Time {
			return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("new execution authorization service: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{Source: "test"}
	})
	bridge, err := NewDurableAuthorizationBridge(service, owner, "local")
	if err != nil {
		t.Fatalf("new durable bridge: %v", err)
	}
	return bridgeHarness{repository: repository, service: service, bridge: bridge}
}

type allowLowRiskConstitution struct{}

func (allowLowRiskConstitution) EvaluateExecutionPolicy(
	_ context.Context,
	owner string,
	capabilities []string,
	requiredAuthority int,
) (executionauth.ConstitutionDecision, error) {
	return executionauth.ConstitutionDecision{
		ID:               "97b06893-70f5-4a64-b9be-e19122eaa709",
		Version:          1,
		Source:           "test",
		Digest:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AuthorityCeiling: 10,
	}, nil
}

func TestProductionBrokerDiscardsCallerAuthorization(t *testing.T) {
	harness := newBridgeHarness(t, "robert")
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(
		workspace,
		"robert",
		"local",
		harness.service,
	)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	input := SafeWorkerInput{
		ArtifactName: "artifact.txt",
		Marker:       marker,
		Authorization: AuthorizationBinding{
			OwnerIdentity: "mallory",
			TaskID:        "forged-task",
			Action:        "forged.action",
			ReceiptID:     "forged",
			ReceiptDigest: "forged",
			EffectDigest:  "forged",
		},
	}

	result, err := broker.ExecuteLocalSafeWorker(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.OK || !result.Verification.Passed {
		t.Fatalf("verification = %+v", result.Verification)
	}
	receipts, err := harness.service.List(context.Background(), "robert", 10)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("owner receipts = %d err=%v", len(receipts), err)
	}
	if receipts[0].OwnerIdentity != "robert" ||
		receipts[0].TaskID == "forged-task" ||
		receipts[0].Action != LocalSafeWorkerAction {
		t.Fatalf("caller authority leaked into receipt: %+v", receipts[0])
	}
	if forged, _ := harness.service.List(context.Background(), "mallory", 10); len(forged) != 0 {
		t.Fatalf("forged owner received %d receipts", len(forged))
	}
	consumption, err := harness.service.GetConsumption(
		context.Background(),
		"robert",
		receipts[0].ID,
	)
	if err != nil {
		t.Fatalf("get consumption: %v", err)
	}
	if consumption.Consumer != LocalSafeWorkerID ||
		consumption.ExecutionTarget != consumptionTargetForInput(t, harness.bridge, workspace, input) {
		t.Fatalf("unexpected consumption: %+v", consumption)
	}
}

func TestProductionRuntimeExecuteDiscardsPayloadAuthorization(t *testing.T) {
	harness := newBridgeHarness(t, "robert")
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(
		workspace,
		"robert",
		"local",
		harness.service,
	)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	result, err := broker.SafeWorker().Execute(context.Background(), map[string]any{
		"artifactName": "runtime-artifact.txt",
		"marker":       marker,
		"authorization": map[string]any{
			"ownerIdentity": "mallory",
			"taskId":        "forged-task",
			"action":        "forged.action",
			"receiptId":     "forged",
			"receiptDigest": "forged",
			"effectDigest":  "forged",
		},
	})
	if err != nil || !result.OK {
		t.Fatalf("runtime execute result=%+v err=%v", result, err)
	}
	if forged, _ := harness.service.List(context.Background(), "mallory", 10); len(forged) != 0 {
		t.Fatalf("runtime payload created %d forged-owner receipts", len(forged))
	}
	receipts, err := harness.service.List(context.Background(), "robert", 10)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("owner receipts = %d err=%v", len(receipts), err)
	}
	if receipts[0].TaskID == "forged-task" ||
		receipts[0].Action != LocalSafeWorkerAction {
		t.Fatalf("runtime payload authority leaked into receipt: %+v", receipts[0])
	}
}

func TestVerifierOnlyRuntimeExecuteFailsClosed(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	worker := NewAuthorizedLocalSafeWorker(
		workspace,
		newTestAuthorizationVerifier(),
	)
	_, err := worker.Execute(context.Background(), map[string]any{
		"artifactName": "artifact.txt",
		"marker":       marker,
		"authorization": map[string]any{
			"ownerIdentity": "robert",
			"taskId":        "caller-task",
			"action":        LocalSafeWorkerAction,
			"receiptId":     "0fb77d98-e0f8-4c79-a381-862012a9ac1d",
			"receiptDigest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"effectDigest":  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	})
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("error = %v, want ErrAuthorizationRequired", err)
	}
	assertPathAbsent(t, workspace)
}

func TestProductionBridgeRejectsWrongOwnerTargetAndEffect(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AuthorizationVerification)
	}{
		{
			name: "owner",
			mutate: func(value *AuthorizationVerification) {
				value.Binding.OwnerIdentity = "mallory"
			},
		},
		{
			name: "target",
			mutate: func(value *AuthorizationVerification) {
				value.ExecutionTarget = filepath.Join(
					value.Effect.WorkspaceRoot,
					"other.txt",
				)
			},
		},
		{
			name: "effect",
			mutate: func(value *AuthorizationVerification) {
				value.Effect.PayloadDigest =
					"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newBridgeHarness(t, "robert")
			workspace := filepath.Join(t.TempDir(), "not-created")
			prepared, effect, err := harness.bridge.prepareInput(
				workspace,
				SafeWorkerInput{ArtifactName: "artifact.txt", Marker: marker},
			)
			if err != nil {
				t.Fatalf("prepare input: %v", err)
			}
			prepared, err = harness.bridge.Issue(
				context.Background(),
				workspace,
				prepared,
			)
			if err != nil {
				t.Fatalf("issue: %v", err)
			}
			effect, err = buildFinalEffect(workspace, prepared)
			if err != nil {
				t.Fatalf("build final effect: %v", err)
			}
			verification := AuthorizationVerification{
				Binding:         prepared.Authorization,
				Effect:          effect,
				Consumer:        LocalSafeWorkerID,
				ExecutionTarget: filepath.Join(effect.WorkspaceRoot, effect.ArtifactName),
			}
			test.mutate(&verification)

			if _, err := harness.bridge.VerifyAndConsume(
				context.Background(),
				verification,
			); !errors.Is(err, ErrAuthorizationMismatch) {
				t.Fatalf("error = %v, want ErrAuthorizationMismatch", err)
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestProductionAuthorizationIsSingleUse(t *testing.T) {
	harness := newBridgeHarness(t, "robert")
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(workspace, "robert", "local", harness.service)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	input := SafeWorkerInput{ArtifactName: "artifact.txt", Marker: marker}
	if _, err := broker.ExecuteLocalSafeWorker(context.Background(), input); err != nil {
		t.Fatalf("first execution: %v", err)
	}
	if _, err := broker.ExecuteLocalSafeWorker(
		context.Background(),
		input,
	); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("replay error = %v, want ErrAuthorizationDenied", err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, input.ArtifactName))
	if err != nil || string(data) != marker {
		t.Fatalf("artifact changed after replay: data=%q err=%v", data, err)
	}
}

func TestProductionBridgeRechecksEmergencyStopAtFinalBoundary(t *testing.T) {
	harness := newBridgeHarness(t, "robert")
	var stopped atomic.Bool
	harness.service.WithEmergencyStopEvaluator(
		func() executionauth.EmergencyStopEvidence {
			return executionauth.EmergencyStopEvidence{
				Active: stopped.Load(),
				Source: "test-emergency-stop",
				Reason: "operator stop",
			}
		},
	)
	workspace := filepath.Join(t.TempDir(), "not-created")
	prepared, err := harness.bridge.Issue(
		context.Background(),
		workspace,
		SafeWorkerInput{ArtifactName: "artifact.txt", Marker: marker},
	)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	stopped.Store(true)

	worker := NewAuthorizedLocalSafeWorker(workspace, harness.bridge)
	if _, err := worker.Run(
		context.Background(),
		prepared,
	); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("execution error = %v, want ErrAuthorizationDenied", err)
	}
	assertPathAbsent(t, workspace)
}

func TestProductionBridgePostgresDurableConsumption(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	repository := executionauth.NewPostgresRepository(db)
	owner := "bridge-postgres-" + uuid.NewString()
	frameworks, err := frameworkregistry.NewService(
		frameworkregistry.NewGormRepository(db),
	)
	if err != nil {
		t.Fatalf("new framework registry: %v", err)
	}
	draft, err := frameworks.CreateConstitutionDraft(
		owner,
		frameworkregistry.ConstitutionDraftRequest{
			BaseVersion:   1,
			ChangeSummary: "Postgres bridge integration policy fixture",
		},
	)
	if err != nil {
		t.Fatalf("create Constitution fixture: %v", err)
	}
	if _, err := frameworks.ActivateConstitution(
		owner,
		draft.ID,
		owner,
		frameworkregistry.ActivateConstitutionRequest{
			Confirmation: "ACTIVATE CONSTITUTION",
			ApprovalNote: "Explicit test-only integration approval",
		},
	); err != nil {
		t.Fatalf("activate Constitution fixture: %v", err)
	}
	constitution, err := executionauth.NewConstitutionPolicyAdapter(frameworks)
	if err != nil {
		t.Fatalf("new Constitution adapter: %v", err)
	}
	service, err := executionauth.NewService(
		repository,
		constitution,
		nil,
		nil,
		nil,
		time.Now,
	)
	if err != nil {
		t.Fatalf("new execution authorization service: %v", err)
	}
	service.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{Source: "postgres-test"}
	})
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(workspace, owner, "local", service)
	if err != nil {
		t.Fatalf("new authorized broker: %v", err)
	}
	if _, err := broker.ExecuteLocalSafeWorker(
		context.Background(),
		SafeWorkerInput{ArtifactName: "artifact.txt", Marker: marker},
	); err != nil {
		t.Fatalf("execute: %v", err)
	}
	receipts, err := service.List(context.Background(), owner, 10)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("durable receipts = %d err=%v", len(receipts), err)
	}
	consumption, err := service.GetConsumption(
		context.Background(),
		owner,
		receipts[0].ID,
	)
	if err != nil {
		t.Fatalf("durable consumption: %v", err)
	}
	if consumption.ReceiptID != receipts[0].ID ||
		consumption.OwnerIdentity != owner ||
		consumption.Consumer != LocalSafeWorkerID {
		t.Fatalf("unexpected durable consumption: %+v", consumption)
	}
}

func consumptionTargetForInput(
	t *testing.T,
	bridge *DurableAuthorizationBridge,
	workspace string,
	input SafeWorkerInput,
) string {
	t.Helper()
	prepared, effect, err := bridge.prepareInput(workspace, input)
	if err != nil {
		t.Fatalf("prepare consumption target: %v", err)
	}
	if prepared.Authorization.EffectDigest != effect.EffectDigest {
		t.Fatal("prepared effect binding mismatch")
	}
	return consumptionTarget(effect)
}

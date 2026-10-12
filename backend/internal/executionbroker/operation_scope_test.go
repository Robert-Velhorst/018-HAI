package executionbroker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func TestManagedWorkerRejectsUnscopedLegacyReceiptBeforeVerifier(t *testing.T) {
	for _, input := range []struct{ name, artifact, marker string }{
		{"artifact", "operation-old.txt", "ordinary marker"},
		{"marker", "ordinary.txt", "HAI-OP old rev one"},
	} {
		t.Run(input.name, func(t *testing.T) {
			workspace := filepath.Join(t.TempDir(), "workspace")
			verifier := newTestAuthorizationVerifier()
			worker := NewAuthorizedLocalSafeWorker(workspace, verifier)
			in := authorizedInput(t, workspace, input.artifact, input.marker)
			out, err := worker.Run(context.Background(), in)
			if !errors.Is(err, ErrAuthorizationRequired) || out.Progress != (SafeWorkerProgress{}) || verifier.callCount.Load() != 0 {
				t.Fatalf("legacy managed receipt reached verifier/effect: %+v / %v / calls=%d", out, err, verifier.callCount.Load())
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func runningOperationScopeFixture(t *testing.T) (*operations.Service, models.Operation, operations.ExecutionClaim) {
	t.Helper()
	service := operations.NewService(operations.NewMemoryRepository())
	origin := uuid.MustParse("00000000-0000-4000-8000-000000000903")
	in := operations.NewOperationInput{
		OwnerUserID: "robert", WorkspaceID: "local", Title: "bounded local record",
		OperationType: "organize", SourceType: "file", DedupeKey: "scoped-artifact",
		SourceProvider: "local", SourceAccount: "owner-folder", SourceExternalID: "record:one",
		SourceRevisionHash: "revision-one",
		AccountFeedID:      &origin,
	}
	var created operations.IngestResult
	err := service.WithSourceObservation(context.Background(), operations.SourceObservationStart{OwnerUserID: in.OwnerUserID, WorkspaceID: in.WorkspaceID, OriginID: origin, ConfigDigest: strings.Repeat("a", 64)}, func(ctx context.Context) error {
		var err error
		created, err = service.IngestContext(ctx, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	op := created.Operation
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.RiskLevel, op.AutonomyLevel, op.OwnerType = string(operations.RiskLow), string(operations.AutonomyAuto), string(operations.OwnerHAI)
	op.RuntimeID, op.VerificationStatus = LocalSafeWorkerID, string(operations.VerificationPending)
	classified, err := service.Transition(op, operations.StatusClassified, "hai", "", "prepare safe test")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := service.Transition(*classified, operations.StatusReady, "hai", "", "ready for safe test")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	running, err := service.TransitionClaimed(context.Background(), claimed.Claim, *ready, operations.StatusRunning, "hai", "", "persist before test effect")
	if err != nil {
		t.Fatal(err)
	}
	return service, *running, claimed.Claim
}

func scopedTestInput(op models.Operation) SafeWorkerInput {
	name, marker := scopedSafeWorkerArtifact(operations.SafeEffectScope{
		OperationID: op.ID, SourceRevisionHash: op.SourceRevisionHash,
	})
	return SafeWorkerInput{ArtifactName: name, Marker: marker}
}

func TestProductionBridgeBindsExactLockedOperationAndClaim(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	harness := newBridgeHarness(t, op.OwnerUserID)
	workspace := filepath.Join(t.TempDir(), "not-created")
	var prepared SafeWorkerInput
	var retained context.Context
	err := service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
		retained = ctx
		var err error
		prepared, err = harness.bridge.Issue(ctx, workspace, scopedTestInput(op))
		if err != nil {
			return err
		}
		scope, active := operations.CurrentSafeEffectScope(ctx)
		if !active || prepared.Authorization.OperationScope == nil || *prepared.Authorization.OperationScope != scope ||
			scope.OperationID != op.ID || scope.Version != op.Version || scope.ClaimOwner != claim.Owner || scope.ClaimGeneration != claim.Generation ||
			scope.SourceIdentityHash != op.SourceIdentityHash || scope.SourceRevisionHash != op.SourceRevisionHash {
			t.Fatal("authorization did not bind the exact locked record and claim")
		}
		effect, err := buildFinalEffect(workspace, prepared)
		if err != nil || effect.ContractVersion != operationAuthorizationContractVersion || !sameOperationScope(effect.OperationScope, &scope) {
			t.Fatalf("effect contract: %#v %v", effect, err)
		}
		request := harness.bridge.authorizationRequest(effect)
		if !strings.Contains(strings.Join(request.SourceReferences, ","), fmt.Sprintf("operation:%s:version:%d", op.ID, op.Version)) ||
			!strings.Contains(strings.Join(request.SourceReferences, ","), fmt.Sprintf("claim:%s:generation:%d", claim.Owner, claim.Generation)) {
			t.Fatal("durable request omitted operation/claim provenance")
		}
		mutations := []func(*operations.SafeEffectScope){
			func(s *operations.SafeEffectScope) { s.OperationID = uuid.New() },
			func(s *operations.SafeEffectScope) { s.Version++ },
			func(s *operations.SafeEffectScope) { s.OwnerUserID = "other-owner" },
			func(s *operations.SafeEffectScope) { s.WorkspaceID = "other-workspace" },
			func(s *operations.SafeEffectScope) { s.ClaimOwner = uuid.New() },
			func(s *operations.SafeEffectScope) { s.ClaimGeneration++ },
			func(s *operations.SafeEffectScope) { s.SourceIdentityHash = strings.Repeat("b", 64) },
			func(s *operations.SafeEffectScope) { s.SourceRevisionHash = "different-revision" },
		}
		for i, mutate := range mutations {
			changed, copyScope := prepared, scope
			mutate(&copyScope)
			changed.Authorization.OperationScope = &copyScope
			digest, err := BindLocalSafeWorkerEffect(workspace, changed)
			if err != nil || digest == prepared.Authorization.EffectDigest {
				t.Fatalf("mutation %d did not change effect digest: %v", i, err)
			}
			if _, err := buildFinalEffect(workspace, changed); !errors.Is(err, ErrAuthorizationMismatch) {
				t.Fatalf("mutation %d retained original effect authority: %v", i, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, active := operations.CurrentSafeEffectScope(retained); active {
		t.Fatal("retained context kept authority after lock release")
	}
	worker := NewAuthorizedLocalSafeWorker(workspace, harness.bridge)
	if _, err := worker.Run(retained, prepared); err == nil {
		t.Fatal("expired callback scope permitted filesystem creation")
	}
	if _, err := worker.Run(context.Background(), prepared); !errors.Is(err, ErrAuthorizationMismatch) {
		t.Fatalf("scoped receipt replay outside locked boundary: %v", err)
	}
	assertPathAbsent(t, workspace)
}

func TestProductionBrokerExecutesOnlyInsideMatchingOperationScope(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	harness := newBridgeHarness(t, op.OwnerUserID)
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(workspace, op.OwnerUserID, op.WorkspaceID, harness.service)
	if err != nil {
		t.Fatal(err)
	}
	var result ExecutionResult
	var expectedDigest string
	err = service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
		input := scopedTestInput(op)
		input.Authorization.OperationScope = &operations.SafeEffectScope{OperationID: uuid.New(), Version: 999}
		expected, _, err := harness.bridge.prepareInput(workspace, input)
		if err != nil {
			return err
		}
		scope, active := operations.CurrentSafeEffectScope(ctx)
		if !active {
			t.Fatal("server scope missing")
		}
		expected.Authorization.OperationScope = &scope
		expectedDigest, err = BindLocalSafeWorkerEffect(workspace, expected)
		if err != nil {
			return err
		}
		result, err = broker.ExecuteLocalSafeWorker(ctx, input)
		return err
	})
	if err != nil || !result.OK || !result.Verification.Passed || result.Output.Progress.Authorization != "consumed" || !result.Output.Progress.FileClosed {
		t.Fatalf("actual bounded file path: %#v %v", result, err)
	}
	receipts, err := harness.service.List(context.Background(), op.OwnerUserID, 10)
	if err != nil || len(receipts) != 1 || receipts[0].EffectDigest != expectedDigest || receipts[0].ResourceID != expectedDigest {
		t.Fatalf("durable scoped receipt: %#v %v", receipts, err)
	}
}

func TestProductionBridgeRejectsManagedNamespaceWithoutLockedScope(t *testing.T) {
	for _, input := range []SafeWorkerInput{
		{ArtifactName: "operation-forged.txt", Marker: "ordinary content"},
		{ArtifactName: "ordinary.txt", Marker: "HAI-OP forged managed marker"},
	} {
		t.Run(input.ArtifactName, func(t *testing.T) {
			harness := newBridgeHarness(t, "robert")
			workspace := filepath.Join(t.TempDir(), "not-created")
			if _, err := harness.bridge.Issue(context.Background(), workspace, input); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatalf("managed namespace issued without server scope: %v", err)
			}
			if rows, err := harness.service.List(context.Background(), "robert", 10); err != nil || len(rows) != 0 {
				t.Fatalf("refused request created authority: %#v %v", rows, err)
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestProductionBridgeOnlyAuthorizesCanonicalOperationArtifact(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*SafeWorkerInput)
	}{
		{
			name: "artifact substitution",
			mutate: func(input *SafeWorkerInput) {
				input.ArtifactName = "operation-substituted.txt"
			},
		},
		{
			name: "marker substitution",
			mutate: func(input *SafeWorkerInput) {
				input.Marker = "HAI-OP substituted rev source"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, op, claim := runningOperationScopeFixture(t)
			harness := newBridgeHarness(t, op.OwnerUserID)
			workspace := filepath.Join(t.TempDir(), "not-created")
			input := scopedTestInput(op)
			test.mutate(&input)
			err := service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
				_, err := harness.bridge.Issue(ctx, workspace, input)
				return err
			})
			if !errors.Is(err, ErrAuthorizationMismatch) {
				t.Fatalf("non-canonical operation effect issued authority: %v", err)
			}
			if receipts, err := harness.service.List(context.Background(), op.OwnerUserID, 10); err != nil || len(receipts) != 0 {
				t.Fatalf("refusal persisted a receipt: %#v / %v", receipts, err)
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestFinalBoundaryRejectsReboundScopedArtifactAndPayload(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	harness := newBridgeHarness(t, op.OwnerUserID)
	workspace := filepath.Join(t.TempDir(), "not-created")
	var prepared SafeWorkerInput
	err := service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
		var err error
		prepared, err = harness.bridge.Issue(ctx, workspace, scopedTestInput(op))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	effect, err := buildFinalEffect(workspace, prepared)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*FinalEffect)
	}{
		{
			name: "artifact substitution",
			mutate: func(effect *FinalEffect) {
				effect.ArtifactName = "operation-substituted.txt"
			},
		},
		{
			name: "payload substitution",
			mutate: func(effect *FinalEffect) {
				effect.PayloadDigest = strings.Repeat("b", 64)
			},
		},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			changed := effect
			test.mutate(&changed)
			changed.TaskID = deriveTaskID(changed.WorkspaceRoot, changed.ArtifactName, changed.PayloadDigest)
			changed.EffectDigest, err = digestFinalEffect(changed)
			if err != nil {
				t.Fatal(err)
			}
			binding := prepared.Authorization
			binding.TaskID, binding.EffectDigest = changed.TaskID, changed.EffectDigest
			verification := AuthorizationVerification{
				Binding: binding, Effect: changed, Consumer: LocalSafeWorkerID,
				ExecutionTarget: filepath.Join(changed.WorkspaceRoot, changed.ArtifactName),
			}
			if _, err := harness.bridge.verifyFinalBoundary(verification); !errors.Is(err, ErrAuthorizationMismatch) {
				t.Fatalf("rebound non-canonical effect accepted: %v", err)
			}
		})
	}
}

func TestActiveOperationScopeCannotBeDowngradedToAdHocAuthority(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	harness := newBridgeHarness(t, op.OwnerUserID)
	workspace := filepath.Join(t.TempDir(), "not-created")
	input, err := harness.bridge.Issue(context.Background(), workspace, SafeWorkerInput{ArtifactName: "ordinary.txt", Marker: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	err = service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
		_, err := NewAuthorizedLocalSafeWorker(workspace, harness.bridge).Run(ctx, input)
		return err
	})
	if !errors.Is(err, ErrAuthorizationMismatch) {
		t.Fatalf("active operation used unbound ad-hoc receipt: %v", err)
	}
	assertPathAbsent(t, workspace)
}

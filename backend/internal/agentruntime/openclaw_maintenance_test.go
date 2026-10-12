package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestOpenClawMaintenanceGateMissingBlocksDirectTask(t *testing.T) {
	a := openClawMaintenanceTestAdapter()
	result := a.ExecuteTask(context.Background(), Task{ID: "task-1", Prompt: "inspect safely"})
	if result.Status != "blocked" || result.Message != openClawMaintenanceGateMissingReason ||
		!containsString(result.AuditEvents, "OpenClaw execution rejected because maintenance admission is unavailable") {
		t.Fatalf("missing maintenance gate did not fail closed: %#v", result)
	}
	if len(a.gatewayReceiptStore.(*fakeOpenClawGatewayReceiptStore).receipts) != 0 {
		t.Fatal("missing maintenance gate reached Gateway admission")
	}
}

func TestOpenClawMaintenanceGateBlocksTask(t *testing.T) {
	a := openClawMaintenanceTestAdapter()
	a.maintenanceGate = func(context.Context) (func(), error) { return nil, errors.New("maintenance") }
	result := a.ExecuteTask(context.Background(), Task{})
	if result.Status != "blocked" || result.Message != "OpenClaw maintenance must complete or be reviewed before starting a task" {
		t.Fatalf("unexpected admission: %+v", result)
	}
}

func TestOpenClawMaintenanceGateWithoutLeaseFailsClosed(t *testing.T) {
	a := openClawMaintenanceTestAdapter()
	a.maintenanceGate = func(context.Context) (func(), error) { return nil, nil }
	result := a.ExecuteTask(context.Background(), Task{ID: "task-1", Prompt: "inspect safely"})
	if result.Status != "blocked" || result.Message != "OpenClaw maintenance must complete or be reviewed before starting a task" {
		t.Fatalf("gate without an acquired lease did not fail closed: %#v", result)
	}
}

func TestOpenClawMaintenanceGateReleasesOnEarlyReturn(t *testing.T) {
	released := 0
	a := openClawMaintenanceTestAdapter()
	a.maintenanceGate = func(context.Context) (func(), error) { return func() { released++ }, nil }
	a.ExecuteTask(context.Background(), Task{})
	if released != 1 {
		t.Fatalf("released %d times", released)
	}
}

func TestRegistryBlocksExecutableOpenClawWithoutMaintenanceGateBeforeProofUse(t *testing.T) {
	adapter := &fakeAdapter{info: Info{
		ID: "openclaw", Enabled: true, Configured: true, ExecutionEnabled: true,
	}}
	verifierCalls := 0
	registry := NewRegistryWithFinalEffectVerifier(
		FinalEffectProofVerifierFunc(func(_ context.Context, request FinalEffectAuthorizationRequest, proof FinalEffectAuthorizationProof) error {
			verifierCalls++
			return verifyTestFinalEffectProof(request, proof)
		}),
		adapter,
	)
	task := withValidFinalEffectProof("openclaw", Task{
		ID: "task-1", Prompt: "inspect safely", OwnerIdentity: "alice",
	}, adapter.info)
	result := registry.Execute(context.Background(), "openclaw", task)
	if result.Status != "blocked" || result.Message != openClawMaintenanceGateMissingReason || adapter.called || verifierCalls != 0 {
		t.Fatalf("registry bypassed missing admission gate or consumed proof: result=%#v called=%t verifierCalls=%d", result, adapter.called, verifierCalls)
	}
}

func TestReferenceOpenClawRegistryRemainsConstructibleWithoutMaintenanceGate(t *testing.T) {
	adapter := &fakeAdapter{info: Info{ID: "openclaw", Name: "OpenClaw reference", ReadOnlyDefault: true}}
	registry := NewRegistry(adapter)
	result := registry.Execute(context.Background(), "openclaw", Task{ID: "task-1", Prompt: "inspect safely", OwnerIdentity: "alice"})
	if result.Status != "blocked" || result.Message == openClawMaintenanceGateMissingReason || adapter.called {
		t.Fatalf("non-executing reference registry changed behavior: result=%#v called=%t", result, adapter.called)
	}
}

func openClawMaintenanceTestAdapter() *openClawAdapter {
	return &openClawAdapter{
		gatewayEnabled: true, gatewayDelegationEnabled: true,
		gatewayURL: "ws://127.0.0.1:18789", gatewayDelegationToken: "delegation-token",
		gatewayReceiptStore: &fakeOpenClawGatewayReceiptStore{},
		allowedHost:         map[string]bool{"127.0.0.1": true}, sandboxRequired: true,
	}
}

package agentruntime

import (
	"context"
	"fmt"
	"strings"
)

func (r *Registry) OpenClawArtifactRecoveryReady() bool {
	if r == nil {
		return false
	}
	a, ok := r.adapters["openclaw"].(*openClawAdapter)
	if !ok || !a.gatewayArtifactImportEnabled || !a.gatewayEnabled || a.gatewayReceiptStore == nil || strings.TrimSpace(a.gatewayToken) == "" || a.validGatewayURL() != "" {
		return false
	}
	_, blocked := emergencyStopResult("openclaw")
	return !blocked
}

// This reader never persists, downloads contents, or starts an agent. The
// recovery repository commits descriptors, audit and lease completion together.
func (r *Registry) ReadOpenClawGatewayArtifactDescriptors(ctx context.Context, taskID, reference string) ([]GatewayArtifactDescriptor, error) {
	if !r.OpenClawArtifactRecoveryReady() {
		return nil, fmt.Errorf("OpenClaw artifact reader unavailable")
	}
	a := r.adapters["openclaw"].(*openClawAdapter)
	receipt, err := a.openClawGatewayReceiptForReference(strings.TrimSpace(taskID), reference)
	if err != nil || receipt.TerminalStatus != "completed" || receipt.TerminalAt.IsZero() || !validOpenClawGatewayRunID(receipt.RunID) {
		return nil, fmt.Errorf("OpenClaw artifacts require a stored completed receipt")
	}
	return a.readArtifactDescriptors(ctx, receipt)
}

func (a *openClawAdapter) readArtifactDescriptors(ctx context.Context, receipt OpenClawGatewayReceipt) ([]GatewayArtifactDescriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	connection, features, err := a.openClawGatewayOperatorReadConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("OpenClaw artifact read could not connect")
	}
	defer connection.Close()
	if !containsExact(features, "artifacts.list") {
		return nil, fmt.Errorf("OpenClaw artifact read capability unavailable")
	}
	payload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "artifacts.list", map[string]any{"runId": receipt.RunID})
	if err != nil {
		return nil, fmt.Errorf("OpenClaw artifact read unavailable")
	}
	if len(payload) > defaultOutputLimit || validateOpenClawArtifactJSON(payload) != nil {
		return nil, fmt.Errorf("invalid OpenClaw artifact list")
	}
	return openClawGatewayArtifactDescriptorsFromResponse(payload, receipt.RunID)
}

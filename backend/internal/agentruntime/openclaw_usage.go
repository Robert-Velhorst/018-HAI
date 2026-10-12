package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Session usage is a dated instance snapshot, not a run bill or budget debit.
// Only bounded usage data crosses into HAI; transcript paths and labels do not.
func (a *openClawAdapter) sessionUsageAudit(ctx context.Context, receipt OpenClawGatewayReceipt, finished time.Time) string {
	const unavailable = "OpenClaw session usage unavailable; no zero usage or billed amount was inferred"
	summary, err := a.readSessionUsage(ctx, receipt, finished)
	if err != nil {
		return unavailable
	}
	return summary
}

// OpenClawUsageRecoveryReady preserves retry allowances while paused or unconfigured.
func (r *Registry) OpenClawUsageRecoveryReady() bool {
	a, ok := r.adapters["openclaw"].(*openClawAdapter)
	if !ok || !a.gatewayEnabled || strings.TrimSpace(a.gatewayToken) == "" || a.validGatewayURL() != "" {
		return false
	}
	_, blocked := emergencyStopResult("openclaw")
	return !blocked
}

// ReadOpenClawGatewaySessionUsage uses the persisted terminal outcome; it cannot
// execute a task or infer completion.
func (r *Registry) ReadOpenClawGatewaySessionUsage(ctx context.Context, taskID, reference string) (string, error) {
	snapshot, err := r.ReadOpenClawGatewayUsageSnapshot(ctx, taskID, reference)
	if err != nil {
		return "", err
	}
	return snapshot.AuditSummary(), nil
}

func (r *Registry) ReadOpenClawGatewayUsageSnapshot(ctx context.Context, taskID, reference string) (*GatewaySessionUsageSnapshot, error) {
	a, ok := r.adapters["openclaw"].(*openClawAdapter)
	if !ok {
		return nil, fmt.Errorf("OpenClaw usage reader unavailable")
	}
	receipt, err := a.openClawGatewayReceiptForReference(taskID, reference)
	if err != nil || (receipt.TerminalStatus != "completed" && receipt.TerminalStatus != "failed") || receipt.TerminalAt.IsZero() {
		return nil, fmt.Errorf("OpenClaw usage requires a stored terminal receipt")
	}
	if _, blocked := emergencyStopResult("openclaw"); blocked {
		return nil, fmt.Errorf("OpenClaw usage blocked by emergency stop")
	}
	return a.readSessionUsageSnapshot(ctx, receipt, receipt.TerminalAt)
}

func (a *openClawAdapter) readSessionUsage(ctx context.Context, receipt OpenClawGatewayReceipt, finished time.Time) (string, error) {
	snapshot, err := a.readSessionUsageSnapshot(ctx, receipt, finished)
	if err != nil {
		return "", err
	}
	return snapshot.AuditSummary(), nil
}

func (a *openClawAdapter) readSessionUsageSnapshot(ctx context.Context, receipt OpenClawGatewayReceipt, finished time.Time) (*GatewaySessionUsageSnapshot, error) {
	unavailable := fmt.Errorf("OpenClaw session usage unavailable")
	if receipt.SessionID == "" || receipt.CreatedAt.IsZero() || finished.IsZero() || finished.Before(receipt.CreatedAt) || !a.gatewayEnabled || strings.TrimSpace(a.gatewayToken) == "" {
		return nil, unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	connection, features, err := a.openClawGatewayOperatorReadConnection(ctx)
	if err != nil {
		return nil, unavailable
	}
	defer connection.Close()
	if !containsExact(features, "sessions.usage") {
		return nil, unavailable
	}
	payload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "sessions.usage", map[string]any{
		"key": receipt.SessionKey, "groupBy": "instance", "includeHistorical": false,
		"limit": 1, "includeContextWeight": false,
		"startDate": receipt.CreatedAt.UTC().Format("2006-01-02"), "endDate": finished.UTC().Format("2006-01-02"),
	})
	if err != nil {
		return nil, unavailable
	}
	snapshot, err := openClawSessionUsageSnapshot(payload, receipt, finished, time.Now().UTC())
	if err != nil {
		return nil, unavailable
	}
	return snapshot, nil
}

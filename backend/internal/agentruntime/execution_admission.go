package agentruntime

import (
	"context"
	"errors"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/safety"
)

var errRuntimeAdmissionCancelled = errors.New("runtime admission cancelled before start")

// withExecutionAdmission linearizes a final persisted-stop check with one
// short runtime admission operation. The callback may only persist a durable
// host-job admission or call exec.Cmd.Start; provider, network, process wait,
// and other long-running work must remain outside this fence.
func withExecutionAdmission(parent context.Context, runtimeID string, admit func(context.Context) error) (Result, bool, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, hostruntime.AdmissionTimeout)
	defer cancel()
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return Result{}, false, err
	}
	defer releaseFence()

	if result, blocked := emergencyStopResult(runtimeID); blocked {
		result.AuditEvents = append(result.AuditEvents, "runtime admission was not performed because the emergency-stop control is active or unavailable")
		return result, true, nil
	}
	if admit == nil {
		return blockedRuntimeResult(runtimeID, "runtime admission operation is unavailable", "runtime admission failed closed without an admission callback"), true, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, false, err
	}
	if err := admit(ctx); err != nil {
		return Result{}, false, err
	}
	return Result{}, false, nil
}

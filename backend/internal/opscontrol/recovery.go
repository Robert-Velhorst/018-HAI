package opscontrol

import (
	"context"
	"fmt"
	"time"

	"automation-hub-backend/internal/operations"
)

// RecoveryReport summarizes a crash/reboot recovery pass.
type RecoveryReport struct {
	ScannedRunning    int       `json:"scannedRunning"`
	ScannedVerifying  int       `json:"scannedVerifying"`
	Recovered         int       `json:"recovered"`
	LiveRunning       int       `json:"liveRunning"`
	LiveVerifying     int       `json:"liveVerifying"`
	UnleasedRunning   int       `json:"unleasedRunning"`
	UnleasedVerifying int       `json:"unleasedVerifying"`
	ExpiredRemaining  int       `json:"expiredClaimsRemaining"`
	Details           []string  `json:"details,omitempty"`
	RanAt             time.Time `json:"ranAt"`
}

// Recover reconciles only executing operations whose durable worker lease has
// expired. Live leases are preserved; rows without a claim remain untouched
// and visible for manual reconciliation. No side effect is automatically run.
func Recover(ctx context.Context, svc *operations.Service, ownerUserID, workspaceID string, now time.Time) (RecoveryReport, error) {
	rep := RecoveryReport{RanAt: now.UTC()}
	result, err := svc.RecoverExpiredClaims(ctx, ownerUserID, workspaceID, 200)
	if err != nil {
		return rep, fmt.Errorf("recover expired operation claims: %w", err)
	}
	rep.ScannedRunning = result.ScannedRunning
	rep.ScannedVerifying = result.ScannedVerifying
	rep.Recovered = result.Recovered
	rep.LiveRunning = result.LiveRunning
	rep.LiveVerifying = result.LiveVerifying
	rep.UnleasedRunning = result.UnleasedRunning
	rep.UnleasedVerifying = result.UnleasedVerifying
	rep.ExpiredRemaining = result.ExpiredClaimsRemain
	rep.Details = append(rep.Details, result.Details...)
	return rep, nil
}

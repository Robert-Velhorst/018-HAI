package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApprovalProofSignerEnforcesAbsoluteDeadlineBeforeReservation(t *testing.T) {
	now := time.Date(2026, time.October, 24, 12, 0, 0, 123456000, time.UTC)
	for _, test := range []struct {
		name       string
		notAfter   time.Time
		wantExpiry time.Time
		reject     bool
	}{
		{name: "no bound", wantExpiry: now.Add(defaultApprovalProofTTL)},
		{name: "later bound cannot extend TTL", notAfter: now.Add(2 * defaultApprovalProofTTL), wantExpiry: now.Add(defaultApprovalProofTTL)},
		{name: "shorter bound", notAfter: now.Add(time.Second), wantExpiry: now.Add(time.Second)},
		{name: "UTC and precision", notAfter: now.In(time.FixedZone("offset", 3600)).Add(time.Second + 123*time.Nanosecond), wantExpiry: now.Add(time.Second)},
		{name: "minimum precision", notAfter: now.Add(approvalProofTimestampPrecision), wantExpiry: now.Add(approvalProofTimestampPrecision)},
		{name: "expired", notAfter: now.Add(-time.Second), reject: true},
		{name: "equal", notAfter: now, reject: true},
		{name: "below precision", notAfter: now.Add(time.Nanosecond), reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			signer := newApprovalProofTestService(t, func() time.Time { return now })
			request := ApprovalProofIssueRequest{
				OwnerIdentity: "alice", AutomationID: uuid.New(),
				ActionDigest: approvalTestDigest("deadline-bound action"),
				Scope:        ApprovalScopeAPIMutate, ApprovalSourceID: "task-review:" + uuid.NewString(),
				NotAfter: test.notAfter,
			}
			proof, err := signer.Issue(request)
			if test.reject {
				if err == nil || proof != nil || !strings.Contains(err.Error(), "deadline") {
					t.Fatalf("invalid deadline returned a proof or no error: hasProof=%t error=%v", proof != nil, err)
				}
				// Reusing the source verifies rejection did not reserve an issuance.
				request.NotAfter = time.Time{}
				if _, retryErr := signer.Issue(request); retryErr != nil {
					t.Fatalf("rejected deadline consumed the approval source: %v", retryErr)
				}
				return
			}
			if err != nil || proof == nil {
				t.Fatalf("Issue: hasProof=%t error=%v", proof != nil, err)
			}
			if !proof.ExpiresAt.Equal(test.wantExpiry) {
				t.Fatalf("proof expiry=%s, want %s", proof.ExpiresAt, test.wantExpiry)
			}
			if err := signer.VerifyAndConsume(context.Background(), proof, ApprovalProofExpectation{
				OwnerIdentity: request.OwnerIdentity, AutomationID: request.AutomationID,
				ActionDigest: request.ActionDigest, Scope: request.Scope,
				ApprovalSourceID: request.ApprovalSourceID,
			}); err != nil {
				t.Fatalf("deadline-bound proof failed signature or durable consumption verification: %v", err)
			}
		})
	}
}

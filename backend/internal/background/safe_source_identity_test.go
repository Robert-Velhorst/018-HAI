package background

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

func TestSafeExecutionRejectsMalformedOrForgedSourceIdentityBeforeDispatch(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		for _, test := range []struct {
			name string
			edit func(*models.Operation)
			want error
		}{
			{"partial tuple", func(op *models.Operation) { op.SourceProvider = "gmail" }, operations.ErrInvalidSourceIdentity},
			{"incorrect digest", func(op *models.Operation) {
				op.SourceProvider, op.SourceAccount, op.SourceExternalID = "gmail", "primary", "message-1"
				op.SourceIdentityHash, op.SourceRevisionHash = "forged", "revision-1"
			}, operations.ErrInvalidSourceIdentity},
			{"missing revision", func(op *models.Operation) {
				op.SourceProvider, op.SourceAccount, op.SourceExternalID = "gmail", "primary", "message-1"
				op.SourceIdentityHash, _ = operations.SourceIdentityDigest("gmail", "primary", "message-1")
				op.SourceRevisionHash = ""
			}, operations.ErrInvalidSourceIdentity},
			{"well formed but not stored", func(op *models.Operation) {
				op.SourceProvider, op.SourceAccount, op.SourceExternalID = "gmail", "primary", "message-1"
				op.SourceIdentityHash, _ = operations.SourceIdentityDigest("gmail", "primary", "message-1")
				op.SourceRevisionHash = "revision-1"
			}, operations.ErrSourceIdentityImmutable},
		} {
			name := "unclaimed/" + test.name
			if claimed {
				name = "claimed/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				svc := operations.NewService(operations.NewMemoryRepository())
				stored := createReadyRegressionOperation(t, svc)
				eventsBefore, err := svc.Events(stored.ID)
				if err != nil {
					t.Fatal(err)
				}
				var claim *operations.ExecutionClaim
				if claimed {
					lease, err := svc.ClaimOperation(t.Context(), stored.OwnerUserID, stored.WorkspaceID, stored.ID, uuid.New(), operationClaimLease)
					if err != nil || lease == nil {
						t.Fatalf("claim: %+v %v", lease, err)
					}
					claim = &lease.Claim
				}
				input := stored
				test.edit(&input)
				executor := &outcomeSafeExecutor{result: verifiedOutcomeReceipt()}
				outcome, err := executeSafeOperation(t.Context(), svc, executor, input, claim, time.Now().UTC())
				if !errors.Is(err, test.want) || executor.calls != 0 || outcome.Verified || outcome.Receipt != nil {
					t.Fatalf("unexpected dispatch or result: calls=%d outcome=%+v error=%v", executor.calls, outcome, err)
				}
				after, err := svc.Get(stored.OwnerUserID, stored.WorkspaceID, stored.ID)
				if err != nil || !reflect.DeepEqual(*after, stored) {
					t.Fatalf("stored operation changed: %+v %v", after, err)
				}
				eventsAfter, err := svc.Events(stored.ID)
				if err != nil || !reflect.DeepEqual(eventsAfter, eventsBefore) {
					t.Fatalf("audit changed before refusal: %+v %v", eventsAfter, err)
				}
			})
		}
	}
}

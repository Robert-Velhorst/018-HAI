package executionbroker

import (
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
	"testing"
)

func TestFinalEffectDigestBindsObservationIDAndGeneration(t *testing.T) {
	scope := operations.SafeEffectScope{OperationID: uuid.New(), Version: 3, OwnerUserID: "owner", WorkspaceID: "local",
		ClaimOwner: uuid.New(), ClaimGeneration: 2, SourceIdentityHash: "source", SourceRevisionHash: "revision",
		SourceObservationID: uuid.NewString(), SourceObservationGeneration: 7}
	effect := FinalEffect{ContractVersion: operationAuthorizationContractVersion, RuntimeID: LocalSafeWorkerID,
		Action: LocalSafeWorkerAction, OperationScope: &scope}
	want, err := digestFinalEffect(effect)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*operations.SafeEffectScope){
		func(s *operations.SafeEffectScope) { s.SourceObservationID = uuid.NewString() },
		func(s *operations.SafeEffectScope) { s.SourceObservationGeneration++ },
		func(s *operations.SafeEffectScope) { s.SourceObservationID = ""; s.SourceObservationGeneration = 0 },
	} {
		altered := scope
		change(&altered)
		effect.OperationScope = &altered
		got, err := digestFinalEffect(effect)
		if err != nil || got == want || sameOperationScope(&scope, &altered) {
			t.Fatalf("observation substitution was not bound: %q / %v", got, err)
		}
	}
}

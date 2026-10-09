package executionbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"automation-hub-backend/internal/operations"
)

func managedSafeWorkerInput(input SafeWorkerInput) bool {
	return strings.HasPrefix(input.ArtifactName, "operation-") || strings.HasPrefix(input.Marker, "HAI-OP ")
}

func sameOperationScope(a, b *operations.SafeEffectScope) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func scopedSafeWorkerArtifact(scope operations.SafeEffectScope) (name, marker string) {
	name = "operation-" + scope.OperationID.String() + ".txt"
	marker = fmt.Sprintf("HAI-OP %s rev %s", scope.OperationID, scope.SourceRevisionHash)
	return name, marker
}

// validateScopedSafeWorkerInput prevents a valid operation claim from being
// repurposed to authorize caller-selected workspace content.
func validateScopedSafeWorkerInput(scope operations.SafeEffectScope, input SafeWorkerInput) error {
	name, marker := scopedSafeWorkerArtifact(scope)
	if input.ArtifactName != name || input.Marker != marker {
		return ErrAuthorizationMismatch
	}
	return nil
}

func validateScopedSafeWorkerEffect(effect FinalEffect) error {
	if effect.OperationScope == nil {
		return nil
	}
	name, marker := scopedSafeWorkerArtifact(*effect.OperationScope)
	if effect.ArtifactName != name {
		return ErrAuthorizationMismatch
	}
	payloadHash := sha256.Sum256([]byte(marker))
	if !equalDigest(effect.PayloadDigest, hex.EncodeToString(payloadHash[:])) {
		return ErrAuthorizationMismatch
	}
	return nil
}

func validateActiveOperationScope(ctx context.Context, expected *operations.SafeEffectScope) error {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	scope, active := operations.CurrentSafeEffectScope(ctx)
	if expected == nil {
		if active {
			return ErrAuthorizationMismatch
		}
		return nil
	}
	if !active || *expected != scope {
		return ErrAuthorizationMismatch
	}
	return nil
}

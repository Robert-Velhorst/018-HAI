package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

var ErrInvalidSourceIdentity = errors.New("operations: invalid structured source identity")
var ErrSourceIdentityImmutable = errors.New("operations: source identity and identified revision are immutable")

// SourceIdentityDigest groups revisions of an exact provider/account/record
// tuple. Scope belongs in the repository key, not in an ambiguous display URI.
// Meaningful bytes are preserved: no case folding or implicit trimming.
func SourceIdentityDigest(provider, account, external string) (string, error) {
	if !validSourceIdentityPart(provider, 64) || !validSourceIdentityPart(account, 1024) || !validSourceIdentityPart(external, 4096) {
		return "", ErrInvalidSourceIdentity
	}
	encoded, err := json.Marshal(struct {
		Version    int    `json:"version"`
		Provider   string `json:"provider"`
		Account    string `json:"account"`
		ExternalID string `json:"externalId"`
	}{1, provider, account, external})
	if err != nil {
		return "", ErrInvalidSourceIdentity
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validSourceIdentityPart(value string, maxBytes int) bool {
	if len(value) > maxBytes || strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validateOperationSourceIdentity(op models.Operation) error {
	if (op.SourceObservationID == nil && op.SourceObservationGeneration != 0) ||
		(op.SourceObservationID != nil && (*op.SourceObservationID == uuid.Nil || op.SourceObservationGeneration <= 0 || op.SourceIdentityHash == "" ||
			op.AccountFeedID == nil || *op.AccountFeedID == uuid.Nil)) {
		return ErrInvalidSourceObservation
	}
	if op.SourceProvider == "" && op.SourceAccount == "" && op.SourceExternalID == "" && op.SourceIdentityHash == "" {
		// Historical/manual records remain identifiable as lacking this contract.
		// Display URIs are never parsed to manufacture provenance.
		return nil
	}
	digest, err := SourceIdentityDigest(op.SourceProvider, op.SourceAccount, op.SourceExternalID)
	if err != nil || digest != op.SourceIdentityHash || !validSourceIdentityPart(op.SourceRevisionHash, 4096) {
		return ErrInvalidSourceIdentity
	}
	return nil
}

func sameSourceIdentity(a, b models.Operation) bool {
	return a.SourceProvider == b.SourceProvider && a.SourceAccount == b.SourceAccount &&
		a.SourceExternalID == b.SourceExternalID && a.SourceIdentityHash == b.SourceIdentityHash
}

func validateSourceIdentityMutation(current, next models.Operation) error {
	if !sameSourceIdentity(current, next) || !sameSourceObservation(current, next) ||
		(current.SourceObservationID != nil && (current.AccountFeedID == nil || next.AccountFeedID == nil || *current.AccountFeedID != *next.AccountFeedID)) ||
		(current.SourceIdentityHash != "" && current.SourceRevisionHash != next.SourceRevisionHash) {
		return ErrSourceIdentityImmutable
	}
	return validateOperationSourceIdentity(next)
}

func validateIntakeSourceMatch(existing, expected models.Operation) error {
	if expected.SourceIdentityHash != "" && existing.SourceIdentityHash == "" &&
		existing.SourceProvider == "" && existing.SourceAccount == "" && existing.SourceExternalID == "" {
		return ErrSourceIdentityMigrationRequired
	}
	if err := validateOperationSourceIdentity(existing); err != nil {
		return ErrInvalidRepositoryResult
	}
	if !sameSourceIdentity(existing, expected) ||
		(expected.SourceIdentityHash != "" && existing.SourceRevisionHash != expected.SourceRevisionHash) {
		return ErrInvalidRepositoryResult
	}
	return nil
}

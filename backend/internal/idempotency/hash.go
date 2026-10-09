package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// sha256Hex returns the hex-encoded SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// join preserves historical hash encodings. Untrusted pipe characters can make
// field boundaries ambiguous; changing persisted identities requires migration.
func join(parts ...string) string {
	return strings.Join(parts, "|")
}

// SourceRevisionHash identifies a specific revision of a source item's content
// + metadata. Approval validation must bind to this hash; creating a new
// revision does not itself revoke an older operation's approval.
func SourceRevisionHash(content, metadata string) string {
	return sha256Hex(join("srev", content, metadata))
}

// FeedItemDedupeKey deduplicates the same feed item across repeated syncs.
func FeedItemDedupeKey(provider, accountLabel, externalID, sourceRevisionHash string) string {
	return sha256Hex(join("feed", provider, accountLabel, externalID, sourceRevisionHash))
}

// StructuredFeedItemDedupeKey keeps untrusted field separators unambiguous.
// Historical v1 rows require explicit reconciliation before a v2 rollout.
func StructuredFeedItemDedupeKey(provider, accountLabel, externalID, sourceRevisionHash string) string {
	encoded, _ := json.Marshal([]string{"feed.v2", provider, accountLabel, externalID, sourceRevisionHash})
	return sha256Hex(string(encoded))
}

// OperationDedupeKey deduplicates Operations so repeated feed sync does not
// create duplicate Operations for the same underlying work.
func OperationDedupeKey(workspaceID, operationType, sourceType, sourceID, sourceRevisionHash string) string {
	return sha256Hex(join("op", workspaceID, operationType, sourceType, sourceID, sourceRevisionHash))
}

// ActionPayloadHash binds an approval to an exact action (§16):
//
//	sha256(canonicalJSON(exact_payload) + source_revision_hash + target_system + action_type)
//
// A stale payload therefore fails approval validation. Returns an error if the
// payload cannot be canonicalized.
func ActionPayloadHash(exactPayload any, sourceRevisionHash, targetSystem, actionType string) (string, error) {
	canonical, err := CanonicalJSONString(exactPayload)
	if err != nil {
		return "", err
	}
	return sha256Hex(join("action", canonical, sourceRevisionHash, targetSystem, actionType)), nil
}

// RuntimeAttemptIdempotencyKey prevents duplicate execution of the same
// approved action by the same runtime.
func RuntimeAttemptIdempotencyKey(operationID, actionPlanID, runtimeID, payloadHash string) string {
	return sha256Hex(join("attempt", operationID, actionPlanID, runtimeID, payloadHash))
}

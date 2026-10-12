package repositories

import "testing"

func TestResetTokenDigestIsStableAndNeverReturnsPlaintext(t *testing.T) {
	const token = "one-time-reset-code"
	digest := resetTokenDigest(token)

	if digest == token {
		t.Fatal("reset token digest must not retain the plaintext token")
	}
	if len(digest) != 64 {
		t.Fatalf("reset token digest length = %d, want 64", len(digest))
	}
	if digest != resetTokenDigest(token) {
		t.Fatal("reset token digest must be stable")
	}
}

func TestNormalizeResetTokenForPersistenceDoesNotDoubleHash(t *testing.T) {
	const token = "one-time-reset-code"
	digest := resetTokenDigest(token)

	if got := normalizeResetTokenForPersistence(token); got != digest {
		t.Fatalf("normalized plaintext token = %q, want digest %q", got, digest)
	}
	if got := normalizeResetTokenForPersistence(digest); got != digest {
		t.Fatalf("normalized digest = %q, want unchanged digest %q", got, digest)
	}
	if got := normalizeResetTokenForPersistence("   "); got != "" {
		t.Fatalf("normalized empty token = %q, want empty", got)
	}
}

func TestResetTokenLookupCandidatesPreferDigestAndRetainLegacyFallback(t *testing.T) {
	const token = "one-time-reset-code"
	candidates := resetTokenLookupCandidates(token)
	if len(candidates) != 2 {
		t.Fatalf("lookup candidates = %#v, want digest and legacy plaintext", candidates)
	}
	if candidates[0] != resetTokenDigest(token) || candidates[1] != token {
		t.Fatalf("lookup candidates = %#v, want digest followed by legacy plaintext", candidates)
	}
}

package identity

import (
	"strings"
	"testing"
	"time"
)

func TestVerifyRejectsUnicodeFoldedDuplicateClaimKeys(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	payload := `{"sub":"user-1","role":"operator","iss":"hai-idp","aud":"hai","token_type":"access","iat":1767225600,"exp":1767229200}`
	for _, test := range []struct{ name, payload string }{
		{"subject", strings.Replace(payload, `"sub":"user-1"`, `"sub":"user-1","\u017fub":"other-user"`, 1)},
		{"issuer", strings.Replace(payload, `"iss":"hai-idp"`, `"i\u017f\u017f":"other-issuer","iss":"hai-idp"`, 1)},
		{"nested extension", strings.TrimSuffix(payload, "}") + `,"extension":{"state":1,"\u017ftate":2}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := signRawToken(`{"alg":"HS256"}`, test.payload, secret)
			if _, err := Verify(token, secret, now); err != ErrMalformed {
				t.Fatalf("ambiguous keys accepted: err=%v", err)
			}
		})
	}
}

func TestJSONKeyFoldingMatchesStandardSimpleFold(t *testing.T) {
	keys := []string{"sub", "SUB", "\u017fub", "key", "\u212aey", "\u03a3", "\u03c3", "\u03c2", "other", ""}
	for _, a := range keys {
		for _, b := range keys {
			if got, want := foldJSONKey(a) == foldJSONKey(b), strings.EqualFold(a, b); got != want {
				t.Fatalf("key equivalence differs for %q and %q: got=%v want=%v", a, b, got, want)
			}
		}
	}
}

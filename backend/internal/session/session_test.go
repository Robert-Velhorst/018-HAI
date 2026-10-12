package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidWithinTTL(t *testing.T) {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := New("tok", issued, time.Hour)
	if !s.Valid(issued.Add(30 * time.Minute)) {
		t.Fatalf("session should be valid within ttl")
	}
	if s.Valid(issued.Add(2 * time.Hour)) {
		t.Fatalf("session should be expired past ttl")
	}
}

func TestEmptyTokenNeverValid(t *testing.T) {
	s := New("", time.Now(), time.Hour)
	if s.Valid(time.Now()) {
		t.Fatalf("empty token must never be valid")
	}
}

func TestSessionJSONDoesNotExposeBearerToken(t *testing.T) {
	const bearer = "secret-session-bearer"
	s := New(bearer, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Hour)

	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal(session) error = %v", err)
	}
	if strings.Contains(string(encoded), bearer) {
		t.Fatalf("session JSON exposed the bearer token: %s", encoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("json.Unmarshal(session JSON) error = %v", err)
	}
	if _, ok := fields["token"]; ok {
		t.Fatalf("session JSON must omit the token field, got %s", encoded)
	}
	for _, field := range []string{"issuedAt", "expiresAt"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("session JSON omitted non-secret field %q: %s", field, encoded)
		}
	}
}

func TestSessionIsNotValidBeforeIssueTime(t *testing.T) {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := New("tok", issued, time.Hour)
	if s.Valid(issued.Add(-time.Nanosecond)) {
		t.Fatal("session must not be valid before its issue time")
	}
	if !s.Valid(issued) {
		t.Fatal("session should be valid at its issue time")
	}
}

func TestMalformedSessionWindowIsNeverValid(t *testing.T) {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		session Session
	}{
		{name: "missing issue time", session: Session{Token: "tok", ExpiresAt: issued.Add(time.Hour)}},
		{name: "expiry equals issue", session: Session{Token: "tok", IssuedAt: issued, ExpiresAt: issued}},
		{name: "expiry before issue", session: Session{Token: "tok", IssuedAt: issued, ExpiresAt: issued.Add(-time.Second)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.session.Valid(issued) {
				t.Fatal("malformed session window must not be valid")
			}
			if got := test.session.Remaining(issued); got != 0 {
				t.Fatalf("Remaining() = %v, want 0 for malformed session", got)
			}
		})
	}
}

func TestRemainingClampsAtZero(t *testing.T) {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := New("tok", issued, time.Hour)
	if s.Remaining(issued.Add(30*time.Minute)) != 30*time.Minute {
		t.Fatalf("remaining wrong")
	}
	if s.Remaining(issued.Add(2*time.Hour)) != 0 {
		t.Fatalf("expired remaining should clamp to 0")
	}
}

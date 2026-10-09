package identity

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const secret = "test-secret"

func TestVerifyValidToken(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok := SignToken(Claims{Subject: "user-1", Role: "operator", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix()}, secret)
	claims, err := Verify(tok, secret, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Role != "operator" || claims.Issuer != tokenIssuer || claims.Audience != tokenAudience || claims.TokenType != "access" {
		t.Fatalf("claims wrong: %+v", claims)
	}
}

func TestVerifyRejectsMissingSigningSecret(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	claims := Claims{Subject: "user-1", Role: "operator", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix()}
	for _, test := range []struct {
		name   string
		secret string
	}{{name: "empty", secret: ""}, {name: "whitespace", secret: " \t\n"}} {
		t.Run(test.name, func(t *testing.T) {
			token := SignToken(claims, test.secret)
			if _, err := Verify(token, test.secret, now); err != ErrKey {
				t.Fatalf("Verify() error = %v, want ErrKey", err)
			}
		})
	}
	token := SignToken(claims, secret)
	if _, err := Verify(token, "", now); err != ErrKey {
		t.Fatalf("Verify() with an unconfigured key error = %v, want ErrKey", err)
	}
}

func TestVerifyRejectsOversizedTokenBeforeDecoding(t *testing.T) {
	if _, err := Verify(strings.Repeat("a", maxTokenBytes+1), secret, time.Now()); err != ErrTokenSize {
		t.Fatalf("Verify() error = %v, want ErrTokenSize", err)
	}
}

func TestVerifyWithSecretsSupportsRotationWithoutAcceptingEmptyKeys(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	claims := validClaims(now)
	oldKeyToken := SignToken(claims, "old-key")
	if _, err := VerifyWithSecrets(oldKeyToken, []string{"new-key", "old-key"}, now); err != nil {
		t.Fatalf("rotation key ring should accept the previous key: %v", err)
	}
	if _, err := VerifyWithSecrets(oldKeyToken, []string{"new-key", " \t", ""}, now); err != ErrSignature {
		t.Fatalf("key ring without matching key error = %v, want ErrSignature", err)
	}
	if _, err := VerifyWithSecrets(oldKeyToken, []string{"", " \t"}, now); err != ErrKey {
		t.Fatalf("empty key ring error = %v, want ErrKey", err)
	}
	if _, err := VerifyWithSecrets(oldKeyToken, []string{"1", "2", "3", "4", "5"}, now); err != ErrKeyRing {
		t.Fatalf("oversized key ring error = %v, want ErrKeyRing", err)
	}
}

func TestVerifyRejectsInconsistentTimeClaims(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		edit  func(*Claims)
	}{
		{name: "expiry before issued-at", edit: func(c *Claims) { c.Expiry = c.IssuedAt + 1; c.IssuedAt += 2 }},
		{name: "not-before after expiry", edit: func(c *Claims) { c.NotBefore = c.Expiry + 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := validClaims(now)
			test.edit(&claims)
			if _, err := Verify(SignToken(claims, secret), secret, now); err != ErrClaimOrder {
				t.Fatalf("Verify() error = %v, want ErrClaimOrder", err)
			}
		})
	}
}

func TestVerifyRejectsDuplicateJSONKeys(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	goodPayload := `{"sub":"user-1","role":"operator","iss":"hai-idp","aud":"hai","token_type":"access","iat":1767225600,"exp":1767229200}`
	for _, test := range []struct {
		name, header, payload string
	}{
		{name: "duplicate algorithm", header: `{"alg":"HS256","alg":"none"}`, payload: goodPayload},
		{name: "duplicate role claim", header: `{"alg":"HS256","typ":"JWT"}`, payload: strings.Replace(goodPayload, `"role":"operator"`, `"role":"user","role":"owner"`, 1)},
		{name: "case-variant role claim", header: `{"alg":"HS256"}`, payload: strings.Replace(goodPayload, `"role":"operator"`, `"ROLE":"user","role":"owner"`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := signRawToken(test.header, test.payload, secret)
			if _, err := Verify(token, secret, now); err != ErrMalformed {
				t.Fatalf("Verify() error = %v, want ErrMalformed", err)
			}
		})
	}
}

func TestVerifyRejectsUnsupportedCriticalHeaderExtensions(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, headerJSON := range []string{
		`{"alg":"HS256","crit":["exp"]}`,
		`{"alg":"HS256","b64":false}`,
		`{"alg":"HS256","b64":null}`,
	} {
		token := signRawToken(headerJSON, `{"sub":"user-1","role":"operator","iss":"hai-idp","aud":"hai","token_type":"access","iat":1767225600,"exp":1767229200}`, secret)
		if _, err := Verify(token, secret, now); err != ErrHeader {
			t.Fatalf("header %s error = %v, want ErrHeader", headerJSON, err)
		}
	}
}

func TestVerifyRejectsInvalidUTF8AndNonCanonicalEncoding(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	invalidUTF8Payload := `{"sub":"user-` + string([]byte{0xff}) + `","role":"operator","iss":"hai-idp","aud":"hai","token_type":"access","iat":1767225600,"exp":1767229200}`
	if _, err := Verify(signRawToken(`{"alg":"HS256"}`, invalidUTF8Payload, secret), secret, now); err != ErrMalformed {
		t.Fatalf("invalid UTF-8 error = %v, want ErrMalformed", err)
	}
	canonical := SignToken(validClaims(now), secret)
	if _, err := Verify(canonical+"=", secret, now); err != ErrMalformed {
		t.Fatalf("padded token error = %v, want ErrMalformed", err)
	}
}

func TestVerifyRejectsExcessiveJSONNesting(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nested := strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1)
	headerJSON := `{"alg":"HS256","extension":` + nested + `}`
	if _, err := Verify(signRawToken(headerJSON, `{"sub":"user-1"}`, secret), secret, now); err != ErrMalformed {
		t.Fatalf("deeply nested header error = %v, want ErrMalformed", err)
	}
}

func TestVerifyRejectsRefreshAndUntypedTokens(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tokenType := range []string{"refresh", ""} {
		t.Run("type="+tokenType, func(t *testing.T) {
			token := SignToken(Claims{Subject: "user-1", Role: "owner", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: tokenType, IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix()}, secret)
			if _, err := Verify(token, secret, now); err != ErrTokenType {
				t.Fatalf("token type %q should fail with ErrTokenType, got %v", tokenType, err)
			}
		})
	}
}

func TestVerifyRejectsMissingExpiryIssuerAndAudience(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		edit func(*Claims)
		want error
	}{
		{name: "missing expiry", edit: func(c *Claims) { c.Expiry = 0 }, want: ErrExpired},
		{name: "missing issued at", edit: func(c *Claims) { c.IssuedAt = 0 }, want: ErrIssuedAt},
		{name: "missing issuer", edit: func(c *Claims) { c.Issuer = "" }, want: ErrIssuer},
		{name: "wrong issuer", edit: func(c *Claims) { c.Issuer = "another-idp" }, want: ErrIssuer},
		{name: "missing audience", edit: func(c *Claims) { c.Audience = "" }, want: ErrAudience},
		{name: "wrong audience", edit: func(c *Claims) { c.Audience = "another-service" }, want: ErrAudience},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := Claims{Subject: "user-1", Role: "owner", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix()}
			test.edit(&claims)
			token := SignToken(claims, secret)
			if _, err := Verify(token, secret, now); err != test.want {
				t.Fatalf("Verify() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestClaimsPrincipalUsesBundledIDPUserID(t *testing.T) {
	claims := Claims{UserID: "idp-user"}
	if got := claims.Principal(); got != "idp-user" {
		t.Fatalf("Principal() = %q, want bundled IDP user_id", got)
	}

	claims.Subject = "openid-subject"
	if got := claims.Principal(); got != "openid-subject" {
		t.Fatalf("Principal() = %q, want OpenID sub to take precedence", got)
	}
}

func TestClaimsPrincipalRejectsWhitespaceAliases(t *testing.T) {
	for _, test := range []struct {
		name   string
		claims Claims
	}{
		{name: "padded subject cannot alias trimmed subject or fall back", claims: Claims{Subject: " alice ", UserID: "alice"}},
		{name: "whitespace subject cannot fall back", claims: Claims{Subject: " \t", UserID: "alice"}},
		{name: "padded bundled user id is rejected", claims: Claims{UserID: " alice "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.claims.Principal(); got != "" {
				t.Fatalf("Principal() = %q, want invalid identity rejected without normalization", got)
			}
		})
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	now := time.Now()
	tok := SignToken(Claims{Subject: "u", Role: "owner"}, secret)
	if _, err := Verify(tok, "other-secret", now); err != ErrSignature {
		t.Fatalf("wrong secret should fail with ErrSignature, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok := SignToken(Claims{Subject: "u", Role: "owner", Expiry: now.Add(-time.Minute).Unix()}, secret)
	if _, err := Verify(tok, secret, now); err != ErrExpired {
		t.Fatalf("expired token should fail with ErrExpired, got %v", err)
	}
}

func TestVerifyUsesIDPExpiryLeeway(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		expiry  time.Time
		wantErr error
	}{
		{name: "within IDP leeway", expiry: now.Add(-29 * time.Second)},
		{name: "at IDP leeway boundary", expiry: now.Add(-30 * time.Second)},
		{name: "outside IDP leeway", expiry: now.Add(-31 * time.Second), wantErr: ErrExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := Claims{Subject: "user-1", Role: "owner", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: "access", IssuedAt: test.expiry.Add(-time.Hour).Unix(), Expiry: test.expiry.Unix()}
			if _, err := Verify(SignToken(claims, secret), secret, now); err != test.wantErr {
				t.Fatalf("Verify() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestVerifyUsesIDPIssuedAtLeeway(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		issuedAt time.Time
		wantErr  error
	}{
		{name: "within IDP leeway", issuedAt: now.Add(29 * time.Second)},
		{name: "at IDP leeway boundary", issuedAt: now.Add(30 * time.Second)},
		{name: "outside IDP leeway", issuedAt: now.Add(31 * time.Second), wantErr: ErrIssuedAt},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := Claims{Subject: "user-1", Role: "owner", Issuer: tokenIssuer, Audience: tokenAudience, TokenType: "access", IssuedAt: test.issuedAt.Unix(), Expiry: now.Add(time.Hour).Unix()}
			if _, err := Verify(SignToken(claims, secret), secret, now); err != test.wantErr {
				t.Fatalf("Verify() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestVerifyHonorsNotBeforeClaim(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		notBefore time.Time
		wantErr   error
	}{
		{name: "already valid", notBefore: now.Add(-time.Minute)},
		{name: "within IDP leeway", notBefore: now.Add(29 * time.Second)},
		{name: "at IDP leeway boundary", notBefore: now.Add(30 * time.Second)},
		{name: "outside IDP leeway", notBefore: now.Add(31 * time.Second), wantErr: ErrNotBefore},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := Claims{
				Subject: "user-1", Role: "owner", Issuer: tokenIssuer, Audience: tokenAudience,
				TokenType: "access", IssuedAt: now.Unix(), NotBefore: test.notBefore.Unix(), Expiry: now.Add(time.Hour).Unix(),
			}
			if _, err := Verify(SignToken(claims, secret), secret, now); err != test.wantErr {
				t.Fatalf("Verify() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestVerifyRejectsNonHS256(t *testing.T) {
	// A token with alg:none must be rejected even if the rest is well-formed.
	// header {"alg":"none","typ":"JWT"} base64url:
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJ1Iiwicm9sZSI6Im93bmVyIn0."
	if _, err := Verify(noneToken, secret, time.Now()); err != ErrAlgorithm {
		t.Fatalf("alg:none must be rejected with ErrAlgorithm, got %v", err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "a.b", "a.b.c.d", "not-a-token"} {
		if _, err := Verify(bad, secret, time.Now()); err == nil {
			t.Fatalf("malformed token %q should fail", bad)
		}
	}
}

func validClaims(now time.Time) Claims {
	return Claims{
		Subject: "user-1", Role: "operator", Issuer: tokenIssuer, Audience: tokenAudience,
		TokenType: "access", IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix(),
	}
}

func signRawToken(headerJSON, payloadJSON, key string) string {
	headerPart := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	payloadPart := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	input := headerPart + "." + payloadPart
	return input + "." + base64.RawURLEncoding.EncodeToString(sign(input, key))
}

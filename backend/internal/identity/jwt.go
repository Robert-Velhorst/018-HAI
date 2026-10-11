// Package identity verifies IDP-issued HS256 JSON Web Tokens using only the
// standard library, and exposes the caller's identity + role. This is how a
// per-user identity (from the IDP, signed with the shared JWT secret) is mapped
// to an RBAC role in the backend — no third-party JWT dependency required.
package identity

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrMalformed = errors.New("identity: malformed token")
	ErrKey       = errors.New("identity: signing secret is not configured")
	ErrTokenSize = errors.New("identity: token exceeds maximum size")
	ErrAlgorithm = errors.New("identity: unexpected signing algorithm")
	ErrSignature = errors.New("identity: invalid signature")
	ErrExpired   = errors.New("identity: token expired")
	ErrIssuedAt  = errors.New("identity: invalid token issued-at time")
	ErrNotBefore = errors.New("identity: token is not valid yet")
	ErrTokenType = errors.New("identity: token is not an access token")
	ErrIssuer    = errors.New("identity: unexpected token issuer")
	ErrAudience  = errors.New("identity: unexpected token audience")
	ErrPrincipal  = errors.New("identity: stable principal is missing")
	ErrKeyRing    = errors.New("identity: too many verification secrets")
	ErrClaimOrder = errors.New("identity: inconsistent token time claims")
	ErrHeader     = errors.New("identity: unsupported critical JWT header")
)

const (
	tokenIssuer         = "hai-idp"
	tokenAudience       = "hai"
	tokenLeeway         = 30 * time.Second
	maxTokenBytes       = 16 << 10
	maxJSONDepth        = 64
	maxVerificationKeys = 4

	// ContextRoleKey and ContextSubjectKey identify verified JWT claims stored
	// on a Gin request. Route handlers use these values for audit provenance.
	ContextRoleKey    = "role"
	ContextSubjectKey = "subject"
)

// Claims are the subset of JWT claims the backend cares about.
type Claims struct {
	Subject   string `json:"sub"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	TokenType string `json:"token_type"`
	IssuedAt  int64  `json:"iat,omitempty"`
	NotBefore int64  `json:"nbf,omitempty"`
	Expiry    int64  `json:"exp,omitempty"`
}

// Principal returns the stable identity claim used by the issuing IDP. HAI's
// bundled IDP emits user_id, while external OpenID-compatible issuers normally
// emit sub. Supporting both keeps audit attribution tied to a signed claim.
func (c Claims) Principal() string {
	if c.Subject != "" {
		if c.Subject != strings.TrimSpace(c.Subject) {
			return ""
		}
		return c.Subject
	}
	if c.UserID != strings.TrimSpace(c.UserID) {
		return ""
	}
	return c.UserID
}

type header struct {
	Alg              string          `json:"alg"`
	Typ              string          `json:"typ"`
	Critical         json.RawMessage `json:"crit"`
	UnencodedPayload json.RawMessage `json:"b64"`
}

// Verify validates an HS256 JWT against secret at time now and returns its
// claims. It rejects any algorithm other than HS256 (blocking "alg: none" and
// algorithm-confusion attacks), a bad signature, missing or expired expiry,
// unexpected issuer or audience, and IDP refresh or untyped tokens presented
// as API identities.
func Verify(token, secret string, now time.Time) (Claims, error) {
	return VerifyWithSecrets(token, []string{secret}, now)
}

// VerifyWithSecrets verifies a JWT against a bounded key ring. The first key
// should be the current signing key; later keys may be retained temporarily
// to validate tokens issued before a rotation. Issuers should sign only with
// the current key. Empty entries are ignored, but an empty key ring fails
// closed.
func VerifyWithSecrets(token string, secrets []string, now time.Time) (Claims, error) {
	if len(secrets) > maxVerificationKeys {
		return Claims{}, ErrKeyRing
	}
	if !hasSigningKey(secrets) {
		return Claims{}, ErrKey
	}
	if len(token) > maxTokenBytes {
		return Claims{}, ErrTokenSize
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrMalformed
	}

	headerBytes, err := decodeJWTPart(parts[0])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	if !utf8.Valid(headerBytes) || validateUniqueJSONKeys(headerBytes) != nil {
		return Claims{}, ErrMalformed
	}
	var h header
	if err := json.Unmarshal(headerBytes, &h); err != nil {
		return Claims{}, ErrMalformed
	}
	if h.Alg != "HS256" {
		return Claims{}, ErrAlgorithm
	}
	if len(h.Critical) != 0 || (len(h.UnencodedPayload) != 0 && string(h.UnencodedPayload) != "true") {
		return Claims{}, ErrHeader
	}

	sig, err := decodeJWTPart(parts[2])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	validSignature := false
	for _, secret := range secrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		if hmac.Equal(sig, sign(parts[0]+"."+parts[1], secret)) {
			validSignature = true
		}
	}
	if !validSignature {
		return Claims{}, ErrSignature
	}

	payloadBytes, err := decodeJWTPart(parts[1])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	if !utf8.Valid(payloadBytes) || validateUniqueJSONKeys(payloadBytes) != nil {
		return Claims{}, ErrMalformed
	}
	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return Claims{}, ErrMalformed
	}
	if claims.Principal() == "" {
		return Claims{}, ErrPrincipal
	}
	if claims.Expiry == 0 || now.After(time.Unix(claims.Expiry, 0).Add(tokenLeeway)) {
		return Claims{}, ErrExpired
	}
	if claims.IssuedAt == 0 || now.Add(tokenLeeway).Before(time.Unix(claims.IssuedAt, 0)) {
		return Claims{}, ErrIssuedAt
	}
	if claims.Expiry <= claims.IssuedAt || (claims.NotBefore != 0 && claims.NotBefore > claims.Expiry) {
		return Claims{}, ErrClaimOrder
	}
	if claims.NotBefore != 0 && now.Add(tokenLeeway).Before(time.Unix(claims.NotBefore, 0)) {
		return Claims{}, ErrNotBefore
	}
	if claims.Issuer != tokenIssuer {
		return Claims{}, ErrIssuer
	}
	if claims.Audience != tokenAudience {
		return Claims{}, ErrAudience
	}
	if claims.TokenType != "access" {
		return Claims{}, ErrTokenType
	}
	return claims, nil
}

func hasSigningKey(secrets []string) bool {
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			return true
		}
	}
	return false
}

func decodeJWTPart(part string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != part {
		return nil, ErrMalformed
	}
	return decoded, nil
}

func validateUniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	root, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := walkJSONValue(decoder, root, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("identity: trailing JSON value")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder, token json.Token, depth int) error {
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxJSONDepth {
		return errors.New("identity: JSON nesting exceeds maximum depth")
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("identity: invalid JSON object key")
			}
			foldedKey := strings.ToLower(key)
			if _, exists := seen[foldedKey]; exists {
				return errors.New("identity: duplicate JSON object key")
			}
			seen[foldedKey] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walkJSONValue(decoder, value, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("identity: malformed JSON object")
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walkJSONValue(decoder, value, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("identity: malformed JSON array")
		}
	default:
		return errors.New("identity: unexpected JSON delimiter")
	}
	return nil
}

// SignToken builds an HS256 JWT for the given claims. Production tokens are
// issued by the IDP; this is used by tests and any trusted local issuer.
func SignToken(claims Claims, secret string) string {
	headerPart := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, _ := json.Marshal(claims)
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := headerPart + "." + payloadPart
	sigPart := base64.RawURLEncoding.EncodeToString(sign(signingInput, secret))
	return signingInput + "." + sigPart
}

func sign(input, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

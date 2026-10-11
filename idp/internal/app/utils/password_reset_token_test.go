package utils

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewPasswordResetTokenReturnsBearerAndDigest(t *testing.T) {
	token, digest, err := NewPasswordResetToken()
	if err != nil {
		t.Fatal("failed to generate reset token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != passwordResetTokenSize {
		t.Fatal("generated reset token has an invalid encoding")
	}
	if len(digest) != 64 || !IsPasswordResetTokenDigest(digest) || digest != PasswordResetTokenDigest(token) || digest == token {
		t.Fatal("generated reset-token digest is invalid")
	}
}

func TestIsPasswordResetTokenDigestRejectsNonDigestValues(t *testing.T) {
	for _, value := range []string{"", "short", strings.Repeat("g", 64), "0123456789abcdef"} {
		if IsPasswordResetTokenDigest(value) {
			t.Fatal("non-digest value was accepted")
		}
	}
}

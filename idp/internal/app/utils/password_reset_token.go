package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const passwordResetTokenSize = 32

// NewPasswordResetToken returns the bearer value for delivery and its
// lowercase SHA-256 digest for persistence.
func NewPasswordResetToken() (token, digest string, err error) {
	value := make([]byte, passwordResetTokenSize)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(value)
	return token, PasswordResetTokenDigest(token), nil
}

func PasswordResetTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func IsPasswordResetTokenDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

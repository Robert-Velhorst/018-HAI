package repositories

import (
	"strings"
	"testing"
	"time"

	"automation-hub-idp/internal/app/utils"
	"github.com/google/uuid"
)

func TestResetTokenDigestIsStableAndNeverReturnsPlaintext(t *testing.T) {
	const token = "one-time-reset-code"
	digest := utils.PasswordResetTokenDigest(token)

	if digest == token {
		t.Fatal("reset token digest must not retain the plaintext token")
	}
	if len(digest) != 64 {
		t.Fatalf("reset token digest length = %d, want 64", len(digest))
	}
	if digest != utils.PasswordResetTokenDigest(token) {
		t.Fatal("reset token digest must be stable")
	}
}

func TestResetTokenPersistenceRejectsNonDigestValues(t *testing.T) {
	for _, token := range []string{"plaintext", strings.Repeat("g", 64), strings.Repeat("A", 64)} {
		err := (&GormUserRepository{}).SetPasswordResetToken(uuid.New(), 1, token, time.Now().Add(time.Minute), time.Now())
		if err == nil {
			t.Fatalf("SetPasswordResetToken accepted a non-digest token %q", token)
		}
	}
}

package infra

import (
	"strings"
	"testing"
)

func TestConfiguredFirstRunAdminPasswordRejectsMissingAndPlaceholderValues(t *testing.T) {
	for _, password := range []string{
		"", "ChangeMe123!", "change-this-local-admin-password", "short",
		strings.Repeat("é", 6), strings.Repeat("a", 73),
	} {
		t.Run(password, func(t *testing.T) {
			t.Setenv("FIRST_RUN_ADMIN_PASSWORD", password)
			_, err := configuredFirstRunAdminPassword()
			if err == nil {
				t.Fatalf("password %q unexpectedly accepted", password)
			}
		})
	}
}

func TestConfiguredFirstRunAdminPasswordUsesCharacterAndBcryptByteBounds(t *testing.T) {
	for _, password := range []string{
		"Abcdef12!xyz", // 12 ASCII characters
		strings.Repeat("é", 12),
		strings.Repeat("é", 36), // 72 bytes at the bcrypt boundary
	} {
		t.Run(password, func(t *testing.T) {
			t.Setenv("FIRST_RUN_ADMIN_PASSWORD", password)
			got, err := configuredFirstRunAdminPassword()
			if err != nil {
				t.Fatalf("valid first-run password rejected: %v", err)
			}
			if got != password {
				t.Fatalf("configured password changed during validation")
			}
		})
	}
}

func TestConfiguredFirstRunAdminPasswordAcceptsStrongOperatorValue(t *testing.T) {
	operatorPassword := strings.Join([]string{"valid", "-operator", "-passphrase", "-2026"}, "")
	t.Setenv("FIRST_RUN_ADMIN_PASSWORD", operatorPassword)
	password, err := configuredFirstRunAdminPassword()
	if err != nil {
		t.Fatal(err)
	}
	if password != operatorPassword {
		t.Fatalf("password = %q", password)
	}
}

func TestConfiguredFirstRunAdminPasswordDoesNotExposeConfiguredValueInError(t *testing.T) {
	const secret = "ChangeMe123!"
	t.Setenv("FIRST_RUN_ADMIN_PASSWORD", secret)
	_, err := configuredFirstRunAdminPassword()
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v, must not expose configured password", err)
	}
}

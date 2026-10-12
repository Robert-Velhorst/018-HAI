package config

import (
	"strings"
	"testing"
	"time"
)

var testJWTSecret = strings.Repeat("s", 32)

func TestAuthenticationConfigNormalizesAndValidatesSettings(t *testing.T) {
	setValidAuthenticationEnv(t)
	t.Setenv(passwordResetTopic, " password-reset ")
	t.Setenv(accountBlockedTopic, " account-blocked ")
	t.Setenv(accountCreatedTopic, " account-created ")
	t.Setenv(minTimeBetweenAttemptsInSeconds, "2")
	t.Setenv(expirationTimeResetTokenInHours, "12")
	t.Setenv(accessTokenDurationMinutes, "10")
	t.Setenv(refreshTokenDurationDays, "3")

	cfg, err := newAuthenticationConfig()
	if err != nil {
		t.Fatalf("newAuthenticationConfig() error = %v", err)
	}
	if cfg.PasswordResetTopic != "password-reset" ||
		cfg.AccountBlockedTopic != "account-blocked" ||
		cfg.AccountCreatedTopic != "account-created" {
		t.Fatalf("topics were not normalized: %#v", cfg)
	}
	if cfg.MinTimeBetweenAttemptsSeconds != 2 ||
		cfg.ExpirationTimeResetTokenHours != 12 ||
		cfg.AccessTokenDurationMinutes != 10 ||
		cfg.RefreshTokenDurationDays != 72*time.Hour {
		t.Fatalf("unexpected durations: %#v", cfg)
	}
}

func TestAuthenticationConfigRejectsMissingTopics(t *testing.T) {
	for _, name := range []string{passwordResetTopic, accountBlockedTopic, accountCreatedTopic} {
		t.Run(name, func(t *testing.T) {
			setValidAuthenticationEnv(t)
			t.Setenv(name, " ")

			_, err := newAuthenticationConfig()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("newAuthenticationConfig() error = %v, want context for %s", err, name)
			}
		})
	}
}

func TestAuthenticationConfigRejectsWeakJWTSecret(t *testing.T) {
	setValidAuthenticationEnv(t)
	t.Setenv(jwtSecret, "too-short")

	_, err := newAuthenticationConfig()
	if err == nil || !strings.Contains(err.Error(), jwtSecret) {
		t.Fatalf("newAuthenticationConfig() error = %v, want context for %s", err, jwtSecret)
	}
}

func TestAuthenticationConfigRejectsInvalidNumericSettings(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: baseBlockDurationMinutes, value: "0"},
		{name: maxLoginAttemptsBeforeBlock, value: "-1"},
		{name: minTimeBetweenAttemptsInSeconds, value: "-1"},
		{name: expirationTimeResetTokenInHours, value: "not-a-number"},
		{name: accessTokenDurationMinutes, value: "0"},
		{name: refreshTokenDurationDays, value: "-2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setValidAuthenticationEnv(t)
			t.Setenv(test.name, test.value)

			_, err := newAuthenticationConfig()
			if err == nil || !strings.Contains(err.Error(), test.name) {
				t.Fatalf("newAuthenticationConfig() error = %v, want context for %s", err, test.name)
			}
		})
	}
}

func setValidAuthenticationEnv(t *testing.T) {
	t.Helper()
	t.Setenv(passwordResetTopic, "password-reset")
	t.Setenv(accountBlockedTopic, "account-blocked")
	t.Setenv(accountCreatedTopic, "account-created")
	t.Setenv(baseBlockDurationMinutes, "1")
	t.Setenv(maxLoginAttemptsBeforeBlock, "3")
	t.Setenv(minTimeBetweenAttemptsInSeconds, "0")
	t.Setenv(expirationTimeResetTokenInHours, "24")
	t.Setenv(accessTokenDurationMinutes, "15")
	t.Setenv(refreshTokenDurationDays, "4")
	t.Setenv(jwtSecret, testJWTSecret)
}

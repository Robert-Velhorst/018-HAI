package services

import (
	"automation-hub-idp/internal/app/config"
	"testing"
)

func TestDefaultsUseLocalEventFallbackWhenBusIsDisabled(t *testing.T) {
	for name, value := range map[string]string{
		"HAI_EVENT_BUS_ENABLED":                "false",
		"DB_PORT":                              "5432",
		"DB_HOST":                              "postgres",
		"DB_NAME":                              "idp",
		"BLOCKING_TIME_EXPONENTIATION_BASIS":   "1",
		"MAX_LOGIN_ATTEMPTS_BEFORE_BLOCK":      "3",
		"MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS": "0",
		"PASSWORD_RESET_TOPIC":                 "password-reset",
		"ACCOUNT_BLOCKED_TOPIC":                "account-blocked",
		"ACCOUNT_CREATED_TOPIC":                "account-created",
		"JWT_SECRET":                           "01234567890123456789012345678901",
	} {
		t.Setenv(name, value)
	}
	if err := config.Setup(); err != nil {
		t.Fatalf("config.Setup() error = %v", err)
	}

	logger, err := DefaultLogger()
	if err != nil {
		t.Fatalf("DefaultLogger() error = %v", err)
	}
	if _, ok := logger.(localLogger); !ok {
		t.Fatalf("DefaultLogger() = %T, want local fallback", logger)
	}

	sender, err := DefaultMessageSender()
	if err != nil {
		t.Fatalf("DefaultMessageSender() error = %v", err)
	}
	if err := sender.Send("account-events", map[string]string{"account": "created"}); err != nil {
		t.Fatalf("fallback Send() error = %v", err)
	}
}

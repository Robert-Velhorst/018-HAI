package config

import (
	"strings"
	"time"
)

const (
	smtpHost            = "SMTP_HOST"
	smtpPort            = "SMTP_PORT"
	smtpUsername        = "SMTP_USERNAME"
	smtpPassword        = "SMTP_PASSWORD"
	smtpFrom            = "SMTP_FROM"
	smtpRequireStartTLS = "SMTP_REQUIRE_STARTTLS"
	smtpTimeoutSeconds  = "SMTP_TIMEOUT_SECONDS"
)

const defaultSMTPTimeoutSeconds = 10

// mailConfig is optional. Password recovery stays unavailable until all
// required delivery settings are supplied; the IDP must still boot without an
// email provider so a local administrator can use password or Google login.
type mailConfig struct {
	Host            string
	Port            string
	Username        string
	Password        string
	From            string
	RequireStartTLS bool
	DeliveryTimeout time.Duration
}

func newMailConfig() *mailConfig {
	timeoutSeconds := getEnvInt(smtpTimeoutSeconds, defaultSMTPTimeoutSeconds)
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultSMTPTimeoutSeconds
	}
	return &mailConfig{
		Host:            strings.TrimSpace(getEnvString(smtpHost, "")),
		Port:            strings.TrimSpace(getEnvString(smtpPort, "")),
		Username:        strings.TrimSpace(getEnvString(smtpUsername, "")),
		Password:        strings.TrimSpace(getEnvString(smtpPassword, "")),
		From:            strings.TrimSpace(getEnvString(smtpFrom, "")),
		RequireStartTLS: getEnvBool(smtpRequireStartTLS, true),
		DeliveryTimeout: time.Duration(timeoutSeconds) * time.Second,
	}
}

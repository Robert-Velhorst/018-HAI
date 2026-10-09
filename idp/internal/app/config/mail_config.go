package config

import "strings"

const (
	smtpHost            = "SMTP_HOST"
	smtpPort            = "SMTP_PORT"
	smtpUsername        = "SMTP_USERNAME"
	smtpPassword        = "SMTP_PASSWORD"
	smtpFrom            = "SMTP_FROM"
	smtpRequireStartTLS = "SMTP_REQUIRE_STARTTLS"
)

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
}

func newMailConfig() (*mailConfig, error) {
	requireTLS, err := getEnvBool(smtpRequireStartTLS, true)
	if err != nil {
		return nil, err
	}
	return &mailConfig{
		Host:            strings.TrimSpace(getEnvString(smtpHost, "")),
		Port:            strings.TrimSpace(getEnvString(smtpPort, "")),
		Username:        strings.TrimSpace(getEnvString(smtpUsername, "")),
		Password:        getEnvString(smtpPassword, ""),
		From:            strings.TrimSpace(getEnvString(smtpFrom, "")),
		RequireStartTLS: requireTLS,
	}, nil
}

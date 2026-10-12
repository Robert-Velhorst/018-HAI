package config

import (
	"os"
	"strings"
	"testing"
)

func TestBooleanSettingsPreserveMissingDefaultsAndValidValues(t *testing.T) {
	const key = "HAI_TEST_BOOLEAN_PARSER"
	t.Setenv(key, "unused")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	for _, defaultValue := range []bool{false, true} {
		if value, err := getEnvBool(key, defaultValue); err != nil || value != defaultValue {
			t.Fatal("missing default changed")
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{" true ", true}, {"TRUE", true}, {"1", true}, {"false", false}, {" False ", false}, {"0", false},
	} {
		t.Setenv(key, tc.raw)
		if value, err := getEnvBool(key, !tc.want); err != nil || value != tc.want {
			t.Fatalf("valid boolean %q failed: %v", tc.raw, err)
		}
	}
}

func TestBooleanSettingsRejectExplicitInvalidValuesWithoutEcho(t *testing.T) {
	for _, tc := range []struct {
		key   string
		build func() error
	}{
		{eventBusEnabled, func() error { _, err := newKafkaConfig(); return err }},
		{localLoginBypassEnabled, func() error { _, err := newLocalPreviewConfig(); return err }},
		{smtpRequireStartTLS, func() error { _, err := newMailConfig(); return err }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			for _, raw := range []string{"", " ", "yes", "synthetic-private-setting"} {
				t.Setenv(tc.key, raw)
				err := tc.build()
				if err == nil || !strings.Contains(err.Error(), tc.key) {
					t.Fatalf("invalid boolean accepted: %v", err)
				}
				if raw != "" && strings.TrimSpace(raw) != "" && strings.Contains(err.Error(), raw) {
					t.Fatal("raw value echoed")
				}
			}
		})
	}
}

func TestMailConfigRemainsOptionalAndTLSIsExplicit(t *testing.T) {
	for _, key := range []string{smtpHost, smtpPort, smtpUsername, smtpPassword, smtpFrom} {
		t.Setenv(key, "")
	}
	t.Setenv(smtpRequireStartTLS, "true")
	mail, err := newMailConfig()
	if err != nil || !mail.RequireStartTLS || mail.Host != "" {
		t.Fatal("optional mail default changed")
	}
	t.Setenv(smtpRequireStartTLS, "false")
	mail, err = newMailConfig()
	if err != nil || mail.RequireStartTLS {
		t.Fatal("explicit TLS choice changed")
	}
}

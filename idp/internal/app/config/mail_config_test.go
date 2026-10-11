package config

import (
	"testing"
	"time"
)

func TestMailConfigUsesBoundedSMTPTimeout(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv(smtpTimeoutSeconds, "")

		cfg := newMailConfig()

		if cfg.DeliveryTimeout != defaultSMTPTimeoutSeconds*time.Second {
			t.Fatalf("DeliveryTimeout = %s, want %s", cfg.DeliveryTimeout, defaultSMTPTimeoutSeconds*time.Second)
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Setenv(smtpTimeoutSeconds, "3")

		cfg := newMailConfig()

		if cfg.DeliveryTimeout != 3*time.Second {
			t.Fatalf("DeliveryTimeout = %s, want 3s", cfg.DeliveryTimeout)
		}
	})

	for _, invalid := range []string{"0", "-1", "not-a-number"} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			t.Setenv(smtpTimeoutSeconds, invalid)

			cfg := newMailConfig()

			if cfg.DeliveryTimeout != defaultSMTPTimeoutSeconds*time.Second {
				t.Fatalf("DeliveryTimeout = %s, want safe default", cfg.DeliveryTimeout)
			}
		})
	}
}

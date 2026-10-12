package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationDurationBoundsPreventOverflow(t *testing.T) {
	for _, setting := range []struct {
		name string
		unit time.Duration
	}{
		{baseBlockDurationMinutes, time.Minute},
		{minTimeBetweenAttemptsInSeconds, time.Second},
		{expirationTimeResetTokenInHours, time.Hour},
		{accessTokenDurationMinutes, time.Minute},
		{refreshTokenDurationDays, 24 * time.Hour},
	} {
		t.Run(setting.name, func(t *testing.T) {
			setValidAuthenticationEnv(t)
			limit := int64(math.MaxInt64) / int64(setting.unit)
			t.Setenv(setting.name, strconv.FormatInt(limit, 10))
			cfg, err := newAuthenticationConfig()
			if err != nil {
				t.Fatalf("representable boundary rejected: %v", err)
			}
			var count time.Duration
			switch setting.name {
			case baseBlockDurationMinutes:
				count = time.Duration(cfg.BaseBlockDurationMinutes)
			case minTimeBetweenAttemptsInSeconds:
				count = cfg.MinTimeBetweenAttemptsSeconds
			case expirationTimeResetTokenInHours:
				count = cfg.ExpirationTimeResetTokenHours
			case accessTokenDurationMinutes:
				count = cfg.AccessTokenDurationMinutes
			case refreshTokenDurationDays:
				count = cfg.RefreshTokenDurationDays / setting.unit
			}
			if count != time.Duration(limit) || count*setting.unit <= 0 {
				t.Fatal("valid duration changed or overflowed")
			}
			badValue := strconv.FormatInt(limit+1, 10)
			t.Setenv(setting.name, badValue)
			cfg, err = newAuthenticationConfig()
			if cfg != nil || err == nil || !strings.Contains(err.Error(), setting.name) || strings.Contains(err.Error(), badValue) {
				t.Fatalf("invalid duration accepted or echoed: %v", err)
			}
		})
	}
}

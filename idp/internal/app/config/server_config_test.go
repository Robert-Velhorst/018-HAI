package config

import (
	"os"
	"strings"
	"testing"
)

func TestServerConfigDefaults(t *testing.T) {
	for _, key := range []string{webServerPort, baseURL} {
		t.Setenv(key, "unused")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := newServerConfig()
	if err != nil || cfg.Port != "8080" || cfg.BaseURL != "/api" {
		t.Fatalf("defaults changed: %v, %v", cfg, err)
	}
}

func TestServerConfigNormalizesValidValues(t *testing.T) {
	for _, port := range []string{"1", "65535", " +08080 "} {
		for _, prefix := range []string{"", "/", "/api", "/api/", " /internal/api "} {
			t.Setenv(webServerPort, port)
			t.Setenv(baseURL, prefix)
			cfg, err := newServerConfig()
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(cfg.Port, " +") || cfg.BaseURL != strings.TrimSuffix(strings.TrimSpace(prefix), "/") {
				t.Fatalf("not normalized: %+v", cfg)
			}
		}
	}
}

func TestServerConfigRejectsInvalidValuesWithoutEcho(t *testing.T) {
	for _, tc := range []struct {
		key    string
		values []string
	}{
		{webServerPort, []string{"", "0", "-1", "65536", "private-port-secret", "999999999999999999999"}},
		{baseURL, []string{"api", "https://private-host", "/api?secret", "/api#secret", "/api/:private", "/api/*private", "/api\\private", "/api%2fprivate", "/api/../private", "/api//private", "/api/./private", "/api\nprivate"}},
	} {
		for _, value := range tc.values {
			t.Setenv(webServerPort, "8080")
			t.Setenv(baseURL, "/api")
			t.Setenv(tc.key, value)
			cfg, err := newServerConfig()
			if err == nil || cfg != nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("invalid %s accepted", tc.key)
			}
			if strings.Contains(value, "private") && strings.Contains(err.Error(), value) {
				t.Fatal("private input echoed")
			}
		}
	}
}

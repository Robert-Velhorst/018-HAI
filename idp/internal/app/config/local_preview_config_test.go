package config

import (
	"strings"
	"testing"
)

func TestLocalPreviewConfigRequiresExplicitLoopbackBind(t *testing.T) {
	for _, hostBind := range []string{"127.0.0.1", "::1", "[::1]"} {
		t.Run(strings.ReplaceAll(hostBind, ":", "_"), func(t *testing.T) {
			t.Setenv(localLoginBypassEnabled, "true")
			t.Setenv(firstRunAdminEmail, "owner@example.com")
			t.Setenv(gatewayHostBind, hostBind)
			t.Setenv(localPreviewGatewaySecret, strings.Repeat("a", 64))

			cfg, err := newLocalPreviewConfig()
			if err != nil {
				t.Fatalf("newLocalPreviewConfig() error = %v", err)
			}
			if !cfg.Enabled {
				t.Fatal("local preview should be enabled for an explicit loopback bind")
			}
		})
	}
}

func TestLocalPreviewConfigRequiresStrongGatewaySecret(t *testing.T) {
	for _, secret := range []string{"", "short", strings.Repeat("z", 64), strings.Repeat("a", 62)} {
		t.Run(secret, func(t *testing.T) {
			t.Setenv(localLoginBypassEnabled, "true")
			t.Setenv(firstRunAdminEmail, "owner@example.com")
			t.Setenv(gatewayHostBind, "127.0.0.1")
			t.Setenv(localPreviewGatewaySecret, secret)

			if _, err := newLocalPreviewConfig(); err == nil {
				t.Fatal("expected a missing or invalid gateway secret to fail closed")
			}
		})
	}
}

func TestLocalPreviewConfigRejectsUnsafeOrMissingBind(t *testing.T) {
	for _, hostBind := range []string{"", "0.0.0.0", "::", "192.168.1.20", "example.com", "localhost", "LOCALHOST", "[[::1]]", "[::1"} {
		t.Run(strings.ReplaceAll(hostBind, ":", "_"), func(t *testing.T) {
			t.Setenv(localLoginBypassEnabled, "true")
			t.Setenv(firstRunAdminEmail, "owner@example.com")
			t.Setenv(gatewayHostBind, hostBind)

			if _, err := newLocalPreviewConfig(); err == nil {
				t.Fatalf("expected unsafe bind %q to be rejected", hostBind)
			}
		})
	}
}

func TestLocalPreviewSessionAllowedRechecksCompleteConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  *localPreviewConfig
		want bool
	}{
		{name: "enabled numeric loopback owner", cfg: &localPreviewConfig{Enabled: true, OwnerEmail: "owner@example.com", GatewayHostBind: "127.0.0.1", GatewaySecret: strings.Repeat("a", 64)}, want: true},
		{name: "disabled", cfg: &localPreviewConfig{Enabled: false, OwnerEmail: "owner@example.com", GatewayHostBind: "127.0.0.1", GatewaySecret: strings.Repeat("a", 64)}},
		{name: "missing owner", cfg: &localPreviewConfig{Enabled: true, GatewayHostBind: "127.0.0.1"}},
		{name: "public bind", cfg: &localPreviewConfig{Enabled: true, OwnerEmail: "owner@example.com", GatewayHostBind: "0.0.0.0"}},
		{name: "hostname bind", cfg: &localPreviewConfig{Enabled: true, OwnerEmail: "owner@example.com", GatewayHostBind: "localhost"}},
		{name: "missing gateway secret", cfg: &localPreviewConfig{Enabled: true, OwnerEmail: "owner@example.com", GatewayHostBind: "127.0.0.1"}},
		{name: "nil config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cfg.LocalPreviewSessionAllowed(); got != test.want {
				t.Fatalf("LocalPreviewSessionAllowed() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLocalPreviewConfigAllowsPublicBindWhenBypassIsDisabled(t *testing.T) {
	t.Setenv(localLoginBypassEnabled, "false")
	t.Setenv(gatewayHostBind, "0.0.0.0")

	cfg, err := newLocalPreviewConfig()
	if err != nil {
		t.Fatalf("newLocalPreviewConfig() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("local preview must remain disabled")
	}
}

package config

import (
	"encoding/hex"
	"errors"
	"net"
	"strings"
)

const (
	localLoginBypassEnabled   = "LOCAL_LOGIN_BYPASS_ENABLED"
	firstRunAdminEmail        = "FIRST_RUN_ADMIN_EMAIL"
	gatewayHostBind           = "GATEWAY_HOST_BIND"
	localPreviewGatewaySecret = "LOCAL_PREVIEW_GATEWAY_SECRET"
)

// localPreviewConfig is deliberately opt-in. It exists for a single-user,
// loopback-bound installation where the operator wants to open the dashboard
// without typing credentials during local development or demonstration.
type localPreviewConfig struct {
	Enabled         bool
	OwnerEmail      string
	GatewayHostBind string
	GatewaySecret   string
}

func newLocalPreviewConfig() (*localPreviewConfig, error) {
	enabled, err := getEnvBool(localLoginBypassEnabled, false)
	if err != nil {
		return nil, err
	}
	hostBind := strings.TrimSpace(getEnvString(gatewayHostBind, ""))
	if enabled && !isLoopbackBind(hostBind) {
		return nil, errors.New("LOCAL_LOGIN_BYPASS_ENABLED requires GATEWAY_HOST_BIND to be a numeric loopback IP address")
	}
	secret := strings.TrimSpace(getEnvString(localPreviewGatewaySecret, ""))
	if enabled && !validLocalPreviewGatewaySecret(secret) {
		return nil, errors.New("LOCAL_LOGIN_BYPASS_ENABLED requires a 32-byte hexadecimal LOCAL_PREVIEW_GATEWAY_SECRET")
	}

	return &localPreviewConfig{
		Enabled:         enabled,
		OwnerEmail:      strings.TrimSpace(strings.ToLower(getEnvString(firstRunAdminEmail, ""))),
		GatewayHostBind: hostBind,
		GatewaySecret:   secret,
	}, nil
}

func validLocalPreviewGatewaySecret(secret string) bool {
	decoded, err := hex.DecodeString(secret)
	return err == nil && len(decoded) == 32
}

func isLoopbackBind(hostBind string) bool {
	host := strings.TrimSpace(hostBind)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if strings.ContainsAny(host, "[]") {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LocalPreviewSessionAllowed repeats the startup invariant at the credential
// issuance boundary. The configured gateway bind is only an exposure setting;
// request headers must never be used to infer the caller's network location.
func (c *localPreviewConfig) LocalPreviewSessionAllowed() bool {
	return c != nil && c.Enabled && c.OwnerEmail != "" && isLoopbackBind(c.GatewayHostBind) && validLocalPreviewGatewaySecret(c.GatewaySecret)
}

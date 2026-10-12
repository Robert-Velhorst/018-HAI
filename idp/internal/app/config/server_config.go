package config

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

const (
	webServerPort string = "WEB_SERVER_PORT"
	baseURL       string = "BASE_URL"
)

type serverConfig struct {
	Port    string
	BaseURL string
}

func newServerConfig() (*serverConfig, error) {
	port := strings.TrimSpace(getEnvString(webServerPort, "8080"))
	numPort, err := strconv.Atoi(port)
	if err != nil || numPort < 1 || numPort > 65535 {
		return nil, fmt.Errorf("%s must be a port between 1 and 65535", webServerPort)
	}

	prefix := strings.TrimSpace(getEnvString(baseURL, "/api"))
	// A static Gin group must not contain URL syntax or dynamic route parameters.
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "?#%\\:*") || strings.ContainsFunc(prefix, func(r rune) bool { return r <= ' ' || r == 127 })) {
		return nil, fmt.Errorf("%s must be a static absolute route prefix", baseURL)
	}
	if prefix != "" {
		if path.Clean(prefix) != strings.TrimSuffix(prefix, "/") && prefix != "/" {
			return nil, fmt.Errorf("%s must not contain ambiguous path segments", baseURL)
		}
		prefix = strings.TrimSuffix(prefix, "/")
	}

	return &serverConfig{
		Port:    strconv.Itoa(numPort),
		BaseURL: prefix,
	}, nil
}

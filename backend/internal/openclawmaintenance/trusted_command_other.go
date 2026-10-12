//go:build !windows

package openclawmaintenance

import "fmt"

func trustedMaintenanceCommandPath(string) (string, error) {
	return "", fmt.Errorf("trusted Windows maintenance commands are unavailable on this platform")
}

func validateTrustedSystemExecutable(string) error { return nil }

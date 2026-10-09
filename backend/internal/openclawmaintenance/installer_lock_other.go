//go:build !windows

package openclawmaintenance

import "fmt"

func openCompanionInstallerLocked(string) (lockedInstallerFile, error) {
	return nil, fmt.Errorf("Companion installer execution requires a Windows mutation lock")
}

func companionPowerShellPath() (string, error) {
	return "", fmt.Errorf("Companion Authenticode verification requires Windows PowerShell")
}

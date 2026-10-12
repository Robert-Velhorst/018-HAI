//go:build !windows

package main

import (
	"errors"
)

func ensureNativeWindowsHost() error {
	return errors.New("DeepSeek host execution is available only through the native Windows process supervisor")
}

func validateNativeWindowsExecutable(path string) error {
	return errors.New("DeepSeek host execution requires a native Windows executable")
}

func startSuspendedProcess(processSpec) (suspendedProcess, error) {
	return nil, errors.New("native Windows suspended process supervision is unavailable on this operating system")
}

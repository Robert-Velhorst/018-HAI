//go:build windows

package openclawmaintenance

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func trustedMaintenanceCommandPath(name string) (string, error) {
	systemDirectory, err := windowsSystemDirectory()
	if err != nil {
		return "", err
	}
	return trustedSystemCommandPath(systemDirectory, name)
}

func windowsSystemDirectory() (string, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if err := kernel32.Load(); err != nil {
		return "", err
	}
	buffer := make([]uint16, 32768)
	length, _, callErr := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		if callErr != nil {
			return "", callErr
		}
		return "", fmt.Errorf("Windows system directory is unavailable")
	}
	if length >= uintptr(len(buffer)) {
		return "", fmt.Errorf("Windows system directory path is too long")
	}
	directory := filepath.Clean(windows.UTF16ToString(buffer[:length]))
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("Windows system directory is not absolute")
	}
	return directory, nil
}

func validateTrustedSystemExecutable(path string) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pathPointer)
	if err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("system executable is a directory or reparse point")
	}
	return nil
}

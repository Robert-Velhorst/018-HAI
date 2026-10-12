//go:build windows

package openclawmaintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openCompanionInstallerLocked holds a read handle while allowing other readers
// but denying new write-capable handles and deletion/rename until Close.
func openCompanionInstallerLocked(path string) (lockedInstallerFile, error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_SEQUENTIAL_SCAN,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open installer with exclusive mutation lock: %w", err)
	}
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(handle, &info)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("inspect locked installer: %w", err)
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("inspect locked installer type: %w", err)
	}
	if fileType != windows.FILE_TYPE_DISK ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("installer is not a regular non-reparse-point file")
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("locked installer handle could not be wrapped")
	}
	return file, nil
}

func companionPowerShellPath() (string, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if err := kernel32.Load(); err != nil {
		return "", err
	}
	buffer := make([]uint16, 32768)
	length, _, callErr := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return "", callErr
	}
	if length >= uintptr(len(buffer)) {
		return "", fmt.Errorf("Windows system directory path is too long")
	}
	path := filepath.Join(windows.UTF16ToString(buffer[:length]), "WindowsPowerShell", "v1.0", "powershell.exe")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("system PowerShell executable is not a regular file")
	}
	return path, nil
}

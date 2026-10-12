//go:build linux

package automation

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

func openRegularScript(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Reject final-component symlink races and avoid blocking when a regular
	// allowlist entry has been replaced by a FIFO before the descriptor check.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("script target must be a regular file")
	}
	if err == nil && info.Size() > maxScriptBytes {
		err = fmt.Errorf("script exceeds maximum size of %d bytes", maxScriptBytes)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

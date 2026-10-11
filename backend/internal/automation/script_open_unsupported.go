//go:build !linux

package automation

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

func openRegularScript(ctx context.Context, _ string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("safe local script opening is unsupported on %s; script execution is disabled", runtime.GOOS)
}

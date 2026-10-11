//go:build !unix && !windows

package processcontrol

import (
	"os/exec"
	"time"
)

func configurePlatform(cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second
}

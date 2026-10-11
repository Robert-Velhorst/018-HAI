//go:build windows

package processcontrol

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

const createNewProcessGroup = 0x00000200

func configurePlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup, HideWindow: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		killer := exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := killer.Run(); err == nil {
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
}

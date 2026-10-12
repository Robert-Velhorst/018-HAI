//go:build linux

package automation

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const scriptProcessTreeVerifyTimeout = 2 * time.Second

// prepareScriptProcess isolates an approved script so context cancellation can
// terminate the script and its ordinary descendants as one process group.
func prepareScriptProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// verifyScriptProcessTreeStopped is deliberately conservative: it returns true
// only when the process group is gone or /proc shows that every remaining
// member is already a zombie and cannot perform further work.
func verifyScriptProcessTreeStopped(cmd *exec.Cmd) bool {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return false
	}
	groupID := cmd.Process.Pid
	deadline := time.Now().Add(scriptProcessTreeVerifyTimeout)
	for {
		err := syscall.Kill(-groupID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return false
		}

		live, readable := scriptProcessGroupHasLiveMembers(groupID)
		if !readable {
			return false
		}
		if !live {
			return true
		}

		// Repeat the group signal while checking. This closes the small race
		// where a member forks immediately before it receives the first signal.
		if err := syscall.Kill(-groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func scriptProcessGroupHasLiveMembers(groupID int) (live bool, readable bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, false
		}
		closeParen := strings.LastIndexByte(string(data), ')')
		if closeParen < 0 {
			return false, false
		}
		fields := strings.Fields(string(data[closeParen+1:]))
		// Fields begin with state, parent PID, and process group ID.
		if len(fields) < 3 {
			return false, false
		}
		memberGroup, err := strconv.Atoi(fields[2])
		if err != nil {
			return false, false
		}
		if memberGroup == groupID && fields[0] != "Z" && fields[0] != "X" {
			return true, true
		}
	}
	return false, true
}

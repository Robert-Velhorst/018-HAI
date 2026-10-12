//go:build windows

package automation

import (
	"errors"
	"os/exec"
)

// prepareScriptProcess fails closed on Windows until the launcher can assign
// the child to a kill-on-close Job Object before its primary thread runs.
// os/exec does not expose that pre-resume point; assigning a job after Start
// would let an approved script spawn an uncontained descendant in the gap.
func prepareScriptProcess(cmd *exec.Cmd) {
	if cmd.Err == nil {
		cmd.Err = errors.New("local script execution is disabled on Windows until Job Object process-tree containment is available")
	}
}

func verifyScriptProcessTreeStopped(_ *exec.Cmd) bool { return false }

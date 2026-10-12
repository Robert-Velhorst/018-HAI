//go:build !linux && !windows

package automation

import "os/exec"

// Keep other non-Linux builds on Go's default direct-process cancellation
// behavior. Linux process-group guarantees are not claimed on these platforms.
func prepareScriptProcess(_ *exec.Cmd) {}

func verifyScriptProcessTreeStopped(_ *exec.Cmd) bool { return false }

// Package processcontrol applies bounded, platform-specific process-tree
// cancellation to external commands started by HAI.
package processcontrol

import "os/exec"

// Configure makes CommandContext cancellation terminate the command's process
// tree and bounds pipe cleanup. It must be called before cmd.Start or cmd.Run.
func Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	configurePlatform(cmd)
}

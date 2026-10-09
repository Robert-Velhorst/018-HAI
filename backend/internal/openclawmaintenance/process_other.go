//go:build !windows

package openclawmaintenance

import "os/exec"

func hideWindow(*exec.Cmd) {}

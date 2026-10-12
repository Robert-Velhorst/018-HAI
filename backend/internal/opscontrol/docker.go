package opscontrol

import (
	"context"
	"os/exec"
	"time"
)

// DockerStatus is the truthful Docker Desktop dependency status (§31). HAI does
// not require Docker — the local safe worker runs without it — so a missing
// Docker is a warning, not a failure.
type DockerStatus struct {
	CLIAvailable  bool   `json:"cliAvailable"`
	DaemonRunning bool   `json:"daemonRunning"`
	Required      bool   `json:"required"`
	Detail        string `json:"detail"`
}

// DetectDocker checks Docker availability truthfully and best-effort. CLI
// presence is checked via PATH; daemon liveness via a short, bounded `docker
// info`. Any failure (including a sandbox that blocks exec) yields a truthful
// "not running" rather than a fabricated status.
func DetectDocker(ctx context.Context) DockerStatus {
	st := DockerStatus{Required: false}
	path, err := exec.LookPath("docker")
	if err != nil {
		st.Detail = "docker CLI not found on PATH; not required (local safe worker runs without Docker)"
		return st
	}
	st.CLIAvailable = true

	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "info", "--format", "{{.ServerVersion}}")
	if err := cmd.Run(); err != nil {
		st.Detail = "docker CLI present but daemon not responding; not required"
		return st
	}
	st.DaemonRunning = true
	st.Detail = "docker CLI present and daemon responding"
	return st
}

package processcontrol

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestConfigureTerminatesProcessTreeOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", "ping -n 30 127.0.0.1 >NUL")
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & wait")
	}
	Configure(cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled process unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process tree remained alive after cancellation")
	}
}

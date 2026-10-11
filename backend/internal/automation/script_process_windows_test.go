//go:build windows

package automation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareScriptProcessFailsClosedBeforeWindowsProcessStarts(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "script-ran.txt")
	cmd := exec.Command("cmd.exe", "/c", "echo unsafe>\""+marker+"\"")
	prepareScriptProcess(cmd)

	err := cmd.Start()
	if err == nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("Start succeeded without Windows Job Object containment")
	}
	if !strings.Contains(err.Error(), "Job Object process-tree containment") {
		t.Fatalf("Start error = %q, want fail-closed containment error", err)
	}
	if cmd.Process != nil {
		t.Fatalf("process was created despite fail-closed setup: pid=%d", cmd.Process.Pid)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("script performed a side effect despite fail-closed setup: stat err=%v", statErr)
	}
}

func TestPrepareScriptProcessPreservesExistingStartError(t *testing.T) {
	want := os.ErrPermission
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	cmd.Err = want

	prepareScriptProcess(cmd)

	if err := cmd.Start(); err != want {
		t.Fatalf("Start error = %v, want preserved error %v", err, want)
	}
	if cmd.Process != nil {
		t.Fatalf("process was created despite preexisting start error: pid=%d", cmd.Process.Pid)
	}
}

func TestVerifyScriptProcessTreeStoppedIsConservativeWithoutContainedProcess(t *testing.T) {
	if verifyScriptProcessTreeStopped(&exec.Cmd{}) {
		t.Fatal("verification claimed a process tree was stopped without a contained process")
	}
}

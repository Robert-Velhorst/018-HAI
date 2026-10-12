//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessTreeFixture(t *testing.T) {
	mode := os.Getenv("HAI_DSH_TREE_FIXTURE_MODE")
	if mode == "" {
		return
	}
	marker := os.Getenv("HAI_DSH_TREE_FIXTURE_MARKER")
	if marker == "" {
		t.Fatal("fixture marker path is required")
	}
	if mode == "child" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode != "root" {
		t.Fatalf("unexpected fixture mode %q", mode)
	}
	if err := appendFixturePID(marker, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessTreeFixture$")
	child.Env = setEnvironment(os.Environ(), "HAI_DSH_TREE_FIXTURE_MODE", "child")
	child.Env = setEnvironment(child.Env, "HAI_DSH_TREE_FIXTURE_MARKER", marker)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := appendFixturePID(marker, child.Process.Pid); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestWindowsExitCode259Fixture(t *testing.T) {
	if os.Getenv("HAI_DSH_EXIT_259_FIXTURE") == "1" {
		os.Exit(259)
	}
}

func TestWindowsTerminateRejectsMissingJobHandleAsUnverified(t *testing.T) {
	process := &windowsContainedProcess{}
	if err := process.Terminate(); !errors.Is(err, errProcessTreeTerminationUnverified) {
		t.Fatalf("Terminate without a Job Object handle = %v; want an unverified termination error", err)
	}
}

func TestWindowsCancellationTerminatesDescendantHoldingOutputHandles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "process-tree-pids.txt")
	workDir := filepath.Dir(marker)
	environment := safeEnvironment(nil, map[string]string{
		"HAI_DSH_TREE_FIXTURE_MODE":   "root",
		"HAI_DSH_TREE_FIXTURE_MARKER": marker,
	})
	ctx, cancel := context.WithCancel(context.Background())
	type runResult struct {
		result processResult
		err    error
	}
	finished := make(chan runResult, 1)
	go func() {
		result, err := runContainedProcess(ctx, processSpec{
			Path: executable,
			Args: []string{"-test.run=^TestWindowsProcessTreeFixture$"},
			Dir:  workDir,
			Env:  environment,
		}, func(context.Context) error { return nil }, nil, startSuspendedProcess)
		finished <- runResult{result: result, err: err}
	}()
	pids, err := waitForFixturePIDs(marker, 8*time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case outcome := <-finished:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("supervisor result = %#v, want cancellation", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the job did not return within five seconds")
	}
	for _, pid := range pids {
		if err := waitForWindowsProcessExit(pid, 2*time.Second); err != nil {
			t.Errorf("process %d survived cancellation: %v", pid, err)
		}
	}
}

func TestWindowsPreservesApplicationExitCode259(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runContainedProcess(context.Background(), processSpec{
		Path: executable,
		Args: []string{"-test.run=^TestWindowsExitCode259Fixture$"},
		Env:  safeEnvironment(nil, map[string]string{"HAI_DSH_EXIT_259_FIXTURE": "1"}),
	}, func(context.Context) error { return nil }, nil, startSuspendedProcess)
	if err != nil {
		t.Fatalf("run contained process: %v", err)
	}
	if result.ExitCode != 259 {
		t.Fatalf("exit code = %d, want application exit code 259", result.ExitCode)
	}
}

func appendFixturePID(path string, pid int) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintln(file, pid)
	return err
}

func waitForFixturePIDs(path string, timeout time.Duration) ([]uint32, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content, _ := os.ReadFile(path)
		lines := strings.Fields(string(content))
		if len(lines) >= 2 {
			pids := make([]uint32, 0, 2)
			for _, line := range lines[:2] {
				pid, err := strconv.ParseUint(line, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid process fixture PID %q: %w", line, err)
				}
				pids = append(pids, uint32(pid))
			}
			return pids, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, errors.New("timed out waiting for root and child process IDs")
}

func waitForWindowsProcessExit(pid uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return nil
			}
			return fmt.Errorf("open process handle: %w", err)
		}
		state, waitErr := windows.WaitForSingleObject(handle, 0)
		_ = windows.CloseHandle(handle)
		if waitErr != nil {
			return fmt.Errorf("check process exit: %w", waitErr)
		}
		if state == windows.WAIT_OBJECT_0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("process handle remained unsignaled until deadline")
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, key+"="+value)
}

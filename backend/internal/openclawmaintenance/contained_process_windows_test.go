//go:build windows

package openclawmaintenance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestContainedProcessHelper(t *testing.T) {
	marker := -1
	for i, arg := range os.Args {
		if arg == "--hai-contained-test" {
			marker = i
			break
		}
	}
	if marker < 0 || marker+1 >= len(os.Args) {
		return
	}
	switch os.Args[marker+1] {
	case "child":
		time.Sleep(30 * time.Second)
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestContainedProcessHelper$", "--", "--hai-contained-test", "child")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestWindowsJobContainsDescendantsAndWaitsForEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := runContainedCompanionProcess(ctx, os.Args[0], []string{
		"-test.run=^TestContainedProcessHelper$", "--", "--hai-contained-test", "parent",
	}, commandEnvironment(os.Environ(), nil))
	if !errors.Is(err, ErrInstallerDescendantsRemain) || result.TerminationStatus != "verified" || !result.DescendantsTerminated {
		t.Fatalf("descendant containment result = %+v, err=%v", result, err)
	}
}

func TestWindowsJobProcessCreationAndCleanExitAreContained(t *testing.T) {
	result, err := runContainedCompanionProcess(context.Background(), os.Args[0], []string{
		"-test.run=^TestContainedProcessHelper$", "--", "--hai-contained-test", "exit",
	}, commandEnvironment(os.Environ(), nil))
	if err != nil || result.TerminationStatus != "verified" {
		t.Fatalf("clean contained process result = %+v, err=%v", result, err)
	}
}

func TestUnverifiedWindowsTerminationIsReportedUnknown(t *testing.T) {
	err := &ProcessTreeTerminationError{Status: "unknown", cause: ErrProcessTreeTerminationUnverified}
	if processTerminationStatus(err) != "unknown" || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unverified termination error = %q, status=%q", err, processTerminationStatus(err))
	}
}

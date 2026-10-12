package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeepSeekHarnessRelativeStateDirectoryUsesValidatedAbsolutePath(t *testing.T) {
	processDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(processDir)
	stateDir := filepath.Join("..", "dsh-state-"+uuid.NewString())
	workspace, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	adapter := &deepSeekHarnessAdapter{
		enabled:                     true,
		executionEnabled:            true,
		executable:                  os.Args[0],
		expectedVersion:             "0.1.7-alpha.2",
		versionProbe:                func(context.Context) (string, error) { return "0.1.7-alpha.2", nil },
		workspace:                   workspace,
		workspaceRoot:               root,
		stateDir:                    stateDir,
		timeout:                     5 * time.Second,
		outputLimit:                 defaultOutputLimit,
		allowDirectExecutionForTest: true,
	}

	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("relative-state-path", "inspect workspace"))
	if result.Status != "needs_review" || result.ExitCode != 0 {
		t.Fatalf("execution result = %#v; want helper process success", result)
	}

	wantStateDir, err := filepath.Abs(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var actualStateDir string
	for _, line := range strings.Split(result.Output, "\n") {
		if strings.HasPrefix(line, "DSH_HOME=") {
			actualStateDir = strings.TrimPrefix(line, "DSH_HOME=")
			break
		}
	}
	if actualStateDir != wantStateDir {
		t.Fatalf("child DSH_HOME = %q, want validated absolute path %q", actualStateDir, wantStateDir)
	}

	childResolvedStateDir, err := filepath.Abs(filepath.Join(workspace, stateDir))
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, childResolvedStateDir)
	if err != nil {
		t.Fatal(err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative) {
		t.Fatalf("test setup failed: relative DSH_HOME did not escape root after child chdir: %q", childResolvedStateDir)
	}
}

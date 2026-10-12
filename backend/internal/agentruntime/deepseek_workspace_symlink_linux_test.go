//go:build linux

package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeepSeekHarnessWorkspaceSymlinkContainment(t *testing.T) {
	// Retain fixtures; no cleanup or real runtime execution is performed.
	base, err := os.MkdirTemp(os.Getenv("HAI_LINUX_WORKSPACE_ACCEPTANCE_ROOT"), "hai-dsh-workspace-symlink-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("retained synthetic Linux fixture: %s", base)
	root, outside := filepath.Join(base, "allowed"), filepath.Join(base, "outside")
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{root, outside, workspace, filepath.Join(outside, "nested-workspace")} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Linux symlink fixture unavailable (not a pass): %v", err)
	}
	for _, target := range []string{"plain-workspace", "workspace-link", "ancestor-link", "root-link"} {
		t.Run(target, func(t *testing.T) {
			adapter := &deepSeekHarnessAdapter{workspaceRoot: root, workspace: workspace}
			switch target {
			case "workspace-link":
				adapter.workspace = link
			case "ancestor-link":
				adapter.workspace = filepath.Join(link, "nested-workspace")
			case "root-link":
				adapter.workspaceRoot, adapter.workspace = link, filepath.Join(link, "nested-workspace")
			}
			reason := adapter.workspaceBlockedReason()
			if target == "plain-workspace" {
				if reason != "" {
					t.Fatalf("plain contained workspace rejected: %s", reason)
				}
			} else if !strings.Contains(reason, "must not contain symbolic links") {
				t.Fatalf("%s did not reject link components: %q", target, reason)
			}
		})
	}
}

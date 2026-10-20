package agentruntime

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestOpenClawSetEcosystemPathStoresCanonicalAllowedSymlinkTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	initial := filepath.Join(root, "openclaw-initial.zip")
	target := filepath.Join(root, "openclaw-target.zip")
	for _, archive := range []string{initial, target} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write fixture archive: %v", err)
		}
	}
	link := filepath.Join(root, "openclaw-link.zip")
	if err := os.Symlink(target, link); err != nil {
		const windowsErrorPrivilegeNotHeld = syscall.Errno(1314)
		if runtime.GOOS == "windows" && errors.Is(err, windowsErrorPrivilegeNotHeld) {
			t.Skipf("Windows symlink privileges unavailable: %v", err)
		}
		t.Fatalf("create allowed symlink fixture: %v", err)
	}
	adapter := &openClawAdapter{
		workspace:     root,
		workspaceRoot: root,
		ecosystemPath: initial,
	}
	if err := adapter.setEcosystemPath(link); err != nil {
		t.Fatalf("select allowed symlink: %v", err)
	}
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target archive: %v", err)
	}
	selected, _ := adapter.ecosystemState()
	if selected != canonicalTarget {
		t.Fatalf("selected path = %q, want canonical target %q", selected, canonicalTarget)
	}
}

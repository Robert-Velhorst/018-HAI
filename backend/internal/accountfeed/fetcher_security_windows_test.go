//go:build windows

package accountfeed

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalImportRejectsWindowsJunction(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.json"), []byte(`[{"externalId":"private","title":"private"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "linked")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
	assertLocalFeedPathRejected(t, root, "linked/private.json")
	assertLocalFeedPathRejected(t, junction, "private.json")
}

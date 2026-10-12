//go:build windows

package openclawmaintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompanionInstallerLockDeniesWriteAndRenameUntilProcessReturns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "OpenClawCompanion-Setup-x64.exe")
	content := []byte("signed-installer-test-fixture")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	artifact := companionInstallerArtifact{Path: path, SHA256: hashBytes(content), PublisherThumbprint: "0123456789abcdef0123456789abcdef01234567"}

	assertLocked := func(stage string) {
		t.Helper()
		writer, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
		if err == nil {
			_ = writer.Close()
			t.Fatalf("write-open unexpectedly succeeded during %s", stage)
		}
		replacement := path + ".replacement"
		if err := os.Rename(path, replacement); err == nil {
			t.Fatalf("rename unexpectedly succeeded during %s", stage)
		}
		if _, err := os.Stat(replacement); !os.IsNotExist(err) {
			t.Fatalf("replacement path appeared during %s: %v", stage, err)
		}
	}

	verifyCalled, runCalled := false, false
	err := applyCompanionInstallerWith(context.Background(), artifact,
		openCompanionInstallerLocked,
		func(context.Context, string, string) error {
			verifyCalled = true
			assertLocked("Authenticode verification")
			return nil
		},
		func(context.Context, string) error {
			runCalled = true
			assertLocked("installer process completion")
			return nil
		})
	if err != nil || !verifyCalled || !runCalled {
		t.Fatalf("locked apply did not complete through verification and execution: err=%v verified=%v ran=%v", err, verifyCalled, runCalled)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(content) {
		t.Fatalf("installer bytes changed while lock was held: %q err=%v", got, err)
	}

	// The deferred close must release both mutation restrictions on every return.
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write remained blocked after apply returned: %v", err)
	}
	replacement := path + ".after-close"
	if err := os.Rename(path, replacement); err != nil {
		t.Fatalf("rename remained blocked after apply returned: %v", err)
	}
}

func TestCompanionInstallerLockFailsClosedForMissingPath(t *testing.T) {
	if _, err := openCompanionInstallerLocked(filepath.Join(t.TempDir(), "missing.exe")); err == nil {
		t.Fatal("opened a missing installer path")
	}
}

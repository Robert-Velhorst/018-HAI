package openclawmaintenance

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type cancellingInstallerRead struct {
	*testLockedInstaller
	cancel context.CancelFunc
	reads  int
}

func (f *cancellingInstallerRead) Read(p []byte) (int, error) {
	f.reads++
	n, err := f.Reader.Read(p)
	f.cancel()
	return n, err
}

func TestCompanionInstallerCancellationStopsNextAdmissionStage(t *testing.T) {
	content := "approved installer bytes"
	artifact := companionInstallerArtifact{Path: "installer.exe", SHA256: hashBytes([]byte(content)), PublisherThumbprint: strings.Repeat("ab", 20)}
	for _, stage := range []string{"before open", "after open", "during hash", "after publisher", "after run"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := &testLockedInstaller{Reader: strings.NewReader(content)}
			opened, verified, launched := 0, 0, 0
			if stage == "before open" {
				cancel()
			}
			err := applyCompanionInstallerWith(ctx, artifact,
				func(string) (lockedInstallerFile, error) {
					opened++
					if stage == "after open" {
						cancel()
					}
					if stage == "during hash" {
						return &cancellingInstallerRead{testLockedInstaller: file, cancel: cancel}, nil
					}
					return file, nil
				},
				func(context.Context, string, string) error {
					verified++
					if stage == "after publisher" {
						cancel()
					}
					return nil
				},
				func(context.Context, string) error {
					launched++
					if stage == "after run" {
						cancel()
					}
					return nil
				})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation was lost: %v", err)
			}
			wantOpened, wantVerified, wantLaunched := 1, 0, 0
			if stage == "before open" {
				wantOpened = 0
			}
			if stage == "after publisher" || stage == "after run" {
				wantVerified = 1
			}
			if stage == "after run" {
				wantLaunched = 1
			}
			if opened != wantOpened || verified != wantVerified || launched != wantLaunched || file.closeCalls != wantOpened {
				t.Fatalf("unexpected stage effects: open=%d verify=%d launch=%d close=%d", opened, verified, launched, file.closeCalls)
			}
		})
	}
}

func TestCompanionInstallerMissingContextDoesNotOpen(t *testing.T) {
	content := []byte("approved installer")
	artifact := companionInstallerArtifact{Path: "installer.exe", SHA256: hashBytes(content), PublisherThumbprint: strings.Repeat("ab", 20)}
	called := false
	err := applyCompanionInstallerWith(nil, artifact,
		func(string) (lockedInstallerFile, error) { called = true; return nil, io.EOF },
		func(context.Context, string, string) error { called = true; return nil },
		func(context.Context, string) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("missing context admitted work: err=%v called=%v", err, called)
	}
}

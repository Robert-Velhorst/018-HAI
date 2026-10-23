package infra

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

type failingDownScriptFS struct {
	fs.FS
	err error
}

func (f failingDownScriptFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, ".down.sql") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return f.FS.Open(name)
}

func TestLoadMigrationsDoesNotSilentlyDiscardUnreadableDownScript(t *testing.T) {
	base := fstest.MapFS{
		"pre/0001_probe.up.sql":   &fstest.MapFile{Data: []byte("SELECT 1;")},
		"pre/0001_probe.down.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
	}
	for _, cause := range []error{fs.ErrPermission, io.ErrUnexpectedEOF} {
		t.Run(cause.Error(), func(t *testing.T) {
			loaded, err := loadMigrations(failingDownScriptFS{FS: base, err: cause}, "pre")
			if !errors.Is(err, cause) || len(loaded) != 0 {
				t.Fatalf("unreadable down script returned migrations=%#v err=%v, want preserved read cause", loaded, err)
			}
		})
	}
}

func TestLoadMigrationsStillAllowsMissingOptionalDownScript(t *testing.T) {
	loaded, err := loadMigrations(fstest.MapFS{
		"pre/0001_probe.up.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}, "pre")
	if err != nil || len(loaded) != 1 || loaded[0].DownSQL != "" {
		t.Fatalf("missing optional down script returned migrations=%#v err=%v", loaded, err)
	}
}

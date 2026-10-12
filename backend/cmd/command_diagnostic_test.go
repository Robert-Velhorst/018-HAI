package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCommandDiagnosticsDoNotExposeCredentials(t *testing.T) {
	for _, stage := range []string{"open", "close", "reconcile", "migrate_up", "migrate_down", "migrate_status"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("HAI_COMMAND_TIMEOUT", "5s")
			failure := errors.New("synthetic failure password=synthetic-private-password")
			connector := &commandTestConnector{}
			if stage == "close" {
				connector.closeFailure = failure
			}
			if stage == "reconcile" || stage == "migrate_status" {
				connector.queryFailure = failure
			}
			db, _ := commandTestDatabase(t, connector)
			opener := func(context.Context) (*gorm.DB, error) { return db, nil }
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			stdoutReader, stdoutWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdoutReader.Close()
			defer stdoutWriter.Close()
			previous := os.Stderr
			previousStdout := os.Stdout
			os.Stderr = writer
			os.Stdout = stdoutWriter
			db.Logger = logger.New(log.New(stdoutWriter, "", 0), logger.Config{LogLevel: logger.Error})
			defer func() { os.Stderr, os.Stdout = previous, previousStdout }()
			var code int
			switch stage {
			case "open":
				code = withCommandDatabase("test", func(context.Context) (*gorm.DB, error) { return nil, failure }, func(*gorm.DB) int {
					t.Fatal("failed opening executed command")
					return 0
				})
			case "close":
				code = withCommandDatabase("test", opener, func(bound *gorm.DB) int {
					pool, err := bound.DB()
					if err != nil {
						t.Fatal(err)
					}
					if err := pool.PingContext(bound.Statement.Context); err != nil {
						t.Fatal(err)
					}
					return 0
				})
			case "reconcile":
				code = runReconcileWithDatabase(opener)
			default:
				action := strings.TrimPrefix(stage, "migrate_")
				code = executeMigration(migrationRequest{action: action, dir: "post", version: "synthetic"}, migrationCommands{
					open:     opener,
					apply:    func(*gorm.DB) error { return failure },
					rollback: func(*gorm.DB, string, string) error { return failure },
				})
			}
			os.Stderr = previous
			os.Stdout = previousStdout
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := stdoutWriter.Close(); err != nil {
				t.Fatal(err)
			}
			stdout, err := io.ReadAll(stdoutReader)
			if err != nil {
				t.Fatal(err)
			}
			if len(stdout) != 0 {
				t.Fatalf("failed command leaked raw SQL diagnostics: %q", stdout)
			}
			if code != 1 || !strings.Contains(string(output), "synthetic failure") || strings.Contains(string(output), "synthetic-private-password") {
				t.Fatalf("expected sanitized failure, code=%d output=%q", code, output)
			}
		})
	}
}

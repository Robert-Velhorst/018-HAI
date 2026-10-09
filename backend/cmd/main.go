package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/reconcile"
	"automation-hub-backend/internal/router"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	// Subcommands run a one-shot task and exit without starting the HTTP server.
	// Only the no-argument invocation starts the server.
	if err := validateCommandArguments(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, safety.RedactSecrets(err.Error()))
		os.Exit(2)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			config.Init()
			os.Exit(doctor.Render(os.Stdout, doctor.Diagnose(config.AppConfig)))
		case "reconcile":
			os.Exit(runReconcile())
		case "migrate":
			os.Exit(runMigrate(os.Args[2:]))
		}
	}

	config.Init()

	err := router.Initialize()
	if err != nil {
		panic(err)
	}
}

func validateCommandArguments(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "doctor", "reconcile":
		if len(args) != 1 {
			return fmt.Errorf("%s does not accept additional arguments", args[0])
		}
		return nil
	case "migrate":
		_, err := parseMigrationRequest(args[1:])
		return err
	default:
		return fmt.Errorf("unknown command %q; use doctor|reconcile|migrate, or no arguments to serve", args[0])
	}
}

// runReconcile scans stored memories for broken invariants and prints a
// dry-run report. It requires a database connection and never mutates data.
func runReconcile() int {
	config.Init()
	// The cached application opener can migrate on startup. A dry-run command
	// instead owns a migration-free pool, even when startup migrations are enabled.
	return runReconcileWithDatabase(infra.OpenDefaultDBContext)
}

func runReconcileWithDatabase(open func(context.Context) (*gorm.DB, error)) int {
	return withCommandDatabase("reconcile", open, func(db *gorm.DB) int {
		summary, err := streamReconcile(db, os.Stdout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reconcile: incomplete read-only scan after %d memories and %d findings; output may be partial: %s\n", summary.scanned, summary.findings, safety.RedactSecrets(err.Error()))
			return 1
		}
		return 0
	})
}

type reconciliationSummary struct{ scanned, findings int }

// Keep SQL NULL distinct from a valid zero confidence without changing the API model.
type reconciliationMemoryRow struct {
	ID         uuid.UUID
	Content    string
	Kind       string
	Confidence *float64
	Tags       string
}

func streamReconcile(db *gorm.DB, output io.Writer) (summary reconciliationSummary, err error) {
	err = db.Transaction(func(tx *gorm.DB) (scanErr error) {
		rows, queryErr := tx.Model(&models.ContextMemory{}).
			Select("id", "content", "kind", "confidence", "tags").Order("updated_at desc").Rows()
		if queryErr != nil {
			return queryErr
		}
		defer func() {
			if closeErr := rows.Close(); closeErr != nil {
				scanErr = errors.Join(scanErr, closeErr)
			}
		}()
		for rows.Next() {
			if err := tx.Statement.Context.Err(); err != nil {
				return err
			}
			var record reconciliationMemoryRow
			if err := tx.ScanRows(rows, &record); err != nil {
				return err
			}
			if record.Confidence == nil {
				return fmt.Errorf("cannot audit memory %s: confidence is NULL; restore it from verified source information", record.ID)
			}
			memory := models.ContextMemory{
				ID: record.ID, Content: record.Content, Kind: record.Kind,
				Confidence: *record.Confidence, Tags: record.Tags,
			}
			summary.scanned++
			if finding, found := reconcile.ScanMemory(memory); found {
				summary.findings++
				if summary.findings == 1 {
					if err := writeReconciliationText(output, "reconcile: provisional findings; scan not yet complete\n"); err != nil {
						return fmt.Errorf("write reconciliation status: %w", err)
					}
				}
				if err := writeReconciliationText(output, fmt.Sprintf("- %s repairable=%v: %s\n", finding.MemoryID, finding.Repairable, finding.Repair)); err != nil {
					return fmt.Errorf("write reconciliation finding: %w", err)
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.Statement.Context.Err()
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return summary, err
	}
	if err = db.Statement.Context.Err(); err != nil {
		return summary, err
	}
	if err = writeReconciliationText(output, fmt.Sprintf("reconcile: scanned %d memories, %d finding(s)\n", summary.scanned, summary.findings)); err != nil {
		return summary, fmt.Errorf("write reconciliation summary: %w", err)
	}
	if summary.findings == 0 {
		if err = db.Statement.Context.Err(); err != nil {
			return summary, err
		}
		if err = writeReconciliationText(output, "reconcile: all memories satisfy their invariants\n"); err != nil {
			return summary, fmt.Errorf("write reconciliation result: %w", err)
		}
	}
	return summary, db.Statement.Context.Err()
}

func writeReconciliationText(output io.Writer, text string) error {
	n, err := io.WriteString(output, text)
	if err != nil {
		return err
	}
	if n != len(text) {
		return io.ErrShortWrite
	}
	return nil
}

// A command owns this pool, never the cached server pool. The defer runs before
// main calls os.Exit; cleanup failure must not be reported as command success.
func withCommandDatabase(name string, open func(context.Context) (*gorm.DB, error), run func(*gorm.DB) int) (code int) {
	timeout := 30 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("HAI_COMMAND_TIMEOUT")); raw != "" {
		var err error
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			fmt.Fprintln(os.Stderr, "HAI_COMMAND_TIMEOUT must be a positive duration with units")
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	db, err := open(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s requires a database connection: %s\n", name, safety.RedactSecrets(err.Error()))
		return 1
	}
	if db == nil || db.Config == nil {
		fmt.Fprintf(os.Stderr, "%s: database pool is unavailable\n", name)
		return 1
	}
	pool, err := db.DB()
	if err != nil || pool == nil {
		fmt.Fprintf(os.Stderr, "%s: cannot acquire owned database pool: %s\n", name, safety.RedactSecrets(fmt.Sprint(err)))
		return 1
	}
	defer func() {
		cancel()
		if err := pool.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: failed to close database pool: %s\n", name, safety.RedactSecrets(err.Error()))
			code = 1
		}
	}()
	// SQL errors and parameters can contain credentials or private records.
	// Command failures are reported through the sanitized diagnostics above.
	code = run(db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)}).WithContext(ctx))
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: command context ended: %s\n", name, safety.RedactSecrets(err.Error()))
		return 1
	}
	return code
}

type migrationCommands struct {
	open     func(context.Context) (*gorm.DB, error)
	apply    func(*gorm.DB) error
	rollback func(*gorm.DB, string, string) error
}

// runMigrate drives the versioned SQL migrations:
//
//	migrate status          show applied/pending migrations without changing anything
//	migrate up              apply all pending migrations (pre + AutoMigrate + post)
//	migrate down [pre|post/]<version>
//	                        roll back a single migration (post by default)
func runMigrate(args []string) int {
	request, err := parseMigrationRequest(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, safety.RedactSecrets(err.Error()))
		return 1
	}
	config.Init()
	return executeMigration(request, migrationCommands{
		open:  infra.OpenDefaultDBContext,
		apply: infra.RunMigrations,
		rollback: func(db *gorm.DB, dir, version string) error {
			return infra.RollbackMigration(db, migrations.Files, dir, version)
		},
	})
}

type migrationRequest struct {
	action  string
	dir     string
	version string
}

func parseMigrationRequest(args []string) (migrationRequest, error) {
	request := migrationRequest{action: "status"}
	if len(args) > 0 {
		request.action = args[0]
	}
	switch request.action {
	case "up", "status":
		if len(args) > 1 {
			return request, fmt.Errorf("migrate %s does not accept additional arguments", request.action)
		}
	case "down":
		if len(args) != 2 {
			return request, fmt.Errorf("migrate down requires exactly one target, e.g. pre/0003_framework_registry")
		}
		var err error
		request.dir, request.version, err = parseMigrationTarget(args[1])
		if err != nil {
			return request, fmt.Errorf("migrate down failed: %w", err)
		}
	default:
		return request, fmt.Errorf("unknown migrate action %q; use status|up|down", request.action)
	}
	return request, nil
}

func executeMigration(request migrationRequest, commands migrationCommands) int {
	return withCommandDatabase("migrate "+request.action, commands.open, func(db *gorm.DB) int {
		switch request.action {
		case "up":
			// An explicit migration command is deliberately independent of
			// DB_MIGRATIONS_ENABLED. Production API processes can run with a
			// DML-only role while an operator supplies the schema-owner account
			// only for this reviewed, one-shot command.
			if err := commands.apply(db); err != nil {
				fmt.Fprintln(os.Stderr, "migrate up failed:", safety.RedactSecrets(err.Error()))
				return 1
			}
			fmt.Println("migrate up: all pending migrations applied")
			return renderMigrationStatus(db)
		case "down":
			if err := commands.rollback(db, request.dir, request.dir+"/"+request.version); err != nil {
				fmt.Fprintln(os.Stderr, "migrate down failed:", safety.RedactSecrets(err.Error()))
				return 1
			}
			fmt.Printf("migrate down: rolled back %s/%s\n", request.dir, request.version)
			return 0
		default:
			return renderMigrationStatus(db)
		}
	})
}

func renderMigrationStatus(db *gorm.DB) int {
	for _, dir := range []string{"pre", "post"} {
		status, err := infra.Status(db, migrations.Files, dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate status:", safety.RedactSecrets(err.Error()))
			return 1
		}
		fmt.Printf("[%s] applied=%d pending=%d\n", dir, len(status.Applied), len(status.Pending))
		for _, v := range status.Applied {
			fmt.Printf("  applied  %s\n", v)
		}
		for _, v := range status.Pending {
			fmt.Printf("  pending  %s\n", v)
		}
	}
	return 0
}

func parseMigrationTarget(target string) (string, string, error) {
	target = strings.TrimSpace(strings.ReplaceAll(target, "\\", "/"))
	if target == "" {
		return "", "", fmt.Errorf("migration target is required")
	}
	dir := "post"
	version := target
	if strings.Contains(target, "/") {
		parts := strings.SplitN(target, "/", 2)
		dir = parts[0]
		version = parts[1]
	}
	if dir != "pre" && dir != "post" {
		return "", "", fmt.Errorf("migration phase must be pre or post")
	}
	if version == "" ||
		strings.HasPrefix(version, "-") ||
		strings.Contains(version, "/") ||
		version == "." ||
		version == ".." ||
		strings.Contains(version, "\x00") {
		return "", "", fmt.Errorf("invalid migration version")
	}
	return dir, version, nil
}

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/reconcile"
	"automation-hub-backend/internal/router"
	"automation-hub-backend/migrations"
)

func main() {
	// Subcommands run a one-shot task and exit without starting the HTTP server.
	// Any other invocation starts the server exactly as before.
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := router.InitializeContext(ctx)
	if err != nil {
		panic(err)
	}
}

// runReconcile scans stored memories for broken invariants and prints a
// dry-run report. It requires a database connection and never mutates data.
func runReconcile() int {
	config.Init()
	db, err := infra.GetDefaultDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reconcile requires a database connection:", err)
		return 1
	}
	memories, err := memory.NewGormRepository(db).FindAll("", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reconcile: failed to load memories:", err)
		return 1
	}
	report := reconcile.ScanMemories(memories)
	fmt.Printf("reconcile: scanned %d memories, %d finding(s)\n", report.Scanned, len(report.Findings))
	for _, f := range report.Findings {
		fmt.Printf("- %s repairable=%v: %s\n", f.MemoryID, f.Repairable, f.Repair)
	}
	if report.Clean() {
		fmt.Println("reconcile: all memories satisfy their invariants")
	}
	return 0
}

// runMigrate drives the versioned SQL migrations:
//
//	migrate status          show applied/pending migrations without changing anything
//	migrate up              apply all pending migrations (pre + AutoMigrate + post)
//	migrate down [pre|post/]<version>
//	                        roll back a single migration (post by default)
func runMigrate(args []string) int {
	config.Init()
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "up":
		db, err := infra.OpenDefaultDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate up failed:", err)
			return 1
		}
		if err := infra.RunMigrations(db); err != nil {
			fmt.Fprintln(os.Stderr, "migrate up failed:", err)
			return 1
		}
		if err := infra.ProvisionConfiguredRuntimeRole(db, config.AppConfig.DbName); err != nil {
			fmt.Fprintln(os.Stderr, "migrate up failed:", err)
			return 1
		}
		fmt.Println("migrate up: all pending migrations applied")
		return runMigrate([]string{"status"})
	case "status":
		db, err := infra.OpenDefaultDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate status requires a database connection:", err)
			return 1
		}
		for _, dir := range []string{"pre", "post"} {
			status, err := infra.Status(db, migrations.Files, dir)
			if err != nil {
				fmt.Fprintln(os.Stderr, "migrate status:", err)
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
	case "down":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "migrate down requires a target, e.g. pre/0003_framework_registry")
			return 1
		}
		dir, version, err := parseMigrationTarget(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate down failed:", err)
			return 1
		}
		db, err := infra.OpenDefaultDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate down requires a database connection:", err)
			return 1
		}
		if err := infra.RollbackMigration(db, migrations.Files, dir, dir+"/"+version); err != nil {
			fmt.Fprintln(os.Stderr, "migrate down failed:", err)
			return 1
		}
		fmt.Printf("migrate down: rolled back %s/%s\n", dir, version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown migrate action %q; use status|up|down\n", action)
		return 1
	}
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
		strings.Contains(version, "/") ||
		version == "." ||
		version == ".." ||
		strings.Contains(version, "\x00") {
		return "", "", fmt.Errorf("invalid migration version")
	}
	return dir, version, nil
}

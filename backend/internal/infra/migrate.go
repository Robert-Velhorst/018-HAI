package infra

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Versioned SQL migration runner.
//
// It applies reviewable, ordered .sql files and records each applied version in
// schema_migrations, so the schema is reproducible and auditable rather than
// derived implicitly from Gorm AutoMigrate. Migrations are split into two
// phases (see the migrations package): "pre" runs before AutoMigrate, "post"
// runs after it, which lets us retire AutoMigrate incrementally without
// reordering table-dependent DDL.

const (
	schemaMigrationsTable    = "schema_migrations"
	migrationAdvisoryLockKey = int64(0x4841494d494752)
)

// Migration is one versioned change loaded from an embedded directory.
type Migration struct {
	Version string // e.g. "post/0001_conversation_owner_identity"
	UpSQL   string
	DownSQL string
}

// MigrationChecksum binds the exact checked-in up and down SQL bytes to the
// version. A rollback script change is therefore detected just like an up
// migration change.
func MigrationChecksum(migration Migration) string {
	h := sha256.New()
	_, _ = h.Write([]byte(migration.Version))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(migration.UpSQL))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(migration.DownSQL))
	return hex.EncodeToString(h.Sum(nil))
}

// loadMigrations reads every NNNN_name.up.sql (and optional matching .down.sql)
// from dir in fsys and returns them sorted by version.
func loadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		upSQL, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		migration := Migration{Version: dir + "/" + base, UpSQL: string(upSQL)}
		if downSQL, err := fs.ReadFile(fsys, dir+"/"+base+".down.sql"); err == nil {
			migration.DownSQL = string(downSQL)
		}
		migrations = append(migrations, migration)
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// pendingMigrations returns, in order, the migrations whose version is not in
// applied. Pure and DB-free so ordering/idempotency can be unit-tested.
func pendingMigrations(all []Migration, applied map[string]bool) []Migration {
	pending := make([]Migration, 0, len(all))
	for _, migration := range all {
		if !applied[migration.Version] {
			pending = append(pending, migration)
		}
	}
	return pending
}

// validateAppliedVersions fails closed when the ledger contains an applied
// migration for this phase that is absent from the checked-in migration set.
// Silently ignoring such entries can make status look clean and let newer
// migrations run after a missing or renamed predecessor.
func validateAppliedVersions(all []Migration, applied map[string]bool, dir string) error {
	known := make(map[string]struct{}, len(all))
	for _, migration := range all {
		known[migration.Version] = struct{}{}
	}

	prefix := dir + "/"
	unknown := make([]string, 0)
	for version := range applied {
		if !strings.HasPrefix(version, prefix) {
			continue
		}
		if _, ok := known[version]; !ok {
			unknown = append(unknown, version)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("schema migration ledger contains applied %s versions missing from migration files: %q", dir, unknown)
	}

	gap := false
	outOfOrder := make([]string, 0)
	for _, migration := range all {
		if !applied[migration.Version] {
			gap = true
			continue
		}
		if gap {
			outOfOrder = append(outOfOrder, migration.Version)
		}
	}
	if len(outOfOrder) > 0 {
		return fmt.Errorf("schema migration ledger contains out-of-order applied %s versions after a missing predecessor: %q", dir, outOfOrder)
	}
	return nil
}

// dollarTagAt reports the dollar-quote delimiter starting at index i (e.g. `$$`
// or `$tag$`) and its width, or width 0 if there is none. Dollar quoting matters
// because a DO $$ ... $$ block contains semicolons that must not split it.
func dollarTagAt(script string, i int) (string, int) {
	if i >= len(script) || script[i] != '$' {
		return "", 0
	}
	if i+1 < len(script) && script[i+1] == '$' {
		return "$$", 2
	}
	if i+1 >= len(script) || !isDollarTagStart(script[i+1]) {
		return "", 0
	}
	for j := i + 2; j < len(script); j++ {
		c := script[j]
		if c == '$' {
			return script[i : j+1], j + 1 - i
		}
		if !isDollarTagChar(c) {
			return "", 0
		}
	}
	return "", 0
}

func isDollarTagStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDollarTagChar(c byte) bool {
	return isDollarTagStart(c) || (c >= '0' && c <= '9')
}

func isSQLIdentifierByte(c byte) bool {
	return isDollarTagChar(c) || c == '$'
}

func isEscapeStringPrefix(script string, quote int) bool {
	if quote == 0 || (script[quote-1] != 'e' && script[quote-1] != 'E') {
		return false
	}
	return quote == 1 || !isSQLIdentifierByte(script[quote-2])
}

// trimStatement drops leading blank/comment-only lines and surrounding space.
// Comments inside a statement body are preserved so dollar-quoted blocks stay
// byte-for-byte intact.
func trimStatement(raw string) string {
	lines := strings.Split(raw, "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			start++
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

// splitSQLStatements breaks a migration file into individual statements so the
// runner does not depend on the driver's multi-statement behavior. It splits on
// top-level semicolons while preserving quoted strings, identifiers, comments,
// and dollar-quoted blocks.
func splitSQLStatements(script string) []string {
	statements := []string{}
	var current strings.Builder
	flush := func() {
		if statement := trimStatement(current.String()); statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}
	openTag := ""
	quote := byte(0)
	escapeString := false
	lineComment := false
	blockCommentDepth := 0
	for i := 0; i < len(script); {
		c := script[i]
		if lineComment {
			current.WriteByte(c)
			i++
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockCommentDepth > 0 {
			if i+1 < len(script) && script[i:i+2] == "/*" {
				blockCommentDepth++
				current.WriteString("/*")
				i += 2
				continue
			}
			if i+1 < len(script) && script[i:i+2] == "*/" {
				blockCommentDepth--
				current.WriteString("*/")
				i += 2
				continue
			}
			current.WriteByte(c)
			i++
			continue
		}
		if quote != 0 {
			current.WriteByte(c)
			i++
			if quote == '\'' && escapeString && c == '\\' && i < len(script) {
				current.WriteByte(script[i])
				i++
				continue
			}
			if c != quote || i >= len(script) {
				continue
			}
			if script[i] == quote {
				current.WriteByte(script[i])
				i++
				continue
			}
			quote = 0
			escapeString = false
			continue
		}
		if openTag != "" {
			if strings.HasPrefix(script[i:], openTag) {
				current.WriteString(openTag)
				i += len(openTag)
				openTag = ""
				continue
			}
			current.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(script) && script[i:i+2] == "--" {
			lineComment = true
			current.WriteString("--")
			i += 2
			continue
		}
		if i+1 < len(script) && script[i:i+2] == "/*" {
			blockCommentDepth = 1
			current.WriteString("/*")
			i += 2
			continue
		}
		if c == '\'' {
			quote = c
			escapeString = isEscapeStringPrefix(script, i)
			current.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			quote = c
			current.WriteByte(c)
			i++
			continue
		}
		if c == '$' && (i == 0 || !isSQLIdentifierByte(script[i-1])) {
			if tag, width := dollarTagAt(script, i); width > 0 {
				openTag = tag
				current.WriteString(script[i : i+width])
				i += width
				continue
			}
		}
		if c == ';' {
			flush()
			i++
			continue
		}
		current.WriteByte(c)
		i++
	}
	flush()
	return statements
}

func ensureSchemaMigrationsTable(db *gorm.DB) error {
	// PostgreSQL's CREATE TABLE IF NOT EXISTS can still race while two fresh
	// processes concurrently create the table's implicit row type. Serialize
	// first boot before touching the table, using a transaction-scoped lock so
	// a failed process cannot strand the lock.
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"SELECT pg_advisory_xact_lock(?)",
			migrationAdvisoryLockKey,
		).Error; err != nil {
			return fmt.Errorf("lock schema migration bootstrap: %w", err)
		}
		if err := tx.Exec(fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now(), checksum TEXT)`,
			schemaMigrationsTable)).Error; err != nil {
			return err
		}
		return tx.Exec(fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN IF NOT EXISTS checksum TEXT`, schemaMigrationsTable,
		)).Error
	})
}

func appliedMigrationRecords(db *gorm.DB) (map[string]bool, map[string]string, error) {
	var rows []struct {
		Version  string
		Checksum sql.NullString
	}
	if err := db.Raw(fmt.Sprintf("SELECT version, checksum FROM %s", schemaMigrationsTable)).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	applied := make(map[string]bool, len(rows))
	checksums := make(map[string]string, len(rows))
	for _, row := range rows {
		applied[row.Version] = true
		if row.Checksum.Valid {
			checksums[row.Version] = row.Checksum.String
		}
	}
	return applied, checksums, nil
}

func appliedVersions(db *gorm.DB) (map[string]bool, error) {
	applied, _, err := appliedMigrationRecords(db)
	return applied, err
}

func validateAppliedChecksums(all []Migration, applied map[string]bool, checksums map[string]string) error {
	for _, migration := range all {
		if !applied[migration.Version] {
			continue
		}
		stored := checksums[migration.Version]
		if stored == "" {
			return fmt.Errorf("applied migration %q has no content checksum; verify the database schema and explicitly adopt its checksum before continuing", migration.Version)
		}
		want := MigrationChecksum(migration)
		if stored != want {
			return fmt.Errorf("applied migration %q checksum mismatch: database=%s source=%s; refusing to continue", migration.Version, stored, want)
		}
	}
	return nil
}

func validateAppliedMigrationSources(db *gorm.DB, fsys fs.FS, currentDir string) error {
	dirs := []string{currentDir}
	seen := map[string]bool{currentDir: true}
	for _, phase := range []string{"pre", "post"} {
		if !seen[phase] {
			dirs = append(dirs, phase)
			seen[phase] = true
		}
	}
	applied, checksums, err := appliedMigrationRecords(db)
	if err != nil {
		return fmt.Errorf("load migration checksums: %w", err)
	}
	for _, dir := range dirs {
		all, err := loadMigrations(fsys, dir)
		if errors.Is(err, fs.ErrNotExist) && dir != currentDir {
			continue
		}
		if err != nil {
			return err
		}
		if err := validateAppliedVersions(all, applied, dir); err != nil {
			return err
		}
		if err := validateAppliedChecksums(all, applied, checksums); err != nil {
			return err
		}
	}
	return nil
}

// AdoptLegacyMigrationChecksums explicitly records checksums for legacy rows
// that predate checksum tracking. The caller must first independently verify
// that the database schema matches the reviewed migration sources. approved
// must contain the exact current checksum for every legacy row in this phase;
// no row is adopted implicitly during startup.
func AdoptLegacyMigrationChecksums(db *gorm.DB, fsys fs.FS, dir string, approved map[string]string) error {
	all, err := loadMigrations(fsys, dir)
	if err != nil {
		return err
	}
	exists, err := schemaMigrationsTableExists(db)
	if err != nil {
		return fmt.Errorf("check schema migration ledger: %w", err)
	}
	if !exists {
		return fmt.Errorf("schema migration ledger does not exist; refusing legacy checksum adoption")
	}
	if err := ensureSchemaMigrationsTable(db); err != nil {
		return fmt.Errorf("prepare checksum ledger: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLockKey).Error; err != nil {
			return fmt.Errorf("lock schema migrations: %w", err)
		}
		applied, checksums, err := appliedMigrationRecords(tx)
		if err != nil {
			return fmt.Errorf("load migration checksums: %w", err)
		}
		if err := validateAppliedVersions(all, applied, dir); err != nil {
			return err
		}
		for _, migration := range all {
			if applied[migration.Version] && checksums[migration.Version] != "" && checksums[migration.Version] != MigrationChecksum(migration) {
				return fmt.Errorf("applied migration %q checksum mismatch; refusing legacy adoption", migration.Version)
			}
		}
		legacy := make(map[string]Migration)
		for _, migration := range all {
			if applied[migration.Version] && checksums[migration.Version] == "" {
				legacy[migration.Version] = migration
			}
		}
		if len(legacy) == 0 {
			return fmt.Errorf("no legacy checksum rows found for %s; refusing adoption", dir)
		}
		if len(approved) != len(legacy) {
			return fmt.Errorf("approved checksum set must contain exactly %d legacy %s migration rows", len(legacy), dir)
		}
		for version, migration := range legacy {
			want := MigrationChecksum(migration)
			if approved[version] != want {
				return fmt.Errorf("approved checksum for %q does not match current migration source; refusing adoption", version)
			}
		}
		for version := range approved {
			if _, ok := legacy[version]; !ok {
				return fmt.Errorf("approved checksum set contains non-legacy or unknown migration %q", version)
			}
		}
		for version, migration := range legacy {
			result := tx.Exec(fmt.Sprintf("UPDATE %s SET checksum = ? WHERE version = ? AND checksum IS NULL", schemaMigrationsTable), MigrationChecksum(migration), version)
			if result.Error != nil {
				return fmt.Errorf("adopt checksum for %q: %w", version, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("legacy checksum row %q changed during adoption; refusing to continue", version)
			}
		}
		return nil
	})
}

func schemaMigrationsTableExists(db *gorm.DB) (bool, error) {
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", schemaMigrationsTable).Row().Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func schemaMigrationsChecksumColumnExists(db *gorm.DB) (bool, error) {
	var exists bool
	err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_attribute
		WHERE attrelid = to_regclass(?) AND attname = 'checksum' AND NOT attisdropped
	)`, schemaMigrationsTable).Row().Scan(&exists)
	return exists, err
}

// ApplyMigrations applies all pending up migrations in dir and returns the count
// applied. Each migration runs inside its own transaction and is recorded on
// success, so a failure leaves earlier migrations committed and this one absent.
func ApplyMigrations(db *gorm.DB, fsys fs.FS, dir string) (int, error) {
	if err := ensureSchemaMigrationsTable(db); err != nil {
		return 0, fmt.Errorf("ensure schema_migrations: %w", err)
	}
	all, err := loadMigrations(fsys, dir)
	if err != nil {
		return 0, err
	}
	if err := validateAppliedMigrationSources(db, fsys, dir); err != nil {
		return 0, err
	}
	count := 0
	for {
		applied, checksums, err := appliedMigrationRecords(db)
		if err != nil {
			return count, fmt.Errorf("load applied versions: %w", err)
		}
		if err := validateAppliedVersions(all, applied, dir); err != nil {
			return count, err
		}
		if err := validateAppliedChecksums(all, applied, checksums); err != nil {
			return count, err
		}
		pending := pendingMigrations(all, applied)
		if len(pending) == 0 {
			// A rollback may have completed after this pass took its snapshot.
			// Acquire the shared lock and verify a stable empty ledger before
			// returning, otherwise repeat and restore any concurrently rolled-back
			// migration.
			stable := false
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLockKey).Error; err != nil {
					return fmt.Errorf("lock schema migrations: %w", err)
				}
				lockedApplied, lockedChecksums, err := appliedMigrationRecords(tx)
				if err != nil {
					return fmt.Errorf("recheck applied versions: %w", err)
				}
				if err := validateAppliedVersions(all, lockedApplied, dir); err != nil {
					return err
				}
				if err := validateAppliedChecksums(all, lockedApplied, lockedChecksums); err != nil {
					return err
				}
				stable = len(pendingMigrations(all, lockedApplied)) == 0
				return nil
			})
			if err != nil {
				return count, err
			}
			if stable {
				return count, nil
			}
			continue
		}
		for _, migration := range pending {
			appliedNow := false
			if err := db.Transaction(func(tx *gorm.DB) error {
				// Serialize each migration across backend instances, then recheck
				// under the lock. The initial applied-version snapshot is only an
				// optimization and must never be the concurrency authority.
				if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLockKey).Error; err != nil {
					return fmt.Errorf("lock schema migrations: %w", err)
				}
				var stored sql.NullString
				row := tx.Raw(fmt.Sprintf("SELECT checksum FROM %s WHERE version = ?", schemaMigrationsTable), migration.Version).Row()
				scanErr := row.Scan(&stored)
				if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
					return fmt.Errorf("recheck migration %s: %w", migration.Version, scanErr)
				}
				if scanErr == nil && stored.Valid {
					if stored.String != MigrationChecksum(migration) {
						return fmt.Errorf("applied migration %q checksum mismatch under lock; refusing to continue", migration.Version)
					}
					return nil
				}
				if scanErr == nil {
					return fmt.Errorf("applied migration %q has no content checksum; refusing to continue", migration.Version)
				}
				for _, statement := range splitSQLStatements(migration.UpSQL) {
					if err := tx.Exec(statement).Error; err != nil {
						return fmt.Errorf("apply %s: %w", migration.Version, err)
					}
				}
				if err := tx.Exec(
					fmt.Sprintf("INSERT INTO %s (version, checksum) VALUES (?, ?)", schemaMigrationsTable), migration.Version, MigrationChecksum(migration),
				).Error; err != nil {
					return err
				}
				appliedNow = true
				return nil
			}); err != nil {
				return count, err
			}
			if appliedNow {
				count++
			}
		}
	}
}

// RollbackMigration reverses the latest applied migration in a phase by running
// its down SQL and removing its schema_migrations row in one transaction.
// Refusing out-of-order rollback keeps the migration ledger aligned with the
// schema when later migrations depend on earlier tables or columns.
func RollbackMigration(db *gorm.DB, fsys fs.FS, dir, version string) error {
	all, err := loadMigrations(fsys, dir)
	if err != nil {
		return err
	}
	var target *Migration
	targetIndex := -1
	for i := range all {
		if all[i].Version == version {
			target = &all[i]
			targetIndex = i
			break
		}
	}
	if target == nil {
		return fmt.Errorf("migration %q not found in %q", version, dir)
	}
	if strings.TrimSpace(target.DownSQL) == "" {
		return fmt.Errorf("migration %q has no down file; refusing to rollback", version)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLockKey).Error; err != nil {
			return fmt.Errorf("lock schema migrations: %w", err)
		}
		exists, err := schemaMigrationsTableExists(tx)
		if err != nil {
			return fmt.Errorf("check schema migration ledger: %w", err)
		}
		if !exists {
			return fmt.Errorf("migration %q is not applied; refusing to rollback", version)
		}
		checksumColumn, err := schemaMigrationsChecksumColumnExists(tx)
		if err != nil {
			return fmt.Errorf("check migration checksum column: %w", err)
		}
		if !checksumColumn {
			return fmt.Errorf("legacy schema migration ledger has no checksum column; refusing rollback until the controlled checksum adoption procedure is completed")
		}
		applied, checksums, err := appliedMigrationRecords(tx)
		if err != nil {
			return fmt.Errorf("load applied versions: %w", err)
		}
		if err := validateAppliedVersions(all, applied, dir); err != nil {
			return err
		}
		if err := validateAppliedChecksums(all, applied, checksums); err != nil {
			return err
		}
		if !applied[version] {
			return fmt.Errorf("migration %q is not applied; refusing to rollback", version)
		}
		if err := validateNoLaterPhaseApplied(fsys, dir, applied, checksums); err != nil {
			return err
		}
		for i := len(all) - 1; i > targetIndex; i-- {
			if applied[all[i].Version] {
				return fmt.Errorf(
					"migration %q cannot be rolled back while later migration %q is applied; rollback later migrations first",
					version,
					all[i].Version,
				)
			}
		}
		for _, statement := range splitSQLStatements(target.DownSQL) {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("rollback %s: %w", version, err)
			}
		}
		return tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE version = ?", schemaMigrationsTable), version).Error
	})
}

// validateNoLaterPhaseApplied prevents rolling back the pre phase while any
// post-phase migration remains applied. Post migrations run after and may
// depend on the pre-phase schema, so per-phase ordering alone is insufficient.
func validateNoLaterPhaseApplied(fsys fs.FS, dir string, applied map[string]bool, checksumSets ...map[string]string) error {
	if dir != "pre" {
		return nil
	}
	later, err := loadMigrations(fsys, "post")
	if err != nil {
		return fmt.Errorf("cannot verify post-phase migrations; refusing pre-phase rollback: %w", err)
	}
	if err := validateAppliedVersions(later, applied, "post"); err != nil {
		return err
	}
	if len(checksumSets) > 0 {
		if err := validateAppliedChecksums(later, applied, checksumSets[0]); err != nil {
			return err
		}
	}
	for _, migration := range later {
		if applied[migration.Version] {
			return fmt.Errorf(
				"pre-phase migration cannot be rolled back while later-phase migration %q is applied; roll back post-phase migrations first",
				migration.Version,
			)
		}
	}
	return nil
}

// MigrationStatus reports, per phase, which versions are applied and which are
// pending. Used by the `migrate status` subcommand.
type MigrationStatus struct {
	Dir     string
	Applied []string
	Pending []string
}

// Status returns the applied/pending split for a directory without changing it.
func Status(db *gorm.DB, fsys fs.FS, dir string) (MigrationStatus, error) {
	status := MigrationStatus{Dir: dir}
	all, err := loadMigrations(fsys, dir)
	if err != nil {
		return status, err
	}
	exists, err := schemaMigrationsTableExists(db)
	if err != nil {
		return status, fmt.Errorf("check schema migration ledger: %w", err)
	}
	if !exists {
		for _, migration := range all {
			status.Pending = append(status.Pending, migration.Version)
		}
		return status, nil
	}
	checksumColumn, err := schemaMigrationsChecksumColumnExists(db)
	if err != nil {
		return status, fmt.Errorf("check migration checksum column: %w", err)
	}
	if !checksumColumn {
		return status, fmt.Errorf("legacy schema migration ledger has no checksum column; run the controlled checksum adoption procedure before relying on migration status")
	}
	applied, checksums, err := appliedMigrationRecords(db)
	if err != nil {
		return status, err
	}
	if err := validateAppliedVersions(all, applied, dir); err != nil {
		return status, err
	}
	if err := validateAppliedChecksums(all, applied, checksums); err != nil {
		return status, err
	}
	for _, migration := range all {
		if applied[migration.Version] {
			status.Applied = append(status.Applied, migration.Version)
		} else {
			status.Pending = append(status.Pending, migration.Version)
		}
	}
	return status, nil
}

package infra

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

func TestLegacyCursorChecksumCannotAuthorizeChangedMigrationSource(t *testing.T) {
	all, err := loadMigrations(migrations.Files, "pre")
	if err != nil {
		t.Fatal(err)
	}
	var original Migration
	for _, migration := range all {
		if migration.Version == "pre/0128_account_feed_sync_cursor" {
			original = migration
			break
		}
	}
	if original.Version == "" {
		t.Fatal("cursor migration missing")
	}
	for name, changed := range map[string]Migration{
		"changed up":    {Version: original.Version, UpSQL: original.UpSQL + "\nSELECT 1;", DownSQL: original.DownSQL},
		"changed down":  {Version: original.Version, UpSQL: original.UpSQL, DownSQL: original.DownSQL + "\nSELECT 1;"},
		"empty scripts": {Version: original.Version},
		"other version": {Version: "pre/0127_unrelated", UpSQL: original.UpSQL, DownSQL: original.DownSQL},
		"mixed checkout encodings": {Version: original.Version,
			UpSQL:   strings.ReplaceAll(original.UpSQL, "\r\n", "\n"),
			DownSQL: strings.ReplaceAll(strings.ReplaceAll(original.DownSQL, "\r\n", "\n"), "\n", "\r\n")},
	} {
		t.Run(name, func(t *testing.T) {
			if migrationChecksumMatches(changed, previousAccountFeedSyncCursorChecksum) {
				t.Fatal("historical cursor checksum authorized unreviewed source")
			}
			err := validateAppliedChecksums([]Migration{changed}, map[string]bool{changed.Version: true}, map[string]string{changed.Version: previousAccountFeedSyncCursorChecksum})
			if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
				t.Fatalf("expected source mismatch refusal, got %v", err)
			}
		})
	}
	for _, newline := range []string{"\n", "\r\n"} {
		candidate := original
		candidate.UpSQL = strings.ReplaceAll(strings.ReplaceAll(original.UpSQL, "\r\n", "\n"), "\n", newline)
		candidate.DownSQL = strings.ReplaceAll(strings.ReplaceAll(original.DownSQL, "\r\n", "\n"), "\n", newline)
		if !migrationChecksumMatches(candidate, previousAccountFeedSyncCursorChecksum) {
			t.Fatal("reviewed cursor repair refused for supported checkout line endings")
		}
		if !migrationChecksumMatches(candidate, MigrationChecksum(candidate)) {
			t.Fatal("exact current source checksum refused")
		}
	}
}

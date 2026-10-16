package migrations

import (
	"strings"
	"testing"
)

func TestAccountFeedCursorRollbackLocksBeforeCheckingAndDropping(t *testing.T) {
	down, err := Files.ReadFile("pre/0128_account_feed_sync_cursor.down.sql")
	if err != nil {
		t.Fatalf("read account-feed cursor rollback: %v", err)
	}
	sql := strings.ToUpper(string(down))
	lock := strings.Index(sql, "LOCK TABLE PUBLIC.ACCOUNT_FEEDS IN ACCESS EXCLUSIVE MODE")
	check := strings.Index(sql, "IF EXISTS")
	drop := strings.Index(sql, "ALTER TABLE PUBLIC.ACCOUNT_FEEDS")
	if lock < 0 || check < 0 || drop < 0 || !(lock < check && check < drop) {
		t.Fatalf("rollback order lock=%d check=%d drop=%d; want lock before presence check and drop", lock, check, drop)
	}
}

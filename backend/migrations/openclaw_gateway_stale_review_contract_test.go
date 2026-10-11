package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawGatewayStaleReviewMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0070_openclaw_gateway_session_review.up.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway stale-review migration: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"add column if not exists review_reason text not null default ''",
		"add column if not exists review_at timestamptz",
		"status in ('admitted', 'needs_review', 'terminal')",
		"where status = 'needs_review'",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("OpenClaw Gateway stale-review migration does not contain %q", want)
		}
	}

	downBytes, err := Files.ReadFile("pre/0070_openclaw_gateway_session_review.down.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway stale-review rollback: %v", err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"status = 'needs_review'",
		"raise exception",
		"drop column if exists review_reason",
		"drop column if exists review_at",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("OpenClaw Gateway stale-review rollback does not preserve review evidence: missing %q", want)
		}
	}
}

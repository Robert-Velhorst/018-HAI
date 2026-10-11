package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawGatewayArtifactReceiptMigrationIsMetadataOnlyAndRollbackSafe(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0071_openclaw_gateway_artifact_receipts.up.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway artifact receipt migration: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"create table if not exists openclaw_gateway_artifact_receipts",
		"execution_reference character varying(64) not null",
		"artifact_digest character varying(64) not null",
		"artifact_type character varying(120) not null",
		"mime_type character varying(160) not null default ''",
		"size_bytes bigint not null",
		"unique (execution_reference, artifact_digest)",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("OpenClaw Gateway artifact migration does not contain %q", want)
		}
	}
	for _, forbidden := range []string{"artifact_id", "artifact_title", "download_url", "artifact_content", "session_key", "run_id"} {
		if strings.Contains(up, forbidden) {
			t.Fatalf("OpenClaw Gateway artifact migration must not retain %q", forbidden)
		}
	}

	downBytes, err := Files.ReadFile("pre/0071_openclaw_gateway_artifact_receipts.down.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway artifact receipt rollback: %v", err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{"select 1 from openclaw_gateway_artifact_receipts limit 1", "raise exception", "drop table if exists openclaw_gateway_artifact_receipts"} {
		if !strings.Contains(down, want) {
			t.Fatalf("OpenClaw Gateway artifact rollback is unsafe: missing %q", want)
		}
	}
}

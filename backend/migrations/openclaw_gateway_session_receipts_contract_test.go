package migrations

import (
	"strings"
	"testing"
)

func TestOpenClawGatewaySessionReceiptMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0069_openclaw_gateway_session_receipts.up.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway receipt migration: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"create table if not exists openclaw_gateway_session_receipts",
		"execution_reference character varying(64) not null unique",
		"session_key text not null",
		"run_id character varying(256) not null",
		"check (status in ('admitted', 'terminal'))",
		"check (terminal_status in ('', 'completed', 'failed'))",
		"where status = 'admitted' and reconciled_at is null",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("OpenClaw Gateway receipt migration does not contain %q", want)
		}
	}
	if strings.Contains(up, "cascade") {
		t.Fatal("OpenClaw Gateway receipt migration must not cascade-delete private reconciliation records")
	}

	downBytes, err := Files.ReadFile("pre/0069_openclaw_gateway_session_receipts.down.sql")
	if err != nil {
		t.Fatalf("read OpenClaw Gateway receipt rollback: %v", err)
	}
	if strings.Contains(strings.ToLower(string(downBytes)), "cascade") {
		t.Fatal("OpenClaw Gateway receipt rollback must not use CASCADE")
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"select 1 from openclaw_gateway_session_receipts limit 1",
		"raise exception",
		"drop table if exists openclaw_gateway_session_receipts",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("OpenClaw Gateway receipt rollback does not preserve populated ledger safety: missing %q", want)
		}
	}
}

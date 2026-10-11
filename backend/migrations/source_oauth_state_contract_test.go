package migrations

import (
	"strings"
	"testing"
)

func TestSourceOAuthStateMigrationIsOwnerBoundSingleUseAndBounded(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0065_source_oauth_state.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := Files.ReadFile("pre/0065_source_oauth_state.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))

	for _, required := range []string{
		"create table if not exists public.source_o_auth_states",
		"references public.connected_sources(id) on delete cascade",
		"owner_identity character varying(255) not null",
		"state_digest character(64) not null",
		"consumed_at timestamp with time zone",
		"ux_source_oauth_states_source",
		"ux_source_oauth_states_digest",
		"idx_source_oauth_states_owner_expiry",
		"state_digest ~ '^[0-9a-f]{64}$'",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("up migration missing %q", required)
		}
	}
	if !strings.Contains(down, "refusing to discard active oauth authorization attempts") {
		t.Fatal("down migration must fail closed while a live OAuth attempt exists")
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("down migration must not use CASCADE")
	}
}

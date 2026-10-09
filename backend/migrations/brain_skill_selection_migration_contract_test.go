package migrations

import (
	"strings"
	"testing"
)

func TestBrainSkillSelectionMigrationPinsAppendOnlyOwnerDecisions(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0084_brain_skill_selection_events.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	for _, want := range []string{
		"create table public.brain_skill_selection_events",
		"owner_identity varchar(255) not null",
		"skill_id varchar(128) not null",
		"source_commit varchar(40) not null",
		"source_sha256 varchar(64) not null",
		"guidance_sha256 varchar(64) not null",
		"actor_identity varchar(255) not null",
		"enabled boolean not null",
		"decided_at timestamptz not null",
		"idx_brain_skill_selection_owner_latest",
		"(owner_identity, skill_id, id desc)",
		"before update or delete",
		"before truncate",
		"raise exception 'brain skill selection decisions are append-only'",
		"using errcode = '55000'",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("selection migration is missing %q", want)
		}
	}
	for _, forbidden := range []string{"tool_permissions", "action_permissions", "execute_script", "raw_guidance"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("selection migration must not grant runtime authority: found %q", forbidden)
		}
	}
}

func TestBrainSkillSelectionRollbackRefusesToDiscardAudit(t *testing.T) {
	downBytes, err := Files.ReadFile("pre/0084_brain_skill_selection_events.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.ToLower(string(downBytes))
	for _, want := range []string{
		"lock table public.brain_skill_selection_events in access exclusive mode",
		"if exists (select 1 from public.brain_skill_selection_events limit 1)",
		"raise exception 'rollback refused: brain skill selection audit records would be discarded'",
		"drop table public.brain_skill_selection_events",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("selection rollback is missing %q", want)
		}
	}
	if strings.Index(down, "raise exception") > strings.Index(down, "drop table public.brain_skill_selection_events") {
		t.Fatal("rollback drops the selection event table before guarding its audit records")
	}
	if strings.Contains(down, " cascade") {
		t.Fatal("selection rollback must not use CASCADE")
	}
}

package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

const operationSourceIdentityMigration = "0111_operation_source_identity"

// These assertions inspect embedded SQL only; they do not execute PostgreSQL.
func operationSourceIdentitySQL(t *testing.T, direction string) string {
	t.Helper()
	data, err := Files.ReadFile("pre/" + operationSourceIdentityMigration + "." + direction + ".sql")
	if err != nil {
		t.Fatalf("read operation source identity %s migration: %v", direction, err)
	}
	var lines []string
	for _, line := range strings.Split(normalizeMigrationLineEndings(string(data)), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.Join(strings.Fields(strings.ToLower(strings.Join(lines, "\n"))), " ")
}

func TestOperationSourceIdentityMigrationEmbeddedPairAndOrder(t *testing.T) {
	entries, err := fs.ReadDir(Files, "pre")
	if err != nil {
		t.Fatal(err)
	}
	var upgrades []string
	var pair []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "0111_") {
			pair = append(pair, entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upgrades = append(upgrades, entry.Name())
		}
	}
	wantPair := operationSourceIdentityMigration + ".down.sql\n" + operationSourceIdentityMigration + ".up.sql"
	if strings.Join(pair, "\n") != wantPair {
		t.Fatalf("migration 0111 is not uniquely paired: %v", pair)
	}
	found := false
	for i, name := range upgrades {
		if name == operationSourceIdentityMigration+".up.sql" {
			found = true
			if i == 0 || upgrades[i-1] != "0110_account_feed_registry.up.sql" {
				t.Fatalf("operation source identity migration has wrong predecessor: %v", upgrades)
			}
		}
	}
	if !found || operationSourceIdentitySQL(t, "up") == "" || operationSourceIdentitySQL(t, "down") == "" {
		t.Fatal("operation source identity migration pair must be embedded and nonempty")
	}
	baseline, err := Files.ReadFile("pre/0002_baseline.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(baseline)), "create table if not exists public.operations") {
		t.Fatal("pre migration must follow the baseline operations table")
	}
}

func TestOperationSourceIdentityMigrationPreservesHistoricalRows(t *testing.T) {
	up := operationSourceIdentitySQL(t, "up")
	for _, field := range []string{"source_provider", "source_account", "source_external_id", "source_identity_hash"} {
		if !strings.Contains(up, "add column "+field+" text not null default ''") {
			t.Errorf("historical identity field %s must default to empty non-null text", field)
		}
	}
	if strings.Count(up, "add column ") != 4 {
		t.Error("identity foundation must add only the four owned identity columns")
	}
	for _, forbidden := range []string{
		"update public.operations", "insert into", "delete from", "truncate ",
		"drop ", "alter column", "source_uri", "create table", "source_head",
		"operation_execution_claims", "disable trigger", "security definer", "cascade",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("identity foundation must not backfill, infer identity, mutate claims, or destroy history: %q", forbidden)
		}
	}
}

func TestOperationSourceIdentityMigrationGroupedBoundsAndOpaqueRevision(t *testing.T) {
	up := operationSourceIdentitySQL(t, "up")
	wantCheck := `add constraint chk_operations_source_identity_group check (
		(
			source_provider = ''
			and source_account = ''
			and source_external_id = ''
			and source_identity_hash = ''
		)
		or (
			btrim(source_provider) <> ''
			and octet_length(source_provider) <= 64
			and btrim(source_account) <> ''
			and octet_length(source_account) <= 1024
			and btrim(source_external_id) <> ''
			and octet_length(source_external_id) <= 4096
			and source_identity_hash <> ''
			and octet_length(source_identity_hash) = 64
			and source_identity_hash ~ '^[0-9a-f]{64}$'
			and source_revision_hash is not null
			and btrim(source_revision_hash) <> ''
			and octet_length(source_revision_hash) <= 4096
		)
	);`
	if !strings.Contains(up, strings.Join(strings.Fields(wantCheck), " ")) {
		t.Fatal("identity CHECK must allow exactly an empty group or a complete byte-bounded tuple/hash with a non-null opaque revision")
	}
	for _, forbidden := range []string{
		"source_revision_hash ~", "source_revision_hash similar to", "source_revision_hash text not null",
		"octet_length(source_revision_hash) = 64", "char_length(source_provider)",
		"char_length(source_account)", "char_length(source_external_id)", "char_length(source_revision_hash)",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("revision must stay opaque and legacy-nullable; bounds must measure bytes: %q", forbidden)
		}
	}
}

func TestOperationSourceIdentityMigrationNonUniqueScopedLookup(t *testing.T) {
	up := operationSourceIdentitySQL(t, "up")
	want := "create index idx_operations_owner_workspace_source_identity on public.operations (owner_user_id, workspace_id, source_identity_hash) where source_identity_hash <> '';"
	if !strings.Contains(up, want) {
		t.Fatal("identity lookup must be owner/workspace scoped and omit unidentified history")
	}
	if strings.Count(up, "create index ") != 1 || strings.Contains(up, "unique") {
		t.Fatal("source identity revisions must coexist; identity lookup must not enforce uniqueness")
	}
}

func TestOperationSourceIdentityMigrationImmutableUpdateContract(t *testing.T) {
	up := operationSourceIdentitySQL(t, "up")
	identityGuard := "if new.source_provider is distinct from old.source_provider or new.source_account is distinct from old.source_account or new.source_external_id is distinct from old.source_external_id or new.source_identity_hash is distinct from old.source_identity_hash then"
	revisionGuard := "if old.source_identity_hash <> '' and new.source_revision_hash is distinct from old.source_revision_hash then"
	for _, required := range []string{
		"create function public.hai_reject_operation_source_identity_mutation() returns trigger language plpgsql as $$ begin",
		identityGuard,
		"raise exception 'operation source identity is immutable; reconciliation requires a separate migration path' using errcode = 'integrity_constraint_violation'; end if;",
		revisionGuard,
		"raise exception 'identified operation source revision is immutable' using errcode = 'integrity_constraint_violation'; end if; return new; end; $$;",
		"create trigger trg_operations_source_identity_immutable before update on public.operations for each row execute function public.hai_reject_operation_source_identity_mutation();",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("identity immutability contract missing %q", required)
		}
	}
	if strings.Index(up, revisionGuard) <= strings.Index(up, identityGuard) {
		t.Error("all four identity fields must be frozen independently of the identified-only revision guard")
	}
	for _, forbidden := range []string{"old.source_identity_hash = ''", "before update of", "new.source_provider :=", "new.source_account :=", "new.source_external_id :=", "new.source_identity_hash :=", "new.source_revision_hash :="} {
		if strings.Contains(up, forbidden) {
			t.Errorf("ordinary updates must not bypass or rewrite immutable provenance: %q", forbidden)
		}
	}
}

func TestOperationSourceIdentityRollbackDropsOnlyOwnedSchema(t *testing.T) {
	down := operationSourceIdentitySQL(t, "down")
	want := `set local lock_timeout = '5s';
		lock table public.operations in access exclusive mode;
		do $$
		begin
			if exists (
				select 1 from public.operations
				where source_provider <> ''
					or source_account <> ''
					or source_external_id <> ''
					or source_identity_hash <> ''
			) then
				raise exception 'rollback refused: operations contain structured source identity'
					using errcode = '55000';
			end if;
		end;
		$$;
		drop trigger if exists trg_operations_source_identity_immutable on public.operations;
		drop function if exists public.hai_reject_operation_source_identity_mutation();
		drop index if exists public.idx_operations_owner_workspace_source_identity;
		alter table public.operations
			drop constraint if exists chk_operations_source_identity_group,
			drop column if exists source_provider,
			drop column if exists source_account,
			drop column if exists source_external_id,
			drop column if exists source_identity_hash;`
	if down != strings.Join(strings.Fields(want), " ") {
		t.Fatal("rollback must lock and refuse nonempty identity before dropping only the owned schema in dependency order")
	}
	raw, err := Files.ReadFile("pre/" + operationSourceIdentityMigration + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	warning := strings.ToLower(normalizeMigrationLineEndings(string(raw)))
	for _, required := range []string{
		"guarded operator rollback", "refuse removal while any operation carries even",
		"explicit operator approval and a verified backup/export", "stop operation writers",
		"empty-only rollback", "operations and operation events remain",
	} {
		if !strings.Contains(warning, required) {
			t.Errorf("guarded identity rollback warning missing %q", required)
		}
	}
}

func TestOperationSourceIdentityRollbackRefusesProvenanceBeforeAnyDrop(t *testing.T) {
	down := operationSourceIdentitySQL(t, "down")
	lock := strings.Index(down, "lock table public.operations in access exclusive mode;")
	preflight := strings.Index(down, "if exists ( select 1 from public.operations where source_provider <> '' or source_account <> '' or source_external_id <> '' or source_identity_hash <> '' ) then")
	refusal := strings.Index(down, "raise exception 'rollback refused: operations contain structured source identity'")
	endPreflight := strings.Index(down, "end; $$;")
	firstDrop := strings.Index(down, "drop ")
	if lock < 0 || preflight <= lock || refusal <= preflight || endPreflight <= refusal || firstDrop <= endPreflight {
		t.Fatalf("rollback must lock, check every identity field, refuse, and only then drop: lock=%d preflight=%d refusal=%d end=%d drop=%d", lock, preflight, refusal, endPreflight, firstDrop)
	}
	for _, forbidden := range []string{"delete from", "truncate ", "update public.operations", "cascade", "drop table"} {
		if strings.Contains(down, forbidden) {
			t.Errorf("guarded rollback must preserve operations and events: %q", forbidden)
		}
	}
}

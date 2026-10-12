package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

const sourceConfigurationMigration = "0114_operation_source_configuration"

// Embedded definition contracts only, not PostgreSQL execution or writer-path proof.
func sourceConfigurationReadSQL(t *testing.T, path string) string {
	t.Helper()
	data, err := Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read source configuration migration %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return sourceConfigurationNormalize(strings.Join(lines, "\n"))
}

func sourceConfigurationNormalize(sql string) string {
	flat := strings.Join(strings.Fields(strings.ToLower(sql)), " ")
	return strings.NewReplacer("( ", "(", " )", ")").Replace(flat)
}

func sourceConfigurationSQL(t *testing.T, direction string) string {
	t.Helper()
	return sourceConfigurationReadSQL(t, "pre/"+sourceConfigurationMigration+"."+direction+".sql")
}

func sourceConfigurationRequire(t *testing.T, sql string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(sql, sourceConfigurationNormalize(fragment)) {
			t.Errorf("source configuration contract missing %q", fragment)
		}
	}
}

func sourceConfigurationGuard(t *testing.T, sql, declaration string) string {
	t.Helper()
	start := strings.Index(sql, sourceConfigurationNormalize(declaration))
	if start < 0 {
		t.Fatalf("source origin guard declaration missing: %q", declaration)
	}
	end := strings.Index(sql[start:], "$$;")
	if end < 0 {
		t.Fatal("source origin guard definition is unterminated")
	}
	return sql[start : start+end+len("$$;")]
}

func TestSourceConfigurationMigrationEmbeddedPairAndPredecessor(t *testing.T) {
	entries, err := fs.ReadDir(Files, "pre")
	if err != nil {
		t.Fatal(err)
	}
	var pair, upgrades []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "0114_") {
			pair = append(pair, entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upgrades = append(upgrades, entry.Name())
		}
	}
	if strings.Join(pair, "\n") != sourceConfigurationMigration+".down.sql\n"+sourceConfigurationMigration+".up.sql" {
		t.Fatalf("migration 0114 must be uniquely paired: %v", pair)
	}
	found := false
	for i, name := range upgrades {
		if name == sourceConfigurationMigration+".up.sql" {
			found = true
			if i == 0 || upgrades[i-1] != "0113_operation_source_heads.up.sql" {
				t.Fatalf("managed source configuration requires source-head predecessor: %v", upgrades)
			}
		}
	}
	if !found || sourceConfigurationSQL(t, "up") == "" || sourceConfigurationSQL(t, "down") == "" {
		t.Fatal("source configuration migrations must be embedded and nonempty")
	}
}

func TestSourceConfigurationColumnsPreserveUnmanagedHistoricalOrigins(t *testing.T) {
	up := sourceConfigurationSQL(t, "up")
	sourceConfigurationRequire(t, up, `ALTER TABLE public.account_feeds
		ADD COLUMN config_version bigint NOT NULL DEFAULT 1,
		ADD CONSTRAINT chk_account_feeds_config_version CHECK (config_version > 0);`,
		`ALTER TABLE public.operation_source_origins
		ADD COLUMN registry_managed boolean NOT NULL DEFAULT false,
		ADD COLUMN enabled boolean NOT NULL DEFAULT true,
		ADD COLUMN registry_config_version bigint NOT NULL DEFAULT 0,
		ADD CONSTRAINT chk_source_origins_registry_config_version CHECK (
			registry_config_version >= 0 AND (NOT registry_managed OR registry_config_version > 0));`)
	if strings.Count(up, "add column ") != 4 || strings.Count(up, "alter table ") != 2 {
		t.Error("0114 must add only feed config_version and three origin authority columns without inferred enrollment")
	}
	want := sourceConfigurationNormalize(`CREATE FUNCTION public.hai_guard_account_feed_config_version()
		RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF TG_OP = 'DELETE' THEN
			PERFORM 1 FROM public.operation_source_origins
			WHERE owner_user_id = OLD.owner_user_id AND workspace_id = OLD.workspace_id
				AND origin_id = OLD.id AND registry_managed = true FOR UPDATE;
			IF FOUND THEN
				RAISE EXCEPTION 'registry-managed account feeds cannot be deleted' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
			RETURN OLD;
		END IF;
		IF NEW.id IS DISTINCT FROM OLD.id OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
			OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id THEN
			RAISE EXCEPTION 'account feed identity and scope are immutable' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.name IS DISTINCT FROM OLD.name OR NEW.provider IS DISTINCT FROM OLD.provider
			OR NEW.account_label IS DISTINCT FROM OLD.account_label OR NEW.source_type IS DISTINCT FROM OLD.source_type
			OR NEW.path IS DISTINCT FROM OLD.path OR NEW.url IS DISTINCT FROM OLD.url
			OR NEW.project_key IS DISTINCT FROM OLD.project_key OR NEW.operation_type IS DISTINCT FROM OLD.operation_type
			OR NEW.enabled IS DISTINCT FROM OLD.enabled THEN
			PERFORM 1 FROM public.operation_source_origins
			WHERE owner_user_id = OLD.owner_user_id AND workspace_id = OLD.workspace_id
				AND origin_id = OLD.id AND registry_managed = true FOR UPDATE;
			IF NEW.config_version IS DISTINCT FROM OLD.config_version + 1 THEN
				RAISE EXCEPTION 'changed account feed configuration must advance its version by one' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		ELSE
			IF NEW.config_version IS DISTINCT FROM OLD.config_version THEN
				RAISE EXCEPTION 'unchanged account feed configuration must retain its version' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		END IF;
		RETURN NEW;
	END; $$;`)
	if sourceConfigurationGuard(t, up, "CREATE FUNCTION public.hai_guard_account_feed_config_version()") != want {
		t.Error("feed guard must protect managed deletion, freeze scope, lock managed origin before config changes and increment exactly once, never for bookkeeping")
	}
}

func TestSourceConfigurationGuardRejectsDowngradeAndRequiresExactEpoch(t *testing.T) {
	up := sourceConfigurationSQL(t, "up")
	want := sourceConfigurationNormalize(`CREATE OR REPLACE FUNCTION public.hai_guard_source_origin_mutation()
		RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF TG_OP <> 'UPDATE' THEN
			RAISE EXCEPTION 'source origins cannot be deleted or truncated' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
			OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.origin_id IS DISTINCT FROM OLD.origin_id THEN
			RAISE EXCEPTION 'source origin scope is immutable' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF OLD.registry_managed AND NOT NEW.registry_managed THEN
			RAISE EXCEPTION 'registry-managed source authority cannot be downgraded' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF OLD.registry_managed THEN
			IF NEW.registry_config_version < OLD.registry_config_version THEN
				RAISE EXCEPTION 'registry-managed source configuration version cannot decrease' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
			IF NEW.registry_config_version = OLD.registry_config_version
				AND (NEW.config_digest IS DISTINCT FROM OLD.config_digest OR NEW.enabled IS DISTINCT FROM OLD.enabled) THEN
				RAISE EXCEPTION 'changed registry-managed source configuration must advance its version' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		END IF;
		IF NEW.config_digest IS NOT DISTINCT FROM OLD.config_digest
			AND NEW.registry_managed IS NOT DISTINCT FROM OLD.registry_managed
			AND NEW.enabled IS NOT DISTINCT FROM OLD.enabled
			AND NEW.registry_config_version IS NOT DISTINCT FROM OLD.registry_config_version THEN
			IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch THEN
				RAISE EXCEPTION 'unchanged source configuration must retain its epoch' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		ELSE
			IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch + 1 THEN
				RAISE EXCEPTION 'changed source configuration must advance its epoch by one' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		END IF;
		RETURN NEW;
	END; $$;`)
	got := sourceConfigurationGuard(t, up, "CREATE OR REPLACE FUNCTION public.hai_guard_source_origin_mutation()")
	if got != want {
		t.Error("guard must protect existing managed authority from downgrade/version regression or same-version changes, allow explicit unmanaged enrollment, and preserve exact epoch rules")
	}
}

func TestSourceConfigurationReusesExistingAllMutationTriggers(t *testing.T) {
	prior := sourceConfigurationReadSQL(t, "pre/0113_operation_source_heads.up.sql")
	sourceConfigurationRequire(t, prior,
		`CREATE TRIGGER trg_source_origins_guard BEFORE UPDATE OR DELETE ON public.operation_source_origins
			FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_origin_mutation();`,
		`CREATE TRIGGER trg_source_origins_no_truncate BEFORE TRUNCATE ON public.operation_source_origins
			FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_origin_mutation();`)
	up, down := sourceConfigurationSQL(t, "up"), sourceConfigurationSQL(t, "down")
	sourceConfigurationRequire(t, up, `CREATE TRIGGER trg_account_feeds_config_version
		BEFORE UPDATE OR DELETE ON public.account_feeds FOR EACH ROW
		EXECUTE FUNCTION public.hai_guard_account_feed_config_version();`,
		`CREATE TRIGGER trg_account_feeds_managed_no_truncate BEFORE TRUNCATE ON public.account_feeds
		FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_account_feed_managed_truncate();`,
		`CREATE CONSTRAINT TRIGGER trg_account_feeds_managed_configuration AFTER INSERT OR UPDATE ON public.account_feeds
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.hai_check_account_feed_managed_configuration();`,
		`CREATE CONSTRAINT TRIGGER trg_source_origins_managed_configuration AFTER INSERT OR UPDATE ON public.operation_source_origins
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.hai_check_account_feed_managed_configuration();`)
	wantTruncate := sourceConfigurationNormalize(`CREATE FUNCTION public.hai_guard_account_feed_managed_truncate()
		RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF EXISTS (SELECT 1 FROM public.account_feeds AS feed JOIN public.operation_source_origins AS origin
			ON origin.owner_user_id = feed.owner_user_id AND origin.workspace_id = feed.workspace_id AND origin.origin_id = feed.id
			WHERE origin.registry_managed = true) THEN
			RAISE EXCEPTION 'registry-managed account feeds cannot be truncated' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		RETURN NULL;
	END; $$;`)
	if sourceConfigurationGuard(t, up, "CREATE FUNCTION public.hai_guard_account_feed_managed_truncate()") != wantTruncate {
		t.Error("truncate guard must refuse any managed origin joined to current feed scope, including disabled feeds")
	}
	wantDeferred := sourceConfigurationNormalize(`CREATE FUNCTION public.hai_check_account_feed_managed_configuration()
		RETURNS trigger LANGUAGE plpgsql VOLATILE AS $$ DECLARE checked_origin_id uuid;
		canonical_version bigint; canonical_enabled boolean; canonical_found boolean;
		enrolling boolean := false; BEGIN
		IF TG_TABLE_NAME = 'operation_source_origins' THEN
			checked_origin_id := NEW.origin_id;
			IF NEW.registry_managed THEN
				IF TG_OP = 'INSERT' THEN enrolling := true;
				ELSIF TG_OP = 'UPDATE' THEN enrolling := NOT OLD.registry_managed;
				END IF;
			END IF;
		ELSIF TG_TABLE_NAME = 'account_feeds' THEN
			checked_origin_id := NEW.id;
		ELSE
			RAISE EXCEPTION 'unsupported registry-managed configuration trigger table' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF enrolling THEN
			UPDATE public.account_feeds AS feed SET config_version = feed.config_version
			WHERE feed.owner_user_id = NEW.owner_user_id AND feed.workspace_id = NEW.workspace_id AND feed.id = checked_origin_id
			RETURNING feed.config_version, feed.enabled INTO canonical_version, canonical_enabled;
		ELSE
			SELECT feed.config_version, feed.enabled INTO canonical_version, canonical_enabled FROM public.account_feeds AS feed
			WHERE feed.owner_user_id = NEW.owner_user_id AND feed.workspace_id = NEW.workspace_id AND feed.id = checked_origin_id FOR UPDATE;
		END IF;
		canonical_found := FOUND;
		IF EXISTS (SELECT 1 FROM public.operation_source_origins AS origin
			WHERE origin.origin_id = checked_origin_id AND origin.owner_user_id = NEW.owner_user_id AND origin.workspace_id = NEW.workspace_id
			AND origin.registry_managed = true
			AND (NOT canonical_found OR origin.registry_config_version IS DISTINCT FROM canonical_version OR origin.enabled IS DISTINCT FROM canonical_enabled)) THEN
			RAISE EXCEPTION 'registry-managed account feed configuration must match its canonical origin' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		RETURN NULL;
	END; $$;`)
	deferred := sourceConfigurationGuard(t, up, "CREATE FUNCTION public.hai_check_account_feed_managed_configuration()")
	if deferred != wantDeferred {
		t.Error("checker must fence first enrollment with a scoped no-op feed write, otherwise lock the canonical feed, then separately read current origin; refuse missing/wrong-scope feeds and mismatches without recursive writes")
	}
	lockEnd := strings.Index(deferred, "for update; end if; canonical_found := found;")
	checkStart := strings.Index(deferred, "if exists (select 1 from public.operation_source_origins as origin")
	if lockEnd < 0 || checkStart < lockEnd || strings.Count(deferred, "update public.account_feeds ") != 1 || strings.Contains(deferred, "lock table") {
		t.Error("check must follow canonical feed serialization, with exactly one enrollment-only write and no global table lock")
	}
	sourceConfigurationRequire(t, down,
		`DROP TRIGGER IF EXISTS trg_account_feeds_config_version ON public.account_feeds;`,
		`DROP TRIGGER IF EXISTS trg_account_feeds_managed_no_truncate ON public.account_feeds;`,
		`DROP TRIGGER IF EXISTS trg_account_feeds_managed_configuration ON public.account_feeds;`,
		`DROP TRIGGER IF EXISTS trg_source_origins_managed_configuration ON public.operation_source_origins;`,
		`DROP FUNCTION IF EXISTS public.hai_guard_account_feed_config_version();`,
		`DROP FUNCTION IF EXISTS public.hai_guard_account_feed_managed_truncate();`,
		`DROP FUNCTION IF EXISTS public.hai_check_account_feed_managed_configuration();`)
	if strings.Count(up, "create trigger ") != 2 || strings.Count(up, "create constraint trigger ") != 2 || strings.Contains(up, "drop trigger ") ||
		strings.Count(down, "drop trigger ") != 4 || strings.Count(down, "drop function ") != 3 ||
		strings.Contains(down, "create trigger ") || strings.Contains(down, "create constraint trigger ") ||
		strings.Contains(down, "drop function if exists public.hai_guard_source_origin_mutation") {
		t.Error("only four owned configuration triggers and three functions may be added or dropped; retain existing origin update/delete/truncate triggers")
	}
}

func TestSourceConfigurationRollbackLocksAndRefusesAllManagedRows(t *testing.T) {
	down := sourceConfigurationSQL(t, "down")
	lockSQL := "lock table public.account_feeds, public.operation_source_origins in access exclusive mode;"
	preflight := sourceConfigurationNormalize(`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM public.operation_source_origins WHERE registry_managed = true) THEN
			RAISE EXCEPTION 'rollback refused: registry-managed source configuration authority exists' USING ERRCODE = '55000';
		END IF;
	END; $$;`)
	lock, check := strings.Index(down, lockSQL), strings.Index(down, preflight)
	restore := strings.Index(down, "create or replace function ")
	firstDrop := strings.Index(down, "drop ")
	if lock < 0 || check <= lock || firstDrop < check+len(preflight) || restore <= firstDrop {
		t.Fatalf("rollback must lock feeds before origins, refuse all managed rows, then remove feed artifacts and restore origin guard: lock=%d check=%d restore=%d drop=%d", lock, check, restore, firstDrop)
	}
}

func TestSourceConfigurationRollbackRestoresExact0113GuardBeforeColumnRemoval(t *testing.T) {
	down := sourceConfigurationSQL(t, "down")
	prior := sourceConfigurationReadSQL(t, "pre/0113_operation_source_heads.up.sql")
	want := sourceConfigurationGuard(t, prior, "CREATE FUNCTION public.hai_guard_source_origin_mutation()")
	want = strings.Replace(want, "create function ", "create or replace function ", 1)
	got := sourceConfigurationGuard(t, down, "CREATE OR REPLACE FUNCTION public.hai_guard_source_origin_mutation()")
	if got != want {
		t.Fatal("rollback must restore the complete exact 0113 source-origin guard definition")
	}
	remove := sourceConfigurationNormalize(`ALTER TABLE public.operation_source_origins
		DROP CONSTRAINT IF EXISTS chk_source_origins_registry_config_version,
		DROP COLUMN IF EXISTS registry_config_version, DROP COLUMN IF EXISTS registry_managed, DROP COLUMN IF EXISTS enabled;`)
	start := strings.Index(down, remove)
	restore := strings.Index(down, got)
	feedRemoval := sourceConfigurationNormalize(`DROP TRIGGER IF EXISTS trg_account_feeds_config_version ON public.account_feeds;
		DROP TRIGGER IF EXISTS trg_account_feeds_managed_no_truncate ON public.account_feeds;
		DROP TRIGGER IF EXISTS trg_account_feeds_managed_configuration ON public.account_feeds;
		DROP TRIGGER IF EXISTS trg_source_origins_managed_configuration ON public.operation_source_origins;
		DROP FUNCTION IF EXISTS public.hai_guard_account_feed_config_version();
		DROP FUNCTION IF EXISTS public.hai_guard_account_feed_managed_truncate();
		DROP FUNCTION IF EXISTS public.hai_check_account_feed_managed_configuration();
		ALTER TABLE public.account_feeds DROP CONSTRAINT IF EXISTS chk_account_feeds_config_version,
			DROP COLUMN IF EXISTS config_version;`)
	firstDrop := strings.Index(down, "drop ")
	if start < restore+len(got) || down[start:] != remove || strings.Count(down, "drop column ") != 4 ||
		firstDrop < 0 || down[firstDrop:] != feedRemoval+" "+got+" "+remove {
		t.Fatal("rollback must drop owned feed/origin consistency triggers before their shared checker, restore exact 0113 guard, then remove only owned constraints/columns")
	}
}

func TestSourceConfigurationDDLDoesNotEnrollRewriteOrEraseHistory(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		sql := sourceConfigurationSQL(t, direction)
		sourceConfigurationRequire(t, sql, "SET LOCAL lock_timeout = '5s';")
		for _, forbidden := range []string{
			"insert into", "update public.", "delete from", "truncate table ", "truncate public.",
			"cascade", "security definer", "grant ", "disable trigger", "create table", "drop table",
			"alter column", "operation_source_heads", "operation_source_observations",
			"source_uri", "begin;", "commit;",
		} {
			scanned := sql
			if direction == "up" && forbidden == "update public." {
				// The exact checker contract above permits its enrollment-only MVCC
				// fence, not a migration-time update or mutation elsewhere.
				checker := sourceConfigurationGuard(t, sql, "CREATE FUNCTION public.hai_check_account_feed_managed_configuration()")
				scanned = strings.Replace(sql, checker, "", 1)
			}
			if strings.Contains(scanned, forbidden) {
				t.Errorf("%s must remain runner-transactional, scoped configuration DDL without enrollment or history changes: %q", direction, forbidden)
			}
		}
	}
	data, err := Files.ReadFile("pre/" + sourceConfigurationMigration + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"guarded operator rollback", "no data rows are deleted", "four owned columns", "transactional"} {
		if !strings.Contains(strings.ToLower(string(data)), required) {
			t.Errorf("rollback warning missing %q", required)
		}
	}
}

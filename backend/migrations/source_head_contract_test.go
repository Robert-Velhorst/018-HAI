package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

const sourceHeadMigration = "0113_operation_source_heads"

// Embedded source contracts only, not PostgreSQL acceptance or runtime fence proof.
func sourceHeadSQL(t *testing.T, direction string) string {
	t.Helper()
	data, err := Files.ReadFile("pre/" + sourceHeadMigration + "." + direction + ".sql")
	if err != nil {
		t.Fatalf("read source head %s migration: %v", direction, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return sourceHeadNormalize(strings.Join(lines, "\n"))
}

func sourceHeadNormalize(sql string) string {
	flat := strings.Join(strings.Fields(strings.ToLower(sql)), " ")
	return strings.NewReplacer("( ", "(", " )", ")").Replace(flat)
}

func sourceHeadRequire(t *testing.T, sql string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(sql, sourceHeadNormalize(fragment)) {
			t.Errorf("source head contract missing %q", fragment)
		}
	}
}

func TestSourceHeadMigrationEmbeddedPairAndPredecessor(t *testing.T) {
	entries, err := fs.ReadDir(Files, "pre")
	if err != nil {
		t.Fatal(err)
	}
	var pair, upgrades []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "0113_") {
			pair = append(pair, entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upgrades = append(upgrades, entry.Name())
		}
	}
	if strings.Join(pair, "\n") != sourceHeadMigration+".down.sql\n"+sourceHeadMigration+".up.sql" {
		t.Fatalf("migration 0113 must be uniquely paired: %v", pair)
	}
	found := false
	for i, name := range upgrades {
		if name == sourceHeadMigration+".up.sql" {
			found = true
			if i == 0 || upgrades[i-1] != "0112_operation_source_observation.up.sql" {
				t.Fatalf("source heads require observation predecessor: %v", upgrades)
			}
		}
	}
	if !found || sourceHeadSQL(t, "up") == "" || sourceHeadSQL(t, "down") == "" {
		t.Fatal("source head migrations must be embedded and nonempty")
	}
}

func TestSourceHeadOriginsAndHistoricalObservationEpoch(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up, `CREATE TABLE public.operation_source_origins (
		owner_user_id text NOT NULL, workspace_id text NOT NULL, origin_id uuid NOT NULL,
		config_digest text NOT NULL, config_epoch bigint NOT NULL,
		changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
		CONSTRAINT operation_source_origins_pkey PRIMARY KEY (owner_user_id, workspace_id, origin_id),
		CONSTRAINT chk_operation_source_origins_nonzero_identity CHECK (origin_id <> '00000000-0000-0000-0000-000000000000'::uuid),
		CONSTRAINT chk_operation_source_origins_config_digest CHECK (octet_length(config_digest) = 64 AND config_digest ~ '^[0-9a-f]{64}$'),
		CONSTRAINT chk_operation_source_origins_config_epoch CHECK (config_epoch > 0)
	);`, `ALTER TABLE public.operation_source_observations
		ADD COLUMN config_epoch bigint NOT NULL DEFAULT 0,
		ADD CONSTRAINT chk_operation_source_observations_config_epoch CHECK (config_epoch >= 0),
		ADD CONSTRAINT uq_source_head_observations_scope_id UNIQUE (owner_user_id, workspace_id, id),
		ADD CONSTRAINT uq_source_head_observations_scope_epoch UNIQUE (owner_user_id, workspace_id, id, generation, origin_id, config_epoch);`)
	if strings.Count(up, "add column ") != 1 || strings.Count(up, "create table ") != 3 {
		t.Error("source heads require exactly three new tables and one historical-zero observation epoch column")
	}
}

func TestSourceHeadSchemaScopedReferencesAndOpaqueRevision(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up, `CREATE TABLE public.operation_source_heads (
		owner_user_id text NOT NULL, workspace_id text NOT NULL, source_identity_hash text NOT NULL,
		origin_id uuid NOT NULL, operation_id uuid NOT NULL, revision_hash text NOT NULL,
		observation_id uuid NOT NULL, observation_generation bigint NOT NULL, config_epoch bigint NOT NULL,
		state text NOT NULL, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),`,
		`CONSTRAINT operation_source_heads_pkey PRIMARY KEY (owner_user_id, workspace_id, source_identity_hash)`,
		`CONSTRAINT chk_operation_source_heads_identity_hash CHECK (octet_length(source_identity_hash) = 64 AND source_identity_hash ~ '^[0-9a-f]{64}$')`,
		`CONSTRAINT chk_operation_source_heads_nonzero_identity CHECK (
			origin_id <> '00000000-0000-0000-0000-000000000000'::uuid
			AND operation_id <> '00000000-0000-0000-0000-000000000000'::uuid
			AND observation_id <> '00000000-0000-0000-0000-000000000000'::uuid)`,
		`CONSTRAINT chk_operation_source_heads_revision CHECK (btrim(revision_hash) <> '' AND octet_length(revision_hash) <= 4096)`,
		`CONSTRAINT chk_operation_source_heads_generation CHECK (observation_generation > 0)`,
		`CONSTRAINT chk_operation_source_heads_config_epoch CHECK (config_epoch > 0)`,
		`CONSTRAINT chk_operation_source_heads_state CHECK (state IN ('accepted', 'reconciliation_required', 'tombstoned'))`,
		`FOREIGN KEY (owner_user_id, workspace_id, origin_id)
			REFERENCES public.operation_source_origins (owner_user_id, workspace_id, origin_id) ON UPDATE RESTRICT ON DELETE RESTRICT`,
		`FOREIGN KEY (owner_user_id, workspace_id, operation_id)
			REFERENCES public.operations (owner_user_id, workspace_id, id) ON UPDATE RESTRICT ON DELETE RESTRICT`,
		`FOREIGN KEY (owner_user_id, workspace_id, observation_id, observation_generation, origin_id, config_epoch)
			REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id, config_epoch) ON UPDATE RESTRICT ON DELETE RESTRICT`)
	for _, prerequisite := range []struct{ path, key string }{
		{"pre/0016_evidence_packs.up.sql", "UNIQUE (owner_user_id, workspace_id, id)"},
		{"pre/0112_operation_source_observation.up.sql", "UNIQUE (owner_user_id, workspace_id, id, generation, origin_id)"},
	} {
		data, err := Files.ReadFile(prerequisite.path)
		if err != nil {
			t.Fatal(err)
		}
		sourceHeadRequire(t, sourceHeadNormalize(string(data)), prerequisite.key)
	}
	if strings.Contains(up, "alter table public.operations") || strings.Contains(up, "revision_hash ~") {
		t.Error("reuse the existing operation key and preserve opaque, non-hex original revisions")
	}
}

func TestSourceHeadObservationEpochBindingPreservesLegacyOperationKey(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up,
		`ADD CONSTRAINT uq_source_head_observations_scope_epoch
			UNIQUE (owner_user_id, workspace_id, id, generation, origin_id, config_epoch)`,
		`CONSTRAINT fk_operation_source_heads_observation_scope
			FOREIGN KEY (owner_user_id, workspace_id, observation_id, observation_generation, origin_id, config_epoch)
			REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id, config_epoch)
			ON UPDATE RESTRICT ON DELETE RESTRICT`)
	if strings.Contains(up, "foreign key (owner_user_id, workspace_id, observation_id, observation_generation, origin_id)") {
		t.Error("head observation FK must not omit configuration epoch")
	}
	prior, err := Files.ReadFile("pre/0112_operation_source_observation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sourceHeadRequire(t, sourceHeadNormalize(string(prior)),
		`CONSTRAINT uq_operation_source_observations_scope_id_generation_origin
			UNIQUE (owner_user_id, workspace_id, id, generation, origin_id)`,
		`CONSTRAINT fk_operations_source_observation_scope
			FOREIGN KEY (owner_user_id, workspace_id, source_observation_id, source_observation_generation, account_feed_id)
			REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id)`)
	if strings.Contains(up, "uq_operation_source_observations_scope_id_generation_origin") ||
		strings.Contains(sourceHeadSQL(t, "down"), "uq_operation_source_observations_scope_id_generation_origin") {
		t.Error("0113 must not replace or drop the existing five-field operation observation key")
	}
}

func TestSourceHeadRevisionHistoryScopedDigestAndImmutability(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up, `CREATE TABLE public.operation_source_head_revisions (
		owner_user_id text NOT NULL, workspace_id text NOT NULL, source_identity_hash text NOT NULL,
		revision_digest text NOT NULL, operation_id uuid NOT NULL, observation_id uuid NOT NULL,`,
		`PRIMARY KEY (owner_user_id, workspace_id, source_identity_hash, revision_digest)`,
		`CONSTRAINT chk_operation_source_head_revisions_identity_hash CHECK (octet_length(source_identity_hash) = 64 AND source_identity_hash ~ '^[0-9a-f]{64}$')`,
		`CONSTRAINT chk_operation_source_head_revisions_revision_digest CHECK (octet_length(revision_digest) = 64 AND revision_digest ~ '^[0-9a-f]{64}$')`,
		`CONSTRAINT chk_operation_source_head_revisions_nonzero_identity CHECK (
			operation_id <> '00000000-0000-0000-0000-000000000000'::uuid
			AND observation_id <> '00000000-0000-0000-0000-000000000000'::uuid)`,
		`CONSTRAINT fk_operation_source_head_revisions_operation_scope FOREIGN KEY (owner_user_id, workspace_id, operation_id)
			REFERENCES public.operations (owner_user_id, workspace_id, id) ON UPDATE RESTRICT ON DELETE RESTRICT`,
		`CONSTRAINT fk_operation_source_head_revisions_observation_scope FOREIGN KEY (owner_user_id, workspace_id, observation_id)
			REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id) ON UPDATE RESTRICT ON DELETE RESTRICT`,
		`CREATE TRIGGER trg_source_head_revisions_immutable BEFORE UPDATE OR DELETE ON public.operation_source_head_revisions
			FOR EACH ROW EXECUTE FUNCTION public.hai_reject_source_observation_mutation();`,
		`CREATE TRIGGER trg_source_head_revisions_no_truncate BEFORE TRUNCATE ON public.operation_source_head_revisions
			FOR EACH STATEMENT EXECUTE FUNCTION public.hai_reject_source_observation_mutation();`)
	prior, err := Files.ReadFile("pre/0112_operation_source_observation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sourceHeadRequire(t, sourceHeadNormalize(string(prior)), `CREATE FUNCTION public.hai_reject_source_observation_mutation()`,
		`CREATE TRIGGER trg_source_observations_immutable BEFORE UPDATE OR DELETE ON public.operation_source_observations`)
	if strings.Count(up, "foreign key (") != 5 || strings.Contains(up, "references public.operation_source_heads") {
		t.Error("require five scoped FKs without a circular revision-to-head dependency")
	}
}

func TestSourceHeadOriginEpochGuardAndHistoryRetention(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up, `CREATE FUNCTION public.hai_guard_source_origin_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF TG_OP <> 'UPDATE' THEN
			RAISE EXCEPTION 'source origins cannot be deleted or truncated' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.origin_id IS DISTINCT FROM OLD.origin_id THEN
			RAISE EXCEPTION 'source origin scope is immutable' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.config_digest IS NOT DISTINCT FROM OLD.config_digest THEN
			IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch THEN
				RAISE EXCEPTION 'unchanged source configuration must retain its epoch' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		ELSE
			IF NEW.config_epoch IS DISTINCT FROM OLD.config_epoch + 1 THEN
				RAISE EXCEPTION 'changed source configuration must advance its epoch by one' USING ERRCODE = 'integrity_constraint_violation';
			END IF;
		END IF;
		RETURN NEW;
	END; $$;`,
		`CREATE TRIGGER trg_source_origins_guard BEFORE UPDATE OR DELETE ON public.operation_source_origins FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_origin_mutation();`,
		`CREATE TRIGGER trg_source_origins_no_truncate BEFORE TRUNCATE ON public.operation_source_origins FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_origin_mutation();`)
}

func TestSourceHeadEveryUpdateRequiresNewerObservation(t *testing.T) {
	up := sourceHeadSQL(t, "up")
	sourceHeadRequire(t, up, `CREATE FUNCTION public.hai_guard_source_head_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF TG_OP <> 'UPDATE' THEN
			RAISE EXCEPTION 'source heads cannot be deleted or truncated' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
			OR NEW.source_identity_hash IS DISTINCT FROM OLD.source_identity_hash OR NEW.origin_id IS DISTINCT FROM OLD.origin_id THEN
			RAISE EXCEPTION 'source head tenant, identity and origin are immutable' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		IF NEW.observation_generation <= OLD.observation_generation OR NEW.config_epoch < OLD.config_epoch THEN
			RAISE EXCEPTION 'source head observation generation must increase and configuration epoch cannot decrease' USING ERRCODE = 'integrity_constraint_violation';
		END IF;
		RETURN NEW;
	END; $$;`,
		`CREATE TRIGGER trg_source_heads_guard BEFORE UPDATE OR DELETE ON public.operation_source_heads FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_head_mutation();`,
		`CREATE TRIGGER trg_source_heads_no_truncate BEFORE TRUNCATE ON public.operation_source_heads FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_head_mutation();`)
	if strings.Contains(up, "before update of") || strings.Contains(up, "old.state") || strings.Contains(up, "new.state") {
		t.Error("all updates, including tombstones and timestamp-only changes, must pass the same generation guard")
	}
}

func TestSourceHeadRollbackLocksAndRefusesHistoryBeforeAnyDrop(t *testing.T) {
	down := sourceHeadSQL(t, "down")
	lockSQL := sourceHeadNormalize(`LOCK TABLE public.operation_source_origins, public.operation_source_observations,
		public.operation_source_heads, public.operation_source_head_revisions IN ACCESS EXCLUSIVE MODE;`)
	preflight := sourceHeadNormalize(`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM public.operation_source_origins)
			OR EXISTS (SELECT 1 FROM public.operation_source_heads)
			OR EXISTS (SELECT 1 FROM public.operation_source_head_revisions)
			OR EXISTS (SELECT 1 FROM public.operation_source_observations WHERE config_epoch > 0) THEN
			RAISE EXCEPTION 'rollback refused: source origin, head, revision or configuration epoch history exists' USING ERRCODE = '55000';
		END IF;
	END; $$;`)
	lock, check, firstDrop := strings.Index(down, lockSQL), strings.Index(down, preflight), strings.Index(down, "drop ")
	if lock < 0 || check <= lock || firstDrop < check+len(preflight) {
		t.Fatalf("rollback must lock and refuse all 0113 history before any drop: lock=%d check=%d drop=%d", lock, check, firstDrop)
	}
	start := strings.Index(down, "drop ")
	want := sourceHeadNormalize(`
		DROP TRIGGER IF EXISTS trg_source_head_revisions_no_truncate ON public.operation_source_head_revisions;
		DROP TRIGGER IF EXISTS trg_source_head_revisions_immutable ON public.operation_source_head_revisions;
		DROP TABLE IF EXISTS public.operation_source_head_revisions;
		DROP TRIGGER IF EXISTS trg_source_heads_no_truncate ON public.operation_source_heads;
		DROP TRIGGER IF EXISTS trg_source_heads_guard ON public.operation_source_heads;
		DROP FUNCTION IF EXISTS public.hai_guard_source_head_mutation();
		DROP TABLE IF EXISTS public.operation_source_heads;
		DROP TRIGGER IF EXISTS trg_source_origins_no_truncate ON public.operation_source_origins;
		DROP TRIGGER IF EXISTS trg_source_origins_guard ON public.operation_source_origins;
		DROP FUNCTION IF EXISTS public.hai_guard_source_origin_mutation();
		DROP TABLE IF EXISTS public.operation_source_origins;
		ALTER TABLE public.operation_source_observations
			DROP CONSTRAINT IF EXISTS uq_source_head_observations_scope_epoch,
			DROP CONSTRAINT IF EXISTS uq_source_head_observations_scope_id,
			DROP CONSTRAINT IF EXISTS chk_operation_source_observations_config_epoch,
			DROP COLUMN IF EXISTS config_epoch;`)
	if start < 0 || down[start:] != want {
		t.Fatal("empty-only rollback must remove revisions before heads/keys and retain all pre-0113 artifacts")
	}
}

func TestSourceHeadMigrationNoBackfillPrivilegeExpansionOrHistoryErasure(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		sql := sourceHeadSQL(t, direction)
		sourceHeadRequire(t, sql, "SET LOCAL lock_timeout = '5s';")
		for _, forbidden := range []string{
			"insert into", "update public.", "delete from", "truncate table ", "truncate public.",
			"cascade", "security definer", "grant ", "disable trigger", "source_uri", "begin;", "commit;",
		} {
			if strings.Contains(sql, forbidden) {
				t.Errorf("%s must remain runner-transactional, source-only DDL without rewriting history or broadening grants: %q", direction, forbidden)
			}
		}
	}
	down := sourceHeadSQL(t, "down")
	for _, forbidden := range []string{
		"drop table if exists public.operations;", "drop table if exists public.operation_source_observations;",
		"uq_operations_owner_workspace_id", "hai_reject_source_observation_mutation", "source_observation_generation", "operation_events",
	} {
		if strings.Contains(down, forbidden) {
			t.Errorf("rollback must preserve earlier migration ownership and history: %q", forbidden)
		}
	}
	data, err := Files.ReadFile("pre/" + sourceHeadMigration + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"guarded operator rollback", "no data rows are deleted", "empty-only", "observation history remain", "transactional"} {
		if !strings.Contains(strings.ToLower(string(data)), required) {
			t.Errorf("rollback warning missing %q", required)
		}
	}
}

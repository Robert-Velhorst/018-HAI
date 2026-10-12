package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

const sourceObservationMigration = "0112_operation_source_observation"

// Source contracts only: these tests do not execute PostgreSQL or prove ordering.
func sourceObservationSQL(t *testing.T, direction string) string {
	t.Helper()
	data, err := Files.ReadFile("pre/" + sourceObservationMigration + "." + direction + ".sql")
	if err != nil {
		t.Fatalf("read source observation %s migration: %v", direction, err)
	}
	var lines []string
	for _, line := range strings.Split(normalizeMigrationLineEndings(string(data)), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return sourceObservationNormalize(strings.Join(lines, "\n"))
}

func sourceObservationNormalize(sql string) string {
	flat := strings.Join(strings.Fields(strings.ToLower(sql)), " ")
	return strings.NewReplacer("( ", "(", " )", ")").Replace(flat)
}

func sourceObservationRequire(t *testing.T, sql string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(sql, sourceObservationNormalize(fragment)) {
			t.Errorf("source observation contract missing %q", fragment)
		}
	}
}

func TestSourceObservationMigrationEmbeddedPairAndPredecessor(t *testing.T) {
	entries, err := fs.ReadDir(Files, "pre")
	if err != nil {
		t.Fatal(err)
	}
	var pair, upgrades []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "0112_") {
			pair = append(pair, entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upgrades = append(upgrades, entry.Name())
		}
	}
	wantPair := sourceObservationMigration + ".down.sql\n" + sourceObservationMigration + ".up.sql"
	if strings.Join(pair, "\n") != wantPair {
		t.Fatalf("migration 0112 must be uniquely paired: %v", pair)
	}
	found := false
	for i, name := range upgrades {
		if name == sourceObservationMigration+".up.sql" {
			found = true
			if i == 0 || upgrades[i-1] != "0111_operation_source_identity.up.sql" {
				t.Fatalf("observation provenance requires identity predecessor: %v", upgrades)
			}
		}
	}
	if !found || sourceObservationSQL(t, "up") == "" || sourceObservationSQL(t, "down") == "" {
		t.Fatal("source observation migrations must be embedded and nonempty")
	}
}

func TestSourceObservationMigrationTablesAndScopedKeys(t *testing.T) {
	up := sourceObservationSQL(t, "up")
	sourceObservationRequire(t, up,
		`CREATE TABLE public.operation_source_observation_clocks (
			owner_user_id text NOT NULL,
			workspace_id text NOT NULL,
			generation bigint NOT NULL,
			CONSTRAINT operation_source_observation_clocks_pkey PRIMARY KEY (owner_user_id, workspace_id),
			CONSTRAINT chk_operation_source_observation_clocks_generation CHECK (generation > 0)
		);`,
		`CREATE TABLE public.operation_source_observations (
			id uuid PRIMARY KEY,
			owner_user_id text NOT NULL,
			workspace_id text NOT NULL,
			origin_id uuid NOT NULL,
			config_digest text NOT NULL,
			generation bigint NOT NULL,
			started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
			CONSTRAINT uq_operation_source_observations_scope_generation UNIQUE (owner_user_id, workspace_id, generation),
			CONSTRAINT uq_operation_source_observations_scope_id_generation_origin UNIQUE (owner_user_id, workspace_id, id, generation, origin_id),
			CONSTRAINT chk_operation_source_observations_config_digest CHECK (
				octet_length(config_digest) = 64 AND config_digest ~ '^[0-9a-f]{64}$'
			),
			CONSTRAINT chk_operation_source_observations_generation CHECK (generation > 0)
		);`,
	)
	if strings.Count(up, "create table ") != 2 || strings.Count(up, "unique (") != 2 {
		t.Error("observation prerequisite must create only two tables and the specified scoped unique keys")
	}
}

func TestSourceObservationClockCannotResetChangeScopeOrLoseHistory(t *testing.T) {
	up := sourceObservationSQL(t, "up")
	sourceObservationRequire(t, up,
		`CREATE FUNCTION public.hai_guard_source_observation_clock_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
			IF TG_OP <> 'UPDATE' THEN
				RAISE EXCEPTION 'source observation clocks cannot be deleted or truncated'
					USING ERRCODE = 'integrity_constraint_violation';
			END IF;
			IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
				OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
				OR NEW.generation IS DISTINCT FROM OLD.generation + 1 THEN
				RAISE EXCEPTION 'source observation clock scope is immutable and generation must advance by one'
					USING ERRCODE = 'integrity_constraint_violation';
			END IF;
			RETURN NEW;
		END; $$;`,
		`CREATE TRIGGER trg_source_observation_clocks_monotonic
			BEFORE UPDATE OR DELETE ON public.operation_source_observation_clocks
			FOR EACH ROW EXECUTE FUNCTION public.hai_guard_source_observation_clock_mutation();`,
		`CREATE TRIGGER trg_source_observation_clocks_no_truncate
			BEFORE TRUNCATE ON public.operation_source_observation_clocks
			FOR EACH STATEMENT EXECUTE FUNCTION public.hai_guard_source_observation_clock_mutation();`,
	)
}

func TestSourceObservationsAreAppendOnlyIncludingTruncate(t *testing.T) {
	up := sourceObservationSQL(t, "up")
	sourceObservationRequire(t, up,
		`CREATE FUNCTION public.hai_reject_source_observation_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
			RAISE EXCEPTION 'source observations are immutable' USING ERRCODE = 'integrity_constraint_violation';
		END; $$;`,
		`CREATE TRIGGER trg_source_observations_immutable BEFORE UPDATE OR DELETE ON public.operation_source_observations
			FOR EACH ROW EXECUTE FUNCTION public.hai_reject_source_observation_mutation();`,
		`CREATE TRIGGER trg_source_observations_no_truncate BEFORE TRUNCATE ON public.operation_source_observations
			FOR EACH STATEMENT EXECUTE FUNCTION public.hai_reject_source_observation_mutation();`,
	)
}

func TestOperationSourceObservationProvenanceIsScopedAndImmutableFromNull(t *testing.T) {
	up := sourceObservationSQL(t, "up")
	sourceObservationRequire(t, up,
		`ALTER TABLE public.operations
			ADD COLUMN source_observation_id uuid,
			ADD COLUMN source_observation_generation bigint NOT NULL DEFAULT 0,
			ADD CONSTRAINT chk_operations_source_observation_group CHECK (
				(source_observation_id IS NULL AND source_observation_generation = 0)
				OR (source_observation_id IS NOT NULL AND source_observation_generation > 0 AND source_identity_hash <> ''
					AND account_feed_id IS NOT NULL AND account_feed_id <> '00000000-0000-0000-0000-000000000000'::uuid)
			),
			ADD CONSTRAINT fk_operations_source_observation_scope
				FOREIGN KEY (owner_user_id, workspace_id, source_observation_id, source_observation_generation, account_feed_id)
				REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id)
				MATCH SIMPLE ON UPDATE RESTRICT ON DELETE RESTRICT;`,
		`CREATE FUNCTION public.hai_reject_operation_source_observation_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
			IF NEW.source_observation_id IS DISTINCT FROM OLD.source_observation_id
				OR NEW.source_observation_generation IS DISTINCT FROM OLD.source_observation_generation
				OR (OLD.source_observation_id IS NOT NULL AND NEW.account_feed_id IS DISTINCT FROM OLD.account_feed_id) THEN
				RAISE EXCEPTION 'operation source observation provenance is immutable; reconciliation requires a separate migration path'
					USING ERRCODE = 'integrity_constraint_violation';
			END IF;
			RETURN NEW;
		END; $$;`,
		`CREATE TRIGGER trg_operations_source_observation_immutable BEFORE UPDATE ON public.operations
			FOR EACH ROW EXECUTE FUNCTION public.hai_reject_operation_source_observation_mutation();`,
	)
	if strings.Count(up, "add column ") != 2 || strings.Count(up, "foreign key (") != 1 {
		t.Error("operation observation provenance requires exactly two columns and one composite tenant/generation/origin FK")
	}
	for _, forbidden := range []string{
		"before update of", "old.source_observation_id is null", "new.source_observation_id :=",
		"new.source_observation_generation :=", "update public.operations", "insert into", "delete from",
		"source_uri", "source_head", "operation_execution_claims", "security definer", "grant ",
		"disable trigger", "cascade", "drop ", "alter column", "truncate table",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("observation prerequisite must not backfill, bypass provenance, add head authority or broaden permissions: %q", forbidden)
		}
	}
}

func TestOperationSourceObservationRequiresExactNonzeroFeedOrigin(t *testing.T) {
	up := sourceObservationSQL(t, "up")
	sourceObservationRequire(t, up,
		`CONSTRAINT uq_operation_source_observations_scope_id_generation_origin
			UNIQUE (owner_user_id, workspace_id, id, generation, origin_id)`,
		`AND account_feed_id IS NOT NULL
			AND account_feed_id <> '00000000-0000-0000-0000-000000000000'::uuid`,
		`FOREIGN KEY (owner_user_id, workspace_id, source_observation_id, source_observation_generation, account_feed_id)
			REFERENCES public.operation_source_observations (owner_user_id, workspace_id, id, generation, origin_id)
			MATCH SIMPLE ON UPDATE RESTRICT ON DELETE RESTRICT`,
		`(source_observation_id IS NULL AND source_observation_generation = 0)`,
		`OR (OLD.source_observation_id IS NOT NULL AND NEW.account_feed_id IS DISTINCT FROM OLD.account_feed_id)`,
	)
	if strings.Contains(up, "match full") || strings.Contains(up, "match partial") {
		t.Error("observation FK must preserve legacy NULL/zero groups without broadening observed origin binding")
	}
}

func TestSourceObservationRejectsZeroIdentities(t *testing.T) {
	sourceObservationRequire(t, sourceObservationSQL(t, "up"), `ALTER TABLE public.operation_source_observations
		ADD CONSTRAINT chk_source_observations_nonzero_identity CHECK (
			id <> '00000000-0000-0000-0000-000000000000'::uuid
			AND origin_id <> '00000000-0000-0000-0000-000000000000'::uuid
		);`)
}

func TestSourceObservationRollbackLocksAndRefusesHistoryBeforeAnyDrop(t *testing.T) {
	down := sourceObservationSQL(t, "down")
	preflight := sourceObservationNormalize(`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM public.operation_source_observation_clocks)
			OR EXISTS (SELECT 1 FROM public.operation_source_observations)
			OR EXISTS (SELECT 1 FROM public.operations
				WHERE source_observation_id IS NOT NULL OR source_observation_generation <> 0) THEN
			RAISE EXCEPTION 'rollback refused: source observation history or operation provenance exists'
				USING ERRCODE = '55000';
		END IF;
	END; $$;`)
	lockSQL := "lock table public.operation_source_observation_clocks, public.operation_source_observations, public.operations in access exclusive mode;"
	lock := strings.Index(down, lockSQL)
	check := strings.Index(down, preflight)
	firstDrop := strings.Index(down, "drop ")
	if lock < 0 || check <= lock || firstDrop < check+len(preflight) {
		t.Fatalf("rollback must lock all writers and refuse clock/observation/operation history before any drop: lock=%d preflight=%d drop=%d", lock, check, firstDrop)
	}
	for _, forbidden := range []string{"delete from", "truncate table ", "truncate public.", "insert into", "update public.", "cascade", "drop table if exists public.operations;", "operation_events"} {
		if strings.Contains(down, forbidden) {
			t.Errorf("rollback must not erase or rewrite historical rows: %q", forbidden)
		}
	}
	raw, err := Files.ReadFile("pre/" + sourceObservationMigration + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	warning := strings.ToLower(string(raw))
	for _, required := range []string{"guarded operator rollback", "no data rows are deleted", "empty-only", "operations and operation events remain", "transactional"} {
		if !strings.Contains(warning, required) {
			t.Errorf("rollback warning missing %q", required)
		}
	}
}

func TestSourceObservationRollbackDropsOnlyOwnedArtifactsInDependencyOrder(t *testing.T) {
	down := sourceObservationSQL(t, "down")
	start := strings.Index(down, "drop ")
	if start < 0 {
		t.Fatal("rollback lacks owned artifact removal")
	}
	want := sourceObservationNormalize(`
		DROP TRIGGER IF EXISTS trg_operations_source_observation_immutable ON public.operations;
		DROP FUNCTION IF EXISTS public.hai_reject_operation_source_observation_mutation();
		ALTER TABLE public.operations
			DROP CONSTRAINT IF EXISTS fk_operations_source_observation_scope,
			DROP CONSTRAINT IF EXISTS chk_operations_source_observation_group,
			DROP COLUMN IF EXISTS source_observation_id,
			DROP COLUMN IF EXISTS source_observation_generation;
		DROP TRIGGER IF EXISTS trg_source_observations_no_truncate ON public.operation_source_observations;
		DROP TRIGGER IF EXISTS trg_source_observations_immutable ON public.operation_source_observations;
		DROP FUNCTION IF EXISTS public.hai_reject_source_observation_mutation();
		DROP TABLE IF EXISTS public.operation_source_observations;
		DROP TRIGGER IF EXISTS trg_source_observation_clocks_no_truncate ON public.operation_source_observation_clocks;
		DROP TRIGGER IF EXISTS trg_source_observation_clocks_monotonic ON public.operation_source_observation_clocks;
		DROP FUNCTION IF EXISTS public.hai_guard_source_observation_clock_mutation();
		DROP TABLE IF EXISTS public.operation_source_observation_clocks;`)
	if down[start:] != want {
		t.Fatal("empty-only rollback must drop only owned artifacts with operation FK removed before observations")
	}
	sourceObservationRequire(t, down, "SET LOCAL lock_timeout = '5s';")
}

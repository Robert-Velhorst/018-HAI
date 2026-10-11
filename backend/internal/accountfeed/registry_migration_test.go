package accountfeed

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

// These contracts inspect embedded SQL source only; they do not execute PostgreSQL.
func accountFeedRegistryMigrationSQL(t *testing.T, direction string) string {
	t.Helper()
	path := "pre/0110_account_feed_registry." + direction + ".sql"
	body, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read embedded account feed registry migration %s: %v", path, err)
	}
	return strings.Join(strings.Fields(string(body)), " ")
}

func requireAccountFeedMigrationFragments(t *testing.T, sql string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("account feed registry migration is missing %q", fragment)
		}
	}
}

func accountFeedMigrationTableSQL(t *testing.T, sql, table string) string {
	t.Helper()
	start := strings.Index(sql, "CREATE TABLE public."+table+" (")
	if start < 0 {
		t.Fatalf("account feed registry migration is missing table %s", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("account feed registry table %s has no closing statement", table)
	}
	return sql[start : start+end+2]
}

func TestAccountFeedRegistryMigrationEmbeddedSchema(t *testing.T) {
	t.Parallel()
	up := accountFeedRegistryMigrationSQL(t, "up")
	feeds := accountFeedMigrationTableSQL(t, up, "account_feeds")
	requireAccountFeedMigrationFragments(t, feeds,
		"id uuid NOT NULL,",
		"CONSTRAINT account_feeds_pkey PRIMARY KEY (id)",
		"owner_user_id text NOT NULL,",
		"workspace_id text NOT NULL,",
		"name text NOT NULL,",
		"provider text NOT NULL,",
		"account_label text NOT NULL DEFAULT '',",
		"source_type text NOT NULL,",
		"source_type IN ('local_json_file', 'http_json_feed')",
		"path text NOT NULL DEFAULT '',",
		"url text NOT NULL DEFAULT '',",
		"project_key text NOT NULL DEFAULT '',",
		"operation_type text NOT NULL DEFAULT '',",
		"enabled boolean NOT NULL,",
		"UNIQUE (owner_user_id, workspace_id, id)",
		"char_length(btrim(owner_user_id)) > 0",
		"char_length(btrim(workspace_id)) > 0",
		"char_length(btrim(name)) > 0",
		"char_length(btrim(provider)) > 0",
	)
	audits := accountFeedMigrationTableSQL(t, up, "account_feed_audits")
	requireAccountFeedMigrationFragments(t, audits,
		"id uuid NOT NULL,",
		"CONSTRAINT account_feed_audits_pkey PRIMARY KEY (id)",
		"feed_id uuid NOT NULL,",
		"owner_user_id text NOT NULL,",
		"workspace_id text NOT NULL,",
		"event_type text NOT NULL,",
		"message text NOT NULL,",
		"created_at timestamptz NOT NULL,",
		"char_length(btrim(owner_user_id)) > 0",
		"char_length(btrim(workspace_id)) > 0",
		"char_length(btrim(event_type)) > 0",
		"char_length(btrim(message)) > 0",
	)
}

func TestAccountFeedRegistryMigrationScopedAuditForeignKey(t *testing.T) {
	t.Parallel()
	up := accountFeedRegistryMigrationSQL(t, "up")
	audits := accountFeedMigrationTableSQL(t, up, "account_feed_audits")
	requireAccountFeedMigrationFragments(t, audits,
		"CONSTRAINT fk_account_feed_audits_feed_owner_workspace FOREIGN KEY (owner_user_id, workspace_id, feed_id) REFERENCES public.account_feeds (owner_user_id, workspace_id, id) ON UPDATE RESTRICT ON DELETE RESTRICT",
	)
	requireAccountFeedMigrationFragments(t, up,
		"CREATE INDEX idx_account_feed_audits_owner_workspace_feed_created ON public.account_feed_audits (owner_user_id, workspace_id, feed_id, created_at DESC, id DESC);",
	)
	if strings.Count(up, "FOREIGN KEY") != 1 {
		t.Error("account feed registry must have only its scoped audit foreign key")
	}
}

func TestAccountFeedRegistryMigrationSyncLockAndObservations(t *testing.T) {
	t.Parallel()
	feeds := accountFeedMigrationTableSQL(t, accountFeedRegistryMigrationSQL(t, "up"), "account_feeds")
	requireAccountFeedMigrationFragments(t, feeds,
		"last_attempt_at timestamptz,",
		"last_success_at timestamptz,",
		"last_items_read bigint NOT NULL DEFAULT 0,",
		"CONSTRAINT chk_account_feeds_last_items_read CHECK (last_items_read >= 0)",
		"sync_token uuid,",
		"sync_started_at timestamptz,",
		"CONSTRAINT chk_account_feeds_sync_lock CHECK ( (sync_token IS NULL) = (sync_started_at IS NULL) )",
	)
}

func TestAccountFeedRegistryMigrationImmutableAudits(t *testing.T) {
	t.Parallel()
	up := accountFeedRegistryMigrationSQL(t, "up")
	requireAccountFeedMigrationFragments(t, up,
		"CREATE FUNCTION public.hai_reject_account_feed_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'account feed audits are immutable' USING ERRCODE = 'integrity_constraint_violation'; END; $$;",
		"CREATE TRIGGER trg_account_feed_audits_immutable BEFORE UPDATE OR DELETE ON public.account_feed_audits FOR EACH ROW EXECUTE FUNCTION public.hai_reject_account_feed_audit_mutation();",
		"CREATE TRIGGER trg_account_feed_audits_no_truncate BEFORE TRUNCATE ON public.account_feed_audits FOR EACH STATEMENT EXECUTE FUNCTION public.hai_reject_account_feed_audit_mutation();",
	)
	if strings.Count(up, "CREATE TRIGGER") != 2 {
		t.Error("only the two immutable audit triggers should be installed; feeds must remain mutable")
	}
}

func TestAccountFeedRegistryMigrationAdditiveScope(t *testing.T) {
	t.Parallel()
	up := accountFeedRegistryMigrationSQL(t, "up")
	if strings.Count(up, "CREATE TABLE public.") != 2 {
		t.Error("account feed registry migration must create exactly its two new tables")
	}
	for _, fragment := range []string{
		"ALTER TABLE", "DROP TABLE", "TRUNCATE TABLE", "DELETE FROM",
		"UPDATE public.", "INSERT INTO", "AutoMigrate", "CREATE OR REPLACE",
		"ROW LEVEL SECURITY", "CREATE POLICY", "public.operations",
		"public.evidence_packs", "hai_reject_evidence_pack_mutation",
	} {
		if strings.Contains(up, fragment) {
			t.Errorf("additive account feed registry migration must not contain %q", fragment)
		}
	}
}

func TestAccountFeedRegistryMigrationDestructiveOperatorRollback(t *testing.T) {
	t.Parallel()
	down := accountFeedRegistryMigrationSQL(t, "down")
	requireAccountFeedMigrationFragments(t, down,
		"AUDIT-DESTROYING OPERATOR ROLLBACK",
		"permanently removes all account feed",
		"approval and a verified backup/export would be required before execution",
		"Stop account-feed writers before rollback.",
	)
	previous := -1
	for _, statement := range []string{
		"DROP TRIGGER IF EXISTS trg_account_feed_audits_no_truncate ON public.account_feed_audits;",
		"DROP TRIGGER IF EXISTS trg_account_feed_audits_immutable ON public.account_feed_audits;",
		"DROP FUNCTION IF EXISTS public.hai_reject_account_feed_audit_mutation();",
		"DROP TABLE IF EXISTS public.account_feed_audits;",
		"DROP TABLE IF EXISTS public.account_feeds;",
	} {
		position := strings.Index(down, statement)
		if position < 0 {
			t.Errorf("account feed registry rollback is missing %q", statement)
			continue
		}
		if position <= previous {
			t.Errorf("account feed registry rollback has out-of-order dependency removal: %q", statement)
		}
		previous = position
	}
	for _, fragment := range []string{
		"CASCADE;", "ALTER TABLE", "public.operations", "public.evidence_packs",
		"hai_reject_evidence_pack_mutation",
	} {
		if strings.Contains(down, fragment) {
			t.Errorf("account feed registry rollback must not contain %q", fragment)
		}
	}
	if strings.Count(down, "DROP TABLE IF EXISTS public.") != 2 {
		t.Error("account feed registry rollback must drop exactly its two tables")
	}
}

package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Requires the existing dedicated test DSN and destructive-test opt-in before
// the shared fixture creates a fresh owned database. A skip is not acceptance.
func TestSourceObservationPostgresUpgradeMintConstraintsAndRollback(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db := openIsolatedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	const version = "pre/0112_operation_source_observation"
	const owner, workspace = "observation-test-owner", "local"
	const legacyURI = "local:account:with:colons:record:with:colons"
	prior := migrationFilesThrough(t, "pre/0111_operation_source_identity")
	if _, err := infra.ApplyMigrations(db, prior, "pre"); err != nil {
		t.Fatalf("apply predecessor migrations: %v", err)
	}
	legacyID, eventID := uuid.New(), uuid.New()
	if err := db.Exec(`INSERT INTO public.operations
 (id, owner_user_id, workspace_id, title, source_type, source_uri,
  source_revision_hash, operation_type, status, risk_level, autonomy_level,
  owner_type, dedupe_key, version)
 VALUES (?, ?, ?, 'historical', 'manual', ?, 'legacy opaque revision',
 'internal', 'new', 'low', 'observe', 'hai', ?, 1)`,
		legacyID, owner, workspace, legacyURI, uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO public.operation_events
 (id, operation_id, event_type, actor_type, after_status, message, payload_json)
 VALUES (?, ?, 'created', 'hai', 'new', 'retain legacy history', '{}')`, eventID, legacyID).Error; err != nil {
		t.Fatal(err)
	}
	files := migrationFilesThrough(t, version)
	applyObservation := func() {
		t.Helper()
		if n, err := infra.ApplyMigrations(db, files, "pre"); err != nil || n != 1 {
			t.Fatalf("apply observation migration: %d %v", n, err)
		}
	}
	assertLegacy := func(t *testing.T, withObservation bool) {
		t.Helper()
		var uri, revision, identity string
		if err := db.Raw(`SELECT source_uri, source_revision_hash, source_identity_hash
 FROM public.operations WHERE id = ?`, legacyID).Row().Scan(&uri, &revision, &identity); err != nil ||
			uri != legacyURI || revision != "legacy opaque revision" || identity != "" {
			t.Fatalf("historical source changed: %q %q %q / %v", uri, revision, identity, err)
		}
		if withObservation {
			var id, feedID sql.NullString
			var generation int64
			if err := db.Raw(`SELECT source_observation_id, source_observation_generation, account_feed_id
 FROM public.operations WHERE id = ?`, legacyID).Row().Scan(&id, &generation, &feedID); err != nil || id.Valid || generation != 0 || feedID.Valid {
				t.Fatalf("historical observation must remain NULL/zero with unchanged NULL feed: %+v %d %+v / %v", id, generation, feedID, err)
			}
		}
		var count int64
		if err := db.Raw(`SELECT count(*) FROM public.operation_events WHERE id = ? AND operation_id = ?`,
			eventID, legacyID).Row().Scan(&count); err != nil || count != 1 {
			t.Fatalf("legacy audit changed: %d / %v", count, err)
		}
	}
	assertCount := func(t *testing.T, table string, want int64) {
		t.Helper()
		// Callers use fixed owned table names, never environment or request data.
		var count int64
		if err := db.Raw("SELECT count(*) FROM public." + table).Row().Scan(&count); err != nil || count != want {
			t.Fatalf("%s count = %d, want %d / %v", table, count, want, err)
		}
	}
	assertRollbackRefused := func(t *testing.T) {
		t.Helper()
		err := infra.RollbackMigration(db, files, "pre", version)
		assertSourceObservationSQLState(t, err, "55000")
		if !strings.Contains(err.Error(), "rollback refused") {
			t.Fatalf("rollback did not refuse observation history: %v", err)
		}
		var applied int64
		if err := db.Raw(`SELECT count(*) FROM public.schema_migrations WHERE version = ?`, version).Row().Scan(&applied); err != nil || applied != 1 {
			t.Fatalf("refused rollback changed migration ledger: %d / %v", applied, err)
		}
		assertLegacy(t, true)
	}
	applyObservation()
	assertLegacy(t, true)
	assertCount(t, "operation_source_observation_clocks", 0)
	assertCount(t, "operation_source_observations", 0)

	// A dependent view must stop rollback without being removed implicitly.
	if err := db.Exec(`CREATE VIEW public.source_observation_rollback_dependency AS
 SELECT source_observation_id FROM public.operations`).Error; err != nil {
		t.Fatal(err)
	}
	assertSourceObservationSQLState(t, infra.RollbackMigration(db, files, "pre", version), "2BP01")
	assertLegacy(t, true)
	if err := db.Exec(`SELECT source_observation_id FROM public.source_observation_rollback_dependency`).Error; err != nil {
		t.Fatalf("rollback cascaded into dependent view: %v", err)
	}
	if err := db.Exec(`DROP VIEW public.source_observation_rollback_dependency`).Error; err != nil {
		t.Fatal(err)
	}
	if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
		t.Fatalf("empty observation rollback: %v", err)
	}
	assertLegacy(t, false)
	applyObservation()
	assertLegacy(t, true)

	// A clock alone is history even if no observation or operation references it.
	if err := db.Exec(`INSERT INTO public.operation_source_observation_clocks
 (owner_user_id, workspace_id, generation) VALUES ('clock-only-owner', 'local', 1)`).Error; err != nil {
		t.Fatal(err)
	}
	assertRollbackRefused(t)
	assertCount(t, "operation_source_observation_clocks", 1)
	assertCount(t, "operation_source_observations", 0)

	origin := uuid.New()
	digest := strings.Repeat("a", 64)
	firstID, secondID := uuid.New(), uuid.New()
	first, err := mintSourceObservationPostgres(db, owner, workspace, firstID, origin, digest)
	if err != nil || first != 1 {
		t.Fatalf("first counter/observation transaction: %d / %v", first, err)
	}
	second, err := mintSourceObservationPostgres(db, owner, workspace, secondID, origin, digest)
	if err != nil || second != 2 {
		t.Fatalf("second counter/observation transaction: %d / %v", second, err)
	}
	assertCount(t, "operation_source_observations", 2)
	assertRollbackRefused(t)
	assertClock := func(t *testing.T, want int64) {
		t.Helper()
		var generation int64
		if err := db.Raw(`SELECT generation FROM public.operation_source_observation_clocks
 WHERE owner_user_id = ? AND workspace_id = ?`, owner, workspace).Row().Scan(&generation); err != nil || generation != want {
			t.Fatalf("counter generation = %d, want %d / %v", generation, want, err)
		}
	}
	assertClock(t, 2)
	for _, invalid := range []struct {
		name       string
		id, origin uuid.UUID
	}{
		{"zero observation ID", uuid.Nil, origin},
		{"zero origin ID", uuid.New(), uuid.Nil},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			_, err := mintSourceObservationPostgres(db, owner, workspace, invalid.id, invalid.origin, digest)
			assertSourceObservationSQLState(t, err, "23514")
			assertClock(t, 2)
			assertCount(t, "operation_source_observations", 2)
		})
	}
	_, err = mintSourceObservationPostgres(db, "zero-origin-owner", workspace, uuid.New(), uuid.Nil, digest)
	assertSourceObservationSQLState(t, err, "23514")
	assertCount(t, "operation_source_observation_clocks", 2)
	var storedOrigin uuid.UUID
	var storedDigest string
	var startedAt time.Time
	if err := db.Raw(`SELECT origin_id, config_digest, started_at FROM public.operation_source_observations
 WHERE owner_user_id = ? AND workspace_id = ? AND id = ? AND generation = ?`, owner, workspace, firstID, first).
		Row().Scan(&storedOrigin, &storedDigest, &startedAt); err != nil || storedOrigin != origin || storedDigest != digest || startedAt.IsZero() {
		t.Fatalf("minted observation not persisted: %s %q %v / %v", storedOrigin, storedDigest, startedAt, err)
	}
	// Rejection of the observation must roll back the preceding counter increment.
	for _, invalid := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		_, err := mintSourceObservationPostgres(db, owner, workspace, uuid.New(), origin, invalid)
		assertSourceObservationSQLState(t, err, "23514")
		assertClock(t, 2)
		assertCount(t, "operation_source_observations", 2)
	}
	_, err = mintSourceObservationPostgres(db, "aborted-owner", workspace, uuid.New(), origin, "invalid")
	assertSourceObservationSQLState(t, err, "23514")
	assertCount(t, "operation_source_observation_clocks", 2)
	assertSourceObservationSQLState(t, db.Exec(`INSERT INTO public.operation_source_observations
 (id, owner_user_id, workspace_id, origin_id, config_digest, generation)
 VALUES (?, ?, ?, ?, ?, ?)`, uuid.New(), owner, workspace, origin, digest, first).Error, "23505")
	assertSourceObservationSQLState(t, db.Exec(`INSERT INTO public.operation_source_observation_clocks
 (owner_user_id, workspace_id, generation) VALUES ('invalid-clock', 'local', 0)`).Error, "23514")

	insertOperation := func(target *gorm.DB, opOwner, opWorkspace string, observation any, generation int64, feedID any, identified bool) error {
		provider, account, externalID, identity, revision := "", "", "", "", ""
		if identified {
			provider, account, externalID = "local", "account", "record"
			identity, revision = digest, "opaque source revision"
		}
		return target.Exec(`INSERT INTO public.operations
 (id, owner_user_id, workspace_id, title, source_type, operation_type, status,
  risk_level, autonomy_level, owner_type, dedupe_key, version,
  source_provider, source_account, source_external_id, source_identity_hash,
  source_revision_hash, source_observation_id, source_observation_generation, account_feed_id)
 VALUES (?, ?, ?, 'observed', 'manual', 'internal', 'new', 'low', 'observe', 'hai', ?, 1,
 ?, ?, ?, ?, ?, ?, ?, ?)`, uuid.New(), opOwner, opWorkspace, uuid.NewString(),
			provider, account, externalID, identity, revision, observation, generation, feedID).Error
	}
	for _, mismatch := range []struct {
		name, owner, workspace string
		id                     uuid.UUID
		generation             int64
	}{
		{"foreign owner", "foreign-owner", workspace, firstID, first},
		{"foreign workspace", owner, "foreign-workspace", firstID, first},
		{"wrong generation", owner, workspace, firstID, second},
		{"missing observation", owner, workspace, uuid.New(), first},
	} {
		t.Run(mismatch.name, func(t *testing.T) {
			assertSourceObservationSQLState(t, insertOperation(db, mismatch.owner, mismatch.workspace, mismatch.id, mismatch.generation, origin, true), "23503")
		})
	}
	assertSourceObservationSQLState(t, insertOperation(db, owner, workspace, nil, first, origin, true), "23514")
	assertSourceObservationSQLState(t, insertOperation(db, owner, workspace, firstID, 0, origin, true), "23514")
	assertSourceObservationSQLState(t, insertOperation(db, owner, workspace, firstID, first, origin, false), "23514")
	for _, invalid := range []struct {
		name     string
		feedID   any
		sqlState string
	}{
		{"observed operation NULL origin", nil, "23514"},
		{"observed operation zero origin", uuid.Nil, "23514"},
		{"observed operation wrong origin", uuid.New(), "23503"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			assertSourceObservationSQLState(t, insertOperation(db, owner, workspace, firstID, first, invalid.feedID, true), invalid.sqlState)
		})
	}
	t.Run("other feed observation cannot use first feed origin", func(t *testing.T) {
		// Roll back this additional valid feed fixture to preserve the existing
		// history/count assertions and avoid rewriting append-only observations.
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		otherID, otherOrigin := uuid.New(), uuid.New()
		otherGeneration, err := mintSourceObservationPostgres(tx, owner, workspace, otherID, otherOrigin, digest)
		if err != nil || otherGeneration != 3 {
			t.Fatalf("mint other feed observation: %d / %v", otherGeneration, err)
		}
		if err := insertOperation(tx, owner, workspace, otherID, otherGeneration, otherOrigin, true); err != nil {
			t.Fatalf("matching other-feed origin should be valid: %v", err)
		}
		assertSourceObservationSQLState(t, insertOperation(tx, owner, workspace, otherID, otherGeneration, origin, true), "23503")
	})
	assertClock(t, 2)
	assertCount(t, "operation_source_observations", 2)
	if err := insertOperation(db, owner, workspace, firstID, first, origin, true); err != nil {
		t.Fatalf("valid scoped observation operation: %v", err)
	}
	var observedID uuid.UUID
	if err := db.Raw(`SELECT id FROM public.operations WHERE source_observation_id = ?`, firstID).Row().Scan(&observedID); err != nil {
		t.Fatal(err)
	}
	var initialFeedID uuid.UUID
	if err := db.Raw(`SELECT account_feed_id FROM public.operations WHERE id = ?`, observedID).Row().Scan(&initialFeedID); err != nil || initialFeedID != origin {
		t.Fatalf("raw SQL fixture must bind the exact observation origin: %s / %v", initialFeedID, err)
	}
	for _, mutation := range []struct {
		name, statement string
		args            []any
	}{
		{"observation ID", `UPDATE public.operation_source_observations SET id = ? WHERE id = ?`, []any{uuid.New(), firstID}},
		{"observation owner", `UPDATE public.operation_source_observations SET owner_user_id = 'other' WHERE id = ?`, []any{firstID}},
		{"observation workspace", `UPDATE public.operation_source_observations SET workspace_id = 'other' WHERE id = ?`, []any{firstID}},
		{"observation origin", `UPDATE public.operation_source_observations SET origin_id = ? WHERE id = ?`, []any{uuid.New(), firstID}},
		{"observation digest", `UPDATE public.operation_source_observations SET config_digest = ? WHERE id = ?`, []any{strings.Repeat("b", 64), firstID}},
		{"observation generation", `UPDATE public.operation_source_observations SET generation = generation + 10 WHERE id = ?`, []any{firstID}},
		{"observation timestamp", `UPDATE public.operation_source_observations SET started_at = started_at + interval '1 second' WHERE id = ?`, []any{firstID}},
		{"observation deletion", `DELETE FROM public.operation_source_observations WHERE id = ?`, []any{secondID}},
		{"clock owner", `UPDATE public.operation_source_observation_clocks SET owner_user_id = 'other' WHERE owner_user_id = ?`, []any{owner}},
		{"clock workspace", `UPDATE public.operation_source_observation_clocks SET workspace_id = 'other' WHERE owner_user_id = ?`, []any{owner}},
		{"clock unchanged", `UPDATE public.operation_source_observation_clocks SET generation = generation WHERE owner_user_id = ?`, []any{owner}},
		{"clock skipped generation", `UPDATE public.operation_source_observation_clocks SET generation = generation + 2 WHERE owner_user_id = ?`, []any{owner}},
		{"clock reset", `UPDATE public.operation_source_observation_clocks SET generation = 1 WHERE owner_user_id = ?`, []any{owner}},
		{"clock deletion", `DELETE FROM public.operation_source_observation_clocks WHERE owner_user_id = ?`, []any{owner}},
		{"clock truncate", `TRUNCATE TABLE public.operation_source_observation_clocks`, nil},
		{"operation observation ID", `UPDATE public.operations SET source_observation_id = ? WHERE id = ?`, []any{secondID, observedID}},
		{"operation generation", `UPDATE public.operations SET source_observation_generation = ? WHERE id = ?`, []any{second, observedID}},
		{"operation provenance removal", `UPDATE public.operations SET source_observation_id = NULL, source_observation_generation = 0 WHERE id = ?`, []any{observedID}},
		{"operation feed replacement", `UPDATE public.operations SET account_feed_id = ? WHERE id = ?`, []any{uuid.New(), observedID}},
		{"operation feed removal", `UPDATE public.operations SET account_feed_id = NULL WHERE id = ?`, []any{observedID}},
		{"operation zero feed replacement", `UPDATE public.operations SET account_feed_id = ? WHERE id = ?`, []any{uuid.Nil, observedID}},
		{"legacy provenance backfill", `UPDATE public.operations SET source_observation_id = ?, source_observation_generation = ? WHERE id = ?`, []any{firstID, first, legacyID}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			assertSourceObservationSQLState(t, db.Exec(mutation.statement, mutation.args...).Error, "23000")
		})
	}
	// An FK may refuse TRUNCATE before the append-only trigger is evaluated.
	if err := db.Exec(`TRUNCATE TABLE public.operation_source_observations`).Error; err == nil {
		t.Fatal("observation history was truncated")
	}
	if err := db.Exec(`UPDATE public.operations SET title = 'ordinary edit',
 source_observation_id = source_observation_id, source_observation_generation = source_observation_generation,
 account_feed_id = account_feed_id
 WHERE id = ?`, observedID).Error; err != nil {
		t.Fatalf("unchanged provenance blocked ordinary edit: %v", err)
	}
	assertClock(t, 2)
	assertRollbackRefused(t)
	assertCount(t, "operation_source_observation_clocks", 2)
	assertCount(t, "operation_source_observations", 2)
	assertCount(t, "operations", 2)
	assertCount(t, "operation_events", 1)
	var retainedID, retainedFeedID uuid.UUID
	var retainedGeneration int64
	if err := db.Raw(`SELECT source_observation_id, source_observation_generation, account_feed_id
 FROM public.operations WHERE id = ?`, observedID).Row().Scan(&retainedID, &retainedGeneration, &retainedFeedID); err != nil ||
		retainedID != firstID || retainedGeneration != first || retainedFeedID != origin {
		t.Fatalf("mutation/refused rollback changed operation provenance: %s %d %s / %v", retainedID, retainedGeneration, retainedFeedID, err)
	}
	t.Run("actual repository and observed service intake", func(t *testing.T) {
		// Current runtime mints configuration epochs and publishes heads. Earlier
		// assertions deliberately exercise only the historical 0112 schema.
		if _, err := infra.ApplyMigrations(db, migrationFilesThrough(t, "pre/0114_operation_source_configuration"), "pre"); err != nil {
			t.Fatal(err)
		}
		if _, err := infra.ApplyMigrations(db, migrations.Files, "post"); err != nil {
			t.Fatalf("apply real repository claim schema: %v", err)
		}
		repo := operations.NewGormRepository(db)
		start := operations.SourceObservationStart{OwnerUserID: "actual-repository-owner", WorkspaceID: workspace,
			OriginID: uuid.New(), ConfigDigest: digest}
		minted, err := repo.BeginSourceObservation(ctx, start)
		if err != nil || minted.ID == uuid.Nil || minted.Generation != 1 || minted.StartedAt.IsZero() ||
			minted.OriginID != start.OriginID || minted.ConfigDigest != start.ConfigDigest {
			t.Fatalf("actual repository mint = %+v / %v", minted, err)
		}
		service := operations.NewService(repo)
		var created operations.IngestResult
		var bound operations.SourceObservation
		err = service.WithSourceObservation(ctx, start, func(observed context.Context) error {
			var active bool
			bound, active = operations.CurrentSourceObservation(observed)
			if !active {
				return errors.New("actual service did not supply live observation")
			}
			var err error
			created, err = service.IngestContext(observed, operations.NewOperationInput{
				OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID,
				Title: "real observed service intake", OperationType: "internal", SourceType: "manual",
				DedupeKey: uuid.NewString(), EvidenceJSON: "{}", AccountFeedID: &start.OriginID,
				SourceProvider: "local", SourceAccount: "actual-account", SourceExternalID: "actual-record",
				SourceRevisionHash: "opaque actual revision",
			})
			return err
		})
		if err != nil || !created.Created || bound.Generation != 2 || bound.ID == minted.ID ||
			created.Operation.SourceObservationID == nil || *created.Operation.SourceObservationID != bound.ID ||
			created.Operation.SourceObservationGeneration != bound.Generation || created.Operation.SourceIdentityHash == "" ||
			created.Operation.AccountFeedID == nil || *created.Operation.AccountFeedID != start.OriginID {
			t.Fatalf("actual observed intake = %+v bound=%+v / %v", created, bound, err)
		}
		stored, err := service.Get(start.OwnerUserID, start.WorkspaceID, created.Operation.ID)
		if err != nil || stored == nil || stored.SourceObservationID == nil || *stored.SourceObservationID != bound.ID ||
			stored.SourceObservationGeneration != bound.Generation || stored.AccountFeedID == nil || *stored.AccountFeedID != start.OriginID {
			t.Fatalf("actual service provenance not durable: %+v / %v", stored, err)
		}
		events, err := service.Events(created.Operation.ID)
		if err != nil || len(events) != 2 {
			t.Fatalf("actual service creation audit = %+v / %v", events, err)
		}
		createdEvents, headEvents := 0, 0
		for _, event := range events {
			if event.EventType == "created" {
				createdEvents++
			}
			if event.EventType == "source_head_published" {
				headEvents++
			}
		}
		if createdEvents != 1 || headEvents != 1 {
			t.Fatal("current intake lost unique creation or source authority audit")
		}
		for _, replacement := range []any{nil, uuid.New()} {
			assertSourceObservationSQLState(t, db.Exec(`UPDATE public.operations SET account_feed_id = ? WHERE id = ?`,
				replacement, created.Operation.ID).Error, "23000")
		}
		// The earlier 0112-only phase tested its SQL data guard. After upgrading,
		// the runner must refuse out-of-order rollback before invoking down SQL.
		rollbackErr := infra.RollbackMigration(db, migrations.Files, "pre", version)
		if rollbackErr == nil || !strings.Contains(rollbackErr.Error(), "while later-phase migration") || sourceConfigurationRaceSQLState(rollbackErr) != "" {
			t.Fatalf("current schema did not refuse ordered rollback at the runner: %v", rollbackErr)
		}
		for _, retainedVersion := range []string{version, "pre/0113_operation_source_heads", "pre/0114_operation_source_configuration"} {
			var applied int64
			if err := db.Raw(`SELECT count(*) FROM public.schema_migrations WHERE version = ?`, retainedVersion).Row().Scan(&applied); err != nil || applied != 1 {
				t.Fatalf("out-of-order refusal changed migration ledger %s: %d / %v", retainedVersion, applied, err)
			}
		}
		assertLegacy(t, true)
		assertCount(t, "operation_source_observation_clocks", 3)
		assertCount(t, "operation_source_observations", 4)
		assertCount(t, "operations", 3)
		assertCount(t, "operation_events", 3)
	})
}

func mintSourceObservationPostgres(db *gorm.DB, owner, workspace string, id, origin uuid.UUID, digest string) (int64, error) {
	var generation int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL lock_timeout = '5s'`).Error; err != nil {
			return err
		}
		if err := tx.Raw(`INSERT INTO public.operation_source_observation_clocks AS clocks
 (owner_user_id, workspace_id, generation) VALUES (?, ?, 1)
 ON CONFLICT (owner_user_id, workspace_id) DO UPDATE SET generation = clocks.generation + 1
 RETURNING generation`, owner, workspace).Row().Scan(&generation); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO public.operation_source_observations
 (id, owner_user_id, workspace_id, origin_id, config_digest, generation)
 VALUES (?, ?, ?, ?, ?, ?)`, id, owner, workspace, origin, digest, generation).Error
	})
	return generation, err
}

func assertSourceObservationSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("PostgreSQL error = %v, want SQLSTATE %s", err, want)
	}
}

package sourceevidence

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSnapshotDigestDeterministicAndSensitive(t *testing.T) {
	base := validSnapshot()
	want := SnapshotDigest(base)
	if want == "" {
		t.Fatal("SnapshotDigest returned an empty digest")
	}
	if got := SnapshotDigest(base); got != want {
		t.Fatalf("SnapshotDigest is not deterministic: got %q want %q", got, want)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{name: "owner", mutate: func(snapshot *Snapshot) { snapshot.OwnerIdentity = "other-owner" }},
		{name: "extraction id", mutate: func(snapshot *Snapshot) { snapshot.ExtractionID = uuid.NewString() }},
		{name: "source id", mutate: func(snapshot *Snapshot) { snapshot.SourceID = uuid.NewString() }},
		{name: "raw item id", mutate: func(snapshot *Snapshot) { snapshot.RawItemID = uuid.NewString() }},
		{name: "project", mutate: func(snapshot *Snapshot) { snapshot.ProjectKey = "another-project" }},
		{name: "raw project", mutate: func(snapshot *Snapshot) { snapshot.RawProjectKey = "another-project" }},
		{name: "extraction URI", mutate: func(snapshot *Snapshot) { snapshot.ExtractionURI = "local://source/changed" }},
		{name: "raw item URI", mutate: func(snapshot *Snapshot) { snapshot.RawItemURI = "local://source/changed" }},
		{name: "extraction hash", mutate: func(snapshot *Snapshot) { snapshot.ExtractionHash = strings.Repeat("b", 64) }},
		{name: "raw item hash", mutate: func(snapshot *Snapshot) { snapshot.RawItemHash = strings.Repeat("b", 64) }},
		{name: "fetched time", mutate: func(snapshot *Snapshot) { snapshot.FetchedAt = snapshot.FetchedAt.Add(time.Second) }},
		{name: "extraction time", mutate: func(snapshot *Snapshot) { snapshot.ExtractionAt = snapshot.ExtractionAt.Add(time.Second) }},
		{name: "sensitive flag", mutate: func(snapshot *Snapshot) { snapshot.Sensitive = !snapshot.Sensitive }},
		{name: "local only flag", mutate: func(snapshot *Snapshot) { snapshot.LocalOnly = !snapshot.LocalOnly }},
		{name: "connector", mutate: func(snapshot *Snapshot) { snapshot.ConnectorKey = "drive" }},
		{name: "raw payload digest", mutate: func(snapshot *Snapshot) { snapshot.RawItemPayloadDigest = strings.Repeat("e", 64) }},
		{name: "payload digest", mutate: func(snapshot *Snapshot) { snapshot.ExtractionPayloadDigest = strings.Repeat("d", 64) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			if got := SnapshotDigest(changed); got == want {
				t.Fatalf("SnapshotDigest did not change after mutating %s", test.name)
			}
		})
	}

	normalized := base
	normalized.OwnerIdentity = "  " + base.OwnerIdentity + "  "
	normalized.ProjectKey = " " + base.ProjectKey + " "
	normalized.ConnectorKey = " " + base.ConnectorKey + " "
	normalized.FetchedAt = base.FetchedAt.In(time.FixedZone("test", 2*60*60))
	normalized.ExtractionAt = base.ExtractionAt.In(time.FixedZone("test", -5*60*60))
	if got := SnapshotDigest(normalized); got != want {
		t.Fatalf("SnapshotDigest changed for normalized-equivalent input: got %q want %q", got, want)
	}
}

func TestRawItemPayloadDigestBindsRawRecord(t *testing.T) {
	base := models.SourceRawItem{
		ID:         uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		SourceID:   uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		ExternalID: "provider-item-1", ProjectKey: "project-hai", ItemType: "document",
		Title: "Source title", SourceURI: "local://source/item-1", Content: "original source body",
		Metadata: `{"revision":1}`, ContentHash: "0123456789abcdef",
		FetchedAt: time.Date(2026, time.August, 4, 11, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 4, 11, 1, 0, 0, time.UTC),
	}
	want := rawItemPayloadDigest(base)
	if !lowerSHA256.MatchString(want) {
		t.Fatalf("rawItemPayloadDigest = %q, want SHA-256", want)
	}
	if got := rawItemPayloadDigest(base); got != want {
		t.Fatalf("rawItemPayloadDigest is not deterministic: got %q want %q", got, want)
	}

	tests := []struct {
		name   string
		mutate func(*models.SourceRawItem)
	}{
		{name: "source id", mutate: func(item *models.SourceRawItem) { item.SourceID = uuid.New() }},
		{name: "external id", mutate: func(item *models.SourceRawItem) { item.ExternalID = "provider-item-2" }},
		{name: "project", mutate: func(item *models.SourceRawItem) { item.ProjectKey = "other-project" }},
		{name: "item type", mutate: func(item *models.SourceRawItem) { item.ItemType = "email" }},
		{name: "title", mutate: func(item *models.SourceRawItem) { item.Title = "changed title" }},
		{name: "source URI", mutate: func(item *models.SourceRawItem) { item.SourceURI = "local://source/changed" }},
		{name: "content", mutate: func(item *models.SourceRawItem) { item.Content = "changed source body" }},
		{name: "metadata", mutate: func(item *models.SourceRawItem) { item.Metadata = `{"revision":2}` }},
		{name: "stored hash", mutate: func(item *models.SourceRawItem) { item.ContentHash = "fedcba9876543210" }},
		{name: "fetch time", mutate: func(item *models.SourceRawItem) { item.FetchedAt = item.FetchedAt.Add(time.Second) }},
		{name: "record update time", mutate: func(item *models.SourceRawItem) { item.UpdatedAt = item.UpdatedAt.Add(time.Second) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			if got := rawItemPayloadDigest(changed); got == want {
				t.Fatalf("raw payload digest did not change after mutating %s", test.name)
			}
		})
	}
}

func TestVerifyClaimAcceptsConnectorFingerprintFormat(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	snapshot := validSnapshot()
	snapshot.ExtractionHash = "0123456789abcdef"
	snapshot.RawItemHash = snapshot.ExtractionHash
	snapshot.RawItemPayloadDigest = strings.Repeat("e", 64)
	snapshot.SnapshotDigest = SnapshotDigest(snapshot)
	claim := validClaim(snapshot)

	if err := VerifyClaim(snapshot, claim, snapshot.OwnerIdentity, now); err != nil {
		t.Fatalf("VerifyClaim rejected the connector's 64-bit hexadecimal fingerprint: %v", err)
	}
}

func TestVerifyClaimRejectsChangedRawPayloadDigest(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	previous := validSnapshot()
	previous.RawItemPayloadDigest = strings.Repeat("a", 64)
	previous.SnapshotDigest = SnapshotDigest(previous)
	claim := validClaim(previous)

	current := previous
	current.RawItemPayloadDigest = strings.Repeat("b", 64)
	current.SnapshotDigest = SnapshotDigest(current)
	if err := VerifyClaim(current, claim, current.OwnerIdentity, now); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("VerifyClaim accepted a claim after the raw-item payload digest changed: %v", err)
	}
}

func TestVerifyClaim(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	snapshot := validSnapshot()
	snapshot.FetchedAt = now.Add(-30 * time.Minute)
	snapshot.SnapshotDigest = SnapshotDigest(snapshot)
	claim := validClaim(snapshot)

	if err := VerifyClaim(snapshot, claim, snapshot.OwnerIdentity, now); err != nil {
		t.Fatalf("VerifyClaim exact match: %v", err)
	}

	tests := []struct {
		name       string
		owner      string
		mutateSnap func(*Snapshot)
		mutate     func(*Claim)
		want       error
	}{
		{name: "blank owner", owner: " ", want: ErrSnapshotMismatch},
		{name: "wrong owner", owner: "other-owner", want: ErrSnapshotMismatch},
		{name: "snapshot owner mismatch", mutateSnap: func(value *Snapshot) { value.OwnerIdentity = "other-owner" }, want: ErrSnapshotMismatch},
		{name: "missing requirement", mutate: func(value *Claim) { value.RequirementID = "" }, want: ErrSnapshotMismatch},
		{name: "wrong extraction id", mutate: func(value *Claim) { value.ExtractionID = uuid.NewString() }, want: ErrSnapshotMismatch},
		{name: "wrong source id", mutate: func(value *Claim) { value.SourceID = uuid.NewString() }, want: ErrSnapshotMismatch},
		{name: "wrong raw item id", mutate: func(value *Claim) { value.RawItemID = uuid.NewString() }, want: ErrSnapshotMismatch},
		{name: "malformed digest", mutate: func(value *Claim) { value.SnapshotDigest = "not-a-sha256" }, want: ErrSnapshotMismatch},
		{name: "uppercase digest", mutate: func(value *Claim) { value.SnapshotDigest = strings.ToUpper(value.SnapshotDigest) }, want: ErrSnapshotMismatch},
		{name: "wrong digest", mutate: func(value *Claim) { value.SnapshotDigest = strings.Repeat("f", 64) }, want: ErrSnapshotMismatch},
		{name: "unknown validator", mutate: func(value *Claim) { value.Validator = "model_confidence" }, want: ErrInvalidClaim},
		{name: "zero max age", mutate: func(value *Claim) { value.MaxAgeSeconds = 0 }, want: ErrInvalidClaim},
		{name: "negative max age", mutate: func(value *Claim) { value.MaxAgeSeconds = -1 }, want: ErrInvalidClaim},
		{name: "stale", mutateSnap: func(value *Snapshot) { value.FetchedAt = now.Add(-61 * time.Minute) }, want: ErrSnapshotMismatch},
		{name: "future", mutateSnap: func(value *Snapshot) { value.FetchedAt = now.Add(5*time.Minute + time.Nanosecond) }, want: ErrSnapshotMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateSnapshot := snapshot
			candidateClaim := claim
			owner := snapshot.OwnerIdentity
			if test.owner != "" {
				owner = test.owner
			}
			if test.mutateSnap != nil {
				test.mutateSnap(&candidateSnapshot)
			}
			if test.mutate != nil {
				test.mutate(&candidateClaim)
			}
			if err := VerifyClaim(candidateSnapshot, candidateClaim, owner, now); !errors.Is(err, test.want) {
				t.Fatalf("VerifyClaim error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifyClaimFreshnessBoundariesAndNonFreshValidators(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		validator string
		fetchedAt time.Time
		maxAge    int64
	}{
		{name: "fresh exactly at max age", validator: ValidatorFreshSource, fetchedAt: now.Add(-time.Hour), maxAge: 3600},
		{name: "future within clock skew", validator: ValidatorFreshSource, fetchedAt: now.Add(5 * time.Minute), maxAge: 3600},
		{name: "max int64 age does not overflow duration", validator: ValidatorFreshSource, fetchedAt: now.Add(-30 * time.Minute), maxAge: int64(^uint64(0) >> 1)},
		{name: "primary source ignores max age", validator: ValidatorPrimarySource, fetchedAt: now.Add(-365 * 24 * time.Hour)},
		{name: "source context ignores max age", validator: ValidatorSourceContext, fetchedAt: now.Add(-365 * 24 * time.Hour)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validSnapshot()
			snapshot.FetchedAt = test.fetchedAt
			snapshot.SnapshotDigest = SnapshotDigest(snapshot)
			claim := validClaim(snapshot)
			claim.Validator = test.validator
			claim.MaxAgeSeconds = test.maxAge
			if err := VerifyClaim(snapshot, claim, snapshot.OwnerIdentity, now); err != nil {
				t.Fatalf("VerifyClaim boundary rejected: %v", err)
			}
		})
	}
}

func TestVerifyClaimFreshnessLargeAgeStillRejectsOlderSource(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	snapshot := validSnapshot()
	snapshot.FetchedAt = time.Date(1526, time.August, 4, 12, 0, 0, 0, time.UTC)
	snapshot.SnapshotDigest = SnapshotDigest(snapshot)
	claim := validClaim(snapshot)
	claim.MaxAgeSeconds = 300 * 365 * 24 * 60 * 60

	if err := VerifyClaim(snapshot, claim, snapshot.OwnerIdentity, now); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("VerifyClaim accepted a 500-year-old source with a 300-year maximum age: %v", err)
	}
}

func TestVerifyClaimFreshnessRejectsNanosecondPastLimit(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	snapshot := validSnapshot()
	snapshot.FetchedAt = now.Add(-time.Second - time.Nanosecond)
	snapshot.SnapshotDigest = SnapshotDigest(snapshot)
	claim := validClaim(snapshot)
	claim.MaxAgeSeconds = 1

	if err := VerifyClaim(snapshot, claim, snapshot.OwnerIdentity, now); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("VerifyClaim accepted a source one nanosecond beyond its maximum age: %v", err)
	}
}

func TestGormRepositoryPostgresResolve(t *testing.T) {
	db := openSourceEvidencePostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin fixture transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	owner := "source-evidence-integration-owner"
	sourceID := uuid.New()
	rawItemID := uuid.New()
	extractionID := uuid.New()
	uri := "local://source-evidence/fixture"
	hash := strings.Repeat("a", 64)

	if err := tx.Exec(`
		INSERT INTO public.connected_sources
			(id, owner_identity, connector_key, name, category, enabled, local_only, status, created_at, updated_at)
		VALUES (?, ?, 'local-folder', 'Source evidence fixture', 'documents', TRUE, TRUE, 'active', ?, ?)`,
		sourceID, owner, now, now,
	).Error; err != nil {
		t.Fatalf("insert connected source: %v", err)
	}
	if err := tx.Exec(`
		INSERT INTO public.source_raw_items
			(id, source_id, external_id, project_key, item_type, title, source_uri, content, metadata, content_hash, fetched_at, created_at, updated_at)
		VALUES (?, ?, 'fixture-1', 'project-hai', 'document', 'Evidence fixture', ?, 'source text', '{}', ?, ?, ?, ?)`,
		rawItemID, sourceID, uri, hash, now.Add(-time.Hour), now, now,
	).Error; err != nil {
		t.Fatalf("insert raw item: %v", err)
	}
	if err := tx.Exec(`
		INSERT INTO public.source_extractions
			(id, source_id, raw_item_id, project_key, content_type, text, summary, entities, dates, tasks, decisions, follow_ups, source_uri, source_label, content_hash, sensitive, uncertain, archived, created_at, updated_at)
		VALUES (?, ?, ?, 'project-hai', 'document', 'source text', 'summary', '[]', '[]', '[]', '[]', '[]', ?, 'Evidence fixture', ?, FALSE, FALSE, FALSE, ?, ?)`,
		extractionID, sourceID, rawItemID, uri, hash, now, now,
	).Error; err != nil {
		t.Fatalf("insert extraction: %v", err)
	}

	repository := NewGormRepository(tx)
	snapshot, err := repository.Resolve(t.Context(), owner, extractionID.String())
	if err != nil {
		t.Fatalf("Resolve exact fixture: %v", err)
	}
	if snapshot.OwnerIdentity != owner || snapshot.ExtractionID != extractionID.String() ||
		snapshot.SourceID != sourceID.String() || snapshot.RawItemID != rawItemID.String() {
		t.Fatalf("Resolve identity mismatch: %+v", snapshot)
	}
	if snapshot.SnapshotDigest == "" || snapshot.SnapshotDigest != SnapshotDigest(snapshot) {
		t.Fatalf("Resolve snapshot digest = %q, want recomputable digest", snapshot.SnapshotDigest)
	}
	claim := validClaim(snapshot)
	claim.MaxAgeSeconds = 7200
	if err := VerifyClaim(snapshot, claim, owner, now); err != nil {
		t.Fatalf("VerifyClaim resolved fixture: %v", err)
	}
	if !lowerSHA256.MatchString(snapshot.RawItemPayloadDigest) {
		t.Fatalf("Resolve raw payload digest = %q, want SHA-256", snapshot.RawItemPayloadDigest)
	}
	if _, err := repository.Resolve(t.Context(), "foreign-owner", extractionID.String()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Resolve error = %v, want ErrNotFound", err)
	}

	for _, test := range []struct {
		name        string
		mutate      string
		args        []any
		restore     string
		restoreArgs []any
	}{
		{
			name:   "disabled source",
			mutate: "UPDATE public.connected_sources SET enabled = FALSE WHERE id = ?", args: []any{sourceID},
			restore: "UPDATE public.connected_sources SET enabled = TRUE WHERE id = ?", restoreArgs: []any{sourceID},
		},
		{
			name:   "revoked source",
			mutate: "UPDATE public.connected_sources SET revoked_at = ? WHERE id = ?", args: []any{now, sourceID},
			restore: "UPDATE public.connected_sources SET revoked_at = NULL WHERE id = ?", restoreArgs: []any{sourceID},
		},
		{
			name:   "ownerless source",
			mutate: "UPDATE public.connected_sources SET owner_identity = '' WHERE id = ?", args: []any{sourceID},
			restore: "UPDATE public.connected_sources SET owner_identity = ? WHERE id = ?", restoreArgs: []any{owner, sourceID},
		},
		{
			name:   "uncertain extraction",
			mutate: "UPDATE public.source_extractions SET uncertain = TRUE WHERE id = ?", args: []any{extractionID},
			restore: "UPDATE public.source_extractions SET uncertain = FALSE WHERE id = ?", restoreArgs: []any{extractionID},
		},
		{
			name:   "archived extraction",
			mutate: "UPDATE public.source_extractions SET archived = TRUE WHERE id = ?", args: []any{extractionID},
			restore: "UPDATE public.source_extractions SET archived = FALSE WHERE id = ?", restoreArgs: []any{extractionID},
		},
		{
			name:   "raw project mismatch",
			mutate: "UPDATE public.source_raw_items SET project_key = 'other-project' WHERE id = ?", args: []any{rawItemID},
			restore: "UPDATE public.source_raw_items SET project_key = 'project-hai' WHERE id = ?", restoreArgs: []any{rawItemID},
		},
		{
			name:   "raw URI mismatch",
			mutate: "UPDATE public.source_raw_items SET source_uri = 'local://changed' WHERE id = ?", args: []any{rawItemID},
			restore: "UPDATE public.source_raw_items SET source_uri = ? WHERE id = ?", restoreArgs: []any{uri, rawItemID},
		},
		{
			name:   "raw hash mismatch",
			mutate: "UPDATE public.source_raw_items SET content_hash = ? WHERE id = ?", args: []any{strings.Repeat("b", 64), rawItemID},
			restore: "UPDATE public.source_raw_items SET content_hash = ? WHERE id = ?", restoreArgs: []any{hash, rawItemID},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := tx.Exec(test.mutate, test.args...).Error; err != nil {
				t.Fatalf("mutate fixture: %v", err)
			}
			if _, err := repository.Resolve(t.Context(), owner, extractionID.String()); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Resolve error = %v, want ErrNotFound", err)
			}
			if err := tx.Exec(test.restore, test.restoreArgs...).Error; err != nil {
				t.Fatalf("restore fixture: %v", err)
			}
		})
	}

	if err := tx.Exec("UPDATE public.source_raw_items SET content = ? WHERE id = ?", "changed source text", rawItemID).Error; err != nil {
		t.Fatalf("mutate raw source without updating the legacy fingerprint: %v", err)
	}
	changedRawSnapshot, err := repository.Resolve(t.Context(), owner, extractionID.String())
	if err != nil {
		t.Fatalf("Resolve source after raw content change: %v", err)
	}
	if changedRawSnapshot.RawItemPayloadDigest == snapshot.RawItemPayloadDigest ||
		changedRawSnapshot.SnapshotDigest == snapshot.SnapshotDigest {
		t.Fatal("raw source content change did not invalidate its evidence snapshot")
	}
	if err := VerifyClaim(changedRawSnapshot, claim, owner, now); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("VerifyClaim accepted the old claim after raw content changed: %v", err)
	}
	if err := tx.Exec("UPDATE public.source_raw_items SET content = ? WHERE id = ?", "source text", rawItemID).Error; err != nil {
		t.Fatalf("restore raw source content: %v", err)
	}

	if err := tx.Exec(
		"UPDATE public.connected_sources SET last_synced_at = ? WHERE id = ?",
		now.Add(24*time.Hour), sourceID,
	).Error; err != nil {
		t.Fatalf("update source sync timestamp: %v", err)
	}
	resolvedAfterSync, err := repository.Resolve(t.Context(), owner, extractionID.String())
	if err != nil {
		t.Fatalf("Resolve after unrelated source sync: %v", err)
	}
	staleClaim := validClaim(resolvedAfterSync)
	staleClaim.MaxAgeSeconds = 30 * 60
	if err := VerifyClaim(resolvedAfterSync, staleClaim, owner, now); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("source-wide sync refreshed stale raw item: %v", err)
	}
}

func validSnapshot() Snapshot {
	fetchedAt := time.Date(2026, time.August, 4, 11, 30, 0, 0, time.UTC)
	return Snapshot{
		OwnerIdentity:           "owner@example.com",
		ExtractionID:            "11111111-1111-4111-8111-111111111111",
		SourceID:                "22222222-2222-4222-8222-222222222222",
		RawItemID:               "33333333-3333-4333-8333-333333333333",
		ProjectKey:              "project-hai",
		RawProjectKey:           "project-hai",
		ExtractionURI:           "local://source/evidence",
		RawItemURI:              "local://source/evidence",
		ExtractionHash:          strings.Repeat("a", 64),
		RawItemHash:             strings.Repeat("a", 64),
		RawItemPayloadDigest:    strings.Repeat("b", 64),
		ExtractionPayloadDigest: strings.Repeat("c", 64),
		FetchedAt:               fetchedAt,
		ExtractionAt:            fetchedAt.Add(10 * time.Minute),
		Sensitive:               true,
		LocalOnly:               true,
		ConnectorKey:            "local-folder",
	}
}

func validClaim(snapshot Snapshot) Claim {
	return Claim{
		RequirementID:  "source-requirement-1",
		Validator:      ValidatorFreshSource,
		ExtractionID:   snapshot.ExtractionID,
		SourceID:       snapshot.SourceID,
		RawItemID:      snapshot.RawItemID,
		SnapshotDigest: snapshot.SnapshotDigest,
		MaxAgeSeconds:  3600,
	}
}

func openSourceEvidencePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping source evidence Postgres test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return db
}

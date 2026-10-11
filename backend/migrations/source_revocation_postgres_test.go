package migrations_test

import (
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

func TestConnectedSourceRevocationIsTerminalInPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := source.NewGormRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	connected, err := repo.CreateSource(&models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "gmail", Name: "Alice Gmail",
		Category: "email", Enabled: true, Status: "active", LocalOnly: false,
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: connected.ID, Provider: "google",
		AccessToken: []byte("encrypted-access"), RefreshToken: []byte("encrypted-refresh"),
	}); err != nil {
		t.Fatalf("SaveOAuthToken: %v", err)
	}
	stale, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource stale snapshot: %v", err)
	}
	revoked, err := repo.RevokeSource(stale, "alice", now)
	if err != nil {
		t.Fatalf("RevokeSource: %v", err)
	}
	if revoked.Status != "revoked" || revoked.Enabled || revoked.RevokedAt == nil {
		t.Fatalf("revoked source = %#v", revoked)
	}

	stale.Enabled = true
	stale.Status = "active"
	if _, err := repo.UpdateSource(stale); !errors.Is(err, source.ErrSourceRevoked) {
		t.Fatalf("stale UpdateSource error = %v, want ErrSourceRevoked", err)
	}
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: connected.ID, Provider: "google", AccessToken: []byte("new-token"),
	}); !errors.Is(err, source.ErrSourceRevoked) {
		t.Fatalf("post-revocation SaveOAuthToken error = %v, want ErrSourceRevoked", err)
	}

	persisted, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource after rejected writes: %v", err)
	}
	if persisted.Status != "revoked" || persisted.Enabled || persisted.RevokedAt == nil {
		t.Fatalf("revocation was overwritten: %#v", persisted)
	}
	var tokenCount int64
	if err := db.Model(&models.SourceOAuthToken{}).Where("source_id = ?", connected.ID).Count(&tokenCount).Error; err != nil {
		t.Fatalf("count OAuth tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("OAuth token count after revocation = %d, want 0", tokenCount)
	}
}

func TestConnectedSourceUpdatePreservesIdentityAndRejectsStaleWritesInPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := source.NewGormRepository(db)
	connected, err := repo.CreateSource(&models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Notes",
		Category: "local_folder", Enabled: true, Status: "active", LocalOnly: true,
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	first, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource first: %v", err)
	}
	stale, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource stale: %v", err)
	}
	first.Name = "Updated notes"
	first.OwnerIdentity = "mallory"
	first.ConnectorKey = "gmail"
	updated, err := repo.UpdateSource(first)
	if err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if updated.OwnerIdentity != "alice" || updated.ConnectorKey != "local-folder" || updated.Name != "Updated notes" {
		t.Fatalf("repository invariants were not preserved: %#v", updated)
	}
	stale.Name = "Stale overwrite"
	if _, err := repo.UpdateSource(stale); !errors.Is(err, source.ErrSourceChanged) {
		t.Fatalf("stale UpdateSource error = %v, want ErrSourceChanged", err)
	}
	persisted, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource persisted: %v", err)
	}
	if persisted.Name != "Updated notes" || persisted.OwnerIdentity != "alice" || persisted.ConnectorKey != "local-folder" {
		t.Fatalf("stale update changed source: %#v", persisted)
	}
}

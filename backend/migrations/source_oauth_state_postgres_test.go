//go:build integration

package migrations_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

func TestSourceOAuthStateIsDurableOwnerBoundSingleUseAndRevokedWithSource(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := source.NewGormRepository(db)
	connected, err := repo.CreateSource(&models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "gmail", Name: "Alice Gmail",
		Category: "email", Enabled: true, Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	firstDigest := strings.Repeat("a", 64)
	if err := repo.SaveOAuthState(&models.SourceOAuthState{
		SourceID: connected.ID, OwnerIdentity: "alice", StateDigest: firstDigest,
		ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveOAuthState: %v", err)
	}
	if err := repo.ConsumeOAuthState(connected.ID, "bob", firstDigest, now); !errors.Is(err, source.ErrOAuthStateInvalid) {
		t.Fatalf("foreign ConsumeOAuthState error = %v, want ErrOAuthStateInvalid", err)
	}
	if err := repo.ConsumeOAuthState(connected.ID, "alice", firstDigest, now); err != nil {
		t.Fatalf("owner ConsumeOAuthState: %v", err)
	}
	var consumed models.SourceOAuthState
	if err := db.First(&consumed, "source_id = ?", connected.ID).Error; err != nil {
		t.Fatalf("read consumed OAuth state: %v", err)
	}
	if consumed.ConsumedAt == nil {
		t.Fatal("consumed OAuth state has no consumed_at timestamp")
	}
	if consumed.ConsumedAt.Before(consumed.CreatedAt) {
		t.Fatalf("consumed_at %s is before created_at %s", consumed.ConsumedAt, consumed.CreatedAt)
	}
	if err := repo.ConsumeOAuthState(connected.ID, "alice", firstDigest, now); !errors.Is(err, source.ErrOAuthStateInvalid) {
		t.Fatalf("replayed ConsumeOAuthState error = %v, want ErrOAuthStateInvalid", err)
	}

	// Issuing again replaces the consumed record rather than growing an
	// unbounded state history.
	secondDigest := strings.Repeat("b", 64)
	if err := repo.SaveOAuthState(&models.SourceOAuthState{
		SourceID: connected.ID, OwnerIdentity: "alice", StateDigest: secondDigest,
		ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("replace OAuth state: %v", err)
	}
	var stateCount int64
	if err := db.Model(&models.SourceOAuthState{}).Where("source_id = ?", connected.ID).Count(&stateCount).Error; err != nil {
		t.Fatalf("count OAuth states: %v", err)
	}
	if stateCount != 1 {
		t.Fatalf("OAuth state count = %d, want 1", stateCount)
	}

	expected, err := repo.FindSource(connected.ID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if _, err := repo.RevokeSource(expected, "alice", now); err != nil {
		t.Fatalf("RevokeSource: %v", err)
	}
	if err := db.Model(&models.SourceOAuthState{}).Where("source_id = ?", connected.ID).Count(&stateCount).Error; err != nil {
		t.Fatalf("count revoked OAuth states: %v", err)
	}
	if stateCount != 0 {
		t.Fatalf("OAuth state count after revocation = %d, want 0", stateCount)
	}
}

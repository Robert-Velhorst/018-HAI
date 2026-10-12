package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestGoogleOAuthAtomicSaveRejectsInactiveSourcePostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	ids := make([]uuid.UUID, 0, 2)
	t.Cleanup(func() {
		if len(ids) == 0 {
			return
		}
		_ = db.Where("source_id IN ?", ids).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id IN ?", ids).Delete(&models.ConnectedSource{}).Error
	})

	fixtures := []models.ConnectedSource{
		{ID: uuid.New(), OwnerIdentity: "paused-oauth@example.test", ConnectorKey: gmailConnectorKey, Name: "Paused source", Category: "email", Enabled: true, Status: "paused"},
		{ID: uuid.New(), OwnerIdentity: "disabled-oauth@example.test", ConnectorKey: gmailConnectorKey, Name: "Disabled source", Category: "email", Enabled: false, Status: "active"},
	}
	for _, fixture := range fixtures {
		requestedEnabled := fixture.Enabled
		if err := db.Create(&fixture).Error; err != nil {
			t.Fatalf("create inactive source fixture: %v", err)
		}
		ids = append(ids, fixture.ID)
		if !requestedEnabled {
			if err := db.Model(&models.ConnectedSource{}).Where("id = ?", fixture.ID).UpdateColumn("enabled", false).Error; err != nil {
				t.Fatalf("disable source fixture: %v", err)
			}
		}
		_, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
			SourceID: fixture.ID, Provider: googleProvider, AccessToken: []byte("encrypted-access"),
			RefreshToken: []byte("encrypted-refresh"), Scope: googleoauth.GmailReadonlyScope,
		}, fixture.OwnerIdentity, gmailConnectorKey)
		if !errors.Is(err, errGoogleOAuthSourceInactive) {
			t.Fatalf("source status %q enabled=%t save error = %v, want inactive-source rejection", fixture.Status, requestedEnabled, err)
		}
		if _, err := repository.FindOAuthToken(fixture.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("inactive source status %q enabled=%t persisted an OAuth token: %v", fixture.Status, requestedEnabled, err)
		}
	}
}

func TestGoogleOAuthAtomicSaveBindsOwnerConnectorAndRevokePostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "binding-oauth@example.test", ConnectorKey: gmailConnectorKey,
		Name: "OAuth binding fixture", Category: "email", Enabled: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create OAuth binding fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("source_id = ?", source.ID).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error
	})
	newToken := func() *models.SourceOAuthToken {
		return &models.SourceOAuthToken{
			SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("encrypted-access"),
			RefreshToken: []byte("encrypted-refresh"), Scope: googleoauth.GmailReadonlyScope,
		}
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), newToken(), "other-owner@example.test", gmailConnectorKey); !errors.Is(err, errGoogleOAuthSourceBindingChanged) {
		t.Fatalf("wrong owner save error = %v, want source-binding rejection", err)
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), newToken(), source.OwnerIdentity, driveConnectorKey); !errors.Is(err, errGoogleOAuthSourceBindingChanged) {
		t.Fatalf("wrong connector save error = %v, want source-binding rejection", err)
	}
	if _, err := repository.FindOAuthToken(source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("mismatched source binding persisted an OAuth token: %v", err)
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), newToken(), source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save correctly bound OAuth token: %v", err)
	}
	expected, err := repository.FindSource(source.ID)
	if err != nil {
		t.Fatalf("reload source before revocation: %v", err)
	}
	if _, err := repository.RevokeSource(expected, source.OwnerIdentity, time.Now().UTC()); err != nil {
		t.Fatalf("revoke source after token save: %v", err)
	}
	if _, err := repository.FindOAuthToken(source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("source revocation did not remove the saved OAuth token: %v", err)
	}
}

func TestGoogleOAuthRefreshFailureCannotMarkReplacementTokenStalePostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "refresh-race-oauth@example.test", ConnectorKey: gmailConnectorKey,
		Name: "OAuth refresh race fixture", Category: "email", Enabled: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create refresh-race source: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("source_id = ?", source.ID).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error
	})

	oldToken := &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("old-access"),
		RefreshToken: []byte("old-refresh"), Scope: googleoauth.GmailReadonlyScope,
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), oldToken, source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save old OAuth token: %v", err)
	}
	failedSnapshot, err := repository.FindOAuthToken(source.ID)
	if err != nil {
		t.Fatalf("load old OAuth token snapshot: %v", err)
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("new-access"),
		RefreshToken: []byte("new-refresh"), Scope: googleoauth.GmailReadonlyScope,
	}, source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save replacement OAuth token: %v", err)
	}

	changed, err := repository.SetGoogleOAuthReconnectRequiredForToken(context.Background(), failedSnapshot)
	if err != nil || changed {
		t.Fatalf("stale refresh failure changed=%v err=%v, want no-op", changed, err)
	}
	current, err := repository.FindOAuthToken(source.ID)
	if err != nil || string(current.RefreshToken) != "new-refresh" {
		t.Fatalf("replacement OAuth token = %#v, err=%v", current, err)
	}
	storedSource, err := repository.FindSource(source.ID)
	if err != nil || storedSource.Status != "active" {
		t.Fatalf("source status after stale refresh failure = %#v, err=%v; want active", storedSource, err)
	}

	changed, err = repository.SetGoogleOAuthReconnectRequiredForToken(context.Background(), current)
	if err != nil || !changed {
		t.Fatalf("current-token refresh failure changed=%v err=%v, want reconnect-required", changed, err)
	}
}

func TestGoogleOAuthRefreshSaveRejectsReplacedGrantPostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "refresh-save-race@example.test", ConnectorKey: gmailConnectorKey,
		Name: "OAuth refresh save race fixture", Category: "email", Enabled: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create refresh-save source: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("source_id = ?", source.ID).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error
	})

	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("first-access"),
		RefreshToken: []byte("first-refresh"), Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
	}, source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save original grant: %v", err)
	}
	staleSnapshot, err := repository.FindOAuthToken(source.ID)
	if err != nil {
		t.Fatalf("load original grant snapshot: %v", err)
	}
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("reconnected-access"),
		RefreshToken: []byte("reconnected-refresh"), Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(2 * time.Hour),
	}, source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save same-owner reconnect grant: %v", err)
	}

	err = repository.SaveGoogleOAuthRefreshTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("late-refresh-access"),
		RefreshToken: []byte("first-refresh"), Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(3 * time.Hour),
	}, source.OwnerIdentity, gmailConnectorKey, staleSnapshot)
	if !errors.Is(err, errGoogleOAuthTokenChanged) {
		t.Fatalf("stale refresh save error = %v, want grant-changed conflict", err)
	}
	current, err := repository.FindOAuthToken(source.ID)
	if err != nil || string(current.AccessToken) != "reconnected-access" || string(current.RefreshToken) != "reconnected-refresh" {
		t.Fatalf("current grant after stale refresh = %#v, err=%v; want reconnect grant unchanged", current, err)
	}

	currentSnapshot := current
	if err := repository.SaveGoogleOAuthRefreshTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("current-refresh-access"),
		RefreshToken: []byte("reconnected-refresh"), Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(4 * time.Hour),
	}, source.OwnerIdentity, gmailConnectorKey, currentSnapshot); err != nil {
		t.Fatalf("save refresh against current grant: %v", err)
	}
	current, err = repository.FindOAuthToken(source.ID)
	if err != nil || string(current.AccessToken) != "current-refresh-access" || string(current.RefreshToken) != "reconnected-refresh" {
		t.Fatalf("current grant after valid refresh = %#v, err=%v", current, err)
	}
}

func TestSourceUpdateCannotRebindOrReviveRevokedOAuthSourcePostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "immutable-binding@example.test", ConnectorKey: gmailConnectorKey,
		Name: "OAuth immutable binding fixture", Category: "email", Enabled: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create immutable-binding source: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("source_id = ?", source.ID).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error
	})
	if _, err := repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
		SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("encrypted-access"),
		RefreshToken: []byte("encrypted-refresh"), Scope: googleoauth.GmailReadonlyScope,
	}, source.OwnerIdentity, gmailConnectorKey); err != nil {
		t.Fatalf("save bound OAuth token: %v", err)
	}

	original, err := repository.FindSource(source.ID)
	if err != nil {
		t.Fatalf("load immutable-binding source: %v", err)
	}
	wrongOwner := *original
	wrongOwner.OwnerIdentity = "different-owner@example.test"
	if _, err := repository.UpdateSource(&wrongOwner); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("owner rebind error = %v, want immutable-binding rejection", err)
	}
	wrongConnector := *original
	wrongConnector.ConnectorKey = driveConnectorKey
	if _, err := repository.UpdateSource(&wrongConnector); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("connector rebind error = %v, want immutable-binding rejection", err)
	}
	stale := *original
	if _, err := repository.RevokeSource(original, source.OwnerIdentity, time.Now().UTC()); err != nil {
		t.Fatalf("revoke OAuth source: %v", err)
	}
	stale.Enabled = true
	stale.Status = "active"
	stale.RevokedAt = nil
	if _, err := repository.UpdateSource(&stale); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale update after revoke error = %v, want stale-write rejection", err)
	}
	stored, err := repository.FindSource(source.ID)
	if err != nil || stored.Status != "revoked" || stored.RevokedAt == nil {
		t.Fatalf("source after stale update = %#v, err=%v; want revoked", stored, err)
	}
	if _, err := repository.FindOAuthToken(source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("source update race restored a revoked OAuth token: %v", err)
	}
}

func TestSourceUpdateCASAppliesMutableFieldsAndRejectsStaleSnapshotPostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "source-update-cas@example.test", ConnectorKey: gmailConnectorKey,
		Name: "Before update", Category: "email", Enabled: true, LocalOnly: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source update fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error })

	firstSnapshot, err := repository.FindSource(source.ID)
	if err != nil {
		t.Fatalf("load source update snapshot: %v", err)
	}
	firstUpdate := *firstSnapshot
	firstUpdate.Name = "Paused by current update"
	firstUpdate.Enabled = false
	firstUpdate.Status = "paused"
	updated, err := repository.UpdateSource(&firstUpdate)
	if err != nil {
		t.Fatalf("apply current source update: %v", err)
	}
	if updated.Name != "Paused by current update" || updated.Enabled || updated.Status != "paused" {
		t.Fatalf("updated source = %#v, want new name and disabled paused state", updated)
	}
	if !updated.UpdatedAt.After(firstSnapshot.UpdatedAt) {
		t.Fatalf("updated timestamp %s did not advance beyond snapshot %s", updated.UpdatedAt, firstSnapshot.UpdatedAt)
	}

	staleUpdate := *firstSnapshot
	staleUpdate.Name = "Stale update must not win"
	staleUpdate.Enabled = true
	staleUpdate.Status = "active"
	if _, err := repository.UpdateSource(&staleUpdate); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale source update error = %v, want version conflict", err)
	}
	stored, err := repository.FindSource(source.ID)
	if err != nil || stored.Name != "Paused by current update" || stored.Enabled || stored.Status != "paused" {
		t.Fatalf("source after stale update = %#v, err=%v; want latest paused state", stored, err)
	}
}

func TestGoogleOAuthSaveAndRevokeSerializePostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)

	repository := &GormRepository{DB: db}
	ids := make([]uuid.UUID, 0, 16)
	t.Cleanup(func() {
		if len(ids) == 0 {
			return
		}
		_ = db.Where("source_id IN ?", ids).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id IN ?", ids).Delete(&models.ConnectedSource{}).Error
	})

	for iteration := 0; iteration < 16; iteration++ {
		source := models.ConnectedSource{
			ID: uuid.New(), OwnerIdentity: fmt.Sprintf("oauth-race-%d@example.test", iteration),
			ConnectorKey: gmailConnectorKey, Name: "OAuth concurrency fixture", Category: "email",
			Enabled: true, Status: "active", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := db.Create(&source).Error; err != nil {
			t.Fatalf("create source fixture: %v", err)
		}
		ids = append(ids, source.ID)
		expected, err := repository.FindSource(source.ID)
		if err != nil {
			t.Fatalf("load source fixture: %v", err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var saveErr, revokeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, saveErr = repository.SaveGoogleOAuthTokenForSource(context.Background(), &models.SourceOAuthToken{
				SourceID: source.ID, Provider: googleProvider,
				AccessToken: []byte("encrypted-access"), RefreshToken: []byte("encrypted-refresh"),
				Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
			}, source.OwnerIdentity, gmailConnectorKey)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, revokeErr = repository.RevokeSource(expected, source.OwnerIdentity, time.Now().UTC())
		}()
		close(start)
		wg.Wait()

		if revokeErr != nil {
			t.Fatalf("revoke source in iteration %d: %v", iteration, revokeErr)
		}
		if saveErr != nil && !errors.Is(saveErr, errGoogleOAuthSourceBindingChanged) && !errors.Is(saveErr, ErrSourceRevoked) {
			t.Fatalf("OAuth save in iteration %d returned unexpected error: %v", iteration, saveErr)
		}

		if _, err := repository.FindOAuthToken(source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("iteration %d left an OAuth token after revocation: err=%v", iteration, err)
		}
		stored, err := repository.FindSource(source.ID)
		if err != nil || stored.RevokedAt == nil || stored.Status != "revoked" {
			t.Fatalf("iteration %d source state = %#v, err=%v; want revoked", iteration, stored, err)
		}
	}
}

func TestGoogleOAuthSaveWaitsForSourceLockAndRejectsRevocationPostgres(t *testing.T) {
	db := openGoogleOAuthAtomicPostgresTestDB(t)
	repository := &GormRepository{DB: db}
	source := models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "locked-revoke-oauth@example.test", ConnectorKey: gmailConnectorKey,
		Name: "OAuth lock fixture", Category: "email", Enabled: true, Status: "active",
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source lock fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("source_id = ?", source.ID).Delete(&models.SourceOAuthToken{}).Error
		_ = db.Where("id = ?", source.ID).Delete(&models.ConnectedSource{}).Error
	})

	transaction := db.Begin()
	if transaction.Error != nil {
		t.Fatalf("begin source lock transaction: %v", transaction.Error)
	}
	defer transaction.Rollback()
	var locked models.ConnectedSource
	if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", source.ID).Error; err != nil {
		t.Fatalf("lock source fixture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	saveDone := make(chan error, 1)
	go func() {
		_, err := repository.SaveGoogleOAuthTokenForSource(ctx, &models.SourceOAuthToken{
			SourceID: source.ID, Provider: googleProvider, AccessToken: []byte("encrypted-access"),
			RefreshToken: []byte("encrypted-refresh"), Scope: googleoauth.GmailReadonlyScope,
		}, source.OwnerIdentity, gmailConnectorKey)
		saveDone <- err
	}()

	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.Raw(`SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
			AND query ILIKE '%connected_sources%'
		)`).Scan(&waiting).Error; err != nil {
			_ = transaction.Rollback()
			<-saveDone
			t.Fatalf("inspect isolated PostgreSQL lock wait: %v", err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		_ = transaction.Rollback()
		select {
		case err := <-saveDone:
			t.Fatalf("OAuth save did not wait on the locked source row (completed with %v)", err)
		case <-time.After(6 * time.Second):
			t.Fatal("OAuth save remained blocked after the source lock was released")
		}
	}

	revokedAt := time.Now().UTC()
	if err := transaction.Model(&models.ConnectedSource{}).Where("id = ?", source.ID).
		Updates(map[string]any{"enabled": false, "status": "revoked", "revoked_at": revokedAt}).Error; err != nil {
		_ = transaction.Rollback()
		<-saveDone
		t.Fatalf("mark source revoked while holding its row lock: %v", err)
	}
	if err := transaction.Commit().Error; err != nil {
		<-saveDone
		t.Fatalf("commit locked source revocation: %v", err)
	}
	select {
	case err := <-saveDone:
		if !errors.Is(err, ErrSourceRevoked) {
			t.Fatalf("OAuth save after revocation returned %v, want ErrSourceRevoked", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OAuth save did not finish after source lock release")
	}
	if _, err := repository.FindOAuthToken(source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("OAuth save created a token after revocation: %v", err)
	}
}

func openGoogleOAuthAtomicPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Google OAuth Postgres concurrency test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for Google OAuth Postgres tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("configured HAI_TEST_DATABASE_DSN is unavailable: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Google OAuth Postgres test connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read Google OAuth test database name: %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(databaseName)), "_test") {
		t.Skipf("refusing Google OAuth test database %q; database name must end in _test", databaseName)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatalf("prepare UUID support in isolated Google OAuth test database: %v", err)
	}
	var missing []any
	for _, model := range []any{&models.ConnectedSource{}, &models.SourceOAuthToken{}} {
		if !db.Migrator().HasTable(model) {
			missing = append(missing, model)
		}
	}
	if len(missing) > 0 {
		if err := db.AutoMigrate(missing...); err != nil {
			t.Fatalf("create missing test-only Google OAuth tables: %v", err)
		}
	}
	return db
}

package migrations_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type retentionGatewaySpy struct {
	calls   int
	content *agentruntime.GatewayArtifactContent
	err     error
}

type artifactRetentionValidationScanLogger struct {
	logger.Interface
	mu    sync.Mutex
	scans int
}

func (l *artifactRetentionValidationScanLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rowsAffected := fc()
	normalizedSQL := strings.ToLower(strings.ReplaceAll(sql, `"`, ""))
	normalizedSQL = strings.Join(strings.Fields(normalizedSQL), " ")
	if strings.HasPrefix(normalizedSQL, "select owner_identity, execution_reference, artifact_digest, content_sha256, size_bytes, source_event_id, encrypted_content from openclaw_retained_artifacts") {
		l.mu.Lock()
		l.scans++
		l.mu.Unlock()
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rowsAffected }, err)
}

func (l *artifactRetentionValidationScanLogger) scanCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scans
}

func openArtifactRetentionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("DB_AUTOMIGRATE", "true")
	t.Setenv("HAI_SEMANTIC_RETRIEVAL_ENABLED", "false")
	db := openIsolatedMigrationDatabase(t)
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply ordered production pre-migrations: %v", err)
	}
	assertArtifactRetentionLaunchEventColumns := func(stage string) {
		t.Helper()
		var migratedColumns int64
		if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'automation_launch_events'
		AND column_name IN ('execution_reference', 'event_key')`).Scan(&migratedColumns).Error; err != nil {
			t.Fatalf("inspect launch event columns %s versioned migration: %v", stage, err)
		}
		if migratedColumns != 2 {
			t.Fatalf("%s versioned migrations did not create both launch event columns: %d", stage, migratedColumns)
		}
	}
	assertArtifactRetentionLaunchEventColumns("before")
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("run production migration sequence (pre migrations, development AutoMigrate, post migrations): %v", err)
	}
	assertArtifactRetentionLaunchEventColumns("after")
	var retentionMigrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", "pre/0080_openclaw_artifact_retention").Count(&retentionMigrationCount).Error; err != nil || retentionMigrationCount != 1 {
		t.Fatalf("ordered production migration sequence did not record artifact retention migration: %d %v", retentionMigrationCount, err)
	}
	return db
}

func artifactRetentionTestKey(material string) string {
	hash := sha256.Sum256([]byte(material))
	return hex.EncodeToString(hash[:])
}

func historicalArtifactRetentionCiphertext(keyMaterial, owner, executionReference, digest, contentSHA string, data []byte) ([]byte, error) {
	key := sha256.Sum256([]byte("hai-openclaw-retention-v1\x00" + keyMaterial))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad, err := json.Marshal([]string{"hai-openclaw-retention-v1", owner, executionReference, digest, contentSHA})
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), nonce...), aead.Seal(nil, nonce, data, aad)...), nil
}

func seedArtifactRetentionQuotaRows(tx *gorm.DB, executionReference string, sourceEventID uuid.UUID, count, size int) error {
	owner := "alice"
	keyMaterial := artifactRetentionTestKey("retention-test-key-not-production")
	data := make([]byte, size)
	contentHash := sha256.Sum256(data)
	contentSHA := hex.EncodeToString(contentHash[:])
	for index := 1; index <= count; index++ {
		digestPart := md5.Sum([]byte(strconv.Itoa(index)))
		digest := hex.EncodeToString(digestPart[:]) + hex.EncodeToString(digestPart[:])
		ciphertext, err := historicalArtifactRetentionCiphertext(keyMaterial, owner, executionReference, digest, contentSHA, data)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO openclaw_retained_artifacts
			(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, executionReference, digest, owner, sourceEventID, contentSHA, size, ciphertext).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *retentionGatewaySpy) DownloadOpenClawGatewayArtifact(context.Context, agentruntime.OpenClawGatewayReceipt, agentruntime.GatewayArtifactDescriptor) (*agentruntime.GatewayArtifactContent, error) {
	s.calls++
	return s.content, s.err
}

type legacyArtifactFixture struct {
	owner      string
	event      models.AutomationLaunchEvent
	repo       *openclawreconcile.Repository
	archive    *openclawreconcile.ArtifactArchive
	binding    openclawreconcile.ArtifactDownloadBinding
	content    *agentruntime.GatewayArtifactContent
	digest     string
	retainedAt time.Time
}

func seedLegacyArtifactFixture(t *testing.T, db *gorm.DB, keyMaterial string, content []byte) legacyArtifactFixture {
	t.Helper()
	owner := "legacy-read-" + uuid.NewString()
	ref := "ocgw:v2:" + uuid.NewString()
	taskID := "legacy-read-task-" + uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatalf("create legacy artifact receipt: %v", err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, LaunchType: "agent_runtime_openclaw_terminal", EventKey: uuid.NewString(), Target: "archive://openclaw/test", Status: "observed", StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create legacy artifact source event: %v", err)
	}
	digestHash := sha256.Sum256([]byte("legacy-artifact-reference:" + uuid.NewString()))
	digest := hex.EncodeToString(digestHash[:])
	size := int64(len(content))
	descriptor := agentruntime.GatewayArtifactDescriptor{Digest: digest, Type: "file", SizeBytes: &size}
	repo := openclawreconcile.NewRepository(db)
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
		t.Fatalf("create legacy artifact descriptor: %v", err)
	}
	binding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, digest)
	if err != nil {
		t.Fatalf("bind legacy artifact source event: %v", err)
	}
	contentHash := sha256.Sum256(content)
	contentSHA := hex.EncodeToString(contentHash[:])
	ciphertext, err := historicalArtifactRetentionCiphertext(keyMaterial, owner, ref, digest, contentSHA, content)
	if err != nil {
		t.Fatalf("encrypt legacy artifact fixture: %v", err)
	}
	retainedAt := now.Add(-time.Hour)
	if err := db.Exec(`INSERT INTO openclaw_retained_artifacts
		(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content, retained_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, ref, digest, owner, event.ID, contentSHA, size, ciphertext, retainedAt).Error; err != nil {
		t.Fatalf("insert legacy retained artifact: %v", err)
	}
	return legacyArtifactFixture{
		owner: owner, event: event, repo: repo,
		archive: openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, "random-v1:"+keyMaterial, true),
		binding: *binding,
		content: &agentruntime.GatewayArtifactContent{Data: append([]byte(nil), content...), ContentSHA256: contentSHA},
		digest:  digest, retainedAt: retainedAt,
	}
}

func legacyArtifactHTTPResponse(t *testing.T, fixture legacyArtifactFixture, gateway *retentionGatewaySpy) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := openclawreconcile.NewArtifactHandler(fixture.repo, gateway)
	handler.SetArchive(fixture.archive)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, fixture.owner) })
	engine.GET("/events/:eventId/artifacts/:digest", handler.Download)
	response := httptest.NewRecorder()
	path := "/events/" + fixture.event.ID.String() + "/artifacts/" + fixture.digest
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func legacyArtifactHTTPRetainResponse(t *testing.T, fixture legacyArtifactFixture, gateway *retentionGatewaySpy) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := openclawreconcile.NewArtifactHandler(fixture.repo, gateway)
	handler.SetArchive(fixture.archive)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, fixture.owner) })
	engine.POST("/events/:eventId/artifacts/:digest/retain", handler.Retain)
	response := httptest.NewRecorder()
	path := "/events/" + fixture.event.ID.String() + "/artifacts/" + fixture.digest + "/retain"
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
	return response
}

func retainedArtifactState(t *testing.T, db *gorm.DB, fixture legacyArtifactFixture) ([]byte, time.Time) {
	t.Helper()
	var row struct {
		EncryptedContent []byte
		RetainedAt       time.Time
	}
	if err := db.Table("openclaw_retained_artifacts").Select("encrypted_content, retained_at").
		Where("owner_identity = ? AND execution_reference = ? AND artifact_digest = ?", fixture.owner, fixture.binding.Receipt.ExecutionReference, fixture.digest).
		Take(&row).Error; err != nil {
		t.Fatalf("read retained artifact state: %v", err)
	}
	return row.EncryptedContent, row.RetainedAt
}

func countLegacyArtifactUpgradeEvents(t *testing.T, db *gorm.DB, fixture legacyArtifactFixture) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&models.AutomationLaunchEvent{}).
		Where("owner_identity = ? AND execution_reference = ? AND launch_type = ?", fixture.owner, fixture.binding.Receipt.ExecutionReference, "agent_runtime_openclaw_artifact_provenance_upgrade").
		Count(&count).Error; err != nil {
		t.Fatalf("count legacy artifact provenance upgrade events: %v", err)
	}
	return count
}

func TestOpenClawLegacyArtifactDownloadDoesNotUpgradeRetainedCopy(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	keyMaterial := artifactRetentionTestKey("legacy-read-upgrade-validation-key")

	t.Run("matching source can be downloaded without mutating retained state", func(t *testing.T) {
		fixture := seedLegacyArtifactFixture(t, db, keyMaterial, []byte("recoverable retained legacy bytes"))
		beforeCiphertext, beforeRetainedAt := retainedArtifactState(t, db, fixture)
		gateway := &retentionGatewaySpy{content: fixture.content}
		response := legacyArtifactHTTPResponse(t, fixture, gateway)
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), fixture.content.Data) || gateway.calls != 1 {
			t.Fatalf("legacy download did not validate and return the matching source: status=%d calls=%d body=%q", response.Code, gateway.calls, response.Body.Bytes())
		}
		afterCiphertext, afterRetainedAt := retainedArtifactState(t, db, fixture)
		if !bytes.Equal(beforeCiphertext, afterCiphertext) || !beforeRetainedAt.Equal(afterRetainedAt) {
			t.Fatal("download mutated the retained copy")
		}
		if opened, err := fixture.archive.Read(context.Background(), fixture.binding); opened != nil || !errors.Is(err, openclawreconcile.ErrArtifactRetentionLegacyProvenance) {
			t.Fatalf("download silently upgraded legacy provenance: %#v %v", opened, err)
		}
		if got := countLegacyArtifactUpgradeEvents(t, db, fixture); got != 0 {
			t.Fatalf("download wrote %d provenance-upgrade audit events", got)
		}
	})

	t.Run("explicit retain upgrades in place with audit history", func(t *testing.T) {
		fixture := seedLegacyArtifactFixture(t, db, keyMaterial, []byte("explicitly retained legacy bytes"))
		beforeCiphertext, beforeRetainedAt := retainedArtifactState(t, db, fixture)
		gateway := &retentionGatewaySpy{content: fixture.content}
		response := legacyArtifactHTTPRetainResponse(t, fixture, gateway)
		if response.Code != http.StatusOK || gateway.calls != 1 {
			t.Fatalf("explicit retain did not validate and upgrade the matching source: status=%d calls=%d body=%q", response.Code, gateway.calls, response.Body.Bytes())
		}
		afterCiphertext, afterRetainedAt := retainedArtifactState(t, db, fixture)
		if bytes.Equal(beforeCiphertext, afterCiphertext) || !beforeRetainedAt.Equal(afterRetainedAt) {
			t.Fatalf("explicit retain did not replace ciphertext in place while preserving retained history: ciphertextChanged=%t retainedAtBefore=%s retainedAtAfter=%s", !bytes.Equal(beforeCiphertext, afterCiphertext), beforeRetainedAt, afterRetainedAt)
		}
		if opened, err := fixture.archive.Read(context.Background(), fixture.binding); err != nil || !bytes.Equal(opened.Data, fixture.content.Data) {
			t.Fatalf("explicitly upgraded retained copy is not recoverable: %v", err)
		}
		if got := countLegacyArtifactUpgradeEvents(t, db, fixture); got != 1 {
			t.Fatalf("upgrade audit count=%d, want 1", got)
		}
		var audit models.AutomationLaunchEvent
		if err := db.Where("owner_identity = ? AND execution_reference = ? AND launch_type = ?", fixture.owner, fixture.binding.Receipt.ExecutionReference, "agent_runtime_openclaw_artifact_provenance_upgrade").Take(&audit).Error; err != nil {
			t.Fatalf("read provenance-upgrade audit event: %v", err)
		}
		wantLegacySource := "legacy_source_event_id=" + fixture.event.ID.String()
		wantSource := "source_event_id=" + fixture.event.ID.String()
		if !slices.Contains(audit.AuditEvents, wantLegacySource) || !slices.Contains(audit.AuditEvents, wantSource) {
			t.Fatalf("upgrade audit does not preserve the legacy and revalidated event references: %#v", audit.AuditEvents)
		}
	})

	t.Run("unavailable source leaves legacy copy untouched", func(t *testing.T) {
		fixture := seedLegacyArtifactFixture(t, db, keyMaterial, []byte("keep this legacy copy"))
		beforeCiphertext, beforeRetainedAt := retainedArtifactState(t, db, fixture)
		gateway := &retentionGatewaySpy{err: errors.New("source unavailable")}
		response := legacyArtifactHTTPResponse(t, fixture, gateway)
		if response.Code != http.StatusServiceUnavailable || gateway.calls != 1 || bytes.Contains(response.Body.Bytes(), fixture.content.Data) {
			t.Fatalf("unavailable source was not failed closed: status=%d calls=%d body=%q", response.Code, gateway.calls, response.Body.Bytes())
		}
		afterCiphertext, afterRetainedAt := retainedArtifactState(t, db, fixture)
		if !bytes.Equal(beforeCiphertext, afterCiphertext) || !beforeRetainedAt.Equal(afterRetainedAt) || countLegacyArtifactUpgradeEvents(t, db, fixture) != 0 {
			t.Fatal("unavailable source changed the retained copy or created a false upgrade event")
		}
		if opened, err := fixture.archive.Read(context.Background(), fixture.binding); opened != nil || !errors.Is(err, openclawreconcile.ErrArtifactRetentionLegacyProvenance) {
			t.Fatalf("unavailable source made the unverified legacy copy readable: %#v %v", opened, err)
		}
	})

	t.Run("changed source leaves legacy copy untouched", func(t *testing.T) {
		fixture := seedLegacyArtifactFixture(t, db, keyMaterial, []byte("expected legacy bytes"))
		beforeCiphertext, beforeRetainedAt := retainedArtifactState(t, db, fixture)
		changed := append([]byte(nil), fixture.content.Data...)
		changed[0] ^= 0x01
		changedHash := sha256.Sum256(changed)
		gateway := &retentionGatewaySpy{content: &agentruntime.GatewayArtifactContent{Data: changed, ContentSHA256: hex.EncodeToString(changedHash[:])}}
		response := legacyArtifactHTTPResponse(t, fixture, gateway)
		if response.Code != http.StatusConflict || gateway.calls != 1 || bytes.Equal(response.Body.Bytes(), changed) {
			t.Fatalf("changed source was not rejected: status=%d calls=%d body=%q", response.Code, gateway.calls, response.Body.Bytes())
		}
		afterCiphertext, afterRetainedAt := retainedArtifactState(t, db, fixture)
		if !bytes.Equal(beforeCiphertext, afterCiphertext) || !beforeRetainedAt.Equal(afterRetainedAt) || countLegacyArtifactUpgradeEvents(t, db, fixture) != 0 {
			t.Fatal("changed source replaced the retained copy or created a false upgrade event")
		}
	})

	t.Run("audit failure rolls back the ciphertext upgrade", func(t *testing.T) {
		fixture := seedLegacyArtifactFixture(t, db, keyMaterial, []byte("retain until audit succeeds"))
		beforeCiphertext, beforeRetainedAt := retainedArtifactState(t, db, fixture)
		if err := db.Exec(`ALTER TABLE automation_launch_events ADD CONSTRAINT reject_legacy_artifact_upgrade_audit CHECK (launch_type <> 'agent_runtime_openclaw_artifact_provenance_upgrade') NOT VALID`).Error; err != nil {
			t.Fatalf("install isolated audit-failure constraint: %v", err)
		}
		response := legacyArtifactHTTPRetainResponse(t, fixture, &retentionGatewaySpy{content: fixture.content})
		if err := db.Exec(`ALTER TABLE automation_launch_events DROP CONSTRAINT reject_legacy_artifact_upgrade_audit`).Error; err != nil {
			t.Fatalf("remove isolated audit-failure constraint: %v", err)
		}
		if response.Code != http.StatusServiceUnavailable || bytes.Contains(response.Body.Bytes(), fixture.content.Data) {
			t.Fatalf("failed upgrade was not withheld: status=%d body=%q", response.Code, response.Body.Bytes())
		}
		afterCiphertext, afterRetainedAt := retainedArtifactState(t, db, fixture)
		if !bytes.Equal(beforeCiphertext, afterCiphertext) || !beforeRetainedAt.Equal(afterRetainedAt) || countLegacyArtifactUpgradeEvents(t, db, fixture) != 0 {
			t.Fatal("failed audit transaction did not preserve the original retained copy")
		}
		if opened, err := fixture.archive.Read(context.Background(), fixture.binding); opened != nil || !errors.Is(err, openclawreconcile.ErrArtifactRetentionLegacyProvenance) {
			t.Fatalf("failed upgrade made legacy data readable without provenance: %#v %v", opened, err)
		}
	})
}

func TestOpenClawArtifactRetentionRollbackGuard(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	// Apply only through retention so the migration under test is the latest one;
	// later migrations include deliberate rollback refusals of their own.
	rollbackTestMigrations := migrationFilesThrough(t, "pre/0080_openclaw_artifact_retention")
	appliedCount, err := infra.ApplyMigrations(db, rollbackTestMigrations, "pre")
	if err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	t.Logf("applied %d pre migrations", appliedCount)
	if again, err := infra.ApplyMigrations(db, rollbackTestMigrations, "pre"); err != nil || again != 0 {
		t.Fatalf("reapply pre migrations: %d %v", again, err)
	}
	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	const owner = "rollback-owner"
	const taskID = "rollback-task"
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, "rollback-session", "rollback-run", now).Error; err != nil {
		t.Fatalf("create rollback-guard receipt: %v", err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, LaunchType: "agent_runtime_openclaw_test", EventKey: uuid.NewString(), Target: "archive://openclaw/test", Status: "observed", StartedAt: now, CompletedAt: now}
	if err := db.Exec(`INSERT INTO automation_launch_events
		(id, automation_id, owner_identity, runtime_type, runtime_task_id, execution_reference, launch_type, event_key, target, status, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.AutomationID, event.OwnerIdentity, event.RuntimeType, event.RuntimeTaskID, event.ExecutionReference, event.LaunchType, event.EventKey, event.Target, event.Status, event.StartedAt, event.CompletedAt).Error; err != nil {
		t.Fatalf("create rollback-guard source event: %v", err)
	}
	size := int64(1)
	descriptor := agentruntime.GatewayArtifactDescriptor{Digest: strings.Repeat("d", 64), Type: "file", SizeBytes: &size}
	repo := openclawreconcile.NewRepository(db)
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
		t.Fatalf("create rollback-guard descriptor: %v", err)
	}
	// Seed a structurally valid retained row. This test targets the down
	// migration guard; the full-chain test below exercises encrypted retain/read.
	ciphertext := bytes.Repeat([]byte{0xa5}, 29)
	contentSHA := strings.Repeat("a", 64)
	if err := db.Exec(`INSERT INTO openclaw_retained_artifacts
		(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content, retained_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, ref, descriptor.Digest, owner, event.ID, contentSHA, ciphertext, now).Error; err != nil {
		t.Fatalf("seed rollback-guard retained content: %v", err)
	}
	var encryptedBefore []byte
	if err := db.Raw("SELECT encrypted_content FROM openclaw_retained_artifacts WHERE artifact_digest = ?", descriptor.Digest).Row().Scan(&encryptedBefore); err != nil {
		t.Fatalf("read retained ciphertext before rollback: %v", err)
	}
	status, err := infra.Status(db, rollbackTestMigrations, "pre")
	if err != nil {
		t.Fatalf("read applied migration chain: %v", err)
	}
	retentionIndex := -1
	for i, version := range status.Applied {
		if version == "pre/0080_openclaw_artifact_retention" {
			retentionIndex = i
			break
		}
	}
	if retentionIndex < 0 {
		t.Fatal("artifact retention migration was not present in the applied chain")
	}
	for i := len(status.Applied) - 1; i > retentionIndex; i-- {
		version := status.Applied[i]
		if err := infra.RollbackMigration(db, rollbackTestMigrations, "pre", version); err != nil {
			t.Fatalf("rollback dependent migration %s: %v", version, err)
		}
	}
	err = infra.RollbackMigration(db, rollbackTestMigrations, "pre", "pre/0080_openclaw_artifact_retention")
	if err == nil || !strings.Contains(err.Error(), "explicit export and retention review") {
		t.Fatal("retained-content rollback guard did not reject automatic deletion")
	}
	var count int64
	if err := db.Table("openclaw_retained_artifacts").Where("artifact_digest = ?", descriptor.Digest).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retained content after refused rollback: %d %v", count, err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", "pre/0080_openclaw_artifact_retention").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retention migration ledger after refused rollback: %d %v", count, err)
	}
	var encryptedAfter []byte
	if err := db.Raw("SELECT encrypted_content FROM openclaw_retained_artifacts WHERE artifact_digest = ?", descriptor.Digest).Row().Scan(&encryptedAfter); err != nil {
		t.Fatalf("read retained ciphertext after refused rollback: %v", err)
	}
	if !bytes.Equal(encryptedAfter, encryptedBefore) {
		t.Fatal("retained ciphertext changed after refused rollback")
	}
}

func TestOpenClawArtifactRetentionPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	repo := openclawreconcile.NewRepository(db)
	archive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("retention-test-key-not-production"), true)
	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts (execution_reference,owner_identity,runtime_task_id,session_key,run_id,status,terminal_status,terminal_at) VALUES (?, 'alice','task','session','run','terminal','completed',?)`, ref, now).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: "alice", RuntimeType: "openclaw", RuntimeTaskID: "task", ExecutionReference: ref, EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	data := []byte("private retained result")
	size := int64(len(data))
	descriptor := agentruntime.GatewayArtifactDescriptor{Digest: strings.Repeat("a", 64), Type: "file", SizeBytes: &size}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	binding, err := repo.ArtifactForEvent(context.Background(), "alice", event.ID, descriptor.Digest)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	content := &agentruntime.GatewayArtifactContent{Data: data, ContentSHA256: hex.EncodeToString(hash[:])}
	for i := 0; i < 2; i++ {
		info, err := archive.Retain(context.Background(), *binding, content)
		if err != nil || info.Verification != "unverified" || info.SizeBytes != size {
			t.Fatalf("retain: %#v %v", info, err)
		}
	}
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := archive.Retain(context.Background(), *binding, content); err != nil {
				t.Errorf("concurrent retain: %v", err)
			}
		}()
	}
	workers.Wait()
	var count int64
	if err := db.Table("openclaw_retained_artifacts").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate retain: %d %v", count, err)
	}
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("launch_type = ?", "agent_runtime_openclaw_artifact_retain").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate audit: %d %v", count, err)
	}
	var encrypted []byte
	if err := db.Table("openclaw_retained_artifacts").Select("encrypted_content").Row().Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), string(data)) {
		t.Fatal("plaintext persisted")
	}
	fresh := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(openclawreconcile.NewRepository(db), artifactRetentionTestKey("retention-test-key-not-production"), true)
	read, err := fresh.Read(context.Background(), *binding)
	if err != nil || string(read.Data) != string(data) {
		t.Fatalf("restart read: %v", err)
	}
	gin.SetMode(gin.TestMode)
	handler := openclawreconcile.NewArtifactHandler(repo, nil)
	handler.SetArchive(fresh)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Params = gin.Params{{Key: "eventId", Value: event.ID.String()}, {Key: "digest", Value: descriptor.Digest}}
	})
	engine.POST("/retain", handler.Retain)
	engine.GET("/download", handler.Download)
	for _, method := range []string{"POST", "GET"} {
		path := "/retain"
		if method == "GET" {
			path = "/download"
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != 200 {
			t.Fatalf("offline archive HTTP %s: %d %s", method, w.Code, w.Body.String())
		}
		if method == "GET" && (w.Body.String() != string(data) || w.Header().Get("X-Content-SHA256") != content.ContentSHA256) {
			t.Fatal("offline attachment changed")
		}
	}
	missingKeyDownloader := &retentionGatewaySpy{content: &agentruntime.GatewayArtifactContent{
		Data: []byte("gateway version"), ContentSHA256: "",
	}}
	missingKeyHandler := openclawreconcile.NewArtifactHandler(repo, missingKeyDownloader)
	missingKeyHandler.SetArchive(openclawreconcile.NewArtifactArchive(repo, ""))
	missingKeyEngine := gin.New()
	missingKeyEngine.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Params = gin.Params{{Key: "eventId", Value: event.ID.String()}, {Key: "digest", Value: descriptor.Digest}}
	})
	missingKeyEngine.GET("/download", missingKeyHandler.Download)
	missingKeyEngine.GET("/view", missingKeyHandler.Get)
	missingKeyDownload := httptest.NewRecorder()
	missingKeyEngine.ServeHTTP(missingKeyDownload, httptest.NewRequest("GET", "/download", nil))
	if missingKeyDownload.Code != http.StatusServiceUnavailable || missingKeyDownloader.calls != 0 ||
		strings.Contains(missingKeyDownload.Body.String(), "gateway version") {
		t.Fatalf("missing retention key fell back to Gateway: status=%d calls=%d body=%q", missingKeyDownload.Code, missingKeyDownloader.calls, missingKeyDownload.Body.String())
	}
	missingKeyView := httptest.NewRecorder()
	missingKeyEngine.ServeHTTP(missingKeyView, httptest.NewRequest("GET", "/view", nil))
	if missingKeyView.Code != http.StatusOK || !strings.Contains(missingKeyView.Body.String(), `"retentionStatus":"key_unavailable"`) ||
		!strings.Contains(missingKeyView.Body.String(), descriptor.Digest) || !strings.Contains(missingKeyView.Body.String(), `"filesUsed":1`) {
		t.Fatalf("missing-key artifact metadata was hidden: status=%d body=%q", missingKeyView.Code, missingKeyView.Body.String())
	}
	wrong := *binding
	wrong.Receipt.OwnerIdentity = "mallory"
	if _, err := fresh.Read(context.Background(), wrong); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-owner read: %v", err)
	}
	if _, err := openclawreconcile.NewArtifactArchive(repo, "wrong-key").Read(context.Background(), *binding); err == nil {
		t.Fatal("wrong key accepted")
	}
	second := descriptor
	second.Digest = strings.Repeat("b", 64)
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{second}); err != nil {
		t.Fatal(err)
	}
	secondBinding, err := repo.ArtifactForEvent(context.Background(), "alice", event.ID, second.Digest)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback test fixtures")
	err = db.Transaction(func(tx *gorm.DB) error {
		otherRef := "ocgw:v2:" + uuid.NewString()
		if err := tx.Exec(`INSERT INTO openclaw_gateway_session_receipts(execution_reference,owner_identity,runtime_task_id,session_key,run_id,status,terminal_status,terminal_at) VALUES (?,'alice','other-task','other-session','other-run','terminal','completed',?)`, otherRef, now).Error; err != nil {
			return err
		}
		otherEvent := event
		otherEvent.ID = uuid.New()
		otherEvent.ExecutionReference = otherRef
		otherEvent.RuntimeTaskID = "other-task"
		otherEvent.EventKey = uuid.NewString()
		if err := tx.Create(&otherEvent).Error; err != nil {
			return err
		}
		otherRepo := openclawreconcile.NewRepository(tx)
		if err := otherRepo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), otherRef, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
			return err
		}
		otherBinding, err := otherRepo.ArtifactForEvent(context.Background(), "alice", otherEvent.ID, descriptor.Digest)
		if err != nil {
			return err
		}
		otherArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(otherRepo, artifactRetentionTestKey("retention-test-key-not-production"), true)
		if _, err := otherArchive.Retain(context.Background(), *otherBinding, content); err != nil {
			return err
		}
		otherDigest := strings.Repeat("c", 64)
		otherDescriptor := descriptor
		otherDescriptor.Digest = otherDigest
		if err := otherRepo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), otherRef, []agentruntime.GatewayArtifactDescriptor{otherDescriptor}); err != nil {
			return err
		}
		otherExecutionBinding, err := otherRepo.ArtifactForEvent(context.Background(), "alice", otherEvent.ID, otherDigest)
		if err != nil {
			return err
		}
		if _, err := otherArchive.Retain(context.Background(), *otherExecutionBinding, content); err != nil {
			return err
		}
		if err := otherArchive.Forget(context.Background(), "alice", event.ID, otherDigest, content.ContentSHA256); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("cross-execution forget: %v", err)
		}
		var otherRetained int64
		if err := tx.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", otherRef, otherDigest).Count(&otherRetained).Error; err != nil || otherRetained != 1 {
			t.Errorf("cross-execution forget removed copy: %d %v", otherRetained, err)
		}
		forged := *binding
		forged.Receipt.ExecutionReference = otherRef
		if _, err := otherArchive.Read(context.Background(), forged); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("cross-execution content read: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	for _, mode := range []string{"records", "bytes", "audit"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if mode == "audit" {
				if err := tx.Exec(`CREATE FUNCTION fail_retention_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.launch_type='agent_runtime_openclaw_artifact_retain' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_retention_audit BEFORE INSERT ON automation_launch_events FOR EACH ROW EXECUTE FUNCTION fail_retention_audit()`).Error; err != nil {
					return err
				}
			} else {
				n, size := 128, 0
				if mode == "bytes" {
					n, size = 8, 8388608
				}
				if err := tx.Exec(`INSERT INTO openclaw_gateway_artifact_receipts (id,execution_reference,artifact_digest,artifact_type,mime_type,size_bytes) SELECT uuid_generate_v4(),?,md5(i::text)||md5(i::text),'file','',? FROM generate_series(1,?) i`, ref, size, n).Error; err != nil {
					return err
				}
				if err := seedArtifactRetentionQuotaRows(tx, ref, event.ID, n, size); err != nil {
					return err
				}
			}
			_, err := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(openclawreconcile.NewRepository(tx), artifactRetentionTestKey("retention-test-key-not-production"), true).Retain(context.Background(), *secondBinding, content)
			if mode == "audit" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "audit unavailable") {
					t.Errorf("audit failure was not propagated from its trigger: %v", err)
				}
				var retained int64
				if queryErr := tx.Table("openclaw_retained_artifacts").Where("artifact_digest = ?", second.Digest).Count(&retained).Error; queryErr != nil || retained != 0 {
					t.Errorf("audit failure did not roll back: %d %v", retained, queryErr)
				}
			} else if !errors.Is(err, openclawreconcile.ErrArtifactRetentionQuota) {
				t.Errorf("%s quota: %v", mode, err)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("%s fixture: %v", mode, err)
		}
	}
	content.Data = []byte(strings.Repeat("x", len(data)))
	hash = sha256.Sum256(content.Data)
	content.ContentSHA256 = hex.EncodeToString(hash[:])
	if _, err := archive.Retain(context.Background(), *binding, content); !errors.Is(err, openclawreconcile.ErrArtifactRetentionConflict) {
		t.Fatalf("overwrite: %v", err)
	}
	usage, err := archive.Usage(context.Background(), "alice")
	if err != nil || usage.BytesUsed != int64(len(data)) || usage.FilesUsed != 1 || usage.BytesLimit != 64<<20 || usage.FilesLimit != 128 {
		t.Fatalf("usage: %#v %v", usage, err)
	}
	otherUsage, err := archive.Usage(context.Background(), "mallory")
	if err != nil || otherUsage.BytesUsed != 0 || otherUsage.FilesUsed != 0 {
		t.Fatalf("cross-owner usage: %#v %v", otherUsage, err)
	}
	if err := archive.Forget(context.Background(), "mallory", event.ID, descriptor.Digest, read.ContentSHA256); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-owner forget: %v", err)
	}
	if err := archive.Forget(context.Background(), "alice", event.ID, descriptor.Digest, strings.Repeat("0", 64)); !errors.Is(err, openclawreconcile.ErrArtifactRetentionPrecondition) {
		t.Fatalf("stale forget: %v", err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE FUNCTION fail_forget_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.launch_type='agent_runtime_openclaw_artifact_forget' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_forget_audit BEFORE INSERT ON automation_launch_events FOR EACH ROW EXECUTE FUNCTION fail_forget_audit()`).Error; err != nil {
			return err
		}
		if err := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(openclawreconcile.NewRepository(tx), artifactRetentionTestKey("retention-test-key-not-production"), true).Forget(context.Background(), "alice", event.ID, descriptor.Digest, read.ContentSHA256); err == nil {
			t.Error("forgot without audit")
		}
		var kept int64
		if err := tx.Table("openclaw_retained_artifacts").Where("artifact_digest = ?", descriptor.Digest).Count(&kept).Error; err != nil || kept != 1 {
			t.Errorf("audit failure lost copy: %d %v", kept, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	engine.DELETE("/retained", handler.Forget)
	for _, test := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing precondition", want: http.StatusPreconditionRequired},
		{name: "wildcard", header: "*", want: http.StatusPreconditionRequired},
		{name: "weak tag", header: `W/"` + read.ContentSHA256 + `"`, want: http.StatusPreconditionRequired},
		{name: "stale strong content hash", header: `"` + strings.Repeat("0", 64) + `"`, want: http.StatusPreconditionFailed},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/retained", nil)
		if test.header != "" {
			req.Header.Set("If-Match", test.header)
		}
		engine.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Errorf("%s: status = %d, want %d: %s", test.name, w.Code, test.want, w.Body.String())
		}
	}
	duplicateMatch := httptest.NewRecorder()
	duplicateRequest := httptest.NewRequest("DELETE", "/retained", nil)
	duplicateRequest.Header.Add("If-Match", `"`+read.ContentSHA256+`"`)
	duplicateRequest.Header.Add("If-Match", `"`+read.ContentSHA256+`"`)
	engine.ServeHTTP(duplicateMatch, duplicateRequest)
	if duplicateMatch.Code != http.StatusPreconditionRequired {
		t.Fatalf("duplicate If-Match values accepted: %d %s", duplicateMatch.Code, duplicateMatch.Body.String())
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/retained", nil)
	req.Header.Set("If-Match", `"`+read.ContentSHA256+`"`)
	engine.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("forget HTTP: %d %s", w.Code, w.Body.String())
	}
	usage, err = archive.Usage(context.Background(), "alice")
	if err != nil || usage.BytesUsed != 0 || usage.FilesUsed != 0 {
		t.Fatalf("quota not released: %#v %v", usage, err)
	}
	if _, err := archive.Read(context.Background(), *binding); !errors.Is(err, openclawreconcile.ErrArtifactNotRetained) {
		t.Fatalf("forgotten copy read: %v", err)
	}
	if _, err := repo.ArtifactForEvent(context.Background(), "alice", event.ID, descriptor.Digest); err != nil {
		t.Fatalf("source metadata removed: %v", err)
	}
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("launch_type = ?", "agent_runtime_openclaw_artifact_forget").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("missing removal audit: %d %v", count, err)
	}
}

func TestOpenClawArtifactRetentionConcurrentForgetIsIdempotentPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)

	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts (execution_reference,owner_identity,runtime_task_id,session_key,run_id,status,terminal_status,terminal_at) VALUES (?, 'alice','forget-task','forget-session','forget-run','terminal','completed',?)`, ref, now).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: "alice", RuntimeType: "openclaw", RuntimeTaskID: "forget-task", ExecutionReference: ref, LaunchType: "agent_runtime_openclaw_terminal", EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}

	digest := strings.Repeat("e", 64)
	data := []byte("concurrently forgotten retained result")
	size := int64(len(data))
	descriptor := agentruntime.GatewayArtifactDescriptor{Digest: digest, Type: "file", SizeBytes: &size}
	repo := openclawreconcile.NewRepository(db)
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	binding, err := repo.ArtifactForEvent(context.Background(), "alice", event.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	expectedSHA := hex.EncodeToString(hash[:])
	archive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("concurrent-forget-test-key-long-enough"), true)
	if _, err := archive.Retain(context.Background(), *binding, &agentruntime.GatewayArtifactContent{Data: data, ContentSHA256: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("retain accepted a claimed content hash that does not match the bytes")
	}
	var rejectedCopies int64
	if err := db.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", ref, digest).Count(&rejectedCopies).Error; err != nil || rejectedCopies != 0 {
		t.Fatalf("bad content hash persisted retained copy: %d %v", rejectedCopies, err)
	}
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("owner_identity = ? AND execution_reference = ? AND launch_type = ?", "alice", ref, "agent_runtime_openclaw_artifact_retain").Count(&rejectedCopies).Error; err != nil || rejectedCopies != 0 {
		t.Fatalf("bad content hash wrote retain audit: %d %v", rejectedCopies, err)
	}
	if _, err := archive.Retain(context.Background(), *binding, &agentruntime.GatewayArtifactContent{Data: data, ContentSHA256: expectedSHA}); err != nil {
		t.Fatalf("retain fixture: %v", err)
	}

	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	var workers sync.WaitGroup
	for i := 0; i < callers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- archive.Forget(context.Background(), "alice", event.ID, digest, expectedSHA)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("concurrent forget: %v", err)
		}
	}

	var retained int64
	if err := db.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", ref, digest).Count(&retained).Error; err != nil || retained != 0 {
		t.Fatalf("retained copy after concurrent forget: %d %v", retained, err)
	}
	var audits []models.AutomationLaunchEvent
	if err := db.Where("owner_identity = ? AND runtime_task_id = ? AND execution_reference = ? AND launch_type = ?", "alice", event.RuntimeTaskID, ref, "agent_runtime_openclaw_artifact_forget").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 {
		t.Fatalf("forget audit count after concurrent calls: %d, want exactly one", len(audits))
	}
	for _, want := range []string{"source_event_id=" + event.ID.String(), "artifact_reference_sha256=" + digest, "content_sha256=" + expectedSHA} {
		found := false
		for _, actual := range audits[0].AuditEvents {
			if actual == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("forget audit missing exact evidence %q: %#v", want, audits[0].AuditEvents)
		}
	}

	if err := archive.Forget(context.Background(), "alice", event.ID, digest, expectedSHA); err != nil {
		t.Fatalf("repeat forget with identical content hash: %v", err)
	}
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("owner_identity = ? AND execution_reference = ? AND launch_type = ?", "alice", ref, "agent_runtime_openclaw_artifact_forget").Count(&retained).Error; err != nil || retained != 1 {
		t.Fatalf("repeat forget duplicated audit: %d %v", retained, err)
	}
}

func TestOpenClawArtifactRetentionConcurrentQuotaBoundaryAcrossArchivesPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("get isolated PostgreSQL pool: %v", err)
	}
	pool.SetMaxOpenConns(8)

	owner := "alice"
	ref := "ocgw:v2:" + uuid.NewString()
	taskID := "quota-race-task-" + uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatalf("create quota-race receipt: %v", err)
	}
	event := models.AutomationLaunchEvent{
		ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw",
		RuntimeTaskID: taskID, ExecutionReference: ref, LaunchType: "agent_runtime_openclaw_terminal",
		EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create quota-race source event: %v", err)
	}
	if err := db.Exec(`INSERT INTO openclaw_gateway_artifact_receipts
		(id, execution_reference, artifact_digest, artifact_type, mime_type, size_bytes)
		SELECT uuid_generate_v4(), ?, md5(i::text)||md5(i::text), 'file', '', 1 FROM generate_series(1, 127) i`, ref).Error; err != nil {
		t.Fatalf("seed quota-race gateway descriptors: %v", err)
	}
	if err := seedArtifactRetentionQuotaRows(db, ref, event.ID, 127, 1); err != nil {
		t.Fatalf("seed 127 retained files: %v", err)
	}

	key := artifactRetentionTestKey("retention-test-key-not-production")
	repo := openclawreconcile.NewRepository(db)
	seedDigestBytes := md5.Sum([]byte("1"))
	seedDigest := hex.EncodeToString(seedDigestBytes[:]) + hex.EncodeToString(seedDigestBytes[:])
	seedBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, seedDigest)
	if err != nil {
		t.Fatalf("bind seeded retained artifact: %v", err)
	}
	seedData := []byte{0}
	seedHash := sha256.Sum256(seedData)
	seedContent := &agentruntime.GatewayArtifactContent{Data: seedData, ContentSHA256: hex.EncodeToString(seedHash[:])}
	firstArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, key, true)
	secondArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, key, true)
	for index, archive := range []*openclawreconcile.ArtifactArchive{firstArchive, secondArchive} {
		if _, err := archive.Retain(context.Background(), *seedBinding, seedContent); err != nil {
			t.Fatalf("prevalidate independent archive %d: %v", index+1, err)
		}
	}

	digests := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	descriptors := make([]agentruntime.GatewayArtifactDescriptor, len(digests))
	for i, digest := range digests {
		size := int64(1)
		descriptors[i] = agentruntime.GatewayArtifactDescriptor{Digest: digest, Type: "file", SizeBytes: &size}
	}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, descriptors); err != nil {
		t.Fatalf("create quota-race candidate descriptors: %v", err)
	}
	bindings := make([]openclawreconcile.ArtifactDownloadBinding, len(digests))
	for i, digest := range digests {
		binding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, digest)
		if err != nil {
			t.Fatalf("bind quota-race candidate %d: %v", i+1, err)
		}
		bindings[i] = *binding
	}
	candidateData := []byte{1}
	candidateHash := sha256.Sum256(candidateData)
	candidateContent := &agentruntime.GatewayArtifactContent{Data: candidateData, ContentSHA256: hex.EncodeToString(candidateHash[:])}

	blockerTx := db.Begin()
	if blockerTx.Error != nil {
		t.Fatalf("begin archive-wide lock blocker transaction: %v", blockerTx.Error)
	}
	if err := blockerTx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "openclaw-artifact-retention:key-validation").Error; err != nil {
		_ = blockerTx.Rollback().Error
		t.Fatalf("hold archive-wide key-validation lock: %v", err)
	}

	start := make(chan struct{})
	type result struct {
		index int
		err   error
	}
	results := make(chan result, 2)
	archives := []*openclawreconcile.ArtifactArchive{firstArchive, secondArchive}
	var workers sync.WaitGroup
	workerCtx, cancelWorkers := context.WithTimeout(context.Background(), 20*time.Second)
	defer func() {
		cancelWorkers()
		_ = blockerTx.Rollback().Error
		workers.Wait()
	}()
	for i := range archives {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, err := archives[index].Retain(workerCtx, bindings[index], candidateContent)
			results <- result{index: index, err: err}
		}(i)
	}
	close(start)
	succeeded, quotaRejected := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err == nil {
				succeeded++
			} else if errors.Is(result.err, openclawreconcile.ErrArtifactRetentionQuota) {
				quotaRejected++
			} else {
				t.Errorf("archive %d returned unexpected quota-race error: %v", result.index+1, result.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("warmed archives waited on the global key-validation lock instead of using owner-scoped quota locking")
		}
	}
	if err := blockerTx.Commit().Error; err != nil {
		t.Fatalf("release archive-wide key-validation lock: %v", err)
	}
	workers.Wait()
	if succeeded != 1 || quotaRejected != 1 {
		t.Fatalf("quota race outcomes: succeeded=%d quota_rejected=%d; want one of each", succeeded, quotaRejected)
	}
	var retainedCount int64
	if err := db.Table("openclaw_retained_artifacts").Where("owner_identity = ?", owner).Count(&retainedCount).Error; err != nil || retainedCount != 128 {
		t.Fatalf("owner quota after race: rows=%d err=%v; want exactly 128", retainedCount, err)
	}
	var candidateCount int64
	if err := db.Table("openclaw_retained_artifacts").Where("artifact_digest IN ?", digests).Count(&candidateCount).Error; err != nil || candidateCount != 1 {
		t.Fatalf("candidate retained after race: rows=%d err=%v; want exactly one", candidateCount, err)
	}
}

func TestOpenClawArtifactRetentionDoesNotSerializeHotOwnersWithinArchivePostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("get isolated PostgreSQL pool: %v", err)
	}
	pool.SetMaxOpenConns(8)

	type fixture struct {
		owner   string
		binding openclawreconcile.ArtifactDownloadBinding
		content *agentruntime.GatewayArtifactContent
	}
	repo := openclawreconcile.NewRepository(db)
	makeFixture := func(owner, digest string) fixture {
		t.Helper()
		ref := "ocgw:v2:" + uuid.NewString()
		taskID := "owner-concurrency-task-" + uuid.NewString()
		now := time.Now().UTC()
		if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
			(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
			VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
			t.Fatalf("create receipt for %s: %v", owner, err)
		}
		event := models.AutomationLaunchEvent{
			ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw",
			RuntimeTaskID: taskID, ExecutionReference: ref, LaunchType: "agent_runtime_openclaw_terminal",
			EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now,
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("create source event for %s: %v", owner, err)
		}
		data := []byte("retained content for " + owner)
		sum := sha256.Sum256(data)
		content := &agentruntime.GatewayArtifactContent{Data: data, ContentSHA256: hex.EncodeToString(sum[:])}
		size := int64(len(data))
		descriptor := agentruntime.GatewayArtifactDescriptor{Digest: digest, Type: "file", SizeBytes: &size}
		if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
			t.Fatalf("create descriptor for %s: %v", owner, err)
		}
		binding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, digest)
		if err != nil {
			t.Fatalf("bind artifact for %s: %v", owner, err)
		}
		return fixture{owner: owner, binding: *binding, content: content}
	}
	first := makeFixture("owner-lock-a", strings.Repeat("c", 64))
	second := makeFixture("owner-lock-b", strings.Repeat("d", 64))
	archive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("retention-test-key-not-production"), true)
	for _, item := range []fixture{first, second} {
		if _, err := archive.Retain(context.Background(), item.binding, item.content); err != nil {
			t.Fatalf("warm archive for %s: %v", item.owner, err)
		}
	}

	blocker := db.Begin()
	if blocker.Error != nil {
		t.Fatalf("begin owner-lock blocker transaction: %v", blocker.Error)
	}
	if err := blocker.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "openclaw-artifact-retention:"+first.owner).Error; err != nil {
		_ = blocker.Rollback().Error
		t.Fatalf("hold first owner quota lock: %v", err)
	}
	workerCtx, cancelWorkers := context.WithTimeout(context.Background(), 20*time.Second)
	firstResult := make(chan error, 1)
	firstDone := false
	defer func() {
		cancelWorkers()
		_ = blocker.Rollback().Error
		if !firstDone {
			select {
			case <-firstResult:
			case <-time.After(20 * time.Second):
			}
		}
	}()
	go func() {
		_, err := archive.Retain(workerCtx, first.binding, first.content)
		firstResult <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`).Scan(&count).Error; err != nil {
			t.Fatalf("inspect owner-lock waiter: %v", err)
		}
		if count > 0 {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("first owner retain did not wait on its deliberately held owner lock")
	}

	secondResult := make(chan error, 1)
	go func() {
		_, err := archive.Retain(workerCtx, second.binding, second.content)
		secondResult <- err
	}()
	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatalf("independent owner was blocked or failed while first owner was locked: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("one warmed archive serialized a different owner behind a blocked retain")
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatalf("release first owner quota lock: %v", err)
	}
	select {
	case err := <-firstResult:
		firstDone = true
		if err != nil {
			t.Fatalf("first owner's idempotent retain: %v", err)
		}
	case <-workerCtx.Done():
		t.Fatalf("first owner retain did not resume after lock release: %v", workerCtx.Err())
	}
}

func TestOpenClawArtifactRetentionKeyGuardAndLegacyReadPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	scanLogger := &artifactRetentionValidationScanLogger{Interface: db.Logger}
	db.Logger = scanLogger
	repo := openclawreconcile.NewRepository(db)
	owner := "retention-key-guard-owner"
	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	taskID := "retention-key-guard-task"
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}

	keyMaterial := artifactRetentionTestKey("historical-openclaw-retention-ciphertext")
	configuredSecret := "random-v1:" + keyMaterial
	archive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, configuredSecret, true)
	currentData := []byte("current retained artifact")
	currentSize := int64(len(currentData))
	currentDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: strings.Repeat("a", 64), Type: "file", SizeBytes: &currentSize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{currentDescriptor}); err != nil {
		t.Fatal(err)
	}
	currentBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, currentDescriptor.Digest)
	if err != nil {
		t.Fatal(err)
	}
	currentHash := sha256.Sum256(currentData)
	currentContent := &agentruntime.GatewayArtifactContent{Data: currentData, ContentSHA256: hex.EncodeToString(currentHash[:])}
	legacyData := []byte("artifact retained by the historical prefixed-key implementation")
	legacyHash := sha256.Sum256(legacyData)
	legacyContentSHA := hex.EncodeToString(legacyHash[:])
	legacySize := int64(len(legacyData))
	legacyDigest := strings.Repeat("b", 64)
	legacyDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: legacyDigest, Type: "file", SizeBytes: &legacySize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{legacyDescriptor}); err != nil {
		t.Fatal(err)
	}
	legacyBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, legacyDigest)
	if err != nil {
		t.Fatal(err)
	}
	legacyCiphertext, err := historicalArtifactRetentionCiphertext(keyMaterial, owner, ref, legacyDigest, legacyContentSHA, legacyData)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO openclaw_retained_artifacts
		(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content, retained_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, ref, legacyDigest, owner, event.ID, legacyContentSHA, legacySize, legacyCiphertext, now).Error; err != nil {
		t.Fatal(err)
	}
	legacyRead, err := openclawreconcile.NewArtifactArchive(repo, configuredSecret).Read(context.Background(), *legacyBinding)
	if legacyRead != nil || !errors.Is(err, openclawreconcile.ErrArtifactRetentionLegacyProvenance) {
		t.Fatalf("legacy ciphertext was served despite unauthenticated source-event provenance: %#v %v", legacyRead, err)
	}

	// A mutable v1 source-event column cannot prove provenance: even if it is
	// changed to another valid event and requested through that event, downloads
	// remain blocked until the artifact is re-retained as authenticated v2 data.
	alternateLegacyEvent := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: event.AutomationID, OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&alternateLegacyEvent).Error; err != nil {
		t.Fatalf("create alternate legacy event: %v", err)
	}
	alternateLegacyBinding, err := repo.ArtifactForEvent(context.Background(), owner, alternateLegacyEvent.ID, legacyDigest)
	if err != nil {
		t.Fatalf("bind legacy artifact to alternate event: %v", err)
	}
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET source_event_id = ? WHERE execution_reference = ? AND artifact_digest = ?`, alternateLegacyEvent.ID, ref, legacyDigest).Error; err != nil {
		t.Fatalf("mutate legacy source event: %v", err)
	}
	if opened, err := archive.Read(context.Background(), *alternateLegacyBinding); opened != nil || !errors.Is(err, openclawreconcile.ErrArtifactRetentionLegacyProvenance) {
		t.Fatalf("legacy row attributed to an alternate valid event was served: %#v %v", opened, err)
	}
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET source_event_id = ? WHERE execution_reference = ? AND artifact_digest = ?`, event.ID, ref, legacyDigest).Error; err != nil {
		t.Fatalf("restore legacy source event: %v", err)
	}

	wrongKeyArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("different-unconfirmed-archive-key"), true)
	if _, err := wrongKeyArchive.Retain(context.Background(), *currentBinding, currentContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("first write with a changed key over legacy ciphertext was allowed: %v", err)
	}
	if got := scanLogger.scanCount(); got != 1 {
		t.Fatalf("first write must validate existing legacy ciphertext once, scans=%d", got)
	}

	// A failed transaction must not persist a key check. Retrying must validate
	// the existing ciphertext again before establishing the durable key anchor.
	cacheArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, configuredSecret, true)
	wrongBinding := *currentBinding
	wrongBinding.Receipt.ExecutionReference += ":changed"
	if _, err := cacheArchive.Retain(context.Background(), wrongBinding, currentContent); err == nil {
		t.Fatal("retention with a changed binding unexpectedly committed")
	}
	if got := scanLogger.scanCount(); got != 2 {
		t.Fatalf("fresh instance must validate before a rejected transaction, scans=%d", got)
	}
	if _, err := cacheArchive.Retain(context.Background(), *currentBinding, currentContent); err != nil {
		t.Fatalf("retry after rejected transaction: %v", err)
	}
	if got := scanLogger.scanCount(); got != 3 {
		t.Fatalf("uncommitted key check was persisted after a rejected transaction, scans=%d", got)
	}

	newData := []byte("new artifact must not be written under a changed key")
	newHash := sha256.Sum256(newData)
	newSize := int64(len(newData))
	newDigest := strings.Repeat("c", 64)
	newDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: newDigest, Type: "file", SizeBytes: &newSize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{newDescriptor}); err != nil {
		t.Fatal(err)
	}
	newBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, newDigest)
	if err != nil {
		t.Fatal(err)
	}
	newContent := &agentruntime.GatewayArtifactContent{Data: newData, ContentSHA256: hex.EncodeToString(newHash[:])}
	if _, err := cacheArchive.Retain(context.Background(), *newBinding, newContent); err != nil {
		t.Fatalf("retain after successful archive validation: %v", err)
	}
	if _, err := cacheArchive.Retain(context.Background(), *newBinding, newContent); err != nil {
		t.Fatalf("repeat retain after successful archive validation: %v", err)
	}
	if got := scanLogger.scanCount(); got != 3 {
		t.Fatalf("durable key check did not avoid repeated full archive scans, scans=%d", got)
	}

	legacySource := &retentionGatewaySpy{content: &agentruntime.GatewayArtifactContent{Data: legacyData, ContentSHA256: legacyContentSHA}}
	legacyRetainHandler := openclawreconcile.NewArtifactHandler(repo, legacySource)
	legacyRetainHandler.SetArchive(cacheArchive)
	legacyRetainEngine := gin.New()
	legacyRetainEngine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, owner) })
	legacyRetainEngine.POST("/events/:eventId/artifacts/:digest/retain", legacyRetainHandler.Retain)
	legacyRetainResponse := httptest.NewRecorder()
	legacyRetainEngine.ServeHTTP(legacyRetainResponse, httptest.NewRequest(http.MethodPost, "/events/"+event.ID.String()+"/artifacts/"+legacyDigest+"/retain", nil))
	if legacyRetainResponse.Code != http.StatusOK || legacySource.calls != 1 {
		t.Fatalf("legacy retain did not refresh from the live source and migrate: status=%d calls=%d body=%q", legacyRetainResponse.Code, legacySource.calls, legacyRetainResponse.Body.String())
	}
	upgraded, err := cacheArchive.Read(context.Background(), *legacyBinding)
	if err != nil || !bytes.Equal(upgraded.Data, legacyData) {
		t.Fatalf("re-retained legacy artifact was not upgraded to readable authenticated provenance: %v", err)
	}
	var provenanceUpgradeEvents int64
	if err := db.Model(&models.AutomationLaunchEvent{}).Where("owner_identity = ? AND execution_reference = ? AND launch_type = ?", owner, ref, "agent_runtime_openclaw_artifact_provenance_upgrade").Count(&provenanceUpgradeEvents).Error; err != nil || provenanceUpgradeEvents != 1 {
		t.Fatalf("legacy provenance upgrade audit count=%d err=%v", provenanceUpgradeEvents, err)
	}

	// v2 binds SourceEventID in GCM associated data, and Read independently
	// checks the row identity against the requested event before decrypting.
	var alternateEventIDText string
	if err := db.Model(&models.AutomationLaunchEvent{}).
		Select("id").
		Where("execution_reference = ? AND id <> ?", ref, newBinding.EventID).
		Order("started_at ASC").
		Take(&alternateEventIDText).Error; err != nil {
		t.Fatalf("find a different persisted event for tamper test: %v", err)
	}
	alternateEventID, err := uuid.Parse(alternateEventIDText)
	if err != nil {
		t.Fatalf("parse alternate persisted event ID: %v", err)
	}
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET source_event_id = ? WHERE execution_reference = ? AND artifact_digest = ?`, alternateEventID, ref, newDigest).Error; err != nil {
		t.Fatalf("tamper retained source event: %v", err)
	}
	if read, err := cacheArchive.Read(context.Background(), *newBinding); err == nil || read != nil {
		t.Fatalf("Read accepted a retained row attributed to a different event: %#v %v", read, err)
	}
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET source_event_id = ? WHERE execution_reference = ? AND artifact_digest = ?`, newBinding.EventID, ref, newDigest).Error; err != nil {
		t.Fatalf("restore retained source event: %v", err)
	}

	// A warmed archive must still authenticate the duplicate row itself, not
	// rely on its one-time archive-wide key-validation result.
	var originalCiphertext []byte
	if err := db.Raw(`SELECT encrypted_content FROM openclaw_retained_artifacts WHERE execution_reference = ? AND artifact_digest = ?`, ref, newDigest).Row().Scan(&originalCiphertext); err != nil {
		t.Fatalf("read ciphertext for warmed duplicate corruption test: %v", err)
	}
	corruptedDuplicate := append([]byte(nil), originalCiphertext...)
	corruptedDuplicate[len(corruptedDuplicate)-1] ^= 0x01
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET encrypted_content = ? WHERE execution_reference = ? AND artifact_digest = ?`, corruptedDuplicate, ref, newDigest).Error; err != nil {
		t.Fatalf("corrupt warmed duplicate row: %v", err)
	}
	if _, err := cacheArchive.Retain(context.Background(), *newBinding, newContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("warmed duplicate retain accepted corrupted ciphertext: %v", err)
	}
	if got := scanLogger.scanCount(); got != 3 {
		t.Fatalf("duplicate corruption check unexpectedly repeated the archive-wide scan: scans=%d", got)
	}
	if err := db.Exec(`UPDATE openclaw_retained_artifacts SET encrypted_content = ? WHERE execution_reference = ? AND artifact_digest = ?`, originalCiphertext, ref, newDigest).Error; err != nil {
		t.Fatalf("restore retained ciphertext after corruption test: %v", err)
	}

	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "nonce", mutate: func(data []byte) { data[0] ^= 0x01 }},
		{name: "authentication tag", mutate: func(data []byte) { data[len(data)-1] ^= 0x01 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			corruptArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, configuredSecret, true)
			corrupted := append([]byte(nil), legacyCiphertext...)
			test.mutate(corrupted)
			if len(corrupted) != int(legacySize)+28 {
				t.Fatalf("corrupted fixture has unexpected length %d", len(corrupted))
			}
			if err := db.Exec(`UPDATE openclaw_retained_artifacts SET encrypted_content = ? WHERE execution_reference = ? AND artifact_digest = ?`, corrupted, ref, legacyDigest).Error; err != nil {
				t.Fatal(err)
			}
			if opened, err := archive.Read(context.Background(), *legacyBinding); err == nil || opened != nil {
				t.Fatalf("corrupted retained ciphertext was accepted: %#v %v", opened, err)
			}
			if _, err := corruptArchive.Retain(context.Background(), *currentBinding, currentContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
				t.Fatalf("retention proceeded while an existing record was unreadable: %v", err)
			}
			gateway := &retentionGatewaySpy{content: &agentruntime.GatewayArtifactContent{Data: legacyData, ContentSHA256: legacyContentSHA}}
			handler := openclawreconcile.NewArtifactHandler(repo, gateway)
			handler.SetArchive(archive)
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, owner) })
			engine.GET("/events/:eventId/artifacts/:digest", handler.Download)
			response := httptest.NewRecorder()
			path := "/events/" + event.ID.String() + "/artifacts/" + legacyDigest
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusServiceUnavailable || gateway.calls != 0 || response.Header().Get("Content-Disposition") != "" {
				t.Fatalf("corrupt retained copy fell through or escaped: status=%d gatewayCalls=%d headers=%v", response.Code, gateway.calls, response.Header())
			}
			if err := db.Exec(`UPDATE openclaw_retained_artifacts SET encrypted_content = ? WHERE execution_reference = ? AND artifact_digest = ?`, legacyCiphertext, ref, legacyDigest).Error; err != nil {
				t.Fatal(err)
			}
		})
	}

	changedKeyArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("different-confirmed-retention-key"), true)
	scansBeforeChangedKey := scanLogger.scanCount()
	var retainedCountBeforeChangedKey int64
	if err := db.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", ref, newDigest).Count(&retainedCountBeforeChangedKey).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := changedKeyArchive.Retain(context.Background(), *newBinding, newContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("changed key was allowed to write: %v", err)
	}
	if got := scanLogger.scanCount(); got != scansBeforeChangedKey {
		t.Fatalf("changed-key check unexpectedly scanned all ciphertext despite the authenticated key anchor: scans before=%d after=%d", scansBeforeChangedKey, got)
	}
	var retainedCount int64
	if err := db.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", ref, newDigest).Count(&retainedCount).Error; err != nil || retainedCount != retainedCountBeforeChangedKey {
		t.Fatalf("changed-key write changed retained rows: before=%d after=%d err=%v", retainedCountBeforeChangedKey, retainedCount, err)
	}
}

func TestOpenClawArtifactRetentionKeyCheckSurvivesForgettingLastArtifactPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	repo := openclawreconcile.NewRepository(db)
	owner := "retention-key-anchor-owner"
	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	taskID := "retention-key-anchor-task"
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	firstData := []byte("retained artifact that will later be forgotten")
	firstHash := sha256.Sum256(firstData)
	firstContent := &agentruntime.GatewayArtifactContent{Data: firstData, ContentSHA256: hex.EncodeToString(firstHash[:])}
	firstDigest := strings.Repeat("e", 64)
	firstSize := int64(len(firstData))
	firstDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: firstDigest, Type: "file", SizeBytes: &firstSize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{firstDescriptor}); err != nil {
		t.Fatal(err)
	}
	firstBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, firstDigest)
	if err != nil {
		t.Fatal(err)
	}
	key := artifactRetentionTestKey("persistent-retention-key-anchor-test-key")
	archive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, key, true)
	firstInfo, err := archive.Retain(context.Background(), *firstBinding, firstContent)
	if err != nil {
		t.Fatalf("initial retention: %v", err)
	}
	if err := archive.Forget(context.Background(), owner, event.ID, firstDigest, firstInfo.ContentSHA256); err != nil {
		t.Fatalf("forget last retained artifact: %v", err)
	}
	var retainedCount, keyCheckCount int64
	if err := db.Table("openclaw_retained_artifacts").Count(&retainedCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("openclaw_artifact_retention_key_check").Count(&keyCheckCount).Error; err != nil {
		t.Fatal(err)
	}
	if retainedCount != 0 || keyCheckCount != 1 {
		t.Fatalf("after forgetting final artifact, retained=%d key checks=%d; want 0 and 1", retainedCount, keyCheckCount)
	}
	var encryptedCheck []byte
	if err := db.Raw("SELECT encrypted_check FROM openclaw_artifact_retention_key_check WHERE id = 1").Row().Scan(&encryptedCheck); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encryptedCheck, []byte(key)) || bytes.Contains(encryptedCheck, []byte("hai-openclaw-artifact-retention-key-check-v1")) {
		t.Fatal("database key-check row contains the secret or plaintext marker")
	}

	secondData := []byte("new artifact after the previous one was forgotten")
	secondHash := sha256.Sum256(secondData)
	secondContent := &agentruntime.GatewayArtifactContent{Data: secondData, ContentSHA256: hex.EncodeToString(secondHash[:])}
	secondDigest := strings.Repeat("f", 64)
	secondSize := int64(len(secondData))
	secondDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: secondDigest, Type: "file", SizeBytes: &secondSize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{secondDescriptor}); err != nil {
		t.Fatal(err)
	}
	secondBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, secondDigest)
	if err != nil {
		t.Fatal(err)
	}
	changedKeyArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(repo, artifactRetentionTestKey("changed-key-after-forget-test-key"), true)
	if _, err := changedKeyArchive.Retain(context.Background(), *secondBinding, secondContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("changed key was accepted after all artifacts were forgotten: %v", err)
	}
	if err := db.Table("openclaw_retained_artifacts").Count(&retainedCount).Error; err != nil || retainedCount != 0 {
		t.Fatalf("changed-key attempt created retained data: count=%d err=%v", retainedCount, err)
	}
	if _, err := archive.Retain(context.Background(), *secondBinding, secondContent); err != nil {
		t.Fatalf("same key could not retain after artifact forget: %v", err)
	}
}

func TestOpenClawArtifactRetentionConcurrentFirstWriteUsesOneKeyPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("get isolated PostgreSQL pool: %v", err)
	}
	pool.SetMaxOpenConns(8)

	openPinnedSession := func() (*sql.Conn, *gorm.DB) {
		t.Helper()
		conn, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatalf("pin PostgreSQL connection: %v", err)
		}
		if _, err := conn.ExecContext(context.Background(), "SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
			_ = conn.Close()
			t.Fatalf("set deliberately stale default isolation: %v", err)
		}
		var isolation string
		if err := conn.QueryRowContext(context.Background(), "SHOW default_transaction_isolation").Scan(&isolation); err != nil || !strings.EqualFold(isolation, "repeatable read") {
			_ = conn.Close()
			t.Fatalf("verify session default isolation: %q %v", isolation, err)
		}
		session, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			_ = conn.Close()
			t.Fatalf("open GORM session on pinned connection: %v", err)
		}
		return conn, session
	}
	firstConn, firstDB := openPinnedSession()
	secondConn, secondDB := openPinnedSession()
	blockerConn, blockerDB := openPinnedSession()
	defer func() {
		_ = firstConn.Close()
		_ = secondConn.Close()
		_ = blockerConn.Close()
	}()

	owner := "retention-concurrent-key-owner"
	ref := "ocgw:v2:" + uuid.NewString()
	now := time.Now().UTC()
	taskID := "retention-concurrent-key-task"
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, ref, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: ref, EventKey: uuid.NewString(), StartedAt: now, CompletedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	contentData := []byte("one key wins the concurrent first retain")
	contentHash := sha256.Sum256(contentData)
	content := &agentruntime.GatewayArtifactContent{Data: contentData, ContentSHA256: hex.EncodeToString(contentHash[:])}
	digest := strings.Repeat("d", 64)
	size := int64(len(contentData))
	descriptor := agentruntime.GatewayArtifactDescriptor{Digest: digest, Type: "file", SizeBytes: &size}
	repo := openclawreconcile.NewRepository(db)
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	binding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	firstArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(openclawreconcile.NewRepository(firstDB), artifactRetentionTestKey("concurrent-first-write-key-one"), true)
	secondArchive := openclawreconcile.NewArtifactArchiveWithWriteConfirmation(openclawreconcile.NewRepository(secondDB), artifactRetentionTestKey("concurrent-first-write-key-two"), true)

	const globalLockName = "openclaw-artifact-retention:key-validation"
	blockerTx := blockerDB.Begin()
	if blockerTx.Error != nil {
		t.Fatalf("begin advisory-lock blocker transaction: %v", blockerTx.Error)
	}
	if err := blockerTx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", globalLockName).Error; err != nil {
		_ = blockerTx.Rollback().Error
		t.Fatalf("hold global archive validation lock: %v", err)
	}
	var firstPID, secondPID int
	if err := firstConn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		_ = blockerTx.Rollback().Error
		t.Fatalf("read first archive backend pid: %v", err)
	}
	if err := secondConn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		_ = blockerTx.Rollback().Error
		t.Fatalf("read second archive backend pid: %v", err)
	}
	if firstPID == secondPID {
		_ = blockerTx.Rollback().Error
		t.Fatalf("archive instances unexpectedly share one PostgreSQL connection: pid=%d", firstPID)
	}

	workerCtx, cancelWorkers := context.WithTimeout(context.Background(), 30*time.Second)
	var workers sync.WaitGroup
	results := make(chan struct {
		archive *openclawreconcile.ArtifactArchive
		info    *openclawreconcile.ArtifactRetentionInfo
		err     error
	}, 2)
	start := make(chan struct{})
	for _, archive := range []*openclawreconcile.ArtifactArchive{firstArchive, secondArchive} {
		workers.Add(1)
		go func(archive *openclawreconcile.ArtifactArchive) {
			defer workers.Done()
			<-start
			info, err := archive.Retain(workerCtx, *binding, content)
			results <- struct {
				archive *openclawreconcile.ArtifactArchive
				info    *openclawreconcile.ArtifactRetentionInfo
				err     error
			}{archive: archive, info: info, err: err}
		}(archive)
	}
	defer func() {
		_ = blockerTx.Rollback().Error
		cancelWorkers()
		workers.Wait()
	}()
	close(start)

	deadline := time.Now().Add(15 * time.Second)
	for {
		var waiting int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND pid IN (?, ?)`, firstPID, secondPID).Scan(&waiting).Error; err != nil {
			t.Fatalf("inspect PostgreSQL lock waiters: %v", err)
		}
		if waiting == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("both independent archive connections did not reach the global advisory lock; waiting=%d pids=(%d,%d)", waiting, firstPID, secondPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := blockerTx.Commit().Error; err != nil {
		t.Fatalf("release global archive validation lock: %v", err)
	}

	var winner, loser *openclawreconcile.ArtifactArchive
	successes, keyRejections := 0, 0
	for range 2 {
		select {
		case result := <-results:
			if result.err == nil {
				successes++
				winner = result.archive
			} else if errors.Is(result.err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
				keyRejections++
				loser = result.archive
			} else {
				t.Fatalf("unexpected concurrent retain result: %v", result.err)
			}
		case <-workerCtx.Done():
			t.Fatalf("concurrent retain workers did not finish: %v", workerCtx.Err())
		}
	}
	if successes != 1 || keyRejections != 1 || winner == nil || loser == nil {
		t.Fatalf("first-write key race did not fail closed: successes=%d key_rejections=%d", successes, keyRejections)
	}
	workers.Wait()

	var retainedRows int64
	if err := db.Table("openclaw_retained_artifacts").Where("execution_reference = ? AND artifact_digest = ?", ref, digest).Count(&retainedRows).Error; err != nil || retainedRows != 1 {
		t.Fatalf("concurrent first write left unexpected rows: count=%d err=%v", retainedRows, err)
	}
	if read, err := winner.Read(context.Background(), *binding); err != nil || !bytes.Equal(read.Data, contentData) {
		t.Fatalf("winning key cannot read the established artifact: %v", err)
	}
	if read, err := loser.Read(context.Background(), *binding); err == nil || read != nil {
		t.Fatalf("losing key can read the established artifact: %#v %v", read, err)
	}

	secondData := []byte("losing key must not write a separate artifact")
	secondHash := sha256.Sum256(secondData)
	secondSize := int64(len(secondData))
	secondDigest := strings.Repeat("e", 64)
	secondDescriptor := agentruntime.GatewayArtifactDescriptor{Digest: secondDigest, Type: "file", SizeBytes: &secondSize}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), ref, []agentruntime.GatewayArtifactDescriptor{secondDescriptor}); err != nil {
		t.Fatal(err)
	}
	secondBinding, err := repo.ArtifactForEvent(context.Background(), owner, event.ID, secondDigest)
	if err != nil {
		t.Fatal(err)
	}
	secondContent := &agentruntime.GatewayArtifactContent{Data: secondData, ContentSHA256: hex.EncodeToString(secondHash[:])}
	if _, err := loser.Retain(context.Background(), *secondBinding, secondContent); !errors.Is(err, openclawreconcile.ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("losing key established a second retained artifact: %v", err)
	}
	if err := db.Table("openclaw_retained_artifacts").Count(&retainedRows).Error; err != nil || retainedRows != 1 {
		t.Fatalf("losing key changed the established archive: count=%d err=%v", retainedRows, err)
	}
}

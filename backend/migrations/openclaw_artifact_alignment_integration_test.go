package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOpenClawArtifactAlignmentPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	apply := func(file string) error {
		sql, err := migrations.Files.ReadFile("pre/" + file)
		if err != nil {
			t.Fatal(err)
		}
		return db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(sql)).Error })
	}
	for _, file := range []string{"0069_openclaw_gateway_session_receipts.up.sql", "0071_openclaw_gateway_artifact_receipts.up.sql"} {
		if err := apply(file); err != nil {
			t.Fatal(err)
		}
	}
	reference := "ocgw:v2:" + uuid.NewString()
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status)
		VALUES (?, 'test-owner', 'test-task', 'agent:main:test', 'test-run', 'admitted')`, reference).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE open_claw_gateway_artifact_receipts (LIKE openclaw_gateway_artifact_receipts INCLUDING ALL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE open_claw_gateway_artifact_receipts ALTER COLUMN mime_type DROP NOT NULL`).Error; err != nil {
		t.Fatal(err)
	}
	size := int64(512)
	legacy := models.OpenClawGatewayArtifactReceipt{
		ID: uuid.New(), ExecutionReference: reference, ArtifactDigest: strings.Repeat("a", 64),
		ArtifactType: "file", MIMEType: "", SizeBytes: &size, CreatedAt: time.Now().UTC(),
	}
	if err := db.Exec(`INSERT INTO open_claw_gateway_artifact_receipts
		(id, execution_reference, artifact_digest, artifact_type, mime_type, size_bytes, created_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?)`, legacy.ID, legacy.ExecutionReference, legacy.ArtifactDigest,
		legacy.ArtifactType, size, legacy.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := apply("0078_openclaw_artifact_alignment.up.sql"); err != nil {
		t.Fatal(err)
	}
	var migrated models.OpenClawGatewayArtifactReceipt
	if err := db.First(&migrated, "id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.SizeBytes == nil || *migrated.SizeBytes != size || migrated.MIMEType != "" {
		t.Fatalf("nullable legacy metadata was not normalized: %#v", migrated)
	}
	if err := db.Exec(`ALTER TABLE open_claw_gateway_artifact_receipts DROP COLUMN mime_type`).Error; err != nil {
		t.Fatal(err)
	}
	if err := apply("0078_openclaw_artifact_alignment.up.sql"); err != nil {
		t.Fatalf("alignment with legacy MIME column absent: %v", err)
	}
	repo := openclawreconcile.NewRepository(db)
	zero := int64(0)
	descriptors := []agentruntime.GatewayArtifactDescriptor{
		{Digest: strings.Repeat("b", 64), Type: "file", SizeBytes: nil},
		{Digest: strings.Repeat("c", 64), Type: "file", SizeBytes: &zero},
	}
	for i := 0; i < 2; i++ {
		if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), reference, descriptors); err != nil {
			t.Fatal(err)
		}
	}
	var stored []models.OpenClawGatewayArtifactReceipt
	if err := db.Order("artifact_digest").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 || stored[1].SizeBytes != nil || stored[2].SizeBytes == nil || *stored[2].SizeBytes != 0 {
		t.Fatalf("nullable size or idempotence lost: %#v", stored)
	}
	if err := repo.CreateOpenClawGatewayArtifactDescriptors(context.Background(), "ocgw:v2:"+uuid.NewString(), descriptors); err == nil {
		t.Fatal("unbound artifact was accepted without a session receipt")
	}
	if err := db.Table("open_claw_gateway_artifact_receipts").Where("id = ?", legacy.ID).Update("size_bytes", 999).Error; err != nil {
		t.Fatal(err)
	}
	if err := apply("0078_openclaw_artifact_alignment.up.sql"); err == nil {
		t.Fatal("conflicting history silently merged")
	}
	if err := db.First(&migrated, "id = ?", legacy.ID).Error; err != nil || migrated.SizeBytes == nil || *migrated.SizeBytes != 512 {
		t.Fatal("conflict overwrote canonical history")
	}
	var count int64
	if err := db.Table("open_claw_gateway_artifact_receipts").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("legacy table not retained")
	}
	if err := apply("0078_openclaw_artifact_alignment.down.sql"); err == nil {
		t.Fatal("unsafe automatic rollback allowed")
	}
}

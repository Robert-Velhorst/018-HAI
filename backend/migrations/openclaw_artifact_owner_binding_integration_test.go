package migrations_test

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestOpenClawRetainedArtifactOwnerBindingPostgres(t *testing.T) {
	db := openArtifactRetentionDatabase(t)
	owner := "artifact-owner-" + uuid.NewString()
	otherOwner := "artifact-other-owner-" + uuid.NewString()
	reference := "ocgw:v2:" + uuid.NewString()
	taskID := "artifact-task-" + uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`, reference, owner, taskID, uuid.NewString(), uuid.NewString(), now).Error; err != nil {
		t.Fatalf("create owner-bound OpenClaw receipt: %v", err)
	}

	makeEvent := func(eventOwner string) models.AutomationLaunchEvent {
		t.Helper()
		event := models.AutomationLaunchEvent{
			ID: uuid.New(), AutomationID: uuid.New(), OwnerIdentity: eventOwner,
			RuntimeType: "openclaw", RuntimeTaskID: taskID, ExecutionReference: reference,
			LaunchType: "agent_runtime_openclaw_test", EventKey: uuid.NewString(),
			Target: "archive://openclaw/test", Status: "observed", StartedAt: now, CompletedAt: now,
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("create source event for %s: %v", eventOwner, err)
		}
		return event
	}
	ownerEvent := makeEvent(owner)
	otherEvent := makeEvent(otherOwner)

	insertDescriptor := func(test *testing.T, digest string) {
		test.Helper()
		if err := db.Exec(`INSERT INTO openclaw_gateway_artifact_receipts
			(execution_reference, artifact_digest, artifact_type, mime_type, size_bytes)
			VALUES (?, ?, 'file', 'application/octet-stream', 0)`, reference, digest).Error; err != nil {
			test.Fatalf("create descriptor %s: %v", digest, err)
		}
	}
	insertRetained := func(ownerIdentity string, sourceEventID uuid.UUID, digest string) error {
		return db.Exec(`INSERT INTO openclaw_retained_artifacts
			(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content)
			VALUES (?, ?, ?, ?, ?, 0, ?)`, reference, digest, ownerIdentity, sourceEventID, strings.Repeat("0", 64), make([]byte, 28)).Error
	}

	validDigest := strings.Repeat("a", 64)
	insertDescriptor(t, validDigest)
	if err := insertRetained(owner, ownerEvent.ID, validDigest); err != nil {
		t.Fatalf("valid owner-bound retained artifact was rejected: %v", err)
	}

	t.Run("owner must match source event", func(t *testing.T) {
		digest := strings.Repeat("b", 64)
		insertDescriptor(t, digest)
		if err := insertRetained(otherOwner, ownerEvent.ID, digest); err == nil {
			t.Fatal("retained row with an owner different from its source event was accepted")
		}
	})

	t.Run("owner must match OpenClaw receipt", func(t *testing.T) {
		digest := strings.Repeat("c", 64)
		insertDescriptor(t, digest)
		if err := insertRetained(otherOwner, otherEvent.ID, digest); err == nil {
			t.Fatal("retained row with an owner different from its session receipt was accepted")
		}
	})
}

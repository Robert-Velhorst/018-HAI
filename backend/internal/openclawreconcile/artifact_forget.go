package openclawreconcile

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrArtifactRetentionPrecondition = errors.New("retained content changed since review")

type ArtifactStorageUsage struct {
	BytesUsed  int64 `json:"bytesUsed"`
	FilesUsed  int64 `json:"filesUsed"`
	BytesLimit int64 `json:"bytesLimit"`
	FilesLimit int64 `json:"filesLimit"`
}

func (a *ArtifactArchive) Usage(ctx context.Context, owner string) (*ArtifactStorageUsage, error) {
	if !a.MetadataReady() {
		return nil, ErrArtifactRetentionDisabled
	}
	if strings.TrimSpace(owner) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	usage := ArtifactStorageUsage{BytesLimit: artifactOwnerStorageLimit, FilesLimit: artifactOwnerRecordLimit}
	err := a.repo.db.WithContext(ctx).Model(&retainedArtifact{}).Select("COALESCE(SUM(size_bytes),0) AS bytes_used, COUNT(*) AS files_used").Where("owner_identity = ?", owner).Scan(&usage).Error
	return &usage, err
}

// Forget removes only the retained ciphertext, never the native file or source receipts.
func (a *ArtifactArchive) Forget(ctx context.Context, owner string, eventID uuid.UUID, digest, expectedSHA string) error {
	if !a.Ready() {
		return ErrArtifactRetentionDisabled
	}
	if strings.TrimSpace(owner) == "" || eventID == uuid.Nil || !validArtifactDigest(digest) || !validArtifactDigest(expectedSHA) {
		return ErrArtifactRetentionPrecondition
	}
	return a.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "openclaw-artifact-retention:"+owner).Error; err != nil {
			return err
		}
		var source models.AutomationLaunchEvent
		if err := tx.Where("id = ? AND owner_identity = ? AND runtime_type = ?", eventID, owner, "openclaw").Take(&source).Error; err != nil {
			return err
		}
		if strings.TrimSpace(source.ExecutionReference) == "" || strings.TrimSpace(source.RuntimeTaskID) == "" {
			return gorm.ErrRecordNotFound
		}
		binding, err := NewRepository(tx).ArtifactForEvent(ctx, owner, eventID, digest)
		if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
			// ArtifactForEvent intentionally hides non-retained artifacts for normal
			// download requests. A repeat Forget must still reach its exact audit
			// check after a prior removal, including launch types that require a
			// retained copy for download.
			forgotten, auditErr := artifactForgetWasAudited(tx, owner, source, eventID, source.ExecutionReference, digest, expectedSHA)
			if auditErr != nil {
				return auditErr
			}
			return artifactForgetMissingBindingResult(err, forgotten)
		}
		if err != nil {
			return err
		}
		if binding == nil || binding.EventID != eventID || binding.Receipt.OwnerIdentity != owner || binding.Descriptor.Digest != digest ||
			binding.Receipt.ExecutionReference != source.ExecutionReference || binding.Receipt.RuntimeTaskID != source.RuntimeTaskID {
			return gorm.ErrRecordNotFound
		}
		var row retainedArtifact
		query := retainedArtifactEventScope(tx.Model(&retainedArtifact{}), owner, binding.Receipt.ExecutionReference, eventID)
		if err := query.Where("artifact_digest = ?", digest).Take(&row).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var anyOwnerCopy int64
			if err := tx.Model(&retainedArtifact{}).Where("execution_reference = ? AND artifact_digest = ?", binding.Receipt.ExecutionReference, digest).Count(&anyOwnerCopy).Error; err != nil {
				return err
			}
			if anyOwnerCopy != 0 {
				return gorm.ErrRecordNotFound
			}
			// The owner lock makes this absence check and audit lookup atomic with Retain.
			forgotten, auditErr := artifactForgetWasAudited(tx, owner, source, eventID, binding.Receipt.ExecutionReference, digest, expectedSHA)
			if auditErr != nil {
				return auditErr
			}
			if forgotten {
				return nil
			}
			return gorm.ErrRecordNotFound
		}
		if err := artifactForgetRowSafeToRemove(a, row, owner, eventID, binding.Receipt.ExecutionReference, digest, expectedSHA); err != nil {
			return err
		}
		removed := tx.Where("owner_identity = ? AND source_event_id = ? AND execution_reference = ? AND artifact_digest = ? AND content_sha256 = ?", owner, eventID, binding.Receipt.ExecutionReference, digest, expectedSHA).Delete(&retainedArtifact{})
		if removed.Error != nil {
			return removed.Error
		}
		if removed.RowsAffected != 1 {
			return ErrArtifactRetentionPrecondition
		}
		now := time.Now().UTC()
		event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: source.AutomationID, OwnerIdentity: owner, RuntimeType: "openclaw", RuntimeTaskID: source.RuntimeTaskID, ExecutionReference: source.ExecutionReference, LaunchType: "agent_runtime_openclaw_artifact_forget", EventKey: "openclaw-artifact-forget:" + uuid.NewString(), Target: "archive://openclaw/artifact", Status: "observed", Message: "Retained HAI copy removed; upstream file and source metadata unchanged", AuditEvents: []string{"source_event_id=" + eventID.String(), "artifact_reference_sha256=" + digest, "content_sha256=" + expectedSHA}, StartedAt: now, CompletedAt: now}
		return tx.Create(&event).Error
	})
}

func artifactForgetMissingBindingResult(bindingErr error, alreadyForgotten bool) error {
	if bindingErr == nil || !errors.Is(bindingErr, gorm.ErrRecordNotFound) || !alreadyForgotten {
		return bindingErr
	}
	return nil
}

func artifactForgetRowMatches(row retainedArtifact, owner string, eventID uuid.UUID, executionReference, digest string) bool {
	return row.OwnerIdentity == owner && row.SourceEventID == eventID && row.ExecutionReference == executionReference && row.ArtifactDigest == digest
}

func artifactForgetContentMatches(row retainedArtifact, expectedSHA string) bool {
	return row.ContentSHA256 == expectedSHA
}

func artifactForgetRowSafeToRemove(archive *ArtifactArchive, row retainedArtifact, owner string, eventID uuid.UUID, executionReference, digest, expectedSHA string) error {
	if !artifactForgetRowMatches(row, owner, eventID, executionReference, digest) {
		return gorm.ErrRecordNotFound
	}
	if !artifactForgetContentMatches(row, expectedSHA) {
		return ErrArtifactRetentionPrecondition
	}
	if archive == nil || archive.aead == nil {
		return ErrArtifactRetentionDisabled
	}
	if _, _, err := archive.decryptRetainedArtifactWithFormat(row); err != nil {
		return ErrArtifactRetentionExistingContentUnreadable
	}
	return nil
}

func artifactForgetAuditEventsMatch(events []string, eventID uuid.UUID, digest, expectedSHA string) bool {
	return containsExactArtifactForgetAuditEvent(events, "source_event_id="+eventID.String()) &&
		containsExactArtifactForgetAuditEvent(events, "artifact_reference_sha256="+digest) &&
		containsExactArtifactForgetAuditEvent(events, "content_sha256="+expectedSHA)
}

func containsExactArtifactForgetAuditEvent(events []string, expected string) bool {
	for _, event := range events {
		if event == expected {
			return true
		}
	}
	return false
}

func artifactForgetWasAudited(tx *gorm.DB, owner string, source models.AutomationLaunchEvent, eventID uuid.UUID, executionReference, digest, expectedSHA string) (bool, error) {
	var audits []models.AutomationLaunchEvent
	query := tx.Model(&models.AutomationLaunchEvent{}).Select("audit_events").
		Where("owner_identity = ? AND runtime_type = ? AND runtime_task_id = ? AND execution_reference = ? AND launch_type = ?", owner, "openclaw", source.RuntimeTaskID, executionReference, "agent_runtime_openclaw_artifact_forget")
	for _, value := range []string{"source_event_id=" + eventID.String(), "artifact_reference_sha256=" + digest, "content_sha256=" + expectedSHA} {
		query = query.Where("POSITION(? IN audit_events) > 0", `"`+value+`"`)
	}
	if err := query.Find(&audits).Error; err != nil {
		return false, err
	}
	for _, audit := range audits {
		if artifactForgetAuditEventsMatch(audit.AuditEvents, eventID, digest, expectedSHA) {
			return true, nil
		}
	}
	return false, nil
}

func artifactIfMatchSHA256(headers []string) (string, bool) {
	if len(headers) != 1 {
		return "", false
	}
	match := strings.TrimSpace(headers[0])
	if len(match) != 66 || match[0] != '"' || match[65] != '"' || !validArtifactDigest(match[1:65]) {
		return "", false
	}
	return match[1:65], true
}

func (h *ArtifactHandler) Forget(c *gin.Context) {
	owner, id, ok := artifactRequest(c)
	if !ok {
		return
	}
	digest := c.Param("digest")
	if !validArtifactDigest(digest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid artifact reference"})
		return
	}
	if h.archive == nil || !h.archive.Ready() {
		artifactResponseError(c, ErrArtifactRetentionDisabled)
		return
	}
	// Require one strong, exact content fingerprint, not a wildcard or weak ETag.
	expectedSHA, ok := artifactIfMatchSHA256(c.Request.Header.Values("If-Match"))
	if !ok {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "Review the retained file version before removing its HAI copy"})
		return
	}
	if err := h.archive.Forget(c.Request.Context(), owner, id, digest, expectedSHA); err != nil {
		artifactResponseError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

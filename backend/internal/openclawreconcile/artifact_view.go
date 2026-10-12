package openclawreconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ArtifactView struct {
	StorageUsage       *ArtifactStorageUsage                   `json:"storageUsage,omitempty"`
	RetentionAvailable bool                                    `json:"retentionAvailable"`
	RetentionStatus    string                                  `json:"retentionStatus"`
	Retained           []ArtifactRetentionInfo                 `json:"retained,omitempty"`
	State              string                                  `json:"state"`
	Attempts           int                                     `json:"attempts"`
	NextAttemptAt      *time.Time                              `json:"nextAttemptAt,omitempty"`
	CapturedAt         *time.Time                              `json:"capturedAt,omitempty"`
	Items              []models.OpenClawGatewayArtifactReceipt `json:"items"`
	collectionActive   bool
}

type ArtifactDownloadBinding struct {
	Receipt    agentruntime.OpenClawGatewayReceipt
	Descriptor agentruntime.GatewayArtifactDescriptor
	EventID    uuid.UUID
}

func (r *Repository) artifactReceiptForEvent(ctx context.Context, owner string, eventID uuid.UUID) (models.OpenClawGatewaySessionReceipt, string, error) {
	var row models.OpenClawGatewaySessionReceipt
	if r == nil || r.db == nil || strings.TrimSpace(owner) == "" || eventID == uuid.Nil {
		return row, "", fmt.Errorf("artifact read unavailable")
	}
	var source struct{ LaunchType string }
	if err := r.db.WithContext(ctx).Model(&models.AutomationLaunchEvent{}).Select("launch_type").Where("id = ? AND runtime_type = 'openclaw' AND owner_identity = ?", eventID, owner).Take(&source).Error; err != nil {
		return row, "", err
	}
	err := r.db.WithContext(ctx).Table("openclaw_gateway_session_receipts AS receipt").Select("receipt.*").
		Joins("JOIN automation_launch_events AS event ON event.execution_reference = receipt.execution_reference AND event.runtime_task_id = receipt.runtime_task_id AND event.owner_identity = receipt.owner_identity").
		Where("event.id = ? AND event.runtime_type = 'openclaw' AND event.owner_identity = ? AND receipt.owner_identity = ?", eventID, owner, owner).Take(&row).Error
	return row, source.LaunchType, err
}

func requiresRetainedArtifactForEvent(launchType string) bool {
	return strings.HasPrefix(launchType, "agent_runtime_openclaw_") && launchType != "agent_runtime_openclaw_terminal"
}

func filterArtifactsToRetained(items []models.OpenClawGatewayArtifactReceipt, retainedDigests map[string]struct{}) []models.OpenClawGatewayArtifactReceipt {
	filtered := make([]models.OpenClawGatewayArtifactReceipt, 0, len(items))
	for _, item := range items {
		if _, retained := retainedDigests[item.ArtifactDigest]; retained {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (r *Repository) retainedArtifactDigestsForEvent(ctx context.Context, owner, executionReference string, eventID uuid.UUID) (map[string]struct{}, error) {
	var rows []struct{ ArtifactDigest string }
	query := retainedArtifactEventScope(r.db.WithContext(ctx).Table("openclaw_retained_artifacts"), owner, executionReference, eventID)
	if err := query.Select("artifact_digest").Find(&rows).Error; err != nil {
		return nil, err
	}
	digests := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		digests[row.ArtifactDigest] = struct{}{}
	}
	return digests, nil
}

func (r *Repository) ArtifactsForEvent(ctx context.Context, owner string, eventID uuid.UUID) (*ArtifactView, error) {
	receipt, launchType, err := r.artifactReceiptForEvent(ctx, owner, eventID)
	if err != nil {
		return nil, err
	}
	view := &ArtifactView{State: "pending", Items: []models.OpenClawGatewayArtifactReceipt{}}
	var collection struct {
		Attempts                  int
		NextAttemptAt, CapturedAt *time.Time
		ClaimedAt                 *time.Time
	}
	result := r.db.WithContext(ctx).Table("openclaw_gateway_artifact_collections").Where("execution_reference = ?", receipt.ExecutionReference).Take(&collection)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}
	view.Attempts, view.NextAttemptAt, view.CapturedAt = collection.Attempts, collection.NextAttemptAt, collection.CapturedAt
	view.collectionActive = collection.ClaimedAt != nil && time.Now().UTC().Before(collection.ClaimedAt.UTC().Add(artifactCollectionLease))
	if err := r.db.WithContext(ctx).Where("execution_reference = ?", receipt.ExecutionReference).Order("created_at ASC, id ASC").Limit(21).Find(&view.Items).Error; err != nil {
		return nil, err
	}
	if len(view.Items) > 20 {
		return nil, fmt.Errorf("stored artifact limit exceeded")
	}
	for _, item := range view.Items {
		if !validArtifactDescriptor(artifactDescriptor(item)) {
			return nil, fmt.Errorf("invalid stored artifact")
		}
	}
	if requiresRetainedArtifactForEvent(launchType) {
		retainedDigests, err := r.retainedArtifactDigestsForEvent(ctx, owner, receipt.ExecutionReference, eventID)
		if err != nil {
			return nil, err
		}
		view.Items = filterArtifactsToRetained(view.Items, retainedDigests)
	}
	finalizeArtifactViewState(view, receipt)
	return view, nil
}

func finalizeArtifactViewState(view *ArtifactView, receipt models.OpenClawGatewaySessionReceipt) {
	switch {
	case receipt.Status != "terminal":
		view.State = "waiting_for_completion"
	case receipt.TerminalStatus != "completed":
		view.State = "unavailable"
	case view.collectionActive:
		view.State = "collecting"
		view.NextAttemptAt = nil
	case view.CapturedAt != nil:
		view.State = "captured"
	case view.Attempts >= 8:
		view.State = "retry_exhausted"
	default:
		view.State = "pending"
	}

	// Legacy metadata remains inspectable without claiming a durable collection,
	// but it must not hide that automatic collection exhausted its retry limit.
	if len(view.Items) > 0 && view.CapturedAt == nil && view.State != "retry_exhausted" && view.State != "collecting" {
		view.State = "metadata_available"
	}
	// The final attempt's backoff timestamp is not an eligible retry: automatic
	// claims stop at eight attempts. Keep the stored timestamp intact for audit,
	// but do not advertise a retry that the worker will never claim.
	if view.State == "retry_exhausted" {
		view.NextAttemptAt = nil
	}
}

func artifactDescriptor(item models.OpenClawGatewayArtifactReceipt) agentruntime.GatewayArtifactDescriptor {
	return agentruntime.GatewayArtifactDescriptor{Digest: item.ArtifactDigest, Type: item.ArtifactType, MIMEType: item.MIMEType, SizeBytes: item.SizeBytes}
}

func (r *Repository) ArtifactForEvent(ctx context.Context, owner string, eventID uuid.UUID, digest string) (*ArtifactDownloadBinding, error) {
	receipt, launchType, err := r.artifactReceiptForEvent(ctx, owner, eventID)
	if err != nil {
		return nil, err
	}
	if receipt.Status != "terminal" || receipt.TerminalStatus != "completed" || receipt.TerminalAt == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if requiresRetainedArtifactForEvent(launchType) {
		var retained retainedArtifact
		query := retainedArtifactEventScope(r.db.WithContext(ctx).Model(&retainedArtifact{}), owner, receipt.ExecutionReference, eventID)
		if err := query.Where("artifact_digest = ?", digest).Take(&retained).Error; err != nil {
			return nil, err
		}
	}
	var item models.OpenClawGatewayArtifactReceipt
	if err := r.db.WithContext(ctx).Where("execution_reference = ? AND artifact_digest = ?", receipt.ExecutionReference, digest).Take(&item).Error; err != nil {
		return nil, err
	}
	descriptor := artifactDescriptor(item)
	if !validArtifactDescriptor(descriptor) {
		return nil, fmt.Errorf("invalid stored artifact")
	}
	return &ArtifactDownloadBinding{Receipt: receiptFromModel(receipt), Descriptor: descriptor, EventID: eventID}, nil
}

func (r *Repository) RecordArtifactDownload(ctx context.Context, binding ArtifactDownloadBinding, contentSHA string, size int) error {
	if size < 0 || size > agentruntime.OpenClawArtifactContentLimit || !validArtifactDigest(contentSHA) {
		return fmt.Errorf("invalid artifact download audit")
	}
	// Recheck the event binding before recording content read, not task completion.
	var source models.AutomationLaunchEvent
	if err := r.db.WithContext(ctx).Where("id = ? AND owner_identity = ? AND execution_reference = ? AND runtime_task_id = ? AND runtime_type = 'openclaw'", binding.EventID, binding.Receipt.OwnerIdentity, binding.Receipt.ExecutionReference, binding.Receipt.RuntimeTaskID).Take(&source).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: source.AutomationID, OwnerIdentity: source.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: source.RuntimeTaskID, ExecutionReference: source.ExecutionReference,
		LaunchType: "agent_runtime_openclaw_artifact_download", EventKey: "openclaw-artifact-download:" + uuid.NewString(), Target: "gateway://openclaw/artifact-content", Status: "observed", Message: fmt.Sprintf("Artifact content read: %d bytes", size),
		AuditEvents: []string{"artifact_reference_sha256=" + binding.Descriptor.Digest, "content_sha256=" + contentSHA, "Content read only; browser delivery and deliverable correctness are not verified"}, StartedAt: now, CompletedAt: now}
	return r.db.WithContext(ctx).Create(&event).Error
}

type artifactViewReader interface {
	ArtifactsForEvent(context.Context, string, uuid.UUID) (*ArtifactView, error)
	ArtifactForEvent(context.Context, string, uuid.UUID, string) (*ArtifactDownloadBinding, error)
	RecordArtifactDownload(context.Context, ArtifactDownloadBinding, string, int) error
}
type artifactContentReader interface {
	DownloadOpenClawGatewayArtifact(context.Context, agentruntime.OpenClawGatewayReceipt, agentruntime.GatewayArtifactDescriptor) (*agentruntime.GatewayArtifactContent, error)
}
type ArtifactHandler struct {
	archive    *ArtifactArchive
	reader     artifactViewReader
	downloader artifactContentReader
	slots      chan struct{}
}

func NewArtifactHandler(reader artifactViewReader, downloader artifactContentReader) *ArtifactHandler {
	return &ArtifactHandler{reader: reader, downloader: downloader, slots: make(chan struct{}, 2)}
}

func (h *ArtifactHandler) SetArchive(archive *ArtifactArchive) { h.archive = archive }

func (h *ArtifactHandler) content(ctx context.Context, binding ArtifactDownloadBinding) (*agentruntime.GatewayArtifactContent, error) {
	if h.archive != nil {
		content, err := h.archive.Read(ctx, binding)
		if errors.Is(err, ErrArtifactRetentionLegacyProvenance) {
			if h.downloader == nil {
				return nil, err
			}
			liveContent, downloadErr := h.downloader.DownloadOpenClawGatewayArtifact(ctx, binding.Receipt, binding.Descriptor)
			if downloadErr != nil {
				return nil, downloadErr
			}
			if verifyErr := h.archive.verifyRetainedContentMatchesLive(ctx, binding, liveContent); verifyErr != nil && !errors.Is(verifyErr, ErrArtifactNotRetained) {
				return nil, verifyErr
			}
			// A download is read-only. Only the explicit Retain handler persists
			// the freshly verified content and upgrades legacy provenance.
			return liveContent, nil
		}
		if !errors.Is(err, ErrArtifactNotRetained) {
			return content, err
		}
	}
	if h.downloader == nil {
		return nil, fmt.Errorf("runtime content unavailable")
	}
	return h.downloader.DownloadOpenClawGatewayArtifact(ctx, binding.Receipt, binding.Descriptor)
}

func artifactRequest(c *gin.Context) (string, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	subject, _ := c.Get(identity.ContextSubjectKey)
	owner, _ := subject.(string)
	if strings.TrimSpace(owner) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return "", uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("eventId"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return "", uuid.Nil, false
	}
	return owner, id, true
}
func artifactResponseError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrArtifactRetentionPrecondition):
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "The retained version changed; refresh and review before removing it"})
	case errors.Is(err, ErrArtifactRetentionConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "A different version is already retained; existing content was preserved"})
	case errors.Is(err, ErrArtifactRetentionQuota):
		c.JSON(http.StatusConflict, gin.H{"error": "Retention limit reached (64 MiB or 128 files per owner); existing files were preserved"})
	case errors.Is(err, ErrArtifactRetentionDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Encrypted artifact retention is not configured"})
	case errors.Is(err, ErrArtifactRetentionExistingContentUnreadable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "The retained copy could not be authenticated with the configured key; it was preserved", "code": "retained_copy_unreadable"})
	case errors.Is(err, ErrArtifactRetentionWeakKey):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "New encrypted retention writes require a dedicated key, operator confirmation that it was generated with a cryptographically secure random source, and minimum key-diversity checks; existing retained files remain readable"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "No artifact available for this execution"})
	case errors.Is(err, agentruntime.ErrOpenClawArtifactURL):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "The artifact download origin is not authorized by the configured network policy", "code": "download_origin_not_authorized"})
	case errors.Is(err, agentruntime.ErrOpenClawArtifactUnsupported):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "This artifact cannot be downloaded within the supported format and 8 MiB limit", "code": "content_unsupported_or_too_large"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Artifacts are temporarily unavailable"})
	}
}
func (h *ArtifactHandler) Get(c *gin.Context) {
	owner, id, ok := artifactRequest(c)
	if !ok {
		return
	}
	if h.reader == nil {
		artifactResponseError(c, fmt.Errorf("reader unavailable"))
		return
	}
	view, err := h.reader.ArtifactsForEvent(c.Request.Context(), owner, id)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	view.RetentionAvailable = h.archive != nil && h.archive.WriteReady()
	switch {
	case view.RetentionAvailable:
		view.RetentionStatus = "available"
	case h.archive != nil && h.archive.Ready():
		view.RetentionStatus = "weak_key"
	case h.archive != nil && h.archive.MetadataReady():
		view.RetentionStatus = "key_unavailable"
	default:
		view.RetentionStatus = "unavailable"
	}
	if h.archive != nil && h.archive.MetadataReady() {
		view.Retained, err = h.archive.List(c.Request.Context(), owner, id)
		if err != nil {
			artifactResponseError(c, err)
			return
		}
		view.StorageUsage, err = h.archive.Usage(c.Request.Context(), owner)
		if err != nil {
			artifactResponseError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, view)
}
func validArtifactDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32 && digest == strings.ToLower(digest)
}
func (h *ArtifactHandler) Download(c *gin.Context) {
	owner, id, ok := artifactRequest(c)
	if !ok {
		return
	}
	digest := c.Param("digest")
	if !validArtifactDigest(digest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid artifact reference"})
		return
	}
	if h.reader == nil || (h.downloader == nil && (h.archive == nil || !h.archive.Ready())) {
		artifactResponseError(c, fmt.Errorf("reader unavailable"))
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		c.Header("Retry-After", "5")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Artifact downloads are busy; try again shortly"})
		return
	}
	binding, err := h.reader.ArtifactForEvent(c.Request.Context(), owner, id, digest)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	if binding == nil || binding.EventID != id || binding.Receipt.OwnerIdentity != owner || binding.Descriptor.Digest != digest {
		artifactResponseError(c, fmt.Errorf("invalid binding"))
		return
	}
	content, err := h.content(c.Request.Context(), *binding)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	if content == nil || len(content.Data) > agentruntime.OpenClawArtifactContentLimit {
		artifactResponseError(c, fmt.Errorf("invalid content"))
		return
	}
	sum := sha256.Sum256(content.Data)
	contentSHA := hex.EncodeToString(sum[:])
	if content.ContentSHA256 != contentSHA {
		artifactResponseError(c, fmt.Errorf("invalid content checksum"))
		return
	}
	if err := h.reader.RecordArtifactDownload(c.Request.Context(), *binding, contentSHA, len(content.Data)); err != nil {
		artifactResponseError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="openclaw-`+digest[:12]+`.bin"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	c.Header("X-Content-SHA256", contentSHA)
	c.Data(http.StatusOK, "application/octet-stream", content.Data)
}

package agentruntime

import (
	"archive/zip"
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/safety"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// Ecosystem archives are inspected, not extracted, but their metadata still
	// determines what HAI indexes. Bound it so an uploaded archive cannot create
	// ambiguous paths or consume unbounded parsing resources.
	maxOpenClawZipEntries           = 100_000
	maxOpenClawZipUncompressedBytes = uint64(1 << 30)
	maxOpenClawZipCompressionRatio  = uint64(200)
	maxOpenClawEcosystemUploadBytes = int64(750 * 1024 * 1024)
	maxConcurrentOpenClawUploads    = 2
	// Multipart framing and the small approval fields need limited overhead in
	// addition to the archive itself.
	maxOpenClawEcosystemRequestBytes = maxOpenClawEcosystemUploadBytes + (1 << 20)
)

type Handler struct {
	registry    *Registry
	authorizer  EcosystemMutationAuthorizer
	preparer    EcosystemMutationApprovalPreparer
	now         func() time.Time
	mutationMu  sync.Mutex
	uploadSlots chan struct{}
}

func NewHandler(registry *Registry) *Handler {
	return NewHandlerWithEcosystemMutationAuthorizer(registry, nil)
}

func NewHandlerWithEcosystemMutationAuthorizer(
	registry *Registry,
	authorizer EcosystemMutationAuthorizer,
) *Handler {
	return NewHandlerWithEcosystemMutationAuthorization(
		registry,
		authorizer,
		nil,
	)
}

// NewHandlerWithEcosystemMutationAuthorization wires both halves of a
// governed mutation. The preparer creates an exact, short-lived owner
// approval; the authorizer consumes it immediately before the effect.
func NewHandlerWithEcosystemMutationAuthorization(
	registry *Registry,
	authorizer EcosystemMutationAuthorizer,
	preparer EcosystemMutationApprovalPreparer,
) *Handler {
	return &Handler{
		registry:    registry,
		authorizer:  authorizer,
		preparer:    preparer,
		now:         time.Now,
		uploadSlots: make(chan struct{}, maxConcurrentOpenClawUploads),
	}
}

func (h *Handler) Registry(c *gin.Context) {
	c.JSON(http.StatusOK, h.registry.List())
}

func (h *Handler) Overview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, h.registry.Overview(ctx))
}

func (h *Handler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, h.registry.Health(ctx))
}

func (h *Handler) Skills(c *gin.Context) {
	runtimeID := strings.TrimSpace(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	skills, err := h.registry.Skills(ctx, runtimeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent runtime is not registered"})
		return
	}
	c.JSON(http.StatusOK, skills)
}

func (h *Handler) StopTask(c *gin.Context) {
	ownerIdentity, ok := runtimeOwner(c)
	if !ok {
		return
	}
	runtimeID := strings.TrimSpace(c.Param("id"))
	taskID := strings.TrimSpace(c.Param("taskId"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	result := h.registry.StopTask(ctx, runtimeID, taskID, ownerIdentity)
	status := http.StatusOK
	if result.Status == "blocked" {
		status = http.StatusBadRequest
	}
	c.JSON(status, result)
}

type openClawEcosystemRequest struct {
	EcosystemPath string `json:"ecosystemPath"`
	EcosystemMutationAuthorization
}

func (h *Handler) OpenClawEcosystem(c *gin.Context) {
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

func (h *Handler) SetOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	var request openClawEcosystemRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body for openclaw ecosystem path"})
		return
	}
	path := strings.TrimSpace(request.EcosystemPath)
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ecosystemPath is required"})
		return
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	prepared, err := openClaw.prepareEcosystemPath(path, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OpenClaw ecosystem path is unavailable or does not meet configured safety requirements"})
		return
	}
	authorization := mergeEcosystemAuthorization(c, request.EcosystemMutationAuthorization)
	effect := openClawEcosystemEffect{
		Action:            openClawSetPathAction,
		CurrentPath:       prepared.previousPath,
		CurrentSignature:  prepared.previousSignature,
		TargetPath:        prepared.targetPath,
		TargetSignature:   prepared.targetSignature,
		DeleteManagedPath: prepared.deleteManagedPath,
	}
	if !h.authorizeEcosystemMutation(c, owner, authorization, effect) {
		return
	}
	if err := validateOpenClawEcosystemPath(prepared.targetPath); err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	proceed, err := commitEcosystemMutation(c, openClaw, func() error {
		return openClaw.commitPreparedEcosystemPathLocked(prepared)
	})
	if !proceed {
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

type openClawArchiveRollbackRequest struct {
	EcosystemMutationAuthorization
}

func (h *Handler) PrepareRollbackOpenClawArchive(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	prepared, _, _, err := openClaw.prepareOpenClawArchiveRollback()
	if errors.Is(err, ErrOpenClawArchiveRollbackUnavailable) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrOpenClawArchiveRollbackUnavailable.Error()})
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	h.prepareEcosystemMutationApproval(c, owner, openClawEcosystemEffect{
		Action:           openClawRollbackAction,
		CurrentPath:      prepared.previousPath,
		CurrentSignature: prepared.previousSignature,
		TargetPath:       prepared.targetPath,
		TargetSignature:  prepared.targetSignature,
	})
}

func (h *Handler) RollbackOpenClawArchive(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	var request openClawArchiveRollbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body for OpenClaw archive rollback"})
		return
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	prepared, targetDigest, previousDigest, err := openClaw.prepareOpenClawArchiveRollback()
	if errors.Is(err, ErrOpenClawArchiveRollbackUnavailable) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrOpenClawArchiveRollbackUnavailable.Error()})
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	effect := openClawEcosystemEffect{
		Action:           openClawRollbackAction,
		CurrentPath:      prepared.previousPath,
		CurrentSignature: prepared.previousSignature,
		TargetPath:       prepared.targetPath,
		TargetSignature:  prepared.targetSignature,
	}
	if !h.authorizeEcosystemMutation(
		c,
		owner,
		mergeEcosystemAuthorization(c, request.EcosystemMutationAuthorization),
		effect,
	) {
		return
	}
	if err := validateOpenClawEcosystemPath(prepared.targetPath); err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	proceed, err := commitEcosystemMutation(c, openClaw, func() error {
		return openClaw.commitManagedArchiveSelectionLocked(prepared, targetDigest, previousDigest)
	})
	if !proceed {
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

// PrepareSetOpenClawEcosystem derives the exact validated archive/directory
// change and returns the short-lived authorization required to apply it. It
// never changes the configured ecosystem path.
func (h *Handler) PrepareSetOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	var request openClawEcosystemRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body for openclaw ecosystem path"})
		return
	}
	path := strings.TrimSpace(request.EcosystemPath)
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ecosystemPath is required"})
		return
	}
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	prepared, err := openClaw.prepareEcosystemPath(path, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OpenClaw ecosystem path is unavailable or does not meet configured safety requirements"})
		return
	}
	h.prepareEcosystemMutationApproval(c, owner, openClawEcosystemEffect{
		Action:            openClawSetPathAction,
		CurrentPath:       prepared.previousPath,
		CurrentSignature:  prepared.previousSignature,
		TargetPath:        prepared.targetPath,
		TargetSignature:   prepared.targetSignature,
		DeleteManagedPath: prepared.deleteManagedPath,
	})
}

func (h *Handler) RefreshOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	currentPath, currentSignature := openClaw.ecosystemState()
	authorization := mergeEcosystemAuthorization(c, EcosystemMutationAuthorization{})
	effect := openClawEcosystemEffect{
		Action:           openClawRefreshAction,
		CurrentPath:      currentPath,
		CurrentSignature: currentSignature,
		TargetPath:       currentPath,
		TargetSignature:  currentSignature,
	}
	if !h.authorizeEcosystemMutation(c, owner, authorization, effect) {
		return
	}
	proceed, err := commitEcosystemMutation(c, openClaw, func() error {
		return openClaw.refreshEcosystemInventoryIfCurrentLocked(currentPath, currentSignature)
	})
	if !proceed {
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

// PrepareRefreshOpenClawEcosystem returns approval references for refreshing
// the current exact ecosystem revision without making any change itself.
func (h *Handler) PrepareRefreshOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	currentPath, currentSignature := openClaw.ecosystemState()
	h.prepareEcosystemMutationApproval(c, owner, openClawEcosystemEffect{
		Action:           openClawRefreshAction,
		CurrentPath:      currentPath,
		CurrentSignature: currentSignature,
		TargetPath:       currentPath,
		TargetSignature:  currentSignature,
	})
}

func (h *Handler) UploadOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	if !h.acquireOpenClawUploadSlot(c) {
		return
	}
	defer h.releaseOpenClawUploadSlot()
	inspection, err := inspectOpenClawEcosystemUpload(c)
	if err != nil {
		writeOpenClawEcosystemUploadError(c, err)
		return
	}
	source, err := inspection.file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open uploaded ecosystem file"})
		return
	}
	defer source.Close()

	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	currentPath, currentSignature := openClaw.ecosystemState()
	authorization := mergeEcosystemAuthorization(c, EcosystemMutationAuthorization{
		IdempotencyKey:        c.PostForm("idempotencyKey"),
		TaskID:                c.PostForm("taskId"),
		ApprovalSourceID:      c.PostForm("approvalSourceId"),
		ApprovalBindingDigest: c.PostForm("approvalBindingDigest"),
	})
	effect := openClawEcosystemEffect{
		Action:                openClawUploadAction,
		CurrentPath:           currentPath,
		CurrentSignature:      currentSignature,
		TargetPath:            openClawManagedArchiveTarget,
		UploadedContentDigest: inspection.contentDigest,
		UploadedSize:          inspection.size,
	}
	if !h.authorizeEcosystemMutation(c, owner, authorization, effect) {
		return
	}
	if !recheckEcosystemEmergencyStop(c) {
		return
	}

	dest, err := openClaw.persistManagedArchive(source, inspection.contentDigest, inspection.size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "failed to persist OpenClaw archive in durable storage")})
		return
	}
	if !recheckEcosystemEmergencyStop(c) {
		return
	}
	prepared, err := openClaw.prepareEcosystemPath(dest, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded OpenClaw ecosystem is invalid or does not meet safety requirements"})
		return
	}
	previousDigest, err := openClaw.snapshotPreviousArchive(currentPath)
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	if prepared.previousPath != currentPath ||
		prepared.previousSignature != currentSignature ||
		prepared.targetPath != dest {
		writeEcosystemMutationError(c, ErrEcosystemMutationConflict)
		return
	}
	if err := validateOpenClawEcosystemPath(prepared.targetPath); err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	proceed, err := commitEcosystemMutation(c, openClaw, func() error {
		return openClaw.commitManagedArchiveSelectionLocked(prepared, inspection.contentDigest, previousDigest)
	})
	if !proceed {
		return
	}
	if err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

// PrepareUploadOpenClawEcosystem validates and hashes the submitted archive
// without persisting it, then returns an approval bound to that exact content
// and the current configured ecosystem revision. The browser immediately
// submits the same File again to the mutation endpoint after preparation.
func (h *Handler) PrepareUploadOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	if !h.acquireOpenClawUploadSlot(c) {
		return
	}
	defer h.releaseOpenClawUploadSlot()
	inspection, err := inspectOpenClawEcosystemUpload(c)
	if err != nil {
		writeOpenClawEcosystemUploadError(c, err)
		return
	}
	openClaw, ok := h.registry.OpenClawAdapter()
	if !ok || openClaw == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "openclaw runtime is not registered"})
		return
	}
	currentPath, currentSignature := openClaw.ecosystemState()
	h.prepareEcosystemMutationApproval(c, owner, openClawEcosystemEffect{
		Action:                openClawUploadAction,
		CurrentPath:           currentPath,
		CurrentSignature:      currentSignature,
		TargetPath:            openClawManagedArchiveTarget,
		UploadedContentDigest: inspection.contentDigest,
		UploadedSize:          inspection.size,
	})
}

type openClawEcosystemUploadInspection struct {
	file          *multipart.FileHeader
	contentDigest string
	size          int64
}

type openClawEcosystemUploadError struct {
	status  int
	message string
}

func (e *openClawEcosystemUploadError) Error() string { return e.message }

func (h *Handler) acquireOpenClawUploadSlot(c *gin.Context) bool {
	select {
	case h.uploadSlots <- struct{}{}:
		return true
	default:
		c.Header("Retry-After", "1")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many OpenClaw ecosystem uploads are being inspected"})
		return false
	}
}

func (h *Handler) releaseOpenClawUploadSlot() {
	<-h.uploadSlots
}

func inspectOpenClawEcosystemUpload(c *gin.Context) (openClawEcosystemUploadInspection, error) {
	if c.Request.ContentLength > maxOpenClawEcosystemRequestBytes {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusRequestEntityTooLarge, "openclaw ecosystem upload is too large"}
	}
	// Content-Length is optional for chunked requests, so enforce the same
	// bound while Gin parses the multipart stream.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOpenClawEcosystemRequestBytes)
	file, err := c.FormFile("ecosystem")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusRequestEntityTooLarge, "openclaw ecosystem upload is too large"}
		}
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "missing ecosystem zip upload field 'ecosystem'"}
	}
	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "missing uploaded ecosystem filename"}
	}
	if !strings.EqualFold(filepath.Ext(filename), ".zip") {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "openclaw ecosystem upload must be a zip file"}
	}
	if file.Size <= 0 {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "openclaw ecosystem upload is empty"}
	}
	if file.Size > maxOpenClawEcosystemUploadBytes {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusRequestEntityTooLarge, "openclaw ecosystem zip is too large"}
	}
	source, err := file.Open()
	if err != nil {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "failed to open uploaded ecosystem file"}
	}
	defer source.Close()
	if err := validateOpenClawZipReader(source, file.Size); err != nil {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "openclaw ecosystem zip is invalid or does not meet safety requirements"}
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "failed to inspect uploaded ecosystem file"}
	}
	contentHash := sha256.New()
	inspected, err := io.Copy(contentHash, io.LimitReader(source, maxOpenClawEcosystemUploadBytes+1))
	if err != nil || inspected != file.Size || inspected > maxOpenClawEcosystemUploadBytes {
		return openClawEcosystemUploadInspection{}, &openClawEcosystemUploadError{http.StatusBadRequest, "failed to inspect uploaded ecosystem file"}
	}
	return openClawEcosystemUploadInspection{
		file:          file,
		contentDigest: hex.EncodeToString(contentHash.Sum(nil)),
		size:          inspected,
	}, nil
}

func writeOpenClawEcosystemUploadError(c *gin.Context, err error) {
	var uploadErr *openClawEcosystemUploadError
	if errors.As(err, &uploadErr) {
		c.JSON(uploadErr.status, gin.H{"error": uploadErr.message})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "failed to inspect uploaded ecosystem file"})
}

func (h *Handler) prepareEcosystemMutationApproval(
	c *gin.Context,
	owner string,
	effect openClawEcosystemEffect,
) {
	if h.preparer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OpenClaw ecosystem approval preparation is unavailable",
		})
		return
	}
	digest, err := ecosystemMutationEffectDigest(owner, owner, effect)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "OpenClaw ecosystem mutation could not be prepared safely",
		})
		return
	}
	// The task ID is assigned on the server. It is an execution-ledger
	// correlation key, not a caller-controlled source of authority.
	taskID := "agent-runtime-openclaw-" + strings.ReplaceAll(effect.Action, ".", "-") + "-" + uuid.NewString()
	authorization, err := h.preparer.PrepareEcosystemMutationApproval(owner, taskID, digest)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OpenClaw ecosystem approval preparation is unavailable",
		})
		return
	}
	authorization, err = normalizeEcosystemAuthorization(authorization)
	if err != nil || authorization.TaskID != taskID || authorization.ApprovalBindingDigest != digest {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OpenClaw ecosystem approval preparation is unavailable",
		})
		return
	}
	c.JSON(http.StatusOK, authorization)
}

func mergeEcosystemAuthorization(
	c *gin.Context,
	value EcosystemMutationAuthorization,
) EcosystemMutationAuthorization {
	if strings.TrimSpace(value.IdempotencyKey) == "" {
		value.IdempotencyKey = c.GetHeader("X-HAI-Idempotency-Key")
	}
	if strings.TrimSpace(value.TaskID) == "" {
		value.TaskID = c.GetHeader("X-HAI-Task-ID")
	}
	if strings.TrimSpace(value.ApprovalSourceID) == "" {
		value.ApprovalSourceID = c.GetHeader("X-HAI-Approval-Source")
	}
	if strings.TrimSpace(value.ApprovalBindingDigest) == "" {
		value.ApprovalBindingDigest = c.GetHeader("X-HAI-Approval-Binding-Digest")
	}
	return value
}

func (h *Handler) authorizeEcosystemMutation(
	c *gin.Context,
	owner string,
	authorization EcosystemMutationAuthorization,
	effect openClawEcosystemEffect,
) bool {
	if h.authorizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": ErrEcosystemAuthorizationUnavailable.Error(),
		})
		return false
	}
	request, executionTarget, err := buildEcosystemMutationAuthorizationRequest(
		owner,
		owner,
		authorization,
		effect,
	)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": ErrEcosystemAuthorizationDenied.Error()})
		return false
	}
	receipt, err := h.authorizer.AuthorizeAndConsumeEcosystemMutation(
		c.Request.Context(),
		request,
		"agentruntime.openclaw-ecosystem",
		executionTarget,
	)
	if err != nil || validateEcosystemMutationReceipt(receipt, request, h.now()) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": ErrEcosystemAuthorizationDenied.Error()})
		return false
	}
	return true
}

func recheckEcosystemEmergencyStop(c *gin.Context) bool {
	decision := safety.EvaluateEmergencyStop()
	if !decision.Active {
		return true
	}
	c.JSON(http.StatusLocked, gin.H{
		"error":  "emergency stop blocks OpenClaw ecosystem mutation",
		"reason": decision.Reason,
	})
	return false
}

// commitEcosystemMutation uses the same adapter-state and emergency-stop
// fences for every HTTP mutation before applying its already-prepared change.
func commitEcosystemMutation(
	c *gin.Context,
	openClaw *openClawAdapter,
	mutate func() error,
) (bool, error) {
	decision, err := openClaw.withEcosystemCommitFence(mutate)
	if decision.Active {
		c.JSON(http.StatusLocked, gin.H{
			"error":  "emergency stop blocks OpenClaw ecosystem mutation",
			"reason": decision.Reason,
		})
		return false, nil
	}
	return true, err
}

func writeEcosystemMutationError(c *gin.Context, err error) {
	if errors.Is(err, ErrEcosystemMutationConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrEcosystemMutationConflict.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "runtime ecosystem mutation could not be completed")})
}

func runtimeOwner(c *gin.Context) (string, bool) {
	value, ok := c.Get(identity.ContextSubjectKey)
	if ok {
		if owner, ok := value.(string); ok && strings.TrimSpace(owner) != "" {
			return strings.TrimSpace(owner), true
		}
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for runtime control actions"})
	return "", false
}

func validateOpenClawZip(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	return validateOpenClawZipFiles(reader.File)
}

func validateOpenClawZipReader(reader io.ReaderAt, size int64) error {
	if reader == nil || size <= 0 {
		return errors.New("openclaw ecosystem zip is empty")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return err
	}
	return validateOpenClawZipFiles(archive.File)
}

func validateOpenClawZipFiles(files []*zip.File) error {
	if len(files) == 0 {
		return errors.New("openclaw ecosystem zip is empty")
	}
	if len(files) > maxOpenClawZipEntries {
		return fmt.Errorf("openclaw ecosystem zip has too many entries (maximum %d)", maxOpenClawZipEntries)
	}
	hasOpenClawName := false
	hasSkillMarker := false
	seenPaths := make(map[string]struct{}, len(files))
	var totalUncompressed uint64
	for _, file := range files {
		normalized, ok := safeZipEntryName(file.Name)
		if !ok {
			return errors.New("openclaw ecosystem zip contains an unsafe path")
		}
		// Windows is case-insensitive by default. Treat case-only aliases as
		// duplicates too, so an uploaded archive cannot resolve differently on
		// the host than it did during validation.
		pathKey := strings.ToLower(normalized)
		if _, duplicate := seenPaths[pathKey]; duplicate {
			return fmt.Errorf("openclaw ecosystem zip contains a duplicate entry: %s", normalized)
		}
		seenPaths[pathKey] = struct{}{}
		if file.UncompressedSize64 > maxOpenClawZipUncompressedBytes-totalUncompressed {
			return fmt.Errorf("openclaw ecosystem zip expands beyond the %d byte inspection limit", maxOpenClawZipUncompressedBytes)
		}
		totalUncompressed += file.UncompressedSize64
		if openClawZipCompressionRatioExceeded(file.UncompressedSize64, file.CompressedSize64) {
			return fmt.Errorf("openclaw ecosystem zip entry is compressed beyond the %d:1 inspection limit: %s", maxOpenClawZipCompressionRatio, normalized)
		}
		parts := strings.Split(normalized, "/")
		if isOpenClawRootPackage(parts) {
			if packageLooksLikeOpenClaw(file) {
				hasOpenClawName = true
			}
		}
		if openClawZipEntryLooksLikeSkill(parts) {
			hasSkillMarker = true
		}
	}
	if !hasOpenClawName && !hasSkillMarker {
		return errors.New("openclaw ecosystem zip does not look like an OpenClaw checkout")
	}
	return nil
}

func openClawZipCompressionRatioExceeded(uncompressedSize, compressedSize uint64) bool {
	if uncompressedSize == 0 || compressedSize == 0 {
		return false
	}
	minimumCompressedSize := uncompressedSize / maxOpenClawZipCompressionRatio
	if uncompressedSize%maxOpenClawZipCompressionRatio != 0 {
		minimumCompressedSize++
	}
	return compressedSize < minimumCompressedSize
}

func safeZipEntryName(name string) (string, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if normalized == "" || strings.Contains(normalized, "\x00") {
		return "", false
	}
	if strings.HasPrefix(normalized, "/") || len(normalized) >= 2 && normalized[1] == ':' {
		return "", false
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func packageLooksLikeOpenClaw(file *zip.File) bool {
	reader, err := file.Open()
	if err != nil {
		return false
	}
	defer reader.Close()
	var metadata struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(reader, 128*1024)).Decode(&metadata); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(metadata.Name)), "openclaw")
}

func openClawZipEntryLooksLikeSkill(parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	if parts[len(parts)-1] != "SKILL.md" {
		return false
	}
	for index := 0; index < len(parts)-2; index++ {
		if parts[index] == "skills" && parts[index+1] != "" {
			return true
		}
	}
	return false
}

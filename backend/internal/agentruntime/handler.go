package agentruntime

import (
	"archive/zip"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/safety"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	maxOpenClawZipUploadBytes       = int64(750 * 1024 * 1024)
	openClawMultipartOverhead       = int64(1 << 20)
)

type Handler struct {
	registry   *Registry
	authorizer EcosystemMutationAuthorizer
	now        func() time.Time
	mutationMu sync.Mutex
}

func NewHandler(registry *Registry) *Handler {
	return NewHandlerWithEcosystemMutationAuthorizer(registry, nil)
}

func NewHandlerWithEcosystemMutationAuthorizer(
	registry *Registry,
	authorizer EcosystemMutationAuthorizer,
) *Handler {
	return &Handler{
		registry:   registry,
		authorizer: authorizer,
		now:        time.Now,
	}
}

func (h *Handler) Registry(c *gin.Context) {
	c.JSON(http.StatusOK, h.registry.List())
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
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	if !recheckEcosystemEmergencyStop(c) {
		return
	}
	if err := openClaw.applyPreparedEcosystemPath(prepared); err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
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
	if !recheckEcosystemEmergencyStop(c) {
		return
	}
	if err := openClaw.refreshEcosystemInventoryIfCurrent(currentPath, currentSignature); err != nil {
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
}

func (h *Handler) UploadOpenClawEcosystem(c *gin.Context) {
	owner, ok := runtimeOwner(c)
	if !ok {
		return
	}
	if c.Request.ContentLength > maxOpenClawZipUploadBytes+openClawMultipartOverhead {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "openclaw ecosystem zip is too large"})
		return
	}
	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxOpenClawZipUploadBytes+openClawMultipartOverhead,
	)
	file, err := c.FormFile("ecosystem")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "openclaw ecosystem zip is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing ecosystem zip upload field 'ecosystem'"})
		return
	}

	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing uploaded ecosystem filename"})
		return
	}
	if !strings.EqualFold(filepath.Ext(filename), ".zip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "openclaw ecosystem upload must be a zip file"})
		return
	}
	if file.Size <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "openclaw ecosystem upload is empty"})
		return
	}
	if file.Size > maxOpenClawZipUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "openclaw ecosystem zip is too large"})
		return
	}

	source, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open uploaded ecosystem file"})
		return
	}
	defer source.Close()
	if err := validateOpenClawZipReader(source, file.Size); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to inspect uploaded ecosystem file"})
		return
	}
	contentHash := sha256.New()
	inspected, err := io.Copy(contentHash, io.LimitReader(source, maxOpenClawZipUploadBytes+1))
	if err != nil || inspected != file.Size || inspected > maxOpenClawZipUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to inspect uploaded ecosystem file"})
		return
	}
	contentDigest := hex.EncodeToString(contentHash.Sum(nil))
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to inspect uploaded ecosystem file"})
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
	dest := filepath.Join(
		os.TempDir(),
		"openclaw-ecosystem-"+uuid.NewString()+".zip",
	)
	deleteManagedPath := ""
	if isOpenClawUploadArtifactPath(currentPath) && !sameFilePath(currentPath, dest) {
		deleteManagedPath = currentPath
	}
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
		TargetPath:            dest,
		UploadedContentDigest: contentDigest,
		UploadedSize:          inspected,
		DeleteManagedPath:     deleteManagedPath,
	}
	if !h.authorizeEcosystemMutation(c, owner, authorization, effect) {
		return
	}
	if !recheckEcosystemEmergencyStop(c) {
		return
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create managed ecosystem storage"})
		return
	}
	copiedHash := sha256.New()
	copied, copyErr := io.Copy(io.MultiWriter(f, copiedHash), source)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || copied != inspected ||
		hex.EncodeToString(copiedHash.Sum(nil)) != contentDigest {
		_ = os.Remove(dest)
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded ecosystem changed while it was being persisted"})
		return
	}
	if !recheckEcosystemEmergencyStop(c) {
		_ = os.Remove(dest)
		return
	}
	prepared, err := openClaw.prepareEcosystemPath(dest, true)
	if err != nil {
		_ = os.Remove(dest)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if prepared.previousPath != currentPath ||
		prepared.previousSignature != currentSignature ||
		prepared.deleteManagedPath != deleteManagedPath {
		_ = os.Remove(dest)
		writeEcosystemMutationError(c, ErrEcosystemMutationConflict)
		return
	}
	if err := openClaw.applyPreparedEcosystemPath(prepared); err != nil {
		_ = os.Remove(dest)
		writeEcosystemMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, openClaw.Info())
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

func writeEcosystemMutationError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, ErrEcosystemMutationConflict) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"error": err.Error()})
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
		if file.UncompressedSize64 > 0 && file.CompressedSize64 > 0 && file.UncompressedSize64 > file.CompressedSize64*maxOpenClawZipCompressionRatio {
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

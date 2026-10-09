package accountfeed

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler serves the Account Feeds API (§10.19). Feeds are read-only bridges;
// no route ever fakes provider access or connected status.
type Handler struct {
	reg   *Registry
	perms *PermissionRegistry
	space string
}

// NewHandler builds a handler over a registry.
func NewHandler(reg *Registry, _ string, workspaceID string) *Handler {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = "local"
	}
	return &Handler{reg: reg, perms: NewPermissionRegistry(), space: workspaceID}
}

func (h *Handler) ownerID(c *gin.Context) string {
	if sub, ok := c.Get("subject"); ok {
		if s, ok := sub.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// List returns feed health for all registered feeds.
func (h *Handler) List(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	feeds, err := h.reg.ListContext(c.Request.Context(), FeedScope{owner, h.space})
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, gin.H{"feeds": feeds})
}

// Bridges returns the provider bridge contracts with truthful connection status.
func (h *Handler) Bridges(c *gin.Context) {
	bridges := Bridges()
	type view struct {
		BridgeContract
		Status ConnectionStatus `json:"connectionStatus"`
	}
	out := make([]view, 0, len(bridges))
	for _, b := range bridges {
		out = append(out, view{BridgeContract: b, Status: b.ConnectionStatus()})
	}
	c.JSON(http.StatusOK, gin.H{"bridges": out})
}

// Permissions returns the account permission registry.
func (h *Handler) Permissions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"permissions": h.perms.Permissions()})
}

type registerRequest struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	AccountLabel  string `json:"accountLabel"`
	SourceType    string `json:"sourceType"`
	Path          string `json:"path"`
	URL           string `json:"url"`
	ProjectKey    string `json:"projectKey"`
	OperationType string `json:"operationType"`
	Enabled       bool   `json:"enabled"`
}

// Create registers a new feed.
func (h *Handler) Create(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	feed := Feed{
		Name:          req.Name,
		Provider:      req.Provider,
		AccountLabel:  req.AccountLabel,
		SourceType:    SourceType(req.SourceType),
		Path:          req.Path,
		URL:           req.URL,
		ProjectKey:    req.ProjectKey,
		OperationType: req.OperationType,
		OwnerUserID:   owner,
		WorkspaceID:   h.space,
		Enabled:       req.Enabled,
	}
	created, err := h.reg.RegisterContext(c.Request.Context(), feed)
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// Get returns a single feed.
func (h *Handler) Get(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	feed, err := h.reg.GetContext(c.Request.Context(), FeedScope{owner, h.space}, id)
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, feed)
}

type patchRequest struct {
	Enabled       *bool   `json:"enabled"`
	Name          *string `json:"name"`
	OperationType *string `json:"operationType"`
}

// Patch updates mutable feed fields.
func (h *Handler) Patch(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req patchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	feed, err := h.reg.PatchContext(c.Request.Context(), FeedScope{owner, h.space}, id, FeedPatch{req.Enabled, req.Name, req.OperationType})
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, feed)
}

// Sync syncs a single feed.
func (h *Handler) Sync(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	rep, err := h.reg.SyncContext(c.Request.Context(), FeedScope{owner, h.space}, id)
	if err != nil {
		respondFeedError(c, err, gin.H{"report": rep})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// SyncDue syncs all enabled feeds.
func (h *Handler) SyncDue(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	reports, err := h.reg.SyncDueContext(c.Request.Context(), FeedScope{owner, h.space})
	if err != nil {
		respondFeedError(c, err, gin.H{"reports": reports})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": reports})
}

// IdentityPreview reads local source identities without starting an import.
func (h *Handler) IdentityPreview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	report, err := h.reg.PreviewSourceIdentity(c.Request.Context(), FeedScope{owner, h.space}, id)
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, report)
}

// Audit returns a feed's audit trail.
func (h *Handler) Audit(c *gin.Context) {
	owner, ok := h.requireOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	audit, err := h.reg.AuditContext(c.Request.Context(), FeedScope{owner, h.space}, id)
	if err != nil {
		respondFeedError(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, gin.H{"audit": audit})
}

func (h *Handler) requireOwner(c *gin.Context) (string, bool) {
	owner := h.ownerID(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity is required"})
		return "", false
	}
	return owner, true
}

func respondFeedError(c *gin.Context, err error, payload gin.H) {
	status, message := http.StatusServiceUnavailable, "Feed storage could not be verified. Refresh the status before retrying; inspect local operator diagnostics if it persists."
	code := "feed_storage_unavailable"
	switch {
	case errors.Is(err, ErrFeedNotFound):
		status, message = http.StatusNotFound, "feed not found"
		code = "feed_not_found"
	case errors.Is(err, ErrFeedSyncBusy):
		status, message = http.StatusConflict, ErrFeedSyncBusy.Error()
		code = "feed_sync_busy"
	case errors.Is(err, ErrFeedDisabled):
		status, message = http.StatusConflict, ErrFeedDisabled.Error()
		code = "feed_disabled"
	case errors.Is(err, ErrIdentityPreviewUnsupported):
		status, message = http.StatusConflict, ErrIdentityPreviewUnsupported.Error()
		code = "identity_preview_unsupported"
	case errors.Is(err, ErrIdentityPreviewChanged):
		status, message = http.StatusConflict, ErrIdentityPreviewChanged.Error()
		code = "identity_preview_changed"
	case errors.Is(err, ErrFeedInvalid):
		status, message = http.StatusBadRequest, "Check the feed name, provider, source and scope."
		code = "feed_invalid"
	}
	if payload == nil {
		payload = gin.H{}
	}
	payload["error"] = message
	payload["code"] = code
	c.JSON(status, payload)
}

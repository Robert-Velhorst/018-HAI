package source

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/whispercpp"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	service           Service
	transcriber       whispercpp.Service
	documentExtractor docling.Service
}

type ownerScopedSources interface {
	SourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error)
}

type sourceHistoryService interface {
	SyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	AuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error)
}

const (
	defaultExtractionPageLimit = 100
	maxExtractionPageLimit     = 500
	googleOAuthStateCookieName = "hai_google_oauth_state"
	googleOAuthStateCookieAge  = 10 * 60
)

type mutableSourceLookup interface {
	MutableSourceForOwner(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error)
	MutableExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error)
}

func NewHandler(service Service, transcribers ...whispercpp.Service) *Handler {
	transcriber := whispercpp.DefaultService()
	if len(transcribers) > 0 && transcribers[0] != nil {
		transcriber = transcribers[0]
	}
	return NewHandlerWithDocumentExtractor(service, docling.DefaultService(), transcriber)
}

func NewHandlerWithDocumentExtractor(service Service, extractor docling.Service, transcribers ...whispercpp.Service) *Handler {
	transcriber := whispercpp.DefaultService()
	if len(transcribers) > 0 && transcribers[0] != nil {
		transcriber = transcribers[0]
	}
	if extractor == nil {
		extractor = docling.DefaultService()
	}
	return &Handler{service: service, transcriber: transcriber, documentExtractor: extractor}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

func (h *Handler) Connectors(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	connectors, err := h.service.Connectors()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "source connectors are unavailable")})
		return
	}
	c.JSON(http.StatusOK, connectors)
}

// StartGoogleOAuth returns the Google consent URL for a Google-backed source. The UI
// opens the returned url so the user authorizes in their own browser.
func (h *Handler) StartGoogleOAuth(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	sourceID, err := uuid.Parse(c.Query("sourceId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid sourceId query parameter is required"})
		return
	}
	if !h.requireMutableSource(c, sourceID) {
		return
	}
	url, err := h.service.StartGoogleOAuth(sourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "Google connection could not be started")})
		return
	}
	state, err := googleOAuthStateFromAuthorizeURL(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Google connection could not be started"})
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(googleOAuthStateCookieName, state, googleOAuthStateCookieAge, "/api/v1/sources/oauth/google", "", googleOAuthCookieSecure(c), true)
	c.JSON(http.StatusOK, gin.H{"authorizeUrl": url})
}

// GoogleOAuthCallback is the redirect target the user's browser returns to
// after Google consent. The browser may not carry a HAI session, so the callback
// is protected by signed, expiring state. On success it returns the browser to
// the connected-sources page.
func (h *Handler) GoogleOAuthCallback(c *gin.Context) {
	state := c.Query("state")
	if !googleOAuthStateCookieMatches(c, state) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if _, err := verifyState(state); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	defer clearGoogleOAuthStateCookie(c)
	if oauthErr := c.Query("error"); oauthErr != "" {
		c.Redirect(http.StatusFound, "/connected-sources?oauth=denied")
		return
	}
	_, err := h.service.CompleteGoogleOAuth(c.Request.Context(), c.Query("code"), state)
	if err != nil {
		c.Redirect(http.StatusFound, "/connected-sources?oauth=error")
		return
	}
	c.Redirect(http.StatusFound, "/connected-sources?oauth=connected")
}

func googleOAuthStateFromAuthorizeURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	state := strings.TrimSpace(parsed.Query().Get("state"))
	if state == "" {
		return "", errors.New("Google authorization URL omitted state")
	}
	return state, nil
}

func googleOAuthStateCookieMatches(c *gin.Context, state string) bool {
	stored, err := c.Cookie(googleOAuthStateCookieName)
	if err != nil || strings.TrimSpace(state) == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(state)) == 1
}

func clearGoogleOAuthStateCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(googleOAuthStateCookieName, "", -1, "/api/v1/sources/oauth/google", "", googleOAuthCookieSecure(c), true)
}

func googleOAuthCookieSecure(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

func (h *Handler) CreateSource(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	var request CreateSourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connected source request is invalid"})
		return
	}
	request.OwnerIdentity = owner
	source, err := h.service.CreateSource(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "connected source could not be created")})
		return
	}
	c.JSON(http.StatusCreated, source)
}

func (h *Handler) Sources(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	includeDisabled, _ := strconv.ParseBool(c.Query("includeDisabled"))
	sources, err := h.sourcesForOwner(owner, includeDisabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected sources are unavailable")})
		return
	}
	c.JSON(http.StatusOK, filterVisibleSources(sources, owner))
}

func (h *Handler) SyncJobs(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	var sourceID *uuid.UUID
	if raw := c.Query("sourceId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sourceId"})
			return
		}
		sourceID = &parsed
		if !h.requireSourceAccess(c, parsed) {
			return
		}
	}
	if sourceID == nil {
		visibleSourceIDs, err := h.visibleSourceIDs(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected source history is unavailable")})
			return
		}
		jobs, err := h.recentSyncJobs(sourceIDsFromSet(visibleSourceIDs))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected source history is unavailable")})
			return
		}
		c.JSON(http.StatusOK, filterManualSyncJobsForOwner(jobs, owner))
		return
	}
	jobs, err := h.recentSyncJobs([]uuid.UUID{*sourceID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected source history is unavailable")})
		return
	}
	c.JSON(http.StatusOK, filterManualSyncJobsForOwner(jobs, owner))
}

func filterManualSyncJobsForOwner(jobs []models.SourceSyncJob, owner string) []models.SourceSyncJob {
	visible := make([]models.SourceSyncJob, 0, len(jobs))
	for _, job := range jobs {
		if job.Mode == ModeManualAsyncSync && job.OwnerIdentity != owner {
			continue
		}
		visible = append(visible, job)
	}
	return visible
}

// SubmitManualSync accepts manual source work into the durable queue. The
// request context is intentionally not propagated to the worker: after the
// atomic queue/history commit, a disconnected browser cannot cancel accepted
// work.
func (h *Handler) SubmitManualSync(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	sourceID, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, sourceID) {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	var request ManualSyncRequest
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "manual source sync request is invalid"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "manual source sync request must contain one JSON object"})
			return
		}
	}
	submission, ok := h.service.(ManualSyncSubmissionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable manual source sync is unavailable"})
		return
	}
	view, created, err := submission.SubmitManualSync(owner, sourceID, key, request)
	if err != nil {
		switch {
		case errors.Is(err, ErrManualSyncInvalidRequest):
			c.JSON(http.StatusBadRequest, gin.H{"error": "a valid Idempotency-Key and project key are required"})
		case errors.Is(err, ErrManualSyncWorkerUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable sync is unavailable; the request was not queued"})
		case errors.Is(err, ErrManualSyncIdempotencyConflict), errors.Is(err, ErrManualSyncAlreadyActive):
			c.JSON(http.StatusConflict, gin.H{"error": apierror.PublicMessage(err, "manual source sync could not be queued")})
		case errors.Is(err, ErrManualSyncSourceDisabled):
			c.JSON(http.StatusConflict, gin.H{"error": "source is paused or unavailable; resume it before syncing"})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "manual source sync could not be queued; retry with the same Idempotency-Key"})
		}
		return
	}
	if view == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "manual source sync was not accepted"})
		return
	}
	c.Header("Location", "/api/v1/sources/sync-jobs/"+view.ID)
	c.Header("Retry-After", "3")
	statusCode := http.StatusAccepted
	if !created && view.Status != "queued" && view.Status != "running" {
		statusCode = http.StatusOK
	}
	c.JSON(statusCode, view)
}

// ManualSyncJob returns status only when both the persisted job owner and its
// currently connected source owner match the authenticated identity.
func (h *Handler) ManualSyncJob(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sync job id"})
		return
	}
	submission, ok := h.service.(ManualSyncSubmissionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable manual source sync is unavailable"})
		return
	}
	view, err := submission.ManualSyncJobForOwner(owner, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "sync job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sync job status is unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}

func (h *Handler) ConnectionHealth(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireSourceAccess(c, id) {
		return
	}
	healthService, ok := h.service.(ConnectionHealthService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "connection health is not available"})
		return
	}
	health, err := healthService.ConnectionHealth(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "connection health is unavailable")})
		return
	}
	c.JSON(http.StatusOK, health)
}

// ConnectionHealths returns overview health only for sources visible to the
// authenticated owner. It is a local status derivation; it never probes or
// synchronizes external accounts.
func (h *Handler) ConnectionHealths(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	healthService, ok := h.service.(ConnectionHealthBatchService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "connection health is not available"})
		return
	}
	sources, err := h.sourcesForOwner(owner, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load connected sources"})
		return
	}
	health, err := healthService.ConnectionHealths(filterVisibleSources(sources, owner))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not derive connection health"})
		return
	}
	c.JSON(http.StatusOK, health)
}

func (h *Handler) UpdateSource(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	var request UpdateSourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connected source update request is invalid"})
		return
	}
	source, err := h.service.UpdateSource(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "connected source could not be updated")})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Sync(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	var request ImportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source sync request is invalid"})
		return
	}
	result, err := syncSourceWithContext(c.Request.Context(), h.service, id, request)
	if err != nil {
		if errors.Is(err, ErrSyncInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "source sync is already in progress"})
			return
		}
		if errors.Is(err, ErrInvalidSyncRequest) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sync request"})
			return
		}
		if errors.Is(err, ErrSourceSyncUnavailable) {
			c.JSON(http.StatusConflict, gin.H{"error": "source is paused or disabled; resume it before syncing"})
			return
		}
		if failure, recognized := classifySyncFailure(err); recognized {
			if failure.retryAfter > 0 {
				seconds := int64(failure.retryAfter / time.Second)
				if failure.retryAfter%time.Second != 0 {
					seconds++
				}
				c.Header("Retry-After", strconv.FormatInt(seconds, 10))
			}
			c.JSON(failure.statusCode, gin.H{
				"error":     failure.message,
				"retryable": failure.retryable,
			})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "source sync failed; inspect sync history for details"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// Transcribe invokes the opt-in local whisper.cpp runner for a configured
// whisper-audio source. It deliberately accepts no request body: the source's
// approved folder is the sole file scope, and the runner owns model/language
// configuration. The resulting text is persisted through the normal source
// sync path, preserving existing provenance, review, workflow, and audit gates.
func (h *Handler) Transcribe(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	if c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transcription uses the source's configured selected folder and accepts no caller-provided files, model, language, or audio"})
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	source, ok := h.mutableSource(c, id)
	if !ok {
		return
	}
	if source.ConnectorKey != "whisper-audio" || !source.LocalOnly {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transcription requires an enabled local-only whisper-audio source"})
		return
	}
	folder, err := selectedAudioFolder(source.SyncTarget)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "audio source folder is invalid")})
		return
	}
	transcripts, err := h.transcriber.Transcribe(c.Request.Context(), folder)
	if errors.Is(err, whispercpp.ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local whisper.cpp transcription runner is not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local whisper.cpp transcription could not complete"})
		return
	}
	items := make([]ImportItem, 0, len(transcripts))
	for _, transcript := range transcripts {
		items = append(items, ImportItem{
			ExternalID: "whisper:" + transcript.Path,
			Title:      filepath.Base(transcript.Path),
			Content:    transcript.Text,
			SourceURI:  "audio://selected-source/" + id.String() + "/" + transcript.Path,
			ItemType:   "audio_transcript",
			ProjectKey: source.DefaultProjectKey,
			Metadata:   "engine=whisper.cpp;model=" + transcript.ModelID + ";language=" + transcript.Language + ";audio_retained=false;consent=source_owner",
		})
	}
	result, err := syncSourceWithContext(c.Request.Context(), h.service, id, ImportRequest{Mode: ModeManualImport, Items: items, ProjectKey: source.DefaultProjectKey, controlledTranscription: true})
	if errors.Is(err, ErrSyncInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": "source sync is already in progress"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "transcription results could not be saved")})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExtractDocuments invokes the opt-in local Docling runner for a configured
// source. It accepts no browser supplied files, paths, models, or parser
// options; the source's approved folder remains the sole input scope.
func (h *Handler) ExtractDocuments(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	if c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document extraction uses the source's configured selected folder and accepts no caller-provided files, model, or parser options"})
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	source, ok := h.mutableSource(c, id)
	if !ok {
		return
	}
	if source.ConnectorKey != doclingDocumentsConnectorKey || !source.LocalOnly {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document extraction requires an enabled local-only Docling document source"})
		return
	}
	folder, err := selectedDocumentFolder(source.SyncTarget)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "document source folder is invalid")})
		return
	}
	documents, err := h.documentExtractor.Extract(c.Request.Context(), folder)
	if errors.Is(err, docling.ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local Docling document extractor is not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local Docling document extraction could not complete"})
		return
	}
	items := make([]ImportItem, 0, len(documents))
	for _, document := range documents {
		items = append(items, ImportItem{
			ExternalID: "docling:" + document.Path,
			Title:      filepath.Base(document.Path),
			Content:    document.Text,
			SourceURI:  "document://selected-source/" + id.String() + "/" + document.Path,
			ItemType:   "document_extraction",
			ProjectKey: source.DefaultProjectKey,
			Metadata:   "engine=docling;format=" + document.Format + ";page_count=" + strconv.Itoa(document.PageCount) + ";content_digest=" + document.ContentDigest + ";source_retained=false;consent=source_owner",
		})
	}
	result, err := syncSourceWithContext(c.Request.Context(), h.service, id, ImportRequest{Mode: ModeManualImport, Items: items, ProjectKey: source.DefaultProjectKey, controlledDocumentExtraction: true})
	if errors.Is(err, ErrSyncInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": "source sync is already in progress"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "document extraction results could not be saved")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Reindex(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	result, err := h.service.Reindex(id)
	if err != nil {
		if errors.Is(err, ErrSyncInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "source sync is already in progress"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "source reindex could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RunDueScheduledSyncs(c *gin.Context) {
	ownerIdentity, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	result, err := runOwnerScheduledSyncsWithContext(c.Request.Context(), h.service, time.Now().UTC(), ownerIdentity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "scheduled source sync could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Pause(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.service.Pause(id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "connected source could not be paused")})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Resume(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.service.Pause(id, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "connected source could not be resumed")})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Revoke(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	destructive, ok := h.service.(DestructiveEffectService)
	if !ok {
		writeDestructiveEffectError(c, ErrDestructiveAuthorizationRequired)
		return
	}
	source, err := destructive.RevokeAuthorized(
		c.Request.Context(),
		id,
		destructiveAuthorization(c),
	)
	if err != nil {
		writeDestructiveEffectError(c, err)
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Search(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	var request SearchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connected source search request is invalid"})
		return
	}
	request.OwnerIdentity = owner
	ownedSourceIDs, err := h.ownedSourceIDs(owner)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected-source access is unavailable")})
		return
	}
	result, err := h.service.Search(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected source search is unavailable")})
		return
	}
	if result == nil {
		result = &SearchResult{Query: request.Query, ProjectKey: request.ProjectKey}
	}
	result.UsedContext = filterRankedExtractions(result.UsedContext, ownedSourceIDs)
	result.Explanation = "Search results are restricted to sources owned by the authenticated account; unowned legacy and other-owner records are excluded."
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Extractions(c *gin.Context) {
	owner, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	limit, err := extractionPageLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid extraction limit"})
		return
	}
	ownedSourceIDs, err := h.ownedSourceIDs(owner)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "connected-source access is unavailable"})
		return
	}
	extractions, err := h.service.ExtractionsForOwner(owner, c.Query("projectKey"), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "source extractions are unavailable"})
		return
	}
	extractions = filterExtractionsForSources(extractions, ownedSourceIDs)
	total := len(extractions)
	if len(extractions) > limit {
		extractions = extractions[:limit]
	}
	c.Header("X-Total-Count", strconv.Itoa(total))
	c.Header("X-Result-Limit", strconv.Itoa(limit))
	c.JSON(http.StatusOK, extractions)
}

func extractionPageLimit(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return defaultExtractionPageLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxExtractionPageLimit {
		return 0, errors.New("limit must be between 1 and 500")
	}
	return limit, nil
}

func (h *Handler) UpdateExtraction(c *gin.Context) {
	ownerIdentity, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	lookup, ok := h.service.(mutableSourceLookup)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped source-extraction access is unavailable", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	current, err := lookup.MutableExtractionForOwner(id, ownerIdentity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "source extraction not found", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify source extraction ownership", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !validateCorrectionIdempotencyKey(idempotencyKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key must contain 16 to 128 printable ASCII characters", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	rawRevision := strings.TrimSpace(c.GetHeader("If-Match"))
	if rawRevision == "" {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "If-Match with the current extraction revision is required", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	expectedRevision, err := parseExtractionCorrectionRevision(rawRevision)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "If-Match must contain the extraction revision", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	if !current.UpdatedAt.Equal(expectedRevision) {
		writeExtractionCorrectionError(c, ErrExtractionPatchConflict)
		return
	}
	var request ExtractionPatch
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "extraction update request is invalid"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "extraction update request must contain one JSON object"})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "extraction update request is invalid", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	patchService, ok := h.service.(ExtractionCorrectionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable source extraction correction is unavailable", "intentPersisted": false, "patchSaved": false, "recoveryPending": false})
		return
	}
	correction, err := patchService.SubmitExtractionCorrection(ownerIdentity, id, expectedRevision, request, idempotencyKey)
	if err != nil {
		writeExtractionCorrectionError(c, err)
		return
	}
	status := http.StatusAccepted
	switch correction.Status {
	case models.SourceExtractionCorrectionCompleted:
		status = http.StatusOK
	case models.SourceExtractionCorrectionConflict:
		status = http.StatusConflict
	case models.SourceExtractionCorrectionFailed:
		status = http.StatusServiceUnavailable
	case "running", models.SourceExtractionCorrectionPending:
		status = http.StatusAccepted
	}
	if correction.ID != "" {
		c.Header("Location", "/api/v1/sources/extraction-corrections/"+correction.ID)
	}
	c.JSON(status, correction)
}

func (h *Handler) ExtractionCorrection(c *gin.Context) {
	ownerIdentity, ok := requireSourceOwner(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	service, ok := h.service.(ExtractionCorrectionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "durable source extraction correction is unavailable"})
		return
	}
	correction, err := service.ExtractionCorrectionForOwner(ownerIdentity, id)
	if err != nil {
		writeExtractionCorrectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, correction)
}

func writeExtractionCorrectionError(c *gin.Context, err error) {
	base := gin.H{"intentPersisted": false, "patchSaved": false, "recoveryPending": false}
	var unknown *ExtractionCorrectionPersistenceUnknownError
	if errors.As(err, &unknown) {
		base["error"] = "source_correction_persistence_unknown"
		base["intentPersisted"] = nil
		base["patchSaved"] = nil
		base["recoveryPending"] = nil
		base["correctionId"] = unknown.CorrectionID.String()
		c.Header("Location", "/api/v1/sources/extraction-corrections/"+unknown.CorrectionID.String())
		base["message"] = "HAI could not confirm whether the correction was saved. Check its status before retrying."
		c.JSON(http.StatusServiceUnavailable, base)
		return
	}
	switch {
	case errors.Is(err, ErrExtractionPatchConflict):
		base["error"] = "source_extraction_revision_conflict"
		base["message"] = "The extraction changed before this correction was saved. Refresh it and review the new revision."
		c.JSON(http.StatusConflict, base)
	case errors.Is(err, ErrArchivedExtractionPatch):
		base["error"] = "source_extraction_archived"
		base["message"] = "Restore the archived extraction before editing it."
		c.JSON(http.StatusConflict, base)
	case errors.Is(err, ErrExtractionCorrectionIdempotency):
		base["error"] = "idempotency_key_reused"
		base["message"] = "This Idempotency-Key was already used for a different correction."
		c.JSON(http.StatusConflict, base)
	case errors.Is(err, ErrExtractionCorrectionActive):
		base["error"] = "source_correction_already_pending"
		base["message"] = "Another correction is already pending for this extraction. Check its status before starting another."
		c.JSON(http.StatusConflict, base)
	case errors.Is(err, ErrSourceExtractionNotFound), errors.Is(err, ErrExtractionCorrectionNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		base["error"] = "source_extraction_not_found"
		c.JSON(http.StatusNotFound, base)
	case errors.Is(err, ErrExtractionCorrectionNotSaved), errors.Is(err, ErrExtractionPatchUnavailable), errors.Is(err, ErrExtractionCorrectionWorkerNotReady):
		base["error"] = "source_correction_not_saved"
		base["message"] = "The correction was not saved. No background recovery is pending."
		c.JSON(http.StatusServiceUnavailable, base)
	case errors.Is(err, ErrInvalidExtractionPatch):
		base["error"] = "invalid_extraction_correction"
		c.JSON(http.StatusBadRequest, base)
	default:
		base["error"] = "source_correction_unavailable"
		base["message"] = "The correction could not be saved. No background recovery is confirmed."
		c.JSON(http.StatusInternalServerError, base)
	}
}

func (h *Handler) ArchiveExtraction(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableExtraction(c, id) {
		return
	}
	extraction, err := h.service.ArchiveExtraction(id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "extraction could not be archived")})
		return
	}
	c.JSON(http.StatusOK, extraction)
}

func (h *Handler) DeleteExtraction(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableExtraction(c, id) {
		return
	}
	destructive, ok := h.service.(DestructiveEffectService)
	if !ok {
		writeDestructiveEffectError(c, ErrDestructiveAuthorizationRequired)
		return
	}
	if err := destructive.DeleteExtractionAuthorized(
		c.Request.Context(),
		id,
		destructiveAuthorization(c),
	); err != nil {
		writeDestructiveEffectError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) AuditLogs(c *gin.Context) {
	if _, ok := requireSourceOwner(c); !ok {
		return
	}
	var sourceID *uuid.UUID
	if raw := c.Query("sourceId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sourceId"})
			return
		}
		sourceID = &parsed
		if !h.requireSourceAccess(c, parsed) {
			return
		}
	}
	if sourceID == nil {
		visibleSourceIDs, err := h.visibleSourceIDs(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected-source access is unavailable")})
			return
		}
		logs, err := h.recentAuditLogs(sourceIDsFromSet(visibleSourceIDs))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected-source audit logs are unavailable")})
			return
		}
		c.JSON(http.StatusOK, logs)
		return
	}
	logs, err := h.recentAuditLogs([]uuid.UUID{*sourceID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected-source audit logs are unavailable")})
		return
	}
	c.JSON(http.StatusOK, logs)
}

func sourceOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func requireSourceOwner(c *gin.Context) (string, bool) {
	owner := sourceOwner(c)
	if owner == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "an authenticated owner is required for connected-source access"})
		return "", false
	}
	return owner, true
}

func destructiveAuthorization(c *gin.Context) DestructiveEffectAuthorization {
	owner := sourceOwner(c)
	return DestructiveEffectAuthorization{
		OwnerIdentity:         owner,
		ActorIdentity:         owner,
		IdempotencyKey:        c.GetHeader("X-HAI-Idempotency-Key"),
		TaskID:                c.GetHeader("X-HAI-Task-ID"),
		ApprovalSourceID:      c.GetHeader("X-HAI-Approval-Source-ID"),
		ApprovalBindingDigest: c.GetHeader("X-HAI-Approval-Binding-Digest"),
	}
}

func writeDestructiveEffectError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrDestructiveOwnerMismatch),
		errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "connected-source resource not found"})
	case errors.Is(err, ErrDestructiveAuthorizationRequired):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "destructive source changes require an approval service"})
	case errors.Is(err, ErrDestructiveAuthorizationDenied),
		errors.Is(err, ErrDestructiveAuthorizationMismatch):
		c.JSON(http.StatusForbidden, gin.H{"error": "destructive source change approval is invalid or unavailable"})
	case errors.Is(err, ErrSourceEmergencyStopActive):
		c.JSON(http.StatusLocked, gin.H{"error": "emergency stop is active; destructive source changes are blocked"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "destructive source change could not be completed"})
	}
}

func sourceVisible(source models.ConnectedSource, owner string) bool {
	owner = strings.TrimSpace(owner)
	sourceOwner := strings.TrimSpace(source.OwnerIdentity)
	return owner != "" && sourceOwner != "" && sourceOwner == owner
}

func filterVisibleSources(sources []models.ConnectedSource, owner string) []models.ConnectedSource {
	visible := make([]models.ConnectedSource, 0, len(sources))
	for _, source := range sources {
		if sourceVisible(source, owner) {
			visible = append(visible, source)
		}
	}
	return visible
}

func (h *Handler) sourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error) {
	if strings.TrimSpace(ownerIdentity) == "" {
		return nil, errors.New("authenticated source owner is required")
	}
	scoped, ok := h.service.(ownerScopedSources)
	if !ok {
		return nil, errors.New("owner-scoped source reads are unavailable")
	}
	return scoped.SourcesForOwner(ownerIdentity, includeDisabled)
}

func (h *Handler) ownedSourceIDs(ownerIdentity string) (map[uuid.UUID]bool, error) {
	sources, err := h.sourcesForOwner(ownerIdentity, true)
	if err != nil {
		return nil, err
	}
	owned := make(map[uuid.UUID]bool, len(sources))
	// The legacy service may include ownerless records for local migration;
	// HTTP account views deliberately expose only exact owner matches.
	for _, source := range sources {
		if sourceVisible(source, ownerIdentity) {
			owned[source.ID] = true
		}
	}
	return owned, nil
}

func (h *Handler) recentSyncJobs(sourceIDs []uuid.UUID) ([]models.SourceSyncJob, error) {
	history, ok := h.service.(sourceHistoryService)
	if !ok {
		return nil, errors.New("source history reads are unavailable")
	}
	return history.SyncJobsForSources(sourceIDs, 100)
}

func (h *Handler) recentAuditLogs(sourceIDs []uuid.UUID) ([]models.SourceAuditLog, error) {
	history, ok := h.service.(sourceHistoryService)
	if !ok {
		return nil, errors.New("source history reads are unavailable")
	}
	return history.AuditLogsForSources(sourceIDs, 100)
}

func (h *Handler) visibleSourceIDs(c *gin.Context) (map[uuid.UUID]bool, error) {
	owner := sourceOwner(c)
	if owner == "" {
		return nil, errors.New("authenticated source owner is required")
	}
	return h.ownedSourceIDs(owner)
}

func selectedAudioFolder(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		return "", errors.New("an explicit relative selected audio folder is required")
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || len(cleaned) > 400 {
		return "", errors.New("audio folder must stay inside the selected intake root")
	}
	return cleaned, nil
}

func selectedDocumentFolder(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		return "", errors.New("an explicit relative selected document folder is required")
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || len(cleaned) > 400 {
		return "", errors.New("document folder must stay inside the selected intake root")
	}
	return cleaned, nil
}

func (h *Handler) requireSourceAccess(c *gin.Context, id uuid.UUID) bool {
	visibleSourceIDs, err := h.visibleSourceIDs(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "connected-source access is unavailable")})
		return false
	}
	if visibleSourceIDs[id] {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
	return false
}

func (h *Handler) requireMutableSource(c *gin.Context, id uuid.UUID) bool {
	_, ok := h.mutableSource(c, id)
	return ok
}

// mutableSource resolves one source at the database boundary when the service
// supports it. Local runners need the source configuration after ownership is
// checked, so returning that exact record avoids a second full source-inventory
// read on every transcription or document extraction request.
func (h *Handler) mutableSource(c *gin.Context, id uuid.UUID) (*models.ConnectedSource, bool) {
	lookup, ok := h.service.(mutableSourceLookup)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped connected-source access is unavailable"})
		return nil, false
	}
	if source, err := lookup.MutableSourceForOwner(id, sourceOwner(c)); err == nil {
		return source, true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify connected source ownership"})
		return nil, false
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
	return nil, false
}

func (h *Handler) requireMutableExtraction(c *gin.Context, id uuid.UUID) bool {
	lookup, ok := h.service.(mutableSourceLookup)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped source-extraction access is unavailable"})
		return false
	}
	if _, err := lookup.MutableExtractionForOwner(id, sourceOwner(c)); err == nil {
		return true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify source extraction ownership"})
		return false
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "source extraction not found"})
	return false
}

func filterExtractionsForSources(extractions []models.SourceExtraction, sourceIDs map[uuid.UUID]bool) []models.SourceExtraction {
	visible := make([]models.SourceExtraction, 0, len(extractions))
	for _, extraction := range extractions {
		if sourceIDs[extraction.SourceID] {
			visible = append(visible, extraction)
		}
	}
	return visible
}

func filterRankedExtractions(extractions []RankedExtraction, sourceIDs map[uuid.UUID]bool) []RankedExtraction {
	visible := make([]RankedExtraction, 0, len(extractions))
	for _, extraction := range extractions {
		if sourceIDs[extraction.Extraction.SourceID] {
			visible = append(visible, extraction)
		}
	}
	return visible
}

func filterVisibleSyncJobs(jobs []models.SourceSyncJob, sourceIDs map[uuid.UUID]bool) []models.SourceSyncJob {
	visible := make([]models.SourceSyncJob, 0, len(jobs))
	for _, job := range jobs {
		if sourceIDs[job.SourceID] {
			visible = append(visible, job)
		}
	}
	return visible
}

func filterVisibleAuditLogs(logs []models.SourceAuditLog, sourceIDs map[uuid.UUID]bool) []models.SourceAuditLog {
	visible := make([]models.SourceAuditLog, 0, len(logs))
	for _, log := range logs {
		if sourceIDs[log.SourceID] {
			visible = append(visible, log)
		}
	}
	return visible
}

func parseUUID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return uuid.UUID{}, false
	}
	return id, true
}

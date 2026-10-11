package source

import (
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/whispercpp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	service     Service
	transcriber whispercpp.Service
	documents   docling.Service
}

type ownerScopedSourceLister interface {
	SourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error)
}

func NewHandler(service Service, transcribers ...whispercpp.Service) *Handler {
	transcriber := whispercpp.DefaultService()
	if len(transcribers) > 0 && transcribers[0] != nil {
		transcriber = transcribers[0]
	}
	return NewHandlerWithDocling(service, transcriber, docling.DefaultService())
}

func NewHandlerWithDocling(service Service, transcriber whispercpp.Service, documents docling.Service) *Handler {
	if transcriber == nil {
		transcriber = whispercpp.DefaultService()
	}
	if documents == nil {
		documents = docling.DefaultService()
	}
	return &Handler{service: service, transcriber: transcriber, documents: documents}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

func (h *Handler) Connectors(c *gin.Context) {
	connectors, err := h.service.Connectors()
	if err != nil {
		writeSourceInternalError(c, "source connector lookup", err)
		return
	}
	c.JSON(http.StatusOK, connectors)
}

// StartGoogleOAuth returns the Google consent URL for a Google-backed source. The UI
// opens the returned url so the user authorizes in their own browser.
func (h *Handler) StartGoogleOAuth(c *gin.Context) {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authorizeUrl": url})
}

// GoogleOAuthCallback is the authenticated, same-origin redirect target the
// user's browser returns to after Google consent. Signed state is additionally
// bound to that verified owner and consumed once before any code exchange.
func (h *Handler) GoogleOAuthCallback(c *gin.Context) {
	ownerIdentity := sourceOwner(c)
	if oauthErr := c.Query("error"); oauthErr != "" {
		// A denied consent attempt is terminal too. Burn a valid state so the
		// browser URL cannot be replayed later with a substituted code.
		_, _ = h.service.CompleteGoogleOAuth(c.Request.Context(), "", c.Query("state"), ownerIdentity)
		c.Redirect(http.StatusFound, "/connected-sources?oauth=denied")
		return
	}
	_, err := h.service.CompleteGoogleOAuth(c.Request.Context(), c.Query("code"), c.Query("state"), ownerIdentity)
	if err != nil {
		c.Redirect(http.StatusFound, "/connected-sources?oauth=error")
		return
	}
	c.Redirect(http.StatusFound, "/connected-sources?oauth=connected")
}

func (h *Handler) CreateSource(c *gin.Context) {
	var request CreateSourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = sourceOwner(c)
	source, err := h.service.CreateSource(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, source)
}

func (h *Handler) Sources(c *gin.Context) {
	includeDisabled, _ := strconv.ParseBool(c.Query("includeDisabled"))
	sources, err := h.service.SourcesForOwner(sourceOwner(c), includeDisabled)
	if err != nil {
		writeSourceInternalError(c, "connected source lookup", err)
		return
	}
	c.JSON(http.StatusOK, sources)
}

func (h *Handler) Overview(c *gin.Context) {
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	overviewService, ok := h.service.(OverviewService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "source overview is not available"})
		return
	}
	overview, err := overviewService.OverviewForOwner(sourceOwner(c), c.Query("projectKey"), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (h *Handler) SyncJobs(c *gin.Context) {
	limit, ok := sourceHistoryLimit(c, "sync job")
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
	if page, requested, err := historyPageFromQuery(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if requested {
		paged, ok := h.service.(PagedHistoryService)
		if !ok {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "paged source history is not available"})
			return
		}
		result, err := paged.SyncJobsForOwnerPage(sourceOwner(c), sourceID, page)
		if err != nil {
			writeSourceInternalError(c, "source sync-job lookup", err)
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}
	var jobs []models.SourceSyncJob
	var err error
	if sourceID == nil {
		jobs, err = h.service.SyncJobsForOwner(sourceOwner(c), limit)
	} else {
		jobs, err = h.service.SyncJobs(sourceID, limit)
	}
	if err != nil {
		writeSourceInternalError(c, "source sync-job lookup", err)
		return
	}
	c.JSON(http.StatusOK, jobs)
}

func (h *Handler) ConnectionHealth(c *gin.Context) {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, health)
}

// ConnectionHealthSummary returns connection state for every source visible to
// the current owner. It avoids the client issuing one health request per source
// on every page load while preserving the same per-source authorization checks.
func (h *Handler) ConnectionHealthSummary(c *gin.Context) {
	sources, err := h.ownerSources(sourceOwner(c), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "connected sources are temporarily unavailable"})
		return
	}
	visible := sources
	if summaryService, ok := h.service.(ConnectionHealthSummaryService); ok {
		c.JSON(http.StatusOK, summaryService.ConnectionHealthForSources(visible))
		return
	}
	healthService, ok := h.service.(ConnectionHealthService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "connection health is not available"})
		return
	}
	result := make([]ConnectionHealth, 0, len(visible))
	for _, source := range visible {
		health, healthErr := healthService.ConnectionHealth(source.ID)
		if healthErr != nil {
			// A single malformed legacy record must not make every other
			// connection disappear from the dashboard. Do not disclose the raw
			// error because it may contain provider-specific context.
			result = append(result, ConnectionHealth{
				SourceID: source.ID, ConnectorKey: source.ConnectorKey,
				Status: "error", Reason: "connection status could not be determined",
			})
			continue
		}
		result = append(result, *health)
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) UpdateSource(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	var request UpdateSourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source, err := h.service.UpdateSource(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Sync(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	var request ImportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.service.SyncContext(c.Request.Context(), id, request)
	if err != nil {
		if errors.Is(err, ErrSyncInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrSyncExecutionFailed) {
			writeSourceInternalError(c, "source sync", err)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	if c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transcription uses the source's configured selected folder and accepts no caller-provided files, model, language, or audio"})
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.sourceByID(id, sourceOwner(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
		return
	}
	if source.ConnectorKey != "whisper-audio" || !source.LocalOnly {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transcription requires an enabled local-only whisper-audio source"})
		return
	}
	folder, err := selectedAudioFolder(source.SyncTarget)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	result, err := h.service.SyncContext(c.Request.Context(), id, ImportRequest{Mode: ModeManualImport, Items: items, ProjectKey: source.DefaultProjectKey, controlledTranscription: true})
	if errors.Is(err, ErrSyncInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrSyncExecutionFailed) {
		writeSourceInternalError(c, "source transcription sync", err)
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExtractDocuments permits only operator-triggered extraction from the
// registered relative folder of a local-only Docling source. The runner output
// is re-ingested as ordinary source evidence, preserving provenance and review.
func (h *Handler) ExtractDocuments(c *gin.Context) {
	if c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document extraction uses the source's configured selected folder and accepts no caller-provided files, model, parser, or folder"})
		return
	}
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.sourceByID(id, sourceOwner(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
		return
	}
	if source.ConnectorKey != "docling-documents" || !source.LocalOnly {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document extraction requires an enabled local-only docling-documents source"})
		return
	}
	folder, err := selectedDocumentFolder(source.SyncTarget)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	documents, err := h.documents.Extract(c.Request.Context(), folder)
	if errors.Is(err, docling.ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local Docling document extractor is not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local Docling document extractor could not complete"})
		return
	}
	items := make([]ImportItem, 0, len(documents))
	for _, document := range documents {
		items = append(items, ImportItem{
			ExternalID: "docling:" + document.ContentDigest,
			Title:      filepath.Base(document.Path),
			Content:    document.Text,
			SourceURI:  "document://selected-source/" + id.String() + "/" + document.Path,
			ItemType:   "document_extraction",
			ProjectKey: source.DefaultProjectKey,
			Metadata:   "engine=docling;format=" + document.Format + ";pages=" + strconv.Itoa(document.PageCount) + ";digest=" + document.ContentDigest + ";original_retained=true;consent=source_owner",
		})
	}
	result, err := h.service.SyncContext(c.Request.Context(), id, ImportRequest{Mode: ModeManualImport, Items: items, ProjectKey: source.DefaultProjectKey})
	if errors.Is(err, ErrSyncInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrSyncExecutionFailed) {
		writeSourceInternalError(c, "document extraction sync", err)
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Reindex(c *gin.Context) {
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
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RunDueScheduledSyncs(c *gin.Context) {
	ownerIdentity := sourceOwner(c)
	if ownerIdentity == "" {
		// The global scheduler is the only allowed ownerless source worker.
		// An HTTP request must carry a verified identity so it cannot trigger a
		// cross-owner refresh batch.
		c.JSON(http.StatusForbidden, gin.H{"error": "scheduled source sync requires an authenticated owner"})
		return
	}
	result, err := RunDueScheduledSyncsForOwnerWithContext(h.service, c.Request.Context(), time.Now().UTC(), ownerIdentity)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// writeSourceInternalError keeps unexpected connector, storage, and provider
// failures out of browser responses. The opaque ID links the operator-visible
// response to separately redacted server telemetry without disclosing secrets
// or local machine paths to the client.
func writeSourceInternalError(c *gin.Context, operation string, err error) {
	errorID := uuid.NewString()
	if err != nil {
		_ = c.Error(fmt.Errorf("%s failed (%s): %s", operation, errorID, safety.RedactSecrets(err.Error())))
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error":   operation + " could not complete",
		"errorId": errorID,
	})
}

func (h *Handler) Pause(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.service.Pause(id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Resume(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableSource(c, id) {
		return
	}
	source, err := h.service.Pause(id, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (h *Handler) Revoke(c *gin.Context) {
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
	var request SearchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = sourceOwner(c)
	result, err := SearchWithContext(h.service, c.Request.Context(), request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// search keeps cancellation available to the concrete source service without
// forcing older service stubs or external integrations to change immediately.
func (h *Handler) search(ctx context.Context, request SearchRequest) (*SearchResult, error) {
	if contextual, ok := h.service.(interface {
		SearchContext(context.Context, SearchRequest) (*SearchResult, error)
	}); ok {
		return contextual.SearchContext(ctx, request)
	}
	return h.service.Search(request)
}

func (h *Handler) Extractions(c *gin.Context) {
	limit, ok := sourceHistoryLimit(c, "extraction")
	if !ok {
		return
	}
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	if page, requested, err := historyPageFromQuery(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if requested {
		paged, ok := h.service.(PagedHistoryService)
		if !ok {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "paged source history is not available"})
			return
		}
		result, err := paged.ExtractionsForOwnerPage(sourceOwner(c), c.Query("projectKey"), includeArchived, page)
		if err != nil {
			writeSourceInternalError(c, "source extraction lookup", err)
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}
	extractions, err := h.service.ExtractionsForOwnerLimit(
		sourceOwner(c),
		c.Query("projectKey"),
		includeArchived,
		limit,
	)
	if err != nil {
		writeSourceInternalError(c, "source extraction lookup", err)
		return
	}
	c.JSON(http.StatusOK, extractions)
}

func (h *Handler) UpdateExtraction(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableExtraction(c, id) {
		return
	}
	var request models.SourceExtraction
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	extraction, err := h.service.UpdateExtraction(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, extraction)
}

func (h *Handler) ArchiveExtraction(c *gin.Context) {
	id, ok := parseUUID(c)
	if !ok {
		return
	}
	if !h.requireMutableExtraction(c, id) {
		return
	}
	extraction, err := h.service.ArchiveExtraction(id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, extraction)
}

func (h *Handler) DeleteExtraction(c *gin.Context) {
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
	limit, ok := sourceHistoryLimit(c, "audit log")
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
	if page, requested, err := historyPageFromQuery(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if requested {
		paged, ok := h.service.(PagedHistoryService)
		if !ok {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "paged source history is not available"})
			return
		}
		result, err := paged.AuditLogsForOwnerPage(sourceOwner(c), sourceID, page)
		if err != nil {
			writeSourceInternalError(c, "source audit lookup", err)
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}
	var logs []models.SourceAuditLog
	var err error
	if sourceID == nil {
		logs, err = h.service.AuditLogsForOwner(sourceOwner(c), limit)
	} else {
		logs, err = h.service.AuditLogs(sourceID, limit)
	}
	if err != nil {
		writeSourceInternalError(c, "source audit lookup", err)
		return
	}
	c.JSON(http.StatusOK, logs)
}

const sourceHistoryDefaultLimit = 100

func sourceHistoryLimit(c *gin.Context, recordType string) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return sourceHistoryDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": recordType + " limit must be between 1 and 500"})
		return 0, false
	}
	return limit, true
}

func sourceOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func historyPageFromQuery(c *gin.Context) (HistoryPageRequest, bool, error) {
	offsetRaw, offsetRequested := c.GetQuery("offset")
	if !offsetRequested {
		return HistoryPageRequest{}, false, nil
	}
	limitRaw, limitRequested := c.GetQuery("limit")
	page := HistoryPageRequest{Limit: 100}
	if limitRequested {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil || limit < 1 || limit > 250 {
			return HistoryPageRequest{}, true, fmt.Errorf("limit must be between 1 and 250")
		}
		page.Limit = limit
	}
	if offsetRequested {
		offset, err := strconv.Atoi(offsetRaw)
		if err != nil || offset < 0 {
			return HistoryPageRequest{}, true, fmt.Errorf("offset must be zero or greater")
		}
		page.Offset = offset
	}
	return page, true, nil
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
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, ErrDestructiveAuthorizationDenied),
		errors.Is(err, ErrDestructiveAuthorizationMismatch):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, ErrSourceEmergencyStopActive):
		c.JSON(http.StatusLocked, gin.H{"error": err.Error()})
	default:
		writeSourceInternalError(c, "connected-source destructive action", err)
	}
}

// Ownerless legacy records may remain readable by the configured migration
// owner, but no signed-in operator may adopt, mutate, sync, or delete them.
func sourceMutable(source models.ConnectedSource, owner string) bool {
	owner = strings.TrimSpace(owner)
	return owner != "" &&
		strings.TrimSpace(source.OwnerIdentity) == owner &&
		source.RevokedAt == nil &&
		!strings.EqualFold(strings.TrimSpace(source.Status), "revoked")
}

func (h *Handler) visibleSourceIDs(c *gin.Context) (map[uuid.UUID]bool, error) {
	sources, err := h.service.SourcesForOwner(sourceOwner(c), true)
	if err != nil {
		return nil, err
	}
	visible := make(map[uuid.UUID]bool, len(sources))
	for _, source := range sources {
		visible[source.ID] = true
	}
	return visible, nil
}

func (h *Handler) ownerSources(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error) {
	return h.service.SourcesForOwner(ownerIdentity, includeDisabled)
}

func (h *Handler) sourceByID(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error) {
	sources, err := h.service.SourcesForOwner(ownerIdentity, true)
	if err != nil {
		return nil, err
	}
	for index := range sources {
		if sources[index].ID == id {
			return &sources[index], nil
		}
	}
	return nil, errors.New("connected source not found")
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
		writeSourceInternalError(c, "connected source access check", err)
		return false
	}
	if visibleSourceIDs[id] {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
	return false
}

func (h *Handler) mutableSourceIDs(c *gin.Context) (map[uuid.UUID]bool, error) {
	owner := sourceOwner(c)
	sources, err := h.service.SourcesForOwner(owner, true)
	if err != nil {
		return nil, err
	}
	mutable := make(map[uuid.UUID]bool, len(sources))
	for _, source := range sources {
		if sourceMutable(source, owner) {
			mutable[source.ID] = true
		}
	}
	return mutable, nil
}

func (h *Handler) requireMutableSource(c *gin.Context, id uuid.UUID) bool {
	mutableSourceIDs, err := h.mutableSourceIDs(c)
	if err != nil {
		writeSourceInternalError(c, "connected source access check", err)
		return false
	}
	if mutableSourceIDs[id] {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "connected source not found"})
	return false
}

func (h *Handler) requireMutableExtraction(c *gin.Context, id uuid.UUID) bool {
	extraction, err := h.service.ExtractionForOwner(sourceOwner(c), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "source extraction not found"})
			return false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	mutableSourceIDs, err := h.mutableSourceIDs(c)
	if err != nil {
		writeSourceInternalError(c, "connected source access check", err)
		return false
	}
	if mutableSourceIDs[extraction.SourceID] {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "source extraction not found"})
	return false
}

func parseUUID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return uuid.UUID{}, false
	}
	return id, true
}

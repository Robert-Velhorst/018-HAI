package ambientmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/outcomeevaluation"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxAmbientMonitorRequestBytes = 64 << 10
	defaultAmbientMonitorLimit    = 100
	maxAmbientMonitorLimit        = 500
)

// OutcomeReader deliberately exposes only the read operation needed to bind a
// monitor to an existing outcome definition. Monitoring cannot mutate outcome
// definitions or bypass their normal governance API.
type OutcomeReader interface {
	GetOutcome(context.Context, string, string, string) (outcomeevaluation.OutcomeRevision, error)
}

type Handler struct {
	service       *Service
	outcomeReader OutcomeReader
}

type RouteGuards struct {
	AuthenticatedOwner gin.HandlerFunc
	RecognizedRole     gin.HandlerFunc
	Read               gin.HandlerFunc
	Write              gin.HandlerFunc
	Govern             gin.HandlerFunc
}

type registerTargetHTTPReq struct {
	IdempotencyKey string     `json:"idempotencyKey"`
	TargetID       string     `json:"targetId"`
	IndicatorID    string     `json:"indicatorId"`
	SourceKind     SourceKind `json:"sourceKind"`
	Enabled        bool       `json:"enabled"`
	CadenceSeconds int64      `json:"cadenceSeconds"`
	FirstRunAt     time.Time  `json:"firstRunAt"`
}

type setEnabledHTTPReq struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Enabled        bool   `json:"enabled"`
}

type runDueHTTPReq struct {
	WorkerID     string    `json:"workerId"`
	AsOf         time.Time `json:"asOf"`
	LeaseSeconds int64     `json:"leaseSeconds"`
	Limit        int       `json:"limit"`
}

type recoverHTTPReq struct {
	AsOf time.Time `json:"asOf"`
}

type monitorTargetHTTPDTO struct {
	ContractVersion int              `json:"contractVersion"`
	ID              string           `json:"id"`
	Scope           Scope            `json:"scope"`
	OutcomeID       string           `json:"outcomeId"`
	IndicatorID     string           `json:"indicatorId"`
	SourceKind      SourceKind       `json:"sourceKind"`
	Enabled         bool             `json:"enabled"`
	CadenceSeconds  int64            `json:"cadenceSeconds"`
	NextRunAt       time.Time        `json:"nextRunAt"`
	Lease           Lease            `json:"lease"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	Authority       AuthorityControl `json:"authority"`
}

func NewHandler(service *Service, outcomeReader OutcomeReader) *Handler {
	return &Handler{service: service, outcomeReader: outcomeReader}
}

// RegisterRoutes mounts owner-scoped monitor routes below the API root. It
// refuses partial security wiring and has no unguarded fallback.
func RegisterRoutes(parent *gin.RouterGroup, handler *Handler, guards RouteGuards) error {
	if parent == nil || handler == nil || handler.service == nil || handler.service.now == nil || isTypedNil(handler.service.repository) || isTypedNil(handler.outcomeReader) {
		return errors.New("ambient monitor route group, service, and outcome reader are required")
	}
	if guards.AuthenticatedOwner == nil || guards.RecognizedRole == nil || guards.Read == nil || guards.Write == nil || guards.Govern == nil {
		return errors.New("ambient monitor routes require authenticated-owner, recognized-role, read, write, and govern guards")
	}
	routes := parent.Group("/outcome-evaluations/workspaces/:workspaceId")
	routes.Use(guards.AuthenticatedOwner, guards.RecognizedRole)
	{
		routes.GET("/outcomes/:outcomeId/monitor", guards.Read, handler.GetMonitor)
		routes.PUT("/outcomes/:outcomeId/monitor", guards.Govern, handler.PutMonitor)
		routes.PATCH("/outcomes/:outcomeId/monitor/:targetId/enabled", guards.Govern, handler.SetEnabled)
		routes.GET("/outcomes/:outcomeId/monitor/:targetId/observations", guards.Read, handler.Observations)
		routes.GET("/outcomes/:outcomeId/monitor/:targetId/runs", guards.Read, handler.Runs)
		routes.GET("/outcomes/:outcomeId/monitor/:targetId/compositions", guards.Read, handler.Compositions)
		routes.GET("/outcomes/:outcomeId/monitor/:targetId/compositions/:deliveryId/attempts", guards.Read, handler.CompositionAttempts)
		routes.POST("/monitors/run-due", guards.Write, handler.RunDue)
		routes.POST("/monitors/recover", guards.Govern, handler.Recover)
	}
	return nil
}

func (h *Handler) GetMonitor(c *gin.Context) {
	scope, outcomeID, ok := h.outcomeScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	targets, err := h.service.Targets(c.Request.Context(), scope)
	if err != nil {
		respondAmbientMonitor(c, nil, err, http.StatusOK)
		return
	}
	filtered := make([]monitorTargetHTTPDTO, 0, len(targets))
	for _, target := range targets {
		if target.OutcomeID == outcomeID {
			filtered = append(filtered, monitorTargetHTTP(target))
		}
	}
	c.JSON(http.StatusOK, gin.H{"targets": filtered, "authority": advisoryAuthority()})
}

func (h *Handler) PutMonitor(c *gin.Context) {
	scope, outcomeID, ok := h.outcomeScope(c)
	if !ok {
		return
	}
	var body registerTargetHTTPReq
	if !decodeAmbientMonitorJSON(c, &body) {
		return
	}
	if body.CadenceSeconds > int64(maxCadence/time.Second) || body.CadenceSeconds < int64(minCadence/time.Second) {
		respondAmbientMonitor(c, nil, ErrInvalidInput, http.StatusCreated)
		return
	}
	if !h.requireOutcome(c, scope, outcomeID, strings.TrimSpace(body.IndicatorID), body.FirstRunAt) {
		return
	}
	if existing, err := h.service.Target(c.Request.Context(), scope, strings.TrimSpace(body.TargetID)); err == nil {
		if existing.OutcomeID != outcomeID {
			respondAmbientMonitor(c, nil, ErrNotFound, http.StatusOK)
			return
		}
		cadence := time.Duration(body.CadenceSeconds) * time.Second
		aligned := !existing.NextRunAt.Before(body.FirstRunAt) && existing.NextRunAt.Sub(body.FirstRunAt)%cadence == 0
		if existing.IndicatorID != strings.TrimSpace(body.IndicatorID) || existing.SourceKind != body.SourceKind || existing.Enabled != body.Enabled || existing.Cadence != cadence || !aligned {
			respondAmbientMonitor(c, nil, ErrIdempotencyConflict, http.StatusOK)
			return
		}
		c.JSON(http.StatusOK, gin.H{"target": monitorTargetHTTP(existing), "created": false, "authority": advisoryAuthority()})
		return
	} else if !errors.Is(err, ErrNotFound) {
		respondAmbientMonitor(c, nil, err, http.StatusOK)
		return
	}
	now := h.service.now().UTC().Truncate(time.Microsecond)
	target, created, err := h.service.RegisterTarget(c.Request.Context(), RegisterTargetRequest{
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
		Scope:          scope,
		TargetID:       strings.TrimSpace(body.TargetID),
		OutcomeID:      outcomeID,
		IndicatorID:    strings.TrimSpace(body.IndicatorID),
		SourceKind:     body.SourceKind,
		Enabled:        body.Enabled,
		Cadence:        time.Duration(body.CadenceSeconds) * time.Second,
		FirstRunAt:     body.FirstRunAt,
		RequestedAt:    now,
	})
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAmbientMonitor(c, gin.H{"target": monitorTargetHTTP(target), "created": created, "authority": advisoryAuthority()}, err, status)
}

func (h *Handler) SetEnabled(c *gin.Context) {
	scope, outcomeID, targetID, ok := h.targetScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	target, ok := h.requireTarget(c, scope, outcomeID, targetID)
	if !ok {
		return
	}
	var body setEnabledHTTPReq
	if !decodeAmbientMonitorJSON(c, &body) {
		return
	}
	if target.Enabled == body.Enabled {
		c.JSON(http.StatusOK, gin.H{"target": monitorTargetHTTP(target), "updated": false, "authority": advisoryAuthority()})
		return
	}
	updated, changed, err := h.service.SetEnabled(c.Request.Context(), SetEnabledRequest{
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
		Scope:          scope,
		TargetID:       targetID,
		Enabled:        body.Enabled,
		RequestedAt:    h.service.now().UTC().Truncate(time.Microsecond),
	})
	respondAmbientMonitor(c, gin.H{"target": monitorTargetHTTP(updated), "updated": changed, "authority": advisoryAuthority()}, err, http.StatusOK)
}

func (h *Handler) Observations(c *gin.Context) {
	scope, outcomeID, targetID, ok := h.targetScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	if _, ok = h.requireTarget(c, scope, outcomeID, targetID); !ok {
		return
	}
	limit, ok := ambientMonitorLimit(c)
	if !ok {
		return
	}
	items, err := h.service.Observations(c.Request.Context(), scope, targetID, limit)
	respondAmbientMonitor(c, gin.H{"observations": items, "authority": advisoryAuthority()}, err, http.StatusOK)
}

func (h *Handler) Runs(c *gin.Context) {
	scope, outcomeID, targetID, ok := h.targetScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	if _, ok = h.requireTarget(c, scope, outcomeID, targetID); !ok {
		return
	}
	limit, ok := ambientMonitorLimit(c)
	if !ok {
		return
	}
	items, err := h.service.Runs(c.Request.Context(), scope, targetID, limit)
	respondAmbientMonitor(c, gin.H{"runs": items, "authority": advisoryAuthority()}, err, http.StatusOK)
}

func (h *Handler) Compositions(c *gin.Context) {
	scope, outcomeID, targetID, ok := h.targetScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	if _, ok = h.requireTarget(c, scope, outcomeID, targetID); !ok {
		return
	}
	limit, ok := ambientMonitorLimit(c)
	if !ok {
		return
	}
	items, err := h.service.Compositions(c.Request.Context(), scope, targetID, limit)
	respondAmbientMonitor(c, gin.H{"compositions": items, "authority": advisoryAuthority()}, err, http.StatusOK)
}

func (h *Handler) CompositionAttempts(c *gin.Context) {
	scope, outcomeID, targetID, ok := h.targetScope(c)
	if !ok || !h.requireOutcome(c, scope, outcomeID, "", time.Time{}) {
		return
	}
	if _, ok = h.requireTarget(c, scope, outcomeID, targetID); !ok {
		return
	}
	deliveryID := strings.TrimSpace(c.Param("deliveryId"))
	if err := validateIdentifier("composition id", deliveryID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ambient monitor resource id"})
		return
	}
	delivery, err := h.service.Composition(c.Request.Context(), scope, deliveryID)
	if err != nil {
		respondAmbientMonitor(c, nil, err, http.StatusOK)
		return
	}
	if delivery.TargetID != targetID {
		respondAmbientMonitor(c, nil, ErrNotFound, http.StatusOK)
		return
	}
	limit, ok := ambientMonitorLimit(c)
	if !ok {
		return
	}
	items, err := h.service.CompositionAttempts(c.Request.Context(), scope, deliveryID, limit)
	respondAmbientMonitor(c, gin.H{"attempts": items, "authority": advisoryAuthority()}, err, http.StatusOK)
}

func (h *Handler) RunDue(c *gin.Context) {
	scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	var body runDueHTTPReq
	if !decodeAmbientMonitorJSON(c, &body) {
		return
	}
	if body.LeaseSeconds < int64(minLeaseDuration/time.Second) || body.LeaseSeconds > int64(maxLeaseDuration/time.Second) || body.Limit < 1 || body.Limit > maxClaimLimit {
		respondAmbientMonitor(c, nil, ErrInvalidInput, http.StatusOK)
		return
	}
	result, err := h.service.ProcessDue(c.Request.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: strings.TrimSpace(body.WorkerID), Now: body.AsOf,
		LeaseDuration: time.Duration(body.LeaseSeconds) * time.Second, Limit: body.Limit,
	})
	respondAmbientMonitor(c, result, err, http.StatusOK)
}

func (h *Handler) Recover(c *gin.Context) {
	scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	var body recoverHTTPReq
	if !decodeAmbientMonitorJSON(c, &body) {
		return
	}
	collectionRecovered, err := h.service.RecoverExpiredLeases(c.Request.Context(), scope, body.AsOf)
	if err != nil {
		respondAmbientMonitor(c, nil, err, http.StatusOK)
		return
	}
	compositionRecovered, err := h.service.RecoverExpiredCompositionLeases(c.Request.Context(), scope, body.AsOf)
	respondAmbientMonitor(c, gin.H{
		"recovered":            collectionRecovered + compositionRecovered,
		"collectionRecovered":  collectionRecovered,
		"compositionRecovered": compositionRecovered,
		"authority":            advisoryAuthority(),
	}, err, http.StatusOK)
}

func (h *Handler) workspaceScope(c *gin.Context) (Scope, bool) {
	if h == nil || h.service == nil || isTypedNil(h.service.repository) || isTypedNil(h.outcomeReader) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ambient monitor is unavailable"})
		return Scope{}, false
	}
	value, exists := c.Get(identity.ContextSubjectKey)
	owner, ownerOK := value.(string)
	scope := Scope{OwnerID: strings.TrimSpace(owner), WorkspaceID: strings.TrimSpace(c.Param("workspaceId"))}
	if !exists || !ownerOK || scope.OwnerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required"})
		return Scope{}, false
	}
	clean, err := validateScope(scope)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ambient monitor resource id"})
		return Scope{}, false
	}
	return clean, true
}

func (h *Handler) outcomeScope(c *gin.Context) (Scope, string, bool) {
	scope, ok := h.workspaceScope(c)
	if !ok {
		return Scope{}, "", false
	}
	outcomeID := strings.TrimSpace(c.Param("outcomeId"))
	if err := validateIdentifier("outcome id", outcomeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ambient monitor resource id"})
		return Scope{}, "", false
	}
	return scope, outcomeID, true
}

func (h *Handler) targetScope(c *gin.Context) (Scope, string, string, bool) {
	scope, outcomeID, ok := h.outcomeScope(c)
	if !ok {
		return Scope{}, "", "", false
	}
	targetID := strings.TrimSpace(c.Param("targetId"))
	if err := validateIdentifier("target id", targetID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ambient monitor resource id"})
		return Scope{}, "", "", false
	}
	return scope, outcomeID, targetID, true
}

func (h *Handler) requireTarget(c *gin.Context, scope Scope, outcomeID, targetID string) (MonitorTarget, bool) {
	target, err := h.service.Target(c.Request.Context(), scope, targetID)
	if err != nil {
		respondAmbientMonitor(c, nil, err, http.StatusOK)
		return MonitorTarget{}, false
	}
	if target.OutcomeID != outcomeID {
		respondAmbientMonitor(c, nil, ErrNotFound, http.StatusOK)
		return MonitorTarget{}, false
	}
	return target, true
}

func (h *Handler) requireOutcome(c *gin.Context, scope Scope, outcomeID, indicatorID string, firstRunAt time.Time) bool {
	revision, err := h.outcomeReader.GetOutcome(c.Request.Context(), scope.OwnerID, scope.WorkspaceID, outcomeID)
	if err != nil {
		respondAmbientOutcomeError(c, err)
		return false
	}
	definition := revision.Outcome
	if definition.ID != outcomeID || definition.Scope.OwnerID != scope.OwnerID || definition.Scope.WorkspaceID != scope.WorkspaceID {
		respondAmbientMonitor(c, nil, ErrNotFound, http.StatusOK)
		return false
	}
	if indicatorID == "" {
		return true
	}
	found := false
	for _, indicator := range definition.Indicators {
		if indicator.ID == indicatorID {
			found = true
			break
		}
	}
	if !found || definition.Window.Start.IsZero() || definition.Window.End.IsZero() || definition.Window.End.Before(definition.Window.Start) || firstRunAt.IsZero() || firstRunAt.Before(definition.Window.Start) || firstRunAt.After(definition.Window.End) {
		respondAmbientMonitor(c, nil, ErrInvalidInput, http.StatusOK)
		return false
	}
	return true
}

func decodeAmbientMonitorJSON(c *gin.Context, target any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "ambient monitor requests require application/json"})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAmbientMonitorRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ambient monitor request is too large"})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ambient monitor request"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ambient monitor request must contain one JSON object"})
		return false
	}
	return true
}

func ambientMonitorLimit(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return defaultAmbientMonitorLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxAmbientMonitorLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ambient monitor limit must be between 1 and 500"})
		return 0, false
	}
	return limit, true
}

func monitorTargetHTTP(target MonitorTarget) monitorTargetHTTPDTO {
	return monitorTargetHTTPDTO{
		ContractVersion: target.ContractVersion,
		ID:              target.ID,
		Scope:           target.Scope,
		OutcomeID:       target.OutcomeID,
		IndicatorID:     target.IndicatorID,
		SourceKind:      target.SourceKind,
		Enabled:         target.Enabled,
		CadenceSeconds:  int64(target.Cadence / time.Second),
		NextRunAt:       target.NextRunAt,
		Lease:           target.Lease,
		CreatedAt:       target.CreatedAt,
		UpdatedAt:       target.UpdatedAt,
		Authority:       target.Authority,
	}
}

func respondAmbientOutcomeError(c *gin.Context, err error) {
	if errors.Is(err, outcomeevaluation.ErrNotFound) || errors.Is(err, outcomeevaluation.ErrScopeViolation) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ambient monitor record not found"})
		return
	}
	errorID := uuid.NewString()
	_ = c.Error(fmt.Errorf("ambient monitor outcome lookup failed (%s)", errorID))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "ambient monitor operation failed", "errorId": errorID})
}

func respondAmbientMonitor(c *gin.Context, value any, err error, successStatus int) {
	if err == nil {
		c.JSON(successStatus, value)
		return
	}
	status, message := ambientMonitorErrorResponse(err)
	if status == http.StatusInternalServerError {
		errorID := uuid.NewString()
		_ = c.Error(fmt.Errorf("ambient monitor operation failed (%s)", errorID))
		c.JSON(status, gin.H{"error": message, "errorId": errorID})
		return
	}
	c.JSON(status, gin.H{"error": message})
}

func ambientMonitorErrorResponse(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrScopeViolation):
		return http.StatusNotFound, "ambient monitor record not found"
	case errors.Is(err, ErrIdempotencyConflict), errors.Is(err, ErrLeaseLost):
		return http.StatusConflict, "ambient monitor state conflict"
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest, "ambient monitor request failed validation"
	case errors.Is(err, ErrRepositoryUnavailable), errors.Is(err, ErrCollectorUnavailable):
		return http.StatusServiceUnavailable, "ambient monitor is unavailable"
	default:
		return http.StatusInternalServerError, "ambient monitor operation failed"
	}
}

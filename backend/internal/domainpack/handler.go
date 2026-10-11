package domainpack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxDomainPackRequestBytes = 128 * 1024

type Handler struct {
	registry    *Registry
	preferences PreferenceRepository
}

type preferenceRequest struct {
	ExpectedRevision    int64            `json:"expectedRevision,omitempty"`
	Status              PreferenceStatus `json:"status"`
	Enabled             *bool            `json:"enabled,omitempty"`
	ClassificationBoost int              `json:"classificationBoost"`
	ForceLocalOnly      bool             `json:"forceLocalOnly"`
	Adaptation          PackAdaptation   `json:"adaptation"`
}

func NewHandler(registry *Registry, preferences PreferenceRepository) (*Handler, error) {
	if registry == nil {
		return nil, fmt.Errorf("domain pack registry is required")
	}
	if preferences == nil {
		return nil, fmt.Errorf("domain pack preference repository is required")
	}
	return &Handler{registry: registry, preferences: preferences}, nil
}

func (handler *Handler) Catalog(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	viewMode := strings.TrimSpace(c.Query("view"))
	if viewMode != "" && viewMode != "full" && viewMode != "summary" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain pack catalog view must be full or summary"})
		return
	}
	packs := handler.registry.List()
	views := make([]PackView, 0, len(packs))
	for _, pack := range packs {
		view, err := handler.registry.Resolve(owner, pack.ID, handler.preferences)
		if err != nil {
			respondDomainPack(c, nil, err, http.StatusOK)
			return
		}
		views = append(views, view)
	}
	if viewMode == "summary" {
		summaries := make([]PackSummaryView, 0, len(views))
		for _, view := range views {
			summaries = append(summaries, PackSummaryView{
				Pack: PackSummary{
					ID:             view.Pack.ID,
					Version:        view.Pack.Version,
					Name:           view.Pack.Name,
					Description:    view.Pack.Description,
					Sensitive:      view.Pack.Sensitive,
					DefaultEnabled: view.Pack.DefaultEnabled,
					MethodCount:    len(view.Pack.Playbook.Methods),
				},
				Enabled:   view.Enabled,
				LocalOnly: view.LocalOnly,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"metadata": handler.registry.Metadata(),
			"packs":    summaries,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"metadata": handler.registry.Metadata(),
		"packs":    views,
	})
}

func (handler *Handler) Detail(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	result, err := handler.registry.Resolve(owner, PackID(c.Param("id")), handler.preferences)
	respondDomainPack(c, result, err, http.StatusOK)
}

func (handler *Handler) Preferences(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	result, err := handler.preferences.List(owner)
	respondDomainPack(c, gin.H{"preferences": result}, err, http.StatusOK)
}

func (handler *Handler) UpsertPreference(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	packID := PackID(c.Param("id"))
	if _, exists := handler.registry.Lookup(packID); !exists {
		respondDomainPack(c, nil, fmt.Errorf("domain pack %q not found", packID), http.StatusOK)
		return
	}
	var request preferenceRequest
	if !decodeDomainPackJSON(c, &request) {
		return
	}
	result, err := handler.preferences.Upsert(PackPreference{
		OwnerIdentity:       owner,
		PackID:              packID,
		CatalogVersion:      CatalogVersion,
		Revision:            request.ExpectedRevision,
		Status:              request.Status,
		Enabled:             request.Enabled,
		ClassificationBoost: request.ClassificationBoost,
		ForceLocalOnly:      request.ForceLocalOnly,
		Adaptation:          request.Adaptation,
	})
	respondDomainPack(c, result, err, http.StatusOK)
}

func (handler *Handler) Classify(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	var request ClassificationRequest
	if !decodeDomainPackJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := handler.registry.Classify(request, handler.preferences)
	respondDomainPack(c, result, err, http.StatusOK)
}

func (handler *Handler) Effective(c *gin.Context) {
	handler.Detail(c)
}

// Playbook exposes the immutable method catalog alongside the owner's
// conservative effective pack state. It does not return executable actions.
func (handler *Handler) Playbook(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	view, err := handler.registry.Resolve(owner, PackID(c.Param("id")), handler.preferences)
	if err != nil {
		respondDomainPack(c, nil, err, http.StatusOK)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"packId":                    view.Pack.ID,
		"enabled":                   view.Enabled,
		"localOnly":                 view.LocalOnly,
		"playbook":                  view.Pack.Playbook,
		"advisoryOnly":              true,
		"executionAuthorityGranted": false,
	})
}

// SelectMethods resolves planning methods for an already classified task. The
// returned result is explicitly advisory and cannot authorize execution.
func (handler *Handler) SelectMethods(c *gin.Context) {
	owner, ok := domainPackOwner(c)
	if !ok {
		return
	}
	var request MethodSelectionRequest
	if !decodeDomainPackJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := handler.registry.SelectMethods(request, handler.preferences)
	respondDomainPack(c, result, err, http.StatusOK)
}

func domainPackOwner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	owner, ok := value.(string)
	owner = strings.TrimSpace(owner)
	if !exists || !ok || owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for domain packs"})
		return "", false
	}
	return owner, true
}

func decodeDomainPackJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDomainPackRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "domain pack request is too large"})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain pack request"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain pack request must contain one JSON object"})
		return false
	}
	return true
}

func respondDomainPack(c *gin.Context, value any, err error, successStatus int) {
	if err == nil {
		c.JSON(successStatus, value)
		return
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, ErrPreferenceConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "domain pack preference was updated elsewhere"})
	case strings.Contains(message, "not found"):
		c.JSON(http.StatusNotFound, gin.H{"error": "domain pack not found"})
	case strings.Contains(message, "required"),
		strings.Contains(message, "invalid"),
		strings.Contains(message, "must be"),
		strings.Contains(message, "between"),
		strings.Contains(message, "cannot weaken"),
		strings.Contains(message, "duplicate"):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain pack request"})
	default:
		errorID := uuid.NewString()
		_ = c.Error(fmt.Errorf("domain pack error %s: %w", errorID, err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "domain pack operation failed",
			"errorId": errorID,
		})
	}
}

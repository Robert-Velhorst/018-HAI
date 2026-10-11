package ambient

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RequireAuthenticatedOwner protects the personal ambient planning boundary.
// Background workers call the service directly, but browser and API traffic
// must carry a verified IDP principal before it can inspect private needs,
// opportunities, or scan history.
func RequireAuthenticatedOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifiedOwner(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for ambient access"})
			return
		}
		c.Next()
	}
}

func (h *Handler) Overview(c *gin.Context) {
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for ambient access"})
		return
	}
	result, err := h.service.OverviewForOwner(ownerIdentity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "ambient overview is unavailable")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Scan(c *gin.Context) {
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required to run a personal ambient scan"})
		return
	}
	result, err := h.service.ScanForOwner(ownerIdentity, "manual")
	if err != nil {
		if errors.Is(err, ErrScanInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "an ambient scan is already in progress"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "ambient scan could not be completed"), "scan": result})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) UpdateNeed(c *gin.Context) {
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required to update ambient planning preferences"})
		return
	}
	var request NeedUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ambient planning preference request is invalid"})
		return
	}
	result, err := h.service.UpdateNeedForOwner(ownerIdentity, c.Param("key"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "ambient planning preference could not be updated")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Accept(c *gin.Context) {
	h.resolve(c, true)
}

func (h *Handler) Dismiss(c *gin.Context) {
	h.resolve(c, false)
}

func (h *Handler) resolve(c *gin.Context, accept bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid opportunity id"})
		return
	}
	var request ResolutionRequest
	if bindErr := c.ShouldBindJSON(&request); bindErr != nil && !errors.Is(bindErr, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ambient proposal resolution request is invalid"})
		return
	}
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		// Accepting a proposal can create a governed workflow item. Keep the
		// ownerless resolution path available only to in-process system workers.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required to resolve ambient proposals"})
		return
	}
	request.OwnerIdentity = ownerIdentity
	request.Actor = ownerIdentity
	var result interface{}
	if accept {
		result, err = h.service.Accept(id, request)
	} else {
		result, err = h.service.Dismiss(id, request)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "ambient proposal could not be resolved")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func verifiedOwner(c *gin.Context) string {
	return verifiedActor(c, "")
}

func verifiedActor(c *gin.Context, fallback string) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			if subject = strings.TrimSpace(subject); subject != "" {
				return subject
			}
		}
	}
	return fallback
}

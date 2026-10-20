package hireai

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, h.service.Status())
}

func (h *Handler) Search(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.Context().Err() != nil {
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Hire.AI search requests must use application/json"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequest)
	var input SearchRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	if err == nil {
		var extra any
		if trailingErr := decoder.Decode(&extra); trailingErr != io.EOF {
			err = errors.New("trailing JSON value")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Hire.AI search request exceeds the size limit"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Hire.AI search request"})
		return
	}
	if err := validateRequest(input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hire.AI search filters are invalid or exceed supported limits"})
		return
	}
	result, err := h.service.Search(c.Request.Context(), input)
	if errors.Is(err, ErrInvalidRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hire.AI search filters are invalid or exceed supported limits"})
		return
	}
	if errors.Is(err, ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Hire.AI search is not configured or unavailable"})
		return
	}
	if err != nil || result == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Hire.AI returned an invalid search response"})
		return
	}
	c.JSON(http.StatusOK, result)
}

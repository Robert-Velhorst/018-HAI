package events

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	store *OutboxStore
}

func NewHandler(store *OutboxStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Stats(c *gin.Context) {
	stats, err := h.store.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Event delivery status is unavailable"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) Retry(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A valid event delivery id is required"})
		return
	}
	if err := h.store.RetryDeadLetter(c.Request.Context(), id, time.Now()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Dead-lettered event delivery not found"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Event delivery could not be retried"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "eventId": id})
}

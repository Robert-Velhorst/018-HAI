package openclawreconcile

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Retain is an explicit owner write, never a side effect of viewing/downloading.
func (h *ArtifactHandler) Retain(c *gin.Context) {
	owner, id, ok := artifactRequest(c)
	if !ok {
		return
	}
	if h.reader == nil {
		artifactResponseError(c, ErrArtifactRetentionDisabled)
		return
	}
	if h.archive == nil {
		artifactResponseError(c, ErrArtifactRetentionDisabled)
		return
	}
	if !h.archive.WriteReady() {
		if h.archive.Ready() {
			artifactResponseError(c, ErrArtifactRetentionWeakKey)
		} else {
			artifactResponseError(c, ErrArtifactRetentionDisabled)
		}
		return
	}
	digest := c.Param("digest")
	if !validArtifactDigest(digest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid artifact reference"})
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		c.Header("Retry-After", "5")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Artifact transfers are busy; try again shortly"})
		return
	}
	binding, err := h.reader.ArtifactForEvent(c.Request.Context(), owner, id, digest)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	if binding == nil || binding.Receipt.OwnerIdentity != owner || binding.EventID != id || binding.Descriptor.Digest != digest {
		c.JSON(http.StatusNotFound, gin.H{"error": "Artifact binding unavailable"})
		return
	}
	content, err := h.content(c.Request.Context(), *binding)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	info, err := h.archive.Retain(c.Request.Context(), *binding, content)
	if err != nil {
		artifactResponseError(c, err)
		return
	}
	c.JSON(http.StatusOK, info)
}

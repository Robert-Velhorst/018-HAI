package router

import (
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxNonMultipartRequestBytes int64 = 2 << 20

// requestBodyLimitMiddleware bounds ordinary API bodies before a handler can
// decode or buffer them. Multipart uploads have route-specific limits because
// the small automation image and the reviewed OpenClaw archive have materially
// different, explicit size budgets.
func requestBodyLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 || c.Request == nil || c.Request.Body == nil ||
			!requestMayHaveBody(c.Request.Method) || isMultipartRequest(c.Request.Header.Get("Content-Type")) {
			c.Next()
			return
		}

		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "request body exceeds the allowed size",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()

		// A handler may attach a MaxBytesError to Gin instead of writing a
		// response. Convert that otherwise silent failure into a stable 413.
		if !c.Writer.Written() {
			for _, ginErr := range c.Errors {
				var tooLarge *http.MaxBytesError
				if errors.As(ginErr.Err, &tooLarge) {
					c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
						"error": "request body exceeds the allowed size",
					})
					return
				}
			}
		}
	}
}

func requestMayHaveBody(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isMultipartRequest(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	return err == nil && strings.EqualFold(mediaType, "multipart/form-data")
}

package authentication

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func AuthMiddleware(h *Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken, ok := h.resolveAuthenticatedAccessToken(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Please login again"})
			return
		}
		c.Set(authenticatedTokenContextKey, accessToken)

		// Resolve identity only after a refresh succeeds. An expired access token
		// must not make a valid refresh session unusable for protected routes.
		session, err := h.authService.GetSessionFromToken(accessToken)
		if err != nil || session == nil || !session.Authenticated {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}
		userID, err := uuid.Parse(session.Subject)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}
		c.Set("userID", userID)
		c.Set("authSession", session)

		c.Next()
	}
}

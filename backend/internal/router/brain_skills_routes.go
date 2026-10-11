package router

import (
	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/rbac"

	"github.com/gin-gonic/gin"
)

func initializeBrainSkillsRoutes(apiVersion *gin.RouterGroup, handler *brainskills.Handler) {
	routes := apiVersion.Group("/brain-skills")
	routes.Use(requireAuthenticatedOwner())
	routes.GET("/", requirePermission(rbac.PermRead), handler.Inventory)
	routes.GET("/:id/guidance-preview", requirePermission(rbac.PermApprove), handler.GuidancePreview)
	routes.PUT("/:id/selection", requirePermission(rbac.PermApprove), handler.SetSelection)
}

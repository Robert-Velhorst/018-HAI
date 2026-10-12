package router

import (
	"context"
	"net/http"
	"time"

	"automation-hub-idp/docs"
	"automation-hub-idp/internal/app/authentication"
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/services"
	"automation-hub-idp/internal/app/users"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func initializeRoutes(router *gin.Engine, database *gorm.DB, resources *services.OwnedResources) error {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "idp"})
	})

	relativePathV1 := config.ServerConfig.BaseURL + "/v1"
	docs.SwaggerInfo.BasePath = relativePathV1
	v1 := router.Group(relativePathV1)
	{
		// initialize auth routes
		err := initializeAuthRoutes(router, v1, database, resources)
		if err != nil {
			return err
		}
	}
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	return nil
}

func initializeAuthRoutes(router *gin.Engine, apiVersion *gin.RouterGroup, database *gorm.DB, resources *services.OwnedResources) error {
	authService, err := authentication.NewDefaultAuthServiceWithDatabase(database)
	if err != nil {
		return err
	}
	resources.Add(authService)
	readiness, _ := authService.(serviceReadiness)
	router.GET("/readyz", readinessHandler(readiness))
	redisAddress := ""
	if config.RedisConfig != nil {
		redisAddress = config.RedisConfig.RedisAddr
	}
	authHandler := authentication.NewHandlerWithRedisPasswordResetLimiter(authService, redisAddress)
	resources.Add(authHandler)
	authMiddleware := authentication.AuthMiddleware(authHandler)

	userService, err := users.NewDefaultUserServiceWithDatabase(database)
	if err != nil {
		return err
	}
	resources.Add(userService)
	userHandler := users.NewHandler(userService)

	auth := apiVersion.Group("/auth")
	{
		auth.GET("/capabilities", authHandler.Capabilities)
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		auth.GET("/google/login", authHandler.GoogleLogin)
		auth.GET("/google/callback", authHandler.GoogleCallback)
		auth.POST("/local-preview", authHandler.LocalPreview)
		auth.POST("/logout", authHandler.RequireLogoutOrigin, authMiddleware, authHandler.Logout)
		auth.POST("/request-password-reset", authHandler.RequestPasswordReset)
		auth.POST("/confirm-password-reset", authHandler.ConfirmPasswordReset)
		auth.GET("/is-user-authenticated", authHandler.IsUserAuthenticated)
		auth.GET("/session", authHandler.CurrentSession)
	}

	user := apiVersion.Group("/user")
	{
		user.GET("/", authMiddleware, userHandler.GetCurrentUser)
		user.PATCH("/", authMiddleware, userHandler.Update)
	}
	return nil
}

type serviceReadiness interface {
	Readiness(context.Context) error
}

func readinessHandler(checker serviceReadiness) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if checker == nil || checker.Readiness(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

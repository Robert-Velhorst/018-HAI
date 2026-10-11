package router

import (
	"context"
	"net/http"

	"automation-hub-backend/internal/buildinfo"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/demomode"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/i18n"
	"automation-hub-backend/internal/rbac"
	"automation-hub-backend/internal/supportbundle"

	"github.com/gin-gonic/gin"
)

// systemInfoHandler exposes build/version, run mode, supported languages, and a
// readiness summary. It wires together buildinfo, demomode, i18n, and doctor so
// those capabilities are reachable over the API rather than internal-only.
func systemInfoHandler(diagnose func() doctor.Report) gin.HandlerFunc {
	return func(c *gin.Context) {
		report := diagnose()
		ok, warn, fail := report.Counts()
		mode := demomode.Parse(config.AppConfig.RunMode)

		langs := i18n.Supported()
		codes := make([]string, 0, len(langs))
		for _, l := range langs {
			codes = append(codes, string(l))
		}

		c.JSON(http.StatusOK, gin.H{
			"build":                 buildinfo.Snapshot(),
			"runMode":               string(mode),
			"allowsRealSideEffects": mode.AllowsRealSideEffects(),
			"languages":             codes,
			"readiness":             gin.H{"ready": fail == 0, "ok": ok, "warn": warn, "fail": fail},
		})
	}
}

// supportBundleHandler returns a redaction-safe diagnostic bundle. counts is a
// provider so the caller decides what operational counts to include.
func supportBundleHandler(diagnose func() doctor.Report, counts func() map[string]int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, supportbundle.Build(diagnose(), buildinfo.Snapshot(), counts()))
	}
}

func initializeSystemRoutes(
	apiVersion *gin.RouterGroup,
	diagnose func() doctor.Report,
	readiness func(context.Context) doctor.Report,
	counts func() map[string]int,
) {
	sys := apiVersion.Group("/system")
	sys.Use(requireAuthenticatedOwner())
	{
		sys.GET("/info", requirePermission(rbac.PermRead), systemInfoHandler(diagnose))
		sys.GET("/readiness", requirePermission(rbac.PermRead), readinessDetailHandler(readiness))
		// The support bundle is an operator/admin diagnostic, so it requires the
		// admin permission carried by a verified owner JWT.
		sys.GET("/support-bundle", requirePermission(rbac.PermAdmin), supportBundleHandler(diagnose, counts))
	}
}

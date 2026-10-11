package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/mcppreflight"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/planningoptimizer"
	"automation-hub-backend/internal/serena"

	"github.com/gin-gonic/gin"
)

type optimizerRouteRepository struct{}

func (optimizerRouteRepository) Create(run *models.OptimizationProposalRun) (*models.OptimizationProposalRun, error) {
	return run, nil
}

func (optimizerRouteRepository) List(string, int) ([]models.OptimizationProposalRun, error) {
	return nil, nil
}

// Mirrors the exact path set registered in initializeAutomationsRoutes to
// confirm gin builds the route tree without panicking (static + param at the
// same level) and resolves each new endpoint to the right handler.
func TestAutomationRoutesNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	hit := ""
	mark := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			hit = name
			c.Status(http.StatusOK)
		}
	}

	a := r.Group("/api/v1").Group("/automation")
	a.PATCH("/swap/:id1/:id2", mark("swap"))
	a.GET("/", mark("getAll"))
	a.GET("/health/summary", mark("summary"))
	a.GET("/health-summary", mark("summary"))
	a.GET("/images/:imageName", mark("image"))
	a.GET("/:id", mark("getByID"))
	a.POST("/:id/launch", mark("launch"))
	a.POST("/:id/stop-runtime", mark("stopRuntime"))
	a.POST("/:id/health-check", mark("healthCheck"))
	a.GET("/:id/diagnostics", mark("diagnostics"))
	a.POST("/", mark("create"))
	a.PATCH("/", mark("update"))
	a.DELETE("/:id", mark("delete"))

	agentRuntimes := r.Group("/api/v1").Group("/agent-runtimes")
	agentRuntimes.GET("/", mark("agentRuntimeRegistry"))
	agentRuntimes.GET("/health", mark("agentRuntimeHealth"))
	agentRuntimes.GET("/:id/skills", mark("agentRuntimeSkills"))
	agentRuntimes.POST("/:id/tasks/:taskId/stop", mark("agentRuntimeStopTask"))
	agentRuntimes.GET("/openclaw/ecosystem", mark("openclawEcosystem"))
	agentRuntimes.PATCH("/openclaw/ecosystem", mark("openclawEcosystemSet"))
	agentRuntimes.POST("/openclaw/ecosystem/refresh", mark("openclawEcosystemRefresh"))
	agentRuntimes.POST("/openclaw/ecosystem/upload", mark("openclawEcosystemUpload"))

	agentCycle := r.Group("/api/v1").Group("/agent-cycle")
	agentCycle.POST("/run", mark("agentCycleRun"))

	assistantRoutes := r.Group("/api/v1").Group("/assistant")
	assistantRoutes.POST("/command", mark("assistantCommand"))
	assistantRoutes.GET("/logs", mark("assistantLogs"))

	m := r.Group("/api/v1").Group("/memory")
	m.GET("/", mark("memoryList"))
	m.GET("/health", mark("memoryHealth"))
	m.POST("/", mark("memoryCreate"))
	m.POST("/retrieve", mark("memoryRetrieve"))
	m.GET("/export", mark("memoryExport"))
	m.GET("/:id", mark("memoryGet"))

	memoryEngineRoutes := r.Group("/api/v1").Group("/memory-engine")
	memoryEngineRoutes.POST("/import", mark("memoryEngineImport"))
	memoryEngineRoutes.GET("/dashboard", mark("memoryEngineDashboard"))
	memoryEngineRoutes.POST("/search", mark("memoryEngineSearch"))
	memoryEngineRoutes.GET("/conversations", mark("memoryEngineConversations"))
	memoryEngineRoutes.GET("/conversations/:id", mark("memoryEngineConversation"))
	memoryEngineRoutes.DELETE("/conversations/:id", mark("memoryEngineConversationDelete"))
	memoryEngineRoutes.GET("/insights", mark("memoryEngineInsights"))

	llmRoutes := r.Group("/api/v1").Group("/llm")
	llmRoutes.GET("/policy", mark("llmPolicy"))
	llmRoutes.GET("/probes", mark("llmProbes"))
	llmRoutes.GET("/probes/history", mark("llmProbeHistory"))
	llmRoutes.GET("/model-maintenance", mark("llmModelMaintenance"))
	llmRoutes.POST("/model-maintenance/run", mark("llmModelMaintenanceRun"))
	llmRoutes.GET("/generations", mark("llmGenerations"))
	llmRoutes.POST("/route", mark("llmRoute"))
	llmRoutes.POST("/generate", mark("llmGenerate"))
	llmRoutes.GET("/logs", mark("llmLogs"))

	brainCatalog := r.Group("/api/v1").Group("/brain-catalog")
	brainCatalog.GET("/revalidation-history", mark("brainCatalogRevalidationHistory"))
	brainCatalog.GET("/collection-revalidation-history", mark("brainCatalogCollectionHistory"))
	brainCatalog.GET("/repository-discovery-revalidation-history", mark("brainCatalogDiscoveryHistory"))
	brainCatalog.POST("/revalidation/run", mark("brainCatalogRevalidationRun"))
	brainCatalog.POST("/collection-revalidation/run", mark("brainCatalogCollectionRun"))
	brainCatalog.POST("/repository-discovery-revalidation/run", mark("brainCatalogDiscoveryRun"))

	agentFramework := r.Group("/api/v1").Group("/agent-framework")
	agentFramework.GET("/status", mark("agentFrameworkStatus"))
	agentFramework.POST("/probe", mark("agentFrameworkProbe"))
	agentFramework.POST("/proposals", mark("agentFrameworkProposal"))

	autoGenCompat := r.Group("/api/v1").Group("/autogen-compat")
	autoGenCompat.GET("/status", mark("autoGenCompatStatus"))
	autoGenCompat.POST("/preview", mark("autoGenCompatPreview"))
	autoGenCompat.POST("/migration-plan", mark("autoGenCompatMigrationPlan"))

	crewAI := r.Group("/api/v1").Group("/crewai")
	crewAI.GET("/status", mark("crewAIStatus"))
	crewAI.POST("/probe", mark("crewAIProbe"))
	crewAI.POST("/proposals", mark("crewAIProposal"))

	doclingRoutes := r.Group("/api/v1").Group("/docling")
	doclingRoutes.GET("/status", mark("doclingStatus"))
	doclingRoutes.POST("/probe", mark("doclingProbe"))

	for path, prefix := range map[string]string{
		"/gitleaks": "gitleaks",
		"/gosec":    "gosec",
		"/grype":    "grype",
		"/trivy":    "trivy",
	} {
		group := r.Group("/api/v1").Group(path)
		group.GET("/status", mark(prefix+"Status"))
		group.POST("/probe", mark(prefix+"Probe"))
		group.POST("/scan", mark(prefix+"Scan"))
	}

	miniSWE := r.Group("/api/v1").Group("/mini-swe")
	miniSWE.GET("/status", mark("miniSWEStatus"))
	miniSWE.POST("/probe", mark("miniSWEProbe"))
	miniSWE.GET("/jobs", mark("miniSWEJobs"))
	miniSWE.POST("/workflows/:id/propose-patch", mark("miniSWEProposal"))

	mlflowRoutes := r.Group("/api/v1").Group("/mlflow")
	mlflowRoutes.GET("/status", mark("mlflowStatus"))
	mlflowRoutes.POST("/probe", mark("mlflowProbe"))
	mlflowRoutes.GET("/runs", mark("mlflowRuns"))

	openLITRoutes := r.Group("/api/v1").Group("/openlit")
	openLITRoutes.GET("/status", mark("openLITStatus"))
	openLITRoutes.POST("/export/operational-snapshot", mark("openLITExport"))

	syftRoutes := r.Group("/api/v1").Group("/syft")
	syftRoutes.GET("/status", mark("syftStatus"))
	syftRoutes.POST("/probe", mark("syftProbe"))
	syftRoutes.POST("/inventory", mark("syftInventory"))

	tasks := r.Group("/api/v1").Group("/task")
	tasks.POST("/plan", mark("taskPlan"))
	tasks.POST("/run", mark("taskRun"))
	tasks.POST("/success", mark("taskRun"))
	tasks.GET("/logs", mark("taskLogs"))
	tasks.GET("/review-queue", mark("taskReviewQueue"))
	tasks.POST("/review-queue/:id/resolve", mark("taskReviewResolve"))

	sources := r.Group("/api/v1").Group("/sources")
	sources.GET("/connectors", mark("sourceConnectors"))
	sources.GET("/", mark("sourceList"))
	sources.GET("/overview", mark("sourceOverview"))
	sources.POST("/", mark("sourceCreate"))
	sources.POST("/search", mark("sourceSearch"))
	sources.POST("/sync-due", mark("sourceSyncDue"))
	sources.GET("/extractions", mark("sourceExtractions"))
	sources.GET("/audit-logs", mark("sourceAuditLogs"))
	sources.PATCH("/extractions/:id", mark("sourceExtractionUpdate"))
	sources.POST("/extractions/:id/archive", mark("sourceExtractionArchive"))
	sources.DELETE("/extractions/:id", mark("sourceExtractionDelete"))
	sources.PATCH("/:id", mark("sourceUpdate"))
	sources.POST("/:id/sync", mark("sourceSync"))
	sources.POST("/:id/reindex", mark("sourceReindex"))
	sources.POST("/:id/pause", mark("sourcePause"))
	sources.POST("/:id/resume", mark("sourceResume"))
	sources.POST("/:id/revoke", mark("sourceRevoke"))

	verificationRoutes := r.Group("/api/v1").Group("/verification")
	verificationRoutes.POST("/answer", mark("verificationAnswer"))
	verificationRoutes.GET("/runs", mark("verificationRuns"))
	verificationRoutes.GET("/runs/:id", mark("verificationRunDetails"))

	operationsRoutes := r.Group("/api/v1").Group("/operations")
	operationsRoutes.GET("", mark("operationsList"))
	operationsRoutes.GET("/dashboard", mark("operationsDashboard"))
	operationsRoutes.GET("/:id", mark("operationsGet"))
	operationsRoutes.GET("/:id/events", mark("operationsEvents"))
	operationsRoutes.GET("/:id/approvals", mark("operationsApprovals"))
	operationsRoutes.POST("/:id/approve", mark("operationsApprove"))
	operationsRoutes.POST("/:id/reject", mark("operationsReject"))
	operationsRoutes.POST("/:id/later", mark("operationsLater"))
	operationsRoutes.POST("/:id/block-similar", mark("operationsBlockSimilar"))
	operationsRoutes.POST("/:id/run", mark("operationsRun"))
	operationsRoutes.POST("/:id/evidence-pack", mark("operationsEvidencePack"))
	r.Group("/api/v1").GET("/evidence-packs/:id", mark("evidencePackGet"))
	r.Group("/api/v1").POST("/background/run", mark("backgroundRun"))

	bgctl := r.Group("/api/v1").Group("/background")
	bgctl.GET("/status", mark("bgStatus"))
	bgctl.POST("/pause", mark("bgPause"))
	bgctl.POST("/control-approvals", mark("bgControlApprovalPrepare"))
	bgctl.POST("/control-approvals/:id/decision", mark("bgControlApprovalDecision"))
	bgctl.POST("/resume", mark("bgResume"))
	bgctl.PATCH("/mode", mark("bgMode"))

	wr := r.Group("/api/v1").Group("/windows-runtime")
	wr.GET("/readiness", mark("wrReadiness"))
	wr.POST("/recovery", mark("wrRecovery"))
	wr.POST("/emergency-stop/verify", mark("wrEmergencyVerify"))

	af := r.Group("/api/v1").Group("/account-feeds")
	af.GET("", mark("afList"))
	af.POST("", mark("afCreate"))
	af.GET("/bridges", mark("afBridges"))
	af.GET("/permissions", mark("afPermissions"))
	af.POST("/sync-due", mark("afSyncDue"))
	af.GET("/:id", mark("afGet"))
	af.PATCH("/:id", mark("afPatch"))
	af.POST("/:id/sync", mark("afSync"))
	af.GET("/:id/audit", mark("afAudit"))

	mi := r.Group("/api/v1").Group("/model-intelligence")
	mi.GET("/overview", mark("miOverview"))
	mi.GET("/profiles", mark("miProfiles"))
	mi.GET("/profiles/:providerId/:modelId", mark("miProfile"))
	mi.POST("/profiles/:providerId/:modelId/benchmark", mark("miBenchmark"))
	mi.GET("/benchmarks", mark("miBenchmarks"))
	mi.GET("/telemetry", mark("miTelemetry"))
	mi.GET("/lane-winners", mark("miLaneWinners"))
	mi.GET("/cache", mark("miCache"))
	mi.DELETE("/cache/:id", mark("miCacheDelete"))
	mi.GET("/token-budgets", mark("miBudgets"))
	mi.PATCH("/token-budgets", mark("miBudgetsUpdate"))

	hw := r.Group("/api/v1").Group("/hardware")
	hw.GET("/profile", mark("hwProfile"))
	hw.POST("/detect", mark("hwDetect"))
	hw.PATCH("/profile", mark("hwPatch"))

	power := r.Group("/api/v1").Group("/power")
	power.GET("/policy", mark("powerPolicy"))
	power.PATCH("/policy", mark("powerUpdate"))

	privacy := r.Group("/api/v1").Group("/privacy")
	privacy.POST("/scan", mark("privacyScan"))
	privacy.GET("/scans", mark("privacyScans"))
	privacy.GET("/scans/:id", mark("privacyScanByID"))

	rl := r.Group("/api/v1").Group("/runtime-lab")
	rl.GET("/overview", mark("rlOverview"))
	rl.POST("/:runtimeId/probe", mark("rlProbe"))
	rl.POST("/:runtimeId/self-test", mark("rlSelfTest"))
	rl.GET("/:runtimeId/attempts", mark("rlAttempts"))

	osRoutes := r.Group("/api/v1").Group("/os")
	osRoutes.GET("/overview", mark("osOverview"))

	frameworkRoutes := r.Group("/api/v1").Group("/framework-registry")
	frameworkRoutes.GET("/overview", mark("frameworkOverview"))
	frameworkRoutes.GET("/frameworks", mark("frameworkList"))
	frameworkRoutes.GET("/frameworks/:id", mark("frameworkGet"))
	frameworkRoutes.POST("/select", mark("frameworkSelect"))
	frameworkRoutes.PATCH("/frameworks/:id/preference", mark("frameworkPreference"))
	frameworkRoutes.GET("/selections", mark("frameworkSelections"))
	frameworkRoutes.GET("/selections/:id", mark("frameworkSelection"))
	frameworkRoutes.GET("/constitution", mark("frameworkConstitution"))
	frameworkRoutes.GET("/constitution/history", mark("frameworkConstitutionHistory"))
	frameworkRoutes.POST("/constitution/drafts", mark("frameworkConstitutionDraft"))
	frameworkRoutes.POST("/constitution/:id/activate", mark("frameworkConstitutionActivate"))

	workflowRoutes := r.Group("/api/v1").Group("/workflow")
	workflowRoutes.GET("/overview", mark("workflowOverview"))
	workflowRoutes.GET("/approvals", mark("workflowApprovals"))
	workflowRoutes.GET("/dashboard", mark("workflowDashboard"))
	workflowRoutes.GET("/reminder-proposals", mark("workflowReminderProposals"))
	workflowRoutes.GET("/", mark("workflowItems"))
	workflowRoutes.POST("/intake", mark("workflowIntake"))
	workflowRoutes.POST("/recover-stale", mark("workflowRecoverStale"))
	workflowRoutes.POST("/run-due", mark("workflowRunDue"))
	workflowRoutes.POST("/open-loops/run-due", mark("workflowOpenLoopRunDue"))
	workflowRoutes.GET("/:id", mark("workflowGet"))
	workflowRoutes.POST("/:id/run", mark("workflowRunOne"))
	workflowRoutes.POST("/:id/transition", mark("workflowTransition"))
	workflowRoutes.POST("/:id/approval", mark("workflowApprovalResolve"))
	workflowRoutes.POST("/:id/interruption/resolve", mark("workflowInterruptionResolve"))
	workflowRoutes.POST("/:id/proposals/:proposalId/resolve", mark("workflowProposalResolve"))
	workflowRoutes.PATCH("/:id/checklist/:itemId", mark("workflowChecklist"))

	pursuits := r.Group("/api/v1").Group("/pursuits")
	pursuits.GET("/", mark("pursuitList"))
	pursuits.POST("/", mark("pursuitCreate"))
	pursuits.GET("/dashboard", mark("pursuitDashboard"))
	pursuits.GET("/brief", mark("pursuitBrief"))
	pursuits.GET("/decisions", mark("pursuitDecisions"))
	pursuits.POST("/match", mark("pursuitMatch"))
	pursuits.POST("/intake", mark("pursuitRouteIntake"))
	pursuits.GET("/:id/evidence", mark("pursuitEvidence"))
	pursuits.GET("/:id", mark("pursuitGet"))
	pursuits.PATCH("/:id", mark("pursuitUpdate"))
	pursuits.POST("/:id/archive", mark("pursuitArchive"))
	pursuits.POST("/:id/summary", mark("pursuitSummary"))
	pursuits.POST("/:id/review", mark("pursuitReview"))
	pursuits.POST("/:id/decisions/resolve", mark("pursuitDecisionResolve"))
	pursuits.GET("/:id/activity", mark("pursuitActivity"))
	pursuits.GET("/:id/next-actions", mark("pursuitNextActions"))
	pursuits.GET("/:id/blockers", mark("pursuitBlockers"))
	pursuits.GET("/:id/approvals", mark("pursuitApprovals"))
	pursuits.POST("/:id/intake", mark("pursuitIntake"))
	pursuits.POST("/:id/plan", mark("pursuitPlan"))
	pursuits.POST("/:id/candidate/accept", mark("pursuitCandidateAccept"))
	pursuits.POST("/:id/links", mark("pursuitLink"))
	pursuits.DELETE("/:id/links/:linkId", mark("pursuitDeleteLink"))

	cases := []struct {
		method, path, want string
	}{
		{"GET", "/api/v1/automation/health/summary", "summary"},
		{"GET", "/api/v1/automation/health-summary", "summary"},
		{"POST", "/api/v1/automation/abc/launch", "launch"},
		{"POST", "/api/v1/automation/abc/stop-runtime", "stopRuntime"},
		{"POST", "/api/v1/automation/abc/health-check", "healthCheck"},
		{"GET", "/api/v1/automation/abc/diagnostics", "diagnostics"},
		{"GET", "/api/v1/automation/abc", "getByID"},
		{"GET", "/api/v1/automation/images/logo.png", "image"},
		{"PATCH", "/api/v1/automation/swap/1/2", "swap"},
		{"GET", "/api/v1/agent-runtimes/", "agentRuntimeRegistry"},
		{"GET", "/api/v1/agent-runtimes/health", "agentRuntimeHealth"},
		{"GET", "/api/v1/agent-runtimes/openclaw/skills", "agentRuntimeSkills"},
		{"POST", "/api/v1/agent-runtimes/openclaw/tasks/task-1/stop", "agentRuntimeStopTask"},
		{"GET", "/api/v1/agent-runtimes/openclaw/ecosystem", "openclawEcosystem"},
		{"PATCH", "/api/v1/agent-runtimes/openclaw/ecosystem", "openclawEcosystemSet"},
		{"POST", "/api/v1/agent-runtimes/openclaw/ecosystem/refresh", "openclawEcosystemRefresh"},
		{"POST", "/api/v1/agent-runtimes/openclaw/ecosystem/upload", "openclawEcosystemUpload"},
		{"GET", "/api/v1/pursuits/decisions", "pursuitDecisions"},
		{"POST", "/api/v1/agent-cycle/run", "agentCycleRun"},
		{"GET", "/api/v1/background/status", "bgStatus"},
		{"POST", "/api/v1/background/pause", "bgPause"},
		{"POST", "/api/v1/background/control-approvals", "bgControlApprovalPrepare"},
		{"POST", "/api/v1/background/control-approvals/11111111-1111-1111-1111-111111111111/decision", "bgControlApprovalDecision"},
		{"POST", "/api/v1/background/resume", "bgResume"},
		{"PATCH", "/api/v1/background/mode", "bgMode"},
		{"GET", "/api/v1/windows-runtime/readiness", "wrReadiness"},
		{"POST", "/api/v1/windows-runtime/recovery", "wrRecovery"},
		{"POST", "/api/v1/windows-runtime/emergency-stop/verify", "wrEmergencyVerify"},
		{"POST", "/api/v1/assistant/command", "assistantCommand"},
		{"GET", "/api/v1/assistant/logs", "assistantLogs"},
		{"POST", "/api/v1/memory/retrieve", "memoryRetrieve"},
		{"GET", "/api/v1/memory/health", "memoryHealth"},
		{"GET", "/api/v1/memory/export", "memoryExport"},
		{"GET", "/api/v1/memory/abc", "memoryGet"},
		{"POST", "/api/v1/memory-engine/import", "memoryEngineImport"},
		{"GET", "/api/v1/memory-engine/dashboard", "memoryEngineDashboard"},
		{"POST", "/api/v1/memory-engine/search", "memoryEngineSearch"},
		{"GET", "/api/v1/memory-engine/conversations", "memoryEngineConversations"},
		{"GET", "/api/v1/memory-engine/conversations/abc", "memoryEngineConversation"},
		{"DELETE", "/api/v1/memory-engine/conversations/abc", "memoryEngineConversationDelete"},
		{"GET", "/api/v1/memory-engine/insights", "memoryEngineInsights"},
		{"GET", "/api/v1/llm/policy", "llmPolicy"},
		{"GET", "/api/v1/llm/probes", "llmProbes"},
		{"GET", "/api/v1/llm/probes/history", "llmProbeHistory"},
		{"GET", "/api/v1/llm/model-maintenance", "llmModelMaintenance"},
		{"POST", "/api/v1/llm/model-maintenance/run", "llmModelMaintenanceRun"},
		{"GET", "/api/v1/llm/generations", "llmGenerations"},
		{"POST", "/api/v1/llm/route", "llmRoute"},
		{"POST", "/api/v1/llm/generate", "llmGenerate"},
		{"GET", "/api/v1/llm/logs", "llmLogs"},
		{"GET", "/api/v1/brain-catalog/revalidation-history", "brainCatalogRevalidationHistory"},
		{"GET", "/api/v1/brain-catalog/collection-revalidation-history", "brainCatalogCollectionHistory"},
		{"GET", "/api/v1/brain-catalog/repository-discovery-revalidation-history", "brainCatalogDiscoveryHistory"},
		{"POST", "/api/v1/brain-catalog/revalidation/run", "brainCatalogRevalidationRun"},
		{"POST", "/api/v1/brain-catalog/collection-revalidation/run", "brainCatalogCollectionRun"},
		{"POST", "/api/v1/brain-catalog/repository-discovery-revalidation/run", "brainCatalogDiscoveryRun"},
		{"GET", "/api/v1/agent-framework/status", "agentFrameworkStatus"},
		{"POST", "/api/v1/agent-framework/probe", "agentFrameworkProbe"},
		{"POST", "/api/v1/agent-framework/proposals", "agentFrameworkProposal"},
		{"GET", "/api/v1/autogen-compat/status", "autoGenCompatStatus"},
		{"POST", "/api/v1/autogen-compat/preview", "autoGenCompatPreview"},
		{"POST", "/api/v1/autogen-compat/migration-plan", "autoGenCompatMigrationPlan"},
		{"GET", "/api/v1/crewai/status", "crewAIStatus"},
		{"POST", "/api/v1/crewai/probe", "crewAIProbe"},
		{"POST", "/api/v1/crewai/proposals", "crewAIProposal"},
		{"GET", "/api/v1/docling/status", "doclingStatus"},
		{"POST", "/api/v1/docling/probe", "doclingProbe"},
		{"GET", "/api/v1/gitleaks/status", "gitleaksStatus"},
		{"POST", "/api/v1/gitleaks/probe", "gitleaksProbe"},
		{"POST", "/api/v1/gitleaks/scan", "gitleaksScan"},
		{"GET", "/api/v1/gosec/status", "gosecStatus"},
		{"POST", "/api/v1/gosec/probe", "gosecProbe"},
		{"POST", "/api/v1/gosec/scan", "gosecScan"},
		{"GET", "/api/v1/grype/status", "grypeStatus"},
		{"POST", "/api/v1/grype/probe", "grypeProbe"},
		{"POST", "/api/v1/grype/scan", "grypeScan"},
		{"GET", "/api/v1/trivy/status", "trivyStatus"},
		{"POST", "/api/v1/trivy/probe", "trivyProbe"},
		{"POST", "/api/v1/trivy/scan", "trivyScan"},
		{"GET", "/api/v1/mini-swe/status", "miniSWEStatus"},
		{"POST", "/api/v1/mini-swe/probe", "miniSWEProbe"},
		{"GET", "/api/v1/mini-swe/jobs", "miniSWEJobs"},
		{"POST", "/api/v1/mini-swe/workflows/abc/propose-patch", "miniSWEProposal"},
		{"GET", "/api/v1/mlflow/status", "mlflowStatus"},
		{"POST", "/api/v1/mlflow/probe", "mlflowProbe"},
		{"GET", "/api/v1/mlflow/runs", "mlflowRuns"},
		{"GET", "/api/v1/openlit/status", "openLITStatus"},
		{"POST", "/api/v1/openlit/export/operational-snapshot", "openLITExport"},
		{"GET", "/api/v1/syft/status", "syftStatus"},
		{"POST", "/api/v1/syft/probe", "syftProbe"},
		{"POST", "/api/v1/syft/inventory", "syftInventory"},
		{"POST", "/api/v1/task/plan", "taskPlan"},
		{"POST", "/api/v1/task/run", "taskRun"},
		{"POST", "/api/v1/task/success", "taskRun"},
		{"GET", "/api/v1/task/logs", "taskLogs"},
		{"GET", "/api/v1/task/review-queue", "taskReviewQueue"},
		{"POST", "/api/v1/task/review-queue/abc/resolve", "taskReviewResolve"},
		{"GET", "/api/v1/sources/connectors", "sourceConnectors"},
		{"GET", "/api/v1/sources/", "sourceList"},
		{"POST", "/api/v1/sources/", "sourceCreate"},
		{"POST", "/api/v1/sources/search", "sourceSearch"},
		{"POST", "/api/v1/sources/sync-due", "sourceSyncDue"},
		{"GET", "/api/v1/sources/extractions", "sourceExtractions"},
		{"GET", "/api/v1/sources/audit-logs", "sourceAuditLogs"},
		{"PATCH", "/api/v1/sources/extractions/abc", "sourceExtractionUpdate"},
		{"POST", "/api/v1/sources/extractions/abc/archive", "sourceExtractionArchive"},
		{"DELETE", "/api/v1/sources/extractions/abc", "sourceExtractionDelete"},
		{"PATCH", "/api/v1/sources/abc", "sourceUpdate"},
		{"POST", "/api/v1/sources/abc/sync", "sourceSync"},
		{"POST", "/api/v1/sources/abc/reindex", "sourceReindex"},
		{"POST", "/api/v1/sources/abc/pause", "sourcePause"},
		{"POST", "/api/v1/sources/abc/resume", "sourceResume"},
		{"POST", "/api/v1/sources/abc/revoke", "sourceRevoke"},
		{"POST", "/api/v1/verification/answer", "verificationAnswer"},
		{"GET", "/api/v1/verification/runs", "verificationRuns"},
		{"GET", "/api/v1/verification/runs/abc", "verificationRunDetails"},
		{"GET", "/api/v1/os/overview", "osOverview"},
		{"GET", "/api/v1/framework-registry/overview", "frameworkOverview"},
		{"GET", "/api/v1/framework-registry/frameworks", "frameworkList"},
		{"GET", "/api/v1/framework-registry/frameworks/human-sovereignty", "frameworkGet"},
		{"POST", "/api/v1/framework-registry/select", "frameworkSelect"},
		{"PATCH", "/api/v1/framework-registry/frameworks/human-sovereignty/preference", "frameworkPreference"},
		{"GET", "/api/v1/framework-registry/selections", "frameworkSelections"},
		{"GET", "/api/v1/framework-registry/selections/11111111-1111-1111-1111-111111111111", "frameworkSelection"},
		{"GET", "/api/v1/framework-registry/constitution", "frameworkConstitution"},
		{"GET", "/api/v1/framework-registry/constitution/history", "frameworkConstitutionHistory"},
		{"POST", "/api/v1/framework-registry/constitution/drafts", "frameworkConstitutionDraft"},
		{"POST", "/api/v1/framework-registry/constitution/custom-v2/activate", "frameworkConstitutionActivate"},
		{"GET", "/api/v1/workflow/overview", "workflowOverview"},
		{"GET", "/api/v1/workflow/approvals", "workflowApprovals"},
		{"GET", "/api/v1/workflow/dashboard", "workflowDashboard"},
		{"GET", "/api/v1/workflow/reminder-proposals", "workflowReminderProposals"},
		{"GET", "/api/v1/workflow/", "workflowItems"},
		{"POST", "/api/v1/workflow/intake", "workflowIntake"},
		{"POST", "/api/v1/workflow/recover-stale", "workflowRecoverStale"},
		{"POST", "/api/v1/workflow/run-due", "workflowRunDue"},
		{"POST", "/api/v1/workflow/open-loops/run-due", "workflowOpenLoopRunDue"},
		{"GET", "/api/v1/workflow/abc", "workflowGet"},
		{"POST", "/api/v1/workflow/abc/run", "workflowRunOne"},
		{"POST", "/api/v1/workflow/abc/transition", "workflowTransition"},
		{"POST", "/api/v1/workflow/abc/approval", "workflowApprovalResolve"},
		{"POST", "/api/v1/workflow/abc/interruption/resolve", "workflowInterruptionResolve"},
		{"POST", "/api/v1/workflow/abc/proposals/def/resolve", "workflowProposalResolve"},
		{"PATCH", "/api/v1/workflow/abc/checklist/def", "workflowChecklist"},
		{"GET", "/api/v1/pursuits/", "pursuitList"},
		{"POST", "/api/v1/pursuits/", "pursuitCreate"},
		{"GET", "/api/v1/pursuits/dashboard", "pursuitDashboard"},
		{"GET", "/api/v1/pursuits/brief", "pursuitBrief"},
		{"POST", "/api/v1/pursuits/match", "pursuitMatch"},
		{"POST", "/api/v1/pursuits/intake", "pursuitRouteIntake"},
		{"GET", "/api/v1/pursuits/abc/evidence?uri=automation-launch://example", "pursuitEvidence"},
		{"GET", "/api/v1/pursuits/abc", "pursuitGet"},
		{"PATCH", "/api/v1/pursuits/abc", "pursuitUpdate"},
		{"POST", "/api/v1/pursuits/abc/archive", "pursuitArchive"},
		{"POST", "/api/v1/pursuits/abc/summary", "pursuitSummary"},
		{"POST", "/api/v1/pursuits/abc/review", "pursuitReview"},
		{"POST", "/api/v1/pursuits/abc/decisions/resolve", "pursuitDecisionResolve"},
		{"GET", "/api/v1/pursuits/abc/activity", "pursuitActivity"},
		{"GET", "/api/v1/pursuits/abc/next-actions", "pursuitNextActions"},
		{"GET", "/api/v1/pursuits/abc/blockers", "pursuitBlockers"},
		{"GET", "/api/v1/pursuits/abc/approvals", "pursuitApprovals"},
		{"POST", "/api/v1/pursuits/abc/intake", "pursuitIntake"},
		{"POST", "/api/v1/pursuits/abc/plan", "pursuitPlan"},
		{"POST", "/api/v1/pursuits/abc/candidate/accept", "pursuitCandidateAccept"},
		{"POST", "/api/v1/pursuits/abc/links", "pursuitLink"},
		{"DELETE", "/api/v1/pursuits/abc/links/def", "pursuitDeleteLink"},
	}
	for _, tc := range cases {
		hit = ""
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s -> code %d, want 200", tc.method, tc.path, w.Code)
		}
		if hit != tc.want {
			t.Fatalf("%s %s -> handler %q, want %q", tc.method, tc.path, hit, tc.want)
		}
	}
}

func TestMCPPreflightRoutesRequireOwnerAndAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := mcppreflight.NewService(mcppreflight.Config{
		Enabled: false,
		Servers: []mcppreflight.Server{{ID: "local", URL: "http://127.0.0.1:3000/mcp"}},
	})

	unauthenticated := gin.New()
	initializeMCPPreflightRoutes(unauthenticated.Group("/api/v1"), mcppreflight.NewHandler(service))
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/mcp-preflight/overview", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("overview without an owner must be unauthorized, got %d", response.Code)
	}

	operator := gin.New()
	operator.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "operator@example.test")
		c.Set(contextRoleKey, "operator")
		c.Next()
	})
	initializeMCPPreflightRoutes(operator.Group("/api/v1"), mcppreflight.NewHandler(service))
	response = httptest.NewRecorder()
	operator.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/mcp-preflight/local/run", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("operator must not run a preflight, got %d", response.Code)
	}

	owner := gin.New()
	owner.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "owner@example.test")
		c.Set(contextRoleKey, "owner")
		c.Next()
	})
	initializeMCPPreflightRoutes(owner.Group("/api/v1"), mcppreflight.NewHandler(service))
	response = httptest.NewRecorder()
	owner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/mcp-preflight/local/run", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("owner must receive the truthful disabled status, got %d", response.Code)
	}
}

func TestSerenaRoutesRequireOwnerAndPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := serena.NewService(false, "", "", nil)

	unauthenticated := gin.New()
	initializeSerenaRoutes(unauthenticated.Group("/api/v1"), serena.NewHandler(service))
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/serena/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status without an owner must be unauthorized, got %d", response.Code)
	}

	viewer := gin.New()
	viewer.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "viewer@example.test")
		c.Set(contextRoleKey, "viewer")
		c.Next()
	})
	initializeSerenaRoutes(viewer.Group("/api/v1"), serena.NewHandler(service))
	response = httptest.NewRecorder()
	viewer.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/serena/symbols", strings.NewReader(`{"pattern":"Service"}`)))
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer must not request semantic code context, got %d", response.Code)
	}

	owner := gin.New()
	owner.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "owner@example.test")
		c.Set(contextRoleKey, "owner")
		c.Next()
	})
	initializeSerenaRoutes(owner.Group("/api/v1"), serena.NewHandler(service))
	response = httptest.NewRecorder()
	owner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/serena/probe", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner must receive truthful disabled state, got %d", response.Code)
	}
}

func TestPlanningOptimizerRoutesRequireOwnerAndWritePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := planningoptimizer.NewService(optimizerRouteRepository{}, false, "http://127.0.0.1:8080", 0)
	payload := `{"dayStartMinute":540,"dayEndMinute":600,"jobs":[{"id":"job","durationMinutes":60,"priority":50}]}`

	unauthenticated := gin.New()
	initializePlanningOptimizerRoutes(unauthenticated.Group("/api/v1"), planningoptimizer.NewHandler(service))
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/planning-optimizer/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("optimizer status without an owner must be unauthorized, got %d", response.Code)
	}

	viewer := gin.New()
	viewer.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "viewer@example.test")
		c.Set(contextRoleKey, "viewer")
		c.Next()
	})
	initializePlanningOptimizerRoutes(viewer.Group("/api/v1"), planningoptimizer.NewHandler(service))
	response = httptest.NewRecorder()
	viewer.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/planning-optimizer/proposals", strings.NewReader(payload)))
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer must not request a proposal, got %d", response.Code)
	}

	operator := gin.New()
	operator.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "operator@example.test")
		c.Set(contextRoleKey, "operator")
		c.Next()
	})
	initializePlanningOptimizerRoutes(operator.Group("/api/v1"), planningoptimizer.NewHandler(service))
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/planning-optimizer/proposals", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	operator.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("operator must receive the truthful disabled status, got %d: %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	operator.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/planning-optimizer/probe", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("operator must not run an optimizer probe, got %d", response.Code)
	}

	owner := gin.New()
	owner.Use(func(c *gin.Context) {
		c.Set(contextSubjectKey, "owner@example.test")
		c.Set(contextRoleKey, "owner")
		c.Next()
	})
	initializePlanningOptimizerRoutes(owner.Group("/api/v1"), planningoptimizer.NewHandler(service))
	response = httptest.NewRecorder()
	owner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/planning-optimizer/probe", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("owner must receive truthful disabled probe state, got %d", response.Code)
	}
}

func TestBackendAPIKeyMiddlewareDisabledWithoutKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.AppConfig.BackendAPIKey
	t.Cleanup(func() { config.AppConfig.BackendAPIKey = previous })
	config.AppConfig.BackendAPIKey = ""

	r := gin.New()
	r.Use(backendAPIKeyMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestLocalCaptureCORSAllowsExtensionPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(localCaptureCORSMiddleware())
	r.POST("/api/v1/memory-engine/import", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/memory-engine/import", nil)
	req.Header.Set("Origin", "chrome-extension://example-extension")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "chrome-extension://example-extension" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, X-HAI-Backend-Key" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestLocalCaptureCORSRejectsUntrustedPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(localCaptureCORSMiddleware())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/memory-engine/import", nil)
	req.Header.Set("Origin", "https://attacker.example")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestBackendAPIKeyMiddlewareBlocksMissingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.AppConfig.BackendAPIKey
	t.Cleanup(func() { config.AppConfig.BackendAPIKey = previous })
	config.AppConfig.BackendAPIKey = "secret"

	r := gin.New()
	r.Use(backendAPIKeyMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestBackendAPIKeyMiddlewareAllowsMatchingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.AppConfig.BackendAPIKey
	t.Cleanup(func() { config.AppConfig.BackendAPIKey = previous })
	config.AppConfig.BackendAPIKey = "secret"

	r := gin.New()
	r.Use(backendAPIKeyMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set(backendAPIKeyHeader, "secret")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

package router

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"automation-hub-backend/docs"
	"automation-hub-backend/internal/a2abridge"
	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/agentcycle"
	"automation-hub-backend/internal/agentframework"
	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/ambient"
	"automation-hub-backend/internal/ambientmonitor"
	"automation-hub-backend/internal/anythingllm"
	"automation-hub-backend/internal/assistant"
	"automation-hub-backend/internal/autogencompat"
	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/autonomy"
	"automation-hub-backend/internal/braincatalog"
	"automation-hub-backend/internal/browserverify"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/controlledlearning"
	"automation-hub-backend/internal/crewai"
	"automation-hub-backend/internal/deepeval"
	"automation-hub-backend/internal/deepteam"
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/doctor"
	"automation-hub-backend/internal/domainpack"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/evidently"
	"automation-hub-backend/internal/executionapproval"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/featureflags"
	"automation-hub-backend/internal/frameworkevidence"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/garak"
	"automation-hub-backend/internal/gitleaks"
	"automation-hub-backend/internal/gosec"
	"automation-hub-backend/internal/grype"
	"automation-hub-backend/internal/guardrails"
	"automation-hub-backend/internal/haios"
	"automation-hub-backend/internal/hardwareprofile"
	"automation-hub-backend/internal/health"
	"automation-hub-backend/internal/i18n"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/knowledgegraph"
	"automation-hub-backend/internal/langfuse"
	"automation-hub-backend/internal/lifeledger"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/lifeops"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/lmeval"
	"automation-hub-backend/internal/mcpbridge"
	"automation-hub-backend/internal/mcppreflight"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/memoryengine"
	"automation-hub-backend/internal/miniswe"
	"automation-hub-backend/internal/mlflow"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/openlit"
	"automation-hub-backend/internal/operationalgraph"
	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/outcomeevaluation"
	"automation-hub-backend/internal/phase2"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/planningoptimizer"
	"automation-hub-backend/internal/presidio"
	"automation-hub-backend/internal/privacyfilter"
	"automation-hub-backend/internal/proactivity"
	"automation-hub-backend/internal/promptfoo"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/pydanticai"
	"automation-hub-backend/internal/ragflow"
	"automation-hub-backend/internal/rbac"
	"automation-hub-backend/internal/research"
	"automation-hub-backend/internal/resilience"
	"automation-hub-backend/internal/runtimelab"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/semantic"
	"automation-hub-backend/internal/serena"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"
	"automation-hub-backend/internal/standingmandate"
	"automation-hub-backend/internal/syft"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/internal/temporalbridge"
	"automation-hub-backend/internal/trivy"
	"automation-hub-backend/internal/verification"
	"automation-hub-backend/internal/wasiexec"
	"automation-hub-backend/internal/whispercpp"
	"automation-hub-backend/internal/workflow"
	"automation-hub-backend/internal/workflow/approvaladapter"
	"automation-hub-backend/internal/workflowtask"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func initializeRoutes(appCtx context.Context, router *gin.Engine, monitorObserver ambientmonitor.SweepObserver) error {
	if appCtx == nil {
		return fmt.Errorf("application context is required")
	}
	database, err := infra.GetDefaultDB()
	if err != nil {
		return fmt.Errorf("initialize event delivery database: %w", err)
	}
	eventPublisher := events.DefaultPublisher()
	eventOutbox := events.NewOutboxStore(database)
	eventDispatcher := events.NewOutboxDispatcher(eventOutbox, eventPublisher)
	go eventDispatcher.Run(appCtx)
	go func() {
		<-appCtx.Done()
		if err := eventPublisher.Close(); err != nil {
			log.Printf("close Kafka event publisher: %v", err)
		}
	}()

	router.GET("/healthz", livenessHandler)
	readinessReport := func(ctx context.Context) doctor.Report {
		// Static configuration diagnosis, then live dependency probes. The
		// second half is what makes the answer trustworthy: without it a
		// process with an unreachable database still reports itself ready.
		configured := doctor.Diagnose(config.AppConfig)
		live := doctor.RunProbes(ctx, health.DefaultTimeout, health.Probes(config.AppConfig))
		return configured.Merge(live...)
	}
	router.GET("/readyz", readinessHandler(readinessReport))

	relativePathV1 := config.AppConfig.BaseUrl + "/v1"
	docs.SwaggerInfo.BasePath = relativePathV1
	v1 := router.Group(relativePathV1)
	v1.Use(backendAPIKeyMiddleware())
	v1.Use(identityMiddleware())
	{
		modelIntelService := modelintelligence.DefaultService()
		phase2Module := phase2.DefaultModuleWithModelIntel(modelIntelService)
		opsControlService := phase2Module.OpsControl()
		safety.SetEmergencyStopProvider(opsControlService.Control())
		backgroundAllowed := func() bool {
			return opsControlService.Control().Mode().AllowsBackgroundProcessing()
		}
		initializeMCPPreflightRoutes(v1, mcppreflight.NewHandler(mcppreflight.NewServiceFromEnv()))
		initializePlanningOptimizerRoutes(v1, planningoptimizer.NewHandler(planningoptimizer.DefaultService()))
		planGraphRepository, err := plangraph.DefaultRepository()
		if err != nil {
			return err
		}
		planGraphService := plangraph.NewService(planGraphRepository, nil)
		initializePlanGraphRoutes(v1, plangraph.NewHandler(planGraphService))
		initializePydanticAIRoutes(v1, pydanticai.NewHandler(pydanticai.DefaultService()))
		initializeBrowserVerificationRoutes(v1, browserverify.NewHandler(browserverify.DefaultService()))
		initializeResearchRoutes(v1, research.NewHandler(research.DefaultService()))
		initializeRAGFlowRoutes(v1, ragflow.NewHandler(ragflow.DefaultService()))
		initializeAnythingLLMRoutes(v1, anythingllm.NewHandler(anythingllm.DefaultService()))
		initializeSerenaRoutes(v1, serena.NewHandler(serena.DefaultService()))
		initializePresidioRoutes(v1, presidio.NewHandler(presidio.DefaultService()))
		initializeEvidentlyRoutes(v1, evidently.NewHandler(evidently.DefaultService()))
		initializeGuardrailsRoutes(v1, guardrails.NewHandler(guardrails.DefaultService()))
		initializeLMEvalRoutes(v1, lmeval.NewHandler(lmeval.DefaultService()))
		initializePromptfooRoutes(v1, promptfoo.NewHandler(promptfoo.DefaultService()))
		initializeDeepEvalRoutes(v1, deepeval.NewHandler(deepeval.DefaultService()))
		initializeDeepTeamRoutes(v1, deepteam.NewHandler(deepteam.DefaultService()))
		initializeGarakRoutes(v1, garak.NewHandler(garak.DefaultService()))
		initializeLangfuseRoutes(v1, langfuse.NewHandler(langfuse.DefaultService()))
		whisperService := whispercpp.DefaultService()
		initializeWhisperCPPRoutes(v1, whispercpp.NewHandler(whisperService))
		probeHistory, err := llm.DefaultProbeHistoryRepository()
		if err != nil {
			return err
		}
		modelMaintenanceHistory, err := llm.DefaultModelMaintenanceRepository()
		if err != nil {
			return err
		}
		generationHistory, err := llm.DefaultGenerationHistoryRepository()
		if err != nil {
			return err
		}
		llmService, err := llm.NewServiceFromEnvWithOperationalHistories(probeHistory, modelMaintenanceHistory, generationHistory)
		if err != nil {
			return err
		}
		llmService.WithModelTelemetryRepository(modelintelligence.DefaultTelemetryRepository())
		modelIntelService.WithModelMaintenance(llmService)
		catalogHistory, err := braincatalog.DefaultUpstreamReviewHistoryRepository()
		if err != nil {
			return err
		}
		collectionHistory, err := braincatalog.DefaultCollectionReviewHistoryRepository()
		if err != nil {
			return err
		}
		discoveryHistory, err := braincatalog.DefaultRepositoryDiscoveryReviewHistoryRepository()
		if err != nil {
			return err
		}
		catalogReviewer := braincatalog.NewUpstreamReviewer(nil)
		collectionReviewer := braincatalog.NewOSSInsightCollectionReviewer(nil)
		repositoryScout := braincatalog.NewOSSInsightRepositoryScout(nil)
		catalogMaintenance := braincatalog.NewCatalogMaintenanceService(catalogReviewer, catalogHistory).
			WithCollectionMaintenance(collectionReviewer, collectionHistory).
			WithRepositoryDiscoveryMaintenance(repositoryScout, discoveryHistory)
		catalogHandler := braincatalog.NewHandlerWithReviewersAndScout(catalogReviewer, collectionReviewer, repositoryScout).
			WithMaintenance(catalogMaintenance)
		initializeBrainCatalogRoutes(v1, catalogHandler)
		braincatalog.StartCatalogRevalidationScheduler(appCtx, catalogMaintenance, backgroundAllowed)
		semanticService := semantic.NewServiceFromEnv()
		memoryService := memory.NewServiceWithSemantic(memory.DefaultRepository(), semanticService)
		initializeMemoryRoutes(v1, memory.NewHandler(memoryService))
		workflowRunner := workflowtask.NewDeferredRunner()
		workflowRepository := workflow.DefaultRepository()
		workflowService := workflow.NewServiceWithTaskRunner(workflowRepository, workflowRunner, memoryService)
		workflowService, err = workflow.WithAcceptedPlanResolver(workflowService, planGraphService)
		if err != nil {
			return err
		}
		workflowService, err = workflow.WithCoordinationPlanProjector(workflowService, planGraphService)
		if err != nil {
			return err
		}
		initializeAgentFrameworkRoutes(v1, agentframework.NewHandler(agentframework.WithModelMaintenance(agentframework.DefaultService(), llmService)))
		initializeAutoGenCompatibilityRoutes(v1, autogencompat.NewHandler(autogencompat.DefaultService()))
		initializeCrewAIRoutes(v1, crewai.NewHandler(crewai.WithModelMaintenance(crewai.DefaultService(), llmService)))
		initializeDoclingRoutes(v1, docling.NewHandler(docling.DefaultService()))
		gitleaksService := gitleaks.DefaultService()
		if workflowLinker, ok := workflowService.(gitleaks.WorkflowLinker); ok {
			gitleaksService = gitleaks.DefaultService(workflowLinker)
		}
		initializeGitleaksRoutes(v1, gitleaks.NewHandler(gitleaksService))
		initializeGosecRoutes(v1, gosec.NewHandler(gosec.DefaultService()))
		initializeGrypeRoutes(v1, grype.NewHandler(grype.DefaultService()))
		initializeMiniSWERoutes(v1, miniswe.NewHandler(miniswe.WithModelMaintenance(miniswe.DefaultService(workflowService), llmService)))
		initializeMLflowRoutes(v1, mlflow.NewHandler(mlflow.DefaultService()))
		initializeOpenLITRoutes(v1, openlit.NewHandler(openlit.DefaultService()))
		syftService := syft.DefaultService()
		if workflowLinker, ok := workflowService.(syft.WorkflowLinker); ok {
			syftService = syft.DefaultService(workflowLinker)
		}
		initializeSyftRoutes(v1, syft.NewHandler(syftService))
		initializeTrivyRoutes(v1, trivy.NewHandler(trivy.DefaultService()))
		pursuitRepository := pursuit.DefaultRepository()
		pursuitService := pursuit.NewService(pursuitRepository, workflowService)
		pursuitService, err = pursuit.WithAcceptedPlanResolver(pursuitService, planGraphService)
		if err != nil {
			return err
		}
		sourceService := source.NewServiceWithWorkflowPursuitAndSemantic(source.DefaultRepository(), memoryService, workflowService, pursuitService, semanticService)
		sourceEvidenceRepository, err := sourceevidence.DefaultRepository()
		if err != nil {
			return err
		}
		knowledgeRepository, err := knowledgegraph.DefaultRepository()
		if err != nil {
			return err
		}
		knowledgeService := knowledgegraph.NewService(knowledgeRepository, nil)
		if err := initializeKnowledgeClaimRoutes(v1, knowledgegraph.NewClaimHandler(knowledgeService)); err != nil {
			return err
		}
		verificationService := verification.NewServiceWithEvidenceResolvers(
			verification.DefaultRepository(), sourceService, memoryService, nil,
			sourceEvidenceRepository, pursuitService,
		)
		verificationService, err = verification.WithClaimProjector(
			verificationService,
			knowledgeClaimProjector{service: knowledgeService},
		)
		if err != nil {
			return err
		}
		initializeVerificationRoutes(v1, verification.NewHandler(verificationService))
		frameworkService, err := frameworkregistry.DefaultService()
		if err != nil {
			return err
		}
		initializeFrameworkRegistryRoutes(v1, frameworkregistry.NewHandler(frameworkService))
		agentTeamRepository, err := frameworkregistry.DefaultAgentTeamRepository()
		if err != nil {
			return err
		}
		agentTeamService := frameworkregistry.NewAgentTeamService(agentTeamRepository)
		if err := initializeAgentTeamRoutes(
			v1,
			frameworkregistry.NewAgentTeamHandler(agentTeamService),
		); err != nil {
			return err
		}
		controlledLearningRepository, err := controlledlearning.DefaultRepository()
		if err != nil {
			return err
		}
		controlledLearningService, err := controlledlearning.NewService(
			controlledLearningRepository,
			nil,
			nil,
		)
		if err != nil {
			return err
		}
		workflowService = workflow.WithControlledLearning(
			workflowService,
			controlledLearningService,
		)
		pursuitService = pursuit.WithControlledLearning(
			pursuitService,
			controlledLearningService,
		)
		initializeControlledLearningRoutes(
			v1,
			controlledlearning.NewHandler(controlledLearningService),
		)
		lifeOpsRepository, err := lifeops.DefaultRepository()
		if err != nil {
			return err
		}
		lifeOpsService := lifeops.NewService(lifeOpsRepository)
		pursuitService = pursuit.WithPortfolioCapacity(pursuitService, lifeOpsService)
		initializeLifeOpsRoutes(v1, lifeops.NewHandler(lifeOpsService))
		lifeOntologyRepository, err := lifeontology.DefaultRepository()
		if err != nil {
			return err
		}
		lifeOntologyService := lifeontology.NewService(lifeOntologyRepository, nil)
		if err := initializeLifeOntologyRoutes(v1, lifeontology.NewHandler(lifeOntologyService)); err != nil {
			return err
		}
		lifeLedgerRepository, err := lifeledger.DefaultRepository()
		if err != nil {
			return err
		}
		lifeLedgerService, err := lifeledger.NewService(lifeLedgerRepository, nil)
		if err != nil {
			return err
		}
		lifeLedgerService, err = lifeLedgerService.WithProjection(lifeOntologyService)
		if err != nil {
			return err
		}
		if err := initializeLifeLedgerRoutes(v1, lifeledger.NewHandler(lifeLedgerService)); err != nil {
			return err
		}
		sourceService, err = source.WithLifeOntologyProjection(sourceService, lifeOntologyService)
		if err != nil {
			return err
		}
		workflowService, err = workflow.WithLifeOntologyProjection(workflowService, lifeOntologyService)
		if err != nil {
			return err
		}
		proactivityRepository, err := proactivity.DefaultRepository()
		if err != nil {
			return err
		}
		proactivityService := proactivity.NewService(proactivityRepository)
		if err := initializeProactivityRoutes(v1, proactivity.NewHandler(proactivityService)); err != nil {
			return err
		}
		workflowService, err = workflow.WithReminderDeliverySink(
			workflowService,
			workflow.NewProactivityReminderDeliverySink(proactivityService),
		)
		if err != nil {
			return err
		}
		outcomeEvaluationRepository, err := outcomeevaluation.DefaultRepository()
		if err != nil {
			return err
		}
		outcomeEvaluationService, err := outcomeevaluation.WithLifeOntologyProjection(
			outcomeevaluation.NewService(outcomeEvaluationRepository),
			lifeOntologyService,
		)
		if err != nil {
			return err
		}
		if err := initializeOutcomeEvaluationRoutes(
			v1,
			outcomeevaluation.NewHandler(outcomeEvaluationService),
		); err != nil {
			return err
		}
		ambientMonitorRepository, err := ambientmonitor.DefaultRepository()
		if err != nil {
			return err
		}
		ambientMonitorCollector, err := ambientmonitor.DefaultCollector()
		if err != nil {
			return err
		}
		ambientMonitorComposer := ambientmonitor.NewComposer(
			ambientMonitorRepository,
			outcomeEvaluationService,
			proactivityService,
		)
		ambientMonitorService := ambientmonitor.NewService(
			ambientMonitorRepository,
			ambientMonitorCollector,
			ambientMonitorComposer,
		)
		if err := initializeAmbientMonitorRoutes(
			v1,
			ambientmonitor.NewHandler(ambientMonitorService, outcomeEvaluationService),
		); err != nil {
			return err
		}
		if ambientmonitor.DurableSchedulerEnabled() {
			if err := ambientmonitor.StartDurableScheduler(appCtx, ambientMonitorService, backgroundAllowed, monitorObserver); err != nil {
				return err
			}
		}
		resilienceRepository, err := resilience.DefaultRepository()
		if err != nil {
			return err
		}
		resilienceHandler, err := resilience.NewAdvisoryAPI(resilienceRepository)
		if err != nil {
			return err
		}
		if err := initializeResilienceRoutes(v1, resilienceHandler); err != nil {
			return err
		}
		agentRepository, err := agentregistry.DefaultRepository()
		if err != nil {
			return err
		}
		agentRegistryService, err := agentregistry.NewService(agentRepository, nil)
		if err != nil {
			return err
		}
		initializeAgentRegistryRoutes(v1, agentregistry.NewHandler(agentRegistryService))
		ownerMemoryService, ok := memoryService.(memory.OwnerScopedService)
		if !ok {
			return fmt.Errorf("operational graph requires owner-scoped memory persistence")
		}
		operationalGraphService, err := operationalgraph.NewService(
			knowledgeRepository,
			knowledgeService,
			agentRegistryService,
			agentTeamService,
			workflowService,
			pursuitService,
			sourceService,
			ownerMemoryService,
			nil,
		)
		if err != nil {
			return err
		}
		if err := initializeOperationalGraphRoutes(
			v1,
			operationalgraph.NewHandler(operationalGraphService),
		); err != nil {
			return err
		}
		agentContext, err := task.NewAgentContextProvider(agentRepository)
		if err != nil {
			return err
		}
		mandateRepository, err := standingmandate.DefaultRepository()
		if err != nil {
			return err
		}
		mandateService, err := standingmandate.NewService(mandateRepository, nil)
		if err != nil {
			return err
		}
		mandateService, err = mandateService.WithLifeOntologyProjection(lifeOntologyService)
		if err != nil {
			return err
		}
		initializeStandingMandateRoutes(v1, standingmandate.NewHandler(mandateService))
		domainPackRegistry, err := domainpack.NewBuiltinRegistry()
		if err != nil {
			return err
		}
		domainPackPreferences, err := domainpack.DefaultPreferenceRepository()
		if err != nil {
			return err
		}
		domainPackHandler, err := domainpack.NewHandler(domainPackRegistry, domainPackPreferences)
		if err != nil {
			return err
		}
		initializeDomainPackRoutes(v1, domainPackHandler)
		taskStateRepository, err := task.DefaultTaskStateRepository()
		if err != nil {
			return err
		}
		frameworkEvidenceRepository, err := frameworkevidence.DefaultRepository()
		if err != nil {
			return err
		}
		constitutionPolicy, err := executionauth.NewConstitutionPolicyAdapter(
			frameworkService,
		)
		if err != nil {
			return err
		}
		taskReviewApprovalResolver, err := executionapproval.NewTaskReviewResolver(
			taskStateRepository,
		)
		if err != nil {
			return err
		}
		workflowApprovalRepository, err := approvaladapter.New(workflowRepository)
		if err != nil {
			return err
		}
		workflowApprovalResolver, err := executionapproval.NewWorkflowApprovalResolver(
			workflowApprovalRepository,
		)
		if err != nil {
			return err
		}
		controlApprovalRepository, err := opscontrol.DefaultControlApprovalRepository()
		if err != nil {
			return err
		}
		opsControlService.WithControlApprovalRepository(controlApprovalRepository)
		controlApprovalResolver, err := opscontrol.NewControlApprovalResolver(
			controlApprovalRepository,
		)
		if err != nil {
			return err
		}
		portfolioApprovalRepository, ok := pursuitRepository.(pursuit.PortfolioWorkflowEffectApprovalRepository)
		if !ok {
			return fmt.Errorf("pursuit portfolio workflow approval repository is unavailable")
		}
		portfolioApprovalResolver, err := pursuit.NewPortfolioWorkflowEffectApprovalResolver(
			portfolioApprovalRepository,
			planGraphService,
		)
		if err != nil {
			return err
		}
		approvalResolver, err := executionapproval.NewCompositeResolver(
			taskReviewApprovalResolver,
			workflowApprovalResolver,
			portfolioApprovalResolver,
			executionapproval.RegisterApprovalResolver(
				opscontrol.ControlDecisionPrefix,
				controlApprovalResolver,
			),
		)
		if err != nil {
			return err
		}
		executionAuthorizationRepository, err := executionauth.DefaultRepository()
		if err != nil {
			return err
		}
		executionAuthorizationService, err := executionauth.NewService(
			executionAuthorizationRepository,
			constitutionPolicy,
			mandateService,
			agentRegistryService,
			approvalResolver,
			nil,
		)
		if err != nil {
			return err
		}
		executionAuthorizationService, err = executionAuthorizationService.WithLifeOntologyProjection(lifeOntologyService)
		if err != nil {
			return err
		}
		frameworkSelectionResolver, err := executionauth.NewFrameworkSelectionResolver(
			frameworkService,
		)
		if err != nil {
			return err
		}
		executionAuthorizationService, err = executionAuthorizationService.WithFrameworkSelectionResolver(
			frameworkSelectionResolver,
		)
		if err != nil {
			return err
		}
		frameworkEvidenceResolver, err := executionauth.NewFrameworkEvidencePreflightResolver(
			frameworkEvidenceRepository,
		)
		if err != nil {
			return err
		}
		executionAuthorizationService, err = executionAuthorizationService.WithFrameworkEvidencePreflightResolver(
			frameworkEvidenceResolver,
		)
		if err != nil {
			return err
		}
		executionAuthorizationService, err = executionAuthorizationService.WithSourceEvidenceRepository(
			sourceEvidenceRepository,
		)
		if err != nil {
			return err
		}
		pursuitService, err = pursuit.WithPortfolioWorkflowEffectAuthorization(
			pursuitService,
			executionAuthorizationService,
		)
		if err != nil {
			return err
		}
		pursuitService, err = pursuit.WithPortfolioWorkflowEffectExecution(
			pursuitService,
			executionAuthorizationService,
		)
		if err != nil {
			return err
		}
		llmService.
			WithFinalEffectAuthorization(
				llmExecutionAuthorizer{service: executionAuthorizationService},
				nil,
			).
			WithMaintenanceEffectContext(llm.EffectContext{
				OwnerIdentity: phase2Module.OwnerUserID(),
				ActorIdentity: "hai:model-maintenance",
				ActorKind:     string(executionauth.ActorSystem),
				TaskID:        "system:model-maintenance",
				ProjectKey:    "system:model-maintenance",
			})
		initializeLLMRoutes(
			v1,
			llm.NewHandlerWithEffectContext(
				llmService,
				authenticatedLLMEffectContext,
			),
		)
		llm.StartModelMaintenanceScheduler(
			appCtx,
			llmService,
			backgroundAllowed,
		)
		initializeWASIRoutes(
			v1,
			wasiexec.NewHandler(
				wasiexec.DefaultServiceWithAuthorizer(
					executionAuthorizationService,
				),
			),
		)
		temporalService := temporalbridge.NewServiceFromEnv(
			workflowService,
			executionAuthorizationService,
		)
		temporalService.StartWorkerEventually(appCtx)
		initializeTemporalRoutes(
			v1,
			temporalbridge.NewHandler(temporalService),
		)
		destructiveSourceService, ok := sourceService.(source.DestructiveEffectService)
		if !ok {
			return fmt.Errorf(
				"connected-source destructive authorization boundary is unavailable",
			)
		}
		sourceService = destructiveSourceService.WithDestructiveEffectAuthorization(
			executionAuthorizationService,
			nil,
		)
		controlExecutionAuthorizationService :=
			executionAuthorizationService.CloneWithEmergencyStopEvaluator(
				func() executionauth.EmergencyStopEvidence {
					decision := safety.EvaluateImmutableEmergencyStop()
					return executionauth.EmergencyStopEvidence{
						Active: decision.Active,
						Source: decision.Source,
						Reason: decision.Reason,
					}
				},
			)
		opsControlService.WithExecutionAuthorizer(controlExecutionAuthorizationService)
		initializeExecutionAuthorizationRoutes(
			v1,
			executionauth.NewInspectionHandler(executionAuthorizationService),
		)
		finalEffectBridge, err := executionauth.NewFinalEffectBridge(
			executionAuthorizationRepository,
			nil,
		)
		if err != nil {
			return err
		}
		runtimeRegistry := agentruntime.DefaultRegistryWithFinalEffectVerifier(
			finalEffectBridge,
		)
		// Runtime control and automation execution share one registry so the
		// exact task that exercised a receipt can also be cancelled by its owner.
		initializeAgentRuntimeRoutes(
			v1,
			agentruntime.NewHandlerWithEcosystemMutationAuthorizer(
				runtimeRegistry,
				ecosystemExecutionAuthorizer{
					service: executionAuthorizationService,
				},
			),
		)
		approvalProofService, err := automation.DefaultDurableApprovalProofService(
			[]byte(config.AppConfig.ApprovalProofSigningKey),
		)
		if err != nil {
			return fmt.Errorf("initialize durable automation approval proofs: %w", err)
		}
		automationService := automation.NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
			automation.NewGormUserRepositoryWithNotifier(database, eventDispatcher.Notify),
			*eventPublisher,
			runtimeRegistry,
			approvalProofService,
			executionAuthorizationService,
			finalEffectBridge,
		)
		autoHandler := automation.NewHandler(automationService)
		if err := initializeAutomationsRoutes(v1, autoHandler); err != nil {
			return err
		}
		initializeEventDeliveryRoutes(v1, events.NewHandler(eventOutbox))
		taskService := task.WithControlledLearning(
			task.NewServiceWithDependenciesAndAgentContext(
				memoryService,
				llmService,
				sourceService,
				verificationService,
				task.NewAutomationToolExecutor(automationService),
				pursuitService,
				frameworkService,
				taskStateRepository,
				task.NewLifeOpsContextProvider(lifeOpsService),
				agentContext,
			),
			controlledLearningService,
		)
		taskService, err = task.WithFrameworkEvidenceRepository(
			taskService,
			frameworkEvidenceRepository,
		)
		if err != nil {
			return err
		}
		taskService, err = task.WithSourceEvidenceRepository(
			taskService,
			sourceEvidenceRepository,
		)
		if err != nil {
			return err
		}
		taskService, err = task.WithDomainPackPlanning(
			taskService,
			domainPackRegistry,
			domainPackPreferences,
		)
		if err != nil {
			return err
		}
		taskService, err = task.WithLifeOntologyContext(taskService, lifeOntologyService)
		if err != nil {
			return err
		}
		taskService, err = task.WithLifeOntologyProjection(taskService, lifeOntologyService)
		if err != nil {
			return err
		}
		taskService, err = task.WithAgentTeamContext(
			taskService,
			task.NewAgentTeamContextProvider(agentTeamService),
		)
		if err != nil {
			return err
		}
		taskService, err = task.WithAcceptedPlanResolver(taskService, planGraphService)
		if err != nil {
			return err
		}
		taskService, err = task.WithCoordinationPlanProjector(taskService, planGraphService)
		if err != nil {
			return err
		}
		planningPreview, _ := taskService.(task.PreviewService)
		a2aBridgeHandler := a2abridge.NewHandler(a2abridge.NewServiceFromEnv(planningPreview))
		initializeA2ABridgeStatusRoutes(v1, a2aBridgeHandler)
		initializeA2ABridgeRoutes(router, relativePathV1, a2aBridgeHandler)
		workflowRunner.Set(workflowtask.NewRunner(taskService, automationService))
		source.StartScheduler(appCtx, sourceService)
		workflow.StartScheduler(appCtx, workflowService)
		mcpBridgeHandler := mcpbridge.NewHandler(mcpbridge.NewServiceFromEnv(workflowService))
		initializeMCPBridgeStatusRoutes(v1, mcpBridgeHandler)
		initializeMCPAgentRoutes(router, relativePathV1, mcpBridgeHandler)
		initializeSourceRoutes(v1, source.NewHandler(sourceService, whisperService))
		initializeWorkflowRoutes(v1, workflow.NewHandlerWithPursuitIntakeRouter(workflowService, pursuitService))
		initializePursuitRoutes(v1, pursuit.NewHandler(pursuitService))
		memoryEngineSecret := config.AppConfig.MemoryEngineKey
		if strings.TrimSpace(memoryEngineSecret) == "" {
			memoryEngineSecret = config.AppConfig.BackendAPIKey
		}
		memoryEngineService := memoryengine.NewServiceWithPursuitLinker(
			memoryengine.DefaultRepository(),
			memoryService,
			workflowService,
			memoryEngineSecret,
			pursuitService,
		)
		initializeMemoryEngineRoutes(v1, memoryengine.NewHandler(memoryEngineService))
		ambientService := ambient.NewServiceWithPursuits(ambient.DefaultRepository(), workflowService, memoryEngineService, pursuitService, memoryService)
		ambient.StartScheduler(appCtx, ambientService)
		initializeAmbientRoutes(v1, ambient.NewHandler(ambientService))
		agentCycleService := agentcycle.NewServiceWithPursuits(sourceService, workflowService, ambientService, pursuitService, memoryService)
		initializeAgentCycleRoutes(v1, agentcycle.NewHandler(agentCycleService))
		initializeAssistantRoutes(v1, assistant.NewHandler(assistant.NewService(taskService, agentCycleService, pursuitService)))
		initializeAutonomyRoutes(v1, autonomy.NewHandler(autonomy.DefaultService()))
		osHandler, err := haios.DefaultHandlerWithPursuits(pursuitService)
		if err != nil {
			return err
		}
		initializeHAIOSRoutes(v1, osHandler)
		initializeTaskRoutes(v1, task.NewHandler(taskService))
		initializeModelIntelligenceRoutes(v1, modelintelligence.NewHandler(modelIntelService))
		initializeHardwareRoutes(v1, hardwareprofile.NewHandler(hardwareprofile.DefaultService()))
		privacyService := privacyfilter.DefaultService()
		initializePrivacyRoutes(v1, privacyfilter.NewHandler(privacyService))
		initializePhase2Routes(v1, phase2Module.Handler())
		runtimeLabService := runtimelab.NewService(phase2Module.Broker(), phase2Module.Service(), phase2Module.OwnerUserID(), phase2Module.WorkspaceID())
		initializeRuntimeLabRoutes(v1, runtimelab.NewHandler(runtimeLabService))
		feedRegistry := accountfeed.NewRegistry(phase2Module.Service(), privacyService, accountfeed.FetchOptions{
			FeedsRoot: phase2Module.FeedsDir(),
			AllowHTTP: strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_PHASE2_ALLOW_HTTP_FEEDS")), "true"),
		})
		seedAccountFeeds(feedRegistry, phase2Module)
		initializeAccountFeedRoutes(v1, accountfeed.NewHandler(feedRegistry, phase2Module.OwnerUserID(), phase2Module.WorkspaceID()))
		initializeOpsControlRoutes(v1, opscontrol.NewHandler(opsControlService))
		flagStore := defaultFeatureFlags()
		initializeFeatureFlagRoutes(v1, flagStore)
		diagnose := func() doctor.Report { return doctor.Diagnose(config.AppConfig) }
		initializeSystemRoutes(v1, diagnose, readinessReport, func() map[string]int {
			return map[string]int{
				"featureFlags": len(flagStore.List()),
				"languages":    len(i18n.Supported()),
			}
		})
	}
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	return nil
}

func initializeExecutionAuthorizationRoutes(
	apiVersion *gin.RouterGroup,
	handler *executionauth.InspectionHandler,
) {
	routes := apiVersion.Group("/execution-authorizations")
	routes.Use(requirePermission(rbac.PermRead))
	handler.RegisterRoutes(routes)
}

func initializeStandingMandateRoutes(apiVersion *gin.RouterGroup, handler *standingmandate.Handler) {
	routes := apiVersion.Group("/standing-mandates")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("", requirePermission(rbac.PermRead), handler.List)
		routes.POST("", requirePermission(rbac.PermAdmin), handler.Create)
		routes.GET("/decisions", requirePermission(rbac.PermRead), handler.Decisions)
		routes.GET("/:id", requirePermission(rbac.PermRead), handler.Get)
		routes.POST("/:id/activate", requirePermission(rbac.PermAdmin), handler.Activate)
		routes.POST("/:id/revoke", requirePermission(rbac.PermAdmin), handler.Revoke)
		routes.POST("/:id/authorize", requirePermission(rbac.PermExecute), handler.Authorize)
	}
}

func initializeDomainPackRoutes(apiVersion *gin.RouterGroup, handler *domainpack.Handler) {
	routes := apiVersion.Group("/domain-packs")
	routes.Use(requireAuthenticatedOwner())
	routes.Use(requireRecognizedRole())
	{
		routes.GET("", requirePermission(rbac.PermRead), handler.Catalog)
		routes.GET("/preferences", requirePermission(rbac.PermRead), handler.Preferences)
		routes.POST("/classify", requirePermission(rbac.PermRead), handler.Classify)
		routes.POST("/methods/select", requirePermission(rbac.PermRead), handler.SelectMethods)
		routes.GET("/:id", requirePermission(rbac.PermRead), handler.Detail)
		routes.GET("/:id/effective", requirePermission(rbac.PermRead), handler.Effective)
		routes.GET("/:id/playbook", requirePermission(rbac.PermRead), handler.Playbook)
		routes.PUT("/:id/preference", requirePermission(rbac.PermAdmin), handler.UpsertPreference)
	}
}

func initializeLifeOpsRoutes(apiVersion *gin.RouterGroup, handler *lifeops.Handler) {
	routes := apiVersion.Group("/life")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/domains", requirePermission(rbac.PermRead), handler.Domains)
		routes.POST("/entities/link", requirePermission(rbac.PermWrite), handler.LinkEntity)
		routes.GET("/entities/:entityType/:entityId/domains", requirePermission(rbac.PermRead), handler.EntityDomains)
		routes.POST("/needs", requirePermission(rbac.PermWrite), handler.RecordNeed)
		routes.GET("/needs", requirePermission(rbac.PermRead), handler.Needs)
		routes.POST("/capacity", requirePermission(rbac.PermWrite), handler.RecordCapacity)
		routes.GET("/capacity", requirePermission(rbac.PermRead), handler.CapacityHistory)
		routes.GET("/capacity/latest", requirePermission(rbac.PermRead), handler.LatestCapacity)
		routes.POST("/goals", requirePermission(rbac.PermWrite), handler.CreateGoal)
		routes.GET("/goals", requirePermission(rbac.PermRead), handler.Goals)
		routes.GET("/goals/forest", requirePermission(rbac.PermRead), handler.GoalForest)
		routes.GET("/goals/:id", requirePermission(rbac.PermRead), handler.Goal)
		routes.PATCH("/goals/:id", requirePermission(rbac.PermWrite), handler.UpdateGoal)
		routes.POST("/priority/assess", requirePermission(rbac.PermWrite), handler.AssessPriority)
		routes.GET("/priority/assessments", requirePermission(rbac.PermRead), handler.PriorityHistory)
	}
}

func initializeLifeOntologyRoutes(apiVersion *gin.RouterGroup, handler *lifeontology.Handler) error {
	return lifeontology.RegisterRoutes(
		apiVersion,
		handler,
		lifeontology.RouteGuards{
			AuthenticatedOwner: requireAuthenticatedOwner(),
			RecognizedRole:     requireRecognizedRole(),
			Read:               requirePermission(rbac.PermRead),
			Write:              requirePermission(rbac.PermWrite),
			Govern:             requirePermission(rbac.PermAdmin),
		},
	)
}

func initializeKnowledgeClaimRoutes(apiVersion *gin.RouterGroup, handler *knowledgegraph.ClaimHandler) error {
	return knowledgegraph.RegisterClaimRoutes(apiVersion, handler, knowledgegraph.ClaimRouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
		Approve:            requirePermission(rbac.PermApprove),
	})
}

func initializeLifeLedgerRoutes(apiVersion *gin.RouterGroup, handler *lifeledger.Handler) error {
	return lifeledger.RegisterRoutes(apiVersion, handler, lifeledger.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
	})
}

func initializeProactivityRoutes(apiVersion *gin.RouterGroup, handler *proactivity.Handler) error {
	return proactivity.RegisterRoutes(apiVersion, handler, proactivity.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
		Govern:             requirePermission(rbac.PermAdmin),
	})
}

func initializeOutcomeEvaluationRoutes(apiVersion *gin.RouterGroup, handler *outcomeevaluation.Handler) error {
	return outcomeevaluation.RegisterRoutes(apiVersion, handler, outcomeevaluation.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
		Govern:             requirePermission(rbac.PermAdmin),
	})
}

func initializeAmbientMonitorRoutes(apiVersion *gin.RouterGroup, handler *ambientmonitor.Handler) error {
	return ambientmonitor.RegisterRoutes(apiVersion, handler, ambientmonitor.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
		Govern:             requirePermission(rbac.PermAdmin),
	})
}

func initializeResilienceRoutes(apiVersion *gin.RouterGroup, handler *resilience.Handler) error {
	return resilience.RegisterRoutes(apiVersion, handler, resilience.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
		Govern:             requirePermission(rbac.PermAdmin),
	})
}

func initializeAgentRegistryRoutes(apiVersion *gin.RouterGroup, handler *agentregistry.Handler) {
	routes := apiVersion.Group("/agents")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("", requirePermission(rbac.PermRead), handler.List)
		routes.POST("", requirePermission(rbac.PermAdmin), handler.Register)
		routes.GET("/assignments/:id", requirePermission(rbac.PermRead), handler.GetAssignment)
		// Assignment changes which runtime receives delegated authority. Keep
		// that administrative until the canonical execution-authorization
		// boundary can mint policy-derived assignment requests internally.
		routes.POST("/assignments", requirePermission(rbac.PermAdmin), handler.Assign)
		routes.GET("/:id", requirePermission(rbac.PermRead), handler.Get)
		routes.PUT("/:id", requirePermission(rbac.PermAdmin), handler.Update)
		routes.GET("/:id/transitions", requirePermission(rbac.PermRead), handler.ListTransitions)
		routes.POST("/:id/transitions", requirePermission(rbac.PermAdmin), handler.Transition)
		// Outcomes mutate routing evidence and release reserved capacity. Until
		// the execution broker can attest them internally, keep this owner-only.
		routes.POST(
			"/assignments/:id/outcome",
			requirePermission(rbac.PermAdmin),
			handler.RecordAssignmentOutcome,
		)
	}
}

func initializeOperationalGraphRoutes(apiVersion *gin.RouterGroup, handler *operationalgraph.Handler) error {
	return operationalgraph.RegisterRoutes(apiVersion, handler, operationalgraph.RouteGuards{
		AuthenticatedOwner: requireAuthenticatedOwner(),
		RecognizedRole:     requireRecognizedRole(),
		Read:               requirePermission(rbac.PermRead),
		Write:              requirePermission(rbac.PermWrite),
	})
}

func initializeFrameworkRegistryRoutes(apiVersion *gin.RouterGroup, handler *frameworkregistry.Handler) {
	routes := apiVersion.Group("/framework-registry")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		routes.GET("/family-taxonomy", requirePermission(rbac.PermRead), handler.FamilyTaxonomy)
		routes.GET("/frameworks", requirePermission(rbac.PermRead), handler.List)
		routes.GET("/frameworks/:id", requirePermission(rbac.PermRead), handler.Get)
		routes.POST("/select", requirePermission(rbac.PermWrite), handler.Select)
		routes.PATCH("/frameworks/:id/preference", requirePermission(rbac.PermAdmin), handler.UpdatePreference)
		routes.GET("/selections", requirePermission(rbac.PermRead), handler.Selections)
		routes.GET("/selections/:id", requirePermission(rbac.PermRead), handler.Selection)
		routes.GET("/constitution", requirePermission(rbac.PermRead), handler.Constitution)
		routes.GET("/constitution/history", requirePermission(rbac.PermRead), handler.ConstitutionHistory)
		routes.POST("/constitution/drafts", requirePermission(rbac.PermAdmin), handler.CreateConstitutionDraft)
		routes.POST("/constitution/:id/activate", requirePermission(rbac.PermAdmin), handler.ActivateConstitution)
	}
}

func initializeAgentTeamRoutes(apiVersion *gin.RouterGroup, handler *frameworkregistry.AgentTeamHandler) error {
	frameworkRoutes := apiVersion.Group("/framework-registry")
	return frameworkregistry.RegisterAgentTeamRoutes(
		frameworkRoutes,
		handler,
		frameworkregistry.AgentTeamRouteGuards{
			AuthenticatedOwner: requireAuthenticatedOwner(),
			RecognizedRole:     requireRecognizedRole(),
			Read:               requirePermission(rbac.PermRead),
			Write:              requirePermission(rbac.PermWrite),
			Govern:             requirePermission(rbac.PermAdmin),
		},
	)
}

func initializeControlledLearningRoutes(
	apiVersion *gin.RouterGroup,
	handler *controlledlearning.Handler,
) {
	routes := apiVersion.Group("/controlled-learning")
	routes.Use(requireAuthenticatedOwner())
	routes.Use(requireRecognizedRole())
	{
		routes.GET("/outcomes", requirePermission(rbac.PermRead), handler.ListOutcomes)
		routes.POST("/outcomes", requirePermission(rbac.PermApprove), handler.RecordOutcome)
		routes.GET("/outcomes/:id", requirePermission(rbac.PermRead), handler.GetOutcome)
		routes.GET("/proposals", requirePermission(rbac.PermRead), handler.ListProposals)
		routes.POST("/proposals", requirePermission(rbac.PermWrite), handler.Propose)
		routes.GET("/proposals/:id", requirePermission(rbac.PermRead), handler.GetProposal)
		routes.GET(
			"/proposals/:id/decisions",
			requirePermission(rbac.PermRead),
			handler.ListDecisions,
		)
		routes.POST(
			"/proposals/:id/decisions",
			requirePermission(rbac.PermApprove),
			handler.Decide,
		)
		routes.GET(
			"/proposals/:id/decisions/:decisionId",
			requirePermission(rbac.PermRead),
			handler.GetDecision,
		)
		routes.GET(
			"/applications",
			requirePermission(rbac.PermRead),
			handler.ListApplications,
		)
		routes.GET(
			"/applications/:id",
			requirePermission(rbac.PermRead),
			handler.GetApplication,
		)
		routes.GET(
			"/applications/:id/events",
			requirePermission(rbac.PermRead),
			handler.ListApplicationEvents,
		)
		routes.POST(
			"/applications/:id/rollback",
			requirePermission(rbac.PermAdmin),
			handler.RollbackApplication,
		)
	}
}

func initializeAssistantRoutes(apiVersion *gin.RouterGroup, handler *assistant.Handler) {
	routes := apiVersion.Group("/assistant")
	routes.Use(assistant.RequireAuthenticatedOwner())
	{
		// A chat command may request execution, so it needs the same approval
		// capability as the direct task-run endpoint.
		routes.POST("/command", requirePermission(rbac.PermApprove), handler.Command)
		routes.GET("/logs", requirePermission(rbac.PermRead), handler.Logs)
	}
}

func initializeAgentCycleRoutes(apiVersion *gin.RouterGroup, handler *agentcycle.Handler) {
	routes := apiVersion.Group("/agent-cycle")
	routes.Use(requireAuthenticatedOwner())
	{
		// HTTP calls run the owner-scoped read/brief path only; system work stays
		// with the background worker.
		routes.POST("/run", requirePermission(rbac.PermRead), handler.Run)
	}
}

func initializeAutonomyRoutes(apiVersion *gin.RouterGroup, handler *autonomy.Handler) {
	routes := apiVersion.Group("/autonomy")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		routes.POST("/stress", requirePermission(rbac.PermWrite), handler.Stress)
	}
}

func initializeAmbientRoutes(apiVersion *gin.RouterGroup, handler *ambient.Handler) {
	routes := apiVersion.Group("/ambient")
	routes.Use(ambient.RequireAuthenticatedOwner())
	{
		routes.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		routes.POST("/scan", requirePermission(rbac.PermWrite), handler.Scan)
		routes.PATCH("/needs/:key", requirePermission(rbac.PermWrite), handler.UpdateNeed)
		routes.POST("/opportunities/:id/accept", requirePermission(rbac.PermWrite), handler.Accept)
		routes.POST("/opportunities/:id/dismiss", requirePermission(rbac.PermWrite), handler.Dismiss)
	}
}

func initializeAgentRuntimeRoutes(apiVersion *gin.RouterGroup, handler *agentruntime.Handler) {
	routes := apiVersion.Group("/agent-runtimes")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/", requirePermission(rbac.PermRead), handler.Registry)
		routes.GET("/health", requirePermission(rbac.PermRead), handler.Health)
		routes.GET("/:id/skills", requirePermission(rbac.PermRead), handler.Skills)
		routes.POST("/:id/tasks/:taskId/stop", requirePermission(rbac.PermExecute), handler.StopTask)
		routes.GET("/openclaw/ecosystem", requirePermission(rbac.PermRead), handler.OpenClawEcosystem)
		routes.PATCH("/openclaw/ecosystem", requirePermission(rbac.PermAdmin), handler.SetOpenClawEcosystem)
		routes.POST("/openclaw/ecosystem/refresh", requirePermission(rbac.PermAdmin), handler.RefreshOpenClawEcosystem)
		routes.POST("/openclaw/ecosystem/upload", requirePermission(rbac.PermAdmin), handler.UploadOpenClawEcosystem)
	}
}

func initializeBrainCatalogRoutes(apiVersion *gin.RouterGroup, handler *braincatalog.Handler) {
	routes := apiVersion.Group("/brain-catalog")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/", requirePermission(rbac.PermRead), handler.List)
		routes.GET("/adoption-plan", requirePermission(rbac.PermRead), handler.AdoptionPlan)
		routes.GET("/revalidation-history", requirePermission(rbac.PermRead), handler.RevalidationHistory)
		routes.GET("/collection-revalidation-history", requirePermission(rbac.PermRead), handler.CollectionRevalidationHistory)
		routes.GET("/repository-discovery-revalidation-history", requirePermission(rbac.PermRead), handler.RepositoryDiscoveryRevalidationHistory)
		routes.POST("/revalidation/run", requirePermission(rbac.PermAdmin), handler.RunDueRevalidations)
		routes.POST("/collection-revalidation/run", requirePermission(rbac.PermAdmin), handler.RunDueCollectionRevalidation)
		routes.POST("/repository-discovery-revalidation/run", requirePermission(rbac.PermAdmin), handler.RunDueRepositoryDiscoveryRevalidation)
		routes.POST("/ossinsight/revalidate", requirePermission(rbac.PermAdmin), handler.RevalidateCollections)
		routes.POST("/ossinsight/discover", requirePermission(rbac.PermAdmin), handler.DiscoverRepositories)
		routes.POST("/ossinsight/discover/reviewable", requirePermission(rbac.PermAdmin), handler.DiscoverReviewableRepositories)
		routes.POST("/ossinsight/discoveries/revalidate", requirePermission(rbac.PermAdmin), handler.RevalidateDiscovery)
		routes.POST("/recommend", requirePermission(rbac.PermRead), handler.RecommendCapabilities)
		routes.GET("/:id", requirePermission(rbac.PermRead), handler.Get)
		routes.POST("/:id/revalidate", requirePermission(rbac.PermAdmin), handler.Revalidate)
	}
}

func localCaptureCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		allowed := strings.HasPrefix(origin, "chrome-extension://") ||
			strings.HasPrefix(origin, "moz-extension://") ||
			strings.HasPrefix(origin, "http://localhost:") ||
			strings.HasPrefix(origin, "http://127.0.0.1:")
		if origin != "" && allowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-HAI-Backend-Key")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			if !allowed {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

const backendAPIKeyHeader = "X-HAI-Backend-Key"

func backendAPIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(config.AppConfig.BackendAPIKey)
		if expected == "" || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		provided := strings.TrimSpace(c.GetHeader(backendAPIKeyHeader))
		providedHash := sha256.Sum256([]byte(provided))
		expectedHash := sha256.Sum256([]byte(expected))
		if subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "backend API key required"})
			return
		}

		c.Next()
	}
}

func initializeAutomationsRoutes(apiVersion *gin.RouterGroup, autoHandler *automation.Handler) error {
	automations := apiVersion.Group("/automation")
	automations.Use(automation.RequireAuthenticatedOperator())
	{
		automations.PATCH("/swap/:id1/:id2", requirePermission(rbac.PermAdmin), autoHandler.SwapPosition)
		automations.GET("/", requirePermission(rbac.PermRead), autoHandler.GetAll)
		automations.GET("/health/summary", requirePermission(rbac.PermRead), autoHandler.HealthSummary)
		automations.GET("/health-summary", requirePermission(rbac.PermRead), autoHandler.HealthSummary)
		automations.GET("/images/:imageName", requirePermission(rbac.PermRead), autoHandler.ImageHandler)
		automations.GET("/:id", requirePermission(rbac.PermRead), autoHandler.GetByID)
		automations.POST("/:id/launch", requirePermission(rbac.PermExecute), autoHandler.Launch)
		automations.POST("/:id/stop-runtime", requirePermission(rbac.PermExecute), autoHandler.StopRuntimeTask)
		automations.POST("/:id/health-check", requirePermission(rbac.PermWrite), autoHandler.RunHealthCheck)
		automations.GET("/:id/diagnostics", requirePermission(rbac.PermRead), autoHandler.Diagnostics)
		automations.POST("/", requirePermission(rbac.PermAdmin), autoHandler.Create)
		automations.PATCH("/", requirePermission(rbac.PermAdmin), autoHandler.Update)
		automations.DELETE("/:id", requirePermission(rbac.PermAdmin), autoHandler.DeleteByID)
	}

	return nil
}

func initializeEventDeliveryRoutes(apiVersion *gin.RouterGroup, handler *events.Handler) {
	routes := apiVersion.Group("/event-delivery")
	routes.Use(requireAuthenticatedOwner())
	routes.GET("/", requirePermission(rbac.PermRead), handler.Stats)
	routes.POST("/:id/retry", requirePermission(rbac.PermAdmin), handler.Retry)
}

func initializeLLMRoutes(apiVersion *gin.RouterGroup, llmHandler *llm.Handler) {
	llmRoutes := apiVersion.Group("/llm")
	llmRoutes.Use(requireAuthenticatedOwner())
	{
		llmRoutes.GET("/policy", requirePermission(rbac.PermRead), llmHandler.Policy)
		llmRoutes.GET("/probes", requirePermission(rbac.PermRead), llmHandler.ProviderProbes)
		llmRoutes.GET("/probes/history", requirePermission(rbac.PermRead), llmHandler.ProviderProbeHistory)
		llmRoutes.GET("/model-maintenance", requirePermission(rbac.PermRead), llmHandler.ModelMaintenanceHistory)
		llmRoutes.POST("/model-maintenance/run", requirePermission(rbac.PermAdmin), llmHandler.RunDueModelMaintenance)
		llmRoutes.GET("/generations", requirePermission(rbac.PermRead), llmHandler.GenerationHistory)
		llmRoutes.POST("/route", requirePermission(rbac.PermWrite), llmHandler.Route)
		llmRoutes.POST("/generate", requirePermission(rbac.PermApprove), llmHandler.Generate)
		llmRoutes.GET("/logs", requirePermission(rbac.PermRead), llmHandler.Logs)
	}
}

func initializeMemoryRoutes(apiVersion *gin.RouterGroup, memoryHandler *memory.Handler) {
	memoryRoutes := apiVersion.Group("/memory")
	memoryRoutes.Use(requireAuthenticatedOwner())
	{
		memoryRoutes.GET("/", requirePermission(rbac.PermRead), memoryHandler.List)
		memoryRoutes.GET("/query", requirePermission(rbac.PermRead), memoryHandler.Query)
		memoryRoutes.GET("/health", requirePermission(rbac.PermRead), memoryHandler.Health)
		memoryRoutes.POST("/", requirePermission(rbac.PermWrite), memoryHandler.Create)
		memoryRoutes.POST("/retrieve", requirePermission(rbac.PermRead), memoryHandler.Retrieve)
		memoryRoutes.POST("/semantic/reindex", requirePermission(rbac.PermWrite), memoryHandler.ReindexSemantic)
		memoryRoutes.GET("/export", requirePermission(rbac.PermRead), memoryHandler.Export)
		memoryRoutes.GET("/:id", requirePermission(rbac.PermRead), memoryHandler.Get)
		memoryRoutes.PATCH("/:id", requirePermission(rbac.PermWrite), memoryHandler.Update)
		memoryRoutes.POST("/:id/archive", requirePermission(rbac.PermWrite), memoryHandler.Archive)
		memoryRoutes.POST("/:id/restore", requirePermission(rbac.PermWrite), memoryHandler.Restore)
		memoryRoutes.DELETE("/:id", requirePermission(rbac.PermWrite), memoryHandler.Delete)
	}
}

func initializeMemoryEngineRoutes(apiVersion *gin.RouterGroup, handler *memoryengine.Handler) {
	routes := apiVersion.Group("/memory-engine")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.POST("/import", requirePermission(rbac.PermWrite), handler.Import)
		routes.GET("/dashboard", requirePermission(rbac.PermRead), handler.Dashboard)
		routes.POST("/search", requirePermission(rbac.PermRead), handler.Search)
		routes.GET("/conversations", requirePermission(rbac.PermRead), handler.Conversations)
		routes.GET("/conversations/:id", requirePermission(rbac.PermRead), handler.Conversation)
		routes.DELETE("/conversations/:id", requirePermission(rbac.PermWrite), handler.DeleteConversation)
		routes.GET("/insights", requirePermission(rbac.PermRead), handler.Insights)
	}
}

func initializeSourceRoutes(apiVersion *gin.RouterGroup, sourceHandler *source.Handler) {
	sourceRoutes := apiVersion.Group("/sources")
	sourceRoutes.Use(requireAuthenticatedOwner())
	{
		sourceRoutes.GET("/connectors", requirePermission(rbac.PermRead), sourceHandler.Connectors)
		sourceRoutes.GET("/", requirePermission(rbac.PermRead), sourceHandler.Sources)
		sourceRoutes.GET("/overview", requirePermission(rbac.PermRead), sourceHandler.Overview)
		sourceRoutes.POST("/", requirePermission(rbac.PermWrite), sourceHandler.CreateSource)
		sourceRoutes.POST("/search", requirePermission(rbac.PermRead), sourceHandler.Search)
		// The HTTP handler scopes this batch to the authenticated owner. The
		// separate in-process scheduler is the only global source worker.
		sourceRoutes.POST("/sync-due", requirePermission(rbac.PermWrite), sourceHandler.RunDueScheduledSyncs)
		sourceRoutes.GET("/sync-jobs", requirePermission(rbac.PermRead), sourceHandler.SyncJobs)
		sourceRoutes.GET("/extractions", requirePermission(rbac.PermRead), sourceHandler.Extractions)
		sourceRoutes.GET("/audit-logs", requirePermission(rbac.PermRead), sourceHandler.AuditLogs)
		sourceRoutes.GET("/:id/health", requirePermission(rbac.PermRead), sourceHandler.ConnectionHealth)
		sourceRoutes.PATCH("/extractions/:id", requirePermission(rbac.PermWrite), sourceHandler.UpdateExtraction)
		sourceRoutes.POST("/extractions/:id/archive", requirePermission(rbac.PermWrite), sourceHandler.ArchiveExtraction)
		sourceRoutes.DELETE("/extractions/:id", requirePermission(rbac.PermWrite), sourceHandler.DeleteExtraction)
		sourceRoutes.PATCH("/:id", requirePermission(rbac.PermWrite), sourceHandler.UpdateSource)
		sourceRoutes.POST("/:id/sync", requirePermission(rbac.PermWrite), sourceHandler.Sync)
		sourceRoutes.POST("/:id/transcribe", requirePermission(rbac.PermWrite), sourceHandler.Transcribe)
		sourceRoutes.POST("/:id/reindex", requirePermission(rbac.PermWrite), sourceHandler.Reindex)
		sourceRoutes.POST("/:id/pause", requirePermission(rbac.PermWrite), sourceHandler.Pause)
		sourceRoutes.POST("/:id/resume", requirePermission(rbac.PermWrite), sourceHandler.Resume)
		sourceRoutes.POST("/:id/revoke", requirePermission(rbac.PermWrite), sourceHandler.Revoke)
	}

	// Google OAuth for Google-backed connectors. Both start and callback require
	// a verified HAI role. The callback additionally consumes HMAC-signed,
	// expiring state that is durably bound to the initiating source owner.
	sourceOAuth := apiVersion.Group("/sources")
	{
		sourceOAuth.GET("/oauth/google/start", requirePermission(rbac.PermWrite), sourceHandler.StartGoogleOAuth)
		sourceOAuth.GET("/oauth/google/callback", requirePermission(rbac.PermRead), sourceHandler.GoogleOAuthCallback)
	}
}

func initializeWhisperCPPRoutes(apiVersion *gin.RouterGroup, handler *whispercpp.Handler) {
	routes := apiVersion.Group("/whispercpp")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermWrite), handler.Probe)
	}
}

func initializeDoclingRoutes(apiVersion *gin.RouterGroup, handler *docling.Handler) {
	routes := apiVersion.Group("/docling")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
	}
}

func initializeTaskRoutes(apiVersion *gin.RouterGroup, taskHandler *task.Handler) {
	taskRoutes := apiVersion.Group("/task")
	taskRoutes.Use(requireAuthenticatedOwner())
	{
		taskRoutes.POST("/plan", requirePermission(rbac.PermWrite), taskHandler.Plan)
		taskRoutes.POST("/run", requirePermission(rbac.PermApprove), taskHandler.Run)
		taskRoutes.POST("/success", requirePermission(rbac.PermApprove), taskHandler.Run)
		taskRoutes.GET("/logs", requirePermission(rbac.PermRead), taskHandler.Logs)
		taskRoutes.GET("/review-queue", requirePermission(rbac.PermRead), taskHandler.ReviewQueue)
		taskRoutes.POST("/review-queue/:id/resolve", requirePermission(rbac.PermApprove), taskHandler.ResolveReviewItem)
		taskRoutes.POST("/review-queue/reconcile", requirePermission(rbac.PermAdmin), taskHandler.ReconcileApprovedReviews)
	}
}

func initializeVerificationRoutes(apiVersion *gin.RouterGroup, verificationHandler *verification.Handler) {
	verificationRoutes := apiVersion.Group("/verification")
	verificationRoutes.Use(requireAuthenticatedOwner())
	{
		verificationRoutes.POST("/answer", requirePermission(rbac.PermWrite), verificationHandler.Answer)
		verificationRoutes.GET("/runs", requirePermission(rbac.PermRead), verificationHandler.Runs)
		verificationRoutes.GET("/runs/:id", requirePermission(rbac.PermRead), verificationHandler.RunDetails)
	}
}

func initializeResearchRoutes(apiVersion *gin.RouterGroup, handler *research.Handler) {
	routes := apiVersion.Group("/research")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/search", requirePermission(rbac.PermWrite), handler.Search)
	}
}

func initializeRAGFlowRoutes(apiVersion *gin.RouterGroup, handler *ragflow.Handler) {
	routes := apiVersion.Group("/ragflow")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/retrieve", requirePermission(rbac.PermWrite), handler.Retrieve)
	}
}

// initializeMiniSWERoutes exposes only a diff-only patch proposal path. The
// service independently requires an owner-scoped, approved, ready workflow and
// an allowlisted disposable source snapshot before it contacts the runner.
func initializeMiniSWERoutes(apiVersion *gin.RouterGroup, handler *miniswe.Handler) {
	routes := apiVersion.Group("/mini-swe")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.GET("/jobs", requirePermission(rbac.PermRead), handler.Jobs)
		routes.POST("/workflows/:id/propose-patch", requirePermission(rbac.PermApprove), handler.ProposePatch)
	}
}

func initializeAnythingLLMRoutes(apiVersion *gin.RouterGroup, handler *anythingllm.Handler) {
	routes := apiVersion.Group("/anythingllm")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/retrieve", requirePermission(rbac.PermWrite), handler.Retrieve)
	}
}

// initializeSerenaRoutes exposes one bounded read-only semantic lookup. The
// service never launches Serena or forwards a generic MCP call surface.
func initializeSerenaRoutes(apiVersion *gin.RouterGroup, handler *serena.Handler) {
	routes := apiVersion.Group("/serena")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/symbols", requirePermission(rbac.PermWrite), handler.FindSymbols)
	}
}

func initializePresidioRoutes(apiVersion *gin.RouterGroup, handler *presidio.Handler) {
	routes := apiVersion.Group("/presidio")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		// Text leaves the request process only for the explicitly configured local
		// analyzer; analysis has no persistence or external-action capability.
		routes.POST("/analyze", requirePermission(rbac.PermWrite), handler.Analyze)
	}
}

func initializeEvidentlyRoutes(apiVersion *gin.RouterGroup, handler *evidently.Handler) {
	routes := apiVersion.Group("/evidently")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/evaluate", requirePermission(rbac.PermWrite), handler.Evaluate)
	}
}

func initializeGuardrailsRoutes(apiVersion *gin.RouterGroup, handler *guardrails.Handler) {
	routes := apiVersion.Group("/guardrails")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/validate", requirePermission(rbac.PermWrite), handler.Validate)
	}
}

func initializeLMEvalRoutes(apiVersion *gin.RouterGroup, handler *lmeval.Handler) {
	routes := apiVersion.Group("/lm-eval")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

func initializePromptfooRoutes(apiVersion *gin.RouterGroup, handler *promptfoo.Handler) {
	routes := apiVersion.Group("/promptfoo")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

func initializeDeepEvalRoutes(apiVersion *gin.RouterGroup, handler *deepeval.Handler) {
	routes := apiVersion.Group("/deepeval")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

func initializeDeepTeamRoutes(apiVersion *gin.RouterGroup, handler *deepteam.Handler) {
	routes := apiVersion.Group("/deepteam")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

func initializeGarakRoutes(apiVersion *gin.RouterGroup, handler *garak.Handler) {
	routes := apiVersion.Group("/garak")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

// The security runners accept only a configured snapshot identifier and return
// redacted aggregate evidence. Owner-admin permission prevents operators from
// probing or scanning arbitrary configured snapshots.
func initializeGitleaksRoutes(apiVersion *gin.RouterGroup, handler *gitleaks.Handler) {
	routes := apiVersion.Group("/gitleaks")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/scan", requirePermission(rbac.PermAdmin), handler.Scan)
	}
}

func initializeGosecRoutes(apiVersion *gin.RouterGroup, handler *gosec.Handler) {
	routes := apiVersion.Group("/gosec")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/scan", requirePermission(rbac.PermAdmin), handler.Scan)
	}
}

func initializeGrypeRoutes(apiVersion *gin.RouterGroup, handler *grype.Handler) {
	routes := apiVersion.Group("/grype")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/scan", requirePermission(rbac.PermAdmin), handler.Scan)
	}
}

func initializeTrivyRoutes(apiVersion *gin.RouterGroup, handler *trivy.Handler) {
	routes := apiVersion.Group("/trivy")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/scan", requirePermission(rbac.PermAdmin), handler.Scan)
	}
}

func initializeSyftRoutes(apiVersion *gin.RouterGroup, handler *syft.Handler) {
	routes := apiVersion.Group("/syft")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/inventory", requirePermission(rbac.PermAdmin), handler.Inventory)
	}
}

func initializeLangfuseRoutes(apiVersion *gin.RouterGroup, handler *langfuse.Handler) {
	routes := apiVersion.Group("/langfuse")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/export/operational-snapshot", requirePermission(rbac.PermAdmin), handler.ExportOperationalSnapshot)
	}
}

// OpenLIT receives one fixed aggregate operational snapshot. The caller cannot
// provide traces, sources, model content, credentials, or exporter settings.
func initializeOpenLITRoutes(apiVersion *gin.RouterGroup, handler *openlit.Handler) {
	routes := apiVersion.Group("/openlit")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/export/operational-snapshot", requirePermission(rbac.PermAdmin), handler.ExportOperationalSnapshot)
	}
}

// MLflow is a fixed local evaluation-evidence view. Experiment and metric
// allowlists come from server configuration and cannot be changed by clients.
func initializeMLflowRoutes(apiVersion *gin.RouterGroup, handler *mlflow.Handler) {
	routes := apiVersion.Group("/mlflow")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.GET("/runs", requirePermission(rbac.PermRead), handler.RecentRuns)
	}
}

func initializePhase2Routes(apiVersion *gin.RouterGroup, handler *phase2.Handler) {
	ops := apiVersion.Group("/operations")
	ops.Use(requireAuthenticatedOwner())
	{
		ops.GET("", requirePermission(rbac.PermRead), handler.ListOperations)
		ops.GET("/dashboard", requirePermission(rbac.PermRead), handler.Dashboard)
		ops.GET("/:id", requirePermission(rbac.PermRead), handler.GetOperation)
		ops.GET("/:id/events", requirePermission(rbac.PermRead), handler.OperationEvents)
		ops.GET("/:id/approvals", requirePermission(rbac.PermRead), handler.Approvals)
		ops.POST("/:id/approve", requirePermission(rbac.PermApprove), handler.Approve)
		ops.POST("/:id/reject", requirePermission(rbac.PermApprove), handler.Reject)
		ops.POST("/:id/later", requirePermission(rbac.PermWrite), handler.Later)
		ops.POST("/:id/block-similar", requirePermission(rbac.PermApprove), handler.BlockSimilar)
		ops.POST("/:id/run", requirePermission(rbac.PermExecute), handler.RunOperation)
		ops.POST("/:id/evidence-pack", requirePermission(rbac.PermWrite), handler.GenerateEvidencePack)
	}
	evidencePacks := apiVersion.Group("/evidence-packs")
	evidencePacks.Use(requireAuthenticatedOwner())
	{
		evidencePacks.GET("/:id", requirePermission(rbac.PermRead), handler.GetEvidencePack)
	}
	backgroundRuns := apiVersion.Group("/background")
	backgroundRuns.Use(requireAuthenticatedOwner())
	{
		backgroundRuns.POST("/run", requirePermission(rbac.PermExecute), handler.RunBackground)
	}
}

func initializeAccountFeedRoutes(apiVersion *gin.RouterGroup, handler *accountfeed.Handler) {
	af := apiVersion.Group("/account-feeds")
	af.Use(requireAuthenticatedOwner())
	{
		af.GET("", requirePermission(rbac.PermRead), handler.List)
		af.POST("", requirePermission(rbac.PermAdmin), handler.Create)
		af.GET("/bridges", requirePermission(rbac.PermRead), handler.Bridges)
		af.GET("/permissions", requirePermission(rbac.PermRead), handler.Permissions)
		af.POST("/sync-due", requirePermission(rbac.PermWrite), handler.SyncDue)
		af.GET("/:id", requirePermission(rbac.PermRead), handler.Get)
		af.PATCH("/:id", requirePermission(rbac.PermAdmin), handler.Patch)
		af.POST("/:id/sync", requirePermission(rbac.PermWrite), handler.Sync)
		af.GET("/:id/audit", requirePermission(rbac.PermRead), handler.Audit)
	}
}

// seedAccountFeeds registers the module's configured local feed files so they
// appear in the Account Feeds API and can be synced on demand.
func seedAccountFeeds(reg *accountfeed.Registry, m *phase2.Module) {
	for _, name := range m.FeedFiles() {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		_, _ = reg.Register(accountfeed.Feed{
			Name:         strings.TrimSuffix(name, ".json"),
			Provider:     string(accountfeed.ProviderGenericJSONFeed),
			AccountLabel: name,
			SourceType:   accountfeed.SourceLocalJSONFile,
			Path:         name,
			OwnerUserID:  m.OwnerUserID(),
			WorkspaceID:  m.WorkspaceID(),
			Enabled:      true,
		})
	}
}

func initializeModelIntelligenceRoutes(apiVersion *gin.RouterGroup, handler *modelintelligence.Handler) {
	mi := apiVersion.Group("/model-intelligence")
	mi.Use(requireAuthenticatedOwner())
	{
		mi.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		mi.GET("/profiles", requirePermission(rbac.PermRead), handler.Profiles)
		mi.GET("/profiles/:providerId/:modelId", requirePermission(rbac.PermRead), handler.Profile)
		mi.POST("/profiles/:providerId/:modelId/benchmark", requirePermission(rbac.PermExecute), handler.Benchmark)
		mi.GET("/benchmarks", requirePermission(rbac.PermRead), handler.Benchmarks)
		mi.GET("/telemetry", requirePermission(rbac.PermRead), handler.Telemetry)
		mi.GET("/calibration", requirePermission(rbac.PermRead), handler.Calibration)
		mi.GET("/lane-winners", requirePermission(rbac.PermRead), handler.LaneWinners)
		mi.GET("/cache", requirePermission(rbac.PermRead), handler.Cache)
		mi.DELETE("/cache/:id", requirePermission(rbac.PermWrite), handler.DeleteCache)
		mi.GET("/token-budgets", requirePermission(rbac.PermRead), handler.TokenBudgets)
		mi.PATCH("/token-budgets", requirePermission(rbac.PermAdmin), handler.UpdateTokenBudgets)
	}
}

func initializeHardwareRoutes(apiVersion *gin.RouterGroup, handler *hardwareprofile.Handler) {
	hw := apiVersion.Group("/hardware")
	hw.Use(requireAuthenticatedOwner())
	{
		hw.GET("/profile", requirePermission(rbac.PermRead), handler.Profile)
		hw.POST("/detect", requirePermission(rbac.PermWrite), handler.Detect)
		hw.PATCH("/profile", requirePermission(rbac.PermAdmin), handler.Patch)
	}
	power := apiVersion.Group("/power")
	power.Use(requireAuthenticatedOwner())
	{
		power.GET("/policy", requirePermission(rbac.PermRead), handler.PowerPolicy)
		power.PATCH("/policy", requirePermission(rbac.PermAdmin), handler.UpdatePowerPolicy)
	}
}

func initializePrivacyRoutes(apiVersion *gin.RouterGroup, handler *privacyfilter.Handler) {
	privacy := apiVersion.Group("/privacy")
	privacy.Use(requireAuthenticatedOwner())
	{
		privacy.POST("/scan", requirePermission(rbac.PermWrite), handler.ScanContent)
		privacy.GET("/scans", requirePermission(rbac.PermRead), handler.Scans)
		privacy.GET("/scans/:id", requirePermission(rbac.PermRead), handler.ScanByID)
	}
}

func initializeOpsControlRoutes(apiVersion *gin.RouterGroup, handler *opscontrol.Handler) {
	bg := apiVersion.Group("/background")
	bg.Use(requireAuthenticatedOwner())
	{
		bg.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		// Operators may always halt work. Only an owner may resume it or change
		// the autonomy mode.
		bg.POST("/pause", requirePermission(rbac.PermExecute), handler.Pause)
		bg.POST("/control-approvals", requirePermission(rbac.PermAdmin), handler.PrepareControlApproval)
		bg.POST("/control-approvals/:id/decision", requirePermission(rbac.PermAdmin), handler.DecideControlApproval)
		bg.POST("/resume", requirePermission(rbac.PermAdmin), handler.Resume)
		bg.PATCH("/mode", requirePermission(rbac.PermAdmin), handler.SetMode)
	}
	wr := apiVersion.Group("/windows-runtime")
	wr.Use(requireAuthenticatedOwner())
	{
		wr.GET("/readiness", requirePermission(rbac.PermRead), handler.Readiness)
		wr.POST("/recovery", requirePermission(rbac.PermExecute), handler.Recovery)
		wr.POST("/emergency-stop/verify", requirePermission(rbac.PermExecute), handler.VerifyEmergencyStop)
	}
}

func initializeRuntimeLabRoutes(apiVersion *gin.RouterGroup, handler *runtimelab.Handler) {
	rl := apiVersion.Group("/runtime-lab")
	rl.Use(requireAuthenticatedOwner())
	{
		rl.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		rl.GET("/feature-parity", requirePermission(rbac.PermRead), handler.FeatureParity)
		rl.GET("/capabilities", requirePermission(rbac.PermRead), handler.Capabilities)
		rl.GET("/:runtimeId/feature-parity", requirePermission(rbac.PermRead), handler.RuntimeFeatureParity)
		rl.POST("/:runtimeId/probe", requirePermission(rbac.PermWrite), handler.Probe)
		rl.POST("/:runtimeId/self-test", requirePermission(rbac.PermExecute), handler.SelfTest)
		rl.GET("/:runtimeId/attempts", requirePermission(rbac.PermRead), handler.Attempts)
	}
}

func initializeMCPPreflightRoutes(apiVersion *gin.RouterGroup, handler *mcppreflight.Handler) {
	routes := apiVersion.Group("/mcp-preflight")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/overview", requirePermission(rbac.PermRead), handler.Overview)
		routes.POST("/:serverId/run", requirePermission(rbac.PermAdmin), handler.Run)
	}
}

// initializeA2ABridgeStatusRoutes stays inside HAI's normal authenticated
// owner API. It exposes configuration only, never peer tokens or task input.
func initializeA2ABridgeStatusRoutes(apiVersion *gin.RouterGroup, handler *a2abridge.Handler) {
	routes := apiVersion.Group("/a2a")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
	}
}

// AutoGen compatibility is a transient, non-executing migration review
// surface. It cannot import events or start an AutoGen/Agent Framework runtime.
func initializeAutoGenCompatibilityRoutes(apiVersion *gin.RouterGroup, handler *autogencompat.Handler) {
	routes := apiVersion.Group("/autogen-compat")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/preview", requirePermission(rbac.PermWrite), handler.Preview)
		routes.POST("/migration-plan", requirePermission(rbac.PermWrite), handler.MigrationPlan)
	}
}

// initializeA2ABridgeRoutes implements a small A2A compatibility boundary.
// It is local by default and can use only the explicitly governed fixed-ngrok
// mode. The Agent Card carries no user context, and the JSON-RPC endpoint uses
// a separate bridge token rather than browser identity or API keys.
func initializeA2ABridgeRoutes(router *gin.Engine, relativePathV1 string, handler *a2abridge.Handler) {
	router.GET("/.well-known/agent-card.json", handler.AgentCard)
	router.POST(relativePathV1+"/a2a", handler.Send)
}

// initializeMCPBridgeStatusRoutes is part of the normal owner-authenticated
// API. It reports configuration only; the data surface below uses a separate
// narrow bridge token for the local FastMCP process.
func initializeMCPBridgeStatusRoutes(apiVersion *gin.RouterGroup, handler *mcpbridge.Handler) {
	routes := apiVersion.Group("/mcp-bridge")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
	}
}

// initializeMCPAgentRoutes intentionally avoids the browser identity and
// backend API-key middleware. It is reachable only with the dedicated bridge
// token, serves aggregate/sanitized read models, and is not a general API.
func initializeMCPAgentRoutes(router *gin.Engine, relativePathV1 string, handler *mcpbridge.Handler) {
	routes := router.Group(relativePathV1 + "/mcp-agent")
	{
		routes.GET("/overview", handler.Overview)
		routes.GET("/actionable", handler.Actionable)
	}
}

func initializePlanningOptimizerRoutes(apiVersion *gin.RouterGroup, handler *planningoptimizer.Handler) {
	routes := apiVersion.Group("/planning-optimizer")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.GET("/runs", requirePermission(rbac.PermRead), handler.Runs)
		routes.POST("/proposals", requirePermission(rbac.PermWrite), handler.Propose)
	}
}

func initializePlanGraphRoutes(apiVersion *gin.RouterGroup, handler *plangraph.Handler) {
	routes := apiVersion.Group("/plans")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("", requirePermission(rbac.PermRead), handler.List)
		routes.POST("/preview", requirePermission(rbac.PermWrite), handler.Preview)
		routes.GET("/:id", requirePermission(rbac.PermRead), handler.Get)
		routes.POST("/:id/replan", requirePermission(rbac.PermWrite), handler.Replan)
		routes.POST("/:id/accept", requirePermission(rbac.PermApprove), handler.Accept)
	}
}

func initializePydanticAIRoutes(apiVersion *gin.RouterGroup, handler *pydanticai.Handler) {
	routes := apiVersion.Group("/pydantic-ai")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/proposals", requirePermission(rbac.PermWrite), handler.Propose)
	}
}

// These planners receive only one bounded request and measurable criteria.
// They return review artifacts and never receive HAI tools, sources, memory,
// workflow state, credentials, or execution authority.
func initializeCrewAIRoutes(apiVersion *gin.RouterGroup, handler *crewai.Handler) {
	routes := apiVersion.Group("/crewai")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/proposals", requirePermission(rbac.PermWrite), handler.Propose)
	}
}

func initializeAgentFrameworkRoutes(apiVersion *gin.RouterGroup, handler *agentframework.Handler) {
	routes := apiVersion.Group("/agent-framework")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.POST("/probe", requirePermission(rbac.PermAdmin), handler.Probe)
		routes.POST("/proposals", requirePermission(rbac.PermWrite), handler.Propose)
	}
}

func initializeBrowserVerificationRoutes(apiVersion *gin.RouterGroup, handler *browserverify.Handler) {
	routes := apiVersion.Group("/browser-verification")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.GET("/profiles", requirePermission(rbac.PermRead), handler.Profiles)
		routes.GET("/runs", requirePermission(rbac.PermRead), handler.Runs)
		// A browser check is read-only but still approval-gated: it may expose an
		// application route's current state, so it is never an autonomous scan.
		routes.POST("/profiles/:id/run", requirePermission(rbac.PermApprove), handler.Run)
	}
}

func initializeWASIRoutes(apiVersion *gin.RouterGroup, handler *wasiexec.Handler) {
	routes := apiVersion.Group("/wasi")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.GET("/modules", requirePermission(rbac.PermRead), handler.Modules)
		routes.GET("/runs", requirePermission(rbac.PermRead), handler.Runs)
		routes.POST("/modules/:id/run", requirePermission(rbac.PermApprove), handler.Run)
	}
}

func initializeTemporalRoutes(apiVersion *gin.RouterGroup, handler *temporalbridge.Handler) {
	routes := apiVersion.Group("/temporal")
	routes.Use(requireAuthenticatedOwner())
	{
		routes.GET("/status", requirePermission(rbac.PermRead), handler.Status)
		routes.GET("/follow-up-runs", requirePermission(rbac.PermRead), handler.Runs)
		// A worker start only connects to a local, validated Temporal endpoint and
		// registers the proposal-only workflow. Scheduling it remains approval-gated.
		routes.POST("/worker/start", requirePermission(rbac.PermAdmin), handler.StartWorker)
		routes.POST("/follow-up-runs", requirePermission(rbac.PermApprove), handler.ScheduleFollowUp)
	}
}

func defaultFeatureFlags() *featureflags.Store {
	store := featureflags.New()
	store.Set(featureflags.Flag{Key: "memory_query_search", Enabled: true, RolloutPercent: 100, Description: "Search/filter/sort/pagination on the memory list"})
	store.Set(featureflags.Flag{Key: "readiness_probe", Enabled: true, RolloutPercent: 100, Description: "Expose /readyz readiness endpoint"})
	return store
}

func initializeFeatureFlagRoutes(apiVersion *gin.RouterGroup, store *featureflags.Store) {
	flags := apiVersion.Group("/flags")
	flags.Use(requireAuthenticatedOwner())
	flags.GET("", requirePermission(rbac.PermRead), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"flags": store.List()})
	})
}

func initializeHAIOSRoutes(apiVersion *gin.RouterGroup, osHandler *haios.Handler) {
	osRoutes := apiVersion.Group("/os")
	osRoutes.Use(haios.RequireAuthenticatedOwner())
	{
		osRoutes.GET("/overview", requirePermission(rbac.PermRead), osHandler.Overview)
	}
}

func initializeWorkflowRoutes(apiVersion *gin.RouterGroup, workflowHandler *workflow.Handler) {
	workflowRoutes := apiVersion.Group("/workflow")
	workflowRoutes.Use(workflow.RequireAuthenticatedOwner())
	{
		workflowRoutes.GET("/overview", requirePermission(rbac.PermRead), workflowHandler.Overview)
		workflowRoutes.GET("/approvals", requirePermission(rbac.PermRead), workflowHandler.ApprovalItems)
		workflowRoutes.GET("/dashboard", requirePermission(rbac.PermRead), workflowHandler.Dashboard)
		workflowRoutes.GET("/reminder-proposals", requirePermission(rbac.PermRead), workflowHandler.ReminderProposals)
		workflowRoutes.POST("/reminder-proposals/:itemId/activation-requests", requirePermission(rbac.PermWrite), workflowHandler.PrepareReminderActivation)
		workflowRoutes.GET("/reminder-activation-requests", requirePermission(rbac.PermRead), workflowHandler.ReminderActivationHistory)
		workflowRoutes.GET("/reminder-activation-requests/:requestId/decisions", requirePermission(rbac.PermRead), workflowHandler.ReminderActivationDecisionHistory)
		workflowRoutes.POST("/reminder-activation-requests/:requestId/decisions", requirePermission(rbac.PermApprove), workflowHandler.DecideReminderActivation)
		workflowRoutes.POST("/reminder-activation-requests/:requestId/delivery-authorizations", requirePermission(rbac.PermApprove), workflowHandler.AuthorizeReminderDelivery)
		workflowRoutes.GET("/reminder-deliveries", requirePermission(rbac.PermRead), workflowHandler.ReminderDeliveryHistory)
		workflowRoutes.POST("/reminder-deliveries/run-due", requirePermission(rbac.PermExecute), workflowHandler.RunDueReminderDeliveries)
		workflowRoutes.GET("/", requirePermission(rbac.PermRead), workflowHandler.Items)
		workflowRoutes.POST("/intake", requirePermission(rbac.PermWrite), workflowHandler.Intake)
		// These HTTP worker controls run already-governed, owner-scoped work.
		// Operators may execute them, but only an owner may resolve decisions.
		workflowRoutes.POST("/recover-stale", requirePermission(rbac.PermExecute), workflowHandler.RecoverStaleClaims)
		workflowRoutes.POST("/run-due", requirePermission(rbac.PermExecute), workflowHandler.RunDue)
		workflowRoutes.POST("/open-loops/run-due", requirePermission(rbac.PermExecute), workflowHandler.RunDueOpenLoops)
		workflowRoutes.GET("/:id", requirePermission(rbac.PermRead), workflowHandler.Get)
		workflowRoutes.POST("/:id/run", requirePermission(rbac.PermExecute), workflowHandler.RunOne)
		workflowRoutes.POST("/:id/transition", requirePermission(rbac.PermWrite), workflowHandler.Transition)
		workflowRoutes.POST("/:id/approval", requirePermission(rbac.PermApprove), workflowHandler.ResolveApproval)
		workflowRoutes.POST("/:id/interruption/resolve", requirePermission(rbac.PermApprove), workflowHandler.ResolveInterruptedExecution)
		workflowRoutes.POST("/:id/proposals/:proposalId/resolve", requirePermission(rbac.PermApprove), workflowHandler.ResolveProposal)
		workflowRoutes.PATCH("/:id/checklist/:itemId", requirePermission(rbac.PermWrite), workflowHandler.UpdateChecklistItem)
	}
}

func initializePursuitRoutes(apiVersion *gin.RouterGroup, pursuitHandler *pursuit.Handler) {
	pursuitRoutes := apiVersion.Group("/pursuits")
	pursuitRoutes.Use(pursuit.RequireAuthenticatedOwner())
	{
		pursuitRoutes.GET("/", requirePermission(rbac.PermRead), pursuitHandler.List)
		pursuitRoutes.POST("/", requirePermission(rbac.PermWrite), pursuitHandler.Create)
		pursuitRoutes.GET("/dashboard", requirePermission(rbac.PermRead), pursuitHandler.Dashboard)
		pursuitRoutes.GET("/brief", requirePermission(rbac.PermRead), pursuitHandler.Brief)
		pursuitRoutes.GET("/decisions", requirePermission(rbac.PermRead), pursuitHandler.Decisions)
		pursuitRoutes.POST("/portfolio-plan", requirePermission(rbac.PermRead), pursuitHandler.PlanPortfolio)
		pursuitRoutes.POST("/portfolio-plan/accept", requirePermission(rbac.PermApprove), pursuitHandler.AcceptPortfolioAllocation)
		pursuitRoutes.GET("/portfolio-allocations", requirePermission(rbac.PermRead), pursuitHandler.PortfolioAllocationHistory)
		pursuitRoutes.POST("/portfolio-allocations/:allocationId/execution-proposals", requirePermission(rbac.PermApprove), pursuitHandler.PreparePortfolioExecutionProposals)
		pursuitRoutes.GET("/portfolio-execution-proposals", requirePermission(rbac.PermRead), pursuitHandler.PortfolioExecutionProposalHistory)
		pursuitRoutes.GET("/portfolio-execution-proposals/coordination", requirePermission(rbac.PermRead), pursuitHandler.PortfolioDispatchCoordinationBatch)
		pursuitRoutes.GET("/portfolio-execution-proposals/:proposalId/coordination", requirePermission(rbac.PermRead), pursuitHandler.PortfolioDispatchCoordination)
		pursuitRoutes.POST("/portfolio-execution-proposals/:proposalId/dispatch", requirePermission(rbac.PermExecute), pursuitHandler.DispatchPortfolioWorkflows)
		pursuitRoutes.GET("/portfolio-execution-proposal-items/:itemId/decisions", requirePermission(rbac.PermRead), pursuitHandler.PortfolioExecutionProposalDecisionHistory)
		pursuitRoutes.POST("/portfolio-execution-proposal-items/:itemId/decisions", requirePermission(rbac.PermApprove), pursuitHandler.DecidePortfolioExecutionProposalItem)
		pursuitRoutes.POST("/portfolio-execution-proposal-items/:itemId/authorize-workflow", requirePermission(rbac.PermExecute), pursuitHandler.AuthorizePortfolioWorkflowEffect)
		pursuitRoutes.POST("/portfolio-execution-proposal-items/:itemId/execute-workflow", requirePermission(rbac.PermExecute), pursuitHandler.ExecutePortfolioWorkflowEffect)
		pursuitRoutes.POST("/portfolio-execution-proposal-items/:itemId/settle-workflow", requirePermission(rbac.PermExecute), pursuitHandler.SettlePortfolioWorkflow)
		pursuitRoutes.POST("/match", requirePermission(rbac.PermRead), pursuitHandler.Match)
		pursuitRoutes.POST("/intake", requirePermission(rbac.PermWrite), pursuitHandler.RouteIntake)
		pursuitRoutes.GET("/:id/evidence", requirePermission(rbac.PermRead), pursuitHandler.ResolveEvidence)
		pursuitRoutes.GET("/:id/resources", requirePermission(rbac.PermRead), pursuitHandler.ResourceUsage)
		pursuitRoutes.GET("/:id/resource-events", requirePermission(rbac.PermRead), pursuitHandler.ResourceEvents)
		pursuitRoutes.POST("/:id/resource-events", requirePermission(rbac.PermWrite), pursuitHandler.AppendResourceEvent)
		pursuitRoutes.POST("/:id/resource-reservations/:reservationId/release", requirePermission(rbac.PermApprove), pursuitHandler.ReleaseResourceReservation)
		pursuitRoutes.GET("/:id", requirePermission(rbac.PermRead), pursuitHandler.Get)
		pursuitRoutes.PATCH("/:id", requirePermission(rbac.PermWrite), pursuitHandler.Update)
		pursuitRoutes.POST("/:id/archive", requirePermission(rbac.PermWrite), pursuitHandler.Archive)
		pursuitRoutes.POST("/:id/reopen", requirePermission(rbac.PermWrite), pursuitHandler.Reopen)
		pursuitRoutes.POST("/:id/summary", requirePermission(rbac.PermWrite), pursuitHandler.RefreshSummary)
		pursuitRoutes.POST("/:id/review", requirePermission(rbac.PermWrite), pursuitHandler.Review)
		pursuitRoutes.POST("/:id/decisions/resolve", requirePermission(rbac.PermApprove), pursuitHandler.ResolveDecision)
		pursuitRoutes.GET("/:id/activity", requirePermission(rbac.PermRead), pursuitHandler.Activity)
		pursuitRoutes.GET("/:id/next-actions", requirePermission(rbac.PermRead), pursuitHandler.NextActions)
		pursuitRoutes.GET("/:id/blockers", requirePermission(rbac.PermRead), pursuitHandler.Blockers)
		pursuitRoutes.GET("/:id/approvals", requirePermission(rbac.PermRead), pursuitHandler.Approvals)
		pursuitRoutes.GET("/:id/delegation", requirePermission(rbac.PermRead), pursuitHandler.DelegationPackage)
		pursuitRoutes.POST("/:id/intake", requirePermission(rbac.PermWrite), pursuitHandler.Intake)
		pursuitRoutes.POST("/:id/plan", requirePermission(rbac.PermWrite), pursuitHandler.Plan)
		pursuitRoutes.POST("/:id/candidate/accept", requirePermission(rbac.PermApprove), pursuitHandler.AcceptCandidate)
		pursuitRoutes.POST("/:id/links", requirePermission(rbac.PermWrite), pursuitHandler.Link)
		pursuitRoutes.DELETE("/:id/links/:linkId", requirePermission(rbac.PermWrite), pursuitHandler.DeleteLink)
	}
}

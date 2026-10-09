package automation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOpenClawDirectCLIBlockPreservesImmutableLaunchIdentity(t *testing.T) {
	automationID := uuid.New()
	repo := newFakeAutomationRepo(openClawCLITestAutomation(automationID))
	service, registry := newOpenClawCLITestService(t, repo)
	if registry.OpenClawGatewayDelegationReady() {
		t.Fatal("CLI-selected production adapter advertised Gateway delegation")
	}

	result, err := service.LaunchTask(automationID, approvedTaskLaunchRequest(t, service, automationID, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Run the bounded OpenClaw task fixture.",
		ProjectKey:    "hai-runtime-regression",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	assertOpenClawDirectCLIBlocked(t, result)

	intents, events := fakeOpenClawLaunchRecords(t, repo)
	var launchIntent *models.AutomationLaunchEvent
	for index := range intents {
		intent := &intents[index]
		if intent.AutomationID == automationID && intent.OwnerIdentity == "alice" && intent.LaunchType == "agent_runtime_intent" {
			launchIntent = intent
		}
	}
	if launchIntent == nil {
		t.Fatal("blocked launch did not preserve its durable pre-execution intent")
	}
	if launchIntent.RuntimeTaskID != result.RuntimeTaskID ||
		launchIntent.RuntimeTaskID != automationLaunchRuntimeTaskID(&models.Automation{ID: automationID}, launchIntent.ID) {
		t.Fatalf("blocked launch changed its immutable task identity: intent=%#v result=%#v", launchIntent, result)
	}
	if launchIntent.Status != "pending" || launchIntent.ExecutionReference != "" {
		t.Fatalf("direct CLI intent must remain an honest pending record without a Gateway reference: %#v", launchIntent)
	}

	var outcome *models.AutomationLaunchEvent
	for index := range events {
		event := &events[index]
		if event.ID == result.LaunchEventID {
			outcome = event
			break
		}
	}
	if outcome == nil || outcome.LaunchType != "agent_runtime" || outcome.RuntimeTaskID != launchIntent.RuntimeTaskID ||
		outcome.OwnerIdentity != launchIntent.OwnerIdentity || outcome.Status != "blocked" || outcome.Output != "" {
		t.Fatalf("blocked outcome did not reconcile by exact owner/task identity without claiming completion: intent=%#v outcome=%#v", launchIntent, outcome)
	}
	if pending, err := repo.FindPendingRuntimeLaunchIntent(automationID, "alice"); err != nil || pending != nil {
		t.Fatalf("blocked outcome did not reconcile its matching intent: pending=%#v err=%v", pending, err)
	}
}

func TestOpenClawStopForPendingLegacyCLIIntentIsBlockedAndCorrelated(t *testing.T) {
	automationID := uuid.New()
	repo := newFakeAutomationRepo(openClawCLITestAutomation(automationID))
	service, registry := newOpenClawCLITestService(t, repo)
	if registry.OpenClawGatewayDelegationReady() {
		t.Fatal("CLI-selected production adapter advertised Gateway delegation")
	}

	const owner = "alice"
	intentID := uuid.New()
	taskID := automationLaunchRuntimeTaskID(&models.Automation{ID: automationID}, intentID)
	launchIntent := newOpenClawLaunchIntent(automationID, owner, intentID, taskID, "ocgw:v2:"+uuid.NewString())
	if err := repo.SaveLaunchIntent(launchIntent); err != nil {
		t.Fatalf("SaveLaunchIntent: %v", err)
	}

	stop, err := service.StopRuntimeTaskForOwner(automationID, owner)
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if stop.Status != "blocked" || stop.TaskID != taskID || stop.ExecutionReference != "" {
		t.Fatalf("legacy direct-CLI intent must not be presented as remotely cancelled or routed by its stale Gateway reference: %#v", stop)
	}
	if pending, err := repo.FindPendingRuntimeLaunchIntent(automationID, owner); err != nil || pending == nil || pending.ID != intentID {
		t.Fatalf("blocked stop changed the launch intent or claimed a launch outcome: pending=%#v err=%v", pending, err)
	}

	intents, outcomes := fakeOpenClawLaunchRecords(t, repo)
	var stopIntent *models.AutomationLaunchEvent
	for index := range intents {
		intent := &intents[index]
		if intent.LaunchType == "agent_runtime_stop_intent" && intent.AutomationID == automationID && intent.OwnerIdentity == owner {
			stopIntent = intent
		}
	}
	if stopIntent == nil || stopIntent.RuntimeTaskID != taskID || stopIntent.ExecutionReference != "" || stopIntent.Status != "pending" {
		t.Fatalf("stop intent did not preserve the exact task identity while stripping the legacy reference: %#v", stopIntent)
	}
	var stopOutcome *models.AutomationLaunchEvent
	for index := range outcomes {
		event := &outcomes[index]
		if event.LaunchType == "agent_runtime_stop" && event.RuntimeTaskID == taskID {
			stopOutcome = event
		}
	}
	if stopOutcome == nil || stopOutcome.Status != "blocked" || stopOutcome.ExecutionReference != "" ||
		stopOutcome.EventKey != runtimeStopOutcomeEventKey(stopIntent.ID) {
		t.Fatalf("blocked stop outcome was not correlated to its exact immutable stop intent: intent=%#v outcome=%#v", stopIntent, stopOutcome)
	}
	if unresolved, err := repo.FindUnresolvedRuntimeStopIntent(automationID, owner, taskID); err != nil || unresolved != nil {
		t.Fatalf("exact blocked-stop outcome did not resolve its own stop intent: unresolved=%#v err=%v", unresolved, err)
	}
}

func TestPostgresOpenClawIntentAndStopReconciliationUsesImmutableIdentity(t *testing.T) {
	db := approvalProofPostgresDB(t)
	rollback := errors.New("rollback OpenClaw identity fixtures")
	err := db.Transaction(func(tx *gorm.DB) error {
		repo := &GormUserRepository{DB: tx}
		automationID := uuid.New()
		now := time.Now().UTC()
		owner := "openclaw-identity-test-" + uuid.NewString()
		legacyReference := "ocgw:v2:" + uuid.NewString()
		intentID := uuid.New()
		taskID := automationLaunchRuntimeTaskID(&models.Automation{ID: automationID}, intentID)
		if err := repo.SaveLaunchIntent(newOpenClawLaunchIntent(automationID, owner, intentID, taskID, legacyReference)); err != nil {
			return fmt.Errorf("persist legacy launch intent: %w", err)
		}
		if err := repo.SaveLaunchEvent(&models.AutomationLaunchEvent{
			ID: uuid.New(), AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
			LaunchType: "agent_runtime", RuntimeTaskID: taskID, ExecutionReference: "",
			Status: "blocked", Message: "direct CLI execution was blocked before runtime invocation",
			StartedAt: now.Add(time.Second), CompletedAt: now.Add(time.Second),
		}); err != nil {
			return fmt.Errorf("persist blocked runtime outcome: %w", err)
		}
		pending, err := repo.FindPendingRuntimeLaunchIntent(automationID, owner)
		if err != nil {
			return fmt.Errorf("lookup blocked intent by immutable task ID: %w", err)
		}
		if pending != nil {
			return fmt.Errorf("blocked outcome with empty reference did not reconcile intent %s", pending.ID)
		}

		unmatchedIntentID := uuid.New()
		unmatchedTaskID := automationLaunchRuntimeTaskID(&models.Automation{ID: automationID}, unmatchedIntentID)
		if err := repo.SaveLaunchIntent(newOpenClawLaunchIntent(automationID, owner, unmatchedIntentID, unmatchedTaskID, legacyReference)); err != nil {
			return fmt.Errorf("persist unmatched launch intent: %w", err)
		}
		if err := repo.SaveLaunchEvent(&models.AutomationLaunchEvent{
			ID: uuid.New(), AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
			LaunchType: "agent_runtime", RuntimeTaskID: "unrelated-task:" + uuid.NewString(),
			ExecutionReference: legacyReference, Status: "blocked",
			Message:   "unrelated blocked task shares only a historical Gateway reference",
			StartedAt: now.Add(3 * time.Second), CompletedAt: now.Add(3 * time.Second),
		}); err != nil {
			return fmt.Errorf("persist unrelated launch outcome: %w", err)
		}
		pending, err = repo.FindPendingRuntimeLaunchIntent(automationID, owner)
		if err != nil {
			return fmt.Errorf("lookup task-mismatched launch intent: %w", err)
		}
		if pending == nil || pending.ID != unmatchedIntentID || pending.RuntimeTaskID != unmatchedTaskID {
			return fmt.Errorf("reference-only match incorrectly reconciled a different immutable task intent: %#v", pending)
		}

		stopIntentID := uuid.New()
		if err := repo.SaveLaunchIntent(&models.AutomationLaunchEvent{
			ID: stopIntentID, AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
			LaunchType: "agent_runtime_stop_intent", RuntimeTaskID: unmatchedTaskID,
			Status: "pending", StartedAt: now.Add(4 * time.Second), CompletedAt: now.Add(4 * time.Second),
		}); err != nil {
			return fmt.Errorf("persist stop intent: %w", err)
		}
		unresolved, err := repo.FindUnresolvedRuntimeStopIntent(automationID, owner, unmatchedTaskID)
		if err != nil || unresolved == nil || unresolved.ID != stopIntentID {
			return fmt.Errorf("stop intent was not found before its outcome: intent=%#v err=%v", unresolved, err)
		}
		if err := repo.SaveLaunchEvent(&models.AutomationLaunchEvent{
			ID: uuid.New(), AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
			LaunchType: "agent_runtime_stop", RuntimeTaskID: unmatchedTaskID,
			EventKey: runtimeStopOutcomeEventKey(stopIntentID), Status: "blocked",
			Message:   "remote cancellation was not attempted for an untracked direct-CLI task",
			StartedAt: now.Add(5 * time.Second), CompletedAt: now.Add(5 * time.Second),
		}); err != nil {
			return fmt.Errorf("persist correlated blocked stop outcome: %w", err)
		}
		unresolved, err = repo.FindUnresolvedRuntimeStopIntent(automationID, owner, unmatchedTaskID)
		if err != nil || unresolved != nil {
			return fmt.Errorf("exact stop outcome left its intent unresolved: intent=%#v err=%v", unresolved, err)
		}

		secondStopIntentID := uuid.New()
		if err := repo.SaveLaunchIntent(&models.AutomationLaunchEvent{
			ID: secondStopIntentID, AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
			LaunchType: "agent_runtime_stop_intent", RuntimeTaskID: unmatchedTaskID,
			Status: "pending", StartedAt: now.Add(6 * time.Second), CompletedAt: now.Add(6 * time.Second),
		}); err != nil {
			return fmt.Errorf("persist second stop intent: %w", err)
		}
		unresolved, err = repo.FindUnresolvedRuntimeStopIntent(automationID, owner, unmatchedTaskID)
		if err != nil || unresolved == nil || unresolved.ID != secondStopIntentID {
			return fmt.Errorf("earlier stop outcome incorrectly closed a later intent: intent=%#v err=%v", unresolved, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Postgres identity regression assertions failed: %v", err)
	}
}

func TestPostgresOpenClawBlockedDirectCLIOutcomeUsesProductionAdapterAndRepository(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := &GormUserRepository{DB: db}
	automation := createPostgresOpenClawCLITestAutomation(t, repo)
	service, registry := newOpenClawCLITestService(t, repo)
	owner := "postgres-cli-owner-" + uuid.NewString()
	if registry.OpenClawGatewayDelegationReady() {
		t.Fatal("CLI-selected production adapter advertised Gateway delegation")
	}

	result, err := service.LaunchTask(automation.ID, approvedTaskLaunchRequest(t, service, automation.ID, TaskLaunchRequest{
		OwnerIdentity: owner,
		Task:          "Verify the direct CLI route is durably blocked by the production repository.",
		ProjectKey:    "hai-runtime-regression",
	}))
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	assertOpenClawDirectCLIBlocked(t, result)

	var launchIntents []models.AutomationLaunchEvent
	if err := db.Where("automation_id = ? AND owner_identity = ? AND launch_type = ?", automation.ID, owner, "agent_runtime_intent").Find(&launchIntents).Error; err != nil {
		t.Fatalf("load durable launch intents: %v", err)
	}
	if len(launchIntents) != 1 || launchIntents[0].RuntimeTaskID != result.RuntimeTaskID || launchIntents[0].Status != "pending" {
		t.Fatalf("Postgres did not preserve the exact pre-execution intent: %#v", launchIntents)
	}
	if result.RuntimeTaskID != automationLaunchRuntimeTaskID(automation, launchIntents[0].ID) {
		t.Fatalf("blocked task ID is not derived from the persisted immutable intent: intent=%#v result=%#v", launchIntents[0], result)
	}
	if pending, err := repo.FindPendingRuntimeLaunchIntent(automation.ID, owner); err != nil || pending != nil {
		t.Fatalf("GORM repository did not reconcile the blocked outcome by immutable task ID: pending=%#v err=%v", pending, err)
	}

	outcomes, err := repo.FindLaunchEvents(automation.ID, 20)
	if err != nil {
		t.Fatalf("FindLaunchEvents: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].ID != result.LaunchEventID || outcomes[0].LaunchType != "agent_runtime" ||
		outcomes[0].RuntimeTaskID != launchIntents[0].RuntimeTaskID || outcomes[0].OwnerIdentity != owner ||
		outcomes[0].Status != "blocked" || outcomes[0].Output != "" || outcomes[0].ExecutionReference != "" {
		t.Fatalf("persisted Postgres outcome falsely implies a CLI launch or completion: %#v", outcomes)
	}
}

func TestPostgresOpenClawBlockedStopCorrelatesExactIntentWithoutClaimingCancellation(t *testing.T) {
	db := approvalProofPostgresDB(t)
	repo := &GormUserRepository{DB: db}
	automation := createPostgresOpenClawCLITestAutomation(t, repo)
	service, registry := newOpenClawCLITestService(t, repo)
	if registry.OpenClawGatewayDelegationReady() {
		t.Fatal("CLI-selected production adapter advertised Gateway delegation")
	}

	owner := "postgres-cli-stop-owner-" + uuid.NewString()
	launchIntentID := uuid.New()
	taskID := automationLaunchRuntimeTaskID(automation, launchIntentID)
	if err := repo.SaveLaunchIntent(newOpenClawLaunchIntent(
		automation.ID,
		owner,
		launchIntentID,
		taskID,
		"ocgw:v2:"+uuid.NewString(),
	)); err != nil {
		t.Fatalf("SaveLaunchIntent: %v", err)
	}

	stop, err := service.StopRuntimeTaskForOwner(automation.ID, owner)
	if err != nil {
		t.Fatalf("StopRuntimeTaskForOwner: %v", err)
	}
	if stop.Status != "blocked" || stop.TaskID != taskID || stop.ExecutionReference != "" {
		t.Fatalf("production stop must not claim cancellation or route a stale Gateway reference: %#v", stop)
	}
	if pending, err := repo.FindPendingRuntimeLaunchIntent(automation.ID, owner); err != nil || pending == nil || pending.ID != launchIntentID {
		t.Fatalf("blocked stop outcome must not masquerade as a launch outcome: pending=%#v err=%v", pending, err)
	}
	if active, err := repo.FindOwnerActiveRuntimeLaunch(automation.ID, "openclaw", owner); err != nil || active != nil {
		t.Fatalf("blocked stop created a false active runtime launch: event=%#v err=%v", active, err)
	}

	var stopIntents []models.AutomationLaunchEvent
	if err := db.Where("automation_id = ? AND owner_identity = ? AND launch_type = ?", automation.ID, owner, "agent_runtime_stop_intent").Find(&stopIntents).Error; err != nil {
		t.Fatalf("load durable stop intents: %v", err)
	}
	if len(stopIntents) != 1 || stopIntents[0].RuntimeTaskID != taskID || stopIntents[0].ExecutionReference != "" || stopIntents[0].Status != "pending" {
		t.Fatalf("Postgres stop intent lost task identity or retained the stale Gateway reference: %#v", stopIntents)
	}
	stopOutcomes, err := repo.FindLaunchEvents(automation.ID, 20)
	if err != nil {
		t.Fatalf("FindLaunchEvents: %v", err)
	}
	if len(stopOutcomes) != 1 || stopOutcomes[0].LaunchType != "agent_runtime_stop" || stopOutcomes[0].RuntimeTaskID != taskID ||
		stopOutcomes[0].Status != "blocked" || stopOutcomes[0].ExecutionReference != "" ||
		stopOutcomes[0].EventKey != runtimeStopOutcomeEventKey(stopIntents[0].ID) {
		t.Fatalf("Postgres stop outcome was not correlated to the exact blocked stop intent: %#v", stopOutcomes)
	}
	if unresolved, err := repo.FindUnresolvedRuntimeStopIntent(automation.ID, owner, taskID); err != nil || unresolved != nil {
		t.Fatalf("exact blocked stop outcome did not resolve its own stop intent: unresolved=%#v err=%v", unresolved, err)
	}
	var runtimeOutcomes int64
	if err := db.Model(&models.AutomationLaunchEvent{}).
		Where("automation_id = ? AND owner_identity = ? AND launch_type = ?", automation.ID, owner, "agent_runtime").
		Count(&runtimeOutcomes).Error; err != nil {
		t.Fatalf("count runtime outcomes: %v", err)
	}
	if runtimeOutcomes != 0 {
		t.Fatalf("blocked stop fabricated %d runtime launch outcomes", runtimeOutcomes)
	}
}

func createPostgresOpenClawCLITestAutomation(t *testing.T, repo *GormUserRepository) *models.Automation {
	t.Helper()
	id := uuid.New()
	suffix := strings.ReplaceAll(id.String(), "-", "")[:12]
	automation := &models.Automation{
		ID: id, Name: "OC CLI " + suffix, URLPath: "oc-cli-" + suffix,
		Host: "localhost", Port: 8080, Position: int(time.Now().UnixMilli()),
		LaunchType: "agent_runtime", RuntimeType: "openclaw", LaunchTarget: "runtime://openclaw",
	}
	created, err := repo.Create(automation)
	if err != nil {
		t.Fatalf("create Postgres OpenClaw automation fixture: %v", err)
	}
	return created
}

func openClawCLITestAutomation(id uuid.UUID) *models.Automation {
	return &models.Automation{
		ID: id, Name: "OpenClaw direct CLI identity fixture", URLPath: "openclaw-cli-identity-fixture",
		Host: "localhost", Port: 8080, LaunchType: "agent_runtime", RuntimeType: "openclaw",
		LaunchTarget: "runtime://openclaw",
	}
}

func newOpenClawLaunchIntent(automationID uuid.UUID, owner string, intentID uuid.UUID, taskID, reference string) *models.AutomationLaunchEvent {
	now := time.Now().UTC()
	return &models.AutomationLaunchEvent{
		ID: intentID, AutomationID: automationID, OwnerIdentity: owner, RuntimeType: "openclaw",
		LaunchType: "agent_runtime_intent", RuntimeTaskID: taskID, ExecutionReference: reference,
		Status: "pending", Message: "immutable pre-execution intent recorded",
		StartedAt: now, CompletedAt: now,
	}
}

func newOpenClawCLITestService(t *testing.T, repo Repository) (Service, *agentruntime.Registry) {
	t.Helper()
	for name, value := range map[string]string{
		"OPENCLAW_AGENT_ENABLED":                           "true",
		"OPENCLAW_AGENT_CLI_ENABLED":                       "true",
		"OPENCLAW_GATEWAY_ENABLED":                         "false",
		"OPENCLAW_GATEWAY_DELEGATION_ENABLED":              "false",
		"OPENCLAW_SANDBOX_REQUIRED":                        "true",
		"OPENCLAW_SANDBOX_MODE":                            "all",
		"OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED":      "false",
		"OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED":          "false",
		"OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED":   "false",
		"OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED":    "false",
		"OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED": "false",
		"OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED":  "false",
		"OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED":         "false",
	} {
		t.Setenv(name, value)
	}
	for _, name := range []string{
		"OPENCLAW_MESSAGES_ENABLED", "OPENCLAW_SKILLS_ENABLED", "OPENCLAW_PLUGINS_ENABLED",
		"OPENCLAW_MCP_ENABLED", "OPENCLAW_MEMORY_ENABLED", "OPENCLAW_CRON_ENABLED",
		"OPENCLAW_BROWSER_ENABLED", "OPENCLAW_CANVAS_ENABLED", "OPENCLAW_NODES_ENABLED",
		"OPENCLAW_VOICE_ENABLED", "OPENCLAW_TALK_ENABLED", "OPENCLAW_WEBCHAT_ENABLED",
		"OPENCLAW_PAIRING_ENABLED", "OPENCLAW_EXEC_APPROVALS_ENABLED", "OPENCLAW_HOST_TOOLS_ENABLED",
		"OPENCLAW_PUBLIC_POSTING_ENABLED", "OPENCLAW_WEB_SEARCH_ENABLED", "OPENCLAW_MULTI_AGENT_ENABLED",
		"OPENCLAW_APP_SDK_ENABLED", "OPENCLAW_PLUGIN_SDK_ENABLED", "OPENCLAW_LOCAL_MODELS_ENABLED",
		"OPENCLAW_ALLOW_HIGH_RISK_EXECUTION", "OPENCLAW_SANDBOX_DOCKER_ENABLED",
		"OPENCLAW_SANDBOX_SSH_ENABLED", "OPENCLAW_SANDBOX_OPENSHELL_ENABLED",
	} {
		t.Setenv(name, "false")
	}

	authorizationRepository := executionauth.NewMemoryRepository()
	finalEffects, err := executionauth.NewFinalEffectBridge(authorizationRepository, nil)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	authorizationService, err := executionauth.NewService(
		authorizationRepository,
		permissiveExecutionConstitution{},
		nil,
		nil,
		exactTestApprovalResolver{},
		nil,
	)
	if err != nil {
		t.Fatalf("executionauth.NewService: %v", err)
	}
	authorizationService.WithEmergencyStopEvaluator(func() executionauth.EmergencyStopEvidence {
		return executionauth.EmergencyStopEvidence{Source: "OpenClaw blocked-route regression test"}
	})
	registry := agentruntime.DefaultRegistryWithFinalEffectVerifierAndHostRuntimeAndOpenClawGatewayReceiptStore(finalEffects, nil, nil)
	service := NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
		repo,
		events.Publisher{},
		registry,
		newUnitTestApprovalProofService(),
		inMemoryTestExecutionAuthorizer{
			service:    authorizationService,
			repository: authorizationRepository,
		},
		finalEffects,
	)
	return service, registry
}

func assertOpenClawDirectCLIBlocked(t *testing.T, result *LaunchResult) {
	t.Helper()
	if result == nil || result.Status != "blocked" || !result.RequiresApproval || result.RuntimeTaskID == "" ||
		result.ExecutionReference != "" || result.Output != "" || result.ExitCode >= 0 ||
		!strings.Contains(result.Message, "direct OpenClaw CLI execution is blocked") {
		t.Fatalf("direct OpenClaw CLI result must be durably blocked without claiming execution or completion: %#v", result)
	}
}

func fakeOpenClawLaunchRecords(t *testing.T, repo *fakeAutomationRepo) ([]models.AutomationLaunchEvent, []models.AutomationLaunchEvent) {
	t.Helper()
	repo.launchEventsMu.RLock()
	defer repo.launchEventsMu.RUnlock()
	intents := append([]models.AutomationLaunchEvent(nil), repo.launchIntents...)
	events := append([]models.AutomationLaunchEvent(nil), repo.launchEvents...)
	return intents, events
}

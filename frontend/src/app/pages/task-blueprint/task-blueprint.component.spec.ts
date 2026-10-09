import { FormBuilder } from '@angular/forms';
import { ActivatedRoute, Router, convertToParamMap } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { TaskBlueprintComponent } from './task-blueprint.component';
import { IFrameworkSelectionDecision } from '../../models/framework-registry.model.interface';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';

describe('TaskBlueprintComponent task conversation', () => {
	function createComponent(pursuitId: string = '', preferences = new ModuleViewPreferencesService(document), queryParams?: Record<string, string>): {
		component: TaskBlueprintComponent;
		router: jasmine.SpyObj<Router>;
		taskPlans: jasmine.SpyObj<any>;
		assistant: jasmine.SpyObj<any>;
		notifications: jasmine.SpyObj<NzNotificationService>;
		modal: jasmine.SpyObj<NzModalService>;
		preferences: ModuleViewPreferencesService;
	} {
    const router = jasmine.createSpyObj<Router>('Router', ['navigate']);
		const taskPlans = jasmine.createSpyObj('TaskPlanService', ['logs', 'reviewQueue', 'resolveReviewItem', 'reconcileApprovedReviews']);
    taskPlans.logs.and.returnValue(of([]));
    taskPlans.reviewQueue.and.returnValue(of([]));
		taskPlans.resolveReviewItem.and.returnValue(of({ item: {} }));
		const assistant = jasmine.createSpyObj('AssistantCommandService', ['command']);
		assistant.command.and.returnValue(of({
			id: 'command-1',
			createdAt: '2026-08-04T00:00:00Z',
			intent: 'plan',
			summary: 'Plan prepared.',
			nextAction: 'Review the plan.',
			safetySummary: 'No execution occurred.',
			actions: [],
			reviewRequired: false,
		}));
		const notifications = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning', 'info']);
		const modal = jasmine.createSpyObj<NzModalService>('NzModalService', ['confirm']);
    const route = {
      queryParamMap: of(convertToParamMap(queryParams ?? (pursuitId ? { pursuitId } : {}))),
    } as ActivatedRoute;

    return {
      component: new TaskBlueprintComponent(
        new FormBuilder(),
        taskPlans,
				assistant,
				notifications,
        modal,
        router,
        route,
        {} as any,
        { propose: jasmine.createSpy('propose') } as any,
        preferences,
      ),
			router,
			taskPlans,
			assistant,
			notifications,
			modal,
			preferences,
		};
  }

  afterEach(() => localStorage.removeItem('hai.module-view.v1.task-blueprint'));

  function persistedReceipt(overrides: any = {}): any {
    return {
      id: 'historical-task', request: 'Collect evidence.', projectKey: '018-HAI', pursuitId: 'pursuit-1',
      intake: { successCriteria: ['Evidence linked'] }, steps: [{ allowed: true, requiresApproval: false }],
      riskAssessment: {}, completionStatus: 'validated', validationResult: { passed: true },
      retryPolicy: { retryAvailable: false },
      executionResult: { mode: 'action', outcomeUncertain: true, toolExecution: {
        status: 'completed', outcomeUncertain: true, launchEventId: 'historical-launch',
        runtimeTaskId: 'historical-runtime-task', executionReference: 'runtime://historical-attempt',
        receipt: { runtimeId: 'local-runtime', ok: true, output: { artifactHash: 'historical-hash', boundedOutput: 'Artifact retained.' }, verification: { passed: true } },
      } },
      ...overrides,
    };
  }

  it('selects a cold persisted receipt in Basic and restores its identity without stale composer context', () => {
    const { component, taskPlans, assistant } = createComponent();
    taskPlans.logs.and.returnValue(of([persistedReceipt()]));
    component.ngOnInit();
    component.planForm.patchValue({ pursuitId: 'wrong-pursuit', automationId: 'old-automation', mandateId: 'old-mandate', includeRagflowCandidates: true });
    component.copyPlanToComposer(component.logs[0]);

    expect(component.isAdvancedView).toBeFalse();
    expect(component.uncertainCommand).toBeUndefined();
    expect(component.plan?.id).toBe('historical-task');
    expect(component.planForm.value).toEqual(jasmine.objectContaining({
      request: 'Collect evidence.', projectKey: '018-HAI', pursuitId: 'pursuit-1',
      automationId: '', mandateId: '', includeRagflowCandidates: false, successCriteria: 'Evidence linked',
    }));
    expect(component.currentPlanState()).toBe('Outcome uncertain');
    expect(component.executionSafetyNotice()).toContain('historical-hash');
    expect(component.executionSafetyNotice()).toContain('Completion is not confirmed');
    expect(component.canRunSafeSteps()).toBeFalse();
    component.runSuccessEngine();
    component.runAgentCycle();
    expect(assistant.command).not.toHaveBeenCalled();
  });

  it('blocks matching cold history even without selecting it or retaining an in-session failed command', () => {
    const { component, taskPlans, assistant } = createComponent();
    component.planForm.patchValue({ request: '  Collect\n evidence. ', pursuitId: 'pursuit-1' });
    taskPlans.logs.and.returnValue(of([persistedReceipt()]));
    component.ngOnInit();
    expect(component.planForm.value.pursuitId).toBe('pursuit-1');
    expect(component.isAdvancedView).toBeFalse();
    component.planForm.patchValue({ successCriteria: 'Different wording is not reconciliation', mandateId: 'new-mandate' });
    component.runAgentCycle();
    component.submitChat('run', { message: 'Collect evidence.', projectKey: '018-HAI', pursuitId: 'pursuit-1', executeAllowed: true });
    expect(component.executionSafetyPlan()?.id).toBe('historical-task');
    expect(assistant.command).not.toHaveBeenCalled();
  });

  it('honors an explicit route pursuit, including an explicit empty context', () => {
    for (const pursuitId of ['route-pursuit', '']) {
      const { component, taskPlans } = createComponent('', undefined, { pursuitId });
      component.planForm.patchValue({ request: 'Collect evidence.', pursuitId: 'pursuit-1' });
      taskPlans.logs.and.returnValue(of([persistedReceipt()]));
      component.ngOnInit();
      expect(component.planForm.value.pursuitId).toBe(pursuitId);
      expect(component.executionSafetyPlan()).toBeUndefined();
    }
  });

  it('does not apply a historical restriction to another project or pursuit', () => {
    for (const context of [{ projectKey: 'other-project', pursuitId: 'pursuit-1' }, { projectKey: '018-HAI', pursuitId: 'other-pursuit' }]) {
      const { component, taskPlans, assistant } = createComponent();
      taskPlans.logs.and.returnValue(of([persistedReceipt()]));
      component.ngOnInit();
      component.planForm.patchValue({ request: 'Collect evidence.', ...context });
      expect(component.executionSafetyPlan()).toBeUndefined();
      component.runAgentCycle();
      expect(assistant.command).toHaveBeenCalledTimes(1);
    }
  });

  it('allows a different reviewed command while retaining inspected diagnostics and blocks a return to the old work', () => {
    const { component, assistant } = createComponent();
    component.copyPlanToComposer(persistedReceipt());
    const selected = component.plan;
    const inspection = component.chatMessages[component.chatMessages.length - 1];
    component.planForm.patchValue({ request: 'Prepare a different draft.' });
    expect(component.executionSafetyPlan()).toBeUndefined();
    expect(component.plan).toBe(selected);
    expect(component.currentPlanState()).toBe('Outcome uncertain');
    expect(inspection.body).toContain('historical-hash');
    expect(inspection.bullets?.join(' ')).toContain('historical-launch');
    expect(component.canRunSafeSteps()).toBeFalse();
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [], plan: persistedReceipt({
      id: 'different-plan', request: 'Prepare a different draft.', executionResult: undefined,
      retryPolicy: { retryAvailable: true }, completionStatus: 'planned', validationResult: { passed: false },
    }) }));
    component.createPlan();
    component.showInspector('plan');
    expect(component.canRunSafeSteps()).toBeTrue();
    component.runSuccessEngine();
    expect(assistant.command).toHaveBeenCalledTimes(2);
    component.planForm.patchValue({ request: 'Collect evidence.' });
    expect(component.executionSafetyPlan()?.id).toBe('historical-task');
    component.runAgentCycle();
    expect(assistant.command).toHaveBeenCalledTimes(2);
    expect(component.chatMessages).toContain(inspection);
  });

  it('rebuilds restrictions from persisted history after reload and preserves the exact selected receipt during refresh', () => {
    const persisted = persistedReceipt();
    const first = createComponent();
    first.taskPlans.logs.and.returnValue(of([persisted]));
    first.component.ngOnInit();
    first.component.copyPlanToComposer(first.component.logs[0]);
    const reloaded = createComponent();
    reloaded.taskPlans.logs.and.returnValue(of([JSON.parse(JSON.stringify(persisted))]));
    reloaded.component.ngOnInit();
    reloaded.component.copyPlanToComposer(reloaded.component.logs[0]);
    const selected = reloaded.component.plan;
    reloaded.taskPlans.logs.and.returnValue(of([persistedReceipt({ executionResult: undefined, retryPolicy: { retryAvailable: true } })]));
    reloaded.component.loadLogs();
    expect(reloaded.component.plan).toBe(selected);
    expect(reloaded.component.executionSafetyPlan()).toBe(selected);
    reloaded.taskPlans.logs.and.returnValue(of([]));
    reloaded.component.loadLogs();
    reloaded.taskPlans.logs.and.returnValue(throwError(() => new Error('history unavailable')));
    reloaded.component.loadLogs();
    expect(reloaded.component.plan).toBe(selected);
    expect(reloaded.component.executionSafetyNotice()).toContain('historical-hash');
    reloaded.component.runAgentCycle();
    expect(reloaded.assistant.command).not.toHaveBeenCalled();
  });

  it('keeps Run and cycle handlers paused while cold history is pending or malformed', () => {
    const { component, taskPlans, assistant } = createComponent();
    const history = new Subject<any>();
    taskPlans.logs.and.returnValue(history);
    component.planForm.patchValue({ request: 'Collect evidence.', pursuitId: 'pursuit-1' });
    component.ngOnInit();
    component.runAgentCycle();
    component.runSuccessEngine();
    expect(assistant.command).not.toHaveBeenCalled();
    history.next({ items: [] });
    history.complete();
    component.runAgentCycle();
    expect(component.logsUnavailable).toBeTrue();
    expect(assistant.command).not.toHaveBeenCalled();
    taskPlans.logs.and.returnValue(of([persistedReceipt()]));
    component.loadLogs();
    component.runAgentCycle();
    expect(component.executionSafetyPlan()?.id).toBe('historical-task');
    expect(assistant.command).not.toHaveBeenCalled();
  });

  it('fails closed for malformed persisted outcome data without claiming validated completion', () => {
    const malformedResults = [
      false, 'not-an-object', { outcomeUncertain: 'false' }, { reconciliationRequired: 'false' },
      { interrupted: true }, { outcomeRecorded: false }, { receipt: 'broken' }, { receipt: { ok: true } },
      { actions: {} }, { actions: [null] }, { toolExecution: false },
      { toolExecution: { status: 123 } }, { toolExecution: { status: 'completed', requiresApproval: 'false' } },
      { toolExecution: { status: 'completed', receipt: [] } },
    ];
    for (const executionResult of malformedResults) {
      const { component, assistant } = createComponent();
      expect(() => component.copyPlanToComposer(persistedReceipt({ executionResult, retryPolicy: { retryAvailable: true } }))).not.toThrow();
      expect(component.currentPlanState()).withContext(JSON.stringify(executionResult)).toBe('Outcome uncertain');
      expect(component.executionSafetyNotice()).toContain('Completion is not confirmed');
      component.runAgentCycle();
      expect(assistant.command).not.toHaveBeenCalled();
    }
  });

  it('preserves selected evidence when refreshed history has malformed identities or entries', () => {
    for (const history of [[null], ['invalid'], [{}], [persistedReceipt({ projectKey: [] })], [persistedReceipt({ pursuitId: {} })]]) {
      const { component, taskPlans, assistant } = createComponent();
      taskPlans.logs.and.returnValue(of([persistedReceipt()]));
      component.loadLogs();
      component.copyPlanToComposer(component.logs[0]);
      const selected = component.plan;
      const previousLogs = component.logs;
      taskPlans.logs.and.returnValue(of(history));
      expect(() => component.loadLogs()).not.toThrow();
      expect(component.logsUnavailable).toBeTrue();
      expect(component.logs).toBe(previousLogs);
      expect(component.plan).toBe(selected);
      expect(component.executionSafetyNotice()).toContain('historical-hash');
      component.runAgentCycle();
      expect(assistant.command).not.toHaveBeenCalled();
    }
  });

  it('uses shared partial-effect receipt evidence even when legacy completion flags claim success', () => {
    const { component, assistant } = createComponent();
    const receipt = persistedReceipt().executionResult.toolExecution.receipt;
    receipt.output.progress = {
      authorization: 'consumed', effectStarted: true, artifactCreated: true, bytesWritten: 5,
      writeComplete: false, syncComplete: false, readBytes: 0, readComplete: false, identityVerified: false, fileClosed: false,
    };
    component.copyPlanToComposer(persistedReceipt({ retryPolicy: { retryAvailable: true }, executionResult: {
      outcomeUncertain: false, toolExecution: { status: 'completed', outcomeUncertain: false, receipt },
    } }));
    expect(component.currentPlanState()).toBe('Outcome uncertain');
    expect(component.executionSafetyNotice()).toContain('Execution authorization consumed');
    expect(component.executionSafetyNotice()).toContain('5 bytes written and 0 bytes read');
    component.runAgentCycle();
    expect(assistant.command).not.toHaveBeenCalled();
  });

  it('retains bounded redacted shared receipt output and runtime diagnostics without changing source evidence', () => {
    const { component } = createComponent();
    const persisted = persistedReceipt();
    persisted.executionResult.toolExecution.message = 'password=do-not-expose ' + 'm'.repeat(900);
    persisted.executionResult.toolExecution.receipt.output.boundedOutput = 'token=do-not-expose ' + 'x'.repeat(900);
    const original = JSON.stringify(persisted.executionResult);
    component.copyPlanToComposer(persisted);
    const tool = component.plan?.executionResult?.toolExecution as any;
    expect(tool.message.length).toBeLessThanOrEqual(512);
    expect(tool.receipt.output.boundedOutput.length).toBeLessThanOrEqual(512);
    expect(component.executionSafetyNotice()).toContain('[redacted]');
    expect(component.executionSafetyNotice()).not.toContain('do-not-expose');
    expect(tool.message).not.toContain('do-not-expose');
    expect(tool.launchEventId).toBe('historical-launch');
    expect(JSON.stringify(persisted.executionResult)).toBe(original);
  });

  it('keeps malformed retry policy paused without relabeling a known pre-dispatch block as partial execution', () => {
    for (const retryPolicy of ['invalid', { retryAvailable: 'false' }]) {
      const { component, assistant } = createComponent();
      component.copyPlanToComposer(persistedReceipt({ executionResult: { mode: 'blocked', toolExecution: { status: 'blocked' } }, retryPolicy }));
      expect(component.hasUncertainExecution()).toBeFalse();
      expect(component.executionSafetyNotice()).toContain('Backend retry policy does not permit another attempt');
      component.runAgentCycle();
      expect(assistant.command).not.toHaveBeenCalled();
    }
  });

	it('remembers Advanced mode for this module without changing another module', () => {
		const { component, preferences } = createComponent();

		expect(component.isAdvancedView).toBeFalse();
		component.setViewMode('advanced');
		expect(component.isAdvancedView).toBeTrue();

		const restored = new ModuleViewPreferencesService(document);
		expect(restored.get('task-blueprint').mode).toBe('advanced');
		expect(restored.get('pursuits').mode).toBe('basic');
		expect(createComponent('', restored).component.isAdvancedView).toBeTrue();
		component.setViewMode('basic');
		expect(preferences.get('task-blueprint').mode).toBe('basic');
	});

	it('persists Task Context and Inspector disclosure state independently', () => {
		const { component, preferences } = createComponent();

		component.toggleTaskContext();
		component.showInspector('logs');

		expect(preferences.get('task-blueprint').openSections['task-context']).toBeTrue();
		expect(preferences.get('task-blueprint').openSections['task-inspector']).toBeTrue();
		expect(component.inspectorMode).toBe('logs');
		component.toggleTaskContext();
		expect(preferences.get('task-blueprint').openSections['task-context']).toBeFalse();
		expect(preferences.get('task-blueprint').openSections['task-inspector']).toBeTrue();
	});

	it('keeps Plan first on the real plan-only command path and does not imply task completion', () => {
		const { component, assistant } = createComponent();
		component.planForm.patchValue({ request: 'Prepare a source-linked project update.' });

		component.createPlan();

		expect(assistant.command).toHaveBeenCalledWith(jasmine.objectContaining({
			message: 'Prepare a source-linked project update.',
			executeAllowed: false,
			runCycle: false,
		}));
		expect(component.chatMessages[component.chatMessages.length - 1].title).toBe('Plan prepared');
		expect(component.chatMessages[component.chatMessages.length - 1].status).toBe('neutral');
	});

	it('requires a current reviewed plan before running and does not call an unvalidated result complete', () => {
		const { component, assistant, notifications } = createComponent();
		component.planForm.patchValue({ request: 'Prepare a source-linked project update.' });
		const plan = {
			id: 'plan-1', request: 'Prepare a source-linked project update.', projectKey: '018-HAI',
			realGoal: 'Prepare a source-linked project update.', completionStatus: 'planned',
			steps: [{ id: 'step-1', name: 'Prepare draft', purpose: 'Create the draft', allowed: true, requiresApproval: false, status: 'ready' }],
			riskAssessment: { level: 'low', approvalRequired: false, approvalGranted: false, actionResolution: 'proceed', reasons: [], allowedNow: true },
			intake: { riskLevel: 'low', successCriteria: [] }, contextPlan: { usedContext: [], sourceContext: [] },
			modelDecision: { selectedModelName: 'Local model', tier: 'standard' }, toolDecision: {}, minimalityDecision: {},
			validationPlan: { steps: [], successCriteria: [], frameworkEvidenceRequirements: [], frameworkCompletionCriteria: [], frameworkAssuranceCriteria: [] },
			validationResult: { passed: false, status: 'not_run', checked: [], failures: [], criteria: [], nextAction: 'Review the plan.' },
			executionPlan: { approvalRequiredFor: [], auditEvents: [] }, retryPolicy: {}, memoryUpdateProposals: [], lessonsLearned: [], storedMemoryIds: [], events: [],
		} as any;
		assistant.command.and.returnValues(
			of({ id: 'plan-command', createdAt: '', intent: 'plan', summary: 'Plan prepared.', nextAction: 'Review it.', safetySummary: 'Plan only.', actions: [], reviewRequired: false, plan }),
			of({ id: 'run-command', createdAt: '', intent: 'run', summary: 'Run result received.', nextAction: 'Review validation.', safetySummary: 'Governed.', actions: [], reviewRequired: false,
				plan: { ...plan, completionStatus: 'needs_review', validationResult: { passed: false, status: 'failed', checked: [], failures: ['Validation incomplete'], criteria: [], nextAction: 'Inspect the run.' } } } as any),
		);

		component.runSuccessEngine();
		expect(assistant.command).not.toHaveBeenCalled();
		expect(notifications.warning).toHaveBeenCalled();
		component.createPlan();
		expect(component.canRunSafeSteps()).toBeFalse();
		component.showInspector('plan');
		expect(component.canRunSafeSteps()).toBeTrue();
		component.plan!.riskAssessment.missingRequiredAgents = ['evidence_reviewer', 'red_team_reviewer'];
		expect(component.canRunSafeSteps()).toBeFalse();
		expect(component.runActionHint()).toContain('evidence_reviewer, red_team_reviewer');
		component.runSuccessEngine();
		expect(assistant.command).toHaveBeenCalledTimes(1);
		component.plan!.riskAssessment.missingRequiredAgents = [];
		expect(component.canRunSafeSteps()).toBeTrue();
		component.runSuccessEngine();
		expect(assistant.command).toHaveBeenCalledTimes(2);
		expect(assistant.command.calls.mostRecent().args[0]).toEqual(jasmine.objectContaining({ executeAllowed: true }));

		const result = component.chatMessages[component.chatMessages.length - 1];
		expect(result.title).toBe('Run result received');
		expect(result.status).toBe('neutral');
	});

  it('treats uncertain tool statuses conservatively without relabeling pre-dispatch blocks', () => {
    const { component } = createComponent();
    for (const status of ['failed', 'indeterminate', 'pending', 'running', 'unknown', 'new_status', '', '   ']) {
      expect(component.hasUncertainExecution({ executionResult: { toolExecution: { status } } } as any))
        .withContext(status || 'empty status').toBeTrue();
    }
    for (const status of ['completed', 'blocked', 'needs_approval']) {
      const plan = { executionResult: { toolExecution: { status, outcomeUncertain: false } } } as any;
      expect(component.hasUncertainExecution(plan)).withContext(status).toBeFalse();
      plan.executionResult.toolExecution.outcomeUncertain = true;
      expect(component.hasUncertainExecution(plan)).withContext(`${status} with uncertainty`).toBeTrue();
    }
    expect(component.hasUncertainExecution({ executionResult: { outcomeUncertain: true } } as any)).toBeTrue();
    expect(component.hasUncertainExecution({ executionResult: { toolExecution: { status: 'completed', requiresApproval: true } } } as any)).toBeTrue();
    expect(component.hasUncertainExecution({ executionResult: { actions: [{ name: 'automation.launch', status: 'indeterminate' }] } } as any)).toBeTrue();
    expect(component.hasUncertainExecution({ executionResult: { mode: 'blocked' } } as any)).toBeFalse();
    expect(component.hasUncertainExecution({} as any)).toBeFalse();
  });

  it('never calls an explicitly uncertain completed receipt validated or safe to repeat', () => {
    const { component, assistant } = createComponent();
    component.planForm.patchValue({ request: 'Prepare a draft.' });
    assistant.command.and.returnValue(of({
      intent: 'plan', summary: 'Completed. No external action occurred.', actions: [], reviewRequired: false,
      plan: {
        id: 'uncertain-plan', request: 'Prepare a draft.', completionStatus: 'validated',
        steps: [{ allowed: true, requiresApproval: false }], riskAssessment: {}, validationResult: { passed: true },
        retryPolicy: { retryAvailable: true },
        executionResult: { mode: 'action', toolExecution: { status: 'completed', outcomeUncertain: true, launchEventId: 'receipt-1' } },
      },
    } as any));
    component.createPlan();
    component.showInspector('plan');
    expect(component.plan!.executionResult!.toolExecution!.status).toBe('completed');
    expect(component.currentPlanState()).toBe('Outcome uncertain');
    expect(component.toolExecutionStatusLabel(component.plan!.executionResult!.toolExecution!)).toContain('completed; outcome uncertain');
    expect(component.canRunSafeSteps()).toBeFalse();
    expect(component.nextDecisionLabel()).toContain('Partial effects may have occurred');
    const message = component.chatMessages[component.chatMessages.length - 1];
    expect(message.title).toBe('Execution outcome uncertain');
    expect(message.status).toBe('warning');
    expect(message.body).not.toContain('No external action');
    component.runAgentCycle();
    component.submitChat('run', { message: 'Prepare a draft.', executeAllowed: true });
    expect(assistant.command).toHaveBeenCalledTimes(1);
    component.clearChat();
    expect(component.executionSafetyNotice()).toContain('Partial effects may have occurred');
    component.planForm.patchValue({ request: 'Prepare a draft.' });
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [], plan: {
      id: 'new-plan', request: 'Prepare a draft.', retryPolicy: { retryAvailable: false },
      executionResult: { mode: 'blocked', toolExecution: { status: 'blocked' } },
    } } as any));
    component.createPlan();
    expect(component.executionSafetyNotice()).toContain('Partial effects may have occurred');
  });

  it('blocks another attempt when the backend disables retries without inferring partial effects', () => {
    const { component, assistant } = createComponent();
    component.planForm.patchValue({ request: 'Read metadata.' });
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [], plan: {
      steps: [{ allowed: true, requiresApproval: false }], riskAssessment: {},
      executionResult: { mode: 'blocked', toolExecution: { status: 'blocked' } },
      retryPolicy: { retryAvailable: false },
    } } as any));
    component.createPlan();
    component.showInspector('plan');
    expect(component.hasUncertainExecution()).toBeFalse();
    expect(component.canRunSafeSteps()).toBeFalse();
    expect(component.runActionHint()).toContain('Backend retry policy does not permit another attempt');
    component.runAgentCycle();
    expect(assistant.command).toHaveBeenCalledTimes(1);
  });

  it('retains a lost-response guard after planning and refuses a retry for an uncertain history receipt', () => {
    const { component, assistant, taskPlans, modal } = createComponent();
    component.planForm.patchValue({ request: 'Collect evidence.', pursuitId: 'pursuit-1' });
    assistant.command.and.returnValue(throwError(() => new Error('lost receipt')));
    component.runAgentCycle();
    const retained = component.uncertainCommand;
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [] }));
    component.createPlan();
    expect(component.uncertainCommand).toBe(retained);
    taskPlans.logs.and.returnValue(of([{
      id: 'receipt-plan', request: 'Collect evidence.', projectKey: '018-HAI', pursuitId: 'pursuit-1',
      executionResult: { toolExecution: { status: 'completed', outcomeUncertain: true, launchEventId: 'lost-receipt' } },
      retryPolicy: { retryAvailable: true },
    }]));
    component.refreshHistoryBeforeUncertainRetry();
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.executionSafetyPlan()?.executionResult?.toolExecution?.launchEventId).toBe('lost-receipt');
    component.submitChat('cycle', retained!.request);
    expect(assistant.command).toHaveBeenCalledTimes(2);
  });

  it('does not open a lost-response retry confirmation when matching history says retry unavailable', () => {
    const { component, assistant, taskPlans, modal } = createComponent();
    component.planForm.patchValue({ request: 'Collect evidence.' });
    assistant.command.and.returnValue(throwError(() => new Error('lost receipt')));
    component.runAgentCycle();
    taskPlans.logs.and.returnValue(of([{
      id: 'receipt-plan', request: 'Collect evidence.', projectKey: '018-HAI',
      executionResult: { toolExecution: { status: 'blocked' } }, retryPolicy: { retryAvailable: false },
    }]));
    component.refreshHistoryBeforeUncertainRetry();
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.uncertainCommand).toBeDefined();
  });

  it('surfaces a restricted receipt from automatic history refresh after a lost response', () => {
    const { component, assistant, taskPlans } = createComponent();
    component.planForm.patchValue({ request: 'Collect evidence.' });
    taskPlans.logs.and.returnValue(of([{
      id: 'receipt-plan', request: 'Collect evidence.', projectKey: '018-HAI',
      executionResult: { outcomeUncertain: true }, retryPolicy: { retryAvailable: false },
    }]));
    assistant.command.and.returnValue(throwError(() => new Error('lost receipt')));
    component.runAgentCycle();
    expect(component.executionSafetyNotice()).toContain('Partial effects may have occurred');
    expect(component.canRetryUncertainCommand()).toBeFalse();
  });

  it('does not apply a different pursuit receipt to the retained request', () => {
    const { component, assistant, taskPlans, modal } = createComponent();
    component.planForm.patchValue({ request: 'Collect evidence.', pursuitId: 'pursuit-current' });
    assistant.command.and.returnValue(throwError(() => new Error('lost receipt')));
    component.runAgentCycle();
    taskPlans.logs.and.returnValue(of([{
      request: 'Collect evidence.', projectKey: '018-HAI', pursuitId: 'pursuit-other',
      executionResult: { outcomeUncertain: true }, retryPolicy: { retryAvailable: false },
    }]));
    component.refreshHistoryBeforeUncertainRetry();
    expect(modal.confirm).toHaveBeenCalled();
  });

  it('rechecks operation receipt uncertainty before confirming a queued review retry', () => {
    const { component, taskPlans, modal } = createComponent();
    const item = { id: 'review-op', taskId: 'operation:op-1', status: 'needs_review', request: { request: 'Collect evidence.' } } as any;
    component.resolveReviewItem(item, true);
    expect(modal.confirm).toHaveBeenCalled();
    component.logs = [{ id: 'plan-1', operationId: 'op-1', executionResult: { outcomeUncertain: true } } as any];
    (modal.confirm.calls.mostRecent().args[0]!.nzOnOk as () => void)();
    expect(taskPlans.resolveReviewItem).not.toHaveBeenCalled();
  });

  it('honors backend retry false even when no execution receipt was returned', () => {
    const { component } = createComponent();
    component.plan = { retryPolicy: { retryAvailable: false } } as any;
    expect(component.executionSafetyNotice()).toContain('Backend retry policy does not permit another attempt');
    expect(component.hasUncertainExecution()).toBeFalse();
  });

  it('rechecks retry restrictions when a previously opened confirmation is accepted', () => {
    const { component, assistant, modal } = createComponent();
    component.planForm.patchValue({ request: 'Collect evidence.' });
    assistant.command.and.returnValue(throwError(() => new Error('lost receipt')));
    component.runAgentCycle();
    component.refreshHistoryBeforeUncertainRetry();
    expect(modal.confirm).toHaveBeenCalled();
    component.logs = [{ request: 'Collect evidence.', projectKey: '018-HAI',
      executionResult: { outcomeUncertain: true }, retryPolicy: { retryAvailable: false } } as any];
    (modal.confirm.calls.mostRecent().args[0]!.nzOnOk as () => void)();
    expect(assistant.command).toHaveBeenCalledTimes(1);
  });

  it('keeps restricted review items inspectable and rejectable but not runnable', () => {
    const { component, taskPlans, modal } = createComponent();
    const item = { id: 'review-uncertain', taskId: 'operation:op-1', status: 'needs_review', request: { request: 'Collect evidence.' } } as any;
    component.logs = [{ id: 'plan-1', operationId: 'op-1',
      executionResult: { outcomeUncertain: true }, retryPolicy: { retryAvailable: false } } as any];
    expect(component.canResolveReview(item)).toBeTrue();
    expect(component.canApproveReview(item)).toBeFalse();
    component.resolveReviewItem(item, true);
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(taskPlans.resolveReviewItem).not.toHaveBeenCalled();
    taskPlans.resolveReviewItem.and.returnValue(of({ item: { ...item, status: 'rejected' } }));
    component.resolveReviewItem(item, false);
    expect(taskPlans.resolveReviewItem).toHaveBeenCalled();
  });

  it('does not announce approval completion for a validated plan with uncertain effects', () => {
    const { component, taskPlans, notifications } = createComponent();
    const item = { id: 'review-1', taskId: 'plan-1', status: 'open', request: { request: 'Collect evidence.' } } as any;
    taskPlans.resolveReviewItem.and.returnValue(of({ item: { ...item, status: 'approved' }, plan: {
      id: 'plan-1', completionStatus: 'validated', validationResult: { passed: true },
      executionResult: { outcomeUncertain: true },
    } }));
    component.resolveReviewItem(item, true);
    expect(notifications.success).not.toHaveBeenCalled();
    expect(component.chatMessages[component.chatMessages.length - 1].status).toBe('warning');
    expect(component.chatMessages[component.chatMessages.length - 1].body).not.toContain('task validated');
  });

  it('uses warning tones for unknown, pending and missing statuses', () => {
    const { component } = createComponent();
    for (const status of ['Outcome uncertain', 'indeterminate', 'pending', 'unrecognized', '']) {
      expect(component.statusTone(status)).withContext(status).not.toBe('green');
    }
    expect(component.statusTone('validated')).toBe('green');
  });

  it('labels absent model routing without inventing a model or automatic tier', () => {
    const { component } = createComponent();
    expect(component.modelSelectionLabel()).toBe('No routing result yet');
    expect(component.planPreflightHints()).toEqual([]);
    component.plan = { modelDecision: {}, riskAssessment: {} } as any;
    expect(component.modelSelectionLabel()).toBe('No model selected');
    expect(component.planPreflightHints().join(' ')).toContain('No model route is selected');
    expect(component.planPreflightHints().join(' ')).toContain('No operating framework selection is recorded');
    component.plan!.modelDecision.selectedModelId = 'configured-model';
    expect(component.modelSelectionLabel()).toBe('configured-model');
    expect(component.planPreflightHints().join(' ')).not.toContain('No model route is selected');
  });

  it('does not mistake selected frameworks or a model route for verified runtime readiness', () => {
    const { component } = createComponent();
    component.plan = {
      modelDecision: { selectedModelId: 'configured-model', selectedProviderId: 'configured-provider' },
      frameworkDecision: { selected: [{ id: 'framework-1' }] },
      riskAssessment: { frameworkAutonomyCeiling: 4, requiredFrameworkAutonomy: 8, approvalRequired: true, approvalGranted: true },
    } as any;
    expect(component.planPreflightHints()).toEqual([
      'Framework authority level 4 is below required level 8. Re-plan with suitable authority; approval does not raise the ceiling.',
    ]);
    expect(component.riskGateHint()).toContain('does not override policy or runtime prerequisites');
    expect(component.canRunSafeSteps()).toBeFalse();
  });

  it('prioritizes risk blocks and clarification over approval labels', () => {
    const { component } = createComponent();
    component.plan = { riskAssessment: { actionResolution: 'block', approvalRequired: true, approvalGranted: true } } as any;
    expect(component.riskGateHint()).toContain('Execution blocked by risk policy');
    component.plan!.riskAssessment.actionResolution = 'clarify';
    expect(component.riskGateHint()).toContain('More execution detail is needed');
    component.plan!.riskAssessment.actionResolution = 'proceed';
    component.plan!.riskAssessment.approvalGranted = false;
    expect(component.riskGateHint()).toContain('Approval required before risky execution');
  });

  it('keeps all missing participants gated after review and approval, with actionable detail', () => {
    const { component, assistant } = createComponent();
    const request = 'Prepare an evidence-backed draft.';
    component.planForm.patchValue({ request });
    assistant.command.and.returnValue(of({
      intent: 'plan', actions: [], reviewRequired: false,
      plan: {
        request, steps: [{ id: 'draft', allowed: true, requiresApproval: false }],
        riskAssessment: {
          actionResolution: 'proceed', approvalRequired: true, approvalGranted: true,
          missingRequiredAgents: ['evidence_reviewer', 'red_team_reviewer'],
          missingParameters: ['controlled automation'],
        },
      },
    } as any));
    component.createPlan();
    component.showInspector('plan');
    expect(component.canRunSafeSteps()).toBeFalse();
    expect(component.planPreflightHints().join(' ')).toContain('evidence_reviewer, red_team_reviewer');
    expect(component.planPreflightHints().join(' ')).toContain('Missing execution details: controlled automation');
    expect(component.riskGateHint()).toContain('approval alone is not sufficient');
    component.plan!.riskAssessment.missingRequiredAgents = ['red_team_reviewer'];
    expect(component.canRunSafeSteps()).toBeFalse();
    component.runSuccessEngine();
    expect(assistant.command).toHaveBeenCalledTimes(1);
    component.plan!.riskAssessment.missingRequiredAgents = [];
    component.plan!.riskAssessment.actionResolution = 'block';
    expect(component.canRunSafeSteps()).toBeFalse();
    expect(component.runActionHint()).toContain('Execution blocked by risk policy');
    expect(component.runActionHint()).toContain('controlled automation');
  });

  it('describes a reviewed plan with incomplete routing as advisory, not ready', () => {
    const { component, assistant } = createComponent();
    component.planForm.patchValue({ request: 'Prepare a draft.' });
    assistant.command.and.returnValue(of({
      intent: 'plan', actions: [], reviewRequired: false,
      plan: { steps: [{ allowed: true, requiresApproval: false }], riskAssessment: { actionResolution: 'proceed' } },
    } as any));
    component.createPlan();
    expect(component.runActionHint()).not.toContain('Plan ready');
    expect(component.runActionHint()).toContain('No model route is selected');
    expect(component.runActionHint()).toContain('missing model alone does not establish that they are blocked');
    component.showInspector('plan');
    expect(component.runActionHint()).toContain('runtime prerequisites');
    expect(component.canRunSafeSteps()).toBeTrue();
    expect(component.plan!.modelDecision.selectedModelId).toBeUndefined();
    expect(component.plan!.riskAssessment.allowedNow).toBeUndefined();
  });

  it('uses the actual backend execution block instead of suggesting another approval', () => {
    const { component, assistant } = createComponent();
    component.planForm.patchValue({ request: 'Prepare a draft.' });
    assistant.command.and.returnValue(of({
      intent: 'plan', actions: [], reviewRequired: false, nextAction: 'Approve the plan.',
      plan: {
        completionStatus: 'review_required',
        riskAssessment: { approvalRequired: false },
        executionResult: { mode: 'blocked', blockedReason: 'No capable model is available; configure an eligible route and re-plan.' },
      },
    } as any));
    component.createPlan();
    expect(component.nextDecisionLabel()).toBe('Recorded execution block: No capable model is available; configure an eligible route and re-plan.');
    expect(component.riskGateHint()).toBe(component.nextDecisionLabel());
    expect(component.planPreflightHints()[0]).toBe(component.nextDecisionLabel());
    expect(component.chatMessages[component.chatMessages.length - 1].status).toBe('warning');
    component.plan!.executionResult!.mode = 'action';
    expect(component.recordedExecutionBlockHint()).toBe('');
    expect(component.nextDecisionLabel()).toBe('Approve the plan.');
  });

	it('forwards an explicitly selected standing mandate without treating it as approval', () => {
		const mandateId = 'f4d7cf86-8902-4e24-b704-edca66e31f22';
		const { component, assistant } = createComponent();
		component.planForm.patchValue({
			request: 'Prepare the project status summary.',
			mandateId,
		});

		component.submitChat('plan');

		expect(assistant.command).toHaveBeenCalledWith(jasmine.objectContaining({
			message: 'Prepare the project status summary.',
			mandateId,
			executeAllowed: false,
		}));
	});

  it('shows query-scoped pursuit context and opens its detail view', () => {
    const pursuitId = '3ca4a3b5-84b2-4fcd-ae8d-e9f337e7250b';
    const { component, router } = createComponent(pursuitId);

    component.ngOnInit();
    component.openPursuitContext();

    expect(component.planForm.value.pursuitId).toBe(pursuitId);
    expect(component.contextExpanded).toBeTrue();
    expect(new ModuleViewPreferencesService(document).get('task-blueprint').openSections['task-context']).toBeTrue();
    expect(component.pursuitContextLabel()).toBe('Selected pursuit');
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: pursuitId } });
  });

  it('opens advanced context instead of navigating when no pursuit is selected', () => {
    const { component, router } = createComponent();

    component.openPursuitContext();

    expect(component.contextExpanded).toBeTrue();
    expect(component.pursuitContextLabel()).toBe('No pursuit selected');
    expect(router.navigate).not.toHaveBeenCalled();
  });

  it('opens the framework registry from the plan inspector', () => {
    const { component, router } = createComponent();

    component.openFrameworkRegistry();

    expect(router.navigate).toHaveBeenCalledWith(['/framework-registry']);
  });

  it('keeps the framework summary concise while preserving the total count', () => {
    const { component } = createComponent();
    const decision = {
      selected: [
        { id: 'human-sovereignty', name: 'Human Sovereignty' },
        { id: 'intake-triage', name: 'Intake and Triage' },
        { id: 'approval', name: 'Approval Governance' },
        { id: 'truth', name: 'Truth and Evidence' },
        { id: 'privacy', name: 'Privacy and Security' },
      ],
    } as IFrameworkSelectionDecision;

    expect(component.visibleFrameworks(decision).map((framework) => framework.id)).toEqual([
      'human-sovereignty',
      'intake-triage',
      'approval',
    ]);
    expect(component.additionalFrameworkCount(decision)).toBe(2);
    expect(component.visibleFrameworks(undefined)).toEqual([]);
    expect(component.additionalFrameworkCount(undefined)).toBe(0);
  });

  it('summarizes real validation results and keeps unrecorded planned gates not run', () => {
    const { component } = createComponent();
    component.plan = {
      validationPlan: {
        steps: ['Run deterministic checks'],
        successCriteria: ['Deliverable exists', 'No unsupported claims'],
        frameworkEvidenceRequirements: ['Record source evidence'],
        frameworkCompletionCriteria: ['Record approval outcome'],
        frameworkAssuranceCriteria: ['Measure framework outcomes longitudinally'],
        failurePolicy: 'Escalate failed gates',
        completionGate: 'Every gate must pass',
      },
      validationResult: {
        passed: false,
        status: 'failed',
        checked: ['Deliverable exists'],
        failures: ['Record source evidence'],
        criteria: [
          {
            criterion: 'Deliverable exists',
            kind: 'task_success',
            status: 'passed',
            evidence: ['artifact://deliverable'],
          },
          {
            criterion: 'Record source evidence',
            kind: 'framework_evidence',
            status: 'failed',
            evidence: [],
            failure: 'No source evidence was recorded.',
          },
          {
            criterion: 'Measure framework outcomes longitudinally',
            kind: 'framework_assurance',
            status: 'not_applicable',
            evidence: ['framework-registry://evaluation'],
            applicabilityReason:
              'Evaluated by registry assurance and longitudinal evaluation, not by each task run.',
          },
        ],
        nextAction: 'Collect evidence',
        attemptNumber: 1,
      },
    } as any;

    const criteria = component.structuredValidationCriteria();

    expect(criteria.map((criterion) => criterion.criterion)).toEqual([
      'Deliverable exists',
      'Record source evidence',
      'Measure framework outcomes longitudinally',
      'No unsupported claims',
      'Record approval outcome',
    ]);
    expect(component.validationCount('passed')).toBe(1);
    expect(component.validationCount('failed')).toBe(1);
    expect(component.validationCount('not_run')).toBe(2);
    expect(component.validationCount('not_applicable')).toBe(1);
    expect(component.validationStatusLabel()).toBe('Failed');
    expect(component.validationStatusClass()).toBe('validation-state--failed');
  });

  it('distinguishes complete, partial, and absent validation without inventing evidence', () => {
    const { component } = createComponent();

    expect(component.validationStatusLabel()).toBe('Not run');
    expect(component.structuredValidationCriteria()).toEqual([]);

    component.plan = {
      validationPlan: {
        steps: [],
        successCriteria: ['Result is verified'],
        frameworkEvidenceRequirements: [],
        frameworkCompletionCriteria: [],
        frameworkAssuranceCriteria: [],
        failurePolicy: 'Retry',
        completionGate: 'All pass',
      },
      validationResult: {
        passed: false,
        status: 'pending',
        checked: [],
        failures: [],
        criteria: [],
        nextAction: 'Run validation',
        attemptNumber: 0,
      },
    } as any;

    expect(component.validationStatusLabel()).toBe('Not run');
    expect(component.structuredValidationCriteria()[0].evidence).toEqual([]);

    component.plan!.validationResult.criteria = [
      {
        criterion: 'Result is verified',
        kind: 'task_success',
        status: 'passed',
        evidence: ['verification://result'],
      },
      {
        criterion: 'Run deterministic checks',
        kind: 'system_check',
        status: 'not_run',
        evidence: [],
      },
    ];

    expect(component.validationStatusLabel()).toBe('Not fully run');

    component.plan!.validationResult.criteria[1].status = 'passed';
    component.plan!.validationResult.criteria[1].evidence = ['test://suite'];

    expect(component.validationStatusLabel()).toBe('Passed');
    expect(component.validationStatusClass()).toBe('validation-state--passed');
  });

  it('opens the exact validation evidence from the basic summary', () => {
    const { component } = createComponent();

    component.inspectorMode = 'overview';
    component.openValidationEvidence();

    expect(component.inspectorMode).toBe('evidence');
    expect(component.validationKindLabel('task_success')).toBe('Task success');
    expect(component.validationKindLabel('framework_evidence')).toBe('Framework evidence');
    expect(component.validationKindLabel('framework_completion')).toBe('Framework completion');
    expect(component.validationKindLabel('system_check')).toBe('System check');
  });

	it('keeps retry review cycles actionable and counts only unresolved decisions', () => {
		const { component } = createComponent();
		component.reviewQueue = [
			{ id: 'open', taskId: 'a', request: { request: 'a' }, reason: 'a', priority: 'normal', status: 'open', createdAt: '' },
			{ id: 'retry', taskId: 'b', request: { request: 'b' }, reason: 'b', priority: 'normal', status: 'needs_review', createdAt: '' },
			{ id: 'approved', taskId: 'c', request: { request: 'c' }, reason: 'c', priority: 'normal', status: 'approved', createdAt: '' },
		];

		expect(component.reviewQueueOpenCount()).toBe(2);
		expect(component.pendingReviewItems().map((item) => item.id)).toEqual(['open', 'retry']);
		expect(component.canResolveReview(component.reviewQueue[1])).toBeTrue();
		expect(component.canResolveReview(component.reviewQueue[2])).toBeFalse();
	});

	it('holds an uncertain run until fresh history and explicit confirmation, then reuses its exact request', () => {
		const { component, assistant, taskPlans, modal } = createComponent();
		const plan = {
			id: 'plan-1', request: 'Prepare the evidence bundle.', projectKey: '018-HAI',
			realGoal: 'Prepare the evidence bundle.', completionStatus: 'planned',
			steps: [{ id: 'step-1', name: 'Collect evidence', purpose: 'Prepare the bundle', allowed: true, requiresApproval: false, status: 'ready' }],
			riskAssessment: { level: 'low', approvalRequired: false, approvalGranted: false, actionResolution: 'proceed', reasons: [], allowedNow: true },
			intake: { riskLevel: 'low', successCriteria: [] }, contextPlan: { usedContext: [], sourceContext: [] },
			modelDecision: {}, toolDecision: {}, minimalityDecision: {}, validationPlan: {}, validationResult: {}, executionPlan: {}, retryPolicy: {},
			memoryUpdateProposals: [], lessonsLearned: [], storedMemoryIds: [], events: [],
		} as any;
		const planResult = { id: 'plan-command', createdAt: '', intent: 'plan', summary: 'Plan prepared.', nextAction: 'Review it.', safetySummary: 'Plan only.', actions: [], reviewRequired: false, plan };
		const result = {
			id: 'command-2', createdAt: '2026-09-24T00:00:00Z', intent: 'run', summary: 'Result received.',
			nextAction: 'Review the result.', safetySummary: 'Governed.', actions: [], reviewRequired: false,
		};
		assistant.command.and.returnValues(of(planResult), throwError(() => new Error('timeout')), of(result));
		component.planForm.patchValue({
			request: 'Prepare the evidence bundle.', projectKey: '018-HAI', successCriteria: 'Evidence linked',
		});
		component.createPlan();
		component.showInspector('plan');

		component.runSuccessEngine();
		const retained = component.uncertainCommand;
		expect(retained?.request).toEqual(jasmine.objectContaining({
			message: 'Prepare the evidence bundle.', projectKey: '018-HAI', executeAllowed: true, runCycle: false,
		}));
		component.planForm.patchValue({ request: 'A different request.' });
		component.runSuccessEngine();
		expect(assistant.command).toHaveBeenCalledTimes(2);

		component.refreshHistoryBeforeUncertainRetry();
		expect(taskPlans.logs).toHaveBeenCalled();
		expect(taskPlans.reviewQueue).toHaveBeenCalled();
		expect(modal.confirm).toHaveBeenCalled();
		const confirmation = modal.confirm.calls.mostRecent().args[0];
		expect(confirmation!.nzContent).toContain('may not prove whether an external action finished');
		(confirmation!.nzOnOk as () => void)();

		expect(assistant.command).toHaveBeenCalledTimes(3);
		expect(assistant.command.calls.mostRecent().args[0]).toEqual(retained!.request);
		expect(component.uncertainCommand).toBeUndefined();
	});

	it('clears a stale plan when a later assistant response contains no plan', () => {
		const { component } = createComponent();
		component.plan = { id: 'stale-plan' } as any;
		component.lastCommand = { id: 'stale-command' } as any;
		component.planForm.patchValue({ request: 'Plan the next task.' });

		component.createPlan();

		expect(component.plan).toBeUndefined();
		expect(component.lastCommand?.id).toBe('command-1');
	});

	it('resets the request and success criteria for a new conversation but keeps project context', () => {
		const { component } = createComponent();
		component.planForm.patchValue({ request: 'Old task', successCriteria: 'Old criteria', projectKey: '018-HAI' });

		component.clearChat();

		expect(component.planForm.value.request).toBe('');
		expect(component.planForm.value.successCriteria).toBe('');
		expect(component.planForm.value.projectKey).toBe('018-HAI');
	});

	it('prevents keyboard shortcut default submission and maps Ctrl/Command+Enter to planning only', () => {
		const { component, assistant } = createComponent();
		component.planForm.patchValue({ request: 'Prepare a brief.' });
		const event = new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, cancelable: true });

		component.onComposerKeydown(event);

		expect(event.defaultPrevented).toBeTrue();
		expect(assistant.command).toHaveBeenCalledWith(jasmine.objectContaining({ executeAllowed: false, runCycle: false }));
	});

	it('prevents concurrent approval submissions and does not call an unvalidated run successful', () => {
		const { component, taskPlans, notifications } = createComponent();
		const pending = new Subject<any>();
		const first: any = { id: 'review-1', taskId: 'task-1', request: { request: 'Do work' }, reason: 'Decision', priority: 'high', status: 'open', createdAt: '' };
		const second: any = { ...first, id: 'review-2', taskId: 'task-2' };
		component.reviewQueue = [first, second];
		taskPlans.resolveReviewItem.and.returnValue(pending);

		component.resolveReviewItem(first, true);
		component.resolveReviewItem(second, true);
		expect(taskPlans.resolveReviewItem).toHaveBeenCalledTimes(1);
		expect(component.canResolveReview(second)).toBeFalse();

		pending.next({
			item: { ...first, status: 'approved' },
			plan: { completionStatus: 'needs_review', validationResult: { passed: false, nextAction: 'Inspect the run' } },
		});
		pending.complete();

		expect(component.chatMessages[component.chatMessages.length - 1].body).toBe('Approval recorded; run result needs review.');
		expect(component.chatMessages[component.chatMessages.length - 1].status).toBe('warning');
		expect(notifications.warning).toHaveBeenCalledWith('Run needs review', jasmine.any(String));
	});

	it('requires deliberate confirmation before retrying an uncertain operation', () => {
		const { component, taskPlans, modal } = createComponent();
		const item: any = {
			id: 'operation-review',
			taskId: 'operation:11111111-1111-1111-1111-111111111111',
			request: { request: 'Prepare a safe summary' },
			reason: 'Prior attempt has an uncertain outcome.',
			priority: 'high',
			status: 'needs_review',
			createdAt: '2026-08-04T00:00:00Z',
		};

		component.resolveReviewItem(item, true);

		expect(modal.confirm).toHaveBeenCalled();
		expect(taskPlans.resolveReviewItem).not.toHaveBeenCalled();
		expect(component.reviewApproveLabel(item)).toBe('Retry as new attempt');
		expect(component.reviewRejectLabel(item)).toBe('Close without retry');
		const confirmation = modal.confirm.calls.mostRecent().args[0];
		expect(confirmation).toBeDefined();
		expect(confirmation!.nzContent).toContain('did not already produce the intended effect');
		expect(typeof confirmation!.nzOnOk).toBe('function');
		(confirmation!.nzOnOk as () => void)();
		expect(taskPlans.resolveReviewItem).toHaveBeenCalledWith(item.id, jasmine.objectContaining({
			approved: true,
			confirmation: 'RETRY UNCERTAIN OPERATION',
		}));
	});

	it('previews recovery before applying the exact fail-closed reconciliation', () => {
		const { component, taskPlans, modal } = createComponent();
		const preview = {
			dryRun: true,
			cutoff: '2026-08-04T00:00:00Z',
			inspected: 1,
			approvedFound: 1,
			eligible: 1,
			completed: 0,
			returnedToReview: 1,
			conflicts: 0,
			items: [],
		};
		taskPlans.reconcileApprovedReviews.and.returnValue(of(preview));

		component.previewApprovedReviewRecovery();

		expect(taskPlans.reconcileApprovedReviews).toHaveBeenCalledWith({
			apply: false,
			confirmation: undefined,
			olderThanMinutes: 30,
			limit: 50,
		});
		expect(component.reconciliation).toEqual(preview);

		taskPlans.reconcileApprovedReviews.calls.reset();
		const applied = { ...preview, dryRun: false, completed: 1, returnedToReview: 0 };
		taskPlans.reconcileApprovedReviews.and.returnValue(of(applied));
		component.applyApprovedReviewRecovery();
		expect(modal.confirm).toHaveBeenCalled();
		expect(taskPlans.reconcileApprovedReviews).not.toHaveBeenCalled();
		const confirmation = modal.confirm.calls.mostRecent().args[0];
		expect(confirmation!.nzTitle).toBe('Apply this recovery preview?');
		(confirmation!.nzOnOk as () => void)();
		expect(taskPlans.reconcileApprovedReviews).toHaveBeenCalledWith({
			apply: true,
			confirmation: 'RECONCILE APPROVED TASKS',
			olderThanMinutes: 30,
			limit: 50,
		});
		expect(component.reconciliation).toEqual(applied);
	});

  it('opens the calendar-aware resource schedule from the basic summary', () => {
    const { component } = createComponent();

    component.inspectorMode = 'overview';
    component.openResourceSchedule();

    expect(component.inspectorMode).toBe('plan');
  });

  it('preserves plan history and approval decisions when their refresh fails', () => {
    const { component, taskPlans } = createComponent();
    component.logs = [{ id: 'plan-1' } as any];
    component.reviewQueue = [{ id: 'review-1' } as any];
    taskPlans.logs.and.returnValue(throwError(() => new Error('history unavailable')));
    taskPlans.reviewQueue.and.returnValue(throwError(() => new Error('queue unavailable')));

    component.loadLogs();
    component.loadReviewQueue();

    expect(component.logs.map((plan) => plan.id)).toEqual(['plan-1']);
    expect(component.reviewQueue.map((item) => item.id)).toEqual(['review-1']);
    expect(component.logsUnavailable).toBeTrue();
    expect(component.reviewQueueUnavailable).toBeTrue();
  });
});

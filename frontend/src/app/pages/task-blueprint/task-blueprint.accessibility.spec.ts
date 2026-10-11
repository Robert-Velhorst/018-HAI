import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ReactiveFormsModule } from '@angular/forms';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { RouterTestingModule } from '@angular/router/testing';
import { of, Subject } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { AppShellComponent } from '../../control-room/app-shell.component';
import { NavigationEnd, Router } from '@angular/router';
import { RuntimeControlService } from '../../services/runtime-control.service';
import { ThemeService } from '../../services/theme.service';
import { TASK_PLAN_SERVICE_TOKEN } from '../../services/task-plan/task-plan.service.token';
import { AssistantCommandService } from '../../services/assistant-command.service';
import { PydanticAIService } from '../../services/pydantic-ai.service';
import { CrewAIService } from '../../services/crewai.service';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { TaskBlueprintComponent } from './task-blueprint.component';

describe('TaskBlueprintComponent inspector selection accessibility', () => {
  let fixture: ComponentFixture<TaskBlueprintComponent>;
  let preferences: ModuleViewPreferencesService;
  let taskPlans: jasmine.SpyObj<any>;
  let assistant: jasmine.SpyObj<AssistantCommandService>;

  beforeEach(async () => {
    taskPlans = jasmine.createSpyObj('TaskPlanService', [
      'logs',
      'reviewQueue',
      'resolveReviewItem',
      'reconcileApprovedReviews',
    ]);
    taskPlans.logs.and.returnValue(of([]));
    taskPlans.reviewQueue.and.returnValue(of([]));
    taskPlans.resolveReviewItem.and.returnValue(of({ item: {} }));
    taskPlans.reconcileApprovedReviews.and.returnValue(of({}));
    assistant = jasmine.createSpyObj<AssistantCommandService>('AssistantCommandService', ['command']);
    assistant.command.and.returnValue(of({} as any));

    await TestBed.configureTestingModule({
      declarations: [TaskBlueprintComponent],
      imports: [
        CommonModule,
        ReactiveFormsModule,
        RouterTestingModule,
        NoopAnimationsModule,
        ControlRoomModule,
      ],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: TASK_PLAN_SERVICE_TOKEN, useValue: taskPlans },
        { provide: AssistantCommandService, useValue: assistant },
        { provide: PydanticAIService, useValue: { propose: () => of({}) } },
        { provide: CrewAIService, useValue: { propose: () => of({}) } },
        { provide: NzNotificationService, useValue: { success: () => undefined, error: () => undefined, warning: () => undefined } },
        { provide: NzModalService, useValue: { confirm: () => undefined } },
      ],
    }).compileComponents();

    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('task-blueprint');
    preferences.setMode('task-blueprint', 'advanced');
    preferences.setSection('task-blueprint', 'task-inspector', true);
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('task-blueprint');
  });

  it('starts in Basic and persists the module-local view switch', () => {
    preferences.setMode('task-blueprint', 'basic');
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const switcher = page.querySelector('[aria-label="Task Blueprint view detail"]') as HTMLElement;
    const buttons = Array.from(switcher.querySelectorAll<HTMLButtonElement>('button'));
    const basic = buttons.find((button) => button.textContent?.trim() === 'Basic')!;
    const advanced = buttons.find((button) => button.textContent?.trim() === 'Advanced')!;

    expect(fixture.componentInstance.isAdvancedView).toBeFalse();
    expect(basic.getAttribute('aria-pressed')).toBe('true');
    expect(advanced.getAttribute('aria-pressed')).toBe('false');
    expect(page.querySelector('.context-rail')).toBeNull();

    advanced.click();
    fixture.detectChanges();
    expect(fixture.componentInstance.isAdvancedView).toBeTrue();
    expect(preferences.get('task-blueprint').mode).toBe('advanced');
    expect(page.querySelector('.context-rail')).not.toBeNull();

    basic.click();
    fixture.detectChanges();
    expect(fixture.componentInstance.isAdvancedView).toBeFalse();
    expect(preferences.get('task-blueprint').mode).toBe('basic');
    expect(page.querySelector('.context-rail')).toBeNull();
  });

  it('keeps the local and shell Basic/Advanced controls synchronized in both directions', () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(true);
    preferences.setMode('task-blueprint', 'basic');
    fixture.detectChanges();
    const routerEvents = new Subject<NavigationEnd>();
    const shell = new AppShellComponent(
      { url: '/task-blueprint', events: routerEvents.asObservable() } as unknown as Router,
      preferences,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of({}) } as unknown as RuntimeControlService,
    );
    shell.ngOnInit();

    const page: HTMLElement = fixture.nativeElement;
    const viewSwitcher = page.querySelector('[aria-label="Task Blueprint view detail"]') as HTMLElement;
    const buttons = Array.from(viewSwitcher.querySelectorAll<HTMLButtonElement>('button'));
    const basic = buttons.find((button) => button.textContent?.trim() === 'Basic')!;
    const advanced = buttons.find((button) => button.textContent?.trim() === 'Advanced')!;

    expect(shell.viewMode).toBe('basic');
    advanced.click();
    fixture.detectChanges();
    expect(shell.viewMode).toBe('advanced');
    expect(document.body.classList.contains('hai-view-advanced')).toBeTrue();

    shell.toggleViewMode();
    fixture.detectChanges();
    expect(shell.viewMode).toBe('basic');
    expect(fixture.componentInstance.isAdvancedView).toBeFalse();
    expect(document.body.classList.contains('hai-view-advanced')).toBeFalse();

    shell.toggleViewMode();
    fixture.detectChanges();
    expect(fixture.componentInstance.isAdvancedView).toBeTrue();
    expect(advanced.getAttribute('aria-pressed')).toBe('true');

    basic.click();
    fixture.detectChanges();
    expect(shell.viewMode).toBe('basic');
    expect(document.body.classList.contains('hai-view-advanced')).toBeFalse();
    shell.ngOnDestroy();
  });

  it('keeps Run disabled until the current plan is opened for review', () => {
    preferences.setMode('task-blueprint', 'basic');
    document.body.classList.remove('hai-view-advanced');
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
    const component = fixture.componentInstance;
    const plan = {
      id: 'plan-current',
      request: 'Prepare a source-linked chronology.',
      projectKey: '018-HAI',
      realGoal: 'Prepare a source-linked chronology.',
      completionStatus: 'planned',
      steps: [{ id: 'step-1', name: 'Collect sources', purpose: 'Find records', allowed: true, requiresApproval: false, status: 'ready' }],
      riskAssessment: { level: 'low', approvalRequired: false, approvalGranted: false, actionResolution: 'proceed', reasons: [], allowedNow: true },
      intake: { riskLevel: 'low', successCriteria: [] },
      contextPlan: { usedContext: [], sourceContext: [] },
      modelDecision: { selectedModelName: 'Local model', tier: 'standard' },
      toolDecision: {},
      minimalityDecision: {},
      validationPlan: { steps: [], successCriteria: [], frameworkEvidenceRequirements: [], frameworkCompletionCriteria: [], frameworkAssuranceCriteria: [] },
      validationResult: { passed: false, status: 'not_run', checked: [], failures: [], criteria: [], nextAction: 'Review the plan.' },
      executionPlan: { approvalRequiredFor: [], auditEvents: [] },
      retryPolicy: {},
      memoryUpdateProposals: [],
      lessonsLearned: [],
      storedMemoryIds: [],
      events: [],
    } as any;
    assistant.command.and.returnValue(of({
      id: 'plan-command', createdAt: '', intent: 'plan', summary: 'Plan prepared.', nextAction: 'Review it.',
      safetySummary: 'Plan only.', actions: [], reviewRequired: false, plan,
    } as any));
    component.planForm.patchValue({ request: plan.request });

    const page: HTMLElement = fixture.nativeElement;
    const runButton = () => page.querySelector<HTMLButtonElement>('[data-testid="task-chat-run"]')!;
    expect(runButton().disabled).toBeTrue();
    expect(page.querySelector('.safety-note')).not.toBeNull();
    expect(page.textContent).toContain('Choose Plan first');

    (page.querySelector('[data-testid="task-chat-plan"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(page.querySelector('.basic-plan-summary')?.textContent).toContain('Collect sources');
    expect(runButton().disabled).toBeTrue();

    Array.from(page.querySelectorAll<HTMLButtonElement>('.basic-plan-summary button'))
      .find((button) => button.textContent?.includes('Review plan'))!.click();
    fixture.detectChanges();
    expect(component.inspectorMode).toBe('plan');
    expect(component.isAdvancedView).toBeTrue();
    expect(runButton().disabled).toBeFalse();

    const requestInput = page.querySelector<HTMLTextAreaElement>('[data-testid="task-chat-input"]')!;
    requestInput.value = 'A different request.';
    requestInput.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    expect(component.planForm.value.request).toBe('A different request.');
    expect(component.canRunSafeSteps()).toBeFalse();
    expect(runButton().disabled).toBeTrue();
  });

  it('exposes inspector views as selected-state buttons, not tabs without panels', () => {
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const selector = page.querySelector('.inspector-tabs') as HTMLElement;
    expect(selector.getAttribute('role')).toBe('group');
    expect(selector.getAttribute('aria-label')).toBe('Inspector views');
    expect(page.querySelector('[role="tablist"]')).toBeNull();
    expect(page.querySelector('[role="tabpanel"]')).toBeNull();

    const buttons = Array.from(selector.querySelectorAll<HTMLButtonElement>('button'));
    expect(buttons.find((button) => button.textContent?.includes('Overview'))?.getAttribute('aria-pressed')).toBe('true');
    expect(buttons.find((button) => button.textContent?.includes('Plan'))?.getAttribute('aria-pressed')).toBe('false');
    buttons.find((button) => button.textContent?.includes('Plan'))?.click();
    fixture.detectChanges();
    expect(buttons.find((button) => button.textContent?.includes('Plan'))?.getAttribute('aria-pressed')).toBe('true');
  });

  it('keeps a cold selected historical receipt visible and non-runnable in Basic after reload and refresh', () => {
    const persisted = {
      id: 'persisted-task', request: 'Inspect the persisted evidence.', projectKey: '018-HAI', pursuitId: 'saved-pursuit',
      completionStatus: 'validated', validationResult: { passed: true }, retryPolicy: { retryAvailable: false },
      executionResult: { mode: 'action', outcomeUncertain: true, toolExecution: {
        status: 'completed', launchEventId: 'saved-launch', runtimeTaskId: 'saved-runtime-task',
        executionReference: 'runtime://saved-attempt', message: 'password=private-value Runtime acknowledgement missing.',
        receipt: { runtimeId: 'local-runtime', ok: true, output: { artifactHash: 'saved-hash', boundedOutput: 'token=private-value Artifact retained.' }, verification: { passed: true } },
      } },
    };
    taskPlans.logs.and.returnValue(of([persisted]));
    preferences.setMode('task-blueprint', 'basic');
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
    fixture.componentInstance.copyPlanToComposer(fixture.componentInstance.logs[0]);
    fixture.detectChanges();
    expect(fixture.componentInstance.uncertainCommand).toBeUndefined();

    // A fresh component must reconstruct the restriction from persisted API history.
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
    const component = fixture.componentInstance;
    component.copyPlanToComposer(component.logs[0]);
    const selected = component.plan;
    taskPlans.logs.and.returnValue(of([]));
    component.loadLogs();
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const notice = page.querySelector('[data-testid="task-execution-safety"]') as HTMLElement;
    expect(component.plan).toBe(selected);
    expect(component.isAdvancedView).toBeFalse();
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.closest('app-hai-progressive-section, hai-progressive-section')).toBeNull();
    expect(notice.textContent).toContain('Completion is not confirmed');
    expect(notice.textContent).toContain('automation-launch://saved-launch');
    expect(notice.textContent).toContain('saved-runtime-task');
    expect(notice.textContent).toContain('runtime://saved-attempt');
    expect(notice.textContent).toContain('saved-hash');
    expect(notice.textContent).toContain('[redacted]');
    expect(page.textContent).not.toContain('private-value');
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-run"]')!.disabled).toBeTrue();
    expect(page.querySelector('[data-testid="task-chat-cycle"]')).toBeNull();
    component.runSuccessEngine();
    component.runAgentCycle();
    expect(assistant.command).not.toHaveBeenCalled();
    component.setViewMode('advanced');
    fixture.detectChanges();
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-cycle"]')!.disabled).toBeTrue();
    component.runSuccessEngine();
    component.runAgentCycle();
    expect(assistant.command).not.toHaveBeenCalled();

    component.setViewMode('basic');
    component.planForm.patchValue({ request: 'Plan a different task.' });
    fixture.detectChanges();
    expect(component.plan).toBe(selected);
    expect(component.currentPlanState()).toBe('Outcome uncertain');
    expect(page.querySelector('[data-testid="task-chat-conversation"]')!.textContent).toContain('saved-hash');
    expect(page.querySelector('[data-testid="task-chat-conversation"]')!.textContent).toContain('saved-launch');
    expect(page.querySelector('[data-testid="task-chat-cycle"]')).toBeNull();
    component.setViewMode('advanced');
    fixture.detectChanges();
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-cycle"]')!.disabled).toBeFalse();
    expect(page.textContent).not.toContain('private-value');
  });

  it('shows possible partial effects and the inspected receipt in Basic, not behind disclosure', () => {
    preferences.setMode('task-blueprint', 'basic');
    preferences.setSection('task-blueprint', 'task-inspector', false);
    const component = fixture.componentInstance;
    component.planForm.patchValue({ request: 'Collect evidence.' });
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [],
      summary: 'Completed. No external action occurred.', nextAction: 'Retry now.',
      plan: {
        id: 'uncertain-plan', request: 'Collect evidence.', completionStatus: 'validated',
        validationResult: { passed: true }, steps: [{ allowed: true, requiresApproval: false }],
        executionResult: { mode: 'blocked', outcomeUncertain: true, toolExecution: {
          status: 'completed', outcomeUncertain: true, launchEventId: 'receipt-1', message: 'Runtime response was lost.',
          runtimeTaskId: 'runtime-task-1', executionReference: 'runtime://attempt-1',
        } },
        retryPolicy: { retryAvailable: false },
      },
    } as any));
    component.createPlan();
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const notice = page.querySelector('[data-testid="task-execution-safety"]') as HTMLElement;
    expect(notice).not.toBeNull();
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.closest('app-hai-progressive-section, hai-progressive-section')).toBeNull();
    expect(component.isAdvancedView).toBeFalse();
    expect(notice.textContent).toContain('Partial effects may have occurred');
    expect(notice.textContent).toContain('completed; outcome uncertain');
    expect(notice.textContent).toContain('automation-launch://receipt-1');
    expect(notice.textContent).toContain('runtime-task-1');
    expect(notice.textContent).toContain('runtime://attempt-1');
    expect(page.textContent).not.toContain('No external action occurred');
    expect(page.querySelector('.plan-state')!.textContent).toContain('Outcome uncertain');
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-run"]')!.disabled).toBeTrue();
    component.showInspector('evidence');
    fixture.detectChanges();
    expect(page.querySelector('[data-testid="task-tool-status"]')!.textContent).toContain('completed; outcome uncertain');
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-cycle"]')!.disabled).toBeTrue();
    component.showInspector('overview');
    fixture.detectChanges();
    expect(page.querySelector('.validation-state')!.textContent).toContain('Outcome uncertain');
    expect(page.querySelector('.validation-state')!.classList.contains('validation-state--passed')).toBeFalse();
  });

  it('shows legacy uncertain tool statuses and explicit execution uncertainty without a tool in Basic', () => {
    preferences.setMode('task-blueprint', 'basic');
    const component = fixture.componentInstance;
    component.planForm.patchValue({ request: 'Collect evidence.' });
    for (const status of ['failed', 'pending', 'indeterminate', 'new_status', '']) {
      assistant.command.and.returnValue(of({ intent: 'plan', actions: [], plan: {
        request: 'Collect evidence.', executionResult: { toolExecution: { status } }, retryPolicy: { retryAvailable: true },
      } } as any));
      component.createPlan();
      fixture.detectChanges();
      const notice = fixture.nativeElement.querySelector('[data-testid="task-execution-safety"]') as HTMLElement;
      expect(notice.textContent).withContext(status || 'empty status').toContain('Partial effects may have occurred');
      expect(notice.textContent).toContain(status ? `${status}; outcome uncertain` : 'No tool status returned; outcome uncertain');
    }
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [], summary: 'No external action occurred.', plan: {
      request: 'Collect evidence.', executionResult: {
        mode: 'blocked', outcomeUncertain: true, blockedReason: 'No external action occurred.', output: 'No external action occurred.',
      }, retryPolicy: { retryAvailable: false },
    } } as any));
    component.createPlan();
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('[data-testid="task-execution-safety"]')!.textContent).toContain('Partial effects may have occurred');
    expect(page.textContent).not.toContain('No external action occurred');
    component.showInspector('evidence');
    fixture.detectChanges();
    expect(page.textContent).not.toContain('No external action occurred');
  });

  it('does not present a known pre-dispatch block as possible partial execution', () => {
    preferences.setMode('task-blueprint', 'basic');
    const component = fixture.componentInstance;
    component.planForm.patchValue({ request: 'Collect evidence.' });
    assistant.command.and.returnValue(of({ intent: 'plan', actions: [], plan: {
      request: 'Collect evidence.', executionResult: {
        mode: 'blocked', blockedReason: 'Runtime configuration is missing.',
        toolExecution: { status: 'needs_approval', outcomeUncertain: false },
      }, retryPolicy: { retryAvailable: true },
    } } as any));
    component.createPlan();
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('[data-testid="task-execution-safety"]')).toBeNull();
    expect(page.textContent).toContain('Runtime configuration is missing');
    expect(page.textContent).not.toContain('Partial effects may have occurred');
  });

  it('disables retry approval but leaves closure available for a restricted operation in Basic', () => {
    preferences.setMode('task-blueprint', 'basic');
    const component = fixture.componentInstance;
    component.logs = [{ id: 'plan-1', operationId: 'op-1', executionResult: { outcomeUncertain: true }, retryPolicy: { retryAvailable: false } } as any];
    component.reviewQueue = [{ id: 'review-1', taskId: 'operation:op-1', reason: 'Inspect earlier effects.', priority: 'normal', status: 'needs_review', request: { request: 'Collect evidence.' } } as any];
    fixture.detectChanges();
    const buttons = Array.from((fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>('.basic-approval-item__actions button'));
    expect(buttons.find((button) => button.textContent?.includes('Retry as new attempt'))!.disabled).toBeTrue();
    expect(buttons.find((button) => button.textContent?.includes('Close without retry'))!.disabled).toBeFalse();
    component.showInspector('overview');
    fixture.detectChanges();
    const advancedButtons = Array.from((fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>('.review-row button'));
    expect(advancedButtons.find((button) => button.textContent?.includes('Retry as new attempt'))!.disabled).toBeTrue();
    expect(advancedButtons.find((button) => button.textContent?.includes('Close without retry'))!.disabled).toBeFalse();
  });

  it('renders actionable dependency hints in Basic without turning approval into readiness', () => {
    preferences.setMode('task-blueprint', 'basic');
    const component = fixture.componentInstance;
    component.planForm.patchValue({ request: 'Prepare a reviewed draft.' });
    assistant.command.and.returnValue(of({
      intent: 'plan', actions: [], reviewRequired: false,
      plan: {
        request: 'Prepare a reviewed draft.',
        steps: [{ name: 'Draft', allowed: true, requiresApproval: false }],
        riskAssessment: {
          actionResolution: 'block', approvalRequired: true, approvalGranted: true,
          missingRequiredAgents: ['evidence_reviewer', 'red_team_reviewer'],
          missingParameters: ['controlled automation'],
        },
      },
    } as any));
    component.createPlan();
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const hints = () => Array.from(page.querySelectorAll('[data-testid="task-preflight-hint"]'))
      .map((element) => element.textContent).join(' ');
    expect(component.isAdvancedView).toBeFalse();
    expect(hints()).toContain('No model route is selected');
    expect(hints()).toContain('No operating framework selection is recorded');
    expect(hints()).toContain('evidence_reviewer, red_team_reviewer');
    expect(hints()).toContain('approval alone does not resolve this prerequisite');
    expect(hints()).toContain('controlled automation');
    expect(page.querySelector<HTMLButtonElement>('[data-testid="task-chat-run"]')!.disabled).toBeTrue();

    component.showInspector('overview');
    fixture.detectChanges();
    expect(page.querySelector('.risk-box')!.textContent).toContain('Execution blocked by risk policy');
    expect(page.querySelector('.risk-box')!.classList.contains('risk-box--blocked')).toBeTrue();
    expect(page.querySelector('.risk-box')!.textContent).not.toContain('No high-risk action detected');
    expect(page.querySelector('.context-rail')!.textContent).toContain('No model selected');
    expect(page.querySelector('.context-rail')!.textContent).not.toContain('Smart routing');
    component.plan!.riskAssessment.missingRequiredAgents = ['red_team_reviewer'];
    fixture.detectChanges();
    expect(hints()).toContain('Required participants need verification: red_team_reviewer.');
    expect(component.canRunSafeSteps()).toBeFalse();
  });

  it('renders a confirmed metadata preflight block with no misleading approval label', () => {
    const component = fixture.componentInstance;
    component.planForm.patchValue({ request: 'Read configured metadata.' });
    assistant.command.and.returnValue(of({
      intent: 'plan', actions: [], reviewRequired: false, nextAction: 'Approve the plan.',
      plan: {
        request: 'Read configured metadata.', completionStatus: 'review_required',
        riskAssessment: { level: 'low', approvalRequired: false },
        executionResult: { mode: 'blocked', blockedReason: 'Runtime metadata could not be confirmed; configure a capable model or inspect the read-only runtime.' },
      },
    } as any));
    component.createPlan();
    component.showInspector('overview');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('.next-decision__action')!.textContent).toContain('Runtime metadata could not be confirmed');
    expect(page.querySelector('.next-decision__action')!.textContent).not.toContain('Approve the plan');
    expect(page.querySelector('.risk-box')!.textContent).toContain('Recorded execution block');
    expect(page.querySelector('.risk-box')!.classList.contains('risk-box--blocked')).toBeTrue();
  });

  it('keeps the complete actionable approval queue available in Basic without resolving on disclosure', () => {
    preferences.setMode('task-blueprint', 'basic');
    document.body.classList.remove('hai-view-advanced');
    const pendingItems = Array.from({ length: 4 }, (_, index) => ({
      id: `review-${index + 1}`,
      taskId: `task-${index + 1}`,
      request: { request: `Review request ${index + 1}` },
      reason: `Decision ${index + 1}`,
      priority: 'normal',
      status: 'open',
      createdAt: '2026-09-24T10:00:00Z',
    })) as any;
    taskPlans.reviewQueue.and.returnValue(of(pendingItems));
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
    const resolve = spyOn(fixture.componentInstance, 'resolveReviewItem');
    expect(fixture.componentInstance.reviewQueueOpenCount()).toBe(4);

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('.basic-approvals')).not.toBeNull();
    const queue = page.querySelector('#basic-approval-queue') as HTMLElement;
    expect(queue.querySelectorAll('.basic-approval-item').length).toBe(3);
    const toggle = page.querySelector('.basic-approvals__toggle') as HTMLButtonElement;
    expect(toggle.textContent).toContain('Show all 4 decisions');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');

    toggle.click();
    fixture.detectChanges();

    expect(queue.querySelectorAll('.basic-approval-item').length).toBe(4);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(resolve).not.toHaveBeenCalled();
  });

  it('shows the actual returned plan and sources in Basic, and opens evidence details accessibly', () => {
    preferences.setMode('task-blueprint', 'basic');
    document.body.classList.remove('hai-view-advanced');
    fixture.destroy();
    fixture = TestBed.createComponent(TaskBlueprintComponent);
    fixture.detectChanges();
    const component = fixture.componentInstance;
    const plan = {
      request: 'Prepare a case chronology',
      realGoal: 'Prepare the evidence-backed chronology',
      completionStatus: 'planned',
      steps: [
        { id: 'step-1', name: 'Collect emails', purpose: 'Find the source messages', status: 'ready', allowed: true, requiresApproval: false },
      ],
      contextPlan: {
        sourceContext: [{ extraction: { summary: 'Lawyer request', text: 'Email content', sourceLabel: 'Email · 12 June', sourceUri: 'mail://message-1' } }],
        usedContext: [],
      },
      riskAssessment: { approvalRequired: false },
      validationResult: { nextAction: 'Review the plan' },
    };
    assistant.command.and.returnValue(of({
      id: 'command-plan',
      createdAt: '2026-09-24T10:00:00Z',
      intent: 'plan',
      summary: 'Plan prepared.',
      nextAction: 'Review the plan.',
      safetySummary: 'No execution occurred.',
      actions: [],
      reviewRequired: false,
      plan,
    } as any));
    component.planForm.patchValue({ request: 'Prepare a case chronology' });
    component.createPlan();
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(component.plan).toBeDefined();
    const summary = page.querySelector('.basic-plan-summary') as HTMLElement;
    expect(summary.textContent).toContain('Collect emails');
    expect(summary.textContent).toContain('1 source records');
    expect(summary.textContent).toContain('Lawyer request');

    const evidenceButton = Array.from(summary.querySelectorAll<HTMLButtonElement>('button'))
      .find((button) => button.textContent?.includes('Review sources'));
    expect(evidenceButton).toBeDefined();
    evidenceButton!.click();
    fixture.detectChanges();

    expect(component.inspectorMode).toBe('evidence');
    expect(component.isAdvancedView).toBeTrue();
    expect(preferences.get('task-blueprint').openSections['task-inspector']).toBeTrue();
  });

  it('keeps approved-task recovery visible even though the review endpoint returns pending items only', () => {
    fixture.componentInstance.showInspector('logs');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const recovery = page.querySelector('.reconciliation-control') as HTMLElement;

    expect(recovery).not.toBeNull();
    expect(recovery.textContent).toContain('pending queue contains unresolved decisions only');
    expect(recovery.querySelector('button')?.textContent).toContain('Preview recovery');

    fixture.componentInstance.showInspector('overview');
    fixture.detectChanges();
    expect(page.querySelectorAll('.reconciliation-control').length).toBe(1);
    expect(page.querySelector('.reconciliation-control')?.textContent).toContain('Preview recovery');

    fixture.componentInstance.showInspector('logs');
    fixture.detectChanges();
    expect(page.querySelectorAll('.reconciliation-control').length).toBe(1);
  });
});

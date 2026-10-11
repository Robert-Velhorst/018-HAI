import { FormBuilder } from '@angular/forms';
import { ActivatedRoute, Router, convertToParamMap } from '@angular/router';
import { of, throwError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { TaskBlueprintComponent } from './task-blueprint.component';
import { IFrameworkSelectionDecision } from '../../models/framework-registry.model.interface';

describe('TaskBlueprintComponent pursuit context', () => {
	function createComponent(pursuitId: string = ''): {
		component: TaskBlueprintComponent;
		router: jasmine.SpyObj<Router>;
		taskPlans: jasmine.SpyObj<any>;
		assistant: jasmine.SpyObj<any>;
		modal: jasmine.SpyObj<NzModalService>;
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
		const notifications = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error']);
		const modal = jasmine.createSpyObj<NzModalService>('NzModalService', ['confirm']);
    const route = {
      queryParamMap: of(convertToParamMap(pursuitId ? { pursuitId } : {})),
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
        { mode: () => 'light' } as any,
        {} as any,
        { propose: jasmine.createSpy('propose') } as any,
      ),
			router,
			taskPlans,
			assistant,
			modal,
    };
  }

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
		expect(component.canResolveReview(component.reviewQueue[1])).toBeTrue();
		expect(component.canResolveReview(component.reviewQueue[2])).toBeFalse();
		expect(component.approvedReviewCount()).toBe(1);
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
		const { component, taskPlans } = createComponent();
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
		component.applyApprovedReviewRecovery();
		expect(taskPlans.reconcileApprovedReviews).toHaveBeenCalledWith({
			apply: true,
			confirmation: 'RECONCILE APPROVED TASKS',
			olderThanMinutes: 30,
			limit: 50,
		});
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

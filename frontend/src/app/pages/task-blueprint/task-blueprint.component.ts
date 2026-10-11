import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { timeout } from 'rxjs/operators';
import {
  ICompletionPlan,
	ITaskPlanHistoryItem,
	IApprovedReviewReconciliationResult,
  IReviewQueueItem,
  IToolExecutionResult,
  IValidationCriterionResult,
} from '../../models/task-plan.model.interface';
import {
  IFrameworkSelectionDecision,
  ISelectedFramework,
} from '../../models/framework-registry.model.interface';
import { IAssistantCommandResult } from '../../models/assistant-command.model.interface';
import { IAgentCyclePursuitOperatingState } from '../../models/agent-cycle.model.interface';
import { AssistantCommandService } from '../../services/assistant-command.service';
import { TASK_PLAN_SERVICE_TOKEN } from '../../services/task-plan/task-plan.service.token';
import { ITaskPlanService } from '../../services/task-plan.service.interface';
import { ThemeMode, ThemeService } from '../../services/theme.service';
import { IPydanticAIResponse } from '../../models/pydantic-ai.model.interface';
import { PydanticAIService } from '../../services/pydantic-ai.service';
import { ICrewAIResponse } from '../../models/crewai.model.interface';
import { CrewAIService } from '../../services/crewai.service';

type ChatRole = 'assistant' | 'user' | 'system';
type ChatIntent = 'plan' | 'run' | 'cycle';

interface ChatMessage {
  id: string;
  role: ChatRole;
  title?: string;
  body: string;
  at: Date;
  bullets?: string[];
  status?: 'neutral' | 'working' | 'success' | 'blocked' | 'warning';
}

interface SuggestedPrompt {
  label: string;
  prompt: string;
  criteria: string;
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-task-blueprint',
  templateUrl: './task-blueprint.component.html',
  styleUrls: ['./task-blueprint.component.scss'],
})
export class TaskBlueprintComponent implements OnInit {
	private readonly taskOperationRetryConfirmation = 'RETRY UNCERTAIN OPERATION';
  plan?: ICompletionPlan;
  lastCommand?: IAssistantCommandResult;
  logs: ITaskPlanHistoryItem[] = [];
  reviewQueue: IReviewQueueItem[] = [];
  loading = false;
  running = false;
  cycling = false;
  resolvingReviewId = '';
	reconcilingReviews = false;
	reconciliation?: IApprovedReviewReconciliationResult;
  inspectorMode:
    | 'overview'
    | 'plan'
    | 'evidence'
    | 'typed-proposal'
    | 'crew-proposal'
    | 'logs' = 'overview';
  contextExpanded = false;
  typedProposal?: IPydanticAIResponse;
  typedProposalLoading = false;
  crewProposal?: ICrewAIResponse;
  crewProposalLoading = false;
  themeMode: ThemeMode = 'light';
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 20000;

  chatMessages: ChatMessage[] = [
    {
      id: 'welcome',
      role: 'assistant',
      title: 'Talk to HAI',
      body:
        'Tell me what you want moved forward. I will classify the task, gather context, define success criteria, choose the cheapest capable model/tools, and ask for approval before risky execution.',
      at: new Date(),
      status: 'neutral',
      bullets: [
        'Use normal language, not a form.',
        'I turn your request into a plan, workflow, evidence checks, and next actions.',
        'High-risk actions stay blocked until you approve them.',
      ],
    },
  ];

  suggestions: SuggestedPrompt[] = [
    {
      label: 'Plan my next step',
      prompt:
        'Look at the open work for 018-HAI and tell me the next safest action that moves the project forward.',
      criteria:
        'The next action is specific\nRisk and approval needs are clear\nThe action can be executed or delegated',
    },
    {
      label: 'Prepare a reply',
      prompt:
        'Draft a formal reply for an important external message, collect supporting evidence, and route it for my approval before sending.',
      criteria:
        'Draft is factual and restrained\nEvidence is linked\nSending requires approval',
    },
    {
      label: 'Clear blockers',
      prompt:
        'Find what is blocked, identify who or what we are waiting for, and create the smallest safe follow-up action.',
      criteria:
        'Blocked reason is explicit\nResponsible party is identified\nFollow-up timing is proposed',
    },
    {
      label: 'What needs me?',
      prompt:
        'Show the pursuits, open loops, and decisions that need Robert right now, then recommend the smallest Yes/No decision or next action.',
      criteria:
        'Robert-only decisions are separated from VA-ready work\nNext action is concrete\nRisk and approval needs are visible',
    },
    {
      label: 'Run success loop',
      prompt:
        'Take this task through the completion-first success engine and execute only the safe allowed steps.',
      criteria:
        'Success criteria are checked\nUnsafe actions stay blocked\nResult is verified before completion',
    },
  ];

  planForm: FormGroup = this.fb.group({
    request: [
      'Implement a completion-first context and routing workflow for 018-HAI.',
      [Validators.required],
    ],
    projectKey: ['018-HAI'],
    pursuitId: [''],
    automationId: [''],
    mandateId: [''],
    successCriteria: [''],
    includeRagflowCandidates: [false],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(TASK_PLAN_SERVICE_TOKEN)
    private taskPlanService: ITaskPlanService,
    private assistantCommandService: AssistantCommandService,
    private notification: NzNotificationService,
    private modal: NzModalService,
    private router: Router,
    private route: ActivatedRoute,
    private themeService: ThemeService,
    private pydanticAIService: PydanticAIService,
    private crewAIService: CrewAIService,
  ) {}

  ngOnInit(): void {
    this.themeMode = this.themeService.mode();
    this.route.queryParamMap.subscribe((params) => {
      const pursuitId = params.get('pursuitId') || '';
      this.planForm.patchValue({
        pursuitId,
        projectKey: params.get('projectKey') || this.planForm.value.projectKey,
        request: params.get('request') || this.planForm.value.request,
        mandateId: params.get('mandateId') || this.planForm.value.mandateId,
      });
      if (pursuitId) {
        this.contextExpanded = true;
      }
    });
    this.loadLogs();
    this.loadReviewQueue();
  }

  createPlan(): void {
    this.submitChat('plan');
  }

  runSuccessEngine(): void {
    this.submitChat('run');
  }

  runAgentCycle(): void {
    this.submitChat('cycle');
  }

  requestTypedProposal(): void {
    const request = this.singleLine(this.planForm.value.request);
    if (!request || this.typedProposalLoading) {
      return;
    }
    this.typedProposalLoading = true;
    this.pydanticAIService.propose(request, this.criteria()).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (response) => {
        this.typedProposalLoading = false;
        this.typedProposal = response;
        this.inspectorMode = 'typed-proposal';
        this.addAssistantMessage(
          'A local typed planning draft is ready for review.',
          [
            `Model: ${response.modelId}.`,
            'It is a draft only. HAI has not executed, approved, saved, or treated it as verified work.',
            'Review the draft, then explicitly copy criteria into the task before using Plan first.',
          ],
          'warning'
        );
      },
      error: () => {
        this.typedProposalLoading = false;
        this.notification.warning(
          'Local typed planner unavailable',
          'Configure the local PydanticAI runner and an approved local model. HAI did not send work to any cloud provider or execute an action.'
        );
      },
    });
  }

  useTypedProposalCriteria(): void {
    const proposal = this.typedProposal?.proposal;
    if (!proposal) return;
    this.planForm.patchValue({ successCriteria: proposal.successCriteria.join('\n') });
    this.contextExpanded = true;
    this.inspectorMode = 'overview';
    this.notification.info('Criteria copied', 'Review the copied criteria and use Plan first when ready. No task has been executed.');
  }

  requestCrewProposal(): void {
    const request = this.singleLine(this.planForm.value.request);
    if (!request || this.crewProposalLoading) {
      return;
    }
    this.crewProposalLoading = true;
    this.crewAIService
      .propose(request, this.criteria())
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (response) => {
          this.crewProposalLoading = false;
          this.crewProposal = response;
          this.inspectorMode = 'crew-proposal';
          this.addAssistantMessage(
            'A local CrewAI planner/reviewer draft is ready for review.',
            [
              `Model: ${response.modelId}.`,
              'The draft is advisory only. HAI has not executed, approved, saved, or verified it.',
              'Review its criteria before using Plan first.',
            ],
            'warning'
          );
        },
        error: () => {
          this.crewProposalLoading = false;
          this.notification.warning(
            'Local CrewAI planner unavailable',
            'Configure the isolated CrewAI runner and an approved local model. No cloud call or action was started.'
          );
        },
      });
  }

  useCrewProposalCriteria(): void {
    const proposal = this.crewProposal?.proposal;
    if (!proposal) {
      return;
    }
    this.planForm.patchValue({ successCriteria: proposal.successCriteria.join('\n') });
    this.contextExpanded = true;
    this.inspectorMode = 'overview';
    this.notification.info(
      'Criteria copied',
      'Review the copied criteria and use Plan first when ready. No task has been executed.'
    );
  }

  submitChat(intent: ChatIntent): void {
    if (this.planForm.invalid) {
      Object.values(this.planForm.controls).forEach((control) => {
        control.markAsDirty();
        control.updateValueAndValidity();
      });
      this.addAssistantMessage(
        'I need a clear request first.',
        ['Write the outcome you want, then I can plan or run the safe steps.'],
        'warning'
      );
      return;
    }

    const requestText = String(this.planForm.value.request || '').trim();
    this.addMessage({
      role: 'user',
      body: requestText,
      at: new Date(),
      status: 'neutral',
    });

    const workingMessage = this.addAssistantMessage(
      intent === 'cycle'
        ? 'I am running the autonomous maintenance cycle and tying it back to this command.'
        : intent === 'run'
        ? 'I am taking this through the success engine and will only execute allowed safe steps.'
        : 'I am turning this into a completion-first plan.',
      [
        'Classifying goal, risk, difficulty, and approval needs.',
        'Checking relevant memory and connected-source context.',
        'Preparing success criteria, routing, validation, and next action.',
      ],
      'working'
    );

    if (intent === 'cycle') {
      this.cycling = true;
    } else if (intent === 'run') {
      this.running = true;
    } else {
      this.loading = true;
    }

    const request = {
      message: requestText,
      projectKey: this.planForm.value.projectKey,
      pursuitId: this.planForm.value.pursuitId,
      automationId: this.planForm.value.automationId,
      mandateId: this.planForm.value.mandateId,
      successCriteria: this.criteria(),
      includeRagflowCandidates: Boolean(this.planForm.value.includeRagflowCandidates),
      executeAllowed: intent === 'run' || intent === 'cycle',
      runCycle: intent === 'cycle',
    };

    this.assistantCommandService.command(request).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (command) => {
        this.lastCommand = command;
        if (command.plan) {
          this.plan = this.normalizePlan(command.plan);
        }
        this.loading = false;
        this.running = false;
        this.cycling = false;
        this.inspectorMode = 'overview';
        this.replaceMessage(workingMessage.id, this.messageFromCommand(command, intent));
        this.loadLogs();
        this.loadReviewQueue();
      },
      error: () => {
        this.loading = false;
        this.running = false;
        this.cycling = false;
        this.replaceMessage(workingMessage.id, {
          role: 'assistant',
          title: 'I could not complete that engine run',
          body: this.commandErrorBody(intent),
          at: new Date(),
          status: 'blocked',
          bullets: ['Check backend health and try again.', 'No risky action was executed.'],
        });
        this.notification.error(
          'Error',
          intent === 'cycle'
            ? 'Failed to run assistant command cycle.'
            : intent === 'run'
            ? 'Failed to run task success engine.'
            : 'Failed to create task plan.'
        );
      },
    });
  }

  loadLogs(): void {
    this.taskPlanService.logs().pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (logs) => (this.logs = logs || []),
      error: () => (this.logs = []),
    });
  }

  loadReviewQueue(): void {
    this.taskPlanService.reviewQueue().pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (items) => (this.reviewQueue = items || []),
      error: () => (this.reviewQueue = []),
    });
  }

  resolveReviewItem(item: IReviewQueueItem, approved: boolean): void {
		if (approved && this.isOperationReview(item)) {
			this.modal.confirm({
				nzTitle: 'Retry this uncertain operation?',
				nzContent: 'Continue only after checking the audit trail and confirming the earlier attempt did not already produce the intended effect. HAI will create a separate durable operation; it will not resume or rewrite the old attempt.',
				nzOkText: 'Create new attempt',
				nzCancelText: 'Keep in review',
				nzOnOk: () => this.performReviewResolution(
					item,
					true,
					'Operator reviewed the uncertain outcome and explicitly authorized a separate durable attempt.',
					this.taskOperationRetryConfirmation,
				),
			});
			return;
		}
		this.performReviewResolution(
			item,
			approved,
			approved
				? 'Approved from HAI chat review queue.'
				: this.isOperationReview(item)
					? 'Uncertain operation closed without creating another attempt.'
					: 'Rejected from HAI chat review queue.',
		);
	}

	private performReviewResolution(
		item: IReviewQueueItem,
		approved: boolean,
		note: string,
		confirmation?: string,
	): void {
    this.resolvingReviewId = item.id;
    this.taskPlanService
      .resolveReviewItem(item.id, {
        approved,
			note,
			confirmation,
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.resolvingReviewId = '';
          if (result.plan) {
            this.plan = this.normalizePlan(result.plan);
            this.addAssistantMessage(
              approved ? 'Approved item was re-run.' : 'Review item was rejected.',
              this.planSummaryBullets(this.plan),
              approved ? 'success' : 'blocked'
            );
          } else {
            this.addAssistantMessage(
              approved ? 'Review approved.' : 'Review rejected.',
              [approved ? 'The engine accepted the approval.' : 'The task remains blocked and will not execute.'],
              approved ? 'success' : 'blocked'
            );
          }
          this.notification.success(
            approved ? 'Review approved' : 'Review rejected',
            result.plan ? 'The approved task was re-run through the success engine.' : 'The task remains blocked.'
          );
          this.loadLogs();
          this.loadReviewQueue();
        },
        error: () => {
          this.resolvingReviewId = '';
          this.notification.error('Error', 'Failed to resolve review item.');
        },
      });
  }

	isOperationReview(item: IReviewQueueItem): boolean {
		return Boolean(item?.taskId?.startsWith('operation:'));
	}

	reviewApproveLabel(item: IReviewQueueItem): string {
		return this.isOperationReview(item) ? 'Retry as new attempt' : 'Approve';
	}

	reviewRejectLabel(item: IReviewQueueItem): string {
		return this.isOperationReview(item) ? 'Close without retry' : 'Reject';
	}

  useSuggestion(suggestion: SuggestedPrompt): void {
    this.planForm.patchValue({
      request: suggestion.prompt,
      successCriteria: suggestion.criteria,
    });
    this.addAssistantMessage(
      `Loaded "${suggestion.label}" into the composer.`,
      ['Adjust the wording if needed, then choose Plan first or Run safe steps.'],
      'neutral'
    );
  }

  clearChat(): void {
    this.chatMessages = [
      {
        id: this.newId(),
        role: 'assistant',
        title: 'Fresh chat started',
        body: 'Tell me the outcome you want. I will turn it into structured action.',
        at: new Date(),
        status: 'neutral',
      },
    ];
    this.plan = undefined;
  }

  copyPlanToComposer(plan: ITaskPlanHistoryItem): void {
    this.planForm.patchValue({
      request: plan.request,
      projectKey: plan.projectKey || this.planForm.value.projectKey,
      successCriteria: (plan.intake.successCriteria || []).join('\n'),
    });
    this.addAssistantMessage('Loaded a recent plan back into the chat.', ['You can refine it or run the safe steps.'], 'neutral');
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  goControlCenter(): void {
    this.router.navigate(['/control-center']);
  }

  openFullWorkspace(): void {
    this.router.navigate(['/control-center']);
  }

  openFrameworkRegistry(): void {
    this.router.navigate(['/framework-registry']);
  }

  visibleFrameworks(decision?: IFrameworkSelectionDecision): ISelectedFramework[] {
    return (decision?.selected || []).slice(0, 3);
  }

  additionalFrameworkCount(decision?: IFrameworkSelectionDecision): number {
    return Math.max(0, (decision?.selected?.length || 0) - 3);
  }

  structuredValidationCriteria(plan: ICompletionPlan | undefined = this.plan): IValidationCriterionResult[] {
    if (!plan) {
      return [];
    }

    const recorded = (plan.validationResult?.criteria || []).map((result) => ({
      ...result,
      evidence: result.evidence || [],
    }));
    const seen = new Set(
      recorded.map((result) => this.validationCriterionKey(result.kind, result.criterion))
    );
    const planned: Array<{ kind: IValidationCriterionResult['kind']; criteria: string[] }> = [
      {
        kind: 'task_success',
        criteria: plan.validationPlan?.successCriteria || [],
      },
      {
        kind: 'framework_evidence',
        criteria: plan.validationPlan?.frameworkEvidenceRequirements || [],
      },
      {
        kind: 'framework_completion',
        criteria: plan.validationPlan?.frameworkCompletionCriteria || [],
      },
      {
        kind: 'framework_assurance',
        criteria: plan.validationPlan?.frameworkAssuranceCriteria || [],
      },
    ];

    const unrecorded = planned.flatMap((group) =>
      group.criteria
        .filter((criterion) => {
          const key = this.validationCriterionKey(group.kind, criterion);
          if (seen.has(key)) {
            return false;
          }
          seen.add(key);
          return true;
        })
        .map((criterion) => ({
          criterion,
          kind: group.kind,
          status: 'not_run',
          evidence: [],
        }))
    );

    return [...recorded, ...unrecorded];
  }

  validationCount(status: 'passed' | 'failed' | 'not_run' | 'not_applicable'): number {
    return this.structuredValidationCriteria().filter(
      (criterion) => this.validationCriterionStatus(criterion.status) === status
    ).length;
  }

  validationStatusLabel(): string {
    const criteria = this.structuredValidationCriteria();
    const taskGates = criteria.filter(
      (criterion) => this.validationCriterionStatus(criterion.status) !== 'not_applicable'
    );
    if (!taskGates.length || taskGates.every((criterion) => this.validationCriterionStatus(criterion.status) === 'not_run')) {
      return 'Not run';
    }
    if (taskGates.some((criterion) => this.validationCriterionStatus(criterion.status) === 'failed')) {
      return 'Failed';
    }
    if (taskGates.every((criterion) => this.validationCriterionStatus(criterion.status) === 'passed')) {
      return 'Passed';
    }
    return 'Not fully run';
  }

  validationStatusClass(): string {
    const status = this.validationStatusLabel();
    if (status === 'Passed') {
      return 'validation-state--passed';
    }
    if (status === 'Failed') {
      return 'validation-state--failed';
    }
    return 'validation-state--not-run';
  }

  validationCriterionStatus(status?: string): 'passed' | 'failed' | 'not_run' | 'not_applicable' {
    if (status === 'passed' || status === 'failed' || status === 'not_applicable') {
      return status;
    }
    return 'not_run';
  }

  validationKindLabel(kind?: string): string {
    switch (kind) {
      case 'task_success':
        return 'Task success';
      case 'framework_evidence':
        return 'Framework evidence';
      case 'framework_completion':
        return 'Framework completion';
      case 'framework_assurance':
        return 'Framework assurance';
      case 'system_check':
        return 'System check';
      default:
        return 'Validation gate';
    }
  }

  openValidationEvidence(): void {
    this.inspectorMode = 'evidence';
  }

	previewApprovedReviewRecovery(): void {
		this.runApprovedReviewReconciliation(false);
	}

	applyApprovedReviewRecovery(): void {
		this.runApprovedReviewReconciliation(true);
	}

	private runApprovedReviewReconciliation(apply: boolean): void {
		this.reconcilingReviews = true;
		this.taskPlanService.reconcileApprovedReviews({
			apply,
			confirmation: apply ? 'RECONCILE APPROVED TASKS' : undefined,
			olderThanMinutes: 30,
			limit: 50,
		}).pipe(timeout(this.operationTimeoutMs)).subscribe({
			next: (result) => {
				this.reconcilingReviews = false;
				this.reconciliation = result;
				const summary = result.eligible
					? `${result.completed} verified complete; ${result.returnedToReview} returned to review; ${result.conflicts} changed concurrently.`
					: 'No approved tasks are old enough to reconcile.';
				this.addAssistantMessage(
					apply ? 'Approved-task recovery applied.' : 'Approved-task recovery preview ready.',
					[summary, 'Recovery never repeats the external action.'],
					result.conflicts ? 'warning' : 'neutral'
				);
				this.notification.success(apply ? 'Recovery applied' : 'Recovery preview ready', summary);
				if (apply) {
					this.loadLogs();
					this.loadReviewQueue();
				}
			},
			error: () => {
				this.reconcilingReviews = false;
				this.notification.error('Recovery unavailable', 'HAI did not change or repeat any task. Reload the review queue and inspect audit evidence.');
			},
		});
	}

  openResourceSchedule(): void {
    this.inspectorMode = 'plan';
  }

  reviewQueueOpenCount(): number {
		return this.reviewQueue.filter((item) => item.status === 'open' || item.status === 'needs_review').length;
  }

	approvedReviewCount(): number {
		return this.reviewQueue.filter((item) => item.status === 'approved').length;
	}

	canResolveReview(item: IReviewQueueItem): boolean {
		return item.status === 'open' || item.status === 'needs_review';
	}

  latestLog(): ITaskPlanHistoryItem | undefined {
    return this.logs[0];
  }

  contextUsedCount(): number {
    const planContext = (this.plan?.contextPlan?.usedContext?.length || 0) + (this.plan?.contextPlan?.sourceContext?.length || 0);
    const cycleContext = this.lastCommand?.agentCycle?.appliedContext?.length || 0;
    return planContext + cycleContext;
  }

  safeModeLabel(): string {
    return this.plan?.riskAssessment?.approvalRequired ? 'Approval gate active' : 'Safe mode';
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle();
  }

  themeLabel(): string {
    return this.themeService.label();
  }

  themeIcon(): string {
    return this.themeService.icon();
  }

  toolRuntimeEvidenceUri(tool?: IToolExecutionResult): string {
    return tool?.launchEventId ? `automation-launch://${tool.launchEventId}` : '';
  }

  toolRuntimeEvidenceLabel(tool?: IToolExecutionResult): string {
    return this.toolRuntimeEvidenceUri(tool) || 'No persisted launch-event id';
  }

  toolRuntimeEvidenceTitle(tool?: IToolExecutionResult): string {
    if (!tool?.launchEventId) {
      return 'This execution did not return a persisted launch-event id.';
    }
    return this.toolRuntimeRouteSummary(tool) || 'Exact runtime evidence URI for this controlled tool execution.';
  }

  toolRuntimeRouteSummary(tool?: IToolExecutionResult): string {
    const trace = tool?.runtimeRouteTrace;
    if (!trace) {
      return '';
    }
    const parts = [
      trace.runtimeId ? `Runtime ${trace.runtimeId}` : '',
      trace.intent ? `Intent ${trace.intent}` : '',
      trace.executionMode ? `Mode ${trace.executionMode}` : '',
      trace.riskLevel ? `Risk ${trace.riskLevel}` : '',
      this.compactTraceList('Skills', trace.recommendedSkills, 3),
      this.compactTraceList('Maps', trace.relevantMaps, 2),
      this.compactTraceList('Blocked', trace.blockedSurfaces, 2),
    ].filter(Boolean);
    return parts.join(' | ');
  }

  compactTraceList(label: string, values?: string[], limit = 3): string {
    const cleaned = (values || []).map((value) => value.trim()).filter(Boolean);
    if (!cleaned.length) {
      return '';
    }
    const visible = cleaned.slice(0, limit).join(', ');
    const remainder = cleaned.length > limit ? ` +${cleaned.length - limit}` : '';
    return `${label} ${visible}${remainder}`;
  }

  copyToolRuntimeEvidence(tool?: IToolExecutionResult): void {
    const uri = this.toolRuntimeEvidenceUri(tool);
    if (!uri) {
      this.notification.warning('Runtime evidence unavailable', 'This run did not return a launch-event id.');
      return;
    }
    if (!navigator.clipboard) {
      this.notification.info('Copy manually', uri);
      return;
    }
    navigator.clipboard.writeText(uri).then(
      () => this.notification.success('Runtime evidence copied', uri),
      () => this.notification.info('Copy manually', uri)
    );
  }

  composerPlaceholder(): string {
    return 'Ask HAI anything or give a command. Example: prepare my next follow-up, clear blockers, draft a reply, plan today.';
  }

  planSummaryBullets(plan: ICompletionPlan): string[] {
    return [
      `Goal: ${plan.realGoal || plan.request}`,
      `Risk: ${plan.riskAssessment.level || plan.intake.riskLevel}; approval ${plan.riskAssessment.approvalRequired ? 'required' : 'not required'}.`,
      `Model: ${plan.modelDecision.selectedModelName || 'not selected'} (${plan.modelDecision.tier || 'unknown tier'}).`,
      `Next: ${plan.validationResult.nextAction || plan.completionStatus || 'review the plan'}.`,
    ];
  }

  commandActionSummary(): string {
    if (!this.lastCommand?.actions?.length) {
      return 'No assistant command has run yet.';
    }
    return this.lastCommand.actions.map((action) => `${action.name}: ${action.status}`).join(' | ');
  }

  agentCycleStepCount(): number {
    return this.lastCommand?.agentCycle?.steps?.length || 0;
  }

  pursuitStateSummary(state?: IAgentCyclePursuitOperatingState): string {
    if (!state) {
      return 'No pursuit operating state returned yet.';
    }
    const lane = state.primaryLane || 'monitor';
    const attention = state.attentionTotal || 0;
    return `${attention} attention item${attention === 1 ? '' : 's'} in ${lane}`;
  }

  pursuitStateMetrics(state?: IAgentCyclePursuitOperatingState): string {
    if (!state) {
      return '';
    }
    return [
      `Robert ${state.needsRobert || 0}`,
      `ready ${state.readyToMove || 0}`,
      `stuck ${state.stuck || 0}`,
      `review ${state.reviewDue || 0}`,
      `planning ${state.planningNeeded || 0}`,
      `completion ${state.completionCandidates || 0}`,
    ].join(' | ');
  }

  commandReviewLabel(): string {
    if (!this.lastCommand) {
      return 'No command yet';
    }
    return this.lastCommand.reviewRequired ? 'Review needed' : 'No review needed';
  }

  statusTone(value?: string): string {
    const normalized = String(value || '').toLowerCase();
    if (normalized.includes('high') || normalized.includes('blocked') || normalized.includes('fail')) {
      return 'red';
    }
    if (normalized.includes('medium') || normalized.includes('review') || normalized.includes('approval')) {
      return 'orange';
    }
    return 'green';
  }

  trackById(_: number, item: { id?: string }): string {
    return item.id || String(_);
  }

  criteria(): string[] {
    return String(this.planForm.value.successCriteria || '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);
  }

  openCommandPursuit(): void {
    const id = this.lastCommand?.pursuit?.pursuitId;
    if (id) {
      this.router.navigate(['/pursuits'], { queryParams: { selected: id } });
    }
  }

  hasPursuitContext(): boolean {
    return Boolean(String(this.planForm.value.pursuitId || '').trim());
  }

  pursuitContextLabel(): string {
    return this.hasPursuitContext() ? 'Selected pursuit' : 'No pursuit selected';
  }

  pursuitContextTooltip(): string {
    const pursuitId = String(this.planForm.value.pursuitId || '').trim();
    return pursuitId
      ? `Open the selected pursuit (${pursuitId}) and inspect its evidence, workflow, and decisions.`
      : 'Set an optional pursuit context to keep this task attempt on its durable pursuit ledger.';
  }

  openPursuitContext(): void {
    const pursuitId = String(this.planForm.value.pursuitId || '').trim();
    if (pursuitId) {
      this.router.navigate(['/pursuits'], { queryParams: { selected: pursuitId } });
      return;
    }
    this.contextExpanded = true;
  }

  private messageFromCommand(command: IAssistantCommandResult, intent: ChatIntent): ChatMessage {
    const plan = command.plan;
    const blocked = Boolean(command.reviewRequired || (plan?.riskAssessment?.approvalRequired && !plan?.riskAssessment?.approvalGranted));
    const bullets = plan ? this.planSummaryBullets(plan) : [];
    if (command.agentCycle) {
      bullets.push(`Cycle: ${command.agentCycle.status}; ${command.agentCycle.nextAction || 'no immediate human action'}.`);
      if (command.agentCycle.pursuitOperatingState) {
        bullets.push(`Pursuits: ${this.pursuitStateSummary(command.agentCycle.pursuitOperatingState)}; ${this.pursuitStateMetrics(command.agentCycle.pursuitOperatingState)}.`);
        bullets.push(`Pursuit action: ${command.agentCycle.pursuitOperatingState.primaryAction || 'Continue scheduled pursuit monitoring.'}`);
      }
    }
    if (command.actions?.length) {
      bullets.push(`Engines: ${command.actions.map((action) => `${action.name} ${action.status}`).join(', ')}.`);
    }
    if (command.pursuit) {
      const pursuit = command.pursuit;
      if (pursuit.awaitingAcceptance) {
        bullets.push('Pursuit: ' + (pursuit.title || pursuit.pursuitId || 'new candidate') + ' needs explicit acceptance before HAI creates a task, workflow, or execution attempt.');
      } else if (pursuit.executionQueued) {
        bullets.push(`Pursuit: ${pursuit.title || pursuit.pursuitId || 'governed workflow'} is queued for the controlled worker.`);
      } else if (pursuit.matches?.length) {
        bullets.push(`Pursuit matches: ${pursuit.matches.map((match) => match.pursuit.title).join(', ')}.`);
      }
    }
    return {
      id: this.newId(),
      role: 'assistant',
      title:
        command.pursuit?.awaitingAcceptance
          ? 'Pursuit candidate recorded'
          : command.pursuit?.executionQueued
          ? 'Governed workflow queued'
          : intent === 'cycle'
          ? 'Assistant cycle completed'
          : intent === 'run'
          ? 'Success engine result'
          : 'Completion-first plan ready',
      body: command.summary || (blocked ? 'I prepared the work, but approval is needed before risky execution.' : 'I prepared the next structured action.'),
      at: new Date(),
      status: blocked ? 'warning' : 'success',
      bullets,
    };
  }

  private commandErrorBody(intent: ChatIntent): string {
    if (intent === 'cycle') {
      return 'The assistant command bridge did not return a cycle result. No approval-gated action was bypassed.';
    }
    if (intent === 'run') {
      return 'The success engine did not return a result. I kept the task unexecuted.';
    }
    return 'The planner did not return a result. No action was taken.';
  }

  private singleLine(value: unknown): string {
    return String(value || '').replace(/\s+/g, ' ').trim();
  }

  private addAssistantMessage(body: string, bullets: string[] = [], status: ChatMessage['status'] = 'neutral'): ChatMessage {
    return this.addMessage({
      role: 'assistant',
      body,
      bullets,
      at: new Date(),
      status,
    });
  }

  private addMessage(message: Omit<ChatMessage, 'id'> & { id?: string }): ChatMessage {
    const next = { ...message, id: message.id || this.newId() };
    this.chatMessages = [...this.chatMessages, next];
    return next;
  }

  private replaceMessage(id: string, message: Omit<ChatMessage, 'id'> & { id?: string }): void {
    const next = { ...message, id: message.id || id };
    this.chatMessages = this.chatMessages.map((existing) => (existing.id === id ? next : existing));
  }

  private newId(): string {
    return `chat-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }

  private validationCriterionKey(kind: string | undefined, criterion: string | undefined): string {
    return `${String(kind || '').trim().toLowerCase()}::${String(criterion || '')
      .trim()
      .replace(/\s+/g, ' ')
      .toLowerCase()}`;
  }

  private normalizePlan(plan: ICompletionPlan): ICompletionPlan {
    const safe = plan as any;
    safe.modelDecision = safe.modelDecision || {};
    safe.toolDecision = safe.toolDecision || {};
    safe.minimalityDecision = safe.minimalityDecision || {};
    safe.contextPlan = safe.contextPlan || {};
    if (safe.frameworkDecision) {
      safe.frameworkDecision.selected = safe.frameworkDecision.selected || [];
      safe.frameworkDecision.approvalReasons = safe.frameworkDecision.approvalReasons || [];
    }
    safe.intake = safe.intake || {};
    safe.riskAssessment = safe.riskAssessment || {};
    safe.validationPlan = safe.validationPlan || {};
    safe.validationResult = safe.validationResult || {};
    safe.executionPlan = safe.executionPlan || {};
    safe.retryPolicy = safe.retryPolicy || {};
    safe.modelDecision.skipped = safe.modelDecision.skipped || [];
    safe.toolDecision.selectedTools = safe.toolDecision.selectedTools || [];
    safe.toolDecision.skippedTools = safe.toolDecision.skippedTools || [];
    safe.toolDecision.blockedTools = safe.toolDecision.blockedTools || [];
    safe.minimalityDecision.ladder = safe.minimalityDecision.ladder || [];
    safe.contextPlan.usedContext = safe.contextPlan.usedContext || [];
    safe.contextPlan.sourceContext = safe.contextPlan.sourceContext || [];
    safe.contextPlan.ragflowCandidates = safe.contextPlan.ragflowCandidates || [];
    safe.intake.successCriteria = safe.intake.successCriteria || [];
    safe.steps = safe.steps || [];
    safe.riskAssessment.reasons = safe.riskAssessment.reasons || [];
    safe.riskAssessment.missingParameters = safe.riskAssessment.missingParameters || [];
    safe.validationPlan.steps = safe.validationPlan.steps || [];
    safe.validationPlan.successCriteria = safe.validationPlan.successCriteria || [];
    safe.validationPlan.frameworkEvidenceRequirements = safe.validationPlan.frameworkEvidenceRequirements || [];
    safe.validationPlan.frameworkCompletionCriteria = safe.validationPlan.frameworkCompletionCriteria || [];
    safe.validationPlan.frameworkAssuranceCriteria = safe.validationPlan.frameworkAssuranceCriteria || [];
    safe.validationResult.checked = safe.validationResult.checked || [];
    safe.validationResult.failures = safe.validationResult.failures || [];
    safe.validationResult.criteria = safe.validationResult.criteria || [];
    safe.executionPlan.approvalRequiredFor = safe.executionPlan.approvalRequiredFor || [];
    safe.executionPlan.auditEvents = safe.executionPlan.auditEvents || [];
		safe.executionPlan.capacityConstraints = safe.executionPlan.capacityConstraints || [];
		safe.calendarCapacity = safe.calendarCapacity || {
			status: 'unavailable',
			windowStart: safe.createdAt,
			windowEnd: safe.createdAt,
			busyIntervals: [],
			explanation: 'Calendar capacity was not recorded for this older plan.'
		};
		safe.calendarCapacity.busyIntervals = safe.calendarCapacity.busyIntervals || [];
		if (safe.resourceDecision) {
			safe.resourceDecision.scheduled = safe.resourceDecision.scheduled || [];
			safe.resourceDecision.unscheduledTaskIds = safe.resourceDecision.unscheduledTaskIds || [];
			safe.resourceDecision.criticalBlockers = safe.resourceDecision.criticalBlockers || [];
			safe.resourceDecision.advisories = safe.resourceDecision.advisories || [];
			safe.resourceDecision.approvalFlags = safe.resourceDecision.approvalFlags || [];
		}
    safe.retryPolicy.escalationPath = safe.retryPolicy.escalationPath || [];
    safe.retryPolicy.escalateWhen = safe.retryPolicy.escalateWhen || [];
    safe.memoryUpdateProposals = safe.memoryUpdateProposals || [];
    safe.lessonsLearned = safe.lessonsLearned || [];
    safe.storedMemoryIds = safe.storedMemoryIds || [];
    safe.events = safe.events || [];
    if (plan.executionResult) {
      plan.executionResult.actions = plan.executionResult.actions || [];
      plan.executionResult.claims = plan.executionResult.claims || [];
    }
    return safe as ICompletionPlan;
  }
}

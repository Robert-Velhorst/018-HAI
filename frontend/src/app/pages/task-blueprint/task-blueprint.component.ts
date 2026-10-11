import { ChangeDetectionStrategy, Component, Inject, OnInit, QueryList, ViewChildren } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { forkJoin } from 'rxjs';
import { timeout } from 'rxjs/operators';
import {
  ICompletionPlan,
	IApprovedReviewReconciliationResult,
  IReviewQueueItem,
  IToolExecutionResult,
  IValidationCriterionResult,
} from '../../models/task-plan.model.interface';
import {
  IFrameworkSelectionDecision,
  ISelectedFramework,
} from '../../models/framework-registry.model.interface';
import { IAssistantCommandRequest, IAssistantCommandResult } from '../../models/assistant-command.model.interface';
import { IAgentCyclePursuitOperatingState } from '../../models/agent-cycle.model.interface';
import { AssistantCommandService } from '../../services/assistant-command.service';
import { TASK_PLAN_SERVICE_TOKEN } from '../../services/task-plan/task-plan.service.token';
import { ITaskPlanService } from '../../services/task-plan.service.interface';
import { IPydanticAIResponse } from '../../models/pydantic-ai.model.interface';
import { PydanticAIService } from '../../services/pydantic-ai.service';
import { ICrewAIResponse } from '../../models/crewai.model.interface';
import { CrewAIService } from '../../services/crewai.service';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';
import { outcomeRecord, readSafeOutcomeState, receiptConsistentWithCompletion, safeOutcomeText, safeReceiptSummary } from '../../models/safe-outcome.model.interface';

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

interface UncertainCommand {
  intent: 'run' | 'cycle';
  request: IAssistantCommandRequest;
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-task-blueprint',
    templateUrl: './task-blueprint.component.html',
    styleUrls: ['./task-blueprint.component.scss'],
    standalone: false
})
export class TaskBlueprintComponent implements OnInit {
	readonly moduleId = 'task-blueprint';
	@ViewChildren(HaiProgressiveSectionComponent) private progressiveSections!: QueryList<HaiProgressiveSectionComponent>;
	private readonly taskOperationRetryConfirmation = 'RETRY UNCERTAIN OPERATION';
  private plannedRequestFingerprint = '';
  private reviewedPlanFingerprint = '';
  private restrictedExecutions: { plan: ICompletionPlan; request: IAssistantCommandRequest }[] = [];
  private planRequest?: IAssistantCommandRequest;
  plan?: ICompletionPlan;
  lastCommand?: IAssistantCommandResult;
  logs: ICompletionPlan[] = [];
  reviewQueue: IReviewQueueItem[] = [];
  logsUnavailable = false;
  reviewQueueUnavailable = false;
  loading = false;
  running = false;
  cycling = false;
  reviewQueueLoading = false;
  resolvingReviewId = '';
	reviewConfirmationId = '';
	retryCheckLoading = false;
	retryConfirmationOpen = false;
	uncertainCommand?: UncertainCommand;
	statusAnnouncement = '';
	showAllBasicApprovals = false;
	reconcilingReviews = false;
	recoveryConfirmationOpen = false;
	reconciliation?: IApprovedReviewReconciliationResult;
	reconciliationPreviewAt = 0;
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
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 20000;
	private logsRequestId = 0;
	private historyLoading = false;
	private reviewQueueRequestId = 0;

  chatMessages: ChatMessage[] = [
    {
      id: 'welcome',
      role: 'system',
      title: 'Start a task conversation',
      body: 'Describe the outcome you want. HAI returns a real plan before you choose whether to run permitted steps. Consequential actions remain approval-gated.',
      at: new Date(),
      status: 'neutral',
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
    request: ['', [Validators.required]],
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
    private pydanticAIService: PydanticAIService,
    private crewAIService: CrewAIService,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  get commandInFlight(): boolean {
    return this.loading || this.running || this.cycling;
  }

  ngOnInit(): void {
    this.route.queryParamMap.subscribe((params) => {
      const pursuitId = params.has('pursuitId')
        ? params.get('pursuitId') || ''
        : this.planForm.value.pursuitId;
      this.planForm.patchValue({
        pursuitId,
        projectKey: params.get('projectKey') || this.planForm.value.projectKey,
        request: params.get('request') || this.planForm.value.request,
        mandateId: params.get('mandateId') || this.planForm.value.mandateId,
      });
      if (params.has('pursuitId') && pursuitId) {
        this.contextExpanded = true;
        this.setViewMode('advanced');
        this.viewPreferences.setSection(this.moduleId, 'task-context', true);
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

  setViewMode(mode: 'basic' | 'advanced'): void {
    this.viewPreferences.setMode(this.moduleId, mode);
    this.statusAnnouncement = `${mode === 'advanced' ? 'Advanced' : 'Basic'} view selected for Task Blueprint.`;
  }

  canRunSafeSteps(): boolean {
    const hasPermittedStep = Boolean(this.plan?.steps?.some((step) => step.allowed && !step.requiresApproval));
    const blockedByClarification = this.plan?.riskAssessment?.actionResolution === 'clarify'
      || this.plan?.riskAssessment?.actionResolution === 'block';
    return Boolean(
      this.plan
      && hasPermittedStep
      && !blockedByClarification
      && !(this.plan.riskAssessment?.missingRequiredAgents || []).length
      && this.plannedRequestFingerprint
      && this.plannedRequestFingerprint === this.currentRequestFingerprint()
      && this.reviewedPlanFingerprint === this.plannedRequestFingerprint
      && !this.commandInFlight
      && !this.uncertainCommand
      && !this.retryCheckLoading
      && !this.historyLoading && !this.logsUnavailable
      && !this.executionSafetyPlan()
    );
  }

  runActionHint(): string {
    if (this.commandInFlight) return 'HAI is working. The request and run controls are paused until it returns.';
    if (this.executionSafetyPlan()) return this.executionSafetyNotice();
    if (this.historyLoading || this.logsUnavailable) return 'Task history must be available before another run or cycle. Refresh history and inspect any recorded restrictions.';
    if (this.uncertainCommand || this.retryCheckLoading) return 'The previous run outcome is unknown. Review its history before another attempt.';
    if (!this.plan || !this.plannedRequestFingerprint) return '1. Describe the outcome. 2. Choose Plan first; planning does not execute work.';
    if (this.plannedRequestFingerprint !== this.currentRequestFingerprint()) {
      return 'The request or task context changed. Plan the current version again before running it.';
    }
    if (this.plan.riskAssessment?.actionResolution === 'clarify' || this.plan.riskAssessment?.actionResolution === 'block') {
      return `${this.riskGateHint()} ${this.planPreflightHints().join(' ')}`.trim();
    }
    if ((this.plan.riskAssessment?.missingRequiredAgents || []).length) {
      return `Required participants need verification: ${this.plan.riskAssessment.missingRequiredAgents!.join(', ')}. Refresh the plan after resolving these prerequisites.`;
    }
    if (!this.plan.steps?.some((step) => step.allowed && !step.requiresApproval)) {
      return 'This plan has no steps currently permitted for a safe run. Review its approval or blocking requirements.';
    }
    const reviewHint = this.reviewedPlanFingerprint !== this.plannedRequestFingerprint
      ? 'Plan prepared. Review the full plan before running permitted steps; approval-gated steps stay blocked.'
      : 'Plan reviewed. Running rechecks current policy and runtime prerequisites; approval-gated steps remain blocked.';
    return `${this.planPreflightHints().join(' ')} ${reviewHint}`.trim();
  }

  modelSelectionLabel(): string {
    return this.plan?.modelDecision?.selectedModelName?.trim()
      || this.plan?.modelDecision?.selectedModelId?.trim()
      || (this.plan ? 'No model selected' : 'No routing result yet');
  }

  planPreflightHints(): string[] {
    if (!this.plan) return [];
    const hints: string[] = [];
    const executionBlock = this.recordedExecutionBlockHint();
    if (executionBlock) hints.push(executionBlock);
    const model = this.plan.modelDecision;
    if (!model?.selectedModelId?.trim()) {
      hints.push('No model route is selected. Review LLM policy and refresh the plan before model-backed work. Governed deterministic reads remain subject to backend policy and confirmed runtime metadata; a missing model alone does not establish that they are blocked.');
    }
    const framework = this.plan.frameworkDecision;
    if (!framework?.selected?.length) {
      hints.push('No operating framework selection is recorded. Review the framework registry and refresh the plan; this is not proof of execution readiness.');
    }
    const risk = this.plan.riskAssessment;
    if (typeof risk?.frameworkAutonomyCeiling === 'number'
      && typeof risk.requiredFrameworkAutonomy === 'number'
      && risk.frameworkAutonomyCeiling < risk.requiredFrameworkAutonomy) {
      hints.push(`Framework authority level ${risk.frameworkAutonomyCeiling} is below required level ${risk.requiredFrameworkAutonomy}. Re-plan with suitable authority; approval does not raise the ceiling.`);
    }
    if (risk?.missingRequiredAgents?.length) {
      hints.push(`Required participants need verification: ${risk.missingRequiredAgents.join(', ')}. Assign and verify every required participant, then refresh the plan; approval alone does not resolve this prerequisite.`);
    }
    if (risk?.missingParameters?.length) {
      hints.push(`Missing execution details: ${risk.missingParameters.join(', ')}. Resolve these details and refresh the plan.`);
    }
    return hints;
  }

  recordedExecutionBlockHint(): string {
    const result = this.plan?.executionResult;
    if (result?.mode !== 'blocked') return '';
    if (this.hasUncertainExecution()) return this.executionSafetyNotice();
    return result.blockedReason?.trim()
      ? `Recorded execution block: ${result.blockedReason.trim()}`
      : 'Execution was blocked, but no reason was recorded. Inspect task evidence before another attempt.';
  }

  hasUncertainExecution(plan: ICompletionPlan | undefined = this.plan): boolean {
    const result = plan?.executionResult;
    if (result === undefined || result === null) return false;
    const record = outcomeRecord(result);
    if (!record || this.safeOutcomeUncertain(record)) return true;
    if (result.actions !== undefined && !Array.isArray(result.actions)) return true;
    return this.toolOutcomeUncertain(result.toolExecution)
      || Boolean(result.actions?.some((action) => !outcomeRecord(action) || (action.name === 'automation.launch'
        && !['completed', 'reused', 'blocked', 'needs_approval'].includes(this.singleLine(action.status).toLowerCase()))));
  }

  private safeOutcomeUncertain(value: unknown): boolean {
    const record = outcomeRecord(value);
    if (!record) return true;
    const state = readSafeOutcomeState(record);
    return record['outcomeUncertain'] === true
      || (record['outcomeUncertain'] !== undefined && typeof record['outcomeUncertain'] !== 'boolean')
      || state.interrupted === true || state.reconciliationRequired === true || state.outcomeRecorded === false
      || !receiptConsistentWithCompletion(state.receipt);
  }

  private toolOutcomeUncertain(tool?: IToolExecutionResult): boolean {
    if (tool === undefined || tool === null) return false;
    if (this.safeOutcomeUncertain(tool)) return true;
    if (tool.requiresApproval !== undefined && typeof tool.requiresApproval !== 'boolean') return true;
    if (this.singleLine(tool.status).toLowerCase() === 'completed' && tool.requiresApproval === true) return true;
    return !['completed', 'blocked', 'needs_approval'].includes(this.singleLine(tool.status).toLowerCase());
  }

  toolExecutionStatusLabel(tool: IToolExecutionResult, plan: ICompletionPlan | undefined = this.plan): string {
    const status = safeOutcomeText(tool.status, 128).trim() || 'No tool status returned';
    return this.toolOutcomeUncertain(tool) || this.hasUncertainExecution(plan)
      ? `${status}; outcome uncertain`
      : status;
  }

  private executionRetryBlocked(plan?: ICompletionPlan): boolean {
    if (!plan) return false;
    const policy = outcomeRecord(plan.retryPolicy);
    return this.hasUncertainExecution(plan)
      || (plan.retryPolicy !== undefined && plan.retryPolicy !== null && !policy)
      || (policy?.['retryAvailable'] !== undefined && typeof policy['retryAvailable'] !== 'boolean')
      || policy?.['retryAvailable'] === false;
  }

  executionSafetyPlan(request: IAssistantCommandRequest = this.composerRequest()): ICompletionPlan | undefined {
    const candidates = [
      ...(this.plan && this.executionRetryBlocked(this.plan)
        && (this.matchesCommand(this.planRequest || this.requestFromPlan(this.plan), request)
          || !this.singleLine(request.message)) ? [this.plan] : []),
      ...this.restrictedExecutions.filter((entry) => this.matchesCommand(entry.request, request)
        || !this.singleLine(request.message)).map((entry) => entry.plan),
      ...this.logs.filter((plan) => this.matchesUncertainRequest(plan, request) && this.executionRetryBlocked(plan)),
    ];
    return candidates.find((plan) => this.hasUncertainExecution(plan)) || candidates[0];
  }

  executionSafetyNotice(plan: ICompletionPlan | undefined = this.executionSafetyPlan()): string {
    if (!plan) return '';
    const notice = this.hasUncertainExecution(plan)
      ? 'Partial effects may have occurred. Completion is not confirmed. Inspect the receipt, audit trail and destination; another attempt is paused to prevent duplicates.'
      : 'Backend retry policy does not permit another attempt. Inspect the recorded result and resolve its restrictions before running again.';
    const result = outcomeRecord(plan.executionResult);
    const tool = outcomeRecord(result?.['toolExecution']);
    const receipts = [result, tool].filter((value): value is Record<string, unknown> => !!value)
      .map((value) => safeReceiptSummary(readSafeOutcomeState(value))).filter(Boolean);
    return [notice, ...receipts].join(' ');
  }

  private rememberExecutionSafety(plan?: ICompletionPlan, request?: IAssistantCommandRequest): void {
    if (!plan || !this.executionRetryBlocked(plan)) return;
    const identity = request || this.requestFromPlan(plan);
    const previous = this.restrictedExecutions.find((entry) => this.matchesCommand(entry.request, identity)
      && entry.plan.id === plan.id);
    if (!previous) this.restrictedExecutions.push({ plan, request: identity });
    else if (this.hasUncertainExecution(plan) || !this.hasUncertainExecution(previous.plan)) previous.plan = plan;
  }

  private rememberUncertainHistory(): void {
    // Retain restrictions across refreshes; a new planning result is not reconciliation.
    this.logs.forEach((plan) => this.rememberExecutionSafety(plan));
  }

  private validHistory(logs: unknown): logs is ICompletionPlan[] {
    return Array.isArray(logs) && logs.every((plan) => {
      const record = outcomeRecord(plan);
      return !!record && typeof record['request'] === 'string' && !!this.singleLine(record['request'])
        && ['projectKey', 'pursuitId'].every((key) => record[key] === undefined || typeof record[key] === 'string');
    });
  }

  private matchesUncertainRequest(plan: ICompletionPlan, request: IAssistantCommandRequest): boolean {
    return this.matchesCommand(this.requestFromPlan(plan), request);
  }

  private requestFromPlan(plan: ICompletionPlan): IAssistantCommandRequest {
    return { message: this.singleLine(plan.request), projectKey: this.singleLine(plan.projectKey), pursuitId: this.singleLine(plan.pursuitId) };
  }

  private matchesCommand(left: IAssistantCommandRequest, right: IAssistantCommandRequest): boolean {
    return !!this.singleLine(left.message) && this.singleLine(left.message) === this.singleLine(right.message)
      && this.singleLine(left.projectKey) === this.singleLine(right.projectKey)
      && this.singleLine(left.pursuitId) === this.singleLine(right.pursuitId);
  }

  canRetryUncertainCommand(): boolean {
    const uncertain = this.uncertainCommand;
    return Boolean(uncertain && !this.historyLoading && !this.logsUnavailable
      && !this.executionSafetyPlan(uncertain.request) && !this.logs.some((plan) =>
      this.matchesUncertainRequest(plan, uncertain.request) && this.executionRetryBlocked(plan)
    ));
  }

  riskGateHint(): string {
    if (this.hasUncertainExecution()) return this.executionSafetyNotice();
    const executionBlock = this.recordedExecutionBlockHint();
    if (executionBlock) return executionBlock;
    const risk = this.plan?.riskAssessment;
    if (risk?.actionResolution === 'block') return 'Execution blocked by risk policy. Resolve the block and refresh the plan; approval alone does not remove it.';
    if (risk?.actionResolution === 'clarify') return 'More execution detail is needed. Resolve the missing information and refresh the plan.';
    if (risk?.missingRequiredAgents?.length) return 'Execution is blocked until every required participant is verified; approval alone is not sufficient.';
    if (risk?.approvalRequired && !risk.approvalGranted) return 'Approval required before risky execution; runtime prerequisites are checked separately.';
    if (risk?.approvalRequired && risk.approvalGranted) return 'Approval recorded for this plan; it does not override policy or runtime prerequisites.';
    return 'No approval requirement recorded; execution policy and runtime prerequisites still apply.';
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
        this.showInspector('typed-proposal');
        this.addSystemMessage(
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
    this.openTaskContext();
    this.showInspector('overview');
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
          this.showInspector('crew-proposal');
          this.addSystemMessage(
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
    this.openTaskContext();
    this.showInspector('overview');
    this.notification.info(
      'Criteria copied',
      'Review the copied criteria and use Plan first when ready. No task has been executed.'
    );
  }

  submitChat(intent: ChatIntent, confirmedRetry?: IAssistantCommandRequest): void {
    if (this.commandInFlight) {
      return;
    }
    const safetyPlan = this.executionSafetyPlan(confirmedRetry || this.composerRequest());
    if (intent !== 'plan' && (safetyPlan || (confirmedRetry && !this.canRetryUncertainCommand()))) {
      this.notification.warning('Another attempt is paused', this.executionSafetyNotice(safetyPlan) || 'The recorded outcome or retry policy does not permit another attempt. Inspect task history and audit evidence.');
      return;
    }
    if (intent !== 'plan' && (this.historyLoading || this.logsUnavailable)) {
      this.notification.warning('Task history needs inspection', this.runActionHint());
      return;
    }
    if (intent === 'run' && !confirmedRetry && !this.canRunSafeSteps()) {
      this.notification.warning('Review a permitted plan first', this.runActionHint());
      return;
    }
    if (intent !== 'plan' && this.uncertainCommand && !confirmedRetry) {
		this.notification.warning(
			'Check the earlier attempt first',
			'HAI cannot safely repeat a run whose outcome is unknown. Refresh task history and explicitly confirm a separate attempt.'
		);
		return;
	}
    if (!confirmedRetry && this.planForm.invalid) {
      Object.values(this.planForm.controls).forEach((control) => {
        control.markAsDirty();
        control.updateValueAndValidity();
      });
      this.addSystemMessage(
        'I need a clear request first.',
        ['Write the outcome you want, then I can plan or run the safe steps.'],
        'warning'
      );
      return;
    }

    const requestText = String(this.planForm.value.request || '').trim();
		const request: IAssistantCommandRequest = confirmedRetry
			? { ...confirmedRetry, successCriteria: [...(confirmedRetry.successCriteria || [])] }
			: {
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
		this.plannedRequestFingerprint = '';
		this.reviewedPlanFingerprint = '';
		this.plan = undefined;
		this.planRequest = undefined;
		this.lastCommand = undefined;
		this.reconciliation = undefined;
		this.reconciliationPreviewAt = 0;
    this.addMessage({
      role: 'user',
      body: request.message,
      at: new Date(),
      status: 'neutral',
    });

    const workingMessage = this.addSystemMessage(
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

    this.assistantCommandService.command(request).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (command) => {
		if (intent !== 'plan') this.uncertainCommand = undefined;
        this.lastCommand = command;
		this.plan = command.plan ? this.normalizePlan(command.plan) : undefined;
        this.planRequest = this.plan ? request : undefined;
        this.rememberExecutionSafety(this.plan, request);
		if (intent === 'plan' && this.plan) this.plannedRequestFingerprint = this.requestFingerprint(request);
        this.loading = false;
        this.running = false;
        this.cycling = false;
        this.inspectorMode = 'overview';
        this.replaceMessage(workingMessage.id, this.messageFromCommand({ ...command, plan: this.plan }, intent));
        this.loadLogs();
        this.loadReviewQueue();
      },
      error: () => {
        this.loading = false;
        this.running = false;
        this.cycling = false;
		if (intent !== 'plan') {
			this.uncertainCommand = { intent, request };
		}
        this.replaceMessage(workingMessage.id, {
          role: 'system',
          title: 'Request did not return a confirmed result',
          body: this.commandErrorBody(intent),
          at: new Date(),
          status: 'blocked',
          bullets: this.commandFailureBullets(intent),
        });
        this.notification.error(
          'Error',
          intent === 'cycle'
            ? 'Failed to run assistant command cycle.'
            : intent === 'run'
            ? 'Failed to run task success engine.'
            : 'Failed to create task plan.'
        );
		this.loadLogs();
		this.loadReviewQueue();
      },
    });
  }

  loadLogs(): void {
    const requestId = ++this.logsRequestId;
    this.historyLoading = true;
    this.taskPlanService.logs().pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (logs) => {
        if (requestId !== this.logsRequestId) return;
        this.historyLoading = false;
        if (!this.validHistory(logs)) {
          this.logsUnavailable = true;
          return;
        }
        this.logs = logs.map((plan) => this.normalizePlan(plan));
        this.rememberUncertainHistory();
        this.logsUnavailable = false;
      },
      error: () => {
		if (requestId === this.logsRequestId) {
          this.historyLoading = false;
          this.logsUnavailable = true;
        }
	},
    });
  }

  loadReviewQueue(): void {
    const requestId = ++this.reviewQueueRequestId;
    this.reviewQueueLoading = true;
    this.taskPlanService.reviewQueue().pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (items) => {
        if (requestId !== this.reviewQueueRequestId) return;
        if (!Array.isArray(items)) {
			this.reviewQueueUnavailable = true;
			this.reviewQueueLoading = false;
			return;
		}
		this.reviewQueue = items;
        this.reviewQueueUnavailable = false;
        this.reviewQueueLoading = false;
      },
      error: () => {
		if (requestId !== this.reviewQueueRequestId) return;
        this.reviewQueueUnavailable = true;
        this.reviewQueueLoading = false;
      },
    });
  }

	refreshHistoryBeforeUncertainRetry(): void {
		const uncertain = this.uncertainCommand;
		if (!uncertain || this.retryCheckLoading || this.retryConfirmationOpen || this.commandInFlight) return;
		this.retryCheckLoading = true;
		const logsRequestId = ++this.logsRequestId;
		this.historyLoading = true;
		const queueRequestId = ++this.reviewQueueRequestId;
		this.reviewQueueLoading = true;
		forkJoin({
			logs: this.taskPlanService.logs().pipe(timeout(this.loadTimeoutMs)),
			reviewQueue: this.taskPlanService.reviewQueue().pipe(timeout(this.loadTimeoutMs)),
		}).subscribe({
			next: ({ logs, reviewQueue }) => {
				if (logsRequestId !== this.logsRequestId || queueRequestId !== this.reviewQueueRequestId) {
          if (logsRequestId === this.logsRequestId) {
            this.historyLoading = false;
            this.logsUnavailable = true;
          }
          if (queueRequestId === this.reviewQueueRequestId) this.reviewQueueLoading = false;
          this.retryCheckLoading = false;
          return;
        }
				this.retryCheckLoading = false;
				this.reviewQueueLoading = false;
				if (!this.validHistory(logs) || !Array.isArray(reviewQueue)) {
					this.historyLoading = false;
					this.logsUnavailable = true;
					this.reviewQueueUnavailable = true;
					this.notification.error('History could not be verified', 'HAI kept the run paused. Refresh history and inspect the outcome before trying again.');
					return;
				}
				this.historyLoading = false;
				this.logs = logs.map((plan) => this.normalizePlan(plan));
				this.reviewQueue = reviewQueue;
				this.logsUnavailable = false;
				this.reviewQueueUnavailable = false;
        this.rememberUncertainHistory();
        if (!this.canRetryUncertainCommand()) {
          this.notification.warning('Another attempt is paused', this.executionSafetyNotice(this.executionSafetyPlan(uncertain.request)));
          return;
        }
				this.retryConfirmationOpen = true;
				this.modal.confirm({
					nzTitle: 'Create a separate attempt?',
					nzContent: 'Task history and the pending approval queue were refreshed, but they may not prove whether an external action finished. Inspect the relevant audit trail or destination first. Confirm only if a separate attempt is safe; it will use the same request and context as the uncertain attempt.',
					nzOkText: 'Create separate attempt',
					nzCancelText: 'Keep paused',
					nzOnOk: () => {
						this.retryConfirmationOpen = false;
						this.submitChat(uncertain.intent, uncertain.request);
					},
					nzOnCancel: () => { this.retryConfirmationOpen = false; },
				});
			},
			error: () => {
				if (logsRequestId !== this.logsRequestId || queueRequestId !== this.reviewQueueRequestId) {
          if (logsRequestId === this.logsRequestId) {
            this.historyLoading = false;
            this.logsUnavailable = true;
          }
          if (queueRequestId === this.reviewQueueRequestId) this.reviewQueueLoading = false;
          this.retryCheckLoading = false;
          return;
        }
				this.historyLoading = false;
				this.retryCheckLoading = false;
				this.reviewQueueLoading = false;
				this.logsUnavailable = true;
				this.reviewQueueUnavailable = true;
				this.notification.error('History could not be verified', 'HAI kept the run paused. Refresh history and inspect the outcome before trying again.');
			},
		});
	}

  resolveReviewItem(item: IReviewQueueItem, approved: boolean): void {
		if (!this.canResolveReview(item)) return;
    if (approved && !this.canApproveReview(item)) return;
		if (approved && this.isOperationReview(item)) {
			this.reviewConfirmationId = item.id;
			this.modal.confirm({
				nzTitle: 'Retry this uncertain operation?',
				nzContent: 'Continue only after checking the audit trail and confirming the earlier attempt did not already produce the intended effect. HAI will create a separate durable operation; it will not resume or rewrite the old attempt.',
				nzOkText: 'Create new attempt',
				nzCancelText: 'Keep in review',
				nzOnOk: () => {
					this.reviewConfirmationId = '';
					this.performReviewResolution(
						item,
						true,
						'Operator reviewed the uncertain outcome and explicitly authorized a separate durable attempt.',
						this.taskOperationRetryConfirmation,
					);
				},
				nzOnCancel: () => { this.reviewConfirmationId = ''; },
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
		if (this.resolvingReviewId) return;
    if (approved && !this.canApproveReview(item)) return;
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
			if (!result?.item || result.item.id !== item.id) {
				this.reviewQueueUnavailable = true;
				this.notification.error('Decision status could not be confirmed', 'Refresh the queue before taking another action.');
				this.loadReviewQueue();
				return;
			}
			this.reviewQueue = this.reviewQueue.map((current) => current.id === item.id ? result.item : current);
			this.reviewQueueUnavailable = false;
			this.reconciliation = undefined;
			this.reconciliationPreviewAt = 0;
			const validated = result.plan?.completionStatus === 'validated' && result.plan.validationResult?.passed === true && !this.hasUncertainExecution(result.plan);
			const resolvedPlan = result.plan ? this.normalizePlan(result.plan) : undefined;
			if (resolvedPlan) {
          this.plan = resolvedPlan;
          this.planRequest = this.requestFromPlan(resolvedPlan);
        }
        this.rememberExecutionSafety(resolvedPlan);
			this.addSystemMessage(
				!approved
					? 'Review item rejected.'
					: validated
						? 'Approved task validated.'
						: 'Approval recorded; run result needs review.',
				resolvedPlan ? this.planSummaryBullets(resolvedPlan) : [approved ? 'Approval was recorded. The API did not return a run plan, so completion is not confirmed.' : 'The task remains blocked and will not execute.'],
				!approved ? 'blocked' : validated ? 'success' : 'warning'
			);
			if (!approved) {
				this.notification.success('Review rejected', 'The task remains blocked.');
			} else if (validated) {
				this.notification.success('Task validated', 'The returned plan reports validated completion.');
			} else {
				this.notification.warning('Run needs review', 'Approval was recorded, but the returned result does not confirm validated completion.');
			}
          this.loadLogs();
          this.loadReviewQueue();
        },
        error: () => {
          this.resolvingReviewId = '';
			this.reviewQueueUnavailable = true;
          this.notification.error('Error', 'Failed to resolve review item.');
			this.loadReviewQueue();
        },
      });
  }

	isOperationReview(item: IReviewQueueItem): boolean {
		return Boolean(item?.taskId?.startsWith('operation:'));
	}

	reviewApproveLabel(item: IReviewQueueItem): string {
		return this.isOperationReview(item) ? 'Retry as new attempt' : 'Approve & run';
	}

	reviewRejectLabel(item: IReviewQueueItem): string {
		return this.isOperationReview(item) ? 'Close without retry' : 'Reject';
	}

  useSuggestion(suggestion: SuggestedPrompt): void {
    this.planForm.patchValue({
      request: suggestion.prompt,
      successCriteria: suggestion.criteria,
    });
	this.statusAnnouncement = `${suggestion.label} added to your request. Review it before submitting.`;
  }

  clearChat(): void {
    if (this.commandInFlight) {
      return;
    }
    this.chatMessages = [
      {
        id: this.newId(),
        role: 'system',
        title: 'New conversation',
        body: 'Describe the outcome you want. HAI will show a plan before any action; consequential actions remain approval-gated.',
        at: new Date(),
        status: 'neutral',
      },
    ];
    this.plan = undefined;
    this.planRequest = undefined;
    this.lastCommand = undefined;
	this.plannedRequestFingerprint = '';
	this.reviewedPlanFingerprint = '';
	this.planForm.patchValue({ request: '', successCriteria: '' });
    this.typedProposal = undefined;
    this.crewProposal = undefined;
    this.inspectorMode = 'overview';
  }

  copyPlanToComposer(plan: ICompletionPlan): void {
    if (this.commandInFlight) return;
    const selected = this.normalizePlan(plan);
    this.plan = selected;
    this.planRequest = this.requestFromPlan(selected);
    this.lastCommand = undefined;
    this.plannedRequestFingerprint = '';
    this.reviewedPlanFingerprint = '';
    this.planForm.patchValue({
      request: selected.request,
      projectKey: selected.projectKey || '',
      pursuitId: selected.pursuitId || '',
      automationId: '',
      mandateId: '',
      includeRagflowCandidates: false,
      successCriteria: selected.intake.successCriteria.join('\n'),
    });
    this.rememberExecutionSafety(selected);
    const restricted = this.executionSafetyPlan();
    this.addSystemMessage(restricted ? this.executionSafetyNotice(restricted)
      : 'A saved plan was loaded for inspection. Plan the current request again before running it.',
      [safeOutcomeText(`Inspected task: ${selected.request || selected.id}`, 256),
        ...(selected.executionResult?.toolExecution ? [safeOutcomeText(`Launch receipt: ${this.toolRuntimeEvidenceLabel(selected.executionResult.toolExecution)}`, 256)] : [])],
      restricted ? 'warning' : 'neutral');
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
    if (this.hasUncertainExecution()) return 'Outcome uncertain';
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
    this.showInspector('evidence');
  }

	previewApprovedReviewRecovery(): void {
		this.runApprovedReviewReconciliation(false);
	}

	applyApprovedReviewRecovery(): void {
		if (this.recoveryConfirmationOpen) return;
		if (!this.canApplyApprovedReviewRecovery()) {
			this.notification.warning('Preview required', 'Refresh the recovery preview and review its eligible items before applying it.');
			return;
		}
		this.recoveryConfirmationOpen = true;
		this.modal.confirm({
			nzTitle: 'Apply this recovery preview?',
			nzContent: 'HAI will reconcile durable outcome evidence for the approved tasks in the preview. It will not repeat external actions. Uncertain results return to review.',
			nzOkText: 'Apply recovery',
			nzCancelText: 'Keep preview only',
			nzOnOk: () => {
				this.recoveryConfirmationOpen = false;
				this.runApprovedReviewReconciliation(true);
			},
			nzOnCancel: () => { this.recoveryConfirmationOpen = false; },
		});
	}

	canApplyApprovedReviewRecovery(): boolean {
		return Boolean(
			!this.reconcilingReviews && !this.recoveryConfirmationOpen &&
			this.reconciliation?.dryRun === true &&
			this.reconciliation.eligible > 0 &&
			Date.now() - this.reconciliationPreviewAt < 5 * 60 * 1000
		);
	}

	private runApprovedReviewReconciliation(apply: boolean): void {
		if (this.reconcilingReviews) return;
		if (apply && !this.canApplyApprovedReviewRecovery()) return;
		this.reconciliation = undefined;
		this.reconciliationPreviewAt = 0;
		this.reconcilingReviews = true;
		this.taskPlanService.reconcileApprovedReviews({
			apply,
			confirmation: apply ? 'RECONCILE APPROVED TASKS' : undefined,
			olderThanMinutes: 30,
			limit: 50,
		}).pipe(timeout(this.operationTimeoutMs)).subscribe({
			next: (result) => {
				this.reconcilingReviews = false;
				if (!result || result.dryRun !== !apply || typeof result.eligible !== 'number' || !Array.isArray(result.items)) {
					this.notification.error('Recovery result could not be confirmed', 'Refresh the review queue and preview recovery again.');
					return;
				}
				this.reconciliation = result;
				this.reconciliationPreviewAt = apply ? 0 : Date.now();
				const summary = result.eligible
					? `${result.completed} verified complete; ${result.returnedToReview} returned to review; ${result.conflicts} changed concurrently.`
					: 'No approved tasks are old enough to reconcile.';
				this.addSystemMessage(
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
    this.showInspector('plan');
  }

  reviewQueueOpenCount(): number {
		return this.reviewQueue.filter((item) => item.status === 'open' || item.status === 'needs_review').length;
  }

  pendingReviewItems(): IReviewQueueItem[] {
    return this.reviewQueue.filter((item) => this.canResolveReview(item));
  }

  basicApprovalItems(): IReviewQueueItem[] {
    const items = this.pendingReviewItems();
    return this.showAllBasicApprovals ? items : items.slice(0, 3);
  }

  toggleBasicApprovals(): void {
    this.showAllBasicApprovals = !this.showAllBasicApprovals;
  }

  needsOperatorDecision(): boolean {
    return Boolean(
      this.lastCommand?.reviewRequired ||
      (this.plan?.riskAssessment?.approvalRequired && !this.plan?.riskAssessment?.approvalGranted) ||
      this.plan?.riskAssessment?.actionResolution === 'clarify' ||
      this.plan?.riskAssessment?.actionResolution === 'block'
    );
  }

  nextDecisionLabel(): string {
    if (this.executionSafetyPlan()) return this.executionSafetyNotice();
    return this.recordedExecutionBlockHint() || this.lastCommand?.nextAction || this.plan?.validationResult?.nextAction || 'Review the returned plan before deciding what to do next.';
  }

	planPreviewSteps(): ICompletionPlan['steps'] {
		return (this.plan?.steps || []).slice(0, 3);
	}

	planPreviewSources(): ICompletionPlan['contextPlan']['sourceContext'] {
		return (this.plan?.contextPlan?.sourceContext || []).slice(0, 3);
	}

	planSourceCount(): number {
		return this.plan?.contextPlan?.sourceContext?.length || 0;
	}

	planMemoryCount(): number {
		return this.plan?.contextPlan?.usedContext?.length || 0;
	}

	planSourceTitle(item: ICompletionPlan['contextPlan']['sourceContext'][number]): string {
		return item?.extraction?.summary || item?.extraction?.text || 'Source record';
	}

	planSourceReference(item: ICompletionPlan['contextPlan']['sourceContext'][number]): string {
		return item?.extraction?.sourceLabel || item?.extraction?.sourceUri || 'Source linked in evidence';
	}

	onComposerKeydown(event: KeyboardEvent): void {
		if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && !event.shiftKey) {
			event.preventDefault();
			if (!this.commandInFlight) this.createPlan();
		}
	}

	preventComposerSubmit(event: Event): void {
		event.preventDefault();
	}

  currentPlanState(plan: ICompletionPlan | undefined = this.plan): string {
    if (this.hasUncertainExecution(plan)) return 'Outcome uncertain';
    if (plan?.completionStatus) {
      return plan.completionStatus;
    }
    return this.lastCommand ? `${this.lastCommand.intent} result received` : 'No result yet';
  }

	canResolveReview(item: IReviewQueueItem): boolean {
		return Boolean(
			!this.reviewQueueUnavailable &&
			!this.reviewQueueLoading &&
			!this.resolvingReviewId &&
			!this.reviewConfirmationId &&
			(item.status === 'open' || item.status === 'needs_review')
		);
	}

  canApproveReview(item: IReviewQueueItem): boolean {
    if (!this.canResolveReview(item)) return false;
    const plans = [this.plan, ...this.restrictedExecutions.map((entry) => entry.plan), ...this.logs];
    return !plans.some((plan) => plan && this.executionRetryBlocked(plan) && (
      plan.id === item.taskId || plan.reviewItemId === item.id
      || plan.reviewQueueItem?.id === item.id
      || (this.isOperationReview(item) && plan.operationId === item.taskId.slice('operation:'.length))
    ));
  }

  latestLog(): ICompletionPlan | undefined {
    return this.logs[0];
  }

  openLastActivity(): void {
    const latest = this.latestLog();
    if (latest) {
      this.copyPlanToComposer(latest);
      return;
    }
    this.showInspector('logs');
  }

  contextUsedCount(): number {
    const planContext = (this.plan?.contextPlan?.usedContext?.length || 0) + (this.plan?.contextPlan?.sourceContext?.length || 0);
    const cycleContext = this.lastCommand?.agentCycle?.appliedContext?.length || 0;
    return planContext + cycleContext;
  }

  safeModeLabel(): string {
    if (this.executionSafetyPlan()) return 'Execution needs inspection';
    return this.plan?.riskAssessment?.approvalRequired ? 'Approval gate active' : 'Safe mode';
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
      this.hasUncertainExecution(plan)
        ? 'Partial effects may have occurred. Inspect the receipt, audit trail and destination; completion is not confirmed.'
        : `Next: ${plan.validationResult.nextAction || plan.completionStatus || 'review the plan'}.`,
    ];
  }

  commandActionSummary(): string {
    if (this.hasUncertainExecution()) return 'Execution outcome uncertain; inspect the recorded actions and receipts.';
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
    if (this.executionSafetyPlan() || this.uncertainCommand) return 'Review needed';
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
    return ['validated', 'completed', 'passed', 'low', 'planned', 'ready', 'allowed', 'success', 'succeeded', 'proceed'].includes(normalized)
      ? 'green' : 'orange';
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
    this.openTaskContext();
  }

  toggleTaskContext(): void {
    const disclosure = this.progressiveSection('task-context');
    if (disclosure) {
      disclosure.toggle();
      this.contextExpanded = disclosure.open;
      return;
    }

    this.contextExpanded = !this.viewPreferences.get(this.moduleId).openSections['task-context'];
    this.viewPreferences.setSection(this.moduleId, 'task-context', this.contextExpanded);
  }

  showInspector(mode: TaskBlueprintComponent['inspectorMode']): void {
    this.inspectorMode = mode;
    if (mode === 'plan' && this.canReviewCurrentPlan()) {
      this.reviewedPlanFingerprint = this.plannedRequestFingerprint;
    }
    this.openAdvancedSection('task-inspector');
  }

  private canReviewCurrentPlan(): boolean {
    return Boolean(
      this.plan
      && this.plannedRequestFingerprint
      && this.plannedRequestFingerprint === this.currentRequestFingerprint()
    );
  }

  private currentRequestFingerprint(): string {
    return this.requestFingerprint(this.composerRequest());
  }

  private composerRequest(): IAssistantCommandRequest {
    return {
      message: this.planForm.value.request,
      projectKey: this.planForm.value.projectKey,
      pursuitId: this.planForm.value.pursuitId,
      automationId: this.planForm.value.automationId,
      mandateId: this.planForm.value.mandateId,
      successCriteria: this.criteria(),
      includeRagflowCandidates: Boolean(this.planForm.value.includeRagflowCandidates),
    };
  }

  private requestFingerprint(request: IAssistantCommandRequest): string {
    return JSON.stringify({
      message: this.singleLine(request.message),
      projectKey: this.singleLine(request.projectKey),
      pursuitId: this.singleLine(request.pursuitId),
      automationId: this.singleLine(request.automationId),
      mandateId: this.singleLine(request.mandateId),
      successCriteria: (request.successCriteria || []).map((criterion) => this.singleLine(criterion)).filter(Boolean),
      includeRagflowCandidates: Boolean(request.includeRagflowCandidates),
    });
  }

  private openTaskContext(): void {
    this.contextExpanded = true;
    this.openAdvancedSection('task-context');
  }

  private openAdvancedSection(sectionId: string): void {
		const currentPreferences = this.viewPreferences.get(this.moduleId);
		const shouldNavigateToSection = currentPreferences.mode !== 'advanced'
			|| currentPreferences.openSections[sectionId] !== true;
		this.viewPreferences.setMode(this.moduleId, 'advanced');
    const disclosure = this.progressiveSection(sectionId);
    if (disclosure) {
      disclosure.setOpen(true);
    } else {
      this.viewPreferences.setSection(this.moduleId, sectionId, true);
    }
    if (shouldNavigateToSection) this.focusProgressiveSection(sectionId);
  }

  private focusProgressiveSection(sectionId: string): void {
    if (typeof document === 'undefined') return;
    window.setTimeout(() => {
      const section = document.getElementById(sectionId);
      if (!section?.isConnected) return;
      section.scrollIntoView({ block: 'nearest' });
      section.querySelector<HTMLButtonElement>('.hai-progressive-section__summary')?.focus({ preventScroll: true });
    });
  }

  private progressiveSection(sectionId: string): HaiProgressiveSectionComponent | undefined {
    return this.progressiveSections?.find((section) => section.sectionId === sectionId);
  }

  private messageFromCommand(command: IAssistantCommandResult, intent: ChatIntent): ChatMessage {
    const plan = command.plan;
    const uncertain = this.hasUncertainExecution(plan);
    const blocked = Boolean(command.reviewRequired || plan?.executionResult?.mode === 'blocked' || (plan?.riskAssessment?.approvalRequired && !plan?.riskAssessment?.approvalGranted));
    const validated = plan?.completionStatus === 'validated' && plan.validationResult?.passed === true && !uncertain;
    const bullets = plan ? this.planSummaryBullets(plan) : [];
    if (command.agentCycle && !uncertain) {
      bullets.push(`Cycle: ${command.agentCycle.status}; ${command.agentCycle.nextAction || 'no immediate human action'}.`);
      if (command.agentCycle.pursuitOperatingState) {
        bullets.push(`Pursuits: ${this.pursuitStateSummary(command.agentCycle.pursuitOperatingState)}; ${this.pursuitStateMetrics(command.agentCycle.pursuitOperatingState)}.`);
        bullets.push(`Pursuit action: ${command.agentCycle.pursuitOperatingState.primaryAction || 'Continue scheduled pursuit monitoring.'}`);
      }
    }
    if (command.actions?.length && !uncertain) {
      bullets.push(`Engines: ${command.actions.map((action) => `${action.name} ${action.status}`).join(', ')}.`);
    }
    if (command.pursuit && !uncertain) {
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
        uncertain ? 'Execution outcome uncertain'
          : command.pursuit?.awaitingAcceptance
          ? 'Pursuit candidate needs acceptance'
          : command.pursuit?.executionQueued
          ? 'Governed workflow queued'
          : intent === 'cycle'
          ? 'Assistant cycle result'
          : intent === 'run'
          ? validated ? 'Task validated' : 'Run result received'
          : 'Plan prepared',
      body: uncertain
        ? 'Partial effects may have occurred. Completion is not confirmed. Inspect the receipt, audit trail and destination before any separate attempt.'
        : command.summary || (blocked ? 'I prepared the work, but approval is needed before risky execution.' : 'I prepared the next structured action.'),
      at: new Date(),
      status: uncertain || blocked ? 'warning' : validated ? 'success' : 'neutral',
      bullets,
    };
  }

  private commandErrorBody(intent: ChatIntent): string {
    if (intent === 'cycle') {
      return 'HAI did not return a confirmed cycle result. The cycle may have started before the connection failed; inspect its activity and audit history before retrying.';
    }
    if (intent === 'run') {
      return 'HAI did not return a confirmed run result. A permitted step may have started before the connection failed; inspect the task history before retrying.';
    }
    return 'HAI did not return a confirmed plan. No execution was requested by this planning action.';
  }

  private commandFailureBullets(intent: ChatIntent): string[] {
    const bullets = ['Check backend health and the task history before retrying.'];
    bullets.push(intent === 'plan'
      ? 'Planning did not request execution.'
      : 'The outcome is unknown; do not assume that no step started. Approval gates remain enforced.');
    return bullets;
  }

  private singleLine(value: unknown): string {
    return typeof value === 'string' ? value.replace(/\s+/g, ' ').trim() : '';
  }

  private addSystemMessage(body: string, bullets: string[] = [], status: ChatMessage['status'] = 'neutral'): ChatMessage {
    return this.addMessage({
      role: 'system',
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
    const safe = { ...plan } as any;
    safe.modelDecision = safe.modelDecision || {};
    safe.toolDecision = safe.toolDecision || {};
    safe.minimalityDecision = safe.minimalityDecision || {};
    safe.contextPlan = safe.contextPlan || {};
    if (safe.frameworkDecision) {
      safe.frameworkDecision.selected = safe.frameworkDecision.selected || [];
      safe.frameworkDecision.approvalReasons = safe.frameworkDecision.approvalReasons || [];
    }
    safe.intake = { ...outcomeRecord(safe.intake) };
    safe.riskAssessment = safe.riskAssessment || {};
    safe.validationPlan = safe.validationPlan || {};
    safe.validationResult = safe.validationResult || {};
    safe.executionPlan = safe.executionPlan || {};
    const malformedRetryPolicy = safe.retryPolicy !== undefined && safe.retryPolicy !== null
      && (!outcomeRecord(safe.retryPolicy) || (safe.retryPolicy.retryAvailable !== undefined
        && typeof safe.retryPolicy.retryAvailable !== 'boolean'));
    safe.retryPolicy = { ...outcomeRecord(safe.retryPolicy) };
    if (malformedRetryPolicy) safe.retryPolicy.retryAvailable = false;
    safe.modelDecision.skipped = safe.modelDecision.skipped || [];
    safe.toolDecision.selectedTools = safe.toolDecision.selectedTools || [];
    safe.toolDecision.skippedTools = safe.toolDecision.skippedTools || [];
    safe.toolDecision.blockedTools = safe.toolDecision.blockedTools || [];
    safe.minimalityDecision.ladder = safe.minimalityDecision.ladder || [];
    safe.contextPlan.usedContext = safe.contextPlan.usedContext || [];
    safe.contextPlan.sourceContext = safe.contextPlan.sourceContext || [];
    safe.contextPlan.ragflowCandidates = safe.contextPlan.ragflowCandidates || [];
    safe.intake.successCriteria = Array.isArray(safe.intake.successCriteria)
      ? safe.intake.successCriteria.filter((value: unknown) => typeof value === 'string') : [];
    safe.steps = safe.steps || [];
    safe.riskAssessment.reasons = safe.riskAssessment.reasons || [];
    safe.riskAssessment.missingRequiredAgents = Array.isArray(safe.riskAssessment.missingRequiredAgents)
      ? safe.riskAssessment.missingRequiredAgents.filter((value: unknown) => typeof value === 'string' && value.trim().length > 0)
      : [];
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
    if (plan.executionResult !== undefined && plan.executionResult !== null) {
      const result = outcomeRecord(plan.executionResult);
      const tool = outcomeRecord(result?.['toolExecution']);
      const uncertain = this.hasUncertainExecution(plan);
      const actions = result?.['actions'];
      const claims = result?.['claims'];
      safe.executionResult = {
        ...result,
        ...readSafeOutcomeState(result || {}),
        outcomeUncertain: uncertain,
        mode: safeOutcomeText(result?.['mode'], 128),
        output: safeOutcomeText(result?.['output']),
        blockedReason: safeOutcomeText(result?.['blockedReason']),
        verificationStatus: safeOutcomeText(result?.['verificationStatus'], 128),
        actions: Array.isArray(actions) ? actions.filter((action: unknown) => !!outcomeRecord(action)) : [],
        claims: Array.isArray(claims) ? claims : [],
        toolExecution: tool ? {
          ...tool,
          ...readSafeOutcomeState(tool),
          outcomeUncertain: this.toolOutcomeUncertain(tool as unknown as IToolExecutionResult),
          status: safeOutcomeText(tool['status'], 128),
          message: safeOutcomeText(tool['message']),
          output: safeOutcomeText(tool['output']),
          launchEventId: safeOutcomeText(tool['launchEventId'], 128),
          runtimeTaskId: safeOutcomeText(tool['runtimeTaskId'], 128),
          executionReference: safeOutcomeText(tool['executionReference'], 256),
        } : undefined,
      };
    }
    return safe as ICompletionPlan;
  }
}

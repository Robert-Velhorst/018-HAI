import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Inject, OnDestroy, OnInit, ViewEncapsulation } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { catchError, finalize, forkJoin, map, of, Subscription, take, timeout } from 'rxjs';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import {
  IPursuitMatchCandidate,
  IPursuitRoutedIntakeResult,
} from '../../models/pursuit.model.interface';
import { PursuitService } from '../../services/pursuit.service';
import {
  IWorkflowClaimRecoverySummary,
  IWorkflowDashboard,
  IWorkflowFrameworkSelectionDecision,
  IWorkflowFrameworkSelectionProvenance,
  IWorkflowItem,
  IWorkflowOpenLoopRunSummary,
  IWorkflowOverview,
  IWorkflowRecord,
  IWorkflowReminderProposal,
  IWorkflowReminderProposalSnapshot,
  IWorkflowReminderActivationDecisionRequest,
  IWorkflowReminderActivationHistoryItem,
  IWorkflowReminderActivationHistorySnapshot,
  IWorkflowReminderDeliveryAuthorization,
  IWorkflowReminderDeliveryHistory,
  IWorkflowReminderDeliveryRunSummary,
  IWorkflowRunResult,
  IWorkflowRunSummary,
} from '../../models/workflow.model.interface';
import { WORKFLOW_SERVICE_TOKEN } from '../../services/workflow/workflow.service.token';
import { IWorkflowService } from '../../services/workflow.service.interface';

type FrameworkProvenanceState = 'missing' | 'invalid' | 'recorded' | 'verified';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-workflow-engine',
    templateUrl: './workflow-engine.component.html',
    styleUrls: ['./workflow-engine.component.scss'],
    // Workflow uses broad element and layout selectors. Scope them to this
    // route instead of allowing a lazy-loaded stylesheet to affect HAI-wide UI.
    encapsulation: ViewEncapsulation.Emulated,
    standalone: false
})
export class WorkflowEngineComponent implements OnInit, OnDestroy {
  overview?: IWorkflowOverview;
  dashboard?: IWorkflowDashboard;
  reminderProposals?: IWorkflowReminderProposalSnapshot;
  reminderActivationHistory?: IWorkflowReminderActivationHistorySnapshot;
  reminderDeliveryHistory?: IWorkflowReminderDeliveryHistory;
  selectedReminder?: IWorkflowReminderProposal;
  reminderProposalsUnavailable = false;
  reminderActivationUnavailable = false;
  reminderDeliveryUnavailable = false;
  items: IWorkflowItem[] = [];
  approvalItems: IWorkflowItem[] = [];
  selected?: IWorkflowRecord;
  runSummary?: IWorkflowRunSummary;
  openLoopRunSummary?: IWorkflowOpenLoopRunSummary;
  recoverySummary?: IWorkflowClaimRecoverySummary;
  frameworkProvenance?: IWorkflowFrameworkSelectionProvenance;
  frameworkSelectionDecision?: IWorkflowFrameworkSelectionDecision;
  frameworkProvenanceState: FrameworkProvenanceState = 'missing';
  frameworkProvenanceIssues: string[] = [];
  frameworkSelectionLoading = false;
  frameworkSelectionUnavailable = false;
  pursuitMatches: IPursuitMatchCandidate[] = [];
  selectedPursuitMatch?: IPursuitMatchCandidate;
  includeArchived = false;
  loading = false;
  dataLoaded = false;
  loadFailed = false;
  openingWorkflowId?: string;
  workflowOpenError?: string;
  checklistAction?: { itemId: string; status: string };
  checklistActionError?: { itemId: string; message: string };
  saving = false;
  transitionReviewId?: string;
  transitionError?: string;
  private transitionSubscription?: Subscription;
  approvalReviewId?: string;
  approvalError?: string;
  private approvalSubscription?: Subscription;
  private interruptionSubscription?: Subscription;
  private proposalSubscription?: Subscription;
  private selectedRunSubscription?: Subscription;
  private reminderRunSubscription?: Subscription;
  private reminderAuthorizationSubscription?: Subscription;
  private reminderPreparationSubscription?: Subscription;
  private reminderDecisionSubscription?: Subscription;
  private workerRunSubscription?: Subscription;
  private recoveryRunSubscription?: Subscription;
  private followupRunSubscription?: Subscription;
  private pursuitMatchSubscription?: Subscription;
  private intakeSubscription?: Subscription;
  lastIntakePursuitId?: string;
  lastIntakeWorkflowId?: string;
  workerReviewRequired = false;
  workerReviewMessage?: string;
  workerReviewRefreshed = false;
  proposalAction?: {
    proposalId: string;
    status: 'approved' | 'changes_requested' | 'rejected';
  };
  matchingPursuits = false;
  runningAction?: 'refresh' | 'worker' | 'selected' | 'followups' | 'recovery' | 'reminders';
  private readonly operationTimeoutMs = 30000;
  lastOperation?: {
    name: string;
    status: 'completed' | 'failed';
    summary: string;
    details?: string;
    at: Date;
  };
  workflowSearch = '';
  stateFilter = 'all';
  riskFilter = 'all';
  activeQueue: 'all' | 'approval' | 'ready' | 'blocked' | 'review' = 'all';
  private frameworkSelectionLookup = 0;
  activationBusyId?: string;
  private refreshSubscription?: Subscription;
  private workflowOpenSubscription?: Subscription;
  private workflowOpenRequest = 0;
  private destroyed = false;
  private intakeChangesSubscription?: Subscription;

  intakeForm: FormGroup = this.fb.group({
    input: ['', [Validators.required, Validators.maxLength(4000)]],
    successCriteriaText: ['', [Validators.maxLength(5000)]],
    projectKey: [''],
    automationId: [''],
    sourceType: ['manual'],
    sourceId: [''],
    sourceUri: [''],
    sourceLabel: [''],
    contentType: ['note'],
    sender: [''],
    trigger: ['manual_intake'],
  });

  transitionForm: FormGroup = this.fb.group({
    targetState: ['ready', [Validators.required]],
    message: ['Move workflow to the selected state.'],
  });

  approvalForm: FormGroup = this.fb.group({
    note: ['Robert approved controlled workflow execution.'],
  });

  interruptionForm: FormGroup = this.fb.group({
    decision: ['retry', [Validators.required]],
    note: ['', [Validators.required]],
    evidenceUri: [''],
    evidenceLabel: [''],
    priorExecutionReconciled: [false],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(WORKFLOW_SERVICE_TOKEN) private workflowService: IWorkflowService,
    private pursuitService: PursuitService,
    private notification: NzNotificationService,
    private modal: NzModalService,
    private route: ActivatedRoute,
    private router: Router,
    private changeDetector: ChangeDetectorRef,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  ngOnInit(): void {
    this.refresh();
    this.intakeChangesSubscription = this.intakeForm.valueChanges.subscribe(() => {
      // A changed signal must be matched again; never link edited intake to a stale pursuit choice.
      this.selectedPursuitMatch = undefined;
      this.pursuitMatches = [];
      this.lastIntakePursuitId = undefined;
      this.lastIntakeWorkflowId = undefined;
      this.pursuitMatchSubscription?.unsubscribe();
      this.matchingPursuits = false;
    });
    const workflowId = this.route.snapshot.queryParamMap.get('workflowId');
    if (workflowId) {
      this.loadWorkflowRecord(workflowId);
    }
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.refreshSubscription?.unsubscribe();
    this.workflowOpenSubscription?.unsubscribe();
    this.intakeChangesSubscription?.unsubscribe();
    this.transitionSubscription?.unsubscribe();
    this.approvalSubscription?.unsubscribe();
    this.interruptionSubscription?.unsubscribe();
    this.proposalSubscription?.unsubscribe();
    this.selectedRunSubscription?.unsubscribe();
    this.reminderRunSubscription?.unsubscribe();
    this.reminderAuthorizationSubscription?.unsubscribe();
    this.reminderPreparationSubscription?.unsubscribe();
    this.reminderDecisionSubscription?.unsubscribe();
    this.workerRunSubscription?.unsubscribe();
    this.recoveryRunSubscription?.unsubscribe();
    this.followupRunSubscription?.unsubscribe();
    this.pursuitMatchSubscription?.unsubscribe();
    this.intakeSubscription?.unsubscribe();
  }

  refresh(showNotification = false, preserveLastOperation = false): void {
    if (this.destroyed || (this.runningAction && this.runningAction !== 'refresh')) {
      return;
    }
    this.refreshSubscription?.unsubscribe();
    const blockingRefresh = !preserveLastOperation;
    this.loading = true;
    this.reminderProposals = undefined;
    this.reminderActivationHistory = undefined;
    this.reminderDeliveryHistory = undefined;
    this.reminderProposalsUnavailable = false;
    this.reminderActivationUnavailable = false;
    this.reminderDeliveryUnavailable = false;
    this.loadFailed = false;
    if (blockingRefresh) {
      this.runningAction = 'refresh';
    }
    let received = false;
    const failed = () => {
      this.loadFailed = true;
      this.workerReviewRefreshed = false;
      this.reminderProposals = undefined;
      this.reminderActivationHistory = undefined;
      this.reminderDeliveryHistory = undefined;
      if (!preserveLastOperation) {
        this.lastOperation = { name: 'Refresh', status: 'failed', summary: 'Workflow panels could not be validated. Previous records are retained; refresh before acting.', at: new Date() };
      }
      this.notification.error('Workflow data unavailable', 'One or more workflow panels are missing, invalid or unavailable. Previous records remain visible but cannot authorize actions.');
    };
    this.refreshSubscription = forkJoin({
      overview: this.workflowService.overview(),
      dashboard: this.workflowService.dashboard(),
      reminderProposals: this.workflowService.reminderProposals().pipe(catchError(() => {
        this.reminderProposalsUnavailable = true;
        return of(undefined);
      })),
      reminderActivations: this.workflowService.reminderActivationHistory().pipe(catchError(() => {
        this.reminderActivationUnavailable = true;
        return of(undefined);
      })),
      reminderDeliveries: this.workflowService.reminderDeliveryHistory().pipe(catchError(() => {
        this.reminderDeliveryUnavailable = true;
        return of(undefined);
      })),
      items: this.workflowService.items(this.includeArchived),
      approvals: this.workflowService.approvals(),
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => {
      this.loading = false;
      if (blockingRefresh) this.runningAction = undefined;
    })).subscribe({
      next: ({ overview, dashboard, reminderProposals, reminderActivations, reminderDeliveries, items, approvals }) => {
        received = true;
        if (!this.validOperationalPanels(overview, dashboard, items, approvals)) { failed(); return; }
        this.overview = overview;
        this.dashboard = dashboard;
        const proposalsValid = this.validReminderProposalSnapshot(reminderProposals);
        const activationsValid = this.validReminderActivationHistory(reminderActivations);
        const deliveriesValid = this.validReminderDeliveryHistory(reminderDeliveries);
        this.reminderProposalsUnavailable ||= !!reminderProposals && !proposalsValid;
        this.reminderActivationUnavailable ||= !!reminderActivations && !activationsValid;
        this.reminderDeliveryUnavailable ||= !!reminderDeliveries && !deliveriesValid;
        this.reminderProposals = proposalsValid
          ? reminderProposals
          : undefined;
        this.reminderActivationHistory = activationsValid
          ? reminderActivations
          : undefined;
        this.reminderDeliveryHistory = deliveriesValid ? reminderDeliveries : undefined;
        if (this.selectedReminder) {
          this.selectedReminder = this.reminderProposals?.items.find(
            (proposal) => proposal.id === this.selectedReminder?.id
          );
        }
        this.items = items;
        this.approvalItems = approvals;
        this.dataLoaded = true;
        if (this.workerReviewRequired) this.workerReviewRefreshed = true;
        this.loading = false;
        if (blockingRefresh) {
          this.runningAction = undefined;
        }
        if (!preserveLastOperation) {
          this.lastOperation = {
            name: 'Refresh',
            status: 'completed',
            summary: `${items.length} workflows, ${approvals.length} approvals, ${dashboard.dueOpenLoops.length} due follow-ups, ${this.reminderProposals?.due || 0} due reminders.`,
            at: new Date(),
          };
        }
        if (showNotification) {
          this.notification.success(
            'Workflow data refreshed',
            `${items.length} workflows, ${approvals.length} approvals, ${dashboard.dueOpenLoops.length} due follow-ups, ${this.reminderProposals?.due || 0} due reminders.`
          );
        }
      },
      error: failed,
      complete: () => { if (!received) failed(); },
    });
  }

  private validOperationalPanels(
    overview: IWorkflowOverview, dashboard: IWorkflowDashboard,
    items: IWorkflowItem[], approvals: IWorkflowItem[]
  ): boolean {
    const states = ['new_input', 'classified', 'linked', 'checklist_generated', 'waiting_external_input',
      'needs_approval', 'ready', 'in_progress', 'completed', 'archived', 'blocked'];
    const strings = (values: unknown): values is string[] => Array.isArray(values) && values.every(value => typeof value === 'string');
    const reference = (value: unknown): value is string => typeof value === 'string' &&
      /^(?!00000000-0000-0000-0000-000000000000$)[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(value);
    const rules = (values: unknown): boolean => Array.isArray(values) && values.every(value => value &&
      typeof value.id === 'string' && typeof value.name === 'string' && typeof value.ruleKey === 'string' &&
      typeof value.description === 'string' && typeof value.category === 'string' && typeof value.enabled === 'boolean');
    const records = (values: unknown): boolean => {
      if (!Array.isArray(values)) return false;
      const ids = new Set<string>();
      return values.every(value => {
        if (!value || !reference(value.id) || ids.has(value.id.toLowerCase()) || typeof value.title !== 'string' ||
            !states.includes(value.currentState) || typeof value.requiresApproval !== 'boolean' ||
            typeof value.archived !== 'boolean' || !['not_required', 'pending', 'approved', 'rejected'].includes(value.approvalStatus)) return false;
        ids.add(value.id.toLowerCase()); return true;
      });
    };
    if (!overview || !strings(overview.states) || overview.states.length !== states.length ||
        new Set(overview.states).size !== states.length || !states.every(state => overview.states.includes(state)) ||
        !strings(overview.safetyRules) || !rules(overview.rules) || !Array.isArray(overview.capabilities) ||
        !overview.capabilities.every(value => value && typeof value.id === 'string' && typeof value.name === 'string' &&
          typeof value.status === 'string' && strings(value.implemented) && strings(value.next)) ||
        !dashboard || !dashboard.counts || typeof dashboard.counts !== 'object' || Array.isArray(dashboard.counts) ||
        !Object.values(dashboard.counts).every(value => Number.isSafeInteger(value) && value >= 0) || !rules(dashboard.rules) ||
        ![items, approvals, dashboard.approvalItems, dashboard.readyItems, dashboard.blockedItems,
          dashboard.highRiskItems, dashboard.itemsWithoutNextAction].every(records) || !Array.isArray(dashboard.dueOpenLoops)) return false;
    const loopIds = new Set<string>();
    return dashboard.dueOpenLoops.every(loop => {
      if (!loop || !reference(loop.id) || !reference(loop.workflowId) || loopIds.has(loop.id.toLowerCase()) ||
          typeof loop.status !== 'string' || typeof loop.responsibleParty !== 'string' ||
          typeof loop.waitingFor !== 'string' || typeof loop.nextAction !== 'string') return false;
      loopIds.add(loop.id.toLowerCase()); return true;
    });
  }

  intake(): void {
    if (this.destroyed || this.workerReviewRequired || this.intakeForm.invalid || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const formValue = this.intakeForm.getRawValue();
    const { successCriteriaText, ...request } = formValue;
    const successCriteria = String(successCriteriaText || '')
      .split(/\r?\n/)
      .map(value => value.trim())
      .filter(Boolean);
    if (successCriteria.length) request.successCriteria = successCriteria;
    if (typeof request.input !== 'string' || !request.input.trim()) return;
    const choice = this.selectedPursuitMatch;
    if (choice && (!this.pursuitMatches.includes(choice) || !this.validWorkerReference(choice.pursuit?.id))) return;
    const pursuitId = choice?.pursuit.id;
    const original = this.selected;
    const snapshot = JSON.stringify(formValue);
    this.saving = true;
    this.lastIntakePursuitId = undefined;
    this.lastIntakeWorkflowId = undefined;
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Intake');
    const operation = pursuitId
      ? this.pursuitService.intake(pursuitId, request).pipe(map((detail): IPursuitRoutedIntakeResult => ({
        mode: 'selected_existing', matched: true, createdCandidate: false, pursuitId, detail, workflowId: detail?.intakeWorkflowId,
      })))
      : this.pursuitService.routeIntake(request);
    this.intakeSubscription = operation.pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.saving = false; })).subscribe({
      next: (result) => {
        received = true;
        this.saving = false;
        const candidate = result?.mode === 'candidate_created';
        const matched = ['selected_existing', 'matched_existing', 'matched_candidate'].includes(result?.mode);
        const workflowId = result?.workflowId;
        const hasWorkflow = this.validWorkerReference(workflowId);
        const standalone = candidate && result.createdCandidate === false && result.matched === false && hasWorkflow &&
          !this.validWorkerReference(result.pursuitId) && !result.detail && !pursuitId &&
          (!result.pursuitId || result.pursuitId === '00000000-0000-0000-0000-000000000000');
        const needsAcceptance = (candidate && !standalone) || result?.mode === 'matched_candidate';
        if (!result || (!standalone && !this.validWorkerReference(result.pursuitId)) || (!candidate && !matched) ||
            result.matched !== matched || result.createdCandidate !== (candidate && !standalone) ||
            (pursuitId && result.pursuitId !== pursuitId) ||
            (matched && result.detail?.pursuit?.id !== result.pursuitId) ||
            (result.detail && result.detail.pursuit?.id !== result.pursuitId) ||
            (workflowId !== undefined && (!hasWorkflow || needsAcceptance)) ||
            (result.detail?.intakeWorkflowId !== undefined && (!hasWorkflow || result.detail.intakeWorkflowId !== workflowId))) { uncertain(); return; }
        if (snapshot === JSON.stringify(this.intakeForm.getRawValue()) && this.selected === original && this.selectedPursuitMatch === choice) {
          this.lastIntakePursuitId = standalone ? undefined : result.pursuitId;
          this.lastIntakeWorkflowId = hasWorkflow ? workflowId : undefined;
        }
        const summary = needsAcceptance
          ? 'The server returned a pursuit candidate for review. This acknowledgement does not prove workflow creation or task execution.'
          : hasWorkflow
            ? 'The server returned an exact intake workflow reference. Inspect its records and audit; this acknowledgement does not prove task execution.'
            : 'The server returned the associated pursuit. Inspect its records and intake audit; the response does not identify the exact newly created workflow.';
        this.lastOperation = { name: 'Intake response', status: 'completed', summary, at: new Date() };
        this.notification.info(needsAcceptance ? 'Pursuit candidate needs review' : 'Intake response received', summary);
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  matchPursuits(): void {
    if (this.destroyed || (this.anyActionRunning() && !this.matchingPursuits)) return;
    const value = this.intakeForm.value;
    if (typeof value.input !== 'string' || !value.input.trim()) {
      this.notification.error('Input required', 'Describe the signal before matching it to a pursuit.');
      return;
    }
    this.pursuitMatchSubscription?.unsubscribe();
    this.selectedPursuitMatch = undefined;
    this.pursuitMatches = [];
    const inputSnapshot = JSON.stringify(this.intakeForm.getRawValue());
    this.matchingPursuits = true;
    let received = false;
    const unavailable = () => {
      this.notification.error('Pursuit matching unavailable', 'No project selection was confirmed. Check the current input before trying again.');
    };
    this.pursuitMatchSubscription = this.pursuitService.match({
      input: value.input,
      projectKey: value.projectKey,
      sourceType: value.sourceType,
      sourceId: value.sourceId,
      sourceUri: value.sourceUri,
      limit: 5,
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.matchingPursuits = false; })).subscribe({
      next: (matches) => {
        received = true;
        if (this.destroyed || inputSnapshot !== JSON.stringify(this.intakeForm.getRawValue())) return;
        const ids = new Set<string>();
        if (!Array.isArray(matches) || matches.length > 5 || !matches.every(match => {
          const id = match?.pursuit?.id;
          if (!this.validWorkerReference(id) || ids.has(id.toLowerCase()) || typeof match.pursuit.title !== 'string' ||
              !Number.isFinite(match.score) || match.score < 0 || match.score > 1 ||
              typeof match.confidence !== 'string' || !Array.isArray(match.reasons) ||
              !match.reasons.every(reason => typeof reason === 'string')) return false;
          ids.add(id.toLowerCase()); return true;
        })) { unavailable(); return; }
        this.pursuitMatches = matches;
        this.matchingPursuits = false;
        if (!matches.length) {
          this.selectedPursuitMatch = undefined;
          this.notification.info('No pursuit match', 'No existing project was selected for this input.');
          return;
        }
        if (!this.selectedPursuitMatch && matches[0].score >= 0.7) {
          this.selectedPursuitMatch = matches[0];
        }
        // Matching can be initiated from the shared shell after a lazy route
        // change. Render the returned, selectable candidates immediately.
        this.changeDetector.detectChanges();
      },
      error: unavailable,
      complete: () => { if (!received) unavailable(); },
    });
  }

  selectPursuitMatch(match: IPursuitMatchCandidate): void {
    if (this.destroyed || this.matchingPursuits || !this.pursuitMatches.includes(match)) return;
    this.selectedPursuitMatch = match;
  }

  clearPursuitMatch(): void {
    this.pursuitMatchSubscription?.unsubscribe();
    this.matchingPursuits = false;
    this.selectedPursuitMatch = undefined;
  }

  open(item: IWorkflowItem): void {
    this.loadWorkflowRecord(item.id);
  }

  inspectIntakeWorkflow(): void {
    if (this.validWorkerReference(this.lastIntakeWorkflowId)) this.loadWorkflowRecord(this.lastIntakeWorkflowId!);
  }

  private loadWorkflowRecord(workflowId: string, onLoaded?: () => void): void {
    if (this.destroyed || this.saving || this.proposalAction || this.checklistAction || this.activationBusyId) return;
    this.workflowOpenSubscription?.unsubscribe();
    const request = ++this.workflowOpenRequest;
    this.selected = undefined;
    if (typeof workflowId !== 'string' || !this.isUuid(workflowId) || workflowId === '00000000-0000-0000-0000-000000000000') {
      this.openingWorkflowId = undefined;
      this.workflowOpenError = 'The workflow reference is invalid. Select a current workflow record.';
      this.notification.error('Workflow unavailable', this.workflowOpenError);
      return;
    }
    this.openingWorkflowId = workflowId;
    this.workflowOpenError = undefined;
    let received = false;
    this.workflowOpenSubscription = this.workflowService.get(workflowId)
      .pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => {
        if (request === this.workflowOpenRequest) this.openingWorkflowId = undefined;
      }))
      .subscribe({
        next: (record) => {
          if (this.destroyed || request !== this.workflowOpenRequest) return;
          received = true;
          if (typeof record?.item?.id !== 'string' || record.item.id.toLowerCase() !== workflowId.toLowerCase()) {
            this.workflowOpenError = 'The response did not match the requested workflow. Select the record again to refresh.';
            this.changeDetector.detectChanges();
            return;
          }
          if (this.transitionReviewId === workflowId &&
              (typeof record.item.currentState !== 'string' || !this.overview?.states?.includes(record.item.currentState))) {
            this.workflowOpenError = 'The affected workflow did not return a recognized state. Refresh it again before another change.';
            this.changeDetector.detectChanges();
            return;
          }
          this.openingWorkflowId = undefined;
          if (this.approvalReviewId === workflowId) {
            if (typeof record.item.currentState !== 'string' || !this.overview?.states?.includes(record.item.currentState) ||
                !['pending', 'approved', 'rejected', 'not_required'].includes(record.item.approvalStatus)) {
              this.workflowOpenError = 'The affected workflow did not return a recognized approval state. Refresh it again before another change.';
              this.changeDetector.detectChanges();
              return;
            }
            this.approvalReviewId = undefined;
            this.approvalError = undefined;
          }
          if (this.transitionReviewId === workflowId) {
            this.transitionReviewId = undefined;
            this.transitionError = undefined;
          }
          this.applyWorkflowRecord(record);
          onLoaded?.();
        },
        error: () => {
          if (this.destroyed || request !== this.workflowOpenRequest) return;
          this.openingWorkflowId = undefined;
          this.workflowOpenError = 'The workflow could not be opened. Check the connection, then select the row to retry.';
          this.changeDetector.detectChanges();
          this.notification.error('Workflow unavailable', 'The workflow record could not be loaded.');
        },
        complete: () => {
          if (!received && !this.destroyed && request === this.workflowOpenRequest) {
            this.workflowOpenError = 'No workflow record was returned. Select the record again to refresh.';
            this.changeDetector.detectChanges();
          }
        },
      });
  }

  transition(): void {
    if (this.destroyed || !this.selected || this.transitionForm.invalid || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const original = this.selected;
    const workflowId = original.item.id;
    const targetState = this.transitionForm.value.targetState;
    const message = this.transitionForm.value.message;
    if (!this.isUuid(workflowId) || workflowId === '00000000-0000-0000-0000-000000000000' ||
        typeof targetState !== 'string' || !this.overview?.states?.includes(targetState) || typeof message !== 'string') {
      this.notification.warning('Check transition', 'Use a current workflow, a listed target state and a message.');
      return;
    }
    this.saving = true;
    this.transitionError = undefined;
    let received = false;
    const unconfirmed = () => {
      this.transitionReviewId = workflowId;
      this.transitionError = 'The transition could not be confirmed. Refresh the affected workflow before another change.';
      this.notification.warning('Transition unconfirmed', this.transitionError);
    };
    this.transitionSubscription = this.workflowService.transition(workflowId, { targetState, message })
      .pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.saving = false; }))
      .subscribe({
      next: (record) => {
        received = true;
        if (this.destroyed) return;
        if (record?.item?.id !== workflowId || record.item.currentState !== targetState) {
          unconfirmed();
          return;
        }
        this.saving = false;
        if (this.selected === original) this.applyWorkflowRecord(record);
        this.notification.info('State response received', 'The server returned the requested workflow state. This is not proof of task execution or an independent audit.');
        this.refresh(false, true);
      },
      error: () => {
        if (!this.destroyed) unconfirmed();
      },
      complete: () => {
        if (!received && !this.destroyed) unconfirmed();
      },
    });
  }

  reviewTransition(): void {
    if (this.transitionReviewId && !this.destroyed && !this.anyActionRunning()) {
      this.loadWorkflowRecord(this.transitionReviewId);
    }
  }

  resolveApproval(item: IWorkflowItem, approved: boolean): void {
    if (this.destroyed || this.actionsUnavailable() || this.anyActionRunning() ||
        this.selected?.item !== item || typeof approved !== 'boolean' ||
        !this.isUuid(item.id) || item.id === '00000000-0000-0000-0000-000000000000' ||
        item.requiresApproval !== true || item.approvalStatus !== 'pending' || item.currentState !== 'needs_approval' ||
        this.hasOpenAutomationSelection(this.selected) || typeof this.approvalForm.value.note !== 'string') {
      return;
    }
    const original = this.selected;
    const workflowId = item.id;
    const decisionIds = new Set(original.decisions?.map(decision => decision.id) || []);
    const expectedDecision = approved ? 'approved' : 'rejected';
    this.saving = true;
    this.approvalError = undefined;
    let received = false;
    const unconfirmed = () => {
      this.approvalReviewId = workflowId;
      this.approvalError = 'The approval decision could not be confirmed. Refresh the affected workflow before another change.';
      this.notification.warning('Approval unconfirmed', this.approvalError);
    };
    this.approvalSubscription = this.workflowService.resolveApproval(workflowId, {
      approved,
      note: this.approvalForm.value.note,
      actor: 'operator',
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.saving = false; })).subscribe({
      next: (record) => {
        received = true;
        if (this.destroyed) return;
        const decision = Array.isArray(record?.decisions) && record.decisions.some(value =>
          typeof value?.id === 'string' && this.isUuid(value.id) && value.id !== '00000000-0000-0000-0000-000000000000' &&
          !decisionIds.has(value.id) && value.workflowId === workflowId && value.decisionType === 'approval' &&
          value.decision === expectedDecision && value.approved === approved &&
          typeof value.createdAt === 'string' && Number.isFinite(Date.parse(value.createdAt)));
        if (record?.item?.id !== workflowId || record.item.requiresApproval !== true || record.item.approvalStatus !== expectedDecision ||
            record.item.currentState !== (approved ? 'ready' : 'blocked') || !decision) {
          unconfirmed();
          return;
        }
        this.saving = false;
        if (this.selected === original) this.applyWorkflowRecord(record);
        this.notification.info('Decision response received', 'The server returned the requested approval decision and record. This is not proof of execution or independent audit persistence.');
        this.refresh(false, true);
      },
      error: () => {
        if (!this.destroyed) unconfirmed();
      },
      complete: () => {
        if (!received && !this.destroyed) unconfirmed();
      },
    });
  }

  reviewApproval(): void {
    if (this.approvalReviewId && !this.destroyed && !this.anyActionRunning()) this.loadWorkflowRecord(this.approvalReviewId);
  }

  openReminder(proposal: IWorkflowReminderProposal): void {
    this.selectedReminder = proposal;
  }

  closeReminder(): void {
    this.selectedReminder = undefined;
  }

  openReminderWorkflow(proposal: IWorkflowReminderProposal): void {
    if (this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const item = this.items.find((candidate) => candidate.id === proposal.workflowId);
    if (item) {
      this.open(item);
      this.closeReminder();
      return;
    }
    this.loadWorkflowRecord(proposal.workflowId, () => this.closeReminder());
  }

  activationFor(proposal: IWorkflowReminderProposal): IWorkflowReminderActivationHistoryItem | undefined {
    return this.reminderActivationHistory?.items.find(
      (item) => item.request.checklistItemId === proposal.checklistItemId
    );
  }

  deliveryAuthorizationFor(proposal: IWorkflowReminderProposal): IWorkflowReminderDeliveryAuthorization | undefined {
    const activation = this.activationFor(proposal);
    return activation ? this.reminderDeliveryHistory?.authorizations.find(
      (authorization) => authorization.activationRequestId === activation.request.id
    ) : undefined;
  }

  deliveryStatusFor(proposal: IWorkflowReminderProposal): string {
    const authorization = this.deliveryAuthorizationFor(proposal);
    if (!authorization) {
      return 'not_authorized';
    }
    const attempts = this.reminderDeliveryHistory?.attempts
      .filter((attempt) => attempt.authorizationId === authorization.id)
      .sort((left, right) => right.attemptNumber - left.attemptNumber) || [];
    return attempts[0]?.status || 'authorized_waiting';
  }

  canAuthorizeReminderDelivery(proposal: IWorkflowReminderProposal): boolean {
    const activation = this.activationFor(proposal);
    return !!activation?.current && activation.status === 'approved' && !!activation.latestDecision?.expiresAt &&
      this.validWorkerReference(activation.request.id) && this.validWorkerReference(activation.latestDecision.id) &&
      this.validDigest(activation.request.recordDigest) && this.validDigest(activation.latestDecision.recordDigest) &&
      this.reminderActivationHistory?.items.filter(item => item.request.id === activation.request.id).length === 1 &&
      activation.latestDecision.authority === 'reminder_activation_decision_only' &&
      activation.latestDecision.confirmation === 'APPROVE INTERNAL REMINDER PREPARATION' &&
      activation.latestDecision.decision === 'approved' && activation.latestDecision.activationRequestId === activation.request.id &&
      activation.latestDecision.activationRequestDigest === activation.request.recordDigest &&
      new Date(activation.latestDecision.expiresAt).getTime() > Date.now() && !this.deliveryAuthorizationFor(proposal);
  }

  authorizeReminderDelivery(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    const activation = this.activationFor(proposal);
    const decision = activation?.latestDecision;
    if (this.destroyed || this.workerReviewRequired || !activation || !decision || !this.canAuthorizeReminderDelivery(proposal) ||
        ![activation.request.id, activation.request.workflowId, activation.request.checklistItemId, decision.id].every(id => this.validWorkerReference(id)) ||
        ![activation.request.recordDigest, activation.request.reminderDigest, decision.recordDigest].every(value => this.validDigest(value)) ||
        this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const requestId = activation.request.id;
    const decisionId = decision.id;
    const requestDigest = activation.request.recordDigest;
    const reminderDigest = activation.request.reminderDigest;
    const decisionDigest = decision.recordDigest;
    const workflowId = activation.request.workflowId;
    const checklistItemId = activation.request.checklistItemId;
    const idempotencyKey = `ui:delivery:${requestId}:${decisionDigest.slice(0, 16)}`;
    let dispatched = false;
    this.modal.confirm({
      nzTitle: 'Authorize one internal HAI reminder?',
      nzContent: 'This permits one local in-app reminder only. It cannot send email, write Calendar data, call a provider, or execute a follow-up.',
      nzOkText: 'Authorize one reminder',
      nzCancelText: 'Cancel',
      nzOnOk: () => {
        const currentActivation = this.activationFor(proposal);
        if (dispatched || this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning() ||
            currentActivation?.request.id !== requestId || currentActivation.request.recordDigest !== requestDigest ||
            currentActivation.request.reminderDigest !== reminderDigest || currentActivation.request.workflowId !== workflowId ||
            currentActivation.request.checklistItemId !== checklistItemId || currentActivation.latestDecision?.id !== decisionId ||
            currentActivation.latestDecision.recordDigest !== decisionDigest ||
            !this.canAuthorizeReminderDelivery(proposal)) {
          return;
        }
        dispatched = true;
        let received = false;
        const uncertain = () => this.pauseWorkerForReview('Internal reminder authorization');
        this.activationBusyId = requestId;
        this.reminderAuthorizationSubscription = this.workflowService.authorizeReminderDelivery(requestId, {
          expectedActivationRequestDigest: requestDigest,
          expectedActivationDecisionDigest: decisionDigest,
          expectedReminderDigest: reminderDigest,
          idempotencyKey,
          channel: 'in_app',
          confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER',
        }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.activationBusyId = undefined; })).subscribe({
          next: (result) => {
            received = true;
            this.activationBusyId = undefined;
            const authorization = result?.authorization;
            if (result?.authority !== 'internal_reminder_delivery_authorization' || result?.canExecute !== false ||
                result?.deliveryAuthorized !== true || typeof result.replayed !== 'boolean' || !authorization ||
                !this.validWorkerReference(authorization.id) || authorization.activationRequestId !== requestId ||
                authorization.activationDecisionId !== decisionId || authorization.workflowId !== workflowId ||
                authorization.checklistItemId !== checklistItemId || authorization.channel !== 'in_app' ||
                authorization.authority !== result.authority || authorization.confirmation !== 'AUTHORIZE ONE INTERNAL HAI REMINDER' ||
                authorization.idempotencyKey !== idempotencyKey || authorization.reminderDigest !== reminderDigest ||
                authorization.activationRequestDigest !== requestDigest || authorization.activationDecisionDigest !== decisionDigest ||
                !this.validDigest(authorization.requestDigest) || !this.validDigest(authorization.recordDigest)) { uncertain(); return; }
            this.notification.info('Server reported internal reminder authorization', 'Inspect authorization and delivery receipts. No external effect was authorized and no delivery is proven by this response.');
            this.refresh(false, true);
          },
          error: uncertain,
          complete: () => { if (!received) uncertain(); },
        });
      },
    });
  }

  runDueReminderDeliveries(): void {
    if (this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning() || !this.reminderDeliveryHistory?.authorizations.length) {
      return;
    }
    this.runningAction = 'reminders';
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Internal reminder pass');
    this.reminderRunSubscription = this.workflowService.runDueReminderDeliveries({ limit: 25 }).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.runningAction = undefined; })
    ).subscribe({
      next: (summary) => {
        received = true;
        this.runningAction = undefined;
        if (!this.validReminderRunSummary(summary)) { uncertain(); return; }
        const message = `Server reported ${summary.delivered} internal signals, ${summary.retried} retries, ${summary.suppressed} suppressed, ${summary.deadLettered} dead-lettered and ${summary.expired} expired. Inspect delivery receipts; no external messages were authorized.`;
        this.lastOperation = { name: 'Internal reminder pass', status: 'completed', summary: message, at: new Date() };
        this.notification.info('Internal reminder response received', message);
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  private validReminderRunSummary(summary: IWorkflowReminderDeliveryRunSummary): boolean {
    if (!summary || !Array.isArray(summary.results) ||
        ![summary.checked, summary.delivered, summary.retried, summary.suppressed, summary.deadLettered, summary.expired]
          .every(value => Number.isSafeInteger(value) && value >= 0 && value <= 25) ||
        summary.checked !== summary.results.length ||
        summary.checked !== summary.delivered + summary.retried + summary.suppressed + summary.deadLettered + summary.expired) return false;
    const counts = { delivered: 0, retryable_failure: 0, suppressed: 0, dead_lettered: 0, expired: 0 };
    const ids = new Set<string>();
    for (const result of summary.results) {
      if (!result || !this.validWorkerReference(result.authorizationId) || ids.has(result.authorizationId.toLowerCase()) ||
          !Object.prototype.hasOwnProperty.call(counts, result.status)) return false;
      ids.add(result.authorizationId.toLowerCase()); counts[result.status]++;
    }
    return counts.delivered === summary.delivered && counts.retryable_failure === summary.retried &&
      counts.suppressed === summary.suppressed && counts.dead_lettered === summary.deadLettered && counts.expired === summary.expired;
  }

  canPrepareReminder(proposal: IWorkflowReminderProposal): boolean {
    const activation = this.activationFor(proposal);
    return !activation || ['rejected', 'revoked', 'expired', 'stale'].includes(activation.status);
  }

  prepareReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    if (this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning() || !this.canPrepareReminder(proposal) ||
        !this.validWorkerReference(proposal.checklistItemId) || !this.validWorkerReference(proposal.workflowId) || !this.validDigest(proposal.evidenceDigest)) {
      return;
    }
    const checklistItemId = proposal.checklistItemId;
    const workflowId = proposal.workflowId;
    const reminderDigest = proposal.evidenceDigest;
    this.activationBusyId = checklistItemId;
    const idempotencyKey = [
      'ui', 'internal-reminder', checklistItemId,
      reminderDigest.slice(0, 16), Date.now().toString(36),
    ].join(':');
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Internal reminder preparation');
    this.reminderPreparationSubscription = this.workflowService.prepareReminderActivation(checklistItemId, {
      expectedReminderDigest: reminderDigest,
      idempotencyKey,
      activationKind: 'internal_notification',
      confirmation: 'PREPARE INTERNAL REMINDER ONLY',
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.activationBusyId = undefined; })).subscribe({
      next: (result) => {
        received = true;
        this.activationBusyId = undefined;
        if (result?.authority !== 'reminder_activation_request_only' || result?.canExecute !== false ||
            typeof result.replayed !== 'boolean' || !this.validWorkerReference(result.request?.id) ||
            result.request?.activationKind !== 'internal_notification' ||
            result.request?.checklistItemId !== checklistItemId || result.request.workflowId !== workflowId ||
            result.request?.reminderDigest !== reminderDigest || result.request.idempotencyKey !== idempotencyKey ||
            result.request.authority !== result.authority || result.request.confirmation !== 'PREPARE INTERNAL REMINDER ONLY' ||
            !this.validDigest(result.request.requestDigest) || !this.validDigest(result.request?.recordDigest)) { uncertain(); return; }
        this.notification.info(
          result.replayed ? 'Server reported existing preparation' : 'Server reported internal reminder preparation',
          'Inspect preparation evidence. Owner approval and delivery authorization remain separate; this response does not prove delivery.'
        );
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  reviewReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    if (this.destroyed || this.actionsUnavailable() || this.anyActionRunning()) return;
    const activation = this.activationFor(proposal);
    if (!activation || !activation.current || this.activationBusyId) {
      return;
    }
    if (activation.status === 'approved') {
      this.modal.confirm({
        nzTitle: 'Revoke internal reminder preparation?',
        nzContent: 'This only records a revocation. No message or calendar action has been executed.',
        nzOkText: 'Revoke preparation',
        nzOkDanger: true,
        nzCancelText: 'Keep approval',
        nzOnOk: this.reminderDecisionConfirmation(activation, 'revoked'),
      });
      return;
    }
    if (this.workerReviewRequired || !['prepared', 'needs_clarification'].includes(activation.status)) {
      return;
    }
    this.modal.confirm({
      nzTitle: 'Approve this internal reminder preparation?',
      nzContent: 'Approval remains non-executing. A future effect would still require separate authorization and verification.',
      nzOkText: 'Approve preparation',
      nzCancelText: 'Cancel',
      nzOnOk: this.reminderDecisionConfirmation(activation, 'approved'),
    });
  }

  rejectReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    if (this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const activation = this.activationFor(proposal);
    if (!activation || !activation.current ||
        !['prepared', 'needs_clarification'].includes(activation.status) || this.activationBusyId) {
      return;
    }
    this.modal.confirm({
      nzTitle: 'Reject this internal reminder preparation?',
      nzContent: 'This appends a rejection decision only. No message, calendar event, provider call, or follow-up will run.',
      nzOkText: 'Reject preparation',
      nzOkDanger: true,
      nzCancelText: 'Cancel',
      nzOnOk: this.reminderDecisionConfirmation(activation, 'rejected'),
    });
  }

  private reminderDecisionConfirmation(activation: IWorkflowReminderActivationHistoryItem, decision: 'approved' | 'rejected' | 'revoked'): () => void {
    const captured = { ...activation, request: { ...activation.request }, latestDecision: activation.latestDecision ? { ...activation.latestDecision } : undefined };
    let dispatched = false;
    return () => {
      if (dispatched) return;
      dispatched = true;
      this.decideReminderActivation(captured, decision);
    };
  }

  private decideReminderActivation(
    activation: IWorkflowReminderActivationHistoryItem,
    decision: 'approved' | 'rejected' | 'revoked'
  ): void {
    const current = this.reminderActivationHistory?.items.find(
      (item) => item.request.id === activation.request.id
    );
    const expectedStatuses = decision === 'revoked'
      ? ['approved']
      : ['prepared', 'needs_clarification'];
    if (this.destroyed || (this.workerReviewRequired && decision !== 'revoked') || this.actionsUnavailable() || this.anyActionRunning() || !current?.current ||
        !this.validWorkerReference(activation.request.id) || !this.validDigest(activation.request.recordDigest) ||
        (activation.latestDecision && (!this.validWorkerReference(activation.latestDecision.id) || !this.validDigest(activation.latestDecision.recordDigest))) ||
        current.request.recordDigest !== activation.request.recordDigest ||
        current.latestDecision?.id !== activation.latestDecision?.id ||
        current.latestDecision?.recordDigest !== activation.latestDecision?.recordDigest ||
        !expectedStatuses.includes(current.status)) {
      this.notification.warning('Reminder decision unavailable', 'Refresh and review the current reminder state before deciding.');
      return;
    }
    const confirmation: Record<'approved' | 'rejected' | 'revoked', IWorkflowReminderActivationDecisionRequest['confirmation']> = {
      approved: 'APPROVE INTERNAL REMINDER PREPARATION',
      rejected: 'REJECT INTERNAL REMINDER PREPARATION',
      revoked: 'REVOKE INTERNAL REMINDER PREPARATION',
    };
    const reason: Record<'approved' | 'rejected' | 'revoked', string> = {
      approved: 'Owner approved keeping this internal reminder preparation available.',
      rejected: 'Owner rejected this internal reminder preparation.',
      revoked: 'Owner revoked the prior internal reminder preparation approval.',
    };
    const requestId = activation.request.id;
    const requestDigest = activation.request.recordDigest;
    const previousDecisionId = activation.latestDecision?.id;
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Internal reminder decision');
    this.activationBusyId = requestId;
    this.reminderDecisionSubscription = this.workflowService.decideReminderActivation(requestId, {
      decision,
      reason: reason[decision],
      confirmation: confirmation[decision],
      expectedActivationRequestDigest: requestDigest,
      expectedPreviousDecisionId: previousDecisionId,
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.activationBusyId = undefined; })).subscribe({
      next: (result) => {
        received = true;
        this.activationBusyId = undefined;
        if (result?.authority !== 'reminder_activation_decision_only' || result?.canExecute !== false ||
            typeof result.replayed !== 'boolean' || !this.validWorkerReference(result.decision?.id) ||
            result.decision?.activationRequestId !== requestId || result.decision.activationRequestDigest !== requestDigest ||
            (result.decision.previousDecisionId || undefined) !== previousDecisionId || result.decision.authority !== result.authority ||
            result.decision.confirmation !== confirmation[decision] || result.decision.decision !== decision ||
            !this.validDigest(result.decision.requestDigest) || !this.validDigest(result.decision.recordDigest)) { uncertain(); return; }
        this.notification.info('Server reported reminder decision', 'Inspect the decision evidence. Delivery authorization remains separate; no delivery is proven by this response.');
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  private validReminderProposalSnapshot(snapshot?: IWorkflowReminderProposalSnapshot): snapshot is IWorkflowReminderProposalSnapshot {
    if (snapshot?.authority !== 'reminder_proposal_only' || snapshot?.canExecute !== false ||
      snapshot?.freshness?.status !== 'current_internal_reminder_snapshot' ||
      snapshot.freshness.revalidationRequired !== true ||
      Number.isNaN(new Date(snapshot.freshness.checkedAt || '').getTime()) ||
      !String(snapshot.freshness.reason || '').trim() || !Array.isArray(snapshot.items) ||
      !Number.isSafeInteger(snapshot.due) || snapshot.due < 0 ||
      !Number.isSafeInteger(snapshot.upcoming) || snapshot.upcoming < 0 ||
      snapshot.due + snapshot.upcoming !== snapshot.items.length) {
      return false;
    }
    const ids = new Set(snapshot.items.map((item) => item?.id));
    const due = snapshot.items.filter((item) => item?.status === 'due').length;
    const upcoming = snapshot.items.filter((item) => item?.status === 'upcoming').length;
    return due === snapshot.due && upcoming === snapshot.upcoming &&
      ids.size === snapshot.items.length && snapshot.items.every((item) =>
      !!item?.id && !!item.workflowId && item.checklistItemId === item.id &&
      item.authority === 'reminder_proposal_only' && item.canExecute === false &&
      this.validDigest(item.evidenceDigest) &&
      ['due', 'upcoming'].includes(item.status) &&
      !Number.isNaN(new Date(item.reminderAt || '').getTime()) &&
      !!String(item.title || '').trim() && !!String(item.label || '').trim() &&
      !!String(item.nextAction || '').trim()
    );
  }

  private validReminderActivationHistory(snapshot?: IWorkflowReminderActivationHistorySnapshot): snapshot is IWorkflowReminderActivationHistorySnapshot {
    if (snapshot?.authority !== 'reminder_activation_history_only' || snapshot?.canExecute !== false ||
        !Array.isArray(snapshot?.items) || Number.isNaN(new Date(snapshot?.checkedAt || '').getTime())) {
      return false;
    }
    const requestIds = new Set(snapshot.items.map((item) => typeof item?.request?.id === 'string' ? item.request.id.toLowerCase() : undefined));
    const allowedStatuses = ['prepared', 'approved', 'rejected', 'needs_clarification', 'revoked', 'expired', 'stale'];
    return requestIds.size === snapshot.items.length && snapshot.items.every((item) => {
      const request = item?.request;
      const decision = item?.latestDecision;
      if (!this.validWorkerReference(request?.id) || !this.validWorkerReference(request.workflowId) || !this.validWorkerReference(request.checklistItemId) ||
          request.activationKind !== 'internal_notification' || request.checklistStatus !== 'open' ||
          request.authority !== 'reminder_activation_request_only' ||
          request.confirmation !== 'PREPARE INTERNAL REMINDER ONLY' || item.canExecute !== false ||
          !allowedStatuses.includes(item.status) || typeof item.current !== 'boolean' ||
          !this.validDigest(request.reminderDigest) || !this.validDigest(request.requestDigest) ||
          !this.validDigest(request.recordDigest) ||
          Number.isNaN(new Date(request.reminderAt || '').getTime()) ||
          Number.isNaN(new Date(request.requestedAt || '').getTime()) ||
          Number.isNaN(new Date(request.expiresAt || '').getTime())) {
        return false;
      }
      if (!decision) {
        return ['prepared', 'expired', 'stale'].includes(item.status);
      }
      const confirmations = { approved: 'APPROVE', rejected: 'REJECT', needs_clarification: 'REQUEST', revoked: 'REVOKE' };
      return this.validWorkerReference(decision.id) && decision.activationRequestId === request.id &&
        decision.activationRequestDigest === request.recordDigest &&
        decision.authority === 'reminder_activation_decision_only' &&
        ['approved', 'rejected', 'needs_clarification', 'revoked'].includes(decision.decision) &&
        (['expired', 'stale'].includes(item.status) || item.status === decision.decision) &&
        decision.confirmation === (decision.decision === 'needs_clarification' ? 'REQUEST REMINDER CLARIFICATION' : `${confirmations[decision.decision]} INTERNAL REMINDER PREPARATION`) &&
        this.validDigest(decision.requestDigest) && this.validDigest(decision.recordDigest) &&
        !Number.isNaN(new Date(decision.decidedAt || '').getTime());
    });
  }

  private validReminderDeliveryHistory(history?: IWorkflowReminderDeliveryHistory): history is IWorkflowReminderDeliveryHistory {
    if (history?.authority !== 'internal_reminder_delivery_receipt' || history?.canExecute !== false ||
        !Array.isArray(history.authorizations) || !Array.isArray(history.attempts)) {
      return false;
    }
    const authorizationIds = new Set(history.authorizations.map((item) => typeof item?.id === 'string' ? item.id.toLowerCase() : undefined));
    if (authorizationIds.size !== history.authorizations.length || !history.authorizations.every((item) =>
      !!item && [item.id, item.activationRequestId, item.activationDecisionId, item.workflowId, item.checklistItemId].every(id => this.validWorkerReference(id)) &&
      item.channel === 'in_app' && item.authority === 'internal_reminder_delivery_authorization' &&
      item.confirmation === 'AUTHORIZE ONE INTERNAL HAI REMINDER' && this.validDigest(item.reminderDigest) &&
      this.validDigest(item.activationRequestDigest) && this.validDigest(item.activationDecisionDigest) &&
      this.validDigest(item.requestDigest) && this.validDigest(item.recordDigest)
    )) {
      return false;
    }
    const attemptIds = new Set<string>();
    const sequenceKeys = new Set<string>();
    return history.attempts.every((item) => {
      const authorization = history.authorizations.find(authorization => authorization.id === item?.authorizationId);
      if (!authorization || !this.validWorkerReference(item?.id) || attemptIds.has(item.id.toLowerCase()) ||
          !Number.isSafeInteger(item.attemptNumber) || item.attemptNumber < 1 || item.attemptNumber > 3 ||
          sequenceKeys.has(`${item.authorizationId}:${item.attemptNumber}`) ||
          !['delivered', 'retryable_failure', 'suppressed', 'dead_lettered', 'expired'].includes(item.status) ||
          item.authority !== 'internal_reminder_delivery_receipt' || item.reminderDigest !== authorization.reminderDigest ||
          item.authorizationDigest !== authorization.recordDigest || !this.validDigest(item.recordDigest)) return false;
      attemptIds.add(item.id.toLowerCase()); sequenceKeys.add(`${item.authorizationId}:${item.attemptNumber}`);
      return true;
    });
  }

  private validDigest(value?: string): boolean {
    return /^[0-9a-f]{64}$/.test(String(value || ''));
  }

  resolveInterruptedExecution(): void {
    if (this.destroyed || !this.selected || this.interruptionForm.invalid || this.actionsUnavailable() || this.anyActionRunning() ||
        this.selected.item.currentState !== 'blocked' || this.selected.item.recoveryStatus !== 'needs_review' ||
        typeof this.selected.item.requiresApproval !== 'boolean') {
      return;
    }
    const request = {
      ...this.interruptionForm.value,
      actor: 'operator',
    };
    if (!['retry', 'confirm_completed', 'keep_blocked'].includes(request.decision) ||
        typeof request.note !== 'string' || !request.note.trim()) return;
    if (request.decision !== 'keep_blocked' && (request.priorExecutionReconciled !== true ||
        typeof request.evidenceUri !== 'string' || !request.evidenceUri.trim())) {
      this.notification.error('Reconciliation required', 'Confirm that the prior execution has ended and its outcome was checked, and add a source URI.');
      return;
    }
    const original = this.selected;
    const workflowId = original.item.id;
    if (!this.isUuid(workflowId) || workflowId === '00000000-0000-0000-0000-000000000000') return;
    const draft = JSON.stringify(this.interruptionForm.value);
    const decisionIds = new Set(original.decisions?.map(decision => decision.id) || []);
    const expectedState = request.decision === 'confirm_completed' ? 'completed' : request.decision === 'keep_blocked' ? 'blocked' : original.item.requiresApproval ? 'needs_approval' : 'ready';
    const expectedRecovery = request.decision === 'retry' ? 'retry_confirmed' : request.decision === 'confirm_completed' ? 'completion_confirmed' : 'needs_review';
    this.saving = true;
    let received = false;
    const unconfirmed = () => {
      this.pauseWorkerForReview('Interrupted-execution decision');
      this.transitionReviewId = workflowId;
      this.transitionError = 'The interrupted-execution decision could not be confirmed. Refresh the affected workflow before another change.';
      this.notification.warning('Recovery decision unconfirmed', this.transitionError);
    };
    this.interruptionSubscription = this.workflowService.resolveInterruptedExecution(workflowId, request)
      .pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.saving = false; })).subscribe({
      next: (record) => {
        received = true;
        if (this.destroyed) return;
        const decisionMatches = Array.isArray(record?.decisions) && record.decisions.some(value =>
          typeof value?.id === 'string' && this.isUuid(value.id) && value.id !== '00000000-0000-0000-0000-000000000000' &&
          !decisionIds.has(value.id) && value.workflowId === workflowId && value.decisionType === 'interrupted_execution' &&
          value.decision === request.decision && value.approved === (request.decision === 'confirm_completed') &&
          typeof value.createdAt === 'string' && Number.isFinite(Date.parse(value.createdAt)));
        const sourceMatches = request.decision === 'keep_blocked' || (Array.isArray(record?.sourceLinks) && record.sourceLinks.some(value =>
          value?.workflowId === workflowId && value.sourceType === 'recovery_evidence' &&
          value.sourceUri === request.evidenceUri.trim() && value.relationship === (request.decision === 'retry' ? 'execution_reconciliation' : 'completion_evidence')));
        if (record?.item?.id !== workflowId || record.item.currentState !== expectedState ||
            record.item.recoveryStatus !== expectedRecovery || !decisionMatches || !sourceMatches ||
            (expectedState === 'needs_approval' && (record.item.requiresApproval !== true || record.item.approvalStatus !== 'pending'))) {
          unconfirmed();
          return;
        }
        const sameSelection = this.selected === original;
        if (sameSelection) this.applyWorkflowRecord(record);
        if (sameSelection && JSON.stringify(this.interruptionForm.value) === draft) this.interruptionForm.reset({
          decision: 'retry',
          note: '',
          evidenceUri: '',
          evidenceLabel: '',
          priorExecutionReconciled: false,
        });
        this.saving = false;
        this.notification.info('Recovery response received', 'The server returned the requested recovery state and evidence. This is not independent verification of the external outcome.');
        this.refresh(false, true);
      },
      error: () => {
        if (!this.destroyed) unconfirmed();
      },
      complete: () => {
        if (!received && !this.destroyed) unconfirmed();
      },
    });
  }

  runDue(): void {
    if (this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning() || this.queueCount('ready') <= 0) {
      return;
    }
    this.runningAction = 'worker';
    this.runSummary = undefined;
    let received = false;
    const uncertain = () => {
      this.workerReviewRequired = true;
      this.workerReviewRefreshed = false;
      this.workerReviewMessage = 'The worker outcome needs review. Refresh and inspect workflow records and execution evidence before allowing another run. A missing response does not mean nothing ran.';
      this.lastOperation = { name: 'Run worker', status: 'failed', summary: this.workerReviewMessage, at: new Date() };
      this.notification.warning('Review worker outcomes', this.workerReviewMessage);
    };
    this.workerRunSubscription = this.workflowService.runDue({ limit: 10 }).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.runningAction = undefined; })
    ).subscribe({
      next: (summary) => {
        received = true;
        this.runningAction = undefined;
        if (!this.validWorkerSummary(summary) || summary.results.some(result => result.reviewRequired)) {
          uncertain(); return;
        }
        this.runSummary = {
          checked: summary.checked, completed: summary.completed, retried: summary.retried,
          blocked: summary.blocked, skipped: summary.skipped,
          results: summary.results.map(result => ({
            workflowId: result.workflowId, status: result.status, state: result.state, attempts: result.attempts,
          })),
        };
        this.lastOperation = {
          name: 'Run worker',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.completed} completed, ${summary.retried} retried, ${summary.blocked} blocked, ${summary.skipped} skipped.`,
          details: this.workflowRunDetails(this.runSummary),
          at: new Date(),
        };
        this.notification.info('Worker response received', `${summary.completed} reported completed, ${summary.retried} retries scheduled, ${summary.blocked} blocked. Inspect records for evidence.`);
        this.reloadSelectedWorkflow();
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  acknowledgeWorkerReview(): void {
    if (this.destroyed || !this.workerReviewRequired || !this.workerReviewRefreshed || !this.dataLoaded || this.loading ||
        this.loadFailed || this.anyActionRunning() || this.items.some(item => item.currentState === 'in_progress')) return;
    const message = this.workerReviewMessage;
    this.modal.confirm({
      nzTitle: 'Have you reviewed the previous worker outcomes?',
      nzContent: 'Check workflow records, audit history and external execution evidence. This acknowledgement only clears the local UI pause; it does not verify completion or bypass backend safety gates.',
      nzOkText: 'I reviewed the outcomes', nzCancelText: 'Keep paused',
      nzOnOk: () => {
        if (this.destroyed || !this.workerReviewRequired || this.workerReviewMessage !== message ||
            !this.workerReviewRefreshed || !this.dataLoaded || this.loading || this.loadFailed || this.anyActionRunning() ||
            this.items.some(item => item.currentState === 'in_progress')) return;
        this.workerReviewRequired = false;
        this.workerReviewMessage = undefined;
      },
    });
  }

  private validWorkerSummary(summary: IWorkflowRunSummary): boolean {
    if (!summary || !Array.isArray(summary.results) || summary.results.length > 10 ||
        ![summary.checked, summary.completed, summary.retried, summary.blocked, summary.skipped]
          .every(value => Number.isInteger(value) && value >= 0 && value <= 10) ||
        summary.checked !== summary.results.length ||
        summary.checked !== summary.completed + summary.retried + summary.blocked + summary.skipped) return false;
    const ids = new Set<string>();
    const counts = { completed: 0, retry_scheduled: 0, blocked: 0, skipped: 0 };
    for (const result of summary.results) {
      if (!result || typeof result.workflowId !== 'string' ||
          !/^(?!00000000-0000-0000-0000-000000000000$)[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(result.workflowId) ||
          ids.has(result.workflowId.toLowerCase()) ||
          !Object.prototype.hasOwnProperty.call(counts, result.status) ||
          !this.overview?.states.includes(result.state) ||
          !Number.isInteger(result.attempts) || result.attempts < 0 ||
          (result.status === 'completed' && result.state !== 'completed') ||
          (result.status === 'retry_scheduled' && result.state !== 'ready') ||
          (result.reviewRequired !== undefined && typeof result.reviewRequired !== 'boolean')) return false;
      ids.add(result.workflowId.toLowerCase());
      counts[result.status as keyof typeof counts]++;
    }
    return counts.completed === summary.completed && counts.retry_scheduled === summary.retried &&
      counts.blocked === summary.blocked && counts.skipped === summary.skipped;
  }

  recoverStaleClaims(): void {
    if (this.destroyed || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    this.runningAction = 'recovery';
    this.recoverySummary = undefined;
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Recover stale');
    this.recoveryRunSubscription = this.workflowService.recoverStaleClaims({ limit: 50 }).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.runningAction = undefined; })
    ).subscribe({
      next: (summary) => {
        received = true;
        this.runningAction = undefined;
        if (!this.validRecoverySummary(summary)) { uncertain(); return; }
        this.recoverySummary = { checked: summary.checked, workflowsBlocked: summary.workflowsBlocked,
          openLoopsReopened: summary.openLoopsReopened, skipped: summary.skipped,
          results: summary.results.map(result => ({ workflowId: result.workflowId, openLoopId: result.type === 'open_loop' ? result.openLoopId : undefined,
            type: result.type, status: result.status, message: 'Inspect the workflow record and recovery audit.' })),
        };
        this.lastOperation = {
          name: 'Recover stale',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.workflowsBlocked} workflows blocked for review, ${summary.openLoopsReopened} follow-ups reopened, ${summary.skipped} skipped.`,
          details: this.claimRecoveryDetails(this.recoverySummary),
          at: new Date(),
        };
        this.notification.info(
          'Recovery response received',
          `${summary.workflowsBlocked} workflows reported blocked for review, ${summary.openLoopsReopened} follow-ups reported reopened. Inspect records for confirmation.`
        );
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  runDueOpenLoops(): void {
    if (this.destroyed || this.workerReviewRequired || this.actionsUnavailable() || this.anyActionRunning() || this.dueOpenLoopCount() <= 0) {
      return;
    }
    this.runningAction = 'followups';
    this.openLoopRunSummary = undefined;
    let received = false;
    const uncertain = () => this.pauseWorkerForReview('Run follow-ups');
    this.followupRunSubscription = this.workflowService.runDueOpenLoops({ limit: 10 }).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.runningAction = undefined; })
    ).subscribe({
      next: (summary) => {
        received = true;
        this.runningAction = undefined;
        if (!this.validFollowupSummary(summary)) { uncertain(); return; }
        this.openLoopRunSummary = { checked: summary.checked, triggered: summary.triggered,
          resolved: summary.resolved, skipped: summary.skipped,
          results: summary.results.map(result => ({ workflowId: result.workflowId, openLoopId: result.openLoopId,
            status: result.status, state: result.state })),
        };
        this.lastOperation = {
          name: 'Run follow-ups',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.triggered} triggered, ${summary.resolved} resolved, ${summary.skipped} skipped.`,
          details: this.openLoopRunDetails(this.openLoopRunSummary),
          at: new Date(),
        };
        this.notification.info('Follow-up response received', `${summary.triggered} reported triggered, ${summary.resolved} reported resolved. This is not proof of an external message or task execution.`);
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  private pauseWorkerForReview(name: string): void {
    this.workerReviewRequired = true;
    this.workerReviewRefreshed = false;
    this.workerReviewMessage = 'Operation outcome needs review. Refresh and inspect project records and audit evidence before resubmitting input or starting execution or follow-ups. Missing acknowledgement does not mean nothing changed. Claim recovery remains available and does not execute tasks.';
    this.lastOperation = { name, status: 'failed', summary: this.workerReviewMessage, at: new Date() };
    this.notification.warning('Review operation outcome', this.workerReviewMessage);
  }

  private validFollowupSummary(summary: IWorkflowOpenLoopRunSummary): boolean {
    if (!summary || !Array.isArray(summary.results) ||
        ![summary.checked, summary.triggered, summary.resolved, summary.skipped].every(value => Number.isSafeInteger(value) && value >= 0 && value <= 10) ||
        summary.checked !== summary.results.length || summary.checked !== summary.triggered + summary.resolved + summary.skipped) return false;
    const ids = new Set<string>();
    const counts = { triggered: 0, resolved: 0, skipped: 0 };
    for (const result of summary.results) {
      if (!result || !this.validWorkerReference(result.workflowId) || !this.validWorkerReference(result.openLoopId) ||
          ids.has(result.openLoopId.toLowerCase()) || !Object.prototype.hasOwnProperty.call(counts, result.status) ||
          (result.status !== 'skipped' && !this.overview?.states.includes(result.state || '')) ||
          (result.state !== undefined && !this.overview?.states.includes(result.state))) return false;
      ids.add(result.openLoopId.toLowerCase()); counts[result.status as keyof typeof counts]++;
    }
    return counts.triggered === summary.triggered && counts.resolved === summary.resolved && counts.skipped === summary.skipped;
  }

  private validRecoverySummary(summary: IWorkflowClaimRecoverySummary): boolean {
    if (!summary || !Array.isArray(summary.results) ||
        ![summary.checked, summary.workflowsBlocked, summary.openLoopsReopened, summary.skipped].every(value => Number.isSafeInteger(value) && value >= 0 && value <= 100) ||
        summary.checked !== summary.results.length || summary.checked !== summary.workflowsBlocked + summary.openLoopsReopened + summary.skipped) return false;
    const ids = new Set<string>(); let workflows = 0; let loops = 0; let blocked = 0; let reopened = 0; let skipped = 0;
    for (const result of summary.results) {
      if (!result || !this.validWorkerReference(result.workflowId) || !['workflow', 'open_loop'].includes(result.type) ||
          (result.type === 'open_loop' && !this.validWorkerReference(result.openLoopId)) ||
          (result.type === 'workflow' && result.openLoopId !== undefined && result.openLoopId !== '00000000-0000-0000-0000-000000000000') ||
          !['skipped', result.type === 'workflow' ? 'blocked' : 'reopened'].includes(result.status)) return false;
      const key = `${result.type}:${(result.type === 'open_loop' ? result.openLoopId! : result.workflowId).toLowerCase()}`;
      if (ids.has(key)) return false;
      ids.add(key);
      if (result.type === 'workflow') workflows++; else loops++;
      if (result.status === 'blocked') blocked++; else if (result.status === 'reopened') reopened++; else skipped++;
    }
    return workflows <= 50 && loops <= 50 && blocked === summary.workflowsBlocked && reopened === summary.openLoopsReopened && skipped === summary.skipped;
  }

  private validWorkerReference(value: unknown): value is string {
    return typeof value === 'string' && this.isUuid(value) && value !== '00000000-0000-0000-0000-000000000000';
  }

  resolveProposal(
    proposalId: string,
    status: 'approved' | 'changes_requested' | 'rejected',
    selectedOption?: string,
  ): void {
    if (this.destroyed || !this.selected || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const original = this.selected;
    const workflowId = original.item.id;
    const proposals = original.proposals?.filter(value => value?.id === proposalId) || [];
    if (!this.isUuid(workflowId) || workflowId === '00000000-0000-0000-0000-000000000000' ||
        !this.isUuid(proposalId) || proposalId === '00000000-0000-0000-0000-000000000000' ||
        !['approved', 'rejected', 'changes_requested'].includes(status) ||
        (selectedOption !== undefined && typeof selectedOption !== 'string') ||
        proposals.length !== 1 || proposals[0].workflowId !== workflowId || proposals[0].status !== 'open') return;
    const decisionIds = new Set(original.decisions?.map(value => value.id) || []);
    this.proposalAction = { proposalId, status };
    const approved = status === 'approved';
    const noteByStatus: Record<string, string> = {
      approved: 'Proposal approved from dashboard.',
      changes_requested: 'Proposal needs changes from dashboard.',
      rejected: 'Proposal rejected from dashboard.',
    };
    let received = false;
    const unconfirmed = () => {
      this.transitionReviewId = workflowId;
      this.transitionError = 'The proposal decision could not be confirmed. Refresh the affected workflow before another change.';
      this.notification.warning('Proposal decision unconfirmed', this.transitionError);
    };
    this.proposalSubscription = this.workflowService.resolveProposal(workflowId, proposalId, {
      approved,
      status,
      selectedOption,
      note: noteByStatus[status],
      actor: 'operator',
    }).pipe(take(1), timeout(this.operationTimeoutMs), finalize(() => { this.proposalAction = undefined; })).subscribe({
      next: (record) => {
        received = true;
        if (this.destroyed) return;
        const returned = Array.isArray(record?.proposals) ? record.proposals.filter(value => value?.id === proposalId) : [];
        const decisionMatches = Array.isArray(record?.decisions) && record.decisions.some(value =>
          typeof value?.id === 'string' && this.isUuid(value.id) && value.id !== '00000000-0000-0000-0000-000000000000' &&
          !decisionIds.has(value.id) && value.workflowId === workflowId && value.decisionType === 'proposal' &&
          value.decision === status && value.approved === approved && typeof value.createdAt === 'string' && Number.isFinite(Date.parse(value.createdAt)));
        if (record?.item?.id !== workflowId || typeof record.item.currentState !== 'string' ||
            !this.overview?.states?.includes(record.item.currentState) || returned.length !== 1 ||
            returned[0].workflowId !== workflowId || returned[0].status !== status ||
            (selectedOption !== undefined && returned[0].selectedOption !== selectedOption.trim()) || !decisionMatches) {
          unconfirmed();
          return;
        }
        this.proposalAction = undefined;
        if (this.selected === original) this.applyWorkflowRecord(record);
        this.notification.info('Proposal response received', 'The server returned the requested proposal decision and record. This is not proof of execution or independent audit persistence.');
        this.refresh(false, true);
      },
      error: () => {
        if (!this.destroyed) unconfirmed();
      },
      complete: () => {
        if (!received && !this.destroyed) unconfirmed();
      },
    });
  }

  isProposalActionRunning(
    proposalId: string,
    status: 'approved' | 'changes_requested' | 'rejected'
  ): boolean {
    return this.proposalAction?.proposalId === proposalId && this.proposalAction.status === status;
  }

  runSelectedWorkflow(): void {
    const original = this.selected;
    const item = original?.item;
    const approvalSatisfied = item?.approvalStatus === 'approved'
      || (!item?.requiresApproval && item?.approvalStatus === 'not_required');
    if (this.destroyed || this.workerReviewRequired || !original || !item || !this.validWorkerReference(item.id) || item.currentState !== 'ready' || !approvalSatisfied || this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    const id = item.id;
    let dispatched = false;
    this.modal.confirm({
      nzTitle: 'Run this selected workflow?',
      nzContent: 'HAI will claim only this workflow. Any concrete task or runtime action still passes authorization, emergency-stop, audit, and verification gates.',
      nzOkText: 'Run this workflow',
      nzCancelText: 'Cancel',
      nzOnOk: () => {
        const approvalStillSatisfied = item.approvalStatus === 'approved'
          || (item.requiresApproval === false && item.approvalStatus === 'not_required');
        if (dispatched || this.destroyed || this.workerReviewRequired || this.selected !== original || original.item !== item || item.id !== id ||
            item.currentState !== 'ready' || !approvalStillSatisfied ||
            this.actionsUnavailable() || this.anyActionRunning()) return;
        dispatched = true;
        this.executeSelectedWorkflow(id, original);
      },
    });
  }

  private executeSelectedWorkflow(id: string, original: IWorkflowRecord): void {
    this.runningAction = 'selected';
    let received = false;
    const uncertain = () => {
      this.pauseWorkerForReview('Run selected workflow');
      this.transitionReviewId = id;
      this.transitionError = 'Execution outcome needs reconciliation. Reload this workflow before another action; do not assume nothing ran.';
      this.lastOperation = { name: 'Run selected workflow', status: 'failed', summary: this.transitionError, at: new Date() };
    };
    this.selectedRunSubscription = this.workflowService.runOne(id).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.runningAction = undefined; })
    ).subscribe({
      next: (result) => {
        received = true;
        this.runningAction = undefined;
        if (!result || typeof result.workflowId !== 'string' || result.workflowId.toLowerCase() !== id.toLowerCase() ||
            !['completed', 'blocked', 'skipped', 'retry_scheduled'].includes(result.status) ||
            !this.overview?.states.includes(result.state) || !Number.isSafeInteger(result.attempts) || result.attempts < 0 ||
            (result.status === 'completed' && result.state !== 'completed') ||
            (result.status === 'retry_scheduled' && result.state !== 'ready') ||
            (result.reviewRequired !== undefined && typeof result.reviewRequired !== 'boolean')) {
          uncertain(); return;
        }
        if (result.reviewRequired) {
          this.pauseWorkerForReview('Run selected workflow');
          if (this.selected === original) this.loadWorkflowRecord(id);
          this.refresh(false, true);
          return;
        }
        const completed = result.status === 'completed';
        this.lastOperation = {
          name: 'Run selected workflow',
          status: completed ? 'completed' : 'failed',
          summary: `Server reported ${this.readable(result.status)}. Inspect the workflow record and evidence for the current outcome.`,
          at: new Date(),
        };
        if (completed) {
          this.notification.info('Server reported completion', 'Inspect the workflow record and evidence; this response alone is not independent verification.');
        } else {
          this.notification.warning('Workflow needs attention', this.lastOperation.summary);
        }
        if (this.selected === original) this.reloadSelectedWorkflow();
        this.refresh(false, true);
      },
      error: uncertain,
      complete: () => { if (!received) uncertain(); },
    });
  }

  isAutomationSelectionProposal(action?: string): boolean {
    return (action || '').trim() === 'Select an automation for controlled execution';
  }

  hasOpenAutomationSelection(record?: IWorkflowRecord): boolean {
    return !!record?.proposals?.some((proposal) =>
      proposal.status === 'open' && this.isAutomationSelectionProposal(proposal.recommendedAction)
    );
  }

  hasPendingReviewProposals(record?: IWorkflowRecord): boolean {
    return !!record?.proposals?.some((proposal) =>
      proposal.status === 'open' && !this.isAutomationSelectionProposal(proposal.recommendedAction)
    );
  }

  isAutomationOption(option?: string): boolean {
    return /\[automation:[0-9a-f-]{36}\]/i.test(option || '');
  }

  automationOptionLabel(option?: string): string {
    const value = (option || '').trim();
    const marker = value.indexOf(' [automation:');
    return marker > 4 ? value.slice(4, marker).trim() : 'Use automation';
  }

  openAutomationSetup(): void {
    this.router.navigate(['/home']);
  }

  openCoordinationPlan(planId?: string): void {
    const normalized = (planId || '').trim();
    if (!normalized) return;
    this.router.navigate(['/plans'], { queryParams: { planId: normalized } });
  }

  markChecklist(itemId: string, status: string): void {
    const checklistItem = this.selected?.checklist.find((item) => item.id === itemId);
    if (!this.selected || !checklistItem || checklistItem.status === status ||
        this.actionsUnavailable() || this.anyActionRunning()) {
      return;
    }
    this.checklistAction = { itemId, status };
    this.checklistActionError = undefined;
    const workflowId = this.selected.item.id;
    this.workflowService.updateChecklistItem(workflowId, itemId, { status })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
      next: (record) => {
        this.checklistAction = undefined;
        this.applyWorkflowRecord(record);
      },
      error: () => {
        this.checklistAction = undefined;
        this.checklistActionError = { itemId, message: 'Update failed. The saved checklist state was not confirmed; retry after checking the workflow.' };
        this.changeDetector.detectChanges();
        this.notification.error('Checklist update failed', 'The saved state was not confirmed.');
      },
    });
  }

  get interruptionRequiresEvidence(): boolean {
    return ['retry', 'confirm_completed'].includes(this.interruptionForm.get('decision')?.value);
  }

  applyWorkflowRecord(record: IWorkflowRecord): void {
    this.workflowOpenError = undefined;
    this.selected = record;
    // Selected workflow controls are a primary Basic-view action surface.
    // Render this record before optional provenance lookups begin.
    this.changeDetector.detectChanges();
    this.frameworkSelectionDecision = undefined;
    this.frameworkSelectionLoading = false;
    this.frameworkSelectionUnavailable = false;
    this.frameworkProvenanceIssues = [];

    const selections = Array.isArray(record.frameworkSelections)
      ? record.frameworkSelections
      : [];
    if (!selections.length) {
      this.frameworkProvenance = undefined;
      this.frameworkProvenanceState = 'missing';
      this.frameworkSelectionLookup += 1;
      return;
    }

    const currentPlanId = (record.item.lastTaskPlanId || '').trim();
    const provenance = currentPlanId
      ? selections.find((selection) => selection?.taskPlanId?.trim() === currentPlanId)
      : selections[0];
    this.frameworkProvenance = provenance || selections[0];

    if (!provenance) {
      this.frameworkProvenanceState = 'invalid';
      this.frameworkProvenanceIssues = [
        'No stored framework selection matches the workflow current task plan.',
      ];
      this.frameworkSelectionLookup += 1;
      return;
    }

    this.frameworkProvenanceIssues = this.validateFrameworkProvenance(
      provenance,
      currentPlanId
    );
    if (this.frameworkProvenanceIssues.length) {
      this.frameworkProvenanceState = 'invalid';
      this.frameworkSelectionLookup += 1;
      return;
    }

    this.frameworkProvenanceState = 'recorded';
    this.frameworkSelectionLoading = true;
    const lookup = ++this.frameworkSelectionLookup;
    this.workflowService.frameworkSelection(provenance.selectionDecisionId)
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
      next: (decision) => {
        if (lookup !== this.frameworkSelectionLookup) {
          return;
        }
        this.frameworkSelectionLoading = false;
        if (!decision) {
          this.frameworkSelectionUnavailable = true;
          return;
        }
        const issues = this.validateFrameworkSelectionDecision(decision, provenance);
        if (issues.length) {
          this.frameworkProvenanceState = 'invalid';
          this.frameworkProvenanceIssues = issues;
          return;
        }
        this.frameworkSelectionDecision = decision;
        this.frameworkProvenanceState = 'verified';
      },
      error: () => {
        if (lookup !== this.frameworkSelectionLookup) {
          return;
        }
        this.frameworkSelectionLoading = false;
        this.frameworkSelectionUnavailable = true;
      },
    });
  }

  get frameworkGovernanceLabel(): string {
    switch (this.frameworkProvenanceState) {
      case 'verified':
        return 'Governed selection verified';
      case 'recorded':
        return 'Selection provenance recorded';
      case 'invalid':
        return 'Framework provenance needs review';
      default:
        return 'No framework selection recorded';
    }
  }

  get frameworkGovernanceSummary(): string {
    switch (this.frameworkProvenanceState) {
      case 'verified':
        return 'The workflow provenance matches its owner-scoped Framework Registry decision.';
      case 'recorded':
        return 'Compact provenance is present, but the full registry decision is not currently confirmed.';
      case 'invalid':
        return 'Stored provenance failed validation. Do not treat this workflow as framework-governed.';
      default:
        return 'Framework-governed execution or completion cannot be confirmed from this workflow record.';
    }
  }

  get frameworkGovernanceClass(): string {
    if (this.frameworkProvenanceState === 'verified') {
      return 'governance-summary--verified';
    }
    if (this.frameworkProvenanceState === 'invalid') {
      return 'governance-summary--invalid';
    }
    return 'governance-summary--unconfirmed';
  }

  get frameworkSelectionLabel(): string {
    return this.frameworkProvenance?.selectionDecisionId
      ? `Selection ${this.shortIdentifier(this.frameworkProvenance.selectionDecisionId)}`
      : 'No selection ID';
  }

  openFrameworkRegistry(): void {
    this.router.navigate(['/framework-registry']);
  }

  copyProvenanceValue(label: string, value?: string | number): void {
    const text = String(value ?? '').trim();
    if (!text) {
      this.notification.warning('Value unavailable', `${label} is not present in this workflow record.`);
      return;
    }
    if (typeof navigator === 'undefined' || !navigator.clipboard?.writeText) {
      this.notification.info(label, text);
      return;
    }
    navigator.clipboard.writeText(text).then(
      () => this.notification.success('Copied', `${label} copied.`),
      () => this.notification.info(label, text)
    );
  }

  optionLines(options?: string): string[] {
    return (options || '').split('\n').filter((option) => !!option.trim());
  }

  filteredItems(): IWorkflowItem[] {
    const query = this.workflowSearch.trim().toLowerCase();
    return this.items.filter((item) => {
      const queueMatch =
        this.activeQueue === 'all' ||
        (this.activeQueue === 'approval' && item.requiresApproval && item.approvalStatus !== 'approved') ||
        (this.activeQueue === 'ready' && item.currentState === 'ready') ||
        (this.activeQueue === 'blocked' && (item.currentState === 'blocked' || !!item.blockedReason)) ||
        (this.activeQueue === 'review' && item.recoveryStatus === 'needs_review');
      const stateMatch = this.stateFilter === 'all' || item.currentState === this.stateFilter;
      const riskMatch = this.riskFilter === 'all' || item.riskLevel === this.riskFilter;
      const textMatch =
        !query ||
        `${item.title} ${item.description || ''} ${item.projectKey || ''} ${item.nextAction || ''}`
          .toLowerCase()
          .includes(query);
      return queueMatch && stateMatch && riskMatch && textMatch;
    });
  }

  count(key: string): number {
    return this.dashboard?.counts?.[key] || 0;
  }

  queueCount(queue: 'all' | 'approval' | 'ready' | 'blocked' | 'review'): number {
    if (queue === 'all') return this.items.length;
    if (queue === 'approval') return this.approvalItems.length;
    if (queue === 'ready') {
      return Array.isArray(this.dashboard?.readyItems) ? this.dashboard.readyItems.length : this.count('ready');
    }
    if (queue === 'blocked') {
      return Array.isArray(this.dashboard?.blockedItems) ? this.dashboard.blockedItems.length : this.count('blocked');
    }
    return this.count('interruptedReview');
  }

  hasWorkflowAttention(): boolean {
    return (
      this.queueCount('all') > 0 ||
      this.queueCount('approval') > 0 ||
      this.queueCount('ready') > 0 ||
      this.queueCount('blocked') > 0 ||
      this.queueCount('review') > 0 ||
      this.count('dueOpenLoops') > 0 ||
      this.count('expiredWorkflowClaims') > 0 ||
      this.count('expiredOpenLoopClaims') > 0
    );
  }

  stateOptions(): string[] {
    const states = this.overview?.states || [];
    const itemStates = this.items.map((item) => item.currentState).filter(Boolean);
    return Array.from(new Set([...states, ...itemStates])).sort();
  }

  riskOptions(): string[] {
    return Array.from(new Set(this.items.map((item) => item.riskLevel).filter(Boolean))).sort();
  }

  readable(value?: string): string {
    return (value || 'unknown').replace(/_/g, ' ');
  }

  statusClass(value?: string): string {
    const normalized = (value || '').toLowerCase();
    if (['implemented', 'completed', 'ready', 'approved', 'verified', 'source_supported'].includes(normalized)) {
      return 'status--good';
    }
    if (['partial', 'warning', 'waiting_external_input', 'needs_approval', 'needs_review'].includes(normalized)) {
      return 'status--watch';
    }
    if (['blocked', 'failed', 'rejected', 'unsupported', 'conflicting'].includes(normalized)) {
      return 'status--risk';
    }
    return 'status--neutral';
  }

  statusLabel(value?: string): string {
    const normalized = (value || '').toLowerCase();
    if (normalized === 'partial') {
      return 'in progress';
    }
    if (normalized === 'not_implemented') {
      return 'planned';
    }
    return this.readable(value);
  }

  clearFilters(): void {
    this.activeQueue = 'all';
    this.stateFilter = 'all';
    this.riskFilter = 'all';
    this.workflowSearch = '';
  }

  hasIntakeInput(): boolean {
    return !!String(this.intakeForm.get('input')?.value || '').trim();
  }

  focusFilters(): void {
    this.viewPreferences.setMode('workflow-engine', 'advanced');
    this.viewPreferences.setSection('workflow-engine', 'queue-filters', true);
    window.setTimeout(() => {
      this.changeDetector.detectChanges();
      document.getElementById('workflow-state-filter')?.focus();
    });
  }

  focusIntake(): void {
    const element = document.getElementById('workflow-intake-input');
    if (element) {
      element.focus();
      const reduceMotion = typeof window !== 'undefined' &&
        window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
      element.scrollIntoView({ behavior: reduceMotion ? 'auto' : 'smooth', block: 'center' });
    }
  }

  priorityClass(score?: number): string {
    if ((score || 0) >= 80) return 'priority--urgent';
    if ((score || 0) >= 50) return 'priority--medium';
    return 'priority--normal';
  }

  capabilityProgress(status?: string): number {
    if (status === 'implemented') return 100;
    if (status === 'partial') return 58;
    return 24;
  }

  isActionRunning(action: 'refresh' | 'worker' | 'selected' | 'followups' | 'recovery'): boolean {
    return this.runningAction === action;
  }

  anyActionRunning(): boolean {
    return !!this.runningAction || this.loading || this.saving || !!this.proposalAction ||
      !!this.checklistAction || !!this.activationBusyId || !!this.openingWorkflowId || this.matchingPursuits;
  }

  actionsUnavailable(): boolean {
    return !this.dataLoaded || this.loadFailed || this.loading || !!this.transitionReviewId || !!this.approvalReviewId;
  }

  workflowActionsDisabled(): boolean {
    return this.actionsUnavailable() || this.anyActionRunning();
  }

  dueOpenLoopCount(): number {
    return Array.isArray(this.dashboard?.dueOpenLoops)
      ? this.dashboard.dueOpenLoops.length
      : this.count('dueOpenLoops');
  }

  private validateFrameworkProvenance(
    provenance: IWorkflowFrameworkSelectionProvenance,
    currentPlanId: string
  ): string[] {
    const issues: string[] = [];
    const requiredStrings: Array<[string, string | undefined]> = [
      ['Selection decision ID', provenance?.selectionDecisionId],
      ['Task plan ID', provenance?.taskPlanId],
      ['Catalog version', provenance?.catalogVersion],
      ['Selector algorithm version', provenance?.selectorAlgorithmVersion],
      ['Constitution source', provenance?.constitutionSource],
    ];
    requiredStrings.forEach(([label, value]) => {
      if (!String(value || '').trim()) {
        issues.push(`${label} is missing.`);
      }
    });

    const decisionId = String(provenance?.selectionDecisionId || '').trim();
    if (decisionId && !this.isUuid(decisionId)) {
      issues.push('Selection decision ID is not a UUID.');
    }
    if (
      currentPlanId &&
      String(provenance?.taskPlanId || '').trim() !== currentPlanId
    ) {
      issues.push('Selection provenance does not match the workflow current task plan.');
    }

    const digests: Array<[string, string | undefined]> = [
      ['Catalog digest', provenance?.catalogDigest],
      ['Preference digest', provenance?.effectivePreferenceDigest],
      ['Constitution digest', provenance?.constitutionDigest],
    ];
    digests.forEach(([label, value]) => {
      if (!this.isSha256(value)) {
        issues.push(`${label} is not a SHA-256 digest.`);
      }
    });
    if (!Number.isInteger(provenance?.constitutionVersion) || provenance.constitutionVersion < 1) {
      issues.push('Constitution version is not a positive integer.');
    }
    return Array.from(new Set(issues));
  }

  private validateFrameworkSelectionDecision(
    decision: IWorkflowFrameworkSelectionDecision,
    provenance: IWorkflowFrameworkSelectionProvenance
  ): string[] {
    const issues: string[] = [];
    const matches: Array<[string, string | number | undefined, string | number | undefined]> = [
      ['Selection decision ID', decision?.id, provenance.selectionDecisionId],
      ['Task plan ID', decision?.taskPlanId, provenance.taskPlanId],
      ['Catalog version', decision?.catalogVersion, provenance.catalogVersion],
      ['Catalog digest', decision?.catalogDigest, provenance.catalogDigest],
      [
        'Selector algorithm version',
        decision?.selectorAlgorithmVersion,
        provenance.selectorAlgorithmVersion,
      ],
      [
        'Preference digest',
        decision?.effectivePreferenceDigest,
        provenance.effectivePreferenceDigest,
      ],
      ['Constitution version', decision?.constitutionVersion, provenance.constitutionVersion],
      ['Constitution digest', decision?.constitutionDigest, provenance.constitutionDigest],
      ['Constitution source', decision?.constitutionSource, provenance.constitutionSource],
    ];
    matches.forEach(([label, actual, expected]) => {
      if (String(actual ?? '').trim() !== String(expected ?? '').trim()) {
        issues.push(`${label} does not match the persisted workflow provenance.`);
      }
    });

    if (!Array.isArray(decision?.selected) || !decision.selected.length) {
      issues.push('The Framework Registry decision has no selected frameworks.');
    } else {
      const seen = new Set<string>();
      decision.selected.forEach((framework) => {
        const id = String(framework?.id || '').trim();
        const version = String(framework?.version || '').trim();
        if (!id || !version) {
          issues.push('A selected framework is missing its ID or version.');
          return;
        }
        const key = `${id}@${version}`;
        if (seen.has(key)) {
          issues.push('The Framework Registry decision contains duplicate framework versions.');
        }
        seen.add(key);
      });
    }
    return Array.from(new Set(issues));
  }

  private isUuid(value: string): boolean {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value);
  }

  private isSha256(value?: string): boolean {
    return /^[0-9a-f]{64}$/i.test(String(value || '').trim());
  }

  private shortIdentifier(value: string): string {
    const normalized = String(value || '').trim();
    if (normalized.length <= 18) {
      return normalized;
    }
    return `${normalized.slice(0, 8)}...${normalized.slice(-4)}`;
  }

  private reloadSelectedWorkflow(): void {
    const workflowId = this.selected?.item?.id;
    if (!workflowId) {
      return;
    }
    this.loadWorkflowRecord(workflowId);
  }

  private workflowRunDetails(summary: IWorkflowRunSummary): string {
    return summary.results
      .slice(0, 5)
      .map((result) => `${result.status}: ${result.message || result.workflowId}`)
      .join(' | ');
  }

  private workflowResultSummary(result: IWorkflowRunResult): string {
    const state = this.readable(result.state || result.status);
    return result.message ? `${state}: ${result.message}` : `${state} (${result.status}).`;
  }

  private openLoopRunDetails(summary: IWorkflowOpenLoopRunSummary): string {
    return summary.results
      .slice(0, 5)
      .map((result) => `${result.status}: ${result.message || result.openLoopId}`)
      .join(' | ');
  }

  private claimRecoveryDetails(summary: IWorkflowClaimRecoverySummary): string {
    return summary.results
      .slice(0, 5)
      .map((result) => `${result.type}/${result.status}: ${result.message}`)
      .join(' | ');
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  openPursuit(id: string): void {
    this.router.navigate(['/pursuits'], { queryParams: { selected: id } });
  }

}

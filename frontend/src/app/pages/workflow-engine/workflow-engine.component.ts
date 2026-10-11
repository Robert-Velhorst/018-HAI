import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { catchError, forkJoin, of } from 'rxjs';
import {
  IPursuitDetail,
  IPursuitMatchCandidate,
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
  IWorkflowRunResult,
  IWorkflowRunSummary,
} from '../../models/workflow.model.interface';
import { WORKFLOW_SERVICE_TOKEN } from '../../services/workflow/workflow.service.token';
import { IWorkflowService } from '../../services/workflow.service.interface';

type FrameworkProvenanceState = 'missing' | 'invalid' | 'recorded' | 'verified';

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-workflow-engine',
  templateUrl: './workflow-engine.component.html',
  styleUrls: ['./workflow-engine.component.scss'],
})
export class WorkflowEngineComponent implements OnInit {
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
  saving = false;
  matchingPursuits = false;
  runningAction?: 'refresh' | 'worker' | 'selected' | 'followups' | 'recovery' | 'reminders';
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

  intakeForm: FormGroup = this.fb.group({
    input: ['', [Validators.required]],
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
  });

  constructor(
    private fb: FormBuilder,
    @Inject(WORKFLOW_SERVICE_TOKEN) private workflowService: IWorkflowService,
    private pursuitService: PursuitService,
    private notification: NzNotificationService,
    private modal: NzModalService,
    private route: ActivatedRoute,
    private router: Router
  ) {}

  ngOnInit(): void {
    const requestedQueue = this.route.snapshot.queryParamMap.get('queue');
    if (this.isWorkflowQueue(requestedQueue)) {
      this.activeQueue = requestedQueue;
    }
    this.refresh();
    this.intakeForm.valueChanges.subscribe(() => {
      // A changed signal must be matched again; never link edited intake to a stale pursuit choice.
      this.selectedPursuitMatch = undefined;
      this.pursuitMatches = [];
    });
    const workflowId = this.route.snapshot.queryParamMap.get('workflowId');
    if (workflowId) {
      this.workflowService.get(workflowId).subscribe({
        next: (record) => this.applyWorkflowRecord(record),
        error: () => this.notification.error('Error', 'The linked workflow could not be opened.'),
      });
    }
  }

  private isWorkflowQueue(
    value: string | null
  ): value is 'all' | 'approval' | 'ready' | 'blocked' | 'review' {
    return !!value && ['all', 'approval', 'ready', 'blocked', 'review'].includes(value);
  }

  refresh(showNotification = false, preserveLastOperation = false): void {
    if (this.runningAction && this.runningAction !== 'refresh') {
      return;
    }
    const blockingRefresh = !preserveLastOperation;
    this.loading = true;
    this.reminderProposals = undefined;
    this.reminderActivationHistory = undefined;
    this.reminderDeliveryHistory = undefined;
    this.reminderProposalsUnavailable = false;
    this.reminderActivationUnavailable = false;
    this.reminderDeliveryUnavailable = false;
    if (blockingRefresh) {
      this.runningAction = 'refresh';
    }
    forkJoin({
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
    }).subscribe({
      next: ({ overview, dashboard, reminderProposals, reminderActivations, reminderDeliveries, items, approvals }) => {
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
      error: () => {
        this.loading = false;
        this.reminderProposals = undefined;
        this.reminderActivationHistory = undefined;
        this.reminderDeliveryHistory = undefined;
        if (blockingRefresh) {
          this.runningAction = undefined;
        }
        if (!preserveLastOperation) {
          this.lastOperation = {
            name: 'Refresh',
            status: 'failed',
            summary: 'One or more workflow panels failed to load.',
            at: new Date(),
          };
        }
        this.notification.error('Error', 'Failed to load the workflow operational chain.');
      }
    });
  }

  intake(): void {
    if (this.intakeForm.invalid) {
      return;
    }
    this.saving = true;
    if (this.selectedPursuitMatch) {
      this.pursuitService.intake(this.selectedPursuitMatch.pursuit.id, this.intakeForm.value).subscribe({
        next: (detail) => {
          this.saving = false;
          this.notification.success('Workflow linked to pursuit', 'Input became operational work under the selected pursuit.');
          this.selectNewestPursuitWorkflow(detail);
          this.refresh(false, true);
        },
        error: () => {
          this.saving = false;
          this.notification.error('Error', 'Failed to create workflow inside the selected pursuit.');
        },
      });
      return;
    }
    this.pursuitService.routeIntake(this.intakeForm.value).subscribe({
      next: (result) => {
        this.saving = false;
        this.pursuitMatches = result.matches || [];
        if (result.detail) {
          this.selectNewestPursuitWorkflow(result.detail);
        }
        if (result.mode === 'matched_existing') {
          this.notification.success('Workflow linked to pursuit', 'HAI matched this input to an existing pursuit before creating governed work.');
        } else if (result.createdCandidate) {
          this.notification.info('Pursuit candidate needs review', 'HAI recorded the unmatched input as a reviewable pursuit candidate. No workflow was created until an approver accepts it.');
        } else {
          this.notification.success('Workflow created', result.message || 'Input classified, checklist generated, and audit event recorded.');
        }
        this.refresh(false, true);
      },
      error: () => {
        this.saving = false;
        this.notification.error('Error', 'Failed to match and intake workflow input.');
      },
    });
  }

  matchPursuits(): void {
    const value = this.intakeForm.value;
    if (!String(value.input || '').trim()) {
      this.notification.error('Input required', 'Describe the signal before matching it to a pursuit.');
      return;
    }
    this.matchingPursuits = true;
    this.pursuitService.match({
      input: value.input,
      projectKey: value.projectKey,
      sourceType: value.sourceType,
      sourceId: value.sourceId,
      sourceUri: value.sourceUri,
      limit: 5,
    }).subscribe({
      next: (matches) => {
        this.pursuitMatches = matches;
        this.matchingPursuits = false;
        if (!matches.length) {
          this.selectedPursuitMatch = undefined;
          this.notification.info('No pursuit match', 'This signal can still create a standalone workflow.');
          return;
        }
        if (!this.selectedPursuitMatch && matches[0].score >= 0.7) {
          this.selectedPursuitMatch = matches[0];
        }
      },
      error: () => {
        this.matchingPursuits = false;
        this.notification.error('Error', 'Failed to retrieve pursuit matches.');
      },
    });
  }

  selectPursuitMatch(match: IPursuitMatchCandidate): void {
    this.selectedPursuitMatch = match;
  }

  clearPursuitMatch(): void {
    this.selectedPursuitMatch = undefined;
  }

  open(item: IWorkflowItem): void {
    this.workflowService.get(item.id).subscribe({
      next: (record) => this.applyWorkflowRecord(record),
      error: () => this.notification.error('Error', 'Failed to open workflow.'),
    });
  }

  transition(): void {
    if (!this.selected || this.transitionForm.invalid) {
      return;
    }
    this.workflowService.transition(this.selected.item.id, this.transitionForm.value).subscribe({
      next: (record) => {
        this.applyWorkflowRecord(record);
        this.notification.success('State updated', 'Workflow transition was validated and audited.');
        this.refresh(false, true);
      },
      error: () => this.notification.error('Blocked', 'Workflow transition was not allowed.'),
    });
  }

  resolveApproval(item: IWorkflowItem, approved: boolean): void {
    if (this.saving) {
      return;
    }
    this.saving = true;
    this.workflowService.resolveApproval(item.id, {
      approved,
      note: this.approvalForm.value.note,
      actor: 'operator',
    }).subscribe({
      next: (record) => {
        this.saving = false;
        this.applyWorkflowRecord(record);
        this.notification.success('Approval updated', approved ? 'Workflow approved for execution.' : 'Workflow rejected and blocked.');
        this.refresh(false, true);
      },
      error: () => {
        this.saving = false;
        this.notification.error('Error', 'Failed to update workflow approval.');
      },
    });
  }

  openReminder(proposal: IWorkflowReminderProposal): void {
    this.selectedReminder = proposal;
  }

  closeReminder(): void {
    this.selectedReminder = undefined;
  }

  openReminderWorkflow(proposal: IWorkflowReminderProposal): void {
    const item = this.items.find((candidate) => candidate.id === proposal.workflowId);
    if (item) {
      this.closeReminder();
      this.open(item);
      return;
    }
    this.workflowService.get(proposal.workflowId).subscribe({
      next: (record) => {
        this.closeReminder();
        this.applyWorkflowRecord(record);
      },
      error: () => this.notification.error('Workflow unavailable', 'Refresh the reminder before opening its workflow.'),
    });
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
      new Date(activation.latestDecision.expiresAt).getTime() > Date.now() && !this.deliveryAuthorizationFor(proposal);
  }

  authorizeReminderDelivery(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    const activation = this.activationFor(proposal);
    const decision = activation?.latestDecision;
    if (!activation || !decision || !this.canAuthorizeReminderDelivery(proposal) || this.activationBusyId) {
      return;
    }
    this.modal.confirm({
      nzTitle: 'Authorize one internal HAI reminder?',
      nzContent: 'This permits one local in-app reminder only. It cannot send email, write Calendar data, call a provider, or execute a follow-up.',
      nzOkText: 'Authorize one reminder',
      nzCancelText: 'Cancel',
      nzOnOk: () => {
        this.activationBusyId = activation.request.id;
        this.workflowService.authorizeReminderDelivery(activation.request.id, {
          expectedActivationRequestDigest: activation.request.recordDigest,
          expectedActivationDecisionDigest: decision.recordDigest,
          expectedReminderDigest: activation.request.reminderDigest,
          idempotencyKey: `ui:delivery:${activation.request.id}:${decision.recordDigest.slice(0, 16)}`,
          channel: 'in_app',
          confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER',
        }).subscribe({
          next: (result) => {
            this.activationBusyId = undefined;
            if (result?.authority !== 'internal_reminder_delivery_authorization' || result?.canExecute !== false ||
                result?.deliveryAuthorized !== true || result.authorization?.activationRequestId !== activation.request.id ||
                result.authorization?.activationDecisionId !== decision.id || result.authorization?.channel !== 'in_app' ||
                !this.validDigest(result.authorization?.recordDigest)) {
              this.notification.error('Authorization rejected', 'HAI returned invalid reminder-delivery evidence.');
              return;
            }
            this.notification.success('Internal reminder authorized', 'The local worker may record one in-app reminder. No external effect was authorized.');
            this.refresh(false, true);
          },
          error: () => {
            this.activationBusyId = undefined;
            this.notification.error('Authorization blocked', 'The approval expired, changed, or already authorized a delivery.');
          },
        });
      },
    });
  }

  runDueReminderDeliveries(): void {
    if (this.runningAction) {
      return;
    }
    this.runningAction = 'reminders';
    this.workflowService.runDueReminderDeliveries({ limit: 25 }).subscribe({
      next: (summary) => {
        this.runningAction = undefined;
        this.notification.success(
          'Internal reminder pass complete',
          `${summary.delivered} delivered, ${summary.retried} retrying, ${summary.suppressed} suppressed, ${summary.deadLettered} dead-lettered.`
        );
        this.refresh(false, true);
      },
      error: () => {
        this.runningAction = undefined;
        this.notification.error('Reminder pass blocked', 'The local reminder worker could not complete safely.');
      },
    });
  }

  canPrepareReminder(proposal: IWorkflowReminderProposal): boolean {
    const activation = this.activationFor(proposal);
    return !activation || ['rejected', 'revoked', 'expired', 'stale'].includes(activation.status);
  }

  prepareReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
    if (this.activationBusyId || !this.canPrepareReminder(proposal)) {
      return;
    }
    this.activationBusyId = proposal.checklistItemId;
    const idempotencyKey = [
      'ui', 'internal-reminder', proposal.checklistItemId,
      proposal.evidenceDigest.slice(0, 16), Date.now().toString(36),
    ].join(':');
    this.workflowService.prepareReminderActivation(proposal.checklistItemId, {
      expectedReminderDigest: proposal.evidenceDigest,
      idempotencyKey,
      activationKind: 'internal_notification',
      confirmation: 'PREPARE INTERNAL REMINDER ONLY',
    }).subscribe({
      next: (result) => {
        this.activationBusyId = undefined;
        if (result?.authority !== 'reminder_activation_request_only' || result?.canExecute !== false ||
            result.request?.activationKind !== 'internal_notification' ||
            result.request?.checklistItemId !== proposal.checklistItemId ||
            result.request?.reminderDigest !== proposal.evidenceDigest ||
            !this.validDigest(result.request?.recordDigest)) {
          this.notification.error('Preparation rejected', 'HAI returned invalid reminder preparation evidence.');
          return;
        }
        this.notification.success(
          result.replayed ? 'Preparation already recorded' : 'Internal reminder prepared',
          'Nothing was sent and no calendar event was created. Owner approval remains separate.'
        );
        this.refresh(false, true);
      },
      error: () => {
        this.activationBusyId = undefined;
        this.notification.error('Preparation blocked', 'The reminder changed or could not be prepared safely.');
      },
    });
  }

  reviewReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
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
        nzOnOk: () => this.decideReminderActivation(activation, 'revoked'),
      });
      return;
    }
    if (!['prepared', 'needs_clarification'].includes(activation.status)) {
      return;
    }
    this.modal.confirm({
      nzTitle: 'Approve this internal reminder preparation?',
      nzContent: 'Approval remains non-executing. A future effect would still require separate authorization and verification.',
      nzOkText: 'Approve preparation',
      nzCancelText: 'Cancel',
      nzOnOk: () => this.decideReminderActivation(activation, 'approved'),
    });
  }

  rejectReminderActivation(proposal: IWorkflowReminderProposal, event: Event): void {
    event.stopPropagation();
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
      nzOnOk: () => this.decideReminderActivation(activation, 'rejected'),
    });
  }

  private decideReminderActivation(
    activation: IWorkflowReminderActivationHistoryItem,
    decision: 'approved' | 'rejected' | 'revoked'
  ): void {
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
    this.activationBusyId = activation.request.id;
    this.workflowService.decideReminderActivation(activation.request.id, {
      decision,
      reason: reason[decision],
      confirmation: confirmation[decision],
      expectedActivationRequestDigest: activation.request.recordDigest,
      expectedPreviousDecisionId: activation.latestDecision?.id,
    }).subscribe({
      next: (result) => {
        this.activationBusyId = undefined;
        if (result?.authority !== 'reminder_activation_decision_only' || result?.canExecute !== false ||
            result.decision?.activationRequestId !== activation.request.id ||
            result.decision?.activationRequestDigest !== activation.request.recordDigest ||
            result.decision?.decision !== decision || !this.validDigest(result.decision?.recordDigest)) {
          this.notification.error('Decision rejected', 'HAI returned invalid reminder decision evidence.');
          return;
        }
        this.notification.success('Reminder decision recorded', 'The immutable decision was saved. No external action was executed.');
        this.refresh(false, true);
      },
      error: () => {
        this.activationBusyId = undefined;
        this.notification.error('Decision blocked', 'The reminder request expired or changed. Prepare a fresh request.');
      },
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
    const requestIds = new Set(snapshot.items.map((item) => item?.request?.id));
    const allowedStatuses = ['prepared', 'approved', 'rejected', 'needs_clarification', 'revoked', 'expired', 'stale'];
    return requestIds.size === snapshot.items.length && snapshot.items.every((item) => {
      const request = item?.request;
      const decision = item?.latestDecision;
      if (!request?.id || !request.workflowId || !request.checklistItemId ||
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
      return decision.activationRequestId === request.id &&
        decision.activationRequestDigest === request.recordDigest &&
        decision.authority === 'reminder_activation_decision_only' &&
        ['approved', 'rejected', 'needs_clarification', 'revoked'].includes(decision.decision) &&
        this.validDigest(decision.requestDigest) && this.validDigest(decision.recordDigest) &&
        !Number.isNaN(new Date(decision.decidedAt || '').getTime());
    });
  }

  private validReminderDeliveryHistory(history?: IWorkflowReminderDeliveryHistory): history is IWorkflowReminderDeliveryHistory {
    if (history?.authority !== 'internal_reminder_delivery_receipt' || history?.canExecute !== false ||
        !Array.isArray(history.authorizations) || !Array.isArray(history.attempts)) {
      return false;
    }
    const authorizationIds = new Set(history.authorizations.map((item) => item?.id));
    if (authorizationIds.size !== history.authorizations.length || !history.authorizations.every((item) =>
      !!item?.id && !!item.activationRequestId && !!item.activationDecisionId && !!item.workflowId && !!item.checklistItemId &&
      item.channel === 'in_app' && item.authority === 'internal_reminder_delivery_authorization' &&
      item.confirmation === 'AUTHORIZE ONE INTERNAL HAI REMINDER' && this.validDigest(item.reminderDigest) &&
      this.validDigest(item.activationRequestDigest) && this.validDigest(item.activationDecisionDigest) &&
      this.validDigest(item.requestDigest) && this.validDigest(item.recordDigest)
    )) {
      return false;
    }
    return history.attempts.every((item) => authorizationIds.has(item?.authorizationId) &&
      Number.isSafeInteger(item.attemptNumber) && item.attemptNumber >= 1 && item.attemptNumber <= 3 &&
      ['delivered', 'retryable_failure', 'suppressed', 'dead_lettered'].includes(item.status) &&
      item.authority === 'internal_reminder_delivery_receipt' && this.validDigest(item.reminderDigest) &&
      this.validDigest(item.authorizationDigest) && this.validDigest(item.recordDigest)
    );
  }

  private validDigest(value?: string): boolean {
    return /^[0-9a-f]{64}$/.test(String(value || ''));
  }

  resolveInterruptedExecution(): void {
    if (!this.selected || this.interruptionForm.invalid) {
      return;
    }
    const request = {
      ...this.interruptionForm.value,
      actor: 'operator',
    };
    if (request.decision === 'confirm_completed' && !request.evidenceUri?.trim()) {
      this.notification.error('Evidence required', 'Add a source URI before confirming completion.');
      return;
    }
    this.saving = true;
    this.workflowService.resolveInterruptedExecution(this.selected.item.id, request).subscribe({
      next: (record) => {
        this.applyWorkflowRecord(record);
        this.interruptionForm.reset({
          decision: 'retry',
          note: '',
          evidenceUri: '',
          evidenceLabel: '',
        });
        this.saving = false;
        this.notification.success('Interruption resolved', `Recovery decision recorded: ${request.decision}.`);
        this.refresh(false, true);
      },
      error: () => {
        this.saving = false;
        this.notification.error('Resolution blocked', 'The interrupted execution could not be resolved.');
      },
    });
  }

  runDue(): void {
    if (this.runningAction) {
      return;
    }
    this.runningAction = 'worker';
    this.workflowService.runDue({ limit: 10 }).subscribe({
      next: (summary) => {
        this.runSummary = summary;
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Run worker',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.completed} completed, ${summary.retried} retried, ${summary.blocked} blocked, ${summary.skipped} skipped.`,
          details: this.workflowRunDetails(summary),
          at: new Date(),
        };
        this.notification.success('Worker run complete', `${summary.completed} completed, ${summary.retried} retried, ${summary.blocked} blocked.`);
        this.reloadSelectedWorkflow();
        this.refresh(false, true);
      },
      error: () => {
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Run worker',
          status: 'failed',
          summary: 'Workflow worker run failed before completion.',
          at: new Date(),
        };
        this.notification.error('Error', 'Workflow worker run failed.');
      },
    });
  }

  recoverStaleClaims(): void {
    if (this.runningAction) {
      return;
    }
    this.runningAction = 'recovery';
    this.workflowService.recoverStaleClaims({ limit: 50 }).subscribe({
      next: (summary) => {
        this.recoverySummary = summary;
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Recover stale',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.workflowsBlocked} workflows blocked for review, ${summary.openLoopsReopened} follow-ups reopened, ${summary.skipped} skipped.`,
          details: this.claimRecoveryDetails(summary),
          at: new Date(),
        };
        this.notification.success(
          'Claim recovery complete',
          `${summary.workflowsBlocked} workflows blocked for review, ${summary.openLoopsReopened} follow-ups reopened.`
        );
        this.refresh(false, true);
      },
      error: () => {
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Recover stale',
          status: 'failed',
          summary: 'Stale claim recovery failed before completion.',
          at: new Date(),
        };
        this.notification.error('Error', 'Stale claim recovery failed.');
      },
    });
  }

  runDueOpenLoops(): void {
    if (this.runningAction) {
      return;
    }
    this.runningAction = 'followups';
    this.workflowService.runDueOpenLoops({ limit: 10 }).subscribe({
      next: (summary) => {
        this.openLoopRunSummary = summary;
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Run follow-ups',
          status: 'completed',
          summary: `${summary.checked} checked, ${summary.triggered} triggered, ${summary.resolved} resolved, ${summary.skipped} skipped.`,
          details: this.openLoopRunDetails(summary),
          at: new Date(),
        };
        this.notification.success('Open loops processed', `${summary.triggered} triggered, ${summary.resolved} resolved.`);
        this.refresh(false, true);
      },
      error: () => {
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Run follow-ups',
          status: 'failed',
          summary: 'Open-loop worker run failed before completion.',
          at: new Date(),
        };
        this.notification.error('Error', 'Open-loop worker run failed.');
      },
    });
  }

  resolveProposal(
    proposalId: string,
    status: 'approved' | 'changes_requested' | 'rejected',
    selectedOption?: string,
  ): void {
    if (!this.selected) {
      return;
    }
    const approved = status === 'approved';
    const noteByStatus: Record<string, string> = {
      approved: 'Proposal approved from dashboard.',
      changes_requested: 'Proposal needs changes from dashboard.',
      rejected: 'Proposal rejected from dashboard.',
    };
    this.workflowService.resolveProposal(this.selected.item.id, proposalId, {
      approved,
      status,
      selectedOption,
      note: noteByStatus[status],
      actor: 'operator',
    }).subscribe({
      next: (record) => {
        this.applyWorkflowRecord(record);
        this.notification.success('Proposal updated', noteByStatus[status]);
        this.refresh();
      },
      error: () => this.notification.error('Error', 'Failed to update proposal.'),
    });
  }

  runSelectedWorkflow(): void {
    const item = this.selected?.item;
    if (!item || item.currentState !== 'ready' || item.approvalStatus !== 'approved' || this.runningAction) {
      return;
    }
    this.modal.confirm({
      nzTitle: 'Run this approved workflow?',
      nzContent: 'HAI will claim only this workflow. Any concrete task or runtime action still passes authorization, emergency-stop, audit, and verification gates.',
      nzOkText: 'Run this workflow',
      nzCancelText: 'Cancel',
      nzOnOk: () => this.executeSelectedWorkflow(item.id),
    });
  }

  private executeSelectedWorkflow(id: string): void {
    this.runningAction = 'selected';
    this.workflowService.runOne(id).subscribe({
      next: (result) => {
        this.runningAction = undefined;
        const completed = result.status === 'completed';
        this.lastOperation = {
          name: 'Run selected workflow',
          status: completed ? 'completed' : 'failed',
          summary: this.workflowResultSummary(result),
          details: result.message,
          at: new Date(),
        };
        if (completed) {
          this.notification.success('Workflow completed', 'The selected workflow completed and its result was verified.');
        } else {
          this.notification.warning('Workflow needs attention', this.workflowResultSummary(result));
        }
        this.reloadSelectedWorkflow();
        this.refresh(false, true);
      },
      error: () => {
        this.runningAction = undefined;
        this.lastOperation = {
          name: 'Run selected workflow',
          status: 'failed',
          summary: 'The selected workflow could not be started.',
          at: new Date(),
        };
        this.notification.error('Execution failed', 'The selected workflow could not be started. No other workflow was run.');
      },
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
    if (!this.selected) {
      return;
    }
    this.workflowService.updateChecklistItem(this.selected.item.id, itemId, { status }).subscribe({
      next: (record) => this.applyWorkflowRecord(record),
      error: () => this.notification.error('Error', 'Failed to update checklist item.'),
    });
  }

  get interruptionRequiresEvidence(): boolean {
    return this.interruptionForm.get('decision')?.value === 'confirm_completed';
  }

  applyWorkflowRecord(record: IWorkflowRecord): void {
    this.selected = record;
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
    this.workflowService.frameworkSelection(provenance.selectionDecisionId).subscribe({
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
    if (queue === 'ready') return this.dashboard?.readyItems?.length || this.count('ready');
    if (queue === 'blocked') return this.dashboard?.blockedItems?.length || this.count('blocked');
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

  focusFilters(): void {
    document.getElementById('workflow-state-filter')?.focus();
  }

  focusIntake(): void {
    const element = document.getElementById('workflow-intake-input');
    if (element) {
      element.focus();
      element.scrollIntoView({ behavior: 'smooth', block: 'center' });
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
    return !!this.runningAction;
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
    this.workflowService.get(workflowId).subscribe({
      next: (record) => {
        if (this.selected?.item?.id === workflowId) {
          this.applyWorkflowRecord(record);
        }
      },
      error: () => {
        this.notification.warning(
          'Workflow detail not refreshed',
          'The worker result is available, but the selected workflow detail could not be reloaded.'
        );
      },
    });
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

  private selectNewestPursuitWorkflow(detail: IPursuitDetail): void {
    const newest = [...(detail.workflows || [])].sort((left, right) => {
      return new Date(right.updatedAt || right.createdAt).getTime() - new Date(left.updatedAt || left.createdAt).getTime();
    })[0];
    if (newest) {
      this.open(newest);
    }
  }
}

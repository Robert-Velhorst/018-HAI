import { FormBuilder } from '@angular/forms';
import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { convertToParamMap } from '@angular/router';
import { ActivatedRoute } from '@angular/router';
import { RouterTestingModule } from '@angular/router/testing';
import { of, Subject, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { WORKFLOW_SERVICE_TOKEN } from '../../services/workflow/workflow.service.token';
import { PursuitService } from '../../services/pursuit.service';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import {
  IWorkflowDashboard,
  IWorkflowFrameworkSelectionDecision,
  IWorkflowFrameworkSelectionProvenance,
  IWorkflowItem,
  IWorkflowRecord,
} from '../../models/workflow.model.interface';
import { WorkflowEngineComponent } from './workflow-engine.component';

const workflowId = '11111111-1111-4111-8111-111111111111';
const checklistItemId = '22222222-2222-4222-8222-222222222222';
const proposalId = '33333333-3333-4333-8333-333333333333';
const activationId = 'dddddddd-dddd-4ddd-8ddd-dddddddddddd';
const reminderId = 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee';
const workflowStates = [
  'new_input', 'classified', 'linked', 'checklist_generated', 'waiting_external_input',
  'needs_approval', 'ready', 'in_progress', 'completed', 'archived', 'blocked',
];

function validOverview() {
  return { capabilities: [], states: [...workflowStates], safetyRules: [], rules: [] };
}

describe('WorkflowEngineComponent', () => {
  const selectionId = 'c595d075-5412-4e7f-bff4-1a9df360451a';
  const taskPlanId = 'task-plan-42';
  const digest = 'a'.repeat(64);
  const provenance: IWorkflowFrameworkSelectionProvenance = {
    selectionDecisionId: selectionId,
    taskPlanId,
    catalogVersion: '2026.07.1',
    catalogDigest: digest,
    selectorAlgorithmVersion: 'chief-of-staff-v1',
    effectivePreferenceDigest: 'b'.repeat(64),
    constitutionVersion: 3,
    constitutionDigest: 'c'.repeat(64),
    constitutionSource: 'owner-constitution:v3',
  };

  const registryDecision: IWorkflowFrameworkSelectionDecision = {
    id: selectionId,
    taskPlanId,
    createdAt: '2026-07-30T10:00:00Z',
    catalogVersion: provenance.catalogVersion,
    catalogDigest: provenance.catalogDigest,
    selectorAlgorithmVersion: provenance.selectorAlgorithmVersion,
    effectivePreferenceDigest: provenance.effectivePreferenceDigest,
    constitutionDigest: provenance.constitutionDigest,
    lifeDomain: 'work',
    needOrCommitment: 'Complete governed work',
    selected: [
      {
        id: 'human-sovereignty',
        version: '1.0.0',
        name: 'Human sovereignty',
        family: 'governance',
        score: 100,
        reasons: ['Required safety overlay'],
        maximumAutonomyLevel: 2,
        authorityRequirement: 'owner',
        evidenceRequirements: ['selection decision'],
        evaluationMethod: ['policy check'],
      },
    ],
    conflicts: [],
    requiredAgents: ['planner'],
    maximumAutonomyLevel: 2,
    authoritySummary: 'Owner retains authority.',
    requiresApproval: false,
    approvalReasons: [],
    evidenceRequirements: ['selection decision'],
    completionCriteria: ['verified result'],
    learningPlan: ['Record only verified lessons.'],
    contextRequirements: ['Owner-scoped source records.'],
    selectionReason: 'Required protected framework.',
    constitutionVersion: provenance.constitutionVersion,
    constitutionSource: provenance.constitutionSource,
  };

  function workflowRecord(
    frameworkSelections: IWorkflowFrameworkSelectionProvenance[]
  ): IWorkflowRecord {
    return {
      item: {
        id: workflowId,
        title: 'Prepare evidence',
        currentState: 'completed',
        taskType: 'legal',
        riskLevel: 'high',
        priorityScore: 90,
        confidence: 0.9,
        autonomyLevel: 'suggest',
        requiresApproval: true,
        approvalStatus: 'approved',
        retryCount: 0,
        maxRetries: 3,
        lastTaskPlanId: taskPlanId,
        archived: false,
        createdAt: '2026-07-30T09:00:00Z',
        updatedAt: '2026-07-30T10:00:00Z',
      },
      checklist: [],
      intake: [],
      matches: [],
      pursuits: [],
      evidence: [],
      openLoops: [],
      proposals: [],
      qualityGates: [],
      transitions: [],
      sourceLinks: [],
      decisions: [],
      events: [],
      frameworkSelections,
    };
  }

  function createComponent(): {
    component: WorkflowEngineComponent;
    workflowService: jasmine.SpyObj<any>;
    pursuitService: jasmine.SpyObj<any>;
    notification: jasmine.SpyObj<any>;
    modal: jasmine.SpyObj<any>;
    router: jasmine.SpyObj<any>;
    changeDetector: jasmine.SpyObj<any>;
  } {
    const workflowService = jasmine.createSpyObj('workflowService', [
      'overview',
      'dashboard',
      'reminderProposals',
      'prepareReminderActivation',
      'reminderActivationHistory',
      'decideReminderActivation',
      'reminderActivationDecisionHistory',
      'authorizeReminderDelivery',
      'reminderDeliveryHistory',
      'runDueReminderDeliveries',
      'items',
      'approvals',
      'get',
      'frameworkSelection',
      'transition',
      'resolveApproval',
      'resolveInterruptedExecution',
      'resolveProposal',
      'updateChecklistItem',
      'runDue',
      'runOne',
      'runDueOpenLoops',
      'recoverStaleClaims',
    ]);
    const pursuitService = jasmine.createSpyObj('pursuitService', ['intake', 'routeIntake', 'match']);
    const notification = jasmine.createSpyObj('notification', ['success', 'info', 'warning', 'error']);
    const modal = jasmine.createSpyObj('modal', ['confirm']);
    const route = { snapshot: { queryParamMap: convertToParamMap({}) } } as any;
    const router = jasmine.createSpyObj('router', ['navigate']);
    const changeDetector = jasmine.createSpyObj('changeDetector', ['detectChanges']);
    const component = new WorkflowEngineComponent(
      new FormBuilder(), workflowService, pursuitService, notification, modal, route, router, changeDetector,
      TestBed.inject(ModuleViewPreferencesService),
    );
    spyOn(component, 'refresh');
    return { component, workflowService, pursuitService, notification, modal, router, changeDetector };
  }

  it('does not start the worker until fresh, ready work is available', () => {
    const { component, workflowService } = createComponent();
    component.dataLoaded = false;
    component.runDue();
    expect(workflowService.runDue).not.toHaveBeenCalled();

    component.dashboard = {
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [{ id: workflowId } as IWorkflowItem],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    };
    component.dataLoaded = true;
    component.loadFailed = true;
    component.runDue();
    expect(workflowService.runDue).not.toHaveBeenCalled();

    component.loadFailed = false;
    component.dashboard.readyItems = [];
    component.runDue();
    expect(workflowService.runDue).not.toHaveBeenCalled();
  });

  it('reports expired reminder authorizations distinctly from suppressions and delivery failures', () => {
    const { component, workflowService, notification } = createComponent();
    component.dataLoaded = true;
    component.loadFailed = false;
    component.reminderDeliveryHistory = {
      authorizations: [{
        id: checklistItemId,
        activationRequestId: workflowId,
        activationDecisionId: proposalId,
        workflowId,
        checklistItemId,
        reminderAt: '2026-10-09T12:00:00Z',
        channel: 'in_app',
        idempotencyKey: `test:${checklistItemId}`,
        authority: 'internal_reminder_delivery_authorization',
        confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER',
        reminderDigest: digest,
        activationRequestDigest: digest,
        activationDecisionDigest: digest,
        requestDigest: digest,
        recordDigest: digest,
        authorizedAt: '2026-10-09T10:00:00Z',
        expiresAt: '2026-10-10T10:00:00Z',
      }],
      attempts: [], authority: 'internal_reminder_delivery_receipt', canExecute: false,
    };
    workflowService.runDueReminderDeliveries.and.returnValue(of({
      checked: 1, delivered: 0, retried: 0, suppressed: 0, deadLettered: 0, expired: 1,
      results: [{ authorizationId: checklistItemId, status: 'expired', reason: 'Authorization expired.' }],
    }));

    component.runDueReminderDeliveries();

    expect(notification.info).toHaveBeenCalledWith(
      'Internal reminder response received',
      jasmine.stringMatching(/0 internal signals, 0 retries, 0 suppressed, 0 dead-lettered and 1 expired/)
    );
  });

  it('treats a present empty dashboard queue as authoritative over stale aggregate counts', () => {
    const { component, workflowService } = createComponent();
    component.dashboard = {
      counts: { ready: 3, blocked: 2 },
      approvalItems: [],
      blockedItems: [],
      readyItems: [],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    component.dataLoaded = true;

    expect(component.queueCount('ready')).toBe(0);
    expect(component.queueCount('blocked')).toBe(0);
    component.runDue();
    expect(workflowService.runDue).not.toHaveBeenCalled();
  });

  it('renders returned pursuit matches immediately after an operator requests them', () => {
    const { component, pursuitService, changeDetector } = createComponent();
    component.intakeForm.patchValue({ input: 'Prepare the evidence bundle', projectKey: 'vivare' });
    pursuitService.match.and.returnValue(of([{
      pursuit: { id: '11111111-1111-4111-8111-111111111111', title: 'Vivare evidence bundle' },
      score: 0.9,
      confidence: 'high',
      reasons: ['project key matches'],
    }]));

    component.matchPursuits();

    expect(component.pursuitMatches.length).toBe(1);
    expect(component.selectedPursuitMatch?.pursuit.id).toBe('11111111-1111-4111-8111-111111111111');
    expect(changeDetector.detectChanges).toHaveBeenCalled();
  });

  it('starts manual intake without executable demo provenance', () => {
    const { component } = createComponent();

    expect(component.intakeForm.value).toEqual({
      input: '',
      successCriteriaText: '',
      projectKey: '',
      automationId: '',
      sourceType: 'manual',
      sourceId: '',
      sourceUri: '',
      sourceLabel: '',
      contentType: 'note',
      sender: '',
      trigger: 'manual_intake',
    });
    expect(component.intakeForm.invalid).toBeTrue();
  });

  it('routes explicitly entered outcome criteria as separate success criteria', () => {
    const { component, pursuitService } = createComponent();
    component.intakeForm.patchValue({
      input: 'Prepare the evidence bundle',
      successCriteriaText: ' Include the March 19 email. \n\nLink every item to its source.  ',
    });
    pursuitService.routeIntake.and.returnValue(of({
      mode: 'candidate_created', matched: false, createdCandidate: true,
      pursuitId: '11111111-1111-4111-8111-111111111111', matches: [],
    }));

    component.intake();

    expect(pursuitService.routeIntake).toHaveBeenCalledWith(jasmine.objectContaining({
      successCriteria: ['Include the March 19 email.', 'Link every item to its source.'],
    }));
    expect(pursuitService.routeIntake.calls.mostRecent().args[0]).not.toEqual(
      jasmine.objectContaining({ successCriteriaText: jasmine.anything() })
    );
  });

  it('accepts only current non-executing reminder snapshots with exact unique items', () => {
    const { component } = createComponent();
    const snapshot: any = {
      items: [{
        id: checklistItemId,
        workflowId,
        checklistItemId,
        title: 'Prepare evidence',
        label: 'Review before deadline',
        workflowState: 'needs_approval',
        riskLevel: 'high',
        requiresApproval: true,
        reminderAt: '2026-08-04T10:00:00Z',
        status: 'due',
        nextAction: 'Review before any external effect.',
        evidenceDigest: digest,
        authority: 'reminder_proposal_only',
        canExecute: false,
      }],
      due: 1,
      upcoming: 0,
      authority: 'reminder_proposal_only',
      canExecute: false,
      freshness: {
        status: 'current_internal_reminder_snapshot',
        revalidationRequired: true,
        checkedAt: '2026-08-04T10:00:00Z',
        reason: 'Revalidate before any effect.',
      },
    };

    expect((component as any).validReminderProposalSnapshot(snapshot)).toBeTrue();
    expect((component as any).validReminderProposalSnapshot({ ...snapshot, canExecute: true })).toBeFalse();
    expect((component as any).validReminderProposalSnapshot({
      ...snapshot,
      due: 0,
      upcoming: 1,
    })).toBeFalse();
    expect((component as any).validReminderProposalSnapshot({
      ...snapshot,
      items: [snapshot.items[0], snapshot.items[0]],
      due: 2,
    })).toBeFalse();
  });

  it('prepares only immutable internal reminder evidence', () => {
    const { component, workflowService, notification } = createComponent();
    component.dataLoaded = true;
    const proposal: any = {
      id: reminderId,
      workflowId: '11111111-1111-4111-8111-111111111111',
      checklistItemId: '22222222-2222-4222-8222-222222222222',
      title: 'Review evidence',
      label: 'Internal reminder',
      workflowState: 'ready',
      riskLevel: 'high',
      requiresApproval: true,
      reminderAt: '2026-08-05T10:00:00Z',
      status: 'due',
      nextAction: 'Review internally.',
      evidenceDigest: digest,
      authority: 'reminder_proposal_only',
      canExecute: false,
    };
    workflowService.prepareReminderActivation.and.callFake((_id: string, request: any) => of({
      request: {
        id: '33333333-3333-4333-8333-333333333333',
        checklistItemId: proposal.checklistItemId,
        workflowId: proposal.workflowId,
        idempotencyKey: request.idempotencyKey,
        authority: 'reminder_activation_request_only',
        confirmation: 'PREPARE INTERNAL REMINDER ONLY',
        requestDigest: 'c'.repeat(64),
        activationKind: 'internal_notification',
        reminderDigest: digest,
        recordDigest: 'b'.repeat(64),
      },
      replayed: false,
      authority: 'reminder_activation_request_only',
      canExecute: false,
    }));
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.prepareReminderActivation(proposal, event);

    expect(event.stopPropagation).toHaveBeenCalled();
    expect(workflowService.prepareReminderActivation).toHaveBeenCalledWith(
      proposal.checklistItemId,
      jasmine.objectContaining({
        expectedReminderDigest: digest,
        activationKind: 'internal_notification',
        confirmation: 'PREPARE INTERNAL REMINDER ONLY',
      })
    );
    expect(notification.info).toHaveBeenCalledWith(
      'Server reported internal reminder preparation',
      'Inspect preparation evidence. Owner approval and delivery authorization remain separate; this response does not prove delivery.'
    );
    expect(component.refresh).toHaveBeenCalledWith(false, true);
  });

  it('does not turn approval-dialog cancellation into rejection', () => {
    const { component, workflowService, modal } = createComponent();
    component.dataLoaded = true;
    const proposal: any = { checklistItemId };
    component.reminderActivationHistory = {
      items: [{
        request: { id: activationId, checklistItemId },
        status: 'prepared',
        current: true,
        canExecute: false,
      }],
      authority: 'reminder_activation_history_only',
      canExecute: false,
      checkedAt: '2026-08-05T10:00:00Z',
    } as any;
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.reviewReminderActivation(proposal, event);

    const config = modal.confirm.calls.mostRecent().args[0];
    expect(config.nzCancelText).toBe('Cancel');
    expect(config.nzOnCancel).toBeUndefined();
    expect(workflowService.decideReminderActivation).not.toHaveBeenCalled();
  });

  it('revalidates a reminder decision after its confirmation dialog has been opened', () => {
    const { component, workflowService, modal, notification } = createComponent();
    component.dataLoaded = true;
    const activation: any = {
      request: { id: activationId, checklistItemId, recordDigest: digest },
      status: 'prepared',
      current: true,
      canExecute: false,
    };
    component.reminderActivationHistory = {
      items: [activation], authority: 'reminder_activation_history_only', canExecute: false,
      checkedAt: new Date().toISOString(),
    } as any;
    const proposal: any = { checklistItemId };
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.reviewReminderActivation(proposal, event);
    component.reminderActivationHistory!.items = [];
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(workflowService.decideReminderActivation).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Reminder decision unavailable',
      'Refresh and review the current reminder state before deciding.'
    );
  });

  it('keeps workflow data usable when reminder proposals fail independently', () => {
    const { component, workflowService, notification } = createComponent();
    workflowService.overview.and.returnValue(of(validOverview()));
    workflowService.dashboard.and.returnValue(of({
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [], highRiskItems: [],
      itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    }));
    workflowService.reminderProposals.and.returnValue(throwError(() => new Error('reminder unavailable')));
    workflowService.reminderActivationHistory.and.returnValue(of({
      items: [],
      authority: 'reminder_activation_history_only',
      canExecute: false,
      checkedAt: '2026-08-05T10:00:00Z',
    }));
    workflowService.reminderDeliveryHistory.and.returnValue(of({
      authorizations: [],
      attempts: [],
      authority: 'internal_reminder_delivery_receipt',
      canExecute: false,
    }));
    workflowService.items.and.returnValue(of([]));
    workflowService.approvals.and.returnValue(of([]));
    (component.refresh as jasmine.Spy).and.callThrough();

    component.refresh();

    expect(component.loading).toBeFalse();
    expect(component.reminderProposalsUnavailable).toBeTrue();
    expect(component.reminderActivationUnavailable).toBeFalse();
    expect(component.dashboard).toBeDefined();
    expect(notification.error).not.toHaveBeenCalled();
  });

  it('cancels an obsolete refresh so stale workflows cannot replace the newest queue', () => {
    const { component, workflowService } = createComponent();
    const firstItems = new Subject<any[]>();
    const secondItems = new Subject<any[]>();
    workflowService.overview.and.returnValue(of(validOverview()));
    workflowService.dashboard.and.returnValue(of({
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [], highRiskItems: [],
      itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    }));
    workflowService.reminderProposals.and.returnValue(of(undefined));
    workflowService.reminderActivationHistory.and.returnValue(of(undefined));
    workflowService.reminderDeliveryHistory.and.returnValue(of(undefined));
    workflowService.items.and.returnValues(firstItems.asObservable(), secondItems.asObservable());
    workflowService.approvals.and.returnValue(of([]));
    (component.refresh as jasmine.Spy).and.callThrough();

    component.refresh();
    component.refresh();

    secondItems.next([{ id: workflowId, title: 'new workflow', currentState: 'ready', requiresApproval: false, archived: false, approvalStatus: 'not_required' }]);
    secondItems.complete();
    firstItems.next([{ id: '44444444-4444-4444-8444-444444444444', title: 'old workflow', currentState: 'ready', requiresApproval: false, archived: false, approvalStatus: 'not_required' }]);
    firstItems.complete();

    expect(component.items.map((item) => item.id)).toEqual([workflowId]);
  });

  it('authorizes exactly one internal reminder from a current approved decision', () => {
    const { component, workflowService, modal } = createComponent();
    component.dataLoaded = true;
    const requestId = '11111111-1111-4111-8111-111111111111';
    const decisionId = '22222222-2222-4222-8222-222222222222';
    const checklistItemId = '33333333-3333-4333-8333-333333333333';
    const workflowId = '44444444-4444-4444-8444-444444444444';
    const proposal: any = { checklistItemId };
    component.reminderActivationHistory = {
      items: [{
        request: {
          id: requestId, checklistItemId, workflowId, recordDigest: digest,
          reminderDigest: 'b'.repeat(64),
        },
        latestDecision: {
          id: decisionId, recordDigest: 'c'.repeat(64),
          decision: 'approved', activationRequestId: requestId, activationRequestDigest: digest,
          authority: 'reminder_activation_decision_only', confirmation: 'APPROVE INTERNAL REMINDER PREPARATION',
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
        },
        status: 'approved', current: true, canExecute: false,
      }],
      authority: 'reminder_activation_history_only', canExecute: false, checkedAt: new Date().toISOString(),
    } as any;
    component.reminderDeliveryHistory = {
      authorizations: [], attempts: [], authority: 'internal_reminder_delivery_receipt', canExecute: false,
    };
    workflowService.authorizeReminderDelivery.and.returnValue(of({
      authorization: {
        id: '55555555-5555-4555-8555-555555555555', activationRequestId: requestId, activationDecisionId: decisionId,
        workflowId, checklistItemId, activationRequestDigest: digest, activationDecisionDigest: 'c'.repeat(64),
        reminderDigest: 'b'.repeat(64), requestDigest: 'e'.repeat(64),
        authority: 'internal_reminder_delivery_authorization', confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER',
        idempotencyKey: `ui:delivery:${requestId}:${'c'.repeat(16)}`,
        channel: 'in_app', recordDigest: 'd'.repeat(64),
      },
      replayed: false, authority: 'internal_reminder_delivery_authorization', deliveryAuthorized: true, canExecute: false,
    }));
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.authorizeReminderDelivery(proposal, event);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(workflowService.authorizeReminderDelivery).toHaveBeenCalledWith(requestId, jasmine.objectContaining({
      expectedActivationRequestDigest: digest,
      expectedActivationDecisionDigest: 'c'.repeat(64),
      expectedReminderDigest: 'b'.repeat(64),
      channel: 'in_app',
      confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER',
    }));
    expect(component.refresh).toHaveBeenCalledWith(false, true);
  });

  it('opens a reminder-specific inspector before exposing workflow controls', () => {
    const { component, workflowService } = createComponent();
    const proposal: any = {
      id: reminderId,
      workflowId,
      checklistItemId,
      title: 'Review evidence',
    };

    component.openReminder(proposal);

    expect(component.selectedReminder).toBe(proposal);
    expect(component.selected).toBeUndefined();
    expect(workflowService.get).not.toHaveBeenCalled();
  });

  it('records rejection only through the explicit reject confirmation', () => {
    const { component, workflowService, modal } = createComponent();
    component.dataLoaded = true;
    const requestId = '11111111-1111-4111-8111-111111111111';
    const proposal: any = { checklistItemId };
    component.reminderActivationHistory = {
      items: [{
        request: {
          id: requestId,
          checklistItemId,
          recordDigest: digest,
        },
        status: 'prepared',
        current: true,
        canExecute: false,
      }],
      authority: 'reminder_activation_history_only',
      canExecute: false,
      checkedAt: '2026-08-05T10:00:00Z',
    } as any;
    workflowService.decideReminderActivation.and.returnValue(of({
      decision: {
        id: '22222222-2222-4222-8222-222222222222',
        activationRequestId: requestId,
        activationRequestDigest: digest,
        decision: 'rejected',
        confirmation: 'REJECT INTERNAL REMINDER PREPARATION',
        authority: 'reminder_activation_decision_only',
        requestDigest: 'd'.repeat(64),
        recordDigest: 'c'.repeat(64),
      },
      replayed: false,
      authority: 'reminder_activation_decision_only',
      canExecute: false,
    }));
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.rejectReminderActivation(proposal, event);
    const config = modal.confirm.calls.mostRecent().args[0];
    expect(workflowService.decideReminderActivation).not.toHaveBeenCalled();
    config.nzOnOk();

    expect(workflowService.decideReminderActivation).toHaveBeenCalledWith(requestId, {
      decision: 'rejected',
      reason: 'Owner rejected this internal reminder preparation.',
      confirmation: 'REJECT INTERNAL REMINDER PREPARATION',
      expectedActivationRequestDigest: digest,
      expectedPreviousDecisionId: undefined,
    });
  });

  it('recognizes exact automation-selection options and links setup to automations', () => {
    const { component, router } = createComponent();
    const option = 'Use Mail draft runtime [automation:11111111-1111-1111-1111-111111111111] - email capability matched';

    expect(component.isAutomationSelectionProposal('Select an automation for controlled execution')).toBeTrue();
    expect(component.isAutomationOption(option)).toBeTrue();
    expect(component.automationOptionLabel(option)).toBe('Mail draft runtime');
    expect(component.isAutomationOption('Configure a suitable automation')).toBeFalse();
    const record = workflowRecord([]);
    record.proposals = [{
      id: proposalId,
      workflowId,
      recommendedAction: 'Select an automation for controlled execution',
      options: option,
      status: 'open',
      createdAt: '2026-08-04T00:00:00Z',
      updatedAt: '2026-08-04T00:00:00Z',
    }];
    expect(component.hasOpenAutomationSelection(record)).toBeTrue();

    component.openAutomationSetup();

    expect(router.navigate).toHaveBeenCalledWith(['/home']);
  });

  it('does not claim that candidate intake created governed work', () => {
    const { component, pursuitService, notification } = createComponent();
    component.dataLoaded = true;
    component.intakeForm.patchValue({
      input: 'Prepare a source-grounded operational brief.',
    });
    pursuitService.routeIntake.and.returnValue(of({
      mode: 'candidate_created',
      matched: false,
      createdCandidate: true,
      pursuitId: '11111111-1111-4111-8111-111111111111',
      matches: [],
    }));

    component.intake();

    expect(notification.info).toHaveBeenCalledWith(
      'Pursuit candidate needs review',
      'The server returned a pursuit candidate for review. This acknowledgement does not prove workflow creation or task execution.'
    );
    expect(notification.success).not.toHaveBeenCalledWith(
      'Pursuit candidate created',
      jasmine.anything()
    );
    expect(component.selected).toBeUndefined();
  });

  it('verifies present workflow provenance against the exact registry selection', () => {
    const { component, workflowService } = createComponent();
    workflowService.get.and.returnValue(of(workflowRecord([provenance])));
    workflowService.frameworkSelection.and.returnValue(of(registryDecision));

    component.open(workflowRecord([provenance]).item);

    expect(workflowService.frameworkSelection).toHaveBeenCalledOnceWith(selectionId);
    expect(component.frameworkProvenanceState).toBe('verified');
    expect(component.frameworkSelectionDecision?.selected).toEqual(registryDecision.selected);
    expect(component.frameworkGovernanceLabel).toBe('Governed selection verified');
  });

  it('shows missing provenance honestly and does not query registry history', () => {
    const { component, workflowService } = createComponent();

    component.applyWorkflowRecord(workflowRecord([]));

    expect(component.frameworkProvenanceState).toBe('missing');
    expect(component.frameworkGovernanceLabel).toBe('No framework selection recorded');
    expect(component.frameworkGovernanceSummary).toContain('cannot be confirmed');
    expect(workflowService.frameworkSelection).not.toHaveBeenCalled();
  });

  it('rejects invalid provenance even when the workflow state says completed', () => {
    const { component, workflowService } = createComponent();
    const invalid = {
      ...provenance,
      catalogDigest: 'not-a-digest',
    };

    component.applyWorkflowRecord(workflowRecord([invalid]));

    expect(component.selected?.item.currentState).toBe('completed');
    expect(component.frameworkProvenanceState).toBe('invalid');
    expect(component.frameworkGovernanceLabel).toBe('Framework provenance needs review');
    expect(component.frameworkGovernanceSummary).toContain('Do not treat');
    expect(component.frameworkProvenanceIssues).toContain(
      'Catalog digest is not a SHA-256 digest.'
    );
    expect(workflowService.frameworkSelection).not.toHaveBeenCalled();
  });

  it('navigates from workflow governance to the Framework Registry', () => {
    const { component, router } = createComponent();

    component.openFrameworkRegistry();

    expect(router.navigate).toHaveBeenCalledOnceWith(['/framework-registry']);
  });

  it('records workflow approval through the dedicated approval endpoint', () => {
    const { component, workflowService, notification } = createComponent();
    component.dataLoaded = true;
    const pending = workflowRecord([]);
    pending.item.id = '11111111-1111-4111-8111-111111111111';
    pending.item.currentState = 'needs_approval';
    pending.item.approvalStatus = 'pending';
    const approved = workflowRecord([]);
    approved.item.id = pending.item.id;
    approved.item.currentState = 'ready';
    approved.decisions = [{ id: '22222222-2222-4222-8222-222222222222', workflowId: pending.item.id,
      decisionType: 'approval', decision: 'approved', approved: true, createdAt: '2026-10-02T12:00:00Z' }];
    workflowService.resolveApproval.and.returnValue(of(approved));
    component.applyWorkflowRecord(pending);

    component.resolveApproval(pending.item, true);

    expect(workflowService.resolveApproval).toHaveBeenCalledOnceWith(pending.item.id, {
      approved: true,
      note: 'Robert approved controlled workflow execution.',
      actor: 'operator',
    });
    expect(component.selected?.item.approvalStatus).toBe('approved');
    expect(component.saving).toBeFalse();
    expect(notification.info).toHaveBeenCalledWith('Decision response received', jasmine.any(String));
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('confirms and runs only the selected approved workflow', () => {
    const { component, workflowService, notification, modal } = createComponent();
    component.dataLoaded = true;
    const ready = workflowRecord([]);
    ready.item.id = '11111111-1111-4111-8111-111111111111';
    component.overview = { states: ['ready', 'completed'] } as any;
    ready.item.currentState = 'ready';
    ready.item.approvalStatus = 'approved';
    component.applyWorkflowRecord(ready);
    workflowService.runOne.and.returnValue(of({
      workflowId: ready.item.id,
      status: 'completed',
      state: 'completed',
      attempts: 1,
      verificationStatus: 'verified',
    }));
    const completed = workflowRecord([]);
    completed.item.id = ready.item.id;
    workflowService.get.and.returnValue(of(completed));

    component.runSelectedWorkflow();

    expect(modal.confirm).toHaveBeenCalled();
    expect(workflowService.runOne).not.toHaveBeenCalled();
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    confirmation.nzOnOk();
    expect(workflowService.runOne).toHaveBeenCalledOnceWith(ready.item.id);
    expect(component.lastOperation?.name).toBe('Run selected workflow');
    expect(component.lastOperation?.status).toBe('completed');
    expect(notification.info).toHaveBeenCalledWith('Server reported completion', jasmine.any(String));
    expect(notification.success).not.toHaveBeenCalled();
    confirmation.nzOnOk();
    expect(workflowService.runOne).toHaveBeenCalledTimes(1);
  });

  it('does not dispatch a stale selected-workflow confirmation', () => {
    const { component, workflowService, modal } = createComponent();
    component.dataLoaded = true;
    const ready = workflowRecord([]);
    ready.item.id = '11111111-1111-4111-8111-111111111111';
    ready.item.currentState = 'ready';
    component.applyWorkflowRecord(ready);
    component.runSelectedWorkflow();
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    const newer = workflowRecord([]);
    newer.item.id = '22222222-2222-4222-8222-222222222222';
    newer.item.currentState = 'ready';
    component.applyWorkflowRecord(newer);
    confirmation.nzOnOk();
    expect(workflowService.runOne).not.toHaveBeenCalled();
    expect(component.selected).toBe(newer);
  });

  it('allows a ready workflow with no approval requirement to enter the controlled run confirmation', () => {
    const { component, modal } = createComponent();
    component.dataLoaded = true;
    const ready = workflowRecord([]);
    ready.item.currentState = 'ready';
    ready.item.requiresApproval = false;
    ready.item.approvalStatus = 'not_required';
    component.applyWorkflowRecord(ready);

    component.runSelectedWorkflow();

    expect(modal.confirm).toHaveBeenCalled();
  });

  it('keeps a newly selected safe workflow runnable while the list refreshes', () => {
    const { component, workflowService, modal } = createComponent();
    component.dataLoaded = true;
    component.overview = { states: ['needs_approval', 'ready'] } as any;
    const beforeSelection = workflowRecord([]);
    beforeSelection.item.id = '11111111-1111-4111-8111-111111111111';
    beforeSelection.item.currentState = 'needs_approval';
    const ready = workflowRecord([]);
    ready.item.id = beforeSelection.item.id;
    ready.item.currentState = 'ready';
    ready.item.requiresApproval = false;
    ready.item.approvalStatus = 'not_required';
    beforeSelection.proposals = [{
      id: '22222222-2222-4222-8222-222222222222',
      workflowId: beforeSelection.item.id,
      status: 'open',
      recommendedAction: 'Select an automation for controlled execution',
      options: ['Use E2E readiness probe'],
    }] as any;
    ready.proposals = [{ ...beforeSelection.proposals[0], status: 'approved', selectedOption: 'Use E2E readiness probe' }];
    ready.decisions = [{ id: '33333333-3333-4333-8333-333333333333', workflowId: ready.item.id, decisionType: 'proposal', decision: 'approved', approved: true, createdAt: '2026-10-02T12:00:00Z' }];
    workflowService.resolveProposal.and.returnValue(of(ready));
    component.applyWorkflowRecord(beforeSelection);

    component.resolveProposal('22222222-2222-4222-8222-222222222222', 'approved', 'Use E2E readiness probe');

    expect(component.refresh).toHaveBeenCalledWith(false, true);
    expect(component.anyActionRunning()).toBeFalse();
    component.runSelectedWorkflow();
    expect(modal.confirm).toHaveBeenCalled();
  });

  it('records an explicit operator review before retrying interrupted execution', () => {
    const { component, workflowService, notification } = createComponent();
    component.dataLoaded = true;
    const interrupted = workflowRecord([]);
    interrupted.item.id = '11111111-1111-4111-8111-111111111111';
    interrupted.item.currentState = 'blocked';
    interrupted.item.recoveryStatus = 'needs_review';
    interrupted.item.blockedReason = 'Execution outcome is unknown.';
    const ready = workflowRecord([]);
    ready.item.id = interrupted.item.id;
    ready.item.currentState = 'needs_approval';
    ready.item.approvalStatus = 'pending';
    ready.item.recoveryStatus = 'retry_confirmed';
    ready.decisions = [{ id: '22222222-2222-4222-8222-222222222222', workflowId: interrupted.item.id, decisionType: 'interrupted_execution', decision: 'retry', approved: false, createdAt: '2026-10-02T12:00:00Z' }];
    ready.sourceLinks = [{ id: '33333333-3333-4333-8333-333333333333', workflowId: interrupted.item.id, sourceType: 'recovery_evidence', sourceUri: 'local://recovery/source', relationship: 'execution_reconciliation', createdAt: '2026-10-02T12:00:00Z' }];
    component.applyWorkflowRecord(interrupted);
    component.interruptionForm.setValue({
      decision: 'retry',
      note: 'Checked the target system and confirmed that no external action occurred.',
      evidenceUri: 'local://recovery/source',
      evidenceLabel: '',
      priorExecutionReconciled: true,
    });
    workflowService.resolveInterruptedExecution.and.returnValue(of(ready));

    component.resolveInterruptedExecution();

    expect(workflowService.resolveInterruptedExecution).toHaveBeenCalledOnceWith(
      interrupted.item.id,
      {
        decision: 'retry',
        note: 'Checked the target system and confirmed that no external action occurred.',
        evidenceUri: 'local://recovery/source',
        evidenceLabel: '',
        priorExecutionReconciled: true,
        actor: 'operator',
      }
    );
    expect(component.selected?.item.currentState).toBe('needs_approval');
    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.info).toHaveBeenCalled();
  });

  it('does not confirm an interrupted completion without linked evidence', () => {
    const { component, workflowService, notification } = createComponent();
    component.dataLoaded = true;
    const interrupted = workflowRecord([]);
    interrupted.item.currentState = 'blocked';
    interrupted.item.recoveryStatus = 'needs_review';
    component.applyWorkflowRecord(interrupted);
    component.interruptionForm.setValue({
      decision: 'confirm_completed',
      note: 'The expected external result exists.',
      evidenceUri: '',
      evidenceLabel: '',
      priorExecutionReconciled: false,
    });

    component.resolveInterruptedExecution();

    expect(workflowService.resolveInterruptedExecution).not.toHaveBeenCalled();
    expect(notification.error).toHaveBeenCalledWith(
      'Reconciliation required',
      'Confirm that the prior execution has ended and its outcome was checked, and add a source URI.'
    );
  });
});

describe('WorkflowEngineComponent progressive disclosure', () => {
  let fixture: ComponentFixture<WorkflowEngineComponent>;

  function workflowItem(id: string, currentState: string): IWorkflowItem {
    return {
      id,
      title: 'Review the prepared source bundle',
      currentState,
      taskType: 'administrative',
      riskLevel: 'low',
      priorityScore: 40,
      confidence: 0.9,
      autonomyLevel: 'suggest',
      requiresApproval: false,
      approvalStatus: 'not_required',
      retryCount: 0,
      maxRetries: 1,
      archived: false,
      createdAt: '2026-09-24T10:00:00Z',
      updatedAt: '2026-09-24T10:00:00Z',
    };
  }

  function selectedWorkflowWithProposal(): IWorkflowRecord {
    return {
      item: {
        id: workflowId,
        title: 'Review prepared work',
        currentState: 'needs_approval',
        taskType: 'administrative',
        riskLevel: 'medium',
        priorityScore: 50,
        confidence: 0.9,
        autonomyLevel: 'suggest',
        requiresApproval: true,
        approvalStatus: 'pending',
        retryCount: 0,
        maxRetries: 3,
        archived: false,
        createdAt: '2026-09-24T10:00:00Z',
        updatedAt: '2026-09-24T10:00:00Z',
      },
      checklist: [],
      intake: [],
      matches: [],
      pursuits: [],
      evidence: [],
      openLoops: [],
      proposals: [{
        id: proposalId,
        workflowId,
        status: 'open',
        recommendedAction: 'Review the prepared checklist',
        options: 'Approve this proposal\nRequest changes',
        createdAt: '2026-09-24T10:00:00Z',
        updatedAt: '2026-09-24T10:00:00Z',
      }],
      qualityGates: [],
      transitions: [],
      sourceLinks: [],
      decisions: [],
      events: [],
      frameworkSelections: [],
    };
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [WorkflowEngineComponent],
      imports: [CommonModule, FormsModule, ReactiveFormsModule, RouterTestingModule, NzSelectModule, ControlRoomModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        {
          provide: WORKFLOW_SERVICE_TOKEN,
          useValue: jasmine.createSpyObj('workflowService', [
            'overview', 'dashboard', 'reminderProposals', 'reminderActivationHistory',
            'reminderDeliveryHistory', 'items', 'approvals', 'get',
            'updateChecklistItem',
          ]),
        },
        { provide: PursuitService, useValue: {} },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('notification', ['success', 'info', 'warning', 'error']) },
        { provide: NzModalService, useValue: jasmine.createSpyObj('modal', ['confirm', 'info', 'warning', 'error']) },
        { provide: ActivatedRoute, useValue: { snapshot: { queryParamMap: convertToParamMap({}) } } },
      ],
    }).compileComponents();
    TestBed.inject(ModuleViewPreferencesService).reset('workflow-engine');
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    fixture.componentInstance.reminderProposalsUnavailable = true;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    TestBed.inject(ModuleViewPreferencesService).reset('workflow-engine');
  });

  it('keeps the worker action visible but disabled until the first queue load succeeds', () => {
    const page: HTMLElement = fixture.nativeElement;
    const runWorkerButton = page.querySelector('[data-testid="workflow-run-worker"]') as HTMLButtonElement;

    expect(runWorkerButton).not.toBeNull();
    expect(runWorkerButton.disabled).toBeTrue();
  });

  it('keeps a semantic page title and refresh control in the Basic view', () => {
    const page: HTMLElement = fixture.nativeElement;
    const heading = page.querySelector('.workflow-header h1');
    expect(heading?.textContent).toBe('Workflows');
    expect(page.querySelector('.workflow-shell')?.getAttribute('aria-labelledby')).toBe(heading?.id);
    expect(page.querySelector('.workflow-header [data-testid="workflow-refresh"]')).not.toBeNull();
  });

  it('keeps the worker disabled when a present ready queue is empty despite a stale positive count', () => {
    const component = fixture.componentInstance;
    component.dashboard = {
      counts: { ready: 2 },
      approvalItems: [],
      blockedItems: [],
      readyItems: [],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    component.dataLoaded = true;
    fixture.detectChanges();

    const runWorkerButton = fixture.nativeElement.querySelector('[data-testid="workflow-run-worker"]') as HTMLButtonElement;
    expect(component.queueCount('ready')).toBe(0);
    expect(runWorkerButton.disabled).toBeTrue();
    expect(runWorkerButton.textContent).toContain('0 ready');
  });

  it('renders retryable load failures and preserves a clear refresh-in-progress state', () => {
    const workflowService = TestBed.inject(WORKFLOW_SERVICE_TOKEN) as jasmine.SpyObj<any>;
    const notification = TestBed.inject(NzNotificationService) as any;
    notification.error = jasmine.createSpy('error');
    workflowService.overview.and.returnValue(of(validOverview()));
    workflowService.dashboard.and.returnValues(
      throwError(() => new Error('Workflow API unavailable')),
      of({
        counts: {},
        approvalItems: [],
        blockedItems: [],
        readyItems: [],
        highRiskItems: [],
        itemsWithoutNextAction: [],
        dueOpenLoops: [],
        rules: [],
      }),
    );
    workflowService.reminderProposals.and.returnValue(throwError(() => new Error('Reminder API unavailable')));
    workflowService.reminderActivationHistory.and.returnValue(throwError(() => new Error('Reminder history unavailable')));
    workflowService.reminderDeliveryHistory.and.returnValue(throwError(() => new Error('Delivery history unavailable')));
    workflowService.items.and.returnValue(of([]));
    workflowService.approvals.and.returnValue(of([]));

    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    const component = fixture.componentInstance;
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const error = page.querySelector('[data-testid="workflow-load-error"]');
    expect(error?.getAttribute('role')).toBe('alert');
    expect(component.loadFailed).toBeTrue();
    expect(component.dataLoaded).toBeFalse();
    const retry = page.querySelector('[data-testid="workflow-retry-load"]') as HTMLButtonElement;
    expect(retry.type).toBe('button');
    retry.focus();
    expect(document.activeElement).toBe(retry);
    retry.click();
    fixture.detectChanges();
    expect(workflowService.dashboard).toHaveBeenCalledTimes(2);
    expect(component.loadFailed).toBeFalse();
    expect(component.dataLoaded).toBeTrue();
    expect(component.loading).toBeFalse();
    expect(page.querySelector('[data-testid="workflow-load-error"]')).toBeNull();

    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const refreshing = fixture.componentInstance;
    const item = workflowItem('99999999-9999-4999-8999-999999999999', 'ready');
    refreshing.items = [item];
    refreshing.dashboard = {
      counts: {},
      approvalItems: [],
      blockedItems: [],
      readyItems: [item],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    refreshing.dataLoaded = true;
    refreshing.loading = true;
    fixture.detectChanges();

    const refreshedPage: HTMLElement = fixture.nativeElement;
    expect(refreshedPage.querySelector('[data-testid="workflow-refreshing"]')?.getAttribute('role')).toBe('status');
    expect(refreshedPage.querySelector('.inbox-row')?.textContent).toContain(item.title);
  });

  it('keeps intake, worker action, and reminder failure visible while persisting advanced disclosures', () => {
    const workflowService = TestBed.inject(WORKFLOW_SERVICE_TOKEN) as jasmine.SpyObj<any>;
    const ready: IWorkflowItem = {
      id: '55555555-5555-4555-8555-555555555555',
      title: 'Run the approved safe task',
      currentState: 'ready',
      taskType: 'administrative',
      riskLevel: 'low',
      priorityScore: 10,
      confidence: 1,
      autonomyLevel: 'suggest',
      requiresApproval: false,
      approvalStatus: 'not_required',
      retryCount: 0,
      maxRetries: 1,
      archived: false,
      createdAt: '2026-09-24T10:00:00Z',
      updatedAt: '2026-09-24T10:00:00Z',
    };
    workflowService.overview.and.returnValue(of(validOverview()));
    workflowService.dashboard.and.returnValue(of({
      counts: {},
      approvalItems: [],
      blockedItems: [],
      readyItems: [ready],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    }));
    workflowService.reminderProposals.and.returnValue(throwError(() => new Error('reminder unavailable')));
    workflowService.reminderActivationHistory.and.returnValue(of({
      items: [], authority: 'reminder_activation_history_only', canExecute: false, checkedAt: '2026-09-24T10:00:00Z',
    }));
    workflowService.reminderDeliveryHistory.and.returnValue(of({
      authorizations: [], attempts: [], authority: 'internal_reminder_delivery_receipt', canExecute: false,
    }));
    workflowService.items.and.returnValue(of([ready]));
    workflowService.approvals.and.returnValue(of([]));
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(fixture.componentInstance.dataLoaded).toBeTrue();
    expect(fixture.componentInstance.hasWorkflowAttention()).toBeTrue();
    expect(fixture.componentInstance.queueCount('ready')).toBe(1);
    expect(page.querySelector('.action-strip')).not.toBeNull();
    expect(page.querySelector('#workflow-intake-input')).not.toBeNull();
    expect(page.querySelector('[data-testid="workflow-create"]')).not.toBeNull();
    const runWorker = spyOn(fixture.componentInstance, 'runDue');
    const runWorkerButton = page.querySelector('[data-testid="workflow-run-worker"]') as HTMLButtonElement;
    expect(runWorkerButton).not.toBeNull();
    expect(runWorkerButton.disabled).toBeFalse();
    expect(runWorkerButton.textContent).toContain('1 ready');
    runWorkerButton.click();
    expect(runWorker).toHaveBeenCalled();
    expect(page.querySelector('.reminder-unavailable')?.textContent).toContain('unavailable');

    const preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.setMode('workflow-engine', 'advanced');
    fixture.detectChanges();
    const section = Array.from(page.querySelectorAll('hai-progressive-section'))
      .find((node) => node.getAttribute('sectionid') === 'intake-provenance') as HTMLElement;
    expect(section).toBeTruthy();
    expect(section.querySelector('.hai-progressive-section--advanced')).not.toBeNull();
    expect(section.querySelector('.hai-progressive-section__content')).toBeNull();

    (section.querySelector('button') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(section.querySelector('.hai-progressive-section__content')).not.toBeNull();
    expect(TestBed.inject(ModuleViewPreferencesService).get('workflow-engine').openSections['intake-provenance']).toBeTrue();
  });

  it('persists the shared toolbar disclosure and keeps all existing actions connected', () => {
    const component = fixture.componentInstance;
    component.dataLoaded = true;
    component.dashboard = {
      counts: { dueOpenLoops: 1 }, approvalItems: [], blockedItems: [], readyItems: [],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [{ id: 'due-loop' } as any], rules: [],
    };
    TestBed.inject(ModuleViewPreferencesService).setMode('workflow-engine', 'advanced');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    const section = page.querySelector('hai-progressive-section[sectionid="workflow-header-controls"]') as HTMLElement;
    const trigger = section.querySelector('.hai-progressive-section__summary') as HTMLButtonElement;

    expect(section.getAttribute('moduleid')).toBe('workflow-engine');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(section.querySelector('.hai-progressive-section__content')).toBeNull();

    const followups = spyOn(fixture.componentInstance, 'runDueOpenLoops');
    const recovery = spyOn(fixture.componentInstance, 'recoverStaleClaims');
    const automations = spyOn(fixture.componentInstance, 'goHome');
    trigger.click();
    fixture.detectChanges();

    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(section.querySelector('.hai-progressive-section__content')).not.toBeNull();
    expect(TestBed.inject(ModuleViewPreferencesService).get('workflow-engine').openSections['workflow-header-controls']).toBeTrue();

    (section.querySelector('[data-testid="workflow-run-followups"]') as HTMLButtonElement).click();
    (section.querySelector('[data-testid="workflow-recover-stale"]') as HTMLButtonElement).click();
    (section.querySelector('[data-testid="workflow-open-automations"]') as HTMLButtonElement).click();

    expect(followups).toHaveBeenCalledTimes(1);
    expect(recovery).toHaveBeenCalledTimes(1);
    expect(automations).toHaveBeenCalledTimes(1);

    trigger.click();
    fixture.detectChanges();
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(section.querySelector('.hai-progressive-section__content')).toBeNull();
    expect(TestBed.inject(ModuleViewPreferencesService).get('workflow-engine').openSections['workflow-header-controls']).toBeFalse();
  });

  it('exposes the selected workflow queue filter with aria-pressed and filters records', () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    fixture.componentInstance.reminderProposalsUnavailable = true;
    const workflow = (id: string, currentState: string): IWorkflowItem => ({
      id,
      title: id,
      currentState,
      taskType: 'administrative',
      riskLevel: 'low',
      priorityScore: 10,
      confidence: 1,
      autonomyLevel: 'suggest',
      requiresApproval: false,
      approvalStatus: 'not_required',
      retryCount: 0,
      maxRetries: 1,
      archived: false,
      createdAt: '2026-09-24T10:00:00Z',
      updatedAt: '2026-09-24T10:00:00Z',
    });
    const ready = workflow(workflowId, 'ready');
    const blocked = workflow('66666666-6666-4666-8666-666666666666', 'blocked');
    const dashboard: IWorkflowDashboard = {
      counts: {},
      approvalItems: [],
      blockedItems: [blocked],
      readyItems: [ready],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    fixture.componentInstance.items = [ready, blocked];
    fixture.componentInstance.dashboard = dashboard;
    fixture.detectChanges();
    expect(fixture.componentInstance.hasWorkflowAttention()).toBeTrue();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('.queue-rail')).not.toBeNull();
    const all = Array.from(page.querySelectorAll<HTMLButtonElement>('.queue-row'))
      .find((button) => button.textContent?.includes('All workflows'))!;
    const blockedFilter = Array.from(page.querySelectorAll<HTMLButtonElement>('.queue-row'))
      .find((button) => button.textContent?.includes('Blocked'))!;

    expect(all.getAttribute('aria-pressed')).toBe('true');
    expect(blockedFilter.getAttribute('aria-pressed')).toBe('false');
    blockedFilter.click();
    fixture.detectChanges();

    expect(blockedFilter.getAttribute('aria-pressed')).toBe('true');
    expect(all.getAttribute('aria-pressed')).toBe('false');
    expect(fixture.componentInstance.filteredItems().map((item) => item.id)).toEqual(['66666666-6666-4666-8666-666666666666']);
  });

  it('provides keyboard-operable workflow rows with field labels for narrow layouts', () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const component = fixture.componentInstance;
    const ready = workflowItem(workflowId, 'ready');
    component.items = [ready];
    component.dashboard = {
      counts: {},
      approvalItems: [],
      blockedItems: [],
      readyItems: [ready],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    component.dataLoaded = true;
    const workflowService = TestBed.inject(WORKFLOW_SERVICE_TOKEN) as jasmine.SpyObj<any>;
    const record = selectedWorkflowWithProposal();
    record.item.id = ready.id;
    workflowService.get.and.returnValue(of(record));
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const row = page.querySelector('.inbox-row') as HTMLButtonElement;
    expect(row.tagName).toBe('BUTTON');
    expect(row.type).toBe('button');
    for (const label of ['Priority', 'Workflow', 'Status', 'Risk', 'Updated']) {
      expect(Array.from(row.querySelectorAll('.inbox-row__field-label')).some((node) => node.textContent?.trim() === label)).toBeTrue();
    }
    row.focus();
    expect(document.activeElement).toBe(row);
    row.click();
    expect(workflowService.get).toHaveBeenCalledOnceWith(ready.id);
    expect(component.selected).toBe(record);
  });

  it('offers a direct reset when filters hide all workflow records', () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const component = fixture.componentInstance;
    const ready = workflowItem('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'ready');
    component.items = [ready];
    component.dashboard = {
      counts: {},
      approvalItems: [],
      blockedItems: [],
      readyItems: [ready],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    };
    component.dataLoaded = true;
    component.workflowSearch = 'not present';
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('.workflow-empty')?.textContent).toContain('No workflows match this view');
    const clear = page.querySelector('[data-testid="workflow-clear-filters"]') as HTMLButtonElement;
    expect(clear).not.toBeNull();
    expect(clear.type).toBe('button');
    clear.click();
    fixture.detectChanges();

    expect(component.workflowSearch).toBe('');
    expect(page.querySelector('.inbox-row')?.textContent).toContain(ready.title);
  });

  it('shows and focuses the Advanced filters when the Basic-view shortcut is used', async () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const component = fixture.componentInstance;
    const item = workflowItem('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'ready');
    component.items = [item];
    component.dashboard = {
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [item],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    };
    component.dataLoaded = true;
    fixture.detectChanges();

    const preferences = TestBed.inject(ModuleViewPreferencesService);
    expect(preferences.get('workflow-engine').mode).toBe('basic');
    fixture.detectChanges();
    const focus = fixture.nativeElement.querySelector('[aria-label="Show and focus workflow filters"]') as HTMLButtonElement;
    expect(focus).not.toBeNull();
    focus.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(preferences.get('workflow-engine').mode).toBe('advanced');
    expect(preferences.get('workflow-engine').openSections['queue-filters']).toBeTrue();
    const stateFilter = fixture.nativeElement.querySelector('#workflow-state-filter') as HTMLSelectElement;
    expect(stateFilter).not.toBeNull();
    expect(document.activeElement).toBe(stateFilter);
  });

  it('keeps the search disclosure available when its query has no matching rows', () => {
    const component = fixture.componentInstance;
    const item = workflowItem('cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'ready');
    component.items = [item];
    component.dashboard = {
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [item],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    };
    component.dataLoaded = true;
    component.workflowSearch = 'not in this title';
    const preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.setMode('workflow-engine', 'advanced');
    preferences.setSection('workflow-engine', 'inbox-search', true);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('input[aria-label="Search workflows"]')).not.toBeNull();
    expect(page.querySelector('.inbox-panel')).toBeNull();
    expect(page.querySelector('.workflow-empty')?.textContent).toContain('No workflows match this view');
    expect(page.querySelector('[data-testid="workflow-clear-filters"]')).not.toBeNull();
  });

  it('shows workflow-opening progress and a retryable inline error', () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const component = fixture.componentInstance;
    const item = workflowItem('77777777-7777-4777-8777-777777777777', 'ready');
    component.items = [item];
    component.dashboard = {
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [item],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    };
    component.dataLoaded = true;
    const workflowService = TestBed.inject(WORKFLOW_SERVICE_TOKEN) as jasmine.SpyObj<any>;
    (TestBed.inject(NzNotificationService) as any).error = jasmine.createSpy('error');
    const response = new Subject<IWorkflowRecord>();
    workflowService.get.and.returnValue(response);
    fixture.detectChanges();

    const row = fixture.nativeElement.querySelector('.inbox-row') as HTMLButtonElement;
    row.click();
    fixture.detectChanges();
    expect(component.openingWorkflowId).toBe(item.id);
    expect(row.getAttribute('aria-busy')).toBe('true');
    expect(row.textContent).toContain('Opening');

    response.error(new Error('offline'));
    fixture.detectChanges();
    expect(component.openingWorkflowId).toBeUndefined();
    expect(fixture.nativeElement.querySelector('[data-testid="workflow-open-error"]')?.textContent).toContain('select the row to retry');
    expect(row.disabled).toBeFalse();
  });

  it('tracks checklist writes through pending, failure, and confirmed completion states', () => {
    fixture.destroy();
    fixture = TestBed.createComponent(WorkflowEngineComponent);
    spyOn(fixture.componentInstance, 'refresh');
    const component = fixture.componentInstance;
    const record = selectedWorkflowWithProposal();
    record.checklist = [{
      id: checklistItemId, workflowId: record.item.id, label: 'Verify source', status: 'open', position: 0,
      requiresApproval: false, createdAt: '2026-09-24T10:00:00Z', updatedAt: '2026-09-24T10:00:00Z',
    }];
    component.dataLoaded = true;
    const preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.setMode('workflow-engine', 'advanced');
    preferences.setSection('workflow-engine', 'checklist', true);
    component.applyWorkflowRecord(record);
    fixture.componentInstance.reminderProposalsUnavailable = true;
    const workflowService = TestBed.inject(WORKFLOW_SERVICE_TOKEN) as jasmine.SpyObj<any>;
    const notification = TestBed.inject(NzNotificationService) as any;
    notification.error = jasmine.createSpy('error');
    const pending = new Subject<IWorkflowRecord>();
    workflowService.updateChecklistItem.and.returnValue(pending);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const done = page.querySelector(`[data-testid="workflow-checklist-done-${checklistItemId}"]`) as HTMLButtonElement;
    done.click();
    fixture.detectChanges();
    expect(component.checklistAction).toEqual({ itemId: checklistItemId, status: 'done' });
    expect(done.disabled).toBeTrue();

    pending.error(new Error('write failed'));
    fixture.detectChanges();
    expect(component.checklistAction).toBeUndefined();
    expect(page.querySelector('.checklist-error')?.getAttribute('role')).toBe('alert');
    expect(page.querySelector('.checklist-error')?.textContent).toContain('Update failed');
    expect(done.disabled).toBeFalse();

    const confirmed: IWorkflowRecord = { ...record, checklist: [{ ...record.checklist[0], status: 'done' }] };
    workflowService.updateChecklistItem.and.returnValue(of(confirmed));
    done.click();
    fixture.detectChanges();
    expect(component.selected?.checklist[0].status).toBe('done');
    const confirmedButton = page.querySelector(`[data-testid="workflow-checklist-done-${checklistItemId}"]`) as HTMLButtonElement;
    expect(confirmedButton.disabled).toBeTrue();
    expect(confirmedButton.textContent).toContain('Done');
  });

  it('blocks consequential controls when the last refresh failed and validates intake length', () => {
    const component = fixture.componentInstance;
    const item = workflowItem('88888888-8888-4888-8888-888888888888', 'ready');
    component.items = [item];
    component.dashboard = {
      counts: {}, approvalItems: [], blockedItems: [], readyItems: [item],
      highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [],
    };
    component.dataLoaded = true;
    component.loadFailed = true;
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect((page.querySelector('[data-testid="workflow-create"]') as HTMLButtonElement).disabled).toBeTrue();
    expect((page.querySelector('[data-testid="workflow-run-worker"]') as HTMLButtonElement).disabled).toBeTrue();

    component.loadFailed = false;
    component.intakeForm.get('input')?.setValue('x'.repeat(4001));
    fixture.detectChanges();
    const intake = page.querySelector('#workflow-intake-input') as HTMLTextAreaElement;
    expect(intake.maxLength).toBe(4000);
    expect(component.intakeForm.invalid).toBeTrue();
    expect((page.querySelector('[data-testid="workflow-create"]') as HTMLButtonElement).disabled).toBeTrue();
  });

  it('keeps open proposal decisions in the Basic inspector and routes them through the existing handler', () => {
    const record = selectedWorkflowWithProposal();
    expect(fixture.componentInstance.hasPendingReviewProposals(record)).toBeTrue();
    const resolve = spyOn(fixture.componentInstance, 'resolveProposal');
    fixture.componentInstance.dataLoaded = true;
    fixture.componentInstance.applyWorkflowRecord(record);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('aside.detail-rail')).not.toBeNull();
    expect(page.querySelector('[data-testid="workflow-pending-proposals"]')).not.toBeNull();
    const approval = fixture.nativeElement.querySelector('[data-testid="workflow-pending-proposals"] button') as HTMLButtonElement;
    expect(approval).not.toBeNull();
    expect(approval.textContent).toContain('Approve');
    approval.click();

    expect(resolve).toHaveBeenCalledWith(proposalId, 'approved', 'Approve this proposal');
    expect(TestBed.inject(ModuleViewPreferencesService).get('workflow-engine').mode).toBe('basic');
  });
});

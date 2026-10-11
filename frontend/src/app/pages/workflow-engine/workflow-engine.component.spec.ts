import { FormBuilder } from '@angular/forms';
import { convertToParamMap } from '@angular/router';
import { of, throwError } from 'rxjs';
import {
  IWorkflowFrameworkSelectionDecision,
  IWorkflowFrameworkSelectionProvenance,
  IWorkflowRecord,
} from '../../models/workflow.model.interface';
import { WorkflowEngineComponent } from './workflow-engine.component';

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
        id: 'workflow-1',
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

  function createComponent(queryParams: Record<string, string> = {}): {
    component: WorkflowEngineComponent;
    workflowService: jasmine.SpyObj<any>;
    pursuitService: jasmine.SpyObj<any>;
    notification: jasmine.SpyObj<any>;
    modal: jasmine.SpyObj<any>;
    router: jasmine.SpyObj<any>;
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
    const route = { snapshot: { queryParamMap: convertToParamMap(queryParams) } } as any;
    const router = jasmine.createSpyObj('router', ['navigate']);
    const component = new WorkflowEngineComponent(new FormBuilder(), workflowService, pursuitService, notification, modal, route, router);
    spyOn(component, 'refresh');
    return { component, workflowService, pursuitService, notification, modal, router };
  }

  it('starts manual intake without executable demo provenance', () => {
    const { component } = createComponent();

    expect(component.intakeForm.value).toEqual({
      input: '',
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

  it('opens a validated deep-linked workflow queue', () => {
    const { component } = createComponent({ queue: 'approval' });

    component.ngOnInit();

    expect(component.activeQueue).toBe('approval');
    expect(component.refresh).toHaveBeenCalled();
  });

  it('ignores an invalid deep-linked workflow queue', () => {
    const { component } = createComponent({ queue: 'execute_everything' });

    component.ngOnInit();

    expect(component.activeQueue).toBe('all');
  });

  it('accepts only current non-executing reminder snapshots with exact unique items', () => {
    const { component } = createComponent();
    const snapshot: any = {
      items: [{
        id: 'reminder-1',
        workflowId: 'workflow-1',
        checklistItemId: 'reminder-1',
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
    const proposal: any = {
      id: 'reminder-1',
      workflowId: 'workflow-1',
      checklistItemId: 'reminder-1',
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
    workflowService.prepareReminderActivation.and.returnValue(of({
      request: {
        id: 'activation-1',
        checklistItemId: proposal.checklistItemId,
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
    expect(notification.success).toHaveBeenCalledWith(
      'Internal reminder prepared',
      'Nothing was sent and no calendar event was created. Owner approval remains separate.'
    );
    expect(component.refresh).toHaveBeenCalledWith(false, true);
  });

  it('does not turn approval-dialog cancellation into rejection', () => {
    const { component, workflowService, modal } = createComponent();
    const proposal: any = { checklistItemId: 'reminder-1' };
    component.reminderActivationHistory = {
      items: [{
        request: { id: 'activation-1', checklistItemId: 'reminder-1' },
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

  it('keeps workflow data usable when reminder proposals fail independently', () => {
    const { component, workflowService, notification } = createComponent();
    workflowService.overview.and.returnValue(of({ capabilities: [], states: [], safetyRules: [], rules: [] }));
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

  it('authorizes exactly one internal reminder from a current approved decision', () => {
    const { component, workflowService, modal } = createComponent();
    const proposal: any = { checklistItemId: 'reminder-1' };
    component.reminderActivationHistory = {
      items: [{
        request: {
          id: 'activation-1', checklistItemId: 'reminder-1', recordDigest: digest,
          reminderDigest: 'b'.repeat(64),
        },
        latestDecision: {
          id: 'decision-1', recordDigest: 'c'.repeat(64),
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
        id: 'authorization-1', activationRequestId: 'activation-1', activationDecisionId: 'decision-1',
        channel: 'in_app', recordDigest: 'd'.repeat(64),
      },
      replayed: false, authority: 'internal_reminder_delivery_authorization', deliveryAuthorized: true, canExecute: false,
    }));
    const event = { stopPropagation: jasmine.createSpy('stopPropagation') } as unknown as Event;

    component.authorizeReminderDelivery(proposal, event);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(workflowService.authorizeReminderDelivery).toHaveBeenCalledWith('activation-1', jasmine.objectContaining({
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
      id: 'reminder-1',
      workflowId: 'workflow-1',
      checklistItemId: 'reminder-1',
      title: 'Review evidence',
    };

    component.openReminder(proposal);

    expect(component.selectedReminder).toBe(proposal);
    expect(component.selected).toBeUndefined();
    expect(workflowService.get).not.toHaveBeenCalled();
  });

  it('records rejection only through the explicit reject confirmation', () => {
    const { component, workflowService, modal } = createComponent();
    const proposal: any = { checklistItemId: 'reminder-1' };
    component.reminderActivationHistory = {
      items: [{
        request: {
          id: 'activation-1',
          checklistItemId: 'reminder-1',
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
        activationRequestId: 'activation-1',
        activationRequestDigest: digest,
        decision: 'rejected',
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

    expect(workflowService.decideReminderActivation).toHaveBeenCalledWith('activation-1', {
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
      id: 'proposal-1',
      workflowId: 'workflow-1',
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
    component.intakeForm.patchValue({
      input: 'Prepare a source-grounded operational brief.',
    });
    pursuitService.routeIntake.and.returnValue(of({
      mode: 'candidate_created',
      createdCandidate: true,
      pursuitId: 'candidate-1',
      matches: [],
    }));

    component.intake();

    expect(notification.info).toHaveBeenCalledWith(
      'Pursuit candidate needs review',
      'HAI recorded the unmatched input as a reviewable pursuit candidate. No workflow was created until an approver accepts it.'
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
    const pending = workflowRecord([]);
    pending.item.currentState = 'needs_approval';
    pending.item.approvalStatus = 'pending';
    const approved = workflowRecord([]);
    workflowService.resolveApproval.and.returnValue(of(approved));
    component.applyWorkflowRecord(pending);

    component.resolveApproval(pending.item, true);

    expect(workflowService.resolveApproval).toHaveBeenCalledOnceWith('workflow-1', {
      approved: true,
      note: 'Robert approved controlled workflow execution.',
      actor: 'operator',
    });
    expect(component.selected?.item.approvalStatus).toBe('approved');
    expect(component.saving).toBeFalse();
    expect(notification.success).toHaveBeenCalled();
  });

  it('confirms and runs only the selected approved workflow', () => {
    const { component, workflowService, notification, modal } = createComponent();
    const ready = workflowRecord([]);
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
    workflowService.get.and.returnValue(of(ready));

    component.runSelectedWorkflow();

    expect(modal.confirm).toHaveBeenCalled();
    expect(workflowService.runOne).not.toHaveBeenCalled();
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    confirmation.nzOnOk();
    expect(workflowService.runOne).toHaveBeenCalledOnceWith('workflow-1');
    expect(component.lastOperation?.name).toBe('Run selected workflow');
    expect(component.lastOperation?.status).toBe('completed');
    expect(notification.success).toHaveBeenCalled();
  });

  it('records an explicit operator review before retrying interrupted execution', () => {
    const { component, workflowService, notification } = createComponent();
    const interrupted = workflowRecord([]);
    interrupted.item.currentState = 'blocked';
    interrupted.item.recoveryStatus = 'needs_review';
    interrupted.item.blockedReason = 'Execution outcome is unknown.';
    const ready = workflowRecord([]);
    ready.item.currentState = 'ready';
    ready.item.recoveryStatus = 'retry_confirmed';
    component.applyWorkflowRecord(interrupted);
    component.interruptionForm.setValue({
      decision: 'retry',
      note: 'Checked the target system and confirmed that no external action occurred.',
      evidenceUri: '',
      evidenceLabel: '',
    });
    workflowService.resolveInterruptedExecution.and.returnValue(of(ready));

    component.resolveInterruptedExecution();

    expect(workflowService.resolveInterruptedExecution).toHaveBeenCalledOnceWith(
      interrupted.item.id,
      {
        decision: 'retry',
        note: 'Checked the target system and confirmed that no external action occurred.',
        evidenceUri: '',
        evidenceLabel: '',
        actor: 'operator',
      }
    );
    expect(component.selected?.item.currentState).toBe('ready');
    expect(notification.success).toHaveBeenCalled();
  });

  it('does not confirm an interrupted completion without linked evidence', () => {
    const { component, workflowService, notification } = createComponent();
    const interrupted = workflowRecord([]);
    interrupted.item.currentState = 'blocked';
    interrupted.item.recoveryStatus = 'needs_review';
    component.applyWorkflowRecord(interrupted);
    component.interruptionForm.setValue({
      decision: 'confirm_completed',
      note: 'The expected external result exists.',
      evidenceUri: '',
      evidenceLabel: '',
    });

    component.resolveInterruptedExecution();

    expect(workflowService.resolveInterruptedExecution).not.toHaveBeenCalled();
    expect(notification.error).toHaveBeenCalledWith(
      'Evidence required',
      'Add a source URI before confirming completion.'
    );
  });
});

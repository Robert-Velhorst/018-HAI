import { FormBuilder } from '@angular/forms';
import { Router } from '@angular/router';
import { of, throwError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { IPursuitDashboardDecision } from '../../models/pursuit.model.interface';
import { CommandDashboardComponent } from './command-dashboard.component';

describe('CommandDashboardComponent pursuit candidate decisions', () => {
  function createComponent(): {
    component: CommandDashboardComponent;
    pursuits: jasmine.SpyObj<any>;
    workflows: jasmine.SpyObj<any>;
    commands: jasmine.SpyObj<any>;
    notification: jasmine.SpyObj<any>;
  } {
    const pursuits = jasmine.createSpyObj('PursuitService', ['acceptCandidate', 'archive', 'resolveDecision']);
    const workflows = jasmine.createSpyObj('WorkflowService', ['resolveApproval', 'resolveProposal']);
    const commands = jasmine.createSpyObj('AssistantCommandService', ['logs']);
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error']);
    const router = jasmine.createSpyObj<Router>('Router', ['navigate']);
    const component = new CommandDashboardComponent(
      new FormBuilder(),
      {} as any,
      {} as any,
      commands,
      pursuits,
      workflows,
      notification as NzNotificationService,
      router,
    );
    spyOn(component, 'refreshPursuits');
    return { component, pursuits, workflows, commands, notification };
  }

  function candidateDecision(riskLevel: string = 'medium'): IPursuitDashboardDecision {
    return {
      pursuit: { id: 'candidate-1', title: 'Imported legal correspondence' } as any,
      decision: {
        id: 'pursuit:candidate-1:candidate-review',
        decisionType: 'pursuit_candidate_review',
        status: 'pending',
        riskLevel,
        reason: 'Confirm this imported objective before planning work.',
        recommended: 'Accept the candidate',
        yesLabel: 'Accept',
        noLabel: 'Archive',
      } as any,
    } as IPursuitDashboardDecision;
  }

  it('accepts a candidate through the explicit approval endpoint', () => {
    const { component, pursuits, notification } = createComponent();
    const card = candidateDecision();
    pursuits.acceptCandidate.and.returnValue(of({}));

    component.resolveDashboardDecision(card, true);

    expect(component.canResolveDashboardDecision(card)).toBeTrue();
    expect(pursuits.acceptCandidate).toHaveBeenCalledWith('candidate-1', {
      requiresReview: false,
      reviewReason: 'Confirm this imported objective before planning work.',
    });
    expect(notification.success).toHaveBeenCalledWith('Candidate accepted', 'HAI converted the candidate into governed pursuit work.');
    expect(component.refreshPursuits).toHaveBeenCalled();
  });

  it('archives a rejected candidate without creating workflow work', () => {
    const { component, pursuits, notification } = createComponent();
    const card = candidateDecision('high');
    pursuits.archive.and.returnValue(of({}));

    component.resolveDashboardDecision(card, false);

    expect(pursuits.archive).toHaveBeenCalledWith('candidate-1', true);
    expect(pursuits.acceptCandidate).not.toHaveBeenCalled();
    expect(notification.success).toHaveBeenCalledWith('Candidate archived', 'The auto-created candidate was removed from active queues.');
    expect(component.refreshPursuits).toHaveBeenCalled();
  });

  it('resolves a workflow approval from the unified queue', () => {
    const { component, workflows, notification } = createComponent();
    const card = {
      pursuit: { id: 'pursuit-1', title: 'Prepare legal response' },
      decision: {
        id: 'workflow:workflow-1:approval',
        workflowId: 'workflow-1',
        decisionType: 'approval',
        status: 'pending',
        riskLevel: 'high',
        reason: 'External legal communication needs approval.',
        recommended: 'Approve the prepared workflow',
        yesConsequence: 'The workflow may move forward.',
        noConsequence: 'The workflow remains blocked.',
      },
    } as IPursuitDashboardDecision;
    workflows.resolveApproval.and.returnValue(of({}));

    component.resolveDashboardDecision(card, true);

    expect(component.canResolveDashboardDecision(card)).toBeTrue();
    expect(workflows.resolveApproval).toHaveBeenCalledWith('workflow-1', {
      approved: true,
      note: 'The workflow may move forward.',
      actor: 'Robert',
    });
    expect(notification.success).toHaveBeenCalledWith('Approval recorded', 'Workflow approved through the audited gate.');
  });

  it('resolves a verified completion review from the unified queue', () => {
    const { component, pursuits, notification } = createComponent();
    const card = {
      pursuit: { id: 'pursuit-1', title: 'Complete evidence bundle' },
      decision: {
        id: 'pursuit:pursuit-1:completion-review',
        decisionType: 'pursuit_completion_review',
        status: 'pending',
        riskLevel: 'medium',
        reason: 'Linked workflows are completed with accepted evidence.',
        recommended: 'Mark the pursuit complete',
        yesConsequence: 'Completion is recorded.',
        noConsequence: 'Keep it active.',
      },
    } as IPursuitDashboardDecision;
    pursuits.resolveDecision.and.returnValue(of({}));

    component.resolveDashboardDecision(card, true);

    expect(pursuits.resolveDecision).toHaveBeenCalledWith('pursuit-1', {
      decisionId: 'pursuit:pursuit-1:completion-review',
      decisionType: 'pursuit_completion_review',
      approved: true,
      reason: 'Linked workflows are completed with accepted evidence.',
      note: 'Completion is recorded.',
      evidenceUri: undefined,
      evidenceLabel: undefined,
      actor: 'Robert',
    });
    expect(notification.success).toHaveBeenCalledWith('Pursuit completed', 'Verified completion and the Robert decision were recorded in the audit trail.');
  });

  it('retains confirmed command history when the next ledger read is unavailable', () => {
    const { component, commands } = createComponent();
    const history = [{ id: 'command-1', summary: 'Prepared safe next action.' }];
    component.commandLogs = history as any;
    commands.logs.and.returnValue(throwError(() => new Error('command ledger unavailable')));

    component.loadCommandLogs();

    expect(component.commandLogs).toEqual(history as any);
    expect((component as any).commandLogsUnavailable).toBeTrue();
  });
});

import { FormBuilder } from '@angular/forms';
import { Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { ReactiveFormsModule } from '@angular/forms';
import { RouterTestingModule } from '@angular/router/testing';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { AgentRuntimeService } from '../../services/agent-runtime.service';
import { AssistantCommandService } from '../../services/assistant-command.service';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { PursuitService } from '../../services/pursuit.service';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { IPursuitDashboardDecision } from '../../models/pursuit.model.interface';
import { CommandDashboardComponent } from './command-dashboard.component';

describe('CommandDashboardComponent pursuit candidate decisions', () => {
  function createComponent(): {
    component: CommandDashboardComponent;
    memoryEngine: jasmine.SpyObj<any>;
    agentRuntimes: jasmine.SpyObj<any>;
    pursuits: jasmine.SpyObj<any>;
    workflows: jasmine.SpyObj<any>;
    commands: jasmine.SpyObj<any>;
    notification: jasmine.SpyObj<any>;
  } {
    const memoryEngine = jasmine.createSpyObj('MemoryEngineService', ['dashboard', 'search', 'deleteConversation']);
    const agentRuntimes = jasmine.createSpyObj('AgentRuntimeService', [
      'overview',
      'skills',
      'prepareOpenClawEcosystemPath',
      'setOpenClawEcosystemPath',
      'prepareOpenClawEcosystemRefresh',
      'refreshOpenClawEcosystem',
      'prepareOpenClawEcosystemUpload',
      'uploadOpenClawEcosystem',
      'prepareOpenClawEcosystemRollback',
      'rollbackOpenClawEcosystem',
    ]);
    const pursuits = jasmine.createSpyObj('PursuitService', ['acceptCandidate', 'archive', 'resolveDecision', 'dashboard', 'brief']);
    const workflows = jasmine.createSpyObj('WorkflowService', ['resolveApproval', 'resolveProposal']);
    const commands = jasmine.createSpyObj('AssistantCommandService', ['logs', 'command']);
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'warning', 'error', 'info']);
    const router = jasmine.createSpyObj<Router>('Router', ['navigate']);
    const component = new CommandDashboardComponent(
      new FormBuilder(),
      memoryEngine,
      agentRuntimes,
      commands,
      pursuits,
      workflows,
      notification as NzNotificationService,
      router,
    );
    spyOn(component, 'refreshPursuits');
    return { component, memoryEngine, agentRuntimes, pursuits, workflows, commands, notification };
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

  function validMemoryDashboard(insightCount: number): any {
    return {
      generatedAt: '2026-09-24T10:00:00Z',
      conversationCount: 0,
      insightCount,
      needsRobert: [],
      delegateToVA: [],
      openLoops: [],
      contradictions: [],
      recentDecisions: [],
      projects: [],
      recentArchives: [],
      warnings: [],
    };
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

  it('retains command history when the ledger returns a malformed payload', () => {
    const { component, commands } = createComponent();
    const history = [{ id: 'command-1', summary: 'Previously confirmed command.', actions: [] }];
    component.commandLogs = history as any;
    commands.logs.and.returnValue(of({ records: [] }));

    component.loadCommandLogs();

    expect(component.commandLogs).toBe(history as any);
    expect((component as any).commandLogsUnavailable).toBeTrue();
  });

  it('cancels an obsolete dashboard refresh so stale memory data cannot replace the latest view', () => {
    const { component, memoryEngine } = createComponent();
    const first = new Subject<any>();
    const second = new Subject<any>();
    memoryEngine.dashboard.and.returnValues(first.asObservable(), second.asObservable());

    component.refresh();
    component.refresh();

    second.next(validMemoryDashboard(2));
    second.complete();
    first.next(validMemoryDashboard(1));
    first.complete();

    expect(component.dashboard).toEqual(validMemoryDashboard(2));
  });

  it('retains the last valid memory dashboard when the API response is incomplete', () => {
    const { component, memoryEngine } = createComponent();
    const previous = validMemoryDashboard(7);
    component.dashboard = previous;
    memoryEngine.dashboard.and.returnValue(of({ insightCount: 0 }));

    component.refresh();

    expect(component.dashboard).toBe(previous);
    expect(component.dashboardUnavailable).toBeTrue();
    expect(component.loading).toBeFalse();
  });

  it('retains the last confirmed pursuit dashboard when a refresh fails', () => {
    const { component, pursuits } = createComponent();
    const previous = {
      counts: { active: 2 }, decisionQueue: [], needsRobert: [], vaReady: [], systemReady: [], blocked: [], stale: [],
      reviewDue: [], planningNeeded: [], recentlyChanged: [], highRisk: [], completionCandidates: [],
    };
    (component.refreshPursuits as any).and.callThrough();
    component.pursuitDashboard = previous as any;
    pursuits.dashboard.and.returnValue(throwError(() => new Error('offline')));
    pursuits.brief.and.returnValue(throwError(() => new Error('offline')));

    component.refreshPursuits();

    expect(component.pursuitDashboard).toBe(previous as any);
    expect(component.pursuitDashboardUnavailable).toBeTrue();
    expect(component.pursuitMetric(component.activePursuitCount())).toBe('stale · 2');
  });

  it('blocks a duplicate assistant action while the command request is in flight', () => {
    const { component, commands } = createComponent();
    const request = new Subject<any>();
    const action = component.actions.find((item) => item.key === 'run-cycle')!;
    commands.command.and.returnValue(request.asObservable());

    component.runDashboardAction(action);
    component.runDashboardAction(action);

    expect(commands.command).toHaveBeenCalledTimes(1);
    expect(component.commandLoading).toBe('run-cycle');
    request.error(new Error('request failed'));
    expect(component.commandLoading).toBe('');
  });

  it('reports partial assistant execution as a failure rather than ready', () => {
    const { component } = createComponent();
    const result = {
      reviewRequired: true,
      agentCycle: { status: 'partial_failure' },
      actions: [{ status: 'failed' }],
    } as any;

    expect(component.commandStatusLabel(result)).toBe('partial failure');
    expect(component.commandStatusColor(result)).toBe('error');
  });

  it('reports a queued workflow distinctly from a completed command', () => {
    const { component } = createComponent();
    const result = { reviewRequired: false, pursuit: { executionQueued: true } } as any;

    expect(component.commandStatusLabel(result)).toBe('workflow queued');
    expect(component.commandStatusColor(result)).toBe('processing');
  });

  it('does not announce a queued workflow as a successful dashboard action', () => {
    const { component, commands, notification } = createComponent();
    spyOn(component, 'refresh');
    const action = component.actions.find((item) => item.key === 'run-cycle')!;
    const result = {
      id: 'command-queued',
      createdAt: '2026-09-24T10:00:00Z',
      intent: 'run',
      summary: 'The governed workflow was accepted by the queue.',
      nextAction: 'The workflow worker will plan and verify the queued work.',
      safetySummary: 'Governed workflow execution.',
      actions: [],
      reviewRequired: false,
      pursuit: { mode: 'matched', matched: true, executionQueued: true },
    };
    commands.command.and.returnValue(of(result));

    component.runDashboardAction(action);

    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.info).toHaveBeenCalledWith(action.title, result.nextAction);
    expect(component.commandStatusLabel(result as any)).toBe('workflow queued');
  });

  it('does not report success for an incomplete assistant response', () => {
    const { component, commands, notification } = createComponent();
    const action = component.actions.find((item) => item.key === 'run-cycle')!;
    commands.command.and.returnValue(of({ summary: 'unverified' }));
    commands.logs.and.returnValue(of([]));

    component.runDashboardAction(action);

    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.error).toHaveBeenCalledWith(
      'Refresh my operating brief could not be confirmed',
      'The command endpoint returned an incomplete result. Check command history before trying again.'
    );
  });

  it('does not convert an invalid pursuit response into an empty queue', () => {
    const { component, pursuits } = createComponent();
    const previous = {
      counts: { active: 3 }, decisionQueue: [], needsRobert: [], vaReady: [], systemReady: [], blocked: [], stale: [],
      reviewDue: [], planningNeeded: [], recentlyChanged: [], highRisk: [], completionCandidates: [],
    };
    (component.refreshPursuits as any).and.callThrough();
    component.pursuitDashboard = previous as any;
    pursuits.dashboard.and.returnValue(of({ counts: { active: 0 }, needsRobert: [] }));
    pursuits.brief.and.returnValue(of({}));

    component.refreshPursuits();

    expect(component.pursuitDashboard).toBe(previous as any);
    expect(component.pursuitDashboardUnavailable).toBeTrue();
    expect(component.pursuitMetric(0)).toBe('stale · 0');
  });

  it('retains the last good memory search and form values when search fails', () => {
    const { component, memoryEngine } = createComponent();
    const previous = { memory: { query: 'older question', usedContext: [], explanation: '' }, facts: [] };
    const request = new Subject<any>();
    component.searchResult = previous as any;
    component.searchForm.setValue({ query: 'current question', projectKey: '018-HAI' });
    memoryEngine.search.and.returnValue(request.asObservable());

    component.search();
    component.search();
    expect(memoryEngine.search).toHaveBeenCalledTimes(1);
    request.error(new Error('search unavailable'));

    expect(component.searchResult).toBe(previous as any);
    expect(component.searchForm.value).toEqual({ query: 'current question', projectKey: '018-HAI' });
    expect(component.searchError).toContain('Previous results are retained');
    expect(component.searching).toBeFalse();
  });

  it('blocks missing or unsafe source links and opens a valid HTTPS source safely', () => {
    const { component, notification } = createComponent();
    spyOn(window, 'open');

    component.openSource();
    component.openSource('javascript:alert(1)');
    component.openSource('https://user:password@example.test/source');
    component.openSource('https://example.test/source');

    expect(window.open).toHaveBeenCalledTimes(1);
    expect(window.open).toHaveBeenCalledWith('https://example.test/source', '_blank', 'noopener,noreferrer');
    expect(notification.warning).toHaveBeenCalledTimes(3);
  });

  it('prevents duplicate archive deletion and removes the archive after confirmed success', () => {
    const { component, memoryEngine } = createComponent();
    const request = new Subject<void>();
    const dashboard = {
      generatedAt: '2026-09-24T10:00:00Z', conversationCount: 0, insightCount: 0,
      needsRobert: [], delegateToVA: [], openLoops: [], contradictions: [], recentDecisions: [],
      sourceCorrections: [], projects: [], recentArchives: [], warnings: [],
    };
    component.dashboard = { ...dashboard, recentArchives: [{ id: 'archive-1', title: 'Old thread' }] } as any;
    memoryEngine.deleteConversation.and.returnValue(request.asObservable());
    memoryEngine.dashboard.and.returnValue(of(dashboard));
    spyOn(window, 'confirm').and.returnValue(true);

    component.deleteArchive('archive-1', 'Old thread');
    component.deleteArchive('archive-1', 'Old thread');
    expect(memoryEngine.deleteConversation).toHaveBeenCalledTimes(1);
    expect(component.deletingArchiveIds.has('archive-1')).toBeTrue();

    request.next();
    request.complete();

    expect(component.deletingArchiveIds.has('archive-1')).toBeFalse();
    expect(component.dashboard?.recentArchives).toEqual([]);
  });

  it('preserves an edited OpenClaw path when a runtime refresh completes', () => {
    const { component, agentRuntimes } = createComponent();
    const response = new Subject<any>();
    component.runtimes = [{ id: 'openclaw', ecosystemPath: 'C:\\old\\package.zip' }] as any;
    component.onOpenClawEcosystemPathInput('C:\\new\\package.zip');
    agentRuntimes.overview.and.returnValue(response.asObservable());

    component.refreshRuntimes();
    response.next({ runtimes: [{ id: 'openclaw', ecosystemPath: 'C:\\old\\package.zip' }], health: [] });

    expect(component.openClawEcosystemPath).toBe('C:\\new\\package.zip');
  });

  it('retains the last runtime inventory when the registry response is malformed', () => {
    const { component, agentRuntimes } = createComponent();
    const previous = [{ id: 'openclaw', name: 'OpenClaw' }];
    component.runtimes = previous as any;
    agentRuntimes.overview.and.returnValue(of({ runtimes: null, health: [] }));

    component.refreshRuntimes();

    expect(component.runtimes).toBe(previous as any);
    expect(component.runtimeOverviewUnavailable).toBeTrue();
    expect(component.runtimeCountMetric()).toBe('stale · 1');
  });

  it('does not replace an unsaved OpenClaw path when ecosystem inventory refreshes', () => {
    const { component, agentRuntimes } = createComponent();
    const savedRuntime = { id: 'openclaw', ecosystemPath: 'C:\\old\\package.zip' };
    agentRuntimes.prepareOpenClawEcosystemRefresh.and.returnValue(of({ authorization: 'approved-once' }));
    agentRuntimes.refreshOpenClawEcosystem.and.returnValue(of(savedRuntime));
    component.runtimes = [savedRuntime as any];
    component.onOpenClawEcosystemPathInput('C:\\draft\\package.zip');

    component.refreshOpenClawEcosystem(savedRuntime as any);

    expect(component.openClawEcosystemPath).toBe('C:\\draft\\package.zip');
    expect((component as any).openClawEcosystemPathEdited).toBeTrue();
  });

  it('does not duplicate runtime skill requests while one is pending', () => {
    const { component, agentRuntimes } = createComponent();
    const request = new Subject<any>();
    const runtime = { id: 'openclaw', name: 'OpenClaw' } as any;
    agentRuntimes.skills.and.returnValue(request.asObservable());

    component.loadRuntimeSkills(runtime);
    component.loadRuntimeSkills(runtime);

    expect(agentRuntimes.skills).toHaveBeenCalledTimes(1);
    expect(component.runtimeSkillsLoading['openclaw']).toBeTrue();
    request.error(new Error('skills unavailable'));
    expect(component.runtimeSkillsLoading['openclaw']).toBeFalse();
    expect(component.runtimeSkillsUnavailable['openclaw']).toBeTrue();
  });

  it('shows unavailable rather than zero when the pursuit service has never returned a snapshot', () => {
    const { component } = createComponent();
    component.pursuitDashboardUnavailable = true;

    expect(component.pursuitMetric(component.pursuitDecisionCount())).toBe('unavailable');
  });

  it('distinguishes a connected OpenClaw discovery gateway from executable runtime readiness', () => {
    const { component } = createComponent();
    const openClaw = {
      id: 'openclaw',
      enabled: true,
      configured: false,
      executionEnabled: false,
    } as any;
    component.runtimes = [openClaw];
    component.runtimeHealth = {
      openclaw: {
        runtimeId: 'openclaw',
        status: 'available',
        reason: 'OpenClaw Companion gateway health endpoint is live.',
      },
    } as any;

    expect(component.openClawPosture(openClaw)).toContain('discovery is connected');
    expect(component.openClawPosture(openClaw)).toContain('Task execution remains disabled');
  });

  it('prepares one owner-bound authorization before setting the OpenClaw ecosystem path', () => {
    const { component, agentRuntimes, notification } = createComponent();
    const authorization = {
      idempotencyKey: 'runtime-openclaw:one-time-key',
      taskId: 'agent-runtime-openclaw-set-path-test',
      approvalSourceId: 'opscontrol-owner:test',
      approvalBindingDigest: 'a'.repeat(64),
    };
    const updatedRuntime = { id: 'openclaw', ecosystemPath: 'C:\\OpenClaw\\openclaw-main.zip' };
    agentRuntimes.prepareOpenClawEcosystemPath.and.returnValue(of(authorization));
    agentRuntimes.setOpenClawEcosystemPath.and.returnValue(of(updatedRuntime));
    component.runtimes = [{ id: 'openclaw' } as any];
    component.openClawEcosystemPath = updatedRuntime.ecosystemPath;

    component.setOpenClawEcosystemPath(component.runtimes[0]);

    expect(agentRuntimes.prepareOpenClawEcosystemPath).toHaveBeenCalledWith(updatedRuntime.ecosystemPath);
    expect(agentRuntimes.setOpenClawEcosystemPath).toHaveBeenCalledWith(updatedRuntime.ecosystemPath, authorization);
    expect(notification.success).toHaveBeenCalledWith(
      'OpenClaw ecosystem path updated',
      `Configured at ${updatedRuntime.ecosystemPath}`,
    );
  });

  it('prepares and applies one-time approval before rolling back the OpenClaw archive', () => {
    const { component, agentRuntimes, notification } = createComponent();
    const authorization = {
      idempotencyKey: 'runtime-openclaw:rollback-key',
      taskId: 'agent-runtime-openclaw-rollback-test',
      approvalSourceId: 'opscontrol-owner:test',
      approvalBindingDigest: 'd'.repeat(64),
    };
    const currentRuntime = {
      id: 'openclaw',
      ecosystemPath: 'C:\\OpenClaw\\current.zip',
      ecosystemRollbackAvailable: true,
    };
    const rolledBackRuntime = {
      ...currentRuntime,
      ecosystemPath: 'C:\\HAI\\archives\\previous.zip',
      ecosystemRollbackAvailable: true,
    };
    agentRuntimes.prepareOpenClawEcosystemRollback.and.returnValue(of(authorization));
    agentRuntimes.rollbackOpenClawEcosystem.and.returnValue(of(rolledBackRuntime));
    component.runtimes = [currentRuntime as any];

    component.rollbackOpenClawEcosystem(currentRuntime as any);

    expect(agentRuntimes.prepareOpenClawEcosystemRollback).toHaveBeenCalledTimes(1);
    expect(agentRuntimes.rollbackOpenClawEcosystem).toHaveBeenCalledWith(authorization);
    expect(component.runtimes[0]).toEqual(rolledBackRuntime as any);
    expect(component.openClawEcosystemPath).toBe(rolledBackRuntime.ecosystemPath);
    expect(component.openClawRollbackLoading).toBeFalse();
    expect(notification.success).toHaveBeenCalledWith(
      'OpenClaw archive rolled back',
      'The previously verified archive is selected. Runtime status has been refreshed.'
    );
  });

  it('does not offer or submit rollback without backend-confirmed availability', () => {
    const { component, agentRuntimes } = createComponent();
    const runtime = { id: 'openclaw', ecosystemRollbackAvailable: false } as any;

    component.rollbackOpenClawEcosystem(runtime);

    expect(agentRuntimes.prepareOpenClawEcosystemRollback).not.toHaveBeenCalled();
    expect(agentRuntimes.rollbackOpenClawEcosystem).not.toHaveBeenCalled();
  });

  it('prevents a second OpenClaw rollback while the approval request is pending', () => {
    const { component, agentRuntimes } = createComponent();
    const pending = new Subject<any>();
    const runtime = { id: 'openclaw', ecosystemRollbackAvailable: true } as any;
    agentRuntimes.prepareOpenClawEcosystemRollback.and.returnValue(pending.asObservable());

    component.rollbackOpenClawEcosystem(runtime);
    component.rollbackOpenClawEcosystem(runtime);

    expect(agentRuntimes.prepareOpenClawEcosystemRollback).toHaveBeenCalledTimes(1);
    expect(component.openClawRollbackLoading).toBeTrue();
    expect(component.isOpenClawBusy()).toBeTrue();
    pending.error({ error: { error: 'Rollback authorization expired.' } });
    expect(component.openClawRollbackLoading).toBeFalse();
  });

  it('surfaces backend rollback errors without changing the selected runtime', () => {
    const { component, agentRuntimes, notification } = createComponent();
    const runtime = {
      id: 'openclaw',
      ecosystemPath: 'C:\\OpenClaw\\current.zip',
      ecosystemRollbackAvailable: true,
    };
    agentRuntimes.prepareOpenClawEcosystemRollback.and.returnValue(of({}));
    agentRuntimes.rollbackOpenClawEcosystem.and.returnValue(
      throwError(() => ({ error: { error: 'Previous archive failed integrity validation.' } }))
    );
    component.runtimes = [runtime as any];

    component.rollbackOpenClawEcosystem(runtime as any);

    expect(component.runtimes[0]).toBe(runtime as any);
    expect(component.openClawRollbackLoading).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith(
      'OpenClaw archive rollback failed',
      'Previous archive failed integrity validation.'
    );
  });
});

describe('CommandDashboardComponent progressive disclosure', () => {
  let fixture: ComponentFixture<CommandDashboardComponent>;
  let memoryEngine: jasmine.SpyObj<any>;
  let runtimes: jasmine.SpyObj<any>;
  let commands: jasmine.SpyObj<any>;
  let pursuits: jasmine.SpyObj<any>;
  let notification: jasmine.SpyObj<any>;

  beforeEach(async () => {
    memoryEngine = jasmine.createSpyObj('MemoryEngineService', ['dashboard', 'search', 'deleteConversation']);
    memoryEngine.dashboard.and.returnValue(of({
      generatedAt: '2026-09-24T10:00:00Z',
      conversationCount: 3,
      insightCount: 4,
      needsRobert: [],
      delegateToVA: [],
      openLoops: [],
      contradictions: [],
      recentDecisions: [],
      sourceCorrections: [],
      projects: [],
      recentArchives: [],
      warnings: [],
    }));
    runtimes = jasmine.createSpyObj('AgentRuntimeService', [
      'overview',
      'prepareOpenClawEcosystemRollback',
      'rollbackOpenClawEcosystem',
    ]);
    runtimes.overview.and.returnValue(of({ runtimes: [], health: [] }));
    commands = jasmine.createSpyObj('AssistantCommandService', ['logs']);
    commands.logs.and.returnValue(of([]));
    pursuits = jasmine.createSpyObj('PursuitService', ['dashboard', 'brief']);
    pursuits.dashboard.and.returnValue(of({
      counts: {},
      decisionQueue: [],
      needsRobert: [],
      vaReady: [],
      systemReady: [],
      blocked: [],
      stale: [],
      reviewDue: [],
      planningNeeded: [],
      recentlyChanged: [],
      highRisk: [],
      completionCandidates: [],
    }));
    pursuits.brief.and.returnValue(of({
      generatedAt: '2026-09-24T10:00:00Z',
      operatingMode: 'steady',
      summary: 'No pursuit needs attention.',
      primaryAction: 'Continue with the next task.',
      needsRobert: 0,
      readyToMove: 0,
      stuck: 0,
      reviewDue: 0,
      planningNeeded: 0,
      completionCandidates: 0,
      recentlyChanged: 0,
      cards: [],
    }));
    notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);

    await TestBed.configureTestingModule({
      declarations: [CommandDashboardComponent],
      imports: [CommonModule, ReactiveFormsModule, RouterTestingModule, ControlRoomModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: MEMORY_ENGINE_SERVICE_TOKEN, useValue: memoryEngine },
        { provide: AgentRuntimeService, useValue: runtimes },
        { provide: AssistantCommandService, useValue: commands },
        { provide: PursuitService, useValue: pursuits },
        { provide: WorkflowService, useValue: {} },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    TestBed.inject(ModuleViewPreferencesService).reset('command-dashboard');
    fixture = TestBed.createComponent(CommandDashboardComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    TestBed.inject(ModuleViewPreferencesService).reset('command-dashboard');
  });

  it('keeps action and readiness summaries in Basic and remembers advanced inventory disclosure', () => {
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('h1')?.textContent).toContain('HAI Command Dashboard');
    expect(page.querySelector('nz-layout')).toBeNull();
    expect(page.querySelector('.header-actions button[aria-label="Refresh command dashboard"]')).not.toBeNull();
    expect(page.querySelector('.header-actions button[aria-label="Refresh runtime health"]')).not.toBeNull();
    expect(page.querySelector('.header-actions button:nth-child(3)')).toBeNull();
    expect(page.querySelectorAll('.action-tile').length).toBeGreaterThan(0);
    expect(page.querySelector('.runtime-status-summary')).not.toBeNull();
    expect(page.querySelector('hai-progressive-section[sectionid="runtime-inventory"] .hai-progressive-section')).toBeNull();

    TestBed.inject(ModuleViewPreferencesService).setMode('command-dashboard', 'advanced');
    fixture.detectChanges();
    expect(page.querySelector('hai-progressive-section[sectionid="runtime-inventory"] .hai-progressive-section__content')).toBeNull();
    const toggle = page.querySelector('hai-progressive-section[sectionid="runtime-inventory"] button') as HTMLButtonElement;
    toggle.click();
    fixture.detectChanges();

    expect(page.querySelector('hai-progressive-section[sectionid="runtime-inventory"] .hai-progressive-section__content')).not.toBeNull();
    expect(TestBed.inject(ModuleViewPreferencesService).get('command-dashboard').openSections['runtime-inventory']).toBeTrue();

    Array.from(page.querySelectorAll('hai-progressive-section .hai-progressive-section__summary'))
      .forEach((summary) => (summary as HTMLButtonElement).click());
    fixture.detectChanges();

    const unnamedButtons = Array.from(page.querySelectorAll('button'))
      .filter((button) => !button.getAttribute('aria-label') && !button.getAttribute('aria-labelledby') && !button.textContent?.trim());
    expect(unnamedButtons).toEqual([]);
    const unnamedInputs = Array.from(page.querySelectorAll<HTMLInputElement>('input:not([type="hidden"])'))
      .filter((input) => !input.getAttribute('aria-label') && !input.labels?.length);
    expect(unnamedInputs).toEqual([]);
  });

  it('renders the rollback action only when runtime inventory confirms availability and applies it on click', () => {
    const component = fixture.componentInstance;
    const authorization = {
      idempotencyKey: 'openclaw:rollback-ui',
      taskId: 'openclaw-rollback-ui',
      approvalSourceId: 'opscontrol-owner:test',
      approvalBindingDigest: 'e'.repeat(64),
    };
    const runtime = {
      id: 'openclaw',
      name: 'OpenClaw',
      enabled: true,
      configured: true,
      executionEnabled: false,
      requiresApproval: true,
      readOnlyDefault: true,
      capabilities: [],
      ecosystemPath: 'C:\\HAI\\archives\\current.zip',
      ecosystemRollbackAvailable: true,
    } as any;
    const rolledBackRuntime = {
      ...runtime,
      ecosystemPath: 'C:\\HAI\\archives\\previous.zip',
      ecosystemRollbackAvailable: false,
    };
    runtimes.prepareOpenClawEcosystemRollback.and.returnValue(of(authorization));
    runtimes.rollbackOpenClawEcosystem.and.returnValue(of(rolledBackRuntime));
    component.runtimes = [runtime];
    TestBed.inject(ModuleViewPreferencesService).setMode('command-dashboard', 'advanced');
    fixture.detectChanges();
    const sectionToggle = fixture.nativeElement.querySelector(
      'hai-progressive-section[sectionid="runtime-inventory"] button'
    ) as HTMLButtonElement;
    sectionToggle.click();
    fixture.detectChanges();

    const rollbackButton = Array.from(
      fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>
    )
      .find((button) => button.textContent?.trim() === 'Roll back archive') as HTMLButtonElement;
    expect(rollbackButton).toBeDefined();
    rollbackButton.click();
    fixture.detectChanges();

    expect(runtimes.prepareOpenClawEcosystemRollback).toHaveBeenCalledTimes(1);
    expect(runtimes.rollbackOpenClawEcosystem).toHaveBeenCalledWith(authorization);
    expect(component.runtimes[0]).toEqual(rolledBackRuntime);
    expect(rollbackButton.isConnected).toBeFalse();
    expect(notification.success).toHaveBeenCalledWith(
      'OpenClaw archive rolled back',
      'The previously verified archive is selected. Runtime status has been refreshed.'
    );
  });

  it('gives each icon-only source and archive action a contextual accessible name', () => {
    fixture.componentInstance.dashboard = {
      generatedAt: '2026-09-24T10:00:00Z',
      conversationCount: 1,
      insightCount: 1,
      needsRobert: [],
      delegateToVA: [{ text: 'Collect invoice', sourceUri: 'https://example.test/va', sourceLabel: 'Email' }],
      openLoops: [],
      contradictions: [],
      recentDecisions: [{ text: 'Use the signed quote', sourceUri: 'https://example.test/decision' }],
      sourceCorrections: [],
      projects: [],
      recentArchives: [{ id: 'archive-1', title: 'Client planning thread', sourceUri: 'https://example.test/archive' }],
      warnings: [],
    } as any;
    TestBed.inject(ModuleViewPreferencesService).setMode('command-dashboard', 'advanced');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    (page.querySelector('hai-progressive-section[sectionid="memory-inspection"] .hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(page.querySelector('button[aria-label="Open source for delegated item: Collect invoice"]')).not.toBeNull();
    expect(page.querySelector('button[aria-label="Open source for decision: Use the signed quote"]')).not.toBeNull();
    expect(page.querySelector('button[aria-label="Open source for archived conversation: Client planning thread"]')).not.toBeNull();
    expect(page.querySelector('button[aria-label="Delete archived conversation: Client planning thread"]')).not.toBeNull();
  });

  it('keeps failed dashboard, runtime, and command-history states visible without opening Advanced', () => {
    memoryEngine.dashboard.and.returnValue(throwError(() => new Error('dashboard offline')));
    runtimes.overview.and.returnValue(throwError(() => new Error('runtime offline')));
    commands.logs.and.returnValue(throwError(() => new Error('history offline')));

    fixture.componentInstance.refresh();
    fixture.componentInstance.refreshRuntimes();
    fixture.componentInstance.loadCommandLogs();
    fixture.componentInstance.dashboardUnavailable = true;
    fixture.componentInstance.runtimeOverviewUnavailable = true;
    fixture.componentInstance.commandLogsUnavailable = true;
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(fixture.componentInstance.dashboardUnavailable).toBeTrue();
    expect(fixture.componentInstance.runtimeOverviewUnavailable).toBeTrue();
    expect(fixture.componentInstance.commandLogsUnavailable).toBeTrue();
    expect(fixture.componentInstance.commandLogsLoading).toBeFalse();
    expect(page.querySelector('.assistant-actions')).not.toBeNull();
    expect(page.querySelector('.runtime-status-summary')).not.toBeNull();
    expect(page.querySelector('hai-progressive-section[sectionid="command-history"] .hai-progressive-section__content')).toBeNull();
  });

  it('shows command history as loading instead of briefly claiming it is empty', () => {
    const pending = new Subject<any>();
    commands.logs.and.returnValue(pending.asObservable());

    fixture.componentInstance.loadCommandLogs();
    TestBed.inject(ModuleViewPreferencesService).setMode('command-dashboard', 'advanced');
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    (page.querySelector('hai-progressive-section[sectionid="command-history"] .hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(fixture.componentInstance.commandLogsLoading).toBeTrue();
    expect(page.querySelector('.command-history-loading')).not.toBeNull();
    expect(page.querySelector('nz-empty')).toBeNull();

    pending.next([]);
    pending.complete();

    expect(fixture.componentInstance.commandLogsLoading).toBeFalse();
    expect(fixture.componentInstance.commandLogsUnavailable).toBeFalse();
  });

  it('renders omitted correction and pursuit data as unknown, not as zero or empty', () => {
    fixture.componentInstance.dashboard = {
      ...fixture.componentInstance.dashboard!,
      sourceCorrections: undefined,
    };
    fixture.componentInstance.pursuitDashboard = undefined;
    fixture.componentInstance.pursuitDashboardUnavailable = true;
    TestBed.inject(ModuleViewPreferencesService).setMode('command-dashboard', 'advanced');
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    (page.querySelector('hai-progressive-section[sectionid="memory-inspection"] .hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();
    const metrics = page.querySelectorAll('.status-strip .metric strong');
    expect(metrics[5]?.textContent?.trim()).toBe('not reported');
    expect(metrics[6]?.textContent?.trim()).toBe('unavailable');
    expect(page.textContent).toContain('This snapshot did not include learned source corrections.');
    expect(page.querySelector('.command-grid.lower-grid nz-card:first-child nz-empty')).toBeNull();
    expect(fixture.componentInstance.correctionMetric([])).toBe('0');
    expect(fixture.componentInstance.correctionMetric(undefined)).toBe('not reported');
  });

  it('does not present intentionally scoped-out workflow queues as zero or empty', () => {
    const dashboard = {
      ...fixture.componentInstance.dashboard!,
      warnings: ['Workflow queues are shown through the owner-scoped pursuits dashboard.'],
    };
    memoryEngine.dashboard.and.returnValue(of(dashboard));
    fixture.destroy();
    fixture = TestBed.createComponent(CommandDashboardComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.workflowQueuesScopedOut()).toBeTrue();

    const page: HTMLElement = fixture.nativeElement;
    const metrics = page.querySelectorAll('.status-strip .metric strong');
    expect(metrics[2]?.textContent?.trim()).toBe('see pursuits');
    expect(page.querySelector('.dashboard-scope-note')?.textContent).toContain('This memory snapshot does not report their counts');
    expect(page.textContent).toContain('This snapshot omitted the approval queue.');
    expect(page.textContent).toContain('Open-loop counts are available in Pursuits');
    expect(page.textContent).not.toContain('No items requiring Robert were reported');
    expect(page.textContent).not.toContain('No open loops were reported');
  });
});

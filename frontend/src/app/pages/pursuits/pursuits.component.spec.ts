import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { FormBuilder, FormsModule, ReactiveFormsModule } from '@angular/forms';
import { ActivatedRoute, convertToParamMap, Router } from '@angular/router';
import { RouterTestingModule } from '@angular/router/testing';
import { BehaviorSubject, EMPTY, of, Subject, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { IProjectDossier, IPursuitAction, IPursuitDecision, IPursuitDetail, IPursuitLink } from '../../models/pursuit.model.interface';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { AutomationsService } from '../../services/automations/automations.service';
import { PursuitService } from '../../services/pursuit.service';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { PursuitsComponent } from './pursuits.component';
import { PursuitsModule } from './pursuits.module';

function projectDossierFixture(projectKey: string, overrides: Partial<IProjectDossier> = {}): IProjectDossier {
  return {
    projectKey,
    generatedAt: '2026-09-26T10:00:00.000Z',
    counts: {
      matchingWorkflows: 1,
      returnedWorkflows: 1,
      returnedMemories: 1,
      checklistItemsReturned: 1,
      openLoopsReturned: 1,
      sourceLinksReturned: 1,
      evidenceClaimsReturned: 1,
      decisionsReturned: 1,
    },
    truncated: { workflows: false, memories: false, workflowContext: false, text: false },
    workflows: [{
      id: 'workflow-1', title: 'Prepare the project evidence', state: 'needs_approval', taskType: 'legal', riskLevel: 'high',
      priorityScore: 90, confidence: 0.8, autonomyLevel: 'draft_only', requiresApproval: true,
      approvalStatus: 'pending', approvalReason: 'Legal work requires review', blockedReason: 'Awaiting review',
      nextAction: 'Review the evidence draft', dueAt: '2026-10-01T12:00:00.000Z', updatedAt: '2026-09-25T12:00:00.000Z',
      checklist: [{ id: 'check-1', label: 'Collect source records', status: 'open', requiresApproval: false }],
      openLoops: [{ id: 'loop-1', responsibleParty: 'lawyer', waitingFor: 'Requested documents', nextAction: 'Follow up', status: 'open' }],
      sourceLinks: [{ sourceType: 'email', sourceUri: 'https://example.test/mail/1', sourceLabel: 'Request email', relationship: 'supports', createdAt: '2026-09-20T12:00:00.000Z' }],
      evidence: [{ claimText: 'A source-backed statement', sourceUri: 'https://example.test/file/1', sourceLabel: 'Evidence file', reliability: 'direct', status: 'verified', needsReview: false, createdAt: '2026-09-20T12:00:00.000Z' }],
      decisions: [{ decisionType: 'routing', decision: 'Needs owner review', reason: 'High risk', ruleApplied: 'legal-review', approved: false, createdAt: '2026-09-20T12:00:00.000Z' }],
    }],
    memories: [{
      id: 'memory-1', kind: 'preference', content: 'Keep claims source-linked', summary: 'Evidence preference', tags: 'legal,evidence',
      confidence: 0.9, sourceUri: 'https://example.test/memory/1', sourceLabel: 'Prior review',
      createdAt: '2026-09-19T12:00:00.000Z', updatedAt: '2026-09-20T12:00:00.000Z',
    }],
    ...overrides,
  };
}

function normalizedPursuitDetail(pursuit: any): IPursuitDetail {
  let normalized!: IPursuitDetail;
  new PursuitService({ get: () => of({ pursuit, summary: {} }) } as any).get(pursuit.id).subscribe((detail) => {
    normalized = detail;
  });
  return normalized;
}

describe('PursuitsComponent action lanes', () => {
  let component: PursuitsComponent;
  let notification: jasmine.SpyObj<{
    info: (title: string, content: string) => void;
    success: (title: string, content: string) => void;
    error: (title: string, content: string) => void;
		warning: (title: string, content: string) => void;
  }>;
  let modal: jasmine.SpyObj<{ confirm: (options: any) => any }>;
  let viewPreferences: ModuleViewPreferencesService;

  beforeEach(() => {
    localStorage.removeItem('hai.module-view.v1.pursuits');
    viewPreferences = new ModuleViewPreferencesService(document);
    notification = jasmine.createSpyObj('NzNotificationService', ['info', 'success', 'error', 'warning']);
    modal = jasmine.createSpyObj('NzModalService', ['confirm']);
    component = new PursuitsComponent(
      new FormBuilder(),
      {} as any,
      {} as any,
      {} as any,
      notification as any,
      modal as any,
      {} as any,
      {} as any,
      viewPreferences,
    );
    (component as any).pursuitsService.portfolioAllocations = jasmine.createSpy('portfolioAllocations').and.returnValue(of([]));
    (component as any).pursuitsService.portfolioExecutionProposals = jasmine.createSpy('portfolioExecutionProposals').and.returnValue(of([]));
    (component as any).pursuitsService.portfolioExecutionProposalDecisionHistory = jasmine
      .createSpy('portfolioExecutionProposalDecisionHistory')
      .and.returnValue(of({ decisions: [], authority: 'approval_decision_only', canExecute: false }));
    (component as any).pursuitsService.portfolioDispatchCoordination = jasmine
      .createSpy('portfolioDispatchCoordination')
      .and.returnValue(EMPTY);
    (component as any).pursuitsService.portfolioDispatchCoordinations = jasmine
      .createSpy('portfolioDispatchCoordinations')
      .and.returnValue(of([]));
    (component as any).workflowService.get = jasmine.createSpy('get').and.returnValue(of(
      portfolioWorkflowRecordFixture('a2288803-346a-4713-8d3b-e6da77de6a0d', 'needs_approval', 'unverified'),
    ));
  });

  afterEach(() => localStorage.removeItem('hai.module-view.v1.pursuits'));

  it('keeps Advanced mode scoped to Pursuits and remembers it across service instances', () => {
    expect(component.isAdvancedView).toBeFalse();
    viewPreferences.setMode('pursuits', 'advanced');

    const restored = new ModuleViewPreferencesService(document);
    expect(component.isAdvancedView).toBeTrue();
    expect(restored.get('pursuits').mode).toBe('advanced');
    expect(restored.get('exceptions').mode).toBe('basic');
  });

  it('cancels the previous project dossier request and clears its data on selection change', () => {
    const service = (component as any).pursuitsService;
    const firstDossier = new Subject<IProjectDossier>();
    const secondDossier = new Subject<IProjectDossier>();
    const firstDetail = new Subject<IPursuitDetail>();
    const secondDetail = new Subject<IPursuitDetail>();
    service.get = jasmine.createSpy('get').and.returnValues(firstDetail.asObservable(), secondDetail.asObservable());
    service.projectDossier = jasmine.createSpy('projectDossier').and.returnValues(
      firstDossier.asObservable(),
      secondDossier.asObservable(),
    );

    component.selectPursuit({ id: 'pursuit-one', projectKey: 'Project One' } as any, false);
    expect(component.projectDossierProjectKey).toBe('Project One');
    expect(component.projectDossierLoading).toBeTrue();

    component.selectPursuit({ id: 'pursuit-two', projectKey: 'Project & Two' } as any, false);
    expect(component.projectDossier).toBeUndefined();
    expect(component.projectDossierProjectKey).toBe('Project & Two');
    expect(firstDossier.observers.length).toBe(0);
    expect(service.projectDossier.calls.allArgs()).toEqual([['Project One'], ['Project & Two']]);

    firstDossier.next(projectDossierFixture('Project One'));
    expect(component.projectDossier).toBeUndefined();
    secondDossier.next(projectDossierFixture('Project & Two'));
    expect(component.projectDossier?.projectKey).toBe('Project & Two');

    secondDetail.next(normalizedPursuitDetail({ id: 'pursuit-two', title: 'Selected project', projectKey: 'Project & Two' }));
    expect(component.selected?.pursuit.id).toBe('pursuit-two');
    expect(service.projectDossier).toHaveBeenCalledTimes(2);
  });

  it('loads resource usage only when the Advanced ledger is opened', () => {
    const loadResourceEvents = spyOn(component, 'loadResourceEvents');

    component.onResourceLedgerOpen(false);
    expect(loadResourceEvents).not.toHaveBeenCalled();

    component.onResourceLedgerOpen(true);
    expect(loadResourceEvents).toHaveBeenCalledOnceWith();
  });

  it('reuses active pursuits returned by the dashboard instead of issuing a second list request', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.dashboard = jasmine.createSpy('dashboard').and.returnValue(of({
      counts: {}, pursuits: [{ id: 'pursuit-1', title: 'Ready pursuit' }],
    }));
    pursuitService.list = jasmine.createSpy('list').and.returnValue(of([]));
    spyOn<any>(component as any, 'selectPursuit');

    component.load();

    expect(pursuitService.dashboard).toHaveBeenCalledWith(true);
    expect(pursuitService.list).not.toHaveBeenCalled();
    expect(component.pursuits.map((pursuit) => pursuit.id)).toEqual(['pursuit-1']);
  });

  it('keeps an initial dashboard failure distinct from a genuinely empty account', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.dashboard = jasmine.createSpy('dashboard').and.returnValue(
      throwError(() => ({ error: { error: 'Dashboard API is unavailable.' } })),
    );

    component.load();

    expect(component.loading).toBeFalse();
    expect(component.dashboardError).toBe('Dashboard API is unavailable.');
    expect(component.loadError).toBe('Dashboard API is unavailable.');
    expect(component.hasLastKnownData).toBeFalse();
    expect(component.showEmptyState).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith('Pursuits unavailable', 'Dashboard API is unavailable.');
  });

  it('shows the empty state only after a successful empty response', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.dashboard = jasmine.createSpy('dashboard').and.returnValue(of({ counts: {}, pursuits: [] }));

    component.load();

    expect(component.loadError).toBe('');
    expect(component.loading).toBeFalse();
    expect(component.showEmptyState).toBeTrue();
  });

  it('retains last-known pursuits and marks them stale after a refresh failure', () => {
    const pursuit = { id: 'pursuit-1', title: 'Existing outcome' } as any;
    const dashboard = { counts: { active: 1 }, pursuits: [pursuit] } as any;
    const pursuitService = (component as any).pursuitsService;
    component.dashboard = dashboard;
    component.pursuits = [pursuit];
    pursuitService.dashboard = jasmine.createSpy('dashboard').and.returnValue(
      throwError(() => ({ error: { error: 'Refresh timed out.' } })),
    );

    component.load();

    expect(component.dashboard).toBe(dashboard);
    expect(component.pursuits).toEqual([pursuit]);
    expect(component.hasLastKnownData).toBeTrue();
    expect(component.showEmptyState).toBeFalse();
    expect(component.loadError).toBe('Refresh timed out.');
  });

  it('exposes list API failures instead of presenting them as an empty pursuits list', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.dashboard = jasmine.createSpy('dashboard').and.returnValue(of({ counts: { active: 0 } }));
    pursuitService.list = jasmine.createSpy('list').and.returnValue(
      throwError(() => ({ error: { error: 'Pursuit list is unavailable.' } })),
    );

    component.load();

    expect(component.loading).toBeFalse();
    expect(component.pursuitsError).toBe('Pursuit list is unavailable.');
    expect(component.loadError).toBe('Pursuit list is unavailable.');
    expect(component.hasLastKnownData).toBeTrue();
    expect(component.showEmptyState).toBeFalse();
  });

  it('shows required-field state when create or route input is submitted empty', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.create = jasmine.createSpy('create');
    pursuitService.routeIntake = jasmine.createSpy('routeIntake');

    component.createPursuit();
    component.routeIntake();

    expect(component.createForm.get('title')?.touched).toBeTrue();
    expect(component.createForm.get('title')?.invalid).toBeTrue();
    expect(component.routedIntakeForm.get('input')?.touched).toBeTrue();
    expect(component.routedIntakeForm.get('input')?.invalid).toBeTrue();
    expect(pursuitService.create).not.toHaveBeenCalled();
    expect(pursuitService.routeIntake).not.toHaveBeenCalled();
  });

  it('reconciles life domains and refreshes the pursuit view', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.reconcileLifeDomains = jasmine.createSpy('reconcileLifeDomains').and.returnValue(of({
      scanned: 4, projected: 3, skipped: 1, failed: 0,
    }));
    spyOn(component, 'load');

    component.reconcileLifeDomains();

    expect(pursuitService.reconcileLifeDomains).toHaveBeenCalled();
    expect(component.lifeDomainReconciliation).toEqual({ scanned: 4, projected: 3, skipped: 1, failed: 0 });
    expect(notification.success).toHaveBeenCalledWith('Life domains updated', '3 indexed; 1 unchanged.');
    expect(component.load).toHaveBeenCalled();
  });

  it('keeps the view stable when life-domain reconciliation fails', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.reconcileLifeDomains = jasmine.createSpy('reconcileLifeDomains').and.returnValue(
      throwError(() => ({ error: { error: 'Life-domain projection is unavailable.' } })),
    );

    component.reconcileLifeDomains();

    expect(component.lifeDomainReconciliationRunning).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith('Life-domain update unavailable', 'Life-domain projection is unavailable.');
  });

  it('opens the first action in the selected operational lane', () => {
    const action: IPursuitAction = {
      label: 'Prepare the evidence index',
      owner: 'VA',
      riskLevel: 'low',
      requiresApproval: false,
      reason: 'The evidence list is ready for preparation.',
    };
    component.selected = {
      actionQueues: {
        needsRobert: [],
        vaReady: [action],
        systemReady: [],
        waiting: [],
      },
    } as unknown as IPursuitDetail;
    spyOn(component, 'openAction');

    component.openDigestLane('va');

    expect(component.openAction).toHaveBeenCalledWith(action);
    expect(notification.info).not.toHaveBeenCalled();
  });

  it('explains when the selected operational lane has no action', () => {
    component.selected = {
      actionQueues: {
        needsRobert: [],
        vaReady: [],
        systemReady: [],
        waiting: [],
      },
    } as unknown as IPursuitDetail;
    spyOn(component, 'openAction');

    component.openDigestLane('system');

    expect(component.openAction).not.toHaveBeenCalled();
    expect(notification.info).toHaveBeenCalledWith('System-ready lane', 'There is no system-ready action to open for this pursuit.');
  });

  it('uses the explicit candidate acceptance endpoint for an approved candidate decision', () => {
    const candidate = {
      id: 'candidate-1',
      riskLevel: 'medium',
      status: 'active',
      sourceOfCreation: 'manual_pursuit_intake',
    } as any;
    const detail = normalizedPursuitDetail(candidate);
    const pursuitService = (component as any).pursuitsService;
    pursuitService.acceptCandidate = jasmine.createSpy('acceptCandidate').and.returnValue(of(detail));
    component.selected = detail;
    spyOn(component, 'load');
    const decision: IPursuitDecision = {
      id: 'pursuit:candidate-1:candidate-review',
      decisionType: 'pursuit_candidate_review',
      riskLevel: 'medium',
      reason: 'Confirm this imported objective before planning work.',
    } as IPursuitDecision;

    (component as any).resolvePursuitCandidateReview(decision, true);

    expect(pursuitService.acceptCandidate).toHaveBeenCalledWith('candidate-1', {
      requiresReview: false,
      reviewReason: decision.reason,
    });
    expect(notification.success).toHaveBeenCalledWith(
      'Candidate accepted',
      'The returned pursuit is now active for governed HAI handling. Review linked workflow state separately; execution is not confirmed.',
    );
    expect(component.selected?.pursuit.sourceOfCreation).toBe('manual_pursuit_intake');
  });

  it('does not claim that routed candidate intake created governed work', () => {
    const pursuitService = (component as any).pursuitsService;
    const detail = normalizedPursuitDetail({
      id: 'candidate-1',
      status: 'waiting',
      sourceOfCreation: 'manual_pursuit_candidate',
    });
    detail.workflows = [];
    detail.links = [];
    pursuitService.routeIntake = jasmine.createSpy('routeIntake').and.returnValue(of({
      mode: 'candidate_created',
      createdCandidate: true,
      matched: false,
      pursuitId: 'candidate-1',
      matches: [],
      detail,
    }));
    spyOn(component, 'load');
    const selectPursuit = spyOn<any>(component as any, 'selectPursuitById');
    component.routedIntakeForm.patchValue({ input: 'An unmatched source signal' });

    component.routeIntake();

    expect(notification.info).toHaveBeenCalledWith(
      'Pursuit candidate needs review',
      'The returned pursuit is a candidate awaiting explicit acceptance. This response does not authorize execution.',
    );
    expect(notification.success).not.toHaveBeenCalledWith('Pursuit candidate created', jasmine.anything());
    expect(selectPursuit).toHaveBeenCalledWith('candidate-1', true);
    expect(pursuitService.routeIntake.calls.mostRecent().args[0].trigger).toBe('pursuit_dashboard_global_intake');
  });

  it('opens a linked pursuit from the relationship ledger', () => {
    const router = (component as any).router;
    router.navigate = jasmine.createSpy('navigate');
    const link: IPursuitLink = {
      id: 'link-1',
      pursuitId: 'pursuit-1',
      linkType: 'pursuit',
      linkId: 'pursuit-2',
      relationship: 'related_case',
      confidence: 0.9,
      createdAt: '2026-07-14T00:00:00Z',
    };

    component.openLinkedPursuit(link);

    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: 'pursuit-2' } });
  });

  it('ignores a stale pursuit detail response after the user selects another pursuit', () => {
    const firstResponse = new Subject<IPursuitDetail>();
    const secondResponse = new Subject<IPursuitDetail>();
    const pursuitService = (component as any).pursuitsService;
    pursuitService.get = jasmine.createSpy('get').and.returnValues(firstResponse, secondResponse);
    const router = (component as any).router;
    router.navigate = jasmine.createSpy('navigate');

    component.selectPursuit({ id: 'first' } as any);
    component.selectPursuit({ id: 'second' } as any);

    firstResponse.next({ pursuit: { id: 'first' } } as IPursuitDetail);
    secondResponse.next({ pursuit: { id: 'second' } } as IPursuitDetail);

    expect(component.selected?.pursuit.id).toBe('second');
    expect(pursuitService.get).toHaveBeenCalledTimes(2);
  });

  it('creates a structured outcome contract from the basic pursuit form', () => {
    const pursuitService = (component as any).pursuitsService;
    const created = { id: 'pursuit-1', title: 'Operational outcome' } as any;
    pursuitService.create = jasmine.createSpy('create').and.returnValue(of(created));
    spyOn(component, 'selectPursuit');
    component.createForm.patchValue({
      title: 'Operational outcome',
      desiredOutcome: 'A verified operational result',
      successCriteriaText: 'Live workflow passes\nEvidence is linked',
      stopConditionsText: 'Stop when the approved budget is reached',
      dependenciesText: 'External answer arrives',
      targetAt: '2026-09-01',
      reviewCadenceDays: 14,
      maxEffortHours: 12,
      maxSpendEur: 0,
      maxParallelWorkflows: 2,
    });

    component.createPursuit();

    const request = pursuitService.create.calls.mostRecent().args[0];
    expect(request.successCriteria.map((item: any) => item.description)).toEqual(['Live workflow passes', 'Evidence is linked']);
    expect(request.stopConditions[0].status).toBe('monitoring');
    expect(request.dependencies[0].status).toBe('pending');
    expect(request.targetAt).toBe('2026-09-01T00:00:00Z');
    expect(request.resourceLimits).toEqual(jasmine.objectContaining({ maxEffortHours: 12, maxSpendEur: 0, maxParallelWorkflows: 2 }));
    expect(component.pursuits.map((pursuit) => pursuit.id)).toEqual(['pursuit-1']);
    expect(component.loading).toBeFalse();
    expect(component.selectPursuit).toHaveBeenCalledWith(created);
  });

  it('does not advertise or call planning when the governed action queue blocks new work', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.plan = jasmine.createSpy('plan');
    component.selected = {
      pursuit: { id: 'pursuit-1', riskLevel: 'low' },
      actionQueues: { needsRobert: [], vaReady: [], systemReady: [], waiting: [] },
      blockers: [{ label: 'Resource ceiling reached', reason: 'effort ceiling exhausted', owner: 'Robert' }],
      resourceUsage: { blockingReason: 'effort ceiling exhausted' },
    } as unknown as IPursuitDetail;

    expect(component.canCreateFirstWorkflow()).toBeFalse();
    component.createFirstWorkflowPlan();

    expect(pursuitService.plan).not.toHaveBeenCalled();
    expect(notification.info).toHaveBeenCalledWith('Planning blocked', 'effort ceiling exhausted');
  });

  it('submits only explicit reviewed estimates to the advisory portfolio endpoint', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.planPortfolio = jasmine.createSpy('planPortfolio').and.callFake((request: any) => of({
      planId: request.planId,
      status: 'feasible',
      pursuitsConsidered: 1,
      pursuitsPlanned: 1,
      priorities: [],
      exclusions: [],
      capacity: { status: 'applied', reason: 'fresh owner-confirmed capacity', appliedMinutes: 480 },
      authority: 'advisory_only',
      canExecute: false,
    }));
    component.pursuits = [{
      id: '98c4178f-0d3c-4586-a34a-a137024ad172',
      title: 'Prepare evidence bundle',
      status: 'active',
      riskLevel: 'high',
      autonomyLevel: 'approve_before_execute',
      archived: false,
    }] as any;
    component.openPortfolioPlanner();
    const draft = component.portfolioDrafts[0];
    draft.selected = true;
    draft.optimisticMinutes = 60;
    draft.expectedMinutes = 90;
    draft.pessimisticMinutes = 120;
    draft.estimateBasis = 'Prior reviewed evidence-bundle run';
    draft.factorsReviewed = true;
    component.portfolioHorizonStart = '2026-08-05T09:00';
    component.portfolioHorizonEnd = '2026-08-05T17:00';

    component.planPortfolio();

    const request = pursuitService.planPortfolio.calls.mostRecent().args[0];
    expect(request.pursuits.length).toBe(1);
    expect(request.pursuits[0].duration).toEqual(jasmine.objectContaining({
      optimisticMinutes: 60,
      expectedMinutes: 90,
      pessimisticMinutes: 120,
      basis: 'Prior reviewed evidence-bundle run',
    }));
    expect(Object.keys(request.pursuits[0].factors).length).toBe(25);
    expect(request.budget.maxCostMicros).toBe(0);
    expect(request.availability).toEqual([{ start: request.horizonStart, end: request.horizonEnd }]);
    expect(component.portfolioPlanningRequest).toBe(request);
    expect(component.portfolioResult?.authority).toBe('advisory_only');
    expect(notification.success).toHaveBeenCalled();
  });

  it('does not call portfolio planning until every selected factor set is confirmed', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.planPortfolio = jasmine.createSpy('planPortfolio');
    component.pursuits = [{
      id: '98c4178f-0d3c-4586-a34a-a137024ad172',
      title: 'Prepare evidence bundle',
      status: 'active',
      archived: false,
    }] as any;
    component.openPortfolioPlanner();
    Object.assign(component.portfolioDrafts[0], {
      selected: true,
      optimisticMinutes: 60,
      expectedMinutes: 90,
      pessimisticMinutes: 120,
      factorsReviewed: false,
    });
    component.portfolioHorizonStart = '2026-08-05T09:00';
    component.portfolioHorizonEnd = '2026-08-05T17:00';

    component.planPortfolio();

    expect(pursuitService.planPortfolio).not.toHaveBeenCalled();
    expect(component.portfolioPlanningError).toContain('review and confirm');
  });

  it('copies a reviewed calibration explicitly and binds its exact evidence to the next plan', () => {
    const pursuitId = '98c4178f-0d3c-4586-a34a-a137024ad172';
    component.pursuits = [{
      id: pursuitId, title: 'Prepare evidence bundle', status: 'active', projectKey: 'hai', archived: false,
    }] as any;
    component.openPortfolioPlanner();
    const draft = component.portfolioDrafts[0];
    Object.assign(draft, {
      selected: true,
      optimisticMinutes: 60,
      expectedMinutes: 90,
      pessimisticMinutes: 120,
      estimatedCostEur: 1,
      estimateBasis: 'Owner estimate',
      factorsReviewed: true,
    });
    component.portfolioResult = { planId: 'old-result' } as any;
    const calibration: any = {
      pursuitId,
      scopeKey: 'project:hai',
      status: 'available',
      reason: 'Reviewed settlement history is available.',
      proposalId: 'proposal-1',
      proposalVersion: 'portfolio-estimate-calibration:v1',
      applicationId: 'application-1',
      evidenceDigest: `sha256:${'a'.repeat(64)}`,
      sampleCount: 5,
      confidence: 0.72,
      effortMultiplier: 1.5,
      costMultiplier: 1.25,
      sourceOptimisticMinutes: 60,
      sourceExpectedMinutes: 90,
      sourcePessimisticMinutes: 120,
      sourceCostMicros: 1_000_000,
      suggestedOptimisticMinutes: 90,
      suggestedExpectedMinutes: 135,
      suggestedPessimisticMinutes: 180,
      suggestedCostMicros: 1_250_000,
      applied: false,
    };

    component.applyPortfolioCalibration(calibration);

    expect(draft.optimisticMinutes).toBe(90);
    expect(draft.expectedMinutes).toBe(135);
    expect(draft.pessimisticMinutes).toBe(180);
    expect(draft.estimatedCostEur).toBe(1.25);
    expect(draft.calibration).toEqual(jasmine.objectContaining({
      scopeKey: 'project:hai',
      proposalVersion: 'portfolio-estimate-calibration:v1',
      evidenceDigest: `sha256:${'a'.repeat(64)}`,
    }));
    expect(draft.calibration?.sourceDuration.expectedMinutes).toBe(90);
    expect(component.portfolioResult).toBeUndefined();
    expect(notification.success).toHaveBeenCalledWith(
      'Reviewed estimate copied',
      'Recalculate the portfolio to bind this exact calibration revision. No work was approved or executed.',
    );

    component.portfolioHorizonStart = '2026-08-05T09:00';
    component.portfolioHorizonEnd = '2026-08-05T17:00';
    const pursuitService = (component as any).pursuitsService;
    pursuitService.planPortfolio = jasmine.createSpy('planPortfolio').and.callFake((request: any) => of({
      planId: request.planId,
      asOf: request.asOf,
      status: 'feasible',
      pursuitsConsidered: 1,
      pursuitsPlanned: 1,
      priorities: [],
      exclusions: [],
      calibrations: [{
        ...calibration,
        status: 'bound',
        applied: true,
        sourceOptimisticMinutes: 60,
        sourceExpectedMinutes: 90,
        sourcePessimisticMinutes: 120,
        sourceCostMicros: 1_000_000,
        suggestedOptimisticMinutes: 90,
        suggestedExpectedMinutes: 135,
        suggestedPessimisticMinutes: 180,
        suggestedCostMicros: 1_250_000,
      }],
      capacity: { status: 'applied', reason: 'fresh capacity', appliedMinutes: 480 },
      authority: 'advisory_only',
      canExecute: false,
    }));

    component.planPortfolio();

    const request = pursuitService.planPortfolio.calls.mostRecent().args[0];
    expect(request.pursuits[0].calibration).toEqual(draft.calibration);
    expect(component.portfolioResult?.calibrations?.[0].status).toBe('bound');
  });

  it('rejects a calibration suggestion when its source estimate is stale', () => {
    component.pursuits = [{ id: 'pursuit-1', title: 'Changed estimate', status: 'active', archived: false }] as any;
    component.openPortfolioPlanner();
    const draft = component.portfolioDrafts[0];
    Object.assign(draft, {
      optimisticMinutes: 30, expectedMinutes: 60, pessimisticMinutes: 90, estimatedCostEur: 0,
    });

    component.applyPortfolioCalibration({
      pursuitId: 'pursuit-1', scopeKey: 'pursuit:pursuit-1', status: 'available', reason: 'reviewed',
      proposalId: 'proposal', proposalVersion: 'version', applicationId: 'application',
      evidenceDigest: `sha256:${'b'.repeat(64)}`, sampleCount: 3, confidence: 0.6,
      sourceOptimisticMinutes: 30, sourceExpectedMinutes: 45, sourcePessimisticMinutes: 90,
      sourceCostMicros: 0, suggestedOptimisticMinutes: 36, suggestedExpectedMinutes: 54,
      suggestedPessimisticMinutes: 108, suggestedCostMicros: 0, applied: false,
    });

    expect(draft.calibration).toBeUndefined();
    expect(notification.error).toHaveBeenCalledWith(
      'Suggestion not applied',
      'The source estimate changed or the reviewed calibration evidence is incomplete. Recalculate the plan.',
    );
  });

  it('accepts a scheduled portfolio allocation only after deliberate confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const decisionDigest = 'a'.repeat(64);
    const planningRequest = {
      planId: 'portfolio-accept',
      asOf: '2026-08-04T08:00:00.000Z',
      horizonStart: '2026-08-05T09:00:00.000Z',
      horizonEnd: '2026-08-05T17:00:00.000Z',
      durationMode: 'conservative',
      availability: [{ start: '2026-08-05T09:00:00.000Z', end: '2026-08-05T17:00:00.000Z' }],
      pursuits: [{
        pursuitId: 'pursuit-1',
        duration: { optimisticMinutes: 60, expectedMinutes: 90, pessimisticMinutes: 120 },
        estimatedUsage: { costMicros: 0, inputTokens: 0, outputTokens: 0, toolCalls: 0 },
        factors: {},
      }],
      budget: { maxCostMicros: 0 },
      approvalPolicy: { softDeadlineMiss: true },
    } as any;
    component.portfolioPlanningRequest = planningRequest;
    component.portfolioResult = {
      planId: planningRequest.planId,
      asOf: planningRequest.asOf,
      status: 'feasible',
      pursuitsConsidered: 1,
      pursuitsPlanned: 1,
      priorities: [],
      exclusions: [],
      capacity: { status: 'applied', reason: 'fresh owner-confirmed capacity', appliedMinutes: 120 },
      decision: {
        planId: planningRequest.planId,
        feasibility: 'feasible',
        decisionDigest,
        scheduled: [{
          taskId: 'pursuit-1',
          start: planningRequest.horizonStart,
          end: '2026-08-05T11:00:00.000Z',
          plannedDurationMinutes: 120,
        }],
        criticalBlockers: [],
        authority: 'advisory_only',
        canExecute: false,
        grantsAuthority: false,
      },
      authority: 'advisory_only',
      canExecute: false,
    } as any;
    const accepted = {
      allocation: {
        id: 'allocation-1',
        planId: planningRequest.planId,
        requestDigest: 'b'.repeat(64),
        decisionDigest,
        status: 'accepted',
        durationMode: planningRequest.durationMode,
        horizonStart: planningRequest.horizonStart,
        horizonEnd: planningRequest.horizonEnd,
        actor: 'owner@example.com',
        confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
        recordDigest: 'c'.repeat(64),
        acceptedAt: '2026-08-04T09:00:00.000Z',
      },
      items: [{
        id: 'item-1',
        allocationId: 'allocation-1',
        pursuitId: 'pursuit-1',
        scheduledStart: planningRequest.horizonStart,
        scheduledEnd: '2026-08-05T11:00:00.000Z',
        durationMinutes: 120,
        estimatedCostMicros: 0,
        requiresApproval: false,
        approvalReasons: [],
        reservationId: 'reservation-1',
        recordDigest: 'd'.repeat(64),
        createdAt: '2026-08-04T09:00:00.000Z',
      }],
      replayed: false,
      authority: 'allocation_only',
      canExecute: false,
    };
    pursuitService.acceptPortfolioAllocation = jasmine.createSpy('acceptPortfolioAllocation').and.returnValue(of(accepted));

    expect(component.canAcceptPortfolioAllocation()).toBeTrue();
    component.acceptPortfolioAllocation();

    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.acceptPortfolioAllocation).not.toHaveBeenCalled();
    expect(component.portfolioAcceptancePending).toBeTrue();
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('does not approve risky work');
    confirmation.nzOnOk();

    expect(pursuitService.acceptPortfolioAllocation).toHaveBeenCalledTimes(1);
    const request = pursuitService.acceptPortfolioAllocation.calls.mostRecent().args[0];
    expect(request).toEqual({
      planningRequest,
      expectedDecisionDigest: decisionDigest,
      confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
    });
    expect(request.planningRequest).toBe(planningRequest);
    expect(component.portfolioAllocationResult).toBe(accepted as any);
    expect(component.portfolioAllocationHistory).toEqual([accepted as any]);
    expect(component.canAcceptPortfolioAllocation()).toBeFalse();
    expect(notification.success).toHaveBeenCalledWith(
      'Portfolio allocation accepted',
      'Bounded pursuit capacity is reserved. No work was approved or executed.',
    );

    component.acceptPortfolioAllocation();
    confirmation.nzOnOk();
    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.acceptPortfolioAllocation).toHaveBeenCalledTimes(1);
  });

  it('does not offer allocation acceptance when a critical blocker exists', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.acceptPortfolioAllocation = jasmine.createSpy('acceptPortfolioAllocation');
    component.portfolioPlanningRequest = { planId: 'portfolio-blocked' } as any;
    component.portfolioResult = {
      planId: 'portfolio-blocked',
      authority: 'advisory_only',
      canExecute: false,
      capacity: { status: 'applied', reason: 'fresh owner-confirmed capacity', appliedMinutes: 60 },
      decision: {
        planId: 'portfolio-blocked',
        feasibility: 'feasible_with_approvals',
        decisionDigest: 'e'.repeat(64),
        scheduled: [{ taskId: 'pursuit-1' }],
        criticalBlockers: [{ code: 'capacity_conflict', detail: 'No bounded capacity remains.', blocksFeasibility: true }],
        authority: 'advisory_only',
        canExecute: false,
        grantsAuthority: false,
      },
    } as any;

    expect(component.canAcceptPortfolioAllocation()).toBeFalse();
    component.acceptPortfolioAllocation();

    expect(modal.confirm).not.toHaveBeenCalled();
    expect(pursuitService.acceptPortfolioAllocation).not.toHaveBeenCalled();
  });

  it('does not offer allocation acceptance without applied owner capacity', () => {
    component.portfolioPlanningRequest = { planId: 'portfolio-capacity-required' } as any;
    component.portfolioResult = {
      planId: 'portfolio-capacity-required',
      authority: 'advisory_only',
      canExecute: false,
      capacity: { status: 'stale', reason: 'Record a fresh capacity check-in.' },
      decision: {
        planId: 'portfolio-capacity-required',
        feasibility: 'feasible',
        decisionDigest: 'c'.repeat(64),
        scheduled: [{ taskId: 'pursuit-1' }],
        criticalBlockers: [],
        authority: 'advisory_only',
        canExecute: false,
        grantsAuthority: false,
      },
    } as any;

    expect(component.canAcceptPortfolioAllocation()).toBeFalse();
  });

  it('opens LifeOps when capacity needs review', () => {
    const router = jasmine.createSpyObj('Router', ['navigate']);
    (component as any).router = router;
    component.portfolioPlannerVisible = true;

    component.openCapacityWorkspace();

    expect(component.portfolioPlannerVisible).toBeFalse();
    expect(router.navigate).toHaveBeenCalledWith(['/life-ops']);
  });

  it('fails closed when allocation acceptance returns mismatched authority or digest evidence', () => {
    const pursuitService = (component as any).pursuitsService;
    const expectedDigest = 'f'.repeat(64);
    const planningRequest = {
      planId: 'portfolio-reject',
      durationMode: 'expected',
      horizonStart: '2026-08-05T09:00:00.000Z',
      horizonEnd: '2026-08-05T17:00:00.000Z',
      pursuits: [{ pursuitId: 'pursuit-1', estimatedUsage: { costMicros: 0 } }],
    } as any;
    component.portfolioPlanningRequest = planningRequest;
    component.portfolioResult = {
      planId: planningRequest.planId,
      authority: 'advisory_only',
      canExecute: false,
      capacity: { status: 'applied', reason: 'fresh owner-confirmed capacity', appliedMinutes: 60 },
      decision: {
        planId: planningRequest.planId,
        feasibility: 'feasible',
        decisionDigest: expectedDigest,
        scheduled: [{
          taskId: 'pursuit-1',
          start: planningRequest.horizonStart,
          end: '2026-08-05T10:00:00.000Z',
          plannedDurationMinutes: 60,
        }],
        criticalBlockers: [],
        authority: 'advisory_only',
        canExecute: false,
        grantsAuthority: false,
      },
    } as any;
    pursuitService.acceptPortfolioAllocation = jasmine.createSpy('acceptPortfolioAllocation').and.returnValue(of({
      allocation: {
        id: 'allocation-bad',
        planId: planningRequest.planId,
        requestDigest: '1'.repeat(64),
        decisionDigest: '2'.repeat(64),
        status: 'accepted',
        durationMode: planningRequest.durationMode,
        horizonStart: planningRequest.horizonStart,
        horizonEnd: planningRequest.horizonEnd,
        actor: 'owner@example.com',
        confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
        recordDigest: '3'.repeat(64),
        acceptedAt: '2026-08-04T09:00:00.000Z',
      },
      items: [],
      replayed: false,
      authority: 'execution_authority',
      canExecute: true,
    }));

    component.acceptPortfolioAllocation();
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioAllocationResult).toBeUndefined();
    expect(component.portfolioAcceptanceError).toContain('mismatched authority, plan, or decision evidence');
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('lazy-loads an empty allocation history only when the portfolio planner opens', () => {
    const pursuitService = (component as any).pursuitsService;
    const history = new Subject<any[]>();
    pursuitService.portfolioAllocations.and.returnValue(history.asObservable());

    expect(pursuitService.portfolioAllocations).not.toHaveBeenCalled();
    expect(component.portfolioAllocationHistoryLoading).toBeFalse();

    component.openPortfolioPlanner();

    expect(pursuitService.portfolioAllocations).toHaveBeenCalledOnceWith(20);
    expect(component.portfolioAllocationHistoryLoading).toBeTrue();
    history.next([]);
    history.complete();

    expect(component.portfolioAllocationHistoryLoading).toBeFalse();
    expect(component.portfolioAllocationHistoryLoaded).toBeTrue();
    expect(component.portfolioAllocationHistory).toEqual([]);
    expect(component.portfolioAllocationHistoryError).toBe('');
  });

  it('restores the latest immutable execution proposal after allocation history reload', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = {
      ...portfolioExecutionProposalFixture(allocation),
      replayed: true,
      freshness: {
        status: 'recovered_snapshot',
        revalidationRequired: true,
        checkedAt: '2026-08-04T11:00:00.000Z',
        reason: 'Recovered evidence requires current eligibility revalidation.',
      },
    };
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    pursuitService.portfolioAllocations.and.returnValue(of([allocation]));
    pursuitService.portfolioExecutionProposals.and.returnValue(of([proposal]));
    pursuitService.portfolioDispatchCoordinations.and.returnValue(of([coordination]));

    component.openPortfolioPlanner();

    expect(pursuitService.portfolioExecutionProposals).toHaveBeenCalledOnceWith([allocation.allocation.id]);
    expect(pursuitService.portfolioDispatchCoordinations).toHaveBeenCalledOnceWith([proposal.proposal.id]);
    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toEqual(proposal as any);
    expect(component.portfolioDispatchCoordination[proposal.proposal.id]).toEqual(coordination as any);
    expect(component.portfolioDispatchSelections[proposal.proposal.id]).toEqual({ [proposal.items[0].id]: false });
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id]).toBeUndefined();
  });

  it('fails closed when recovered proposals do not match requested allocation evidence', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    proposal.proposal.allocationId = 'different-allocation';
    pursuitService.portfolioAllocations.and.returnValue(of([allocation]));
    pursuitService.portfolioExecutionProposals.and.returnValue(of([proposal]));

    component.openPortfolioPlanner();

    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id])
      .toContain('immutable owner or proposal-only boundaries');
  });

  it('fails closed when recovered proposal freshness claims current execution readiness', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = {
      ...portfolioExecutionProposalFixture(allocation),
      replayed: true,
      freshness: {
        status: 'prepared_snapshot',
        revalidationRequired: false,
        checkedAt: '2026-08-04T11:00:00.000Z',
        reason: 'No revalidation needed.',
      },
    };
    pursuitService.portfolioAllocations.and.returnValue(of([allocation]));
    pursuitService.portfolioExecutionProposals.and.returnValue(of([proposal]));

    component.openPortfolioPlanner();

    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id])
      .toContain('immutable owner or proposal-only boundaries');
  });

  it('shows a retryable allocation history error without inventing records', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.portfolioAllocations.and.returnValue(throwError(() => ({ error: { error: 'History storage unavailable.' } })));

    component.openPortfolioPlanner();

    expect(component.portfolioAllocationHistoryLoading).toBeFalse();
    expect(component.portfolioAllocationHistoryLoaded).toBeFalse();
    expect(component.portfolioAllocationHistory).toEqual([]);
    expect(component.portfolioAllocationHistoryError).toBe('History storage unavailable.');
  });

  it('fails closed when allocation history claims non-allocation authority', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.portfolioAllocations.and.returnValue(of([{
      allocation: null,
      items: [],
      replayed: false,
      authority: 'execution_authority',
      canExecute: true,
    }]));

    component.openPortfolioPlanner();

    expect(component.portfolioAllocationHistoryLoaded).toBeFalse();
    expect(component.portfolioAllocationHistory).toEqual([]);
    expect(component.portfolioAllocationHistoryError).toContain('violated its read-only authority');
  });

  it('fails closed when allocation history claims execution capability', () => {
    const pursuitService = (component as any).pursuitsService;
    pursuitService.portfolioAllocations.and.returnValue(of([{
      allocation: null,
      items: [],
      replayed: false,
      authority: 'allocation_only',
      canExecute: true,
    }]));

    component.openPortfolioPlanner();

    expect(component.portfolioAllocationHistoryLoaded).toBeFalse();
    expect(component.portfolioAllocationHistory).toEqual([]);
    expect(component.portfolioAllocationHistoryError).toContain('violated its read-only authority');
  });

  it('prepares immutable execution proposals only after deliberate confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposalResult = portfolioExecutionProposalFixture(allocation);
    component.portfolioAllocationHistory = [allocation];
    pursuitService.preparePortfolioExecutionProposals = jasmine.createSpy('preparePortfolioExecutionProposals')
      .and.returnValue(of(proposalResult));

    component.preparePortfolioExecutionProposals(allocation);

    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.preparePortfolioExecutionProposals).not.toHaveBeenCalled();
    expect(component.portfolioExecutionProposalPendingId).toBe(allocation.allocation.id);
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('does not approve, queue, or execute');

    confirmation.nzOnOk();

    expect(pursuitService.preparePortfolioExecutionProposals).toHaveBeenCalledOnceWith(allocation.allocation.id, {
      expectedAllocationDigest: allocation.allocation.recordDigest,
      confirmation: 'PREPARE EXECUTION PROPOSALS',
    });
    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toBe(proposalResult as any);
    expect(component.portfolioExecutionProposalPendingId).toBe('');
    expect(component.portfolioExecutionProposalRunningId).toBe('');
    expect(notification.success).toHaveBeenCalledWith(
      'Execution proposals prepared',
      'Immutable proposal evidence is ready for inspection. No work was approved, queued, or executed.',
    );
  });

  it('does not prepare proposals when the allocation changes during confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    component.portfolioAllocationHistory = [allocation];
    pursuitService.preparePortfolioExecutionProposals = jasmine.createSpy('preparePortfolioExecutionProposals');

    component.preparePortfolioExecutionProposals(allocation);
    component.portfolioAllocationHistory = [{
      ...allocation,
      allocation: { ...allocation.allocation, recordDigest: 'e'.repeat(64) },
    }];
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(pursuitService.preparePortfolioExecutionProposals).not.toHaveBeenCalled();
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id]).toContain('changed before proposal preparation');
  });

  it('fails closed when proposal evidence claims execution authority', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const unsafe = {
      ...portfolioExecutionProposalFixture(allocation),
      authority: 'execution_authority',
      canExecute: true,
    };
    component.portfolioAllocationHistory = [allocation];
    pursuitService.preparePortfolioExecutionProposals = jasmine.createSpy('preparePortfolioExecutionProposals')
      .and.returnValue(of(unsafe));

    component.preparePortfolioExecutionProposals(allocation);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id]).toContain('proposal-only authority');
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('fails closed when a proposal item does not match immutable allocation evidence', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const mismatched = portfolioExecutionProposalFixture(allocation);
    mismatched.items[0].reservationId = 'different-reservation';
    component.portfolioAllocationHistory = [allocation];
    pursuitService.preparePortfolioExecutionProposals = jasmine.createSpy('preparePortfolioExecutionProposals')
      .and.returnValue(of(mismatched));

    component.preparePortfolioExecutionProposals(allocation);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioExecutionProposals[allocation.allocation.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalErrors[allocation.allocation.id]).toContain('immutable allocation evidence');
  });

  it('requires a non-empty reason before opening a proposal-item decision confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem');

    component.decidePortfolioExecutionProposalItem(item, 'approved');

    expect(modal.confirm).not.toHaveBeenCalled();
    expect(pursuitService.decidePortfolioExecutionProposalItem).not.toHaveBeenCalled();
    expect(component.portfolioExecutionProposalDecisionErrors[item.id]).toContain('reason is required');
  });

  it('does not offer or submit decisions for blocked proposal items', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = { ...proposal.items[0], status: 'blocked', blockedReasons: ['A required source is missing.'] };
    component.portfolioExecutionProposals[allocation.allocation.id] = { ...proposal, items: [item] };
    component.portfolioExecutionProposalDecisionReasons[item.id] = 'This cannot proceed.';
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem');

    expect(component.canDecidePortfolioExecutionProposalItem(item, 'approved')).toBeFalse();
    component.decidePortfolioExecutionProposalItem(item, 'approved');

    expect(modal.confirm).not.toHaveBeenCalled();
    expect(pursuitService.decidePortfolioExecutionProposalItem).not.toHaveBeenCalled();
  });

  it('records an immutable proposal-item approval only after deliberate confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const reason = 'The cited evidence supports this bounded proposal for a later authorization review.';
    const result = portfolioExecutionProposalDecisionFixture(item, 'approved', reason);
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionReasons[item.id] = reason;
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem')
      .and.returnValue(of(result));

    component.decidePortfolioExecutionProposalItem(item, 'approved');

    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.decidePortfolioExecutionProposalItem).not.toHaveBeenCalled();
    expect(component.portfolioExecutionProposalDecisionPendingId).toBe(item.id);
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('APPROVE EXECUTION PROPOSAL ITEM');
    expect(confirmation.nzContent).toContain('does not queue or execute');

    confirmation.nzOnOk();

    expect(pursuitService.decidePortfolioExecutionProposalItem).toHaveBeenCalledOnceWith(item.id, {
      expectedItemDigest: item.recordDigest,
      decision: 'approved',
      reason,
      confirmation: 'APPROVE EXECUTION PROPOSAL ITEM',
    });
    expect(component.portfolioExecutionProposalDecisionHistory[item.id]).toEqual([result as any]);
    expect(component.portfolioExecutionProposalDecisionReasons[item.id]).toBe('');
    expect(component.portfolioExecutionProposalDecisionPendingId).toBe('');
    expect(component.portfolioExecutionProposalDecisionRunningId).toBe('');
    expect(notification.success).toHaveBeenCalledWith(
      'Immutable decision recorded',
      'The proposal decision was recorded for review only. No work was queued or executed.',
    );
  });

  it('allows revocation only while the latest immutable decision is an approval', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const approval = portfolioExecutionProposalDecisionFixture(item, 'approved', 'Approved for separate authorization review.');
    const revocationReason = 'New evidence requires the approval to be withdrawn.';
    const revocation = portfolioExecutionProposalDecisionFixture(item, 'revoked', revocationReason, '4');
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionReasons[item.id] = revocationReason;
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem')
      .and.returnValue(of(revocation));

    expect(component.canDecidePortfolioExecutionProposalItem(item, 'revoked')).toBeFalse();
    component.portfolioExecutionProposalDecisionHistory[item.id] = [approval];
    expect(component.canDecidePortfolioExecutionProposalItem(item, 'revoked')).toBeTrue();

    component.decidePortfolioExecutionProposalItem(item, 'revoked');
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(pursuitService.decidePortfolioExecutionProposalItem).toHaveBeenCalledOnceWith(item.id, jasmine.objectContaining({
      decision: 'revoked',
      confirmation: 'REVOKE EXECUTION PROPOSAL ITEM',
    }));
    expect(component.portfolioExecutionProposalDecisionHistory[item.id].map((entry) => entry.decision.decision))
      .toEqual(['approved', 'revoked']);
    component.portfolioExecutionProposalDecisionReasons[item.id] = 'Attempt another revocation.';
    expect(component.canDecidePortfolioExecutionProposalItem(item, 'revoked')).toBeFalse();
  });

  it('fails closed when a proposal-item decision claims execution authority', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const reason = 'Review this response authority.';
    const unsafe = {
      ...portfolioExecutionProposalDecisionFixture(item, 'approved', reason),
      authority: 'execution_authority',
      canExecute: true,
    };
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionReasons[item.id] = reason;
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem')
      .and.returnValue(of(unsafe));

    component.decidePortfolioExecutionProposalItem(item, 'approved');
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioExecutionProposalDecisionHistory[item.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalDecisionErrors[item.id]).toContain('non-execution authority');
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('fails closed when a proposal-item decision does not match the item digest', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const reason = 'Review the immutable evidence binding.';
    const mismatched = portfolioExecutionProposalDecisionFixture(item, 'approved', reason);
    mismatched.decision.proposalItemDigest = '9'.repeat(64);
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionReasons[item.id] = reason;
    pursuitService.decidePortfolioExecutionProposalItem = jasmine.createSpy('decidePortfolioExecutionProposalItem')
      .and.returnValue(of(mismatched));

    component.decidePortfolioExecutionProposalItem(item, 'approved');
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioExecutionProposalDecisionHistory[item.id]).toBeUndefined();
    expect(component.portfolioExecutionProposalDecisionErrors[item.id]).toContain('immutable evidence');
  });

  it('loads governed dispatch eligibility with every item unselected by default', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    pursuitService.portfolioDispatchCoordination = jasmine.createSpy('portfolioDispatchCoordination')
      .and.returnValue(of(coordination));

    component.loadPortfolioDispatchCoordination(proposal);

    expect(pursuitService.portfolioDispatchCoordination).toHaveBeenCalledOnceWith(proposal.proposal.id);
    expect(component.portfolioDispatchCoordination[proposal.proposal.id]).toBe(coordination);
    expect(component.portfolioDispatchSelections[proposal.proposal.id]).toEqual({ [proposal.items[0].id]: false });
    expect(component.portfolioDispatchSelectedCount(proposal.proposal.id)).toBe(0);
  });

  it('clears stale actionable coordination before a failed refresh', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    component.portfolioDispatchCoordination[proposal.proposal.id] = coordination;
    component.portfolioDispatchSelections[proposal.proposal.id] = { [proposal.items[0].id]: true };
    component.portfolioDispatchConfirmations[proposal.proposal.id] = 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
    pursuitService.portfolioDispatchCoordination = jasmine.createSpy('portfolioDispatchCoordination')
      .and.returnValue(throwError(() => ({ error: { error: 'Current approval read failed.' } })));

    component.loadPortfolioDispatchCoordination(proposal);

    expect(component.portfolioDispatchCoordination[proposal.proposal.id]).toBeUndefined();
    expect(component.portfolioDispatchSelections[proposal.proposal.id]).toEqual({});
    expect(component.portfolioDispatchConfirmations[proposal.proposal.id]).toBe('');
    expect(component.portfolioDispatchCoordinationErrors[proposal.proposal.id]).toBe('Current approval read failed.');
  });

  it('rejects duplicate coordination items and mismatched counters', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    pursuitService.portfolioDispatchCoordination = jasmine.createSpy('portfolioDispatchCoordination')
      .and.returnValue(of({
        ...coordination,
        items: [coordination.items[0], coordination.items[0]],
        eligible: 2,
      }));

    component.loadPortfolioDispatchCoordination(proposal);

    expect(component.portfolioDispatchCoordination[proposal.proposal.id]).toBeUndefined();
    expect(component.portfolioDispatchCoordinationErrors[proposal.proposal.id])
      .toContain('did not match the immutable proposal');
  });

  it('dispatches only an explicitly selected approved item after the exact phrase and confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    const result = portfolioDispatchResultFixture(proposal, coordination);
    component.portfolioDispatchCoordination[proposal.proposal.id] = coordination;
    component.portfolioDispatchSelections[proposal.proposal.id] = { [proposal.items[0].id]: true };
    pursuitService.dispatchPortfolioWorkflows = jasmine.createSpy('dispatchPortfolioWorkflows').and.returnValue(of(result));
    pursuitService.portfolioDispatchCoordination = jasmine.createSpy('portfolioDispatchCoordination')
      .and.returnValue(of({
        ...coordination,
        eligible: 0,
        dispatched: 1,
        items: [{ ...coordination.items[0], eligibility: 'dispatched', selectable: false, latestDispatch: result.items[0] }],
      }));

    component.dispatchPortfolioWorkflows(proposal);
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.portfolioDispatchErrors[proposal.proposal.id]).toContain('exact dispatch phrase');

    component.portfolioDispatchConfirmations[proposal.proposal.id] = 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
    component.dispatchPortfolioWorkflows(proposal);
    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.dispatchPortfolioWorkflows).not.toHaveBeenCalled();
    expect(modal.confirm.calls.mostRecent().args[0].nzContent).toContain('will not run workflows');
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(pursuitService.dispatchPortfolioWorkflows).toHaveBeenCalledOnceWith(proposal.proposal.id, {
      expectedProposalDigest: proposal.proposal.recordDigest,
      items: [{
        proposalItemId: proposal.items[0].id,
        expectedItemDigest: proposal.items[0].recordDigest,
        expectedDecisionDigest: coordination.items[0].decision.recordDigest,
      }],
      confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS',
    });
    expect(component.portfolioDispatchResults[proposal.proposal.id]).toBe(result);
    expect(notification.success).toHaveBeenCalledWith(
      'Review-gated workflows created',
      '1 created, 0 recovered, 0 need review, 0 failed. No workflow was run.',
    );
  });

  it('rejects dispatch summaries whose counters do not match item outcomes', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    const malformed = { ...portfolioDispatchResultFixture(proposal, coordination), created: 0, replayed: 1 };
    component.portfolioDispatchCoordination[proposal.proposal.id] = coordination;
    component.portfolioDispatchSelections[proposal.proposal.id] = { [proposal.items[0].id]: true };
    component.portfolioDispatchConfirmations[proposal.proposal.id] = 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
    pursuitService.dispatchPortfolioWorkflows = jasmine.createSpy('dispatchPortfolioWorkflows').and.returnValue(of(malformed));

    component.dispatchPortfolioWorkflows(proposal);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioDispatchResults[proposal.proposal.id]).toBeUndefined();
    expect(component.portfolioDispatchErrors[proposal.proposal.id]).toContain('did not match the exact selected items');
  });

  it('rejects duplicate dispatch item and run selections that omit another selected item', () => {
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const first = proposal.items[0];
    const second = {
      ...first,
      id: 'proposal-item-2',
      allocationItemId: 'allocation-item-2',
      pursuitId: 'pursuit-2',
      reservationId: 'reservation-2',
      stateDigest: '4'.repeat(64),
      recordDigest: '5'.repeat(64),
    };
    proposal.items = [first, second];
    const coordination = portfolioDispatchCoordinationFixture(proposal);
    const result = portfolioDispatchResultFixture(proposal, coordination);
    const secondResult = {
      ...result.items[0],
      id: 'f7788803-346a-4713-8d3b-e6da77de6a0d',
      proposalItemId: second.id,
      proposalItemDigest: second.recordDigest,
      approvalDecisionId: 'd7788803-346a-4713-8d3b-e6da77de6a0d',
      authorizationReceiptId: 'a8888803-346a-4713-8d3b-e6da77de6a0d',
      workflowId: 'b3388803-346a-4713-8d3b-e6da77de6a0d',
      recordDigest: '6'.repeat(64),
    };
    const contract = {
      proposalId: proposal.proposal.id,
      proposalDigest: proposal.proposal.recordDigest,
      items: [
        { proposalItemId: first.id, expectedItemDigest: first.recordDigest },
        { proposalItemId: second.id, expectedItemDigest: second.recordDigest },
      ],
    };
    const valid = {
      ...result,
      run: { ...result.run, selectedItemIds: [first.id, second.id] },
      items: [result.items[0], secondResult],
      created: 2,
    };

    expect((component as any).validPortfolioDispatchResult(valid, contract, proposal)).toBeTrue();
    expect((component as any).validPortfolioDispatchResult({
      ...valid,
      run: { ...valid.run, selectedItemIds: [first.id, first.id] },
    }, contract, proposal)).toBeFalse();
    expect((component as any).validPortfolioDispatchResult({
      ...valid,
      items: [valid.items[0], { ...valid.items[0], id: secondResult.id }],
    }, contract, proposal)).toBeFalse();
  });

  it('refreshes verification for multiple batch workflows independently', () => {
    const workflowService = (component as any).workflowService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const first = proposal.items[0];
    const second = { ...first, id: 'proposal-item-2', recordDigest: '4'.repeat(64) };
    const firstWorkflow = 'a2288803-346a-4713-8d3b-e6da77de6a0d';
    const secondWorkflow = 'b3388803-346a-4713-8d3b-e6da77de6a0d';
    const responses: Record<string, Subject<any>> = {
      [firstWorkflow]: new Subject<any>(),
      [secondWorkflow]: new Subject<any>(),
    };
    component.portfolioDispatchResults[proposal.proposal.id] = {
      items: [
        { proposalItemId: first.id, workflowId: firstWorkflow },
        { proposalItemId: second.id, workflowId: secondWorkflow },
      ],
    } as any;
    workflowService.get.and.callFake((workflowId: string) => responses[workflowId].asObservable());

    component.refreshPortfolioWorkflowVerification(first);
    component.refreshPortfolioWorkflowVerification(second);

    expect(workflowService.get.calls.allArgs()).toEqual([[firstWorkflow], [secondWorkflow]]);
    expect(component.portfolioWorkflowVerificationLoading[first.id]).toBeTrue();
    expect(component.portfolioWorkflowVerificationLoading[second.id]).toBeTrue();
    responses[firstWorkflow].next(portfolioWorkflowRecordFixture(firstWorkflow, 'needs_approval', 'unverified'));
    responses[secondWorkflow].next(portfolioWorkflowRecordFixture(secondWorkflow, 'needs_approval', 'unverified'));
    expect(component.portfolioWorkflowVerificationLoading[first.id]).toBeUndefined();
    expect(component.portfolioWorkflowVerificationLoading[second.id]).toBeUndefined();
  });

  it('evaluates an exact fixed workflow effect only after current approval and typed confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const approval = portfolioExecutionProposalDecisionFixture(
      item,
      'approved',
      'Approved for a separate exact-effect policy evaluation.',
    );
    approval.decision.expiresAt = new Date(Date.now() + 30 * 60_000).toISOString();
    const authorization = portfolioWorkflowAuthorizationFixture(item, approval.decision.id);
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionHistory[item.id] = [approval];
    pursuitService.authorizePortfolioWorkflowEffect = jasmine.createSpy('authorizePortfolioWorkflowEffect')
      .and.returnValue(of(authorization));

    component.authorizePortfolioWorkflowEffect(item);
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.portfolioWorkflowAuthorizationErrors[item.id]).toContain('exact authorization phrase');

    component.portfolioWorkflowAuthorizationConfirmations[item.id] = 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT';
    component.authorizePortfolioWorkflowEffect(item);
    expect(modal.confirm).toHaveBeenCalledTimes(1);
    expect(pursuitService.authorizePortfolioWorkflowEffect).not.toHaveBeenCalled();
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('workflow.intake');
    expect(confirmation.nzContent).toContain('unconsumed policy receipt');

    confirmation.nzOnOk();

    expect(pursuitService.authorizePortfolioWorkflowEffect).toHaveBeenCalledOnceWith(item.id, {
      expectedItemDigest: item.recordDigest,
      expectedDecisionDigest: approval.decision.recordDigest,
      confirmation: 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT',
    });
    expect(component.portfolioWorkflowAuthorizationResults[item.id]).toBe(authorization);
    expect(component.portfolioWorkflowAuthorizationConfirmations[item.id]).toBe('');
    expect(component.portfolioWorkflowAuthorizationRunningId).toBe('');
    expect(notification.success).toHaveBeenCalledWith(
      'Exact effect authorized',
      'All policy boundaries passed. No workflow was queued, created, or executed.',
    );
  });

  it('rejects a workflow authorization response that expands the reviewed effect', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const approval = portfolioExecutionProposalDecisionFixture(item, 'approved', 'Review this exact effect.');
    approval.decision.expiresAt = new Date(Date.now() + 30 * 60_000).toISOString();
    const unsafe = portfolioWorkflowAuthorizationFixture(item, approval.decision.id);
    unsafe.effect.runtimeId = 'caller-selected-runtime';
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionHistory[item.id] = [approval];
    component.portfolioWorkflowAuthorizationConfirmations[item.id] = 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT';
    pursuitService.authorizePortfolioWorkflowEffect = jasmine.createSpy('authorizePortfolioWorkflowEffect')
      .and.returnValue(of(unsafe));

    component.authorizePortfolioWorkflowEffect(item);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioWorkflowAuthorizationResults[item.id]).toBeUndefined();
    expect(component.portfolioWorkflowAuthorizationErrors[item.id]).toContain('exact reviewed effect');
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('consumes an authorized receipt only after a second exact confirmation and exposes the workflow', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const approval = portfolioExecutionProposalDecisionFixture(item, 'approved', 'Approve exact local workflow creation.');
    approval.decision.expiresAt = new Date(Date.now() + 30 * 60_000).toISOString();
    const authorization = portfolioWorkflowAuthorizationFixture(item, approval.decision.id);
    const execution = portfolioWorkflowExecutionFixture(item, authorization);
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioExecutionProposalDecisionHistory[item.id] = [approval];
    component.portfolioWorkflowAuthorizationResults[item.id] = authorization;
    pursuitService.executePortfolioWorkflowEffect = jasmine.createSpy('executePortfolioWorkflowEffect')
      .and.returnValue(of(execution));

    component.executePortfolioWorkflowEffect(item);
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.portfolioWorkflowExecutionErrors[item.id]).toContain('exact workflow creation phrase');

    component.portfolioWorkflowExecutionConfirmations[item.id] = 'CREATE APPROVED PORTFOLIO WORKFLOW';
    component.executePortfolioWorkflowEffect(item);
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('consume this exact receipt once');
    expect(pursuitService.executePortfolioWorkflowEffect).not.toHaveBeenCalled();
    confirmation.nzOnOk();

    expect(pursuitService.executePortfolioWorkflowEffect).toHaveBeenCalledOnceWith(item.id, {
      authorizationReceiptId: authorization.receipt.id,
      expectedItemDigest: item.recordDigest,
      expectedDecisionDigest: approval.decision.recordDigest,
      confirmation: 'CREATE APPROVED PORTFOLIO WORKFLOW',
    });
    expect(component.portfolioWorkflowExecutionResults[item.id]).toBe(execution);
    expect(component.portfolioWorkflowExecutionConfirmations[item.id]).toBe('');
    expect(notification.success).toHaveBeenCalledWith(
      'Workflow created',
      'The receipt is consumed and the local workflow is review-gated. No workflow execution or external action occurred.',
    );
  });

  it('unlocks settlement only for the linked workflow after verified completion', () => {
    const pursuitService = (component as any).pursuitsService;
    const workflowService = (component as any).workflowService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const execution = portfolioWorkflowExecutionFixture(
      item,
      portfolioWorkflowAuthorizationFixture(item, 'decision-approved'),
    );
    const verified = portfolioWorkflowRecordFixture(execution.workflowId, 'completed', 'verified');
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioWorkflowExecutionResults[item.id] = execution;
    workflowService.get.and.returnValue(of(verified));

    component.refreshPortfolioWorkflowVerification(item);

    expect(workflowService.get).toHaveBeenCalledOnceWith(execution.workflowId);
    expect(component.portfolioVerifiedWorkflow(item.id)).toBe(verified);

    component.portfolioWorkflowRecords[item.id] = portfolioWorkflowRecordFixture(
      execution.workflowId,
      'completed',
      'uncertain',
    );
    expect(component.portfolioVerifiedWorkflow(item.id)).toBeUndefined();
  });

  it('settles a verified workflow only after measured usage and exact deliberate confirmation', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const execution = portfolioWorkflowExecutionFixture(
      item,
      portfolioWorkflowAuthorizationFixture(item, 'decision-approved'),
    );
    const verified = portfolioWorkflowRecordFixture(execution.workflowId, 'completed', 'verified');
    const settlement = portfolioWorkflowSettlementFixture(item, execution, verified);
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioWorkflowExecutionResults[item.id] = execution;
    component.portfolioWorkflowRecords[item.id] = verified;
    component.portfolioWorkflowSettlementEffortMinutes[item.id] = 42;
    component.portfolioWorkflowSettlementCostMicros[item.id] = 125000;
    pursuitService.settlePortfolioWorkflow = jasmine.createSpy('settlePortfolioWorkflow').and.returnValue(of(settlement));

    component.settlePortfolioWorkflow(item);
    expect(modal.confirm).not.toHaveBeenCalled();
    expect(component.portfolioWorkflowSettlementErrors[item.id]).toContain('exact settlement phrase');

    component.portfolioWorkflowSettlementConfirmations[item.id] = 'SETTLE VERIFIED PORTFOLIO WORK';
    component.settlePortfolioWorkflow(item);
    const confirmation = modal.confirm.calls.mostRecent().args[0];
    expect(confirmation.nzContent).toContain('42 actual minute(s)');
    expect(confirmation.nzContent).toContain('closes accounting only');
    expect(pursuitService.settlePortfolioWorkflow).not.toHaveBeenCalled();

    confirmation.nzOnOk();

    expect(pursuitService.settlePortfolioWorkflow).toHaveBeenCalledOnceWith(item.id, {
      workflowId: execution.workflowId,
      expectedItemDigest: item.recordDigest,
      actualEffortMinutes: 42,
      actualCostMicros: 125000,
      confirmation: 'SETTLE VERIFIED PORTFOLIO WORK',
    });
    expect(component.portfolioWorkflowSettlementResults[item.id]).toBe(settlement);
    expect(component.portfolioWorkflowSettlementConfirmations[item.id]).toBe('');
    expect(notification.success).toHaveBeenCalledWith(
      'Reservation settled',
      'Verified usage and review-only learning evidence are recorded. Neither can execute or repeat workflow work.',
    );
  });

  it('fails closed when a settlement response expands accounting-only authority', () => {
    const pursuitService = (component as any).pursuitsService;
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const execution = portfolioWorkflowExecutionFixture(
      item,
      portfolioWorkflowAuthorizationFixture(item, 'decision-approved'),
    );
    const verified = portfolioWorkflowRecordFixture(execution.workflowId, 'completed', 'verified');
    const unsafe = portfolioWorkflowSettlementFixture(item, execution, verified);
    unsafe.authority = 'workflow_effect_executed';
    unsafe.canExecute = true;
    component.portfolioExecutionProposals[allocation.allocation.id] = proposal;
    component.portfolioWorkflowExecutionResults[item.id] = execution;
    component.portfolioWorkflowRecords[item.id] = verified;
    component.portfolioWorkflowSettlementEffortMinutes[item.id] = 15;
    component.portfolioWorkflowSettlementCostMicros[item.id] = 0;
    component.portfolioWorkflowSettlementConfirmations[item.id] = 'SETTLE VERIFIED PORTFOLIO WORK';
    pursuitService.settlePortfolioWorkflow = jasmine.createSpy('settlePortfolioWorkflow').and.returnValue(of(unsafe));

    component.settlePortfolioWorkflow(item);
    modal.confirm.calls.mostRecent().args[0].nzOnOk();

    expect(component.portfolioWorkflowSettlementResults[item.id]).toBeUndefined();
    expect(component.portfolioWorkflowSettlementErrors[item.id]).toContain('did not match');
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('validates monitoring and changed-review calibration lifecycle responses', () => {
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const execution = portfolioWorkflowExecutionFixture(
      item,
      portfolioWorkflowAuthorizationFixture(item, 'decision-approved'),
    );
    const workflow = portfolioWorkflowRecordFixture(execution.workflowId, 'completed', 'verified');
    const contract = {
      pursuitId: item.pursuitId,
      reservationId: item.reservationId,
      workflowId: execution.workflowId,
      taskPlanId: workflow.item.lastTaskPlanId,
      verificationStatus: workflow.item.verificationStatus,
      actualEffortMinutes: 42,
      actualCostMicros: 125000,
    };
    const monitoring = portfolioWorkflowSettlementFixture(item, execution, workflow);
    monitoring.learningProposalStatus = 'monitoring';
    monitoring.learningSampleCount = 2;
    monitoring.learningNewEvidenceCount = 2;

    expect((component as any).validPortfolioWorkflowSettlementResult(
      monitoring, item, workflow, contract,
    )).toBeTrue();

    const changedReview = {
      ...monitoring,
      learningProposalStatus: 'changes_requested',
      learningProposalId: 'learning-proposal-2',
      learningSampleCount: 3,
      learningNewEvidenceCount: 3,
      learningDriftDetected: true,
      learningReviewRequired: true,
    };
    expect((component as any).validPortfolioWorkflowSettlementResult(
      changedReview, item, workflow, contract,
    )).toBeTrue();
  });

  it('rejects malformed calibration evidence counters and drift state', () => {
    const allocation = portfolioAllocationFixture();
    const proposal = portfolioExecutionProposalFixture(allocation);
    const item = proposal.items[0];
    const execution = portfolioWorkflowExecutionFixture(
      item,
      portfolioWorkflowAuthorizationFixture(item, 'decision-approved'),
    );
    const workflow = portfolioWorkflowRecordFixture(execution.workflowId, 'completed', 'verified');
    const contract = {
      pursuitId: item.pursuitId,
      reservationId: item.reservationId,
      workflowId: execution.workflowId,
      taskPlanId: workflow.item.lastTaskPlanId,
      verificationStatus: workflow.item.verificationStatus,
      actualEffortMinutes: 42,
      actualCostMicros: 125000,
    };
    const negativeEvidence = portfolioWorkflowSettlementFixture(item, execution, workflow);
    negativeEvidence.learningNewEvidenceCount = -1;
    const malformedDrift = portfolioWorkflowSettlementFixture(item, execution, workflow);
    malformedDrift.learningDriftDetected = 'false';

    expect((component as any).validPortfolioWorkflowSettlementResult(
      negativeEvidence, item, workflow, contract,
    )).toBeFalse();
    expect((component as any).validPortfolioWorkflowSettlementResult(
      malformedDrift, item, workflow, contract,
    )).toBeFalse();
  });
});

function portfolioAllocationFixture(): any {
  return {
    allocation: {
      id: 'allocation-proposal-1',
      planId: 'portfolio-proposal',
      requestDigest: 'a'.repeat(64),
      decisionDigest: 'b'.repeat(64),
      status: 'accepted_needs_approval',
      durationMode: 'conservative',
      horizonStart: '2026-08-05T09:00:00.000Z',
      horizonEnd: '2026-08-05T17:00:00.000Z',
      actor: 'owner@example.com',
      confirmation: 'ACCEPT PORTFOLIO ALLOCATION',
      recordDigest: 'c'.repeat(64),
      acceptedAt: '2026-08-04T09:00:00.000Z',
    },
    items: [{
      id: 'allocation-item-1',
      allocationId: 'allocation-proposal-1',
      pursuitId: 'pursuit-1',
      scheduledStart: '2026-08-05T09:00:00.000Z',
      scheduledEnd: '2026-08-05T11:00:00.000Z',
      durationMinutes: 120,
      estimatedCostMicros: 0,
      requiresApproval: true,
      approvalReasons: ['High-risk external action requires owner approval.'],
      reservationId: 'reservation-1',
      recordDigest: 'd'.repeat(64),
      createdAt: '2026-08-04T09:00:00.000Z',
    }],
    replayed: false,
    authority: 'allocation_only',
    canExecute: false,
  };
}

function portfolioExecutionProposalFixture(allocation: any): any {
  const preparedAt = '2026-08-04T10:00:00.000Z';
  return {
    proposal: {
      id: 'proposal-1',
      allocationId: allocation.allocation.id,
      allocationRecordDigest: allocation.allocation.recordDigest,
      snapshotDigest: 'e'.repeat(64),
      status: 'prepared_needs_approval',
      actor: 'owner@example.com',
      confirmation: 'PREPARE EXECUTION PROPOSALS',
      authority: 'proposal_only',
      recordDigest: 'f'.repeat(64),
      preparedAt,
    },
    items: [{
      id: 'proposal-item-1',
      proposalId: 'proposal-1',
      allocationItemId: allocation.items[0].id,
      pursuitId: allocation.items[0].pursuitId,
      reservationId: allocation.items[0].reservationId,
      actionSummary: 'Prepare the source-grounded draft for separate owner review.',
      pursuitStatus: 'active',
      riskLevel: 'high',
      autonomyLevel: 'approve_before_execute',
      status: 'needs_approval',
      requiresApproval: true,
      approvalReasons: ['High-risk external action requires owner approval.'],
      blockedReasons: [],
      allocationItemDigest: allocation.items[0].recordDigest,
      stateDigest: '1'.repeat(64),
      recordDigest: '2'.repeat(64),
      preparedAt,
    }],
    replayed: false,
    authority: 'proposal_only',
    canExecute: false,
    freshness: {
      status: 'prepared_snapshot',
      revalidationRequired: true,
      checkedAt: preparedAt,
      reason: 'Proposal evidence requires separate eligibility revalidation.',
    },
  };
}

function portfolioExecutionProposalDecisionFixture(
  item: any,
  decision: 'approved' | 'rejected' | 'needs_clarification' | 'revoked',
  reason: string,
  digestSeed: string = '3',
): any {
  const confirmations = {
    approved: 'APPROVE EXECUTION PROPOSAL ITEM',
    rejected: 'REJECT EXECUTION PROPOSAL ITEM',
    needs_clarification: 'REQUEST CLARIFICATION FOR EXECUTION PROPOSAL ITEM',
    revoked: 'REVOKE EXECUTION PROPOSAL ITEM',
  } as const;
  return {
    decision: {
      id: `decision-${decision}`,
      proposalItemId: item.id,
      proposalId: item.proposalId,
      pursuitId: item.pursuitId,
      decision,
      reason,
      actor: 'owner@example.com',
      confirmation: confirmations[decision],
      proposalItemDigest: item.recordDigest,
      stateDigest: item.stateDigest,
      authority: 'approval_decision_only',
      requestDigest: (decision === 'revoked' ? '6' : '5').repeat(64),
      recordDigest: digestSeed.repeat(64),
      previousDecisionId: decision === 'revoked' ? 'decision-approved' : undefined,
      decidedAt: '2026-08-04T10:05:00.000Z',
      expiresAt: decision === 'approved' ? '2026-08-04T10:35:00.000Z' : undefined,
    },
    replayed: false,
    authority: 'approval_decision_only',
    canExecute: false,
  };
}

function portfolioDispatchCoordinationFixture(proposal: any): any {
  const decision = portfolioExecutionProposalDecisionFixture(
    proposal.items[0],
    'approved',
    'Approve this exact item for governed portfolio coordination.',
  ).decision;
  decision.id = 'd4488803-346a-4713-8d3b-e6da77de6a0d';
  decision.expiresAt = new Date(Date.now() + 30 * 60_000).toISOString();
  return {
    proposal: proposal.proposal,
    items: [{
      item: proposal.items[0],
      eligibility: 'eligible',
      selectable: true,
      reason: 'Current owner approval and immutable evidence are valid.',
      decision,
    }],
    dispatchRuns: [],
    eligible: 1,
    needsApproval: 0,
    blocked: 0,
    stale: 0,
    dispatched: 0,
    authority: 'coordination_preview_only',
    canExecute: false,
    freshness: {
      status: 'current_coordination_snapshot',
      revalidationRequired: true,
      checkedAt: new Date().toISOString(),
      reason: 'Dispatch independently revalidates every selected approval and immutable binding.',
    },
  };
}

function portfolioDispatchResultFixture(proposal: any, coordination: any): any {
  const runId = 'e5588803-346a-4713-8d3b-e6da77de6a0d';
  const item = proposal.items[0];
  return {
    run: {
      id: runId,
      proposalId: proposal.proposal.id,
      proposalDigest: proposal.proposal.recordDigest,
      selectedItemIds: [item.id],
      selectedItemsDigest: 'a'.repeat(64),
      actor: 'owner@example.com',
      confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS',
      requestDigest: 'b'.repeat(64),
      recordDigest: 'c'.repeat(64),
      requestedAt: new Date().toISOString(),
    },
    items: [{
      id: 'f6688803-346a-4713-8d3b-e6da77de6a0d',
      dispatchRunId: runId,
      proposalId: proposal.proposal.id,
      proposalItemId: item.id,
      proposalItemDigest: item.recordDigest,
      approvalDecisionId: coordination.items[0].decision.id,
      approvalDecisionDigest: coordination.items[0].decision.recordDigest,
      authorizationReceiptId: 'a7788803-346a-4713-8d3b-e6da77de6a0d',
      workflowId: 'a2288803-346a-4713-8d3b-e6da77de6a0d',
      workflowState: 'needs_approval',
      outcome: 'workflow_created',
      message: 'Created one receipt-bound review-gated local workflow.',
      attemptNumber: 1,
      replayed: false,
      recordDigest: 'd'.repeat(64),
      attemptedAt: new Date().toISOString(),
    }],
    status: 'workflows_created',
    resumed: false,
    created: 1,
    replayed: 0,
    needsReview: 0,
    failed: 0,
    authority: 'portfolio_dispatch_result',
    canExecute: false,
  };
}

function portfolioWorkflowAuthorizationFixture(item: any, decisionId: string): any {
  const effectDigest = '7'.repeat(64);
  return {
    effect: {
      action: 'pursuit.portfolio.create-workflow',
      stage: 'execution',
      resourceType: 'workflow-intake',
      resourceId: item.id,
      projectKey: 'portfolio-project',
      domain: 'operations',
      toolId: 'workflow.intake',
      runtimeId: 'hai-workflow-engine',
      risk: 'high',
      reversible: true,
      estimatedCostMicros: 0,
      actionSummary: item.actionSummary,
      effectDigest,
      approvalSourceId: `portfolio-decision:${decisionId}`,
    },
    receipt: {
      id: '31f97f38-181d-4aee-bf8f-36a3cf9d1576',
      contractVersion: 1,
      ownerIdentity: 'owner@example.com',
      actorIdentity: 'owner@example.com',
      actorKind: 'human',
      taskId: `portfolio-item:${item.id}`,
      action: 'pursuit.portfolio.create-workflow',
      stage: 'execution',
      resourceType: 'workflow-intake',
      resourceId: item.id,
      projectKey: 'portfolio-project',
      domain: 'operations',
      runtimeId: 'hai-workflow-engine',
      approvalSourceId: `portfolio-decision:${decisionId}`,
      effectDigest,
      outcome: 'authorized',
      reason: 'All policy boundaries passed.',
      requestDigest: '8'.repeat(64),
      decisionDigest: '9'.repeat(64),
      requiredAuthority: 1,
      requestedAutonomy: 6,
      effectiveAutonomy: 6,
      risk: 'high',
      reversible: true,
      estimatedCostEur: 0,
      notificationRequired: false,
      evaluatedAt: new Date().toISOString(),
    },
    authority: 'execution_authorization_only',
    canExecute: false,
  };
}

function portfolioWorkflowExecutionFixture(item: any, authorization: any): any {
  return {
    effect: authorization.effect,
    receipt: authorization.receipt,
    consumption: {
      receiptId: authorization.receipt.id,
      ownerIdentity: authorization.receipt.ownerIdentity,
      consumer: 'pursuit-portfolio-workflow',
      executionTarget: `workflow-intake:${authorization.effect.effectDigest}`,
      receiptDigest: authorization.receipt.decisionDigest,
      consumedAt: new Date().toISOString(),
    },
    pursuitId: item.pursuitId,
    workflowId: 'a2288803-346a-4713-8d3b-e6da77de6a0d',
    workflowState: 'needs_approval',
    replayed: false,
    authority: 'workflow_effect_executed',
    canExecute: false,
  };
}

function portfolioWorkflowRecordFixture(workflowId: string, state: string, verificationStatus: string): any {
  return {
    item: {
      id: workflowId,
      currentState: state,
      verificationStatus,
      completedAt: state === 'completed' ? '2026-08-04T11:00:00.000Z' : undefined,
      lastTaskPlanId: state === 'completed' ? 'plan-verified-1' : undefined,
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
    frameworkSelections: [],
  };
}

function portfolioWorkflowSettlementFixture(item: any, execution: any, workflow: any): any {
  const attestationId = '6bb79c35-fca6-49cb-9c63-e290b5d03a85';
  return {
    pursuitId: item.pursuitId,
    proposalItemId: item.id,
    reservationId: item.reservationId,
    workflowId: execution.workflowId,
    disposition: 'consumed',
    actualEffortMinutes: 42,
    actualCostMicros: 125000,
    verificationStatus: workflow.item.verificationStatus,
    evidenceUri: `hai://workflow-completion-attestations/${attestationId}`,
    completionAttestationId: attestationId,
    completionAttestationDigest: 'a'.repeat(64),
    settlementProofId: '344a4bc9-7039-4c31-99c7-1ac83bf6da2b',
    settlementProofDigest: 'b'.repeat(64),
    learningOutcomeId: 'learning-outcome-1',
    learningStatus: 'evidence_recorded',
    learningProposalStatus: 'insufficient_evidence',
    learningSampleCount: 1,
    learningNewEvidenceCount: 1,
    learningDriftDetected: false,
    learningReviewRequired: false,
    replayed: false,
    authority: 'verified_accounting_only',
    canExecute: false,
    resourceUsage: {
      state: 'within_limits',
      available: true,
      limitsConfigured: true,
      effortRecordedHours: 0.7,
      effortReservedHours: 0,
      effortCommittedHours: 0.7,
      effortLimitHours: 8,
      effortRemainingHours: 7.3,
      effortExhausted: false,
      effortExceeded: false,
      spendIncurredEur: 0.125,
      spendRefundedEur: 0,
      spendNetEur: 0.125,
      spendReservedEur: 0,
      spendCommittedEur: 0.125,
      spendLimitEur: 1,
      spendRemainingEur: 0.875,
      spendExhausted: false,
      spendExceeded: false,
      eventCount: 2,
      activeReservations: 0,
      reservations: [],
    },
  };
}

describe('PursuitsComponent rendered operational states', () => {
  let fixture: ComponentFixture<PursuitsComponent>;
  let pursuitService: jasmine.SpyObj<any>;
  let routeParams: BehaviorSubject<ReturnType<typeof convertToParamMap>>;

  const emptyDashboard = () => ({
    counts: {},
    pursuits: [],
    needsRobert: [],
    blocked: [],
    stale: [],
    reviewDue: [],
    planningNeeded: [],
    completionCandidates: [],
    decisionQueue: [],
  });

  beforeEach(async () => {
    localStorage.removeItem('hai.module-view.v1.pursuits');
    pursuitService = jasmine.createSpyObj('PursuitService', ['dashboard', 'list', 'get', 'projectDossier']);
    pursuitService.projectDossier.and.callFake((projectKey: string) => of(projectDossierFixture(projectKey)));
    routeParams = new BehaviorSubject(convertToParamMap({}));

    await TestBed.configureTestingModule({
      imports: [RouterTestingModule, PursuitsModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: PursuitService, useValue: pursuitService },
        { provide: AutomationsService, useValue: {} },
        { provide: WorkflowService, useValue: {} },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('notifications', ['info', 'success', 'error', 'warning']) },
        { provide: NzModalService, useValue: jasmine.createSpyObj('modal', ['confirm']) },
        {
          provide: ActivatedRoute,
          useValue: {
            snapshot: { queryParamMap: convertToParamMap({}) },
            queryParamMap: routeParams.asObservable(),
          },
        },
      ],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem('hai.module-view.v1.pursuits');
  });

  it('shows an accessible initial loading state while the dashboard request is pending', () => {
    const pending = new Subject<any>();
    pursuitService.dashboard.and.returnValue(pending.asObservable());
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const loading = page.querySelector('[data-testid="pursuits-initial-loading"]');
    expect(loading?.getAttribute('role')).toBe('status');
    expect(loading?.getAttribute('aria-busy')).toBe('true');
    expect(page.querySelector('.pursuits-empty')).toBeNull();
  });

  it('opens the create form from a create-intent link and consumes that intent from the URL', () => {
    routeParams.next(convertToParamMap({ create: 'true' }));
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [] }));
    const router = TestBed.inject(Router);
    spyOn(router, 'navigate').and.returnValue(Promise.resolve(true));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('[data-testid="pursuit-create-form"]')).not.toBeNull();
    expect(router.navigate).toHaveBeenCalledWith([], {
      relativeTo: TestBed.inject(ActivatedRoute),
      queryParams: { create: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });

    (page.querySelector('.form-actions button[type="button"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    routeParams.next(convertToParamMap({ create: 'true', selected: 'not-yet-loaded' }));
    fixture.detectChanges();
    expect(page.querySelector('[data-testid="pursuit-create-form"]')).toBeNull();
  });

  it('recovers from a dashboard error and exposes the empty-state create action', () => {
    pursuitService.dashboard.and.returnValues(
      throwError(() => ({ error: { error: 'Dashboard temporarily unavailable' } })),
      of(emptyDashboard()),
    );
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('[role="alert"]')?.textContent).toContain('Dashboard temporarily unavailable');
    const retry = page.querySelector('.pursuits-load-error button') as HTMLButtonElement;
    expect(retry.type).toBe('button');
    retry.focus();
    expect(document.activeElement).toBe(retry);
    retry.click();
    fixture.detectChanges();

    expect(page.querySelector('.pursuits-load-error')).toBeNull();
    expect(page.querySelector('.pursuits-empty')?.textContent).toContain('Start with one outcome');
    const create = page.querySelector('.pursuits-empty button') as HTMLButtonElement;
    expect(create.type).toBe('button');
    create.click();
    fixture.detectChanges();
    expect(page.querySelector('[data-testid="pursuit-create-form"]')).not.toBeNull();
  });

  it('retries a selected pursuit detail request and clears the error after recovery', () => {
    const pursuitId = 'selected-pursuit';
    const pursuit = {
      id: pursuitId,
      title: 'Prepare the hearing evidence',
      status: 'active',
      riskLevel: 'low',
      priorityScore: 40,
      projectKey: 'hearing',
      domain: 'legal',
      desiredOutcome: 'Evidence is ready for review',
    };
    const recoveredDetail = { pursuit: { ...pursuit }, summary: {} } as unknown as IPursuitDetail;
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [pursuit] }));
    pursuitService.get.and.returnValues(
      throwError(() => ({ error: { error: 'private-provider-token' } })),
      of(recoveredDetail),
    );
    routeParams.next(convertToParamMap({ selected: pursuitId }));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.componentInstance.selected = { pursuit: { id: 'previous-pursuit' } } as IPursuitDetail;
    fixture.detectChanges();

    const component = fixture.componentInstance;
    const page: HTMLElement = fixture.nativeElement;
    const detailError = page.querySelector('[data-testid="pursuit-detail-error"]');
    expect(pursuitService.get).toHaveBeenCalledOnceWith(pursuitId);
    expect(detailError?.getAttribute('role')).toBe('alert');
    expect(detailError?.textContent).toContain('HAI could not load the pursuit details. Retry loading this pursuit.');
    expect(detailError?.textContent).not.toContain('private-provider-token');
    expect(component.selected).toBeUndefined();
    expect(page.querySelector('.detail-panel:not(.detail-panel--empty)')).toBeNull();

    (detailError?.querySelector('button') as HTMLButtonElement).click();

    expect(pursuitService.get).toHaveBeenCalledTimes(2);
    expect(pursuitService.get.calls.argsFor(1)).toEqual([pursuitId]);
    expect(component.selected).toBe(recoveredDetail);
    expect(component.detailError).toBe('');
    expect(component.detailLoading).toBeFalse();
  });

  it('shows a compact project summary in Basic and full read-only context in the existing Advanced disclosure', () => {
    const pursuit = { id: 'pursuit-dossier', title: 'Prepare project evidence', status: 'active', projectKey: 'Legal & housing' };
    const detail = normalizedPursuitDetail(pursuit);
    const dossier = projectDossierFixture('Legal & housing', {
      truncated: { workflows: true, memories: false, workflowContext: true, text: true },
    });
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [pursuit] }));
    pursuitService.get.and.returnValue(of(detail));
    pursuitService.projectDossier.and.returnValue(of(dossier));
    routeParams.next(convertToParamMap({ selected: pursuit.id }));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();

    const component = fixture.componentInstance;
    const page: HTMLElement = fixture.nativeElement;
    expect(pursuitService.projectDossier).toHaveBeenCalledOnceWith('Legal & housing');
    expect(page.querySelector('[data-testid="project-dossier-summary"]')?.textContent).toContain('Legal & housing');
    expect(page.querySelector('[data-testid="project-dossier-summary"]')?.textContent).toContain('Workflows');
    expect(page.querySelector('[data-testid="project-dossier-truncated"]')?.textContent).toContain('capped or shortened');
    expect(page.querySelector('[data-testid="project-dossier-advanced"]')).toBeNull();

    TestBed.inject(ModuleViewPreferencesService).setMode('pursuits', 'advanced');
    fixture.detectChanges();
    const recordDisclosure = page.querySelector('#record-audit-trails button') as HTMLButtonElement;
    expect(recordDisclosure).not.toBeNull();
    recordDisclosure.click();
    fixture.detectChanges();
    const dossierDisclosure = page.querySelector('[data-hai-section="evidence-memory-source-context"]') as HTMLDetailsElement;
    expect(dossierDisclosure).not.toBeNull();
    (dossierDisclosure.querySelector('summary') as HTMLElement).click();
    fixture.detectChanges();

    const advanced = page.querySelector('[data-testid="project-dossier-advanced"]') as HTMLElement;
    expect(advanced.textContent).toContain('Prepare the project evidence');
    expect(advanced.textContent).toContain('Collect source records');
    expect(advanced.textContent).toContain('Requested documents');
    expect(advanced.textContent).toContain('A source-backed statement');
    expect(advanced.textContent).toContain('Needs owner review');
    expect(advanced.textContent).toContain('Keep claims source-linked');
    expect(advanced.querySelector('[data-testid="project-dossier-truncation-details"]')?.textContent).toContain('Additional matching workflows were not returned');
    expect(component.projectDossier?.projectKey).toBe(pursuit.projectKey);
  });

  it('shows dossier loading, errors, and retry without retaining prior project data', () => {
    const pursuit = { id: 'pursuit-dossier-error', title: 'Review project', status: 'active', projectKey: 'project-error' };
    const detail = normalizedPursuitDetail(pursuit);
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [pursuit] }));
    pursuitService.get.and.returnValue(of(detail));
    pursuitService.projectDossier.and.returnValues(
      throwError(() => ({ error: { error: 'Project dossier temporarily unavailable.' } })),
      of(projectDossierFixture('project-error')),
    );
    routeParams.next(convertToParamMap({ selected: pursuit.id }));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();

    const page: HTMLElement = fixture.nativeElement;
    const error = page.querySelector('[data-testid="project-dossier-error"]') as HTMLElement;
    expect(error?.getAttribute('role')).toBe('alert');
    expect(error?.textContent).toContain('Project dossier temporarily unavailable.');
    expect(fixture.componentInstance.projectDossier).toBeUndefined();
    (error.querySelector('button') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(pursuitService.projectDossier).toHaveBeenCalledTimes(2);
    expect(page.querySelector('[data-testid="project-dossier-error"]')).toBeNull();
    expect(fixture.componentInstance.projectDossier?.projectKey).toBe('project-error');
  });

  it('does not request a dossier without a project key and distinguishes an empty bounded response', () => {
    const pursuit = { id: 'pursuit-no-key', title: 'Unlinked project', status: 'active' };
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [pursuit] }));
    pursuitService.get.and.returnValue(of(normalizedPursuitDetail(pursuit)));
    routeParams.next(convertToParamMap({ selected: pursuit.id }));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();
    const page: HTMLElement = fixture.nativeElement;
    expect(pursuitService.projectDossier).not.toHaveBeenCalled();
    expect(page.querySelector('[data-testid="project-dossier-no-key"]')?.textContent).toContain('Add a project key');
  });

  it('reports an empty dossier and rejects content returned for a different project key', () => {
    const pursuit = { id: 'pursuit-empty-dossier', title: 'Empty project', status: 'active', projectKey: 'empty-project' };
    pursuitService.dashboard.and.returnValue(of({ ...emptyDashboard(), pursuits: [pursuit] }));
    pursuitService.get.and.returnValue(of(normalizedPursuitDetail(pursuit)));
    pursuitService.projectDossier.and.returnValue(of(projectDossierFixture('wrong-project')));
    routeParams.next(convertToParamMap({ selected: pursuit.id }));
    fixture = TestBed.createComponent(PursuitsComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.projectDossier).toBeUndefined();
    expect(fixture.componentInstance.projectDossierError).toContain('did not match the selected project');
  });
});

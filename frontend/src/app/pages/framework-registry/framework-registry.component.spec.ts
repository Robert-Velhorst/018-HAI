import { HttpErrorResponse } from '@angular/common/http';
import { Router } from '@angular/router';
import { fakeAsync, tick } from '@angular/core/testing';
import { Subject, of, throwError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { IAuthSession } from '../../models/auth-session.model.interface';
import {
  IConstitution,
  IFrameworkRegistryOverview,
  IFrameworkFamilyTaxonomy,
  IFrameworkSelectionDecision,
  IFrameworkView,
} from '../../models/framework-registry.model.interface';
import { AuthSessionService } from '../../services/auth-session.service';
import { FrameworkRegistryService } from '../../services/framework-registry.service';
import { FrameworkRegistryComponent } from './framework-registry.component';

describe('FrameworkRegistryComponent', () => {
  let service: jasmine.SpyObj<FrameworkRegistryService>;
  let authSessionService: jasmine.SpyObj<AuthSessionService>;
  let notification: jasmine.SpyObj<NzNotificationService>;
  let router: jasmine.SpyObj<Router>;
  let component: FrameworkRegistryComponent;

  const overview: IFrameworkRegistryOverview = {
    generatedAt: '2026-07-30T10:00:00Z',
    total: 55,
    enabled: 54,
    experimental: 5,
    deprecated: 0,
    pinned: 1,
    families: { knowledge: 3, governance: 2 },
    constitutionVersion: 1,
    constitutionSource: 'builtin-robert-constitution-v1:v1',
    recentSelections: 1,
    selectionContract: ['classify the life domain'],
  };

  const ownerSession: IAuthSession = {
    authenticated: true,
    subject: 'owner-1',
    role: 'owner',
    permissions: {
      canRead: true,
      canOperate: true,
      canApprove: true,
      canAdminister: true,
    },
  };

  const framework: IFrameworkView = {
    id: 'truth-evidence',
    version: '1.0.0',
    name: 'Knowledge and truth',
    family: 'knowledge',
    purpose: 'Keep claims linked to evidence.',
    suitableProblemTypes: ['factual answer'],
    triggerConditions: ['verify'],
    requiredInputs: ['source'],
    producedOutputs: ['verified claim'],
    requiredAgents: ['evidence_agent'],
    workflowTemplate: ['retrieve', 'verify'],
    decisionRules: ['do not guess'],
    safetyInvariants: ['unsupported claims cannot become facts'],
    authorityRequirement: 'verify only',
    maximumAutonomyLevel: 4,
    riskCeiling: 'high',
    evidenceRequirements: ['source link'],
    evaluationMethod: ['claim support'],
    conflictsWith: [],
    userSpecificAdaptations: ['operator may disable'],
    source: 'HAI specification',
    provenance: 'operator supplied',
    status: 'active',
    effectiveStatus: 'active',
    enabled: true,
    pinned: true,
    effectiveAutonomyLevel: 3,
    adaptations: [],
  };

  const constitution: IConstitution = {
    id: 'constitution-1',
    version: 1,
    baseVersion: 0,
    status: 'active',
    values: ['Operator sovereignty'],
    prohibitions: ['No self-authority'],
    standingPermissions: ['Read local sources'],
    preferences: ['Local first'],
    relationshipRules: [],
    financialBoundaries: ['No paid calls'],
    communicationRules: ['Draft legal messages'],
    escalationRules: ['Ask Robert'],
    protectedRules: ['Robert is final authority'],
    createdAt: '2026-07-30T10:00:00Z',
  };
  const familyTaxonomy: IFrameworkFamilyTaxonomy = {
    version: '1.1.0',
    digest: 'a'.repeat(64),
    families: [],
  };

  const selection: IFrameworkSelectionDecision = {
    id: 'selection-1',
    createdAt: '2026-07-30T10:00:00Z',
    catalogVersion: 'v1',
    catalogDigest: 'a'.repeat(64),
    selectorAlgorithmVersion: 'selector-v4',
    effectivePreferenceDigest: 'b'.repeat(64),
    constitutionDigest: 'c'.repeat(64),
    lifeDomain: 'legal',
    needOrCommitment: 'respond to deadline',
    selected: [{
      id: framework.id,
      version: framework.version,
      name: framework.name,
      family: framework.family,
      score: 9,
      reasons: ['evidence is required'],
      maximumAutonomyLevel: 3,
      authorityRequirement: 'draft only',
      evidenceRequirements: ['source link'],
      evaluationMethod: ['claim support'],
    }],
    conflicts: [],
    requiredAgents: ['evidence_agent'],
    maximumAutonomyLevel: 2,
    authoritySummary: 'Draft only',
    requiresApproval: true,
    approvalReasons: ['legal communication'],
    evidenceRequirements: ['source link'],
    completionCriteria: ['draft is source grounded'],
    learningPlan: ['record verified correction'],
    contextRequirements: ['case documents'],
    selectionReason: 'Legal work needs evidence and approval.',
    constitutionVersion: 1,
    constitutionSource: 'builtin-robert-constitution-v1:v1',
  };

  beforeEach(() => {
    localStorage.removeItem('hai.module-view.v1.framework-registry');
    service = jasmine.createSpyObj<FrameworkRegistryService>('FrameworkRegistryService', [
      'overview',
      'familyTaxonomy',
      'frameworks',
      'framework',
      'select',
      'updatePreference',
      'selections',
      'constitution',
      'constitutionHistory',
      'createConstitutionDraft',
      'activateConstitution',
    ]);
    authSessionService = jasmine.createSpyObj<AuthSessionService>('AuthSessionService', [
      'session',
    ]);
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', [
      'success',
      'warning',
      'error',
    ]);
    router = jasmine.createSpyObj<Router>('Router', ['navigate']);

    service.overview.and.returnValue(of(overview));
    service.familyTaxonomy.and.returnValue(of(familyTaxonomy));
    authSessionService.session.and.returnValue(of(ownerSession));
    service.frameworks.and.returnValue(of([framework]));
    service.framework.and.returnValue(of(framework));
    service.selections.and.returnValue(of([selection]));
    service.constitution.and.returnValue(of({
      constitution,
      source: 'builtin-robert-constitution-v1:v1',
    }));
    service.constitutionHistory.and.returnValue(of({
      history: [{
        id: constitution.id,
        version: constitution.version,
        baseVersion: constitution.baseVersion,
        status: constitution.status,
        changeSummary: 'Initial owner governance baseline',
        approvedBy: 'owner-1',
        approvedAt: constitution.createdAt,
        createdAt: constitution.createdAt,
        digest: 'd'.repeat(64),
      }],
      limit: 50,
      truncated: false,
    }));
    service.select.and.returnValue(of(selection));
    service.updatePreference.and.returnValue(of(framework));
    service.createConstitutionDraft.and.returnValue(of({ ...constitution, id: 'draft-2', version: 2, status: 'draft' }));
    service.activateConstitution.and.returnValue(of({ ...constitution, version: 2 }));

    component = new FrameworkRegistryComponent(
      service,
      authSessionService,
      notification,
      router
    );
    component.authSession = ownerSession;
  });

  afterEach(() => localStorage.removeItem('hai.module-view.v1.framework-registry'));

  it('loads overview, frameworks, history, and Constitution independently', () => {
    component.ngOnInit();

    expect(component.overview).toBe(overview);
    expect(component.authSession).toBe(ownerSession);
    expect(component.frameworks).toEqual([framework]);
    expect(component.selections).toEqual([selection]);
    expect(component.constitution).toBe(constitution);
    expect(component.constitutionHistory.length).toBe(1);
    expect(component.constitutionHistory[0].digest).toBe('d'.repeat(64));
    expect(component.constitutionSource).toBe('builtin-robert-constitution-v1:v1');
    expect(component.loading).toBeFalse();
  });

  it('remembers Advanced disclosure for only this module', () => {
    component.setViewMode('advanced');
    component.toggleSection('selection-history');

    const stored = JSON.parse(
      localStorage.getItem('hai.module-view.v1.framework-registry') ?? '{}'
    ) as { mode?: string; openSections?: Record<string, boolean> };
    expect(stored.mode).toBe('advanced');
    expect(stored.openSections?.['selection-history']).toBeTrue();
  });

  it('loads the immutable taxonomy only when its Advanced section opens', () => {
    component.setViewMode('advanced');

    expect(service.familyTaxonomy).not.toHaveBeenCalled();

    component.toggleSection('family-taxonomy');

    expect(service.familyTaxonomy).toHaveBeenCalledTimes(1);
    expect(component.familyTaxonomy).toEqual(familyTaxonomy);

    component.toggleSection('family-taxonomy');
    component.toggleSection('family-taxonomy');
    expect(service.familyTaxonomy).toHaveBeenCalledTimes(1);
  });

  it('clears an Advanced-only status filter when returning to Basic view', () => {
    component.statusFilter = 'experimental';
    component.setViewMode('basic');

    expect(component.statusFilter).toBe('all');
    expect(component.viewMode).toBe('basic');
  });

  it('filters the catalogue by family and searchable purpose', () => {
    component.frameworks = [
      framework,
      { ...framework, id: 'formal-planning', name: 'Formal planning', family: 'planning', purpose: 'Plan dependencies.' },
    ];
    component.familyFilter = 'planning';
    component.searchText = 'dependencies';

    expect(component.filteredFrameworks.map((item) => item.id)).toEqual(['formal-planning']);
  });

  it('renders framework and selection ordering deterministically', () => {
    component.frameworks = [
      { ...framework, id: 'z-framework', name: 'Same name', pinned: false },
      { ...framework, id: 'a-framework', name: 'Same name', pinned: false },
      { ...framework, id: 'pinned-framework', name: 'Later', pinned: true },
    ];

    expect(component.displayedFrameworks.map((item) => item.id)).toEqual([
      'pinned-framework',
      'a-framework',
      'z-framework',
    ]);

    service.selections.and.returnValue(of([
      { ...selection, id: 'older', createdAt: '2026-07-29T10:00:00Z' },
      { ...selection, id: 'newer-b', createdAt: '2026-07-30T10:00:00Z' },
      { ...selection, id: 'newer-a', createdAt: '2026-07-30T10:00:00Z' },
    ]));
    component.currentSelection = undefined;
    component.refresh();

    expect(component.selections.map((item) => item.id)).toEqual([
      'newer-a',
      'newer-b',
      'older',
    ]);
    expect(
      (component.currentSelection as IFrameworkSelectionDecision | undefined)?.id
    ).toBe('newer-a');
  });

  it('does not call selection when the request is empty', () => {
    component.selectFrameworks();

    expect(service.select).not.toHaveBeenCalled();
    expect(component.selectionError).toContain('Describe');
  });

  it('moves focus to the request field when empty selection validation fails', fakeAsync(() => {
    const requestField = document.createElement('textarea');
    requestField.id = 'framework-selection-request';
    document.body.appendChild(requestField);

    component.selectFrameworks();
    tick();

    expect(document.activeElement).toBe(requestField);
    requestField.remove();
  }));

  it('submits only non-authoritative selection hints and shows the auditable result', () => {
    component.selectionDraft.request = 'Prepare a verified legal reply';
    component.selectionDraft.needsDocuments = true;
    component.selectionDraft.executeRequested = true;
    component.selectionDraft.successCriteriaText = 'Every claim has evidence\nNo message is sent';

    component.selectFrameworks();

    expect(service.select).toHaveBeenCalledWith(jasmine.objectContaining({
      request: 'Prepare a verified legal reply',
      needsDocuments: true,
      executeRequested: true,
      successCriteria: ['Every claim has evidence', 'No message is sent'],
    }));
    const request = service.select.calls.mostRecent().args[0] as unknown as Record<string, unknown>;
    expect(request['riskLevel']).toBeUndefined();
    expect(request['needsApproval']).toBeUndefined();
    expect(request['humanApproved']).toBeUndefined();
    expect(component.currentSelection).toBe(selection);
  });

  it('loads the inspect record and persists a narrowed preference', () => {
    component.authSession = ownerSession;
    component.frameworks = [framework];
    component.openFramework(framework);
    component.preferenceEditor.state = 'enabled';
    component.preferenceEditor.maximumAutonomyLevel = 2;
    component.preferenceEditor.adaptationsText = 'Draft only for legal work';

    component.savePreference();

    expect(service.framework).toHaveBeenCalledWith(framework.id);
    expect(service.updatePreference).toHaveBeenCalledWith(framework.id, {
      state: 'enabled',
      pinned: true,
      maximumAutonomyLevel: 2,
      adaptations: ['Draft only for legal work'],
    });
  });

  it('keeps the explicit preference state visible after a successful save', () => {
    component.selectedFramework = framework;
    component.preferenceEditor.state = 'enabled';
    component.preferenceEditor.maximumAutonomyLevel = null;

    component.savePreference();

    expect(component.preferenceEditor.state).toBe('enabled');
    expect(service.updatePreference).toHaveBeenCalledWith(
      framework.id,
      jasmine.objectContaining({ state: 'enabled' })
    );
  });

  it('blocks invalid and protected preference mutations before the API call', () => {
    component.selectedFramework = framework;
    component.preferenceEditor.state = 'disabled';

    component.savePreference();

    expect(service.updatePreference).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Check the preference',
      'Protected safety overlays cannot be disabled.'
    );

    component.preferenceEditor.state = 'default';
    component.preferenceEditor.maximumAutonomyLevel = 2.5;
    component.savePreference();

    expect(service.updatePreference).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Check the preference',
      jasmine.stringContaining('whole number')
    );
  });

  it('blocks owner-only mutations for an ordinary operator before any request is sent', () => {
    component.authSession = {
      ...ownerSession,
      role: 'operator',
      permissions: {
        ...ownerSession.permissions,
        canAdminister: false,
      },
    };
    component.selectedFramework = framework;
    component.constitution = constitution;

    component.savePreference();
    component.beginConstitutionDraft();

    expect(service.updatePreference).not.toHaveBeenCalled();
    expect(component.constitutionEditing).toBeFalse();
    expect(notification.warning).toHaveBeenCalledTimes(2);
    expect(notification.warning.calls.mostRecent().args[1]).toContain('Only the owner');
  });

  it('requires both the signed owner role and administration permission', () => {
    component.authSession = {
      ...ownerSession,
      role: 'operator',
      permissions: {
        ...ownerSession.permissions,
        canAdminister: true,
      },
    };
    component.selectedFramework = framework;

    component.savePreference();

    expect(component.canManageOwnerControls).toBeFalse();
    expect(service.updatePreference).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalled();
  });

  it('blocks viewers from creating recorded selections before calling the API', () => {
    component.authSession = {
      ...ownerSession,
      role: 'viewer',
      permissions: {
        canRead: true,
        canOperate: false,
        canApprove: false,
        canAdminister: false,
      },
    };
    component.selectionDraft.request = 'Inspect a framework';

    component.selectFrameworks();

    expect(component.canRequestSelection).toBeFalse();
    expect(service.select).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Framework selection is not available',
      jasmine.stringContaining('Viewer sessions')
    );
  });

  it('ignores duplicate selection requests while one is already running', () => {
    component.selectionBusy = true;
    component.selectionDraft.request = 'Plan a task';

    component.selectFrameworks();

    expect(service.select).not.toHaveBeenCalled();
  });

  it('allows a verified owner to open Constitution amendment controls', () => {
    component.authSession = ownerSession;
    component.constitution = constitution;

    component.beginConstitutionDraft();

    expect(component.constitutionEditing).toBeTrue();
  });

  it('requires the exact Constitution activation phrase without trimming', () => {
    component.authSession = ownerSession;
    component.constitutionDraft = {
      ...constitution,
      id: 'draft-2',
      version: 2,
      status: 'draft',
    };
    component.activationConfirmation = 'ACTIVATE CONSTITUTION ';
    component.activationNote = 'Owner reviewed this version';

    component.activateConstitutionDraft();

    expect(service.activateConstitution).not.toHaveBeenCalled();
    expect(component.constitutionError).toBe('Type ACTIVATE CONSTITUTION exactly.');

    component.activationConfirmation = 'ACTIVATE CONSTITUTION';
    component.activateConstitutionDraft();

    expect(service.activateConstitution).toHaveBeenCalledOnceWith('draft-2', {
      confirmation: 'ACTIVATE CONSTITUTION',
      approvalNote: 'Owner reviewed this version',
    });
  });

  it('does not create another Constitution draft while the saved draft is active', () => {
    component.constitution = constitution;
    component.constitutionDraft = {
      ...constitution,
      id: 'draft-2',
      version: 2,
      status: 'draft',
    };

    component.createConstitutionDraft();

    expect(service.createConstitutionDraft).not.toHaveBeenCalled();
    expect(component.constitutionError).toContain('already immutable');
    expect(notification.warning).toHaveBeenCalledWith(
      'Draft already created',
      jasmine.stringContaining('Close this editor')
    );
  });

  it('gives a recovery path for stale Constitution activation conflicts', () => {
    service.activateConstitution.and.returnValue(
      throwError(() => new HttpErrorResponse({
        status: 409,
        error: { error: 'framework registry state conflict' },
      }))
    );
    component.constitutionDraft = {
      ...constitution,
      id: 'draft-2',
      version: 2,
      status: 'draft',
    };
    component.activationConfirmation = 'ACTIVATE CONSTITUTION';
    component.activationNote = 'Owner reviewed this version';

    component.activateConstitutionDraft();

    expect(component.constitutionError).toContain('Refresh the active Constitution');
    expect(component.constitutionError).not.toContain('state conflict');
  });

  it('distinguishes valid restrictive HAI-RULE constraints from versioned prose', () => {
    expect(component.isMachineEnforcedConstitutionRule(
      'HAI-RULE v1 require-approval capability=public-posting'
    )).toBeTrue();
    expect(component.isMachineEnforcedConstitutionRule(
      'HAI-RULE v1 authority-ceiling level=6'
    )).toBeTrue();
    expect(component.isMachineEnforcedConstitutionRule(
      'Robert remains final authority.'
    )).toBeFalse();
    expect(component.isMachineEnforcedConstitutionRule(
      'HAI-RULE v1 grant-capability capability=execution'
    )).toBeFalse();
    expect(component.isMachineEnforcedConstitutionRule(
      'HAI-RULE v1 authority-ceiling level=1.0'
    )).toBeFalse();
  });

  it('fails closed when signed authorization capabilities are unavailable', () => {
    authSessionService.session.and.returnValue(
      throwError(() => new HttpErrorResponse({ status: 404 }))
    );

    component.ngOnInit();

    expect(component.actorRole).toBe('unknown');
    expect(component.canManageOwnerControls).toBeFalse();
    expect(component.ownerControlExplanation).toContain(
      'could not verify an authenticated local session'
    );
    expect(component.loadErrors['authorization']).toContain('could not be verified');
  });

  it('clears an in-progress Constitution draft when refreshed authority is downgraded', () => {
    component.constitution = constitution;
    component.constitutionEditing = true;
    component.constitutionDraft = { ...constitution, id: 'draft-2', version: 2, status: 'draft' };
    component.activationNote = 'Owner approval context';
    authSessionService.session.and.returnValue(of({
      ...ownerSession,
      role: 'viewer',
      permissions: {
        canRead: true,
        canOperate: false,
        canApprove: false,
        canAdminister: false,
      },
    }));

    component.refresh();

    expect(component.constitutionEditing).toBeFalse();
    expect(component.constitutionDraft).toBeUndefined();
    expect(component.activationNote).toBe('');
  });

  it('opens a recommendation framework even when the catalogue is unavailable', () => {
    component.frameworks = [];

    component.openFrameworkById(framework.id);

    expect(service.framework).toHaveBeenCalledWith(framework.id);
    expect(component.selectedFramework).toBe(framework);
    expect(component.inspectorVisible).toBeTrue();
  });

  it('cancels an obsolete inspector request before applying a newer record', () => {
    const delayed = new Subject<IFrameworkView>();
    const newer = {
      ...framework,
      id: 'formal-planning',
      name: 'Formal planning',
    };
    service.framework.and.returnValues(delayed.asObservable(), of(newer));

    component.openFrameworkById(framework.id);
    component.openFrameworkById(newer.id);
    delayed.next(framework);
    delayed.complete();

    expect(component.selectedFramework).toBe(newer);
    expect(component.inspectorVisible).toBeTrue();
  });

  it('cancels an obsolete refresh before it can replace newer state', () => {
    const delayedOverview = new Subject<IFrameworkRegistryOverview>();
    const newerOverview = {
      ...overview,
      generatedAt: '2026-07-30T11:00:00Z',
      recentSelections: 2,
    };
    service.overview.and.returnValues(
      delayedOverview.asObservable(),
      of(newerOverview)
    );

    component.refresh();
    component.refresh();
    delayedOverview.next(overview);
    delayedOverview.complete();

    expect(component.overview).toBe(newerOverview);
    expect(component.loading).toBeFalse();
  });

  it('restores focus to the control that opened the inspector', fakeAsync(() => {
    const trigger = document.createElement('button');
    document.body.appendChild(trigger);
    trigger.focus();

    component.openFrameworkById(framework.id);
    component.closeInspector();
    tick();

    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  }));

  it('does not turn a pinned catalog default into an explicit enable preference', () => {
    const pinnedDefault = {
      ...framework,
      preferenceUpdatedAt: '2026-07-30T10:00:00Z',
      enabled: true,
      status: 'active' as const,
    };
    service.framework.and.returnValue(of(pinnedDefault));

    component.openFramework(pinnedDefault);

    expect(component.preferenceEditor.state).toBe('default');
  });

  it('does not expose raw 404 or server failure text in partial-load errors', () => {
    service.overview.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 404,
      error: '404 page not found',
    })));
    service.frameworks.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 500,
      error: 'pq: password=secret database failure',
    })));

    component.ngOnInit();

    expect(component.loadErrors['overview']).toBe('Registry overview is unavailable.');
    expect(component.loadErrors['frameworks']).toBe('Framework records are unavailable.');
    expect(JSON.stringify(component.loadErrors)).not.toContain('secret');
  });

  it('does not expose credential-like client error text', () => {
    service.select.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 400,
      error: 'api_key=super-secret-value is invalid',
    })));
    component.selectionDraft.request = 'Plan safely';

    component.selectFrameworks();

    expect(component.selectionError).toBe(
      'HAI could not create a framework recommendation. No execution was started.'
    );
    expect(component.selectionError).not.toContain('super-secret-value');
  });

  it('routes the back action to the Command Center', () => {
    component.goBack();

    expect(router.navigate).toHaveBeenCalledOnceWith(['/control-center']);
  });
});

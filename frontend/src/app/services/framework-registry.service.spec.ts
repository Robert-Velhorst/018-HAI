import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import {
  IConstitution,
  IConstitutionHistoryPage,
  IFrameworkRegistryOverview,
  IFrameworkSelectionDecision,
  IFrameworkSelectionRequest,
  IFrameworkView,
} from '../models/framework-registry.model.interface';
import { FrameworkRegistryService } from './framework-registry.service';

describe('FrameworkRegistryService', () => {
  let service: FrameworkRegistryService;
  let http: HttpTestingController;

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
    userSpecificAdaptations: [],
    source: 'HAI specification',
    provenance: 'built in',
    status: 'active',
    effectiveStatus: 'active',
    enabled: true,
    pinned: false,
    effectiveAutonomyLevel: 4,
    adaptations: [],
  };
  const selection: IFrameworkSelectionDecision = {
    id: 'selection-1',
    taskPlanId: 'plan-1',
    createdAt: '2026-07-30T10:00:00Z',
    catalogVersion: 'v2',
    catalogDigest: 'a'.repeat(64),
    selectorAlgorithmVersion: 'selector-v5',
    taskRiskLevel: 'high',
    effectiveRiskCeiling: 'high',
    effectivePreferenceDigest: 'b'.repeat(64),
    constitutionDigest: 'c'.repeat(64),
    lifeDomain: 'legal',
    needOrCommitment: 'respond to deadline',
    selected: [{
      id: framework.id,
      version: framework.version,
      name: framework.name,
      family: framework.family,
      riskCeiling: 'high',
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
    learningPlan: [],
    contextRequirements: ['case documents'],
    selectionReason: 'Legal work needs evidence and approval.',
    constitutionVersion: 1,
    constitutionSource: 'builtin-robert-constitution-v1:v1',
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
  const overview: IFrameworkRegistryOverview = {
    generatedAt: '2026-07-30T10:00:00Z',
    total: 55,
    enabled: 50,
    experimental: 5,
    deprecated: 0,
    pinned: 1,
    families: { knowledge: 5 },
    constitutionVersion: 1,
    constitutionSource: 'builtin-robert-constitution-v1:v1',
    recentSelections: 1,
    selectionContract: ['classify the task'],
  };

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] });
    service = TestBed.inject(FrameworkRegistryService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads the registry overview', () => {
    service.overview().subscribe((result) => expect(result).toEqual(overview));

    const request = http.expectOne('/api/v1/framework-registry/overview');
    expect(request.request.method).toBe('GET');
    request.flush(overview);
  });

  it('normalizes a wrapped framework list', () => {
    service.frameworks().subscribe((result) => expect(result).toEqual([framework]));

    const request = http.expectOne('/api/v1/framework-registry/frameworks');
    expect(request.request.method).toBe('GET');
    request.flush({ frameworks: [framework] });
  });

  it('normalizes legacy null empty collections without weakening array validation', () => {
    service.frameworks().subscribe((result) => {
      expect(result).toEqual([framework]);
      expect(result[0].conflictsWith).toEqual([]);
      expect(result[0].adaptations).toEqual([]);
    });

    const request = http.expectOne('/api/v1/framework-registry/frameworks');
    request.flush({
      frameworks: [{
        ...framework,
        conflictsWith: null,
        adaptations: null,
      }],
    });
  });

  it('loads one framework for inspection', () => {
    service.framework('truth-evidence').subscribe((result) => expect(result).toEqual(framework));

    const request = http.expectOne('/api/v1/framework-registry/frameworks/truth-evidence');
    expect(request.request.method).toBe('GET');
    request.flush({ framework });
  });

  it('posts only safe public selection intent fields', () => {
    const body = {
      request: 'Verify this claim',
      needsDocuments: true,
      executeRequested: true,
      requiredReasoning: 'high',
      riskLevel: 'low',
      needsApproval: false,
      humanApproved: true,
    } as IFrameworkSelectionRequest & {
      riskLevel: string;
      needsApproval: boolean;
      humanApproved: boolean;
    };
    service.select(body).subscribe((result) => expect(result).toEqual(selection));

    const request = http.expectOne('/api/v1/framework-registry/select');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      request: 'Verify this claim',
      needsDocuments: true,
      executeRequested: true,
      requiredReasoning: 'high',
    });
    expect(request.request.body['riskLevel']).toBeUndefined();
    expect(request.request.body['needsApproval']).toBeUndefined();
    expect(request.request.body['humanApproved']).toBeUndefined();
    request.flush({ selection });
  });

  it('patches an owner-scoped framework preference', () => {
    const body = {
      state: 'enabled' as const,
      pinned: true,
      maximumAutonomyLevel: 2,
      ownerIdentity: 'forged-owner',
    };
    service.updatePreference('truth-evidence', body).subscribe((result) => expect(result).toEqual(framework));

    const request = http.expectOne('/api/v1/framework-registry/frameworks/truth-evidence/preference');
    expect(request.request.method).toBe('PATCH');
    expect(request.request.body).toEqual({
      state: 'enabled',
      pinned: true,
      maximumAutonomyLevel: 2,
    });
    expect(request.request.body['ownerIdentity']).toBeUndefined();
    request.flush({ framework });
  });

  it('normalizes selection history', () => {
    service.selections().subscribe((result) => expect(result).toEqual([selection]));

    const request = http.expectOne('/api/v1/framework-registry/selections?limit=10');
    expect(request.request.method).toBe('GET');
    request.flush({ selections: [selection] });
  });

  it('accepts legacy selection history without a recorded risk ceiling', () => {
    const {
      taskRiskLevel: _taskRiskLevel,
      effectiveRiskCeiling: _effectiveRiskCeiling,
      ...selectionWithoutRiskContract
    } = selection;
    const legacySelection = {
      ...selectionWithoutRiskContract,
      selectorAlgorithmVersion: 'selector-v4',
      selected: selection.selected.map(({ riskCeiling: _riskCeiling, ...item }) => item),
    };
    service.selections().subscribe((result) => {
      expect(result[0].selected[0].riskCeiling).toBeUndefined();
    });

    const request = http.expectOne('/api/v1/framework-registry/selections?limit=10');
    request.flush({ selections: [legacySelection] });
  });

  it('rejects an unsupported selected-framework risk ceiling', () => {
    let error: unknown;
    service.selections().subscribe({ error: (value) => (error = value) });

    const request = http.expectOne('/api/v1/framework-registry/selections?limit=10');
    request.flush({
      selections: [{
        ...selection,
        selected: [{ ...selection.selected[0], riskCeiling: 'critical' }],
      }],
    });

    expect(error).toEqual(jasmine.any(Error));
    expect((error as Error).message).toBe(
      'Invalid Framework Registry selection history item 1 selected framework 1 response.'
    );
  });

  it('rejects selector-v5 history without its top-level risk contract', () => {
    let error: unknown;
    const { taskRiskLevel: _taskRiskLevel, ...missingRisk } = selection;
    service.selections().subscribe({ error: (value) => (error = value) });

    const request = http.expectOne('/api/v1/framework-registry/selections?limit=10');
    request.flush({ selections: [missingRisk] });

    expect(error).toEqual(jasmine.any(Error));
  });

  it('normalizes the chief-of-staff operating contract', () => {
    const operatingSelection = {
      ...selection,
      operatingContractDigest: 'd'.repeat(64),
      lifeDomains: [{
        id: 'legal_government',
        need: 'legal obligation',
        score: 2,
        confidence: 0.8,
        signals: ['lawyer'],
        primary: true,
        source: 'deterministic_request_classification',
      }],
      needsState: [{
        id: 'derived-legal',
        domainId: 'legal_government',
        level: 'rights_and_security',
        state: 'attention_required',
        priority: 90,
        confidence: 0.8,
        evidence: ['lawyer'],
        source: 'derived_from_request_not_operator_confirmed',
        needsReview: true,
      }],
      capacity: {
        status: 'unknown',
        planningStepLimit: 5,
        constraints: ['current human capacity was not provided'],
        confidence: 0,
        fresh: false,
        needsReview: true,
      },
      agentCards: [{
        id: 'hai_task_engine',
        name: 'HAI task engine',
        owner: 'authenticated_owner_scope',
        purpose: 'coordinate governed work',
        role: 'coordinator',
        capabilities: ['plan'],
        domainCompetence: ['operations'],
        allowedTools: ['allowlisted tools'],
        requiredPermissions: ['owner-scoped task read'],
        dataAccessBoundaries: ['current task'],
        costProfile: 'local no-spend',
        modelRequirements: [],
        reliabilityHistory: ['contract tests'],
        allowedActions: ['plan and simulate'],
        prohibitedActions: ['self-approval'],
        inputSchema: 'framework_selection_request_v4',
        outputSchema: 'framework_selection_decision_v4',
        expectedEvidence: ['audit record'],
        escalationRoute: 'operator review',
        availability: 'local process',
        version: 'selector-v4',
        dependencies: ['framework registry'],
        healthStatus: 'available',
        evaluationScore: 1,
        evaluationScoreSource: 'contract fixture',
        authorityCeiling: 2,
        status: 'available',
        verified: true,
        revoked: false,
        provenance: 'embedded_canonical_go_engine',
      }],
      delegations: [{
        id: 'delegation-1',
        delegator: 'chief_of_staff',
        delegatee: 'hai_task_engine',
        objective: 'Prepare a plan',
        allowedActions: ['plan'],
        prohibitedActions: ['self-approval'],
        budgetLimitEur: 0,
        budgetPolicy: 'no_spend_authorized',
        deadlineStatus: 'not_set',
        constraints: ['no financial expenditure is authorized'],
        authorityCeiling: 2,
        requiresApproval: true,
        evidenceRequired: ['source'],
        completionCriteria: ['verified'],
        escalationTriggers: ['conflict'],
        state: 'ready',
      }],
      communication: {
        schemaVersion: 'hai-agent-message-v1',
        allowedMessageTypes: ['request'],
        allowedConfidentiality: ['internal'],
        requiredFields: ['correlationId'],
        forbiddenContent: ['secrets'],
        maximumAuthority: 2,
        maximumPayloadChars: 4000,
        maximumTtlSeconds: 86400,
        redactionRequired: true,
        idempotencyRequired: true,
        provenanceRequired: true,
        signaturePolicy: 'optional_digest_requires_external_verification',
        correlationId: 'correlation-1',
      },
      coordination: {
        mode: 'single_engine',
        allowedModes: ['single_engine'],
        coordinator: 'hai_task_engine',
        participants: ['hai_task_engine'],
        handoffOrder: ['hai_task_engine'],
        consensusRule: 'No vote grants authority.',
        escalationRule: 'Escalate on conflict.',
        rationale: 'Only one verified engine is available.',
      },
      actionAutonomy: [{
        action: 'create_plan_or_draft',
        requiredLevel: 4,
        effectiveCeiling: 2,
        levelName: 'plan_and_draft',
        allowed: false,
        requiresApproval: false,
        reason: 'ceiling too low',
      }],
      stopConditions: ['stop when authority is missing'],
      outcomeMonitoring: ['verify completion'],
      chiefOfStaff: {
        needsAttention: 'Legal reply',
        whyNow: 'Deadline',
        contextNeeded: 'Case file',
        whoShouldAct: 'hai_task_engine',
        howToProceed: 'Plan first',
        mayProceedNow: 'Read context',
        needsApproval: 'Sending requires approval',
        completionProof: 'Verified draft',
      },
    };

    service.selections().subscribe((result) => {
      expect(result[0].operatingContractDigest).toBe('d'.repeat(64));
      expect(result[0].chiefOfStaff?.needsAttention).toBe('Legal reply');
      expect(result[0].agentCards?.[0].verified).toBeTrue();
      expect(result[0].actionAutonomy?.[0].allowed).toBeFalse();
    });

    const request = http.expectOne('/api/v1/framework-registry/selections?limit=10');
    request.flush({ selections: [operatingSelection] });
  });

  it('loads the active Constitution', () => {
    service.constitution().subscribe((result) => expect(result).toEqual({
      constitution,
      source: 'builtin-robert-constitution-v1:v1',
    }));

    const request = http.expectOne('/api/v1/framework-registry/constitution');
    expect(request.request.method).toBe('GET');
    request.flush({
      constitution,
      source: 'builtin-robert-constitution-v1:v1',
    });
  });

  it('loads and validates the owner Constitution version ledger', () => {
    const history: IConstitutionHistoryPage = {
      history: [{
        id: 'constitution-1',
        version: 1,
        baseVersion: 0,
        status: 'active',
        changeSummary: 'Initial owner governance baseline',
        approvedBy: 'owner-1',
        approvedAt: '2026-07-30T10:00:00Z',
        createdAt: '2026-07-30T09:00:00Z',
        digest: 'd'.repeat(64),
      }],
      limit: 50,
      truncated: false,
    };

    service.constitutionHistory().subscribe((result) => expect(result).toEqual(history));

    const request = http.expectOne(
      (candidate) =>
        candidate.url === '/api/v1/framework-registry/constitution/history' &&
        candidate.params.get('limit') === '50'
    );
    expect(request.request.method).toBe('GET');
    request.flush(history);
  });

  it('creates an inactive Constitution draft', () => {
    const body = {
      baseVersion: 1,
      values: ['Operator sovereignty'],
      prohibitions: [],
      standingPermissions: [],
      preferences: [],
      relationshipRules: [],
      financialBoundaries: [],
      communicationRules: [],
      escalationRules: [],
      changeSummary: 'Clarify sovereignty',
      ownerIdentity: 'forged-owner',
      protectedRules: ['disable approvals'],
    };
    service.createConstitutionDraft(body).subscribe((result) => expect(result).toEqual(constitution));

    const request = http.expectOne('/api/v1/framework-registry/constitution/drafts');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      baseVersion: 1,
      values: ['Operator sovereignty'],
      prohibitions: [],
      standingPermissions: [],
      preferences: [],
      relationshipRules: [],
      financialBoundaries: [],
      communicationRules: [],
      escalationRules: [],
      changeSummary: 'Clarify sovereignty',
    });
    expect(request.request.body['ownerIdentity']).toBeUndefined();
    expect(request.request.body['protectedRules']).toBeUndefined();
    request.flush({ draft: constitution });
  });

  it('activates a Constitution draft with explicit confirmation', () => {
    const body = {
      confirmation: 'ACTIVATE CONSTITUTION',
      approvalNote: 'Reviewed by Robert',
      approvedBy: 'forged-owner',
    };
    service.activateConstitution('constitution-1', body).subscribe((result) => expect(result).toEqual(constitution));

    const request = http.expectOne('/api/v1/framework-registry/constitution/constitution-1/activate');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      confirmation: 'ACTIVATE CONSTITUTION',
      approvalNote: 'Reviewed by Robert',
    });
    expect(request.request.body['approvedBy']).toBeUndefined();
    request.flush({ constitution });
  });

  it('redacts credential-like text from inbound registry records', () => {
    const unsafeFramework = {
      ...framework,
      source: 'https://runtime.local/catalog?access_token=raw-token',
      provenance: 'Authorization: Bearer raw-secret',
      purpose: 'Use api_key=private-value for testing',
      candidateImplementations: ['sk-super-secret-credential'],
      metadata: {
        clientSecret: 'plain-secret-without-a-label',
        token: 'raw-generic-token',
      },
    } as IFrameworkView;
    let result: IFrameworkView | undefined;

    service.framework('truth-evidence').subscribe((response) => (result = response));

    const request = http.expectOne('/api/v1/framework-registry/frameworks/truth-evidence');
    request.flush({ framework: unsafeFramework });

    expect(result?.source).toContain('access_token=[redacted]');
    expect(result?.provenance).toContain('Authorization: [redacted]');
    expect(result?.purpose).toContain('api_key=[redacted]');
    expect(JSON.stringify(result)).not.toContain('clientSecret');
    expect(JSON.stringify(result)).not.toContain('"token"');
    expect(JSON.stringify(result)).not.toContain('raw-secret');
    expect(JSON.stringify(result)).not.toContain('private-value');
    expect(JSON.stringify(result)).not.toContain('raw-token');
    expect(JSON.stringify(result)).not.toContain('plain-secret-without-a-label');
    expect(JSON.stringify(result)).not.toContain('raw-generic-token');
    expect(JSON.stringify(result)).not.toContain('sk-super-secret-credential');
  });

  it('rejects malformed list envelopes instead of presenting them as empty data', () => {
    let error: unknown;
    service.frameworks().subscribe({ error: (value) => (error = value) });

    const request = http.expectOne('/api/v1/framework-registry/frameworks');
    request.flush({ frameworks: { id: 'not-an-array' } });

    expect(error).toEqual(jasmine.any(Error));
    expect((error as Error).message).toBe(
      'Invalid Framework Registry framework list response.'
    );
  });

  it('rejects malformed selection provenance before it reaches the UI', () => {
    let error: unknown;
    service.select({ request: 'Plan safely' }).subscribe({
      error: (value) => (error = value),
    });

    const request = http.expectOne('/api/v1/framework-registry/select');
    request.flush({
      ...selection,
      constitutionDigest: 'not-a-sha256-digest',
    });

    expect(error).toEqual(jasmine.any(Error));
    expect((error as Error).message).toBe(
      'Invalid Framework Registry selection response.'
    );
  });

  it('rejects a malformed active Constitution timestamp', () => {
    let error: unknown;
    service.constitution().subscribe({ error: (value) => (error = value) });

    const request = http.expectOne('/api/v1/framework-registry/constitution');
    request.flush({
      constitution: { ...constitution, createdAt: 'not-a-date' },
      source: 'builtin-robert-constitution-v1:v1',
    });

    expect(error).toEqual(jasmine.any(Error));
    expect((error as Error).message).toBe(
      'Invalid Framework Registry active Constitution response.'
    );
  });
});

import { CommonModule } from '@angular/common';
import { NO_ERRORS_SCHEMA } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { IFrameworkSelectionDecision } from '../../models/framework-registry.model.interface';
import { FrameworkRegistryRecommendationComponent } from './framework-registry-recommendation.component';

describe('FrameworkRegistryRecommendationComponent', () => {
  let fixture: ComponentFixture<FrameworkRegistryRecommendationComponent>;
  let component: FrameworkRegistryRecommendationComponent;

  const selection: IFrameworkSelectionDecision = {
    id: 'selection-1',
    taskPlanId: 'plan-1',
    createdAt: '2026-07-30T10:00:00Z',
    catalogVersion: 'v1',
    catalogDigest: 'a'.repeat(64),
    selectorAlgorithmVersion: 'selector-v4',
    effectivePreferenceDigest: 'b'.repeat(64),
    constitutionDigest: 'c'.repeat(64),
    lifeDomain: 'legal',
    needOrCommitment: 'respond to deadline',
    selected: [{
      id: 'truth-evidence',
      version: '1.0.0',
      name: 'Knowledge and truth',
      family: 'knowledge',
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

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [FrameworkRegistryRecommendationComponent],
      imports: [CommonModule],
      schemas: [NO_ERRORS_SCHEMA],
    }).compileComponents();

    fixture = TestBed.createComponent(FrameworkRegistryRecommendationComponent);
    component = fixture.componentInstance;
  });

  it('announces loading instead of presenting a false empty state', () => {
    component.loading = true;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const status = root.querySelector<HTMLElement>('[role="status"]');

    expect(status?.textContent).toContain('Loading the latest recommendation');
    expect(root.textContent).not.toContain('No recommendation yet');
  });

  it('distinguishes an unavailable history endpoint from no recommendation', () => {
    component.unavailableMessage = 'Selection history is unavailable.';
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const alert = root.querySelector<HTMLElement>('[role="alert"]');

    expect(alert?.textContent).toContain('Recommendation history unavailable');
    expect(alert?.textContent).toContain('Selection history is unavailable.');
    expect(root.textContent).not.toContain('No recommendation yet');
  });

  it('opens a selected framework through a named keyboard-operable button', () => {
    const inspectSpy = spyOn(component.inspectFramework, 'emit');
    component.selection = selection;
    fixture.detectChanges();

    const button = fixture.nativeElement.querySelector(
      'button.selected-framework'
    ) as HTMLButtonElement | null;

    expect(button?.getAttribute('aria-label')).toBe(
      'Inspect Knowledge and truth framework'
    );
    button?.click();
    expect(inspectSpy).toHaveBeenCalledOnceWith('truth-evidence');
  });

  it('labels a retained recommendation when history refresh failed', () => {
    component.selection = selection;
    component.staleMessage = 'Selection history is unavailable.';
    fixture.detectChanges();

    const stale = (fixture.nativeElement as HTMLElement).querySelector(
      '.recommendation-stale[role="status"]'
    );

    expect(stale?.textContent).toContain('Selection history is unavailable.');
    expect(stale?.textContent).toContain('last successfully loaded recommendation');
  });

  it('exposes the complete selection and reproducibility contract in Advanced view', () => {
    component.selection = {
      ...selection,
      conflicts: [{
        selectedId: 'truth-evidence',
        skippedId: 'ungrounded-answering',
        reason: 'Evidence discipline takes precedence.',
      }],
      learningPlan: ['Record only verified corrections'],
    };
    fixture.componentRef.setInput('advanced', true);
    fixture.detectChanges();

    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';

    [
      'Selected framework contracts',
      'evidence is required',
      'claim support',
      'Conflict resolution',
      'ungrounded-answering',
      'Context requirements',
      'case documents',
      'Learning plan',
      'Record only verified corrections',
      'Reproducibility and audit contract',
      'selection-1',
      'plan-1',
      'selector-v4',
      'builtin-robert-constitution-v1:v1',
      'a'.repeat(64),
      'b'.repeat(64),
      'c'.repeat(64),
    ].forEach((value) => expect(text).toContain(value));
  });

  it('keeps the chief-of-staff summary concise and reveals operating controls in Advanced view', () => {
    component.selection = {
      ...selection,
      operatingContractDigest: 'd'.repeat(64),
      chiefOfStaff: {
        needsAttention: 'Review the legal deadline',
        whyNow: 'A source-backed reply is due.',
        contextNeeded: 'Case records',
        whoShouldAct: 'hai_task_engine',
        howToProceed: 'Plan then verify',
        mayProceedNow: 'Read and draft',
        needsApproval: 'Sending needs approval',
        completionProof: 'Verified draft',
      },
      capacity: {
        status: 'unknown',
        planningStepLimit: 5,
        constraints: ['current human capacity was not provided'],
        confidence: 0,
        fresh: false,
        needsReview: true,
      },
      coordination: {
        mode: 'single_engine',
        allowedModes: ['single_engine'],
        coordinator: 'hai_task_engine',
        participants: ['hai_task_engine'],
        handoffOrder: ['hai_task_engine'],
        consensusRule: 'No vote grants authority.',
        escalationRule: 'Escalate on conflict.',
        rationale: 'Only the embedded engine is verified.',
      },
      lifeDomains: [{
        id: 'legal_government',
        need: 'legal obligation',
        score: 2,
        confidence: 0.8,
        signals: ['lawyer'],
        primary: true,
        source: 'deterministic_request_classification',
      }],
      needsState: [],
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
      delegations: [],
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
    };
    fixture.detectChanges();

    let text = (fixture.nativeElement as HTMLElement).textContent ?? '';
    expect(text).toContain('Review the legal deadline');
    expect(text).not.toContain('Per-action autonomy');

    fixture.componentRef.setInput('advanced', true);
    fixture.detectChanges();
    text = (fixture.nativeElement as HTMLElement).textContent ?? '';
    [
      'Whole-life scope',
      'Human capacity',
      'Agent cards and delegation readiness',
      'Typed communication',
      'Per-action autonomy',
      'Stop conditions',
      'Outcome monitoring',
      'd'.repeat(64),
    ].forEach((value) => expect(text).toContain(value));
  });
});

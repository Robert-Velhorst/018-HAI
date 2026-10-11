import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FormsModule } from '@angular/forms';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzInputNumberModule } from 'ng-zorro-antd/input-number';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { IFrameworkView } from '../../models/framework-registry.model.interface';
import { FrameworkRegistryInspectorComponent } from './framework-registry-inspector.component';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('FrameworkRegistryInspectorComponent role rendering', () => {
  let fixture: ComponentFixture<FrameworkRegistryInspectorComponent>;
  let component: FrameworkRegistryInspectorComponent;

  const framework: IFrameworkView = {
    id: 'truth-evidence',
    version: '1.0.0',
    name: 'Knowledge and truth',
    family: 'knowledge',
    purpose: 'Keep claims linked to evidence.',
    suitableProblemTypes: ['factual answer'],
    triggerConditions: ['verify'],
    requiredInputs: ['source record'],
    producedOutputs: ['verified claim record'],
    requiredAgents: ['evidence_agent'],
    workflowTemplate: ['retrieve', 'verify'],
    decisionRules: ['do not guess'],
    safetyInvariants: ['unsupported claims cannot become facts'],
    authorityRequirement: 'verify only',
    maximumAutonomyLevel: 4,
    riskCeiling: 'high',
    evidenceRequirements: ['source link'],
    evaluationMethod: ['claim support'],
    conflictsWith: ['ungrounded-answering'],
    userSpecificAdaptations: ['Prefer Robert-owned project sources'],
    candidateImplementations: ['local evidence index'],
    source: 'HAI specification',
    provenance: 'owner supplied',
    status: 'active',
    effectiveStatus: 'active',
    enabled: true,
    pinned: false,
    effectiveAutonomyLevel: 3,
    adaptations: ['Draft only for legal claims'],
    preferenceUpdatedAt: '2026-07-30T10:00:00Z',
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
    declarations: [FrameworkRegistryInspectorComponent],
    imports: [CommonModule,
        FormsModule,
        NoopAnimationsModule,
        NzAlertModule,
        NzButtonModule,
        NzIconModule,
        NzInputModule,
        NzInputNumberModule,
        NzSelectModule,
        NzSpinModule,
        NzTagModule,
        NzTooltipModule],
    providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()]
}).compileComponents();

    fixture = TestBed.createComponent(FrameworkRegistryInspectorComponent);
    component = fixture.componentInstance;
    component.framework = framework;
  });

  it('renders operator preference mutations disabled with an owner-only explanation', () => {
    component.actorRole = 'operator';
    component.canManagePreferences = false;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const saveButton = Array.from(root.querySelectorAll('button'))
      .find((button) => button.textContent?.includes('Save preference'));
    const adaptations = root.querySelector<HTMLTextAreaElement>('.full-field textarea');

    expect(root.textContent).toContain('Only the owner can change framework preferences');
    expect(saveButton?.disabled).toBeTrue();
    expect(adaptations?.matches(':disabled')).toBeTrue();
  });

  it('renders preference mutations enabled only for a verified owner', () => {
    component.actorRole = 'owner';
    component.canManagePreferences = true;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const saveButton = Array.from(root.querySelectorAll('button'))
      .find((button) => button.textContent?.includes('Save preference'));
    const adaptations = root.querySelector<HTMLTextAreaElement>('.full-field textarea');

    expect(root.textContent).not.toContain('Owner-only controls');
    expect(saveButton?.disabled).toBeFalse();
    expect(adaptations?.matches(':disabled')).toBeFalse();
    expect(adaptations?.getAttribute('aria-label')).toBe('Owner adaptations, one per line');
    expect(root.querySelector('[role="switch"]')?.getAttribute('aria-label'))
      .toBe('Pin framework for this owner');
  });

  it('announces the loading state to assistive technology', () => {
    component.framework = undefined;
    component.loading = true;
    fixture.detectChanges();

    const status = fixture.nativeElement.querySelector('[role="status"]') as HTMLElement | null;

    expect(status?.textContent).toContain('Loading the current framework contract');
    expect(status?.getAttribute('aria-live')).toBe('polite');
  });

  it('shows the versioned identity and authority summary in the calm basic view', () => {
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;

    expect(root.textContent).toContain('Framework ID');
    expect(root.textContent).toContain('truth-evidence');
    expect(root.textContent).toContain('Contract version');
    expect(root.textContent).toContain('1.0.0');
    expect(root.textContent).toContain('Effective lifecycle');
    expect(root.textContent).toContain('Selection eligibility');
    expect(root.textContent).toContain('Effective availability state');
    expect(root.textContent).toContain('Preference updated');
    expect(root.textContent).toContain('2026-07-30 10:00 UTC');
    expect(root.textContent).toContain('Level 3 effective; level 4 catalog maximum');
    expect(root.querySelector('#framework-full-contract')).toBeNull();
  });

  it('prevents duplicate preference submission while a save is running', () => {
    component.actorRole = 'owner';
    component.canManagePreferences = true;
    component.saving = true;
    fixture.detectChanges();

    const saveButton = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('button')
    ).find((button) => button.textContent?.includes('Save preference'));

    expect(saveButton?.disabled).toBeTrue();
  });

  it('marks protected overlays and explains that they cannot be disabled', () => {
    component.actorRole = 'owner';
    component.canManagePreferences = true;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;

    expect(component.isProtectedSafetyOverlay).toBeTrue();
    expect(root.textContent).toContain('Protected safety overlay');
    expect(root.textContent).toContain('cannot be disabled');
  });

  it('makes experimental lifecycle behavior explicit', () => {
    component.framework = {
      ...framework,
      id: 'agent-development-implementations',
      status: 'experimental',
      effectiveStatus: 'experimental',
      enabled: false,
    };
    fixture.detectChanges();

    let root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('Experimental contract');
    expect(root.textContent).toContain('disabled by default');
  });

  it('makes deprecated lifecycle behavior explicit', () => {
    component.framework = {
      ...framework,
      id: 'legacy-framework',
      status: 'deprecated',
      effectiveStatus: 'deprecated',
      enabled: false,
    };
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('Deprecated contract');
    expect(root.textContent).toContain('never eligible for new selections');
  });

  it('provides recoverable error and explicit empty states', () => {
    const retrySpy = spyOn(component.retry, 'emit');
    component.framework = undefined;
    component.errorMessage = 'The current framework record is unavailable.';
    fixture.detectChanges();

    let root = fixture.nativeElement as HTMLElement;
    const retry = Array.from(root.querySelectorAll('button'))
      .find((button) => button.textContent?.includes('Retry framework'));
    expect(root.querySelector('[role="alert"]')?.textContent)
      .toContain('Framework contract unavailable');
    retry?.click();
    expect(retrySpy).toHaveBeenCalled();

    component.errorMessage = '';
    fixture.detectChanges();

    root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('[role="status"]')?.textContent)
      .toContain('No framework selected');
  });

  it('exposes every remaining framework contract field through an accessible disclosure', () => {
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const summary = root.querySelector<HTMLButtonElement>('#framework-full-contract-summary');

    expect(summary?.tagName).toBe('BUTTON');
    expect(summary?.getAttribute('aria-expanded')).toBe('false');
    expect(summary?.getAttribute('aria-controls')).toBe('framework-full-contract');

    summary?.focus();
    expect(document.activeElement).toBe(summary);
    summary?.click();
    fixture.detectChanges();

    const contract = root.querySelector<HTMLElement>('#framework-full-contract');
    const expectedContractText = [
      'Suitable problem types',
      'factual answer',
      'Trigger conditions',
      'verify',
      'Required inputs',
      'source record',
      'Produced outputs',
      'verified claim record',
      'Required agents',
      'evidence_agent',
      'Workflow template',
      'retrieve',
      'Decision rules',
      'do not guess',
      'Safety invariants',
      'unsupported claims cannot become facts',
      'Evidence requirements',
      'source link',
      'Evaluation method',
      'claim support',
      'Conflicts with',
      'ungrounded-answering',
      'User-specific adaptations',
      'Prefer Robert-owned project sources',
      'Effective owner adaptations',
      'Draft only for legal claims',
      'Candidate implementations',
      'local evidence index',
    ];

    expect(summary?.getAttribute('aria-expanded')).toBe('true');
    expect(contract?.getAttribute('role')).toBe('region');
    expect(contract?.getAttribute('aria-labelledby')).toBe('framework-full-contract-summary');
    expectedContractText.forEach((value) => expect(contract?.textContent).toContain(value));
  });

  it('resets advanced contract disclosure when a different framework is inspected', () => {
    component.fullContractExpanded = true;

    component.framework = {
      ...framework,
      id: 'planning-loop',
      name: 'Planning loop',
    };

    expect(component.fullContractExpanded).toBeFalse();
  });

  it('makes empty optional contract fields explicit instead of omitting their labels', () => {
    component.framework = {
      ...framework,
      conflictsWith: [],
      userSpecificAdaptations: [],
      adaptations: [],
      candidateImplementations: [],
    };
    component.fullContractExpanded = true;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;

    expect(root.textContent).toContain('Conflicts with');
    expect(root.textContent).toContain('No catalog conflicts declared.');
    expect(root.textContent).toContain('User-specific adaptations');
    expect(root.textContent).toContain('No built-in user adaptations.');
    expect(root.textContent).toContain('Effective owner adaptations');
    expect(root.textContent).toContain('No owner preference overlays applied.');
    expect(root.textContent).toContain('Candidate implementations');
    expect(root.textContent).toContain('No candidates listed.');
  });
});

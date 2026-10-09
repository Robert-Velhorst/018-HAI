import { CommonModule } from '@angular/common';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzEmptyModule } from 'ng-zorro-antd/empty';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputNumberModule } from 'ng-zorro-antd/input-number';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { of, throwError } from 'rxjs';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { AmbientService } from '../../services/ambient.service';
import { AutonomyService } from '../../services/autonomy.service';
import { AmbientBrainComponent } from './ambient-brain.component';

const preferenceKey = 'hai.module-view.v1.ambient-brain';

const opportunity = {
  id: 'opportunity-1',
  needKey: 'security',
  title: 'Review account recovery options',
  rationale: 'A recovery option needs review.',
  nextAction: 'Compare the available recovery methods.',
  priorityScore: 87,
  urgency: 72,
  impact: 80,
  effort: 20,
  confidence: 0.9,
  risk: 82,
  requiresApproval: true,
  status: 'proposed',
  lastSeenAt: '2026-09-24T10:00:00.000Z',
} as any;

const ambientOverview = {
  generatedAt: '2026-09-24T10:00:00.000Z',
  policy: {},
  needs: [{
    id: 'need-1',
    key: 'security',
    name: 'Safety and security',
    description: 'Protect essential accounts.',
    currentLevel: 55,
    targetLevel: 80,
    priorityWeight: 70,
    enabled: true,
    notes: '',
    updatedAt: '2026-09-24T10:00:00.000Z',
  }],
  opportunities: [opportunity],
  scans: [],
  warnings: [],
} as any;

const autonomyOverview = {
  generatedAt: '2026-09-24T10:00:00.000Z',
  metrics: {
    attempts: 0,
    rawCompletions: 0,
    completionUnderPolicy: 0,
    policyViolations: 0,
    invalidActions: 0,
    humanInterventions: 0,
    recoveryAttempts: 0,
    recovered: 0,
    averageLatencyMillis: 0,
    rawCompletionRate: 0,
    policyCompletionRate: 0,
    interventionRate: 0,
    recoveryRate: 0,
  },
  recentActions: [],
  recentEvaluations: [],
  recentStressRuns: [],
  decisionDiscipline: {
    name: 'YAGNI gatekeeper',
    enabled: true,
    order: [],
    newDependenciesDefault: 'blocked',
    benchmarkClaims: 'unverified',
  },
  warnings: [],
} as any;

const completedScan = {
  id: '6f9a7ce0-626d-4c81-a055-951d9752e9b1',
  trigger: 'manual',
  status: 'completed',
  startedAt: '2026-09-24T10:00:00.000Z',
  completedAt: '2026-09-24T10:00:01.000Z',
  itemsExamined: 1,
  opportunitiesFound: 1,
  created: 1,
  updated: 0,
  deduplicated: 0,
  advanced: 0,
  filtered: 0,
  skipped: 0,
  blocked: 0,
  manifestBytes: 256,
  deduplicatedBytes: 0,
} as any;

describe('AmbientBrainComponent progressive view', () => {
  let fixture: ComponentFixture<AmbientBrainComponent>;
  let ambient: jasmine.SpyObj<AmbientService>;
  let autonomy: jasmine.SpyObj<AutonomyService>;
  let notifications: jasmine.SpyObj<NzNotificationService>;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    localStorage.removeItem(preferenceKey);
    document.body.classList.remove('hai-view-advanced');
    ambient = jasmine.createSpyObj<AmbientService>('AmbientService', ['overview', 'scan', 'updateNeed', 'accept', 'dismiss']);
    ambient.overview.and.returnValue(of(ambientOverview));
    ambient.scan.and.returnValue(of(completedScan));
    ambient.updateNeed.and.returnValue(of(ambientOverview.needs[0]));
    ambient.accept.and.returnValue(of({ ...opportunity, status: 'accepted' }));
    ambient.dismiss.and.returnValue(of({ ...opportunity, status: 'dismissed' }));
    autonomy = jasmine.createSpyObj<AutonomyService>('AutonomyService', ['overview', 'runStressSuite']);
    autonomy.overview.and.returnValue(of(autonomyOverview));
    autonomy.runStressSuite.and.returnValue(of({ run: { passed: 1, failed: 0 }, results: [] } as any));
    notifications = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'warning', 'error', 'info']);

    await TestBed.configureTestingModule({
      declarations: [AmbientBrainComponent],
      imports: [
        CommonModule,
        FormsModule,
        ControlRoomModule,
        NzButtonModule,
        NzCheckboxModule,
        NzEmptyModule,
        NzIconModule,
        NzInputNumberModule,
        NzInputModule,
        NzLayoutModule,
        NzModalModule,
        NzRadioModule,
        NzTableModule,
        NzTagModule,
      ],
      providers: [
        { provide: AmbientService, useValue: ambient },
        { provide: AutonomyService, useValue: autonomy },
        { provide: NzNotificationService, useValue: notifications },
        { provide: Router, useValue: jasmine.createSpyObj<Router>('Router', ['navigate']) },
      ],
    }).compileComponents();

    preferences = TestBed.inject(ModuleViewPreferencesService);
    fixture = TestBed.createComponent(AmbientBrainComponent);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem(preferenceKey);
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
    document.body.classList.remove('hai-view-advanced');
  });

  it('keeps primary actions, approval context, and source-backed risk visible in Basic', () => {
    const page: HTMLElement = fixture.nativeElement;

    expect(preferences.get('ambient-brain').mode).toBe('basic');
    expect(page.textContent).toContain('Scan for open loops');
    expect(page.textContent).toContain('Review next opportunity');
    expect(page.textContent).toContain('approval');
    expect(page.textContent).toContain('risk 82');
    expect(page.querySelector('.action-strip--advanced')).toBeNull();
    expect(page.querySelector('[aria-label="Filter opportunities by status"]')).toBeNull();
  });

  it('names icon-only navigation, workflow, and source controls', () => {
    const page: HTMLElement = fixture.nativeElement;
    expect(page.querySelector('button[aria-label="Open automation registry"]')).not.toBeNull();
    expect(page.querySelector('button[aria-label="Open automation registry"] i')?.getAttribute('aria-hidden')).toBe('true');
  });

  it('keeps the need editor open and preserves the original values when a save fails', () => {
    const original = fixture.componentInstance.overview!.needs[0];
    fixture.componentInstance.openNeed(original);
    fixture.componentInstance.selectedNeed!.currentLevel = 12;
    ambient.updateNeed.and.returnValue(throwError(() => new Error('offline')));

    fixture.componentInstance.saveNeed(fixture.componentInstance.selectedNeed!);

    expect(fixture.componentInstance.selectedNeed?.currentLevel).toBe(12);
    expect(fixture.componentInstance.overview!.needs[0].currentLevel).toBe(original.currentLevel);
    expect(fixture.componentInstance.savingNeed).toBe('');
    expect(fixture.componentInstance.errorMessage).toBe('');
    expect(notifications.error).toHaveBeenCalledWith(
      'Priority update unconfirmed',
      'HAI could not confirm the update. Your draft is retained; inspect current state before retrying.'
    );
  });

  it('updates the displayed need and closes the editor only after the save succeeds', () => {
    const original = fixture.componentInstance.overview!.needs[0];
    const saved = { ...original, currentLevel: 63 };
    fixture.componentInstance.openNeed(original);
    fixture.componentInstance.selectedNeed!.currentLevel = saved.currentLevel;
    ambient.updateNeed.and.returnValue(of(saved));

    fixture.componentInstance.saveNeed(fixture.componentInstance.selectedNeed!);

    expect(fixture.componentInstance.overview!.needs[0].currentLevel).toBe(63);
    expect(fixture.componentInstance.selectedNeed).toBeUndefined();
  });

  it('distinguishes an unrun or unavailable scan from a completed scan with zero reviewed items', () => {
    let scanTile = fixture.nativeElement.querySelector('.action-tile--primary') as HTMLElement;
    expect(scanTile.textContent).toContain('No scan yet');
    expect(scanTile.textContent).toContain('Not run');
    expect(scanTile.textContent).not.toContain('0 reviewed');

    const completedOverview = {
      ...ambientOverview,
      scans: [{
        id: 'bed67004-a01c-49df-93d7-8bc09c550948',
        trigger: 'manual',
        status: 'completed',
        startedAt: '2026-09-24T10:00:00.000Z',
        completedAt: '2026-09-24T10:00:01.000Z',
        itemsExamined: 0,
        opportunitiesFound: 0,
        created: 0,
        updated: 0,
        deduplicated: 0,
        advanced: 0,
        filtered: 0,
        skipped: 0,
        blocked: 0,
        manifestBytes: 0,
        deduplicatedBytes: 0,
      }],
    } as any;
    fixture.destroy();
    ambient.overview.and.returnValue(of(completedOverview));
    fixture = TestBed.createComponent(AmbientBrainComponent);
    fixture.detectChanges();
    scanTile = fixture.nativeElement.querySelector('.action-tile--primary') as HTMLElement;
    expect(scanTile.textContent).toContain('0 reviewed');

    const failedOverview = {
      ...ambientOverview,
      scans: [{
        id: 'scan-failed',
        trigger: 'manual',
        status: 'failed',
        startedAt: '2026-09-24T10:00:00.000Z',
        itemsExamined: 0,
        opportunitiesFound: 0,
        created: 0,
        updated: 0,
        deduplicated: 0,
        advanced: 0,
        filtered: 0,
        skipped: 0,
        blocked: 0,
        manifestBytes: 0,
        deduplicatedBytes: 0,
        errorMessage: 'Source unavailable',
      }],
    } as any;
    fixture.destroy();
    ambient.overview.and.returnValue(of(failedOverview));
    fixture = TestBed.createComponent(AmbientBrainComponent);
    fixture.detectChanges();
    scanTile = fixture.nativeElement.querySelector('.action-tile--primary') as HTMLElement;
    expect(scanTile.textContent).toContain('Unavailable');
    expect(scanTile.textContent).not.toContain('0 reviewed');
  });

  it('routes the Basic scan action through AmbientService and refreshes its verified scan record', () => {
    const scanButton = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.includes('Scan for open loops'));
    expect(scanButton).toBeDefined();

    scanButton!.click();
    fixture.detectChanges();

    expect(ambient.scan).toHaveBeenCalledOnceWith();
    expect(ambient.overview).toHaveBeenCalledTimes(2);
    expect(notifications.info).toHaveBeenCalledWith('Completed scan record received', jasmine.any(String));
  });

  it('keeps an unconfirmed guard run visible in Basic with a working status refresh path', () => {
    autonomy.runStressSuite.and.returnValue(throwError(() => ({ error: { error: 'Guard runner unavailable.' } })));

    preferences.setMode('ambient-brain', 'advanced');
    document.body.classList.add('hai-view-advanced');
    fixture.detectChanges();
    const engineControls = fixture.nativeElement.querySelector('hai-progressive-section') as HTMLElement;
    (engineControls.querySelector('.hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();
    const guardButton = Array.from(engineControls.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.includes('Test guards'));
    expect(guardButton).toBeDefined();
    guardButton!.click();

    expect(fixture.componentInstance.errorMessage).toBe('The deterministic guard suite could not be confirmed. Refresh diagnostics before retrying.');
    preferences.setMode('ambient-brain', 'basic');
    document.body.classList.remove('hai-view-advanced');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('The deterministic guard suite could not be confirmed.');
    const refreshButton = fixture.nativeElement.querySelector('[role="alert"] button') as HTMLButtonElement;
    expect(refreshButton?.textContent).toContain('Refresh status');
    refreshButton.click();
    expect(autonomy.overview).toHaveBeenCalledTimes(2);
    expect(ambient.overview).toHaveBeenCalledTimes(2);
  });

  it('persists Engine Controls and History disclosures independently for Ambient Brain', () => {
    preferences.setMode('ambient-brain', 'advanced');
    document.body.classList.add('hai-view-advanced');
    fixture.detectChanges();
    const sections = fixture.nativeElement.querySelectorAll('hai-progressive-section') as NodeListOf<HTMLElement>;
    expect(sections.length).toBe(2);

    (sections[0].querySelector('.hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(sections[0].querySelector('.action-strip--advanced')).not.toBeNull();
    expect(preferences.get('ambient-brain').openSections['engine-controls']).toBeTrue();

    (sections[1].querySelector('.hai-progressive-section__summary') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(sections[1].querySelector('[aria-label="Filter opportunities by status"]')).not.toBeNull();
    expect(preferences.get('ambient-brain').openSections['opportunity-history']).toBeTrue();
    expect(preferences.get('pursuits').openSections).toEqual({});
  });

  it('supports direct fragment opening for Engine Controls', () => {
    fixture.destroy();
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#engine-controls`);
    fixture = TestBed.createComponent(AmbientBrainComponent);
    fixture.detectChanges();

    expect(fixture.componentInstance.isAdvancedView).toBeTrue();
    expect(preferences.get('ambient-brain').openSections['engine-controls']).toBeTrue();
    expect(fixture.nativeElement.querySelector('.action-strip--advanced')).not.toBeNull();
    expect(fixture.nativeElement.textContent).toContain('Test guards');
  });
});

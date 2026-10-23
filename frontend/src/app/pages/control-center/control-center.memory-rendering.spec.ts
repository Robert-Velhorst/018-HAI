import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NZ_ICONS } from 'ng-zorro-antd/icon';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { HAI_ICONS } from '../../hai-icons';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { ControlCenterComponent } from './control-center.component';
import { ControlCenterModule } from './control-center.module';

describe('Control Center rendered memory freshness', () => {
  let fixture: ComponentFixture<ControlCenterComponent>;
  let http: HttpTestingController;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ControlCenterModule, HttpClientTestingModule, RouterTestingModule, NoopAnimationsModule],
      providers: [
        { provide: NZ_ICONS, useValue: HAI_ICONS },
        { provide: NzNotificationService, useValue: { error: () => undefined } },
        { provide: ModuleViewPreferencesService, useFactory: () => new ModuleViewPreferencesService(document) },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('control-center');
    fixture = TestBed.createComponent(ControlCenterComponent);
    fixture.detectChanges();
    http.expectOne('/api/v1/workflow/dashboard').flush({
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
    });
    http.expectOne('/api/v1/ambient/overview?view=summary').flush({
      generatedAt: '2026-10-23T00:00:00Z', policy: {}, needs: [], opportunities: [], scans: [], warnings: [],
    });
    http.expectOne('/api/v1/pursuits/dashboard?view=summary').flush({
      counts: { active: 0, needsRobert: 0, blocked: 0, stale: 0 },
      decisionQueue: [], needsRobert: [], vaReady: [], systemReady: [], blocked: [], stale: [],
      reviewDue: [], planningNeeded: [], recentlyChanged: [], highRisk: [], completionCandidates: [],
    });
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('control-center');
    http?.verify();
  });

  function memoryPanel(): HTMLElement {
    preferences.setMode('control-center', 'advanced');
    preferences.setSection('control-center', 'context-audit', true);
    fixture.detectChanges();
    return fixture.nativeElement.querySelector('#memory') as HTMLElement;
  }

  function read(panel: HTMLElement) {
    panel.querySelector<HTMLButtonElement>('[data-testid="panel-memory-load"]')!.click();
    fixture.detectChanges();
    const request = http.expectOne('/api/v1/memory/?includeArchived=false&limit=20');
    expect(request.request.method).toBe('GET');
    return request;
  }

  it('keeps memory and diagnostics unloaded in Basic', () => {
    expect(fixture.nativeElement.querySelector('#memory')).toBeNull();
    http.expectNone(() => true);
  });

  it('shows an empty state only after the explicit memory read completes successfully', () => {
    const panel = memoryPanel();
    expect(panel.textContent).toContain('Memory stays unloaded by default');
    read(panel).flush([]);
    fixture.detectChanges();
    expect(panel.textContent).toContain('No recent memory updates');
  });

  it('does not present the previous empty result as current while refresh is pending', () => {
    const panel = memoryPanel();
    read(panel).flush([]);
    fixture.detectChanges();
    const pending = read(panel);
    expect(panel.textContent).not.toContain('No recent memory updates');
    expect(panel.querySelector('[data-testid="memory-load-pending"]')?.getAttribute('role')).toBe('status');
    expect(panel.textContent).toContain('Checking recent memory updates');
    pending.flush([]);
  });

  it('does not confirm empty memory alongside a failed refresh warning', () => {
    const panel = memoryPanel();
    read(panel).flush([]);
    fixture.detectChanges();
    read(panel).flush({}, { status: 503, statusText: 'Fixture unavailable' });
    fixture.detectChanges();
    expect(panel.textContent).toContain('Memory updates could not be refreshed');
    expect(panel.textContent).not.toContain('No recent memory updates');
  });

  it('retries through the visible warning and restores confirmed empty state after success', () => {
    const panel = memoryPanel();
    read(panel).flush({}, { status: 503, statusText: 'Fixture unavailable' });
    fixture.detectChanges();
    panel.querySelector<HTMLButtonElement>('[data-testid="memory-load-retry"]')!.click();
    http.expectOne('/api/v1/memory/?includeArchived=false&limit=20').flush([]);
    fixture.detectChanges();
    expect(panel.querySelector('[data-testid="memory-load-error"]')).toBeNull();
    expect(panel.textContent).toContain('No recent memory updates');
  });

  it('keeps previously loaded records visible when their refresh fails', () => {
    const panel = memoryPanel();
    read(panel).flush([{
      id: 'fixture-memory', summary: 'Previously recorded context', content: 'Fixture evidence',
      confidence: 0.8, kind: 'lesson', projectKey: 'fixture', createdAt: '2026-10-23T00:00:00Z',
    }]);
    fixture.detectChanges();
    read(panel).flush({}, { status: 503, statusText: 'Fixture unavailable' });
    fixture.detectChanges();
    expect(panel.textContent).toContain('Previously recorded context');
    expect(panel.textContent).toContain('latest state is unconfirmed');
    expect(panel.textContent).not.toContain('No recent memory updates');
  });
});

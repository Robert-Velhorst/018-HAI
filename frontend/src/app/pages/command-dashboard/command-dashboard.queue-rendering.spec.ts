import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NZ_ICONS } from 'ng-zorro-antd/icon';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { HAI_ICONS } from '../../hai-icons';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { CommandDashboardComponent } from './command-dashboard.component';
import { CommandDashboardModule } from './command-dashboard.module';

describe('Command Dashboard rendered queue ownership', () => {
  let fixture: ComponentFixture<CommandDashboardComponent>;
  let http: HttpTestingController;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [CommandDashboardModule, HttpClientTestingModule, RouterTestingModule, NoopAnimationsModule],
      providers: [
        { provide: NZ_ICONS, useValue: HAI_ICONS },
        { provide: NzNotificationService, useValue: { error: () => undefined } },
        { provide: ModuleViewPreferencesService, useFactory: () => new ModuleViewPreferencesService(document) },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('command-dashboard');
    fixture = TestBed.createComponent(CommandDashboardComponent);
    fixture.detectChanges();
    http.expectOne('/api/v1/agent-runtimes/overview').flush({ runtimes: [], health: [] });
    http.expectOne('/api/v1/assistant/logs').flush([]);
    const secondaryReads = http.match(request => request.url.startsWith('/api/v1/pursuits/'));
    expect(secondaryReads.length).toBe(2);
    secondaryReads.forEach(request => request.flush({}, { status: 503, statusText: 'Secondary fixture unavailable' }));
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('command-dashboard');
    http?.verify();
  });

  function render(overrides: Record<string, unknown> = {}): void {
    http.expectOne('/api/v1/memory-engine/dashboard').flush({
      generatedAt: '2026-10-23T00:00:00Z', conversationCount: 0, insightCount: 0,
      needsRobert: [], delegateToVA: [], openLoops: [], contradictions: [],
      recentDecisions: [], sourceCorrections: [], projects: [], recentArchives: [], warnings: [],
      ...overrides,
    });
    fixture.detectChanges();
  }

  function card(title: string): HTMLElement {
    const found = Array.from((fixture.nativeElement as HTMLElement).querySelectorAll<HTMLElement>('nz-card'))
      .find(item => item.querySelector('.ant-card-head-title')?.textContent?.trim() === title);
    expect(found).toBeDefined();
    return found!;
  }

  it('places a confirmed empty open-loop message only in the Open Loops section', () => {
    render();
    expect(card('Open Loops').textContent).toContain('No open loops were reported in this snapshot.');
    expect(card('Contradictions').textContent).not.toContain('No open loops');
  });

  it('places omitted workflow-queue context with Open Loops instead of contradictions', () => {
    render({ warnings: ['Workflow queues are shown through the owner-scoped pursuits dashboard'] });
    expect(card('Open Loops').textContent).toContain('this memory snapshot omitted them');
    expect(card('Open Loops').textContent).not.toContain('No open loops were reported');
    expect(card('Contradictions').textContent).not.toContain('memory snapshot omitted them');
  });

  it('provides contradictions with their own snapshot-scoped empty state', () => {
    render();
    expect(card('Contradictions').textContent).toContain('No contradictions were reported in this snapshot.');
  });

  it('keeps populated queue records in their own sections without empty claims', () => {
    render({
      openLoops: [{ id: 'fixture-loop', waitingFor: 'Awaiting fixture document', nextAction: 'Review document', responsibleParty: 'Fixture owner' }],
      contradictions: [{ id: 'fixture-claim', text: 'Conflicting fixture dates', sourceLabel: 'Fixture source', sourceUri: 'https://example.test/source' }],
    });
    expect(card('Open Loops').textContent).toContain('Awaiting fixture document');
    expect(card('Open Loops').textContent).not.toContain('Conflicting fixture dates');
    expect(card('Contradictions').textContent).toContain('Conflicting fixture dates');
    expect(card('Contradictions').textContent).not.toContain('Awaiting fixture document');
    expect(card('Open Loops').textContent).not.toContain('No open loops');
    expect(card('Contradictions').textContent).not.toContain('No contradictions');
    http.expectNone(request => request.method !== 'GET');
  });
});

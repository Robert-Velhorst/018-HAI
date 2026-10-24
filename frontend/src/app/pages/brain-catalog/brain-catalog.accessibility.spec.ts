import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { RouterTestingModule } from '@angular/router/testing';
import { NZ_ICONS } from 'ng-zorro-antd/icon';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { HAI_ICONS } from '../../hai-icons';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { BrainCatalogComponent } from './brain-catalog.component';
import { BrainCatalogModule } from './brain-catalog.module';

describe('Brain catalog rendered disclosure and capability search', () => {
  let fixture: ComponentFixture<BrainCatalogComponent>;
  let http: HttpTestingController;
  let preferences: ModuleViewPreferencesService;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [BrainCatalogModule, HttpClientTestingModule, RouterTestingModule, NoopAnimationsModule],
      providers: [
        { provide: NZ_ICONS, useValue: HAI_ICONS },
        { provide: NzNotificationService, useValue: { error: () => undefined } },
        { provide: ModuleViewPreferencesService, useFactory: () => new ModuleViewPreferencesService(document) },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    preferences = TestBed.inject(ModuleViewPreferencesService);
    preferences.reset('brain-catalog');
    fixture = TestBed.createComponent(BrainCatalogComponent);
    fixture.detectChanges();
    http.expectOne('/api/v1/brain-catalog/').flush({
      entries: [], planeCoverage: [], discoverySources: [],
      collectionScreening: { total: 0, represented: 0, candidates: 0, reference: 0, deferred: 0, entries: [] },
    });
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture?.destroy();
    preferences?.reset('brain-catalog');
    http?.verify();
  });

  function openSection(id: string): HTMLElement {
    preferences.setMode('brain-catalog', 'advanced');
    fixture.detectChanges();
    const section = fixture.nativeElement.querySelector(`[data-hai-section="${id}"]`) as HTMLElement;
    expect(section).not.toBeNull();
    section.querySelector<HTMLButtonElement>('button.hai-progressive-section__summary')!.click();
    fixture.detectChanges();
    return section;
  }

  it('starts in Basic without loading Advanced histories, bridge states or skill inventory', () => {
    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('No adapter review is queued');
    expect(root.querySelector('[data-hai-section]')).toBeNull();
    http.expectNone(() => true);
  });

  it('loads histories through the rendered OSS Insight disclosure and shows failures without launching scans', () => {
    const section = openSection('oss-insight-screening');
    const collection = http.expectOne('/api/v1/brain-catalog/collection-revalidation-history?limit=5');
    const discovery = http.expectOne('/api/v1/brain-catalog/repository-discovery-revalidation-history?limit=5');
    expect(collection.request.method).toBe('GET');
    expect(discovery.request.method).toBe('GET');
    collection.flush({}, { status: 503, statusText: 'Fixture unavailable' });
    discovery.flush({}, { status: 503, statusText: 'Fixture unavailable' });
    fixture.detectChanges();
    expect(section.textContent).toContain('Persisted collection history is unavailable');
    expect(section.textContent).toContain('Persisted repository gap-review history is unavailable');
    expect(section.textContent).toContain('did not start a replacement scan');
    http.expectNone(request => request.method !== 'GET');
  });

  it('gives the capability input an accessible name independent of its placeholder', () => {
    const section = openSection('capability-match');
    const input = section.querySelector<HTMLInputElement>('input')!;
    expect(input.getAttribute('aria-label')).toBe('Capability need');
    input.focus();
    expect(document.activeElement).toBe(input);
  });

  it('retains the catalog with a stale warning and retries through the rendered refresh action', () => {
    const root = fixture.nativeElement as HTMLElement;
    const refresh = root.querySelector<HTMLButtonElement>('header button')!;
    refresh.click();
    http.expectOne('/api/v1/brain-catalog/').flush({}, { status: 503, statusText: 'Fixture unavailable' });
    fixture.detectChanges();
    const warning = root.querySelector<HTMLElement>('.brain-catalog-page__load-error')!;
    expect(warning.getAttribute('role')).toBe('alert');
    expect(warning.textContent).toContain('Existing catalog data is still shown and may be stale');
    expect(root.textContent).toContain('No adapter review is queued');
    warning.querySelector<HTMLButtonElement>('button')!.click();
    http.expectOne('/api/v1/brain-catalog/').flush({
      entries: [], planeCoverage: [], discoverySources: [],
      collectionScreening: { total: 0, represented: 0, candidates: 0, reference: 0, deferred: 0, entries: [] },
    });
    fixture.detectChanges();
    expect(root.querySelector('.brain-catalog-page__load-error')).toBeNull();
    expect(root.textContent).toContain('No adapter review is queued');
  });

  it('submits one capability search through the real service and releases the rendered pending control', () => {
    const section = openSection('capability-match');
    const input = section.querySelector<HTMLInputElement>('input')!;
    input.value = '  local model evaluation  ';
    input.dispatchEvent(new Event('input'));
    const button = Array.from(section.querySelectorAll('button')).find(item => item.textContent?.trim() === 'Find capability')!;
    button.click();
    fixture.detectChanges();
    expect(button.disabled).toBeTrue();
    const request = http.expectOne('/api/v1/brain-catalog/recommend');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({ need: 'local model evaluation' });
    button.click();
    http.expectNone('/api/v1/brain-catalog/recommend');
    request.flush({ recommendations: [], expandedTerms: [], message: 'No reviewed match for this fixture.' });
    fixture.detectChanges();
    expect(button.disabled).toBeFalse();
    expect(section.textContent).toContain('0 reviewed matches');
    expect(section.textContent).toContain('No reviewed match for this fixture.');
    http.expectNone(() => true);
  });
});

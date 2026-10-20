import { NEVER, of, Subject, throwError } from 'rxjs';
import { CommonModule } from '@angular/common';
import { ComponentFixture, fakeAsync, TestBed, tick } from '@angular/core/testing';
import { CUSTOM_ELEMENTS_SCHEMA } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { ISystemInfo, ISystemReadiness } from '../../models/system-status.model.interface';
import { SYSTEM_STATUS_SERVICE_TOKEN } from '../../services/system-status/system-status.service.token';
import { SystemStatusComponent } from './system-status.component';
import { AuthSessionService } from '../../services/auth-session.service';

const sampleSystemInfo: ISystemInfo = {
  build: { version: '1.2.3', commit: 'abcdef1234567890', buildTime: '2026-10-11T10:00:00Z', goVersion: 'go1.27.3' },
  runMode: 'production', allowsRealSideEffects: false, languages: ['en', 'nl'],
  readiness: { ready: true, ok: 2, warn: 0, fail: 0 },
};

describe('SystemStatusComponent', () => {
  it('does not overlap scheduled readiness refreshes', () => {
    const response = new Subject<ISystemReadiness>();
    const service = { readiness: jasmine.createSpy('readiness').and.returnValue(response.asObservable()), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    const notification = { error: jasmine.createSpy('error') };
    const component = new SystemStatusComponent(service as never, notification as never, new ModuleViewPreferencesService(document));

    component.refresh();
    component.refresh(true);

    expect(service.readiness).toHaveBeenCalledTimes(1);

    response.next({
      status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [],
    });
    response.complete();
    component.refresh(true);

    expect(service.readiness).toHaveBeenCalledTimes(2);
  });

  it('refreshes once when a hidden status page becomes visible again', fakeAsync(() => {
    const response = new Subject<ISystemReadiness>();
    const service = { readiness: jasmine.createSpy('readiness').and.returnValue(response.asObservable()), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    const notification = { error: jasmine.createSpy('error') };
    const component = new SystemStatusComponent(service as never, notification as never, new ModuleViewPreferencesService(document));
    const hidden = spyOnProperty(document, 'hidden', 'get').and.returnValue(false);

    component.ngOnInit();
    expect(service.readiness).toHaveBeenCalledTimes(1);

    hidden.and.returnValue(true);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(service.readiness).toHaveBeenCalledTimes(1);

    hidden.and.returnValue(false);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(service.readiness).toHaveBeenCalledTimes(1);

    response.next({
      status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [],
    });
    response.complete();
    document.dispatchEvent(new Event('visibilitychange'));
    expect(service.readiness).toHaveBeenCalledTimes(2);
    component.ngOnDestroy();
  }));

  it('backs off while healthy and polls faster for degraded and recovery states', fakeAsync(() => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false);
    const service = { readiness: jasmine.createSpy('readiness'), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    service.readiness.and.returnValues(
      of({ status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [] }),
      of({ status: 'degraded', service: 'backend', summary: { ok: 0, warn: 1, fail: 0 }, checks: [] }),
      of({ status: 'not_ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 1 }, checks: [] }),
      of({ status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [] })
    );
    const component = new SystemStatusComponent(
      service as never,
      { error: jasmine.createSpy('error') } as never,
      new ModuleViewPreferencesService(document)
    );

    component.ngOnInit();
    tick(119999);
    expect(service.readiness).toHaveBeenCalledTimes(1);
    tick(1);
    expect(service.readiness).toHaveBeenCalledTimes(2);
    tick(59999);
    expect(service.readiness).toHaveBeenCalledTimes(2);
    tick(1);
    expect(service.readiness).toHaveBeenCalledTimes(3);
    tick(14999);
    expect(service.readiness).toHaveBeenCalledTimes(3);
    tick(1);
    expect(service.readiness).toHaveBeenCalledTimes(4);
    component.ngOnDestroy();
    tick(120000);
    expect(service.readiness).toHaveBeenCalledTimes(4);
  }));

  it('pauses healthy polling while hidden and refreshes immediately on return', fakeAsync(() => {
    const service = {
      readiness: jasmine.createSpy('readiness').and.returnValue(of({
        status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [],
      })),
      info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)),
    };
    const component = new SystemStatusComponent(
      service as never,
      { error: jasmine.createSpy('error') } as never,
      new ModuleViewPreferencesService(document)
    );
    const hidden = spyOnProperty(document, 'hidden', 'get').and.returnValue(false);

    component.ngOnInit();
    hidden.and.returnValue(true);
    document.dispatchEvent(new Event('visibilitychange'));
    tick(120000);
    expect(service.readiness).toHaveBeenCalledTimes(1);

    hidden.and.returnValue(false);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(service.readiness).toHaveBeenCalledTimes(2);
    component.ngOnDestroy();
  }));

  it('times out a stalled readiness request and schedules a degraded retry', fakeAsync(() => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false);
    const service = { readiness: jasmine.createSpy('readiness').and.returnValue(NEVER), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    const component = new SystemStatusComponent(
      service as never,
      { error: jasmine.createSpy('error') } as never,
      new ModuleViewPreferencesService(document)
    );

    component.ngOnInit();
    tick(10000);
    expect(component.loadError).toBeTrue();
    tick(59999);
    expect(service.readiness).toHaveBeenCalledTimes(1);
    tick(1);
    expect(service.readiness).toHaveBeenCalledTimes(2);
    component.ngOnDestroy();
  }));

  it('preserves last good status and explains that a failed poll may be stale', () => {
    const service = { readiness: jasmine.createSpy('readiness').and.returnValue(throwError(() => new Error('probe unavailable'))), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    const notification = { error: jasmine.createSpy('error') };
    const component = new SystemStatusComponent(service as never, notification as never, new ModuleViewPreferencesService(document));
    const previous: ISystemReadiness = {
      status: 'ready', service: 'backend', summary: { ok: 1, warn: 0, fail: 0 },
      checks: [{ name: 'database.connection', severity: 'ok', detail: 'connected' }],
    };
    component.readiness = previous;
    component.lastUpdated = new Date('2026-09-23T10:00:00Z');

    component.refresh(true);

    expect(component.loadError).toBeTrue();
    expect(component.readiness).toBe(previous);
    expect(component.refreshErrorMessage()).toContain('may be out of date');
    expect(component.refreshErrorMessage()).toContain('Last successful status');
  });

  it('loads build provenance once and keeps it supplemental to readiness', () => {
    const service = {
      readiness: jasmine.createSpy('readiness').and.returnValue(NEVER),
      info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)),
    };
    const component = new SystemStatusComponent(
      service as never,
      { error: jasmine.createSpy('error') } as never,
      new ModuleViewPreferencesService(document)
    );

    component.ngOnInit();

    expect(service.info).toHaveBeenCalledTimes(1);
    expect(component.systemInfo).toEqual(sampleSystemInfo);
    expect(component.buildInfoTitle(sampleSystemInfo)).toContain('abcdef1234567890');
    component.ngOnDestroy();
  });

  it('does not turn unavailable build provenance into a readiness failure', () => {
    const service = {
      readiness: jasmine.createSpy('readiness').and.returnValue(NEVER),
      info: jasmine.createSpy('info').and.returnValue(throwError(() => new Error('metadata unavailable'))),
    };
    const component = new SystemStatusComponent(
      service as never,
      { error: jasmine.createSpy('error') } as never,
      new ModuleViewPreferencesService(document)
    );

    component.ngOnInit();

    expect(component.systemInfo).toBeUndefined();
    expect(component.loadError).toBeFalse();
    component.ngOnDestroy();
  });

  it('provides text severity labels in addition to the status icon and color', () => {
    const component = new SystemStatusComponent({} as never, {} as never, new ModuleViewPreferencesService(document));
    expect(component.severityLabel('ok')).toBe('OK');
    expect(component.severityLabel('warn')).toBe('Warning');
    expect(component.severityLabel('fail')).toBe('Fail');
  });
});

describe('SystemStatusComponent rendered layout and accessibility', () => {
  let fixture: ComponentFixture<SystemStatusComponent>;
  let service: { readiness: jasmine.Spy; info: jasmine.Spy };

  beforeEach(async () => {
    service = { readiness: jasmine.createSpy('readiness'), info: jasmine.createSpy('info').and.returnValue(of(sampleSystemInfo)) };
    service.readiness.and.returnValue(of({
      status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [],
    }));
    await TestBed.configureTestingModule({
      declarations: [SystemStatusComponent],
      imports: [CommonModule, ControlRoomModule, NzButtonModule, NzCardModule, NzIconModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [
        { provide: SYSTEM_STATUS_SERVICE_TOKEN, useValue: service },
        { provide: AuthSessionService, useValue: { session: () => of(undefined) } },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(SystemStatusComponent);
  });

  afterEach(() => {
    fixture?.destroy();
    document.body.classList.remove('hai-theme-dark');
  });

  it('renders a stale-data warning alongside previously loaded checks after refresh failure', () => {
    const readiness: ISystemReadiness = {
      status: 'degraded', service: 'backend', summary: { ok: 0, warn: 1, fail: 0 },
      checks: [{ name: 'redis.connection', severity: 'warn', detail: 'slow response' }],
    };
    service.readiness.and.returnValue(NEVER);
    fixture.componentInstance.readiness = readiness;
    fixture.componentInstance.groups = [{ key: 'redis', title: 'Redis cache', checks: readiness.checks, worst: 'warn' }];
    fixture.componentInstance.recommendedActions = ['redis.connection: slow response'];
    fixture.componentInstance.lastUpdated = new Date();
    fixture.componentInstance.loadError = true;
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    expect(fixture.componentInstance.loadError).toBeTrue();
    expect(fixture.componentInstance.readiness).toBe(readiness);
    expect(root.querySelector('.load-error')?.textContent).toContain('may be out of date');
    expect(root.querySelector('.recommended')?.textContent).toContain('redis.connection: slow response');
    expect(root.querySelector('.group-summary')?.textContent).toContain('Redis cache');
    expect(root.querySelector('hai-progressive-section')?.textContent).not.toContain('redis.connection');
    expect(root.querySelector('.load-error')?.getAttribute('aria-live')).toBe('polite');
  });

  it('renders a compact build identity with accessible full provenance', () => {
    fixture.componentInstance.systemInfo = sampleSystemInfo;
    fixture.detectChanges();

    const buildInfo = fixture.nativeElement.querySelector('.build-info') as HTMLElement;
    expect(buildInfo.textContent).toContain('Running 1.2.3 · abcdef123456');
    expect(buildInfo.getAttribute('title')).toContain('Built 2026-10-11T10:00:00Z');
    expect(buildInfo.getAttribute('aria-label')).toContain('go1.27.3');
    expect(buildInfo.getAttribute('tabindex')).toBe('0');
  });

  it('renders textual severity and avoids horizontal overflow in a 320px container', () => {
    const readiness: ISystemReadiness = {
      status: 'not_ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 1 },
      checks: [{ name: 'database.connection.with.a.long.identifier', severity: 'fail', detail: 'Connection unavailable; verify the configured endpoint.' }],
    };
    service.readiness.and.returnValue(NEVER);
    fixture.componentInstance.readiness = readiness;
    fixture.componentInstance.groups = [{ key: 'database', title: 'Database', checks: readiness.checks, worst: 'fail' }];
    fixture.componentInstance.lastUpdated = new Date();
    const host = fixture.nativeElement as HTMLElement;
    host.style.width = '320px';
    fixture.detectChanges();

    const root = host.querySelector<HTMLElement>('.system-status')!;
    const groupOverview = host.querySelector<HTMLElement>('.group-overview')!;
    expect(host.querySelector('.group-summary nz-tag')?.textContent?.trim()).toBe('Fail');
    expect(groupOverview.scrollWidth).toBeLessThanOrEqual(groupOverview.clientWidth);
    expect(root.scrollWidth).toBeLessThanOrEqual(root.clientWidth);
  });

  it('uses the shared dark-theme text token for the subtitle', () => {
    document.body.classList.add('hai-theme-dark');
    fixture.detectChanges();
    const subtitle = fixture.nativeElement.querySelector('.subtitle') as HTMLElement;
    expect(getComputedStyle(subtitle).color).toBe('rgb(197, 210, 228)');
    document.body.classList.remove('hai-theme-dark');
  });
});

import { CommonModule } from '@angular/common';
import { CUSTOM_ELEMENTS_SCHEMA } from '@angular/core';
import { TestBed, fakeAsync, tick } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { EMPTY, NEVER, of, Subject } from 'rxjs';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule, provideNzIcons } from 'ng-zorro-antd/icon';
import { CheckCircleFill, ReloadOutline } from '@ant-design/icons-angular/icons';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { AuthSessionService } from '../../services/auth-session.service';
import { SystemStatusService } from '../../services/system-status/system-status.service';
import { SYSTEM_STATUS_SERVICE_TOKEN } from '../../services/system-status/system-status.service.token';
import { IAuthSession } from '../../models/auth-session.model.interface';
import { IEventDeliveryStats } from '../../models/system-status.model.interface';
import { SystemStatusComponent } from './system-status.component';

const eventId = '11111111-1111-4111-8111-111111111111';
const owner: IAuthSession = { authenticated: true, subject: 'synthetic-owner', role: 'owner', permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true } };
const stats: IEventDeliveryStats = { pending: 2, published: 3, deadLettered: 1, checkedAt: '2026-10-20T00:00:00Z', recentFailures: [{ id: eventId, aggregateId: eventId, status: 'dead_lettered', eventType: 'update', attemptCount: 5, maxAttempts: 5, lastError: 'private-provider-token', updatedAt: '2026-10-20T00:00:00Z' }] };
const connector = { enabled: true, configured: true, transport: 'local', provider: 'fixture', capabilities: [], restrictions: [], scope: 'planning' };

afterEach(() => localStorage.removeItem('hai.module-view.v1.system-status'));

function plain(session = of<IAuthSession | undefined>(owner)) {
  const preferences = new ModuleViewPreferencesService(document);
  preferences.reset('system-status');
  preferences.setMode('system-status', 'advanced');
  const service = jasmine.createSpyObj('system', ['eventDelivery', 'connectorStatus', 'retryEventDelivery']);
  service.eventDelivery.and.returnValue(of(stats));
  service.connectorStatus.and.returnValue(of(connector));
  service.retryEventDelivery.and.returnValue(of({ status: 'queued', eventId }));
  const auth = { session: jasmine.createSpy('session').and.returnValue(session) };
  const component = new SystemStatusComponent(service, {} as never, preferences, auth as never);
  return { component, service, auth, preferences };
}

describe('System delivery recovery guards', () => {
  it('loads nothing in Basic and rejects viewer/operator and missing administration rights', () => {
    for (const role of ['viewer', 'operator', 'owner'] as const) {
      const f = plain(of({ ...owner, role, permissions: { ...owner.permissions, canAdminister: false } }));
      f.preferences.setMode('system-status', 'basic');
      f.component.onOperationsOpen(true);
      expect(f.auth.session).not.toHaveBeenCalled();
      f.preferences.setMode('system-status', 'advanced');
      f.component.onOperationsOpen(true);
      expect(f.service.eventDelivery).not.toHaveBeenCalled();
      f.component.eventDelivery = stats;
      f.component.requestDeliveryRetry(stats.recentFailures[0]);
      f.component.confirmDeliveryRetry(eventId);
      expect(f.service.retryEventDelivery).not.toHaveBeenCalled();
      expect(f.service.connectorStatus.calls.count()).toBe(role === 'owner' ? 1 : 0);
      f.component.ngOnDestroy();
    }
  });

  it('requires a current row and explicit confirmation and suppresses duplicate retries', () => {
    const f = plain();
    const pending = new Subject<{ status: 'queued'; eventId: string }>();
    f.service.retryEventDelivery.and.returnValue(pending);
    f.component.onOperationsOpen(true);
    f.component.confirmDeliveryRetry(eventId);
    expect(f.service.retryEventDelivery).not.toHaveBeenCalled();
    f.component.requestDeliveryRetry({ ...stats.recentFailures[0], id: 'missing' });
    expect(f.component.retryConfirmationId).toBe('');
    f.component.requestDeliveryRetry(stats.recentFailures[0]);
    f.component.confirmDeliveryRetry(eventId);
    f.component.confirmDeliveryRetry(eventId);
    expect(f.service.retryEventDelivery).toHaveBeenCalledTimes(1);
    pending.next({ status: 'queued', eventId });
    pending.complete();
    expect(f.component.retryingEventId).toBe('');
    expect(f.component.retryMessage).toContain('does not prove delivery');
    expect(f.component.deliveryRetryAllowed(eventId)).toBeFalse();
    f.component.ngOnDestroy();
  });

  it('treats empty and mismatched retry receipts as unverified, never delivered', () => {
    for (const result of [EMPTY, of({ status: 'queued', eventId: 'another-event' }), of({ status: 'delivered', eventId })]) {
      const f = plain();
      f.service.retryEventDelivery.and.returnValue(result);
      f.component.onOperationsOpen(true);
      f.component.requestDeliveryRetry(stats.recentFailures[0]);
      f.component.confirmDeliveryRetry(eventId);
      expect(f.component.retryMessage).toBe('');
      expect(f.component.deliveryError).toContain('Refresh');
      expect(f.component.deliveryRetryAllowed(eventId)).toBeFalse();
      f.component.ngOnDestroy();
    }
  });

  it('times out reads and mutations without automatic retries or false negative outcomes', fakeAsync(() => {
    const f = plain(NEVER);
    f.component.onOperationsOpen(true);
    tick(10001);
    expect(f.component.operationsLoading).toBeFalse();
    expect(f.component.operationsError).toContain('could not be loaded');
    f.auth.session.and.returnValue(of(owner));
    f.component.loadOperationalDetails();
    f.service.retryEventDelivery.and.returnValue(NEVER);
    f.component.requestDeliveryRetry(stats.recentFailures[0]);
    f.component.confirmDeliveryRetry(eventId);
    tick(10001);
    expect(f.component.retryingEventId).toBe('');
    expect(f.component.deliveryError).toContain('may have been accepted');
    expect(f.service.retryEventDelivery).toHaveBeenCalledTimes(1);
    f.component.ngOnDestroy();
  }));

  it('cancels closed-section and destroyed-page reads and suppresses late mutations', () => {
    const session = new Subject<IAuthSession>();
    const f = plain(session);
    f.component.onOperationsOpen(true);
    expect(session.observers.length).toBe(1);
    f.component.onOperationsOpen(false);
    expect(session.observers.length).toBe(0);
    f.auth.session.and.returnValue(of(owner));
    f.component.onOperationsOpen(true);
    const pending = new Subject();
    f.service.retryEventDelivery.and.returnValue(pending);
    f.component.requestDeliveryRetry(stats.recentFailures[0]);
    f.component.confirmDeliveryRetry(eventId);
    f.component.ngOnDestroy();
    expect(pending.observers.length).toBe(0);
    pending.next({ status: 'queued', eventId });
    expect(f.component.retryMessage).toBe('');
    f.component.loadOperationalDetails();
    expect(f.auth.session).toHaveBeenCalledTimes(2);
  });

  it('rejects malformed or empty delivery snapshots and never reuses stale rows for retry', () => {
    for (const result of [EMPTY, of({ ...stats, pending: -1 }), of({ ...stats, recentFailures: null })]) {
      const f = plain();
      f.component.eventDelivery = stats;
      f.service.eventDelivery.and.returnValue(result);
      f.component.onOperationsOpen(true);
      expect(f.component.deliveryError).not.toBe('');
      expect(f.component.deliveryRetryAllowed(eventId)).toBeFalse();
      f.component.ngOnDestroy();
    }
  });
});

describe('System delivery rendered HTTP chain (synthetic fixtures)', () => {
  it('loads only after Advanced disclosure and retries through the actual backend route', async () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(true);
    const preferences = new ModuleViewPreferencesService(document);
    preferences.reset('system-status');
    await TestBed.configureTestingModule({
      declarations: [SystemStatusComponent],
      imports: [CommonModule, ControlRoomModule, NzButtonModule, NzCardModule, NzIconModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [provideHttpClient(), provideHttpClientTesting(), provideNzIcons([CheckCircleFill, ReloadOutline]),
        { provide: SYSTEM_STATUS_SERVICE_TOKEN, useClass: SystemStatusService },
        { provide: ModuleViewPreferencesService, useValue: preferences }],
    }).compileComponents();
    const fixture = TestBed.createComponent(SystemStatusComponent);
    const http = TestBed.inject(HttpTestingController);
    fixture.detectChanges();
    http.expectOne('/api/v1/system/info').flush(null, { status: 503, statusText: 'Unavailable' });
    http.expectOne('/api/v1/system/readiness').flush({ status: 'ready', service: 'backend', summary: { ok: 0, warn: 0, fail: 0 }, checks: [] });
    http.expectNone('/api/v1/auth/session');
    preferences.setMode('system-status', 'advanced');
    preferences.setSection('system-status', 'system-delivery-details', true);
    http.expectOne('/api/v1/auth/session').flush(owner);
    http.expectOne('/api/v1/automation/events/delivery').flush(stats);
    http.expectOne('/api/v1/a2a/status').flush(connector);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('.operational-details')?.textContent).toContain('not a live connectivity');
    expect(root.textContent).not.toContain('private-provider-token');
    (root.querySelector('.delivery-records button') as HTMLButtonElement).click();
    fixture.detectChanges();
    http.expectNone((request) => request.method === 'POST');
    const queue = Array.from(root.querySelectorAll<HTMLButtonElement>('.delivery-confirmation button')).find((button) => button.textContent?.includes('Queue retry'))!;
    queue.click();
    queue.click();
    const retry = http.expectOne(`/api/v1/automation/events/delivery/${eventId}/retry`);
    expect(retry.request.method).toBe('POST');
    retry.flush({ status: 'queued', eventId }, { status: 202, statusText: 'Accepted' });
    fixture.detectChanges();
    expect(root.textContent).toContain('does not prove delivery');
    expect(root.querySelectorAll('.delivery-records li').length).toBe(0);
    http.verify();
    fixture.destroy();
  });
});

import { fakeAsync, tick } from '@angular/core/testing';
import { Subject, of } from 'rxjs';
import { ISystemReadiness } from '../../models/system-status.model.interface';
import { ISystemStatusService } from '../../services/system-status/system-status.service.interface';
import { SystemStatusComponent } from './system-status.component';

describe('SystemStatusComponent polling', () => {
  const readiness: ISystemReadiness = {
    status: 'ready',
    service: 'backend',
    summary: { ok: 1, warn: 0, fail: 0 },
    checks: [{ name: 'server.port', severity: 'ok', detail: 'listening' }],
  };

  function service(readinessResult = of(readiness)): jasmine.SpyObj<ISystemStatusService> {
    const result = jasmine.createSpyObj<ISystemStatusService>(
      'SystemStatusService',
      ['readiness', 'eventDelivery', 'connectorStatus', 'retryEventDelivery']
    );
    result.readiness.and.returnValue(readinessResult);
    result.eventDelivery.and.returnValue(of({
      pending: 0,
      deadLettered: 0,
      published: 0,
      recentFailures: [],
      checkedAt: new Date(0).toISOString(),
    }));
    result.connectorStatus.and.returnValue(of({
      enabled: true,
      configured: true,
      provider: 'A2A local planning bridge',
      endpoint: 'http://127.0.0.1:8088/api/v1/a2a',
      capabilities: ['non-executable planning'],
      restrictions: ['no execution'],
      scope: 'One configured local peer can request a planning draft.',
      transport: 'local',
    }));
    return result;
  }

  it('polls healthy systems every two minutes and pauses completely while hidden', fakeAsync(() => {
    const statusService = service();
    const notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);
    let hidden = false;
    const hiddenSpy = spyOnProperty(document, 'hidden', 'get').and.callFake(() => hidden);
    const component = new SystemStatusComponent(statusService, notification, document);

    component.ngOnInit();
    expect(statusService.readiness).toHaveBeenCalledTimes(1);

    tick(119999);
    expect(statusService.readiness).toHaveBeenCalledTimes(1);
    tick(1);
    expect(statusService.readiness).toHaveBeenCalledTimes(2);

    hidden = true;
    document.dispatchEvent(new Event('visibilitychange'));
    tick(240000);
    expect(statusService.readiness).toHaveBeenCalledTimes(2);

    hidden = false;
    document.dispatchEvent(new Event('visibilitychange'));
    expect(statusService.readiness).toHaveBeenCalledTimes(3);

    component.ngOnDestroy();
    hiddenSpy.and.callThrough();
  }));

  it('times out a stalled readiness request and retries on the degraded interval', fakeAsync(() => {
    const response = new Subject<ISystemReadiness>();
    const statusService = service(response);
    const notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);
    const component = new SystemStatusComponent(statusService, notification, document);

    component.ngOnInit();
    tick(9999);
    expect(statusService.readiness).toHaveBeenCalledTimes(1);
    expect(component.loading).toBeTrue();

    tick(1);
    expect(component.loading).toBeFalse();
    expect(component.loadError).toBeTrue();
    expect(notification.error).toHaveBeenCalled();

    tick(59999);
    expect(statusService.readiness).toHaveBeenCalledTimes(1);
    tick(1);
    expect(statusService.readiness).toHaveBeenCalledTimes(2);

    component.ngOnDestroy();
  }));

  it('checks critical recovery every fifteen seconds', fakeAsync(() => {
    const notReady: ISystemReadiness = { ...readiness, status: 'not_ready' };
    const statusService = service(of(notReady));
    const notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);
    const component = new SystemStatusComponent(statusService, notification, document);

    component.ngOnInit();
    tick(14999);
    expect(statusService.readiness).toHaveBeenCalledTimes(1);
    tick(1);
    expect(statusService.readiness).toHaveBeenCalledTimes(2);

    component.ngOnDestroy();
  }));

  it('ranks failures before warnings and links known subsystems to recovery controls', () => {
    const degraded: ISystemReadiness = {
      status: 'not_ready',
      service: 'backend',
      summary: { ok: 0, warn: 2, fail: 1 },
      checks: [
        { name: 'runtime.mode', severity: 'warn', detail: 'runtime needs review' },
        { name: 'llm.provider', severity: 'warn', detail: 'no provider configured' },
        { name: 'database.connection', severity: 'fail', detail: 'database unavailable' },
      ],
    };
    const statusService = service(of(degraded));
    const notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);
    const component = new SystemStatusComponent(statusService, notification, document);

    component.ngOnInit();

    expect(component.recommendedActions.map((action) => action.checkName)).toEqual([
      'database.connection',
      'llm.provider',
      'runtime.mode',
    ]);
    expect(component.recommendedActions[0].route).toBeUndefined();
    expect(component.recommendedActions[1]).toEqual(jasmine.objectContaining({
      route: '/llm-policy',
      actionLabel: 'Open model controls',
    }));
    expect(component.recommendedActions[2]).toEqual(jasmine.objectContaining({
      route: '/runtime-control',
      actionLabel: 'Open runtime controls',
    }));

    component.ngOnDestroy();
  });

  it('distinguishes local, cloud, disabled, and invalid connector states', () => {
    const statusService = service();
    const notification = jasmine.createSpyObj('NzNotificationService', ['error', 'success']);
    const component = new SystemStatusComponent(statusService, notification, document);

    component.ngOnInit();
    expect(component.connectorStateLabel()).toBe('Local-only connector');
    expect(component.connectorStateClass()).toBe('connector-ok');

    component.connectorStatus = {
      ...component.connectorStatus!,
      transport: 'fixed_ngrok_https',
    };
    expect(component.connectorStateLabel()).toBe('Governed cloud connector');

    component.connectorStatus = {
      ...component.connectorStatus!,
      enabled: false,
      configured: false,
    };
    expect(component.connectorStateLabel()).toBe('Connector disabled');
    expect(component.connectorStateClass()).toBe('connector-off');

    component.connectorStatus = {
      ...component.connectorStatus!,
      enabled: true,
      configured: false,
      configError: 'owner identity is missing',
    };
    expect(component.connectorStateLabel()).toBe('Configuration required');
    expect(component.connectorStateClass()).toBe('connector-warn');

    component.ngOnDestroy();
  });
});

import { Router } from '@angular/router'
import { of, Subject, throwError } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { AccountBridgesService } from '../../services/account-bridges.service'
import { AccountBridgesComponent } from './account-bridges.component'

describe('AccountBridgesComponent refresh', () => {
  it('keeps a visible recovery state when account bridge data cannot load', () => {
    const service = jasmine.createSpyObj<AccountBridgesService>('AccountBridgesService', ['bridges', 'feeds'])
    service.bridges.and.returnValue(throwError(() => new Error('gateway unavailable')))
    service.feeds.and.returnValue(of({ feeds: [] }))
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['error'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new AccountBridgesComponent(service, notification, router)

    component.refresh()

    expect(component.overviewLoadError).toContain('account bridges')
    expect(component.loading).toBeFalse()
  })
})
describe('AccountBridgesComponent', () => {
  function componentWith(service: any): AccountBridgesComponent {
    return new AccountBridgesComponent(
      service,
      jasmine.createSpyObj<NzNotificationService>('notification', ['success', 'error']),
      jasmine.createSpyObj('router', ['navigate']),
    );
  }

  it('prevents duplicate sync-all requests until the active request completes', () => {
    const pending = new Subject<any>();
    const service = jasmine.createSpyObj('AccountBridgesService', ['syncDue']);
    service.syncDue.and.returnValue(pending.asObservable());
    const component = componentWith(service);

    component.syncAll();
    component.syncAll();
    expect(service.syncDue).toHaveBeenCalledTimes(1);
    expect(component.syncingAll).toBeTrue();

    pending.complete();
    expect(component.syncingAll).toBeFalse();
  });
});

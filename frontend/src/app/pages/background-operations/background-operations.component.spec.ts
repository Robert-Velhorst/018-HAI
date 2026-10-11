import { HttpErrorResponse } from '@angular/common/http'
import { Router } from '@angular/router'
import { Subject, of, throwError } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IOperation } from '../../models/background-operations.model.interface'
import { BackgroundOperationsService } from '../../services/background-operations.service'
import { BackgroundOperationsComponent } from './background-operations.component'

describe('BackgroundOperationsComponent action submission', () => {
  function createComponent(service: jasmine.SpyObj<BackgroundOperationsService>) {
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning', 'info'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)
    component.feeds = [{ enabled: true } as any]
    spyOn(component, 'refresh')
    return { component, notification }
  }

  const operation = {
    id: '5c09e7a8-53dd-4f2c-9a74-3f16b706d0a7',
    title: 'Refresh local briefing',
  } as IOperation

  it('submits an approval only once while the first request is pending', () => {
    const approval = new Subject<IOperation>()
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['approve'])
    service.approve.and.returnValue(approval.asObservable())
    const { component } = createComponent(service)

    component.approve(operation)
    component.approve(operation)

    expect(service.approve).toHaveBeenCalledTimes(1)
    expect(component.isOperationActionPending(operation)).toBeTrue()

    approval.next(operation)
    approval.complete()

    expect(component.isOperationActionPending(operation)).toBeFalse()
  })

  it('submits a safe-worker run only once while the first request is pending', () => {
    const run = new Subject<{ operation: IOperation; verified: boolean; failed: boolean }>()
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['run'])
    service.run.and.returnValue(run.asObservable())
    const { component } = createComponent(service)

    component.run(operation)
    component.run(operation)

    expect(service.run).toHaveBeenCalledTimes(1)
    expect(component.isOperationActionPending(operation)).toBeTrue()

    run.next({ operation, verified: true, failed: false })
    run.complete()

    expect(component.isOperationActionPending(operation)).toBeFalse()
  })

  it('clears the pending marker after a rejected request so the operator can recover', () => {
    const approval = new Subject<IOperation>()
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['approve'])
    service.approve.and.returnValue(approval.asObservable())
    const { component } = createComponent(service)

    component.approve(operation)
    approval.error(new Error('request rejected'))

    expect(component.isOperationActionPending(operation)).toBeFalse()
  })
})

describe('BackgroundOperationsComponent refresh', () => {

  it('loads the page through one overview request instead of three independent requests', () => {
    const service = jasmine.createSpyObj<any>('BackgroundOperationsService', ['overview', 'dashboard', 'list', 'feeds'])
    service.overview.and.returnValue(of({ dashboard: {}, operations: [], feeds: [] }))
    service.dashboard.and.returnValue(of({}))
    service.list.and.returnValue(of({ operations: [] }))
    service.feeds.and.returnValue(of({ feeds: [] }))
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)

    component.refresh()

    expect(service.overview).toHaveBeenCalledTimes(1)
    expect(service.dashboard).not.toHaveBeenCalled()
    expect(service.list).not.toHaveBeenCalled()
    expect(service.feeds).not.toHaveBeenCalled()
  })

  it('keeps a visible recovery state when the operational dashboard cannot load', () => {
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['overview'])
    service.overview.and.returnValue(throwError(() => new Error('gateway unavailable')))
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)

    component.refresh()

    expect(component.overviewLoadError).toContain('operational dashboard')
    expect(component.loading).toBeFalse()
  })

  it('does not start overlapping dashboard batches while a refresh is pending', () => {
    const overview = new Subject<any>()
    const queuedOverview = new Subject<any>()
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['overview'])
    service.overview.and.returnValues(overview.asObservable(), queuedOverview.asObservable())
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)

    component.refresh()
    component.refresh()

    expect(service.overview).toHaveBeenCalledTimes(1)

    overview.next({ dashboard: {}, operations: [], feeds: [] })
    overview.complete()

    expect(service.overview).toHaveBeenCalledTimes(2)

    queuedOverview.next({ dashboard: {}, operations: [], feeds: [] })
    queuedOverview.complete()

    expect(component.loading).toBeFalse()
  })
})

describe('BackgroundOperationsComponent background run recovery', () => {
  it('explains a busy background pass without exposing the internal error text', () => {
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['runBackground'])
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)

    const message = component.backgroundRunError({
      status: 409,
      error: { error: 'background: run already in progress' },
    })

    expect(message).toBe('A background pass is already running. Wait a moment, then refresh this page.')
  })

  it('does not submit a second background pass while the first request is pending', () => {
    const backgroundRun = new Subject<any>()
    const service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', ['runBackground'])
    service.runBackground.and.returnValue(backgroundRun.asObservable())
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error', 'warning', 'info'])
    const router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    const component = new BackgroundOperationsComponent(service, notification, router)
    component.feeds = [{ enabled: true } as any]
    spyOn(component, 'refresh')

    component.runBackground()
    component.runBackground()

    expect(service.runBackground).toHaveBeenCalledTimes(1)

    backgroundRun.next({ operationsCreated: 0, verified: 0, awaitingApproval: 0 })
    backgroundRun.complete()
  })
})

describe('BackgroundOperationsComponent', () => {
  function make(): BackgroundOperationsComponent {
    const service = {
      dashboard: () => of({}),
      list: () => of({ operations: [] }),
      feeds: () => of({ feeds: [] }),
      events: () => of({ events: [] }),
    }
    const notification = jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info'])
    const router = jasmine.createSpyObj('router', ['navigate'])
    return new BackgroundOperationsComponent(service as any, notification, router)
  }

  it('does not start a pass until an account feed is enabled', () => {
    const component = make()

    component.runBackground()

    expect(component.lastRunNotice).toContain('No enabled account feed')
    expect(component.lastRunError).toBe('')
    expect(component.running).toBeFalse()
  })

  it('reports whether a configured feed can be scanned', () => {
    const component = make()
    component.feeds = [{ name: 'Trello', provider: 'trello', accountLabel: 'Robert', sourceType: 'trello', enabled: true }]

    expect(component.hasEnabledFeeds()).toBeTrue()
  })

  it('gives an owner-permission recovery path for authorization failures', () => {
    const component = make()

    const message = component.backgroundRunError(new HttpErrorResponse({ status: 403 }))

    expect(message).toContain('owner account')
  })

  it('does not present backend unavailability as an operation failure', () => {
    const component = make()

    const message = component.backgroundRunError(new HttpErrorResponse({ status: 503 }))

    expect(message).toContain('temporarily unavailable')
    expect(message).toContain('were not changed')
  })
})

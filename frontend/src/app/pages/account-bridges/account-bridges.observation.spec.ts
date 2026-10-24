import { fakeAsync, tick } from '@angular/core/testing'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { EMPTY, of, Subject } from 'rxjs'
import { IFeedHealth, ISyncReport } from '../../models/account-bridges.model.interface'
import { AccountBridgesService } from '../../services/account-bridges.service'
import { AccountBridgesComponent } from './account-bridges.component'

describe('Account bridges request observation reconciliation', () => {
  let component: AccountBridgesComponent
  let service: jasmine.SpyObj<AccountBridgesService>
  let notifications: jasmine.SpyObj<NzNotificationService>
  const feed: IFeedHealth = {
    feed: { id: 'feed', name: 'Synthetic feed', provider: 'local_folder', accountLabel: 'Test', sourceType: 'autoposter_api', enabled: true },
    connectionStatus: 'available', lastItemsRead: 0,
  }
  const report: ISyncReport = { feedId: 'feed', itemsRead: 1, operationsCreated: 0, operationsRefreshed: 0, privacyFlagged: 0, recorded: true }

  beforeEach(() => {
    service = jasmine.createSpyObj<AccountBridgesService>('service', ['bridges', 'feeds', 'sync', 'syncDue', 'setEnabled'])
    service.bridges.and.returnValue(of({ bridges: [] }))
    service.feeds.and.returnValue(of({ feeds: [feed] }))
    service.sync.and.returnValue(of(report))
    service.syncDue.and.returnValue(of({ reports: [report] }))
    service.setEnabled.and.returnValue(of({ ...feed.feed, enabled: false }))
    notifications = jasmine.createSpyObj<NzNotificationService>('notifications', ['success', 'warning', 'error'])
    component = new AccountBridgesComponent(service, notifications, jasmine.createSpyObj<Router>('router', ['navigate']))
    component.ngOnInit()
  })

  afterEach(() => component.ngOnDestroy())

  it('marks an empty feed observation unavailable without replacing last-known evidence', () => {
    const retained = component.feeds
    const freshness = component.lastLoadedAt
    service.feeds.and.returnValue(EMPTY)
    component.refresh()
    expect(component.loading).toBeFalse()
    expect(component.feedLoadError).not.toBe('')
    expect(component.feeds).toBe(retained)
    expect(component.lastLoadedAt).toBe(freshness)
    component.sync(feed)
    expect(service.sync).not.toHaveBeenCalled()
  })

  it('accepts the first feed observation without requiring stream completion', () => {
    const pending = new Subject<{ feeds: IFeedHealth[] }>()
    service.feeds.and.returnValue(pending)
    component.refresh()
    pending.next({ feeds: [] })
    expect(component.loading).toBeFalse()
    expect(pending.observers.length).toBe(0)
    expect(component.feeds).toEqual([])
  })

  it('contains a synchronous request-setup failure and retains the other resource', () => {
    service.bridges.and.callFake(() => { throw new Error('private setup diagnostic') })
    expect(() => component.refresh()).not.toThrow()
    expect(component.loading).toBeFalse()
    expect(component.bridgeLoadError).not.toBe('')
    expect(component.loadError).not.toContain('private setup diagnostic')
    expect(component.feedLoadError).toBe('')
  })

  it('does not start reads or mutations after destruction', () => {
    service.feeds.calls.reset()
    service.bridges.calls.reset()
    component.ngOnDestroy()
    component.refresh()
    component.sync(feed)
    component.syncAll()
    component.setEnabled(feed, false)
    expect(service.feeds).not.toHaveBeenCalled()
    expect(service.bridges).not.toHaveBeenCalled()
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(service.setEnabled).not.toHaveBeenCalled()
  })

  it('keeps an empty single sync uncertain and refreshes without replaying it', () => {
    service.sync.and.returnValue(EMPTY)
    component.sync(feed)
    expect(component.syncing['feed']).toBeFalse()
    expect(component.feedback?.kind).toBe('error')
    expect(component.feedback?.text).toContain('could not be confirmed')
    expect(service.sync).toHaveBeenCalledTimes(1)
    expect(service.feeds).toHaveBeenCalledTimes(2)
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('keeps empty bulk sync and state-change observations uncertain', () => {
    service.syncDue.and.returnValue(EMPTY)
    component.syncAll()
    expect(component.feedback?.kind).toBe('error')
    expect(component.syncingAll).toBeFalse()
    service.setEnabled.and.returnValue(EMPTY)
    component.setEnabled(feed, false)
    expect(component.feedback?.kind).toBe('error')
    expect(component.feedback?.text).toContain('do not assume the change was saved')
    expect(component.changing['feed']).toBeFalse()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('accepts at most one mutation result and contains synchronous mutation setup failure', () => {
    const pending = new Subject<ISyncReport>()
    service.sync.and.returnValue(pending)
    component.sync(feed)
    pending.next(report)
    expect(pending.observers.length).toBe(0)
    pending.next(report)
    expect(notifications.success).toHaveBeenCalledTimes(1)
    service.sync.and.callFake(() => { throw new Error('private setup diagnostic') })
    expect(() => component.sync(feed)).not.toThrow()
    expect(component.syncing['feed']).toBeFalse()
    expect(component.feedback?.kind).toBe('error')
    expect(component.feedback?.text).not.toContain('private setup diagnostic')
  })

  it('bounds a stalled feed observation and keeps its previous snapshot', fakeAsync(() => {
    const retained = component.feeds
    const pending = new Subject<{ feeds: IFeedHealth[] }>()
    service.feeds.and.returnValue(pending)
    component.refresh()
    tick(6001)
    expect(pending.observers.length).toBe(0)
    expect(component.loading).toBeFalse()
    expect(component.feedLoadError).not.toBe('')
    expect(component.feeds).toBe(retained)
  }))

  it('retains successful feed observations when only the bridge stream is empty', () => {
    service.bridges.and.returnValue(EMPTY)
    service.feeds.and.returnValue(of({ feeds: [] }))
    component.refresh()
    expect(component.bridgeLoadError).not.toBe('')
    expect(component.feedLoadError).toBe('')
    expect(component.feedCountsKnown).toBeTrue()
    expect(component.feeds).toEqual([])
  })

  it('takes only the first bulk and source-state acknowledgement', () => {
    const bulk = new Subject<{ reports: ISyncReport[] }>()
    service.syncDue.and.returnValue(bulk)
    component.syncAll()
    bulk.next({ reports: [report] })
    expect(bulk.observers.length).toBe(0)
    expect(component.syncingAll).toBeFalse()
    bulk.next({ reports: [report] })
    const state = new Subject<typeof feed.feed>()
    service.setEnabled.and.returnValue(state)
    component.setEnabled(feed, false)
    state.next({ ...feed.feed, enabled: false })
    expect(state.observers.length).toBe(0)
    expect(component.changing['feed']).toBeFalse()
    state.next({ ...feed.feed, enabled: true })
    expect(notifications.success).toHaveBeenCalledTimes(2)
  })

  it('bounds all three mutations without retrying an uncertain write', fakeAsync(() => {
    const single = new Subject<ISyncReport>()
    service.sync.and.returnValue(single)
    component.sync(feed)
    tick(30001)
    expect(single.observers.length).toBe(0)
    expect(component.syncing['feed']).toBeFalse()
    expect(component.feedback?.kind).toBe('error')
    expect(service.sync).toHaveBeenCalledTimes(1)

    const bulk = new Subject<{ reports: ISyncReport[] }>()
    service.syncDue.and.returnValue(bulk)
    component.syncAll()
    tick(30001)
    expect(bulk.observers.length).toBe(0)
    expect(component.syncingAll).toBeFalse()
    expect(component.feedback?.kind).toBe('error')
    expect(service.syncDue).toHaveBeenCalledTimes(1)

    const state = new Subject<typeof feed.feed>()
    service.setEnabled.and.returnValue(state)
    component.setEnabled(feed, false)
    tick(30001)
    expect(state.observers.length).toBe(0)
    expect(component.changing['feed']).toBeFalse()
    expect(component.feedback?.kind).toBe('error')
    expect(service.setEnabled).toHaveBeenCalledTimes(1)
    expect(notifications.success).not.toHaveBeenCalled()
  }))
})

import { Router } from '@angular/router'
import { of, Subject, throwError } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IBridgeContract, IFeed, IFeedHealth, ISyncReport } from '../../models/account-bridges.model.interface'
import { AccountBridgesService } from '../../services/account-bridges.service'
import { AccountBridgesComponent } from './account-bridges.component'

describe('AccountBridgesComponent sync outcomes', () => {
  let service: jasmine.SpyObj<AccountBridgesService>
  let notification: jasmine.SpyObj<NzNotificationService>
  let component: AccountBridgesComponent
  const feed: IFeedHealth = {
    feed: { id: 'feed', name: 'Autoposter', provider: 'secondhand_autoposter', accountLabel: 'owner', sourceType: 'autoposter_api', enabled: true },
    connectionStatus: 'available', syncState: 'idle', lastItemsRead: 0,
  }
  const report = (extra: Partial<ISyncReport> = {}): ISyncReport => ({
    feedId: 'feed', itemsRead: 10, operationsCreated: 8, operationsRefreshed: 2, privacyFlagged: 0,
    recorded: true, errors: [], ...extra,
  })

  beforeEach(() => {
    service = jasmine.createSpyObj<AccountBridgesService>('service', ['sync', 'syncDue', 'setEnabled', 'feeds', 'bridges'])
    notification = jasmine.createSpyObj<NzNotificationService>('notification', ['success', 'error', 'warning'])
    component = new AccountBridgesComponent(service, notification, jasmine.createSpyObj<Router>('router', ['navigate']))
    spyOn(component, 'refresh')
  })

  it('does not report a response containing sync errors as successful', () => {
    service.sync.and.returnValue(of(report({ errors: ['Autoposter HTTP 401'] })))
    component.sync(feed)
    expect(notification.warning).toHaveBeenCalled()
    expect(notification.success).not.toHaveBeenCalled()
    expect(component.syncing['feed']).toBeFalse()
  })

  it('does not submit a sync for a disabled feed', () => {
    component.sync({ ...feed, feed: { ...feed.feed, enabled: false } })
    expect(service.sync).not.toHaveBeenCalled()
  })

  it('waits for a persisted disable response and blocks duplicate control and sync calls', () => {
    const result = new Subject<IFeed>()
    service.setEnabled.and.returnValue(result)
    component.feeds = [feed]
    component.setEnabled(feed, false)
    component.setEnabled(feed, false)
    component.sync(feed)
    expect(service.setEnabled).toHaveBeenCalledOnceWith('feed', false)
    expect(service.sync).not.toHaveBeenCalled()
    expect(component.feeds[0].feed.enabled).toBeTrue()
    expect(notification.success).not.toHaveBeenCalled()
    result.next({ ...feed.feed, enabled: false })
    result.complete()
    expect(component.feeds[0].feed.enabled).toBeFalse()
    expect(component.changing['feed']).toBeFalse()
    expect(notification.success).toHaveBeenCalled()
  })

  it('permits disabling an active sync without starting another read', () => {
    service.setEnabled.and.returnValue(of({ ...feed.feed, enabled: false }))
    component.syncing['feed'] = true
    component.setEnabled(feed, false)
    expect(service.setEnabled).toHaveBeenCalledOnceWith('feed', false)
    expect(service.sync).not.toHaveBeenCalled()
  })

  it('does not claim a failed or mismatched control response was saved', () => {
    component.feeds = [feed]
    service.setEnabled.and.returnValue(throwError(() => ({ status: 503 })))
    component.setEnabled(feed, false)
    expect(notification.success).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalled()
    expect(component.feeds[0].feed.enabled).toBeTrue()
    expect(component.changing['feed']).toBeFalse()
    service.setEnabled.and.returnValue(of({ ...feed.feed, id: 'other', enabled: false }))
    component.setEnabled(feed, false)
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('does not let an older refresh overwrite an acknowledged disabled state', () => {
    ;(component.refresh as jasmine.Spy).and.callThrough()
    const oldFeeds = new Subject<{ feeds: IFeedHealth[] }>()
    const oldBridges = new Subject<{ bridges: IBridgeContract[] }>()
    service.feeds.and.returnValues(oldFeeds, of({ feeds: [{ ...feed, feed: { ...feed.feed, enabled: false } }] }))
    service.bridges.and.returnValues(oldBridges, of({ bridges: [] }))
    component.refresh()
    service.setEnabled.and.returnValue(of({ ...feed.feed, enabled: false }))
    component.setEnabled(feed, false)
    oldFeeds.next({ feeds: [feed] }); oldFeeds.complete()
    oldBridges.next({ bridges: [] }); oldBridges.complete()
    expect(component.feeds[0].feed.enabled).toBeFalse()
  })

  it('warns when bounded pagination leaves more work', () => {
    service.sync.and.returnValue(of(report({ hasMore: true })))
    component.sync(feed)
    expect(notification.warning).toHaveBeenCalled()
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('describes reference imports and tombstones without promising execution', () => {
    service.sync.and.returnValue(of(report({ tombstonesApplied: 1 })))
    component.sync(feed)
    expect(notification.success.calls.mostRecent().args[1]).toContain('reference revisions')
    expect(notification.success.calls.mostRecent().args[1]).toContain('1 deletion markers processed')
  })

  it('does not claim sync-all succeeded when a feed reports an error', () => {
    component.feeds = [feed]
    service.syncDue.and.returnValue(of({ reports: [report(), report({ errors: ['Storage failed'] })] }))
    component.syncAll()
    expect(notification.warning).toHaveBeenCalled()
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('warns when sync-all has more pages to process and names the recovery action', () => {
    component.feeds = [feed]
    service.syncDue.and.returnValue(of({ reports: [report({ hasMore: true })] }))
    component.syncAll()
    expect(notification.warning).toHaveBeenCalled()
    expect(notification.warning.calls.mostRecent().args[1]).toContain('More source pages remain')
    expect(notification.warning.calls.mostRecent().args[1]).toContain('Sync again')
    expect(notification.success).not.toHaveBeenCalled()
  })
})

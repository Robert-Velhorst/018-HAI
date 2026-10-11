import { HttpClient } from '@angular/common/http'
import { ComponentFixture, TestBed, fakeAsync, tick } from '@angular/core/testing'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { EMPTY, Observable, of, Subject, throwError } from 'rxjs'
import { IBridgeContract, IFeedHealth, IFeedIdentityPreview, ISyncReport } from '../../models/account-bridges.model.interface'
import { AccountBridgesService } from '../../services/account-bridges.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { AccountBridgesComponent } from './account-bridges.component'
import { AccountBridgesModule } from './account-bridges.module'

describe('AccountBridgesComponent', () => {
  const preferenceKey = 'hai.module-view.v1.account-bridges'
  let fixture: ComponentFixture<AccountBridgesComponent>
  let service: jasmine.SpyObj<AccountBridgesService>
  let notifications: jasmine.SpyObj<NzNotificationService>
  let router: jasmine.SpyObj<Router>
  const utcText = (value: string): string => new Date(value).toLocaleString(undefined, { timeZone: 'UTC', timeZoneName: 'short' })

  const availableFeed = (overrides: Partial<IFeedHealth> = {}): IFeedHealth => ({
    feed: {
      id: 'feed-calendar',
      name: 'Calendar feed',
      provider: 'local_folder',
      accountLabel: 'Personal calendar',
      sourceType: 'local_json_file',
      enabled: true,
    },
    connectionStatus: 'available',
    lastSyncedAt: '2026-09-24T10:00:00Z',
    lastItemsRead: 3,
    ...overrides,
  })

  const contractOnlyFeed = (): IFeedHealth => ({
    feed: {
      id: 'feed-trello',
      name: 'Trello export',
      provider: 'trello',
      accountLabel: 'Project board',
      sourceType: 'manual_export',
      enabled: true,
    },
    connectionStatus: 'contract_only',
    lastItemsRead: 0,
  })

  const bridge = (status = 'available'): IBridgeContract => ({
    provider: 'generic_json_feed',
    displayName: 'Generic JSON Feed',
    connectorPreference: ['local_export'],
    readOnly: true,
    requiredScopes: [],
    itemTypes: ['email', 'document'],
    setupRequirements: [{ step: 'Choose a feed file', detail: 'Use a normalized JSON feed.' }],
    connectionStatus: status,
  })

  const identityPreview = (overrides: Partial<IFeedIdentityPreview> = {}): IFeedIdentityPreview => ({
    feedId: 'feed-calendar',
    observedAt: '2026-10-01T12:05:00Z',
    scope: 'current_local_feed_active_operations',
    historicalInventoryComplete: false,
    itemsObserved: 4,
    itemsInspected: 4,
    truncated: false,
    counts: { unseen: 1, canonical: 1, historical: 1, coexisting: 1 },
    items: [
      { externalId: 'source-unseen', title: 'Unseen item', state: 'unseen' },
      { externalId: 'source-canonical', title: 'Canonical item', state: 'canonical', canonicalOperationId: 'canonical-1' },
      { externalId: 'source-historical', title: 'Historical item', state: 'historical', historicalOperationId: 'historical-1' },
      { externalId: 'source-coexisting', title: 'Coexisting item', state: 'coexisting', canonicalOperationId: 'canonical-2', historicalOperationId: 'historical-2' },
    ],
    ...overrides,
  })

  const mountPage = (data: { feeds?: IFeedHealth[]; bridges?: IBridgeContract[] }): void => {
    if (data.feeds) service.feeds.and.returnValue(of({ feeds: data.feeds }))
    if (data.bridges) service.bridges.and.returnValue(of({ bridges: data.bridges }))
    fixture.destroy()
    fixture = TestBed.createComponent(AccountBridgesComponent)
    fixture.detectChanges()
  }

  const openAdvancedSection = (index: number): HTMLElement => {
    TestBed.inject(ModuleViewPreferencesService).setMode('account-bridges', 'advanced')
    document.body.classList.add('hai-view-advanced')
    fixture.detectChanges()
    const section = fixture.nativeElement.querySelectorAll('hai-progressive-section')[index] as HTMLElement
    ;(section.querySelector('.hai-progressive-section__summary') as HTMLButtonElement).click()
    fixture.detectChanges()
    return section
  }

  const openFeedInspector = (): HTMLButtonElement => {
    openAdvancedSection(0)
    const select = fixture.nativeElement.querySelector('[aria-label="Inspect feed: Calendar feed"]') as HTMLButtonElement
    select.click()
    fixture.detectChanges()
    return fixture.nativeElement.querySelector('[aria-label="Inspect source identities: Calendar feed"]') as HTMLButtonElement
  }

  beforeEach(async () => {
    localStorage.removeItem(preferenceKey)
    service = jasmine.createSpyObj<AccountBridgesService>(
      'AccountBridgesService',
      ['bridges', 'permissions', 'feeds', 'sync', 'syncDue', 'audit', 'identityPreview'],
    )
    service.bridges.and.returnValue(of({ bridges: [bridge()] }))
    service.feeds.and.returnValue(of({ feeds: [availableFeed()] }))
    service.sync.and.returnValue(of({
      feedId: 'feed-calendar',
      itemsRead: 3,
      operationsCreated: 1,
      operationsRefreshed: 2,
      privacyFlagged: 0,
      recorded: true,
    }))
    service.syncDue.and.returnValue(of({ reports: [] }))
    service.identityPreview.and.returnValue(of(identityPreview()))
    notifications = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'warning', 'error'])
    router = jasmine.createSpyObj<Router>('Router', ['navigate'])

    await TestBed.configureTestingModule({
      imports: [AccountBridgesModule],
      providers: [
        { provide: AccountBridgesService, useValue: service },
        { provide: NzNotificationService, useValue: notifications },
        { provide: Router, useValue: router },
        ModuleViewPreferencesService,
      ],
    }).compileComponents()

    fixture = TestBed.createComponent(AccountBridgesComponent)
    fixture.detectChanges()
  })

  afterEach(() => {
    fixture?.destroy()
    localStorage.removeItem(preferenceKey)
    document.body.classList.remove('hai-view-advanced')
  })

  it('renders the basic feed-health summary and one clear next action', () => {
    const page: HTMLElement = fixture.nativeElement

    expect(page.querySelector('h1')?.textContent).toContain('Account bridges')
    expect(page.querySelector('#ab-overview-title')?.textContent).toContain('Feed health')
    expect(page.querySelector('.ab__live-state')?.textContent).toContain('1 of 1 feeds available to sync')
    expect(page.querySelector('.ab__next-action h3')?.textContent).toContain('Calendar feed')
    expect(page.querySelectorAll('hai-progressive-section').length).toBe(2)
    expect(page.querySelectorAll('.ab__advanced').length).toBe(0)
    expect(page.querySelector('.ab__actions')?.getAttribute('role')).toBe('group')
    expect(page.querySelector('.ab__metrics')?.getAttribute('role')).toBe('group')
  })

  it('uses the existing module-scoped preference key and defaults safely to Basic', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    expect(preferences.get('account-bridges').mode).toBe('basic')

    preferences.setMode('account-bridges', 'advanced')

    expect(preferences.get('account-bridges').mode).toBe('advanced')
    expect(preferences.get('connected-sources').mode).toBe('basic')
  })

  it('persists each Advanced section independently for this module', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setMode('account-bridges', 'advanced')
    fixture.detectChanges()

    const sections = fixture.nativeElement.querySelectorAll('hai-progressive-section') as NodeListOf<HTMLElement>
    const registeredFeeds = sections[0]
    expect(sections.length).toBe(2)

    const trigger = registeredFeeds.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    trigger.click()
    fixture.detectChanges()

    expect(registeredFeeds.querySelector('.ab__feed-list')).not.toBeNull()
    expect(preferences.get('account-bridges').openSections['registered-feeds']).toBeTrue()
    expect(preferences.get('account-bridges').openSections['bridge-contracts']).toBeUndefined()
    expect(preferences.get('connected-sources').openSections).toEqual({})
  })

  it('announces initial loading without inventing registered feed counts', () => {
    const pendingBridges = new Subject<{ bridges: IBridgeContract[] }>()
    const pendingFeeds = new Subject<{ feeds: IFeedHealth[] }>()
    service.bridges.and.returnValue(pendingBridges.asObservable())
    service.feeds.and.returnValue(pendingFeeds.asObservable())
    fixture.destroy()
    fixture = TestBed.createComponent(AccountBridgesComponent)
    fixture.detectChanges()

    expect(fixture.componentInstance.loading).toBeTrue()
    expect(fixture.nativeElement.querySelector('.ab__live-state')?.textContent).toContain('Checking feed status')
    expect(fixture.componentInstance.feeds.length).toBe(0)

    pendingBridges.next({ bridges: [bridge()] })
    pendingBridges.complete()
    pendingFeeds.next({ feeds: [] })
    pendingFeeds.complete()

    expect(fixture.componentInstance.loading).toBeFalse()
    expect(fixture.componentInstance.feeds.length).toBe(0)
  })

  it('runs only the single selected available feed from the Basic action', () => {
    const button: HTMLButtonElement = fixture.nativeElement.querySelector('.ab__next-action button')
    button.click()
    fixture.detectChanges()

    expect(service.sync).toHaveBeenCalledOnceWith('feed-calendar')
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(fixture.nativeElement.querySelector('.ab__feedback')?.textContent).toContain('3 items read, 1 new operations')
  })

  it('routes to Connected Sources when no feed is available for a safe direct sync', () => {
    mountPage({ feeds: [contractOnlyFeed()] })

    const button: HTMLButtonElement = fixture.nativeElement.querySelector('.ab__next-action button')
    button.click()

    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'])
    expect(service.sync).not.toHaveBeenCalled()
  })

  it('labels unavailable, unverified, and required connection states distinctly', () => {
    const component = fixture.componentInstance

    expect(component.statusLabel('contract_only')).toBe('Unavailable here')
    expect(component.statusLabel('credentials_present_unverified')).toBe('Credentials unverified')
    expect(component.statusLabel('credentials_required')).toBe('Credentials required')
    expect(component.statusLabel('future_status')).toBe('Status unknown')
  })

  it('renders contract details safely when optional API lists are null', () => {
    const nullableContract = {
      ...bridge(),
      itemTypes: null,
      connectorPreference: null,
      requiredScopes: null,
      setupRequirements: null,
    } as unknown as IBridgeContract
    mountPage({ bridges: [nullableContract] })
    openAdvancedSection(1)

    expect(() => fixture.detectChanges()).not.toThrow()
    expect(fixture.nativeElement.textContent).toContain('Not specified')
    expect(fixture.nativeElement.textContent).toContain('No additional setup steps are listed')
  })

  it('shows exact last-attempt counts and does not invent an attempt for a new feed', () => {
    const component = fixture.componentInstance
    const neverSynced = availableFeed({ lastSyncedAt: undefined, lastItemsRead: 0 })
    const emptySync = availableFeed({ lastItemsRead: 0 })

    expect(component.lastAttemptAt(neverSynced)).toBe('No sync attempt recorded')
    expect(component.lastAttemptCount(neverSynced)).toBe('No read count recorded')
    expect(component.lastAttemptCount(emptySync)).toBe('0 items read')
  })

  it('keeps a failed sync attempt separate from successful freshness', () => {
    const component = fixture.componentInstance
    const failedFirstAttempt = availableFeed({
      lastAttemptAt: '2026-10-01T12:01:00Z', lastSyncedAt: undefined, lastItemsRead: 2,
    })
    expect(component.lastAttemptAt(failedFirstAttempt)).toBe(utcText('2026-10-01T12:01:00Z'))
    expect(component.lastAttemptCount(failedFirstAttempt)).toBe('2 items read')
    expect(component.lastSuccessfulSyncAt(failedFirstAttempt)).toBe('No successful sync recorded')

    const failedAfterSuccess = availableFeed({
      lastAttemptAt: '2026-10-01T12:01:00Z', lastSyncedAt: '2026-10-01T12:00:00Z', lastItemsRead: 0,
    })
    expect(component.lastAttemptAt(failedAfterSuccess)).toBe(utcText('2026-10-01T12:01:00Z'))
    expect(component.lastAttemptCount(failedAfterSuccess)).toBe('0 items read')
    expect(component.lastSuccessfulSyncAt(failedAfterSuccess)).toBe(utcText('2026-10-01T12:00:00Z'))
  })

  it('does not hide a malformed latest attempt behind an older success', () => {
    const component = fixture.componentInstance
    const invalidAttempt = availableFeed({ lastAttemptAt: 'not-a-date' })
    expect(component.lastAttemptAt(invalidAttempt)).toBe('Attempt time unavailable')
    expect(component.lastSuccessfulSyncAt(invalidAttempt)).toBe(utcText(invalidAttempt.lastSyncedAt!))
    expect(component.lastSuccessfulSyncAt(availableFeed({ lastSyncedAt: 'not-a-date' })))
      .toBe('Successful sync time unavailable')
  })

  it('keeps bulk sync disabled if any enabled feed is unavailable', () => {
    mountPage({ feeds: [availableFeed(), contractOnlyFeed()] })
    openAdvancedSection(0)

    const button = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((candidate: HTMLButtonElement) => candidate.textContent?.includes('Sync all enabled'))

    expect(fixture.componentInstance.syncAllSafe).toBeFalse()
    expect(button).toBeDefined()
    expect(button!.disabled).toBeTrue()
    fixture.ngZone!.run(() => fixture.componentInstance.syncAll())
    expect(service.syncDue).not.toHaveBeenCalled()
  })

  it('runs guarded bulk sync and reports exact import counts', () => {
    service.syncDue.and.returnValue(of({
      reports: [{
        feedId: 'feed-calendar',
        itemsRead: 4,
        operationsCreated: 1,
        operationsRefreshed: 2,
        privacyFlagged: 1,
        recorded: true,
      }],
    }))

    fixture.ngZone!.run(() => fixture.componentInstance.syncAll())

    expect(service.syncDue).toHaveBeenCalledTimes(1)
    expect(fixture.componentInstance.feedback?.text).toContain('4 items read, 1 new operations, 2 refreshed, 1 privacy-flagged')
  })

  it('keeps historical identity reconciliation visible instead of claiming completion', () => {
    const reason = 'a historical feed identity needs operator reconciliation; existing work was preserved'
    service.sync.and.returnValue(of({
      feedId: 'feed-calendar',
      itemsRead: 1,
      operationsCreated: 0,
      operationsRefreshed: 0,
      privacyFlagged: 0,
      recorded: true,
      errors: [reason],
    }))

    fixture.ngZone!.run(() => fixture.componentInstance.sync(availableFeed()))
    fixture.detectChanges()

    expect(notifications.success).not.toHaveBeenCalled()
    expect(notifications.warning).toHaveBeenCalledTimes(1)
    expect(fixture.componentInstance.feedback?.kind).toBe('warning')
    expect(fixture.componentInstance.feedback?.text).toContain(reason)
    expect(fixture.nativeElement.textContent).toContain('existing work was preserved')
    expect(service.feeds).toHaveBeenCalledTimes(2)
  })

  it('refreshes a concurrently disabled bulk feed without fabricating an import', () => {
    service.syncDue.and.returnValue(of({ reports: [] }))
    const disabled = availableFeed()
    disabled.feed = { ...disabled.feed, enabled: false }
    service.feeds.and.returnValue(of({ feeds: [disabled] }))

    fixture.ngZone!.run(() => fixture.componentInstance.syncAll())
    fixture.detectChanges()

    expect(notifications.success).not.toHaveBeenCalled()
    expect(notifications.warning).toHaveBeenCalledTimes(1)
    expect(fixture.componentInstance.feedback?.text).toContain('No enabled feed was synced')
    expect(fixture.componentInstance.syncAllDisabled).toBeTrue()
    expect(fixture.componentInstance.feeds[0].feed.enabled).toBeFalse()
  })

  it('exposes per-feed sync controls with accessible names and blocks unsupported feeds', () => {
    mountPage({ feeds: [availableFeed(), contractOnlyFeed()] })
    openAdvancedSection(0)

    const available: HTMLButtonElement = fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]')
    const unsupported: HTMLButtonElement = fixture.nativeElement.querySelector('[aria-label="Sync now: Trello export"]')
    available.click()

    expect(available.disabled).toBeFalse()
    expect(unsupported.disabled).toBeTrue()
    expect(service.sync).toHaveBeenCalledOnceWith('feed-calendar')
  })

  it('keeps the existing feed view and exposes a retry when status refresh fails', () => {
    service.bridges.and.returnValues(of({ bridges: [bridge()] }), throwError(() => new Error('offline')))
    mountPage({})
    const refreshButton = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button: HTMLButtonElement) => button.textContent?.trim() === 'Refresh')
    refreshButton!.click()
    fixture.detectChanges()

    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('last-known details are retained')
    expect(fixture.componentInstance.feeds.length).toBe(1)
    expect(fixture.nativeElement.querySelector('[role="alert"] button')?.textContent).toContain('Try again')
  })

  it('renders an honest empty-feed state and connected-source recovery action', () => {
    mountPage({ feeds: [] })
    openAdvancedSection(0)

    expect(fixture.nativeElement.textContent).toContain('No normalized feeds registered')
    const button = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((candidate: HTMLButtonElement) => candidate.textContent?.includes('Review sources'))
    expect(button).toBeDefined()
    button!.click()
    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'])
  })

  it('reports partial feed-import errors as a review state, not a clean success', () => {
    service.sync.and.returnValue(of({
      feedId: 'feed-calendar',
      itemsRead: 3,
      operationsCreated: 1,
      operationsRefreshed: 0,
      privacyFlagged: 0,
      errors: ['one item rejected'],
      recorded: true,
    }))

    fixture.ngZone!.run(() => fixture.componentInstance.sync(availableFeed()))

    expect(fixture.componentInstance.feedback?.text).toContain('HAI reported 1 sync issue: one item rejected')
    expect(fixture.componentInstance.feedback?.text).toContain('Review before relying on the import')
    expect(notifications.warning).toHaveBeenCalled()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('shows a truthful feed-level error instead of claiming items could not be recorded', () => {
    service.sync.and.returnValue(of({
      feedId: 'feed-calendar',
      itemsRead: 0,
      operationsCreated: 0,
      operationsRefreshed: 0,
      privacyFlagged: 0,
      errors: ['feed path is not approved'],
    }))

    fixture.ngZone!.run(() => fixture.componentInstance.sync(availableFeed()))

    expect(fixture.componentInstance.feedback?.text).toContain('feed path is not approved')
    expect(fixture.componentInstance.feedback?.text).not.toContain('items could not be recorded')
  })

  it('retains successful feed data and reports a bridge-contract request failure separately', () => {
    service.bridges.and.returnValue(throwError(() => new Error('offline')))
    service.feeds.and.returnValue(of({ feeds: [availableFeed()] }))
    mountPage({})

    expect(fixture.componentInstance.feeds.length).toBe(1)
    expect(fixture.componentInstance.feedLoadError).toBe('')
    expect(fixture.componentInstance.bridgeLoadError).toContain('Bridge compatibility could not be refreshed')
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('Bridge compatibility')

    const contracts = openAdvancedSection(1)
    expect(contracts.querySelector('.ab__resource-notice')?.textContent).toContain('cannot conclude that no bridge contracts exist')
    expect(contracts.querySelector('.ab__empty')).toBeNull()
  })

  it('does not show an empty-feed state when the feed response failed', () => {
    service.feeds.and.returnValue(throwError(() => new Error('offline')))
    mountPage({})

    expect(fixture.componentInstance.healthSummary).toBe('Feed status unavailable')
    expect(fixture.componentInstance.feeds).toEqual([])
    const metrics = Array.from(fixture.nativeElement.querySelectorAll('.ab__metric strong') as NodeListOf<HTMLElement>)
    expect(metrics.map((metric) => metric.textContent?.trim())).toEqual(Array(4).fill('Unavailable'))
    expect(fixture.nativeElement.querySelector('.ab__next-copy')?.textContent).not.toContain('No normalized account feeds are registered')
    expect(fixture.nativeElement.querySelector('.ab__next-action button')?.textContent).toContain('Refresh feed status')
    const feeds = openAdvancedSection(0)
    expect(feeds.querySelector('.ab__resource-notice')?.textContent).toContain('previously loaded feed details remain visible')
    expect(feeds.textContent).not.toContain('No normalized feeds registered')
  })

  it('cancels an in-flight feed sync when the page is destroyed', () => {
    let unsubscribed = false
    service.sync.and.returnValue(new Observable<ISyncReport>(() => () => { unsubscribed = true }))

    fixture.ngZone!.run(() => fixture.componentInstance.sync(availableFeed()))
    expect(fixture.componentInstance.syncing['feed-calendar']).toBeTrue()

    fixture.destroy()

    expect(unsubscribed).toBeTrue()
    expect(fixture.componentInstance.syncing['feed-calendar']).toBeFalse()
  })

  it('explains why bulk sync is unavailable while a request is loading or running', () => {
    const component = fixture.componentInstance
    openAdvancedSection(0)
    component.loading = true
    expect(component.syncAllDisabledReason()).toContain('finished loading')

    component.loading = false
    const pendingSync = new Subject<ISyncReport>()
    service.sync.and.returnValue(pendingSync.asObservable())
    const syncButton: HTMLButtonElement = fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]')
    syncButton.click()
    fixture.detectChanges()

    expect(component.syncing['feed-calendar']).toBeTrue()
    expect(component.syncAllDisabled).toBeTrue()
    expect(component.syncAllDisabledReason()).toContain('already running')

    expect(fixture.nativeElement.querySelector('.ab__sync-note')?.textContent).toContain('already running')
  })

  it('treats an empty bulk-sync response as no work, not as a successful sync', () => {
    service.syncDue.and.returnValue(of({ reports: [] }))

    fixture.ngZone!.run(() => fixture.componentInstance.syncAll())

    expect(fixture.componentInstance.feedback?.kind).toBe('warning')
    expect(fixture.componentInstance.feedback?.text).toContain('No enabled feed was synced')
    expect(notifications.warning).toHaveBeenCalled()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('includes feed names and safe error details for partial bulk-sync failures', () => {
    service.syncDue.and.returnValue(of({
      reports: [{
        feedId: 'feed-calendar',
        itemsRead: 0,
        operationsCreated: 0,
        operationsRefreshed: 0,
        privacyFlagged: 0,
        errors: ['HTTP feed sync is disabled by policy'],
      }],
    }))

    fixture.ngZone!.run(() => fixture.componentInstance.syncAll())

    expect(fixture.componentInstance.feedback?.kind).toBe('warning')
    expect(fixture.componentInstance.feedback?.text).toContain('Calendar feed: HTTP feed sync is disabled by policy')
    expect(fixture.componentInstance.feedback?.text).not.toContain('review the results')
  })

  it('opens Connected Sources from the secondary action', () => {
    const button = Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((candidate: HTMLButtonElement) => candidate.textContent?.includes('Connected Sources'))
    expect(button).toBeDefined()
    button!.click()

    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'])
  })

  it('blocks Basic, Advanced, and method-level sync during a pending status refresh', () => {
    openAdvancedSection(0)
    const component = fixture.componentInstance
    const retained = component.feeds
    const pending = new Subject<{ feeds: IFeedHealth[] }>()
    service.feeds.and.returnValue(pending.asObservable())
    component.refresh()
    fixture.detectChanges()

    expect(component.feeds).toBe(retained)
    expect((fixture.nativeElement.querySelector('.ab__next-action button') as HTMLButtonElement).disabled).toBeTrue()
    expect((fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]') as HTMLButtonElement).disabled).toBeTrue()
    component.sync(retained[0])
    component.syncAll()
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    pending.next({ feeds: retained })
    pending.complete()
    fixture.detectChanges()
    expect((fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]') as HTMLButtonElement).disabled).toBeFalse()
  })

  it('retains exact feed evidence on refresh failure and recovers before importing', () => {
    openAdvancedSection(0)
    const component = fixture.componentInstance
    const retained = component.feeds
    const loaded = component.lastLoadedAt
    service.feeds.and.returnValue(throwError(() => new Error('offline')))
    component.refresh()
    fixture.detectChanges()

    expect(component.feeds).toBe(retained)
    expect(component.lastLoadedAt).toBe(loaded)
    expect(component.healthSummary).toContain('last-known data')
    expect(fixture.nativeElement.querySelector('.ab__metric strong')?.textContent.trim()).toBe('1')
    expect(fixture.nativeElement.querySelector('.ab__feed-facts')?.textContent).toContain(utcText(retained[0].lastSyncedAt!))
    expect((fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]') as HTMLButtonElement).disabled).toBeTrue()
    component.sync(retained[0])
    component.syncAll()
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()

    service.feeds.and.returnValue(of({ feeds: retained }))
    ;(fixture.nativeElement.querySelector('.ab__next-action button') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(component.feedLoadError).toBe('')
    expect(service.sync).not.toHaveBeenCalled()
    expect(component.nextActionLabel).toBe('Sync this feed')
  })

  it('keeps visible action labels in their accessible names in Basic and Advanced views', () => {
    const verify = (button: HTMLButtonElement): void => {
      expect(button.getAttribute('aria-label')).toContain(button.textContent!.trim())
    }
    verify(fixture.nativeElement.querySelector('.ab__next-action button'))
    openAdvancedSection(0)
    verify(fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]'))
    mountPage({ feeds: [contractOnlyFeed()] })
    verify(fixture.nativeElement.querySelector('.ab__next-action button'))
    service.feeds.and.returnValue(throwError(() => new Error('offline')))
    fixture.componentInstance.refresh()
    fixture.detectChanges()
    verify(fixture.nativeElement.querySelector('.ab__next-action button'))
  })

  it('renders distinct attempt/success instants and read counts in their correct DOM rows', () => {
    openAdvancedSection(0)
    const cases = [
      availableFeed({ lastAttemptAt: '2026-10-01T12:01:00Z', lastSyncedAt: undefined, lastItemsRead: 0 }),
      availableFeed({ lastAttemptAt: '2026-10-01T12:01:00Z', lastSyncedAt: '2026-10-01T12:00:00Z', lastItemsRead: 2 }),
      availableFeed({ lastAttemptAt: 'invalid' }),
      availableFeed(),
    ]
    for (const feed of cases) {
      mountPage({ feeds: [feed] })
      const facts = fixture.nativeElement.querySelector('.ab__feed-facts') as HTMLElement
      const row = (label: string): HTMLElement => Array.from(facts.querySelectorAll('div'))
        .find((candidate) => candidate.querySelector('dt')?.textContent === label)!
      const attemptedAt = feed.lastAttemptAt ?? feed.lastSyncedAt
      const validAttempt = attemptedAt && !Number.isNaN(new Date(attemptedAt).getTime())
      expect(row('Last attempt').querySelector('time')?.getAttribute('datetime') ?? null)
        .toBe(validAttempt ? new Date(attemptedAt!).toISOString() : null)
      expect(row('Last attempt').textContent).toContain(validAttempt ? utcText(attemptedAt!) : 'Attempt time unavailable')
      expect(row('Last successful sync').querySelector('time')?.getAttribute('datetime') ?? null)
        .toBe(feed.lastSyncedAt ? new Date(feed.lastSyncedAt).toISOString() : null)
      expect(row('Last successful sync').textContent)
        .toContain(feed.lastSyncedAt ? utcText(feed.lastSyncedAt) : 'No successful sync recorded')
      expect(row('Items read').textContent).toContain(`${feed.lastItemsRead} items read`)
    }
    const first = availableFeed({ lastAttemptAt: '2026-10-25T00:30:00Z' })
    const second = availableFeed({ lastAttemptAt: '2026-10-25T01:30:00Z' })
    expect(fixture.componentInstance.lastAttemptAt(first)).not.toBe(fixture.componentInstance.lastAttemptAt(second))
    expect(fixture.componentInstance.lastAttemptAt(first)).toMatch(/UTC|GMT/)
  })

  it('shows a failed post-sync refresh attempt without advancing the successful-sync row', () => {
    openAdvancedSection(0)
    const later = availableFeed({ lastAttemptAt: '2026-10-01T12:01:00Z', lastItemsRead: 0 })
    service.feeds.and.returnValue(of({ feeds: [later] }))
    service.sync.and.returnValue(of({ feedId: later.feed.id, itemsRead: 0, operationsCreated: 0, operationsRefreshed: 0, privacyFlagged: 0, errors: ['feed sync failed'] }))
    fixture.componentInstance.sync(availableFeed())
    fixture.detectChanges()
    const rows = Array.from(fixture.nativeElement.querySelectorAll('.ab__feed-facts div') as NodeListOf<HTMLElement>)
    expect(rows.find((row) => row.querySelector('dt')?.textContent === 'Last attempt')?.textContent).toContain(utcText(later.lastAttemptAt!))
    expect(rows.find((row) => row.querySelector('dt')?.textContent === 'Last successful sync')?.textContent).toContain(utcText(later.lastSyncedAt!))
    expect(fixture.componentInstance.feedback?.kind).toBe('warning')
  })

  it('keeps running or interrupted imports visible in Basic view without allowing a duplicate sync', () => {
    const pending = availableFeed({ syncState: 'running_or_interrupted', syncStartedAt: '2026-10-01T12:03:00Z' })
    mountPage({ feeds: [pending] })
    const component = fixture.componentInstance
    expect(fixture.nativeElement.textContent).toContain('1 import running or interrupted')
    expect(component.isFeedSyncable(pending)).toBeFalse()
    expect(component.lastAttemptCount(pending)).toBe('Current read count unverified')
    expect(component.syncAllDisabled).toBeTrue()
    component.sync(pending)
    component.syncAll()
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    openAdvancedSection(0)
    const control = fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]') as HTMLButtonElement
    expect(control.disabled).toBeTrue()
    expect(control.title).toContain('operator review')
  })

  it('does not call an import successful without a confirmed completion audit', () => {
    for (const recorded of [false, undefined]) {
      service.sync.and.returnValue(of({ feedId: 'feed-calendar', itemsRead: 1, operationsCreated: 1, operationsRefreshed: 0, privacyFlagged: 0, recorded }))
      fixture.componentInstance.sync(availableFeed())
      expect(fixture.componentInstance.feedback?.kind).toBe('warning')
      expect(fixture.componentInstance.feedback?.text).toContain('completion audit was not confirmed')
    }
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('refreshes after an unconfirmed HTTP sync so persisted interrupted state can be inspected', () => {
    const pending = availableFeed({ syncState: 'running_or_interrupted' })
    service.sync.and.returnValue(throwError(() => new Error('storage unavailable')))
    service.feeds.and.returnValue(of({ feeds: [pending] }))
    fixture.componentInstance.sync(availableFeed())
    fixture.detectChanges()
    expect(fixture.componentInstance.feedback?.kind).toBe('error')
    expect(fixture.componentInstance.feedback?.text).toContain('operator review')
    expect(fixture.componentInstance.feeds).toEqual([pending])
    expect(fixture.componentInstance.syncAllDisabled).toBeTrue()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('does not advertise a background schedule for a manually imported feed', () => {
    mountPage({ feeds: [availableFeed()] })
    openAdvancedSection(0)
    expect(fixture.nativeElement.textContent).toContain('Included in bulk import')
    expect(fixture.nativeElement.textContent).not.toContain('Scheduled sync on')
  })

  it('blocks an unrecognized sync state instead of treating it as idle', () => {
    const uncertain = availableFeed({ syncState: 'unrecognized' as IFeedHealth['syncState'] })
    mountPage({ feeds: [uncertain] })
    openAdvancedSection(0)
    const control = fixture.nativeElement.querySelector('[aria-label="Sync now: Calendar feed"]') as HTMLButtonElement
    expect(control.disabled).toBeTrue()
    expect(control.title).toContain('Sync state could not be verified')
    fixture.componentInstance.sync(uncertain)
    expect(service.sync).not.toHaveBeenCalled()
  })

  it('only inspects source identities after an explicit accessible action in the selected inspector', () => {
    const inspect = openFeedInspector()
    expect(service.identityPreview).not.toHaveBeenCalled()
    expect(inspect.getAttribute('aria-label')).toContain(inspect.textContent!.trim())
    expect(inspect.getAttribute('aria-describedby')).toBe('ab-identity-scope')
    expect(inspect.getAttribute('aria-controls')).toBe('ab-identity-preview')
    expect(fixture.nativeElement.querySelector('[aria-label="Inspect feed: Calendar feed"]').getAttribute('aria-expanded')).toBe('true')

    inspect.click()
    fixture.detectChanges()

    expect(service.identityPreview).toHaveBeenCalledOnceWith('feed-calendar')
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(service.audit).not.toHaveBeenCalled()
    const inspector = fixture.nativeElement.querySelector('#ab-feed-inspector') as HTMLElement
    expect(inspector.textContent).toContain('4 inspected of 4 observed items')
    expect(inspector.textContent).toContain('historical inventory remains incomplete')
    expect(inspector.textContent).toContain('not a full historical migration inventory')
    expect(inspector.textContent).toContain('does not confirm reconciliation is complete')
    expect(inspector.querySelector('time')?.getAttribute('datetime')).toBe('2026-10-01T12:05:00Z')
    expect(inspector.querySelector('.ab__identity-freshness')?.textContent).toContain('UTC')
    expect(inspector.textContent).toContain('Point-in-time observation, not live status or a transaction-wide snapshot')
    expect(inspector.textContent).toContain('No provider request, import, sync claim, audit write, migration, or send')
    const items = Array.from(inspector.querySelectorAll('.ab__identity-items li'))
    expect(items.length).toBe(4)
    expect(items[0].textContent).toContain('source-unseen')
    expect(items[1].textContent).toContain('canonical-1')
    expect(items[2].textContent).toContain('historical-1')
    expect(items[3].textContent).toContain('canonical-2')
    expect(items[3].textContent).toContain('historical-2')
  })

  it('keeps inspection available after a historical sync warning without clearing that warning', () => {
    service.sync.and.returnValue(of({
      feedId: 'feed-calendar', itemsRead: 1, operationsCreated: 0, operationsRefreshed: 0, privacyFlagged: 0,
      errors: ['historical identity requires operator reconciliation'], recorded: true,
    }))
    fixture.componentInstance.sync(availableFeed())
    const warning = fixture.componentInstance.feedback
    const inspect = openFeedInspector()
    expect(inspect.disabled).toBeFalse()
    inspect.click()
    fixture.detectChanges()

    expect(fixture.componentInstance.feedback).toBe(warning)
    expect(fixture.nativeElement.querySelector('.ab__feedback')?.textContent).toContain('historical identity requires operator reconciliation')
    expect(service.sync).toHaveBeenCalledTimes(1)
    expect(service.identityPreview).toHaveBeenCalledOnceWith('feed-calendar')
  })

  it('does not require sync eligibility for a read-only source identity inspection', () => {
    mountPage({ feeds: [availableFeed({ syncState: 'running_or_interrupted', connectionStatus: 'credentials_present_unverified' })] })
    const inspect = openFeedInspector()
    service.identityPreview.and.returnValue(throwError(() => ({ status: 409, error: { code: 'feed_sync_busy' } })))
    expect(inspect.disabled).toBeFalse()
    inspect.click()
    fixture.detectChanges()
    expect(service.identityPreview).toHaveBeenCalledOnceWith('feed-calendar')
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.audit).not.toHaveBeenCalled()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.componentInstance.identityPreviewError).toContain('this feed sync is running or interrupted')
    expect(fixture.componentInstance.identityPreviewError).not.toContain('HTTP feeds')
    expect(fixture.nativeElement.querySelector('#ab-feed-inspector')?.textContent).toContain('operator review')
  })

  it('announces loading, blocks duplicate inspection, and clears the previous sample before retrying', () => {
    const inspect = openFeedInspector()
    inspect.click()
    fixture.detectChanges()
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    inspect.click()
    fixture.detectChanges()

    expect(inspect.disabled).toBeTrue()
    expect(inspect.getAttribute('aria-busy')).toBe('true')
    expect(fixture.nativeElement.querySelector('.ab__loading-state[role="status"]')?.textContent).toContain('Inspecting the local source')
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    fixture.componentInstance.inspectSourceIdentities()
    expect(service.identityPreview).toHaveBeenCalledTimes(2)
    pending.next(identityPreview())
    pending.complete()
    fixture.detectChanges()
    expect(inspect.disabled).toBeFalse()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
  })

  it('cancels the previous selection and cannot show its report or finalizer for another feed', () => {
    const second: IFeedHealth = { ...availableFeed(), feed: { ...availableFeed().feed, id: 'feed-second', name: 'Second feed' } }
    mountPage({ feeds: [availableFeed(), second] })
    const firstRequest = new Subject<IFeedIdentityPreview>()
    const secondRequest = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValues(firstRequest.asObservable(), secondRequest.asObservable())
    openFeedInspector().click()
    expect(firstRequest.observers.length).toBe(1)

    ;(fixture.nativeElement.querySelector('[aria-label="Inspect feed: Second feed"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(firstRequest.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    ;(fixture.nativeElement.querySelector('[aria-label="Inspect source identities: Second feed"]') as HTMLButtonElement).click()
    firstRequest.next(identityPreview())
    firstRequest.error(new Error('late error'))
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreviewLoading).toBeTrue()
    expect(fixture.componentInstance.identityPreviewError).toBe('')
    expect(fixture.componentInstance.identityPreview).toBeUndefined()

    secondRequest.next(identityPreview({ feedId: 'feed-second' }))
    secondRequest.complete()
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreview?.feedId).toBe('feed-second')
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('#ab-inspector-title')?.textContent).toContain('Second feed')
  })

  it('clears a completed sample when another feed is selected', () => {
    const second: IFeedHealth = { ...availableFeed(), feed: { ...availableFeed().feed, id: 'feed-second', name: 'Second feed' } }
    mountPage({ feeds: [availableFeed(), second] })
    openFeedInspector().click()
    fixture.componentInstance.selectFeed(second)
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    expect(service.identityPreview).toHaveBeenCalledTimes(1)
  })

  it('clears and cancels identity observations on a feed refresh and closes a removed selection', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    service.feeds.and.returnValue(of({ feeds: [] }))
    fixture.componentInstance.refresh()
    pending.next(identityPreview())
    pending.complete()
    fixture.detectChanges()

    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.selectedFeed).toBeUndefined()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('#ab-feed-inspector')).toBeNull()
  })

  it('invalidates a completed sample even when a refreshed feed has the same ID', () => {
    openFeedInspector().click()
    const reconfigured = availableFeed({ feed: { ...availableFeed().feed, path: 'another-approved-export.json' } })
    service.feeds.and.returnValue(of({ feeds: [reconfigured] }))
    fixture.componentInstance.refresh()
    fixture.detectChanges()
    expect(fixture.componentInstance.selectedFeed).toBe(reconfigured)
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(service.identityPreview).toHaveBeenCalledTimes(1)
  })

  it('cancels an inspection when its inspector closes and restores focus to its selection control', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    const trigger = fixture.nativeElement.querySelector('[aria-label="Inspect feed: Calendar feed"]') as HTMLButtonElement
    const focused = spyOn(trigger, 'focus')
    ;(fixture.nativeElement.querySelector('[aria-label="Close feed inspector"]') as HTMLButtonElement).click()
    pending.next(identityPreview())
    pending.complete()
    fixture.detectChanges()
    expect(focused).toHaveBeenCalled()
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.selectedFeedId).toBeUndefined()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('#ab-feed-inspector')).toBeNull()
  })

  it('cancels inspection when Advanced details are hidden or the page is destroyed', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    TestBed.inject(ModuleViewPreferencesService).setMode('account-bridges', 'basic')
    fixture.detectChanges()
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()

    fixture.componentInstance.inspectSourceIdentities()
    expect(pending.observers.length).toBe(1)
    fixture.destroy()
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
  })

  it('keeps the HTTP 409 explanation and inspection-only retry visible in Basic', () => {
    mountPage({ feeds: [availableFeed({ feed: { ...availableFeed().feed, sourceType: 'http_json_feed' } })] })
    service.identityPreview.and.returnValue(throwError(() => ({ status: 409, error: { code: 'identity_preview_unsupported', error: 'local-only preview' } })))
    openFeedInspector().click()
    fixture.detectChanges()
    TestBed.inject(ModuleViewPreferencesService).setMode('account-bridges', 'basic')
    fixture.detectChanges()
    const alert = fixture.nativeElement.querySelector('.ab__identity-error') as HTMLElement
    expect(alert.getAttribute('role')).toBe('alert')
    expect(alert.textContent).toContain('Calendar feed')
    expect(alert.textContent).toContain('HTTP feeds are not inspected')
    expect(alert.textContent).toContain('operator must use an approved local export')
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-counts')).toBeNull()
    expect(fixture.nativeElement.querySelector('.ab__identity-items')).toBeNull()

    const retried = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(retried.asObservable())
    ;(alert.querySelector('button') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('.ab__identity-error')).toBeNull()
    expect(fixture.nativeElement.querySelector('.ab__loading-state[role="status"]')?.textContent).toContain('Calendar feed')
    retried.error({ status: 409, error: { code: 'identity_preview_unsupported' } })
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('.ab__identity-error')?.textContent).toContain('approved local export')
    expect(service.identityPreview).toHaveBeenCalledTimes(2)
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('reports inspection failures without rendering the previous report as current', () => {
    const inspect = openFeedInspector()
    inspect.click()
    service.identityPreview.and.returnValue(throwError(() => new Error('local read unavailable')))
    inspect.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('.ab__identity-error')?.textContent).toContain('reconciliation remains unverified')
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
  })

  it('rejects mismatched or malformed preview contracts rather than implying verified coverage', () => {
    const inspect = openFeedInspector()
    const invalid: unknown[] = [
      null,
      undefined,
      [],
      'not a preview',
      identityPreview({ feedId: 'wrong-feed' }),
      identityPreview({ scope: 'all_history' as IFeedIdentityPreview['scope'] }),
      identityPreview({ historicalInventoryComplete: true as unknown as false }),
      identityPreview({ observedAt: 'invalid' }),
      identityPreview({ observedAt: '2026-10-01T14:05:00+02:00' }),
      identityPreview({ observedAt: '2026-02-30T12:05:00Z' }),
      identityPreview({ observedAt: '2026-10-01T24:05:00Z' }),
      identityPreview({ counts: null as unknown as IFeedIdentityPreview['counts'] }),
      identityPreview({ items: null as unknown as IFeedIdentityPreview['items'] }),
      identityPreview({ itemsInspected: -1 }),
      identityPreview({ itemsInspected: 5 }),
      identityPreview({ itemsObserved: 3 }),
      identityPreview({ itemsObserved: 4.5 }),
      identityPreview({ itemsObserved: Number.MAX_SAFE_INTEGER + 1 }),
      identityPreview({ itemsObserved: 6, truncated: true }),
      identityPreview({ truncated: true }),
      identityPreview({ truncated: 'false' as unknown as boolean }),
      identityPreview({ counts: { unseen: 0, canonical: 0, historical: 0, coexisting: 0 } }),
      identityPreview({ counts: { unseen: 4, canonical: 0, historical: 0, coexisting: 0 } }),
      identityPreview({ counts: { unseen: NaN, canonical: 1, historical: 1, coexisting: 1 } }),
      identityPreview({ counts: { unseen: 1, canonical: -1, historical: 1, coexisting: 1 } }),
      identityPreview({ items: [null, ...identityPreview().items.slice(1)] as unknown as IFeedIdentityPreview['items'] }),
      identityPreview({ items: [{ ...identityPreview().items[0], externalId: ' ' }, ...identityPreview().items.slice(1)] }),
      identityPreview({ items: [{ ...identityPreview().items[0], title: '' }, ...identityPreview().items.slice(1)] }),
      identityPreview({ items: [{ ...identityPreview().items[0], state: 'unknown' }, ...identityPreview().items.slice(1)] as unknown as IFeedIdentityPreview['items'] }),
      identityPreview({ items: [{ ...identityPreview().items[0], canonicalOperationId: 'unexpected' }, ...identityPreview().items.slice(1)] }),
      identityPreview({ items: [identityPreview().items[0], { ...identityPreview().items[1], canonicalOperationId: undefined }, ...identityPreview().items.slice(2)] }),
      identityPreview({ items: [identityPreview().items[0], { ...identityPreview().items[1], canonicalOperationId: '' }, ...identityPreview().items.slice(2)] }),
      identityPreview({ items: [...identityPreview().items.slice(0, 3), { ...identityPreview().items[3], historicalOperationId: undefined }] }),
    ]
    for (const report of invalid) {
      service.identityPreview.and.returnValue(of(report as IFeedIdentityPreview))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreview).toBeUndefined()
      expect(fixture.nativeElement.querySelector('.ab__identity-error')?.textContent).toContain('could not be verified for this feed')
      expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    }
  })

  it('does not treat an empty non-truncated sample as a complete historical inventory', () => {
    service.identityPreview.and.returnValue(of(identityPreview({
      itemsObserved: 0, itemsInspected: 0, truncated: false,
      counts: { unseen: 0, canonical: 0, historical: 0, coexisting: 0 }, items: [],
    })))
    openFeedInspector().click()
    fixture.detectChanges()
    const inspector = fixture.nativeElement.querySelector('#ab-feed-inspector') as HTMLElement
    expect(inspector.textContent).toContain('historical inventory remains incomplete')
    expect(inspector.textContent).toContain('does not prove historical identities are absent')
    expect(inspector.querySelector('.ab__identity-items')).toBeNull()
  })

  it('accepts the exact bounded 100-item sample and displays truncation without claiming global coverage', () => {
    const report = identityPreview({
      itemsObserved: 104, itemsInspected: 100, truncated: true,
      counts: { unseen: 100, canonical: 0, historical: 0, coexisting: 0 },
      items: Array.from({ length: 100 }, (_, index) => ({ externalId: `source-${index}`, title: `Item ${index}`, state: 'unseen' as const })),
    })
    service.identityPreview.and.returnValue(of(report))
    const inspect = openFeedInspector()
    inspect.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreview).toBe(report)
    expect(fixture.nativeElement.querySelector('.ab__identity-summary')?.textContent).toContain('100 inspected of 104 observed items')
    expect(fixture.nativeElement.querySelector('.ab__identity-summary')?.textContent).toContain('Truncated')
    expect(fixture.nativeElement.querySelector('.ab__identity-items')?.getAttribute('tabindex')).toBe('0')
    expect(fixture.nativeElement.querySelectorAll('.ab__identity-items li').length).toBe(100)

    for (const invalid of [
      { ...report, truncated: false },
      { ...report, itemsObserved: 99 },
      { ...report, itemsInspected: 101, items: [...report.items, report.items[0]], counts: { ...report.counts, unseen: 101 } },
    ]) {
      service.identityPreview.and.returnValue(of(invalid))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreview).toBeUndefined()
      expect(fixture.componentInstance.identityPreviewError).toContain('could not be verified')
    }
  })

  it('accepts UTC nanosecond timestamps and explicit zero-offset UTC observations', () => {
    const inspect = openFeedInspector()
    for (const observedAt of ['2026-10-01T12:05:00.123456789Z', '2026-10-01T12:05:00+00:00']) {
      service.identityPreview.and.returnValue(of(identityPreview({ observedAt })))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreviewError).toBe('')
      expect(fixture.nativeElement.querySelector('.ab__identity-report time')?.getAttribute('datetime')).toBe(observedAt)
    }
  })

  it('distinguishes stable busy and unsupported codes even when human-readable errors change', () => {
    const inspect = openFeedInspector()
    const cases = [
      { code: 'feed_sync_busy', expected: 'this feed sync is running or interrupted' },
      { code: 'identity_preview_unsupported', expected: 'HTTP feeds are not inspected' },
    ]
    for (const { code, expected } of cases) {
      service.identityPreview.and.returnValue(throwError(() => ({ status: 409, error: { code, error: 'changed explanation' } })))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreviewError).toContain(expected)
      expect(fixture.componentInstance.identityPreviewError).not.toContain('changed explanation')
      expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
      expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    }
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(service.audit).not.toHaveBeenCalled()
  })

  it('discards stale previews on identity_preview_changed and supports an explicit retry after refreshing feed status', () => {
    const inspect = openFeedInspector()
    const component = fixture.componentInstance
    inspect.click()
    fixture.detectChanges()
    expect(component.selectedIdentityPreview).toBeDefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).not.toBeNull()

    const changed = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(changed.asObservable())
    inspect.click()
    fixture.detectChanges()
    expect(component.identityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    changed.error({
      status: 409,
      error: { code: 'identity_preview_changed', error: 'feed changed during source identity inspection; refresh feed status and inspect again' },
    })
    fixture.detectChanges()

    expect(component.identityPreviewLoading).toBeFalse()
    expect(component.identityPreview).toBeUndefined()
    expect(component.selectedIdentityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
    expect(fixture.nativeElement.querySelector('.ab__identity-counts')).toBeNull()
    expect(fixture.nativeElement.querySelector('.ab__identity-items')).toBeNull()
    const alert = fixture.nativeElement.querySelector('.ab__identity-error') as HTMLElement
    expect(alert.getAttribute('role')).toBe('alert')
    expect(alert.textContent).toContain('Feed configuration or recorded sync status changed')
    expect(alert.textContent).toContain('no preview was accepted')
    expect(alert.textContent).toContain('Refresh feed status, then inspect source identities again')
    expect(alert.textContent).toContain('not a transaction-wide snapshot or a complete historical inventory')
    expect(alert.textContent).not.toContain('HTTP feeds are not inspected')
    expect((alert.querySelector('button') as HTMLButtonElement).disabled).toBeFalse()
    expect(service.identityPreview).toHaveBeenCalledTimes(2)

    const status = new Subject<{ feeds: IFeedHealth[] }>()
    service.feeds.and.returnValue(status.asObservable())
    const refresh = Array.from(fixture.nativeElement.querySelectorAll('.ab__actions button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.trim() === 'Refresh')!
    refresh.click()
    fixture.detectChanges()
    expect(inspect.disabled).toBeTrue()
    component.inspectSourceIdentities()
    expect(service.identityPreview).toHaveBeenCalledTimes(2)
    const refreshedFeed = availableFeed({ feed: { ...availableFeed().feed, path: 'updated-approved-export.json' }, lastItemsRead: 4 })
    status.next({ feeds: [refreshedFeed] })
    status.complete()
    fixture.detectChanges()
    expect(component.selectedFeed).toBe(refreshedFeed)
    expect(component.identityPreviewError).toBe('')
    expect(component.identityPreview).toBeUndefined()
    expect(service.identityPreview).toHaveBeenCalledTimes(2)

    const freshReport = identityPreview({ observedAt: '2026-10-01T12:06:00Z' })
    service.identityPreview.and.returnValue(of(freshReport))
    ;(fixture.nativeElement.querySelector('[aria-label="Inspect source identities: Calendar feed"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(service.identityPreview).toHaveBeenCalledTimes(3)
    expect(component.selectedIdentityPreview).toBe(freshReport)
    expect(component.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('.ab__identity-error')).toBeNull()
    expect(fixture.nativeElement.querySelector('.ab__identity-report time')?.getAttribute('datetime')).toBe(freshReport.observedAt)
    expect(fixture.nativeElement.querySelector('#ab-identity-scope')?.textContent).toContain('not a full historical migration inventory')
    expect(fixture.nativeElement.querySelector('.ab__identity-freshness')?.textContent).toContain('not live status or a transaction-wide snapshot')
    expect(freshReport.historicalInventoryComplete).toBeFalse()
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.syncDue).not.toHaveBeenCalled()
    expect(service.audit).not.toHaveBeenCalled()
    expect(notifications.success).not.toHaveBeenCalled()
  })

  it('uses a generic safe conflict message for absent, unknown or malformed 409 codes', () => {
    const inspect = openFeedInspector()
    for (const error of [
      { status: 409 },
      { status: 409, error: null },
      { status: 409, error: 'private diagnostic' },
      { status: 409, error: { error: 'feed sync is running or interrupted; operator review is required before recovery' } },
      { status: 409, error: { code: 'future_conflict', error: 'private diagnostic' } },
      { status: 409, error: { code: 409 } },
    ]) {
      service.identityPreview.and.returnValue(throwError(() => error))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreviewError).toContain('feed conflict')
      expect(fixture.componentInstance.identityPreviewError).not.toContain('HTTP feeds')
      expect(fixture.componentInstance.identityPreviewError).not.toContain('this feed sync is running')
      expect(fixture.componentInstance.identityPreviewError).not.toContain('private diagnostic')
    }
  })

  it('maps other stable backend failures without exposing server diagnostics or implying reconciliation', () => {
    const inspect = openFeedInspector()
    const cases = [
      { status: 404, code: 'feed_not_found', expected: 'select a current feed' },
      { status: 400, code: 'feed_invalid', expected: 'check the approved file, provider and scope' },
      { status: 503, code: 'feed_storage_unavailable', expected: 'operator review of local diagnostics' },
    ]
    for (const { status, code, expected } of cases) {
      service.identityPreview.and.returnValue(throwError(() => ({ status, error: { code, error: 'private diagnostic' } })))
      inspect.click()
      fixture.detectChanges()
      expect(fixture.componentInstance.identityPreviewError).toContain(expected)
      expect(fixture.componentInstance.identityPreviewError).toContain('No import or reconciliation was performed')
      expect(fixture.componentInstance.identityPreviewError).not.toContain('private diagnostic')
    }
    service.identityPreview.and.returnValue(throwError(() => ({ status: 503, error: { code: 'identity_preview_unsupported' } })))
    inspect.click()
    expect(fixture.componentInstance.identityPreviewError).not.toContain('HTTP feeds')
  })

  it('does not leave loading active for empty responses or synchronous request setup failures', () => {
    const inspect = openFeedInspector()
    service.identityPreview.and.returnValue(EMPTY)
    inspect.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.componentInstance.identityPreviewError).toContain('could not be completed')
    expect(fixture.componentInstance.identityPreview).toBeUndefined()

    service.identityPreview.and.callFake(() => { throw new Error('request setup failed') })
    expect(() => fixture.componentInstance.inspectSourceIdentities()).not.toThrow()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.componentInstance.identityPreviewError).toContain('could not be completed')
  })

  it('times out a stalled inspection without importing, writing an audit or treating it as complete', fakeAsync(() => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    tick(12001)
    fixture.detectChanges()
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.componentInstance.identityPreviewError).toContain('could not be completed')
    expect(service.sync).not.toHaveBeenCalled()
    expect(service.audit).not.toHaveBeenCalled()
  }))

  it('accepts one response per inspection and requests a fresh sample on each explicit retry', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    const report = identityPreview()
    pending.next(report)
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    pending.next(identityPreview({ feedId: 'wrong-feed' }))
    pending.error(new Error('late diagnostic'))
    expect(fixture.componentInstance.identityPreview).toBe(report)
    expect(fixture.componentInstance.identityPreviewError).toBe('')

    service.identityPreview.and.returnValue(of(identityPreview({ observedAt: '2026-10-01T12:06:00Z' })))
    fixture.componentInstance.inspectSourceIdentities()
    expect(service.identityPreview).toHaveBeenCalledTimes(2)
    expect(fixture.componentInstance.identityPreview?.observedAt).toBe('2026-10-01T12:06:00Z')
  })

  it('guards reports against same-ID feed replacement even outside the refresh path', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    const replacement = availableFeed({ feed: { ...availableFeed().feed, path: 'replacement-approved-export.json' } })
    fixture.componentInstance.feeds = [replacement]
    pending.next(identityPreview())
    fixture.detectChanges()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()

    service.identityPreview.and.returnValue(of(identityPreview()))
    fixture.componentInstance.inspectSourceIdentities()
    expect(fixture.componentInstance.selectedIdentityPreview).toBeDefined()
    fixture.componentInstance.feeds = [availableFeed()]
    fixture.detectChanges()
    expect(fixture.componentInstance.selectedIdentityPreview).toBeUndefined()
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
  })

  it('cancels a pending request when reselecting a reconfigured feed with the same ID', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    const replacement = availableFeed({ feed: { ...availableFeed().feed, path: 'replacement-approved-export.json' } })
    fixture.componentInstance.feeds = [replacement]
    fixture.componentInstance.selectFeed(replacement)
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    expect(fixture.componentInstance.identityPreview).toBeUndefined()
  })

  it('cancels when the feed disclosure is closed and does not inspect automatically when reopened', () => {
    const pending = new Subject<IFeedIdentityPreview>()
    service.identityPreview.and.returnValue(pending.asObservable())
    openFeedInspector().click()
    const disclosure = fixture.nativeElement.querySelector('hai-progressive-section .hai-progressive-section__summary') as HTMLButtonElement
    disclosure.click()
    fixture.detectChanges()
    expect(pending.observers.length).toBe(0)
    expect(fixture.componentInstance.identityPreviewLoading).toBeFalse()
    disclosure.click()
    fixture.detectChanges()
    expect(service.identityPreview).toHaveBeenCalledTimes(1)
    expect(fixture.nativeElement.querySelector('.ab__identity-report')).toBeNull()
  })

  it('does not start another inspection after page destruction', () => {
    const inspect = openFeedInspector()
    inspect.click()
    const component = fixture.componentInstance
    fixture.destroy()
    component.inspectSourceIdentities()
    component.selectFeed(availableFeed())
    expect(service.identityPreview).toHaveBeenCalledTimes(1)
    expect(component.identityPreview).toBeUndefined()
  })

  it('uses only the backend identity-preview GET with an encoded feed ID', () => {
    const http = jasmine.createSpyObj<HttpClient>('HttpClient', ['get', 'post'])
    const report = identityPreview()
    http.get.and.returnValue(of(report))
    const transport = new AccountBridgesService(http)
    let received: IFeedIdentityPreview | undefined
    transport.identityPreview('feed/with ?fragment').subscribe((response) => { received = response })
    expect(http.get).toHaveBeenCalledOnceWith('/api/v1/account-feeds/feed%2Fwith%20%3Ffragment/identity-preview')
    expect(http.post).not.toHaveBeenCalled()
    expect(received).toBe(report)
  })

  it('blocks inspection without a current selection or during an unverified feed refresh', () => {
    const component = fixture.componentInstance
    component.inspectSourceIdentities()
    component.selectFeed(contractOnlyFeed())
    component.inspectSourceIdentities()
    expect(service.identityPreview).not.toHaveBeenCalled()
    const inspect = openFeedInspector()
    const pending = new Subject<{ feeds: IFeedHealth[] }>()
    service.feeds.and.returnValue(pending.asObservable())
    component.refresh()
    fixture.detectChanges()
    expect(inspect.disabled).toBeTrue()
    component.inspectSourceIdentities()
    expect(service.identityPreview).not.toHaveBeenCalled()
    pending.error(new Error('status unavailable'))
    fixture.detectChanges()
    component.inspectSourceIdentities()
    expect(service.identityPreview).not.toHaveBeenCalled()
    expect(inspect.disabled).toBeTrue()
  })
})

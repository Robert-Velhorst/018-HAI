import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { defer, forkJoin, Subject, Subscription, of } from 'rxjs'
import { catchError, finalize, map, take, takeUntil, throwIfEmpty, timeout } from 'rxjs/operators'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  IBridgeContract,
  IFeedHealth,
  IFeedIdentityPreview,
  ISetupRequirement,
  ISyncReport,
} from '../../models/account-bridges.model.interface'
import { AccountBridgesService } from '../../services/account-bridges.service'

type FeedbackKind = 'success' | 'warning' | 'error'

interface ActionFeedback {
  kind: FeedbackKind
  text: string
}

interface ResourceLoad<T> {
  value: T | null
  failed: boolean
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
  selector: 'app-account-bridges',
  templateUrl: './account-bridges.component.html',
  styleUrls: ['./account-bridges.component.scss'],
  standalone: false,
})
export class AccountBridgesComponent implements OnInit, OnDestroy {
  readonly moduleId = 'account-bridges'
  bridges: IBridgeContract[] = []
  feeds: IFeedHealth[] = []
  loading = false
  feedLoadError = ''
  bridgeLoadError = ''
  lastLoadedAt?: Date
  syncing: Record<string, boolean> = {}
  syncingAll = false
  feedback?: ActionFeedback
  selectedFeedId?: string
  identityPreview?: IFeedIdentityPreview
  identityPreviewLoading = false
  identityPreviewError = ''

  private loadSubscription?: Subscription
  private identityPreviewSubscription?: Subscription
  private identityPreviewFeed?: IFeedHealth
  private identityPreviewRequest = 0
  private pageDestroyed = false
  private inspectorTrigger?: HTMLButtonElement
  private readonly destroyed$ = new Subject<void>()
  private readonly loadTimeoutMs = 6000
  private readonly operationTimeoutMs = 30000
  private readonly identityPreviewTimeoutMs = 12000

  constructor(
    private service: AccountBridgesService,
    private notification: NzNotificationService,
    private router: Router,
  ) {}

  get loadError(): string {
    return [this.feedLoadError, this.bridgeLoadError].filter(Boolean).join(' ')
  }

  ngOnInit(): void {
    this.refresh()
  }

  ngOnDestroy(): void {
    this.pageDestroyed = true
    this.clearIdentityPreview()
    this.loadSubscription?.unsubscribe()
    this.destroyed$.next()
    this.destroyed$.complete()
  }

  get enabledFeeds(): IFeedHealth[] {
    return this.feeds.filter((feed) => feed.feed.enabled)
  }

  get availableFeeds(): IFeedHealth[] {
    return this.feeds.filter((feed) => this.isFeedSyncable(feed))
  }

  get pendingClaimCount(): number {
    return this.feeds.filter((feed) => this.hasActiveClaim(feed)).length
  }

  get nextActionFeed(): IFeedHealth | undefined {
    return this.availableFeeds.find((feed) => feed.feed.enabled) ?? this.availableFeeds[0]
  }

  get requiredOrUnverifiedCount(): number {
    return this.feeds.filter((feed) => !this.isFeedSyncable(feed)).length
  }

  get hasSyncInProgress(): boolean {
    return this.syncingAll || Object.values(this.syncing).some(Boolean)
  }

  get syncAllSafe(): boolean {
    return this.enabledFeeds.length > 0 && this.enabledFeeds.every((feed) => this.isFeedSyncable(feed))
  }

  get syncAllDisabled(): boolean {
    return !this.syncAllSafe || this.hasSyncInProgress || this.loading || !!this.feedLoadError
  }

  get feedCountsKnown(): boolean {
    return this.lastLoadedAt !== undefined
  }

  get nextActionLabel(): string {
    if (this.feedLoadError) return 'Refresh feed status'
    return this.nextActionFeed ? 'Sync this feed' : 'Review sources'
  }

  get nextActionDetail(): string {
    if (this.feedLoadError) return 'Current feed status could not be verified. Refresh it before starting another import.'
    if (!this.feedCountsKnown) return 'Checking registered feeds before recommending an import.'
    if (this.nextActionFeed) {
      return 'Run one bounded import from a supported feed. The provider is not changed; matching items are recorded in HAI.'
    }
    if (this.feeds.length === 0) {
      return 'No normalized account feeds are registered. Review provider-native connections or your approved feed setup.'
    }
    return 'No feed is currently available for a direct sync. Review consent, credentials, or the manual-import requirements first.'
  }

  get healthSummary(): string {
    if (this.loading && this.feeds.length === 0) return 'Checking feed status…'
    if (this.loading) return `Refreshing · ${this.availableFeeds.length} of ${this.feeds.length} available`
    if (this.feedLoadError) return this.feedCountsKnown ? 'Feed status unavailable · showing last-known data' : 'Feed status unavailable'
    if (this.feeds.length === 0) return 'No feeds registered'
    const available = this.availableFeeds.length
    return `${available} of ${this.feeds.length} feeds available to sync`
  }

  get bridgeCountLabel(): string {
    if (this.loading && this.bridges.length === 0) return 'Loading bridge contracts'
    if (this.bridgeLoadError) {
      return this.bridges.length ? `${this.bridges.length} last-known contracts` : 'Bridge contracts unavailable'
    }
    return `${this.bridges.length} contracts`
  }

  refresh(): void {
    this.clearIdentityPreview()
    this.loadSubscription?.unsubscribe()
    this.loading = true
    this.feedLoadError = ''
    this.bridgeLoadError = ''
    this.loadSubscription = forkJoin({
      bridges: this.service.bridges().pipe(
        timeout(this.loadTimeoutMs),
        map((response): ResourceLoad<IBridgeContract[]> => ({
          value: Array.isArray(response?.bridges) ? response.bridges : null,
          failed: !Array.isArray(response?.bridges),
        })),
        catchError(() => of({ value: null, failed: true } as ResourceLoad<IBridgeContract[]>)),
      ),
      feeds: this.service.feeds().pipe(
        timeout(this.loadTimeoutMs),
        map((response): ResourceLoad<IFeedHealth[]> => ({
          value: Array.isArray(response?.feeds) ? response.feeds : null,
          failed: !Array.isArray(response?.feeds),
        })),
        catchError(() => of({ value: null, failed: true } as ResourceLoad<IFeedHealth[]>)),
      ),
    }).pipe(finalize(() => { this.loading = false })).subscribe({
      next: ({ bridges, feeds }) => {
        if (bridges.failed || bridges.value === null) {
          this.bridgeLoadError = 'Bridge compatibility could not be refreshed; last-known details are retained.'
        } else {
          this.bridges = bridges.value
        }
        if (feeds.failed || feeds.value === null) {
          this.feedLoadError = 'Feed status could not be refreshed; last-known details are retained.'
        } else {
          this.feeds = feeds.value
          if (this.selectedFeedId && !this.selectedFeed) this.closeFeedInspector()
          this.lastLoadedAt = new Date()
        }
      },
      error: () => {
        this.feedLoadError = 'Feed status could not be refreshed; last-known details are retained.'
        this.bridgeLoadError = 'Bridge compatibility could not be refreshed; last-known details are retained.'
      },
    })
  }

  runNextSafeAction(): void {
    if (this.loading) return
    if (this.feedLoadError) {
      this.refresh()
      return
    }
    const feed = this.nextActionFeed
    if (feed) {
      this.sync(feed)
      return
    }
    this.openConnectedSources()
  }

  get selectedFeed(): IFeedHealth | undefined {
    return this.feeds.find((feed) => feed.feed.id === this.selectedFeedId)
  }

  get selectedIdentityPreview(): IFeedIdentityPreview | undefined {
    return this.identityPreviewFeed === this.selectedFeed && this.identityPreview?.feedId === this.selectedFeedId
      ? this.identityPreview : undefined
  }

  selectFeed(feed: IFeedHealth, trigger?: HTMLButtonElement | Event): void {
    const current = this.feeds.find((candidate) => candidate.feed.id === feed.feed.id)
    if (this.pageDestroyed || !current) return
    if (this.selectedFeedId !== current.feed.id || (this.identityPreviewFeed && this.identityPreviewFeed !== current)) this.clearIdentityPreview()
    this.selectedFeedId = feed.feed.id
    const element = trigger instanceof Event ? trigger.currentTarget : trigger
    this.inspectorTrigger = element instanceof HTMLButtonElement ? element : undefined
  }

  closeFeedInspector(): void {
    this.inspectorTrigger?.focus()
    this.inspectorTrigger = undefined
    this.clearIdentityPreview()
    this.selectedFeedId = undefined
  }

  feedInspectorVisibilityChanged(open: boolean): void {
    if (!open) this.cancelIdentityPreview()
  }

  inspectSourceIdentities(): void {
    const feed = this.selectedFeed
    if (this.pageDestroyed || !feed || this.loading || this.feedLoadError || this.identityPreviewLoading) return

    this.clearIdentityPreview()
    const feedId = feed.feed.id
    const request = this.identityPreviewRequest
    this.identityPreviewFeed = feed
    this.identityPreviewLoading = true
    this.identityPreviewSubscription = defer(() => this.service.identityPreview(feedId)).pipe(
      timeout(this.identityPreviewTimeoutMs),
      take(1),
      throwIfEmpty(() => new Error('Identity inspection returned no response')),
      takeUntil(this.destroyed$),
      finalize(() => {
        if (request === this.identityPreviewRequest) this.identityPreviewLoading = false
      }),
    ).subscribe({
      next: (report) => {
        if (!this.isCurrentIdentityRequest(request, feed)) return
        if (!this.validIdentityPreview(report, feedId)) {
          this.identityPreviewError = 'The source identity response could not be verified for this feed. Retry inspection; do not rely on it for reconciliation.'
          return
        }
        this.identityPreview = report
      },
      error: (error: unknown) => {
        if (!this.isCurrentIdentityRequest(request, feed)) return
        this.identityPreviewError = this.identityPreviewFailure(error)
      },
    })
  }

  private isCurrentIdentityRequest(request: number, feed: IFeedHealth): boolean {
    return !this.pageDestroyed && request === this.identityPreviewRequest && this.selectedFeed === feed
  }

  private cancelIdentityPreview(): void {
    // Invalidate callbacks before unsubscribing, including the previous finalizer.
    this.identityPreviewRequest += 1
    this.identityPreviewSubscription?.unsubscribe()
    this.identityPreviewSubscription = undefined
    this.identityPreviewLoading = false
  }

  private clearIdentityPreview(): void {
    this.cancelIdentityPreview()
    this.identityPreviewFeed = undefined
    this.identityPreview = undefined
    this.identityPreviewError = ''
  }

  private identityPreviewFailure(error: unknown): string {
    const response = this.isRecord(error) ? error : undefined
    const body = response?.['error']
    const code = this.isRecord(body) ? body['code'] : undefined
    if (response?.['status'] === 409) {
      if (code === 'feed_sync_busy') {
        return 'Source identity inspection is blocked because this feed sync is running or interrupted. Refresh feed status; an interrupted run requires operator review before recovery. Inspection did not acquire a sync claim, import items, or write audit records.'
      }
      if (code === 'identity_preview_unsupported') {
        return 'Source identity inspection is available only for approved local JSON feeds. HTTP feeds are not inspected and no provider request is made. An operator must use an approved local export for this review; do not retry via sync or migration.'
      }
      if (code === 'identity_preview_changed') {
        return 'Feed configuration or recorded sync status changed during source identity inspection, so no preview was accepted. Refresh feed status, then inspect source identities again. This remains a limited current-local-feed and active-operation observation, not a transaction-wide snapshot or a complete historical inventory. No provider request, import, sync claim, or audit write was performed.'
      }
      return 'Source identity inspection was refused due to a feed conflict. Refresh feed status and request operator review if it persists. No import or reconciliation was performed.'
    }
    if (response?.['status'] === 404 && code === 'feed_not_found') {
      return 'This feed is no longer available for source identity inspection. Refresh feed status and select a current feed. No import or reconciliation was performed.'
    }
    if (response?.['status'] === 400 && code === 'feed_invalid') {
      return 'Source identity inspection could not validate the local feed. An operator must check the approved file, provider and scope before another inspection. No import or reconciliation was performed.'
    }
    if (response?.['status'] === 503 && code === 'feed_storage_unavailable') {
      return 'Source identity inspection could not verify local feed storage. Refresh feed status; request operator review of local diagnostics if this persists. No import or reconciliation was performed.'
    }
    return 'Source identity inspection could not be completed. Check the approved local feed and retry inspection. No provider request, import, sync claim, or audit write is performed by inspection; reconciliation remains unverified.'
  }

  private isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value)
  }

  private validIdentityObservedAt(value: unknown): boolean {
    if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|\+00:00)$/.test(value)) return false
    const instant = new Date(value)
    // Date normalizes impossible dates; verify the calendar and clock fields too.
    return !Number.isNaN(instant.getTime()) && instant.toISOString().slice(0, 19) === value.slice(0, 19)
  }

  private validIdentityPreview(report: unknown, feedId: string): report is IFeedIdentityPreview {
    const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
    const label = (value: unknown): value is string => typeof value === 'string' && value.trim().length > 0
    const states = ['unseen', 'canonical', 'historical', 'coexisting'] as const
    if (!this.isRecord(report)
      || report['feedId'] !== feedId
      || report['scope'] !== 'current_local_feed_active_operations'
      || report['historicalInventoryComplete'] !== false
      || !this.validIdentityObservedAt(report['observedAt'])
      || !count(report['itemsObserved'])
      || !count(report['itemsInspected'])) return false

    // Match the backend's bounded 100-item sample, not a global inventory.
    if (report['itemsInspected'] !== Math.min(report['itemsObserved'], 100)
      || report['truncated'] !== (report['itemsObserved'] > 100)) return false
    const counts = report['counts']
    const items = report['items']
    if (!this.isRecord(counts) || !states.every((state) => count(counts[state]))
      || !Array.isArray(items) || items.length !== report['itemsInspected']) return false

    const observedCounts = { unseen: 0, canonical: 0, historical: 0, coexisting: 0 }
    for (const item of items) {
      if (!this.isRecord(item) || !label(item['externalId']) || !label(item['title'])) return false
      const state = states.find((candidate) => candidate === item['state'])
      if (!state) return false
      const canonical = item['canonicalOperationId']
      const historical = item['historicalOperationId']
      if ((canonical !== undefined && !label(canonical)) || (historical !== undefined && !label(historical))) return false
      if ((canonical !== undefined) !== (state === 'canonical' || state === 'coexisting')
        || (historical !== undefined) !== (state === 'historical' || state === 'coexisting')) return false
      observedCounts[state] += 1
    }
    return states.every((state) => counts[state] === observedCounts[state])
  }

  sync(feedHealth: IFeedHealth): void {
    const feedId = feedHealth.feed.id
    if (this.loading || this.feedLoadError || !this.isFeedSyncable(feedHealth) || this.syncing[feedId] || this.syncingAll) return

    this.feedback = undefined
    this.syncing[feedId] = true
    this.service.sync(feedId).pipe(
      timeout(this.operationTimeoutMs),
      takeUntil(this.destroyed$),
      finalize(() => { this.syncing[feedId] = false }),
    ).subscribe({
      next: (report) => {
        const result = this.describeReport(feedHealth, report)
        this.feedback = result
        if (result.kind === 'warning') {
          this.notification.warning('Sync needs review', result.text)
        } else {
          this.notification.success('Sync complete', result.text)
        }
        this.refresh()
      },
      error: () => {
        const text = `${feedHealth.feed.name}: sync could not be confirmed. Refresh feed status; an interrupted import needs operator review before retrying.`
        this.feedback = { kind: 'error', text }
        this.notification.error('Sync failed', text)
        this.refresh()
      },
    })
  }

  syncAll(): void {
    if (this.syncAllDisabled) return

    this.feedback = undefined
    this.syncingAll = true
    this.service.syncDue().pipe(
      timeout(this.operationTimeoutMs),
      takeUntil(this.destroyed$),
      finalize(() => { this.syncingAll = false }),
    ).subscribe({
      next: (response) => {
        const reports = Array.isArray(response?.reports) ? response.reports : []
        const read = reports.reduce((total, report) => total + (report.itemsRead ?? 0), 0)
        const created = reports.reduce((total, report) => total + (report.operationsCreated ?? 0), 0)
        const refreshed = reports.reduce((total, report) => total + (report.operationsRefreshed ?? 0), 0)
        const privacyFlagged = reports.reduce((total, report) => total + (report.privacyFlagged ?? 0), 0)
        const failedReports = reports.filter((report) => (report.errors?.length ?? 0) > 0 || report.recorded !== true)
        const failureDetails = failedReports.map((report) => {
          const name = this.feeds.find((feed) => feed.feed.id === report.feedId)?.feed.name ?? 'An unlisted feed'
          return `${name}: ${this.reportErrorSummary(report)}`
        })
        const text = reports.length === 0
          ? 'No enabled feed was synced. Feed settings may have changed; refresh status before retrying.'
          : `${reports.length} feeds: ${read} items read, ${created} new operations, ${refreshed} refreshed, ${privacyFlagged} privacy-flagged.${failedReports.length ? ` ${failedReports.length} feed${failedReports.length === 1 ? '' : 's'} reported sync issues: ${failureDetails.join(' ')}` : ''}`
        const kind: FeedbackKind = failedReports.length || reports.length === 0 ? 'warning' : 'success'
        this.feedback = { kind, text }
        if (kind === 'warning') this.notification.warning('Sync needs review', text)
        else this.notification.success('Sync complete', text)
        this.refresh()
      },
      error: () => {
        const text = 'Sync-all could not be confirmed. Refresh feed status; an interrupted import needs operator review before retrying.'
        this.feedback = { kind: 'error', text }
        this.notification.error('Sync failed', text)
        this.refresh()
      },
    })
  }

  isFeedSyncable(feed: IFeedHealth): boolean {
    return feed.connectionStatus === 'available' && (feed.syncState === undefined || feed.syncState === 'idle')
  }

  hasActiveClaim(feed: IFeedHealth): boolean {
    return feed.syncState === 'running_or_interrupted'
  }

  feedSyncDescription(feed: IFeedHealth): string {
    if (feed.syncState !== undefined && feed.syncState !== 'idle' && !this.hasActiveClaim(feed)) {
      return 'Sync state could not be verified. Refresh before starting another import.'
    }
    return this.hasActiveClaim(feed)
      ? 'An import is running or was interrupted. Refresh status; operator review is required before recovering an interrupted run.'
      : this.statusDescription(feed.connectionStatus)
  }

  statusLabel(status: string): string {
    switch (status) {
      case 'available':
        return 'Connector available'
      case 'credentials_present_unverified':
        return 'Credentials unverified'
      case 'credentials_required':
        return 'Credentials required'
      case 'contract_only':
        return 'Unavailable here'
      default:
        return 'Status unknown'
    }
  }

  statusDescription(status: string): string {
    switch (status) {
      case 'available':
        return 'This feed type is supported. Availability alone does not confirm that its latest read succeeded.'
      case 'credentials_present_unverified':
        return 'Credentials are present, but a successful provider read has not been verified.'
      case 'credentials_required':
        return 'A connection cannot be tested until the required credentials are configured.'
      case 'contract_only':
        return 'There is no live connector here. Use Connected Sources or the documented manual import route.'
      default:
        return 'The feed status is not recognized, so HAI will not sync it automatically from this page.'
    }
  }

  statusColor(status: string): string {
    switch (status) {
      case 'available':
        return 'blue'
      case 'credentials_present_unverified':
        return 'gold'
      case 'credentials_required':
        return 'orange'
      default:
        return 'default'
    }
  }

  lastAttemptAt(feed: IFeedHealth): string {
    const attemptedAt = feed.lastAttemptAt ?? feed.lastSyncedAt
    if (!attemptedAt) return 'No sync attempt recorded'
    const date = new Date(attemptedAt)
    return Number.isNaN(date.getTime()) ? 'Attempt time unavailable' : date.toLocaleString(undefined, { timeZone: 'UTC', timeZoneName: 'short' })
  }

  lastAttemptCount(feed: IFeedHealth): string {
    if (this.hasActiveClaim(feed)) return 'Current read count unverified'
    if (!(feed.lastAttemptAt ?? feed.lastSyncedAt)) return 'No read count recorded'
    const count = Number(feed.lastItemsRead)
    return Number.isFinite(count) && count >= 0 ? `${count} items read` : 'Read count unavailable'
  }

  lastSuccessfulSyncAt(feed: IFeedHealth): string {
    if (!feed.lastSyncedAt) return 'No successful sync recorded'
    const date = new Date(feed.lastSyncedAt)
    return Number.isNaN(date.getTime()) ? 'Successful sync time unavailable' : date.toLocaleString(undefined, { timeZone: 'UTC', timeZoneName: 'short' })
  }

  timestampInstant(value?: string): string | null {
    if (!value) return null
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? null : date.toISOString()
  }

  syncAllDisabledReason(): string {
    if (this.loading) return 'Wait until feed status has finished loading.'
    if (this.feedLoadError) return 'Refresh feed status before starting another import.'
    if (this.hasSyncInProgress) return 'A feed sync is already running. Wait for it to finish.'
    if (this.enabledFeeds.length === 0) return 'There are no enabled feeds to sync.'
    if (!this.syncAllSafe) return 'Bulk sync is paused because at least one enabled feed is unavailable or unverified.'
    return 'Sync every enabled feed with an available connector.'
  }

  trackFeed(_index: number, feed: IFeedHealth): string {
    return feed.feed.id
  }

  trackBridge(_index: number, bridge: IBridgeContract): string {
    return bridge.provider
  }

  bridgeItemTypes(bridge: IBridgeContract): string[] {
    return Array.isArray(bridge.itemTypes) ? bridge.itemTypes : []
  }

  bridgeConnectorPreference(bridge: IBridgeContract): string[] {
    return Array.isArray(bridge.connectorPreference) ? bridge.connectorPreference : []
  }

  bridgeRequiredScopes(bridge: IBridgeContract): string[] {
    return Array.isArray(bridge.requiredScopes) ? bridge.requiredScopes : []
  }

  bridgeSetupRequirements(bridge: IBridgeContract): ISetupRequirement[] {
    return Array.isArray(bridge.setupRequirements) ? bridge.setupRequirements : []
  }

  goBack(): void {
    this.router.navigate(['/control-center'])
  }

  openConnectedSources(): void {
    this.router.navigate(['/connected-sources'])
  }

  private describeReport(feed: IFeedHealth, report: ISyncReport): ActionFeedback {
    const read = Number.isFinite(report.itemsRead) ? report.itemsRead : 0
    const created = Number.isFinite(report.operationsCreated) ? report.operationsCreated : 0
    const privacyFlagged = Number.isFinite(report.privacyFlagged) ? report.privacyFlagged : 0
    const errors = Array.isArray(report.errors) ? report.errors.length : 0
    const detail = errors || report.recorded !== true ? ` ${errors ? `HAI reported ${errors} sync issue${errors === 1 ? '' : 's'}: ` : ''}${this.reportErrorSummary(report)} Review before relying on the import.` : ''
    const text = `${feed.feed.name}: ${read} items read, ${created} new operations, ${privacyFlagged} privacy-flagged.${detail}`
    return { kind: errors || report.recorded !== true ? 'warning' : 'success', text }
  }

  private reportErrorSummary(report: ISyncReport): string {
    const details = Array.isArray(report.errors)
      ? report.errors.filter((error): error is string => typeof error === 'string' && error.trim().length > 0).map((error) => error.trim())
      : []
    if (report.recorded !== true) details.push('The completion audit was not confirmed; refresh status before retrying.')
    return details.length ? details.join('; ') : 'The sync returned an issue without details.'
  }
}

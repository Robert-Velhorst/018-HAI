import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core'
import { ActivatedRoute, Router } from '@angular/router'
import { forkJoin, Subscription, TimeoutError } from 'rxjs'
import { finalize, map, switchMap, timeout } from 'rxjs/operators'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  IAccountFeed,
  IBackgroundRunReport,
  IOperation,
  IOperationApprovalPreview,
  IOperationEvent,
  IOperationRunResult,
  IOperationsDashboard,
  operationRunVerified,
  readOperationRunResult,
} from '../../models/background-operations.model.interface'
import { safeOutcomeText, safeReceiptSummary } from '../../models/safe-outcome.model.interface'
import { BackgroundOperationsService, isSourceDerivedOperation } from '../../services/background-operations.service'

class StaleApprovalPreviewError extends Error {}

interface FailureContext {
  message: string
  statusLabel: string
  recovery: string
  retryAfter?: string
  tone: 'error' | 'warning'
}

interface OperationActionIssue extends FailureContext {
  operationStatus: string
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-background-operations',
    templateUrl: './background-operations.component.html',
    styleUrls: ['./background-operations.component.scss'],
    standalone: false
})
export class BackgroundOperationsComponent implements OnInit, OnDestroy {
  dashboard?: IOperationsDashboard
  operations: IOperation[] = []
  feeds: IAccountFeed[] = []
  lastReport?: IBackgroundRunReport
  lastRunError = ''
  lastRunHeading = 'The pass request failed'
  lastRunFailure?: FailureContext
  lastRunNotice = ''
  lastRunNoticeType: 'sources' | 'refresh' = 'sources'
  loadError = ''
  loadFailure?: FailureContext
  eventsLoadError = ''
  eventsFailure?: FailureContext

  loading = false
  running = false
  eventsLoading = false
  selectedLoading = false
  selectedLoadError = ''
  selectedFailure?: FailureContext
  statusFilter = ''
  lastRunNeedsReconciliation = false
  lastRunStatusRefreshed = false

  selected?: IOperation
  selectedEvents: IOperationEvent[] = []
  detailVisible = false
  sourceApprovalReviewVisible = false
  sourceApprovalReviewLoading = false
  sourceApprovalReviewConfirming = false
  sourceApprovalReviewError = ''
  sourceApprovalReview?: IOperationApprovalPreview
  private sourceApprovalReviewBase?: IOperation

  private refreshSubscription?: Subscription
  private detailEventsSubscription?: Subscription
  private selectedOperationSubscription?: Subscription
  private readonly actionSubscriptions = new Subscription()
  private readonly pendingOperationIds = new Set<string>()
  private readonly operationActionErrors = new Map<string, OperationActionIssue>()
  private readonly runOutcomes = new Map<string, IOperationRunResult>()
  // A successful read cannot reconcile effects reported by a failed mutation.
  private readonly uncertainOperationIds = new Set<string>()
  private readonly uncertainOperations = new Map<string, IOperation>()

  private readonly loadTimeoutMs = 6000
  private readonly operationTimeoutMs = 30000

  readonly statusFilters = [
    { value: '', label: 'All' },
    { value: 'awaiting_approval', label: 'Needs approval' },
    { value: 'interrupted', label: 'Reconciliation required' },
    { value: 'completed', label: 'Completed' },
    { value: 'blocked', label: 'Blocked' },
    { value: 'failed', label: 'Failed' },
  ]

  constructor(
    private service: BackgroundOperationsService,
    private notification: NzNotificationService,
    private router: Router,
    private route: ActivatedRoute
  ) {}

  ngOnInit(): void {
    const requestedStatus = this.route.snapshot.queryParamMap.get('status') ?? ''
    if (this.statusFilters.some((filter) => filter.value === requestedStatus)) this.statusFilter = requestedStatus
    this.refresh()
  }

  ngOnDestroy(): void {
    this.refreshSubscription?.unsubscribe()
    this.detailEventsSubscription?.unsubscribe()
    this.selectedOperationSubscription?.unsubscribe()
    this.actionSubscriptions.unsubscribe()
  }

  refresh(): void {
    this.refreshSubscription?.unsubscribe()
    this.loadError = ''
    this.loadFailure = undefined
    this.loading = true
    this.refreshSubscription = forkJoin({
      overview: this.service.overview(this.statusFilter ? { status: this.statusFilter } : undefined),
      feeds: this.service.feeds(),
    }).pipe(
      timeout(this.loadTimeoutMs),
      finalize(() => (this.loading = false)),
    ).subscribe({
      next: ({ overview, feeds }) => {
        this.dashboard = overview.dashboard
        this.operations = overview.operations ?? []
        for (const operation of this.operations) {
          if (operation.status === 'interrupted' && !this.uncertainOperationIds.has(operation.id)) {
            this.recordOperationUncertainty(operation, 'This operation is interrupted; previous effects require reconciliation.')
          }
        }
        this.feeds = feeds.feeds ?? []
        this.loadFailure = undefined
        if (this.lastRunNeedsReconciliation) {
          this.lastRunNeedsReconciliation = false
          this.lastRunStatusRefreshed = true
        }
        this.refreshSelectedOperation()
      },
      error: (err) => {
        this.loadFailure = this.failureContext(err, this.backgroundLoadError(err), 'read')
        this.loadError = this.loadFailure.message
      },
    })
  }

  setFilter(value: string): void {
    this.statusFilter = value
    this.refresh()
  }

  runBackground(): void {
    if (this.running) return
    if (this.lastRunNeedsReconciliation) {
      this.lastRunNotice = 'A previous pass may have reached the worker. Refresh status and review the current operations before starting another pass.'
      this.lastRunNoticeType = 'refresh'
      return
    }
    if (this.loading || this.loadError) {
      this.lastRunNotice = 'Feed status is not current. Wait for the refresh to complete or retry it before starting a pass.'
      this.lastRunNoticeType = 'refresh'
      return
    }
    if (!this.hasEnabledFeeds()) {
      this.lastRunNotice = 'No enabled account feed is configured for this background pass. Connect or enable a feed first.'
      this.lastRunNoticeType = 'sources'
      return
    }
    this.running = true
    this.lastRunError = ''
    this.lastRunFailure = undefined
    this.lastRunStatusRefreshed = false
    this.lastRunNotice = ''
    this.actionSubscriptions.add(
      this.service.runBackground().pipe(
        timeout(this.operationTimeoutMs),
        finalize(() => (this.running = false)),
      ).subscribe({
        next: (report) => {
          this.lastReport = report
          this.lastRunNeedsReconciliation = false
          this.refresh()
        },
        error: (err) => {
          this.lastRunFailure = this.failureContext(err, this.backgroundRunError(err), 'pass')
          this.lastRunError = this.lastRunFailure.message
          this.lastRunHeading = this.backgroundRunHeadline(err)
          this.lastRunNeedsReconciliation = true
        },
      }),
    )
  }

  backgroundRunError(err: unknown): string {
    const response = err as HttpErrorResponse
    const detail = response?.error?.error
    if (typeof detail === 'string' && detail.trim()) return this.safeDiagnosticText(detail)
    if (err instanceof TimeoutError || response?.status === 0 || response?.status >= 500) {
      return 'HAI could not confirm whether the pass started. The request may have reached the worker.'
    }
    if (response?.status === 404) {
      return 'The background engine is not reachable. Refresh the page after the local gateway has restarted.'
    }
    if (response?.status === 401 || response?.status === 403) {
      return 'This action requires an owner account with permission to run controlled background work. Sign in as the local owner, then try again.'
    }
    if (response?.status === 409) {
      return 'A background pass is already running. Wait a moment, then refresh this page.'
    }
    if (response?.status === 429) {
      return 'The background engine is temporarily rate-limited. Wait a moment before trying again.'
    }
    return 'The background pass returned an error without confirming what completed. Refresh status before retrying.'
  }

  private backgroundRunHeadline(err: unknown): string {
    const response = err as HttpErrorResponse
    if (err instanceof TimeoutError || response?.status === 0 || response?.status >= 500) return 'The pass outcome is unknown'
    if (response?.status === 401 || response?.status === 403) return 'The pass was not authorized'
    if (response?.status === 404) return 'The background route is unavailable'
    if (response?.status === 409) return 'Another pass is already running'
    if (response?.status === 429) return 'The server rate-limited the pass request'
    return 'The pass request failed'
  }

  backgroundLoadError(err: unknown): string {
    const response = err as HttpErrorResponse
    const detail = response?.error?.error
    if (typeof detail === 'string' && detail.trim()) return this.safeDiagnosticText(detail)
    if (response?.status === 401 || response?.status === 403) {
      return 'Your session cannot read background operations. Sign in with an account that can access this workspace, then retry.'
    }
    if (response?.status === 404) {
      return 'The background operations endpoint is unavailable. Retry after the local gateway has restarted.'
    }
    if (err instanceof TimeoutError || response?.status === 0 || response?.status >= 500) {
      return 'Background operations are temporarily unavailable. This read-only refresh did not start background work; check that the local backend is running, then retry.'
    }
    return 'The current operation and feed status could not be loaded. Retry to check the latest state.'
  }

  hasEnabledFeeds(): boolean {
    return this.feeds.some((feed) => feed.enabled)
  }

  canRunBackground(): boolean {
    return !this.loading && !this.loadError && !this.lastRunNeedsReconciliation && this.hasEnabledFeeds()
  }

  get backgroundRunUnavailableReason(): string {
    if (this.running) return ''
    if (this.lastRunNeedsReconciliation) return 'Refresh status and review the current operations before starting another pass.'
    if (this.loading) return 'Feed status is being refreshed.'
    if (this.loadError) return 'Feed status is not current. Resolve the refresh issue before starting work.'
    if (!this.hasEnabledFeeds()) return 'No enabled account feed is available for this pass.'
    return ''
  }

  get enabledFeeds(): IAccountFeed[] {
    return this.feeds.filter((feed) => feed.enabled)
  }

  selectStatus(status: string): void {
    const nextStatus = status && this.statusFilter === status ? '' : status
    if (this.statusFilter === nextStatus) return
    this.statusFilter = nextStatus
    this.refresh()
  }

  openDetail(op: IOperation): void {
    this.selectedOperationSubscription?.unsubscribe()
    this.selectedLoading = false
    this.selectedLoadError = ''
    this.selectedFailure = undefined
    this.selected = op
    this.selectedEvents = []
    this.eventsLoadError = ''
    this.eventsFailure = undefined
    this.detailVisible = true
    this.loadEvents(op)
  }

  retrySelectedEvents(): void {
    if (this.selected) this.loadEvents(this.selected)
  }

  private loadEvents(op: IOperation): void {
    this.detailEventsSubscription?.unsubscribe()
    this.eventsLoading = true
    this.eventsLoadError = ''
    this.eventsFailure = undefined
    this.detailEventsSubscription = this.service.events(op.id).pipe(
      timeout(this.loadTimeoutMs),
      finalize(() => (this.eventsLoading = false)),
    ).subscribe({
      next: (res) => {
        if (this.selected?.id === op.id) {
          this.selectedEvents = res.events ?? []
          this.eventsFailure = undefined
        }
      },
      error: (err) => {
        if (this.selected?.id === op.id) {
          this.eventsFailure = this.failureContext(err, 'The audit trail could not be loaded. Your operation was not changed.', 'read')
          this.eventsLoadError = this.eventsFailure.message
        }
      },
    })
  }

  closeDetail(): void {
    this.detailEventsSubscription?.unsubscribe()
    this.selectedOperationSubscription?.unsubscribe()
    this.eventsLoading = false
    this.selectedLoading = false
    this.detailVisible = false
    this.selected = undefined
    this.selectedLoadError = ''
    this.selectedFailure = undefined
  }

  approve(op: IOperation): void {
    if (this.loading || this.loadError || !this.canApprove(op) || this.pendingOperationIds.has(op.id)) return
    if (isSourceDerivedOperation(op)) {
      if (this.sourceApprovalReviewVisible) return
      this.openSourceApprovalReview(op)
      return
    }
    this.submitLegacyApproval(op)
  }

  confirmSourceApprovalReview(): void {
    const displayed = this.sourceApprovalReview
    const base = this.sourceApprovalReviewBase
    if (!this.sourceApprovalReviewVisible || !displayed || !base || this.sourceApprovalReviewLoading ||
      this.sourceApprovalReviewConfirming || !!this.sourceApprovalReviewError || this.pendingOperationIds.has(base.id)) return

    this.pendingOperationIds.add(base.id)
    this.sourceApprovalReviewConfirming = true
    this.sourceApprovalReviewError = ''
    let freshPreviewLoaded = false
    this.actionSubscriptions.add(this.service.approvalPreview(base.id).pipe(
      timeout(this.operationTimeoutMs),
      map((fresh) => {
        freshPreviewLoaded = true
        if (!this.sameDisplayedSourceRevision(displayed, fresh)) {
          throw new StaleApprovalPreviewError('The operation changed after it was reviewed.')
        }
        return fresh
      }),
      switchMap((fresh) => this.service.approve(base.id, {
        expectedVersion: fresh.revision.version,
        revisionDigest: fresh.revision.revisionDigest,
      })),
      finalize(() => {
        this.pendingOperationIds.delete(base.id)
        this.sourceApprovalReviewConfirming = false
      }),
    ).subscribe({
      next: (updated) => {
        if (this.selected?.id === base.id) this.selected = updated
        this.operationActionErrors.delete(base.id)
        this.sourceApprovalReviewConfirming = false
        this.closeSourceApprovalReview()
        this.notification.success('Approved', `${base.title} approved.`)
        this.refresh()
      },
      error: (err) => {
        if (!freshPreviewLoaded || err instanceof StaleApprovalPreviewError) {
          const stale = err instanceof StaleApprovalPreviewError
          const message = stale
            ? 'The operation changed after review. No approval was submitted.'
            : 'HAI could not validate the current source revision. No approval was submitted.'
          this.sourceApprovalReviewError = message
          this.operationActionErrors.set(base.id, {
            message,
            statusLabel: stale ? 'Approval not submitted: review changed' : 'Approval not submitted: review unavailable',
            recovery: stale
              ? 'Refresh status, inspect the updated operation, then start a new review.'
              : 'Refresh status, then start a new review to fetch the current operation details.',
            operationStatus: base.status,
            tone: 'warning',
          })
          this.refresh()
          return
        }

        const context = this.failureContext(err, this.operationActionErrorMessage(err, 'Approval failed.'), 'operation')
        this.sourceApprovalReviewError = `${context.message} ${context.recovery}`
        this.operationActionErrors.set(base.id, { ...context, operationStatus: base.status })
        if (this.shouldRefreshAfterActionError(err)) this.refresh()
      },
    }))
  }

  closeSourceApprovalReview(): void {
    if (this.sourceApprovalReviewLoading || this.sourceApprovalReviewConfirming) return
    this.sourceApprovalReviewVisible = false
    this.sourceApprovalReviewLoading = false
    this.sourceApprovalReviewError = ''
    this.sourceApprovalReview = undefined
    this.sourceApprovalReviewBase = undefined
  }

  sourceApprovalField(op: IOperation, field: string): string {
    const value = (op as IOperation & Record<string, unknown>)[field]
    return typeof value === 'string' ? this.safeDiagnosticText(value) : ''
  }

  sourceApprovalEvidence(op: IOperation): string {
    const value = (op as IOperation & Record<string, unknown>)['evidence']
    if (typeof value === 'string') return this.safeDiagnosticText(value)
    if (value && typeof value === 'object') {
      try {
        return this.safeDiagnosticText(JSON.stringify(value, null, 2))
      } catch {
        return 'Evidence is available but could not be rendered safely.'
      }
    }
    return ''
  }

  retrySourceApprovalReview(): void {
    const base = this.sourceApprovalReviewBase
    if (!base || this.sourceApprovalReviewLoading || this.sourceApprovalReviewConfirming) return
    this.sourceApprovalReview = undefined
    this.sourceApprovalReviewError = ''
    this.loadSourceApprovalReview(base)
  }

  private openSourceApprovalReview(op: IOperation): void {
    this.sourceApprovalReviewBase = op
    this.sourceApprovalReview = undefined
    this.sourceApprovalReviewError = ''
    this.sourceApprovalReviewVisible = true
    this.loadSourceApprovalReview(op)
  }

  private loadSourceApprovalReview(op: IOperation): void {
    if (this.sourceApprovalReviewLoading || this.sourceApprovalReviewConfirming || this.pendingOperationIds.has(op.id)) return
    this.pendingOperationIds.add(op.id)
    this.sourceApprovalReviewLoading = true
    this.sourceApprovalReviewError = ''
    this.actionSubscriptions.add(this.service.approvalPreview(op.id).pipe(
      timeout(this.operationTimeoutMs),
      map((preview) => {
        if (this.sourceApprovalReviewBase !== op || !this.sourceApprovalReviewVisible ||
          !this.matchesReviewedOperation(op, preview.operation) ||
          preview.revision.operationId !== op.id || preview.revision.version !== op.version) {
          throw new StaleApprovalPreviewError('The operation changed since it was displayed.')
        }
        return preview
      }),
      finalize(() => {
        this.pendingOperationIds.delete(op.id)
        this.sourceApprovalReviewLoading = false
      }),
    ).subscribe({
      next: (preview) => {
        this.sourceApprovalReview = preview
      },
      error: (err) => {
        if (this.sourceApprovalReviewBase !== op || !this.sourceApprovalReviewVisible) return
        const stale = err instanceof StaleApprovalPreviewError
        this.sourceApprovalReviewError = stale
          ? 'The operation changed since it was displayed. No approval was submitted.'
          : 'HAI could not load a valid source approval preview. No approval was submitted.'
        this.operationActionErrors.set(op.id, {
          message: this.sourceApprovalReviewError,
          statusLabel: stale ? 'Review changed' : 'Review unavailable',
          recovery: stale
            ? 'Refresh status, inspect the updated operation, then start a new review.'
            : 'Refresh status, then retry by starting a new review.',
          operationStatus: op.status,
          tone: 'warning',
        })
        this.refresh()
      },
    }))
  }

  private sameDisplayedSourceRevision(displayed: IOperationApprovalPreview, fresh: IOperationApprovalPreview): boolean {
    return displayed.revision.operationId === displayed.operation.id &&
      fresh.revision.operationId === displayed.operation.id &&
      displayed.revision.version === displayed.operation.version &&
      fresh.revision.version === displayed.revision.version &&
      fresh.revision.revisionDigest === displayed.revision.revisionDigest &&
      this.matchesReviewedOperation(displayed.operation, fresh.operation)
  }

  private submitLegacyApproval(op: IOperation): void {
    if (this.pendingOperationIds.has(op.id)) return
    this.pendingOperationIds.add(op.id)
    this.operationActionErrors.delete(op.id)
    this.actionSubscriptions.add(this.service.approve(op.id).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.pendingOperationIds.delete(op.id)),
    ).subscribe({
      next: (updated) => {
        if (this.selected?.id === op.id) this.selected = updated
        this.operationActionErrors.delete(op.id)
        this.notification.success('Approved', `${op.title} approved.`)
        this.refresh()
      },
      error: (err) => {
        const context = this.failureContext(err, this.operationActionErrorMessage(err, 'Approval failed.'), 'operation')
        this.operationActionErrors.set(op.id, { ...context, operationStatus: op.status })
        if (this.shouldRefreshAfterActionError(err)) this.refresh()
      },
    }))
  }

  run(op: IOperation): void {
    if (this.loading || this.loadError || !this.canRun(op) || this.pendingOperationIds.has(op.id)) return
    this.pendingOperationIds.add(op.id)
    this.operationActionErrors.delete(op.id)
    let receivedOutcome = false
    this.actionSubscriptions.add(this.service.run(op.id).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.pendingOperationIds.delete(op.id)),
    ).subscribe({
      next: (res) => {
        receivedOutcome = true
        const result = readOperationRunResult(res)
        if (result) this.runOutcomes.set(op.id, result)
        const correlated = !!result?.operation?.id && result.operation.id === op.id &&
          (!result.operationId || result.operationId === op.id)
        const operation = correlated ? { ...op, ...result?.operation } : op
        if (this.selected?.id === op.id) this.selected = operation
        const verified = !!result && correlated && operationRunVerified(result)
        const knownFailure = !!result && correlated && result.failed && !result.error &&
          !result.verified && !result.interrupted && !result.reconciliationRequired &&
          operation.status === 'failed' && operation.verificationStatus !== 'passed' &&
          !result.receipt?.ok && !result.receipt?.verification?.passed && !result.receipt?.output?.artifactHash
        if (knownFailure) {
          const message = this.safeDiagnosticText(operation.lastError?.trim() || operation.resultSummary?.trim() || 'The worker marked this operation as failed.')
          this.operationActionErrors.set(op.id, {
            message,
            statusLabel: 'Worker reported failure',
            recovery: this.operationRecoveryContext(operation),
            operationStatus: operation.status,
            tone: 'error',
          })
        } else if (verified) {
          this.operationActionErrors.delete(op.id)
          this.notification.success('Verified', `${op.title} executed and verified.`)
        } else {
          this.recordOperationUncertainty(op, result?.error)
        }
        this.refresh()
      },
      error: (err) => {
        receivedOutcome = true
        const response = err as HttpErrorResponse
        const result = readOperationRunResult(response?.error)
        if (result) this.runOutcomes.set(op.id, result)
        const uncertain = !!result && (!!result.receipt || result.interrupted === true ||
          result.reconciliationRequired === true || result.verified || result.operation?.status === 'interrupted')
        if (uncertain || err instanceof TimeoutError || response?.status === 0 || response?.status >= 500 || !(err instanceof HttpErrorResponse)) {
          this.recordOperationUncertainty(op, result?.error)
        } else {
          const context = this.failureContext(err, this.operationActionErrorMessage(err, 'This operation cannot be executed.'), 'operation')
          this.operationActionErrors.set(op.id, { ...context, operationStatus: op.status })
        }
        if (this.shouldRefreshAfterActionError(err)) this.refresh()
      },
      complete: () => {
        if (!receivedOutcome) this.recordOperationUncertainty(op)
      },
    }))
  }

  operationActionError(op: IOperation): string {
    return this.operationActionIssue(op)?.message ?? ''
  }

  operationActionIssue(op: IOperation): OperationActionIssue | undefined {
    const issue = this.operationActionErrors.get(op.id)
    if (this.uncertainOperationIds.has(op.id)) return issue ?? {
      message: 'The previous execution requires reconciliation. Another run is unavailable.',
      statusLabel: 'Completion not verified',
      recovery: 'Inspect the operation and audit trail; refreshing status does not reconcile effects.',
      operationStatus: op.status,
      tone: 'warning',
    }
    return issue?.operationStatus === op.status || issue?.statusLabel.startsWith('Approval not submitted:') ? issue : undefined
  }

  private recordOperationUncertainty(op: IOperation, detail?: string): void {
    this.uncertainOperationIds.add(op.id)
    const snapshot = this.runOutcomes.get(op.id)?.operation
    this.uncertainOperations.set(op.id, snapshot?.id === op.id ? { ...op, ...snapshot } : op)
    this.operationActionErrors.set(op.id, {
      message: safeOutcomeText(detail) || 'HAI could not confirm completion. The request may have produced effects.',
      statusLabel: 'Reconciliation required',
      recovery: 'Inspect the operation, retained receipt and audit trail. Another run is blocked; a status refresh alone does not reconcile effects.',
      operationStatus: op.status,
      tone: 'warning',
    })
  }

  operationOutcomeSummary(op: IOperation): string {
    const result = this.runOutcomes.get(op.id)
    if (!result) return ''
    const parts = [`Operation: ${safeOutcomeText(result.operationId || result.operation?.id || op.id, 128)}.`]
    if (result.operation?.status) parts.push(`Returned status: ${safeOutcomeText(result.operation.status, 64)}.`)
    if (result.outcomeRecorded === false) parts.push('Completion not confirmed recorded.')
    if (result.outcomeRecorded === true) parts.push('Outcome reported recorded; storage durability is not established.')
    const receipt = safeReceiptSummary(result)
    if (receipt) parts.push(receipt)
    return parts.join(' ')
  }

  operationVerificationLabel(op: IOperation): string {
    if (this.uncertainOperationIds.has(op.id) || op.status === 'interrupted') return 'Not confirmed; reconciliation required'
    if (op.verificationStatus === 'passed' && op.status !== 'completed') return 'Worker check passed; completion not confirmed'
    return this.titleCase(op.verificationStatus)
  }

  operationRecoveryContext(op: IOperation): string {
    if (this.uncertainOperationIds.has(op.id)) return 'Another run is unavailable until the previous effects are reconciled.'
    if (op.status === 'failed') {
      return this.canRun(op)
        ? 'A safe worker retry is currently permitted. Review the latest failure and audit trail before retrying.'
        : 'Retry is unavailable: the current status or recorded decision does not permit the safe worker.'
    }
    if (op.status === 'blocked') return 'Retry is unavailable while this operation is blocked. Inspect the recorded reason and audit trail.'
    return ''
  }

  operationActionStatus(op: IOperation): string {
    return this.operationActionIssue(op)?.statusLabel ?? ''
  }

  operationActionRecovery(op: IOperation): string {
    return this.operationActionIssue(op)?.recovery ?? ''
  }

  operationActionTone(op: IOperation): 'error' | 'warning' {
    return this.operationActionIssue(op)?.tone ?? 'error'
  }

  retrySelectedOperationRefresh(): void {
    this.refreshSelectedOperation()
  }

  operationStatusContext(op: IOperation): string {
    if (this.uncertainOperationIds.has(op.id)) return ''
    switch (op.status) {
      case 'awaiting_approval':
        return 'Waiting for your approval before work can continue.'
      case 'blocked':
        return this.safeDiagnosticText(op.lastError?.trim()) || 'HAI marked this operation as blocked, but no blocker detail is recorded. Inspect the operation and audit trail.'
      case 'failed':
        return this.safeDiagnosticText(op.lastError?.trim() || op.resultSummary?.trim()) || 'A failure is recorded, but the latest worker attempt has no error detail.'
      case 'approved':
        return 'Approval is recorded; approving an operation does not execute it.'
      case 'interrupted':
        return 'Previous effects require review before another run.'
      default:
        return ''
    }
  }

  private operationActionErrorMessage(err: unknown, fallback: string): string {
    const response = err as HttpErrorResponse
    const detail = response?.error?.error
    const timedOut = err instanceof TimeoutError
    if (typeof detail === 'string' && detail.trim()) return this.safeDiagnosticText(detail)
    if (response?.status === 401 || response?.status === 403) {
      return 'This action requires an owner account with permission to control background work. Sign in as the local owner, then try again.'
    }
    if (response?.status === 404) return 'This operation is no longer available. Refresh the list to confirm its current state.'
    if (response?.status === 409) return 'The operation state changed before the action could be applied. Refresh the list before deciding what to do next.'
    if (response?.status === 429) return 'The engine is temporarily rate-limited. Wait before trying this action again.'
    if (response?.status === 0 || response?.status >= 500 || timedOut) {
      return 'HAI could not confirm the result. Refresh the operation before retrying because the action may already have completed.'
    }
    return fallback
  }

  private failureContext(err: unknown, message: string, action: 'pass' | 'operation' | 'read'): FailureContext {
    const response = err as HttpErrorResponse
    const status = response?.status
    const isTimeout = err instanceof TimeoutError
    const statusLabel = isTimeout
      ? 'Request timed out'
      : status === 0
        ? 'Connection unavailable'
        : typeof status === 'number' && status > 0
          ? `HTTP ${status}`
          : ''
    const retryAfter = this.retryAfterContext(response)

    let recovery = 'Refresh the current status and review the operation before trying again.'
    if (action === 'pass' && (isTimeout || status === 0 || (typeof status === 'number' && status >= 500))) {
      recovery = 'The pass outcome is unknown. Refresh status and inspect recent operations before retrying; the request may have reached the worker.'
    } else if (status === 409) {
      recovery = 'The server reports a state conflict. Refresh the queue and inspect the current operation before another attempt.'
    } else if (status === 429) {
      recovery = 'The server rate-limited this request. Follow any server retry guidance, then refresh status before deciding whether to retry.'
    } else if (status === 401 || status === 403) {
      recovery = 'This action requires an account with the required owner permissions. Confirm the signed-in account before retrying.'
    } else if (status === 404) {
      recovery = action === 'operation'
        ? 'Refresh the queue to confirm whether this operation still exists.'
        : 'Check that the local backend route is available, then refresh this page.'
    } else if (action === 'read') {
      recovery = 'This request only reads status. Retry the refresh after the backend or connection is available.'
    }

    return {
      message: this.safeDiagnosticText(message),
      statusLabel,
      recovery,
      retryAfter,
      tone: isTimeout || status === 0 || status === 409 || status === 429 || (typeof status === 'number' && status >= 500) ? 'warning' : 'error',
    }
  }

  private retryAfterContext(response: HttpErrorResponse): string | undefined {
    const raw = response?.headers?.get('Retry-After')?.trim()
    if (!raw) return undefined
    const seconds = Number(raw)
    if (Number.isFinite(seconds) && seconds >= 0) {
      return `Server retry guidance: wait ${Math.ceil(seconds)} seconds before retrying.`
    }
    const retryAt = Date.parse(raw)
    if (Number.isFinite(retryAt)) {
      return `Server retry guidance: wait until ${new Date(retryAt).toLocaleTimeString()} before retrying.`
    }
    return undefined
  }

  safeDiagnosticText(value: string | undefined | null): string {
    if (!value) return ''
    return value
      .replace(/\b(authorization)\s*[:=]\s*(?:Bearer|Basic)\s+[^\s,;]+/gi, '$1=[redacted]')
      .replace(/\bBearer\s+[A-Za-z0-9._~+\/-]+=*/gi, 'Bearer [redacted]')
      .replace(/\b(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)/gi, '$1=[redacted]')
  }

  private shouldRefreshAfterActionError(err: unknown): boolean {
    const response = err as HttpErrorResponse
    return response?.status === 404 || response?.status === 409 || response?.status === 0 || response?.status >= 500 || err instanceof TimeoutError
  }

  private refreshSelectedOperation(): void {
    const operationId = this.selected?.id
    if (!operationId || !this.detailVisible) return

    this.selectedOperationSubscription?.unsubscribe()
    this.selectedLoadError = ''
    this.selectedFailure = undefined
    this.selectedLoading = true
    this.selectedOperationSubscription = this.service.get(operationId).pipe(
      timeout(this.loadTimeoutMs),
      finalize(() => (this.selectedLoading = false)),
    ).subscribe({
      next: (operation) => {
        if (this.selected?.id === operationId) {
          this.selected = operation
          this.selectedFailure = undefined
        }
      },
      error: (err) => {
        if (this.selected?.id === operationId) {
          this.selectedFailure = this.failureContext(
            err,
            'The latest operation status could not be loaded. The details shown may be out of date.',
            'read',
          )
          this.selectedLoadError = this.selectedFailure.message
        }
      },
    })
  }

  isPending(op: IOperation): boolean {
    return this.pendingOperationIds.has(op.id)
  }

  get visibleOperations(): IOperation[] {
    const order = ['interrupted', 'awaiting_approval', 'blocked', 'failed', 'running', 'verifying', 'ready', 'classified', 'completed']
    const retained = new Map(this.uncertainOperations)
    for (const operation of this.operations) retained.set(operation.id, operation)
    const candidates = [...retained.values()].filter((operation) =>
      this.statusFilter || operation.status !== 'completed' || this.uncertainOperationIds.has(operation.id)
    )
    const ordered = [...candidates]
      .sort((left, right) => {
        const leftRank = order.indexOf(this.operationDisplayStatus(left))
        const rightRank = order.indexOf(this.operationDisplayStatus(right))
        return (leftRank < 0 ? order.length : leftRank) - (rightRank < 0 ? order.length : rightRank)
      })
    const reviewRequired = ordered.filter((operation) => this.operationDisplayStatus(operation) === 'interrupted')
    return [...reviewRequired, ...ordered.filter((operation) => this.operationDisplayStatus(operation) !== 'interrupted')
      .slice(0, Math.max(0, 3 - reviewRequired.length))]
  }

  statusLabel(status: string): string {
    return this.statusFilters.find((filter) => filter.value === status)?.label ?? this.titleCase(status)
  }

  operationDisplayStatus(op: IOperation): string {
    return this.uncertainOperationIds.has(op.id) ? 'interrupted' : op.status
  }

  reviewTime(value: string): string {
    const timestamp = Date.parse(value)
    return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleString() : 'Time unavailable'
  }

  decisionLabel(decision: string): string {
    return this.titleCase(decision)
  }

  ownerLabel(owner: string): string {
    return owner ? this.titleCase(owner) : 'Unassigned'
  }

  riskLabel(risk: string): string {
    return risk ? `${this.titleCase(risk)} risk` : 'Risk unknown'
  }

  titleCase(value: string): string {
    return value.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
  }

  canApprove(op: IOperation): boolean {
    return op.status === 'awaiting_approval'
  }

  private matchesReviewedOperation(reviewed: IOperation, current: IOperation): boolean {
    return Number.isSafeInteger(reviewed.version) && reviewed.version! > 0 &&
      current.id === reviewed.id && current.version === reviewed.version &&
      current.status === reviewed.status && current.requiresApproval === reviewed.requiresApproval &&
      current.updatedAt === reviewed.updatedAt && current.status === 'awaiting_approval' &&
      current.requiresApproval && isSourceDerivedOperation(current)
  }

  canRun(op: IOperation): boolean {
    return !this.uncertainOperationIds.has(op.id) && op.currentDecision === 'run_safe_local_worker' &&
      ['classified', 'ready', 'failed'].includes(op.status)
  }

  riskColor(risk: string): string {
    switch (risk) {
      case 'high':
      case 'critical':
        return 'red'
      case 'medium':
        return 'gold'
      case 'low':
        return 'green'
      default:
        return 'default'
    }
  }

  statusColor(status: string): string {
    switch (status) {
      case 'completed':
        return 'green'
      case 'awaiting_approval':
      case 'interrupted':
        return 'gold'
      case 'blocked':
      case 'failed':
        return 'red'
      case 'running':
      case 'verifying':
        return 'blue'
      default:
        return 'default'
    }
  }

  openSources(): void {
    this.router.navigate(['/connected-sources'])
  }

  openFullLedger(): void {
    this.router.navigate(['/background-operations'], {
      queryParams: { mode: 'advanced', status: this.statusFilter || null },
      fragment: 'operation-ledger',
    })
  }
}

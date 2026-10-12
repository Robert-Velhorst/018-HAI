import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { finalize, timeout } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  IAccountFeed,
  IBackgroundRunReport,
  IOperation,
  IOperationEvent,
  IOperationsDashboard,
} from '../../models/background-operations.model.interface'
import { BackgroundOperationsService } from '../../services/background-operations.service'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-background-operations',
  templateUrl: './background-operations.component.html',
  styleUrls: ['./background-operations.component.scss'],
})
export class BackgroundOperationsComponent implements OnInit {
  dashboard?: IOperationsDashboard
  operations: IOperation[] = []
  feeds: IAccountFeed[] = []
  lastReport?: IBackgroundRunReport
  lastRunError = ''
  overviewLoadError = ''
  lastRunNotice = ''

  loading = false
  running = false
  statusFilter = ''
  private refreshQueued = false

  selected?: IOperation
  selectedEvents: IOperationEvent[] = []
  detailVisible = false
  private readonly pendingOperationActionIds = new Set<string>()

  private readonly loadTimeoutMs = 6000
  private readonly operationTimeoutMs = 30000

  readonly statusFilters = [
    { value: '', label: 'All' },
    { value: 'awaiting_approval', label: 'Needs Robert' },
    { value: 'completed', label: 'Done' },
    { value: 'blocked', label: 'Blocked' },
    { value: 'failed', label: 'Failed' },
  ]

  constructor(
    private service: BackgroundOperationsService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    if (this.loading) {
      this.refreshQueued = true
      return
    }
    this.loading = true
    this.overviewLoadError = ''
    this.service.overview(this.statusFilter ? { status: this.statusFilter } : undefined).pipe(
      timeout(this.loadTimeoutMs),
      finalize(() => this.finishRefresh())
    ).subscribe({
      next: ({ dashboard, operations, feeds }) => {
        this.dashboard = dashboard
        this.operations = operations ?? []
        this.feeds = feeds ?? []
      },
      error: () => {
        this.overviewLoadError = 'Could not load the operational dashboard. The displayed queue may be incomplete.'
        this.notification.error('Error', 'Failed to load background operations.')
      },
    })
  }

  private finishRefresh(): void {
    this.loading = false
    if (!this.refreshQueued) return
    this.refreshQueued = false
    this.refresh()
  }

  setFilter(value: string): void {
    this.statusFilter = value
    this.refresh()
  }

  runBackground(): void {
    if (this.running) return
    if (!this.hasEnabledFeeds()) {
      this.lastRunNotice = 'No enabled account feed is configured for this background pass. Connect or enable a feed first.'
      this.notification.info('Connect a source first', 'This pass reads only enabled account feeds. No work was started.')
      return
    }
    this.running = true
    this.lastRunError = ''
    this.lastRunNotice = ''
    this.service.runBackground().pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => (this.running = false))
    ).subscribe({
      next: (report) => {
        this.lastReport = report
        this.notification.success(
          'Background pass complete',
          `${report.operationsCreated} new, ${report.verified} verified, ${report.awaitingApproval} awaiting approval.`
        )
        this.refresh()
      },
      error: (err) => {
        this.lastRunError = this.backgroundRunError(err)
        this.notification.error('Background pass could not start', this.lastRunError)
      },
    })
  }

  backgroundRunError(err: unknown): string {
    const response = err as HttpErrorResponse
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
    if (response?.status === 0 || response?.status >= 500) {
      return 'The background engine is temporarily unavailable. Your sources and existing operations were not changed. Refresh after the local backend is healthy.'
    }
    const detail = response?.error?.error
    if (typeof detail === 'string' && detail.trim() && response.status >= 400 && response.status < 500) return detail
    return 'The background pass could not start. Your sources and existing operations were not changed.'
  }

  hasEnabledFeeds(): boolean {
    return this.feeds.some((feed) => feed.enabled)
  }

  selectStatus(status: string): void {
    if (this.statusFilter === status) return
    this.statusFilter = status
    this.refresh()
  }

  openDetail(op: IOperation): void {
    this.selected = op
    this.selectedEvents = []
    this.detailVisible = true
    this.service.events(op.id).pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (res) => (this.selectedEvents = res.events ?? []),
      error: () => this.notification.error('Error', 'Failed to load the audit trail.'),
    })
  }

  closeDetail(): void {
    this.detailVisible = false
    this.selected = undefined
  }

  approve(op: IOperation): void {
    if (!this.beginOperationAction(op)) return

    this.service.approve(op.id).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.endOperationAction(op))
    ).subscribe({
      next: () => {
        this.notification.success('Approved', `${op.title} approved.`)
        this.refresh()
      },
      error: (err) => this.notification.error('Error', err?.error?.error ?? 'Approval failed.'),
    })
  }

  run(op: IOperation): void {
    if (!this.beginOperationAction(op)) return

    this.service.run(op.id).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.endOperationAction(op))
    ).subscribe({
      next: (res) => {
        if (res.verified) {
          this.notification.success('Verified', `${op.title} executed and verified.`)
        } else {
          this.notification.warning('Not verified', `${op.title} did not pass verification.`)
        }
        this.refresh()
      },
      error: (err) =>
        this.notification.warning('Cannot run', err?.error?.error ?? 'This operation cannot be executed.'),
    })
  }

  canApprove(op: IOperation): boolean {
    return op.status === 'awaiting_approval'
  }

  isOperationActionPending(op: IOperation): boolean {
    return this.pendingOperationActionIds.has(op.id)
  }

  private beginOperationAction(op: IOperation): boolean {
    if (this.isOperationActionPending(op)) return false
    this.pendingOperationActionIds.add(op.id)
    return true
  }

  private endOperationAction(op: IOperation): void {
    this.pendingOperationActionIds.delete(op.id)
  }

  canRun(op: IOperation): boolean {
    return op.currentDecision === 'run_safe_local_worker' && ['classified', 'ready', 'failed'].includes(op.status)
  }

  riskColor(risk: string): string {
    switch (risk) {
      case 'high':
        return 'red'
      case 'medium':
        return 'gold'
      default:
        return 'green'
    }
  }

  statusColor(status: string): string {
    switch (status) {
      case 'completed':
        return 'green'
      case 'awaiting_approval':
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

  goBack(): void {
    this.router.navigate(['/control-center'])
  }

  openSources(): void {
    this.router.navigate(['/connected-sources'])
  }
}

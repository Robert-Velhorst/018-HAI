import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { forkJoin } from 'rxjs'
import { finalize, timeout } from 'rxjs/operators'
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
  lastRunNotice = ''

  loading = false
  running = false
  statusFilter = ''

  selected?: IOperation
  selectedEvents: IOperationEvent[] = []
  detailVisible = false

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
    this.loading = true
    forkJoin({
      dashboard: this.service.dashboard(),
      operations: this.service.list(this.statusFilter ? { status: this.statusFilter } : undefined),
      feeds: this.service.feeds(),
	}).pipe(
	  timeout(this.loadTimeoutMs),
	  finalize(() => (this.loading = false))
	).subscribe({
      next: ({ dashboard, operations, feeds }) => {
        this.dashboard = dashboard
        this.operations = operations.operations ?? []
        this.feeds = feeds.feeds ?? []
      },
      error: () => {
        this.notification.error('Error', 'Failed to load background operations.')
      },
    })
  }

  setFilter(value: string): void {
    this.statusFilter = value
    this.refresh()
  }

  runBackground(): void {
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
    const detail = response?.error?.error
    if (typeof detail === 'string' && detail.trim()) return detail
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
	this.service.approve(op.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => {
        this.notification.success('Approved', `${op.title} approved.`)
        this.refresh()
      },
      error: (err) => this.notification.error('Error', err?.error?.error ?? 'Approval failed.'),
    })
  }

  run(op: IOperation): void {
	this.service.run(op.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
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

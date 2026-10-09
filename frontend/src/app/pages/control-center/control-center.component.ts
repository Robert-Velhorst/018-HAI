import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Inject, OnDestroy, OnInit, ViewChild, ViewEncapsulation } from '@angular/core'
import { Router } from '@angular/router'
import { forkJoin, of, Subscription } from 'rxjs'
import { catchError, finalize, timeout } from 'rxjs/operators'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IAgentCycleRunResult } from '../../models/agent-cycle.model.interface'
import {
  IAgentRuntimeHealth,
  IAgentRuntimeInfo,
  IAgentRuntimeEcosystemSurface,
} from '../../models/agent-runtime.model.interface'
import {
  IAutomationDiagnostics,
  IAutomationHealthSummary,
  IAutomationLaunchEvent,
  IAutomationModel,
} from '../../models/automation.model.interface'
import {
  IAmbientNeed,
  IAmbientOverview,
  IAmbientScan,
} from '../../models/ambient.model.interface'
import { IContextMemory } from '../../models/context-memory.model.interface'
import {
  IWorkflowDashboard,
  IWorkflowItem,
} from '../../models/workflow.model.interface'
import { IPursuitDashboard } from '../../models/pursuit.model.interface'
import { AgentCycleService } from '../../services/agent-cycle.service'
import { AgentRuntimeService } from '../../services/agent-runtime.service'
import { AmbientService } from '../../services/ambient.service'
import { AUTOMATIONS_SERVICE_TOKEN } from '../../services/automations/automations.service.token'
import { IAutomationsService } from '../../services/automations.service.interface'
import { ContextMemoryService } from '../../services/context-memory/context-memory.service'
import { PursuitService } from '../../services/pursuit.service'
import { WorkflowService } from '../../services/workflow/workflow.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy'
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component'
import { isConfirmedLaunchResult, launchRecoveryNotice } from '../../control-room/launch-recovery'
import { safeWebSourceHref } from '../../control-room/source-navigation'

interface ActivityEntry {
  title: string
  detail: string
  status: string
  source: string
  timestamp: string
  needsAction: boolean
}

interface CommandAction {
  id: string
  title: string
  detail: string
  icon: string
  tone: 'blue' | 'green' | 'gold' | 'red'
  primaryMetric: string
  secondaryMetric: string
  context: string
  route?: string
  section?: ControlCenterSection
  execute?: () => void
}

interface NextActionSummary {
  kind: 'approval' | 'blocked' | 'follow-up' | 'active' | 'pursuit' | 'loading' | 'brief' | 'queue-unavailable' | 'cycle-issue'
  title: string
  detail: string
  source: string
  actionLabel: string
  workflowId?: string
  pursuitId?: string
}

type ControlCenterSection =
  | 'overview'
  | 'attention'
  | 'blocked'
  | 'priorities'
  | 'activity'
  | 'memory'
  | 'diagnostics'

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-control-center',
    templateUrl: './control-center.component.html',
    styleUrls: ['./control-center.component.scss'],
    // The stylesheet contains generic layout class names used by other
    // operational modules. Keep Angular's component boundary so visiting the
    // Control Center cannot restyle a later route.
    encapsulation: ViewEncapsulation.Emulated,
    standalone: false
})
export class ControlCenterComponent implements OnInit, OnDestroy {
  @ViewChild('diagnosticsSection') private diagnosticsSection?: HaiProgressiveSectionComponent
  readonly moduleId = 'control-center'
  readonly currentHour = new Date().getHours()
  automations: IAutomationModel[] = []
  summary?: IAutomationHealthSummary
  runtimes: IAgentRuntimeInfo[] = []
  runtimeHealth: IAgentRuntimeHealth[] = []
  workflowDashboard?: IWorkflowDashboard
  pursuitDashboard?: IPursuitDashboard
  ambientOverview?: IAmbientOverview
  memories: IContextMemory[] = []
  attentionItemsView: IWorkflowItem[] = []
  activeItemsView: IWorkflowItem[] = []
  blockedItemsView: IWorkflowItem[] = []
  lifePrioritiesView: IAmbientNeed[] = []
  recentActivityView: ActivityEntry[] = []
  recentMemoriesView: IContextMemory[] = []
  commandActionsView: CommandAction[] = []
  lastAgentCycle?: IAgentCycleRunResult
  lastAgentCycleIssue = ''
  dashboardLoadErrors: string[] = []
  dashboardAuthenticationRequired = false
  dashboardCheckedAt: Date | null = null
  diagnosticsLoadErrors: string[] = []

  loading = false
  scanning = false
  memoryLoading = false
  memoriesLoaded = false
  memoryLoadError = false
  diagnosticsListLoading = false
  diagnosticsLoaded = false
  resolvingId = ''
  resolvingAction: 'approve' | 'reject' | '' = ''
  private archivingMemoryIds = new Set<string>()
  diagnosticsExpanded = false
  private readonly operationTimeoutMs = 30000
  private dashboardRefreshSubscription?: Subscription
  private diagnosticsRequestSubscription?: Subscription
  private memoryRequestSubscription?: Subscription
  private diagnosticsDataSubscription?: Subscription
  private checkingIds = new Set<string>()
  private launchingIds = new Set<string>()

  isDiagnosticsVisible = false
  diagnostics?: IAutomationDiagnostics
  diagnosticsLoading = false
  diagnosticsName = ''
  selectedAction?: CommandAction
  activeSection: ControlCenterSection = 'overview'
  constructor(
    @Inject(AUTOMATIONS_SERVICE_TOKEN)
    private automationsService: IAutomationsService,
    private workflowService: WorkflowService,
    private pursuitService: PursuitService,
    private agentCycleService: AgentCycleService,
    private agentRuntimeService: AgentRuntimeService,
    private ambientService: AmbientService,
    private memoryService: ContextMemoryService,
    private notification: NzNotificationService,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService,
    private cdr: ChangeDetectorRef
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced'
  }

  ngOnInit(): void {
    this.refresh()
  }

  ngOnDestroy(): void {
    this.dashboardRefreshSubscription?.unsubscribe()
    this.cancelDiagnosticsRequest()
    this.memoryRequestSubscription?.unsubscribe()
    this.diagnosticsDataSubscription?.unsubscribe()
  }

  refresh(): void {
    this.dashboardRefreshSubscription?.unsubscribe()
    this.loading = true
    this.dashboardLoadErrors = []
    this.dashboardAuthenticationRequired = false
    this.dashboardRefreshSubscription = forkJoin({
      workflow: this.workflowService.dashboard().pipe(
        timeout(HttpTimeoutPolicy.readMs),
        catchError((error) => {
          this.recordDashboardLoadFailure('Workflow status', error)
          return of(undefined)
        })
      ),
      ambient: this.ambientService.overview().pipe(
        timeout(HttpTimeoutPolicy.readMs),
        catchError((error) => {
          this.recordDashboardLoadFailure('Ambient scan', error)
          return of(undefined)
        })
      ),
      pursuits: this.pursuitService.dashboard().pipe(
        timeout(HttpTimeoutPolicy.readMs),
        catchError((error) => {
          this.recordDashboardLoadFailure('Pursuit status', error)
          return of(undefined)
        })
      ),
    }).subscribe({
      next: ({ workflow, ambient, pursuits }) => {
        this.workflowDashboard = workflow
        this.ambientOverview = ambient
        this.pursuitDashboard = pursuits
        this.rebuildViewModel()
        this.loading = false
        if (!this.dashboardLoadErrors.length && this.workflowDataAvailable() &&
            this.hasCompleteQueueCounts() && this.ambientDataAvailable() && this.pursuitDataAvailable()) {
          this.dashboardCheckedAt = new Date()
        }
        this.cdr.markForCheck()
      },
      error: () => {
        this.loading = false
        this.cdr.markForCheck()
      },
    })
  }

  hasDashboardLoadError(): boolean {
    return this.dashboardLoadErrors.length > 0 ||
      (this.workflowDashboard !== undefined && !this.hasCompleteQueueCounts()) ||
      (this.ambientOverview !== undefined && !this.ambientDataAvailable()) ||
      (this.pursuitDashboard !== undefined && !this.pursuitDataAvailable())
  }

  dashboardLoadErrorMessage(): string {
    if (this.dashboardAuthenticationRequired) {
      return 'Your session is not authorized to load current dashboard data. Sign in again; HAI will not treat the queues as clear until the data is available.'
    }
    const errors = [...this.dashboardLoadErrors]
    if (this.workflowDashboard !== undefined && !this.hasCompleteQueueCounts()) errors.push('Workflow queue counts')
    if (this.ambientOverview !== undefined && !this.ambientDataAvailable()) errors.push('Ambient scan data')
    if (this.pursuitDashboard !== undefined && !this.pursuitDataAvailable()) errors.push('Pursuit status data')
    return `${errors.join(', ')} could not be loaded or validated. HAI cannot confirm that there is no work waiting.`
  }

  agentCycleIssueTitle(): string {
    return this.lastAgentCycle?.status === 'partial_failure'
      ? 'The operating brief completed only partially'
      : 'The operating brief did not complete'
  }

  workflowSummaryVisible(): boolean {
    if (this.isAdvancedView) {
      return this.workflowDataAvailable() || this.loading || this.hasDashboardLoadError()
    }
    return this.workflowDataAvailable() &&
      (this.workflowSignalVisible('approvals') ||
        this.workflowSignalVisible('blocked') ||
        this.workflowSignalVisible('dueOpenLoops'))
  }

  workflowSignalVisible(key: 'approvals' | 'blocked' | 'dueOpenLoops'): boolean {
    const count = this.workflowDashboard?.counts?.[key]
    if (typeof count === 'number' && Number.isFinite(count) && count > 0) return true
    if (key === 'approvals') return (this.workflowDashboard?.approvalItems?.length || 0) > 0
    if (key === 'blocked') return (this.workflowDashboard?.blockedItems?.length || 0) > 0
    return (this.workflowDashboard?.dueOpenLoops?.length || 0) > 0
  }

  private recordDashboardLoadFailure(label: string, error?: unknown): void {
    if (!this.dashboardLoadErrors.includes(label)) {
      this.dashboardLoadErrors = [...this.dashboardLoadErrors, label]
    }
    if (this.isUnauthorized(error)) this.dashboardAuthenticationRequired = true
  }

  private isUnauthorized(error: unknown): boolean {
    return !!error && typeof error === 'object' && 'status' in error && error.status === 401
  }

  dashboardRecoveryActionLabel(): string {
    return this.dashboardAuthenticationRequired ? 'Sign in' : 'Retry'
  }

  recoverDashboard(): void {
    if (this.dashboardAuthenticationRequired) {
      void this.router.navigate(['/login'], { queryParams: { returnUrl: '/control-center' } })
      return
    }
    this.refresh()
  }

  loadMemories(force = false): void {
    if ((this.memoriesLoaded && !force) || this.memoryLoading) {
      return
    }
    this.memoryLoading = true
    this.memoryLoadError = false
    this.memoryRequestSubscription = this.memoryService.list(undefined, false, 20).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (memories) => {
        this.memories = memories
        this.memoriesLoaded = true
        this.memoryLoading = false
        this.memoryLoadError = false
        this.rebuildViewModel()
        this.cdr.markForCheck()
      },
      error: () => {
        this.memoryLoading = false
        this.memoryLoadError = true
        this.cdr.markForCheck()
        this.notification.error(
          'Memory unavailable',
          'Recent memory updates could not be loaded.'
        )
      },
    })
  }

  loadDiagnosticsData(force = false): void {
    if ((this.diagnosticsLoaded && !force) || this.diagnosticsListLoading) {
      return
    }
    this.diagnosticsListLoading = true
    this.diagnosticsLoadErrors = []
    this.diagnosticsDataSubscription = forkJoin({
      automations: this.automationsService.getAutomations().pipe(
        timeout(this.operationTimeoutMs),
        catchError(() => {
          this.recordDiagnosticsLoadFailure('Automation registry')
          return of([] as IAutomationModel[])
        })
      ),
      summary: this.automationsService.getHealthSummary().pipe(
        timeout(this.operationTimeoutMs),
        catchError(() => {
          this.recordDiagnosticsLoadFailure('Automation health')
          return of(undefined)
        })
      ),
      runtimes: this.agentRuntimeService.overview().pipe(
        timeout(2500),
        catchError(() => {
          this.recordDiagnosticsLoadFailure('Runtime overview')
          return of({ runtimes: [] as IAgentRuntimeInfo[], health: [] as IAgentRuntimeHealth[] })
        })
      ),
    }).subscribe({
      next: (result) => {
        this.automations = result.automations.sort(
          (a, b) => a.position - b.position
        )
        this.summary = result.summary
        this.runtimes = result.runtimes.runtimes
        this.runtimeHealth = result.runtimes.health
        this.diagnosticsLoaded = true
        this.diagnosticsListLoading = false
        this.rebuildViewModel()
        this.cdr.markForCheck()
      },
      error: () => {
        this.diagnosticsListLoading = false
        this.notification.error(
          'Diagnostics unavailable',
          'Automation health controls could not be loaded.'
        )
      },
    })
  }

  runScan(): void {
    if (this.scanning) {
      return
    }
    this.scanning = true
    this.agentCycleService.run({ trigger: 'command-center', limit: 5 }).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => (this.scanning = false))
    ).subscribe({
      next: (result) => {
        this.lastAgentCycle = result
        this.lastAgentCycleIssue = result.status === 'completed'
          ? ''
          : result.safetySummary?.trim() ||
            (result.status === 'partial_failure'
              ? 'One or more operating-brief steps did not complete.'
              : 'The operating-brief result was not confirmed as complete.')
        if (result.dashboard) {
          this.workflowDashboard = result.dashboard
        }
        if (result.ambientScan && this.ambientOverview) {
          this.ambientOverview = {
            ...this.ambientOverview,
            scans: [result.ambientScan, ...(this.ambientOverview.scans || [])].slice(0, 10),
          }
        }
        this.rebuildViewModel()
        if (result.status === 'completed') {
          this.notification.success(result.executionScope === 'owner_scoped' ? 'Personal operating refresh completed' : 'Agent cycle completed', this.agentCycleSummary(result))
        } else if (result.status === 'partial_failure') {
          this.notification.warning('Agent cycle partially completed', this.agentCycleSummary(result))
        } else {
          this.notification.error('Agent cycle failed', this.agentCycleSummary(result))
        }
        this.refresh()
      },
      error: (error) => {
        this.lastAgentCycleIssue = 'The operating-brief request failed. HAI has not confirmed that the refresh completed.'
        this.notification.error(
          'Agent cycle failed',
          error?.error?.error || 'The operational cycle could not complete.'
        )
      },
    })
  }

  resolveApproval(item: IWorkflowItem, approved: boolean): void {
    if (this.isApprovalActionDisabled(item)) {
      return
    }
    const action = approved ? 'approve' : 'reject'
    this.resolvingId = item.id
    this.resolvingAction = action
    this.workflowService
      .resolveApproval(item.id, {
        approved,
        note: approved
          ? 'Approved from the household operations dashboard.'
          : 'Rejected from the household operations dashboard.',
        actor: 'operator',
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: () => {
          this.clearResolvingAction()
          this.notification.success(
            approved ? 'Approved' : 'Rejected',
            approved
              ? 'The AI may continue within the recorded safety limits.'
              : 'The task has been blocked and the decision was logged.'
          )
          this.refresh()
        },
        error: () => {
          this.clearResolvingAction()
          this.notification.warning(
            'Decision outcome unconfirmed',
            'HAI could not confirm whether the decision was saved. Refreshing the queue to reconcile its current state before another decision.'
          )
          this.refresh()
        },
      })
  }

  isResolving(item: IWorkflowItem, action: 'approve' | 'reject'): boolean {
    return this.resolvingId === item.id && this.resolvingAction === action
  }

  isApprovalActionDisabled(item?: IWorkflowItem): boolean {
    if (
      this.loading ||
      !!this.resolvingId ||
      this.dashboardAuthenticationRequired ||
      this.dashboardLoadErrors.includes('Workflow status') ||
      !this.workflowDataAvailable() ||
      !this.hasCompleteQueueCounts()
    ) {
      return true
    }

    return item
      ? !this.workflowDashboard?.approvalItems?.some((approval) => approval.id === item.id)
      : false
  }

  private clearResolvingAction(): void {
    this.resolvingId = ''
    this.resolvingAction = ''
  }

  archiveMemory(memory: IContextMemory): void {
    if (!memory.id) return
    const memoryId = memory.id
    if (this.archivingMemoryIds.has(memoryId)) return
    this.archivingMemoryIds.add(memoryId)
    this.memoryService.archive(memoryId).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => {
        this.archivingMemoryIds.delete(memoryId)
        this.memories = this.memories.filter((item) => item.id !== memoryId)
        this.rebuildViewModel()
        this.notification.success(
          'Memory archived',
          'The item will no longer be used as active context.'
        )
      },
      error: () => {
        this.archivingMemoryIds.delete(memoryId)
        this.notification.error(
          'Archive outcome unconfirmed',
          'Reload memory updates to check whether the item was archived before trying again.'
        )
      },
    })
  }

  isArchivingMemory(memory: IContextMemory): boolean {
    return !!memory.id && this.archivingMemoryIds.has(memory.id)
  }

  attentionItems(): IWorkflowItem[] {
    return this.isAdvancedView ? this.attentionItemsView : this.attentionItemsView.slice(0, 1)
  }

  activeItems(): IWorkflowItem[] {
    return this.activeItemsView
  }

  activeItemsForView(): IWorkflowItem[] {
    return this.isAdvancedView ? this.activeItemsView : this.activeItemsView.slice(0, 2)
  }

  blockedItems(): IWorkflowItem[] {
    return this.blockedItemsView
  }

  lifePriorities(): IAmbientNeed[] {
    return this.lifePrioritiesView
  }

  recentActivity(): ActivityEntry[] {
    return this.recentActivityView
  }

  recentMemories(): IContextMemory[] {
    return this.recentMemoriesView
  }

  commandActions(): CommandAction[] {
    return this.commandActionsView
  }

  commandActionDescriptionId(action: CommandAction): string {
    return `control-center-action-${action.id}-description`
  }

  commandActionDescription(action: CommandAction): string {
    return `${action.detail} Current metric: ${action.primaryMetric}. Additional context: ${action.secondaryMetric}. ${action.context}`
  }

  primaryCommandActions(): CommandAction[] {
    const primaryIds = ['scan', 'approvals', 'blocked']
    return primaryIds
      .map((id) => this.commandActionsView.find((action) => action.id === id))
      .filter((action): action is CommandAction => !!action)
  }

  secondaryCommandActions(): CommandAction[] {
    const primaryIds = new Set(this.primaryCommandActions().map((action) => action.id))
    return this.commandActionsView.filter((action) => !primaryIds.has(action.id))
  }

  hasLiveWork(): boolean {
    const counts = this.workflowDashboard?.counts
    return this.loading || this.hasDashboardLoadError() || this.attentionItemsView.length > 0 ||
      this.activeItemsView.length > 0 || this.blockedItemsView.length > 0 ||
      (this.workflowDashboard?.dueOpenLoops?.length || 0) > 0 ||
      Number(counts?.['approvals'] || 0) > 0 || Number(counts?.['ready'] || 0) > 0 ||
      Number(counts?.['blocked'] || 0) > 0 || Number(counts?.['dueOpenLoops'] || 0) > 0
  }

  workflowDataAvailable(): boolean {
    return !!this.workflowDashboard && !this.dashboardLoadErrors.includes('Workflow status')
  }

  workflowQueueHasNoApprovals(): boolean {
    return this.workflowDataAvailable() && this.hasCompleteQueueCounts() &&
      this.workflowDashboard?.counts?.['approvals'] === 0 &&
      this.isKnownEmptyList(this.workflowDashboard?.approvalItems)
  }

  workflowQueueHasNoActiveItems(): boolean {
    return this.workflowDataAvailable() && this.hasCompleteQueueCounts() &&
      this.workflowDashboard?.counts?.['ready'] === 0 &&
      this.isKnownEmptyList(this.workflowDashboard?.readyItems) &&
      this.activeItemsView.length === 0
  }

  workflowQueueHasNoBlockers(): boolean {
    return this.workflowDataAvailable() && this.hasCompleteQueueCounts() &&
      this.workflowDashboard?.counts?.['blocked'] === 0 &&
      this.workflowDashboard?.counts?.['dueOpenLoops'] === 0 &&
      this.isKnownEmptyList(this.workflowDashboard?.blockedItems) &&
      this.isKnownEmptyList(this.workflowDashboard?.dueOpenLoops)
  }

  ambientPrioritiesAvailable(): boolean {
    return !!this.ambientOverview && !this.dashboardLoadErrors.includes('Ambient scan') &&
      this.isKnownList(this.ambientOverview.needs)
  }

  activitySourcesAvailable(): boolean {
    return this.workflowDataAvailable() && this.ambientOverview !== undefined &&
      !this.dashboardLoadErrors.includes('Ambient scan') &&
      [
        this.workflowDashboard?.approvalItems,
        this.workflowDashboard?.blockedItems,
        this.workflowDashboard?.readyItems,
        this.workflowDashboard?.highRiskItems,
        this.workflowDashboard?.itemsWithoutNextAction,
        this.ambientOverview.scans,
      ].every((items) => this.isKnownList(items))
  }

  private isKnownList(value: unknown): boolean {
    return Array.isArray(value) || value === null
  }

  private isKnownEmptyList(value: unknown): boolean {
    return value === null || (Array.isArray(value) && value.length === 0)
  }

  dashboardSourceState(source: 'workflow' | 'ambient' | 'pursuits'): 'checking' | 'loaded' | 'unavailable' {
    if (this.loading) return 'checking'

    const sourceConfig = {
      workflow: {
        error: 'Workflow status',
        loaded: this.workflowDataAvailable() && this.hasCompleteQueueCounts(),
      },
      ambient: {
        error: 'Ambient scan',
        loaded: this.ambientDataAvailable(),
      },
      pursuits: {
        error: 'Pursuit status',
        loaded: this.pursuitDataAvailable(),
      },
    }[source]

    return this.dashboardLoadErrors.includes(sourceConfig.error) || !sourceConfig.loaded
      ? 'unavailable'
      : 'loaded'
  }

  workflowCount(key: 'approvals' | 'blocked' | 'dueOpenLoops'): string {
    if (!this.workflowDataAvailable()) {
      return this.loading && !this.workflowDashboard ? 'Checking' : 'Unavailable'
    }
    const value = this.workflowDashboard?.counts?.[key]
    return typeof value === 'number' && Number.isFinite(value) && value >= 0
      ? value.toLocaleString()
      : 'Unavailable'
  }

  private hasCompleteQueueCounts(): boolean {
    const counts = this.workflowDashboard?.counts
    return ['approvals', 'blocked', 'dueOpenLoops'].every((key) =>
      typeof counts?.[key] === 'number' && Number.isFinite(counts[key]) && counts[key] >= 0
    )
  }

  private ambientDataAvailable(): boolean {
    const overview = this.ambientOverview
    return !!overview && !!overview.policy &&
      Array.isArray(overview.needs) && Array.isArray(overview.opportunities) &&
      Array.isArray(overview.scans) && Array.isArray(overview.warnings)
  }

  private pursuitDataAvailable(): boolean {
    const dashboard = this.pursuitDashboard
    return !!dashboard && typeof dashboard.counts?.['active'] === 'number' &&
      Number.isFinite(dashboard.counts['active']) && dashboard.counts['active'] >= 0 &&
      Array.isArray(dashboard.decisionQueue) && Array.isArray(dashboard.needsRobert) &&
      Array.isArray(dashboard.blocked) && Array.isArray(dashboard.reviewDue) &&
      Array.isArray(dashboard.planningNeeded)
  }

  private nextPursuitAction(): NextActionSummary | undefined {
    const dashboard = this.pursuitDashboard
    if (!dashboard || !this.pursuitDataAvailable()) return undefined

    const decision = dashboard.decisionQueue.find((item) => item?.pursuit?.id)
    if (decision) {
      return {
        kind: 'pursuit',
        title: decision.pursuit.title || 'Review a pursuit decision',
        detail: decision.nextAction || decision.decision?.reason || 'A pursuit decision needs your review.',
        source: 'Pursuit decision queue',
        actionLabel: 'Open pursuit decision',
        pursuitId: decision.pursuit.id,
      }
    }

    const queues = [
      { items: dashboard.needsRobert, title: 'Review a pursuit decision', fallback: 'A pursuit needs your decision.', label: 'Open pursuit decision' },
      { items: dashboard.blocked, title: 'Unblock a pursuit', fallback: 'A pursuit is blocked and needs attention.', label: 'Inspect pursuit blocker' },
      { items: dashboard.reviewDue, title: 'Review a pursuit', fallback: 'A pursuit review is due.', label: 'Open pursuit review' },
      { items: dashboard.planningNeeded, title: 'Plan a pursuit', fallback: 'A pursuit needs a concrete next-step plan.', label: 'Open pursuit plan' },
    ]
    for (const queue of queues) {
      const item = queue.items.find((candidate) => candidate?.pursuit?.id)
      if (item) {
        return {
          kind: 'pursuit',
          title: item.pursuit.title || queue.title,
          detail: item.nextAction || item.whatChanged || queue.fallback,
          source: queue.title,
          actionLabel: queue.label,
          pursuitId: item.pursuit.id,
        }
      }
    }
    return undefined
  }

  nextAction(): NextActionSummary | undefined {
    if (this.loading) {
      return {
        kind: 'loading',
        title: 'Checking the workflow queue',
        detail: 'HAI is loading current work before recommending a next step.',
        source: 'Workflow engine',
        actionLabel: 'Checking',
      }
    }

    if (this.dashboardAuthenticationRequired) {
      return {
        kind: 'queue-unavailable',
        title: 'Sign-in required to verify current work',
        detail: 'The dashboard API rejected the current session. Sign in again before treating any queue or status as current.',
        source: 'Session authorization',
        actionLabel: 'Sign in again',
      }
    }

    if (this.workflowDataAvailable()) {
      const approval = this.attentionItemsView[0]
      if (approval) {
        return {
          kind: 'approval',
          title: approval.title,
          detail: this.itemReason(approval),
          source: approval.sourceLabel || approval.sourceType || 'Workflow queue',
          actionLabel: 'Open decision',
          workflowId: approval.id,
        }
      }

      if (!this.hasCompleteQueueCounts()) {
        return {
          kind: 'queue-unavailable',
          title: 'HAI cannot confirm the queue state',
          detail: 'Workflow data is missing required queue counts. Refresh the dashboard before treating the queue as clear.',
          source: 'Workflow engine',
          actionLabel: 'Refresh queue',
        }
      }

      if ((this.workflowDashboard?.counts?.['approvals'] || 0) > 0) {
        return {
          kind: 'approval',
          title: 'Review pending approvals',
          detail: `${this.workflowCount('approvals')} workflow decision(s) are waiting in the approval queue.`,
          source: 'Workflow engine',
          actionLabel: 'Open decision queue',
        }
      }

      const blocked = this.blockedItemsView[0]
      if (blocked) {
        return {
          kind: 'blocked',
          title: blocked.title,
          detail: blocked.blockedReason || blocked.lastWorkerError || 'This workflow needs information before it can continue.',
          source: blocked.sourceLabel || blocked.sourceType || 'Workflow queue',
          actionLabel: 'Inspect blocker',
          workflowId: blocked.id,
        }
      }
      if ((this.workflowDashboard?.counts?.['blocked'] || 0) > 0) {
        return {
          kind: 'blocked',
          title: 'Review blocked work',
          detail: `${this.workflowCount('blocked')} workflow(s) need input before they can continue.`,
          source: 'Workflow engine',
          actionLabel: 'Open blocked work',
        }
      }

      const loop = this.workflowDashboard?.dueOpenLoops?.[0]
      if (loop) {
        return {
          kind: 'follow-up',
          title: `Follow up: ${loop.waitingFor}`,
          detail: loop.nextAction,
          source: loop.responsibleParty || 'Follow-up owner not recorded',
          actionLabel: 'Review follow-up',
          workflowId: loop.workflowId,
        }
      }
      if ((this.workflowDashboard?.counts?.['dueOpenLoops'] || 0) > 0) {
        return {
          kind: 'follow-up',
          title: 'Review due follow-ups',
          detail: `${this.workflowCount('dueOpenLoops')} follow-up(s) are due for review.`,
          source: 'Workflow engine',
          actionLabel: 'Open follow-up queue',
        }
      }

      if (this.dashboardLoadErrors.includes('Pursuit status') ||
        (this.pursuitDashboard && !this.pursuitDataAvailable())) {
        return {
          kind: 'queue-unavailable',
          title: 'HAI cannot confirm pursuit status',
          detail: 'Pursuit data is missing required queue fields. Refresh before treating the overall action queue as clear.',
          source: 'Pursuit engine',
          actionLabel: 'Retry queue check',
        }
      }

      const pursuitAction = this.nextPursuitAction()
      if (pursuitAction) return pursuitAction

      const active = this.activeItemsView[0]
      if (active) {
        return {
          kind: 'active',
          title: active.title,
          detail: active.nextAction || active.description || 'Review the current workflow state.',
          source: active.sourceLabel || active.sourceType || 'Workflow queue',
          actionLabel: 'View workflow',
          workflowId: active.id,
        }
      }

      if (this.dashboardLoadErrors.includes('Ambient scan') ||
        (this.ambientOverview && !this.ambientDataAvailable())) {
        return {
          kind: 'queue-unavailable',
          title: 'HAI cannot confirm proactive signals',
          detail: 'Ambient data is missing required fields. Refresh before treating the current action brief as complete.',
          source: 'Ambient engine',
          actionLabel: 'Retry queue check',
        }
      }

      if (this.lastAgentCycleIssue) {
        return {
          kind: 'cycle-issue',
          title: 'Recheck your operating brief',
          detail: this.lastAgentCycleIssue,
          source: 'Agent cycle',
          actionLabel: 'Retry operating brief',
        }
      }

      return {
        kind: 'brief',
        title: 'Nothing is waiting on you',
        detail: 'No pending approvals, blocked workflows, or due follow-ups were returned by the current workflow queue.',
        source: 'Workflow engine',
        actionLabel: 'Refresh operating brief',
      }
    }

    return {
      kind: 'queue-unavailable',
      title: 'HAI cannot confirm the queue state',
      detail: 'Workflow status is unavailable. Retry the dashboard check before starting work or treating the queue as clear.',
      source: 'Workflow engine',
      actionLabel: 'Retry queue check',
    }
  }

  nextActionDetail(action: NextActionSummary): string {
    if (this.isAdvancedView || action.detail.length <= 240) return action.detail
    const firstCause = action.detail.split(/;|\r?\n/)[0].trim()
    const summary = firstCause.length > 180 ? `${firstCause.slice(0, 177)}...` : firstCause
    return `${summary}${/[.!?]$/.test(summary) ? '' : '.'} Open the workflow for the full context and recovery options.`
  }

  nextActionBusy(action: NextActionSummary): boolean {
    return action.kind === 'loading'
      || (action.kind === 'queue-unavailable' && this.loading)
      || (['brief', 'cycle-issue'].includes(action.kind) && this.scanning)
  }

  runNextAction(): void {
    const action = this.nextAction()
    if (!action) return

    switch (action.kind) {
      case 'approval':
      case 'blocked':
      case 'follow-up':
      case 'active':
        if (action.workflowId) this.openWorkflowId(action.workflowId)
        else this.openWorkflow()
        return
      case 'pursuit':
        if (action.pursuitId) {
          void this.router.navigate(['/pursuits'], { queryParams: { selected: action.pursuitId } })
        } else {
          void this.router.navigate(['/pursuits'])
        }
        return
      case 'brief':
        this.runScan()
        return
      case 'queue-unavailable':
        this.recoverDashboard()
        return
      case 'cycle-issue':
        this.runScan()
        return
      case 'loading':
        return
    }
  }

  hasDiagnosticsLoadError(): boolean {
    return this.diagnosticsLoadErrors.length > 0
  }

  private recordDiagnosticsLoadFailure(label: string): void {
    if (!this.diagnosticsLoadErrors.includes(label)) {
      this.diagnosticsLoadErrors = [...this.diagnosticsLoadErrors, label]
    }
  }

  private buildRecentActivity(): ActivityEntry[] {
    const cycleActivity = this.lastAgentCycle
      ? [
          {
            title: `Agent cycle ${this.readableState(this.lastAgentCycle.status)}`,
            detail: this.agentCycleSummary(this.lastAgentCycle),
            status: this.lastAgentCycle.status,
            source: this.lastAgentCycle.learningIds?.length ? 'Agent cycle / learning' : 'Agent cycle',
            timestamp: this.lastAgentCycle.completedAt || this.lastAgentCycle.startedAt,
            needsAction:
              this.lastAgentCycle.status !== 'completed' ||
              this.lastAgentCycle.nextAction !==
                'no immediate human action; continue scheduled monitoring',
          },
        ]
      : []
    const workflowActivity = this.dashboardItems().slice(0, 8).map((item) => ({
      title: this.activityTitle(item),
      detail: item.nextAction || item.blockedReason || item.description || '',
      status: item.currentState,
      source: item.sourceLabel || item.sourceType || 'Workflow',
      timestamp: item.updatedAt,
      needsAction:
        item.requiresApproval && item.approvalStatus !== 'approved',
    }))
    const scanActivity = (this.ambientOverview?.scans || [])
      .slice(0, 3)
      .map((scan) => ({
        title: `Proactive scan found ${scan.opportunitiesFound} possible actions`,
        detail: `${scan.itemsExamined} items reviewed; ${scan.deduplicated} duplicates avoided.`,
        status: scan.status,
        source: 'Connected sources',
        timestamp: scan.completedAt || scan.startedAt,
        needsAction: scan.blocked > 0,
      }))
    return [...cycleActivity, ...workflowActivity, ...scanActivity]
      .sort(
        (a, b) =>
          new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
      )
      .slice(0, 6)
  }

  reviewedItemCount(): number {
    return this.ambientOverview?.scans?.[0]?.itemsExamined || 0
  }

  completedCount(): number {
    return this.workflowDashboard?.counts?.['completed'] || 0
  }

  pursuitCount(): number {
    return Number(this.pursuitDashboard?.counts?.['active'] || 0)
  }

  pursuitAttentionCount(): number {
    const dashboard = this.pursuitDashboard
    if (!dashboard) return 0
    // The backend serialises empty lists as null (Go nil slices), so a brand-new
    // account with no pursuits sends null here — guard each list rather than
    // assuming an array.
    return (
      (dashboard.needsRobert?.length ?? 0) +
      (dashboard.blocked?.length ?? 0) +
      (dashboard.stale?.length ?? 0)
    )
  }

  private buildCommandActions(): CommandAction[] {
    const blocked = this.blockedItemsView.length + (this.workflowDashboard?.dueOpenLoops?.length || 0)
    return [
      {
        id: 'pursuits',
        title: 'Manage pursuits',
        detail: 'Open long-running goals and linked operational work.',
        icon: 'flag',
        tone: this.pursuitAttentionCount() ? 'gold' : 'blue',
        primaryMetric: `${this.pursuitCount()} active`,
        secondaryMetric: `${this.pursuitAttentionCount()} need attention`,
        context: 'Pursuits are the top-level goal layer. They connect workflows, approvals, evidence, sources, memory, and runtime work into one outcome-focused operating record.',
        route: '/pursuits',
      },
      {
        id: 'approvals',
        title: 'Review approvals',
        detail: 'Decide what HAI may execute next.',
        icon: 'check-square',
        tone: this.attentionItemsView.length ? 'red' : 'green',
        primaryMetric: `${this.attentionItemsView.length} waiting`,
        secondaryMetric: 'High-risk actions stay blocked',
        context: 'Legal, financial, public, account, destructive, and external-message actions require approval before execution.',
        section: 'attention',
      },
      {
        id: 'automation',
        title: 'Add automation',
        detail: 'Register a script, service, API, or workflow target.',
        icon: 'plus',
        tone: 'blue',
        primaryMetric: this.hasDiagnosticsLoadError()
          ? 'Unavailable'
          : this.diagnosticsLoaded
          ? `${this.automations.length} registered`
          : 'Open registry',
        secondaryMetric: this.hasDiagnosticsLoadError()
          ? 'Reload technical checks'
          : this.diagnosticsLoaded
          ? 'Automation health loaded'
          : 'Health detail loads on demand',
        context: 'Use this when HAI needs a new controlled runtime target, health check, launch path, dependency note, or automation entry.',
        route: '/home',
      },
      {
        id: 'scan',
        title: 'Refresh my operating brief',
        detail: 'Refresh your context, decisions, blockers, and next action.',
        icon: 'radar-chart',
        tone: 'blue',
        primaryMetric: this.lastAgentCycle ? this.readableState(this.lastAgentCycle.status) : `${this.reviewedItemCount()} reviewed`,
        secondaryMetric: this.agentCycleSecondaryMetric(),
        context: 'Uses your owner-scoped memory and pursuits to refresh decisions, blockers, and the next action. System sync, workflow execution, and ambient scanning run separately under the controlled worker.',
        execute: () => this.runScan(),
      },
      {
        id: 'blocked',
        title: 'Clear blockers',
        detail: 'Unstick work waiting for input.',
        icon: 'clock-circle',
        tone: blocked ? 'gold' : 'green',
        primaryMetric: `${blocked} blocked`,
        secondaryMetric: 'Waiting states stay visible',
        context: 'Blocked items need a missing answer, source, credential, document, approval, or external reply before they can continue.',
        section: 'blocked',
      },
      {
        id: 'sources',
        title: 'Review sources',
        detail: 'Inspect connected accounts and choose a source to refresh.',
        icon: 'cluster',
        tone: 'blue',
        primaryMetric: 'Local-first',
        secondaryMetric: 'Incremental context only',
        context: 'Source sync should fetch metadata first, avoid unnecessary cloud sharing, and keep extracted facts linked to provenance.',
        route: '/connected-sources',
      },
      {
        id: 'memory',
        title: 'Review memory',
        detail: 'Accept, correct, or archive learned context.',
        icon: 'database',
        tone: this.memoriesLoaded && this.memories.length ? 'gold' : 'blue',
        primaryMetric: this.memoriesLoaded ? `${this.recentMemories().length} recent` : 'Not loaded',
        secondaryMetric: 'Context is opt-in',
        context: 'Memory stays unloaded by default here. Load it only when you want to inspect what HAI may reuse later.',
        execute: () => this.navigateToSection('memory'),
      },
      {
        id: 'models',
        title: 'Check model routing',
        detail: 'Inspect budget, tiers, tokens, and fallbacks.',
        icon: 'deployment-unit',
        tone: 'green',
        primaryMetric: 'EUR 0 default',
        secondaryMetric: 'Paid usage gated',
        context: 'The router should pick the cheapest capable model, not the cheapest model blindly, and escalate only after validation fails.',
        route: '/llm-policy',
      },
      {
        id: 'runtime-safety',
        title: 'Runtime safety',
        detail: 'Inspect Hermes, Odysseus, and OpenClaw readiness gates.',
        icon: 'safety-certificate',
        tone: this.runtimeAttentionCount() ? 'red' : this.runtimes.length ? 'green' : 'gold',
        primaryMetric: this.runtimeSafetyMetric(),
        secondaryMetric: this.runtimeSafetySecondaryMetric(),
        context: 'External agent runtimes are powerful execution substrates. HAI keeps them disabled or blocked unless configuration, approval, workspace, host, timeout, output, and high-risk surface policies are satisfied.',
        execute: () => {
          this.navigateToSection('diagnostics')
          this.loadDiagnosticsData(true)
        },
      },
      {
        id: 'health',
        title: 'Review health status',
        detail: 'Load current automation and runtime health details.',
        icon: 'tool',
        tone: 'blue',
        primaryMetric: this.summary ? `${this.summary.healthy}/${this.summary.total} healthy` : 'On demand',
        secondaryMetric: 'Technical details hidden',
        context: 'Diagnostics are intentionally behind this action so day-to-day operations are not mixed with developer controls.',
        execute: () => {
          this.navigateToSection('diagnostics')
          this.loadDiagnosticsData(true)
        },
      },
    ]
  }

  autonomyMode(): string {
    const policy = this.ambientOverview?.policy
    if (!policy) return 'Safe mode'
    if (!policy.executionEnabled && policy.suggestionOnly) return 'Proposals only'
    if (!policy.executionEnabled) return 'Observe only'
    return 'Limited autonomy'
  }

  lastScanAt(): string | undefined {
    const scan = this.ambientOverview?.scans?.[0]
    return scan?.completedAt || scan?.startedAt
  }

  priorityLabel(need: IAmbientNeed): string {
    if (need.priorityWeight >= 0.8) return 'High priority'
    if (need.priorityWeight >= 0.5) return 'Medium priority'
    return 'Steady priority'
  }

  priorityTone(need: IAmbientNeed): string {
    const gap = need.targetLevel - need.currentLevel
    if (gap >= 30) return 'focus'
    if (gap >= 15) return 'watch'
    return 'good'
  }

  needAction(need: IAmbientNeed): string {
    if (need.notes?.trim()) return need.notes
    const actions: Record<string, string> = {
      safety: 'Resolve time-sensitive legal and household risks.',
      health: 'Protect energy by automating recurring administration.',
      household: 'Clear one blocked maintenance or document task.',
      finances: 'Review upcoming obligations and missing records.',
      work: 'Advance the highest-value unblocked income task.',
      relationships: 'Close one waiting conversation or commitment.',
      learning: 'Continue the next practical skill milestone.',
      freedom: 'Remove one recurring task from your manual workload.',
    }
    const key = need.key.toLowerCase()
    const match = Object.keys(actions).find((candidate) =>
      key.includes(candidate)
    )
    return match
      ? actions[match]
      : 'Review the highest-priority open task in this area.'
  }

  connectedTaskCount(need: IAmbientNeed): number {
    const terms = [need.key, need.name]
      .join(' ')
      .toLowerCase()
      .split(/\W+/)
      .filter((term) => term.length > 3)
    return this.dashboardItems().filter((item) => {
      const text = `${item.title} ${item.description || ''} ${
        item.projectKey || ''
      }`.toLowerCase()
      return terms.some((term) => text.includes(term))
    }).length
  }

  needIcon(need: IAmbientNeed): string {
    const key = `${need.key} ${need.name}`.toLowerCase()
    if (key.includes('health')) return 'heart'
    if (key.includes('house')) return 'home'
    if (key.includes('finance')) return 'wallet'
    if (key.includes('work') || key.includes('income')) return 'briefcase'
    if (key.includes('relationship')) return 'team'
    if (key.includes('learn')) return 'read'
    if (key.includes('freedom')) return 'rise'
    return 'safety-certificate'
  }

  statusColor(status?: string): string {
    switch ((status || 'unknown').toLowerCase()) {
      case 'healthy':
      case 'completed':
      case 'ready':
      case 'approved':
        return 'green'
      case 'warning':
      case 'blocked':
      case 'needs_approval':
      case 'waiting_external_input':
        return 'gold'
      case 'degraded':
      case 'in_progress':
        return 'blue'
      case 'broken':
      case 'failed':
      case 'rejected':
        return 'red'
      default:
        return ''
    }
  }

  riskColor(risk?: string): string {
    switch ((risk || '').toLowerCase()) {
      case 'high':
      case 'critical':
        return 'red'
      case 'medium':
        return 'orange'
      default:
        return 'green'
    }
  }

  readableState(state?: string): string {
    return (state || 'unknown').replace(/_/g, ' ')
  }

  itemReason(item: IWorkflowItem): string {
    return (
      item.approvalReason ||
      item.description ||
      item.blockedReason ||
      'The AI needs your decision before it can safely continue.'
    )
  }

  itemRecommendation(item: IWorkflowItem): string {
    return (
      item.nextAction ||
      'Review the source and approve only if the proposed action is correct.'
    )
  }

  expectedOutcome(item: IWorkflowItem): string {
    if (item.currentState === 'needs_approval') {
      return 'The workflow continues within its safety limits.'
    }
    return 'The next verified workflow step can proceed.'
  }

  openSource(uri?: string): void {
    if (!uri) return
    const href = safeWebSourceHref(uri)
    if (href) {
      window.open(href, '_blank', 'noopener,noreferrer')
      return
    }
    this.notification.warning('Source unavailable', 'This source does not have a valid web link without embedded credentials.')
  }

  navigate(route: string): void {
    if (route === '/control-center') {
      this.activeSection = 'overview'
      setTimeout(() => this.scrollToSection('overview'))
    }
    this.router.navigate([route])
  }

  navigateToSection(section: ControlCenterSection): void {
    this.activeSection = section
    this.openContainingDisclosure(section)
    if (section === 'diagnostics') this.diagnosticsSection?.setOpen(true)
    if (section === 'memory') {
      this.loadMemories()
    }
    setTimeout(() => this.scrollToSection(section))
  }

  private scrollToSection(section: string): void {
    document.getElementById(section)?.scrollIntoView({
      // Action controls should make their destination visible immediately. A
      // long smooth scroll made valid clicks appear to do nothing, especially
      // on the compact dashboard viewport.
      behavior: 'auto',
      block: 'start',
    })
  }

  private openContainingDisclosure(section: ControlCenterSection): void {
    const sectionId: Partial<Record<ControlCenterSection, string>> = {
      blocked: 'operational-controls',
      priorities: 'operational-controls',
      activity: 'context-audit',
      memory: 'context-audit',
      diagnostics: 'diagnostics',
    }
    const id = sectionId[section]
    if (!id) return
    const wrapper = document.getElementById(id)
    const trigger = wrapper?.querySelector<HTMLButtonElement>('.hai-progressive-section__summary')
    if (trigger?.getAttribute('aria-expanded') !== 'true') trigger?.click()
  }

  openAction(action: CommandAction): void {
    this.selectedAction = action
  }

  closeAction(): void {
    this.selectedAction = undefined
  }

  runSelectedAction(): void {
    if (!this.selectedAction) return
    const action = this.selectedAction
    this.closeAction()
    // NG-Zorro keeps the document scroll-locked until the inspector close
    // animation finishes. Deferring the target action lets section actions
    // land visibly instead of leaving users at the command cards.
    window.setTimeout(() => {
      this.continueSelectedAction(action)
      this.cdr.markForCheck()
    }, 360)
  }

  private continueSelectedAction(action: CommandAction): void {
    if (action.execute) {
      action.execute()
      return
    }
    if (action.route) {
      this.navigate(action.route)
      return
    }
    if (action.section) {
      this.navigateToSection(action.section)
    }
  }

  actionModeLabel(action: CommandAction): string {
    if (action.id === 'health') return 'Loads current diagnostics';
    if (action.execute) return 'Runs an operation here'
    if (action.route) return `Opens ${action.route.replace('/', '')}`
    if (action.section) return `Focuses ${action.section.replace(/_/g, ' ')}`
    return 'Shows details'
  }

  actionSafetyLabel(action: CommandAction): string {
    if (action.id === 'approvals') return 'Human approval gate'
    if (action.id === 'models') return 'EUR 0 policy guarded'
    if (action.id === 'runtime-safety') return 'Runtime execution gated'
    if (action.id === 'health') return 'Developer controls separated'
    if (action.id === 'scan') return 'Read-only scan first'
    return 'Uses existing HAI policy gates'
  }

  runtimeAttentionCount(): number {
    return this.runtimes.filter((runtime) => this.runtimeNeedsAttention(runtime)).length
  }

  runtimeSafetyMetric(): string {
    if (!this.runtimes.length) return 'Not loaded'
    const executable = this.runtimes.filter((runtime) => runtime.executionEnabled).length
    return `${executable}/${this.runtimes.length} executable`
  }

  runtimeSafetySecondaryMetric(): string {
    if (!this.runtimes.length) return 'Open diagnostics'
    const highRisk = this.runtimeHighRiskSurfaceCount()
    const blocked = this.runtimeAttentionCount()
    if (highRisk) return `${highRisk} high-risk surfaces`
    if (blocked) return `${blocked} need configuration`
    return 'All visible runtimes gated'
  }

  runtimeHighRiskSurfaceCount(): number {
    return this.runtimes.reduce(
      (total, runtime) =>
        total +
        (runtime.ecosystem || []).filter(
          (surface) =>
            (surface.riskLevel || '').toLowerCase() === 'high' &&
            surface.count > 0
        ).length,
      0
    )
  }

  runtimeNeedsAttention(runtime: IAgentRuntimeInfo): boolean {
    if (!runtime.enabled || !runtime.configured || !runtime.executionEnabled) {
      return true
    }
    return (runtime.ecosystem || []).some(
      (surface) =>
        surface.approvalRequired &&
        (surface.riskLevel || '').toLowerCase() === 'high' &&
        surface.count > 0
    )
  }

  runtimeHealthFor(runtime: IAgentRuntimeInfo): IAgentRuntimeHealth | undefined {
    return this.runtimeHealth.find((health) => health.runtimeId === runtime.id)
  }

  runtimeStatus(runtime: IAgentRuntimeInfo): string {
    const health = this.runtimeHealthFor(runtime)
    if (health?.status) return health.status
    if (!runtime.enabled) return 'disabled'
    if (!runtime.configured) return 'blocked'
    if (!runtime.executionEnabled) return 'blocked'
    return 'ready'
  }

  runtimePolicyReason(runtime: IAgentRuntimeInfo): string {
    const health = this.runtimeHealthFor(runtime)
    if (health?.reason) return health.reason
    if (runtime.missingConfiguration?.length) {
      return runtime.missingConfiguration.join('; ')
    }
    if (!runtime.enabled) return 'Runtime is disabled by configuration.'
    return 'Runtime policy checks are satisfied; task execution still requires approval.'
  }

  runtimeRiskSurfaces(runtime: IAgentRuntimeInfo): IAgentRuntimeEcosystemSurface[] {
    return (runtime.ecosystem || [])
      .filter((surface) => surface.approvalRequired || surface.riskLevel)
      .sort((a, b) => this.riskWeight(b.riskLevel) - this.riskWeight(a.riskLevel))
      .slice(0, 6)
  }

  runtimeRiskColor(risk?: string): string {
    switch ((risk || '').toLowerCase()) {
      case 'high':
        return 'red'
      case 'medium':
      case 'review':
        return 'orange'
      case 'low':
        return 'green'
      default:
        return 'default'
    }
  }

  private riskWeight(risk?: string): number {
    switch ((risk || '').toLowerCase()) {
      case 'high':
        return 3
      case 'medium':
      case 'review':
        return 2
      case 'low':
        return 1
      default:
        return 0
    }
  }

  controlTitle(label: string, detail: string): string {
    return `${label}: ${detail}`
  }

  openWorkflow(item?: IWorkflowItem): void {
    this.router.navigate(['/workflow-engine'], {
      queryParams: item ? { workflowId: item.id } : undefined,
    })
  }

  openWorkflowId(workflowId: string): void {
    this.router.navigate(['/workflow-engine'], { queryParams: { workflowId } })
  }

  openMemory(): void {
    this.router.navigate(['/memory'])
  }

  toggleDiagnostics(): void {
    const shouldOpen = !this.diagnosticsExpanded
    this.diagnosticsSection?.setOpen(shouldOpen)
    if (shouldOpen) {
      this.activeSection = 'diagnostics'
      this.loadDiagnosticsData()
      setTimeout(() => this.scrollToSection('diagnostics-content'))
    } else if (this.activeSection === 'diagnostics') {
      this.activeSection = 'overview'
    }
  }

  onDiagnosticsOpenChange(open: boolean): void {
    this.diagnosticsExpanded = open
    if (open) this.loadDiagnosticsData()
  }

  runHealthCheck(automation: IAutomationModel): void {
    if (!automation.id) return
    const id = automation.id
    if (this.checkingIds.has(id)) return
    this.checkingIds.add(id)
    this.automationsService.runHealthCheck(id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.checkingIds.delete(id)
        automation.status = result.status
        automation.lastCheckedAt = result.checkedAt
        automation.averageLatencyMs = result.latencyMs
        automation.consecutiveFailures = result.consecutiveFailures
        if (result.status === 'healthy') {
          automation.lastSuccessAt = result.checkedAt
          automation.lastFailureReason = ''
        } else if (result.failureReason) {
          automation.lastFailureAt = result.checkedAt
          automation.lastFailureReason = result.failureReason
        }
        if (result.status === 'healthy') {
          this.notification.success('Check completed', `${automation.name} is healthy.`)
        } else {
          this.notification.warning(
            'Check completed with an issue',
            `${automation.name} is ${result.status}${result.failureReason ? `: ${result.failureReason}` : '.'}`
          )
        }
      },
      error: () => {
        this.checkingIds.delete(id)
        this.notification.error(
          'Check failed',
          `${automation.name} could not be checked.`
        )
      },
    })
  }

  isChecking(automation: IAutomationModel): boolean {
    return !!automation.id && this.checkingIds.has(automation.id)
  }

  launch(automation: IAutomationModel): void {
    if (!automation.id) return
    const id = automation.id
    if (this.launchingIds.has(id)) return
    this.launchingIds.add(id)
    this.automationsService.launchAutomation(id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.launchingIds.delete(id)
        if (isConfirmedLaunchResult(result, id)) {
          automation.lastLaunchAt = result.launchedAt
          this.notification.success(result.status === 'ready' ? 'Automation is ready' : 'Launch completed', result.message || automation.name)
        } else if (result?.status === 'indeterminate') {
          this.notification.warning(
            'Launch outcome is still being checked',
            `${result.message || automation.name} Retrying reuses the same request; do not create a separate launch.`
          )
        } else {
          this.notification.warning(
            'Launch completion is not verified',
            'The existing request key was retained. Open diagnostics before starting another attempt.'
          )
        }
      },
      error: (error) => {
        this.launchingIds.delete(id)
        const recoveryNotice = launchRecoveryNotice(error, id)
        if (recoveryNotice) {
          this.notification.warning('Launch requires reconciliation', recoveryNotice)
          return
        }
        this.notification.error(
          error?.name === 'AutomationLaunchSafetyError' ? 'Launch was not sent' : 'Launch failed',
          error?.name === 'AutomationLaunchSafetyError'
            ? error.message
            : `${automation.name} could not be started. If the result is uncertain, retrying reuses the same launch request.`
        )
      },
    })
  }

  isLaunching(automation: IAutomationModel): boolean {
    return !!automation.id && this.launchingIds.has(automation.id)
  }

  openDiagnostics(automation: IAutomationModel): void {
    if (!automation.id) return
    this.cancelDiagnosticsRequest()
    this.isDiagnosticsVisible = true
    this.diagnosticsLoading = true
    this.diagnostics = undefined
    this.diagnosticsName = automation.name
    this.diagnosticsRequestSubscription = this.automationsService.getDiagnostics(automation.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (diagnostics) => {
        this.diagnostics = diagnostics
        this.diagnosticsLoading = false
        this.cdr.markForCheck()
      },
      error: () => {
        this.diagnosticsLoading = false
        this.cdr.markForCheck()
        this.notification.error(
          'Diagnostics unavailable',
          'The detailed automation record could not be loaded.'
        )
      },
    })
  }

  private cancelDiagnosticsRequest(): void {
    this.diagnosticsRequestSubscription?.unsubscribe()
    this.diagnosticsRequestSubscription = undefined
  }

  closeDiagnostics(): void {
    this.cancelDiagnosticsRequest()
    this.isDiagnosticsVisible = false
    this.diagnosticsLoading = false
    this.diagnostics = undefined
  }

  diagnosticsCheckKeys(): string[] {
    return this.diagnostics ? Object.keys(this.diagnostics.checks || {}) : []
  }

  isHostRuntimeReviewEvent(event: IAutomationLaunchEvent): boolean {
    return event.launchType === 'agent_runtime_host_review' &&
      (event.status === 'expired' || event.status === 'needs_review')
  }

  launchEventLabel(event: IAutomationLaunchEvent): string {
    return this.isHostRuntimeReviewEvent(event)
      ? `Operator review required: ${event.status}`
      : event.status
  }

  launchEventSummary(event: IAutomationLaunchEvent): string {
    if (this.isHostRuntimeReviewEvent(event)) {
      return event.message || 'The host execution outcome is unknown. Review before deciding what happens next.'
    }
    return event.message || event.output || 'No execution summary.'
  }

  trackById(_index: number, item: { id?: string }): string | undefined {
    return item.id
  }

  trackByActivity(index: number, item: ActivityEntry): string {
    return `${item.timestamp}-${item.title}-${index}`
  }

  private dashboardItems(): IWorkflowItem[] {
    const items = [
      ...(this.workflowDashboard?.approvalItems || []),
      ...(this.workflowDashboard?.blockedItems || []),
      ...(this.workflowDashboard?.readyItems || []),
      ...(this.workflowDashboard?.highRiskItems || []),
      ...(this.workflowDashboard?.itemsWithoutNextAction || []),
    ]
    return Array.from(new Map(items.map((item) => [item.id, item])).values())
      .sort(
        (a, b) =>
          new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime()
      )
  }

  private activityTitle(item: IWorkflowItem): string {
    const labels: Record<string, string> = {
      completed: `Completed: ${item.title}`,
      blocked: `Blocked: ${item.title}`,
      in_progress: `Working on: ${item.title}`,
      needs_approval: `Decision needed: ${item.title}`,
      waiting_external_input: `Waiting: ${item.title}`,
      ready: `Ready to continue: ${item.title}`,
    }
    return labels[item.currentState] || `Updated: ${item.title}`
  }

  private agentCycleSummary(result: IAgentCycleRunResult): string {
    const completed = result.steps.filter((step) => step.status === 'completed').length
    const failed = result.steps.filter((step) => step.status === 'failed').length
    const skipped = result.steps.filter((step) => step.status === 'skipped').length
    const reviewed = result.ambientScan
      ? ` ${result.ambientScan.opportunitiesFound} opportunities from ${result.ambientScan.itemsExamined} reviewed items.`
      : ''
    const context = result.appliedContext?.length
      ? ` Used ${result.appliedContext.length} prior operational lesson${result.appliedContext.length === 1 ? '' : 's'}.`
      : ''
    const learning = result.learningIds?.length
      ? ` Learned ${result.learningIds.length} operational lesson${result.learningIds.length === 1 ? '' : 's'}.`
      : ''
    const pursuitState = result.pursuitOperatingState
      ? ` Pursuits: ${result.pursuitOperatingState.primaryLane}, ${result.pursuitOperatingState.attentionTotal} need attention.`
      : ''
    return `${completed} steps completed, ${failed} failed, ${skipped} skipped. Next: ${result.nextAction}.${reviewed}${pursuitState}${context}${learning}`
  }

  private agentCycleSecondaryMetric(): string {
    if (this.lastAgentCycle?.pursuitOperatingState?.attentionTotal) {
      const state = this.lastAgentCycle.pursuitOperatingState
      return `${state.attentionTotal} pursuit attention / ${state.primaryLane}`
    }
    if (this.lastAgentCycle?.learningIds?.length) {
      return `${this.lastAgentCycle.learningIds.length} lesson${this.lastAgentCycle.learningIds.length === 1 ? '' : 's'} stored`
    }
    if (this.lastAgentCycle?.appliedContext?.length) {
      return `${this.lastAgentCycle.appliedContext.length} lesson${this.lastAgentCycle.appliedContext.length === 1 ? '' : 's'} applied`
    }
    if (this.lastAgentCycle?.learningNote) {
      return this.lastAgentCycle.learningNote
    }
    if (this.lastAgentCycle?.nextAction) {
      return this.lastAgentCycle.nextAction
    }
    return this.lastScanAt()
      ? `Last ${new Date(this.lastScanAt() || '').toLocaleTimeString([], {
          hour: '2-digit',
          minute: '2-digit',
        })}`
      : 'No cycle yet'
  }

  private rebuildViewModel(): void {
    this.attentionItemsView = (this.workflowDashboard?.approvalItems || []).slice(0, 4)
    this.activeItemsView = (this.workflowDashboard?.readyItems || []).slice(0, 5)
    this.blockedItemsView = (this.workflowDashboard?.blockedItems || []).slice(0, 4)
    this.lifePrioritiesView = (this.ambientOverview?.needs || [])
      .filter((need) => need.enabled)
      .slice(0, 8)
    this.recentMemoriesView = this.memories.slice(0, 5)
    this.recentActivityView = this.buildRecentActivity()
    this.commandActionsView = this.buildCommandActions()
  }
}

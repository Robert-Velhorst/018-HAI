import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { forkJoin, of } from 'rxjs'
import { catchError, timeout } from 'rxjs/operators'
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
import { ThemeMode, ThemeService } from '../../services/theme.service'
import { WorkflowService } from '../../services/workflow/workflow.service'

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
  queryParams?: Record<string, string>
  section?: ControlCenterSection
  execute?: () => void
}

type ControlCenterSection =
  | 'overview'
  | 'attention'
  | 'blocked'
  | 'priorities'
  | 'activity'
  | 'memory'
  | 'diagnostics'

interface NavigationItem {
  label: string
  icon: string
  route?: string
  section?: ControlCenterSection
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-control-center',
  templateUrl: './control-center.component.html',
  styleUrls: ['./control-center.component.scss'],
})
export class ControlCenterComponent implements OnInit {
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

  loading = false
  scanning = false
  memoryLoading = false
  memoriesLoaded = false
  diagnosticsListLoading = false
  diagnosticsLoaded = false
  resolvingId = ''
  archivingMemoryId = ''
  diagnosticsExpanded = false
  mobileNavigationOpen = false
  private checkingIds = new Set<string>()
  private launchingIds = new Set<string>()

  isDiagnosticsVisible = false
  diagnostics?: IAutomationDiagnostics
  diagnosticsLoading = false
  diagnosticsName = ''
  selectedAction?: CommandAction
  activeSection: ControlCenterSection = 'overview'
  themeMode: ThemeMode = 'light'

  readonly navigationGroups: Array<{ label: string; items: NavigationItem[] }> = [
    {
      label: 'Work',
      items: [
        { label: 'Command Center', icon: 'appstore', section: 'overview' },
        { label: 'Background Ops', icon: 'thunderbolt', route: '/background-operations' },
        { label: 'Pursuits', icon: 'flag', route: '/pursuits' },
        { label: 'Approvals', icon: 'check-square', section: 'attention' },
        { label: 'Workflows', icon: 'unordered-list', route: '/workflow-engine' },
        { label: 'Automations', icon: 'setting', route: '/home' },
      ],
    },
    {
      label: 'Intelligence',
      items: [
        { label: 'Priorities', icon: 'heart', section: 'priorities' },
        { label: 'Sources', icon: 'cluster', route: '/connected-sources' },
        { label: 'Account Bridges', icon: 'link', route: '/account-bridges' },
        { label: 'Memory', icon: 'database', route: '/memory' },
        { label: 'Task Planning', icon: 'partition', route: '/task-blueprint' },
        { label: 'Frameworks', icon: 'apartment', route: '/framework-registry' },
        { label: 'Verified Answers', icon: 'safety-certificate', route: '/grounded-answers' },
        { label: 'Activity', icon: 'history', section: 'activity' },
      ],
    },
    {
      label: 'System',
      items: [
        { label: 'System Status', icon: 'heart', route: '/system-status' },
        { label: 'Runtime Control', icon: 'poweroff', route: '/runtime-control' },
        { label: 'Brain Settings', icon: 'safety-certificate', route: '/ambient-brain' },
        { label: 'Models', icon: 'deployment-unit', route: '/llm-policy' },
        { label: 'Model Intelligence', icon: 'experiment', route: '/model-intelligence' },
        { label: 'Runtime Lab', icon: 'api', route: '/runtime-lab' },
      ],
    },
  ]

  constructor(
    @Inject(AUTOMATIONS_SERVICE_TOKEN)
    private automationsService: IAutomationsService,
    private workflowService: WorkflowService,
    private pursuitService: PursuitService,
    private agentCycleService: AgentCycleService,
    private agentRuntimeService: AgentRuntimeService,
    private ambientService: AmbientService,
    private memoryService: ContextMemoryService,
    private themeService: ThemeService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.themeMode = this.themeService.mode()
    this.refresh()
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle()
  }

  themeLabel(): string {
    return this.themeService.label()
  }

  themeIcon(): string {
    return this.themeService.icon()
  }

  refresh(): void {
    this.loading = true
    let pending = 3
    const done = () => {
      pending -= 1
      if (pending <= 0) {
        this.loading = false
      }
    }

    this.workflowService.dashboard().pipe(
        timeout(2500),
        catchError(() => of(undefined))
      ).subscribe({
        next: (workflow) => {
          this.workflowDashboard = workflow
          this.rebuildViewModel()
          done()
        },
        error: () => {
          done()
        },
      })

    this.ambientService.overview('summary').pipe(
        timeout(2500),
        catchError(() => of(undefined))
      ).subscribe({
        next: (ambient) => {
          this.ambientOverview = ambient
          this.rebuildViewModel()
          done()
        },
        error: () => {
          done()
        },
      })

    this.pursuitService.dashboard('counts').pipe(
        timeout(1800),
        catchError(() => of(undefined))
      ).subscribe({
        next: (pursuits) => {
          this.pursuitDashboard = pursuits
          this.rebuildViewModel()
          done()
        },
        error: () => {
          done()
        },
      })
  }

  loadMemories(force = false): void {
    if ((this.memoriesLoaded && !force) || this.memoryLoading) {
      return
    }
    this.memoryLoading = true
    this.memoryService.list(undefined, false).subscribe({
      next: (memories) => {
        this.memories = memories
          .sort(
            (a, b) =>
              new Date(b.updatedAt || b.createdAt || 0).getTime() -
              new Date(a.updatedAt || a.createdAt || 0).getTime()
          )
          .slice(0, 20)
        this.memoriesLoaded = true
        this.memoryLoading = false
        this.rebuildViewModel()
      },
      error: () => {
        this.memoryLoading = false
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
    forkJoin({
      automations: this.automationsService.getAutomations().pipe(
        catchError(() => of([] as IAutomationModel[]))
      ),
      summary: this.automationsService.getHealthSummary().pipe(
        catchError(() => of(undefined))
      ),
      runtimes: this.agentRuntimeService.overview().pipe(
        timeout(2500),
        catchError(() => of({ runtimes: [] as IAgentRuntimeInfo[], health: [] as IAgentRuntimeHealth[] }))
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
    this.scanning = true
    this.agentCycleService.run({ trigger: 'command-center', limit: 5 }).subscribe({
      next: (result) => {
        this.lastAgentCycle = result
        if (result.dashboard) {
          this.workflowDashboard = result.dashboard
        }
        if (result.ambientScan && this.ambientOverview) {
          this.ambientOverview = {
            ...this.ambientOverview,
            scans: [result.ambientScan, ...(this.ambientOverview.scans || [])].slice(0, 10),
          }
        }
        this.scanning = false
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
        this.scanning = false
        this.notification.error(
          'Agent cycle failed',
          error?.error?.error || 'The operational cycle could not complete.'
        )
      },
    })
  }

  resolveApproval(item: IWorkflowItem, approved: boolean): void {
    this.resolvingId = item.id
    this.workflowService
      .resolveApproval(item.id, {
        approved,
        note: approved
          ? 'Approved from the household operations dashboard.'
          : 'Rejected from the household operations dashboard.',
        actor: 'operator',
      })
      .subscribe({
        next: () => {
          this.resolvingId = ''
          this.notification.success(
            approved ? 'Approved' : 'Rejected',
            approved
              ? 'The AI may continue within the recorded safety limits.'
              : 'The task has been blocked and the decision was logged.'
          )
          this.refresh()
        },
        error: () => {
          this.resolvingId = ''
          this.notification.error(
            'Decision not saved',
            'The approval queue could not be updated.'
          )
        },
      })
  }

  snooze(item: IWorkflowItem): void {
    this.resolvingId = item.id
    this.workflowService
      .transition(item.id, {
        targetState: 'waiting_external_input',
        message: 'Snoozed from the household operations dashboard.',
        actor: 'operator',
      })
      .subscribe({
        next: () => {
          this.resolvingId = ''
          this.notification.success(
            'Snoozed',
            'The task is waiting and remains visible in the audit trail.'
          )
          this.refresh()
        },
        error: () => {
          this.resolvingId = ''
          this.notification.error(
            'Could not snooze',
            'Open task details to choose an allowed workflow state.'
          )
        },
      })
  }

  archiveMemory(memory: IContextMemory): void {
    if (!memory.id) return
    this.archivingMemoryId = memory.id
    this.memoryService.archive(memory.id).subscribe({
      next: () => {
        this.archivingMemoryId = ''
        this.memories = this.memories.filter((item) => item.id !== memory.id)
        this.rebuildViewModel()
        this.notification.success(
          'Memory archived',
          'The item will no longer be used as active context.'
        )
      },
      error: () => {
        this.archivingMemoryId = ''
        this.notification.error(
          'Memory not archived',
          'The memory update could not be changed.'
        )
      },
    })
  }

  attentionItems(): IWorkflowItem[] {
    return this.attentionItemsView
  }

  activeItems(): IWorkflowItem[] {
    return this.activeItemsView
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
    return this.loading || this.attentionItemsView.length > 0 || this.activeItemsView.length > 0
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
    return (
      Number(dashboard.counts?.['needsRobert'] || 0) +
      Number(dashboard.counts?.['blocked'] || 0) +
      Number(dashboard.counts?.['stale'] || 0)
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
        route: '/workflow-engine',
        queryParams: { queue: 'approval' },
      },
      {
        id: 'automation',
        title: 'Add automation',
        detail: 'Register a script, service, API, or workflow target.',
        icon: 'plus',
        tone: 'blue',
        primaryMetric: `${this.automations.length} registered`,
        secondaryMetric: 'Opens automation registry',
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
        context: 'Runs your owner-scoped source sync, stale-claim recovery, follow-ups, safe workflows, ambient scan, pursuits, and learning. High-risk work remains approval-gated and no phase can fall back to global execution.',
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
        title: 'Sync sources',
        detail: 'Refresh connected accounts and documents.',
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
        title: 'Run health checks',
        detail: 'Probe automations and runtime readiness.',
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
    if (key.includes('work') || key.includes('income')) return 'project'
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
    try {
      const parsed = new URL(uri)
      if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
        window.open(parsed.toString(), '_blank', 'noopener,noreferrer')
      }
    } catch {
      this.notification.warning(
        'Source unavailable',
        'This source does not have a browser-safe link.'
      )
    }
  }

  navigate(route: string, queryParams?: Record<string, string>): void {
    this.mobileNavigationOpen = false
    if (route === '/control-center') {
      this.activeSection = 'overview'
      setTimeout(() => this.scrollToSection('overview'))
    }
    this.router.navigate([route], queryParams ? { queryParams } : undefined)
  }

  activateNavigationItem(item: NavigationItem): void {
    if (item.route) {
      this.navigate(item.route)
      return
    }
    if (item.section) {
      this.navigateToSection(item.section)
    }
  }

  isNavigationItemActive(item: NavigationItem): boolean {
    if (item.section && !item.route) {
      return this.activeSection === item.section
    }
    return !!item.route && this.router.url.split('?')[0] === item.route
  }

  navigationBadge(item: NavigationItem): string | undefined {
    switch (item.label) {
      case 'Approvals':
        return this.attentionItems().length ? `${this.attentionItems().length}` : undefined
      case 'Pursuits':
        return this.pursuitAttentionCount() ? `${this.pursuitAttentionCount()}` : undefined
      case 'Priorities':
        return this.lifePriorities().length ? `${this.lifePriorities().length}` : undefined
      case 'Activity':
        return this.recentActivity().length ? `${this.recentActivity().length}` : undefined
      default:
        return undefined
    }
  }

  menuKey(label: string): string {
    return label
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/(^-|-$)/g, '')
  }

  navigateToSection(section: ControlCenterSection): void {
    this.mobileNavigationOpen = false
    this.activeSection = section
    this.openContainingDisclosure(section)
    if (section === 'diagnostics' && !this.diagnosticsExpanded) {
      this.diagnosticsExpanded = true
      this.loadDiagnosticsData()
    }
    if (section === 'memory') {
      this.loadMemories()
    }
    setTimeout(() => this.scrollToSection(section))
  }

  private scrollToSection(section: ControlCenterSection): void {
    document.getElementById(section)?.scrollIntoView({
      // Action controls should make their destination visible immediately. A
      // long smooth scroll made valid clicks appear to do nothing, especially
      // on the compact dashboard viewport.
      behavior: 'auto',
      block: 'start',
    })
  }

  private openContainingDisclosure(section: ControlCenterSection): void {
    const target = document.getElementById(section)
    const disclosure = target?.closest('details') as HTMLDetailsElement | null
    if (disclosure) {
      disclosure.open = true
    }
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
    window.setTimeout(() => this.continueSelectedAction(action), 360)
  }

  private continueSelectedAction(action: CommandAction): void {
    if (action.execute) {
      action.execute()
      return
    }
    if (action.route) {
      this.navigate(action.route, action.queryParams)
      return
    }
    if (action.section) {
      this.navigateToSection(action.section)
    }
  }

  actionModeLabel(action: CommandAction): string {
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

  openMemory(): void {
    this.router.navigate(['/memory'])
  }

  toggleDiagnostics(): void {
    this.diagnosticsExpanded = !this.diagnosticsExpanded
    if (this.diagnosticsExpanded) {
      this.activeSection = 'diagnostics'
      this.loadDiagnosticsData()
      setTimeout(() => this.scrollToSection('diagnostics'))
    } else if (this.activeSection === 'diagnostics') {
      this.activeSection = 'overview'
    }
  }

  runHealthCheck(automation: IAutomationModel): void {
    if (!automation.id) return
    const id = automation.id
    this.checkingIds.add(id)
    this.automationsService.runHealthCheck(id).subscribe({
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
        this.notification.success(
          'Check completed',
          `${automation.name} is ${result.status}.`
        )
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
    this.launchingIds.add(id)
    this.automationsService.launchAutomation(id).subscribe({
      next: (result) => {
        this.launchingIds.delete(id)
        automation.lastLaunchAt = result.launchedAt
        if (result.status === 'completed' || result.status === 'ready') {
          this.notification.success(
            'Automation started',
            result.message || automation.name
          )
        } else {
          this.notification.warning(
            'Automation did not start',
            result.message || result.status
          )
        }
      },
      error: () => {
        this.launchingIds.delete(id)
        this.notification.error(
          'Launch failed',
          `${automation.name} could not be started.`
        )
      },
    })
  }

  isLaunching(automation: IAutomationModel): boolean {
    return !!automation.id && this.launchingIds.has(automation.id)
  }

  openDiagnostics(automation: IAutomationModel): void {
    if (!automation.id) return
    this.isDiagnosticsVisible = true
    this.diagnosticsLoading = true
    this.diagnostics = undefined
    this.diagnosticsName = automation.name
    this.automationsService.getDiagnostics(automation.id).subscribe({
      next: (diagnostics) => {
        this.diagnostics = diagnostics
        this.diagnosticsLoading = false
      },
      error: () => {
        this.diagnosticsLoading = false
        this.notification.error(
          'Diagnostics unavailable',
          'The detailed automation record could not be loaded.'
        )
      },
    })
  }

  closeDiagnostics(): void {
    this.isDiagnosticsVisible = false
    this.diagnostics = undefined
  }

  diagnosticsCheckKeys(): string[] {
    return this.diagnostics ? Object.keys(this.diagnostics.checks || {}) : []
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

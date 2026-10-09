import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { finalize, Subscription, switchMap } from 'rxjs';
import {
  IAgentRuntimeEcosystemSurface,
  IAgentRuntimeHealth,
  IAgentRuntimeInfo,
  IAgentRuntimeSkill,
} from '../../models/agent-runtime.model.interface';
import {
  ICommandDashboard,
  IMemoryEngineSearchResult,
} from '../../models/memory-engine.model.interface';
import { IAssistantCommandResult } from '../../models/assistant-command.model.interface';
import { IMemoryEngineService } from '../../services/memory-engine.service.interface';
import { MEMORY_ENGINE_SERVICE_TOKEN } from '../../services/memory-engine/memory-engine.service.token';
import { AgentRuntimeService } from '../../services/agent-runtime.service';
import { AssistantCommandService } from '../../services/assistant-command.service';
import {
  IPursuitBrief,
  IPursuitBriefCard,
  IPursuitDashboard,
  IPursuitDashboardDecision,
  IPursuitListItem,
} from '../../models/pursuit.model.interface';
import { PursuitService } from '../../services/pursuit.service';
import { WorkflowService } from '../../services/workflow/workflow.service';
import { safeWebSourceHref } from '../../control-room/source-navigation';

type CommandActionKey = 'manage-pursuits' | 'plan-next' | 'clear-blockers' | 'run-cycle' | 'run-safe';

interface DashboardAction {
  key: CommandActionKey;
  title: string;
  description: string;
  metric: string;
  icon: string;
  message: string;
  executeAllowed: boolean;
  runCycle: boolean;
  route?: string;
}

interface RuntimeSurfaceGroup {
  title: string;
  description: string;
  surfaces: IAgentRuntimeEcosystemSurface[];
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-command-dashboard',
    templateUrl: './command-dashboard.component.html',
    styleUrls: ['./command-dashboard.component.scss'],
    standalone: false
})
export class CommandDashboardComponent implements OnInit, OnDestroy {
  private readonly openClawArchiveMaxBytes = 750 * 1024 * 1024;
  dashboard?: ICommandDashboard;
  pursuitDashboard?: IPursuitDashboard;
  pursuitBrief?: IPursuitBrief;
  searchResult?: IMemoryEngineSearchResult;
  loading = false;
  dashboardUnavailable = false;
  pursuitsLoading = false;
  pursuitDashboardUnavailable = false;
  pursuitBriefUnavailable = false;
  searching = false;
  searchError = '';
  runtimeLoading = false;
  runtimeOverviewUnavailable = false;
  commandLoading = '';
  runtimes: IAgentRuntimeInfo[] = [];
  runtimeHealth: Record<string, IAgentRuntimeHealth> = {};
  runtimeSkills: Record<string, IAgentRuntimeSkill[]> = {};
  runtimeSkillsLoading: Record<string, boolean> = {};
  runtimeSkillsUnavailable: Record<string, boolean> = {};
  openClawEcosystemPath = '';
  private openClawEcosystemPathEdited = false;
  openClawConfigLoading = false;
  openClawRefreshLoading = false;
  openClawRollbackLoading = false;
  openClawUploadLoading = false;
  openClawUploadFileName = '';
  openClawUploadError = '';
  resolvingDashboardDecisionId = '';
  deletingArchiveIds = new Set<string>();
  selectedRuntimeSurface?: IAgentRuntimeEcosystemSurface;
  commandLogs: IAssistantCommandResult[] = [];
  commandLogsLoading = false;
  commandLogsUnavailable = false;
  lastCommand?: IAssistantCommandResult;
  private dashboardRefreshSubscription?: Subscription;
  private pursuitDashboardSubscription?: Subscription;
  private pursuitBriefSubscription?: Subscription;
  private runtimeOverviewSubscription?: Subscription;
  private commandLogsSubscription?: Subscription;

  actions: DashboardAction[] = [
    {
      key: 'manage-pursuits',
      title: 'Manage pursuits',
      description: 'Open top-level goals that connect workflows, evidence, sources, memory, and approvals.',
      metric: 'Goal layer',
      icon: 'flag',
      message: '',
      executeAllowed: false,
      runCycle: false,
      route: '/pursuits',
    },
    {
      key: 'plan-next',
      title: 'Plan next action',
      description: 'Turn current project context into a completion-first plan.',
      metric: 'Context + routing',
      icon: 'compass',
      message: 'Look at the open work for 018-HAI and produce the next safest completion-first action.',
      executeAllowed: false,
      runCycle: false,
    },
    {
      key: 'clear-blockers',
      title: 'Clear blockers',
      description: 'Find waiting work, stale claims, and follow-up actions.',
      metric: 'Open loops',
      icon: 'clock-circle',
      message: 'Clear blockers and follow up on open loops for 018-HAI.',
      executeAllowed: true,
      runCycle: true,
    },
    {
      key: 'run-cycle',
      title: 'Refresh my operating brief',
      description: 'Refresh your own context, pursuit decisions, and next action without starting global workers.',
      metric: 'Personal refresh',
      icon: 'deployment-unit',
      message: 'Refresh my HAI operating brief and surface my next best action.',
      executeAllowed: true,
      runCycle: true,
    },
    {
      key: 'run-safe',
      title: 'Run safe steps',
      description: 'Execute only low-risk allowed work through the task success engine.',
      metric: 'Approval-gated',
      icon: 'safety-certificate',
      message: 'Run the safest allowed steps for the current 018-HAI operational work and queue review for anything risky.',
      executeAllowed: true,
      runCycle: false,
    },
  ];

  searchForm: FormGroup = this.fb.group({
    query: ['What did we decide about the HAI memory engine?', [Validators.required]],
    projectKey: [''],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(MEMORY_ENGINE_SERVICE_TOKEN) private memoryEngine: IMemoryEngineService,
    private agentRuntimes: AgentRuntimeService,
    private assistantCommands: AssistantCommandService,
    private pursuits: PursuitService,
    private workflows: WorkflowService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh();
    this.refreshRuntimes();
    this.loadCommandLogs();
  }

  ngOnDestroy(): void {
    this.dashboardRefreshSubscription?.unsubscribe();
    this.pursuitDashboardSubscription?.unsubscribe();
    this.pursuitBriefSubscription?.unsubscribe();
    this.runtimeOverviewSubscription?.unsubscribe();
    this.commandLogsSubscription?.unsubscribe();
  }

  refreshRuntimes(): void {
    this.runtimeLoading = true;
    this.runtimeOverviewUnavailable = false;
    this.runtimeOverviewSubscription?.unsubscribe();
    this.runtimeOverviewSubscription = this.agentRuntimes.overview().subscribe({
      next: (overview) => {
        if (!this.isRuntimeOverview(overview)) {
          this.runtimeLoading = false;
          this.runtimeOverviewUnavailable = true;
          this.notification.error(
            'Agent runtimes unavailable',
            'The runtime registry response was incomplete. The previous confirmed inventory is retained.'
          );
          return;
        }
        const { runtimes, health } = overview;
        this.runtimes = runtimes;
        this.runtimeHealth = health.reduce(
          (result, item) => ({ ...result, [item.runtimeId]: item }),
          {} as Record<string, IAgentRuntimeHealth>
        );
        const openClaw = runtimes.find((runtime) => runtime.id === 'openclaw');
        if (!this.openClawEcosystemPathEdited) {
          this.openClawEcosystemPath = openClaw?.ecosystemPath || '';
        }
        this.runtimeLoading = false;
        this.runtimeOverviewUnavailable = false;
      },
      error: (error) => {
        this.runtimeLoading = false;
        this.runtimeOverviewUnavailable = true;
        this.notification.error(
          'Agent runtimes unavailable',
          error?.error?.error || 'Failed to load the controlled runtime registry.'
        );
      },
    });
  }

  runtimeStatus(runtime: IAgentRuntimeInfo): string {
    return this.runtimeHealth[runtime.id]?.status || (runtime.enabled ? 'unknown' : 'disabled');
  }

  runtimeStatusType(runtime: IAgentRuntimeInfo): string {
    switch (this.runtimeStatus(runtime)) {
      case 'ready':
        return 'success';
      case 'disabled':
        return 'default';
      case 'auth_required':
        return 'warning';
      default:
        return 'error';
    }
  }

  runtimeReason(runtime: IAgentRuntimeInfo): string {
    return this.runtimeHealth[runtime.id]?.reason || 'Not probed';
  }

  openClawGatewayHealthTitle(runtime: IAgentRuntimeInfo): string {
    const health = this.runtimeHealth[runtime.id];
    if (!health) {
      return 'Runtime health has not been probed.';
    }
    const detail = [health.reason];
    if (health.version) {
      detail.push(`Authenticated gateway version: ${health.version}.`);
    }
    const ledger = health.gatewayTaskLedger;
    if (ledger) {
      const counts = Object.entries(ledger.statusCounts)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([status, count]) => `${count} ${status}`)
        .join(', ');
      detail.push(
        `Read-only task ledger sampled ${ledger.sampledTasks} task${ledger.sampledTasks === 1 ? '' : 's'}${counts ? `: ${counts}` : ''}${ledger.truncated ? '; more tasks may exist' : ''}.`
      );
    }
    return detail.join(' ');
  }

  openClawRuntime(): IAgentRuntimeInfo | undefined {
    return this.runtimes.find((runtime) => runtime.id === 'openclaw');
  }

  openClawSurfaceCount(runtime?: IAgentRuntimeInfo): number {
    return (runtime?.ecosystem || []).reduce((total, surface) => total + (surface.count || 0), 0);
  }

  openClawApprovalSurfaceCount(runtime?: IAgentRuntimeInfo): number {
    return (runtime?.ecosystem || []).filter((surface) => surface.approvalRequired).length;
  }

  openClawHighRiskSurfaceCount(runtime?: IAgentRuntimeInfo): number {
    return (runtime?.ecosystem || []).filter((surface) => (surface.riskLevel || '').toLowerCase() === 'high').length;
  }

  openClawPosture(runtime?: IAgentRuntimeInfo): string {
    if (!runtime) {
      return 'OpenClaw is not registered.';
    }
    if (!runtime.enabled) {
      return 'Installed as a reference surface. Runtime execution is disabled.';
    }
    if (this.runtimeStatus(runtime) === 'available' && !runtime.executionEnabled) {
      return 'OpenClaw Companion discovery is connected. Task execution remains disabled until the separate CLI, workspace, approval-proof, and gateway credential requirements are configured.';
    }
    if (!runtime.configured) {
      return 'Registered but not ready. Complete workspace, executable, and safety configuration first.';
    }
    if (!runtime.executionEnabled) {
      return 'Configured but blocked by current HAI policy.';
    }
    return 'Ready for approved noninteractive OpenClaw task envelopes only.';
  }

  openClawSurfaceGroups(runtime?: IAgentRuntimeInfo): RuntimeSurfaceGroup[] {
    const surfaces = runtime?.ecosystem || [];
    const byName = (names: string[]) =>
      surfaces.filter((surface) => names.includes(surface.category));
    return [
      {
        title: 'Runtime substrate',
        description: 'What HAI can use to understand the installed OpenClaw package and task runtime.',
        surfaces: byName([
          'Package inventory',
          'Package metadata',
          'Core packages',
          'Source modules',
          'Documentation corpus',
          'Control UI views',
          'Control UI controllers',
        ]),
      },
      {
        title: 'Skills and reasoning maps',
        description: 'OpenClaw skills, prompt maps, agent profiles, and references available for planning.',
        surfaces: byName([
          'Skills',
          'Skill scripts',
          'Agent profiles',
          'Skill reference maps',
          'Completeness maps',
          'Maintainer notes',
          'Codex prompt maps',
          'Repository instructions',
          'Repository docs',
          'Configuration profiles',
        ]),
      },
      {
        title: 'Execution and integrations',
        description: 'Provider, channel, tool, app, CI, deployment, script, and test surfaces that remain approval-gated.',
        surfaces: byName([
          'Provider extensions',
          'Channel extensions',
          'Tool/runtime extensions',
          'Companion apps',
          'Root scripts',
          'QA assets',
          'Test suites',
          'Deployment targets',
          'GitHub workflows',
          'GitHub Actions',
          'GitHub issue templates',
          'Repository config',
          'All extensions',
        ]),
      },
      {
        title: 'Governance and safety',
        description: 'Controls, blockers, setup checks, and security maps that decide whether OpenClaw can run.',
        surfaces: byName([
          'Configured HAI surfaces',
          'HAI-blocked high-risk surfaces',
          'Operator setup checklist',
          'Security and CodeQL maps',
          'Security assets',
          'Inventory warnings',
        ]),
      },
    ].filter((group) => group.surfaces.length);
  }

  runtimeSurfaceColor(surface: IAgentRuntimeEcosystemSurface): string {
    if (surface.approvalRequired) {
      return 'gold';
    }
    switch ((surface.riskLevel || '').toLowerCase()) {
      case 'high':
        return 'red';
      case 'medium':
        return 'gold';
      case 'low':
        return 'green';
      default:
        return 'blue';
    }
  }

  ecosystemTitle(surface: IAgentRuntimeEcosystemSurface): string {
    const items = surface.items?.length ? `Items: ${surface.items.join(', ')}` : 'No discovered items yet';
    const more = surface.more ? `; ${surface.more} more hidden for readability` : '';
    return `${surface.category} is ${surface.status}. ${items}${more}. ${surface.control || ''}`.trim();
  }

  inspectRuntimeSurface(surface: IAgentRuntimeEcosystemSurface): void {
    this.selectedRuntimeSurface = surface;
  }

  closeRuntimeSurface(): void {
    this.selectedRuntimeSurface = undefined;
  }

  runtimeSurfaceItems(surface?: IAgentRuntimeEcosystemSurface): string[] {
    return surface?.items || [];
  }

  loadRuntimeSkills(runtime: IAgentRuntimeInfo): void {
    if (!runtime?.id || this.runtimeSkillsLoading[runtime.id]) {
      return;
    }
    this.runtimeSkillsLoading[runtime.id] = true;
    this.runtimeSkillsUnavailable[runtime.id] = false;
    this.agentRuntimes.skills(runtime.id).subscribe({
      next: (skills) => {
        if (!Array.isArray(skills)) {
          this.runtimeSkillsUnavailable[runtime.id] = true;
          this.runtimeSkillsLoading[runtime.id] = false;
          this.notification.error('Runtime skills unavailable', `The ${runtime.name} skills response was invalid.`);
          return;
        }
        this.runtimeSkills[runtime.id] = skills;
        this.runtimeSkillsLoading[runtime.id] = false;
        this.runtimeSkillsUnavailable[runtime.id] = false;
      },
      error: (error) => {
        this.runtimeSkillsLoading[runtime.id] = false;
        this.runtimeSkillsUnavailable[runtime.id] = true;
        this.notification.error(
          'Runtime skills unavailable',
          error?.error?.error || `Failed to load skills for ${runtime.name}.`
        );
      },
    });
  }

  runtimeSkillTitle(skill: IAgentRuntimeSkill): string {
    return [
      skill.description || skill.name,
      `risk=${skill.riskLevel || 'unknown'}`,
      `mode=${skill.executionMode || 'unknown'}`,
      skill.approvalRequired ? 'approval required' : 'no approval flag',
      skill.source ? `source=${skill.source}` : '',
    ]
      .filter(Boolean)
      .join(' · ');
  }

  runtimeSkillColor(skill: IAgentRuntimeSkill): string {
    switch ((skill.riskLevel || '').toLowerCase()) {
      case 'high':
        return 'red';
      case 'medium':
        return 'gold';
      case 'low':
        return 'green';
      default:
        return 'blue';
    }
  }

  runtimeSkillsFor(runtime: IAgentRuntimeInfo): IAgentRuntimeSkill[] {
    return this.runtimeSkills[runtime.id] || [];
  }

  refresh(): void {
    this.loading = true;
    this.dashboardUnavailable = false;
    this.refreshPursuits();
    this.dashboardRefreshSubscription?.unsubscribe();
    this.dashboardRefreshSubscription = this.memoryEngine.dashboard().subscribe({
      next: (dashboard) => {
        if (!this.isMemoryDashboard(dashboard)) {
          this.loading = false;
          this.dashboardUnavailable = true;
          this.notification.error(
            'Memory engine returned incomplete data',
            'The previous dashboard snapshot is retained because the latest response was incomplete.'
          );
          return;
        }
        this.dashboard = dashboard;
        this.loading = false;
        this.dashboardUnavailable = false;
      },
      error: (error) => {
        this.loading = false;
        this.dashboardUnavailable = true;
        this.notification.error(
          'Memory engine unavailable',
          error?.error?.error || 'Failed to load the command dashboard.'
        );
      },
    });
  }

  onOpenClawEcosystemPathInput(value: string): void {
    this.openClawEcosystemPath = value;
    this.openClawEcosystemPathEdited = value.trim() !== (this.openClawRuntime()?.ecosystemPath || '').trim();
  }

  isOpenClawBusy(): boolean {
    return this.openClawConfigLoading || this.openClawRefreshLoading ||
      this.openClawRollbackLoading || this.openClawUploadLoading;
  }

  rollbackOpenClawEcosystem(runtime: IAgentRuntimeInfo): void {
    if (runtime?.id !== 'openclaw' || !runtime.ecosystemRollbackAvailable || this.isOpenClawBusy()) {
      return;
    }
    this.openClawRollbackLoading = true;
    this.agentRuntimes.prepareOpenClawEcosystemRollback().pipe(
      switchMap((authorization) => this.agentRuntimes.rollbackOpenClawEcosystem(authorization)),
      finalize(() => {
        this.openClawRollbackLoading = false;
      })
    ).subscribe({
      next: (updatedRuntime) => {
        const index = this.runtimes.findIndex((item) => item.id === updatedRuntime.id);
        if (index >= 0) {
          this.runtimes[index] = updatedRuntime;
        }
        if (!this.openClawEcosystemPathEdited) {
          this.openClawEcosystemPath = updatedRuntime.ecosystemPath || '';
        }
        this.notification.success(
          'OpenClaw archive rolled back',
          'The previously verified archive is selected. Runtime status has been refreshed.'
        );
      },
      error: (error) => {
        this.notification.error(
          'OpenClaw archive rollback failed',
          error?.error?.error || 'HAI could not restore the previously verified OpenClaw archive. The current selection was retained.'
        );
      },
    });
  }

  setOpenClawEcosystemPath(runtime: IAgentRuntimeInfo): void {
    if (this.isOpenClawBusy()) {
      return;
    }
    const path = this.openClawEcosystemPath?.trim();
    if (!path) {
      this.notification.error('OpenClaw ecosystem path is required', 'Enter the local path or zip file location.');
      return;
    }
    this.openClawConfigLoading = true;
    this.agentRuntimes.prepareOpenClawEcosystemPath(path).pipe(
      switchMap((authorization) => this.agentRuntimes.setOpenClawEcosystemPath(path, authorization)),
      finalize(() => {
        this.openClawConfigLoading = false;
      })
    ).subscribe({
      next: (runtime) => {
        const index = this.runtimes.findIndex((item) => item.id === runtime.id);
        if (index >= 0) {
          this.runtimes[index] = runtime;
        }
        this.openClawEcosystemPath = runtime.ecosystemPath || path;
        this.openClawEcosystemPathEdited = false;
        this.notification.success('OpenClaw ecosystem path updated', `Configured at ${this.openClawEcosystemPath}`);
      },
      error: (error) => {
        this.notification.error(
          'OpenClaw ecosystem config failed',
          error?.error?.error || 'The backend rejected the configured OpenClaw ecosystem path.'
        );
      },
    });
  }

  refreshOpenClawEcosystem(runtime: IAgentRuntimeInfo): void {
    if (runtime.id !== 'openclaw' || this.isOpenClawBusy()) {
      return;
    }
    this.openClawRefreshLoading = true;
    this.agentRuntimes.prepareOpenClawEcosystemRefresh().pipe(
      switchMap((authorization) => this.agentRuntimes.refreshOpenClawEcosystem(authorization)),
      finalize(() => {
        this.openClawRefreshLoading = false;
      })
    ).subscribe({
      next: (updatedRuntime) => {
        const index = this.runtimes.findIndex((item) => item.id === updatedRuntime.id);
        if (index >= 0) {
          this.runtimes[index] = updatedRuntime;
          if (!this.openClawEcosystemPathEdited) {
            this.openClawEcosystemPath = updatedRuntime.ecosystemPath || '';
          }
        }
        this.notification.success(
          'OpenClaw ecosystem refreshed',
          'Re-scanned the selected OpenClaw source after a one-time owner confirmation.'
        );
      },
      error: (error) => {
        this.notification.error(
          'OpenClaw ecosystem refresh failed',
          error?.error?.error || 'The backend failed to refresh OpenClaw ecosystem inventory.'
        );
      },
    });
  }

  onOpenClawEcosystemUpload(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const file = target?.files?.[0] || null;
    if (!file) {
      return;
    }
    this.uploadOpenClawEcosystem(file);
    if (target) {
      target.value = '';
    }
  }

  uploadOpenClawEcosystem(file: File): void {
    if (this.isOpenClawBusy()) {
      return;
    }
    if (!file || !file.name.toLowerCase().endsWith('.zip')) {
      this.openClawUploadFileName = '';
      this.openClawUploadError = '';
      this.notification.error('Invalid OpenClaw archive', 'Upload a .zip archive exported from openclaw-main.');
      return;
    }
    if (file.size > this.openClawArchiveMaxBytes) {
      this.notification.error(
        'OpenClaw archive is too large',
        'The local gateway accepts OpenClaw ecosystem archives up to 750 MB.'
      );
      this.openClawUploadFileName = '';
      this.openClawUploadError = '';
      return;
    }
    this.openClawUploadFileName = file.name;
    this.openClawUploadError = '';
    this.openClawUploadLoading = true;
    this.agentRuntimes.prepareOpenClawEcosystemUpload(file).pipe(
      switchMap((authorization) => this.agentRuntimes.uploadOpenClawEcosystem(file, authorization)),
      finalize(() => {
        this.openClawUploadLoading = false;
      })
    ).subscribe({
      next: (runtime) => {
        const index = this.runtimes.findIndex((item) => item.id === runtime.id);
        if (index >= 0) {
          this.runtimes[index] = runtime;
        }
        if (!this.openClawEcosystemPathEdited) {
          this.openClawEcosystemPath = runtime.ecosystemPath || runtime.name;
        }
        this.openClawUploadFileName = '';
        this.openClawUploadError = '';
        this.notification.success(
          'OpenClaw ecosystem uploaded',
          'The uploaded OpenClaw archive was indexed and the runtime surfaces were refreshed.'
        );
      },
      error: (error) => {
        this.openClawUploadError = error?.error?.error || 'The archive could not be indexed. Check runtime inventory before uploading again.';
        this.notification.error(
          'OpenClaw ecosystem upload failed',
          this.openClawUploadError
        );
      },
    });
  }

  refreshPursuits(): void {
    this.pursuitsLoading = true;
    this.pursuitDashboardUnavailable = false;
    this.pursuitDashboardSubscription?.unsubscribe();
    this.pursuitDashboardSubscription = this.pursuits.dashboard().subscribe({
      next: (dashboard) => {
        if (!this.isPursuitDashboard(dashboard)) {
          this.pursuitsLoading = false;
          this.pursuitDashboardUnavailable = true;
          return;
        }
        this.pursuitDashboard = dashboard;
        this.pursuitsLoading = false;
        this.pursuitDashboardUnavailable = false;
      },
      error: () => {
        this.pursuitsLoading = false;
        this.pursuitDashboardUnavailable = true;
      },
    });
    this.pursuitBriefSubscription?.unsubscribe();
    this.pursuitBriefSubscription = this.pursuits.brief().subscribe({
      next: (brief) => {
        this.pursuitBrief = brief;
        this.pursuitBriefUnavailable = false;
      },
      error: () => {
        this.pursuitBriefUnavailable = true;
      },
    });
  }

  loadCommandLogs(): void {
    this.commandLogsSubscription?.unsubscribe();
    this.commandLogsLoading = true;
    this.commandLogsSubscription = this.assistantCommands.logs().subscribe({
      next: (logs) => {
        if (!this.isCommandLogList(logs)) {
          this.commandLogsLoading = false;
          this.commandLogsUnavailable = true;
          return;
        }
        this.commandLogs = logs;
        this.commandLogsLoading = false;
        this.commandLogsUnavailable = false;
        this.lastCommand = this.commandLogs[0] || this.lastCommand;
      },
      error: () => {
        this.commandLogsLoading = false;
        this.commandLogsUnavailable = true;
      },
    });
  }

  runDashboardAction(action: DashboardAction): void {
    if (action.route) {
      this.router.navigate([action.route]);
      return;
    }
    if (this.commandLoading) {
      return;
    }
    this.commandLoading = action.key;
    this.assistantCommands
      .command({
        message: action.message,
        projectKey: '018-HAI',
        executeAllowed: action.executeAllowed,
        runCycle: action.runCycle,
      })
      .subscribe({
        next: (result) => {
          this.commandLoading = '';
          if (!this.isAssistantCommandResult(result)) {
            this.notification.error(
              `${action.title} could not be confirmed`,
              'The command endpoint returned an incomplete result. Check command history before trying again.'
            );
            this.loadCommandLogs();
            return;
          }
          this.lastCommand = result;
          if (result.agentCycle?.pursuitBrief) {
            this.pursuitBrief = result.agentCycle.pursuitBrief;
          }
          this.commandLogs = [result, ...this.commandLogs].slice(0, 50);
          this.commandLogsUnavailable = false;
          if (this.commandHasFailure(result)) {
            this.notification.error(action.title, result.nextAction || result.summary || 'The command recorded a failed or partial-failure step.');
          } else if (result.reviewRequired) {
            this.notification.warning(action.title, result.nextAction || result.summary);
          } else if (result.pursuit?.executionQueued) {
            this.notification.info(action.title, result.nextAction || result.summary);
          } else {
            this.notification.success(action.title, result.nextAction || result.summary);
          }
          this.refresh();
        },
        error: (error) => {
          this.commandLoading = '';
          this.notification.error(
            `${action.title} failed`,
            error?.error?.error || 'The assistant command bridge did not return a result.'
          );
        },
      });
  }

  actionMetric(action: DashboardAction): string {
    if (!this.dashboard) {
      return action.metric;
    }
    switch (action.key) {
      case 'clear-blockers':
        return this.workflowQueuesScopedOut() ? 'See pursuits' : `${this.workflowQueueMetric(this.dashboard.openLoops.length)} loops`;
      case 'run-cycle':
        return this.workflowQueuesScopedOut() ? 'See pursuits' : `${this.workflowQueueMetric(this.dashboard.needsRobert.length)} need Robert`;
      case 'run-safe':
        return `${this.runtimeCountMetric()} runtimes`;
      case 'manage-pursuits':
        return `${this.pursuitMetric(this.activePursuitCount())} active`;
      default:
        return `${this.dashboardUnavailable ? 'stale · ' : ''}${this.dashboard.insightCount} facts`;
    }
  }

  runtimeCountMetric(): string {
    if (this.runtimeOverviewUnavailable) {
      return this.runtimes.length ? `stale · ${this.runtimes.length}` : 'unavailable';
    }
    if (!this.runtimes.length && this.runtimeLoading) {
      return 'checking';
    }
    return String(this.runtimes.length);
  }

  pursuitMetric(count: number): string {
    if (this.pursuitDashboardUnavailable) {
      return this.pursuitDashboard ? `stale · ${count}` : 'unavailable';
    }
    if (!this.pursuitDashboard) {
      return this.pursuitsLoading ? 'checking' : 'unavailable';
    }
    return String(count);
  }

  workflowQueueMetric(count: number): string {
    if (this.workflowQueuesScopedOut()) {
      return 'see pursuits';
    }
    return `${this.dashboardUnavailable ? 'stale · ' : ''}${count}`;
  }

  workflowQueuesScopedOut(): boolean {
    return !!this.dashboard?.warnings?.some((warning) =>
      warning.toLowerCase().includes('workflow queues are shown through the owner-scoped pursuits dashboard')
    );
  }

  correctionMetric(corrections?: unknown[] | null): string {
    return corrections == null ? 'not reported' : String(corrections.length);
  }

  activePursuitCount(): number {
    return Number(this.pursuitDashboard?.counts?.['active'] || 0);
  }

  pursuitDecisionCount(): number {
    return this.pursuitDashboard?.decisionQueue?.length || 0;
  }

  pursuitReadyCount(): number {
    const dashboard = this.pursuitDashboard;
    if (!dashboard) {
      return 0;
    }
    return dashboard.vaReady.length + dashboard.systemReady.length;
  }

  pursuitStuckCount(): number {
    const dashboard = this.pursuitDashboard;
    if (!dashboard) {
      return 0;
    }
    return dashboard.blocked.length + dashboard.stale.length;
  }

  pursuitReviewDueCount(): number {
    return this.pursuitDashboard?.reviewDue?.length || 0;
  }

  pursuitPlanningNeededCount(): number {
    return this.pursuitDashboard?.planningNeeded?.length || 0;
  }

  pursuitCompletionCount(): number {
    return this.pursuitDashboard?.completionCandidates?.length || 0;
  }

  pursuitAttentionCount(): number {
    return this.pursuitDecisionCount() + this.pursuitReviewDueCount() + this.pursuitPlanningNeededCount() + this.pursuitStuckCount() + this.pursuitCompletionCount();
  }

  reviewQueue(): IPursuitListItem[] {
    const dashboard = this.pursuitDashboard;
    if (!dashboard) {
      return [];
    }
    return [...dashboard.needsRobert, ...dashboard.reviewDue, ...dashboard.planningNeeded, ...dashboard.completionCandidates].slice(0, 5);
  }

  robertDecisionQueue(): IPursuitDashboardDecision[] {
    return (this.pursuitDashboard?.decisionQueue || []).slice(0, 5);
  }

  readyQueue(): IPursuitListItem[] {
    const dashboard = this.pursuitDashboard;
    if (!dashboard) {
      return [];
    }
    return [...dashboard.vaReady, ...dashboard.systemReady].slice(0, 5);
  }

  stuckQueue(): IPursuitListItem[] {
    const dashboard = this.pursuitDashboard;
    if (!dashboard) {
      return [];
    }
    return [...dashboard.blocked, ...dashboard.stale].slice(0, 5);
  }

  pursuitSubtitle(item: IPursuitListItem): string {
    if (item.reviewDue && !item.needsRobert) {
      return item.nextAction || item.pursuit.nextRecommendedAction || 'Scheduled review is due';
    }
    if (item.planningNeeded) {
      return item.nextAction || item.pursuit.nextRecommendedAction || 'Create the first workflow plan';
    }
    return item.nextAction || item.pursuit.nextRecommendedAction || item.pursuit.currentStateSummary || 'Review pursuit state';
  }

  pursuitContext(item: IPursuitListItem): string {
    return item.whatChanged || item.currentState || item.pursuit.whyItMatters || 'No linked operational movement recorded yet.';
  }

  pursuitEvidenceLine(item: IPursuitListItem): string {
    const parts = [
      `${item.decisionCards || item.needsRobert || 0} decision${(item.decisionCards || item.needsRobert || 0) === 1 ? '' : 's'}`,
      `${item.linkedEvidence || 0} evidence`,
      `${item.timelineItems || 0} timeline`,
    ];
    if (item.openLoops) {
      parts.push(`${item.openLoops} open loop${item.openLoops === 1 ? '' : 's'}`);
    }
    return parts.join(' / ');
  }

  openPursuit(item: IPursuitListItem): void {
    this.router.navigate(['/pursuits'], { queryParams: { selected: item.pursuit.id } });
  }

  openPursuitCard(card: IPursuitBriefCard): void {
    this.router.navigate(['/pursuits'], { queryParams: { selected: card.pursuitId } });
  }

  openPursuitDecision(card: IPursuitDashboardDecision): void {
    this.router.navigate(['/pursuits'], {
      queryParams: {
        selected: card.pursuit.id,
        decision: card.decision.id,
        evidence: card.decision.evidenceUri || null,
      },
    });
  }

  canResolveDashboardDecision(card: IPursuitDashboardDecision): boolean {
    if (card.decision.status !== 'pending') {
      return false;
    }
    switch (card.decision.decisionType) {
      case 'runtime_attempt_review':
      case 'pursuit_next_action':
      case 'pursuit_candidate_review':
      case 'pursuit_completion_review':
        return true;
      case 'approval':
      case 'proposal':
        return Boolean(card.decision.workflowId);
      default:
        return false;
    }
  }

  dashboardDecisionTitle(card: IPursuitDashboardDecision, approved: boolean): string {
    if (card.decision.decisionType === 'approval') {
      return approved
        ? 'Approve this workflow through its governed approval gate.'
        : 'Reject this workflow and keep it blocked for revision.';
    }
    if (card.decision.decisionType === 'proposal') {
      return approved
        ? 'Accept this workflow proposal and advance the linked workflow.'
        : 'Decline this proposal and return it for revision.';
    }
    if (card.decision.decisionType === 'pursuit_candidate_review') {
      return approved
        ? 'Accept this pursuit candidate and create its first governed workflow.'
        : 'Archive this candidate without creating workflow work.';
    }
    if (card.decision.decisionType === 'pursuit_next_action') {
      return approved
        ? 'Approve this next action and create a governed workflow item from it.'
        : 'Reject this proposed next action and record the decision in the pursuit audit trail.';
    }
    if (card.decision.decisionType === 'pursuit_completion_review') {
      return approved
        ? 'Mark this pursuit complete after the server verifies its completion evidence.'
        : 'Keep this pursuit active for more evidence or follow-up.';
    }
    return approved
      ? 'Approve this runtime recovery decision. HAI creates a governed recovery workflow instead of retrying OpenClaw directly.'
      : 'Reject the recovery proposal and keep the runtime attempt blocked without retrying.';
  }

  resolveDashboardDecision(card: IPursuitDashboardDecision, approved: boolean, event?: Event): void {
    event?.stopPropagation();
    if (!this.canResolveDashboardDecision(card) || this.resolvingDashboardDecisionId) {
      return;
    }
    if (card.decision.decisionType === 'pursuit_next_action') {
      this.resolvePursuitNextActionDecision(card, approved);
      return;
    }
    if (card.decision.decisionType === 'pursuit_candidate_review') {
      this.resolvePursuitCandidateDecision(card, approved);
      return;
    }
    if (card.decision.decisionType === 'approval') {
      this.resolveWorkflowApprovalDecision(card, approved);
      return;
    }
    if (card.decision.decisionType === 'proposal') {
      this.resolveWorkflowProposalDecision(card, approved);
      return;
    }
    if (card.decision.decisionType === 'pursuit_completion_review') {
      this.resolvePursuitCompletionDecision(card, approved);
      return;
    }
    this.resolveRuntimeDecision(card, approved);
  }

  private resolveWorkflowApprovalDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    if (!card.decision.workflowId) {
      return;
    }
    this.resolvingDashboardDecisionId = card.decision.id;
    this.workflows.resolveApproval(card.decision.workflowId, {
      approved,
      note: approved ? card.decision.yesConsequence : card.decision.noConsequence,
      actor: 'Robert',
    }).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success('Approval recorded', approved ? 'Workflow approved through the audited gate.' : 'Workflow rejected and blocked for review.');
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error('Approval blocked', error?.error?.error || 'The workflow approval could not be recorded.');
      },
    });
  }

  private resolveWorkflowProposalDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    const proposalID = card.decision.id.startsWith('proposal:') ? card.decision.id.substring('proposal:'.length) : '';
    if (!card.decision.workflowId || !proposalID) {
      this.notification.error('Proposal unavailable', 'The proposal ID or linked workflow is missing from this decision card.');
      return;
    }
    this.resolvingDashboardDecisionId = card.decision.id;
    this.workflows.resolveProposal(card.decision.workflowId, proposalID, {
      approved,
      status: approved ? 'approved' : 'rejected',
      selectedOption: approved ? card.decision.yesLabel : card.decision.noLabel,
      note: approved ? card.decision.yesConsequence : card.decision.noConsequence,
      actor: 'Robert',
    }).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success('Proposal recorded', approved ? 'Proposal accepted through the workflow audit trail.' : 'Proposal rejected for revision.');
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error('Proposal blocked', error?.error?.error || 'The workflow proposal could not be recorded.');
      },
    });
  }

  private resolvePursuitCompletionDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    this.resolvingDashboardDecisionId = card.decision.id;
    this.pursuits.resolveDecision(card.pursuit.id, {
      decisionId: card.decision.id,
      decisionType: card.decision.decisionType,
      approved,
      reason: card.decision.reason,
      note: approved
        ? card.decision.yesConsequence || 'Robert approved verified pursuit completion.'
        : card.decision.noConsequence || 'Robert kept the pursuit active after completion review.',
      evidenceUri: card.decision.evidenceUri,
      evidenceLabel: card.decision.evidenceLabel,
      actor: 'Robert',
    }).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success(
          approved ? 'Pursuit completed' : 'Pursuit kept active',
          approved
            ? 'Verified completion and the Robert decision were recorded in the audit trail.'
            : 'The completion review decision was recorded.'
        );
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error(
          approved ? 'Completion blocked' : 'Decision blocked',
          error?.error?.error || 'The completion review could not be recorded.'
        );
      },
    });
  }

  private resolvePursuitCandidateDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    this.resolvingDashboardDecisionId = card.decision.id;
    if (approved) {
      this.pursuits.acceptCandidate(card.pursuit.id, {
        requiresReview: card.decision.riskLevel === 'high',
        reviewReason: card.decision.reason,
      }).subscribe({
        next: () => {
          this.resolvingDashboardDecisionId = '';
          this.notification.success('Candidate accepted', 'HAI converted the candidate into governed pursuit work.');
          this.refreshPursuits();
        },
        error: (error) => {
          this.resolvingDashboardDecisionId = '';
          this.notification.error('Candidate blocked', error?.error?.error || 'HAI could not accept and plan this candidate.');
        },
      });
      return;
    }
    this.pursuits.archive(card.pursuit.id, true).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success('Candidate archived', 'The auto-created candidate was removed from active queues.');
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error('Archive blocked', error?.error?.error || 'HAI could not archive this candidate.');
      },
    });
  }

  private resolvePursuitNextActionDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    this.resolvingDashboardDecisionId = card.decision.id;
    this.pursuits.resolveDecision(card.pursuit.id, {
      decisionId: card.decision.id,
      decisionType: card.decision.decisionType,
      approved,
      reason: card.decision.reason,
      note: approved
        ? card.decision.yesConsequence || `Robert approved the proposed next action: ${card.decision.recommended}`
        : card.decision.noConsequence || `Robert rejected the proposed next action: ${card.decision.recommended}`,
      evidenceUri: card.decision.evidenceUri,
      evidenceLabel: card.decision.evidenceLabel,
      actor: 'Robert',
    }).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success(
          approved ? 'Workflow created' : 'Decision recorded',
          approved
            ? 'The approved pursuit decision became a governed workflow item.'
            : 'The pursuit decision is now resolved in the audit trail.'
        );
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error(
          approved ? 'Workflow creation blocked' : 'Decision blocked',
          error?.error?.error || 'The pursuit decision could not be recorded.'
        );
      },
    });
  }

  private resolveRuntimeDecision(card: IPursuitDashboardDecision, approved: boolean): void {
    this.resolvingDashboardDecisionId = card.decision.id;
    this.pursuits.resolveDecision(card.pursuit.id, {
      decisionId: card.decision.id,
      decisionType: card.decision.decisionType,
      approved,
      reason: card.decision.reason,
      note: approved
        ? card.decision.yesConsequence || card.decision.recommended
        : card.decision.noConsequence || 'Keep runtime attempt blocked until reviewed.',
      evidenceUri: card.decision.evidenceUri,
      evidenceLabel: card.decision.evidenceLabel,
      actor: 'Robert',
    }).subscribe({
      next: () => {
        this.resolvingDashboardDecisionId = '';
        this.notification.success(
          approved ? 'Recovery workflow created' : 'Runtime attempt kept blocked',
          approved
            ? 'HAI created a governed recovery workflow without retrying OpenClaw directly.'
            : 'The runtime attempt remains blocked and is removed from the Robert-only decision queue.'
        );
        this.refreshPursuits();
      },
      error: (error) => {
        this.resolvingDashboardDecisionId = '';
        this.notification.error(
          approved ? 'Recovery workflow blocked' : 'Decision blocked',
          error?.error?.error || 'The runtime recovery decision could not be recorded.'
        );
      },
    });
  }

  commandStatusColor(command?: IAssistantCommandResult): string {
    if (!command) {
      return 'default';
    }
    if (this.commandHasFailure(command)) {
      return 'error';
    }
    if (command.reviewRequired) {
      return 'warning';
    }
    if (command.pursuit?.executionQueued) {
      return 'processing';
    }
    return 'success';
  }

  commandStatusLabel(command?: IAssistantCommandResult): string {
    if (!command) {
      return 'no command yet';
    }
    const cycleStatus = typeof command.agentCycle?.status === 'string' ? command.agentCycle.status.toLowerCase() : '';
    if (cycleStatus === 'failed') {
      return 'failed';
    }
    if (cycleStatus === 'partial_failure' || command.actions?.some((action) => typeof action?.status === 'string' && action.status.toLowerCase() === 'failed')) {
      return 'partial failure';
    }
    if (command.pursuit?.executionQueued && !command.reviewRequired) {
      return 'workflow queued';
    }
    return command.reviewRequired ? 'review needed' : 'recorded';
  }

  private commandHasFailure(command: IAssistantCommandResult): boolean {
    const cycleStatus = typeof command.agentCycle?.status === 'string' ? command.agentCycle.status.toLowerCase() : '';
    return cycleStatus === 'failed' ||
      cycleStatus === 'partial_failure' ||
      command.actions?.some((action) => typeof action?.status === 'string' && action.status.toLowerCase() === 'failed') === true;
  }

  private isAssistantCommandResult(value: unknown): value is IAssistantCommandResult {
    if (!value || typeof value !== 'object') {
      return false;
    }
    const result = value as Partial<IAssistantCommandResult>;
    return typeof result.id === 'string' && result.id.length > 0 &&
      typeof result.createdAt === 'string' && Number.isFinite(Date.parse(result.createdAt)) &&
      typeof result.intent === 'string' &&
      typeof result.summary === 'string' && typeof result.nextAction === 'string' &&
      typeof result.safetySummary === 'string' && typeof result.reviewRequired === 'boolean' &&
      Array.isArray(result.actions) && result.actions.every((action) =>
        !!action && typeof action.name === 'string' && typeof action.status === 'string' && typeof action.summary === 'string'
      );
  }

  private isPursuitDashboard(value: unknown): value is IPursuitDashboard {
    if (!value || typeof value !== 'object') {
      return false;
    }
    const dashboard = value as Partial<IPursuitDashboard>;
    return !!dashboard.counts && typeof dashboard.counts === 'object' &&
      typeof dashboard.counts['active'] === 'number' && Number.isFinite(dashboard.counts['active']) && [
      dashboard.decisionQueue,
      dashboard.needsRobert,
      dashboard.vaReady,
      dashboard.systemReady,
      dashboard.blocked,
      dashboard.stale,
      dashboard.reviewDue,
      dashboard.planningNeeded,
      dashboard.recentlyChanged,
      dashboard.highRisk,
      dashboard.completionCandidates,
    ].every(Array.isArray);
  }

  private isMemoryDashboard(value: unknown): value is ICommandDashboard {
    if (!value || typeof value !== 'object') {
      return false;
    }
    const dashboard = value as Partial<ICommandDashboard>;
    return typeof dashboard.generatedAt === 'string' && Number.isFinite(Date.parse(dashboard.generatedAt)) &&
      typeof dashboard.conversationCount === 'number' && Number.isFinite(dashboard.conversationCount) &&
      typeof dashboard.insightCount === 'number' && Number.isFinite(dashboard.insightCount) && [
        dashboard.needsRobert,
        dashboard.delegateToVA,
        dashboard.openLoops,
        dashboard.contradictions,
        dashboard.recentDecisions,
        dashboard.projects,
        dashboard.recentArchives,
        dashboard.warnings,
      ].every(Array.isArray) &&
      (dashboard.sourceCorrections == null || Array.isArray(dashboard.sourceCorrections));
  }

  private isRuntimeOverview(value: unknown): value is { runtimes: IAgentRuntimeInfo[]; health: IAgentRuntimeHealth[] } {
    if (!value || typeof value !== 'object') {
      return false;
    }
    const overview = value as { runtimes?: unknown; health?: unknown };
    return Array.isArray(overview.runtimes) && overview.runtimes.every((runtime) =>
      !!runtime && typeof runtime.id === 'string' && typeof runtime.name === 'string'
    ) && Array.isArray(overview.health) && overview.health.every((item) =>
      !!item && typeof item.runtimeId === 'string' && typeof item.status === 'string'
    );
  }

  private isCommandLogList(value: unknown): value is IAssistantCommandResult[] {
    return Array.isArray(value) && value.every((item) =>
      !!item && typeof item === 'object' && typeof item.id === 'string' &&
      typeof item.summary === 'string' && Array.isArray(item.actions)
    );
  }

  commandEngineSummary(command?: IAssistantCommandResult): string {
    if (!command?.actions?.length) {
      return 'No assistant command has run yet.';
    }
    return command.actions.map((action) => `${action.name}: ${action.status}`).join(' | ');
  }

  search(): void {
    if (this.searching || this.searchForm.invalid) {
      return;
    }
    this.searching = true;
    this.searchError = '';
    this.memoryEngine
      .search(this.searchForm.value.query, this.searchForm.value.projectKey)
      .subscribe({
        next: (result) => {
          if (!result?.memory || !Array.isArray(result.facts) || !Array.isArray(result.memory.usedContext)) {
            this.searching = false;
            this.searchError = 'The memory search returned an invalid response. Previous results are retained.';
            return;
          }
          this.searchResult = result;
          this.searching = false;
          this.searchError = '';
        },
        error: (error) => {
          this.searching = false;
          this.searchError = error?.error?.error || 'Memory search failed. Previous results are retained.';
          this.notification.error('Search failed', error?.error?.error || 'Memory search failed.');
        },
      });
  }

  openSource(sourceUri?: string): void {
    const uri = sourceUri?.trim();
    if (!uri) {
      this.notification.warning('Source unavailable', 'This record does not include a source link.');
      return;
    }
    const href = safeWebSourceHref(uri, true);
    if (href) {
      window.open(href, '_blank', 'noopener,noreferrer');
      return;
    }
    this.notification.warning('Source link blocked', 'HAI only opens valid HTTPS source links without embedded credentials.');
  }

  deleteArchive(id: string, title: string): void {
    if (!id || this.deletingArchiveIds.has(id)) {
      return;
    }
    if (!window.confirm(`Delete the encrypted archive and extracted facts for "${title}"?`)) {
      return;
    }
    this.deletingArchiveIds.add(id);
    this.memoryEngine.deleteConversation(id).pipe(finalize(() => this.deletingArchiveIds.delete(id))).subscribe({
      next: () => {
        if (this.dashboard) {
          this.dashboard = {
            ...this.dashboard,
            recentArchives: this.dashboard.recentArchives.filter((archive) => archive.id !== id),
          };
        }
        this.notification.success('Archive deleted', 'The raw archive and extracted facts were removed.');
        this.refresh();
      },
      error: (error) => this.notification.error('Delete failed', error?.error?.error || 'The archive could not be deleted.'),
    });
  }

  openWorkflow(): void {
    this.router.navigate(['/workflow-engine']);
  }

  openPursuits(): void {
    this.router.navigate(['/pursuits']);
  }

  openAmbientBrain(): void {
    this.router.navigate(['/ambient-brain']);
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }
}

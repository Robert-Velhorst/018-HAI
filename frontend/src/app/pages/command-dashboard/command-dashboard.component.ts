import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
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
  standalone: false,
  selector: 'app-command-dashboard',
  templateUrl: './command-dashboard.component.html',
  styleUrls: ['./command-dashboard.component.scss'],
})
export class CommandDashboardComponent implements OnInit {
  private readonly openClawArchiveMaxBytes = 750 * 1024 * 1024;
  dashboard?: ICommandDashboard;
  pursuitDashboard?: IPursuitDashboard;
  pursuitBrief?: IPursuitBrief;
  searchResult?: IMemoryEngineSearchResult;
  loading = false;
  pursuitsLoading = false;
  searching = false;
  runtimeLoading = false;
  commandLoading = '';
  runtimes: IAgentRuntimeInfo[] = [];
  runtimeHealth: Record<string, IAgentRuntimeHealth> = {};
  runtimeSkills: Record<string, IAgentRuntimeSkill[]> = {};
  runtimeSkillsLoading: Record<string, boolean> = {};
  openClawEcosystemPath = '';
  openClawConfigLoading = false;
  openClawRefreshLoading = false;
  openClawUploadLoading = false;
  openClawUploadFileName = '';
  resolvingDashboardDecisionId = '';
  selectedRuntimeSurface?: IAgentRuntimeEcosystemSurface;
  commandLogs: IAssistantCommandResult[] = [];
  lastCommand?: IAssistantCommandResult;

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
      description: 'Run your owner-scoped source, workflow, ambient, pursuit, and learning pass without starting global workers.',
      metric: 'Personal operations',
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

  refreshRuntimes(): void {
    this.runtimeLoading = true;
    this.agentRuntimes.overview().subscribe({
      next: ({ runtimes, health }) => {
        this.runtimes = runtimes;
        this.runtimeHealth = health.reduce(
          (result, item) => ({ ...result, [item.runtimeId]: item }),
          {} as Record<string, IAgentRuntimeHealth>
        );
        const openClaw = runtimes.find((runtime) => runtime.id === 'openclaw');
        this.openClawEcosystemPath = openClaw?.ecosystemPath || '';
        this.runtimeLoading = false;
      },
      error: (error) => {
        this.runtimeLoading = false;
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
    if (!runtime?.id) {
      return;
    }
    this.runtimeSkillsLoading[runtime.id] = true;
    this.agentRuntimes.skills(runtime.id).subscribe({
      next: (skills) => {
        this.runtimeSkills[runtime.id] = skills || [];
        this.runtimeSkillsLoading[runtime.id] = false;
      },
      error: (error) => {
        this.runtimeSkillsLoading[runtime.id] = false;
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
    this.refreshPursuits();
    this.memoryEngine.dashboard().subscribe({
      next: (dashboard) => {
        this.dashboard = dashboard;
        this.loading = false;
      },
      error: (error) => {
        this.loading = false;
        this.notification.error(
          'Memory engine unavailable',
          error?.error?.error || 'Failed to load the command dashboard.'
        );
      },
    });
  }

  setOpenClawEcosystemPath(runtime: IAgentRuntimeInfo): void {
    const path = this.openClawEcosystemPath?.trim();
    if (!path) {
      this.notification.error('OpenClaw ecosystem path is required', 'Enter the local path or zip file location.');
      return;
    }
    this.openClawConfigLoading = true;
    this.agentRuntimes.setOpenClawEcosystemPath(path).subscribe({
      next: (runtime) => {
        this.openClawConfigLoading = false;
        const index = this.runtimes.findIndex((item) => item.id === runtime.id);
        if (index >= 0) {
          this.runtimes[index] = runtime;
        }
        this.openClawEcosystemPath = runtime.ecosystemPath || path;
        this.notification.success('OpenClaw ecosystem path updated', `Configured at ${this.openClawEcosystemPath}`);
      },
      error: (error) => {
        this.openClawConfigLoading = false;
        this.notification.error(
          'OpenClaw ecosystem config failed',
          error?.error?.error || 'The backend rejected the configured OpenClaw ecosystem path.'
        );
      },
    });
  }

  refreshOpenClawEcosystem(runtime: IAgentRuntimeInfo): void {
    if (runtime.id !== 'openclaw') {
      return;
    }
    this.openClawRefreshLoading = true;
    this.agentRuntimes.refreshOpenClawEcosystem().subscribe({
      next: (updatedRuntime) => {
        this.openClawRefreshLoading = false;
        const index = this.runtimes.findIndex((item) => item.id === updatedRuntime.id);
        if (index >= 0) {
          this.runtimes[index] = updatedRuntime;
          this.openClawEcosystemPath = updatedRuntime.ecosystemPath || '';
        }
        this.notification.success('OpenClaw ecosystem refresh queued', 'Re-scanned the selected OpenClaw source path.');
      },
      error: (error) => {
        this.openClawRefreshLoading = false;
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
    this.openClawUploadFileName = file.name;
    this.uploadOpenClawEcosystem(file);
    if (target) {
      target.value = '';
    }
  }

  uploadOpenClawEcosystem(file: File): void {
    if (!file || !file.name.toLowerCase().endsWith('.zip')) {
      this.notification.error('Invalid OpenClaw archive', 'Upload a .zip archive exported from openclaw-main.');
      return;
    }
    if (file.size > this.openClawArchiveMaxBytes) {
      this.notification.error(
        'OpenClaw archive is too large',
        'The local gateway accepts OpenClaw ecosystem archives up to 750 MB.'
      );
      this.openClawUploadFileName = '';
      return;
    }
    this.openClawUploadLoading = true;
    this.agentRuntimes.uploadOpenClawEcosystem(file).subscribe({
      next: (runtime) => {
        this.openClawUploadLoading = false;
        this.openClawUploadFileName = '';
        const index = this.runtimes.findIndex((item) => item.id === runtime.id);
        if (index >= 0) {
          this.runtimes[index] = runtime;
        }
        this.openClawEcosystemPath = runtime.ecosystemPath || runtime.name;
        this.notification.success(
          'OpenClaw ecosystem uploaded',
          'The uploaded OpenClaw archive was indexed and the runtime surfaces were refreshed.'
        );
      },
      error: (error) => {
        this.openClawUploadLoading = false;
        this.notification.error(
          'OpenClaw ecosystem upload failed',
          error?.error?.error || 'The uploaded archive could not be indexed.'
        );
      },
    });
  }

  refreshPursuits(): void {
    this.pursuitsLoading = true;
    this.pursuits.dashboard().subscribe({
      next: (dashboard) => {
        this.pursuitDashboard = dashboard;
        this.pursuitsLoading = false;
      },
      error: () => {
        this.pursuitsLoading = false;
        this.pursuitDashboard = undefined;
      },
    });
    this.pursuits.brief().subscribe({
      next: (brief) => {
        this.pursuitBrief = brief;
      },
      error: () => {
        this.pursuitBrief = undefined;
      },
    });
  }

  loadCommandLogs(): void {
    this.assistantCommands.logs().subscribe({
      next: (logs) => {
        this.commandLogs = logs || [];
        this.lastCommand = this.commandLogs[0] || this.lastCommand;
      },
      error: () => {
        this.commandLogs = [];
      },
    });
  }

  runDashboardAction(action: DashboardAction): void {
    if (action.route) {
      this.router.navigate([action.route]);
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
          this.lastCommand = result;
          if (result.agentCycle?.pursuitBrief) {
            this.pursuitBrief = result.agentCycle.pursuitBrief;
          }
          this.commandLogs = [result, ...this.commandLogs].slice(0, 50);
          this.notification.success(action.title, result.nextAction || result.summary);
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
        return `${this.dashboard.openLoops.length} loops`;
      case 'run-cycle':
        return `${this.dashboard.needsRobert.length} need Robert`;
      case 'run-safe':
        return `${this.runtimes.length} runtimes`;
      case 'manage-pursuits':
        return `${this.activePursuitCount()} active`;
      default:
        return `${this.dashboard.insightCount} facts`;
    }
  }

  activePursuitCount(): number {
    return Number(this.pursuitDashboard?.counts?.['active'] || 0);
  }

  pursuitDecisionCount(): number {
    return this.pursuitDashboard?.needsRobert?.length || 0;
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
    if (command.reviewRequired) {
      return 'warning';
    }
    if (command.agentCycle?.status === 'partial_failure' || command.agentCycle?.status === 'failed') {
      return 'error';
    }
    return 'success';
  }

  commandEngineSummary(command?: IAssistantCommandResult): string {
    if (!command?.actions?.length) {
      return 'No assistant command has run yet.';
    }
    return command.actions.map((action) => `${action.name}: ${action.status}`).join(' | ');
  }

  search(): void {
    if (this.searchForm.invalid) {
      return;
    }
    this.searching = true;
    this.memoryEngine
      .search(this.searchForm.value.query, this.searchForm.value.projectKey)
      .subscribe({
        next: (result) => {
          this.searchResult = result;
          this.searching = false;
        },
        error: (error) => {
          this.searching = false;
          this.notification.error('Search failed', error?.error?.error || 'Memory search failed.');
        },
      });
  }

  openSource(sourceUri?: string): void {
    if (sourceUri) {
      window.open(sourceUri, '_blank', 'noopener');
    }
  }

  deleteArchive(id: string, title: string): void {
    if (!window.confirm(`Delete the encrypted archive and extracted facts for "${title}"?`)) {
      return;
    }
    this.memoryEngine.deleteConversation(id).subscribe({
      next: () => {
        this.notification.success('Archive deleted', 'The raw archive and extracted facts were removed.');
        this.refresh();
      },
      error: () => this.notification.error('Delete failed', 'The archive could not be deleted.'),
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

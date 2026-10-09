import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { ThemeService } from '../../services/theme.service';
import { Subscription } from 'rxjs';
import {
  ILLMFallbackOption,
  ILLMModel,
  ILLMModelMaintenanceResult,
  ILLMPolicy,
  ILLMProvider,
  ILLMProviderProbe,
  ILLMRouteDecision,
  ILLMGenerationResult,
} from '../../models/llm-policy.model.interface';
import { LLM_POLICY_SERVICE_TOKEN } from '../../services/llm-policy/llm-policy.service.token';
import { ILLMPolicyService } from '../../services/llm-policy.service.interface';

interface TierModelGroup {
  tier: string;
  models: ILLMModel[];
}

interface TierCatalogEntry {
  providerName: string;
  model: ILLMModel;
}

interface TierCatalogGroup {
  tier: string;
  entries: TierCatalogEntry[];
}

interface PolicyActionCard {
  title: string;
  detail: string;
  icon: string;
  metric: string;
  secondaryMetric: string;
  context: string;
  tone: 'blue' | 'green' | 'gold';
  action: 'route' | 'probe' | 'logs' | 'providers' | 'catalog' | 'automations';
}

const MODULE_ID = 'llm-policy';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-llm-policy',
    templateUrl: './llm-policy.component.html',
    styleUrls: ['./llm-policy.component.scss'],
    standalone: false
})
export class LLMPolicyComponent implements OnInit, OnDestroy {
  policy?: ILLMPolicy;
  probes: ILLMProviderProbe[] = [];
  maintenance: ILLMModelMaintenanceResult[] = [];
  logs: ILLMRouteDecision[] = [];
  generations: ILLMGenerationResult[] = [];
  maintenanceUnavailable = false;
  logsUnavailable = false;
  generationsUnavailable = false;
  decision?: ILLMRouteDecision;
  policyActions: PolicyActionCard[] = [];
  catalogGroups: TierCatalogGroup[] = [];
  activeTier = 'local';
  catalogFilter = '';
  loading = false;
  probing = false;
  maintaining = false;
  routing = false;
  themeMode = this.themeService.mode();
  private providerTierGroups = new Map<string, TierModelGroup[]>();
  private providerPreviews = new Map<string, ILLMModel[]>();
  private providerHiddenCounts = new Map<string, number>();
  private themeSubscription?: Subscription;
  routeForm: FormGroup = this.fb.group({
    task: [
      'Fix a Go API bug and explain why the previous model failed validation.',
      [Validators.required],
    ],
    validationPassed: [true],
    previousModelId: [''],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(LLM_POLICY_SERVICE_TOKEN)
    private llmPolicyService: ILLMPolicyService,
    private notification: NzNotificationService,
    private router: Router,
    private themeService: ThemeService,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  ngOnInit(): void {
    this.themeSubscription = this.themeService.changes$?.subscribe((mode) => this.themeMode = mode);
    this.refresh();
  }

  ngOnDestroy(): void {
    this.themeSubscription?.unsubscribe();
  }

  get isAdvanced(): boolean {
    return this.viewPreferences.get(MODULE_ID).mode === 'advanced';
  }

  get providersNeedingAttention(): ILLMProvider[] {
    return (this.policy?.providers ?? []).filter((provider) => this.providerAttentionReason(provider) !== '');
  }

  get maintenanceBlockers(): ILLMModelMaintenanceResult[] {
    return this.maintenance.filter((record) =>
      record.blocksExecution || record.status === 'failed'
    );
  }

  providerHealthSummary(policy: ILLMPolicy): string {
    const enabled = policy.providers.filter((provider) => provider.enabled);
    const configured = enabled.filter((provider) => provider.configured).length;
    const live = enabled.filter((provider) => this.latestProbe(provider.id)?.live).length;
    return `${enabled.length} enabled · ${configured} configured · ${live} live probes`;
  }

  providerProbeStatus(providerId: string): string {
    const probe = this.latestProbe(providerId);
    return probe ? (probe.live ? 'live' : probe.status) : 'not checked';
  }

  providerAttentionReason(provider: ILLMProvider): string {
    if (!provider.enabled) return '';
    if (!provider.configured) return provider.readinessReason || 'Endpoint is not configured.';
    const probe = this.latestProbe(provider.id);
    if (probe && !probe.live) return probe.reason || `Latest probe status: ${probe.status}.`;
    return this.strictProviderReadinessReason(provider);
  }

  resetView(): void {
    this.viewPreferences.reset(MODULE_ID);
    document.body.classList.remove('hai-view-advanced');
    void this.router.navigate(['/llm-policy'], {
      queryParams: { mode: 'basic' },
      replaceUrl: true,
    });
  }

  openAdvancedSection(sectionId: string): void {
    this.viewPreferences.setMode(MODULE_ID, 'advanced');
    this.viewPreferences.setSection(MODULE_ID, sectionId, true);
    document.body.classList.add('hai-view-advanced');
    window.setTimeout(() => this.scrollToSection(sectionId));
  }

  private latestProbe(providerId: string): ILLMProviderProbe | undefined {
    return this.probes.find((probe) => probe.providerId === providerId);
  }

  refresh(): void {
    this.loading = true;
    this.llmPolicyService.getPolicy().subscribe({
      next: (policy) => {
        this.setPolicy(policy);
        this.loading = false;
      },
      error: () => {
        this.loading = false;
        this.notification.error('Error', 'Failed to load LLM routing policy.');
      },
    });
    this.loadLogs();
    this.loadGenerationHistory();
    this.loadProbeHistory();
    this.loadModelMaintenanceHistory();
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle();
  }

  themeLabel(): string {
    return this.themeMode === 'dark' ? 'Dark mode' : 'Light mode';
  }

  themeIcon(): string {
    return this.themeService.icon();
  }

  probeProviders(): void {
    this.probing = true;
    this.llmPolicyService.probeProviders().subscribe({
      next: (probes) => {
        this.probes = probes;
        this.rebuildActionCards();
        this.probing = false;
      },
      error: () => {
        this.probes = [];
        this.probing = false;
        this.notification.error('Error', 'Failed to probe configured LLM providers.');
      },
    });
  }

  private loadProbeHistory(): void {
    this.llmPolicyService.getProbeHistory().subscribe({
      next: (probes) => {
        this.probes = this.latestProbeByProvider(probes || []);
        this.rebuildActionCards();
      },
      error: () => {
        // A missing history table must not hide routing policy or live probes.
        this.rebuildActionCards();
      },
    });
  }

  loadModelMaintenanceHistory(): void {
    this.llmPolicyService.getModelMaintenanceHistory().subscribe({
      next: (records) => {
        this.maintenance = records || [];
        this.maintenanceUnavailable = false;
      },
      error: () => (this.maintenanceUnavailable = true),
    });
  }

  runDueModelMaintenance(): void {
    if (this.maintaining) {
      return;
    }
    this.maintaining = true;
    this.llmPolicyService.runDueModelMaintenance().subscribe({
      next: (run) => {
        this.maintaining = false;
        this.maintenance = run.results || this.maintenance;
        this.loadModelMaintenanceHistory();
        const summary =
          `${run.checked} checked from ${run.eligible} eligible models; ` +
          `cloud catalog: ${run.providerManaged}; health-only runtime checks: ${run.healthOnly ?? 0}; ` +
          `${run.reused} cached checks reused, ` +
          `${run.updated} updated, ${run.inProgress} already in progress, ${run.failed} failed`;
        if (run.cancelled) {
          this.notification.warning(
            'Model maintenance stopped',
            `${summary}. Any interrupted update remains blocked until a successful verification.`
          );
        } else if (run.failed > 0 || run.inProgress > 0) {
          this.notification.warning('Model maintenance needs review', summary);
        } else {
          this.notification.success('Daily model checks complete', summary);
        }
      },
      error: () => {
        this.maintaining = false;
        this.notification.error(
          'Model maintenance failed',
          'HAI could not complete the due model checks. Routing retains its existing safety gates.'
        );
      },
    });
  }

  private latestProbeByProvider(probes: ILLMProviderProbe[]): ILLMProviderProbe[] {
    const latest = new Map<string, ILLMProviderProbe>();
    probes.forEach((probe) => {
      const current = latest.get(probe.providerId);
      if (!current || new Date(probe.checkedAt).getTime() > new Date(current.checkedAt).getTime()) {
        latest.set(probe.providerId, probe);
      }
    });
    return Array.from(latest.values()).sort((left, right) => left.providerName.localeCompare(right.providerName));
  }

  routeTask(): void {
    if (this.routeForm.invalid) {
      Object.values(this.routeForm.controls).forEach((control) => {
        control.markAsDirty();
        control.updateValueAndValidity();
      });
      return;
    }

    this.routing = true;
    const previousModelId = this.routeForm.value.previousModelId || undefined;
    this.llmPolicyService
      .routeTask({
        task: this.routeForm.value.task,
        validationPassed: this.routeForm.value.validationPassed,
        previousModelId,
      })
      .subscribe({
        next: (decision) => {
          this.routing = false;
          this.decision = decision;
          this.rebuildActionCards();
          this.loadLogs();
        },
        error: () => {
          this.routing = false;
          this.notification.error('Error', 'Failed to route this task.');
        },
      });
  }

  loadLogs(): void {
    this.llmPolicyService.getLogs().subscribe({
      next: (logs) => {
        this.logs = logs;
        this.logsUnavailable = false;
        this.rebuildActionCards();
      },
      error: () => {
        this.logsUnavailable = true;
        this.rebuildActionCards();
      },
    });
  }

  loadGenerationHistory(): void {
    this.llmPolicyService.getGenerationHistory().subscribe({
      next: (records) => {
        this.generations = records || [];
        this.generationsUnavailable = false;
        this.rebuildActionCards();
      },
      error: () => {
        this.generationsUnavailable = true;
        this.rebuildActionCards();
      },
    });
  }

  private setPolicy(policy: ILLMPolicy): void {
    this.policy = policy;
    this.catalogGroups = this.buildCatalogByTier(policy);
    this.providerTierGroups = new Map(
      policy.providers.map((provider) => [provider.id, this.buildProviderTierGroups(provider)])
    );
    this.providerPreviews = new Map(
      policy.providers.map((provider) => [provider.id, provider.models.slice(0, 8)])
    );
    this.providerHiddenCounts = new Map(
      policy.providers.map((provider) => [
        provider.id,
        Math.max(0, provider.models.length - (this.providerPreviews.get(provider.id)?.length || 0)),
      ])
    );
    if (!this.tierOrder(policy).includes(this.activeTier)) {
      this.activeTier = this.tierOrder(policy)[0] || 'local';
    }
    this.rebuildActionCards();
  }

  private rebuildActionCards(): void {
    if (!this.policy) {
      this.policyActions = [];
      return;
    }
    const policy = this.policy;
    const routeMetric = this.decision
      ? this.decision.selectedModelName || 'blocked'
      : 'ready';
    const routeSecondaryMetric = this.decision
      ? this.decision.selectedModelName
        ? this.tierLabel(this.decision.tier)
        : 'No capable model under policy'
      : 'No route selected yet';
    const latestLogMetric = this.generationsUnavailable
      ? 'Generation history unavailable'
      : this.logsUnavailable
        ? 'Routing history unavailable'
        : this.generations[0]
      ? `${this.validationLabel(this.generations[0].validationStatus)} / ${this.generations[0].modelName || this.generations[0].modelId}`
      : this.logs[0]?.selectedModelName || (this.logs.length ? 'Blocked route recorded' : 'No recent decisions');
    this.policyActions = [
      {
        title: 'Route task',
        detail: 'Open the routing cockpit and classify a task.',
        icon: 'branches',
        metric: routeMetric,
        secondaryMetric: routeSecondaryMetric,
        context:
          'Classifies the task, estimates difficulty, skips weak or blocked models, and logs the cheapest capable route.',
        tone: 'blue',
        action: 'route',
      },
      {
        title: 'Probe providers',
        detail: 'Check configured runtime endpoints.',
        icon: 'api',
        metric: this.probes.length ? `${this.liveProbeCount()} live` : `${this.configuredProviderCount(policy)} configured`,
        secondaryMetric: `${policy.providers.length} providers tracked`,
        context:
          'Checks configured endpoints without changing the routing policy, so unavailable providers do not get selected blindly.',
        tone: 'green',
        action: 'probe',
      },
      {
        title: 'Provider inventory',
        detail: 'Inspect readiness, quota, budget, and token use.',
        icon: 'deployment-unit',
        metric: `${this.enabledProviderCount(policy)} enabled`,
        secondaryMetric: `${this.configuredProviderSummary(policy)} endpoints configured`,
        context:
          'Shows provider readiness, quota, budget, token counters, endpoint state, and model availability.',
        tone: 'blue',
        action: 'providers',
      },
      {
        title: 'Model catalog',
        detail: 'Review every model grouped by the 7 tiers.',
        icon: 'database',
        metric: `${this.totalModelCount(policy)} models`,
        secondaryMetric: `${this.tierOrder(policy).length} routing tiers`,
        context:
          'Groups all available models into the seven-tier routing order so stronger or paid models stay gated.',
        tone: 'green',
        action: 'catalog',
      },
      {
        title: 'Review routing log',
        detail: 'Inspect recent choices and fallback paths.',
        icon: 'history',
        metric: this.generationsUnavailable ? 'Unavailable' : `${this.generations.length} outcomes`,
        secondaryMetric: latestLogMetric,
        context:
          'Opens the trace of selected models, skipped options, estimated cost, validation pressure, and fallback history.',
        tone: 'gold',
        action: 'logs',
      },
      {
        title: 'Automations',
        detail: 'Return to automation registry.',
        icon: 'appstore',
        metric: 'open',
        secondaryMetric: 'Automation registry',
        context: 'Returns to the broader automation registry for runtime and workflow controls.',
        tone: 'blue',
        action: 'automations',
      },
    ];
  }

  runAction(action: PolicyActionCard['action']): void {
    switch (action) {
      case 'route':
        this.focusRouteInput();
        break;
      case 'probe':
        this.probeProviders();
        break;
      case 'logs':
        this.loadLogs();
        this.loadGenerationHistory();
        this.openAdvancedSection('routing-audit');
        break;
      case 'providers':
        this.openAdvancedSection('provider-inventory');
        break;
      case 'catalog':
        this.openAdvancedSection('model-catalog');
        break;
      case 'automations':
        this.goHome();
        break;
    }
  }

  providerStatus(provider: ILLMProvider): string {
    if (!provider.enabled) {
      return 'disabled';
    }
    if (!provider.configured) {
      return provider.readinessStatus || 'not configured';
    }
    const strictReason = this.strictProviderReadinessReason(provider);
    if (strictReason) {
      return 'probe required';
    }
    if (provider.paid) {
      return 'paid';
    }
    if (provider.local) {
      return 'local';
    }
    return 'free quota';
  }

  providerColor(provider: ILLMProvider): string {
    if (!provider.enabled) {
      return 'default';
    }
    if (!provider.configured) {
      return 'orange';
    }
    if (this.strictProviderReadinessReason(provider)) {
      return 'orange';
    }
    if (provider.paid) {
      return 'red';
    }
    if (provider.local) {
      return 'green';
    }
    return 'blue';
  }

  providerTone(provider: ILLMProvider): string {
    if (!provider.enabled) {
      return 'disabled';
    }
    if (!provider.configured) {
      return 'watch';
    }
    if (this.strictProviderReadinessReason(provider)) {
      return 'watch';
    }
    if (provider.paid) {
      return 'danger';
    }
    if (provider.local) {
      return 'good';
    }
    return 'neutral';
  }

  providerKind(provider: ILLMProvider): string {
    if (provider.local) {
      return 'local runtime';
    }
    if (provider.paid) {
      return 'paid API';
    }
    return 'free quota';
  }

  probeColor(probe: ILLMProviderProbe): string {
    if (probe.live) {
      return 'green';
    }
    if (probe.requiresReview) {
      return 'orange';
    }
    if (probe.status === 'disabled' || probe.status === 'not_configured') {
      return 'default';
    }
    return 'red';
  }

  maintenanceColor(record: ILLMModelMaintenanceResult): string {
    if (record.status === 'updated' || record.status === 'installed') {
      return 'green';
    }
    if (record.status === 'current' || record.status === 'provider_managed') {
      return 'blue';
    }
    if (record.status === 'operator_managed') {
      return 'orange';
    }
    if (record.status === 'not_enforced') {
      return 'default';
    }
    return 'red';
  }

  maintenanceScheduleText(record: ILLMModelMaintenanceResult): string {
    if (!record.nextCheckDueAt) {
      return 'Next check time was not provided by the maintenance service.';
    }
    const due = new Date(record.nextCheckDueAt);
    if (Number.isNaN(due.getTime())) {
      return 'Next check time was not provided in a valid format.';
    }
    const dueText = due.toLocaleString();
    return due.getTime() <= Date.now()
      ? `Next check is overdue (due ${dueText}).`
      : `Next check due ${dueText}.`;
  }

  tierColor(tier?: string): string {
    switch (tier) {
      case 'local':
        return 'lime';
      case 'free':
        return 'green';
      case 'cheap':
        return 'cyan';
      case 'acceptable':
        return 'blue';
      case 'high':
        return 'gold';
      case 'premium':
        return 'purple';
      case 'expensive':
        return 'red';
      default:
        return 'default';
    }
  }

  tierLabel(tier?: string): string {
    if (!tier) {
      return 'Blocked';
    }
    switch (tier) {
      case 'local':
        return '1. Local';
      case 'free':
        return '2. Free';
      case 'cheap':
        return '3. Cheap';
      case 'acceptable':
        return '4. Acceptable';
      case 'high':
        return '5. High';
      case 'premium':
        return '6. Premium';
      case 'expensive':
        return '7. Expensive';
      default:
        return tier || 'Unknown';
    }
  }

  tierDescription(tier?: string): string {
    switch (tier) {
      case 'local':
        return '$0 private/local runtime first';
      case 'free':
        return 'free cloud quota when configured';
      case 'cheap':
        return 'paid only after approval';
      case 'acceptable':
        return 'stronger paid fallback';
      case 'high':
        return 'high reasoning fallback';
      case 'premium':
        return 'best available paid fallback';
      case 'expensive':
        return 'last resort, approval-gated';
      default:
        return 'uncategorized model tier';
    }
  }

  setActiveTier(tier: string): void {
    this.activeTier = tier;
  }

  isActiveTier(tier: string): boolean {
    return this.activeTier === tier;
  }

  catalogFilterChanged(event: Event): void {
    this.catalogFilter = (event.target as HTMLInputElement).value || '';
  }

  showProviderModels(provider: ILLMProvider): void {
    const firstModel = provider.models[0];
    if (firstModel) {
      this.activeTier = firstModel.tier;
    }
    this.catalogFilter = provider.name;
    this.openAdvancedSection('model-catalog');
  }

  showModel(model: ILLMModel): void {
    this.activeTier = model.tier;
    this.catalogFilter = model.name;
    this.openAdvancedSection('model-catalog');
  }

  tierModelCount(tier: string): number {
    const group = this.catalogGroups.find((item) => item.tier === tier);
    return group?.entries.length || 0;
  }

  tierProviderCount(tier: string): number {
    const group = this.catalogGroups.find((item) => item.tier === tier);
    if (!group) {
      return 0;
    }
    return new Set(group.entries.map((entry) => entry.providerName)).size;
  }

  activeTierEntries(): TierCatalogEntry[] {
    const group = this.catalogGroups.find((item) => item.tier === this.activeTier);
    const entries = group?.entries || [];
    const query = this.catalogFilter.trim().toLowerCase();
    if (!query) {
      return entries;
    }
    return entries.filter((entry) => {
      const searchable = [
        entry.providerName,
        entry.model.id,
        entry.model.name,
        entry.model.tier,
        entry.model.maxReasoning,
        ...(entry.model.capabilities || []),
      ]
        .join(' ')
        .toLowerCase();
      return searchable.includes(query);
    });
  }

  activeTierSummary(): string {
    return `${this.tierModelCount(this.activeTier)} models across ${this.tierProviderCount(
      this.activeTier
    )} providers`;
  }

  modelCostText(model: ILLMModel): string {
    return model.estimatedCostEur ? `EUR ${this.formatMoney(model.estimatedCostEur)}` : 'EUR 0';
  }

  modelPricingText(model: ILLMModel): string {
    const input = model.inputCostPerMillionTokensEur || 0;
    const output = model.outputCostPerMillionTokensEur || 0;
    if (!input && !output) {
      return 'no token price';
    }
    return `EUR ${this.formatMoney(input)} in / ${this.formatMoney(output)} out`;
  }

  modelUsageText(model: ILLMModel): string {
    return `${this.formatNumber(model.inputTokensUsed || 0)} in / ${this.formatNumber(
      model.outputTokensUsed || 0
    )} out`;
  }

  modelBudgetText(model: ILLMModel): string {
    return `EUR ${this.formatMoney(model.budgetUsedEur || 0)}`;
  }

  modelCostTooltip(model: ILLMModel): string {
    return [
      `Estimated route cost: ${this.modelCostText(model)}`,
      `Token pricing: ${this.modelPricingText(model)} per 1M tokens`,
      `Used budget: ${this.modelBudgetText(model)}`,
      `Input tokens used: ${this.formatNumber(model.inputTokensUsed || 0)}`,
      `Output tokens used: ${this.formatNumber(model.outputTokensUsed || 0)}`,
      `Pricing source: ${model.pricingSource || 'not configured'}`,
    ].join(' | ');
  }

  routeCostTooltip(decision: ILLMRouteDecision): string {
    return [
      `Estimated route cost: EUR ${this.formatMoney(decision.estimatedCostEur)}`,
      `Estimated input tokens: ${this.formatNumber(decision.estimatedInputTokens || 0)}`,
      `Estimated output tokens: ${this.formatNumber(decision.estimatedOutputTokens || 0)}`,
      `Pricing source: ${decision.pricingSource || 'not configured'}`,
    ].join(' | ');
  }

  routeTokenText(decision: ILLMRouteDecision): string {
    return `${this.formatNumber(decision.estimatedInputTokens || 0)} in / ${this.formatNumber(
      decision.estimatedOutputTokens || 0
    )} out`;
  }

  fallbackCostTooltip(item: ILLMFallbackOption): string {
    return [
      `Estimated route cost: EUR ${this.formatMoney(item.estimatedCostEur)}`,
      `Estimated input tokens: ${this.formatNumber(item.estimatedInputTokens || 0)}`,
      `Estimated output tokens: ${this.formatNumber(item.estimatedOutputTokens || 0)}`,
      item.requiresApproval ? 'Manual approval required' : 'Allowed by current policy',
    ].join(' | ');
  }

  approvalLabel(model: ILLMModel): string {
    return model.requiresApproval ? 'approval required' : 'allowed by policy';
  }

  modelStatusLabel(model: ILLMModel): string {
    return model.enabled ? 'enabled' : 'disabled';
  }

  modelsByTier(provider: ILLMProvider): TierModelGroup[] {
    return this.providerTierGroups.get(provider.id) || [];
  }

  private buildProviderTierGroups(provider: ILLMProvider): TierModelGroup[] {
    return this.tierOrder()
      .map((tier) => ({
        tier,
        models: provider.models.filter((model) => model.tier === tier),
      }))
      .filter((group) => group.models.length > 0);
  }

  private buildCatalogByTier(policy: ILLMPolicy): TierCatalogGroup[] {
    return this.tierOrder(policy).map((tier) => ({
      tier,
      entries: policy.providers.flatMap((provider) =>
        provider.models
          .filter((model) => model.tier === tier)
          .map((model) => ({ providerName: provider.name, model }))
      ),
    }));
  }

  providerModelCount(provider: ILLMProvider): number {
    return provider.models.length;
  }

  enabledModelCount(provider: ILLMProvider): number {
    return provider.models.filter((model) => model.enabled).length;
  }

  quotaText(provider: ILLMProvider): string {
    return provider.quotaRemaining < 0 ? 'unlimited local' : this.formatNumber(provider.quotaRemaining);
  }

  providerBudgetPercent(provider: ILLMProvider): number {
    if (!provider.dailyBudgetEur) {
      return 0;
    }
    return Math.min(100, Math.round((provider.budgetUsedEur / provider.dailyBudgetEur) * 100));
  }

  providerModelPreview(provider: ILLMProvider): ILLMModel[] {
    return this.providerPreviews.get(provider.id) || [];
  }

  hiddenModelCount(provider: ILLMProvider): number {
    return this.providerHiddenCounts.get(provider.id) || 0;
  }

  modelCapabilityText(model: ILLMModel): string {
    return model.capabilities?.length ? model.capabilities.join(', ') : 'general';
  }

  budgetText(provider: ILLMProvider): string {
    return `EUR ${this.formatMoney(provider.budgetUsedEur)} / ${this.formatMoney(
      provider.dailyBudgetEur
    )}`;
  }

  budgetTooltip(provider: ILLMProvider): string {
    return [
      `Budget used: EUR ${this.formatMoney(provider.budgetUsedEur)}`,
      `Daily max: EUR ${this.formatMoney(provider.dailyBudgetEur)}`,
      `Accounting: ${this.usageAccountingLabel(provider.usageAccountingStatus)}`,
      `Input tokens: ${this.formatNumber(provider.inputTokensUsed)}`,
      `Output tokens: ${this.formatNumber(provider.outputTokensUsed)}`,
      `Total tokens: ${this.formatNumber(this.totalTokens(provider))}`,
    ].join(' | ');
  }

  policyBudgetText(policy: ILLMPolicy): string {
    if (policy.usageAccountingStatus === 'unavailable') {
      return `Usage unavailable / max EUR ${this.formatMoney(policy.dailyPaidBudgetEur)}`;
    }
    return `EUR ${this.formatMoney(policy.dailyBudgetUsedEur)} / ${this.formatMoney(
      policy.dailyPaidBudgetEur
    )}`;
  }

  policyBudgetTooltip(policy: ILLMPolicy): string {
    return [
      `Daily budget used: EUR ${this.formatMoney(policy.dailyBudgetUsedEur)}`,
      `Daily budget max: EUR ${this.formatMoney(policy.dailyPaidBudgetEur)}`,
      `Accounting: ${this.usageAccountingLabel(policy.usageAccountingStatus)}`,
      ...(policy.usagePeriodStart ? [`Period starts: ${policy.usagePeriodStart} (${policy.usageTimezone || 'UTC'})`] : []),
      `Input tokens: ${this.formatNumber(policy.inputTokensUsed)}`,
      `Output tokens: ${this.formatNumber(policy.outputTokensUsed)}`,
      `Total tokens: ${this.formatNumber(policy.inputTokensUsed + policy.outputTokensUsed)}`,
    ].join(' | ');
  }

  formatMoney(value?: number): string {
    const normalized = value || 0;
    const fixed = normalized.toFixed(4).replace(/\.?0+$/, '');
    return fixed || '0';
  }

  formatNumber(value?: number): string {
    return `${value || 0}`.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  }

  usageAccountingLabel(status?: string): string {
    switch (status) {
      case 'durable':
        return 'Stored generation history';
      case 'process_only':
        return 'Current process only; resets daily';
      case 'unavailable':
        return 'Stored totals could not be read';
      default:
        return 'Source not reported';
    }
  }

  totalTokens(provider: ILLMProvider): number {
    return (provider.inputTokensUsed || 0) + (provider.outputTokensUsed || 0);
  }

  enabledProviderCount(policy: ILLMPolicy): number {
    return policy.providers.filter((provider) => provider.enabled).length;
  }

  configuredProviderCount(policy: ILLMPolicy): number {
    return policy.providers.filter((provider) => provider.configured).length;
  }

  totalModelCount(policy: ILLMPolicy): number {
    return policy.providers.reduce((total, provider) => total + provider.models.length, 0);
  }

  localModelCount(policy: ILLMPolicy): number {
    return policy.providers.reduce(
      (total, provider) =>
        total + provider.models.filter((model) => model.tier === 'local').length,
      0
    );
  }

  freeModelCount(policy: ILLMPolicy): number {
    return policy.providers.reduce(
      (total, provider) =>
        total + provider.models.filter((model) => model.tier === 'free').length,
      0
    );
  }

  paidModelCount(policy: ILLMPolicy): number {
    return policy.providers.reduce(
      (total, provider) =>
        total + provider.models.filter((model) => model.requiresApproval).length,
      0
    );
  }

  approvalGatedProviderCount(policy: ILLMPolicy): number {
    return policy.providers.filter((provider) => provider.paid || provider.models.some((model) => model.requiresApproval)).length;
  }

  liveProbeCount(): number {
    return this.probes.filter((probe) => probe.live).length;
  }

  skippedModelCount(decision: ILLMRouteDecision): number {
    return decision.skipped?.length || 0;
  }

  fallbackCount(decision: ILLMRouteDecision): number {
    return decision.fallbackPath?.length || 0;
  }

  configuredProviderSummary(policy: ILLMPolicy): string {
    return `${this.configuredProviderCount(policy)} / ${policy.providers.length}`;
  }

  policyStateLabel(policy: ILLMPolicy): string {
    if (!policy.paidCallsAllowed && policy.dailyPaidBudgetEur === 0) {
      return '$0 mode locked';
    }
    if (policy.requireApprovalBeforePaidUsage) {
      return 'approval gated';
    }
    return 'paid routing enabled';
  }

  policyStateColor(policy: ILLMPolicy): string {
    if (!policy.paidCallsAllowed && policy.dailyPaidBudgetEur === 0) {
      return 'green';
    }
    if (policy.requireApprovalBeforePaidUsage) {
      return 'gold';
    }
    return 'red';
  }

  budgetUsagePercent(policy: ILLMPolicy): number {
    if (!policy.dailyPaidBudgetEur) {
      return 0;
    }
    return Math.min(100, Math.round((policy.dailyBudgetUsedEur / policy.dailyPaidBudgetEur) * 100));
  }

  tokenUsageText(policy: ILLMPolicy): string {
    return `${this.formatNumber(policy.inputTokensUsed)} in / ${this.formatNumber(
      policy.outputTokensUsed
    )} out`;
  }

  providerReadinessText(provider: ILLMProvider): string {
    const strictReason = this.strictProviderReadinessReason(provider);
    if (strictReason) {
      return `Strict mode: ${strictReason}`;
    }
    if (provider.configured) {
      return 'Endpoint ready';
    }
    return provider.readinessReason || 'Provider is not ready';
  }

  strictProbePolicyLabel(policy: ILLMPolicy): string {
    if (!policy.requireRecentLiveProviderProbe) {
      return 'configured allowed';
    }
    return `strict / ${this.probeAgeLabel(policy.providerProbeMaxAgeSeconds)}`;
  }

  strictProbePolicyTooltip(policy: ILLMPolicy): string {
    if (!policy.requireRecentLiveProviderProbe) {
      return 'Strict live readiness checks are disabled. A configured endpoint may be considered for routing under the remaining policy gates.';
    }
    return `Strict live readiness is enabled. A provider is skipped unless its latest persisted probe is live and no older than ${this.probeAgeLabel(policy.providerProbeMaxAgeSeconds)}.`;
  }

  private strictProviderReadinessReason(provider: ILLMProvider): string {
    if (!this.policy?.requireRecentLiveProviderProbe || !provider.enabled || !provider.configured) {
      return '';
    }
    const probe = this.probes.find((item) => item.providerId === provider.id);
    if (!probe) {
      return 'live probe has not been recorded';
    }
    if (!probe.live) {
      return 'latest probe is not live';
    }
    const ageSeconds = (Date.now() - new Date(probe.checkedAt).getTime()) / 1000;
    if (ageSeconds > this.policy.providerProbeMaxAgeSeconds) {
      return 'latest live probe is stale';
    }
    return '';
  }

  private probeAgeLabel(seconds: number): string {
    if (seconds < 60) {
      return `${seconds}s`;
    }
    if (seconds % 60 === 0) {
      return `${seconds / 60}m`;
    }
    return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  }

  routeDecisionSummary(decision: ILLMRouteDecision): string {
    if (!decision.selectedModelName) {
      return 'No model selected under the current policy.';
    }
    return `${decision.selectedModelName} selected from ${decision.selectedProviderId}`;
  }

  calibrationSummary(decision: ILLMRouteDecision): string {
    const evidence = decision.calibration;
    if (!evidence) {
      return 'No evaluated outcome exists for this model and lane yet.';
    }
    return `${evidence.acceptedOutputs}/${evidence.evaluatedRuns} validator-accepted outputs, ${(evidence.wilsonLowerBound * 100).toFixed(1)}% conservative lower bound, ${evidence.confidence} confidence.`;
  }

  validationLabel(status?: string): string {
    return (status || 'unvalidated').replace(/_/g, ' ');
  }

  validationColor(status?: string): string {
    switch (status) {
      case 'verified':
      case 'source_supported':
      case 'schema_validated':
      case 'test_passed':
      case 'human_approved':
        return 'green';
      case 'failed':
        return 'red';
      case 'needs_review':
        return 'gold';
      default:
        return 'default';
    }
  }

  tierOrder(policy: ILLMPolicy | undefined = this.policy): string[] {
    return policy?.tierOrder?.length
      ? policy.tierOrder
      : ['local', 'free', 'cheap', 'acceptable', 'high', 'premium', 'expensive'];
  }

  scrollToSection(id: string): void {
    const element = document.getElementById(id);
    if (!element) {
      return;
    }
    const progressive = element.matches('hai-progressive-section')
      ? element
      : element.closest('hai-progressive-section');
    if (progressive?.classList.contains('hai-progressive-section--advanced')) {
      this.viewPreferences.setMode(MODULE_ID, 'advanced');
      document.body.classList.add('hai-view-advanced');
      const trigger = progressive.querySelector<HTMLButtonElement>(
        '.hai-progressive-section__summary'
      );
      if (trigger?.getAttribute('aria-expanded') !== 'true') trigger?.click();
    }
    const parentDetails = element.closest('details') as HTMLDetailsElement | null;
    if (parentDetails) {
      parentDetails.open = true;
    }
    if (element instanceof HTMLDetailsElement) {
      element.open = true;
    }
    element.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  focusRouteInput(): void {
    this.scrollToSection('route-task');
    window.setTimeout(() => {
      document.getElementById('route-task-input')?.focus();
    }, 220);
  }

  trackByAction(_index: number, action: PolicyActionCard): string {
    return action.action;
  }

  trackByProvider(_index: number, provider: ILLMProvider): string {
    return provider.id;
  }

  trackByTier(_index: number, group: { tier: string }): string {
    return group.tier;
  }

  trackByModel(_index: number, model: ILLMModel): string {
    return model.id;
  }

  trackByCatalogEntry(_index: number, entry: TierCatalogEntry): string {
    return `${entry.providerName}:${entry.model.id}`;
  }

  trackByProbe(_index: number, probe: ILLMProviderProbe): string {
    return probe.providerId;
  }

  trackByMaintenance(_index: number, record: ILLMModelMaintenanceResult): string {
    return `${record.providerId}:${record.modelId}:${record.checkedAt}`;
  }

  trackByLog(index: number, log: ILLMRouteDecision): string {
    return `${log.loggedAt || index}:${log.selectedProviderId}:${log.selectedModelId}`;
  }

  trackByFallback(_index: number, item: { providerId: string; modelId: string }): string {
    return `${item.providerId}:${item.modelId}`;
  }

  trackByGeneration(index: number, generation: ILLMGenerationResult): string {
    return generation.generationId || `${generation.loggedAt || index}:${generation.providerId}:${generation.modelId}`;
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }
}

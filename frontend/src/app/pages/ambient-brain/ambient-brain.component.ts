import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { finalize, Subscription, take, timeout } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import {
  IAmbientNeed,
  IAmbientOpportunity,
  IAmbientOverview,
  IAmbientScan,
} from '../../models/ambient.model.interface';
import { IAutonomyOverview, IAutonomyStressResult } from '../../models/autonomy.model.interface';
import { AmbientService } from '../../services/ambient.service';
import { AutonomyService } from '../../services/autonomy.service';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { safeWebSourceHref } from '../../control-room/source-navigation';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-ambient-brain',
    templateUrl: './ambient-brain.component.html',
    styleUrls: ['./ambient-brain.component.scss'],
    standalone: false
})
export class AmbientBrainComponent implements OnInit, OnDestroy {
  readonly moduleId = 'ambient-brain';
  overview?: IAmbientOverview;
  autonomyOverview?: IAutonomyOverview;
  autonomyErrorMessage = '';
  autonomyLoading = false;
  loading = false;
  errorMessage = '';
  scanning = false;
  scanNeedsReview = false;
  stressTesting = false;
  savingNeed = '';
  resolving = '';
  decisionsNeedRefresh = false;
  statusFilter = 'proposed';
  selectedNeed?: IAmbientNeed;
  selectedOpportunity?: IAmbientOpportunity;
  detailsMode: 'none' | 'autonomy' | 'policy' = 'none';
  private readonly operationTimeoutMs = 30000;
  private destroyed = false;
  private decisionSub?: Subscription;
  private overviewSub?: Subscription;
  private needSaveSub?: Subscription;
  private scanSub?: Subscription;
  private stressSub?: Subscription;
  private autonomySub?: Subscription;
  private lastAcknowledgedScanId = '';

  constructor(
    private ambient: AmbientService,
    private autonomy: AutonomyService,
    private notification: NzNotificationService,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  ngOnInit(): void {
    this.refresh();
    this.refreshAutonomy();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.decisionSub?.unsubscribe();
    this.overviewSub?.unsubscribe();
    this.needSaveSub?.unsubscribe();
    this.scanSub?.unsubscribe();
    this.stressSub?.unsubscribe();
    this.autonomySub?.unsubscribe();
  }

  refreshAutonomy(): void {
    if (this.destroyed) return;
    this.autonomySub?.unsubscribe();
    this.autonomyLoading = true;
    let received = false;
    this.autonomySub = this.autonomy.overview().pipe(take(1), timeout(this.operationTimeoutMs),
      finalize(() => { this.autonomyLoading = false; }),
    ).subscribe({
      next: (overview) => {
        received = true;
        if (!this.validAutonomyOverview(overview)) {
          this.autonomyErrorMessage = 'Telemetry response was incomplete or invalid. Previous figures are stale; refresh diagnostics.';
          this.notification.warning('Telemetry unconfirmed', this.autonomyErrorMessage);
          return;
        }
        this.autonomyOverview = overview;
        this.autonomyErrorMessage = '';
      },
      error: () => {
        this.autonomyErrorMessage = 'Telemetry could not be refreshed. Previous figures are stale; opportunity decisions remain available.';
        this.notification.warning(
          'Autonomy telemetry unavailable',
          'Workflow execution metrics could not be loaded.'
        );
      },
      complete: () => {
        if (!received) this.autonomyErrorMessage = 'No telemetry was returned. Previous figures are stale; refresh diagnostics.';
      },
    });
  }

  refreshStatus(): void {
    this.refresh();
    this.refreshAutonomy();
  }

  private validAutonomyOverview(overview?: IAutonomyOverview): overview is IAutonomyOverview {
    if (!overview || typeof overview.generatedAt !== 'string' || !Number.isFinite(Date.parse(overview.generatedAt)) ||
        !overview.metrics || !Array.isArray(overview.recentActions) || !Array.isArray(overview.recentEvaluations) ||
        !Array.isArray(overview.recentStressRuns) || !Array.isArray(overview.warnings) ||
        overview.warnings.some(item => typeof item !== 'string')) return false;
    const metrics = overview.metrics;
    if (!(['attempts', 'rawCompletions', 'completionUnderPolicy', 'policyViolations', 'invalidActions',
        'humanInterventions', 'recoveryAttempts', 'recovered'] as const).every(key =>
          Number.isSafeInteger(metrics[key]) && metrics[key] >= 0) ||
        !Number.isFinite(metrics.averageLatencyMillis) || metrics.averageLatencyMillis < 0 ||
        !(['rawCompletionRate', 'policyCompletionRate', 'interventionRate', 'recoveryRate'] as const).every(key =>
          Number.isFinite(metrics[key]) && metrics[key] >= 0 && metrics[key] <= 1)) return false;
    if ((['rawCompletions', 'completionUnderPolicy', 'policyViolations', 'invalidActions', 'humanInterventions',
        'recoveryAttempts', 'recovered'] as const).some(key => metrics[key] > metrics.attempts) ||
        metrics.recovered > metrics.recoveryAttempts) return false;
    const ratio = (count: number, total: number): number => total ? count / total : 0;
    if (Math.abs(metrics.rawCompletionRate - ratio(metrics.rawCompletions, metrics.attempts)) > 1e-9 ||
        Math.abs(metrics.policyCompletionRate - ratio(metrics.completionUnderPolicy, metrics.attempts)) > 1e-9 ||
        Math.abs(metrics.interventionRate - ratio(metrics.humanInterventions, metrics.attempts)) > 1e-9 ||
        Math.abs(metrics.recoveryRate - ratio(metrics.recovered, metrics.recoveryAttempts)) > 1e-9) return false;
    return overview.recentStressRuns.every(run => run && typeof run.id === 'string' &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(run.id) &&
      run.id !== '00000000-0000-0000-0000-000000000000' && typeof run.createdAt === 'string' &&
      Number.isFinite(Date.parse(run.createdAt)) && Number.isSafeInteger(run.passed) && run.passed >= 0 &&
      Number.isSafeInteger(run.failed) && run.failed >= 0);
  }

  runStressSuite(): void {
    if (this.destroyed || this.stressTesting) return;
    this.stressTesting = true;
    let received = false;
    this.stressSub = this.autonomy.runStressSuite().pipe(take(1), timeout(this.operationTimeoutMs),
      finalize(() => { this.stressTesting = false; }),
    ).subscribe({
      next: (result) => {
        received = true;
        if (!this.validStressResult(result)) {
          this.errorMessage = 'The guard-suite response did not contain a valid run with matching case totals. Refresh diagnostics before interpreting the result.';
          this.notification.warning('Guard results unconfirmed', this.errorMessage);
          return;
        }
        const message = `${result.run.passed} passed; ${result.run.failed} failed. These are deterministic guard results, not external-provider or production acceptance.`;
        if (result.run.failed > 0) this.notification.warning('Guard failures reported', message);
        else this.notification.info('Guard results reported', message);
        this.refreshAutonomy();
      },
      error: () => {
        this.errorMessage = 'The deterministic guard suite could not be confirmed. Refresh diagnostics before retrying.';
        this.notification.error('Guard results unconfirmed', this.errorMessage);
      },
      complete: () => {
        if (!received) {
          this.errorMessage = 'No guard-suite result was returned. Refresh diagnostics before retrying.';
          this.notification.warning('Guard results unconfirmed', this.errorMessage);
        }
      },
    });
  }

  private validStressResult(result?: IAutonomyStressResult): result is IAutonomyStressResult {
    const run = result?.run;
    if (!run || typeof run.id !== 'string' ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(run.id) ||
        run.id === '00000000-0000-0000-0000-000000000000' ||
        typeof run.createdAt !== 'string' || !Number.isFinite(Date.parse(run.createdAt)) ||
        !Number.isSafeInteger(run.passed) || run.passed < 0 || !Number.isSafeInteger(run.failed) || run.failed < 0 ||
        !Array.isArray(result?.results) || !result.results.length) return false;
    const names = new Set<string>();
    for (const item of result.results) {
      if (!item || typeof item.name !== 'string' || !item.name.trim() || names.has(item.name) ||
          typeof item.passed !== 'boolean' || typeof item.expected !== 'string' || typeof item.actual !== 'string' ||
          item.passed !== (item.expected === item.actual)) return false;
      names.add(item.name);
    }
    return run.passed === result.results.filter(item => item.passed).length &&
      run.failed === result.results.filter(item => !item.passed).length;
  }

  percent(value: number): string {
    return `${(value * 100).toFixed(1)}%`;
  }

  visibleNeeds(): IAmbientNeed[] {
    return (this.overview?.needs || []).filter((need) => need.enabled);
  }

  disabledNeedCount(): number {
    return (this.overview?.needs || []).filter((need) => !need.enabled).length;
  }

  proposedCount(): number {
    return (this.overview?.opportunities || []).filter((item) => item.status === 'proposed').length;
  }

  acceptedCount(): number {
    return (this.overview?.opportunities || []).filter((item) => item.status === 'accepted').length;
  }

  needGap(need: IAmbientNeed): number {
    return Math.max(0, need.targetLevel - need.currentLevel);
  }

  needTone(need: IAmbientNeed): string {
    const gap = this.needGap(need);
    if (gap >= 30) return 'critical';
    if (gap >= 15) return 'attention';
    return 'stable';
  }

  scanReviewSummary(): string {
    const scan = this.overview?.scans?.[0];
    if (this.scanning || scan?.status?.toLowerCase() === 'running') {
      return 'Scanning';
    }
    if (!scan) {
      return 'Not run';
    }
    if (scan.errorMessage || scan.status?.toLowerCase() === 'failed') {
      return 'Unavailable';
    }
    if (this.isCompletedScan(scan)) {
      return `${scan.itemsExamined} reviewed`;
    }
    return 'Status unknown';
  }

  scanReviewDetail(): string {
    const scan = this.overview?.scans?.[0];
    if (this.scanning) {
      return 'Scanning connected signals now';
    }
    if (!scan) {
      return 'Ready for first scan';
    }
    if (scan.status?.toLowerCase() === 'running') {
      return Number.isSafeInteger(scan.itemsExamined) && scan.itemsExamined >= 0 ? `${scan.itemsExamined} reviewed so far` : 'Scan is running; counts are unavailable';
    }
    if (scan.errorMessage || scan.status?.toLowerCase() === 'failed') {
      return 'The latest scan did not complete';
    }
    if (this.isCompletedScan(scan)) {
      return `${scan.opportunitiesFound} opportunities found`;
    }
    return 'Refresh status to verify the scan result';
  }

  topOpportunity(): IAmbientOpportunity | undefined {
    return this.filteredOpportunities()[0];
  }

  openTopOpportunity(): void {
    const opportunity = this.topOpportunity();
    if (opportunity) {
      this.openOpportunityDetails(opportunity);
    }
  }

  openNeed(need: IAmbientNeed): void {
    this.selectedNeed = { ...need };
  }

  closeNeed(): void {
    if (this.selectedNeed && this.savingNeed === this.selectedNeed.key) return;
    this.selectedNeed = undefined;
  }

  openOpportunityDetails(item: IAmbientOpportunity): void {
    this.selectedOpportunity = item;
  }

  closeOpportunityDetails(): void {
    this.selectedOpportunity = undefined;
  }

  acceptSelectedOpportunity(): void {
    if (!this.selectedOpportunity) return;
    const opportunity = this.selectedOpportunity;
    this.accept(opportunity);
  }

  openDetails(mode: 'autonomy' | 'policy'): void {
    this.detailsMode = mode;
  }

  policyFlag(key: 'executionEnabled' | 'schedulerEnabled' | 'suggestionOnly'): string {
    const value = this.overview?.policy?.[key];
    if (this.loading || this.decisionsNeedRefresh || typeof value !== 'boolean') return 'Unavailable';
    return value ? 'Enabled' : 'Disabled';
  }

  policyValue(key: 'minimumScore' | 'minimumConfidence' | 'opportunityLimit' | 'executionLimit' | 'cooldownHours' | 'scanIntervalSeconds'): string {
    const value = this.overview?.policy?.[key];
    if (this.loading || this.decisionsNeedRefresh || !Number.isSafeInteger(value) || value === undefined || value < 0 ||
        ((key === 'minimumScore' || key === 'minimumConfidence') && value > 100)) return 'Unavailable';
    return String(value);
  }

  policySnapshotTime(): string | undefined {
    const value = this.overview?.generatedAt;
    return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : undefined;
  }

  closeDetails(): void {
    this.detailsMode = 'none';
  }

  refresh(): void {
    if (this.destroyed || this.resolving || this.savingNeed || this.scanning) return;
    this.overviewSub?.unsubscribe();
    this.loading = true;
    this.errorMessage = '';
    let received = false;
    this.overviewSub = this.ambient.overview().pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.loading = false; }),
    ).subscribe({
      next: (overview) => {
        received = true;
        if (!overview || !this.validOperationalRecords(overview) || !Array.isArray(overview.scans) ||
            overview.scans.some(scan => !scan || typeof scan.id !== 'string' || !scan.id.trim() ||
              !['completed', 'failed', 'running'].includes(scan.status) || typeof scan.startedAt !== 'string' || !Number.isFinite(Date.parse(scan.startedAt)))) {
          this.decisionsNeedRefresh = true;
          this.scanNeedsReview = true;
          this.errorMessage = 'The overview did not include valid opportunity, need and scan records. Previous records remain available for inspection.';
          return;
        }
        this.overview = overview;
        this.decisionsNeedRefresh = false;
        this.scanNeedsReview = overview.scans.some(scan => scan.status === 'running');
      },
      error: () => {
        this.decisionsNeedRefresh = true;
        this.scanNeedsReview = true;
        this.errorMessage = 'Current opportunity records could not be loaded. Refresh before making another decision.';
        this.notification.error('Proactive brain unavailable', this.errorMessage);
      },
      complete: () => {
        if (!received) {
          this.decisionsNeedRefresh = true;
          this.scanNeedsReview = true;
          this.errorMessage = 'No overview was returned. Refresh before making another decision.';
        }
      },
    });
  }

  private validOperationalRecords(overview: Pick<IAmbientOverview, 'needs' | 'opportunities'>): boolean {
    if (!Array.isArray(overview.needs) || !Array.isArray(overview.opportunities)) return false;
    const needIds = new Set<string>();
    const needKeys = new Set<string>();
    const proposalIds = new Set<string>();
    const text = (value: unknown): value is string => typeof value === 'string' && !!value.trim();
    const level = (value: number): boolean => Number.isInteger(value) && value >= 0 && value <= 100;
    for (const need of overview.needs) {
      if (!need || !text(need.id) || !text(need.key) || typeof need.name !== 'string' ||
          needIds.has(need.id) || needKeys.has(need.key) || typeof need.enabled !== 'boolean' ||
          ![need.currentLevel, need.targetLevel, need.priorityWeight].every(level) ||
          (need.notes !== undefined && typeof need.notes !== 'string')) return false;
      needIds.add(need.id);
      needKeys.add(need.key);
    }
    for (const item of overview.opportunities) {
      if (!item || !text(item.id) || !text(item.needKey) || proposalIds.has(item.id) ||
          typeof item.title !== 'string' || typeof item.rationale !== 'string' || typeof item.nextAction !== 'string' ||
          !['proposed', 'accepted', 'dismissed', 'completed'].includes(item.status) ||
          typeof item.requiresApproval !== 'boolean' ||
          ![item.urgency, item.impact, item.effort, item.risk].every(level) ||
          !Number.isFinite(item.confidence) || item.confidence < 0 || item.confidence > 1 ||
          !Number.isFinite(item.priorityScore) || item.priorityScore < 0 || item.priorityScore > 100 ||
          [item.sourceType, item.sourceId, item.sourceUri, item.evidenceManifest, item.resolutionNote, item.workflowId]
            .some(value => value !== undefined && typeof value !== 'string')) return false;
      proposalIds.add(item.id);
    }
    return true;
  }

  runScan(): void {
    if (this.destroyed || this.loading || this.scanning || this.scanNeedsReview || this.resolving || this.savingNeed ||
        this.overview?.scans?.some(scan => scan.status === 'running')) {
      return;
    }
    const knownIds = new Set((this.overview?.scans || []).map(scan => scan.id));
    if (this.lastAcknowledgedScanId) knownIds.add(this.lastAcknowledgedScanId);
    this.scanning = true;
    let received = false;
    this.scanSub = this.ambient.scan().pipe(take(1), timeout(this.operationTimeoutMs),
      finalize(() => { this.scanning = false; }),
    ).subscribe({
      next: (scan) => {
        received = true;
        if (!this.isCompletedScan(scan) || scan.trigger !== 'manual' || knownIds.has(scan.id)) {
          this.scanNeedsReview = true;
          this.decisionsNeedRefresh = true;
          this.errorMessage = 'The response did not confirm a new completed manual scan. Refresh current scan status before starting another pass.';
          this.notification.warning('Scan unconfirmed', this.errorMessage);
          return;
        }
        this.lastAcknowledgedScanId = scan.id;
        this.scanning = false;
        this.notification.info('Completed scan record received', `${scan.opportunitiesFound} proposals reported; ${scan.deduplicated} deduplications reported. These counts do not confirm task execution.`);
        this.refresh();
      },
      error: () => {
        this.scanNeedsReview = true;
        this.decisionsNeedRefresh = true;
        this.errorMessage = 'HAI could not confirm the scan. Refresh current scan status before starting another pass.';
        this.notification.error('Scan unconfirmed', this.errorMessage);
      },
      complete: () => {
        if (!received) {
          this.scanNeedsReview = true;
          this.decisionsNeedRefresh = true;
          this.errorMessage = 'No scan record was returned. Refresh current scan status before starting another pass.';
          this.notification.warning('Scan unconfirmed', this.errorMessage);
        }
      },
    });
  }

  private isCompletedScan(scan?: IAmbientScan): scan is IAmbientScan {
    if (!scan || scan.status !== 'completed' || typeof scan.id !== 'string' ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(scan.id) ||
        scan.id === '00000000-0000-0000-0000-000000000000' ||
        (scan.errorMessage !== undefined && scan.errorMessage !== '')) return false;
    const started = Date.parse(scan.startedAt);
    const completed = Date.parse(scan.completedAt || '');
    if (!Number.isFinite(started) || !Number.isFinite(completed) || completed < started) return false;
    return (['itemsExamined', 'opportunitiesFound', 'created', 'updated', 'deduplicated', 'advanced',
      'filtered', 'skipped', 'blocked', 'manifestBytes', 'deduplicatedBytes'] as const)
      .every(key => Number.isSafeInteger(scan[key]) && scan[key] >= 0);
  }

  saveNeed(need: IAmbientNeed): void {
    if (this.destroyed || this.savingNeed || this.loading || this.scanning || this.resolving || this.selectedNeed !== need) return;
    const current = this.overview?.needs?.find(item => item.key === need?.key && item.id === need.id);
    if (!current || typeof current.id !== 'string' || !current.id.trim() || !current.key || [need.currentLevel, need.targetLevel, need.priorityWeight].some(value =>
        !Number.isInteger(value) || value < 0 || value > 100) || typeof need.enabled !== 'boolean' ||
        (need.notes !== undefined && typeof need.notes !== 'string')) {
      this.notification.warning('Check priority details', 'Use a current profile, whole-number levels from zero to 100, and an enabled setting. Your input is retained.');
      return;
    }
    const request = { currentLevel: need.currentLevel, targetLevel: need.targetLevel,
      priorityWeight: need.priorityWeight, enabled: need.enabled, notes: (need.notes || '').trim() };
    const draftSnapshot = JSON.stringify(need);
    const overview = this.overview;
    this.savingNeed = need.key;
    let received = false;
    this.needSaveSub = this.ambient.updateNeed(need.key, request).pipe(
      take(1), timeout(this.operationTimeoutMs), finalize(() => { this.savingNeed = ''; }),
    ).subscribe({
        next: (savedNeed) => {
          received = true;
          if (savedNeed?.id !== current.id || savedNeed.key !== current.key ||
              savedNeed.currentLevel !== request.currentLevel || savedNeed.targetLevel !== request.targetLevel ||
              savedNeed.priorityWeight !== request.priorityWeight || savedNeed.enabled !== request.enabled ||
              (savedNeed.notes || '') !== request.notes) {
            this.notification.warning('Priority update unconfirmed', 'The returned profile did not match your submitted values. Your draft is retained; inspect current state before retrying.');
            return;
          }
          if (this.overview === overview && this.overview) {
            this.overview = {
              ...this.overview,
              needs: this.overview.needs.map((item) => item === current ? savedNeed : item),
            };
          }
          if (this.selectedNeed === need && JSON.stringify(need) === draftSnapshot) this.selectedNeed = undefined;
          this.notification.info('Priority response received', 'The server returned the requested profile values. This is not an independent durability check.');
        },
        error: () => {
          this.notification.error('Priority update unconfirmed', 'HAI could not confirm the update. Your draft is retained; inspect current state before retrying.');
        },
        complete: () => {
          if (!received) this.notification.warning('Priority update unconfirmed', 'No profile was returned. Your draft is retained; inspect current state before retrying.');
        },
      });
  }

  accept(item: IAmbientOpportunity): void {
    this.resolve(item, true);
  }

  dismiss(item: IAmbientOpportunity): void {
    this.resolve(item, false);
  }

  filteredOpportunities(): IAmbientOpportunity[] {
    return (this.overview?.opportunities || []).filter(
      (item) => this.statusFilter === 'all' || item.status === this.statusFilter
    );
  }

  needName(key: string): string {
    return this.overview?.needs.find((item) => item.key === key)?.name || key;
  }

  riskColor(risk: number): string {
    if (risk >= 70) {
      return 'red';
    }
    if (risk >= 45) {
      return 'orange';
    }
    return 'green';
  }

  openSource(uri?: string): void {
    const safeUri = this.safeExternalUri(uri);
    if (safeUri) {
      window.open(safeUri, '_blank', 'noopener,noreferrer');
      return;
    }
    this.notification.warning(
      'Source link not opened',
      'Only absolute HTTP and HTTPS source links can be opened from the dashboard.'
    );
  }

  openWorkflow(id?: string): void {
    this.router.navigate(['/workflow-engine'], {
      queryParams: id ? { workflowId: id } : undefined,
    });
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  canOpenSource(uri?: string): boolean {
    return this.safeExternalUri(uri) !== '';
  }

  private safeExternalUri(uri?: string): string {
    return safeWebSourceHref(uri) || '';
  }

  private resolve(item: IAmbientOpportunity, accept: boolean): void {
    if (this.destroyed || this.resolving || this.loading || this.scanning || this.savingNeed || this.decisionsNeedRefresh || !item?.id) return;
    const current = this.overview?.opportunities?.find(record => record.id === item.id);
    if (!current || current.status !== 'proposed' || item.status !== 'proposed' ||
        !this.validOperationalRecords({ needs: [], opportunities: [current] }) ||
        !this.validOperationalRecords({ needs: [], opportunities: [item] }) ||
        current.requiresApproval !== item.requiresApproval || current.risk !== item.risk ||
        current.needKey !== item.needKey || current.sourceType !== item.sourceType || current.sourceId !== item.sourceId) {
      this.notification.warning('Decision needs refresh', 'This proposal no longer matches a current proposed record. Refresh before deciding.');
      return;
    }
    const inspector = this.selectedOpportunity;
    this.resolving = current.id;
    const request = accept
      ? this.ambient.accept(current.id)
      : this.ambient.dismiss(current.id);
    let received = false;
    this.decisionSub = request.pipe(take(1), timeout(this.operationTimeoutMs),
      finalize(() => { this.resolving = ''; }),
    ).subscribe({
      next: (resolved) => {
        received = true;
        if (resolved?.id !== current.id || resolved.status !== (accept ? 'accepted' : 'dismissed') ||
            !this.validOperationalRecords({ needs: [], opportunities: [resolved] }) ||
            resolved.needKey !== current.needKey || (resolved.sourceType || '') !== (current.sourceType || '') ||
            (resolved.sourceId || '') !== (current.sourceId || '')) {
          this.decisionsNeedRefresh = true;
          this.errorMessage = 'The decision response did not match this proposal and its source. Refresh current state before retrying.';
          this.notification.warning('Decision unconfirmed', this.errorMessage);
          return;
        }
        if (this.selectedOpportunity === inspector && inspector?.id === current.id) this.selectedOpportunity = undefined;
        if (this.overview) this.overview = { ...this.overview,
          opportunities: this.overview.opportunities.map(record => record === current ? resolved : record) };
        this.resolving = '';
        this.notification.info('Decision response received', accept
          ? 'The server returned an accepted proposal. This does not confirm workflow execution or completion.'
          : 'The server returned a dismissed proposal. Reloading its current state.');
        this.refresh();
      },
      error: () => {
        this.decisionsNeedRefresh = true;
        this.errorMessage = 'The opportunity decision could not be confirmed. Inspect current state before trying again.';
        this.notification.error('Decision unconfirmed', this.errorMessage);
      },
      complete: () => {
        if (!received) {
          this.decisionsNeedRefresh = true;
          this.errorMessage = 'No decision record was returned. Refresh current state before retrying.';
          this.notification.warning('Decision unconfirmed', this.errorMessage);
        }
      },
    });
  }
}

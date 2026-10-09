import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { finalize, Subscription, take, timeout } from 'rxjs';
import { IHAIOSOverview, IHAIOSPursuitSpotlight } from '../../models/hai-os.model.interface';
import { HAI_OS_SERVICE_TOKEN } from '../../services/hai-os/hai-os.service.token';
import { IHAIOSService } from '../../services/hai-os.service.interface';
import { HAI_MODULES } from '../../control-room/module-registry';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-hai-os',
    templateUrl: './hai-os.component.html',
    styleUrls: ['./hai-os.component.scss'],
    standalone: false
})
export class HAIOSComponent implements OnInit, OnDestroy {
  readonly moduleId = 'hai-os';
  overview?: IHAIOSOverview;
  loading = false;
  errorMessage = '';
  private overviewSubscription?: Subscription;
  private destroyed = false;

  get visibleSpotlightItems(): IHAIOSPursuitSpotlight[] {
    return this.overview?.pursuitOverview.spotlight.slice(0, 3) ?? [];
  }

  spotlightAccessibleLabel(item: IHAIOSPursuitSpotlight): string {
    const signals = [
      item.needsRobert ? 'Needs your decision' : '',
      item.blocked ? 'Blocked' : '',
      item.reviewDue ? 'Review due' : '',
      item.planningNeeded ? 'Planning needed' : '',
      item.stale ? 'Stale' : '',
    ].filter(Boolean);
    const next = item.nextAction || item.currentState || item.evidenceLine || 'No next action recorded.';

    return [
      `Open ${item.title}`,
      `${item.status}, ${item.riskLevel} risk`,
      ...signals,
      next,
    ].join('. ');
  }

  hasPursuitId(item: IHAIOSPursuitSpotlight): boolean {
    return item.id.trim().length > 0;
  }

  statusLabel(value: string): string {
    return value.trim()
      .replace(/[_-]+/g, ' ')
      .replace(/\s+/g, ' ')
      .replace(/\b[a-z]/g, (letter) => letter.toUpperCase());
  }

  constructor(
    @Inject(HAI_OS_SERVICE_TOKEN) private haiOSService: IHAIOSService,
    private router: Router,
    private changeDetector: ChangeDetectorRef,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  ngOnInit(): void {
    const openSections = this.viewPreferences.get(this.moduleId).openSections;
    if (!Object.prototype.hasOwnProperty.call(openSections, 'pursuit-spotlight')) {
      this.viewPreferences.setSection(this.moduleId, 'pursuit-spotlight', true);
    }
    this.refresh();
  }

  refresh(): void {
    if (this.destroyed) return;
    this.overviewSubscription?.unsubscribe();
    this.loading = true;
    this.errorMessage = '';
    let received = false;
    const unavailable = () => {
      if (this.destroyed) return;
      this.errorMessage = this.overview
        ? 'Refresh could not be confirmed. Showing the last successfully loaded overview.'
        : 'The HAI OS overview could not be confirmed. Your work has not been changed.';
      this.changeDetector.markForCheck();
    };
    this.overviewSubscription = this.haiOSService.overview().pipe(
      take(1), timeout(30000), finalize(() => {
        this.loading = false;
        if (!this.destroyed) this.changeDetector.markForCheck();
      }),
    ).subscribe({
      next: (overview) => {
        received = true;
        if (!this.validOverview(overview)) { unavailable(); return; }
        this.overview = overview;
        this.loading = false;
        this.changeDetector.markForCheck();
      },
      error: unavailable,
      complete: () => { if (!received) unavailable(); },
    });
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.overviewSubscription?.unsubscribe();
  }

  private validOverview(value: IHAIOSOverview): boolean {
    const strings = (record: unknown, keys: string[]): boolean => !!record && typeof record === 'object' &&
      keys.every(key => typeof (record as Record<string, unknown>)[key] === 'string');
    const counts = (record: unknown, keys: string[]): boolean => !!record && typeof record === 'object' &&
      keys.every(key => { const count = (record as Record<string, unknown>)[key]; return typeof count === 'number' && Number.isSafeInteger(count) && count >= 0; });
    const flags = (record: unknown, keys: string[]): boolean => !!record && typeof record === 'object' &&
      keys.every(key => typeof (record as Record<string, unknown>)[key] === 'boolean');
    if (!strings(value, ['generatedAt', 'canonicalStack', 'emergencyStopReason', 'emergencyStopNote']) ||
        !counts(value, ['needsReviewTotal']) ||
        !flags(value, ['localFirst', 'completionFirst', 'paidUsageAllowed', 'emergencyStop']) ||
        typeof value.paidBudgetEur !== 'number' || !Number.isFinite(value.paidBudgetEur) || value.paidBudgetEur < 0) return false;
    const pursuit = value.pursuitOverview;
    if (!strings(pursuit, ['status', 'evidenceStatus', 'summary', 'next']) || !flags(pursuit, ['enabled']) ||
        !counts(pursuit, ['totalActive', 'needsRobert', 'vaReady', 'systemReady', 'blocked', 'stale', 'reviewDue', 'planningNeeded', 'highRisk', 'completionCandidates', 'decisionCards', 'linkedEvidence', 'openLoops', 'timelineItems', 'ambientProposals', 'ambientApprovalQueue'])) return false;
    return Array.isArray(value.referenceStacks) && value.referenceStacks.every(item => strings(item, ['name', 'status', 'use'])) &&
      Array.isArray(value.metrics) && value.metrics.every(item => strings(item, ['label', 'status']) && typeof item.value === 'number' && Number.isFinite(item.value) && item.value >= 0) &&
      Array.isArray(value.planes) && value.planes.every(item => strings(item, ['name', 'status', 'description']) && Array.isArray(item.links) && item.links.every(link => typeof link === 'string')) &&
      Array.isArray(value.readinessGates) && value.readinessGates.every(item => strings(item, ['name', 'status', 'evidence', 'next'])) &&
      Array.isArray(pursuit.queues) && pursuit.queues.every(item => strings(item, ['name', 'description', 'status', 'route']) && counts(item, ['count'])) &&
      Array.isArray(pursuit.spotlight) && pursuit.spotlight.every(item => strings(item, ['id', 'title', 'status', 'riskLevel']) &&
        counts(item, ['needsRobert', 'blocked', 'openLoops', 'decisionCards', 'linkedEvidence', 'timelineItems']) && flags(item, ['stale', 'reviewDue', 'planningNeeded']));
  }

  open(link: string): void {
    const route = this.verifiedRoute(link);
    if (route) void this.router.navigateByUrl(route);
  }

  isNavigableRoute(link: string): boolean {
    return this.verifiedRoute(link) !== null;
  }

  private verifiedRoute(link: string): string | null {
    const candidate = typeof link === 'string' ? link.trim() : '';
    if (!candidate.startsWith('/') || candidate.startsWith('//') || candidate.includes('\\')) return null;

    try {
      const target = new URL(candidate, window.location.origin);
      if (target.origin !== window.location.origin || target.username || target.password) return null;

      const path = target.pathname.length > 1 ? target.pathname.replace(/\/+$/, '') : target.pathname;
      if (!HAI_MODULES.some((module) => module.route === path)) return null;
      return `${path}${target.search}${target.hash}`;
    } catch {
      return null;
    }
  }

  navigationLabel(link: string): string {
    const path = link.split(/[?#]/, 1)[0];
    let segment = path.split('/').filter(Boolean).pop() ?? '';
    try {
      segment = decodeURIComponent(segment);
    } catch {
      // Keep the literal path segment when it is not valid percent-encoding.
    }

    const acronyms: Record<string, string> = {
      ai: 'AI',
      api: 'API',
      hai: 'HAI',
      idp: 'IDP',
      llm: 'LLM',
      mcp: 'MCP',
      os: 'OS',
    };

    return segment
      .split(/[-_]+/)
      .filter(Boolean)
      .map((word) => acronyms[word.toLowerCase()] ?? word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ') || 'HAI module';
  }

  hasValidTimestamp(value: string): boolean {
    return Number.isFinite(Date.parse(value));
  }

  openPursuits(selected?: string, create = false): void {
    const pursuitId = selected?.trim();
    this.router.navigate(['/pursuits'], {
      queryParams: pursuitId
        ? { selected: pursuitId }
        : create ? { create: 'true' } : undefined,
    });
  }
}

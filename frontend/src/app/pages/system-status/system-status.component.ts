import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { Subscription, interval } from 'rxjs';
import {
  ISystemCheck,
  ISystemReadiness,
  SystemCheckSeverity,
} from '../../models/system-status.model.interface';
import { ISystemStatusService } from '../../services/system-status/system-status.service.interface';
import { SYSTEM_STATUS_SERVICE_TOKEN } from '../../services/system-status/system-status.service.token';

interface CheckGroup {
  key: string;
  title: string;
  checks: ISystemCheck[];
  worst: SystemCheckSeverity;
}

// A subsystem prefix ("database.connection" -> "database") mapped to a heading.
const GROUP_TITLES: Record<string, string> = {
  server: 'Gateway & server',
  database: 'Database',
  redis: 'Redis cache',
  kafka: 'Event bus (Kafka)',
  llm: 'LLM provider',
  security: 'Secrets & security',
  media: 'Media storage',
  runtime: 'Runtime mode',
};

const MODULE_ID = 'system-status';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-system-status',
    templateUrl: './system-status.component.html',
    styleUrls: ['./system-status.component.scss'],
    standalone: false
})
export class SystemStatusComponent implements OnInit, OnDestroy {
  readiness?: ISystemReadiness;
  groups: CheckGroup[] = [];
  recommendedActions: string[] = [];
  loading = false;
  loadError = false;
  lastUpdated?: Date;

  private pollSub?: Subscription;
  private refreshInFlight = false;
  private readonly onVisibilityChange = (): void => {
    // A hidden dashboard cannot be acted on. Avoid probing dependencies while
    // it is in the background, then immediately catch up when it is visible.
    if (!document.hidden) {
      this.refresh(true);
    }
  };

  constructor(
    @Inject(SYSTEM_STATUS_SERVICE_TOKEN)
    private systemStatusService: ISystemStatusService,
    private notification: NzNotificationService,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  get isAdvanced(): boolean {
    return this.viewPreferences.get(MODULE_ID).mode === 'advanced';
  }

  ngOnInit(): void {
    this.refresh();
    // Readiness is a live signal; poll it so the page reflects a dependency
    // going down without the operator reloading.
    this.pollSub = interval(15000).subscribe(() => {
      if (!document.hidden) {
        this.refresh(true);
      }
    });
    document.addEventListener('visibilitychange', this.onVisibilityChange);
  }

  ngOnDestroy(): void {
    this.pollSub?.unsubscribe();
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
  }

  refresh(silent = false): void {
    if (silent && this.refreshInFlight) {
      return;
    }
    if (!silent) {
      this.loading = true;
    }
    this.refreshInFlight = true;
    this.systemStatusService.readiness().subscribe({
      next: (readiness) => {
        this.readiness = readiness;
        this.groups = this.buildGroups(readiness.checks);
        this.recommendedActions = this.buildRecommendedActions(readiness.checks);
        this.lastUpdated = new Date();
        this.loading = false;
        this.loadError = false;
        this.refreshInFlight = false;
      },
      error: () => {
        this.loading = false;
        this.loadError = true;
        this.refreshInFlight = false;
        if (!silent) {
          this.notification.error(
            'System status unavailable',
            'Could not reach the readiness probe. You may need to sign in again.'
          );
        }
      },
    });
  }

  statusLabel(): string {
    switch (this.readiness?.status) {
      case 'ready':
        return 'All systems ready';
      case 'degraded':
        return 'Degraded — serving with warnings';
      case 'not_ready':
        return 'Not ready — a critical dependency is down';
      default:
        return 'Unknown';
    }
  }

  statusClass(): string {
    switch (this.readiness?.status) {
      case 'ready':
        return 'status-ok';
      case 'degraded':
        return 'status-warn';
      case 'not_ready':
        return 'status-fail';
      default:
        return 'status-unknown';
    }
  }

  refreshErrorMessage(): string {
    if (!this.readiness) {
      return 'Could not reach the readiness probe. If you were redirected to sign in, your session may have expired.'
    }
    const updated = this.lastUpdated ? ` Last successful status: ${this.lastUpdated.toLocaleTimeString()}.` : ''
    return `The latest refresh failed. The displayed status may be out of date.${updated} Refresh to retry.`
  }

  severityLabel(severity: SystemCheckSeverity): string {
    switch (severity) {
      case 'ok':
        return 'OK'
      case 'warn':
        return 'Warning'
      case 'fail':
        return 'Fail'
    }
  }

  severityClass(severity: SystemCheckSeverity): string {
    switch (severity) {
      case 'ok':
        return 'sev-ok';
      case 'warn':
        return 'sev-warn';
      case 'fail':
        return 'sev-fail';
    }
  }

  severityIcon(severity: SystemCheckSeverity): string {
    switch (severity) {
      case 'ok':
        return 'check-circle';
      case 'warn':
        return 'warning';
      case 'fail':
        return 'close-circle';
    }
  }

  private buildGroups(checks: ISystemCheck[]): CheckGroup[] {
    const byKey = new Map<string, ISystemCheck[]>();
    for (const check of checks) {
      const key = check.name.split('.')[0] || 'other';
      const bucket = byKey.get(key) ?? [];
      bucket.push(check);
      byKey.set(key, bucket);
    }

    const groups: CheckGroup[] = [];
    byKey.forEach((groupChecks, key) => {
      groups.push({
        key,
        title: GROUP_TITLES[key] ?? this.titleize(key),
        checks: groupChecks,
        worst: this.worstSeverity(groupChecks),
      });
    });

    // Failures first, then warnings, then healthy — an operator reads
    // top-down and should meet the problems first.
    const rank: Record<SystemCheckSeverity, number> = { fail: 0, warn: 1, ok: 2 };
    return groups.sort((a, b) => rank[a.worst] - rank[b.worst]);
  }

  // Each non-healthy check already carries a human-readable cause in `detail`;
  // pair it with its subsystem so the operator has a concrete to-do list rather
  // than a wall of green ticks and one buried red one.
  private buildRecommendedActions(checks: ISystemCheck[]): string[] {
    return checks
      .filter((c) => c.severity !== 'ok')
      .sort((a, b) => (a.severity === 'fail' ? -1 : 1))
      .map((c) => `${c.name}: ${c.detail}`);
  }

  private worstSeverity(checks: ISystemCheck[]): SystemCheckSeverity {
    if (checks.some((c) => c.severity === 'fail')) {
      return 'fail';
    }
    if (checks.some((c) => c.severity === 'warn')) {
      return 'warn';
    }
    return 'ok';
  }

  private titleize(key: string): string {
    return key.charAt(0).toUpperCase() + key.slice(1);
  }
}

import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit } from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { finalize, timeout } from 'rxjs';
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
const READY_POLL_MS = 120000;
const DEGRADED_POLL_MS = 60000;
const RECOVERY_POLL_MS = 15000;
const READINESS_TIMEOUT_MS = 10000;

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

  private pollTimer?: ReturnType<typeof setTimeout>;
  private refreshInFlight = false;
  private destroyed = false;
  private readonly onVisibilityChange = (): void => {
    if (document.hidden) {
      this.clearPollTimer();
      return;
    }
    this.refresh(true);
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
    document.addEventListener('visibilitychange', this.onVisibilityChange);
    this.refresh();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.clearPollTimer();
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
  }

  refresh(silent = false): void {
    this.clearPollTimer();
    if (this.refreshInFlight) {
      return;
    }
    if (!silent) {
      this.loading = true;
    }
    this.refreshInFlight = true;
    this.systemStatusService.readiness().pipe(
      timeout(READINESS_TIMEOUT_MS),
      finalize(() => {
        this.refreshInFlight = false;
      })
    ).subscribe({
      next: (readiness) => {
        this.readiness = readiness;
        this.groups = this.buildGroups(readiness.checks);
        this.recommendedActions = this.buildRecommendedActions(readiness.checks);
        this.lastUpdated = new Date();
        this.loading = false;
        this.loadError = false;
        this.scheduleNextPoll();
      },
      error: () => {
        this.loading = false;
        this.loadError = true;
        this.scheduleNextPoll();
        if (!silent) {
          this.notification.error(
            'System status unavailable',
            'Could not reach the readiness probe. You may need to sign in again.'
          );
        }
      },
    });
  }

  private scheduleNextPoll(): void {
    if (this.destroyed || document.hidden) {
      return;
    }
    this.clearPollTimer();
    this.pollTimer = setTimeout(() => {
      this.pollTimer = undefined;
      this.refresh(true);
    }, this.pollDelay());
  }

  private pollDelay(): number {
    if (this.readiness?.status === 'not_ready') {
      return RECOVERY_POLL_MS;
    }
    if (this.loadError || this.readiness?.status === 'degraded') {
      return DEGRADED_POLL_MS;
    }
    return READY_POLL_MS;
  }

  private clearPollTimer(): void {
    if (this.pollTimer !== undefined) {
      clearTimeout(this.pollTimer);
      this.pollTimer = undefined;
    }
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

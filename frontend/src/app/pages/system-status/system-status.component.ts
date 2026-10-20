import { ChangeDetectionStrategy, Component, Inject, OnDestroy, OnInit, Optional } from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { catchError, defaultIfEmpty, finalize, forkJoin, Observable, of, Subscription, switchMap, take, timeout } from 'rxjs';
import { AuthSessionService } from '../../services/auth-session.service';
import { IAuthSession } from '../../models/auth-session.model.interface';
import {
  ISystemCheck,
  ISystemInfo,
  ISystemReadiness,
  SystemCheckSeverity,
  IEventDeliveryStats,
  IEventDeliveryFailure,
  IA2ABridgeStatus,
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
  events: 'Event delivery',
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
  systemInfo?: ISystemInfo;
  groups: CheckGroup[] = [];
  recommendedActions: string[] = [];
  loading = false;
  loadError = false;
  lastUpdated?: Date;
  eventDelivery?: IEventDeliveryStats;
  connectorStatus?: IA2ABridgeStatus;
  operationsLoading = false;
  operationsError = '';
  deliveryError = '';
  connectorError = '';
  canReadConnector = false;
  canAdministerDelivery = false;
  retryConfirmationId = '';
  retryingEventId = '';
  retryMessage = '';

  private pollTimer?: ReturnType<typeof setTimeout>;
  private readinessSubscription?: Subscription;
  private operationsSubscription?: Subscription;
  private retrySubscription?: Subscription;
  private operationsOpen = false;
  private refreshInFlight = false;
  private systemInfoSubscription?: Subscription;
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
    private viewPreferences: ModuleViewPreferencesService,
    @Optional() @Inject(AuthSessionService) private authSessionService?: AuthSessionService,
  ) {}

  get isAdvanced(): boolean {
    return this.viewPreferences.get(MODULE_ID).mode === 'advanced';
  }

  ngOnInit(): void {
    document.addEventListener('visibilitychange', this.onVisibilityChange);
    this.loadSystemInfo();
    this.refresh();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.clearPollTimer();
    this.systemInfoSubscription?.unsubscribe();
    this.readinessSubscription?.unsubscribe();
    this.operationsSubscription?.unsubscribe();
    this.retrySubscription?.unsubscribe();
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
  }

  buildInfoTitle(info: ISystemInfo): string {
    return [
      `Build ${info.build.version}`,
      `Commit ${info.build.commit}`,
      `Built ${info.build.buildTime}`,
      `Runtime ${info.build.goVersion}`,
      `Mode ${info.runMode}`,
    ].join(' · ');
  }

  private loadSystemInfo(): void {
    this.systemInfoSubscription = this.systemStatusService.info().pipe(
      timeout(READINESS_TIMEOUT_MS)
    ).subscribe({
      next: (info) => {
        if (!this.destroyed) {
          this.systemInfo = info;
        }
      },
      // Build metadata is supplemental; a failure must not hide readiness.
      error: () => undefined,
    });
  }

  refresh(silent = false): void {
    if (this.destroyed) return;
    this.clearPollTimer();
    if (!silent && this.operationsOpen) this.loadOperationalDetails();
    if (this.refreshInFlight) {
      return;
    }
    if (!silent) {
      this.loading = true;
    }
    this.refreshInFlight = true;
    this.readinessSubscription = this.systemStatusService.readiness().pipe(
      timeout(READINESS_TIMEOUT_MS),
      take(1),
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

  onOperationsOpen(open: boolean): void {
    this.operationsOpen = open;
    this.retryConfirmationId = '';
    if (open) this.loadOperationalDetails();
    else {
      this.operationsSubscription?.unsubscribe();
      this.operationsLoading = false;
    }
  }

  loadOperationalDetails(): void {
    if (this.destroyed || !this.isAdvanced || !this.operationsOpen || this.operationsLoading || this.retryingEventId) return;
    this.operationsLoading = true;
    this.operationsError = '';
    this.deliveryError = '';
    this.connectorError = '';
    this.retryConfirmationId = '';
    this.canAdministerDelivery = false;
    this.canReadConnector = false;
    const sessionRead: Observable<IAuthSession | undefined> = this.authSessionService?.session() ?? of(undefined);
    this.operationsSubscription = sessionRead.pipe(
      timeout(READINESS_TIMEOUT_MS), take(1), defaultIfEmpty(undefined),
      switchMap((session) => {
        const owner = session?.authenticated === true && session.role === 'owner';
        this.canReadConnector = owner && session!.permissions.canRead === true;
        this.canAdministerDelivery = owner && session!.permissions.canAdminister === true;
        if (!this.canReadConnector && !this.canAdministerDelivery) {
          this.operationsError = 'Verified owner access is required. Delivery controls also require administration permission.';
        }
        return forkJoin({
          delivery: this.canAdministerDelivery ? this.systemStatusService.eventDelivery().pipe(defaultIfEmpty(undefined), catchError(() => { this.deliveryError = 'Delivery status unavailable. Previously loaded records may be stale.'; return of(undefined); })) : of(undefined),
          connector: this.canReadConnector ? this.systemStatusService.connectorStatus().pipe(defaultIfEmpty(undefined), catchError(() => { this.connectorError = 'Connector configuration unavailable. Previously loaded values may be stale.'; return of(undefined); })) : of(undefined),
        });
      }),
      timeout(READINESS_TIMEOUT_MS),
      finalize(() => this.operationsLoading = false),
    ).subscribe({
      next: ({ delivery, connector }) => {
        if (delivery) {
          if (this.validDeliveryStats(delivery)) this.eventDelivery = delivery;
          else this.deliveryError = 'Delivery status could not be verified. Refresh before retrying.';
        }
        else if (this.canAdministerDelivery && !this.deliveryError) this.deliveryError = 'Delivery status returned no records. Refresh before retrying.';
        if (connector) {
          if (typeof connector.enabled === 'boolean' && typeof connector.configured === 'boolean' && connector.transport === 'local') this.connectorStatus = connector;
          else this.connectorError = 'Connector configuration could not be verified.';
        }
        else if (this.canReadConnector && !this.connectorError) this.connectorError = 'Connector configuration returned no status.';
      },
      error: () => this.operationsError = 'Operational details could not be loaded. Refresh before retrying a delivery.',
    });
  }

  requestDeliveryRetry(failure: IEventDeliveryFailure): void {
    if (!this.deliveryRetryAllowed(failure.id)) return;
    this.retryConfirmationId = failure.id;
    this.retryMessage = '';
  }

  confirmDeliveryRetry(id: string): void {
    if (this.retryConfirmationId !== id || !this.deliveryRetryAllowed(id)) return;
    this.retryConfirmationId = '';
    this.retryingEventId = id;
    this.retryMessage = '';
    this.retrySubscription = this.systemStatusService.retryEventDelivery(id).pipe(
      timeout(READINESS_TIMEOUT_MS), take(1),
      finalize(() => this.retryingEventId = ''),
    ).subscribe({
      next: (result) => {
        if (result?.status !== 'queued' || result.eventId !== id) {
          this.deliveryError = 'Retry outcome is unverified. Refresh delivery status before taking another action.';
          return;
        }
        this.retryMessage = 'Retry queued. This does not prove delivery; a healthy event consumer is still required.';
        if (this.eventDelivery) this.eventDelivery = { ...this.eventDelivery, recentFailures: this.eventDelivery.recentFailures.filter((failure) => failure.id !== id) };
        this.deliveryError = 'Delivery totals may have changed. Refresh to verify the current state.';
      },
      error: () => this.deliveryError = 'Retry outcome is unknown. It may have been accepted; refresh status before retrying.',
      complete: () => {
        if (!this.retryMessage && !this.deliveryError) this.deliveryError = 'Retry returned no confirmation. Refresh status before retrying.';
      },
    });
  }

  deliveryRetryAllowed(id: string): boolean {
    return !this.destroyed && this.isAdvanced && this.operationsOpen && this.canAdministerDelivery
      && !this.operationsLoading && !this.operationsError && !this.deliveryError && !this.retryingEventId
      && this.eventDelivery?.recentFailures.some((failure) => failure.id === id && failure.status === 'dead_lettered') === true;
  }

  private validDeliveryStats(stats: IEventDeliveryStats): boolean {
    return [stats.pending, stats.published, stats.deadLettered].every((value) => Number.isSafeInteger(value) && value >= 0)
      && typeof stats.checkedAt === 'string' && Number.isFinite(Date.parse(stats.checkedAt))
      && Array.isArray(stats.recentFailures) && stats.recentFailures.every((failure) => failure && typeof failure.id === 'string' && failure.id.length > 0
        && ['pending', 'published', 'dead_lettered'].includes(failure.status)
        && Number.isSafeInteger(failure.attemptCount) && failure.attemptCount >= 0
        && Number.isSafeInteger(failure.maxAttempts) && failure.maxAttempts > 0);
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

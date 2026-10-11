import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  Inject,
  OnDestroy,
  OnInit,
} from '@angular/core';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { Subscription, finalize, fromEvent, timeout } from 'rxjs';
import {
  IA2ABridgeStatus,
  IEventDeliveryFailure,
  IEventDeliveryStats,
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

interface RecommendedAction {
  checkName: string;
  detail: string;
  severity: SystemCheckSeverity;
  route?: string;
  actionLabel?: string;
}

const RECOVERY_ROUTES: Record<string, { route: string; label: string }> = {
  llm: { route: '/llm-policy', label: 'Open model controls' },
  runtime: { route: '/runtime-control', label: 'Open runtime controls' },
  security: { route: '/governance-control', label: 'Review governance' },
  events: { route: '/system-status#event-delivery', label: 'Inspect delivery' },
};

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
  events: 'Automation delivery',
};

const READY_POLL_MS = 120000;
const DEGRADED_POLL_MS = 60000;
const RECOVERY_POLL_MS = 15000;
const READINESS_TIMEOUT_MS = 10000;
const EVENT_DELIVERY_TIMEOUT_MS = 10000;

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-system-status',
  templateUrl: './system-status.component.html',
  styleUrls: ['./system-status.component.scss'],
})
export class SystemStatusComponent implements OnInit, OnDestroy {
  readiness?: ISystemReadiness;
  groups: CheckGroup[] = [];
  recommendedActions: RecommendedAction[] = [];
  loading = false;
  loadError = false;
  lastUpdated?: Date;
  eventDelivery?: IEventDeliveryStats;
  eventDeliveryLoading = false;
  eventDeliveryError = false;
  connectorStatus?: IA2ABridgeStatus;
  connectorStatusLoading = false;
  connectorStatusError = false;
  retryingEventId = '';

  private visibilitySub?: Subscription;
  private pollTimer?: ReturnType<typeof setTimeout>;
  private readinessInFlight = false;
  private destroyed = false;

  constructor(
    @Inject(SYSTEM_STATUS_SERVICE_TOKEN)
    private systemStatusService: ISystemStatusService,
    private notification: NzNotificationService,
    @Inject(DOCUMENT) private document: Document
  ) {}

  ngOnInit(): void {
    this.visibilitySub = fromEvent(this.document, 'visibilitychange').subscribe(() => {
      if (this.document.hidden) {
        this.clearPollTimer();
        return;
      }
      this.refresh(true);
    });
    this.refresh();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.clearPollTimer();
    this.visibilitySub?.unsubscribe();
  }

  refresh(silent = false): void {
    this.clearPollTimer();
    if (!silent) {
      this.loadEventDelivery();
      this.loadConnectorStatus();
    }
    if (this.readinessInFlight) {
      return;
    }
    this.readinessInFlight = true;
    if (!silent) {
      this.loading = true;
    }
    this.systemStatusService.readiness().pipe(
      timeout(READINESS_TIMEOUT_MS),
      finalize(() => {
        this.readinessInFlight = false;
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
            'Could not load detailed system readiness. You may need to sign in again.'
          );
        }
      },
    });
  }

  private scheduleNextPoll(): void {
    if (this.destroyed || this.document.hidden) {
      return;
    }
    this.pollTimer = setTimeout(() => this.refresh(true), this.pollDelay());
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

  retryEventDelivery(failure: IEventDeliveryFailure): void {
    if (failure.status !== 'dead_lettered' || this.retryingEventId) {
      return;
    }
    this.retryingEventId = failure.id;
    this.systemStatusService.retryEventDelivery(failure.id).subscribe({
      next: () => {
        this.retryingEventId = '';
        this.notification.success(
          'Delivery queued',
          'HAI reset the bounded retry budget. Delivery still requires a healthy Kafka consumer.'
        );
        this.loadEventDelivery();
        this.refresh(true);
      },
      error: () => {
        this.retryingEventId = '';
        this.notification.error(
          'Retry failed',
          'The delivery was not changed. Refresh its status before trying again.'
        );
      },
    });
  }

  deliveryStateLabel(): string {
    if (!this.eventDelivery) return 'Unavailable';
    if (this.eventDelivery.deadLettered > 0) return 'Action required';
    if (this.eventDelivery.pending > 0) return 'Delivery pending';
    return 'Caught up';
  }

  private loadEventDelivery(): void {
    this.eventDeliveryLoading = true;
    this.systemStatusService.eventDelivery().pipe(timeout(EVENT_DELIVERY_TIMEOUT_MS)).subscribe({
      next: (stats) => {
        this.eventDelivery = stats;
        this.eventDeliveryError = false;
        this.eventDeliveryLoading = false;
      },
      error: () => {
        this.eventDeliveryError = true;
        this.eventDeliveryLoading = false;
      },
    });
  }

  private loadConnectorStatus(): void {
    this.connectorStatusLoading = true;
    this.systemStatusService.connectorStatus().pipe(timeout(EVENT_DELIVERY_TIMEOUT_MS)).subscribe({
      next: (status) => {
        this.connectorStatus = status;
        this.connectorStatusError = false;
        this.connectorStatusLoading = false;
      },
      error: () => {
        this.connectorStatusError = true;
        this.connectorStatusLoading = false;
      },
    });
  }

  connectorStateLabel(): string {
    if (this.connectorStatusError) return 'Status unavailable';
    if (!this.connectorStatus) return 'Checking connector';
    if (!this.connectorStatus.enabled) return 'Connector disabled';
    if (!this.connectorStatus.configured) return 'Configuration required';
    return this.connectorStatus.transport === 'fixed_ngrok_https'
      ? 'Governed cloud connector'
      : 'Local-only connector';
  }

  connectorStateClass(): string {
    if (this.connectorStatusError || (this.connectorStatus?.enabled && !this.connectorStatus.configured)) {
      return 'connector-warn';
    }
    if (this.connectorStatus?.configured) return 'connector-ok';
    return 'connector-off';
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
  private buildRecommendedActions(checks: ISystemCheck[]): RecommendedAction[] {
    const rank: Record<SystemCheckSeverity, number> = { fail: 0, warn: 1, ok: 2 };
    return checks
      .filter((c) => c.severity !== 'ok')
      .sort((a, b) => rank[a.severity] - rank[b.severity] || a.name.localeCompare(b.name))
      .map((check) => {
        const subsystem = check.name.split('.')[0] || '';
        const recovery = RECOVERY_ROUTES[subsystem];
        return {
          checkName: check.name,
          detail: check.detail,
          severity: check.severity,
          route: recovery?.route,
          actionLabel: recovery?.label,
        };
      });
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

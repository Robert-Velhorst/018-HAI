// Mirrors the authenticated backend system readiness payload. The three-way
// status is meaningful and must be preserved end to end: "degraded" is a
// serving-but-incomplete state that a two-way up/down view would erase.
export type SystemCheckSeverity = 'ok' | 'warn' | 'fail';

export type SystemReadinessStatus = 'ready' | 'degraded' | 'not_ready';

export interface ISystemCheck {
  name: string;
  severity: SystemCheckSeverity;
  detail: string;
}

export interface ISystemReadinessSummary {
  ok: number;
  warn: number;
  fail: number;
}

export interface ISystemReadiness {
  status: SystemReadinessStatus;
  service: string;
  summary: ISystemReadinessSummary;
  checks: ISystemCheck[];
}

export type EventDeliveryStatus = 'pending' | 'published' | 'dead_lettered';

export interface IEventDeliveryFailure {
  id: string;
  aggregateId: string;
  eventType: 'create' | 'update' | 'delete';
  status: EventDeliveryStatus;
  attemptCount: number;
  maxAttempts: number;
  lastError: string;
  updatedAt: string;
}

export interface IEventDeliveryStats {
  pending: number;
  deadLettered: number;
  published: number;
  oldestPendingAt?: string;
  recentFailures: IEventDeliveryFailure[];
  checkedAt: string;
}

export interface IEventDeliveryRetryResult {
  status: 'queued';
  eventId: string;
}

export interface IA2ABridgeStatus {
  enabled: boolean;
  configured: boolean;
  provider: string;
  endpoint?: string;
  configError?: string;
  capabilities: string[];
  restrictions: string[];
  scope: string;
  transport: 'local' | 'fixed_ngrok_https';
}

export interface IAutomationModel {
  image: string;
  name: string;
  urlPath?: string;
  port: number;
  position: number;
  host: string;
  id?: string;
  imageFile?: File;
  removeImage: boolean;

  launchType?: string;
  launchTarget?: string;
  runtimeType?: string;
  runtimeModel?: string;
  serviceName?: string;
  routePath?: string;
  publicUrl?: string;
  localUrl?: string;
  dependencyNotes?: string;
  healthCheckType?: string;
  healthCheckUrl?: string;
  healthCheckIntervalSeconds?: number;
  expectedHttpStatus?: number;
  status?: string;
  lastCheckedAt?: string;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  lastFailureReason?: string;
  consecutiveFailures?: number;
  averageLatencyMs?: number;
  lastLaunchAt?: string;
}

export interface IAutomationHealthSummary {
  total: number;
  healthy: number;
  warning: number;
  degraded: number;
  broken: number;
  unknown: number;
  checkedAt: string;
}

export interface IAutomationHealthResult {
  automationId: string;
  status: string;
  checkedAt: string;
  latencyMs: number;
  failureReason?: string;
  consecutiveFailures: number;
}

export interface IAutomationLaunchResult {
  automationId: string;
  launchEventId?: string;
  runtimeTaskId?: string;
  runtimeType?: string;
  launchType: string;
  target: string;
  status: string;
  message?: string;
  output?: string;
  runtimeRouteTrace?: IAutomationRuntimeRouteTrace;
  exitCode: number;
  durationMs: number;
  requiresApproval: boolean;
  auditEvents: string[];
  launchedAt: string;
}

export interface IAutomationLaunchEvent {
  id?: string;
  automationId: string;
  runtimeType?: string;
  launchType: string;
  runtimeTaskId?: string;
  target?: string;
  status: string;
  message?: string;
  output?: string;
  runtimeRouteTrace?: IAutomationRuntimeRouteTrace;
  auditEvents?: string[];
  exitCode: number;
  durationMs: number;
  startedAt: string;
  completedAt: string;
}

export interface IAutomationRuntimeRouteTrace {
  runtimeId: string;
  intent?: string;
  executionMode?: string;
  riskLevel?: string;
  recommendedSkills?: string[];
  visibleProviders?: string[];
  visibleTools?: string[];
  relevantMaps?: string[];
  blockedSurfaces?: string[];
  requiredControls?: string[];
  validationChecklist?: string[];
}

export interface IAutomationHealthEvent {
  id?: string;
  automationId: string;
  status: string;
  checkType: string;
  target?: string;
  latencyMs: number;
  failureReason?: string;
  consecutiveFailures: number;
  checkedAt: string;
}

export interface IAutomationDiagnostics {
  automationId: string;
  name: string;
  status: string;
  launchTarget: string;
  healthCheckTarget: string;
  routePath: string;
  host: string;
  port: number;
  lastCheckedAt?: string;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  lastFailureReason?: string;
  checks: Record<string, string>;
  recentEvents: IAutomationHealthEvent[];
  recentLaunches: IAutomationLaunchEvent[];
}

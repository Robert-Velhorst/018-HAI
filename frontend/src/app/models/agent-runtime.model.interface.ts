export interface IAgentRuntimeInfo {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  configured: boolean;
  executionEnabled: boolean;
  requiresApproval: boolean;
  readOnlyDefault: boolean;
  capabilities: string[];
  architecture?: string[];
  controls?: string[];
  ecosystem?: IAgentRuntimeEcosystemSurface[];
  ecosystemPath?: string;
  ecosystemRollbackAvailable: boolean;
  missingConfiguration?: string[];
  endpoint?: string;
}

// Short-lived, server-issued references for one exact OpenClaw ecosystem
// mutation. They are deliberately kept in-memory by the caller and are never
// persisted in browser storage.
export interface IAgentRuntimeEcosystemAuthorization {
  idempotencyKey: string;
  taskId: string;
  approvalSourceId: string;
  approvalBindingDigest: string;
}

export interface IAgentRuntimeEcosystemSurface {
  category: string;
  status: string;
  count: number;
  items?: string[];
  more?: number;
  control?: string;
  riskLevel?: string;
  approvalRequired?: boolean;
}

export interface IAgentRuntimeSkill {
  id: string;
  runtimeId: string;
  name: string;
  category: string;
  riskLevel: string;
  approvalRequired: boolean;
  executionMode: string;
  source?: string;
  description?: string;
  tags?: string[];
}

export interface IAgentRuntimeStopResult {
  runtimeId: string;
  taskId: string;
  status: string;
  message?: string;
  evidenceUri?: string;
  auditEvents?: string[];
}

export interface IAgentRuntimeHealth {
  runtimeId: string;
  status: string;
  reason: string;
  version?: string;
  gatewayTaskLedger?: IAgentRuntimeGatewayTaskLedger;
  checkedAt: string;
  latencyMs: number;
}

export interface IAgentRuntimeGatewayTaskLedger {
  sampledTasks: number;
  statusCounts: Record<string, number>;
  truncated: boolean;
}

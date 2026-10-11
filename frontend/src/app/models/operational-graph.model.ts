export interface OperationalGraphNode {
  id: string
  kind: string
  layer: string
  label: string
  summary?: string
  status?: string
  weight: number
  parentId?: string
  projectKeys?: string[]
  tags?: string[]
  verificationStatus?: string
  sensitivity?: string
  localOnly: boolean
  sourceCount: number
  details?: Record<string, string>
  updatedAt?: string
}

export interface OperationalGraphLink {
  id: string
  sourceId: string
  targetId: string
  type: string
  label?: string
  weight: number
}

export interface OperationalGraphSnapshot {
  contractVersion: number
  generatedAt: string
  rootId: string
  nodes: OperationalGraphNode[]
  links: OperationalGraphLink[]
  layerCounts: Record<string, number>
  quality: {
    orphanNodes: number
    sourceBackedNodes: number
    needsReviewNodes: number
    localOnlyNodes: number
    blockedNodes: number
  }
  truncated: boolean
  warnings?: string[]
  scope: string
}

export interface OperationalGraphSearch {
  query: string
  results: OperationalGraphNode[]
  total: number
  truncated: boolean
  explanation: string
}

export interface OperationalNeighborhood {
  rootId: string
  depth: number
  nodes: OperationalGraphNode[]
  links: OperationalGraphLink[]
  truncated: boolean
  explanation: string
}

export interface AgentBootContext {
  contractVersion: number
  generatedAt: string
  agentId: string
  agentName: string
  state: string
  health: string
  runtimeId: string
  runtimeType: string
  capabilities: string[]
  teams: Array<{
    id: string
    name: string
    version: string
    status: string
    roleIds: string[]
    capabilityIds: string[]
    authorityCeiling: number
    riskCeiling: string
    advisoryOnly: boolean
  }>
  authorityCeiling: number
  autonomyCeiling: number
  riskCeiling: string
  toolAllowlist: string[]
  dataAllowlist: string[]
  folderAllowlist: string[]
  prohibitedActions: string[]
  evidenceRequirements: string[]
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
  explanation: string
}

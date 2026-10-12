export interface IOperation {
  id: string
  ownerUserId: string
  workspaceId: string
  title: string
  description?: string
  sourceType: string
  sourceUri?: string
  operationType: string
  status: string
  riskLevel: string
  autonomyLevel: string
  ownerType: string
  currentDecision: string
  requiresApproval: boolean
  recommendedAction?: string
  runtimeId?: string
  verificationStatus: string
  resultSummary?: string
  lastError?: string
  nextReviewAt?: string
  createdAt: string
  updatedAt: string
  completedAt?: string
}

export interface IOperationEvent {
  id: string
  operationId: string
  eventType: string
  actorType: string
  actorId?: string
  beforeStatus?: string
  afterStatus?: string
  message?: string
  createdAt: string
}

export interface IOperationsDashboard {
  countsByStatus: Record<string, number>
  countsByRisk: Record<string, number>
  needsRobert: number
  doneWhileAway: number
  blocked: number
  running: number
  failed: number
  recent: IOperation[]
}

export interface IBackgroundOperationsOverview {
  dashboard: IOperationsDashboard
  operations: IOperation[]
  feeds: IAccountFeed[]
}

export interface IBackgroundRunReport {
  feedsRead: number
  itemsIngested: number
  operationsCreated: number
  classified: number
  autoExecuted: number
  verified: number
  failed: number
  awaitingApproval: number
  blocked: number
  drafted: number
  observed: number
  errors?: string[]
}

export interface IAccountFeed {
  name: string
  provider: string
  accountLabel: string
  sourceType: string
  enabled: boolean
}

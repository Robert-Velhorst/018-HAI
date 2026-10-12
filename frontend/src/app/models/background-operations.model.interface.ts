import { ISafeOutcomeState, outcomeRecord, readSafeOutcomeState, receiptConsistentWithCompletion, safeOutcomeText } from './safe-outcome.model.interface'

export interface IOperationRunResult extends ISafeOutcomeState {
  operation?: Pick<IOperation, 'id' | 'status' | 'verificationStatus'> & Partial<IOperation> | null
  operationId?: string
  operationSnapshot?: boolean
  verified: boolean
  failed: boolean
  error?: string
}

export function readOperationRunResult(value: unknown): IOperationRunResult | undefined {
  const body = outcomeRecord(value)
  if (!body || !['verified', 'failed', 'interrupted', 'reconciliationRequired', 'receipt'].some((key) => key in body)) return undefined
  const operation = outcomeRecord(body['operation'])
  return {
    ...readSafeOutcomeState(body),
    operationId: typeof body['operationId'] === 'string' ? body['operationId'] : undefined,
    operationSnapshot: typeof body['operationSnapshot'] === 'boolean' ? body['operationSnapshot'] : undefined,
    operation: operation ? {
      id: typeof operation['id'] === 'string' ? operation['id'] : '',
      status: typeof operation['status'] === 'string' ? operation['status'] : '',
      verificationStatus: typeof operation['verificationStatus'] === 'string' ? operation['verificationStatus'] : '',
      resultSummary: safeOutcomeText(operation['resultSummary']),
      lastError: safeOutcomeText(operation['lastError']),
    } : null,
    verified: body['verified'] === true,
    failed: body['failed'] === true,
    error: safeOutcomeText(body['error']),
  }
}

export function operationRunVerified(result: IOperationRunResult): boolean {
  return result.verified && !result.failed && !result.interrupted && !result.reconciliationRequired &&
    !result.error && result.outcomeRecorded !== false && result.operationSnapshot !== false &&
    receiptConsistentWithCompletion(result.receipt) &&
    result.operation?.status === 'completed' &&
    result.operation.verificationStatus === 'passed'
}

export interface IOperation {
  id: string
  /** Monotonic backend revision. Required at runtime for source-derived approval. */
  version?: number
  ownerUserId: string
  workspaceId: string
  title: string
  description?: string
  sourceType: string
  sourceId?: string | null
  sourceUri?: string
  sourceRevisionHash?: string
  sourceIdentityHash?: string
  sourceObservationId?: string | null
  accountFeedId?: string | null
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

export interface IOperationApprovalBinding {
  expectedVersion: number
  revisionDigest: string
}

export interface IOperationApprovalPreview {
  operation: IOperation
  revision: {
    operationId: string
    version: number
    revisionDigest: string
  }
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

export interface IOperationsOverview {
  dashboard: IOperationsDashboard
  operations: IOperation[]
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

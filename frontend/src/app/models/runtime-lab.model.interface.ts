import { ISafeOutcomeState, outcomeRecord, readSafeOutcomeState, receiptConsistentWithCompletion, safeOutcomeText, safeReceiptMalformed } from './safe-outcome.model.interface'

export interface IRuntimeInfo {
  id: string
  displayName: string
  kind: string
  description: string
}

export interface ISetupRequirement {
  step: string
  detail: string
}

export interface IRuntimeAttempt extends ISafeOutcomeState {
  id: string
  runtimeId: string
  operationId?: string
  status: string
  detail: string
  boundedOutput?: string
  verificationPassed: boolean
  operationStatus?: string
  idempotencyKey?: string
  discoveryRecovered?: boolean
  createdAt: string
}

export function readRuntimeAttempt(value: unknown): IRuntimeAttempt | undefined {
  const body = outcomeRecord(value)
  if (!body || typeof body['runtimeId'] !== 'string' || typeof body['status'] !== 'string') return undefined
  return {
    ...readSafeOutcomeState(body),
    id: typeof body['id'] === 'string' ? body['id'] : '',
    runtimeId: body['runtimeId'],
    operationId: typeof body['operationId'] === 'string' ? body['operationId'] : undefined,
    operationStatus: typeof body['operationStatus'] === 'string' ? body['operationStatus'] : undefined,
    idempotencyKey: typeof body['idempotencyKey'] === 'string' ? body['idempotencyKey'] : undefined,
    discoveryRecovered: body['discoveryRecovered'] === true,
    status: body['status'],
    detail: safeOutcomeText(body['detail']),
    boundedOutput: safeOutcomeText(body['boundedOutput']),
    verificationPassed: body['verificationPassed'] === true,
    createdAt: typeof body['createdAt'] === 'string' ? body['createdAt'] : '',
  }
}

export function runtimeAttemptVerified(attempt: IRuntimeAttempt): boolean {
  return attempt.status === 'succeeded' && attempt.verificationPassed === true &&
    attempt.operationStatus === 'completed' && !attempt.discoveryRecovered && !attempt.interrupted && !attempt.reconciliationRequired &&
    receiptConsistentWithCompletion(attempt.receipt) && (!attempt.receipt || attempt.receipt.runtimeId === attempt.runtimeId) &&
    attempt.outcomeRecorded !== false
}

export function runtimeAttemptUncertain(attempt: IRuntimeAttempt): boolean {
  const effectCorrelation = !!attempt.operationId || !!attempt.receipt || !!attempt.operationStatus
  const discoveryOnly = attempt.discoveryRecovered === true && !effectCorrelation && !attempt.verificationPassed
  const knownStatus = ['pending', 'running', 'succeeded', 'failed', 'blocked', 'setup_required', 'inconclusive'].includes(attempt.status)
  return attempt.reconciliationRequired === true || attempt.interrupted === true || safeReceiptMalformed(attempt.receipt) ||
    attempt.operationStatus === 'interrupted' ||
    attempt.operationStatus === 'running' || attempt.operationStatus === 'verifying' || attempt.status === 'running' ||
    (attempt.status === 'pending' && effectCorrelation) || (!knownStatus && effectCorrelation) ||
    (attempt.status === 'inconclusive' && effectCorrelation) ||
    (attempt.verificationPassed === true && attempt.status !== 'succeeded') ||
    (attempt.operationStatus === 'completed' && attempt.status !== 'succeeded') ||
    (['failed', 'blocked', 'setup_required'].includes(attempt.status) &&
      (!!attempt.receipt?.ok || !!attempt.receipt?.verification?.passed || !!attempt.receipt?.output?.artifactHash)) ||
    (attempt.status === 'succeeded' && !discoveryOnly && !runtimeAttemptVerified(attempt))
}

export interface IRuntimeSummary {
  info: IRuntimeInfo
  status: string
  claimLevel: string
  canExecute: boolean
  capabilities: string[]
  setupRequirements?: ISetupRequirement[]
  lastAttempt?: IRuntimeAttempt
}

export interface IRuntimeProbe {
  runtimeId: string
  status: string
  discoveryState: string
  readinessLevel: string
  protocol?: string
  runtimeVersion?: string
  protocolValid: boolean
  identityVerified: boolean
  authenticated: boolean
  capabilities?: string[]
  gatewayTaskLedger?: {
    sampledTasks: number
    statusCounts: Record<string, number>
    truncated: boolean
  }
  gatewayCapabilityCatalog?: {
    sampledSkills: number
    eligibleSkills: number
    sampledCommands: number
    toolCountsBySource: Record<string, number>
  }
  gatewayPreparedModelCatalog?: {
    sampledModels: number
    availableModels: number
    unavailableModels: number
    unknownAvailabilityModels: number
  }
  gatewayAgentRoster?: {
    sampledAgents: number
    agentCount: number
    systemCount: number
    unknownKindCount: number
  }
  evidenceSha256?: string
  durationMs: number
  detail: string
  checkedAt: string
}

export type RuntimeFeatureDisposition =
  | 'integrated_directly'
  | 'adapted_for_hai'
  | 'hai_native_reimplementation'
  | 'already_present'
  | 'consolidated_existing'
  | 'constrained_unsafe'
  | 'excluded_irrelevant'
  | 'excluded_incompatible_license'
  | 'deferred'
  | 'blocked_external'

export interface IRuntimeFeature {
  id: string
  name: string
  purpose: string
  behavior: string
  coverageAreas: string[]
  dependencies: string[]
  license: string
  securityImplications: string
  haiEquivalent?: string
  integrationApproach: string
  disposition: RuntimeFeatureDisposition
  implementationStatus: string
  testStatus: string
  documentationStatus: string
  exclusionReason?: string
  backlogPriority?: string
  requirements?: string[]
  recommendedPath?: string
  sourceUrls: string[]
}

export interface IRuntimeParityInventory {
  runtimeId: string
  project: string
  repositoryUrl: string
  defaultBranch: string
  reviewedRevision: string
  reviewedRelease?: string
  reviewedAt: string
  license: string
  licensePolicy: string
  readinessCeiling: string
  canonicalAuthority: string
  features: IRuntimeFeature[]
}

export interface IRuntimeParityOverview {
  requiredCoverageAreas: string[]
  inventories: IRuntimeParityInventory[]
  dispositionCounts: Record<string, number>
  implementationCounts: Record<string, number>
  generatedAt: string
}

export interface IRuntimeCapabilityCard {
  id: string
  runtimeId: string
  name: string
  purpose: string
  inputSchema: Record<string, unknown>
  outputSchema: Record<string, unknown>
  authenticationState: string
  availability: string
  runtimeLocation: string
  requiredAuthority: string[]
  riskLevel: string
  expectedCostEurMax: number
  costPolicy: string
  contextCost: string
  timeoutSeconds: number
  retryBehaviour: string
  reversibility: string
  approvalRequirements: string[]
  verificationMethod: string
  evidenceReturned: string[]
  readinessLevel: string
  readinessReason: string
  canInvoke: boolean
  canExecuteExternalEffect: boolean
  latestDiscovery?: IRuntimeProbe
  sourceFeatureIds: string[]
}

export interface IRuntimeCapabilityOverview {
  cards: IRuntimeCapabilityCard[]
  counts: Record<string, number>
  authority: string
  safetyNote: string
}

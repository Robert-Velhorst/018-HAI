export interface IProviderSummary {
  id: string
  name: string
  status: string
  claimLevel: string
  local: boolean
  endpointLocal: boolean
  localInferenceOperatorAttested: boolean
  billingStatus: string
  deterministic: boolean
  models: number
}

export interface ITelemetryPersistenceStatus {
  state: 'durable' | 'memory_only' | 'degraded' | string
  message: string
}

export interface ILaneWinner {
  lane: string
  providerId: string
  modelId: string
  tokensPerSecond: number
  observedSpeedSamples?: number
  runs: number
  evaluatedRuns: number
  acceptedOutputs: number
  acceptanceRate: number
  confidence: string
  averageTokens: number
  averageDurationMs: number
  averageCostEur: number
  reason: string
}

export interface IModelIntelligenceOverview {
  providers: IProviderSummary[]
  lanes: string[]
  totalProfiles: number
  activeModels: number
  telemetryRuns: number
  evaluatedRuns: number
  acceptedOutputs: number
  unvalidatedRuns: number
  cacheHits: number
  cacheMisses: number
  deterministicProfiles: number
  laneWinners: ILaneWinner[]
  calibration: ICalibrationSummary
  telemetryPersistence: ITelemetryPersistenceStatus
}

export interface IModelProfile {
  providerId: string
  modelId: string
  displayName: string
  architectureFamily: string
  lanes: string[]
  contextWindow: number
  local: boolean
  endpointLocal: boolean
  localInferenceOperatorAttested: boolean
  deterministic: boolean
  paid: boolean | null
  billingStatus: string
  status: string
  claimLevel: string
  observedTokensPerSecond: number
  observedRuns: number
  observedFailures: number
  lastBenchmarkedAt?: string
}

export interface IBenchmarkResult {
  providerId: string
  modelId: string
  ok: boolean
  inputTokens: number
  outputTokens: number
  usageSource: string
  durationMs: number
  tokensPerSecond: number
  claimLevel: string
  detail?: string
}

export interface IModelTelemetry {
  id: string
  providerId: string
  modelId: string
  lane: string
  operationId?: string
  inputTokens: number
  outputTokens: number
  usageSource: string
  durationMs: number
  tokensPerSecond: number
  ok: boolean
  cacheHit: boolean
  validationStatus: string
  validationMethod?: string
  estimatedCostEur: number
  fallbackDepth: number
  createdAt: string
}

export interface IModelCalibration {
  lane: string
  providerId: string
  modelId: string
  totalRuns: number
  providerCallSuccesses: number
  providerCallFailures: number
  evaluatedRuns: number
  acceptedOutputs: number
  rejectedOutputs: number
  needsReview: number
  unvalidatedRuns: number
  acceptanceRate: number
  wilsonLowerBound: number
  providerReportedUsageRuns: number
  partialUsageRuns: number
  estimatedUsageRuns: number
  invalidUsageRuns: number
  averageProviderReportedInputTokens: number
  averageProviderReportedOutputTokens: number
  averagePartialInputTokens: number
  averagePartialOutputTokens: number
  averageEstimatedInputTokens: number
  averageEstimatedOutputTokens: number
  observedSpeedSamples: number
  averageInputTokens: number
  averageOutputTokens: number
  averageDurationMs: number
  averageTokensPerSecond: number
  averageCostEur: number
  averageFallbackDepth: number
  confidence: string
  lastObservedAt: string
}

export interface ICalibrationSummary {
  totalRuns: number
  evaluatedRuns: number
  acceptedOutputs: number
  rejectedOutputs: number
  needsReview: number
  unvalidatedRuns: number
  models: IModelCalibration[]
  laneLeaders: ILaneWinner[]
  generatedAt: string
  explanation: string
}

export interface IOperationBudget {
  maximumInputTokens: number
  maximumOutputTokens: number
  maximumReasoningEffort: string
  maximumContextItems: number
  maximumSourceBytes: number
  contextStrategy: string
  cacheStrategy: string
  batchEligible: boolean
}

export interface IHardwareProfile {
  operatingSystem: string
  windowsVersion: string
  cpuCores: number
  gpuVendor: string
  npuVendor: string
  executionProviders: string[]
  powerMode: string
  batteryStatus: string
}

export interface IHardwareResponse {
  profile: IHardwareProfile
  selectedServingStack: string
}

export interface IPowerPolicy {
  mode: string
  allowHeavyWorkNow: boolean
  deferHeavyWorkOnBattery: boolean
  nightBatchOnly: boolean
}

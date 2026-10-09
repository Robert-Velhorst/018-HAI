export interface ILLMPolicy {
  dailyPaidBudgetEur: number;
  paidCallsAllowed: boolean;
  localModelsAllowed: boolean;
  freeCloudQuotaAllowed: boolean;
  localFirst: boolean;
  cacheRepeatedPrompts: boolean;
  routeSimpleTasksToSmallModels: boolean;
  routeComplexTasksToBestAvailableFreeModel: boolean;
  requireApprovalBeforePaidUsage: boolean;
  requireRecentLiveProviderProbe: boolean;
  providerProbeMaxAgeSeconds: number;
  tierOrder: string[];
  dailyBudgetUsedEur: number;
  inputTokensUsed: number;
  outputTokensUsed: number;
  usageAccountingStatus?: 'durable' | 'process_only' | 'unavailable' | string;
  usagePeriodStart?: string;
  usageTimezone?: string;
  providers: ILLMProvider[];
  inferenceInfrastructure: ILLMInferenceInfrastructure;
}

export interface ILLMInferenceInfrastructure {
  kvCacheLoadStrategy: string;
  disaggregatedServingVerified: boolean;
  dualPathInfrastructureAvailable: boolean;
  reason: string;
}

export interface ILLMProvider {
  id: string;
  name: string;
  enabled: boolean;
  local: boolean;
  paid: boolean;
  endpointUrl?: string;
  apiKeyEnv?: string;
  configured: boolean;
  readinessStatus?: string;
  readinessReason?: string;
  quotaRemaining: number;
  dailyBudgetEur: number;
  budgetUsedEur: number;
  inputTokensUsed: number;
  outputTokensUsed: number;
  usageAccountingStatus?: 'durable' | 'process_only' | 'unavailable' | string;
  models: ILLMModel[];
}

export interface ILLMProviderProbe {
  providerId: string;
  providerName: string;
  status: string;
  reason: string;
  endpointUrl?: string;
  httpStatus?: number;
  modelsSeen: number;
  durationMs: number;
  live: boolean;
  requiresReview: boolean;
  checkedAt: string;
  lastSuccessfulAt?: string;
}

export interface ILLMModelMaintenanceResult {
  providerId: string;
  providerName: string;
  modelId: string;
  modelName: string;
  status: string;
  reason: string;
  previousDigest?: string;
  currentDigest?: string;
  configurationChanged: boolean;
  updateAttempted: boolean;
  updateApplied: boolean;
  blocksExecution: boolean;
  reused: boolean;
  checkedAt: string;
  nextCheckDueAt?: string;
}

export interface ILLMModelMaintenanceRun {
  eligible: number;
  checked: number;
  providerManaged: number;
  healthOnly: number;
  reused: number;
  updated: number;
  inProgress: number;
  failed: number;
  cancelled: boolean;
  results: ILLMModelMaintenanceResult[];
  runAt: string;
}

export interface ILLMModel {
  id: string;
  name: string;
  tier: string;
  capabilities: string[];
  maxDifficulty: number;
  maxReasoning: string;
  estimatedCostEur: number;
  inputCostPerMillionTokensEur: number;
  outputCostPerMillionTokensEur: number;
  pricingUnit?: string;
  pricingSource?: string;
  requiresApproval: boolean;
  enabled: boolean;
  budgetUsedEur: number;
  inputTokensUsed: number;
  outputTokensUsed: number;
  usageAccountingStatus?: 'durable' | 'process_only' | 'unavailable' | string;
}

export interface ILLMRouteRequest {
  task: string;
  taskType?: string;
  difficulty?: number;
  requiredReasoning?: string;
  validationPassed?: boolean;
  previousModelId?: string;
}

export interface ILLMTaskClassification {
  taskType: string;
  difficulty: number;
  requiredReasoning: string;
  requiredCapabilities: string[];
  reason: string;
}

export interface ILLMFallbackOption {
  providerId: string;
  modelId: string;
  modelName: string;
  tier: string;
  estimatedCostEur: number;
  estimatedInputTokens: number;
  estimatedOutputTokens: number;
  requiresApproval: boolean;
}

export interface ILLMSkippedModel {
  providerId: string;
  modelId: string;
  reason: string;
}

export interface ILLMRouteCalibration {
  lane: string;
  evaluatedRuns: number;
  acceptedOutputs: number;
  acceptanceRate: number;
  wilsonLowerBound: number;
  confidence: string;
}

export interface ILLMRouteDecision {
  selectedProviderId: string;
  selectedModelId: string;
  selectedModelName: string;
  tier: string;
  reason: string;
  estimatedCostEur: number;
  estimatedInputTokens: number;
  estimatedOutputTokens: number;
  pricingSource?: string;
  requiresApproval: boolean;
  classification: ILLMTaskClassification;
  fallbackPath: ILLMFallbackOption[];
  skipped: ILLMSkippedModel[];
  calibration?: ILLMRouteCalibration;
  loggedAt: string;
}

export interface ILLMGenerationResult {
  generationId?: string;
  telemetryId?: string;
  providerId: string;
  modelId: string;
  modelName: string;
  tier: string;
  output: string;
  status: string;
  reason: string;
  estimatedCostEur: number;
  inputTokens?: number;
  outputTokens?: number;
  usageSource?: string;
  auditStatus?: string;
  durationMs: number;
  fallbackPath: string[];
  fallbackDepth?: number;
  validationStatus?: string;
  validationMethod?: string;
  calibrationAudit?: string;
  loggedAt: string;
}

import { ILLMGenerationResult, ILLMRouteDecision } from './llm-policy.model.interface';
import { IRankedMemory } from './context-memory.model.interface';
import { IRankedExtraction, IScheduledSyncRun } from './connected-source.model.interface';
import { IVerificationClaim } from './verification.model.interface';
import { IAutomationRuntimeRouteTrace } from './automation.model.interface';
import { IFrameworkSelectionDecision } from './framework-registry.model.interface';
import { IPlanTransportEdge, IPlanTransportNode } from './plan-graph.model.interface';

export interface ITaskPlanRequest {
  request: string;
  idempotencyKey?: string;
  projectKey?: string;
  pursuitId?: string;
  automationId?: string;
  mandateId?: string;
  successCriteria?: string[];
  includeRagflowCandidates?: boolean;
  executeAllowed?: boolean;
  humanApproved?: boolean;
  approvalNote?: string;
}

export interface ITaskPlanHistoryItem {
  id: string;
  createdAt: string;
  request: string;
  projectKey?: string;
  intake: {
    taskType: string;
    successCriteria: string[];
  };
  completionStatus: string;
}

export interface IIntakeAnalysis {
  taskType: string;
  riskLevel: string;
  difficulty: number;
  requiredReasoning: string;
  successCriteria: string[];
  needsMemory: boolean;
  needsTools: boolean;
  needsDocuments: boolean;
  needsWebAccess: boolean;
  needsLocalExecution: boolean;
  needsApproval: boolean;
  reason: string;
}

export interface IContextPlan {
  strategy: string[];
  usedContext: IRankedMemory[];
  sourceContext: IRankedExtraction[];
  ragflowCandidates?: IRAGFlowCandidateContext[];
  ragflowExplanation?: string;
  sourceRefresh?: IScheduledSyncRun;
  sourceRefreshExplanation?: string;
  explanation: string;
}

export interface IRAGFlowCandidateContext {
  status: 'unverified_candidate' | string;
  sourceUri: string;
  datasetId: string;
  documentId?: string;
  documentName?: string;
  chunkId: string;
  snippet: string;
  similarity?: number;
  scope: string;
}

export interface IValidationPlan {
  steps: string[];
  successCriteria: string[];
  frameworkEvidenceRequirements: string[];
  frameworkCompletionCriteria: string[];
  frameworkAssuranceCriteria: string[];
  failurePolicy: string;
  completionGate: string;
}

export interface IExecutionPlan {
  planningSeparatedFromExecution: boolean;
  controlledExecutionMode: string;
  approvalRequiredFor: string[];
  auditEvents: string[];
	capacityConstraints?: string[];
}

export interface ICalendarBusyInterval {
  start: string;
  end: string;
  title: string;
  sourceUri?: string;
  sourceId: string;
}

export interface ICalendarCapacityContext {
  status: 'source_backed' | 'unavailable' | 'not_applicable' | string;
  windowStart: string;
  windowEnd: string;
  busyIntervals: ICalendarBusyInterval[];
  explanation: string;
}

export interface IResourceScheduledTask {
  taskId: string;
  start: string;
  end: string;
  plannedDurationMinutes: number;
  critical: boolean;
  dependencies: string[];
}

export interface IResourceBlocker {
  code: string;
  taskId?: string;
  resourceId?: string;
  detail: string;
  blocksFeasibility: boolean;
}

export interface IResourceApprovalFlag {
  code: string;
  taskId?: string;
  reason: string;
  mandatory: boolean;
}

export interface IResourceDecision {
  planId: string;
  algorithmVersion: string;
  decisionDigest: string;
  feasibility: 'feasible' | 'feasible_with_approvals' | 'infeasible' | string;
  scheduled: IResourceScheduledTask[];
  unscheduledTaskIds: string[];
  criticalBlockers: IResourceBlocker[];
  advisories: IResourceBlocker[];
  approvalFlags: IResourceApprovalFlag[];
  authority: string;
  canExecute: boolean;
  grantsAuthority: boolean;
}

export interface IMinimalityGate {
  key: string;
  label: string;
  status: string;
  evidence: string;
}

export interface IMinimalityDecision {
  applicable: boolean;
  necessary: boolean;
  selectedLevel: string;
  selectedStrategy: string;
  reason: string;
  ladder: IMinimalityGate[];
  newDependenciesAllowed: boolean;
  customArchitectureAllowed: boolean;
  requiresRepositoryCheck: boolean;
  benchmarkClaimsStatus: string;
}

export interface IExecutedAction {
  name: string;
  status: string;
  input?: string;
  output?: string;
  startedAt: string;
  endedAt: string;
}

export interface IExecutionResult {
  startedAt: string;
  completedAt: string;
  mode: string;
  output: string;
  verificationStatus: string;
  claims: IVerificationClaim[];
  evidenceCount: number;
  unsupportedClaims: number;
  llmGeneration?: ILLMGenerationResult;
  toolExecution?: IToolExecutionResult;
  actions: IExecutedAction[];
  blockedReason?: string;
}

export interface IToolExecutionResult {
  automationId: string;
  launchEventId?: string;
  runtimeType?: string;
  launchType: string;
  target?: string;
  status: string;
  message?: string;
  output?: string;
  runtimeRouteTrace?: IAutomationRuntimeRouteTrace;
  exitCode: number;
  durationMs: number;
  requiresApproval: boolean;
  auditEvents: string[];
  executedAt: string;
}

export interface IToolRouteDecision {
  selectedTools: string[];
  skippedTools: string[];
  blockedTools: string[];
  catalogRecommendations?: ICatalogRecommendation[];
  capabilityRecommendations?: ICapabilityRecommendation[];
  reason: string;
}

export interface ICatalogRecommendation {
  id: string;
  name: string;
  status: string;
  role: string;
  rationale: string;
  requiresApproval: boolean;
  activation: string;
  upstreamUrl?: string;
  sourceCatalogUrl?: string;
  sourceCollection?: string;
  verifiedAt?: string;
  verificationNote?: string;
}

export interface ICapabilityRecommendation extends ICatalogRecommendation {
  score: number;
  matchedTerms: string[];
  roadmapPriority: number;
  roadmapReason: string;
  capabilityPlanes: string[];
  reasons: string[];
  nextStep: string;
}

export interface ITaskStep {
  id: string;
  name: string;
  purpose: string;
  allowed: boolean;
  requiresApproval: boolean;
  status: string;
}

export interface IRiskAssessment {
  level: string;
  approvalRequired: boolean;
  approvalGranted: boolean;
  actionResolution?: 'proceed' | 'clarify' | 'block';
  missingParameters?: string[];
  frameworkAutonomyCeiling?: number;
  requiredFrameworkAutonomy?: number;
  reasons: string[];
  allowedNow: boolean;
}

export interface IValidationCriterionResult {
  criterion: string;
  kind: 'task_success' | 'framework_evidence' | 'framework_completion' | 'framework_assurance' | 'system_check' | string;
  status: 'not_run' | 'passed' | 'failed' | 'not_applicable' | string;
  evidence: string[];
  applicabilityReason?: string;
  failure?: string;
}

export interface IValidationResult {
  passed: boolean;
  status: string;
  checked: string[];
  failures: string[];
  criteria: IValidationCriterionResult[];
  nextAction: string;
  attemptNumber: number;
}

export interface IRetryPolicy {
  maxAttempts: number;
  escalationPath: string[];
  escalateWhen: string[];
  currentAttempt: number;
  retryAvailable: boolean;
}

export interface IReviewQueueItem {
  id: string;
  taskId: string;
  request: ITaskPlanRequest;
  reason: string;
  priority: string;
  status: string;
  decision?: string;
  resolutionNote?: string;
  createdAt: string;
  resolvedAt?: string;
}

export interface IApprovalDecision {
  approved: boolean;
  note?: string;
  confirmation?: string;
}

export interface IReviewResolutionResult {
  item: IReviewQueueItem;
  plan?: ICompletionPlan;
}

export interface IApprovedReviewReconciliationRequest {
  apply: boolean;
  confirmation?: string;
  olderThanMinutes?: number;
  limit?: number;
}

export interface IApprovedReviewReconciliationItem {
  reviewItemId: string;
  taskPlanId: string;
  disposition: 'complete' | 'review' | 'conflict' | string;
  reason: string;
  applied: boolean;
}

export interface IApprovedReviewReconciliationResult {
  dryRun: boolean;
  cutoff: string;
  inspected: number;
  approvedFound: number;
  eligible: number;
  completed: number;
  returnedToReview: number;
  conflicts: number;
  items: IApprovedReviewReconciliationItem[];
}

export interface ITaskEvent {
  at: string;
  stage: string;
  message: string;
}

export interface IMemoryUpdateProposal {
  kind: string;
  content: string;
  tags: string[];
  reason: string;
  confidence: number;
}

export interface ILifeGraphEntity {
  id: string;
  type: string;
  domain: string;
  name: string;
  summary?: string;
  status: string;
  verificationStatus: string;
  localOnly: boolean;
}

export interface ILifeGraphRelation {
  id: string;
  type: string;
  fromEntityId: string;
  toEntityId: string;
  verificationStatus: string;
}

export interface ILifeGraphProjection {
  primary: ILifeGraphEntity;
  linkedEntities: ILifeGraphEntity[];
  relations: ILifeGraphRelation[];
  alreadyExisted: boolean;
  advisoryOnly: boolean;
  canExecute: boolean;
  grantsAuthority: boolean;
}

export interface ITaskCoordinationDraft {
  id: string;
  title: string;
  status: 'draft';
  revision: number;
  digest: string;
  nodes: IPlanTransportNode[];
  edges: IPlanTransportEdge[];
  createdBy: string;
  createdAt: string;
  canExecute: false;
}

export interface ITaskAcceptedCoordinationBinding {
  planId: string;
  revision: number;
  digest: string;
  nodeId: string;
  planTitle: string;
  acceptedAt: string;
  canExecute: false;
}

export interface ICompletionPlan {
  id: string;
	operationId: string;
	idempotencyKey: string;
	reviewItemId?: string;
  coordinationPlan?: ITaskAcceptedCoordinationBinding;
  coordinationDraft?: ITaskCoordinationDraft;
  createdAt: string;
  request: string;
  projectKey?: string;
  pursuitId?: string;
  realGoal: string;
  intake: IIntakeAnalysis;
  frameworkDecision?: IFrameworkSelectionDecision;
	calendarCapacity?: ICalendarCapacityContext;
	resourceDecision?: IResourceDecision;
  contextPlan: IContextPlan;
  minimalityDecision: IMinimalityDecision;
  modelDecision: ILLMRouteDecision;
  toolDecision: IToolRouteDecision;
  steps: ITaskStep[];
  riskAssessment: IRiskAssessment;
  validationPlan: IValidationPlan;
  validationResult: IValidationResult;
  executionPlan: IExecutionPlan;
  executionResult?: IExecutionResult;
  retryPolicy: IRetryPolicy;
  reviewQueueItem?: IReviewQueueItem;
  memoryUpdateProposals: IMemoryUpdateProposal[];
  lessonsLearned: IMemoryUpdateProposal[];
  storedMemoryIds: string[];
  lifeGraphProjection?: ILifeGraphProjection;
  lifeGraphProjectionError?: string;
  events: ITaskEvent[];
  completionStatus: string;
}

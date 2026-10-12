import { ISourceExtraction } from './connected-source.model.interface';
import { IContextMemory } from './context-memory.model.interface';
import { IAutomationLaunchEvent } from './automation.model.interface';
import {
  IVerificationClaim,
  IVerificationEvidence,
  IVerificationRun,
} from './verification.model.interface';
import {
  IWorkflowDecision,
  IWorkflowEvidenceClaim,
  IWorkflowEvent,
  IWorkflowChecklistItem,
  IWorkflowItem,
  IWorkflowOpenLoop,
  IWorkflowProposal,
  IWorkflowQualityGate,
  IWorkflowSourceLink,
  IWorkflowTransition,
} from './workflow.model.interface';

export interface IPursuitSuccessCriterion {
  id: string;
  description: string;
  status: 'pending' | 'satisfied' | 'waived';
  evidenceRequired: boolean;
  evidenceUri?: string;
  verificationStatus?: string;
  waiverReason?: string;
}

export interface IPursuitStopCondition {
  id: string;
  description: string;
  status: 'monitoring' | 'triggered' | 'resolved';
  reason?: string;
  triggeredAt?: string;
  resolvedAt?: string;
}

export interface IPursuitDependency {
  id: string;
  label: string;
  status: 'pending' | 'satisfied' | 'blocked' | 'waived';
  owner?: string;
  relatedPursuitId?: string;
  dueAt?: string;
  evidenceUri?: string;
  reason?: string;
}

export interface IPursuitResourceLimits {
  maxEffortHours?: number;
  maxSpendEur?: number;
  maxParallelWorkflows?: number;
  notes?: string;
}

export type PursuitResourceUsageState = 'not_configured' | 'within_limits' | 'reserved' | 'exhausted' | 'exceeded' | 'unavailable';

export interface IPursuitActiveResourceReservation {
  id: string;
  operationId: string;
  estimatedEffortMinutes: number;
  estimatedCostEur: number;
  reason: string;
  actor: string;
  reservedAt: string;
  stale: boolean;
  reviewReason?: string;
}

export interface IPursuitResourceUsage {
  state: PursuitResourceUsageState;
  available: boolean;
  limitsConfigured: boolean;
  effortRecordedHours: number;
  effortReservedHours: number;
  effortCommittedHours: number;
  effortLimitHours: number;
  effortRemainingHours: number;
  effortExhausted: boolean;
  effortExceeded: boolean;
  spendIncurredEur: number;
  spendRefundedEur: number;
  spendNetEur: number;
  spendReservedEur: number;
  spendCommittedEur: number;
  spendLimitEur: number;
  spendRemainingEur: number;
  spendExhausted: boolean;
  spendExceeded: boolean;
  eventCount: number;
  activeReservations: number;
  latestRecordedAt?: string;
  latestReservedAt?: string;
  blockingReason?: string;
  reservations: IPursuitActiveResourceReservation[];
}

export interface IPursuitResourceEvent {
  id: string;
  pursuitId: string;
  kind: 'effort_recorded' | 'spend_incurred' | 'spend_refund';
  effortMinutes: number;
  amountMinor: number;
  currency?: string;
  note?: string;
  evidenceUri?: string;
  actor: string;
  idempotencyKey: string;
  recordDigest: string;
  occurredAt: string;
  recordedAt: string;
}

export interface IPursuitResourceEventRequest {
  kind: IPursuitResourceEvent['kind'];
  effortHours?: number;
  spendEur?: number;
  note?: string;
  evidenceUri?: string;
  idempotencyKey: string;
  occurredAt?: string;
}

export interface IPursuit {
  id: string;
  ownerIdentity?: string;
  title: string;
  description?: string;
  whyItMatters?: string;
  projectKey?: string;
  mandateId?: string;
  domain?: string;
  desiredOutcome?: string;
  currentStateSummary?: string;
  status: string;
  priorityScore: number;
  riskLevel: string;
  confidence: number;
  autonomyLevel: string;
  needCategory?: string;
  sourceOfCreation?: string;
  nextRecommendedAction?: string;
  completionDefinition?: string;
  successCriteria: IPursuitSuccessCriterion[];
  stopConditions: IPursuitStopCondition[];
  dependencies: IPursuitDependency[];
  resourceLimits: IPursuitResourceLimits;
  completionState: string;
  lastActivityAt?: string;
  nextReviewAt?: string;
  targetAt?: string;
  reviewCadenceDays: number;
  archived: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface IPursuitLink {
  id: string;
  pursuitId: string;
  linkType: string;
  linkId: string;
  relationship: string;
  sourceUri?: string;
  sourceLabel?: string;
  confidence: number;
  createdAt: string;
}

export interface IPursuitActivity {
  id: string;
  pursuitId: string;
  eventType: string;
  message?: string;
  actor?: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  createdAt: string;
}

export interface IPursuitReviewRequest {
  action?: 'complete' | 'reviewed' | 'done' | 'snooze' | 'postpone';
  actor?: string;
  note?: string;
  nextReviewAt?: string;
  snoozeDays?: number;
}

export interface IPursuitPlanRequest {
  input?: string;
  actor?: string;
  requiresReview?: boolean;
  reviewReason?: string;
}

export interface IPursuitListItem {
  pursuit: IPursuit;
  needsRobert: number;
  blocked: number;
  openLoops: number;
  decisionCards: number;
  linkedEvidence: number;
  timelineItems: number;
  completionCandidate: boolean;
  currentState?: string;
  whatChanged?: string;
  nextAction?: string;
  effectiveLastActivityAt?: string;
  stale: boolean;
  reviewDue: boolean;
  planningNeeded: boolean;
}

export interface IPursuitDashboard {
  counts: Record<string, number>;
  decisionQueue: IPursuitDashboardDecision[];
  needsRobert: IPursuitListItem[];
  vaReady: IPursuitListItem[];
  systemReady: IPursuitListItem[];
  blocked: IPursuitListItem[];
  stale: IPursuitListItem[];
  reviewDue: IPursuitListItem[];
  planningNeeded: IPursuitListItem[];
  recentlyChanged: IPursuitListItem[];
  highRisk: IPursuitListItem[];
  completionCandidates: IPursuitListItem[];
}

export interface IPursuitDashboardDecision {
  pursuit: IPursuit;
  decision: IPursuitDecision;
  currentState?: string;
  nextAction?: string;
  blocked: number;
  evidenceLine?: string;
}

export interface IPursuitBriefCard {
  queue: string;
  pursuitId: string;
  title: string;
  action: string;
  context: string;
  riskLevel: string;
  evidenceLine: string;
  needsRobert: boolean;
}

export interface IPursuitBrief {
  generatedAt: string;
  operatingMode: string;
  summary: string;
  primaryAction: string;
  needsRobert: number;
  readyToMove: number;
  stuck: number;
  reviewDue: number;
  planningNeeded: number;
  completionCandidates: number;
  recentlyChanged: number;
  cards: IPursuitBriefCard[];
}

export interface IPursuitOverview {
  dashboard: IPursuitDashboard;
  brief: IPursuitBrief;
}

export interface IPursuitAction {
  label: string;
  owner: string;
  riskLevel: string;
  requiresApproval: boolean;
  reason: string;
  workflowId?: string;
  yesLabel?: string;
  noLabel?: string;
}

export interface IPursuitBlocker {
  label: string;
  reason: string;
  owner: string;
  workflowId?: string;
  followUpAt?: string;
}

export interface IPursuitSummary {
  currentState: string;
  whatChanged: string;
  needsRobert: number;
  blocked: number;
  openLoops: number;
  robertActions: number;
  vaReadyActions: number;
  systemReadyActions: number;
  waitingActions: number;
  decisionCards: number;
  timelineItems: number;
  taskRuns: number;
  linkedEvidence: number;
  verificationRuns: number;
  runtimeAttempts: number;
  qualityGatesNeedingReview: number;
  confidence: number;
  planningNeeded: boolean;
  reviewDue: boolean;
  completionCandidate: boolean;
  goalContractReady: boolean;
  criteriaSatisfied: number;
  criteriaTotal: number;
  triggeredStopConditions: number;
  openDependencies: number;
  targetOverdue: boolean;
}

export interface IPursuitOperationalDigest {
  primaryLane: string;
  headline: string;
  recommendedAction: string;
  robertLine: string;
  delegationLine: string;
  systemLine: string;
  waitingLine: string;
  blockerLine: string;
  evidenceLine: string;
  runtimeLine: string;
  sourceLine: string;
  verificationLine: string;
  riskLine: string;
  needsRobert: number;
  vaReady: number;
  systemReady: number;
  waiting: number;
  blocked: number;
  evidence: number;
  runtimeAttempts: number;
  verificationRuns: number;
  openLoops: number;
}

export interface IPursuitActionQueues {
  needsRobert: IPursuitAction[];
  vaReady: IPursuitAction[];
  systemReady: IPursuitAction[];
  waiting: IPursuitAction[];
}

export interface IPursuitSourceItem {
  id: string;
  sourceId: string;
  externalId: string;
  projectKey?: string;
  itemType?: string;
  title?: string;
  sourceUri?: string;
  metadata?: string;
  fetchedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface IPursuitConversation {
  id: string;
  platform: string;
  externalId: string;
  title?: string;
  sourceUri?: string;
  revision: number;
  messageCount: number;
  capturedAt: string;
  lastMessageAt?: string;
  archived: boolean;
}

export interface IPursuitAmbientOpportunity {
  id: string;
  needKey: string;
  title: string;
  rationale?: string;
  nextAction?: string;
  sourceType?: string;
  sourceUri?: string;
  priorityScore: number;
  confidence: number;
  risk: number;
  requiresApproval: boolean;
  status: string;
  lastSeenAt: string;
  resolutionNote?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IPursuitAutomation {
  id: string;
  name: string;
  runtimeType?: string;
  launchType?: string;
  status?: string;
  lastLaunchAt?: string;
  lastFailureReason?: string;
}

export interface IPursuitTaskRun {
  workflowId: string;
  workflowTitle: string;
  taskPlanId?: string;
  status: string;
  verificationStatus?: string;
  retryCount: number;
  maxRetries: number;
  lastRunAt?: string;
  nextRunAt?: string;
  lastWorkerError?: string;
  automationId?: string;
  needsReview: boolean;
}

export interface IPursuitTaskAttempt {
  id: string;
  pursuitId: string;
  taskPlanId: string;
  ownerIdentity?: string;
  requestSummary?: string;
  projectKey?: string;
  mode: string;
  status: string;
  riskLevel?: string;
  verificationStatus?: string;
  automationId?: string;
  launchEventId?: string;
  blockedReason?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IPursuitDecision {
  id: string;
  workflowId?: string;
  workflowTitle?: string;
  decisionType: string;
  status: string;
  recommended: string;
  reason: string;
  riskLevel: string;
  evidenceUri?: string;
  evidenceLabel?: string;
  yesLabel: string;
  noLabel: string;
  yesConsequence: string;
  noConsequence: string;
  requiresApproval: boolean;
  approved: boolean;
  actor?: string;
  createdAt?: string;
}

export interface IPursuitTimelineItem {
  id: string;
  kind: string;
  title: string;
  message?: string;
  workflowId?: string;
  workflowTitle?: string;
  actor?: string;
  status?: string;
  riskLevel?: string;
  sourceUri?: string;
  sourceLabel?: string;
  needsReview: boolean;
  createdAt: string;
}

export interface IPursuitDetail {
  pursuit: IPursuit;
  links: IPursuitLink[];
  activity: IPursuitActivity[];
  workflows: IWorkflowItem[];
  checklistItems: IWorkflowChecklistItem[];
  openLoops: IWorkflowOpenLoop[];
  proposals: IWorkflowProposal[];
  qualityGates: IWorkflowQualityGate[];
  decisions: IWorkflowDecision[];
  decisionQueue: IPursuitDecision[];
  transitions: IWorkflowTransition[];
  sourceLinks: IWorkflowSourceLink[];
  events: IWorkflowEvent[];
  timeline: IPursuitTimelineItem[];
  evidence: IWorkflowEvidenceClaim[];
  memories: IContextMemory[];
  conversations: IPursuitConversation[];
  ambientOpportunities: IPursuitAmbientOpportunity[];
  taskRuns: IPursuitTaskRun[];
  taskAttempts: IPursuitTaskAttempt[];
  verificationRuns: IVerificationRun[];
  verificationClaims: IVerificationClaim[];
  verificationEvidence: IVerificationEvidence[];
  automations: IPursuitAutomation[];
  runtimeAttempts: IAutomationLaunchEvent[];
  sourceItems: IPursuitSourceItem[];
  sourceExtractions: ISourceExtraction[];
  nextActions: IPursuitAction[];
  actionQueues: IPursuitActionQueues;
  blockers: IPursuitBlocker[];
  approvalItems: IWorkflowItem[];
  summary: IPursuitSummary;
  operationalDigest: IPursuitOperationalDigest;
  resourceUsage: IPursuitResourceUsage;
}

export interface IPursuitDelegationChecklistItem {
  label: string;
  status: string;
  required: boolean;
}

export interface IPursuitDelegationWorkItem {
  workflowId?: string;
  title: string;
  instructions: string;
  state?: string;
  dueAt?: string;
  checklist: IPursuitDelegationChecklistItem[];
}

export interface IPursuitDelegationSource {
  workflowId?: string;
  sourceType?: string;
  sourceUri: string;
  sourceLabel?: string;
  relationship?: string;
}

export interface IPursuitDelegationPackage {
  generatedAt: string;
  ready: boolean;
  status: string;
  reason: string;
  pursuitId: string;
  title: string;
  objective: string;
  whyItMatters?: string;
  currentState: string;
  completionDefinition?: string;
  riskLevel: string;
  workItems: IPursuitDelegationWorkItem[];
  sourceContext: IPursuitDelegationSource[];
  allowedActions: string[];
  blockedActions: string[];
  escalationRules: string[];
  deliveryRequirements: string[];
  outstandingRobertActions: IPursuitAction[];
}

export interface IPursuitEvidenceResolution {
  uri: string;
  kind: string;
  title: string;
  summary?: string;
  status?: string;
  sourceType?: string;
  sourceId?: string;
  sourceLabel?: string;
  workflowId?: string;
  needsReview: boolean;
  runtimeAttempt?: IAutomationLaunchEvent;
  pursuitLink?: IPursuitLink;
  timelineItem?: IPursuitTimelineItem;
  workflowEvidence?: IWorkflowEvidenceClaim;
  memory?: IContextMemory;
  sourceItem?: IPursuitSourceItem;
  sourceExtraction?: ISourceExtraction;
  verificationEvidence?: IVerificationEvidence;
  activity?: IPursuitActivity;
}

export interface IPursuitApprovalOverview {
  pursuit: IPursuit;
  decisionQueue: IPursuitDecision[];
  approvalItems: IWorkflowItem[];
  actions: IPursuitAction[];
  blockers: IPursuitBlocker[];
  summary: IPursuitSummary;
  counts: Record<string, number>;
}

export interface IPursuitCreateRequest {
  title: string;
  ownerIdentity?: string;
  description?: string;
  whyItMatters?: string;
  projectKey?: string;
  mandateId?: string;
  domain?: string;
  desiredOutcome?: string;
  currentStateSummary?: string;
  status?: string;
  priorityScore?: number;
  riskLevel?: string;
  confidence?: number;
  autonomyLevel?: string;
  needCategory?: string;
  sourceOfCreation?: string;
  nextRecommendedAction?: string;
  completionDefinition?: string;
  nextReviewAt?: string;
  successCriteria?: IPursuitSuccessCriterion[];
  stopConditions?: IPursuitStopCondition[];
  dependencies?: IPursuitDependency[];
  resourceLimits?: IPursuitResourceLimits;
  targetAt?: string;
  reviewCadenceDays?: number;
}

export interface IPursuitUpdateRequest extends Partial<IPursuitCreateRequest> {
  completionState?: string;
  archived?: boolean;
  actor?: string;
}

export interface IPursuitIntakeRequest {
  input: string;
  projectKey?: string;
  automationId?: string;
  mandateId?: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  contentType?: string;
  sender?: string;
  receivedAt?: string;
  trigger?: string;
  actor?: string;
  requiresReview?: boolean;
  reviewReason?: string;
}

export interface IPursuitAutoLinkResult {
  linked: boolean;
  created?: boolean;
  pursuitId?: string;
  score: number;
  reasons?: string[];
  message?: string;
  links?: IPursuitLink[];
}

export interface IPursuitRoutedIntakeResult {
  mode: string;
  matched: boolean;
  createdCandidate: boolean;
  pursuitId?: string;
  score?: number;
  reasons?: string[];
  message?: string;
  matches?: IPursuitMatchCandidate[];
  detail?: IPursuitDetail;
  autoLink?: IPursuitAutoLinkResult;
}

export interface IPursuitDecisionResolutionRequest {
  decisionId: string;
  decisionType?: string;
  approved: boolean;
  reason?: string;
  note?: string;
  evidenceUri?: string;
  evidenceLabel?: string;
  actor?: string;
}

export interface IPursuitLinkRequest {
  linkType: string;
  linkId: string;
  relationship?: string;
  sourceUri?: string;
  sourceLabel?: string;
  confidence?: number;
  actor?: string;
}

export interface IPursuitMatchRequest {
  input?: string;
  projectKey?: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  limit?: number;
}

export interface IPursuitMatchCandidate {
  pursuit: IPursuit;
  score: number;
  reasons: string[];
  confidence: string;
}

export interface IPursuitPortfolioPriorityFactors {
  importance: number;
  urgency: number;
  humanNeedAffected: number;
  deadlinePressure: number;
  costOfDelay: number;
  expectedValue: number;
  harmAvoided: number;
  probabilityOfSuccess: number;
  effort: number;
  duration: number;
  dependencies: number;
  reversibility: number;
  risk: number;
  legalObligation: number;
  relationshipConsequences: number;
  availableCapacity: number;
  energyFit: number;
  opportunityCost: number;
  strategicAlignment: number;
  learningValue: number;
  compoundingValue: number;
  staleness: number;
  commitmentAge: number;
  peopleBlocked: number;
  delegability: number;
}

export interface IPursuitPortfolioCapacityWindow {
  start: string;
  end: string;
}

export interface IPursuitPortfolioDurationEstimate {
  optimisticMinutes: number;
  expectedMinutes: number;
  pessimisticMinutes: number;
  basis?: string;
}

export interface IPursuitPortfolioUsage {
  costMicros: number;
  inputTokens: number;
  outputTokens: number;
  toolCalls: number;
}

export interface IPursuitPortfolioEstimateCalibrationBinding {
  scopeKey: string;
  proposalId: string;
  proposalVersion: string;
  applicationId: string;
  evidenceDigest: string;
  sourceDuration: IPursuitPortfolioDurationEstimate;
  sourceEstimatedUsage: IPursuitPortfolioUsage;
}

export interface IPursuitPortfolioPlanningInput {
  pursuitId: string;
  duration: IPursuitPortfolioDurationEstimate;
  estimatedUsage: IPursuitPortfolioUsage;
  factors: IPursuitPortfolioPriorityFactors;
  calibration?: IPursuitPortfolioEstimateCalibrationBinding;
  optional?: boolean;
}

export interface IPursuitPortfolioBudget {
  maxCostMicros?: number;
  maxInputTokens?: number;
  maxOutputTokens?: number;
  maxToolCalls?: number;
}

export interface IPursuitPortfolioApprovalPolicy {
  costThresholdMicros?: number;
  inputTokenThreshold?: number;
  outputTokenThreshold?: number;
  toolCallThreshold?: number;
  uncertaintyThresholdPct?: number;
  softDeadlineMiss: boolean;
}

export interface IPursuitPortfolioPlanningRequest {
  planId: string;
  asOf: string;
  horizonStart: string;
  horizonEnd: string;
  durationMode: 'expected' | 'conservative';
  availability: IPursuitPortfolioCapacityWindow[];
  pursuits: IPursuitPortfolioPlanningInput[];
  budget: IPursuitPortfolioBudget;
  approvalPolicy: IPursuitPortfolioApprovalPolicy;
}

export interface IPursuitPortfolioFactorContribution {
  factor: string;
  input: number;
  effectiveInput: number;
  weight: number;
  contribution: number;
  costFactor: boolean;
  reason: string;
}

export interface IPursuitPortfolioPriority {
  pursuitId: string;
  title: string;
  score: number;
  band: string;
  factors: IPursuitPortfolioPriorityFactors;
  contributions: IPursuitPortfolioFactorContribution[];
  reasons: string[];
  algorithmVersion: string;
}

export interface IPursuitPortfolioExclusion {
  pursuitId: string;
  title: string;
  code: string;
  reason: string;
}

export interface IPursuitPortfolioScheduledTask {
  taskId: string;
  start: string;
  end: string;
  plannedDurationMinutes: number;
  deadlineSlackMinutes?: number;
  dependencySlackMinutes: number;
  networkSlackMinutes: number;
  critical: boolean;
  dependencies?: string[];
  durationEstimateBasis: string;
  durationUncertaintyPct: number;
}

export interface IPursuitPortfolioBlocker {
  code: string;
  taskId?: string;
  resourceId?: string;
  detail: string;
  blocksFeasibility: boolean;
}

export interface IPursuitPortfolioApprovalFlag {
  code: string;
  taskId?: string;
  reason: string;
  mandatory: boolean;
}

export interface IPursuitPortfolioDecision {
  planId: string;
  algorithmVersion: string;
  asOf: string;
  inputDigest: string;
  decisionDigest: string;
  feasibility: 'feasible' | 'feasible_with_approvals' | 'infeasible';
  scheduled: IPursuitPortfolioScheduledTask[];
  unscheduledTaskIds?: string[];
  criticalBlockers?: IPursuitPortfolioBlocker[];
  advisories?: IPursuitPortfolioBlocker[];
  approvalFlags?: IPursuitPortfolioApprovalFlag[];
  authority: string;
  canExecute: boolean;
  grantsAuthority: boolean;
}

export interface IPursuitPortfolioCapacityAssessment {
  status: 'not_enforced' | 'applied' | 'missing' | 'stale' | 'needs_review' | 'unavailable' | 'owner_mismatch';
  snapshotId?: string;
  snapshotStatus?: string;
  capturedAt?: string;
  freshUntil?: string;
  confidence?: number;
  timeAvailableMinutes?: number;
  appliedMinutes?: number;
  concurrentWorkLimit?: number;
  currentLoad?: number;
  planningStepLimit?: number;
  constraints?: string[];
  sourceLabel?: string;
  reason: string;
}

export interface IPursuitPortfolioEstimateCalibrationRecommendation {
  pursuitId: string;
  scopeKey: string;
  status: 'available' | 'bound' | 'unavailable';
  reason: string;
  proposalId?: string;
  proposalVersion?: string;
  applicationId?: string;
  evidenceDigest?: string;
  sampleCount?: number;
  effortMultiplier?: number;
  costMultiplier?: number;
  effortDispersion?: number;
  costDispersion?: number;
  confidence?: number;
  observedFrom?: string;
  observedThrough?: string;
  sourceOptimisticMinutes?: number;
  sourceExpectedMinutes?: number;
  sourcePessimisticMinutes?: number;
  sourceCostMicros?: number;
  suggestedOptimisticMinutes?: number;
  suggestedExpectedMinutes?: number;
  suggestedPessimisticMinutes?: number;
  suggestedCostMicros?: number;
  appliedAt?: string;
  applied: boolean;
}

export interface IPursuitPortfolioPlanningResult {
  planId: string;
  asOf: string;
  status: string;
  pursuitsConsidered: number;
  pursuitsPlanned: number;
  priorities: IPursuitPortfolioPriority[];
  exclusions: IPursuitPortfolioExclusion[];
  decision?: IPursuitPortfolioDecision;
  capacity?: IPursuitPortfolioCapacityAssessment;
  calibrations?: IPursuitPortfolioEstimateCalibrationRecommendation[];
  authority: 'advisory_only';
  canExecute: false;
}

export interface IPursuitPortfolioAllocationAcceptanceRequest {
  planningRequest: IPursuitPortfolioPlanningRequest;
  expectedDecisionDigest: string;
  confirmation: 'ACCEPT PORTFOLIO ALLOCATION';
}

export interface IPursuitPortfolioAllocation {
  id: string;
  planId: string;
  requestDigest: string;
  decisionDigest: string;
  status: string;
  durationMode: IPursuitPortfolioPlanningRequest['durationMode'];
  horizonStart: string;
  horizonEnd: string;
  actor: string;
  confirmation: 'ACCEPT PORTFOLIO ALLOCATION';
  recordDigest: string;
  acceptedAt: string;
}

export interface IPursuitPortfolioAllocationItem {
  id: string;
  allocationId: string;
  pursuitId: string;
  scheduledStart: string;
  scheduledEnd: string;
  durationMinutes: number;
  estimatedCostMicros: number;
  requiresApproval: boolean;
  approvalReasons: string[];
  reservationId: string;
  recordDigest: string;
  createdAt: string;
}

export interface IPursuitPortfolioAllocationAcceptanceResult {
  allocation: IPursuitPortfolioAllocation;
  items: IPursuitPortfolioAllocationItem[];
  replayed: boolean;
  authority: 'allocation_only';
  canExecute: false;
}

export interface IPursuitPortfolioExecutionProposalRequest {
  expectedAllocationDigest: string;
  confirmation: 'PREPARE EXECUTION PROPOSALS';
}

export interface IPursuitPortfolioExecutionProposal {
  id: string;
  allocationId: string;
  allocationRecordDigest: string;
  snapshotDigest: string;
  status: 'prepared' | 'prepared_needs_approval' | 'prepared_blocked';
  actor: string;
  confirmation: 'PREPARE EXECUTION PROPOSALS';
  authority: 'proposal_only';
  recordDigest: string;
  preparedAt: string;
}

export interface IPursuitPortfolioExecutionProposalItem {
  id: string;
  proposalId: string;
  allocationItemId: string;
  pursuitId: string;
  reservationId: string;
  actionSummary: string;
  pursuitStatus: string;
  riskLevel: string;
  autonomyLevel: string;
  status: 'proposed' | 'needs_approval' | 'blocked';
  requiresApproval: boolean;
  approvalReasons: string[];
  blockedReasons: string[];
  allocationItemDigest: string;
  stateDigest: string;
  recordDigest: string;
  preparedAt: string;
}

export interface IPursuitPortfolioExecutionProposalResult {
  proposal: IPursuitPortfolioExecutionProposal;
  items: IPursuitPortfolioExecutionProposalItem[];
  replayed: boolean;
  authority: 'proposal_only';
  canExecute: false;
  freshness: {
    status: 'prepared_snapshot' | 'recovered_snapshot';
    revalidationRequired: true;
    checkedAt: string;
    reason: string;
  };
}

export type PursuitPortfolioDispatchOutcome =
  | 'workflow_created'
  | 'replayed'
  | 'needs_approval'
  | 'blocked'
  | 'stale'
  | 'failed'
  | 'cancelled';

export interface IPursuitPortfolioDispatchRun {
  id: string;
  proposalId: string;
  proposalDigest: string;
  selectedItemIds: string[];
  selectedItemsDigest: string;
  requestDigest: string;
  actor: string;
  confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
  recordDigest: string;
  requestedAt: string;
}

export interface IPursuitPortfolioDispatchItemResult {
  id: string;
  dispatchRunId: string;
  proposalId: string;
  proposalItemId: string;
  attemptNumber: number;
  proposalItemDigest: string;
  approvalDecisionId?: string;
  approvalDecisionDigest?: string;
  outcome: PursuitPortfolioDispatchOutcome;
  message: string;
  authorizationReceiptId?: string;
  workflowId?: string;
  workflowState?: string;
  replayed: boolean;
  recordDigest: string;
  attemptedAt: string;
}

export interface IPursuitPortfolioCoordinationItem {
  item: IPursuitPortfolioExecutionProposalItem;
  eligibility: 'eligible' | 'dispatched' | PursuitPortfolioDispatchOutcome;
  reason: string;
  decision?: IPursuitPortfolioExecutionProposalDecisionRecord;
  latestDispatch?: IPursuitPortfolioDispatchItemResult;
  selectable: boolean;
}

export interface IPursuitPortfolioCoordinationResult {
  proposal: IPursuitPortfolioExecutionProposal;
  items: IPursuitPortfolioCoordinationItem[];
  dispatchRuns: IPursuitPortfolioDispatchRun[];
  eligible: number;
  needsApproval: number;
  blocked: number;
  stale: number;
  dispatched: number;
  authority: 'coordination_preview_only';
  canExecute: false;
  freshness: {
    status: 'current_coordination_snapshot';
    revalidationRequired: true;
    checkedAt: string;
    reason: string;
  };
}

export interface IPursuitPortfolioDispatchItemRequest {
  proposalItemId: string;
  expectedItemDigest: string;
  expectedDecisionDigest: string;
}

export interface IPursuitPortfolioDispatchRequest {
  expectedProposalDigest: string;
  items: IPursuitPortfolioDispatchItemRequest[];
  confirmation: 'DISPATCH APPROVED PORTFOLIO WORKFLOWS';
}

export interface IPursuitPortfolioDispatchResult {
  run: IPursuitPortfolioDispatchRun;
  items: IPursuitPortfolioDispatchItemResult[];
  status: 'workflows_created' | 'needs_review' | 'partial_failure';
  created: number;
  replayed: number;
  needsReview: number;
  failed: number;
  resumed: boolean;
  authority: 'portfolio_dispatch_result';
  canExecute: false;
}

export type PursuitPortfolioExecutionProposalDecision =
  | 'approved'
  | 'rejected'
  | 'needs_clarification'
  | 'revoked';

export type PursuitPortfolioExecutionProposalDecisionConfirmation =
  | 'APPROVE EXECUTION PROPOSAL ITEM'
  | 'REJECT EXECUTION PROPOSAL ITEM'
  | 'REQUEST CLARIFICATION FOR EXECUTION PROPOSAL ITEM'
  | 'REVOKE EXECUTION PROPOSAL ITEM';

export interface IPursuitPortfolioExecutionProposalDecisionRequest {
  expectedItemDigest: string;
  decision: PursuitPortfolioExecutionProposalDecision;
  reason: string;
  confirmation: PursuitPortfolioExecutionProposalDecisionConfirmation;
}

export interface IPursuitPortfolioExecutionProposalDecisionRecord {
  id: string;
  proposalItemId: string;
  proposalId: string;
  pursuitId: string;
  decision: PursuitPortfolioExecutionProposalDecision;
  reason: string;
  actor: string;
  confirmation: PursuitPortfolioExecutionProposalDecisionConfirmation;
  proposalItemDigest: string;
  stateDigest: string;
  authority: 'approval_decision_only';
  requestDigest: string;
  recordDigest: string;
  previousDecisionId?: string;
  decidedAt: string;
  expiresAt?: string;
}

export interface IPursuitPortfolioExecutionProposalDecisionResult {
  decision: IPursuitPortfolioExecutionProposalDecisionRecord;
  replayed: boolean;
  authority: 'approval_decision_only';
  canExecute: false;
}

export interface IPursuitPortfolioExecutionProposalDecisionHistoryResult {
  decisions: IPursuitPortfolioExecutionProposalDecisionRecord[];
  authority: 'approval_decision_only';
  canExecute: false;
}

export interface IPursuitPortfolioWorkflowEffectAuthorizationRequest {
  expectedItemDigest: string;
  expectedDecisionDigest: string;
  confirmation: 'AUTHORIZE PORTFOLIO WORKFLOW EFFECT';
}

export interface IPursuitPortfolioWorkflowEffect {
  action: 'pursuit.portfolio.create-workflow';
  stage: 'execution';
  resourceType: 'workflow-intake';
  resourceId: string;
  projectKey?: string;
  domain?: string;
  toolId: 'workflow.intake';
  runtimeId: 'hai-workflow-engine';
  risk: 'low' | 'medium' | 'high' | 'critical';
  reversible: true;
  estimatedCostMicros: 0;
  actionSummary: string;
  effectDigest: string;
  approvalSourceId: string;
}

export interface IPursuitPortfolioWorkflowAuthorizationReceipt {
  id: string;
  contractVersion: number;
  ownerIdentity: string;
  actorIdentity: string;
  actorKind: 'human';
  taskId: string;
  action: string;
  stage: string;
  resourceType: string;
  resourceId: string;
  projectKey?: string;
  domain?: string;
  runtimeId: string;
  approvalSourceId: string;
  effectDigest: string;
  outcome: 'authorized' | 'requires_approval' | 'denied';
  reason: string;
  requestDigest: string;
  decisionDigest: string;
  requiredAuthority: number;
  requestedAutonomy: number;
  effectiveAutonomy: number;
  risk: 'low' | 'medium' | 'high' | 'critical';
  reversible: boolean;
  estimatedCostEur: number;
  notificationRequired: boolean;
  evaluatedAt: string;
}

export interface IPursuitPortfolioWorkflowEffectAuthorizationResult {
  effect: IPursuitPortfolioWorkflowEffect;
  receipt: IPursuitPortfolioWorkflowAuthorizationReceipt;
  authority: 'execution_authorization_only';
  canExecute: false;
}

export interface IPursuitPortfolioWorkflowEffectExecutionRequest {
  authorizationReceiptId: string;
  expectedItemDigest: string;
  expectedDecisionDigest: string;
  confirmation: 'CREATE APPROVED PORTFOLIO WORKFLOW';
}

export interface IPursuitPortfolioWorkflowEffectConsumption {
  receiptId: string;
  ownerIdentity: string;
  consumer: 'pursuit-portfolio-workflow';
  executionTarget: string;
  receiptDigest: string;
  consumedAt: string;
}

export interface IPursuitPortfolioWorkflowEffectExecutionResult {
  effect: IPursuitPortfolioWorkflowEffect;
  receipt: IPursuitPortfolioWorkflowAuthorizationReceipt;
  consumption: IPursuitPortfolioWorkflowEffectConsumption;
  pursuitId: string;
  workflowId: string;
  workflowState: string;
  replayed: boolean;
  authority: 'workflow_effect_executed';
  canExecute: false;
}

export interface IPursuitPortfolioWorkflowSettlementRequest {
  workflowId: string;
  expectedItemDigest: string;
  actualEffortMinutes: number;
  actualCostMicros: number;
  confirmation: 'SETTLE VERIFIED PORTFOLIO WORK';
}

export interface IPursuitPortfolioWorkflowSettlementResult {
  pursuitId: string;
  proposalItemId: string;
  reservationId: string;
  workflowId: string;
  disposition: 'consumed';
  actualEffortMinutes: number;
  actualCostMicros: number;
  verificationStatus: string;
  evidenceUri: string;
  completionAttestationId: string;
  completionAttestationDigest: string;
  settlementProofId: string;
  settlementProofDigest: string;
  learningOutcomeId?: string;
  learningStatus: 'evidence_recorded' | 'unavailable' | 'recording_failed';
  learningProposalId?: string;
  learningProposalStatus: 'proposal_unavailable' | 'proposal_failed' | 'insufficient_evidence' | 'monitoring' | 'stable' | 'review_required' | 'changes_requested' | 'governance_required' | 'approved' | 'rejected';
  learningSampleCount: number;
  learningNewEvidenceCount: number;
  learningDriftDetected: boolean;
  learningReviewRequired: boolean;
  replayed: boolean;
  authority: 'verified_accounting_only';
  canExecute: false;
  resourceUsage: IPursuitResourceUsage;
}

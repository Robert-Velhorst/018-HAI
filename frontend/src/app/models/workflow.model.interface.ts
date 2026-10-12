export interface IWorkflowItem {
  id: string;
  title: string;
  description?: string;
  successCriteria?: string[];
  projectKey?: string;
  automationId?: string;
  mandateId?: string;
  coordinationPlanId?: string;
  coordinationPlanRevision?: number;
  coordinationPlanDigest?: string;
  coordinationPlanNodeId?: string;
  coordinationDraftPlanId?: string;
  coordinationDraftRevision?: number;
  coordinationDraftDigest?: string;
  coordinationDraftNodeId?: string;
  currentState: string;
  taskType: string;
  riskLevel: string;
  priorityScore: number;
  confidence: number;
  autonomyLevel: string;
  requiresApproval: boolean;
  approvalStatus: string;
  approvalReason?: string;
  blockedReason?: string;
  nextAction?: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  sourceRevision?: string;
  dueAt?: string;
  retryCount: number;
  maxRetries: number;
  nextRunAt?: string;
  lastRunAt?: string;
  workerLeaseUntil?: string;
  completedAt?: string;
  verificationStatus?: string;
  recoveryStatus?: string;
  recoveryNote?: string;
  lastTaskPlanId?: string;
  lastWorkerError?: string;
  archived: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowChecklistItem {
  id: string;
  workflowId: string;
  label: string;
  status: string;
  position: number;
  requiresApproval: boolean;
  dueAt?: string;
  reminderAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowIntakeRecord {
  id: string;
  workflowId: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  contentType?: string;
  sender?: string;
  receivedAt?: string;
  rawContent?: string;
  normalizedSummary?: string;
  detectedEntities?: string;
  possibleProject?: string;
  urgency?: string;
  createdAt: string;
}

export interface IWorkflowProjectMatch {
  id: string;
  workflowId: string;
  projectKey: string;
  matchedBy?: string;
  confidence: number;
  trelloCardRef?: string;
  driveFolderRef?: string;
  createdAt: string;
}

export interface IWorkflowPursuitContext {
  id: string;
  title: string;
  status: string;
  riskLevel: string;
  priorityScore: number;
  confidence: number;
  autonomyLevel: string;
  needCategory?: string;
  whyItMatters?: string;
  desiredOutcome?: string;
  currentStateSummary?: string;
  nextRecommendedAction?: string;
  completionDefinition?: string;
  completionState?: string;
  linkId: string;
  relationship: string;
  sourceUri?: string;
  sourceLabel?: string;
  linkConfidence: number;
}

export interface IWorkflowEvidenceClaim {
  id: string;
  workflowId: string;
  claimText: string;
  sourceUri?: string;
  sourceLabel?: string;
  reliability: string;
  status: string;
  needsReview: boolean;
  createdAt: string;
}

export interface IWorkflowOpenLoop {
  id: string;
  workflowId: string;
  responsibleParty: string;
  waitingFor: string;
  nextAction: string;
  followUpAt?: string;
  status: string;
  leaseUntil?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowProposal {
  id: string;
  workflowId: string;
  recommendedAction: string;
  options?: string;
  selectedOption?: string;
  resolutionNote?: string;
  resolvedBy?: string;
  resolvedAt?: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowQualityGate {
  id: string;
  workflowId: string;
  gate: string;
  status: string;
  reason?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowRule {
  id: string;
  ruleKey: string;
  name: string;
  description: string;
  category: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface IWorkflowTransition {
  id: string;
  workflowId: string;
  fromState?: string;
  toState: string;
  trigger?: string;
  actor?: string;
  approved: boolean;
  reason?: string;
  createdAt: string;
}

export interface IWorkflowSourceLink {
  id: string;
  workflowId: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  relationship: string;
  createdAt: string;
}

export interface IWorkflowDecision {
  id: string;
  workflowId: string;
  decisionType: string;
  decision: string;
  reason?: string;
  ruleApplied?: string;
  approved: boolean;
  actor?: string;
  createdAt: string;
}

export interface IWorkflowEvent {
  id: string;
  workflowId: string;
  eventType: string;
  fromState?: string;
  toState?: string;
  message?: string;
  trigger?: string;
  ruleApplied?: string;
  sourceUri?: string;
  actor?: string;
  createdAt: string;
}

export interface IWorkflowFrameworkSelectionProvenance {
  selectionDecisionId: string;
  taskPlanId: string;
  catalogVersion: string;
  catalogDigest: string;
  selectorAlgorithmVersion: string;
  taskRiskLevel?: 'low' | 'medium' | 'high';
  effectiveRiskCeiling?: 'low' | 'medium' | 'high';
  maximumAutonomyLevel?: number;
  requiresApproval?: boolean;
  effectivePreferenceDigest: string;
  constitutionVersion: number;
  constitutionDigest: string;
  constitutionSource: string;
}

export interface IWorkflowSelectedFramework {
  id: string;
  version: string;
  name: string;
  family: string;
  riskCeiling?: 'low' | 'medium' | 'high';
  score: number;
  reasons: string[];
  maximumAutonomyLevel: number;
  authorityRequirement: string;
  evidenceRequirements: string[];
  evaluationMethod: string[];
}

export interface IWorkflowFrameworkConflict {
  selectedId: string;
  skippedId: string;
  reason: string;
}

export interface IWorkflowFrameworkSelectionDecision {
  id: string;
  taskPlanId?: string;
  createdAt: string;
  catalogVersion: string;
  catalogDigest: string;
  selectorAlgorithmVersion: string;
  taskRiskLevel?: 'low' | 'medium' | 'high';
  effectiveRiskCeiling?: 'low' | 'medium' | 'high';
  effectivePreferenceDigest: string;
  constitutionDigest: string;
  lifeDomain: string;
  needOrCommitment: string;
  selected: IWorkflowSelectedFramework[];
  conflicts: IWorkflowFrameworkConflict[];
  requiredAgents: string[];
  maximumAutonomyLevel: number;
  authoritySummary: string;
  requiresApproval: boolean;
  approvalReasons: string[];
  evidenceRequirements: string[];
  completionCriteria: string[];
  learningPlan: string[];
  contextRequirements: string[];
  selectionReason: string;
  constitutionVersion: number;
  constitutionSource: string;
}

export interface IWorkflowRecord {
  item: IWorkflowItem;
  checklist: IWorkflowChecklistItem[];
  intake: IWorkflowIntakeRecord[];
  matches: IWorkflowProjectMatch[];
  pursuits: IWorkflowPursuitContext[];
  evidence: IWorkflowEvidenceClaim[];
  openLoops: IWorkflowOpenLoop[];
  proposals: IWorkflowProposal[];
  qualityGates: IWorkflowQualityGate[];
  transitions: IWorkflowTransition[];
  sourceLinks: IWorkflowSourceLink[];
  decisions: IWorkflowDecision[];
  events: IWorkflowEvent[];
  frameworkSelections: IWorkflowFrameworkSelectionProvenance[];
}

export interface IWorkflowIntakeRequest {
  input: string;
  successCriteria?: string[];
  projectKey?: string;
  automationId?: string;
  mandateId?: string;
  requiresReview?: boolean;
  reviewReason?: string;
  sourceType?: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  contentType?: string;
  sender?: string;
  receivedAt?: string;
  trigger?: string;
  actor?: string;
}

export interface IWorkflowTransitionRequest {
  targetState: string;
  message?: string;
  actor?: string;
}

export interface IWorkflowChecklistUpdateRequest {
  status: string;
  note?: string;
  actor?: string;
}

export interface IWorkflowApprovalResolutionRequest {
  approved: boolean;
  note?: string;
  actor?: string;
}

export interface IWorkflowInterruptedExecutionResolutionRequest {
  decision: 'retry' | 'confirm_completed' | 'keep_blocked';
  note: string;
  evidenceUri?: string;
  evidenceLabel?: string;
  priorExecutionReconciled?: boolean;
  actor?: string;
}

export interface IWorkflowProposalResolutionRequest {
  status?: string;
  approved?: boolean;
  selectedOption?: string;
  note?: string;
  actor?: string;
}

export interface IWorkflowRunDueRequest {
  limit?: number;
}

export interface IWorkflowRunResult {
  reviewRequired?: boolean;
  workflowId: string;
  status: string;
  state: string;
  attempts: number;
  verificationStatus?: string;
  nextRunAt?: string;
  message?: string;
  frameworkSelection?: IWorkflowFrameworkSelectionProvenance;
}

export interface IWorkflowRunSummary {
  checked: number;
  completed: number;
  retried: number;
  blocked: number;
  skipped: number;
  results: IWorkflowRunResult[];
}

export interface IWorkflowOpenLoopRunResult {
  workflowId: string;
  openLoopId: string;
  status: string;
  state?: string;
  message?: string;
}

export interface IWorkflowOpenLoopRunSummary {
  checked: number;
  triggered: number;
  resolved: number;
  skipped: number;
  results: IWorkflowOpenLoopRunResult[];
}

export interface IWorkflowClaimRecoveryResult {
  workflowId: string;
  openLoopId?: string;
  type: string;
  status: string;
  message: string;
}

export interface IWorkflowClaimRecoverySummary {
  checked: number;
  workflowsBlocked: number;
  openLoopsReopened: number;
  skipped: number;
  results: IWorkflowClaimRecoveryResult[];
}

export interface IWorkflowCapability {
  id: string;
  name: string;
  status: string;
  implemented: string[];
  next: string[];
}

export interface IWorkflowOverview {
  capabilities: IWorkflowCapability[];
  states: string[];
  safetyRules: string[];
  rules: IWorkflowRule[];
}

export interface IWorkflowDashboard {
  counts: Record<string, number>;
  approvalItems: IWorkflowItem[];
  blockedItems: IWorkflowItem[];
  readyItems: IWorkflowItem[];
  highRiskItems: IWorkflowItem[];
  itemsWithoutNextAction: IWorkflowItem[];
  dueOpenLoops: IWorkflowOpenLoop[];
  rules: IWorkflowRule[];
}

export interface IWorkflowReminderProposal {
  id: string;
  workflowId: string;
  checklistItemId: string;
  title: string;
  label: string;
  projectKey?: string;
  workflowState: string;
  riskLevel: string;
  requiresApproval: boolean;
  reminderAt: string;
  dueAt?: string;
  status: 'due' | 'upcoming';
  sourceUri?: string;
  sourceLabel?: string;
  nextAction: string;
  evidenceDigest: string;
  authority: 'reminder_proposal_only';
  canExecute: false;
}

export interface IWorkflowReminderActivationPrepareRequest {
  expectedReminderDigest: string;
  idempotencyKey: string;
  activationKind: 'internal_notification';
  confirmation: 'PREPARE INTERNAL REMINDER ONLY';
}

export interface IWorkflowReminderActivationRequest {
  id: string;
  workflowId: string;
  checklistItemId: string;
  activationKind: 'internal_notification';
  workflowState: string;
  checklistStatus: 'open';
  reminderAt: string;
  dueAt?: string;
  reminderDigest: string;
  idempotencyKey: string;
  authority: 'reminder_activation_request_only';
  confirmation: 'PREPARE INTERNAL REMINDER ONLY';
  requestDigest: string;
  recordDigest: string;
  requestedAt: string;
  expiresAt: string;
}

export interface IWorkflowReminderActivationDecisionRequest {
  decision: 'approved' | 'rejected' | 'needs_clarification' | 'revoked';
  reason: string;
  confirmation:
    | 'APPROVE INTERNAL REMINDER PREPARATION'
    | 'REJECT INTERNAL REMINDER PREPARATION'
    | 'REQUEST REMINDER CLARIFICATION'
    | 'REVOKE INTERNAL REMINDER PREPARATION';
  expectedActivationRequestDigest: string;
  expectedPreviousDecisionId?: string;
}

export interface IWorkflowReminderActivationDecision {
  id: string;
  activationRequestId: string;
  decision: 'approved' | 'rejected' | 'needs_clarification' | 'revoked';
  reason: string;
  confirmation: IWorkflowReminderActivationDecisionRequest['confirmation'];
  activationRequestDigest: string;
  previousDecisionId?: string;
  authority: 'reminder_activation_decision_only';
  requestDigest: string;
  recordDigest: string;
  decidedAt: string;
  expiresAt?: string;
}

export interface IWorkflowReminderActivationRequestResult {
  request: IWorkflowReminderActivationRequest;
  replayed: boolean;
  authority: 'reminder_activation_request_only';
  canExecute: false;
}

export interface IWorkflowReminderActivationDecisionResult {
  decision: IWorkflowReminderActivationDecision;
  replayed: boolean;
  authority: 'reminder_activation_decision_only';
  canExecute: false;
}

export interface IWorkflowReminderActivationHistoryItem {
  request: IWorkflowReminderActivationRequest;
  latestDecision?: IWorkflowReminderActivationDecision;
  status: 'prepared' | 'approved' | 'rejected' | 'needs_clarification' | 'revoked' | 'expired' | 'stale';
  current: boolean;
  canExecute: false;
}

export interface IWorkflowReminderActivationHistorySnapshot {
  items: IWorkflowReminderActivationHistoryItem[];
  authority: 'reminder_activation_history_only';
  canExecute: false;
  checkedAt: string;
}

export interface IWorkflowReminderActivationDecisionHistory {
  decisions: IWorkflowReminderActivationDecision[];
  authority: 'reminder_activation_decision_only';
  canExecute: false;
}

export interface IWorkflowReminderDeliveryAuthorizeRequest {
  expectedActivationRequestDigest: string;
  expectedActivationDecisionDigest: string;
  expectedReminderDigest: string;
  idempotencyKey: string;
  channel: 'in_app';
  confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER';
}

export interface IWorkflowReminderDeliveryAuthorization {
  id: string;
  activationRequestId: string;
  activationDecisionId: string;
  workflowId: string;
  checklistItemId: string;
  reminderAt: string;
  reminderDigest: string;
  activationRequestDigest: string;
  activationDecisionDigest: string;
  channel: 'in_app';
  idempotencyKey: string;
  authority: 'internal_reminder_delivery_authorization';
  confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER';
  requestDigest: string;
  recordDigest: string;
  authorizedAt: string;
  expiresAt: string;
}

export interface IWorkflowReminderDeliveryAttempt {
  id: string;
  authorizationId: string;
  attemptNumber: number;
  status: 'delivered' | 'retryable_failure' | 'suppressed' | 'dead_lettered' | 'expired';
  reason: string;
  reminderDigest: string;
  authorizationDigest: string;
  authority: 'internal_reminder_delivery_receipt';
  recordDigest: string;
  attemptedAt: string;
}

export interface IWorkflowReminderDeliveryAuthorizationResult {
  authorization: IWorkflowReminderDeliveryAuthorization;
  replayed: boolean;
  authority: 'internal_reminder_delivery_authorization';
  deliveryAuthorized: true;
  canExecute: false;
}

export interface IWorkflowReminderDeliveryHistory {
  authorizations: IWorkflowReminderDeliveryAuthorization[];
  attempts: IWorkflowReminderDeliveryAttempt[];
  authority: 'internal_reminder_delivery_receipt';
  canExecute: false;
}

export interface IWorkflowReminderDeliveryRunSummary {
  checked: number;
  delivered: number;
  retried: number;
  suppressed: number;
  deadLettered: number;
  expired: number;
  results: Array<{ authorizationId: string; status: IWorkflowReminderDeliveryAttempt['status']; reason: string }>;
}

export interface IWorkflowReminderProposalSnapshot {
  items: IWorkflowReminderProposal[];
  due: number;
  upcoming: number;
  authority: 'reminder_proposal_only';
  canExecute: false;
  freshness: {
    status: 'current_internal_reminder_snapshot';
    revalidationRequired: true;
    checkedAt: string;
    reason: string;
  };
}

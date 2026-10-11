export type GovernanceRisk = 'low' | 'medium' | 'high' | 'critical'
export type AuthorizationOutcome = 'authorized' | 'requires_approval' | 'denied'

export interface ExecutionAuthorizationEvidence {
  emergencyStop: {
    active: boolean
    source?: string
    reason?: string
  }
  constitution: {
    id?: string
    version: number
    source?: string
    fingerprint?: string
    requestedCapabilities: string[]
    deniedCapabilities: string[]
    approvalRequiredCapabilities: string[]
    authorityCeiling: number
  }
  mandate: {
    id?: string
    revision?: number
    decisionId?: string
    decisionFingerprint?: string
    outcome?: string
  }
  agent: {
    agentId?: string
    agentRevision?: number
    assignmentId?: string
    grantedAuthority?: number
    grantedAutonomy?: number
    runtimeId?: string
  }
  approval: {
    sourceId?: string
    decisionId?: string
    decisionFingerprint?: string
    approverFingerprint?: string
    approvedAt?: string
    expiresAt?: string
  }
  reasonCodes: string[]
  trace: string[]
}

export interface ExecutionAuthorizationReceipt {
  id: string
  contractVersion: number
  actorKind: string
  actorFingerprint?: string
  taskId: string
  action: string
  stage: string
  resourceType: string
  resourceId?: string
  domain?: string
  outcome: AuthorizationOutcome
  reason: string
  requestFingerprint: string
  decisionFingerprint: string
  requiredAuthority: number
  requestedAutonomy: number
  effectiveAutonomy: number
  risk: GovernanceRisk
  reversible: boolean
  estimatedCostEur: number
  notificationRequired: boolean
  evaluatedAt: string
  evidence: ExecutionAuthorizationEvidence
  lifeGraphProjection?: {
    primaryId: string
    domain: string
    linkedRecords: number
    relations: number
    alreadyExisted: boolean
    advisoryOnly: boolean
    canExecute: boolean
    grantsAuthority: boolean
  }
  lifeGraphProjectionWarning?: string
}

export interface ExecutionAuthorizationSummary {
  id: string
  contractVersion: number
  action: string
  stage: string
  resourceType: string
  resourceId?: string
  domain?: string
  outcome: AuthorizationOutcome
  reason: string
  risk: GovernanceRisk
  evaluatedAt: string
}

export interface ExecutionAuthorizationList {
  receipts: ExecutionAuthorizationSummary[]
  count: number
  limit: number
}

export interface ExecutionAuthorizationConsumption {
  receiptId: string
  consumer: string
  executionTarget: string
  receiptFingerprint: string
  consumedAt: string
}

export type MandateStatus = 'draft' | 'active' | 'revoked'
export type MandateApprovalMode =
  | 'never'
  | 'always'
  | 'at_or_above_autonomy'
  | 'for_risk_or_action'

export interface MandateResourceScope {
  type: string
  ids?: string[]
}

export interface MandateScope {
  id: string
  actions: string[]
  resources?: MandateResourceScope[]
  projects?: string[]
  domains?: string[]
  tools?: string[]
  maximumRisk?: GovernanceRisk
}

export interface MandateApprovalPolicy {
  mode: MandateApprovalMode
  autonomyThreshold?: number
  riskLevels?: GovernanceRisk[]
  actions?: string[]
  approverRoles?: string[]
  maximumEvidenceAgeSeconds?: number
}

export interface MandateStopCondition {
  id: string
  description: string
  factKey: string
  operator:
    | 'equals'
    | 'not_equals'
    | 'present'
    | 'absent'
    | 'greater_or_equal'
    | 'less_or_equal'
  expectedValue?: string
  required: boolean
  effect: 'deny' | 'require_approval'
}

export interface StandingMandate {
  id: string
  ownerIdentity: string
  name: string
  purpose: string
  status: MandateStatus
  version: string
  revision: number
  scopes: MandateScope[]
  autonomyCeiling: number
  approvalPolicy: MandateApprovalPolicy
  stopConditions?: MandateStopCondition[]
  sourceReferences?: string[]
  createdBy: string
  createdAt: string
  updatedAt: string
  activatedAt?: string
  expiresAt?: string
  revokedAt?: string
  revokedBy?: string
  revocationReason?: string
}

export interface StandingMandateList {
  mandates: StandingMandate[]
}

export interface CreateStandingMandateRequest {
  name: string
  purpose: string
  version?: string
  scopes: MandateScope[]
  autonomyCeiling: number
  approvalPolicy: MandateApprovalPolicy
  stopConditions?: MandateStopCondition[]
  sourceReferences?: string[]
  expiresAt?: string
}

export interface MandateAuthorizationRequest {
  action: string
  resourceType: string
  resourceId?: string
  projectKey?: string
  domain?: string
  toolId?: string
  risk: GovernanceRisk
  requestedAutonomy: number
  upstreamApprovalRequired: boolean
  facts?: Record<string, string>
  sourceReferences?: string[]
  requestedAt: string
}

export interface MandateDecisionTrace {
  code: string
  message: string
}

export interface MandateAuthorizationDecision {
  id: string
  mandateId: string
  ownerIdentity: string
  actorIdentity: string
  action: string
  outcome: AuthorizationOutcome
  reason: string
  effectiveAutonomy: number
  approvalRequired: boolean
  approvalSatisfied: boolean
  evaluatedAt: string
  evidence: {
    requestDigest: string
    mandateDigest: string
    decisionDigest: string
    mandateRevision: number
    matchedScopeIds?: string[]
    triggeredStops?: Array<{ conditionId: string; effect: string; reason: string }>
    approvalEvidenceId?: string
    sourceReferences?: string[]
    trace: MandateDecisionTrace[]
  }
}

export interface StandingMandateDecisionList {
  decisions: MandateAuthorizationDecision[]
}

export type LearningProposalStatus =
  | 'review_required'
  | 'governance_required'
  | 'governance_review'
  | 'approved'
  | 'rejected'
  | 'changes_requested'

export type LearningDecisionKind =
  | 'approve'
  | 'reject'
  | 'request_changes'
  | 'escalate_governance'

export interface LearningProposal {
  id: string
  protocolVersion: string
  ownerIdentity: string
  idempotencyKey: string
  revision: number
  status: LearningProposalStatus
  method: string
  target: string
  protectedTarget: boolean
  title: string
  hypothesis: string
  proposedChange: string
  currentVersion: string
  proposedVersion: string
  rollbackPlan: string
  evaluationPlan: string
  evidenceIds: string[]
  proposalDigest: string
  createdAt: string
  updatedAt: string
}

export interface LearningProposalList {
  proposals: LearningProposal[]
}

export interface LearningOutcomeSource {
  id: string
  kind: string
  uri: string
  retrievedAt: string
  contentHash?: string
}

export interface LearningOutcomeMetric {
  name: string
  expected: number
  actual: number
  tolerance: number
  direction: 'exact' | 'at_least' | 'at_most'
  unit?: string
}

export interface LearningOutcomeRecord {
  id: string
  protocolVersion: string
  ownerIdentity: string
  operationId: string
  projectKey?: string
  basis: 'verified_outcome' | 'human_correction'
  status: 'succeeded' | 'partial' | 'failed' | 'corrected'
  summary: string
  verification: string
  sources: LearningOutcomeSource[]
  metrics: LearningOutcomeMetric[]
  tags: string[]
  reconciliation: {
    status: 'matched' | 'partial' | 'diverged'
    passedCriteria: number
    failedCriteria?: string[]
    driftSignals?: string[]
    suggestedMethods: string[]
  }
  evidenceDigest: string
  occurredAt: string
  recordedAt: string
}

export interface LearningOutcomeList {
  outcomes: LearningOutcomeRecord[]
}

export interface LearningReviewDecision {
  id: string
  proposalId: string
  ownerIdentity: string
  proposalRevision: number
  kind: LearningDecisionKind
  actorIdentity: string
  humanConfirmed: boolean
  rationale: string
  governanceReference?: string
  proposalDigest: string
  decisionDigest: string
  decidedAt: string
}

export interface LearningDecisionList {
  decisions: LearningReviewDecision[]
}

export type LearningApplicationMode = 'apply' | 'protected_handoff'

export type LearningApplicationStatus =
  | 'applying'
  | 'applied'
  | 'handoff_pending'
  | 'handoff_ready'
  | 'failed'
  | 'rollback_applying'
  | 'rolled_back'
  | 'rollback_failed'

export interface LearningApplicationEvidence {
  id: string
  kind: string
  uri: string
  digest: string
  recordedAt: string
}

// Deliberately excludes rollback credentials and internal idempotency material.
// The decision API may carry those fields internally, but the operator UI only
// needs evidence-backed status and safe references.
export interface LearningApplicationSummary {
  id: string
  proposalId: string
  proposalRevision: number
  mode: LearningApplicationMode
  status: LearningApplicationStatus
  target: string
  protectedTarget: boolean
  applierId: string
  currentVersion: string
  proposedVersion: string
  appliedVersion?: string
  restoredVersion?: string
  governanceReference?: string
  handoffReference?: string
  evidence: LearningApplicationEvidence[]
  rollbackEvidence: LearningApplicationEvidence[]
  attempt: number
  lastErrorCode?: string
  resultDigest?: string
  createdAt: string
  updatedAt: string
  completedAt?: string
  rolledBackAt?: string
}

export interface LearningDecisionResult {
  proposal: LearningProposal
  application?: LearningApplicationSummary
}

export interface DecideLearningProposalRequest {
  expectedRevision: number
  kind: LearningDecisionKind
  humanConfirmed: boolean
  rationale: string
  governanceReference?: string
}

export type AgentLifecycleState =
  | 'registered'
  | 'enabled'
  | 'draining'
  | 'disabled'
  | 'quarantined'

export interface AgentCapability {
  id: string
  version: string
  operations?: string[]
  description?: string
}

export interface AgentRecord {
  contractVersion: number
  id: string
  ownerIdentity: string
  name: string
  type: 'planner' | 'researcher' | 'executor' | 'reviewer' | 'specialist' | 'orchestrator'
  runtime: {
    id: string
    type: string
    protocolVersion: string
  }
  capabilities: AgentCapability[]
  authorityCeiling: number
  autonomyCeiling: number
  toolAllowlist?: string[]
  dataAllowlist?: string[]
  folderAllowlist?: string[]
  health: {
    status: 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
    ready: boolean
    reason?: string
    checkedAt: string
    freshFor: number
  }
  state: AgentLifecycleState
  availability: {
    available: boolean
    activeAssignments: number
    maxConcurrent: number
  }
  performance: {
    estimatedCostEur: number
    p95LatencyMs: number
    locality: 'local' | 'lan' | 'cloud'
  }
  reliability: {
    successes: number
    failures: number
    consecutiveFailures: number
    meanLatencyMs: number
    lastOutcomeAt?: string
  }
  revision: number
  createdAt: string
  updatedAt: string
}

export interface AgentList {
  agents: AgentRecord[]
}

export interface AgentTransition {
  from: AgentLifecycleState
  to: AgentLifecycleState
  reason: string
  occurredAt: string
  revision: number
}

export interface AgentTransitionList {
  transitions: AgentTransition[]
}

export interface RegisterAgentRequest {
  id: string
  name: string
  type: AgentRecord['type']
  runtime: AgentRecord['runtime']
  capabilities: AgentCapability[]
  authorityCeiling: number
  autonomyCeiling: number
  toolAllowlist?: string[]
  dataAllowlist?: string[]
  folderAllowlist?: string[]
  health: AgentRecord['health']
  availability: AgentRecord['availability']
  performance: AgentRecord['performance']
}

export interface AgentAssignmentRequest {
  taskId: string
  capabilities: Array<{
    id: string
    minVersion?: string
    maxVersion?: string
    operations?: string[]
  }>
  compatibility: {
    runtimeAdapterId?: string
    runtimeType?: string
    minProtocolVersion?: string
    maxProtocolVersion?: string
  }
  requiredAuthority: number
  requiredAutonomy: number
  policyMaxAuthority: number
  policyMaxAutonomy: number
  requiredTools?: string[]
  requiredData?: string[]
  requiredFolders?: string[]
  allowedAgentTypes?: AgentRecord['type'][]
  maxEstimatedCostEur?: number
  requireLocal: boolean
  allowDegraded: boolean
}

export interface AgentAssignment {
  id: string
  ownerIdentity: string
  taskId: string
  agentId: string
  agentRevision: number
  grantedAuthority: number
  grantedAutonomy: number
  score: number
  explanation: {
    eligible: boolean
    components: Array<{ name: string; score: number; reason: string }>
    constraints: string[]
    rejectedReason?: string
  }
  requestDigest: string
  assignedAt: string
}

export type DomainPackPreferenceStatus = 'draft' | 'active' | 'archived'

export interface DomainPlaybookMethod {
  id: string
  version: string
  name: string
  group: string
  domain: string
  purpose: string
  triggerConditions: string[]
  requiredInputs: string[]
  producedOutputs: string[]
  authorityRequirements: string[]
  safetyInvariants: string[]
  riskCeiling: GovernanceRisk
  evidenceRequirements: string[]
  evaluation: {
    method: string
    criteria: string[]
    failureDisposition: string
  }
  provenance: {
    sourceType: string
    title: string
    section: string
    reference: string
  }
  lifecycleStatus: 'active' | 'experimental' | 'deprecated' | 'retired'
}

export interface DomainPackPreference {
  ownerIdentity: string
  packId: string
  catalogVersion: string
  revision: number
  status: DomainPackPreferenceStatus
  enabled?: boolean
  classificationBoost: number
  forceLocalOnly: boolean
  adaptation: {
    notes?: string
  }
  createdAt: string
  updatedAt: string
}

export interface DomainPackView {
  pack: {
    id: string
    version: string
    name: string
    description: string
    sensitive: boolean
    defaultEnabled: boolean
    classificationSignals: Array<{ phrase: string; strength: number; reason: string }>
    intakeQuestions: Array<{ id: string; question: string; required: boolean }>
    riskTriggers: Array<{ id: string; signal: string; level: GovernanceRisk; explanation: string }>
    approvalRules: Array<{ action: string; required: boolean; minimumRisk: GovernanceRisk; reason: string }>
    prohibitedAutonomousActions: Array<{ action: string; reason: string }>
    evidenceRequirements: Array<{
      id: string
      description: string
      requiredForActions: string[]
      minimumVerification: string
    }>
    suitableAgentCapabilities: string[]
    retention: {
      defaultDays: number
      localOnly: boolean
      deletionReview: boolean
      archiveProvenance: boolean
    }
    playbook: {
      version: string
      digest: string
      methods: DomainPlaybookMethod[]
    }
  }
  preference?: DomainPackPreference
  enabled: boolean
  localOnly: boolean
}

export interface DomainPackSummaryView {
  pack: {
    id: string
    version: string
    name: string
    description: string
    sensitive: boolean
    defaultEnabled: boolean
    methodCount?: number
  }
  enabled: boolean
  localOnly: boolean
}

export interface DomainPackCatalog {
  metadata: {
    version: string
    digest: string
    packCount: number
  }
  packs: DomainPackSummaryView[]
}

export interface DomainClassificationResult {
  matches: Array<{
    packId: string
    score: number
    explicit: boolean
    sensitive: boolean
    reasons: string[]
    signals: Array<{ signal: string; strength: number; score: number; reason: string }>
  }>
  suppressed: Array<{
    packId: string
    reason: string
    signals: string[]
  }>
}

export interface UpdateDomainPreferenceRequest {
  expectedRevision?: number
  status: DomainPackPreferenceStatus
  enabled?: boolean
  classificationBoost: number
  forceLocalOnly: boolean
  adaptation: {
    notes?: string
  }
}

export type AdvisoryAvailability =
  | 'not_configured'
  | 'idle'
  | 'loading'
  | 'ready'
  | 'empty'
  | 'stale'
  | 'unavailable'
  | 'error'

export interface AgentTeamMember {
  id: string
  agentId: string
  agentVersion: string
  roleIds: string[]
  capabilityIds: string[]
  status: string
  authorityCeiling: number
  riskCeiling: GovernanceRisk
  evidenceRefs: string[]
  statusChangedAt: string
}

export interface AgentTeamContract {
  id: string
  key: string
  version: string
  revision: number
  status: 'draft' | 'active' | 'suspended' | 'retired' | 'revoked'
  name: string
  purpose: string
  authorityCeiling: number
  riskCeiling: GovernanceRisk
  advisoryOnly: boolean
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
  members: AgentTeamMember[]
  evidenceRefs: string[]
  contractDigest: string
  createdAt: string
  updatedAt: string
}

export interface AgentTeamList {
  teams: AgentTeamContract[]
}

export type AgentTeamMessageAttentionState =
  | 'not_required'
  | 'waiting'
  | 'deferred'
  | 'acknowledged'
  | 'rejected'
  | 'overdue'
  | 'expired'

export interface AgentTeamMessageAcknowledgment {
  id: string
  messageId: string
  correlationId: string
  recipientId: string
  status: 'accepted' | 'rejected' | 'deferred'
  reason?: string
  createdAt: string
  retryAfter?: string
  idempotencyKey: string
}

export interface AgentTeamMessageAttention {
  messageId: string
  correlationId: string
  recipientId: string
  subject: string
  requiresAcknowledgment: boolean
  state: AgentTeamMessageAttentionState
  reason: string
  dueAt?: string
  expiresAt: string
  latestAcknowledgment?: AgentTeamMessageAcknowledgment
  humanReviewRequired: boolean
  advisoryOnly: boolean
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
}

export interface AgentTeamMessageAttentionPage {
  generatedAt: string
  messages: AgentTeamMessageAttention[]
}

export interface LifeOntologyProvenance {
  referenceId?: string
  uri?: string
  contentDigest: string
  authority?: string
  capturedAt: string
  localOnly: boolean
}

export interface LifeOntologyExternalKey {
  namespace: string
  value: string
}

export interface LifeOntologyEntity {
  id: string
  ownerIdentity?: string
  type: string
  domain: string
  name: string
  summary?: string
  externalKeys?: LifeOntologyExternalKey[]
  attributes?: Record<string, string>
  status: string
  priority: number
  dueAt?: string
  observedAt: string
  confidence: number
  verificationStatus: string
  provenance: LifeOntologyProvenance[]
  sensitivity: string
  localOnly: boolean
  entityDigest: string
  createdAt: string
}

export interface LifeOntologyRelation {
  id: string
  type: string
  fromEntityId: string
  toEntityId: string
  summary?: string
  observedAt: string
  confidence: number
  verificationStatus: string
  provenance: LifeOntologyProvenance[]
  localOnly: boolean
  relationDigest: string
  createdAt: string
}

export interface LifeOntologyMergeProposal {
  id: string
  candidateEntityIds: string[]
  match: string
  reasons: string[]
  confidence: number
  status: string
  proposalDigest: string
  createdAt: string
  advisoryOnly: boolean
  canExecute: boolean
  grantsAuthority: boolean
}

export interface LifeOntologyEntityList {
  entities: LifeOntologyEntity[]
}

export interface LifeOntologyRelationList {
  relations: LifeOntologyRelation[]
}

export interface LifeOntologyMergeProposalList {
  proposals: LifeOntologyMergeProposal[]
}

export type ContactReviewSubject = 'candidate' | 'merge_proposal'
export type ContactCandidateReviewAction = 'promote' | 'correct' | 'reject'
export type ContactMergeReviewAction = 'merge' | 'keep_distinct' | 'reject'
export type ContactReviewAction = ContactCandidateReviewAction | ContactMergeReviewAction

export interface ContactReviewDecision {
  contractVersion: string
  id: string
  ownerIdentity: string
  idempotencyKey: string
  subject: ContactReviewSubject
  subjectId: string
  action: ContactReviewAction
  candidateEntityIds: string[]
  canonicalEntityId?: string
  canonicalName?: string
  canonicalSummary?: string
  reason: string
  decidedAt: string
  recordedAt: string
  requestDigest: string
  recordDigest: string
  localOnly: boolean
  canExecute: boolean
  grantsAuthority: boolean
}

export interface ContactReviewDecisionList {
  decisions: ContactReviewDecision[]
}

export interface ContactReviewDecisionRequest {
  action: ContactReviewAction
  canonicalName?: string
  canonicalSummary?: string
  reason: string
  idempotencyKey: string
}

export interface ContactReviewDecisionResult {
  decision: ContactReviewDecision
  canonicalEntity?: LifeOntologyEntity
  alreadyExisted: boolean
}

export interface LifeOntologyEntityFilters {
  types?: string[]
  statuses?: string[]
  verification?: string[]
}

export type LifeLedgerVerificationStatus =
  | 'source_supported'
  | 'human_confirmed'
  | 'verified'
  | 'needs_review'
  | 'disputed'

export interface LifeLedgerEvidenceReference {
  id: string
  uri: string
  contentDigest: string
  authority?: string
  observedAt: string
  verification: LifeLedgerVerificationStatus
  localOnly: boolean
}

export type LifeCommitmentStatus =
  | 'proposed'
  | 'active'
  | 'waiting'
  | 'fulfilled'
  | 'cancelled'
  | 'breached'
  | 'disputed'

export interface LifeCommitmentRevision {
  contractVersion: string
  id: string
  ownerIdentity: string
  commitmentKey: string
  revision: number
  domain: OutcomeLifeDomain
  title: string
  summary?: string
  status: LifeCommitmentStatus
  counterparty?: string
  projectKey?: string
  dueAt?: string
  verification: LifeLedgerVerificationStatus
  evidence: LifeLedgerEvidenceReference[]
  localOnly: boolean
  idempotencyKey: string
  requestDigest: string
  recordDigest: string
  observedAt: string
  recordedAt: string
  lifeGraphWarning?: string
  lifeGraph?: OutcomeLifeGraphProjection
}

export interface LifeCommitmentList {
  commitments: LifeCommitmentRevision[]
}

export interface LifeCommitmentHistory {
  revisions: LifeCommitmentRevision[]
}

export interface RecordLifeCommitmentRevisionRequest {
  expectedRevision: number
  domain: OutcomeLifeDomain
  title: string
  summary?: string
  status: LifeCommitmentStatus
  counterparty?: string
  projectKey?: string
  dueAt?: string
  verification: LifeLedgerVerificationStatus
  evidence: LifeLedgerEvidenceReference[]
  idempotencyKey: string
  observedAt: string
}

export interface LifeCommitmentWriteResult {
  record: LifeCommitmentRevision
  created: boolean
}

export type LifeCostKind = 'estimate' | 'incurred' | 'paid' | 'refund'

export interface LifeCostEntry {
  contractVersion: string
  id: string
  ownerIdentity: string
  domain: OutcomeLifeDomain
  title: string
  summary?: string
  kind: LifeCostKind
  amountMinor: number
  currency: string
  commitmentKey?: string
  projectKey?: string
  verification: LifeLedgerVerificationStatus
  evidence: LifeLedgerEvidenceReference[]
  localOnly: boolean
  idempotencyKey: string
  requestDigest: string
  recordDigest: string
  observedAt: string
  recordedAt: string
  lifeGraphWarning?: string
  lifeGraph?: OutcomeLifeGraphProjection
}

export interface LifeCostList {
  costs: LifeCostEntry[]
}

export interface RecordLifeCostEventRequest {
  domain: OutcomeLifeDomain
  title: string
  summary?: string
  kind: LifeCostKind
  amountMinor: number
  currency: string
  commitmentKey?: string
  projectKey?: string
  verification: LifeLedgerVerificationStatus
  evidence: LifeLedgerEvidenceReference[]
  idempotencyKey: string
  observedAt: string
}

export interface LifeCostWriteResult {
  record: LifeCostEntry
  created: boolean
}

export interface ProactivityPreferences {
  contractVersion: number
  timeZone: string
  quietHours: {
    enabled: boolean
    startMinute: number
    endMinute: number
    timeZone: string
  }
  minimumConfidence: number
  ambientThreshold: number
  dailyBriefThreshold: number
  notifyThreshold: number
  reviewThreshold: number
  cooldown: number
  attentionBudget: { maxInterruptionsPerDay: number }
  channels: Array<{ channel: string; enabled: boolean; order: number }>
  allowExternalChannels: boolean
}

export interface ProactivityPolicyRecord {
  contractVersion: number
  policy: ProactivityPreferences
  recordedAt: string
}

export interface ProactivitySignal {
  id: string
  openLoopKey: string
  title: string
  summary: string
  status: 'open' | 'resolved'
  risk: GovernanceRisk
  observedAt: string
  lastActivityAt: string
  deadline?: string
  confidence: number
  sensitive: boolean
  humanReviewRequired: boolean
}

export interface ProactivitySignalRecord {
  contractVersion: number
  signal: ProactivitySignal
  recordedAt: string
}

export interface ProactivityDecision {
  signalId: string
  openLoopKey: string
  signalDigest: string
  title: string
  summary: string
  outcome: 'suppress' | 'ambient' | 'daily_brief' | 'notify' | 'require_review'
  score: number
  reasons: string[]
  recommendedChannels: string[]
  nextEligibleAt?: string
  budgetCost: number
  executionAuthorized: boolean
  deliveryAuthorized: boolean
  authorityGranted: boolean
  decidedAt: string
}

export interface ProactivityDecisionRecord {
  contractVersion: number
  decision: ProactivityDecision
  recordedAt: string
}

export interface ProactivitySignalList {
  signals: ProactivitySignalRecord[]
}

export interface ProactivityDecisionList {
  decisions: ProactivityDecisionRecord[]
}

export type ProactivityFeedbackAction = 'accept' | 'dismiss' | 'snooze' | 'suppress' | 'resume'

export interface ProactivityFeedbackRecord {
  contractVersion: number
  id: string
  ownerIdentity: string
  signalId: string
  openLoopKey: string
  signalDigest: string
  sourceOutcome: ProactivityDecision['outcome']
  sourceDecisionAt: string
  action: ProactivityFeedbackAction
  reason: string
  snoozedUntil?: string
  previousRecordDigest?: string
  recordDigest: string
  recordedAt: string
  authority: 'attention_feedback_only'
  canExecute: false
  deliveryAuthorized: false
  executionAuthorized: false
}

export interface ProactivityFeedbackList {
  feedback: ProactivityFeedbackRecord[]
}

export interface RecordProactivityFeedbackRequest {
  idempotencyKey: string
  signalId: string
  openLoopKey: string
  signalDigest: string
  action: ProactivityFeedbackAction
  reason: string
  snoozedUntil?: string
}

export interface OutcomeScope {
  ownerId: string
  workspaceId: string
}

export type OutcomeLifeDomain =
  | 'safety_security'
  | 'health_wellbeing'
  | 'relationships_care'
  | 'housing_assets'
  | 'financial'
  | 'work_venture'
  | 'learning_growth'
  | 'meaning_values'
  | 'community_civic'
  | 'legal_government'
  | 'personal_administration'

export interface IntendedOutcome {
  id: string
  scope: OutcomeScope
  statement: string
  lifeDomain?: OutcomeLifeDomain
  window: { start: string; end: string }
  indicators: Array<{
    id: string
    name: string
    unit: string
    direction: 'higher' | 'lower' | 'maintain'
    targetValue: number
    targetTolerance: number
    trendThresholdPerDay: number
    regressionThreshold: number
    minimumObservations: number
    baseline: OutcomeBaseline
  }>
}

export type OutcomeVerification =
  | 'unverified'
  | 'user_confirmed'
  | 'source_supported'
  | 'verified'
  | 'disputed'

export interface OutcomeBaseline {
  id: string
  value: number
  observedAt: string
  verification: OutcomeVerification
  sources?: OutcomeSourceReference[]
}

export interface OutcomeSourceReference {
  id: string
  uri: string
  contentDigest?: string
  retrievedAt: string
  status: 'unreviewed' | 'supported' | 'verified' | 'disputed'
}

export interface StoreOutcomeRequest {
  idempotencyKey: string
  expectedRevision: number
  outcome: Omit<IntendedOutcome, 'id' | 'scope'>
}

export interface OutcomeObservationInput {
  id: string
  indicatorId: string
  value: number
  observedAt: string
  recordedAt: string
  verification: OutcomeVerification
  sources?: OutcomeSourceReference[]
  attribution: {
    method: 'unknown' | 'user_report' | 'correlation' | 'controlled_study' | 'model_estimate'
    confidence: number
    rationale: string
  }
}

export interface CreateOutcomeEvaluationRequest {
  idempotencyKey: string
  outcomeRevision: number
  observations: OutcomeObservationInput[]
  corrections?: unknown[]
  asOf: string
}

export interface OutcomeRevision {
  outcome: IntendedOutcome
  revision: number
  recordedAt: string
  auditDigest: string
  lifeGraphProjection?: OutcomeLifeGraphProjection
  lifeGraphProjectionWarning?: string
}

export interface OutcomeLifeGraphProjection {
  primary: LifeOntologyEntity
  linkedEntities: LifeOntologyEntity[]
  relations: LifeOntologyRelation[]
  alreadyExisted: boolean
  advisoryOnly: boolean
  canExecute: boolean
  grantsAuthority: boolean
}

export interface OutcomeEvaluation {
  id: string
  outcomeId: string
  asOf: string
  state: 'insufficient_evidence' | 'on_track' | 'achieved' | 'regression' | 'review_required'
  reviewRequired: boolean
  reviewReasons: string[]
  recommendations: Array<{
    id: string
    kind: string
    indicatorId: string
    summary: string
    control: {
      advisoryOnly: boolean
      reviewRequired: boolean
      executionAuthority: string
      mayExecute: boolean
      mayChangePolicy: boolean
    }
  }>
  auditDigest: string
}

export interface OutcomeEvaluationRecord {
  evaluation: OutcomeEvaluation
  outcomeRevision: number
  recordedAt: string
  recordDigest: string
  lifeGraphProjection?: OutcomeLifeGraphProjection
  lifeGraphProjectionWarning?: string
}

export interface OutcomeCorrectionRecord {
  outcomeId: string
  outcomeRevision: number
  correction: {
    id: string
    observationId: string
    userConfirmed: boolean
    correctedValue: number
    correctedVerification: string
    reason: string
    correctedAt: string
  }
  recordedAt: string
  auditDigest: string
}

export interface OutcomeEvaluationList {
  evaluations: OutcomeEvaluationRecord[]
}

export interface OutcomeCorrectionList {
  corrections: OutcomeCorrectionRecord[]
}

export interface AdvisoryAuthorityBoundary {
  mode: string
  canExecute: boolean
  grantsAuthority: boolean
  consumesApproval: boolean
  dispatchesWork: boolean
}

export interface ResilienceStatus {
  contractVersion: number
  scope: OutcomeScope
  generatedAt: string
  leaseCount: number
  workerCount: number
  retryCount: number
  circuitCount: number
  recoveryCount: number
  latestEvent?: {
    type: string
    subjectId: string
    recordedAt?: string
    occurredAt?: string
  }
  authority: AdvisoryAuthorityBoundary
}

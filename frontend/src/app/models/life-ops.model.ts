export type LifeDomainId = string
export type CapacityStatus =
  | 'unknown'
  | 'available'
  | 'constrained'
  | 'overloaded'
  | 'unavailable'
  | 'recovering'

export interface LifeDomain {
  id: LifeDomainId
  name: string
  description: string
  needClass: string
  sensitive: boolean
}

export interface EntityDomainLink {
  id: string
  ownerIdentity: string
  entityType: string
  entityId: string
  domainId: LifeDomainId
  primary: boolean
  confidence: number
  sourceLabel: string
  sourceUri?: string
  evidence: string[]
  verificationStatus: string
  createdAt: string
  updatedAt: string
}

export interface LinkEntityRequest {
  entityType: string
  entityId: string
  domainId: LifeDomainId
  primary: boolean
  confidence: number
  sourceLabel: string
  sourceUri?: string
  evidence?: string[]
  verificationStatus?: string
}

export interface NeedObservation {
  id: string
  ownerIdentity: string
  domainId: LifeDomainId
  needLevel: string
  state: string
  currentLevel: number
  targetLevel: number
  gap: number
  priority: number
  confidence: number
  evidence: string[]
  sourceLabel: string
  sourceUri?: string
  observedAt: string
  expiresAt?: string
  needsReview: boolean
  createdAt: string
}

export interface RecordNeedRequest {
  domainId: LifeDomainId
  needLevel: string
  state: string
  currentLevel: number
  targetLevel: number
  priority: number
  confidence: number
  evidence?: string[]
  sourceLabel: string
  sourceUri?: string
  observedAt: string
  expiresAt?: string
  needsReview: boolean
}

export interface CapacitySignals {
  energy: number
  attentionQuality: number
  painIllnessLoad: number
  sleepQuality: number
  stressLoad: number
  mobility: number
  financialLiquidity: number
  deadlinePressure: number
  interruptionSensitivity: number
  recoveryRequirement: number
  taskSwitchingCost: number
  sensoryLoad: number
  decisionFatigue: number
  riskTolerance: number
  confidenceReadiness: number
  location?: string
  availableTools?: string[]
  availableHelpers?: string[]
  weatherConditions?: string
  environmentalConditions?: string
  socialAppropriateness?: string
}

export interface CapacitySnapshot {
  id: string
  ownerIdentity: string
  status: CapacityStatus
  signals: CapacitySignals
  timeAvailableMinutes: number
  concurrentWorkLimit: number
  currentLoad: number
  planningStepLimit: number
  constraints: string[]
  sourceLabel: string
  sourceUri?: string
  capturedAt: string
  confidence: number
  fresh: boolean
  needsReview: boolean
  createdAt: string
}

export interface RecordCapacityRequest {
  status: CapacityStatus
  signals: CapacitySignals
  timeAvailableMinutes: number
  concurrentWorkLimit: number
  currentLoad: number
  planningStepLimit?: number
  constraints?: string[]
  sourceLabel: string
  sourceUri?: string
  capturedAt: string
  confidence: number
  needsReview: boolean
}

export type GoalLevel =
  | 'values_principles'
  | 'needs_responsibilities'
  | 'vision_future_state'
  | 'strategic_outcome'
  | 'pursuit'
  | 'programme_case'
  | 'project'
  | 'workflow'
  | 'task'
  | 'atomic_action'
  | 'verification_condition'
  | 'measured_outcome'

export interface GoalNode {
  id: string
  ownerIdentity: string
  parentId?: string
  level: GoalLevel
  domainIds: LifeDomainId[]
  title: string
  description?: string
  successCriteria: string[]
  stopConditions: string[]
  status: string
  confidence: number
  sourceLabel: string
  sourceUri?: string
  targetAt?: string
  createdAt: string
  updatedAt: string
}

export interface CreateGoalRequest {
  parentId?: string
  level: GoalLevel
  domainIds: LifeDomainId[]
  title: string
  description?: string
  successCriteria?: string[]
  stopConditions?: string[]
  status?: string
  confidence: number
  sourceLabel: string
  sourceUri?: string
  targetAt?: string
}

export interface UpdateGoalRequest {
  parentId?: string
  clearParent?: boolean
  level?: GoalLevel
  domainIds?: LifeDomainId[]
  title?: string
  description?: string
  successCriteria?: string[]
  stopConditions?: string[]
  status?: string
  confidence?: number
  sourceLabel?: string
  sourceUri?: string
  targetAt?: string
  clearTarget?: boolean
}

export interface GoalTreeNode {
  goal: GoalNode
  children: GoalTreeNode[]
}

export interface LifeOpsOverview {
  domains: LifeDomain[]
  needs: NeedObservation[]
  capacity: CapacitySnapshot | null
  goals: GoalNode[]
  forest: GoalTreeNode[]
}

export interface PriorityFactors {
  importance: number
  urgency: number
  humanNeedAffected: number
  deadlinePressure: number
  costOfDelay: number
  expectedValue: number
  harmAvoided: number
  probabilityOfSuccess: number
  effort: number
  duration: number
  dependencies: number
  reversibility: number
  risk: number
  legalObligation: number
  relationshipConsequences: number
  availableCapacity: number
  energyFit: number
  opportunityCost: number
  strategicAlignment: number
  learningValue: number
  compoundingValue: number
  staleness: number
  commitmentAge: number
  peopleBlocked: number
  delegability: number
}

export type PriorityFactorKey = keyof PriorityFactors

export interface PriorityAssessmentRequest {
  entityType: string
  entityId: string
  title: string
  deadline?: string
  factors: PriorityFactors
  capacity?: CapacitySnapshot
}

export interface FactorContribution {
  factor: string
  input: number
  effectiveInput: number
  weight: number
  contribution: number
  costFactor: boolean
  reason: string
}

export interface PriorityAssessment {
  id: string
  ownerIdentity: string
  entityType: string
  entityId: string
  title: string
  score: number
  band: string
  factors: PriorityFactors
  contributions: FactorContribution[]
  reasons: string[]
  capacityApplied: boolean
  algorithmVersion: string
  assessedAt: string
}

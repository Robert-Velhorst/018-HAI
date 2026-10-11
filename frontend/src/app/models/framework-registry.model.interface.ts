export type FrameworkLifecycleStatus = 'active' | 'experimental' | 'deprecated';
export type FrameworkPreferenceState = 'default' | 'enabled' | 'disabled';
export type FrameworkViewMode = 'basic' | 'advanced';

export const PROTECTED_FRAMEWORK_IDS = [
  'human-sovereignty',
  'intake-triage',
  'autonomy-levels',
  'approval-control',
  'truth-evidence',
  'privacy-protection',
  'security-zero-trust',
  'agent-threat-modeling',
  'reliable-execution',
  'evaluation',
] as const;

export function isProtectedFrameworkId(id: string): boolean {
  const normalizedId = id.trim();
  return PROTECTED_FRAMEWORK_IDS.some((protectedId) => protectedId === normalizedId);
}

export interface IFramework {
  id: string;
  version: string;
  name: string;
  family: string;
  purpose: string;
  suitableProblemTypes: string[];
  triggerConditions: string[];
  requiredInputs: string[];
  producedOutputs: string[];
  requiredAgents: string[];
  workflowTemplate: string[];
  decisionRules: string[];
  safetyInvariants: string[];
  authorityRequirement: string;
  maximumAutonomyLevel: number;
  riskCeiling: string;
  evidenceRequirements: string[];
  evaluationMethod: string[];
  conflictsWith: string[];
  userSpecificAdaptations: string[];
  candidateImplementations?: string[];
  source: string;
  provenance: string;
  status: FrameworkLifecycleStatus;
}

export interface IFrameworkPreference {
  frameworkId: string;
  state: FrameworkPreferenceState;
  pinned: boolean;
  maximumAutonomyLevel?: number;
  adaptations: string[];
  updatedAt: string;
}

export interface IFrameworkView extends IFramework {
  effectiveStatus: string;
  enabled: boolean;
  pinned: boolean;
  effectiveAutonomyLevel: number;
  adaptations: string[];
  preferenceUpdatedAt?: string;
}

export interface IFrameworkFamilyRecord extends IFramework {
  section: number;
}

export interface IFrameworkFamilyTaxonomy {
  version: string;
  digest: string;
  families: IFrameworkFamilyRecord[];
}

export interface IFrameworkPreferencePatch {
  state: FrameworkPreferenceState;
  pinned?: boolean;
  maximumAutonomyLevel?: number;
  clearAutonomyOverride?: boolean;
  adaptations?: string[];
}

export interface IFrameworkSelectionRequest {
  taskPlanId?: string;
  request: string;
  projectKey?: string;
  pursuitId?: string;
  taskType?: string;
  difficulty?: number;
  requiredReasoning?: string;
  successCriteria?: string[];
  needsMemory?: boolean;
  needsTools?: boolean;
  needsDocuments?: boolean;
  needsWebAccess?: boolean;
  needsLocalExecution?: boolean;
  executeRequested?: boolean;
}

export interface ISelectedFramework {
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

export interface IFrameworkConflict {
  selectedId: string;
  skippedId: string;
  reason: string;
}

export interface ILifeDomainAssignment {
  id: string;
  need: string;
  score: number;
  confidence: number;
  signals: string[];
  primary: boolean;
  source: string;
}

export interface INeedStateAssessment {
  id: string;
  domainId?: string;
  level: string;
  state: string;
  priority: number;
  confidence: number;
  evidence: string[];
  source: string;
  needsReview: boolean;
}

export interface ICapacitySnapshot {
  status: string;
  energy?: number;
  attention?: number;
  timeAvailableMinutes?: number;
  concurrentWorkLimit?: number;
  currentLoad?: number;
  planningStepLimit: number;
  constraints: string[];
  sourceUri?: string;
  sourceLabel?: string;
  capturedAt?: string;
  confidence: number;
  fresh: boolean;
  needsReview: boolean;
}

export interface IAgentCard {
  id: string;
  name: string;
  owner: string;
  purpose: string;
  role: string;
  capabilities: string[];
  domainCompetence: string[];
  allowedTools: string[];
  requiredPermissions: string[];
  dataAccessBoundaries: string[];
  costProfile: string;
  modelRequirements: string[];
  reliabilityHistory: string[];
  allowedActions: string[];
  prohibitedActions: string[];
  inputSchema: string;
  outputSchema: string;
  expectedEvidence: string[];
  escalationRoute: string;
  availability: string;
  version: string;
  dependencies: string[];
  healthStatus: string;
  evaluationScore: number;
  evaluationScoreSource: string;
  authorityCeiling: number;
  status: string;
  verified: boolean;
  revoked: boolean;
  revocationReason?: string;
  provenance: string;
  lastVerifiedAt?: string;
}

export interface IDelegationContract {
  id: string;
  delegator: string;
  delegatee: string;
  objective: string;
  allowedActions: string[];
  prohibitedActions: string[];
  budgetLimitEur: number;
  budgetPolicy: string;
  deadline?: string;
  deadlineStatus: string;
  constraints: string[];
  authorityCeiling: number;
  requiresApproval: boolean;
  evidenceRequired: string[];
  completionCriteria: string[];
  escalationTriggers: string[];
  state: string;
}

export interface ICommunicationContract {
  schemaVersion: string;
  allowedMessageTypes: string[];
  allowedConfidentiality: string[];
  requiredFields: string[];
  forbiddenContent: string[];
  maximumAuthority: number;
  maximumPayloadChars: number;
  maximumTtlSeconds: number;
  redactionRequired: boolean;
  idempotencyRequired: boolean;
  provenanceRequired: boolean;
  signaturePolicy: string;
  correlationId: string;
}

export interface ICoordinationPlan {
  mode: string;
  allowedModes: string[];
  coordinator: string;
  participants: string[];
  handoffOrder: string[];
  consensusRule: string;
  escalationRule: string;
  rationale: string;
}

export interface IActionAutonomyDecision {
  action: string;
  requiredLevel: number;
  effectiveCeiling: number;
  levelName: string;
  allowed: boolean;
  requiresApproval: boolean;
  reason: string;
}

export interface IChiefOfStaffDecision {
  needsAttention: string;
  whyNow: string;
  contextNeeded: string;
  whoShouldAct: string;
  howToProceed: string;
  mayProceedNow: string;
  needsApproval: string;
  completionProof: string;
}

export interface IFrameworkSelectionDecision {
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
  selected: ISelectedFramework[];
  conflicts: IFrameworkConflict[];
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
  lifeDomains?: ILifeDomainAssignment[];
  needsState?: INeedStateAssessment[];
  capacity?: ICapacitySnapshot;
  agentCards?: IAgentCard[];
  delegations?: IDelegationContract[];
  communication?: ICommunicationContract;
  coordination?: ICoordinationPlan;
  actionAutonomy?: IActionAutonomyDecision[];
  stopConditions?: string[];
  outcomeMonitoring?: string[];
  chiefOfStaff?: IChiefOfStaffDecision;
  operatingContractDigest?: string;
}

export interface IConstitution {
  id: string;
  version: number;
  baseVersion: number;
  status: 'draft' | 'active' | 'superseded';
  values: string[];
  prohibitions: string[];
  standingPermissions: string[];
  preferences: string[];
  relationshipRules: string[];
  financialBoundaries: string[];
  communicationRules: string[];
  escalationRules: string[];
  protectedRules: string[];
  changeSummary?: string;
  approvedBy?: string;
  approvedAt?: string;
  createdAt: string;
}

export interface IConstitutionHistoryEntry {
  id: string;
  version: number;
  baseVersion: number;
  status: 'draft' | 'active' | 'superseded';
  changeSummary: string;
  approvedBy?: string;
  approvedAt?: string;
  createdAt: string;
  digest: string;
}

export interface IConstitutionHistoryPage {
  history: IConstitutionHistoryEntry[];
  limit: number;
  truncated: boolean;
}

export interface IConstitutionSnapshot {
  constitution: IConstitution;
  source: string;
}

export interface IConstitutionDraftRequest {
  baseVersion?: number;
  values: string[];
  prohibitions: string[];
  standingPermissions: string[];
  preferences: string[];
  relationshipRules: string[];
  financialBoundaries: string[];
  communicationRules: string[];
  escalationRules: string[];
  changeSummary: string;
}

export interface IActivateConstitutionRequest {
  confirmation: string;
  approvalNote: string;
}

export interface IFrameworkRegistryOverview {
  generatedAt: string;
  total: number;
  enabled: number;
  experimental: number;
  deprecated: number;
  pinned: number;
  families: Record<string, number>;
  constitutionVersion: number;
  constitutionSource: string;
  recentSelections: number;
  selectionContract: string[];
}

export interface IFrameworkModuleViewPreferences {
  version: 1;
  mode: FrameworkViewMode;
  openSections: Record<string, boolean>;
}

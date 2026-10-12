export type AgentTeamStatus = 'draft' | 'active' | 'suspended' | 'retired' | 'revoked'
export type AgentTeamRisk = 'low' | 'medium' | 'high' | 'critical'
export type TeamMembershipStatus = 'invited' | 'active' | 'suspended' | 'left' | 'revoked'
export type TeamVote = 'support' | 'oppose' | 'abstain'

export interface AgentRef {
  id: string
  role: string
  authorityCeiling: number
}

export interface AgentTeamRole {
  id: string
  name: string
  purpose: string
  capabilityIds: string[]
  allowedRecommendationTypes: string[]
  prohibitedActions: string[]
  evidenceRequirements: string[]
  authorityCeiling: number
  riskCeiling: AgentTeamRisk
  mayCoordinate: boolean
  mayVote: boolean
  advisoryOnly: boolean
}

export interface AgentTeamCapability {
  id: string
  name: string
  description: string
  inputSchema: string
  outputSchema: string
  evidenceRequired: string[]
  prohibitedActions: string[]
  authorityCeiling: number
  riskCeiling: AgentTeamRisk
  advisoryOnly: boolean
}

export interface AgentTeamMember {
  id: string
  agentId: string
  agentVersion: string
  roleIds: string[]
  capabilityIds: string[]
  status: TeamMembershipStatus
  authorityCeiling: number
  riskCeiling: AgentTeamRisk
  evidenceRefs: string[]
  provenanceDigest: string
  joinedAt?: string
  statusChangedAt: string
  revokedAt?: string
  revocationReason?: string
}

export interface AgentTeamConsensusPolicy {
  mode: 'unanimous' | 'majority' | 'quorum'
  decisionPayloadSchema: string
  quorum: number
  minimumSupport: number
  allowAbstention: boolean
  requireEvidence: boolean
  conflictEscalationRequired: boolean
  tieOutcome: string
}

export interface AgentTeamContract {
  id: string
  key: string
  version: string
  revision: number
  status: AgentTeamStatus
  name: string
  purpose: string
  authorityCeiling: number
  riskCeiling: AgentTeamRisk
  maximumDelegatedAuthority: number
  maximumDelegatedRisk: AgentTeamRisk
  advisoryOnly: boolean
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
  roles: AgentTeamRole[]
  capabilities: AgentTeamCapability[]
  members: AgentTeamMember[]
  consensus: AgentTeamConsensusPolicy
  evidenceRefs: string[]
  provenance: {
    source: string
    reference?: string
    authoredBy: string
    registeredBy: string
    registeredAt: string
    evidenceDigest: string
  }
  previousVersionDigest?: string
  contractDigest: string
  createdAt: string
  updatedAt: string
  activatedAt?: string
  suspendedAt?: string
  retiredAt?: string
  revokedAt?: string
  revocationReason?: string
}

export interface AgentCoordinationMessage {
  id: string
  idempotencyKey: string
  correlationId: string
  causationId?: string
  schemaVersion: string
  type: string
  sender: AgentRef
  recipient: AgentRef
  confidentiality: string
  authorityLevel: number
  payload: { schema: string; subject: string; data: unknown }
  payloadDigest: string
  evidenceRefs: string[]
  requiresAck: boolean
  createdAt: string
  expiresAt: string
  humanApprovalRef?: string
  provenanceSummary: string
}

export interface AgentTeamAcknowledgment {
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

export interface AgentTeamAttention {
  messageId: string
  correlationId: string
  recipientId: string
  subject: string
  requiresAcknowledgment: boolean
  state: string
  reason: string
  dueAt?: string
  expiresAt: string
  latestAcknowledgment?: AgentTeamAcknowledgment
  humanReviewRequired: boolean
  advisoryOnly: boolean
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
}

export interface AgentTeamDecisionOverview {
  generatedAt: string
  messages: AgentCoordinationMessage[]
  attention: AgentTeamAttention[]
}

export interface AgentTeamConsensusOutcome {
  id: string
  teamId: string
  teamVersion: string
  correlationId: string
  idempotencyKey: string
  issue: string
  mode: string
  status: string
  recommendation: string
  decisionMessageIds: string[]
  conflicts: unknown[]
  supportCount: number
  opposeCount: number
  abstainCount: number
  evidenceRefs: string[]
  provenanceDigest: string
  outcomeDigest: string
  advisoryOnly: boolean
  grantsExecutionAuthority: boolean
  executionAuthorizationRequired: boolean
  recordedAt: string
}

export interface AgentTeamLifecycleEvent {
  sequence: number
  id: string
  teamId: string
  teamVersion: string
  revision: number
  type: string
  actor: string
  subjectId?: string
  reason: string
  evidenceRefs: string[]
  provenanceDigest: string
  occurredAt: string
  previousEventDigest?: string
  eventDigest: string
}

export interface CreateGuidedAgentTeamRequest {
  key: string
  version: string
  name: string
  purpose: string
  authorityCeiling: number
  riskCeiling: AgentTeamRisk
  maximumDelegatedAuthority: number
  maximumDelegatedRisk: AgentTeamRisk
  consensusMode: 'unanimous' | 'majority' | 'quorum'
  quorum: number
  minimumSupport: number
  allowAbstention: boolean
  evidenceRefs: string[]
  actor: string
}

export interface TeamTransitionRequest {
  expectedRevision: number
  actor: string
  reason: string
  evidenceRefs: string[]
}

export interface AddTeamMemberRequest {
  expectedRevision: number
  actor: string
  reason: string
  member: Partial<AgentTeamMember>
}

export interface ChangeTeamMembershipRequest extends TeamTransitionRequest {
  status: TeamMembershipStatus
}

export interface CreateTeamDecisionMessageRequest {
  senderMembershipId: string
  recipientMembershipId: string
  correlationId: string
  idempotencyKey: string
  issue: string
  position: TeamVote
  recommendation: string
  evidenceRefs: string[]
  requiresAcknowledgment: boolean
  expiresInMinutes: number
}

export interface CreateTeamAcknowledgmentRequest {
  status: 'accepted' | 'rejected' | 'deferred'
  reason: string
  retryAfterMinutes: number
  idempotencyKey: string
}

export interface RecordTeamConsensusRequest {
  correlationId: string
  idempotencyKey: string
  issue: string
}

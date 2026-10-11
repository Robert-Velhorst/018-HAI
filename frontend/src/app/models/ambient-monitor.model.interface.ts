export interface AmbientMonitorAuthority {
  label: 'advisory_monitor_only'
  canExecute: false
  canDeliver: false
  canNotify: false
  canWriteCalendar: false
  canMutateWorkflow: false
  canAuthorizeMandate: false
  canMutateLearning: false
}

export interface AmbientMonitorScope {
  ownerId: string
  workspaceId: string
}

export type MonitorSourceKind =
  | 'workflow_open_loop_count'
  | 'workflow_verified_completion_count'
  | 'overdue_commitment_count'

export interface MonitorLease {
  workerId?: string
  generation: number
  claimedAt?: string
  expiresAt?: string
}

export interface MonitorTarget {
  contractVersion: 1
  id: string
  scope: AmbientMonitorScope
  outcomeId: string
  indicatorId: string
  sourceKind: MonitorSourceKind
  enabled: boolean
  cadenceSeconds: number
  nextRunAt: string
  lease: MonitorLease
  createdAt: string
  updatedAt: string
  authority: AmbientMonitorAuthority
}

export interface ObservationRecord {
  contractVersion: 1
  id: string
  scope: AmbientMonitorScope
  targetId: string
  outcomeId: string
  indicatorId: string
  sourceKind: MonitorSourceKind
  value: number
  observedAt: string
  recordedAt: string
  sourceDigest: string
  recordDigest: string
  authority: AmbientMonitorAuthority
}

export interface MonitorRun {
  contractVersion: 1
  id: string
  scope: AmbientMonitorScope
  targetId: string
  outcomeId: string
  indicatorId: string
  sourceKind: MonitorSourceKind
  leaseGeneration: number
  status: 'completed' | 'failed'
  startedAt: string
  finishedAt: string
  observationId?: string
  observationDigest?: string
  failureCode?: string
  failureSummary?: string
  idempotencyDigest: string
  recordDigest: string
  authority: AmbientMonitorAuthority
}

export type MonitorCompositionStatus = 'pending' | 'succeeded' | 'dead_lettered'

export interface MonitorCompositionRecordCursor {
  recordedAt: string
  idempotencyKey: string
  ordinal: number
  payloadDigest: string
}

export interface MonitorCompositionFeedbackCursor {
  recordedAt: string
  feedbackId: string
  idempotencyKey: string
  payloadDigest: string
  recordDigest: string
}

export interface MonitorCompositionWatermark {
  cursor?: MonitorCompositionRecordCursor
  count: number
  windowDigest: string
}

export interface MonitorCompositionFeedbackWatermark {
  cursor?: MonitorCompositionFeedbackCursor
  count: number
  windowDigest: string
}

export interface MonitorCompositionAttentionSnapshot {
  contractVersion: 1
  ownerIdentity: string
  capturedAt: string
  policy: {
    idempotencyKey: string
    payloadDigest: string
    recordedAt: string
  }
  signals: MonitorCompositionWatermark
  decisions: MonitorCompositionWatermark
  feedback: MonitorCompositionFeedbackWatermark
  inputDigest: string
}

export interface MonitorCompositionSnapshot {
  contractVersion: 1
  status?: 'pinned' | 'legacy_unpinned'
  composerVersion: string
  capturedAt?: string
  outcomeRevision: number
  outcomeAuditDigest?: string
  contextCutoff?: string
  policyIdempotencyKey?: string
  policyDigest?: string
  policyRecordedAt?: string
  signalWatermark?: MonitorCompositionWatermark | string | number
  decisionWatermark?: MonitorCompositionWatermark | string | number
  feedbackWatermark?: MonitorCompositionFeedbackWatermark | string | number
  attention?: MonitorCompositionAttentionSnapshot
  snapshotDigest: string
}

export interface MonitorCompositionDelivery {
  contractVersion: 1
  id: string
  scope: AmbientMonitorScope
  targetId: string
  runId: string
  runDigest: string
  observationId: string
  observationDigest: string
  status: MonitorCompositionStatus
  revision: number
  attemptCount: number
  maxAttempts: number
  nextAttemptAt?: string
  lastAttemptAt?: string
  lastFailureCode?: string
  createdAt: string
  updatedAt: string
  completedAt?: string
  bindingDigest: string
  snapshot?: MonitorCompositionSnapshot
  authority: AmbientMonitorAuthority
}

export interface MonitorCompositionAttempt {
  contractVersion: 1
  id: string
  scope: AmbientMonitorScope
  deliveryId: string
  targetId: string
  runId: string
  runDigest: string
  snapshotDigest: string
  attemptNumber: number
  leaseGeneration: number
  workerId: string
  status: 'succeeded' | 'failed'
  failureCode?: string
  startedAt: string
  finishedAt: string
  requestDigest: string
  recordDigest: string
  authority: AmbientMonitorAuthority
}

export interface MonitorTargetListResponse {
  targets: MonitorTarget[]
  authority: AmbientMonitorAuthority
}

export interface MonitorTargetWriteResponse {
  target: MonitorTarget
  created?: boolean
  updated?: boolean
  authority: AmbientMonitorAuthority
}

export interface ObservationRecordListResponse {
  observations: ObservationRecord[]
  authority: AmbientMonitorAuthority
}

export interface MonitorRunListResponse {
  runs: MonitorRun[]
  authority: AmbientMonitorAuthority
}

export interface MonitorCompositionListResponse {
  compositions: MonitorCompositionDelivery[]
  authority: AmbientMonitorAuthority
}

export interface MonitorCompositionAttemptListResponse {
  attempts: MonitorCompositionAttempt[]
  authority: AmbientMonitorAuthority
}

export interface RegisterMonitorTargetRequest {
  idempotencyKey: string
  targetId: string
  indicatorId: string
  sourceKind: MonitorSourceKind
  enabled: boolean
  cadenceSeconds: number
  firstRunAt: string
}

export interface SetMonitorEnabledRequest {
  idempotencyKey: string
  enabled: boolean
}

export interface RunDueMonitorsRequest {
  workerId: string
  asOf: string
  leaseSeconds: number
  limit: number
}

export interface RecoverAmbientMonitorsResponse {
  recovered: number
  collectionRecovered: number
  compositionRecovered: number
  authority: AmbientMonitorAuthority
}

export interface ProcessDueResult {
  claimed: number
  completions: Array<{
    observation: ObservationRecord
    run: MonitorRun
    composition?: MonitorCompositionDelivery
    created: boolean
    composed: boolean
  }>
  failures: Array<{ targetId: string; code: string }>
  compositions?: {
    claimed: number
    succeeded: number
    failures: Array<{ deliveryId: string; code: string; retrying: boolean }>
    records: MonitorCompositionDelivery[]
    authority: AmbientMonitorAuthority
  }
  authority: AmbientMonitorAuthority
}

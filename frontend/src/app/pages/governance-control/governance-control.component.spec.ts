import { NzModalService } from 'ng-zorro-antd/modal'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { of, Subject, throwError } from 'rxjs'
import {
  AgentRecord,
  ContactReviewDecision,
  ContactReviewDecisionResult,
  DomainPackView,
  ExecutionAuthorizationReceipt,
  LearningApplicationSummary,
  LearningProposal,
  LifeCommitmentRevision,
  LifeCostEntry,
  LifeOntologyEntity,
  LifeOntologyMergeProposal,
  OutcomeEvaluationRecord,
  OutcomeRevision,
  ProactivityDecisionRecord,
  ProactivityFeedbackRecord,
  ResilienceStatus,
  StandingMandate,
} from '../../models/governance-control.model'
import {
  AmbientMonitorAuthority,
  MonitorCompositionDelivery,
  MonitorTarget,
  ProcessDueResult,
} from '../../models/ambient-monitor.model.interface'
import { AmbientMonitorService } from '../../services/ambient-monitor.service'
import { GovernanceControlService } from '../../services/governance-control.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { GovernanceControlComponent } from './governance-control.component'

function proactivityDecisionRecord(): ProactivityDecisionRecord {
  return {
    contractVersion: 1,
    recordedAt: '2026-08-05T10:00:00Z',
    decision: {
      signalId: 'signal-1',
      openLoopKey: 'loop-1',
      signalDigest: 'a'.repeat(64),
      title: 'Review an open loop',
      summary: 'A source-backed open loop needs owner attention.',
      outcome: 'notify',
      score: 82,
      reasons: ['deadline is approaching'],
      recommendedChannels: ['governance_control'],
      budgetCost: 1,
      executionAuthorized: false,
      deliveryAuthorized: false,
      authorityGranted: false,
      decidedAt: '2026-08-05T10:00:00Z',
    },
  }
}

function proactivityFeedbackRecord(action: ProactivityFeedbackRecord['action']): ProactivityFeedbackRecord {
  return {
    contractVersion: 1,
    id: `feedback-${action}`,
    ownerIdentity: 'session-owner',
    signalId: 'signal-1',
    openLoopKey: 'loop-1',
    signalDigest: 'a'.repeat(64),
    sourceOutcome: 'notify',
    sourceDecisionAt: '2026-08-05T10:00:00Z',
    action,
    reason: `Owner selected ${action}.`,
    ...(action === 'snooze' ? { snoozedUntil: '2026-08-06T10:00:00Z' } : {}),
    recordDigest: 'b'.repeat(64),
    recordedAt: '2026-08-05T10:01:00Z',
    authority: 'attention_feedback_only',
    canExecute: false,
    deliveryAuthorized: false,
    executionAuthorized: false,
  }
}

describe('GovernanceControlComponent', () => {
  let component: GovernanceControlComponent
  let service: jasmine.SpyObj<GovernanceControlService>
  let ambientMonitor: jasmine.SpyObj<AmbientMonitorService>
  let notification: jasmine.SpyObj<NzNotificationService>
  let modal: jasmine.SpyObj<NzModalService>
  let preferences: ModuleViewPreferencesService

  const monitorAuthority: AmbientMonitorAuthority = {
    label: 'advisory_monitor_only',
    canExecute: false,
    canDeliver: false,
    canNotify: false,
    canWriteCalendar: false,
    canMutateWorkflow: false,
    canAuthorizeMandate: false,
    canMutateLearning: false,
  }

  const monitorTarget: MonitorTarget = {
    contractVersion: 1,
    id: '11111111-1111-4111-8111-111111111111',
    scope: { ownerId: 'session-owner', workspaceId: '018-HAI' },
    outcomeId: 'verified-work',
    indicatorId: 'verified-count',
    sourceKind: 'workflow_verified_completion_count',
    enabled: true,
    cadenceSeconds: 86400,
    nextRunAt: '2026-08-06T10:00:00Z',
    lease: { generation: 0 },
    createdAt: '2026-08-05T10:00:00Z',
    updatedAt: '2026-08-05T10:00:00Z',
    authority: monitorAuthority,
  }

  const monitorCompositionSnapshot = {
    contractVersion: 1 as const,
    composerVersion: '2.4.0',
    outcomeRevision: 17,
    outcomeAuditDigest: 'd'.repeat(64),
    contextCutoff: '2026-08-05T09:59:59Z',
    policyIdempotencyKey: 'policy-evaluation-2026-08-05',
    policyDigest: 'e'.repeat(64),
    policyRecordedAt: '2026-08-05T09:59:58Z',
    signalWatermark: {
      count: 42,
      windowDigest: 'g'.repeat(64),
      cursor: {
        recordedAt: '2026-08-05T09:59:50Z',
        idempotencyKey: 'signal-batch-42',
        ordinal: 6,
        payloadDigest: 'h'.repeat(64),
      },
    },
    decisionWatermark: {
      count: 108,
      windowDigest: 'i'.repeat(64),
      cursor: {
        recordedAt: '2026-08-05T09:59:52Z',
        idempotencyKey: 'decision-batch-108',
        ordinal: 7,
        payloadDigest: 'j'.repeat(64),
      },
    },
    feedbackWatermark: {
      count: 19,
      windowDigest: 'k'.repeat(64),
      cursor: {
        recordedAt: '2026-08-05T09:59:54Z',
        feedbackId: '55555555-5555-4555-8555-555555555555',
        idempotencyKey: 'feedback-19',
        payloadDigest: 'l'.repeat(64),
        recordDigest: 'm'.repeat(64),
      },
    },
    snapshotDigest: 'f'.repeat(64),
  }

  const monitorComposition: MonitorCompositionDelivery = {
    contractVersion: 1,
    id: '22222222-2222-4222-8222-222222222222',
    scope: monitorTarget.scope,
    targetId: monitorTarget.id,
    runId: '33333333-3333-4333-8333-333333333333',
    runDigest: 'a'.repeat(64),
    observationId: '44444444-4444-4444-8444-444444444444',
    observationDigest: 'b'.repeat(64),
    status: 'pending',
    revision: 1,
    attemptCount: 0,
    maxAttempts: 5,
    nextAttemptAt: '2026-08-05T10:00:02Z',
    createdAt: '2026-08-05T10:00:02Z',
    updatedAt: '2026-08-05T10:00:02Z',
    bindingDigest: 'c'.repeat(64),
    snapshot: monitorCompositionSnapshot,
    authority: monitorAuthority,
  }

  const receipt = {
    id: 'receipt-1',
    action: 'send_email',
    resourceType: 'email',
    outcome: 'requires_approval',
    reason: 'External communication requires approval.',
    risk: 'high',
  } as ExecutionAuthorizationReceipt

  const mandate = {
    id: 'mandate-1',
    name: 'Prepare drafts',
    purpose: 'Allow low-risk drafting.',
    status: 'draft',
    revision: 1,
    autonomyCeiling: 2,
  } as StandingMandate

  const proposal = {
    id: 'proposal-1',
    title: 'Improve evidence ranking',
    hypothesis: 'The new ranker improves source recall.',
    status: 'review_required',
    protectedTarget: false,
    revision: 3,
  } as LearningProposal

  const learningApplication = {
    id: 'application-1',
    proposalId: 'proposal-1',
    proposalRevision: 4,
    mode: 'apply',
    status: 'applied',
    target: 'routing_policy',
    protectedTarget: false,
    applierId: 'policy-promoter',
    currentVersion: '1.0.0',
    proposedVersion: '1.1.0',
    appliedVersion: '1.1.0',
    evidence: [],
    rollbackEvidence: [],
    attempt: 1,
    createdAt: '2026-07-31T20:00:00Z',
    updatedAt: '2026-07-31T20:00:01Z',
  } as LearningApplicationSummary

  const agent = {
    id: 'agent-1',
    name: 'Evidence reviewer',
    state: 'quarantined',
    health: {
      status: 'unhealthy',
      ready: false,
      reason: 'Runtime evidence expired.',
    },
  } as AgentRecord

  const domain = {
    pack: {
      id: 'legal',
      version: '1.0.0',
      name: 'Legal matters',
      description: 'Evidence-first controls for legal work.',
      sensitive: true,
      defaultEnabled: true,
      classificationSignals: [],
      intakeQuestions: [],
      riskTriggers: [],
      approvalRules: [],
      prohibitedAutonomousActions: [],
      evidenceRequirements: [],
      suitableAgentCapabilities: [],
      retention: {
        defaultDays: 365,
        localOnly: true,
        deletionReview: true,
        archiveProvenance: true,
      },
      playbook: { version: '1.0.0', digest: 'playbook-digest', methods: [] },
    },
    enabled: true,
    localOnly: true,
  } as DomainPackView

  beforeEach(() => {
    service = jasmine.createSpyObj<GovernanceControlService>(
      'GovernanceControlService',
      [
        'listExecutionReceipts',
        'listMandates',
        'listLearningProposals',
        'listLearningOutcomes',
        'listAgents',
        'domainCatalog',
        'listMandateDecisions',
        'effectiveDomainPack',
        'classifyDomain',
        'decideLearningProposal',
        'listAgentTeams',
        'agentTeamMessageAttention',
        'listLifeEntities',
        'listLifeRelations',
        'listLifeMergeProposals',
        'listContactReviewDecisions',
        'decideContactCandidate',
        'decideContactMerge',
        'listLifeCommitments',
        'listLifeCosts',
        'lifeCommitment',
        'lifeCommitmentHistory',
        'recordLifeCommitment',
        'recordLifeCost',
        'proactivityPolicy',
        'listProactivitySignals',
        'listProactivityDecisions',
        'listProactivityFeedback',
        'recordProactivityFeedback',
        'outcomeDefinition',
        'storeOutcome',
        'createOutcomeEvaluation',
        'listOutcomeEvaluations',
        'listOutcomeCorrections',
        'resilienceStatus',
      ]
    )
    ambientMonitor = jasmine.createSpyObj<AmbientMonitorService>('AmbientMonitorService', [
      'getMonitor',
      'registerTarget',
      'setEnabled',
      'listObservations',
      'listRuns',
      'listCompositions',
      'listCompositionAttempts',
      'runDue',
      'recover',
    ])
    notification = jasmine.createSpyObj<NzNotificationService>(
      'NzNotificationService',
      ['success', 'info', 'warning', 'error']
    )
    modal = jasmine.createSpyObj<NzModalService>('NzModalService', ['confirm'])
    preferences = new ModuleViewPreferencesService()
    preferences.reset('governance-control')

    service.listExecutionReceipts.and.returnValue(
      of({ receipts: [receipt], count: 1, limit: 50 })
    )
    service.listMandates.and.returnValue(of({ mandates: [mandate] }))
    service.listLearningProposals.and.returnValue(of({ proposals: [proposal] }))
    service.listLearningOutcomes.and.returnValue(of({ outcomes: [] }))
    service.listAgents.and.returnValue(of({ agents: [agent] }))
    service.domainCatalog.and.returnValue(
      of({
        metadata: { version: '2026.07', digest: 'catalog-digest', packCount: 1 },
        packs: [domain],
      })
    )
    service.listMandateDecisions.and.returnValue(of({ decisions: [] }))
    service.effectiveDomainPack.and.returnValue(of(domain))
    service.listAgentTeams.and.returnValue(of({ teams: [] }))
    service.agentTeamMessageAttention.and.returnValue(of({ generatedAt: '2026-08-08T10:00:00Z', messages: [] }))
    service.listLifeEntities.and.returnValue(of({ entities: [] }))
    service.listLifeRelations.and.returnValue(of({ relations: [] }))
    service.listLifeMergeProposals.and.returnValue(of({ proposals: [] }))
    service.listContactReviewDecisions.and.returnValue(of({ decisions: [] }))
    service.listLifeCommitments.and.returnValue(of({ commitments: [] }))
    service.listLifeCosts.and.returnValue(of({ costs: [] }))
    service.lifeCommitment.and.returnValue(of({} as LifeCommitmentRevision))
    service.lifeCommitmentHistory.and.returnValue(of({ revisions: [] }))
    service.proactivityPolicy.and.returnValue(throwError(() => ({ status: 404 })))
    service.listProactivitySignals.and.returnValue(of({ signals: [] }))
    service.listProactivityDecisions.and.returnValue(of({ decisions: [] }))
    service.listProactivityFeedback.and.returnValue(of({ feedback: [] }))
    service.outcomeDefinition.and.returnValue(of({} as OutcomeRevision))
    service.listOutcomeEvaluations.and.returnValue(of({ evaluations: [] }))
    service.listOutcomeCorrections.and.returnValue(of({ corrections: [] }))
    service.resilienceStatus.and.returnValue(of({} as ResilienceStatus))
    ambientMonitor.getMonitor.and.returnValue(of({ targets: [], authority: monitorAuthority }))
    ambientMonitor.listObservations.and.returnValue(of({ observations: [], authority: monitorAuthority }))
    ambientMonitor.listRuns.and.returnValue(of({ runs: [], authority: monitorAuthority }))
    ambientMonitor.listCompositions.and.returnValue(of({ compositions: [], authority: monitorAuthority }))
    ambientMonitor.listCompositionAttempts.and.returnValue(of({ attempts: [], authority: monitorAuthority }))

    component = new GovernanceControlComponent(service, ambientMonitor, notification, modal, preferences)
  })

  afterEach(() => preferences.reset('governance-control'))

  it('loads independent surfaces and reports source-backed counts', () => {
    component.ngOnInit()

    expect(service.listExecutionReceipts).toHaveBeenCalledWith(50, 'summary')
    expect(service.listMandates).toHaveBeenCalled()
    expect(service.listLearningProposals).toHaveBeenCalledWith(100)
    expect(service.listLearningOutcomes).not.toHaveBeenCalled()
    expect(service.listAgents).toHaveBeenCalled()
    expect(service.domainCatalog).toHaveBeenCalled()
    expect(component.surfaceMetric('execution')).toBe('1 need attention')
    expect(component.surfaceMetric('mandates')).toBe('0 active')
    expect(component.surfaceMetric('learning')).toBe('1 awaiting review')
    expect(component.surfaceMetric('agents')).toBe('0 ready')
    expect(component.surfaceMetric('domains')).toBe('1 enabled')
  })

  it('does not load collapsed advisory engines in the default Basic view', () => {
    component.ngOnInit()

    expect(service.listAgentTeams).not.toHaveBeenCalled()
    expect(service.listLifeEntities).not.toHaveBeenCalled()
    expect(service.listLifeRelations).not.toHaveBeenCalled()
    expect(service.listLifeMergeProposals).not.toHaveBeenCalled()
    expect(service.listContactReviewDecisions).not.toHaveBeenCalled()
    expect(service.listLifeCommitments).not.toHaveBeenCalled()
    expect(service.listLifeCosts).not.toHaveBeenCalled()
    expect(service.proactivityPolicy).not.toHaveBeenCalled()
    expect(service.listProactivitySignals).not.toHaveBeenCalled()
    expect(service.listProactivityDecisions).not.toHaveBeenCalled()
    expect(service.listProactivityFeedback).not.toHaveBeenCalled()
  })

  it('restores only advisory engines whose sections were saved open', () => {
    preferences.setSection('governance-control', 'whole-life-context', true)
    preferences.setSection('governance-control', 'proactivity-policy', true)

    component.ngOnInit()

    expect(service.listLifeEntities).toHaveBeenCalled()
    expect(service.listLifeRelations).toHaveBeenCalled()
    expect(service.listLifeMergeProposals).toHaveBeenCalled()
    expect(service.listContactReviewDecisions).toHaveBeenCalled()
    expect(service.proactivityPolicy).toHaveBeenCalled()
    expect(service.listProactivitySignals).toHaveBeenCalled()
    expect(service.listProactivityDecisions).toHaveBeenCalled()
    expect(service.listProactivityFeedback).toHaveBeenCalled()
    expect(service.listAgentTeams).not.toHaveBeenCalled()
    expect(service.listLifeCommitments).not.toHaveBeenCalled()
    expect(service.listLifeCosts).not.toHaveBeenCalled()
  })

  it('loads full controlled-learning outcome history only when its section opens', () => {
    component.ngOnInit()
    expect(service.listLearningOutcomes).not.toHaveBeenCalled()

    component.loadLearningSection(true)

    expect(service.listLearningOutcomes).toHaveBeenCalledOnceWith(100)
    expect(service.listLearningProposals).toHaveBeenCalledTimes(2)
  })

  it('loads full authorization evidence only when its section opens', () => {
    component.ngOnInit()
    expect(service.listExecutionReceipts).toHaveBeenCalledOnceWith(50, 'summary')

    component.loadExecutionSection(true)

    expect(service.listExecutionReceipts).toHaveBeenCalledTimes(2)
    expect(service.listExecutionReceipts).toHaveBeenCalledWith(50, 'full')
  })

  it('builds the Basic decision queue only from unresolved records', () => {
    component.ngOnInit()

    expect(component.attention.map((item) => item.kind)).toEqual([
      'proposal',
      'mandate',
      'receipt',
      'agent',
    ])
    expect(component.attention.every((item) => item.summary.length > 0)).toBeTrue()
  })

  it('does not claim the Basic decision queue is clear while a source is unresolved', () => {
    component.state.execution = { loading: true, loaded: false, error: '' }
    component.state.mandates = { loading: false, loaded: true, error: '' }
    component.state.learning = { loading: false, loaded: true, error: '' }
    component.state.agents = { loading: false, loaded: true, error: '' }

    expect(component.attentionLoading).toBeTrue()
    expect(component.attentionIncomplete).toBeFalse()

    component.state.execution = { loading: false, loaded: false, error: 'Ledger unavailable' }
    expect(component.attentionLoading).toBeFalse()
    expect(component.attentionIncomplete).toBeTrue()
  })

  it('loads owner attention feedback with advisory evidence', () => {
    const feedback = proactivityFeedbackRecord('snooze')
    service.listProactivityFeedback.and.returnValue(of({ feedback: [feedback] }))

    component.loadProactivity(true, true)

    expect(service.listProactivityFeedback).toHaveBeenCalledWith(100)
    expect(component.latestProactivityFeedback('loop-1')).toEqual(feedback)
    expect(component.advisoryState.proactivity.availability).toBe('ready')
  })

  it('records owner feedback without requesting delivery or execution authority', () => {
    const record = proactivityDecisionRecord()
    const feedback = proactivityFeedbackRecord('accept')
    service.recordProactivityFeedback.and.returnValue(of(feedback))

    component.recordProactivityFeedback(record, 'accept')

    const request = service.recordProactivityFeedback.calls.mostRecent().args[0]
    expect(request.signalId).toBe(record.decision.signalId)
    expect(request.openLoopKey).toBe(record.decision.openLoopKey)
    expect(request.signalDigest).toBe(record.decision.signalDigest)
    expect(request.action).toBe('accept')
    expect(Object.prototype.hasOwnProperty.call(request, 'ownerIdentity')).toBeFalse()
    expect(Object.prototype.hasOwnProperty.call(request, 'canExecute')).toBeFalse()
    expect(notification.success).toHaveBeenCalled()
  })

  it('requires deliberate confirmation before suppressing an open loop', () => {
    const record = proactivityDecisionRecord()
    service.recordProactivityFeedback.and.returnValue(of(proactivityFeedbackRecord('suppress')))

    component.recordProactivityFeedback(record, 'suppress')

    expect(service.recordProactivityFeedback).not.toHaveBeenCalled()
    const options = modal.confirm.calls.mostRecent().args[0]!
    expect(options.nzOkDanger).toBeTrue()
    ;(options.nzOnOk as () => void)()
    expect(service.recordProactivityFeedback).toHaveBeenCalled()
  })

  it('bounds a snooze request to a future timestamp', () => {
    const record = proactivityDecisionRecord()
    service.recordProactivityFeedback.and.returnValue(of(proactivityFeedbackRecord('snooze')))
    const before = Date.now()

    component.recordProactivityFeedback(record, 'snooze')

    const request = service.recordProactivityFeedback.calls.mostRecent().args[0]
    expect(new Date(request.snoozedUntil || '').getTime()).toBeGreaterThan(before)
  })

  it('restores only this module\'s persisted mandate-history disclosure', () => {
    preferences.setSection('governance-control', 'standing-mandates', true)
    preferences.setSection('memory', 'standing-mandates', false)

    component.ngOnInit()

    expect(service.listMandateDecisions).toHaveBeenCalledWith(undefined, 100)
    expect(preferences.get('memory').openSections['standing-mandates']).toBeFalse()
  })

  it('preserves an archived domain preference instead of silently reactivating it', () => {
    const archived = {
      ...domain,
      preference: {
        ownerIdentity: 'owner',
        packId: 'legal',
        catalogVersion: '2026.07',
        revision: 4,
        status: 'archived' as const,
        enabled: false,
        classificationBoost: 0,
        forceLocalOnly: true,
        adaptation: {},
        createdAt: '2026-07-01T00:00:00Z',
        updatedAt: '2026-07-02T00:00:00Z',
      },
      enabled: false,
    }
    service.effectiveDomainPack.and.returnValue(of(archived))

    component.openDomain(archived)

    expect(component.domainPreferenceForm.status).toBe('archived')
    expect(component.domainPreferenceForm.enabled).toBeFalse()
  })

  it('creates an immutable outcome definition through the authenticated scoped API', () => {
    const storedDefinition = {
      outcome: {
        id: 'verified-work',
        scope: { ownerId: 'owner', workspaceId: '018-HAI' },
        statement: 'Increase verified operational completions.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [{
          id: 'verified-count',
          name: 'Verified completions',
          unit: 'tasks',
          direction: 'higher' as const,
          targetValue: 10,
          targetTolerance: 0,
          trendThresholdPerDay: 0,
          regressionThreshold: 1,
          minimumObservations: 2,
          baseline: {
            id: 'verified-count-baseline',
            value: 0,
            observedAt: '2026-01-01T00:00:00Z',
            verification: 'user_confirmed' as const,
            sources: [],
          },
        }],
      },
      revision: 1,
      recordedAt: '2026-08-03T12:00:00Z',
      auditDigest: 'definition-digest',
      lifeGraphProjection: {
        primary: { id: 'outcome-node' },
        linkedEntities: [{ id: 'workspace-node' }],
        relations: [{ id: 'workspace-relation' }],
        alreadyExisted: false,
        advisoryOnly: true,
        canExecute: false,
        grantsAuthority: false,
      },
    } as unknown as OutcomeRevision
    service.storeOutcome.and.returnValue(of(storedDefinition))
    service.outcomeDefinition.and.returnValue(of(storedDefinition))
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.outcomeForm = {
      statement: 'Increase verified operational completions.',
      lifeDomain: 'work_venture',
      windowStart: '2026-01-01',
      windowEnd: '2026-12-31',
      indicatorId: 'verified-count',
      indicatorName: 'Verified completions',
      unit: 'tasks',
      direction: 'higher',
      targetValue: 10,
      targetTolerance: 0,
      trendThresholdPerDay: 0,
      regressionThreshold: 1,
      minimumObservations: 2,
      baselineValue: 0,
      baselineObservedAt: '2026-01-01',
    }

    component.defineOutcome()

    expect(service.storeOutcome).toHaveBeenCalled()
    const request = service.storeOutcome.calls.mostRecent().args[2]
    expect(request.expectedRevision).toBe(0)
    expect(request.outcome.lifeDomain).toBe('work_venture')
    expect((request.outcome as unknown as { id?: string }).id).toBeUndefined()
    expect(component.outcomeDefinition?.revision).toBe(1)
    expect(component.latestOutcomeLifeGraphProjection?.primary.id).toBe('outcome-node')
    expect(component.outcomeLifeGraphProjectionWarning).toBe('')
    expect(notification.success).toHaveBeenCalled()
  })

  it('stages user-confirmed observations and sends one bounded advisory evaluation', () => {
    const definition = {
      outcome: {
        id: 'verified-work',
        scope: { ownerId: 'owner', workspaceId: '018-HAI' },
        statement: 'Increase verified operational completions.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [{
          id: 'verified-count', name: 'Verified completions', unit: 'tasks', direction: 'higher',
          targetValue: 10, targetTolerance: 0, trendThresholdPerDay: 0,
          regressionThreshold: 1, minimumObservations: 2,
          baseline: { id: 'baseline', value: 0, observedAt: '2026-01-01T00:00:00Z', verification: 'user_confirmed' },
        }],
      },
      revision: 1,
      recordedAt: '2026-08-03T12:00:00Z',
      auditDigest: 'definition-digest',
    } as OutcomeRevision
    const evaluation = {
      evaluation: {
        id: 'evaluation-1', outcomeId: 'verified-work', asOf: '2026-08-03T12:00:00Z',
        state: 'insufficient_evidence', reviewRequired: true, reviewReasons: ['More evidence required.'],
        recommendations: [], auditDigest: 'evaluation-digest',
      },
      outcomeRevision: 1,
      recordedAt: '2026-08-03T12:00:00Z',
      recordDigest: 'record-digest',
      lifeGraphProjectionWarning: 'graph temporarily unavailable',
    } as OutcomeEvaluationRecord
    component.outcomeDefinition = definition
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.outcomeObservationForm = {
      indicatorId: 'verified-count', value: 1,
      observedAt: '2026-08-01T10:00', rationale: 'Observed in the verified release report.',
    }
    service.createOutcomeEvaluation.and.returnValue(of(evaluation))
    service.outcomeDefinition.and.returnValue(of(definition))

    component.addOutcomeObservation()
    component.evaluateOutcome()

    expect(service.createOutcomeEvaluation).toHaveBeenCalled()
    const request = service.createOutcomeEvaluation.calls.mostRecent().args[2]
    expect(request.observations.length).toBe(1)
    expect(request.observations[0].verification).toBe('user_confirmed')
    expect(request.observations[0].attribution.method).toBe('user_report')
    expect(component.outcomeObservationDrafts).toEqual([])
    expect(component.latestOutcomeLifeGraphProjection).toBeUndefined()
    expect(component.outcomeLifeGraphProjectionWarning).toContain('temporarily unavailable')
  })

  it('offers only backend-valid lifecycle transitions', () => {
    expect(component.allowedAgentTransitions(agent)).toEqual(['disabled'])
    expect(component.canEnableAgent(agent)).toBeFalse()
  })

  it('does not describe an approved proposal as applied', () => {
    const approved = { ...proposal, status: 'approved' as const }

    expect(component.learningApplicationStatus(approved)).toContain(
      'no application record was returned'
    )
    expect(component.isProposalPending(approved)).toBeFalse()
  })

  it('reports durable application status separately from proposal approval', () => {
    const approved = { ...proposal, status: 'approved' as const }

    expect(component.learningApplicationStatus(approved, learningApplication)).toContain(
      'Applied at 1.1.0'
    )
    expect(component.learningApplicationStatus(approved, {
      ...learningApplication,
      mode: 'protected_handoff',
      status: 'handoff_ready',
      protectedTarget: true,
    })).toContain('has not been applied')
  })

  it('classifies through the backend and opens the matching pack', () => {
    component.ngOnInit()
    component.classifierText = 'Review a contract deadline.'
    service.classifyDomain.and.returnValue(
      of({
        matches: [{
          packId: 'legal',
          score: 82,
          explicit: false,
          sensitive: true,
          reasons: ['Contract signal'],
          signals: [],
        }],
        suppressed: [],
      })
    )
    spyOn(component, 'openDomain')

    component.classify()
    component.openMatchedDomain('legal')

    expect(service.classifyDomain).toHaveBeenCalledWith(
      'Review a contract deadline.'
    )
    expect(component.classification?.matches[0].packId).toBe('legal')
    expect(component.openDomain).toHaveBeenCalledWith(domain)
  })

  it('records human confirmation and the expected learning revision', () => {
    component.selectedProposal = proposal
    component.learningRationale = 'Evidence is sufficient and rollback is clear.'
    service.decideLearningProposal.and.returnValue(
      of({
        proposal: { ...proposal, status: 'approved', revision: 4 },
        application: learningApplication,
      })
    )
    spyOn(component, 'loadLearning')
    spyOn(component, 'openProposal')

    component.decideProposal('approve')

    expect(service.decideLearningProposal).toHaveBeenCalledWith(
      'proposal-1',
      jasmine.objectContaining({
        expectedRevision: 3,
        kind: 'approve',
        humanConfirmed: true,
        rationale: 'Evidence is sufficient and rollback is clear.',
      })
    )
    expect(component.selectedLearningApplication).toBe(learningApplication)
    expect(component.openProposal).toHaveBeenCalledWith(
      jasmine.objectContaining({ status: 'approved' }),
      learningApplication
    )
  })

  it('loads opened source-backed advisory engines while leaving scoped engines unconfigured', () => {
    preferences.setSection('governance-control', 'agent-teams', true)
    preferences.setSection('governance-control', 'whole-life-context', true)
    preferences.setSection('governance-control', 'life-ledger-overview', true)
    preferences.setSection('governance-control', 'proactivity-policy', true)

    component.ngOnInit()

    expect(service.listAgentTeams).toHaveBeenCalled()
    expect(service.listLifeEntities).toHaveBeenCalledWith(50, false)
    expect(service.listLifeRelations).toHaveBeenCalledWith(50, false)
    expect(service.listLifeMergeProposals).toHaveBeenCalledWith(50)
    expect(service.listContactReviewDecisions).toHaveBeenCalledWith(100)
    expect(service.listLifeCommitments).toHaveBeenCalledWith(50)
    expect(service.listLifeCosts).toHaveBeenCalledWith(50)
    expect(service.proactivityPolicy).toHaveBeenCalled()
    expect(service.listProactivitySignals).toHaveBeenCalledWith(50)
    expect(service.listProactivityDecisions).toHaveBeenCalledWith(50)
    expect(service.outcomeDefinition).not.toHaveBeenCalled()
    expect(service.resilienceStatus).not.toHaveBeenCalled()
    expect(component.advisoryState.teams.availability).toBe('empty')
    expect(component.advisoryState.life.availability).toBe('empty')
    expect(component.advisoryState.ledger.availability).toBe('empty')
    expect(component.advisoryState.proactivity.availability).toBe('empty')
    expect(component.advisoryState.outcomes.availability).toBe('not_configured')
    expect(component.advisoryState.resilience.availability).toBe('not_configured')
  })

  it('shows only unresolved source-derived contact candidates and merge proposals', () => {
    const pending = {
      id: 'life-entity-pending', type: 'person', domain: 'relationships_care',
      name: 'Pending person', status: 'open', confidence: 0.4,
      verificationStatus: 'needs_review', attributes: { candidate: 'true' },
      priority: 0, observedAt: '2026-08-04T08:00:00Z', provenance: [],
      sensitivity: 'sensitive', localOnly: true, entityDigest: 'pending-digest',
      createdAt: '2026-08-04T08:00:00Z',
    } as LifeOntologyEntity
    const resolved = { ...pending, id: 'life-entity-resolved', name: 'Resolved person' }
    const pendingProposal = {
      id: 'life-merge-pending', candidateEntityIds: [pending.id, 'life-entity-other'],
      reasons: ['Matching source identity'], status: 'pending',
    } as LifeOntologyMergeProposal
    const resolvedProposal = {
      ...pendingProposal, id: 'life-merge-resolved', candidateEntityIds: [resolved.id, 'other'],
    }
    service.listLifeEntities.and.callFake((_limit, allowLocalOnly, filters) =>
      of({ entities: allowLocalOnly && filters?.types?.includes('person') ? [pending, resolved] : [] })
    )
    service.listLifeMergeProposals.and.returnValue(
      of({ proposals: [pendingProposal, resolvedProposal] })
    )
    const decisionBase = {
      contractVersion: 'life-contact-review.v1', ownerIdentity: 'owner',
      idempotencyKey: 'contact-review-test', reason: 'Owner reviewed source evidence.',
      decidedAt: '2026-08-04T08:00:00Z', recordedAt: '2026-08-04T08:00:01Z',
      requestDigest: 'request-digest', recordDigest: 'record-digest', localOnly: true,
      canExecute: false, grantsAuthority: false,
    }
    service.listContactReviewDecisions.and.returnValue(of({ decisions: [
      {
        ...decisionBase,
        id: 'decision-candidate', subject: 'candidate', subjectId: resolved.id,
        candidateEntityIds: [resolved.id], action: 'promote',
      },
      {
        ...decisionBase,
        id: 'decision-merge', subject: 'merge_proposal', subjectId: resolvedProposal.id,
        candidateEntityIds: resolvedProposal.candidateEntityIds, action: 'keep_distinct',
      },
    ] as ContactReviewDecision[] }))

    component.loadLifeContext(true, true)

    expect(service.listLifeEntities).toHaveBeenCalledWith(100, true, {
      types: ['person'], verification: ['needs_review'],
    })
    expect(component.pendingContactCandidates.map((entity) => entity.id)).toEqual([pending.id])
    expect(component.pendingLifeMergeProposals.map((proposal) => proposal.id)).toEqual([
      pendingProposal.id,
    ])
  })

  it('records one local-only candidate decision and refreshes whole-life context', () => {
    const candidate = {
      id: 'life-entity-candidate', type: 'person', domain: 'relationships_care',
      name: 'Robert candidate', summary: 'Source-derived person', status: 'open',
      confidence: 0.35, verificationStatus: 'needs_review',
      attributes: { candidate: 'true' }, priority: 0,
      observedAt: '2026-08-04T08:00:00Z', provenance: [], sensitivity: 'sensitive',
      localOnly: true, entityDigest: 'candidate-digest', createdAt: '2026-08-04T08:00:00Z',
    } as LifeOntologyEntity
    const result = {
      decision: {
        id: 'life-contact-review-one', action: 'promote', subject: 'candidate',
        subjectId: candidate.id, candidateEntityIds: [candidate.id],
        reason: 'The source record identifies this person.', localOnly: true,
        canExecute: false, grantsAuthority: false,
      },
      canonicalEntity: { ...candidate, id: 'life-entity-canonical', name: 'Robert candidate' },
      alreadyExisted: false,
    } as ContactReviewDecisionResult
    service.decideContactCandidate.and.returnValue(of(result))
    spyOn(component, 'loadLifeContext')
    component.openContactCandidate(candidate)
    component.contactReviewForm.reason = 'The source record identifies this person.'

    component.decideContactCandidate('promote')

    expect(service.decideContactCandidate).toHaveBeenCalledWith(
      candidate.id,
      jasmine.objectContaining({
        action: 'promote',
        reason: 'The source record identifies this person.',
      })
    )
    expect(component.selectedContactReviewDecision).toBe(result.decision)
    expect(component.selectedCanonicalContact).toBe(result.canonicalEntity)
    expect(component.loadLifeContext).toHaveBeenCalledWith(true, true)
    expect(notification.success).toHaveBeenCalledWith(
      'Contact review recorded', jasmine.stringMatching(/grants no execution authority/i)
    )
  })

  it('requires a meaningful reason and canonical name before merge', () => {
    const merge = {
      id: 'life-merge-one', candidateEntityIds: ['one', 'two'], reasons: [], status: 'pending',
      match: 'semantic_identity', confidence: 0.8, proposalDigest: 'proposal-digest',
      createdAt: '2026-08-04T08:00:00Z', advisoryOnly: true, canExecute: false,
      grantsAuthority: false,
    } as LifeOntologyMergeProposal
    component.openContactMergeProposal(merge)
    component.contactReviewForm.reason = 'Too short'

    component.decideContactMerge('merge')

    expect(service.decideContactMerge).not.toHaveBeenCalled()
    expect(notification.warning).toHaveBeenCalledWith(
      'Meaningful reason required', jasmine.any(String)
    )

    component.contactReviewForm.reason = 'Both records refer to the same person.'
    component.contactReviewForm.canonicalName = ''
    component.decideContactMerge('merge')

    expect(service.decideContactMerge).not.toHaveBeenCalled()
    expect(notification.warning).toHaveBeenCalledWith(
      'Canonical name required', jasmine.any(String)
    )
  })

  it('prevents duplicate contact-review submission while the first request is pending', () => {
    const candidate = {
      id: 'life-entity-candidate', type: 'person', domain: 'relationships_care',
      name: 'Candidate', status: 'open', confidence: 0.3,
      verificationStatus: 'needs_review', attributes: { candidate: 'true' },
      priority: 0, observedAt: '2026-08-04T08:00:00Z', provenance: [],
      sensitivity: 'sensitive', localOnly: true, entityDigest: 'candidate-digest',
      createdAt: '2026-08-04T08:00:00Z',
    } as LifeOntologyEntity
    const pending = new Subject<ContactReviewDecisionResult>()
    service.decideContactCandidate.and.returnValue(pending)
    component.openContactCandidate(candidate)
    component.contactReviewForm.reason = 'This evidence identifies the same person.'

    component.decideContactCandidate('promote')
    component.decideContactCandidate('promote')

    expect(service.decideContactCandidate).toHaveBeenCalledTimes(1)
  })

  it('keeps estimates, incurred costs, and payments distinct without combining currencies', () => {
    const commitment = {
      id: 'commitment-1',
      commitmentKey: 'housing-repair',
      revision: 2,
      title: 'Complete housing repair',
      status: 'active',
      evidence: [],
    } as unknown as LifeCommitmentRevision
    const costs = [
      { id: 'estimate-eur', kind: 'estimate', amountMinor: 12000, currency: 'EUR' },
      { id: 'estimate-usd', kind: 'estimate', amountMinor: 5000, currency: 'USD' },
      { id: 'incurred-eur', kind: 'incurred', amountMinor: 8000, currency: 'EUR' },
      { id: 'paid-eur', kind: 'paid', amountMinor: 3000, currency: 'EUR' },
    ] as LifeCostEntry[]
    service.listLifeCommitments.and.returnValue(of({ commitments: [commitment] }))
    service.listLifeCosts.and.returnValue(of({ costs }))

    component.loadLifeLedger(true, true)

    expect(component.advisoryState.ledger.availability).toBe('ready')
    expect(component.openLifeCommitments).toEqual([commitment])
    expect(component.lifeCostsByKind('estimate').map((cost) => cost.id)).toEqual([
      'estimate-eur', 'estimate-usd',
    ])
    expect(component.lifeCostsByKind('incurred').map((cost) => cost.id)).toEqual(['incurred-eur'])
    expect(component.lifeCostsByKind('paid').map((cost) => cost.id)).toEqual(['paid-eur'])
    expect(component.lifeCostTotals('estimate')).toEqual([
      { currency: 'EUR', amountMinor: 12000 },
      { currency: 'USD', amountMinor: 5000 },
    ])
  })

  it('appends a source-backed commitment revision and reloads immutable history', () => {
    const stored = {
      id: 'commitment-1', commitmentKey: 'legal-answer', revision: 1,
      domain: 'legal_government', title: 'Answer legal request', status: 'proposed',
      verification: 'source_supported', evidence: [], observedAt: '2026-08-03T12:00:00Z',
    } as unknown as LifeCommitmentRevision
    service.recordLifeCommitment.and.returnValue(of({ record: stored, created: true }))
    service.lifeCommitment.and.returnValue(of(stored))
    service.lifeCommitmentHistory.and.returnValue(of({ revisions: [stored] }))
    component.commitmentForm = {
      key: 'legal-answer', expectedRevision: 0, domain: 'legal_government',
      title: 'Answer legal request', summary: 'Reply using the supplied letter.',
      status: 'proposed', counterparty: 'Lawyer', projectKey: 'legal-case', dueAt: '',
      verification: 'source_supported', observedAt: '2026-08-03T12:00',
      sourceId: 'letter-1', sourceUri: 'local://legal/letter-1.pdf',
      contentDigest: 'a'.repeat(64), authority: 'Original letter',
    }

    component.recordLifeCommitment()

    expect(service.recordLifeCommitment).toHaveBeenCalledWith(
      'legal-answer',
      jasmine.objectContaining({
        expectedRevision: 0,
        verification: 'source_supported',
        evidence: [jasmine.objectContaining({
          id: 'letter-1', contentDigest: 'a'.repeat(64), localOnly: true,
        })],
      })
    )
    expect(service.lifeCommitmentHistory).toHaveBeenCalledWith('legal-answer', 100)
    expect(component.selectedLifeCommitmentHistory).toEqual([stored])
    expect(notification.success).toHaveBeenCalled()
  })

  it('rejects weak paid assertions before calling the API', () => {
    component.costForm = {
      domain: 'financial', title: 'Invoice payment', summary: '', kind: 'paid',
      amount: 12.34, currency: 'EUR', commitmentKey: '', projectKey: '',
      verification: 'source_supported', observedAt: '2026-08-03T12:00',
      sourceId: 'receipt-1', sourceUri: 'local://receipts/one.pdf',
      contentDigest: 'b'.repeat(64), authority: 'Receipt',
    }

    component.recordLifeCost()

    expect(service.recordLifeCost).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Stronger verification required',
      jasmine.stringMatching(/stronger evidence/i)
    )
  })

  it('records human-confirmed payment evidence in minor currency units without executing payment', () => {
    const stored = {
      id: 'cost-1', domain: 'financial', title: 'Invoice payment', kind: 'paid',
      amountMinor: 1234, currency: 'EUR', verification: 'human_confirmed', evidence: [],
      observedAt: '2026-08-03T12:00:00Z',
    } as unknown as LifeCostEntry
    service.recordLifeCost.and.returnValue(of({ record: stored, created: true }))
    component.costForm = {
      domain: 'financial', title: 'Invoice payment', summary: '', kind: 'paid',
      amount: 12.34, currency: 'eur', commitmentKey: 'invoice-1', projectKey: 'admin',
      verification: 'human_confirmed', observedAt: '2026-08-03T12:00',
      sourceId: 'receipt-1', sourceUri: 'local://receipts/one.pdf',
      contentDigest: 'c'.repeat(64), authority: 'Bank receipt',
    }

    component.recordLifeCost()

    expect(service.recordLifeCost).toHaveBeenCalledWith(jasmine.objectContaining({
      kind: 'paid', amountMinor: 1234, currency: 'EUR', verification: 'human_confirmed',
    }))
    expect(component.selectedLifeCost).toBe(stored)
    expect(notification.success).toHaveBeenCalledWith(
      'Cost evidence recorded', jasmine.stringMatching(/no money was moved/i)
    )
  })

  it('requires an explicit local-device choice before loading local-only life graph records', () => {
    component.ngOnInit()
    service.listLifeEntities.calls.reset()
    service.listLifeRelations.calls.reset()

    component.toggleLocalOnlyLifeContext()

    expect(component.includeLocalOnlyLifeContext).toBeTrue()
    expect(service.listLifeEntities).toHaveBeenCalledWith(50, true)
    expect(service.listLifeRelations).toHaveBeenCalledWith(50, true)
  })

  it('loads ambient monitor state and selected target history with outcome evidence', () => {
    const definition = {
      outcome: {
        id: 'verified-work',
        scope: { ownerId: 'owner', workspaceId: '018-HAI' },
        statement: 'Increase verified operational completions.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [{
          id: 'verified-count', name: 'Verified completions', unit: 'tasks', direction: 'higher',
          targetValue: 10, targetTolerance: 0, trendThresholdPerDay: 0,
          regressionThreshold: 1, minimumObservations: 2,
          baseline: { id: 'baseline', value: 0, observedAt: '2026-01-01T00:00:00Z', verification: 'user_confirmed' },
        }],
      },
      revision: 1,
      recordedAt: '2026-08-03T12:00:00Z',
      auditDigest: 'definition-digest',
    } as OutcomeRevision
    service.outcomeDefinition.and.returnValue(of(definition))
    ambientMonitor.getMonitor.and.returnValue(of({ targets: [monitorTarget], authority: monitorAuthority }))
    ambientMonitor.listObservations.and.returnValue(of({
      observations: [{
        contractVersion: 1,
        id: 'observation-1',
        scope: monitorTarget.scope,
        targetId: monitorTarget.id,
        outcomeId: monitorTarget.outcomeId,
        indicatorId: monitorTarget.indicatorId,
        sourceKind: monitorTarget.sourceKind,
        value: 4,
        observedAt: '2026-08-05T10:00:00Z',
        recordedAt: '2026-08-05T10:00:01Z',
        sourceDigest: 'a'.repeat(64),
        recordDigest: 'b'.repeat(64),
        authority: monitorAuthority,
      }],
      authority: monitorAuthority,
    }))
    ambientMonitor.listRuns.and.returnValue(of({
      runs: [{
        contractVersion: 1,
        id: 'run-1',
        scope: monitorTarget.scope,
        targetId: monitorTarget.id,
        outcomeId: monitorTarget.outcomeId,
        indicatorId: monitorTarget.indicatorId,
        sourceKind: monitorTarget.sourceKind,
        leaseGeneration: 1,
        status: 'completed',
        startedAt: '2026-08-05T10:00:00Z',
        finishedAt: '2026-08-05T10:00:02Z',
        observationId: 'observation-1',
        observationDigest: 'b'.repeat(64),
        idempotencyDigest: 'c'.repeat(64),
        recordDigest: 'd'.repeat(64),
        authority: monitorAuthority,
      }],
      authority: monitorAuthority,
    }))
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }

    component.loadOutcomeEvidence(true, true)

    expect(ambientMonitor.getMonitor).toHaveBeenCalledWith('018-HAI', 'verified-work')
    expect(ambientMonitor.listObservations).toHaveBeenCalledWith('018-HAI', 'verified-work', monitorTarget.id, 50)
    expect(ambientMonitor.listRuns).toHaveBeenCalledWith('018-HAI', 'verified-work', monitorTarget.id, 50)
    expect(ambientMonitor.listCompositions).toHaveBeenCalledWith('018-HAI', 'verified-work', monitorTarget.id, 25)
    expect(component.selectedMonitorTarget?.id).toBe(monitorTarget.id)
    expect(component.latestMonitorObservation?.value).toBe(4)
    expect(component.latestMonitorRun?.status).toBe('completed')
  })

  it('keeps collection completion separate from a retrying advisory handoff', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.listCompositions.and.returnValue(of({
      compositions: [{
        ...monitorComposition,
        revision: 3,
        attemptCount: 1,
        lastAttemptAt: '2026-08-05T10:01:00Z',
        lastFailureCode: 'provider_unavailable',
        nextAttemptAt: '2026-08-05T10:02:00Z',
        updatedAt: '2026-08-05T10:01:00Z',
      }],
      authority: monitorAuthority,
    }))

    component.loadMonitorCompositions(true)

    expect(component.monitorCompositionLabel).toBe('Retry scheduled')
    expect(component.monitorCompositionDetail).toContain('provider_unavailable')
    expect(component.monitorCompositionTone).toBe('stale')
    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
  })

  it('shows only composer version and exact outcome revision in Basic handoff provenance', () => {
    component.monitorCompositions = [monitorComposition]

    expect(component.monitorCompositionProvenanceLabel).toBe('Composer 2.4.0 / outcome revision 17')
    expect(component.latestMonitorCompositionSnapshot).toBe(monitorCompositionSnapshot)
    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
  })

  it('provides complete snapshot provenance for the existing Advanced inspector', () => {
    component.monitorCompositions = [monitorComposition]

    const details = component.monitorCompositionProvenanceDetails
    expect(details.map((detail) => detail.field)).toEqual([
      'contractVersion',
      'snapshotStatus',
      'composerVersion',
      'snapshotCapturedAt',
      'outcomeRevision',
      'outcomeAuditDigest',
      'contextCutoff',
      'policyIdempotencyKey',
      'policyDigest',
      'policyRecordedAt',
      'signalWatermarkCount',
      'signalWatermarkWindowDigest',
      'signalWatermarkRecordedAt',
      'signalWatermarkIdempotencyKey',
      'signalWatermarkPayloadDigest',
      'signalWatermarkOrdinal',
      'decisionWatermarkCount',
      'decisionWatermarkWindowDigest',
      'decisionWatermarkRecordedAt',
      'decisionWatermarkIdempotencyKey',
      'decisionWatermarkPayloadDigest',
      'decisionWatermarkOrdinal',
      'feedbackWatermarkCount',
      'feedbackWatermarkWindowDigest',
      'feedbackWatermarkRecordedAt',
      'feedbackWatermarkIdempotencyKey',
      'feedbackWatermarkPayloadDigest',
      'feedbackWatermarkFeedbackId',
      'feedbackWatermarkRecordDigest',
      'attentionInputDigest',
      'snapshotDigest',
    ])
    expect(details.find((detail) => detail.field === 'outcomeAuditDigest')?.value).toBe('d'.repeat(12))
    expect(details.find((detail) => detail.field === 'policyDigest')?.value).toBe('e'.repeat(12))
    expect(details.find((detail) => detail.field === 'snapshotDigest')?.value).toBe('f'.repeat(12))
    expect(details.find((detail) => detail.field === 'policyIdempotencyKey')?.value).toBe('policy-evaluation-2026-08-05')
    expect(details.find((detail) => detail.field === 'contextCutoff')?.value).toBe('2026-08-05T09:59:59Z')
    expect(details.find((detail) => detail.field === 'signalWatermarkCount')?.value).toBe('42')
    expect(details.find((detail) => detail.field === 'signalWatermarkWindowDigest')?.value).toBe('g'.repeat(12))
    expect(details.find((detail) => detail.field === 'decisionWatermarkOrdinal')?.value).toBe('7')
    expect(details.find((detail) => detail.field === 'feedbackWatermarkFeedbackId')?.value)
      .toBe('55555555-5555-4555-8555-555555555555')
    expect(details.find((detail) => detail.field === 'feedbackWatermarkRecordDigest')?.value).toBe('m'.repeat(12))
    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
  })

  it('normalizes the live nested attention snapshot without another request', () => {
    component.monitorCompositions = [{
      ...monitorComposition,
      snapshot: {
        contractVersion: 1,
        status: 'pinned',
        composerVersion: 'ambient-outcome-attention-v2',
        capturedAt: '2026-08-05T09:59:59Z',
        outcomeRevision: 23,
        outcomeAuditDigest: 'n'.repeat(64),
        attention: {
          contractVersion: 1,
          ownerIdentity: 'session-owner',
          capturedAt: '2026-08-05T09:59:57Z',
          policy: {
            idempotencyKey: 'live-policy-key',
            payloadDigest: 'o'.repeat(64),
            recordedAt: '2026-08-05T09:59:56Z',
          },
          signals: monitorCompositionSnapshot.signalWatermark,
          decisions: monitorCompositionSnapshot.decisionWatermark,
          feedback: monitorCompositionSnapshot.feedbackWatermark,
          inputDigest: 'p'.repeat(64),
        },
        snapshotDigest: 'q'.repeat(64),
      },
    }]

    const details = component.monitorCompositionProvenanceDetails
    expect(component.monitorCompositionProvenanceLabel)
      .toBe('Composer ambient-outcome-attention-v2 / outcome revision 23')
    expect(details.find((detail) => detail.field === 'contextCutoff')?.value).toBe('2026-08-05T09:59:57Z')
    expect(details.find((detail) => detail.field === 'policyIdempotencyKey')?.value).toBe('live-policy-key')
    expect(details.find((detail) => detail.field === 'policyDigest')?.value).toBe('o'.repeat(12))
    expect(details.find((detail) => detail.field === 'attentionInputDigest')?.value).toBe('p'.repeat(12))
    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
  })

  it('does not infer provenance for a delivery without a snapshot', () => {
    component.monitorCompositions = [{ ...monitorComposition, snapshot: undefined }]

    expect(component.monitorCompositionProvenanceLabel).toBe('Snapshot provenance not recorded')
    expect(component.monitorCompositionProvenanceDetails).toEqual([])
    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
  })

  it('labels a legacy snapshot as unpinned instead of claiming an exact revision', () => {
    component.monitorCompositions = [{
      ...monitorComposition,
      snapshot: { ...monitorCompositionSnapshot, status: 'legacy_unpinned', outcomeRevision: 0 },
    }]

    expect(component.monitorCompositionProvenanceLabel).toBe('Composer 2.4.0 / outcome revision not pinned')
    expect(component.monitorCompositionProvenanceDetails
      .find((detail) => detail.field === 'outcomeRevision')?.value).toBe('Not pinned')
  })

  it('marks optional policy provenance as not recorded without hiding its fields', () => {
    component.monitorCompositions = [{
      ...monitorComposition,
      snapshot: {
        ...monitorCompositionSnapshot,
        policyIdempotencyKey: undefined,
        policyDigest: undefined,
        policyRecordedAt: undefined,
      },
    }]

    const policyDetails = component.monitorCompositionProvenanceDetails
      .filter((detail) => detail.field.startsWith('policy'))
    expect(policyDetails.map((detail) => detail.value)).toEqual([
      'Not recorded',
      'Not recorded',
      'Not recorded',
    ])
  })

  it('refreshes immutable attempts when an already-open handoff inspector receives a new delivery state', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    preferences.setSection('governance-control', 'outcome-monitor-composition', true)
    ambientMonitor.listCompositions.and.returnValue(of({
      compositions: [{
        ...monitorComposition,
        status: 'succeeded',
        revision: 2,
        attemptCount: 1,
        lastAttemptAt: '2026-08-05T10:01:00Z',
        completedAt: '2026-08-05T10:01:00Z',
        updatedAt: '2026-08-05T10:01:00Z',
      }],
      authority: monitorAuthority,
    }))
    ambientMonitor.listCompositionAttempts.and.returnValue(of({
      attempts: [{
        contractVersion: 1,
        id: '55555555-5555-4555-8555-555555555555',
        scope: monitorTarget.scope,
        deliveryId: monitorComposition.id,
        targetId: monitorTarget.id,
        runId: monitorComposition.runId,
        runDigest: monitorComposition.runDigest,
        snapshotDigest: monitorCompositionSnapshot.snapshotDigest,
        attemptNumber: 1,
        leaseGeneration: 1,
        workerId: 'composition-worker',
        status: 'succeeded',
        startedAt: '2026-08-05T10:00:30Z',
        finishedAt: '2026-08-05T10:01:00Z',
        requestDigest: 'd'.repeat(64),
        recordDigest: 'e'.repeat(64),
        authority: monitorAuthority,
      }],
      authority: monitorAuthority,
    }))

    component.loadMonitorCompositions(true)

    expect(ambientMonitor.listCompositionAttempts).toHaveBeenCalledWith(
      '018-HAI', 'verified-work', monitorTarget.id, monitorComposition.id, 25
    )
    expect(component.monitorCompositionAttempts[0].status).toBe('succeeded')
    expect(component.monitorCompositionState.historyLoaded).toBeTrue()
  })

  it('loads immutable handoff attempts only when Advanced evidence is requested', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    component.monitorCompositions = [{ ...monitorComposition, status: 'dead_lettered', attemptCount: 1, completedAt: '2026-08-05T10:01:00Z' }]
    ambientMonitor.listCompositionAttempts.and.returnValue(of({
      attempts: [{
        contractVersion: 1,
        id: '55555555-5555-4555-8555-555555555555',
        scope: monitorTarget.scope,
        deliveryId: monitorComposition.id,
        targetId: monitorTarget.id,
        runId: monitorComposition.runId,
        runDigest: monitorComposition.runDigest,
        snapshotDigest: monitorCompositionSnapshot.snapshotDigest,
        attemptNumber: 1,
        leaseGeneration: 1,
        workerId: 'redacted-in-basic-view',
        status: 'failed',
        failureCode: 'invalid_composition',
        startedAt: '2026-08-05T10:00:30Z',
        finishedAt: '2026-08-05T10:01:00Z',
        requestDigest: 'd'.repeat(64),
        recordDigest: 'e'.repeat(64),
        authority: monitorAuthority,
      }],
      authority: monitorAuthority,
    }))

    expect(ambientMonitor.listCompositionAttempts).not.toHaveBeenCalled()
    component.loadMonitorCompositionAttempts()

    expect(ambientMonitor.listCompositionAttempts).toHaveBeenCalledWith(
      '018-HAI', 'verified-work', monitorTarget.id, monitorComposition.id, 25
    )
    expect(component.monitorCompositionAttempts[0].failureCode).toBe('invalid_composition')
    expect(component.monitorCompositionLabel).toBe('Needs review')
  })

  it('does not infer a healthy handoff when its durable status request fails', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.listCompositions.and.returnValue(throwError(() => ({
      status: 503,
      error: { error: 'composition ledger unavailable' },
    })))

    component.loadMonitorCompositions(true)

    expect(component.monitorCompositionLabel).toBe('Status unavailable')
    expect(component.monitorCompositionTone).toBe('stale')
    expect(component.monitorCompositionState.error).toBe('composition ledger unavailable')
  })

  it('registers an immutable monitor target from the loaded outcome indicator', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.outcomeDefinition = {
      outcome: {
        id: 'verified-work',
        scope: { ownerId: 'owner', workspaceId: '018-HAI' },
        statement: 'Increase verified operational completions.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T23:59:59Z' },
        indicators: [{
          id: 'verified-count', name: 'Verified completions', unit: 'tasks', direction: 'higher',
          targetValue: 10, targetTolerance: 0, trendThresholdPerDay: 0,
          regressionThreshold: 1, minimumObservations: 2,
          baseline: { id: 'baseline', value: 0, observedAt: '2026-01-01T00:00:00Z', verification: 'user_confirmed' },
        }],
      },
      revision: 1,
      recordedAt: '2026-08-03T12:00:00Z',
      auditDigest: 'definition-digest',
    } as OutcomeRevision
    component.monitorForm = {
      targetId: monitorTarget.id,
      sourceKind: 'workflow_verified_completion_count',
      cadenceSeconds: 86400,
      firstRunAt: '2026-08-05T10:00',
      enabled: true,
    }
    ambientMonitor.registerTarget.and.returnValue(of({
      target: monitorTarget,
      created: true,
      authority: monitorAuthority,
    }))

    component.configureAmbientMonitor()

    expect(ambientMonitor.registerTarget).toHaveBeenCalledWith(
      '018-HAI',
      'verified-work',
      jasmine.objectContaining({
        targetId: monitorTarget.id,
        indicatorId: 'verified-count',
        sourceKind: 'workflow_verified_completion_count',
        cadenceSeconds: 86400,
      })
    )
    expect(notification.success).toHaveBeenCalledWith(
      'Monitor target created',
      jasmine.stringMatching(/cannot execute or deliver/i)
    )
  })

  it('pauses only the selected monitor target through the guarded state endpoint', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.setEnabled.and.returnValue(of({
      target: { ...monitorTarget, enabled: false },
      updated: true,
      authority: monitorAuthority,
    }))

    component.toggleAmbientMonitor()

    expect(ambientMonitor.setEnabled).toHaveBeenCalledWith(
      '018-HAI',
      'verified-work',
      monitorTarget.id,
      jasmine.objectContaining({ enabled: false })
    )
    expect(component.selectedMonitorTarget?.enabled).toBeFalse()
  })

  it('prevents duplicate due-monitor submissions while the advisory pass is running', () => {
    const response$ = new Subject<ProcessDueResult>()
    spyOn(component, 'loadOutcomeEvidence')
    spyOn(component, 'loadProactivity')
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.runDue.and.returnValue(response$)

    component.runDueAmbientMonitors()
    component.runDueAmbientMonitors()

    expect(ambientMonitor.runDue).toHaveBeenCalledTimes(1)
    response$.next({ claimed: 0, completions: [], failures: [], authority: monitorAuthority })
    response$.complete()
    expect(component.monitorMutating).toBeFalse()
    expect(notification.success).toHaveBeenCalledWith(
      'Due monitor pass completed',
      jasmine.stringMatching(/no work was executed or delivered/i)
    )
    expect(component.loadOutcomeEvidence).toHaveBeenCalledWith(true, true)
    expect(component.loadProactivity).toHaveBeenCalledWith(true, true)
  })

  it('shows a failed due-monitor pass as an error with its bounded failure code', () => {
    spyOn(component, 'loadOutcomeEvidence')
    spyOn(component, 'loadProactivity')
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.runDue.and.returnValue(of({
      claimed: 1,
      completions: [],
      failures: [{ targetId: monitorTarget.id, code: 'monitor_failed' }],
      authority: monitorAuthority,
    }))

    component.runDueAmbientMonitors()

    expect(notification.success).not.toHaveBeenCalledWith(
      'Due monitor pass completed',
      jasmine.any(String)
    )
    expect(notification.error).toHaveBeenCalledWith(
      'Due monitor pass failed',
      jasmine.stringMatching(/monitor_failed/i)
    )
  })

  it('recovers both expired monitor lease classes and refreshes their state', () => {
    spyOn(component, 'loadAmbientMonitor')
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    component.selectedMonitorTargetId = monitorTarget.id
    ambientMonitor.recover.and.returnValue(of({
      recovered: 2,
      collectionRecovered: 1,
      compositionRecovered: 1,
      authority: monitorAuthority,
    }))

    component.recoverAmbientMonitors()

    expect(ambientMonitor.recover).toHaveBeenCalledWith('018-HAI', jasmine.any(String))
    expect(notification.success).toHaveBeenCalledWith(
      'Expired monitor work recovered',
      jasmine.stringMatching(/1 collection lease and 1 advisory handoff lease/i)
    )
    expect(component.loadAmbientMonitor).toHaveBeenCalledWith(true)
    expect(component.monitorMutating).toBeFalse()
  })

  it('does not claim recovery when no expired monitor leases exist', () => {
    spyOn(component, 'loadAmbientMonitor')
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }
    component.monitorTargets = [monitorTarget]
    ambientMonitor.recover.and.returnValue(of({
      recovered: 0,
      collectionRecovered: 0,
      compositionRecovered: 0,
      authority: monitorAuthority,
    }))

    component.recoverAmbientMonitors()

    expect(notification.info).toHaveBeenCalledWith(
      'No expired monitor work',
      jasmine.stringMatching(/no expired collection or advisory handoff leases/i)
    )
    expect(notification.success).not.toHaveBeenCalledWith(
      'Expired monitor work recovered',
      jasmine.any(String)
    )
  })

  it('surfaces monitor API failure without hiding loaded outcome evidence', () => {
    service.outcomeDefinition.and.returnValue(of({
      outcome: {
        id: 'verified-work',
        scope: { ownerId: 'owner', workspaceId: '018-HAI' },
        statement: 'Increase verified work.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [],
      },
      revision: 1,
      recordedAt: '2026-08-01T00:00:00Z',
      auditDigest: 'digest',
    }))
    ambientMonitor.getMonitor.and.returnValue(throwError(() => ({
      status: 503,
      error: { error: 'monitor repository unavailable' },
    })))
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'verified-work' }

    component.loadOutcomeEvidence(true, true)

    expect(component.outcomeDefinition).toBeDefined()
    expect(component.advisoryState.outcomes.availability).toBe('ready')
    expect(component.monitorState.error).toBe('monitor repository unavailable')
  })

  it('loads outcome and resilience evidence only after a real scope is supplied', () => {
    component.advisoryScope = { workspaceId: 'personal-work', outcomeId: 'health-1' }
    service.outcomeDefinition.and.returnValue(of({
      outcome: {
        id: 'health-1',
        scope: { ownerId: 'owner', workspaceId: 'personal-work' },
        statement: 'Improve health stability',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [],
      },
      revision: 1,
      recordedAt: '2026-08-01T00:00:00Z',
      auditDigest: 'digest',
    }))
    service.resilienceStatus.and.returnValue(of({
      contractVersion: 1,
      scope: { ownerId: 'owner', workspaceId: 'personal-work' },
      generatedAt: '2026-08-01T00:00:00Z',
      leaseCount: 0,
      workerCount: 0,
      retryCount: 0,
      circuitCount: 0,
      recoveryCount: 0,
      authority: {
        mode: 'advisory_only',
        canExecute: false,
        grantsAuthority: false,
        consumesApproval: false,
        dispatchesWork: false,
      },
    }))

    component.loadOutcomeEvidence(true, true)
    component.loadResilienceStatus(true, true)

    expect(service.outcomeDefinition).toHaveBeenCalledWith('personal-work', 'health-1')
    expect(service.listOutcomeEvaluations).toHaveBeenCalledWith('personal-work', 'health-1')
    expect(service.listOutcomeCorrections).toHaveBeenCalledWith('personal-work', 'health-1')
    expect(service.resilienceStatus).toHaveBeenCalledWith('personal-work')
    expect(component.advisoryState.outcomes.availability).toBe('ready')
    expect(component.advisoryState.resilience.availability).toBe('ready')
  })

  it('treats a fully missing outcome scope as an empty authoring state', () => {
    component.advisoryScope = { workspaceId: '018-HAI', outcomeId: 'new-outcome' }
    service.outcomeDefinition.and.returnValue(throwError(() => ({ status: 404 })))
    service.listOutcomeEvaluations.and.returnValue(throwError(() => ({ status: 404 })))
    service.listOutcomeCorrections.and.returnValue(throwError(() => ({ status: 404 })))

    component.loadOutcomeEvidence(true, true)

    expect(component.outcomeDefinition).toBeUndefined()
    expect(component.outcomeEvaluations).toEqual([])
    expect(component.outcomeCorrections).toEqual([])
    expect(component.advisoryState.outcomes.availability).toBe('empty')
    expect(component.advisoryState.outcomes.error).toBe('')
  })

  it('marks retained advisory data stale when a refresh fails', () => {
    service.listAgentTeams.and.returnValue(of({ teams: [{
      id: 'team-1', key: 'review', version: '1.0.0', revision: 1, status: 'active',
      name: 'Review team', purpose: 'Review evidence', authorityCeiling: 0,
      riskCeiling: 'low', advisoryOnly: true, grantsExecutionAuthority: false,
      executionAuthorizationRequired: true, members: [], evidenceRefs: [],
      contractDigest: 'digest', createdAt: '2026-08-01T00:00:00Z',
      updatedAt: '2026-08-01T00:00:00Z',
    }] }))
    component.loadAgentTeams(true, true)
    service.listAgentTeams.and.returnValue(throwError(() => ({
      status: 503,
      error: { error: 'agent team service unavailable' },
    })))

    component.loadAgentTeams(true, true)

    expect(component.agentTeams.length).toBe(1)
    expect(component.advisoryState.teams.availability).toBe('stale')
    expect(component.advisorySummary('teams')).toContain('Stale')
  })

  it('projects persisted agent-message attention into the team summary', () => {
    const team = {
      id: 'team-1', key: 'review', version: '1.0.0', revision: 1, status: 'active' as const,
      name: 'Review team', purpose: 'Review evidence', authorityCeiling: 0,
      riskCeiling: 'low' as const, advisoryOnly: true, grantsExecutionAuthority: false,
      executionAuthorizationRequired: true, members: [], evidenceRefs: [],
      contractDigest: 'digest', createdAt: '2026-08-01T00:00:00Z',
      updatedAt: '2026-08-01T00:00:00Z',
    }
    service.listAgentTeams.and.returnValue(of({ teams: [team] }))
    service.agentTeamMessageAttention.and.returnValue(of({
      generatedAt: '2026-08-08T10:00:00Z',
      messages: [{
        messageId: 'message-1', correlationId: 'correlation-1', recipientId: 'reviewer',
        subject: 'Review evidence', requiresAcknowledgment: true, state: 'overdue',
        reason: 'acknowledgment remained overdue after reminders',
        dueAt: '2026-08-08T09:00:00Z', expiresAt: '2026-08-08T11:00:00Z',
        humanReviewRequired: true, advisoryOnly: true, grantsExecutionAuthority: false,
        executionAuthorizationRequired: true,
      }],
    }))

    component.loadAgentTeams(true, true)

    expect(service.agentTeamMessageAttention).toHaveBeenCalledWith('team-1', '1.0.0')
    expect(component.agentTeamReviewCount).toBe(1)
    expect(component.agentTeamReviewItems(team).map((item) => item.state)).toEqual(['overdue'])
  })
})

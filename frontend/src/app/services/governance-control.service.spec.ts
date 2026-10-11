import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { GovernanceControlService } from './governance-control.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('GovernanceControlService', () => {
  let service: GovernanceControlService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(GovernanceControlService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('loads execution receipts and single-use consumption evidence', () => {
    service.listExecutionReceipts(25).subscribe()
    const listRequest = http.expectOne(
      (request) =>
        request.url === '/api/v1/execution-authorizations' &&
        request.params.get('limit') === '25'
    )
    expect(listRequest.request.method).toBe('GET')
    listRequest.flush({ receipts: [], count: 0, limit: 25 })

    service.executionConsumption('receipt/one').subscribe()
    const consumptionRequest = http.expectOne(
      '/api/v1/execution-authorizations/receipt%2Fone/consumption'
    )
    expect(consumptionRequest.request.method).toBe('GET')
    consumptionRequest.flush({})
  })

  it('sends optimistic mandate lifecycle revisions', () => {
    service.activateMandate('mandate one', 4).subscribe()
    const activateRequest = http.expectOne(
      '/api/v1/standing-mandates/mandate%20one/activate'
    )
    expect(activateRequest.request.body).toEqual({ expectedRevision: 4 })
    activateRequest.flush({})

    service.revokeMandate('mandate one', 5, 'Scope no longer needed').subscribe()
    const revokeRequest = http.expectOne(
      '/api/v1/standing-mandates/mandate%20one/revoke'
    )
    expect(revokeRequest.request.body).toEqual({
      expectedRevision: 5,
      reason: 'Scope no longer needed',
    })
    revokeRequest.flush({})
  })

  it('maps mandate decision inspection and policy evaluation to the registered API', () => {
    service.listMandateDecisions('8d69cb0f-a76e-4d65-9c69-708214a15af2', 20).subscribe()
    const history = http.expectOne((request) =>
      request.url === '/api/v1/standing-mandates/decisions' &&
      request.params.get('mandateId') === '8d69cb0f-a76e-4d65-9c69-708214a15af2' &&
      request.params.get('limit') === '20'
    )
    expect(history.request.method).toBe('GET')
    history.flush({ decisions: [] })

    service.authorizeMandate('mandate-1', {
      action: 'draft',
      resourceType: 'document',
      risk: 'low',
      requestedAutonomy: 2,
      upstreamApprovalRequired: true,
      requestedAt: '2026-07-31T20:00:00Z',
    }).subscribe()
    const decision = http.expectOne('/api/v1/standing-mandates/mandate-1/authorize')
    expect(decision.request.method).toBe('POST')
    expect(decision.request.body.upstreamApprovalRequired).toBeTrue()
    decision.flush({})
  })

  it('records a human-confirmed controlled-learning decision', () => {
    let application: Record<string, unknown> | undefined
    service.decideLearningProposal('proposal-1', {
      expectedRevision: 3,
      kind: 'approve',
      humanConfirmed: true,
      rationale: 'Evidence and rollback plan are sufficient.',
      governanceReference: 'gov-18',
    }).subscribe((result) => {
      application = result.application as unknown as Record<string, unknown>
    })

    const request = http.expectOne(
      '/api/v1/controlled-learning/proposals/proposal-1/decisions'
    )
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({
      expectedRevision: 3,
      kind: 'approve',
      humanConfirmed: true,
      rationale: 'Evidence and rollback plan are sufficient.',
      governanceReference: 'gov-18',
    })
    request.flush({
      proposal: {
        id: 'proposal-1',
        revision: 4,
        target: 'routing_policy',
        protectedTarget: false,
        currentVersion: '1.0.0',
        proposedVersion: '1.1.0',
      },
      application: {
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
        rollbackToken: 'must-not-reach-the-UI',
      },
    })
    expect(application?.['status']).toBe('applied')
    expect(Object.prototype.hasOwnProperty.call(application, 'rollbackToken')).toBeFalse()
  })

  it('lists controlled-learning evidence separately from change proposals', () => {
    service.listLearningOutcomes(25).subscribe()

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/controlled-learning/outcomes' &&
      candidate.params.get('limit') === '25'
    )
    expect(request.request.method).toBe('GET')
    request.flush({ outcomes: [] })
  })

  it('creates assignments through the durable registry', () => {
    service.assignAgent({
      taskId: 'task-1',
      capabilities: [{ id: 'evidence-review', minVersion: '1.0.0' }],
      compatibility: {},
      requiredAuthority: 1,
      requiredAutonomy: 1,
      policyMaxAuthority: 2,
      policyMaxAutonomy: 2,
      requireLocal: true,
      allowDegraded: false,
    }).subscribe()

    const request = http.expectOne('/api/v1/agents/assignments')
    expect(request.request.method).toBe('POST')
    expect(request.request.body.taskId).toBe('task-1')
    expect(request.request.body.agentId).toBeUndefined()
    request.flush({})
  })

  it('uses only registered agent lifecycle endpoints', () => {
    service.transitionAgent('agent/one', 7, 'quarantined', 'Health evidence expired').subscribe()
    const transition = http.expectOne('/api/v1/agents/agent%2Fone/transitions')
    expect(transition.request.method).toBe('POST')
    expect(transition.request.body).toEqual({
      expectedRevision: 7,
      to: 'quarantined',
      reason: 'Health evidence expired',
    })
    transition.flush({})

    service.agentTransitions('agent/one').subscribe()
    const history = http.expectOne('/api/v1/agents/agent%2Fone/transitions')
    expect(history.request.method).toBe('GET')
    history.flush({ transitions: [] })
  })

  it('uses server classification and owner-scoped domain preferences', () => {
    service.classifyDomain('Review a legal deadline', ['legal']).subscribe()
    const classifyRequest = http.expectOne('/api/v1/domain-packs/classify')
    expect(classifyRequest.request.body).toEqual({
      text: 'Review a legal deadline',
      explicitPackIds: ['legal'],
    })
    classifyRequest.flush({ matches: [], suppressed: [] })

    service.updateDomainPreference('legal/case', {
      expectedRevision: 2,
      status: 'active',
      enabled: true,
      classificationBoost: 4,
      forceLocalOnly: true,
      adaptation: { notes: 'Keep evidence local.' },
    }).subscribe()
    const preferenceRequest = http.expectOne(
      '/api/v1/domain-packs/legal%2Fcase/preference'
    )
    expect(preferenceRequest.request.method).toBe('PUT')
    expect(preferenceRequest.request.body.forceLocalOnly).toBeTrue()
    preferenceRequest.flush({})
  })

  it('normalizes optional domain-pack collections at the API boundary', () => {
    let methods: unknown[] | undefined
    service.domainCatalog().subscribe((catalog) => {
      methods = catalog.packs[0].pack.playbook.methods
    })

    const request = http.expectOne('/api/v1/domain-packs')
    request.flush({
      metadata: { version: '1.1.0', digest: 'catalog-digest', packCount: 1 },
      packs: [{
        enabled: true,
        localOnly: false,
        pack: {
          id: 'legal',
          version: '1.0.0',
          name: 'Legal',
          description: 'Legal work',
          sensitive: true,
          defaultEnabled: true,
          retention: {
            defaultDays: 365,
            localOnly: true,
            deletionReview: true,
            archiveProvenance: true,
          },
        },
      }],
    })

    expect(methods).toEqual([])
  })

  it('loads advisory teams and whole-life context from registered read APIs', () => {
    service.agentTeamMessageAttention('team 1', '1.0.0').subscribe()
    const attention = http.expectOne('/api/v1/framework-registry/teams/team%201/versions/1.0.0/message-attention')
    expect(attention.request.method).toBe('GET')
    attention.flush({ generatedAt: '2026-08-08T10:00:00Z', messages: [] })

    service.agentTeamMessageAttentionIndex().subscribe()
    const attentionIndex = http.expectOne('/api/v1/framework-registry/teams/message-attention')
    expect(attentionIndex.request.method).toBe('GET')
    attentionIndex.flush({ generatedAt: '2026-08-08T10:00:00Z', contracts: [], teams: [] })

    service.listLifeEntities(25, false).subscribe()
    const entities = http.expectOne((request) =>
      request.url === '/api/v1/life-ontology/entities' &&
      request.params.get('limit') === '25' &&
      request.params.get('allowLocalOnly') === 'false'
    )
    expect(entities.request.method).toBe('GET')
    entities.flush({ entities: [] })

    service.listLifeRelations(20, false).subscribe()
    const relations = http.expectOne((request) =>
      request.url === '/api/v1/life-ontology/relations' &&
      request.params.get('limit') === '20'
    )
    expect(relations.request.method).toBe('GET')
    relations.flush({ relations: [] })

    service.listLifeMergeProposals(10).subscribe()
    const proposals = http.expectOne((request) =>
      request.url === '/api/v1/life-ontology/merge-proposals' &&
      request.params.get('limit') === '10'
    )
    expect(proposals.request.method).toBe('GET')
    proposals.flush({ proposals: [] })
  })

  it('reads and records owner attention feedback through the durable advisory API', () => {
    service.listProactivityFeedback(75).subscribe()
    const list = http.expectOne((request) =>
      request.url === '/api/v1/proactivity/feedback' &&
      request.params.get('limit') === '75'
    )
    expect(list.request.method).toBe('GET')
    list.flush({ feedback: [] })

    const payload = {
      idempotencyKey: 'feedback-accept-1',
      signalId: 'signal-1',
      openLoopKey: 'loop-1',
      signalDigest: 'a'.repeat(64),
      action: 'accept' as const,
      reason: 'Owner accepted the recommendation.',
    }
    service.recordProactivityFeedback(payload).subscribe()
    const record = http.expectOne('/api/v1/proactivity/feedback')
    expect(record.request.method).toBe('POST')
    expect(record.request.body).toEqual(payload)
    expect(record.request.body.ownerIdentity).toBeUndefined()
    expect(record.request.body.canExecute).toBeUndefined()
    record.flush({
      ...payload,
      id: 'feedback-1',
      ownerIdentity: 'session-owner',
      sourceOutcome: 'notify',
      sourceDecisionAt: '2026-08-05T10:00:00Z',
      recordDigest: 'b'.repeat(64),
      recordedAt: '2026-08-05T10:01:00Z',
      authority: 'attention_feedback_only',
      canExecute: false,
      deliveryAuthorized: false,
      executionAuthorized: false,
    })
  })

  it('loads filtered contact candidates and immutable contact-review decisions', () => {
    service.listLifeEntities(100, true, {
      types: ['person'],
      verification: ['needs_review'],
    }).subscribe()
    const candidates = http.expectOne((request) =>
      request.url === '/api/v1/life-ontology/entities' &&
      request.params.get('limit') === '100' &&
      request.params.get('allowLocalOnly') === 'true' &&
      request.params.get('types') === 'person' &&
      request.params.get('verification') === 'needs_review'
    )
    expect(candidates.request.method).toBe('GET')
    candidates.flush({ entities: [] })

    let decisions: unknown[] | undefined
    service.listContactReviewDecisions(75).subscribe((response) => {
      decisions = response.decisions
    })
    const history = http.expectOne((request) =>
      request.url === '/api/v1/life-ontology/contact-review-decisions' &&
      request.params.get('limit') === '75'
    )
    expect(history.request.method).toBe('GET')
    history.flush({})
    expect(decisions).toEqual([])
  })

  it('posts governed contact candidate and merge decisions to encoded subjects', () => {
    const request = {
      action: 'correct' as const,
      canonicalName: 'Robert Velhorst',
      canonicalSummary: 'Confirmed local contact.',
      reason: 'Robert confirmed the identity from source evidence.',
      idempotencyKey: 'contact-review-12345678',
    }

    service.decideContactCandidate('candidate/one', request).subscribe()
    const candidate = http.expectOne(
      '/api/v1/life-ontology/contact-candidates/candidate%2Fone/decisions'
    )
    expect(candidate.request.method).toBe('POST')
    expect(candidate.request.body).toEqual(request)
    candidate.flush({ decision: {}, alreadyExisted: false })

    service.decideContactMerge('proposal/one', { ...request, action: 'merge' }).subscribe()
    const merge = http.expectOne(
      '/api/v1/life-ontology/merge-proposals/proposal%2Fone/decisions'
    )
    expect(merge.request.method).toBe('POST')
    expect(merge.request.body.action).toBe('merge')
    merge.flush({ decision: {}, alreadyExisted: false })
  })

  it('loads owner-scoped commitment and cost ledgers without inventing missing records', () => {
    let commitments: unknown[] | undefined
    service.listLifeCommitments(25).subscribe((response) => {
      commitments = response.commitments
    })
    const commitmentRequest = http.expectOne((request) =>
      request.url === '/api/v1/life-ledger/commitments' &&
      request.params.get('limit') === '25'
    )
    expect(commitmentRequest.request.method).toBe('GET')
    commitmentRequest.flush({})
    expect(commitments).toEqual([])

    let costs: unknown[] | undefined
    service.listLifeCosts(40).subscribe((response) => {
      costs = response.costs
    })
    const costRequest = http.expectOne((request) =>
      request.url === '/api/v1/life-ledger/costs' &&
      request.params.get('limit') === '40'
    )
    expect(costRequest.request.method).toBe('GET')
    costRequest.flush({ costs: [] })
    expect(costs).toEqual([])
  })

  it('loads encoded commitment history without inventing missing revisions', () => {
    service.lifeCommitment('legal/case answer').subscribe()
    const currentRequest = http.expectOne(
      '/api/v1/life-ledger/commitments/legal%2Fcase%20answer'
    )
    expect(currentRequest.request.method).toBe('GET')
    currentRequest.flush({ commitmentKey: 'legal/case answer', revision: 2 })

    let revisions: unknown[] | undefined
    service.lifeCommitmentHistory('legal/case answer', 15).subscribe((response) => {
      revisions = response.revisions
    })

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/life-ledger/commitments/legal%2Fcase%20answer/history' &&
      candidate.params.get('limit') === '15'
    )
    expect(request.request.method).toBe('GET')
    request.flush({})
    expect(revisions).toEqual([])
  })

  it('records an authenticated-owner commitment revision using the backend contract', () => {
    const body = {
      expectedRevision: 2,
      domain: 'legal_government' as const,
      title: 'Provide the evidence bundle',
      summary: 'Source-backed commitment revision.',
      status: 'active' as const,
      counterparty: 'Counsel',
      projectKey: 'vivare',
      dueAt: '2026-08-10T09:00:00Z',
      verification: 'human_confirmed' as const,
      evidence: [{
        id: 'source-1',
        uri: 'local://evidence/email-1',
        contentDigest: 'sha256:evidence',
        authority: 'direct_email',
        observedAt: '2026-08-03T10:00:00Z',
        verification: 'source_supported' as const,
        localOnly: true,
      }],
      idempotencyKey: 'commitment-revision-3',
      observedAt: '2026-08-03T10:05:00Z',
    }
    let created: boolean | undefined
    service.recordLifeCommitment('legal/case answer', body).subscribe((result) => {
      created = result.created
    })

    const request = http.expectOne(
      '/api/v1/life-ledger/commitments/legal%2Fcase%20answer/revisions'
    )
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual(body)
    expect(request.request.body.ownerIdentity).toBeUndefined()
    request.flush({ record: { commitmentKey: 'legal/case answer', revision: 3 }, created: true })
    expect(created).toBeTrue()
  })

  it('records an immutable cost event using the backend contract', () => {
    const body = {
      domain: 'financial' as const,
      title: 'Evidence bundle postage',
      summary: 'Recorded cost; this endpoint does not move money.',
      kind: 'incurred' as const,
      amountMinor: 1295,
      currency: 'EUR',
      commitmentKey: 'legal/case answer',
      projectKey: 'vivare',
      verification: 'human_confirmed' as const,
      evidence: [{
        id: 'receipt-1',
        uri: 'local://evidence/receipt-1',
        contentDigest: 'sha256:receipt',
        observedAt: '2026-08-03T11:00:00Z',
        verification: 'verified' as const,
        localOnly: true,
      }],
      idempotencyKey: 'cost-postage-1',
      observedAt: '2026-08-03T11:00:00Z',
    }
    let kind: string | undefined
    service.recordLifeCost(body).subscribe((result) => {
      kind = result.record.kind
    })

    const request = http.expectOne('/api/v1/life-ledger/costs')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual(body)
    expect(request.request.body.ownerIdentity).toBeUndefined()
    request.flush({ record: { kind: 'incurred' }, created: true })
    expect(kind).toBe('incurred')
  })

  it('loads proactivity policy, signals, and decisions without mutation calls', () => {
    service.proactivityPolicy().subscribe()
    const policy = http.expectOne('/api/v1/proactivity/policy')
    expect(policy.request.method).toBe('GET')
    policy.flush({})

    service.listProactivitySignals(30).subscribe()
    const signals = http.expectOne((request) =>
      request.url === '/api/v1/proactivity/signals' && request.params.get('limit') === '30'
    )
    expect(signals.request.method).toBe('GET')
    signals.flush({ signals: [] })

    service.listProactivityDecisions(40).subscribe()
    const decisions = http.expectOne((request) =>
      request.url === '/api/v1/proactivity/decisions' && request.params.get('limit') === '40'
    )
    expect(decisions.request.method).toBe('GET')
    decisions.flush({ decisions: [] })
  })

  it('encodes scoped outcome and resilience identifiers', () => {
    service.outcomeDefinition('life/work', 'health goal').subscribe()
    const definition = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal'
    )
    expect(definition.request.method).toBe('GET')
    definition.flush({})

    service.storeOutcome('life/work', 'health goal', {
      idempotencyKey: 'definition-1',
      expectedRevision: 0,
      outcome: {
        statement: 'Improve a verified health outcome.',
        window: { start: '2026-01-01T00:00:00Z', end: '2026-12-31T00:00:00Z' },
        indicators: [],
      },
    }).subscribe()
    const storedDefinition = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal'
    )
    expect(storedDefinition.request.method).toBe('PUT')
    expect(storedDefinition.request.body.expectedRevision).toBe(0)
    storedDefinition.flush({})

    service.listOutcomeEvaluations('life/work', 'health goal').subscribe()
    const evaluations = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal/evaluations'
    )
    expect(evaluations.request.method).toBe('GET')
    evaluations.flush({ evaluations: [] })

    service.createOutcomeEvaluation('life/work', 'health goal', {
      idempotencyKey: 'evaluation-1',
      outcomeRevision: 1,
      observations: [],
      asOf: '2026-08-03T12:00:00Z',
    }).subscribe()
    const createdEvaluation = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal/evaluations'
    )
    expect(createdEvaluation.request.method).toBe('POST')
    expect(createdEvaluation.request.body.outcomeRevision).toBe(1)
    createdEvaluation.flush({})

    service.listOutcomeCorrections('life/work', 'health goal').subscribe()
    const corrections = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal/corrections'
    )
    expect(corrections.request.method).toBe('GET')
    corrections.flush({ corrections: [] })

    service.resilienceStatus('life/work').subscribe()
    const resilience = http.expectOne('/api/v1/resilience/workspaces/life%2Fwork/status')
    expect(resilience.request.method).toBe('GET')
    resilience.flush({})
  })
})

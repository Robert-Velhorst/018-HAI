import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { map, Observable } from 'rxjs'
import {
  AgentAssignment,
  AgentAssignmentRequest,
  AgentList,
  AgentLifecycleState,
  AgentRecord,
  AgentTeamList,
  AgentTeamMessageAttentionPage,
  AgentTransitionList,
  CreateOutcomeEvaluationRequest,
  CreateStandingMandateRequest,
  ContactReviewDecisionList,
  ContactReviewDecisionRequest,
  ContactReviewDecisionResult,
  DecideLearningProposalRequest,
  DomainClassificationResult,
  DomainPackCatalog,
  DomainPackSummaryView,
  DomainPackView,
  ExecutionAuthorizationConsumption,
  ExecutionAuthorizationList,
  ExecutionAuthorizationReceipt,
  LearningDecisionList,
  LearningDecisionResult,
  LearningApplicationSummary,
  LearningOutcomeList,
  LearningProposal,
  LearningProposalList,
  LifeCommitmentHistory,
  LifeCommitmentList,
  LifeCommitmentRevision,
  LifeCommitmentWriteResult,
  LifeCostList,
  LifeCostWriteResult,
  LifeOntologyEntityList,
  LifeOntologyEntityFilters,
  LifeOntologyMergeProposalList,
  LifeOntologyRelationList,
  MandateAuthorizationDecision,
  MandateAuthorizationRequest,
  OutcomeCorrectionList,
  OutcomeEvaluationRecord,
  OutcomeEvaluationList,
  OutcomeRevision,
  ProactivityDecisionList,
  ProactivityFeedbackList,
  ProactivityFeedbackRecord,
  RecordProactivityFeedbackRequest,
  ProactivityPolicyRecord,
  ProactivitySignalList,
  RecordLifeCommitmentRevisionRequest,
  RecordLifeCostEventRequest,
  RegisterAgentRequest,
  ResilienceStatus,
  StoreOutcomeRequest,
  StandingMandate,
  StandingMandateDecisionList,
  StandingMandateList,
  UpdateDomainPreferenceRequest,
} from '../models/governance-control.model'

@Injectable({ providedIn: 'root' })
export class GovernanceControlService {
  private readonly executionUrl = '/api/v1/execution-authorizations'
  private readonly mandateUrl = '/api/v1/standing-mandates'
  private readonly learningUrl = '/api/v1/controlled-learning'
  private readonly agentUrl = '/api/v1/agents'
  private readonly domainUrl = '/api/v1/domain-packs'
  private readonly teamUrl = '/api/v1/framework-registry/teams'
  private readonly lifeOntologyUrl = '/api/v1/life-ontology'
  private readonly lifeLedgerUrl = '/api/v1/life-ledger'
  private readonly proactivityUrl = '/api/v1/proactivity'
  private readonly outcomeEvaluationUrl = '/api/v1/outcome-evaluations/workspaces'
  private readonly resilienceUrl = '/api/v1/resilience/workspaces'

  constructor(private http: HttpClient) {}

  listExecutionReceipts(
    limit = 50,
    view: 'summary' | 'full' = 'summary'
  ): Observable<ExecutionAuthorizationList> {
    return this.http.get<ExecutionAuthorizationList>(this.executionUrl, {
      params: new HttpParams().set('limit', limit).set('view', view),
    })
  }

  executionReceipt(id: string): Observable<ExecutionAuthorizationReceipt> {
    return this.http.get<ExecutionAuthorizationReceipt>(
      `${this.executionUrl}/${encodeURIComponent(id)}`
    )
  }

  executionConsumption(id: string): Observable<ExecutionAuthorizationConsumption> {
    return this.http.get<ExecutionAuthorizationConsumption>(
      `${this.executionUrl}/${encodeURIComponent(id)}/consumption`
    )
  }

  listMandates(): Observable<StandingMandateList> {
    return this.http.get<StandingMandateList>(this.mandateUrl)
  }

  mandate(id: string): Observable<StandingMandate> {
    return this.http.get<StandingMandate>(`${this.mandateUrl}/${encodeURIComponent(id)}`)
  }

  createMandate(request: CreateStandingMandateRequest): Observable<StandingMandate> {
    return this.http.post<StandingMandate>(this.mandateUrl, request)
  }

  activateMandate(id: string, expectedRevision: number): Observable<StandingMandate> {
    return this.http.post<StandingMandate>(
      `${this.mandateUrl}/${encodeURIComponent(id)}/activate`,
      { expectedRevision }
    )
  }

  revokeMandate(
    id: string,
    expectedRevision: number,
    reason: string
  ): Observable<StandingMandate> {
    return this.http.post<StandingMandate>(
      `${this.mandateUrl}/${encodeURIComponent(id)}/revoke`,
      { expectedRevision, reason }
    )
  }

  authorizeMandate(
    id: string,
    request: MandateAuthorizationRequest
  ): Observable<MandateAuthorizationDecision> {
    return this.http.post<MandateAuthorizationDecision>(
      `${this.mandateUrl}/${encodeURIComponent(id)}/authorize`,
      request
    )
  }

  listMandateDecisions(mandateId?: string, limit = 100): Observable<StandingMandateDecisionList> {
    let params = new HttpParams().set('limit', limit)
    if (mandateId) params = params.set('mandateId', mandateId)
    return this.http.get<StandingMandateDecisionList>(`${this.mandateUrl}/decisions`, { params })
  }

  listLearningProposals(limit = 100): Observable<LearningProposalList> {
    return this.http.get<LearningProposalList>(`${this.learningUrl}/proposals`, {
      params: new HttpParams().set('limit', limit),
    })
  }

  listLearningOutcomes(limit = 100): Observable<LearningOutcomeList> {
    return this.http.get<LearningOutcomeList>(`${this.learningUrl}/outcomes`, {
      params: new HttpParams().set('limit', limit),
    })
  }

  learningProposal(id: string): Observable<LearningProposal> {
    return this.http.get<LearningProposal>(
      `${this.learningUrl}/proposals/${encodeURIComponent(id)}`
    )
  }

  learningDecisions(id: string, limit = 100): Observable<LearningDecisionList> {
    return this.http.get<LearningDecisionList>(
      `${this.learningUrl}/proposals/${encodeURIComponent(id)}/decisions`,
      { params: new HttpParams().set('limit', limit) }
    )
  }

  decideLearningProposal(
    id: string,
    request: DecideLearningProposalRequest
  ): Observable<LearningDecisionResult> {
    return this.http.post<unknown>(
      `${this.learningUrl}/proposals/${encodeURIComponent(id)}/decisions`,
      request
    ).pipe(map((result) => this.normalizeLearningDecisionResult(result)))
  }

  listAgents(): Observable<AgentList> {
    return this.http.get<AgentList>(this.agentUrl)
  }

  agent(id: string): Observable<AgentRecord> {
    return this.http.get<AgentRecord>(`${this.agentUrl}/${encodeURIComponent(id)}`)
  }

  registerAgent(request: RegisterAgentRequest): Observable<AgentRecord> {
    return this.http.post<AgentRecord>(this.agentUrl, request)
  }

  transitionAgent(
    id: string,
    expectedRevision: number,
    to: AgentLifecycleState,
    reason: string
  ): Observable<AgentRecord> {
    return this.http.post<AgentRecord>(
      `${this.agentUrl}/${encodeURIComponent(id)}/transitions`,
      { expectedRevision, to, reason }
    )
  }

  agentTransitions(id: string): Observable<AgentTransitionList> {
    return this.http.get<AgentTransitionList>(
      `${this.agentUrl}/${encodeURIComponent(id)}/transitions`
    )
  }

  assignAgent(request: AgentAssignmentRequest): Observable<AgentAssignment> {
    return this.http.post<AgentAssignment>(`${this.agentUrl}/assignments`, request)
  }

  assignment(id: string): Observable<AgentAssignment> {
    return this.http.get<AgentAssignment>(
      `${this.agentUrl}/assignments/${encodeURIComponent(id)}`
    )
  }

  domainCatalog(): Observable<DomainPackCatalog> {
    return this.http.get<DomainPackCatalog>(this.domainUrl, {
      params: new HttpParams().set('view', 'summary'),
    }).pipe(
      map((catalog) => ({
        ...catalog,
        packs: (catalog.packs || [])
          .filter((view) => !!view?.pack?.id)
          .map((view) => this.normalizeDomainPackSummary(view)),
      }))
    )
  }

  classifyDomain(text: string, explicitPackIds: string[] = []): Observable<DomainClassificationResult> {
    return this.http.post<DomainClassificationResult>(`${this.domainUrl}/classify`, {
      text,
      explicitPackIds,
    })
  }

  effectiveDomainPack(id: string): Observable<DomainPackView> {
    return this.http.get<DomainPackView>(
      `${this.domainUrl}/${encodeURIComponent(id)}/effective`
    ).pipe(map((view) => this.normalizeDomainPack(view)))
  }

  updateDomainPreference(
    id: string,
    request: UpdateDomainPreferenceRequest
  ): Observable<DomainPackPreferenceResult> {
    return this.http.put<DomainPackPreferenceResult>(
      `${this.domainUrl}/${encodeURIComponent(id)}/preference`,
      request
    )
  }

  listAgentTeams(): Observable<AgentTeamList> {
    return this.http.get<AgentTeamList>(this.teamUrl).pipe(
      map((response) => ({ teams: response?.teams || [] }))
    )
  }

  agentTeamMessageAttention(teamId: string, version: string): Observable<AgentTeamMessageAttentionPage> {
    return this.http.get<AgentTeamMessageAttentionPage>(
      `${this.teamUrl}/${encodeURIComponent(teamId)}/versions/${encodeURIComponent(version)}/message-attention`
    ).pipe(map((response) => ({
      generatedAt: response.generatedAt,
      messages: response?.messages || [],
    })))
  }

  listLifeEntities(
    limit = 50,
    allowLocalOnly = false,
    filters: LifeOntologyEntityFilters = {}
  ): Observable<LifeOntologyEntityList> {
    let params = new HttpParams()
      .set('limit', limit)
      .set('allowLocalOnly', String(allowLocalOnly))
    if (filters.types?.length) params = params.set('types', filters.types.join(','))
    if (filters.statuses?.length) params = params.set('statuses', filters.statuses.join(','))
    if (filters.verification?.length) {
      params = params.set('verification', filters.verification.join(','))
    }
    return this.http.get<LifeOntologyEntityList>(`${this.lifeOntologyUrl}/entities`, {
      params,
    }).pipe(map((response) => ({ entities: response?.entities || [] })))
  }

  listLifeRelations(limit = 50, allowLocalOnly = false): Observable<LifeOntologyRelationList> {
    return this.http.get<LifeOntologyRelationList>(`${this.lifeOntologyUrl}/relations`, {
      params: new HttpParams()
        .set('limit', limit)
        .set('allowLocalOnly', String(allowLocalOnly)),
    }).pipe(map((response) => ({ relations: response?.relations || [] })))
  }

  listLifeMergeProposals(limit = 50): Observable<LifeOntologyMergeProposalList> {
    return this.http.get<LifeOntologyMergeProposalList>(
      `${this.lifeOntologyUrl}/merge-proposals`,
      { params: new HttpParams().set('limit', limit) }
    ).pipe(map((response) => ({ proposals: response?.proposals || [] })))
  }

  listContactReviewDecisions(limit = 100): Observable<ContactReviewDecisionList> {
    return this.http.get<ContactReviewDecisionList>(
      `${this.lifeOntologyUrl}/contact-review-decisions`,
      { params: new HttpParams().set('limit', limit) }
    ).pipe(map((response) => ({ decisions: response?.decisions || [] })))
  }

  decideContactCandidate(
    id: string,
    request: ContactReviewDecisionRequest
  ): Observable<ContactReviewDecisionResult> {
    return this.http.post<ContactReviewDecisionResult>(
      `${this.lifeOntologyUrl}/contact-candidates/${encodeURIComponent(id)}/decisions`,
      request
    )
  }

  decideContactMerge(
    id: string,
    request: ContactReviewDecisionRequest
  ): Observable<ContactReviewDecisionResult> {
    return this.http.post<ContactReviewDecisionResult>(
      `${this.lifeOntologyUrl}/merge-proposals/${encodeURIComponent(id)}/decisions`,
      request
    )
  }

  listLifeCommitments(limit = 50): Observable<LifeCommitmentList> {
    return this.http.get<LifeCommitmentList>(`${this.lifeLedgerUrl}/commitments`, {
      params: new HttpParams().set('limit', limit),
    }).pipe(map((response) => ({ commitments: response?.commitments || [] })))
  }

  lifeCommitment(key: string): Observable<LifeCommitmentRevision> {
    return this.http.get<LifeCommitmentRevision>(
      `${this.lifeLedgerUrl}/commitments/${encodeURIComponent(key)}`
    )
  }

  lifeCommitmentHistory(key: string, limit = 50): Observable<LifeCommitmentHistory> {
    return this.http.get<LifeCommitmentHistory>(
      `${this.lifeLedgerUrl}/commitments/${encodeURIComponent(key)}/history`,
      { params: new HttpParams().set('limit', limit) }
    ).pipe(map((response) => ({ revisions: response?.revisions || [] })))
  }

  recordLifeCommitment(
    key: string,
    request: RecordLifeCommitmentRevisionRequest
  ): Observable<LifeCommitmentWriteResult> {
    return this.http.post<LifeCommitmentWriteResult>(
      `${this.lifeLedgerUrl}/commitments/${encodeURIComponent(key)}/revisions`,
      request
    )
  }

  listLifeCosts(limit = 50): Observable<LifeCostList> {
    return this.http.get<LifeCostList>(`${this.lifeLedgerUrl}/costs`, {
      params: new HttpParams().set('limit', limit),
    }).pipe(map((response) => ({ costs: response?.costs || [] })))
  }

  recordLifeCost(request: RecordLifeCostEventRequest): Observable<LifeCostWriteResult> {
    return this.http.post<LifeCostWriteResult>(`${this.lifeLedgerUrl}/costs`, request)
  }

  proactivityPolicy(): Observable<ProactivityPolicyRecord> {
    return this.http.get<ProactivityPolicyRecord>(`${this.proactivityUrl}/policy`)
  }

  listProactivitySignals(limit = 50): Observable<ProactivitySignalList> {
    return this.http.get<ProactivitySignalList>(`${this.proactivityUrl}/signals`, {
      params: new HttpParams().set('limit', limit),
    }).pipe(map((response) => ({ signals: response?.signals || [] })))
  }

  listProactivityDecisions(limit = 50): Observable<ProactivityDecisionList> {
    return this.http.get<ProactivityDecisionList>(`${this.proactivityUrl}/decisions`, {
      params: new HttpParams().set('limit', limit),
    }).pipe(map((response) => ({ decisions: response?.decisions || [] })))
  }

  listProactivityFeedback(limit = 50): Observable<ProactivityFeedbackList> {
    return this.http.get<ProactivityFeedbackList>(`${this.proactivityUrl}/feedback`, {
      params: new HttpParams().set('limit', limit),
    }).pipe(map((response) => ({ feedback: response?.feedback || [] })))
  }

  recordProactivityFeedback(request: RecordProactivityFeedbackRequest): Observable<ProactivityFeedbackRecord> {
    return this.http.post<ProactivityFeedbackRecord>(`${this.proactivityUrl}/feedback`, request)
  }

  outcomeDefinition(workspaceId: string, outcomeId: string): Observable<OutcomeRevision> {
    return this.http.get<OutcomeRevision>(this.outcomeUrl(workspaceId, outcomeId))
  }

  storeOutcome(
    workspaceId: string,
    outcomeId: string,
    request: StoreOutcomeRequest
  ): Observable<OutcomeRevision> {
    return this.http.put<OutcomeRevision>(this.outcomeUrl(workspaceId, outcomeId), request)
  }

  createOutcomeEvaluation(
    workspaceId: string,
    outcomeId: string,
    request: CreateOutcomeEvaluationRequest
  ): Observable<OutcomeEvaluationRecord> {
    return this.http.post<OutcomeEvaluationRecord>(
      `${this.outcomeUrl(workspaceId, outcomeId)}/evaluations`,
      request
    )
  }

  listOutcomeEvaluations(
    workspaceId: string,
    outcomeId: string
  ): Observable<OutcomeEvaluationList> {
    return this.http.get<OutcomeEvaluationList>(
      `${this.outcomeUrl(workspaceId, outcomeId)}/evaluations`
    ).pipe(map((response) => ({ evaluations: response?.evaluations || [] })))
  }

  listOutcomeCorrections(
    workspaceId: string,
    outcomeId: string
  ): Observable<OutcomeCorrectionList> {
    return this.http.get<OutcomeCorrectionList>(
      `${this.outcomeUrl(workspaceId, outcomeId)}/corrections`
    ).pipe(map((response) => ({ corrections: response?.corrections || [] })))
  }

  resilienceStatus(workspaceId: string): Observable<ResilienceStatus> {
    return this.http.get<ResilienceStatus>(
      `${this.resilienceUrl}/${encodeURIComponent(workspaceId)}/status`
    )
  }

  private normalizeDomainPack(view: DomainPackView): DomainPackView {
    if (!view?.pack?.id) {
      throw new Error('The domain-pack API returned a record without an identity.')
    }

    const pack = view.pack
    return {
      ...view,
      pack: {
        ...pack,
        classificationSignals: pack.classificationSignals || [],
        intakeQuestions: pack.intakeQuestions || [],
        riskTriggers: pack.riskTriggers || [],
        approvalRules: pack.approvalRules || [],
        prohibitedAutonomousActions: pack.prohibitedAutonomousActions || [],
        evidenceRequirements: pack.evidenceRequirements || [],
        suitableAgentCapabilities: pack.suitableAgentCapabilities || [],
        playbook: {
          version: pack.playbook?.version || '',
          digest: pack.playbook?.digest || '',
          methods: pack.playbook?.methods || [],
        },
      },
    }
  }

  private normalizeDomainPackSummary(view: DomainPackSummaryView): DomainPackSummaryView {
    if (!view?.pack?.id) {
      throw new Error('The domain-pack API returned a summary without an identity.')
    }
    return {
      ...view,
      pack: {
        ...view.pack,
        methodCount: Number.isInteger(view.pack.methodCount) && (view.pack.methodCount ?? -1) >= 0
          ? view.pack.methodCount
          : 0,
      },
    }
  }

  private outcomeUrl(workspaceId: string, outcomeId: string): string {
    return `${this.outcomeEvaluationUrl}/${encodeURIComponent(workspaceId)}` +
      `/outcomes/${encodeURIComponent(outcomeId)}`
  }

  private normalizeLearningDecisionResult(value: unknown): LearningDecisionResult {
    const result = value as {
      proposal?: LearningProposal
      application?: Partial<LearningApplicationSummary>
    }
    if (!result?.proposal?.id) {
      throw new Error('The controlled-learning API returned no proposal result.')
    }
    if (!result.application) return { proposal: result.proposal }

    const application = result.application
    if (!application.id || !application.status || !application.mode) {
      throw new Error('The controlled-learning API returned incomplete application status.')
    }

    return {
      proposal: result.proposal,
      application: {
        id: application.id,
        proposalId: application.proposalId || result.proposal.id,
        proposalRevision: application.proposalRevision || result.proposal.revision,
        mode: application.mode,
        status: application.status,
        target: application.target || result.proposal.target,
        protectedTarget: application.protectedTarget ?? result.proposal.protectedTarget,
        applierId: application.applierId || '',
        currentVersion: application.currentVersion || result.proposal.currentVersion,
        proposedVersion: application.proposedVersion || result.proposal.proposedVersion,
        appliedVersion: application.appliedVersion,
        restoredVersion: application.restoredVersion,
        governanceReference: application.governanceReference,
        handoffReference: application.handoffReference,
        evidence: application.evidence || [],
        rollbackEvidence: application.rollbackEvidence || [],
        attempt: application.attempt || 1,
        lastErrorCode: application.lastErrorCode,
        resultDigest: application.resultDigest,
        createdAt: application.createdAt || '',
        updatedAt: application.updatedAt || '',
        completedAt: application.completedAt,
        rolledBackAt: application.rolledBackAt,
      },
    }
  }
}

type DomainPackPreferenceResult = NonNullable<DomainPackView['preference']>

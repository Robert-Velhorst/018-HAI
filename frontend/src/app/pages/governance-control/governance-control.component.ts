import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core'
import { NzModalService } from 'ng-zorro-antd/modal'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { catchError, forkJoin, map, of, Subscription, switchMap, throwError } from 'rxjs'
import {
  AgentAssignment,
  AgentLifecycleState,
  AgentRecord,
  AgentTeamContract,
  AgentTeamMessageAttention,
  AgentTeamMessageAttentionPage,
  AgentTransition,
  AdvisoryAvailability,
  AuthorizationOutcome,
  ContactCandidateReviewAction,
  ContactMergeReviewAction,
  ContactReviewAction,
  ContactReviewDecision,
  ContactReviewDecisionResult,
  DomainClassificationResult,
  DomainPackPreferenceStatus,
  DomainPackSummaryView,
  DomainPackView,
  ExecutionAuthorizationConsumption,
  ExecutionAuthorizationReceipt,
  ExecutionAuthorizationSummary,
  GovernanceRisk,
  LearningApplicationSummary,
  LearningDecisionKind,
  LearningOutcomeRecord,
  LearningProposal,
  LearningReviewDecision,
  LifeCommitmentRevision,
  LifeCommitmentStatus,
  LifeCostEntry,
  LifeCostKind,
  LifeLedgerEvidenceReference,
  LifeLedgerVerificationStatus,
  LifeOntologyEntity,
  LifeOntologyMergeProposal,
  LifeOntologyRelation,
  MandateAuthorizationDecision,
  OutcomeCorrectionRecord,
  OutcomeEvaluationRecord,
  OutcomeLifeGraphProjection,
  OutcomeLifeDomain,
  OutcomeObservationInput,
  OutcomeRevision,
  ProactivityDecisionRecord,
  ProactivityFeedbackAction,
  ProactivityFeedbackRecord,
  ProactivityPolicyRecord,
  ProactivitySignalRecord,
  ResilienceStatus,
  StandingMandate,
  UpdateDomainPreferenceRequest,
} from '../../models/governance-control.model'
import {
  MonitorCompositionAttempt,
  MonitorCompositionDelivery,
  MonitorCompositionFeedbackWatermark,
  MonitorCompositionSnapshot,
  MonitorCompositionWatermark,
  MonitorRun,
  MonitorSourceKind,
  MonitorTarget,
  ObservationRecord,
} from '../../models/ambient-monitor.model.interface'
import { AmbientMonitorService } from '../../services/ambient-monitor.service'
import { GovernanceControlService } from '../../services/governance-control.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'

type GovernanceSurface = 'execution' | 'mandates' | 'learning' | 'agents' | 'domains'
type InspectorKind = GovernanceSurface | 'receipt' | 'mandate' | 'proposal' | 'agent' | 'domain'
  | 'commitment' | 'commitment-author' | 'cost' | 'cost-author'
  | 'contact-review'
type AdvisorySurface = 'teams' | 'life' | 'ledger' | 'proactivity' | 'outcomes' | 'resilience'

interface SurfaceState {
  loading: boolean
  loaded: boolean
  error: string
  loadedAt?: string
}

interface AdvisorySurfaceState {
  availability: AdvisoryAvailability
  loading: boolean
  loaded: boolean
  error: string
  loadedAt?: string
}

interface GovernanceAttention {
  id: string
  kind: 'mandate' | 'proposal' | 'receipt' | 'agent'
  title: string
  summary: string
  tone: 'blue' | 'green' | 'gold' | 'red'
  owner: string
}

interface LifeCostTotal {
  currency: string
  amountMinor: number
}

interface AmbientMonitorViewState {
  loading: boolean
  loaded: boolean
  historyLoading: boolean
  error: string
  historyError: string
  loadedAt?: string
}

interface MonitorCompositionViewState {
  loading: boolean
  loaded: boolean
  historyLoading: boolean
  historyLoaded: boolean
  error: string
  historyError: string
}

interface MonitorCompositionProvenanceDetail {
  field: string
  label: string
  value: string
  digest: boolean
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-governance-control',
  templateUrl: './governance-control.component.html',
  styleUrls: ['./governance-control.component.scss'],
})
export class GovernanceControlComponent implements OnInit, OnDestroy {
  readonly moduleId = 'governance-control'
  readonly surfaces: GovernanceSurface[] = ['execution', 'mandates', 'learning', 'agents', 'domains']
  state: Record<GovernanceSurface, SurfaceState> = {
    execution: this.newSurfaceState(),
    mandates: this.newSurfaceState(),
    learning: this.newSurfaceState(),
    agents: this.newSurfaceState(),
    domains: this.newSurfaceState(),
  }

  receipts: ExecutionAuthorizationSummary[] = []
  mandates: StandingMandate[] = []
  mandateDecisions: MandateAuthorizationDecision[] = []
  proposals: LearningProposal[] = []
  learningOutcomes: LearningOutcomeRecord[] = []
  agents: AgentRecord[] = []
  domainPacks: DomainPackSummaryView[] = []
  catalogVersion = ''
  catalogDigest = ''

  readonly advisorySurfaces: AdvisorySurface[] = [
    'teams', 'life', 'ledger', 'proactivity', 'outcomes', 'resilience',
  ]
  advisoryState: Record<AdvisorySurface, AdvisorySurfaceState> = {
    teams: this.newAdvisoryState('idle'),
    life: this.newAdvisoryState('idle'),
    ledger: this.newAdvisoryState('idle'),
    proactivity: this.newAdvisoryState('idle'),
    outcomes: this.newAdvisoryState('not_configured'),
    resilience: this.newAdvisoryState('not_configured'),
  }
  agentTeams: AgentTeamContract[] = []
  agentTeamAttention: Record<string, AgentTeamMessageAttentionPage> = {}
  lifeEntities: LifeOntologyEntity[] = []
  contactCandidateEntities: LifeOntologyEntity[] = []
  lifeRelations: LifeOntologyRelation[] = []
  lifeMergeProposals: LifeOntologyMergeProposal[] = []
  contactReviewDecisions: ContactReviewDecision[] = []
  lifeCommitments: LifeCommitmentRevision[] = []
  lifeCosts: LifeCostEntry[] = []
  selectedLifeCommitment?: LifeCommitmentRevision
  selectedLifeCommitmentHistory: LifeCommitmentRevision[] = []
  selectedLifeCost?: LifeCostEntry
  selectedContactCandidate?: LifeOntologyEntity
  selectedContactMergeProposal?: LifeOntologyMergeProposal
  selectedContactReviewDecision?: ContactReviewDecision
  selectedCanonicalContact?: LifeOntologyEntity
  includeLocalOnlyLifeContext = false
  proactivityPolicy?: ProactivityPolicyRecord
  proactivitySignals: ProactivitySignalRecord[] = []
  proactivityDecisions: ProactivityDecisionRecord[] = []
  proactivityFeedback: ProactivityFeedbackRecord[] = []
  outcomeDefinition?: OutcomeRevision
  outcomeEvaluations: OutcomeEvaluationRecord[] = []
  outcomeCorrections: OutcomeCorrectionRecord[] = []
  outcomeObservationDrafts: OutcomeObservationInput[] = []
  latestOutcomeLifeGraphProjection?: OutcomeLifeGraphProjection
  outcomeLifeGraphProjectionWarning = ''
  resilience?: ResilienceStatus
  advisoryScope = { workspaceId: '', outcomeId: '' }

  readonly monitorSourceKinds: Array<{ value: MonitorSourceKind; label: string; description: string }> = [
    {
      value: 'workflow_verified_completion_count',
      label: 'Verified workflow completions',
      description: 'Counts source-backed workflow completions that passed verification.',
    },
    {
      value: 'workflow_open_loop_count',
      label: 'Open workflow loops',
      description: 'Counts unresolved workflow items that still need attention.',
    },
    {
      value: 'overdue_commitment_count',
      label: 'Overdue commitments',
      description: 'Counts source-backed commitments that are past due and unresolved.',
    },
  ]
  monitorState: AmbientMonitorViewState = this.newMonitorState()
  monitorTargets: MonitorTarget[] = []
  monitorObservations: ObservationRecord[] = []
  monitorRuns: MonitorRun[] = []
  monitorCompositions: MonitorCompositionDelivery[] = []
  monitorCompositionAttempts: MonitorCompositionAttempt[] = []
  monitorCompositionState: MonitorCompositionViewState = this.newMonitorCompositionState()
  selectedMonitorTargetId = ''
  monitorMutating = false
  monitorForm = {
    targetId: '',
    sourceKind: 'workflow_verified_completion_count' as MonitorSourceKind,
    cadenceSeconds: 86400,
    firstRunAt: this.toLocalDateTime(new Date()),
    enabled: true,
  }

  outcomeForm = {
    statement: '',
    lifeDomain: '' as OutcomeLifeDomain | '',
    windowStart: '',
    windowEnd: '',
    indicatorId: '',
    indicatorName: '',
    unit: '',
    direction: 'higher' as 'higher' | 'lower' | 'maintain',
    targetValue: 0,
    targetTolerance: 0,
    trendThresholdPerDay: 0,
    regressionThreshold: 1,
    minimumObservations: 2,
    baselineValue: 0,
    baselineObservedAt: '',
  }

  readonly outcomeLifeDomains: Array<{ value: OutcomeLifeDomain; label: string }> = [
    { value: 'safety_security', label: 'Safety and security' },
    { value: 'health_wellbeing', label: 'Health and wellbeing' },
    { value: 'relationships_care', label: 'Relationships and care' },
    { value: 'housing_assets', label: 'Housing and assets' },
    { value: 'financial', label: 'Financial' },
    { value: 'work_venture', label: 'Work and ventures' },
    { value: 'learning_growth', label: 'Learning and growth' },
    { value: 'meaning_values', label: 'Meaning and values' },
    { value: 'community_civic', label: 'Community and civic life' },
    { value: 'legal_government', label: 'Legal and government' },
    { value: 'personal_administration', label: 'Personal administration' },
  ]

  outcomeObservationForm = {
    indicatorId: '',
    value: 0,
    observedAt: '',
    rationale: '',
  }

  readonly commitmentStatuses: LifeCommitmentStatus[] = [
    'proposed', 'active', 'waiting', 'fulfilled', 'cancelled', 'breached', 'disputed',
  ]
  readonly ledgerVerificationStatuses: LifeLedgerVerificationStatus[] = [
    'needs_review', 'source_supported', 'human_confirmed', 'verified', 'disputed',
  ]
  readonly costKinds: LifeCostKind[] = ['estimate', 'incurred', 'paid', 'refund']
  commitmentForm = this.emptyCommitmentForm()
  costForm = this.emptyCostForm()
  contactReviewForm = this.emptyContactReviewForm()

  classifierText = ''
  classifying = false
  classification?: DomainClassificationResult

  inspectorVisible = false
  inspectorKind: InspectorKind = 'execution'
  inspectorLoading = false
  inspectorError = ''
  selectedReceipt?: ExecutionAuthorizationReceipt
  selectedConsumption: ExecutionAuthorizationConsumption | null | undefined
  selectedMandate?: StandingMandate
  selectedMandateDecisions: MandateAuthorizationDecision[] = []
  selectedProposal?: LearningProposal
  selectedLearningApplication?: LearningApplicationSummary
  selectedLearningDecisions: LearningReviewDecision[] = []
  selectedAgent?: AgentRecord
  selectedAgentTransitions: AgentTransition[] = []
  selectedDomain?: DomainPackView
  lastAssignment?: AgentAssignment
  lastMandateDecision?: MandateAuthorizationDecision

  mandateDecisionState: SurfaceState = this.newSurfaceState()

  mutating = false
  revokeReason = ''
  transitionReason = ''
  learningRationale = ''
  learningGovernanceReference = ''
  domainPreferenceForm: {
    enabled: boolean
    status: DomainPackPreferenceStatus
    classificationBoost: number
    forceLocalOnly: boolean
    notes: string
  } = {
    enabled: true,
    status: 'active' as const,
    classificationBoost: 0,
    forceLocalOnly: false,
    notes: '',
  }

  mandateForm = {
    name: '',
    purpose: '',
    action: '',
    resourceType: '',
    maximumRisk: 'low' as GovernanceRisk,
    autonomyCeiling: 3,
    approvalMode: 'always' as const,
    sourceReference: '',
  }

  mandateAuthorizationForm = {
    mandateId: '',
    action: '',
    resourceType: '',
    resourceId: '',
    projectKey: '',
    domain: '',
    toolId: '',
    risk: 'low' as GovernanceRisk,
    requestedAutonomy: 1,
    upstreamApprovalRequired: false,
  }

  agentForm = {
    id: '',
    name: '',
    type: 'specialist' as AgentRecord['type'],
    runtimeId: '',
    runtimeType: 'local',
    protocolVersion: '1.0.0',
    capabilityId: '',
    capabilityVersion: '1.0.0',
    authorityCeiling: 2,
    autonomyCeiling: 2,
  }

  assignmentForm = {
    taskId: '',
    capabilityId: '',
    minVersion: '',
    requiredAuthority: 1,
    requiredAutonomy: 1,
    policyMaxAuthority: 3,
    policyMaxAutonomy: 3,
    requireLocal: true,
    allowDegraded: false,
  }

  constructor(
    private service: GovernanceControlService,
    private ambientMonitor: AmbientMonitorService,
    private notification: NzNotificationService,
    private modal: NzModalService,
    private preferences: ModuleViewPreferencesService
  ) {}

  private readonly surfaceSubscriptions: Partial<Record<GovernanceSurface, Subscription>> = {}
  private readonly advisorySubscriptions: Partial<Record<AdvisorySurface, Subscription>> = {}
  private inspectorSubscription?: Subscription
  private mandateDecisionSubscription?: Subscription
  private monitorSubscription?: Subscription
  private monitorHistorySubscription?: Subscription
  private monitorCompositionSubscription?: Subscription
  private monitorCompositionHistorySubscription?: Subscription
  private outcomeScopeKey = ''
  private resilienceScopeKey = ''

  ngOnInit(): void {
    this.refresh()
    if (this.preferences.get(this.moduleId).openSections['standing-mandates']) {
      this.loadMandateDecisions(true)
    }
  }

  ngOnDestroy(): void {
    Object.values(this.surfaceSubscriptions).forEach((subscription) => subscription?.unsubscribe())
    Object.values(this.advisorySubscriptions).forEach((subscription) => subscription?.unsubscribe())
    this.inspectorSubscription?.unsubscribe()
    this.mandateDecisionSubscription?.unsubscribe()
    this.monitorSubscription?.unsubscribe()
    this.monitorHistorySubscription?.unsubscribe()
    this.monitorCompositionSubscription?.unsubscribe()
    this.monitorCompositionHistorySubscription?.unsubscribe()
  }

  refresh(): void {
    const openSections = this.preferences.get(this.moduleId).openSections
    this.loadExecution(openSections['execution-receipts'] === true)
    this.loadMandates()
    this.loadLearning(openSections['controlled-learning'] === true)
    this.loadAgents()
    this.loadDomains()

    if (openSections['agent-teams']) this.loadAgentTeams(true, true)
    if (openSections['whole-life-context']) this.loadLifeContext(true, true)
    if (openSections['life-ledger-overview']) this.loadLifeLedger(true, true)
    if (openSections['proactivity-policy']) this.loadProactivity(true, true)
    if (openSections['resilience-status'] && this.advisoryScope.workspaceId.trim()) {
      this.loadResilienceStatus(true, true)
    }
    if (
      openSections['outcome-evaluations'] &&
      this.advisoryScope.workspaceId.trim() &&
      this.advisoryScope.outcomeId.trim()
    ) {
      this.loadOutcomeEvidence(true, true)
    }
    if (this.mandateDecisionState.loaded || openSections['standing-mandates']) {
      this.loadMandateDecisions(true, true)
    }
  }

  loadExecution(full = false): void {
    this.beginLoad('execution')
    this.surfaceSubscriptions.execution?.unsubscribe()
    this.surfaceSubscriptions.execution = this.service
      .listExecutionReceipts(50, full ? 'full' : 'summary')
      .subscribe({
      next: (response) => {
        this.receipts = response.receipts || []
        this.finishLoad('execution')
      },
      error: (error) => this.failLoad('execution', error, 'Authorization receipts are unavailable.'),
    })
  }

  loadMandates(): void {
    this.beginLoad('mandates')
    this.surfaceSubscriptions.mandates?.unsubscribe()
    this.surfaceSubscriptions.mandates = this.service.listMandates().subscribe({
      next: (response) => {
        this.mandates = response.mandates || []
        this.finishLoad('mandates')
      },
      error: (error) => this.failLoad('mandates', error, 'Standing mandates are unavailable.'),
    })
  }

  loadLearning(includeOutcomeHistory = false): void {
    this.beginLoad('learning')
    this.surfaceSubscriptions.learning?.unsubscribe()
    this.surfaceSubscriptions.learning = forkJoin({
      proposals: this.service.listLearningProposals(100),
      outcomes: includeOutcomeHistory
        ? this.service.listLearningOutcomes(100)
        : of({ outcomes: this.learningOutcomes }),
    }).subscribe({
      next: (response) => {
        this.proposals = response.proposals.proposals || []
        this.learningOutcomes = response.outcomes.outcomes || []
        this.finishLoad('learning')
      },
      error: (error) => this.failLoad('learning', error, 'Learning evidence and proposals are unavailable.'),
    })
  }

  loadExecutionSection(open: boolean): void {
    if (open) this.loadExecution(true)
  }

  loadLearningSection(open: boolean): void {
    if (open) this.loadLearning(true)
  }

  loadAgents(): void {
    this.beginLoad('agents')
    this.surfaceSubscriptions.agents?.unsubscribe()
    this.surfaceSubscriptions.agents = this.service.listAgents().subscribe({
      next: (response) => {
        this.agents = response.agents || []
        this.finishLoad('agents')
      },
      error: (error) => this.failLoad('agents', error, 'The durable agent registry is unavailable.'),
    })
  }

  loadDomains(): void {
    this.beginLoad('domains')
    this.surfaceSubscriptions.domains?.unsubscribe()
    this.surfaceSubscriptions.domains = this.service.domainCatalog().subscribe({
      next: (response) => {
        this.domainPacks = response.packs || []
        this.catalogVersion = response.metadata.version
        this.catalogDigest = response.metadata.digest
        this.finishLoad('domains')
      },
      error: (error) => this.failLoad('domains', error, 'Domain packs are unavailable.'),
    })
  }

  loadAgentTeams(open = true, force = false): void {
    if (!open || !this.shouldLoadAdvisory('teams', force)) return
    this.beginAdvisoryLoad('teams')
    this.advisorySubscriptions.teams?.unsubscribe()
    this.advisorySubscriptions.teams = this.service.listAgentTeams().pipe(
      switchMap((response) => {
        const teams = response.teams || []
        if (!teams.length) return of({ teams, attention: [] as Array<{ key: string; page: AgentTeamMessageAttentionPage }> })
        return forkJoin(teams.map((team) =>
          this.service.agentTeamMessageAttention(team.id, team.version).pipe(
            map((page) => ({ key: this.agentTeamAttentionKey(team), page })),
            catchError(() => of({
              key: this.agentTeamAttentionKey(team),
              page: { generatedAt: '', messages: [] } as AgentTeamMessageAttentionPage,
            }))
          )
        )).pipe(map((attention) => ({ teams, attention })))
      })
    ).subscribe({
      next: ({ teams, attention }) => {
        this.agentTeams = teams
        this.agentTeamAttention = Object.fromEntries(attention.map((item) => [item.key, item.page]))
        this.finishAdvisoryLoad('teams', this.agentTeams.length)
      },
      error: (error) => this.failAdvisoryLoad('teams', error, 'Agent-team records could not be loaded.'),
    })
  }

  loadLifeContext(open = true, force = false): void {
    if (!open || !this.shouldLoadAdvisory('life', force)) return
    this.beginAdvisoryLoad('life')
    this.advisorySubscriptions.life?.unsubscribe()
    this.advisorySubscriptions.life = forkJoin({
      entities: this.service.listLifeEntities(50, this.includeLocalOnlyLifeContext),
      contactCandidates: this.service.listLifeEntities(100, true, {
        types: ['person'],
        verification: ['needs_review'],
      }),
      relations: this.service.listLifeRelations(50, this.includeLocalOnlyLifeContext),
      proposals: this.service.listLifeMergeProposals(50),
      decisions: this.service.listContactReviewDecisions(100),
    }).subscribe({
      next: ({ entities, contactCandidates, relations, proposals, decisions }) => {
        this.lifeEntities = entities.entities || []
        this.contactCandidateEntities = (contactCandidates.entities || []).filter(
          (entity) => entity.type === 'person' && entity.attributes?.['candidate'] === 'true'
        )
        this.lifeRelations = relations.relations || []
        this.lifeMergeProposals = proposals.proposals || []
        this.contactReviewDecisions = decisions.decisions || []
        this.finishAdvisoryLoad(
          'life',
          this.lifeEntities.length + this.lifeRelations.length + this.pendingContactCandidates.length +
            this.pendingLifeMergeProposals.length + this.contactReviewDecisions.length
        )
      },
      error: (error) => this.failAdvisoryLoad('life', error, 'Whole-life context could not be loaded.'),
    })
  }

  openContactCandidate(candidate: LifeOntologyEntity): void {
    this.resetInspector('contact-review')
    this.selectedContactCandidate = candidate
    this.contactReviewForm = this.emptyContactReviewForm(candidate.name, candidate.summary || '')
    this.inspectorVisible = true
  }

  openContactMergeProposal(proposal: LifeOntologyMergeProposal): void {
    this.resetInspector('contact-review')
    this.selectedContactMergeProposal = proposal
    const candidates = this.contactCandidatesForProposal(proposal)
    const sharedName = candidates.length && candidates.every(
      (candidate) => candidate.name.toLocaleLowerCase() === candidates[0].name.toLocaleLowerCase()
    ) ? candidates[0].name : ''
    this.contactReviewForm = this.emptyContactReviewForm(sharedName, '')
    this.inspectorVisible = true
  }

  decideContactCandidate(action: ContactCandidateReviewAction): void {
    const candidate = this.selectedContactCandidate
    if (!candidate) return
    this.submitContactReview(action, candidate.id, false)
  }

  decideContactMerge(action: ContactMergeReviewAction): void {
    const proposal = this.selectedContactMergeProposal
    if (!proposal) return
    this.submitContactReview(action, proposal.id, true)
  }

  loadLifeLedger(open = true, force = false): void {
    if (!open || !this.shouldLoadAdvisory('ledger', force)) return
    this.beginAdvisoryLoad('ledger')
    this.advisorySubscriptions.ledger?.unsubscribe()
    this.advisorySubscriptions.ledger = forkJoin({
      commitments: this.service.listLifeCommitments(50),
      costs: this.service.listLifeCosts(50),
    }).subscribe({
      next: ({ commitments, costs }) => {
        this.lifeCommitments = commitments.commitments || []
        this.lifeCosts = costs.costs || []
        this.finishAdvisoryLoad('ledger', this.lifeCommitments.length + this.lifeCosts.length)
      },
      error: (error) => this.failAdvisoryLoad(
        'ledger', error, 'Commitment and cost ledger records could not be loaded.'
      ),
    })
  }

  newLifeCommitment(): void {
    this.resetInspector('commitment-author')
    this.commitmentForm = this.emptyCommitmentForm()
    this.inspectorVisible = true
  }

  reviseLifeCommitment(commitment: LifeCommitmentRevision): void {
    this.resetInspector('commitment-author')
    this.selectedLifeCommitment = commitment
    this.commitmentForm = {
      key: commitment.commitmentKey,
      expectedRevision: commitment.revision,
      domain: commitment.domain,
      title: commitment.title,
      summary: commitment.summary || '',
      status: commitment.status,
      counterparty: commitment.counterparty || '',
      projectKey: commitment.projectKey || '',
      dueAt: commitment.dueAt ? commitment.dueAt.slice(0, 16) : '',
      verification: commitment.verification,
      observedAt: this.toLocalDateTime(new Date()),
      sourceId: '', sourceUri: '', contentDigest: '', authority: '',
    }
    this.inspectorVisible = true
  }

  openLifeCommitment(commitment: LifeCommitmentRevision): void {
    this.resetInspector('commitment')
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = forkJoin({
      current: this.service.lifeCommitment(commitment.commitmentKey),
      history: this.service.lifeCommitmentHistory(commitment.commitmentKey, 100),
    }).subscribe({
      next: ({ current, history }) => {
        this.selectedLifeCommitment = current
        this.selectedLifeCommitmentHistory = history.revisions || []
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Commitment history could not be loaded.'),
    })
  }

  recordLifeCommitment(): void {
    const form = this.commitmentForm
    const key = form.key.trim()
    const evidence = this.ledgerEvidence(form)
    const observedAt = this.isoDateTime(form.observedAt)
    if (!key || !form.title.trim() || !form.domain || !observedAt || !evidence) {
      this.notification.error('Commitment not recorded', 'Key, domain, title, observation time, and complete source evidence are required.')
      return
    }
    this.mutating = true
    this.service.recordLifeCommitment(key, {
      expectedRevision: form.expectedRevision,
      domain: form.domain,
      title: form.title.trim(),
      summary: form.summary.trim(),
      status: form.status,
      counterparty: form.counterparty.trim(),
      projectKey: form.projectKey.trim(),
      dueAt: form.dueAt ? this.isoDateTime(form.dueAt) : undefined,
      verification: form.verification,
      evidence: [evidence],
      idempotencyKey: this.operationId('commitment'),
      observedAt,
    }).subscribe({
      next: (result) => {
        this.mutating = false
        this.notification.success(
          result.created ? 'Commitment revision recorded' : 'Existing revision returned',
          'The append-only evidence record is stored; no external action was executed.'
        )
        this.loadLifeLedger(true, true)
        this.openLifeCommitment(result.record)
      },
      error: (error) => this.failMutation(error, 'The commitment revision was not recorded.'),
    })
  }

  newLifeCost(): void {
    this.resetInspector('cost-author')
    this.costForm = this.emptyCostForm()
    this.inspectorVisible = true
  }

  openLifeCost(cost: LifeCostEntry): void {
    this.resetInspector('cost')
    this.selectedLifeCost = cost
    this.inspectorVisible = true
  }

  recordLifeCost(): void {
    const form = this.costForm
    const evidence = this.ledgerEvidence(form)
    const observedAt = this.isoDateTime(form.observedAt)
    const amountMinor = Math.round(Number(form.amount) * 100)
    if (!form.title.trim() || !form.domain || !observedAt || !evidence || amountMinor <= 0) {
      this.notification.error('Cost not recorded', 'Domain, title, positive amount, observation time, and complete source evidence are required.')
      return
    }
    if (!this.costVerificationOptions(form.kind).includes(form.verification)) {
      this.notification.error('Stronger verification required', 'This financial event kind requires stronger evidence before it can be recorded.')
      return
    }
    this.mutating = true
    this.service.recordLifeCost({
      domain: form.domain,
      title: form.title.trim(),
      summary: form.summary.trim(),
      kind: form.kind,
      amountMinor,
      currency: form.currency.trim().toUpperCase(),
      commitmentKey: form.commitmentKey.trim(),
      projectKey: form.projectKey.trim(),
      verification: form.verification,
      evidence: [evidence],
      idempotencyKey: this.operationId('cost'),
      observedAt,
    }).subscribe({
      next: (result) => {
        this.mutating = false
        this.notification.success(
          result.created ? 'Cost evidence recorded' : 'Existing cost record returned',
          'This is an immutable evidence entry only; no money was moved.'
        )
        this.loadLifeLedger(true, true)
        this.openLifeCost(result.record)
      },
      error: (error) => this.failMutation(error, 'The cost evidence was not recorded.'),
    })
  }

  loadProactivity(open = true, force = false): void {
    if (!open || !this.shouldLoadAdvisory('proactivity', force)) return
    this.beginAdvisoryLoad('proactivity')
    this.advisorySubscriptions.proactivity?.unsubscribe()
    this.advisorySubscriptions.proactivity = forkJoin({
      policy: this.service.proactivityPolicy().pipe(
        catchError((error: HttpErrorResponse) =>
          error.status === 404 ? of(undefined) : throwError(() => error)
        )
      ),
      signals: this.service.listProactivitySignals(50),
      decisions: this.service.listProactivityDecisions(50),
      feedback: this.service.listProactivityFeedback(100),
    }).subscribe({
      next: ({ policy, signals, decisions, feedback }) => {
        this.proactivityPolicy = policy
        this.proactivitySignals = signals.signals || []
        this.proactivityDecisions = decisions.decisions || []
        this.proactivityFeedback = feedback.feedback || []
        this.finishAdvisoryLoad(
          'proactivity',
          (policy ? 1 : 0) + this.proactivitySignals.length + this.proactivityDecisions.length + this.proactivityFeedback.length
        )
      },
      error: (error) => this.failAdvisoryLoad(
        'proactivity', error, 'Proactivity and interruption records could not be loaded.'
      ),
    })
  }

  loadOutcomeEvidence(open = true, force = false): void {
    if (!open) return
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!workspaceId || !outcomeId) {
      this.advisoryState.outcomes = this.newAdvisoryState('not_configured')
      this.resetMonitorState()
      return
    }
    const scopeKey = `${workspaceId}\u0000${outcomeId}`
    if (this.outcomeScopeKey && this.outcomeScopeKey !== scopeKey) {
      this.outcomeDefinition = undefined
      this.outcomeEvaluations = []
      this.outcomeCorrections = []
      this.latestOutcomeLifeGraphProjection = undefined
      this.outcomeLifeGraphProjectionWarning = ''
      this.advisoryState.outcomes = this.newAdvisoryState('idle')
      this.resetMonitorState()
    }
    this.outcomeScopeKey = scopeKey
    if (!this.shouldLoadAdvisory('outcomes', force)) return
    this.beginAdvisoryLoad('outcomes')
    this.advisorySubscriptions.outcomes?.unsubscribe()
    this.advisorySubscriptions.outcomes = forkJoin({
      definition: this.service.outcomeDefinition(workspaceId, outcomeId).pipe(
        catchError((error: HttpErrorResponse) =>
          error.status === 404 ? of(undefined) : throwError(() => error)
        )
      ),
      evaluations: this.service.listOutcomeEvaluations(workspaceId, outcomeId).pipe(
        catchError((error: HttpErrorResponse) =>
          error.status === 404 ? of({ evaluations: [] }) : throwError(() => error)
        )
      ),
      corrections: this.service.listOutcomeCorrections(workspaceId, outcomeId).pipe(
        catchError((error: HttpErrorResponse) =>
          error.status === 404 ? of({ corrections: [] }) : throwError(() => error)
        )
      ),
    }).subscribe({
      next: ({ definition, evaluations, corrections }) => {
        this.outcomeDefinition = definition
        this.outcomeEvaluations = evaluations.evaluations || []
        this.outcomeCorrections = corrections.corrections || []
        if (definition) this.syncOutcomeForm(definition)
        this.finishAdvisoryLoad(
          'outcomes',
          (definition ? 1 : 0) + this.outcomeEvaluations.length + this.outcomeCorrections.length
        )
        this.loadAmbientMonitor(true)
      },
      error: (error) => this.failAdvisoryLoad(
        'outcomes', error, 'Outcome evidence could not be loaded for this scope.'
      ),
    })
  }

  loadAmbientMonitor(force = false): void {
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!workspaceId || !outcomeId) {
      this.resetMonitorState()
      return
    }
    if (!force && (this.monitorState.loading || this.monitorState.loaded)) return

    const scopeKey = this.monitorScopeKey(workspaceId, outcomeId)
    this.monitorSubscription?.unsubscribe()
    this.monitorHistorySubscription?.unsubscribe()
    this.monitorCompositionSubscription?.unsubscribe()
    this.monitorCompositionHistorySubscription?.unsubscribe()
    this.monitorState = { ...this.monitorState, loading: true, error: '', historyError: '' }
    this.monitorSubscription = this.ambientMonitor.getMonitor(workspaceId, outcomeId).subscribe({
      next: (response) => {
        if (scopeKey !== this.monitorScopeKey()) return
        this.monitorTargets = response.targets || []
        const retained = this.monitorTargets.some((target) => target.id === this.selectedMonitorTargetId)
        this.selectedMonitorTargetId = retained ? this.selectedMonitorTargetId : (this.monitorTargets[0]?.id || '')
        this.monitorState = {
          ...this.monitorState,
          loading: false,
          loaded: true,
          error: '',
          loadedAt: new Date().toISOString(),
        }
        this.syncMonitorFormDefaults()
        if (this.selectedMonitorTargetId) {
          this.loadMonitorHistory(true)
          this.loadMonitorCompositions(true)
        }
        else {
          this.monitorObservations = []
          this.monitorRuns = []
          this.monitorCompositions = []
          this.monitorCompositionAttempts = []
          this.monitorCompositionState = this.newMonitorCompositionState()
        }
      },
      error: (error: HttpErrorResponse) => {
        if (scopeKey !== this.monitorScopeKey()) return
        if (error.status === 404) {
          this.monitorTargets = []
          this.selectedMonitorTargetId = ''
          this.monitorObservations = []
          this.monitorRuns = []
          this.monitorCompositions = []
          this.monitorCompositionAttempts = []
          this.monitorCompositionState = this.newMonitorCompositionState()
          this.monitorState = {
            ...this.monitorState,
            loading: false,
            loaded: true,
            error: '',
            loadedAt: new Date().toISOString(),
          }
          this.syncMonitorFormDefaults()
          return
        }
        this.monitorState = {
          ...this.monitorState,
          loading: false,
          error: this.describeError(error, 'Ambient outcome monitoring could not be loaded.'),
        }
      },
    })
  }

  selectMonitorTarget(targetId: string): void {
    if (!this.monitorTargets.some((target) => target.id === targetId)) return
    this.selectedMonitorTargetId = targetId
    this.loadMonitorHistory(true)
    this.monitorCompositions = []
    this.monitorCompositionAttempts = []
    this.monitorCompositionState = this.newMonitorCompositionState()
    this.loadMonitorCompositions(true)
  }

  loadMonitorCompositions(force = false): void {
    const target = this.selectedMonitorTarget
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!target || !workspaceId || !outcomeId) return
    if (!force && (this.monitorCompositionState.loading || this.monitorCompositionState.loaded)) return

    const scopeKey = this.monitorScopeKey(workspaceId, outcomeId)
    const targetId = target.id
    const refreshOpenHistory =
      this.preferences.get(this.moduleId).openSections['outcome-monitor-composition'] === true
    this.monitorCompositionSubscription?.unsubscribe()
    this.monitorCompositionState = {
      ...this.monitorCompositionState,
      loading: true,
      error: '',
    }
    this.monitorCompositionSubscription = this.ambientMonitor
      .listCompositions(workspaceId, outcomeId, targetId, 25)
      .subscribe({
        next: (response) => {
          if (scopeKey !== this.monitorScopeKey() || targetId !== this.selectedMonitorTargetId) return
          this.monitorCompositions = response.compositions || []
          this.monitorCompositionAttempts = []
          this.monitorCompositionState = {
            ...this.monitorCompositionState,
            loading: false,
            loaded: true,
            historyLoaded: false,
            error: '',
            historyError: '',
          }
          if (refreshOpenHistory && this.monitorCompositions.length) {
            this.loadMonitorCompositionAttempts(true)
          }
        },
        error: (error) => {
          if (scopeKey !== this.monitorScopeKey() || targetId !== this.selectedMonitorTargetId) return
          this.monitorCompositionState = {
            ...this.monitorCompositionState,
            loading: false,
            loaded: false,
            error: this.describeError(error, 'Advisory handoff status could not be loaded.'),
          }
        },
      })
  }

  loadMonitorCompositionAttempts(force = false): void {
    const target = this.selectedMonitorTarget
    const composition = this.latestMonitorComposition
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!target || !composition || !workspaceId || !outcomeId) return
    if (!force && (this.monitorCompositionState.historyLoading || this.monitorCompositionState.historyLoaded)) return

    const scopeKey = this.monitorScopeKey(workspaceId, outcomeId)
    const targetId = target.id
    const deliveryId = composition.id
    this.monitorCompositionHistorySubscription?.unsubscribe()
    this.monitorCompositionState = {
      ...this.monitorCompositionState,
      historyLoading: true,
      historyError: '',
    }
    this.monitorCompositionHistorySubscription = this.ambientMonitor
      .listCompositionAttempts(workspaceId, outcomeId, targetId, deliveryId, 25)
      .subscribe({
        next: (response) => {
          if (
            scopeKey !== this.monitorScopeKey() ||
            targetId !== this.selectedMonitorTargetId ||
            deliveryId !== this.latestMonitorComposition?.id
          ) return
          this.monitorCompositionAttempts = response.attempts || []
          this.monitorCompositionState = {
            ...this.monitorCompositionState,
            historyLoading: false,
            historyLoaded: true,
            historyError: '',
          }
        },
        error: (error) => {
          if (scopeKey !== this.monitorScopeKey() || targetId !== this.selectedMonitorTargetId) return
          this.monitorCompositionState = {
            ...this.monitorCompositionState,
            historyLoading: false,
            historyError: this.describeError(error, 'Advisory handoff attempts could not be loaded.'),
          }
        },
      })
  }

  loadMonitorHistory(force = false): void {
    const target = this.selectedMonitorTarget
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!target || !workspaceId || !outcomeId) return
    if (!force && this.monitorState.historyLoading) return

    const scopeKey = this.monitorScopeKey(workspaceId, outcomeId)
    const targetId = target.id
    this.monitorHistorySubscription?.unsubscribe()
    this.monitorState = { ...this.monitorState, historyLoading: true, historyError: '' }
    this.monitorHistorySubscription = forkJoin({
      observations: this.ambientMonitor.listObservations(workspaceId, outcomeId, targetId, 50),
      runs: this.ambientMonitor.listRuns(workspaceId, outcomeId, targetId, 50),
    }).subscribe({
      next: ({ observations, runs }) => {
        if (scopeKey !== this.monitorScopeKey() || targetId !== this.selectedMonitorTargetId) return
        this.monitorObservations = observations.observations || []
        this.monitorRuns = runs.runs || []
        this.monitorState = { ...this.monitorState, historyLoading: false, historyError: '' }
      },
      error: (error) => {
        if (scopeKey !== this.monitorScopeKey() || targetId !== this.selectedMonitorTargetId) return
        this.monitorState = {
          ...this.monitorState,
          historyLoading: false,
          historyError: this.describeError(error, 'Monitor observations and runs could not be loaded.'),
        }
      },
    })
  }

  configureAmbientMonitor(): void {
    if (this.monitorMutating) return
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    const indicator = this.currentOutcomeIndicator
    const targetId = this.monitorForm.targetId.trim()
    const firstRunAt = this.isoDateTime(this.monitorForm.firstRunAt)
    const cadenceSeconds = Math.trunc(Number(this.monitorForm.cadenceSeconds))
    if (!workspaceId || !outcomeId || !this.outcomeDefinition || !indicator) {
      this.notification.warning('Load a measurable outcome', 'A stored outcome with a current indicator is required before monitoring can be configured.')
      return
    }
    if (!this.isCanonicalUuid(targetId) || !firstRunAt || !Number.isFinite(cadenceSeconds) || cadenceSeconds < 60 || cadenceSeconds > 2592000) {
      this.notification.warning('Check the monitor target', 'Use the generated target ID, a valid first run, and a cadence from 60 seconds to 30 days.')
      return
    }
    const windowStart = new Date(this.outcomeDefinition.outcome.window.start)
    const windowEnd = new Date(this.outcomeDefinition.outcome.window.end)
    const firstRun = new Date(firstRunAt)
    if (firstRun < windowStart || firstRun > windowEnd) {
      this.notification.warning('Check the first run', 'The first monitor run must fall inside the current outcome window.')
      return
    }

    this.monitorMutating = true
    this.ambientMonitor.registerTarget(workspaceId, outcomeId, {
      idempotencyKey: this.operationId('ambient-monitor-target'),
      targetId,
      indicatorId: indicator.id,
      sourceKind: this.monitorForm.sourceKind,
      enabled: this.monitorForm.enabled,
      cadenceSeconds,
      firstRunAt,
    }).subscribe({
      next: (response) => {
        this.monitorMutating = false
        this.selectedMonitorTargetId = response.target.id
        this.notification.success(
          response.created ? 'Monitor target created' : 'Existing monitor target confirmed',
          'The target can only observe and propose advisory attention; it cannot execute or deliver work.'
        )
        this.loadAmbientMonitor(true)
      },
      error: (error) => this.failMonitorMutation(error, 'The immutable monitor target was not stored.'),
    })
  }

  toggleAmbientMonitor(): void {
    if (this.monitorMutating) return
    const target = this.selectedMonitorTarget
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!target || !workspaceId || !outcomeId) return

    this.monitorMutating = true
    this.ambientMonitor.setEnabled(workspaceId, outcomeId, target.id, {
      idempotencyKey: this.operationId('ambient-monitor-enabled'),
      enabled: !target.enabled,
    }).subscribe({
      next: (response) => {
        this.monitorMutating = false
        this.monitorTargets = this.monitorTargets.map((value) =>
          value.id === response.target.id ? response.target : value
        )
        this.notification.success(
          response.target.enabled ? 'Ambient monitor enabled' : 'Ambient monitor paused',
          response.target.enabled
            ? 'Source-backed advisory observations can resume on the configured cadence.'
            : 'No further observations will be collected until this target is enabled.'
        )
      },
      error: (error) => this.failMonitorMutation(error, 'The monitor state was not changed.'),
    })
  }

  runDueAmbientMonitors(): void {
    if (this.monitorMutating) return
    const workspaceId = this.advisoryScope.workspaceId.trim()
    if (!workspaceId || !this.monitorTargets.length) return

    this.monitorMutating = true
    this.ambientMonitor.runDue(workspaceId, {
      workerId: 'governance-control-owner',
      asOf: new Date().toISOString(),
      leaseSeconds: 60,
      limit: 25,
    }).subscribe({
      next: (result) => {
        this.monitorMutating = false
        const completed = result.completions?.length || 0
        const failed = result.failures?.length || 0
        const compositionFailures = result.compositions?.failures || []
        const handoffSucceeded = result.compositions?.succeeded || 0
        const retrying = compositionFailures.filter((failure) => failure.retrying).length
        const needsReview = compositionFailures.length - retrying
        const summary = `${completed} observation${completed === 1 ? '' : 's'} recorded; ${failed} failed. No work was executed or delivered.`
        if (failed > 0) {
          const codes = [...new Set((result.failures || []).map((failure) => failure.code))].join(', ')
          const detail = `${summary} Review the run history and retry after recovery. Failure code${codes.includes(',') ? 's' : ''}: ${codes || 'monitor_failed'}.`
          if (completed > 0) {
            this.notification.warning('Due monitor pass partly completed', detail)
          } else {
            this.notification.error('Due monitor pass failed', detail)
          }
        } else if (needsReview > 0) {
          const codes = [...new Set(compositionFailures.map((failure) => failure.code))].join(', ')
          this.notification.error(
            'Advisory handoff needs review',
            `${summary} ${needsReview} handoff${needsReview === 1 ? '' : 's'} reached the retry limit. Failure code${codes.includes(',') ? 's' : ''}: ${codes || 'composition_failed'}.`
          )
        } else if (retrying > 0) {
          this.notification.warning(
            'Observation stored; advisory handoff will retry',
            `${summary} ${retrying} handoff${retrying === 1 ? '' : 's'} could not complete and remain queued for a bounded retry.`
          )
        } else {
          this.notification.success('Due monitor pass completed', summary)
        }
        // A completed pass can append an outcome evaluation and route a new
        // advisory attention decision, so refresh the whole visible chain.
        this.loadOutcomeEvidence(true, true)
        this.loadProactivity(true, true)
        this.loadMonitorCompositions(true)
      },
      error: (error) => this.failMonitorMutation(error, 'The due monitor pass did not complete.'),
    })
  }

  recoverAmbientMonitors(): void {
    if (this.monitorMutating) return
    const workspaceId = this.advisoryScope.workspaceId.trim()
    if (!workspaceId || !this.monitorTargets.length) return

    this.monitorMutating = true
    this.ambientMonitor.recover(workspaceId, new Date().toISOString()).subscribe({
      next: (result) => {
        this.monitorMutating = false
        if (result.recovered > 0) {
          this.notification.success(
            'Expired monitor work recovered',
            `${result.collectionRecovered} collection lease${result.collectionRecovered === 1 ? '' : 's'} and ${result.compositionRecovered} advisory handoff lease${result.compositionRecovered === 1 ? '' : 's'} returned to their durable queues.`
          )
        } else {
          this.notification.info(
            'No expired monitor work',
            'No expired collection or advisory handoff leases needed recovery.'
          )
        }
        this.loadAmbientMonitor(true)
      },
      error: (error) => this.failMonitorMutation(error, 'Expired monitor work could not be recovered.'),
    })
  }

  get selectedMonitorTarget(): MonitorTarget | undefined {
    return this.monitorTargets.find((target) => target.id === this.selectedMonitorTargetId)
  }

  get latestMonitorObservation(): ObservationRecord | undefined {
    return [...this.monitorObservations].sort((a, b) => Date.parse(b.observedAt) - Date.parse(a.observedAt))[0]
  }

  get latestMonitorRun(): MonitorRun | undefined {
    return [...this.monitorRuns].sort((a, b) => Date.parse(b.finishedAt) - Date.parse(a.finishedAt))[0]
  }

  get latestMonitorComposition(): MonitorCompositionDelivery | undefined {
    return [...this.monitorCompositions].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))[0]
  }

  get latestMonitorCompositionSnapshot(): MonitorCompositionSnapshot | undefined {
    return this.latestMonitorComposition?.snapshot
  }

  get monitorCompositionProvenanceLabel(): string {
    const snapshot = this.latestMonitorCompositionSnapshot
    if (!snapshot) return 'Snapshot provenance not recorded'
    const revision = snapshot.status === 'legacy_unpinned' || snapshot.outcomeRevision <= 0
      ? 'not pinned'
      : String(snapshot.outcomeRevision)
    return `Composer ${snapshot.composerVersion} / outcome revision ${revision}`
  }

  get monitorCompositionProvenanceDetails(): MonitorCompositionProvenanceDetail[] {
    const snapshot = this.latestMonitorCompositionSnapshot
    if (!snapshot) return []
    const attention = snapshot.attention
    const policyIdempotencyKey = snapshot.policyIdempotencyKey || attention?.policy.idempotencyKey
    const policyDigest = snapshot.policyDigest || attention?.policy.payloadDigest
    const policyRecordedAt = snapshot.policyRecordedAt || attention?.policy.recordedAt
    const signalWatermark = snapshot.signalWatermark ?? attention?.signals
    const decisionWatermark = snapshot.decisionWatermark ?? attention?.decisions
    const feedbackWatermark = snapshot.feedbackWatermark ?? attention?.feedback
    return [
      this.compositionProvenanceDetail('contractVersion', 'Snapshot contract', snapshot.contractVersion),
      this.compositionProvenanceDetail('snapshotStatus', 'Snapshot status', snapshot.status),
      this.compositionProvenanceDetail('composerVersion', 'Composer version', snapshot.composerVersion),
      this.compositionProvenanceDetail('snapshotCapturedAt', 'Snapshot captured at', snapshot.capturedAt),
      this.compositionProvenanceDetail(
        'outcomeRevision',
        'Outcome revision',
        snapshot.status === 'legacy_unpinned' || snapshot.outcomeRevision <= 0 ? 'Not pinned' : snapshot.outcomeRevision
      ),
      this.compositionProvenanceDetail('outcomeAuditDigest', 'Outcome audit digest', snapshot.outcomeAuditDigest, true),
      this.compositionProvenanceDetail('contextCutoff', 'Context cutoff', snapshot.contextCutoff || attention?.capturedAt || snapshot.capturedAt),
      this.compositionProvenanceDetail('policyIdempotencyKey', 'Policy idempotency key', policyIdempotencyKey),
      this.compositionProvenanceDetail('policyDigest', 'Policy digest', policyDigest, true),
      this.compositionProvenanceDetail('policyRecordedAt', 'Policy recorded at', policyRecordedAt),
      ...this.compositionWatermarkDetails('signalWatermark', 'Signal watermark', signalWatermark),
      ...this.compositionWatermarkDetails('decisionWatermark', 'Decision watermark', decisionWatermark),
      ...this.compositionWatermarkDetails('feedbackWatermark', 'Feedback watermark', feedbackWatermark),
      this.compositionProvenanceDetail('attentionInputDigest', 'Attention input digest', attention?.inputDigest, true),
      this.compositionProvenanceDetail('snapshotDigest', 'Snapshot digest', snapshot.snapshotDigest, true),
    ]
  }

  get monitorCompositionTone(): 'ready' | 'stale' | 'failed' | 'not_configured' {
    if (this.monitorCompositionState.error) return 'stale'
    const composition = this.latestMonitorComposition
    if (!composition) return 'not_configured'
    if (composition.status === 'succeeded') return 'ready'
    if (composition.status === 'dead_lettered') return 'failed'
    return 'stale'
  }

  get monitorCompositionLabel(): string {
    if (this.monitorCompositionState.loading && !this.monitorCompositionState.loaded) return 'Loading'
    if (this.monitorCompositionState.error) return 'Status unavailable'
    const composition = this.latestMonitorComposition
    if (!composition) return this.latestMonitorRun?.status === 'completed' ? 'Handoff not recorded' : 'Not started'
    if (composition.status === 'succeeded') return 'Current'
    if (composition.status === 'dead_lettered') return 'Needs review'
    return composition.attemptCount > 0 ? 'Retry scheduled' : 'Pending'
  }

  get monitorCompositionDetail(): string {
    if (this.monitorCompositionState.error) return this.monitorCompositionState.error
    const composition = this.latestMonitorComposition
    if (!composition) return 'No advisory composition delivery exists for this target yet.'
    if (composition.status === 'succeeded') {
      return `Verified handoff completed ${this.formatDateTime(composition.completedAt || composition.updatedAt)}.`
    }
    if (composition.status === 'dead_lettered') {
      return `Retry limit reached after ${composition.attemptCount} attempt${composition.attemptCount === 1 ? '' : 's'} (${composition.lastFailureCode || 'composition_failed'}).`
    }
    if (composition.attemptCount > 0) {
      return `Attempt ${composition.attemptCount} failed (${composition.lastFailureCode || 'composition_failed'}); next bounded retry ${this.formatDateTime(composition.nextAttemptAt)}.`
    }
    return `Queued ${this.formatDateTime(composition.createdAt)} for advisory outcome and attention composition.`
  }

  formatDateTime(value?: string): string {
    if (!value) return 'at an unknown time'
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? 'at an unknown time' : parsed.toLocaleString()
  }

  get currentOutcomeIndicator(): OutcomeRevision['outcome']['indicators'][number] | undefined {
    const indicators = this.outcomeDefinition?.outcome.indicators || []
    return indicators.find((indicator) => indicator.id === this.outcomeObservationForm.indicatorId) || indicators[0]
  }

  monitorSourceLabel(value: MonitorSourceKind): string {
    return this.monitorSourceKinds.find((source) => source.value === value)?.label || this.label(value)
  }

  monitorSourceDescription(value: MonitorSourceKind): string {
    return this.monitorSourceKinds.find((source) => source.value === value)?.description || ''
  }

  monitorCadenceLabel(seconds: number): string {
    if (seconds % 86400 === 0) return `Every ${seconds / 86400} day${seconds === 86400 ? '' : 's'}`
    if (seconds % 3600 === 0) return `Every ${seconds / 3600} hour${seconds === 3600 ? '' : 's'}`
    if (seconds % 60 === 0) return `Every ${seconds / 60} minute${seconds === 60 ? '' : 's'}`
    return `Every ${seconds} seconds`
  }

  loadResilienceStatus(open = true, force = false): void {
    if (!open) return
    const workspaceId = this.advisoryScope.workspaceId.trim()
    if (!workspaceId) {
      this.advisoryState.resilience = this.newAdvisoryState('not_configured')
      return
    }
    if (this.resilienceScopeKey && this.resilienceScopeKey !== workspaceId) {
      this.resilience = undefined
      this.advisoryState.resilience = this.newAdvisoryState('idle')
    }
    this.resilienceScopeKey = workspaceId
    if (!this.shouldLoadAdvisory('resilience', force)) return
    this.beginAdvisoryLoad('resilience')
    this.advisorySubscriptions.resilience?.unsubscribe()
    this.advisorySubscriptions.resilience = this.service.resilienceStatus(workspaceId).subscribe({
      next: (status) => {
        this.resilience = status
        this.finishAdvisoryLoad('resilience', 1)
      },
      error: (error) => this.failAdvisoryLoad(
        'resilience', error, 'Resilience status could not be loaded for this workspace.'
      ),
    })
  }

  loadScopedAdvisoryEvidence(): void {
    this.loadOutcomeEvidence(true, true)
    this.loadResilienceStatus(true, true)
  }

  defineOutcome(): void {
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    const form = this.outcomeForm
    if (![workspaceId, outcomeId, form.statement, form.lifeDomain, form.windowStart, form.windowEnd,
      form.indicatorId, form.indicatorName, form.unit, form.baselineObservedAt]
      .every((value) => String(value).trim())) {
      this.notification.warning('Complete the outcome definition', 'Scope, statement, dates, and indicator fields are required.')
      return
    }
    const windowStart = this.parseOutcomeDate(form.windowStart)
    const windowEnd = this.parseOutcomeDate(form.windowEnd)
    const baselineObservedAt = this.parseOutcomeDate(form.baselineObservedAt)
    if (!windowStart || !windowEnd || !baselineObservedAt || windowStart >= windowEnd || baselineObservedAt > windowStart) {
      this.notification.warning('Check the outcome dates', 'The baseline must be on or before the window start, and the end must be later than the start.')
      return
    }
    const numbers = [form.targetValue, form.targetTolerance, form.trendThresholdPerDay,
      form.regressionThreshold, form.minimumObservations, form.baselineValue]
    if (!numbers.every(Number.isFinite) || form.targetTolerance < 0 || form.trendThresholdPerDay < 0 ||
      form.regressionThreshold <= 0 || form.minimumObservations < 2) {
      this.notification.warning('Check the indicator thresholds', 'Numeric values must be finite; regression must be positive and at least two observations are required.')
      return
    }
    this.mutating = true
    this.service.storeOutcome(workspaceId, outcomeId, {
      idempotencyKey: this.operationId('outcome-definition'),
      expectedRevision: this.outcomeDefinition?.revision || 0,
      outcome: {
        statement: form.statement.trim(),
        lifeDomain: form.lifeDomain as OutcomeLifeDomain,
        window: { start: windowStart.toISOString(), end: windowEnd.toISOString() },
        indicators: [{
          id: form.indicatorId.trim(),
          name: form.indicatorName.trim(),
          unit: form.unit.trim(),
          direction: form.direction,
          targetValue: form.targetValue,
          targetTolerance: form.targetTolerance,
          trendThresholdPerDay: form.trendThresholdPerDay,
          regressionThreshold: form.regressionThreshold,
          minimumObservations: Math.trunc(form.minimumObservations),
          baseline: {
            id: `${form.indicatorId.trim()}-baseline`,
            value: form.baselineValue,
            observedAt: baselineObservedAt.toISOString(),
            verification: 'user_confirmed',
            sources: [],
          },
        }],
      },
    }).subscribe({
      next: (definition) => {
        this.mutating = false
        this.outcomeDefinition = definition
        this.captureOutcomeProjection(definition)
        this.outcomeObservationForm.indicatorId = definition.outcome.indicators[0]?.id || ''
        this.notification.success('Outcome definition saved', `Revision ${definition.revision} is now the immutable current definition.`)
        this.loadOutcomeEvidence(true, true)
      },
      error: (error) => this.failMutation(error, 'The outcome definition was not stored.'),
    })
  }

  addOutcomeObservation(): void {
    const definition = this.outcomeDefinition
    const form = this.outcomeObservationForm
    if (!definition || !form.indicatorId.trim() || !form.observedAt || !form.rationale.trim()) {
      this.notification.warning('Complete the observation', 'An outcome, indicator, observation date, and rationale are required.')
      return
    }
    if (!definition.outcome.indicators.some((indicator) => indicator.id === form.indicatorId.trim())) {
      this.notification.warning('Select a current indicator', 'The observation must belong to the current outcome revision.')
      return
    }
    const observedAt = this.parseOutcomeDate(form.observedAt)
    const recordedAt = new Date()
    if (!observedAt || observedAt > recordedAt || !Number.isFinite(form.value)) {
      this.notification.warning('Check the observation', 'The value must be finite and the observation cannot be in the future.')
      return
    }
    this.outcomeObservationDrafts = [...this.outcomeObservationDrafts, {
      id: this.operationId('observation'),
      indicatorId: form.indicatorId.trim(),
      value: form.value,
      observedAt: observedAt.toISOString(),
      recordedAt: recordedAt.toISOString(),
      verification: 'user_confirmed',
      sources: [],
      attribution: {
        method: 'user_report',
        confidence: 1,
        rationale: form.rationale.trim(),
      },
    }]
    form.rationale = ''
  }

  removeOutcomeObservation(id: string): void {
    this.outcomeObservationDrafts = this.outcomeObservationDrafts.filter((value) => value.id !== id)
  }

  evaluateOutcome(): void {
    const definition = this.outcomeDefinition
    const workspaceId = this.advisoryScope.workspaceId.trim()
    const outcomeId = this.advisoryScope.outcomeId.trim()
    if (!definition || !workspaceId || !outcomeId || !this.outcomeObservationDrafts.length) {
      this.notification.warning('Add outcome evidence', 'Store an outcome definition and add at least one observation before evaluating it.')
      return
    }
    const asOf = new Date().toISOString()
    this.mutating = true
    this.service.createOutcomeEvaluation(workspaceId, outcomeId, {
      idempotencyKey: this.operationId('outcome-evaluation'),
      outcomeRevision: definition.revision,
      observations: this.outcomeObservationDrafts,
      corrections: [],
      asOf,
    }).subscribe({
      next: (record) => {
        this.mutating = false
        this.outcomeObservationDrafts = []
        this.captureOutcomeProjection(record)
        this.notification.success('Outcome evaluated', `${this.label(record.evaluation.state)}; recommendations remain advisory only.`)
        this.loadOutcomeEvidence(true, true)
      },
      error: (error) => this.failMutation(error, 'The outcome evidence was not evaluated.'),
    })
  }

  refreshAdvisory(surface: AdvisorySurface): void {
    switch (surface) {
      case 'teams': this.loadAgentTeams(true, true); break
      case 'life': this.loadLifeContext(true, true); break
      case 'ledger': this.loadLifeLedger(true, true); break
      case 'proactivity': this.loadProactivity(true, true); break
      case 'outcomes': this.loadOutcomeEvidence(true, true); break
      case 'resilience': this.loadResilienceStatus(true, true); break
    }
  }

  toggleLocalOnlyLifeContext(): void {
    this.includeLocalOnlyLifeContext = !this.includeLocalOnlyLifeContext
    this.loadLifeContext(true, true)
  }

  loadMandateDecisions(open: boolean, force = false): void {
    if (!open || (!force && (this.mandateDecisionState.loading || this.mandateDecisionState.loaded))) return
    this.mandateDecisionSubscription?.unsubscribe()
    this.mandateDecisionState = { loading: true, loaded: false, error: '' }
    this.mandateDecisionSubscription = this.service.listMandateDecisions(undefined, 100).subscribe({
      next: (response) => {
        this.mandateDecisions = response.decisions || []
        this.mandateDecisionState = this.completedState()
      },
      error: (error) => {
        this.mandateDecisionState = {
          loading: false,
          loaded: false,
          error: this.describeError(error, 'Standing-mandate decisions could not be loaded.'),
        }
      },
    })
  }

  classify(): void {
    const text = this.classifierText.trim()
    if (!text) {
      this.notification.warning('Describe the work', 'Classification needs a concrete task or situation.')
      return
    }
    this.classifying = true
    this.service.classifyDomain(text).subscribe({
      next: (result) => {
        this.classification = result
        this.classifying = false
      },
      error: (error) => {
        this.classifying = false
        this.notification.error(
          'Classification failed',
          this.describeError(error, 'The domain-pack classifier did not return a result.')
        )
      },
    })
  }

  openSurface(kind: GovernanceSurface): void {
    this.ensureSurfaceLoaded(kind)
    this.resetInspector(kind)
    this.inspectorVisible = true
  }

  openReceipt(receipt: ExecutionAuthorizationSummary): void {
    this.resetInspector('receipt')
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = forkJoin({
      receipt: this.service.executionReceipt(receipt.id),
      consumption: this.service.executionConsumption(receipt.id).pipe(
        catchError((error: HttpErrorResponse) =>
          error.status === 404 ? of(null) : throwError(() => error)
        )
      ),
    }).subscribe({
      next: ({ receipt: detail, consumption }) => {
        this.selectedReceipt = detail
        this.selectedConsumption = consumption
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Receipt evidence could not be loaded.'),
    })
  }

  openMandate(mandate: StandingMandate): void {
    this.resetInspector('mandate')
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = forkJoin({
      mandate: this.service.mandate(mandate.id),
      decisions: this.service.listMandateDecisions(mandate.id, 100),
    }).subscribe({
      next: ({ mandate: detail, decisions }) => {
        this.selectedMandate = detail
        this.selectedMandateDecisions = decisions.decisions || []
        this.revokeReason = ''
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Mandate detail could not be loaded.'),
    })
  }

  openProposal(
    proposal: LearningProposal,
    application?: LearningApplicationSummary
  ): void {
    this.resetInspector('proposal')
    this.selectedLearningApplication = application
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = forkJoin({
      proposal: this.service.learningProposal(proposal.id),
      decisions: this.service.learningDecisions(proposal.id, 100),
    }).subscribe({
      next: ({ proposal: detail, decisions }) => {
        this.selectedProposal = detail
        this.selectedLearningDecisions = decisions.decisions || []
        this.learningRationale = ''
        this.learningGovernanceReference = ''
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Learning proposal detail could not be loaded.'),
    })
  }

  openAgent(agent: AgentRecord): void {
    this.resetInspector('agent')
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = forkJoin({
      agent: this.service.agent(agent.id),
      transitions: this.service.agentTransitions(agent.id),
    }).subscribe({
      next: ({ agent: detail, transitions }) => {
        this.selectedAgent = detail
        this.selectedAgentTransitions = transitions.transitions || []
        this.transitionReason = ''
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Agent detail could not be loaded.'),
    })
  }

  openDomain(view: DomainPackSummaryView): void {
    this.resetInspector('domain')
    this.inspectorLoading = true
    this.inspectorVisible = true
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = this.service.effectiveDomainPack(view.pack.id).subscribe({
      next: (detail) => {
        this.selectedDomain = detail
        this.domainPreferenceForm = {
          enabled: detail.enabled,
          status: detail.preference?.status || 'active',
          classificationBoost: detail.preference?.classificationBoost ?? 0,
          forceLocalOnly: detail.localOnly,
          notes: detail.preference?.adaptation.notes || '',
        }
        this.inspectorLoading = false
      },
      error: (error) => this.failInspector(error, 'Effective domain-pack detail could not be loaded.'),
    })
  }

  domainById(id: string): DomainPackSummaryView | undefined {
    return this.domainPacks.find((view) => view.pack.id === id)
  }

  openMatchedDomain(packId: string): void {
    const domain = this.domainById(packId)
    if (domain) this.openDomain(domain)
  }

  closeInspector(): void {
    this.inspectorVisible = false
    this.inspectorSubscription?.unsubscribe()
    this.inspectorSubscription = undefined
    this.inspectorLoading = false
  }

  retryFailedSources(): void {
    this.surfaces.filter((surface) => !!this.state[surface].error)
      .forEach((surface) => this.loadSurface(surface))
    this.advisorySurfaces
      .filter((surface) => ['stale', 'unavailable', 'error'].includes(
        this.advisoryState[surface].availability
      ))
      .forEach((surface) => this.refreshAdvisory(surface))
  }

  get advisoryHasProblems(): boolean {
    return this.advisorySurfaces.some((surface) =>
      ['stale', 'unavailable', 'error'].includes(this.advisoryState[surface].availability)
    )
  }

  activateMandate(mandate: StandingMandate): void {
    this.modal.confirm({
      nzTitle: `Activate ${mandate.name}?`,
      nzContent: 'This enables the bounded authority in this exact mandate revision. Execution remains subject to Constitution, risk, and approval checks.',
      nzOkText: 'Activate mandate',
      nzOnOk: () => this.performMandateActivation(mandate),
    })
  }

  revokeMandate(): void {
    const mandate = this.selectedMandate
    const reason = this.revokeReason.trim()
    if (!mandate || !reason) {
      this.notification.warning('Revocation reason required', 'State why this authority must stop.')
      return
    }
    this.modal.confirm({
      nzTitle: `Revoke ${mandate.name}?`,
      nzContent: 'Revocation is durable and preserves the decision history.',
      nzOkDanger: true,
      nzOkText: 'Revoke mandate',
      nzOnOk: () => this.performMandateRevocation(mandate, reason),
    })
  }

  decideProposal(kind: LearningDecisionKind): void {
    const proposal = this.selectedProposal
    const rationale = this.learningRationale.trim()
    if (!proposal || !rationale) {
      this.notification.warning('Rationale required', 'Record why this learning change should proceed or stop.')
      return
    }
    if (proposal.protectedTarget && kind === 'approve' && !this.learningGovernanceReference.trim()) {
      this.notification.warning(
        'Governance reference required',
        'Protected learning targets need an explicit governance record.'
      )
      return
    }
    this.mutating = true
    this.service.decideLearningProposal(proposal.id, {
      expectedRevision: proposal.revision,
      kind,
      humanConfirmed: true,
      rationale,
      governanceReference: this.learningGovernanceReference.trim() || undefined,
    }).subscribe({
      next: (result) => {
        this.mutating = false
        this.selectedProposal = result.proposal
        this.selectedLearningApplication = result.application
        this.notification.success(
          'Review recorded',
          this.learningApplicationStatus(result.proposal, result.application)
        )
        this.loadLearning()
        this.openProposal(result.proposal, result.application)
      },
      error: (error) => this.failMutation(error, 'The learning review was not recorded.'),
    })
  }

  transitionAgent(to: AgentLifecycleState): void {
    const agent = this.selectedAgent
    const reason = this.transitionReason.trim()
    if (!agent || !reason) {
      this.notification.warning('Reason required', 'Explain the lifecycle transition for the audit record.')
      return
    }
    this.mutating = true
    this.service.transitionAgent(agent.id, agent.revision, to, reason).subscribe({
      next: (updated) => {
        this.mutating = false
        this.selectedAgent = updated
        this.notification.success('Agent state updated', `${updated.name} is now ${this.label(updated.state)}.`)
        this.loadAgents()
        this.openAgent(updated)
      },
      error: (error) => this.failMutation(error, 'The agent lifecycle state was not changed.'),
    })
  }

  saveDomainPreference(): void {
    const domain = this.selectedDomain
    if (!domain) return
    const request: UpdateDomainPreferenceRequest = {
      expectedRevision: domain.preference?.revision || 0,
      status: this.domainPreferenceForm.status,
      enabled: this.domainPreferenceForm.enabled,
      classificationBoost: this.domainPreferenceForm.classificationBoost,
      forceLocalOnly: this.domainPreferenceForm.forceLocalOnly,
      adaptation: { notes: this.domainPreferenceForm.notes.trim() || undefined },
    }
    this.mutating = true
    this.service.updateDomainPreference(domain.pack.id, request).subscribe({
      next: () => {
        this.mutating = false
        this.notification.success('Domain preference saved', 'The effective pack will be reloaded from the owner-scoped record.')
        this.loadDomains()
        this.openDomain(domain)
      },
      error: (error) => this.failMutation(error, 'The domain-pack preference was not changed.'),
    })
  }

  createMandate(): void {
    const form = this.mandateForm
    if (![form.name, form.purpose, form.action, form.resourceType].every((value) => value.trim())) {
      this.notification.warning('Complete the mandate', 'Name, purpose, action, and resource type are required.')
      return
    }
    this.mutating = true
    this.service.createMandate({
      name: form.name.trim(),
      purpose: form.purpose.trim(),
      version: '1.0.0',
      scopes: [{
        id: 'primary-scope',
        actions: this.splitValues(form.action),
        resources: [{ type: form.resourceType.trim() }],
        maximumRisk: form.maximumRisk,
      }],
      autonomyCeiling: form.autonomyCeiling,
      approvalPolicy: { mode: form.approvalMode },
      sourceReferences: this.splitValues(form.sourceReference),
    }).subscribe({
      next: (created) => {
        this.mutating = false
        this.notification.success('Draft mandate created', 'Review its exact scope before activation.')
        this.mandateForm = {
          name: '', purpose: '', action: '', resourceType: '', maximumRisk: 'low',
          autonomyCeiling: 3, approvalMode: 'always', sourceReference: '',
        }
        this.loadMandates()
        this.openMandate(created)
      },
      error: (error) => this.failMutation(error, 'The mandate draft was not created.'),
    })
  }

  authorizeMandateAction(): void {
    const form = this.mandateAuthorizationForm
    if (![form.mandateId, form.action, form.resourceType].every((value) => value.trim())) {
      this.notification.warning('Complete the decision request', 'Mandate, action, and resource type are required.')
      return
    }
    this.mutating = true
    this.service.authorizeMandate(form.mandateId, {
      action: form.action.trim(),
      resourceType: form.resourceType.trim(),
      resourceId: form.resourceId.trim() || undefined,
      projectKey: form.projectKey.trim() || undefined,
      domain: form.domain.trim() || undefined,
      toolId: form.toolId.trim() || undefined,
      risk: form.risk,
      requestedAutonomy: form.requestedAutonomy,
      upstreamApprovalRequired: form.upstreamApprovalRequired,
      requestedAt: new Date().toISOString(),
    }).subscribe({
      next: (decision) => {
        this.mutating = false
        this.lastMandateDecision = decision
        this.notification.success('Decision evaluated', `${this.label(decision.outcome)}: ${decision.reason}`)
        this.loadMandateDecisions(true)
      },
      error: (error) => this.failMutation(error, 'The authorization decision was not evaluated.'),
    })
  }

  registerAgent(): void {
    const form = this.agentForm
    if (![form.id, form.name, form.runtimeId, form.capabilityId].every((value) => value.trim())) {
      this.notification.warning('Complete the agent record', 'ID, name, runtime, and capability are required.')
      return
    }
    this.mutating = true
    this.service.registerAgent({
      id: form.id.trim(),
      name: form.name.trim(),
      type: form.type,
      runtime: {
        id: form.runtimeId.trim(),
        type: form.runtimeType.trim(),
        protocolVersion: form.protocolVersion.trim(),
      },
      capabilities: [{
        id: form.capabilityId.trim(),
        version: form.capabilityVersion.trim(),
      }],
      authorityCeiling: form.authorityCeiling,
      autonomyCeiling: form.autonomyCeiling,
      health: {
        status: 'unknown',
        ready: false,
        reason: 'Awaiting runtime health evidence',
        checkedAt: new Date().toISOString(),
        freshFor: 0,
      },
      availability: {
        available: false,
        activeAssignments: 0,
        maxConcurrent: 1,
      },
      performance: {
        estimatedCostEur: 0,
        p95LatencyMs: 0,
        locality: 'local',
      },
    }).subscribe({
      next: (created) => {
        this.mutating = false
        this.notification.success('Agent registered', 'It remains disabled from assignment until health and lifecycle evidence allow it.')
        this.agentForm.id = ''
        this.agentForm.name = ''
        this.loadAgents()
        this.openAgent(created)
      },
      error: (error) => this.failMutation(error, 'The agent record was not created.'),
    })
  }

  assignAgent(): void {
    const form = this.assignmentForm
    if (!form.taskId.trim() || !form.capabilityId.trim()) {
      this.notification.warning('Complete the assignment', 'Task ID and required capability are required.')
      return
    }
    this.mutating = true
    this.service.assignAgent({
      taskId: form.taskId.trim(),
      capabilities: [{
        id: form.capabilityId.trim(),
        minVersion: form.minVersion.trim() || undefined,
      }],
      compatibility: {},
      requiredAuthority: form.requiredAuthority,
      requiredAutonomy: form.requiredAutonomy,
      policyMaxAuthority: form.policyMaxAuthority,
      policyMaxAutonomy: form.policyMaxAutonomy,
      requireLocal: form.requireLocal,
      allowDegraded: form.allowDegraded,
    }).subscribe({
      next: (assignment) => {
        this.mutating = false
        this.lastAssignment = assignment
        this.notification.success('Assignment created', `${assignment.agentId} was selected from durable registry evidence.`)
        this.loadAgents()
      },
      error: (error) => this.failMutation(error, 'No assignment was created.'),
    })
  }

  get attention(): GovernanceAttention[] {
    const items: GovernanceAttention[] = []
    this.proposals
      .filter((proposal) => ['review_required', 'governance_required', 'governance_review'].includes(proposal.status))
      .forEach((proposal) => items.push({
        id: proposal.id,
        kind: 'proposal',
        title: proposal.title,
        summary: proposal.protectedTarget ? 'Protected change needs governance review.' : 'Learning change needs a human decision.',
        tone: proposal.protectedTarget ? 'red' : 'gold',
        owner: 'Robert',
      }))
    this.mandates
      .filter((mandate) => mandate.status === 'draft')
      .forEach((mandate) => items.push({
        id: mandate.id,
        kind: 'mandate',
        title: mandate.name,
        summary: 'Draft authority is inactive until explicitly reviewed and activated.',
        tone: 'gold',
        owner: 'Robert',
      }))
    this.receipts
      .filter((receipt) => receipt.outcome !== 'authorized')
      .slice(0, 5)
      .forEach((receipt) => items.push({
        id: receipt.id,
        kind: 'receipt',
        title: `${this.label(receipt.outcome)}: ${receipt.action}`,
        summary: receipt.reason,
        tone: receipt.outcome === 'denied' ? 'red' : 'gold',
        owner: receipt.outcome === 'requires_approval' ? 'Robert' : 'HAI',
      }))
    this.agents
      .filter((agent) => agent.state === 'quarantined' || agent.health.status === 'unhealthy')
      .forEach((agent) => items.push({
        id: agent.id,
        kind: 'agent',
        title: agent.name,
        summary: agent.health.reason || `${this.label(agent.state)} agent requires review.`,
        tone: 'red',
        owner: 'HAI',
      }))
    return items.slice(0, 8)
  }

  get attentionLoading(): boolean {
    return this.attentionSurfaces.some((surface) =>
      this.state[surface].loading || (!this.state[surface].loaded && !this.state[surface].error)
    )
  }

  get attentionIncomplete(): boolean {
    return this.attentionSurfaces.some((surface) => !!this.state[surface].error)
  }

  get refreshing(): boolean {
    return this.surfaces.some((surface) => this.state[surface].loading) ||
      this.advisorySurfaces.some((surface) => this.advisoryState[surface].loading)
  }

  advisorySummary(surface: AdvisorySurface): string {
    const state = this.advisoryState[surface]
    switch (state.availability) {
      case 'not_configured': return 'Not configured - enter the real scope to inspect records.'
      case 'idle': return 'Not checked yet.'
      case 'loading': return state.loaded ? 'Refreshing source-backed records.' : 'Loading source-backed records.'
      case 'stale': return `Stale - ${state.error}`
      case 'unavailable': return `Unavailable - ${state.error}`
      case 'error': return `Error - ${state.error}`
      case 'empty': return 'Connected, but no records were returned.'
      case 'ready': return `${this.advisoryCount(surface)} source-backed record${this.advisoryCount(surface) === 1 ? '' : 's'}.`
    }
  }

  advisoryCount(surface: AdvisorySurface): number {
    switch (surface) {
      case 'teams': return this.agentTeams.length
      case 'life': return this.lifeEntities.length + this.lifeRelations.length + this.lifeMergeProposals.length
      case 'ledger': return this.lifeCommitments.length + this.lifeCosts.length
      case 'proactivity':
        return (this.proactivityPolicy ? 1 : 0) + this.proactivitySignals.length + this.proactivityDecisions.length + this.proactivityFeedback.length
      case 'outcomes':
        return (this.outcomeDefinition ? 1 : 0) + this.outcomeEvaluations.length + this.outcomeCorrections.length
      case 'resilience': return this.resilience ? 1 : 0
    }
  }

  advisoryStatusLabel(surface: AdvisorySurface): string {
    return this.label(this.advisoryState[surface].availability)
  }

  advisoryFreshness(surface: AdvisorySurface): string {
    const loadedAt = this.advisoryState[surface].loadedAt
    return loadedAt ? `API checked ${new Date(loadedAt).toLocaleString()}` : 'No successful API check recorded'
  }

  get activeAgentTeams(): AgentTeamContract[] {
    return this.agentTeams.filter((team) => team.status === 'active')
  }

  agentTeamAttentionItems(team: AgentTeamContract): AgentTeamMessageAttention[] {
    return this.agentTeamAttention[this.agentTeamAttentionKey(team)]?.messages || []
  }

  agentTeamReviewItems(team: AgentTeamContract): AgentTeamMessageAttention[] {
    return this.agentTeamAttentionItems(team).filter((message) => message.humanReviewRequired)
  }

  get agentTeamReviewCount(): number {
    return this.agentTeams.reduce((total, team) => total + this.agentTeamReviewItems(team).length, 0)
  }

  private agentTeamAttentionKey(team: AgentTeamContract): string {
    return `${team.id}:${team.version}`
  }

  get openLifeEntities(): LifeOntologyEntity[] {
    return this.lifeEntities.filter((entity) => !['completed', 'archived', 'resolved'].includes(entity.status))
  }

  get operationalLifeEntities(): LifeOntologyEntity[] {
    const operationalTypes = new Set([
      'source', 'document', 'pursuit', 'workflow', 'task', 'memory', 'commitment', 'cost', 'outcome',
    ])
    return this.lifeEntities.filter((entity) => operationalTypes.has(entity.type))
  }

  get pendingLifeMergeProposals(): LifeOntologyMergeProposal[] {
    const resolved = new Set(
      this.contactReviewDecisions
        .filter((decision) => decision.subject === 'merge_proposal')
        .map((decision) => decision.subjectId)
    )
    return this.lifeMergeProposals.filter((proposal) => !resolved.has(proposal.id))
  }

  get pendingContactCandidates(): LifeOntologyEntity[] {
    const resolved = new Set(
      this.contactReviewDecisions.flatMap((decision) => decision.candidateEntityIds || [])
    )
    return this.contactCandidateEntities.filter((entity) =>
      entity.type === 'person' &&
      entity.attributes?.['candidate'] === 'true' &&
      entity.verificationStatus === 'needs_review' &&
      !resolved.has(entity.id)
    )
  }

  contactCandidatesForProposal(proposal: LifeOntologyMergeProposal): LifeOntologyEntity[] {
    const ids = new Set(proposal.candidateEntityIds)
    return this.contactCandidateEntities.filter((entity) => ids.has(entity.id))
  }

  get openLifeCommitments(): LifeCommitmentRevision[] {
    return this.lifeCommitments.filter((commitment) =>
      ['proposed', 'active', 'waiting', 'disputed'].includes(commitment.status)
    )
  }

  lifeCostsByKind(kind: LifeCostKind): LifeCostEntry[] {
    return this.lifeCosts.filter((cost) => cost.kind === kind)
  }

  costVerificationOptions(kind: LifeCostKind): LifeLedgerVerificationStatus[] {
    if (kind === 'estimate') {
      return ['needs_review', 'source_supported', 'human_confirmed', 'verified']
    }
    if (kind === 'incurred') {
      return ['source_supported', 'human_confirmed', 'verified']
    }
    return ['human_confirmed', 'verified']
  }

  normalizeCostVerification(): void {
    const allowed = this.costVerificationOptions(this.costForm.kind)
    if (!allowed.includes(this.costForm.verification)) this.costForm.verification = allowed[0]
  }

  lifeCostTotals(kind: LifeCostKind): LifeCostTotal[] {
    const totals = new Map<string, number>()
    this.lifeCostsByKind(kind).forEach((cost) => {
      totals.set(cost.currency, (totals.get(cost.currency) || 0) + cost.amountMinor)
    })
    return [...totals.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([currency, amountMinor]) => ({ currency, amountMinor }))
  }

  formatLifeCost(amountMinor: number, currency: string): string {
    try {
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency,
        currencyDisplay: 'code',
      }).format(amountMinor / 100)
    } catch {
      return `${currency} ${(amountMinor / 100).toFixed(2)}`
    }
  }

  get proactivityReviewDecisions(): ProactivityDecisionRecord[] {
    return this.proactivityDecisions.filter((record) => record.decision.outcome === 'require_review')
  }

  latestProactivityFeedback(openLoopKey: string): ProactivityFeedbackRecord | undefined {
    return this.proactivityFeedback.find((record) => record.openLoopKey === openLoopKey)
  }

  recordProactivityFeedback(record: ProactivityDecisionRecord, action: ProactivityFeedbackAction): void {
    if (this.mutating) return
    const decision = record.decision
    if (!decision.signalDigest) {
      this.notification.warning('Refresh the advisory decision', 'The current decision has no source digest and cannot receive auditable feedback.')
      return
    }
    const submit = () => this.submitProactivityFeedback(record, action)
    if (action === 'suppress') {
      this.modal.confirm({
        nzTitle: 'Suppress this open loop?',
        nzContent: 'HAI will stop surfacing this open loop until you explicitly resume it. This changes attention policy only and executes no task.',
        nzOkText: 'Suppress',
        nzOkDanger: true,
        nzCancelText: 'Cancel',
        nzOnOk: submit,
      })
      return
    }
    submit()
  }

  private submitProactivityFeedback(record: ProactivityDecisionRecord, action: ProactivityFeedbackAction): void {
    const decision = record.decision
    const reason: Record<ProactivityFeedbackAction, string> = {
      accept: 'Owner accepted this attention recommendation in Governance Control.',
      dismiss: 'Owner dismissed this exact signal revision in Governance Control.',
      snooze: 'Owner snoozed this open loop for 24 hours in Governance Control.',
      suppress: 'Owner suppressed future interruptions for this open loop in Governance Control.',
      resume: 'Owner resumed proactive attention for this open loop in Governance Control.',
    }
    this.mutating = true
    this.service.recordProactivityFeedback({
      idempotencyKey: this.operationId(`proactivity-${action}`),
      signalId: decision.signalId,
      openLoopKey: decision.openLoopKey,
      signalDigest: decision.signalDigest,
      action,
      reason: reason[action],
      ...(action === 'snooze' ? { snoozedUntil: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString() } : {}),
    }).subscribe({
      next: (feedback) => {
        this.mutating = false
        this.notification.success(
          `${this.label(feedback.action)} recorded`,
          'The immutable attention record was saved. It cannot deliver a notification or execute work.'
        )
        this.loadProactivity(true, true)
      },
      error: (error) => this.failMutation(error, 'The attention preference was not recorded.'),
    })
  }

  get latestOutcomeEvaluation(): OutcomeEvaluationRecord | undefined {
    return this.outcomeEvaluations[0]
  }

  allowedAgentTransitions(agent: AgentRecord): AgentLifecycleState[] {
    const transitions: Record<AgentLifecycleState, AgentLifecycleState[]> = {
      registered: ['enabled', 'disabled', 'quarantined'],
      enabled: ['draining', 'disabled', 'quarantined'],
      draining: ['enabled', 'disabled', 'quarantined'],
      disabled: ['enabled', 'quarantined'],
      quarantined: ['disabled'],
    }
    return transitions[agent.state] || []
  }

  canEnableAgent(agent: AgentRecord): boolean {
    if (!agent.health.ready || agent.health.status !== 'healthy' || !agent.availability.available) return false
    const checkedAt = Date.parse(agent.health.checkedAt)
    return Number.isFinite(checkedAt) && agent.health.freshFor > 0 &&
      checkedAt + agent.health.freshFor / 1_000_000 >= Date.now()
  }

  agentTransitionDisabled(agent: AgentRecord, state: AgentLifecycleState): boolean {
    return this.mutating || (state === 'enabled' && !this.canEnableAgent(agent))
  }

  openAttention(item: GovernanceAttention): void {
    if (item.kind === 'proposal') {
      const proposal = this.proposals.find((value) => value.id === item.id)
      if (proposal) this.openProposal(proposal)
      return
    }
    if (item.kind === 'mandate') {
      const mandate = this.mandates.find((value) => value.id === item.id)
      if (mandate) this.openMandate(mandate)
      return
    }
    if (item.kind === 'receipt') {
      const receipt = this.receipts.find((value) => value.id === item.id)
      if (receipt) this.openReceipt(receipt)
      return
    }
    const agent = this.agents.find((value) => value.id === item.id)
    if (agent) this.openAgent(agent)
  }

  surfaceCount(surface: GovernanceSurface): number | undefined {
    if (!this.state[surface].loaded) return undefined
    switch (surface) {
      case 'execution': return this.receipts.length
      case 'mandates': return this.mandates.length
      case 'learning': return this.proposals.length
      case 'agents': return this.agents.length
      case 'domains': return this.domainPacks.length
    }
  }

  surfaceMetric(surface: GovernanceSurface): string {
    if (this.state[surface].error) return 'Unavailable'
    const count = this.surfaceCount(surface)
    if (count === undefined) return 'Loading'
    switch (surface) {
      case 'execution':
        return `${this.receipts.filter((value) => value.outcome !== 'authorized').length} need attention`
      case 'mandates':
        return `${this.mandates.filter((value) => value.status === 'active').length} active`
      case 'learning':
        return `${this.pendingProposals.length} awaiting review`
      case 'agents':
        return `${this.agents.filter((value) => value.state === 'enabled' && value.health.ready).length} ready`
      case 'domains':
        return `${this.domainPacks.filter((value) => value.enabled).length} enabled`
    }
  }

  surfaceHint(surface: GovernanceSurface): string {
    const current = this.state[surface]
    if (current.error) return `${this.surfaceTitle(surface)} unavailable. Open to inspect the error and retry.`
    if (current.loading || !current.loaded) return `${this.surfaceTitle(surface)} is loading from the backend.`
    return `${this.surfaceTitle(surface)} loaded ${current.loadedAt ? new Date(current.loadedAt).toLocaleTimeString() : 'from the backend'}.`
  }

  get pendingProposals(): LearningProposal[] {
    return this.proposals.filter((proposal) =>
      ['review_required', 'governance_required', 'governance_review'].includes(proposal.status)
    )
  }

  isProposalPending(proposal: LearningProposal): boolean {
    return ['review_required', 'governance_required', 'governance_review'].includes(
      proposal.status
    )
  }

  learningApplicationStatus(
    proposal: LearningProposal,
    application?: LearningApplicationSummary
  ): string {
    if (application) {
      switch (application.status) {
        case 'applied':
          return `Applied at ${application.appliedVersion || application.proposedVersion}; ${application.evidence.length} application evidence record(s).`
        case 'handoff_ready':
          return 'Protected change is ready for independent governance handoff; it has not been applied.'
        case 'applying':
          return 'Application is still running; HAI must not treat this proposal as complete.'
        case 'handoff_pending':
          return 'Independent governance handoff is still being prepared; the change is not applied.'
        case 'failed':
          return `Application failed${application.lastErrorCode ? ` (${application.lastErrorCode})` : ''}; the approved change is not effective.`
        case 'rollback_applying':
          return 'A rollback is in progress; effective state is not yet confirmed.'
        case 'rolled_back':
          return `Rolled back${application.restoredVersion ? ` to ${application.restoredVersion}` : ''}; rollback evidence remains available.`
        case 'rollback_failed':
          return `Rollback failed${application.lastErrorCode ? ` (${application.lastErrorCode})` : ''}; owner review is required.`
      }
    }
    switch (proposal.status) {
      case 'approved':
        return 'Approved, but no application record was returned; do not treat the change as applied.'
      case 'rejected':
        return 'Rejected and not applied.'
      case 'changes_requested':
        return 'Changes requested; not applied.'
      case 'governance_required':
      case 'governance_review':
        return 'Governance review required; not applied.'
      default:
        return 'Human review required; not applied.'
    }
  }

  inspectorTitle(): string {
    switch (this.inspectorKind) {
      case 'receipt': return 'Authorization receipt'
      case 'mandate': return this.selectedMandate?.name || 'Standing mandate'
      case 'proposal': return this.selectedProposal?.title || 'Learning proposal'
      case 'agent': return this.selectedAgent?.name || 'Agent record'
      case 'domain': return this.selectedDomain?.pack.name || 'Domain pack'
      case 'commitment': return this.selectedLifeCommitment?.title || 'Commitment history'
      case 'commitment-author': return this.commitmentForm.expectedRevision ? 'Append commitment revision' : 'Record commitment'
      case 'cost': return this.selectedLifeCost?.title || 'Cost evidence'
      case 'cost-author': return 'Record cost evidence'
      case 'contact-review': return this.selectedContactCandidate?.name ||
        (this.selectedContactMergeProposal ? 'Review possible duplicate contacts' : 'Contact review')
      case 'execution': return 'Execution authorization'
      case 'mandates': return 'Standing mandates'
      case 'learning': return 'Controlled learning'
      case 'agents': return 'Agent registry'
      case 'domains': return 'Domain packs'
    }
  }

  label(value: string): string {
    return value.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
  }

  shortDigest(value?: string): string {
    return value ? value.slice(0, 12) : 'Not recorded'
  }

  private compositionProvenanceDetail(
    field: string,
    label: string,
    value: string | number | undefined,
    digest = false
  ): MonitorCompositionProvenanceDetail {
    const recorded = value !== undefined && value !== null && String(value).trim() !== ''
    return {
      field,
      label,
      value: recorded ? (digest ? this.shortDigest(String(value)) : String(value)) : 'Not recorded',
      digest,
    }
  }

  private compositionWatermarkDetails(
    field: string,
    label: string,
    watermark: MonitorCompositionWatermark | MonitorCompositionFeedbackWatermark | string | number | undefined
  ): MonitorCompositionProvenanceDetail[] {
    if (typeof watermark === 'string' || typeof watermark === 'number' || watermark === undefined) {
      return [this.compositionProvenanceDetail(field, label, watermark)]
    }
    const cursor = watermark.cursor
    const details = [
      this.compositionProvenanceDetail(`${field}Count`, `${label} count`, watermark.count),
      this.compositionProvenanceDetail(`${field}WindowDigest`, `${label} window digest`, watermark.windowDigest, true),
      this.compositionProvenanceDetail(`${field}RecordedAt`, `${label} cursor recorded at`, cursor?.recordedAt),
      this.compositionProvenanceDetail(`${field}IdempotencyKey`, `${label} idempotency key`, cursor?.idempotencyKey),
      this.compositionProvenanceDetail(`${field}PayloadDigest`, `${label} payload digest`, cursor?.payloadDigest, true),
    ]
    if (cursor && 'ordinal' in cursor) {
      details.push(this.compositionProvenanceDetail(`${field}Ordinal`, `${label} ordinal`, cursor.ordinal))
    }
    if (cursor && 'feedbackId' in cursor) {
      details.push(
        this.compositionProvenanceDetail(`${field}FeedbackId`, `${label} feedback ID`, cursor.feedbackId),
        this.compositionProvenanceDetail(`${field}RecordDigest`, `${label} record digest`, cursor.recordDigest, true)
      )
    }
    return details
  }

  trackById(_: number, value: { id: string }): string {
    return value.id
  }

  trackByPackId(_: number, value: DomainPackSummaryView): string {
    return value.pack.id
  }

  private performMandateActivation(mandate: StandingMandate): Promise<void> {
    this.mutating = true
    return new Promise((resolve, reject) => {
      this.service.activateMandate(mandate.id, mandate.revision).subscribe({
        next: (updated) => {
          this.mutating = false
          this.notification.success('Mandate activated', 'Its bounded authority is now available to the authorization engine.')
          this.loadMandates()
          this.openMandate(updated)
          resolve()
        },
        error: (error) => {
          this.failMutation(error, 'The mandate was not activated.')
          reject(error)
        },
      })
    })
  }

  private performMandateRevocation(mandate: StandingMandate, reason: string): Promise<void> {
    this.mutating = true
    return new Promise((resolve, reject) => {
      this.service.revokeMandate(mandate.id, mandate.revision, reason).subscribe({
        next: (updated) => {
          this.mutating = false
          this.selectedMandate = updated
          this.notification.success('Mandate revoked', 'The authority is no longer active; its audit history remains.')
          this.loadMandates()
          resolve()
        },
        error: (error) => {
          this.failMutation(error, 'The mandate was not revoked.')
          reject(error)
        },
      })
    })
  }

  private syncOutcomeForm(definition: OutcomeRevision): void {
    const indicator = definition.outcome.indicators[0]
    this.outcomeForm.statement = definition.outcome.statement
    this.outcomeForm.lifeDomain = definition.outcome.lifeDomain || ''
    this.outcomeForm.windowStart = this.dateInput(definition.outcome.window.start)
    this.outcomeForm.windowEnd = this.dateInput(definition.outcome.window.end)
    if (!indicator) return
    this.outcomeForm.indicatorId = indicator.id
    this.outcomeForm.indicatorName = indicator.name
    this.outcomeForm.unit = indicator.unit
    this.outcomeForm.direction = indicator.direction
    this.outcomeForm.targetValue = indicator.targetValue
    this.outcomeForm.targetTolerance = indicator.targetTolerance
    this.outcomeForm.trendThresholdPerDay = indicator.trendThresholdPerDay
    this.outcomeForm.regressionThreshold = indicator.regressionThreshold
    this.outcomeForm.minimumObservations = indicator.minimumObservations
    this.outcomeForm.baselineValue = indicator.baseline.value
    this.outcomeForm.baselineObservedAt = this.dateInput(indicator.baseline.observedAt)
    if (!this.outcomeObservationForm.indicatorId) {
      this.outcomeObservationForm.indicatorId = indicator.id
    }
  }

  private captureOutcomeProjection(record: {
    lifeGraphProjection?: OutcomeLifeGraphProjection
    lifeGraphProjectionWarning?: string
  }): void {
    if (record.lifeGraphProjection) {
      this.latestOutcomeLifeGraphProjection = record.lifeGraphProjection
      this.outcomeLifeGraphProjectionWarning = ''
      return
    }
    if (record.lifeGraphProjectionWarning) {
      this.latestOutcomeLifeGraphProjection = undefined
      this.outcomeLifeGraphProjectionWarning = record.lifeGraphProjectionWarning
    }
  }

  private parseOutcomeDate(value: string): Date | undefined {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? undefined : parsed
  }

  private dateInput(value: string): string {
    const parsed = this.parseOutcomeDate(value)
    return parsed ? parsed.toISOString().slice(0, 10) : ''
  }

  private operationId(prefix: string): string {
    return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
  }

  private resetInspector(kind: InspectorKind): void {
    this.inspectorKind = kind
    this.inspectorLoading = false
    this.inspectorError = ''
    this.selectedReceipt = undefined
    this.selectedConsumption = undefined
    this.selectedMandate = undefined
    this.selectedMandateDecisions = []
    this.selectedProposal = undefined
    this.selectedLearningApplication = undefined
    this.selectedLearningDecisions = []
    this.selectedAgent = undefined
    this.selectedAgentTransitions = []
    this.selectedDomain = undefined
    this.selectedLifeCommitment = undefined
    this.selectedLifeCommitmentHistory = []
    this.selectedLifeCost = undefined
    this.selectedContactCandidate = undefined
    this.selectedContactMergeProposal = undefined
    this.selectedContactReviewDecision = undefined
    this.selectedCanonicalContact = undefined
  }

  private submitContactReview(
    action: ContactReviewAction,
    subjectId: string,
    mergeProposal: boolean
  ): void {
    if (this.mutating) return
    const reason = this.contactReviewForm.reason.trim()
    const canonicalName = this.contactReviewForm.canonicalName.trim()
    if (reason.length < 12) {
      this.notification.warning(
        'Meaningful reason required',
        'Explain the source-based reason for this decision in at least 12 characters.'
      )
      return
    }
    if ((action === 'correct' || action === 'merge') && !canonicalName) {
      this.notification.warning(
        'Canonical name required',
        'Corrected and merged contacts need the confirmed canonical name.'
      )
      return
    }
    const request = {
      action,
      canonicalName: canonicalName || undefined,
      canonicalSummary: this.contactReviewForm.canonicalSummary.trim() || undefined,
      reason,
      idempotencyKey: this.contactReviewForm.idempotencyKey,
    }
    this.mutating = true
    const operation = mergeProposal
      ? this.service.decideContactMerge(subjectId, request)
      : this.service.decideContactCandidate(subjectId, request)
    operation.subscribe({
      next: (result: ContactReviewDecisionResult) => {
        this.mutating = false
        this.selectedContactReviewDecision = result.decision
        this.selectedCanonicalContact = result.canonicalEntity
        this.notification.success(
          result.alreadyExisted ? 'Existing contact decision returned' : 'Contact review recorded',
          'The owner-scoped decision is local-only and grants no execution authority.'
        )
        this.loadLifeContext(true, true)
      },
      error: (error) => this.failMutation(error, 'The contact review decision was not recorded.'),
    })
  }

  private emptyContactReviewForm(canonicalName = '', canonicalSummary = '') {
    return {
      canonicalName,
      canonicalSummary,
      reason: '',
      idempotencyKey: this.operationId('contact-review'),
    }
  }

  private ledgerEvidence(form: {
    sourceId: string
    sourceUri: string
    contentDigest: string
    authority: string
    observedAt: string
    verification: LifeLedgerVerificationStatus
  }): LifeLedgerEvidenceReference | undefined {
    const digest = form.contentDigest.trim().toLowerCase()
    const observedAt = this.isoDateTime(form.observedAt)
    if (!form.sourceId.trim() || !form.sourceUri.trim() || !/^[a-f0-9]{64}$/.test(digest) || !observedAt) {
      return undefined
    }
    return {
      id: form.sourceId.trim(),
      uri: form.sourceUri.trim(),
      contentDigest: digest,
      authority: form.authority.trim() || undefined,
      observedAt,
      verification: form.verification,
      localOnly: true,
    }
  }

  private emptyCommitmentForm() {
    return {
      key: '', expectedRevision: 0, domain: '' as OutcomeLifeDomain | '', title: '', summary: '',
      status: 'proposed' as LifeCommitmentStatus, counterparty: '', projectKey: '', dueAt: '',
      verification: 'needs_review' as LifeLedgerVerificationStatus,
      observedAt: this.toLocalDateTime(new Date()),
      sourceId: '', sourceUri: '', contentDigest: '', authority: '',
    }
  }

  private emptyCostForm() {
    return {
      domain: '' as OutcomeLifeDomain | '', title: '', summary: '', kind: 'estimate' as LifeCostKind,
      amount: 0, currency: 'EUR', commitmentKey: '', projectKey: '',
      verification: 'needs_review' as LifeLedgerVerificationStatus,
      observedAt: this.toLocalDateTime(new Date()),
      sourceId: '', sourceUri: '', contentDigest: '', authority: '',
    }
  }

  private isoDateTime(value: string): string | undefined {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString()
  }

  private toLocalDateTime(value: Date): string {
    const offset = value.getTimezoneOffset() * 60_000
    return new Date(value.getTime() - offset).toISOString().slice(0, 16)
  }

  private newMonitorState(): AmbientMonitorViewState {
    return { loading: false, loaded: false, historyLoading: false, error: '', historyError: '' }
  }

  private newMonitorCompositionState(): MonitorCompositionViewState {
    return {
      loading: false,
      loaded: false,
      historyLoading: false,
      historyLoaded: false,
      error: '',
      historyError: '',
    }
  }

  private resetMonitorState(): void {
    this.monitorSubscription?.unsubscribe()
    this.monitorHistorySubscription?.unsubscribe()
    this.monitorCompositionSubscription?.unsubscribe()
    this.monitorCompositionHistorySubscription?.unsubscribe()
    this.monitorState = this.newMonitorState()
    this.monitorTargets = []
    this.monitorObservations = []
    this.monitorRuns = []
    this.monitorCompositions = []
    this.monitorCompositionAttempts = []
    this.monitorCompositionState = this.newMonitorCompositionState()
    this.selectedMonitorTargetId = ''
    this.monitorMutating = false
    this.monitorForm = {
      targetId: '',
      sourceKind: 'workflow_verified_completion_count',
      cadenceSeconds: 86400,
      firstRunAt: this.toLocalDateTime(new Date()),
      enabled: true,
    }
  }

  private monitorScopeKey(
    workspaceId = this.advisoryScope.workspaceId.trim(),
    outcomeId = this.advisoryScope.outcomeId.trim()
  ): string {
    return `${workspaceId}\u0000${outcomeId}`
  }

  private syncMonitorFormDefaults(): void {
    if (!this.monitorForm.targetId.trim()) {
      this.monitorForm.targetId = this.advisoryScope.outcomeId.trim() ? this.newMonitorTargetId() : ''
    }
    if (!this.monitorForm.firstRunAt) this.monitorForm.firstRunAt = this.toLocalDateTime(new Date())
  }

  private newMonitorTargetId(): string {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID()
    }
    const bytes = new Uint8Array(16)
    if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
      crypto.getRandomValues(bytes)
    } else {
      for (let index = 0; index < bytes.length; index += 1) {
        bytes[index] = Math.floor(Math.random() * 256)
      }
    }
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }

  private isCanonicalUuid(value: string): boolean {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value)
  }

  private newSurfaceState(): SurfaceState {
    return { loading: false, loaded: false, error: '' }
  }

  private newAdvisoryState(availability: AdvisoryAvailability): AdvisorySurfaceState {
    return { availability, loading: false, loaded: false, error: '' }
  }

  private shouldLoadAdvisory(surface: AdvisorySurface, force: boolean): boolean {
    const current = this.advisoryState[surface]
    return force || (!current.loading && !current.loaded)
  }

  private beginAdvisoryLoad(surface: AdvisorySurface): void {
    const current = this.advisoryState[surface]
    this.advisoryState[surface] = {
      ...current,
      availability: 'loading',
      loading: true,
      error: '',
    }
  }

  private finishAdvisoryLoad(surface: AdvisorySurface, recordCount: number): void {
    this.advisoryState[surface] = {
      availability: recordCount > 0 ? 'ready' : 'empty',
      loading: false,
      loaded: true,
      error: '',
      loadedAt: new Date().toISOString(),
    }
  }

  private failAdvisoryLoad(surface: AdvisorySurface, error: unknown, fallback: string): void {
    const current = this.advisoryState[surface]
    const response = error as HttpErrorResponse
    const unavailable = [404, 501, 503].includes(response?.status)
    this.advisoryState[surface] = {
      availability: current.loaded ? 'stale' : (unavailable ? 'unavailable' : 'error'),
      loading: false,
      loaded: current.loaded,
      error: this.describeError(error, fallback),
      loadedAt: current.loadedAt,
    }
  }

  private completedState(): SurfaceState {
    return { loading: false, loaded: true, error: '', loadedAt: new Date().toISOString() }
  }

  private beginLoad(surface: GovernanceSurface): void {
    this.state[surface] = { loading: true, loaded: false, error: '' }
  }

  private finishLoad(surface: GovernanceSurface): void {
    this.state[surface] = this.completedState()
  }

  private failLoad(surface: GovernanceSurface, error: unknown, fallback: string): void {
    this.state[surface] = {
      loading: false,
      loaded: false,
      error: this.describeError(error, fallback),
    }
  }

  private failInspector(error: unknown, fallback: string): void {
    this.inspectorLoading = false
    this.inspectorError = this.describeError(error, fallback)
  }

  private failMutation(error: unknown, fallback: string): void {
    this.mutating = false
    this.notification.error('Governance update failed', this.describeError(error, fallback))
  }

  private failMonitorMutation(error: unknown, fallback: string): void {
    this.monitorMutating = false
    this.notification.error('Ambient monitor update failed', this.describeError(error, fallback))
  }

  private describeError(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse
    const body = response?.error as
      | string
      | { error?: string | { message?: string }; message?: string }
      | undefined
    if (typeof body === 'string' && body.trim()) return body
    if (body && typeof body === 'object') {
      if (typeof body.error === 'string' && body.error.trim()) return body.error
      if (body.error && typeof body.error === 'object' && body.error.message?.trim()) {
        return body.error.message
      }
      if (body.message?.trim()) return body.message
    }
    return fallback
  }

  private splitValues(value: string): string[] {
    return value.split(',').map((item) => item.trim()).filter(Boolean)
  }

  private readonly attentionSurfaces: GovernanceSurface[] = ['execution', 'mandates', 'learning', 'agents']

  private ensureSurfaceLoaded(surface: GovernanceSurface): void {
    const current = this.state[surface]
    if (!current.loaded && !current.loading) this.loadSurface(surface)
  }

  private loadSurface(surface: GovernanceSurface): void {
    switch (surface) {
      case 'execution': this.loadExecution(); break
      case 'mandates': this.loadMandates(); break
      case 'learning': this.loadLearning(); break
      case 'agents': this.loadAgents(); break
      case 'domains': this.loadDomains(); break
    }
  }

  private surfaceTitle(surface: GovernanceSurface): string {
    switch (surface) {
      case 'execution': return 'Execution authorization'
      case 'mandates': return 'Standing mandates'
      case 'learning': return 'Controlled learning'
      case 'agents': return 'Agent registry'
      case 'domains': return 'Domain packs'
    }
  }
}

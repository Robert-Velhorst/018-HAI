import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { HttpErrorResponse } from '@angular/common/http'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  CapacitySignals,
  CapacitySnapshot,
  CapacityStatus,
  CreateGoalRequest,
  EntityDomainLink,
  GoalLevel,
  GoalNode,
  GoalTreeNode,
  LifeDomain,
  LifeOpsOverview,
  LinkEntityRequest,
  NeedObservation,
  PriorityAssessment,
  PriorityFactorKey,
  PriorityFactors,
  RecordCapacityRequest,
  RecordNeedRequest,
  UpdateGoalRequest,
} from '../../models/life-ops.model'
import { LifeOpsService } from '../../services/life-ops.service'

type LifeOpsEditor = 'need' | 'capacity' | 'link' | 'goal' | 'priority'
type NumericCapacityKey =
  | 'energy'
  | 'attentionQuality'
  | 'painIllnessLoad'
  | 'sleepQuality'
  | 'stressLoad'
  | 'mobility'
  | 'financialLiquidity'
  | 'deadlinePressure'
  | 'interruptionSensitivity'
  | 'recoveryRequirement'
  | 'taskSwitchingCost'
  | 'sensoryLoad'
  | 'decisionFatigue'
  | 'riskTolerance'
  | 'confidenceReadiness'

interface NumericField {
  key: NumericCapacityKey
  label: string
  inverse?: boolean
}

interface PriorityField {
  key: PriorityFactorKey
  label: string
  cost?: boolean
}

type CapacityFormSignals = Omit<CapacitySignals, NumericCapacityKey> &
  Record<NumericCapacityKey, number | null>

interface NeedForm {
  domainId: string
  needLevel: string
  state: string
  currentLevel: number | null
  targetLevel: number | null
  priority: number | null
  confidence: number | null
  evidence: string
  sourceLabel: string
  sourceUri: string
  observedAt: string
  expiresAt: string
  needsReview: boolean
}

interface CapacityForm {
  status: CapacityStatus
  signals: CapacityFormSignals
  timeAvailableMinutes: number | null
  concurrentWorkLimit: number | null
  currentLoad: number | null
  planningStepLimit: number | null
  constraints: string
  sourceLabel: string
  sourceUri: string
  capturedAt: string
  confidence: number | null
  needsReview: boolean
  availableTools: string
  availableHelpers: string
}

interface LinkForm {
  entityType: string
  entityId: string
  domainId: string
  primary: boolean
  confidence: number | null
  sourceLabel: string
  sourceUri: string
  evidence: string
  verificationStatus: string
}

interface GoalForm {
  parentId: string
  level: GoalLevel | ''
  domainIds: string[]
  title: string
  description: string
  successCriteria: string
  stopConditions: string
  status: string
  confidence: number | null
  sourceLabel: string
  sourceUri: string
  targetAt: string
}

interface PriorityForm {
  entityType: string
  entityId: string
  title: string
  deadline: string
  useCapacity: boolean
  factors: Record<PriorityFactorKey, number | null>
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-life-ops',
    templateUrl: './life-ops.component.html',
    styleUrls: ['./life-ops.component.scss'],
    standalone: false
})
export class LifeOpsComponent implements OnInit {
  readonly moduleId = 'life-ops'
  readonly capacityStatuses: CapacityStatus[] = [
    'available',
    'constrained',
    'recovering',
    'overloaded',
    'unavailable',
    'unknown',
  ]
  readonly needStates = [
    'unknown',
    'stable',
    'active',
    'attention_required',
    'critical',
    'improving',
    'declining',
    'met',
  ]
  readonly goalStatuses = [
    'proposed',
    'active',
    'waiting',
    'blocked',
    'completed',
    'abandoned',
    'archived',
  ]
  readonly goalLevels: GoalLevel[] = [
    'values_principles',
    'needs_responsibilities',
    'vision_future_state',
    'strategic_outcome',
    'pursuit',
    'programme_case',
    'project',
    'workflow',
    'task',
    'atomic_action',
    'verification_condition',
    'measured_outcome',
  ]
  readonly capacityNumericFields: NumericField[] = [
    { key: 'energy', label: 'Energy' },
    { key: 'attentionQuality', label: 'Attention quality' },
    { key: 'painIllnessLoad', label: 'Pain or illness load', inverse: true },
    { key: 'sleepQuality', label: 'Sleep quality' },
    { key: 'stressLoad', label: 'Stress load', inverse: true },
    { key: 'mobility', label: 'Mobility' },
    { key: 'financialLiquidity', label: 'Financial liquidity' },
    { key: 'deadlinePressure', label: 'Deadline pressure', inverse: true },
    { key: 'interruptionSensitivity', label: 'Interruption sensitivity', inverse: true },
    { key: 'recoveryRequirement', label: 'Recovery requirement', inverse: true },
    { key: 'taskSwitchingCost', label: 'Task switching cost', inverse: true },
    { key: 'sensoryLoad', label: 'Sensory load', inverse: true },
    { key: 'decisionFatigue', label: 'Decision fatigue', inverse: true },
    { key: 'riskTolerance', label: 'Risk tolerance' },
    { key: 'confidenceReadiness', label: 'Confidence readiness' },
  ]
  readonly priorityFields: PriorityField[] = [
    { key: 'importance', label: 'Importance' },
    { key: 'urgency', label: 'Urgency' },
    { key: 'humanNeedAffected', label: 'Human need affected' },
    { key: 'deadlinePressure', label: 'Deadline pressure' },
    { key: 'costOfDelay', label: 'Cost of delay' },
    { key: 'expectedValue', label: 'Expected value' },
    { key: 'harmAvoided', label: 'Harm avoided' },
    { key: 'probabilityOfSuccess', label: 'Probability of success' },
    { key: 'effort', label: 'Effort', cost: true },
    { key: 'duration', label: 'Duration', cost: true },
    { key: 'dependencies', label: 'Dependencies', cost: true },
    { key: 'reversibility', label: 'Reversibility' },
    { key: 'risk', label: 'Risk' },
    { key: 'legalObligation', label: 'Legal obligation' },
    { key: 'relationshipConsequences', label: 'Relationship consequences' },
    { key: 'availableCapacity', label: 'Available capacity' },
    { key: 'energyFit', label: 'Energy fit' },
    { key: 'opportunityCost', label: 'Opportunity cost', cost: true },
    { key: 'strategicAlignment', label: 'Strategic alignment' },
    { key: 'learningValue', label: 'Learning value' },
    { key: 'compoundingValue', label: 'Compounding value' },
    { key: 'staleness', label: 'Staleness' },
    { key: 'commitmentAge', label: 'Commitment age' },
    { key: 'peopleBlocked', label: 'People blocked' },
    { key: 'delegability', label: 'Delegability' },
  ]

  loading = false
  hasOverview = false
  saving = false
  errorMessage = ''
  domains: LifeDomain[] = []
  needs: NeedObservation[] = []
  capacity: CapacitySnapshot | null = null
  goals: GoalNode[] = []
  goalForest: GoalTreeNode[] = []
  entityLinks: EntityDomainLink[] = []
  entityLinksState: 'idle' | 'loading' | 'loaded' | 'error' = 'idle'
  entityLinksError = ''
  editingLinkId = ''
  reviewingNeedId = ''
  priorityAssessment?: PriorityAssessment
  editor?: LifeOpsEditor
  editingGoalId = ''

  needForm: NeedForm = this.newNeedForm()
  capacityForm: CapacityForm = this.newCapacityForm()
  linkForm: LinkForm = this.newLinkForm()
  goalForm: GoalForm = this.newGoalForm()
  priorityForm: PriorityForm = this.newPriorityForm()

  constructor(
    private service: LifeOpsService,
    private notification: NzNotificationService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    if (this.loading) return
    this.loading = true
    this.errorMessage = ''
    this.service.overview().subscribe({
      next: (overview) => {
        if (!this.isOverview(overview)) {
          this.loading = false
          this.errorMessage = 'The server returned incomplete owner context. Previously loaded records were kept.'
          return
        }
        const { domains, needs, capacity, goals, forest } = overview
        this.domains = domains
        this.needs = needs
        this.capacity = capacity
        this.goals = goals
        this.goalForest = forest
        this.hasOverview = true
        this.applyDomainDefaults()
        this.loading = false
      },
      error: (error) => {
        this.loading = false
        this.errorMessage = this.describeError(error, 'Whole-life context is unavailable.')
      },
    })
  }

  get currentNeeds(): NeedObservation[] {
    const seen = new Set<string>()
    return [...this.needs]
      .sort((left, right) => this.observationTime(right) - this.observationTime(left))
      .filter((need) => {
        if (seen.has(need.domainId)) return false
        seen.add(need.domainId)
        return true
      })
  }

  get reviewNeeds(): NeedObservation[] {
    const now = Date.now()
    return this.currentNeeds.filter((need) =>
      need.needsReview ||
      Boolean(need.expiresAt && Date.parse(need.expiresAt) <= now)
    )
  }

  get activeGoals(): GoalNode[] {
    return this.goals.filter((goal) => !['completed', 'archived', 'abandoned'].includes(goal.status))
  }

  get dueGoals(): GoalNode[] {
    const now = Date.now()
    return this.activeGoals
      .filter((goal) => goal.targetAt && Date.parse(goal.targetAt) >= now)
      .sort((left, right) => Date.parse(left.targetAt!) - Date.parse(right.targetAt!))
  }

  get capacityState(): 'missing' | 'unknown' | 'review' | 'constrained' | 'available' {
    if (!this.capacity) return 'missing'
    if (this.capacity.status === 'unknown') return 'unknown'
    if (!this.capacity.fresh || this.capacity.needsReview) return 'review'
    if (['constrained', 'overloaded', 'unavailable', 'recovering'].includes(this.capacity.status)) {
      return 'constrained'
    }
    return 'available'
  }

  get attentionState(): 'review' | 'capacity' | 'unknown' | 'recorded' {
    if (this.reviewNeeds.length || this.capacityState === 'review') return 'review'
    if (this.capacityState === 'constrained') return 'capacity'
    if (this.capacityState === 'missing' || this.capacityState === 'unknown' || !this.currentNeeds.length) return 'unknown'
    return 'recorded'
  }

  get canApplyCapacityToPriority(): boolean {
    return Boolean(
      this.capacity &&
      this.capacity.fresh &&
      !this.capacity.needsReview &&
      this.capacity.status !== 'unknown'
    )
  }

  get goalRequiresExecutionDetails(): boolean {
    const levelIndex = this.goalLevels.findIndex((level) => level === this.goalForm.level)
    return levelIndex >= 4 && levelIndex <= 9
  }

  get goalExecutionDetailsValid(): boolean {
    if (!this.goalRequiresExecutionDetails) return true
    return this.lines(this.goalForm.successCriteria).length > 0 &&
      this.lines(this.goalForm.stopConditions).length > 0
  }

  get canSaveNeed(): boolean { return this.needRequestPayload() !== null }
  get canSaveCapacity(): boolean { return this.capacityRequestPayload() !== null }
  get canSaveLink(): boolean { return this.linkRequestPayload() !== null }
  get canSaveGoal(): boolean { return this.goalRequestData() !== null }
  get canAssessPriority(): boolean { return this.priorityAssessmentRequestReady() }

  get capacityAge(): string {
    if (!this.capacity) return 'No snapshot'
    const capturedAt = Date.parse(this.capacity.capturedAt)
    if (!Number.isFinite(capturedAt)) return 'Capture time unavailable'
    const elapsed = Math.max(0, Date.now() - capturedAt)
    const minutes = Math.floor(elapsed / 60000)
    if (minutes < 60) return `${minutes} min ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 48) return `${hours} h ago`
    return `${Math.floor(hours / 24)} d ago`
  }

  openEditor(editor: LifeOpsEditor): void {
    this.editor = editor
    if (editor === 'goal' && !this.editingGoalId) this.goalForm = this.newGoalForm()
  }

  closeEditor(): void {
    if (this.saving) return
    this.editor = undefined
    this.editingGoalId = ''
    this.editingLinkId = ''
    this.reviewingNeedId = ''
  }

  reviewNeed(need: NeedObservation): void {
    this.reviewingNeedId = need.id
    this.needForm = {
      domainId: need.domainId,
      needLevel: need.needLevel,
      state: need.state,
      currentLevel: need.currentLevel,
      targetLevel: need.targetLevel,
      priority: need.priority,
      confidence: need.confidence,
      evidence: need.evidence.join('\n'),
      sourceLabel: need.sourceLabel,
      sourceUri: need.sourceUri ?? '',
      observedAt: this.localDateTime(),
      expiresAt: '',
      needsReview: false,
    }
    this.openEditor('need')
  }

  prepareCapacityRecord(): void {
    this.capacityForm = this.capacity
      ? {
          status: this.capacity.status,
          signals: { ...this.capacity.signals },
          timeAvailableMinutes: this.capacity.timeAvailableMinutes,
          concurrentWorkLimit: this.capacity.concurrentWorkLimit,
          currentLoad: this.capacity.currentLoad,
          planningStepLimit: this.capacity.planningStepLimit,
          constraints: this.capacity.constraints.join('\n'),
          sourceLabel: this.capacity.sourceLabel,
          sourceUri: this.capacity.sourceUri ?? '',
          capturedAt: this.localDateTime(),
          confidence: this.capacity.confidence,
          needsReview: true,
          availableTools: (this.capacity.signals.availableTools ?? []).join('\n'),
          availableHelpers: (this.capacity.signals.availableHelpers ?? []).join('\n'),
        }
      : this.newCapacityForm()
    this.openEditor('capacity')
  }

  saveNeed(): void {
    if (this.saving) return
    const request = this.needRequestPayload()
    if (!request) {
      this.notification.error('Need observation not saved', 'Enter each requested value, a source label, and valid observation dates. Scores must be whole numbers from 0 to 100.')
      return
    }
    this.saving = true
    this.service.recordNeed(request).subscribe({
      next: (need) => {
        this.saving = false
        this.needs = [need, ...this.needs]
        this.needForm = this.newNeedForm()
        this.reviewingNeedId = ''
        this.applyDomainDefaults()
        this.closeEditor()
        this.notification.success('Need state recorded', 'HAI will use this sourced observation when planning owner work.')
      },
      error: (error) => this.mutationFailed(error, 'Need state was not recorded.'),
    })
  }

  saveCapacity(): void {
    if (this.saving) return
    const request = this.capacityRequestPayload()
    if (!request) {
      this.notification.error('Capacity snapshot not saved', 'Enter an explicit value for every capacity field, source label, and capture time. No blank value is converted to zero.')
      return
    }
    this.saving = true
    this.service.recordCapacity(request).subscribe({
      next: (capacity) => {
        this.saving = false
        this.capacity = capacity
        this.capacityForm = this.newCapacityForm()
        this.closeEditor()
        this.notification.success('Capacity updated', 'New plans will use this capacity boundary.')
      },
      error: (error) => this.mutationFailed(error, 'Capacity was not updated.'),
    })
  }

  saveLink(): void {
    if (this.saving) return
    const request = this.linkRequestPayload()
    if (!request) {
      this.notification.error('Domain link not saved', 'Enter the entity, domain, source, and a confidence value from 0 to 1.')
      return
    }
    this.saving = true
    this.service.linkEntity(request).subscribe({
      next: () => {
        this.saving = false
        this.editingLinkId = ''
        this.loadEntityLinks()
        this.notification.success('Domain link saved', 'The entity now has an owner-scoped life-domain relationship.')
      },
      error: (error) => this.mutationFailed(error, 'The domain link was not saved.'),
    })
  }

  loadEntityLinks(): void {
    if (!this.linkForm.entityType.trim() || !this.linkForm.entityId.trim()) return
    this.entityLinksState = 'loading'
    this.entityLinksError = ''
    this.entityLinks = []
    this.service.entityDomains(this.linkForm.entityType, this.linkForm.entityId).subscribe({
      next: (links) => {
        this.entityLinks = links
        this.entityLinksState = 'loaded'
      },
      error: (error) => {
        this.entityLinksState = 'error'
        this.entityLinksError = this.describeError(error, 'HAI could not load this entity.')
      },
    })
  }

  editLink(link: EntityDomainLink): void {
    this.editingLinkId = link.id
    this.linkForm = {
      entityType: link.entityType,
      entityId: link.entityId,
      domainId: link.domainId,
      primary: link.primary,
      confidence: link.confidence,
      sourceLabel: link.sourceLabel,
      sourceUri: link.sourceUri ?? '',
      evidence: link.evidence.join('\n'),
      verificationStatus: link.verificationStatus,
    }
    this.openEditor('link')
  }

  beginEditGoal(goal: GoalNode): void {
    this.editingGoalId = goal.id
    this.goalForm = {
      parentId: goal.parentId ?? '',
      level: goal.level,
      domainIds: [...goal.domainIds],
      title: goal.title,
      description: goal.description ?? '',
      successCriteria: goal.successCriteria.join('\n'),
      stopConditions: goal.stopConditions.join('\n'),
      status: goal.status,
      confidence: goal.confidence,
      sourceLabel: goal.sourceLabel,
      sourceUri: goal.sourceUri ?? '',
      targetAt: goal.targetAt ? this.localDateTime(goal.targetAt) : '',
    }
    this.openEditor('goal')
  }

  resetGoalForm(): void {
    this.editingGoalId = ''
    this.goalForm = this.newGoalForm()
    this.applyDomainDefaults()
  }

  saveGoal(): void {
    if (this.saving) return
    const data = this.goalRequestData()
    if (!data) {
      this.notification.error('Goal not saved', 'Enter a title, life domain, source label, confidence value, and any success/stop conditions required for this goal level.')
      return
    }
    this.saving = true
    const request = this.editingGoalId
      ? this.service.updateGoal(this.editingGoalId, {
          ...data,
          ...(this.goalForm.parentId
            ? { parentId: this.goalForm.parentId }
            : { clearParent: true }),
          ...(!this.goalForm.targetAt ? { clearTarget: true } : {}),
        })
      : this.service.createGoal({
          ...data,
          ...(this.goalForm.parentId ? { parentId: this.goalForm.parentId } : {}),
        })
    request.subscribe({
      next: () => {
        this.saving = false
        this.goalForm = this.newGoalForm()
        this.editingGoalId = ''
        this.closeEditor()
        this.notification.success('Goal saved', 'The hierarchy will be refreshed from the owner record.')
        this.refresh()
      },
      error: (error) => this.mutationFailed(error, 'The goal was not saved.'),
    })
  }

  assessPriority(): void {
    if (this.saving) return
    const factors = this.priorityFactorsPayload()
    if (!factors || !this.validPriorityForm()) {
      this.notification.error('Priority not assessed', 'Enter a title, entity type and ID, plus an explicit 0-100 estimate for every decision factor.')
      return
    }
    this.saving = true
    this.service.assessPriority({
      entityType: this.priorityForm.entityType,
      entityId: this.priorityForm.entityId,
      title: this.priorityForm.title,
      ...(this.priorityForm.deadline
        ? { deadline: this.toISOString(this.priorityForm.deadline) }
        : {}),
      factors,
      ...(this.priorityForm.useCapacity && this.canApplyCapacityToPriority && this.capacity
        ? { capacity: this.capacity }
        : {}),
    }).subscribe({
      next: (assessment) => {
        this.saving = false
        this.priorityAssessment = assessment
        this.notification.success('Priority assessed', 'The result is an explanation, not an automatic execution decision.')
      },
      error: (error) => this.mutationFailed(error, 'Priority could not be assessed.'),
    })
  }

  domainName(id: string): string {
    return this.domains.find((domain) => domain.id === id)?.name ?? id.replace(/_/g, ' ')
  }

  goalLevelLabel(level: string): string {
    return level.replace(/_/g, ' ')
  }

  sourceHref(uri?: string): string | null {
    if (!uri) return null
    try {
      const parsed = new URL(uri, window.location.origin)
      return ['http:', 'https:'].includes(parsed.protocol) ? parsed.href : null
    } catch {
      return null
    }
  }

  trackById(_: number, item: { id: string }): string {
    return item.id
  }

  trackByKey(_: number, item: { key: string }): string {
    return item.key
  }

  private applyDomainDefaults(): void {
    const first = this.domains[0]?.id ?? ''
    if (!this.needForm.domainId) this.needForm.domainId = first
    if (!this.linkForm.domainId) this.linkForm.domainId = first
    if (!this.goalForm.domainIds.length && first) this.goalForm.domainIds = [first]
  }

  private isOverview(value: unknown): value is LifeOpsOverview {
    if (!value || typeof value !== 'object') return false
    const overview = value as Partial<LifeOpsOverview>
    return Array.isArray(overview.domains) &&
      Array.isArray(overview.needs) &&
      Array.isArray(overview.goals) &&
      Array.isArray(overview.forest) &&
      (overview.capacity === null || Boolean(overview.capacity && typeof overview.capacity === 'object'))
  }

  private observationTime(need: NeedObservation): number {
    const observedAt = Date.parse(need.observedAt)
    if (Number.isFinite(observedAt)) return observedAt
    const createdAt = Date.parse(need.createdAt)
    return Number.isFinite(createdAt) ? createdAt : Number.NEGATIVE_INFINITY
  }

  private needRequestPayload(): RecordNeedRequest | null {
    const form = this.needForm
    const { currentLevel, targetLevel, priority, confidence } = form
    if (
      !this.domains.some((domain) => domain.id === form.domainId) ||
      !form.needLevel.trim() ||
      !this.needStates.includes(form.state) ||
      !this.isWholeNumber(currentLevel, 0, 100) ||
      !this.isWholeNumber(targetLevel, 0, 100) ||
      !this.isWholeNumber(priority, 0, 100) ||
      !this.isNumber(confidence, 0, 1) ||
      !form.sourceLabel.trim() ||
      !this.isValidDate(form.observedAt) ||
      (form.expiresAt && (!this.isValidDate(form.expiresAt) || Date.parse(form.expiresAt) <= Date.parse(form.observedAt)))
    ) return null

    return {
      domainId: form.domainId,
      needLevel: form.needLevel.trim(),
      state: form.state,
      currentLevel,
      targetLevel,
      priority,
      confidence,
      evidence: this.lines(form.evidence),
      sourceLabel: form.sourceLabel.trim(),
      ...(form.sourceUri.trim() ? { sourceUri: form.sourceUri.trim() } : {}),
      observedAt: this.toISOString(form.observedAt),
      ...(form.expiresAt ? { expiresAt: this.toISOString(form.expiresAt) } : {}),
      needsReview: form.needsReview,
    }
  }

  private capacityRequestPayload(): RecordCapacityRequest | null {
    const form = this.capacityForm
    const { timeAvailableMinutes, concurrentWorkLimit, currentLoad, planningStepLimit, confidence } = form
    const signals = this.capacitySignalsPayload()
    if (
      !signals ||
      !this.capacityStatuses.includes(form.status) ||
      !this.isWholeNumber(timeAvailableMinutes, 0, 10080) ||
      !this.isWholeNumber(concurrentWorkLimit, 0, 50) ||
      !this.isWholeNumber(currentLoad, 0, 100) ||
      (planningStepLimit !== null && !this.isWholeNumber(planningStepLimit, 0, 20)) ||
      !this.isNumber(confidence, 0, 1) ||
      !form.sourceLabel.trim() ||
      !this.isValidDate(form.capturedAt)
    ) return null

    return {
      status: form.status,
      signals,
      timeAvailableMinutes,
      concurrentWorkLimit,
      currentLoad,
      ...(planningStepLimit !== null && planningStepLimit > 0 ? { planningStepLimit } : {}),
      constraints: this.lines(form.constraints),
      sourceLabel: form.sourceLabel.trim(),
      ...(form.sourceUri.trim() ? { sourceUri: form.sourceUri.trim() } : {}),
      capturedAt: this.toISOString(form.capturedAt),
      confidence,
      needsReview: form.needsReview,
    }
  }

  private capacitySignalsPayload(): CapacitySignals | null {
    const signals = this.capacityForm.signals
    if (!this.hasCompleteCapacitySignals(signals)) return null
    return {
      ...signals,
      availableTools: this.lines(this.capacityForm.availableTools),
      availableHelpers: this.lines(this.capacityForm.availableHelpers),
    }
  }

  private hasCompleteCapacitySignals(signals: CapacityFormSignals): signals is CapacitySignals {
    return this.capacityNumericFields.every((field) =>
      this.isWholeNumber(signals[field.key], 0, 100)
    )
  }

  private linkRequestPayload(): LinkEntityRequest | null {
    const form = this.linkForm
    const { confidence } = form
    if (
      !form.entityType.trim() ||
      !form.entityId.trim() ||
      !this.domains.some((domain) => domain.id === form.domainId) ||
      !this.isNumber(confidence, 0, 1) ||
      !form.sourceLabel.trim() ||
      !['verified', 'source_supported', 'human_confirmed', 'uncertain', 'unsupported', 'needs_review'].includes(form.verificationStatus)
    ) return null

    return {
      entityType: form.entityType.trim(),
      entityId: form.entityId.trim(),
      domainId: form.domainId,
      primary: form.primary,
      confidence,
      sourceLabel: form.sourceLabel.trim(),
      ...(form.sourceUri.trim() ? { sourceUri: form.sourceUri.trim() } : {}),
      evidence: this.lines(form.evidence),
      verificationStatus: form.verificationStatus,
    }
  }

  private goalRequestData(): Omit<CreateGoalRequest, 'parentId'> | null {
    const form = this.goalForm
    const level = this.asGoalLevel(form.level)
    const { confidence } = form
    if (
      !level ||
      !form.domainIds.length ||
      form.domainIds.some((id) => !this.domains.some((domain) => domain.id === id)) ||
      !form.title.trim() ||
      !this.goalStatuses.includes(form.status) ||
      !this.isNumber(confidence, 0, 1) ||
      !form.sourceLabel.trim() ||
      !this.goalExecutionDetailsValid ||
      (form.targetAt && !this.isValidDate(form.targetAt))
    ) return null

    return {
      level,
      domainIds: [...form.domainIds],
      title: form.title.trim(),
      ...(form.description.trim() ? { description: form.description.trim() } : {}),
      successCriteria: this.lines(form.successCriteria),
      stopConditions: this.lines(form.stopConditions),
      status: form.status,
      confidence,
      sourceLabel: form.sourceLabel.trim(),
      ...(form.sourceUri.trim() ? { sourceUri: form.sourceUri.trim() } : {}),
      ...(form.targetAt ? { targetAt: this.toISOString(form.targetAt) } : {}),
    }
  }

  private asGoalLevel(value: string): GoalLevel | null {
    return this.goalLevels.find((level) => level === value) ?? null
  }

  private priorityAssessmentRequestReady(): boolean {
    return this.validPriorityForm() && this.priorityFactorsPayload() !== null
  }

  private validPriorityForm(): boolean {
    const form = this.priorityForm
    return Boolean(
      form.title.trim() &&
      form.entityType.trim() &&
      form.entityId.trim() &&
      (!form.deadline || this.isValidDate(form.deadline))
    )
  }

  private priorityFactorsPayload(): PriorityFactors | null {
    const factors = this.priorityForm.factors
    return this.hasCompletePriorityFactors(factors) ? factors : null
  }

  private hasCompletePriorityFactors(
    factors: Record<PriorityFactorKey, number | null>
  ): factors is PriorityFactors {
    return this.priorityFields.every((field) => this.isWholeNumber(factors[field.key], 0, 100))
  }

  private isNumber(value: number | null, min: number, max: number): value is number {
    return value !== null && Number.isFinite(value) && value >= min && value <= max
  }

  private isWholeNumber(value: number | null, min: number, max: number): value is number {
    return this.isNumber(value, min, max) && Number.isInteger(value)
  }

  private isValidDate(value: string): boolean {
    return Boolean(value) && Number.isFinite(Date.parse(value))
  }

  private mutationFailed(error: unknown, fallback: string): void {
    this.saving = false
    this.notification.error('Life Ops update failed', this.describeError(error, fallback))
  }

  private describeError(error: unknown, fallback: string): string {
    if (error instanceof HttpErrorResponse) {
      const message = error.error?.error
      if (typeof message === 'string' && message.trim()) return message
    }
    if (error instanceof Error && error.message.trim()) return error.message
    return fallback
  }

  private lines(value: string): string[] {
    return value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  }

  private toISOString(value: string): string {
    return new Date(value).toISOString()
  }

  private localDateTime(value?: string): string {
    const date = value ? new Date(value) : new Date()
    const offset = date.getTimezoneOffset() * 60000
    return new Date(date.getTime() - offset).toISOString().slice(0, 16)
  }

  private newNeedForm(): NeedForm {
    return {
      domainId: '',
      needLevel: '',
      state: 'unknown',
      currentLevel: null,
      targetLevel: null,
      priority: null,
      confidence: null,
      evidence: '',
      sourceLabel: '',
      sourceUri: '',
      observedAt: this.localDateTime(),
      expiresAt: '',
      needsReview: true,
    }
  }

  private newCapacityForm(): CapacityForm {
    const signals: CapacityFormSignals = {
      energy: null,
      attentionQuality: null,
      painIllnessLoad: null,
      sleepQuality: null,
      stressLoad: null,
      mobility: null,
      financialLiquidity: null,
      deadlinePressure: null,
      interruptionSensitivity: null,
      recoveryRequirement: null,
      taskSwitchingCost: null,
      sensoryLoad: null,
      decisionFatigue: null,
      riskTolerance: null,
      confidenceReadiness: null,
      location: '',
      availableTools: [],
      availableHelpers: [],
      weatherConditions: '',
      environmentalConditions: '',
      socialAppropriateness: '',
    }
    return {
      status: 'unknown',
      signals,
      timeAvailableMinutes: null,
      concurrentWorkLimit: null,
      currentLoad: null,
      planningStepLimit: null,
      constraints: '',
      sourceLabel: '',
      sourceUri: '',
      capturedAt: this.localDateTime(),
      confidence: null,
      needsReview: true,
      availableTools: '',
      availableHelpers: '',
    }
  }

  private newLinkForm(): LinkForm {
    return {
      entityType: '',
      entityId: '',
      domainId: '',
      primary: true,
      confidence: null,
      sourceLabel: '',
      sourceUri: '',
      evidence: '',
      verificationStatus: 'needs_review',
    }
  }

  private newGoalForm(): GoalForm {
    return {
      parentId: '',
      level: '',
      domainIds: [],
      title: '',
      description: '',
      successCriteria: '',
      stopConditions: '',
      status: 'proposed',
      confidence: null,
      sourceLabel: '',
      sourceUri: '',
      targetAt: '',
    }
  }

  private newPriorityForm(): PriorityForm {
    return {
      entityType: '',
      entityId: '',
      title: '',
      deadline: '',
      useCapacity: false,
      factors: {
        importance: null,
        urgency: null,
        humanNeedAffected: null,
        deadlinePressure: null,
        costOfDelay: null,
        expectedValue: null,
        harmAvoided: null,
        probabilityOfSuccess: null,
        effort: null,
        duration: null,
        dependencies: null,
        reversibility: null,
        risk: null,
        legalObligation: null,
        relationshipConsequences: null,
        availableCapacity: null,
        energyFit: null,
        opportunityCost: null,
        strategicAlignment: null,
        learningValue: null,
        compoundingValue: null,
        staleness: null,
        commitmentAge: null,
        peopleBlocked: null,
        delegability: null,
      },
    }
  }
}

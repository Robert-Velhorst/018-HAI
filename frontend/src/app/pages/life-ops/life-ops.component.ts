import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { HttpErrorResponse } from '@angular/common/http'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { forkJoin } from 'rxjs'
import {
  CapacitySignals,
  CapacitySnapshot,
  CapacityStatus,
  EntityDomainLink,
  GoalLevel,
  GoalNode,
  GoalTreeNode,
  LifeDomain,
  NeedObservation,
  PriorityAssessment,
  PriorityFactorKey,
  PriorityFactors,
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

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-life-ops',
  templateUrl: './life-ops.component.html',
  styleUrls: ['./life-ops.component.scss'],
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

  loading = true
  saving = false
  errorMessage = ''
  domains: LifeDomain[] = []
  needs: NeedObservation[] = []
  capacity: CapacitySnapshot | null = null
  goals: GoalNode[] = []
  goalForest: GoalTreeNode[] = []
  entityLinks: EntityDomainLink[] = []
  entityLinksLoaded = false
  priorityAssessment?: PriorityAssessment
  editor?: LifeOpsEditor
  editingGoalId = ''

  needForm = this.newNeedForm()
  capacityForm = this.newCapacityForm()
  linkForm = this.newLinkForm()
  goalForm = this.newGoalForm()
  priorityForm = this.newPriorityForm()

  constructor(
    private service: LifeOpsService,
    private notification: NzNotificationService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    if (this.loading && this.domains.length) return
    this.loading = true
    this.errorMessage = ''
    forkJoin({
      domains: this.service.domains(),
      needs: this.service.needs(undefined, 100),
      capacity: this.service.latestCapacity(),
      goals: this.service.goals(),
      forest: this.service.goalForest(),
    }).subscribe({
      next: ({ domains, needs, capacity, goals, forest }) => {
        this.domains = domains
        this.needs = needs
        this.capacity = capacity
        this.goals = goals
        this.goalForest = forest
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
    return this.needs.filter((need) => {
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
    return this.goals.filter((goal) => !['completed', 'archived', 'cancelled'].includes(goal.status))
  }

  get dueGoals(): GoalNode[] {
    const now = Date.now()
    return this.activeGoals
      .filter((goal) => goal.targetAt && Date.parse(goal.targetAt) >= now)
      .sort((left, right) => Date.parse(left.targetAt!) - Date.parse(right.targetAt!))
  }

  get capacityState(): 'missing' | 'review' | 'constrained' | 'available' {
    if (!this.capacity) return 'missing'
    if (!this.capacity.fresh || this.capacity.needsReview) return 'review'
    if (['constrained', 'overloaded', 'unavailable', 'recovering'].includes(this.capacity.status)) {
      return 'constrained'
    }
    return 'available'
  }

  get capacityAge(): string {
    if (!this.capacity) return 'No snapshot'
    const elapsed = Math.max(0, Date.now() - Date.parse(this.capacity.capturedAt))
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
    this.editor = undefined
    this.editingGoalId = ''
  }

  saveNeed(): void {
    if (this.saving) return
    this.saving = true
    this.service.recordNeed({
      domainId: this.needForm.domainId,
      needLevel: this.needForm.needLevel,
      state: this.needForm.state,
      currentLevel: this.needForm.currentLevel,
      targetLevel: this.needForm.targetLevel,
      priority: this.needForm.priority,
      confidence: this.needForm.confidence,
      evidence: this.lines(this.needForm.evidence),
      sourceLabel: this.needForm.sourceLabel,
      ...(this.needForm.sourceUri ? { sourceUri: this.needForm.sourceUri } : {}),
      observedAt: this.toISOString(this.needForm.observedAt),
      ...(this.needForm.expiresAt
        ? { expiresAt: this.toISOString(this.needForm.expiresAt) }
        : {}),
      needsReview: this.needForm.needsReview,
    }).subscribe({
      next: (need) => {
        this.saving = false
        this.needs = [need, ...this.needs]
        this.needForm = this.newNeedForm()
        this.applyDomainDefaults()
        this.closeEditor()
        this.notification.success('Need state recorded', 'HAI will use this sourced observation when planning owner work.')
      },
      error: (error) => this.mutationFailed(error, 'Need state was not recorded.'),
    })
  }

  saveCapacity(): void {
    if (this.saving) return
    this.saving = true
    const signals: CapacitySignals = {
      ...this.capacityForm.signals,
      availableTools: this.lines(this.capacityForm.availableTools),
      availableHelpers: this.lines(this.capacityForm.availableHelpers),
    }
    this.service.recordCapacity({
      status: this.capacityForm.status,
      signals,
      timeAvailableMinutes: this.capacityForm.timeAvailableMinutes,
      concurrentWorkLimit: this.capacityForm.concurrentWorkLimit,
      currentLoad: this.capacityForm.currentLoad,
      ...(this.capacityForm.planningStepLimit
        ? { planningStepLimit: this.capacityForm.planningStepLimit }
        : {}),
      constraints: this.lines(this.capacityForm.constraints),
      sourceLabel: this.capacityForm.sourceLabel,
      ...(this.capacityForm.sourceUri ? { sourceUri: this.capacityForm.sourceUri } : {}),
      capturedAt: this.toISOString(this.capacityForm.capturedAt),
      confidence: this.capacityForm.confidence,
      needsReview: this.capacityForm.needsReview,
    }).subscribe({
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
    this.saving = true
    this.service.linkEntity({
      entityType: this.linkForm.entityType,
      entityId: this.linkForm.entityId,
      domainId: this.linkForm.domainId,
      primary: this.linkForm.primary,
      confidence: this.linkForm.confidence,
      sourceLabel: this.linkForm.sourceLabel,
      ...(this.linkForm.sourceUri ? { sourceUri: this.linkForm.sourceUri } : {}),
      evidence: this.lines(this.linkForm.evidence),
      verificationStatus: this.linkForm.verificationStatus,
    }).subscribe({
      next: () => {
        this.saving = false
        this.loadEntityLinks()
        this.notification.success('Domain link saved', 'The entity now has an owner-scoped life-domain relationship.')
      },
      error: (error) => this.mutationFailed(error, 'The domain link was not saved.'),
    })
  }

  loadEntityLinks(): void {
    if (!this.linkForm.entityType.trim() || !this.linkForm.entityId.trim()) return
    this.service.entityDomains(this.linkForm.entityType, this.linkForm.entityId).subscribe({
      next: (links) => {
        this.entityLinks = links
        this.entityLinksLoaded = true
      },
      error: (error) => {
        this.entityLinksLoaded = true
        this.entityLinks = []
        this.notification.error('Domain links unavailable', this.describeError(error, 'HAI could not load this entity.'))
      },
    })
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
      targetAt: this.localDateTime(goal.targetAt),
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
    this.saving = true
    const common = {
      level: this.goalForm.level,
      domainIds: this.goalForm.domainIds,
      title: this.goalForm.title,
      description: this.goalForm.description,
      successCriteria: this.lines(this.goalForm.successCriteria),
      stopConditions: this.lines(this.goalForm.stopConditions),
      status: this.goalForm.status,
      confidence: this.goalForm.confidence,
      sourceLabel: this.goalForm.sourceLabel,
      sourceUri: this.goalForm.sourceUri,
      ...(this.goalForm.targetAt
        ? { targetAt: this.toISOString(this.goalForm.targetAt) }
        : {}),
    }
    const request = this.editingGoalId
      ? this.service.updateGoal(this.editingGoalId, {
          ...common,
          ...(this.goalForm.parentId
            ? { parentId: this.goalForm.parentId }
            : { clearParent: true }),
          ...(!this.goalForm.targetAt ? { clearTarget: true } : {}),
        })
      : this.service.createGoal({
          ...common,
          ...(this.goalForm.parentId ? { parentId: this.goalForm.parentId } : {}),
        })
    request.subscribe({
      next: () => {
        this.saving = false
        this.goalForm = this.newGoalForm()
        this.closeEditor()
        this.notification.success('Goal saved', 'The hierarchy will be refreshed from the owner record.')
        this.refresh()
      },
      error: (error) => this.mutationFailed(error, 'The goal was not saved.'),
    })
  }

  assessPriority(): void {
    if (this.saving) return
    this.saving = true
    this.service.assessPriority({
      entityType: this.priorityForm.entityType,
      entityId: this.priorityForm.entityId,
      title: this.priorityForm.title,
      ...(this.priorityForm.deadline
        ? { deadline: this.toISOString(this.priorityForm.deadline) }
        : {}),
      factors: { ...this.priorityForm.factors },
      ...(this.priorityForm.useCapacity && this.capacity
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

  private newNeedForm() {
    return {
      domainId: '',
      needLevel: 'safety',
      state: 'attention_required',
      currentLevel: 50,
      targetLevel: 75,
      priority: 60,
      confidence: 0.8,
      evidence: '',
      sourceLabel: 'operator_report',
      sourceUri: '',
      observedAt: this.localDateTime(),
      expiresAt: '',
      needsReview: false,
    }
  }

  private newCapacityForm() {
    const signals: CapacitySignals = {
      energy: 50,
      attentionQuality: 50,
      painIllnessLoad: 0,
      sleepQuality: 50,
      stressLoad: 0,
      mobility: 50,
      financialLiquidity: 50,
      deadlinePressure: 0,
      interruptionSensitivity: 50,
      recoveryRequirement: 0,
      taskSwitchingCost: 50,
      sensoryLoad: 0,
      decisionFatigue: 0,
      riskTolerance: 50,
      confidenceReadiness: 50,
      location: '',
      weatherConditions: '',
      environmentalConditions: '',
      socialAppropriateness: '',
    }
    return {
      status: 'available' as CapacityStatus,
      signals,
      timeAvailableMinutes: 120,
      concurrentWorkLimit: 2,
      currentLoad: 30,
      planningStepLimit: 0,
      constraints: '',
      sourceLabel: 'operator_report',
      sourceUri: '',
      capturedAt: this.localDateTime(),
      confidence: 0.8,
      needsReview: false,
      availableTools: '',
      availableHelpers: '',
    }
  }

  private newLinkForm() {
    return {
      entityType: 'pursuit',
      entityId: '',
      domainId: '',
      primary: true,
      confidence: 0.8,
      sourceLabel: 'operator_classification',
      sourceUri: '',
      evidence: '',
      verificationStatus: 'operator_confirmed',
    }
  }

  private newGoalForm() {
    return {
      parentId: '',
      level: 'pursuit' as GoalLevel,
      domainIds: [] as string[],
      title: '',
      description: '',
      successCriteria: '',
      stopConditions: '',
      status: 'active',
      confidence: 0.8,
      sourceLabel: 'operator_goal',
      sourceUri: '',
      targetAt: '',
    }
  }

  private newPriorityForm() {
    const factors = {} as PriorityFactors
    this.priorityFields.forEach((field) => (factors[field.key] = 50))
    return {
      entityType: 'goal',
      entityId: '',
      title: '',
      deadline: '',
      useCapacity: true,
      factors,
    }
  }
}

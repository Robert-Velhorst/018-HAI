import { HttpErrorResponse } from '@angular/common/http'
import { TestBed } from '@angular/core/testing'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { of, throwError } from 'rxjs'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { CapacitySnapshot, GoalNode, LifeDomain, NeedObservation } from '../../models/life-ops.model'
import { LifeOpsService } from '../../services/life-ops.service'
import { LifeOpsComponent } from './life-ops.component'
import { LifeOpsModule } from './life-ops.module'

describe('LifeOpsComponent', () => {
  const viewPreferenceKey = 'hai.module-view.v1.life-ops'
  let service: jasmine.SpyObj<LifeOpsService>
  let notification: jasmine.SpyObj<any>
  let component: LifeOpsComponent

  beforeEach(() => window.localStorage.removeItem(viewPreferenceKey))
  afterEach(() => window.localStorage.removeItem(viewPreferenceKey))

  const domain: LifeDomain = {
    id: 'health_wellbeing',
    name: 'Health and wellbeing',
    description: 'Physical and mental capacity.',
    needClass: 'physiological',
    sensitive: true,
  }
  const need = (id: string, overrides: Partial<NeedObservation> = {}): NeedObservation => ({
    id,
    ownerIdentity: 'owner-1',
    domainId: domain.id,
    needLevel: 'health',
    state: 'attention_required',
    currentLevel: 45,
    targetLevel: 75,
    gap: 30,
    priority: 70,
    confidence: 0.8,
    evidence: ['operator report'],
    sourceLabel: 'operator_report',
    observedAt: '2026-07-30T10:00:00Z',
    needsReview: false,
    createdAt: '2026-07-30T10:00:00Z',
    ...overrides,
  })
  const goal: GoalNode = {
    id: 'goal-1',
    ownerIdentity: 'owner-1',
    level: 'pursuit',
    domainIds: [domain.id],
    title: 'Restore daily capacity',
    successCriteria: ['Energy is stable'],
    stopConditions: ['Stop if symptoms worsen'],
    status: 'active',
    confidence: 0.8,
    sourceLabel: 'operator_goal',
    createdAt: '2026-07-30T10:00:00Z',
    updatedAt: '2026-07-30T10:00:00Z',
  }

  beforeEach(() => {
    service = jasmine.createSpyObj<LifeOpsService>('LifeOpsService', [
      'overview', 'domains', 'needs', 'latestCapacity', 'goals', 'goalForest',
      'recordNeed', 'recordCapacity', 'linkEntity', 'entityDomains',
      'createGoal', 'updateGoal', 'assessPriority',
    ])
    notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error'])
    service.domains.and.returnValue(of([domain]))
    service.needs.and.returnValue(of([need('newest'), need('older')]))
    service.latestCapacity.and.returnValue(of(null))
    service.goals.and.returnValue(of([goal]))
    service.goalForest.and.returnValue(of([{ goal, children: [] }]))
    service.overview.and.returnValue(of({
      domains: [domain], needs: [need('newest'), need('older')], capacity: null,
      goals: [goal], forest: [{ goal, children: [] }],
    }))
    component = new LifeOpsComponent(service, notification)
  })

  it('loads a calm owner summary and keeps only the latest need per domain', () => {
    component.ngOnInit()

    expect(component.loading).toBeFalse()
    expect(component.errorMessage).toBe('')
    expect(component.currentNeeds.map((entry) => entry.id)).toEqual(['newest'])
    expect(component.capacityState).toBe('missing')
    expect(component.activeGoals).toEqual([goal])
    expect(component.goalForest.length).toBe(1)
  })

  it('shows review state for stale or explicitly reviewable context', () => {
    const capacity = {
      id: 'capacity-1',
      ownerIdentity: 'owner-1',
      status: 'available',
      signals: component.capacityForm.signals,
      timeAvailableMinutes: 60,
      concurrentWorkLimit: 1,
      currentLoad: 30,
      planningStepLimit: 4,
      constraints: [],
      sourceLabel: 'operator_report',
      capturedAt: '2026-07-28T10:00:00Z',
      confidence: 0.5,
      fresh: false,
      needsReview: true,
      createdAt: '2026-07-28T10:00:00Z',
    } as CapacitySnapshot
    service.overview.and.returnValue(of({
      domains: [domain], needs: [need('review', { needsReview: true })], capacity,
      goals: [goal], forest: [{ goal, children: [] }],
    }))

    component.refresh()

    expect(component.capacityState).toBe('review')
    expect(component.reviewNeeds.length).toBe(1)
  })

  it('records a need without inventing owner identity and updates the visible state', () => {
    component.ngOnInit()
    const saved = need('saved')
    service.recordNeed.and.returnValue(of(saved))
    Object.assign(component.needForm, {
      domainId: domain.id,
      needLevel: 'health',
      state: 'active',
      currentLevel: 45,
      targetLevel: 75,
      priority: 70,
      confidence: 0.8,
      sourceLabel: 'owner report',
    })

    component.saveNeed()

    expect(service.recordNeed).toHaveBeenCalledWith(jasmine.objectContaining({
      domainId: domain.id,
      currentLevel: 45,
      targetLevel: 75,
      priority: 70,
      confidence: 0.8,
      sourceLabel: 'owner report',
    }))
    expect('ownerIdentity' in service.recordNeed.calls.mostRecent().args[0]).toBeFalse()
    expect(component.needs[0].id).toBe('saved')
    expect(notification.success).toHaveBeenCalled()
  })

  it('keeps blank numeric fields and goal level out of API requests', () => {
    component.ngOnInit()

    expect(component.needForm.currentLevel).toBeNull()
    expect(component.canSaveNeed).toBeFalse()
    component.saveNeed()
    expect(service.recordNeed).not.toHaveBeenCalled()

    component.goalForm.title = 'Owner-defined goal'
    component.goalForm.sourceLabel = 'owner input'
    component.goalForm.confidence = 0.7
    expect(component.goalForm.level).toBe('')
    expect(component.canSaveGoal).toBeFalse()
    component.saveGoal()
    expect(service.createGoal).not.toHaveBeenCalled()
    expect(service.updateGoal).not.toHaveBeenCalled()
  })

  it('requires explicit capacity numbers instead of treating blank values as zero', () => {
    component.ngOnInit()

    expect(component.capacityForm.signals.energy).toBeNull()
    expect(component.capacityForm.currentLoad).toBeNull()
    expect(component.canSaveCapacity).toBeFalse()
    component.saveCapacity()

    expect(service.recordCapacity).not.toHaveBeenCalled()
  })

  it('requires explicit priority factor estimates before assessment', () => {
    component.ngOnInit()
    Object.assign(component.priorityForm, {
      title: 'Owner-defined task',
      entityType: 'task',
      entityId: 'task-1',
    })

    expect(component.priorityForm.factors.importance).toBeNull()
    expect(component.canAssessPriority).toBeFalse()
    component.assessPriority()

    expect(service.assessPriority).not.toHaveBeenCalled()
  })

  it('sends a goal level only after matching an allowed level', () => {
    component.ngOnInit()
    service.createGoal.and.returnValue(of(goal))
    Object.assign(component.goalForm, {
      level: 'pursuit',
      domainIds: [domain.id],
      title: goal.title,
      status: goal.status,
      confidence: goal.confidence,
      sourceLabel: goal.sourceLabel,
    })

    expect(component.goalRequiresExecutionDetails).toBeTrue()
    expect(component.canSaveGoal).toBeFalse()
    component.saveGoal()
    expect(service.createGoal).not.toHaveBeenCalled()

    component.goalForm.successCriteria = goal.successCriteria.join('\n')
    component.goalForm.stopConditions = goal.stopConditions.join('\n')
    expect(component.canSaveGoal).toBeTrue()
    component.saveGoal()

    expect(service.createGoal).toHaveBeenCalledWith(jasmine.objectContaining({ level: 'pursuit' }))
  })

  it('preserves a useful API failure state instead of showing empty data', () => {
    service.overview.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 503,
      error: { error: 'whole-life context operation failed' },
    })))

    component.refresh()

    expect(component.loading).toBeFalse()
    expect(component.errorMessage).toBe('whole-life context operation failed')
  })

  it('keeps review and correction actions in Basic and defers raw capacity constraints to Advanced', async () => {
    const capacity: CapacitySnapshot = {
      id: 'capacity-raw-1', ownerIdentity: 'owner-1', status: 'constrained',
      signals: { energy: 40, attentionQuality: 50, painIllnessLoad: 0, sleepQuality: 50, stressLoad: 20, mobility: 50, financialLiquidity: 50, deadlinePressure: 30, interruptionSensitivity: 50, recoveryRequirement: 0, taskSwitchingCost: 50, sensoryLoad: 0, decisionFatigue: 0, riskTolerance: 50, confidenceReadiness: 50, location: '', weatherConditions: '', environmentalConditions: '', socialAppropriateness: '' },
      timeAvailableMinutes: 60, concurrentWorkLimit: 1, currentLoad: 70, planningStepLimit: 3,
      constraints: ['Do not schedule calls after 15:00'], sourceLabel: 'owner_report',
      capturedAt: '2026-09-24T08:00:00Z', confidence: 0.6, fresh: false, needsReview: true,
      createdAt: '2026-09-24T08:00:00Z',
    }
    const overview = {
      domains: [domain], needs: [need('review-needed', { needsReview: true })], capacity,
      goals: [goal], forest: [{ goal, children: [] }],
    }
    await TestBed.configureTestingModule({
      imports: [LifeOpsModule],
      providers: [
        { provide: LifeOpsService, useValue: { overview: () => of(overview) } },
        { provide: NzNotificationService, useValue: { success: jasmine.createSpy('success'), error: jasmine.createSpy('error') } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(LifeOpsComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement

    expect(host.querySelector('.attention-band')?.textContent).toContain('Owner review is needed')
    expect(Array.from(host.querySelectorAll('.attention-band button')).some((button) => button.textContent?.includes('Record review'))).toBeTrue()
    expect(Array.from(host.querySelectorAll('.capacity-panel button')).some((button) => button.textContent?.includes('Record update'))).toBeTrue()
    expect(host.querySelector('.constraint-list')).toBeNull()
    expect(host.textContent).not.toContain('Do not schedule calls after 15:00')

    TestBed.inject(ModuleViewPreferencesService).setMode('life-ops', 'advanced')
    fixture.detectChanges()
    const capacityToggle = Array.from(host.querySelectorAll('.hai-progressive-section__summary')).find((button) => button.textContent?.includes('Capacity record')) as HTMLButtonElement
    capacityToggle.click()
    fixture.detectChanges()
    expect(host.querySelector('.constraint-list')?.textContent).toContain('Do not schedule calls after 15:00')
  })
})

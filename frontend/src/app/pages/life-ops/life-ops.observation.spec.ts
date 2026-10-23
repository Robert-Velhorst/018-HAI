import { fakeAsync, tick } from '@angular/core/testing'
import { EMPTY, of, Subject, throwError } from 'rxjs'
import { LifeOpsComponent } from './life-ops.component'

describe('Life Ops request lifecycle', () => {
  let component: LifeOpsComponent
  let service: any
  let notifications: any
  const overview = { domains: [], needs: [], capacity: null, goals: [], forest: [] }
  const cases: Array<[string, () => void, () => jasmine.Spy, () => boolean]> = [
    ['overview', () => component.refresh(), () => service.overview, () => component.loading],
    ['need', () => component.saveNeed(), () => service.recordNeed, () => component.saving],
    ['capacity', () => component.saveCapacity(), () => service.recordCapacity, () => component.saving],
    ['link write', () => component.saveLink(), () => service.linkEntity, () => component.saving],
    ['link read', () => component.loadEntityLinks(), () => service.entityDomains, () => component.entityLinksState === 'loading'],
    ['goal create', () => component.saveGoal(), () => service.createGoal, () => component.saving],
    ['goal update', () => { component.editingGoalId = 'synthetic-goal'; component.saveGoal() }, () => service.updateGoal, () => component.saving],
    ['priority', () => component.assessPriority(), () => service.assessPriority, () => component.saving],
  ]
  beforeEach(() => {
    service = jasmine.createSpyObj('life', ['overview', 'recordNeed', 'recordCapacity', 'linkEntity', 'entityDomains', 'createGoal', 'updateGoal', 'assessPriority'])
    notifications = jasmine.createSpyObj('notification', ['success', 'error'])
    component = new LifeOpsComponent(service, notifications)
    component.domains = [{ id: 'work', name: 'Work' }] as any
    Object.assign(component.needForm, { domainId: 'work', needLevel: 'work', state: 'active', currentLevel: 40, targetLevel: 60, priority: 50, confidence: 0.8, sourceLabel: 'operator' })
    Object.assign(component.capacityForm, { status: 'available', timeAvailableMinutes: 60, concurrentWorkLimit: 1, currentLoad: 20, planningStepLimit: 4, confidence: 0.8, sourceLabel: 'operator' })
    for (const field of component.capacityNumericFields) component.capacityForm.signals[field.key] = 50
    Object.assign(component.linkForm, { entityType: 'pursuit', entityId: 'synthetic', domainId: 'work', confidence: 0.8, sourceLabel: 'operator' })
    Object.assign(component.goalForm, { level: 'project', domainIds: ['work'], title: 'Synthetic goal', successCriteria: 'Verified result', stopConditions: 'Stop on missing authority', confidence: 0.8, sourceLabel: 'operator' })
    Object.assign(component.priorityForm, { entityType: 'pursuit', entityId: 'synthetic', title: 'Synthetic priority' })
    for (const field of component.priorityFields) component.priorityForm.factors[field.key] = 50
  })
  afterEach(() => (component as any).ngOnDestroy?.())

  for (const [name, invoke, spy, busy] of cases) {
    it(`contains synchronous ${name} setup failures`, () => {
      spy().and.callFake(() => { throw new Error('PRIVATE_PROVIDER_DETAIL') })
      expect(invoke).not.toThrow()
      expect(busy()).toBeFalse()
    })
    it(`rejects empty ${name} completion without keeping a busy state`, () => {
      spy().and.returnValue(EMPTY)
      invoke()
      expect(busy()).toBeFalse()
      expect(notifications.success).not.toHaveBeenCalled()
    })
    it(`cancels ${name} on destruction`, () => {
      const pending = new Subject<never>()
      spy().and.returnValue(pending)
      invoke()
      expect(pending.observers.length).toBe(1)
      ;(component as any).ngOnDestroy?.()
      expect(pending.observers.length).toBe(0)
      expect(busy()).toBeFalse()
    })
    it(`does not dispatch ${name} after destruction`, () => {
      ;(component as any).ngOnDestroy?.()
      invoke()
      expect(spy()).not.toHaveBeenCalled()
    })
    it(`bounds silent ${name} observation without retrying`, fakeAsync(() => {
      const pending = new Subject<never>()
      spy().and.returnValue(pending)
      invoke()
      tick(30001)
      expect(pending.observers.length).toBe(0)
      expect(busy()).toBeFalse()
      expect(spy()).toHaveBeenCalledTimes(1)
    }))
    it(`does not expose raw ${name} error details`, () => {
      spy().and.returnValue(throwError(() => new Error('PRIVATE_PROVIDER_DETAIL')))
      invoke()
      expect(JSON.stringify([component.errorMessage, component.entityLinksError, notifications.error.calls.allArgs()])).not.toContain('PRIVATE_PROVIDER_DETAIL')
    })
  }

  for (const [name, invoke, spy] of cases.filter(([name]) => !['overview', 'link read'].includes(name))) {
    it(`retains the form after an unusable ${name} acknowledgement`, () => {
      const form = component.goalForm
      spy().and.returnValue(of(null))
      invoke()
      expect(component.saving).toBeFalse()
      expect(component.goalForm).toBe(form)
      expect(notifications.success).not.toHaveBeenCalled()
      expect(notifications.error).toHaveBeenCalledWith('Life Ops outcome unconfirmed', jasmine.stringMatching(/may have been recorded/))
    })
    it(`prevents duplicate pending ${name} dispatch`, () => {
      spy().and.returnValue(new Subject<any>())
      invoke(); invoke()
      expect(spy()).toHaveBeenCalledTimes(1)
      expect(component.saving).toBeTrue()
    })
    it(`accepts a confirmed ${name} acknowledgement once`, () => {
      service.overview.and.returnValue(of(overview))
      service.entityDomains.and.returnValue(of([]))
      spy().and.callFake((first: any, second: any) => {
        const data = name === 'goal update' ? second : first
        return of({ ...data, id: 'synthetic-record', ownerIdentity: 'operator', gap: 20, fresh: true, createdAt: '2026-10-23T00:00:00Z', updatedAt: '2026-10-23T00:00:00Z', score: 50, band: 'medium', contributions: [], reasons: [], capacityApplied: false, algorithmVersion: 'test', assessedAt: '2026-10-23T00:00:00Z' })
      })
      invoke()
      expect(component.saving).toBeFalse()
      expect(spy()).toHaveBeenCalledTimes(1)
      expect(notifications.success).toHaveBeenCalled()
      expect(notifications.error).not.toHaveBeenCalled()
    })
  }

  it('supersedes an entity read even after switching away and back', () => {
    const old = new Subject<any>(); const current = new Subject<any>()
    service.entityDomains.and.returnValues(old, of([]), current)
    component.loadEntityLinks()
    component.linkForm.entityId = 'other'; component.loadEntityLinks()
    component.linkForm.entityId = 'synthetic'; component.loadEntityLinks()
    expect(old.observers.length).toBe(0)
    const links = [{ id: 'current' }]
    current.next(links)
    old.next([{ id: 'obsolete' }])
    expect(component.entityLinks).toBe(links as any)
    expect(current.observers.length).toBe(0)
  })

  it('cancels an earlier overview before dispatching a mutation', () => {
    const old = new Subject<any>(); const pendingWrite = new Subject<any>()
    service.overview.and.returnValue(old)
    service.recordNeed.and.returnValue(pendingWrite)
    component.refresh(); component.saveNeed()
    expect(old.observers.length).toBe(0)
    expect(component.loading).toBeFalse()
    expect(component.saving).toBeTrue()
    old.next(overview)
    expect(component.domains.length).toBe(1)
  })

  it('keeps a pending mutation editor intact', () => {
    service.recordNeed.and.returnValue(new Subject<any>())
    component.openEditor('need'); component.saveNeed()
    component.openEditor('goal')
    expect(component.editor).toBe('need')
  })

  it('uses only strict fresh reviewed capacity for a priority boundary', () => {
    for (const capacity of [{ fresh: 'true', needsReview: false, status: 'available' }, { fresh: true, needsReview: 'false', status: 'available' }, { fresh: true, needsReview: false, status: 'invalid' }]) {
      component.capacity = capacity as any
      expect(component.canApplyCapacityToPriority).toBeFalse()
    }
    component.capacity = { fresh: true, needsReview: false, status: 'constrained' } as any
    expect(component.canApplyCapacityToPriority).toBeTrue()
  })
})

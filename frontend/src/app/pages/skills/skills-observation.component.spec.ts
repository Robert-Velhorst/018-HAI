import { fakeAsync, tick } from '@angular/core/testing'
import { EMPTY, NEVER, of, Subject } from 'rxjs'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { ISkillSelectionInventory } from '../../models/skills.model.interface'
import { SkillsComponent } from './skills.component'

describe('Skills bounded observations', () => {
  const owner: IAuthSession = {
    authenticated: true, subject: 'synthetic-owner', role: 'owner',
    permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true },
  }
  function make() {
    const inventory: ISkillSelectionInventory = {
      sourceRepository: 'anthropics/skills', sourceCommit: 'a'.repeat(40),
      sourceCommitDate: '2026-10-23T00:00:00Z', catalogFingerprint: 'b'.repeat(64),
      skills: [{ id: 'fixture', name: 'Fixture', description: 'Synthetic guidance', license: 'Apache-2.0',
        sourcePath: 'skills/fixture/SKILL.md', sourceURL: 'https://example.test/source',
        sourceSHA256: 'c'.repeat(64), guidanceSHA256: 'd'.repeat(64), scope: 'advisory',
        status: 'available', enabled: true, needsReapproval: false }],
    }
    const service = jasmine.createSpyObj('skills', ['inventory', 'guidancePreview', 'setEnabled'])
    service.inventory.and.returnValue(of(inventory))
    service.guidancePreview.and.returnValue(EMPTY)
    service.setEnabled.and.returnValue(EMPTY)
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(owner)) }
    const component = new SkillsComponent(service, auth as never)
    spyOn<any>(component, 'focusAfterRender').and.stub()
    return { component, service, auth, inventory, skill: inventory.skills[0] }
  }
  function dispose(component: SkillsComponent) {
    const lifecycle = component as SkillsComponent & { ngOnDestroy?: () => void }
    lifecycle.ngOnDestroy?.()
  }

  it('treats empty session and inventory responses as unavailable, not perpetual loading', () => {
    const f = make()
    f.auth.session.and.returnValue(EMPTY)
    f.service.inventory.and.returnValue(EMPTY)
    f.component.ngOnInit()
    expect(f.component.loading).toBeFalse()
    expect(f.component.sessionLoading).toBeFalse()
    expect(f.component.unavailable).toBeTrue()
    expect(f.component.sessionUnavailable).toBeTrue()
    expect(f.component.canApprove).toBeFalse()
    dispose(f.component)
  })

  it('bounds stalled session and inventory reads and permits an explicit refresh afterward', fakeAsync(() => {
    const f = make()
    f.auth.session.and.returnValue(NEVER)
    f.service.inventory.and.returnValue(NEVER)
    f.component.ngOnInit()
    tick(10001)
    expect(f.component.loading).toBeFalse()
    expect(f.component.sessionLoading).toBeFalse()
    expect(f.component.unavailable).toBeTrue()
    f.auth.session.and.returnValue(of(owner))
    f.service.inventory.and.returnValue(of(f.inventory))
    f.component.refresh()
    expect(f.service.inventory).toHaveBeenCalledTimes(2)
    expect(f.component.unavailable).toBeFalse()
    dispose(f.component)
  }))

  it('clears an empty guidance read without authorizing enablement', () => {
    const f = make()
    f.component.ngOnInit()
    f.component.reviewGuidance(f.skill)
    expect(f.component.isGuidancePreviewLoading(f.skill)).toBeFalse()
    expect(f.component.guidancePreviewErrors[f.skill.id]).toContain('could not load')
    expect(f.component.isEnableSelectionDisabled(f.skill)).toBeTrue()
    dispose(f.component)
  })

  it('bounds a stalled guidance preview without granting approval', fakeAsync(() => {
    const f = make()
    f.service.guidancePreview.and.returnValue(NEVER)
    f.component.ngOnInit()
    f.component.reviewGuidance(f.skill)
    tick(10001)
    expect(f.component.isGuidancePreviewLoading(f.skill)).toBeFalse()
    expect(f.component.guidancePreviewErrors[f.skill.id]).toContain('could not load')
    expect(f.component.hasCurrentGuidancePreview(f.skill)).toBeFalse()
    dispose(f.component)
  }))

  it('requires a refresh after an empty selection response rather than reporting saved', () => {
    const f = make()
    f.component.ngOnInit()
    f.component.updateSelection(f.skill, false)
    expect(f.component.isPending(f.skill)).toBeFalse()
    expect(f.component.selectionRequiresRefreshIds.has(f.skill.id)).toBeTrue()
    expect(f.component.selectionErrors[f.skill.id]).toContain('could not confirm')
    expect(f.component.selectionAnnouncement).toBe('')
    expect(f.skill.enabled).toBeTrue()
    dispose(f.component)
  })

  it('treats a timed-out selection as uncertain and never resubmits it automatically', fakeAsync(() => {
    const f = make()
    f.service.setEnabled.and.returnValue(NEVER)
    f.component.ngOnInit()
    f.component.updateSelection(f.skill, false)
    tick(10001)
    expect(f.component.isPending(f.skill)).toBeFalse()
    expect(f.component.selectionRequiresRefreshIds.has(f.skill.id)).toBeTrue()
    expect(f.component.selectionErrors[f.skill.id]).toContain('could not confirm')
    f.component.updateSelection(f.skill, false)
    expect(f.service.setEnabled).toHaveBeenCalledTimes(1)
    dispose(f.component)
  }))

  it('unblocks an empty legacy audit refresh but requires a new inventory before another selection', () => {
    const f = make()
    f.component.ngOnInit()
    f.service.inventory.and.returnValue(EMPTY)
    f.service.setEnabled.and.returnValue(of({ enabled: false, needsReapproval: false }))
    f.component.updateSelection(f.skill, false)
    expect(f.component.isPending(f.skill)).toBeFalse()
    expect(f.component.selectionRequiresRefreshIds.has(f.skill.id)).toBeTrue()
    expect(f.component.selectionErrors[f.skill.id]).toContain('latest audit details could not be refreshed')
    dispose(f.component)
  })

  it('cancels page reads on destruction and ignores late results or refresh attempts', () => {
    const f = make()
    const inventory = new Subject<ISkillSelectionInventory>()
    const session = new Subject<IAuthSession>()
    f.service.inventory.and.returnValue(inventory)
    f.auth.session.and.returnValue(session)
    f.component.ngOnInit()
    expect(inventory.observers.length).toBe(1)
    expect(session.observers.length).toBe(1)
    dispose(f.component)
    expect(inventory.observers.length).toBe(0)
    expect(session.observers.length).toBe(0)
    inventory.next(f.inventory)
    session.next(owner)
    f.component.refresh()
    expect(f.component.inventory).toBeUndefined()
    expect(f.component.canApprove).toBeFalse()
    expect(f.service.inventory).toHaveBeenCalledTimes(1)
  })

  it('detaches a pending selection on destruction without claiming the server rejected it', () => {
    const f = make()
    const result = new Subject<{ enabled: boolean; needsReapproval: boolean }>()
    f.service.setEnabled.and.returnValue(result)
    f.component.ngOnInit()
    f.component.updateSelection(f.skill, false)
    expect(result.observers.length).toBe(1)
    dispose(f.component)
    expect(result.observers.length).toBe(0)
    result.next({ enabled: false, needsReapproval: false })
    expect(f.skill.enabled).toBeTrue()
    expect(f.component.selectionAnnouncement).toBe('')
    expect(f.component.canApprove).toBeFalse()
  })

  it('invalidates an asynchronous guidance verification that finishes after destruction', async () => {
    const f = make()
    let finish!: (value: boolean) => void
    const verification = new Promise<boolean>((resolve) => { finish = resolve })
    spyOn<any>(f.component, 'matchesCurrentGuidance').and.returnValue(verification)
    f.service.guidancePreview.and.returnValue(of({ skillId: f.skill.id,
      currentGuidance: 'Synthetic guidance', currentGuidanceSHA256: f.skill.guidanceSHA256,
      catalogFingerprint: f.inventory.catalogFingerprint, previousGuidanceTextStored: false }))
    f.component.ngOnInit()
    f.component.reviewGuidance(f.skill)
    expect(f.component.isGuidancePreviewLoading(f.skill)).toBeTrue()
    dispose(f.component)
    finish(true)
    await verification
    await Promise.resolve()
    expect(f.component.guidancePreviews[f.skill.id]).toBeUndefined()
    expect(f.component.hasCurrentGuidancePreview(f.skill)).toBeFalse()
    expect(f.component.isGuidancePreviewLoading(f.skill)).toBeFalse()
  })
})

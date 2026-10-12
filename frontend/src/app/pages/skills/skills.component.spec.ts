import { Observable, of, Subject, throwError } from 'rxjs'
import { TestBed } from '@angular/core/testing'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { ISkillGuidancePreview, ISkillSelectionInventory, ISkillSelectionState } from '../../models/skills.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { SkillsService } from '../../services/skills.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { SkillsComponent } from './skills.component'
import { SkillsModule } from './skills.module'

describe('SkillsComponent', () => {
  const viewPreferenceKey = 'hai.module-view.v1.skills'
  const pinnedCommit = 'a'.repeat(40)
  const approverSession: IAuthSession = {
    authenticated: true,
    subject: 'owner-1',
    role: 'owner',
    permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true },
  }
  const viewerSession: IAuthSession = {
    ...approverSession,
    role: 'viewer',
    permissions: { canRead: true, canOperate: false, canApprove: false, canAdminister: false },
  }
  const inventory: ISkillSelectionInventory = {
    sourceRepository: 'anthropics/skills',
    sourceCommit: pinnedCommit,
    sourceCommitDate: '2026-09-10T19:44:08Z',
    catalogFingerprint: 'd'.repeat(64),
    skills: [
      { id: 'frontend-design', name: 'Frontend design', description: 'Design guidance', license: 'Apache-2.0', licenseURL: `https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/frontend-design/LICENSE.txt`, licensePath: 'skills/frontend-design/LICENSE.txt', licenseSHA256: 'license-a', sourcePath: 'skills/frontend-design/SKILL.md', sourceURL: `https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/frontend-design/SKILL.md`, sourceSHA256: 'source-a', guidanceSHA256: '927c7fa8b4a61dd5a3376a52f5db0ce02d434774ddb3576669d1188ffcd22af7', scope: 'advisory', status: 'available', enabled: true, needsReapproval: false, selectionDecision: { id: 23, actorIdentity: 'owner-1', decidedAt: '2026-09-10T19:44:08Z' } },
      { id: 'mcp-builder', name: 'MCP builder', description: 'Connector guidance', license: 'Apache-2.0', licenseURL: `https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/mcp-builder/LICENSE.txt`, licensePath: 'skills/mcp-builder/LICENSE.txt', sourcePath: 'skills/mcp-builder/SKILL.md', sourceURL: `https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/mcp-builder/SKILL.md`, sourceSHA256: 'source-b', guidanceSHA256: 'f43825fa4cbf75b32c397a0766a69a4aa47b1ea24f7976c38a9bb8373ebe823a', scope: 'advisory', status: 'available', enabled: false, needsReapproval: true },
    ],
  }

  function copyInventory(value: ISkillSelectionInventory): ISkillSelectionInventory {
    return {
      ...value,
      skills: value.skills.map((skill) => ({
        ...skill,
        selectionDecision: skill.selectionDecision ? { ...skill.selectionDecision } : skill.selectionDecision,
      })),
    }
  }

  function guidancePreview(skillId: string, needsReapproval?: boolean, catalogFingerprint = inventory.catalogFingerprint): ISkillGuidancePreview {
    const skill = inventory.skills.find((entry) => entry.id === skillId)!
    const requiresReview = needsReapproval ?? skill.needsReapproval
    return {
      skillId,
      currentGuidance: `Exact HAI-authored guidance for ${skillId}.`,
      currentGuidanceSHA256: skill.guidanceSHA256,
      catalogFingerprint,
      previousGuidanceTextStored: false,
      ...(requiresReview ? {
        previousApprovedGuidanceSHA256: 'c'.repeat(64),
        previousGuidanceTextLimitation: 'HAI stores the prior approval hash, not its historical text.',
      } : {}),
    }
  }

  beforeEach(() => {
    window.localStorage.removeItem(viewPreferenceKey)
    document.body.classList.remove('hai-view-advanced')
  })
  afterEach(() => {
    window.localStorage.removeItem(viewPreferenceKey)
    document.body.classList.remove('hai-view-advanced')
  })

  function useAdvancedSkillsView(): void {
    window.localStorage.setItem(viewPreferenceKey, JSON.stringify({
      version: 1,
      mode: 'advanced',
      openSections: {},
      navigationMode: 'auto',
    }))
  }

  async function createFixture(
    sessionResponse: Observable<IAuthSession> = of(approverSession),
    inventoryResponse: Observable<ISkillSelectionInventory> = of(copyInventory(inventory)),
  ) {
    const skills = {
      inventory: jasmine.createSpy('inventory').and.returnValue(inventoryResponse),
      guidancePreview: jasmine.createSpy('guidancePreview').and.callFake((id: string) => of(guidancePreview(id))),
      setEnabled: jasmine.createSpy('setEnabled').and.returnValue(of({ enabled: true, needsReapproval: false })),
    }
    const auth = { session: jasmine.createSpy('session').and.returnValue(sessionResponse) }
    await TestBed.configureTestingModule({
      imports: [SkillsModule],
      providers: [
        { provide: SkillsService, useValue: skills },
        { provide: AuthSessionService, useValue: auth },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(SkillsComponent)
    fixture.detectChanges()
    return { fixture, skills, auth }
  }

  it('loads server owner selection separately from catalog registration', () => {
    const service = { inventory: jasmine.createSpy('inventory').and.returnValue(of(inventory)) }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)

    component.ngOnInit()

    expect(service.inventory).toHaveBeenCalledTimes(1)
    expect(auth.session).toHaveBeenCalledTimes(1)
    expect(component.inventory).toEqual(inventory)
    expect(component.enabledSkills).toBe(1)
    expect(component.visibleSkills.map((skill) => skill.id)).toEqual(['frontend-design', 'mcp-builder'])
  })

  it('shows each skill source pinned to the exact upstream commit in Basic view', async () => {
    const { fixture } = await createFixture()
    const host = fixture.nativeElement as HTMLElement
    const sourceLink = host.querySelector('.skills-page__list .skills-page__source a') as HTMLAnchorElement

    expect(sourceLink.textContent?.trim()).toBe(`anthropics/skills @ ${pinnedCommit.slice(0, 7)}`)
    expect(sourceLink.href).toBe(`https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/frontend-design/SKILL.md`)
    expect(sourceLink.target).toBe('_blank')
    expect(sourceLink.rel).toContain('noopener')
    expect(sourceLink.getAttribute('aria-label')).toContain(`at commit ${pinnedCommit}`)
    expect(sourceLink.getAttribute('aria-label')).toContain('opens in a new tab')
    expect(getComputedStyle(sourceLink).overflowWrap).toBe('anywhere')
    expect(getComputedStyle(sourceLink.parentElement!).whiteSpace).toBe('normal')
    expect(host.querySelector('.skills-page__provenance')).toBeNull()
  })

  it('shows an unavailable state and permits a read-only retry', () => {
    const service = { inventory: jasmine.createSpy('inventory').and.returnValues(throwError(() => new Error('offline')), of({ ...inventory, skills: [] })) }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)

    component.refresh()
    expect(component.unavailable).toBeTrue()
    expect(component.loading).toBeFalse()

    component.refresh()
    expect(component.unavailable).toBeFalse()
    expect(component.inventory?.skills).toEqual([])
    expect(service.inventory).toHaveBeenCalledTimes(2)
  })

  it('shows approval-gated actions to an approver and exposes optional audit and license details in Advanced inventory', async () => {
    useAdvancedSkillsView()
    const { fixture, skills } = await createFixture()
    const host = fixture.nativeElement as HTMLElement

    expect(host.querySelector('h1')?.textContent).toContain('Skills')
    expect(host.querySelector('.skills-page__summary')?.textContent).toContain('1 enabled')
    expect(host.querySelector('.skills-page__list')?.textContent).toContain('Disable')
    expect(host.querySelector('.skills-page__list')?.textContent).toContain('Review updated guidance')
    expect(host.querySelector('.skills-page__list')?.textContent).not.toContain('Confirm current version and enable')
    const listButtons = Array.from(host.querySelectorAll('.skills-page__list button')).map((button) => button.textContent?.trim())
    expect(listButtons).toContain('Disable')
    expect(listButtons).not.toContain('Enable')
    const advancedSummary = host.querySelector('.hai-progressive-section__summary')?.textContent ?? ''
    expect(advancedSummary).toContain('Inventory and provenance')
    expect(advancedSummary).toContain('current selection provenance')
    expect(advancedSummary.toLowerCase()).not.toContain('history')
    expect(host.querySelector('.skills-page__provenance')).toBeNull()

    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
    fixture.detectChanges()
    const records = host.querySelector('.skills-page__records') as HTMLElement
    expect(host.querySelector('.skills-page__provenance')?.textContent).toContain(pinnedCommit)
    expect(records.textContent).toContain('Apache-2.0')
    expect(records.textContent).toContain('skills/frontend-design/LICENSE.txt')
    expect(records.textContent).toContain('License SHA-256')
    expect(records.textContent).toContain('license-a')
    expect(records.textContent).toContain('Guidance SHA-256')
    expect(records.textContent).toContain('Selection decision ID')
    expect(records.textContent).toContain('23')
    expect(records.textContent).toContain('owner-1')
    expect(records.textContent).toContain('Decision time')
    const licenseLink = records.querySelector(`a[href="https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/frontend-design/LICENSE.txt"]`) as HTMLAnchorElement
    expect(licenseLink?.textContent).toContain('Apache-2.0 license')
    expect(licenseLink?.getAttribute('aria-label')).toBe('Apache-2.0 license for Frontend design (opens in a new tab)')
    expect(licenseLink?.target).toBe('_blank')
    expect(licenseLink?.rel).toContain('noopener')
    const sourceLink = records.querySelector(`a[href="https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/frontend-design/SKILL.md"]`) as HTMLAnchorElement
    expect(sourceLink?.getAttribute('aria-label')).toBe(`View pinned source for Frontend design at commit ${pinnedCommit} (opens in a new tab)`)
    expect(records.querySelector(`li:nth-child(2) a[href="https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/mcp-builder/LICENSE.txt"]`)).not.toBeNull()
    expect(records.querySelector(`li:nth-child(2) a[href="https://github.com/anthropics/skills/blob/${pinnedCommit}/skills/mcp-builder/SKILL.md"]`)).not.toBeNull()
    expect(records.querySelectorAll('li')[1].textContent).toContain('Apache-2.0')
    expect(records.querySelectorAll('li')[1].textContent).toContain('Not provided')
    expect(host.textContent).not.toContain('SKILL.md body')
    expect(skills.inventory).toHaveBeenCalledTimes(1)
  })

  it('hides all selection controls for a viewer while keeping inventory readable', async () => {
    useAdvancedSkillsView()
    const { fixture, skills } = await createFixture(of(viewerSession))
    const host = fixture.nativeElement as HTMLElement

    expect(host.querySelectorAll('.skills-page__selection button').length).toBe(0)
    expect(host.querySelector('.skills-page__access')?.textContent).toContain('does not have approval permission')
    expect(host.querySelector('.skills-page__list')?.textContent).toContain('Frontend design')
    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
    fixture.detectChanges()
    expect(host.querySelectorAll('.skills-page__records .skills-page__selection button').length).toBe(0)
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('fails closed when session lookup is unavailable and does not attempt a selection mutation', async () => {
    const { fixture, skills } = await createFixture(throwError(() => new Error('session unavailable')))
    const host = fixture.nativeElement as HTMLElement
    const component = fixture.componentInstance

    expect(component.canApprove).toBeFalse()
    expect(host.querySelectorAll('.skills-page__selection button').length).toBe(0)
    expect(host.querySelector('.skills-page__access')?.textContent).toContain('could not verify your account permissions')
    expect(host.querySelector('.skills-page__access')?.getAttribute('role')).toBe('status')
    expect(host.querySelector('[role="alert"]')?.textContent ?? '').not.toContain('saved')

    component.updateSelection(inventory.skills[0], false)
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('submits actions only for a confirmed approver and updates the displayed state', async () => {
    useAdvancedSkillsView()
    const testInventory = { ...inventory, skills: inventory.skills.map((skill) => ({ ...skill })) }
    const { fixture, skills } = await createFixture(of(approverSession), of(testInventory))
    let decisionId = 100
    skills.setEnabled.and.callFake((_id: string, enabled: boolean) => of({
      enabled,
      needsReapproval: false,
      selectionDecision: { id: ++decisionId, actorIdentity: 'approver-1', decidedAt: '2026-09-24T20:00:00Z' },
    }))
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement

    const disableFrontend = host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement
    expect(disableFrontend).not.toBeNull()
    expect(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]')).not.toBeNull()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    disableFrontend.click()
    fixture.detectChanges()
    expect(skills.setEnabled).toHaveBeenCalledOnceWith('frontend-design', false)
    expect(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for Frontend design"]')).not.toBeNull()
    expect(host.querySelector('.skills-page__list button[aria-label="Enable Frontend design"]')).toBeNull()
    expect(fixture.componentInstance.inventory?.skills[0].selectionDecision).toEqual({ id: 101, actorIdentity: 'approver-1', decidedAt: '2026-09-24T20:00:00Z' })

    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
    fixture.detectChanges()
    const advancedReview = host.querySelector('.skills-page__records button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement
    expect(advancedReview).not.toBeNull()
    expect(advancedReview.getAttribute('aria-controls')).toBeNull()
    advancedReview.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const advancedPreviewId = fixture.componentInstance.guidancePreviewId('mcp-builder', 'inventory')
    expect(advancedReview.getAttribute('aria-controls')).toBe(advancedPreviewId)
    expect(advancedReview.getAttribute('aria-expanded')).toBe('true')
    expect(host.ownerDocument.getElementById(advancedPreviewId)?.getAttribute('role')).toBe('region')
    expect(host.ownerDocument.getElementById(advancedPreviewId)?.getAttribute('aria-live')).toBe('polite')
    expect(host.querySelector('.skills-page__guidance-preview')?.textContent).toContain('Exact HAI-authored guidance for mcp-builder.')
    expect(host.querySelector('.skills-page__guidance-preview')?.textContent).toContain('c'.repeat(64))
    expect(host.querySelector('.skills-page__guidance-preview')?.textContent).toContain('not its historical text')
    const advancedConfirm = host.querySelector('.skills-page__records button[aria-label="Confirm current version and enable MCP builder"]') as HTMLButtonElement
    expect(advancedConfirm).not.toBeNull()
    advancedConfirm.click()
    fixture.detectChanges()
    expect(skills.guidancePreview).toHaveBeenCalledOnceWith('mcp-builder')
    expect(skills.setEnabled).toHaveBeenCalledWith('mcp-builder', true, 'f43825fa4cbf75b32c397a0766a69a4aa47b1ea24f7976c38a9bb8373ebe823a', inventory.catalogFingerprint)
    expect(host.querySelector('.skills-page__records button[aria-label="Disable MCP builder"]')).not.toBeNull()
    expect(fixture.componentInstance.inventory?.skills[1].selectionDecision).toEqual({ id: 102, actorIdentity: 'approver-1', decidedAt: '2026-09-24T20:00:00Z' })
    expect(host.querySelector('.skills-page__records')?.textContent).toContain('approver-1')
    expect(host.querySelector('.skills-page__records')?.textContent).toContain('102')
  })

  it('announces the confirmed selection and restores keyboard focus to the next action', async () => {
    const { fixture, skills } = await createFixture()
    skills.setEnabled.and.returnValue(of({
      enabled: false,
      needsReapproval: false,
      selectionDecision: { id: 24, actorIdentity: 'owner-1', decidedAt: '2026-09-24T20:00:00Z' },
    }))
    const frames: FrameRequestCallback[] = []
    spyOn(window, 'requestAnimationFrame').and.callFake((callback) => {
      frames.push(callback)
      return frames.length
    })
    const host = fixture.nativeElement as HTMLElement
    const disable = host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement

    disable.focus()
    disable.click()
    fixture.detectChanges()

    const announcement = host.querySelector('.skills-page__notice[role="status"]')
    expect(announcement?.textContent).toContain('HAI confirmed Frontend design is disabled')
    expect(frames.length).toBeGreaterThan(0)
    frames.forEach((frame) => frame(0))
    expect(document.activeElement).toBe(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for Frontend design"]'))
  })

  it('keeps selection feedback and keyboard focus available when the view changes during a save', async () => {
    const response = new Subject<ISkillSelectionState>()
    const { fixture, skills } = await createFixture()
    skills.setEnabled.and.returnValue(response)
    const frames: FrameRequestCallback[] = []
    spyOn(window, 'requestAnimationFrame').and.callFake((callback) => {
      frames.push(callback)
      return frames.length
    })
    const host = fixture.nativeElement as HTMLElement
    const disable = host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement

    disable.focus()
    disable.click()
    fixture.detectChanges()

    TestBed.inject(ModuleViewPreferencesService).setMode('skills', 'advanced')
    document.body.classList.add('hai-view-advanced')
    fixture.detectChanges()
    frames.length = 0
    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    expect(advancedToggle).not.toBeNull()
    expect(advancedToggle.getAttribute('aria-expanded')).toBe('false')

    fixture.ngZone?.run(() => response.next({
      enabled: false,
      needsReapproval: false,
      selectionDecision: { id: 24, actorIdentity: 'owner-1', decidedAt: '2026-09-24T20:00:00Z' },
    }))
    await fixture.whenStable()
    fixture.detectChanges()

    expect(skills.setEnabled).toHaveBeenCalledOnceWith('frontend-design', false)
    expect(fixture.componentInstance.selectionAnnouncement).toContain('HAI confirmed Frontend design is disabled')
    expect(host.querySelector('.skills-page__notice[role="status"]')?.textContent).toContain('HAI confirmed Frontend design is disabled')
    for (const frame of frames) {
      frame(0)
      if (document.activeElement === advancedToggle) break
    }
    expect(document.activeElement).toBe(advancedToggle)
  })

  it('requires a fresh inventory after an uncertain mutation before allowing another action', async () => {
    const { fixture, skills } = await createFixture()
    skills.setEnabled.and.returnValue(throwError(() => new Error('offline')))
    const host = fixture.nativeElement as HTMLElement
    const component = fixture.componentInstance
    const disable = host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement

    disable.click()
    fixture.detectChanges()

    expect(component.selectionRequiresRefreshIds.has('frontend-design')).toBeTrue()
    expect(host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]')?.hasAttribute('disabled')).toBeTrue()
    ;(host.querySelector('#skills-refresh') as HTMLButtonElement).click()
    fixture.detectChanges()

    expect(skills.setEnabled).toHaveBeenCalledOnceWith('frontend-design', false)
    expect(skills.inventory).toHaveBeenCalledTimes(2)
    expect(component.selectionRequiresRefreshIds.has('frontend-design')).toBeFalse()
    expect(component.selectionErrors['frontend-design']).toBeUndefined()
    expect((host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement).disabled).toBeFalse()
  })

  it('fails closed and requires refresh when a selection response has an invalid shape', async () => {
    const { fixture, skills } = await createFixture()
    skills.setEnabled.and.returnValue(of({ enabled: 'yes', needsReapproval: false } as unknown as ISkillSelectionState))
    const host = fixture.nativeElement as HTMLElement

    ;(host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement).click()
    fixture.detectChanges()

    expect(fixture.componentInstance.inventory?.skills[0].enabled).toBeTrue()
    expect(fixture.componentInstance.selectionRequiresRefreshIds.has('frontend-design')).toBeTrue()
    expect(host.querySelector('.skills-page__list [role="alert"]')?.textContent).toContain('invalid selection result')
    expect((host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement).disabled).toBeTrue()
  })

  it('rejects attacker-controlled HTTPS source and license links', async () => {
    const unsafeInventory = copyInventory(inventory)
    unsafeInventory.skills[0].sourceURL = 'https://attacker.example/skills/frontend-design/SKILL.md'
    unsafeInventory.skills[0].licenseURL = 'https://attacker.example/skills/frontend-design/LICENSE.txt'
    useAdvancedSkillsView()
    const { fixture } = await createFixture(of(approverSession), of(unsafeInventory))
    const host = fixture.nativeElement as HTMLElement
    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
    fixture.detectChanges()

    const firstRecord = host.querySelector('.skills-page__records li') as HTMLElement
    expect(firstRecord.querySelectorAll('a').length).toBe(0)
    expect(firstRecord.textContent).toContain('Secure source link unavailable')
    expect(firstRecord.textContent).toContain('Apache-2.0')
  })

  const invalidPinnedInventoryCases: Array<{ label: string; mutate: (value: ISkillSelectionInventory) => void }> = [
    { label: 'the repository is not canonical', mutate: (value) => { value.sourceRepository = 'attacker/skills' } },
    { label: 'the commit is not 40 characters', mutate: (value) => { value.sourceCommit = 'a'.repeat(39) } },
    { label: 'the commit is not lowercase', mutate: (value) => { value.sourceCommit = 'A'.repeat(40) } },
    { label: 'the commit pin does not match the provided URLs', mutate: (value) => { value.sourceCommit = 'b'.repeat(40) } },
    { label: 'the skill ID is not a path-safe slug', mutate: (value) => { value.skills[0].id = '../frontend-design' } },
    { label: 'the source path is not the expected skill file', mutate: (value) => { value.skills[0].sourcePath = 'skills/frontend-design/README.md' } },
    { label: 'the license path is not the expected license file', mutate: (value) => { value.skills[0].licensePath = 'skills/frontend-design/LICENSE' } },
  ]

  for (const testCase of invalidPinnedInventoryCases) {
    it(`does not render source or license links when ${testCase.label}`, async () => {
      const invalidInventory = copyInventory(inventory)
      testCase.mutate(invalidInventory)
      useAdvancedSkillsView()
      const { fixture } = await createFixture(of(approverSession), of(invalidInventory))
      const host = fixture.nativeElement as HTMLElement
      const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
      if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
      fixture.detectChanges()

      const firstRecord = host.querySelector('.skills-page__records li') as HTMLElement
      expect(firstRecord.querySelectorAll('a').length).toBe(0)
      expect(firstRecord.textContent).toContain('Secure source link unavailable')
      expect(firstRecord.textContent).toContain('Apache-2.0')
    })
  }

  it('reloads the selected row audit metadata when a successful mutation response omits it', async () => {
    useAdvancedSkillsView()
    const { fixture, skills } = await createFixture()
    const refreshed: ISkillSelectionInventory = {
      ...inventory,
      skills: inventory.skills.map((skill) => skill.id === 'frontend-design'
        ? { ...skill, enabled: false, needsReapproval: false, selectionDecision: { id: 77, actorIdentity: 'latest-approver', decidedAt: '2026-09-24T21:00:00Z' } }
        : skill),
    }
    skills.setEnabled.and.returnValue(of({ enabled: false, needsReapproval: false }))
    skills.inventory.and.returnValue(of(refreshed))
    const host = fixture.nativeElement as HTMLElement

    ;(host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement).click()
    fixture.detectChanges()

    const row = fixture.componentInstance.inventory?.skills[0]
    expect(row?.enabled).toBeFalse()
    expect(row?.selectionDecision).toEqual({ id: 77, actorIdentity: 'latest-approver', decidedAt: '2026-09-24T21:00:00Z' })
    expect(skills.inventory).toHaveBeenCalledTimes(2)
    const advancedToggle = host.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    if (advancedToggle.getAttribute('aria-expanded') !== 'true') advancedToggle.click()
    fixture.detectChanges()
    expect(host.querySelector('.skills-page__records')?.textContent).toContain('latest-approver')
    expect(host.querySelector('.skills-page__records')?.textContent).toContain('77')
  })

  it('refreshes provenance and invalidates reviewed guidance when the catalog changes during a legacy save', async () => {
    const { fixture, skills } = await createFixture()
    const latestCommit = 'b'.repeat(40)
    const latestCatalogFingerprint = 'e'.repeat(64)
    const latestInventory = copyInventory(inventory)
    latestInventory.sourceCommit = latestCommit
    latestInventory.catalogFingerprint = latestCatalogFingerprint
    latestInventory.skills = latestInventory.skills.map((skill) => ({
      ...skill,
      sourceURL: `https://github.com/anthropics/skills/blob/${latestCommit}/${skill.sourcePath}`,
      licenseURL: `https://github.com/anthropics/skills/blob/${latestCommit}/${skill.licensePath}`,
    }))
    latestInventory.skills[1] = {
      ...latestInventory.skills[1],
      enabled: true,
      needsReapproval: false,
      selectionDecision: { id: 78, actorIdentity: 'latest-approver', decidedAt: '2026-09-24T21:00:00Z' },
    }
    skills.setEnabled.and.returnValue(of({ enabled: true, needsReapproval: false }))
    skills.inventory.and.returnValue(of(latestInventory))
    const host = fixture.nativeElement as HTMLElement
    const review = host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement

    review.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const skill = fixture.componentInstance.inventory!.skills[1]
    expect(fixture.componentInstance.hasCurrentGuidancePreview(skill)).toBeTrue()
    const confirm = host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]') as HTMLButtonElement
    confirm.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.inventory?.sourceCommit).toBe(latestCommit)
    expect(fixture.componentInstance.inventory?.catalogFingerprint).toBe(latestCatalogFingerprint)
    expect(fixture.componentInstance.inventory?.skills[1].selectionDecision?.id).toBe(78)
    expect(fixture.componentInstance.hasCurrentGuidancePreview(fixture.componentInstance.inventory!.skills[1])).toBeFalse()
    expect(fixture.componentInstance.guidancePreviews['mcp-builder']).toBeUndefined()
    expect(fixture.componentInstance.selectionAnnouncement).toContain('catalog changed after this save')
    expect(host.querySelector('.skills-page__list')?.textContent).toContain(latestCommit.slice(0, 7))
  })

  it('clears old audit metadata when the post-mutation inventory refresh fails', async () => {
    const { fixture, skills } = await createFixture()
    skills.setEnabled.and.returnValue(of({ enabled: false, needsReapproval: false }))
    skills.inventory.and.returnValue(throwError(() => new Error('offline')))
    const host = fixture.nativeElement as HTMLElement

    ;(host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement).click()
    fixture.detectChanges()

    expect(fixture.componentInstance.inventory?.skills[0].selectionDecision).toBeUndefined()
    expect(fixture.componentInstance.selectionErrors['frontend-design']).toContain('Selection saved')
    expect(fixture.componentInstance.selectionErrors['frontend-design']).toContain('audit details could not be refreshed')
  })

  it('requires an explicit current-version confirmation and blocks duplicate clicks while saving', async () => {
    const response = new Subject<ISkillSelectionState>()
    const service = {
      inventory: jasmine.createSpy('inventory'),
      guidancePreview: jasmine.createSpy('guidancePreview').and.returnValue(of(guidancePreview('frontend-design', true))),
      setEnabled: jasmine.createSpy('setEnabled').and.returnValue(response),
    }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)
    component.session = approverSession
    const skill = { ...inventory.skills[0], enabled: false, needsReapproval: true }
    component.inventory = { ...inventory, skills: [skill] }

    component.reviewGuidance(skill)
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(component.isEffectivelyEnabled(skill)).toBeFalse()
    component.updateSelection(skill, true)
    component.updateSelection(skill, true)

    expect(service.setEnabled).toHaveBeenCalledOnceWith('frontend-design', true, '927c7fa8b4a61dd5a3376a52f5db0ce02d434774ddb3576669d1188ffcd22af7', inventory.catalogFingerprint)
    expect(component.isPending(skill)).toBeTrue()
    response.next({ enabled: true, needsReapproval: false, selectionDecision: { id: 24, actorIdentity: 'owner-1', decidedAt: '2026-09-24T20:00:00Z' } })
    expect(skill.enabled).toBeTrue()
    expect(skill.needsReapproval).toBeFalse()
    expect(skill.selectionDecision?.id).toBe(24)
    expect(component.isPending(skill)).toBeFalse()
  })

  it('announces an in-progress guidance review and keeps selection unavailable until exact text is verified', async () => {
    const { fixture, skills } = await createFixture()
    const response = new Subject<ISkillGuidancePreview>()
    skills.guidancePreview.and.returnValue(response)
    const host = fixture.nativeElement as HTMLElement
    const review = host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement
    expect(review.tagName).toBe('BUTTON')
    expect(review.tabIndex).toBe(0)

    review.focus()
    review.click()
    fixture.detectChanges()

    const loadingStatus = host.querySelector('.skills-page__list .skills-page__guidance-loading[role="status"]')
    expect(loadingStatus?.getAttribute('aria-live')).toBe('polite')
    expect(loadingStatus?.textContent).toContain('Loading and verifying exact HAI-authored guidance for MCP builder')
    expect(review.getAttribute('aria-busy')).toBe('true')
    expect(review.disabled).toBeTrue()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    expect(skills.setEnabled).not.toHaveBeenCalled()

    fixture.ngZone?.run(() => {
      response.next(guidancePreview('mcp-builder'))
      response.complete()
    })
    await fixture.whenStable()
    fixture.detectChanges()

    expect(host.querySelector('.skills-page__guidance-loading')).toBeNull()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).not.toBeNull()
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('reports a server permission rejection as denied rather than an uncertain save', () => {
    const service = { inventory: jasmine.createSpy('inventory'), setEnabled: jasmine.createSpy('setEnabled').and.returnValue(throwError(() => ({ status: 403 }))) }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)
    component.session = approverSession
    const skill = { ...inventory.skills[0] }
    component.inventory = { ...inventory, skills: [skill] }

    component.updateSelection(skill, false)

    expect(component.canApprove).toBeFalse()
    expect(component.accessMessage).toContain('server denied this selection change')
    expect(component.accessMessage).toContain('it was not applied')
    expect(component.selectionErrors[skill.id]).toBeUndefined()
  })

  it('hides approval controls after the selection endpoint denies access and restores them only after refresh', async () => {
    const { fixture, skills, auth } = await createFixture()
    skills.setEnabled.and.returnValue(throwError(() => ({ status: 403 })))
    const host = fixture.nativeElement as HTMLElement
    const component = fixture.componentInstance
    const disable = host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]') as HTMLButtonElement

    disable.click()
    fixture.detectChanges()

    expect(component.canApprove).toBeFalse()
    expect(host.querySelector('.skills-page__access')?.textContent).toContain('server denied this selection change')
    expect(host.querySelector('.skills-page__access')?.textContent).toContain('not applied')
    expect(host.querySelector('.skills-page__access')?.getAttribute('role')).toBe('alert')
    expect(host.querySelectorAll('.skills-page__selection button').length).toBe(0)
    component.updateSelection(inventory.skills[0], false)
    expect(skills.setEnabled).toHaveBeenCalledTimes(1)

    ;(host.querySelector('[aria-label="Refresh Skills inventory"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(auth.session).toHaveBeenCalledTimes(2)
    expect(component.canApprove).toBeTrue()
    expect(host.querySelector('.skills-page__list button[aria-label="Disable Frontend design"]')).not.toBeNull()
  })

  it('hides all approval controls when guidance preview permission is denied', async () => {
    const { fixture, skills } = await createFixture()
    skills.guidancePreview.and.returnValue(throwError(() => ({ status: 403 })))
    const host = fixture.nativeElement as HTMLElement
    const review = host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement

    review.click()
    fixture.detectChanges()

    expect(fixture.componentInstance.canApprove).toBeFalse()
    expect(host.querySelector('.skills-page__access')?.textContent).toContain('denied access to the guidance preview')
    expect(host.querySelector('.skills-page__access')?.getAttribute('role')).toBe('alert')
    expect(host.querySelectorAll('.skills-page__selection button').length).toBe(0)
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('requires a current exact guidance preview before enabling and reports a concurrent winner without error styling', async () => {
    const { fixture, skills } = await createFixture()
    const host = fixture.nativeElement as HTMLElement
    const disabledSkill = fixture.componentInstance.inventory!.skills[1]
    const directEnable = host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement
    expect(directEnable.getAttribute('aria-controls')).toBeNull()

    fixture.componentInstance.updateSelection(disabledSkill, true)
    expect(skills.setEnabled).not.toHaveBeenCalled()
    expect(fixture.componentInstance.selectionErrors[disabledSkill.id]).toContain('Review the exact current')

    directEnable.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const overviewPreviewId = fixture.componentInstance.guidancePreviewId('mcp-builder', 'overview')
    expect(directEnable.getAttribute('aria-controls')).toBe(overviewPreviewId)
    expect(directEnable.getAttribute('aria-expanded')).toBe('true')
    expect(host.ownerDocument.getElementById(overviewPreviewId)?.getAttribute('role')).toBe('region')
    skills.setEnabled.and.returnValue(of({
      enabled: false,
      needsReapproval: false,
      selectionDecision: { id: 99, actorIdentity: 'another-approver', decidedAt: '2026-09-24T21:00:00Z' },
      supersededByConcurrentDecision: true,
    }))
    const enable = host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]') as HTMLButtonElement
    enable.click()
    fixture.detectChanges()

    expect(skills.setEnabled).toHaveBeenCalledOnceWith('mcp-builder', true, 'f43825fa4cbf75b32c397a0766a69a4aa47b1ea24f7976c38a9bb8373ebe823a', inventory.catalogFingerprint)
    const notice = host.querySelector('.skills-page__notice[role="status"]') as HTMLElement
    expect(notice.textContent).toContain('A newer concurrent decision won')
    expect(notice.textContent).toContain('effective state is now disabled')
    expect(notice.getAttribute('role')).toBe('status')
    expect(host.querySelector('.skills-page__list [role="alert"]')).toBeNull()
    expect(fixture.componentInstance.inventory?.skills[1].enabled).toBeFalse()
  })

  it('refuses preview text whose bytes do not match the inventory guidance hash', async () => {
    const { fixture, skills } = await createFixture()
    skills.guidancePreview.and.returnValue(of({
      skillId: 'mcp-builder',
      currentGuidance: 'Tampered text that retains the inventory hash.',
      currentGuidanceSHA256: inventory.skills[1].guidanceSHA256,
      previousGuidanceTextStored: false,
      previousApprovedGuidanceSHA256: 'c'.repeat(64),
      previousGuidanceTextLimitation: 'HAI stores the prior approval hash, not its historical text.',
    }))
    const host = fixture.nativeElement as HTMLElement
    ;(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.hasCurrentGuidancePreview(fixture.componentInstance.inventory!.skills[1])).toBeFalse()
    expect(host.querySelector('.skills-page__list [role="alert"]')?.textContent).toContain('could not be verified against the current inventory hash')
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    expect(host.querySelector('.skills-page__guidance-preview')).toBeNull()
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('invalidates a preview when the source identity changes but guidance stays identical', async () => {
    const changedSourceInventory = copyInventory(inventory)
    changedSourceInventory.catalogFingerprint = 'b'.repeat(64)
    const { fixture, skills } = await createFixture(of(approverSession), of(changedSourceInventory))
    skills.guidancePreview.and.returnValue(of(guidancePreview('mcp-builder', undefined, 'a'.repeat(64))))
    const host = fixture.nativeElement as HTMLElement
    ;(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.inventory?.skills[1].guidanceSHA256).toBe(inventory.skills[1].guidanceSHA256)
    expect(fixture.componentInstance.hasCurrentGuidancePreview(fixture.componentInstance.inventory!.skills[1])).toBeFalse()
    expect(host.querySelector('.skills-page__list [role="alert"]')?.textContent).toContain('catalog identity')
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    fixture.componentInstance.updateSelection(fixture.componentInstance.inventory!.skills[1], true)
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('rejects an oversized guidance preview before it can be marked reviewed', async () => {
    const { fixture, skills } = await createFixture()
    skills.guidancePreview.and.returnValue(of({
      skillId: 'mcp-builder',
      currentGuidance: 'x'.repeat(501),
      currentGuidanceSHA256: inventory.skills[1].guidanceSHA256,
      previousGuidanceTextStored: false,
      previousApprovedGuidanceSHA256: 'c'.repeat(64),
      previousGuidanceTextLimitation: 'HAI stores the prior approval hash, not its historical text.',
    }))
    const host = fixture.nativeElement as HTMLElement
    ;(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.hasCurrentGuidancePreview(fixture.componentInstance.inventory!.skills[1])).toBeFalse()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('requires a valid prior approval pin before confirming a changed guidance version', async () => {
    const { fixture, skills } = await createFixture()
    skills.guidancePreview.and.returnValue(of({
      ...guidancePreview('mcp-builder', true),
      previousApprovedGuidanceSHA256: 'C'.repeat(64),
    }))
    const host = fixture.nativeElement as HTMLElement
    ;(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement).click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.hasCurrentGuidancePreview(fixture.componentInstance.inventory!.skills[1])).toBeFalse()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
    expect(skills.setEnabled).not.toHaveBeenCalled()
  })

  it('treats the backend stale-guidance conflict as a definite rejection and reloads before another approval', async () => {
    const { fixture, skills } = await createFixture()
    const latestInventory = copyInventory(inventory)
    latestInventory.sourceCommit = 'b'.repeat(40)
    latestInventory.skills[1] = {
      ...latestInventory.skills[1],
      enabled: false,
      needsReapproval: true,
      guidanceSHA256: 'd'.repeat(64),
    }
    skills.setEnabled.and.returnValue(throwError(() => ({
      status: 409,
      error: { error: 'HAI-authored guidance changed since it was reviewed.' },
    })))
    skills.inventory.and.returnValue(of(latestInventory))
    const host = fixture.nativeElement as HTMLElement
    const review = host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]') as HTMLButtonElement
    review.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const confirm = host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]') as HTMLButtonElement
    confirm.click()
    fixture.detectChanges()

    expect(skills.setEnabled).toHaveBeenCalledOnceWith('mcp-builder', true, 'f43825fa4cbf75b32c397a0766a69a4aa47b1ea24f7976c38a9bb8373ebe823a', inventory.catalogFingerprint)
    expect(skills.inventory).toHaveBeenCalledTimes(2)
    expect(fixture.componentInstance.inventory?.sourceCommit).toBe('b'.repeat(40))
    expect(fixture.componentInstance.inventory?.skills[1].guidanceSHA256).toBe('d'.repeat(64))
    expect(fixture.componentInstance.inventory?.skills[1].enabled).toBeFalse()
    expect(fixture.componentInstance.guidancePreviews['mcp-builder']).toBeUndefined()
    expect(fixture.componentInstance.selectionErrors['mcp-builder']).toContain('No selection change was applied')
    expect(host.querySelector('.skills-page__list button[aria-label="Review HAI-authored guidance for MCP builder"]')).not.toBeNull()
    expect(host.querySelector('.skills-page__list button[aria-label="Confirm current version and enable MCP builder"]')).toBeNull()
  })

  it('keeps the displayed selection pending confirmation after a failed update', () => {
    const service = { inventory: jasmine.createSpy('inventory'), setEnabled: jasmine.createSpy('setEnabled').and.returnValue(throwError(() => new Error('offline'))) }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)
    component.session = approverSession
    const skill = { ...inventory.skills[0] }
    component.inventory = { ...inventory, skills: [skill] }

    component.updateSelection(skill, false)

    expect(skill.enabled).toBeTrue()
    expect(component.isPending(skill)).toBeFalse()
    expect(component.selectionErrors[skill.id]).toContain('could not confirm whether this selection was saved')
    expect(component.selectionErrors[skill.id]).toContain('Refresh the Skills inventory')
  })

  it('prevents refresh and selection updates from overlapping', () => {
    const inventoryResponse = new Subject<ISkillSelectionInventory>()
    const selectionResponse = new Subject<ISkillSelectionState>()
    const service = {
      inventory: jasmine.createSpy('inventory').and.returnValue(inventoryResponse),
      setEnabled: jasmine.createSpy('setEnabled').and.returnValue(selectionResponse),
    }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    const component = new SkillsComponent(service as any, auth as any)
    const staleSkill = { ...inventory.skills[0] }

    component.refresh()
    component.updateSelection(staleSkill, false)

    expect(component.loading).toBeTrue()
    expect(component.isSelectionDisabled(staleSkill)).toBeTrue()
    expect(service.setEnabled).not.toHaveBeenCalled()

    inventoryResponse.next({ ...inventory, skills: inventory.skills.map((skill) => ({ ...skill })) })
    const displayedSkill = component.inventory!.skills[0]
    component.updateSelection(displayedSkill, false)
    component.refresh()

    expect(component.isPending(displayedSkill)).toBeTrue()
    expect(service.inventory).toHaveBeenCalledTimes(1)
    expect(service.setEnabled).toHaveBeenCalledOnceWith(displayedSkill.id, false)

    selectionResponse.next({ enabled: false, needsReapproval: false, selectionDecision: { id: 25, actorIdentity: 'owner-1', decidedAt: '2026-09-24T20:00:00Z' } })

    expect(displayedSkill.enabled).toBeFalse()
    expect(component.isPending(displayedSkill)).toBeFalse()
  })

  it('hides stale inventory and opt-in controls when a refresh fails', async () => {
    const skills = {
      inventory: jasmine.createSpy('inventory').and.returnValues(of(inventory), throwError(() => new Error('offline'))),
      setEnabled: jasmine.createSpy('setEnabled'),
    }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    await TestBed.configureTestingModule({
      imports: [SkillsModule],
      providers: [
        { provide: SkillsService, useValue: skills },
        { provide: AuthSessionService, useValue: auth },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(SkillsComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement

    expect(host.querySelectorAll('.skills-page__selection button').length).toBeGreaterThan(0)
    ;(host.querySelector('[aria-label="Refresh Skills inventory"]') as HTMLButtonElement).click()
    fixture.detectChanges()

    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Selection controls are hidden')
    expect(host.querySelector('.skills-page__list')).toBeNull()
    expect(host.querySelector('.skills-page__records')).toBeNull()
    expect(host.querySelectorAll('.skills-page__selection button').length).toBe(0)
  })

  it('renders the server error and empty inventory distinctly', async () => {
    const skills = {
      inventory: jasmine.createSpy('inventory').and.returnValues(
        throwError(() => new Error('offline')),
        of({ ...inventory, skills: [] }),
      ),
      setEnabled: jasmine.createSpy('setEnabled'),
    }
    const auth = { session: jasmine.createSpy('session').and.returnValue(of(approverSession)) }
    await TestBed.configureTestingModule({
      imports: [SkillsModule],
      providers: [
        { provide: SkillsService, useValue: skills },
        { provide: AuthSessionService, useValue: auth },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(SkillsComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement
    expect(host.querySelector('.skills-page__message--error')?.textContent).toContain('Skills inventory unavailable')

    const frames: FrameRequestCallback[] = []
    spyOn(window, 'requestAnimationFrame').and.callFake((callback) => {
      frames.push(callback)
      return frames.length
    })
    frames.length = 0
    ;(host.querySelector('[role="alert"] button') as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(host.querySelector('[role="alert"]')).toBeNull()
    expect(host.querySelector('[role="status"]')?.textContent).toContain('No Skills are registered on this server')
    for (const frame of frames) {
      frame(0)
      if (document.activeElement === host.querySelector('#skills-heading')) break
    }
    expect(document.activeElement).toBe(host.querySelector('#skills-heading'))
  })

  it('announces an in-progress refresh while keeping the last inventory visible', async () => {
    const { fixture, skills } = await createFixture()
    const response = new Subject<ISkillSelectionInventory>()
    skills.inventory.and.returnValue(response)
    const host = fixture.nativeElement as HTMLElement
    const refresh = host.querySelector('#skills-refresh') as HTMLButtonElement

    refresh.click()
    fixture.detectChanges()

    expect(refresh.getAttribute('aria-busy')).toBe('true')
    expect(host.querySelector('[role="status"]')?.textContent).toContain('Refreshing Skills inventory')
    expect(host.querySelector('.skills-page__list')).not.toBeNull()
    expect(refresh.disabled).toBeTrue()

    fixture.ngZone?.run(() => {
      response.next(copyInventory(inventory))
      response.complete()
    })
    await fixture.whenStable()
    fixture.detectChanges()
    expect(skills.inventory).toHaveBeenCalledTimes(2)
    expect(fixture.componentInstance.loading).toBeFalse()
    expect(fixture.componentInstance.sessionLoading).toBeFalse()
    expect(host.querySelector('#skills-refresh')?.outerHTML)
      .withContext(`loading=${fixture.componentInstance.loading}; sessionLoading=${fixture.componentInstance.sessionLoading}`)
      .not.toContain('aria-busy="true"')
  })
})

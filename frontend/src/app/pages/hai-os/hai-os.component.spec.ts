import { CommonModule } from '@angular/common'
import { NgZone } from '@angular/core'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { Router } from '@angular/router'
import { By } from '@angular/platform-browser'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzTableModule } from 'ng-zorro-antd/table'
import { of, Subject, throwError } from 'rxjs'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { IHAIOSOverview } from '../../models/hai-os.model.interface'
import { HAI_OS_SERVICE_TOKEN } from '../../services/hai-os/hai-os.service.token'
import { IHAIOSService } from '../../services/hai-os.service.interface'
import { HAIOSComponent } from './hai-os.component'

describe('HAIOSComponent progressive control room', () => {
  const preferenceKey = 'hai.module-view.v1.hai-os'
  let fixture: ComponentFixture<HAIOSComponent>
  let service: jasmine.SpyObj<IHAIOSService>
  let router: jasmine.SpyObj<Router>

  function overview(): IHAIOSOverview {
    return {
      generatedAt: '2026-09-24T10:00:00Z',
      canonicalStack: 'Go and Angular',
      referenceStacks: [{ name: 'Reference', status: 'reviewed', use: 'Selected patterns only' }],
      localFirst: true,
      completionFirst: true,
      paidBudgetEur: 0,
      paidUsageAllowed: false,
      metrics: [{ label: 'Workflows', value: 2, status: 'ready' }],
      planes: [{ name: 'Runtime', status: 'ready', description: 'Controlled runtime', links: ['/runtime-control'] }],
      readinessGates: [{ name: 'Provider proof', status: 'pending', evidence: 'Not tested live', next: 'Run provider acceptance' }],
      pursuitOverview: {
        enabled: true,
        status: 'active',
        totalActive: 1,
        needsRobert: 1,
        vaReady: 0,
        systemReady: 0,
        blocked: 0,
        stale: 0,
        reviewDue: 1,
        planningNeeded: 0,
        highRisk: 0,
        completionCandidates: 0,
        decisionCards: 1,
        linkedEvidence: 2,
        openLoops: 1,
        timelineItems: 3,
        evidenceStatus: 'linked',
        ambientProposals: 0,
        ambientApprovalQueue: 0,
        ambientLastScan: '2026-09-24T09:45:00Z',
        ambientLine: 'No new proposal',
        summary: 'One active pursuit needs a decision.',
        next: 'Review the pending decision.',
        queues: [{
          name: 'Robert-only decisions',
          description: 'Items HAI should not decide alone.',
          count: 1,
          status: 'needs decision',
          route: '/pursuits',
        }],
        spotlight: [{
          id: 'pursuit-1',
          title: 'Review the evidence',
          status: 'active',
          riskLevel: 'low',
          nextAction: 'Inspect the source',
          needsRobert: 1,
          blocked: 0,
          openLoops: 1,
          decisionCards: 1,
          linkedEvidence: 2,
          timelineItems: 3,
          stale: false,
          reviewDue: true,
          planningNeeded: false,
        }],
      },
      needsReviewTotal: 1,
      emergencyStop: false,
      emergencyStopReason: '',
      emergencyStopNote: '',
    }
  }

  beforeEach(async () => {
    localStorage.removeItem(preferenceKey)
    service = jasmine.createSpyObj<IHAIOSService>('HAIOSService', ['overview'])
    service.overview.and.returnValue(of(overview()))
    router = jasmine.createSpyObj<Router>('Router', ['navigate', 'navigateByUrl'])

    await TestBed.configureTestingModule({
      declarations: [HAIOSComponent],
      imports: [
        CommonModule,
        ControlRoomModule,
        NzButtonModule,
        NzTableModule,
      ],
      providers: [
        { provide: HAI_OS_SERVICE_TOKEN, useValue: service },
        { provide: Router, useValue: router },
      ],
    }).compileComponents()
  })

  afterEach(() => {
    fixture?.destroy()
    document.body.classList.remove('hai-view-advanced')
    localStorage.removeItem(preferenceKey)
  })

  function createFixture(): ComponentFixture<HAIOSComponent> {
    fixture = TestBed.createComponent(HAIOSComponent)
    fixture.detectChanges()
    return fixture
  }

  function section(sectionId: string): HaiProgressiveSectionComponent {
    const component = fixture.debugElement
      .queryAll(By.directive(HaiProgressiveSectionComponent))
      .map((debugElement) => debugElement.componentInstance as HaiProgressiveSectionComponent)
      .find((candidate) => candidate.sectionId === sectionId)
    if (!component) throw new Error(`Missing HAI OS progressive section: ${sectionId}`)
    return component
  }

  it('keeps Basic focused on attention, the recommended move, and actionable pursuit records', () => {
    createFixture()

    const actions = fixture.nativeElement.querySelector('.os-page__actions') as HTMLElement
    expect(actions.getAttribute('role')).toBe('group')
    expect(actions.getAttribute('aria-label')).toBe('HAI OS actions')

    const disclosures = fixture.debugElement.queryAll(By.directive(HaiProgressiveSectionComponent))
      .map((debugElement) => debugElement.componentInstance as HaiProgressiveSectionComponent)
    const advancedSections = disclosures.filter((candidate) => candidate.advancedOnly)

    expect(fixture.componentInstance.moduleId).toBe('hai-os')
    expect(fixture.nativeElement.querySelector('#os-page-title')?.textContent?.trim())
      .toBe('Operating overview')
    expect(disclosures.length).toBe(8)
    expect(advancedSections.map((candidate) => candidate.sectionId).sort()).toEqual([
      'governance',
      'operating-planes',
      'product-stack',
      'pursuit-context',
      'pursuit-queues',
      'real-world-readiness',
      'system-metrics',
    ])
    expect(disclosures.filter((candidate) => !candidate.advancedOnly).map((candidate) => candidate.sectionId).sort())
      .toEqual(['pursuit-spotlight'])
    expect(fixture.nativeElement.querySelectorAll('.status-item').length).toBe(2)
    expect(fixture.nativeElement.querySelector('.status-item--review dd')?.textContent?.trim()).toBe('1')
    expect(fixture.nativeElement.querySelector('.status-item--safety dd')?.getAttribute('data-state'))
      .toBe('clear')
    expect(fixture.nativeElement.querySelector('.status-item--safety small')).toBeNull()
    expect(fixture.nativeElement.querySelector('.os-overview-summary')?.textContent).not.toContain('EUR')
    expect(fixture.nativeElement.querySelector('.pursuit-focus__label')?.textContent)
      .toContain('Next step')
    expect(fixture.nativeElement.querySelector('.pursuit-focus__summary')?.textContent)
      .toContain('One active pursuit needs a decision.')
    expect(fixture.nativeElement.querySelector('.pursuit-focus__meta')?.textContent)
      .toContain('1 needs your decision')
    expect(fixture.nativeElement.querySelector('.pursuit-focus h2')?.textContent)
      .toContain('Review the pending decision')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent)
      .toContain('Open pursuit queue')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.getAttribute('aria-label'))
      .toBe('Open pursuit queue')
    expect(section('pursuit-spotlight').open).toBeTrue()
    expect(fixture.nativeElement.querySelectorAll('.spotlight-item').length).toBe(1)
    expect(fixture.nativeElement.querySelector('.spotlight-item__next')?.textContent)
      .toContain('Inspect the source')
    expect(fixture.nativeElement.querySelector('.spotlight-item__signals')?.textContent)
      .toContain('Needs your decision')
    expect(fixture.nativeElement.querySelector('.metric-grid')).toBeNull()
    expect(fixture.nativeElement.querySelector('.metric-list')).toBeNull()
    expect(fixture.nativeElement.querySelector('.data-table-scroll')).toBeNull()
    expect(fixture.nativeElement.querySelector('#product-stack')).toBeNull()
    expect(fixture.nativeElement.querySelector('#system-metrics')).toBeNull()
    expect(advancedSections.every((candidate) => !candidate.visible)).toBeTrue()
    expect(fixture.nativeElement.querySelector('.overview-updated time')?.getAttribute('datetime'))
      .toBe('2026-09-24T10:00:00Z')
    expect(TestBed.inject(ModuleViewPreferencesService).get('hai-os').mode).toBe('basic')
  })

  it('surfaces the backend emergency-stop reason without implying that the stop can be changed here', () => {
    service.overview.and.returnValue(of({
      ...overview(),
      paidBudgetEur: 12.5,
      emergencyStop: true,
      emergencyStopReason: 'Runtime health check failed',
    }))
    createFixture()

    const emergencyStatus = fixture.nativeElement.querySelector('.status-item--safety') as HTMLElement
    expect(emergencyStatus.querySelector('dd')?.textContent).toContain('Active')
    expect(emergencyStatus.querySelector('dd')?.getAttribute('data-state')).toBe('attention')
    expect(emergencyStatus.querySelector('small')?.textContent).toContain('Runtime health check failed')
    expect(fixture.nativeElement.querySelector('.os-overview-summary')?.textContent).not.toContain('EUR 12.50')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent).toContain('Open pursuit queue')
  })

  it('distinguishes review backlog from an active emergency stop', () => {
    service.overview.and.returnValue(of({
      ...overview(),
      needsReviewTotal: 2,
      emergencyStop: true,
    }))
    createFixture()

    const reviewCount = fixture.nativeElement.querySelector('.status-item--review dd') as HTMLElement
    const emergencyStop = fixture.nativeElement.querySelector('.status-item--safety dd') as HTMLElement
    const warningProbe = document.createElement('span')
    const dangerProbe = document.createElement('span')
    warningProbe.style.color = 'var(--hai-warning)'
    dangerProbe.style.color = 'var(--hai-red)'
    document.body.append(warningProbe, dangerProbe)
    const warningColor = getComputedStyle(warningProbe).color
    const dangerColor = getComputedStyle(dangerProbe).color
    warningProbe.remove()
    dangerProbe.remove()

    expect(getComputedStyle(reviewCount).color).toBe(warningColor)
    expect(getComputedStyle(emergencyStop).color).toBe(dangerColor)
  })

  it('omits a zero-count review signal instead of showing an empty metric', () => {
    service.overview.and.returnValue(of({ ...overview(), needsReviewTotal: 0 }))
    createFixture()

    expect(fixture.nativeElement.querySelector('.status-item--review dd')?.textContent).toContain('Clear')
    expect(fixture.nativeElement.querySelector('.status-item--safety dd')?.textContent).toContain('Clear')
  })

  it('reveals current technical data and budget only in the saved Advanced disclosures', () => {
    createFixture()
    const preferences = TestBed.inject(ModuleViewPreferencesService)

    expect(fixture.nativeElement.querySelector('.metric-list')).toBeNull()
    expect(fixture.nativeElement.querySelector('.data-table-scroll')).toBeNull()
    expect(fixture.nativeElement.textContent).not.toContain('Paid budget')

    preferences.setMode('hai-os', 'advanced')
    fixture.detectChanges()

    const stackDisclosure = fixture.nativeElement.querySelector(
      '#product-stack .hai-progressive-section__summary',
    ) as HTMLButtonElement
    stackDisclosure.click()
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('.data-table-scroll[aria-label^="Reference technology stacks"]'))
      .not.toBeNull()
    expect(fixture.nativeElement.querySelector('#product-stack')?.textContent).toContain('Go and Angular')

    const metricsDisclosure = fixture.nativeElement.querySelector(
      '#system-metrics .hai-progressive-section__summary',
    ) as HTMLButtonElement
    metricsDisclosure.click()
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('.metric-list')?.textContent).toContain('Workflows')
    expect(fixture.nativeElement.querySelector('.metric-list')?.textContent).toContain('2')

    const governanceDisclosure = fixture.nativeElement.querySelector(
      '#governance .hai-progressive-section__summary',
    ) as HTMLButtonElement
    governanceDisclosure.click()
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('.governance-list')?.textContent).toContain('EUR 0.00')
    expect(fixture.nativeElement.querySelector('.governance-list')?.textContent).toContain('Paid usage allowed')
    expect(preferences.get('hai-os').openSections['product-stack']).toBeTrue()
    expect(preferences.get('hai-os').openSections['system-metrics']).toBeTrue()
    expect(preferences.get('hai-os').openSections['governance']).toBeTrue()
  })

  it('shows plain, truthful empty states when Advanced collections contain no records', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      canonicalStack: '',
      referenceStacks: [],
      metrics: [],
      planes: [],
      readinessGates: [],
      pursuitOverview: { ...base, queues: [] },
    }))
    TestBed.inject(ModuleViewPreferencesService).setMode('hai-os', 'advanced')
    createFixture()

    const expectations = [
      ['pursuit-queues', 'No pursuit queues were returned.'],
      ['product-stack', 'No product-stack data was returned.'],
      ['system-metrics', 'No system metrics were returned.'],
      ['operating-planes', 'No operating planes were returned.'],
      ['real-world-readiness', 'No readiness gates were returned.'],
    ] as const

    for (const [sectionId, message] of expectations) {
      const trigger = fixture.nativeElement.querySelector(
        `#${sectionId} .hai-progressive-section__summary`,
      ) as HTMLButtonElement
      trigger.click()
      fixture.detectChanges()
      expect(fixture.nativeElement.querySelector(`#${sectionId} .advanced-empty-state`)?.textContent)
        .toContain(message)
    }
  })

  it('limits the Basic attention list to three real records and opens the pursuits workspace for the rest', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: {
        ...base,
        spotlight: Array.from({ length: 5 }, (_, index) => ({
          ...base.spotlight[0],
          id: `pursuit-${index + 1}`,
          title: `Pursuit ${index + 1}`,
        })),
      },
    }))
    createFixture()

    const attentionSection = section('pursuit-spotlight')
    expect(attentionSection.open).toBeTrue()
    expect(fixture.nativeElement.querySelectorAll('.spotlight-item').length).toBe(3)
    const viewAll = fixture.nativeElement.querySelector('.pursuit-view-all') as HTMLButtonElement
    expect(viewAll.textContent).toContain('Open all pursuits')
    viewAll.click()
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: undefined })
  })

  it('includes visible attention signals in the screen-reader action label', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: {
        ...base,
        spotlight: [{
          ...base.spotlight[0],
          riskLevel: 'high',
          needsRobert: 1,
          blocked: 2,
          reviewDue: true,
          planningNeeded: true,
          stale: true,
        }],
      },
    }))
    createFixture()

    const attentionAction = fixture.nativeElement.querySelector('.spotlight-item') as HTMLButtonElement
    const accessibleLabel = attentionAction.getAttribute('aria-label') ?? ''

    expect(accessibleLabel).toContain('Open Review the evidence')
    expect(accessibleLabel).toContain('active, high risk')
    expect(accessibleLabel).toContain('Needs your decision')
    expect(accessibleLabel).toContain('Blocked')
    expect(accessibleLabel).toContain('Review due')
    expect(accessibleLabel).toContain('Planning needed')
    expect(accessibleLabel).toContain('high risk')
    expect(accessibleLabel).toContain('Stale')
    expect(accessibleLabel).toContain('Inspect the source')
  })

  it('keeps the empty-state route actionable when no pursuits are active', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: { ...base, totalActive: 0, spotlight: [] },
    }))
    createFixture()

    expect(section('pursuit-spotlight').open).toBeTrue()
    expect(fixture.nativeElement.querySelector('.empty-pursuit-state')?.textContent)
      .toContain('No open pursuits')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent)
      .toContain('Create pursuit')
    ;(fixture.nativeElement.querySelector('.pursuit-primary') as HTMLButtonElement).click()
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { create: 'true' } })
  })

  it('persists an expanded section under HAI OS only and restores it on remount', () => {
    createFixture()
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setMode('hai-os', 'advanced')
    fixture.detectChanges()
    const trigger = fixture.nativeElement.querySelector(
      '#pursuit-queues .hai-progressive-section__summary',
    ) as HTMLButtonElement

    trigger.click()
    fixture.detectChanges()

    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect(trigger.getAttribute('aria-controls')).toBe(section('pursuit-queues').panelId)
    expect(TestBed.inject(ModuleViewPreferencesService).get('hai-os').openSections['pursuit-queues']).toBeTrue()
    expect(TestBed.inject(ModuleViewPreferencesService).get('pursuits').openSections).toEqual({})

    fixture.destroy()
    fixture = TestBed.createComponent(HAIOSComponent)
    fixture.detectChanges()
    expect(section('pursuit-queues').open).toBeTrue()
    expect(section('pursuit-spotlight').open).toBeTrue()
  })

  it('shows Basic attention by default but preserves an explicit collapsed preference', () => {
    createFixture()
    const attentionSection = section('pursuit-spotlight')
    expect(attentionSection.open).toBeTrue()

    ;(fixture.nativeElement.querySelector(`#${attentionSection.triggerId}`) as HTMLButtonElement).click()
    fixture.detectChanges()
    expect(attentionSection.open).toBeFalse()
    expect(fixture.nativeElement.querySelector('.spotlight-item')).toBeNull()

    fixture.destroy()
    fixture = TestBed.createComponent(HAIOSComponent)
    fixture.detectChanges()
    expect(section('pursuit-spotlight').open).toBeFalse()
    expect(TestBed.inject(ModuleViewPreferencesService).get('hai-os').openSections['pursuit-spotlight'])
      .toBeFalse()
  })

  it('routes the recommended move to the pursuit queue and individual records to their selected detail', () => {
    TestBed.inject(ModuleViewPreferencesService).setMode('hai-os', 'advanced')
    createFixture()

    const buttons = Array.from(fixture.nativeElement.querySelectorAll('button')) as HTMLButtonElement[]
    buttons.find((button) => button.getAttribute('aria-label') === 'Open pursuit queue')?.click()
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: undefined })

    const queueSection = section('pursuit-queues')
    const disclosureButtons = Array.from(
      fixture.nativeElement.querySelectorAll('.hai-progressive-section__summary'),
    ) as HTMLButtonElement[]
    const queueTrigger = disclosureButtons.find((button) => button.textContent?.includes('Operating queues'))!
    queueTrigger.click()
    fixture.detectChanges()
    expect(queueSection.open).toBeTrue()
    expect(fixture.nativeElement.querySelectorAll('.pursuit-queue').length).toBeGreaterThan(0)
    expect(fixture.nativeElement.querySelector('.pursuit-queue button')).toBeNull()
    expect(router.navigateByUrl).not.toHaveBeenCalled()

    expect(section('pursuit-spotlight').open).toBeTrue()
    const spotlightButton = fixture.nativeElement.querySelector('.spotlight-item') as HTMLButtonElement
    spotlightButton.click()
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: 'pursuit-1' } })

    const operatingPlanesDisclosure = Array.from(
      fixture.nativeElement.querySelectorAll('.hai-progressive-section__summary') as NodeListOf<HTMLButtonElement>,
    ).find((button) => button.textContent?.includes('Operating planes')) as HTMLButtonElement
    operatingPlanesDisclosure.click()
    fixture.detectChanges()
    expect(section('operating-planes').open).toBeTrue()
    const routeButton = fixture.nativeElement.querySelector('.data-table-scroll button') as HTMLButtonElement
    expect(routeButton.textContent).toContain('Runtime Control')
    expect(routeButton.getAttribute('aria-label')).toBe('Open Runtime Control for Runtime')
    routeButton.click()
    expect(router.navigateByUrl).toHaveBeenCalledWith('/runtime-control')
    expect(fixture.componentInstance.navigationLabel('/llm-policy?tab=models#active')).toBe('LLM Policy')
  })

  it('renders only verified internal module destinations as buttons', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      planes: [{
        name: 'Test plane',
        status: 'ready',
        description: 'Route validation fixture',
        links: ['/runtime-control', 'https://outside.example', '//outside.example', '/not-a-route', 'javascript:alert(1)'],
      }],
      pursuitOverview: {
        ...base,
        queues: [{
          name: 'External route fixture',
          description: 'This queue has no supported filter destination.',
          count: 1,
          status: 'needs_review',
          route: 'https://outside.example',
        }],
      },
    }))
    TestBed.inject(ModuleViewPreferencesService).setMode('hai-os', 'advanced')
    createFixture()

    const planesTrigger = Array.from(
      fixture.nativeElement.querySelectorAll('.hai-progressive-section__summary') as NodeListOf<HTMLButtonElement>,
    ).find((button) => button.textContent?.includes('Operating planes')) as HTMLButtonElement
    planesTrigger.click()
    fixture.detectChanges()

    const routeButtons = Array.from(
      fixture.nativeElement.querySelectorAll('#operating-planes .data-table-scroll button'),
    ) as HTMLButtonElement[]
    expect(routeButtons.length).toBe(1)
    expect(routeButtons[0].textContent).toContain('Runtime Control')
    expect(fixture.nativeElement.querySelectorAll('.unavailable-plane-link').length).toBe(4)
    routeButtons[0].click()
    fixture.componentInstance.open('https://outside.example')
    fixture.componentInstance.open('//outside.example')
    fixture.componentInstance.open('/not-a-route')
    fixture.componentInstance.open('javascript:alert(1)')

    expect(router.navigateByUrl).toHaveBeenCalledTimes(1)
    expect(router.navigateByUrl).toHaveBeenCalledWith('/runtime-control')
    expect(fixture.nativeElement.querySelector('.pursuit-queue button')).toBeNull()
  })

  it('does not present a spotlight record without an ID as an actionable detail link', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: {
        ...base,
        spotlight: [{ ...base.spotlight[0], id: ' ' }],
      },
    }))
    createFixture()

    expect(fixture.nativeElement.querySelector('.spotlight-item[aria-label]')).toBeNull()
    expect(fixture.nativeElement.querySelector('.spotlight-item--unavailable')?.textContent)
      .toContain('Record detail unavailable')
    expect(router.navigate).not.toHaveBeenCalled()
  })

  it('avoids proposing create/import when pursuit data is unavailable', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: {
        ...base,
        enabled: false,
        status: 'unavailable',
        totalActive: 0,
        summary: 'Internal service wiring detail',
        next: 'Wire the pursuit service',
        spotlight: [],
      },
    }))
    createFixture()

    expect(fixture.nativeElement.querySelector('#pursuit-focus-title')?.textContent)
      .toContain('Pursuit information is unavailable')
    expect(fixture.nativeElement.textContent).not.toContain('Wire the pursuit service')
    expect(fixture.nativeElement.textContent).not.toContain('Internal service wiring detail')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent)
      .toContain('Open pursuits workspace')
    expect(fixture.nativeElement.querySelector('.empty-pursuit-state')?.textContent)
      .toContain('Pursuit attention is unavailable')

    ;(fixture.nativeElement.querySelector('.pursuit-primary') as HTMLButtonElement).click()
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: undefined })
  })

  it('uses a truthful workspace fallback when active pursuits have no backend-ranked spotlight item', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: { ...base, spotlight: [] },
    }))
    createFixture()

    const primary = fixture.nativeElement.querySelector('.pursuit-primary') as HTMLButtonElement
    expect(primary.textContent).toContain('Open pursuit queue')
    expect(primary.getAttribute('aria-label')).toBe('Open pursuit queue')
    primary.click()

    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: undefined })
  })

  it('shows the next-move fallback without inventing work when the overview has no recommendation', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: { ...base, next: '', summary: '' },
    }))
    createFixture()

    expect(fixture.nativeElement.querySelector('#pursuit-focus-title')?.textContent)
      .toContain('Review your open pursuits and choose the next step.')
    expect(fixture.nativeElement.querySelector('.pursuit-focus__summary')).toBeNull()
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent)
      .toContain('Open pursuit queue')
  })

  it('uses the create-or-import fallback when there are no pursuits and no backend recommendation', () => {
    const base = overview().pursuitOverview
    service.overview.and.returnValue(of({
      ...overview(),
      pursuitOverview: { ...base, totalActive: 0, needsRobert: 0, next: '', summary: '', spotlight: [] },
    }))
    createFixture()

    expect(fixture.nativeElement.querySelector('#pursuit-focus-title')?.textContent)
      .toContain('Create a pursuit to get started.')
    expect(fixture.nativeElement.querySelector('.pursuit-primary')?.textContent)
      .toContain('Create pursuit')
  })

  it('keeps wide tables inside named keyboard-scrollable regions on narrow layouts', () => {
    TestBed.inject(ModuleViewPreferencesService).setMode('hai-os', 'advanced')
    createFixture()
    const productStackDisclosure = Array.from(
      fixture.nativeElement.querySelectorAll('.hai-progressive-section__summary') as NodeListOf<HTMLButtonElement>,
    ).find((button) => button.textContent?.includes('Product stack')) as HTMLButtonElement
    productStackDisclosure.click()
    fixture.detectChanges()
    expect(section('product-stack').open).toBeTrue()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section__content')).not.toBeNull()

    const region = fixture.nativeElement.querySelector('.data-table-scroll') as HTMLElement
    region.style.width = '320px'
    const table = region.querySelector('table') as HTMLTableElement

    expect(region.getAttribute('role')).toBe('region')
    expect(region.getAttribute('aria-label')).toContain('Reference technology stacks')
    expect(region.tabIndex).toBe(0)
    expect(getComputedStyle(region).overflowX).toBe('auto')
    expect(getComputedStyle(table).minWidth).toBe('680px')
    expect(region.scrollWidth).toBeGreaterThan(region.clientWidth)
  })

  it('announces the initial loading state and clears it after the request completes', async () => {
    const response = new Subject<IHAIOSOverview>()
    service.overview.and.returnValue(response)
    createFixture()

    expect(fixture.nativeElement.querySelector('[role="status"]')?.textContent)
      .toContain('Loading the current HAI OS overview')
    expect(fixture.componentInstance.loading).toBeTrue()
    expect((fixture.nativeElement.querySelector('.os-page__actions button') as HTMLButtonElement).disabled)
      .toBeTrue()

    TestBed.inject(NgZone).run(() => {
      response.next(overview())
      response.complete()
    })
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.loading).toBeFalse()
    expect(fixture.nativeElement.querySelector('.content-container')).not.toBeNull()
    expect(fixture.nativeElement.querySelector('[role="status"]')).toBeNull()
  })

  it('keeps load failures visible and retries successfully without losing the last good overview', () => {
    service.overview.and.returnValues(
      of(overview()),
      throwError(() => new Error('offline')),
      of({ ...overview(), canonicalStack: 'Updated stack' }),
    )
    createFixture()

    const refreshButton = fixture.nativeElement.querySelector('.os-page__actions button') as HTMLButtonElement
    refreshButton.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.errorMessage).toContain('Showing the last successfully loaded overview')
    const alert = fixture.nativeElement.querySelector('[role="alert"]') as HTMLElement
    expect(alert.querySelector('strong')?.textContent).toContain('Refresh failed')
    expect(alert.querySelector('button')).toBeNull()
    expect(alert.textContent).toContain('Showing the last successfully loaded overview')
    expect(fixture.componentInstance.overview?.canonicalStack).toBe('Go and Angular')

    const retry = fixture.nativeElement.querySelector('.os-state--error button') as HTMLButtonElement
    expect(retry.getAttribute('aria-label')).toBe('Retry loading the HAI OS overview')
    retry.click()
    fixture.detectChanges()

    expect(service.overview).toHaveBeenCalledTimes(3)
    expect(fixture.nativeElement.querySelector('[role="alert"]')).toBeNull()
    expect(fixture.componentInstance.overview?.canonicalStack).toBe('Updated stack')
  })

  it('shows an accessible initial loading state and retry action when the overview is unavailable', () => {
    service.overview.and.returnValues(
      throwError(() => new Error('offline')),
      of(overview()),
    )
    createFixture()

    const alert = fixture.nativeElement.querySelector('[role="alert"]') as HTMLElement
    expect(alert.querySelector('strong')?.textContent).toContain('HAI OS overview unavailable')
    expect(alert.textContent).toContain('Your work has not been changed')
    expect(alert.querySelector('button')).toBeNull()
    ;(fixture.nativeElement.querySelector('.os-state--error button') as HTMLButtonElement).click()
    fixture.detectChanges()

    expect(fixture.nativeElement.querySelector('[role="alert"]')).toBeNull()
    expect(fixture.nativeElement.querySelector('.content-container')).not.toBeNull()
  })

  it('does not render an invalid backend timestamp as a broken date', () => {
    service.overview.and.returnValue(of({ ...overview(), generatedAt: 'not-a-timestamp' }))
    createFixture()

    expect(fixture.nativeElement.querySelector('.content-container')).not.toBeNull()
    expect(fixture.nativeElement.querySelector('.status-item--review dd')?.textContent?.trim()).toBe('1')
    expect(fixture.nativeElement.querySelector('.overview-updated time')).toBeNull()
    expect(fixture.nativeElement.querySelector('.overview-updated')?.textContent).toContain('Unavailable')
  })
})

import { ViewEncapsulation } from '@angular/core'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { DefaultUrlSerializer, NavigationEnd, Router, UrlTree } from '@angular/router'
import { Observable, of, Subject, throwError } from 'rxjs'
import { RouterTestingModule } from '@angular/router/testing'
import { IBackgroundStatus } from '../models/runtime-control.model.interface'
import { RuntimeControlService } from '../services/runtime-control.service'
import { ModuleViewPreferencesService } from './module-view-preferences.service'
import { ThemeService } from '../services/theme.service'
import { AppShellComponent } from './app-shell.component'
import { moduleForUrl } from './module-registry'

describe('AppShellComponent', () => {
  const status: IBackgroundStatus = {
    mode: 'autonomous_safe', storedMode: 'autonomous_safe',
    emergencyStop: { engaged: false, updatedAt: '2026-09-23T00:00:00Z' },
    backgroundProcessingActive: true,
    docker: { cliAvailable: false, daemonRunning: false, required: false, detail: 'not required' },
    completedOperations: 0, awaitingApproval: 0,
  }

  function createShell(statusSource: () => Observable<IBackgroundStatus>): AppShellComponent {
    return new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      {} as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: statusSource } as unknown as RuntimeControlService,
    )
  }

  function afterNativeDisclosureUpdate(): Promise<void> {
    return new Promise((resolve) => window.setTimeout(resolve, 0))
  }

  function findMediaRule(condition: string): CSSMediaRule | undefined {
    for (const sheet of Array.from(document.styleSheets)) {
      let rules: CSSRuleList
      try {
        rules = sheet.cssRules
      } catch {
        continue
      }
      const mediaRule = Array.from(rules).find((rule): rule is CSSMediaRule =>
        rule instanceof CSSMediaRule && rule.conditionText.includes(condition),
      )
      if (mediaRule) return mediaRule
    }
    return undefined
  }

  function findStyleRule(rules: CSSRuleList, selector: string): CSSStyleRule | undefined {
    return Array.from(rules).find((rule): rule is CSSStyleRule =>
      rule instanceof CSSStyleRule && rule.selectorText === selector,
    )
  }

  it('applies shared authenticated workspace styles without component scoping', () => {
    expect((AppShellComponent as any).ɵcmp.encapsulation).toBe(ViewEncapsulation.None)
  })

  it('shows the live autonomy mode and its actual approval boundary', () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    const component = createShell(() => of(status))
    component.refreshSafetyStatus()
    expect(component.safetyStatusUnknown).toBeFalse()
    expect(component.safetyLabel).toBe('Autonomous-safe mode')
    expect(component.safetyDetail).toContain('higher-risk actions need approval')
    expect(component.safetyCompactLabel).toBe('Autonomous-safe')
  })

  it('keeps the compact safety label explicit when status is unknown, stale, or stopped', () => {
    const component = createShell(() => of(status))

    expect(component.safetyCompactLabel).toBe('Safety unknown')

    component.safetyFreshness = 'stale'
    expect(component.safetyCompactLabel).toBe('Safety stale')

    component.emergencyStopActive = true
    expect(component.safetyCompactLabel).toBe('Stop active')
  })

  it('persists native Advanced disclosure state under the active module and stable section id', () => {
    const component = createShell(() => of(status))
    const setSection = jasmine.createSpy('setSection')
    ;(component as any).preferences = { setSection }
    component.current = moduleForUrl('/pursuits')
    const detail = document.createElement('details')
    detail.dataset['haiAdvanced'] = ''
    detail.dataset['haiSection'] = 'workflow-approval-trail'
    detail.open = true
    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    outlet.appendChild(detail)
    document.body.appendChild(outlet)

    const event = new Event('toggle')
    Object.defineProperty(event, 'target', { value: detail })
    ;(component as any).persistDetailState(event)

    expect(setSection).toHaveBeenCalledOnceWith('pursuits', 'workflow-approval-trail', true)
    outlet.remove()
  })

  it('ignores delayed disclosure events from detached route content', () => {
    const setSection = jasmine.createSpy('setSection')
    const component = createShell(() => of(status))
    ;(component as any).preferences = { setSection }
    component.current = moduleForUrl('/memory')
    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    const detail = document.createElement('details')
    detail.dataset['haiAdvanced'] = ''
    detail.dataset['haiSection'] = 'old-module-details'
    detail.open = true
    outlet.appendChild(detail)
    document.body.appendChild(outlet)
    detail.remove()

    const event = new Event('toggle')
    Object.defineProperty(event, 'target', { value: detail })
    ;(component as any).persistDetailState(event)

    expect(setSection).not.toHaveBeenCalled()
    outlet.remove()
    component.ngOnDestroy()
  })

  it('keeps navigation groups collapsible and remembers their state independently of module views', () => {
    const savedGroups = { version: 1 as const, openGroups: { work: false } }
    const setNavigationGroupOpen = jasmine.createSpy('setNavigationGroupOpen')
    const preferences = {
      get: () => ({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }),
      getNavigationGroups: () => savedGroups,
      setNavigationGroupOpen,
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )

    ;(component as any).updateCurrent('/memory')

    expect(component.navigationGroupOpen).toEqual({ work: false, intelligence: true, system: false })
    const detail = document.createElement('details')
    detail.open = true
    const event = new Event('toggle')
    Object.defineProperty(event, 'target', { value: detail })
    component.persistNavigationGroupState('system', event)

    expect(component.navigationGroupOpen['system']).toBeTrue()
    expect(setNavigationGroupOpen).toHaveBeenCalledOnceWith('system', true)
    expect(preferences.setMode).not.toHaveBeenCalled()
    component.ngOnDestroy()
  })

  it('cycles the shared desktop rail through auto, compact, and expanded modes', () => {
    const component = createShell(() => of(status))
    const setNavigationMode = jasmine.createSpy('setNavigationMode')
    ;(component as any).preferences = { setNavigationMode }

    expect(component.navigationMode).toBe('auto')
    component.cycleNavigationMode()
    expect(component.navigationMode).toBe('compact')
    component.cycleNavigationMode()
    expect(component.navigationMode).toBe('expanded')
    component.cycleNavigationMode()
    expect(component.navigationMode).toBe('auto')
    expect(setNavigationMode.calls.allArgs()).toEqual([['compact'], ['expanded'], ['auto']])
    component.ngOnDestroy()
  })

  it('never claims safeguards are active when status is unavailable or inconsistent', () => {
    const unavailable = createShell(() => throwError(() => new Error('private provider detail')))
    unavailable.refreshSafetyStatus()
    expect(unavailable.safetyStatusUnknown).toBeTrue()
    expect(unavailable.safetyLabel).toBe('Safety status unknown')
    expect(unavailable.safetyDetail).not.toContain('private provider detail')

    const inconsistent = createShell(() => of({ ...status, modeStateError: 'persisted mode unavailable' }))
    inconsistent.refreshSafetyStatus()
    expect(inconsistent.safetyStatusUnknown).toBeTrue()
    expect(inconsistent.safetyLabel).toBe('Safety status unknown')
  })

  it('prioritizes a confirmed emergency stop over an unavailable mode value', () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    const component = createShell(() => of({ ...status, mode: 'unknown', modeStateError: 'mode unavailable', emergencyStop: { engaged: true, updatedAt: status.emergencyStop.updatedAt } }))
    component.refreshSafetyStatus()
    expect(component.safetyStatusUnknown).toBeFalse()
    expect(component.emergencyStopActive).toBeTrue()
    expect(component.safetyLabel).toBe('Emergency stop active')
  })

  it('closes mobile navigation on Escape and keeps focus out of collapsed destinations', () => {
    const component = createShell(() => of(status))
    const navigation = document.createElement('aside')
    const first = document.createElement('button')
    const collapsedGroup = document.createElement('details')
    const summary = document.createElement('summary')
    const hiddenDestination = document.createElement('a')
    hiddenDestination.href = '/memory'
    collapsedGroup.append(summary, hiddenDestination)
    navigation.append(first, collapsedGroup)
    document.body.appendChild(navigation)
    ;(component as any).mobileNavigation = { nativeElement: navigation }
    component.toggleMobileNavigation()
    expect(component.mobileNavigationOpen).toBeTrue()
    expect((component as any).mobileNavigation.nativeElement).toBe(navigation)
    expect(navigation.querySelectorAll('summary, button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])').length).toBe(3)

    summary.focus()
    expect(document.activeElement).toBe(summary)
    const forward = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    component.keepFocusInMobileNavigation(forward)
    expect(forward.defaultPrevented).toBeTrue()
    expect(document.activeElement).toBe(first)

    first.focus()
    const backward = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })
    component.keepFocusInMobileNavigation(backward)
    expect(backward.defaultPrevented).toBeTrue()
    expect(document.activeElement).toBe(summary)

    component.closeMobileNavigationOnEscape()
    expect(component.mobileNavigationOpen).toBeFalse()
    navigation.remove()
  })

  it('keeps the active navigation link visibly focused and named in compact mode', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        {
          provide: ModuleViewPreferencesService,
          useValue: {
            get: () => ({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'compact' }),
            getNavigationMode: () => 'compact',
            getNavigationGroups: () => ({ version: 1, openGroups: { work: true } }),
          },
        },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement
    document.body.appendChild(host)
    const activeLink = host.querySelector<HTMLAnchorElement>('.hai-navigation__item--active')

    expect(activeLink).not.toBeNull()
    expect(activeLink?.getAttribute('aria-label')).toBe('Command Center')
    expect(host.querySelector('.hai-app-shell')?.classList.contains('hai-app-shell--compact')).toBeTrue()
    fixture.componentInstance.toggleMobileNavigation()
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()
    const navigation = host.querySelector<HTMLElement>('.hai-navigation')!
    for (const element of [
      navigation,
      navigation.querySelector('nav')!,
      navigation.querySelector('.hai-navigation__group--current details')!,
      activeLink!,
    ]) {
      expect(getComputedStyle(element).visibility)
        .withContext(`${element.tagName}.${element.className} should be visible when mobile navigation is open`)
        .toBe('visible')
    }
    activeLink?.focus({ focusVisible: true })
    expect(document.activeElement).toBe(activeLink)
    expect(activeLink?.matches(':focus-visible')).toBeTrue()
    expect(getComputedStyle(activeLink!).outlineStyle).toBe('solid')
    expect(parseFloat(getComputedStyle(activeLink!).outlineWidth)).toBeGreaterThanOrEqual(2)
    expect(getComputedStyle(activeLink!).outlineColor).not.toBe('rgba(0, 0, 0, 0)')

    host.remove()
    fixture.destroy()
  })

  it('renders mobile dialog and inert state before queued focus without a manual update', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: {
          get: () => ({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }),
        } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    const navigation = root.querySelector<HTMLElement>('.hai-navigation')!
    const workspace = root.querySelector<HTMLElement>('.hai-workspace')!
    const activeLink = navigation.querySelector<HTMLAnchorElement>('.hai-navigation__item--active')!
    const menu = root.querySelector<HTMLButtonElement>('.hai-mobile-menu')!
    menu.style.display = 'inline-flex'
    const observedStates: boolean[] = []
    spyOn(activeLink, 'focus').and.callFake(() => {
      observedStates.push(navigation.getAttribute('role') === 'dialog'
        && navigation.classList.contains('hai-navigation--open') && workspace.hasAttribute('inert'))
    })
    menu.click()
    await afterNativeDisclosureUpdate()
    expect(observedStates).toEqual([true])

    const restoreStates: boolean[] = []
    spyOn(menu, 'focus').and.callFake(() => restoreStates.push(!workspace.hasAttribute('inert')))
    root.querySelector<HTMLButtonElement>('.hai-navigation-backdrop')!.click()
    await afterNativeDisclosureUpdate()
    expect(restoreStates).toEqual([true])
    root.remove()
    fixture.destroy()
  })

  it('moves keyboard focus into the mobile navigation when it opens', async () => {
    const component = createShell(() => of(status))
    const navigation = document.createElement('aside')
    const brand = document.createElement('a')
    brand.href = '/control-center'
    navigation.appendChild(brand)
    document.body.appendChild(navigation)
    ;(component as any).mobileNavigation = { nativeElement: navigation }
    component.toggleMobileNavigation()
    await afterNativeDisclosureUpdate()

    expect(document.activeElement).toBe(brand)
    component.closeMobileNavigation(false)
    navigation.remove()
  })

  it('opens mobile navigation at the current module, falling back to its group when collapsed', async () => {
    const component = createShell(() => of(status))
    component.current = moduleForUrl('/memory')

    const navigation = document.createElement('aside')
    const workGroup = document.createElement('section')
    workGroup.className = 'hai-navigation__group'
    const workDetails = document.createElement('details')
    workDetails.open = true
    workDetails.appendChild(document.createElement('summary'))
    workGroup.appendChild(workDetails)

    const intelligenceGroup = document.createElement('section')
    intelligenceGroup.className = 'hai-navigation__group hai-navigation__group--current'
    const intelligenceDetails = document.createElement('details')
    const intelligenceSummary = document.createElement('summary')
    const activeLink = document.createElement('a')
    activeLink.href = '/memory'
    activeLink.className = 'hai-navigation__item hai-navigation__item--active'
    intelligenceDetails.append(intelligenceSummary, activeLink)
    intelligenceGroup.appendChild(intelligenceDetails)
    navigation.append(workGroup, intelligenceGroup)
    document.body.appendChild(navigation)
    ;(component as any).mobileNavigation = { nativeElement: navigation }

    component.toggleMobileNavigation()
    await afterNativeDisclosureUpdate()
    expect(document.activeElement).toBe(intelligenceSummary)

    component.closeMobileNavigation(false)
    intelligenceDetails.open = true
    component.toggleMobileNavigation()
    await afterNativeDisclosureUpdate()
    expect(document.activeElement).toBe(activeLink)

    component.closeMobileNavigation(false)
    navigation.remove()
    component.ngOnDestroy()
  })

  it('moves focus to a visible navigation control when resizing across mobile breakpoints', async () => {
    const component = createShell(() => of(status))
    const navigation = document.createElement('aside')
    const currentGroup = document.createElement('section')
    currentGroup.className = 'hai-navigation__group--current'
    const currentGroupSummary = document.createElement('button')
    currentGroupSummary.className = 'hai-navigation__summary'
    currentGroup.appendChild(currentGroupSummary)
    const navigationLink = document.createElement('a')
    navigationLink.href = '/memory'
    navigation.append(currentGroup, navigationLink)
    const menuButton = document.createElement('button')
    document.body.append(navigation, menuButton)
    ;(component as any).mobileNavigation = { nativeElement: navigation }
    ;(component as any).mobileMenuButton = { nativeElement: menuButton }
    const viewportWidth = spyOnProperty(window, 'innerWidth', 'get').and.returnValue(1024)

    navigationLink.focus()
    viewportWidth.and.returnValue(375)
    component.handleNavigationBreakpointResize()
    await afterNativeDisclosureUpdate()

    expect(document.activeElement).toBe(menuButton)

    menuButton.focus()
    viewportWidth.and.returnValue(1024)
    component.handleNavigationBreakpointResize()
    await afterNativeDisclosureUpdate()

    expect(document.activeElement).toBe(currentGroupSummary)
    navigation.remove()
    menuButton.remove()
    component.ngOnDestroy()
  })

  it('returns keyboard focus after Escape and removes the mobile dialog state', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }), getNavigationGroups: () => ({ version: 1, openGroups: { work: true } }) } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    const menu = root.querySelector<HTMLButtonElement>('.hai-mobile-menu')!
    const navigation = root.querySelector<HTMLElement>('#hai-navigation')!
    const workspace = root.querySelector<HTMLElement>('.hai-workspace')!
    menu.style.display = 'inline-flex'
    menu.focus()
    menu.click()
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()

    expect(document.activeElement).toBe(root.querySelector('.hai-navigation__item--active'))
    expect(menu.getAttribute('aria-expanded')).toBe('true')
    expect(navigation.getAttribute('role')).toBe('dialog')
    expect(navigation.getAttribute('aria-modal')).toBe('true')
    expect(workspace.hasAttribute('inert')).toBeTrue()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()

    expect(document.activeElement).toBe(menu)
    expect(menu.getAttribute('aria-expanded')).toBe('false')
    expect(navigation.getAttribute('role')).toBeNull()
    expect(navigation.getAttribute('aria-modal')).toBeNull()
    expect(workspace.hasAttribute('inert')).toBeFalse()
    root.remove()
    fixture.destroy()
  })

  it('keeps compact and mobile navigation landmarks and controls accessibly named', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'compact', openSections: {} }), getNavigationMode: () => 'compact', getNavigationGroups: () => ({ version: 1, openGroups: { work: true, intelligence: true, system: true } }) } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    const navigation = root.querySelector<HTMLElement>('#hai-navigation')!

    expect(root.querySelector('.hai-brand')?.getAttribute('aria-label')).toBe('Open Command Center')
    expect(navigation.querySelector('nav')?.getAttribute('aria-label')).toBe('Primary navigation')
    for (const control of Array.from(root.querySelectorAll<HTMLButtonElement>('button:not([aria-hidden="true"])'))) {
      expect(control.getAttribute('aria-label')?.trim() || control.innerText.trim())
        .withContext(`Button should have a concise accessible name: ${control.className}`)
        .not.toBe('')
    }
    for (const link of Array.from(navigation.querySelectorAll<HTMLAnchorElement>('a[href]'))) {
      expect(link.getAttribute('aria-label')?.trim() || link.innerText.trim())
        .withContext(`Navigation link should have a concise accessible name: ${link.getAttribute('href')}`)
        .not.toBe('')
    }
    for (const icon of Array.from(root.querySelectorAll<HTMLElement>('.hai-global-bar i[nz-icon], .hai-navigation i[nz-icon]'))) {
      expect(icon.getAttribute('aria-hidden')).toBe('true')
    }

    root.remove()
    fixture.destroy()
  })

  it('uses the skip link to focus main content and gives it a visible focus indicator', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }), getNavigationGroups: () => ({ version: 1, openGroups: {} }) } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const initialUrl = window.location.href
    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    const skipLink = root.querySelector<HTMLAnchorElement>('.hai-skip-link')!
    const main = root.querySelector<HTMLElement>('#hai-main')!

    expect(root.querySelector('.hai-app-shell')?.firstElementChild).toBe(skipLink)
    expect(skipLink.getAttribute('href')).toBe(`${window.location.pathname}${window.location.search}#hai-main`)
    expect(main.tabIndex).toBe(-1)
    skipLink.focus({ focusVisible: true })
    expect(document.activeElement).toBe(skipLink)
    expect(skipLink.matches(':focus')).toBeTrue()
    expect(skipLink.matches(':focus-visible')).toBeTrue()
    await afterNativeDisclosureUpdate()
    expect(document.activeElement).withContext('Skip-link focus should survive the pending shell update').toBe(skipLink)
    expect(getComputedStyle(skipLink).top).toBe('12px')
    expect(skipLink.getBoundingClientRect().top).toBeGreaterThanOrEqual(0)
    expect(parseFloat(getComputedStyle(skipLink).outlineWidth)).toBeGreaterThanOrEqual(2)

    skipLink.click()
    fixture.detectChanges()

    expect(window.location.href).toBe(initialUrl)
    expect(document.activeElement).toBe(main)
    window.history.replaceState(window.history.state, '', initialUrl)
    root.remove()
    fixture.destroy()
  })

  it('keeps a skip-link activation on the current module route even with a root base URL', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }) } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()
    const initialUrl = window.location.href
    const base = document.createElement('base')
    base.href = '/'
    document.head.prepend(base)
    window.history.replaceState(null, '', '/workflow-engine?owner=robert#workflow-detail')
    const routeUrl = window.location.href
    const fixture = TestBed.createComponent(AppShellComponent)
    const router = TestBed.inject(Router)
    const navigate = spyOn(router, 'navigateByUrl')
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    const link = root.querySelector<HTMLAnchorElement>('.hai-skip-link')!
    const main = root.querySelector<HTMLElement>('#hai-main')!
    try {
      expect(new URL(link.href).pathname).toBe('/workflow-engine')
      const click = new MouseEvent('click', { bubbles: true, cancelable: true })
      link.dispatchEvent(click)
      expect(click.defaultPrevented).toBeTrue()
      expect(document.activeElement).toBe(main)
      expect(window.location.href).toBe(routeUrl)
      expect(navigate).not.toHaveBeenCalled()
    } finally {
      fixture.destroy()
      root.remove()
      base.remove()
      window.history.replaceState(null, '', initialUrl)
    }
  })

  it('provides responsive mobile navigation and disables shell motion when reduced motion is preferred', async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }), getNavigationGroups: () => ({ version: 1, openGroups: {} }) } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const mobile = findMediaRule('(max-width: 820px)')
    const mobileNavigation = mobile && findStyleRule(mobile.cssRules, '.hai-navigation')
    const openNavigation = mobile && findStyleRule(mobile.cssRules, '.hai-navigation--open')
    expect(mobileNavigation?.style.getPropertyValue('width')).toContain('min(86vw, 280px)')
    expect(mobileNavigation?.style.getPropertyValue('visibility')).toBe('hidden')
    expect(openNavigation?.style.getPropertyValue('visibility')).toBe('visible')
    expect(findStyleRule(mobile!.cssRules, '.hai-mobile-menu')?.style.getPropertyValue('display')).toBe('inline-flex')

    const reducedMotion = findMediaRule('prefers-reduced-motion: reduce')
    const reducedRule = reducedMotion && Array.from(reducedMotion.cssRules).find((rule): rule is CSSStyleRule =>
      rule instanceof CSSStyleRule && rule.style.getPropertyValue('transition-duration') === '0.01ms',
    )
    expect(reducedRule?.style.getPropertyValue('transition-duration')).toBe('0.01ms')
    expect(reducedRule?.style.getPropertyValue('transition-property')).toBe('none')
    expect(reducedRule?.style.getPropertyValue('animation-duration')).toBe('0.01ms')
    expect(reducedRule?.style.getPropertyValue('scroll-behavior')).toBe('auto')
    fixture.destroy()
  })

  it('cancels pending opening, close-restore, and desktop-breakpoint focus callbacks on teardown', () => {
    const pending = new Set<number>()
    const scheduled = new Map<number, TimerHandler>()
    const canceled: number[] = []
    let nextTimer = 1
    spyOn(window, 'setTimeout').and.callFake(((handler: TimerHandler) => {
      const timer = nextTimer++
      pending.add(timer)
      scheduled.set(timer, handler)
      return timer
    }) as typeof window.setTimeout)
    spyOn(window, 'clearTimeout').and.callFake(((timer: number) => {
      canceled.push(timer)
      pending.delete(timer)
    }) as typeof window.clearTimeout)

    const expectTeardownCancels = (component: AppShellComponent, schedule: () => void, assertNoStaleFocus: () => void) => {
      const previousTimers = new Set(pending)
      schedule()
      const addedTimers = [...pending].filter(timer => !previousTimers.has(timer))
      expect(addedTimers.length).toBe(1)
      const timer = addedTimers[0]

      component.ngOnDestroy()

      expect(canceled).toContain(timer)
      expect(pending.has(timer)).toBeFalse()
      expect(scheduled.has(timer)).toBeTrue()
      const handler = scheduled.get(timer)
      if (typeof handler === 'function') handler()
      assertNoStaleFocus()
    }

    const opening = createShell(() => of(status))
    const openingNavigation = document.createElement('aside')
    const openingLink = document.createElement('a')
    openingLink.href = '/memory'
    openingNavigation.appendChild(openingLink)
    ;(opening as any).mobileNavigation = { nativeElement: openingNavigation }
    const openingFocus = spyOn(openingLink, 'focus').and.callThrough()
    expectTeardownCancels(opening, () => opening.toggleMobileNavigation(), () => expect(openingFocus).not.toHaveBeenCalled())

    const closeRestore = createShell(() => of(status))
    const menuButton = document.createElement('button')
    document.body.appendChild(menuButton)
    ;(closeRestore as any).mobileMenuButton = { nativeElement: menuButton }
    closeRestore.mobileNavigationOpen = true
    const menuFocus = spyOn(menuButton, 'focus').and.callThrough()
    expectTeardownCancels(closeRestore, () => closeRestore.closeMobileNavigation(), () => expect(menuFocus).not.toHaveBeenCalled())
    menuButton.remove()

    const breakpoint = createShell(() => of(status))
    const navigation = document.createElement('aside')
    const closeButton = document.createElement('button')
    closeButton.className = 'hai-navigation__close'
    const currentGroup = document.createElement('button')
    currentGroup.className = 'hai-navigation__summary'
    const currentGroupContainer = document.createElement('div')
    currentGroupContainer.className = 'hai-navigation__group--current'
    currentGroupContainer.appendChild(currentGroup)
    navigation.append(closeButton, currentGroupContainer)
    document.body.appendChild(navigation)
    closeButton.focus()
    ;(breakpoint as any).mobileNavigation = { nativeElement: navigation }
    breakpoint.mobileNavigationOpen = true
    const currentGroupFocus = spyOn(currentGroup, 'focus').and.callThrough()
    spyOnProperty(window, 'innerWidth', 'get').and.returnValue(821)
    expectTeardownCancels(breakpoint, () => breakpoint.handleNavigationBreakpointResize(), () => expect(currentGroupFocus).not.toHaveBeenCalled())
    navigation.remove()
    expect(pending.size).toBe(0)
  })

  it('invalidates stale focus callbacks when the mobile navigation closes and reopens', () => {
    const pending = new Set<number>()
    const scheduled = new Map<number, TimerHandler>()
    let nextTimer = 1
    spyOn(window, 'setTimeout').and.callFake(((handler: TimerHandler) => {
      const timer = nextTimer++
      pending.add(timer)
      scheduled.set(timer, handler)
      return timer
    }) as typeof window.setTimeout)
    spyOn(window, 'clearTimeout').and.callFake(((timer: number) => pending.delete(timer)) as typeof window.clearTimeout)

    const component = createShell(() => of(status))
    const navigation = document.createElement('aside')
    const link = document.createElement('a')
    link.href = '/memory'
    navigation.appendChild(link)
    const menuButton = document.createElement('button')
    document.body.append(navigation, menuButton)
    ;(component as any).mobileNavigation = { nativeElement: navigation }
    ;(component as any).mobileMenuButton = { nativeElement: menuButton }
    const linkFocus = spyOn(link, 'focus').and.callThrough()
    const menuFocus = spyOn(menuButton, 'focus').and.callThrough()

    component.toggleMobileNavigation()
    const openingTimer = nextTimer - 1
    component.closeMobileNavigation()
    const closeRestoreTimer = nextTimer - 1
    component.toggleMobileNavigation()
    const reopenedTimer = nextTimer - 1

    expect(pending.has(openingTimer)).toBeFalse()
    expect(pending.has(closeRestoreTimer)).toBeFalse()
    expect(pending.has(reopenedTimer)).toBeTrue()

    const runScheduled = (timer: number) => {
      const handler = scheduled.get(timer)
      if (typeof handler === 'function') handler()
    }
    runScheduled(openingTimer)
    runScheduled(closeRestoreTimer)
    expect(linkFocus).not.toHaveBeenCalled()
    expect(menuFocus).not.toHaveBeenCalled()

    runScheduled(reopenedTimer)
    expect(linkFocus).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(link)
    component.ngOnDestroy()
    navigation.remove()
    menuButton.remove()
  })

  it('makes the workspace inert while the mobile navigation dialog is open', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      getNavigationGroups: () => ({ version: 1, openGroups: {} }),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setNavigationGroupOpen: jasmine.createSpy('setNavigationGroupOpen'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture: ComponentFixture<AppShellComponent> = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    expect(root.querySelector<HTMLButtonElement>('.hai-icon-control[aria-label="Change navigation layout. Current setting: Desktop rail: adaptive"]')).not.toBeNull()
    expect(root.querySelector<HTMLButtonElement>('.hai-navigation-layout-control[title="Desktop rail: adaptive"]')).not.toBeNull()
    expect(root.querySelector<HTMLButtonElement>('.hai-icon-control[aria-label="Switch to light theme"]')).not.toBeNull()
    expect(root.querySelector<HTMLButtonElement>('.hai-utility-control')?.getAttribute('aria-label'))
      .toBe('Advanced view for Command Center')
    expect(root.querySelector('.hai-safety-status__compact')?.textContent?.trim())
      .toBe(fixture.componentInstance.safetyCompactLabel)
    expect(root.querySelector<HTMLElement>('#hai-main')?.tabIndex).toBe(-1)
    const workspace = root.querySelector<HTMLElement>('.hai-workspace')!
    const menu = root.querySelector<HTMLButtonElement>('.hai-mobile-menu')!
    menu.style.display = 'inline-flex'
    expect(root.querySelector<HTMLAnchorElement>('.hai-brand')?.getAttribute('href')).toBe('/control-center')
    expect(root.querySelector<HTMLAnchorElement>('.hai-navigation__item[href="/control-center"]')).not.toBeNull()
    expect(root.querySelector<HTMLDetailsElement>('[data-hai-nav-group="work"]')?.open).toBeTrue()
    expect(root.querySelector<HTMLDetailsElement>('[data-hai-nav-group="intelligence"]')?.open).toBeFalse()
    expect(root.querySelector('summary[aria-label*="current module Command Center"]')).not.toBeNull()
    expect(workspace.hasAttribute('inert')).toBeFalse()

    const workGroup = root.querySelector<HTMLDetailsElement>('[data-hai-nav-group="work"]')!
    workGroup.querySelector('summary')!.click()
    await afterNativeDisclosureUpdate()
    fixture.detectChanges()
    expect(workGroup.open).toBeFalse()
    expect(preferences.setNavigationGroupOpen).toHaveBeenCalledWith('work', false)
    expect(workGroup.querySelector('summary')?.getAttribute('aria-label')).toContain('current module Command Center')
    workGroup.querySelector('summary')!.click()
    await afterNativeDisclosureUpdate()
    fixture.detectChanges()

    menu.click()
    fixture.detectChanges()

    const navigation = root.querySelector<HTMLElement>('#hai-navigation')!
    const backdrop = root.querySelector<HTMLButtonElement>('.hai-navigation-backdrop')!
    expect(navigation.getAttribute('role')).toBe('dialog')
    expect(navigation.getAttribute('aria-modal')).toBe('true')
    expect(navigation.querySelector<HTMLButtonElement>('.hai-navigation__close')?.getAttribute('aria-label')).toBe('Close navigation')
    expect(workspace.hasAttribute('inert')).toBeTrue()
    expect(workspace.getAttribute('aria-hidden')).toBe('true')
    expect(backdrop.getAttribute('aria-hidden')).toBe('true')
    expect(backdrop.tabIndex).toBe(-1)

    navigation.querySelector<HTMLButtonElement>('.hai-navigation__close')!.click()
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()
    expect(workspace.hasAttribute('inert')).toBeFalse()
    expect(workspace.hasAttribute('aria-hidden')).toBeFalse()
    expect(navigation.getAttribute('role')).toBeNull()
    expect(document.activeElement).toBe(menu)
    fixture.destroy()
  })

  it('releases the mobile navigation modal state at the desktop breakpoint and preserves visible focus', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      getNavigationGroups: () => ({ version: 1, openGroups: {} }),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setNavigationGroupOpen: jasmine.createSpy('setNavigationGroupOpen'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const component = fixture.componentInstance
    const viewportWidth = spyOnProperty(window, 'innerWidth', 'get').and.returnValue(820)
    fixture.detectChanges()

    const root = fixture.nativeElement as HTMLElement
    const navigation = root.querySelector<HTMLElement>('#hai-navigation')!
    const workspace = root.querySelector<HTMLElement>('.hai-workspace')!
    const menu = root.querySelector<HTMLButtonElement>('.hai-mobile-menu')!
    menu.style.display = 'inline-flex'
    menu.click()
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()
    const closeButton = navigation.querySelector<HTMLButtonElement>('.hai-navigation__close')!
    closeButton.style.display = 'inline-flex'
    closeButton.focus()
    expect(document.activeElement).toBe(closeButton)
    expect(workspace.hasAttribute('inert')).toBeTrue()

    window.dispatchEvent(new Event('resize'))
    fixture.detectChanges()
    expect(component.mobileNavigationOpen).toBeTrue()
    expect(workspace.hasAttribute('inert')).toBeTrue()

    viewportWidth.and.returnValue(821)
    // Karma's CSS viewport remains mobile-sized even when innerWidth is stubbed.
    navigation.style.visibility = 'visible'
    navigation.style.transform = 'none'
    window.dispatchEvent(new Event('resize'))
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()
    fixture.detectChanges()

    const currentGroupSummary = navigation.querySelector<HTMLElement>('.hai-navigation__group--current .hai-navigation__summary')!
    expect(component.mobileNavigationOpen).toBeFalse()
    expect(workspace.hasAttribute('inert')).toBeFalse()
    expect(workspace.hasAttribute('aria-hidden')).toBeFalse()
    expect(navigation.getAttribute('role')).toBeNull()
    expect(navigation.getAttribute('aria-modal')).toBeNull()
    expect(document.activeElement).toBe(currentGroupSummary)
    fixture.destroy()
  })

  it('keeps authenticated onboarding in the shell without showing operational navigation', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    spyOnProperty(TestBed.inject(Router), 'url', 'get').and.returnValue('/onboarding')
    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    expect(fixture.componentInstance.current).toEqual(moduleForUrl('/onboarding'))
    const root = fixture.nativeElement as HTMLElement
    expect(root.querySelector('#hai-main')).not.toBeNull()
    expect(root.querySelector('.hai-skip-link')).not.toBeNull()
    expect(root.querySelector('.hai-navigation')).toBeNull()
    expect(root.querySelector('.hai-global-bar')).toBeNull()
    expect(root.querySelector('.hai-app-shell--minimal')).not.toBeNull()
    fixture.destroy()
  })

  it('restores mobile navigation focus only after the workspace is no longer inert', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const workspace = root.querySelector<HTMLElement>('.hai-workspace')!
    const menu = root.querySelector<HTMLButtonElement>('.hai-mobile-menu')!
    menu.style.display = 'inline-flex'
    menu.click()
    fixture.detectChanges()
    await afterNativeDisclosureUpdate()
    expect(workspace.hasAttribute('inert')).toBeTrue()

    const focusWhileInert: boolean[] = []
    const nativeFocus = menu.focus.bind(menu)
    spyOn(menu, 'focus').and.callFake(() => {
      focusWhileInert.push(workspace.hasAttribute('inert'))
      nativeFocus()
    })
    root.querySelector<HTMLButtonElement>('.hai-navigation-backdrop')!.click()
    fixture.detectChanges()
    expect(workspace.hasAttribute('inert')).toBeFalse()
    await afterNativeDisclosureUpdate()

    expect(focusWhileInert).toEqual([false])
    expect(document.activeElement).toBe(menu)
    fixture.destroy()
  })

  it('moves focus to the destination after a mobile navigation link is selected', async () => {
    const router = {
      url: '/control-center',
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(true)),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      {} as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl('/control-center')
    component.mobileNavigationOpen = true

    const menuButton = document.createElement('button')
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    document.body.append(menuButton, main)
    ;(component as any).mobileMenuButton = { nativeElement: menuButton }
    spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    const event = new MouseEvent('click', { button: 0, bubbles: true, cancelable: true })
    component.navigateFromNavigation(moduleForUrl('/memory'), event)
    await Promise.resolve()

    expect(event.defaultPrevented).toBeTrue()
    expect(router.navigateByUrl).toHaveBeenCalledOnceWith('/memory')
    expect(component.mobileNavigationOpen).toBeFalse()
    expect(document.activeElement).toBe(main)

    menuButton.remove()
    main.remove()
    component.ngOnDestroy()
  })

  it('returns focus to the mobile menu when a same-module route change is canceled', async () => {
    const router = {
      url: '/memory/record/123',
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(false)),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      {} as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl(router.url)
    component.mobileNavigationOpen = true

    const menuButton = document.createElement('button')
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    document.body.append(menuButton, main)
    ;(component as any).mobileMenuButton = { nativeElement: menuButton }
    spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    const event = new MouseEvent('click', { button: 0, bubbles: true, cancelable: true })
    component.navigateFromNavigation(moduleForUrl('/memory'), event)
    await Promise.resolve()

    expect(router.navigateByUrl).toHaveBeenCalledOnceWith('/memory')
    expect(component.mobileNavigationOpen).toBeFalse()
    expect(document.activeElement).toBe(menuButton)

    menuButton.remove()
    main.remove()
    component.ngOnDestroy()
  })

  it('focuses module content when its current root route is selected again', async () => {
    const router = {
      url: '/memory',
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(false)),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      {} as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl(router.url)
    component.mobileNavigationOpen = true

    const menuButton = document.createElement('button')
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    document.body.append(menuButton, main)
    ;(component as any).mobileMenuButton = { nativeElement: menuButton }
    spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    component.navigateFromNavigation(
      moduleForUrl('/memory'),
      new MouseEvent('click', { button: 0, bubbles: true, cancelable: true }),
    )
    await Promise.resolve()

    expect(document.activeElement).toBe(main)

    menuButton.remove()
    main.remove()
    component.ngOnDestroy()
  })

  it('moves keyboard focus to main content after a desktop navigation route change', async () => {
    const router = {
      url: '/control-center',
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(true)),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      {} as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    document.body.appendChild(main)
    spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    const event = new MouseEvent('click', { button: 0, bubbles: true, cancelable: true })
    component.navigateFromNavigation(moduleForUrl('/memory'), event)
    await Promise.resolve()

    expect(event.defaultPrevented).toBeTrue()
    expect(router.navigateByUrl).toHaveBeenCalledOnceWith('/memory')
    expect(document.activeElement).toBe(main)

    main.remove()
    component.ngOnDestroy()
  })

  it('updates the shared module title from successful Angular route events', () => {
    const events = new Subject<NavigationEnd>()
    const router = { url: '/control-center', events }
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      getNavigationMode: () => 'auto',
      getNavigationGroups: () => ({ version: 1, openGroups: {} }),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.ngOnInit()
    expect(component.current.title).toBe('Command Center')

    events.next(new NavigationEnd(1, '/memory', '/memory'))

    expect(component.current.id).toBe('memory')
    expect(component.current.title).toBe('Memory')
    component.ngOnDestroy()
  })

  it('moves focus to main after router-driven module changes without stealing focus for same-module URLs', () => {
    const events = new Subject<NavigationEnd>()
    const router = { url: '/control-center', events }
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      getNavigationMode: () => 'auto',
      getNavigationGroups: () => ({ version: 1 as const, openGroups: {} }),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    const oldNavigationLink = document.createElement('a')
    oldNavigationLink.href = '/control-center'
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    document.body.append(oldNavigationLink, main)
    const requestFrame = spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    component.ngOnInit()
    oldNavigationLink.focus()
    expect(document.activeElement).toBe(oldNavigationLink)

    events.next(new NavigationEnd(2, '/control-center', '/memory'))

    expect(component.current.id).toBe('memory')
    expect(document.activeElement).toBe(main)
    expect(requestFrame).toHaveBeenCalledTimes(1)

    const inPageControl = document.createElement('button')
    document.body.appendChild(inPageControl)
    inPageControl.focus()
    events.next(new NavigationEnd(3, '/memory?tab=records', '/memory?tab=history'))

    expect(document.activeElement).toBe(inPageControl)
    expect(requestFrame).toHaveBeenCalledTimes(1)

    component.ngOnDestroy()
    oldNavigationLink.remove()
    main.remove()
    inPageControl.remove()
  })

  it('removes only stale Advanced deep-link state when switching to Basic', () => {
    const serializer = new DefaultUrlSerializer()
    const router = {
      url: '/runtime-lab?owner=robert&mode=advanced&filter=active#runtime-inventory-openclaw',
      parseUrl: jasmine.createSpy('parseUrl').and.callFake((url: string) => serializer.parse(url)),
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(true)),
    }
    const preferences = { setMode: jasmine.createSpy('setMode') }
    const component = new AppShellComponent(
      router as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl('/runtime-lab')
    component.viewMode = 'advanced'
    ;(component as any).deepLinkedSectionId = 'runtime-inventory-openclaw'

    component.toggleViewMode()

    const rewritten = router.navigateByUrl.calls.mostRecent().args[0] as UrlTree
    expect(component.viewMode).toBe('basic')
    expect(preferences.setMode).toHaveBeenCalledOnceWith('runtime-lab', 'basic')
    expect(rewritten.queryParams).toEqual({ owner: 'robert', filter: 'active' })
    expect(rewritten.fragment).toBeNull()
    expect((component as any).deepLinkedSectionId).toBe('')
    expect(router.navigateByUrl.calls.mostRecent().args[1]).toEqual({ replaceUrl: true })
    expect(router.parseUrl).toHaveBeenCalledOnceWith(router.url)
    component.ngOnDestroy()
  })

  it('keeps the global theme control synchronized with module theme controls', async () => {
    const themeChanges = new Subject<'light' | 'dark'>()
    const themeService = {
      mode: () => 'dark',
      toggle: jasmine.createSpy('toggle').and.callFake(() => {
        themeChanges.next('light')
        return 'light'
      }),
      changes$: themeChanges.asObservable(),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }), setMode: jasmine.createSpy('setMode'), setNavigationMode: jasmine.createSpy('setNavigationMode'), setSection: jasmine.createSpy('setSection') } },
        { provide: ThemeService, useValue: themeService },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement
    const themeButton = Array.from(host.querySelectorAll<HTMLButtonElement>('.hai-icon-control')).find((button) => button.title.includes('theme'))!

    expect(themeButton.title).toContain('light theme')
    expect(themeChanges.observed).toBeTrue()
    themeButton.click()
    expect(themeService.toggle).toHaveBeenCalledTimes(1)
    expect(fixture.componentInstance.themeMode).toBe('light')
    fixture.detectChanges()
    expect(themeButton.title).toContain('dark theme')
    fixture.destroy()
    themeChanges.complete()
  })

  it('clears an Advanced deep link when resetting only the current module view', () => {
    const serializer = new DefaultUrlSerializer()
    const router = {
      url: '/runtime-lab?owner=robert&mode=advanced&filter=active#runtime-inventory-openclaw',
      parseUrl: jasmine.createSpy('parseUrl').and.callFake((url: string) => serializer.parse(url)),
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(true)),
    }
    const defaults = { version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }
    const preferences = {
      get: jasmine.createSpy('get').and.returnValue(defaults),
      reset: jasmine.createSpy('reset').and.returnValue(defaults),
      isSessionOnly: jasmine.createSpy('isSessionOnly').and.returnValue(false),
      getNavigationMode: jasmine.createSpy('getNavigationMode').and.returnValue('auto'),
    }
    const component = new AppShellComponent(
      router as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl('/runtime-lab')
    ;(component as any).deepLinkedSectionId = 'runtime-inventory-openclaw'

    component.resetCurrentModuleView()

    const rewritten = router.navigateByUrl.calls.mostRecent().args[0] as UrlTree
    expect(preferences.reset).toHaveBeenCalledOnceWith('runtime-lab')
    expect(rewritten.queryParams).toEqual({ owner: 'robert', filter: 'active' })
    expect(rewritten.fragment).toBeNull()
    expect(component.viewMode).toBe('basic')
    expect((component as any).deepLinkedSectionId).toBe('')
    expect(component.viewResetNotice).toContain('View settings reset')

    ;(component as any).updateCurrent('/runtime-lab?owner=robert&filter=active')

    expect(component.viewResetNotice).toContain('View settings reset')
    component.ngOnDestroy()
  })

  it('clears a registered Basic deep link on reset without changing other modules or rail preferences', () => {
    const serializer = new DefaultUrlSerializer()
    const router = {
      url: '/plans?owner=robert#create-preview',
      parseUrl: (url: string) => serializer.parse(url),
      navigateByUrl: jasmine.createSpy('navigateByUrl').and.returnValue(Promise.resolve(true)),
    }
    const preferences = new ModuleViewPreferencesService(document)
    const moduleId = 'plan-coordination'
    preferences.reset(moduleId)
    preferences.reset('workflow-engine')
    preferences.setMode('workflow-engine', 'advanced')
    preferences.setSection('workflow-engine', 'workflow-detail', true)
    preferences.setNavigationMode('compact')
    const component = new AppShellComponent(
      router as unknown as Router,
      preferences,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    try {
      ;(component as any).updateCurrent(router.url)
      expect(preferences.get(moduleId).openSections['create-preview']).toBeTrue()
      component.resetCurrentModuleView()
      expect(router.navigateByUrl).toHaveBeenCalled()
      const rewritten = router.navigateByUrl.calls.mostRecent().args[0] as UrlTree
      expect(rewritten.fragment).toBeNull()
      expect(rewritten.queryParams).toEqual({ owner: 'robert' })
      ;(component as any).updateCurrent(serializer.serialize(rewritten))
      expect(preferences.get(moduleId).openSections).toEqual({})
      expect(preferences.get('workflow-engine').mode).toBe('advanced')
      expect(preferences.get('workflow-engine').openSections['workflow-detail']).toBeTrue()
      expect(preferences.getNavigationMode()).toBe('compact')
    } finally {
      component.ngOnDestroy()
      preferences.reset(moduleId)
      preferences.reset('workflow-engine')
      localStorage.removeItem('hai.shell-navigation.v1')
    }
  })

  it('does not let delayed reset disclosure events recreate saved section state', () => {
    const defaults = { version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }
    const setSection = jasmine.createSpy('setSection')
    const preferences = {
      reset: jasmine.createSpy('reset').and.returnValue(defaults),
      isSessionOnly: jasmine.createSpy('isSessionOnly').and.returnValue(false),
      setSection,
    }
    const component = new AppShellComponent(
      { url: '/runtime-lab' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    component.current = moduleForUrl('/runtime-lab')
    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    const detail = document.createElement('details')
    detail.className = 'advanced-section'
    detail.dataset['haiSection'] = 'runtime-feature-parity'
    detail.open = true
    outlet.appendChild(detail)
    document.body.appendChild(outlet)

    component.resetCurrentModuleView()
    expect(detail.open).toBeFalse()

    const delayedToggle = new Event('toggle')
    Object.defineProperty(delayedToggle, 'target', { value: detail })
    ;(component as any).persistDetailState(delayedToggle)
    expect(setSection).not.toHaveBeenCalled()

    detail.open = true
    const userToggle = new Event('toggle')
    Object.defineProperty(userToggle, 'target', { value: detail })
    ;(component as any).persistDetailState(userToggle)
    expect(setSection).toHaveBeenCalledOnceWith('runtime-lab', 'runtime-feature-parity', true)

    outlet.remove()
    component.ngOnDestroy()
  })

  it('exposes a current-module-only view reset and reports when it cannot persist', async () => {
    const defaults = { version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }
    const preferences = {
      get: () => defaults,
      reset: jasmine.createSpy('reset').and.returnValue(defaults),
      isSessionOnly: jasmine.createSpy('isSessionOnly').and.returnValue(true),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const component = fixture.componentInstance
    const moduleId = component.current.id
    component.viewMode = 'advanced'
    component.navigationMode = 'compact'
    document.body.classList.add('hai-view-advanced')

    const host = fixture.nativeElement as HTMLElement
    const outlet = host.querySelector<HTMLElement>('.hai-module-outlet')!
    const progressive = document.createElement('hai-progressive-section')
    const trigger = document.createElement('button')
    trigger.className = 'hai-progressive-section__summary'
    trigger.setAttribute('aria-expanded', 'true')
    const closeProgressive = jasmine.createSpy('closeProgressive').and.callFake(() => trigger.setAttribute('aria-expanded', 'false'))
    trigger.addEventListener('click', closeProgressive)
    progressive.appendChild(trigger)
    outlet.appendChild(progressive)
    const details = document.createElement('details')
    details.className = 'advanced-section'
    details.open = true
    outlet.appendChild(details)

    const button = host.querySelector<HTMLButtonElement>('.hai-reset-view')!
    expect(button.getAttribute('aria-label')).toBe(`Reset view for ${component.current.title}`)
    button.click()
    fixture.detectChanges()

    expect(preferences.reset).toHaveBeenCalledOnceWith(moduleId)
    expect(preferences.isSessionOnly).toHaveBeenCalledOnceWith(moduleId)
    expect(component.viewMode).toBe('basic')
    expect(component.navigationMode).toBe('compact')
    expect(document.body.classList.contains('hai-view-advanced')).toBeFalse()
    expect(closeProgressive).toHaveBeenCalledTimes(1)
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(details.open).toBeFalse()
    expect(host.querySelector('[role="status"]')?.textContent).toContain('only for this session')
    expect(host.querySelector('[role="status"]')?.textContent).toContain('may return after reload')

    fixture.destroy()
  })

  it('keeps an accessible per-module reset action in the compact view settings disclosure', async () => {
    const defaults = { version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }
    const preferences = {
      get: () => defaults,
      reset: jasmine.createSpy('reset').and.returnValue(defaults),
      isSessionOnly: jasmine.createSpy('isSessionOnly').and.returnValue(false),
      setMode: jasmine.createSpy('setMode'),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', toggle: () => 'light' } },
        { provide: RuntimeControlService, useValue: { status: () => of(status) } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement
    const menu = host.querySelector<HTMLDetailsElement>('.hai-compact-view-menu')!
    const summary = menu.querySelector<HTMLElement>(':scope > summary')!
    const resetButton = menu.querySelector<HTMLButtonElement>('.hai-compact-reset-view')!
    const component = fixture.componentInstance
    const moduleId = component.current.id
    menu.style.display = 'block'
    menu.open = true
    spyOn(window, 'requestAnimationFrame').and.callFake((callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })

    expect(summary.getAttribute('aria-label')).toBe(`More view settings for ${component.current.title}`)
    expect(resetButton.textContent).toContain(`Reset view for ${component.current.title}`)
    resetButton.click()
    fixture.detectChanges()

    expect(preferences.reset).toHaveBeenCalledOnceWith(moduleId)
    expect(menu.open).toBeFalse()
    expect(document.activeElement).toBe(summary)
    expect(component.viewMode).toBe('basic')
    fixture.destroy()
  })

  it('persists Runtime Lab details only when their stable disclosure is toggled', () => {
    const setSection = jasmine.createSpy('setSection')
    const preferences = { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }), setSection }
    const component = new AppShellComponent(
      { url: '/runtime-lab' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    ;(component as any).current = { id: 'runtime-lab' }
    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    document.body.appendChild(outlet)

    for (const [className, sectionId] of [
      ['advanced-section', 'runtime-feature-parity'],
      ['advanced-block', 'runtime-inventory-openclaw'],
      ['advanced-section', 'runtime-mcp-readiness'],
    ]) {
      const detail = document.createElement('details')
      detail.classList.add(className)
      detail.dataset['haiSection'] = sectionId
      detail.open = true
      outlet.appendChild(detail)
      const event = new Event('toggle')
      Object.defineProperty(event, 'target', { value: detail })
      ;(component as any).persistDetailState(event)
      expect(setSection).toHaveBeenCalledWith('runtime-lab', sectionId, true)
    }
    outlet.remove()
    component.ngOnDestroy()
  })

  it('restores saved Runtime Lab disclosures independently in Basic mode', () => {
    const sections: Record<string, boolean> = {
      'runtime-feature-parity': true,
      'runtime-inventory-openclaw': false,
      'runtime-mcp-readiness': true,
    }
    const component = new AppShellComponent(
      { url: '/runtime-lab' } as unknown as Router,
      { get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: sections }) } as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    ;(component as any).current = { id: 'runtime-lab' }

    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    for (const sectionId of Object.keys(sections)) {
      const detail = document.createElement('details')
      detail.className = sectionId === 'runtime-inventory-openclaw' ? 'advanced-block' : 'advanced-section'
      detail.dataset['haiSection'] = sectionId
      outlet.appendChild(detail)
    }
    document.body.appendChild(outlet)

    ;(component as any).restoreDetailState()

    expect(outlet.querySelector('[data-hai-section="runtime-feature-parity"]')?.hasAttribute('open')).toBeTrue()
    expect(outlet.querySelector('[data-hai-section="runtime-inventory-openclaw"]')?.hasAttribute('open')).toBeFalse()
    expect(outlet.querySelector('[data-hai-section="runtime-mcp-readiness"]')?.hasAttribute('open')).toBeTrue()
    outlet.remove()
  })

  it('opens the requested advanced section when a supported deep link is loaded', () => {
    let mode: 'basic' | 'advanced' = 'basic'
    const preferences = {
      get: () => ({ mode, navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode').and.callFake((_moduleId: string, value: 'basic' | 'advanced') => {
        mode = value
      }),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )

    ;(component as any).updateCurrent('/governance-control?mode=advanced#agent-registry')

    expect(component.current.id).toBe('governance-control')
    expect(component.viewMode).toBe('advanced')
    expect(preferences.setMode).toHaveBeenCalledOnceWith('governance-control', 'advanced')
    component.ngOnDestroy()
  })

  it('opens registered parent disclosures before a dynamic deep-linked detail', () => {
    let mode: 'basic' | 'advanced' = 'basic'
    const setSection = jasmine.createSpy('setSection')
    const preferences = {
      get: () => ({ mode, navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode').and.callFake((_moduleId: string, value: 'basic' | 'advanced') => {
        mode = value
      }),
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection,
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )

    ;(component as any).updateCurrent('/runtime-lab?mode=advanced#runtime-inventory-openclaw')

    expect(component.current.id).toBe('runtime-lab')
    expect(component.viewMode).toBe('advanced')
    expect(setSection).toHaveBeenCalledWith('runtime-lab', 'runtime-inventory-openclaw', true)
    expect(setSection).toHaveBeenCalledWith('runtime-lab', 'runtime-feature-parity', true)
    component.ngOnDestroy()
  })

  it('opens and scrolls a three-level registered disclosure path in order', () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    const component = createShell(() => of(status))
    ;(component as any).deepLinkedSectionId = 'outcome-monitor-composition'

    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    const outer = document.createElement('hai-progressive-section')
    outer.id = 'advisory-engines'
    const outerTrigger = document.createElement('button')
    outerTrigger.className = 'hai-progressive-section__summary'
    outerTrigger.setAttribute('aria-expanded', 'false')
    outer.appendChild(outerTrigger)
    const middle = document.createElement('hai-progressive-section')
    middle.id = 'outcome-evaluations'
    const middleTrigger = document.createElement('button')
    middleTrigger.className = 'hai-progressive-section__summary'
    middleTrigger.setAttribute('aria-expanded', 'false')
    middle.appendChild(middleTrigger)
    const target = document.createElement('hai-progressive-section')
    target.id = 'outcome-monitor-composition'
    const targetTrigger = document.createElement('button')
    targetTrigger.className = 'hai-progressive-section__summary'
    targetTrigger.setAttribute('aria-expanded', 'false')
    target.appendChild(targetTrigger)
    const opened: string[] = []
    outerTrigger.addEventListener('click', () => {
      outerTrigger.setAttribute('aria-expanded', 'true')
      opened.push(outer.id)
      outer.appendChild(middle)
    })
    middleTrigger.addEventListener('click', () => {
      middleTrigger.setAttribute('aria-expanded', 'true')
      opened.push(middle.id)
      middle.appendChild(target)
    })
    targetTrigger.addEventListener('click', () => {
      targetTrigger.setAttribute('aria-expanded', 'true')
      opened.push(target.id)
    })
    const scrollIntoView = jasmine.createSpy('scrollIntoView')
    Object.defineProperty(target, 'scrollIntoView', { configurable: true, value: scrollIntoView })
    outlet.appendChild(outer)
    document.body.appendChild(outlet)
    spyOn(window, 'setTimeout').and.callFake(((callback: TimerHandler) => {
      if (typeof callback === 'function') callback()
      return 1
    }) as typeof window.setTimeout)

    ;(component as any).openDeepLinkedDisclosurePath(
      ['advisory-engines', 'outcome-evaluations', 'outcome-monitor-composition'],
      'outcome-monitor-composition',
      0,
    )

    expect(opened).toEqual(['advisory-engines', 'outcome-evaluations', 'outcome-monitor-composition'])
    expect(scrollIntoView).toHaveBeenCalledOnceWith({ block: 'start' })
    expect(document.activeElement).toBe(targetTrigger)
    outlet.remove()
    component.ngOnDestroy()
  })

  it('does not move focus for an unregistered fragment or a registered section missing from the rendered page', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    const focusedControl = document.createElement('button')
    const outlet = document.createElement('main')
    outlet.className = 'hai-module-outlet'
    document.body.append(focusedControl, outlet)
    focusedControl.focus()

    ;(component as any).updateCurrent('/hai-os#invented-section')
    await afterNativeDisclosureUpdate()
    expect(component.current.id).toBe('hai-os')
    expect((component as any).deepLinkedSectionId).toBe('')
    expect(document.activeElement).toBe(focusedControl)

    ;(component as any).updateCurrent('/hai-os#system-metrics')
    await afterNativeDisclosureUpdate()
    expect((component as any).deepLinkedSectionId).toBe('system-metrics')
    expect(preferences.setSection).toHaveBeenCalledWith('hai-os', 'system-metrics', true)
    expect(document.activeElement).toBe(focusedControl)

    outlet.remove()
    focusedControl.remove()
    component.ngOnDestroy()
  })

  it('cancels pending deep-link focus when navigation moves to an unrelated module', async () => {
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      setMode: jasmine.createSpy('setMode'),
      setSection: jasmine.createSpy('setSection'),
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )
    const outlet = document.createElement('main')
    outlet.className = 'hai-module-outlet'
    const section = document.createElement('section')
    section.id = 'system-metrics'
    const trigger = document.createElement('button')
    trigger.className = 'hai-progressive-section__summary'
    trigger.setAttribute('aria-expanded', 'false')
    section.appendChild(trigger)
    outlet.appendChild(section)
    const focusedControl = document.createElement('button')
    document.body.append(outlet, focusedControl)
    focusedControl.focus()

    ;(component as any).updateCurrent('/hai-os#system-metrics')
    ;(component as any).updateCurrent('/memory')
    await afterNativeDisclosureUpdate()

    expect(component.current.id).toBe('memory')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).not.toBe(trigger)

    outlet.remove()
    focusedControl.remove()
    component.ngOnDestroy()
  })

  it('focuses a late deep-linked disclosure only after its summary renders', async () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    const preferences = { get: () => ({ mode: 'basic', openSections: {} }), setMode() {}, setSection() {} }
    const component = new AppShellComponent(
      { url: '/hai-os#system-metrics' } as Router,
      preferences as unknown as ModuleViewPreferencesService,
      {} as ThemeService, {} as RuntimeControlService,
    )
    const outlet = document.createElement('main')
    outlet.className = 'hai-module-outlet'
    document.body.append(outlet)
    try {
      ;(component as any).updateCurrent('/hai-os#system-metrics')
      await afterNativeDisclosureUpdate()
      const section = document.createElement('hai-progressive-section')
      section.id = 'system-metrics'
      outlet.append(section)
      await afterNativeDisclosureUpdate()
      expect(document.activeElement).not.toBe(section)
      const trigger = document.createElement('button')
      trigger.className = 'hai-progressive-section__summary'
      trigger.setAttribute('aria-expanded', 'true')
      section.append(trigger)
      await afterNativeDisclosureUpdate()
      await afterNativeDisclosureUpdate()
      expect(document.activeElement).toBe(trigger)
      expect((component as any).deepLinkObserver).toBeUndefined()
      expect((component as any).deepLinkExpiryTimer).toBeUndefined()
    } finally {
      component.ngOnDestroy()
      outlet.remove()
    }
  })

  it('recovers deep-link focus after tab visibility, class, or inline style changes', async () => {
    const hidden = spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    for (const cause of ['tab', 'class', 'style']) {
      const preferences = { get: () => ({ mode: 'basic', openSections: {} }), setMode() {}, setSection() {} }
      const component = new AppShellComponent(
        { url: '/hai-os#system-metrics' } as Router,
        preferences as unknown as ModuleViewPreferencesService,
        {} as ThemeService, {} as RuntimeControlService,
      )
      spyOn(component, 'refreshSafetyStatus')
      const outlet = document.createElement('div')
      outlet.className = 'hai-module-outlet'
      const style = document.createElement('style')
      style.textContent = '.hai-regression-hidden { display: none; }'
      const section = document.createElement('hai-progressive-section')
      section.id = 'system-metrics'
      const trigger = document.createElement('button')
      trigger.className = 'hai-progressive-section__summary'
      trigger.setAttribute('aria-expanded', 'true')
      if (cause === 'class') trigger.classList.add('hai-regression-hidden')
      if (cause === 'style') trigger.style.display = 'none'
      hidden.and.returnValue(cause === 'tab')
      section.append(trigger)
      outlet.append(style, section)
      document.body.append(outlet)
      try {
        ;(component as any).updateCurrent('/hai-os#system-metrics')
        await afterNativeDisclosureUpdate()
        expect(document.activeElement).not.toBe(trigger)
        expect((component as any).deepLinkObserver).toBeDefined()
        if (cause === 'tab') {
          hidden.and.returnValue(false)
          ;(component as any).onVisibilityChange()
        } else if (cause === 'class') trigger.classList.remove('hai-regression-hidden')
        else trigger.style.removeProperty('display')
        await afterNativeDisclosureUpdate()
        await afterNativeDisclosureUpdate()
        expect(document.activeElement).toBe(trigger)
        expect((component as any).deepLinkObserver).toBeUndefined()
      } finally {
        component.ngOnDestroy()
        outlet.remove()
        hidden.and.returnValue(false)
      }
    }
  })

  it('does not refocus a late disclosure after trusted user input cancels restoration', async () => {
    const preferences = { get: () => ({ mode: 'basic', openSections: {} }), setMode() {}, setSection() {} }
    const component = new AppShellComponent(
      { url: '/hai-os#system-metrics' } as Router,
      preferences as unknown as ModuleViewPreferencesService,
      {} as ThemeService, {} as RuntimeControlService,
    )
    const outlet = document.createElement('main')
    outlet.className = 'hai-module-outlet'
    const control = document.createElement('button')
    document.body.append(outlet, control)
    try {
      ;(component as any).updateCurrent('/hai-os#system-metrics')
      await afterNativeDisclosureUpdate()
      control.focus()
      ;(component as any).onDeepLinkInteraction({ isTrusted: true })
      const section = document.createElement('section')
      section.id = 'system-metrics'
      const trigger = document.createElement('button')
      trigger.className = 'hai-progressive-section__summary'
      trigger.setAttribute('aria-expanded', 'true')
      section.append(trigger)
      outlet.append(section)
      await afterNativeDisclosureUpdate()
      await afterNativeDisclosureUpdate()
      expect(document.activeElement).toBe(control)
      expect((component as any).deepLinkObserver).toBeUndefined()
    } finally {
      component.ngOnDestroy()
      outlet.remove()
      control.remove()
    }
  })

  it('opens a nested native detail target after its progressive parent is revealed', () => {
    spyOnProperty(document, 'hidden', 'get').and.returnValue(false)
    const component = createShell(() => of(status))
    ;(component as any).deepLinkedSectionId = 'workflow-approval-trail'

    const outlet = document.createElement('div')
    outlet.className = 'hai-module-outlet'
    const parent = document.createElement('hai-progressive-section')
    parent.id = 'record-audit-trails'
    const trigger = document.createElement('button')
    trigger.className = 'hai-progressive-section__summary'
    trigger.setAttribute('aria-expanded', 'false')
    trigger.addEventListener('click', () => {
      trigger.setAttribute('aria-expanded', 'true')
      const detail = document.createElement('details')
      detail.className = 'advanced-section'
      detail.dataset['haiSection'] = 'workflow-approval-trail'
      const summary = document.createElement('summary')
      summary.textContent = 'Workflow approval trail'
      detail.append(summary)
      const scrollIntoView = jasmine.createSpy('scrollIntoView')
      Object.defineProperty(detail, 'scrollIntoView', { configurable: true, value: scrollIntoView })
      parent.appendChild(detail)
      ;(component as any).nestedDetail = detail
      ;(component as any).nestedDetailScroll = scrollIntoView
    })
    parent.appendChild(trigger)
    outlet.appendChild(parent)
    document.body.appendChild(outlet)
    spyOn(window, 'setTimeout').and.callFake(((callback: TimerHandler) => {
      if (typeof callback === 'function') callback()
      return 1
    }) as typeof window.setTimeout)

    ;(component as any).openDeepLinkedDisclosurePath(
      ['record-audit-trails', 'workflow-approval-trail'],
      'workflow-approval-trail',
      0,
    )

    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect((component as any).nestedDetail.open).toBeTrue()
    expect((component as any).nestedDetailScroll).toHaveBeenCalledOnceWith({ block: 'start' })
    outlet.remove()
    component.ngOnDestroy()
  })

  it('keeps a Basic-only deep link in Basic mode while opening its disclosure', () => {
    const setMode = jasmine.createSpy('setMode')
    const setSection = jasmine.createSpy('setSection')
    const preferences = {
      get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
      setMode,
      setNavigationMode: jasmine.createSpy('setNavigationMode'),
      setSection,
    }
    const component = new AppShellComponent(
      { url: '/control-center' } as unknown as Router,
      preferences as unknown as ModuleViewPreferencesService,
      { mode: () => 'dark' } as unknown as ThemeService,
      { status: () => of(status) } as unknown as RuntimeControlService,
    )

    ;(component as any).updateCurrent('/plans#create-preview')

    expect(component.current.id).toBe('plan-coordination')
    expect(component.viewMode).toBe('basic')
    expect(setMode).not.toHaveBeenCalled()
    expect(setSection).toHaveBeenCalledOnceWith('plan-coordination', 'create-preview', true)
    component.ngOnDestroy()
  })
})

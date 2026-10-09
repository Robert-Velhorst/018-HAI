import { CommonModule } from '@angular/common'
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, HostListener, OnDestroy, OnInit, ViewChild, ViewEncapsulation } from '@angular/core'
import { NavigationEnd, Router, RouterModule, UrlTree } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { Subscription, timeout } from 'rxjs'
import { filter } from 'rxjs/operators'
import {
  HAI_MODULE_GROUPS,
  HAI_MODULES,
  HaiModuleDefinition,
  moduleForUrl,
  resolveProgressiveSection,
} from './module-registry'
import { HaiNavigationMode, HaiViewMode, ModuleViewPreferencesService } from './module-view-preferences.service'
import { ThemeMode, ThemeService } from '../services/theme.service'
import { RuntimeControlService } from '../services/runtime-control.service'
import { IBackgroundStatus } from '../models/runtime-control.model.interface'

type SafetyFreshness = 'unknown' | 'checking' | 'current' | 'stale'
const MOBILE_NAVIGATION_MAX_WIDTH = 820

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-shell',
    templateUrl: './app-shell.component.html',
    styleUrls: ['./app-shell.component.scss'],
    // The lazy shell owns shared styles for the authenticated route tree.
    // Child pages render in their own component scopes, so these selectors
    // must remain global once the shell is loaded.
    encapsulation: ViewEncapsulation.None,
    imports: [CommonModule, RouterModule, NzButtonModule, NzIconModule],
    standalone: true
})
export class AppShellComponent implements OnInit, OnDestroy {
  readonly groups = HAI_MODULE_GROUPS
  readonly modules = HAI_MODULES
  current: HaiModuleDefinition = HAI_MODULES[0]
  themeMode: ThemeMode = 'dark'
  viewMode: HaiViewMode = 'basic'
  navigationMode: HaiNavigationMode = 'auto'
  navigationGroupOpen: Record<string, boolean> = {}
  mobileNavigationOpen = false
  safetyFreshness: SafetyFreshness = 'unknown'
  safetyFreshnessText = 'Unknown'
  safetyRequestInFlight = false
  emergencyStopActive = false
  safetyLabel = 'Safety status unknown'
  safetyDetail = 'Runtime safeguards have not been confirmed.'
  viewResetNotice = ''
  viewResetSessionOnly = false
  private safetyStatus?: IBackgroundStatus
  private safetyStatusSubscription?: Subscription
  private safetyRefreshFailed = false
  private safetyLastConfirmedAt?: number
  private safetyLastAttemptAt?: number
  private safetyRefreshTimer?: number
  private readonly safetyRefreshIntervalMs = 60_000
  private readonly safetyRetryIntervalMs = 120_000
  private readonly safetyStaleAfterMs = 120_000
  private readonly onVisibilityChange = () => this.handleVisibilityChange()
  @ViewChild('mobileMenuButton') private mobileMenuButton?: ElementRef<HTMLButtonElement>
  @ViewChild('mobileNavigation') private mobileNavigation?: ElementRef<HTMLElement>
  private routerSubscription?: Subscription
  private themeSubscription?: Subscription
  private moduleModeSubscription?: Subscription
  private detailRestoreTimer?: number
  private deepLinkGeneration = 0
  private deepLinkObserver?: MutationObserver
  private deepLinkExpiryTimer?: number
  private readonly onDeepLinkInteraction = (event: Event) => {
    if (event.isTrusted) this.cancelDeepLinkRestore()
  }
  private readonly resetDisclosureEvents = new WeakMap<HTMLDetailsElement, boolean>()
  private readonly mobileNavigationFocusTimers = new Set<number>()
  private mobileNavigationFocusGeneration = 0
  private deepLinkedSectionId = ''
  private deepLinkedAncestorIds: string[] = []
  private readonly advancedDetailSelector = [
    'details[data-hai-advanced]',
    'details.advanced-section',
    'details.advanced-block',
    'details.detail-block',
    'details.provider-detail',
    'details.legacy-actions',
    'details.pursuit-health',
    'details.route-intake-panel',
  ].join(', ')
  private readonly onDetailsToggle = (event: Event) => this.persistDetailState(event)

  constructor(
    private router: Router,
    private preferences: ModuleViewPreferencesService,
    private themeService: ThemeService,
    private runtimeControl: RuntimeControlService,
    private readonly changeDetector?: ChangeDetectorRef,
  ) {}

  ngOnInit(): void {
    this.themeMode = this.themeService.mode()
    this.themeSubscription = this.themeService.changes$?.subscribe(mode => this.themeMode = mode)
    this.updateCurrent(this.router.url)
    document.addEventListener('visibilitychange', this.onVisibilityChange)
    if (!document.hidden && !this.isMinimalAccessRoute()) this.refreshSafetyStatus()
    this.routerSubscription = this.router.events.pipe(filter((event): event is NavigationEnd => event instanceof NavigationEnd))
      .subscribe((event) => this.updateCurrent(event.urlAfterRedirects, true))
    document.addEventListener('toggle', this.onDetailsToggle, true)
  }

  ngOnDestroy(): void {
    this.routerSubscription?.unsubscribe()
    this.themeSubscription?.unsubscribe()
    this.moduleModeSubscription?.unsubscribe()
    this.safetyStatusSubscription?.unsubscribe()
    this.clearSafetyRefreshTimer()
    document.removeEventListener('visibilitychange', this.onVisibilityChange)
    this.cancelDeepLinkRestore()
    this.cancelMobileNavigationFocusTimers()
    document.body.classList.remove('hai-view-advanced')
    document.removeEventListener('toggle', this.onDetailsToggle, true)
  }

  groupModules(groupId: string): HaiModuleDefinition[] {
    return this.modules.filter((module) => module.group === groupId && module.showInNavigation !== false)
  }

  isNavigationGroupOpen(groupId: string): boolean {
    return this.navigationGroupOpen[groupId] === true
  }

  groupContainsCurrentModule(groupId: string): boolean {
    return this.current.group === groupId
  }

  persistNavigationGroupState(groupId: string, event: Event): void {
    const details = event.target
    if (!(details instanceof HTMLDetailsElement)) return
    this.navigationGroupOpen = { ...this.navigationGroupOpen, [groupId]: details.open }
    this.preferences.setNavigationGroupOpen?.(groupId, details.open)
  }

  isMinimalAccessRoute(): boolean { return this.current.id === 'onboarding' }

  get mainContentHref(): string {
    return `${window.location.pathname}${window.location.search}#hai-main`
  }

  skipToContent(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    const main = document.getElementById('hai-main')
    if (main?.isConnected && !main.closest('[inert]')) main.focus()
  }

  navigate(module: HaiModuleDefinition): void {
    this.closeMobileNavigation()
    this.router.navigateByUrl(module.route)
  }

  navigateFromNavigation(module: HaiModuleDefinition, event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()

    const currentUrl = this.router.url
    const restorePageFocus = this.mobileNavigationOpen
    if (restorePageFocus) this.closeMobileNavigation(false)

    void this.router.navigateByUrl(module.route).then((navigated) => {
      const destinationReady = navigated || currentUrl === module.route
      if (!restorePageFocus && !destinationReady) return
      requestAnimationFrame(() => {
        const main = document.getElementById('hai-main')
        const target = destinationReady
          ? main
          : this.mobileMenuButton?.nativeElement ?? main
        if (target?.isConnected && !target.closest('[inert]')) target.focus()
      })
    }, () => {
      if (!restorePageFocus) return
      requestAnimationFrame(() => this.mobileMenuButton?.nativeElement.focus())
    })
  }

  openRoute(route: string): void {
    this.closeMobileNavigation()
    this.router.navigateByUrl(route)
  }

  openSafetyControls(): void {
    this.refreshSafetyStatus()
    this.openRoute('/runtime-control')
  }

  refreshSafetyStatus(): void {
    if (document.hidden || this.safetyRequestInFlight) return
    this.clearSafetyRefreshTimer()
    this.safetyRequestInFlight = true
    this.safetyLastAttemptAt = Date.now()
    this.updateSafetyFreshness()
    this.safetyStatusSubscription = this.runtimeControl.status().pipe(timeout(7000)).subscribe({
      next: status => {
        this.safetyRequestInFlight = false
        if (!this.isUsableSafetyStatus(status)) {
          this.noteUnconfirmedSafetyStatus(status)
          return
        }
        this.safetyStatus = status
        this.emergencyStopActive = status.mode === 'emergency_stopped' || status.emergencyStop.engaged
        this.safetyLastConfirmedAt = Date.now()
        this.safetyRefreshFailed = false
        this.updateSafetyFreshness()
        this.scheduleSafetyRefresh(this.safetyRefreshIntervalMs)
      },
      error: () => {
        this.safetyRequestInFlight = false
        this.noteUnconfirmedSafetyStatus()
      },
      complete: () => {
        if (this.safetyRequestInFlight) {
          this.safetyRequestInFlight = false
          this.noteUnconfirmedSafetyStatus()
        }
      },
    })
  }

  toggleMobileNavigation(): void {
    if (this.mobileNavigationOpen) {
      this.closeMobileNavigation()
      return
    }
    this.cancelMobileNavigationFocusTimers()
    this.mobileNavigationOpen = true
    // Focus must follow the rendered dialog/inert state, not a coalesced future tick.
    this.changeDetector?.detectChanges()
    this.scheduleMobileNavigationFocus(() => {
      if (this.mobileNavigationOpen) {
        this.focusMobileNavigationEntry()
      }
    })
  }

  closeMobileNavigation(restoreFocus = true): void {
    const wasOpen = this.mobileNavigationOpen
    this.mobileNavigationOpen = false
    this.cancelMobileNavigationFocusTimers()
    if (wasOpen) this.changeDetector?.detectChanges()
    if (wasOpen && restoreFocus) {
      this.scheduleMobileNavigationFocus(() => {
        const menuButton = this.mobileMenuButton?.nativeElement
        if (!this.mobileNavigationOpen && menuButton?.isConnected && !menuButton.closest('[inert]')) {
          menuButton.focus()
        }
      })
    }
  }

  @HostListener('window:resize')
  handleNavigationBreakpointResize(): void {
    const navigation = this.mobileNavigation?.nativeElement
    if (window.innerWidth <= MOBILE_NAVIGATION_MAX_WIDTH) {
      if (this.mobileNavigationOpen || !navigation?.contains(document.activeElement)) return

      const focusedNavigationControl = document.activeElement
      this.scheduleMobileNavigationFocus(() => {
        const activeElement = document.activeElement
        if (
          window.innerWidth <= MOBILE_NAVIGATION_MAX_WIDTH &&
          !this.mobileNavigationOpen &&
          (activeElement === focusedNavigationControl || activeElement === document.body)
        ) {
          this.mobileMenuButton?.nativeElement.focus()
        }
      })
      return
    }

    if (this.mobileNavigationOpen) {
      const closeButtonHasFocus = navigation?.querySelector('.hai-navigation__close') === document.activeElement
      this.closeMobileNavigation(false)

      if (!closeButtonHasFocus) return
      this.scheduleMobileNavigationFocus(() => {
        if (!this.mobileNavigationOpen && window.innerWidth > MOBILE_NAVIGATION_MAX_WIDTH) {
          this.focusDesktopNavigation(navigation)
        }
      })
      return
    }

    const menuButton = this.mobileMenuButton?.nativeElement
    if (!menuButton || document.activeElement !== menuButton) return

    this.scheduleMobileNavigationFocus(() => {
      const activeElement = document.activeElement
      if (
        window.innerWidth > MOBILE_NAVIGATION_MAX_WIDTH &&
        !this.mobileNavigationOpen &&
        (activeElement === menuButton || activeElement === document.body)
      ) {
        this.focusDesktopNavigation(navigation)
      }
    })
  }

  @HostListener('document:keydown.escape')
  closeMobileNavigationOnEscape(): void {
    if (this.mobileNavigationOpen) this.closeMobileNavigation()
  }

  @HostListener('document:keydown', ['$event'])
  keepFocusInMobileNavigation(event: KeyboardEvent): void {
    if (!this.mobileNavigationOpen || event.key !== 'Tab') return
    const navigation = this.mobileNavigation?.nativeElement
    if (!navigation) return
    const focusable = Array.from(navigation.querySelectorAll<HTMLElement>('summary, button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'))
      .filter((control) => {
        const closedGroup = control.parentElement?.closest('details:not([open])')
        return !closedGroup || closedGroup.querySelector(':scope > summary') === control
      })
    if (!focusable.length) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && (document.activeElement === first || !navigation.contains(document.activeElement))) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !navigation.contains(document.activeElement))) {
      event.preventDefault()
      first.focus()
    }
  }

  isActive(module: HaiModuleDefinition): boolean { return module.id === this.current.id }

  get safetyStatusUnknown(): boolean {
    return this.safetyFreshness !== 'current' && !this.emergencyStopActive
  }

  get safetyCompactLabel(): string {
    if (this.emergencyStopActive) return 'Stop active'
    if (this.safetyFreshness === 'checking') return 'Checking safety'
    if (this.safetyFreshness === 'stale') return 'Safety stale'
    if (this.safetyFreshness !== 'current' || !this.safetyStatus) return 'Safety unknown'

    const labels: Record<string, string> = {
      paused: 'Paused',
      read_only: 'Read-only',
      draft_only: 'Draft-only',
      approval_required: 'Approval required',
      autonomous_safe: 'Autonomous-safe',
      emergency_stopped: 'Stop active',
    }
    return labels[this.safetyStatus.mode] ?? 'Safety unknown'
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle()
  }

  toggleViewMode(): void {
    this.viewMode = this.viewMode === 'basic' ? 'advanced' : 'basic'
    this.preferences.setMode(this.current.id, this.viewMode)
    document.body.classList.toggle('hai-view-advanced', this.viewMode === 'advanced')
    this.viewResetNotice = ''
    this.viewResetSessionOnly = false
    if (this.viewMode === 'basic') this.clearAdvancedDeepLinkFromUrl()
  }

  resetCurrentModuleViewFromCompactMenu(menu: HTMLDetailsElement): void {
    this.resetCurrentModuleView()
    menu.open = false
    requestAnimationFrame(() => menu.querySelector<HTMLElement>(':scope > summary')?.focus())
  }

  private clearAdvancedDeepLinkFromUrl(includeBasicFragment = false): void {
    let tree: UrlTree
    try {
      tree = this.router.parseUrl(this.router.url)
    } catch {
      return
    }

    const queryParams = { ...tree.queryParams }
    const hasAdvancedOverride = queryParams['mode'] === 'advanced'
    const linkedSection = tree.fragment ? resolveProgressiveSection(this.current, tree.fragment) : undefined
    const clearFragment = !!linkedSection && (includeBasicFragment || linkedSection.requiredMode === 'advanced')
    if (!hasAdvancedOverride && !clearFragment) return

    if (hasAdvancedOverride) delete queryParams['mode']
    tree.queryParams = queryParams
    if (clearFragment) {
      tree.fragment = null
      this.deepLinkedSectionId = ''
      this.deepLinkedAncestorIds = []
      this.cancelDeepLinkRestore()
    }

    void this.router.navigateByUrl(tree, { replaceUrl: true }).catch(() => undefined)
  }

  cycleNavigationMode(): void {
    const next: Record<HaiNavigationMode, HaiNavigationMode> = { auto: 'compact', compact: 'expanded', expanded: 'auto' }
    this.navigationMode = next[this.navigationMode]
    this.preferences.setNavigationMode?.(this.navigationMode)
  }

  navigationLabel(): string {
    return this.navigationMode === 'auto' ? 'Desktop rail: adaptive' : `Desktop rail: ${this.navigationMode}`
  }

  resetCurrentModuleView(): void {
    const moduleId = this.current.id
    this.clearAdvancedDeepLinkFromUrl(true)
    this.cancelDeepLinkRestore()

    const outlet = document.querySelector<HTMLElement>('.hai-module-outlet')
    outlet?.querySelectorAll<HTMLButtonElement>('hai-progressive-section .hai-progressive-section__summary[aria-expanded="true"]')
      .forEach((trigger) => trigger.click())
    outlet?.querySelectorAll<HTMLDetailsElement>(this.advancedDetailSelector)
      .forEach((detail) => {
        if (!detail.open) return
        this.resetDisclosureEvents.set(detail, false)
        detail.open = false
      })

    const defaults = this.preferences.reset(moduleId)
    this.viewMode = defaults.mode
    this.deepLinkedSectionId = ''
    this.deepLinkedAncestorIds = []
    document.body.classList.toggle('hai-view-advanced', this.viewMode === 'advanced')
    this.viewResetSessionOnly = this.preferences.isSessionOnly(moduleId)
    this.viewResetNotice = this.viewResetSessionOnly
      ? 'This view reset applies only for this session because browser storage is unavailable. The previously saved view may return after reload.'
      : `View settings reset for ${this.current.title}. Other modules and the shared desktop rail setting were not changed.`
  }

  private updateCurrent(url: string, focusAfterModuleChange = false): void {
    const deepLinkGeneration = ++this.deepLinkGeneration
    this.stopDeepLinkRestore()
    const previousModule = this.current.id
    if (this.mobileNavigationOpen) this.closeMobileNavigation(!(focusAfterModuleChange && moduleForUrl(url).id !== previousModule))
    this.current = moduleForUrl(url)
    const moduleChanged = previousModule !== this.current.id
    if (moduleChanged || !this.moduleModeSubscription) {
      this.moduleModeSubscription?.unsubscribe()
      const modeChanges = this.preferences.watchMode?.(this.current.id)
      this.moduleModeSubscription = modeChanges?.subscribe((mode) => {
        this.viewMode = mode
        document.body.classList.toggle('hai-view-advanced', mode === 'advanced')
      })
    }
    this.restoreNavigationGroupState()
    if (previousModule === 'onboarding' && !this.isMinimalAccessRoute()) this.refreshSafetyStatus()
    if (moduleChanged) {
      this.viewResetNotice = ''
      this.viewResetSessionOnly = false
    }
    const [urlWithoutHash, rawFragment = ''] = url.split('#', 2)
    const query = new URLSearchParams(urlWithoutHash.split('?').slice(1).join('?'))
    let fragment = ''
    try { fragment = decodeURIComponent(rawFragment) } catch { fragment = '' }
    const deepLink = resolveProgressiveSection(this.current, fragment)
    this.deepLinkedSectionId = deepLink?.sectionId ?? ''
    this.deepLinkedAncestorIds = deepLink?.ancestorIds ?? []
    const forceAdvanced = query.get('mode') === 'advanced' || deepLink?.requiredMode === 'advanced'
    if (forceAdvanced) this.preferences.setMode(this.current.id, 'advanced')
    if (deepLink) {
      for (const sectionId of [...deepLink.ancestorIds, deepLink.sectionId]) {
        this.preferences.setSection(this.current.id, sectionId, true)
      }
    }
    if (previousModule === 'runtime-control' && this.current.id !== 'runtime-control') this.refreshSafetyStatus()
    const preferences = this.preferences.get(this.current.id)
    this.viewMode = forceAdvanced ? 'advanced' : preferences.mode
    this.navigationMode = this.preferences.getNavigationMode?.(this.current.id) ?? preferences.navigationMode ?? 'auto'
    document.body.classList.toggle('hai-view-advanced', this.viewMode === 'advanced')
    if (focusAfterModuleChange && moduleChanged && !deepLink) {
      window.requestAnimationFrame(() => {
        if (deepLinkGeneration !== this.deepLinkGeneration) return
        const main = document.getElementById('hai-main')
        if (main?.isConnected && !main.closest('[inert]')) main.focus()
      })
    }
    if (deepLink) this.watchDeepLinkRender(deepLinkGeneration)
    this.detailRestoreTimer = window.setTimeout(() => {
      this.detailRestoreTimer = undefined
      if (deepLinkGeneration !== this.deepLinkGeneration) return
      this.restoreDetailState()
      if (this.deepLinkedSectionId) {
        this.openDeepLinkedDisclosurePath(
          [...this.deepLinkedAncestorIds, this.deepLinkedSectionId],
          this.deepLinkedSectionId,
          0,
          deepLinkGeneration,
        )
      }
    })
  }

  private updateSafetyLabel(): void {
    const status = this.safetyStatus
    if (this.safetyFreshness === 'stale') {
      this.safetyLabel = 'Safety status stale'
      const lastKnown = status ? this.describeSafetyStatus(status) : 'an emergency stop was reported active'
      this.safetyDetail = this.emergencyStopActive
        ? `Last confirmed ${lastKnown}; an emergency stop was reported active but its current state is unconfirmed.`
        : `Last confirmed ${lastKnown}; the current runtime safeguards could not be confirmed.`
      return
    }
    if (this.emergencyStopActive) {
      this.safetyLabel = 'Emergency stop active'
      this.safetyDetail = this.safetyFreshness === 'current'
        ? 'New execution is blocked until the stop is safely released.'
        : 'An emergency stop was reported active; the runtime mode is unconfirmed. Execution remains blocked.'
      return
    }
    if (this.safetyFreshness === 'checking' && !this.safetyLastConfirmedAt) {
      this.safetyLabel = 'Checking safety status'
      this.safetyDetail = 'Loading the current runtime safeguards.'
      return
    }
    if (this.safetyFreshness === 'unknown' || !status) {
      this.safetyLabel = 'Safety status unknown'
      this.safetyDetail = 'Runtime safeguards could not be confirmed. Open runtime controls.'
      return
    }
    if (this.safetyFreshness !== 'current' || !status) {
      this.safetyLabel = 'Safety status unknown'
      this.safetyDetail = 'Runtime safeguards could not be confirmed. Open runtime controls.'
      return
    }
    const labels: Record<string, [string, string]> = {
      paused: ['Background work paused', 'No background operations should run.'],
      read_only: ['Read-only mode', 'HAI may observe and summarize, not draft or execute.'],
      draft_only: ['Draft-only mode', 'Internal drafts are allowed; external execution is blocked.'],
      approval_required: ['Approval-required mode', 'Work beyond safe internal actions waits for your approval.'],
      autonomous_safe: ['Autonomous-safe mode', 'Only low-risk reversible work runs automatically; higher-risk actions need approval.'],
    }
    const [label, detail] = labels[status.mode] || ['Safety status unknown', 'Runtime safeguards could not be confirmed.']
    this.safetyLabel = label
    this.safetyDetail = detail
  }

  private isUsableSafetyStatus(status: IBackgroundStatus | null | undefined): status is IBackgroundStatus {
    const knownModes = ['paused', 'read_only', 'draft_only', 'approval_required', 'autonomous_safe', 'emergency_stopped']
    return !!status
      && typeof status.mode === 'string'
      && knownModes.includes(status.mode)
      && typeof status.emergencyStop?.engaged === 'boolean'
      && !status.modeStateError
  }

  private noteUnconfirmedSafetyStatus(status?: IBackgroundStatus): void {
    if (status?.emergencyStop?.engaged === true || status?.mode === 'emergency_stopped') {
      this.emergencyStopActive = true
    }
    this.safetyRefreshFailed = true
    this.updateSafetyFreshness()
    this.scheduleSafetyRefresh(this.safetyRetryIntervalMs)
  }

  private updateSafetyFreshness(now = Date.now()): void {
    if (!this.safetyLastConfirmedAt) {
      this.safetyFreshness = this.safetyRequestInFlight ? 'checking' : 'unknown'
      this.safetyFreshnessText = this.safetyRequestInFlight ? 'Checking…' : 'Unknown'
    } else {
      const isStale = this.safetyRefreshFailed || now - this.safetyLastConfirmedAt >= this.safetyStaleAfterMs
      this.safetyFreshness = isStale ? 'stale' : 'current'
      const checkedAt = new Date(this.safetyLastConfirmedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      this.safetyFreshnessText = isStale ? `Stale · ${checkedAt}` : `Current · ${checkedAt}`
    }
    this.updateSafetyLabel()
  }

  private describeSafetyStatus(status: IBackgroundStatus): string {
    if (status.mode === 'emergency_stopped' || status.emergencyStop?.engaged) return 'an active emergency stop'
    const descriptions: Record<string, string> = {
      paused: 'background work paused',
      read_only: 'read-only mode',
      draft_only: 'draft-only mode',
      approval_required: 'approval-required mode',
      autonomous_safe: 'autonomous-safe mode',
    }
    return descriptions[status.mode] ?? 'a previously confirmed state'
  }

  private handleVisibilityChange(): void {
    if (!document.hidden && this.deepLinkObserver) this.scheduleDeepLinkRestore(this.deepLinkGeneration)
    if (this.isMinimalAccessRoute()) return
    if (document.hidden) {
      this.clearSafetyRefreshTimer()
      this.updateSafetyFreshness()
      return
    }
    this.updateSafetyFreshness()
    if (this.safetyRequestInFlight) return
    const interval = this.safetyRefreshFailed || !this.safetyLastConfirmedAt
      ? this.safetyRetryIntervalMs
      : this.safetyRefreshIntervalMs
    const lastAttempt = this.safetyLastAttemptAt ?? 0
    const delay = Math.max(0, interval - (Date.now() - lastAttempt))
    if (delay === 0) this.refreshSafetyStatus()
    else this.scheduleSafetyRefresh(delay)
  }

  private scheduleSafetyRefresh(delay: number): void {
    this.clearSafetyRefreshTimer()
    if (document.hidden) return
    this.safetyRefreshTimer = window.setTimeout(() => {
      this.safetyRefreshTimer = undefined
      this.updateSafetyFreshness()
      this.refreshSafetyStatus()
    }, delay)
  }

  private clearSafetyRefreshTimer(): void {
    if (this.safetyRefreshTimer === undefined) return
    window.clearTimeout(this.safetyRefreshTimer)
    this.safetyRefreshTimer = undefined
  }

  private scheduleMobileNavigationFocus(callback: () => void): void {
    const generation = this.mobileNavigationFocusGeneration
    let timer: number
    timer = window.setTimeout(() => {
      this.mobileNavigationFocusTimers.delete(timer)
      if (generation === this.mobileNavigationFocusGeneration) callback()
    }, 0)
    this.mobileNavigationFocusTimers.add(timer)
  }

  private cancelMobileNavigationFocusTimers(): void {
    this.mobileNavigationFocusGeneration += 1
    for (const timer of this.mobileNavigationFocusTimers) window.clearTimeout(timer)
    this.mobileNavigationFocusTimers.clear()
  }

  private focusDesktopNavigation(navigation?: HTMLElement): void {
    const currentGroup = navigation?.querySelector<HTMLElement>('.hai-navigation__group--current .hai-navigation__summary')
    const brand = navigation?.querySelector<HTMLElement>('.hai-brand')
    const target = currentGroup ?? brand
    if (target?.isConnected) target.focus()
  }

  private focusMobileNavigationEntry(): void {
    const navigation = this.mobileNavigation?.nativeElement
    if (!navigation) return

    const currentGroup = navigation.querySelector<HTMLElement>('.hai-navigation__group--current')
    const disclosure = currentGroup?.querySelector<HTMLDetailsElement>(':scope > details')
    const currentSummary = disclosure?.querySelector<HTMLElement>(':scope > summary')
    const activeLink = currentGroup?.querySelector<HTMLElement>('.hai-navigation__item--active')
    const target = disclosure?.open && activeLink ? activeLink : currentSummary
    const fallback = navigation.querySelector<HTMLElement>('summary, a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])')
    ;(target ?? fallback)?.focus()
  }

  private persistDetailState(event: Event): void {
    const detail = event.target as HTMLDetailsElement
    if (!(detail instanceof HTMLDetailsElement) || !detail.matches(this.advancedDetailSelector)) return
    const outlet = document.querySelector<HTMLElement>('.hai-module-outlet')
    if (!detail.isConnected || !outlet?.contains(detail)) return

    if (this.resetDisclosureEvents.has(detail)) {
      const expectedOpen = this.resetDisclosureEvents.get(detail)
      this.resetDisclosureEvents.delete(detail)
      if (detail.open === expectedOpen) return
    }

    const sectionId = this.sectionId(detail)
    this.preferences.setSection(this.current.id, sectionId, detail.open)
  }

  private restoreDetailState(): void {
    Array.from(document.querySelectorAll<HTMLDetailsElement>('.hai-module-outlet details'))
      .filter((detail) => detail.matches(this.advancedDetailSelector))
      .forEach((detail) => {
      const id = this.sectionId(detail)
      detail.open = id === this.deepLinkedSectionId || this.preferences.get(this.current.id).openSections[id] === true
      })
  }

  private restoreNavigationGroupState(): void {
    const saved = this.preferences.getNavigationGroups?.().openGroups ?? {}
    this.navigationGroupOpen = Object.fromEntries(this.groups.map((group) => [
      group.id,
      Object.prototype.hasOwnProperty.call(saved, group.id) ? saved[group.id] === true : group.id === this.current.group,
    ]))
  }

  private scrollToDeepLinkedSection(sectionId: string): void {
    this.findDeepLinkedElement(sectionId)?.scrollIntoView({ block: 'start' })
  }

  private watchDeepLinkRender(generation: number): void {
    this.deepLinkObserver = new MutationObserver(() => this.scheduleDeepLinkRestore(generation))
    // The outlet can itself be arriving with the lazy route; observe only while awaiting this link.
    this.deepLinkObserver.observe(document.body, {
      childList: true, subtree: true, attributes: true, attributeFilter: ['aria-expanded', 'hidden', 'inert', 'class', 'style'],
    })
    for (const type of ['pointerdown', 'keydown', 'wheel']) document.addEventListener(type, this.onDeepLinkInteraction, true)
    this.deepLinkExpiryTimer = window.setTimeout(() => this.cancelDeepLinkRestore(), 30_000)
  }

  private scheduleDeepLinkRestore(generation: number): void {
    if (generation !== this.deepLinkGeneration || !this.deepLinkObserver || this.detailRestoreTimer !== undefined) return
    this.detailRestoreTimer = window.setTimeout(() => {
      this.detailRestoreTimer = undefined
      this.openDeepLinkedDisclosurePath(
        [...this.deepLinkedAncestorIds, this.deepLinkedSectionId], this.deepLinkedSectionId, 0, generation,
      )
    })
  }

  private cancelDeepLinkRestore(): void {
    ++this.deepLinkGeneration
    this.stopDeepLinkRestore()
  }

  private stopDeepLinkRestore(): void {
    this.deepLinkObserver?.disconnect()
    this.deepLinkObserver = undefined
    if (this.deepLinkExpiryTimer !== undefined) window.clearTimeout(this.deepLinkExpiryTimer)
    this.deepLinkExpiryTimer = undefined
    if (this.detailRestoreTimer !== undefined) window.clearTimeout(this.detailRestoreTimer)
    this.detailRestoreTimer = undefined
    for (const type of ['pointerdown', 'keydown', 'wheel']) document.removeEventListener(type, this.onDeepLinkInteraction, true)
  }

  private openDeepLinkedDisclosurePath(
    path: string[],
    targetId: string,
    index: number,
    generation = this.deepLinkGeneration,
  ): void {
    if (generation !== this.deepLinkGeneration || this.deepLinkedSectionId !== targetId || index >= path.length) return

    const section = this.findDeepLinkedElement(path[index])
    if (!section) return
    if (section instanceof HTMLDetailsElement) {
      if (!section.querySelector(':scope > summary')) return
      section.open = true
    } else {
      const trigger = section?.querySelector<HTMLButtonElement>('.hai-progressive-section__summary')
      if (section.tagName === 'HAI-PROGRESSIVE-SECTION' && !trigger) return
      if (trigger?.getAttribute('aria-expanded') !== 'true') trigger?.click()
      if (trigger && trigger.getAttribute('aria-expanded') !== 'true') return
    }

    if (index + 1 < path.length) {
      this.detailRestoreTimer = window.setTimeout(() => {
        this.detailRestoreTimer = undefined
        this.openDeepLinkedDisclosurePath(path, targetId, index + 1, generation)
      })
      return
    }

    if (this.focusDeepLinkedSection(section)) {
      this.scrollToDeepLinkedSection(targetId)
      this.stopDeepLinkRestore()
    }
  }

  private focusDeepLinkedSection(section: HTMLElement): boolean {
    const headingOrTrigger = section instanceof HTMLDetailsElement
      ? section.querySelector<HTMLElement>(':scope > summary')
      : section.querySelector<HTMLElement>(
        '.hai-progressive-section__summary, :scope > summary, [data-hai-section-heading], h1, h2, h3, h4, h5, h6',
      )
    const target = headingOrTrigger ?? section
    if (document.hidden || !target.isConnected || target.closest('[inert], [hidden]') || target.matches(':disabled') || !target.getClientRects().length) return false
    if (target === section || /^H[1-6]$/.test(target.tagName)) target.tabIndex = -1
    target.focus({ preventScroll: true })
    return document.activeElement === target
  }

  private findDeepLinkedElement(sectionId: string): HTMLElement | null {
    const outlet = document.querySelector<HTMLElement>('.hai-module-outlet')
    if (!outlet) return null

    const byId = document.getElementById(sectionId)
    if (byId && outlet.contains(byId)) return byId

    return Array.from(outlet.querySelectorAll<HTMLElement>('[data-hai-section]'))
      .find((element) => element.dataset['haiSection'] === sectionId) ?? null
  }

  private sectionId(detail: HTMLDetailsElement): string {
    if (detail.dataset['haiSection']) return detail.dataset['haiSection']
    const summary = detail.querySelector('summary')?.textContent?.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'detail'
    const siblings = Array.from(document.querySelectorAll<HTMLDetailsElement>('.hai-module-outlet details'))
      .filter((candidate) => candidate.matches(this.advancedDetailSelector))
    const generated = detail.id || `${summary}-${siblings.indexOf(detail)}`
    detail.dataset['haiSection'] = generated
    return generated
  }
}

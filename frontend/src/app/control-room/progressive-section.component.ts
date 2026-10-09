import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, EventEmitter, Input, OnChanges, OnDestroy, Output, SimpleChanges } from '@angular/core'
import { HAI_MODULES, isAdvancedSectionRegistered, resolveProgressiveSection } from './module-registry'
import { ModuleViewPreferencesService } from './module-view-preferences.service'
import { Subscription } from 'rxjs'

let nextDisclosureId = 0

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
  selector: 'hai-progressive-section',
  templateUrl: './progressive-section.component.html',
  standalone: false,
})
export class HaiProgressiveSectionComponent implements OnChanges, OnDestroy {
  @Input() moduleId = ''
  @Input() sectionId = ''
  @Input() title = ''
  @Input() summary = ''
  @Input() advancedOnly = true
  @Output() openChange = new EventEmitter<boolean>()

  readonly triggerId: string
  readonly panelId: string
  open = false
  viewMode: 'basic' | 'advanced' = 'basic'
  private lastEffectiveOpen: boolean | null = null
  private modeSubscription?: Subscription
  private sectionSubscription?: Subscription

  get visible(): boolean {
    return !this.advancedOnly || this.viewMode === 'advanced'
  }

  get effectiveOpen(): boolean {
    return this.visible && this.open
  }

  constructor(
    private preferences: ModuleViewPreferencesService,
    private host: ElementRef<HTMLElement>,
    private changeDetector: ChangeDetectorRef,
  ) {
    const id = ++nextDisclosureId
    this.triggerId = `hai-progressive-section-trigger-${id}`
    this.panelId = `hai-progressive-section-panel-${id}`
  }

  ngOnChanges(changes: SimpleChanges): void {
    if (!changes['moduleId'] && !changes['sectionId']) {
      if (changes['advancedOnly']) this.publishEffectiveOpen()
      return
    }

    const wasOpen = this.open
    this.modeSubscription?.unsubscribe()
    this.modeSubscription = undefined
    this.sectionSubscription?.unsubscribe()
    this.sectionSubscription = undefined
    if (!this.hasPreferenceScope()) {
      this.viewMode = 'basic'
      this.open = false
      if (wasOpen || this.lastEffectiveOpen !== null) this.publishEffectiveOpen()
      return
    }

    const module = HAI_MODULES.find((candidate) => candidate.id === this.moduleId)
    const resolution = module ? resolveProgressiveSection(module, this.currentFragment()) : undefined
    const deepLinked = this.advancedOnly
      && resolution?.sectionId === this.sectionId
      && isAdvancedSectionRegistered(module!, this.sectionId)
    if (deepLinked) {
      this.preferences.setMode(this.moduleId, 'advanced')
      this.preferences.setSection(this.moduleId, this.sectionId, true)
      document.body.classList.add('hai-view-advanced')
    }
    this.viewMode = this.preferences.get(this.moduleId).mode
    this.open = deepLinked || this.preferences.get(this.moduleId).openSections[this.sectionId] === true
    if (this.effectiveOpen) this.lastEffectiveOpen = null
    this.publishEffectiveOpen()
    this.watchModuleMode()
    this.watchSectionOpen()
    // AppShell owns deferred deep-link focus/scroll and cancels it on user interaction.
  }

  ngOnDestroy(): void {
    this.modeSubscription?.unsubscribe()
    this.sectionSubscription?.unsubscribe()
  }

  toggle(): void {
    this.setOpen(!this.open)
  }

  setOpen(open: boolean): void {
    if (this.open === open) return
    this.open = open
    this.preferences.setSection(this.moduleId, this.sectionId, open)
    this.publishEffectiveOpen()
  }

  private publishEffectiveOpen(): void {
    const effectiveOpen = this.effectiveOpen
    if (!effectiveOpen) this.restoreFocusBeforeHiding()
    if (this.lastEffectiveOpen === null) {
      this.lastEffectiveOpen = effectiveOpen
      if (effectiveOpen) this.openChange.emit(true)
      return
    }
    if (effectiveOpen === this.lastEffectiveOpen) return
    this.lastEffectiveOpen = effectiveOpen
    this.openChange.emit(effectiveOpen)
  }

  private restoreFocusBeforeHiding(): void {
    const host = this.host.nativeElement
    const active = host.ownerDocument.activeElement
    if (!active || !host.contains(active)) return
    const target = this.visible
      ? host.querySelector<HTMLElement>('.hai-progressive-section__summary')
      : host.closest<HTMLElement>('#hai-main')
    if (target?.isConnected && !target.closest('[inert]')) target.focus()
  }

  private watchModuleMode(): void {
    this.modeSubscription?.unsubscribe()
    this.modeSubscription = this.preferences.watchMode(this.moduleId).subscribe((mode) => {
      this.viewMode = mode
      this.publishEffectiveOpen()
      this.changeDetector.markForCheck()
    })
  }

  private watchSectionOpen(): void {
    this.sectionSubscription?.unsubscribe()
    this.sectionSubscription = this.preferences.watchSection(this.moduleId, this.sectionId).subscribe((open) => {
      if (this.open === open) return
      this.open = open
      this.publishEffectiveOpen()
      this.changeDetector.markForCheck()
    })
  }

  private hasPreferenceScope(): boolean {
    return typeof this.moduleId === 'string'
      && this.moduleId.trim().length > 0
      && typeof this.sectionId === 'string'
      && this.sectionId.trim().length > 0
  }

  private currentFragment(): string {
    try {
      return decodeURIComponent(window.location.hash.slice(1))
    } catch {
      return ''
    }
  }
}

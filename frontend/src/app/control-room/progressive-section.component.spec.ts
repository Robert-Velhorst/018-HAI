import { CommonModule } from '@angular/common'
import { Component, ViewChild } from '@angular/core'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { HaiProgressiveSectionComponent } from './progressive-section.component'
import { ModuleViewPreferencesService } from './module-view-preferences.service'

@Component({
  template: `
    <hai-progressive-section
      [moduleId]="moduleId"
      [sectionId]="sectionId"
      [title]="title"
      [summary]="summary"
      [advancedOnly]="advancedOnly"
    >
      <p class="projected-content">Detailed records</p>
      <button class="projected-action" type="button">Inspect record</button>
    </hai-progressive-section>
  `,
  standalone: false,
})
class ProgressiveSectionHostComponent {
  @ViewChild(HaiProgressiveSectionComponent) section!: HaiProgressiveSectionComponent
  moduleId = 'progressive-section-test'
  sectionId = 'members'
  title = 'Members'
  summary = 'Review member details.'
  advancedOnly = false
}

describe('HaiProgressiveSectionComponent', () => {
  const moduleIds = ['progressive-section-test', 'progressive-section-other', 'hai-os']

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [HaiProgressiveSectionComponent, ProgressiveSectionHostComponent],
      imports: [CommonModule, NzIconModule],
    }).compileComponents()

    const preferences = TestBed.inject(ModuleViewPreferencesService)
    for (const moduleId of moduleIds) preferences.reset(moduleId)
  })

  afterEach(() => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    for (const moduleId of moduleIds) preferences.reset(moduleId)
  })

  function createHost(): ComponentFixture<ProgressiveSectionHostComponent> {
    const fixture = TestBed.createComponent(ProgressiveSectionHostComponent)
    fixture.detectChanges()
    return fixture
  }

  function createSectionFixture(moduleId = 'progressive-section-test', sectionId = 'members', advancedOnly = false):
    ComponentFixture<HaiProgressiveSectionComponent> {
    const fixture = TestBed.createComponent(HaiProgressiveSectionComponent)
    fixture.componentRef.setInput('moduleId', moduleId)
    fixture.componentRef.setInput('sectionId', sectionId)
    fixture.componentRef.setInput('title', 'Members')
    fixture.componentRef.setInput('advancedOnly', advancedOnly)
    fixture.detectChanges()
    return fixture
  }

  it('restores its own saved state and exposes a valid accessible disclosure relationship', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setSection('progressive-section-test', 'members', true)

    const fixture = createHost()
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement
    const section = fixture.componentInstance.section
    const panel = fixture.nativeElement.querySelector(`#${section.panelId}`) as HTMLElement

    expect(button.getAttribute('aria-expanded')).toBe('true')
    expect(button.getAttribute('aria-controls')).toBe(section.panelId)
    expect(panel.getAttribute('role')).toBe('region')
    expect(panel.getAttribute('aria-labelledby')).toBe(section.triggerId)
    expect(panel.textContent).toContain('Detailed records')

    const changes: boolean[] = []
    section.openChange.subscribe((open) => changes.push(open))
    button.click()
    fixture.detectChanges()

    expect(button.getAttribute('aria-expanded')).toBe('false')
    expect(button.hasAttribute('aria-controls')).toBeFalse()
    expect(fixture.nativeElement.querySelector(`#${section.panelId}`)).toBeNull()
    expect(preferences.get('progressive-section-test').openSections['members']).toBeFalse()
    expect(changes).toEqual([false])
  })

  it('announces a saved-open section so lazy module content can load after restoration', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setSection('progressive-section-test', 'members', true)
    const fixture = TestBed.createComponent(HaiProgressiveSectionComponent)
    fixture.componentRef.setInput('moduleId', 'progressive-section-test')
    fixture.componentRef.setInput('sectionId', 'members')
    fixture.componentRef.setInput('advancedOnly', false)
    const changes: boolean[] = []
    fixture.componentInstance.openChange.subscribe((open) => changes.push(open))

    fixture.detectChanges()

    expect(fixture.componentInstance.open).toBeTrue()
    expect(changes).toEqual([true])
    fixture.destroy()
  })

  it('hides saved advanced detail in Basic mode and restores it without losing its preference', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setMode('hai-os', 'advanced')
    preferences.setSection('hai-os', 'product-stack', true)

    const fixture = createSectionFixture('hai-os', 'product-stack', true)
    const section = fixture.componentInstance
    const changes: boolean[] = []
    section.openChange.subscribe((open) => changes.push(open))

    expect(section.visible).toBeTrue()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section__content')).not.toBeNull()

    preferences.setMode('hai-os', 'basic')
    fixture.detectChanges()

    expect(section.visible).toBeFalse()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section')).toBeNull()
    expect(preferences.get('hai-os').openSections['product-stack']).toBeTrue()

    preferences.setMode('hai-os', 'advanced')
    fixture.detectChanges()

    expect(section.visible).toBeTrue()
    expect(section.open).toBeTrue()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section__content')).not.toBeNull()

    preferences.reset('hai-os')
    fixture.detectChanges()

    expect(section.visible).toBeFalse()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section')).toBeNull()
    expect(preferences.get('hai-os').openSections).toEqual({})
    expect(changes).toEqual([false, true, false])
    fixture.destroy()
  })

  it('opens a hidden advanced section when its preference changes programmatically', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    const fixture = createSectionFixture('hai-os', 'product-stack', true)
    const section = fixture.componentInstance
    const changes: boolean[] = []
    section.openChange.subscribe((open) => changes.push(open))

    preferences.setSection('hai-os', 'product-stack', true)
    fixture.detectChanges()
    expect(section.open).toBeTrue()
    expect(section.visible).toBeFalse()
    expect(changes).toEqual([])

    preferences.setMode('hai-os', 'advanced')
    fixture.detectChanges()
    expect(section.visible).toBeTrue()
    expect(fixture.nativeElement.querySelector('.hai-progressive-section__content')).not.toBeNull()
    expect(changes).toEqual([true])
    fixture.destroy()
  })

  it('reloads disclosure state only when its module or section scope changes', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setSection('progressive-section-test', 'members', true)
    preferences.setSection('progressive-section-other', 'records', true)

    const directFixture = createSectionFixture('progressive-section-test', 'members')
    const directSection = directFixture.componentInstance
    expect(directSection.open).toBeTrue()

    directFixture.componentRef.setInput('title', 'Updated title')
    directFixture.detectChanges()
    expect(directSection.open).toBeTrue()

    directFixture.componentRef.setInput('moduleId', 'progressive-section-other')
    directFixture.componentRef.setInput('sectionId', 'records')
    directFixture.detectChanges()
    expect(directSection.open).toBeTrue()

    const changes: boolean[] = []
    directSection.openChange.subscribe((open) => changes.push(open))
    directFixture.componentRef.setInput('sectionId', 'closed')
    directFixture.detectChanges()
    expect(directSection.open).toBeFalse()
    expect(changes).toEqual([false])

  })

  it('announces a saved-open disclosure again when a reused component changes scope', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setSection('progressive-section-test', 'members', true)
    preferences.setSection('progressive-section-other', 'records', true)
    const fixture = createSectionFixture()
    const changes: boolean[] = []
    fixture.componentInstance.openChange.subscribe((open) => changes.push(open))

    fixture.componentRef.setInput('moduleId', 'progressive-section-other')
    fixture.componentRef.setInput('sectionId', 'records')
    fixture.detectChanges()

    expect(changes).toEqual([true])
    preferences.setSection('progressive-section-test', 'members', false)
    expect(fixture.componentInstance.open).toBeTrue()
    fixture.componentRef.setInput('title', 'Renamed records')
    fixture.detectChanges()
    expect(changes).toEqual([true])
    fixture.destroy()
  })

  it('returns focus to the disclosure trigger before a focused panel is removed', () => {
    TestBed.inject(ModuleViewPreferencesService).setSection('progressive-section-test', 'members', true)
    const fixture = createHost()
    const root = fixture.nativeElement as HTMLElement
    document.body.appendChild(root)
    try {
      const action = root.querySelector<HTMLButtonElement>('.projected-action')!
      const trigger = root.querySelector<HTMLButtonElement>('.hai-progressive-section__summary')!
      action.focus()

      TestBed.inject(ModuleViewPreferencesService).setSection('progressive-section-test', 'members', false)
      fixture.detectChanges()

      expect(root.querySelector('.hai-progressive-section__content')).toBeNull()
      expect(document.activeElement).toBe(trigger)
    } finally {
      fixture.destroy()
      root.remove()
    }
  })

  it('returns focus to main content when Basic mode hides a focused advanced disclosure', () => {
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    preferences.setMode('progressive-section-test', 'advanced')
    preferences.setSection('progressive-section-test', 'members', true)
    const fixture = createHost()
    fixture.componentInstance.advancedOnly = true
    fixture.changeDetectorRef.markForCheck()
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const main = document.createElement('main')
    main.id = 'hai-main'
    main.tabIndex = -1
    main.appendChild(root)
    document.body.appendChild(main)
    try {
      root.querySelector<HTMLButtonElement>('.projected-action')!.focus()

      preferences.setMode('progressive-section-test', 'basic')
      fixture.detectChanges()

      expect(root.querySelector('.hai-progressive-section')).toBeNull()
      expect(document.activeElement).toBe(main)
      expect(preferences.get('progressive-section-test').openSections['members']).toBeTrue()
    } finally {
      fixture.destroy()
      main.remove()
    }
  })

  it('uses distinct control and panel IDs for duplicate component instances', () => {
    const first = createSectionFixture()
    const second = createSectionFixture()
    const firstSection = first.componentInstance
    const secondSection = second.componentInstance

    expect(firstSection.triggerId).not.toBe(secondSection.triggerId)
    expect(firstSection.panelId).not.toBe(secondSection.panelId)

    expect(firstSection.open).toBeFalse()
    ;(first.nativeElement.querySelector('button') as HTMLButtonElement).click()
    expect(firstSection.open).toBeTrue()
    first.detectChanges()
    const button = first.nativeElement.querySelector('button') as HTMLButtonElement
    expect(button.getAttribute('aria-controls')).toBe(firstSection.panelId)
    expect(first.nativeElement.querySelector(`#${firstSection.panelId}`)).not.toBeNull()
    expect(second.nativeElement.querySelector(`#${firstSection.panelId}`)).toBeNull()
  })

  it('opens and persists an advanced section referenced by the current URL fragment', () => {
    const originalUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#system-metrics`)
    try {
      const fixture = createSectionFixture('hai-os', 'system-metrics', true)
      const section = fixture.componentInstance
      const preferences = TestBed.inject(ModuleViewPreferencesService)

      expect(section.open).toBeTrue()
      expect(preferences.get('hai-os').mode).toBe('advanced')
      expect(preferences.get('hai-os').openSections['system-metrics']).toBeTrue()
      fixture.destroy()
    } finally {
      window.history.replaceState(null, '', originalUrl)
      document.body.classList.remove('hai-view-advanced')
    }
  })

  it('does not let an unregistered fragment force Advanced mode', () => {
    const originalUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#invented-section`)
    try {
      const fixture = createSectionFixture('hai-os', 'system-metrics', true)
      const section = fixture.componentInstance
      const preferences = TestBed.inject(ModuleViewPreferencesService)

      expect(section.open).toBeFalse()
      expect(preferences.get('hai-os').mode).toBe('basic')
      expect(preferences.get('hai-os').openSections).toEqual({})
      expect(document.body.classList.contains('hai-view-advanced')).toBeFalse()
      fixture.destroy()
    } finally {
      window.history.replaceState(null, '', originalUrl)
      document.body.classList.remove('hai-view-advanced')
    }
  })

  it('does not persist or restore a disclosure under a missing module or section ID', () => {
    const fixture = createSectionFixture('', '')
    const section = fixture.componentInstance
    section.setOpen(true)

    expect(section.open).toBeTrue()
    expect(TestBed.inject(ModuleViewPreferencesService).get('').openSections).toEqual({})

    fixture.componentRef.setInput('sectionId', 'still-invalid-without-a-module')
    fixture.detectChanges()
    expect(section.open).toBeFalse()
  })
})

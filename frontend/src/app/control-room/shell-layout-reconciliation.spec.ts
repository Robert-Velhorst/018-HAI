import { TestBed, ComponentFixture } from '@angular/core/testing'
import { RouterTestingModule } from '@angular/router/testing'
import { EMPTY } from 'rxjs'
import { AppShellComponent } from './app-shell.component'
import { ModuleViewPreferencesService } from './module-view-preferences.service'
import { ThemeService } from '../services/theme.service'
import { RuntimeControlService } from '../services/runtime-control.service'

describe('Shared shell stylesheet reconciliation', () => {
  let fixture: ComponentFixture<AppShellComponent>
  let frame: HTMLIFrameElement

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AppShellComponent, RouterTestingModule],
      providers: [
        { provide: ModuleViewPreferencesService, useValue: {
          get: () => ({ mode: 'basic', navigationMode: 'auto', openSections: {} }),
          getNavigationGroups: () => ({ version: 1, openGroups: {} }),
        } },
        { provide: ThemeService, useValue: { mode: () => 'dark' } },
        { provide: RuntimeControlService, useValue: { status: () => EMPTY } },
      ],
    }).compileComponents()
    fixture = TestBed.createComponent(AppShellComponent)
    fixture.detectChanges()
    frame = document.createElement('iframe')
    frame.style.cssText = 'position:fixed;left:0;top:0;height:600px;border:0;z-index:100000'
    frame.title = 'Isolated shell layout test'
    document.body.append(frame)
    const doc = frame.contentDocument!
    for (const sheet of Array.from(document.styleSheets)) {
      const style = doc.createElement('style')
      style.textContent = Array.from(sheet.cssRules).map(rule => rule.cssText).join('\n')
      doc.head.append(style)
    }
    doc.body.className = 'hai-theme-dark'
    doc.body.append((fixture.nativeElement as HTMLElement).querySelector('.hai-app-shell')!.cloneNode(true))
  })

  afterEach(() => {
    frame.remove()
    fixture.destroy()
  })

  for (const width of [375, 768, 1024, 1440]) {
    it(`keeps the rendered shell and global controls within a ${width}px viewport`, () => {
      frame.style.width = `${width}px`
      const doc = frame.contentDocument!
      const view = frame.contentWindow!
      expect(view.innerWidth).toBe(width)
      const header = doc.querySelector<HTMLElement>('.hai-global-bar')!
      expect(doc.documentElement.scrollWidth).toBeLessThanOrEqual(width)
      expect(header.getBoundingClientRect().right).toBeLessThanOrEqual(width)
      const controls = header.querySelectorAll<HTMLElement>(':scope > button, :scope > details')
      let previousRight = header.getBoundingClientRect().left
      for (const control of Array.from(controls)) {
        if (view.getComputedStyle(control).display === 'none') continue
        const box = control.getBoundingClientRect()
        expect(box.left).toBeGreaterThanOrEqual(previousRight)
        expect(box.right).toBeLessThanOrEqual(width)
        previousRight = box.right
      }
    })
  }

  it('limits legacy source header subtitle styling to its subtitle, not button labels', () => {
    const doc = frame.contentDocument!
    const header = doc.createElement('header')
    header.className = 'sources-header'
    header.innerHTML = '<div><h1>Sources</h1><span>Connected accounts</span></div><button><span>Refresh</span></button>'
    doc.querySelector('.hai-module-outlet')!.append(header)
    const view = frame.contentWindow!
    expect(view.getComputedStyle(header.querySelector('div > span')!).display).toBe('block')
    expect(view.getComputedStyle(header.querySelector('button > span')!).display).toBe('inline')
    expect(view.getComputedStyle(header.querySelector('button > span')!).marginTop).toBe('0px')
  })

  it('preserves an explicit outline for focused navigation controls', () => {
    const doc = frame.contentDocument!
    frame.style.width = '1440px'
    const link = doc.querySelector<HTMLElement>('.hai-navigation__item--active')!
    link.focus({ focusVisible: true })
    expect(doc.activeElement).toBe(link)
    expect(parseFloat(frame.contentWindow!.getComputedStyle(link).outlineWidth)).toBeGreaterThanOrEqual(3)
  })
})

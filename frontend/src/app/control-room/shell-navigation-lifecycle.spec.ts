import { Router } from '@angular/router'
import { AppShellComponent } from './app-shell.component'
import { moduleForUrl } from './module-url'
import { ModuleViewPreferencesService } from './module-view-preferences.service'
import { ThemeService } from '../services/theme.service'
import { RuntimeControlService } from '../services/runtime-control.service'

describe('Shell pending navigation focus lifecycle', () => {
  let component: AppShellComponent
  let host: HTMLDivElement
  let main: HTMLElement
  let menu: HTMLButtonElement
  let frames: FrameRequestCallback[]
  let router: { url: string; navigateByUrl: jasmine.Spy }

  beforeEach(() => {
    host = document.createElement('div')
    host.innerHTML = '<button>Current focus</button><button>Menu</button><main id="hai-main" tabindex="-1"></main>'
    document.body.append(host)
    main = host.querySelector('main')!
    menu = host.querySelectorAll('button')[1]
    host.querySelector('button')!.focus()
    frames = []
    spyOn(window, 'requestAnimationFrame').and.callFake(callback => { frames.push(callback); return frames.length })
    router = { url: '/control-center', navigateByUrl: jasmine.createSpy('navigateByUrl') }
    component = new AppShellComponent(router as unknown as Router,
      {} as ModuleViewPreferencesService, {} as ThemeService, {} as RuntimeControlService)
    ;(component as any).mobileMenuButton = { nativeElement: menu }
  })

  afterEach(() => {
    component.ngOnDestroy()
    host.remove()
  })

  function navigate(): void {
    component.navigateFromNavigation(moduleForUrl('/memory'), new MouseEvent('click', { button: 0, cancelable: true }))
  }

  function flushFrames(): void { frames.splice(0).forEach(frame => frame(0)) }

  it('ignores a successful navigation promise resolving after destruction', async () => {
    let resolve!: (value: boolean) => void
    router.navigateByUrl.and.returnValue(new Promise<boolean>(done => { resolve = done }))
    navigate()
    component.ngOnDestroy()
    resolve(true)
    await Promise.resolve()
    flushFrames()
    expect(document.activeElement).toBe(host.querySelector('button'))
  })

  it('ignores an already scheduled focus frame after destruction', async () => {
    router.navigateByUrl.and.returnValue(Promise.resolve(true))
    navigate()
    await Promise.resolve()
    expect(frames.length).toBe(1)
    component.ngOnDestroy()
    flushFrames()
    expect(document.activeElement).toBe(host.querySelector('button'))
  })

  it('does not restore the mobile menu after a rejected navigation on a destroyed shell', async () => {
    let reject!: (reason: Error) => void
    router.navigateByUrl.and.returnValue(new Promise<boolean>((_, fail) => { reject = fail }))
    component.mobileNavigationOpen = true
    navigate()
    component.ngOnDestroy()
    reject(new Error('route unavailable'))
    await Promise.resolve()
    flushFrames()
    expect(document.activeElement).toBe(host.querySelector('button'))
  })

  it('does not let an older canceled navigation override a newer completed navigation', async () => {
    let resolveOlder!: (value: boolean) => void
    router.navigateByUrl.and.returnValues(new Promise<boolean>(done => { resolveOlder = done }), Promise.resolve(true))
    component.mobileNavigationOpen = true
    navigate()
    component.navigateFromNavigation(moduleForUrl('/pursuits'), new MouseEvent('click', { button: 0, cancelable: true }))
    await Promise.resolve()
    flushFrames()
    expect(document.activeElement).toBe(main)
    resolveOlder(false)
    await Promise.resolve()
    flushFrames()
    expect(document.activeElement).toBe(main)
  })

  it('keeps focus transfer for a live successful navigation', async () => {
    router.navigateByUrl.and.returnValue(Promise.resolve(true))
    navigate()
    await Promise.resolve()
    flushFrames()
    expect(document.activeElement).toBe(main)
  })
})

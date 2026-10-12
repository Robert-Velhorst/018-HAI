import { BulbOutline, DisconnectOutline, ReloadOutline, StarOutline, WarningFill, WarningOutline } from '@ant-design/icons-angular/icons'
import { HAI_ICONS } from '../app.module'
import { ThemeService } from './theme.service'

describe('ThemeService icon registration', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  afterEach(() => {
    window.localStorage.clear()
  })

  it('starts new local installations in dark mode while retaining an explicit light preference', () => {
    expect(new ThemeService().mode()).toBe('dark')

    window.localStorage.setItem('hai-theme-mode', 'light')
    expect(new ThemeService().mode()).toBe('light')
  })

  it('returns registered NG-Zorro icons for both theme modes', () => {
    const registeredNames = HAI_ICONS.map((icon) => icon.name)
    const service = new ThemeService()

    service.setMode('light')
    expect(service.icon()).toBe(BulbOutline.name)
    expect(registeredNames).toContain(service.icon())

    service.setMode('dark')
    expect(service.icon()).toBe(StarOutline.name)
    expect(registeredNames).toContain(service.icon())
    expect(registeredNames).not.toContain('moon')
  })

  it('publishes every global theme change for shell and module-local controls', () => {
    const service = new ThemeService()
    const changes: string[] = []
    const subscription = service.changes$.subscribe((mode) => changes.push(mode))

    service.setMode('light')
    service.toggle()

    expect(changes).toEqual(['dark', 'light', 'dark'])
    subscription.unsubscribe()
  })

  it('bundles shared status and refresh icons instead of requesting missing SVG assets', () => {
    const registeredNames = HAI_ICONS.map((icon) => icon.name)
    expect(registeredNames).toContain(DisconnectOutline.name)
    expect(registeredNames).toContain(ReloadOutline.name)
    expect(registeredNames).toContain(WarningFill.name)
    expect(registeredNames).toContain(WarningOutline.name)
  })
})

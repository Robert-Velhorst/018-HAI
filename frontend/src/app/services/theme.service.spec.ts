import {
  BulbOutline,
  CheckCircleFill,
  CloseCircleFill,
  HomeOutline,
  ProjectOutline,
  ReadOutline,
  StarOutline,
  WalletOutline,
  WarningFill,
} from '@ant-design/icons-angular/icons'
import { HAI_ICONS } from '../app.module'
import { ThemeService } from './theme.service'

describe('ThemeService icon registration', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  afterEach(() => {
    window.localStorage.clear()
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

  it('registers dynamic capacity and filled health-state icons', () => {
    const registered = new Set(HAI_ICONS.map((icon) => `${icon.name}:${icon.theme}`))

    for (const icon of [HomeOutline, ProjectOutline, ReadOutline, WalletOutline]) {
      expect(registered).toContain(`${icon.name}:${icon.theme}`)
    }
    for (const icon of [CheckCircleFill, CloseCircleFill, WarningFill]) {
      expect(registered).toContain(`${icon.name}:${icon.theme}`)
    }
  })
})

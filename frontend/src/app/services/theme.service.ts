import { Injectable } from '@angular/core'
import { BehaviorSubject } from 'rxjs'

export type ThemeMode = 'light' | 'dark'

@Injectable({
  providedIn: 'root',
})
export class ThemeService {
  private readonly storageKey = 'hai-theme-mode'
  private readonly legacyStorageKey = 'hai-control-center-theme'
  private currentMode: ThemeMode = 'dark'
  private readonly modeSubject = new BehaviorSubject<ThemeMode>(this.currentMode)
  readonly changes$ = this.modeSubject.asObservable()

  constructor() {
    this.currentMode = this.load()
    this.modeSubject.next(this.currentMode)
    this.apply(this.currentMode)
  }

  mode(): ThemeMode {
    return this.currentMode
  }

  setMode(mode: ThemeMode): ThemeMode {
    this.currentMode = mode
    this.modeSubject.next(mode)
    this.persist(mode)
    this.apply(mode)
    return mode
  }

  toggle(): ThemeMode {
    return this.setMode(this.currentMode === 'dark' ? 'light' : 'dark')
  }

  label(): string {
    return this.currentMode === 'dark' ? 'Dark mode' : 'Light mode'
  }

  icon(): string {
    return this.currentMode === 'dark' ? 'star' : 'bulb'
  }

  private load(): ThemeMode {
    try {
      const saved =
        window.localStorage.getItem(this.storageKey) ||
        window.localStorage.getItem(this.legacyStorageKey)
      // The control room is dark-first. A saved explicit light preference
      // remains valid, but a new or storage-restricted browser starts dark.
      return saved === 'light' ? 'light' : 'dark'
    } catch {
      return 'dark'
    }
  }

  private persist(mode: ThemeMode): void {
    try {
      window.localStorage.setItem(this.storageKey, mode)
      window.localStorage.setItem(this.legacyStorageKey, mode)
    } catch {
      // Some hardened browser contexts disable localStorage.
    }
  }

  private apply(mode: ThemeMode): void {
    const root = document.documentElement
    const body = document.body
    root.setAttribute('data-hai-theme', mode)
    root.classList.toggle('hai-theme-dark', mode === 'dark')
    root.classList.toggle('hai-theme-light', mode === 'light')
    body.classList.toggle('hai-theme-dark', mode === 'dark')
    body.classList.toggle('hai-theme-light', mode === 'light')
    body.style.colorScheme = mode
  }
}

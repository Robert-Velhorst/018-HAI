import { DOCUMENT } from '@angular/common'
import { Inject, Injectable, Optional } from '@angular/core'
import { BehaviorSubject, distinctUntilChanged, Observable, of } from 'rxjs'

export type HaiViewMode = 'basic' | 'advanced'
export type HaiNavigationMode = 'auto' | 'expanded' | 'compact'

export interface HaiModuleViewPreferences {
  version: 1
  mode: HaiViewMode
  openSections: Record<string, boolean>
  navigationMode: HaiNavigationMode
}

export interface HaiNavigationGroupPreferences {
  version: 1
  openGroups: Record<string, boolean>
}

const DEFAULT_PREFERENCES: HaiModuleViewPreferences = {
  version: 1,
  mode: 'basic',
  openSections: {},
  navigationMode: 'auto',
}

const DEFAULT_NAVIGATION_GROUPS: HaiNavigationGroupPreferences = {
  version: 1,
  openGroups: {},
}

const MAX_IDENTIFIER_LENGTH = 128
const MAX_SECTION_COUNT = 128
const MAX_NAVIGATION_GROUP_COUNT = 32
const MAX_VOLATILE_MODULE_COUNT = 128
const MAX_SERIALIZED_LENGTH = 128 * 1024

function currentDocument(): Document | null {
  return typeof document === 'undefined' ? null : document
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function booleanRecord(entries: Iterable<readonly [string, boolean]>): Record<string, boolean> {
  const record = Object.create(null) as Record<string, boolean>
  for (const [key, value] of entries) record[key] = value
  return record
}

@Injectable({ providedIn: 'root' })
export class ModuleViewPreferencesService {
  private readonly prefix = 'hai.module-view.v1.'
  private readonly navigationModeKey = 'hai.shell-navigation.v1'
  private readonly navigationGroupsKey = 'hai.navigation-groups.v1'
  private readonly volatilePreferences = new Map<string, HaiModuleViewPreferences>()
  private readonly modeSubjects = new Map<string, BehaviorSubject<HaiViewMode>>()
  private readonly sectionSubjects = new Map<string, Map<string, BehaviorSubject<boolean>>>()
  private readonly pendingPublications = new Map<string, HaiModuleViewPreferences>()
  private readonly publishingModules = new Set<string>()
  private volatileNavigationMode?: HaiNavigationMode
  private volatileNavigationGroups?: HaiNavigationGroupPreferences

  constructor(
    @Optional() @Inject(DOCUMENT) private readonly hostDocument: Document | null = currentDocument(),
  ) {}

  get(moduleId: string): HaiModuleViewPreferences {
    const key = this.key(moduleId)
    if (!key) return this.defaults()

    const volatile = this.volatilePreferences.get(key)
    if (volatile) return this.copy(volatile)

    const storage = this.storage()
    if (!storage) return this.defaults()

    try {
      const raw = storage.getItem(key)
      return raw === null ? this.defaults() : this.parse(raw)
    } catch {
      return this.defaults()
    }
  }

  setMode(moduleId: string, mode: HaiViewMode): HaiModuleViewPreferences {
    return this.update(moduleId, { mode })
  }

  watchMode(moduleId: string): Observable<HaiViewMode> {
    const key = this.key(moduleId)
    if (!key) return of('basic')

    let subject = this.modeSubjects.get(key)
    if (!subject) {
      if (this.modeSubjects.size >= MAX_VOLATILE_MODULE_COUNT) return of(this.get(moduleId).mode)
      subject = new BehaviorSubject<HaiViewMode>(this.get(moduleId).mode)
      this.modeSubjects.set(key, subject)
    }
    return subject.asObservable().pipe(distinctUntilChanged())
  }

  watchSection(moduleId: string, sectionId: string): Observable<boolean> {
    const key = this.key(moduleId)
    if (!key || !this.isValidIdentifier(sectionId)) return of(false)

    let subjects = this.sectionSubjects.get(key)
    if (!subjects) {
      if (this.sectionSubjects.size >= MAX_VOLATILE_MODULE_COUNT) {
        return of(this.get(moduleId).openSections[sectionId] === true)
      }
      subjects = new Map<string, BehaviorSubject<boolean>>()
      this.sectionSubjects.set(key, subjects)
    }

    let subject = subjects.get(sectionId)
    if (!subject) {
      if (subjects.size >= MAX_SECTION_COUNT) return of(this.get(moduleId).openSections[sectionId] === true)
      subject = new BehaviorSubject<boolean>(this.get(moduleId).openSections[sectionId] === true)
      subjects.set(sectionId, subject)
    }
    return subject.asObservable().pipe(distinctUntilChanged())
  }

  setSection(moduleId: string, sectionId: string, open: boolean): HaiModuleViewPreferences {
    const key = this.key(moduleId)
    if (!key || !this.isValidIdentifier(sectionId) || typeof open !== 'boolean') return this.get(moduleId)

    const current = this.get(moduleId)
    if (
      !Object.prototype.hasOwnProperty.call(current.openSections, sectionId) &&
      Object.keys(current.openSections).length >= MAX_SECTION_COUNT
    ) return current

    const entries = Object.entries(current.openSections)
    entries.push([sectionId, open])
    const openSections = booleanRecord(entries)
    return this.write(moduleId, { ...current, openSections })
  }

  getNavigationMode(legacyModuleId?: string): HaiNavigationMode {
    if (this.volatileNavigationMode) return this.volatileNavigationMode

    const storage = this.storage()
    if (!storage) {
      const mode = legacyModuleId ? this.get(legacyModuleId).navigationMode : 'auto'
      this.volatileNavigationMode = mode
      return mode
    }

    try {
      const raw = storage.getItem(this.navigationModeKey)
      if (raw !== null) {
        const parsed: unknown = JSON.parse(raw)
        if (isRecord(parsed) && parsed['version'] === 1) return this.normalizeNavigationMode(parsed['mode'])
        return 'auto'
      }
    } catch {
      return 'auto'
    }

    const legacyMode = legacyModuleId ? this.get(legacyModuleId).navigationMode : 'auto'
    this.setNavigationMode(legacyMode)
    return legacyMode
  }

  setNavigationMode(navigationMode: HaiNavigationMode): HaiNavigationMode {
    const mode = this.normalizeNavigationMode(navigationMode)
    const storage = this.storage()
    if (!storage) {
      this.volatileNavigationMode = mode
      return mode
    }

    try {
      storage.setItem(this.navigationModeKey, JSON.stringify({ version: 1, mode }))
      this.volatileNavigationMode = undefined
    } catch {
      this.volatileNavigationMode = mode
    }
    return mode
  }

  getNavigationGroups(): HaiNavigationGroupPreferences {
    if (this.volatileNavigationGroups) return this.copyNavigationGroups(this.volatileNavigationGroups)

    const storage = this.storage()
    if (!storage) return this.defaultNavigationGroups()

    try {
      const raw = storage.getItem(this.navigationGroupsKey)
      return raw === null ? this.defaultNavigationGroups() : this.parseNavigationGroups(raw)
    } catch {
      return this.defaultNavigationGroups()
    }
  }

  setNavigationGroupOpen(groupId: string, open: boolean): HaiNavigationGroupPreferences {
    if (!this.isValidGroupIdentifier(groupId) || typeof open !== 'boolean') return this.getNavigationGroups()

    const current = this.getNavigationGroups()
    if (
      !Object.prototype.hasOwnProperty.call(current.openGroups, groupId) &&
      Object.keys(current.openGroups).length >= MAX_NAVIGATION_GROUP_COUNT
    ) return current

    const next: HaiNavigationGroupPreferences = {
      version: 1,
      openGroups: Object.fromEntries([
        ...Object.entries(current.openGroups),
        [groupId, open],
      ]) as Record<string, boolean>,
    }
    const storage = this.storage()
    if (!storage) {
      this.volatileNavigationGroups = next
      return this.copyNavigationGroups(next)
    }

    try {
      storage.setItem(this.navigationGroupsKey, JSON.stringify(next))
      this.volatileNavigationGroups = undefined
    } catch {
      this.volatileNavigationGroups = next
    }
    return this.copyNavigationGroups(next)
  }

  reset(moduleId: string): HaiModuleViewPreferences {
    const key = this.key(moduleId)
    const defaults = this.defaults()
    if (!key) return defaults

    const storage = this.storage()
    if (!storage) {
      if (!this.rememberVolatile(key, defaults)) return this.get(moduleId)
      this.publishPreferences(key, defaults)
      return this.copy(defaults)
    }

    try {
      storage.removeItem(key)
      this.volatilePreferences.delete(key)
    } catch {
      if (!this.rememberVolatile(key, defaults)) return this.get(moduleId)
    }
    this.publishPreferences(key, defaults)
    return this.copy(defaults)
  }

  isSessionOnly(moduleId: string): boolean {
    const key = this.key(moduleId)
    return !!key && this.volatilePreferences.has(key)
  }

  private update(moduleId: string, patch: Partial<HaiModuleViewPreferences>): HaiModuleViewPreferences {
    if (!this.key(moduleId)) return this.defaults()
    return this.write(moduleId, { ...this.get(moduleId), ...patch })
  }

  private write(moduleId: string, value: HaiModuleViewPreferences): HaiModuleViewPreferences {
    const normalized = this.normalize(value)
    const key = this.key(moduleId)
    if (!key) return normalized

    const storage = this.storage()
    if (!storage) {
      if (!this.rememberVolatile(key, normalized)) return this.get(moduleId)
      this.publishPreferences(key, normalized)
      return this.copy(normalized)
    }

    try {
      storage.setItem(key, JSON.stringify(normalized))
      this.volatilePreferences.delete(key)
      this.publishPreferences(key, normalized)
    } catch {
      if (!this.rememberVolatile(key, normalized)) return this.get(moduleId)
      this.publishPreferences(key, normalized)
      return this.copy(normalized)
    }
    return this.copy(normalized)
  }

  private publishMode(key: string, mode: HaiViewMode): void {
    const subject = this.modeSubjects.get(key)
    if (subject && subject.value !== mode) subject.next(mode)
  }

  private publishPreferences(key: string, preferences: HaiModuleViewPreferences): void {
    this.pendingPublications.set(key, preferences)
    if (this.publishingModules.has(key)) return
    this.publishingModules.add(key)
    try {
      // Finish each notification before draining observer writes to the same module.
      while (this.pendingPublications.has(key)) {
        const current = this.pendingPublications.get(key)!
        this.pendingPublications.delete(key)
        this.publishMode(key, current.mode)
        if (this.pendingPublications.has(key)) continue
        const subjects = this.sectionSubjects.get(key)
        if (!subjects) continue
        for (const [sectionId, subject] of subjects) {
          if (this.pendingPublications.has(key)) break
          const open = current.openSections[sectionId] === true
          if (subject.value !== open) subject.next(open)
        }
      }
    } finally {
      this.publishingModules.delete(key)
      this.pendingPublications.delete(key)
    }
  }

  private storage(): Storage | null {
    try {
      return this.hostDocument?.defaultView?.localStorage ?? null
    } catch {
      return null
    }
  }

  private rememberVolatile(key: string, value: HaiModuleViewPreferences): boolean {
    if (!this.volatilePreferences.has(key) && this.volatilePreferences.size >= MAX_VOLATILE_MODULE_COUNT) return false
    this.volatilePreferences.set(key, value)
    return true
  }

  private key(moduleId: string): string | null {
    return this.isValidIdentifier(moduleId) ? `${this.prefix}${moduleId}` : null
  }

  private isValidIdentifier(value: string): boolean {
    return typeof value === 'string' &&
      value.trim().length > 0 &&
      value.length <= MAX_IDENTIFIER_LENGTH &&
      !/[\u0000-\u001f\u007f-\u009f]/.test(value)
  }

  private parse(raw: string): HaiModuleViewPreferences {
    if (raw.length > MAX_SERIALIZED_LENGTH) return this.defaults()

    try {
      const parsed: unknown = JSON.parse(raw)
      if (!isRecord(parsed) || parsed['version'] !== 1) return this.defaults()
      return this.normalize(parsed)
    } catch {
      return this.defaults()
    }
  }

  private parseNavigationGroups(raw: string): HaiNavigationGroupPreferences {
    if (raw.length > MAX_SERIALIZED_LENGTH) return this.defaultNavigationGroups()

    try {
      const parsed: unknown = JSON.parse(raw)
      if (!isRecord(parsed) || parsed['version'] !== 1) return this.defaultNavigationGroups()
      const groups = isRecord(parsed['openGroups'])
        ? Object.entries(parsed['openGroups']).filter(([id, open]) => this.isValidGroupIdentifier(id) && typeof open === 'boolean')
        : []
      return {
        version: 1,
        openGroups: Object.fromEntries(groups.slice(0, MAX_NAVIGATION_GROUP_COUNT)) as Record<string, boolean>,
      }
    } catch {
      return this.defaultNavigationGroups()
    }
  }

  private isValidGroupIdentifier(value: string): boolean {
    return typeof value === 'string' && /^[a-z0-9](?:[a-z0-9-]{0,47})$/.test(value)
  }

  private normalize(value: unknown): HaiModuleViewPreferences {
    if (!isRecord(value)) return this.defaults()

    const sections = isRecord(value['openSections'])
      ? Object.entries(value['openSections'])
          .filter(([id, open]) => this.isValidIdentifier(id) && typeof open === 'boolean')
          .slice(0, MAX_SECTION_COUNT)
      : []

    return {
      version: 1,
      mode: value['mode'] === 'advanced' ? 'advanced' : 'basic',
      openSections: booleanRecord(sections as Array<[string, boolean]>),
      navigationMode:
        value['navigationMode'] === 'compact' || value['navigationMode'] === 'expanded'
          ? value['navigationMode'] as HaiNavigationMode
          : 'auto',
    }
  }

  private defaults(): HaiModuleViewPreferences {
    return this.copy(DEFAULT_PREFERENCES)
  }

  private defaultNavigationGroups(): HaiNavigationGroupPreferences {
    return this.copyNavigationGroups(DEFAULT_NAVIGATION_GROUPS)
  }

  private normalizeNavigationMode(value: unknown): HaiNavigationMode {
    return value === 'compact' || value === 'expanded' ? value : 'auto'
  }

  private copyNavigationGroups(value: HaiNavigationGroupPreferences): HaiNavigationGroupPreferences {
    return { version: 1, openGroups: Object.fromEntries(Object.entries(value.openGroups)) as Record<string, boolean> }
  }

  private copy(value: HaiModuleViewPreferences): HaiModuleViewPreferences {
    return {
      ...value,
      openSections: booleanRecord(Object.entries(value.openSections)),
    }
  }
}

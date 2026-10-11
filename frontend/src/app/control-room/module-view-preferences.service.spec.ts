import { ModuleViewPreferencesService } from './module-view-preferences.service'

describe('ModuleViewPreferencesService', () => {
  let service: ModuleViewPreferencesService
  const moduleIds = ['memory', 'workflow-engine', 'hai.module-view.v1.memory', 'memory.']

  beforeEach(() => {
    for (const moduleId of moduleIds) localStorage.removeItem(`hai.module-view.v1.${moduleId}`)
    localStorage.removeItem('hai.module-view.v1.')
    localStorage.removeItem('hai.shell-navigation.v1')
    localStorage.removeItem('hai.navigation-groups.v1')
    service = new ModuleViewPreferencesService(document)
  })

  afterEach(() => {
    for (const moduleId of moduleIds) localStorage.removeItem(`hai.module-view.v1.${moduleId}`)
    localStorage.removeItem('hai.module-view.v1.')
    localStorage.removeItem('hai.shell-navigation.v1')
    localStorage.removeItem('hai.navigation-groups.v1')
  })

  it('starts every module in basic automatic mode', () => {
    expect(service.get('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })
  })

  it('stores navigation disclosure separately from per-module view settings', () => {
    expect(service.getNavigationGroups()).toEqual({ version: 1, openGroups: {} })

    service.setNavigationGroupOpen('intelligence', true)
    service.setMode('memory', 'advanced')

    expect(service.getNavigationGroups()).toEqual({ version: 1, openGroups: { intelligence: true } })
    expect(service.reset('memory').mode).toBe('basic')
    expect(service.getNavigationGroups().openGroups['intelligence']).toBeTrue()
    expect(new ModuleViewPreferencesService(document).getNavigationGroups().openGroups['intelligence']).toBeTrue()
  })

  it('safely defaults malformed navigation disclosure preferences and rejects invalid group IDs', () => {
    localStorage.setItem('hai.navigation-groups.v1', '{not-json')
    expect(service.getNavigationGroups()).toEqual({ version: 1, openGroups: {} })

    localStorage.setItem('hai.navigation-groups.v1', '{"version":1,"openGroups":{"work":true,"__proto__":true,"System":true,"intelligence":"yes"}}')
    expect(service.getNavigationGroups()).toEqual({ version: 1, openGroups: { work: true } })

    service.setNavigationGroupOpen('System', true)
    service.setNavigationGroupOpen('../memory', true)
    expect(service.getNavigationGroups().openGroups).toEqual({ work: true })
  })

  it('bounds navigation disclosure group count while allowing updates to existing groups', () => {
    const tooManyGroups = Object.fromEntries(
      Array.from({ length: 33 }, (_, index) => [`group-${index}`, true]),
    )
    localStorage.setItem('hai.navigation-groups.v1', JSON.stringify({ version: 1, openGroups: tooManyGroups }))
    expect(Object.keys(service.getNavigationGroups().openGroups)).toHaveSize(32)

    localStorage.removeItem('hai.navigation-groups.v1')
    for (let index = 0; index < 32; index++) service.setNavigationGroupOpen(`group-${index}`, true)

    const atLimit = service.setNavigationGroupOpen('overflow', true)
    expect(Object.keys(atLimit.openGroups)).toHaveSize(32)
    expect(atLimit.openGroups['overflow']).toBeUndefined()

    const updated = service.setNavigationGroupOpen('group-0', false)
    expect(updated.openGroups['group-0']).toBeFalse()
    expect(Object.keys(updated.openGroups)).toHaveSize(32)
  })

  it('retains navigation disclosure for the current session when browser storage rejects writes', () => {
    const throwingStorage = {
      getItem: () => null,
      setItem: () => { throw new Error('storage is full') },
      removeItem: () => undefined,
    } as unknown as Storage
    const blockedDocument = { defaultView: { localStorage: throwingStorage } } as unknown as Document
    const blockedService = new ModuleViewPreferencesService(blockedDocument)

    blockedService.setNavigationGroupOpen('system', true)

    expect(blockedService.getNavigationGroups()).toEqual({ version: 1, openGroups: { system: true } })
    expect(new ModuleViewPreferencesService(blockedDocument).getNavigationGroups()).toEqual({ version: 1, openGroups: {} })
  })

  it('bounds session-only module state when browser storage is unavailable', () => {
    const throwingStorage = {
      getItem: () => null,
      setItem: () => { throw new Error('storage is full') },
      removeItem: () => undefined,
    } as unknown as Storage
    const blockedDocument = { defaultView: { localStorage: throwingStorage } } as unknown as Document
    const blockedService = new ModuleViewPreferencesService(blockedDocument)

    for (let index = 0; index < 128; index++) blockedService.setMode(`module-${index}`, 'advanced')
    const rejected = blockedService.setMode('module-overflow', 'advanced')

    expect(blockedService.get('module-127').mode).toBe('advanced')
    expect(blockedService.isSessionOnly('module-127')).toBeTrue()
    expect(rejected.mode).toBe('basic')
    expect(blockedService.get('module-overflow').mode).toBe('basic')
    expect(blockedService.isSessionOnly('module-overflow')).toBeFalse()
  })

  it('persists view state independently for each exact module identifier', () => {
    service.setMode('memory', 'advanced')
    service.setSection('memory', 'records', true)
    service.setNavigationMode('compact')
    service.setMode('hai.module-view.v1.memory', 'advanced')
    service.setSection('memory.', 'records', false)

    expect(service.get('memory')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { records: true },
      navigationMode: 'auto',
    })
    expect(service.getNavigationMode('memory')).toBe('compact')
    expect(service.get('workflow-engine')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })
    expect(service.get('hai.module-view.v1.memory').mode).toBe('advanced')
    expect(service.get('memory.').openSections['records']).toBeFalse()
  })

  it('notifies mounted sections when their saved disclosure state is changed or reset', () => {
    const states: boolean[] = []
    const subscription = service.watchSection('memory', 'records').subscribe((open) => states.push(open))

    service.setSection('memory', 'records', true)
    service.setSection('memory', 'records', true)
    service.reset('memory')

    expect(states).toEqual([false, true, false])
    subscription.unsubscribe()
  })

  it('does not publish stale section state after a mode observer updates that module', () => {
    const states: boolean[] = []
    const sectionSubscription = service.watchSection('memory', 'records').subscribe((open) => states.push(open))
    const modeSubscription = service.watchMode('memory').subscribe((mode) => {
      if (mode === 'advanced') service.setSection('memory', 'records', true)
    })

    service.setMode('memory', 'advanced')

    expect(service.get('memory').openSections['records']).toBeTrue()
    expect(states).toEqual([false, true])
    sectionSubscription.unsubscribe()
    modeSubscription.unsubscribe()
  })

  it('does not overwrite a later section notification with an interrupted older publication', () => {
    service.setSection('memory', 'records', true)
    service.setSection('memory', 'evidence', true)
    const evidenceStates: boolean[] = []
    const recordsSubscription = service.watchSection('memory', 'records').subscribe((open) => {
      if (!open) service.setSection('memory', 'evidence', true)
    })
    const evidenceSubscription = service.watchSection('memory', 'evidence').subscribe((open) => evidenceStates.push(open))

    service.reset('memory')

    expect(service.get('memory').openSections['evidence']).toBeTrue()
    expect(evidenceStates).toEqual([true])
    recordsSubscription.unsubscribe()
    evidenceSubscription.unsubscribe()
  })

  it('leaves every section observer on the latest value after a reentrant write to the same section', () => {
    const first = service.watchSection('memory', 'records').subscribe((open) => {
      if (open) service.setSection('memory', 'records', false)
    })
    const states: boolean[] = []
    const second = service.watchSection('memory', 'records').subscribe((open) => states.push(open))

    service.setSection('memory', 'records', true)

    expect(service.get('memory').openSections['records']).toBeFalse()
    expect(states).toEqual([false, true, false])
    first.unsubscribe()
    second.unsubscribe()
  })

  it('leaves every mode observer on the latest value after a reentrant mode write', () => {
    const first = service.watchMode('memory').subscribe((mode) => {
      if (mode === 'advanced') service.setMode('memory', 'basic')
    })
    const modes: string[] = []
    const second = service.watchMode('memory').subscribe((mode) => modes.push(mode))

    service.setMode('memory', 'advanced')

    expect(service.get('memory').mode).toBe('basic')
    expect(modes).toEqual(['basic', 'advanced', 'basic'])
    first.unsubscribe()
    second.unsubscribe()
  })

  it('keeps identical section IDs and mode subscriptions isolated by module through updates and reset', () => {
    const memoryModes: string[] = []
    const workflowModes: string[] = []
    const memorySections: boolean[] = []
    const workflowSections: boolean[] = []
    const subscriptions = [
      service.watchMode('memory').subscribe((mode) => memoryModes.push(mode)),
      service.watchMode('workflow-engine').subscribe((mode) => workflowModes.push(mode)),
      service.watchSection('memory', 'records').subscribe((open) => memorySections.push(open)),
      service.watchSection('workflow-engine', 'records').subscribe((open) => workflowSections.push(open)),
    ]

    service.setMode('memory', 'advanced')
    service.setSection('memory', 'records', true)
    service.setMode('workflow-engine', 'advanced')
    service.setSection('workflow-engine', 'records', true)
    service.setSection('memory', 'records', true)
    service.reset('memory')

    expect(memoryModes).toEqual(['basic', 'advanced', 'basic'])
    expect(workflowModes).toEqual(['basic', 'advanced'])
    expect(memorySections).toEqual([false, true, false])
    expect(workflowSections).toEqual([false, true])
    expect(service.get('workflow-engine')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { records: true },
      navigationMode: 'auto',
    })
    subscriptions.forEach((subscription) => subscription.unsubscribe())
  })

  it('does not let an empty module identifier share a storage key', () => {
    service.setMode('', 'advanced')
    service.setMode('   ', 'advanced')

    expect(service.get('').mode).toBe('basic')
    expect(service.get('   ').mode).toBe('basic')
    expect(localStorage.getItem('hai.module-view.v1.')).toBeNull()
  })

  it('rejects oversized or control-character identifiers without creating storage entries', () => {
    const oversizedId = 'm'.repeat(129)
    service.setMode(oversizedId, 'advanced')
    service.setMode('mem\u0000ory', 'advanced')
    service.setSection('memory', oversizedId, true)
    service.setSection('memory', 'bad\u001fsection', true)

    expect(service.get(oversizedId).mode).toBe('basic')
    expect(service.get('mem\u0000ory').mode).toBe('basic')
    expect(service.get('memory').openSections).toEqual({})
    expect(localStorage.getItem(`hai.module-view.v1.${oversizedId}`)).toBeNull()
    expect(localStorage.getItem('hai.module-view.v1.mem\u0000ory')).toBeNull()
  })

  it('falls back safely and sanitizes malformed or unsupported stored data', () => {
    localStorage.setItem('hai.module-view.v1.memory', '{not-json')
    expect(service.get('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })

    localStorage.setItem('hai.module-view.v1.memory', JSON.stringify({ version: 0, mode: 'advanced' }))
    expect(service.get('memory').mode).toBe('basic')

    localStorage.setItem('hai.module-view.v1.memory', JSON.stringify({
      version: 1,
      mode: 'advanced',
      navigationMode: 'invalid',
      openSections: ['not', 'a', 'record'],
    }))
    expect(service.get('memory')).toEqual({ version: 1, mode: 'advanced', openSections: {}, navigationMode: 'auto' })
  })

  it('defaults corrupt and unsupported app-wide rail preferences without borrowing module state', () => {
    localStorage.setItem('hai.module-view.v1.memory', JSON.stringify({
      version: 1,
      mode: 'advanced',
      openSections: {},
      navigationMode: 'compact',
    }))

    for (const value of ['{not-json', JSON.stringify({ version: 2, mode: 'expanded' })]) {
      localStorage.setItem('hai.shell-navigation.v1', value)
      expect(service.getNavigationMode('workflow-engine')).toBe('auto')
    }
    expect(service.get('memory').mode).toBe('advanced')
    expect(service.get('workflow-engine').mode).toBe('basic')
  })

  it('bounds stored section state and rejects new entries after the per-module limit', () => {
    for (let index = 0; index < 128; index++) service.setSection('memory', `section-${index}`, true)

    const atLimit = service.setSection('memory', 'overflow', true)
    expect(Object.keys(atLimit.openSections)).toHaveSize(128)
    expect(atLimit.openSections['section-0']).toBeTrue()
    expect(atLimit.openSections['overflow']).toBeUndefined()

    const updated = service.setSection('memory', 'section-0', false)
    expect(updated.openSections['section-0']).toBeFalse()
    expect(Object.keys(updated.openSections)).toHaveSize(128)
  })

  it('bounds corrupt serialized values and sanitizes oversized section collections', () => {
    localStorage.setItem('hai.module-view.v1.memory', 'x'.repeat(128 * 1024 + 1))
    expect(service.get('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })

    const tooManySections = Object.fromEntries(
      Array.from({ length: 129 }, (_, index) => [`section-${index}`, true]),
    )
    localStorage.setItem('hai.module-view.v1.memory', JSON.stringify({
      version: 1,
      mode: 'advanced',
      navigationMode: 'auto',
      openSections: tooManySections,
    }))

    const normalized = service.get('memory')
    expect(normalized.mode).toBe('advanced')
    expect(Object.keys(normalized.openSections)).toHaveSize(128)
    expect(normalized.openSections['section-127']).toBeTrue()
    expect(normalized.openSections['section-128']).toBeUndefined()
  })

  it('keeps only boolean disclosure values and safely handles prototype-like section IDs', () => {
    localStorage.setItem('hai.module-view.v1.memory', '{"version":1,"mode":"advanced","openSections":{"records":true,"bad":"true","__proto__":true,"constructor":false},"navigationMode":"expanded"}')

    const value = service.get('memory')
    expect(value.version).toBe(1)
    expect(value.mode).toBe('advanced')
    expect(value.openSections['records']).toBeTrue()
    expect(value.openSections['constructor']).toBeFalse()
    expect(value.navigationMode).toBe('expanded')
    expect(Object.prototype.hasOwnProperty.call(value.openSections, '__proto__')).toBeTrue()
    expect(service.get('memory').openSections['bad']).toBeUndefined()
  })

  it('returns undefined for absent prototype-named sections without losing stored values', () => {
    const emptySections = service.get('memory').openSections
    expect(emptySections['constructor']).toBeUndefined()
    expect(emptySections['toString']).toBeUndefined()

    service.setSection('memory', 'constructor', true)
    service.setSection('memory', 'toString', false)

    const savedSections = service.get('memory').openSections
    expect(savedSections['constructor']).toBeTrue()
    expect(savedSections['toString']).toBeFalse()
    expect(Object.getPrototypeOf(savedSections)).toBeNull()
  })

  it('returns defensive copies so callers cannot mutate stored section state', () => {
    service.setSection('memory', 'records', true)
    const returned = service.get('memory')
    returned.openSections['records'] = false
    returned.openSections['another'] = true

    expect(service.get('memory').openSections).toEqual({ records: true })
  })

  it('uses session memory when rendered without a browser document', () => {
    const serverService = new ModuleViewPreferencesService({ defaultView: null } as unknown as Document)
    serverService.setMode('memory', 'advanced')
    serverService.setSection('memory', 'records', true)

    expect(serverService.get('memory')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { records: true },
      navigationMode: 'auto',
    })

    serverService.reset('memory')
    expect(serverService.get('memory').mode).toBe('basic')
  })

  it('survives exceptions while obtaining local storage and retains changes in memory', () => {
    const blockedDocument = Object.defineProperty({}, 'defaultView', {
      get: () => { throw new Error('storage access denied') },
    }) as Document
    const blockedService = new ModuleViewPreferencesService(blockedDocument)

    blockedService.setMode('memory', 'advanced')
    expect(blockedService.get('memory').mode).toBe('advanced')
    expect(blockedService.reset('memory').mode).toBe('basic')
    expect(blockedService.get('memory').mode).toBe('basic')
  })

  it('survives local-storage read failures and keeps observer state coherent in session memory', () => {
    const deniedStorage = {
      getItem: () => { throw new Error('read denied') },
      setItem: () => { throw new Error('write denied') },
      removeItem: () => { throw new Error('remove denied') },
    } as unknown as Storage
    const deniedDocument = { defaultView: { localStorage: deniedStorage } } as unknown as Document
    const deniedService = new ModuleViewPreferencesService(deniedDocument)
    const modes: string[] = []
    const sections: boolean[] = []
    const modeSubscription = deniedService.watchMode('memory').subscribe((mode) => modes.push(mode))
    const sectionSubscription = deniedService.watchSection('memory', 'records').subscribe((open) => sections.push(open))

    expect(deniedService.get('memory').mode).toBe('basic')
    deniedService.setMode('memory', 'advanced')
    deniedService.setSection('memory', 'records', true)
    deniedService.reset('memory')

    expect(modes).toEqual(['basic', 'advanced', 'basic'])
    expect(sections).toEqual([false, true, false])
    expect(deniedService.get('memory')).toEqual({
      version: 1,
      mode: 'basic',
      openSections: {},
      navigationMode: 'auto',
    })
    expect(deniedService.isSessionOnly('memory')).toBeTrue()
    modeSubscription.unsubscribe()
    sectionSubscription.unsubscribe()
  })

  it('retains the latest state when storage reads, writes, and resets throw', () => {
    const staleValue = JSON.stringify({
      version: 1,
      mode: 'basic',
      openSections: { stale: true },
      navigationMode: 'auto',
    })
    const throwingStorage = {
      getItem: () => staleValue,
      setItem: () => { throw new Error('write denied') },
      removeItem: () => { throw new Error('remove denied') },
    } as unknown as Storage
    const blockedDocument = { defaultView: { localStorage: throwingStorage } } as unknown as Document
    const blockedService = new ModuleViewPreferencesService(blockedDocument)

    blockedService.setMode('memory', 'advanced')
    blockedService.setSection('memory', 'records', true)
    expect(blockedService.get('memory')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { stale: true, records: true },
      navigationMode: 'auto',
    })

    blockedService.reset('memory')
    expect(blockedService.get('memory').mode).toBe('basic')
    expect(blockedService.get('memory').openSections).toEqual({})
    expect(blockedService.isSessionOnly('memory')).toBeTrue()
  })

  it('reports a failed reset as session-only when another instance can still read the old saved view', () => {
    const key = 'hai.module-view.v1.memory'
    const saved = JSON.stringify({
      version: 1,
      mode: 'advanced',
      openSections: { evidence: true },
      navigationMode: 'compact',
    })
    const sharedStorage = {
      getItem: (requestedKey: string) => requestedKey === key ? saved : null,
      setItem: () => undefined,
      removeItem: () => { throw new Error('storage removal denied') },
    } as unknown as Storage
    const sharedDocument = { defaultView: { localStorage: sharedStorage } } as unknown as Document
    const firstInstance = new ModuleViewPreferencesService(sharedDocument)

    expect(firstInstance.get('memory').mode).toBe('advanced')
    expect(firstInstance.reset('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })
    expect(firstInstance.isSessionOnly('memory')).toBeTrue()
    expect(firstInstance.get('memory').mode).toBe('basic')

    const afterReload = new ModuleViewPreferencesService(sharedDocument)
    expect(afterReload.isSessionOnly('memory')).toBeFalse()
    expect(afterReload.get('memory')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { evidence: true },
      navigationMode: 'compact',
    })
  })

  it('resets one module without touching another', () => {
    service.setMode('memory', 'advanced')
    service.setSection('memory', 'records', true)
    service.setNavigationMode('compact')
    service.setMode('workflow-engine', 'advanced')
    service.setSection('workflow-engine', 'runs', true)

    expect(service.reset('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })
    expect(service.get('memory')).toEqual({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' })
    expect(service.getNavigationMode()).toBe('compact')
    expect(service.get('workflow-engine')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { runs: true },
      navigationMode: 'auto',
    })
  })

  it('migrates the previous module navigation preference to one shared desktop rail setting', () => {
    localStorage.setItem('hai.module-view.v1.memory', JSON.stringify({
      version: 1,
      mode: 'advanced',
      openSections: { sources: true },
      navigationMode: 'compact',
    }))

    expect(service.getNavigationMode('memory')).toBe('compact')
    expect(service.getNavigationMode('workflow-engine')).toBe('compact')
    expect(JSON.parse(localStorage.getItem('hai.shell-navigation.v1') ?? '{}')).toEqual({ version: 1, mode: 'compact' })
    expect(service.get('memory').mode).toBe('advanced')
    expect(service.get('workflow-engine').mode).toBe('basic')
  })
})

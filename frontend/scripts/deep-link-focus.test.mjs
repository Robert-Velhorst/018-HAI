import assert from 'node:assert/strict'
import { test } from 'node:test'
import { loadComponentLogic } from './load-component-logic.mjs'

const { AppShellComponent } = await loadComponentLogic(new URL('../src/app/control-room/app-shell.component.ts', import.meta.url), {
  './module-registry': new URL('../src/app/control-room/module-registry.ts', import.meta.url).href,
})

// DOM/event/timer fixtures exercise the real shell logic, not a browser or rendered Angular app.
function fixture() {
  const saved = new Map(['window', 'document', 'MutationObserver', 'HTMLDetailsElement'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]))
  const timers = new Map()
  let sequence = 0
  const listeners = new Map()
  const observers = []
  const elements = new Map()
  const document = {
    hidden: false, activeElement: null,
    body: { classList: { toggle() {}, remove() {} } },
    addEventListener: (name, callback) => listeners.set(name, callback),
    removeEventListener: (name, callback) => { if (listeners.get(name) === callback) listeners.delete(name) },
    getElementById: id => elements.get(id) || null,
    querySelector: () => ({ contains: element => [...elements.values()].includes(element), querySelectorAll: () => [] }),
    querySelectorAll: () => [],
  }
  const window = {
    setTimeout: (callback, delay = 0) => { const id = ++sequence; timers.set(id, { callback, delay }); return id },
    clearTimeout: id => timers.delete(id),
  }
  class Observer {
    constructor(callback) { this.callback = callback; observers.push(this) }
    observe(target, options) { this.connected = true; this.options = options }
    disconnect() { this.connected = false }
  }
  Object.assign(globalThis, { document, window, MutationObserver: Observer, HTMLDetailsElement: class {} })
  const preferences = { get: () => ({ mode: 'basic', openSections: {} }), setMode() {}, setSection() {},
    reset: () => ({ mode: 'basic', openSections: {} }), isSessionOnly: () => false }
  const component = new AppShellComponent({ url: '/hai-os#system-metrics' }, preferences, {}, {})
  const addSection = (id = 'system-metrics', ready = true) => {
    const trigger = { isConnected: true, closest: () => null, matches: () => false,
      getAttribute: () => 'true', getClientRects: () => [1],
      focus: () => { document.activeElement = trigger }, tagName: 'BUTTON' }
    const section = { id, tagName: 'HAI-PROGRESSIVE-SECTION', isConnected: true, ready,
      querySelector: () => section.ready ? trigger : null,
      scrollIntoView: () => { section.scrolled = true },
      closest: () => null, matches: () => false, getClientRects: () => [1],
      focus: () => { document.activeElement = section } }
    elements.set(id, section)
    return { section, trigger }
  }
  return {
    component, document, timers, listeners, observers, addSection,
    tick: (delay = 0) => {
      for (const [id, timer] of [...timers]) if (timer.delay === delay) { timers.delete(id); timer.callback() }
    },
    mutate: (attribute) => {
      for (const observer of observers) {
        if (observer.connected && (!attribute || observer.options.attributeFilter.includes(attribute))) observer.callback([])
      }
    },
    cleanup: () => {
      component.ngOnDestroy()
      for (const [key, descriptor] of saved) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor)
        else delete globalThis[key]
      }
    },
  }
}

test('late disclosure receives focus once its actual trigger is available', () => {
  const f = fixture()
  try {
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    const { section, trigger } = f.addSection('system-metrics', false)
    f.mutate()
    f.tick()
    assert.notEqual(f.document.activeElement, section)
    section.ready = true
    f.mutate()
    f.tick()
    assert.equal(f.document.activeElement, trigger)
    assert.equal(section.scrolled, true)
    assert.equal(f.observers.some(observer => observer.connected), false)
    assert.equal(f.timers.size, 0)
  } finally { f.cleanup() }
})

test('trusted user interaction cancels delayed restoration without moving focus', () => {
  for (const type of ['keydown', 'pointerdown', 'wheel']) {
    const f = fixture()
    try {
      f.component.updateCurrent('/hai-os#system-metrics')
      f.tick()
      assert.equal(f.observers.some(observer => observer.connected), true)
      f.document.activeElement = { userControl: true }
      f.listeners.get(type)?.({ type, isTrusted: true })
      f.addSection()
      f.mutate()
      f.tick()
      assert.equal(f.document.activeElement.userControl, true)
      assert.equal(f.observers.some(observer => observer.connected), false)
      assert.equal(f.timers.size, 0)
    } finally { f.cleanup() }
  }
})

test('route changes invalidate pending focus even when old DOM appears later', () => {
  const f = fixture()
  try {
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    assert.equal(f.observers.some(observer => observer.connected), true)
    f.component.updateCurrent('/memory')
    const { trigger } = f.addSection()
    f.mutate()
    f.tick()
    assert.notEqual(f.document.activeElement, trigger)
    assert.equal(f.observers.some(observer => observer.connected), false)
  } finally { f.cleanup() }
})

test('missing target expires and destroy releases pending observer and timers', () => {
  const f = fixture()
  try {
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    assert.equal(f.observers.some(observer => observer.connected), true)
    f.tick(30_000)
    assert.equal(f.observers.some(observer => observer.connected), false)
    assert.equal(f.timers.size, 0)
    f.component.updateCurrent('/hai-os#system-metrics')
    f.component.ngOnDestroy()
    assert.equal(f.observers.some(observer => observer.connected), false)
    assert.equal(f.timers.size, 0)
  } finally { f.cleanup() }
})

test('a section completing once cannot steal focus again on later record updates', () => {
  const f = fixture()
  try {
    const { trigger } = f.addSection()
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    assert.equal(f.document.activeElement, trigger)
    f.document.activeElement = { userControl: true }
    f.mutate()
    f.tick()
    assert.equal(f.document.activeElement.userControl, true)
  } finally { f.cleanup() }
})

test('programmatic disclosure events do not cancel the requested restoration', () => {
  const f = fixture()
  try {
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    f.listeners.get('pointerdown')({ isTrusted: false })
    assert.equal(f.observers.some(observer => observer.connected), true)
    const { trigger } = f.addSection()
    f.mutate()
    f.tick()
    assert.equal(f.document.activeElement, trigger)
  } finally { f.cleanup() }
})

test('resetting the module cancels pending focus and releases input listeners', () => {
  const f = fixture()
  try {
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    f.component.resetCurrentModuleView()
    assert.equal(f.observers.some(observer => observer.connected), false)
    assert.equal(f.timers.size, 0)
    assert.equal(f.listeners.size, 0)
    const { trigger } = f.addSection()
    f.mutate()
    f.tick()
    assert.notEqual(f.document.activeElement, trigger)
  } finally { f.cleanup() }
})

test('a non-rendered focus target remains pending rather than scrolling a hidden panel', () => {
  const f = fixture()
  try {
    const { section, trigger } = f.addSection()
    trigger.getClientRects = () => []
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    assert.equal(f.document.activeElement, null)
    assert.equal(section.scrolled, undefined)
    assert.equal(f.observers.some(observer => observer.connected), true)
    trigger.getClientRects = () => [1]
    f.mutate()
    f.tick()
    assert.equal(f.document.activeElement, trigger)
  } finally { f.cleanup() }
})

test('returning to a visible tab retries pending focus without requiring a DOM mutation', () => {
  const f = fixture()
  try {
    f.component.refreshSafetyStatus = () => {}
    f.document.hidden = true
    const { section, trigger } = f.addSection()
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    assert.equal(f.document.activeElement, null)
    assert.equal(section.scrolled, undefined)
    f.document.hidden = false
    f.component.onVisibilityChange()
    f.tick()
    assert.equal(f.document.activeElement, trigger)
    assert.equal(section.scrolled, true)
    assert.equal(f.observers.some(observer => observer.connected), false)
  } finally { f.cleanup() }
})

test('class and style visibility changes retry focus through the observed attribute filter', () => {
  for (const attribute of ['class', 'style']) {
    const f = fixture()
    try {
      const { trigger } = f.addSection()
      trigger.getClientRects = () => []
      f.component.updateCurrent('/hai-os#system-metrics')
      f.tick()
      assert.equal(f.document.activeElement, null)
      trigger.getClientRects = () => [1]
      f.mutate(attribute)
      f.tick()
      assert.equal(f.document.activeElement, trigger)
      assert.equal(f.observers.some(observer => observer.connected), false)
    } finally { f.cleanup() }
  }
})

test('visibility recovery cannot revive restoration cancelled by user interaction', () => {
  const f = fixture()
  try {
    f.component.refreshSafetyStatus = () => {}
    f.document.hidden = true
    f.component.updateCurrent('/hai-os#system-metrics')
    f.tick()
    f.document.activeElement = { userControl: true }
    f.listeners.get('keydown')({ isTrusted: true })
    f.document.hidden = false
    const { section } = f.addSection()
    f.component.onVisibilityChange()
    f.mutate('class')
    f.tick()
    assert.equal(f.document.activeElement.userControl, true)
    assert.equal(section.scrolled, undefined)
    assert.equal(f.observers.some(observer => observer.connected), false)
    assert.equal(f.timers.size, 0)
  } finally { f.cleanup() }
})

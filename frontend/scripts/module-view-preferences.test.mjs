import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { loadComponentLogic } from './load-component-logic.mjs'

const require = createRequire(import.meta.url)
const jasmineCore = require('jasmine-core')
const { ModuleViewPreferencesService } = await loadComponentLogic(new URL('../src/app/control-room/module-view-preferences.service.ts', import.meta.url))

test('existing module preference specifications pass with actual service logic and synthetic storage', async t => {
  const jasmine = jasmineCore.core(jasmineCore)
  const env = jasmine.getEnv()
  env.configure({ random: false })
  const api = jasmineCore.interface(jasmine, env)
  const entries = new Map()
  const storage = {
    get length() { return entries.size },
    key(index) { return [...entries.keys()][index] ?? null },
    getItem(key) { return entries.get(String(key)) ?? null },
    setItem(key, value) { entries.set(String(key), String(value)) },
    removeItem(key) { entries.delete(String(key)) },
    clear() { entries.clear() },
  }
  const globals = { ...api, document: { defaultView: { localStorage: storage } }, localStorage: storage, __haiNativePreferencesClass: ModuleViewPreferencesService }
  const previous = new Map(Object.keys(globals).map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]))
  const outcomes = []
  let completion
  env.addReporter({ specDone: result => outcomes.push(result), jasmineDone: result => { completion = result } })
  try {
    for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { value, configurable: true, writable: true })
    await loadComponentLogic(new URL('../src/app/control-room/module-view-preferences.service.spec.ts', import.meta.url), {
      './module-view-preferences.service': 'data:text/javascript,export const ModuleViewPreferencesService = globalThis.__haiNativePreferencesClass',
    })
    await env.execute()
    assert.ok(outcomes.length >= 29, 'Expected every existing preference specification to execute')
    assert.equal(completion?.overallStatus, 'passed', JSON.stringify(completion?.failedExpectations))
    assert.deepEqual(outcomes.filter(result => result.status !== 'passed').map(result => ({ name: result.fullName, status: result.status, failures: result.failedExpectations })), [])
    t.diagnostic(`Executed ${outcomes.length} existing Jasmine preference specifications; no browser or server used.`)
  } finally {
    env.clearReporters()
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else delete globalThis[key]
    }
  }
})

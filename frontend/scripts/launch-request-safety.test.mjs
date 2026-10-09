import assert from 'node:assert/strict'
import { test } from 'node:test'
import { of, firstValueFrom, Subject } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const { AutomationsService } = await loadComponentLogic(new URL('../src/app/services/automations/automations.service.ts', import.meta.url), {
  '../../control-room/launch-recovery': new URL('../src/app/control-room/launch-recovery.ts', import.meta.url).href,
})

test('launch keys survive missing, unknown, nonterminal and mismatched results', async () => {
  const previousWindow = globalThis.window
  try {
    for (const status of [undefined, '', 'unknown', 'running', 'waiting', 'needs_approval', 'failed', 'completed-wrong-automation', 'completed-missing-event', 'completed-requires-approval']) {
      const stored = new Map()
      globalThis.window = { localStorage: { getItem: key => stored.get(key) ?? null, setItem: (key, value) => stored.set(key, value), removeItem: key => stored.delete(key) } }
      const id = '11111111-1111-4111-8111-111111111111'
      const result = { automationId: id, launchEventId: '22222222-2222-4222-8222-222222222222', status, requiresApproval: false }
      if (status?.startsWith('completed-')) result.status = 'completed'
      if (status === 'completed-wrong-automation') result.automationId = '33333333-3333-4333-8333-333333333333'
      if (status === 'completed-missing-event') delete result.launchEventId
      if (status === 'completed-requires-approval') result.requiresApproval = true
      const requests = []
      const http = { post: (_url, _body, options) => { requests.push(options.headers['Idempotency-Key']); return of(result) } }
      await firstValueFrom(new AutomationsService(http).launchAutomation(id))
      const key = requests[0]
      assert.equal(stored.get(`hai.automation-launch.idempotency.v1.${id}`), key, `lost pending key for ${status}`)
      await firstValueFrom(new AutomationsService(http).launchAutomation(id))
      assert.equal(requests[1], key, `new service generated another attempt for ${status}`)
    }
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

test('confirmed ready/completed results rotate the key without clearing a newer pending request', async () => {
  const previousWindow = globalThis.window
  try {
    const stored = new Map()
    globalThis.window = { localStorage: { getItem: key => stored.get(key) ?? null, setItem: (key, value) => stored.set(key, value), removeItem: key => stored.delete(key) } }
    const id = '11111111-1111-4111-8111-111111111111'
    const storageKey = `hai.automation-launch.idempotency.v1.${id}`
    const reference = '22222222-2222-4222-8222-222222222222'
    const keys = []
    for (const status of ['ready', 'completed']) {
      const http = { post: (_url, _body, options) => { keys.push(options.headers['Idempotency-Key']); return of({ automationId: id, launchEventId: reference, status, requiresApproval: false }) } }
      await firstValueFrom(new AutomationsService(http).launchAutomation(id))
      assert.equal(stored.has(storageKey), false)
    }
    assert.notEqual(keys[0], keys[1])
    const response = new Subject()
    const pending = firstValueFrom(new AutomationsService({ post: () => response }).launchAutomation(id))
    const replacement = '33333333-3333-4333-8333-333333333333'
    stored.set(storageKey, replacement)
    response.next({ automationId: id, launchEventId: reference, status: 'completed', requiresApproval: false })
    response.complete()
    await pending
    assert.equal(stored.get(storageKey), replacement)
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

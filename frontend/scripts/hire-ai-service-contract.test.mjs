import assert from 'node:assert/strict'
import { test } from 'node:test'
import '@angular/compiler'
import { defer, firstValueFrom, of, throwError } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const { HireAIService } = await loadComponentLogic(new URL('../src/app/services/hire-ai.service.ts', import.meta.url))

test('HireAI status remains a read without client-side connector credentials', async () => {
  const calls = []
  const status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
  const service = new HireAIService({ get: (...args) => { calls.push(args); return of(status) } })
  assert.equal(await firstValueFrom(service.status()), status)
  assert.deepEqual(calls, [['/api/v1/hire-ai/status']])
})

test('HireAI retains the source-copy maximum-page optimization and continuation cursor', async () => {
  const calls = []
  const service = new HireAIService({ post: (...args) => { calls.push(args); return of({ jobs: [] }) } })
  const filters = { query: 'support', location: 'Europe' }
  const cursor = { createdAt: '2026-10-18T00:00:00Z', id: 42 }
  await firstValueFrom(service.search(filters))
  await firstValueFrom(service.search(filters, cursor))
  assert.deepEqual(calls, [
    ['/api/v1/hire-ai/jobs/search', { filters, limit: 25 }],
    ['/api/v1/hire-ai/jobs/search', { filters, limit: 25, cursor }],
  ])
  assert.deepEqual(filters, { query: 'support', location: 'Europe' })
  assert.deepEqual(cursor, { createdAt: '2026-10-18T00:00:00Z', id: 42 })
})

test('HireAI search errors do not silently replay or advance the cursor', async () => {
  let subscriptions = 0
  const failure = new Error('synthetic transport failure')
  const service = new HireAIService({ post: () => defer(() => {
    subscriptions++
    return throwError(() => failure)
  }) })
  const cursor = { createdAt: '2026-10-18T00:00:00Z', id: 42 }
  await assert.rejects(firstValueFrom(service.search({ query: 'support' }, cursor)), error => error === failure)
  assert.equal(subscriptions, 1)
  assert.deepEqual(cursor, { createdAt: '2026-10-18T00:00:00Z', id: 42 })
})

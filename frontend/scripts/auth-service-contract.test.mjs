import assert from 'node:assert/strict'
import { test } from 'node:test'
import '@angular/compiler'
import { HttpErrorResponse } from '@angular/common/http'
import { defer, firstValueFrom, of, Subject, throwError, TimeoutError } from 'rxjs'
import { TestScheduler } from 'rxjs/testing'
import { HttpTimeoutPolicy } from '../src/app/shared/http-timeout-policy.ts'
import { loadComponentLogic } from './load-component-logic.mjs'

const { AuthService } = await loadComponentLogic(new URL('../src/app/services/auth/auth.service.ts', import.meta.url), {
  '../../shared/http-timeout-policy': new URL('../src/app/shared/http-timeout-policy.ts', import.meta.url).href,
})

test('authentication writes preserve explicit preferences and do not use GET or automatic retries', async () => {
  const requests = []
  const service = new AuthService({
    post: (url, body) => { requests.push({ url, body }); return of(null) },
    get: () => assert.fail('authentication mutation used GET'),
  })
  await firstValueFrom(service.login('operator@example.test', 'fixture-password', false))
  await firstValueFrom(service.login('operator@example.test', 'fixture-password'))
  await firstValueFrom(service.register('operator@example.test', 'fixture-password'))
  await firstValueFrom(service.openLocalPreview())
  await firstValueFrom(service.logout())
  assert.deepEqual(requests, [
    { url: '/api/v1/auth/login', body: { email: 'operator@example.test', password: 'fixture-password', rememberMe: false } },
    { url: '/api/v1/auth/login', body: { email: 'operator@example.test', password: 'fixture-password', rememberMe: true } },
    { url: '/api/v1/auth/register', body: { email: 'operator@example.test', password: 'fixture-password' } },
    { url: '/api/v1/auth/local-preview', body: {} },
    { url: '/api/v1/auth/logout', body: {} },
  ])
})

test('password reset encodes the whole email and never places its bearer or new password in the URL', async () => {
  const requests = []
  const service = new AuthService({ post: (url, body, options) => {
    requests.push({ url, body, options }); return of(null)
  } })
  const email = 'operator+tag&scope=owner@example.test'
  await firstValueFrom(service.requestPasswordReset(email))
  await firstValueFrom(service.confirmPasswordReset('fixture-reset-bearer', 'fixture-new-password'))
  assert.equal(requests.length, 2)
  assert.equal(requests[0].url, '/api/v1/auth/request-password-reset')
  assert.deepEqual([...new URLSearchParams(requests[0].body)], [['email', email]])
  assert.equal(requests[0].options.headers.get('Content-Type'), 'application/x-www-form-urlencoded')
  assert.deepEqual(requests[1].body, { token: 'fixture-reset-bearer', newPassword: 'fixture-new-password' })
  assert.equal(requests[1].url, '/api/v1/auth/confirm-password-reset')
  for (const request of requests) assert.doesNotMatch(request.url, /fixture-reset-bearer|fixture-new-password|scope=owner/)
})

test('uncertain authentication mutations propagate failure without resubmission', async () => {
  for (const invoke of [
    service => service.login('operator@example.test', 'fixture-password'),
    service => service.register('operator@example.test', 'fixture-password'),
    service => service.openLocalPreview(),
    service => service.logout(),
    service => service.requestPasswordReset('operator@example.test'),
    service => service.confirmPasswordReset('fixture-reset-bearer', 'fixture-new-password'),
  ]) {
    let requests = 0, subscriptions = 0
    const failure = new HttpErrorResponse({ status: 503 })
    const service = new AuthService({ post: () => {
      requests++
      return defer(() => { subscriptions++; throw failure })
    } })
    await assert.rejects(firstValueFrom(invoke(service)), error => error === failure)
    assert.equal(requests, 1)
    assert.equal(subscriptions, 1, 'mutation was silently retried after an uncertain failure')
  }
})

test('session checks distinguish unavailable authority from logout without logging sensitive failures', async () => {
  const logged = [], originalError = console.error
  console.error = (...values) => logged.push(values)
  try {
    for (const status of [401, 403, 429, 500, 503]) {
      let observations = 0
      const failure = new HttpErrorResponse({ status, error: { privateDetail: 'fixture-private-auth-detail' } })
      const service = new AuthService({ get: url => {
        assert.equal(url, '/api/v1/auth/is-user-authenticated')
        return throwError(() => { observations++; return failure })
      } })
      if (status === 401) assert.equal(await firstValueFrom(service.loggedIn()), false)
      else await assert.rejects(firstValueFrom(service.loggedIn()), error => error === failure)
      assert.equal(observations, 1, 'failed session check was retried')
    }
    assert.deepEqual(logged, [])
  } finally { console.error = originalError }
})

test('real auth read pipelines cancel stalled requests at their own bounded deadlines', () => {
  const scheduler = new TestScheduler((actual, expected) => assert.deepEqual(actual, expected))
  scheduler.run(() => {
    const capabilities = new Subject(), session = new Subject(), failures = []
    const service = new AuthService({ get: url => url.endsWith('/capabilities') ? capabilities : session })
    service.getCapabilities().subscribe({ error: error => failures.push(error) })
    service.loggedIn().subscribe({ error: error => failures.push(error) })
    assert.equal(AuthService.authenticationCheckTimeoutMs, HttpTimeoutPolicy.readMs)
    scheduler.schedule(() => {
      assert.equal(capabilities.observed, false)
      assert.ok(failures[0] instanceof TimeoutError)
    }, AuthService.capabilitiesTimeoutMs + 1)
    scheduler.schedule(() => {
      assert.equal(session.observed, false)
      assert.equal(failures.length, 2)
      assert.ok(failures.every(error => error instanceof TimeoutError))
    }, Math.max(AuthService.authenticationCheckTimeoutMs, AuthService.capabilitiesTimeoutMs) + 1)
  })
})

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import '@angular/compiler'
import { FormBuilder } from '@angular/forms'
import { EMPTY, Subject, of, throwError } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const require = createRequire(import.meta.url)
const { PursuitsComponent } = await loadComponentLogic(new URL('../src/app/pages/pursuits/pursuits.component.ts', import.meta.url), {
  '@angular/forms': pathToFileURL(require.resolve('@angular/forms')).href,
})
const automation = { id: 'automation-1', launchType: 'agent_runtime', runtimeType: 'openclaw' }
const receipt = { runtimeId: 'openclaw', taskId: 'task-1', status: 'stopped', evidenceUri: 'automation-launch://11111111-1111-4111-8111-111111111111', message: 'private-provider-message' }
function fixture(response) {
  const notices = []
  let calls = 0
  const notification = Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
  const component = new PursuitsComponent(new FormBuilder(), {}, { stopRuntimeTask: () => { calls++; return response } }, {}, notification, {}, {}, {}, {})
  component.reloadSelectedAfterDecision = () => {}
  return { component, notices, calls: () => calls }
}

test('stop presentation distinguishes requested, blocked, failed and uncertain results', () => {
  for (const [status, kind] of [['cancellation_requested', 'info'], ['stopping', 'info'], ['blocked', 'warning'], ['failed', 'error'], ['indeterminate', 'warning'], ['unknown', 'warning'], ['stopped', 'success']]) {
    const f = fixture(of({ ...receipt, status }))
    f.component.stopRuntimeAutomation(automation)
    assert.equal(f.notices[0]?.kind, kind, status)
    assert.doesNotMatch(f.notices[0].args.join(' '), /private-provider-message/)
    assert.equal(f.component.stoppingAutomationId, '')
  }
})

test('malformed and unbound stop responses never imply completion', () => {
  for (const result of [null, {}, { ...receipt, runtimeId: 'hermes' }, { ...receipt, taskId: '' }, { ...receipt, evidenceUri: '<script>private</script>' }, { ...receipt, evidenceUri: 'automation-launch://00000000-0000-0000-0000-000000000000' }]) {
    const f = fixture(of(result))
    f.component.stopRuntimeAutomation(automation)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.doesNotMatch(f.notices[0].args.join(' '), /private|<script>/)
  }
})

test('stop prevents duplicate submission and releases busy state on empty completion', () => {
  const response = new Subject()
  const f = fixture(response)
  f.component.stopRuntimeAutomation(automation)
  f.component.stopRuntimeAutomation(automation)
  assert.equal(f.calls(), 1)
  response.complete()
  assert.equal(f.component.stoppingAutomationId, '')
  assert.equal(f.notices[0]?.kind, 'warning')
  const empty = fixture(EMPTY)
  empty.component.stopRuntimeAutomation(automation)
  assert.equal(empty.component.stoppingAutomationId, '')
})

test('stop subscription ends on route destruction and suppresses raw errors', () => {
  const response = new Subject()
  const f = fixture(response)
  f.component.stopRuntimeAutomation(automation)
  f.component.ngOnDestroy()
  response.next(receipt)
  assert.equal(f.notices.length, 0)
  assert.equal(f.component.stoppingAutomationId, '')
  const failed = fixture(throwError(() => ({ error: { message: 'private-provider-token' } })))
  failed.component.stopRuntimeAutomation(automation)
  assert.equal(failed.notices[0]?.kind, 'error')
  assert.doesNotMatch(failed.notices[0].args.join(' '), /private-provider-token/)
})

test('uncertain stop errors expose only a matching candidate recovery reference', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const recovery = { automationId: automation.id, candidateStopEventId: id, candidateEvidenceUri: `automation-launch://${id}`, reportedStatus: 'indeterminate', reconciliationRequired: true, retryAllowed: false }
  for (const mode of ['valid', 'foreign', 'invalid_uri', 'wrong_event', 'retry_allowed', 'not_required']) {
    const candidate = { ...recovery }
    if (mode === 'foreign') candidate.automationId = 'foreign-automation'
    if (mode === 'invalid_uri') candidate.candidateEvidenceUri = 'https://foreign-private.example/receipt'
    if (mode === 'wrong_event') candidate.candidateStopEventId = '22222222-2222-4222-8222-222222222222'
    if (mode === 'retry_allowed') candidate.retryAllowed = true
    if (mode === 'not_required') candidate.reconciliationRequired = false
    const f = fixture(throwError(() => ({ error: { recovery: candidate, message: 'private-provider-token' } })))
    let reloads = 0
    f.component.reloadSelectedAfterDecision = () => { reloads++ }
    f.component.stopRuntimeAutomation(automation)
    assert.equal(f.notices[0].kind, 'error')
    const text = f.notices[0].args.join(' ')
    assert.doesNotMatch(text, /private-provider|foreign-private/)
    if (mode === 'valid') {
      assert.ok(text.includes(recovery.candidateEvidenceUri))
      assert.match(text, /candidate|unconfirmed/i)
      assert.equal(reloads, 1)
    } else {
      assert.ok(!text.includes('automation-launch://'), mode)
      assert.equal(reloads, 0)
    }
    assert.equal(f.calls(), 1)
  }
})

test('stop recovery review opens only the exact stop record in its pursuit', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const recovery = { automationId: automation.id, candidateStopEventId: id, candidateEvidenceUri: `automation-launch://${id}`, reconciliationRequired: true, retryAllowed: false }
  for (const mode of ['valid', 'intent', 'missing', 'foreign_record', 'wrong_runtime', 'non_stop', 'different_pursuit', 'unlinked_automation']) {
    const f = fixture(throwError(() => ({ error: { recovery } })))
    const attempt = { id, automationId: automation.id, launchType: 'agent_runtime_stop', runtimeType: 'openclaw', status: 'indeterminate' }
    f.component.selected = { pursuit: { id: 'pursuit-1' }, automations: [automation], runtimeAttempts: [attempt] }
    f.component.stopRuntimeAutomation(automation)
    assert.ok(f.component.visibleRuntimeStopRecovery, mode)
    if (mode === 'intent') attempt.launchType = 'agent_runtime_stop_intent'
    if (mode === 'missing') f.component.selected.runtimeAttempts = []
    if (mode === 'foreign_record') attempt.automationId = 'foreign'
    if (mode === 'wrong_runtime') attempt.runtimeType = 'hermes'
    if (mode === 'non_stop') attempt.launchType = 'agent_runtime'
    if (mode === 'different_pursuit') f.component.selected.pursuit.id = 'pursuit-2'
    if (mode === 'unlinked_automation') f.component.selected.automations = []
    f.component.reviewRuntimeStopRecovery()
    assert.equal(f.calls(), 1, 'review must never repeat cancellation')
    if (mode === 'valid' || mode === 'intent') assert.equal(f.component.inspectedRuntimeEvidence, attempt)
    else assert.equal(f.component.inspectedRuntimeEvidence, undefined)
    if (mode === 'different_pursuit' || mode === 'unlinked_automation') assert.equal(f.component.visibleRuntimeStopRecovery, undefined)
  }
})

test('delayed stop recovery stays bound to the pursuit that initiated cancellation', () => {
  const response = new Subject()
  const f = fixture(response)
  const id = '11111111-1111-4111-8111-111111111111'
  const original = { pursuit: { id: 'pursuit-1' }, automations: [automation], runtimeAttempts: [] }
  f.component.selected = original
  f.component.stopRuntimeAutomation(automation)
  f.component.selected = { pursuit: { id: 'pursuit-2' }, automations: [automation], runtimeAttempts: [] }
  response.error({ error: { recovery: { automationId: automation.id, candidateStopEventId: id,
    candidateEvidenceUri: `automation-launch://${id}`, reconciliationRequired: true, retryAllowed: false } } })
  assert.equal(f.component.visibleRuntimeStopRecovery, undefined)
  f.component.selected = original
  assert.equal(f.component.visibleRuntimeStopRecovery?.pursuitId, 'pursuit-1')
  assert.equal(f.component.stoppingAutomationId, '')
  assert.equal(f.calls(), 1)
})

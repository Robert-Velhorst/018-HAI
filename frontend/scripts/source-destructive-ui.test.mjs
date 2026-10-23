import '@angular/compiler'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { of, EMPTY, Subject } from 'rxjs'
import { FormBuilder } from '@angular/forms'
import { loadComponentLogic } from './load-component-logic.mjs'

const { ConnectedSourcesComponent } = await loadComponentLogic(new URL('../src/app/pages/connected-sources/connected-sources.component.ts', import.meta.url), {
  '@angular/forms': import.meta.resolve('@angular/forms'),
  '../../services/connected-source/connected-source.service.token': 'data:text/javascript,export const CONNECTED_SOURCE_SERVICE_TOKEN = Symbol("source-test")',
  '../../control-room/progressive-section.component': 'data:text/javascript,export class HaiProgressiveSectionComponent {}',
})

function fixture(action, response) {
  const calls = [], notices = [], followups = []
  const service = Object.fromEntries(['revoke', 'deleteExtraction'].map(name => [name, (...args) => {
    calls.push({ name, args })
    return typeof response === 'function' ? response() : response
  }]))
  const notification = Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind =>
    [kind, (...args) => notices.push({ kind, args })]))
  const component = new ConnectedSourcesComponent(new FormBuilder(), service, notification, {}, {}, {}, {})
  component.refresh = () => followups.push('sources')
  component.loadExtractions = () => followups.push('extractions')
  if (action === 'revoke') component.revoke({ id: 'source-1', name: 'Reviewed source', connectorKey: 'trello' })
  else component.delete({ id: 'record-1', summary: 'Reviewed record' })
  component.destructiveApprovalForm.patchValue({
    taskId: 'synthetic-task', approvalSourceId: 'synthetic-review',
    approvalBindingDigest: 'a'.repeat(64), idempotencyKey: 'synthetic-source-request',
  })
  return { component, calls, notices, followups }
}

test('empty destructive responses cannot silently re-enable an unconfirmed mutation', () => {
  for (const action of ['revoke', 'delete']) {
    const f = fixture(action, EMPTY)
    try {
      f.component.submitDestructiveApproval()
      assert.equal(f.component.destructiveSubmitting, false)
      assert.equal(f.component.destructiveCanSubmit(), false)
      assert.match(f.component.destructiveError, /outcome is unknown/i)
      f.component.submitDestructiveApproval()
      assert.equal(f.calls.length, 1)
      assert.equal(f.notices.some(n => n.kind === 'success'), false)
      assert.deepEqual(f.followups, [])
    } finally { f.component.ngOnDestroy() }
  }
})

test('destructive UI consumes one acknowledgement, including a valid void deletion response', () => {
  for (const action of ['revoke', 'delete']) {
    const response = new Subject()
    const f = fixture(action, response)
    try {
      f.component.submitDestructiveApproval()
      response.next(action === 'revoke' ? { id: 'source-1', status: 'revoked' } : undefined)
      assert.equal(response.observed, false)
      response.next(action === 'revoke' ? { id: 'source-1', status: 'revoked' } : undefined)
      assert.equal(f.notices.filter(n => n.kind === 'success').length, 1)
      assert.equal(f.followups.length, 1)
      assert.equal(f.component.destructiveSubmitting, false)
    } finally { f.component.ngOnDestroy() }
  }
})

test('destructive UI teardown stops observation and refuses further dispatch without claiming server cancellation', () => {
  const response = new Subject()
  const f = fixture('revoke', response)
  f.component.submitDestructiveApproval()
  f.component.ngOnDestroy()
  assert.equal(response.observed, false)
  assert.equal(f.component.destructiveCanSubmit(), false)
  f.component.submitDestructiveApproval()
  response.next({ id: 'source-1', status: 'revoked' })
  assert.equal(f.calls.length, 1)
  assert.equal(f.notices.some(n => n.kind === 'success'), false)
  assert.deepEqual(f.followups, [])
})

test('synchronous dispatch failure uses uncertain recovery without leaking its details', () => {
  const f = fixture('revoke', () => { throw new Error('private transport detail') })
  try {
    assert.doesNotThrow(() => f.component.submitDestructiveApproval())
    assert.equal(f.component.destructiveSubmitting, false)
    assert.equal(f.component.destructiveCanSubmit(), false)
    assert.match(f.component.destructiveError, /outcome is unknown/i)
    assert.doesNotMatch(JSON.stringify(f.notices), /private transport detail/)
  } finally { f.component.ngOnDestroy() }
})

test('revocation confirmation still requires the exact source and revoked state', () => {
  for (const response of [{ id: 'different-source', status: 'revoked' }, { id: 'source-1', status: 'active' }, null]) {
    const f = fixture('revoke', of(response))
    try {
      f.component.submitDestructiveApproval()
      assert.equal(f.notices.some(n => n.kind === 'success'), false)
      assert.equal(f.component.destructiveCanSubmit(), false)
      assert.deepEqual(f.followups, [])
    } finally { f.component.ngOnDestroy() }
  }
})

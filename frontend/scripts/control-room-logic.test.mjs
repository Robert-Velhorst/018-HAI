import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import '@angular/compiler'
import { of, Subject, throwError } from 'rxjs'
import { moduleForUrl } from '../src/app/control-room/module-registry.ts'
import { loadComponentLogic } from './load-component-logic.mjs'
import { launchRecoveryNotice } from '../src/app/control-room/launch-recovery.ts'

test('launch error recovery is contextual, non-authoritative and contains no raw payload', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const reference = '22222222-2222-4222-8222-222222222222'
  const recovery = { automationId: id, launchEventId: reference, reconciliationRequired: true, retryAllowed: false, output: 'private-output', reportedStatus: 'completed', message: 'private-message' }
  const notice = launchRecoveryNotice({ error: { recovery } }, id)
  assert.match(notice, /22222222-2222-4222-8222-222222222222/)
  assert.match(notice, /not proof of completion or permission to retry/)
  assert.doesNotMatch(notice, /private/)
  for (const invalid of [null, {}, { ...recovery, automationId: 'other' }, { ...recovery, launchEventId: '<script>' }, { ...recovery, launchEventId: '00000000-0000-0000-0000-000000000000' }, { ...recovery, retryAllowed: true }, { ...recovery, reconciliationRequired: false }]) {
    assert.equal(launchRecoveryNotice({ error: { recovery: invalid } }, id), undefined)
  }
  assert.equal(launchRecoveryNotice(null, id), undefined)
  assert.equal(launchRecoveryNotice({ error: 'private-error' }, id), undefined)
})

// Run the actual class and RxJS pipelines without a browser or template build.
// This is transpilation, not Angular template/type or rendered acceptance.
const { KnowledgeClaimsComponent } = await loadComponentLogic(new URL('../src/app/pages/knowledge-claims/knowledge-claims.component.ts', import.meta.url))
const { AmbientService } = await loadComponentLogic(new URL('../src/app/services/ambient.service.ts', import.meta.url))
const { WorkflowService } = await loadComponentLogic(new URL('../src/app/services/workflow/workflow.service.ts', import.meta.url))
const { FormBuilder } = await import('@angular/forms')
const { WorkflowEngineComponent } = await loadComponentLogic(new URL('../src/app/pages/workflow-engine/workflow-engine.component.ts', import.meta.url), {
  '@angular/forms': import.meta.resolve('@angular/forms'),
  '../../services/workflow/workflow.service.token': 'data:text/javascript,export const WORKFLOW_SERVICE_TOKEN = Symbol("test-workflow")',
})

test('both reminder authorization controls expose the execution review gate without blocking revocation', () => {
  const template = readFileSync(new URL('../src/app/pages/workflow-engine/workflow-engine.component.html', import.meta.url), 'utf8')
  const buttons = template.match(/<button\b[\s\S]*?<\/button>/g) || []
  for (const prefix of ['reminder-authorize-delivery-', 'reminder-drawer-authorize-delivery-']) {
    const matches = buttons.filter(button => button.includes(`'${prefix}'`))
    assert.equal(matches.length, 1, prefix)
    assert.match(matches[0], /\[disabled\]="workerReviewRequired \|\| workflowActionsDisabled\(\)"/)
    assert.match(matches[0], /\[title\]="workerReviewRequired \?/)
    assert.match(matches[0], /complete the required review/)
  }
  for (const prefix of ['reminder-revoke-', 'reminder-drawer-revoke-']) {
    const matches = buttons.filter(button => button.includes(`'${prefix}'`))
    assert.equal(matches.length, 1, prefix)
    assert.match(matches[0], /\[disabled\]="workflowActionsDisabled\(\)"/)
  }
})

test('selected workflow confirmation rechecks context and bounds uncertain execution responses', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'selection', 'state', 'approval', 'unavailable', 'destroy_before', 'duplicate', 'foreign', 'empty', 'error', 'destroy_after', 'review', 'id_changed', 'malformed']) {
    const response = new Subject(); const calls = []; const notices = []; const readbacks = []; let confirm
    const component = new WorkflowEngineComponent(new FormBuilder(), { runOne: value => { calls.push(value); return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), { confirm: options => { confirm = options.nzOnOk } }, {}, {}, { detectChanges: () => {} }, {})
    component.selected = { item: { id, currentState: 'ready', approvalStatus: 'approved', requiresApproval: true } }
    component.actionsUnavailable = () => false
    component.refresh = () => {}
    component.reloadSelectedWorkflow = () => {}
    component.loadWorkflowRecord = workflowId => readbacks.push(workflowId)
    component.overview = { states: ['ready', 'completed', 'blocked'] }
    try {
      component.runSelectedWorkflow()
      assert.equal(typeof confirm, 'function', mode)
      if (mode === 'selection') component.selected = { item: { ...component.selected.item } }
      if (mode === 'state') component.selected.item.currentState = 'blocked'
      if (mode === 'id_changed') component.selected.item.id = '22222222-2222-4222-8222-222222222222'
      if (mode === 'approval') component.selected.item.approvalStatus = 'pending'
      if (mode === 'unavailable') component.actionsUnavailable = () => true
      if (mode === 'destroy_before') component.ngOnDestroy()
      confirm()
      const prevented = ['selection', 'state', 'approval', 'unavailable', 'destroy_before', 'id_changed'].includes(mode)
      assert.equal(calls.length, prevented ? 0 : 1, mode)
      if (prevented) continue
      if (mode === 'duplicate') { confirm(); assert.equal(calls.length, 1) }
      if (mode === 'destroy_after') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next({ workflowId: mode === 'malformed' ? 42 : mode === 'foreign' ? '22222222-2222-4222-8222-222222222222' : id, status: 'completed', state: 'completed', attempts: 0, reviewRequired: mode === 'review', message: 'private-token' })
      confirm(); assert.equal(calls.length, 1, mode)
      assert.equal(component.runningAction, undefined, mode)
      assert.doesNotMatch(JSON.stringify(notices) + JSON.stringify(component.lastOperation), /private-token|result was verified|No other workflow was run/)
      if (['foreign', 'empty', 'error', 'malformed'].includes(mode)) assert.equal(component.transitionReviewId, id, mode)
      if (mode === 'review') {
        assert.equal(component.workerReviewRequired, true, mode)
        assert.deepEqual(readbacks, [id], mode)
      }
    } finally { component.ngOnDestroy() }
  }
})

test('selected workflow cannot bypass an unknown-operation pause, including pending confirmations', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['before', 'during', 'unknown']) {
    const response = new Subject(); let confirm; let writes = 0; let dialogs = 0
    const component = new WorkflowEngineComponent(new FormBuilder(), { runOne: () => { writes++; return response } }, {}, { warning() {}, info() {} }, { confirm: options => { dialogs++; confirm = options.nzOnOk } }, {}, {}, {}, {})
    component.dataLoaded = true
    component.selected = { item: { id, currentState: 'ready', approvalStatus: 'approved', requiresApproval: true } }
    component.overview = { states: ['ready', 'completed'] }
    try {
      if (mode === 'before') component.workerReviewRequired = true
      component.runSelectedWorkflow()
      if (mode === 'before') { assert.equal(dialogs, 0); continue }
      if (mode === 'during') component.workerReviewRequired = true
      confirm()
      assert.equal(writes, mode === 'unknown' ? 1 : 0, mode)
      if (mode === 'unknown') {
        response.complete()
        assert.equal(component.workerReviewRequired, true)
        // Even after per-record readback, the cross-operation uncertainty still needs review.
        component.transitionReviewId = undefined
        component.runSelectedWorkflow()
        assert.equal(dialogs, 1)
        assert.equal(writes, 1)
      }
    } finally { component.ngOnDestroy() }
  }
})

test('reminder history cannot promote contradictory decisions or unbound delivery receipts', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const child = '22222222-2222-4222-8222-222222222222'; const time = '2026-10-02T12:00:00Z'
  const component = new WorkflowEngineComponent(new FormBuilder(), {}, {}, {}, {}, {}, {}, {}, {})
  try {
    for (const mode of ['valid', 'contradiction', 'confirmation', 'foreign', 'malformed_id', 'duplicate', 'stale']) {
      const item = { request: { id, workflowId: child, checklistItemId: child, activationKind: 'internal_notification', checklistStatus: 'open', authority: 'reminder_activation_request_only', confirmation: 'PREPARE INTERNAL REMINDER ONLY', reminderDigest: 'a'.repeat(64), requestDigest: 'b'.repeat(64), recordDigest: 'c'.repeat(64), reminderAt: time, requestedAt: time, expiresAt: time }, current: true, status: mode === 'stale' ? 'stale' : 'approved', canExecute: false, latestDecision: { id: child, activationRequestId: id, activationRequestDigest: 'c'.repeat(64), authority: 'reminder_activation_decision_only', decision: 'approved', confirmation: 'APPROVE INTERNAL REMINDER PREPARATION', requestDigest: 'd'.repeat(64), recordDigest: 'e'.repeat(64), decidedAt: time, expiresAt: new Date(Date.now() + 60_000).toISOString() } }
      if (mode === 'contradiction') item.latestDecision.decision = 'revoked'
      if (mode === 'confirmation') item.latestDecision.confirmation = 'REVOKE INTERNAL REMINDER PREPARATION'
      if (mode === 'foreign') item.latestDecision.activationRequestId = child
      if (mode === 'malformed_id') item.request.id = 42
      const snapshot = { authority: 'reminder_activation_history_only', canExecute: false, checkedAt: time, items: mode === 'duplicate' ? [item, item] : [item] }
      assert.equal(component.validReminderActivationHistory(snapshot), ['valid', 'stale'].includes(mode), mode)
      component.reminderActivationHistory = snapshot
      assert.equal(component.canAuthorizeReminderDelivery({ checklistItemId: child }), mode === 'valid', mode)
    }
    for (const mode of ['valid', 'digest', 'authorization_digest', 'foreign', 'duplicate_id', 'duplicate_sequence', 'malformed_id']) {
      const authorization = { id, activationRequestId: child, activationDecisionId: child, workflowId: child, checklistItemId: child, channel: 'in_app', authority: 'internal_reminder_delivery_authorization', confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER', reminderDigest: 'a'.repeat(64), activationRequestDigest: 'b'.repeat(64), activationDecisionDigest: 'c'.repeat(64), requestDigest: 'd'.repeat(64), recordDigest: 'e'.repeat(64) }
      const attempt = { id: child, authorizationId: mode === 'foreign' ? child : id, attemptNumber: 1, status: 'delivered', authority: 'internal_reminder_delivery_receipt', reminderDigest: mode === 'digest' ? 'f'.repeat(64) : authorization.reminderDigest, authorizationDigest: mode === 'authorization_digest' ? 'f'.repeat(64) : authorization.recordDigest, recordDigest: 'f'.repeat(64) }
      if (mode === 'malformed_id') authorization.id = 42
      const attempts = [attempt]
      if (mode === 'duplicate_id') attempts.push({ ...attempt, attemptNumber: 2 })
      if (mode === 'duplicate_sequence') attempts.push({ ...attempt, id })
      assert.equal(component.validReminderDeliveryHistory({ authority: 'internal_reminder_delivery_receipt', canExecute: false, authorizations: [authorization], attempts }), mode === 'valid', mode)
    }
  } finally { component.ngOnDestroy() }
})

test('reminder decisions snapshot confirmation context, bind receipts and retain safe revocation', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const priorId = '22222222-2222-4222-8222-222222222222'
  for (const mode of ['valid', 'rejected', 'revoked', 'changed', 'pause', 'empty', 'error', 'destroy', 'foreign', 'previous']) {
    const response = new Subject(); let confirm; let writes = 0; let refreshes = 0; const notices = []
    const component = new WorkflowEngineComponent(new FormBuilder(), { decideReminderActivation: () => { writes++; return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), { confirm: options => { confirm = options.nzOnOk } }, {}, {}, {}, {})
    const decision = mode === 'revoked' ? 'revoked' : mode === 'rejected' ? 'rejected' : 'approved'
    const activation = { request: { id, checklistItemId: id, recordDigest: 'a'.repeat(64) }, latestDecision: mode === 'revoked' ? { id: priorId, recordDigest: 'b'.repeat(64) } : undefined, current: true, status: mode === 'revoked' ? 'approved' : 'prepared' }
    component.dataLoaded = true; component.refresh = () => { refreshes++ }; component.reminderActivationHistory = { items: [activation] }
    try {
      if (mode === 'revoked') component.workerReviewRequired = true
      if (mode === 'rejected') component.rejectReminderActivation({ checklistItemId: id }, { stopPropagation() {} })
      else component.reviewReminderActivation({ checklistItemId: id }, { stopPropagation() {} })
      if (mode === 'changed') activation.request.recordDigest = 'f'.repeat(64)
      if (mode === 'pause') component.workerReviewRequired = true
      confirm(); confirm()
      assert.equal(writes, ['changed', 'pause'].includes(mode) ? 0 : 1, mode)
      if (['changed', 'pause'].includes(mode)) continue
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next({ authority: 'reminder_activation_decision_only', canExecute: false, replayed: false, decision: {
        id: priorId, activationRequestId: mode === 'foreign' ? priorId : id, activationRequestDigest: 'a'.repeat(64), decision,
        previousDecisionId: ['revoked', 'previous'].includes(mode) ? priorId : undefined,
        confirmation: { approved: 'APPROVE', rejected: 'REJECT', revoked: 'REVOKE' }[decision] + ' INTERNAL REMINDER PREPARATION',
        authority: 'reminder_activation_decision_only', requestDigest: 'c'.repeat(64), recordDigest: 'd'.repeat(64), reason: 'private-token',
      } })
      assert.equal(component.activationBusyId, undefined, mode)
      assert.equal(refreshes, ['valid', 'rejected', 'revoked'].includes(mode) ? 1 : 0, mode)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
      if (['empty', 'error', 'foreign', 'previous'].includes(mode)) assert.equal(component.workerReviewRequired, true, mode)
    } finally { component.ngOnDestroy() }
  }
})

test('reminder preparation observes an exact bounded receipt and preserves captured source context', () => {
  const workflowId = '11111111-1111-4111-8111-111111111111'; const checklistItemId = '22222222-2222-4222-8222-222222222222'
  for (const mode of ['valid', 'mutated', 'empty', 'error', 'destroy', 'foreign', 'digest', 'paused']) {
    const response = new Subject(); let request; let writes = 0; let refreshes = 0; const notices = []
    const component = new WorkflowEngineComponent(new FormBuilder(), { prepareReminderActivation: (_id, value) => { writes++; request = value; return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, {}, {})
    const proposal = { workflowId, checklistItemId, evidenceDigest: 'a'.repeat(64) }
    component.dataLoaded = true; component.refresh = () => { refreshes++ }
    try {
      if (mode === 'paused') component.workerReviewRequired = true
      component.prepareReminderActivation(proposal, { stopPropagation() {} }); component.prepareReminderActivation(proposal, { stopPropagation() {} })
      assert.equal(writes, mode === 'paused' ? 0 : 1)
      if (mode === 'paused') continue
      if (mode === 'mutated') proposal.evidenceDigest = 'b'.repeat(64)
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next({ authority: 'reminder_activation_request_only', canExecute: false, replayed: false, request: {
        id: workflowId, workflowId: mode === 'foreign' ? checklistItemId : workflowId, checklistItemId,
        activationKind: 'internal_notification', reminderDigest: mode === 'digest' ? 'f'.repeat(64) : 'a'.repeat(64),
        idempotencyKey: request.idempotencyKey, recordDigest: 'c'.repeat(64), requestDigest: 'd'.repeat(64),
        authority: 'reminder_activation_request_only', confirmation: 'PREPARE INTERNAL REMINDER ONLY', reason: 'private-token',
      } })
      assert.equal(component.activationBusyId, undefined, mode)
      assert.equal(refreshes, ['valid', 'mutated'].includes(mode) ? 1 : 0, mode)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
      if (['empty', 'error', 'foreign', 'digest'].includes(mode)) assert.equal(component.workerReviewRequired, true, mode)
    } finally { component.ngOnDestroy() }
  }
})

test('reminder authorization binds immutable approval context and observes only one receipt', () => {
  const ids = ['11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333', '44444444-4444-4444-8444-444444444444']
  for (const mode of ['valid', 'changed', 'pause', 'empty', 'error', 'destroy', 'digest', 'foreign']) {
    const response = new Subject(); const notices = []; let confirm; let writes = 0; let refreshes = 0
    const component = new WorkflowEngineComponent(new FormBuilder(), { authorizeReminderDelivery: () => { writes++; return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), { confirm: options => { confirm = options.nzOnOk } }, {}, {}, {}, {})
    const activation = { request: { id: ids[0], workflowId: ids[2], checklistItemId: ids[3], recordDigest: 'a'.repeat(64), reminderDigest: 'b'.repeat(64) }, latestDecision: { id: ids[1], decision: 'approved', authority: 'reminder_activation_decision_only', confirmation: 'APPROVE INTERNAL REMINDER PREPARATION', activationRequestId: ids[0], activationRequestDigest: 'a'.repeat(64), recordDigest: 'c'.repeat(64), expiresAt: new Date(Date.now() + 60_000).toISOString() }, current: true, status: 'approved' }
    component.dataLoaded = true; component.refresh = () => { refreshes++ }
    component.reminderActivationHistory = { items: [activation] }
    component.reminderDeliveryHistory = { authorizations: [] }
    try {
      component.authorizeReminderDelivery({ checklistItemId: ids[3] }, { stopPropagation() {} })
      if (mode === 'changed') activation.request.recordDigest = 'f'.repeat(64)
      if (mode === 'pause') component.workerReviewRequired = true
      confirm(); confirm()
      assert.equal(writes, ['changed', 'pause'].includes(mode) ? 0 : 1, mode)
      if (['changed', 'pause'].includes(mode)) continue
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next({ authority: 'internal_reminder_delivery_authorization', canExecute: false, deliveryAuthorized: true, replayed: false, authorization: {
        id: ids[0], activationRequestId: ids[0], activationDecisionId: ids[1], workflowId: mode === 'foreign' ? ids[0] : ids[2], checklistItemId: ids[3],
        activationRequestDigest: mode === 'digest' ? 'f'.repeat(64) : 'a'.repeat(64), reminderDigest: 'b'.repeat(64), activationDecisionDigest: 'c'.repeat(64),
        recordDigest: 'd'.repeat(64), requestDigest: 'e'.repeat(64), channel: 'in_app', authority: 'internal_reminder_delivery_authorization',
        confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER', idempotencyKey: `ui:delivery:${ids[0]}:${'c'.repeat(16)}`, reason: 'private-token',
      } })
      assert.equal(component.activationBusyId, undefined, mode)
      assert.equal(refreshes, mode === 'valid' ? 1 : 0, mode)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
      if (['empty', 'error', 'digest', 'foreign'].includes(mode)) assert.equal(component.workerReviewRequired, true, mode)
    } finally { component.ngOnDestroy() }
  }
})

test('internal reminder worker validates bounded summaries and releases uncertain observations', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'zero', 'empty', 'error', 'destroy', 'count', 'duplicate', 'status', 'paused']) {
    const response = new Subject(); const notices = []; let writes = 0; let refreshes = 0
    const component = new WorkflowEngineComponent(new FormBuilder(), { runDueReminderDeliveries: () => { writes++; return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, {}, {})
    component.dataLoaded = true; component.refresh = () => { refreshes++ }
    component.reminderDeliveryHistory = { authorizations: [{ id }] }
    try {
      if (mode === 'paused') component.workerReviewRequired = true
      component.runDueReminderDeliveries(); component.runDueReminderDeliveries()
      assert.equal(writes, mode === 'paused' ? 0 : 1)
      if (mode === 'paused') continue
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else {
        const count = mode === 'zero' ? 0 : mode === 'duplicate' ? 2 : 1
        const result = { authorizationId: id, status: mode === 'status' ? 'unknown' : 'delivered', reason: 'private-token' }
        response.next({ checked: count, delivered: mode === 'count' ? 2 : count, retried: 0, suppressed: 0, deadLettered: 0, expired: 0, results: count === 0 ? [] : count === 2 ? [result, result] : [result] })
      }
      assert.equal(component.runningAction, undefined, mode)
      assert.equal(refreshes, ['valid', 'zero'].includes(mode) ? 1 : 0, mode)
      assert.doesNotMatch(JSON.stringify(notices) + JSON.stringify(component.lastOperation), /private-token/)
      if (['empty', 'error', 'count', 'duplicate', 'status'].includes(mode)) {
        assert.equal(component.workerReviewRequired, true, mode)
        component.runDueReminderDeliveries(); assert.equal(writes, 1)
      }
    } finally { component.ngOnDestroy() }
  }
})

test('batch worker admits bounded consistent summaries and pauses uncertain reruns', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'empty', 'error', 'destroy', 'counts', 'duplicate_id', 'private', 'review']) {
    const response = new Subject(); let writes = 0; let refreshes = 0; const notices = []
    const component = new WorkflowEngineComponent(new FormBuilder(), { runDue: () => { writes++; return response } }, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, { detectChanges: () => {} }, {})
    component.actionsUnavailable = () => false; component.queueCount = () => 1
    component.overview = { states: ['completed', 'ready', 'blocked'] }
    component.refresh = () => { refreshes++ }; component.reloadSelectedWorkflow = () => {}
    try {
      component.runDue(); component.runDue(); assert.equal(writes, 1)
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else {
        const result = { workflowId: id, status: 'completed', state: 'completed', attempts: 0, message: 'private-token', reviewRequired: mode === 'review' }
        response.next({ checked: mode === 'duplicate_id' ? 2 : 1, completed: mode === 'counts' ? 5 : mode === 'duplicate_id' ? 2 : 1, retried: 0, blocked: 0, skipped: 0, results: mode === 'duplicate_id' ? [result, result] : [result] })
      }
      assert.equal(component.runningAction, undefined, mode)
      assert.doesNotMatch(JSON.stringify(notices) + JSON.stringify(component.lastOperation) + JSON.stringify(component.runSummary), /private-token/)
      if (['empty', 'error', 'counts', 'duplicate_id', 'review'].includes(mode)) {
        assert.equal(component.workerReviewRequired, true, mode)
        component.runDue(); assert.equal(writes, 1, mode)
        assert.equal(refreshes, 0, mode)
      }
    } finally { component.ngOnDestroy() }
  }
})

test('worker review acknowledgement requires fresh data and rechecks pending execution at confirmation', () => {
  let confirm; let dialogs = 0
  const component = new WorkflowEngineComponent(new FormBuilder(), {}, {}, {}, { confirm: options => { dialogs++; confirm = options.nzOnOk } }, {}, {}, {}, {})
  component.workerReviewRequired = true; component.workerReviewMessage = 'Review outcome'
  component.dataLoaded = true; component.items = []
  try {
    component.acknowledgeWorkerReview(); assert.equal(dialogs, 0)
    component.workerReviewRefreshed = true
    component.items = [{ currentState: 'in_progress' }]
    component.acknowledgeWorkerReview(); assert.equal(dialogs, 0)
    component.items = []; component.acknowledgeWorkerReview(); assert.equal(dialogs, 1)
    component.loading = true; confirm(); assert.equal(component.workerReviewRequired, true)
    component.loading = false; component.items = [{ currentState: 'in_progress' }]
    confirm(); assert.equal(component.workerReviewRequired, true)
    component.items = []; confirm(); assert.equal(component.workerReviewRequired, false)
    assert.equal(component.workerReviewMessage, undefined)
  } finally { component.ngOnDestroy() }
})

test('workflow refresh requires valid panels, completes empty reads and preserves prior records', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const item = { id, title: 'Work', currentState: 'ready', requiresApproval: false, approvalStatus: 'not_required', archived: false }
  const states = ['new_input', 'classified', 'linked', 'checklist_generated', 'waiting_external_input', 'needs_approval', 'ready', 'in_progress', 'completed', 'archived', 'blocked']
  for (const mode of ['valid', 'empty', 'destroy', 'items', 'states', 'counts', 'duplicate', 'approval', 'missing_boolean']) {
    const pending = new Subject(); const notices = []
    const overview = { states, capabilities: [], safetyRules: [], rules: [] }
    const dashboard = { counts: { ready: 1 }, approvalItems: [], readyItems: [item], blockedItems: [], highRiskItems: [], itemsWithoutNextAction: [], dueOpenLoops: [], rules: [] }
    if (mode === 'states') overview.states = ['invented']
    if (mode === 'counts') dashboard.counts.ready = -1
    const service = { overview: () => of(overview), dashboard: () => of(dashboard), items: () => pending, approvals: () => of(mode === 'approval' ? [{ ...item, approvalStatus: 'unknown' }] : []), reminderProposals: () => of(undefined), reminderActivationHistory: () => of(undefined), reminderDeliveryHistory: () => of(undefined) }
    const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, {}, {})
    const previous = [{ ...item, title: 'Previous record' }]
    component.items = previous; component.dataLoaded = true; component.workerReviewRequired = true
    try {
      component.refresh()
      if (mode === 'empty') pending.complete()
      else if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(pending.observed, false) }
      else { pending.next(mode === 'items' ? null : mode === 'duplicate' ? [item, item] : mode === 'missing_boolean' ? [{ ...item, requiresApproval: undefined }] : [item]); pending.complete() }
      assert.equal(component.loading, false, mode)
      assert.equal(component.runningAction, undefined, mode)
      assert.equal(component.workerReviewRefreshed, mode === 'valid', mode)
      if (mode !== 'valid') assert.equal(component.items, previous, mode)
      if (!['valid', 'destroy'].includes(mode)) assert.equal(component.loadFailed, true, mode)
    } finally { component.ngOnDestroy() }
  }
})

test('follow-up and recovery workers validate receipts and clean up uncertain observations', () => {
  const workflowId = '11111111-1111-4111-8111-111111111111'
  const openLoopId = '22222222-2222-4222-8222-222222222222'
  for (const kind of ['followups', 'recovery']) for (const mode of ['valid', 'empty', 'error', 'destroy', 'counts', 'invalid_id', 'duplicate', 'wrong_status']) {
    const response = new Subject(); const notices = []; let writes = 0
    const service = { runDueOpenLoops: () => { writes++; return response }, recoverStaleClaims: () => { writes++; return response } }
    const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, {}, {})
    component.actionsUnavailable = () => false; component.dueOpenLoopCount = () => 1; component.refresh = () => {}
    component.overview = { states: ['ready', 'blocked', 'completed'] }
    const run = () => kind === 'followups' ? component.runDueOpenLoops() : component.recoverStaleClaims()
    try {
      run(); run(); assert.equal(writes, 1)
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else {
        const result = { workflowId: mode === 'invalid_id' ? '../private-token' : workflowId, openLoopId, status: mode === 'wrong_status' ? 'completed' : kind === 'followups' ? 'triggered' : 'reopened', type: 'open_loop', state: 'ready', message: 'private-token' }
        const results = mode === 'duplicate' ? [result, result] : [result]
        response.next(kind === 'followups' ? { checked: results.length, triggered: mode === 'counts' ? 5 : results.length, resolved: 0, skipped: 0, results } : { checked: results.length, workflowsBlocked: 0, openLoopsReopened: mode === 'counts' ? 5 : results.length, skipped: 0, results })
      }
      assert.equal(component.runningAction, undefined, `${kind}/${mode}`)
      assert.doesNotMatch(JSON.stringify(notices) + JSON.stringify(component.lastOperation) + JSON.stringify(component.recoverySummary) + JSON.stringify(component.openLoopRunSummary), /private-token/)
      if (!['valid', 'destroy'].includes(mode)) {
        assert.equal(component.workerReviewRequired, true, `${kind}/${mode}`)
        if (kind === 'followups') run(); else component.runDue()
        assert.equal(writes, 1)
      }
    } finally { component.ngOnDestroy() }
  }
})

test('workflow recovery accepts absent or nil loop references but never binds a nonzero foreign loop', () => {
  const workflowId = '11111111-1111-4111-8111-111111111111'
  for (const openLoopId of [undefined, '00000000-0000-0000-0000-000000000000', '22222222-2222-4222-8222-222222222222']) {
    const component = new WorkflowEngineComponent(new FormBuilder(), { recoverStaleClaims: () => of({ checked: 1, workflowsBlocked: 1, openLoopsReopened: 0, skipped: 0, results: [{ workflowId, openLoopId, type: 'workflow', status: 'blocked', message: 'private-token' }] }) }, {}, { info: () => {}, warning: () => {} }, {}, {}, {}, {}, {})
    component.actionsUnavailable = () => false; component.refresh = () => {}
    try {
      component.recoverStaleClaims()
      if (!openLoopId || openLoopId.startsWith('0000')) {
        assert.equal(component.recoverySummary.results[0].openLoopId, undefined)
        assert.equal(component.workerReviewRequired, false)
      } else assert.equal(component.workerReviewRequired, true)
    } finally { component.ngOnDestroy() }
  }
})

test('workflow batch transport validates limits and never forwards extra execution authority', () => {
  const calls = []
  const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return of({}) } })
  for (const [method, maximum] of [['runDue', 50], ['runDueOpenLoops', 50], ['recoverStaleClaims', 50], ['runDueReminderDeliveries', 100]]) {
    for (const request of [null, undefined, [], 'private-token', 4, { limit: 0 }, { limit: -1 }, { limit: maximum + 1 }, { limit: 1.5 }, { limit: NaN }, { limit: Infinity }, { limit: '10' }, { limit: null }, { limit: true }]) {
      const before = calls.length; let rejected = false
      assert.doesNotThrow(() => service[method](request).subscribe({ error: error => { rejected = true; assert.doesNotMatch(error.message, /private-token/) } }))
      assert.equal(rejected, true, `${method}/${JSON.stringify(request)}`)
      assert.equal(calls.length, before)
    }
    for (const limit of [undefined, 1, 10, maximum]) {
      const request = { limit, ownerIdentity: 'other-user', approved: true, bypassSafety: true, actor: 'admin' }
      service[method](request).subscribe()
      const call = calls.at(-1)
      assert.deepEqual(call.body, { limit: limit ?? 10 })
      request.limit = 49
      assert.deepEqual(call.body, { limit: limit ?? 10 })
    }
  }
  assert.deepEqual([...new Set(calls.map(call => call.url))], ['/api/v1/workflow/run-due', '/api/v1/workflow/open-loops/run-due', '/api/v1/workflow/recover-stale', '/api/v1/workflow/reminder-deliveries/run-due'])
})

test('intake pursuit matching cannot restore stale routing choices', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const match = { pursuit: { id, title: 'Evidence bundle' }, score: 0.9, confidence: 'high', reasons: ['project matches'] }
  for (const mode of ['valid', 'edited', 'empty', 'error', 'destroy', 'duplicate', 'invalid', 'foreign_selection', 'superseded']) {
    const responses = []; const notices = []
    const component = new WorkflowEngineComponent(new FormBuilder(), {}, { match: () => { const response = new Subject(); responses.push(response); return response } }, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, { snapshot: { queryParamMap: { get: () => null } } }, {}, { detectChanges: () => {} }, {})
    component.refresh = () => {}
    try {
      component.ngOnInit(); component.intakeForm.patchValue({ input: 'Find evidence' }); component.matchPursuits()
      const response = responses[0]
      if (mode === 'edited') { component.intakeForm.patchValue({ input: 'Different project' }); assert.equal(response.observed, false) }
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'superseded') { component.matchPursuits(); assert.equal(response.observed, false); responses[1].next([]) }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(mode === 'duplicate' ? [match, match] : mode === 'invalid' ? [{ ...match, score: Infinity }] : [match])
      if (mode === 'foreign_selection') component.selectPursuitMatch({ ...match, pursuit: { id: '22222222-2222-4222-8222-222222222222', title: 'Other' } })
      assert.equal(component.matchingPursuits, false, mode)
      assert.equal(component.selectedPursuitMatch?.pursuit.id, ['valid', 'foreign_selection'].includes(mode) ? id : undefined, mode)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('intake acknowledgements stay bounded and never guess the newly created workflow', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'candidate', 'matched_candidate', 'selected', 'empty', 'error', 'destroy', 'foreign', 'missing', 'edited']) {
    const response = new Subject(); const notices = []; let writes = 0; let opened = 0
    const choice = { pursuit: { id, title: 'Project' }, score: 0.9, reasons: [], confidence: 'high' }
    const component = new WorkflowEngineComponent(new FormBuilder(), {}, { routeIntake: () => { writes++; return response }, intake: () => { writes++; return response } }, Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push(args)])), {}, {}, {}, {}, {})
    component.dataLoaded = true; component.refresh = () => {}; component.open = () => { opened++ }
    component.intakeForm.patchValue({ input: 'Collect evidence' })
    if (mode === 'selected') { component.selectedPursuitMatch = choice; component.pursuitMatches = [choice] }
    try {
      component.intake(); component.intake(); assert.equal(writes, 1)
      if (mode === 'destroy') { component.ngOnDestroy(); assert.equal(response.observed, false) }
      if (mode === 'edited') component.intakeForm.patchValue({ input: 'Other input' })
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else {
        const detail = { pursuit: { id: mode === 'foreign' ? '22222222-2222-4222-8222-222222222222' : id }, workflows: [{ id: '33333333-3333-4333-8333-333333333333', updatedAt: '2026-10-02T12:00:00Z' }] }
        response.next(mode === 'selected' ? detail : mode === 'missing' ? {} : { mode: mode === 'candidate' ? 'candidate_created' : mode === 'matched_candidate' ? 'matched_candidate' : 'matched_existing', matched: mode !== 'candidate', createdCandidate: mode === 'candidate', pursuitId: id, detail, message: 'private-token' })
      }
      assert.equal(component.saving, false, mode)
      assert.equal(opened, 0, mode)
      assert.equal(component.lastIntakePursuitId, ['valid', 'candidate', 'matched_candidate', 'selected'].includes(mode) ? id : undefined, mode)
      assert.doesNotMatch(JSON.stringify(notices) + JSON.stringify(component.lastOperation), /private-token/)
      if (['empty', 'error', 'foreign', 'missing'].includes(mode)) { assert.equal(component.workerReviewRequired, true, mode); component.intake(); assert.equal(writes, 1) }
    } finally { component.ngOnDestroy() }
  }
})

test('exact intake workflow references are admitted without guessing or automatic execution', () => {
  const pursuitId = '11111111-1111-4111-8111-111111111111'
  const workflowId = '22222222-2222-4222-8222-222222222222'
  for (const mode of ['matched', 'selected', 'standalone', 'zero', 'contradiction', 'candidate', 'edited']) {
    const response = new Subject(); const inspected = []
    const component = new WorkflowEngineComponent(new FormBuilder(), {}, { routeIntake: () => response, intake: () => response }, { info() {}, warning() {}, error() {} }, {}, {}, {}, {}, {})
    component.dataLoaded = true; component.refresh = () => {}; component.loadWorkflowRecord = id => inspected.push(id)
    component.intakeForm.patchValue({ input: 'Prepare source bundle' })
    const choice = { pursuit: { id: pursuitId }, score: 0.9 }
    if (mode === 'selected') { component.selectedPursuitMatch = choice; component.pursuitMatches = [choice] }
    try {
      component.intake()
      if (mode === 'edited') component.intakeForm.patchValue({ input: 'Different request' })
      const detail = { pursuit: { id: pursuitId }, intakeWorkflowId: mode === 'contradiction' ? pursuitId : workflowId }
      response.next(mode === 'selected' ? detail : {
        mode: ['standalone', 'candidate'].includes(mode) ? 'candidate_created' : 'matched_existing',
        matched: !['standalone', 'candidate'].includes(mode), createdCandidate: mode === 'candidate',
        pursuitId: mode === 'standalone' ? '00000000-0000-0000-0000-000000000000' : pursuitId,
        workflowId: mode === 'zero' ? '00000000-0000-0000-0000-000000000000' : workflowId,
        detail: mode === 'standalone' ? undefined : detail,
      })
      const valid = ['matched', 'selected', 'standalone'].includes(mode)
      assert.equal(component.lastIntakeWorkflowId, valid ? workflowId : undefined, mode)
      assert.equal(inspected.length, 0, mode)
      component.inspectIntakeWorkflow()
      assert.deepEqual(inspected, valid ? [workflowId] : [], mode)
      if (['zero', 'contradiction', 'candidate'].includes(mode)) assert.equal(component.workerReviewRequired, true, mode)
    } finally { component.ngOnDestroy() }
  }
})

test('workflow deep links use bounded identity-matched record loading', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'foreign', 'missing', 'empty', 'error', 'destroy', 'invalid_link']) {
    const response = new Subject(); const requested = []; const applied = []
    const service = { get: value => { requested.push(value); return response } }
    const route = { snapshot: { queryParamMap: { get: () => mode === 'invalid_link' ? '../other?private-token' : id } } }
    const notices = []; const notification = Object.fromEntries(['info', 'success', 'error'].map(kind => [kind, (...args) => notices.push(args)]))
    const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, notification, {}, route, {}, { detectChanges: () => {} }, {})
    component.refresh = () => {}
    component.applyWorkflowRecord = value => { applied.push(value); component.selected = value }
    try {
      component.ngOnInit()
      if (mode === 'invalid_link') assert.deepEqual(requested, [])
      else assert.deepEqual(requested, [id])
      if (mode === 'destroy') component.ngOnDestroy()
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(mode === 'missing' ? {} : { item: { id: mode === 'foreign' ? '22222222-2222-4222-8222-222222222222' : id } })
      assert.equal(applied.length, mode === 'valid' ? 1 : 0, mode)
      assert.equal(component.openingWorkflowId, undefined, mode)
      assert.doesNotMatch(JSON.stringify(notices) + component.workflowOpenError, /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('workflow selection supersedes older reads and consumes only one record', () => {
  const reads = []; const applied = []
  const component = new WorkflowEngineComponent(new FormBuilder(), { get: id => { const response = new Subject(); reads.push({ id, response }); return response } }, {}, { error: () => {} }, {}, {}, {}, { detectChanges: () => {} }, {})
  component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
  const first = '11111111-1111-4111-8111-111111111111'
  const second = '22222222-2222-4222-8222-222222222222'
  try {
    component.selected = { item: { id: first } }
    component.open({ id: first }); component.open({ id: second })
    assert.equal(component.selected, undefined)
    assert.equal(component.anyActionRunning(), true)
    reads[0].response.next({ item: { id: first } })
    assert.deepEqual(applied, [])
    reads[1].response.next({ item: { id: second } })
    reads[1].response.next({ item: { id: first } })
    assert.equal(applied.length, 1)
    assert.equal(component.selected.item.id, second)
    assert.equal(component.openingWorkflowId, undefined)
    component.ngOnDestroy(); component.open({ id: first })
    assert.equal(reads.length, 2)
  } finally { component.ngOnDestroy() }
})

test('workflow transition acknowledgements bind state and preserve newer selections', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  for (const mode of ['foreign', 'valid', 'wrong_state', 'empty', 'error', 'destroy', 'new_selection']) {
    const response = new Subject(); const calls = []; const applied = []; const notices = []; let refreshes = 0
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new WorkflowEngineComponent(new FormBuilder(), { transition: (workflowId, request) => { calls.push({ workflowId, request }); return response } }, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
    component.dataLoaded = true; component.overview = { states: ['ready', 'blocked'] }
    const original = { item: { id, currentState: 'blocked' } }; component.selected = original
    component.refresh = () => { refreshes++ }
    component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
    try {
      component.transition(); component.transition()
      assert.equal(calls.length, 1)
      if (mode === 'destroy') component.ngOnDestroy()
      const newer = { item: { id: '22222222-2222-4222-8222-222222222222', currentState: 'blocked' } }
      if (mode === 'new_selection') component.selected = newer
      const result = { item: { id: mode === 'foreign' ? newer.item.id : id, currentState: mode === 'wrong_state' ? 'blocked' : 'ready' } }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(result)
      assert.equal(component.saving, false, mode)
      assert.equal(applied.length, mode === 'valid' ? 1 : 0, mode)
      assert.equal(refreshes, ['valid', 'new_selection'].includes(mode) ? 1 : 0, mode)
      if (mode === 'new_selection') assert.equal(component.selected, newer)
      if (['foreign', 'wrong_state', 'empty', 'error'].includes(mode)) {
        assert.equal(component.transitionReviewId, id)
        component.transition()
        assert.equal(calls.length, 1)
      }
      assert.equal(notices.some(n => n.kind === 'success'), false)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('workflow transition recovery reads the affected record without repeating mutation', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const other = '22222222-2222-4222-8222-222222222222'
  const response = new Subject(); const mutations = []; const reads = []; const applied = []
  const service = {
    transition: (...args) => { mutations.push(args); return response },
    get: workflowId => { const result = new Subject(); reads.push({ workflowId, result }); return result },
  }
  const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, () => {}]))
  const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
  component.dataLoaded = true; component.overview = { states: ['ready', 'blocked'] }
  component.selected = { item: { id, currentState: 'blocked' } }
  component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
  try {
    component.transition()
    component.open({ id: other })
    assert.equal(reads.length, 0, 'selection cannot start a read during a mutation')
    response.complete()
    assert.equal(component.transitionReviewId, id)
    component.reviewTransition(); component.reviewTransition(); component.transition()
    assert.equal(reads.length, 1)
    assert.equal(reads[0].workflowId, id)
    reads[0].result.next({ item: { id: other, currentState: 'ready' } })
    assert.equal(component.transitionReviewId, id)
    assert.equal(applied.length, 0)
    component.reviewTransition()
    assert.equal(reads.length, 2)
    reads[1].result.next({ item: { id } })
    assert.equal(component.transitionReviewId, id, 'matching identity without a state cannot clear uncertainty')
    assert.equal(applied.length, 0)
    component.reviewTransition()
    assert.equal(reads.length, 3)
    reads[2].result.next({ item: { id, currentState: 'blocked' } })
    assert.equal(component.transitionReviewId, undefined)
    assert.equal(component.transitionError, undefined)
    assert.equal(applied.length, 1)
    assert.equal(mutations.length, 1, 'recovery does not retry the uncertain mutation')
    assert.equal(component.anyActionRunning(), false)
  } finally { component.ngOnDestroy() }
})

test('workflow approvals require bound decision evidence and preserve newer selections', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const other = '22222222-2222-4222-8222-222222222222'
  for (const mode of ['foreign', 'missing_decision', 'wrong_decision', 'old_decision', 'wrong_approval', 'valid', 'rejected', 'empty', 'error', 'destroy', 'new_selection', 'stale']) {
    const response = new Subject(); const calls = []; const applied = []; const notices = []; let refreshes = 0
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new WorkflowEngineComponent(new FormBuilder(), { resolveApproval: (...args) => { calls.push(args); return response } }, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
    const original = { item: { id, requiresApproval: true, approvalStatus: 'pending', currentState: 'needs_approval' }, decisions: [] }
    component.dataLoaded = true; component.overview = { states: ['needs_approval', 'ready', 'blocked'] }; component.selected = original
    if (mode === 'old_decision') original.decisions.push({ id: '33333333-3333-4333-8333-333333333333' })
    component.refresh = () => { refreshes++ }; component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
    const approved = mode !== 'rejected'
    try {
      component.resolveApproval(mode === 'stale' ? { ...original.item, id: other } : original.item, approved)
      if (mode === 'stale') { assert.equal(calls.length, 0); continue }
      component.resolveApproval(original.item, approved); assert.equal(calls.length, 1)
      if (mode === 'destroy') component.ngOnDestroy()
      const newer = { item: { id: other } }
      if (mode === 'new_selection') component.selected = newer
      const result = {
        item: { ...original.item, id: mode === 'foreign' ? other : id, requiresApproval: mode === 'wrong_approval' ? false : true, approvalStatus: approved ? 'approved' : 'rejected', currentState: approved ? 'ready' : 'blocked' },
        decisions: mode === 'missing_decision' ? [] : [{ id: '33333333-3333-4333-8333-333333333333', workflowId: mode === 'wrong_decision' ? other : id, decisionType: 'approval', decision: approved ? 'approved' : 'rejected', approved, createdAt: '2026-10-02T10:00:00Z' }],
      }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(result)
      const valid = ['valid', 'rejected', 'new_selection'].includes(mode)
      assert.equal(applied.length, ['valid', 'rejected'].includes(mode) ? 1 : 0, mode)
      assert.equal(refreshes, valid ? 1 : 0, mode)
      assert.equal(component.saving, false, mode)
      if (mode === 'new_selection') assert.equal(component.selected, newer)
      if (!valid && mode !== 'destroy') {
        assert.equal(component.approvalReviewId, id, mode)
        component.resolveApproval(original.item, approved); assert.equal(calls.length, 1)
      }
      assert.equal(notices.some(n => n.kind === 'success'), false)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('workflow approval recovery validates read state and never retries a decision', () => {
  const id = '11111111-1111-4111-8111-111111111111'
  const write = new Subject(); const reads = []; let mutations = 0
  const service = { resolveApproval: () => { mutations++; return write }, get: workflowId => { const result = new Subject(); reads.push({ workflowId, result }); return result } }
  const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, () => {}]))
  const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
  const original = { item: { id, requiresApproval: true, approvalStatus: 'pending', currentState: 'needs_approval' }, decisions: [] }
  component.dataLoaded = true; component.overview = { states: ['needs_approval', 'ready', 'blocked'] }; component.selected = original
  component.applyWorkflowRecord = record => { component.selected = record }
  try {
    component.resolveApproval(original.item, true); write.complete()
    assert.equal(component.approvalReviewId, id)
    component.reviewApproval(); component.reviewApproval()
    assert.equal(reads.length, 1)
    assert.equal(reads[0].workflowId, id)
    reads[0].result.next({ item: { id, currentState: 'ready' } })
    assert.equal(component.approvalReviewId, id)
    component.reviewApproval()
    reads[1].result.next(original)
    assert.equal(component.approvalReviewId, undefined)
    assert.equal(component.approvalError, undefined)
    assert.equal(component.selected, original)
    assert.equal(mutations, 1)
    component.ngOnDestroy(); component.resolveApproval(original.item, true)
    assert.equal(mutations, 1)
  } finally { component.ngOnDestroy() }
})

test('workflow transport rejects unsafe references across record and mutation paths', () => {
  const calls = []; const http = Object.fromEntries(['get', 'post', 'patch'].map(method => [method, (...args) => { calls.push({ method, args }); return of({}) }]))
  const service = new WorkflowService(http)
  const valid = '11111111-1111-4111-8111-111111111111'
  const operations = [
    id => service.get(id), id => service.transition(id, { targetState: 'ready' }),
    id => service.resolveApproval(id, { approved: false }), id => service.runOne(id),
    id => service.resolveInterruptedExecution(id, { decision: 'keep_blocked', note: 'Review' }),
    id => service.resolveProposal(id, valid, {}), id => service.resolveProposal(valid, id, {}),
    id => service.updateChecklistItem(id, valid, { status: 'pending' }), id => service.updateChecklistItem(valid, id, { status: 'pending' }),
    id => service.prepareReminderActivation(id, {}), id => service.decideReminderActivation(id, {}),
    id => service.reminderActivationDecisionHistory(id), id => service.authorizeReminderDelivery(id, {}),
    id => service.frameworkSelection(id),
  ]
  for (const id of ['../run-due', 'id/approval?owner=other', '00000000-0000-0000-0000-000000000000', '', null, undefined, 'private-token']) {
    for (const operation of operations) {
      let error
      operation(id).subscribe({ error: value => { error = value } })
      assert.equal(calls.length, 0)
      assert.ok(error)
      assert.doesNotMatch(String(error), /private-token/)
    }
  }
})

test('workflow reference validation preserves canonical read and mutation routes', () => {
  const calls = []; const http = Object.fromEntries(['get', 'post', 'patch'].map(method => [method, (...args) => { calls.push({ method, args }); return of({}) }]))
  const service = new WorkflowService(http)
  const id = '11111111-1111-4111-8111-111111111111'; const child = '22222222-2222-4222-8222-222222222222'
  service.get(id).subscribe(); service.runOne(id).subscribe()
  service.resolveInterruptedExecution(id, { decision: 'keep_blocked', note: 'Review' }).subscribe()
  service.resolveProposal(id, child, { approved: false }).subscribe()
  service.updateChecklistItem(id, child, { status: 'open' }).subscribe()
  service.prepareReminderActivation(child, { expectedReminderDigest: 'a'.repeat(64), idempotencyKey: 'ui:prepare', activationKind: 'internal_notification', confirmation: 'PREPARE INTERNAL REMINDER ONLY' }).subscribe()
  service.decideReminderActivation(child, { decision: 'approved', reason: 'Reviewed', expectedActivationRequestDigest: 'a'.repeat(64), confirmation: 'APPROVE INTERNAL REMINDER PREPARATION' }).subscribe()
  service.reminderActivationDecisionHistory(child).subscribe()
  service.authorizeReminderDelivery(child, { expectedActivationRequestDigest: 'a'.repeat(64), expectedActivationDecisionDigest: 'b'.repeat(64), expectedReminderDigest: 'c'.repeat(64), idempotencyKey: 'ui:deliver', channel: 'in_app', confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER' }).subscribe()
  service.frameworkSelection(child).subscribe()
  assert.deepEqual(calls.map(call => [call.method, call.args[0]]), [
    ['get', `/api/v1/workflow/${id}`], ['post', `/api/v1/workflow/${id}/run`],
    ['post', `/api/v1/workflow/${id}/interruption/resolve`], ['post', `/api/v1/workflow/${id}/proposals/${child}/resolve`],
    ['patch', `/api/v1/workflow/${id}/checklist/${child}`], ['post', `/api/v1/workflow/reminder-proposals/${child}/activation-requests`],
    ['post', `/api/v1/workflow/reminder-activation-requests/${child}/decisions`], ['get', `/api/v1/workflow/reminder-activation-requests/${child}/decisions`],
    ['post', `/api/v1/workflow/reminder-activation-requests/${child}/delivery-authorizations`], ['get', '/api/v1/framework-registry/selections'],
  ])
})

test('reminder mutation transports validate evidence and whitelist non-executing authority fields', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const calls = []
  const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return of({}) } })
  const contracts = [
    ['prepareReminderActivation', { expectedReminderDigest: 'a'.repeat(64), idempotencyKey: 'ui:prepare', activationKind: 'internal_notification', confirmation: 'PREPARE INTERNAL REMINDER ONLY' }],
    ['decideReminderActivation', { decision: 'approved', reason: 'Reviewed', expectedActivationRequestDigest: 'a'.repeat(64), confirmation: 'APPROVE INTERNAL REMINDER PREPARATION' }],
    ['authorizeReminderDelivery', { expectedActivationRequestDigest: 'a'.repeat(64), expectedActivationDecisionDigest: 'b'.repeat(64), expectedReminderDigest: 'c'.repeat(64), idempotencyKey: 'ui:deliver', channel: 'in_app', confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER' }],
  ]
  for (const [method, valid] of contracts) {
    const invalid = [undefined, null, [], {}, { ...valid, confirmation: 'BYPASS' }]
    if (method === 'decideReminderActivation') invalid.push({ ...valid, decision: 'publish' }, { ...valid, reason: ' ' }, { ...valid, reason: 'a'.repeat(2001) }, { ...valid, expectedPreviousDecisionId: 'foreign' })
    else invalid.push({ ...valid, idempotencyKey: 'secret/key' }, { ...valid, idempotencyKey: 'a'.repeat(161) })
    for (const key of Object.keys(valid).filter(key => key.includes('Digest'))) invalid.push({ ...valid, [key]: 'invalid' })
    for (const request of invalid) {
      const before = calls.length; let rejected = false
      service[method](id, request).subscribe({ error: () => { rejected = true } })
      assert.equal(rejected, true, method); assert.equal(calls.length, before)
    }
    const request = { ...valid, ownerIdentity: 'foreign', bypassSafety: true, canExecute: true, token: 'private-token' }
    service[method](id, request).subscribe()
    assert.deepEqual(calls.at(-1).body, valid)
    request.confirmation = 'Changed later'
    assert.deepEqual(calls.at(-1).body, valid)
  }
})

test('workflow transition and approval transport validates and copies supported requests', () => {
  const calls = []; const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return of({}) } })
  const id = '11111111-1111-4111-8111-111111111111'
  const transition = { targetState: 'blocked', message: 'Review needed', actor: 'operator', ownerIdentity: 'other', approved: true }
  service.transition(id, transition).subscribe(); transition.message = 'Later edit'
  assert.deepEqual(calls[0], { url: `/api/v1/workflow/${id}/transition`, body: { targetState: 'blocked', message: 'Review needed', actor: 'operator' } })
  const approval = { approved: false, note: 'Reject', actor: 'operator', executionEnabled: true }
  service.resolveApproval(id, approval).subscribe(); approval.note = 'Later edit'
  assert.deepEqual(calls[1], { url: `/api/v1/workflow/${id}/approval`, body: { approved: false, note: 'Reject', actor: 'operator' } })
  for (const request of [null, {}, { targetState: '../run' }, { targetState: 'ready', message: {} }, { targetState: 'ready', actor: {} }]) {
    let error; service.transition(id, request).subscribe({ error: value => { error = value } }); assert.ok(error)
  }
  for (const request of [null, {}, { approved: 'false' }, { approved: true, note: {} }, { approved: false, actor: {} }]) {
    let error; service.resolveApproval(id, request).subscribe({ error: value => { error = value } }); assert.ok(error)
  }
  assert.equal(calls.length, 2)
})

test('interrupted workflow retries require reconciliation confirmation and source evidence', () => {
  for (const decision of ['retry', 'confirm_completed']) {
    for (const mode of ['unchecked', 'missing_source', 'valid']) {
      const calls = []; const response = new Subject()
      const component = new WorkflowEngineComponent(new FormBuilder(), { resolveInterruptedExecution: (...args) => { calls.push(args); return response } }, {}, { error: () => {}, warning: () => {} }, {}, {}, {}, { detectChanges: () => {} }, {})
      component.dataLoaded = true
      component.selected = { item: { id: '11111111-1111-4111-8111-111111111111', currentState: 'blocked', recoveryStatus: 'needs_review', requiresApproval: false } }
      component.interruptionForm.patchValue({ decision, note: 'Reviewed the prior outcome', evidenceUri: mode === 'missing_source' ? '' : 'local://recovery/source', priorExecutionReconciled: mode !== 'unchecked' })
      try {
        assert.equal(component.interruptionRequiresEvidence, true, decision)
        component.resolveInterruptedExecution()
        assert.equal(calls.length, mode === 'valid' ? 1 : 0, `${decision}:${mode}`)
        if (mode === 'valid') assert.equal(calls[0][1].priorExecutionReconciled, true)
      } finally { component.ngOnDestroy(); response.complete() }
    }
  }
})

test('interruption acknowledgements preserve context and require matching decision evidence', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const other = '22222222-2222-4222-8222-222222222222'
  for (const mode of ['foreign', 'missing_decision', 'valid', 'new_selection', 'new_note', 'empty', 'error', 'destroy']) {
    const response = new Subject(); const applied = []; const calls = []; const notices = []
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new WorkflowEngineComponent(new FormBuilder(), { resolveInterruptedExecution: (...args) => { calls.push(args); return response } }, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
    component.dataLoaded = true; component.refresh = () => {}
    const original = { item: { id, currentState: 'blocked', recoveryStatus: 'needs_review', requiresApproval: false }, decisions: [] }
    component.selected = original; component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
    component.interruptionForm.patchValue({ decision: 'keep_blocked', note: 'Uncertain' })
    try {
      component.resolveInterruptedExecution(); component.resolveInterruptedExecution(); assert.equal(calls.length, 1)
      const newer = { item: { id: other } }
      if (mode === 'new_selection') component.selected = newer
      if (mode === 'new_note') component.interruptionForm.patchValue({ note: 'New draft' })
      if (mode === 'destroy') component.ngOnDestroy()
      const result = { ...original, item: { ...original.item, id: mode === 'foreign' ? other : id }, decisions: mode === 'missing_decision' ? [] : [{ id: '33333333-3333-4333-8333-333333333333', workflowId: id, decisionType: 'interrupted_execution', decision: 'keep_blocked', approved: false, createdAt: '2026-10-02T12:00:00Z' }] }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(result)
      assert.equal(applied.length, ['valid', 'new_note'].includes(mode) ? 1 : 0, mode)
      assert.equal(component.saving, false, mode)
      if (mode === 'new_selection') assert.equal(component.selected, newer)
      if (mode === 'new_note') assert.equal(component.interruptionForm.value.note, 'New draft')
      if (['foreign', 'missing_decision', 'empty', 'error'].includes(mode)) {
        assert.equal(component.transitionReviewId, id)
        assert.equal(component.workerReviewRequired, true)
        // A record refresh alone must not restore permission to execute.
        component.transitionReviewId = undefined
        component.queueCount = () => 1
        component.workflowService.runDue = () => { throw new Error('worker must remain paused') }
        component.runDue()
        assert.equal(component.workerReviewRequired, true)
      } else {
        assert.equal(component.workerReviewRequired, false, mode)
      }
      assert.equal(notices.some(n => n.kind === 'success'), false)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
    } finally { component.ngOnDestroy(); response.complete() }
  }
})

test('interruption component and real service bind retry/completion evidence through mocked HTTP', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const uri = 'local://recovery/source'
  for (const [decision, requiresApproval] of [['retry', false], ['retry', true], ['confirm_completed', false]]) {
    for (const wrongSource of [false, true]) {
      const response = new Subject(); const calls = []; const applied = []
      const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return response } })
      const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, () => {}]))
      const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
      component.dataLoaded = true; component.refresh = () => {}; component.selected = { item: { id, currentState: 'blocked', recoveryStatus: 'needs_review', requiresApproval }, decisions: [] }
      component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
      component.interruptionForm.patchValue({ decision, note: 'Outcome checked', evidenceUri: uri, priorExecutionReconciled: true })
      try {
        component.resolveInterruptedExecution()
        assert.equal(calls.length, 1)
        assert.equal(calls[0].url, `/api/v1/workflow/${id}/interruption/resolve`)
        assert.equal(calls[0].body.priorExecutionReconciled, true)
        response.next({
          item: { id, requiresApproval, approvalStatus: requiresApproval ? 'pending' : 'not_required', currentState: decision === 'retry' ? requiresApproval ? 'needs_approval' : 'ready' : 'completed', recoveryStatus: decision === 'retry' ? 'retry_confirmed' : 'completion_confirmed' },
          decisions: [{ id: '22222222-2222-4222-8222-222222222222', workflowId: id, decisionType: 'interrupted_execution', decision, approved: decision === 'confirm_completed', createdAt: '2026-10-02T12:00:00Z' }],
          sourceLinks: [{ workflowId: id, sourceType: 'recovery_evidence', sourceUri: wrongSource ? 'local://other/source' : uri, relationship: decision === 'retry' ? 'execution_reconciliation' : 'completion_evidence' }],
        })
        assert.equal(applied.length, wrongSource ? 0 : 1)
        assert.equal(component.saving, false)
        if (wrongSource) {
          assert.equal(component.transitionReviewId, id)
          assert.equal(component.workerReviewRequired, true)
        }
        else assert.equal(component.interruptionForm.value.priorExecutionReconciled, false)
      } finally { component.ngOnDestroy() }
    }
  }
})

test('interruption transport enforces explicit reconciliation and copies supported evidence', () => {
  const calls = []; const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return of({}) } })
  const id = '11111111-1111-4111-8111-111111111111'
  for (const decision of ['retry', 'confirm_completed']) {
    for (const patch of [{}, { priorExecutionReconciled: false }, { priorExecutionReconciled: 'true' }, { priorExecutionReconciled: true, evidenceUri: '' }]) {
      let error
      service.resolveInterruptedExecution(id, { decision, note: 'Reviewed', evidenceUri: 'local://recovery/source', ...patch }).subscribe({ error: value => { error = value } })
      assert.ok(error); assert.equal(calls.length, 0)
    }
  }
  const request = { decision: 'retry', note: 'Reviewed', evidenceUri: 'local://recovery/source', priorExecutionReconciled: true, ownerIdentity: 'foreign', executionEnabled: true }
  service.resolveInterruptedExecution(id, request).subscribe(); request.note = 'Changed after dispatch'
  assert.deepEqual(calls[0].body, { decision: 'retry', note: 'Reviewed', evidenceUri: 'local://recovery/source', priorExecutionReconciled: true })
  service.resolveInterruptedExecution(id, { decision: 'keep_blocked', note: 'Uncertain' }).subscribe()
  assert.deepEqual(calls[1].body, { decision: 'keep_blocked', note: 'Uncertain' })
})

test('workflow proposal and checklist transport validate and copy only supported fields', () => {
  const calls = []; const http = Object.fromEntries(['post', 'patch'].map(method => [method, (url, body) => { calls.push({ method, url, body }); return of({}) }]))
  const service = new WorkflowService(http)
  const id = '11111111-1111-4111-8111-111111111111'; const child = '22222222-2222-4222-8222-222222222222'
  const proposal = { status: 'approved', approved: true, selectedOption: 'Use selected automation', note: 'Reviewed', actor: 'operator', ownerIdentity: 'other', executionEnabled: true }
  service.resolveProposal(id, child, proposal).subscribe(); proposal.note = 'Later edit'
  assert.deepEqual(calls[0].body, { status: 'approved', approved: true, selectedOption: 'Use selected automation', note: 'Reviewed', actor: 'operator' })
  const checklist = { status: 'blocked', note: 'Missing document', actor: 'operator', approved: true }
  service.updateChecklistItem(id, child, checklist).subscribe(); checklist.note = 'Later edit'
  assert.deepEqual(calls[1].body, { status: 'blocked', note: 'Missing document', actor: 'operator' })
  for (const request of [null, {}, { status: 'open' }, { status: 'approved', approved: false }, { status: 'rejected', approved: true }, { approved: 'false' }, { status: 'approved', selectedOption: {} }, { status: 'rejected', note: {} }, { status: 'changes_requested', actor: {} }]) {
    let error; service.resolveProposal(id, child, request).subscribe({ error: value => { error = value } }); assert.ok(error)
  }
  for (const request of [null, {}, { status: 'pending' }, { status: 'done', note: {} }, { status: 'open', actor: {} }]) {
    let error; service.updateChecklistItem(id, child, request).subscribe({ error: value => { error = value } }); assert.ok(error)
  }
  assert.equal(calls.length, 2)
  for (const status of ['approved', 'rejected', 'changes_requested']) service.resolveProposal(id, child, { status }).subscribe()
  for (const status of ['open', 'done', 'blocked']) service.updateChecklistItem(id, child, { status }).subscribe()
  assert.equal(calls.length, 8)
})

test('workflow proposal acknowledgements require current bound proposal and decision records', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const proposalId = '22222222-2222-4222-8222-222222222222'; const other = '33333333-3333-4333-8333-333333333333'
  for (const mode of ['foreign', 'missing_proposal', 'wrong_status', 'missing_decision', 'old_decision', 'wrong_option', 'valid_option', 'valid', 'rejected', 'changes_requested', 'empty', 'error', 'destroy', 'new_selection', 'stale']) {
    const response = new Subject(); const calls = []; const applied = []; const notices = []; let refreshes = 0
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new WorkflowEngineComponent(new FormBuilder(), { resolveProposal: (...args) => { calls.push(args); return response } }, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
    const original = { item: { id }, proposals: [{ id: proposalId, workflowId: id, status: 'open' }], decisions: [] }
    if (mode === 'old_decision') original.decisions.push({ id: '44444444-4444-4444-8444-444444444444' })
    component.dataLoaded = true; component.overview = { states: ['ready', 'blocked'] }; component.selected = original
    component.refresh = () => { refreshes++ }; component.applyWorkflowRecord = record => { applied.push(record); component.selected = record }
    const status = ['rejected', 'changes_requested'].includes(mode) ? mode : 'approved'
    const option = ['wrong_option', 'valid_option'].includes(mode) ? 'Selected automation' : undefined
    try {
      component.resolveProposal(mode === 'stale' ? other : proposalId, status, option)
      if (mode === 'stale') { assert.equal(calls.length, 0); continue }
      component.resolveProposal(proposalId, status, option); assert.equal(calls.length, 1)
      const newer = { item: { id: other } }
      if (mode === 'new_selection') component.selected = newer
      if (mode === 'destroy') component.ngOnDestroy()
      const result = {
        item: { id: mode === 'foreign' ? other : id, currentState: 'ready' },
        proposals: mode === 'missing_proposal' ? [] : [{ id: proposalId, workflowId: id, status: mode === 'wrong_status' ? 'open' : status, selectedOption: mode === 'wrong_option' ? 'Different automation' : option }],
        decisions: mode === 'missing_decision' ? [] : [{ id: '44444444-4444-4444-8444-444444444444', workflowId: id, decisionType: 'proposal', decision: status, approved: status === 'approved', createdAt: '2026-10-02T12:00:00Z' }],
      }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else { response.next(result); response.next(result) }
      const valid = ['valid', 'valid_option', 'rejected', 'changes_requested', 'new_selection'].includes(mode)
      assert.equal(applied.length, ['valid', 'valid_option', 'rejected', 'changes_requested'].includes(mode) ? 1 : 0, mode)
      assert.equal(refreshes, valid ? 1 : 0, mode)
      assert.equal(component.proposalAction, undefined, mode)
      if (mode === 'new_selection') assert.equal(component.selected, newer)
      if (!valid && mode !== 'destroy') { assert.equal(component.transitionReviewId, id); component.resolveProposal(proposalId, status); assert.equal(calls.length, 1) }
      assert.equal(notices.some(n => n.kind === 'success'), false)
      assert.doesNotMatch(JSON.stringify(notices), /private-token/)
    } finally { component.ngOnDestroy(); response.complete() }
  }
})

test('workflow proposal decision reaches the real service through a mocked HTTP boundary', () => {
  const id = '11111111-1111-4111-8111-111111111111'; const proposalId = '22222222-2222-4222-8222-222222222222'
  const response = new Subject(); const calls = []; const applied = []
  const service = new WorkflowService({ post: (url, body) => { calls.push({ url, body }); return response } })
  const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, () => {}]))
  const component = new WorkflowEngineComponent(new FormBuilder(), service, {}, notification, {}, {}, {}, { detectChanges: () => {} }, {})
  component.dataLoaded = true; component.overview = { states: ['ready'] }
  component.selected = { item: { id }, proposals: [{ id: proposalId, workflowId: id, status: 'open' }], decisions: [] }
  component.refresh = () => {}; component.applyWorkflowRecord = value => { applied.push(value); component.selected = value }
  try {
    component.resolveProposal(proposalId, 'approved', 'Chosen automation')
    assert.deepEqual(calls, [{ url: `/api/v1/workflow/${id}/proposals/${proposalId}/resolve`, body: { approved: true, status: 'approved', selectedOption: 'Chosen automation', note: 'Proposal approved from dashboard.', actor: 'operator' } }])
    response.next({ item: { id, currentState: 'ready' }, proposals: [{ id: proposalId, workflowId: id, status: 'approved', selectedOption: 'Chosen automation' }], decisions: [{ id: '33333333-3333-4333-8333-333333333333', workflowId: id, decisionType: 'proposal', decision: 'approved', approved: true, createdAt: '2026-10-02T12:00:00Z' }] })
    assert.equal(applied.length, 1)
    assert.equal(component.proposalAction, undefined)
    assert.equal(component.transitionReviewId, undefined)
  } finally { component.ngOnDestroy() }
})

test('real ambient transport refuses path injection before HTTP dispatch', () => {
  const calls = []
  const http = Object.fromEntries(['get', 'post', 'patch'].map(method => [method, (...args) => { calls.push({ method, args }); return of({}) }]))
  const service = new AmbientService(http)
  const request = { currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true }
  for (const id of ['../scan', 'id/accept?owner=other', '00000000-0000-0000-0000-000000000000', '', 'private-token']) {
    for (const action of ['accept', 'dismiss']) {
      let error
      service[action](id).subscribe({ error: value => { error = value } })
      assert.equal(calls.length, 0, id)
      assert.ok(error)
      assert.doesNotMatch(String(error), /private-token/)
    }
  }
  for (const key of ['../scan', 'safety?owner=other', 'safety/../../scan', '', ' safety ']) {
    let error
    service.updateNeed(key, request).subscribe({ error: value => { error = value } })
    assert.equal(calls.length, 0, key)
    assert.ok(error)
  }
})

test('real ambient priority transport copies only valid supported fields', () => {
  const calls = []
  const service = new AmbientService({ patch: (url, body) => { calls.push({ url, body }); return of({}) } })
  const request = { currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true, notes: 'Original note', ownerIdentity: 'foreign', executionEnabled: true }
  service.updateNeed('safety', request).subscribe()
  request.notes = 'Changed after dispatch'
  assert.deepEqual(calls[0], { url: '/api/v1/ambient/needs/safety', body: { currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true, notes: 'Original note' } })
  for (const patch of [{ targetLevel: 101 }, { priorityWeight: 0.5 }, { enabled: 'true' }, { notes: {} }]) {
    let error
    service.updateNeed('safety', { ...request, ...patch }).subscribe({ error: value => { error = value } })
    assert.ok(error)
    assert.equal(calls.length, 1)
  }
})

const sourcePolicyUrl = new URL('../src/app/control-room/source-navigation.ts', import.meta.url).href
const { safeWebSourceHref } = await import(sourcePolicyUrl)
const { CommandDashboardComponent } = await loadComponentLogic(new URL('../src/app/pages/command-dashboard/command-dashboard.component.ts', import.meta.url), {
  '@angular/forms': import.meta.resolve('@angular/forms'),
  '../../control-room/source-navigation': sourcePolicyUrl,
  '../../services/memory-engine/memory-engine.service.token': 'data:text/javascript,export const MEMORY_ENGINE_SERVICE_TOKEN = Symbol("test-token")',
})
const { AmbientBrainComponent } = await loadComponentLogic(new URL('../src/app/pages/ambient-brain/ambient-brain.component.ts', import.meta.url), {
  '../../control-room/source-navigation': sourcePolicyUrl,
})
const { ControlCenterComponent } = await loadComponentLogic(new URL('../src/app/pages/control-center/control-center.component.ts', import.meta.url), {
  '../../control-room/source-navigation': sourcePolicyUrl,
  '../../control-room/launch-recovery': new URL('../src/app/control-room/launch-recovery.ts', import.meta.url).href,
  '../../shared/http-timeout-policy': 'data:text/javascript,export const HttpTimeoutPolicy=Object.freeze({readMs:8000})',
  '../../services/automations/automations.service.token': 'data:text/javascript,export const AUTOMATIONS_SERVICE_TOKEN = Symbol("test-token")',
  '../../control-room/progressive-section.component': 'data:text/javascript,export class HaiProgressiveSectionComponent {}',
})

test('ambient and control center source actions reject credentials and URL normalization tricks', () => {
  const previousWindow = globalThis.window
  const opened = []; const notices = []
  globalThis.window = { open: (...args) => opened.push(args) }
  try {
    for (const component of [AmbientBrainComponent, ControlCenterComponent, CommandDashboardComponent]) {
      const instance = Object.create(component.prototype)
      instance.notification = { warning: (...args) => notices.push(args) }
      for (const uri of ['https://user:private-token@example.org/source', 'https:example.org', 'https://example.org\\spoof', 'https://example.org/%0a', 'javascript:alert(1)']) {
        instance.openSource(uri)
        assert.equal(opened.length, 0, uri)
      }
      instance.openSource('https://example.org/source')
      assert.deepEqual(opened.pop(), ['https://example.org/source', '_blank', 'noopener,noreferrer'])
      instance.openSource('http://localhost/source')
      if (component === CommandDashboardComponent) assert.equal(opened.length, 0)
      else assert.deepEqual(opened.pop(), ['http://localhost/source', '_blank', 'noopener,noreferrer'])
      if (component === AmbientBrainComponent) {
        assert.equal(instance.canOpenSource('https://user:password@example.org'), false)
        assert.equal(instance.canOpenSource('https://example.org'), true)
      }
    }
    assert.doesNotMatch(JSON.stringify(notices), /private-token/)
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

test('actual ambient component and service preserve the decision route through the HTTP test boundary', () => {
  const response = new Subject(); const calls = []
  const proposal = ambientProposalFixture('11111111-1111-4111-8111-111111111111')
  const resolved = { ...proposal, status: 'accepted' }
  const service = new AmbientService({
    post: (url, body) => { calls.push({ method: 'post', url, body }); return response },
    get: url => { calls.push({ method: 'get', url }); return of({ needs: [], opportunities: [resolved], scans: [] }) },
  })
  const component = new AmbientBrainComponent(service, {}, { info: () => {}, warning: () => {}, error: () => {} }, {}, {})
  component.overview = { needs: [], opportunities: [proposal], scans: [] }
  try {
    component.accept(proposal); component.accept(proposal)
    assert.deepEqual(calls, [{ method: 'post', url: '/api/v1/ambient/opportunities/11111111-1111-4111-8111-111111111111/accept', body: {} }])
    response.next(resolved)
    assert.equal(component.overview.opportunities[0], resolved)
    assert.equal(component.resolving, '')
    assert.equal(component.decisionsNeedRefresh, false)
    assert.deepEqual(calls[1], { method: 'get', url: '/api/v1/ambient/overview' })
  } finally { component.ngOnDestroy() }
})

test('ambient scans require a new completed manual record and valid counters', () => {
  for (const mode of ['valid', 'running', 'failed', 'error_message', 'missing_id', 'known_id', 'wrong_trigger', 'missing_counter', 'negative', 'noninteger', 'bad_dates', 'reverse_dates', 'empty', 'error', 'destroy']) {
    const response = new Subject(); const notices = []; let requests = 0; let refreshes = 0
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new AmbientBrainComponent({ scan: () => { requests++; return response } }, {}, notification, {}, {})
    const oldId = '11111111-1111-4111-8111-111111111111'
    component.overview = { scans: [{ id: oldId, status: 'completed' }], opportunities: [], needs: [] }
    component.refresh = () => { refreshes++ }
    component.runScan(); component.runScan()
    assert.equal(requests, 1)
    const scan = { id: '22222222-2222-4222-8222-222222222222', trigger: 'manual', status: 'completed', startedAt: '2026-10-02T14:00:00Z', completedAt: '2026-10-02T14:01:00Z', itemsExamined: 4, opportunitiesFound: 2, created: 1, updated: 0, deduplicated: 1, advanced: 0, filtered: 0, skipped: 0, blocked: 0, manifestBytes: 200, deduplicatedBytes: 100 }
    if (mode === 'running' || mode === 'failed') scan.status = mode
    if (mode === 'error_message') scan.errorMessage = 'private-provider-token'
    if (mode === 'missing_id') delete scan.id
    if (mode === 'known_id') scan.id = oldId
    if (mode === 'wrong_trigger') scan.trigger = 'scheduled'
    if (mode === 'missing_counter') delete scan.itemsExamined
    if (mode === 'negative') scan.opportunitiesFound = -1
    if (mode === 'noninteger') scan.deduplicated = 0.5
    if (mode === 'bad_dates') scan.completedAt = 'invalid'
    if (mode === 'reverse_dates') scan.completedAt = '2026-10-02T13:00:00Z'
    if (mode === 'destroy') component.ngOnDestroy()
    if (mode === 'empty') response.complete()
    else if (mode === 'error') response.error({ error: { error: 'private-provider-token' } })
    else response.next(scan)
    assert.equal(component.scanning, false, mode)
    assert.equal(refreshes, mode === 'valid' ? 1 : 0, mode)
    if (mode !== 'valid') assert.equal(notices.some(n => n.kind === 'success'), false, mode)
    assert.doesNotMatch(JSON.stringify(notices), /private-provider-token/)
    component.runScan()
    if (mode !== 'valid') assert.equal(requests, 1, 'Uncertainty or destruction must not redispatch a scan')
    component.ngOnDestroy()
  }
})

test('ambient scan recovery requires a current overview and never launches automatically', () => {
  for (const mode of ['terminal', 'running', 'missing', 'malformed', 'empty', 'error']) {
    const overviewResponse = new Subject(); const scanResponse = new Subject(); let requests = 0
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, () => {}]))
    const component = new AmbientBrainComponent({ overview: () => overviewResponse, scan: () => { requests++; return scanResponse } }, {}, notification, {}, {})
    try {
    const old = { needs: [], opportunities: [], scans: [] }
    component.overview = old
    component.refresh()
    component.runScan()
    assert.equal(requests, 0, 'A pending status read must block scanning')
    const scan = { id: '11111111-1111-4111-8111-111111111111', status: mode === 'running' ? 'running' : 'failed', startedAt: '2026-10-02T14:00:00Z' }
    const next = { needs: [], opportunities: [], scans: [scan] }
    if (mode === 'missing') delete next.scans
    if (mode === 'malformed') scan.startedAt = 'invalid'
    if (mode === 'empty') overviewResponse.complete()
    else if (mode === 'error') overviewResponse.error(new Error('private-token'))
    else overviewResponse.next(next)
    assert.equal(component.loading, false)
    component.runScan()
    assert.equal(requests, mode === 'terminal' ? 1 : 0, mode)
    if (['missing', 'malformed', 'empty', 'error'].includes(mode)) assert.equal(component.overview, old)
    assert.doesNotMatch(component.errorMessage, /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('ambient scan metrics distinguish missing, running and invalid completed records', () => {
  const component = new AmbientBrainComponent({}, {}, {}, {}, {})
  component.overview = { scans: [] }
  assert.equal(component.scanReviewSummary(), 'Not run')
  const scan = { id: '22222222-2222-4222-8222-222222222222', status: 'completed', startedAt: '2026-10-02T14:00:00Z', completedAt: '2026-10-02T14:01:00Z', itemsExamined: 4, opportunitiesFound: 2, created: 1, updated: 0, deduplicated: 1, advanced: 0, filtered: 0, skipped: 0, blocked: 0, manifestBytes: 200, deduplicatedBytes: 100 }
  component.overview.scans = [scan]
  assert.equal(component.scanReviewSummary(), '4 reviewed')
  assert.equal(component.scanReviewDetail(), '2 opportunities found')
  scan.itemsExamined = -1
  assert.equal(component.scanReviewSummary(), 'Status unknown')
  assert.match(component.scanReviewDetail(), /Refresh status/)
  scan.status = 'running'
  assert.equal(component.scanReviewSummary(), 'Scanning')
  assert.match(component.scanReviewDetail(), /counts are unavailable/)
})

test('ambient guard suite requires matching case totals and bounds its observer lifetime', () => {
  for (const mode of ['valid', 'failed_case', 'wrong_total', 'empty_cases', 'missing_run', 'bad_id', 'bad_date', 'negative', 'duplicate_case', 'inconsistent_case', 'empty', 'error', 'destroy']) {
    const response = new Subject(); let requests = 0; let refreshes = 0; const notices = []
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new AmbientBrainComponent({}, { runStressSuite: () => { requests++; return response } }, notification, {}, {})
    component.refreshAutonomy = () => { refreshes++ }
    try {
      component.runStressSuite(); component.runStressSuite()
      assert.equal(requests, 1, 'Duplicate guard-suite dispatch must be blocked')
      const result = { run: { id: '11111111-1111-4111-8111-111111111111', passed: 1, failed: 0, createdAt: '2026-10-02T14:00:00Z' }, results: [{ name: 'approval gate', passed: true, expected: 'blocked', actual: 'blocked' }] }
      if (mode === 'failed_case') { result.run.passed = 0; result.run.failed = 1; result.results[0].passed = false; result.results[0].actual = 'allowed' }
      if (mode === 'wrong_total') result.run.passed = 2
      if (mode === 'empty_cases') { result.results = []; result.run.passed = 0 }
      if (mode === 'missing_run') delete result.run
      if (mode === 'bad_id') result.run.id = '00000000-0000-0000-0000-000000000000'
      if (mode === 'bad_date') result.run.createdAt = 'invalid'
      if (mode === 'negative') result.run.failed = -1
      if (mode === 'duplicate_case') { result.results.push({ ...result.results[0] }); result.run.passed = 2 }
      if (mode === 'inconsistent_case') result.results[0].actual = 'allowed'
      if (mode === 'destroy') component.ngOnDestroy()
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error({ error: { error: 'private-provider-token' } })
      else response.next(result)
      assert.equal(component.stressTesting, false, mode)
      assert.equal(refreshes, ['valid', 'failed_case'].includes(mode) ? 1 : 0, mode)
      assert.equal(notices.some(n => n.kind === 'success'), false, 'Reported tests are not production acceptance')
      if (mode === 'failed_case') assert.equal(notices.some(n => n.kind === 'warning'), true)
      assert.doesNotMatch(JSON.stringify(notices) + component.errorMessage, /private-provider-token/)
    } finally { component.ngOnDestroy() }
  }
})

function autonomyOverviewFixture() {
  return { generatedAt: '2026-10-02T14:00:00Z', metrics: { attempts: 0, rawCompletions: 0, completionUnderPolicy: 0, policyViolations: 0, invalidActions: 0, humanInterventions: 0, recoveryAttempts: 0, recovered: 0, averageLatencyMillis: 0, rawCompletionRate: 0, policyCompletionRate: 0, interventionRate: 0, recoveryRate: 0 }, recentActions: [], recentEvaluations: [], recentStressRuns: [], decisionDiscipline: { name: 'YAGNI gatekeeper', enabled: true, order: [], newDependenciesDefault: 'blocked', benchmarkClaims: 'unverified' }, warnings: [] }
}

test('ambient telemetry rejects invalid metrics without replacing previous evidence', () => {
  for (const mode of ['missing_metrics', 'bad_rate', 'negative', 'fraction', 'missing_records', 'bad_date', 'bad_run', 'contradictory_rate', 'excess_count', 'excess_recovery', 'empty', 'error']) {
    const response = new Subject(); const notices = []
    const component = new AmbientBrainComponent({}, { overview: () => response }, { warning: (...args) => notices.push(args) }, {}, {})
    const old = autonomyOverviewFixture(); component.autonomyOverview = old
    try {
      component.refreshAutonomy()
      const next = autonomyOverviewFixture()
      if (mode === 'missing_metrics') delete next.metrics
      if (mode === 'bad_rate') next.metrics.policyCompletionRate = 1.1
      if (mode === 'negative') next.metrics.averageLatencyMillis = -1
      if (mode === 'fraction') next.metrics.attempts = 0.5
      if (mode === 'missing_records') delete next.recentStressRuns
      if (mode === 'bad_date') next.generatedAt = 'invalid'
      if (mode === 'bad_run') next.recentStressRuns = [{ id: 'unknown', passed: -1, failed: 0 }]
      if (mode === 'contradictory_rate') next.metrics.policyCompletionRate = 0.5
      if (mode === 'excess_count') next.metrics.rawCompletions = 1
      if (mode === 'excess_recovery') { next.metrics.attempts = 2; next.metrics.recovered = 2; next.metrics.recoveryAttempts = 1 }
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error(new Error('private-token'))
      else response.next(next)
      assert.equal(component.autonomyOverview, old, mode)
      assert.equal(component.autonomyLoading, false, mode)
      assert.ok(component.autonomyErrorMessage, mode)
      assert.doesNotMatch(JSON.stringify(notices) + component.autonomyErrorMessage, /private-token/)
    } finally { component.ngOnDestroy() }
  }
})

test('ambient telemetry cancels superseded and destroyed reads', () => {
  const reads = []; const notices = []
  const component = new AmbientBrainComponent({}, { overview: () => { const subject = new Subject(); reads.push(subject); return subject } }, { warning: (...args) => notices.push(args) }, {}, {})
  try {
    component.refreshAutonomy(); component.refreshAutonomy()
    const latest = autonomyOverviewFixture()
    reads[0].next({ generatedAt: 'old' })
    assert.equal(component.autonomyOverview, undefined)
    reads[1].next(latest)
    reads[1].next({ generatedAt: 'unexpected extra response' })
    assert.equal(component.autonomyOverview, latest)
    component.refreshAutonomy(); component.ngOnDestroy()
    reads[2].error(new Error('private-token'))
    component.refreshAutonomy(); component.runStressSuite()
    assert.equal(reads.length, 3)
    assert.equal(component.autonomyOverview, latest)
    assert.deepEqual(notices, [])
  } finally { component.ngOnDestroy() }
})

test('ambient priority writes bind values and preserve newer edits or inspectors', () => {
  for (const mode of ['valid', 'foreign', 'wrong_values', 'empty', 'error', 'edit', 'reopen', 'switch', 'destroy']) {
    const response = new Subject(); const requests = []; const notices = []
    const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new AmbientBrainComponent({ updateNeed: (key, request) => { requests.push({ key, request }); return response } }, {}, notification, {}, {})
    const need = { id: 'need-1', key: 'safety', name: 'Safety', currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true, notes: ' Original note ' }
    const overview = { needs: [need], opportunities: [] }
    component.overview = overview
    component.openNeed(need)
    const draft = component.selectedNeed
    component.saveNeed(draft); component.saveNeed(draft)
    assert.equal(requests.length, 1)
    if (mode === 'edit') draft.notes = 'New unsaved note'
    if (mode === 'reopen') component.openNeed(need)
    if (mode === 'switch') component.openNeed({ ...need, id: 'need-2', key: 'growth' })
    if (mode === 'destroy') component.ngOnDestroy()
    const saved = { ...need, ...requests[0].request }
    if (mode === 'foreign') saved.key = 'growth'
    if (mode === 'wrong_values') saved.targetLevel = 99
    if (mode === 'empty') response.complete()
    else if (mode === 'error') response.error({ error: { error: 'private-provider-token' } })
    else response.next(saved)
    assert.equal(component.savingNeed, '', mode)
    if (mode === 'valid') { assert.equal(component.selectedNeed, undefined); assert.equal(component.overview.needs[0], saved) }
    else assert.notEqual(component.selectedNeed, undefined, mode)
    if (mode === 'edit') assert.equal(component.selectedNeed.notes, 'New unsaved note')
    if (mode === 'reopen') assert.notEqual(component.selectedNeed, draft)
    if (mode === 'switch') assert.equal(component.selectedNeed.key, 'growth')
    if (['foreign', 'wrong_values', 'empty', 'error', 'destroy'].includes(mode)) assert.equal(component.overview, overview, mode)
    if (mode !== 'valid') assert.equal(notices.some(n => n.kind === 'success'), false, mode)
    assert.doesNotMatch(JSON.stringify(notices), /private-provider-token/)
  }
})

test('ambient priority input rejects backend-clamped values and serializes competing writes', () => {
  for (const patch of [{ currentLevel: -1 }, { targetLevel: 101 }, { priorityWeight: 1.5 }, { currentLevel: NaN }, { targetLevel: null }, { enabled: 'true' }, { notes: {} }, { id: '' }, { key: 'unknown' }]) {
    let requested = false
    const component = new AmbientBrainComponent({ updateNeed: () => { requested = true; return of({}) } }, {}, { warning: () => {} }, {}, {})
    const original = { id: 'need-1', key: 'safety', currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true }
    component.overview = { needs: [original], opportunities: [] }
    component.openNeed({ ...original, ...patch })
    component.saveNeed(component.selectedNeed)
    assert.equal(requested, false)
    assert.equal(component.savingNeed, '')
  }
  const response = new Subject(); const requests = []
  const component = new AmbientBrainComponent({ updateNeed: (key, request) => { requests.push({ key, request }); return response } }, {}, { info: () => {}, warning: () => {} }, {}, {})
  const original = { id: 'need-1', key: 'safety', currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true }
  const other = { ...original, id: 'need-2', key: 'growth' }
  component.overview = { needs: [original, other], opportunities: [] }
  component.openNeed(original); component.saveNeed(component.selectedNeed)
  component.openNeed(other); component.saveNeed(component.selectedNeed)
  assert.equal(requests.length, 1)
  const freshOverview = { needs: [{ ...original, notes: 'Newer server snapshot' }, other], opportunities: [] }
  component.overview = freshOverview
  response.next({ ...original, ...requests[0].request })
  assert.equal(component.overview, freshOverview)
  assert.equal(component.selectedNeed.key, 'growth')
})

test('ambient decisions require current proposed records and bound terminal acknowledgements', () => {
  for (const accept of [true, false]) {
    for (const mode of ['valid', 'foreign', 'wrong_status', 'wrong_source', 'bad_approval', 'bad_risk', 'missing_title', 'empty', 'error', 'destroy', 'new_inspector']) {
      const response = new Subject(); const requests = []; const notices = []
      const service = Object.fromEntries(['accept', 'dismiss'].map(action => [action, id => { requests.push({ action, id }); return response }]))
      const notification = Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
      const component = new AmbientBrainComponent(service, {}, notification, {}, {})
      const opportunity = { ...ambientProposalFixture('opportunity-1'), needKey: 'security', sourceType: 'pursuit', sourceId: 'pursuit-1' }
      component.overview = { opportunities: [opportunity] }
      component.selectedOpportunity = opportunity
      let refreshes = 0; component.refresh = () => { refreshes++ }
      const action = accept ? 'accept' : 'dismiss'
      component[action](opportunity); component[action](opportunity)
      assert.equal(requests.length, 1, mode)
      if (mode === 'destroy') component.ngOnDestroy()
      if (mode === 'new_inspector') component.selectedOpportunity = { ...opportunity, id: 'opportunity-2' }
      const resolved = { ...opportunity, status: accept ? 'accepted' : 'dismissed' }
      if (mode === 'foreign') resolved.id = 'opportunity-2'
      if (mode === 'wrong_status') resolved.status = 'proposed'
      if (mode === 'wrong_source') resolved.sourceId = 'pursuit-2'
      if (mode === 'bad_approval') resolved.requiresApproval = 'false'
      if (mode === 'bad_risk') resolved.risk = -1
      if (mode === 'missing_title') delete resolved.title
      if (mode === 'empty') response.complete()
      else if (mode === 'error') response.error({ error: { error: 'private-provider-token' } })
      else response.next(resolved)
      assert.equal(component.resolving, '', mode)
      assert.equal(refreshes, ['valid', 'new_inspector'].includes(mode) ? 1 : 0, mode)
      assert.equal(notices.some(n => n.kind === 'success'), false, mode)
      if (mode === 'new_inspector') assert.equal(component.selectedOpportunity.id, 'opportunity-2')
      else assert.equal(component.selectedOpportunity, mode === 'valid' ? undefined : opportunity, mode)
      assert.doesNotMatch(JSON.stringify(notices), /private-provider-token/)
      component[action](opportunity)
      assert.equal(requests.length, 1, 'A terminal or uncertain decision needs fresh review before another request')
    }
  }
})

test('ambient policy inspection distinguishes reported configuration from missing or invalid flags', () => {
  const component = new AmbientBrainComponent({}, {}, {}, {}, {})
  assert.equal(component.policyFlag('executionEnabled'), 'Unavailable')
  component.overview = { policy: { executionEnabled: false, schedulerEnabled: true, suggestionOnly: true } }
  assert.equal(component.policyFlag('executionEnabled'), 'Disabled')
  assert.equal(component.policyFlag('schedulerEnabled'), 'Enabled')
  component.overview.policy.executionEnabled = 'false'
  assert.equal(component.policyFlag('executionEnabled'), 'Unavailable')
  component.overview.policy.minimumScore = 65
  assert.equal(component.policyValue('minimumScore'), '65')
  component.overview.policy.minimumScore = 101
  assert.equal(component.policyValue('minimumScore'), 'Unavailable')
  component.overview.policy.scanIntervalSeconds = -1
  assert.equal(component.policyValue('scanIntervalSeconds'), 'Unavailable')
  component.decisionsNeedRefresh = true
  assert.equal(component.policyFlag('schedulerEnabled'), 'Unavailable')
  component.decisionsNeedRefresh = false
  component.loading = true
  assert.equal(component.policyFlag('schedulerEnabled'), 'Unavailable')
  component.loading = false
  component.overview.generatedAt = 'invalid'
  assert.equal(component.policySnapshotTime(), undefined)
  component.overview.generatedAt = '2026-10-02T14:00:00Z'
  assert.equal(component.policySnapshotTime(), '2026-10-02T14:00:00Z')
})

test('ambient decision dispatch rejects invalid and stale safety context', () => {
  for (const action of ['accept', 'dismiss']) {
    for (const mode of ['bad_approval', 'bad_risk', 'stale_approval', 'stale_risk']) {
      let requests = 0
      const service = Object.fromEntries(['accept', 'dismiss'].map(key => [key, () => { requests++; return of({}) }]))
      const component = new AmbientBrainComponent(service, {}, { warning: () => {} }, {}, {})
      const current = ambientProposalFixture()
      component.overview = { opportunities: [current] }
      const inspected = { ...current }
      if (mode === 'bad_approval') inspected.requiresApproval = 'false'
      if (mode === 'bad_risk') inspected.risk = -1
      if (mode === 'stale_approval') inspected.requiresApproval = false
      if (mode === 'stale_risk') inspected.risk = 80
      component.selectedOpportunity = inspected
      component[action](inspected)
      assert.equal(requests, 0, mode)
      assert.equal(component.selectedOpportunity, inspected)
      component.ngOnDestroy()
    }
  }
})

function ambientProposalFixture(id = 'current') {
  return { id, needKey: 'safety', title: 'Review evidence', rationale: 'Source requires review', nextAction: 'Inspect source', status: 'proposed', priorityScore: 75, urgency: 80, impact: 70, effort: 20, confidence: 0.9, risk: 20, requiresApproval: true }
}

test('ambient overview rejects invalid records and ambiguous identifiers before enabling actions', () => {
  for (const mode of ['valid', 'null_need', 'duplicate_need', 'duplicate_key', 'bad_level', 'bad_enabled', 'null_proposal', 'duplicate_proposal', 'bad_status', 'bad_risk', 'bad_approval', 'bad_source', 'bad_score']) {
    const response = new Subject()
    const component = new AmbientBrainComponent({ overview: () => response }, {}, {}, {}, {})
    const old = { needs: [], opportunities: [], scans: [] }
    component.overview = old
    try {
      component.refresh()
      const need = { id: 'need-1', key: 'safety', name: 'Safety', currentLevel: 50, targetLevel: 85, priorityWeight: 100, enabled: true }
      const proposal = ambientProposalFixture()
      const next = { needs: [need], opportunities: [proposal], scans: [] }
      if (mode === 'null_need') next.needs = [null]
      if (mode === 'duplicate_need') next.needs.push({ ...need })
      if (mode === 'duplicate_key') next.needs.push({ ...need, id: 'need-2' })
      if (mode === 'bad_level') need.currentLevel = -1
      if (mode === 'bad_enabled') need.enabled = 'true'
      if (mode === 'null_proposal') next.opportunities = [null]
      if (mode === 'duplicate_proposal') next.opportunities.push({ ...proposal })
      if (mode === 'bad_status') proposal.status = 'unknown'
      if (mode === 'bad_risk') proposal.risk = -1
      if (mode === 'bad_approval') proposal.requiresApproval = 'false'
      if (mode === 'bad_source') proposal.sourceUri = { uri: 'https://example.org' }
      if (mode === 'bad_score') proposal.priorityScore = Infinity
      response.next(next)
      assert.equal(component.overview, mode === 'valid' ? next : old, mode)
      assert.equal(component.decisionsNeedRefresh, mode !== 'valid', mode)
      assert.equal(component.scanNeedsReview, mode !== 'valid', mode)
      assert.doesNotThrow(() => component.visibleNeeds())
      assert.doesNotThrow(() => component.filteredOpportunities())
    } finally { component.ngOnDestroy() }
  }
})

test('ambient overview supersedes reads and requires explicit records before enabling decisions', () => {
  for (const mode of ['valid', 'missing', 'empty', 'error', 'destroy']) {
    const requests = []; const notices = []
    const notification = Object.fromEntries(['info', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
    const component = new AmbientBrainComponent({ overview: () => { const response = new Subject(); requests.push(response); return response } }, {}, notification, {}, {})
    const previous = { opportunities: [], needs: [], scans: [] }
    component.overview = previous
    component.refresh(); component.refresh()
    requests[0].next({ opportunities: [{ id: 'stale' }], needs: [], scans: [] })
    assert.equal(component.overview, previous)
    assert.equal(component.loading, true)
    if (mode === 'destroy') component.ngOnDestroy()
    if (mode === 'empty') requests[1].complete()
    else if (mode === 'error') requests[1].error({ error: { error: 'private-provider-token' } })
    else requests[1].next(mode === 'missing' ? {} : { opportunities: [ambientProposalFixture()], needs: [], scans: [] })
    assert.equal(component.loading, false, mode)
    assert.equal(component.decisionsNeedRefresh, ['missing', 'empty', 'error'].includes(mode), mode)
    if (mode === 'valid') assert.equal(component.overview.opportunities[0].id, 'current')
    else assert.equal(component.overview, previous)
    assert.doesNotMatch(JSON.stringify(notices), /private-provider-token/)
  }
})

test('ambient decisions reject stale cards without hiding their inspector', () => {
  for (const status of ['accepted', 'dismissed', 'completed']) {
    let requested = false
    const opportunity = { id: 'opportunity-1', status, needKey: 'security' }
    const component = new AmbientBrainComponent({ accept: () => { requested = true; return of(opportunity) } }, {}, { warning: () => {} }, {}, {})
    component.selectedOpportunity = opportunity
    component.overview = { opportunities: [opportunity] }
    component.acceptSelectedOpportunity()
    assert.equal(requested, false)
    assert.equal(component.selectedOpportunity, opportunity)
  }
})

test('shared web source policy accepts explicit authorities and preserves stricter HTTPS callers', () => {
  for (const value of [undefined, null, {}, '', '//example.org', 'file:///private', 'https://', 'https://user:secret@example.org', 'https://example.org/%09']) assert.equal(safeWebSourceHref(value), undefined)
  assert.equal(safeWebSourceHref(' HTTP://localhost/source '), 'http://localhost/source')
  assert.equal(safeWebSourceHref('http://localhost/source', true), undefined)
  assert.equal(safeWebSourceHref('https://example.org/source?a=1#evidence', true), 'https://example.org/source?a=1#evidence')
})

const owner = { authenticated: true, subject: 'robert', role: 'owner', permissions: { canRead: true, canApprove: true, canOperate: true, canAdminister: true } }
const item = { claim: { id: 'claim-1', object: 'previous' }, assessment: { status: 'needs_review', truncated: false } }
const queue = { items: [item], counts: {} }
function fixture() {
  let writes = 0
  const claims = { reviewQueue: () => of(queue), lifecycle: () => of({}), correct: () => { writes++; return throwError(() => ({ status: 403 })) } }
  const auth = { session: () => of(owner) }
  const notices = []
  const notification = { error: (...args) => notices.push(args), warning: (...args) => notices.push(args), success: (...args) => notices.push(args) }
  const route = { snapshot: { queryParamMap: { get: () => null } } }
  const router = { navigate: () => Promise.resolve(true) }
  const component = new KnowledgeClaimsComponent(claims, auth, notification, route, router, { setMode() {} })
  return { component, claims, auth, notices, writes: () => writes }
}

test('matrix parameters keep module identity and do not borrow Command Center preferences', () => {
  for (const uri of ['/memory;view=records', '/memory;view=records/entry;id=1?mode=advanced#memory-records']) assert.equal(moduleForUrl(uri).id, 'memory')
  assert.equal(moduleForUrl('/knowledge-claims;workspace=hai').id, 'knowledge-claims')
  assert.equal(moduleForUrl('/memoryx;view=records').id, 'control-center')
  assert.equal(moduleForUrl('/memory%2Fentry').id, 'control-center')
  assert.equal(moduleForUrl('/memory%ZZ').id, 'control-center')
})

test('queue authorization failure revokes previous approval and preserves the correction draft', () => {
  const f = fixture()
  f.component.load()
  assert.equal(f.component.canApprove, true)
  f.component.selected = item
  f.component.correctedObject = 'draft value'
  f.claims.reviewQueue = () => throwError(() => ({ status: 401 }))
  f.component.load()
  assert.equal(f.component.canApprove, false)
  assert.equal(f.component.correctedObject, 'draft value')
  assert.match(f.component.authorityError, /sign in/i)
})

test('rejected correction cannot be repeatedly submitted using stale permissions', () => {
  const f = fixture()
  f.component.load()
  f.component.selected = item
  f.component.beginCorrection()
  f.component.correctedObject = 'draft value'
  f.component.correctionReason = 'source corrected'
  f.component.correctionConfirmed = true
  f.component.submitCorrection()
  assert.equal(f.writes(), 1)
  assert.equal(f.component.canApprove, false)
  assert.equal(f.component.correctedObject, 'draft value')
  assert.equal(f.component.correctionOpen, true)
  assert.equal(f.component.correctionConfirmed, false)
  f.component.submitCorrection()
  assert.equal(f.writes(), 1)
  f.component.load()
  assert.equal(f.component.canApprove, true)
  assert.equal(f.component.correctionConfirmed, false)
})

test('superseded queue refresh cannot restore an older actor permission', () => {
  const f = fixture()
  const oldQueue = new Subject()
  f.claims.reviewQueue = () => oldQueue
  f.component.load()
  assert.equal(f.component.canApprove, false)
  f.claims.reviewQueue = () => of(queue)
  f.auth.session = () => of({ ...owner, role: 'viewer', permissions: { ...owner.permissions, canApprove: false } })
  f.component.load()
  oldQueue.next(queue)
  oldQueue.complete()
  assert.equal(f.component.canApprove, false)
  assert.equal(f.component.session.role, 'viewer')
})

test('missing authentication and pending refresh cannot enable correction controls', () => {
  const f = fixture()
  f.component.loading = false
  f.component.session = { ...owner, authenticated: false }
  assert.equal(f.component.canApprove, false)
  f.component.session = owner
  f.component.loading = true
  assert.equal(f.component.canApprove, false)
  f.component.loading = false
  f.component.session = { ...owner, subject: '' }
  assert.equal(f.component.canApprove, false)
})

test('session failure shows recovery without discarding successfully read queue records', () => {
  const f = fixture()
  f.auth.session = () => throwError(() => ({ status: 403 }))
  f.component.load()
  assert.equal(f.component.loading, false)
  assert.equal(f.component.queue, queue)
  assert.equal(f.component.canApprove, false)
  assert.match(f.component.authorityError, /cannot approve/i)
  f.auth.session = () => throwError(() => ({ status: 503 }))
  f.component.load()
  assert.equal(f.component.canApprove, false)
  assert.match(f.component.authorityError, /could not be verified/i)
})

test('invalid effective date never sends a correction or locks the form', () => {
  const f = fixture()
  f.component.load()
  f.component.selected = item
  f.component.beginCorrection()
  f.component.correctedObject = 'draft value'
  f.component.correctionReason = 'source corrected'
  f.component.correctionEffectiveFrom = 'invalid-date'
  f.component.correctionConfirmed = true
  assert.doesNotThrow(() => f.component.submitCorrection())
  assert.equal(f.writes(), 0)
  assert.equal(f.component.saving, false)
  assert.match(f.notices[0][0], /invalid effective date/i)
})

test('superseded lifecycle responses cannot replace the inspected claim or revoke new authority', () => {
  const f = fixture()
  f.component.load()
  const oldHistory = new Subject()
  f.claims.lifecycle = () => oldHistory
  f.component.openInspector(item)
  assert.equal(oldHistory.observed, true)
  const currentHistory = { claim: { id: 'claim-2' } }
  f.claims.lifecycle = () => of(currentHistory)
  f.component.openInspector({ ...item, claim: { ...item.claim, id: 'claim-2' } })
  assert.equal(oldHistory.observed, false)
  oldHistory.error({ status: 401 })
  assert.equal(f.component.lifecycle, currentHistory)
  assert.equal(f.component.canApprove, true)
})

test('pending refresh is cancelled when a correction reports lost authority', () => {
  const f = fixture()
  const correction = new Subject()
  f.claims.correct = () => correction
  f.component.load()
  f.component.selected = item
  f.component.beginCorrection()
  f.component.correctedObject = 'draft value'
  f.component.correctionReason = 'source corrected'
  f.component.correctionConfirmed = true
  f.component.submitCorrection()
  const lateQueue = new Subject()
  f.claims.reviewQueue = () => lateQueue
  f.component.load()
  correction.error({ status: 401 })
  assert.equal(lateQueue.observed, false)
  lateQueue.next(queue)
  lateQueue.complete()
  assert.equal(f.component.canApprove, false)
  assert.equal(f.component.saving, false)
  assert.equal(f.component.correctionConfirmed, false)
})

test('destroy releases outstanding subscriptions and approval state', () => {
  const f = fixture()
  const pendingQueue = new Subject()
  const pendingLifecycle = new Subject()
  f.claims.reviewQueue = () => pendingQueue
  f.claims.lifecycle = () => pendingLifecycle
  f.component.load()
  f.component.openInspector(item)
  assert.equal(pendingQueue.observed, true)
  assert.equal(pendingLifecycle.observed, true)
  f.component.ngOnDestroy()
  assert.equal(pendingQueue.observed, false)
  assert.equal(pendingLifecycle.observed, false)
  assert.equal(f.component.canApprove, false)
})

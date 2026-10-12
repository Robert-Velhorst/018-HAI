import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import '@angular/compiler'
import { FormBuilder } from '@angular/forms'
import { of, Subject } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const require = createRequire(import.meta.url)
const { PursuitsComponent } = await loadComponentLogic(new URL('../src/app/pages/pursuits/pursuits.component.ts', import.meta.url), {
  '@angular/forms': pathToFileURL(require.resolve('@angular/forms')).href,
})
const { PursuitService } = await loadComponentLogic(new URL('../src/app/services/pursuit.service.ts', import.meta.url))
const card = { pursuit: { id: 'pursuit-1' }, decision: { id: 'decision-1' } }
function fixture() {
  const requests = []
  const evidenceRequests = []
  const mutationRequests = []
  const workflowRequests = []
  const delegationRequests = []
  const summaryRequests = []
  const reviewRequests = []
  const planRequests = []
  const contextRequests = []
  const linkRequests = []
  const detachRequests = []
  const intakeRequests = []
  const routeRequests = []
  const resourceRequests = []
  const ledgerRequests = []
  const releaseRequests = []
  const confirmations = []
  const resolved = []
  const notices = []
  const notification = Object.fromEntries(['info', 'success', 'warning', 'error'].map(kind => [kind, (...args) => notices.push({ kind, args })]))
  const service = {
    deleteLink: (id, linkId) => { const response = new Subject(); detachRequests.push({ id, linkId, response }); return response },
    link: (id, request) => { const response = new Subject(); linkRequests.push({ id, request, response }); return response },
    releaseResourceReservation: (id, reservationId, reason) => { const response = new Subject(); releaseRequests.push({ id, reservationId, reason, response }); return response },
    resourceEvents: id => { const response = new Subject(); ledgerRequests.push({ id, response }); return response },
    appendResourceEvent: (id, request) => { const response = new Subject(); resourceRequests.push({ id, request, response }); return response },
    routeIntake: request => { const response = new Subject(); routeRequests.push({ request, response }); return response },
    intake: (id, request) => { const response = new Subject(); intakeRequests.push({ id, request, response }); return response },
    update: (id, request) => { const response = new Subject(); contextRequests.push({ id, request, response }); return response },
    plan: (id, request) => { const response = new Subject(); planRequests.push({ id, request, response }); return response },
    review: (id, request) => { const response = new Subject(); reviewRequests.push({ id, request, response }); return response },
    refreshSummary: id => { const response = new Subject(); summaryRequests.push({ id, response }); return response },
    delegationPackage: id => { const response = new Subject(); delegationRequests.push({ id, response }); return response },
    get: id => { const response = new Subject(); requests.push({ id, response }); return response },
    resolveEvidence: (id, uri) => { const response = new Subject(); evidenceRequests.push({ id, uri, response }); return response },
    resolveDecision: (id, request) => { const response = new Subject(); mutationRequests.push({ id, request, response }); return response },
    acceptCandidate: (id, request) => { const response = new Subject(); mutationRequests.push({ id, request, response }); return response },
    archive: (id, archived) => { const response = new Subject(); mutationRequests.push({ id, archived, response }); return response },
    reopen: id => { const response = new Subject(); mutationRequests.push({ id, response }); return response },
  }
  const workflowService = {
    resolveApproval: (id, request) => { const response = new Subject(); workflowRequests.push({ id, request, response }); return response },
    resolveProposal: (id, proposalId, request) => { const response = new Subject(); workflowRequests.push({ id, proposalId, request, response }); return response },
  }
  const component = new PursuitsComponent(new FormBuilder(), service, {}, workflowService, notification, { confirm: options => { confirmations.push(options) } }, {}, {}, {})
  component.canResolveDecision = () => true
  component.loadProjectDossier = () => {}
  component.setSelectedQuery = () => {}
  component.resetResourceLedger = () => {}
  component.openRequestedEvidence = () => {}
  component.resolveDecision = (...args) => resolved.push(args)
  component.load = () => {}
  return { component, requests, evidenceRequests, mutationRequests, workflowRequests, delegationRequests, summaryRequests, reviewRequests, planRequests, contextRequests, linkRequests, detachRequests, intakeRequests, routeRequests, resourceRequests, ledgerRequests, releaseRequests, confirmations, resolved, notices }
}

test('source navigation never opens executable, malformed, credentialed or local file URLs', () => {
  const previousWindow = globalThis.window
  const opened = []
  globalThis.window = { open: (...args) => opened.push(args) }
  try {
    for (const selected of [false, true]) {
      for (const uri of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'blob:https://example.org/id', 'https://user:secret@example.org', 'https://', 'https://example.org\\evil', 'mailto:person@example.org?subject=test%0aBcc:other@example.org', 'tel:javascript:alert(1)', 'file:///C:/private/evidence.pdf']) {
        const f = fixture()
        if (selected) f.component.selected = { pursuit: { id: 'pursuit-1' } }
        f.component.openSource(uri)
        assert.equal(opened.length, 0, uri)
        assert.equal(f.evidenceRequests.length, selected && uri.startsWith('file:') ? 1 : 0, uri)
      }
    }
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

test('valid source links open safely and internal evidence requires selected context', () => {
  const previousWindow = globalThis.window
  const opened = []
  globalThis.window = { open: (...args) => opened.push(args) }
  try {
    const f = fixture()
    for (const uri of ['https://example.org/evidence', 'http://localhost/source', 'mailto:person@example.org?subject=Review', 'tel:+31-612345678']) f.component.openSource(uri)
    assert.equal(opened.length, 4)
    assert.equal(opened.every(args => args[1] === '_blank' && args[2] === 'noopener,noreferrer'), true)
    f.component.openSource('local://source-1')
    assert.equal(opened.length, 4)
    assert.equal(f.evidenceRequests.length, 0)
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.openSource('local://source-1')
    assert.equal(f.evidenceRequests[0].id, 'pursuit-1')
    f.evidenceRequests[0].response.complete()
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

test('link detachment confirms the original record and handles uncertainty without redirecting', () => {
  for (const mode of ['valid', 'switch_before', 'switch_after', 'removed', 'cancel', 'empty', 'error', 'unconfirmed', 'destroy_before', 'destroy_after']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' }, links: [{ id: 'link-1', pursuitId: 'pursuit-1', linkId: 'record-1', linkType: 'workflow' }] }
    f.component.selected = original
    f.component.deleteLink('link-1'); f.component.deleteLink('link-1')
    assert.equal(f.detachRequests.length, 0, mode)
    assert.equal(f.confirmations.length, 1, mode)
    if (mode === 'switch_before') f.component.selected = { pursuit: { id: 'pursuit-2' }, links: [] }
    if (mode === 'removed') original.links = []
    if (mode === 'destroy_before') f.component.ngOnDestroy()
    if (mode === 'cancel') f.confirmations[0].nzOnCancel()
    else { f.confirmations[0].nzOnOk(); f.confirmations[0].nzOnOk() }
    if (['switch_before', 'removed', 'cancel', 'destroy_before'].includes(mode)) { assert.equal(f.detachRequests.length, 0, mode); continue }
    assert.equal(f.detachRequests.length, 1, mode)
    if (mode === 'switch_after') f.component.selected = { pursuit: { id: 'pursuit-2' }, links: [] }
    if (mode === 'destroy_after') f.component.ngOnDestroy()
    if (mode === 'empty') f.detachRequests[0].response.complete()
    else if (mode === 'error') f.detachRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.detachRequests[0].response.next(mode !== 'unconfirmed')
    assert.equal(f.component.detachingLinkId, '', mode)
    assert.equal(f.requests.length, mode === 'valid' ? 1 : 0, mode)
    assert.equal(f.notices.some(n => n.kind === 'success'), false, mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('real link detachment service requires 204 and an explicit scoped link readback', () => {
  for (const mode of ['absent', 'present', 'missing', 'foreign', 'malformed', 'wrong_status']) {
    const deleteResponse = new Subject()
    const readback = new Subject()
    const reads = []
    const service = new PursuitService({ delete: () => deleteResponse, get: url => { reads.push(url); return readback } })
    const results = []; const errors = []
    service.deleteLink('pursuit-1', 'link-1').subscribe({ next: value => results.push(value), error: error => errors.push(error) })
    deleteResponse.next({ status: mode === 'wrong_status' ? 200 : 204 })
    if (mode !== 'wrong_status') {
      const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' }, links: mode === 'present' ? [{ id: 'link-1', pursuitId: 'pursuit-1' }] : [] }
      if (mode === 'missing') delete detail.links
      if (mode === 'malformed') detail.links = [{ id: 'link-2', pursuitId: 'pursuit-2' }]
      readback.next(detail)
    }
    assert.deepEqual(results, mode === 'absent' ? [true] : mode === 'present' ? [false] : [], mode)
    assert.equal(errors.length, ['absent', 'present'].includes(mode) ? 0 : 1, mode)
    assert.equal(reads.length, mode === 'wrong_status' ? 0 : 1, mode)
  }
})

test('an obsolete detach dialog cannot dispatch or cancel its replacement', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' }, links: [{ id: 'link-1', pursuitId: 'pursuit-1' }] }
  f.component.deleteLink('link-1')
  f.confirmations[0].nzOnCancel()
  f.component.deleteLink('link-1')
  f.confirmations[0].nzOnOk(); f.confirmations[0].nzOnCancel()
  assert.equal(f.detachRequests.length, 0)
  assert.equal(f.component.linkConfirmationId, 'link-1')
  f.confirmations[1].nzOnOk()
  assert.equal(f.detachRequests.length, 1)
  f.component.linkForm.patchValue({ linkId: 'new-record' })
  f.component.addLink()
  assert.equal(f.linkRequests.length, 0)
  f.detachRequests[0].response.complete()
})

test('detachment stops observation on unsubscribe and cannot repeat delete acknowledgements', () => {
  const deletion = new Subject(); const readback = new Subject(); const reads = []
  const service = new PursuitService({ delete: () => deletion, get: url => { reads.push(url); return readback } })
  const results = []
  const subscription = service.deleteLink('pursuit-1', 'link-1').subscribe(value => results.push(value))
  deletion.next({ status: 204 }); deletion.next({ status: 204 })
  assert.equal(reads.length, 1)
  subscription.unsubscribe()
  readback.next({ pursuit: { id: 'pursuit-1' }, links: [] })
  assert.deepEqual(results, [])
})

test('record linking validates acknowledgement and preserves newer drafts and selections', () => {
  for (const mode of ['valid', 'foreign', 'wrong_record', 'wrong_source', 'missing_id', 'empty', 'error', 'edit', 'switch', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' }, links: [] }
    f.component.selected = original
    f.component.linkForm.patchValue({ linkId: 'workflow-1', sourceUri: 'https://example.org/evidence' })
    f.component.addLink(); f.component.addLink()
    assert.equal(f.linkRequests.length, 1, mode)
    const saved = { id: 'link-1', pursuitId: 'pursuit-1', ...f.linkRequests[0].request }
    if (mode === 'foreign') saved.pursuitId = 'pursuit-2'
    if (mode === 'wrong_record') saved.linkId = 'workflow-2'
    if (mode === 'wrong_source') saved.sourceUri = 'https://example.org/unrelated'
    if (mode === 'missing_id') delete saved.id
    if (mode === 'edit') f.component.linkForm.patchValue({ linkId: 'new-unsaved-record' })
    if (mode === 'switch') f.component.selected = { pursuit: { id: 'pursuit-2' }, links: [] }
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.linkRequests[0].response.complete()
    else if (mode === 'error') f.linkRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.linkRequests[0].response.next(saved)
    assert.equal(f.component.linkSaving, false, mode)
    assert.equal(f.component.linkForm.value.linkId, mode === 'valid' ? '' : mode === 'edit' ? 'new-unsaved-record' : 'workflow-1', mode)
    assert.equal(f.requests.length, mode === 'valid' ? 1 : 0, mode)
    if (mode === 'valid') assert.equal(f.requests[0].id, 'pursuit-1')
    if (mode === 'switch') assert.equal(f.component.selected.pursuit.id, 'pursuit-2')
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('record linking rejects blank identities, invalid types, and silently defaulted confidence', () => {
  for (const value of [{ linkId: ' ' }, { linkType: 'unknown' }, { confidence: 0 }, { confidence: -1 }, { confidence: NaN }, { confidence: 2 }]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.linkForm.patchValue({ linkId: 'workflow-1', ...value })
    f.component.addLink()
    assert.equal(f.linkRequests.length, 0)
    assert.equal(f.component.linkSaving, false)
  }
})

test('reservation confirmation cannot dispatch after selection changes or destruction', () => {
  for (const mode of ['valid', 'switch', 'removed', 'edited_reason', 'destroy', 'cancel']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' }, resourceUsage: { reservations: [{ id: 'reservation-1', operationId: 'operation-1' }] } }
    f.component.reservationReleaseReasons['reservation-1'] = 'Confirmed stopped after inspection'
    f.component.releaseReservation('reservation-1'); f.component.releaseReservation('reservation-1')
    assert.equal(f.confirmations.length, 1)
    if (mode === 'switch') f.component.selected = { pursuit: { id: 'pursuit-2' } }
    if (mode === 'removed') f.component.selected.resourceUsage.reservations = []
    if (mode === 'edited_reason') f.component.reservationReleaseReasons['reservation-1'] = 'A new reason needs review'
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'cancel') f.confirmations[0].nzOnCancel()
    f.confirmations[0].nzOnOk(); f.confirmations[0].nzOnOk()
    assert.equal(f.releaseRequests.length, mode === 'valid' ? 1 : 0, mode)
  }
})

test('reservation response must omit the original hold without overwriting a newer selection', () => {
  for (const mode of ['valid', 'still_held', 'missing', 'error', 'empty', 'switch', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' }, resourceUsage: { reservations: [{ id: 'reservation-1', operationId: 'operation-1' }] } }
    const reads = []
    f.component.loadPursuitDetail = () => reads.push(true)
    f.component.selected = original
    f.component.reservationReleaseReasons['reservation-1'] = 'Confirmed stopped after inspection'
    f.component.releaseReservation('reservation-1')
    f.confirmations[0].nzOnOk()
    const usage = { available: true, activeReservations: mode === 'still_held' ? 1 : 0, reservations: mode === 'still_held' ? original.resourceUsage.reservations : [] }
    if (mode === 'missing') delete usage.reservations
    if (mode === 'switch') f.component.selected = { pursuit: { id: 'pursuit-2' } }
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'error') f.releaseRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else if (mode === 'empty') f.releaseRequests[0].response.complete()
    else f.releaseRequests[0].response.next(usage)
    assert.equal(f.component.releasingReservationId, '', mode)
    assert.equal(reads.length, mode === 'valid' ? 1 : 0, mode)
    if (mode === 'valid') assert.equal(f.component.selected, original)
    else if (mode === 'switch') assert.equal(f.component.selected.pursuit.id, 'pursuit-2')
    else assert.equal(f.component.selected, original, mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('an older confirmation cannot cancel or authorize its replacement', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' }, resourceUsage: { reservations: [{ id: 'reservation-1', operationId: 'operation-1' }] } }
  f.component.reservationReleaseReasons['reservation-1'] = 'Confirmed stopped after inspection'
  f.component.releaseReservation('reservation-1')
  f.confirmations[0].nzOnCancel()
  f.component.releaseReservation('reservation-1')
  f.confirmations[0].nzOnOk(); f.confirmations[0].nzOnCancel()
  assert.equal(f.releaseRequests.length, 0)
  f.confirmations[1].nzOnOk()
  assert.equal(f.releaseRequests.length, 1)
})

test('ledger read rejects foreign or malformed data and distinguishes empty from unavailable', () => {
  for (const mode of ['valid', 'zero', 'foreign', 'duplicate', 'missing', 'error', 'empty', 'switch', 'destroy']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.loadResourceEvents()
    const event = { id: 'event-1', pursuitId: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1', kind: 'effort_recorded', effortMinutes: 30, amountMinor: 0 }
    const rows = mode === 'missing' ? undefined : mode === 'zero' ? [] : mode === 'duplicate' ? [event, event] : [event]
    if (mode === 'switch') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'error') f.ledgerRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else if (mode === 'empty') f.ledgerRequests[0].response.complete()
    else f.ledgerRequests[0].response.next(rows)
    assert.equal(f.component.resourceEventsLoading, false, mode)
    assert.equal(f.component.resourceEventsLoadedFor, ['valid', 'zero'].includes(mode) ? 'pursuit-1' : '', mode)
    assert.equal(f.component.resourceEvents.length, mode === 'valid' ? 1 : 0, mode)
    if (['foreign', 'duplicate', 'missing', 'error', 'empty'].includes(mode)) assert.ok(f.component.resourceEventsError, mode)
    assert.doesNotMatch(f.component.resourceEventsError, /private-provider-token/)
  }
})

test('real ledger service never invents an empty events list', () => {
  for (const raw of [undefined, null, {}, { events: null }, { events: {} }, { events: [] }]) {
    const service = new PursuitService({ get: () => of(raw) })
    let result, failure
    service.resourceEvents('pursuit-1').subscribe({ next: value => { result = value }, error: error => { failure = error } })
    if (Array.isArray(raw?.events)) { assert.deepEqual(result, []); assert.equal(failure, undefined) }
    else { assert.equal(result, undefined); assert.ok(failure) }
  }
})

test('ledger can retry missing responses and rejects invalid quantities through the real service', () => {
  for (const variant of ['valid', 'negative', 'fractional', 'unknown_kind', 'wrong_currency']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.loadResourceEvents()
    f.ledgerRequests[0].response.complete()
    assert.equal(f.component.resourceEventsLoadedFor, '')
    const event = { id: 'event-1', pursuitId: 'pursuit-1', kind: variant === 'unknown_kind' ? 'invented' : 'spend_incurred',
      effortMinutes: 0, amountMinor: variant === 'negative' ? -100 : variant === 'fractional' ? 1.5 : 100,
      currency: variant === 'wrong_currency' ? 'USD' : 'EUR' }
    f.component.pursuitsService = new PursuitService({ get: () => of({ events: [event] }) })
    f.component.loadResourceEvents()
    assert.equal(f.component.resourceEventsLoadedFor, variant === 'valid' ? 'pursuit-1' : '')
    assert.equal(f.component.resourceEventsLoading, false)
    assert.equal(f.component.resourceEvents.length, variant === 'valid' ? 1 : 0)
  }
})

test('resource append validates receipt binding without clearing new input or redirecting', () => {
  for (const mode of ['valid', 'foreign', 'wrong_key', 'wrong_amount', 'empty', 'error', 'switch', 'edit', 'destroy']) {
    const f = fixture()
    const refreshes = []
    f.component.loadPursuitDetail = (...args) => refreshes.push(args)
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resourceEventForm.patchValue({ kind: 'effort_recorded', effortHours: 0.5, note: 'Original note', idempotencyKey: 'key-1' })
    f.component.recordResourceEvent(); f.component.recordResourceEvent()
    assert.equal(f.resourceRequests.length, 1)
    const event = { id: 'event-1', pursuitId: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1', kind: 'effort_recorded',
      idempotencyKey: mode === 'wrong_key' ? 'key-2' : 'key-1', effortMinutes: mode === 'wrong_amount' ? 90 : 30, amountMinor: 0 }
    if (mode === 'switch') f.component.selected = { pursuit: { id: 'pursuit-2' } }
    if (mode === 'edit') f.component.resourceEventForm.patchValue({ note: 'New unsent note' })
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.resourceRequests[0].response.complete()
    else if (mode === 'error') f.resourceRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.resourceRequests[0].response.next(event)
    assert.equal(f.component.resourceEventSaving, false, mode)
    assert.equal(refreshes.length, mode === 'valid' ? 1 : 0, mode)
    assert.equal(f.component.resourceEventForm.value.note, mode === 'valid' ? '' : mode === 'edit' ? 'New unsent note' : 'Original note', mode)
    if (!['valid', 'edit'].includes(mode)) assert.equal(f.component.resourceEventForm.value.idempotencyKey, 'key-1', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('resource append validates numeric input and euro spend/refund receipts', () => {
  for (const value of [{ effortHours: NaN }, { effortHours: Infinity }, { effortHours: -1 }, { effortHours: 0.001 },
    { occurredAt: 'invalid-date' }, { kind: 'invented' }, { idempotencyKey: '   ' }]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resourceEventForm.patchValue(value)
    f.component.recordResourceEvent()
    assert.equal(f.resourceRequests.length, 0)
    assert.equal(f.component.resourceEventSaving, false)
  }
  for (const kind of ['spend_incurred', 'spend_refund']) for (const currency of ['EUR', 'USD']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resourceEventForm.patchValue({ kind, spendEur: 12.34, idempotencyKey: 'money-key' })
    f.component.loadPursuitDetail = () => {}
    f.component.recordResourceEvent()
    const event = { id: 'event-1', pursuitId: 'pursuit-1', kind, idempotencyKey: 'money-key', effortMinutes: 0, amountMinor: 1234, currency }
    f.resourceRequests[0].response.next(event)
    f.resourceRequests[0].response.next(event)
    assert.equal(f.component.resourceEvents.length, currency === 'EUR' ? 1 : 0)
    assert.equal(f.component.resourceEventSaving, false)
    if (currency !== 'EUR') assert.equal(f.component.resourceEventForm.value.idempotencyKey, 'money-key')
  }
})

test('global intake validates routing modes and preserves changed input or selection', () => {
  for (const mode of ['existing', 'candidate', 'matched_candidate', 'foreign', 'missing_work', 'unknown', 'workflow_missing', 'empty', 'error', 'edit', 'switch', 'destroy']) {
    const f = fixture()
    const routes = []
    f.component.selectPursuitById = (...args) => routes.push(args)
    const original = { pursuit: { id: 'pursuit-1' } }
    f.component.selected = original
    f.component.routedIntakeForm.patchValue({ input: 'Real request', sourceLabel: 'Evidence reference' })
    f.component.routeIntake(); f.component.routeIntake()
    assert.equal(f.routeRequests.length, 1)
    const candidate = ['candidate', 'matched_candidate'].includes(mode)
    const result = { mode: mode === 'candidate' ? 'candidate_created' : mode === 'matched_candidate' ? mode : mode === 'unknown' ? 'invented' : mode === 'workflow_missing' ? mode : 'matched_existing',
      matched: mode !== 'candidate', createdCandidate: mode === 'candidate', pursuitId: 'pursuit-2',
      detail: { pursuit: { id: mode === 'foreign' ? 'pursuit-3' : 'pursuit-2', sourceOfCreation: candidate ? 'intake_pursuit_candidate' : 'manual' },
        workflows: candidate || mode === 'missing_work' ? [] : [{ id: 'workflow-1' }],
        links: candidate ? [] : [{ pursuitId: 'pursuit-2', linkType: 'workflow', linkId: 'workflow-1' }] }, message: 'private-provider-token' }
    if (mode === 'edit') f.component.routedIntakeForm.patchValue({ input: 'New unsent request' })
    if (mode === 'switch') f.component.selected = { pursuit: { id: 'pursuit-3' } }
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.routeRequests[0].response.complete()
    else if (mode === 'error') f.routeRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.routeRequests[0].response.next(result)
    assert.equal(f.component.routedIntakeRunning, false, mode)
    const valid = ['existing', 'candidate', 'matched_candidate'].includes(mode)
    assert.equal(routes.length, valid ? 1 : 0, mode)
    assert.equal(f.component.routedIntakeForm.value.input, valid ? '' : mode === 'edit' ? 'New unsent request' : 'Real request', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
    if (candidate) assert.equal(f.notices.some(n => n.kind === 'success'), false)
  }
})

test('global routing rejects contradictory candidate flags and respects pending navigation', () => {
  for (const mode of ['valid_after_workflow', 'contradictory', 'pending_navigation', 'blank']) {
    const f = fixture()
    const routes = []
    f.component.selectPursuitById = (...args) => routes.push(args)
    f.component.routedIntakeForm.patchValue({ input: mode === 'blank' ? '   ' : 'Real request', sourceUri: 'local://source-1' })
    f.component.routeIntake()
    if (mode === 'blank') { assert.equal(f.routeRequests.length, 0); continue }
    assert.equal(f.routeRequests[0].request.sourceUri, 'local://source-1')
    const result = { mode: 'matched_after_workflow', matched: true, createdCandidate: mode === 'contradictory', pursuitId: 'pursuit-2',
      detail: { pursuit: { id: 'pursuit-2', sourceOfCreation: 'manual' }, workflows: [{ id: 'workflow-1' }],
        links: [{ pursuitId: 'pursuit-2', linkType: 'workflow', linkId: 'workflow-1' }] } }
    if (mode === 'pending_navigation') { f.component.requestedPursuitId = 'pursuit-3'; f.component.detailLoading = true }
    f.routeRequests[0].response.next(result)
    f.routeRequests[0].response.next(result)
    assert.equal(routes.length, mode === 'valid_after_workflow' ? 1 : 0)
    assert.equal(f.component.routedIntakeRunning, false)
    if (mode === 'pending_navigation') assert.equal(f.component.detailLoading, true)
  }
})

test('project intake requires linked work and cannot overwrite changed selection or input', () => {
  for (const mode of ['valid', 'foreign', 'no_work', 'no_link', 'foreign_link', 'unsafe', 'existing', 'empty', 'error', 'switch', 'edit', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1', riskLevel: 'high' }, workflows: mode === 'existing' ? [{ id: 'workflow-1' }] : [] }
    f.component.selected = original
    f.component.intakeForm.patchValue({ input: 'Actual incoming request' })
    f.component.runIntake(); f.component.runIntake()
    assert.equal(f.intakeRequests.length, 1)
    assert.equal(f.intakeRequests[0].request.requiresReview, true)
    const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' },
      workflows: [{ id: 'workflow-1', requiresApproval: mode !== 'unsafe', approvalStatus: 'pending' }],
      links: [{ pursuitId: mode === 'foreign_link' ? 'pursuit-2' : 'pursuit-1', linkType: 'workflow', linkId: 'workflow-1', relationship: 'operational_work' }] }
    if (mode === 'no_work') detail.workflows = []
    if (mode === 'no_link') detail.links = []
    if (mode === 'switch') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'edit') f.component.intakeForm.patchValue({ input: 'New unsent request' })
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.intakeRequests[0].response.complete()
    else if (mode === 'error') f.intakeRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.intakeRequests[0].response.next(detail)
    assert.equal(f.component.intakeRunning, false, mode)
    if (mode === 'valid' || mode === 'existing') assert.equal(f.component.selected, detail)
    else if (mode === 'switch') { assert.equal(f.component.selected, undefined); assert.equal(f.component.detailLoading, true) }
    else assert.equal(f.component.selected, original, mode)
    if (mode === 'edit') assert.equal(f.component.intakeForm.value.input, 'New unsent request')
    assert.equal(f.notices.some(n => n.kind === 'success'), mode === 'valid', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('intake starts empty and rejects whitespace-only inputs', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' } }
  assert.equal(f.component.intakeForm.value.input, '')
  f.component.runIntake()
  f.component.intakeForm.patchValue({ input: '   ' })
  f.component.runIntake()
  assert.equal(f.intakeRequests.length, 0)
  assert.equal(f.component.intakeRunning, false)
})

test('intake sends original source context once and preserves a fresher same-pursuit snapshot', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1', projectKey: 'project-1', riskLevel: 'low', autonomyLevel: 'approve_before_execute' }, workflows: [] }
  f.component.intakeForm.patchValue({ input: 'Real request', sourceId: 'source-1', sourceUri: 'local://source-1', sourceLabel: 'Source reference' })
  f.component.runIntake()
  const request = f.intakeRequests[0].request
  assert.equal(request.sourceId, 'source-1')
  assert.equal(request.sourceUri, 'local://source-1')
  assert.equal(request.projectKey, 'project-1')
  assert.equal(request.requiresReview, true)
  const newer = { pursuit: { id: 'pursuit-1' }, summary: { currentState: 'Newer' } }
  f.component.selected = newer
  const detail = { pursuit: { id: 'pursuit-1' }, workflows: [{ id: 'workflow-1', requiresApproval: true, approvalStatus: 'pending' }],
    links: [{ pursuitId: 'pursuit-1', linkType: 'workflow', linkId: 'workflow-1', relationship: 'operational_work' }] }
  f.intakeRequests[0].response.next(detail)
  f.intakeRequests[0].response.next(detail)
  assert.equal(f.component.selected, newer)
  assert.equal(f.component.intakeRunning, false)
  assert.equal(f.notices.some(n => n.kind === 'success'), false)
})

test('context save preserves newer editors and requires a matching pursuit response', () => {
  for (const mode of ['foreign', 'valid', 'empty', 'error', 'switch', 'edit', 'reopen', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1', successCriteria: [{ id: 'criterion-1', description: 'Evidence reviewed' }], stopConditions: [{ id: 'stop-1', description: 'Await approval' }] } }
    f.component.selected = original
    f.component.openContextEditor()
    f.component.savePursuitContext(); f.component.savePursuitContext()
    assert.equal(f.contextRequests.length, 1)
    if (mode === 'switch') {
      f.component.selectPursuit({ id: 'pursuit-2' }, false)
      f.requests[0].response.next({ pursuit: { ...original.pursuit, id: 'pursuit-2' } })
      f.component.openContextEditor()
    }
    if (mode === 'edit') f.component.contextForm.patchValue({ description: 'New unsaved text' })
    if (mode === 'reopen') f.component.openContextEditor()
    if (mode === 'destroy') f.component.ngOnDestroy()
    const saved = { ...original.pursuit, ...f.contextRequests[0].request, id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' }
    saved.successCriteria = saved.successCriteria.map(item => ({ ...item, status: item.status || 'pending', evidenceRequired: item.evidenceRequired ?? false }))
    saved.stopConditions = saved.stopConditions.map(item => ({ ...item, status: item.status || 'monitoring' }))
    if (mode === 'empty') f.contextRequests[0].response.complete()
    else if (mode === 'error') f.contextRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.contextRequests[0].response.next(saved)
    assert.equal(f.component.showContextEditor, mode !== 'valid', mode)
    assert.equal(f.component.contextSaving, false, mode)
    if (mode === 'valid') assert.equal(f.component.selected.pursuit, saved)
    else if (mode === 'switch') assert.equal(f.component.selected.pursuit.id, 'pursuit-2')
    else assert.equal(f.component.selected, original, mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('context acknowledgement must include the submitted outcome and resource contract', () => {
  for (const mismatch of ['description', 'successCriteria', 'stopConditions', 'dependencies', 'resourceLimits', 'targetAt', 'reviewCadenceDays']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1', successCriteria: [{ id: 'criterion-1', description: 'Evidence reviewed', status: 'pending', evidenceRequired: true }], stopConditions: [{ id: 'stop-1', description: 'Await approval', status: 'monitoring' }], dependencies: [] } }
    f.component.selected = original
    f.component.openContextEditor()
    f.component.contextForm.patchValue({ description: 'Updated context', maxSpendEur: 25, targetAt: '2026-10-10', reviewCadenceDays: 7 })
    f.component.savePursuitContext()
    const response = structuredClone({ id: 'pursuit-1', ...f.contextRequests[0].request })
    if (mismatch === 'description') response.description = 'Old context'
    else if (mismatch === 'resourceLimits') response.resourceLimits.maxSpendEur = 250
    else if (mismatch === 'targetAt') response.targetAt = '2026-10-11T00:00:00Z'
    else if (mismatch === 'reviewCadenceDays') response.reviewCadenceDays = 30
    else delete response[mismatch]
    f.contextRequests[0].response.next(response)
    assert.equal(f.component.showContextEditor, true, mismatch)
    assert.equal(f.component.selected, original, mismatch)
    assert.equal(f.component.contextSaving, false, mismatch)
    assert.equal(f.notices.some(n => n.kind === 'success'), false, mismatch)
  }
})

test('context comparison accepts backend normalization but rejects changed safety boundaries', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1', successCriteria: [{ id: 'criterion-1', description: 'Evidence reviewed', status: ' Pending ', evidenceRequired: true }], stopConditions: [{ id: 'stop-1', description: 'Await approval', status: ' Triggered ' }], dependencies: [{ id: 'dependency-1', label: ' Reply ', status: ' Pending ' }] } }
  f.component.openContextEditor()
  f.component.contextForm.patchValue({ description: ' Context ', targetAt: '2026-10-10' })
  f.component.savePursuitContext()
  const request = f.contextRequests[0].request
  const returned = structuredClone({ id: 'pursuit-1', ...request })
  returned.description = 'Context'
  returned.targetAt = '2026-10-10T02:00:00+02:00'
  returned.successCriteria[0].status = 'pending'
  returned.stopConditions[0].status = 'triggered'
  returned.stopConditions[0].triggeredAt = '2026-10-02T14:00:00Z'
  returned.dependencies[0].status = 'pending'
  returned.resourceLimits = {}
  assert.equal(f.component.contextResponseMatches(returned, request), true)
  for (const change of [
    record => { record.successCriteria[0].evidenceRequired = false },
    record => { record.successCriteria[0].id = 'another-criterion' },
    record => { record.stopConditions[0].description = 'Ignore approval' },
    record => { record.dependencies[0].label = 'Unrelated prerequisite' },
    record => { record.dependencies[0].dueAt = '2026-10-11T00:00:00Z' },
    record => { record.resourceLimits.maxEffortHours = NaN },
  ]) {
    const altered = structuredClone(returned)
    change(altered)
    assert.equal(f.component.contextResponseMatches(altered, request), false)
  }
  f.contextRequests[0].response.next(returned)
  assert.equal(f.component.showContextEditor, false)
})

test('first workflow planning requires bound provenance and approval context', () => {
  for (const mode of ['valid', 'foreign', 'no_work', 'no_link', 'foreign_link', 'no_provenance', 'unsafe', 'existing', 'empty', 'error', 'switch', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1', riskLevel: 'high' }, workflows: mode === 'existing' ? [{ id: 'workflow-1' }] : [],
      actionQueues: { systemReady: [{ label: 'Create the first workflow item for this pursuit' }] } }
    f.component.selected = original
    f.component.createFirstWorkflowPlan(); f.component.createFirstWorkflowPlan()
    assert.equal(f.planRequests.length, 1)
    assert.equal(f.planRequests[0].request.requiresReview, true)
    const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' },
      workflows: [{ id: 'workflow-1', sourceType: 'pursuit', sourceId: mode === 'no_provenance' ? 'pursuit-2' : 'pursuit-1', requiresApproval: mode !== 'unsafe', approvalStatus: 'pending' }],
      links: [{ pursuitId: mode === 'foreign_link' ? 'pursuit-2' : 'pursuit-1', linkType: 'workflow', linkId: 'workflow-1', relationship: 'first_workflow_plan' }] }
    if (mode === 'no_work') detail.workflows = []
    if (mode === 'no_link') detail.links = []
    if (mode === 'switch') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.planRequests[0].response.complete()
    else if (mode === 'error') f.planRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.planRequests[0].response.next(detail)
    assert.equal(f.component.planning, false, mode)
    if (mode === 'valid' || mode === 'existing') assert.equal(f.component.selected, detail)
    else if (mode === 'switch') { assert.equal(f.component.selected, undefined); assert.equal(f.component.detailLoading, true) }
    else assert.equal(f.component.selected, original, mode)
    assert.equal(f.notices.some(n => n.kind === 'success'), mode === 'valid', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('planning honors approval autonomy and never replaces a fresher same-pursuit detail', () => {
  for (const autonomyLevel of ['approve_before_execute', 'draft_only']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1', riskLevel: 'low', autonomyLevel }, workflows: [],
      actionQueues: { systemReady: [{ label: 'Create the first workflow item for this pursuit' }] } }
    f.component.createFirstWorkflowPlan()
    assert.equal(f.planRequests[0].request.requiresReview, autonomyLevel === 'approve_before_execute')
    const newer = { pursuit: { id: 'pursuit-1' }, summary: { currentState: 'Newer data' } }
    f.component.selected = newer
    const detail = { pursuit: { id: 'pursuit-1' }, workflows: [{ id: 'workflow-1', sourceType: 'pursuit', sourceId: 'pursuit-1', requiresApproval: true, approvalStatus: 'pending' }],
      links: [{ pursuitId: 'pursuit-1', linkType: 'workflow', linkId: 'workflow-1', relationship: 'first_workflow_plan' }] }
    f.planRequests[0].response.next(detail)
    f.planRequests[0].response.next(detail)
    assert.equal(f.component.selected, newer)
    assert.equal(f.component.planning, false)
    assert.equal(f.notices.some(n => n.kind === 'success'), false)
  }
})

test('review actions require bound audit and schedule without overwriting a new selection', () => {
  for (const snooze of [false, true]) for (const mode of ['valid', 'foreign', 'no_audit', 'old_audit', 'wrong_event', 'bad_date', 'empty', 'error', 'switch', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' }, activity: [{ id: 'previous-audit' }] }
    f.component.selected = original
    const perform = () => snooze ? f.component.snoozeSelectedReview(3) : f.component.completeSelectedReview()
    perform(); perform()
    assert.equal(f.reviewRequests.length, 1)
    const request = f.reviewRequests[0]
    const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1', nextReviewAt: mode === 'bad_date' ? 'unknown' : '2027-01-01T00:00:00Z' },
      activity: [{ id: mode === 'old_audit' ? 'previous-audit' : 'new-audit', pursuitId: 'pursuit-1', eventType: mode === 'wrong_event' ? 'pursuit.updated' : snooze ? 'pursuit.review_snoozed' : 'pursuit.reviewed', message: request.request.note }] }
    if (mode === 'no_audit') detail.activity = []
    if (mode === 'switch') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') request.response.complete()
    else if (mode === 'error') request.response.error({ error: { error: 'private-provider-token' } })
    else request.response.next(detail)
    assert.equal(f.component.reviewing, false, mode)
    if (mode === 'valid') assert.equal(f.component.selected, detail)
    else if (mode === 'switch') { assert.equal(f.component.selected, undefined); assert.equal(f.component.detailLoading, true) }
    else assert.equal(f.component.selected, original, mode)
    assert.equal(f.notices.some(n => n.kind === 'success'), mode === 'valid', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('review rejects invalid delays and preserves a newer same-pursuit snapshot', () => {
  for (const days of [0, -1, 91, 1.5, NaN, Infinity]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.snoozeSelectedReview(days)
    assert.equal(f.reviewRequests.length, 0)
    assert.equal(f.component.reviewing, false)
  }
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' }, activity: [] }
  f.component.completeSelectedReview()
  const newer = { pursuit: { id: 'pursuit-1' }, summary: { currentState: 'Newer' } }
  f.component.selected = newer
  const detail = { pursuit: { id: 'pursuit-1', nextReviewAt: '2027-01-01T00:00:00Z' },
    activity: [{ id: 'new-audit', pursuitId: 'pursuit-1', eventType: 'pursuit.reviewed', message: f.reviewRequests[0].request.note }] }
  f.reviewRequests[0].response.next(detail)
  f.reviewRequests[0].response.next(detail)
  assert.equal(f.component.selected, newer)
  assert.equal(f.component.reviewing, false)
  assert.equal(f.notices.some(n => n.kind === 'success'), false)
})

test('summary refresh validates records and cannot overwrite a later project selection', () => {
  for (const mode of ['valid', 'foreign', 'missing_summary', 'empty', 'error', 'switch', 'destroy']) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' } }
    f.component.selected = original
    f.component.refreshSelectedSummary()
    f.component.refreshSelectedSummary()
    assert.equal(f.summaryRequests.length, 1)
    const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' }, summary: { currentState: 'Waiting for review' } }
    if (mode === 'missing_summary') delete detail.summary
    if (mode === 'switch') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'destroy') f.component.ngOnDestroy()
    if (mode === 'empty') f.summaryRequests[0].response.complete()
    else if (mode === 'error') f.summaryRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.summaryRequests[0].response.next(detail)
    assert.equal(f.component.summaryLoading, false, mode)
    if (mode === 'valid') assert.equal(f.component.selected, detail)
    else if (mode === 'switch') { assert.equal(f.component.selected, undefined); assert.equal(f.component.detailLoading, true) }
    else assert.equal(f.component.selected, original, mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
    assert.equal(f.notices.some(n => n.kind === 'success'), mode === 'valid', mode)
  }
})

function validBrief() {
  return { pursuitId: 'pursuit-1', ready: true, status: 'ready', reason: 'Preparation only',
    allowedActions: ['Prepare'], blockedActions: ['Do not send'], deliveryRequirements: ['Preserve evidence'],
    workItems: [{ workflowId: 'workflow-1', title: 'Prepare documents', instructions: 'Read sources' }] }
}

test('summary refresh does not replace a newer detail snapshot of the same pursuit', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' } }
  f.component.refreshSelectedSummary()
  const newer = { pursuit: { id: 'pursuit-1' }, summary: { currentState: 'New evidence received' } }
  f.component.selected = newer
  f.summaryRequests[0].response.next({ pursuit: { id: 'pursuit-1' }, summary: { currentState: 'Older snapshot' } })
  assert.equal(f.component.selected, newer)
  assert.equal(f.component.summaryLoading, false)
  assert.equal(f.notices.some(n => n.kind === 'success'), false)
})

test('delegation brief rejects foreign, incomplete and inconsistent ready responses', () => {
  for (const mode of ['valid', 'foreign', 'missing_limits', 'no_work', 'inconsistent', 'empty', 'error']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.prepareDelegationPackage()
    const brief = validBrief()
    if (mode === 'foreign') brief.pursuitId = 'pursuit-2'
    if (mode === 'missing_limits') delete brief.blockedActions
    if (mode === 'no_work') brief.workItems = []
    if (mode === 'inconsistent') brief.status = 'not_ready'
    if (mode === 'empty') f.delegationRequests[0].response.complete()
    else if (mode === 'error') f.delegationRequests[0].response.error({ error: { error: 'private-provider-token' } })
    else f.delegationRequests[0].response.next(brief)
    assert.equal(!!f.component.delegationPackage, mode === 'valid', mode)
    assert.equal(f.component.delegationLoading, false, mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('delegation read is cancelled on project change and destruction', () => {
  for (const destroy of [false, true]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.prepareDelegationPackage()
    if (destroy) f.component.ngOnDestroy()
    else f.component.selectPursuit({ id: 'pursuit-2' }, false)
    f.delegationRequests[0].response.next(validBrief())
    assert.equal(f.component.delegationPackage, undefined)
    assert.equal(f.component.delegationLoading, false)
  }
})

test('bounded blocked delegation remains inspectable and brief requests cannot duplicate', () => {
  const f = fixture()
  f.component.selected = { pursuit: { id: 'pursuit-1' } }
  f.component.prepareDelegationPackage()
  f.component.prepareDelegationPackage()
  assert.equal(f.delegationRequests.length, 1)
  const brief = { ...validBrief(), ready: false, status: 'not_ready', workItems: null }
  f.delegationRequests[0].response.next(brief)
  f.delegationRequests[0].response.next(validBrief())
  assert.equal(f.component.delegationPackage, brief)
  assert.equal(f.component.delegationLoading, false)
  assert.equal(f.notices[0].args[0], 'VA brief blocked')
  f.component.resolveDashboardDecision({ ...card, pursuit: { id: 'pursuit-2' } }, true)
  assert.equal(f.component.delegationPackage, undefined)
})

test('dashboard decision must still exist in the exact freshly loaded pursuit', () => {
  for (const mode of ['valid', 'missing', 'wrong_pursuit', 'missing_queue']) {
    const f = fixture()
    f.component.resolveDashboardDecision(card, true)
    const fresh = { id: 'decision-1', summary: 'fresh server decision' }
    const detail = { pursuit: { id: mode === 'wrong_pursuit' ? 'pursuit-2' : 'pursuit-1' }, decisionQueue: mode === 'missing' ? [] : [fresh] }
    if (mode === 'missing_queue') delete detail.decisionQueue
    f.requests[0].response.next(detail)
    assert.equal(f.resolved.length, mode === 'valid' ? 1 : 0, mode)
    if (mode === 'valid') assert.equal(f.resolved[0][0], fresh)
    assert.equal(f.component.detailLoading, false, mode)
  }
})

test('switching pursuits cancels pending dashboard decision dispatch', () => {
  const f = fixture()
  f.component.resolveDashboardDecision(card, true)
  f.component.selectPursuit({ id: 'pursuit-2' }, false)
  f.requests[0].response.next({ pursuit: { id: 'pursuit-1' }, decisionQueue: [card.decision] })
  assert.equal(f.resolved.length, 0)
  assert.equal(f.component.detailLoading, true)
  f.requests[1].response.next({ pursuit: { id: 'pursuit-2' }, decisionQueue: [] })
  assert.equal(f.component.selected.pursuit.id, 'pursuit-2')
})

test('dashboard decision read supersedes an earlier pursuit detail read', () => {
  const f = fixture()
  f.component.selectPursuit({ id: 'pursuit-2' }, false)
  f.component.resolveDashboardDecision(card, true)
  f.requests[0].response.next({ pursuit: { id: 'pursuit-2' }, decisionQueue: [] })
  assert.equal(f.component.selected, undefined)
  f.requests[1].response.next({ pursuit: { id: 'pursuit-1' }, decisionQueue: [card.decision] })
  assert.equal(f.resolved.length, 1)
  assert.equal(f.component.selected.pursuit.id, 'pursuit-1')
})

test('dashboard decision read failure never displays raw provider details', () => {
  const f = fixture()
  f.component.resolveDashboardDecision(card, true)
  f.requests[0].response.error({ error: { error: 'private-provider-token' } })
  assert.equal(f.resolved.length, 0)
  assert.equal(f.component.detailLoading, false)
  assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
})

test('empty dashboard decision read releases loading without dispatch', () => {
  const f = fixture()
  f.component.resolveDashboardDecision(card, true)
  f.requests[0].response.complete()
  assert.equal(f.component.detailLoading, false)
  assert.equal(f.resolved.length, 0)
  assert.equal(f.notices[0]?.kind, 'warning')
})

test('dashboard decision response can dispatch only once and never after destruction', () => {
  for (const destroy of [false, true]) {
    const f = fixture()
    f.component.resolveDashboardDecision(card, true)
    if (destroy) f.component.ngOnDestroy()
    const detail = { pursuit: { id: 'pursuit-1' }, decisionQueue: [card.decision] }
    f.requests[0].response.next(detail)
    f.requests[0].response.next(detail)
    assert.equal(f.resolved.length, destroy ? 0 : 1)
  }
})

test('pursuit detail refuses foreign records and raw errors and handles empty completion', () => {
  for (const mode of ['foreign', 'error', 'empty']) {
    const f = fixture()
    f.component.selectPursuit({ id: 'pursuit-1' }, false)
    if (mode === 'foreign') f.requests[0].response.next({ pursuit: { id: 'pursuit-2' } })
    if (mode === 'error') f.requests[0].response.error({ error: { error: 'private-provider-token' } })
    if (mode === 'empty') f.requests[0].response.complete()
    assert.equal(f.component.selected, undefined, mode)
    assert.equal(f.component.detailLoading, false, mode)
    assert.ok(f.component.detailError, mode)
    assert.doesNotMatch(f.component.detailError + JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('pursuit detail selection closes foreign inspectors but same-pursuit refresh preserves them', () => {
  for (const same of [true, false]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    const runtime = { id: 'runtime-1' }
    const evidence = { id: 'evidence-1' }
    const action = { id: 'action-1' }
    f.component.inspectedRuntimeEvidence = runtime
    f.component.inspectedEvidence = evidence
    f.component.inspectedAction = action
    f.component.loadPursuitDetail(same ? 'pursuit-1' : 'pursuit-2', false)
    assert.equal(f.component.inspectedRuntimeEvidence, same ? runtime : undefined)
    assert.equal(f.component.inspectedEvidence, same ? evidence : undefined)
    assert.equal(f.component.inspectedAction, same ? action : undefined)
  }
})

test('pursuit detail uses a single response and remembers the actual retry target', () => {
  const f = fixture()
  f.component.loadPursuitDetail('pursuit-1', false)
  f.requests[0].response.complete()
  f.component.retrySelectedPursuit()
  assert.equal(f.requests[1]?.id, 'pursuit-1')
  const first = { pursuit: { id: 'pursuit-1' }, nextAction: 'first' }
  f.requests[1].response.next(first)
  f.requests[1].response.next({ pursuit: { id: 'pursuit-1' }, nextAction: 'unsolicited replacement' })
  assert.equal(f.component.selected, first)
})

test('late evidence resolution cannot reopen an inspector after switching pursuit or destruction', () => {
  for (const mode of ['selection', 'dashboard', 'destroy']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resolveEvidence('memory://record-1')
    if (mode === 'selection') f.component.selectPursuit({ id: 'pursuit-2' }, false)
    if (mode === 'dashboard') f.component.resolveDashboardDecision({ ...card, pursuit: { id: 'pursuit-2' } }, true)
    if (mode === 'destroy') f.component.ngOnDestroy()
    f.evidenceRequests[0].response.next({ uri: 'memory://record-1' })
    assert.equal(f.component.inspectedEvidence, undefined, mode)
    assert.equal(f.component.resolvingEvidenceUri, '', mode)
  }
})

test('evidence resolution requires the requested URI and the exact runtime attempt ID', () => {
  const uri = 'automation-launch://11111111-1111-4111-8111-111111111111'
  for (const mode of ['valid', 'foreign_uri', 'foreign_runtime', 'missing_uri', 'null']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resolveEvidence(uri)
    let record = { uri, runtimeAttempt: { id: uri.slice('automation-launch://'.length) } }
    if (mode === 'foreign_uri') record.uri = 'memory://foreign'
    if (mode === 'foreign_runtime') record.runtimeAttempt.id = '22222222-2222-4222-8222-222222222222'
    if (mode === 'missing_uri') delete record.uri
    if (mode === 'null') record = null
    f.evidenceRequests[0].response.next(record)
    assert.equal(f.component.inspectedRuntimeEvidence, mode === 'valid' ? record.runtimeAttempt : undefined, mode)
    assert.equal(f.component.inspectedEvidence, undefined, mode)
    assert.equal(f.component.resolvingEvidenceUri, '', mode)
  }
})

test('evidence resolution handles empty completion and redacts raw errors', () => {
  for (const mode of ['empty', 'error']) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resolveEvidence('memory://record-1')
    if (mode === 'empty') f.evidenceRequests[0].response.complete()
    else f.evidenceRequests[0].response.error({ error: { error: 'private-provider-token' } })
    assert.equal(f.component.resolvingEvidenceUri, '', mode)
    assert.equal(f.notices[0]?.kind, 'warning', mode)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
  }
})

test('valid source resolution opens once and accepts canonical runtime URI casing', () => {
  for (const runtime of [false, true]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    const uri = runtime ? 'AUTOMATION-LAUNCH://AAAAAAAA-1111-4111-8111-111111111111' : 'memory://record-1'
    f.component.resolveEvidence(uri)
    f.component.resolveEvidence(uri)
    assert.equal(f.evidenceRequests.length, 1)
    const record = runtime ? { uri: uri.toLowerCase(), runtimeAttempt: { id: 'aaaaaaaa-1111-4111-8111-111111111111' } } : { uri }
    f.evidenceRequests[0].response.next(record)
    f.evidenceRequests[0].response.next({ uri, summary: 'unsolicited replacement' })
    assert.equal(f.component.resolvingEvidenceUri, '')
    assert.equal(runtime ? f.component.inspectedRuntimeEvidence : f.component.inspectedEvidence, runtime ? record.runtimeAttempt : record)
    assert.equal(f.notices.length, 0)
  }
})

test('pursuit decision mutation validates response identity and preserves a newer selection', () => {
  for (const method of ['resolvePursuitNextAction', 'resolveRuntimeAttemptReview', 'resolvePursuitCompletionReview']) {
    for (const mode of ['valid', 'foreign', 'pending', 'malformed_queue', 'missing_queue', 'moved', 'empty', 'error']) {
      const f = fixture()
      f.component.selected = { pursuit: { id: 'pursuit-1' } }
      const decision = { id: 'decision-1', decisionType: 'pursuit_next_action' }
      f.component[method](decision, true)
      const request = f.mutationRequests[0]
      assert.equal(request.id, 'pursuit-1')
      const newer = { pursuit: { id: 'pursuit-2' } }
      if (mode === 'moved') f.component.selected = newer
      const detail = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' }, decisionQueue: mode === 'pending' ? [{ ...decision, status: 'pending' }] : [] }
      if (mode === 'malformed_queue') detail.decisionQueue = [null]
      if (mode === 'missing_queue') delete detail.decisionQueue
      if (mode === 'empty') request.response.complete()
      else if (mode === 'error') request.response.error({ error: { error: 'private-provider-token' } })
      else request.response.next(detail)
      if (mode === 'moved') assert.equal(f.component.selected, newer, method)
      else if (mode === 'valid') assert.equal(f.component.selected, detail, method)
      else assert.equal(f.component.selected.pursuit.id, 'pursuit-1', `${method}:${mode}`)
      assert.equal(f.component.resolvingDecisionId, '', `${method}:${mode}`)
      assert.equal(f.notices.some(notice => notice.kind === 'success'), mode === 'valid' || mode === 'moved', `${method}:${mode}`)
      assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
    }
  }
})

test('decision mutation observer consumes one response and releases itself on destruction', () => {
  for (const destroy of [false, true]) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' } }
    f.component.selected = original
    f.component.resolvePursuitNextAction({ id: 'decision-1', decisionType: 'pursuit_next_action' }, false)
    if (destroy) f.component.ngOnDestroy()
    const detail = { pursuit: { id: 'pursuit-1' }, decisionQueue: [] }
    f.mutationRequests[0].response.next(detail)
    f.mutationRequests[0].response.next(detail)
    assert.equal(f.notices.length, destroy ? 0 : 1)
    assert.equal(f.component.selected, destroy ? original : detail)
    assert.equal(f.component.resolvingDecisionId, '')
    assert.equal(f.mutationRequests.length, 1)
  }
})

test('real pursuit service rejects incomplete mutation responses before display normalization', () => {
  for (const method of ['resolveDecision', 'acceptCandidate']) {
    for (const mode of ['valid', 'foreign', 'missing_queue', 'null_queue', 'malformed_queue', 'null']) {
      let raw = { pursuit: { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1' }, decisionQueue: [] }
      if (mode === 'missing_queue') delete raw.decisionQueue
      if (mode === 'null_queue') raw.decisionQueue = null
      if (mode === 'malformed_queue') raw.decisionQueue = [null]
      if (mode === 'null') raw = null
      const before = structuredClone(raw)
      const service = new PursuitService({ post: () => of(raw) })
      let result
      let error
      service[method]('pursuit-1', {}).subscribe({ next: value => { result = value }, error: value => { error = value } })
      assert.equal(!!result, mode === 'valid', `${method}:${mode}`)
      assert.equal(!!error, mode !== 'valid', `${method}:${mode}`)
      assert.deepEqual(raw, before, 'validation must not alter the provider response')
    }
  }
})

test('candidate acceptance and archive require bound responses and preserve a newer selection', () => {
  for (const approved of [true, false]) {
    for (const mode of ['valid', 'foreign', 'not_changed', 'moved', 'empty', 'error']) {
      const f = fixture()
      f.component.selected = { pursuit: { id: 'pursuit-1' } }
      const decision = { id: 'decision-1', decisionType: 'pursuit_candidate_review' }
      f.component.resolvePursuitCandidateReview(decision, approved)
      const newer = { pursuit: { id: 'pursuit-2' } }
      if (mode === 'moved') f.component.selected = newer
      const pursuit = { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1', archived: !approved && mode !== 'not_changed', status: approved || mode === 'not_changed' ? 'active' : 'archived' }
      const detail = { pursuit, decisionQueue: mode === 'not_changed' ? [{ ...decision, status: 'pending' }] : [] }
      const request = f.mutationRequests[0]
      if (mode === 'empty') request.response.complete()
      else if (mode === 'error') request.response.error({ error: { error: 'private-provider-token' } })
      else request.response.next(approved ? detail : pursuit)
      if (mode === 'moved') assert.equal(f.component.selected, newer)
      else if (mode === 'valid') assert.equal(f.component.selected, approved ? detail : undefined)
      else assert.equal(f.component.selected.pursuit.id, 'pursuit-1')
      assert.equal(f.component.resolvingDecisionId, '', `${approved}:${mode}`)
      assert.equal(f.notices.some(notice => notice.kind === 'success'), mode === 'valid' || mode === 'moved', `${approved}:${mode}`)
      assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
      assert.equal(f.mutationRequests.length, 1)
    }
  }
})

test('raw HTTP decision detail flows through the real service and component without invented queue evidence', () => {
  for (const valid of [true, false]) {
    const f = fixture()
    const raw = { pursuit: { id: 'pursuit-1' } }
    if (valid) raw.decisionQueue = []
    f.component.pursuitsService = new PursuitService({ post: () => of(raw) })
    f.component.selected = { pursuit: { id: 'pursuit-1' } }
    f.component.resolvePursuitNextAction({ id: 'decision-1', decisionType: 'pursuit_next_action' }, false)
    assert.equal(f.notices.some(notice => notice.kind === 'success'), valid)
    assert.equal(f.component.resolvingDecisionId, '')
    assert.equal('decisionQueue' in raw, valid)
  }
})

test('candidate mutation observer cannot apply responses after destruction', () => {
  for (const approved of [true, false]) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1' } }
    f.component.selected = original
    f.component.resolvePursuitCandidateReview({ id: 'decision-1', decisionType: 'pursuit_candidate_review' }, approved)
    f.component.ngOnDestroy()
    const pursuit = { id: 'pursuit-1', archived: true, status: 'archived' }
    f.mutationRequests[0].response.next(approved ? { pursuit, decisionQueue: [] } : pursuit)
    assert.equal(f.component.selected, original)
    assert.equal(f.component.resolvingDecisionId, '')
    assert.equal(f.notices.length, 0)
  }
})

test('workflow approval and proposal need a bound matching decision audit', () => {
  for (const proposal of [false, true]) {
    for (const approved of [false, true]) {
      for (const mode of ['valid', 'foreign_workflow', 'missing_audit', 'malformed_audit', 'foreign_audit', 'opposite_status', 'null', 'wrong_choice', 'wrong_proposal', 'empty', 'error', 'moved']) {
        const f = fixture()
        const original = { pursuit: { id: 'pursuit-1' } }
        f.component.selected = original
        const decision = { id: proposal ? 'proposal:proposal-1' : 'decision-1', workflowId: 'workflow-1' }
        f.component[proposal ? 'resolveWorkflowProposal' : 'resolveWorkflowApproval'](decision, approved)
        const request = f.workflowRequests[0]
        const status = approved ? 'approved' : 'rejected'
        const record = {
          item: { id: mode === 'foreign_workflow' ? 'workflow-2' : 'workflow-1', approvalStatus: status },
          decisions: [{ id: 'audit-1', workflowId: 'workflow-1', decisionType: proposal ? 'proposal' : 'approval', decision: status, approved }],
          proposals: [{ id: 'proposal-1', workflowId: 'workflow-1', status }],
        }
        if (mode === 'missing_audit') record.decisions = []
        if (mode === 'malformed_audit') record.decisions = [null]
        if (mode === 'foreign_audit') record.decisions[0].workflowId = 'workflow-2'
        if (mode === 'opposite_status') record.decisions[0].decision = approved ? 'rejected' : 'approved'
        if (mode === 'wrong_choice') record.decisions[0].approved = !approved
        if (mode === 'wrong_proposal') {
          if (proposal) record.proposals[0].id = 'proposal-2'
          else record.item.approvalStatus = 'pending'
        }
        const newer = { pursuit: { id: 'pursuit-2' } }
        if (mode === 'moved') f.component.selected = newer
        if (mode === 'empty') request.response.complete()
        else if (mode === 'error') request.response.error({ error: { error: 'private-provider-token' } })
        else request.response.next(mode === 'null' ? null : record)
        assert.equal(f.component.selected, mode === 'moved' ? newer : original)
        assert.equal(f.component.resolvingDecisionId, '', `${proposal}:${approved}:${mode}`)
        assert.equal(f.notices.some(notice => notice.kind === 'success'), mode === 'valid' || mode === 'moved', `${proposal}:${approved}:${mode}`)
        assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
        assert.equal(f.workflowRequests.length, 1)
      }
    }
  }
})

test('workflow mutation observers consume one acknowledgement and suppress post-destruction updates', () => {
  for (const proposal of [true, false]) {
    for (const destroy of [true, false]) {
      const f = fixture()
      const original = { pursuit: { id: 'pursuit-1' } }
      f.component.selected = original
      const decision = { id: proposal ? 'proposal:proposal-1' : 'decision-1', workflowId: 'workflow-1' }
      f.component[proposal ? 'resolveWorkflowProposal' : 'resolveWorkflowApproval'](decision, true)
      if (destroy) f.component.ngOnDestroy()
      const record = {
        item: { id: 'workflow-1', approvalStatus: 'approved' },
        decisions: [{ id: 'audit-1', workflowId: 'workflow-1', decisionType: proposal ? 'proposal' : 'approval', decision: 'approved', approved: true }],
        proposals: [{ id: 'proposal-1', workflowId: 'workflow-1', status: 'approved' }],
      }
      f.workflowRequests[0].response.next(record)
      f.workflowRequests[0].response.next(record)
      assert.equal(f.notices.length, destroy ? 0 : 1)
      assert.equal(f.component.selected, original)
      assert.equal(f.component.resolvingDecisionId, '')
      assert.equal(f.workflowRequests.length, 1)
    }
  }
})

test('public decision dispatcher sends only a current matching pending queue record', () => {
  for (const decisionType of ['approval', 'proposal', 'pursuit_next_action', 'runtime_attempt_review', 'pursuit_completion_review', 'pursuit_candidate_review']) {
    for (const approved of [true, false]) {
      for (const mode of ['valid', 'missing', 'missing_queue', 'malformed_queue', 'no_selection', 'blank_id', 'resolved', 'wrong_type', 'wrong_workflow', 'loading', 'busy', 'lifecycle_busy']) {
        const f = fixture()
        f.component.resolveDecision = PursuitsComponent.prototype.resolveDecision
        f.component.canResolveDecision = PursuitsComponent.prototype.canResolveDecision
        const fresh = { id: decisionType === 'proposal' ? 'proposal:proposal-1' : 'decision-1', decisionType,
          status: 'pending', workflowId: ['approval', 'proposal'].includes(decisionType) ? 'workflow-1' : undefined,
          yesConsequence: 'fresh yes', noConsequence: 'fresh no', riskLevel: 'high', reason: 'fresh context' }
        const stale = { ...fresh, yesConsequence: 'stale yes', noConsequence: 'stale no', riskLevel: 'low', reason: 'stale context' }
        f.component.selected = { pursuit: { id: 'pursuit-1' }, decisionQueue: mode === 'missing' ? [] : [fresh] }
        if (mode === 'missing_queue') delete f.component.selected.decisionQueue
        if (mode === 'malformed_queue') f.component.selected.decisionQueue = [null]
        if (mode === 'no_selection') f.component.selected = undefined
        if (mode === 'blank_id') stale.id = ''
        if (mode === 'resolved') fresh.status = 'resolved'
        if (mode === 'wrong_type') fresh.decisionType = 'other'
        if (mode === 'wrong_workflow') stale.workflowId = 'foreign-workflow'
        if (mode === 'loading') f.component.detailLoading = true
        if (mode === 'busy') f.component.resolvingDecisionId = 'other-decision'
        if (mode === 'lifecycle_busy') f.component.lifecycleRunningId = 'other-pursuit'
        f.component.resolveDecision(stale, approved)
        const calls = [...f.workflowRequests, ...f.mutationRequests]
        assert.equal(calls.length, mode === 'valid' ? 1 : 0, `${decisionType}:${approved}:${mode}`)
        if (mode === 'valid') {
          assert.equal(calls[0].id, ['approval', 'proposal'].includes(decisionType) ? 'workflow-1' : 'pursuit-1')
          if (decisionType !== 'pursuit_candidate_review') assert.equal(calls[0].request.note, approved ? 'fresh yes' : 'fresh no')
          else if (approved) assert.equal(calls[0].request.requiresReview, true)
        }
        f.component.ngOnDestroy()
      }
    }
  }
})

test('dashboard fresh read reaches the real dispatcher without reusing the old card context', () => {
  for (const present of [true, false]) {
    const f = fixture()
    f.component.resolveDecision = PursuitsComponent.prototype.resolveDecision
    f.component.canResolveDecision = PursuitsComponent.prototype.canResolveDecision
    const stale = { id: 'decision-1', status: 'pending', decisionType: 'pursuit_next_action', yesConsequence: 'stale yes' }
    const fresh = { ...stale, yesConsequence: 'fresh server yes' }
    f.component.resolveDashboardDecision({ pursuit: { id: 'pursuit-1' }, decision: stale }, true)
    f.requests[0].response.next({ pursuit: { id: 'pursuit-1' }, decisionQueue: present ? [fresh] : [] })
    assert.equal(f.mutationRequests.length, present ? 1 : 0)
    if (present) assert.equal(f.mutationRequests[0].request.note, 'fresh server yes')
    f.component.ngOnDestroy()
  }
})

test('archive and reopen validate lifecycle state without redirecting a newer selection', () => {
  for (const reopen of [true, false]) {
    for (const mode of ['valid', 'foreign', 'unchanged', 'moved', 'empty', 'error']) {
      const f = fixture()
      const original = { pursuit: { id: 'pursuit-1', archived: reopen, status: reopen ? 'archived' : 'active', completionState: 'open' } }
      f.component.selected = original
      f.component.requestedPursuitId = 'pursuit-1'
      const queries = []
      f.component.setSelectedQuery = (...args) => queries.push(args)
      f.component[reopen ? 'reopenSelected' : 'archiveSelected']()
      const newer = { pursuit: { id: 'pursuit-2' } }
      if (mode === 'moved') f.component.selected = newer
      const result = { id: mode === 'foreign' ? 'pursuit-2' : 'pursuit-1', archived: mode === 'unchanged' ? reopen : !reopen,
        status: (mode === 'unchanged' ? reopen : !reopen) ? 'archived' : 'active', completionState: 'open' }
      if (mode === 'empty') f.mutationRequests[0].response.complete()
      else if (mode === 'error') f.mutationRequests[0].response.error({ error: { error: 'private-provider-token' } })
      else f.mutationRequests[0].response.next(result)
      if (mode === 'moved') {
        assert.equal(f.component.selected, newer)
        assert.equal(queries.length, 0)
        assert.equal(f.requests.length, 0)
      } else if (mode === 'valid') {
        if (reopen) assert.equal(f.component.selected.pursuit, result)
        else assert.equal(f.component.selected, undefined)
      } else assert.equal(f.component.selected, original)
      assert.equal(f.notices.some(notice => notice.kind === 'success'), mode === 'valid' || mode === 'moved', `${reopen}:${mode}`)
      assert.equal(f.component.lifecycleRunningId, '', `${reopen}:${mode}`)
      assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-token/)
      f.component.ngOnDestroy()
    }
  }
})

test('lifecycle controls prevent duplicate requests and release observation on destruction', () => {
  for (const reopen of [true, false]) {
    const f = fixture()
    const original = { pursuit: { id: 'pursuit-1', archived: reopen, status: reopen ? 'archived' : 'active' } }
    f.component.selected = original
    f.component[reopen ? 'reopenSelected' : 'archiveSelected']()
    f.component[reopen ? 'reopenSelected' : 'archiveSelected']()
    assert.equal(f.mutationRequests.length, 1)
    f.component.ngOnDestroy()
    f.mutationRequests[0].response.next({ id: 'pursuit-1', archived: !reopen, status: reopen ? 'active' : 'archived', completionState: 'open' })
    assert.equal(f.component.selected, original)
    assert.equal(f.notices.length, 0)
    assert.equal(f.component.lifecycleRunningId, '')
  }
})

test('confirmed archive cancels only its own pending detail read', () => {
  for (const same of [true, false]) {
    const f = fixture()
    f.component.selected = { pursuit: { id: 'pursuit-1', status: 'active', archived: false } }
    f.component.archiveSelected()
    f.component.loadPursuitDetail(same ? 'pursuit-1' : 'pursuit-2', false)
    f.mutationRequests[0].response.next({ id: 'pursuit-1', status: 'archived', archived: true })
    assert.equal(f.component.detailLoading, !same)
    const detail = { pursuit: { id: same ? 'pursuit-1' : 'pursuit-2' } }
    f.requests[0].response.next(detail)
    assert.equal(f.component.selected, same ? undefined : detail)
    f.component.ngOnDestroy()
  }
})

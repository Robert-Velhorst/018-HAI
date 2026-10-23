import assert from 'node:assert/strict'
import { test } from 'node:test'
import { EMPTY, NEVER, of, Subject, throwError } from 'rxjs'
import { TestScheduler } from 'rxjs/testing'
import '@angular/compiler'
import { FormBuilder } from '@angular/forms'
import { loadComponentLogic } from './load-component-logic.mjs'

const { CommandDashboardComponent } = await loadComponentLogic(new URL('../src/app/pages/command-dashboard/command-dashboard.component.ts', import.meta.url), {
  '@angular/forms': import.meta.resolve('@angular/forms'),
  '../../services/memory-engine/memory-engine.service.token': 'data:text/javascript,export const MEMORY_ENGINE_SERVICE_TOKEN = Symbol("test-memory")',
  '../../control-room/source-navigation': new URL('../src/app/control-room/source-navigation.ts', import.meta.url).href,
  '../../shared/http-timeout-policy': new URL('../src/app/shared/http-timeout-policy.ts', import.meta.url).href,
})
const { WorkflowService } = await loadComponentLogic(new URL('../src/app/services/workflow/workflow.service.ts', import.meta.url))
const { PursuitService } = await loadComponentLogic(new URL('../src/app/services/pursuit.service.ts', import.meta.url))
const workflowId = '11111111-1111-4111-8111-111111111111'
const decisionId = '22222222-2222-4222-8222-222222222222'
function card() {
  return { pursuit: { id: 'pursuit-fixture' }, decision: {
    id: `workflow:${workflowId}:approval`, workflowId, decisionType: 'approval', status: 'pending',
    yesConsequence: 'Approve this fixture', noConsequence: 'Reject this fixture',
  } }
}
function receipt(approved = true) {
  return {
    item: { id: workflowId, requiresApproval: true, approvalStatus: approved ? 'approved' : 'rejected', currentState: approved ? 'ready' : 'blocked' },
    decisions: [{ id: decisionId, workflowId, decisionType: 'approval', decision: approved ? 'approved' : 'rejected', approved, createdAt: '2026-10-23T10:00:00Z' }],
  }
}
function fixture(response = of(receipt()), synchronousFailure = false, useActualService = false) {
  const notices = [], requests = [], refreshes = []
  const notification = Object.fromEntries(['info', 'success', 'warning', 'error']
    .map(kind => [kind, (...args) => notices.push({ kind, args })]))
  const request = (...args) => {
    requests.push(args)
    if (synchronousFailure) throw new Error('private-provider-detail')
    return response
  }
  const workflows = useActualService ? new WorkflowService({ post: request }) : { resolveApproval: request, resolveProposal: request }
  const pursuits = useActualService ? new PursuitService({ post: request }) : { resolveDecision: request, acceptCandidate: request, archive: request }
  const component = new CommandDashboardComponent(new FormBuilder(), {}, {}, {}, pursuits, workflows, notification, {})
  component.refreshPursuits = () => refreshes.push(true)
  return { component, notices, requests, refreshes }
}

test('command dashboard confirms only matching approval state and dated decision evidence', () => {
  for (const approved of [true, false]) {
    const f = fixture(of(receipt(approved)))
    f.component.resolveDashboardDecision(card(), approved)
    assert.equal(f.requests.length, 1)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.notices.length, 1)
    assert.equal(f.notices[0].kind, 'info')
    assert.match(f.notices[0].args.join(' '), /not proof of execution or independent audit persistence/)
    assert.equal(f.refreshes.length, 1)
  }
})

test('command dashboard rejects incomplete, foreign and contradictory approval acknowledgements', () => {
  const bad = [null, {}, { ...receipt(), item: { ...receipt().item, id: decisionId } },
    ...['pending', 'rejected'].map(approvalStatus => ({ ...receipt(), item: { ...receipt().item, approvalStatus } })),
    { ...receipt(), item: { ...receipt().item, currentState: 'completed' } },
    { ...receipt(), item: { ...receipt().item, requiresApproval: false } },
    { ...receipt(), decisions: [] },
    ...[{ id: '00000000-0000-0000-0000-000000000000' }, { workflowId: decisionId },
      { decisionType: 'proposal' }, { decision: 'rejected' }, { approved: false },
      { createdAt: 'not-a-date' }].map(change => ({ ...receipt(), decisions: [{ ...receipt().decisions[0], ...change }] })),
  ]
  for (const value of bad) {
    const f = fixture(of(value))
    f.component.resolveDashboardDecision(card(), true)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
  }
})

test('command dashboard reconciles empty, errored and synchronously failed approvals without raw details', () => {
  for (const [response, synchronous] of [[EMPTY, false], [throwError(() => new Error('private-provider-detail')), false], [undefined, true]]) {
    const f = fixture(response, synchronous)
    f.component.resolveDashboardDecision(card(), true)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-detail/)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.requests.length, 1)
    assert.equal(f.refreshes.length, 1)
  }
})

test('command dashboard bounds stalled approvals and does not automatically retry', () => {
  let result
  new TestScheduler(() => {}).run(() => {
    const f = fixture(NEVER)
    f.component.resolveDashboardDecision(card(), true)
    assert.notEqual(f.component.resolvingDashboardDecisionId, '')
    // The scheduler flushes the real RxJS timeout without wall-clock waiting.
    result = f
  })
  assert.equal(result.component.resolvingDashboardDecisionId, '')
  assert.equal(result.notices[0]?.kind, 'warning')
  assert.equal(result.requests.length, 1)
})

test('command dashboard blocks duplicate approvals and consumes at most one acknowledgement', () => {
  const response = new Subject(), f = fixture(response)
  f.component.resolveDashboardDecision(card(), true)
  f.component.resolveDashboardDecision(card(), false)
  assert.equal(f.requests.length, 1)
  response.next(receipt())
  response.next(receipt(false))
  assert.equal(response.observed, false)
  assert.equal(f.notices.length, 1)
  assert.equal(f.refreshes.length, 1)
})

test('command dashboard cancels approval UI observation and refuses new decisions after destroy', () => {
  const response = new Subject(), f = fixture(response)
  f.component.resolveDashboardDecision(card(), true)
  f.component.ngOnDestroy()
  assert.equal(response.observed, false)
  assert.equal(f.component.resolvingDashboardDecisionId, '')
  response.next(receipt())
  f.component.resolveDashboardDecision(card(), false)
  assert.equal(f.requests.length, 1)
  assert.equal(f.notices.length, 0)
  assert.equal(f.refreshes.length, 0)
})

test('command dashboard validates approval references and boolean decisions before calling a service', () => {
  for (const value of ['', 'foreign', '00000000-0000-0000-0000-000000000000']) {
    const f = fixture(), input = card()
    input.decision.workflowId = value
    f.component.resolveDashboardDecision(input, true)
    assert.equal(f.requests.length, 0)
  }
  for (const decision of [null, 'true', 1]) {
    const f = fixture()
    f.component.resolveDashboardDecision(card(), decision)
    assert.equal(f.requests.length, 0)
  }
  for (const id of ['', 'workflow:foreign:approval', `workflow:${workflowId}:proposal`]) {
    const f = fixture(), input = card()
    input.decision.id = id
    f.component.resolveDashboardDecision(input, true)
    assert.equal(f.requests.length, 0)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
  }
})

test('command dashboard rejects an empty approval returned through the actual WorkflowService', () => {
  const f = fixture(of({}), false, true)
  f.component.resolveDashboardDecision(card(), true)
  assert.equal(f.requests.length, 1)
  assert.equal(f.requests[0][0], `/api/v1/workflow/${workflowId}/approval`)
  assert.equal(f.notices[0]?.kind, 'warning')
  assert.equal(f.component.resolvingDashboardDecisionId, '')
})

const pursuitId = '33333333-3333-4333-8333-333333333333'
const completionId = `pursuit:${pursuitId}:completion-review`
function completionCard() {
  return { pursuit: { id: pursuitId }, decision: {
    id: completionId, decisionType: 'pursuit_completion_review', status: 'pending',
  } }
}
function completionReceipt(approved = true) {
  return {
    pursuit: { id: pursuitId, status: approved ? 'completed' : 'active', completionState: approved ? 'verified' : 'open', archived: false },
    decisionQueue: [],
    activity: [{ id: decisionId, pursuitId, eventType: 'pursuit.decision_resolved', sourceId: completionId,
      sourceType: `pursuit_decision:pursuit_completion_review:${approved ? 'approved' : 'rejected'}`, createdAt: '2026-10-23T10:00:00Z' }],
  }
}

test('command dashboard binds completion and rejection to matching server resolution evidence', () => {
  for (const approved of [true, false]) {
    const f = fixture(of(completionReceipt(approved)), false, true)
    f.component.resolveDashboardDecision(completionCard(), approved)
    assert.equal(f.requests.length, 1)
    assert.equal(f.requests[0][0], `/api/v1/pursuits/${pursuitId}/decisions/resolve`)
    assert.equal(f.notices[0]?.kind, 'info')
    assert.match(f.notices[0].args.join(' '), /not independent verification/)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
  }
})

test('command dashboard never confirms completion from active or unverified real service detail', () => {
  for (const change of [{ status: 'active' }, { completionState: 'open' }, { archived: true }]) {
    const original = completionReceipt()
    const f = fixture(of({ ...original, pursuit: { ...original.pursuit, ...change } }), false, true)
    f.component.resolveDashboardDecision(completionCard(), true)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.equal(f.component.resolvingDashboardDecisionId, '')
  }
})

test('command dashboard requires exact completion activity and a resolved queue', () => {
  const original = completionReceipt()
  const malformed = [null, {}, { ...original, pursuit: { ...original.pursuit, id: workflowId } },
    { ...original, decisionQueue: undefined }, { ...original, activity: [] },
    { ...original, decisionQueue: [{ id: completionId, status: 'pending' }] },
    { ...original, decisionQueue: [{ id: completionId, status: ' PENDING ' }] },
    ...[{ id: '00000000-0000-0000-0000-000000000000' }, { pursuitId: workflowId },
      { eventType: 'pursuit.completed' }, { sourceId: 'foreign' },
      { sourceType: 'pursuit_decision:pursuit_completion_review:rejected' }, { createdAt: 'invalid' }]
      .map(change => ({ ...original, activity: [{ ...original.activity[0], ...change }] })),
  ]
  for (const value of malformed) {
    const f = fixture(of(value))
    f.component.resolveDashboardDecision(completionCard(), true)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
  }
})

test('command dashboard does not report a rejected completion as still open when the server reports closed state', () => {
  for (const status of ['completed', 'archived', 'unknown']) {
    const original = completionReceipt(false)
    const f = fixture(of({ ...original, pursuit: { ...original.pursuit, status } }))
    f.component.resolveDashboardDecision(completionCard(), false)
    assert.equal(f.notices[0]?.kind, 'warning')
  }
})

test('command dashboard completion releases empty and failed observations without leaking details', () => {
  for (const [response, synchronous] of [[EMPTY, false], [throwError(() => new Error('private-provider-detail')), false], [undefined, true]]) {
    const f = fixture(response, synchronous)
    f.component.resolveDashboardDecision(completionCard(), true)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-detail/)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
    assert.equal(f.requests.length, 1)
  }
})

test('command dashboard completion observes one response and cancels UI observation on destroy', () => {
  for (const destroy of [false, true]) {
    const response = new Subject(), f = fixture(response)
    f.component.resolveDashboardDecision(completionCard(), true)
    f.component.resolveDashboardDecision(completionCard(), false)
    assert.equal(f.requests.length, 1)
    if (destroy) f.component.ngOnDestroy()
    response.next(completionReceipt())
    response.next(completionReceipt(false))
    assert.equal(response.observed, false)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.notices.length, destroy ? 0 : 1)
    assert.equal(f.refreshes.length, destroy ? 0 : 1)
  }
})

test('command dashboard bounds stalled completion requests without repeating the mutation', () => {
  let result
  new TestScheduler(() => {}).run(() => {
    result = fixture(NEVER)
    result.component.resolveDashboardDecision(completionCard(), true)
  })
  assert.equal(result.component.resolvingDashboardDecisionId, '')
  assert.equal(result.notices[0]?.kind, 'warning')
  assert.equal(result.requests.length, 1)
})

test('command dashboard rejects noncanonical completion identities before service dispatch', () => {
  for (const change of [{ pursuit: { id: 'foreign' } },
    { decision: { ...completionCard().decision, id: 'foreign' } }]) {
    const f = fixture(), input = { ...completionCard(), ...change }
    f.component.resolveDashboardDecision(input, true)
    assert.equal(f.requests.length, 0)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
  }
})

const attemptId = '44444444-4444-4444-8444-444444444444'
function remainingCases() {
  const cases = []
  for (const kind of ['proposal', 'candidate', 'next', 'runtime']) {
    for (const approved of [true, false]) {
      const decisionType = { proposal: 'proposal', candidate: 'pursuit_candidate_review', next: 'pursuit_next_action', runtime: 'runtime_attempt_review' }[kind]
      const id = { proposal: `proposal:${decisionId}`, candidate: `pursuit:${pursuitId}:candidate-review`, next: `pursuit:${pursuitId}:next-action`, runtime: `runtime:${attemptId}:review` }[kind]
      const input = { pursuit: { id: pursuitId }, decision: { id, decisionType, status: 'pending', riskLevel: 'medium',
        workflowId, yesLabel: 'Accept', noLabel: 'Decline', evidenceUri: kind === 'runtime' ? `automation-launch://${attemptId}` : undefined } }
      let reply = { pursuit: { id: pursuitId, status: 'active', archived: false, sourceOfCreation: 'pursuit_intake' },
        decisionQueue: [], activity: [{ id: decisionId, pursuitId, eventType: kind === 'candidate' ? 'pursuit.candidate_accepted' : 'pursuit.decision_resolved',
          sourceId: id, sourceType: `pursuit_decision:${decisionType}:${approved ? 'approved' : 'rejected'}`, createdAt: '2026-10-23T10:00:00Z' }],
        workflows: [], links: [], runtimeAttempts: [{ id: attemptId }],
      }
      if (kind === 'proposal') reply = { item: { id: workflowId },
        proposals: [{ id: decisionId, workflowId, status: approved ? 'approved' : 'rejected', selectedOption: approved ? 'Accept' : 'Decline', resolvedAt: '2026-10-23T10:00:00Z' }],
        decisions: [{ id: attemptId, workflowId, decisionType: 'proposal', decision: approved ? 'approved' : 'rejected', approved, createdAt: '2026-10-23T10:00:00Z' }],
      }
      if (kind === 'candidate' && !approved) reply = { id: pursuitId, archived: true, status: 'archived' }
      if (['next', 'runtime'].includes(kind) && approved) {
        reply.workflows = [{ id: workflowId, sourceType: 'pursuit_decision', sourceId: id,
          sourceUri: kind === 'runtime' ? `automation-launch://${attemptId}` : `pursuit://${pursuitId}`, requiresApproval: true, archived: false }]
        reply.links = [{ pursuitId, linkId: workflowId, linkType: 'workflow',
          relationship: kind === 'runtime' ? 'runtime_recovery_workflow' : 'approved_next_action_workflow' }]
      }
      const url = kind === 'proposal' ? `/api/v1/workflow/${workflowId}/proposals/${decisionId}/resolve`
        : `/api/v1/pursuits/${pursuitId}/${kind === 'candidate' ? approved ? 'candidate/accept' : 'archive' : 'decisions/resolve'}`
      cases.push({ kind, approved, input, reply, url })
    }
  }
  return cases
}

for (const entry of remainingCases()) {
  test(`command dashboard binds ${entry.kind} ${entry.approved ? 'acceptance' : 'rejection'} through its real service`, () => {
    const f = fixture(of(entry.reply), false, true)
    f.component.resolveDashboardDecision(entry.input, entry.approved)
    assert.equal(f.requests.length, 1)
    assert.equal(f.requests[0][0], entry.url)
    assert.equal(f.notices[0]?.kind, 'info')
    assert.match(f.notices[0].args.join(' '), /not proof|not independent/)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
  })
}

test('all remaining decisions reject empty acknowledgements instead of claiming progress', () => {
  for (const entry of remainingCases()) {
    const f = fixture(of({}))
    f.component.resolveDashboardDecision(entry.input, entry.approved)
    assert.equal(f.notices[0]?.kind, 'warning', entry.kind)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.refreshes.length, 1)
  }
})

test('remaining decisions release empty, error and synchronous failures without raw diagnostics', () => {
  for (const entry of remainingCases()) for (const mode of ['empty', 'error', 'throw']) {
    const f = fixture(mode === 'empty' ? EMPTY : throwError(() => new Error('private-provider-detail')), mode === 'throw')
    f.component.resolveDashboardDecision(entry.input, entry.approved)
    assert.equal(f.notices[0]?.kind, 'warning', `${entry.kind}/${mode}`)
    assert.doesNotMatch(JSON.stringify(f.notices), /private-provider-detail/)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.requests.length, 1)
  }
})

test('remaining decisions serialize dispatch, observe one result and stop observing on destroy', () => {
  for (const entry of remainingCases()) for (const destroy of [false, true]) {
    const response = new Subject(), f = fixture(response)
    f.component.resolveDashboardDecision(entry.input, entry.approved)
    f.component.resolveDashboardDecision(entry.input, entry.approved)
    assert.equal(f.requests.length, 1, entry.kind)
    if (destroy) f.component.ngOnDestroy()
    response.next(entry.reply); response.next(entry.reply)
    if (destroy) f.component.resolveDashboardDecision(entry.input, entry.approved)
    assert.equal(f.requests.length, 1)
    assert.equal(response.observed, false, entry.kind)
    assert.equal(f.component.resolvingDashboardDecisionId, '')
    assert.equal(f.notices.length, destroy ? 0 : 1)
    assert.equal(f.refreshes.length, destroy ? 0 : 1)
  }
})

test('remaining decision timeouts never repeat the consequential request', () => {
  for (const entry of remainingCases()) {
    let f
    new TestScheduler(() => {}).run(() => {
      f = fixture(NEVER)
      f.component.resolveDashboardDecision(entry.input, entry.approved)
    })
    assert.equal(f.component.resolvingDashboardDecisionId, '', entry.kind)
    assert.equal(f.notices[0]?.kind, 'warning')
    assert.equal(f.requests.length, 1)
  }
})

test('remaining decision reference mismatches are rejected before mutation dispatch', () => {
  for (const entry of remainingCases()) {
    const input = structuredClone(entry.input)
    input.decision.id = 'foreign'
    const f = fixture()
    f.component.resolveDashboardDecision(input, entry.approved)
    assert.equal(f.requests.length, 0, entry.kind)
  }
  const entry = remainingCases().find(value => value.kind === 'runtime' && value.approved)
  entry.input.decision.evidenceUri = `automation-launch://${workflowId}`
  const f = fixture()
  f.component.resolveDashboardDecision(entry.input, true)
  assert.equal(f.requests.length, 0)
})

test('next-action and recovery approvals require exact approval-gated workflow links', () => {
  for (const entry of remainingCases().filter(value => ['next', 'runtime'].includes(value.kind) && value.approved)) {
    for (const mutation of [value => { value.workflows = [] }, value => { value.links = [] },
      value => { value.workflows[0].sourceId = 'foreign' }, value => { value.workflows[0].requiresApproval = false },
      value => { value.links[0].pursuitId = workflowId }]) {
      const reply = structuredClone(entry.reply); mutation(reply)
      const f = fixture(of(reply), false, true)
      f.component.resolveDashboardDecision(entry.input, true)
      assert.equal(f.notices[0]?.kind, 'warning', entry.kind)
    }
  }
})

test('remaining decisions reject superficially valid but contradictory record evidence', () => {
  for (const entry of remainingCases()) {
    const changes = entry.kind === 'proposal'
      ? [value => { value.proposals[0].status = 'open' }, value => { value.proposals[0].selectedOption = 'other' },
        value => { value.proposals[0].resolvedAt = 'invalid' }, value => { value.decisions[0].workflowId = pursuitId }]
      : entry.kind === 'candidate' && !entry.approved
        ? [value => { value.id = workflowId }, value => { value.archived = false }, value => { value.status = 'active' }]
        : [value => { value.pursuit.id = workflowId }, value => { value.pursuit.archived = true },
          value => { value.activity = [] }, value => { value.activity[0].pursuitId = workflowId },
          value => { value.activity[0].createdAt = 'invalid' },
          value => { value.decisionQueue = [{ id: entry.input.decision.id, status: 'pending' }] }]
    if (entry.kind === 'candidate' && entry.approved) changes.push(value => { value.pursuit.sourceOfCreation = 'pursuit_candidate' })
    if (entry.kind === 'runtime') changes.push(value => { value.runtimeAttempts = [] })
    for (const change of changes) {
      const reply = structuredClone(entry.reply); change(reply)
      const f = fixture(of(reply), false, true)
      f.component.resolveDashboardDecision(entry.input, entry.approved)
      assert.equal(f.notices[0]?.kind, 'warning', entry.kind)
      assert.equal(f.component.resolvingDashboardDecisionId, '')
    }
  }
})

test('dashboard decision dispatch tolerates incomplete untrusted cards without throwing', () => {
  for (const input of [null, {}, { decision: {} }, { ...completionCard(), pursuit: null },
    { ...completionCard(), decision: { ...completionCard().decision, evidenceUri: {} } }]) {
    const f = fixture()
    assert.doesNotThrow(() => f.component.resolveDashboardDecision(input, true))
    assert.equal(f.requests.length, 0)
  }
})

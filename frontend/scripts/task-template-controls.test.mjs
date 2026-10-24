import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { parseTemplate, TmplAstRecursiveVisitor, tmplAstVisitAll } from '@angular/compiler'

const path = new URL('../src/app/pages/task-blueprint/task-blueprint.component.html', import.meta.url)
const source = readFileSync(path, 'utf8')

function elements(text) {
  const parsed = parseTemplate(text, 'task-blueprint.component.html')
  assert.equal(parsed.errors?.length ?? 0, 0, parsed.errors?.map(error => error.message).join('; '))
  const result = []
  class Visitor extends TmplAstRecursiveVisitor {
    visitElement(node) {
      result.push(node)
      super.visitElement(node)
    }
  }
  tmplAstVisitAll(new Visitor(), parsed.nodes)
  return result
}

const attribute = (node, name) => node.attributes.find(value => value.name === name)?.value
const input = (node, name) => node.inputs.find(value => value.name === name)?.value.source
const click = node => node.outputs.find(value => value.name === 'click')?.handler.source
const buttons = text => elements(text).filter(node => node.name === 'button')
const implicitSubmit = text => buttons(text).filter(node => attribute(node, 'type') !== 'button')

test('task controls never acquire implicit form-submit behavior', () => {
  assert.ok(buttons(source).length >= 40, 'inspect the real template, including structural directives')
  assert.deepEqual(implicitSubmit(source).map(click), [])
})

test('button contract detects omissions inside structural and explicit templates', () => {
  assert.equal(implicitSubmit('<form><button *ngIf="visible" (click)="act()">Run</button><ng-template><button type="button">Inspect</button></ng-template></form>').length, 1)
  assert.equal(implicitSubmit('<button [type]="mode">Run</button>').length, 1)
  assert.equal(implicitSubmit('<button type="button">Run</button>').length, 0)
})

test('both approval surfaces preserve stricter approval and independent rejection gates', () => {
  for (const [handler, gate] of [['resolveReviewItem(item, true)', '!canApproveReview(item)'], ['resolveReviewItem(item, false)', '!canResolveReview(item)']]) {
    const matches = buttons(source).filter(node => click(node) === handler)
    assert.equal(matches.length, 2, handler)
    for (const node of matches) {
      assert.equal(input(node, 'disabled'), gate)
      assert.equal(input(node, 'nzLoading'), 'resolvingReviewId === item.id')
    }
  }
})

test('planning, run, cycle and uncertain retry retain their distinct action gates', () => {
  const controls = buttons(source)
  for (const [handler, gate] of [
    ['createPlan()', 'commandInFlight'],
    ['runSuccessEngine()', '!canRunSafeSteps()'],
    ['runAgentCycle()', 'commandInFlight || uncertainCommand || retryCheckLoading || executionSafetyPlan()'],
    ['refreshHistoryBeforeUncertainRetry()', 'commandInFlight || retryConfirmationOpen || !canRetryUncertainCommand()'],
    ['applyApprovedReviewRecovery()', '!canApplyApprovedReviewRecovery()'],
  ]) {
    const matches = controls.filter(node => click(node) === handler)
    assert.equal(matches.length, 1, handler)
    assert.equal(input(matches[0], 'disabled'), gate)
  }
})

test('navigation and advisory-criteria controls remain non-submitting commands', () => {
  for (const handler of ['openCommandPursuit()', 'useTypedProposalCriteria()', 'useCrewProposalCriteria()', 'previewApprovedReviewRecovery()']) {
    const matches = buttons(source).filter(node => click(node) === handler)
    assert.equal(matches.length, 1, handler)
    assert.equal(attribute(matches[0], 'type'), 'button')
  }
})

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { parseTemplate, TmplAstRecursiveVisitor, tmplAstVisitAll } from '@angular/compiler'

const file = new URL('../src/app/pages/connected-sources/connected-sources.component.html', import.meta.url)

function elements() {
  const parsed = parseTemplate(readFileSync(file, 'utf8'), file.pathname)
  assert.equal(parsed.errors?.length ?? 0, 0)
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

test('source extraction metric context uses the actual page and API total', () => {
  const metrics = elements().filter(node => node.children.some(child =>
    child.children?.some(text => text.value?.source?.includes('extractionCountMetric()'))))
  assert.equal(metrics.length, 1)
  assert.ok(metrics[0].inputs.some(input => input.name === 'title'
    && input.value.source === 'extractionScopeSummary()'))
})

test('source action buttons declare their form behavior explicitly', () => {
  for (const button of elements().filter(node => node.name === 'button')) {
    const type = button.attributes.find(attribute => attribute.name === 'type')?.value
    assert.ok(type === 'button' || type === 'submit', button.sourceSpan.start.toString())
    if (type === 'submit') {
      assert.equal(button.outputs.some(output => output.name === 'click'), false)
    }
  }
})

test('source destructive approval retains a form submit and non-submit recovery controls', () => {
  const nodes = elements()
  const approvalForm = nodes.find(node => node.name === 'form'
    && node.outputs.some(output => output.name === 'ngSubmit'
      && output.handler.source === 'submitDestructiveApproval()'))
  assert.ok(approvalForm)
  const submits = nodes.filter(node => node.name === 'button'
    && node.attributes.some(attribute => attribute.name === 'type' && attribute.value === 'submit'))
  assert.equal(submits.length, 1)
  assert.ok(submits[0].inputs.some(input => input.name === 'disabled'
    && input.value.source === '!destructiveCanSubmit()'))
})

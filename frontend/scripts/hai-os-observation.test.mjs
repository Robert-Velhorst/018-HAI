import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import ts from 'typescript'
import '@angular/compiler'
import { Subject } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const { HAIOSComponent } = await loadComponentLogic(new URL('../src/app/pages/hai-os/hai-os.component.ts', import.meta.url), {
  '../../services/hai-os/hai-os.service.token': 'data:text/javascript,export const HAI_OS_SERVICE_TOKEN = Symbol("test-hai-os")',
  '../../control-room/module-registry': new URL('../src/app/control-room/module-registry.ts', import.meta.url).href,
})
// Reuse the declared component fixture, without running its browser test suite.
const fixtureSource = readFileSync(new URL('../src/app/pages/hai-os/hai-os.component.spec.ts', import.meta.url), 'utf8')
const source = ts.createSourceFile('fixture.ts', fixtureSource, ts.ScriptTarget.Latest, true)
let fixture
function find(node) {
  if (ts.isFunctionDeclaration(node) && node.name?.text === 'overview') fixture = node
  ts.forEachChild(node, find)
}
find(source)
assert.ok(fixture, 'Expected the HAI OS overview fixture')
const fixtureCode = ts.transpileModule(fixture.getText(source), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
const overview = new Function(`${fixtureCode}; return overview;`)()

test('HAI OS observations preserve valid state and release empty, failed, superseded, or destroyed reads', () => {
  for (const mode of ['valid', 'empty', 'error', 'superseded', 'destroyed', 'null', 'pursuit', 'count', 'flag', 'metric', 'spotlight', 'timestamp']) {
    const first = new Subject(); const second = new Subject(); let calls = 0
    const component = new HAIOSComponent({ overview: () => ++calls === 1 ? first : second }, {}, { markForCheck() {} }, {})
    const previous = overview()
    component.overview = previous
    try {
      component.refresh()
      let response = overview()
      if (mode === 'null') response = null
      if (mode === 'pursuit') response.pursuitOverview = undefined
      if (mode === 'count') response.pursuitOverview.totalActive = '0'
      if (mode === 'flag') response.emergencyStop = 'false'
      if (mode === 'metric') response.metrics[0].value = NaN
      if (mode === 'spotlight') response.pursuitOverview.spotlight = [null]
      if (mode === 'timestamp') response.generatedAt = 'unknown'
      if (mode === 'superseded') component.refresh()
      if (mode === 'destroyed') component.ngOnDestroy()
      if (mode === 'empty') first.complete()
      else if (mode === 'error') first.error(new Error('private-provider-token'))
      else first.next(response)
      if (mode === 'valid' || mode === 'timestamp') {
        assert.equal(component.overview, response)
        if (mode === 'timestamp') assert.equal(component.hasValidTimestamp(response.generatedAt), false)
        first.next(overview())
        assert.equal(component.overview, response, 'only one response is admitted')
      } else assert.equal(component.overview, previous, mode)
      if (mode === 'superseded') {
        assert.equal(component.loading, true)
        second.complete()
      }
      assert.equal(component.loading, false, mode)
      if (!['valid', 'timestamp', 'destroyed'].includes(mode)) assert.match(component.errorMessage, /last successfully loaded overview/, mode)
      assert.doesNotMatch(component.errorMessage, /private-provider-token/)
      component.ngOnDestroy()
      const before = calls
      component.refresh()
      assert.equal(calls, before, 'destroyed page must not fetch again')
    } finally { component.ngOnDestroy(); first.complete(); second.complete() }
  }
})

test('HAI OS timeout releases a stalled read and ignores late responses', (context) => {
  context.mock.timers.enable({ apis: ['Date', 'setInterval'] })
  const response = new Subject()
  const previous = overview()
  const component = new HAIOSComponent({ overview: () => response }, {}, { markForCheck() {} }, {})
  component.overview = previous
  try {
    component.refresh()
    context.mock.timers.tick(29999)
    assert.equal(component.loading, true)
    assert.equal(component.errorMessage, '')
    context.mock.timers.tick(1)
    assert.equal(component.loading, false)
    assert.match(component.errorMessage, /last successfully loaded overview/)
    assert.equal(response.observed, false)
    response.next(overview())
    assert.equal(component.overview, previous)
  } finally {
    component.ngOnDestroy()
    response.complete()
    context.mock.timers.reset()
  }
})

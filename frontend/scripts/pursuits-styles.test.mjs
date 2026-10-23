import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { styleFacts, hasDeclaration } from './pursuits-style-facts.mjs'

const source = readFileSync(new URL('../src/app/pages/pursuits/pursuits.component.scss', import.meta.url), 'utf8')
const styles = styleFacts(source)

test('stylesheet facts expand selector lists structurally and retain media context', () => {
  const sample = styleFacts('@media (max-width: 700px) { :is(.a, .b) :is(h3, h4) { color: var(--text); } }')
  assert.equal(sample.facts.size, 4)
  for (const parent of ['.a', '.b']) for (const child of ['h3', 'h4']) {
    assert.equal(hasDeclaration(sample, `${parent} ${child}`, 'color', 'var(--text)', ['@media (max-width: 700px)']), true)
    assert.equal(hasDeclaration(sample, `${parent} ${child}`, 'color', 'var(--text)'), false)
  }
})

test('Pursuits preserves theme tokens and explicit keyboard focus declarations', () => {
  assert.equal(hasDeclaration(styles, '.pursuits-page', 'background', 'var(--hai-page-bg)'), true)
  assert.equal(hasDeclaration(styles, '.pursuits-page', 'color', 'var(--hai-text)'), true)
  for (const element of ['button', 'a', 'input', 'textarea', 'select']) {
    assert.equal(hasDeclaration(styles, `.pursuits-page ${element}:focus-visible`, 'outline', '2px solid var(--hai-blue)'), true)
    assert.equal(hasDeclaration(styles, `.pursuits-page ${element}:focus-visible`, 'outline-offset', '2px'), true)
  }
})

test('Pursuits retains distinct failure and runtime recovery status declarations', () => {
  assert.equal(hasDeclaration(styles, '.pursuits-load-error > i', 'color', 'var(--hai-red)'), true)
  assert.equal(hasDeclaration(styles, '.pursuit-detail-state--error > strong', 'color', 'var(--hai-red)'), true)
  assert.equal(hasDeclaration(styles, '.runtime-stop-recovery > i', 'color', 'var(--hai-warning)'), true)
  assert.equal(hasDeclaration(styles, '.resource-ledger__state--error', 'color', 'var(--hai-danger)'), true)
})

test('Pursuits retains single-column responsive controls at their declared breakpoints', () => {
  assert.equal(hasDeclaration(styles, '.main-grid', 'grid-template-columns', '1fr', ['@media (max-width: 1100px)']), true)
  for (const selector of ['.resource-ledger__form', '.runtime-evidence', '.portfolio-dispatch__action-row',
    '.portfolio-workflow-authorization__contract', '.portfolio-workflow-receipt dl', '.portfolio-workflow-settlement__usage']) {
    assert.equal(hasDeclaration(styles, selector, 'grid-template-columns', '1fr', ['@media (max-width: 700px)']), true, selector)
  }
})

test('Pursuits retains wrapping and responsive project dossier declarations', () => {
  assert.equal(hasDeclaration(styles, '.project-dossier-summary small', 'overflow-wrap', 'anywhere'), true)
  assert.equal(hasDeclaration(styles, '.project-dossier-detail__records li', 'overflow-wrap', 'anywhere'), true)
  assert.equal(hasDeclaration(styles, '.project-dossier-detail__records', 'grid-template-columns', '1fr', ['@media (max-width: 760px)']), true)
  assert.equal(hasDeclaration(styles, '.project-dossier-summary__metrics', 'grid-template-columns', 'repeat(2, minmax(0, 1fr))', ['@media (max-width: 760px)']), true)
})

test('Pursuits keeps the workflow receipt caution and stale reservation boundaries', () => {
  assert.equal(hasDeclaration(styles, '.portfolio-workflow-receipt__boundary', 'border-left', '3px solid var(--hai-warning)'), true)
  assert.equal(hasDeclaration(styles, '.resource-reservations > article.resource-reservation--stale', 'border-color', 'var(--hai-warning)'), true)
  assert.equal(hasDeclaration(styles, '.portfolio-workflow-authorization__contract', 'grid-template-columns', 'repeat(2, minmax(0, 1fr))'), true)
})

test('stylesheet checks detect removed focus and changed breakpoint context in memory', () => {
  const withoutFocus = styleFacts(source.replaceAll('outline: 2px solid var(--hai-blue);', 'outline: none;'))
  assert.equal(hasDeclaration(withoutFocus, '.pursuits-page button:focus-visible', 'outline', '2px solid var(--hai-blue)'), false)
  const changedBreakpoint = styleFacts(source.replaceAll('@media (max-width: 760px)', '@media (max-width: 761px)'))
  assert.equal(hasDeclaration(changedBreakpoint, '.project-dossier-detail__records', 'grid-template-columns', '1fr', ['@media (max-width: 760px)']), false)
  assert.equal(hasDeclaration(changedBreakpoint, '.project-dossier-detail__records', 'grid-template-columns', '1fr', ['@media (max-width: 761px)']), true)
  assert.throws(() => hasDeclaration(styles, '', 'color', 'var(--hai-text)'), /Empty stylesheet selector/)
})

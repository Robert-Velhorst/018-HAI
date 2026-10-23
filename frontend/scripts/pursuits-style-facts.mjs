import { createRequire } from 'node:module'

// Resolve the project's existing build-owned tools, including nested installs.
const require = createRequire(import.meta.url)
const buildRequire = createRequire(require.resolve('@angular/build/package.json'))
const beastiesRequire = createRequire(buildRequire.resolve('beasties'))
const selectorRequire = createRequire(beastiesRequire.resolve('css-select'))
const sass = buildRequire('sass')
const postcss = beastiesRequire('postcss')
const selectors = selectorRequire('css-what')

function expandIs(tokens) {
  let combinations = [[]]
  for (const token of tokens) {
    const choices = token.type === 'pseudo' && token.name === 'is'
      ? token.data.flatMap(expandIs) : [[token]]
    combinations = combinations.flatMap(left => choices.map(right => [...left, ...right]))
  }
  return combinations
}

function selectorKeys(selector) {
  const expanded = selectors.parse(selector).flatMap(expandIs)
  if (!expanded.length || expanded.some(tokens => !tokens.length)) throw new Error('Empty stylesheet selector')
  return expanded.map(tokens => selectors.stringify([tokens]))
}

// Presence only: rule order, specificity, layout and contrast need separate checks.
export function styleFacts(source) {
  const compiled = sass.compileString(source, { style: 'expanded', sourceMap: false })
  const root = postcss.parse(compiled.css)
  const facts = new Set()
  let rules = 0
  root.walkRules(rule => {
    rules++
    const context = []
    for (let parent = rule.parent; parent && parent.type !== 'root'; parent = parent.parent) {
      if (parent.type !== 'atrule') throw new Error('Unexpected compiled stylesheet parent')
      context.unshift(`@${parent.name} ${parent.params}`)
    }
    for (const selector of selectorKeys(rule.selector)) {
      for (const node of rule.nodes) {
        if (node.type === 'decl') facts.add(JSON.stringify([context, selector, node.prop, node.value, node.important === true]))
        else if (node.type !== 'comment') throw new Error('Unexpected compiled stylesheet node')
      }
    }
  })
  return { facts, rules, cssBytes: Buffer.byteLength(compiled.css) }
}

export function hasDeclaration(analysis, selector, property, value, context = [], important = false) {
  return selectorKeys(selector).every(key => analysis.facts.has(JSON.stringify([context, key, property, value, important])))
}

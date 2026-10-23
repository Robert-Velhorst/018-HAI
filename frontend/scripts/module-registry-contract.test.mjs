import assert from 'node:assert/strict'
import { test } from 'node:test'
import { HAI_MODULES, HAI_MODULE_GROUPS, moduleDocumentTitle, resolveProgressiveSection } from '../src/app/control-room/module-registry.ts'

function assertRegistryIdentities(modules) {
  assert.equal(new Set(modules.map(module => module.id)).size, modules.length, 'module IDs must not share saved preferences')
  assert.equal(new Set(modules.map(module => module.route)).size, modules.length, 'routes must have a single module')
  for (const module of modules) {
    assert.match(module.id, /^[a-z][a-z0-9-]*$/)
    assert.match(module.route, /^\/[a-z][a-z0-9-]*$/)
    assert.ok(HAI_MODULE_GROUPS.some(group => group.id === module.group), 'unknown navigation group')
    assert.ok(module.primaryAction || module.primaryActionOwner === 'module', 'primary action owner missing')
    assert.ok(module.title.trim())
    assert.equal(moduleDocumentTitle(module), `${module.title} | HAI Automation Hub`)
    const sections = [...module.advancedSectionIds ?? [], ...module.basicSectionIds ?? []]
    assert.equal(new Set(sections).size, sections.length, 'section disclosure modes must not overlap')
    for (const id of new Set([...sections, ...Object.keys(module.progressiveSectionParents ?? {})])) {
      assert.ok(resolveProgressiveSection(module, id), `unresolvable ${module.id} section: ${id}`)
    }
  }
}

test('real module identities isolate preferences and resolve every declared static section', () => {
  assertRegistryIdentities(HAI_MODULES)
})

test('registry contract rejects identity collisions, invalid groups and contradictory disclosure', () => {
  const first = HAI_MODULES[0]
  for (const replacement of [
    { ...first, id: HAI_MODULES[1].id },
    { ...first, route: HAI_MODULES[1].route },
    { ...first, group: 'unknown-group' },
    { ...first, basicSectionIds: [first.advancedSectionIds[0]] },
    { ...first, progressiveSectionParents: { [first.advancedSectionIds[0]]: 'unregistered-parent' } },
  ]) assert.throws(() => assertRegistryIdentities([replacement, ...HAI_MODULES.slice(1)]))
})

test('nested disclosure keeps outer-to-inner context and inherits the deepest required mode', () => {
  const module = {
    advancedSectionIds: ['outer'], basicSectionIds: ['middle', 'leaf', 'basic-only'],
    progressiveSectionParents: { middle: 'outer', leaf: 'middle' },
  }
  assert.deepEqual(resolveProgressiveSection(module, 'leaf'), {
    sectionId: 'leaf', ancestorIds: ['outer', 'middle'], requiredMode: 'advanced',
  })
  assert.deepEqual(resolveProgressiveSection(module, 'basic-only'), {
    sectionId: 'basic-only', ancestorIds: [], requiredMode: 'basic',
  })
})

test('disclosure resolution rejects self-cycles, multi-node cycles, missing parents and unknown targets', () => {
  for (const parents of [{ leaf: 'leaf' }, { leaf: 'outer', outer: 'leaf' }, { leaf: 'missing' }]) {
    assert.equal(resolveProgressiveSection({ basicSectionIds: ['leaf', 'outer'], progressiveSectionParents: parents }, 'leaf'), undefined)
  }
  assert.equal(resolveProgressiveSection({ basicSectionIds: ['leaf'] }, 'unknown'), undefined)
})

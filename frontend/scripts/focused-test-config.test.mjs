import assert from 'node:assert/strict'
import { dirname, resolve } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { readConfiguration } from '@angular/compiler-cli'
import { compilerConfiguration } from './check-control-room-compiler.mjs'

test('workflow browser specs have an isolated strict readonly compiler group', () => {
  const workflow = compilerConfiguration(false, false, false, true)
  assert.equal(workflow.rootNames.length, 1)
  assert.match(workflow.rootNames[0].replaceAll('\\', '/'), /pages\/workflow-engine\/workflow-engine.component.spec.ts$/)
  assert.equal(workflow.options.noEmit, true)
  assert.equal(workflow.options.strict, true)
  assert.equal(workflow.options.strictTemplates, true)
  for (const group of [[true, false, false, true], [false, true, false, true], [false, false, true, true]]) {
    assert.throws(() => compilerConfiguration(...group), /one compiler group/)
  }
})

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const parse = (name) => {
  const path = resolve(root, name)
  const source = ts.readConfigFile(path, ts.sys.readFile)
  assert.equal(source.error, undefined)
  const config = ts.parseJsonConfigFileContent(source.config, ts.sys, root, undefined, path)
  assert.deepEqual(config.errors, [])
  return config
}

test('focused account-bridge tests retain compiler safety and the full suite remains intact', () => {
  const full = parse('tsconfig.spec.json')
  const focused = parse('tsconfig.account-bridges.spec.json')
  const selected = resolve(root, 'src/app/pages/account-bridges/account-bridges.component.spec.ts')
  const specs = (config) => config.fileNames.filter((name) => name.endsWith('.spec.ts')).map((name) => resolve(name))
  assert.deepEqual(specs(focused), [selected])
  assert.ok(specs(full).includes(selected))
  assert.ok(specs(full).length > specs(focused).length)
  const { configFilePath: focusedPath, ...focusedOptions } = focused.options
  const { configFilePath: fullPath, ...fullOptions } = full.options
  assert.notEqual(focusedPath, fullPath)
  assert.deepEqual(focusedOptions, fullOptions)
  assert.equal(focused.options.strict, true)
  const angular = readConfiguration(resolve(root, 'tsconfig.account-bridges.spec.json'))
  assert.deepEqual(angular.errors, [])
  assert.equal(angular.options.strictTemplates, true)
  assert.equal(angular.options.strictInjectionParameters, true)
  assert.equal(angular.options.strictInputAccessModifiers, true)
})

test('control-room checks retain strict production and spec policy without emitting files', () => {
  const app = compilerConfiguration()
  const spec = compilerConfiguration(true)
  const full = readConfiguration(resolve(root, 'tsconfig.app.json'))
  for (const key of ['strict', 'strictTemplates', 'strictInjectionParameters', 'strictInputAccessModifiers']) {
    assert.equal(app.options[key], full.options[key])
    assert.equal(spec.options[key], true)
  }
  assert.equal(app.options.noEmit, true)
  assert.equal(spec.options.noEmit, true)
  assert.deepEqual(app.rootNames.map(path => path.slice(root.length + 1).replaceAll('\\', '/')), [
    'src/app/control-room/app-shell.component.ts',
    'src/app/control-room/control-room.module.ts',
    'src/app/pages/knowledge-claims/knowledge-claims.module.ts',
    'src/app/services/framework-registry.service.ts',
    'src/app/pages/pursuits/pursuits.module.ts',
    'src/app/pages/hai-os/hai-os.module.ts',
  ])
  const fullSpecs = parse('tsconfig.spec.json').fileNames
  assert.equal(spec.rootNames.filter(path => path.endsWith('.spec.ts')).length, 5)
  assert.ok(spec.rootNames.some(path => path.replaceAll('\\', '/').endsWith('/services/automations/automations.service.spec.ts')))
  assert.ok(spec.rootNames.every(path => fullSpecs.includes(path)))
})

test('source-navigation compiler group retains strict policy without increasing the default group', () => {
  const navigation = compilerConfiguration(false, true)
  const base = compilerConfiguration()
  assert.equal(base.rootNames.length, 6)
  assert.deepEqual(navigation.rootNames.map(path => path.slice(root.length + 1).replaceAll('\\', '/')), [
    'src/app/pages/control-center/control-center.module.ts',
    'src/app/pages/ambient-brain/ambient-brain.module.ts',
    'src/app/pages/command-dashboard/command-dashboard.module.ts',
  ])
  for (const key of ['strict', 'strictTemplates', 'strictInjectionParameters', 'strictInputAccessModifiers', 'noEmit']) assert.equal(navigation.options[key], true)
  assert.throws(() => compilerConfiguration(true, true), /one compiler group/)
})

test('workflow compiler group includes the actual lazy page with strict non-emitting policy', () => {
  const workflow = compilerConfiguration(false, false, true)
  assert.deepEqual(workflow.rootNames.map(path => path.slice(root.length + 1).replaceAll('\\', '/')), ['src/app/pages/workflow-engine/workflow-engine.module.ts'])
  for (const key of ['strict', 'strictTemplates', 'strictInjectionParameters', 'strictInputAccessModifiers', 'noEmit']) assert.equal(workflow.options[key], true)
  assert.throws(() => compilerConfiguration(true, false, true), /one compiler group/)
  assert.throws(() => compilerConfiguration(false, true, true), /one compiler group/)
  assert.equal(compilerConfiguration().rootNames.length, 6)
})

import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { createCompilerHost, EmitFlags, formatDiagnostics, performCompilation, readConfiguration } from '@angular/compiler-cli'
import { assertBuildRuntime } from './build-with-limits.mjs'

const root = fileURLToPath(new URL('..', import.meta.url))

export function compilerConfiguration(specTypes = false, sourceNavigation = false, workflow = false, workflowSpecTypes = false) {
  if ([specTypes, sourceNavigation, workflow, workflowSpecTypes].filter(Boolean).length > 1) throw new Error('Select one compiler group at a time')
  const configuration = readConfiguration(resolve(root, workflowSpecTypes ? 'tsconfig.workflow.spec.json' : workflow ? 'tsconfig.workflow.app.json' : sourceNavigation ? 'tsconfig.source-navigation.app.json' : specTypes ? 'tsconfig.control-room.spec.json' : 'tsconfig.control-room.app.json'))
  if (configuration.errors.length) throw new Error(formatDiagnostics(configuration.errors))
  const options = { ...configuration.options, noEmit: true, incremental: false }
  for (const key of ['strict', 'strictTemplates', 'strictInjectionParameters', 'strictInputAccessModifiers']) {
    if (options[key] !== true) throw new Error(`Compiler check requires ${key}`)
  }
  return { rootNames: configuration.rootNames, options }
}

function check(specTypes, sourceNavigation = false, workflow = false, workflowSpecTypes = false) {
  assertBuildRuntime()
  const { rootNames, options } = compilerConfiguration(specTypes, sourceNavigation, workflow, workflowSpecTypes)
  let diagnostics
  if (specTypes || workflowSpecTypes) {
    const host = ts.createCompilerHost(options)
    host.writeFile = () => { throw new Error('Readonly compiler must not emit files') }
    diagnostics = ts.getPreEmitDiagnostics(ts.createProgram(rootNames, options, host))
  } else {
    const host = createCompilerHost({ options })
    host.writeFile = () => { throw new Error('Readonly compiler must not emit files') }
    diagnostics = performCompilation({ rootNames, options, host, emitFlags: EmitFlags.None }).diagnostics
  }
  if (diagnostics.length) console.error(formatDiagnostics(diagnostics))
  const errors = diagnostics.filter(item => item.category === ts.DiagnosticCategory.Error)
  if (errors.length) return 1
  console.log(`${specTypes || workflowSpecTypes ? 'Control-room spec TypeScript' : 'Control-room Angular templates and TypeScript'} passed; ${rootNames.length} root files; no output emitted.`)
  return 0
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2)
    if (args.length > 1 || (args.length === 1 && !['--spec-types', '--source-navigation', '--workflow', '--workflow-spec-types'].includes(args[0]))) throw new Error('Select --spec-types, --source-navigation, --workflow or --workflow-spec-types, or use the default group')
    process.exitCode = check(args[0] === '--spec-types', args[0] === '--source-navigation', args[0] === '--workflow', args[0] === '--workflow-spec-types')
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}

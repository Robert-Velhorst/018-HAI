import { spawnSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const script = fileURLToPath(import.meta.url)
const frontend = resolve(dirname(script), '..')
const choices = new Set(['1', '2', '4', '8'])
const cliRequire = createRequire(resolve(frontend, 'node_modules/@angular/cli/package.json'))

export function assertBuildRuntime(version = process.versions.node) {
  const manifest = JSON.parse(readFileSync(resolve(frontend, 'package.json'), 'utf8'))
  // Use the locked CLI's existing semver dependency, not a second range parser.
  const { satisfies, validRange } = cliRequire('semver')
  const range = manifest.engines?.node
  if (typeof range !== 'string' || !validRange(range) || !satisfies(version, range)) {
    throw new Error(`Frontend builds require Node ${range ?? '(missing engine policy)'}; current version is ${version}`)
  }
}

export function buildInvocation(args = [], env = process.env) {
  const hai = env.HAI_FRONTEND_BUILD_WORKERS
  const angular = env.NG_BUILD_MAX_WORKERS
  for (const value of [hai, angular]) {
    if (value !== undefined && !choices.has(value)) {
      throw new Error('Frontend build workers must be one of 1, 2, 4, 8')
    }
  }
  if (hai !== undefined && angular !== undefined && hai !== angular) {
    throw new Error('HAI_FRONTEND_BUILD_WORKERS and NG_BUILD_MAX_WORKERS must agree')
  }
  return {
    executable: process.execPath,
    args: [resolve(frontend, 'node_modules/@angular/cli/bin/ng.js'), 'build', ...args],
    options: {
      cwd: frontend,
      env: { ...env, NG_BUILD_MAX_WORKERS: hai ?? angular ?? '1' },
      stdio: 'inherit',
      shell: false,
      windowsHide: true,
    },
  }
}

export function runBuild(args = [], env = process.env, launch = spawnSync, nodeVersion = process.versions.node) {
  const invocation = buildInvocation(args, env)
  assertBuildRuntime(nodeVersion)
  const result = launch(invocation.executable, invocation.args, invocation.options)
  if (result.error) throw new Error('Unable to launch the local Angular build', { cause: result.error })
  if (Number.isInteger(result.status) && result.status >= 0) return result.status
  if (result.signal === 'SIGINT') return 130
  if (result.signal === 'SIGTERM') return 143
  return 1
}

if (process.argv[1] && resolve(process.argv[1]) === script) {
  try {
    process.exitCode = runBuild(process.argv.slice(2))
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}

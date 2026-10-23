import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { assertBuildRuntime, buildInvocation, runBuild } from './build-with-limits.mjs'

test('native builds default to one worker without mutating the caller environment', () => {
  const env = { PATH: 'synthetic-path', OTHER: 'retained' }
  const call = buildInvocation(['--configuration=production'], env)
  assert.equal(call.executable, process.execPath)
  assert.equal(call.options.env.NG_BUILD_MAX_WORKERS, '1')
  assert.equal(call.options.env.OTHER, 'retained')
  assert.equal(call.options.env.PATH, 'synthetic-path')
  assert.deepEqual(env, { PATH: 'synthetic-path', OTHER: 'retained' })
  assert.equal(call.options.shell, false)
  assert.equal(call.options.windowsHide, true)
  assert.equal(call.options.stdio, 'inherit')
  assert.equal(call.args[0], resolve(call.options.cwd, 'node_modules/@angular/cli/bin/ng.js'))
  assert.deepEqual(call.args.slice(1), ['build', '--configuration=production'])
})

test('validated native and Docker worker overrides share the same choices', () => {
  for (const workers of ['1', '2', '4', '8']) {
    for (const env of [
      { HAI_FRONTEND_BUILD_WORKERS: workers },
      { NG_BUILD_MAX_WORKERS: workers },
      { HAI_FRONTEND_BUILD_WORKERS: workers, NG_BUILD_MAX_WORKERS: workers },
    ]) assert.equal(buildInvocation([], env).options.env.NG_BUILD_MAX_WORKERS, workers)
  }
})

test('unsafe, empty or contradictory settings fail before launching a compiler', () => {
  for (const value of ['', '0', '-1', '2.5', '64', ' 2 ', 'private-invalid-value']) {
    for (const name of ['HAI_FRONTEND_BUILD_WORKERS', 'NG_BUILD_MAX_WORKERS']) {
      assert.throws(() => runBuild([], { [name]: value }, () => assert.fail('compiler launched')), (error) => {
        assert.equal(error.message, 'Frontend build workers must be one of 1, 2, 4, 8')
        return true
      })
    }
  }
  assert.throws(() => runBuild([], { HAI_FRONTEND_BUILD_WORKERS: '1', NG_BUILD_MAX_WORKERS: '8' }, () => assert.fail('compiler launched')), /must agree/)
})

test('watch arguments remain literal and launch once with the selected resource limit', () => {
  const args = ['--watch', '--configuration', 'development', '--output-path', 'output path & not a shell']
  let calls = 0
  const status = runBuild(args, { HAI_FRONTEND_BUILD_WORKERS: '2' }, (executable, argv, options) => {
    calls++
    assert.equal(executable, process.execPath)
    assert.deepEqual(argv.slice(1), ['build', ...args])
    assert.equal(options.env.NG_BUILD_MAX_WORKERS, '2')
    assert.equal(options.shell, false)
    return { status: 0 }
  }, '26.12.0')
  assert.equal(status, 0)
  assert.equal(calls, 1)
})

test('build failures, launch errors and interrupted builds cannot become success', () => {
  for (const [result, expected] of [
    [{ status: 0 }, 0], [{ status: 7 }, 7],
    [{ status: null, signal: 'SIGINT' }, 130],
    [{ status: null, signal: 'SIGTERM' }, 143],
    [{ status: null }, 1],
  ]) assert.equal(runBuild([], {}, () => result, '26.12.0'), expected)
  assert.throws(() => runBuild([], {}, () => ({ error: new Error('synthetic failure') }), '26.12.0'), /Unable to launch/)
})

test('native production and watch scripts cannot bypass the worker guard', () => {
  const packageJson = JSON.parse(readFileSync(new URL('../package.json', import.meta.url)))
  assert.equal(packageJson.scripts.build, 'node scripts/build-with-limits.mjs')
  assert.equal(packageJson.scripts.watch, 'node scripts/build-with-limits.mjs --watch --configuration development')
  assert.equal(packageJson.scripts.prebuild, 'node scripts/check-build-runtime.mjs && node scripts/check-progressive-sections.mjs && node scripts/check-ng-zorro-theme.mjs && node scripts/check-ng-zorro-icons.mjs')
  assert.ok(packageJson.scripts.pretest.includes('node scripts/check-ng-zorro-icons.mjs'))
  assert.ok(packageJson.scripts.pretest.includes('node scripts/check-ng-zorro-theme.mjs'))
  assert.equal(packageJson.scripts.test, 'npm run test:native && ng test --watch=false')
  assert.match(packageJson.scripts['test:native'], /^node --test --test-concurrency=1 /)
  assert.doesNotMatch(packageJson.scripts['test:native'], /\bng (?:test|build)\b/)
})

test('production builds fail if the initial bundle exceeds the strict raw-size ceiling', () => {
  const angular = JSON.parse(readFileSync(new URL('../angular.json', import.meta.url)))
  const budgets = angular.projects.app.architect.build.configurations.production.budgets
  const initial = budgets.find((budget) => budget.type === 'initial')

  assert.equal(initial.maximumWarning, '1mb')
  assert.equal(initial.maximumError, '1.02mb')
})

test('runtime policy follows package engines and rejects unsupported majors or prereleases', () => {
  for (const version of ['26.12.0', '26.12.1', '26.13.0', '26.99.0']) assert.doesNotThrow(() => assertBuildRuntime(version))
  for (const version of ['22.22.3', '26.11.9', '25.2.1', '26.0.0', '26.12.0-rc.1', 'invalid']) {
    assert.throws(() => assertBuildRuntime(version), /Frontend builds require Node \^26\.12\.0/)
    assert.throws(() => runBuild([], {}, () => assert.fail('unsupported compiler launched'), version), /Frontend builds require Node/)
  }
})

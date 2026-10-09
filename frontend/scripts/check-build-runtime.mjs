import { assertBuildRuntime } from './build-with-limits.mjs'

try {
  assertBuildRuntime()
} catch (error) {
  console.error(error.message)
  process.exitCode = 1
}

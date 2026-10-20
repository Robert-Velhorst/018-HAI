import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..')

export function writeAuthenticatedThemeManifest(root = frontend) {
  const angular = JSON.parse(readFileSync(resolve(root, 'angular.json'), 'utf8'))
  const output = angular.projects?.app?.architect?.build?.options?.outputPath
  const outputBase = typeof output === 'string' ? output : output?.base
  const browser = typeof output === 'object' && output !== null ? output.browser ?? '' : ''
  if (typeof outputBase !== 'string' || !outputBase) {
    throw new Error('Angular outputPath is missing; cannot locate authenticated theme bundle')
  }

  const outputDirectory = resolve(root, outputBase, browser)
  const bundles = readdirSync(outputDirectory).filter((name) =>
    /^authenticated-theme(?:-[\w-]+)?\.css$/.test(name),
  )
  if (bundles.length !== 1) {
    throw new Error(`Expected one authenticated-theme CSS bundle in ${outputDirectory}; found ${bundles.length}`)
  }
  const stylesheet = readFileSync(resolve(outputDirectory, bundles[0]))
  const digest = createHash('sha256').update(stylesheet).digest('hex')

  writeFileSync(
    resolve(outputDirectory, 'authenticated-theme-manifest.json'),
    `${JSON.stringify({ href: `${bundles[0]}?sha256=${digest}` })}\n`,
    'utf8',
  )
  return bundles[0]
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    console.log(`Wrote authenticated theme manifest for ${writeAuthenticatedThemeManifest()}`)
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}

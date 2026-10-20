import { readdir, readFile, stat } from 'node:fs/promises'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontend = fileURLToPath(new URL('../', import.meta.url))
const sourceRoot = join(frontend, 'src/app')
const iconRegistryPath = join(sourceRoot, 'hai-icons.ts')
const fallbackIconPath = join(frontend, 'node_modules/@ant-design/icons-angular/src/inline-svg/outline/reload.svg')

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(entries.map(async (entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return ['.html', '.ts'].includes(extname(entry.name)) && !entry.name.endsWith('.spec.ts') ? [path] : []
  }))
  return nested.flat()
}

const registry = await readFile(iconRegistryPath, 'utf8')
const registeredIcons = new Set()
for (const block of registry.matchAll(/import\s*\{([\s\S]*?)\}\s*from\s*['"]@ant-design\/icons-angular\/icons['"]/g)) {
  for (const symbol of block[1].matchAll(/\b([A-Z]\w+(?:Outline|Fill|TwoTone))\b/g)) {
    const base = symbol[1].replace(/(?:Outline|Fill|TwoTone)$/, '')
    registeredIcons.add(base.replace(/([a-z0-9])([A-Z])/g, '$1-$2').toLowerCase())
  }
}

const missing = []
for (const path of await sourceFiles(sourceRoot)) {
  const source = await readFile(path, 'utf8')
  if (path.endsWith('.html')) {
    for (const tag of source.matchAll(/<[^>]*\bnz-icon\b[^>]*>/gs)) {
      const attribute = tag[0].match(/(\[nzType\]|nzType)\s*=\s*(["'])(.*?)\2/s)
      if (!attribute) continue
      const binding = attribute[1].startsWith('[')
      const expression = attribute[3]
      const conditionalNames = [...expression.matchAll(/[?:]\s*["']([a-z][a-z0-9-]*)["']/g)].map((match) => match[1])
      const literal = expression.match(/^["']([a-z][a-z0-9-]*)["']$/)?.[1]
      const values = binding ? (conditionalNames.length ? conditionalNames : literal ? [literal] : []) : [expression]
      for (const name of values) {
        if (!registeredIcons.has(name)) missing.push(`${path.slice(sourceRoot.length + 1)}: ${name}`)
      }
    }
  }
  for (const match of source.matchAll(/\bicon\s*:\s*["']([a-z][a-z0-9-]*)["']/g)) {
    if (!registeredIcons.has(match[1])) missing.push(`${path.slice(sourceRoot.length + 1)}: ${match[1]}`)
  }
}

const angular = JSON.parse(await readFile(join(frontend, 'angular.json'), 'utf8'))
const buildAssets = angular.projects.app.architect.build.options.assets
const testAssets = angular.projects.app.architect.test.options.assets
const buildFallbackConfigured = buildAssets.some((asset) =>
  typeof asset === 'object'
  && asset.input === './node_modules/@ant-design/icons-angular/src/inline-svg/'
  && asset.glob === 'outline/reload.svg'
  && asset.output === '/assets/',
)
const testFallbackConfigured = testAssets.some((asset) =>
  typeof asset === 'object'
  && asset.input === './node_modules/@ant-design/icons-angular/src/inline-svg/'
  && asset.glob === 'outline/*.svg'
  && asset.output === '/assets/',
)

if (missing.length) {
  console.error(`NG-ZORRO icons are missing from hai-icons.ts:\n${[...new Set(missing)].sort().join('\n')}`)
  process.exitCode = 1
} else if (!buildFallbackConfigured || !testFallbackConfigured) {
  console.error('Production must copy only outline/reload.svg; isolated tests retain outline/*.svg fallbacks.')
  process.exitCode = 1
} else if ((await stat(fallbackIconPath)).size === 0) {
  console.error('The configured NG-ZORRO reload SVG fallback is missing or empty.')
  process.exitCode = 1
} else {
  console.log('NG-ZORRO registry covers static icon references; production copies only the checked reload SVG fallback.')
}

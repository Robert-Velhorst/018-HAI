import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { test } from 'node:test'
import { writeAuthenticatedThemeManifest } from './authenticated-theme-manifest.mjs'

test('writes the exact hashed authenticated stylesheet name for runtime loading', () => {
  const root = mkdtempSync(join(tmpdir(), 'hai-theme-manifest-'))
  try {
    mkdirSync(join(root, 'dist', 'app'), { recursive: true })
    writeFileSync(join(root, 'angular.json'), JSON.stringify({
      projects: { app: { architect: { build: { options: { outputPath: { base: 'dist/app', browser: '' } } } } },
    }}))
    writeFileSync(join(root, 'dist', 'app', 'authenticated-theme-X7abc_12.css'), '/* theme */')

    assert.equal(writeAuthenticatedThemeManifest(root), 'authenticated-theme-X7abc_12.css')
    const manifest = JSON.parse(readFileSync(join(root, 'dist', 'app', 'authenticated-theme-manifest.json'), 'utf8'))
    const digest = createHash('sha256').update('/* theme */').digest('hex')
    assert.deepEqual(manifest, { href: `authenticated-theme-X7abc_12.css?sha256=${digest}` })
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('fails rather than publishing an ambiguous or missing theme manifest', () => {
  const root = mkdtempSync(join(tmpdir(), 'hai-theme-manifest-'))
  try {
    mkdirSync(join(root, 'dist', 'app'), { recursive: true })
    writeFileSync(join(root, 'angular.json'), JSON.stringify({
      projects: { app: { architect: { build: { options: { outputPath: 'dist/app' } } } },
    }}))
    assert.throws(() => writeAuthenticatedThemeManifest(root), /found 0/)
    writeFileSync(join(root, 'dist', 'app', 'authenticated-theme.css'), '')
    writeFileSync(join(root, 'dist', 'app', 'authenticated-theme-abc123.css'), '')
    assert.throws(() => writeAuthenticatedThemeManifest(root), /found 2/)
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

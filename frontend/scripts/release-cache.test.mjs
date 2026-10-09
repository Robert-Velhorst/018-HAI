import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const config = readFileSync(new URL('../nginx-custom.conf', import.meta.url), 'utf8')
const hashed = /location ~ "([^"\n]+)" \{([^}]+)\}/.exec(config)
const staticFiles = /location ~\* (\S+) \{([^}]+)\}/.exec(config)

test('immutable release caching is restricted to content-hashed root bundles', () => {
  assert.ok(hashed)
  const pattern = new RegExp(hashed[1])
  for (const uri of ['/main-ABCDEFGH.js', '/chunk-B-pCdsS7.js', '/chunk-1nVTihrp2.js', '/styles-ZYXWVUTS.css']) {
    assert.ok(pattern.test(uri), uri)
  }
  for (const uri of ['/favicon.ico', '/assets/logo.svg', '/assets/main-ABCDEFGH.js', '/main.js', '/styles.css', '/main-short.js', '/styles-ABCDEFGH.css.map']) {
    assert.ok(!pattern.test(uri), uri)
  }
  assert.match(hashed[2], /try_files \$uri =404;/)
  assert.match(hashed[2], /Cache-Control "public, max-age=31536000, immutable";/)
  assert.doesNotMatch(hashed[2], /always/)
})

test('unversioned assets revalidate and missing assets cannot receive immutable headers', () => {
  assert.ok(staticFiles)
  const pattern = new RegExp(staticFiles[1], 'i')
  for (const uri of ['/favicon.ico', '/assets/logo.svg', '/assets/photo.webp', '/fonts/regular.woff2', '/main.js']) assert.ok(pattern.test(uri), uri)
  assert.match(staticFiles[2], /try_files \$uri =404;/)
  assert.match(staticFiles[2], /Cache-Control "public, max-age=0, must-revalidate";/)
  assert.doesNotMatch(staticFiles[2], /immutable|always/)
  assert.ok(config.indexOf(hashed[0]) < config.indexOf(staticFiles[0]), 'Nginx uses the first matching regex location')
})

test('document requests keep no-store policy and SPA deep-link fallback', () => {
  assert.match(config, /location = \/index\.html \{\s*add_header Cache-Control "no-store, no-cache, must-revalidate, proxy-revalidate" always;/)
  assert.match(config, /try_files \$uri \$uri\/ \/index\.html;/)
  const angular = JSON.parse(readFileSync(new URL('../angular.json', import.meta.url)))
  assert.equal(angular.projects.app.architect.build.configurations.production.outputHashing, 'all')
})

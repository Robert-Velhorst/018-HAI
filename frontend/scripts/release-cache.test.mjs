import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const config = readFileSync(new URL('../nginx-custom.conf', import.meta.url), 'utf8')
const hashed = /location ~ "([^"\n]+)" \{([^}]+)\}/.exec(config)
const staticFiles = /location ~\* (\S+) \{([^}]+)\}/.exec(config)

test('frontend listens on the non-root runtime port and exposes a frontend-only health result', () => {
  assert.match(config, /\blisten 8080;/)
  assert.doesNotMatch(config, /\blisten 80;/)
  const health = /location = \/healthz \{([^}]*return 200 '[^']+';[^}]*)\}/.exec(config)
  assert.ok(health)
  assert.match(health[1], /access_log off;/)
  assert.match(health[1], /default_type application\/json;/)
  const payload = /return 200 '([^']+)';/.exec(health[1])
  assert.deepEqual(JSON.parse(payload[1]), { service: 'frontend', status: 'ok' })
  assert.doesNotMatch(health[1], /proxy_pass|immutable/)
})

test('frontend retains bounded response compression for its text and vector assets', () => {
  assert.match(config, /\bgzip on;/)
  assert.match(config, /\bgzip_vary on;/)
  assert.match(config, /\bgzip_min_length 1024;/)
  const types = /\bgzip_types\s+([^;]+);/.exec(config)
  assert.ok(types)
  assert.deepEqual(types[1].trim().split(/\s+/).sort(), [
    'application/javascript', 'application/json', 'image/svg+xml', 'text/css', 'text/plain',
  ])
})

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
  assert.equal(angular.projects.app.architect.build.configurations.production.optimization.styles.inlineCritical, false)
})

test('theme bootstrap stays same-origin for the restrictive script policy', () => {
  const html = readFileSync(new URL('../src/index.html', import.meta.url), 'utf8')
  const initializer = readFileSync(new URL('../src/assets/theme-init.js', import.meta.url), 'utf8')
  assert.match(html, /<script src="assets\/theme-init\.js"><\/script>/)
  assert.doesNotMatch(html, /<script(?![^>]*\bsrc=)[^>]*>[\s\S]*?<\/script>/)
  assert.match(initializer, /hai-theme-mode/)
  assert.match(initializer, /data-hai-theme/)
})

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const manifest = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
const lock = JSON.parse(readFileSync(new URL('../package-lock.json', import.meta.url), 'utf8'));

test('frontend lock root matches the manifest declarations and runtime requirement', () => {
  assert.equal(lock.lockfileVersion, 3);
  assert.equal(lock.name, manifest.name);
  assert.equal(lock.version, manifest.version);
  const root = lock.packages[''];
  assert.equal(root.name, manifest.name);
  assert.equal(root.version, manifest.version);
  for (const field of ['dependencies', 'devDependencies', 'engines']) {
    assert.deepEqual(root[field], manifest[field], `lock root ${field} differs from package.json`);
  }
});

test('frontend registry records have bounded package paths and SHA-512 integrity', () => {
  const records = Object.entries(lock.packages).filter(([path]) => path !== '');
  assert.ok(records.length > 0);
  for (const [path, entry] of records) {
    assert.ok(path.startsWith('node_modules/') && !path.includes('\\') &&
      !path.includes('\0') && !path.split('/').includes('..'), 'unreviewed lock package path');
    assert.ok(typeof entry.version === 'string' && entry.version.length > 0, `missing version: ${path}`);
    assert.ok(!entry.link, `unreviewed linked dependency: ${path}`);
    let url;
    try { url = new URL(entry.resolved); } catch { assert.fail(`missing registry URL: ${path}`); }
    assert.ok(url.origin === 'https://registry.npmjs.org' && !url.username && !url.password &&
      !url.search && !url.hash, `unreviewed registry source: ${path}`);
    assert.ok(typeof entry.integrity === 'string' && /^sha512-[A-Za-z0-9+/]{86}==$/.test(entry.integrity),
      `missing SHA-512 integrity: ${path}`);
  }
});

test('every declared frontend dependency has a locked registry record', () => {
  for (const name of Object.keys({ ...manifest.dependencies, ...manifest.devDependencies })) {
    assert.ok(lock.packages[`node_modules/${name}`], `unlocked direct dependency: ${name}`);
  }
});

test('frontend install hooks remain limited to explicitly reviewed package locations', () => {
  const reviewed = new Set([
    'node_modules/@parcel/watcher', 'node_modules/esbuild', 'node_modules/fsevents',
    'node_modules/lmdb', 'node_modules/msgpackr-extract',
  ]);
  for (const [path, entry] of Object.entries(lock.packages)) {
    if (Object.hasOwn(entry, 'hasInstallScript')) {
      assert.equal(typeof entry.hasInstallScript, 'boolean', `invalid install-hook flag: ${path}`);
    }
    if (entry.hasInstallScript) assert.ok(reviewed.has(path), `install hook needs review: ${path}`);
  }
});

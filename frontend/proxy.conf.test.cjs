const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const path = require('node:path');
const test = require('node:test');

const configPath = path.join(__dirname, 'proxy.conf.cjs');

function loadProxyConfig(target) {
  const env = {
    PATH: process.env.PATH,
    SystemRoot: process.env.SystemRoot,
  };
  if (target !== undefined) env.HAI_API_PROXY_TARGET = target;

  return spawnSync(
    process.execPath,
    ['-e', `process.stdout.write(JSON.stringify(require(${JSON.stringify(configPath)})))`],
    { encoding: 'utf8', env },
  );
}

test('defaults to the local Compose gateway', () => {
  const result = loadProxyConfig();
  assert.equal(result.status, 0, result.stderr);
  const proxy = JSON.parse(result.stdout)['/api'];
  assert.equal(proxy.target, 'http://127.0.0.1:8088');
  assert.equal(proxy.changeOrigin, true);
  assert.equal(proxy.secure, false);
});

test('accepts IPv4 and IPv6 loopback gateway origins', () => {
  for (const target of ['http://127.0.0.1', 'https://[::1]:8443']) {
    const result = loadProxyConfig(target);
    assert.equal(result.status, 0, `${target}: ${result.stderr}`);
    assert.equal(JSON.parse(result.stdout)['/api'].target, target);
  }
});

test('rejects non-loopback or non-origin proxy targets', () => {
  for (const target of [
    'http://example.com',
    'ftp://127.0.0.1',
    'http://127.0.0.1/api',
    'http://127.0.0.1?token=secret',
    'http://user:pass@127.0.0.1',
  ]) {
    const result = loadProxyConfig(target);
    assert.notEqual(result.status, 0, `${target} unexpectedly loaded`);
    assert.match(result.stderr, /HAI_API_PROXY_TARGET must be a loopback HTTP\(S\) origin/);
  }
});

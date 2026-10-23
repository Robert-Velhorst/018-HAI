const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const configPath = path.join(__dirname, 'proxy.conf.cjs');

function loadProxyConfig(target) {
  const env = {
    PATH: process.env.PATH,
    SystemRoot: process.env.SystemRoot,
    NODE_DISABLE_COMPILE_CACHE: '1',
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

function assertRealGatewayConfiguration(workspace, scripts) {
  const serve = workspace.projects.app.architect.serve;
  assert.equal(path.resolve(__dirname, serve.options.proxyConfig), configPath);
  for (const configuration of Object.values(serve.configurations ?? {})) {
    const proxy = configuration.proxyConfig ?? serve.options.proxyConfig;
    assert.equal(path.resolve(__dirname, proxy), configPath);
  }
  const historicalMocks = /(?:proxy\.preview\.json|mock-preview-server\.js|\.codex-preview-server\.mjs)/;
  assert.doesNotMatch(JSON.stringify(workspace), historicalMocks);
  for (const command of Object.values(scripts)) assert.doesNotMatch(command, historicalMocks);
}

test('Angular serve configurations and package commands do not reconnect historical mock previews', () => {
  const workspace = JSON.parse(fs.readFileSync(path.join(__dirname, 'angular.json'), 'utf8'));
  const { scripts } = JSON.parse(fs.readFileSync(path.join(__dirname, 'package.json'), 'utf8'));
  assertRealGatewayConfiguration(workspace, scripts);
});

test('gateway configuration guard detects historical mock wiring in every supported entry point', () => {
  const workspace = JSON.parse(fs.readFileSync(path.join(__dirname, 'angular.json'), 'utf8'));
  const { scripts } = JSON.parse(fs.readFileSync(path.join(__dirname, 'package.json'), 'utf8'));
  const baseline = structuredClone(workspace);
  workspace.projects.app.architect.serve.options.proxyConfig = 'proxy.preview.json';
  assert.throws(() => assertRealGatewayConfiguration(workspace, scripts));
  const overridden = structuredClone(baseline);
  overridden.projects.app.architect.serve.configurations.production.proxyConfig = 'proxy.preview.json';
  assert.throws(() => assertRealGatewayConfiguration(overridden, scripts));
  for (const command of ['node mock-preview-server.js', 'node ../.codex-preview-server.mjs']) {
    assert.throws(() => assertRealGatewayConfiguration(baseline, { ...scripts, start: command }));
  }
  const injected = structuredClone(baseline);
  injected.projects.app.architect.build.options.scripts.push('mock-preview-server.js');
  assert.throws(() => assertRealGatewayConfiguration(injected, scripts));
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

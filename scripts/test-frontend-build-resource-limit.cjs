'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const dockerfile = fs.readFileSync(path.join(root, 'frontend', 'Dockerfile'), 'utf8');
const angularOptionsPath = path.join(root, 'frontend', 'node_modules', '@angular', 'build', 'src', 'utils', 'environment-options.js');
const angularOptions = new vm.Script(fs.readFileSync(angularOptionsPath, 'utf8'), { filename: angularOptionsPath });

function parsedWorkers(value) {
  const exports = {};
  const env = value === undefined ? {} : { NG_BUILD_MAX_WORKERS: value };
  // Execute the installed parser with isolated environment and CPU metadata.
  angularOptions.runInNewContext({
    exports,
    process: { env },
    require(name) {
      assert.equal(name, 'node:os');
      return { availableParallelism: () => 64 };
    },
  }, { timeout: 1000 });
  return exports.maxWorkers;
}

function instructions(source) {
  const result = [];
  let pending = '';
  for (const raw of source.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const continued = line.endsWith('\\');
    pending += (pending ? ' ' : '') + (continued ? line.slice(0, -1).trim() : line);
    if (continued) continue;
    const match = /^([A-Z]+)\s+(.+)$/.exec(pending);
    assert.ok(match, `Invalid Dockerfile instruction: ${pending}`);
    result.push({ keyword: match[1], value: match[2] });
    pending = '';
  }
  assert.equal(pending, '', 'Unterminated Dockerfile continuation');
  return result;
}

const parsed = instructions(dockerfile);
const buildIndex = parsed.findIndex(({ keyword, value }) => keyword === 'RUN' && value.includes('npm run build'));
const build = parsed[buildIndex];

test('installed Angular parser honors each bounded worker choice', () => {
  for (const workers of ['1', '2', '4', '8']) {
    assert.equal(parsedWorkers(workers), Number(workers));
  }
  assert.equal(parsedWorkers(undefined), 4);
  assert.equal(parsedWorkers(''), 4);
});

test('installed parser alone does not validate unsafe numeric choices', () => {
  assert.ok(Number.isNaN(parsedWorkers('invalid')));
  assert.equal(parsedWorkers('0'), 0);
  assert.equal(parsedWorkers('-1'), -1);
  assert.equal(parsedWorkers('2.5'), 2.5);
});

test('worker argument defaults to two after the cached dependency and source layers', () => {
  const dependencyIndex = parsed.findIndex(({ keyword, value }) => keyword === 'RUN' && value.includes('npm ci'));
  const sourceIndex = parsed.findIndex(({ keyword, value }) => keyword === 'COPY' && value === '. .');
  const argIndex = parsed.findIndex(({ keyword, value }) => keyword === 'ARG' && value === 'HAI_FRONTEND_BUILD_WORKERS=2');
  assert.ok(dependencyIndex >= 0);
  assert.ok(sourceIndex > dependencyIndex);
  assert.ok(argIndex > sourceIndex);
  assert.ok(buildIndex > argIndex);
  for (const instruction of parsed.slice(0, argIndex)) {
    assert.doesNotMatch(instruction.value, /HAI_FRONTEND_BUILD_WORKERS|NG_BUILD_MAX_WORKERS/);
  }
});

test('build validates only 1, 2, 4, 8 and scopes Angular environment to npm run build', () => {
  assert.ok(build, 'Missing build command');
  assert.match(build.value, /^case "\$HAI_FRONTEND_BUILD_WORKERS" in 1\|2\|4\|8\) ;;/);
  assert.match(build.value, /\*\) echo "HAI_FRONTEND_BUILD_WORKERS must be one of 1, 2, 4, 8" >&2; exit 1 ;; esac/);
  assert.match(build.value, /esac && NG_BUILD_MAX_WORKERS="\$HAI_FRONTEND_BUILD_WORKERS" npm run build$/);
  assert.equal((dockerfile.match(/NG_BUILD_MAX_WORKERS/g) || []).length, 1);
  assert.ok(!parsed.some(({ keyword }) => keyword === 'ENV'));
});

test('runtime stage remains the original Nginx stage without worker settings', () => {
  const runtimeIndex = parsed.findIndex(({ keyword, value }) => keyword === 'FROM' && value === 'nginx:stable-alpine-slim');
  assert.ok(runtimeIndex > buildIndex);
  assert.deepEqual(parsed.slice(runtimeIndex), [
    { keyword: 'FROM', value: 'nginx:stable-alpine-slim' },
    { keyword: 'COPY', value: '--from=build /app/dist/app /usr/share/nginx/html' },
    { keyword: 'COPY', value: './nginx-custom.conf /etc/nginx/conf.d/default.conf' },
  ]);
});

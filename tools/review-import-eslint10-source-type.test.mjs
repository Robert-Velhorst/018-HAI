import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import test from 'node:test';
import { Linter } from 'eslint';

const require = createRequire(import.meta.url);
assert.equal(require('eslint/package.json').version, '10.13.0');
const stage = process.env.IMPORT_SOURCE_TYPE_STAGE;
assert.ok(stage === 'baseline' || stage === 'port');
const { default: sourceType } = await import(stage === 'baseline'
  ? './baseline-sourceType.mjs' : './port-sourceType.mjs');

for (const expected of ['module', 'script', 'commonjs']) {
  test(`actual ESLint 10 ${expected} context`, () => {
    let calls = 0;
    const messages = new Linter().verify('var value = 1;', {
      languageOptions: { ecmaVersion: 'latest', sourceType: expected },
      plugins: {
        probe: {
          rules: {
            'source-type': {
              meta: { type: 'problem', schema: [] },
              create(context) {
                assert.equal(context.parserOptions, undefined);
                assert.equal(context.languageOptions.sourceType, expected);
                assert.equal(sourceType(context), expected);
                calls += 1;
                return {};
              },
            },
          },
        },
      },
      rules: { 'probe/source-type': 'error' },
    });
    assert.deepEqual(messages, []);
    assert.equal(calls, 1);
  });

  test(`legacy ${expected} precedence preserved`, () => {
    const modern = expected === 'module' ? 'script' : 'module';
    assert.equal(sourceType({
      parserOptions: { sourceType: expected },
      languageOptions: { sourceType: modern },
    }), expected);
  });
}

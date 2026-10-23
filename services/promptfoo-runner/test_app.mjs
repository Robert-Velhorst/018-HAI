import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { ENGINE, buildConfig, configuration, summarizeEvaluation, validateLocalModelURL } from './app.mjs';

test('reported engine agrees with the exact dependency pin', async () => {
  const manifest = JSON.parse(await readFile(new URL('./package.json', import.meta.url), 'utf8'));
  assert.match(manifest.dependencies.promptfoo, /^\d+\.\d+\.\d+$/);
  assert.equal(ENGINE, `promptfoo ${manifest.dependencies.promptfoo}`);
});

test('builds a six-case fixed safety suite for one configured local model', () => {
  const config = buildConfig({ modelId: 'qwen2.5:7b', baseUrl: 'http://ollama:11434/v1' });
  assert.equal(config.providers[0].id, 'openai:chat:qwen2.5:7b');
  assert.equal(config.tests.length, 6);
  assert.equal(config.tests.every((entry) => entry.assert[0].value === 'REFUSE_AND_REQUEST_APPROVAL'), true);
});

test('rejects non-local model endpoints and invalid model names', () => {
  assert.throws(() => validateLocalModelURL('https://example.com/v1'));
  assert.throws(() => configuration({ HAI_PROMPTFOO_MODEL_ID: 'model;command', HAI_PROMPTFOO_MODEL_BASE_URL: 'http://ollama:11434/v1' }));
  assert.deepEqual(configuration({ HAI_PROMPTFOO_MODEL_ID: 'qwen2.5:7b', HAI_PROMPTFOO_MODEL_BASE_URL: 'http://ollama:11434/v1' }), { modelId: 'qwen2.5:7b', baseUrl: 'http://ollama:11434/v1' });
});

test('summarizes only six fixed case outcomes', () => {
  const rows = Array.from({ length: 6 }, (_, index) => ({ success: index < 5 }));
  assert.deepEqual(summarizeEvaluation({ results: rows }), { passedCount: 5, failedCount: 1, score: 5 / 6 });
  assert.throws(() => summarizeEvaluation({ results: rows.slice(0, 5) }));
});

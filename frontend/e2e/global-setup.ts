import { request, FullConfig } from '@playwright/test';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { realpathSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { assertIsolatedAcceptanceTarget } from './tests/support/isolated-stack';

export default async function globalSetup(_config: FullConfig) {
  delete process.env.HAI_E2E_AUTH_STATE_PATH;
  if (process.env.E2E_ALLOW_MUTATION !== 'true') return;

  const baseURL = process.env.E2E_BASE_URL || '';
  const email = process.env.E2E_OPERATOR_EMAIL || '';
  const password = process.env.E2E_OPERATOR_PASSWORD || '';
  assertIsolatedAcceptanceTarget(baseURL, process.env.E2E_ISOLATED_STACK, email);
  if (!password) throw new Error('The isolated browser matrix requires its disposable owner password.');

  const evidencePath = process.env.HAI_ACCEPTANCE_EVIDENCE || '';
  if (!path.isAbsolute(evidencePath)) throw new Error('HAI_ACCEPTANCE_EVIDENCE must be an absolute owned evidence directory.');
  try {
    const evidenceRoot = realpathSync(evidencePath);
    const manifest = JSON.parse(await readFile(path.join(evidenceRoot, 'manifest.json'), 'utf8'));
    if (manifest.version !== 1 || !/^[0-9a-f]{32}$/.test(manifest.owner)
      || manifest.project !== `hai-acceptance-${manifest.owner.slice(0, 12)}`
      || manifest.email !== email || manifest.password !== password
      || manifest.port !== Number(new URL(baseURL).port)
      || !Number.isInteger(manifest.port) || manifest.port < 1025 || manifest.port > 65535) {
      throw new Error('ownership');
    }
  } catch {
    throw new Error('Synthetic browser authentication ownership validation failed.');
  }

  const tempRoot = await mkdtemp(path.join(os.tmpdir(), 'hai-e2e-auth-'));
  const storageStatePath = path.join(tempRoot, 'storage-state.json');
  try {
    const api = await request.newContext({ baseURL: new URL(baseURL).origin });
    try {
      const response = await api.post('/api/v1/auth/login', {
        data: { email, password, rememberMe: true },
      });
      if (!response.ok()) {
        throw new Error(`Synthetic browser authentication failed (HTTP ${response.status()}).`);
      }
      const state = await api.storageState();
      if (state.cookies.length === 0) throw new Error('Synthetic browser authentication did not establish a session cookie.');
      await writeFile(storageStatePath, JSON.stringify(state), { flag: 'wx', mode: 0o600 });
    } finally {
      await api.dispose();
    }
    process.env.HAI_E2E_AUTH_STATE_PATH = storageStatePath;
  } catch (error) {
    await rm(tempRoot, { recursive: true, force: true });
    throw error;
  }

  return async () => {
    delete process.env.HAI_E2E_AUTH_STATE_PATH;
    await rm(tempRoot, { recursive: true, force: true });
  };
}

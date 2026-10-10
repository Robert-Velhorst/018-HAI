import { expect, test as base, Page, TestInfo } from '@playwright/test';
import { readFileSync, realpathSync, statSync } from 'node:fs';
import path from 'node:path';
import { assertIsolatedAcceptanceTarget } from './isolated-stack';

export interface OwnedMatrixTarget {
  origin: string;
  email: string;
  password: string;
}

function inside(root: string, candidate: string): boolean {
  const relative = path.relative(root, candidate);
  return relative !== '' && !relative.startsWith(`..${path.sep}`)
    && relative !== '..' && !path.isAbsolute(relative);
}

function resolvedDestination(destination: string): string {
  let ancestor = destination;
  const suffix: string[] = [];
  while (!statExists(ancestor)) {
    const parent = path.dirname(ancestor);
    if (parent === ancestor) throw new Error('Evidence destination has no existing ancestor.');
    suffix.unshift(path.basename(ancestor));
    ancestor = parent;
  }
  return path.join(realpathSync(ancestor), ...suffix);
}

function statExists(value: string): boolean {
  try { statSync(value); return true; } catch { return false; }
}

export function ownedMatrixTarget(info: TestInfo): OwnedMatrixTarget {
  const origin = process.env.E2E_BASE_URL || '';
  const email = process.env.E2E_OPERATOR_EMAIL || '';
  const password = process.env.E2E_OPERATOR_PASSWORD || '';
  assertIsolatedAcceptanceTarget(origin, process.env.E2E_ISOLATED_STACK, email);
  if (process.env.E2E_ALLOW_MUTATION !== 'true') {
    throw new Error('Matrix requires E2E_ALLOW_MUTATION=true for its single synthetic login only.');
  }
  if (!password) throw new Error('Matrix requires the disposable owner password.');
  if (info.project.use.baseURL !== origin) throw new Error('Configured baseURL differs from the explicit isolated target.');
  const evidence = process.env.HAI_ACCEPTANCE_EVIDENCE || '';
  if (!path.isAbsolute(evidence)) throw new Error('HAI_ACCEPTANCE_EVIDENCE must name the parent-owned absolute evidence directory.');
  try {
    const root = realpathSync(evidence);
    const manifest = JSON.parse(readFileSync(path.join(root, 'manifest.json'), 'utf8'));
    if (manifest.version !== 1 || !/^[0-9a-f]{32}$/.test(manifest.owner)
      || manifest.project !== `hai-acceptance-${manifest.owner.slice(0, 12)}`
      || manifest.email !== email || manifest.password !== password
      || manifest.port !== Number(new URL(origin).port)
      || !Number.isInteger(manifest.port) || manifest.port < 1025 || manifest.port > 65535) {
      throw new Error('ownership');
    }
    const html = process.env.PLAYWRIGHT_HTML_OUTPUT_DIR || '';
    if (!path.isAbsolute(info.project.outputDir) || !path.isAbsolute(html)
      || !inside(root, resolvedDestination(info.project.outputDir))
      || !inside(root, resolvedDestination(html))) throw new Error('destination');
  } catch {
    // Never expose private manifest values or filesystem/parser diagnostics.
    throw new Error('Matrix evidence ownership or artifact destination validation failed.');
  }
  return { origin: new URL(origin).origin, email, password };
}

interface MatrixHealth {
  consoleErrors: number;
  pageErrors: number;
  failedRequests: number;
  httpErrors: number;
  deniedWrites: number;
  deniedExternal: number;
  pendingReads: number;
}

export interface OwnedBrowserMatrix {
  page: Page;
  target: OwnedMatrixTarget;
  settle(): Promise<void>;
  assertHealthy(): void;
  capture(name: string): Promise<void>;
}

export const test = base.extend<{ matrix: OwnedBrowserMatrix }>({
  matrix: async ({ browser }, use, info) => {
    const target = ownedMatrixTarget(info);
    if (info.config.workers !== 1) {
      throw new Error('Matrix requires --workers=1.');
    }
    // Do not inherit private cookies, storageState, HARs or recording settings.
    const context = await browser.newContext({
      baseURL: target.origin, storageState: { cookies: [], origins: [] },
      viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce',
      serviceWorkers: 'block', acceptDownloads: false,
    });
    const page = await context.newPage();
    page.setDefaultTimeout(10_000);
    page.setDefaultNavigationTimeout(15_000);
    const health: MatrixHealth = {
      consoleErrors: 0, pageErrors: 0, failedRequests: 0, httpErrors: 0,
      deniedWrites: 0, deniedExternal: 0, pendingReads: 0,
    };
    let loginAvailable = true;
    let bootstrapAuthRefusals = 0;
    const bootstrapProbe = (url: string, beforeLogin = loginAvailable): boolean => {
      try {
        const parsed = new URL(url);
        return beforeLogin && parsed.origin === target.origin
          && parsed.pathname === '/api/v1/auth/is-user-authenticated';
      } catch { return false; }
    };
    const pending = new Set<object>();
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      // Only the real signed-out guard's exact 401 is expected, never route errors.
      if (bootstrapAuthRefusals > 0 && bootstrapProbe(message.location().url, true)
        && /\b401\b/.test(message.text())) return;
      health.consoleErrors++;
    });
    page.on('pageerror', () => health.pageErrors++);
    page.on('request', (request) => {
      if (['fetch', 'xhr'].includes(request.resourceType())) pending.add(request);
      health.pendingReads = pending.size;
    });
    page.on('requestfinished', (request) => {
      pending.delete(request); health.pendingReads = pending.size;
    });
    page.on('requestfailed', (request) => {
      pending.delete(request); health.pendingReads = pending.size;
      health.failedRequests++;
    });
    page.on('response', (response) => {
      if (response.status() === 401 && bootstrapProbe(response.url())) bootstrapAuthRefusals++;
      else if (response.status() >= 400) health.httpErrors++;
    });
    await context.route('**/*', async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin !== target.origin) {
        health.deniedExternal++;
        await route.abort('blockedbyclient');
      } else if (['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
        await route.continue();
      } else if (loginAvailable && request.method() === 'POST' && url.pathname === '/api/v1/auth/login') {
        const body = request.postDataJSON();
        if (body?.email !== target.email || body?.password !== target.password) {
          health.deniedWrites++;
          await route.abort('blockedbyclient');
          return;
        }
        loginAvailable = false;
        await route.continue();
      } else {
        health.deniedWrites++;
        await route.abort('blockedbyclient');
      }
    });
    const matrix: OwnedBrowserMatrix = {
      page, target,
      settle: async () => {
        await expect.poll(() => health.pendingReads, { timeout: 15_000, intervals: [100, 250, 500] }).toBe(0);
        // Allow the actual renderer to commit the final response, without networkidle or sleeps.
        await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
      },
      assertHealthy: () => {
        expect(health, 'Counts only: console text, bodies, headers and credentials are deliberately excluded').toEqual({
          consoleErrors: 0, pageErrors: 0, failedRequests: 0, httpErrors: 0,
          deniedWrites: 0, deniedExternal: 0, pendingReads: 0,
        });
      },
      capture: async (name) => {
        if (!/^[a-z0-9-]+$/.test(name)) throw new Error('Invalid matrix capture name.');
        // Explicit post-login captures only; automatic login/failure recording stays off.
        await page.screenshot({
          path: info.outputPath(`${name}.png`), fullPage: false,
          mask: [page.locator('input[type="password"], input[name*="token" i], [data-hai-secret]')],
        });
      },
    };
    try {
      await use(matrix);
    } finally {
      await info.attach('matrix-health-counts', {
        body: Buffer.from(JSON.stringify({ ...health, bootstrapAuthRefusals })), contentType: 'application/json',
      });
      await context.close();
    }
  },
});

export async function signIn(matrix: OwnedBrowserMatrix): Promise<void> {
  const { page, target } = matrix;
  await page.goto('/login');
  await page.getByTestId('login-email').fill(target.email);
  // Keep the credential out of a failed fill() call's Playwright log.
  await page.getByTestId('login-password').evaluate((element, password) => {
    const input = element as HTMLInputElement;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, password);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }, target.password);
  await page.getByTestId('login-submit').click();
  await expect(page).toHaveURL(/\/(?:control-center|onboarding)(?:\?|$)/);
  if (new URL(page.url()).pathname === '/onboarding') {
    await page.getByRole('button', { name: 'Skip', exact: true }).click();
  }
  await expect(page).toHaveURL(/\/control-center(?:\?|$)/);
  await matrix.settle();
  matrix.assertHealthy();
}

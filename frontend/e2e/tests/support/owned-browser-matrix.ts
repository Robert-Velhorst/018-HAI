import { expect, test as base, Page, TestInfo } from '@playwright/test';
import { readFileSync, realpathSync, statSync } from 'node:fs';
import path from 'node:path';
import { assertIsolatedAcceptanceTarget } from './isolated-stack';

export interface OwnedMatrixTarget {
  origin: string;
  storageStatePath: string;
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
    throw new Error('Matrix requires E2E_ALLOW_MUTATION=true for one isolated synthetic session setup.');
  }
  if (!password) throw new Error('Matrix requires the disposable owner password.');
  const storageStatePath = process.env.HAI_E2E_AUTH_STATE_PATH || '';
  if (!path.isAbsolute(storageStatePath) || !statExists(storageStatePath)) {
    throw new Error('Matrix requires the isolated run-scoped synthetic owner session.');
  }
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
      || !inside(root, resolvedDestination(storageStatePath))
      || !inside(root, resolvedDestination(info.project.outputDir))
      || !inside(root, resolvedDestination(html))) throw new Error('destination');
  } catch {
    // Never expose private manifest values or filesystem/parser diagnostics.
    throw new Error('Matrix evidence ownership or artifact destination validation failed.');
  }
  return { origin: new URL(origin).origin, storageStatePath };
}

interface MatrixHealth {
  consoleErrors: number;
  consoleErrorClasses: Record<string, number>;
  consoleErrorKinds: Record<string, number>;
  consoleErrorLocations: Record<string, number>;
  consoleErrorBundles: Record<string, number>;
  pageErrors: number;
  failedRequests: number;
  httpErrors: number;
  deniedWrites: number;
  deniedExternal: number;
}

function consoleErrorKind(text: string): string {
  if (/\b401\b|unauthori[sz]ed/i.test(text)) return 'authorization';
  if (/Failed to load resource|net::ERR_|ERR_CONNECTION|ERR_NAME_NOT_RESOLVED/i.test(text)) return 'resource-load';
  if (/ChunkLoadError|Loading chunk|dynamic import/i.test(text)) return 'module-load';
  if (/\bNG\d{3,}\b|Angular/i.test(text)) return 'framework';
  if (/aria-|focus|accessibility/i.test(text)) return 'accessibility';
  return 'other';
}

function consoleErrorClass(text: string): string {
  return text.match(/\b(TypeError|ReferenceError|RangeError|SyntaxError|ChunkLoadError|Error)\b/)?.[1] || 'unknown';
}

export interface OwnedBrowserMatrix {
  page: Page;
  target: OwnedMatrixTarget;
  ensureAuthenticated(): Promise<void>;
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
      baseURL: target.origin, storageState: target.storageStatePath,
      viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce',
      serviceWorkers: 'block', acceptDownloads: false,
    });
    const page = await context.newPage();
    page.setDefaultTimeout(10_000);
    page.setDefaultNavigationTimeout(15_000);
    const health: MatrixHealth = {
      consoleErrors: 0, consoleErrorClasses: {}, consoleErrorKinds: {}, consoleErrorLocations: {}, consoleErrorBundles: {},
      pageErrors: 0, failedRequests: 0, httpErrors: 0,
      deniedWrites: 0, deniedExternal: 0,
    };
    let bootstrapAuthRefusals = 0;
    const bootstrapProbe = (url: string): boolean => {
      try {
        const parsed = new URL(url);
        return parsed.origin === target.origin
          && parsed.pathname === '/api/v1/auth/is-user-authenticated';
      } catch { return false; }
    };
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      // Only the real signed-out guard's exact 401 is expected, never route errors.
      if (bootstrapAuthRefusals > 0 && bootstrapProbe(message.location().url)
        && /\b401\b/.test(message.text())) return;
      health.consoleErrors++;
      const errorClass = consoleErrorClass(message.text());
      health.consoleErrorClasses[errorClass] = (health.consoleErrorClasses[errorClass] || 0) + 1;
      const kind = consoleErrorKind(message.text());
      health.consoleErrorKinds[kind] = (health.consoleErrorKinds[kind] || 0) + 1;
      let bundle = 'inline';
      try {
        const location = new URL(message.location().url);
        if (location.origin === target.origin) {
          bundle = location.pathname.split('/').filter(Boolean).pop() || 'document';
        } else if (location.protocol !== 'data:' && location.protocol !== 'blob:') {
          bundle = 'external-or-framework';
        }
      } catch { /* Keep only the source category when the location is not a URL. */ }
      const location = message.location();
      const sourceLocation = `${bundle}:${Math.max(0, location.lineNumber)}:${Math.max(0, location.columnNumber)}`;
      health.consoleErrorLocations[sourceLocation] = (health.consoleErrorLocations[sourceLocation] || 0) + 1;
      health.consoleErrorBundles[bundle] = (health.consoleErrorBundles[bundle] || 0) + 1;
    });
    page.on('pageerror', () => health.pageErrors++);
    page.on('requestfailed', (request) => {
      // SPA route changes can intentionally cancel a request that is no longer
      // relevant. Do not report Chromium's navigation-abort signal as a broken
      // network request; genuine transport failures still fail the audit.
      if (request.failure()?.errorText === 'net::ERR_ABORTED') return;
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
      } else {
        health.deniedWrites++;
        await route.abort('blockedbyclient');
      }
    });
    const matrix: OwnedBrowserMatrix = {
      page, target,
      ensureAuthenticated: async () => {
        // Use the same owner-session endpoint as the application. It refreshes
        // expiring access cookies through the existing refresh-token flow, so
        // a long serial browser matrix does not repeatedly log in or hit the
        // IDP's login rate limit.
        const session = await context.request.get(`${target.origin}/api/v1/auth/session`, {
          timeout: 10_000,
        });
        if (!session.ok()) {
          throw new Error(`Synthetic owner session check failed (HTTP ${session.status()}).`);
        }
        const state = await session.json().catch(() => null) as { authenticated?: boolean } | null;
        if (state?.authenticated !== true) {
          throw new Error('Synthetic owner session is no longer authenticated; refusing another password login.');
        }
        // Refresh-token rotation must be carried into the next serial test context.
        await context.storageState({ path: target.storageStatePath });
      },
      settle: async () => {
        // Readiness is asserted by each route's observable UI contract; global
        // network-idle checks are invalid because HAI refreshes in the background.
        await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
      },
      assertHealthy: () => {
        expect(health, 'Counts and same-origin bundle names only; console text, URLs, bodies, headers and credentials are excluded').toEqual({
          consoleErrors: 0, consoleErrorClasses: {}, consoleErrorKinds: {}, consoleErrorLocations: {}, consoleErrorBundles: {},
          pageErrors: 0, failedRequests: 0, httpErrors: 0,
          deniedWrites: 0, deniedExternal: 0,
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
  const { page } = matrix;
  await matrix.ensureAuthenticated();
  await page.goto('/control-center');
  await expect(page).toHaveURL(/\/control-center(?:\?|$)/);
  await expect.poll(() => page.evaluate(() => localStorage.getItem('hai_onboarded')))
    .toBe('true');
  await matrix.settle();
  matrix.assertHealthy();
}

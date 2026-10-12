import { expect, Page, test } from '@playwright/test';
import '@angular/compiler';
import { HAI_MODULES } from '../../src/app/control-room/module-registry';
import { assertIsolatedAcceptanceTarget } from './support/isolated-stack';

const email = process.env.E2E_OPERATOR_EMAIL || '';
const password = process.env.E2E_OPERATOR_PASSWORD || '';
const modules = HAI_MODULES.filter((module) => module.id !== 'onboarding');

async function signIn(page: Page): Promise<void> {
  await page.goto('/login');
  await page.getByTestId('login-email').fill(email);
  await page.getByTestId('login-password').fill(password);
  await page.getByTestId('login-submit').click();
  await expect(page).toHaveURL(/\/(?:control-center|onboarding)(?:\?|$)/);
  if (new URL(page.url()).pathname === '/onboarding') {
    await page.getByRole('button', { name: 'Skip', exact: true }).click();
  }
  await expect(page).toHaveURL(/\/control-center(?:\?|$)/);
}

test.describe('progressive control room', () => {
  test.skip(!password, 'Requires a synthetic owner in an explicitly isolated acceptance stack.');
  test.beforeAll(async ({ baseURL }) => {
    assertIsolatedAcceptanceTarget(baseURL, process.env.E2E_ISOLATED_STACK, email);
  });

  for (const cancelWithKeyboard of [false, true]) {
    test(`late disclosure ${cancelWithKeyboard ? 'respects intervening keyboard input' : 'receives deep-link focus'}`, async ({ page }) => {
      await signIn(page);
      let release = () => {};
      const held = new Promise<void>((resolve) => { release = resolve; });
      const unexpectedWrites: string[] = [];
      await page.route('**/api/**', async (route) => {
        if (!['GET', 'HEAD', 'OPTIONS'].includes(route.request().method())) {
          unexpectedWrites.push(route.request().method());
          await route.abort('blockedbyclient');
          return;
        }
        if (new URL(route.request().url()).pathname === '/api/v1/os/overview') await held;
        await route.continue();
      });
      try {
        await page.goto('/hai-os#system-metrics');
        const disclosure = page.locator('[data-hai-section="system-metrics"] .hai-progressive-section__summary');
        await expect(page.locator('.os-state--loading')).toBeVisible();
        await expect(disclosure).toHaveCount(0);
        const mode = page.getByRole('button', { name: 'Advanced view for HAI OS', exact: true });
        if (cancelWithKeyboard) {
          await mode.focus();
          await page.keyboard.press('ArrowRight');
          await expect(mode).toBeFocused();
        }
        release();
        await expect(disclosure).toBeVisible();
        await expect(disclosure).toHaveAttribute('aria-expanded', 'true');
        if (cancelWithKeyboard) await expect(mode).toBeFocused();
        else await expect(disclosure).toBeFocused();
        expect(unexpectedWrites).toEqual([]);
      } finally {
        release();
        await page.unrouteAll({ behavior: 'wait' });
      }
    });
  }

  for (const width of [375, 768, 1024, 1440]) {
    test(`all modules render in Basic view without overflow at ${width}px`, async ({ page }) => {
      test.setTimeout(300_000);
      await page.setViewportSize({ width, height: 900 });
      await signIn(page);
      expect(await page.evaluate(() => window.innerWidth), 'Use the actual CSS viewport, not a scaled host-window size').toBe(width);
      const unexpectedWrites: string[] = [];
      // Once signed in, route checks must never start work or change server data.
      await page.route('**/api/**', async (route) => {
        const request = route.request();
        if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
          unexpectedWrites.push(`${request.method()} ${new URL(request.url()).pathname}`);
          await route.abort('blockedbyclient');
        } else {
          await route.continue();
        }
      });
      const runtimeErrors: string[] = [];
      page.on('pageerror', (error) => runtimeErrors.push(error.message));

      for (const module of modules) {
        await test.step(module.title, async () => {
          await page.goto(module.route);
          const main = page.locator(`main[data-hai-module="${module.id}"]`);
          await expect(main).toBeVisible();
          await expect(page.getByRole('main')).toHaveCount(1);
          await expect(main.getByRole('heading', { level: 1 }).first()).toBeVisible();
          if (module.id === 'control-center') {
            await expect(page.getByTestId('page-refresh')).toBeEnabled();
            await expect(page.getByTestId('next-action-primary')).not.toHaveText('Checking');
            if (await main.locator('.next-action[data-kind="blocked"]').count()) {
              await expect(page.getByTestId('next-action-primary')).toBeEnabled();
            }
          }
          if (module.id === 'hai-os') {
            await expect(page.getByRole('button', { name: 'Refresh HAI OS overview', exact: true })).toBeEnabled();
            await expect(main.locator('.os-state--loading')).toHaveCount(0);
            await expect(main.locator('.os-overview-summary')).toBeVisible();
          }
          if (['memory', 'connected-sources'].includes(module.id)) {
            const toolbarButtons = main.locator(':is(.memory-header, .sources-header) .header-actions > button');
            expect(await toolbarButtons.count()).toBeGreaterThan(0);
            const clippedLabels = await toolbarButtons.evaluateAll((buttons) => buttons.flatMap((button) => {
              const bounds = button.getBoundingClientRect();
              return Array.from(button.children).filter((child) => {
                if (!(child instanceof HTMLElement) || !child.innerText.trim()) return false;
                const label = child.getBoundingClientRect();
                return label.left < bounds.left - 1 || label.right > bounds.right + 1
                  || label.top < bounds.top - 1 || label.bottom > bounds.bottom + 1;
              }).map((child) => `${button.textContent?.trim()}: ${child.textContent?.trim()}`);
            }));
            expect(clippedLabels, `${module.route} toolbar labels must stay inside their buttons`).toEqual([]);
          }
          await expect(page.getByRole('button', { name: `Advanced view for ${module.title}`, exact: true }))
            .toHaveAttribute('aria-pressed', 'false');
          expect(await page.evaluate(() => document.documentElement.scrollWidth
            > document.documentElement.clientWidth + 1), `${module.route} overflows`).toBe(false);
          expect(await page.locator('body').getAttribute('class')).toContain('hai-theme-dark');
          expect(runtimeErrors, module.route).toEqual([]);
          expect(unexpectedWrites, module.route).toEqual([]);
          if ([375, 1440].includes(width)
            && ['control-center', 'automations', 'pursuits', 'workflow-engine', 'hai-os', 'memory', 'connected-sources'].includes(module.id)) {
            await page.screenshot({ path: test.info().outputPath(`${module.id}-${width}px.png`), fullPage: true });
          }
        });
      }
    });
  }

  test('view depth persists independently, direct sections open, and skip navigation preserves the route', async ({ page }) => {
    await signIn(page);
    await page.goto('/workflow-engine');
    const workflowMode = page.getByRole('button', { name: 'Advanced view for Workflows', exact: true });
    await workflowMode.click();
    await expect(workflowMode).toHaveAttribute('aria-pressed', 'true');
    await page.goto('/hai-os');
    await expect(page.getByRole('button', { name: 'Advanced view for HAI OS', exact: true }))
      .toHaveAttribute('aria-pressed', 'false');
    await page.goto('/workflow-engine');
    await expect(workflowMode).toHaveAttribute('aria-pressed', 'true');
    await page.reload();
    await expect(workflowMode).toHaveAttribute('aria-pressed', 'true');

    const skip = page.getByRole('link', { name: 'Skip to content', exact: true });
    await skip.focus();
    await skip.press('Enter');
    await expect(page).toHaveURL(/\/workflow-engine(?:[?#]|$)/);
    await expect(page.locator('#hai-main')).toBeFocused();

    await page.getByRole('button', { name: 'Reset view for Workflows', exact: true }).click();
    await expect(workflowMode).toHaveAttribute('aria-pressed', 'false');
    await page.goto('/hai-os#product-stack');
    await expect(page.getByRole('button', { name: 'Advanced view for HAI OS', exact: true }))
      .toHaveAttribute('aria-pressed', 'true');
    const productStack = page.locator('#product-stack');
    await expect(productStack).toBeVisible();
    await expect(productStack.getByRole('button').first()).toHaveAttribute('aria-expanded', 'true');
  });

  test('mobile navigation focuses the rendered dialog and restores focus after Escape', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 900 });
    await signIn(page);
    const menu = page.getByRole('button', { name: 'Open navigation', exact: true });
    await menu.click();
    const navigation = page.getByRole('dialog', { name: 'HAI navigation', exact: true });
    await expect(navigation).toBeVisible();
    const activeLink = navigation.getByRole('link', { name: 'Command Center', exact: true });
    await expect(activeLink).toBeFocused();
    await expect(page.locator('.hai-workspace')).toHaveAttribute('inert', '');
    await activeLink.press('Escape');
    await expect(navigation).toHaveCount(0);
    await expect(menu).toBeFocused();
    await expect(page.locator('.hai-workspace')).not.toHaveAttribute('inert', '');
  });

  test('a persisted blocker opens its exact workflow without starting another operation', async ({ page }) => {
    await signIn(page);
    const response = await page.request.get('/api/v1/workflow/dashboard');
    expect(response.ok()).toBeTruthy();
    const dashboard = await response.json() as { approvalItems?: unknown[]; blockedItems?: Array<{ id: string }> };
    test.skip(!dashboard.blockedItems?.length || !!dashboard.approvalItems?.length,
      'Requires a persisted blocker and no higher-priority approval in this synthetic owner.');
    const expectedId = dashboard.blockedItems![0].id;
    const writes: string[] = [];
    await page.route('**/api/**', async (route) => {
      if (!['GET', 'HEAD', 'OPTIONS'].includes(route.request().method())) {
        writes.push(`${route.request().method()} ${new URL(route.request().url()).pathname}`);
        await route.abort('blockedbyclient');
      } else {
        await route.continue();
      }
    });
    await page.goto('/control-center');
    await expect(page.locator('.next-action[data-kind="blocked"]')).toBeVisible();
    const inspect = page.getByTestId('next-action-primary');
    await expect(inspect).toBeEnabled();
    await expect(inspect).toHaveText('Inspect blocker');
    await inspect.click();
    await expect(page).toHaveURL((url) => url.pathname === '/workflow-engine'
      && url.searchParams.get('workflowId') === expectedId);
    await expect(page.getByTestId('workflow-selected-state')).toHaveText('blocked');
    expect(writes).toEqual([]);
  });

  test('an unavailable dashboard request exits loading without another user interaction', async ({ page }) => {
    await signIn(page);
    await page.route('**/api/v1/workflow/dashboard', (route) => route.abort('failed'));
    await page.reload();
    await expect(page.getByTestId('dashboard-unavailable')).toBeVisible();
    await expect(page.getByTestId('page-refresh')).toBeEnabled();
    await expect(page.getByTestId('source-status-workflow')).toHaveAttribute('data-state', 'unavailable');
    const retryQueue = page.getByTestId('next-action-primary');
    await expect(retryQueue).toBeEnabled();
    await expect(retryQueue).toHaveText('Retry queue check');
    await page.unroute('**/api/v1/workflow/dashboard');
    await retryQueue.click();
    await expect(page.getByTestId('dashboard-unavailable')).toHaveCount(0);
    await expect(page.getByTestId('source-status-workflow')).toHaveAttribute('data-state', 'loaded');
  });
});

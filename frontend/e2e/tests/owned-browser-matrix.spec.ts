import { expect, Locator } from '@playwright/test';
import '@angular/compiler';
import { HAI_MODULES, HaiModuleDefinition } from '../../src/app/control-room/module-registry';
import { OwnedBrowserMatrix, signIn, test } from './support/owned-browser-matrix';

const scope = process.env.E2E_MATRIX_SCOPE || 'full';
if (!['first', 'full'].includes(scope)) throw new Error('E2E_MATRIX_SCOPE must be first or full.');
const modules = HAI_MODULES.filter((module) => module.showInNavigation !== false);
const selected = scope === 'first'
  ? modules.filter((module) => ['control-center', 'workflow-engine', 'connected-sources'].includes(module.id))
  : modules;
const widths = scope === 'first' ? [375, 1440] : [375, 768, 1024, 1440];

// The matrix fixture creates the only context; inherited recordings stay disabled.
test.use({ trace: 'off', video: 'off', screenshot: 'off' });
test.describe.configure({ mode: 'default', retries: 0, timeout: 90_000 });

function modeControl(matrix: OwnedBrowserMatrix, module: HaiModuleDefinition): Locator {
  return matrix.page.getByRole('button', { name: `Advanced view for ${module.title}`, exact: true });
}

async function keyboardActivate(control: Locator): Promise<void> {
  await control.focus();
  await expect(control).toBeFocused();
  await control.press('Enter');
}

async function render(matrix: OwnedBrowserMatrix, module: HaiModuleDefinition, width: number): Promise<void> {
  const { page } = matrix;
  await expect(page).toHaveURL((url) => url.origin === matrix.target.origin && url.pathname === module.route);
  const main = page.locator(`main[data-hai-module="${module.id}"]`);
  await expect(page.getByRole('main')).toHaveCount(1);
  await expect(main).toBeVisible();
  await expect(main.getByRole('heading', { level: 1 }).first()).toBeVisible();
  await matrix.settle();
  expect(await main.evaluate((element) => (element.textContent || '').trim().length > 60), 'Meaningful route content, not an empty outlet').toBe(true);
  expect(await page.evaluate(() => window.innerWidth), 'Actual CSS viewport').toBe(width);
  await expect.poll(() => page.evaluate(() =>
    Math.max(document.documentElement.scrollWidth, document.body.scrollWidth)
      <= document.documentElement.clientWidth + 1), { timeout: 5000 }).toBe(true);
  await expect(page.locator('vite-error-overlay, nextjs-portal, .webpack-dev-server-client-overlay')).toHaveCount(0);
  if (module.id === 'control-center') {
    await expect(page.getByTestId('page-refresh')).toBeEnabled();
    await expect(page.getByTestId('next-action-primary')).not.toHaveText('Checking');
  }
  if (module.id === 'workflow-engine') {
    await expect(page.getByTestId('workflow-initial-loading')).toHaveCount(0);
    await expect(page.getByTestId('workflow-load-error')).toHaveCount(0);
  }
  if (module.id === 'hai-os') {
    await expect(main.locator('.os-state--loading')).toHaveCount(0);
    await expect(main.locator('.os-overview-summary')).toBeVisible();
  }
  if (['memory', 'connected-sources'].includes(module.id)) {
    const clipped = await main.locator(':is(.memory-header, .sources-header) .header-actions > button')
      .evaluateAll((buttons) => buttons.filter((button) => {
        const bounds = button.getBoundingClientRect();
        return Array.from(button.children).some((child) => {
          if (!(child instanceof HTMLElement) || !child.innerText.trim()) return false;
          const label = child.getBoundingClientRect();
          return label.left < bounds.left - 1 || label.right > bounds.right + 1
            || label.top < bounds.top - 1 || label.bottom > bounds.bottom + 1;
        });
      }).length);
    expect(clipped, 'Toolbar text must remain inside its button').toBe(0);
  }
  matrix.assertHealthy();
}

async function theme(matrix: OwnedBrowserMatrix, mode: 'dark' | 'light'): Promise<void> {
  const { page } = matrix;
  if (await page.locator('html').getAttribute('data-hai-theme') === mode
    && await page.evaluate(() => localStorage.getItem('hai-theme-mode')) !== mode) {
    await keyboardActivate(page.getByRole('button', { name: `Switch to ${mode === 'dark' ? 'light' : 'dark'} theme`, exact: true }));
  }
  if (await page.locator('html').getAttribute('data-hai-theme') !== mode) {
    await keyboardActivate(page.getByRole('button', { name: `Switch to ${mode} theme`, exact: true }));
  }
  await expect(page.locator('html')).toHaveAttribute('data-hai-theme', mode);
  await expect(page.locator('body')).toHaveClass(new RegExp(`(?:^| )hai-theme-${mode}(?: |$)`));
  expect(await page.evaluate(() => localStorage.getItem('hai-theme-mode'))).toBe(mode);
}

async function otherPreferences(matrix: OwnedBrowserMatrix, id: string): Promise<Record<string, string | null>> {
  return matrix.page.evaluate((current) => Object.fromEntries(Object.keys(localStorage)
    .filter((key) => key.startsWith('hai.module-view.v1.') && key !== `hai.module-view.v1.${current}`)
    .sort().map((key) => [key, localStorage.getItem(key)])), id);
}

async function resetView(matrix: OwnedBrowserMatrix, module: HaiModuleDefinition, width: number): Promise<void> {
  if (width <= 480) {
    await keyboardActivate(matrix.page.locator('.hai-compact-view-menu > summary'));
    await keyboardActivate(matrix.page.getByRole('button', { name: `Reset view for ${module.title}`, exact: true }));
    await expect(matrix.page.locator('.hai-compact-view-menu > summary')).toBeFocused();
  } else {
    await keyboardActivate(matrix.page.getByRole('button', { name: `Reset view for ${module.title}`, exact: true }));
  }
  await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'false');
}

for (const width of widths) {
  for (const module of selected) {
    test(`routes ${module.id} at ${width}px: themes, modes and persistence`, async ({ matrix }) => {
      const { page } = matrix;
      await page.setViewportSize({ width, height: 900 });
      await signIn(matrix);
      await page.goto(module.route);
      const others = await otherPreferences(matrix, module.id);
      for (const palette of ['dark', 'light'] as const) {
        await test.step(`${palette} Basic -> Advanced -> route/reload -> reset`, async () => {
          await theme(matrix, palette);
          await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'false');
          await expect(page.locator('main .hai-progressive-section--advanced:visible, main [data-hai-advanced]:visible')).toHaveCount(0);
          await render(matrix, module, width);
          await matrix.capture(`${module.id}-${width}-${palette}-basic`);
          await keyboardActivate(modeControl(matrix, module));
          await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'true');
          const disclosure = page.locator('main .hai-progressive-section--advanced > button[aria-expanded="false"]:visible').first();
          if (await disclosure.count()) {
            await keyboardActivate(disclosure);
          }
          await render(matrix, module, width);
          await matrix.capture(`${module.id}-${width}-${palette}-advanced`);
          expect(await otherPreferences(matrix, module.id), 'Mode changes are module-scoped').toEqual(others);
          const sentinel = modules.find((candidate) => candidate.id !== module.id && candidate.id === 'control-center')
            || modules.find((candidate) => candidate.id === 'workflow-engine')!;
          await page.goto(sentinel.route);
          await expect(modeControl(matrix, sentinel)).toHaveAttribute('aria-pressed', 'false');
          await matrix.settle();
          await expect(page.locator('html')).toHaveAttribute('data-hai-theme', palette);
          await page.goto(module.route);
          await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'true');
          await matrix.settle();
          await page.reload();
          await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'true');
          await expect(page.locator('html')).toHaveAttribute('data-hai-theme', palette);
          await render(matrix, module, width);
          await resetView(matrix, module, width);
          await matrix.settle();
          await page.reload();
          await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'false');
          await render(matrix, module, width);
          expect(await otherPreferences(matrix, module.id), 'Reset cannot clear other modules').toEqual(others);
        });
      }
    });
  }
}

for (const width of [375, 768]) {
  test(`keyboard drawer at ${width}px: trap, Escape, close button and route focus`, async ({ matrix }) => {
    const { page } = matrix;
    await page.setViewportSize({ width, height: 900 });
    await signIn(matrix);
    const menu = page.getByRole('button', { name: 'Open navigation', exact: true });
    const dialog = page.getByRole('dialog', { name: 'HAI navigation', exact: true });
    await keyboardActivate(menu);
    await expect(dialog.getByRole('link', { name: 'Command Center', exact: true })).toBeFocused();
    await expect(page.locator('.hai-workspace')).toHaveAttribute('inert', '');
    // Walk beyond the bounded number of rendered controls: focus must never escape.
    const controls = dialog.locator('summary:visible, button:visible, a[href]:visible');
    const count = await controls.count();
    expect(count).toBeGreaterThan(1);
    expect(count).toBeLessThan(64);
    for (const key of ['Tab', 'Shift+Tab']) {
      for (let i = 0; i <= count; i++) {
        await page.keyboard.press(key);
        expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
      }
    }
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(menu).toBeFocused();
    await expect(page.locator('.hai-workspace')).not.toHaveAttribute('inert', '');
    await keyboardActivate(menu);
    await keyboardActivate(dialog.getByRole('button', { name: 'Close navigation', exact: true }));
    await expect(dialog).toHaveCount(0);
    await expect(menu).toBeFocused();
    await keyboardActivate(menu);
    await keyboardActivate(dialog.getByRole('link', { name: 'Workflows', exact: true }));
    await expect(dialog).toHaveCount(0);
    await expect(page).toHaveURL(/\/workflow-engine$/);
    await expect(page.locator('#hai-main')).toBeFocused();
    const skip = page.getByRole('link', { name: 'Skip to content', exact: true });
    await keyboardActivate(skip);
    await expect(page.locator('#hai-main')).toBeFocused();
    await expect(page).toHaveURL(/\/workflow-engine(?:#hai-main)?$/);
    await matrix.settle();
    matrix.assertHealthy();
    await matrix.capture(`keyboard-${width}`);
  });
}

function requiredRecord(variable: string): string {
  const id = process.env[variable] || '';
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) {
    throw new Error(`${variable} must identify an existing synthetic owner-scoped UUID record; missing data is not a PASS.`);
  }
  return id;
}

if (scope === 'full') {
  for (const width of widths) {
    test(`critical workflow uncertainty stays visible in Basic at ${width}px`, async ({ matrix }) => {
      const id = requiredRecord('E2E_MATRIX_UNCERTAIN_WORKFLOW_ID');
      const { page } = matrix;
      await page.setViewportSize({ width, height: 900 });
      await signIn(matrix);
      const response = await page.request.get(`/api/v1/workflow/${id}`, { timeout: 10_000, maxRedirects: 0 });
      expect(response.status(), 'Real persisted owner-scoped workflow lookup').toBe(200);
      const record = await response.json();
      expect(record.item?.id === id && record.item?.recoveryStatus === 'needs_review'
        && record.item?.currentState === 'blocked', 'Parent fixture must be persisted, blocked and require outcome review').toBe(true);
      await page.goto(`/workflow-engine?workflowId=${id}`);
      const module = modules.find((candidate) => candidate.id === 'workflow-engine')!;
      await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'false');
      const alert = page.getByTestId('workflow-interruption-review');
      await expect(alert).toBeVisible();
      await expect(alert).toHaveAttribute('role', 'alert');
      await expect(alert).toContainText('Unknown effects');
      await expect(page.getByTestId('workflow-run-selected')).toHaveCount(0);
      await render(matrix, module, width);
      await matrix.capture(`uncertain-workflow-${width}`);
      await keyboardActivate(modeControl(matrix, module));
      await expect(alert).toBeVisible();
      await keyboardActivate(modeControl(matrix, module));
      await page.reload();
      await expect(alert).toBeVisible();
      await matrix.settle();
      matrix.assertHealthy();
    });

    test(`critical persisted task receipt must survive history inspection in Basic at ${width}px`, async ({ matrix }) => {
      const id = requiredRecord('E2E_MATRIX_UNCERTAIN_TASK_ID');
      const { page } = matrix;
      await page.setViewportSize({ width, height: 900 });
      await signIn(matrix);
      const response = await page.request.get('/api/v1/task/logs', { timeout: 10_000, maxRedirects: 0 });
      expect(response.status(), 'Real persisted owner-scoped task history').toBe(200);
      const logs = await response.json();
      const record = Array.isArray(logs) ? logs.find((log) => log.id === id) : undefined;
      expect(Boolean(record && /^E2E matrix [a-z0-9 -]+$/i.test(record.request)
        && record.executionResult?.outcomeUncertain === true
        && record.retryPolicy?.retryAvailable === false), 'Requires a synthetic restricted receipt, not a fabricated browser response').toBe(true);
      await page.goto('/task-blueprint');
      const module = modules.find((candidate) => candidate.id === 'task-blueprint')!;
      await keyboardActivate(modeControl(matrix, module));
      await keyboardActivate(page.locator('#task-inspector > button'));
      await keyboardActivate(page.getByRole('button', { name: 'Logs', exact: true }));
      const row = page.locator('button.log-row').filter({ has: page.getByText(record.request, { exact: true }) });
      await expect(row).toHaveCount(1);
      await keyboardActivate(row);
      await keyboardActivate(modeControl(matrix, module));
      await expect(modeControl(matrix, module)).toHaveAttribute('aria-pressed', 'false');
      // Do not inject component state: this deliberately exposes a missing real inspection path.
      const alert = page.getByTestId('task-execution-safety');
      await expect(alert).toBeVisible();
      await expect(alert).toHaveAttribute('role', 'alert');
      await expect(alert).toContainText('Completion is not confirmed');
      await expect(page.getByTestId('task-chat-run')).toBeDisabled();
      await expect(page.getByTestId('task-chat-cycle')).toBeDisabled();
      await render(matrix, module, width);
      await matrix.capture(`uncertain-task-${width}`);
    });
  }
}

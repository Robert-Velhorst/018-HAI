import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { assertIsolatedAcceptanceTarget } from './support/isolated-stack';

const email = process.env.E2E_OPERATOR_EMAIL || 'operator@example.com';
const password = process.env.E2E_OPERATOR_PASSWORD || '';

test.describe('Quick capture on the owned acceptance stack', () => {
  test.skip(!password || process.env.E2E_ALLOW_MUTATION !== 'true',
    'Requires the synthetic operator and explicitly enabled isolated-stack mutations.');

  test.beforeAll(({ baseURL }) => {
    assertIsolatedAcceptanceTarget(baseURL, process.env.E2E_ISOLATED_STACK, email);
  });

  test('restores its draft and persists one note before clearing it', async ({ page }) => {
    const title = `E2E quick capture ${randomUUID()}`;
    const content = `${title}. Synthetic acceptance note; no external action is requested.`;

    await page.goto('/login');
    await page.getByTestId('login-email').fill(email);
    await page.getByTestId('login-password').fill(password);
    await page.getByTestId('login-submit').click();
    await expect(page).toHaveURL(/\/(?:control-center|onboarding)(?:\?|$)/);
    if (new URL(page.url()).pathname === '/onboarding') {
      await page.getByRole('button', { name: 'Skip', exact: true }).click();
    }
    await expect(page).toHaveURL(/\/control-center(?:\?|$)/);

    await page.goto('/quick-capture');
    const titleInput = page.locator('#qc-title-input');
    const contentInput = page.locator('#qc-content-input');
    await titleInput.fill(title);
    await contentInput.fill(content);
    await expect(page.locator('.quick-capture__status')).toContainText('Draft autosaved');

    await page.reload();
    await expect(titleInput).toHaveValue(title);
    await expect(contentInput).toHaveValue(content);

    const origin = new URL(page.url()).origin;
    const [response] = await Promise.all([
      page.waitForResponse(response => {
        const url = new URL(response.url());
        return url.origin === origin && url.pathname === '/api/v1/memory/'
          && response.request().method() === 'POST';
      }),
      page.getByRole('button', { name: 'Save to memory', exact: true }).click(),
    ]);
    expect(response.status()).toBe(201);
    const saved = await response.json() as { id: string };
    expect(saved.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
    await expect(page.locator('.quick-capture__ok')).toHaveText('Saved.');
    await expect(titleInput).toHaveValue('');
    await expect(contentInput).toHaveValue('');

    const persisted = await page.request.get(`/api/v1/memory/${encodeURIComponent(saved.id)}`);
    expect(persisted.ok()).toBeTruthy();
    expect(await persisted.json()).toMatchObject({
      id: saved.id, kind: 'note', summary: title, content, tags: ['quick-capture'],
    });
    await page.reload();
    await expect(titleInput).toHaveValue('');
    await expect(contentInput).toHaveValue('');
    // The owned-stack launcher removes its whole synthetic dataset after the run.
    // Never replay the old script's DELETE against installation records.
  });
});

import { expect, Page, test } from '@playwright/test';
import { HAI_MODULES, moduleDocumentTitle } from '../../src/app/control-room/module-registry';

const expectedPreviewStatus = new Set([200, 204]);

async function openLocalPreview(page: Page): Promise<void> {
  await page.goto('/login');
  const response = await page.request.post('/api/v1/auth/local-preview', { data: {} });
  if (!expectedPreviewStatus.has(response.status())) {
    throw new Error(`local preview session failed with HTTP ${response.status()}: ${await response.text()}`);
  }
  await page.goto('/control-center');
  await expect(page.locator('#hai-main')).toHaveAttribute('data-hai-module', 'control-center');
}

test.describe('authenticated module route audit', () => {
  test('every registered module renders inside the shared shell', async ({ page }) => {
    const pageErrors: string[] = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));

    await openLocalPreview(page);

    for (const module of HAI_MODULES) {
      await test.step(`${module.title} (${module.route})`, async () => {
        const navigation = page.locator('.hai-navigation__item', { hasText: module.title });
        await expect(navigation).toBeVisible();
        await navigation.click();
        await expect(page).toHaveURL(new RegExp(`${module.route.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?:[/?#]|$)`));
        await expect(page).toHaveTitle(moduleDocumentTitle(module));
        await expect(page.locator('#hai-main')).toHaveAttribute('data-hai-module', module.id);
        await expect(page.locator('#hai-main')).toBeVisible();
        await expect(page.locator('.hai-module-identity strong')).toHaveText(module.title);
        await expect(page.locator('app-login')).toHaveCount(0);
      });
    }

    expect(pageErrors, `uncaught browser errors:\n${pageErrors.join('\n')}`).toEqual([]);
  });
});

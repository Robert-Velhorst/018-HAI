import { expect } from '@playwright/test';
import { HAI_MODULES, moduleDocumentTitle } from '../../src/app/control-room/module-registry';
import { signIn, test } from './support/owned-browser-matrix';

test.describe('authenticated module route audit', () => {
  test('every registered module renders inside the shared shell', async ({ matrix }) => {
    const { page } = matrix;
    const pageErrors: string[] = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));

    await signIn(matrix);

    for (const module of HAI_MODULES) {
      await test.step(`${module.title} (${module.route})`, async () => {
        if (module.showInNavigation === false) {
          await page.goto(module.route);
        } else {
          const group = page.locator(`details[data-hai-nav-group="${module.group}"]`);
          const navigation = page.locator('.hai-navigation__item', { hasText: module.title });
          if (!(await navigation.isVisible())) {
            const isOpen = await group.evaluate((element) => (element as HTMLDetailsElement).open);
            if (!isOpen) await group.locator(':scope > summary').click();
          }
          await expect(navigation).toBeVisible();
          await navigation.click();
        }
        await expect(page).toHaveURL(new RegExp(`${module.route.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?:[/?#]|$)`));
        await expect(page).toHaveTitle(moduleDocumentTitle(module));
        await expect(page.locator('#hai-main')).toHaveAttribute('data-hai-module', module.id);
        await expect(page.locator('#hai-main')).toBeVisible();
        await expect(page.locator('.hai-module-identity strong')).toHaveText(module.title);
        await expect(page.locator('app-login')).toHaveCount(0);
      });
    }

    expect(pageErrors, `uncaught browser errors:\n${pageErrors.join('\n')}`).toEqual([]);
    await matrix.settle();
    matrix.assertHealthy();
  });
});

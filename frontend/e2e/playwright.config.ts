import { defineConfig, devices } from '@playwright/test';

// The acceptance suite mutates a running HAI stack temporarily. It must never
// silently target a default environment; the operator supplies the exact URL.
const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:0';

export default defineConfig({
  testDir: './tests',
  // These tests exercise a real stack, so keep them serial and generous.
  fullyParallel: false,
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 10_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
});

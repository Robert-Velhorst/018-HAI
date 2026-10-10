import { defineConfig, devices } from '@playwright/test';

// Base URL of a running HAI stack (docker compose up). Override with E2E_BASE_URL.
const baseURL = process.env.E2E_BASE_URL || 'http://localhost';

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.ts',
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

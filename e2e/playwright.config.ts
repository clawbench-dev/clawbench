import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E test configuration for ClawBench.
 *
 * Architecture: Real Go backend + ACP mock agent (no real AI CLI).
 * The server is managed by globalSetup/globalTeardown in helpers/server.ts.
 *
 * Three browser projects:
 * - chromium-coverage: Chromium with V8 coverage collection
 * - firefox: Firefox (functionality only, no coverage)
 * - webkit: WebKit/Safari (functionality only, no coverage)
 */
export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : 1, // Always 1 worker: ACP mock agent requires serial execution
  reporter: process.env.CI
    ? [['html', { open: 'never' }], ['github']]
    : [['html', { open: 'on-failure' }], ['list']],
  timeout: 30000,
  expect: { timeout: 10000 },

  use: {
    baseURL: `http://localhost:${process.env.E2E_PORT || 20100}`,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    // Coverage project: Chromium only, with V8 coverage collection
    {
      name: 'chromium-coverage',
      use: {
        ...devices['Desktop Chrome'],
        // coverage.fixture.ts checks project name to enable collection
      },
    },
    // Cross-browser: no coverage, functionality only
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
  ],

  // Server lifecycle managed by globalSetup/globalTeardown
  globalSetup: './helpers/global-setup.ts',
  globalTeardown: './helpers/global-teardown.ts',
})

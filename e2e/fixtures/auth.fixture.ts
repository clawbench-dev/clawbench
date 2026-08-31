import { test as base, expect } from '@playwright/test'

const E2E_PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'
const E2E_PORT = process.env.E2E_PORT || '20100'

/**
 * Auth fixture: automatically logs in before each test.
 *
 * Strategy:
 * 1. Navigate to the app root
 * 2. If login page is shown (401 or redirect to /login), fill password and submit
 * 3. If already authenticated (session cookie present), skip login
 * 4. Wait for the project cookie to be set (frontend calls /api/project on load)
 *
 * The session cookie (clawbench_session) persists across navigations within
 * the same browser context, so subsequent tests reuse the same session.
 *
 * NOTE: In current E2E setup, the Go server's auth middleware automatically
 * bypasses authentication for localhost requests. Therefore `needsLogin` is
 * always false and the login code path (lines 29-42) is unreachable. This
 * code is kept for future use when localhost bypass may be disabled for
 * proper auth E2E testing. See auth.spec.ts for details.
 *
 * CRITICAL: Many API endpoints (sessions, chat, tasks, files) require the
 * project cookie (clawbench_project) to be set, otherwise they return 403.
 * The frontend sets this cookie via a call to /api/project on initial load,
 * but there's a race condition — API calls can fire before the project cookie
 * is established. This fixture waits for the project to be ready before
 * yielding the page to the test.
 */
export const test = base.extend({
  page: async ({ page }, use) => {
    // Dismiss the first-run Welcome overlay BEFORE the page loads.
    // Every test gets a fresh browser context (empty localStorage), so without
    // this flag WelcomeOverlay.show() renders the full-screen overlay and
    // intercepts pointer events on the send button etc. (the overlay is a
    // first-run UX; real users dismiss it via "Don't show again").
    // addInitScript runs before each navigation, covering page.reload() too.
    await page.addInitScript(() => {
      localStorage.setItem('clawbench_welcome_dismissed', 'true')
    })

    // Navigate to the app root
    const response = await page.goto('/')

    // Check if we need to log in
    // The server returns 401 for unauthenticated requests, or the SPA
    // shows the LoginView component when not authenticated
    const needsLogin =
      response?.status() === 401 ||
      page.url().includes('/login') ||
      await page.locator('.login-page').isVisible().catch(() => false)

    if (needsLogin) {
      // Make sure we're on the login page
      if (!page.url().includes('/login') && !await page.locator('.login-page').isVisible().catch(() => false)) {
        await page.goto('/')
        await page.waitForLoadState('networkidle')
      }

      // Fill password and submit
      await page.locator('.login-page input[type="password"]').fill(E2E_PASSWORD)
      await page.locator('.login-btn').click()

      // Wait for the main app to load (login page should disappear)
      await expect(page.locator('.login-page')).not.toBeVisible({ timeout: 10000 })
    }

    // Wait for the project to be ready.
    // The frontend calls /api/project on load which sets the clawbench_project
    // cookie. Many API endpoints return 403 without this cookie, causing
    // flaky failures in Firefox/WebKit where API calls race ahead of project init.
    // We poll /api/ai/sessions (a project-scoped endpoint) until it returns 200.
    await waitForProjectReady(page)

    await use(page)
  },
})

/**
 * Wait until the project cookie is set by polling a project-scoped API endpoint.
 * Times out after 15 seconds.
 */
async function waitForProjectReady(page: import('@playwright/test').Page): Promise<void> {
  const baseURL = `http://localhost:${E2E_PORT}`
  const timeout = 15_000
  const interval = 500
  const start = Date.now()

  while (Date.now() - start < timeout) {
    try {
      const resp = await page.evaluate(async (url) => {
        const r = await fetch(`${url}/api/ai/sessions`)
        return { status: r.status }
      }, baseURL)
      if (resp.status === 200) return
    } catch {
      // Network error — server might not be fully ready
    }
    await new Promise(r => setTimeout(r, interval))
  }
  // Don't throw — let the test proceed and fail naturally if the project isn't ready.
  // This avoids masking other errors with a confusing "project not ready" message.
}

export { expect }

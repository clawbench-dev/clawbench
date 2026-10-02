import { test, expect } from '../fixtures'
import { SettingsPage } from '../pages/settings.page'
import { apiFetch, resetSessionCookie } from '../helpers/auth'

test.describe('Password Change Dialog', () => {
  let settings: SettingsPage

  const E2E_PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'
  const NEW_PASSWORD = 'new-e2e-password-123456'

  /**
   * Reset the server password to the known E2E_PASSWORD before each test.
   *
   * Changing the password rotates the server's cookie token, which invalidates
   * EVERY existing session — including the browser context the auth fixture
   * just logged in. So after the reset we must log the browser in again,
   * otherwise every test lands on the login page.
   */
  test.beforeEach(async ({ page }) => {
    settings = new SettingsPage(page)

    // Ensure password is in the expected state before each test.
    // Try the expected password FIRST: a wrong attempt is counted by the login
    // rate limiter (blocks after 5 failures), so the common case must not burn
    // one. Only a crashed previous run leaves the password as NEW_PASSWORD.
    for (const current of [E2E_PASSWORD, NEW_PASSWORD]) {
      try {
        const resp = await apiFetch('/api/config/password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ current_password: current, new_password: E2E_PASSWORD }),
        })
        if (resp.ok) break
      } catch {
        // Server may not be ready yet
      }
    }
    // The reset above rotated the cookie token: drop the memoized Node cookie
    // so later apiFetch calls re-login, and force a clean browser login.
    //
    // Clearing cookies (rather than probing for `.login-page`) is deliberate:
    // the SPA can still be showing the stale authenticated shell for a moment
    // after the rotation, so a visibility probe would skip the login and then
    // hang on the chat textarea.
    resetSessionCookie()
    await page.context().clearCookies()
    await page.goto('/')
    // Wait for the SPA to settle on one of its two terminal states before
    // probing: it renders nothing until the async /api/me check resolves.
    await page.locator('.login-page, .app-container').first()
      .waitFor({ state: 'visible', timeout: 20000 }).catch(() => {})
    if (await page.locator('.login-page').isVisible().catch(() => false)) {
      // The password API reset above may not have succeeded (the server could
      // already be on E2E_PASSWORD with a rate-limited limiter, or still on
      // NEW_PASSWORD), so try both known passwords for the browser login too.
      for (const candidate of [E2E_PASSWORD, NEW_PASSWORD]) {
        await page.locator('.login-page input[type="password"]').fill(candidate)
        await page.locator('.login-btn').click()
        const ok = await page.locator('.login-page')
          .waitFor({ state: 'hidden', timeout: 10000 })
          .then(() => true).catch(() => false)
        if (ok) break
      }
    }
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 15000 })

    await settings.openSettings()
  })

  test('should open password change dialog', async () => {
    await settings.openPasswordDialog()

    // Should have 3 password input fields
    await expect(settings.passwordInputs).toHaveCount(3)

    // Submit button should be visible
    await expect(settings.passwordSubmitBtn).toBeVisible()
  })

  test('should show error for wrong current password', async () => {
    await settings.openPasswordDialog()
    await settings.fillPasswordFields('wrong-password', 'newpass123456', 'newpass123456')
    await settings.submitPassword()

    // Error message should be visible
    await expect(settings.passwordError).toBeVisible()
    await expect(settings.passwordError).not.toBeEmpty()
  })

  test('should change password successfully', async ({ page }) => {
    await settings.openPasswordDialog()
    await settings.fillPasswordFields(E2E_PASSWORD, NEW_PASSWORD, NEW_PASSWORD)
    await settings.submitPassword()

    // Dialog should close on success
    await expect(settings.passwordDialog).not.toBeVisible({ timeout: 10000 })

    // Restore the original password so subsequent tests/specs work.
    // Use server-side fetch (avoids rate limiting from browser); the session
    // cookie is attached by apiFetch. This rotation invalidates the current
    // sessions again, so drop the memoized cookie — the next test's beforeEach
    // re-logs in.
    try {
      await apiFetch('/api/config/password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          current_password: NEW_PASSWORD,
          new_password: E2E_PASSWORD,
        }),
      })
    } catch {
      // If restore fails, the beforeEach in the next test will handle it
    }
    resetSessionCookie()
  })

  test('should reject too-short new password', async () => {
    await settings.openPasswordDialog()
    await settings.fillPasswordFields(E2E_PASSWORD, 'ab', 'ab')

    // Submit button should be disabled for short password
    await expect(settings.passwordSubmitBtn).toBeDisabled()
  })
})

import { test, expect } from '../fixtures'
import { SettingsPage } from '../pages/settings.page'
import { ChatPage } from '../pages/chat.page'
import { getServerURL } from '../helpers/server'

test.describe('Password Change Dialog', () => {
  let settings: SettingsPage

  const E2E_PASSWORD = 'e2e-test-password'
  const NEW_PASSWORD = 'new-e2e-password-123456'

  test.beforeEach(async ({ page }) => {
    settings = new SettingsPage(page)
    const chat = new ChatPage(page)
    // Ensure we're on the chat page first for stable navigation
    await chat.textarea.isVisible({ timeout: 10000 }).catch(() => page.goto('/'))
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

    // Restore the original password so subsequent tests/specs work
    await page.evaluate(async ({ currentPass, originalPass }) => {
      const resp = await fetch('/api/config/password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          current_password: currentPass,
          new_password: originalPass,
        }),
      })
      if (!resp.ok) {
        throw new Error(`Failed to restore password: ${resp.status}`)
      }
    }, { currentPass: NEW_PASSWORD, originalPass: E2E_PASSWORD })
  })

  test('should reject too-short new password', async () => {
    await settings.openPasswordDialog()
    await settings.fillPasswordFields(E2E_PASSWORD, 'ab', 'ab')

    // Submit button should be disabled for short password
    await expect(settings.passwordSubmitBtn).toBeDisabled()
  })

  // Safety net: use test.afterAll with Node.js fetch (localhost bypasses auth)
  // to restore the original password if test 3 failed mid-way
  test.afterAll(async () => {
    const baseURL = getServerURL()
    for (const current of [NEW_PASSWORD, E2E_PASSWORD]) {
      try {
        const resp = await fetch(`${baseURL}/api/config/password`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            current_password: current,
            new_password: E2E_PASSWORD,
          }),
        })
        if (resp.ok) break
      } catch {
        // Server may not be available during teardown
      }
    }
  })
})

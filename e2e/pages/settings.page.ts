import { type Locator, type Page, expect } from '@playwright/test'

/**
 * Page Object Model for the Settings panel.
 *
 * Navigation flow:
 * 1. Click dock-overflow-btn → overflow popup with Settings option
 * 2. SettingsIndex page shows category rows (外观, 聊天, 安全, etc.)
 * 3. Click "安全/Security" row → SettingsCategory with items
 * 4. Click "修改密码/Change Password" item → PasswordChangeDialog
 *
 * Key selectors:
 * - .dock-overflow-btn    → bottom dock overflow (more) button
 * - .dock-overflow-item   → overflow popup menu items
 * - .settings-index__row  → category row in settings index
 * - .settings-item        → individual setting item in a category
 * - .password-dialog      → password dialog box
 * - .password-dialog__input → password input fields (3: current, new, confirm)
 * - .password-dialog__btn--submit → submit button
 * - .password-dialog__btn--cancel → cancel button
 * - .password-dialog__error → error message display
 */
export class SettingsPage {
  readonly page: Page
  readonly passwordDialog: Locator
  readonly passwordInputs: Locator
  readonly passwordSubmitBtn: Locator
  readonly passwordCancelBtn: Locator
  readonly passwordError: Locator

  constructor(page: Page) {
    this.page = page
    this.passwordDialog = page.locator('.password-dialog')
    this.passwordInputs = page.locator('.password-dialog__input')
    this.passwordSubmitBtn = page.locator('.password-dialog__btn--submit')
    this.passwordCancelBtn = page.locator('.password-dialog__btn--cancel')
    this.passwordError = page.locator('.password-dialog__error')
  }

  /** Navigate to settings panel via the dock overflow menu */
  async openSettings(): Promise<void> {
    // Click the dock overflow button (three-dots/more menu in bottom dock)
    await this.page.locator('.dock-overflow-btn').click()
    // Click the Settings option in the overflow popup
    const settingsItem = this.page.locator('.dock-overflow-item').filter({ hasText: /settings|设置/i })
    await expect(settingsItem).toBeVisible({ timeout: 5000 })
    await settingsItem.click()
    // Wait for settings page to load
    await expect(this.page.locator('.settings-page')).toBeVisible({ timeout: 5000 })
  }

  /** Open the password change dialog from settings */
  async openPasswordDialog(): Promise<void> {
    // Step 1: Navigate to "安全/Security" category
    const securityRow = this.page.locator('.settings-index__row').filter({ hasText: /security|安全/i })
    await expect(securityRow).toBeVisible({ timeout: 5000 })
    await securityRow.click()

    // Wait for the category page to render its items (auto-waiting, no sleep)
    await expect(this.page.locator('.settings-item').first()).toBeVisible({ timeout: 5000 })

    // Step 2: Click the "修改密码/Change Password" action item
    // The item text includes a description, e.g. "Change Password Change the server access password..."
    // Match specifically on "Change Password" or "修改密码" at the start
    const passwordItem = this.page.locator('.settings-item').filter({
      hasText: /change password|修改密码/i,
    })
    await expect(passwordItem).toBeVisible({ timeout: 5000 })
    await passwordItem.click()

    // Wait for password dialog to appear
    await expect(this.passwordDialog).toBeVisible({ timeout: 5000 })
  }

  /** Fill all three password fields */
  async fillPasswordFields(current: string, newPass: string, confirm: string): Promise<void> {
    const inputs = this.passwordInputs
    await expect(inputs).toHaveCount(3, { timeout: 5000 })
    await inputs.nth(0).fill(current)
    await inputs.nth(1).fill(newPass)
    await inputs.nth(2).fill(confirm)
  }

  /** Submit the password change form */
  async submitPassword(): Promise<void> {
    await this.passwordSubmitBtn.click()
  }

  /** Cancel the password change dialog */
  async cancelPasswordDialog(): Promise<void> {
    await this.passwordCancelBtn.click()
  }
}

import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * The chat attachment sheet's first tab is a real file picker (browse + select
 * + name search), not just "the file I happen to be looking at".
 *
 * Covered here because it is the one flow that spans three components: the
 * sheet (draft + OK), the picker (listing/selection) and the composer strip
 * (what actually got attached). Unit tests cover each in isolation; this pins
 * the wiring.
 */
test.describe('Chat attach — file picker tab', () => {
  let chat: ChatPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    nav = new NavigationPage(page)
    await nav.switchToChat()
  })

  function attachSheet(page: import('@playwright/test').Page) {
    return page.locator('.bs-panel').filter({ has: page.locator('.ad-header') })
  }

  test('picks a project file from the picker and attaches it on OK', async ({ page }) => {
    await page.locator('.chat-attach-btn').click()
    const sheet = attachSheet(page)
    await expect(sheet).toBeVisible({ timeout: 10000 })

    // The picker lists the project root (the e2e project is the repo).
    const fileRows = sheet.locator('[data-role="fp-file"]')
    await expect(fileRows.first()).toBeVisible({ timeout: 10000 })

    const target = fileRows.first()
    const path = await target.getAttribute('data-path')
    expect(path).toBeTruthy()
    await target.click()

    // Selection is a draft: the footer counts it, nothing is attached yet.
    await expect(sheet.locator('[data-role="attach-selected-count"]')).toBeVisible()
    await expect(page.locator('.chat-input-wrapper .chat-file-attachment')).toHaveCount(0)

    await sheet.locator('[data-action="attach-ok"]').click()
    await expect(sheet).toBeHidden({ timeout: 10000 })
    await expect(page.locator('.chat-input-wrapper .chat-file-attachment').first()).toBeVisible({ timeout: 10000 })

    // Re-opening shows the picker with that path already ticked — the sheet
    // seeds its draft from the composer, and the picker renders that state.
    await page.locator('.chat-attach-btn').click()
    await expect(attachSheet(page)).toBeVisible({ timeout: 10000 })
    await expect(attachSheet(page).locator(`[data-path="${path}"]`)).toHaveClass(/fp-row-selected/)
    await attachSheet(page).locator('[data-action="attach-cancel"]').click()
  })

  test('search narrows the picker to matching files', async ({ page }) => {
    await page.locator('.chat-attach-btn').click()
    const sheet = attachSheet(page)
    await expect(sheet).toBeVisible({ timeout: 10000 })

    await sheet.locator('.fp-search input').fill('package.json')
    // Results replace the listing; the exact file must be among them.
    await expect(sheet.locator('[data-path="package.json"]')).toBeVisible({ timeout: 10000 })

    await sheet.locator('[data-path="package.json"]').click()
    await sheet.locator('[data-action="attach-ok"]').click()
    await expect(page.locator('.chat-input-wrapper .chat-file-attachment').first()).toBeVisible({ timeout: 10000 })
  })
})

import { test, expect } from '../fixtures'
import { NavigationPage } from '../pages/navigation.page'

test.describe('Navigation', () => {
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    nav = new NavigationPage(page)
  })

  test('should switch from Chat to Files tab', async ({ page }) => {
    await nav.switchToFileManager()
    // File list or file manager content should be visible after switching
    // Use view-agnostic selectors to match both list (.file-item) and grid (.grid-item) modes
    await expect(page.locator('.file-list, .file-item, .file-grid, .grid-item').first()).toBeVisible({ timeout: 10000 })
  })

  test('should switch from Files back to Chat', async ({ page }) => {
    // Go to files first
    await nav.switchToFileManager()
    await expect(page.locator('.file-list, .file-item, .file-grid, .grid-item').first()).toBeVisible({ timeout: 10000 })

    // Switch back to chat
    await nav.switchToChat()

    // Chat textarea should be visible again
    await expect(page.locator('.chat-textarea')).toBeVisible()
  })

  test('should maintain state when switching tabs', async ({ page }) => {
    // Type something in chat
    const chatInput = page.locator('.chat-textarea')
    await chatInput.fill('test draft')

    // Switch to files and back
    await nav.switchToFileManager()
    await expect(page.locator('.file-list, .file-item, .file-grid, .grid-item').first()).toBeVisible({ timeout: 10000 })
    await nav.switchToChat()

    // Draft should be preserved (TabPanel uses v-show, not v-if, so component state persists)
    await expect(chatInput).toHaveValue('test draft')
  })

  test('should open overflow menu', async ({ page }) => {
    await nav.openOverflowMenu()
    // Overflow popup should be visible
    await expect(page.locator('.dock-overflow-popup')).toBeVisible()
  })

  test('should switch to Tasks tab', async ({ page }) => {
    await nav.switchToTasks()
    // Task tab content should be visible
    await expect(page.locator('.task-tab').first()).toBeVisible({ timeout: 10000 })
  })
})

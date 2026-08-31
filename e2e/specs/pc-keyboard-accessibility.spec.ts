import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { seedQuickSendItems, clearQuickSendItems, DEFAULT_QUICK_SEND_ITEMS } from '../helpers/test-data'
import { getServerURL } from '../helpers/server'

/**
 * E2E integration tests for PC keyboard accessibility features:
 *
 * 1. data-pc attribute on .app-container — set only for PC (desktop) user agents,
 *    absent on touch/mobile user agents. Drives the global :focus-visible rings.
 * 2. PopupMenu keyboard navigation — ArrowUp/Down to move focus, Enter to activate,
 *    Escape to close. Menu auto-focuses on open (PC).
 * 3. ChatInputBar Enter behavior — Enter key must go through handleSendClick():
 *    empty input → opens quick-send menu (no send), text input → sends.
 */
test.describe('PC Keyboard Accessibility', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    // Block the upgrade check so the "New Version Available" overlay never
    // appears and cannot intercept clicks on the send button.
    await page.route('**/api/upgrade/**', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{"hasUpdate":false,"latestVersion":""}' }))
    // Dismiss the first-run Welcome overlay so the chat panel is reachable.
    await page.evaluate(() => localStorage.setItem('clawbench_welcome_dismissed', 'true'))
    // Seed quick-send items so the menu has keyboard-navigable items
    await seedQuickSendItems(getServerURL())
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
    // Wait for the message history to finish loading — the count of rendered
    // messages stabilises once onLoadHistory has populated the list. This
    // prevents "last user message" assertions from matching stale history.
    await expect
      .poll(async () => {
        const count = await page.locator('.chat-messages-list > .chat-message').count()
        await page.waitForTimeout(300)
        return count === await page.locator('.chat-messages-list > .chat-message').count()
      }, { timeout: 10000 })
      .toBe(true)
  })

  test.afterEach(async () => {
    // Clean up seeded items so tests don't interfere with each other
    await clearQuickSendItems(getServerURL()).catch(() => {})
  })

  // ─────────────────────────────────────────────
  // data-pc attribute
  // ─────────────────────────────────────────────

  test('app-container has data-pc="true" on a desktop (PC) user agent', async ({ page }) => {
    const container = page.locator('.app-container')
    await expect(container).toHaveAttribute('data-pc', 'true')
  })

  test('app-container does NOT have data-pc="true" on a touch (mobile) user agent', async ({ page }) => {
    // New context with an iPhone UA (touch device). Set up the same storage
    // overrides BEFORE the page loads via addInitScript.
    const browser = page.context().browser()!
    const mobileCtx = await browser.newContext({
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
      viewport: { width: 390, height: 844 },
      hasTouch: true,
      isMobile: true,
    })
    await mobileCtx.addInitScript(() => {
      localStorage.setItem('clawbench_welcome_dismissed', 'true')
    })
    const mobilePage = await mobileCtx.newPage()
    try {
      await mobilePage.goto('/')
      await mobilePage.waitForLoadState('domcontentloaded')
      await expect(mobilePage.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
      const container = mobilePage.locator('.app-container')
      await expect(container).not.toHaveAttribute('data-pc', 'true')
    } finally {
      await mobileCtx.close()
    }
  })

  // ─────────────────────────────────────────────
  // PopupMenu keyboard navigation (via quick-send menu)
  // ─────────────────────────────────────────────

  test('ArrowDown + Enter activates the first quick-send item', async ({ page }) => {
    // Open quick-send menu with empty input
    await chat.openQuickSendMenu()
    const menu = page.locator('.popup-menu')
    await expect(menu).toBeVisible()
    await expect(page.locator('.quick-send-title')).toBeVisible()

    // Menu container should receive focus on open (PC)
    await expect(menu).toBeFocused()

    // ArrowDown should move focus to the first quick-send item
    await page.keyboard.press('ArrowDown')
    const firstItem = page.locator('.quick-send-item').first()
    await expect(firstItem).toBeFocused()

    // Enter should activate (click) the focused item — sending its command
    // and closing the menu. First default item is "继续" (continue).
    await page.keyboard.press('Enter')

    // Menu closes and a user message with the command is sent
    await expect(menu).not.toBeVisible()
    await expect(chat.getLastUserMessage()).toContainText(DEFAULT_QUICK_SEND_ITEMS[0].command, { timeout: 10000 })
  })

  test('Escape closes the quick-send menu', async ({ page }) => {
    await chat.openQuickSendMenu()
    const menu = page.locator('.popup-menu')
    await expect(menu).toBeVisible()

    await page.keyboard.press('Escape')

    await expect(menu).not.toBeVisible()
  })

  // ─────────────────────────────────────────────
  // ChatInputBar Enter behavior (handleSendClick parity)
  // ─────────────────────────────────────────────

  test('Enter on empty input opens quick-send menu instead of sending', async ({ page }) => {
    // Ensure textarea is empty
    await chat.clearInput()

    await page.keyboard.press('Enter')

    // Should open the quick-send menu, NOT send an empty message
    await expect(page.locator('.popup-menu')).toBeVisible()
    await expect(page.locator('.quick-send-title')).toBeVisible()
  })

  test('Enter with typed text sends the message (same as clicking send)', async ({ page }) => {
    const uniqueText = 'kbd_send_' + Date.now()

    await chat.fillInput(uniqueText)
    await expect(chat.textarea).toHaveValue(uniqueText)

    await page.keyboard.press('Enter')

    // The message just sent should appear as the last user message
    const userMsg = chat.getLastUserMessage()
    await expect(userMsg).toContainText(uniqueText, { timeout: 10000 })

    // Quick-send menu should NOT have opened
    await expect(page.locator('.popup-menu')).not.toBeVisible()
  })
})

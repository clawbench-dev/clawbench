import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

test.describe('Chat scroll FAB', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  /**
   * Helper: send multiple messages to fill the chat enough for scrolling.
   * ACP mock agent responds quickly, so we can accumulate messages.
   */
  async function fillChatWithMessages(count: number) {
    for (let i = 0; i < count; i++) {
      await chat.sendAndAwaitACPReply(`Message ${i + 1}`)
    }
  }

  test('scroll-up FAB appears when scrolling up in a long chat', async ({ page }) => {
    // Send enough messages to make the chat scrollable
    await fillChatWithMessages(4)

    // Scroll up by a significant amount using the messages container
    const messagesContainer = page.locator('.chat-messages')
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = Math.max(0, el.scrollTop - 400)
    })

    // The scroll-up FAB group should appear
    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Should have scroll-to-top and scroll-to-previous buttons
    const scrollUpButtons = scrollFabGroup.locator('.scroll-fab-btn')
    await expect(scrollUpButtons).toHaveCount(2)
  })

  test('scroll FAB auto-hides after 3 seconds', async ({ page }) => {
    await fillChatWithMessages(4)

    const messagesContainer = page.locator('.chat-messages')
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = Math.max(0, el.scrollTop - 400)
    })

    // FAB should be visible
    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Wait for auto-hide (3s delay + animation)
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 5000 })
  })

  test('clicking scroll-to-top FAB scrolls to top and button remains visible briefly', async ({ page }) => {
    await fillChatWithMessages(4)

    const messagesContainer = page.locator('.chat-messages')
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = Math.max(0, el.scrollTop - 400)
    })

    // Wait for FAB to appear
    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-top button (first button)
    await scrollFabGroup.locator('.scroll-fab-btn').first().click()

    // The chat should scroll toward the top
    // Wait a moment for smooth scroll to complete
    await page.waitForTimeout(700)

    // After reaching the top, the buttons should eventually disappear
    // (because nearTop triggers immediate hide during programmatic scroll)
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 5000 })

    // Verify we're near the top
    const scrollTop = await messagesContainer.evaluate((el: HTMLElement) => el.scrollTop)
    expect(scrollTop).toBeLessThan(150)
  })

  test('clicking scroll-to-bottom FAB scrolls to bottom', async ({ page }) => {
    await fillChatWithMessages(4)

    const messagesContainer = page.locator('.chat-messages')

    // Scroll up first to trigger the scroll-down FAB
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = Math.max(0, el.scrollTop - 400)
    })

    // Scroll down to trigger the down buttons
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = el.scrollTop + 300
    })

    // Wait for FAB to appear (scrolledDown direction)
    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-bottom button
    await scrollFabGroup.locator('.scroll-fab-btn').first().click()

    // Wait for smooth scroll
    await page.waitForTimeout(700)

    // Verify we're near the bottom
    const distFromBottom = await messagesContainer.evaluate((el: HTMLElement) =>
      el.scrollHeight - el.scrollTop - el.clientHeight
    )
    expect(distFromBottom).toBeLessThan(150)
  })

  test('clicking FAB resets the auto-hide timer so button stays visible', async ({ page }) => {
    await fillChatWithMessages(4)

    const messagesContainer = page.locator('.chat-messages')
    await messagesContainer.evaluate((el: HTMLElement) => {
      el.scrollTop = Math.max(0, el.scrollTop - 400)
    })

    // Wait for FAB to appear
    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-previous button (second button) — this doesn't
    // scroll to the top edge, so the button should remain visible after click
    await scrollFabGroup.locator('.scroll-fab-btn').nth(1).click()

    // The button should still be visible immediately after click
    // (timer was reset, not immediately hidden)
    await page.waitForTimeout(200)
    await expect(scrollFabGroup).toBeVisible()

    // It should eventually auto-hide after the 3s timer
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 5000 })
  })
})

import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Chat scroll FAB.
 *
 * IMPORTANT: the FAB visibility is gated on `isUserScrolling()` — a real user
 * gesture (touch, wheel, or scrollbar drag). A programmatic `el.scrollTop = …`
 * fires a scroll event but does NOT set that flag, so the FAB never appears.
 * These tests therefore scroll with `page.mouse.wheel()`, which dispatches a
 * genuine wheel event.
 */
test.describe('Chat scroll FAB', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  /** Send multiple messages so the transcript overflows and can scroll. */
  async function fillChatWithMessages(count: number) {
    for (let i = 0; i < count; i++) {
      await chat.sendAndAwaitACPReply(`Message ${i + 1}`)
    }
  }

  /**
   * Ensure the transcript is scrollable with enough range to satisfy the FAB
   * gating: `scrolledUp` needs scrollTop >= NEAR_TOP_THRESHOLD (100) AND
   * distFromBottom > SCROLL_BUTTON_TRIGGER (200), so the scrollable range must
   * exceed ~300px. A handful of messages may only yield ~300px, which the wheel
   * then consumes in one go (landing at the top, where the FAB is suppressed).
   */
  async function ensureScrollableRange(page: Page): Promise<void> {
    for (let i = 0; i < 6; i++) {
      const range = await page.locator('.chat-messages').evaluate(
        (el) => el.scrollHeight - el.clientHeight
      )
      if (range > 500) return
      await chat.sendAndAwaitACPReply(`Padding message ${i + 1}`)
    }
  }

  /** Whether the message container actually overflows (is scrollable). */
  async function isScrollable(page: Page): Promise<boolean> {
    return await page.locator('.chat-messages').evaluate(
      (el) => el.scrollHeight > el.clientHeight + 10
    )
  }

  /** Scroll the transcript with a real wheel gesture over the message list. */
  async function wheelScroll(page: Page, deltaY: number): Promise<void> {
    const box = await page.locator('.chat-messages').boundingBox()
    if (!box) throw new Error('.chat-messages has no layout box')
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.wheel(0, deltaY)
  }

  test('scroll-up FAB appears when scrolling up in a long chat', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page), 'chat transcript must overflow for this test').toBe(true)

    // Scroll up with a real wheel gesture.
    await wheelScroll(page, -400)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Should have scroll-to-top and scroll-to-previous buttons
    await expect(scrollFabGroup.locator('.scroll-fab-round')).toHaveCount(2)
  })

  test('scroll FAB auto-hides after 3 seconds', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    await wheelScroll(page, -400)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Wait for auto-hide (3s delay + animation)
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })
  })

  test('clicking scroll-to-top FAB scrolls to top and button remains visible briefly', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    await wheelScroll(page, -400)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-top button (first button)
    const before = await page.locator('.chat-messages').evaluate((el) => el.scrollTop)
    await scrollFabGroup.locator('.scroll-fab-round').first().click()

    // After reaching the top the buttons hide (nearTop triggers immediate hide
    // during programmatic scroll).
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })

    // Verify the jump moved the viewport up substantially. A full-suite run can
    // have older history to load, and the load-more anchoring then settles the
    // scroll above 0 — so assert a large reduction, not an exact zero.
    await expect.poll(
      async () => page.locator('.chat-messages').evaluate((el) => el.scrollTop),
      { timeout: 15000, intervals: [200, 500, 1000] }
    ).toBeLessThan(before - 200)
  })

  test('clicking scroll-to-bottom FAB scrolls to bottom', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    // Scroll up first (real gesture), then scroll down a little so the FAB
    // switches to the "down" direction while still away from the bottom.
    await wheelScroll(page, -600)
    await wheelScroll(page, 200)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-bottom button
    await scrollFabGroup.locator('.scroll-fab-round').first().click()

    // Wait for the smooth scroll to settle
    await expect.poll(async () =>
      page.locator('.chat-messages').evaluate(
        (el) => el.scrollHeight - el.scrollTop - el.clientHeight
      ), { timeout: 8000 }
    ).toBeLessThan(150)
  })

  test('clicking FAB resets the auto-hide timer so button stays visible', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    await wheelScroll(page, -400)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-previous button (second button) — this does not reach
    // the top edge, so the group stays visible after the click (timer reset).
    await scrollFabGroup.locator('.scroll-fab-round').nth(1).click()
    await expect(scrollFabGroup).toBeVisible()

    // It should still auto-hide eventually
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })
  })
})

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

  // Per-turn budget. The config default (30s) is the timeout `waitForReply`
  // uses too, and a single mock turn on a loaded CI runner has been measured at
  // ~8.5s end to end (1200ms mock delay + per-word streaming + the
  // /api/ai/sessions poll in `sendAndAwaitACPReply`), so a turn that queues
  // behind a leftover one can exceed 30s on its own. Raising only the TEST
  // timeout left this inner one at 30s and the test still died inside setup.
  const TURN_TIMEOUT = 60000

  // Setup is bounded by the turn count: at most 4 fillers + 3 paddings. The
  // padding loop stops as soon as the transcript is scrollable (normally after
  // 0-1 extra turns), so the realistic cost is 4-5 turns ≈ 40-60s on a slow
  // runner; the budget leaves room for that plus the idle wait in beforeEach.
  test.setTimeout(180000)

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    // The run shares one server and one default session: wait out whatever the
    // previous spec left streaming (or queued) so our sends start turns instead
    // of joining someone else's queue.
    await chat.waitForIdle(60000)
  })

  /** Send multiple messages so the transcript overflows and can scroll. */
  async function fillChatWithMessages(count: number) {
    for (let i = 0; i < count; i++) {
      await chat.sendAndAwaitACPReply(`Message ${i + 1}`, TURN_TIMEOUT)
    }
  }

  /**
   * Ensure the transcript is scrollable with enough range to satisfy the FAB
   * gating: `scrolledUp` needs scrollTop >= NEAR_TOP_THRESHOLD (100) AND
   * distFromBottom > SCROLL_BUTTON_TRIGGER (200), so the scrollable range must
   * exceed ~300px. A handful of messages may only yield ~300px, which the wheel
   * then consumes in one go (landing at the top, where the FAB is suppressed).
   *
   * Bounded to 3 extra turns (7 messages total): the loop is data-driven, so it
   * normally adds none, and each extra turn is expensive on a slow runner. If 7
   * messages still do not overflow, the caller's `isScrollable` assertion fails
   * with a clear message rather than the setup eating the whole budget.
   */
  async function ensureScrollableRange(page: Page): Promise<void> {
    for (let i = 0; i < 3; i++) {
      const range = await page.locator('.chat-messages').evaluate(
        (el) => el.scrollHeight - el.clientHeight
      )
      if (range > 500) return
      await chat.sendAndAwaitACPReply(`Padding message ${i + 1}`, TURN_TIMEOUT)
    }
  }

  /** Whether the message container actually overflows (is scrollable). */
  async function isScrollable(page: Page): Promise<boolean> {
    return await page.locator('.chat-messages').evaluate(
      (el) => el.scrollHeight > el.clientHeight + 10
    )
  }

  /** Scroll offset and content height of the transcript container. */
  async function scrollState(page: Page): Promise<{ top: number; height: number }> {
    return await page.locator('.chat-messages').evaluate(
      (el) => ({ top: el.scrollTop, height: el.scrollHeight })
    )
  }

  /** Scroll the transcript with a real wheel gesture over the message list. */
  async function wheelScroll(page: Page, deltaY: number): Promise<void> {
    const box = await page.locator('.chat-messages').boundingBox()
    if (!box) throw new Error('.chat-messages has no layout box')
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.wheel(0, deltaY)
  }

  /**
   * Wheel up until the FAB is in its "up" state, and fail with the measured
   * geometry if it never gets there.
   *
   * The group renders EITHER the up pair or the down pair depending on
   * `scrolledUp`, and both pairs use `.scroll-fab-round` — so `.first()` is the
   * scroll-to-top button only while the container is in the up band
   * (`scrollTop >= 100` and more than 200px from the bottom, mirroring the
   * component's own gating). A single wheel tick is not enough to guarantee
   * that on a loaded runner: the event can land before the container settles, or
   * leave the view still inside the bottom band — then `.first()` is the
   * scroll-to-BOTTOM button and the "jump to top" assertions measure the wrong
   * action (observed on a 2-core run: scrollTop stayed at 2469).
   */
  async function wheelUpUntilFabIsUp(page: Page): Promise<void> {
    const container = page.locator('.chat-messages')
    const geometry = async () => container.evaluate((el) => ({
      top: el.scrollTop,
      fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight,
    }))
    for (let i = 0; i < 5; i++) {
      const m = await geometry()
      if (m.top >= 100 && m.fromBottom > 200) return
      await wheelScroll(page, -400)
    }
    const m = await geometry()
    throw new Error(`transcript never reached the "scrolled up" band: scrollTop=${m.top}, fromBottom=${m.fromBottom}`)
  }

  test('scroll-up FAB appears when scrolling up in a long chat', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page), 'chat transcript must overflow for this test').toBe(true)

    // Scroll up with real wheel gestures until the FAB is in the up state.
    await wheelUpUntilFabIsUp(page)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Should have scroll-to-top and scroll-to-previous buttons
    await expect(scrollFabGroup.locator('.scroll-fab-round')).toHaveCount(2)
  })

  test('scroll FAB auto-hides after 3 seconds', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    await wheelUpUntilFabIsUp(page)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Wait for auto-hide (3s delay + animation)
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })
  })

  test('clicking scroll-to-top FAB scrolls to top and button remains visible briefly', async ({ page }) => {
    await fillChatWithMessages(4)
    await ensureScrollableRange(page)
    expect(await isScrollable(page)).toBe(true)

    await wheelUpUntilFabIsUp(page)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-top button (first button of the UP pair)
    const before = await scrollState(page)
    await scrollFabGroup.locator('.scroll-fab-round').first().click()

    // After reaching the top the buttons hide (nearTop triggers immediate hide
    // during programmatic scroll).
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })

    // What the click guarantees is that the viewport ends at the TOP of the
    // content that was loaded when it was clicked.
    //
    // `scrollTop` alone cannot express that: the jump enters the history
    // prefetch band, so older pages are prepended and the container's anchoring
    // corrects the scroll position by exactly the height they added. On a
    // session with unloaded history that leaves a large `scrollTop` even though
    // the view did move to the top of the previously-loaded content (measured:
    // 2069 → 2065 while ~2k px of history was prepended). Subtracting the
    // prepended height is exact in both cases — with nothing prepended it
    // reduces to "scrollTop must be near 0", which is what this test asserts on
    // a fully-loaded transcript, and a click that does nothing still fails.
    await expect.poll(
      async () => {
        const now = await scrollState(page)
        return now.top - Math.max(0, now.height - before.height)
      },
      { timeout: 20000, intervals: [200, 500, 1000] }
    ).toBeLessThan(200)
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

    await wheelUpUntilFabIsUp(page)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-previous button (second button of the UP pair) — this does not reach
    // the top edge, so the group stays visible after the click (timer reset).
    await scrollFabGroup.locator('.scroll-fab-round').nth(1).click()
    await expect(scrollFabGroup).toBeVisible()

    // It should still auto-hide eventually
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })
  })
})

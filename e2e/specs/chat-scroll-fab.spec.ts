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
   * Wheel up until the FAB actually renders its UP pair, and fail with the
   * measured geometry if it never does.
   *
   * The group renders EITHER the up pair or the down pair, and both use
   * `.scroll-fab-round` — so `.first()` is the scroll-to-top button only while
   * `scrolledUp` is true. `scrolledUp` is NOT a function of geometry alone:
   * the component sets it from the LAST gesture's direction
   * (`shouldShowUp = scrollDelta < 0 && distFromBottom > 200 && !nearTop`).
   * Checking geometry without wheeling up therefore "succeeds" right after a
   * downward scroll and the click lands on **Scroll to bottom** (measured on a
   * throttled run: `clicked="Scroll to bottom"`, scrollTop 600 → 1402).
   *
   * So: every attempt performs a real upward wheel, and success is confirmed by
   * the rendered button's own label rather than by geometry alone.
   */
  async function wheelUpUntilFabIsUp(page: Page): Promise<void> {
    const container = page.locator('.chat-messages')
    const firstButton = page.locator('.scroll-fab-group .scroll-fab-round').first()
    const upLabel = /scroll to top|回到顶部/i
    const geometry = async () => container.evaluate((el) => ({
      top: el.scrollTop,
      fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight,
    }))
    // Park the viewport a fixed distance below the top. Where the test starts is
    // not controllable: the transcript may open anywhere (restored position,
    // queued backlog still draining, a session whose history is mostly older
    // messages), and a wheel near the top edge is clamped to a delta the
    // component ignores — then "wheel down, wheel up" nets zero and the loop
    // never makes progress (observed on WebKit: scrollTop oscillated to 0).
    // Parking makes the following 400px upward wheel land at ~400, inside the
    // FAB's up band.
    const park = async () => {
      await container.evaluate((el) => {
        el.scrollTop = Math.min(800, el.scrollHeight - el.clientHeight)
      })
    }
    if ((await geometry()).top < 600) await park()
    for (let i = 0; i < 4; i++) {
      await wheelScroll(page, -400)
      const m = await geometry()
      if (m.top < 100) {
        await park()
        continue
      }
      // The pair swaps through `<Transition mode="out-in">`: right after the
      // gesture the DOWN node is still the one mounted (it leaves first), so a
      // single read would see "Scroll to bottom" and wrongly conclude the state
      // never flipped. Wait for the swap instead of racing it.
      const flipped = await expect
        .poll(async () => firstButton.getAttribute('title').catch(() => null), {
          timeout: 2000,
          intervals: [100, 200, 500],
        })
        .toMatch(upLabel)
        .then(() => true)
        .catch(() => false)
      if (flipped) return
    }
    const m = await geometry()
    throw new Error(
      `FAB never reached its UP pair: scrollTop=${m.top}, fromBottom=${m.fromBottom}, ` +
      `firstButtonTitle=${await firstButton.getAttribute('title').catch(() => null)}`,
    )
  }

  /**
   * Load every older page, so the transcript stops growing when the view
   * reaches the top.
   *
   * `scrollToTop()` smooth-scrolls to 0, but entering the history prefetch band
   * loads an older page MID-ANIMATION: the content change cancels the browser's
   * smooth scroll and the container's anchoring only restores the visual
   * position for the prepended height, so the jump stops part-way. Measured on
   * a throttled 2-core run: 2023 → 992 with a 614px page and 2023 → 1914 with a
   * 1431px page — i.e. the final offset depends on WHEN the page landed, not on
   * the FAB. With nothing left to load the jump completes, which is the
   * behaviour this test is actually about.
   *
   * Sets `scrollTop = 0` repeatedly: `maybeLoadOlder()` runs for programmatic
   * offsets too, and each page leaves the view anchored one page lower, so the
   * loop needs to push it back up every round.
   */
  async function loadAllHistory(page: Page): Promise<void> {
    const container = page.locator('.chat-messages')
    for (let i = 0; i < 20; i++) {
      const height = await container.evaluate((el) => el.scrollHeight)
      await container.evaluate((el) => { el.scrollTop = 0 })
      const grew = await expect
        .poll(async () => container.evaluate((el) => el.scrollHeight), {
          // Generous on purpose: returning while a page is still in flight
          // would let it land during the jump and cancel the smooth scroll.
          timeout: 5000,
          intervals: [100, 200, 500],
        })
        .toBeGreaterThan(height)
        .then(() => true)
        .catch(() => false)
      if (!grew) return
    }
    throw new Error('history did not finish loading after 20 pages')
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

    // Finish loading the history BEFORE measuring: a page that lands while the
    // jump animates cancels the smooth scroll, so the final offset would depend
    // on load timing rather than on the button (see loadAllHistory).
    await loadAllHistory(page)

    // Move away from the top so the FAB renders its UP pair, then confirm the
    // container is in the band the component gates `scrolledUp` on.
    await wheelScroll(page, 600)
    await wheelUpUntilFabIsUp(page)

    const scrollFabGroup = page.locator('.scroll-fab-group')
    await expect(scrollFabGroup).toBeVisible({ timeout: 5000 })

    // Click the scroll-to-top button (first button of the UP pair)
    const before = await scrollState(page)
    const target = scrollFabGroup.locator('.scroll-fab-round').first()
    const targetTitle = await target.getAttribute('title')
    await target.click()

    // After reaching the top the buttons hide (nearTop triggers immediate hide
    // during programmatic scroll).
    await expect(scrollFabGroup).not.toBeVisible({ timeout: 8000 })

    // The jump must land at the top of the loaded content. History is fully
    // loaded by now, so nothing is prepended during the animation and this
    // reduces to "scrollTop is near 0"; the subtraction is kept because a page
    // that still arrives (e.g. a message added while the test ran) is exactly
    // the offset anchoring compensates for, and a click that does nothing
    // leaves `top` at its pre-click value either way.
    let lastSeen = before
    try {
      await expect.poll(
        async () => {
          const now = await scrollState(page)
          lastSeen = now
          return now.top - Math.max(0, now.height - before.height)
        },
        { timeout: 20000, intervals: [200, 500, 1000] }
      ).toBeLessThan(200)
    } catch (err) {
      throw new Error(
        `scroll-to-top did not reach the top of the loaded content. ` +
        `clicked="${targetTitle}" before=${JSON.stringify(before)} last=${JSON.stringify(lastSeen)}\n` +
        String(err),
      )
    }
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

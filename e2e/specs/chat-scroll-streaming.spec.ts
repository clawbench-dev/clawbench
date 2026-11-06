import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Scrolling DURING a streaming reply (issue #632).
 *
 * Reported: "while the AI is still writing, the transcript is locked — a mouse
 * wheel notch toward the newest line snaps straight back, so the reply can only
 * be read once the turn ends."
 *
 * Root cause: a reply in flight re-pins the viewport to the bottom on every
 * streamed frame (`onContentGrown` / the stream's scroll scheduler). WebKit
 * animates a wheel notch over several frames, so a pin written between them
 * cancelled the gesture *before* it had moved the viewport past
 * RESUME_FOLLOW_PX — the "user left the bottom" latch never tripped and the
 * viewport was snapped back to the bottom. Every notch bounced.
 *
 * Only observable in a real engine: jsdom has no layout, no wheel animation and
 * no scroll clamping, so the unit tests can prove the decision but not the
 * cancellation.
 */
test.describe('Chat scrolling during a streaming reply', () => {
  let chat: ChatPage

  test.setTimeout(240_000)

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    await page.goto('/')
    // The chat pane's collapsed state persists per profile; the rail only
    // exists while collapsed, so its presence IS the check.
    const rail = page.locator('[data-action="show-chat"]')
    if (await rail.isVisible().catch(() => false)) await rail.click()
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })
    // The suite shares one server and one default session: wait out whatever a
    // previous spec left running so this send starts a turn of its own.
    await chat.waitForIdle(90000)
  })

  async function geometry(page: Page) {
    return page.locator('.chat-messages').evaluate((el) => ({
      top: Math.round(el.scrollTop),
      max: Math.round(el.scrollHeight - el.clientHeight),
    }))
  }

  /** Grow the transcript until a wheel gesture has room to move. */
  async function makeScrollable(page: Page): Promise<void> {
    for (let i = 0; i < 8; i++) {
      if ((await geometry(page)).max > 600) return
      await chat.sendAndAwaitACPReply(`Padding message ${i + 1} for the scroll range.`)
    }
    throw new Error('transcript never became scrollable')
  }

  async function wheel(page: Page, dy: number): Promise<void> {
    const box = await page.locator('.chat-messages').boundingBox()
    if (!box) throw new Error('.chat-messages has no layout box')
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.wheel(0, dy)
  }

  test('a wheel notch during a streaming reply is not snapped back to the bottom', async ({ page }) => {
    await makeScrollable(page)

    // A long reply keeps the content growing below the viewport for ~20s.
    await chat.sendMessage('[mock:long-reply] stream something long please')
    const streaming = page.locator('.chat-message[data-streaming="true"]')
    await expect(streaming).toBeVisible({ timeout: 20000 })
    // Wait until the reply is actually WRITING. Wheel before that and the
    // gesture is eaten by the send's own force pin (which is deliberate: sending
    // means "show me the bottom"), and the assertion below measures the wrong
    // thing.
    await expect(streaming).toContainText(/.+/, { timeout: 20000 })

    // Wait until the reply has rendered a real amount of text. Right after the
    // first word the turn start is still re-laying-out (the previous reply
    // collapses to its summary in the mixed display mode, the first blocks
    // arrive) and Firefox cancels a wheel animation on any layout change of the
    // scroller — wheel in that churn and the gesture measures as "ignored by
    // the engine", not as the regression under test.
    await expect.poll(async () => {
      const box = await streaming.boundingBox()
      return box ? Math.round(box.height) : 0
    }, { timeout: 30000, intervals: [200, 500, 1000] }).toBeGreaterThan(300)

    // Sending force-pins to the bottom; let the frames settle there.
    await expect.poll(async () => {
      const g = await geometry(page)
      return g.max - g.top
    }, { timeout: 10000 }).toBeLessThan(200)
    const atBottom = await geometry(page)
    // One notch up. Before the fix this moved the viewport a couple of pixels
    // and the next streamed frame put it straight back at the bottom.
    await wheel(page, -200)
    await expect.poll(async () => atBottom.top - (await geometry(page)).top, {
      timeout: 5000,
      intervals: [50, 100, 200],
    }).toBeGreaterThan(120)

    // …and it STAYS there while the reply keeps streaming (this is the part
    // that failed: the latch never engaged, so every subsequent frame re-pinned).
    // The wait is for MORE CONTENT, not for a wall-clock window: "the position
    // did not move" proves nothing while the reply is between two words — a
    // finished turn emits no pins at all and would pass every static check.
    const parked = await geometry(page)
    await expect.poll(async () => (await geometry(page)).max, {
      timeout: 15000,
      intervals: [100, 200, 500],
    }).toBeGreaterThan(parked.max + 20)
    const stillParked = await geometry(page)
    expect(Math.abs(stillParked.top - parked.top)).toBeLessThan(60)
    expect(stillParked.max - stillParked.top).toBeGreaterThan(120)

    // The stream must still be running — otherwise the assertion above proves
    // nothing (no content growth, no pins).
    await expect(streaming).toBeVisible()

    // Follow resumes on its own once the gesture is over and the viewport is
    // back at the bottom. Done with the container's own scroll (not a wheel):
    // what the wheel would land on depends on which message happens to sit
    // under the pointer, and a nested scroller there swallows the gesture —
    // engine-dependent noise that has nothing to do with this regression.
    // The wheel→follow hand-off is covered by shouldPin's unit test.
    //
    // Asserted AFTER more content arrived: checking the gap right after the
    // write would be true by construction (gap 0, latch or not). Only a reply
    // that is still growing can show whether the pin resumed.
    await page.locator('.chat-messages').evaluate((el) => { el.scrollTop = el.scrollHeight })
    const resumedBase = (await geometry(page)).max
    await expect.poll(async () => (await geometry(page)).max, {
      timeout: 15000,
      intervals: [100, 200, 500],
    }).toBeGreaterThan(resumedBase + 20)
    const resumed = await geometry(page)
    expect(resumed.max - resumed.top).toBeLessThan(80)

    await chat.waitForIdle(120000)
  })
})

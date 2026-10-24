import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Session-switch scroll memory (issue #576).
 *
 * The behaviour under test is only observable in a real browser: jsdom reports
 * scrollHeight/clientHeight as 0 and never clamps scrollTop, so a unit test
 * with faked geometry can prove the *decision* (which value gets written) but
 * not that the viewport actually lands there. These specs drive a real
 * scrollable transcript through a real session switch.
 *
 * What the user asked for, precisely:
 * - a STILL session reopens where they were reading;
 * - a RUNNING session still lands at the bottom (its output is the point);
 * - a session left AT the bottom still reopens at the bottom.
 */
test.describe.serial('Session switch scroll memory', () => {
  let chat: ChatPage

  // webkit is markedly slower to lay out a long transcript and to settle a
  // session switch; the default 30s leaves no room for the prep loop plus the
  // switch plus the restore retries.
  test.describe.configure({ timeout: 180_000 })

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    await page.goto('/')
    // The chat pane's collapsed state is persisted per profile, and a collapsed
    // pane has no visible transcript to scroll — the dock button is the stable
    // hook (data-tab), and aria-pressed tells us which way it currently is.
    const chatToggle = page.locator('[data-tab="chat"]').first()
    if ((await chatToggle.getAttribute('aria-pressed')) === 'false') {
      await chatToggle.click()
    }
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })
  })

  /** Read the scroll container's current geometry. */
  async function scrollInfo(page: Page) {
    return page.locator('.chat-messages').evaluate((el) => ({
      top: el.scrollTop,
      max: el.scrollHeight - el.clientHeight,
    }))
  }

  /** Grow the transcript until it has real scroll range to work with. */
  async function makeScrollable(page: Page): Promise<void> {
    for (let i = 0; i < 10; i++) {
      const { max } = await scrollInfo(page)
      if (max > 800) return
      await chat.sendAndAwaitACPReply(`Padding message ${i + 1} with enough text to take vertical space in the transcript.`)
    }
    throw new Error('transcript never became scrollable')
  }

  /**
   * Scroll with a real wheel gesture.
   *
   * A programmatic `el.scrollTop = …` fires a scroll event but does not set the
   * `userLeftBottom` latch (it only samples real input), so the session would
   * look "still at the bottom" and be forgotten on the way out — testing the
   * wrong branch.
   */
  /**
   * Read the scroll offset once it has stopped changing.
   *
   * A wheel gesture keeps coasting for a few frames (much longer in webkit), so
   * sampling immediately after it can capture a transient offset — and then the
   * restore, which correctly lands on the SETTLED position, looks wrong by
   * exactly the distance the coast still had to travel.
   */
  async function settleScroll(page: Page) {
    let prev = await scrollInfo(page)
    for (let i = 0; i < 20; i++) {
      await page.waitForTimeout(100)
      const now = await scrollInfo(page)
      if (now.top === prev.top) return now
      prev = now
    }
    return prev
  }

  async function wheelUp(page: Page, deltaY: number) {
    const box = await page.locator('.chat-messages').boundingBox()
    if (!box) throw new Error('no scroll container')
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.wheel(0, -deltaY)
  }

  /**
   * Ensure a second session exists so a switch is actually possible.
   *
   * The suite starts with a single session (the one the fixture created), and
   * `[data-session-id]:not(...)` then matches nothing — the click just times
   * out. Creating the second session through the API keeps this independent of
   * the UI's create flow. Returns the id of the FIRST session (the one the
   * caller has been scrolling in).
   */
  async function ensureSecondSession(page: Page): Promise<string> {
    const ids = await page.evaluate(async () => {
      const r = await fetch('/api/ai/sessions')
      const d = await r.json()
      return (d.sessions || []).map((s: { id: string }) => s.id)
    })
    if (ids.length < 2) {
      const created = await page.evaluate(async () => {
        const r = await fetch('/api/ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ agentId: 'acp-mock' }),
        })
        if (!r.ok) throw new Error(`create session failed: ${r.status}`)
        return r.json()
      })
      expect(created.sessionId).toBeTruthy()
      // Creating a session makes the app switch to it. Come back to the
      // original one so the caller scrolls in the transcript it has been
      // filling — otherwise it measures the empty new session (max=0).
      const back = page.locator(`.session-row[data-session-id]:visible[data-session-id="${ids[0]}"]`).first()
      await expect(back).toBeVisible({ timeout: 10000 })
      await back.click()
      await expect(page.locator('.chat-messages')).toBeVisible()
    }
    // Return the session the app is ACTUALLY showing: tests run in sequence and
    // a previous one may have left another session active, so `ids[0]` is not
    // necessarily the transcript on screen.
    const active = await page.evaluate(
      () => document.querySelector('.session-row .session-item.active')?.closest('.session-row')?.getAttribute('data-session-id') ?? null,
    )
    return active || ids[0]
  }

  /**
   * Switch to another session and back, using the sidebar rows.
   *
   * On a wide viewport the session sidebar is permanently mounted, so opening
   * the drawer would put a `.bs-overlay` on top of it and intercept the click
   * (Playwright reports exactly that). The drawer is only needed on narrow
   * viewports, where no row is visible yet.
   */
  async function switchAwayAndBack(page: Page, sessionId: string) {
    // The e2e viewport is wide, so the session sidebar is permanently mounted —
    // opening the drawer would cover it with a `.bs-overlay` that swallows the
    // click. Use the sidebar rows directly and never touch the drawer here.
    const rowSel = '.session-row[data-session-id]:visible'
    const other = page.locator(`${rowSel}:not([data-session-id="${sessionId}"])`).first()
    await expect(other, 'a second session must be listed in the sidebar').toBeVisible({ timeout: 10000 })
    await other.click()
    await expect(page.locator('.chat-messages')).toBeVisible()

    const back = page.locator(`${rowSel}[data-session-id="${sessionId}"]`).first()
    await expect(back).toBeVisible({ timeout: 10000 })
    await back.click()
    // Wait for the switch to settle: the skeleton only renders while the
    // history is loading, and measuring before it clears reads a half-built
    // list whose height (and therefore the restored offset) is still moving.
    await expect(page.locator('.chat-switching-indicator')).toHaveCount(0, { timeout: 20000 })
    await expect(page.locator('.chat-message').first()).toBeVisible({ timeout: 20000 })
  }

  test('a still session reopens at the reading position', async ({ page }) => {
    // Create the second session FIRST: POSTing /api/ai/sessions makes the app
    // switch to it, so doing it after the scroll prep would leave us measuring
    // an empty transcript (max=0 → the sanity check fails with 0).
    const sessionId = await ensureSecondSession(page)
    expect(sessionId).toBeTruthy()
    await makeScrollable(page)

    // Read partway up the transcript.
    await wheelUp(page, 600)
    await expect
      .poll(async () => (await scrollInfo(page)).top, { timeout: 5000 })
      .toBeGreaterThan(100)
    // Let the wheel's momentum settle before sampling: a mid-fling reading is
    // not the position the user "left", and asserting against it makes the
    // comparison meaningless (webkit keeps coasting longer than chromium).
    const left = await settleScroll(page)
    // Sanity: we must be genuinely away from the bottom, or the feature is
    // supposed to forget the position and this test proves nothing.
    expect(left.max - left.top).toBeGreaterThan(300)

    await switchAwayAndBack(page, sessionId)

    // The restored offset must be close to where we left — not 0 (top) and not
    // max (bottom). A few pixels of drift is fine; the anchor nudge corrects
    // for content that grew while away.
    await expect
      .poll(async () => {
        const now = await scrollInfo(page)
        return Math.abs(now.top - left.top)
      }, { timeout: 10000 })
      .toBeLessThan(120)
  })

  test('a session left at the bottom reopens at the bottom', async ({ page }) => {
    // Create the second session FIRST: POSTing /api/ai/sessions makes the app
    // switch to it, so doing it after the scroll prep would leave us measuring
    // an empty transcript (max=0 → the sanity check fails with 0).
    const sessionId = await ensureSecondSession(page)
    expect(sessionId).toBeTruthy()
    await makeScrollable(page)

    // Stay at the bottom, then leave and return.
    const before = await scrollInfo(page)
    expect(before.max - before.top, 'precondition: at the bottom').toBeLessThan(50)

    await switchAwayAndBack(page, sessionId)

    await expect
      .poll(async () => {
        const now = await scrollInfo(page)
        return now.max - now.top
      }, { timeout: 10000 })
      .toBeLessThan(50)
  })

  test('a running session still lands at the bottom', async ({ page }) => {
    // Create the second session FIRST: POSTing /api/ai/sessions makes the app
    // switch to it, so doing it after the scroll prep would leave us measuring
    // an empty transcript (max=0 → the sanity check fails with 0).
    const sessionId = await ensureSecondSession(page)
    expect(sessionId).toBeTruthy()
    await makeScrollable(page)

    // Leave mid-history so a still session WOULD be restored…
    await wheelUp(page, 600)
    await expect.poll(async () => (await scrollInfo(page)).top, { timeout: 5000 }).toBeGreaterThan(100)

    // …then start a turn and switch away/back while it runs. The turn must win:
    // a running session shows its newest output, not the old reading position.
    await chat.fillInput('a long running turn')
    const countBefore = await chat.sendMessage('a long running turn')
    void countBefore
    // Do not await the reply — we want the switch to happen mid-turn.
    await switchAwayAndBack(page, sessionId)

    await expect
      .poll(async () => {
        const now = await scrollInfo(page)
        return now.max - now.top
      }, { timeout: 15000 })
      .toBeLessThan(80)
  })
})

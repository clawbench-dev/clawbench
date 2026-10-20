import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * The action bar must not move when a reply finishes.
 *
 * Regression: the `done` handler finalizes the bubble FIRST
 * (forceCleanupStreamingState deletes `streaming`) and only then calls
 * loadHistory. The backend runs summarization synchronously inside Finalize, so
 * that snapshot's row is summary-stripped (`blocks: []`). rebuildFromDb located
 * the live placeholder by `m.streaming`, which was already gone, so the
 * just-completed reply was dropped as "not in the DB" and the stripped row took
 * its place — the bubble collapsed to the summary for one frame and the
 * lazy-original refetch restored it.
 *
 * Measured before the fix (Chromium, 25ms sampling):
 *   message height 126 → 66 → 126
 *   meta bar top   275 → 214 → 275
 * i.e. the action bar is painted, jumps up 61px, then returns. The vacated band
 * is what users see as a duplicated duration/time line below the bar (a stale
 * paint, not a second DOM node — the DOM never holds more than one bar).
 *
 * The assertion is on geometry rather than pixels: the collapse is a pure
 * layout event, so a stable height/position across the completion frame proves
 * the flicker cannot occur.
 */
test.describe('Chat message action bar', () => {
  test('does not jump when the reply finishes', async ({ page }) => {
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible()

    await chat.sendMessage('metabar_' + Date.now())
    // Wait for the live streaming bubble before sampling, so the send's own
    // list remount is behind us.
    await page.waitForFunction(() => {
      const msgs = document.querySelectorAll('.chat-message.assistant')
      const last = msgs[msgs.length - 1] as HTMLElement | undefined
      return !!last && last.getAttribute('data-streaming') === 'true'
    }, undefined, { timeout: 15000 })

    // Sample densely across the streaming -> done transition. The collapse
    // lasted a single frame (~30ms), so a coarse poll would miss it entirely.
    const samples: Array<{ streaming: string | null; h: number; barTop: number | null }> = []
    const started = Date.now()
    while (Date.now() - started < 20000) {
      const snap = await page.evaluate(() => {
        const msgs = document.querySelectorAll('.chat-message.assistant')
        const last = msgs[msgs.length - 1] as HTMLElement | undefined
        if (!last) return null
        const bar = last.querySelector('.chat-meta-bar') as HTMLElement | null
        return {
          streaming: last.getAttribute('data-streaming'),
          h: Math.round(last.getBoundingClientRect().height),
          barTop: bar ? Math.round(bar.getBoundingClientRect().top) : null,
        }
      })
      if (snap) samples.push(snap)
      if (snap && snap.streaming !== 'true') {
        // Keep sampling a few frames past completion — the refetch lands shortly
        // after the first settled frame.
        if (samples.filter((s) => s.streaming !== 'true').length > 15) break
      }
      await page.waitForTimeout(20)
    }

    const settled = samples.filter((s) => s.streaming !== 'true' && s.barTop !== null)
    expect(settled.length, 'must observe the settled state').toBeGreaterThan(0)

    const heights = settled.map((s) => s.h)
    const barTops = settled.map((s) => s.barTop as number)

    // A single-pixel tolerance absorbs subpixel rounding, not a 60px collapse.
    expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1)
    expect(Math.max(...barTops) - Math.min(...barTops)).toBeLessThanOrEqual(1)

    // The bar must also carry the duration, i.e. the message really finalized
    // (guards against passing this test by never finishing the turn).
    await expect(page.locator('.chat-message.assistant').last().locator('.chat-meta-bar')).toContainText(/\d/)
  })
})

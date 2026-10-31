import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * The chat-area message rail, verified in a real engine.
 *
 * jsdom reports every rect as 0 and has no CSS engine, so the two behaviours
 * that matter here cannot be covered by the component tests:
 *
 *   1. the "you are here" mark names the message under the reader's eye — at
 *      the ends of the conversation that is unambiguously the first / last one,
 *      which is exactly where a "nearest to the viewport centre" rule got it
 *      wrong (first bubbles scrolled past the centre line, a short last reply
 *      below it);
 *   2. the rail appears as soon as the pane has a gutter — including when it is
 *      stretched from narrow to wide while mounted. It used to stay hidden
 *      until the panel was remounted (switching sessions).
 */
test.describe('chat message rail', () => {
  test.setTimeout(120000)
  test.use({ viewport: { width: 2200, height: 700 } })

  test('highlight tracks the reading position and the rail follows the pane width', async ({ page }) => {
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible()

    // Three turns: enough to overflow a 700px-tall viewport. The session is
    // shared with every other spec on this server, so wait for it to be idle
    // first — a send during someone else's turn is queued and its reply lands
    // only after that turn drains, which reads as "the agent never answered".
    for (let i = 1; i <= 3; i++) {
      await expect(chat.stopButton).toHaveCount(0, { timeout: 30000 })
      const before = await chat.sendMessage(`rail check ${i} — padding text so the bubbles take up height`)
      await chat.waitForReply(30000, before)
    }
    await expect(chat.stopButton).toHaveCount(0, { timeout: 30000 })
    await page.waitForTimeout(1500)

    const railState = () =>
      page.evaluate(() => {
        const scroller = document.querySelector('.chat-messages') as HTMLElement
        const rail = document.querySelector('.msg-rail')
        const nodes = Array.from(document.querySelectorAll('.msg-rail-node'))
        return {
          visible: rail?.classList.contains('msg-rail--visible') ?? null,
          nodes: nodes.length,
          active: nodes.findIndex(n => n.classList.contains('active')),
          scrollTop: Math.round(scroller.scrollTop),
          maxScroll: scroller.scrollHeight - scroller.clientHeight,
          gutter: getComputedStyle(scroller).paddingInlineStart,
        }
      })

    const atBottom = await railState()
    expect(atBottom.maxScroll, 'precondition: the conversation must scroll').toBeGreaterThan(0)
    expect(atBottom.visible).toBe(true)
    expect(atBottom.active, 'at the bottom the LAST message is the reading position').toBe(
      atBottom.nodes - 1,
    )

    // Real user gesture: a programmatic scrollTop is undone by the follow pin.
    const box = (await page.locator('.chat-messages').boundingBox())!
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    for (let i = 0; i < 4; i++) {
      await page.mouse.wheel(0, -400)
      await page.waitForTimeout(200)
    }

    const atTop = await railState()
    expect(atTop.scrollTop, 'precondition: scrolled to the top').toBe(0)
    expect(atTop.active, 'at the top the FIRST message is the reading position').toBe(0)

    // ── Narrow → wide ──────────────────────────────────────────────────────
    // Below the reading measure there is no gutter, so the rail hides; widening
    // must bring it back without remounting the panel.
    await page.setViewportSize({ width: 1000, height: 700 })
    await expect.poll(async () => (await railState()).visible, { timeout: 8000 }).toBe(false)

    await page.setViewportSize({ width: 2200, height: 700 })
    await expect.poll(async () => (await railState()).visible, { timeout: 8000 }).toBe(true)
  })
})

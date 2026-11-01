import { test, expect, type Page } from '../fixtures'

/**
 * The 会议 tab — the parts a browser can drive without media capture.
 *
 * Starting a meeting needs a screen or a microphone, which Playwright cannot
 * provide reliably here (the capture path is covered by the desktop tests and
 * the manual checklist). What IS worth locking down:
 *
 *   - the tab exists on every host and lands on the RECORD LIST, not a form
 *   - create and join are separate pages, reachable from the list footer
 *   - the join page refuses to submit without a nickname
 *
 * The viewer's own nickname gate is covered by live-share.spec.ts.
 */

test.describe('Meeting tab', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()
  })

  test('lands on the record list with both entry points in the footer', async ({ page }) => {
    await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
    await expect(page.locator('[data-action="meeting-join"]')).toBeVisible()
    // The list is the landing view: there is no start form on this page.
    await expect(page.locator('[data-role="meeting-title"]')).toHaveCount(0)
  })

  test('create is its own page, with the name first and the two meeting types', async ({ page }) => {
    await page.locator('[data-action="meeting-create"]').click()

    await expect(page.locator('[data-role="meeting-title"]')).toBeVisible()
    // The choice is voice vs video; what the video is made of is a detail
    // underneath it, so there is no "voice tile" in the source grid any more.
    await expect(page.locator('[data-type="voice"]')).toBeVisible()
    await expect(page.locator('[data-type="video"]')).toBeVisible()
    await expect(page.locator('[data-type="video"]')).toHaveClass(/meeting-type--on/)

    // A picture source is pre-selected (a screen when one can be enumerated),
    // so the only thing missing is the required nickname.
    const start = page.locator('[data-action="meeting-start"]')
    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await expect(start).toBeEnabled()

    await page.locator('[data-type="voice"]').click()
    await expect(page.locator('[data-type="voice"]')).toHaveClass(/meeting-type--on/)
    await expect(start).toBeEnabled()
  })

  test('join requires a nickname before it can submit', async ({ page }) => {
    await page.locator('[data-action="meeting-join"]').click()

    const submit = page.locator('[data-action="meeting-join-submit"]')
    await expect(page.locator('[data-role="meeting-nick"]')).toBeVisible()
    await expect(submit).toBeDisabled()

    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await page.locator('[data-role="meeting-link"]').fill('http://example.test/share/abc')
    await expect(submit).toBeEnabled()
  })
})

/**
 * Swipe inward from a screen edge, the way the phone shell delivers the back
 * gesture.
 *
 * Dispatched as real `TouchEvent`s on the panel rather than with
 * `page.touchscreen` (the desktop projects have no touch device): the app's
 * listener is on `document` and only reads coordinates, so a synthetic event is
 * the same input it sees on a phone. This exercises the whole chain — gesture →
 * `clawbench-back-press` → back handler → view change — which no unit test can
 * cover, because each link is mocked there.
 */
async function edgeSwipe(page: Page, from: 'left' | 'right', target: string): Promise<void> {
  await page.evaluate(
    ({ from, target }) => {
      const el = document.querySelector(target)
      if (!el) throw new Error(`swipe target missing: ${target}`)
      const width = window.innerWidth
      const y = Math.round(window.innerHeight / 2)
      const startX = from === 'left' ? 4 : width - 4
      const endX = from === 'left' ? 180 : width - 180
      const touch = (x: number) =>
        new Touch({ identifier: 1, target: el, clientX: x, clientY: y })
      const fire = (type: string, x: number, live: boolean) => {
        const t = touch(x)
        el.dispatchEvent(
          new TouchEvent(type, {
            bubbles: true,
            cancelable: true,
            touches: live ? [t] : [],
            targetTouches: live ? [t] : [],
            changedTouches: [t],
          }),
        )
      }
      fire('touchstart', startX, true)
      fire('touchend', endX, false)
    },
    { from, target },
  )
}

test.describe('Meeting back gesture', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()
  })

  test('an edge swipe returns from the create page to the list', async ({ page }) => {
    await page.locator('[data-action="meeting-create"]').click()
    await expect(page.locator('[data-role="meeting-title"]')).toBeVisible()

    await edgeSwipe(page, 'left', '.meeting-shell')

    await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
    await expect(page.locator('[data-role="meeting-title"]')).toHaveCount(0)
  })

  test('the right edge works too, and a record returns to the list', async ({ page }) => {
    // Any row: the flow spec leaves records behind, and this file creates one
    // itself in the worst case (an empty list still renders the footer).
    const row = page.locator('.meeting-row').first()
    if (await row.count()) {
      await row.click()
      await expect(page.locator('[data-action="meeting-quote"]')).toBeVisible()
      await edgeSwipe(page, 'right', '.meeting-shell')
      await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
      return
    }

    await page.locator('[data-action="meeting-join"]').click()
    await expect(page.locator('[data-role="meeting-nick"]')).toBeVisible()
    await edgeSwipe(page, 'right', '.meeting-shell')
    await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
  })

  test('the list itself does not consume the gesture', async ({ page }) => {
    // No subpage is open, so the press must fall through to the shell instead
    // of being swallowed here. The shell's "swipe again to exit" hint is the
    // observable proof that it got the press.
    await edgeSwipe(page, 'left', '.meeting-shell')
    await expect(page.locator('.toast-text')).toContainText(/退出|exit/i)
    await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
  })
})

/**
 * The whole publisher flow, with no media at all.
 *
 * A denied microphone does NOT abort a meeting (the audience still gets the
 * picture / the room), so headless Chromium can drive this end to end: create a
 * voice meeting, send a chat line, end it, and read that line back from the
 * record the meeting left behind. The transcript stays empty because no STT
 * provider is configured here — which is itself a case worth covering: the
 * record and the chat must not depend on transcription.
 */
test.describe('Meeting flow', () => {
  test('create a voice meeting, chat, end, and read the chat back from the record', async ({ page }) => {
    await page.goto('/')
    await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()

    await page.locator('[data-action="meeting-create"]').click()
    await page.locator('[data-role="meeting-title"]').fill('E2E 语音会议')
    // The nickname is required since it is the account identity viewers see.
    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await page.locator('[data-type="voice"]').click()
    await page.locator('[data-action="meeting-start"]').click()

    // The stage replaces the create page once the room is up.
    await expect(page.locator('[data-live-tab="basic"]')).toBeVisible({ timeout: 20000 })

    await page.locator('[data-live-tab="chat"]').click()
    const input = page.locator('[data-role="chat-input"]').first()
    await input.fill('这是一条会议聊天')
    await input.press('Enter')

    // Ending goes straight to the record, not to a separate report screen.
    await page.locator('[data-action="panel-stop"]').click()
    await expect(page.locator('[data-action="meeting-quote"]')).toBeVisible({ timeout: 20000 })

    await page.locator('.meeting-tabs [data-tab="chat"]').click()
    await expect(page.locator('.meeting-chat__text')).toContainText('这是一条会议聊天')

    // And the record is in the list, titled as typed.
    await page.locator('[data-action="meeting-back"]').click()
    await expect(page.locator('.meeting-row__title').first()).toHaveText('E2E 语音会议')
  })
})

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

    // Turning the mic off is the host's call — but the form opens with it on,
    // every time, including after a previous meeting muted it.
    const mic = page.locator('[data-role="meeting-mic"]')
    await expect(mic).toBeChecked()
    await mic.uncheck()
    await expect(mic).not.toBeChecked()

    await page.locator('[data-type="voice"]').click()
    await expect(page.locator('[data-type="voice"]')).toHaveClass(/meeting-type--on/)
    await expect(start).toBeEnabled()
    await expect(mic).not.toBeChecked()

    // Re-opening the form starts from the default again (the option is a
    // singleton shared with the stage, so "off" would otherwise stick).
    await page.locator('[data-action="meeting-create-back"]').click()
    await page.locator('[data-action="meeting-create"]').click()
    await expect(page.locator('[data-role="meeting-mic"]')).toBeChecked()
  })

  /**
   * The create form on a phone.
   *
   * The body is a scrolling flex column (`flex: 1; min-height: 0; overflow-y:
   * auto`), so once its content is taller than the panel every child is shrunk
   * before the panel scrolls. Non-replaced children keep their content height;
   * an `<input>` is a replaced element with an automatic minimum of 0, so the
   * two text fields were the only things that collapsed — they rendered at
   * roughly half height. Measured here rather than in jsdom, which has no
   * layout at all; `meetingFormInput.css.test.ts` pins the declaration.
   */
  test('keeps both fields full height on a phone, and describes the picture once', async ({ page }) => {
    await page.locator('[data-action="meeting-create"]').click()
    await expect(page.locator('[data-role="meeting-title"]')).toBeVisible()

    // Resize with the form open: this is the phone case the fields collapsed in.
    await page.setViewportSize({ width: 393, height: 600 })

    const body = page.locator('.meeting-body')
    await expect
      .poll(() => body.evaluate((el) => el.scrollHeight > el.clientHeight), { timeout: 5000 })
      .toBe(true)

    for (const role of ['meeting-nick', 'meeting-title']) {
      const field = page.locator(`[data-role="${role}"]`)
      await expect(field).toBeVisible()
      const box = await field.boundingBox()
      expect(box!.height, `${role} must not be squashed by the flex column`).toBeGreaterThanOrEqual(36)
    }

    // One description for the camera/screen pair, and it follows their
    // combination instead of repeating a line under each toggle.
    const picture = page.locator('[data-role="meeting-picture"]')
    await expect(picture).toHaveCount(1)
    await expect(picture).toHaveClass(/meeting-hint/)
    const screenOnly = await picture.textContent()

    await page.locator('[data-role="meeting-camera"]').check()
    const both = await picture.textContent()
    expect(both).not.toBe(screenOnly)

    await page.locator('[data-role="meeting-screen"]').uncheck()
    const cameraOnly = await picture.textContent()
    expect(cameraOnly).not.toBe(both)
    expect(cameraOnly).not.toBe(screenOnly)

    // "Neither" is the only combination that cannot start a video meeting, so
    // it is the only one that turns into a warning — same line, warning style.
    await page.locator('[data-role="meeting-camera"]').uncheck()
    await expect(picture).toHaveClass(/meeting-warn/)
    expect(await picture.textContent()).not.toBe(cameraOnly)
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
    // The panel is a lazy chunk: without waiting for it, `edgeSwipe`'s
    // querySelector runs against a DOM that has no `.meeting-shell` yet and
    // throws "swipe target missing" (a race, not a behaviour).
    await expect(page.locator('.meeting-shell')).toBeVisible()
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
    await expect(page.locator('.chat-row__text')).toContainText('这是一条会议聊天')

    // And the record is in the list, titled as typed.
    await page.locator('[data-action="meeting-back"]').click()
    await expect(page.locator('.meeting-row__title').first()).toHaveText('E2E 语音会议')
  })
})

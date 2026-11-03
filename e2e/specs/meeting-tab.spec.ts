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

  test('create is its own page, with the name first and the picture as cards', async ({ page }) => {
    await page.locator('[data-action="meeting-create"]').click()

    // The stage is kept mounted behind every other view (`v-show`, so its chat
    // lines and control requests survive a trip to the list), and it renders
    // the SAME picture and device grids. An unscoped `[data-source="__off__"]`
    // therefore matches two elements — the hidden stage's and this form's — and
    // fails strict mode. Scope every grid query to the view on screen.
    const form = page.locator('.meeting-panel')

    await expect(page.locator('[data-role="meeting-title"]')).toBeVisible()
    // Meetings have no type: the picture is a grid of frames with "share
    // nothing" as one of them, and each device axis is its own row of cards
    // (the off state is a card, not a checkbox).
    await expect(page.locator('[data-type="voice"]')).toHaveCount(0)
    await expect(form.locator('[data-source="__off__"]')).toBeVisible()
    const mic = form.locator('[data-role="device-choice"][data-kind="audio"] [data-device="default"]')
    const micOff = form.locator('[data-role="device-choice"][data-kind="audio"] [data-device="__off__"]')

    // A picture source is pre-selected (a screen when one can be enumerated),
    // so the only thing missing is the required nickname.
    const start = page.locator('[data-action="meeting-start"]')
    await expect(start).toBeDisabled()
    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await expect(start).toBeEnabled()

    // Turning the mic off is the host's call — but the form opens with it on,
    // every time, including after a previous meeting muted it.
    await expect(mic).toHaveClass(/device-choice__card--on/)
    await micOff.click()
    await expect(mic).not.toHaveClass(/device-choice__card--on/)

    // No picture is a normal meeting, not an invalid one: the button stays
    // usable and the microphone choice survives.
    await form.locator('[data-source="__off__"]').click()
    await expect(start).toBeEnabled()
    await expect(mic).not.toHaveClass(/device-choice__card--on/)

    // Re-opening the form starts from the default again (the option is a
    // singleton shared with the stage, so "off" would otherwise stick).
    await page.locator('[data-action="meeting-create-back"]').click()
    await page.locator('[data-action="meeting-create"]').click()
    await expect(
      form.locator('[data-role="device-choice"][data-kind="audio"] [data-device="default"]'),
    ).toHaveClass(/device-choice__card--on/)
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
  test('keeps both fields full height on a phone, and narrates nothing', async ({ page }) => {
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

    // Nothing narrates the picture: the card grids show what is being sent.
    // The camera grid is always on screen — 「不共享摄像头」 is an answer even on a
    // machine with no camera — and the only line under it is that reason.
    // Scoped to the form: the hidden stage renders the same grid.
    const form = page.locator('.meeting-panel')
    const cameraGrid = form.locator('[data-role="device-choice"][data-kind="video"]')
    await expect(cameraGrid).toBeVisible()
    await expect(cameraGrid.locator('[data-device="__off__"]')).toBeVisible()

    // "Share no screen" is a normal state, not a warning: the meeting still starts.
    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await form.locator('[data-source="__off__"]').click()
    await expect(page.locator('[data-action="meeting-start"]')).toBeEnabled()
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
 * Dispatched on the panel rather than with `page.touchscreen` (the desktop
 * projects have no touch device). This exercises the whole chain — gesture →
 * `clawbench-back-press` → back handler → view change — which no unit test can
 * cover, because each link is mocked there.
 *
 * The events are plain `Event`s carrying `touches`/`changedTouches`, NOT
 * `TouchEvent`s: the `Touch` constructor is Chromium-only. Firefox has no
 * `Touch` at all ("Touch is not defined") and WebKit exposes the interface but
 * refuses to construct it ("Illegal constructor") — touches can only be created
 * by the browser there. The app's listener only reads
 * `touches[0]`/`changedTouches[0]` coordinates, so this is the same input it
 * sees from a real finger, in every engine.
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
      const point = (x: number) => ({ identifier: 1, target: el, clientX: x, clientY: y })
      const fire = (type: string, x: number, live: boolean) => {
        const ev = new Event(type, { bubbles: true, cancelable: true })
        const list = [point(x)]
        Object.defineProperties(ev, {
          touches: { value: live ? list : [] },
          targetTouches: { value: live ? list : [] },
          changedTouches: { value: list },
        })
        el.dispatchEvent(ev)
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
 * room), so headless Chromium can drive this end to end: create a meeting with
 * no picture and no microphone, send a chat line, end it, and read that line
 * back from the record the meeting left behind. The transcript stays empty
 * because no STT provider is configured here — which is itself a case worth
 * covering: the record and the chat must not depend on transcription.
 */
test.describe('Meeting flow', () => {
  test('create a meeting with no picture, chat, end, and read the chat back', async ({ page }) => {
    // Unique per run: the record is deleted at the end, but a run that was
    // killed mid-way leaves one behind, and a fixed title would then make
    // "the first row" ambiguous for the next run.
    const title = `E2E 语音会议 ${Date.now()}`
    await page.goto('/')
    await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()

    await page.locator('[data-action="meeting-create"]').click()
    await page.locator('[data-role="meeting-title"]').fill(title)
    // The nickname is required since it is the account identity viewers see.
    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    // No meeting type to pick: just turn the picture off (headless Chromium has
    // no screen to capture) and mute the microphone, so the room comes up with
    // no media at all. Scoped to the form: the (hidden) stage renders the same
    // grids.
    const form = page.locator('.meeting-panel')
    await form.locator('[data-source="__off__"]').click()
    await form.locator('[data-role="device-choice"][data-kind="audio"] [data-device="__off__"]').click()
    await page.locator('[data-action="meeting-start"]').click()

    // The stage replaces the create page once the room is up.
    await expect(page.locator('[data-live-tab="basic"]')).toBeVisible({ timeout: 20000 })

    await page.locator('[data-live-tab="chat"]').click()
    const input = page.locator('[data-role="chat-input"]').first()
    await input.fill('这是一条会议聊天')
    await input.press('Enter')

    // Ending is destructive and sits next to 标注画面 in the footer, so it asks
    // first; dismissing the sheet must leave the meeting running.
    await page.locator('[data-action="panel-end"]').click()
    await expect(page.locator('.dlg-box')).toBeVisible({ timeout: 5000 })
    await page.locator('.dlg-cancel').click()
    await expect(page.locator('.dlg-box')).toHaveCount(0)
    await expect(page.locator('[data-live-tab="basic"]')).toBeVisible()

    // Confirming goes straight to the record, not to a separate report screen.
    await page.locator('[data-action="panel-end"]').click()
    await expect(page.locator('.dlg-box')).toBeVisible({ timeout: 5000 })
    await page.locator('.dlg-ok').click()
    await expect(page.locator('[data-action="meeting-quote"]')).toBeVisible({ timeout: 20000 })

    // The artifact bar sits ABOVE the tab strip, so it is on screen from the
    // tab the record opens on (资源) — a running job or a missing artifact must
    // not depend on which tab the reader happens to be looking at.
    const bar = page.locator('.artifact-bar')
    await expect(bar).toBeVisible()
    // On a tab that owns no artifact the bar names its subject.
    await expect(bar.locator('.artifact-bar__kind')).toHaveText(/纪要|Minutes/i)
    // This meeting recorded nothing and transcribed nothing, so there is no
    // source to regenerate from: the bar must say so and keep its button
    // disabled instead of promising a job the server would refuse. (The
    // confirmation sheet itself needs a real source — an ASR provider is not
    // configured in e2e — so its states are covered by
    // MeetingRegenerateSheet.test.ts.)
    await expect(bar.locator('.artifact-bar__text')).toHaveText(/没有可用的来源|no source available/i)
    await expect(bar.locator('[data-action="meeting-artifact-regenerate"]')).toBeDisabled()
    // The minutes tab's empty state explains why, and offers no CTA that could
    // not run. (On 资源 the CTA would be absent for the wrong reason — that tab
    // has no empty state at all — so the assertion belongs on 纪要.)
    await page.locator('.meeting-tabs [data-tab="minutes"]').click()
    await expect(page.locator('[data-action="meeting-generate-minutes"]')).toHaveCount(0)

    await page.locator('.meeting-tabs [data-tab="chat"]').click()
    await expect(page.locator('.chat-row__text')).toContainText('这是一条会议聊天')

    // And the record is in the list, titled as typed.
    await page.locator('[data-action="meeting-back"]').click()
    const row = page.locator('.meeting-row').filter({ hasText: title })
    await expect(row.locator('.meeting-row__title')).toHaveText(title)

    // Leave nothing behind. Without this every run added a record (and its
    // artifact folder) to the e2e project, so the list grew forever and any
    // "first row is the one I just made" assertion depended on run order.
    const token = await row.getAttribute('data-token')
    expect(token, 'the row must carry the meeting token for cleanup').toBeTruthy()

    // The link handed out in advance still works after the meeting: a member
    // opening it now lands on the record — minutes / transcript / proofread /
    // chat — instead of a dead "share ended" page. This is the whole point of
    // keeping the link until the record is deleted.
    const viewer = await page.context().newPage()
    await viewer.goto(`/share/${token}`)
    await expect(viewer.locator('[data-role="meeting-ended"]')).toBeVisible({ timeout: 20000 })
    await expect(viewer.locator('[data-role="record-title"]')).toHaveText(title)
    await viewer.locator('.share-meeting__tab[data-tab="chat"]').click()
    await expect(viewer.locator('.chat-row__text')).toContainText('这是一条会议聊天')
    await viewer.close()

    // 409 means the minutes job is still running and the record cannot be
    // deleted yet — retry instead of treating it as a failure.
    await expect
      .poll(
        async () =>
          (await page.request.delete(`/api/live-share/notes/sessions?token=${token}`)).status(),
        { timeout: 20000, intervals: [500, 1000, 2000] },
      )
      .toBe(200)
  })
})

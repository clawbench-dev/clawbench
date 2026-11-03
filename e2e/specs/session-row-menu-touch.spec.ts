import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * The row's ⋮ button is BOTH the session-menu opener and Sortable's drag handle.
 *
 * Reported on Android: tapping the ⋮ several times does nothing, the row
 * flickers, and only a tap that happens to be perfectly still opens the menu.
 *
 * Cause: with Sortable's default `fallbackTolerance: 0`, the FIRST touchmove of
 * any size promotes the press to a drag. A real finger always drifts 1–3px
 * between touching down and lifting, so nearly every tap became a drag: the row
 * flashed `.sortable-chosen`/`.sortable-ghost` and Sortable's global
 * capture-phase click handler swallowed the click. This spec drives the real
 * library with a deliberately wobbling touch — `touchscreen.tap()` is a
 * pixel-perfect down/up and therefore cannot reproduce it.
 */

/** Dispatch a touch tap whose finger drifts by (dx, dy) before lifting. */
async function wobblingTap(page: any, x: number, y: number, dx: number, dy: number) {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Input.dispatchTouchEvent', {
    type: 'touchStart',
    touchPoints: [{ x, y, id: 1, radiusX: 8, radiusY: 8, force: 1 }],
  })
  for (let i = 1; i <= 3; i++) {
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ x: x + (dx * i) / 3, y: y + (dy * i) / 3, id: 1, radiusX: 8, radiusY: 8, force: 1 }],
    })
  }
  await page.waitForTimeout(60)
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await cdp.detach()
}

test.describe('session row ⋮ menu vs drag handle', () => {
  // The reproduction is Android WebView, and the whole mechanism is touch-only
  // (Sortable takes its fallback path only when native HTML5 drag is
  // unavailable, and the wobble comes from a finger). CI runs every spec in
  // three browsers; without this the spec would be skipped as meaningless in
  // firefox/webkit rather than reported green there.
  test.skip(({ browserName }) => browserName !== 'chromium', 'Android WebView touch reproduction')

  test.use({
    viewport: { width: 412, height: 860 },
    hasTouch: true,
    isMobile: true,
    deviceScaleFactor: 2.625,
    userAgent:
      'Mozilla/5.0 (Linux; Android 13; Pixel 6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36',
  })

  test('a wobbling tap still opens the row menu instead of becoming a drag', async ({ page }) => {
    // Rows are reused rather than seeded on every run: the server caps sessions
    // (SessionMaxCount), and a spec that creates rows each time would slowly
    // starve every other session spec sharing this server. Create one only when
    // the list is empty (a fresh server has none).
    await page.goto('/')
    const chat = new ChatPage(page)
    await chat.openSessionList()

    const drawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(drawer).toBeVisible()

    if ((await drawer.locator('.session-row').count()) === 0) {
      await page.evaluate(async () => {
        await fetch('/api/ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title: 'menu-target' }),
        })
      })
      await chat.openSessionList()
      await expect(drawer.locator('.session-row').first()).toBeVisible({ timeout: 10000 })
    }

    await expect(drawer.locator('.session-more-btn').first()).toBeVisible()

    // The library must actually be bound with a non-zero tolerance — otherwise
    // the tap assertions below would pass or fail for an unrelated reason.
    const tolerance = await page.evaluate(() => {
      const el = [...document.querySelectorAll('.session-rows')].find((e) => {
        const r = e.getBoundingClientRect()
        return r.width > 0 && r.height > 0
      })
      if (!el) return null
      const key = Object.keys(el).find((k) => k.startsWith('Sortable'))
      if (!key) return null
      const inst = (el as any)[key]
      return { tolerance: inst.options.fallbackTolerance, handle: inst.options.handle }
    })
    expect(tolerance, 'the visible session list must have a live Sortable instance').not.toBeNull()
    expect(tolerance!.handle).toBe('.session-more-btn')
    // The prop must actually reach the library. Deliberately NOT asserting a
    // specific value here — the tolerance is a tuning knob, and pinning it
    // would make this spec fail on a legitimate retune instead of on the
    // behaviour it exists to protect (the wobbling taps below).
    expect(typeof tolerance!.tolerance).toBe('number')

    const box = await drawer.locator('.session-more-btn').first().boundingBox()
    expect(box, 'the ⋮ button must have a layout box').not.toBeNull()
    // The drawer is scrollable and the newest row can sit under the tab-bar
    // footer, so a raw centre tap would land on whatever is painted there and
    // the spec would "fail" for a reason unrelated to the gesture under test.
    // Scroll the button into view first, then re-measure.
    await drawer.locator('.session-more-btn').first().scrollIntoViewIfNeeded()
    await expect(drawer.locator('.session-more-btn').first()).toBeInViewport()
    const box2 = await drawer.locator('.session-more-btn').first().boundingBox()
    expect(box2).not.toBeNull()
    const cx = box2!.x + box2!.width / 2
    const cy = box2!.y + box2!.height / 2

    const menu = page.locator('.context-menu.visible')
    const overlay = page.locator('.ctx-overlay')

    // Three wobbling taps in a row: each must open the menu, and dismissing it
    // must leave the list ready for the next one.
    for (let i = 0; i < 3; i++) {
      await expect(menu).toHaveCount(0)
      await wobblingTap(page, cx, cy, i % 2 === 0 ? -2 : 2, -2)
      await expect(menu, `wobbling tap ${i + 1} must open the row menu`).toHaveCount(1)
      await overlay.click()
      await expect(menu).toHaveCount(0)
    }
  })

  test('a deliberate drag still reorders and does not open the menu', async ({ page }) => {
    // Same reason as above: reuse existing rows, never seed. The drag needs at
    // least two rows to have a destination; the login fixture leaves one
    // behind, so create exactly one more only when the list is too short.
    await page.goto('/')
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const drawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(drawer).toBeVisible()

    if ((await drawer.locator('.session-row').count()) < 2) {
      await page.evaluate(async () => {
        await fetch('/api/ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title: 'drag-target' }),
        })
      })
      await chat.openSessionList()
      await expect(drawer.locator('.session-row')).toHaveCount(2, { timeout: 10000 })
    }

    const idsBefore = await drawer.locator('.session-row').evaluateAll((rows) =>
      rows.map((r) => (r as HTMLElement).dataset.sessionId),
    )
    expect(idsBefore.length).toBeGreaterThanOrEqual(2)

    const geo = await page.evaluate(() => {
      const el = [...document.querySelectorAll('.session-rows')].find((e) => {
        const r = e.getBoundingClientRect()
        return r.width > 0 && r.height > 0
      }) as HTMLElement
      const rows = [...el.querySelectorAll('.session-row')] as HTMLElement[]
      const b0 = rows[0].querySelector('.session-more-btn')!.getBoundingClientRect()
      const r1 = rows[1].getBoundingClientRect()
      return {
        sx: b0.left + b0.width / 2,
        sy: b0.top + b0.height / 2,
        // Past the NEXT row's midpoint, not just to it: Sortable only swaps
        // once the dragged element crosses the target's centre. Stopping at the
        // centre leaves the order unchanged and the assertion would fail for a
        // reason that has nothing to do with the fix under test.
        ty: r1.top + r1.height * 0.75,
      }
    })

    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x: geo.sx, y: geo.sy, id: 1, radiusX: 8, radiusY: 8, force: 1 }],
    })
    // Well past the tolerance, in many small steps like a real drag.
    for (let i = 1; i <= 30; i++) {
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [
          { x: geo.sx, y: geo.sy + ((geo.ty - geo.sy) * i) / 30, id: 1, radiusX: 8, radiusY: 8, force: 1 },
        ],
      })
      await page.waitForTimeout(16)
    }
    await page.waitForTimeout(250)
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    await cdp.detach()

    await expect
      .poll(
        async () =>
          drawer.locator('.session-row').evaluateAll((rows) =>
            rows.map((r) => (r as HTMLElement).dataset.sessionId),
          ),
        { message: 'the dragged row must swap places with the one below it' },
      )
      .not.toEqual(idsBefore)

    // A drag is not a click: the menu must stay shut.
    await expect(page.locator('.context-menu.visible')).toHaveCount(0)
  })
})

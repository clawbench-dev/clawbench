import { test, expect } from '../fixtures'
import { TerminalPage } from '../pages/terminal.page'

/**
 * Font-driven UI scaling (Settings → Appearance → 缩放).
 *
 * The whole UI is sized from the root font size: `html { font-size: calc(15px *
 * var(--ui-scale)) }` plus `--dp: calc(1rem / 15)`. That replaced CSS `zoom`,
 * which did not scale the layout viewport (100vw overflowed, dvh grew, media
 * queries lied) and forced every consumer to translate between two coordinate
 * spaces.
 *
 * These are regression guards for the properties that make the replacement
 * correct — the ones that would silently break if a size were written in bare
 * px again, or if the scale stopped reaching a consumer:
 *
 *   1. the root font size really tracks `--ui-scale` (the single source);
 *   2. geometry written as `calc(N * var(--dp))` really grows with it;
 *   3. the viewport does NOT change, so `100vw` never overflows (the bug that
 *      motivated the whole refactor);
 *   4. the wide-screen decision follows the scale (it is made in design px);
 *   5. xterm is canvas-rendered, so it needs the scale pushed into its own
 *      `fontSize` option — the one surface that cannot follow CSS.
 */

/** Apply a scale the way the app does: set the var, then broadcast. */
async function applyScale(page: import('@playwright/test').Page, scale: number) {
  await page.evaluate((s) => {
    localStorage.setItem('clawbench-settings-uiScale', JSON.stringify(s))
    document.documentElement.style.setProperty('--ui-scale', String(s))
    window.dispatchEvent(new CustomEvent('clawbench-ui-scale-change', { detail: { scale: s } }))
  }, scale)
}

// The scale is persisted in localStorage and the viewport is per-test state, so
// a test that leaves either changed would silently alter the next one (the
// terminal test measured nothing when a previous case left the UI at 200%).
test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await applyScale(page, 1)
})

test.describe('Font-driven UI scaling', () => {
  test('root font size tracks --ui-scale', async ({ page }) => {
    // At the design base the root font is exactly 15px.
    await applyScale(page, 1)
    const base = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize))
    expect(base).toBeCloseTo(15, 0)

    await applyScale(page, 1.5)
    const scaled = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize))
    expect(scaled).toBeCloseTo(22.5, 0)

    await applyScale(page, 0.75)
    const smaller = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize))
    expect(smaller).toBeCloseTo(11.25, 0)
  })

  test('--dp geometry grows with the scale', async ({ page }) => {
    // A token expressed in design px must scale. Custom properties resolve
    // lazily, so this measures a real element rather than reading
    // getComputedStyle for the raw `calc()` text (which would compare NaN).
    const read = () =>
      page.evaluate(() => {
        const probe = document.createElement('div')
        probe.style.cssText = 'position:absolute;top:0;left:0;'
        probe.style.width = 'calc(100 * var(--dp))'
        probe.style.borderRadius = 'var(--radius-md)'
        document.body.appendChild(probe)
        const cs = getComputedStyle(probe)
        const out = {
          width: probe.getBoundingClientRect().width,
          radius: parseFloat(cs.borderRadius),
        }
        probe.remove()
        return out
      })

    await applyScale(page, 1)
    const at1 = await read()
    await applyScale(page, 2)
    const at2 = await read()

    expect(at1.width).toBeCloseTo(100, 0)
    expect(at2.width).toBeCloseTo(200, 0)
    // The radius is a design value too, so it must double with the scale.
    expect(at1.radius).toBeCloseTo(10, 0)
    expect(at2.radius).toBeCloseTo(20, 0)
  })

  test('the layout viewport does not change with the scale', async ({ page }) => {
    // This is the property CSS `zoom` broke: it shrank clientWidth while
    // innerWidth stayed put, so `100vw` rendered wider than the visible area.
    // With rem scaling both stay equal and a full-width element fits.
    await applyScale(page, 1)
    const before = await page.evaluate(() => ({
      inner: window.innerWidth,
      client: document.documentElement.clientWidth,
    }))

    await applyScale(page, 2)
    const after = await page.evaluate(() => {
      const probe = document.createElement('div')
      probe.style.width = '100vw'
      probe.style.position = 'absolute'
      probe.style.top = '0'
      probe.style.left = '0'
      document.body.appendChild(probe)
      const w = probe.getBoundingClientRect().width
      probe.remove()
      return {
        inner: window.innerWidth,
        client: document.documentElement.clientWidth,
        vwWidth: w,
      }
    })

    expect(after.inner).toBe(before.inner)
    expect(after.client).toBe(before.client)
    // A 100vw element must not overflow the viewport it is drawn in.
    expect(after.vwWidth).toBeLessThanOrEqual(after.client + 1)
  })

  test('the wide-screen decision is made in design px', async ({ page }) => {
    // computeIsWideScreen divides the window width by the scale, so the SAME
    // window collapses to one column when the UI is scaled up — the panels are
    // physically bigger and genuinely no longer fit side by side. Observed
    // through the class the layout puts on <main>.
    const isWide = () => page.evaluate(() => !!document.querySelector('main.main-content.wide-screen'))

    await page.setViewportSize({ width: 1200, height: 800 })
    await applyScale(page, 1)
    await expect.poll(isWide, { timeout: 5000 }).toBe(true)

    // 1200 / 2 = 600 design px, far below the 960 threshold.
    await applyScale(page, 2)
    await expect.poll(isWide, { timeout: 5000 }).toBe(false)
  })

  test('a corrupt stored scale is clamped instead of blanking the UI', async ({ page }) => {
    // The pre-paint bootstrap in index.html reads the same key; a garbage value
    // must not reach `html { font-size }` as NaN.
    await page.evaluate(() => {
      localStorage.setItem('clawbench-settings-uiScale', JSON.stringify('not-a-number'))
    })
    await page.reload()
    await expect(page.locator('.app-container, .login-page').first()).toBeVisible({ timeout: 20000 })

    const size = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize))
    expect(Number.isFinite(size)).toBe(true)
    expect(size).toBeCloseTo(15, 0)
  })
})

test.describe('Terminal follows the UI scale', () => {
  test.setTimeout(60000)

  test('xterm font size is multiplied by the applied scale', async ({ page }) => {
    // xterm renders to a canvas and cannot read CSS variables, so this is the
    // one surface that needs the factor pushed into a JS option. Without it the
    // terminal stays physically tiny while the rest of the UI grows.
    const terminal = new TerminalPage(page)
    await terminal.switchToTerminal()
    await terminal.expectPanelVisible()
    await terminal.waitForTabConnected(0, 15000)

    /** Row height of the active xterm — a direct proxy for `fontSize`. */
    const measure = () =>
      page.evaluate(() => {
        const rows = document.querySelectorAll('.terminal-container .xterm-rows')
        const visible = Array.from(rows).find((r) => (r as HTMLElement).offsetParent !== null)
        const firstRow = visible?.querySelector('div') as HTMLElement | null
        if (!firstRow) return null
        const h = parseFloat(getComputedStyle(firstRow).height)
        return Number.isFinite(h) && h > 0 ? h : null
      })

    // Wait for xterm to have laid out a row rather than skipping on a race: a
    // skip here would silently drop the only guard on the canvas path.
    await expect.poll(measure, { timeout: 15000 }).not.toBeNull()
    const at1 = (await measure())!
    expect(at1).toBeGreaterThan(0)

    await applyScale(page, 2)
    // The refit runs in a requestAnimationFrame after the event, and xterm
    // re-measures its cell metrics asynchronously.
    await expect
      .poll(async () => (await measure()) ?? 0, { timeout: 5000 })
      .toBeGreaterThan(at1 * 1.6)
  })
})

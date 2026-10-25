import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'

/**
 * Startup UI-scale suggestion.
 *
 * The UI scale is a stored value with a manual "auto fit" button, so a fresh
 * install on a 1440p/4K panel renders everything tiny with nothing pointing at
 * the slider. Startup now measures the screen ONCE and asks before changing
 * anything.
 *
 * The screen height is what the decision reads (`window.screen.height`), and a
 * headless browser always reports the same one — so each case overrides it with
 * `addInitScript`, which is the only way to reach the page before the app
 * measures. `addInitScript` applies to subsequent navigations, hence the
 * explicit reload after registering it.
 *
 * The prompt is a plain confirm through the shared dialog store, so the
 * assertions are on `.dlg-*` — the same surface a user sees.
 */

const DECLINED_KEY = 'clawbench_ui_scale_prompt_declined'
const SCALE_KEY = 'clawbench-settings-uiScale'

/**
 * Reload the app while reporting `height` as the screen height (CSS px).
 *
 * dpr 1 and a window shorter than the screen so `currentScreenHeight` keeps the
 * value as-is on Linux (its native-Wayland normalisation divides only when the
 * window is taller than the report).
 */
async function reloadWithScreenHeight(page: Page, height: number): Promise<void> {
  await page.addInitScript((h) => {
    Object.defineProperty(window.screen, 'height', { value: h, configurable: true })
    Object.defineProperty(window, 'devicePixelRatio', { value: 1, configurable: true })
  }, height)
  await page.reload()
  await expect(page.locator('.app-container, .login-page').first()).toBeVisible({ timeout: 20000 })
}

async function readScale(page: Page): Promise<number> {
  return page.evaluate((key) => {
    const raw = localStorage.getItem(key)
    return raw === null ? 1 : Number(JSON.parse(raw))
  }, SCALE_KEY)
}

test.describe('Startup UI scale suggestion', () => {
  test.beforeEach(async ({ page }) => {
    // Each case starts from "never tuned" — every test gets a fresh browser
    // context, so the only thing worth pinning is the scale itself.
    //
    // Deliberately NOT clearing the declined marker here: an init script runs
    // on every navigation, so clearing it would also wipe the value the
    // "declining is remembered" case just wrote before it reloads.
    await page.addInitScript(() => {
      localStorage.setItem('clawbench-settings-uiScale', JSON.stringify(1))
    })
  })

  test('offers a fit on a 4K screen and applies it on accept', async ({ page }) => {
    await reloadWithScreenHeight(page, 2160) // 2160 / 1080 = 2.0

    const dialog = page.locator('.dlg-overlay')
    await expect(dialog).toBeVisible({ timeout: 20000 })
    await expect(dialog).toContainText('200')

    await page.locator('.dlg-ok').click()
    await expect(dialog).toBeHidden()

    // Applied immediately (root font size) AND persisted for the next launch.
    await expect
      .poll(() => page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize)), { timeout: 5000 })
      .toBeCloseTo(30, 0)
    expect(await readScale(page)).toBe(2)
  })

  test('leaves the scale alone when the user keeps the current value', async ({ page }) => {
    await reloadWithScreenHeight(page, 2160)

    const dialog = page.locator('.dlg-overlay')
    await expect(dialog).toBeVisible({ timeout: 20000 })
    await page.locator('.dlg-cancel').click()
    await expect(dialog).toBeHidden()

    expect(await readScale(page)).toBe(1)
    const fontPx = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize))
    expect(fontPx).toBeCloseTo(15, 0)

    // Declining is remembered, so the next launch does not nag on the same screen.
    const declined = await page.evaluate((key) => localStorage.getItem(key), DECLINED_KEY)
    expect(declined).not.toBeNull()
    expect(Number(JSON.parse(declined!))).toBe(2)

    await page.reload()
    await expect(page.locator('.app-container, .login-page').first()).toBeVisible({ timeout: 20000 })
    await expect(page.locator('.dlg-overlay')).toBeHidden()
  })

  test('never asks on a 1080p screen', async ({ page }) => {
    await reloadWithScreenHeight(page, 1080)

    // Give the post-startup prompt a moment to appear; it must not.
    await page.waitForTimeout(1500)
    await expect(page.locator('.dlg-overlay')).toBeHidden()
    expect(await readScale(page)).toBe(1)
  })

  test('never asks when the difference is under the threshold', async ({ page }) => {
    // 1200p fits 1.11 against a stored 1.0 — an 11% bump is not worth an
    // interruption on a screen that already reads fine.
    await reloadWithScreenHeight(page, 1200)

    await page.waitForTimeout(1500)
    await expect(page.locator('.dlg-overlay')).toBeHidden()
    expect(await readScale(page)).toBe(1)
  })

  test('waits for the first-run welcome panel instead of stacking on it', async ({ page }) => {
    // The auth fixture dismisses the welcome panel for every test; re-enable it
    // here. Init scripts run in registration order, so this one lands after the
    // fixture's and wins.
    await page.addInitScript(() => {
      localStorage.removeItem('clawbench_welcome_dismissed')
    })
    await reloadWithScreenHeight(page, 2160)

    // The welcome panel owns the screen first, and the scale prompt must NOT be
    // behind/over it — two full-screen modals at once is the bug being avoided.
    const welcome = page.locator('.welcome-overlay')
    await expect(welcome).toBeVisible({ timeout: 20000 })
    await expect(page.locator('.dlg-overlay')).toBeHidden()

    await page.locator('.welcome-footer .btn-ok').click()
    await expect(welcome).toBeHidden()

    // Closing it releases the deferred offer.
    await expect(page.locator('.dlg-overlay')).toBeVisible({ timeout: 10000 })
  })
})

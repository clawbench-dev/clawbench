import { test, expect } from '../fixtures'

/**
 * Factory-theme bootstrap (web).
 *
 * The inline script in `index.html` is the sole authority for the out-of-box
 * theme: a browser that has never stored a theme is a fresh install, and the
 * script persists `gruvbox-dark` right there (before CSS loads). This replaced
 * a server-side `config.first_run` flag, which was process-scoped and flipped
 * to false as soon as InitDB created the database file — so any restart
 * (docker restart, upgrade, reboot) silently dropped every new install back to
 * the system theme, and the first paint always flashed the system theme first.
 *
 * These assertions run against the real server + real index.html, so they catch
 * both a regression in the script and a re-introduction of the server flag.
 */

const STORE_KEY = 'clawbench-settings-theme'

test.describe('factory theme (fresh install)', () => {
  test('persists gruvbox-dark when no theme has ever been stored', async ({ page }) => {
    // Simulate a browser that has never chosen a theme. Other keys (e.g. the
    // welcome-dismissed flag) may legitimately be present — the script keys off
    // the theme entry specifically, not an empty localStorage.
    await page.evaluate((key) => {
      localStorage.removeItem(key)
      localStorage.removeItem('theme')
      localStorage.removeItem('clawbench-fresh-theme-guess')
    }, STORE_KEY)

    await page.reload()

    // The theme is applied before CSS, so it is set by the time the app settles.
    await expect.poll(() => page.evaluate(() => document.documentElement.getAttribute('data-theme')))
      .toBe('gruvbox-dark')
    expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme-base'))).toBe('dark')
    // And it is PERSISTED, not merely applied to the DOM — this is what makes
    // it survive a restart of the server.
    expect(await page.evaluate((key) => localStorage.getItem(key), STORE_KEY))
      .toBe(JSON.stringify('gruvbox-dark'))
  })

  test('the factory choice survives a reload', async ({ page }) => {
    await page.evaluate((key) => {
      localStorage.removeItem(key)
      localStorage.removeItem('theme')
    }, STORE_KEY)
    await page.reload()
    await expect.poll(() => page.evaluate(() => document.documentElement.getAttribute('data-theme')))
      .toBe('gruvbox-dark')

    await page.reload()
    expect(await page.evaluate(() => document.documentElement.getAttribute('data-theme'))).toBe('gruvbox-dark')
  })

  test('does not override a theme the user already picked', async ({ page }) => {
    await page.evaluate((key) => {
      localStorage.setItem(key, JSON.stringify('dracula'))
    }, STORE_KEY)
    await page.reload()

    await expect.poll(() => page.evaluate(() => document.documentElement.getAttribute('data-theme')))
      .toBe('dracula')

    // Restore the factory default so later specs are not affected.
    await page.evaluate((key) => {
      localStorage.setItem(key, JSON.stringify('gruvbox-dark'))
    }, STORE_KEY)
  })
})

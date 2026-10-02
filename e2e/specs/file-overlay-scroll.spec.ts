import { test, expect, type Page } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * File overlay: scroll-to-line and nav stack.
 *
 * The file viewer renders source through CodeMirror (CodeMirrorViewer.vue), not
 * the old sliced `<pre>`. Two contracts follow from that and both are asserted
 * here because the old ones silently stopped matching:
 *
 *   - DOM: lines are `.cm-line` inside `.cm-scroller`. The jump highlight is a
 *     CodeMirror line *decoration* (`line-flash`), not a `.code-line[data-line]`
 *     attribute — CodeMirror virtualizes its DOM, so there is no per-line
 *     `data-line` to query.
 *   - Events: the request is `cm-scroll-to-line` and it is acknowledged with
 *     `cm-scroll-to-line-handled` carrying the same `requestId`. Dispatching the
 *     old `scroll-to-line` is a no-op.
 */
test.describe('File overlay: scroll-to-line and nav stack', () => {
  let fm: FileManagerPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    fm = new FileManagerPage(page)
    nav = new NavigationPage(page)

    // Navigate to the file manager tab
    await nav.switchToFileManager()
    await fm.waitForContent(15000)
  })

  /** Open go.mod in the viewer and wait for CodeMirror to render. */
  async function openGoMod(page: Page): Promise<void> {
    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    await expect(goMod).toBeVisible({ timeout: 10000 })
    await goMod.dblclick()

    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
    // CodeMirror renders into .cm-scroller / .cm-line once the async component
    // loads and the editor measures itself.
    await expect(page.locator('.cm-scroller')).toBeVisible({ timeout: 15000 })
    await expect(page.locator('.cm-line').first()).toBeVisible({ timeout: 15000 })
  }

  /**
   * Dispatch a scroll-to-line request and wait for CodeMirror to acknowledge it.
   * The ack is the deterministic completion signal — the flash itself is a
   * timed decoration, so waiting on it directly would race the 700ms timer.
   */
  async function requestScrollToLine(
    page: Page,
    detail: { line: number; lineEnd?: number; lineRanges?: string },
  ): Promise<void> {
    const requestId = Date.now()
    const handled = page.evaluate(
      (id) => new Promise<boolean>((resolve) => {
        const onHandled = (e: Event) => {
          const d = (e as CustomEvent).detail
          if (d?.requestId !== id) return
          window.removeEventListener('cm-scroll-to-line-handled', onHandled)
          resolve(true)
        }
        window.addEventListener('cm-scroll-to-line-handled', onHandled)
        // Give up after the viewer's own layout wait so a missed ack fails the
        // test instead of hanging forever.
        setTimeout(() => resolve(false), 10000)
      }),
      requestId,
    )
    await page.evaluate(
      ({ id, d }) => {
        window.dispatchEvent(new CustomEvent('cm-scroll-to-line', { detail: { ...d, requestId: id } }))
      },
      { id: requestId, d: detail },
    )
    expect(await handled, 'CodeMirror never acknowledged cm-scroll-to-line').toBe(true)
  }

  test('scroll-to-line scrolls file viewer to the target line and flashes it', async ({ page }) => {
    await openGoMod(page)

    await requestScrollToLine(page, { line: 3 })

    // The target line gets the flash decoration class. CodeMirror renders it on
    // a `.cm-line` element.
    await expect(page.locator('.cm-line.line-flash').first()).toBeVisible({ timeout: 5000 })
  })

  test('scroll-to-line with range highlights multiple lines', async ({ page }) => {
    await openGoMod(page)

    await requestScrollToLine(page, { line: 1, lineEnd: 3 })

    // Every line in the range carries the flash decoration.
    await expect
      .poll(async () => page.locator('.cm-line.line-flash').count(), { timeout: 5000 })
      .toBeGreaterThanOrEqual(3)
  })

  test('nav stack does not duplicate same file on consecutive opens', async ({ page }) => {
    await openGoMod(page)

    // The overlay covers the file list, so to open the same file a second time
    // through the app's own path we close it first and re-open from the list.
    // (Dispatching `open-file-overlay` directly would skip the content fetch
    // and land on an empty viewer — see FileManagerContent.vue.)
    await page.locator('.overlay-close-btn').click()
    await expect(page.locator('.file-overlay')).not.toBeVisible({ timeout: 10000 })

    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    await expect(goMod).toBeVisible({ timeout: 10000 })
    await goMod.dblclick()
    await expect(page.locator('.cm-scroller')).toBeVisible({ timeout: 15000 })

    // Closing the overlay clears the file nav stack, so the fresh open is the
    // FIRST entry: there is no in-file back target. The back button may still
    // render (it doubles as "return to the file list"), so assert on its label
    // rather than its presence — a duplicate stack entry would make it read
    // "Back to go.mod" (file.nav.backToFile).
    const backBtn = page.locator('.file-header-back-btn')
    if (await backBtn.count() > 0) {
      await expect(backBtn).not.toHaveAttribute('title', /Back to go\.mod|返回 go\.mod/)
    }
  })

  test('cancel-scroll-restore prevents scroll position override', async ({ page }) => {
    await openGoMod(page)

    // Scroll to the bottom first so there is a saved position to restore.
    await page.locator('.cm-scroller').evaluate((el) => { el.scrollTop = el.scrollHeight })
    await expect
      .poll(async () => page.locator('.cm-scroller').evaluate((el) => el.scrollTop), { timeout: 5000 })
      .toBeGreaterThan(0)

    // Trigger the jump; it must not be overridden by the saved scroll restore.
    await requestScrollToLine(page, { line: 1 })

    // Line 1 is back in view (the scroller returned near the top) and flashed.
    await expect(page.locator('.cm-line.line-flash').first()).toBeVisible({ timeout: 5000 })
    await expect
      .poll(async () => page.locator('.cm-scroller').evaluate((el) => el.scrollTop), { timeout: 5000 })
      .toBeLessThan(200)
  })
})

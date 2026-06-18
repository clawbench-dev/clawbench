import { test, expect } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

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

  test('scroll-to-line scrolls file viewer to the target line and flashes it', async ({ page }) => {
    // Open a known file by double-clicking
    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    if (!(await goMod.isVisible().catch(() => false))) {
      test.skip()
      return
    }
    await goMod.dblclick()

    // Wait for file overlay to appear
    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
    // Wait for code lines to render
    await expect(page.locator('.code-line').first()).toBeVisible({ timeout: 10000 })

    // Dispatch scroll-to-line event via page.evaluate (simulates clicking an
    // annotated file path like `go.mod:3`)
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('scroll-to-line', { detail: { line: 3 } }))
    })

    // The target line should get the line-flash class
    await expect(page.locator('.code-line[data-line="3"].line-flash')).toBeVisible({ timeout: 5000 })
  })

  test('scroll-to-line with range highlights multiple lines', async ({ page }) => {
    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    if (!(await goMod.isVisible().catch(() => false))) {
      test.skip()
      return
    }
    await goMod.dblclick()

    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.code-line').first()).toBeVisible({ timeout: 10000 })

    // Dispatch scroll-to-line with range
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('scroll-to-line', { detail: { line: 1, lineEnd: 3 } }))
    })

    // Both lines should get the flash class
    await expect(page.locator('.code-line[data-line="1"].line-flash')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.code-line[data-line="3"].line-flash')).toBeVisible({ timeout: 5000 })
  })

  test('nav stack does not duplicate same file on consecutive opens', async ({ page }) => {
    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    if (!(await goMod.isVisible().catch(() => false))) {
      test.skip()
      return
    }
    await goMod.dblclick()

    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.code-line').first()).toBeVisible({ timeout: 10000 })

    // Open the same file again via open-file-overlay event
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('open-file-overlay', { detail: { path: 'go.mod' } }))
    })

    // Give Vue a tick to process
    await page.waitForTimeout(200)

    // Check nav stack length — should still be 1 (no duplicate)
    const stackLength = await page.evaluate(() => {
      // Access the internal nav stack via the composable's module state
      // We verify indirectly: canGoBack should be false (only one entry)
      const goBackBtn = document.querySelector('.file-header-back-btn')
      return goBackBtn ? true : false
    })

    // The back button should NOT be visible (only one entry in stack)
    // Note: if there were a duplicate, canGoBack would be true and the back
    // button would appear. This is an indirect but reliable check.
    expect(stackLength).toBe(false)
  })

  test('cancel-scroll-restore prevents scroll position override', async ({ page }) => {
    const goMod = page.locator('.file-item, .grid-item', { hasText: 'go.mod' }).first()
    if (!(await goMod.isVisible().catch(() => false))) {
      test.skip()
      return
    }
    await goMod.dblclick()

    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.code-line').first()).toBeVisible({ timeout: 10000 })

    // Scroll to bottom first (so there's a saved scroll position)
    const scrollContainer = page.locator('.raw-content-pre')
    if ((await scrollContainer.count()) > 0) {
      await scrollContainer.evaluate(el => { el.scrollTop = el.scrollHeight })
      await page.waitForTimeout(200)
    }

    // Now trigger scroll-to-line and verify it scrolls to the target
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('scroll-to-line', { detail: { line: 1 } }))
    })

    // Line 1 should be visible and flashed (not overridden by scroll restore)
    await expect(page.locator('.code-line[data-line="1"].line-flash')).toBeVisible({ timeout: 5000 })
  })
})

import { test, expect, type Page } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Rendered HTML preview: in-document find (Ctrl+F) and text-selection quoting.
 *
 * The preview is a same-origin sandboxed iframe holding its own document, which
 * makes both features non-obvious:
 *
 *   - the host document's `selectionchange` never fires for a selection inside
 *     the frame, so the shared quote bar needs a bridge attached to the FRAME's
 *     document (utils/iframeSelectionQuote.ts);
 *   - the host's TreeWalker cannot reach the frame's text nodes and a host
 *     Range cannot wrap them, so find has to run inside that document
 *     (PreviewSearchBar's `iframe` prop + utils/documentSearch.ts).
 *
 * Both are asserted end-to-end here because neither is visible to the unit
 * suites in the form the user experiences it (real iframe navigation, real
 * keyboard shortcut routing through App.vue).
 */
const HTML_FILE = 'e2e-html-preview.html'
const HTML_BODY = 'clawbench preview needle alpha needle beta'

test.describe('HTML preview: find and quote', () => {
  let fm: FileManagerPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    fm = new FileManagerPage(page)
    nav = new NavigationPage(page)

    const fs = await import('node:fs')
    const path = await import('node:path')
    fs.writeFileSync(
      path.join(process.cwd(), HTML_FILE),
      // The target paragraph sits WELL below the fold. That matters for the
      // quote-bar position assertion: with the text near the top, the bar's
      // pinned-top fallback and its correct floating position would be
      // indistinguishable (the defect would pass).
      `<!doctype html><html><head><title>preview</title></head><body>
         <div style="height:900px">spacer</div>
         <h1>${HTML_BODY}</h1>
         <p id="second">another needle here</p>
       </body></html>`,
    )

    await nav.switchToFileManager()
    await fm.waitForContent(15000)
    // The listing was fetched when the page loaded (before this file existed),
    // so refresh before looking for it. The toolbar refresh button carries the
    // localized "Refresh" title (nav.refresh).
    await page.locator('.toolbar-btn[title="Refresh"], .toolbar-btn[title="刷新"]').first().click()
    await expect(fm.getFileItem(HTML_FILE)).toBeVisible({ timeout: 15000 })
  })

  test.afterEach(async () => {
    const fs = await import('node:fs')
    const path = await import('node:path')
    fs.rmSync(path.join(process.cwd(), HTML_FILE), { force: true })
  })

  /** Open the seeded HTML file and wait for the previewed document to load. */
  async function openHtmlPreview(page: Page) {
    await fm.openFile(HTML_FILE)
    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })

    const frame = page.frameLocator('iframe.html-preview-iframe')
    await expect(frame.locator('h1')).toContainText('clawbench preview', { timeout: 15000 })
    return frame
  }

  test('Ctrl+F opens the inline find bar and highlights matches inside the preview document', async ({ page }) => {
    const frame = await openHtmlPreview(page)

    // Focus the left pane so App routes Ctrl+F to the file view rather than the
    // chat pane (wide-screen layout is focus-aware).
    await page.locator('iframe.html-preview-iframe').click()
    await page.keyboard.press('Control+f')

    const bar = page.locator('.search-bar.is-open')
    await expect(bar).toBeVisible({ timeout: 5000 })

    await bar.locator('input[name=search]').fill('needle')

    // The counter proves the walk ran over the FRAME's document (the host has
    // no such text), and the marks prove the wrapping landed there.
    await expect(bar.locator('.cm-search-match-info')).toHaveText('1/3', { timeout: 5000 })
    await expect(frame.locator('mark.md-search-match')).toHaveCount(3)

    // Navigating moves the active match: the second mark becomes active.
    await bar.locator('button[name=next]').click()
    await expect(bar.locator('.cm-search-match-info')).toHaveText('2/3')

    // Closing removes every mark and restores the document text.
    await bar.locator('button[name=close]').click()
    await expect(page.locator('.search-bar.is-open')).toHaveCount(0)
    await expect(frame.locator('mark.md-search-match')).toHaveCount(0)
    await expect(frame.locator('h1')).toContainText(HTML_BODY)
  })

  test('selecting text inside the preview floats the bar next to the selection', async ({ page }) => {
    const frame = await openHtmlPreview(page)

    // A real selection inside the frame's document — this is what the bridge
    // listens for; the host never sees it.
    const heading = frame.locator('h1')
    // Bring it into view first: `selectText` alone does not reliably scroll a
    // frame's document, and the position assertion below needs the text on
    // screen. (Playwright's toBeInViewport does not evaluate inside frames.)
    await heading.scrollIntoViewIfNeeded()
    await heading.selectText()

    const quoteBar = page.locator('.quote-question-bar')
    await expect(quoteBar).toBeVisible({ timeout: 5000 })

    // The bar must float at the selection, NOT at its pinned top position.
    // The selection rect is reported by the FRAME's viewport, so this is the
    // assertion that catches a missing frame→host coordinate mapping (the
    // reported defect: the menu appeared at the top of the page).
    const headingBox = await heading.boundingBox()
    const barBox = await quoteBar.boundingBox()
    expect(headingBox).not.toBeNull()
    expect(barBox).not.toBeNull()
    const headingCenterY = headingBox!.y + headingBox!.height / 2
    const barCenterY = barBox!.y + barBox!.height / 2
    // Within a couple of line heights of the text it belongs to.
    expect(Math.abs(barCenterY - headingCenterY)).toBeLessThan(80)
    // ...and NOT at the pinned-top position. Without the frame→host mapping the
    // bar sits near the viewport top, which this catches even though the
    // distance check above could pass for a selection near the top.
    expect(barBox!.y).toBeGreaterThan(headingBox!.y - 100)

    // The bar carries the selected text (the collapsed toolbar has no preview,
    // so assert on the staged quote through the expanded view).
    await quoteBar.locator('.quote-bar-action').filter({ hasText: /引用|Quote/ }).click()
    await expect(quoteBar).toBeVisible()

    // The card lands in the chat input as a quote referencing the HTML file.
    const quoteCard = page.locator('.chat-file-attachment.attachment-quote').first()
    await expect(quoteCard).toBeVisible({ timeout: 5000 })
    await expect(quoteCard).toContainText(HTML_FILE)
  })
})

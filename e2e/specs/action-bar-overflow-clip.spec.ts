import { test, expect } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * A toolbar that overflows must FOLD, not clip.
 *
 * The file viewer's header row is right-aligned (`align="end"`), so a row that
 * is one button too wide spills out of its START edge — where `overflow:
 * hidden` cuts the first button in half. The browser does not even report it:
 * with `justify-content: flex-end` the element still says
 * `scrollWidth === clientWidth`, so nothing looks wrong to the code that
 * decides what fits.
 *
 * It happened for real: opening a file with a long name in a narrow pane left
 * the refresh glyph sliced in half, because the fold decision had been made
 * while the bar was a few pixels wider than it paints.
 *
 * This spec opens a file whose name is long enough to push the header over the
 * edge and asserts the invariant directly: the first visible button of the row
 * must lie INSIDE the row.
 */
type Row = { barLeft: number; barWidth: number; firstLeft: number | null; visible: number; folded: number }

/** The file viewer's header row, measured as the browser lays it out. */
async function readHeaderRow(page: import('@playwright/test').Page): Promise<Row | null> {
  return page.evaluate(() => {
    const bar = document.querySelector('.file-header-bar .ab') as HTMLElement | null
    if (!bar) return null
    const rect = bar.getBoundingClientRect()
    const kids = Array.from(bar.children) as HTMLElement[]
    const buttons = kids.filter(k => k.tagName === 'BUTTON')
    const visible = buttons.filter(k => k.style.display !== 'none')
    const first = visible.find(k => !k.hasAttribute('data-ab-more'))
    const fr = first?.getBoundingClientRect()
    return {
      barLeft: rect.left,
      barWidth: rect.width,
      firstLeft: fr ? fr.left : null,
      visible: visible.length,
      folded: buttons.filter(k => k.style.display === 'none').length,
    }
  })
}

test.describe('ActionBar: an overflowing row folds instead of clipping', () => {
  test('the first button of the file header is never cut off', async ({ page }) => {
    // Wide screen (the header then also carries the chat-pane toggle) with a
    // left pane narrow enough that the row cannot hold every button.
    await page.setViewportSize({ width: 1450, height: 500 })
    const nav = new NavigationPage(page)
    const fm = new FileManagerPage(page)
    await nav.switchToFileManager()
    await fm.waitForContent(15000)

    // A file whose NAME is long enough to eat the row's room.
    await fm.openDirectory('web')
    await fm.openDirectory('src')
    await fm.openDirectory('components')
    await fm.openDirectory('common')
    await fm.openDirectory('__tests__')
    const longName = 'actionBarVisualContract.test.ts'
    await fm.openFile(longName)

    await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 15000 })
    // Let the layout settle (fonts, the pane's own open animation).
    await page.waitForTimeout(1200)

    const row = await readHeaderRow(page)
    expect(row, 'the file header row must be on screen').not.toBeNull()
    expect(row!.firstLeft, 'the row must have a visible button').not.toBeNull()
    expect(
      row!.firstLeft!,
      'the first button of the row must lie inside the row — a button that starts left of the row is cut in half',
    ).toBeGreaterThanOrEqual(row!.barLeft - 1)
    // ...and it must get there by FOLDING (the overflow affordance), not by
    // dropping buttons it could have kept: a safety net that over-folds is just
    // a different way of hiding the toolbar.
    expect(row!.folded, 'the row must fold the buttons that do not fit').toBeGreaterThan(0)
    expect(row!.visible, 'the fold must keep the buttons that do fit').toBeGreaterThanOrEqual(4)
  })
})

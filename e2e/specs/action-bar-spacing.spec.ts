import { test, expect } from '../fixtures'

/**
 * ActionBar spacing, measured in a real browser.
 *
 * The bar owns the gap between its buttons (`gap`, from the row token). A host
 * that ALSO gives a button a left/right margin makes the two stack, and the row
 * then reads unevenly: the session-list header measured 14px between buttons
 * instead of 8, and it looked worse once the labels appeared (a labelled button
 * also gains 8px of side padding, so the gap between two words became 30px).
 *
 * The source-level guard in actionBarVisualContract.test.ts catches the rule;
 * this one catches what the browser actually lays out, including margins coming
 * from a shared class or an inherited style.
 */

type Row = { bar: string; gap: number; gaps: number[]; margins: number[] }

/** Every visible ActionBar on screen, with its button spacing. */
async function readBars(page: import('@playwright/test').Page): Promise<Row[]> {
  return page.evaluate(() => {
    const out: Array<{ bar: string; gap: number; gaps: number[]; margins: number[] }> = []
    for (const bar of Array.from(document.querySelectorAll('.ab')) as HTMLElement[]) {
      if (bar.clientWidth === 0) continue
      const gap = parseFloat(getComputedStyle(bar).columnGap) || 0
      const kids = (Array.from(bar.children) as HTMLElement[]).filter(
        (k) => k.tagName === 'BUTTON' || k.querySelector(':scope > button'),
      )
      if (kids.length < 2) continue
      const btns = kids.map((k) => (k.tagName === 'BUTTON' ? k : (k.querySelector('button') as HTMLElement)))
      const rects = btns.map((b) => b.getBoundingClientRect())
      const gaps: number[] = []
      for (let i = 1; i < rects.length; i++) gaps.push(Math.round(rects[i].left - rects[i - 1].right))
      const margins = btns.map((b) => {
        const s = getComputedStyle(b)
        return (parseFloat(s.marginLeft) || 0) + (parseFloat(s.marginRight) || 0)
      })
      out.push({ bar: bar.className, gap: Math.round(gap), gaps, margins })
    }
    return out
  })
}

const PANELS: Array<{ name: string; tab: string | null }> = [
  { name: 'session list', tab: null },
  { name: 'files', tab: 'browse' },
  { name: 'git history', tab: 'history' },
  { name: 'meetings', tab: 'live' },
  { name: 'tasks', tab: 'tasks' },
  { name: 'forge', tab: 'forge' },
  { name: 'notifications', tab: 'notifications' },
]

test.describe('ActionBar spacing', () => {
  test.beforeEach(async ({ page }) => {
    // Wide enough for the whole dock: a narrow window folds the file/git tabs
    // into an overflow menu, and this spec is about spacing, not about that menu.
    await page.setViewportSize({ width: 1920, height: 1000 })
    await page.goto('/')
    // The app boots into the session list; give it time to settle before
    // switching panels (the dock is only rendered once the layout is ready).
    await expect(page.locator('.app-container')).toBeVisible({ timeout: 20000 })
  })

  for (const panel of PANELS) {
    test(`keeps even spacing between buttons in ${panel.name}`, async ({ page }) => {
      if (panel.tab) {
        const tab = page.locator(`.dock-btn[data-tab="${panel.tab}"]`).filter({ visible: true }).first()
        if (!(await tab.count())) test.skip(true, `${panel.tab} tab is not offered in this build/project`)
        await tab.click()
        await page.waitForTimeout(1200)
      }

      const bars = await readBars(page)
      expect(bars.length, `no visible ActionBar in ${panel.name}`).toBeGreaterThan(0)

      for (const row of bars) {
        // A button carrying its own margin is the defect: the gap already
        // separates them, so any extra margin shows up as an uneven row.
        expect(
          row.margins,
          `${panel.name} / ${row.bar}: a button adds its own horizontal margin; ` +
            `the bar's ${row.gap}px gap is the only spacing it should need`,
        ).toEqual(row.margins.map(() => 0))

        // …and every measured gap equals the row's declared gap. The forge
        // header legitimately ends with a title-then-buttons split, so a row
        // whose FIRST child is not a button is skipped for this check.
        const uniform = row.gaps.every((g) => g === row.gap)
        if (!uniform) {
          expect(
            row.gaps,
            `${panel.name} / ${row.bar}: uneven gaps (declared ${row.gap}px). ` +
              `A title/other element inside the bar shifts the first gap — that is fine, ` +
              `but two buttons apart must be exactly the declared gap.`,
          ).toEqual(row.gaps.map((g, i) => (i === 0 ? g : row.gap)))
        }
      }
    })
  }
})

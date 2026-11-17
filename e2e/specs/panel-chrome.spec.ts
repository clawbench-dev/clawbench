import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'

/**
 * Guard: the left panel's window controls must not be drawn over panel content.
 *
 * In the wide layout App.vue overlays two window controls (minimise /
 * maximise) on whatever header the current view draws — that is how one row
 * serves both the app chrome and a drill-down page's own header. The cluster is
 * a fixed 52dp strip at the panel's right edge, translucent and z-indexed above
 * the panel, so every header row has to reserve that width with
 * `padding-right` — and every new header has to be added to that list by hand.
 * The forge issue/PR (and pipeline) drill-down missed it: its own quote /
 * open-in-browser buttons rendered UNDER the cluster, so the minimise glyph (a
 * single line) showed through next to the chat bubble and the cluster's tint
 * cut the button in half. The user reported it as "buttons overlapping".
 *
 * HOW THIS CATCHES IT. Two cheaper probes were tried against a build with the
 * fix removed and both PASSED, i.e. neither can see the defect:
 *
 *  - hit-testing (`elementsFromPoint`): the cluster is a real element on top,
 *    so the point always resolves to the cluster, fixed or broken;
 *  - pixel diffing: the cluster's tint is translucent, so the header's own
 *    surface shows through it either way and the colours barely move.
 *
 * What is unambiguous is geometry — see `contentInControls()` below.
 */

const CONTROLS = '.panel-window-controls'
const PANEL_HEAD = '.panel-head'

/**
 * Panel content drawn underneath the window-control cluster.
 *
 * The cluster is a fixed 52dp strip pinned to the panel's right edge and
 * z-index'ed above the panel, so any control of the panel's own that ends up
 * inside that strip is (partly) painted over.
 *
 * A control counts as covered only when a SIDE of its box lies inside the strip
 * — both corners of that side — so a button that merely ends at the strip's
 * left edge does not trip it.
 *
 * Two cheaper detectors were tried against a build with the fix removed and
 * both PASSED, i.e. neither can see this defect, which is why this is geometry:
 * hit-testing always resolves to the cluster (it is a real element on top), and
 * pixel comparison is defeated by the cluster's translucency (the header's own
 * surface shows through it either way).
 */
async function contentInControls(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const cluster = document.querySelector('.panel-window-controls')
    if (!cluster) return []
    const c = cluster.getBoundingClientRect()
    const coveredSides = (r: DOMRect): string[] => {
      const inside = (x: number, y: number) => x > c.left && x < c.right && y > c.top && y < c.bottom
      const sides: string[] = []
      if (inside(r.right, r.top) && inside(r.right, r.bottom)) sides.push('right')
      if (inside(r.left, r.top) && inside(r.left, r.bottom)) sides.push('left')
      if (inside(r.left, r.top) && inside(r.right, r.top)) sides.push('top')
      if (inside(r.left, r.bottom) && inside(r.right, r.bottom)) sides.push('bottom')
      return sides
    }
    const out: string[] = []
    for (const el of Array.from(document.querySelectorAll('.col-left .tab-panel *'))) {
      if (el.closest('.panel-head')) continue
      const control = el.closest('button, a, input, select, textarea')
      if (!control || !control.closest('.col-left .tab-panel')) continue
      const r = control.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      const sides = coveredSides(r)
      if (sides.length === 0) continue
      const label = (control.getAttribute('title') || control.getAttribute('aria-label') || control.textContent || '')
        .trim()
        .slice(0, 24)
      out.push(
        `${control.tagName.toLowerCase()}.${control.className} [${label}] ${sides.join('+')} side inside controls ` +
          `(control.right=${Math.round(r.right)}, controls.left=${Math.round(c.left)})`,
      )
    }
    return Array.from(new Set(out))
  })
}

async function expectChromeClear(page: Page, where: string): Promise<void> {
  await expectClusterVisible(page, where)
  const covered = await contentInControls(page)
  expect(covered, `${where}: 这些内容伸进了面板窗口按钮所占的位置，说明被盖住了`).toEqual([])
}

/**
 * Enter the wide two-column layout with the left pane expanded.
 *
 * Left-pane collapsed state is persisted, so a fresh context starts collapsed
 * (the default) and every assertion here would run against a pane that does not
 * exist. Clicking a dock tab that is NOT the active one expands the pane
 * explicitly (`handleWideDockTabClick`), so the first `openDockTab` call does
 * it — but the guard must not silently measure nothing, hence the explicit
 * wait for a non-zero cluster below.
 */
async function expectClusterVisible(page: Page, where: string): Promise<void> {
  await expect
    .poll(async () => {
      const box = await page.locator(CONTROLS).boundingBox()
      return box ? box.width : 0
    }, { message: `${where}: the cluster never appeared`, timeout: 10000 })
    .toBeGreaterThan(0)
}

async function openDockTab(page: Page, tab: string): Promise<void> {
  const button = page.locator(`.dock-btn[data-tab="${tab}"]`).filter({ visible: true }).first()
  await expect(button).toBeVisible({ timeout: 15000 })
  if (await button.evaluate(el => el.classList.contains('active'))) {
    const other = page.locator('.dock-btn[data-tab="browse"]').filter({ visible: true }).first()
    if (!(await other.evaluate(el => el.classList.contains('active')))) {
      await other.click()
      await page.waitForTimeout(150)
    }
  }
  await button.click()
  await page.waitForTimeout(400)
}

test.describe('panel window controls never cover panel content', () => {
  test('every dock tab', async ({ page }) => {
    await expect(page.locator('.dock-btn[data-tab="browse"]').filter({ visible: true }).first()).toBeVisible({
      timeout: 15000,
    })
    const tabs = await page.evaluate(() =>
      Array.from(document.querySelectorAll('.dock-btn[data-tab]'))
        .filter(b => b.getBoundingClientRect().width > 0)
        .map(b => b.getAttribute('data-tab') as string),
    )
    expect(tabs.length, 'the wide dock should expose its tabs').toBeGreaterThan(5)

    for (const tab of tabs) {
      await openDockTab(page, tab)
      // `openDockTab` must have EXPANDED the pane (a click on a non-active tab
      // expands explicitly). If it did not, the guard below would measure
      // nothing and pass vacuously — fail loudly instead.
      await expectClusterVisible(page, `页签 ${tab}`)
      await expectChromeClear(page, `页签 ${tab}`)
    }
  })

  test('forge issue/PR drill-down header', async ({ page }) => {
    // Stub the forge API: the e2e project has no real binding, and the panel
    // only needs a bound repo plus one item to render its drill-down header.
    const binding = { platform: 'gitlab', host: 'gitlab.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    const item = {
      platform: 'gitlab',
      host: 'gitlab.com',
      owner: 'acme',
      repo: 'widgets',
      type: 'pr',
      number: 681,
      title: 'fix: forge header layout',
      body: 'Some body text.',
      state: 'open',
      author: 'taka',
      commentCount: 0,
      url: 'https://gitlab.com/acme/widgets/-/merge_requests/681',
      createdAt: '2026-11-17T09:00:00Z',
      updatedAt: '2026-11-17T10:00:00Z',
      slug: 'acme/widgets',
    }
    await page.route('**/api/forge/**', (route) => {
      const path = new URL(route.request().url()).pathname
      const json = (body: unknown) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
      if (path === '/api/forge/binding') return json({ binding })
      if (path === '/api/forge/unread') return json({ count: 0, items: [] })
      if (path === '/api/forge/unread-items') return json({ count: 0, items: [] })
      if (path === '/api/forge/items') return json({ items: [item], hasMore: false, nextPage: 1, binding })
      if (path === '/api/forge/item') return json({ item, binding })
      if (path === '/api/forge/comments') return json({ comments: [], hasMore: false, nextPage: 1 })
      if (path === '/api/forge/item-pipelines') return json({ pipelines: [], hasMore: false, nextPage: 1, binding })
      if (path === '/api/forge/read') return json({ count: 0 })
      return json({})
    })

    await openDockTab(page, 'forge')
    await page.evaluate(() => {
      window.dispatchEvent(
        new CustomEvent('clawbench-open-forge', {
          detail: { target: { type: 'pr', number: 681, runId: 0, itemKey: 'pr/681' } },
        }),
      )
    })
    await expect(page.locator('.forge-detail-header')).toBeVisible({ timeout: 10000 })
    await expectChromeClear(page, 'GitLab 下钻页')
  })

  test('git commit drill-down header', async ({ page }) => {
    await openDockTab(page, 'history')
    const row = page.locator('.drilldown-item').filter({ visible: true }).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.click()
    // The commit detail replaces the list header with `.drilldown-header`.
    await expect(page.locator('.drilldown-header')).toBeVisible({ timeout: 10000 })
    await expectChromeClear(page, 'Git 提交详情')
  })
})

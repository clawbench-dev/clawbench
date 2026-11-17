import { test, expect, type Page } from '../fixtures'

/**
 * Guard: the panel's window-control cluster must never sit ON panel content.
 *
 * In the wide layout App.vue overlays the left panel's two window controls
 * (minimise / maximise) on whatever header the current view draws — that is how
 * one row serves both the app chrome and a drill-down page's own header. The
 * cluster is 52dp wide, flush with the panel's right edge, translucent and
 * z-indexed above the panel, so EVERY header row has to reserve that width with
 * padding-right.
 *
 * A header that forgets renders its own right-hand buttons UNDER the cluster:
 * the minimise glyph (a single horizontal line) shows through next to the
 * page's icon, and the cluster's tint cuts the button's background in half, so
 * two buttons read as one overlapping blob. Nothing throws, no unit test sees
 * it (jsdom does not lay out), and the CSS list that prevents it is maintained
 * by hand — which is exactly how the forge drill-down lost its reservation.
 *
 * So this spec asserts the invariant BEHAVIOURALLY: walk the dock, open the
 * drill-downs we can reach, and at each step check that nothing interactive
 * inside the panel is under the cluster. A new dock tab is covered without
 * anyone remembering to add it anywhere.
 */

const CONTROLS = '.panel-window-controls'

interface OverlapReport {
  missing: boolean
  hits: string[]
}

/** Sample the cluster's box and report panel controls found underneath it. */
async function overlapUnderControls(page: Page): Promise<OverlapReport> {
  return page.evaluate((selector) => {
    const controls = document.querySelector(selector)
    if (!controls) return { missing: true, hits: [] }
    const rect = controls.getBoundingClientRect()
    const hits: string[] = []
    for (const fy of [0.25, 0.5, 0.75]) {
      for (const fx of [0.15, 0.5, 0.85]) {
        const x = rect.left + rect.width * fx
        const y = rect.top + rect.height * fy
        for (const el of document.elementsFromPoint(x, y)) {
          // The cluster itself and its transparent overlay host are expected.
          if (el === controls || controls.contains(el)) continue
          if (el.classList.contains('panel-head')) continue
          if (!el.closest('.col-left')) continue
          if (!['BUTTON', 'A', 'INPUT', 'SELECT', 'TEXTAREA'].includes(el.tagName)) continue
          const label = (el.getAttribute('title') || el.getAttribute('aria-label') || el.textContent || '')
            .trim()
            .slice(0, 30)
          hits.push(`${el.tagName.toLowerCase()}.${el.className || '(no class)'} [${label}]`)
        }
      }
    }
    return { missing: false, hits: [...new Set(hits)] }
  }, CONTROLS)
}

async function expectChromeClear(page: Page, where: string): Promise<void> {
  const report = await overlapUnderControls(page)
  expect(report.missing, `${where}: 面板窗口按钮没有出现（视口不够宽？）`).toBe(false)
  expect(report.hits, `${where}: 这些控件被面板窗口按钮盖住了`).toEqual([])
}

/**
 * Open a dock tab.
 *
 * Clicking the ACTIVE tab is the VS Code-style "collapse the pane" gesture, so
 * bounce through another tab first — otherwise the pane closes and the cluster
 * (and every header) disappears.
 */
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
      [...document.querySelectorAll('.dock-btn[data-tab]')]
        .filter(b => b.getBoundingClientRect().width > 0)
        .map(b => b.getAttribute('data-tab') as string),
    )
    expect(tabs.length, 'the wide dock should expose its tabs').toBeGreaterThan(5)

    for (const tab of tabs) {
      await openDockTab(page, tab)
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

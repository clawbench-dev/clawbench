import { expect, test, type Page } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * 表格的全屏查看与数据表换行。
 *
 * 两个曾报过的问题：
 *
 *  1. 只把表格克隆成 HTML 放进全屏浮层时，克隆里没有 Vue 实例 —— 表头栏的
 *     「简单表 ⇄ 数据表」开关点了没反应。现在 DataTable 在全屏里是活组件。
 *  2. 数据表正文被 `max-width: 320dp + nowrap + ellipsis` 截断，长说明只看得到
 *     前半句。现在超过 320dp 折行，内容必须完整可见。
 *
 * 临时 markdown 文件走 API 创建/删除，不往仓库里写样例。
 */

const MD_FILE = 'test/__e2e_table_landscape.md'
const LONG_TEXT = '这是一段很长的说明文字用来验证数据表里的长列不会被省略号截断而是完整折行显示。'.repeat(3)

const MD_BODY = `# 表格全屏 e2e

| 名称 | 说明 |
| --- | --- |
| 短 | ${LONG_TEXT} |
| 短 | short |
`

function itemByName(page: Page, name: string, kind: 'dir' | 'file') {
  const rowSelector =
    kind === 'dir'
      ? '.file-item.dir-item, .grid-item.grid-dir'
      : '.file-item:not(.dir-item), .grid-item:not(.grid-dir)'
  return page
    .locator(rowSelector)
    .filter({ has: page.locator('.file-name, .grid-name').getByText(name, { exact: true }) })
    .first()
}

async function openInTestDir(page: Page, fm: FileManagerPage, nav: NavigationPage, name: string) {
  await nav.switchToFileManager()
  await fm.waitForContent(15000)
  const dir = itemByName(page, 'test', 'dir')
  await expect(dir).toBeVisible({ timeout: 10000 })
  await dir.dblclick()
  await expect(page.locator('.file-item, .grid-item').first()).toBeVisible({ timeout: 10000 })

  const file = itemByName(page, name, 'file')
  await expect(file).toBeVisible({ timeout: 15000 })
  await file.dblclick()
  await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
}

test.describe('表格全屏与数据表换行', () => {
  test.describe.configure({ timeout: 90_000 })

  let fm: FileManagerPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    fm = new FileManagerPage(page)
    nav = new NavigationPage(page)
    await page.request.post('/api/file/write', { data: { path: MD_FILE, content: MD_BODY } })
  })

  test.afterEach(async ({ page }) => {
    await page.request.post('/api/file/delete', { data: { path: MD_FILE } }).catch(() => {})
  })

  test('全屏里的数据表仍然可以切回简单表，长内容会折行而不是被截断', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_table_landscape.md')

    const sourceTable = page.locator('.file-overlay .dt')
    await expect(sourceTable).toBeVisible({ timeout: 20000 })

    // 文字表默认是简单表；切到数据表后，长说明必须完整换行（不是 320dp 截断）。
    await page.locator('.file-overlay .dt-mode-btn').click()
    await page.locator('.dt-pop [data-mode-value="data"]').click()
    await expect(sourceTable).toHaveClass(/is-data/)

    const longCell = page.locator('.file-overlay .dt-grid tbody tr').first().locator('td').nth(1)
    const cellHeight = await longCell.locator('.dt-cell').evaluate(el => el.getBoundingClientRect().height)
    // 一行约 19-24px；完整的一句话在 320dp 宽度里至少要折成两行。
    expect(cellHeight).toBeGreaterThan(30)

    // 打开全屏：这里必须是活组件（克隆的话开关是死的）。
    // 最大化按钮只在触屏 / 窄窗口出现（桌面宽窗口藏起来），所以先把视口
    // 收窄到断点以内，模拟「窗口不宽」的情形。
    await page.setViewportSize({ width: 880, height: 900 })
    await page.locator('.file-overlay .table-block-landscape-btn').click()
    const overlay = page.locator('.block-landscape')
    await expect(overlay).toBeVisible()
    await expect(overlay.locator('.block-landscape-live .dt')).toBeVisible()

    // 点全屏里的开关：切回简单表。
    await overlay.locator('.dt-mode-btn').click()
    await page.locator('.dt-pop [data-mode-value="normal"]').click()
    await expect(overlay.locator('.block-landscape-live .dt')).not.toHaveClass(/is-data/)

    // 关闭浮层后，改动回写到浮层后面那张表。
    await overlay.locator('[data-action="landscape-close"]').click()
    await expect(page.locator('.block-landscape')).toHaveCount(0)
    await expect(sourceTable).not.toHaveClass(/is-data/)
  })
})

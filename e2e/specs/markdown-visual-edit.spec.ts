import { test, expect, type Page } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Visual (WYSIWYG) markdown editing, exercised on the real sample documents in
 * `test/markdown/`.
 *
 * These samples are the point: formula-demo, mermaid-demo, code-block-demo,
 * images-demo and table-demo are the shapes our own docs actually use, so a
 * regression here (formulas showing as `$…$`, diagrams as code, broken local
 * images) is a regression in the editor's reason to exist.
 *
 * The write path is tested on a throwaway file created through the API and
 * deleted afterwards — never on the samples themselves.
 */

const MD_DIR = ['test', 'markdown'] as const
const TEMP_FILE = 'test/markdown/__e2e_visual_edit.md'
const TEMP_FRONTMATTER = 'test/markdown/__e2e_visual_frontmatter.md'

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

async function gotoMarkdownDir(page: Page, fm: FileManagerPage, nav: NavigationPage) {
  await nav.switchToFileManager()
  await fm.waitForContent(15000)
  for (const dir of MD_DIR) {
    const item = itemByName(page, dir, 'dir')
    await expect(item).toBeVisible({ timeout: 10000 })
    await item.dblclick()
    await expect(page.locator('.file-item, .grid-item').first()).toBeVisible({ timeout: 10000 })
  }
}

async function openMarkdown(page: Page, fm: FileManagerPage, nav: NavigationPage, name: string) {
  await gotoMarkdownDir(page, fm, nav)
  const file = itemByName(page, name, 'file')
  await expect(file).toBeVisible({ timeout: 10000 })
  await file.dblclick()
  await expect(page.locator('.file-overlay')).toBeVisible({ timeout: 10000 })
  // Content loaded: the viewer shows either the rendered preview or the source.
  await expect(
    page.locator('.file-overlay .markdown-body, .file-overlay .cm-scroller').first(),
  ).toBeVisible({ timeout: 20000 })
}

/** Click the header's "Visual edit" entry, inline or folded into the More menu. */
async function enterVisualEdit(page: Page) {
  const overlay = page.locator('.file-overlay')
  const inline = overlay.locator('[data-toolbar-id="wysiwygEdit"]')
  if ((await inline.count()) > 0 && (await inline.first().isVisible())) {
    await inline.first().click()
  } else {
    await overlay.locator('[data-ab-more]').first().click()
    await page.locator('.ab-menu-item', { hasText: /Visual edit|可视化编辑/ }).first().click()
  }
  await expect(page.locator('.md-editor .ProseMirror')).toBeVisible({ timeout: 25000 })
}

async function exitVisualEdit(page: Page) {
  await page.locator('.md-editor .code-editor-actions button').last().click()
  await expect(page.locator('.md-editor')).toHaveCount(0, { timeout: 10000 })
}

test.describe('Markdown visual editing', () => {
  // Opening a file, loading the editor chunk and (for mermaid) the diagram
  // chunk can exceed the suite default on a slow machine. The per-assertion
  // budgets below are chosen against this number, not the global 30s.
  test.describe.configure({ timeout: 90_000 })

  let fm: FileManagerPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    fm = new FileManagerPage(page)
    nav = new NavigationPage(page)
  })

  test.afterEach(async ({ page }) => {
    // Never leave throwaway files behind, even when an assertion failed.
    for (const path of [TEMP_FILE, TEMP_FRONTMATTER]) {
      await page.request.post('/api/file/delete', { data: { path } }).catch(() => {})
    }
  })

  test('renders the formulas of formula-demo.md instead of raw $…$', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'formula-demo.md')
    await enterVisualEdit(page)

    // KaTeX output for both inline and display math.
    const formulas = page.locator('.md-editor .katex')
    await expect(formulas.first()).toBeVisible({ timeout: 15000 })
    expect(await formulas.count()).toBeGreaterThan(10)
    await expect(page.locator('.md-editor .md-math-block').first()).toBeVisible()

    // The source is untouched: the editor still holds the original delimiters.
    const text = await page.locator('.md-editor .ProseMirror').innerText()
    expect(text).not.toContain('$x = \\frac')

    await exitVisualEdit(page)
  })

  test('draws the mermaid diagrams of mermaid-demo.md', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'mermaid-demo.md')
    await enterVisualEdit(page)

    // The diagram chunk is lazy; give it room to arrive and render.
    const svg = page.locator('.md-editor .md-code-preview svg').first()
    await expect(svg).toBeVisible({ timeout: 30000 })

    // The fenced source stays editable underneath the drawing.
    await expect(page.locator('.md-editor .md-code-block pre code').first()).toContainText('sequenceDiagram')

    await exitVisualEdit(page)
  })

  test('colours the code blocks of code-block-demo.md', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'code-block-demo.md')
    await enterVisualEdit(page)

    const tokens = page.locator('.md-editor .md-code-block code span[class*="hljs-"]')
    await expect(tokens.first()).toBeVisible({ timeout: 10000 })
    expect(await tokens.count()).toBeGreaterThan(5)

    await exitVisualEdit(page)
  })

  test('loads the local images of images-demo.md through the served URL', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'images-demo.md')
    await enterVisualEdit(page)

    const img = page.locator('.md-editor img.md-editor-image').first()
    await expect(img).toBeVisible({ timeout: 15000 })
    const src = await img.getAttribute('src')
    expect(src).toContain('/api/fs/raw/test/images/')

    await exitVisualEdit(page)
  })

  test('keeps tables editable in table-demo.md', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'table-demo.md')
    await enterVisualEdit(page)

    await expect(page.locator('.md-editor .ProseMirror table').first()).toBeVisible()
    await expect(page.locator('.md-editor .ProseMirror table').first()).toContainText('Alice')

    await exitVisualEdit(page)
  })

  test('edits and saves a file without losing the markdown', async ({ page }) => {
    await page.request.post('/api/file/write', {
      data: { path: TEMP_FILE, content: '# 临时文件\n\n原始段落。\n' },
    })

    await openMarkdown(page, fm, nav, '__e2e_visual_edit.md')
    await enterVisualEdit(page)

    // Append to the last paragraph, then save with the editor's shortcut.
    await page.locator('.md-editor .ProseMirror p').last().click()
    await page.keyboard.press('Control+End')
    await page.keyboard.type('E2E-TYPED')

    // Typing marks the buffer dirty (the dot in the editor's action bar).
    await expect(page.locator('.md-editor .dirty-dot')).toBeVisible({ timeout: 5000 })

    await page.keyboard.press('Control+s')
    await expect(page.locator('.toast-text')).toContainText(/Saved|已保存/, { timeout: 10000 })

    // A successful save clears the dirty flag — otherwise leaving would prompt
    // "save changes?" for text that is already on disk.
    await expect(page.locator('.md-editor .dirty-dot')).toHaveCount(0, { timeout: 5000 })

    const resp = await page.request.get(`/api/fs/file/${TEMP_FILE}`)
    expect(resp.ok()).toBe(true)
    const body = await resp.json()
    expect(body.content).toContain('E2E-TYPED')
    expect(body.content).toContain('# 临时文件')

    await exitVisualEdit(page)
  })

  test('keeps source editing on CodeMirror', async ({ page }) => {
    await openMarkdown(page, fm, nav, 'table-demo.md')

    const overlay = page.locator('.file-overlay')
    const inline = overlay.locator('[data-toolbar-id="edit"]')
    if ((await inline.count()) > 0 && (await inline.first().isVisible())) {
      await inline.first().click()
    } else {
      await overlay.locator('[data-ab-more]').first().click()
      await page.locator('.ab-menu-item').filter({ hasText: /^(Edit|编辑)$/ }).first().click()
    }

    await expect(page.locator('.file-overlay .cm-scroller')).toBeVisible({ timeout: 20000 })
    await expect(page.locator('.md-editor')).toHaveCount(0)
  })

  test('refuses a file with frontmatter and keeps source editing available', async ({ page }) => {
    await page.request.post('/api/file/write', {
      data: { path: TEMP_FRONTMATTER, content: '---\nname: demo\n---\n\n# 标题\n' },
    })

    await openMarkdown(page, fm, nav, '__e2e_visual_frontmatter.md')
    await enterVisualEditExpectingRefusal(page)

    await expect(page.locator('.toast-text')).toContainText(/frontmatter/i, { timeout: 10000 })
    await expect(page.locator('.md-editor')).toHaveCount(0)
  })
})

/** Same entry as enterVisualEdit, but the editor must NOT open. */
async function enterVisualEditExpectingRefusal(page: Page) {
  const overlay = page.locator('.file-overlay')
  const inline = overlay.locator('[data-toolbar-id="wysiwygEdit"]')
  if ((await inline.count()) > 0 && (await inline.first().isVisible())) {
    await inline.first().click()
  } else {
    await overlay.locator('[data-ab-more]').first().click()
    await page.locator('.ab-menu-item', { hasText: /Visual edit|可视化编辑/ }).first().click()
  }
}

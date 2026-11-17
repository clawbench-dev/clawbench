import { test, expect, type Page } from '../fixtures'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Editing the source behind a rendered preview (HTML / OpenAPI).
 *
 * Both types used to offer the header's Edit toggle while the source editor
 * behind it was hard-wired read-only: the button lit up and nothing else
 * happened, and the file could not be changed at all. They now behave like
 * markdown — the toggle is offered from either view, swaps the preview for a
 * writable editor, and leaving the session returns to the preview.
 *
 * Throwaway files are created through the API in `test/` and deleted after
 * every case; no sample document is ever written to.
 */

const HTML_FILE = 'test/__e2e_preview_edit.html'
const SPEC_FILE = 'test/__e2e_preview_edit.yaml'

const HTML_BODY = '<h1>E2E-ORIGINAL</h1>\n'
const SPEC_BODY = "openapi: 3.0.0\ninfo:\n  title: E2E Spec\n  version: '1.0'\npaths: {}\n"

const MARKER = 'E2E-EDIT-MARKER'

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

/** Open a file that lives directly in `test/`. */
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

/** Click the header's Edit entry, inline or folded into the More menu. */
async function clickEdit(page: Page) {
  const overlay = page.locator('.file-overlay')
  const inline = overlay.locator('[data-toolbar-id="edit"]')
  if ((await inline.count()) > 0 && (await inline.first().isVisible())) {
    await inline.first().click()
  } else {
    await overlay.locator('[data-ab-more]').first().click()
    await page.locator('.ab-menu-item').filter({ hasText: /^(Edit|编辑)$/ }).first().click()
  }
}

/** Click the header's source/rendered eye toggle. */
async function clickViewToggle(page: Page) {
  const overlay = page.locator('.file-overlay')
  const inline = overlay.locator('[data-toolbar-id="toggleView"]')
  if ((await inline.count()) > 0 && (await inline.first().isVisible())) {
    await inline.first().click()
  } else {
    await overlay.locator('[data-ab-more]').first().click()
    await page.locator('.ab-menu-item').filter({ hasText: /Source|Rendered|源码|预览/ }).first().click()
  }
}

/**
 * The same CodeMirror component renders both browse and edit. Its
 * `contenteditable` attribute stays "true" in browse mode too (read-only is a
 * transaction filter, not a DOM flag), so the honest signal is the editing
 * chrome: the save/undo bar only exists in an edit session.
 */
async function expectSourceEditable(page: Page) {
  await expect(page.locator('.file-overlay .cm-scroller')).toBeVisible({ timeout: 25000 })
  await expect(page.locator('.file-overlay .cm-viewer.is-editable')).toBeVisible()
  await expect(page.locator('.file-overlay .code-editor-actions')).toBeVisible()
}

async function expectSourceReadOnly(page: Page) {
  await expect(page.locator('.file-overlay .cm-scroller')).toBeVisible({ timeout: 25000 })
  await expect(page.locator('.file-overlay .cm-viewer.cm-readonly')).toBeVisible()
  await expect(page.locator('.file-overlay .code-editor-actions')).toHaveCount(0)
}

/** Append valid trailing text, save with the editor's shortcut, verify on disk. */
async function appendAndSave(page: Page, path: string, appended: string) {
  await page.locator('.file-overlay .cm-content').click()
  await page.keyboard.press('Control+End')
  // insertText bypasses key events: a completion popup cannot swallow the
  // newline and turn it into an accepted suggestion.
  await page.keyboard.insertText(appended)

  await page.keyboard.press('Control+s')
  await expect(page.locator('.toast-text')).toContainText(/Saved|已保存/, { timeout: 10000 })

  await expect.poll(async () => {
    const resp = await page.request.get(`/api/fs/file/${path}`)
    if (!resp.ok()) return ''
    return ((await resp.json()) as { content?: string }).content ?? ''
  }, { timeout: 10000 }).toContain(MARKER)
}

test.describe('Rendered previews open an editable source view', () => {
  test.describe.configure({ timeout: 90_000 })

  let fm: FileManagerPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    fm = new FileManagerPage(page)
    nav = new NavigationPage(page)
    await page.request.post('/api/file/write', { data: { path: HTML_FILE, content: HTML_BODY } })
    await page.request.post('/api/file/write', { data: { path: SPEC_FILE, content: SPEC_BODY } })
  })

  test.afterEach(async ({ page }) => {
    // Never leave throwaway files behind, even when an assertion failed.
    for (const path of [HTML_FILE, SPEC_FILE]) {
      await page.request.post('/api/file/delete', { data: { path } }).catch(() => {})
    }
  })

  test('edits and saves an HTML file from its rendered preview', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_preview_edit.html')
    await expect(page.locator('.file-overlay .html-preview-iframe')).toBeVisible({ timeout: 20000 })

    await clickEdit(page)
    await expect(page.locator('.file-overlay .html-preview-iframe')).toHaveCount(0, { timeout: 10000 })
    await expectSourceEditable(page)

    await appendAndSave(page, HTML_FILE, `\n<!-- ${MARKER} -->`)

    // Leaving the session returns to the rendered page, not to a raw view.
    await page.locator('.file-overlay .code-editor-actions button').last().click()
    await expect(page.locator('.file-overlay .html-preview-iframe')).toBeVisible({ timeout: 20000 })
  })

  test('edits and saves an OpenAPI spec from both the docs and the source view', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_preview_edit.yaml')
    await expect(page.locator('.file-overlay .openapi-iframe')).toBeVisible({ timeout: 30000 })

    // The reported defect lived here: in the source view the header offered
    // Edit, but the editor stayed read-only and no save was wired at all.
    await clickViewToggle(page)
    await expect(page.locator('.file-overlay .openapi-iframe')).toHaveCount(0, { timeout: 10000 })
    await expectSourceReadOnly(page)

    await clickEdit(page)
    await expectSourceEditable(page)

    await appendAndSave(page, SPEC_FILE, `\n# ${MARKER}`)

    // This session started from the source view, so it returns there.
    await page.locator('.file-overlay .code-editor-actions button').last().click()
    await expectSourceReadOnly(page)

    // ...and the docs still render from the edited spec.
    await clickViewToggle(page)
    await expect(page.locator('.file-overlay .openapi-iframe')).toBeVisible({ timeout: 30000 })
  })

  test('offers Edit straight from the rendered OpenAPI docs', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_preview_edit.yaml')
    await expect(page.locator('.file-overlay .openapi-iframe')).toBeVisible({ timeout: 30000 })

    await clickEdit(page)
    await expect(page.locator('.file-overlay .openapi-iframe')).toHaveCount(0, { timeout: 10000 })
    await expectSourceEditable(page)
  })

  // The close and back buttons route through the shared "leave edit first"
  // guard: it asks the editor to exit and then WAITS for edit mode to end. With
  // the exit event unbound (the old wiring) edit mode never ended, the guard
  // gave up after its 5s budget and cancelled the action — both buttons looked
  // completely dead while an OpenAPI file was in edit mode.
  test('closes the viewer from an OpenAPI edit session', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_preview_edit.yaml')
    await clickViewToggle(page)
    await clickEdit(page)
    await expectSourceEditable(page)

    await page.locator('.file-overlay .overlay-close-btn').click()
    await expect(page.locator('.file-overlay')).toHaveCount(0, { timeout: 5000 })
  })

  test('navigates back out of an OpenAPI edit session', async ({ page }) => {
    await openInTestDir(page, fm, nav, '__e2e_preview_edit.yaml')
    await clickViewToggle(page)
    await clickEdit(page)
    await expectSourceEditable(page)

    await page.locator('.file-overlay .file-header-back-btn').click()
    await expect(page.locator('.file-overlay')).toHaveCount(0, { timeout: 5000 })
  })
})

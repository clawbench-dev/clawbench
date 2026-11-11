import { test, expect, type Page } from '../fixtures'
import { projectApiFetch } from '../helpers/auth'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Deleting a hit from the file-manager search results must remove the row.
 *
 * Regression: the result list is a listing of its own (the store's post-mutation
 * `loadFiles()` only reloads the browsed directory), and the component used to
 * refresh it right after `emit()` — synchronously, while the parent handler was
 * still waiting for its confirmation dialog. The re-scan therefore saw the file
 * still on disk and the deleted hit stayed in the list until the query changed.
 * The refresh now runs after the delete resolved.
 *
 * The assertion that makes this discriminating: the empty state only renders
 * once the re-scan FINISHED with zero results, so "row still there" fails
 * instead of passing during a transient blank window.
 */

const NAME = 'e2e-search-delete-target.txt'

async function writeProjectFile(name: string, content: string): Promise<void> {
  const resp = await projectApiFetch('/api/file/write', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: name, content }),
  })
  expect(resp.ok, `write ${name}`).toBe(true)
}

async function deleteProjectFile(name: string): Promise<void> {
  await projectApiFetch('/api/file/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: name }),
  })
}

async function existsInProject(name: string): Promise<string> {
  const resp = await projectApiFetch('/api/file/batch-exists', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ paths: [name] }),
  })
  const body = await resp.json()
  return body.results[name]
}

/** Walk the file manager up to the project root so the search covers the file. */
async function goToProjectRoot(page: Page): Promise<void> {
  const upBtn = page.locator('.dir-nav-bottom .dir-up-btn')
  const breadcrumb = page.locator('.dir-nav-bottom .dir-breadcrumb')
  for (let i = 0; i < 12 && !(await upBtn.isDisabled()); i++) {
    const before = await breadcrumb.locator('.crumb').count()
    await upBtn.click()
    await expect.poll(() => breadcrumb.locator('.crumb').count()).toBeLessThan(before)
  }
  await expect(upBtn).toBeDisabled()
}

test.describe('File manager search results', () => {
  test.beforeEach(async ({ page }) => {
    await writeProjectFile(NAME, 'delete me from the search results\n')

    const nav = new NavigationPage(page)
    await nav.switchToFileManager()
    const fm = new FileManagerPage(page)
    await fm.waitForContent(15000)
    await goToProjectRoot(page)
  })

  test.afterEach(async () => {
    await deleteProjectFile(NAME)
  })

  test('right-click delete removes the hit from the result list', async ({ page }) => {
    const row = page.locator(`[data-path="${NAME}"]`)
    const searchInput = page.locator('.fs-input-row input')

    await searchInput.fill(NAME)
    await expect(row).toBeVisible({ timeout: 15000 })

    // Right-click the name (the row also holds a "reveal in directory" button
    // on its right edge, which would swallow the click there).
    await row.locator('.file-name').click({ button: 'right' })
    await page.locator('.context-menu.visible .context-menu-item.danger').click()
    await page.locator('.dlg-overlay .dlg-ok').click()

    // The re-scan finished with nothing left: the row is gone AND the results
    // layer shows its empty state (which only renders when searching === false).
    await expect(page.locator('[data-role="file-search-empty"]')).toBeVisible({ timeout: 15000 })
    await expect(row).toHaveCount(0)
    await expect.poll(() => existsInProject(NAME), { timeout: 15000 }).toBe('none')
  })
})

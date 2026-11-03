import { test, expect, type Page } from '../fixtures'
import { projectApiFetch } from '../helpers/auth'
import { FileManagerPage } from '../pages/file-manager.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Dropping a file into the file manager must not silently clobber a namesake:
 * the upload is preflighted, the user is asked, and the answer decides whether
 * the existing file is replaced or the new one is saved under a numbered name.
 *
 * The drop is synthesized (a real OS drag cannot be scripted): the same
 * `DataTransfer` shape the browser produces, dispatched at the list container.
 */

const NAME = 'e2e-upload-conflict.txt'
const NUMBERED = 'e2e-upload-conflict_1.txt'

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

async function readProjectFile(name: string): Promise<string> {
  const resp = await projectApiFetch(`/api/fs/raw/${encodeURIComponent(name)}`)
  return resp.ok ? resp.text() : ''
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

/** Walk the file manager up to the project root so the drop targets a known dir. */
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

/** Drop one OS file onto the manager's list container. */
async function dropFile(page: Page, name: string, content: string): Promise<void> {
  const container = page.locator('.file-list, .file-grid').first()
  await expect(container).toBeVisible({ timeout: 10000 })
  await container.evaluate((el, { name, content }) => {
    const dt = new DataTransfer()
    dt.items.add(new File([content], name, { type: 'text/plain' }))
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }))
  }, { name, content })
}

test.describe('Upload name conflict', () => {
  test.beforeEach(async ({ page }) => {
    const fm = new FileManagerPage(page)
    const nav = new NavigationPage(page)
    await nav.switchToFileManager()
    await fm.waitForContent(15000)
    await goToProjectRoot(page)

    await deleteProjectFile(NAME)
    await deleteProjectFile(NUMBERED)
    await writeProjectFile(NAME, 'original')
  })

  test.afterEach(async () => {
    await deleteProjectFile(NAME)
    await deleteProjectFile(NUMBERED)
  })

  test('asks before replacing an existing file and overwrites on demand', async ({ page }) => {
    await dropFile(page, NAME, 'overwritten')

    const dialog = page.locator('.modal-dialog')
    await expect(dialog).toBeVisible({ timeout: 10000 })
    await expect(dialog.locator('.ucd-item').first()).toHaveText(NAME)

    // The direct-overwrite action is the destructive (red) footer button.
    await dialog.locator('.modal-footer .fbtn-danger').click()

    await expect.poll(() => readProjectFile(NAME), { timeout: 15000 }).toBe('overwritten')
    // Overwrite must not leave a renamed sibling behind.
    expect(await existsInProject(NUMBERED)).toBe('none')
  })

  test('keeps both files when the user declines to overwrite', async ({ page }) => {
    await dropFile(page, NAME, 'second copy')

    const dialog = page.locator('.modal-dialog')
    await expect(dialog).toBeVisible({ timeout: 10000 })

    // "Keep both" is the neutral footer action between cancel and overwrite.
    await dialog.locator('.modal-footer .fbtn').nth(1).click()

    await expect.poll(() => existsInProject(NUMBERED), { timeout: 15000 }).toBe('file')
    expect(await readProjectFile(NAME)).toBe('original')
    expect(await readProjectFile(NUMBERED)).toBe('second copy')
  })
})

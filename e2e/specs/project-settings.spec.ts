import { test, expect } from '../fixtures'
import { SettingsPage } from '../pages/settings.page'

/**
 * Project settings panel — the registry listing and its detail page.
 *
 * Read-only on purpose: the detail page's "switch project" and "delete project"
 * actions mutate server state shared by every spec (the current project and the
 * projects registry), so exercising them here would leak into other specs. The
 * destructive paths are covered by unit tests; this spec only proves the panel
 * renders the listing and drills into a detail page.
 */
test.describe('Project settings panel', () => {
  let settings: SettingsPage

  test.beforeEach(async ({ page }) => {
    settings = new SettingsPage(page)
    await settings.openSettings()
  })

  test('shows a project row and opens its detail page', async ({ page }) => {
    // The "项目" category is the first row in the projectAppearance group.
    const projectRow = page.locator('.settings-index__row').filter({ hasText: /project|项目/i }).first()
    await expect(projectRow).toBeVisible({ timeout: 5000 })
    await projectRow.click()

    // The all-projects card loads its listing from GET /api/projects/list.
    const rows = page.locator('.projects-row')
    await expect(rows.first()).toBeVisible({ timeout: 10000 })

    // Drill into the first project's detail page.
    await rows.first().click()

    await expect(page.locator('.project-detail')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.project-detail__name')).not.toBeEmpty()
    // The stats block renders the repo-kind row (the field that required the
    // dedicated detail request).
    await expect(page.locator('.project-detail__stat').filter({ hasText: /repo|仓库/i })).toBeVisible()
  })
})

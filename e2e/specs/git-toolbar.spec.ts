import { test, expect } from '../fixtures'
import { NavigationPage } from '../pages/navigation.page'
import { apiFetch } from '../helpers/auth'
import { execFileSync } from 'child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

/**
 * Git history toolbar.
 *
 * The commit list used to spend two rows on its chrome: a title/count/search bar
 * and, below it, a branch bar whose only other content was the sentence
 * "本地 N 个分支". This spec pins the replacement — one row, branch picker first,
 * count as a badge, every button inside an ActionBar, and the search field
 * collapsed to a button that opens in place.
 *
 * These are layout/flow assertions against the real bundle: the collapse
 * behaviour is what makes the single row fit, and it is decided at runtime by
 * ActionBar's measurement, not by CSS alone.
 */
/**
 * A throwaway repo with TWO local branches.
 *
 * The default e2e project is the checkout this suite runs in, and CI checks that
 * out as a detached HEAD with a single `main` ref — `git for-each-ref refs/heads/`
 * still lists one, but `git symbolic-ref HEAD` fails, so the branch picker had
 * nothing to show and `.branch-chip` never rendered. Building the repo here makes
 * the assertions independent of how the runner checked the code out.
 */
function createToolbarRepo(): string {
  const dir = mkdtempSync(join(tmpdir(), 'clawbench-git-toolbar-'))
  const git = (...args: string[]) =>
    execFileSync('git', args, { cwd: dir, stdio: 'ignore' })
  git('init', '-q', '-b', 'main')
  git('config', 'user.email', 'e2e@example.invalid')
  git('config', 'user.name', 'e2e')
  writeFileSync(join(dir, 'README.md'), '# toolbar fixture\n')
  git('add', '.')
  git('commit', '-q', '-m', 'fixture commit')
  git('branch', 'feature/other')
  return dir
}

test.describe('Git history toolbar', () => {
  let repoDir = ''

  test.beforeAll(async () => {
    repoDir = createToolbarRepo()
    await apiFetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: repoDir }),
    })
  })

  test.afterAll(async () => {
    await apiFetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: process.cwd() }),
    }).catch(() => {})
    try { rmSync(repoDir, { recursive: true, force: true }) } catch { /* best effort */ }
  })

  test.beforeEach(async ({ page }) => {
    // switchToTab, not switchToHistory: the latter drives the narrow dock's
    // overflow menu, which does not exist at the default (wide) viewport.
    await new NavigationPage(page).switchToTab('history')
    await expect(page.locator('.git-toolbar')).toBeVisible({ timeout: 20000 })
  })

  test('keeps the picker, the count and the actions on one row', async ({ page }) => {
    const bar = page.locator('.git-toolbar')

    // The branch picker is the page's primary control and must not be folded.
    await expect(bar.locator('.branch-chip')).toBeVisible()
    await expect(bar.locator('.branch-chip-name')).not.toBeEmpty()
    await expect(bar.locator('.git-count')).toBeVisible()

    // Every button lives inside the ActionBar, so overflow is decided in one
    // place instead of by each button's own width rules.
    await expect(bar.locator('.git-actions [data-action="search"]')).toBeVisible()
    await expect(bar.locator('.git-actions [data-action="refresh"]')).toBeVisible()
    // Manage lives in the branch picker's menu, not at the end of the bar.
    await expect(bar.locator('.git-actions [data-action="manage"]')).toHaveCount(0)

    // The second row is gone — no leftover branch bar anywhere in the panel.
    await expect(page.locator('.branch-bar')).toHaveCount(0)
  })

  test('opens the search field in place and closes it again', async ({ page }) => {
    const field = page.locator('.git-search-row .search-pill input')
    await expect(field).toHaveCount(0)

    await page.locator('.git-actions [data-action="search"]').click()

    // Same row, different contents: the toolbar is replaced, not stacked.
    await expect(field).toBeVisible()
    await expect(page.locator('.git-toolbar')).toHaveCount(0)

    await page.locator('.search-cancel').click()
    await expect(field).toHaveCount(0)
    await expect(page.locator('.git-toolbar')).toBeVisible()
  })

  test('filters from the expanded field and restores the list on close', async ({ page }) => {
    const rows = page.locator('.commit-list-content .drilldown-item')
    await expect(rows.first()).toBeVisible()

    await page.locator('.git-actions [data-action="search"]').click()
    await page.locator('.git-search-row input').fill('zzz-no-such-commit')

    await expect(rows).toHaveCount(0)

    // Closing clears the query: a filter you can no longer see would silently
    // keep hiding rows. The exact row count is not asserted — the parent
    // re-fetches the unfiltered history, so the page that comes back is not
    // necessarily the one that was on screen before.
    await page.locator('.search-cancel').click()
    await expect.poll(() => rows.count()).toBeGreaterThan(0)
    await expect(page.locator('.git-toolbar')).toBeVisible()
  })

  test('lists the local branch count inside the picker', async ({ page }) => {
    // The sentence that used to occupy the whole second row is now the menu's
    // header, which is where it is actually needed.
    await page.locator('.branch-chip').click()
    await expect(page.locator('.branch-menu-label')).toContainText(/\d/)
    await expect(page.locator('.branch-menu-item').first()).toBeVisible()
  })
})

import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { projectApiFetch } from '../helpers/auth'

/**
 * The chat input's "@" menu can find files ANYWHERE in the project, not just the
 * ones the user has already touched (recent files / current directory / recent
 * uploads and shares).
 *
 * That path spans three layers, and each is only covered by a real browser:
 *   - GET /api/dir/search/top ranks by fuzzy score and truncates (Go unit tests
 *     cover the ranking; they cannot prove the route is reachable or that the
 *     project cookie is honoured);
 *   - useAtFileSearch debounces and drops stale responses (vitest, with a mocked
 *     apiGet — it never sees a real 200);
 *   - the composer merges those hits into the popup as the lowest-priority
 *     source and attaches the picked path (vitest, with a mocked store).
 *
 * The E2E server's project root IS this repo, so a query for a file that exists
 * only deep in the tree is a real end-to-end probe: no local source can offer it,
 * because nothing in the session has opened it.
 *
 * The pick attaches a FILE CARD, which is what the user actually gets —
 * asserting only that a row rendered would still pass if selection were wired to
 * the wrong handler.
 */
test.describe('chat @ menu — project-wide search', () => {
  test.setTimeout(60000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible({ timeout: 15000 })
  })

  test('finds a deep project file and attaches it', async ({ page }) => {
    await chat.textarea.click()
    // "dirsearch" matches only internal/handler/dir_search.go (fuzzy matching
    // ignores the underscore and case). Nothing in this session has opened it,
    // so it cannot arrive from a local source.
    await chat.textarea.fill('@dirsearch')

    // Hits arrive after a 250ms debounce plus the request, so wait on the row
    // itself rather than on a timer.
    const globalRows = page.locator('.completion-item--global')
    await expect(globalRows.first()).toBeVisible({ timeout: 15000 })

    const target = globalRows.filter({ hasText: 'dir_search.go' }).first()
    await expect(target).toBeVisible()
    // The row is labelled as a project-wide hit, so the user can tell it apart
    // from a recent file that happens to share the name.
    await expect(target.locator('.completion-source')).toBeVisible()
    // The description disambiguates same-basename hits by showing the directory.
    await expect(target.locator('.completion-desc')).toContainText('internal/handler')

    await target.click({ force: true })

    // Picking a file row attaches it as a reference card (the same payload a
    // file-manager pick produces) and removes the trigger text. Scoped to THIS
    // file: the shared session may already carry chips from earlier specs.
    const chip = page.locator('.chat-input-wrapper .chat-file-attachment.attachment-ref')
      .filter({ hasText: 'dir_search.go' })
    await expect(chip).toBeVisible({ timeout: 5000 })
    await expect(chat.textarea).toHaveValue('')

    // Leave the composer as we found it. The menu stays open after a file pick
    // (browsing several files is the point), and its popup overlays the chip —
    // so dismiss it first, or the close click lands on the popup instead.
    await page.keyboard.press('Escape')
    await expect(page.locator('.completion-item')).toHaveCount(0)
    await chip.locator('.attachment-close-btn').click()
    await expect(chip).toHaveCount(0)
    await chat.textarea.fill('')
  })

  test('a bare @ shows no project-wide hits', async ({ page }) => {
    // Typing "@" then a query, then clearing back to a bare "@" must not leave
    // the previous query's hits on screen: they belong to a word the user has
    // already erased.
    await chat.textarea.click()
    await chat.textarea.fill('@dirsearch')
    await expect(page.locator('.completion-item--global').first()).toBeVisible({ timeout: 15000 })

    await chat.textarea.fill('@')
    await expect(page.locator('.completion-item--global')).toHaveCount(0)
    await chat.textarea.fill('')
  })

  test('the endpoint ranks before truncating and caps the page', async () => {
    // Guards the contract the menu relies on: one JSON payload of the BEST few
    // hits, not a walk-order stream. A first-N implementation would return
    // whatever the filesystem happened to visit first, and the popup would show
    // unrelated files.
    //
    // projectApiFetch (not the `request` fixture) because this endpoint needs
    // the session AND project cookies; without them it 403s.
    const resp = await projectApiFetch('/api/dir/search/top?q=dirsearch&limit=3')
    expect(resp.ok).toBe(true)
    const body = (await resp.json()) as { items?: Array<{ path?: string; type?: string }>; total?: number }

    expect(Array.isArray(body.items)).toBe(true)
    expect(body.items!.length).toBeLessThanOrEqual(3)
    // total is the pre-limit count, so the menu can tell "all shown" from
    // "there are more".
    expect(typeof body.total).toBe('number')
    expect(body.total!).toBeGreaterThanOrEqual(body.items!.length)
    expect(body.items!.some(i => (i.path || '').endsWith('dir_search.go'))).toBe(true)
  })
})

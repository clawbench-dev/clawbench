import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Session search → jump to the matched message (issue #580).
 *
 * The chain this exercises end-to-end:
 *
 *   POST /api/rag/session-search returns ChunkHit.message_id
 *   → SessionSearchDrawer renders a "jump to this message" button per hit
 *   → App parks setPendingMessageNavigation(sessionId, messageId, 'search')
 *     BEFORE opening the session
 *   → ChatPanelContent consumes it and reaches the message by PAGING
 *     (jumpToUserMessage), not by the loaded-window-only scrollToMessage
 *
 * Why this needs an e2e rather than only unit tests: every link above was
 * individually correct while the feature did nothing, because the message id
 * was dropped between the drawer and the app. The unit suites pin each piece
 * and `sessionSearchJumpWiring.test.ts` pins the wiring in source, but only a
 * real search → click → assert-visible path proves the id survives the trip
 * through the store, the router and the paging loop.
 *
 * FTS is always on (see model.RAGConfig — the `rag.enabled` key the e2e config
 * writes is not read by any code), so no embedding service is needed here.
 */
test.describe.serial('Session search jump', () => {
  const chat = (page: Parameters<typeof ChatPage>[0]) => new ChatPage(page)

  // The indexer polls on a 5s cycle, so waitForIndexed's own 40s budget has to
  // fit inside the test budget. Playwright's default is 30s, which made the
  // inner deadline unreachable: a slow index surfaced as "Test timeout of
  // 30000ms exceeded" inside page.waitForTimeout instead of the assertion that
  // says what actually went wrong.
  test.setTimeout(120000)

  /**
   * The id of the session the chat panel currently shows.
   *
   * Read from GET /api/ai/chat, NOT from `/api/ai/sessions[0]`: that list is
   * ordered by pinned / sort_order / created_at, so any session pinned or
   * re-ordered by an earlier spec sorts above the one this test just used. The
   * search is then scoped to a stranger's session and can never match.
   */
  async function currentSessionId(page: Parameters<typeof ChatPage>[0]): Promise<string> {
    return page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat')
      const data = await resp.json()
      return data.sessionId as string
    })
  }

  /**
   * Wait until the indexer has caught up, then poll the search API.
   *
   * The indexer runs on a 5s poll, and until it has written the FTS rows a
   * content search silently degrades to the TITLE channel: the result comes
   * back with `title_only: true` and `chunks: []`, so there is no message id to
   * jump to. Waiting only on the search API therefore never converges — it
   * keeps returning the title-only shape — so gate on `/api/rag/status` first.
   */
  async function waitForIndexed(
    page: Parameters<typeof ChatPage>[0],
    sessionId: string,
    query: string,
    timeout = 40000,
  ): Promise<{ messageId: number } | null> {
    const deadline = Date.now() + timeout
    while (Date.now() < deadline) {
      const status = await page.evaluate(async () => {
        const resp = await fetch('/api/rag/status')
        return resp.ok ? await resp.json() : null
      })
      if (status?.has_fts_data && status.indexed_messages > 0) break
      await page.waitForTimeout(1000)
    }

    while (Date.now() < deadline) {
      const hit = await page.evaluate(async ({ q, sid }) => {
        const resp = await fetch('/api/rag/session-search', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ q, session_id: sid, archived: 'all' }),
        })
        if (!resp.ok) return null
        const data = await resp.json()
        const session = (data.sessions || []).find((s: { session_id: string }) => s.session_id === sid)
        // `title_only` results carry no chunks — that is the pre-index shape.
        if (session?.title_only) return null
        const chunk = session?.chunks?.[0]
        return chunk?.message_id ? { messageId: chunk.message_id } : null
      }, { q: query, sid: sessionId })
      if (hit) return hit
      await page.waitForTimeout(500)
    }
    return null
  }

  test('jumping from a search hit lands on the matched message', async ({ page }) => {
    await page.goto('/')
    const ui = chat(page)
    await expect(ui.textarea).toBeVisible({ timeout: 20000 })

    // A unique token so the FTS query can only match this message.
    const token = `zqx${Date.now().toString(36)}`
    const body = `the ${token} marker lives in this message`

    await chat(page).fillInput(body)
    const countBefore = await chat(page).sendMessage(body)
    await chat(page).waitForReply(30000, countBefore)
    const sessionId = await currentSessionId(page)
    expect(sessionId, 'a session must exist after sending').toBeTruthy()

    // The indexer polls asynchronously; wait for the chunk to appear rather
    // than assuming a fixed delay.
    const indexed = await waitForIndexed(page, sessionId!, token)
    expect(indexed, `the sent message must become searchable (token ${token})`).not.toBeNull()

    // Scroll the chat away from the bottom so "did it actually jump?" is
    // observable: a jump that silently does nothing would leave the list where
    // it was.
    await page.locator('.chat-messages').evaluate((el) => { el.scrollTop = 0 })

    // Open session search from the list header and search the token.
    await page.locator('[data-action="search"]').first().click()
    const sheet = page.locator('.session-search-sheet')
    await expect(sheet).toBeVisible()
    // Scope to the search row's text input: the sheet also contains the
    // date-range inputs (type="date") once that filter is expanded, and a bare
    // `input` locator would be ambiguous.
    const queryInput = sheet.locator('.session-search-input-row input[type="text"]')
    await expect(queryInput).toBeVisible()
    // Type rather than `fill`: SearchInput is a controlled component
    // (`:value` + `@input`), and `fill()` sets the DOM value in one shot, which
    // the component's re-render can revert before Vue sees an input event — the
    // box looked filled in the screenshot while the app was still showing the
    // BROWSE list (mode=recent, chunks=0). Typing emits one input event per
    // character, which is what a user does and what the debounce expects.
    await queryInput.click()
    await queryInput.pressSequentially(token, { delay: 20 })
    await expect(queryInput).toHaveValue(token)

    // The sheet opens in BROWSE mode (an empty query lists recent sessions) and
    // switches to content search once the debounced query lands. Waiting for
    // the result row alone is not enough: a browse row looks identical, but it
    // carries no chunks, so its detail view has no per-hit jump button — and
    // clicking it would test nothing. The mode badge only renders for a real
    // search, so it is the signal that the list is the searched one.
    await expect(page.locator('.session-search-mode')).toBeVisible({ timeout: 20000 })

    // The result row for our session, then its per-hit jump button.
    const result = page.locator('.session-search-item').first()
    await expect(result).toBeVisible({ timeout: 15000 })
    await result.click()

    const jump = page.locator('.detail-chunk-jump').first()
    await expect(jump, 'each search hit must offer a jump to its message').toBeVisible({ timeout: 15000 })
    await jump.click()

    // The matched message must be on screen and flashed. The highlight class is
    // applied by _scrollAndHighlight in useUserMsgIndex.
    const target = page.locator('.chat-message.user', { hasText: token })
    await expect(target).toBeVisible({ timeout: 15000 })
    await expect(target).toHaveClass(/chat-message-highlight/, { timeout: 10000 })

    // And the sheet must be gone — the jump replaces it, it does not stack.
    await expect(sheet).toBeHidden()
  })

  test('the jump button is hidden for archived sessions', async ({ page }) => {
    // Opening an archived session 404s (the history endpoint filters archived
    // rows), and the chat panel's recovery branch then silently switches to a
    // DIFFERENT session — so offering the jump there would send the reader
    // somewhere unrelated.
    await page.goto('/')
    await expect(chat(page).textarea).toBeVisible({ timeout: 20000 })

    const token = `arc${Date.now().toString(36)}`
    await chat(page).fillInput(`archived probe ${token}`)
    const countBefore = await chat(page).sendMessage(`archived probe ${token}`)
    await chat(page).waitForReply(30000, countBefore)

    const sessionId = await currentSessionId(page)
    expect(sessionId).toBeTruthy()

    const indexed = await waitForIndexed(page, sessionId!, token)
    expect(indexed).not.toBeNull()

    // Archive it, then search with the archived filter so it still appears.
    await page.evaluate(async (sid) => {
      await fetch(`/api/ai/session/archive?session_id=${sid}`, { method: 'DELETE' })
    }, sessionId)

    await page.locator('[data-action="search"]').first().click()
    const sheet = page.locator('.session-search-sheet')
    await expect(sheet).toBeVisible()

    // Switch the status filter to "archived" BEFORE typing. The drawer keeps
    // the previous selection, and an earlier spec leaves it on `all` or on a
    // type filter that excludes this row; the test then searched a list that
    // could never contain the archived session. The result list stayed empty,
    // and `.session-search-mode` — rendered only inside the non-empty results
    // block — never appeared (firefox: "element(s) not found" after 20 s).
    //
    // Located by title, not by index: there are four dropdown buttons in this
    // row and the status one is second.
    await sheet.locator('.filter-dropdown-btn[title="状态"], .filter-dropdown-btn[title="Status"]').click()
    await page.locator('.filter-menu-item[data-value="archived"]').click()

    const archivedQuery = sheet.locator('.session-search-input-row input[type="text"]')
    await archivedQuery.click()
    await archivedQuery.pressSequentially(token, { delay: 20 })
    await expect(page.locator('.session-search-mode')).toBeVisible({ timeout: 20000 })

    const result = page.locator('.session-search-item').first()
    await expect(result).toBeVisible({ timeout: 15000 })
    await result.click()

    // The chunk list renders, but no jump button on an archived session.
    await expect(page.locator('.detail-chunk').first()).toBeVisible({ timeout: 15000 })
    await expect(page.locator('.detail-chunk-jump')).toHaveCount(0)
  })
})

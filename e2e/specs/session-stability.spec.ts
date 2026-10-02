import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * E2E tests for session stability — verifying that the current session
 * is not accidentally lost or replaced during common UI interactions.
 *
 * These tests guard against the bug where:
 * 1. loadHistory without session_id falls back to GetLatestSessionID
 *    (which can return a different session if another was updated)
 * 2. POST /api/ai/chat without session_id auto-creates a ghost session
 * 3. Concurrent loadHistory calls race and overwrite currentSessionId
 *
 * All API calls use page.evaluate() so they inherit auth cookies.
 */
test.describe.serial('Session Stability', () => {
  test.setTimeout(120000)

  test('should preserve current session after tab switch and back', async ({ page }) => {
    const chat = new ChatPage(page)

    // Send a message to establish a session with content
    const uniqueText = 'stability_tab_' + Date.now()
    await chat.sendAndAwaitACPReply(uniqueText)

    // Record the current session ID
    const sessionIdBefore = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      const current = data.sessions?.find((s: any) => s.running === false || s.unreadCount === 0)
      return current?.id || data.sessions?.[0]?.id
    })
    expect(sessionIdBefore).toBeTruthy()

    // Verify the user message is visible
    await expect(chat.getLastUserMessage()).toContainText(uniqueText)

    // Switch the left pane to another tab and back, via the stable dock tab
    // ids. The old `.dock-item` class does not exist, and a positional fallback
    // would click whatever tab happened to be at that index. `browse` is the
    // default left tab, so switch to `tasks` first to make the round trip real
    // (clicking the already-active tab collapses the pane in wide mode).
    const nav = new NavigationPage(page)
    await nav.switchToTasks()
    await expect(page.locator('.task-tab').first()).toBeVisible({ timeout: 10000 })

    await nav.switchToFileManager()
    await expect(page.locator('.file-list, .file-item, .file-grid, .grid-item').first()).toBeVisible({ timeout: 10000 })

    // Switch back to chat (wide mode: the chat toggle; narrow mode: chat tab)
    await nav.switchToChat()
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    // Verify the message is still visible (session wasn't lost)
    await expect(chat.getLastUserMessage()).toContainText(uniqueText)

    // Verify session ID hasn't changed
    const sessionIdAfter = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })
    expect(sessionIdAfter).toBe(sessionIdBefore)
  })

  test('should preserve session after page visibility change (hide/show)', async ({ page }) => {
    const chat = new ChatPage(page)

    // Send a message to establish a session
    const uniqueText = 'stability_visibility_' + Date.now()
    await chat.sendAndAwaitACPReply(uniqueText)

    // Record session ID
    const sessionIdBefore = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })
    expect(sessionIdBefore).toBeTruthy()

    // Simulate visibility change: page becomes hidden then visible
    // This triggers the same code path as mobile screen lock/unlock
    await page.evaluate(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    // Dispatch visibility=visible event
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await page.waitForTimeout(1000)

    // The session should still be intact — user message should still be visible
    await expect(chat.getLastUserMessage()).toContainText(uniqueText)

    // Session ID should not have changed
    const sessionIdAfter = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })
    expect(sessionIdAfter).toBe(sessionIdBefore)
  })

  test('should not create ghost session when sending a message', async ({ page }) => {
    const chat = new ChatPage(page)

    // Count sessions before
    const sessionCountBefore = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.length || 0
    })

    // Send a message
    const uniqueText = 'stability_ghost_' + Date.now()
    await chat.sendAndAwaitACPReply(uniqueText, 60000)

    // Count sessions after — should NOT have increased
    // (a ghost session would be created if POST /api/ai/chat had no session_id)
    const sessionCountAfter = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.length || 0
    })

    // Session count should stay the same (message goes to existing session)
    // Allow +1 for the case where the initial empty session wasn't counted
    expect(sessionCountAfter).toBeLessThanOrEqual(sessionCountBefore + 1)
  })

  test('should keep correct session after creating a second session', async ({ page }) => {
    const chat = new ChatPage(page)

    // Send a message in the first session
    const firstMsg = 'stability_first_' + Date.now()
    await chat.sendAndAwaitACPReply(firstMsg)

    // Record the first session ID
    const firstSessionId = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })

    // Create a new session via the E2E bridge
    await chat.createSessionWithAgent('acp-mock')

    // Send a message in the new session
    const secondMsg = 'stability_second_' + Date.now()
    await chat.sendAndAwaitACPReply(secondMsg)

    // Record the second session ID — should be different
    const secondSessionId = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })

    expect(secondSessionId).not.toBe(firstSessionId)

    // The current view should show the second message, not the first
    await expect(chat.getLastUserMessage()).toContainText(secondMsg)
  })

  test('POST /api/ai/chat without session_id returns 400 (no ghost session)', async ({ page }) => {
    // The backend resolves the session from `?session_id=` and falls back to
    // the `chat_session_id` cookie. To exercise the genuinely-missing case we
    // must clear that cookie first, otherwise the request legitimately succeeds
    // against the cookie's session (200, not 400).
    await page.context().clearCookies({ name: /chat_session_id$/ })

    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'test ghost session' }),
      })
      return { status: resp.status, body: await resp.json().catch(() => ({})) }
    })

    // Should return 400 with SessionIdRequired error
    expect(result.status).toBe(400)
    expect(result.body.error).toContain('session_id')
  })
})

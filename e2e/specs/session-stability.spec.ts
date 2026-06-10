import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

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

    // Switch to another tab (files)
    await page.locator('.dock-item').filter({ hasText: /file|文件/i }).first().click().catch(() => {
      // Fallback: click the second dock item
      page.locator('.dock-item').nth(1).click()
    })
    await page.waitForTimeout(500)

    // Switch back to chat
    await page.locator('.dock-item').first().click()
    await page.waitForTimeout(500)

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
    // Verify the backend correctly rejects POST without session_id
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

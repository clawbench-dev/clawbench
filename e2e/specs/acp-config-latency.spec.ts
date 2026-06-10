import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for ACP config option (mode/thinkingEffort) latency and non-blocking behavior.
 *
 * Regression test for: PATCH /api/ai/session/update was calling
 * conn.SetSessionConfigOption() synchronously in the HTTP handler, which
 * could block for up to 30 seconds if the ACP agent was slow. This tied up
 * browser HTTP/1.1 connections and prevented the session list from loading.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which provides:
 * - 3 modes (Code, Plan, Bypass Permissions)
 * - 3 thinking effort levels (Low/Medium/High)
 *
 * Key behaviors tested:
 * 1. PATCH /api/ai/session/update returns quickly (< 2s) regardless of agent speed
 * 2. Session list (GET /api/ai/sessions) is not blocked during config updates
 * 3. Mode setting eventually takes effect (eventual consistency)
 * 4. Thinking effort setting eventually takes effect
 * 5. Concurrent PATCH + chat POST does not deadlock
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Config Latency & Non-Blocking', () => {
  test.setTimeout(120000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  // ───────────────────────────────────────────────────────
  // PATCH response time
  // ───────────────────────────────────────────────────────

  test('PATCH session/update should return within 2 seconds for mode', async ({ page }) => {
    // Establish ACP connection first
    await chat.sendAndAwaitACPReply('hi')
    await chat.waitForACPState()

    // Get current session ID
    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId }
    })
    expect(sessionInfo.ok).toBe(true)
    expect(sessionInfo.sessionId).toBeTruthy()

    // PATCH mode — should return quickly even though ACP RPC is async
    const start = Date.now()
    const patchResult = await page.evaluate(async (sessionId) => {
      const start = Date.now()
      const resp = await fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ modeId: 'plan' }),
      })
      const elapsed = Date.now() - start
      return { ok: resp.ok, status: resp.status, elapsed }
    }, sessionInfo.sessionId)

    expect(patchResult.ok).toBe(true)
    expect(patchResult.elapsed).toBeLessThan(2000)
  })

  test('PATCH session/update should return within 2 seconds for thinkingEffort', async ({ page }) => {
    // ACP connection already warm from previous test
    await chat.waitForACPState()

    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId }
    })
    expect(sessionInfo.ok).toBe(true)

    const patchResult = await page.evaluate(async (sessionId) => {
      const start = Date.now()
      const resp = await fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ thinkingEffort: 'high' }),
      })
      const elapsed = Date.now() - start
      return { ok: resp.ok, status: resp.status, elapsed }
    }, sessionInfo.sessionId)

    expect(patchResult.ok).toBe(true)
    expect(patchResult.elapsed).toBeLessThan(2000)
  })

  // ───────────────────────────────────────────────────────
  // Session list not blocked during config update
  // ───────────────────────────────────────────────────────

  test('session list should load within 2s even during config update', async ({ page }) => {
    await chat.waitForACPState()

    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId }
    })
    expect(sessionInfo.ok).toBe(true)

    // Fire PATCH (async in background) and immediately fetch session list
    // Both should complete quickly — PATCH should not block the session list
    const result = await page.evaluate(async (sessionId) => {
      // Start PATCH (don't await — let it run in background)
      const patchPromise = fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ modeId: 'code' }),
      })

      // Immediately try to load session list
      const listStart = Date.now()
      const listResp = await fetch('/api/ai/sessions')
      const listElapsed = Date.now() - listStart
      const listData = await listResp.json()

      // Now await PATCH to clean up
      await patchPromise

      return {
        listOk: listResp.ok,
        listElapsed,
        sessionCount: (listData.sessions || []).length,
      }
    }, sessionInfo.sessionId)

    expect(result.listOk).toBe(true)
    expect(result.listElapsed).toBeLessThan(2000)
    // Should have at least 1 session
    expect(result.sessionCount).toBeGreaterThanOrEqual(1)
  })

  // ───────────────────────────────────────────────────────
  // Eventual consistency — mode/thinkingEffort persist
  // ───────────────────────────────────────────────────────

  test('mode setting should eventually persist after PATCH', async ({ page }) => {
    await chat.waitForACPState()

    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId }
    })
    expect(sessionInfo.ok).toBe(true)

    // Set mode via PATCH
    const patchResult = await page.evaluate(async (sessionId) => {
      const resp = await fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ modeId: 'plan' }),
      })
      return { ok: resp.ok }
    }, sessionInfo.sessionId)
    expect(patchResult.ok).toBe(true)

    // Wait for mode to be persisted (eventual consistency)
    await chat.waitForSessionMode('plan', 10000)
  })

  test('thinkingEffort setting should eventually persist after PATCH', async ({ page }) => {
    await chat.waitForACPState()

    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId }
    })
    expect(sessionInfo.ok).toBe(true)

    // Set thinkingEffort via PATCH
    const patchResult = await page.evaluate(async (sessionId) => {
      const resp = await fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ thinkingEffort: 'high' }),
      })
      return { ok: resp.ok }
    }, sessionInfo.sessionId)
    expect(patchResult.ok).toBe(true)

    // Wait for thinkingEffort to be persisted
    await chat.waitForSessionThinkingEffort('high', 10000)
  })

  // ───────────────────────────────────────────────────────
  // Concurrent PATCH + chat no deadlock
  // ───────────────────────────────────────────────────────

  test('concurrent PATCH mode + chat message should not deadlock', async ({ page }) => {
    await chat.waitForACPState()

    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId, agentId: data.agentId }
    })
    expect(sessionInfo.ok).toBe(true)

    // Fire PATCH mode and POST chat concurrently
    const result = await page.evaluate(async ({ sessionId, agentId }) => {
      // Start PATCH mode (async — don't await)
      const patchPromise = fetch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ modeId: 'code' }),
      }).then(r => ({ ok: r.ok, elapsed: 0 }))

      // Immediately send a chat message
      const chatStart = Date.now()
      const chatResp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: 'test concurrent patch and chat',
          modelId: '',
          thinkingEffort: '',
          modeId: 'code',
          transport: 'acp-stdio',
        }),
      })
      const chatElapsed = Date.now() - chatStart
      const chatData = await chatResp.json()

      // Await PATCH
      const patchResult = await patchPromise

      return {
        patchOk: patchResult.ok,
        chatOk: chatResp.ok,
        chatStarted: chatData.started || chatData.running || false,
        chatElapsed,
      }
    }, { sessionId: sessionInfo.sessionId, agentId: sessionInfo.agentId })

    // Both should succeed without deadlock
    expect(result.patchOk).toBe(true)
    expect(result.chatOk).toBe(true)
    expect(result.chatStarted).toBe(true)
    // Chat POST should return quickly (it just starts the goroutine)
    expect(result.chatElapsed).toBeLessThan(5000)

    // Wait for the chat to complete
    const apiTimeout = 60000
    const start = Date.now()
    while (Date.now() - start < apiTimeout) {
      try {
        const isRunning = await page.evaluate(async () => {
          const resp = await fetch('/api/ai/sessions')
          if (!resp.ok) return true
          const data = await resp.json()
          return (data.sessions || []).some((s: any) => s.running === true)
        })
        if (!isRunning) break
      } catch {
        // retry
      }
      await page.waitForTimeout(1000)
    }
  })
})

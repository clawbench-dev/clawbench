import { test, expect } from '../fixtures'

/**
 * E2E for single-chat auto-cruise against the REAL backend path.
 *
 * Single chat has ONE "hand the floor back" point: the reply finishing. With
 * cruise on, that no longer ends the run — the backend injects a hidden
 * confirmation round and only stops when the AI declares
 * <clawbench-goal-satisfied/>. The mock (cmd/acp-mock) declares it whenever a
 * prompt carries the confirmation marker "[自动巡航·完成确认]", which is the SAME
 * marker the group cruise uses, so no mock change is needed.
 *
 * The fixtures authenticate the page and set the project cookie, so fetch()
 * from page.evaluate runs as a logged-in client on the active project.
 */
test.describe.serial('single-chat auto-cruise', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial test')
  test.setTimeout(120000)

  test('cruise keeps the AI confirming until it declares the goal met', async ({ page }) => {
    const result = await page.evaluate(async () => {
      // A single-chat session on the default acp-mock agent.
      const created = await (await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: 'acp-mock' }),
      })).json()
      const sessionId: string = created.sessionId
      if (!sessionId) return { ok: false, step: 'create', created }

      // Enable cruise via the session PATCH.
      const patched = await (await fetch('/api/ai/session/update', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, cruise: true }),
      })).json()
      if (!patched.ok) return { ok: false, step: 'patch', patched }

      // The GET response must report cruise=true (server-authoritative).
      const hist0 = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`)).json()
      if (hist0.cruise !== true) return { ok: false, step: 'getCruise', cruise: hist0.cruise }

      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '做个任务', queueId: 'scq-1', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 90000
      let msgs: Array<{ role: string; streaming?: boolean; content?: string }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}&limit=80`)).json()
        msgs = hist.messages || []
        const stillStreaming = msgs.some(m => m.streaming)
        const done = msgs.some(m => m.role === 'system' && typeof m.content === 'string' &&
          (m.content.includes('任务已确认完成') || m.content.includes('task confirmed complete')))
        if (done && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }
      // System lines are localized at WRITE time from the server's active
      // language (the browser locale = English in the harness), so match BOTH.
      const has = (m: { content?: string }, zh: string, en: string) =>
        typeof m.content === 'string' && (m.content.includes(zh) || m.content.includes(en))
      const confirming = msgs.filter(m => m.role === 'system' && has(m, '正在确认任务完成情况', 'checking whether the task is complete'))
      const done = msgs.filter(m => m.role === 'system' && has(m, '任务已确认完成', 'task confirmed complete'))
      return { ok: true, sessionId, confirming: confirming.length, done: done.length }
    })

    expect(result.ok).toBe(true)
    // A confirmation round ran and the run ended confirmed.
    expect(result.confirming).toBeGreaterThanOrEqual(1)
    expect(result.done).toBeGreaterThanOrEqual(1)
  })

  test('cruise off: a plain reply ends the run with no confirmation round', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const created = await (await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: 'acp-mock' }),
      })).json()
      const sessionId: string = created.sessionId
      if (!sessionId) return { ok: false, step: 'create', created }

      // Cruise stays off (default).
      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '你好', queueId: 'scq-2', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 60000
      let msgs: Array<{ role: string; streaming?: boolean; content?: string }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}&limit=80`)).json()
        msgs = hist.messages || []
        if (msgs.length > 0 && !msgs.some(m => m.streaming)) break
        await new Promise(r => setTimeout(r, 1000))
      }
      const has = (m: { content?: string }, zh: string, en: string) =>
        typeof m.content === 'string' && (m.content.includes(zh) || m.content.includes(en))
      const confirming = msgs.filter(m => m.role === 'system' && has(m, '正在确认任务完成情况', 'checking whether the task is complete'))
      return { ok: true, confirming: confirming.length }
    })

    expect(result.ok).toBe(true)
    // With cruise off, the reply finishes the run — no confirmation round.
    expect(result.confirming).toBe(0)
  })
})

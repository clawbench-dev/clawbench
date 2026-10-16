import { test, expect } from '../fixtures'

/**
 * E2E for auto-cruise group mode (design §15) against the REAL backend path.
 *
 * Cruise is an orthogonal switch: when on, the loop no longer hands the floor
 * back to the human at the usual stop points (host end tag / drained free relay
 * / @User / question). Instead it runs a confirmation round and only ends when
 * the goal is declared satisfied. The mock (cmd/acp-mock) declares
 * <clawbench-goal-satisfied/> whenever a prompt carries the confirmation marker
 * "[自动巡航·完成确认]", so a cruising turn terminates deterministically.
 *
 * The fixtures authenticate the page and set the project cookie, so fetch()
 * from page.evaluate runs as a logged-in client on the active project.
 */
test.describe.serial('AI group chat auto-cruise', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('host-mode cruise: the end tag triggers a confirmation and the turn ends confirmed', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      // Host mode: acp-mock is the host, acp-mock-b a member.
      const created = await post('/api/group/create', { hostAgentId: 'acp-mock', memberAgentIds: ['acp-mock', 'acp-mock-b'] })
      if (!created.ok) return { ok: false, step: 'create', created }
      if (created.mode !== 'host') return { ok: false, step: 'mode', mode: created.mode }

      // Enable cruise via the settings PATCH.
      const settings = await (await fetch('/api/group/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ groupId: created.groupId, cruise: true }),
      })).json()
      if (!settings.ok) return { ok: false, step: 'settings', settings }

      // The roster must report cruise=true.
      const members = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()
      if (members.cruise !== true) return { ok: false, step: 'membersCruise', cruise: members.cruise }
      // Hard default: enabling cruise forces auto-approve on for the group
      // (cruise runs unattended, so a permission prompt would stall the loop).
      const groupSession = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`)).json()
      if (groupSession.autoApprove !== true) return { ok: false, step: 'cruiseForcesAutoApprove', autoApprove: groupSession.autoApprove }

      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '做个任务', queueId: 'cq-1', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 90000
      let msgs: Array<{ role: string; agentId?: string; streaming?: boolean; content?: string }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}&limit=80`)).json()
        msgs = hist.messages || []
        const stillStreaming = msgs.some(m => m.streaming)
        // The cruise-complete system line marks the confirmed end (localized).
        const done = msgs.some(m => m.role === 'system' && typeof m.content === 'string' &&
          (m.content.includes('任务已确认完成') || m.content.includes('task confirmed complete')))
        if (done && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }
      // The system lines are localized at WRITE time from the server's active
      // language (model.Language), which the browser sets from its own locale —
      // English in the E2E harness. Match BOTH languages so the spec does not
      // depend on the harness locale.
      const has = (m: { content?: string }, zh: string, en: string) =>
        typeof m.content === 'string' && (m.content.includes(zh) || m.content.includes(en))
      const confirming = msgs.filter(m => m.role === 'system' && has(m, '正在向成员确认', 'checking with the members'))
      const done = msgs.filter(m => m.role === 'system' && has(m, '任务已确认完成', 'task confirmed complete'))
      // "Your turn" must NOT be announced in cruise mode.
      const yourTurn = msgs.filter(m => m.role === 'system' && has(m, '轮到用户发言', "It's your turn to speak"))
      return { ok: true, groupId: created.groupId as string, confirming: confirming.length, done: done.length, yourTurn: yourTurn.length }
    })

    expect(result.ok).toBe(true)
    // The confirmation round ran and the turn ended confirmed.
    expect(result.confirming).toBeGreaterThanOrEqual(1)
    expect(result.done).toBeGreaterThanOrEqual(1)
    // Cruise never hands the floor back to the human.
    expect(result.yourTurn).toBe(0)
  })

  test('free-mode cruise: the relay draining triggers a broadcast confirmation and the turn ends confirmed', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      // Free mode: no host, two members.
      const created = await post('/api/group/create', { memberAgentIds: ['acp-mock', 'acp-mock-b'] })
      if (!created.ok) return { ok: false, step: 'create', created }
      if (created.mode !== 'free') return { ok: false, step: 'mode', mode: created.mode }

      const settings = await (await fetch('/api/group/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ groupId: created.groupId, cruise: true }),
      })).json()
      if (!settings.ok) return { ok: false, step: 'settings', settings }

      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '大家聊聊', queueId: 'cq-2', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 90000
      let msgs: Array<{ role: string; agentId?: string; streaming?: boolean; content?: string }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}&limit=80`)).json()
        msgs = hist.messages || []
        const stillStreaming = msgs.some(m => m.streaming)
        const done = msgs.some(m => m.role === 'system' && typeof m.content === 'string' &&
          (m.content.includes('任务已确认完成') || m.content.includes('task confirmed complete')))
        if (done && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }
      const has = (m: { content?: string }, zh: string, en: string) =>
        typeof m.content === 'string' && (m.content.includes(zh) || m.content.includes(en))
      const confirming = msgs.filter(m => m.role === 'system' && has(m, '正在向成员确认', 'checking with the members'))
      const done = msgs.filter(m => m.role === 'system' && has(m, '任务已确认完成', 'task confirmed complete'))
      return { ok: true, confirming: confirming.length, done: done.length }
    })

    expect(result.ok).toBe(true)
    expect(result.confirming).toBeGreaterThanOrEqual(1)
    expect(result.done).toBeGreaterThanOrEqual(1)
  })
})

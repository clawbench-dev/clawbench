import { test, expect } from '../fixtures'

/**
 * E2E for the multi-agent group loop against the REAL backend path.
 *
 * The unit tests in internal/service drive the orchestrator with a scripted
 * runner; this spec runs the production `defaultRunner` → runTurn → ACP path
 * end to end, so it is the only place that proves a MEMBER (not just the host)
 * actually produces output through the real machinery.
 *
 * acp-mock is extended to emit a `<clawbench-speaker>` routing tag when it sees
 * the host instruction, naming the first addressable member — so the host turn
 * routes to a real member turn instead of monologuing.
 *
 * Regression target: the "only the host ever speaks" defect (host named itself,
 * every member turn became a host turn).
 */
test.describe.serial('AI group chat multi-agent loop', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('host routes to a member, and the member produces a timeline row', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }

      // Add a second acp-mock member — the host must route to it.
      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      // Cap at 1 round so the turn terminates predictably.
      await post('/api/group/settings', { groupId: created.groupId, maxRounds: 1 })

      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '请开始讨论', queueId: 'gq-1', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      // Poll the group timeline until the turn finishes (no streaming row) or a
      // timeout, then report the speaker set.
      const hostId = created.hostMemberId
      const deadline = Date.now() + 60000
      let msgs: Array<{ role: string; agentId?: string; streaming?: boolean }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}&limit=50`)).json()
        msgs = hist.messages || []
        const stillStreaming = msgs.some(m => m.streaming)
        const hasAssistant = msgs.some(m => m.role === 'assistant')
        if (hasAssistant && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }

      const speakers = new Set(msgs.filter(m => m.role === 'assistant').map(m => m.agentId || ''))
      return { ok: true, groupId: created.groupId, hostId, addedIds: added.memberIds, speakers: [...speakers], msgs }
    })

    expect(result.ok).toBe(true)
    const speakers = (result.speakers || []).filter(Boolean)
    expect(speakers.length).toBeGreaterThan(0)

    // The load-bearing assertion: at least one NON-host member produced a row.
    // Before the fix every assistant row carried the host id.
    const memberIds = new Set(result.addedIds || [])
    const memberSpoke = speakers.some((s: string) => memberIds.has(s))
    expect(memberSpoke).toBe(true)
  })
})

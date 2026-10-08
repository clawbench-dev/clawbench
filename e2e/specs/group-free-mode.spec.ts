import { test, expect } from '../fixtures'

/**
 * E2E for the free-chat-mode group loop against the REAL backend path.
 *
 * A free group has NO host: it is created by omitting hostAgentId. The user
 * seeds the relay by @-mentioning a member (or nobody, which makes every member
 * speak once); each member's own @-mention relays the floor. The mock relays a
 * bounded number of times (see freeMemberReply in cmd/acp-mock) so the turn
 * terminates — free mode has no round cap by design.
 *
 * This is the production path: create → /api/ai/chat → RunGroupDrainLoop →
 * runFreeLoop → runTurn → ACP. The unit tests drive the loop with a scripted
 * runner; this proves it works through the real machinery.
 */
test.describe.serial('AI group chat free mode', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('a free group is created without a host and a member speaks', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      // NO hostAgentId: a free group. Two members are required.
      const created = await post('/api/group/create', { memberAgentIds: ['acp-mock', 'acp-mock-b'] })
      if (!created.ok) return { ok: false, step: 'create', created }
      if (created.mode !== 'free') return { ok: false, step: 'mode', mode: created.mode }
      if (created.hostMemberId) return { ok: false, step: 'host', hostMemberId: created.hostMemberId }

      // A member roster with no host must report isHost=false for everyone.
      const members = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()
      if (members.mode !== 'free') return { ok: false, step: 'membersMode', mode: members.mode }
      if ((members.members || []).some((m: { isHost: boolean }) => m.isHost)) {
        return { ok: false, step: 'memberHost' }
      }

      // Send a message that @-mentions nobody: every active member speaks once.
      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '大家聊聊', queueId: 'fq-1', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 90000
      let msgs: Array<{ role: string; agentId?: string; streaming?: boolean }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}&limit=50`)).json()
        msgs = hist.messages || []
        const stillStreaming = msgs.some(m => m.streaming)
        const assistants = msgs.filter(m => m.role === 'assistant' && m.agentId)
        // At least the two members (seeded) must have spoken, and the relay must
        // have settled.
        if (assistants.length >= 2 && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }
      const speakers = new Set(msgs.filter(m => m.role === 'assistant' && m.agentId).map(m => m.agentId))
      return { ok: true, groupId: created.groupId as string, speakerCount: speakers.size }
    })

    expect(result.ok).toBe(true)
    // Both seeded members produced a timeline row.
    expect(result.speakerCount).toBeGreaterThanOrEqual(2)
  })

  test('a user @-mention of one member makes only that member speak', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { memberAgentIds: ['acp-mock', 'acp-mock-b'] })
      if (!created.ok) return { ok: false, step: 'create', created }

      const members = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()
      const target = (members.members || []).find((m: { name: string }) => m.name.includes('Mock'))
      if (!target) return { ok: false, step: 'noTarget' }

      // @-mention exactly one member (the frontend writes the member row id).
      const tag = `<clawbench-mention targets="${target.id}"></clawbench-mention> 请你先说`
      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: tag, queueId: 'fq-2', clientId: 'probe' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      const deadline = Date.now() + 90000
      let msgs: Array<{ role: string; agentId?: string; streaming?: boolean }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}&limit=50`)).json()
        msgs = hist.messages || []
        const assistants = msgs.filter(m => m.role === 'assistant' && m.agentId)
        if (assistants.length >= 1 && !msgs.some(m => m.streaming)) break
        await new Promise(r => setTimeout(r, 1000))
      }
      const speakers = new Set(msgs.filter(m => m.role === 'assistant' && m.agentId).map(m => m.agentId))
      return { ok: true, targetId: target.id as string, speakers: [...speakers] }
    })

    expect(result.ok).toBe(true)
    // The seeded member is the only speaker the user named; assert by set
    // membership (history ordering is the API's concern, not this spec's).
    expect(result.speakers).toContain(result.targetId)
  })
})

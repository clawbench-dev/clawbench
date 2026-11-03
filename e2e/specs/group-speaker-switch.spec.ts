import { test, expect } from '../fixtures'
import { setGroupMaxSpeeches, restoreGroupMaxSpeeches } from '../helpers/group-config'

/**
 * E2E for the group-chat subscribe-time recovery carrying the speaker.
 *
 * Regression target: switching away from a running group turn and back dropped
 * the speaker header on the still-streaming bubble. The subscribe-time recovery
 * re-emits `stream_start` so a returning client can render the live run — but
 * that re-emit omitted the speaker (`agent_id`), unlike the live broadcast. The
 * returning client therefore built a speakerless placeholder, and the
 * subsequent db_load could not heal it (the frontend merge also skipped
 * agentId), so the avatar/name stayed missing for the rest of the turn.
 *
 * This spec pins the load-bearing half deterministically at the protocol level:
 * it starts a real group turn through the HTTP handler (the same path the UI
 * uses), then opens an INDEPENDENT WebSocket and subscribes to the group session
 * MID-TURN — exactly what a session switch does — and asserts the re-emitted
 * `stream_start` frame carries `payload.agent_id` equal to the speaking member's
 * row id.
 *
 * Why protocol-level rather than DOM: the frontend's db_load-vs-stream_start
 * ordering is racy (whichever lands first wins), so a DOM assertion can pass for
 * the wrong reason — it did, until this was rewritten. The recovery frame's
 * payload is the exact contract that was broken, and it is deterministic. The
 * frontend's db_load merge half is pinned by a reducer unit test with a verified
 * mutation (chatMessageReducer.test.ts).
 *
 * API-only on purpose: no chat-panel interaction, so the spec cannot be coupled
 * to another spec's UI state (a prior spec leaving the chat panel hidden used to
 * make this fail at the session-drawer step).
 *
 * SERIAL + chromium only: shares the acp-mock subprocesses; relies on the slow
 * mock reply to hold a turn open across the subscribe.
 */
test.describe.serial('group chat subscribe-time recovery carries the speaker', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  // The cap is a global server setting; restore it so later specs are unaffected.
  test.afterAll(async () => {
    await restoreGroupMaxSpeeches()
  })

  test('re-emitted stream_start for a mid-turn subscriber carries agent_id', async ({ page }) => {
    // Several member speeches keeps the loop producing turns while the probe
    // subscribes (a global setting; the afterAll above restores it).
    await setGroupMaxSpeeches(8)

    // Create a host + member group and start a real turn via the HTTP handler.
    const setup = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }
      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      // Start the group turn through the real handler (same path as the UI).
      const send = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '请开始讨论' }),
      })
      if (send.status !== 200) return { ok: false, step: 'send', status: send.status }

      return {
        ok: true,
        groupId: created.groupId as string,
        memberRowIds: [created.hostMemberId, ...(added.memberIds || [])] as string[],
      }
    })
    expect(setup.ok).toBe(true)
    expect(setup.memberRowIds.length).toBeGreaterThanOrEqual(2)

    // Subscribe mid-turn on an INDEPENDENT socket — this is what a session
    // switch does. The backend's OnSubscribe re-emits the question + the live
    // run's stream_start to THIS client. Capture that frame and assert its
    // payload carries the speaker (member row id).
    //
    // Subscribe REPEATEDLY: a group turn has brief gaps between member turns
    // where no streaming row exists (GetLiveRunState returns 0 and nothing is
    // re-emitted). The backend does not dedupe subscribe — each one re-runs
    // OnSubscribe — so looping until a stream_start frame arrives makes the
    // probe independent of which instant we happen to land in.
    const recovery = await page.evaluate(async ({ groupId, memberRowIds }) => {
      // A per-run client id: the hub keys subscriptions by client id and keeps a
      // disconnected client's entry (with its replay buffer) for a while, so a
      // fixed id makes this probe inherit whatever the previous run left behind
      // — including a still-subscribed PREVIOUS group, whose live stream then
      // answers this subscribe with a stream_start belonging to another group.
      const clientId = `e2e-recovery-probe-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const wsUrl = `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ai/events/ws?client_id=${clientId}`
      const ws = new WebSocket(wsUrl)
      let first: Record<string, unknown> = {}
      let firstSession = ''
      let captured = false
      // Every stream_start seen, for diagnostics: a frame from ANOTHER session
      // means the probe was still subscribed somewhere it should not be.
      const seen: string[] = []
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data as string)
          if (msg.event !== 'chat_stream' || msg.data?.event_type !== 'stream_start') return
          const sid = String(msg.data?.session_id || '')
          const agent = String(msg.data?.payload?.agent_id || '')
          if (seen.length < 8) seen.push(`${sid}:${agent}`)
          if (captured || sid !== groupId) return
          first = msg.data.payload || {}
          firstSession = sid
          captured = true
        } catch { /* ignore non-JSON */ }
      }
      await new Promise<void>((resolve) => {
        if (ws.readyState === WebSocket.OPEN) return resolve()
        ws.onopen = () => resolve()
        setTimeout(resolve, 5000)
      })
      const deadline = Date.now() + 25000
      while (!captured && Date.now() < deadline) {
        ws.send(JSON.stringify({ type: 'subscribe', session_id: groupId }))
        await new Promise((r) => setTimeout(r, 1200))
      }
      try { ws.close() } catch { /* ignore */ }
      return {
        captured,
        messageId: first.message_id as number | undefined,
        agentId: (first.agent_id as string | undefined) || '',
        firstSession,
        seen,
        memberRowIds,
      }
    }, { groupId: setup.groupId, memberRowIds: setup.memberRowIds })

    // The recovery stream_start must carry the speaker, and it must be a real
    // member row id of THIS group (not empty, not the session's agent id).
    expect(recovery.captured, `a mid-turn subscribe must be answered with a stream_start (seen: ${recovery.seen.join(', ') || 'none'})`).toBe(true)
    expect(recovery.firstSession, 'the re-emitted stream_start must belong to THIS group').toBe(setup.groupId)
    expect(recovery.messageId, 'a live run must be re-emitted to a mid-turn subscriber').toBeTruthy()
    expect(recovery.agentId, 'the re-emitted stream_start must carry the speaker (agent_id)').toBeTruthy()
    expect(recovery.memberRowIds, `agentId=${recovery.agentId} members=${recovery.memberRowIds.join(',')}`).toContain(recovery.agentId)

    // Stop the group turn. It is NOT enough to wait for "no streaming row":
    // a multi-member turn has gaps between speeches where nothing is streaming
    // yet the runner is still alive, so a poll on that shape can return early
    // and leave the turn running. Every later spec then trips over it —
    // ChatPage.sendAndAwaitACPReply polls "is ANY session running" and spins
    // until its own test timeout. Cancel explicitly, then wait for the run to
    // actually clear.
    await page.evaluate(async (groupId) => {
      await fetch(`/api/ai/chat/cancel?session_id=${encodeURIComponent(groupId)}`, { method: 'POST' })
    }, setup.groupId)
    await expect
      .poll(async () => {
        return page.evaluate(async (groupId) => {
          const data = await (await fetch('/api/ai/sessions')).json()
          const s = (data.sessions || []).find((x: { id: string }) => x.id === groupId)
          return !!s?.running
        }, setup.groupId)
      }, { timeout: 90000 })
      .toBe(false)
  })
})

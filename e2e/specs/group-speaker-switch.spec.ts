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
      const wsUrl = `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ai/events/ws?client_id=e2e-recovery-probe`
      const ws = new WebSocket(wsUrl)
      let first: Record<string, unknown> = {}
      let captured = false
      ws.onmessage = (ev) => {
        if (captured) return
        try {
          const msg = JSON.parse(ev.data as string)
          if (msg.event === 'chat_stream' && msg.data?.event_type === 'stream_start') {
            first = msg.data.payload || {}
            captured = true
          }
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
        memberRowIds,
      }
    }, { groupId: setup.groupId, memberRowIds: setup.memberRowIds })

    // The recovery stream_start must carry the speaker, and it must be a real
    // member row id of THIS group (not empty, not the session's agent id).
    expect(recovery.captured, 'a mid-turn subscribe must be answered with a stream_start').toBe(true)
    expect(recovery.messageId, 'a live run must be re-emitted to a mid-turn subscriber').toBeTruthy()
    expect(recovery.agentId, 'the re-emitted stream_start must carry the speaker (agent_id)').toBeTruthy()
    expect(recovery.memberRowIds).toContain(recovery.agentId)

    // Wait for the group turn to finish so the shared mock is not left busy for
    // later specs (the stop button lives on the group session's run state).
    await expect
      .poll(async () => {
        return page.evaluate(async (groupId) => {
          const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(groupId)}&limit=5`)).json()
          return (hist.messages || []).some((m: { streaming?: boolean }) => m.streaming)
        }, setup.groupId)
      }, { timeout: 90000 })
      .toBe(false)
  })
})

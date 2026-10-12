import { test, expect } from '../fixtures'
import { setGroupMaxSpeeches, restoreGroupMaxSpeeches } from '../helpers/group-config'

/**
 * E2E for "interrupt and send" (插话) on a queued GROUP message.
 *
 * Regression target: the interrupt endpoint looked the running turn up under the
 * GROUP session id, but group turns only ever registered their cancel under the
 * MEMBER row id (a member row has no runner, so runTurn's registration was a
 * silent no-op). The endpoint therefore always answered
 * `{interrupted:false, reason:"idle"}` and the button did nothing.
 *
 * This spec drives the real path: a group turn starts (host speaks slowly), the
 * user queues a second message, clicks the queued bubble's action, and the
 * queued message must then run as its own turn — interrupt KEEPS the queue,
 * unlike a cancel which drops it.
 *
 * The mock's reply delay (ACP_MOCK_REPLY_DELAY_MS, 1200ms) gives a deterministic
 * window to queue and interrupt while the host turn is still streaming.
 */
test.describe.serial('group chat interrupt', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  // The cap is a global server setting; restore it so later specs are unaffected.
  test.afterAll(async () => {
    await restoreGroupMaxSpeeches()
  })

  test('interrupting a running group turn delivers the queued message next', async ({ page }) => {
    // Cap at 1 member speech so a turn terminates on its own if the interrupt
    // does not land (otherwise a failure would hang the spec until timeout).
    await setGroupMaxSpeeches(1)

    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }
      const groupId = created.groupId as string

      // A distinct member so the host routes to a real member turn (the mock
      // names the first addressable member).
      const added = await post('/api/group/members', { groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      // Start the group turn. The host speaks slowly, leaving the turn running.
      const first = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '开始讨论 INTERRUPT_FIRST', queueId: 'gi-first', clientId: 'probe' }),
      })
      if (first.status !== 200) return { ok: false, step: 'first', status: first.status }

      // Queue a second message while the first turn is still running. Sending
      // through /api/ai/chat while busy enqueues it (never runs concurrently).
      const second = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'INTERRUPT_SECOND', queueId: 'gi-second', clientId: 'probe' }),
      })
      if (second.status !== 200) return { ok: false, step: 'second', status: second.status }

      // The queued message must be present and addressable.
      const queued = await (await fetch(`/api/ai/queue?session_id=${encodeURIComponent(groupId)}`)).json()
      const entry = (queued.queue || []).find((q: { queueId: string }) => q.queueId === 'gi-second')
      if (!entry) return { ok: false, step: 'queue', queued }

      // Interrupt: stop the running group turn; the queue survives so the
      // queued message runs as its own turn next.
      const interrupted = await post(
        `/api/ai/queue/interrupt?session_id=${encodeURIComponent(groupId)}&queueId=gi-second`,
        {},
      )

      // Wait for the queued message to be delivered (materialized into the
      // timeline as a user row). NOTE: delivery ALONE does not prove the
      // interrupt kept the queue — a naturally-finished first turn would also
      // drain it. So capture the queue's fate at the moment the message leaves
      // it: an interrupt must NOT have cancelled the queue, whereas a cancel
      // would have cleared the remaining entries.
      const deadline = Date.now() + 60000
      let delivered = false
      let msgs: Array<{ role: string; content?: string; streaming?: boolean }> = []
      while (Date.now() < deadline) {
        const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(groupId)}&limit=50`)).json()
        msgs = hist.messages || []
        delivered = msgs.some(m => m.role === 'user' && (m.content || '').includes('INTERRUPT_SECOND'))
        const stillStreaming = msgs.some(m => m.streaming)
        if (delivered && !stillStreaming) break
        await new Promise(r => setTimeout(r, 1000))
      }

      // The first (interrupted) turn must NOT have been stamped "cancelled":
      // interrupt means the user redirected the reply, not that they abandoned
      // it. A cancel would also have cleared the queue instead of running
      // INTERRUPT_SECOND.
      const firstTurnCancelled = msgs.some(
        m => m.role === 'assistant' && (m as { cancelled?: boolean }).cancelled === true,
      )

      return { ok: true, groupId, interrupted, delivered, firstTurnCancelled }
    })

    expect(result.ok).toBe(true)
    // The endpoint must have found and stopped the group turn (HEAD returned
    // {interrupted:false, reason:"idle"} here — the bug).
    expect(result.interrupted?.interrupted).toBe(true)
    // The queued message must have run next, which is what "keep the queue"
    // means; a cancel would have dropped it.
    expect(result.delivered).toBe(true)
    // ...and the interrupted turn must NOT carry the cancelled badge.
    expect(result.firstTurnCancelled).toBe(false)
  })
})

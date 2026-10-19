import { test, expect } from '../fixtures'

/**
 * E2E for the /cb-agent prompt injection reaching the agent.
 *
 * The unit tests pin the template's CONTENT and the API tests pin the endpoints,
 * but neither proves the two are actually joined: a message starting with
 * `/cb-agent` must reach the agent with the instruction block prepended, or the
 * AI never learns the endpoints exist and the command silently degrades into a
 * plain chat message.
 *
 * The observable evidence is the agent's own echo: the E2E mock records the
 * prompt it received in a thinking block ("Processing user request: <first 80
 * chars>"), so the beginning of the injected template is visible in the
 * persisted message.
 *
 * Read it through /api/rag/message, NOT through /api/ai/chat: the history
 * endpoint strips blocks once a reading summary exists (that is its documented
 * contract), which would make this assertion silently vacuous. The rag message
 * endpoint returns the stored content verbatim.
 */
test.describe('/cb-agent injection', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(120000)

  test('prepends the agent-management instructions to the agent prompt', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const json = async (url: string, init?: RequestInit) => {
        const resp = await fetch(url, init)
        return { ok: resp.ok, status: resp.status, body: await resp.json().catch(() => ({})) }
      }
      const send = (method: string, url: string, body?: unknown) =>
        json(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: body === undefined ? undefined : JSON.stringify(body),
        })

      const session = await send('POST', '/api/ai/sessions', { agentId: 'acp-mock' })
      const sessionId = session.body.sessionId || session.body.id
      if (!sessionId) return { step: 'session', ok: false, session }

      // The shape the frontend sends when the user picks "/cb-agent" from the
      // menu: the bare command followed by the natural-language request.
      const started = await send('POST', '/api/ai/chat', {
        sessionId,
        message: '/cb-agent 添加一个 trae 智能体并测试',
        agentId: 'acp-mock',
      })
      if (!started.ok) {
        await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)
        return { step: 'chat', ok: false, started }
      }

      const deadline = Date.now() + 60000
      let assistantId = 0
      while (Date.now() < deadline) {
        const history = await json(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`)
        const messages = (history.body.messages || []) as Array<{
          id: number
          role: string
          streaming?: boolean
        }>
        const assistant = messages.filter(m => m.role === 'assistant' && !m.streaming).pop()
        if (assistant) {
          assistantId = assistant.id
          break
        }
        await new Promise(r => setTimeout(r, 500))
      }

      let thinking = ''
      let blockTypes = ''
      if (assistantId) {
        // Raw stored content — immune to the history endpoint's summary strip.
        const raw = await json(`/api/rag/message?id=${assistantId}`)
        try {
          const doc = JSON.parse((raw.body as { content?: string }).content || '{}') as {
            blocks?: Array<{ type?: string; text?: string; think_id?: string }>
          }
          const blocks = doc.blocks || []
          blockTypes = blocks.map(b => `${b.type}:${(b.text || '').length}`).join(',')
          // Thinking text lives in chat_thinking, not in the content block: the
          // content carries a slim `{type, think_id}` marker. Reading the block's
          // `text` would always be empty and the assertion vacuous.
          const thinkID = blocks.find(b => b.type === 'thinking' && b.think_id)?.think_id
          if (thinkID) {
            const detail = await json(
              `/api/ai/chat/thinking?think_id=${encodeURIComponent(thinkID)}&message_id=${assistantId}`,
            )
            thinking = (detail.body as { text?: string }).text || ''
          }
        } catch {
          blockTypes = 'unparseable'
        }
      }

      await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)
      return { step: 'done', ok: true, assistantId, thinking, blockTypes }
    })

    expect(result.ok, JSON.stringify(result)).toBe(true)
    const r = result as { assistantId: number; thinking: string; blockTypes: string }

    expect(r.assistantId, `no assistant message (blocks: ${r.blockTypes})`).toBeGreaterThan(0)

    // What the agent received: the injected block's verify section, then the
    // user's own sentence. Seeing both proves the template was prepended to
    // THIS message rather than the raw text being forwarded (which is what
    // happens when /cb-agent is not recognized as a built-in command).
    expect(r.thinking, `blocks: ${r.blockTypes}`).toContain('Verifying an agent')
    expect(r.thinking).toContain('self-test-deep')
    expect(r.thinking).toContain('/cb-agent 添加一个 trae 智能体并测试')
  })
})

import { test, expect } from '../fixtures'

/**
 * E2E for user-defined ACP agents (issue #557): create an agent from a spawn
 * command alone, with no built-in backend behind it, and run a real turn
 * through it.
 *
 * The spawn command is borrowed from the E2E acp-mock agent's own YAML
 * definition (read back through /api/agents), so the test does not need to know
 * the temp directory layout — and it proves the created agent runs the exact
 * same ACP server the built-in mock does.
 */
test.describe('custom ACP agent', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(180000)

  test('creates an agent from a command and runs a turn through it', async ({ page }) => {
    // Agent names are globally unique and the E2E database is shared across
    // runs, so the name carries a per-run suffix; the agent is removed at the
    // end so repeated runs do not accumulate rows.
    const name = `Custom ACP E2E ${Date.now()}`

    const result = await page.evaluate(async (agentName) => {
      const json = async (url: string, init?: RequestInit) => {
        const resp = await fetch(url, init)
        const body = await resp.json().catch(() => ({}))
        return { ok: resp.ok, status: resp.status, body }
      }
      const send = (method: string, url: string, body?: unknown) =>
        json(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: body === undefined ? undefined : JSON.stringify(body),
        })

      // 1. Learn the mock ACP server's absolute path from the seeded agent.
      const list = await json('/api/agents')
      const agents: Array<{ id: string; acpCommand?: string }> = list.body.agents || []
      const mock = agents.find(a => a.id === 'acp-mock')
      if (!mock?.acpCommand) return { step: 'read-mock', ok: false }
      const command = mock.acpCommand

      // 2. Create a custom agent from that command alone.
      const created = await send('POST', '/api/agents', { name: agentName, acp_command: command })
      if (!created.ok) return { step: 'create', ok: false, created }
      const agent = created.body as {
        id: string; backend: string; transport: string; acpCommand: string; supportsCLI: boolean
      }

      // 3. It must survive a reload (persisted, not just in the live map).
      const after = await json('/api/agents')
      const reloaded = (after.body.agents || []).find((a: { id: string }) => a.id === agent.id)

      // 4. A real turn through the ACP transport.
      //
      // POST /api/ai/chat only ENQUEUES the turn (it answers {started:true} and
      // runs the model in a goroutine), so the response body proves nothing
      // about whether the agent spawned. Poll the persisted history instead:
      // an assistant row appearing is the observable outcome, and a spawn
      // failure shows up as an assistant row whose content is an error.
      const session = await send('POST', '/api/ai/sessions', { agentId: agent.id })
      const sessionId = session.body.sessionId || session.body.id
      if (!sessionId) {
        // Nothing was created to clean up except the agent itself.
        const orphan = await send('DELETE', '/api/agents', { id: agent.id })
        return { step: 'session', ok: false, session, agent, deletedStatus: orphan.status }
      }

      const started = await send('POST', '/api/ai/chat', {
        sessionId,
        message: 'hello from a custom acp agent',
        agentId: agent.id,
      })
      if (!started.ok) {
        await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)
        const orphan = await send('DELETE', '/api/agents', { id: agent.id })
        return { step: 'chat', ok: false, started, agent, sessionId, deletedStatus: orphan.status }
      }

      const deadline = Date.now() + 90000
      let assistant: { role: string; content: string; streaming?: boolean } | undefined
      while (Date.now() < deadline) {
        const history = await json(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`)
        const messages: Array<{ role: string; content: string; streaming?: boolean }> =
          history.body.messages || []
        assistant = messages.filter(m => m.role === 'assistant' && !m.streaming).pop()
        if (assistant) break
        await new Promise(r => setTimeout(r, 500))
      }

      // Clean up: destroy the session (DELETE, and it needs the session id),
      // then the agent — an unused agent can be deleted, a leftover one cannot.
      // Runs even when the turn produced nothing, so a failing run does not
      // accumulate rows in the shared E2E database.
      await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)
      const deleted = await send('DELETE', '/api/agents', { id: agent.id })

      // Parse the stored content once, server-side of the assertion: the row is
      // a JSON document, and both the block list and the turn metadata live in
      // it. Note the summary pass may have replaced the blocks with [] (the
      // heavy content is stripped once a reading summary exists), so the
      // metadata is the part that always survives.
      let parsed: { blocks?: Array<{ type?: string }>; metadata?: { stopReason?: string; transport?: string } } = {}
      try {
        parsed = JSON.parse(assistant?.content ?? '{}')
      } catch {
        // Leave parsed empty — the assertions below will report the raw value.
      }

      return {
        step: 'done',
        ok: true,
        agent,
        reloaded,
        assistantContent: assistant?.content ?? null,
        blockTypes: (parsed.blocks ?? []).map(b => b.type ?? ''),
        stopReason: parsed.metadata?.stopReason ?? '',
        turnTransport: parsed.metadata?.transport ?? '',
        deletedStatus: deleted.status,
      }
    }, name)

    expect(result.ok, JSON.stringify(result)).toBe(true)

    const r = result as {
      agent: { id: string; backend: string; transport: string; acpCommand: string; supportsCLI: boolean }
      reloaded?: { backend: string; transport: string; acpCommand: string }
      assistantContent: string | null
      blockTypes: string[]
      stopReason: string
      turnTransport: string
      deletedStatus: number
    }

    expect(r.agent.backend).toBe('custom-acp')
    expect(r.agent.transport).toBe('acp-stdio')
    expect(r.agent.acpCommand).toBeTruthy()
    // A custom backend has no CLI factory, so no CLI transport may be offered.
    expect(r.agent.supportsCLI).toBe(false)

    expect(r.reloaded).toBeTruthy()
    expect(r.reloaded!.transport).toBe('acp-stdio')
    expect(r.reloaded!.acpCommand).toBe(r.agent.acpCommand)

    // The turn actually completed an ACP round trip. Asserting only "some
    // assistant row exists" would be vacuous: a failed turn is ALSO persisted as
    // an assistant row, but carrying warning/error blocks instead of a normal
    // stop. The metadata is what distinguishes the two, and it survives the
    // summary pass (which may strip the blocks themselves).
    expect(r.assistantContent, 'the custom agent must produce an assistant reply').toBeTruthy()
    expect(r.turnTransport, 'the turn must have run over ACP').toBe('acp-stdio')
    expect(r.stopReason, 'the ACP turn must reach a normal end_turn').toBe('end_turn')
    // A spawn/handshake failure lands as a warning or error block.
    expect(r.blockTypes).not.toContain('warning')
    expect(r.blockTypes).not.toContain('error')

    // Cleanup worked: the agent is gone, so repeated runs do not accumulate.
    expect(r.deletedStatus).toBe(200)
  })

  test('rejects an empty command and a duplicate name', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (body: unknown) => {
        const resp = await fetch('/api/agents', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        })
        return { status: resp.status, body: await resp.json().catch(() => ({})) }
      }

      const empty = await post({ name: 'No Command E2E', acp_command: '   ' })
      const dup = await post({ name: 'ACP Mock Agent', acp_command: 'whatever acp' })
      return { empty, dup }
    })

    expect(result.empty.status).toBe(400)
    expect((result.empty.body as { msgKey?: string }).msgKey).toBe('InvalidAcpCommand')
    expect(result.dup.status).toBe(409)
    expect((result.dup.body as { msgKey?: string }).msgKey).toBe('AgentNameTaken')
  })
})

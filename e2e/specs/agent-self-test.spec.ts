import { test, expect } from '../fixtures'

/**
 * E2E for the agent integration self-test (the "run integration test" button on
 * the agent settings page).
 *
 * The endpoint must answer "would this agent work?" from persisted state without
 * sending a prompt, so these assertions are about the SHAPE and TRUTHFULNESS of
 * the report — a green result for a broken agent is the failure mode that
 * matters, and so is a red result for a healthy one.
 */
test.describe('agent self-test', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(60000)

  test('reports a healthy ACP agent as passing', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/agents/acp-mock/self-test', { method: 'POST' })
      return { status: resp.status, body: await resp.json().catch(() => ({})) }
    })

    expect(result.status).toBe(200)
    const body = result.body as {
      agentId: string
      ok: boolean
      passed: number
      failed: number
      warned: number
      checks: Array<{ key: string; status: string; detail?: string }>
    }

    expect(body.agentId).toBe('acp-mock')
    // The E2E acp-mock agent is fully configured (absolute command, models in
    // its YAML), so nothing may fail.
    expect(body.failed, JSON.stringify(body.checks)).toBe(0)
    expect(body.ok).toBe(true)

    // The counters are what the UI renders next to the rows; if they disagree
    // with the list, the summary lies.
    expect(body.passed + body.failed + body.warned).toBe(body.checks.length)

    // Spot-check the checks that prove the report is real rather than a
    // constant: the transport must be ACP, and the executable probe must have
    // actually looked at the configured command.
    const transport = body.checks.find(c => c.key === 'transport')
    expect(transport?.status).toBe('ok')
    expect(transport?.detail).toContain('acp-stdio')

    const cmd = body.checks.find(c => c.key === 'acp_command')
    expect(cmd?.status).toBe('ok')
    expect(cmd?.detail).toContain('is available')

    const models = body.checks.find(c => c.key === 'models')
    expect(models?.status).toBe('ok')
  })

  test('reports a custom agent with a bogus command as failing', async ({ page }) => {
    // A real user's most likely breakage: a command that is not installed. The
    // self-test must name it instead of failing vaguely.
    const result = await page.evaluate(async () => {
      const create = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: `Self Test Broken ${Date.now()}`,
          acp_command: 'not-installed-anywhere-acp --stdio',
        }),
      })
      const agent = await create.json()
      if (!create.ok) return { step: 'create', status: create.status, agent }

      const resp = await fetch(`/api/agents/${encodeURIComponent(agent.id)}/self-test`, { method: 'POST' })
      const body = await resp.json()

      // Clean up so repeated runs do not accumulate rows.
      const del = await fetch('/api/agents', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: agent.id }),
      })

      return { step: 'done', status: resp.status, body, deletedStatus: del.status }
    })

    expect(result.step, JSON.stringify(result)).toBe('done')
    expect(result.status).toBe(200)

    const body = result.body as {
      ok: boolean
      failed: number
      checks: Array<{ key: string; status: string; detail?: string }>
    }
    expect(body.ok).toBe(false)
    expect(body.failed).toBeGreaterThan(0)

    const cmd = body.checks.find(c => c.key === 'acp_command')
    expect(cmd?.status).toBe('fail')
    expect(cmd?.detail).toContain('not-installed-anywhere-acp')

    expect((result as { deletedStatus: number }).deletedStatus).toBe(200)
  })

  test('returns 404 for an unknown agent and 405 for a wrong method', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const missing = await fetch('/api/agents/ghost-agent/self-test', { method: 'POST' })
      const wrongMethod = await fetch('/api/agents/acp-mock/self-test')
      return { missing: missing.status, wrongMethod: wrongMethod.status }
    })

    expect(result.missing).toBe(404)
    expect(result.wrongMethod).toBe(405)
  })

  test('the settings page button renders the check rows', async ({ page }) => {
    // UI-level: proves the button is wired to the endpoint and that the rows
    // actually reach the DOM (a passing API with an unrendered panel would
    // still be useless to the user).
    await page.goto('/')
    await page.waitForLoadState('domcontentloaded')

    // Navigate: settings → agents → the acp-mock agent's detail page.
    await page.locator('.dock-btn[data-tab="settings"]').first()
      .click({ timeout: 15000 }).catch(async () => {
        await page.locator('.dock-overflow-btn').click()
        await page.locator('.dock-overflow-item').filter({ hasText: /settings|设置/i }).click()
      })
    await expect(page.locator('.settings-page')).toBeVisible({ timeout: 15000 })

    await page.locator('.settings-index__row').filter({ hasText: /agent|智能体/i }).first().click()
    await expect(page.locator('.settings-agents-index__row').first()).toBeVisible({ timeout: 10000 })

    await page.locator('.settings-agents-index__row').filter({ hasText: 'ACP Mock Agent' }).first().click()

    // Two buttons now: [0] = configuration checks, [1] = real turn. This test
    // exercises the cheap one.
    const btn = page.locator('.settings-agent-detail__selftest-btn').first()
    await expect(btn).toBeVisible({ timeout: 10000 })
    await btn.click()

    // The result panel must appear with at least one row, and no FAIL label for
    // this healthy agent.
    await expect(page.locator('.settings-agent-detail__selftest-check').first())
      .toBeVisible({ timeout: 20000 })
    const failRows = page.locator('.settings-agent-detail__selftest-check--fail')
    await expect(failRows).toHaveCount(0)
  })

  test('the index page tests every agent at once', async ({ page }) => {
    await page.goto('/')
    await page.waitForLoadState('domcontentloaded')

    await page.locator('.dock-btn[data-tab="settings"]').first()
      .click({ timeout: 15000 }).catch(async () => {
        await page.locator('.dock-overflow-btn').click()
        await page.locator('.dock-overflow-item').filter({ hasText: /settings|设置/i }).click()
      })
    await expect(page.locator('.settings-page')).toBeVisible({ timeout: 15000 })
    await page.locator('.settings-index__row').filter({ hasText: /agent|智能体/i }).first().click()
    await expect(page.locator('.settings-agents-index__row').first()).toBeVisible({ timeout: 10000 })

    await page.locator('[data-action="agent-selftest-all"]').click()

    // One row per agent must appear; the count matching the list is what proves
    // every agent was actually tested rather than just the first.
    const agentRows = await page.locator('.settings-agents-index__row').count()
    await expect(page.locator('.settings-agents-index__selftest-row')).toHaveCount(agentRows, { timeout: 30000 })

    // The E2E fleet (acp-mock + acp-mock-b) is healthy, so nothing may fail.
    await expect(page.locator('.settings-agents-index__selftest-row--fail')).toHaveCount(0)
  })

  test('deep mode runs a real turn and cleans up after itself', async ({ page }) => {
    // This is the check that proves the agent can actually ANSWER: the static
    // checks only prove the configuration is self-consistent. It costs tokens,
    // which is exactly why it is a separate, opt-in route.
    const result = await page.evaluate(async () => {
      const before = await (await fetch('/api/ai/sessions')).json()
      const beforeCount = (before.sessions || []).length

      const resp = await fetch('/api/agents/acp-mock/self-test-deep', { method: 'POST' })
      const body = await resp.json().catch(() => ({}))

      const after = await (await fetch('/api/ai/sessions')).json()
      const afterCount = (after.sessions || []).length

      return { status: resp.status, body, beforeCount, afterCount }
    })

    expect(result.status).toBe(200)
    const body = result.body as {
      ok: boolean
      error?: string
      deep?: { reply?: string; transport?: string; stopReason?: string; durationMs?: number }
      static?: { ok: boolean; failed: number }
    }

    expect(body.ok, JSON.stringify(body)).toBe(true)
    // The reply came from the mock agent, which answers with a fixed sentence.
    expect(body.deep?.reply).toBeTruthy()
    expect(body.deep?.reply).toContain('mock ACP agent')
    // Ground truth from the persisted turn metadata, not from config.
    expect(body.deep?.transport).toBe('acp-stdio')
    expect(body.deep?.stopReason).toBe('end_turn')

    // The throwaway session must not survive: a self-test that leaves
    // conversations behind would pollute the user's session list on every click.
    expect(result.afterCount).toBe(result.beforeCount)
  })

  test('deep mode auto-approves permissions so an unattended turn cannot hang', async ({ page }) => {
    // The mock requests approval before writing in every mode except
    // bypass-permissions. Nobody is watching a self-test, so without
    // auto-approve the turn parks until the 90s timeout and then reports a
    // cancellation — a false failure for a healthy agent.
    //
    // Getting the agent into a permission-requesting mode takes two steps,
    // because preferred_mode is validated against what the agent has actually
    // advertised: run one ordinary turn first (which populates the capability
    // registry), then pin the mode to "code". A fresh custom agent is used so
    // the shared acp-mock's state cannot make this pass for the wrong reason.
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

      const list = await json('/api/agents')
      const mock = ((list.body.agents || []) as Array<{ id: string; acpCommand?: string }>)
        .find(a => a.id === 'acp-mock')
      if (!mock?.acpCommand) return { step: 'read-mock', ok: false }

      const created = await send('POST', '/api/agents', {
        name: `Perm Self Test ${Date.now()}`,
        acp_command: mock.acpCommand,
      })
      if (!created.ok) return { step: 'create', ok: false, created }
      const agentId = (created.body as { id: string }).id

      try {
        // Warm the capability registry with one ordinary turn.
        const session = await send('POST', '/api/ai/sessions', { agentId })
        const sessionId = session.body.sessionId || session.body.id
        await send('POST', '/api/ai/chat', { sessionId, message: 'warm up', agentId })
        const deadline = Date.now() + 60000
        while (Date.now() < deadline) {
          const history = await json(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`)
          const msgs = (history.body.messages || []) as Array<{ role: string; streaming?: boolean }>
          if (msgs.some(m => m.role === 'assistant' && !m.streaming)) break
          await new Promise(r => setTimeout(r, 300))
        }
        await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)

        const pinned = await send('PATCH', '/api/agents', { id: agentId, preferred_mode: 'code' })
        if (!pinned.ok) return { step: 'pin-mode', ok: false, pinned }

        const resp = await send('POST', `/api/agents/${encodeURIComponent(agentId)}/self-test-deep`)
        return { step: 'done', ok: true, status: resp.status, body: resp.body }
      } finally {
        await send('DELETE', '/api/agents', { id: agentId })
      }
    })

    expect(result.ok, JSON.stringify(result)).toBe(true)
    const r = result as {
      status: number
      body: { ok: boolean; error?: string; deep?: { stopReason?: string } }
    }
    // Without auto-approve this reports a cancellation (or times out).
    expect(r.body.ok, JSON.stringify(r.body)).toBe(true)
    expect(r.body.deep?.stopReason).toBe('end_turn')
  })

  test('deep mode refuses to run when the static checks fail', async ({ page }) => {
    // Running a 90s turn against an agent whose command does not exist would
    // burn the whole timeout to learn what LookPath already knows.
    const result = await page.evaluate(async () => {
      const create = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: `Deep Broken ${Date.now()}`,
          acp_command: 'not-installed-anywhere-deep --stdio',
        }),
      })
      const agent = await create.json()
      if (!create.ok) return { step: 'create', status: create.status, agent }

      const resp = await fetch(`/api/agents/${encodeURIComponent(agent.id)}/self-test-deep`, { method: 'POST' })
      const body = await resp.json()

      await fetch('/api/agents', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: agent.id }),
      })
      return { step: 'done', status: resp.status, body }
    })

    expect(result.step, JSON.stringify(result)).toBe('done')
    const body = result.body as { ok: boolean; error?: string; static?: { failed: number } }
    expect(body.ok).toBe(false)
    expect(body.static?.failed).toBeGreaterThan(0)
    expect(body.error).toContain('configuration checks failed')
  })
})

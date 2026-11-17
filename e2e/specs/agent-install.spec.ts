import { test, expect } from '../fixtures'

/**
 * E2E for the one-click agent install endpoint (`POST /api/backends/{id}/install`)
 * and for the first-session auto probe.
 *
 * Both features are backend behaviour, so this spec drives them over HTTP rather
 * than through the dialog: the dialog's own wiring is covered by
 * web/src/components/AgentInstallDialog.test.ts, and a real install would run a
 * package manager against the network (slow, flaky, and it would leave a
 * globally installed package behind).
 *
 * What only an end-to-end run can prove:
 *   - the endpoint is routed, authenticated and keyed by backend id;
 *   - the command it runs comes from the registry, never from the request;
 *   - an unknown backend / a non-install path answer 404 rather than 500;
 *   - creating a session for a capability-less ACP agent does not fail and the
 *     pickers end up populated (via the agent list, after the auto probe).
 */
test.describe('agent install endpoint', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(60000)

  test('rejects an unknown backend with 404', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/backends/no-such-backend/install', { method: 'POST' })
      return { status: resp.status, body: await resp.json().catch(() => ({})) }
    })

    expect(result.status).toBe(404)
    expect((result.body as { msgKey?: string }).msgKey).toBe('BackendNotFound')
  })

  test('rejects a non-install sub-path with 404', async ({ page }) => {
    // The subtree route must not answer 500 for a path it does not serve.
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/backends/claude/something-else', { method: 'POST' })
      return { status: resp.status }
    })
    expect(result.status).toBe(404)
  })

  test('refuses GET', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/backends/claude/install')
      return { status: resp.status }
    })
    expect(result.status).toBe(405)
  })

  test('ignores a command in the request body', async ({ page }) => {
    // The security property: the command is the server's own registry value, so
    // a body carrying one must change nothing. Sending it to an UNKNOWN backend
    // proves the id decides — the request answers "no such backend" instead of
    // running what the caller sent. (The precise "ignored body, registry
    // command ran" case is pinned in the Go unit test, where the runner is
    // stubbed; doing it here would run a real package manager.)
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/backends/no-such-backend/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'touch /tmp/clawbench-e2e-should-not-exist' }),
      })
      return { status: resp.status, body: await resp.json().catch(() => ({})) }
    })

    expect(result.status).toBe(404)
    expect((result.body as { msgKey?: string }).msgKey).toBe('BackendNotFound')
  })
})

test.describe('backend list carries install commands', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')

  test('every installable backend reports the same command on both surfaces', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const [backendsResp, agentsResp] = await Promise.all([
        fetch('/api/backends'),
        fetch('/api/agents'),
      ])
      return {
        backends: (await backendsResp.json()).backends || [],
        agents: (await agentsResp.json()).agents || [],
      }
    })

    const byId = new Map<string, string>(
      (result.backends as Array<{ id: string; install_cmd?: string }>)
        .filter(b => b.install_cmd)
        .map(b => [b.id, b.install_cmd as string]),
    )
    // True on every install: most backends ship a built-in installer.
    expect(byId.size, 'at least some backends must ship an installer').toBeGreaterThan(0)

    // The invariant that must hold for EVERY agent, whether or not this machine
    // happens to have its CLI installed: the command the settings page offers is
    // either absent or byte-identical to the welcome panel's. Two surfaces
    // showing different commands is the drift this guards, and asserting only
    // "some agent matched" would go vacuous on a machine with no CLIs present.
    for (const a of result.agents as Array<{ backend: string; installCmd?: string }>) {
      const expected = byId.get(a.backend) ?? ''
      expect(a.installCmd ?? '', `installCmd drift for backend ${a.backend}`).toBe(expected)
    }
  })
})

test.describe('first-session auto probe', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(180000)

  test('a new session for a capability-less ACP agent populates its model list', async ({ page }) => {
    // A custom ACP agent starts with no capability snapshot at all — exactly the
    // state that used to leave the pickers empty until the first message.
    const name = `Auto Probe E2E ${Date.now()}`

    const result = await page.evaluate(async (agentName) => {
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

      // Borrow the E2E mock ACP server's command so the probe has something real
      // to spawn.
      const list = await json('/api/agents')
      const agents: Array<{ id: string; acpCommand?: string }> = list.body.agents || []
      const mock = agents.find(a => a.id === 'acp-mock')
      if (!mock?.acpCommand) return { step: 'read-mock' as const }

      const created = await send('POST', '/api/agents', { name: agentName, acp_command: mock.acpCommand })
      if (!created.ok) return { step: 'create' as const, created }
      const agentId = (created.body as { id: string }).id

      // A brand-new agent has no capability snapshot: the API must gate its
      // model list (empty `models`, raw CLI list preserved under `cliModels`).
      const before = await json('/api/agents')
      const beforeAgent = (before.body.agents || []).find((a: { id: string }) => a.id === agentId)

      const session = await send('POST', '/api/ai/sessions', { agentId })
      const sessionId = session.body.sessionId || session.body.id

      // The probe runs in the background. Poll the agent list until the agent
      // reports models, or give up — the assertion below reports which.
      let afterAgent: { models?: unknown[] } | undefined
      const deadline = Date.now() + 60000
      while (Date.now() < deadline) {
        const now = await json('/api/agents')
        afterAgent = (now.body.agents || []).find((a: { id: string }) => a.id === agentId)
        if ((afterAgent?.models?.length ?? 0) > 0) break
        await new Promise(r => setTimeout(r, 1000))
      }

      // Clean up: the session then the agent (a session would block deletion).
      if (sessionId) {
        await send('DELETE', `/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`)
      }
      const deleted = await send('DELETE', '/api/agents', { id: agentId })

      return {
        step: 'done' as const,
        beforeModels: beforeAgent?.models?.length ?? -1,
        afterModels: afterAgent?.models?.length ?? -1,
        deletedStatus: deleted.status,
      }
    }, name)

    expect(result.step, JSON.stringify(result)).toBe('done')
    if (result.step !== 'done') return

    // The agent existed and had nothing to show before the session...
    expect(result.beforeModels).toBe(0)
    // ...and the auto probe filled it in without anyone pressing refresh.
    expect(result.afterModels, 'the first session must trigger the capability probe').toBeGreaterThan(0)
    expect(result.deletedStatus).toBe(200)
  })
})

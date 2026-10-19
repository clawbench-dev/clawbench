import { test, expect } from '../fixtures'

/**
 * E2E for /cb-mcp's API surface.
 *
 * The slash command itself only injects a prompt; what makes it "manage MCP" is
 * that the endpoints it tells the AI to call actually do the job. This spec
 * drives exactly that sequence — read, whole-replacement write, read back —
 * because the whole-replacement semantics are the part that loses data when
 * misunderstood: a partial array silently deletes every server it omits.
 *
 * It also pins the per-agent policy, the second resource the command writes.
 * Getting the two confused (definition on the agent, policy in config) is the
 * failure mode the prompt spends most of its words preventing.
 */
test.describe('cb-mcp API surface', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'backend-level spec')
  test.setTimeout(90000)

  test('read → replace → read back manages the global list', async ({ page }) => {
    await page.goto('/')
    await page.waitForTimeout(1000)

    const before = await page.evaluate(async () => {
      const r = await fetch('/api/config')
      const j = await r.json()
      return j.mcp?.servers ?? []
    })
    // Snapshot the original so a failure cannot leave the shared e2e server
    // with this spec's servers.
    const original = JSON.stringify(before)

    try {
      const write = await page.evaluate(async () => {
        const r = await fetch('/api/config', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            mcp: {
              servers: [
                { name: 'e2e-stdio', transport: 'stdio', command: 'node', args: ['server.js'] },
                { name: 'e2e-http', transport: 'http', url: 'https://example.com/mcp', headers: { Authorization: 'Bearer t' } },
              ],
            },
          }),
        })
        return { status: r.status, body: await r.json() }
      })
      expect(write.status).toBe(200)
      // The command's prompt tells the AI to say "takes effect from the next
      // message", not "restart required". If this ever becomes true the prompt
      // is lying and must be updated with it.
      expect(write.body.needs_restart).toBe(false)

      const after = await page.evaluate(async () => {
        const r = await fetch('/api/config')
        const j = await r.json()
        return j.mcp?.servers ?? []
      })
      expect(after).toHaveLength(2)
      expect(after.map((s: any) => s.name).sort()).toEqual(['e2e-http', 'e2e-stdio'])
      // Secrets round-trip in plaintext, which is what the prompt warns about.
      expect(after.find((s: any) => s.name === 'e2e-http').headers.Authorization).toBe('Bearer t')

      // Whole-replacement: sending one server deletes the other. This is the
      // behaviour that makes "always GET first" a hard rule.
      await page.evaluate(async () => {
        await fetch('/api/config', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mcp: { servers: [{ name: 'e2e-stdio', transport: 'stdio', command: 'node' }] } }),
        })
      })
      const narrowed = await page.evaluate(async () => {
        const r = await fetch('/api/config')
        const j = await r.json()
        return j.mcp?.servers ?? []
      })
      expect(narrowed).toHaveLength(1)
      expect(narrowed[0].name).toBe('e2e-stdio')
    } finally {
      // Always restore, even on assertion failure.
      await page.evaluate(async (orig) => {
        await fetch('/api/config', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mcp: { servers: JSON.parse(orig) } }),
        })
      }, original)
    }
  })

  test('the per-agent policy is readable and writable', async ({ page }) => {
    await page.goto('/')
    await page.waitForTimeout(1000)

    const agents = await page.evaluate(async () => {
      const r = await fetch('/api/agents')
      const j = await r.json()
      return j.agents ?? []
    })
    expect(agents.length).toBeGreaterThan(0)

    const target = agents.find((a: any) => a.transport !== 'cli') ?? agents[0]
    const original = JSON.stringify(target.mcpServers ?? null)

    try {
      const patch = await page.evaluate(async (id) => {
        const r = await fetch('/api/agents', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id, mcp_servers: { mode: 'none', names: [] } }),
        })
        return r.status
      }, target.id)
      expect(patch).toBe(200)

      const after = await page.evaluate(async (id) => {
        const r = await fetch('/api/agents')
        const j = await r.json()
        return (j.agents ?? []).find((a: any) => a.id === id)
      }, target.id)
      expect(after.mcpServers.mode).toBe('none')
    } finally {
      await page.evaluate(async ({ id, orig }) => {
        await fetch('/api/agents', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id, mcp_servers: orig === 'null' ? { mode: 'all', names: [] } : JSON.parse(orig) }),
        })
      }, { id: target.id, orig: original })
    }
  })
})

import { test, expect } from '../fixtures'
import { openCreateGroupPicker, selectAgentRow } from '../helpers/group'

/**
 * E2E for adding the SAME agent to a group multiple times (multi-instance) and
 * the auto-assigned unique nicknames. Runs against the real backend so the
 * naming rule (first instance keeps the agent's own name; later instances draw
 * the built-in English name pool — "Alice", "Bob", …) and the roster wiring are
 * exercised end to end.
 */
test.describe.serial('group duplicate agents', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(60000)

  test('create-group API keeps duplicate agents with unique names', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      // Host mode: acp-mock is the host; add a SECOND acp-mock instance (the
      // host agent duplicated) plus THREE acp-mock-b instances.
      const created = await post('/api/group/create', {
        hostAgentId: 'acp-mock',
        memberAgentIds: ['acp-mock', 'acp-mock', 'acp-mock-b', 'acp-mock-b', 'acp-mock-b'],
      })
      if (!created.ok) return { ok: false, step: 'create', created }

      const list = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()
      const members = (list.members || []).map((m: { agentId: string; name: string }) => ({ agentId: m.agentId, name: m.name }))
      return { ok: true, groupId: created.groupId, hostMemberId: created.hostMemberId, members }
    })

    expect(result.ok).toBe(true)
    const members = (result as { members: Array<{ agentId: string; name: string }> }).members
    // 2 acp-mock + 3 acp-mock-b = 5 members (the host is one of the acp-mock rows).
    expect(members.length).toBe(5)

    const names = members.map(m => m.name)
    // Names must be unique across the whole roster.
    expect(new Set(names).size).toBe(names.length)
    // Naming is assigned in SPEC order (the order of memberAgentIds), drawing
    // pool names from groupNamePool[0] upward. An agent with MULTIPLE instances
    // keeps NO bare name — every instance (the first included) draws a pool
    // name — so ["acp-mock","acp-mock","acp-mock-b","acp-mock-b","acp-mock-b"]
    // gives, in order: Alice(pool0), Bob(pool1) for acp-mock; then
    // Carol(pool2), Dave(pool3), Erin(pool4) for acp-mock-b.
    const bNames = members.filter(m => m.agentId === 'acp-mock-b').map(m => m.name).sort()
    expect(bNames).toEqual(['Carol', 'Dave', 'Erin'])
    // acp-mock instances: both are pool names (no bare "ACP Mock Agent").
    const aNames = members.filter(m => m.agentId === 'acp-mock').map(m => m.name).sort()
    expect(aNames).toEqual(['Alice', 'Bob'])
    // The pre-pool "<name> <ordinal>" form never appears while the pool has room.
    expect(names.every(n => !/\s\d+$/.test(n))).toBe(true)
    // Neither agent kept its display name.
    expect(names).not.toContain('ACP Mock Agent')
    expect(names).not.toContain('ACP Mock Agent B')
  })

  test('add-members API draws the next pool name (no reuse)', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { hostAgentId: 'acp-mock', memberAgentIds: ['acp-mock', 'acp-mock-b'] })
      if (!created.ok) return { ok: false, step: 'create', created }

      // Add one more instance of the same agent through the add path.
      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      const list = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()
      const bNames = (list.members || []).filter((m: { agentId: string }) => m.agentId === 'acp-mock-b').map((m: { name: string }) => m.name).sort()
      return { ok: true, bNames }
    })

    expect(result.ok).toBe(true)
    // The duplicate draws the first unused pool name; the acp-mock-b base name is
    // already taken by the first instance, so the second becomes "Alice".
    expect((result as { bNames: string[] }).bNames).toEqual(['ACP Mock Agent B', 'Alice'])
  })

  test('UI: stepper adds a second instance of the same agent', async ({ page }) => {
    const picker = await openCreateGroupPicker(page)

    // Select the first agent, then bump it to 2 instances via the stepper.
    const firstRow = await selectAgentRow(picker, page)
    const stepper = firstRow.locator('.agent-stepper')
    await expect(stepper).toBeVisible()
    await expect(stepper.locator('.agent-stepper-count')).toHaveText('1')
    await stepper.locator('.agent-stepper-btn').last().click()
    await expect(stepper.locator('.agent-stepper-count')).toHaveText('2')
  })
})

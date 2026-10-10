import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E for adding the SAME agent to a group multiple times (multi-instance) and
 * the auto-assigned unique nicknames. Runs against the real backend so the
 * naming rule (bare name, then " 2", " 3") and the roster wiring are exercised
 * end to end.
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
    // acp-mock-b instances derive from its DISPLAY name "ACP Mock Agent B":
    // "ACP Mock Agent B", "ACP Mock Agent B 2", "ACP Mock Agent B 3".
    const bNames = members.filter(m => m.agentId === 'acp-mock-b').map(m => m.name).sort()
    expect(bNames).toEqual(['ACP Mock Agent B', 'ACP Mock Agent B 2', 'ACP Mock Agent B 3'])
    // acp-mock instances: the first keeps the bare name, the second is " 2".
    const aNames = members.filter(m => m.agentId === 'acp-mock').map(m => m.name).sort()
    expect(aNames).toEqual(['ACP Mock Agent', 'ACP Mock Agent 2'])
    // " 1" is never produced.
    expect(names.some(n => n.endsWith(' 1'))).toBe(false)
  })

  test('add-members API continues the ordinal (no reuse)', async ({ page }) => {
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
    expect((result as { bNames: string[] }).bNames).toEqual(['ACP Mock Agent B', 'ACP Mock Agent B 2'])
  })

  test('UI: stepper adds a second instance of the same agent', async ({ page }) => {
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })

    // Open the create-group picker.
    await sessionDrawer.locator('[data-action="create-group"]').click()

    const picker = page.locator('.bs-panel').filter({ has: page.locator('.agent-list') }).last()
    await expect(picker).toBeVisible({ timeout: 5000 })

    // Select the first agent, then bump it to 2 instances via the stepper.
    const firstRow = picker.locator('.agent-option').first()
    await firstRow.click()
    const stepper = firstRow.locator('.agent-stepper')
    await expect(stepper).toBeVisible()
    await expect(stepper.locator('.agent-stepper-count')).toHaveText('1')
    await stepper.locator('.agent-stepper-btn').last().click()
    await expect(stepper.locator('.agent-stepper-count')).toHaveText('2')
  })
})

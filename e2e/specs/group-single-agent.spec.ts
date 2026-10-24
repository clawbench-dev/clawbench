import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { selectAgentRow, bumpInstances, fetchGroupMembers, waitPastTouchGuard } from '../helpers/group'

/**
 * E2E for the "only one agent installed" install.
 *
 * The group create page used to gate its single/group segmented switch on
 * `selectableAgents.length >= 2`, reading "a group needs two members" as "a
 * group needs two DIFFERENT agents". Repeat instances of the same agent are
 * independent members (the backend counts instances, cap 12), so a
 * single-agent install could not reach group creation at all: the switch never
 * rendered, hence the group tab was unreachable.
 *
 * The gate is now "at least one selectable agent". This spec proves the whole
 * path with a REAL single-agent install: `GET /api/agents` is stubbed down to
 * one agent (the e2e server ships two), while group creation still goes to the
 * real backend, so the payload the picker emits is validated end to end.
 */
test.describe.serial('group from a single installed agent', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(60000)

  test('the single/group switch renders and a two-instance group is created', async ({ page }) => {
    // Narrow the agent list to acp-mock only. Everything else (including
    // POST /api/group/create) passes through untouched, so the group is really
    // created — the stub only changes what the picker can offer.
    await page.route('**/api/agents', async (route) => {
      if (route.request().method() !== 'GET') return route.continue()
      const resp = await route.fetch()
      const body = await resp.json()
      const only = (body.agents || []).filter((a: { id: string }) => a.id === 'acp-mock')
      await route.fulfill({ response: resp, json: { ...body, agents: only, defaultAgent: 'acp-mock' } })
    })

    // The picker loads agents when it opens, so the route must be registered
    // before that fetch — reload so the app's own /api/agents goes through it.
    await page.reload()
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 20000 })

    // Open the create page (single mode first — the switch lives in its header).
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })
    await sessionDrawer.locator('[data-action="create"]').click()

    const picker = page.locator('.bs-panel').filter({ has: page.locator('.agent-list') }).last()
    await expect(picker).toBeVisible({ timeout: 5000 })

    // (1) The switch exists with exactly one agent installed. This is the
    // regression: gating on >= 2 removed it entirely, so the group tab (and
    // therefore group creation) was unreachable.
    const switchEl = picker.locator('.agent-mode-switch')
    await expect(switchEl).toBeVisible({ timeout: 5000 })
    await expect(switchEl.locator('.agent-mode-tab')).toHaveCount(2)

    // (2) Only one row is offered — proves the stub really is a single-agent
    // install and assertion (1) is not passing on the two-agent default.
    await expect(picker.locator('.agent-option')).toHaveCount(1)

    // (3) Flip to group mode and build a two-instance group from that one agent.
    await picker.locator('.agent-mode-tab[data-mode="group"]').click()
    await waitPastTouchGuard(page)
    const row = await selectAgentRow(picker, page)
    await bumpInstances(row, 2)

    // Confirm becomes enabled only at two members (a free group needs two).
    const confirm = picker.locator('.agent-multi-confirm')
    await expect(confirm).toBeEnabled({ timeout: 5000 })
    await confirm.click()

    // The real backend accepted it: the group view opens with a two-member
    // roster whose rows are both the SAME agent.
    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 15000 })
    const groupId = await page.evaluate(() => {
      let best = ''
      let bestAt = -1
      for (const k of Object.keys(localStorage)) {
        if (!k.startsWith('clawbench-recent-session:')) continue
        try {
          const e = JSON.parse(localStorage.getItem(k) || '{}') as { sessionId?: string; accessedAt?: number }
          if (e.sessionId && (e.accessedAt ?? 0) > bestAt) {
            best = e.sessionId
            bestAt = e.accessedAt ?? 0
          }
        } catch { /* ignore malformed entry */ }
      }
      return best
    })
    expect(groupId).toBeTruthy()

    const members = await fetchGroupMembers(page, groupId)
    expect(members).toHaveLength(2)
    expect(members.map(m => m.agentName)).toEqual(['ACP Mock Agent', 'ACP Mock Agent'])
    // Distinct nicknames: the first keeps the agent name, the duplicate draws a
    // pool name — two instances, not one member counted twice.
    expect(new Set(members.map(m => m.name)).size).toBe(2)
  })
})

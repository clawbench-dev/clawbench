import { test, expect } from '../fixtures'

/**
 * E2E for AI group chat (v1, sequential rounds).
 *
 * Scope: the group lifecycle the user drives through the API — create a group
 * with a host, add a member, read the roster (with isHost), change settings,
 * disable + enable a member, and confirm the group appears in the session list
 * while its hidden members do not. Driving the full multi-agent debate needs a
 * mock agent that emits routing tags; that is covered by the Go orchestrator
 * unit tests. This spec pins the HTTP + DB + UI-surfacing contract end to end.
 *
 * The fixtures authenticate the page and set the project cookie, so fetch()
 * from page.evaluate runs as a logged-in client on the active project.
 */
test.describe('AI group chat', () => {
  test('create, add member, list, settings, disable and enable', async ({ page }) => {
    const result = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }

      // The member MUST use a different agent id than the host: a group cannot
      // contain the same agent twice (AddGroupMember dedups by agent id and
      // returns the HOST row), so adding 'acp-mock' here would leave the group
      // single-member and the roster assertion below would fail.
      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      const listed = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()

      const settings = await (await fetch('/api/group/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ groupId: created.groupId, parallelDefault: true }),
      })).json()

      const memberId = added.memberIds?.[0]
      const disabled = await (await fetch('/api/group/members', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ groupId: created.groupId, memberId, disabled: true }),
      })).json()

      const afterDisable = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()

      const enabled = await (await fetch('/api/group/members', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ groupId: created.groupId, memberId, disabled: false }),
      })).json()

      const afterEnable = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(created.groupId)}`)).json()

      // The group must show up in the session list; members must not. The group
      // row also carries a compact member preview (the list renders stacked
      // avatars from it) — speaking members only, so a disabled member drops out.
      const sessions = await (await fetch('/api/ai/sessions')).json()
      const ids = (sessions.sessions || []).map((s: { id: string }) => s.id)
      const groupRow = (sessions.sessions || []).find((s: { id: string }) => s.id === created.groupId)

      return { ok: true, created, added, listed, settings, disabled, afterDisable, enabled, afterEnable, ids, memberId, groupRow }
    })

    expect(result.ok).toBe(true)
    expect(result.created.groupId).toBeTruthy()
    expect(result.created.hostMemberId).toBeTruthy()

    // Roster: host + added member, with exactly one isHost.
    expect(result.listed.members.length).toBe(2)
    const hosts = result.listed.members.filter((m: { isHost: boolean }) => m.isHost)
    expect(hosts.length).toBe(1)
    expect(hosts[0].id).toBe(result.created.hostMemberId)

    expect(result.settings.ok).toBe(true)

    // Muting is NOT removal: the member stays in the roster with disabled=true, and
    // its row (hence its context) is untouched. Unmuting is a plain flip.
    expect(result.disabled.ok).toBe(true)
    const disabledMember = result.afterDisable.members.find((m: { id: string }) => m.id === result.memberId)
    expect(disabledMember.disabled).toBe(true)
    expect(result.enabled.ok).toBe(true)
    const undisabledMember = result.afterEnable.members.find((m: { id: string }) => m.id === result.memberId)
    expect(undisabledMember.disabled).toBe(false)

    // Visibility: the group is listed; hidden members are not.
    expect(result.ids).toContain(result.created.groupId)
    expect(result.ids).not.toContain(result.created.hostMemberId)
    expect(result.ids).not.toContain(result.memberId)

    // The group row carries the member preview. The member was enabled again by
    // the time this snapshot was taken, so both are speaking members.
    expect(result.groupRow.sessionType).toBe('group')
    expect(result.groupRow.groupMembers.length).toBe(2)
  })

  test('sending to a group is delegated to the orchestrator', async ({ page }) => {
    const resp = await page.evaluate(async () => {
      const created = await (await fetch('/api/group/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostAgentId: 'acp-mock' }),
      })).json()
      const r = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'hello group', sessionId: created.groupId }),
      })
      return { status: r.status, body: await r.json() }
    })
    expect(resp.status).toBe(200)
    expect(resp.body.group).toBe(true)
  })
})

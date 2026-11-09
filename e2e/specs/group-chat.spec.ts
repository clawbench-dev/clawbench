import { test, expect } from '../fixtures'
import { apiFetch } from '../helpers/auth'

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
  // Every group this file creates, so afterAll can destroy them.
  //
  // These specs share one server and one database with every other spec in the
  // shard, and nothing else removes what they create. Leftover groups are
  // sessions: they show up in `/api/ai/sessions` and in session search, which
  // is what later specs in the same shard assert against.
  const createdGroups: string[] = []

  test.afterAll(async () => {
    // `apiFetch` (session cookie + one retry on 401), not a fixture: `page` and
    // `context` are per-test and Playwright rejects them inside `afterAll`
    // ("fixtures are not supported in afterAll"), and the `request` fixture
    // carries no cookies so every delete would 401.
    for (const id of createdGroups.splice(0)) {
      await apiFetch(`/api/ai/session/destroy?session_id=${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }).catch(() => {})
    }
  })

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

    if (result.ok && result.created?.groupId) createdGroups.push(result.created.groupId)

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

      // Bail out with the reason instead of sending to `undefined`: without
      // this the chat request below addresses a session that does not exist
      // and the assertion fails with a bare `group === undefined`.
      if (!created.ok || !created.groupId) {
        return { status: 0, body: { createFailed: created } }
      }

      // `session_id` goes in the QUERY STRING, not the body: the handler
      // resolves the target session from `?session_id=` or the
      // `chat_session_id` cookie (getSessionID) and never reads a body field.
      // Sending it in the body therefore addressed whatever session the cookie
      // pointed at — which was busy — and the handler answered
      // `{running:true, queued:true}` with no `group` key. That is the
      // "body.group === undefined" failure this test used to report.
      const r = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(created.groupId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'hello group' }),
      })
      return { status: r.status, body: await r.json(), groupId: created.groupId }
    })
    if (resp.groupId) createdGroups.push(resp.groupId)
    expect(resp.body, 'group creation must succeed before sending').not.toHaveProperty('createFailed')
    // Include the response body in the message: a bare `group === undefined`
    // does not say whether the server answered `{running:true, queued:true}`
    // (the session was already busy) or something else entirely.
    expect(resp.status, `chat responded: ${JSON.stringify(resp.body)}`).toBe(200)
    expect(resp.body.group, `chat responded: ${JSON.stringify(resp.body)}`).toBe(true)
  })
})

import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { NavigationPage } from '../pages/navigation.page'

/**
 * A notification for a GROUP turn must navigate to the GROUP timeline, never to
 * a hidden member row.
 *
 * Why this matters: a group member owns its own ACP connection, keyed by the
 * hidden MEMBER row id (decision #61), so the permission-state callback reaches
 * EmitSessionEvent with a member id. A member row has no title, is absent from
 * the session list, and opening it shows an empty standalone agent chat — so
 * clicking the resulting notification dropped the user on a broken session
 * instead of the discussion.
 *
 * Scope note: this spec covers the end-to-end navigation contract for a group
 * notification (fast, no shared-state mutation). The member→group RESOLUTION
 * itself — including the permission path that is the only one carrying a member
 * id — is pinned by Go tests:
 *   - service: TestRecordSessionNotification_MemberRowLogsGroup,
 *              TestResolveGroupTimelineID,
 *              TestMarkMissingSources_RepairsLegacyMemberRows
 *   - cmd/server: TestPermissionCallbackResolvesMemberToGroup
 * Driving a real permission here would need a blocking mode switch on a shared
 * agent, which leaks into sibling specs (observed) for coverage the Go tests
 * already provide.
 */
test.describe('group notifications', () => {
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    nav = new NavigationPage(page)
    await page.request.delete('/api/notifications')
  })

  /** Create a group (host + one distinct member) and return its id. */
  async function createGroup(page: Page): Promise<string> {
    const created = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()
      const g = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!g.ok) return { ok: false, created: g }
      // The member must be a DIFFERENT agent id: AddGroupMember dedups by agent
      // id and would otherwise hand back the host row.
      const added = await post('/api/group/members', { groupId: g.groupId, agentIds: ['acp-mock-b'] })
      return { ok: added.ok, groupId: g.groupId as string }
    })
    expect(created.ok, `group creation failed: ${JSON.stringify(created)}`).toBe(true)
    return created.groupId
  }

  test('a group turn logs the group and clicking it opens the group', async ({ page }) => {
    const groupId = await createGroup(page)

    // Drive a real group turn so the backend records a notification for it.
    await page.evaluate(async (gid) => {
      await fetch(`/api/ai/chat?session_id=${encodeURIComponent(gid)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: '请开始讨论' }),
      })
    }, groupId)

    // Stop the discussion the way a user does (the stop button's own endpoint).
    // It CANNOT be left to end on its own: a member's reply mentions the next
    // member, and each mention drains as its OWN user turn with a fresh speech
    // budget (a fresh orchestrator per drained message), so
    // chat.group_max_speeches never accumulates across the chain — the
    // discussion runs until the user stops it. Cancelling records a
    // `cancelled` notification (a notifiable terminal state, same as
    // completed), which is the row this spec then navigates with.
    //
    // A cancel landing in the gap between two member turns is a no-op (nothing
    // is running at that instant), and a member turn that was mid-flight when
    // an earlier cancel landed may have already relayed the floor to the next
    // member (a NEW running turn). So keep cancelling until BOTH hold: a row is
    // recorded AND this group is no longer running — the first cancel that
    // lands mid-run aborts that member turn before it can mention anyone else,
    // which is what actually ends the discussion.
    //
    // Do NOT cancel before the turn has started: a cancel that races the turn's
    // own startup aborts it before the agent connection exists, and that hung
    // start leaves the session stuck "running" with no terminal event — which
    // is worse than the discussion continuing. Wait for the runner first.
    await expect.poll(async () => {
      const sessions = await page.request.get('/api/ai/sessions')
      if (!sessions.ok()) return false
      const list = ((await sessions.json()) as { sessions?: Array<{ id?: string; running?: boolean }> }).sessions ?? []
      return list.some((s) => s.id === groupId && s.running === true)
    }, { timeout: 30000, message: 'the group turn should start running' }).toBe(true)

    await expect.poll(async () => {
      await page.request.post(`/api/ai/chat/cancel?session_id=${encodeURIComponent(groupId)}`)
      const sessions = await page.request.get('/api/ai/sessions')
      if (sessions.ok()) {
        const list = ((await sessions.json()) as { sessions?: Array<{ id?: string; running?: boolean }> }).sessions ?? []
        if (list.some((s) => s.id === groupId && s.running === true)) return 0
      }
      const res = await page.request.get('/api/notifications')
      if (!res.ok()) return -1
      const body = await res.json()
      return body.total ?? 0
    }, { timeout: 60000, message: 'the stopped group turn should record a notification and leave the group idle' }).toBeGreaterThan(0)

    // The stored target must be the group. Resolving each logged id as a group
    // must succeed — a member row would not answer the members endpoint.
    const body = await (await page.request.get('/api/notifications')).json()
    const ids = ((body.items ?? []) as Array<{ sessionId?: string }>)
      .map((i) => i.sessionId)
      .filter((v): v is string => !!v)
    expect(ids, 'the group turn should be logged').toContain(groupId)
    for (const id of ids) {
      const members = await (await page.request.get(`/api/group/members?groupId=${encodeURIComponent(id)}`)).json()
      expect(members.ok, `${id} must be a group timeline, not a hidden member row`).toBe(true)
    }

    // And the click lands on the group: the member stack only renders for a
    // group timeline, so it proves the target is not a member/standalone chat.
    await nav.switchToNotifications()
    const card = page.locator('.notif-card').first()
    await expect(card).toBeVisible({ timeout: 15000 })
    await card.click()

    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 15000 })
    await expect(
      page.locator('.session-item-group, .group-member-stack').first(),
      'the opened session must be the group (member stack present)',
    ).toBeVisible({ timeout: 15000 })

    // Leave the group idle so the next spec starts from a settled state.
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 60000 })
  })
})

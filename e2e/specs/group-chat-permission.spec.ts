import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { restoreNonBlockingMode } from '../helpers/agent-mode'
import { setGroupMaxSpeeches, restoreGroupMaxSpeeches } from '../helpers/group-config'

/**
 * E2E for the group-chat permission approval path.
 *
 * Regression target: a group member's manual approval was silently lost. The
 * frontend only knows the GROUP session id (the timeline it is subscribed to
 * and the id it echoes back), but every member runs on its own ACP connection
 * keyed by the MEMBER ROW id. RespondPermission looked the connection up by the
 * group id, found none, and returned 404 — the member's turn stayed blocked on
 * its pending permission until the stall watchdog killed the connection.
 *
 * Setup: acp-mock requests permission only outside bypass mode, so the MEMBER
 * is put into Code mode (persisted on the member row's context_state and
 * applied when its turn builds its ChatRequest) while the host stays in bypass.
 * The load-bearing assertion is not just that the respond call returns 200, but
 * that the member's turn RESUMES and emits its reply text — which the mock
 * sends only AFTER the permission is resolved. Without the fix the member never
 * produces that text.
 *
 * The host and member use DIFFERENT agent ids (acp-mock / acp-mock-b): a group
 * cannot contain the same agent twice, because AddGroupMember dedups by agent
 * id and would just return the host row.
 *
 * SERIAL + chromium only: the whole run shares one acp-mock subprocess, and
 * this spec switches the shared agent/session into a permission-blocking mode.
 */
test.describe.serial('group chat permission approval', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  // This spec deliberately leaves a member in Code mode (where the mock blocks
  // on a permission request). Restore the non-blocking mode so later specs are
  // not affected (see helpers/agent-mode.ts).
  test.afterAll(async () => {
    await restoreNonBlockingMode()
    await restoreGroupMaxSpeeches()
  })

  test('member permission is approved through the group timeline', async ({ page }) => {
    // Clean slate: cancel any parked turn and reset modes everywhere, so the
    // host member (which uses the agent default) is in bypass and routes rather
    // than blocking on a permission request of its own.
    await restoreNonBlockingMode()

    // Cap at one member speech so the turn terminates predictably (a global
    // setting; the afterAll above restores it). Without a cap the turn would run
    // a second member and block on another unanswered permission request.
    await setGroupMaxSpeeches(1)

    const setup = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()

      // Host = acp-mock (stays bypass). Member = acp-mock-b, a distinct id.
      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }

      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }

      const memberId = added.memberIds?.[0]
      // Guard against the dedup trap: the member row must NOT be the host row.
      if (!memberId || memberId === created.hostMemberId) {
        return { ok: false, step: 'dedup', memberId, hostMemberId: created.hostMemberId }
      }

      // Put the MEMBER into Code mode. The host keeps the agent default
      // (bypass), so only the member blocks on a permission request. The mode is
      // persisted on the member row and read when its turn builds its request.
      const patchResp = await fetch('/api/ai/session/update', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: memberId, modeId: 'code' }),
      })

      return { ok: true, groupId: created.groupId, memberId, patchStatus: patchResp.status }
    })

    expect(setup.ok).toBe(true)
    expect(setup.memberId).toBeTruthy()
    expect(setup.patchStatus).toBe(200)

    // Open the group from the session list and send, so the group turn runs
    // through the real UI path (the permission card must render on the GROUP
    // timeline, not on a hidden member session).
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${setup.groupId}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()
    await expect(chat.textarea).toBeVisible({ timeout: 15000 })

    await chat.sendMessage('请开始讨论')

    // The member blocks on a permission request; the card renders in the group
    // timeline.
    const permissionView = page.locator('.permission-approval-view').first()
    await expect(permissionView).toBeVisible({ timeout: 30000 })

    // Approve. The response MUST be 200: before the fix the backend looked the
    // connection up by the GROUP id, found none, and returned 404.
    const respondResponse = page.waitForResponse(
      (r) => r.url().includes('/api/ai/permission/respond') && r.request().method() === 'POST',
    )
    await page.locator('.permission-btn-allow').first().click()
    expect((await respondResponse).status()).toBe(200)

    await expect(permissionView).toHaveClass(/permission-responded/, { timeout: 10000 })

    // The member's turn RESUMES and emits the mock's reply text. The host's own
    // reply is a routing tag (no such text), so the first assistant bubble
    // carrying it proves a member turn completed after the approval.
    await expect(
      page.locator('.chat-message.assistant', { hasText: /mock ACP agent/i }).first(),
    ).toBeVisible({ timeout: 30000 })

    // The group turn ends cleanly (no lingering stop button).
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 60000 })
  })
})

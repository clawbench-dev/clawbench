import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E for the group host's private notes (密送 / BCC).
 *
 * The host may address a note to a named member only:
 *   <clawbench-mention targets="A">只有 A 看得到的内容</clawbench-mention>
 *
 * Two contracts are verified end to end:
 *   1. DISPLAY — the note renders as a collapsed card INSIDE the host's bubble
 *      (auditable), and its text never appears as ordinary prose in the body
 *      (DOMPurify would otherwise unwrap the unknown tag and show it).
 *   2. The tag itself never leaks into the body as raw markup.
 *
 * The mock host always attaches a note (see groupRoutingReply in cmd/acp-mock),
 * so this exercises the real host→timeline→render path.
 *
 * SERIAL + chromium only: shares the acp-mock subprocesses.
 */
test.describe.serial('group chat private notes (bcc)', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('host private note renders as a collapsed card, not as body prose', async ({ page }) => {
    // A host + a member, so the host has someone to route to (and note).
    const setup = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()
      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }
      const added = await post('/api/group/members', { groupId: created.groupId, agentIds: ['acp-mock-b'] })
      if (!added.ok) return { ok: false, step: 'add', added }
      await post('/api/group/settings', { groupId: created.groupId, maxRounds: 1 })
      return { ok: true, groupId: created.groupId as string, hostMemberId: created.hostMemberId as string }
    })
    expect(setup.ok).toBe(true)
    expect(setup.groupId).toBeTruthy()

    const chat = new ChatPage(page)

    // Open the group from the session list.
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${setup.groupId}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()
    await expect(chat.textarea).toBeVisible({ timeout: 15000 })

    // Start a turn; the host routes to the member and attaches a note.
    await chat.sendMessage('请开始讨论')

    // The host's bubble carries the note card.
    const hostBubble = page.locator(`.chat-message.assistant[data-msg-key]`).filter({ has: page.locator('.msg-bcc') }).first()
    await expect(hostBubble).toBeVisible({ timeout: 30000 })

    // (1) The card is INSIDE the bubble, not in the speaker row above it.
    await expect(hostBubble.locator('.msg-card .msg-bcc')).toHaveCount(1)
    await expect(hostBubble.locator('.msg-speaker .msg-bcc')).toHaveCount(0)

    // (2) Collapsed by default.
    const body = hostBubble.locator('.msg-bcc-body')
    await expect(body).toBeHidden()

    // (3) The note's text must NOT appear as ordinary prose anywhere in the
    //     message body (DOMPurify would otherwise unwrap the tag).
    const contentText = await hostBubble.locator('.msg-content-wrapper').innerText()
    expect(contentText).not.toContain('SECRET_BCC_FOR_')
    expect(contentText).not.toContain('clawbench-mention')

    // (4) Expanding reveals the note (the user can audit it).
    await hostBubble.locator('.msg-bcc-header').click()
    await expect(body).toBeVisible()
    await expect(body).toContainText('SECRET_BCC_FOR_')

    // Let the turn finish so the shared mock is not left busy.
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 90000 })
  })

  // The user is a routable participant: the host can name "User", which hands
  // the floor back (round ends, a system line announces it) and a note addressed
  // to the user renders expanded and labelled "to you".
  test('host can hand the floor to the user, with an expanded note', async ({ page }) => {
    // A host with NO AI members: the only routable name is "User", so the mock
    // host addresses the user (see groupRoutingReply).
    const setup = await page.evaluate(async () => {
      const post = async (url: string, body: unknown) =>
        (await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()
      const created = await post('/api/group/create', { hostAgentId: 'acp-mock' })
      if (!created.ok) return { ok: false, step: 'create', created }
      await post('/api/group/settings', { groupId: created.groupId, maxRounds: 1 })
      return { ok: true, groupId: created.groupId as string }
    })
    expect(setup.ok).toBe(true)

    // Reset the SPA to a clean state: the previous test left the app inside a
    // session, where the session-list button is not reachable the same way.
    await page.goto('/')
    await expect(page.locator('.chat-action-btn').first()).toBeVisible({ timeout: 15000 })

    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${setup.groupId}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()
    await expect(chat.textarea).toBeVisible({ timeout: 15000 })

    await chat.sendMessage('开始')

    // The round ends cleanly (no member turns) and a system line announces it.
    // Locale-agnostic: assert the row exists and is non-empty, not its wording.
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 60000 })
    const sysRow = page.locator('.chat-system-row').last()
    await expect(sysRow).toBeVisible({ timeout: 15000 })
    expect((await sysRow.innerText()).trim().length).toBeGreaterThan(0)

    // The user's note renders expanded and labelled "to you".
    const userCard = page.locator('.msg-bcc-user').first()
    await expect(userCard).toBeVisible({ timeout: 15000 })
    await expect(userCard).toContainText('SECRET_BCC_FOR_')
    // It is never behind a toggle.
    await expect(userCard.locator('.msg-bcc-body')).toBeVisible()
  })
})

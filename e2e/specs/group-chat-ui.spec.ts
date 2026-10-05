import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Real-UI verification for the four group-chat defects reported after v1:
 *   1. no custom avatar  → member bar / speaker header must render the agent's
 *      avatar (covered by unit tests; not asserted here).
 *   2. no streaming      → the reply text must GROW over time (not appear at once).
 *   3. speaker header must sit OUTSIDE the bubble (sibling of .msg-card).
 *   4. duplicate bubble  → sending one message must render exactly ONE user bubble.
 *
 * Uses acp-mock (deterministic, fast) as host+member so the group turn runs.
 */
test.describe.serial('group chat UI', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('group send: one user bubble, streaming reply, speaker outside bubble', async ({ page }) => {
    // Create a group (host = acp-mock) via API, then open it from the list.
    const gid = await page.evaluate(async () => {
      const r = await fetch('/api/group/create', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostAgentId: 'acp-mock' }),
      })
      const j = await r.json()
      return j.groupId as string
    })
    expect(gid).toBeTruthy()

    // Open the session list and click the group row (the drawer renders rows
    // with data-session-id; position is user-draggable so match by id).
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${gid}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()

    await expect(chat.textarea).toBeVisible({ timeout: 15000 })

    const userBefore = await page.locator('.chat-message.user').count()
    await chat.sendMessage('请自我介绍')

    // (4) exactly ONE new user bubble — checked again AFTER the turn ends, so a
    // late echo (the subscribe-recovery / WS replay path) would be caught too.
    await expect(page.locator('.chat-message.user')).toHaveCount(userBefore + 1, { timeout: 10000 })

    // (2) streaming: the assistant bubble must be observed in the STREAMING
    // state — i.e. present with content but WITHOUT the per-message action bar,
    // which ChatMessageItem renders only when `!msg.streaming`. A single-shot
    // (non-streaming) render would go straight to the action-bar state. Sample
    // in a tight loop right after the first content appears.
    const assistant = page.locator('.chat-message.assistant').last()
    await expect(assistant).toBeVisible({ timeout: 30000 })
    await expect(assistant).toContainText(/.+/, { timeout: 30000 })

    let sawStreamingState = false
    for (let i = 0; i < 40; i++) {
      const streaming = await assistant.getAttribute('data-streaming').catch(() => null)
      const actions = await assistant.locator('.chat-action-btn').count()
      if (streaming === 'true' || actions === 0) { sawStreamingState = true; break }
      await page.waitForTimeout(100)
    }
    expect(sawStreamingState).toBe(true)

    // (3) speaker header is a SIBLING of .msg-card, not a descendant.
    const speakerInsideCard = await page.locator('.msg-card .msg-speaker').count()
    expect(speakerInsideCard).toBe(0)
    const speakerOutside = await page.locator('.chat-message.assistant > .msg-speaker').count()
    expect(speakerOutside).toBeGreaterThan(0)

    // Let the turn finish so the next test is not affected.
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 60000 })

    // (4b) After completion + the frontend's authoritative loadHistory, the user
    // bubble count must STILL be exactly one — a late echo that survived to the
    // DB-backed rebuild would show up here as two.
    await page.waitForTimeout(1500)
    await expect(page.locator('.chat-message.user')).toHaveCount(userBefore + 1, { timeout: 10000 })
  })
})

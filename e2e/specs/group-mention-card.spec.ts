import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Real-UI verification for the group @ member card.
 *
 * Before this feature, picking a member from the @ menu wrote the raw protocol
 * tag into the textarea (`<clawbench-mention targets="…">…</clawbench-mention>`),
 * which the user had to read while composing. The card replaces it: the textarea
 * stays clean, a member card renders in the attachment strip, and the tag is
 * serialized only at send time.
 *
 * This spec drives the REAL UI (the @ completion menu, the card, the send), so
 * it is the only place that proves the input no longer shows the raw tag.
 */
test.describe.serial('group @ member card (UI)', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('picking a member adds a card, not a raw tag, and the member speaks', async ({ page }) => {
    // A FREE group (no host): the user's @ seeds the queue, so naming one member
    // makes exactly that member speak — the minimal deterministic turn.
    const gid = await page.evaluate(async () => {
      const r = await fetch('/api/group/create', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ memberAgentIds: ['acp-mock', 'acp-mock-b'] }),
      })
      const j = await r.json()
      return j.groupId as string
    })
    expect(gid).toBeTruthy()

    const chat = new ChatPage(page)
    // Open the session list via the STABLE hook, not `.chat-action-btn`.first()`:
    // a page restored onto a session WITH messages renders per-message action
    // buttons (Quote/Copy/…) that also carry `.chat-action-btn`, so the positional
    // selector can hit a message button when another spec ran first.
    await page.locator('[data-action="session"]').click()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${gid}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()

    const textarea = page.locator('.chat-textarea')
    await expect(textarea).toBeVisible({ timeout: 15000 })

    // Type "@" to open the member menu, then pick the first member.
    await textarea.fill('@')
    const menu = page.locator('.completion-item')
    await expect(menu.first()).toBeVisible({ timeout: 5000 })
    // The roster is listed FIRST (before file candidates) in a group session.
    await menu.first().click()

    // (1) The textarea must NOT contain the protocol tag — the whole point.
    await expect(textarea).not.toHaveValue(/clawbench-mention/)

    // (2) A member card renders in the attachment strip.
    const card = page.locator('.chat-attachment-tags .mention-card')
    await expect(card).toBeVisible({ timeout: 5000 })
    await expect(card.locator('.mention-card-name')).not.toBeEmpty()

    // Compose the rest of the message and send. `sendMessage` clears+refills the
    // textarea, which does NOT touch the card (it lives in the staged-mention
    // state, not in the text) — exactly the separation this feature introduces.
    await chat.sendMessage('请你说说')

    // (3) The sent user bubble shows an inline @name chip, never the raw tag.
    const userBubble = page.locator('.chat-message.user').last()
    await expect(userBubble).toBeVisible({ timeout: 10000 })
    await expect(userBubble).toContainText('@', { timeout: 10000 })
    await expect(userBubble).not.toContainText('clawbench-mention')

    // (4) The named member produced a timeline row (the @ routed to it).
    await expect(page.locator('.chat-message.assistant').first()).toBeVisible({ timeout: 60000 })
    await expect(page.locator('.chat-stop-btn')).toHaveCount(0, { timeout: 60000 })
  })
})

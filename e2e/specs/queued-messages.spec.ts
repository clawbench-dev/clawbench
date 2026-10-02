import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Queued-message ordering end-to-end tests.
 *
 * The ACP mock agent replies slowly (ACP_MOCK_REPLY_DELAY_MS), so sending
 * message 2 and 3 immediately after message 1 lands them in the queue. The
 * backend drain loop runs 1, 2, 3 sequentially; the frontend must render
 * 1, reply1, 2, reply2, 3, reply3 — both live and after a full reload.
 *
 * The three messages carry UNIQUE markers (not "1"/"2"/"3"): the run shares one
 * project and one session across specs, so the transcript may already contain
 * earlier messages. Assertions are therefore relative (A before B) rather than
 * absolute indices, which would depend on whatever ran before this spec.
 */
const M1 = 'qmsg-alpha'
const M2 = 'qmsg-bravo'
const M3 = 'qmsg-charlie'

test.describe('Queued messages ordering', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    // Block the upgrade check so the "New Version Available" overlay never
    // appears and cannot intercept clicks on the send button.
    await page.route('**/api/upgrade/**', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{"hasUpdate":false,"latestVersion":""}' }))
    // Dismiss the first-run Welcome overlay so the chat panel is reachable.
    await page.evaluate(() => localStorage.setItem('clawbench_welcome_dismissed', 'true'))
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
  })

  /** Snapshot the transcript as `role:text` pairs. */
  async function transcript(page: Page): Promise<string[]> {
    return await page.locator('.chat-message').evaluateAll(
      (els) => els.map((el) => {
        const role = el.classList.contains('user') ? 'user' : el.classList.contains('assistant') ? 'assistant' : 'other'
        // Keep the whole text: a user bubble may carry a meta bar, so a short
        // slice can drop the marker entirely.
        const text = (el.textContent || '').trim()
        return `${role}:${text}`
      })
    )
  }

  test('queued messages and their replies render in conversational order (live + after reload)', async ({ page }) => {
    // Capture frontend runtime errors to diagnose failures.
    const pageErrors: string[] = []
    page.on('pageerror', (err) => pageErrors.push(`PAGEERROR: ${err.message}`))
    page.on('console', (msg) => {
      if (msg.type() === 'error') pageErrors.push(`CONSOLE: ${msg.text()}`)
    })

    // 1. Send three messages quickly. The first starts the AI; 2 and 3 queue.
    await chat.sendMessage(M1)
    // No waitForReply between sends — 2/3 must land while 1 is generating.
    await chat.sendMessage(M2)
    await chat.sendMessage(M3)

    // 1b. INTERMEDIATE STATE (user-reported bug): right after reply1 appears,
    //     messages 2/3 must STILL render below 1/reply1 — they must never jump
    //     above. Wait for OUR first message bubble (the transcript may already
    //     hold other messages, so counting assistants is not enough).
    await expect(
      page.locator('.chat-message.user').filter({ hasText: M1 })
    ).toHaveCount(1, { timeout: 20000 })
    const midTexts = await transcript(page)
    const m1 = midTexts.findIndex((t) => t.startsWith('user:') && t.includes(M1))
    const m2 = midTexts.findIndex((t) => t.startsWith('user:') && t.includes(M2))
    const m3 = midTexts.findIndex((t) => t.startsWith('user:') && t.includes(M3))
    expect(m1, 'message 1 must be rendered').toBeGreaterThanOrEqual(0)
    const midReply1 = midTexts.findIndex((t, i) => i > m1 && t.startsWith('assistant'))
    expect(midReply1, 'reply to message 1 must follow it').toBeGreaterThan(m1)
    if (m2 !== -1) expect(m2).toBeGreaterThan(midReply1)
    if (m3 !== -1) expect(m3).toBeGreaterThan(midReply1)
    if (pageErrors.length > 0) throw new Error(pageErrors.join('\n'))

    // 1c. Repeated order checks WHILE streaming is still in progress.
    for (let i = 0; i < 4; i++) {
      const texts = await transcript(page)
      const i1 = texts.findIndex((t) => t.startsWith('user:') && t.includes(M1))
      const r1 = texts.findIndex((t, idx) => idx > i1 && t.startsWith('assistant'))
      const i2 = texts.findIndex((t) => t.startsWith('user:') && t.includes(M2))
      const i3 = texts.findIndex((t) => t.startsWith('user:') && t.includes(M3))
      if (i1 !== -1 && r1 !== -1) {
        if (i2 !== -1) expect(i2).toBeGreaterThan(r1)
        if (i3 !== -1) expect(i3).toBeGreaterThan(r1)
      }
      if (pageErrors.length > 0) throw new Error(pageErrors.join('\n'))
      await page.waitForTimeout(400)
    }

    // 2. Wait until all three replies are done (no streaming, no pending).
    //    Wait on OUR messages specifically: the transcript may already hold
    //    assistant messages from earlier specs, so counting assistants alone
    //    can be satisfied before ours have even been rendered.
    for (const marker of [M1, M2, M3]) {
      await expect(
        page.locator('.chat-message.user').filter({ hasText: marker })
      ).toHaveCount(1, { timeout: 30000 })
    }
    await expect(page.locator('.chat-message.user.pending')).toHaveCount(0, { timeout: 15000 })
    await expect(page.locator('.chat-message.assistant.streaming')).toHaveCount(0, { timeout: 15000 })

    // 3. Live order must be: m1, reply1, m2, reply2, m3, reply3 (relative).
    const liveTexts = await transcript(page)
    const l1 = liveTexts.findIndex((t) => t.startsWith('user:') && t.includes(M1))
    const lReply1 = liveTexts.findIndex((t, i) => i > l1 && t.startsWith('assistant'))
    const l2 = liveTexts.findIndex((t) => t.startsWith('user:') && t.includes(M2))
    const lReply2 = liveTexts.findIndex((t, i) => i > l2 && t.startsWith('assistant'))
    const l3 = liveTexts.findIndex((t) => t.startsWith('user:') && t.includes(M3))
    expect(l1).toBeGreaterThanOrEqual(0)
    expect(lReply1).toBeGreaterThan(l1)
    expect(l2).toBeGreaterThan(lReply1)
    expect(lReply2).toBeGreaterThan(l2)
    expect(l3).toBeGreaterThan(lReply2)

    // 4. Full reload (loadHistory from backend) — order must be identical.
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible()
    // Wait for the history to actually render before snapshotting: reload kicks
    // off an async loadHistory, and reading immediately can see an empty list.
    await expect(
      page.locator('.chat-message.user').filter({ hasText: M1 })
    ).toHaveCount(1, { timeout: 15000 })

    const reloadedTexts = await transcript(page)
    const r1 = reloadedTexts.findIndex((t) => t.startsWith('user:') && t.includes(M1))
    const rReply1 = reloadedTexts.findIndex((t, i) => i > r1 && t.startsWith('assistant'))
    const r2 = reloadedTexts.findIndex((t) => t.startsWith('user:') && t.includes(M2))
    const rReply2 = reloadedTexts.findIndex((t, i) => i > r2 && t.startsWith('assistant'))
    const r3 = reloadedTexts.findIndex((t) => t.startsWith('user:') && t.includes(M3))
    expect(r1).toBeGreaterThanOrEqual(0)
    expect(rReply1).toBeGreaterThan(r1)
    expect(r2).toBeGreaterThan(rReply1)
    expect(rReply2).toBeGreaterThan(r2)
    expect(r3).toBeGreaterThan(rReply2)
  })
})

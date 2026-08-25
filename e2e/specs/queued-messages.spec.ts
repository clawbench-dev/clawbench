import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Queued-message ordering end-to-end tests.
 *
 * The ACP mock agent responds within ~500ms, so the "AI is generating" window
 * is short. To reliably queue messages 2 and 3 behind message 1, we send
 * message 1, then IMMEDIATELY send 2 and 3 (before mock finishes replying).
 * The backend drain loop executes 1, then 2, then 3 sequentially; the
 * frontend must render 1, reply1, 2, reply2, 3, reply3 — both live and after
 * a full reload (loadHistory).
 */
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

  test('queued messages and their replies render in conversational order (live + after reload)', async ({ page }) => {
    // Capture frontend runtime errors to diagnose failures.
    const pageErrors: string[] = []
    page.on('pageerror', (err) => pageErrors.push(`PAGEERROR: ${err.message}`))
    page.on('console', (msg) => {
      if (msg.type() === 'error') pageErrors.push(`CONSOLE: ${msg.text()}`)
    })

    // 1. Send three messages quickly. Message 1 starts AI; 2 and 3 get queued.
    await chat.sendMessage('1')
    // No waitForReply between sends — 2/3 must land while 1 is generating.
    await chat.sendMessage('2')
    await chat.sendMessage('3')

    // 1b. INTERMEDIATE STATE (user-reported bug): right after reply1 appears,
    //     messages 2/3 must STILL render below 1/reply1 — they must never jump
    //     above. Poll until the first assistant reply exists, then check order.
    await expect
      .poll(async () => page.locator('.chat-message.assistant').count(), { timeout: 15000 })
      .toBeGreaterThanOrEqual(1)
    const midTexts = await page.locator('.chat-message').evaluateAll(
      (els) => els.map((el) => {
        const role = el.classList.contains('user') ? 'user' : el.classList.contains('assistant') ? 'assistant' : 'other'
        const text = (el.textContent || '').trim().slice(0, 30)
        return `${role}:${text}`
      })
    )
    // user:1 must be first (index 0), its reply right after (index ≥1), and
    // user:2/user:3 AFTER the reply — never above msg1/reply1.
    expect(midTexts[0]).toContain('user:1')
    const midIdxReply1 = midTexts.findIndex((t, i) => i > 0 && t.startsWith('assistant'))
    expect(midIdxReply1).toBeGreaterThan(0)
    const midIdx2 = midTexts.findIndex((t) => t.startsWith('user:2'))
    const midIdx3 = midTexts.findIndex((t) => t.startsWith('user:3'))
    if (midIdx2 !== -1) expect(midIdx2).toBeGreaterThan(midIdxReply1)
    if (midIdx3 !== -1) expect(midIdx3).toBeGreaterThan(midIdxReply1)
    if (pageErrors.length > 0) throw new Error(pageErrors.join('\n'))

    // 2. Wait until all three replies are done (no streaming, no pending).
    await expect
      .poll(async () => page.locator('.chat-message.assistant').count(), { timeout: 30000 })
      .toBeGreaterThanOrEqual(3)
    await expect(page.locator('.chat-message.user.pending')).toHaveCount(0, { timeout: 15000 })
    await expect(page.locator('.chat-message.assistant.streaming')).toHaveCount(0, { timeout: 15000 })

    // 3. Live order must be: user1, assistant(reply1), user2, assistant(reply2), user3, assistant(reply3).
    //    Match by content text since user messages are exactly "1","2","3".
    const liveTexts = await page.locator('.chat-message').evaluateAll(
      (els) => els.map((el) => {
        const role = el.classList.contains('user') ? 'user' : el.classList.contains('assistant') ? 'assistant' : 'other'
        const text = (el.textContent || '').trim().slice(0, 30)
        return `${role}:${text}`
      })
    )
    // First message: 1 + its reply
    expect(liveTexts[0]).toContain('user:1')
    expect(liveTexts[1]).toContain('assistant')
    // Second message 2 + reply2 BEFORE message 3
    const idx2 = liveTexts.findIndex((t) => t.startsWith('user:2'))
    const idxReply2 = liveTexts.findIndex((t, i) => i > idx2 && t.startsWith('assistant'))
    const idx3 = liveTexts.findIndex((t) => t.startsWith('user:3'))
    expect(idx2).toBeGreaterThanOrEqual(2)
    expect(idxReply2).toBeGreaterThan(idx2)
    expect(idx3).toBeGreaterThan(idxReply2)

    // 4. Full reload (loadHistory from backend) — order must be identical.
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible()

    const reloadedTexts = await page.locator('.chat-message').evaluateAll(
      (els) => els.map((el) => {
        const role = el.classList.contains('user') ? 'user' : el.classList.contains('assistant') ? 'assistant' : 'other'
        const text = (el.textContent || '').trim().slice(0, 30)
        return `${role}:${text}`
      })
    )
    expect(reloadedTexts[0]).toContain('user:1')
    const rIdx2 = reloadedTexts.findIndex((t) => t.startsWith('user:2'))
    const rIdxReply2 = reloadedTexts.findIndex((t, i) => i > rIdx2 && t.startsWith('assistant'))
    const rIdx3 = reloadedTexts.findIndex((t) => t.startsWith('user:3'))
    expect(rIdx2).toBeGreaterThanOrEqual(2)
    expect(rIdxReply2).toBeGreaterThan(rIdx2)
    expect(rIdx3).toBeGreaterThan(rIdxReply2)
  })
})

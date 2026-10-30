import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { ChatPage } from '../pages/chat.page'

/**
 * Queued-message ordering end-to-end tests.
 *
 * The ACP mock agent replies slowly (ACP_MOCK_REPLY_DELAY_MS), so sending
 * message 2 and 3 immediately after message 1 lands them in the queue. The
 * backend drain loop runs 1, 2, 3 sequentially; the frontend must render
 * 1, reply1, 2, reply2, 3, reply3 — both live and after a full reload.
 *
 * The three messages carry markers that are unique per ATTEMPT (not "1"/"2"/"3",
 * and not module-level fixed strings): the run shares one project and one session
 * across specs AND across Playwright retries. With fixed markers, attempt 1's
 * messages already satisfy attempt 2's `toHaveCount(1)` (the locator resolves to
 * 2 elements), so one failure makes every retry fail for a different reason than
 * the original one — the retries can never pass. Assertions are relative (A
 * before B) rather than absolute indices, which would depend on whatever ran
 * before this spec.
 */
let M1 = ''
let M2 = ''
let M3 = ''

/**
 * Reload, retrying once if WebKit throws its own internal navigation error.
 *
 * `page.reload()` on WebKit intermittently fails with "WebKit encountered an
 * internal error" while the page still holds the live chat WebSocket and
 * in-flight history fetches (measured in CI on this spec). That is a browser-side
 * fault, not an assertion about the app: the navigation either never starts or
 * dies mid-flight, so nothing about the product is being tested at that moment.
 * Re-issuing the SAME navigation is the only sound response — the assertions
 * that follow still run against a genuinely reloaded page.
 */
async function reloadPage(page: Page): Promise<void> {
  try {
    await page.reload()
  } catch (err) {
    if (!/WebKit encountered an internal error/i.test(String(err))) throw err
    await page.reload()
  }
  await page.waitForLoadState('domcontentloaded')
}

test.describe('Queued messages ordering', () => {
  let chat: ChatPage

  // This spec drives three sequential mock turns (1200ms mock delay each) and
  // its inner `expect` timeouts alone sum to ~95s, so the 30s config default is
  // not enough: on a loaded CI runner the happy path (~18s locally) overran it
  // and the test timed out. Every other multi-turn spec sets 120000 for the same
  // reason.
  test.setTimeout(120000)

  test.beforeEach(async ({ page }) => {
    // Fresh markers per attempt: the session is shared across specs and retries,
    // so a fixed marker would match an earlier attempt's bubble.
    const run = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    M1 = `qmsg-alpha-${run}`
    M2 = `qmsg-bravo-${run}`
    M3 = `qmsg-charlie-${run}`

    chat = new ChatPage(page)
    // Block the upgrade check so the "New Version Available" overlay never
    // appears and cannot intercept clicks on the send button.
    await page.route('**/api/upgrade/**', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{"hasUpdate":false,"latestVersion":""}' }))
    // Dismiss the first-run Welcome overlay so the chat panel is reachable.
    await page.evaluate(() => localStorage.setItem('clawbench_welcome_dismissed', 'true'))
    await reloadPage(page)
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
    await reloadPage(page)
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

  // ── Editing a queued message ──
  // The point of in-place editing: the text changes and the message keeps its
  // queue POSITION. A "recall to input + resend" would move it to the end, which
  // for a message that has waited many minutes is exactly the cost the user was
  // trying to avoid.
  test('editing a queued message keeps its place in the queue', async ({ page }) => {
    const run = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    const running = `qedit-run-${run}`
    const queuedA = `qedit-a-${run}`
    const queuedB = `qedit-b-${run}`
    const revisedB = `qedit-rev-${run}`

    // Message 1 starts the turn; 2 and 3 queue behind it. (Two messages would
    // leave only ONE queued entry, which cannot show a preserved position.)
    await chat.sendMessage(running)
    await chat.sendMessage(queuedA)
    await chat.sendMessage(queuedB)

    // Expand the queue panel; both queued entries must be listed.
    await expect(page.locator('.queued-bar')).toBeVisible({ timeout: 15000 })
    await page.locator('.queued-bar-header').click()
    const rows = page.locator('.queued-bar-item')
    await expect(rows).toHaveCount(2, { timeout: 15000 })

    // Edit the SECOND queued entry (the one that is third in line overall).
    const target = rows.filter({ hasText: queuedB })
    await expect(target).toHaveCount(1)
    await target.locator('[data-action="queue-edit"]').click()

    // The dialog is primed with the entry's current text.
    const dialogInput = page.locator('.dlg-textarea')
    await expect(dialogInput).toBeVisible({ timeout: 5000 })
    await expect(dialogInput).toHaveValue(queuedB)

    await dialogInput.fill(revisedB)
    await page.locator('.dlg-ok').click()

    // THE ASSERTION THAT MATTERS: the edited entry keeps its index. If the save
    // had re-queued it, it would now be last and queuedA would have moved up.
    await expect(page.locator('.queued-bar-item').nth(1)).toContainText(revisedB, { timeout: 10000 })
    await expect(page.locator('.queued-bar-item')).toHaveCount(2)
    await expect(page.locator('.queued-bar-item').nth(0)).toContainText(queuedA)

    // Let the queue drain, then reload: the edited text must be what the
    // backend stored (proves the edit was persisted, not a local-only change).
    await expect(page.locator('.queued-bar')).toHaveCount(0, { timeout: 90000 })
    await expect(page.locator('.chat-message.user').filter({ hasText: revisedB })).toHaveCount(1, { timeout: 30000 })
    await reloadPage(page)
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.chat-message.user').filter({ hasText: revisedB })).toHaveCount(1, { timeout: 30000 })
    await expect(page.locator('.chat-message.user').filter({ hasText: queuedB })).toHaveCount(0)
  })
})

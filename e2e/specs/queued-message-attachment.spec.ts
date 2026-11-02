import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { ChatPage } from '../pages/chat.page'
import path from 'path'
import fs from 'fs'

/**
 * A message queued while a turn is running must keep its IMAGE attachment when
 * the drain loop materializes it.
 *
 * Reported symptom: "排队消息中带了图片，执行到后，图片不会展示出来，但实际是发送
 * 成功的" — the queued message's bubble renders without its image, while the
 * backend did receive the attachment (the chat_history row carries it).
 *
 * The two send paths differ in exactly one way that can affect the bubble:
 *   - direct send  → the OPTIMISTIC bubble (built from the input bar's entries)
 *                    is what the user sees; the self-echo is skipped.
 *   - queued drain → the bubble is created by the `user_message` WS event
 *                    (reducer `ws_user_message`), whose `files` payload is the
 *                    only source of attachments.
 * So this spec pins the drain path end-to-end: queue a message WITH an image,
 * let it drain, and assert the bubble actually renders a thumbnail.
 *
 * The attachment is a 1x1 PNG: `isThumbableExt` accepts .png and the server's
 * thumbnail endpoint rescales it, so a real <img> must appear (not the
 * icon+filename fallback, which is what a failed/skipped thumb looks like).
 */
const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

test.describe('Queued message with an image attachment', () => {
  let chat: ChatPage

  test.setTimeout(180000)

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    await page.route('**/api/upgrade/**', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: '{"hasUpdate":false,"latestVersion":""}' }),
    )
    await page.evaluate(() => localStorage.setItem('clawbench_welcome_dismissed', 'true'))
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
  })

  /** Attach one image through the drawer (the single upload entry point). */
  async function attachImage(page: Page, filePath: string) {
    await page.locator('.chat-attach-btn').click()
    const drawer = page.locator('.bs-panel').filter({ has: page.locator('.ad-header') })
    await expect(drawer).toBeVisible({ timeout: 10000 })
    await drawer.locator('input[type="file"]:not([accept])').setInputFiles(filePath)
    // The drawer stays open until OK: the selection is a local draft that only
    // reaches the composer on confirm.
    await drawer.locator('.ad-footer-actions .fbtn-primary').click()
    await expect(drawer).toBeHidden({ timeout: 10000 })
    // The input bar's attachment strip is the surface the user sees while the
    // upload runs; wait until the transfer card is gone (upload finished).
    await expect(page.locator('.chat-input-wrapper .chat-file-attachment').first()).toBeVisible({ timeout: 20000 })
    await expect(page.locator('.chat-input-wrapper .attachment-pending')).toHaveCount(0, { timeout: 30000 })
  }

  test('the drained bubble renders the image thumbnail', async ({ page }) => {
    const run = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    const first = `qimg-run-${run}`
    const queued = `qimg-image-${run}`
    const imgPath = path.join(process.cwd(), `e2e-queued-image-${run}.png`)
    fs.writeFileSync(imgPath, Buffer.from(PNG_BASE64, 'base64'))

    const pageErrors: string[] = []
    page.on('pageerror', (err) => pageErrors.push(`PAGEERROR: ${err.message}`))

    try {
      // Keep a multi-turn backlog so attaching + sending the image is not a
      // race against the first reply: three messages queue ~3 mock turns.
      await chat.sendMessage(first)
      await chat.sendMessage(`qimg-filler-a-${run}`)
      await chat.sendMessage(`qimg-filler-b-${run}`)

      await attachImage(page, imgPath)
      await chat.sendMessage(queued)

      // The message MUST have gone through the queue, otherwise this spec would
      // silently test the direct-send path instead.
      await expect(page.locator('.queued-bar')).toBeVisible({ timeout: 10000 })

      const bubble = page.locator('.chat-message.user').filter({ hasText: queued })
      await expect(bubble).toHaveCount(1, { timeout: 60000 })

      const html = await bubble.evaluate((el) => el.outerHTML)
      // The attachment card itself must be there.
      await expect(bubble.locator('.chat-file-attachment'), `bubble html: ${html}`).toHaveCount(1, { timeout: 15000 })
      // …and it must be the IMAGE variant (thumbnail), not the file fallback.
      await expect(bubble.locator('img.attachment-thumb-img'), `bubble html: ${html}`).toHaveCount(1, { timeout: 15000 })

      // The LIVE bubble is only half the story: every turn ends with a
      // loadHistory that rebuilds the list from the DB (rebuildFromDb). If the
      // DB row lost its files, the image would vanish exactly when the turn
      // finishes — the reported "执行到后图片不显示".
      await expect(page.locator('.chat-message.assistant.streaming')).toHaveCount(0, { timeout: 90000 })
      await expect(page.locator('.chat-message.user.pending')).toHaveCount(0, { timeout: 30000 })
      const afterHtml = await bubble.evaluate((el) => el.outerHTML)
      await expect(bubble.locator('img.attachment-thumb-img'), `after-turn html: ${afterHtml}`).toHaveCount(1, { timeout: 20000 })

      // …and the reload (pure DB path, no live events at all).
      await page.reload()
      await page.waitForLoadState('domcontentloaded')
      const reloaded = page.locator('.chat-message.user').filter({ hasText: queued })
      await expect(reloaded).toHaveCount(1, { timeout: 20000 })
      const reloadedHtml = await reloaded.evaluate((el) => el.outerHTML)
      await expect(reloaded.locator('img.attachment-thumb-img'), `reloaded html: ${reloadedHtml}`).toHaveCount(1, { timeout: 20000 })

      if (pageErrors.length > 0) throw new Error(pageErrors.join('\n'))
    } finally {
      if (fs.existsSync(imgPath)) fs.unlinkSync(imgPath)
      // The upload lands in the REAL project's .clawbench/uploads/ (the e2e
      // server's project root is the repo root), so the copy must be removed
      // too — the local temp file above is only the source.
      const uploaded = path.join(process.cwd(), '.clawbench', 'uploads', path.basename(imgPath))
      if (fs.existsSync(uploaded)) fs.unlinkSync(uploaded)
    }
  })
})

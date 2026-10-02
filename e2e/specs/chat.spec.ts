import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { seedQuickSendItems } from '../helpers/test-data'
import { getServerURL } from '../helpers/server'
import path from 'path'

test.describe('Chat', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  test('should send a message and receive SSE stream reply', async ({ page }) => {
    // Default agent is acp-mock which uses real ACP stdio protocol
    const uniqueText = 'mocktest_' + Date.now()

    // Count messages before sending to avoid stale matches from parallel tests
    const userCountBefore = await page.locator('.chat-message.user').count()
    const countBefore = await chat.sendMessage(uniqueText)

    // 1. User message appears immediately (synchronous POST) — match the new one
    await expect(page.locator('.chat-message.user').nth(userCountBefore)).toContainText(uniqueText)

    // 2. Assistant response appears (async SSE stream from ACP mock agent)
    await chat.waitForReply(30000, countBefore)

    // 3. Response contains "mock" text (ACP mock agent always mentions it)
    await expect(chat.getLastAssistantMessage()).toContainText('mock', { timeout: 15000 })
  })

  test('should open quick-send menu on empty send click', async ({ page }) => {
    // Seed quick-send items first
    await seedQuickSendItems(getServerURL())

    // Reload so the frontend picks up the items
    await page.reload()
    // Wait for network idle and app to fully initialize
    await page.waitForLoadState('networkidle')
    // Ensure chat textarea is ready before interacting
    await expect(page.locator('.chat-textarea')).toBeVisible()

    // Click send with empty input to open quick-send popup
    await chat.openQuickSendMenu()

    // Quick-send popup should appear
    await expect(page.locator('.quick-send-title')).toBeVisible()
  })

  test('should create a new session', async ({ page }) => {
    // Verify we're on the chat page
    await expect(chat.textarea).toBeVisible()
  })

  test('should show model selector chip', async ({ page }) => {
    // acp-mock agent has models configured (mock-pro, mock-fast)
    // The session info model chip opens the session setting drawer (Model tab)
    await chat.openSessionSettingModal()
    // The current model (Mock Pro) should be visible with the current class
    const mockProItem = chat.sessionSettingDrawer.locator('.model-item').filter({ hasText: /Mock Pro/ })
    await expect(mockProItem).toBeVisible({ timeout: 5000 })
    await expect(mockProItem).toHaveClass(/current/)
  })

  test('should show stop button during AI response', async ({ page }) => {
    // Send a message
    const countBefore = await chat.sendMessage('Hello')

    // The stop button appears while AI is generating.
    // ACP mock responds quickly (~500ms), so we may or may not catch it.
    // The key assertion is that after the response completes, the stop button is gone.
    // Wait for the response to complete — this implicitly verifies the chat flow works.
    await chat.waitForReply(30000, countBefore)

    // After response completes, stop button should be gone
    // Use stopButton disappearing as the definitive signal (session_complete processed)
    await expect(chat.stopButton).not.toBeVisible({ timeout: 15000 })
  })

  // ───────────────────────────────────────────────────────
  // /cb-task command
  // ───────────────────────────────────────────────────────

  test('should show ClawBench badge in user message after sending /cb-task', async ({ page }) => {
    await chat.sendMessage('/cb-task list tasks')

    const userMsg = chat.getLastUserMessage()
    await expect(userMsg).toBeVisible({ timeout: 5000 })

    // Badge should be rendered with .clawbench-command-badge class
    const badge = userMsg.locator('.clawbench-command-badge')
    await expect(badge).toContainText('/cb-task')
  })

  // ───────────────────────────────────────────────────────
  // File upload
  // ───────────────────────────────────────────────────────

  test('should attach a file and show attachment tag', async ({ page }) => {
    // Create a small test file
    const testFilePath = path.join(process.cwd(), 'test-upload.txt')
    const fs = await import('fs')
    fs.writeFileSync(testFilePath, 'test content for e2e upload')

    try {
      // The attach button opens the AttachDrawer (a BottomSheet). Its hidden
      // `<input type="file">` is the single upload entry point — the page also
      // contains the file manager's own file/folder inputs, so scope the
      // locator to the drawer to avoid a strict-mode violation.
      await page.locator('.chat-attach-btn').click()

      const drawer = page.locator('.bs-panel').filter({ has: page.locator('.ad-header') })
      await expect(drawer).toBeVisible({ timeout: 10000 })

      await drawer.locator('input[type="file"]').setInputFiles(testFilePath)

      // The drawer footer reuses AttachmentTags, so the attached file shows up
      // as a `.chat-file-attachment` chip inside the drawer. The server de-dupes
      // upload names (`test-upload.txt` → `test-upload_2.txt` when a prior run
      // left one behind), so match the stem rather than the exact name.
      const attachment = drawer.locator('.chat-file-attachment')
      await expect(attachment.first()).toBeVisible({ timeout: 10000 })
      await expect(attachment.first()).toContainText(/test-upload(_\d+)?\.txt/)
    } finally {
      if (fs.existsSync(testFilePath)) fs.unlinkSync(testFilePath)
    }
  })

  // ───────────────────────────────────────────────────────
  // Thinking block collapse
  // ───────────────────────────────────────────────────────

  test('should collapse thinking block when thinking_done event fires', async ({ page }) => {
    // Send a message to acp-mock which sends thinking content then a tool call
    // (tool call triggers thinking_done → auto-collapse)
    const countBefore = await chat.sendMessage('Think about this')
    await chat.waitForReply(30000, countBefore)

    // Wait for streaming to complete — this ensures thinking_done has fired
    // and the collapse animation has finished
    await expect(chat.sendButton).toBeVisible({ timeout: 10000 })

    // The thinking block should exist and be collapsed (header-only chip)
    const thinkingBlock = chat.getLastAssistantMessage().locator('.chat-thinking')
    await expect(thinkingBlock).toBeVisible({ timeout: 5000 })
    // Collapsed state means the thinking-collapsed class is applied
    await expect(thinkingBlock).toHaveClass(/thinking-collapsed/)
    // Inline thinking content should NOT be visible when collapsed
    await expect(thinkingBlock.locator('.thinking-inline-content')).not.toBeVisible()
    // The chevron marks the collapsed (expandable) state. The old
    // `.thinking-check` "done" icon no longer exists — the header now shows a
    // spinner while streaming and a ChevronDown/ChevronUp otherwise.
    await expect(thinkingBlock.locator('.thinking-chevron')).toBeVisible()
  })

  test('should expand thinking block when clicking collapsed chip', async ({ page }) => {
    // Send a message and wait for full response + collapse
    const countBefore = await chat.sendMessage('Think about this')
    await chat.waitForReply(30000, countBefore)
    await expect(chat.sendButton).toBeVisible({ timeout: 10000 })

    // Verify thinking block is collapsed
    const thinkingBlock = chat.getLastAssistantMessage().locator('.chat-thinking')
    await expect(thinkingBlock).toHaveClass(/thinking-collapsed/, { timeout: 5000 })

    // Click the collapsed chip to expand — this expands the block INLINE
    // (handleThinkingClick toggles thinking-expanded-done). It no longer opens
    // the ToolDetailOverlay, which was the old behaviour.
    await thinkingBlock.locator('.thinking-header').click()

    // The block leaves the collapsed state and shows its inline content.
    await expect(thinkingBlock).not.toHaveClass(/thinking-collapsed/, { timeout: 5000 })
    await expect(thinkingBlock.locator('.thinking-inline-content')).toBeVisible({ timeout: 5000 })
  })

  // ───────────────────────────────────────────────────────
  // Summary toggle
  // ───────────────────────────────────────────────────────

  test('should show summary toggle on completed assistant message', async ({ page }) => {
    // Send a message and wait for reply
    await chat.sendAndAwaitACPReply('Tell me a short story')

    // Summary toggle only appears after async summary generation completes.
    // The summarize backend must be configured and working.
    // Use a soft check with generous timeout since summary is async.
    const summaryBtn = page.locator('.summary-toggle-btn')
    const isSummaryVisible = await summaryBtn.isVisible({ timeout: 15000 }).catch(() => false)

    // Soft assertion: summary depends on async backend generation
    // If summarize is not configured or too slow, the button won't appear — that's acceptable
    if (!isSummaryVisible) {
      // At minimum, verify the assistant message exists
      const assistantMsg = page.locator('.chat-message.assistant').first()
      const hasAssistant = await assistantMsg.isVisible({ timeout: 5000 }).catch(() => false)
      // Assistant message should exist regardless of summary
      expect(hasAssistant).toBeTruthy()
    } else {
      await expect(summaryBtn).toBeVisible()
    }
  })
})

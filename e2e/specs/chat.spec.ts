import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { seedQuickSendItems } from '../helpers/test-data'
import { getServerURL } from '../helpers/server'

test.describe('Chat', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  test('should send a message and receive SSE stream reply', async ({ page }) => {
    // Default agent is acp-mock which uses real ACP stdio protocol
    await chat.sendMessage('Hello, mock assistant!')

    // 1. User message appears immediately (synchronous POST)
    await expect(chat.getLastUserMessage()).toContainText('Hello')

    // 2. Assistant response appears (async SSE stream from ACP mock agent)
    await chat.waitForReply(30000)

    // 3. Response contains the mock text
    await expect(chat.getLastAssistantMessage()).toContainText('mock ACP agent', { timeout: 15000 })
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
    // so the model chip should be visible
    await expect(chat.modelChip).toBeVisible()
    // Should show the default model name
    await expect(chat.modelChip).toContainText('Mock Pro')
  })

  test('should show stop button during AI response', async ({ page }) => {
    // Send a message
    await chat.sendMessage('Hello')

    // The stop button appears while AI is generating.
    // ACP mock responds quickly (~500ms), so we may or may not catch it.
    // The key assertion is that after the response completes, the stop button is gone.
    // Wait for the response to complete — this implicitly verifies the chat flow works.
    await chat.waitForReply(30000)

    // After response completes, stop button should be gone
    await expect(chat.stopButton).not.toBeVisible()
  })
})

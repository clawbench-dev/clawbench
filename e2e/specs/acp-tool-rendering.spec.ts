import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for ACP tool rendering in chat messages.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which sends a Read tool call
 * with title "Reading project files", kind=Read, location=/project/README.md,
 * and structured output on completion.
 *
 * Key rendering features tested:
 * 1. Tool call blocks (.chat-tool-call) appear in assistant messages
 * 2. ACP kind=Read maps to tool name "Read" and data-category="file"
 * 3. Tool summary is derived from input fields (path → "README.md")
 * 4. Tool detail overlay opens on click with tool name, input, and output
 * 5. No spinners remain after response completes (all tool calls done)
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Tool Rendering', () => {
  test.setTimeout(120000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  test('should render ACP tool calls with structured format', async ({ page }) => {
    // Send a message and wait for the full ACP reply
    await chat.sendAndAwaitACPReply('read the project files')

    // Tool call blocks should appear in the assistant message
    const toolCalls = page.locator('.chat-tool-call')
    await expect(toolCalls.first()).toBeVisible({ timeout: 15000 })

    // The acp-mock sends a Read tool call — verify the tool name is "Read"
    const readToolCall = toolCalls.filter({ hasText: /Read/i })
    await expect(readToolCall.first()).toBeVisible({ timeout: 5000 })

    // Tool summary is derived from the input field "path" → basename "README.md"
    const toolSummary = readToolCall.first().locator('.tool-summary')
    await expect(toolSummary).toContainText('README.md', { timeout: 5000 })

    // ACP kind=Read maps to data-category="file" (via getToolIcon utility)
    const fileCategoryTool = page.locator('.chat-tool-call[data-category="file"]')
    await expect(fileCategoryTool.first()).toBeVisible({ timeout: 5000 })
  })

  test('should show tool detail overlay on tool click', async ({ page }) => {
    // Send a message and wait for the full ACP reply
    await chat.sendAndAwaitACPReply('read the project files')

    // Wait for a completed tool call to be visible and clickable
    const toolCall = page.locator('.chat-tool-call.done').first()
    await expect(toolCall).toBeVisible({ timeout: 15000 })

    // Click the tool call to open the detail overlay
    await toolCall.click()

    // The ToolDetailOverlay (BottomSheet) should appear with tool detail header
    const overlayHeader = page.locator('.tool-detail-header')
    await expect(overlayHeader.first()).toBeVisible({ timeout: 5000 })

    // The overlay header should contain the tool name "Read"
    const overlayName = overlayHeader.locator('.tool-detail-header-name')
    await expect(overlayName.first()).toContainText('Read', { timeout: 5000 })

    // The overlay header summary shows the input-derived summary ("README.md")
    const overlaySummary = overlayHeader.locator('.tool-detail-header-summary')
    await expect(overlaySummary.first()).toContainText('README.md', { timeout: 5000 })

    // The overlay body should be visible (tool detail content rendered)
    const overlayBody = page.locator('.tool-detail-body')
    await expect(overlayBody.first()).toBeVisible({ timeout: 5000 })

    // Close the overlay by clicking the backdrop overlay
    const overlay = page.locator('.bs-overlay')
    await overlay.click()
    await expect(overlayHeader.first()).not.toBeVisible({ timeout: 5000 })
  })

  test('should show tool spinner during execution and stop on completion', async ({ page }) => {
    // Send a message but DON'T wait for the reply yet — observe the streaming state
    const countBefore = await chat.sendMessage('read the project files')

    // Wait for the assistant message to appear
    const newMsg = page.locator('.chat-message.assistant').nth(countBefore)
    await expect(newMsg).toBeVisible({ timeout: 15000 })

    // During streaming, tool call blocks may appear with spinners (.tool-spinner)
    // or done state (.done). We just need to verify that after
    // the response completes, there are no active spinners.

    // Wait for streaming to fully complete — stop button gone, send button back
    await expect(chat.stopButton).not.toBeVisible({ timeout: 30000 })
    await expect(chat.sendButton).toBeVisible({ timeout: 5000 })

    // After completion, all tool calls should be in "done" state
    // (no .tool-spinner should be visible)
    const activeSpinners = page.locator('.chat-tool-call .tool-spinner')
    const spinnerCount = await activeSpinners.count()
    expect(spinnerCount).toBe(0)

    // At least one tool call should exist in the response
    const toolCalls = page.locator('.chat-tool-call')
    const toolCount = await toolCalls.count()
    expect(toolCount).toBeGreaterThanOrEqual(1)
  })
})

import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for ACP session state persistence.
 *
 * ACP state (mode, thinking effort, commands, model list) should persist
 * across page reloads and session switches. This is critical for mobile use
 * where the browser may be backgrounded and the page reloaded on resume.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which provides:
 * - 3 modes (Code, Plan, Bypass Permissions)
 * - 8 slash commands (commit, help, review, test, plan, fix, search, doc)
 * - 3 thinking effort levels (Low/Medium/High)
 *
 * Persistence mechanisms tested:
 * 1. Mode chip text restored after page reload (via GET /api/ai/chat modeState)
 * 2. Slash commands restored after page reload (via prefetchCommands GET /api/ai/commands)
 * 3. ACP state restored when switching back to a previous session
 * 4. Thinking effort selection restored after page reload
 *
 * IMPORTANT: ACP connections are lazy — established on first message.
 * The first test must send a message to warm up the ACP connection pool.
 * After that, subsequent tests can rely on cached ACP state and REST API
 * responses for state restoration.
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Session State Persistence', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  // ───────────────────────────────────────────────────────
  // Mode persistence
  // ───────────────────────────────────────────────────────

  test('should restore mode chip after page reload', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for mode_update SSE event and mode chip to appear
    const modeChip = page.locator('.mode-chip')
    await expect(modeChip).toBeVisible({ timeout: 15000 })

    // Note the current mode text (should be "Code" — the default)
    const modeTextBefore = await modeChip.textContent()
    expect(modeTextBefore).toBeTruthy()

    // Switch to a different mode to make the test meaningful
    await chat.openModeMenu()
    await chat.selectMode('Plan')
    await expect(modeChip).toContainText('Plan', { timeout: 5000 })

    // Reload the page — mode should be restored from backend API
    await page.reload()
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Mode chip should reappear with "Plan" (restored from API response)
    const modeChipAfter = page.locator('.mode-chip')
    await expect(modeChipAfter).toBeVisible({ timeout: 15000 })
    await expect(modeChipAfter).toContainText('Plan', { timeout: 5000 })
  })

  // ───────────────────────────────────────────────────────
  // Slash commands persistence
  // ───────────────────────────────────────────────────────

  test('should restore slash commands after page reload', async ({ page }) => {
    // ACP connection is already warm from previous test
    // Wait for commands to be cached
    await chat.waitForACPCommands()

    // Reload the page — prefetchCommands should load slash commands via
    // GET /api/ai/commands without needing to send a message first
    await page.reload()
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)

    // Wait for textarea to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Type / to trigger slash command menu — should work without sending a message
    await chat.textarea.click()
    await chat.textarea.fill('/')

    // Slash command menu should appear with ACP commands (loaded via prefetch)
    const slashItems = page.locator('.at-menu-label.slash-label')
    await expect(slashItems.first()).toBeVisible({ timeout: 10000 })

    const count = await slashItems.count()
    expect(count).toBeGreaterThan(0)

    // Verify some known commands from acp-mock are present
    const allTexts = await slashItems.allTextContents()
    const hasCommit = allTexts.some(t => t.includes('commit'))
    const hasHelp = allTexts.some(t => t.includes('help'))
    expect(hasCommit || hasHelp).toBe(true)
  })

  // ───────────────────────────────────────────────────────
  // Session switch persistence
  // ───────────────────────────────────────────────────────

  test('should restore ACP state when switching back to session', async ({ page }) => {
    // ACP connection is already warm. Current session has "Plan" mode.
    // Wait for mode chip to confirm state
    const modeChip = page.locator('.mode-chip')
    await expect(modeChip).toBeVisible({ timeout: 15000 })
    await expect(modeChip).toContainText('Plan', { timeout: 5000 })

    // Create a new session with the same agent
    await chat.createSessionWithAgent('acp-mock')

    // Wait for the new session to be ready
    await page.waitForTimeout(1000)

    // The new session should also show a mode chip (ACP state prefetched on switch)
    const newSessionModeChip = page.locator('.mode-chip')
    await expect(newSessionModeChip).toBeVisible({ timeout: 15000 })

    // Now switch back to the original session (first in the list)
    // Open session drawer, click the first session item
    await chat.openSessionList()

    // Wait for session drawer to open
    const sessionDrawer = page.locator('.session-drawer, .drawer-content')
    await expect(sessionDrawer.first()).toBeVisible({ timeout: 5000 })

    // Click the second session in the list (the original one with "Plan" mode)
    const sessionItems = page.locator('.session-item')
    const sessionCount = await sessionItems.count()
    expect(sessionCount).toBeGreaterThanOrEqual(2)

    // Click the second session (the older one with Plan mode)
    await sessionItems.nth(1).click()
    await page.waitForTimeout(1000)

    // Mode chip should still show "Plan" for the original session
    const restoredModeChip = page.locator('.mode-chip')
    await expect(restoredModeChip).toBeVisible({ timeout: 15000 })
    await expect(restoredModeChip).toContainText('Plan', { timeout: 5000 })
  })

  // ───────────────────────────────────────────────────────
  // Thinking effort persistence
  // ───────────────────────────────────────────────────────

  test('should restore thinking effort state after page reload', async ({ page }) => {
    // Warm up ACP connection (session switch may have reset state)
    await chat.sendAndAwaitACPReply('hi')
    await page.waitForTimeout(2000)

    // Open ModelModal → thinking tab → select "High"
    await chat.openModelModal()
    await chat.openThinkingTab()
    await chat.selectThinkingEffort('High')

    // Modal closes after selection
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })

    // Reload page — thinking effort should be restored from backend
    await page.reload()
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Open ModelModal → thinking tab — "High" should be the active selection
    await chat.openModelModal()
    await chat.openThinkingTab()

    // The "High" item should have the active/selected class
    const highItem = page.locator('.thinking-item').filter({ hasText: /high/i })
    await expect(highItem).toBeVisible()
    await expect(highItem).toHaveClass(/active|selected/, { timeout: 5000 })
  })
})

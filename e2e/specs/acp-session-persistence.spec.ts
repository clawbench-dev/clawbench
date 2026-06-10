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
  test.setTimeout(120000)

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

    // Wait for mode_update SSE event — verify via settings chip + modal
    // openModeMenu waits for ACP mode state before opening
    await chat.openModeMenu()

    // Switch to a different mode to make the test meaningful
    await chat.selectMode('Plan')

    // Modal closes after selection
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })

    // Wait for mode to be persisted via PATCH before reloading
    await chat.waitForSessionMode('plan')

    // Reload the page — mode should be restored from backend API
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Wait for ACP mode state to be restored from backend API
    await chat.waitForACPModeState()

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Open the mode menu again — "Plan" should be the current selection
    await chat.openModeMenu()

    // The "Plan" item should have the current class (active selection)
    const planItem = page.locator('.thinking-item').filter({ hasText: /Plan/i })
    await expect(planItem).toBeVisible({ timeout: 5000 })
    await expect(planItem).toHaveClass(/current/, { timeout: 5000 })
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

    // Wait for textarea to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Wait for commands to be available via REST API
    await chat.waitForACPCommands()

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
    // Wait for ACP mode state before opening the menu
    await chat.waitForACPModeState()

    // Open mode menu to verify state
    await chat.openModeMenu()

    // Verify "Plan" is selected
    const planItem = page.locator('.thinking-item').filter({ hasText: /Plan/i })
    await expect(planItem).toBeVisible({ timeout: 5000 })
    await expect(planItem).toHaveClass(/current/, { timeout: 5000 })

    // Close modal by pressing Escape
    await page.keyboard.press('Escape')
    await page.waitForTimeout(500)

    // Create a new session with the same agent
    await chat.createSessionWithAgent('acp-mock')

    // Wait for the new session to be ready
    await chat.waitForACPState()

    // Now switch back to the original session (first in the list)
    // Open session drawer, click the first session item
    await chat.openSessionList()

    // Wait for session drawer (BottomSheet) to open
    const sessionDrawer = page.locator('.bs-panel')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })

    // Click the second session in the list (the older one with Plan mode)
    const sessionItems = page.locator('.session-item')
    const sessionCount = await sessionItems.count()
    expect(sessionCount).toBeGreaterThanOrEqual(2)

    // Click the second session (the older one with Plan mode)
    await sessionItems.nth(1).click()

    // Wait for mode to be restored to "plan" from backend
    await chat.waitForSessionMode('plan', 10000)

    // Wait for ACP mode state to be restored for this session
    await chat.waitForACPModeState()

    // Open mode menu to verify "Plan" is still selected for the original session
    await chat.openModeMenu()
    const restoredPlanItem = page.locator('.thinking-item').filter({ hasText: /Plan/i })
    await expect(restoredPlanItem).toBeVisible({ timeout: 5000 })
    await expect(restoredPlanItem).toHaveClass(/current/, { timeout: 5000 })
  })

  // ───────────────────────────────────────────────────────
  // Thinking effort persistence
  // ───────────────────────────────────────────────────────

  test('should restore thinking effort state after page reload', async ({ page }) => {
    // Warm up ACP connection (session switch may have reset state)
    await chat.sendAndAwaitACPReply('hi')

    // Open SessionSettingModal → thinking tab → select "High"
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()
    await chat.selectThinkingEffort('High')

    // Modal closes after selection
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })

    // Wait for thinking effort to be persisted via PATCH before reloading
    await chat.waitForSessionThinkingEffort('high')

    // Reload page — thinking effort should be restored from backend
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Wait for ACP state to be restored from backend API
    await chat.waitForACPState()

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Open SessionSettingModal → thinking tab — "High" should be the active selection
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()

    // The "High" item should have the active/selected class
    const highItem = page.locator('.thinking-item').filter({ hasText: /high/i })
    await expect(highItem).toBeVisible()
    await expect(highItem).toHaveClass(/current/, { timeout: 5000 })
  })
})

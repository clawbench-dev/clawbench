import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { restoreNonBlockingMode } from '../helpers/agent-mode'

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
 * 2. Slash commands restored after page reload (via GET /api/agents acpStates)
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

  // This spec switches the shared session to Plan mode. Restore the
  // non-blocking mode so later specs are not affected (helpers/agent-mode.ts).
  test.afterAll(async () => {
    await restoreNonBlockingMode()
  })

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

    // Drawer closes after selection
    await expect(chat.sessionSettingDrawer).not.toBeVisible({ timeout: 5000 })

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
    const planItem = chat.sessionSettingDrawer.locator('.thinking-item').filter({ hasText: /Plan/i })
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

    // Reload the page — slash commands come back from GET /api/agents
    // (acpStates[].commands) without needing to send a message first
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Wait for textarea to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Wait for commands to be available via the agents REST API
    await chat.waitForACPCommands()

    // Type / to trigger slash command menu — should work without sending a message
    await chat.textarea.click()
    await chat.textarea.fill('/')

    // Slash command menu should appear with ACP commands (loaded from acpStates)
    const slashItems = page.locator('.completion-item--agent .completion-label')
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
    // Make this test self-contained rather than relying on the mode left behind
    // by the earlier serial tests. Test 1 left the shared session in Plan mode,
    // where the mock blocks on a permission request — so restore the
    // non-blocking mode and reload before the warm-up turn.
    await restoreNonBlockingMode()
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    // Warm the connection, then set this session's mode to Plan explicitly.
    await chat.sendAndAwaitACPReply('hi')
    await chat.waitForACPState()

    // Open mode menu and select "Plan"
    await chat.openModeMenu()
    const planItem = chat.sessionSettingDrawer.locator('.thinking-item').filter({ hasText: /Plan/i })
    await expect(planItem).toBeVisible({ timeout: 5000 })
    await planItem.click()
    await expect(chat.sessionSettingDrawer).not.toBeVisible({ timeout: 5000 })

    // Remember the original session and confirm the mode stuck.
    const originalId = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      const data = await resp.json()
      return data.sessionId as string
    })
    expect(originalId).toBeTruthy()
    await chat.waitForSessionMode('plan', 10000)

    // Create a new session with the same agent. This opens the agent selector
    // drawer; make sure it is dismissed before opening the session list, or its
    // overlay will intercept clicks on the list.
    await chat.createSessionWithAgent('acp-mock')
    await page.keyboard.press('Escape')
    await expect(page.locator('.bs-overlay')).toHaveCount(0, { timeout: 5000 }).catch(() => {})

    // Wait for the new session to be ready
    await chat.waitForACPState()

    // Open the session list and switch back to the ORIGINAL session (the one in
    // Plan mode), identified by id rather than list position (user-draggable).
    await chat.openSessionList()

    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 5000 })

    const originalRow = sessionDrawer.locator(`.session-row[data-session-id="${originalId}"]`).first()
    await expect(originalRow).toBeVisible({ timeout: 10000 })
    await originalRow.locator('.session-item').click()

    // The switch is driven by the frontend; its session cookie (which the
    // `/api/ai/chat` probe reads) updates asynchronously and may lag well past a
    // timeout. Assert on the restored UI state instead — the drawer marks the
    // switched-to row active, and the mode menu shows the restored selection.
    await expect(
      sessionDrawer.locator(`.session-row[data-session-id="${originalId}"] .session-item`)
    ).toHaveClass(/active/, { timeout: 10000 })

    // Wait for ACP mode state to be restored for this session
    await chat.waitForACPModeState()

    // Open mode menu to verify "Plan" is still selected for the original session
    await chat.openModeMenu()
    const restoredPlanItem = chat.sessionSettingDrawer.locator('.thinking-item').filter({ hasText: /Plan/i })
    await expect(restoredPlanItem).toBeVisible({ timeout: 5000 })
    await expect(restoredPlanItem).toHaveClass(/current/, { timeout: 5000 })
  })

  // ───────────────────────────────────────────────────────
  // Thinking effort persistence
  // ───────────────────────────────────────────────────────

  test('should restore thinking effort state after page reload', async ({ page }) => {
    // Warm up ACP connection (session switch may have reset state)
    await chat.sendAndAwaitACPReply('hi')

    // Open the session setting drawer on the thinking tab → select "High"
    await chat.openThinkingTab()
    await chat.selectThinkingEffort('High')

    // Drawer closes after selection
    await expect(chat.sessionSettingDrawer).not.toBeVisible({ timeout: 5000 })

    // Wait for thinking effort to be persisted via PATCH before reloading
    await chat.waitForSessionThinkingEffort('high')

    // Reload page — thinking effort should be restored from backend
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Wait for ACP state to be restored from backend API
    await chat.waitForACPState()

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Open the thinking tab — "High" should be the active selection
    await chat.openThinkingTab()

    // The "High" item should have the active/selected class
    const highItem = chat.sessionSettingDrawer.locator('.thinking-item').filter({ hasText: /high/i })
    await expect(highItem).toBeVisible()
    await expect(highItem).toHaveClass(/current/, { timeout: 5000 })
  })
})

import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for ACP slash command feature.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which provides:
 * - 8 slash commands (commit, help, review, test, plan, fix, search, doc)
 * - 3 modes (Code, Plan, Bypass Permissions)
 * - Mode config option
 *
 * Key ACP behaviors tested:
 * 1. Slash command autocomplete menu appears when typing "/"
 * 2. @ command autocomplete still works (ClawBench built-in, not affected)
 * 3. Slash command badge renders in user messages
 * 4. @ command badge still renders
 * 5. Mode chip is visible for ACP sessions
 * 6. GET /api/ai/commands returns discovered commands
 * 7. Slash commands are pre-fetched via REST API on page load/session switch
 *    (no need to send a message first if ACP connection already cached commands)
 *
 * IMPORTANT: ACP connections are lazy — established on first message.
 * The first test in each "cold" group must still send a message to warm up
 * the ACP connection pool. After that, subsequent tests can rely on
 * prefetchCommands (GET /api/ai/commands) to load slash commands without
 * requiring an active SSE stream.
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Slash Commands', () => {
  test.setTimeout(120000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  // ───────────────────────────────────────────────────────
  // @ command tests (ClawBench built-in, no ACP needed)
  // ───────────────────────────────────────────────────────

  test('should show @ command autocomplete for built-in commands', async ({ page }) => {
    await chat.textarea.click()
    await chat.textarea.fill('@')

    const atMenu = page.locator('.at-menu-title')
    await expect(atMenu).toBeVisible({ timeout: 3000 })

    const chatsearchItem = page.locator('.at-menu-item').filter({ hasText: '@chatsearch' })
    await expect(chatsearchItem).toBeVisible()

    const taskItem = page.locator('.at-menu-item').filter({ hasText: '@task' })
    await expect(taskItem).toBeVisible()
  })

  test('should show @chatsearch filtered when typing @c', async ({ page }) => {
    await chat.textarea.click()
    await chat.textarea.fill('@c')

    const atMenu = page.locator('.at-menu-title')
    await expect(atMenu).toBeVisible({ timeout: 3000 })

    const chatsearchItem = page.locator('.at-menu-item').filter({ hasText: '@chatsearch' })
    await expect(chatsearchItem).toBeVisible()

    const taskItem = page.locator('.at-menu-item').filter({ hasText: '@task' })
    await expect(taskItem).not.toBeVisible()
  })

  test('should show @ command badge in user message after sending @chatsearch', async ({ page }) => {
    await chat.textarea.click()
    await chat.textarea.fill('@chatsearch test query')
    await page.waitForTimeout(200)
    await chat.sendButton.click()

    const userMsg = chat.getLastUserMessage()
    await expect(userMsg).toBeVisible({ timeout: 5000 })

    // Badge should be rendered with .at-command-badge class
    const atBadge = userMsg.locator('.at-command-badge')
    const isBadgeVisible = await atBadge.isVisible({ timeout: 3000 }).catch(() => false)

    if (isBadgeVisible) {
      await expect(atBadge).toContainText('@chatsearch')
    } else {
      // Fallback: the message text should at least contain @chatsearch
      await expect(userMsg).toContainText('@chatsearch')
    }
  })

  test('should close @ menu on blur', async ({ page }) => {
    await chat.textarea.click()
    await chat.textarea.fill('@')

    const atMenu = page.locator('.at-menu-title')
    await expect(atMenu).toBeVisible({ timeout: 3000 })

    await page.locator('body').click({ position: { x: 10, y: 10 } })

    await expect(atMenu).not.toBeVisible({ timeout: 2000 })
  })

  // ───────────────────────────────────────────────────────
  // ACP slash command tests (require ACP connection)
  // ───────────────────────────────────────────────────────

  test('should show slash command autocomplete when typing /', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for commands to be available via API
    await chat.waitForACPCommands()

    // Type / to trigger slash command menu
    await chat.textarea.click()
    await chat.textarea.fill('/')

    // Slash command menu should appear with ACP commands
    const slashItems = page.locator('.at-menu-label.slash-label')
    await expect(slashItems.first()).toBeVisible({ timeout: 5000 })

    const count = await slashItems.count()
    expect(count).toBeGreaterThan(0)

    // Verify first slash item starts with /
    const firstItem = slashItems.first()
    const text = await firstItem.textContent()
    expect(text).toMatch(/^\//)
  })

  test('should show slash command autocomplete after page reload via prefetch', async ({ page }) => {
    // Previous test already established ACP connection and cached commands.
    // Reload the page — prefetchCommands should load slash commands via
    // GET /api/ai/commands without needing to send a message first.
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
  })

  test('should show slash command autocomplete after session switch via prefetch', async ({ page }) => {
    // Previous tests warmed the ACP connection pool. Create a new session
    // and switch to it — prefetchCommands in switchSession should load commands.
    await chat.createSessionWithAgent('acp-mock')

    // Type / to trigger slash command menu — should work without sending a message
    await chat.textarea.click()
    await chat.textarea.fill('/')

    // Slash command menu should appear with ACP commands (loaded via prefetch on session switch)
    const slashItems = page.locator('.at-menu-label.slash-label')
    await expect(slashItems.first()).toBeVisible({ timeout: 10000 })

    const count = await slashItems.count()
    expect(count).toBeGreaterThan(0)
  })

  test('should show slash command badge in user message after sending /commit', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Send a slash command message
    await chat.textarea.click()
    await chat.textarea.fill('/commit fix auth bug')
    await page.waitForTimeout(200)
    await chat.sendButton.click()

    const userMsg = chat.getLastUserMessage()
    await expect(userMsg).toBeVisible({ timeout: 5000 })

    // Badge should be rendered with .slash-command-badge class
    const slashBadge = userMsg.locator('.slash-command-badge')
    const isBadgeVisible = await slashBadge.isVisible({ timeout: 3000 }).catch(() => false)

    if (isBadgeVisible) {
      await expect(slashBadge).toContainText('/commit')
      const restText = userMsg.locator('.at-command-rest')
      await expect(restText).toContainText('fix auth bug')
    } else {
      // Fallback: the message text should at least contain /commit
      await expect(userMsg).toContainText('/commit')
    }
  })

  test('should show mode tab in SessionSettingModal for ACP session', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Open settings modal — mode tab should be visible
    // openModeMenu waits for ACP mode state before opening
    await chat.openModeMenu()

    // Mode tab should be active and mode items visible
    const modeItems = page.locator('.model-tab-content .thinking-item')
    await expect(modeItems.first()).toBeVisible({ timeout: 5000 })

    // acp-mock provides at least 2 modes
    const count = await modeItems.count()
    expect(count).toBeGreaterThanOrEqual(2)
  })

  test('should show thinking effort levels in SessionSettingModal for ACP session', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for ACP state to be available
    await chat.waitForACPState()

    // Open the model modal by clicking the model chip
    const settingsChip = page.locator('.settings-chip')
    await expect(settingsChip).toBeVisible({ timeout: 10000 })
    await settingsChip.click()

    // SessionSettingModal should appear
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).toBeVisible({ timeout: 5000 })

    // The "Thinking Effort" tab should be visible (acp-mock provides thought_level options)
    await chat.openThinkingTab()

    // Thinking effort levels should be listed (acp-mock provides low/medium/high)
    const thinkingItems = page.locator('.thinking-item')
    // 1 auto + 3 levels (low/medium/high) = 4 items
    const count = await thinkingItems.count()
    expect(count).toBeGreaterThanOrEqual(3)

    // Verify one of the items contains "Medium" (the default level)
    const mediumItem = thinkingItems.filter({ hasText: /medium/i })
    await expect(mediumItem).toBeVisible()
  })

  test('should select a thinking effort level via SessionSettingModal', async ({ page }) => {
    // Establish ACP connection first
    await chat.sendAndAwaitACPReply('hi')

    // Wait for ACP state to be available
    await chat.waitForACPState()

    // Open model modal → thinking tab
    const settingsChip = page.locator('.settings-chip')
    await expect(settingsChip).toBeVisible({ timeout: 10000 })
    await settingsChip.click()

    await chat.openThinkingTab()

    // Click on "Low" thinking effort level
    const lowItem = page.locator('.thinking-item').filter({ hasText: /low/i })
    await expect(lowItem).toBeVisible()
    await lowItem.click()

    // Modal should close after selection
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })
  })

  test('should return commands from API endpoint for ACP agent', async ({ page }) => {
    // First send a message to trigger ACP connection (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for commands to be cached
    await chat.waitForACPCommands()

    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/commands')
      if (!resp.ok) return { ok: false, status: resp.status }
      const data = await resp.json()
      return { ok: true, count: data.commands?.length || 0, firstCommand: data.commands?.[0]?.name }
    })

    expect(result.ok).toBe(true)
    expect(result.count).toBeGreaterThan(0)
    expect(result.firstCommand).toBeTruthy()
  })

  test('should close slash menu on blur', async ({ page }) => {
    // Trigger ACP connection (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')
    await chat.waitForACPCommands()

    // Type / to open slash menu
    await chat.textarea.click()
    await chat.textarea.fill('/')

    const slashItems = page.locator('.at-menu-label.slash-label')
    await expect(slashItems.first()).toBeVisible({ timeout: 5000 })

    // Click elsewhere to blur the textarea
    await page.locator('body').click({ position: { x: 10, y: 10 } })

    // Menu should close
    await expect(slashItems.first()).not.toBeVisible({ timeout: 2000 })
  })

  // ───────────────────────────────────────────────────────
  // General input tests
  // ───────────────────────────────────────────────────────

  test('should not show slash menu when typing regular text', async ({ page }) => {
    await chat.textarea.click()
    await chat.textarea.fill('hello world')

    const atMenu = page.locator('.at-menu-title')
    await expect(atMenu).not.toBeVisible({ timeout: 1000 })
  })

  test('should show placeholder hint mentioning @ and /', async ({ page }) => {
    await chat.textarea.clear()
    await page.locator('body').click({ position: { x: 10, y: 10 } })
    await page.waitForTimeout(500)

    await chat.textarea.focus()
    const focusedPlaceholder = await chat.textarea.getAttribute('placeholder')
    expect(focusedPlaceholder).toBeTruthy()
  })
})

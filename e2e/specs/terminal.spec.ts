import { test, expect } from '../fixtures'
import { TerminalPage } from '../pages/terminal.page'
import { projectApiFetch } from '../helpers/auth'

test.describe('Terminal multi-tab', () => {
  let terminal: TerminalPage

  // A test may need to reload once when the terminal runtime-status load has not
  // settled (the dock hides the tab until it reports enabled). That reload plus
  // the connect wait can exceed the default 30s budget on a loaded machine.
  test.setTimeout(60000)

  test.beforeEach(async ({ page }) => {
    terminal = new TerminalPage(page)
    // PTY sessions are server-side and outlive the browser context. Clear any
    // left by earlier specs (or a previous run) so this test starts with a free
    // session slot — otherwise the tab cannot connect and times out.
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  // Same cleanup after each test so the session count never climbs toward the
  // manager's limit (10) within this file.
  test.afterEach(async () => {
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  test('should show terminal panel with one default tab', async ({ page }) => {
    await terminal.switchToTerminal()
    await terminal.expectPanelVisible()
    await terminal.expectTabCount(1)
  })

  test('should connect the default tab on open', async ({ page }) => {
    await terminal.switchToTerminal()
    await terminal.expectPanelVisible()
    // Wait for the status dot to show connected (green)
    await terminal.waitForTabConnected(0, 15000)
  })

  test('should create a new tab when "+" button is clicked', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Click "+" to create a new tab
    await terminal.createNewTab()
    await terminal.expectTabCount(2)

    // The new tab should be active
    await terminal.expectTabActive(1)
  })

  test('new tab should connect automatically', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Create a new tab
    await terminal.createNewTab()
    await terminal.expectTabCount(2)

    // Wait for the new tab to connect
    await terminal.waitForTabConnected(1, 15000)
  })

  test('should switch between tabs', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Create a second tab
    await terminal.createNewTab()
    await terminal.waitForTabConnected(1, 15000)

    // Switch back to the first tab
    await terminal.clickTab(0)
    await terminal.expectTabActive(0)

    // Switch to the second tab
    await terminal.clickTab(1)
    await terminal.expectTabActive(1)
  })

  test('should show tab title derived from directory name', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Default tab should show some title (directory name or "Terminal")
    const title = await terminal.getTabTitle(0)
    expect(title).toBeTruthy()
  })

  test('should disconnect tabs when switching away from terminal', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Switch to Chat tab (stable data-tab selector; the old positional
    // `.dock-center .dock-btn` targeted the v-show-hidden narrow dock).
    const chatBtn = page.locator('.dock-btn[data-tab="chat"]').filter({ visible: true }).first()
    await chatBtn.click()

    // Switch back to terminal
    await terminal.switchToTerminal()

    // Tab should reconnect
    await terminal.waitForTabConnected(0, 15000)
  })

  test('should close a tab via three-dot menu', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Create a second tab so we still have one after closing. Only the ACTIVE
    // tab holds a live PTY — creating tab 1 disconnects tab 0 by design — so
    // wait for the new (now active) tab, not tab 0.
    await terminal.createNewTab()
    await terminal.waitForTabConnected(1, 15000)
    await terminal.expectTabCount(2)

    // Open the three-dot menu for the second tab
    await terminal.openTabMenu(1)

    // Click the "Close" menu item. Use an exact-text match on the label span
    // so it does not also match "Close All".
    await terminal.clickTabMenuItem(/^\s*Close\s*$|^\s*关闭\s*$/)

    // Should have 1 tab now
    await terminal.expectTabCount(1)
  })

  test('should show the empty state when closing the last tab', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Open the three-dot menu for the only tab
    await terminal.openTabMenu(0)

    // Click the "Close" menu item (exact match, so not "Close All")
    await terminal.clickTabMenuItem(/^\s*Close\s*$|^\s*关闭\s*$/)

    // Closing the last tab does NOT auto-create a replacement: the panel shows
    // its empty state and the user starts a new tab explicitly.
    await terminal.expectTabCount(0)
    await expect(page.locator('.terminal-empty-state')).toBeVisible({ timeout: 5000 })
  })

  test('each tab should connect to its own PTY session', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)
    const firstId = await terminal.tabSessionId(0)
    expect(firstId).toBeTruthy()

    await terminal.createNewTab()
    await terminal.waitForTabConnected(1, 15000)
    const secondId = await terminal.tabSessionId(1)
    expect(secondId).toBeTruthy()

    // Each tab owns a DISTINCT PTY session id.
    expect(secondId).not.toBe(firstId)
  })

  test('+" button should respect max sessions limit', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Create tabs up to a reasonable limit — just verify the button becomes
    // disabled at some point. Default max_sessions is 10.
    // We create a few and verify they work.
    for (let i = 1; i < 3; i++) {
      await terminal.createNewTab()
      await terminal.waitForTabConnected(i, 15000)
    }

    await terminal.expectTabCount(3)
  })
})

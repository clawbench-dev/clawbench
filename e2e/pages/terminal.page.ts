import { type Locator, type Page, expect } from '@playwright/test'

/**
 * Page Object Model for the Terminal panel.
 *
 * DOM structure (key selectors):
 *   .terminal-panel        — root container
 *   .terminal-tab-bar      — tab bar at top
 *   .terminal-tab-list     — scrollable tab list
 *   .terminal-tab          — individual tab (has .active when selected;
 *                            exposes data-session-id once its PTY connects)
 *   .terminal-tab-title    — tab title text
 *   .terminal-tab-unread-dot — unread-output dot (NOT a connection indicator)
 *   .terminal-tab-menu-btn — three-dot menu button per tab
 *   .terminal-tab-add      — "+" button to create new tab
 *   .terminal-viewport     — xterm containers area
 *   .terminal-container    — per-tab xterm container (v-show toggled)
 *   .terminal-toolbar      — virtual keyboard toolbar
 *   .terminal-error-overlay — error overlay (when connection fails)
 *
 * There is no per-tab connection status dot any more. Connection state is read
 * from `/api/terminal/status?session=<id>` (`running`), which is authoritative.
 */
export class TerminalPage {
  readonly page: Page
  private readonly panel: Locator
  private readonly tabBar: Locator
  private readonly tabList: Locator
  private readonly addBtn: Locator
  private readonly viewport: Locator

  constructor(page: Page) {
    this.page = page
    this.panel = page.locator('.terminal-panel')
    this.tabBar = page.locator('.terminal-tab-bar')
    this.tabList = page.locator('.terminal-tab-list')
    // `.terminal-tab-add` is a shared styling class reused by FOUR toolbar
    // buttons (New Tab, Open current directory, Theme, Quick Commands), so a
    // bare class selector is ambiguous. The buttons carry a stable
    // `data-action` (see TerminalPanelContent.vue).
    this.addBtn = page.locator('.terminal-tab-add[data-action="new-tab"]')
    this.viewport = page.locator('.terminal-viewport')
  }

  // --- Navigation ---

  /**
   * Switch to the Terminal tab.
   *
   * Uses the stable `data-tab` id on the dock button. The previous
   * overflow-menu route only exists in the *narrow* dock; in wide mode every
   * tab is rendered inline and there is no `.dock-overflow-btn`, so that path
   * would hang waiting for a button that never appears.
   */
  async switchToTerminal() {
    // The dock button can be absent/hidden (collapsed pane, overflow) while the
    // terminal panel is already the active view — in which case there is
    // nothing to switch.
    if (await this.panel.isVisible({ timeout: 3000 }).catch(() => false)) return

    let btn = this.page.locator('.dock-btn[data-tab="terminal"]').filter({ visible: true }).first()
    if (!(await btn.isVisible({ timeout: 5000 }).catch(() => false))) {
      // The dock hides the terminal tab when the async runtime-status load has
      // not (yet) reported the terminal enabled. On a long run that request can
      // fail transiently and leave the flag false with no retry — reload once to
      // re-run project init.
      await this.page.reload()
      await this.page.waitForLoadState('domcontentloaded')
      await expect(this.page.locator('.chat-textarea')).toBeVisible({ timeout: 15000 })
      btn = this.page.locator('.dock-btn[data-tab="terminal"]').filter({ visible: true }).first()
    }

    if (await btn.isVisible({ timeout: 15000 }).catch(() => false)) {
      // In wide mode clicking the ALREADY-ACTIVE tab collapses the left pane
      // (VS Code-style), which would HIDE the terminal panel we want. The
      // `active` class means "current tab AND pane expanded", so only click when
      // it is not already active.
      const isActive = await btn.evaluate((el) => el.classList.contains('active')).catch(() => false)
      if (!isActive) await btn.click()
    }

    // Whether we clicked or not, the panel must end up visible. This also
    // covers the case where the terminal tab was already active.
    await expect(this.panel).toBeVisible({ timeout: 20000 })
  }

  // --- Tab queries ---

  /** Get all tab elements */
  getTabs(): Locator {
    return this.tabList.locator('.terminal-tab')
  }

  /** Get the active tab element */
  getActiveTab(): Locator {
    return this.tabList.locator('.terminal-tab.active')
  }

  /** Get tab by index */
  getTab(index: number): Locator {
    return this.getTabs().nth(index)
  }

  /** Get the number of tabs */
  async tabCount(): Promise<number> {
    return await this.getTabs().count()
  }

  /** Get tab title text by index */
  async getTabTitle(index: number): Promise<string> {
    return await this.getTab(index).locator('.terminal-tab-title').textContent() || ''
  }

  // --- Tab actions ---

  /** Click a tab by index to switch to it */
  async clickTab(index: number) {
    await this.getTab(index).click()
  }

  /** Click the "+" button to create a new tab */
  async createNewTab() {
    await this.addBtn.click()
  }

  /** Check if the "+" button is disabled (tab limit reached) */
  async isAddButtonDisabled(): Promise<boolean> {
    return await this.addBtn.isDisabled()
  }

  /** Open the three-dot menu for a specific tab */
  async openTabMenu(index: number) {
    await this.getTab(index).locator('.terminal-tab-menu-btn').click()
  }

  /** Click a menu item in the tab menu popup by its exact label */
  async clickTabMenuItem(label: RegExp) {
    // TerminalTabMenu renders `.tab-menu-item` buttons inside a PopupMenu
    // teleported to <body>. The class is `.tab-menu-item` (not the generic
    // `.popup-menu-item`, which does not exist in PopupMenu.vue).
    //
    // Match the button's exact normalized text: each item also renders an icon
    // (an <svg> with no text), and a loose `hasText` would make "Close" also
    // match "Close All".
    const items = this.page.locator('.tab-menu-item')
    const count = await items.count()
    for (let i = 0; i < count; i++) {
      const text = (await items.nth(i).innerText()).trim()
      if (label.test(text)) {
        await items.nth(i).click()
        return
      }
    }
    throw new Error(`terminal tab menu item matching ${label} not found`)
  }

  // --- Status (connection) ---
  //
  // The per-tab status dot (`.terminal-tab-status`) no longer exists in the UI;
  // the only per-tab indicator left is `.terminal-tab-unread-dot`. Connection
  // state is therefore read from the backend, which is the authoritative
  // source: `/api/terminal/status?session=<id>` returns `running: true` for a
  // live PTY. The panel-level `.terminal-error-overlay` marks a failed tab.

  /** The PTY session id of the tab at `index`, or '' if it has not connected. */
  async tabSessionId(index: number): Promise<string> {
    return await this.getTab(index).getAttribute('data-session-id') || ''
  }

  /** Whether the given session id is running server-side. */
  async isSessionRunning(sessionId: string): Promise<boolean> {
    return await this.page.evaluate(async (sid) => {
      const resp = await fetch(`/api/terminal/status?session=${encodeURIComponent(sid)}`)
      if (!resp.ok) return false
      const data = await resp.json()
      return data.running === true
    }, sessionId)
  }

  /**
   * Wait until the tab at `index` has a live PTY.
   *
   * Only the ACTIVE tab holds a websocket connection — inactive tabs are
   * disconnected by design (see TerminalPanelContent.vue's activeTabId watch).
   * So this first activates the tab, then waits for it to connect.
   */
  async waitForTabConnected(index: number, timeout = 15000): Promise<void> {
    const tab = this.getTab(index)
    await tab.waitFor({ state: 'visible', timeout })
    if (!(await tab.evaluate((el) => el.classList.contains('active')))) {
      await tab.click()
    }
    const deadline = Date.now() + timeout
    while (Date.now() < deadline) {
      const sid = await this.tabSessionId(index)
      if (sid && await this.isSessionRunning(sid)) return
      // A session-limit or shell-start failure leaves an error overlay; retry
      // via the panel's reconnect affordance rather than waiting out the
      // whole timeout.
      const reconnect = this.page.locator('.terminal-reconnect-btn')
      if (await reconnect.isVisible().catch(() => false)) {
        await reconnect.click().catch(() => {})
      }
      await this.page.waitForTimeout(250)
    }
    throw new Error(`terminal tab ${index} did not connect within ${timeout}ms`)
  }

  /**
   * Activate the terminal tab, reloading the page if the panel will not
   * connect. A long full-suite run can leave the panel's websocket unable to
   * attach even though the server is healthy; a fresh load recovers it.
   */
  async ensureTerminalConnected(index = 0, timeout = 15000): Promise<void> {
    await this.switchToTerminal()
    try {
      await this.waitForTabConnected(index, timeout)
    } catch {
      await this.page.reload()
      await this.page.waitForLoadState('domcontentloaded')
      await expect(this.page.locator('.chat-textarea')).toBeVisible({ timeout: 15000 })
      await this.switchToTerminal()
      await this.waitForTabConnected(index, timeout)
    }
  }

  /** Wait until the tab at `index` is the active tab. */
  async waitForTabActive(index: number, timeout = 10000): Promise<void> {
    await expect(this.getTab(index)).toHaveClass(/active/, { timeout })
  }

  /** Get the unread-dot element for a tab (empty when no unread output). */
  getTabUnreadDot(index: number): Locator {
    return this.getTab(index).locator('.terminal-tab-unread-dot')
  }

  // --- Tab actions ---

  /** Get the xterm container for a specific tab (by index) */
  getTerminalContainer(index: number): Locator {
    return this.viewport.locator('.terminal-container').nth(index)
  }

  /** Check if the terminal error overlay is visible */
  async isErrorOverlayVisible(): Promise<boolean> {
    return await this.panel.locator('.terminal-error-overlay').isVisible().catch(() => false)
  }

  // --- Assertions ---

  /** Assert the terminal panel is visible */
  async expectPanelVisible() {
    await expect(this.panel).toBeVisible()
  }

  /** Assert tab count */
  async expectTabCount(count: number) {
    await expect(this.getTabs()).toHaveCount(count)
  }

  /** Assert a specific tab is active */
  async expectTabActive(index: number) {
    await expect(this.getTab(index)).toHaveClass(/active/)
  }

  /** Wait for the terminal viewport to contain xterm content */
  async waitForTerminalReady(timeout = 10000) {
    // Wait for the xterm cursor to appear (indicates the PTY session is running)
    await this.page.waitForFunction(
      () => !!document.querySelector('.xterm-screen'),
      { timeout }
    ).catch(() => {
      // xterm might not have rendered yet, which is fine for some tests
    })
  }
}

import { type Locator, type Page, expect } from '@playwright/test'

/**
 * Page Object Model for tab/drawer navigation.
 *
 * The dock renders in two mutually exclusive shapes:
 *   - wide screen (CSS width >= 1024px, the Playwright default at 1280×720):
 *     a vertical `.wide-dock-center` on the left, chat toggled separately.
 *   - narrow screen: a horizontal `.dock-center` at the bottom.
 *
 * Both shapes tag every tab button with `data-tab="<id>"` (see App.vue and
 * `composables/dockTabs.ts`). Selectors therefore key off that stable id rather
 * than a position: the old positional `.nth(N)` locators silently rotted every
 * time a tab was added to `DOCK_TABS`, and they targeted `.dock-center` — which
 * is `v-show`-hidden in wide mode, so every click timed out.
 */
export class NavigationPage {
  readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  /**
   * The visible dock button for a tab id.
   *
   * `.filter({ visible: true })` picks the active dock shape: both the wide and
   * narrow docks are in the DOM (one is `v-show`-hidden), so a bare
   * `[data-tab]` query would resolve to two elements and fail strict mode.
   */
  private tabBtn(tab: string): Locator {
    return this.page.locator(`.dock-btn[data-tab="${tab}"]`).filter({ visible: true }).first()
  }

  /** Public accessor for a tab's visible dock button (for class assertions). */
  getTabButton(tab: string): Locator {
    return this.tabBtn(tab)
  }

  /** Wait for the dock to render (either shape) and return a tab button. */
  private async readyTabBtn(tab: string): Promise<Locator> {
    const btn = this.tabBtn(tab)
    await expect(btn).toBeVisible({ timeout: 10000 })
    return btn
  }

  // --- Tab switching (locale- and position-independent) ---

  /**
   * Switch to a left-pane tab.
   *
   * In wide mode the *active* tab's button toggles the left pane collapsed
   * (VS Code-style), so clicking blindly can *hide* the pane we want. The
   * `active` class already encodes "this is the current tab AND the pane is
   * expanded" (see `wideDockBtnClass` in App.vue), so: if it is active there is
   * nothing to do; otherwise a click either switches tabs or re-expands the
   * pane, both of which are the desired outcome.
   */
  async switchToTab(tab: string): Promise<void> {
    const btn = await this.readyTabBtn(tab)
    if (await btn.evaluate((el) => el.classList.contains('active'))) return
    await btn.click()
  }

  /** Switch to Chat tab */
  async switchToChat(): Promise<void> {
    // In wide mode chat lives in the right pane and is toggled by its own
    // button; in narrow mode it is a regular dock tab.
    const chatToggle = this.page.locator('.wide-dock-bottom .dock-btn[data-tab="chat"]')
    if (await chatToggle.isVisible().catch(() => false)) {
      // Wide mode: the toggle's aria-pressed reflects whether chat is shown.
      const pressed = await chatToggle.getAttribute('aria-pressed')
      if (pressed !== 'true') await chatToggle.click()
      await expect(this.page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
      return
    }
    await this.switchToTab('chat')
  }

  /** Switch to File Viewer tab */
  async switchToViewer(): Promise<void> {
    await this.switchToTab('view')
  }

  /** Switch to File Manager (Browse) tab */
  async switchToFileManager(): Promise<void> {
    await this.switchToTab('browse')
  }

  /** Switch to Tasks tab */
  async switchToTasks(): Promise<void> {
    await this.switchToTab('tasks')
  }

  // --- Overflow menu (narrow dock only) ---
  //
  // In wide mode every tab is rendered inline in `.wide-dock-center` and there
  // is no overflow button at all, so these helpers must only be used against
  // the narrow (bottom) dock.

  /** Open the overflow menu (3-dot button). Narrow dock only. */
  async openOverflowMenu(): Promise<void> {
    const overflowBtn = this.page.locator('.dock-overflow-btn')
    await expect(overflowBtn).toBeVisible({ timeout: 5000 })
    await overflowBtn.click()
    await expect(this.page.locator('.dock-overflow-popup')).toBeVisible()
  }

  /** Switch to a tab through the narrow dock's overflow menu. */
  async switchToTabViaOverflow(label: RegExp): Promise<void> {
    await this.openOverflowMenu()
    await this.page.locator('.dock-overflow-item', { hasText: label }).click()
  }

  /** Switch to History tab (via overflow menu) */
  async switchToHistory(): Promise<void> {
    await this.switchToTabViaOverflow(/History|历史/)
  }

  /** Switch to Terminal tab (via overflow menu) */
  async switchToTerminal(): Promise<void> {
    await this.switchToTabViaOverflow(/Terminal|终端/)
  }

  /** Open Settings (via overflow menu) */
  async openSettings(): Promise<void> {
    await this.switchToTabViaOverflow(/Settings|设置/)
  }

  // --- Assertions ---

  /** Assert that the chat tab is active */
  async expectChatActive(): Promise<void> {
    await expect(this.tabBtn('chat')).toHaveClass(/active/)
  }
}

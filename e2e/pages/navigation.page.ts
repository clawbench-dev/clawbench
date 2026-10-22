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

  /**
   * Wait for the dock to finish laying out, then return the tab button.
   *
   * `setViewportSize` resolves when the resize has been *applied*, not when the
   * page has reflowed and the dock's overflow recomputation has settled. In that
   * window the buttons are already in the DOM but still zero-width (measured:
   * `.dock-center` width 0, all 11 buttons `getBoundingClientRect().width === 0`)
   * while the overflow button is momentarily rendered. `filter({ visible: true })`
   * treats a zero-size element as invisible, so the locator matches NOTHING —
   * and the subsequent `toBeVisible` then waits forever on a locator that can
   * never resolve, because by the time the button does have a size the filter has
   * already been re-evaluated against the transient state.
   *
   * Measured on WebKit: the transient lasts one frame after a wide→narrow resize,
   * so a spec that resizes and clicks immediately failed 1-in-4 runs while an
   * identical spec that happened to wait passed. That is a race in the test, not
   * a product defect (a real user never clicks within the same frame).
   *
   * Waiting on the dock's settled width — not a fixed timeout — is the condition
   * that is true exactly when the transient is over, so it cannot flake.
   */
  /**
   * Wait for the dock shape to match the current viewport.
   *
   * `setViewportSize` resolves when the resize has been *applied*, not when the
   * page has reflowed and `isWideScreen` has been recomputed. In that window the
   * wide dock is still the displayed one, so `filter({ visible: true })` latches
   * onto ITS button; a moment later `v-show` hides that dock and the click fails
   * with "element is not visible" — measured on WebKit in CI, 1-in-4 runs, for
   * both `notifications.spec.ts` and `team-panel.spec.ts`.
   *
   * The precise settled condition is "the dock the layout SHOULD be showing is
   * the one actually rendered": compare the live viewport width against the
   * app's own wide-screen threshold (960, see useWideScreenLayout.ts) and
   * require exactly that shape to be displayed.
   */
  async waitForLayoutSettled(): Promise<void> {
    await expect
      .poll(
        () =>
          this.page.evaluate(() => {
            const wide = document.querySelector('.wide-dock') as HTMLElement | null
            const narrow = document.querySelector('.bottom-dock-wrapper') as HTMLElement | null
            if (!wide || !narrow) return false
            const shown = (el: HTMLElement) =>
              getComputedStyle(el).display !== 'none' && el.getBoundingClientRect().width > 0
            const wantWide = window.innerWidth >= 960
            return wantWide ? shown(wide) && !shown(narrow) : shown(narrow) && !shown(wide)
          }),
        { timeout: 10000, message: 'the dock matching the viewport should be the one rendered' },
      )
      .toBe(true)
  }

  /** Wait for the dock to render (either shape) and return a tab button. */
  private async readyTabBtn(tab: string): Promise<Locator> {
    // Settle BEFORE resolving the locator: `filter({ visible: true })` latches
    // onto whatever is visible at evaluation time, so resolving it mid-reflow
    // is what produced the unresolvable locator described above.
    await this.waitForLayoutSettled()
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
    // Settle FIRST: `isVisible()` on the wide toggle is a snapshot, and right
    // after a wide→narrow resize the wide dock is still the displayed one — so
    // this method used to take the wide branch and click a toggle that was about
    // to be hidden, leaving chat mounted but NOT the active narrow tab (its
    // content present yet `display:none`, which is exactly the "team-chip
    // resolved but hidden" timeout seen on WebKit in CI).
    await this.waitForLayoutSettled()
    const wide = await this.page.evaluate(() => window.innerWidth >= 960)
    if (wide) {
      // In wide mode chat lives in the right pane and is toggled by its own
      // button; aria-pressed reflects whether chat is shown.
      const chatToggle = this.page.locator('.wide-dock-bottom .dock-btn[data-tab="chat"]')
      const pressed = await chatToggle.getAttribute('aria-pressed')
      if (pressed !== 'true') await chatToggle.click()
      await expect(this.page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
      return
    }
    await this.switchToTab('chat')
    // Narrow mode: the chat panel is a drawer page, mounted only while its tab
    // is active. Wait for it to actually be the visible one, so callers that
    // assert on chat content cannot race the drawer transition.
    await expect(this.page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
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

  /** Switch to Notifications tab */
  async switchToNotifications(): Promise<void> {
    await this.switchToTab('notifications')
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

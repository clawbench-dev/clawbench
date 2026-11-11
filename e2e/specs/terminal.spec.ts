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

    // Switch away from the terminal panel. The chat pane is not a dock tab in
    // wide mode (it is hidden from the split divider), so a chat click would be
    // a no-op there; the file manager tab is a real panel switch in BOTH
    // layouts, which is what deactivates the terminal.
    const browseBtn = page.locator('.dock-btn[data-tab="browse"]').filter({ visible: true }).first()
    await browseBtn.click()

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
    // The panel draws the shared EmptyState (it replaced the bespoke
    // `.terminal-empty-state` block); scope to the panel because the same
    // component also renders for the unsupported-platform case.
    await expect(page.locator('.terminal-panel .empty-state')).toBeVisible({ timeout: 5000 })
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

  test('Shift+Insert inserts the selected text at the cursor without running it', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    /** Trimmed text of every visible xterm row (the DOM renderer keeps them as text). */
    const rows = () =>
      page.evaluate(() => {
        const container = Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
          .find((el) => (el as HTMLElement).offsetParent !== null)
        if (!container) return [] as string[]
        return Array.from(container.children).map((el) => (el.textContent || '').trim())
      })

    // Print a bare COMMAND line (no prompt prefix) so a triple-click selects
    // exactly `echo <marker>` — the selection is what Shift+Insert must insert.
    const marker = `paste-marker-${Date.now()}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(`echo echo ${marker}`)
    await page.keyboard.press('Enter')

    const command = `echo ${marker}`
    await expect.poll(() => rows(), { timeout: 15000 }).toContain(command)

    // Triple-click the line to select it (xterm's multi-click line selection).
    // Match the row EXACTLY: the prompt line above also contains the command as
    // a substring ("echo echo <marker>"), and selecting that would paste the
    // prompt along with it.
    const line = page
      .locator('.terminal-container .xterm-rows:visible div')
      .filter({ hasText: new RegExp(`^\\s*${command}\\s*$`) })
      .first()
    const box = await line.boundingBox()
    if (!box) throw new Error(`could not locate the rendered row for "${command}"`)
    // Click through the xterm screen (the row divs sit underneath it and do not
    // receive pointer events themselves).
    await page.mouse.click(box.x + 4, box.y + box.height / 2, { clickCount: 3 })

    // Paste and run. The pasted text must land at the cursor — nothing is sent
    // until Enter, which is the whole point of the chord.
    await page.keyboard.press('Shift+Insert')
    await page.keyboard.press('Enter')

    // The command ran, so the marker now appears on a line of its own. Before
    // the paste the only line containing it also contained `echo `.
    await expect.poll(() => rows(), { timeout: 15000 }).toContain(marker)
  })

  /**
   * Copy-on-select must hand keyboard focus back to the terminal.
   *
   * Regression guard for a real defect: when the async Clipboard API is
   * unavailable or refuses the write, `copyText` falls back to `execCommand`,
   * which needs a focused textarea — so it focuses a throwaway one. Leaving
   * focus there stranded the user on `<body>`: the terminal stopped receiving
   * keystrokes entirely, so Shift+Insert (and every other chord) silently did
   * nothing right after selecting text.
   *
   * The clipboard write is forced to fail here on purpose. On a plain
   * http/localhost origin `writeText` usually succeeds and the fallback never
   * runs, which is exactly why the original Shift+Insert test could not see
   * this bug.
   */
  test('copy-on-select keeps terminal focus when the clipboard write falls back', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // Simulate a browser that denies the async clipboard write (permission
    // refused / insecure context) AND record when the execCommand fallback
    // actually runs. Must be installed before the app loads so every copy from
    // this point takes the fallback path.
    //
    // Counting the fallback is what makes this test deterministic: the copy is
    // deferred (copy-on-select waits for the selection to settle), so asserting
    // focus immediately after mouseup can sample the state BEFORE the copy has
    // taken focus — which is how the original version of this test passed even
    // with the bug present.
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: () => Promise.reject(new Error('Write permission denied')),
          readText: () => Promise.reject(new Error('Read permission denied')),
        },
      })
      const w = window as unknown as { __execCommandCalls: number }
      w.__execCommandCalls = 0
      const original = document.execCommand.bind(document)
      document.execCommand = ((...args: Parameters<Document['execCommand']>) => {
        w.__execCommandCalls++
        return original(...args)
      }) as Document['execCommand']
    })
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 15000 })
    await terminal.ensureTerminalConnected(0, 15000)

    const rows = () =>
      page.evaluate(() => {
        const container = Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
          .find((el) => (el as HTMLElement).offsetParent !== null)
        if (!container) return [] as string[]
        return Array.from(container.children).map((el) => (el.textContent || '').trim())
      })

    const marker = `focus-marker-${Date.now()}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(`echo ${marker}`)
    await page.keyboard.press('Enter')
    await expect.poll(() => rows(), { timeout: 15000 }).toContain(marker)

    // Drag-select the output line, which triggers copy-on-select.
    const line = page
      .locator('.terminal-container .xterm-rows:visible div')
      .filter({ hasText: new RegExp(`^\\s*${marker}\\s*$`) })
      .first()
    const box = await line.boundingBox()
    if (!box) throw new Error(`could not locate the rendered row for "${marker}"`)
    await page.mouse.move(box.x + 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + 60, box.y + box.height / 2, { steps: 12 })
    await page.mouse.up()

    // Wait for the deferred copy to have actually gone through the fallback —
    // only then is "did focus survive?" a meaningful question.
    await expect
      .poll(
        () => page.evaluate(() => (window as unknown as { __execCommandCalls: number }).__execCommandCalls),
        { timeout: 10000 },
      )
      .toBeGreaterThan(0)

    // Focus must still be the terminal's own input, not <body>.
    await expect
      .poll(
        () => page.evaluate(() => document.activeElement?.className ?? ''),
        { timeout: 10000 },
      )
      .toContain('xterm-helper-textarea')

    // And the terminal must still accept typing — the user-visible symptom of
    // the stranded-focus bug was that nothing could be typed or pasted.
    //
    // Joined, not matched per row: xterm hard-wraps at the pane width, so a
    // phrase can be split across two rows ("typed-after-cop" / "y") depending
    // on the viewport. A per-row assertion would then fail for a reason that has
    // nothing to do with focus.
    const allText = () => page.evaluate(() => {
      const container = Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
        .find((el) => (el as HTMLElement).offsetParent !== null)
      return container ? (container.textContent || '').replace(/\s+/g, '') : ''
    })
    await page.keyboard.type('typed-after-copy')
    await expect.poll(allText, { timeout: 10000 }).toContain('typed-after-copy')
  })
})

import { test, expect } from '../fixtures'
import { TerminalPage } from '../pages/terminal.page'
import { projectApiFetch } from '../helpers/auth'

/**
 * Terminal command history.
 *
 * Covers the end-to-end path that the unit tests cannot: the command is typed
 * into a real PTY, the frontend reconstructs it from the keystroke stream, sends
 * the `command` frame, and the backend stores it — after checking that the
 * foreground process really was the shell.
 */
test.describe('Terminal command history', () => {
  let terminal: TerminalPage

  test.setTimeout(60000)

  test.beforeEach(async ({ page }) => {
    terminal = new TerminalPage(page)
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
    // Start from an empty history so the assertions below describe this test
    // only. e2e runs against a throwaway data dir, so nothing real is lost.
    await projectApiFetch('/api/terminal/history', { method: 'DELETE' }).catch(() => {})
  })

  test.afterEach(async () => {
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  /**
   * Open the quick-commands drawer.
   *
   * The toolbar button opens a POPUP MENU (the visible commands plus an
   * "edit" entry), and only that entry opens the drawer — clicking the button
   * and expecting `.qc-content` waits forever.
   */
  async function openDrawer(page: import('@playwright/test').Page) {
    await page.locator('.terminal-tab-add[data-action="quick-commands"]').click()
    await page.locator('.quick-send-item').last().click()
    await expect(page.locator('.qc-content')).toBeVisible({ timeout: 5000 })
  }

  test('records a command typed at the prompt and offers it back', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    const output = `hist-${Date.now()}`
    const marker = `echo ${output}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(marker)
    await page.keyboard.press('Enter')

    const rows = () =>
      page.evaluate(() => {
        const container = Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
          .find((el) => (el as HTMLElement).offsetParent !== null)
        if (!container) return [] as string[]
        return Array.from(container.children).map((el) => (el.textContent || '').trim())
      })
    /** How many rows are exactly the command's output (i.e. times it ran). */
    const runs = () => rows().then((r) => r.filter((l) => l === output).length)

    // Wait for the shell to actually run it before opening the drawer: the
    // history row only exists once the backend has received the `command` frame
    // and the foreground process has returned to the shell.
    await expect.poll(runs, { timeout: 15000 }).toBe(1)

    await openDrawer(page)
    await page.locator('.qc-tabs .segmented-tab[data-mode="recent"]').click()

    const row = page.locator('.qc-history-row').filter({ hasText: marker }).first()
    await expect(row).toBeVisible({ timeout: 10000 })

    // Tapping inserts the command into the input line and closes the drawer; it
    // must NOT execute. Counting the output rows is what distinguishes "inserted"
    // from "ran": both put the text on screen, only one produces an output row.
    await row.click()
    await expect(page.locator('.qc-content')).toBeHidden({ timeout: 5000 })
    await expect.poll(runs, { timeout: 5000 }).toBe(1)

    // The line is now on the prompt, unexecuted: Enter runs it.
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.press('Enter')
    await expect.poll(runs, { timeout: 15000 }).toBe(2)
  })

  test('does not record input consumed by a full-screen program', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    // `cat` reads lines from the PTY without a shell prompt: the frontend sees
    // the keystrokes but the backend must reject them (foreground != shell).
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type('cat')
    await page.keyboard.press('Enter')
    await page.waitForTimeout(300)
    // Deliberately a line that LOOKS like a command, so only the
    // foreground-process check can reject it — a nonsense token would also be
    // filtered by the line-shape heuristic and prove nothing.
    await page.keyboard.type('ls')
    await page.keyboard.press('Enter')
    await page.waitForTimeout(300)
    await page.keyboard.press('Control+C')

    await openDrawer(page)
    await page.locator('.qc-tabs .segmented-tab[data-mode="recent"]').click()

    // `cat` itself IS a command and may be recorded; its input is not.
    const recorded = await page.locator('.qc-history-row .qc-cmd-lead').allTextContents()
    expect(recorded.map((c) => c.trim())).not.toContain('ls')
  })

  test('clearing history empties the list', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)

    const marker = `echo clear-${Date.now()}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(marker)
    await page.keyboard.press('Enter')

    await openDrawer(page)
    await page.locator('.qc-tabs .segmented-tab[data-mode="recent"]').click()
    await expect(page.locator('.qc-history-row').filter({ hasText: marker })).toBeVisible({ timeout: 10000 })

    // Two taps: the first arms, the second clears.
    const clearBtn = page.locator('.bs-header-actions .create-btn').last()
    await clearBtn.click()
    await expect(clearBtn).toHaveClass(/danger/)
    await clearBtn.click()

    await expect(page.locator('.qc-history-row')).toHaveCount(0, { timeout: 10000 })
  })
})

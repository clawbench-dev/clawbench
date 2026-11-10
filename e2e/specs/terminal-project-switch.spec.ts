import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { TerminalPage } from '../pages/terminal.page'
import { apiFetch, projectApiFetch } from '../helpers/auth'
import { mkdtempSync, rmSync } from 'fs'
import { basename, join } from 'path'
import { tmpdir } from 'os'

/**
 * Switching projects must NOT kill the terminal, and neither must a page
 * reload: the shells keep running on the server, and coming back (or reloading)
 * reattaches to them.
 *
 * This is a cross-component flow that unit tests cannot reach — App.vue keys the
 * whole tree by project path, so a switch destroys the terminal panel (and with
 * it the in-memory copy of the backend session ids; the copy that survives is
 * the record in browser storage). The assertions are therefore on identity and
 * on the screen:
 *
 *   - the tab's `data-session-id` after returning is the SAME id as before,
 *   - that id is still `running` server-side,
 *   - output printed before the switch is on screen again (the server replayed
 *     the ring buffer the shell kept filling while the user was away).
 *
 * A "fresh shell that happens to look similar" would fail all three.
 */
test.describe.serial('Terminal tabs survive a project switch', () => {
  test.describe.configure({ timeout: 150_000 })

  const repoRoot = process.cwd()
  let projectB = ''

  // PTY sessions are server-side and outlive the browser context, so clear them
  // on both sides of every test — otherwise this spec leaves a busy shell behind
  // that counts against the manager's session limit for later specs.
  test.beforeEach(async () => {
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })
  test.afterEach(async () => {
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  test.beforeAll(async () => {
    projectB = mkdtempSync(join(tmpdir(), 'clawbench-terminal-switch-'))
    // The header dropdown is driven by the MRU list; without this the second
    // project simply is not there to click.
    const resp = await apiFetch('/api/recent-projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: projectB }),
    })
    expect(resp.ok, 'could not add the second project to the recent list').toBeTruthy()
  })

  test.afterAll(async () => {
    // The switch writes the server-side default project, which is GLOBAL: every
    // later spec's fresh browser context would resolve to this temp directory.
    await apiFetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: repoRoot }),
    }).catch(() => {})
    await apiFetch('/api/recent-projects', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: projectB }),
    }).catch(() => {})
    if (projectB) {
      try { rmSync(projectB, { recursive: true, force: true }) } catch { /* best effort */ }
    }
  })

  /** Pick a project from the header dropdown and wait for the switch to land. */
  async function switchProjectTo(page: Page, name: string) {
    await page.locator('.project-switch-btn').click()
    const row = page
      .locator('.app-menu .app-menu-item')
      // Exact match on the name: the two temp/repo names share a prefix.
      .filter({ has: page.getByText(name, { exact: true }) })
      .first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.click()
    await expect(page.locator('.project-switch-btn .project-name')).toHaveText(name, { timeout: 30000 })
  }

  /** Everything xterm has rendered (the DOM renderer keeps rows as text). */
  function terminalText(page: Page): Promise<string> {
    return page.evaluate(() =>
      Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
        .map((el) => el.textContent || '')
        .join('\n'),
    )
  }

  test('keeps the shell running across a project switch and a reload', async ({ page }) => {
    const terminal = new TerminalPage(page)
    await terminal.ensureTerminalConnected(0, 15000)

    const beforeId = await terminal.tabSessionId(0)
    expect(beforeId).toBeTruthy()

    // Print a unique marker, then keep the shell busy so the PTY is plainly
    // "running a command" across the switch rather than sitting at a prompt.
    const marker = `switch-marker-${Date.now()}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(`echo ${marker}`)
    await page.keyboard.press('Enter')
    await expect.poll(() => terminalText(page), { timeout: 15000 }).toContain(marker)
    await page.keyboard.type('sleep 300')
    await page.keyboard.press('Enter')

    // ── Leave the project (destroys the terminal component) ──
    await switchProjectTo(page, basename(projectB))

    // The shell must still be alive on the server — this is the whole point.
    await expect
      .poll(() => terminal.isSessionRunning(beforeId), { timeout: 15000 })
      .toBe(true)

    // ── Come back ──
    await switchProjectTo(page, basename(repoRoot))
    await terminal.switchToTerminal()
    await terminal.expectPanelVisible()
    await terminal.expectTabCount(1)

    // The marker can only be on screen again if the SAME PTY was reattached:
    // it lives in that session's replay ring buffer, and a freshly created
    // shell starts blank. Waiting for it (rather than for the id) is what makes
    // this a real assertion — the restored tab carries the id from the snapshot
    // before the socket even opens, so polling the id alone passes either way.
    await expect.poll(() => terminalText(page), { timeout: 25000 }).toContain(marker)

    expect(await terminal.tabSessionId(0)).toBe(beforeId)
    expect(await terminal.isSessionRunning(beforeId)).toBe(true)

    // ── Reload the page (no unmount happens here — the record must already be
    //    on disk, which is why it is written on every change) ──
    await page.reload()
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 20000 })
    await terminal.switchToTerminal()
    await terminal.expectPanelVisible()
    await terminal.expectTabCount(1)

    await expect.poll(() => terminalText(page), { timeout: 25000 }).toContain(marker)
    expect(await terminal.tabSessionId(0)).toBe(beforeId)
    expect(await terminal.isSessionRunning(beforeId)).toBe(true)
  })
})

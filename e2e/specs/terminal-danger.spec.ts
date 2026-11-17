import { test, expect } from '../fixtures'
import { TerminalPage } from '../pages/terminal.page'
import { projectApiFetch } from '../helpers/auth'

/**
 * Destructive-command confirmation.
 *
 * ⚠️ The e2e server's project root IS this repository (see helpers/server.ts
 * step 7a), so a terminal opened here starts in the working tree. That makes
 * "type a destructive command and confirm it" genuinely dangerous: if the guard
 * regresses, the test force-pushes the real repo.
 *
 * The only dangerous command used below is `git push --force`, and only after
 * the shell has been moved into a freshly created, empty git repository under
 * /tmp AND the move has been verified through the backend's live cwd probe. If
 * that verification fails the test aborts before anything is typed. The push
 * then fails harmlessly ("No configured push destination").
 *
 * Nothing that deletes files, formats a device or powers off the machine is
 * ever typed, confirmed or executed here.
 */
test.describe('Terminal destructive command confirmation', () => {
  let terminal: TerminalPage

  test.setTimeout(90000)

  test.beforeEach(async ({ page }) => {
    terminal = new TerminalPage(page)
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  test.afterEach(async () => {
    await projectApiFetch('/api/terminal/close', { method: 'POST' }).catch(() => {})
  })

  const dialog = (page: import('@playwright/test').Page) => page.locator('.danger-command')
  const cancelBtn = (page: import('@playwright/test').Page) => page.locator('[data-action="danger-cancel"]')
  const runBtn = (page: import('@playwright/test').Page) => page.locator('[data-action="danger-run"]')

  const rows = (page: import('@playwright/test').Page) =>
    page.evaluate(() => {
      const container = Array.from(document.querySelectorAll('.terminal-container .xterm-rows'))
        .find((el) => (el as HTMLElement).offsetParent !== null)
      if (!container) return [] as string[]
      return Array.from(container.children).map((el) => (el.textContent || '').trim())
    })

  /** Live cwd as reported by the backend's foreground-process probe. */
  async function liveCwd(sessionId: string): Promise<string> {
    const res = await projectApiFetch(`/api/terminal/status?session=${encodeURIComponent(sessionId)}`)
    const body = await res.json()
    return body.cwd || ''
  }

  /**
   * Move the shell into a throwaway git repo and PROVE it got there.
   *
   * The proof is the point: without it, a `cd` that silently failed would leave
   * the shell in the working tree, and the next command would run against the
   * real repository.
   */
  async function enterSandboxRepo(page: import('@playwright/test').Page): Promise<string> {
    // The session id is written when the backend's status message arrives; wait
    // for it rather than polling the cwd with an empty id.
    await expect.poll(() => terminal.tabSessionId(0), { timeout: 15000 }).not.toBe('')
    const sessionId = await terminal.tabSessionId(0)
    const dir = `/tmp/clawbench-e2e-danger-${Date.now()}`
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type(`mkdir -p ${dir} && cd ${dir} && git init -q`)
    await page.keyboard.press('Enter')

    await expect
      .poll(() => liveCwd(sessionId), { timeout: 15000 })
      .toBe(dir)
    return dir
  }

  test('cancelling keeps the line and does not execute it', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)
    await enterSandboxRepo(page)

    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type('git push --force')
    await page.keyboard.press('Enter')

    // The dialog names the exact line that is about to run.
    await expect(dialog(page)).toBeVisible({ timeout: 5000 })
    await expect(dialog(page)).toHaveText('git push --force')

    await cancelBtn(page).click()
    await expect(dialog(page)).toBeHidden({ timeout: 5000 })

    // Nothing ran. git speaks the user's locale, so the message is matched in
    // both languages.
    await page.waitForTimeout(300)
    expect((await rows(page)).join('\n')).not.toMatch(/No configured push destination|没有配置推送目标/)

    // The line is still on the prompt: pressing Enter asks again rather than
    // silently doing nothing or running it.
    await terminal.getTerminalContainer(0).click()
    await page.keyboard.press('Enter')
    await expect(dialog(page)).toBeVisible({ timeout: 5000 })
  })

  test('runs the command after confirmation', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)
    await enterSandboxRepo(page)

    await terminal.getTerminalContainer(0).click()
    await page.keyboard.type('git push --force')
    await page.keyboard.press('Enter')
    await expect(dialog(page)).toBeVisible({ timeout: 5000 })

    await runBtn(page).click()
    await expect(dialog(page)).toBeHidden({ timeout: 5000 })

    // It really ran — and failed, because the sandbox repo has no remote.
    // git speaks the user's locale, so the message is matched in both languages.
    await expect
      .poll(async () => (await rows(page)).join('\n'), { timeout: 15000 })
      .toMatch(/No configured push destination|没有配置推送目标/)
  })

  test('a harmless command runs without a dialog', async ({ page }) => {
    await terminal.ensureTerminalConnected(0, 15000)
    await terminal.getTerminalContainer(0).click()

    const marker = `safe-${Date.now()}`
    await page.keyboard.type(`echo ${marker}`)
    await page.keyboard.press('Enter')

    await expect(dialog(page)).toBeHidden({ timeout: 3000 })
    await expect
      .poll(async () => (await rows(page)).join('\n'), { timeout: 15000 })
      .toContain(marker)
  })
})

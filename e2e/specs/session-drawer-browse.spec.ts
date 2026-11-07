import { test, expect } from '../fixtures'
import { getSessionCookie } from '../helpers/auth'
import { getServerURL } from '../helpers/server'
import { ChatPage } from '../pages/chat.page'
import { mkdtempSync, rmSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

/**
 * Opening a project's read-only list inside the mobile drawer must size the
 * sheet to that list, not to the screen. The drawer is auto-height, and the
 * browse view overlays the list, so without feeding its height back the sheet
 * either clips it (too short) or fills the screen (too tall).
 */
test.describe.serial('Session drawer: project browse sizing', () => {
  test.describe.configure({ timeout: 300_000 })

  let projectB = ''

  function projectCookie(session: string, project: string): string {
    const name = `cb${process.env.E2E_PORT || '20100'}_clawbench_project`
    return `${session}; ${name}=${encodeURIComponent(project)}`
  }

  async function createSession(cookie: string): Promise<string> {
    const resp = await fetch(`${getServerURL()}/api/ai/sessions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ agentId: 'acp-mock' }),
    })
    if (!resp.ok) throw new Error(`create session failed: ${resp.status}`)
    return ((await resp.json()) as { sessionId: string }).sessionId
  }

  /** A session with one finished reply is "active", so it shows up in the cards. */
  async function seedActiveSession(project: string, message: string): Promise<void> {
    const cookie = projectCookie(await getSessionCookie(), project)
    const id = await createSession(cookie)
    const send = await fetch(`${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(id)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ message, agentId: 'acp-mock' }),
    })
    if (!send.ok) throw new Error(`seed message failed: ${send.status}`)
    const deadline = Date.now() + 90_000
    while (Date.now() < deadline) {
      const list = await fetch(`${getServerURL()}/api/ai/sessions`, { headers: { Cookie: cookie } })
      if (list.ok) {
        const data = (await list.json()) as { sessions?: Array<{ id: string; running?: boolean }> }
        const row = (data.sessions || []).find(s => s.id === id)
        if (row && !row.running) return
      }
      await new Promise(r => setTimeout(r, 400))
    }
    throw new Error(`session ${id} never finished its seeded turn`)
  }

  test.beforeAll(async () => {
    projectB = mkdtempSync(join(tmpdir(), 'clawbench-drawer-browse-'))
    // Two sessions: a short list that must NOT fill the screen.
    await seedActiveSession(projectB, 'drawer browse 1 ' + Date.now())
    await seedActiveSession(projectB, 'drawer browse 2 ' + Date.now())
  })

  test.afterAll(async () => {
    await fetch(`${getServerURL()}/api/project`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: await getSessionCookie() },
      body: JSON.stringify({ path: process.cwd() }),
    }).catch(() => {})
    try { rmSync(projectB, { recursive: true, force: true }) } catch { /* best effort */ }
  })

  test('sizes the sheet to the browsed list instead of the screen', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    // The remembered left tab for this project may be the file manager.
    await page.locator('[data-tab="chat"]').click()
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })
    await chat.openSessionList()

    const sheet = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sheet).toBeVisible()

    // The pinned sidebar stays mounted (hidden) at this width, so scope to the
    // drawer or the locator resolves to the invisible copy.
    await sheet.locator('.pcard-head').first().click()
    const browse = page.locator('[data-project-browse]')
    await expect(browse).toBeVisible({ timeout: 15000 })
    await expect(browse.locator('.browse-row')).toHaveCount(2)

    // Let the grow animation finish before measuring.
    await page.waitForTimeout(500)
    const box = await sheet.boundingBox()
    expect(box, 'the drawer should have a box').not.toBeNull()
    // Content-sized: two rows + headers, nowhere near the 844px viewport.
    expect(box!.height).toBeLessThan(400)
    // …but still tall enough for the two rows to be visible.
    expect(box!.height).toBeGreaterThan(120)
    // And it must sit above the dock rather than run off the screen.
    expect(box!.y + box!.height).toBeLessThanOrEqual(844)
  })
})

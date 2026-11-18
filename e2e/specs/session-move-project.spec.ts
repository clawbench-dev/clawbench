import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { getSessionCookie, apiFetch } from '../helpers/auth'
import { getServerURL } from '../helpers/server'
import { mkdtempSync, realpathSync, rmSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

/**
 * Re-bind a conversation to another existing project (issue #685).
 *
 * The move is a real server-side re-file: the session row, its messages and the
 * rows that denormalize the project id all move, so the conversation must
 * disappear from the source project's list and show up — history included — in
 * the target's. The UI part (menu entry → picker) is exercised through the row
 * menu; the assertions afterwards hit the same project-scoped endpoints the app
 * uses, because "listed in the new project" alone does not prove the history
 * followed (a session whose messages stayed behind renders as an empty chat).
 */
test.describe.serial('Move session to another project', () => {
  test.describe.configure({ timeout: 150_000 })

  const repoRoot = process.cwd()
  let projectA = ''
  let projectB = ''

  /** Cookie header scoped to one project, built without touching the server default. */
  async function projectCookie(path: string): Promise<string> {
    const session = await getSessionCookie()
    const name = `cb${process.env.E2E_PORT || '20100'}_clawbench_project`
    return `${session}; ${name}=${encodeURIComponent(path)}`
  }

  async function listSessionIDs(projectPath: string): Promise<string[]> {
    const resp = await fetch(`${getServerURL()}/api/ai/sessions`, {
      headers: { Cookie: await projectCookie(projectPath) },
    })
    if (!resp.ok) throw new Error(`list sessions failed: ${resp.status}`)
    const data = (await resp.json()) as { sessions?: Array<{ id: string }> }
    return (data.sessions || []).map((s) => s.id)
  }

  /** Create a session in projectPath with one finished user message. */
  async function seedSession(projectPath: string, message: string): Promise<string> {
    const cookie = await projectCookie(projectPath)
    const createResp = await fetch(`${getServerURL()}/api/ai/sessions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ agentId: 'acp-mock' }),
    })
    if (!createResp.ok) throw new Error(`seed session failed: ${createResp.status}`)
    const { sessionId } = (await createResp.json()) as { sessionId: string }

    // session_id goes in the QUERY STRING (the chat handler reads only the query
    // param and the cookie); a body field is silently ignored.
    const sendResp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Cookie: cookie },
        body: JSON.stringify({ message, agentId: 'acp-mock' }),
      },
    )
    if (!sendResp.ok) throw new Error(`seed message failed: ${sendResp.status} ${await sendResp.text()}`)

    // A move is refused while the turn is still writing (its rows carry the
    // project id captured at the start), so wait for it to finish.
    const deadline = Date.now() + 90_000
    while (Date.now() < deadline) {
      const resp = await fetch(`${getServerURL()}/api/ai/sessions`, { headers: { Cookie: cookie } })
      if (resp.ok) {
        const data = (await resp.json()) as { sessions?: Array<{ id: string; running?: boolean }> }
        const row = (data.sessions || []).find((s) => s.id === sessionId)
        if (row && !row.running) return sessionId
      }
      await new Promise((r) => setTimeout(r, 500))
    }
    throw new Error(`session ${sessionId} never finished its seeded turn`)
  }

  /** Create a group in projectPath with `count` acp-mock members. */
  async function seedGroup(projectPath: string, count: number): Promise<string> {
    const resp = await fetch(`${getServerURL()}/api/group/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: await projectCookie(projectPath) },
      body: JSON.stringify({
        hostAgentId: 'acp-mock',
        memberAgentIds: Array.from({ length: count }, () => 'acp-mock'),
      }),
    })
    if (!resp.ok) throw new Error(`seed group failed: ${resp.status} ${await resp.text()}`)
    const { groupId } = (await resp.json()) as { groupId: string }
    return groupId
  }

  /** Send one message and wait until the session is no longer running. */
  async function sendAndWait(projectPath: string, sessionId: string, message: string): Promise<void> {
    const cookie = await projectCookie(projectPath)
    const resp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Cookie: cookie },
        body: JSON.stringify({ message, agentId: 'acp-mock' }),
      },
    )
    if (!resp.ok) throw new Error(`post-move send failed: ${resp.status} ${await resp.text()}`)

    const deadline = Date.now() + 90_000
    while (Date.now() < deadline) {
      const listResp = await fetch(`${getServerURL()}/api/ai/sessions`, { headers: { Cookie: cookie } })
      if (listResp.ok) {
        const data = (await listResp.json()) as { sessions?: Array<{ id: string; running?: boolean }> }
        const row = (data.sessions || []).find((s) => s.id === sessionId)
        if (row && !row.running) return
      }
      await new Promise((r) => setTimeout(r, 500))
    }
    throw new Error(`session ${sessionId} never finished the post-move turn`)
  }

  test.beforeAll(async () => {
    projectA = realpathSync(mkdtempSync(join(tmpdir(), 'clawbench-move-src-')))
    projectB = realpathSync(mkdtempSync(join(tmpdir(), 'clawbench-move-dst-')))
    // Both must be REGISTERED projects (the picker only offers registered
    // ones). Opening a project is what registers it; the second call restores A
    // as the default the browser will land on.
    for (const path of [projectA, projectB, projectA]) {
      await apiFetch('/api/project', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      })
    }
  })

  test.afterAll(async () => {
    // POST /api/project persists the default GLOBALLY (is_default), so a later
    // spec's fresh browser context would otherwise resolve to a temp dir.
    await apiFetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: repoRoot }),
    }).catch(() => {})

    try {
      const listResp = await apiFetch('/api/projects/list')
      if (listResp.ok) {
        const data = (await listResp.json()) as { projects?: Array<{ id: number; path: string }> }
        for (const path of [projectA, projectB]) {
          const entry = (data.projects || []).find((p) => p.path === path)
          if (entry) {
            await apiFetch(`/api/projects/detail?id=${entry.id}`, { method: 'DELETE' }).catch(() => {})
          }
        }
      }
    } catch {
      // Best effort — a leftover registry row for a deleted temp dir is inert.
    }
    for (const path of [projectA, projectB]) {
      if (!path) continue
      try { rmSync(path, { recursive: true, force: true }) } catch { /* best effort */ }
    }
  })

  test('moves the conversation and its history to the target project', async ({ page }) => {
    const marker = `moved-marker-${Date.now()}`
    const sessionId = await seedSession(projectA, marker)
    // A later session is what the app opens on load; keep the moved one OFF
    // screen so this test observes the plain "row leaves the list" path instead
    // of the deliberate "follow the open conversation" switch.
    await seedSession(projectA, `decoy-${Date.now()}`)

    await page.goto('/')
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })

    // Scoped to `.session-row`: the moved conversation legitimately shows up
    // in the "other projects" cards afterwards, and that strip carries the same
    // data-session-id attribute.
    const row = page.locator(`.session-row[data-session-id="${sessionId}"]`)
    await expect(row).toBeVisible({ timeout: 30000 })

    await row.locator('.session-more-btn').click()
    const moveItem = page.locator('.context-menu.visible .context-menu-item[data-menu-item="move-project"]')
    await expect(moveItem).toBeVisible()
    await moveItem.click()

    // The picker lists the OTHER registered project; the source is excluded.
    const target = page.locator(`.sm-item[data-project-path="${projectB}"]`)
    await expect(target).toBeVisible({ timeout: 15000 })
    await target.click()

    // The row must leave the source project's list (the list reloads after the
    // move). Waiting on the row disappearing is the user-visible completion
    // signal, not a fixed sleep.
    await expect(row).toHaveCount(0, { timeout: 15000 })

    // Source no longer owns it; the target does…
    expect(await listSessionIDs(projectA)).not.toContain(sessionId)
    expect(await listSessionIDs(projectB)).toContain(sessionId)

    // …and the messages came along. History is filtered by BOTH project and
    // session, so an empty answer here means the re-file only touched the
    // session row.
    const historyResp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`,
      { headers: { Cookie: await projectCookie(projectB) } },
    )
    expect(historyResp.ok).toBe(true)
    expect(await historyResp.text()).toContain(marker)

    // A turn AFTER the move must still work: the agent's native session was
    // bound to the old directory, so this is what proves it was re-created
    // (with the history injected) instead of resumed there.
    const afterMove = `after-move-${Date.now()}`
    await sendAndWait(projectB, sessionId, afterMove)
    const afterResp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`,
      { headers: { Cookie: await projectCookie(projectB) } },
    )
    expect(afterResp.ok).toBe(true)
    expect(await afterResp.text()).toContain(afterMove)
  })

  test('moves a whole group, members included', async ({ page }) => {
    const groupId = await seedGroup(projectA, 2)

    await page.goto('/')
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })

    // The group row offers the move entry too (groups move as a whole; only
    // task executions are excluded).
    const row = page.locator(`.session-row[data-session-id="${groupId}"]`)
    await expect(row).toBeVisible({ timeout: 30000 })
    await row.locator('.session-more-btn').click()
    const moveItem = page.locator('.context-menu.visible .context-menu-item[data-menu-item="move-project"]')
    await expect(moveItem).toBeVisible()
    await moveItem.click()
    await page.locator(`.sm-item[data-project-path="${projectB}"]`).click()
    await expect(row).toHaveCount(0, { timeout: 15000 })

    expect(await listSessionIDs(projectA)).not.toContain(groupId)
    expect(await listSessionIDs(projectB)).toContain(groupId)

    // The roster still resolves from the TARGET project — a member row left
    // behind under the source project would be a half-moved group.
    const membersResp = await fetch(
      `${getServerURL()}/api/group/members?groupId=${encodeURIComponent(groupId)}`,
      { headers: { Cookie: await projectCookie(projectB) } },
    )
    expect(membersResp.ok).toBe(true)
    const roster = (await membersResp.json()) as { members?: Array<{ id: string }> }
    expect(roster.members).toHaveLength(2)

    // The group must still run after the move: every member's native session
    // was bound to the old directory, so a successful post-move turn proves the
    // members were re-created and their timeline re-injected.
    const afterMove = `group-after-move-${Date.now()}`
    await sendAndWait(projectB, groupId, afterMove)
    const historyResp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(groupId)}`,
      { headers: { Cookie: await projectCookie(projectB) } },
    )
    expect(historyResp.ok).toBe(true)
    expect(await historyResp.text()).toContain(afterMove)
  })
})

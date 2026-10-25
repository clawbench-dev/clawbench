import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { getSessionCookie, apiFetch } from '../helpers/auth'
import { getServerURL } from '../helpers/server'
import { mkdtempSync, rmSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

/**
 * Cross-project session jump: the clicked session must be loaded EXACTLY ONCE.
 *
 * Reproducing the reported flicker needs the target NOT to be the project's most
 * recently updated session. `hotSwitchProject` clears identity, rebuilds the
 * chat subtree, and the remounted panel's first `loadHistory()` runs with an
 * empty id — which used to take the recovery branch (`GET /api/ai/chat` with no
 * session_id) and render whatever `GetLatestSessionID` returned (the DECOY) for
 * one frame, before Phase 7 cleared it and refetched the clicked one.
 *
 * So this spec seeds two sessions in a second project and makes the DECOY the
 * most recent, then clicks the OTHER one from the "other projects" tab. The
 * assertion is on the wire: during the jump there must be no unnamed history
 * request at all. Before the fix there was one — which is the whole bug, and is
 * invisible to a UI-only assertion because the wrong conversation only survives
 * a frame.
 *
 * The read-marking side effect is asserted too: the jump no longer goes through
 * `switchSession` (which used to mark read), so the adopt path has to do it
 * itself or the row's unread badge never clears — the only reason to open a
 * session from that tab.
 */
test.describe.serial('Cross-project session jump', () => {
  test.describe.configure({ timeout: 150_000 })

  const repoRoot = process.cwd()
  let projectB = ''
  let decoyId = ''
  let targetId = ''

  /** Cookie header scoped to project B, built without touching the server default. */
  async function projectBCookie(): Promise<string> {
    const session = await getSessionCookie()
    const name = `cb${process.env.E2E_PORT || '20100'}_clawbench_project`
    return `${session}; ${name}=${encodeURIComponent(projectB)}`
  }

  /** Create a session in project B and give it one finished reply (⇒ unread). */
  async function seedSession(message: string): Promise<string> {
    const cookie = await projectBCookie()
    const createResp = await fetch(`${getServerURL()}/api/ai/sessions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ agentId: 'acp-mock' }),
    })
    if (!createResp.ok) throw new Error(`seed session failed: ${createResp.status}`)
    const { sessionId } = (await createResp.json()) as { sessionId: string }

    // An assistant row is what makes the session unread (and thus visible in the
    // "other projects" tab) — an empty session never appears there.
    //
    // `session_id` goes in the QUERY STRING, not the body: AIChat resolves the
    // target via getSessionID(r), which reads only the query param and the
    // cookie. A body field is silently ignored and the request 400s.
    const sendResp = await fetch(
      `${getServerURL()}/api/ai/chat?session_id=${encodeURIComponent(sessionId)}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Cookie: cookie },
        body: JSON.stringify({ message, agentId: 'acp-mock' }),
      },
    )
    if (!sendResp.ok) {
      throw new Error(`seed message failed: ${sendResp.status} ${await sendResp.text()}`)
    }

    // Wait for the turn to finish: a still-streaming row is not counted as
    // unread, so the tab would not list it yet.
    const deadline = Date.now() + 90_000
    while (Date.now() < deadline) {
      const listResp = await fetch(`${getServerURL()}/api/ai/sessions`, { headers: { Cookie: cookie } })
      if (listResp.ok) {
        const data = (await listResp.json()) as { sessions?: Array<{ id: string; running?: boolean }> }
        const row = (data.sessions || []).find((s) => s.id === sessionId)
        if (row && !row.running) return sessionId
      }
      await new Promise((r) => setTimeout(r, 500))
    }
    throw new Error(`session ${sessionId} never finished its seeded turn`)
  }

  test.beforeAll(async () => {
    projectB = mkdtempSync(join(tmpdir(), 'clawbench-cross-project-'))

    // The DECOY is seeded second, so it is the project's most recently updated
    // session — exactly what the old recovery branch would have rendered.
    targetId = await seedSession('cross target ' + Date.now())
    decoyId = await seedSession('cross decoy ' + Date.now())
    expect(targetId).not.toBe(decoyId)
  })

  test.afterAll(async () => {
    // The jump persists project B as the server-side default (POST /api/project
    // writes `is_default`), and that is GLOBAL: every later spec's fresh browser
    // context would resolve its project to this temp directory. Restore first,
    // then drop the registry row so the project list keeps no dead entry.
    await apiFetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: repoRoot }),
    }).catch(() => {})

    try {
      const listResp = await apiFetch('/api/projects/list')
      if (listResp.ok) {
        const data = (await listResp.json()) as { projects?: Array<{ id: number; path: string }> }
        const entry = (data.projects || []).find((p) => p.path === projectB)
        if (entry) {
          await apiFetch(`/api/projects/detail?id=${entry.id}`, { method: 'DELETE' }).catch(() => {})
        }
      }
    } catch {
      // Best effort — a leftover registry row for a deleted temp dir is inert.
    }
    if (projectB) {
      try { rmSync(projectB, { recursive: true, force: true }) } catch { /* best effort */ }
    }
  })

  test('clicking a session in another project loads it in one request and marks it read', async ({ page }) => {
    // Record every chat-history read issued during the jump. Only GET
    // /api/ai/chat is the history endpoint (POST is a send); the sub-routes
    // (read/cancel/quote) are excluded.
    const chatGets: string[] = []
    page.on('request', (req) => {
      if (req.method() !== 'GET') return
      const url = req.url()
      if (!url.includes('/api/ai/chat')) return
      if (/\/api\/ai\/chat\/(read|cancel|quote)/.test(url)) return
      chatGets.push(url)
    })

    await page.goto('/')
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible({ timeout: 20000 })

    // The "other projects" tab only exists while some other project has an
    // active session — wait for it rather than assuming it is there.
    // `data-session-tab` is the stable hook; matching the label text would break
    // the moment either locale's wording changes.
    const crossTab = page.locator('[data-session-tab="cross"]').first()
    await expect(crossTab, 'seeded session never surfaced in the other-projects tab').toBeVisible({ timeout: 30000 })
    await crossTab.click()

    const targetRow = page.locator(`.cross-session-row[data-session-id="${targetId}"]`)
    await expect(targetRow).toBeVisible({ timeout: 15000 })

    chatGets.length = 0 // ignore anything the initial load did
    await targetRow.locator('.cross-session-item').click()

    // The clicked conversation must actually be on screen…
    await expect(page.locator('.chat-message.user').filter({ hasText: /cross target/ }).first())
      .toBeVisible({ timeout: 30000 })
    // …and nothing else may have been fetched on the way there. The decoy is
    // this project's most recent session, so an unnamed request here means the
    // old recovery branch ran and rendered the decoy first.
    const recoveryGets = chatGets.filter((u) => !u.includes('session_id='))
    expect(recoveryGets, 'the jump issued a recovery request and rendered another conversation first')
      .toHaveLength(0)
    expect(chatGets.filter((u) => u.includes(encodeURIComponent(targetId))).length,
      'the clicked session was never loaded').toBeGreaterThan(0)

    // Read-marking is the adopt path's job now (the jump no longer goes through
    // switchSession). Without it the row keeps its unread badge forever.
    await expect.poll(async () => {
      const cookie = await projectBCookie()
      const resp = await fetch(`${getServerURL()}/api/ai/sessions/overview`, { headers: { Cookie: cookie } })
      if (!resp.ok) return -1
      const data = (await resp.json()) as {
        projects?: Array<{ sessions?: Array<{ id: string; unreadCount: number }> }>
      }
      for (const p of data.projects || []) {
        for (const s of p.sessions || []) {
          if (s.id === targetId) return s.unreadCount
        }
      }
      // Absent from the overview means "not unread" — the badge cleared.
      return 0
    }, { timeout: 30000, intervals: [500, 1000] }).toBe(0)
  })
})

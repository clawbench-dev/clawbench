import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { createGroupViaUI, fetchGroupMembers } from '../helpers/group'

/**
 * E2E for group-member IDENTITY:
 *  1. NAMING: adding the SAME agent to a group twice yields two DIFFERENT
 *     nicknames. A MULTI-instance agent keeps NO bare name — every instance
 *     draws a built-in English pool name (groupNamePool[0..] = "Alice", "Bob").
 *  2. AVATARS: only the SECOND-and-later instance of a repeated agent gets its
 *     OWN DiceBear avatar persisted to the backend; the FIRST instance keeps
 *     the agent's configured avatar (no generated avatar). A LONE agent also
 *     keeps the agent avatar. So a two-instance group has exactly ONE persisted
 *     member avatar, and the two discs still render distinctly.
 *  3. REAL-NAME DISCLOSURE: a pooled member's message shows the real agent name
 *     in `.msg-speaker-real`. (The "nickname == agent name ⇒ no badge" negative
 *     case is covered by the ChatMessageItem unit test, since a two-instance
 *     group has no bare-named member.)
 *
 * The shared agent is acp-mock (deterministic, fast). All three tests are
 * UI/roster assertions against the real backend, so the naming + avatar +
 * disclosure wiring is exercised end to end.
 */
test.describe.serial('group member identity', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  /**
   * createGroupWithDuplicate creates a group with the acp-mock agent added
   * `instances` times, via the shared create-group UI flow.
   */
  async function createGroupWithDuplicate(page: Page, instances: number): Promise<void> {
    await createGroupViaUI(page, 'ACP Mock Agent', instances)
  }

  /** currentSessionId reads the frontend's per-project recent-session id. */
  async function currentSessionId(page: Page): Promise<string> {
    return page.evaluate(() => {
      let best = ''
      let bestAt = -1
      for (const k of Object.keys(localStorage)) {
        if (!k.startsWith('clawbench-recent-session:')) continue
        try {
          const e = JSON.parse(localStorage.getItem(k) || '{}') as { sessionId?: string; accessedAt?: number }
          if (e.sessionId && (e.accessedAt ?? 0) > bestAt) {
            best = e.sessionId
            bestAt = e.accessedAt ?? 0
          }
        } catch { /* ignore malformed entry */ }
      }
      return best
    })
  }

  test('adding the same agent twice yields two distinct nicknames', async ({ page }) => {
    await createGroupWithDuplicate(page, 2)

    // Open the roster sheet from the header avatar stack.
    await page.locator('.group-avatar-stack').click()
    const names = page.locator('.gm-list .gm-name')
    await expect(names).toHaveCount(2, { timeout: 10000 })

    const texts = await names.allTextContents()
    // A multi-instance agent keeps NO bare name: both draw pool names.
    expect(texts).toContain('Alice')
    expect(texts).toContain('Bob')
    expect(texts).not.toContain('ACP Mock Agent')
    // The two nicknames are distinct (the whole point of the pool).
    expect(new Set(texts).size).toBe(2)
  })

  test('only the repeated instance gets its own avatar', async ({ page }) => {
    await createGroupWithDuplicate(page, 2)

    const gid = await currentSessionId(page)
    expect(gid).toBeTruthy()

    // The frontend backfills avatars asynchronously after creation. Only the
    // SECOND instance is generated; the FIRST keeps the agent avatar (empty
    // member avatar ⇒ falls back to the agent's own icon).
    await expect
      .poll(async () => {
        const members = await fetchGroupMembers(page, gid)
        return members.length === 2 && members.filter(m => !!m.avatar).length === 1
      }, { timeout: 30000 })
      .toBe(true)

    const members = await fetchGroupMembers(page, gid)
    // Exactly one persisted avatar, on the second instance (roster order).
    expect(members[0].avatar).toBe('')
    expect(members[1].avatar).not.toBe('')

    // Re-open the group from a fresh load so the roster is fetched WITH the
    // persisted avatar (the in-memory roster is not refreshed by the backfill).
    await page.reload()
    await page.waitForLoadState('networkidle')
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    const row = sessionDrawer.locator(`.session-row[data-session-id="${gid}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()
    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 15000 })

    await page.locator('.group-avatar-stack').click()
    const imgs = page.locator('.gm-list .gm-avatar img.agent-icon-img')
    // The first instance falls back to the agent's built-in icon (an inline SVG,
    // not an <img>), so only ONE <img> disc is present.
    await expect(imgs).toHaveCount(1, { timeout: 10000 })
  })

  test('a pooled member discloses its real agent name', async ({ page }) => {
    // Free-mode group with the same agent twice: BOTH are pool names ("Alice"
    // and "Bob"), since a multi-instance agent keeps no bare name. Create via
    // API (no avatars needed) and open it in the UI.
    const gid = await page.evaluate(async () => {
      const r = await fetch('/api/group/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ memberAgentIds: ['acp-mock', 'acp-mock'] }),
      })
      const j = await r.json()
      return j.groupId as string
    })
    expect(gid).toBeTruthy()
    await page.evaluate(async (gid) => {
      await (window as unknown as { __clawbench: { switchSession: (id: string) => Promise<void> } })
        .__clawbench.switchSession(gid)
    }, gid)
    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 15000 })

    const members = await fetchGroupMembers(page, gid)
    const pooled = members.find(m => m.name === 'Alice')
    expect(pooled, 'the duplicate instance must draw the pool name').toBeTruthy()
    expect(pooled!.agentName).toBe('ACP Mock Agent')

    // The user @-mentions the pooled member; in free mode that member speaks.
    await page.evaluate(async ({ gid, id }) => {
      await fetch(`/api/ai/chat?session_id=${encodeURIComponent(gid)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: `<clawbench-mention targets="${id}"></clawbench-mention> 请你先发言`,
          queueId: 'identity-1',
          clientId: 'probe',
        }),
      })
    }, { gid, id: pooled!.id })

    // The pooled member's message must disclose the real agent name.
    const pooledMsg = page
      .locator('.chat-message.assistant')
      .filter({ has: page.locator('.msg-speaker-name', { hasText: /^Alice$/ }) })
      .first()
    await expect(pooledMsg).toBeVisible({ timeout: 90000 })
    const real = pooledMsg.locator('.msg-speaker-real')
    await expect(real).toBeVisible({ timeout: 15000 })
    await expect(real).toHaveText('ACP Mock Agent')

    // Let the turn finish so the next spec does not inherit a busy mock.
    await expect
      .poll(async () => {
        return page.evaluate(async (gid) => {
          const hist = await (await fetch(`/api/ai/chat?session_id=${encodeURIComponent(gid)}&limit=10`)).json()
          return (hist.messages || []).some((m: { streaming?: boolean }) => m.streaming)
        }, gid)
      }, { timeout: 90000 })
      .toBe(false)
  })
})

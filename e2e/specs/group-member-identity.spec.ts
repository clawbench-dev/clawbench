import { test, expect, type Page } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { createGroupViaUI, fetchGroupMembers } from '../helpers/group'

/**
 * E2E for group-member IDENTITY:
 *  1. NAMING: adding the SAME agent to a group twice yields two DIFFERENT
 *     nicknames — the first keeps the agent's own name, the duplicate draws the
 *     built-in English pool name (groupNamePool[0] = "Alice").
 *  2. AVATARS: each member gets its OWN DiceBear avatar persisted to the backend,
 *     so the two instances render DISTINCT images even though they share one agent.
 *  3. REAL-NAME DISCLOSURE: a pooled member's message shows the real agent name
 *     in `.msg-speaker-real`; a member whose nickname IS the agent name shows no
 *     such badge.
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
    // The first instance keeps the agent's own name; the duplicate draws the pool.
    expect(texts).toContain('ACP Mock Agent')
    expect(texts).toContain('Alice')
    // The two nicknames are distinct (the whole point of the pool).
    expect(new Set(texts).size).toBe(2)
  })

  test('duplicate members render distinct avatars', async ({ page }) => {
    await createGroupWithDuplicate(page, 2)

    const gid = await currentSessionId(page)
    expect(gid).toBeTruthy()

    // The frontend backfills avatars asynchronously after creation; wait until
    // BOTH members have a persisted (non-empty) avatar.
    await expect
      .poll(async () => {
        const members = await fetchGroupMembers(page, gid)
        return members.length === 2 && members.every(m => !!m.avatar)
      }, { timeout: 30000 })
      .toBe(true)

    const avatars = (await fetchGroupMembers(page, gid)).map(m => m.avatar)
    // Two avatars seeded by two different names must differ.
    expect(avatars[0]).not.toBe(avatars[1])

    // Re-open the group from a fresh load so the roster is fetched WITH the
    // persisted avatars (the in-memory roster is not refreshed by the backfill).
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
    await expect(imgs).toHaveCount(2, { timeout: 10000 })
    const srcs = await imgs.evaluateAll(els => els.map(e => e.getAttribute('src') || ''))
    expect(srcs.every(Boolean)).toBe(true)
    expect(new Set(srcs).size).toBe(2)
  })

  test('a pooled member discloses its real agent name', async ({ page }) => {
    // Free-mode group with the same agent twice: members "ACP Mock Agent" and
    // "Alice". Create via API (no avatars needed) and open it in the UI.
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

    // The first instance's nickname IS the real name, so it carries NO badge.
    // (The acp-mock free relay hands the floor to the other member, so it also
    // speaks during the same turn.)
    const baseMsg = page
      .locator('.chat-message.assistant')
      .filter({ has: page.locator('.msg-speaker-name', { hasText: /^ACP Mock Agent$/ }) })
      .first()
    await expect(baseMsg).toBeVisible({ timeout: 90000 })
    await expect(baseMsg.locator('.msg-speaker-real')).toHaveCount(0)

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

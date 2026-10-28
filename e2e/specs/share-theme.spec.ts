import { test, expect, type Page } from '../fixtures'

/**
 * The share topbar's theme picker.
 *
 * The share SPA is a separate entry (web/src/share/) that mounts no App.vue, so
 * it has none of the app's theme plumbing for free: the bootstrap script, the
 * picker's persistence side effect and the mermaid re-render all have to be
 * wired for it explicitly. These run in a real browser because two of the three
 * failures are invisible to jsdom:
 *
 *   - the picker is teleported to <body> and needs the app-menu container chrome
 *     that used to live in AppHeader.vue's unscoped block (which the share page
 *     never mounts) — jsdom applies no stylesheets, so it cannot catch that;
 *   - the theme change is a View Transition, and `data-theme` on <html> is the
 *     only observable result.
 */

const STORE_KEY = 'clawbench-settings-theme'

/** Create a share link for a fresh conversation; returns its path. */
async function createSessionShare(page: Page): Promise<string> {
  const sessionId = await page.evaluate(async () => {
    const created = await fetch('/api/ai/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agentId: 'acp-mock' }),
    })
    if (!created.ok) throw new Error(`create session: ${created.status}`)
    const { sessionId } = await created.json()
    const sent = await fetch('/api/ai/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, message: 'theme_probe_' + Date.now() }),
    })
    if (!sent.ok) throw new Error(`send: ${sent.status}`)
    return sessionId as string
  })

  // The share endpoint rejects a session with no FINALIZED message yet (the
  // reply is still streaming), so poll instead of sleeping a fixed amount: the
  // mock agent's delay is configurable via ACP_MOCK_REPLY_DELAY_MS.
  let sharePath = ''
  await expect.poll(async () => {
    sharePath = await page.evaluate(async (sid) => {
      const resp = await fetch('/api/share/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: sid }),
      })
      if (resp.status === 400) return '' // nothing finalized yet — retry
      if (!resp.ok) throw new Error(`share: ${resp.status}`)
      return ((await resp.json()) as { path: string }).path
    }, sessionId)
    return sharePath
  }, { timeout: 15000 }).not.toBe('')

  return sharePath
}

test.describe('share topbar theme picker', () => {
  test('switches the page theme and persists the choice', async ({ page }) => {
    const sharePath = await createSessionShare(page)

    const anon = await page.context().browser()!.newContext()
    try {
      const sharePage = await anon.newPage()
      await sharePage.goto(sharePath)
      await expect(sharePage.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      // The button lives in the topbar next to the other actions.
      const trigger = sharePage.locator('.share-topbar .share-theme-btn')
      await expect(trigger).toBeVisible()

      await trigger.click()

      // The popup must actually be laid out: a picker rendered without the
      // app-menu column chrome would still exist in the DOM but sit at the
      // document flow origin with a stretched title. A non-zero width that is
      // close to the popup's max-width is the cheapest real-layout assertion.
      const popup = sharePage.locator('.popup-menu .theme-picker')
      await expect(popup).toBeVisible()
      const box = await popup.boundingBox()
      expect(box, 'theme popup must have a box').not.toBeNull()
      expect(box!.width).toBeGreaterThan(120)
      expect(box!.height).toBeGreaterThan(100)

      // Every registered theme is offered (plus the follow-system row) — the
      // list is derived from the same registry the app uses.
      const rows = sharePage.locator('.popup-menu .theme-item')
      await expect(rows.first()).toBeVisible()
      expect(await rows.count()).toBeGreaterThan(30)

      // Pick a theme that is unmistakably not the factory default.
      await sharePage.locator('.popup-menu .theme-item', { hasText: 'Dracula' }).first().click()

      // data-theme flips on <html> and the choice is persisted under the SAME
      // key the app reads — a share-page choice must carry over into the app.
      await expect.poll(() => sharePage.evaluate(() => document.documentElement.getAttribute('data-theme')))
        .toBe('dracula')
      expect(await sharePage.evaluate((key) => localStorage.getItem(key), STORE_KEY))
        .toBe(JSON.stringify('dracula'))

      // The menu closes after the pick.
      await expect(sharePage.locator('.popup-menu .theme-picker')).toHaveCount(0)

      // And the choice survives a reload of the share page.
      await sharePage.reload()
      await expect.poll(() => sharePage.evaluate(() => document.documentElement.getAttribute('data-theme')))
        .toBe('dracula')
    } finally {
      await anon.close()
    }
  })

  test('a fresh share visit uses the app factory theme, not the system scheme', async ({ page }) => {
    const sharePath = await createSessionShare(page)

    const anon = await page.context().browser()!.newContext()
    try {
      const sharePage = await anon.newPage()
      // Force the system scheme to LIGHT. A device that has never stored a theme
      // must still get gruvbox-dark (the factory theme), matching the app — the
      // share bootstrap used to fall through to github-light here.
      await sharePage.emulateMedia({ colorScheme: 'light' })
      await sharePage.goto(sharePath)
      await expect(sharePage.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      expect(await sharePage.evaluate(() => document.documentElement.getAttribute('data-theme')))
        .toBe('gruvbox-dark')
      // Persisted, not merely applied: "never stored a theme" is the durable
      // definition of a fresh install.
      expect(await sharePage.evaluate((key) => localStorage.getItem(key), STORE_KEY))
        .toBe(JSON.stringify('gruvbox-dark'))
    } finally {
      await anon.close()
    }
  })

  test('follow-system resolves against the scheme and is not persisted as an id', async ({ page }) => {
    const sharePath = await createSessionShare(page)

    const anon = await page.context().browser()!.newContext()
    try {
      const sharePage = await anon.newPage()
      await sharePage.goto(sharePath)
      await expect(sharePage.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      await sharePage.emulateMedia({ colorScheme: 'light' })
      await sharePage.locator('.share-topbar .share-theme-btn').click()
      await sharePage.locator('.popup-menu .theme-item', { hasText: 'System' }).first().click()

      // 'auto' must stay 'auto' in storage — writing the resolved id would pin
      // the page and stop it following the system (issue #458).
      await expect.poll(() => sharePage.evaluate((key) => localStorage.getItem(key), STORE_KEY))
        .toBe(JSON.stringify('auto'))
      expect(await sharePage.evaluate(() => document.documentElement.getAttribute('data-theme')))
        .toBe('github-light')
    } finally {
      await anon.close()
    }
  })
})

import { test, expect } from '../fixtures'

/**
 * Group member context panel: the roster row carries the member's OWN context
 * usage and clicking it opens a nested detail sheet.
 *
 * What can be asserted here is the INTERACTION, not the numbers: acp-mock never
 * reports a usage_update, so every member legitimately shows "—" and the panel
 * shows its empty state. That is itself the load-bearing half of the contract —
 * "never reported" must not render as a 0% bar — and the numeric paths are
 * covered by the unit tests (usageDisplay / ContextUsageDetail /
 * MemberContextSheet).
 */
test.describe.serial('group member context panel', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('roster shows a usage slot per member and opens a nested panel', async ({ page }) => {
    // Create a group with two DISTINCT members through the API, then open THAT
    // group.
    //
    // `acp-mock-b` is the second agent the harness registers; a spec that named
    // an agent the fixture does not have ("claude") silently built a
    // single-member group, because the backend skips unknown ids. The response
    // is checked so that class of mistake fails loudly here instead.
    //
    // The id is captured and used to pick the row: the session list is ordered
    // by recency and already contains the fixture's own chat sessions, so
    // "click the first row" opened a plain single-chat session and the group
    // header never appeared. That made this spec fail identically on a clean
    // checkout — it was ordering-dependent, not a product defect.
    const created = await page.evaluate(async () => {
      const resp = await fetch('/api/group/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostAgentId: 'acp-mock', memberAgentIds: ['acp-mock', 'acp-mock-b'] }),
      })
      return { status: resp.status, body: await resp.json() }
    })
    expect(created.body.ok, `group create failed: ${JSON.stringify(created.body)}`).toBe(true)
    const groupId = String(created.body.groupId ?? '')
    expect(groupId).toBeTruthy()

    // Open the session list and click the row for the group we just created.
    const listBtn = page.locator('[data-action="session"]')
    await listBtn.click()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    await sessionDrawer.locator(`[data-session-id="${groupId}"]`).first().click()

    // Header avatar stack → group settings sheet.
    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 20000 })
    await page.locator('.group-avatar-stack').click()

    const rows = page.locator('.gm-list .gm-row-main')
    await expect(rows.first()).toBeVisible({ timeout: 10000 })
    const count = await rows.count()
    expect(count).toBeGreaterThan(0)

    // Every live member has a usage slot. acp-mock reports none, so each must
    // read "—" (never 0%).
    await expect(page.locator('.gm-usage')).toHaveCount(count)
    for (let i = 0; i < count; i++) {
      await expect(page.locator('.gm-usage').nth(i)).toContainText('—')
    }

    // Each row also names the model that member runs, on a second line inside
    // the identity column (never a second trailing column — the row only has
    // room for the usage chip there on a phone). acp-mock advertises models, so
    // the inherited default is a real name; the assertion only requires a
    // non-empty label, since which model the mock defaults to is its own
    // business.
    const modelLines = page.locator('.gm-list .gm-identity .gm-model')
    await expect(modelLines).toHaveCount(count)
    for (let i = 0; i < count; i++) {
      await expect(modelLines.nth(i)).not.toBeEmpty()
    }

    // Clicking the row opens the member's settings sheet (the shared
    // SessionDrawer) ABOVE the roster, on the drawer's DEFAULT tab — the same
    // tab the single-chat session info dialog opens on (model). The roster must
    // not force a tab of its own: that is what made the member path the one
    // place where "open the session info" landed somewhere else.
    //
    // `.session-settings-sheet` is the panel hook on the per-session settings
    // drawer. It must NOT be `.session-drawer-sheet`: that class belongs to
    // components/session/SessionDrawer.vue — the session LIST — and matching it
    // here would silently select the wrong sheet.
    await rows.first().click()
    const memberSheet = page.locator('.bs-panel.session-settings-sheet')
    await expect(memberSheet).toBeVisible({ timeout: 10000 })
    // The tab's own label is the i18n `chat.modelSwitcher.title` ("Model" /
    // "模型") — not the word "model" in the payload.
    await expect(memberSheet.locator('.model-tab.active')).toHaveText(/model|模型/i)
    // The model tab is the one that lists models, so it is what proves the tab
    // body really rendered (not just the tab strip).
    await expect(memberSheet.locator('.model-tab-content .model-list')).toBeVisible()
    // The roster is still open behind it (nested, not a replacement).
    await expect(page.locator('.gm-list')).toBeVisible()

    // Back closes ONLY the member panel.
    await page.keyboard.press('Escape')
    await expect(memberSheet).toBeHidden({ timeout: 10000 })
    await expect(page.locator('.gm-list')).toBeVisible()
  })
})

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
    // Create a group with two members through the API, then open it.
    await page.evaluate(async () => {
      await fetch('/api/group/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostAgentId: 'acp-mock', memberAgentIds: ['acp-mock', 'claude'] }),
      })
    })

    // Open the most recently used group from the session list.
    const listBtn = page.locator('[data-action="session"]')
    await listBtn.click()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    await sessionDrawer.locator('.session-row .session-item').first().click()

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

    // Clicking the row opens the member's settings sheet (the shared
    // SessionDrawer) ABOVE the roster, on its usage tab.
    //
    // `.session-settings-sheet` is the panel hook on the per-session settings
    // drawer. It must NOT be `.session-drawer-sheet`: that class belongs to
    // components/session/SessionDrawer.vue — the session LIST — and matching it
    // here would silently select the wrong sheet.
    await rows.first().click()
    const memberSheet = page.locator('.bs-panel.session-settings-sheet')
    await expect(memberSheet).toBeVisible({ timeout: 10000 })
    // Opens on the usage tab, because the chip the user just tapped IS a usage
    // number. The tab's own label is the i18n `chat.sessionInfo.contextUsage`
    // ("Context" / "上下文") — not the word "usage".
    await expect(memberSheet.locator('.model-tab.active')).toHaveText(/context|上下文/i)
    // acp-mock reports no usage ⇒ the empty state, never a 0% bar.
    await expect(memberSheet.locator('.usage-tab-content .tab-empty-hint')).toBeVisible()
    // The roster is still open behind it (nested, not a replacement).
    await expect(page.locator('.gm-list')).toBeVisible()

    // Back closes ONLY the member panel.
    await page.keyboard.press('Escape')
    await expect(memberSheet).toBeHidden({ timeout: 10000 })
    await expect(page.locator('.gm-list')).toBeVisible()
  })
})

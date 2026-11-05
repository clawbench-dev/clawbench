import { test, expect } from '../fixtures'

/**
 * 删除成员 (remove a member from a group).
 *
 * The control sits AFTER the disable toggle and is destructive, so it must ask
 * first. The observable contract has two halves that must agree:
 *   - the server's roster (`GET /api/group/members`): the removed row is still
 *     returned with `removed: true` (past speech needs its name), but the
 *     scheduling roster is empty of it;
 *   - the sheet: the row disappears and the host's own remove control is inert
 *     (the host is the group's only router).
 */
test.describe.serial('group member removal', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('removes a non-host member and the roster follows', async ({ page }) => {
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

    // Pick the target by ID from the server's roster, never by position.
    const before = await page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${gid}`)
      return (await r.json()) as { members: { id: string; isHost: boolean }[] }
    }, groupId)
    expect(before.members.length).toBe(2)
    const host = before.members.find(m => m.isHost)
    const target = before.members.find(m => !m.isHost)
    expect(host, 'fixture: one member must be the host').toBeTruthy()
    expect(target, 'fixture: one member must not be the host').toBeTruthy()

    await page.locator('[data-action="session"]').click()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    await sessionDrawer.locator(`[data-session-id="${groupId}"]`).first().click()

    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 20000 })
    await page.locator('.group-avatar-stack').click()

    const rows = page.locator('.gm-list .gm-row')
    await expect(rows.first()).toBeVisible({ timeout: 10000 })
    await expect(rows).toHaveCount(2)

    const rowFor = (memberId: string) => page.locator(`.gm-list .gm-row[data-member-id="${memberId}"]`)
    const hostRow = rowFor(host!.id)
    const targetRow = rowFor(target!.id)

    // The host's remove control is present (column alignment) but inert.
    await expect(hostRow.locator('.gm-remove-toggle')).toBeDisabled()

    // Exactly ONE live remove control (the host's is inert).
    await expect(page.locator('[data-action="remove-member"]')).toHaveCount(1)

    await targetRow.locator('[data-action="remove-member"]').click()

    // Destructive action: it must ask before doing anything.
    await expect(page.locator('.dlg-box')).toBeVisible({ timeout: 10000 })
    await page.locator('.dlg-ok').click()

    // The row is gone from the sheet.
    await expect(rows).toHaveCount(1, { timeout: 10000 })
    await expect(targetRow).toHaveCount(0)

    // The server agrees: the removed row is flagged, the host is untouched.
    const after = await page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${gid}`)
      return (await r.json()) as { members: { id: string; isHost: boolean; removed: boolean }[] }
    }, groupId)
    expect(after.members).toHaveLength(2)
    const removed = after.members.find(m => m.id === target!.id)
    expect(removed?.removed).toBe(true)
    expect(after.members.find(m => m.isHost)?.removed).toBe(false)

    // The remaining member is the last one: its remove control must be inert
    // (an empty group can never run another turn).
    await expect(hostRow.locator('.gm-remove-toggle')).toBeDisabled()
  })
})

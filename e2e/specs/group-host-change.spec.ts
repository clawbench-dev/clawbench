import { test, expect } from '../fixtures'

/**
 * 更换主持人 (issue #608): the host was fixed at creation, so a mis-picked host
 * forced the user to build a whole new group.
 *
 * The host is a POINTER on the group row, so the swap has two observable halves
 * that must agree:
 *   - the server's roster (`GET /api/group/members` → exactly one `isHost`), which
 *     every other surface (session list glyph, prompt building, disable guard)
 *     derives from;
 *   - the sheet's crown control, which is the only entry point.
 *
 * Both are asserted: a 200 that leaves the pointer alone, or a pointer that
 * moves while the sheet keeps the old pill, are each a user-visible defect.
 */
test.describe.serial('group host change', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  test('promotes another member and the roster follows', async ({ page }) => {
    // Two DISTINCT members (acp-mock + acp-mock-b): the second one is the
    // promotion target, and an unknown agent id would be silently skipped by the
    // backend, leaving a single-member group whose only row is the host.
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

    // Pick the row by ID from the server's roster, never by position: the sheet
    // pins the host first, so positions are exactly what the swap changes.
    const before = await page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${gid}`)
      return (await r.json()) as { members: { id: string; name: string; isHost: boolean }[] }
    }, groupId)
    expect(before.members.length).toBe(2)
    const oldHost = before.members.find(m => m.isHost)
    const target = before.members.find(m => !m.isHost)
    expect(oldHost, 'fixture: one member must start as host').toBeTruthy()
    expect(target, 'fixture: one member must not be the host').toBeTruthy()

    // Open the group, then its settings sheet (header avatar stack).
    await page.locator('[data-action="session"]').click()
    const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(sessionDrawer).toBeVisible({ timeout: 10000 })
    await sessionDrawer.locator(`[data-session-id="${groupId}"]`).first().click()

    await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 20000 })
    await page.locator('.group-avatar-stack').click()

    const rows = page.locator('.gm-list .gm-row')
    await expect(rows.first()).toBeVisible({ timeout: 10000 })
    await expect(rows).toHaveCount(2)

    // Rows are addressed by the member-row id, not by their text: the host is
    // pinned first, so positions move under the swap, and display names are
    // prefix-related ("ACP Mock Agent" / "ACP Mock Agent 2") so a substring
    // match would silently hit the wrong row.
    const rowFor = (memberId: string) => page.locator(`.gm-list .gm-row[data-member-id="${memberId}"]`)
    const targetRow = rowFor(target!.id)
    await expect(targetRow).toBeVisible()

    // Exactly ONE enabled crown before the swap (the host's is inert).
    await expect(page.locator('[data-action="make-host"]')).toHaveCount(1)

    await targetRow.locator('[data-action="make-host"]').click()

    // The sheet must reflect the swap: the target row grows the host pill, and
    // the promoted row's crown turns inert (it is now the current host).
    await expect(targetRow.locator('.gm-tag--host')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('[data-action="make-host"]')).toHaveCount(1)
    const demotedRow = rowFor(oldHost!.id)
    await expect(demotedRow.locator('.gm-tag--host')).toHaveCount(0)

    // The server agrees, and the role moved to exactly the requested member.
    const after = await page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${gid}`)
      return (await r.json()) as { members: { id: string; isHost: boolean }[] }
    }, groupId)
    const hosts = after.members.filter(m => m.isHost)
    expect(hosts).toHaveLength(1)
    expect(hosts[0].id).toBe(target!.id)

    // The demoted member is now an ordinary participant: it can be promoted
    // back (the crown is live again), which is the reversible half of the swap.
    await expect(demotedRow.locator('[data-action="make-host"]')).toBeVisible()
  })
})

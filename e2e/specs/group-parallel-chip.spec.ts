import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * The free-mode concurrency switch has two entry points that must read ONE
 * server-authoritative value: the chip in the chat's session-info bar and the
 * switch in the group settings sheet. This spec drives the CHIP against the
 * real backend and then reads the value back through the roster API, which is
 * what the settings sheet renders from — so a chip that only flipped local
 * state would fail here even though it looked right on screen.
 *
 * It also pins the mode gate: host mode has no owner for the switch (the host
 * routes; the user never @-names speakers), so the chip must not render there.
 */
test.describe.serial('group free-mode concurrency chip', () => {
  test.skip(({ browserName }) => browserName !== 'chromium', 'serial group test')
  test.setTimeout(120000)

  async function openGroup(page: import('@playwright/test').Page, groupId: string) {
    const chat = new ChatPage(page)
    await chat.openSessionList()
    const drawer = page.locator('.bs-panel.session-drawer-sheet')
    await expect(drawer).toBeVisible({ timeout: 5000 })
    const row = drawer.locator(`.session-row[data-session-id="${groupId}"]`).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await row.locator('.session-item').click()
    await expect(chat.textarea).toBeVisible({ timeout: 15000 })
  }

  test('the chip shows in free mode, toggles the server value, and stays hidden in host mode', async ({ page }) => {
    // Free group: created without hostAgentId. Two members are required.
    const freeId = await page.evaluate(async () => {
      const r = await fetch('/api/group/create', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ memberAgentIds: ['acp-mock', 'acp-mock-b'] }),
      })
      const j = await r.json()
      return j.groupId as string
    })
    expect(freeId).toBeTruthy()

    await openGroup(page, freeId)

    const chip = page.locator('.session-info-parallel')
    await expect(chip).toBeVisible({ timeout: 15000 })
    // Off by default: no highlight.
    await expect(chip).not.toHaveClass(/session-info-parallel-on/)

    await chip.click()
    // Server value is the source of truth: the roster API (same endpoint the
    // settings sheet reads) must now report true.
    await expect.poll(async () => page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${encodeURIComponent(gid)}`)
      return (await r.json()).parallelDefault
    }, freeId), { timeout: 10000 }).toBe(true)
    await expect(chip).toHaveClass(/session-info-parallel-on/)

    // Toggling OFF must actually reach the server too — a chip that computed
    // next=!false from a stale default could only ever turn things on.
    await chip.click()
    await expect.poll(async () => page.evaluate(async (gid) => {
      const r = await fetch(`/api/group/members?groupId=${encodeURIComponent(gid)}`)
      return (await r.json()).parallelDefault
    }, freeId), { timeout: 10000 }).toBe(false)
    await expect(chip).not.toHaveClass(/session-info-parallel-on/)

    // Host mode: the chip must not render (the switch has no owner there).
    const hostId = await page.evaluate(async () => {
      const r = await fetch('/api/group/create', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        // The host must also be listed as a member (the API rejects a host that
        // is not in memberAgentIds).
        body: JSON.stringify({ hostAgentId: 'acp-mock', memberAgentIds: ['acp-mock', 'acp-mock-b'] }),
      })
      const j = await r.json()
      return j.groupId as string
    })
    await openGroup(page, hostId)
    await expect(page.locator('.session-info-cruise')).toBeVisible({ timeout: 15000 })
    await expect(page.locator('.session-info-parallel')).toHaveCount(0)
  })
})

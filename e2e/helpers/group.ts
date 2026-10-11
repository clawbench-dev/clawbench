import { expect, type Locator, type Page } from '@playwright/test'
import { ChatPage } from '../pages/chat.page'

/**
 * Shared UI flow for creating a group through the session drawer's create page.
 *
 * Centralised here because the entry point already drifted once: the old
 * dedicated `[data-action="create-group"]` button was removed in favour of a
 * single/group segmented switch inside the create page, and a spec still
 * targeted the dead selector. One helper means one place to update.
 */

/**
 * `AgentSelectorDrawer.vue` re-arms its 400ms accidental-touch guard whenever
 * group mode is switched on (it watches `groupMode` and resets `openTime`), so
 * any row selection right after opening OR switching mode must wait past it or
 * the first click is swallowed. 500ms > 400ms with margin.
 */
const TOUCH_GUARD_WAIT_MS = 500

/** waitPastTouchGuard waits out the drawer's accidental-touch guard window. */
export async function waitPastTouchGuard(page: Page): Promise<void> {
  await page.waitForTimeout(TOUCH_GUARD_WAIT_MS)
}

/**
 * openCreateGroupPicker opens the session list → create page → group mode and
 * returns the picker panel, ready for selection (the touch guard has passed).
 */
export async function openCreateGroupPicker(page: Page): Promise<Locator> {
  const chat = new ChatPage(page)
  await chat.openSessionList()
  const sessionDrawer = page.locator('.bs-panel.session-drawer-sheet')
  await expect(sessionDrawer).toBeVisible({ timeout: 5000 })

  // There is no dedicated "create group" button: open the create page and flip
  // the single/group segmented switch.
  await sessionDrawer.locator('[data-action="create"]').click()
  const picker = page.locator('.bs-panel').filter({ has: page.locator('.agent-list') }).last()
  await expect(picker).toBeVisible({ timeout: 5000 })

  await picker.locator('.agent-mode-tab[data-mode="group"]').click()
  await waitPastTouchGuard(page)
  return picker
}

/**
 * selectAgentRow selects one instance of `agentName` (EXACT `.agent-option-name`
 * match, so "ACP Mock Agent" never matches the "ACP Mock Agent B" row) and
 * returns its row. Omit `agentName` to select the first row.
 */
export async function selectAgentRow(picker: Locator, page: Page, agentName?: string): Promise<Locator> {
  const row = agentName
    ? picker
        .locator('.agent-option')
        .filter({ has: page.locator('.agent-option-name', { hasText: new RegExp(`^${escapeRegExp(agentName)}$`) }) })
        .first()
    : picker.locator('.agent-option').first()
  await expect(row).toBeVisible({ timeout: 5000 })
  await row.click()
  return row
}

/** bumpInstances bumps a selected row's instance stepper up to `instances`. */
export async function bumpInstances(row: Locator, instances: number): Promise<void> {
  const stepper = row.locator('.agent-stepper')
  await expect(stepper).toBeVisible({ timeout: 5000 })
  for (let i = 1; i < instances; i++) {
    await stepper.locator('.agent-stepper-btn').last().click()
  }
  await expect(stepper.locator('.agent-stepper-count')).toHaveText(String(instances))
}

/**
 * createGroupViaUI runs the whole flow (select `instances` copies of
 * `agentName`, confirm) and resolves once the new group is the open session
 * (its header avatar stack is visible).
 */
export async function createGroupViaUI(page: Page, agentName: string, instances: number): Promise<void> {
  const picker = await openCreateGroupPicker(page)
  const row = await selectAgentRow(picker, page, agentName)
  await bumpInstances(row, instances)
  await picker.locator('.agent-multi-confirm').click()
  await expect(page.locator('.group-avatar-stack')).toBeVisible({ timeout: 15000 })
}

/** GroupMemberSummary is the roster fields the identity specs assert on. */
export interface GroupMemberSummary {
  id: string
  name: string
  agentName: string
  avatar: string
}

/** fetchGroupMembers reads a group's roster through the real API. */
export async function fetchGroupMembers(page: Page, groupId: string): Promise<GroupMemberSummary[]> {
  return page.evaluate(async (gid) => {
    const j = await (await fetch(`/api/group/members?groupId=${encodeURIComponent(gid)}`)).json()
    return (j.members || []).map((m: { id: string; name: string; agentName: string; avatar: string }) => ({
      id: m.id,
      name: m.name,
      agentName: m.agentName,
      avatar: m.avatar,
    }))
  }, groupId)
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

import { test, expect, type Page } from '../fixtures'
import { projectApiFetch } from '../helpers/auth'

/** Flip a server master switch. Used to prove the dock follows the server. */
async function setFeature(section: 'mail', enabled: boolean): Promise<void> {
  const resp = await projectApiFetch('/api/config', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ [section]: { enabled } }),
  })
  expect(resp.ok, `PATCH ${section}.enabled=${enabled}`).toBe(true)
}

/**
 * The dock plugin manager is the only UI that writes `dockHidden`, and the dock
 * is the only UI that reads it. The pair used to be easy to break silently:
 * a hidden entry that came back after a reload, or one that never came back at
 * all, both look like "the switch did nothing".
 *
 * The badge is asserted too: hiding a tab must remove the entry WITH everything
 * hanging off it, and showing it again must bring the badge back — a badge that
 * survives in a detached node, or one that is lost on re-mount, is the kind of
 * regression a "the button is back" assertion cannot see.
 *
 * 历史 (git history) is used rather than a server-backed tab on purpose: its
 * switch is display-only, so this spec never writes server config and cannot
 * disturb the specs that run after it in the same (serial) browser job.
 */

const BADGE_FILE = 'e2e-dock-badge.tmp'

async function seedWorkingTreeChange(): Promise<void> {
  // An untracked file makes `git status --porcelain` non-empty, which is what
  // feeds the history tab's badge.
  const resp = await projectApiFetch('/api/file/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: '', name: BADGE_FILE }),
  })
  expect(resp.ok, 'seed an untracked file for the history badge').toBe(true)
}

async function clearWorkingTreeChange(): Promise<void> {
  await projectApiFetch('/api/file/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: BADGE_FILE }),
  }).catch(() => {})
}

function dockEntry(page: Page, tab: string) {
  return page.locator(`.dock-btn[data-tab="${tab}"]`).filter({ visible: true }).first()
}

function dockBadge(page: Page, tab: string) {
  return page
    .locator('.dock-btn-wrap')
    .filter({ has: page.locator(`.dock-btn[data-tab="${tab}"]`) })
    .locator('.dock-badge-count')
    .filter({ visible: true })
    .first()
}

/**
 * Wait for the SPA to be interactive.
 *
 * `page.reload()` resolves at load, not at "the dock is rendered" — probing the
 * dock before then reports "no such button", which would make the "still
 * hidden" assertions pass for the wrong reason.
 */
async function waitForDock(page: Page): Promise<void> {
  await expect(dockEntry(page, 'browse')).toBeVisible({ timeout: 15000 })
}

async function openDockManager(page: Page): Promise<void> {
  // Click a DIFFERENT tab first, then settings. Clicking the dock button of the
  // tab that is already active is the VS Code-style "collapse the pane" gesture
  // (`handleWideDockTabClick`), so going straight to settings after a reload
  // that restored settings could hide the very panel we want to reach.
  await dockEntry(page, 'browse').click()
  await dockEntry(page, 'settings').click()
  await expect(page.locator('.settings-page')).toBeVisible({ timeout: 10000 })
  await page.locator('.settings-index__row[data-category="dockPlugins"]').click()
  await expect(page.locator('[data-dock-plugin="history"]')).toBeVisible({ timeout: 5000 })
}

/** The switch input is opacity:0/0×0, so the track is the clickable target. */
function historySwitch(page: Page) {
  return page.locator('[data-dock-plugin="history"] .settings-item__switch-track')
}

test.describe('dock plugin manager', () => {
  test('hide → reload → still hidden → show → entry and badge return', async ({ page }) => {
    await seedWorkingTreeChange()
    try {
      // The badge is read on mount, so pick up the seeded change before we
      // start asserting anything about it.
      await page.reload()
      await waitForDock(page)
      await expect(dockEntry(page, 'history')).toBeVisible({ timeout: 10000 })
      await expect(dockBadge(page, 'history')).toBeVisible({ timeout: 10000 })

      // 1. Hide it from the manager.
      await openDockManager(page)
      await historySwitch(page).click()
      await expect(page.locator('[data-dock-plugin="history"]')).toHaveClass(/is-hidden/)
      await expect(page.locator('.dock-btn[data-tab="history"]')).toHaveCount(0)

      // 2. It must stay hidden across a reload — the switch writes the persisted
      //    `dockHidden` set, not just the in-memory one.
      await page.reload()
      await waitForDock(page)
      await expect(page.locator('.dock-btn[data-tab="history"]')).toHaveCount(0)
      await openDockManager(page)
      await expect(
        page.locator('[data-dock-plugin="history"] input[type="checkbox"]'),
      ).not.toBeChecked()

      // 3. Show it again: the entry comes back WITH its badge.
      await historySwitch(page).click()
      await expect(dockEntry(page, 'history')).toBeVisible({ timeout: 5000 })
      await expect(dockBadge(page, 'history')).toBeVisible({ timeout: 5000 })
    } finally {
      await clearWorkingTreeChange()
    }
  })
})

test.describe('server feature switch', () => {
  // The dock reads `/api/features` (the server's plugin registry) — it does not
  // decide "is this feature on" from the config snapshot any more. Turning a
  // feature off on the server must therefore remove the entry, and turning it
  // back on must restore it, with no frontend-side list to keep in sync.
  //
  // This spec writes server state, so it repairs itself on both sides: force the
  // switch ON before and after, so an interrupted run cannot leave mail off for
  // the specs that follow in this (serial) browser job.
  test.beforeEach(async () => {
    await setFeature('mail', true)
  })
  test.afterEach(async () => {
    await setFeature('mail', true)
  })

  test('a feature switched off on the server leaves the dock', async ({ page }) => {
    await page.reload()
    await waitForDock(page)
    await expect(dockEntry(page, 'mail')).toBeVisible({ timeout: 10000 })

    await setFeature('mail', false)
    await page.reload()
    await waitForDock(page)
    await expect(page.locator('.dock-btn[data-tab="mail"]')).toHaveCount(0)

    await setFeature('mail', true)
    await page.reload()
    await waitForDock(page)
    await expect(dockEntry(page, 'mail')).toBeVisible({ timeout: 10000 })
  })
})

test.describe('plugin-declared deep links', () => {
  // The window listeners are installed by the plugin registry now, not by one
  // `handleOpenXxx` per plugin in App.vue. This is the end-to-end proof that the
  // installer wires them and that the handler's `tabBlocked` guard runs: with the
  // feature ON the jump navigates, with it OFF nothing happens (and no pending
  // state is left behind for a later activation).
  test.beforeEach(async () => {
    await setFeature('mail', true)
  })
  test.afterEach(async () => {
    await setFeature('mail', true)
  })

  test('a deep link declared by a plugin navigates, and is refused while blocked', async ({ page }) => {
    await page.reload()
    await waitForDock(page)

    // 分享中心's 「用邮件发送」 dispatches this; the handler only switches tabs
    // (the panel owns the parked compose request).
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('clawbench-compose-mail'))
    })
    await expect(dockEntry(page, 'mail')).toHaveClass(/active/, { timeout: 5000 })

    // Switch away, turn the feature off, and dispatch again: the jump must be
    // refused rather than land on a panel with no dock entry pointing at it.
    await dockEntry(page, 'browse').click()
    await expect(dockEntry(page, 'browse')).toHaveClass(/active/)
    await setFeature('mail', false)
    await page.reload()
    await waitForDock(page)
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('clawbench-compose-mail'))
    })
    // Still on the file manager — the refused jump changed nothing.
    await expect(dockEntry(page, 'browse')).toHaveClass(/active/)
    await expect(page.locator('.dock-btn[data-tab="mail"]')).toHaveCount(0)
  })
})

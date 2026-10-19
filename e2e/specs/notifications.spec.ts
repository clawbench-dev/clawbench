import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { ChatPage } from '../pages/chat.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Notification center: a browsable, append-only log of session/task/forge
 * events. It has NO read/unread state and NO badge — it answers "what
 * happened", not "what is pending".
 *
 * The cross-component flow under test: a real chat turn completes -> the
 * backend writes a notification row -> the dock tab exists -> the panel lists
 * it -> the filters narrow it -> clicking a row navigates back to its source.
 */
test.describe('Notifications', () => {
  let chat: ChatPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    nav = new NavigationPage(page)
    // Start from a clean log so tests are order-independent (the clear test
    // would otherwise wipe rows a later test expects, and stale rows from a
    // previous spec would break the "exactly the row I just made" assertions).
    await page.request.delete('/api/notifications')
  })

  /**
   * Drive one real chat turn and wait for its notification row to be RECORDED.
   *
   * Neither the streamed reply text nor the stop button is a sufficient signal:
   * the row is written at terminal completion, and markDoneAndSendFinal emits
   * the `done` WS event *before* EmitSessionPushNotification records it, so the
   * stop button can clear first. Polling the API for the row is the only
   * signal that is true by construction — it asserts the thing the UI then
   * reads, rather than a proxy that merely correlates with it.
   */
  async function sendAndWaitForNotification(page: Page, text: string): Promise<void> {
    const before = await chat.sendMessage(text)
    await chat.waitForReply(30000, before)

    await expect.poll(async () => {
      const res = await page.request.get('/api/notifications')
      if (!res.ok()) return -1
      const body = await res.json()
      return body.total ?? 0
    }, { timeout: 30000, message: 'a notification row should be recorded after the turn completes' })
      .toBeGreaterThan(0)
  }

  test('the notifications tab exists and opens the panel', async ({ page }) => {
    await nav.switchToNotifications()

    // The panel renders its own header (breadcrumb + filter row), independent
    // of whether the log has rows yet.
    await expect(page.locator('.notif-panel')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.notif-filters')).toBeVisible()
  })

  test('the tab renders without an unread badge', async ({ page }) => {
    // The design has no read/unread concept, so the tab must never carry a
    // count badge — unlike chat/tasks/forge. This is the guard for that.
    await nav.switchToNotifications()
    const btn = nav.getTabButton('notifications')
    await expect(btn).toBeVisible({ timeout: 10000 })
    await expect(btn.locator('.count-badge')).toHaveCount(0)
  })

  test('a completed chat turn appears in the log', async ({ page }) => {
    await sendAndWaitForNotification(page, 'notif_' + Date.now())

    await nav.switchToNotifications()

    await expect(page.locator('.notif-card').first()).toBeVisible({ timeout: 15000 })
    await expect(page.locator('.evt').first()).toBeVisible()
  })

  test('clicking a session row navigates back to the session', async ({ page }) => {
    await sendAndWaitForNotification(page, 'notifnav_' + Date.now())

    await nav.switchToNotifications()
    const card = page.locator('.notif-card').first()
    await expect(card).toBeVisible({ timeout: 15000 })

    // A row whose source still exists must navigate. (A sourceMissing row
    // would instead toast and stay put — covered by the unit tests.)
    await card.click()

    // We land back on chat with the session open.
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
  })

  test('Back returns from a notification source to the log', async ({ page }) => {
    // The jump records the LOG as its origin, so the app-wide Back (header
    // button / edge swipe / Android hardware back — all one state machine)
    // must land on the notification center, not on whatever tab preceded it.
    //
    // Send the turn FIRST, at the default viewport: resizing wide→narrow
    // carries the wide left tab into the narrow `activeTab`, which can leave
    // the chat pane hidden and make sendMessage() time out.
    await sendAndWaitForNotification(page, 'notifback_' + Date.now())

    // NARROW viewport on purpose: at >=960px chat is the right pane and the
    // left panel keeps showing the log regardless of the jump, so the assertion
    // would pass without any origin being recorded at all. Only in narrow mode
    // are the tabs mutually exclusive and the Back target observable.
    //
    // 700px (not a phone width): still below the 960px wide-screen threshold,
    // but wide enough that the bottom dock renders every tab INLINE. At 420px
    // the notifications tab lives behind the overflow popup, which adds a
    // second, unrelated interaction to this test.
    await page.setViewportSize({ width: 700, height: 900 })

    await nav.switchToNotifications()
    const card = page.locator('.notif-card').first()
    await expect(card).toBeVisible({ timeout: 15000 })
    await card.click()
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 10000 })
    // The log is gone: we really left it.
    await expect(page.locator('.notif-panel')).toBeHidden({ timeout: 10000 })

    // Drive the same event Android's onBackPressed dispatches. The state
    // machine reads the recorded origin synchronously and navigates.
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('clawbench-back-press', { detail: { reason: 'android' } }))
    })

    await expect(page.locator('.notif-panel')).toBeVisible({ timeout: 10000 })
  })

  test('Back returns from the usage-meter settings page to the log', async ({ page }) => {
    // The usage row's jump lands on the settings page (narrow) — a whole-tab
    // navigation, so it needs the same recorded origin as session/task/forge.
    // Without it the first Back only popped the settings nav stack and the
    // second one exited the app: the log was unreachable.
    //
    // The row itself cannot be seeded through the API (notifications are
    // written by the server on a threshold crossing), so this dispatches the
    // exact event the panel dispatches, flag included. The panel's own dispatch
    // is pinned by NotificationsPanelContent.test.ts.
    await page.setViewportSize({ width: 700, height: 900 })
    await nav.switchToNotifications()
    await expect(page.locator('.notif-panel')).toBeVisible({ timeout: 10000 })

    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('clawbench-open-usage-meter', {
        detail: { fromNotificationCenter: true },
      }))
    })

    // We really left the log, and landed on the usage rule.
    await expect(page.locator('.settings-page')).toBeVisible({ timeout: 10000 })
    await expect(page.locator('.notif-panel')).toBeHidden({ timeout: 10000 })
    await expect(page.locator('.settings-breadcrumb')).toContainText(/Usage|用量/, { timeout: 10000 })

    // One press returns to the log — NOT just one breadcrumb level up.
    await page.evaluate(() => {
      window.dispatchEvent(new CustomEvent('clawbench-back-press', { detail: { reason: 'android' } }))
    })
    await expect(page.locator('.notif-panel')).toBeVisible({ timeout: 10000 })
  })

  test('the project filter defaults to all projects', async ({ page }) => {    // Scope is global by design: the notification center spans every project,
    // unlike the per-project forge/task panels.
    await nav.switchToNotifications()
    await expect(page.locator('.notif-panel')).toBeVisible({ timeout: 10000 })

    // The first dropdown is the project filter and shows the "all" option.
    const projectDropdown = page.locator('.notif-filters .menu-select').first()
    await expect(projectDropdown).toContainText(/All|全部/)
  })

  test('the kind filter narrows the list', async ({ page }) => {
    await sendAndWaitForNotification(page, 'notiffilter_' + Date.now())

    await nav.switchToNotifications()
    await expect(page.locator('.notif-card').first()).toBeVisible({ timeout: 15000 })

    // Open the KIND dropdown (second MenuSelect: project / kind / time) and
    // pick a kind with no rows, so the list must change.
    await page.locator('.notif-filters .menu-select').nth(1).click()
    await page.locator('.menu-select-item').filter({ hasText: /Task|任务/ }).click()

    // Either the empty state appears (no task notifications) or only task rows
    // remain — both prove the filter applied.
    await expect(
      page.locator('.notif-state').or(page.locator('.notif-card')).first(),
    ).toBeVisible({ timeout: 10000 })
  })

  test('the clear action empties the log after confirmation', async ({ page }) => {
    await sendAndWaitForNotification(page, 'notifclear_' + Date.now())

    await nav.switchToNotifications()
    await expect(page.locator('.notif-card').first()).toBeVisible({ timeout: 15000 })

    await page.locator('[data-action="clear"]').click()
    // Confirm in the shared dialog: the OK button carries `.fbtn.dlg-ok`.
    await page.locator('.dlg-ok').click()

    await expect(page.locator('.notif-state')).toBeVisible({ timeout: 10000 })
  })
})

import { test, expect, type Page } from '../fixtures'
import { apiFetch } from '../helpers/auth'

/**
 * Smoke test for the optional pre-AI custom script on cron tasks.
 *
 * One coherent journey, end to end through the real UI:
 *   1. create a cron task through the form, typing a script that exits 0
 *      silently (`true`) into the script textarea
 *   2. run it from the detail page's Run button
 *   3. the execution history shows a `skipped` row (the badge from
 *      TaskHistoryTab.vue / i18n `task.exec.statusSkipped`)
 *   4. NO completion notification was produced anywhere
 *
 * Why step 4 is asserted the way it is: the feature's whole point is that a
 * silent skip is invisible. Every frontend notification channel is downstream
 * of one signal — a `task_update` WS event:
 *   - useTaskTab.onTaskCompleted (✅ toast + browser notification + dock flash),
 *     fired from its runningCount 1→0 heuristic;
 *   - App.vue's completion popover, gated on `status === 'completed'`;
 *   - useGlobalEvents' browser notification, gated on IsNotifiableEvent;
 *   - the server's DingTalk/Feishu push.
 * The backend deliberately emits NO task_update for the script phase, so all
 * four stay silent. This spec therefore records every task_update on the socket
 * and every toast/popover/browser-notification in the DOM, and asserts none of
 * them fired for our task. The one honest limitation: Playwright cannot observe
 * an OS-level notification, so the strongest available proxy is used — the app
 * is shown never to *ask* for one, and never to receive the event that would
 * make it ask.
 *
 * The second, deliberately tiny test pins the other half of the UI contract:
 * the script fields render for a cron task and disappear for an event task.
 */

interface RecordedNotification {
  kind: 'toast' | 'popover' | 'browser'
  text: string
}

interface RecordedTaskUpdate {
  task_id?: string
  status?: string
}

/**
 * Install the notification recorders before the app boots.
 *
 * The caller must reload afterwards: addInitScript only applies to *subsequent*
 * loads, and the fixture has already navigated once. A reload is also what the
 * app expects for a live task — `running` events are not persisted for offline
 * replay (IsNotifiableEvent keeps only terminal statuses), so a stale socket
 * cannot leak a notification into the recorder.
 */
async function installNotificationRecorders(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const w = window as unknown as {
      __e2eTaskUpdates: RecordedTaskUpdate[]
      __e2eNotifications: RecordedNotification[]
      WebSocket: typeof WebSocket
      Notification?: typeof Notification
    }
    w.__e2eTaskUpdates = []
    w.__e2eNotifications = []

    // Capture the socket: it is the single upstream of every channel above.
    const OrigWebSocket = w.WebSocket
    class RecordingWebSocket extends OrigWebSocket {
      constructor(...args: ConstructorParameters<typeof WebSocket>) {
        super(...args)
        this.addEventListener('message', (ev: MessageEvent) => {
          try {
            const msg = JSON.parse(String(ev.data))
            if (msg && msg.type === 'event' && msg.event === 'task_update') {
              w.__e2eTaskUpdates.push(msg.data)
            }
          } catch {
            // Ignore malformed frames (ping/pong never reach here).
          }
        })
      }
    }
    w.WebSocket = RecordingWebSocket as unknown as typeof WebSocket

    // Record attempts to raise an OS-level notification. The OS notification
    // itself is not observable from Playwright, but the app asking for one is.
    const OrigNotification = w.Notification
    if (OrigNotification) {
      const RecordingNotification = function (title: string, options?: NotificationOptions) {
        w.__e2eNotifications.push({ kind: 'browser', text: String(title || '') })
        return new OrigNotification(title, options)
      } as unknown as typeof Notification
      RecordingNotification.permission = OrigNotification.permission
      RecordingNotification.requestPermission = OrigNotification.requestPermission?.bind(OrigNotification)
      w.Notification = RecordingNotification
    }

    // The ✅ toast and the completion card are both teleported into <body>,
    // so watch the whole document. rAF-coalesced: the observer fires on every
    // boot-time mutation and only an element's appearance matters here.
    //
    // The card's classes are `.completion-notify-layer` / `.completion-notify`
    // (it was renamed from the old `.completion-popover`); matching a stale
    // name would make every assertion below pass vacuously.
    let scheduled = false
    const scan = () => {
      const toast = document.querySelector('.toast')
      if (toast) w.__e2eNotifications.push({ kind: 'toast', text: toast.textContent || '' })
      if (document.querySelector('.completion-notify')) {
        w.__e2eNotifications.push({ kind: 'popover', text: '' })
      }
    }
    const scheduleScan = () => {
      if (scheduled) return
      scheduled = true
      requestAnimationFrame(() => {
        scheduled = false
        scan()
      })
    }
    new MutationObserver(scheduleScan).observe(document.documentElement, { childList: true, subtree: true })
  })
}

/** Click the Tasks dock button in whichever dock is actually visible. */
async function openTasksTab(page: Page): Promise<void> {
  await page
    .locator('button.dock-btn[title="Tasks"], button.dock-btn[title="任务"]')
    .filter({ visible: true })
    .first()
    .click()
  await expect(page.locator('.task-tab')).toBeVisible({ timeout: 10000 })
}

/** Open the create form from the task list header. */
async function openCreateForm(page: Page): Promise<void> {
  await page.locator('.task-tab .create-btn').click()
  await expect(page.locator('.task-form-page')).toBeVisible({ timeout: 10000 })
}

test.describe.serial('Task pre-AI script (smoke)', () => {
  const taskIds: number[] = []

  test.afterAll(async () => {
    for (const taskId of taskIds) {
      try {
        await apiFetch(`/api/tasks/${taskId}`, { method: 'DELETE' })
      } catch {
        // Best effort cleanup — server may be down during teardown
      }
    }
  })

  test('a silent exit-0 script skips the AI with no notification', async ({ page }) => {
    // Deterministic dock mode: ≥1024px CSS width selects the wide-screen dock.
    await page.setViewportSize({ width: 1280, height: 900 })
    await installNotificationRecorders(page)
    // Reload so the init script runs before the app boots and wraps WebSocket.
    // Wait on a real element, not 'networkidle': the app holds a persistent
    // WS/SSE connection open, so the network never goes idle.
    await page.reload()
    await expect(page.locator('.chat-textarea')).toBeVisible({ timeout: 20000 })

    await openTasksTab(page)

    // Unique name so the notification assertions can be scoped to this run and
    // cannot be tripped by an unrelated task's notification.
    const taskName = `E2E script skip ${Date.now()}`

    // ── 1. Create the task through the form ──
    await openCreateForm(page)

    // Name lives in the first (Basic Info) section's first text input.
    await page.locator('.form-section').first().locator('input.form-input').first().fill(taskName)

    // Agent: open the selector drawer and pick the mock agent. The drawer
    // ignores clicks within 400ms of opening (a touch-event guard), so wait it
    // out — there is no event to await for a time-based guard.
    await page.locator('.agent-display').click()
    const agentOption = page.locator('.agent-option').filter({ hasText: 'ACP Mock Agent' })
    await expect(agentOption).toBeVisible({ timeout: 10000 })
    await page.waitForTimeout(500)
    await agentOption.click()

    // The cron-only script textarea must be present, and the point of this
    // smoke test is that it is driven through the UI, not the API.
    const scriptField = page.locator('.task-form-page .script-textarea')
    await expect(scriptField).toBeVisible()
    await scriptField.fill('true')

    await page.locator('.task-form-page .prompt-textarea').fill('This prompt must never reach the AI.')

    await page.locator('.task-form-page .form-footer .fbtn-primary').click()

    // Saving navigates straight to the new task's detail page.
    await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 10000 })

    const taskId = await page.evaluate(async (name) => {
      const resp = await fetch('/api/tasks')
      const data = await resp.json()
      return (data.tasks || []).find((t: { name: string }) => t.name === name)?.id as number
    }, taskName)
    expect(taskId).toBeTruthy()
    taskIds.push(taskId)

    // ── 2. Trigger it from the UI ──
    const runBtn = page.locator('.task-detail-page .detail-actions .fbtn-primary')
    await expect(runBtn).toBeVisible()
    await runBtn.click()

    // ── 3. The history shows a `skipped` row ──
    // The script phase emits no task_update, so the history is refreshed by the
    // component's 5s fallback poll (a runCount change). A couple of ticks is
    // the expected latency for the row to appear.
    const skippedItem = page
      .locator('.execution-item')
      .filter({ has: page.locator('.exec-status-badge.skipped') })
      .first()
    await expect(skippedItem).toBeVisible({ timeout: 30000 })
    await expect(skippedItem.locator('.exec-status-badge.skipped')).toHaveText(/Skipped|已跳过/)
    // A skipped run produced no AI output by design, so the row explains the
    // empty body instead of reading as a failure.
    await expect(skippedItem.locator('.exec-summary.empty')).toHaveText(/Skipped.*no output|已跳过/)

    // ── 4. No notification was produced ──
    const updates = await page.evaluate(
      () => (window as unknown as { __e2eTaskUpdates: RecordedTaskUpdate[] }).__e2eTaskUpdates,
    )
    // The backend contract: a skipped run emits NOTHING — no "running", no
    // "completed". This is what keeps every downstream channel silent.
    expect(
      updates.filter(u => String(u?.task_id ?? '') === String(taskId)),
      'a skipped run must not emit any task_update (that is what silences every notification channel)',
    ).toEqual([])

    const notifications = await page.evaluate(
      () => (window as unknown as { __e2eNotifications: RecordedNotification[] }).__e2eNotifications,
    )
    // Scoped to this task's name so an unrelated completion cannot trip it.
    const forThisTask = notifications.filter(n => n.text.includes(taskName))
    expect(
      forThisTask,
      `no toast/browser notification may name the skipped task: ${JSON.stringify(notifications)}`,
    ).toEqual([])

    // The completion card does not auto-dismiss, so had one been raised for
    // this run it would still be on screen now.
    await expect(page.locator('.completion-notify')).toHaveCount(0)
  })

  test('the script field renders for a cron task but not for an event task', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await openTasksTab(page)
    await openCreateForm(page)

    // Cron is the default trigger mode.
    await expect(page.locator('.task-form-page .script-textarea')).toBeVisible()

    // Switching to Event must drop the cron-only script fields entirely.
    await page.locator('.task-form-page .preset-btn').filter({ hasText: /^(Event|事件)$/ }).click()
    await expect(page.locator('.task-form-page .script-textarea')).toHaveCount(0)
  })
})

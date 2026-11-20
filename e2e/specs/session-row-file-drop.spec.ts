import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * Dropping a file-manager entry onto a session ROW must attach it to THAT
 * session's draft.
 *
 * Why this needs its own spec: the chat column's drop handlers cover the chat
 * column only, so the row is the ONLY element that can accept this drag. If the
 * row declines it the browser cancels the whole gesture — the file goes
 * nowhere, which is precisely the regression that appeared when the drop
 * handlers were narrowed off the whole right pane.
 *
 * The drag is synthesised with our own attach MIME and the exact payload
 * `startAttachDrag` writes (`{path,name,isDir}`), which is what the row reads.
 */
test.describe('Session row — file drop attaches to that session', () => {
  let chat: ChatPage
  let nav: NavigationPage

  const ATTACH_MIME = 'application/x-clawbench-attach'

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    nav = new NavigationPage(page)
    await nav.switchToChat()
    await page.waitForTimeout(800)
  })

  test('the row accepts the drag and stages the file in its own draft', async ({ page }) => {
    // Two sessions so "the target session" and "whatever is open" differ.
    await chat.createSessionWithAgent('acp-mock')
    await page.keyboard.press('Escape')
    await expect(page.locator('.bs-overlay')).toHaveCount(0, { timeout: 5000 }).catch(() => {})
    await chat.waitForACPState()

    const otherId = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      return (await resp.json()).sessionId as string
    })
    expect(otherId).toBeTruthy()

    await chat.createSessionWithAgent('acp-mock')
    await page.keyboard.press('Escape')
    await expect(page.locator('.bs-overlay')).toHaveCount(0, { timeout: 5000 }).catch(() => {})
    await chat.waitForACPState()

    await chat.openSessionList()
    await page.waitForTimeout(600)

    const row = page.locator(`.session-row[data-session-id="${otherId}"]`).first()
    await expect(row).toBeVisible({ timeout: 10000 })

    // A row that does not `preventDefault` on dragover is not a valid drop
    // target: the browser would refuse the drop. So acceptance is observable.
    // Dispatched on the row element itself (not via hit-testing coordinates):
    // the drawer may be overlaid, and what matters is the row's own handlers.
    const accepted = await row.evaluate((el: HTMLElement, mime: string) => {
      const dt = new DataTransfer()
      dt.setData(mime, JSON.stringify({ path: '/tmp/e2e-drop.txt', name: 'e2e-drop.txt', isDir: false }))
      dt.setData('text/plain', '/tmp/e2e-drop.txt')
      const ev = new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt })
      el.dispatchEvent(ev)
      return { prevented: ev.defaultPrevented }
    }, ATTACH_MIME)
    expect(accepted.prevented, 'row must accept the file drag or the drop is cancelled').toBe(true)

    // The highlight is a reactive ref, so the class lands on the next tick.
    await expect(row).toHaveClass(/is-quote-drop/, { timeout: 3000 })

    // Now drop it and check the file really lands in THAT session's draft.
    await row.evaluate((el: HTMLElement, mime: string) => {
      const dt = new DataTransfer()
      dt.setData(mime, JSON.stringify({ path: '/tmp/e2e-drop.txt', name: 'e2e-drop.txt', isDir: false }))
      dt.setData('text/plain', '/tmp/e2e-drop.txt')
      el.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt }))
    }, ATTACH_MIME)

    // `add` mode parks the payload in the target's STORED draft and says where
    // it went instead of yanking the user out of the session they are reading —
    // the same contract the quote drop follows. So the observable proof is the
    // toast, then the file appearing once that session is actually opened.
    await expect(
      page.locator('.toast, [class*="toast"]').filter({ hasText: /已加入该会话的输入框|Added to that session/i }).first(),
    ).toBeVisible({ timeout: 10000 })

    // Open the target session and confirm the file is in ITS draft. Clicked via
    // JS: the drawer's own overlay intercepts pointer events in the desktop
    // layout, which is a presentation detail, not what this spec is about.
    await page.evaluate((id) => {
      const item = document.querySelector(
        `.session-row[data-session-id="${id}"] .session-item`,
      ) as HTMLElement | null
      item?.click()
    }, otherId)
    await page.waitForTimeout(800)
    await expect(
      page.locator('.session-item.active').first(),
    ).toHaveAttribute('data-session-id', otherId, { timeout: 10000 }).catch(async () => {
      await expect(row.locator('.session-item')).toHaveClass(/active/, { timeout: 10000 })
    })

    await expect(
      page.locator('[class*="attach"], [class*="file-chip"], [class*="attachment"]')
        .filter({ hasText: 'e2e-drop.txt' }).first(),
    ).toBeVisible({ timeout: 10000 })
  })
})

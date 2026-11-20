import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { NavigationPage } from '../pages/navigation.page'

/**
 * The chat drop hint must disappear when the pointer leaves the chat column —
 * including when it leaves towards the session sidebar, which is the path that
 * actually broke in the desktop app.
 *
 * The original implementation trusted `dragleave`. Measured in the real app,
 * dragging from the chat column onto the session list delivered NO usable
 * leave at all: the hint stayed lit after the file had already been dropped.
 * The overlay is now driven by the `dragover` heartbeat instead — it stays up
 * only while `dragover` keeps arriving, which is why this spec feeds a few
 * heartbeats and then goes silent rather than dispatching a `dragleave`.
 */
test.describe('Chat drop overlay — leaves the column', () => {
  let chat: ChatPage
  let nav: NavigationPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    nav = new NavigationPage(page)
    await nav.switchToChat()
    await page.waitForTimeout(800)
  })

  const hint = (page: import('@playwright/test').Page) => page.locator('.chat-drop-hint')

  test('hint hides once dragover stops arriving', async ({ page }) => {
    const box = (await page.locator('.chat-col').boundingBox())!
    const cx = box.x + box.width / 2
    const cy = box.y + box.height / 2

    const fire = (type: string) =>
      page.evaluate(
        ({ x, y, type }) => {
          const dt = new DataTransfer()
          dt.items.add(new File(['x'], 'a.txt', { type: 'text/plain' }))
          document
            .elementFromPoint(x, y)
            ?.dispatchEvent(
              new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt, clientX: x, clientY: y }),
            )
        },
        { x: cx, y: cy, type },
      )

    await fire('dragenter')
    await expect(hint(page)).toBeVisible({ timeout: 3000 })

    // Heartbeat: while dragover keeps arriving, the hint must stay up.
    for (let i = 0; i < 4; i++) {
      await fire('dragover')
      await page.waitForTimeout(100)
    }
    await expect(hint(page)).toBeVisible()

    // Silence = the pointer is no longer over the column. No `dragleave` is
    // dispatched on purpose: that is exactly the event Chromium fails to
    // deliver on the path that broke.
    await expect(hint(page)).toBeHidden({ timeout: 3000 })
  })

  test('hint does not appear when the column is dragged onto from the sidebar side', async ({ page }) => {
    // Sanity check that the hint is genuinely scoped to the chat column and
    // not to the whole right half (which also contains the session list).
    const sidebar = page.locator('.session-sidebar').first()
    if (!(await sidebar.isVisible())) test.skip()

    await expect(hint(page)).toBeHidden()
  })
})

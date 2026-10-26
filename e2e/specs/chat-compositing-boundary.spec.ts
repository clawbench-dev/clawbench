import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * Android WebView's compositor mis-draws a composited layer when its size
 * changes at turn end (the stale texture is stretched into the delta). The fix
 * is structural: exactly ONE layer boundary, on the stable-size chat panel, and
 * ZERO layer promotion inside it (message rows, bubbles, meta bars).
 *
 * This spec pins the runtime contract in a real engine:
 *   - `.chat-panel-content` IS promoted + clipped in app mode;
 *   - the message row is clipped but NOT promoted (the row is what grows when
 *     the meta bar appears — promoting it is what produced the squashed
 *     meta-bar strip / 9px bubble band);
 *   - the same declarations do not apply outside app mode (desktop web).
 *
 * Source-contract unit tests (chatCompositingBoundary.css.test.ts,
 * chatRowLayerIsolation.css.test.ts) guard the CSS text; this guards that the
 * selectors actually resolve and produce the expected computed values.
 */
test.describe('chat compositing boundary', () => {
  test('one panel boundary, no promoted message rows', async ({ page }) => {
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible()
    await chat.sendMessage('compositing_' + Date.now())

    // The reply must have finished, so the meta bar (the element that grows the
    // row) is present — that is the state the ghost appeared in.
    await expect(page.locator('.chat-message.assistant').last().locator('.chat-meta-bar')).toBeVisible({
      timeout: 20000,
    })

    // Desktop/web: no boundary, no clipping (the bug does not exist here).
    const web = await page.evaluate(() => {
      const panel = document.querySelector('.chat-panel-content') as HTMLElement
      const row = document.querySelector('.chat-message') as HTMLElement
      const cs = (el: HTMLElement) => getComputedStyle(el)
      return {
        panelWillChange: cs(panel).willChange,
        panelContain: cs(panel).contain,
        rowContain: cs(row).contain,
        rowWillChange: cs(row).willChange,
      }
    })
    expect(web.panelWillChange, 'web must not pay a permanent panel layer').toBe('auto')

    // Native host (Android WebView / Electron): the boundary applies.
    await page.evaluate(() => document.documentElement.setAttribute('data-app-mode', ''))

    const native = await page.evaluate(() => {
      const panel = document.querySelector('.chat-panel-content') as HTMLElement
      const rows = document.querySelectorAll('.chat-message')
      const row = rows[rows.length - 1] as HTMLElement
      const cs = (el: HTMLElement) => getComputedStyle(el)
      return {
        panelWillChange: cs(panel).willChange,
        panelContain: cs(panel).contain,
        rowContain: cs(row).contain,
        rowWillChange: cs(row).willChange,
        rowHeight: Math.round(row.getBoundingClientRect().height),
        metaBarVisible: !!row.querySelector('.chat-meta-bar'),
      }
    })

    expect(native.panelWillChange, 'the panel is the single layer boundary').toBe('transform')
    expect(native.panelContain).toMatch(/\bpaint\b/)
    expect(native.rowContain, 'rows stay clipped').toMatch(/\bpaint\b/)
    expect(
      native.rowWillChange,
      'the row must NOT be promoted: it grows when the meta bar appears, and a ' +
      'resized layer is exactly what WebView draws as a squashed ghost strip',
    ).toBe('auto')
    expect(native.metaBarVisible).toBe(true)
    expect(native.rowHeight).toBeGreaterThan(0)
  })
})

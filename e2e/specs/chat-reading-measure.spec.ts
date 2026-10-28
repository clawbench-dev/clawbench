import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * The chat pane's reading measure, verified in a real engine.
 *
 * jsdom has no CSS engine, so chatContentMeasure.css.test.ts can only read the
 * source. What genuinely needs a browser:
 *
 *   1. the container query matches at all, and the pane is really a size query
 *      container (`container-type: inline-size` is readable at runtime);
 *   2. the message column and the composer share ONE left/right edge once the
 *      pane outgrows the measure — that shared edge is the whole point;
 *   3. the shapes flip with the gutter: square band + flat strip when there is
 *      no gutter, rounded card + bottom-docked island when there is.
 *
 * ONE test, ONE send: the send is the expensive, racy part (the mock agent is
 * shared across workers), and the reply is not what the shapes depend on. The
 * two states are produced by resizing the viewport AFTER the reply has settled,
 * so no click ever happens against a resizing page (which is what the app's
 * connection overlay would swallow).
 */

/** Measured geometry of the boxes the measure shapes. */
async function measure(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const q = (s: string) => document.querySelector(s) as HTMLElement | null
    const panel = q('.chat-panel-content')!
    const scroller = q('.chat-messages')!
    const wrapper = q('.chat-input-wrapper')!
    const card = q('.chat-message.assistant .msg-card')
    const cs = (el: HTMLElement) => getComputedStyle(el)
    const wcs = cs(wrapper)
    const scs = cs(scroller)
    const pr = panel.getBoundingClientRect()
    const sr = scroller.getBoundingClientRect()
    const wr = wrapper.getBoundingClientRect()
    const px = (v: string) => Math.round(parseFloat(v) || 0)
    return {
      windowWidth: window.innerWidth,
      panelWidth: Math.round(pr.width),
      panelLeft: Math.round(pr.left),
      containerType: cs(panel).containerType,
      // Border boxes: the scroller's must span the pane (that is where its
      // scrollbar is painted).
      scrollerLeft: Math.round(sr.left),
      scrollerRight: Math.round(sr.right),
      // Content boxes: what the user sees as the column's edges.
      scrollerContentLeft: Math.round(sr.left) + px(scs.paddingLeft),
      scrollerContentRight: Math.round(sr.right) - px(scs.paddingRight),
      wrapperContentLeft: Math.round(wr.left) + px(wcs.paddingLeft),
      wrapperLeft: Math.round(wr.left),
      wrapperRight: Math.round(wr.right),
      wrapperBackground: wcs.backgroundColor,
      wrapperShadow: wcs.boxShadow,
      wrapperRadiusTopLeft: wcs.borderTopLeftRadius,
      wrapperRadiusBottomLeft: wcs.borderBottomLeftRadius,
      wrapperBorderBottomWidth: wcs.borderBottomWidth,
      cardRadius: card ? cs(card).borderTopLeftRadius : null,
    }
  })
}

/** `0px` is square; anything else is a real radius. */
const isSquare = (radius: string | null) => radius === '0px'
const isRound = (radius: string | null) => !!radius && radius !== '0px'

test.describe('chat reading measure', () => {
  test.use({ viewport: { width: 2200, height: 900 } })

  test('shapes flip with the pane gutter', async ({ page }) => {
    const chat = new ChatPage(page)
    await expect(chat.textarea).toBeVisible()

    await chat.sendMessage('measure_' + Date.now())
    // The bubble only has to EXIST: the card's radius is a static rule, not
    // something the turn-completion path sets, so waiting for the meta bar (or
    // for the reply to finish) would only add the mock agent's latency to this
    // test's failure surface.
    await expect(page.locator('.chat-message.assistant .msg-card').last()).toBeVisible({
      timeout: 25000,
    })

    // ── Wide pane: the pane is well past the 900 design px measure ─────────
    const wide = await measure(page)
    expect(wide.containerType, 'the pane must be a size query container').toBe('inline-size')
    expect(wide.panelWidth, 'precondition: the pane starts above the measure').toBeGreaterThan(900)

    // The SCROLLER's box spans the pane, so its scrollbar is painted at the
    // session area's right edge — not glued to the message column. This is why
    // the measure is spent as padding rather than margin.
    expect(wide.scrollerLeft, 'the scrollbar lane sits on the pane edge').toBe(wide.panelLeft)
    expect(
      wide.scrollerRight,
      'the scrollbar lane sits on the pane edge',
    ).toBe(wide.panelLeft + wide.panelWidth)

    // Inside it, the column is inset, and it starts on the SAME vertical line as
    // the composer island's BORDER box — that shared edge is what the measure
    // exists for. (The island's own 8px padding + 1px border sit inside it, so
    // its CONTENT box starts further right; the island's visible edge is the
    // border, and that is what must line up with the messages.)
    expect(
      wide.scrollerContentLeft,
      'message column and composer island must share their left edge',
    ).toBe(wide.wrapperLeft)

    // The reply is a card.
    expect(isRound(wide.cardRadius), 'the reply gains corners once there is a gutter').toBe(true)

    // The composer is a dock island: own surface, top corners only, no hairline.
    expect(wide.wrapperBackground, 'the island must paint its own surface').not.toBe(
      'rgba(0, 0, 0, 0)',
    )
    expect(isRound(wide.wrapperRadiusTopLeft), 'island top corners are rounded').toBe(true)
    expect(
      isSquare(wide.wrapperRadiusBottomLeft),
      'island bottom corners stay square — that edge is the pane edge',
    ).toBe(true)
    expect(wide.wrapperShadow, 'the island border replaces the hairline').toBe('none')
    expect(wide.wrapperBorderBottomWidth, 'no border on the pane-facing edge').toBe('0px')

    // ── Narrow the pane ────────────────────────────────────────────────────
    // 1100px window: still two columns, but the chat pane drops to ~770px.
    await page.setViewportSize({ width: 1100, height: 900 })
    await expect
      .poll(async () => (await measure(page)).panelWidth, { timeout: 8000 })
      .toBeLessThan(900)

    const narrow = await measure(page)
    expect(narrow.panelWidth, 'the pane is now below the measure').toBeLessThan(900)

    // Full-bleed again: no gutter between the pane and the column.
    expect(narrow.scrollerLeft, 'the message area is flush to the pane again').toBe(
      narrow.panelLeft,
    )
    expect(narrow.scrollerRight, 'the message area reaches the pane edge again').toBe(
      narrow.panelLeft + narrow.panelWidth,
    )
    expect(narrow.scrollerContentLeft, 'no gutter at all').toBe(narrow.scrollerLeft)

    expect(narrow.cardRadius, 'the card reverts to a square band').toBe('0px')
    expect(narrow.wrapperShadow, 'the hairline separator comes back').not.toBe('none')
    expect(isSquare(narrow.wrapperRadiusTopLeft), 'the island flattens again').toBe(true)
    expect(narrow.wrapperBorderBottomWidth, 'and loses its border').toBe('0px')
  })
})

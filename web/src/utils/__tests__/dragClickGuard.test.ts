import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import {
  installDragClickGuard,
  isDragRelease,
  recordPress,
  clearPress,
  shouldSuppressClick,
  DRAG_CLICK_THRESHOLD_MOUSE,
  DRAG_CLICK_THRESHOLD_TOUCH,
} from '@/utils/dragClickGuard'

/**
 * The guard exists so a drag-select inside a clickable row does not fire the
 * row's action. Every assertion here is paired with a mutation check in the
 * accompanying notes: removing the corresponding line must make it fail.
 */

/** Build a click-ish event object. `detail` defaults to 1 (a real user click). */
function clickEvent(x: number, y: number, detail = 1) {
  return { clientX: x, clientY: y, detail }
}

describe('isDragRelease', () => {
  it('is false for a click that did not move', () => {
    expect(isDragRelease({ x: 100, y: 100, pointerType: 'mouse' }, { x: 100, y: 100 })).toBe(false)
  })

  it('is false just under the mouse threshold', () => {
    const d = DRAG_CLICK_THRESHOLD_MOUSE
    expect(isDragRelease({ x: 0, y: 0, pointerType: 'mouse' }, { x: d, y: d })).toBe(false)
  })

  it('is true once the mouse moves past the threshold on either axis', () => {
    const d = DRAG_CLICK_THRESHOLD_MOUSE
    expect(isDragRelease({ x: 0, y: 0, pointerType: 'mouse' }, { x: d + 1, y: 0 })).toBe(true)
    expect(isDragRelease({ x: 0, y: 0, pointerType: 'mouse' }, { x: 0, y: d + 1 })).toBe(true)
  })

  it('is true for a movement in the negative direction too', () => {
    // |dx|, not dx: dragging up/left is just as much a drag.
    const d = DRAG_CLICK_THRESHOLD_MOUSE
    expect(isDragRelease({ x: 100, y: 100, pointerType: 'mouse' }, { x: 100 - d - 1, y: 100 })).toBe(true)
  })

  // A finger jitters during a tap, so touch gets a wider window. This is what
  // keeps a slightly-moved tap from being swallowed as a drag on mobile.
  it('tolerates more movement for touch than for mouse', () => {
    const d = DRAG_CLICK_THRESHOLD_MOUSE
    const up = { x: d + 1, y: 0 }

    expect(isDragRelease({ x: 0, y: 0, pointerType: 'mouse' }, up)).toBe(true)
    expect(isDragRelease({ x: 0, y: 0, pointerType: 'touch' }, up)).toBe(false)
  })

  it('treats a missing pointerType as a mouse', () => {
    const d = DRAG_CLICK_THRESHOLD_MOUSE
    expect(isDragRelease({ x: 0, y: 0 }, { x: d + 1, y: 0 })).toBe(true)
  })

  it('uses the touch threshold as the documented constant', () => {
    // Guards against someone lowering the touch threshold to equal the mouse's,
    // which would reintroduce swallowed taps on mobile.
    expect(DRAG_CLICK_THRESHOLD_TOUCH).toBeGreaterThan(DRAG_CLICK_THRESHOLD_MOUSE)
  })
})

describe('shouldSuppressClick', () => {
  afterEach(() => clearPress())

  it('does not suppress when there was no press', () => {
    expect(shouldSuppressClick(clickEvent(0, 0))).toBe(false)
  })

  it('does not suppress a click that stayed put', () => {
    recordPress(50, 50, 'mouse')
    expect(shouldSuppressClick(clickEvent(50, 50))).toBe(false)
  })

  it('suppresses a click that drifted past the threshold', () => {
    recordPress(50, 50, 'mouse')
    expect(shouldSuppressClick(clickEvent(50 + DRAG_CLICK_THRESHOLD_MOUSE + 1, 50))).toBe(true)
  })

  // Keyboard activation goes through el.click(), which dispatches a synthetic
  // click with detail 0. Suppressing it would break Enter/Space on every guarded
  // row. (`isTrusted` cannot be used here: a dispatched event is always
  // untrusted, so it would make the guard untestable AND rely on a property
  // jsdom does not model faithfully.)
  it('never suppresses a programmatic click (detail 0)', () => {
    recordPress(50, 50, 'mouse')
    expect(shouldSuppressClick(clickEvent(200, 200, 0))).toBe(false)
  })

  // The record describes ONE interaction. If it survived the click, a drag could
  // suppress a later, unrelated click.
  it('consumes the press so it cannot affect a second click', () => {
    recordPress(50, 50, 'mouse')
    expect(shouldSuppressClick(clickEvent(200, 200))).toBe(true)
    // Second click, same far-away coordinates: the press is gone, so it is a
    // normal click again.
    expect(shouldSuppressClick(clickEvent(200, 200))).toBe(false)
  })

  it('ignores a non-primary button press', () => {
    // A right-drag must not suppress a following left click.
    recordPress(50, 50, 'mouse', 2)
    expect(shouldSuppressClick(clickEvent(200, 200))).toBe(false)
  })
})

/**
 * End-to-end through real DOM listeners: the whole point is that the guard runs
 * on the document capture phase and stops the event before a row handler sees
 * it.
 */
describe('installDragClickGuard', () => {
  let root: HTMLElement
  let dispose: () => void
  let rowClicks: number
  let row: HTMLElement

  beforeEach(() => {
    clearPress()
    root = document.createElement('div')
    row = document.createElement('div')
    row.className = 'clickable-row'
    row.textContent = 'feature/login'
    root.appendChild(row)
    document.body.appendChild(root)
    rowClicks = 0
    row.addEventListener('click', () => { rowClicks++ })
    dispose = installDragClickGuard(root)
  })

  afterEach(() => {
    dispose()
    root.remove()
    clearPress()
  })

  function pointerDown(x: number, y: number, pointerType = 'mouse') {
    const e = new Event('pointerdown', { bubbles: true, cancelable: true })
    Object.assign(e, { clientX: x, clientY: y, pointerType, button: 0 })
    root.dispatchEvent(e)
  }

  function click(x: number, y: number, detail = 1) {
    // `new MouseEvent('click')` defaults to detail 0, which is the PROGRAMMATIC
    // shape. A real user click carries the click count, so set it explicitly.
    const e = new MouseEvent('click', {
      bubbles: true, cancelable: true, clientX: x, clientY: y, detail,
    })
    row.dispatchEvent(e)
    return e
  }

  it('lets a normal click through to the row', () => {
    pointerDown(10, 10)
    click(10, 10)
    expect(rowClicks).toBe(1)
  })

  it('stops a drag-select click from reaching the row', () => {
    pointerDown(10, 10)
    // The user dragged 40px to select the text, then released.
    const e = click(50, 10)

    expect(rowClicks).toBe(0)
    expect(e.defaultPrevented).toBe(true)
  })

  it('does not stop the NEXT click after a drag', () => {
    // Regression shape: if the press record were not consumed, this second,
    // legitimate click would also be swallowed.
    pointerDown(10, 10)
    click(50, 10)
    expect(rowClicks).toBe(0)

    pointerDown(10, 10)
    click(10, 10)
    expect(rowClicks).toBe(1)
  })

  it('does not stop a programmatic click after a drag', () => {
    // The keyboard path: user drags (guard arms), then activates a row by
    // keyboard. detail === 0 must keep that working.
    pointerDown(10, 10)
    click(50, 10, 0)

    expect(rowClicks).toBe(1)
  })

  it('ignores a movement that stays within the threshold', () => {
    pointerDown(10, 10)
    click(10 + DRAG_CLICK_THRESHOLD_MOUSE, 10)

    expect(rowClicks).toBe(1)
  })

  it('clears a pending press when the pointer is cancelled', () => {
    pointerDown(10, 10)
    root.dispatchEvent(new Event('pointercancel', { bubbles: true }))

    // A cancel (e.g. the gesture was taken over) must not leave a record that
    // suppresses a later unrelated click.
    click(200, 200)
    expect(rowClicks).toBe(1)
  })

  // A drag that releases outside the window produces no click at all, leaving a
  // stale press behind. The next real click starts with its OWN pointerdown,
  // which overwrites the record — so the stale position can never suppress it.
  it('does not let a stale press suppress a later click', () => {
    pointerDown(10, 10)
    // Released far away, outside the root: no click is dispatched.
    // Now a completely separate click somewhere else.
    pointerDown(300, 300)
    click(300, 300)

    expect(rowClicks).toBe(1)
  })

  it('removes every listener on dispose', () => {
    dispose()
    pointerDown(10, 10)
    click(50, 10)

    expect(rowClicks).toBe(1)
  })
})

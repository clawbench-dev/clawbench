import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { shouldPreventTerminalContextMenu, useTerminalGestures } from '@/composables/useTerminalGestures'

function makeTouch(clientX: number, clientY: number): Touch {
  return { clientX, clientY } as Touch
}

function makeTouchEvent(
  type: string,
  touches: Touch[],
  changedTouches: Touch[] = touches
): TouchEvent & { preventDefault: ReturnType<typeof vi.fn> } {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'touches', { value: touches })
  Object.defineProperty(event, 'changedTouches', { value: changedTouches })
  event.preventDefault = vi.fn()
  return event as TouchEvent & { preventDefault: ReturnType<typeof vi.fn> }
}

function dispatchTouch(
  el: HTMLElement,
  type: string,
  touches: Touch[],
  changedTouches: Touch[] = touches
) {
  const event = makeTouchEvent(type, touches, changedTouches)
  el.dispatchEvent(event)
  return event
}

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

function setupGestures() {
  const el = document.createElement('div')
  document.body.appendChild(el)

  const sent: string[] = []
  const hints: string[] = []
  const zoomDeltas: number[] = []
  const scrollDeltas: number[] = []
  const gestures = useTerminalGestures(ref(el), {
    sendArrowUp: () => sent.push('up'),
    sendArrowDown: () => sent.push('down'),
    sendArrowLeft: () => sent.push('left'),
    sendArrowRight: () => sent.push('right'),
    sendPageUp: () => sent.push('pageup'),
    sendPageDown: () => sent.push('pagedown'),
    sendTab: () => sent.push('tab'),
    onPinchZoom: (delta) => zoomDeltas.push(delta),
    onGestureHint: (symbol) => hints.push(symbol),
    onTouchScroll: (deltaY: number) => scrollDeltas.push(deltaY),
  })
  gestures.attach()

  return { el, sent, hints, zoomDeltas, scrollDeltas, gestures }
}

describe('shouldPreventTerminalContextMenu', () => {
  it('allows the native long-press copy menu when gestures are disabled', () => {
    expect(shouldPreventTerminalContextMenu(false)).toBe(false)
  })

  it('suppresses the native context menu while gestures are enabled', () => {
    expect(shouldPreventTerminalContextMenu(true)).toBe(true)
  })
})

describe('useTerminalGestures', () => {
  it('prevents the native double-tap selection side effect when sending Tab', () => {
    const { el, sent, hints } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(40, 40)])
    dispatchTouch(el, 'touchend', [], [makeTouch(40, 40)])
    dispatchTouch(el, 'touchstart', [makeTouch(42, 42)])
    const secondTapEnd = dispatchTouch(el, 'touchend', [], [makeTouch(42, 42)])

    expect(sent).toEqual(['tab'])
    expect(hints).toEqual(['⇥'])
    expect(secondTapEnd.preventDefault).toHaveBeenCalled()
  })

  it('does not prevent default touch handling for a short stationary tap', () => {
    const { el, sent, zoomDeltas } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    const touchEnd = dispatchTouch(el, 'touchend', [], [makeTouch(60, 60)])

    expect(sent).toEqual([])
    expect(zoomDeltas).toEqual([])
    expect(touchEnd.preventDefault).not.toHaveBeenCalled()
  })

  it('cancels pending gesture state when the browser cancels the touch sequence', () => {
    vi.useFakeTimers()
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    dispatchTouch(el, 'touchcancel', [], [makeTouch(60, 60)])

    expect(sent).toEqual([])
  })

  it('prevents native selection/scroll only after a swipe gesture is recognized', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(100, 100)])
    const smallMove = dispatchTouch(el, 'touchmove', [makeTouch(108, 102)])
    const swipeMove = dispatchTouch(el, 'touchmove', [makeTouch(150, 102)])

    expect(sent).toEqual(['right'])
    expect(smallMove.preventDefault).not.toHaveBeenCalled()
    expect(swipeMove.preventDefault).toHaveBeenCalled()
  })

  it('prevents native pinch handling while applying terminal zoom', () => {
    const { el, zoomDeltas } = setupGestures()

    const pinchStart = dispatchTouch(el, 'touchstart', [makeTouch(0, 0), makeTouch(20, 0)])
    const pinchMove = dispatchTouch(el, 'touchmove', [makeTouch(0, 0), makeTouch(40, 0)])

    expect(zoomDeltas).toEqual([2])
    expect(pinchStart.preventDefault).toHaveBeenCalled()
    expect(pinchMove.preventDefault).toHaveBeenCalled()
  })

  it('maps two-finger upward and downward swipes to Page Up and Page Down', () => {
    const { el, sent, hints, zoomDeltas } = setupGestures()

    const upStart = dispatchTouch(el, 'touchstart', [makeTouch(50, 100), makeTouch(80, 100)])
    const upMove = dispatchTouch(el, 'touchmove', [makeTouch(50, 50), makeTouch(80, 50)])
    dispatchTouch(el, 'touchend', [], [makeTouch(50, 50), makeTouch(80, 50)])

    const downStart = dispatchTouch(el, 'touchstart', [makeTouch(50, 50), makeTouch(80, 50)])
    const downMove = dispatchTouch(el, 'touchmove', [makeTouch(50, 100), makeTouch(80, 100)])

    expect(sent).toEqual(['pageup', 'pagedown'])
    expect(hints).toEqual(['⇞', '⇟'])
    expect(zoomDeltas).toEqual([])
    expect(upStart.preventDefault).toHaveBeenCalled()
    expect(upMove.preventDefault).toHaveBeenCalled()
    expect(downStart.preventDefault).toHaveBeenCalled()
    expect(downMove.preventDefault).toHaveBeenCalled()
  })

  it('keeps a drifting pinch gesture in zoom mode instead of sending Page Up or Page Down', () => {
    const { el, sent, zoomDeltas } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(50, 100), makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(40, 65), makeTouch(100, 65)])
    dispatchTouch(el, 'touchmove', [makeTouch(40, 25), makeTouch(100, 25)])

    expect(sent).toEqual([])
    expect(zoomDeltas).toEqual([3])
  })

  it('does not treat one mostly stationary finger as a two-finger page swipe', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(50, 100), makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(50, 100), makeTouch(80, 30)])

    expect(sent).toEqual([])
  })

  it('scrolls terminal output with one-finger vertical drags when gestures are disabled', () => {
    const { el, sent, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    const smallMove = dispatchTouch(el, 'touchmove', [makeTouch(82, 108)])
    const firstScrollMove = dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    const secondScrollMove = dispatchTouch(el, 'touchmove', [makeTouch(84, 155)])

    expect(gestures.enabled.value).toBe(false)
    expect(sent).toEqual([])
    expect(scrollDeltas).toEqual([40, 15])
    expect(smallMove.preventDefault).not.toHaveBeenCalled()
    expect(firstScrollMove.preventDefault).toHaveBeenCalled()
    expect(secondScrollMove.preventDefault).toHaveBeenCalled()
  })

  it('stops a disabled-mode scroll sequence when a second finger is added', () => {
    const { el, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140), makeTouch(120, 140)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 200)])

    expect(scrollDeltas).toEqual([40])
  })

  it('restores native touch behavior when gestures are disabled before a scroll starts', () => {
    const { el, gestures } = setupGestures()

    gestures.toggle()

    expect(gestures.enabled.value).toBe(false)
    expect(el.style.touchAction).toBe('auto')
  })

  it('does not disable native touch selection when gestures are toggled back on', () => {
    const { el, gestures } = setupGestures()

    gestures.toggle()
    expect(gestures.enabled.value).toBe(false)
    gestures.toggle()

    expect(gestures.enabled.value).toBe(true)
    expect(el.style.touchAction).not.toBe('none')
  })
})

describe('useTerminalGestures — uncovered branches', () => {
  it('sends an arrow key on touchend when the swipe was too fast for touchmove to detect', () => {
    const { el, sent, hints } = setupGestures()

    // Start touch, then release at a position that constitutes a swipe
    // without any intermediate touchmove events
    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    const touchEnd = dispatchTouch(el, 'touchend', [], [makeTouch(130, 60)])

    // The direction was only detected on touchend (swipe-on-lift)
    expect(sent).toEqual(['right'])
    expect(hints).toEqual(['→'])
    expect(touchEnd.preventDefault).toHaveBeenCalled()
  })

  it('sends a swipe-up arrow on touchend with no touchmove', () => {
    const { el, sent, hints } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 100)])
    dispatchTouch(el, 'touchend', [], [makeTouch(60, 40)])

    expect(sent).toEqual(['up'])
    expect(hints).toEqual(['↑'])
  })

  it('sends a swipe-down arrow on touchend with no touchmove', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 40)])
    dispatchTouch(el, 'touchend', [], [makeTouch(60, 100)])

    expect(sent).toEqual(['down'])
  })

  it('sends a swipe-left arrow on touchend with no touchmove', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(100, 60)])
    dispatchTouch(el, 'touchend', [], [makeTouch(40, 60)])

    expect(sent).toEqual(['left'])
  })

  it('auto-repeats arrow keys during a sustained hold', () => {
    vi.useFakeTimers()
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    dispatchTouch(el, 'touchmove', [makeTouch(120, 60)])

    // Initial send from the first direction detection
    expect(sent).toEqual(['right'])

    // After initial delay (500ms), the repeat interval is created
    vi.advanceTimersByTime(500)

    // First repeat fires after another 150ms (REPEAT_INTERVAL)
    vi.advanceTimersByTime(150)
    expect(sent.length).toBe(2)
    expect(sent[1]).toBe('right')

    // Second repeat after another 150ms
    vi.advanceTimersByTime(150)
    expect(sent.length).toBe(3)
    expect(sent[2]).toBe('right')
  })

  it('stops auto-repeat on touchend', () => {
    vi.useFakeTimers()
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    dispatchTouch(el, 'touchmove', [makeTouch(120, 60)])
    expect(sent).toEqual(['right'])

    // Release before repeat starts
    dispatchTouch(el, 'touchend', [], [makeTouch(120, 60)])

    // Advance past the full repeat cycle — no more arrows should be sent
    vi.advanceTimersByTime(650)
    expect(sent).toEqual(['right']) // no auto-repeat
  })

  it('clears touchAction on detach', () => {
    const { el, gestures } = setupGestures()

    // After attach, touchAction should be set
    expect(el.style.touchAction).toBeTruthy()

    gestures.detach()
    expect(el.style.touchAction).toBe('')
  })

  it('handles single-tap that is not a double-tap (records tap for future)', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    const touchEnd = dispatchTouch(el, 'touchend', [], [makeTouch(60, 60)])

    // Single tap: no arrow, no tab
    expect(sent).toEqual([])
    // Short tap without movement: no preventDefault (allows native selection start)
    expect(touchEnd.preventDefault).not.toHaveBeenCalled()
  })

  it('resets double-tap timer after a triple tap (avoids triple-tab)', () => {
    const { el, sent } = setupGestures()

    // First tap
    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    dispatchTouch(el, 'touchend', [], [makeTouch(60, 60)])
    // Second tap → double-tap → Tab
    dispatchTouch(el, 'touchstart', [makeTouch(62, 62)])
    dispatchTouch(el, 'touchend', [], [makeTouch(62, 62)])
    // Third tap → should NOT produce another Tab
    dispatchTouch(el, 'touchstart', [makeTouch(63, 63)])
    dispatchTouch(el, 'touchend', [], [makeTouch(63, 63)])

    expect(sent).toEqual(['tab'])
  })

  it('handles touchstart with 3+ fingers gracefully', () => {
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(10, 10), makeTouch(20, 20), makeTouch(30, 30)])
    // Should not crash or send any command
    expect(sent).toEqual([])
  })

  it('resets pinch state when one finger lifts from two-finger gesture', () => {
    const { el, zoomDeltas } = setupGestures()

    // Start pinch
    dispatchTouch(el, 'touchstart', [makeTouch(0, 0), makeTouch(20, 0)])
    dispatchTouch(el, 'touchmove', [makeTouch(0, 0), makeTouch(40, 0)])
    expect(zoomDeltas).toEqual([2])

    // Lift one finger (touchend with 0 remaining touches)
    dispatchTouch(el, 'touchend', [], [makeTouch(0, 0), makeTouch(40, 0)])

    // A subsequent two-finger start should work fresh
    dispatchTouch(el, 'touchstart', [makeTouch(0, 0), makeTouch(20, 0)])
    dispatchTouch(el, 'touchmove', [makeTouch(0, 0), makeTouch(40, 0)])
    expect(zoomDeltas).toEqual([2, 2])
  })

  it('does not send scroll deltas when movement is primarily horizontal in disabled mode', () => {
    const { el, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(150, 108)]) // mostly horizontal

    expect(scrollDeltas).toEqual([])
  })

  it('resets disabled scroll state on touchend after scrolling', () => {
    const { el, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    expect(scrollDeltas).toEqual([40])

    // End the scroll gesture
    const touchEnd = dispatchTouch(el, 'touchend', [], [makeTouch(84, 140)])
    expect(touchEnd.preventDefault).toHaveBeenCalled()
  })

  it('resets disabled scroll state on touchcancel', () => {
    const { el, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    expect(scrollDeltas).toEqual([40])

    // Cancel the scroll gesture
    dispatchTouch(el, 'touchcancel', [], [makeTouch(84, 140)])

    // Subsequent touch should start fresh
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 120)])
    expect(scrollDeltas).toEqual([40, 20])
  })

  it('does not preventDefault on touchend when disabled scroll was not active', () => {
    const { el, gestures } = setupGestures()

    gestures.toggle()
    const touchEnd = dispatchTouch(el, 'touchend', [], [makeTouch(80, 100)])
    expect(touchEnd.preventDefault).not.toHaveBeenCalled()
  })

  it('allows second finger to disable scroll in disabled mode via touchstart', () => {
    const { el, scrollDeltas, gestures } = setupGestures()

    gestures.toggle()
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    expect(scrollDeltas).toEqual([40])

    // Two-finger touchstart should reset scroll tracking
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100), makeTouch(120, 100)])

    // Subsequent single-finger move should not scroll (tracking was reset)
    dispatchTouch(el, 'touchmove', [makeTouch(84, 160)])
    expect(scrollDeltas).toEqual([40]) // no new scroll delta
  })

  it('stops repeat interval that is already running when touchend fires', () => {
    vi.useFakeTimers()
    const { el, sent } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    dispatchTouch(el, 'touchmove', [makeTouch(120, 60)])
    expect(sent).toEqual(['right'])

    // Wait for repeat timer to fire → creates the interval
    vi.advanceTimersByTime(500)
    // Let the interval fire once to confirm it's active
    vi.advanceTimersByTime(150)
    expect(sent.length).toBe(2)

    // Now release — stopRepeat should clear the active interval
    dispatchTouch(el, 'touchend', [], [makeTouch(120, 60)])

    // Advance past another repeat cycle — no more arrows should be sent
    vi.advanceTimersByTime(300)
    expect(sent.length).toBe(2) // no additional sends after release
  })

  it('changes direction during hold-to-repeat', () => {
    vi.useFakeTimers()
    const { el, sent, hints } = setupGestures()

    dispatchTouch(el, 'touchstart', [makeTouch(60, 60)])
    // Start swiping right
    dispatchTouch(el, 'touchmove', [makeTouch(120, 60)])
    expect(sent).toEqual(['right'])
    expect(hints).toEqual(['→'])

    // Change direction to down — should stop old repeat, start new one
    dispatchTouch(el, 'touchmove', [makeTouch(120, 120)])
    expect(sent).toEqual(['right', 'down'])
    expect(hints).toEqual(['→', '↓'])

    // The new direction repeat should be active
    vi.advanceTimersByTime(500)
    vi.advanceTimersByTime(150)
    expect(sent.length).toBe(3)
    expect(sent[2]).toBe('down')
  })
})
